//go:build amd64

// sparkplug_amd64.go — AMD64 Sparkplug baseline JIT compiler (Tier 1).
//
// Compiles bytecode from js.BytecodeFunction into AMD64 machine code.
// The generated code takes a *js.VMFrame in RAX and executes the bytecode
// directly, using native branches for control flow.
//
// Register convention (AMD64, Go ABI):
//
//	RAX     = first Go argument (frame *js.VMFrame) on entry
//	R12     = frame pointer (holds *js.VMFrame throughout)
//	R13     = scratch (tag constants, etc.)
//	R14     = $g$ goroutine pointer (NEVER clobber — saved/restored)
//	R15     = JIT temp
//	RDX,RCX = scratch
//	RBP     = native frame pointer
//	R8-R11  = scratch / Go ABI args
//	X0-X1   = float64 operations
//
// Frame layout (native AMD64 stack):
//
//	RBP+0   = saved caller RBP
//	RBP-8   = saved R14 ($g$)
//	RBP-16  = reserved
//	RBP-24  = reserved
package jit

import (
	"math"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// AMD64 Sparkplug constants.
const (
	amd64FrameReserve = 32 // reserve 32 bytes for locals/alignment
	amd64PrologueSize = 16 // PUSH RBP (1) + MOV RBP,RSP (3) + SUB RSP,imm (7) + PUSH R14 (2) ≈ 13
)

// CompileSparkplug compiles a BytecodeFunction into AMD64 machine code.
// Returns a CodeBuf containing the executable code, or an error.
//
// The generated code follows the Go ABI calling convention:
//
//	func sparkplugEntry(frame *js.VMFrame)
//
// RAX = frame pointer on entry, saved to R12.
func CompileSparkplug(bf *js.BytecodeFunction) (*CodeBuf, error) {
	// Estimate code size: each bytecode → ~64 bytes AMD64 on average
	estSize := len(bf.Instructions)*96 + 256
	buf, err := NewCodeBuf(estSize)
	if err != nil {
		return nil, err
	}
	as := NewAssembler(buf)

	// --- Prologue: set up native AMD64 frame ---
	// RAX holds *js.VMFrame from caller. Save it in R12 (scratch).
	// Build standard AMD64 stack frame: PUSH RBP / MOV RBP,RSP / SUB RSP,reserve / PUSH R14.
	as.AMD64_PUSH(REG_RBP)
	as.AMD64_MOV_RR(REG_RBP, REG_RSP)
	as.AMD64_SUB_RI(REG_RSP, amd64FrameReserve)
	as.AMD64_PUSH(REG_R14) // save $g$ goroutine pointer

	// Save frame* from RAX into R12 (our VMFrame pointer).
	as.AMD64_MOV_RR(REG_R12, REG_RAX)

	// --- Pre-create labels for all PC targets ---
	labels := make(map[int]*Label)
	for pc := range bf.Instructions {
		labels[pc] = NewLabel()
	}
	epilogue := NewLabel()
	deoptStub := NewLabel()

	// --- IC slot offset tracking ---
	numSlots := 0
	if bf.ICVector != nil {
		numSlots = len(bf.ICVector.Slots)
	}
	icSlotOffsets := make([]int, numSlots)
	for i := range icSlotOffsets {
		icSlotOffsets[i] = -1
	}

	// --- PC mapping for deoptimization ---
	pcToNative := make(map[int]int)
	nativeToPc := make(map[int]int)

	// --- Compile each bytecode instruction ---
	for pc, instr := range bf.Instructions {
		as.AMD64_Bind(labels[pc])
		nativeOff := as.buf.Pos()
		pcToNative[pc] = nativeOff
		nativeToPc[nativeOff] = pc

		// Insert preemption check at backward jump targets (loop headers).
		if pc > 0 && hasBackwardBranch(bf, pc) {
			EmitPreemptCheckAMD64(as, deoptStub)
		}

		emitAMD64SparkplugOp(as, &instr, bf, labels, epilogue, deoptStub, pc, icSlotOffsets)
	}

	// Store PC mapping on the BytecodeFunction for deoptimization.
	bf.PcToNative = pcToNative
	bf.NativeToPc = nativeToPc

	// --- Epilogue: restore R14, add back frame reserve, restore RBP, return ---
	as.AMD64_Bind(epilogue)
	as.AMD64_POP(REG_R14)             // restore $g$
	as.AMD64_MOV_RR(REG_RSP, REG_RBP) // RSP = RBP (unwinds SUB RSP,reserve)
	as.AMD64_POP(REG_RBP)             // restore caller's frame pointer
	as.AMD64_RET()

	// --- Deoptimization stub ---
	emitAMD64DeoptStub(as, deoptStub)

	// Build DeoptimizationInputData.
	deoptData := &DeoptimizationInputData{
		Points: make([]DeoptPoint, 0, len(bf.Instructions)),
	}
	for pc, nativeOff := range pcToNative {
		deoptData.Points = append(deoptData.Points, DeoptPoint{
			NativeOffset: nativeOff,
			BytecodePC:   pc,
		})
	}
	for i := range deoptData.RegMap {
		deoptData.RegMap[i] = i
	}
	bf.DeoptData = deoptData

	// Register code buffer for runtime IC patching.
	rxAddr := buf.RXAddr()
	RegisterCodeBuf(rxAddr, buf, icSlotOffsets)

	return buf, nil
}

// emitAMD64DeoptStub emits the deoptimization stub.
// Clears InSparkplug flag and returns to Go caller (interpreter).
func emitAMD64DeoptStub(as *Assembler, stub *Label) {
	as.AMD64_Bind(stub)

	// Clear frame.InSparkplug: MOV byte [R12 + inSparkplugOff], 0
	// Use MOV_STORE with 0 value.
	as.AMD64_XOR_RR(REG_RCX, REG_RCX) // RCX = 0
	inSparkplugOff := int(unsafe.Offsetof(js.VMFrame{}.InSparkplug))
	if inSparkplugOff <= 127 && inSparkplugOff >= -128 {
		// Store byte — use MOV_STORE for simplicity (writes 8 bytes but InTurboFan
		// is adjacent bool so the zero write is safe).
		_ = inSparkplugOff
	}
	// Use 8-byte store: zero both InSparkplug and InTurboFan atomically.
	// Actually, use MOV with imm=0 and store.
	as.AMD64_XOR_RR(REG_RDX, REG_RDX) // RDX = 0
	// Store 8 bytes (covers both flags) at inSparkplugOff.
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(inSparkplugOff))

	// Restore R14, RBP, and return to caller.
	as.AMD64_POP(REG_R14)
	as.AMD64_MOV_RR(REG_RSP, REG_RBP)
	as.AMD64_POP(REG_RBP)
	as.AMD64_RET()
}

