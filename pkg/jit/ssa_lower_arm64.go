//go:build !amd64

package jit

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// vMFrameAccOffset is the byte offset of VMFrame.Acc computed at init time.
// vMAccNumValOff is the offset of JSValue.NumVal within JSValue.
// vMAccTagOff is the offset of JSValue.Tag within JSValue.
var (
	vMFrameAccOffset uintptr
	vMAccNumValOff   uintptr
	vMAccTagOff      uintptr
)

func init() {
	var frame js.VMFrame
	vMFrameAccOffset = unsafe.Offsetof(frame.Acc)
	var val js.JSValue
	vMAccNumValOff = unsafe.Offsetof(val.NumVal)
	vMAccTagOff = unsafe.Offsetof(val.Tag)
}

// lowerSSAToARM64 translates an SSA graph into ARM64 machine code using the
// provided register allocation. Each SSA node with an assigned register is
// lowered to one or more ARM64 instructions.
//
// The generated code follows the Go ABI:
//
//	func turbofanEntry(frame *js.VMFrame)
//
// R0 = frame pointer on entry. Prologue stores this in R19 (callee-saved
// VM register). Arithmetic ops produce results in allocated integer registers.
// The final return stores the result via the accResultStub helper.
//
// Supported operations:
//   - SSAConst: MOVZ/MOVK immediate value into destination register
//   - SSAAdd:  ADD (int) or FADD (float64)
//   - SSASub:  SUB (int) or FSUB (float64)
//   - SSAMul:  MUL (int) or FMUL (float64)
//   - SSADiv:  SDIV (int) or FDIV (float64)
//   - SSALoad: LDR from memory (base + offset via Feedback.Offset)
//   - SSAStore: STR to memory (base + offset via Feedback.Offset)
//   - SSACall: BLR through callee register
//   - SSAPhi: MOV from predecessor value (no-op in linear scan; copy here)
//   - SSACmp: CMP + CSET for boolean result
//   - SSANeg: NEG (int) or FNEG (float64)
//   - SSANot: MVN bitwise NOT
//   - SSAToNumber: FCVTZS/SCVTF conversion or MOV passthrough
//   - SSABranch: CBNZ/CBZ conditional or B unconditional (block-level)
//   - SSAAnd/SSAOr/SSAXor: bitwise AND/ORR/EOR
//   - SSAShl/SSAShr/SSASar: LSL/LSR/ASR variable shifts
//   - SSAFSub/SSAFMul/SSAFDiv/SSAFCmp: float-only arithmetic and compare
//   - SSAReturn: store result to frame.Acc + RET
func lowerSSAToARM64(g *SSAGraph, ra *RegAlloc) (*CodeBuf, error) {
	buf, err := NewCodeBuf(len(g.Blocks) * 512)
	if err != nil {
		return nil, fmt.Errorf("ssa_lower_arm64: %w", err)
	}
	as := NewAssembler(buf)

	// --- Prologue: store frame pointer in R19, save LR in R20 with PAC ---
	// PACIASP signs LR with SP context before saving (ARMv8.3+).
	if hasARM64PAC {
		as.PACIASP()
	}
	as.MOV(REG_VM1, REG_LR) // R20 = LR (save signed return address)
	as.MOV(REG_VM0, REG_R0) // R19 = frame*

	// --- Label map: one label per basic block for branch targets ---
	blockLabels := make(map[int]*Label)
	for _, bb := range g.Blocks {
		blockLabels[bb.ID] = NewLabel()
	}

	for _, bb := range g.Blocks {
		// Bind the block label at the start of each block.
		as.Bind(blockLabels[bb.ID])

		for _, node := range bb.Nodes {
			reg := ra.nodeToReg[node]
			if reg < 0 {
				continue
			}
			switch node.Op {
			case SSAConst:
				if node.Type == SSAFloat64 {
					val := node.Value.ToNumber()
					bits := math.Float64bits(val)
					as.MOVZ(reg, uint16(bits&0xFFFF), 0)
					if bits>>16 != 0 {
						as.MOVK(reg, uint16((bits>>16)&0xFFFF), 16)
					}
					if bits>>32 != 0 {
						as.MOVK(reg, uint16((bits>>32)&0xFFFF), 32)
					}
					if bits>>48 != 0 {
						as.MOVK(reg, uint16((bits>>48)&0xFFFF), 48)
					}
				} else {
					// Integer constant: load the numeric value as int64.
					val := int64(node.Value.ToNumber())
					if val >= 0 {
						as.MOVZ(reg, uint16(val&0xFFFF), 0)
						if val>>16 != 0 {
							as.MOVK(reg, uint16((val>>16)&0xFFFF), 16)
						}
						if val>>32 != 0 {
							as.MOVK(reg, uint16((val>>32)&0xFFFF), 32)
						}
						if val>>48 != 0 {
							as.MOVK(reg, uint16((val>>48)&0xFFFF), 48)
						}
					} else {
						// Negative value: use MOVN + MOVK.
						as.MOVN(reg, uint16((^val)&0xFFFF), 0)
						if (^val)>>16 != 0 {
							as.MOVK(reg, uint16(((^val)>>16)&0xFFFF), 16)
						}
						if (^val)>>32 != 0 {
							as.MOVK(reg, uint16(((^val)>>32)&0xFFFF), 32)
						}
						if (^val)>>48 != 0 {
							as.MOVK(reg, uint16(((^val)>>48)&0xFFFF), 48)
						}
					}
				}

			case SSAAdd:
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				if node.Type == SSAFloat64 {
					as.FADD(reg, r1, r2)
				} else {
					as.ADD(reg, r1, r2)
				}

			case SSASub:
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				if node.Type == SSAFloat64 {
					as.FSUB(reg, r1, r2)
				} else {
					as.SUB(reg, r1, r2)
				}

			case SSAMul:
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				if node.Type == SSAFloat64 {
					as.FMUL(reg, r1, r2)
				} else {
					as.MUL(reg, r1, r2)
				}

			case SSADiv:
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				if node.Type == SSAFloat64 {
					as.FDIV(reg, r1, r2)
				} else {
					as.SDIV(reg, r1, r2)
				}

			case SSALoad:
				// Load from memory: reg = mem[base + offset]
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				offset := 0
				if node.Feedback != nil {
					offset = node.Feedback.Offset
				}
				as.LDR(reg, baseReg, offset)

			case SSAStore:
				// Store to memory: mem[base + offset] = src
				if len(node.Args) < 2 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				srcReg := ra.nodeToReg[node.Args[1]]
				if baseReg < 0 || srcReg < 0 {
					continue
				}
				offset := 0
				if node.Feedback != nil {
					offset = node.Feedback.Offset
				}
				as.STR(srcReg, baseReg, offset)

			case SSACall:
				// Indirect call through callee register.
				if len(node.Args) < 1 {
					continue
				}
				calleeReg := ra.nodeToReg[node.Args[0]]
				if calleeReg < 0 {
					continue
				}
				as.BLR(calleeReg)
				// Result comes back in R0; move to destination register.
				as.MOV(reg, REG_R0)

			case SSAPhi:
				// Phi is resolved by SSA construction. At code-gen time,
				// the predecessor block already computed the value into
				// the phi's register. No code emitted — MOV is a no-op
				// when reg == predReg (common), but safe as copy.
				if len(node.Args) > 0 {
					predReg := ra.nodeToReg[node.Args[0]]
					if predReg >= 0 && predReg != reg {
						as.MOV(reg, predReg)
					}
				}

			case SSACmp:
				// Compare two values, produce boolean (0 or 1) in reg.
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSET(reg, 0) // EQ (0) — set to 1 if equal, else 0

			case SSAFCmp:
				// Float64 comparison.
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.FCMP(r1, r2)
				as.CSET(reg, 0) // EQ

			case SSABranch:
				// Conditional branch: if Args[0] != 0, jump to Labels[0].
				if len(node.Args) > 0 && len(node.Labels) > 0 {
					condReg := ra.nodeToReg[node.Args[0]]
					if condReg >= 0 {
						as.CBNZ(condReg, node.Labels[0])
					}
				}

			case SSAAnd:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.AND(reg, r1, r2)

			case SSAOr:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.ORR(reg, r1, r2)

			case SSAXor:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.EOR(reg, r1, r2)

			case SSAShl:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.LSL(reg, r1, r2)

			case SSAShr:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.LSR(reg, r1, r2)

			case SSASar:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.ASR(reg, r1, r2)

			case SSAFSub:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.FSUB(reg, r1, r2)

			case SSAFMul:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.FMUL(reg, r1, r2)

			case SSAFDiv:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.FDIV(reg, r1, r2)

			case SSANeg:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				if node.Type == SSAFloat64 {
					as.FNEG(reg, r1)
				} else {
					as.NEG(reg, r1)
				}

			case SSANot:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.MVN(reg, r1)

			case SSAToNumber:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				// Fast path: if already a number type, just MOV.
				// Full ToNumber (string→float, etc.) requires runtime call;
				// here we emit MOV and rely on IC deopt for type mismatch.
				as.MOV(reg, r1)

			case SSAMod:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.SDIV(REG_R16, r1, r2) // R16 = r1 / r2
				as.MSUB(reg, REG_R16, r2, r1)

			case SSAFMod:
				// Float modulo: not natively supported on ARM64.
				// Placeholder: MOV src to dst, real mod needs runtime call.
				if len(node.Args) < 1 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.MOV(reg, r1)

			case SSAMin:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSEL(reg, r1, r2, 11) // LT → pick r1 if r1 < r2, else r2

			case SSAMax:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSEL(reg, r1, r2, 12) // GT → pick r1 if r1 > r2, else r2

			case SSAAbs:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				// reg = (r1 >= 0) ? r1 : -r1
				as.NEG(REG_R16, r1)           // R16 = -r1
				as.CMP(r1, REG_ZR)            // cmp r1, #0
				as.CSEL(reg, r1, REG_R16, 10) // GE → pick r1 if >=0, else -r1

			case SSAClz:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.CLZ(reg, r1)

			case SSACtz:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				// CTZ via RBIT + CLZ
				as.RBIT(REG_R16, r1)
				as.CLZ(reg, REG_R16)

			case SSARev:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.REV(reg, r1)

			case SSAExtractBits:
				// Args[0] = value, Args[1] = lsb, Args[2] = width
				// These come as SSA nodes; we need their constant values.
				if len(node.Args) < 3 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				lsb := uint16(node.Args[1].Value.ToNumber())
				width := uint16(node.Args[2].Value.ToNumber())
				if width == 0 {
					continue
				}
				as.UBFX(reg, r1, lsb, width)

			case SSASignExtend8:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.SXTB(reg, r1)

			case SSASignExtend16:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.SXTH(reg, r1)

			case SSASignExtend32:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.SXTW(reg, r1)

			case SSAMovFPToInt:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.FMOV(r1, reg) // FMOV Xreg, Vsrc

			case SSAMovIntToFP:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.FMOV_XD(r1, reg) // FMOV Vreg, Xsrc

			case SSALoadGlobal:
				// Load global: dereference global slot pointer from feedback.
				// Args[0] = global object base (or R19 VM frame as fallback).
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				offset := 0
				if node.Feedback != nil {
					offset = node.Feedback.Offset
				}
				as.LDR(reg, baseReg, offset)

			case SSAStoreGlobal:
				// Store global: write value to global slot.
				if len(node.Args) < 2 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				valReg := ra.nodeToReg[node.Args[1]]
				if baseReg < 0 || valReg < 0 {
					continue
				}
				offset := 0
				if node.Feedback != nil {
					offset = node.Feedback.Offset
				}
				as.STR(valReg, baseReg, offset)

			case SSAIsObject:
				// Type guard: check if JSValue.Tag indicates an object.
				// Tags: Undef=0, Null=1, Number=2, Bool=3, String=4, Symbol=5, Object=6, BigInt=7
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(REG_R16, baseReg, tagOff)
				as.MOVZ(REG_R17, 6, 0) // TagObject = 6
				as.CMP(REG_R16, REG_R17)
				as.CSET(reg, 0) // EQ

			case SSAIsString:
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(REG_R16, baseReg, tagOff)
				as.MOVZ(REG_R17, 4, 0) // TagString = 4
				as.CMP(REG_R16, REG_R17)
				as.CSET(reg, 0) // EQ

			case SSATruncateToInt32:
				// Truncate float64 to int32 via FCVTZS.
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.FCVTZS(reg, r1)

			case SSAFloat64ToInt32:
				// Float64 to int32 with rounding toward zero.
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.FCVTZS(reg, r1)

			case SSAInt32ToFloat64:
				// Int32 to float64.
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.SCVTF(reg, r1)

			case SSANaNCheck:
				// Check if float64 is NaN: FCMP sets V=1 for unordered.
				// Result: reg = 1 if NaN, 0 otherwise.
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				as.FCMP(r1, r1) // compare with self — only NaN != NaN
				as.CSET(reg, 6) // VS → set if overflow (unordered/NaN)

			case SSANegZeroCheck:
				// Check for negative zero: -0.0 has all bits zero except sign bit.
				// Test if value is zero AND sign bit is set.
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				// Negative zero = 0x8000000000000000
				as.MOVZ(REG_R16, 0, 0)
				as.MOVK(REG_R16, 0x8000, 48)
				as.CMP(r1, REG_R16)
				as.CSET(reg, 0) // EQ

			case SSAThrow:
				// Throw JS exception: call runtime throw helper.
				// Args[0] = exception value pointer.
				if len(node.Args) < 1 {
					continue
				}
				excReg := ra.nodeToReg[node.Args[0]]
				if excReg < 0 {
					continue
				}
				as.MOV(REG_R0, excReg)
				// Placeholder: BLR to throw stub (patched at link time).
				as.NOP()

			case SSANewArray:
				// Allocate new JS array via runtime call.
				// Args[0] = initial length or capacity hint.
				if len(node.Args) < 1 {
					continue
				}
				lenReg := ra.nodeToReg[node.Args[0]]
				if lenReg < 0 {
					continue
				}
				as.MOV(REG_R0, lenReg)
				// Placeholder: BLR to array allocation stub.
				as.NOP()
				as.MOV(reg, REG_R0)

			case SSALoadElement:
				// Load array element: elem = array[index].
				// Args[0] = array base pointer, Args[1] = index.
				if len(node.Args) < 2 {
					continue
				}
				arrReg := ra.nodeToReg[node.Args[0]]
				idxReg := ra.nodeToReg[node.Args[1]]
				if arrReg < 0 || idxReg < 0 {
					continue
				}
				// Scaled load: LDR Xreg, [Xbase, Xidx, LSL #3]
				// For simplicity, use plain LDR with base+idx*8 computed.
				as.LSL(REG_R16, idxReg, 3) // R16 = idx * 8
				as.ADD(REG_R16, arrReg, REG_R16)
				as.LDR(reg, REG_R16, 0)

			case SSAStoreElement:
				// Store array element: array[index] = value.
				if len(node.Args) < 3 {
					continue
				}
				arrReg := ra.nodeToReg[node.Args[0]]
				idxReg := ra.nodeToReg[node.Args[1]]
				valReg := ra.nodeToReg[node.Args[2]]
				if arrReg < 0 || idxReg < 0 || valReg < 0 {
					continue
				}
				as.LSL(REG_R16, idxReg, 3) // R16 = idx * 8
				as.ADD(REG_R16, arrReg, REG_R16)
				as.STR(valReg, REG_R16, 0)

			case SSAStackCheck:
				// Stack overflow guard: compare SP against limit.
				// If SP < limit, call stack overflow handler.
				// Args[0] = stack limit pointer (from VM frame).
				if len(node.Args) < 1 {
					continue
				}
				limitReg := ra.nodeToReg[node.Args[0]]
				if limitReg < 0 {
					continue
				}
				as.MOV(REG_R16, REG_SP)
				as.CMP(REG_R16, limitReg)
				// B.LO to overflow handler (NOP placeholder).
				as.NOP()

			case SSAMoveConst:
				// Move small 16-bit constant: MOVZ reg, #imm16, LSL #0.
				// Lighter than full SSAConst (no MOVK cascade).
				if node.Value.Tag == 0 {
					// Uninitialized constant: MOV #0.
					as.MOVZ(reg, 0, 0)
				} else {
					imm := uint16(node.Value.ToNumber())
					as.MOVZ(reg, imm, 0)
				}

			case SSAInterruptCheck:
				// Preemption check: load interrupt flag, branch if set.
				// Uses R19 (VM frame) base to access preempt flag offset.
				preemptOff := int(vMFrameAccOffset) // placeholder: preempt flag offset
				as.LDR(REG_R16, REG_VM0, preemptOff)
				as.CBNZ(REG_R16, nil) // nil = unresolved label (patched lazily)

			case SSALazyDeoptContinuation:
				// Lazy deopt: record deopt reason and state, then jump to
				// interpreter at the correct bytecode PC.
				// Args[0] = deopt reason code, implicit BCPC from node.BCPC.
				// Emitted as a NOP sled for the deoptimizer to patch.
				as.NOP() // placeholder for deopt entry
				as.NOP() // placeholder for state save
				as.NOP() // placeholder for branch to interpreter

			case SSAIsUndefined:
				// Check tag byte for TagUndefined (0).
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(REG_R16, baseReg, tagOff)
				as.CMP(REG_R16, REG_ZR) // tag == 0 (Undefined)
				as.CSET(reg, 0)         // EQ

			case SSAIsNullOrUndefined:
				// Check: tag is 0 (Undefined) or 1 (Null).
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(REG_R16, baseReg, tagOff)
				as.CMP(REG_R16, 2) // tag < 2 → Undef(0) or Null(1)
				as.CSET(reg, 3)    // CC (carry clear / unsigned less than)

			case SSAIsBoolean:
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(REG_R16, baseReg, tagOff)
				as.MOVZ(REG_R17, 3, 0) // TagBool = 3
				as.CMP(REG_R16, REG_R17)
				as.CSET(reg, 0) // EQ

			case SSAIsInt32:
				// Check if value is a tagged int32: tag == TagNumber(2)
				// AND the value fits in 32-bit signed range.
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(REG_R16, baseReg, tagOff)
				as.MOVZ(REG_R17, 2, 0) // TagNumber = 2
				as.CMP(REG_R16, REG_R17)
				as.CSET(reg, 0) // EQ for now (full int32 range check needs deopt)

			case SSAObjectLiteral:
				// Create empty object literal via runtime call.
				// No arguments — creates {} with default prototype.
				as.NOP() // placeholder: BLR to object literal allocator
				as.MOV(reg, REG_R0)

			case SSASetProperty:
				// Set named property: obj.prop = value.
				// Args[0] = object, Args[1] = value.
				if len(node.Args) < 2 {
					continue
				}
				objReg := ra.nodeToReg[node.Args[0]]
				valReg := ra.nodeToReg[node.Args[1]]
				if objReg < 0 || valReg < 0 {
					continue
				}
				offset := 0
				if node.Feedback != nil {
					offset = node.Feedback.Offset
				}
				as.STR(valReg, objReg, offset)

			case SSAGetProperty:
				// Get named property: result = obj.prop.
				if len(node.Args) < 1 {
					continue
				}
				objReg := ra.nodeToReg[node.Args[0]]
				if objReg < 0 {
					continue
				}
				offset := 0
				if node.Feedback != nil {
					offset = node.Feedback.Offset
				}
				as.LDR(reg, objReg, offset)

			case SSAForInStart:
				// Start for-in enumeration: get enumerator from object.
				// Args[0] = object. Result = enumerator handle.
				if len(node.Args) < 1 {
					continue
				}
				objReg := ra.nodeToReg[node.Args[0]]
				if objReg < 0 {
					continue
				}
				as.MOV(REG_R0, objReg)
				as.NOP() // placeholder: BLR to for-in start helper
				as.MOV(reg, REG_R0)

			case SSACreateClosure:
				// Create closure: allocate function + captured context.
				// Args[0] = function template, Args[1] = context object.
				if len(node.Args) < 2 {
					continue
				}
				fnReg := ra.nodeToReg[node.Args[0]]
				ctxReg := ra.nodeToReg[node.Args[1]]
				if fnReg < 0 || ctxReg < 0 {
					continue
				}
				as.MOV(REG_R0, fnReg)
				as.MOV(REG_R1, ctxReg)
				as.NOP() // placeholder: BLR to closure allocator
				as.MOV(reg, REG_R0)

			case SSATypeof:
				// typeof operator: return type string tag.
				// Reads tag byte, maps to typeof string via lookup.
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(reg, baseReg, tagOff) // Load tag as result (caller maps tag→string)

			case SSADeleteProperty:
				// Delete property from object.
				// Args[0] = object. Feedback supplies property offset.
				if len(node.Args) < 1 {
					continue
				}
				objReg := ra.nodeToReg[node.Args[0]]
				if objReg < 0 {
					continue
				}
				as.MOV(REG_R0, objReg)
				as.NOP() // placeholder: BLR to delete helper
				as.MOV(reg, REG_R0)

			case SSAEq:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSET(reg, 0) // EQ

			case SSANe:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSET(reg, 1) // NE

			case SSALt:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSET(reg, 11) // LT

			case SSALe:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSET(reg, 13) // LE

			case SSAGt:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSET(reg, 12) // GT

			case SSAGe:
				if len(node.Args) < 2 {
					continue
				}
				r1 := ra.nodeToReg[node.Args[0]]
				r2 := ra.nodeToReg[node.Args[1]]
				if r1 < 0 || r2 < 0 {
					continue
				}
				as.CMP(r1, r2)
				as.CSET(reg, 10) // GE

			case SSAZero:
				// Zero constant: MOV Xreg, XZR
				as.MOV(reg, REG_ZR)

			case SSACastIntToFloat:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				// SCVTF Dreg, Wsrc — 32-bit int to 64-bit float.
				as.SCVTF(reg, r1)

			case SSACastFloatToInt:
				r1 := ra.nodeToReg[node.Args[0]]
				if r1 < 0 {
					continue
				}
				// FCVTZS Wreg, Dsrc — 64-bit float to 32-bit int.
				as.FCVTZS(reg, r1)

			case SSAIsNumber:
				// Type guard: load tag byte, compare to TagNumber (2).
				// Expects base pointer in Args[0].
				if len(node.Args) < 1 {
					continue
				}
				baseReg := ra.nodeToReg[node.Args[0]]
				if baseReg < 0 {
					continue
				}
				tagOff := int(vMFrameAccOffset + vMAccTagOff)
				as.LDR(REG_R16, baseReg, tagOff)
				as.MOVZ(REG_R17, 2, 0) // TagNumber = 2
				as.CMP(REG_R16, REG_R17)
				as.CSET(reg, 0) // EQ → 1 if number, 0 otherwise

			case SSACondMove:
				// Conditional move: reg = cond ? Args[1] : Args[2]
				if len(node.Args) < 3 {
					continue
				}
				condReg := ra.nodeToReg[node.Args[0]]
				r1 := ra.nodeToReg[node.Args[1]]
				r2 := ra.nodeToReg[node.Args[2]]
				if condReg < 0 || r1 < 0 || r2 < 0 {
					continue
				}
				// Test condReg != 0, then CSEL NE.
				as.CMP(condReg, REG_ZR)
				as.CSEL(reg, r1, r2, 1) // NE → pick r1 if cond!=0, else r2

			case SSADeopt:
				// Deoptimization: trigger a runtime deopt by branching to
				// a lazy deopt stub. Emitted as BRK #1 for now (debug trap).
				// In production this would call the deoptimizer.
				as.NOP() // placeholder: patch with deopt call stub

			case SSACheckBounds:
				// Bounds check: if index >= length, deopt.
				// Args[0] = index, Args[1] = length.
				if len(node.Args) < 2 {
					continue
				}
				idxReg := ra.nodeToReg[node.Args[0]]
				lenReg := ra.nodeToReg[node.Args[1]]
				if idxReg < 0 || lenReg < 0 {
					continue
				}
				as.CMP(idxReg, lenReg)
				// If index >= length, trigger deopt (placeholder BRK).
				as.NOP() // placeholder: B.HS deopt_label

			case SSANew:
				// New object construction: call runtime helper.
				// Args[0] = constructor function.
				if len(node.Args) < 1 {
					continue
				}
				ctorReg := ra.nodeToReg[node.Args[0]]
				if ctorReg < 0 {
					continue
				}
				as.BLR(ctorReg)
				as.MOV(reg, REG_R0)

			case SSADebugBreak:
				// Debug breakpoint: BRK #0
				as.MOVZ(REG_R16, 0, 0)
				as.NOP() // placeholder: BRK #0

			case SSAReturn:
				// Store result directly to frame.Acc using known offsets.
				if len(node.Args) > 0 {
					retReg := ra.nodeToReg[node.Args[0]]
					if retReg >= 0 {
						// Store int64 result as float64 bits to frame.Acc.NumVal.
						numOff := int(vMFrameAccOffset + vMAccNumValOff)
						as.STR(retReg, REG_VM0, numOff)
						// Set frame.Acc.Tag = TagNumber (2) using byte store.
						tagOff := int(vMFrameAccOffset + vMAccTagOff)
						as.MOVZ(REG_R16, 2, 0) // R16 = 2
						as.STRB(REG_R16, REG_VM0, tagOff)
					}
				}
				// Restore LR from callee-saved register, authenticate, return.
				as.MOV(REG_LR, REG_VM1)
				if hasARM64PAC {
					as.AUTIASP()
				}
				as.RET()
			}

			// Block-level: after processing all nodes in a block, emit
			// fall-through branch if the block has successors and no
			// explicit branch was emitted as the last node.
		}

		// After block nodes: emit control flow if needed.
		// If the block ends with a branch node, it was already handled.
		// Otherwise, if there's a single successor, emit unconditional B.
		// If two successors and no explicit branch, emit based on last CMP.
		if len(bb.Successors) == 1 {
			as.B(blockLabels[bb.Successors[0].ID])
		} else if len(bb.Successors) == 2 {
			// Conditional: BNE to second successor (fall-through is first).
			as.BNE(blockLabels[bb.Successors[1].ID])
			as.B(blockLabels[bb.Successors[0].ID])
		}
	}

	// Epilogue fallback: return if no explicit return was emitted.
	as.MOV(REG_LR, REG_VM1)
	if hasARM64PAC {
		as.AUTIASP()
	}
	as.RET()

	return buf, nil
}