// hasBackwardBranch checks if any Jump/JumpIfFalse/JumpIfTrue targets pc
// from a later bytecode offset (backward branch = loop header).
func hasBackwardBranch(bf *js.BytecodeFunction, pc int) bool {
	for i, instr := range bf.Instructions {
		switch instr.Op {
		case js.OpJump, js.OpJumpIfFalse, js.OpJumpIfTrue,
			js.OpJumpIfToBooleanTrue, js.OpJumpIfToBooleanFalse:
			target := int(instr.OperandA)
			if target == pc && i > pc {
				return true
			}
		}
	}
	return false
}

// emitAMD64SparkplugOp dispatches to per-opcode AMD64 emitters.
func emitAMD64SparkplugOp(as *Assembler, instr *js.Instruction, bf *js.BytecodeFunction, labels map[int]*Label, epilogue *Label, deoptStub *Label, pc int, icSlotOffsets []int) {
	_ = pc
	_ = icSlotOffsets

	switch instr.Op {
	case js.OpNop:
		as.AMD64_NOP()

	case js.OpLdaSmi:
		emitAMD64LdaSmi(as, instr)

	case js.OpLdaConstant:
		emitAMD64LdaConstant(as, instr)

	case js.OpLdaZero:
		emitAMD64LdaZero(as)

	case js.OpLdaOne:
		emitAMD64LdaOne(as)

	case js.OpLdaUndefined:
		emitAMD64LdaUndefined(as)

	case js.OpLdaNull:
		emitAMD64LdaNull(as)

	case js.OpLdaTrue:
		emitAMD64LdaTrue(as)

	case js.OpLdaFalse:
		emitAMD64LdaFalse(as)

	case js.OpStar:
		emitAMD64Star(as, instr)

	case js.OpLdar:
		emitAMD64Ldar(as, instr)

	case js.OpMov:
		emitAMD64Mov(as, instr)

	case js.OpDup:
		emitAMD64Star(as, instr) // Dup = Star (copy Acc to register)

	case js.OpAdd:
		emitAMD64ArithFast(as, instr, 0, deoptStub)

	case js.OpSub:
		emitAMD64ArithFast(as, instr, 1, deoptStub)

	case js.OpMul:
		emitAMD64ArithFast(as, instr, 2, deoptStub)

	case js.OpDiv:
		emitAMD64ArithFast(as, instr, 3, deoptStub)

	case js.OpNegate:
		emitAMD64Negate(as, instr, deoptStub)

	case js.OpReturn:
		emitAMD64Return(as, bf, epilogue)

	case js.OpJump:
		target := int(instr.OperandA)
		if l, ok := labels[target]; ok {
			as.AMD64_JMP(l)
		}

	case js.OpJumpIfFalse:
		emitAMD64JumpIfFalse(as, instr, labels, deoptStub)

	case js.OpJumpIfTrue:
		emitAMD64JumpIfTrue(as, instr, labels, deoptStub)

	case js.OpCall:
		emitAMD64Call(as, instr, deoptStub)

	default:
		// Unsupported opcode: deoptimize to interpreter.
		as.AMD64_JMP(deoptStub)
	}
}

// --- emitAMD64LdaSmi: store small integer to Acc ---

func emitAMD64LdaSmi(as *Assembler, instr *js.Instruction) {
	val := int8(instr.OperandA)

	// Acc.Tag = TagNumber (0x0300)
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accOffset+tagWordOff))

	// Acc.NumVal = float64(val)
	fbits := math.Float64bits(float64(val))
	as.AMD64_MOV_RI(REG_RCX, fbits)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))

	// Zero out StrVal (first 16 bytes) and ObjVal.
	as.AMD64_XOR_RR(REG_RDX, REG_RDX)
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset))                                           // StrVal bytes 0-7
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset+8))                                         // StrVal bytes 8-15
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))) // ObjVal
}

// --- emitAMD64LdaConstant: load constant from pool → Acc ---

func emitAMD64LdaConstant(as *Assembler, instr *js.Instruction) {
	idx := int(instr.OperandA)

	// Load frame.Func.Constants slice data pointer.
	constsOff := int(unsafe.Offsetof(js.BytecodeFunction{}.Constants))
	funcOff := int(unsafe.Offsetof(js.VMFrame{}.Func))

	// R15 = frame.Func
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(funcOff))
	// R15 = frame.Func.Constants data pointer (offset 0 of slice header = data ptr)
	as.AMD64_MOV_LOAD(REG_R15, REG_R15, int8(constsOff))

	constSlot := idx * jsValueSize

	// Copy Constants[idx] → Acc: 8 × 8-byte MOV (64 bytes).
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(constSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}
}

// --- emitAMD64Mov: copy Regs[OperandB] → Regs[OperandA] (64 bytes) ---

func emitAMD64Mov(as *Assembler, instr *js.Instruction) {
	dst := int(instr.OperandA)
	src := int(instr.OperandB)
	dstSlot := dst * jsValueSize
	srcSlot := src * jsValueSize

	// Load Regs slice data pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Copy Regs[src] → Regs[dst]: 8 × 8-byte MOV.
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(srcSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R15, int8(dstSlot+i))
	}
}

// --- emitAMD64Star: copy Acc → Regs[OperandA] (64 bytes) ---

func emitAMD64Star(as *Assembler, instr *js.Instruction) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	// Load Regs slice data pointer: R15 = frame.Regs (data ptr at regsOff).
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Copy Acc → Regs[reg]: 8 × 8-byte MOV (64 bytes total).
	for i := 0; i < 64; i += 8 {
		off := accOffset + i
		as.AMD64_MOV_LOAD(REG_RCX, REG_R12, int8(off))
		as.AMD64_MOV_STORE(REG_RCX, REG_R15, int8(regSlot+i))
	}
}

// --- emitAMD64Ldar: copy Regs[OperandA] → Acc (64 bytes) ---

func emitAMD64Ldar(as *Assembler, instr *js.Instruction) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	// Load Regs slice data pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Copy Regs[reg] → Acc: 8 × 8-byte MOV.
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(regSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}
}

// --- emitAMD64ArithFast: Add/Sub/Mul via XMM (fast path) ---

func emitAMD64ArithFast(as *Assembler, instr *js.Instruction, op int, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer: R15 = frame.Regs data ptr.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Load Acc tag word and NumVal.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))

	// Load Lhs tag word and NumVal from Regs[lhs].
	as.AMD64_MOV_LOAD(REG_R10, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	// Guard: both must be TagNumber (0x0300).
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)
	as.AMD64_CMP_RR(REG_R10, REG_R13)
	as.AMD64_JNE(slowPath)

	// Move float64 bits to XMM registers.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)  // X0 = Acc.NumVal
	as.AMD64_MOVQ_XR(REG_X1, REG_R11) // X1 = Regs[lhs].NumVal

	// Perform FP operation.
	switch op {
	case 0:
		as.AMD64_ADDSD(REG_X0, REG_X1) // X0 += X1
	case 1:
		as.AMD64_SUBSD(REG_X0, REG_X1) // X0 -= X1
	case 2:
		as.AMD64_MULSD(REG_X0, REG_X1) // X0 *= X1
	case 3:
		as.AMD64_DIVSD(REG_X0, REG_X1) // X0 /= X1
	}

	// Move result back to GP register and store to Acc.
	as.AMD64_MOVQ_RX(REG_R9, REG_X0)                               // R9 = X0 (result float64)
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff)) // Acc.NumVal = result
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))        // Acc tag = TagNumber

	as.AMD64_JMP(done)

	// Slow path: deoptimize.
	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)

	as.AMD64_Bind(done)
}

// --- emitAMD64Return: set PC past end and branch to epilogue ---

func emitAMD64Return(as *Assembler, bf *js.BytecodeFunction, epilogue *Label) {
	pcOff := int(unsafe.Offsetof(js.VMFrame{}.PC))
	as.AMD64_MOV_RI(REG_RCX, uint64(len(bf.Instructions)))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(pcOff))
	as.AMD64_JMP(epilogue)
}

// --- emitAMD64JumpIfFalse: tag-based truthy check, branch on falsy ---

func emitAMD64JumpIfFalse(as *Assembler, instr *js.Instruction, labels map[int]*Label, deoptStub *Label) {
	target := int(instr.OperandA)
	l := labels[target]
	slowPath := NewLabel()
	nextPC := NewLabel()

	// Load Acc tag word.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined (0) → falsy → jump to target.
	as.AMD64_TEST_RR(REG_R8, REG_R8) // sets ZF if R8 == 0
	as.AMD64_JZ(l)

	// Null (0x0100) → falsy → jump.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(l)

	// TagBoolean: (tag_word & 0xFF00) == 0x0200.
	as.AMD64_MOV_RI(REG_R9, 0xFF00)
	as.AMD64_AND_RR(REG_R10, REG_R8) // R10 = R10 & R8... wait, AND_RR(dst, src) is dst &= src
	// Need to preserve R8. Let's use R10 = R8 then mask.
	as.AMD64_MOV_RR(REG_R10, REG_R8)
	as.AMD64_AND_RR(REG_R10, REG_R9) // R10 = R8 & 0xFF00
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R10, REG_R9)
	as.AMD64_JNE(slowPath) // not boolean → slow path

	// Boolean: extract BoolVal (low byte). If 0 → jump (falsy).
	as.AMD64_MOV_RI(REG_R9, 1)
	as.AMD64_AND_RR(REG_R8, REG_R9) // R8 = R8 & 1 (BoolVal)
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(l) // false → jump

	// True → fall through.
	as.AMD64_JMP(nextPC)

	// Slow path: deoptimize.
	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)

	as.AMD64_Bind(nextPC)
}

// --- emitAMD64Call: OpCall deoptimizes to interpreter ---

func emitAMD64Call(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	calleeReg := int(instr.OperandA)
	calleeSlot := calleeReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Load callee tag word and ObjVal.
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(calleeSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(calleeSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	// Guard: callee must be an object (tag 0x0500).
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: callee.ObjVal != nil.
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Full call requires Go helper → deoptimize.
	as.AMD64_JMP(slowPath)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)

	as.AMD64_Bind(done)
}

// --- emitAMD64LdaZero: store TagNumber 0.0 to Acc ---

func emitAMD64LdaZero(as *Assembler) {
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX) // RCX = 0 (float64(0))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_XOR_RR(REG_RDX, REG_RDX)
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
}

// --- emitAMD64LdaOne: store TagNumber 1.0 to Acc ---

func emitAMD64LdaOne(as *Assembler) {
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	fbits := math.Float64bits(1.0) // 0x3FF0000000000000
	as.AMD64_MOV_RI(REG_RCX, fbits)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_XOR_RR(REG_RDX, REG_RDX)
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RDX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
}

// --- emitAMD64LdaUndefined: store TagUndefined (0x0000) ---

func emitAMD64LdaUndefined(as *Assembler) {
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
}

// --- emitAMD64LdaNull: store TagNull (0x0100) ---

func emitAMD64LdaNull(as *Assembler) {
	as.AMD64_MOV_RI(REG_R13, 0x0100)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
}

// --- emitAMD64LdaTrue: store TagBoolean true (0x0201) ---

func emitAMD64LdaTrue(as *Assembler) {
	as.AMD64_MOV_RI(REG_R13, 0x0201)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
}

// --- emitAMD64LdaFalse: store TagBoolean false (0x0200) ---

func emitAMD64LdaFalse(as *Assembler) {
	as.AMD64_MOV_RI(REG_R13, 0x0200)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
}

// --- emitAMD64Negate: OpNegate fast path ---

func emitAMD64Negate(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	// Load Acc tag and NumVal.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))

	// Guard: must be TagNumber.
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Negate: X0 = -Acc.NumVal; store result.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9) // X0 = Acc.NumVal
	as.AMD64_XORPD(REG_X1, REG_X1)   // X1 = 0.0
	as.AMD64_SUBSD(REG_X1, REG_X0)   // X1 = 0.0 - X0 = -Acc.NumVal
	as.AMD64_MOVQ_RX(REG_R9, REG_X1) // R9 = -Acc.NumVal
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)

	as.AMD64_Bind(done)
}

// --- emitAMD64JumpIfTrue: tag-based truthy check, branch on truthy ---

func emitAMD64JumpIfTrue(as *Assembler, instr *js.Instruction, labels map[int]*Label, deoptStub *Label) {
	target := int(instr.OperandA)
	l := labels[target]
	slowPath := NewLabel()
	nextPC := NewLabel()

	// Load Acc tag word.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined (0) → falsy → don't jump.
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(nextPC)

	// Null (0x0100) → falsy → don't jump.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(nextPC)

	// TagBoolean: (tag_word & 0xFF00) == 0x0200.
	as.AMD64_MOV_RR(REG_R10, REG_R8)
	as.AMD64_MOV_RI(REG_R9, 0xFF00)
	as.AMD64_AND_RR(REG_R10, REG_R9)
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R10, REG_R9)
	as.AMD64_JNE(slowPath)

	// Boolean: extract BoolVal. If 1 → jump (truthy).
	as.AMD64_MOV_RI(REG_R9, 1)
	as.AMD64_TEST_RR(REG_R8, REG_R9) // R8 & 1 → ZF if BoolVal==0
	as.AMD64_JNZ(l)                  // true → jump

	// False → fall through.
	as.AMD64_JMP(nextPC)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)

	as.AMD64_Bind(nextPC)
}

// init sets up the AMD64 Sparkplug compile hook.
func init() {
	SparkplugCompile = func(bf interface{}) (uintptr, error) {
		code, err := CompileSparkplug(bf.(*js.BytecodeFunction))
		if err != nil {
			return 0, err
		}
		if code == nil {
			return 0, nil
		}
		return code.RXAddr(), nil
	}

	js.SparkplugCompiler = func(bf *js.BytecodeFunction) (uintptr, error) {
		return SparkplugCompile(bf)
	}
}
