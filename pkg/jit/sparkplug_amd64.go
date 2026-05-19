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
	"math/rand"
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

	// --- Per-compilation random cookie for constant blinding (SEC-1) ---
	cookie := uint64(rand.Uint32())<<32 | uint64(rand.Uint32())
	as.BlindingCookie = cookie

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

	// --- Comparison ops ---
	case js.OpStrictEq:
		emitAMD64StrictEq(as, instr, deoptStub)
	case js.OpEq:
		emitAMD64Eq(as, instr, deoptStub)
	case js.OpNotEq:
		emitAMD64NotEq(as, instr, deoptStub)
	case js.OpStrictNotEq:
		emitAMD64StrictNotEq(as, instr, deoptStub)
	case js.OpLessThan:
		emitAMD64LessThan(as, instr, deoptStub)
	case js.OpGreaterThan:
		emitAMD64GreaterThan(as, instr, deoptStub)
	case js.OpLessEq:
		emitAMD64LessEq(as, instr, deoptStub)
	case js.OpGreaterEq:
		emitAMD64GreaterEq(as, instr, deoptStub)

	// --- Bitwise ops ---
	case js.OpBitwiseAnd:
		emitAMD64BitwiseFast(as, instr, 0, deoptStub)
	case js.OpBitwiseOr:
		emitAMD64BitwiseFast(as, instr, 1, deoptStub)
	case js.OpBitwiseXor:
		emitAMD64BitwiseFast(as, instr, 2, deoptStub)
	case js.OpBitwiseNot:
		emitAMD64BitwiseNot(as, instr, deoptStub)

	// --- Shift ops ---
	case js.OpShiftLeft:
		emitAMD64ShiftFast(as, instr, 0, deoptStub)
	case js.OpShiftRight:
		emitAMD64ShiftFast(as, instr, 1, deoptStub)
	case js.OpShiftRightZero:
		emitAMD64ShiftFast(as, instr, 2, deoptStub)

	// --- Logical ops ---
	case js.OpLogicalNot:
		emitAMD64LogicalNot(as, instr, deoptStub)
	case js.OpLogicalAnd:
		emitAMD64LogicalAnd(as, instr, deoptStub)
	case js.OpLogicalOr:
		emitAMD64LogicalOr(as, instr, deoptStub)

	// --- Type ops ---
	case js.OpToBoolean:
		emitAMD64ToBoolean(as, instr, deoptStub)
	case js.OpToNumber:
		emitAMD64ToNumber(as, instr, deoptStub)
	case js.OpToString:
		emitAMD64ToString(as, instr, deoptStub)
	case js.OpTypeof:
		emitAMD64Typeof(as, instr, deoptStub)

	// --- Mod/Inc/Dec ops ---
	case js.OpMod:
		emitAMD64ModFast(as, instr, deoptStub)
	case js.OpInc:
		emitAMD64Inc(as, instr, deoptStub)
	case js.OpDec:
		emitAMD64Dec(as, instr, deoptStub)

	// --- Control flow ---
	case js.OpJumpIfToBooleanTrue:
		emitAMD64JumpIfToBooleanTrue(as, instr, labels, deoptStub)
	case js.OpJumpIfToBooleanFalse:
		emitAMD64JumpIfToBooleanFalse(as, instr, labels, deoptStub)
	case js.OpJumpIfNotNullish:
		emitAMD64JumpIfNotNullish(as, instr, labels, pc, deoptStub)

	// --- Property ops (deopt for now) ---
	case js.OpLdaNamedProperty:
		emitAMD64LdaNamedProperty(as, instr, bf, icSlotOffsets, deoptStub)
	case js.OpStaNamedProperty:
		emitAMD64StaNamedProperty(as, instr, icSlotOffsets, deoptStub)
	case js.OpLdaKeyedProperty:
		emitAMD64LdaKeyedProperty(as, instr, deoptStub)
	case js.OpStaKeyedProperty:
		emitAMD64StaKeyedProperty(as, instr, deoptStub)

	// --- Object/Array creation (deopt) ---
	case js.OpCreateObject:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCreateArray:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCreateObjectLiteral:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCreateClosure:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Call variants ---
	case js.OpCall0:
		emitAMD64CallN(as, instr, 0, deoptStub)
	case js.OpCall1:
		emitAMD64CallN(as, instr, 1, deoptStub)
	case js.OpCall2:
		emitAMD64CallN(as, instr, 2, deoptStub)
	case js.OpCallSpread:
		emitAMD64CallSpread(as, instr, deoptStub)

	// --- Throw/Try/ForIn (deopt) ---
	case js.OpThrow:
		emitAMD64Throw(as, deoptStub)
	case js.OpSetTryHandler:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpClearTryHandler:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpSetFinallyHandler:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpForInSetup:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpForInNext:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Global slot ops ---
	case js.OpLdaGlobalSlot:
		emitAMD64LdaGlobalSlot(as, instr)
	case js.OpStaGlobalSlot:
		emitAMD64StaGlobalSlot(as, instr)

	// --- Delete ops ---
	case js.OpDelete:
		emitAMD64DeleteProperty(as, instr, deoptStub)
	case js.OpDeleteKeyed:
		emitAMD64DeleteKeyed(as, instr, deoptStub)

	// --- Operator ops ---
	case js.OpInstanceof:
		emitAMD64Instanceof(as, instr, deoptStub)
	case js.OpIn:
		emitAMD64In(as, instr, deoptStub)

	// --- Global variable ops ---
	case js.OpLdaGlobal:
		emitAMD64LdaGlobal(as, instr, deoptStub)
	case js.OpStaGlobal:
		emitAMD64StaGlobal(as, instr, deoptStub)

	// --- Fast arithmetic variants (no type guards — compiler proven) ---
	case js.OpAddNumber:
		emitAMD64AddNumber(as, instr)
	case js.OpSubNumber:
		emitAMD64SubNumber(as, instr)
	case js.OpMulNumber:
		emitAMD64MulNumber(as, instr)
	case js.OpDivNumber:
		emitAMD64DivNumber(as, instr)
	case js.OpModNumber:
		emitAMD64ModNumber(as, instr)
	case js.OpNegateNumber:
		emitAMD64NegateNumber(as)
	case js.OpIncNumber:
		emitAMD64IncNumber(as, instr)
	case js.OpDecNumber:
		emitAMD64DecNumber(as, instr)
	case js.OpStrictEqNumber:
		emitAMD64StrictEqNumber(as, instr)
	case js.OpStrictNotEqNumber:
		emitAMD64StrictNotEqNumber(as, instr)
	case js.OpCmpNumber:
		emitAMD64CmpNumber(as, instr)
	case js.OpLessThanNumber:
		emitAMD64LessThanNumber(as, instr)
	case js.OpGreaterThanNumber:
		emitAMD64GreaterThanNumber(as, instr)
	case js.OpLessEqNumber:
		emitAMD64LessEqNumber(as, instr)
	case js.OpGreaterEqNumber:
		emitAMD64GreaterEqNumber(as, instr)

	// --- Bitwise fast variants ---
	case js.OpBitAndNumber:
		emitAMD64BitAndNumber(as, instr)
	case js.OpBitOrNumber:
		emitAMD64BitOrNumber(as, instr)
	case js.OpBitXorNumber:
		emitAMD64BitXorNumber(as, instr)
	case js.OpBitNotNumber:
		emitAMD64BitNotNumber(as, instr)
	case js.OpShiftLeftNumber:
		emitAMD64ShiftLeftNumber(as, instr)
	case js.OpShiftRightNumber:
		emitAMD64ShiftRightNumber(as, instr)
	case js.OpShiftRightZeroNumber:
		emitAMD64ShiftRightZeroNumber(as, instr)

	// --- Type conversion fast variants ---
	case js.OpToBooleanNumber:
		emitAMD64ToBooleanNumber(as, instr)
	case js.OpToStringNumber:
		emitAMD64ToStringNumber(as, instr)

	// --- Call fast variants ---
	case js.OpCallBuiltin:
		emitAMD64CallBuiltin(as, instr, deoptStub)
	case js.OpCallDirect:
		emitAMD64CallDirect(as, instr, deoptStub)
	case js.OpSuperCall:
		emitAMD64SuperCall(as, instr, deoptStub)
	case js.OpNew:
		emitAMD64New(as, instr, deoptStub)

	// --- Object/array/class fast paths ---
	case js.OpCreateEmptyArray:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCreateRegExp:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpDefineClass:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpSetPrototype:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpArrayGetIndex:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpArraySetIndex:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpArrayLength:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCopyDataProperties:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpGetSuperConstructor:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCheckConstructor:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpInitDerived:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCheckThisReinit:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Global/local fast variants ---
	case js.OpLdaGlobalDirect:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStaGlobalDirect:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpIncGlobalSlot:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpDecGlobalSlot:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpIncNamedProperty:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpDecNamedProperty:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpIncKeyedProperty:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpDecKeyedProperty:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Control flow / context ---
	case js.OpCatch:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpEndTry:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpPushContext:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpPopContext:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLoadContextSlot:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStoreContextSlot:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpSwap:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Exception / throw ---
	case js.OpThrowConstAssignment:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpThrowIfNotSuper:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpThrowIfHole:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpThrowSuperAlreadyCalled:
		emitAMD64ThrowSuperAlreadyCalled(as, deoptStub)
	case js.OpThrowSuperNotCalled:
		emitAMD64ThrowSuperNotCalled(as, deoptStub)
	case js.OpThrowReferenceError:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpThrowTypeError:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- String fast paths ---
	case js.OpStringConcat:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStringEq:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStringLength:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- For-in / for-of / iterator ---
	case js.OpForInSetupFast:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpForInNextFast:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpForOfSetup:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpForOfNext:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpGetIterator:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpIteratorNext:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpIteratorClose:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Property fast paths ---
	case js.OpLdaPropByOffset:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStaPropByOffset:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLdaHomeObject:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLdaHomeObjectProperty:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStaHomeObjectProperty:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStaByOffset:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Other ---
	case js.OpDebugger:
		as.AMD64_NOP() // debugger statement is a no-op
	case js.OpExp:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLdaCaptured:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLdaLocal:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStaLocal:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLdaThis:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLdaFalseFast:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpLdaTrueFast:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpMathAbs:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpMathCeil:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpMathFloor:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpMathSqrt:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpNullishCoalesce:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpOptionalChain:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpToObject:
		as.AMD64_JMP(deoptStub) // TODO: native
	// --- Module / private ---
	case js.OpLdaModuleVar:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpStaModuleVar:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpPrivateGet:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpPrivateSet:
		as.AMD64_JMP(deoptStub) // TODO: native

	// --- Generator / async ---
	case js.OpCreateGenerator:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCreateAsyncGenerator:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpCreateGeneratorObject:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpSuspendGenerator:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpResumeGenerator:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpGeneratorRestore:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpYield:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpYieldDelegate:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpAwait:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpAsyncAwait:
		as.AMD64_JMP(deoptStub) // TODO: native
	case js.OpAsyncReturn:
		as.AMD64_JMP(deoptStub) // TODO: native
	}
}

// --- emitAMD64LdaSmi: store small integer to Acc ---

func emitAMD64LdaSmi(as *Assembler, instr *js.Instruction) {
	val := int8(instr.OperandA)
	cookie := as.BlindingCookie

	// Acc.Tag = TagNumber (0x0300) — tags are not blinded (predictable low bytes).
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accOffset+tagWordOff))

	// Acc.NumVal = float64(val), blinded with cookie.
	// Emit: MOV RCX, blinded_val; MOV RDX, cookie; XOR RCX, RDX; store RCX.
	fbits := math.Float64bits(float64(val))
	blinded := fbits ^ cookie
	as.AMD64_MOV_RI(REG_RCX, blinded)
	as.AMD64_MOV_RI(REG_RDX, cookie)
	as.AMD64_XOR_RR(REG_RCX, REG_RDX) // RCX = unblinded float64 bits
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
	// NOTE: Constant blinding for LdaConstant requires coordinated blinding
	// at the GC bridge/interpreter level when values are stored into Constants[].
	// The per-compilation cookie is available on as.BlindingCookie for future use.
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
	// Bytecode: Regs[OperandA] (LHS) OP Acc (RHS).
	// For commutative ops (Add, Mul), order doesn't matter.
	// For non-commutative ops (Sub, Div), X0 must hold LHS.
	switch op {
	case 0: // Add (commutative)
		as.AMD64_MOVQ_XR(REG_X0, REG_R9)  // X0 = Acc.NumVal
		as.AMD64_MOVQ_XR(REG_X1, REG_R11) // X1 = Regs[lhs].NumVal
		as.AMD64_ADDSD(REG_X0, REG_X1)    // X0 += X1
	case 1: // Sub: LHS - RHS
		as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = Regs[lhs].NumVal (LHS)
		as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = Acc.NumVal (RHS)
		as.AMD64_SUBSD(REG_X0, REG_X1)    // X0 -= X1  →  X0 = LHS - RHS ✓
	case 2: // Mul (commutative)
		as.AMD64_MOVQ_XR(REG_X0, REG_R9)  // X0 = Acc.NumVal
		as.AMD64_MOVQ_XR(REG_X1, REG_R11) // X1 = Regs[lhs].NumVal
		as.AMD64_MULSD(REG_X0, REG_X1)    // X0 *= X1
	case 3: // Div: LHS / RHS
		as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = Regs[lhs].NumVal (LHS)
		as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = Acc.NumVal (RHS)
		as.AMD64_DIVSD(REG_X0, REG_X1)    // X0 /= X1  →  X0 = LHS / RHS ✓
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

// --- emitAMD64CompareFast: generic comparison fast path ---
//
// Loads Acc and Lhs (Regs[OperandA]), guards both are TagNumber,
// COMISD compare, then conditional branch to set boolean result.
//
// cond: 0=EQ, 1=LT, 2=GT, 3=LE, 4=GE
func emitAMD64CompareFast(as *Assembler, instr *js.Instruction, cond int, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Load Acc tag word and NumVal.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))

	// Load Lhs tag word and NumVal.
	as.AMD64_MOV_LOAD(REG_R10, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	// Guard: both must be TagNumber (0x0300).
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)
	as.AMD64_CMP_RR(REG_R10, REG_R13)
	as.AMD64_JNE(slowPath)

	// Move float64 bits to XMM and compare.
	// Bytecode: Regs[OperandA] (LHS) OP Acc (RHS).
	// X0 = LHS, X1 = RHS. COMISD(X0, X1) compares LHS with RHS.
	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = Lhs.NumVal (LHS)
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = Acc.NumVal (RHS)
	as.AMD64_COMISD(REG_X0, REG_X1)   // compare LHS, RHS

	// Use condition to set boolean result in R9 (1 or 0).
	isTrue := NewLabel()
	isFalse := NewLabel()
	setResult := NewLabel()
	switch cond {
	case 0: // EQ: LHS === RHS
		as.AMD64_JP(isFalse)   // NaN check: unordered → false
		as.AMD64_JE(isTrue)
	case 1: // LT: LHS < RHS
		as.AMD64_JP(isFalse)   // NaN check: unordered → false
		as.AMD64_JB(isTrue)
	case 2: // GT: LHS > RHS
		as.AMD64_JA(isTrue)    // JA: CF=0 AND ZF=0; NaN (CF=1) correctly falls through
	case 3: // LE: LHS <= RHS
		as.AMD64_JP(isFalse)   // NaN check: unordered → false
		as.AMD64_JBE(isTrue)
	case 4: // GE: LHS >= RHS
		as.AMD64_JAE(isTrue)   // JAE: CF=0; NaN (CF=1) correctly falls through
	}
	// False: R9 = 0.
	as.AMD64_Bind(isFalse)
	as.AMD64_XOR_RR(REG_R9, REG_R9)
	as.AMD64_JMP(setResult)

	as.AMD64_Bind(isTrue)
	as.AMD64_MOV_RI(REG_R9, 1)

	// Build boolean tag word and store.
	as.AMD64_Bind(setResult)
	as.AMD64_MOV_RI(REG_R8, 0x0200)
	as.AMD64_ADD_RR(REG_R8, REG_R9)                                       // R8 = TagBoolean | BoolVal
	as.AMD64_MOV_STORE(REG_R8, REG_R12, int8(accTagAlign))                // Acc tag
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))        // Acc.NumVal
	// Zero ObjVal and StrVal for clean state.
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	// Slow path: deoptimize.
	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)

	as.AMD64_Bind(done)
}

func emitAMD64StrictEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitAMD64CompareFast(as, instr, 0, deoptStub)
}

func emitAMD64LessThan(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitAMD64CompareFast(as, instr, 1, deoptStub)
}

func emitAMD64GreaterThan(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitAMD64CompareFast(as, instr, 2, deoptStub)
}

func emitAMD64LessEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitAMD64CompareFast(as, instr, 3, deoptStub)
}

func emitAMD64GreaterEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitAMD64CompareFast(as, instr, 4, deoptStub)
}

// --- emitAMD64Eq: loose equality fast path ---
// Fast path: both TagNumber → COMISD EQ check. Slow path deopts.
func emitAMD64Eq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitAMD64CompareFast(as, instr, 0, deoptStub)
}

// --- emitAMD64NotEq: loose inequality fast path ---
func emitAMD64NotEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R10, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)
	as.AMD64_CMP_RR(REG_R10, REG_R13)
	as.AMD64_JNE(slowPath)

	// Bytecode: LHS != RHS. Ordered: X0 = LHS, X1 = RHS.
	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = Lhs.NumVal (LHS)
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = Acc.NumVal (RHS)
	as.AMD64_COMISD(REG_X0, REG_X1)

	isTrue := NewLabel()
	setResult := NewLabel()
	as.AMD64_JP(isTrue)      // NaN check: NaN != NaN → true
	as.AMD64_JNE(isTrue)     // not equal → true
	// False
	as.AMD64_XOR_RR(REG_R9, REG_R9)
	as.AMD64_JMP(setResult)

	as.AMD64_Bind(isTrue)
	as.AMD64_MOV_RI(REG_R9, 1)

	as.AMD64_Bind(setResult)
	as.AMD64_MOV_RI(REG_R8, 0x0200)
	as.AMD64_ADD_RR(REG_R8, REG_R9)
	as.AMD64_MOV_STORE(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64StrictNotEq: strict inequality fast path ---
func emitAMD64StrictNotEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R10, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	// Check tag equality first: if tags differ, result is true.
	as.AMD64_CMP_RR(REG_R8, REG_R10)
	isTrue := NewLabel()
	setResult := NewLabel()
	as.AMD64_JNE(isTrue)

	// Same tag: guard TagNumber and compare values.
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Bytecode: LHS !== RHS. Ordered: X0 = LHS, X1 = RHS.
	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = Lhs.NumVal (LHS)
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = Acc.NumVal (RHS)
	as.AMD64_COMISD(REG_X0, REG_X1)
	as.AMD64_JP(isTrue)      // NaN check: NaN !== NaN → true
	as.AMD64_JNE(isTrue)     // not equal → true

	// False
	as.AMD64_XOR_RR(REG_R9, REG_R9)
	as.AMD64_JMP(setResult)

	as.AMD64_Bind(isTrue)
	as.AMD64_MOV_RI(REG_R9, 1)

	as.AMD64_Bind(setResult)
	as.AMD64_MOV_RI(REG_R8, 0x0200)
	as.AMD64_ADD_RR(REG_R8, REG_R9)
	as.AMD64_MOV_STORE(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64BitwiseFast: generic bitwise binary op ---
// op: 0=And, 1=Or, 2=Xor
func emitAMD64BitwiseFast(as *Assembler, instr *js.Instruction, op int, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R10, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)
	as.AMD64_CMP_RR(REG_R10, REG_R13)
	as.AMD64_JNE(slowPath)

	// Convert float64 → int64 via CVTTSD2SI.
	// Operands are commutative for bitwise; no order swap needed.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)  // X0 = Acc.NumVal
	as.AMD64_MOVQ_XR(REG_X1, REG_R11) // X1 = Lhs.NumVal
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0)
	// Check for CVTTSD2SI overflow (INT64_MIN sentinel for values >= 2^63).
	as.AMD64_MOV_RI(REG_R8, 0x8000000000000000)
	as.AMD64_CMP_RR(REG_R9, REG_R8)
	as.AMD64_JE(slowPath)
	as.AMD64_CVTTSD2SI(REG_R11, REG_X1)
	as.AMD64_CMP_RR(REG_R11, REG_R8)
	as.AMD64_JE(slowPath)

	// Mask to 32 bits (JS ToInt32/ToUint32 semantics).
	as.AMD64_MOV_RI(REG_RDX, 0xFFFFFFFF)
	as.AMD64_AND_RR(REG_R9, REG_RDX)
	as.AMD64_AND_RR(REG_R11, REG_RDX)

	// Bitwise operation.
	switch op {
	case 0:
		as.AMD64_AND_RR(REG_R9, REG_R11)
	case 1:
		as.AMD64_OR_RR(REG_R9, REG_R11)
	case 2:
		as.AMD64_XOR_RR(REG_R9, REG_R11)
	}

	// Convert int64 → float64.
	as.AMD64_CVTSI2SD(REG_X2, REG_R9)  // X2 = float64(R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X2)  // R9 = float64 result

	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	// Zero ObjVal and StrVal.
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64BitwiseNot: ~acc fast path ---
func emitAMD64BitwiseNot(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Convert float64 → int64 → NOT → float64.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0)
	// Check for CVTTSD2SI overflow (INT64_MIN sentinel for values >= 2^63).
	as.AMD64_MOV_RI(REG_R8, 0x8000000000000000)
	as.AMD64_CMP_RR(REG_R9, REG_R8)
	as.AMD64_JE(slowPath)

	// Mask to 32 bits (JS ToInt32 semantics).
	as.AMD64_MOV_RI(REG_R11, 0xFFFFFFFF)
	as.AMD64_AND_RR(REG_R9, REG_R11)

	as.AMD64_NOT_R(REG_R9)
	as.AMD64_CVTSI2SD(REG_X1, REG_R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X1)

	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64ShiftFast: shift ops ---
// op: 0=ShiftLeft, 1=ShiftRight, 2=ShiftRightZero
func emitAMD64ShiftFast(as *Assembler, instr *js.Instruction, op int, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R10, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)
	as.AMD64_CMP_RR(REG_R10, REG_R13)
	as.AMD64_JNE(slowPath)

	// Convert float64 → int64.
	// Bytecode: Regs[OperandA] (LHS) << Acc (RHS count).
	// LHS = value to shift, RHS = shift count.
	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = Regs[lhs].NumVal (LHS = value)
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = Acc.NumVal (RHS = shift count)
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0) // R9 = int64(LHS value)
	// Check for CVTTSD2SI overflow (INT64_MIN sentinel for values >= 2^63).
	as.AMD64_MOV_RI(REG_R8, 0x8000000000000000)
	as.AMD64_CMP_RR(REG_R9, REG_R8)
	as.AMD64_JE(slowPath)
	as.AMD64_CVTTSD2SI(REG_R11, REG_X1) // R11 = int64(RHS shift count)
	as.AMD64_CMP_RR(REG_R11, REG_R8)
	as.AMD64_JE(slowPath)

	// Mask value to 32 bits (JS ToInt32 semantics).
	as.AMD64_MOV_RI(REG_RDX, 0xFFFFFFFF)
	as.AMD64_AND_RR(REG_R9, REG_RDX)

	// Mask shift count to 5 bits (JS semantics: ToUint32(rhs) & 0x1F).
	as.AMD64_MOV_RI(REG_RCX, 0x1F)
	as.AMD64_AND_RR(REG_R11, REG_RCX)
	// Move shift count to CL register.
	as.AMD64_MOV_RR(REG_RCX, REG_R11)

	// Shift operation using CL.
	switch op {
	case 0:
		as.AMD64_SHL_CL(REG_R9)
	case 1:
		as.AMD64_SAR_CL(REG_R9)
	case 2:
		as.AMD64_SHR_CL(REG_R9)
	}

	// Convert int64 → float64.
	as.AMD64_CVTSI2SD(REG_X2, REG_R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X2)

	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64LogicalNot: !acc fast path ---
func emitAMD64LogicalNot(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	isFalsy := NewLabel()
	storeBool := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined (0) → falsy → result = true.
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(isFalsy)

	// Null (0x0100) → falsy → result = true.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(isFalsy)

	// Boolean: (tag_word & 0xFF00) == 0x0200.
	as.AMD64_MOV_RR(REG_R10, REG_R8)
	as.AMD64_MOV_RI(REG_R9, 0xFF00)
	as.AMD64_AND_RR(REG_R10, REG_R9)
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R10, REG_R9)
	as.AMD64_JNE(slowPath) // not boolean → slow path

	// Boolean: invert BoolVal (low byte).
	as.AMD64_MOV_RI(REG_R9, 1)
	as.AMD64_AND_RR(REG_R8, REG_R9) // R8 = BoolVal (0 or 1)
	as.AMD64_XOR_RR(REG_R8, REG_R9) // R8 = !BoolVal
	as.AMD64_MOV_RR(REG_R9, REG_R8) // R9 = result
	as.AMD64_JMP(storeBool)

	// Falsy → result = true.
	as.AMD64_Bind(isFalsy)
	as.AMD64_MOV_RI(REG_R9, 1)

	// Store boolean result.
	as.AMD64_Bind(storeBool)
	as.AMD64_MOV_RI(REG_R8, 0x0200)
	as.AMD64_ADD_RR(REG_R8, REG_R9)                                        // tag = 0x0200 | boolVal
	as.AMD64_MOV_STORE(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64LogicalAnd: acc && Regs[OperandA] → acc ---
// Short-circuit: if acc is falsy, keep acc; otherwise load rhs.
func emitAMD64LogicalAnd(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	rhs := int(instr.OperandA)
	rhsSlot := rhs * jsValueSize
	slowPath := NewLabel()
	loadRhs := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined → falsy → keep acc.
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(done)

	// Null → falsy → keep acc.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(done)

	// Boolean false (0x0200) → falsy → keep acc.
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(done)

	// Number: check if 0.0 or NaN.
	as.AMD64_MOV_RR(REG_R10, REG_R8)
	as.AMD64_MOV_RI(REG_R9, 0xFF00)
	as.AMD64_AND_RR(REG_R10, REG_R9)
	as.AMD64_MOV_RI(REG_R9, 0x0300)
	as.AMD64_CMP_RR(REG_R10, REG_R9)
	as.AMD64_JNE(checkBool) // not number → check if boolean

	// Check if NumVal == 0.0 or NaN.
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_XORPD(REG_X1, REG_X1)
	as.AMD64_COMISD(REG_X0, REG_X1)
	as.AMD64_JE(done)  // 0.0 → falsy → keep acc
	as.AMD64_JP(done)  // NaN → falsy → keep acc
	// Non-zero number → truthy → load rhs.
	as.AMD64_JMP(loadRhs)

	// Check if boolean (TagBoolean = 0x0200). Boolean false already handled above.
	as.AMD64_Bind(checkBool)
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R10, REG_R9) // R10 = masked tag
	as.AMD64_JE(loadRhs)              // boolean true → truthy → load rhs
	// String, Object, or other type → deopt for correct falsiness evaluation.
	as.AMD64_JMP(deoptStub)

	// Load rhs.
	as.AMD64_Bind(loadRhs)
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(rhsSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64LogicalOr: acc || Regs[OperandA] → acc ---
// Short-circuit: if acc is truthy, keep acc; otherwise load rhs.
func emitAMD64LogicalOr(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	rhs := int(instr.OperandA)
	rhsSlot := rhs * jsValueSize
	slowPath := NewLabel()
	loadRhs := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined → falsy → load rhs.
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(loadRhs)

	// Null → falsy → load rhs.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(loadRhs)

	// Boolean false (0x0200) → falsy → load rhs.
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(loadRhs)

	// Boolean true → truthy → keep acc.
	as.AMD64_MOV_RI(REG_R9, 0x0201)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(done)

	// Number: check if 0.0 or NaN → load rhs; otherwise keep.
	as.AMD64_MOV_RR(REG_R10, REG_R8)
	as.AMD64_MOV_RI(REG_R9, 0xFF00)
	as.AMD64_AND_RR(REG_R10, REG_R9)
	as.AMD64_MOV_RI(REG_R9, 0x0300)
	as.AMD64_CMP_RR(REG_R10, REG_R9)
	as.AMD64_JNE(slowPath) // not number → deopt (strings, objects need runtime falsiness)

	// Check if NumVal == 0.0 or NaN.
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_XORPD(REG_X1, REG_X1)
	as.AMD64_COMISD(REG_X0, REG_X1)
	as.AMD64_JE(loadRhs)  // 0.0 → load rhs
	as.AMD64_JP(loadRhs)  // NaN → load rhs
	// Non-zero number → truthy → keep acc.
	as.AMD64_JMP(done)

	// Load rhs.
	as.AMD64_Bind(loadRhs)
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(rhsSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}
	as.AMD64_JMP(done)

	// slowPath: reached when value type needs runtime falsiness check (string, object, etc.)
	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)

	as.AMD64_Bind(done)
}

// --- emitAMD64ToBoolean: ToBoolean(acc) → acc ---
func emitAMD64ToBoolean(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	slowPath := NewLabel()
	done := NewLabel()
	isFalse := NewLabel()
	setResult := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Compare NumVal against 0.0.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_XORPD(REG_X1, REG_X1)
	as.AMD64_COMISD(REG_X0, REG_X1)

	// NaN (PF=1) → false.
	as.AMD64_JP(isFalse)
	// +0.0 or -0.0 (ZF=1) → false.
	as.AMD64_JE(isFalse)

	// Truthy: R9 = 1.
	as.AMD64_MOV_RI(REG_R9, 1)
	as.AMD64_JMP(setResult)

	// Falsy: R9 = 0.
	as.AMD64_Bind(isFalse)
	as.AMD64_XOR_RR(REG_R9, REG_R9)

	// Build boolean tag word and store.
	as.AMD64_Bind(setResult)
	as.AMD64_MOV_RI(REG_R8, 0x0200)
	as.AMD64_ADD_RR(REG_R8, REG_R9)
	as.AMD64_MOV_STORE(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64ToNumber: ToNumber(acc) → acc ---
// Fast path: if already TagNumber, do nothing. Else deopt.
func emitAMD64ToNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_RI(REG_R9, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JNE(slowPath)
	as.AMD64_JMP(done) // already number

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64ToString: ToString(acc) → acc ---
// Fast path: if already TagString, do nothing. Else deopt.
func emitAMD64ToString(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_RI(REG_R9, uint64(js.TagString))
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JNE(slowPath)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64Typeof: typeof acc → acc ---
// Fast path for common simple types; deopt for objects (need callable check).
func emitAMD64Typeof(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Check tag and deopt for objects (require callable check).
	as.AMD64_MOV_RI(REG_R9, uint64(js.TagObject)) // TagObject tag word ≈ 0x0500
	// TagObject: tag byte at offset 57 of the tag word.
	// The tag word for Object is 0x0500. Let's check (R8 >> 8) & 0xFF == TagObject.
	// Actually tag_word = Tag | (BoolVal << 0), so tag is the upper byte.
	// TagObject = 5 → tag word = 0x0500.
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(slowPath) // object → slow path (needs callable check)

	// TagSymbol (0x0600) → slow path.
	as.AMD64_MOV_RI(REG_R9, uint64(js.TagSymbol)|0x0600)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(slowPath)

	// TagBigInt (0x0700) → slow path.
	as.AMD64_MOV_RI(REG_R9, uint64(js.TagBigInt)|0x0700)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(slowPath)

	// Simple types fall through to slow path for now.
	// TODO: implement inline typeof with constant pool strings.
	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64ModFast: acc % Regs[OperandA] → acc ---
func emitAMD64ModFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R10, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)
	as.AMD64_CMP_RR(REG_R10, REG_R13)
	as.AMD64_JNE(slowPath)

	// Convert to int64 for TruncMod-like behavior via FPREM.
	// Bytecode: Regs[OperandA] (LHS) % Acc (RHS).
	// X0 = LHS (dividend), X1 = RHS (divisor).
	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = Regs[lhs].NumVal (LHS)
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = Acc.NumVal (RHS)

	// Check divisor == 0 → result = NaN.
	as.AMD64_XORPD(REG_X2, REG_X2)
	as.AMD64_COMISD(REG_X1, REG_X2)   // check RHS (divisor) == 0
	zeroDiv := NewLabel()
	as.AMD64_JE(zeroDiv)

	// Convert to int64 and do integer modulo.
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0)  // R9 = int64(LHS) = dividend
	// Check for CVTTSD2SI overflow (INT64_MIN sentinel for values >= 2^63).
	as.AMD64_MOV_RI(REG_R8, 0x8000000000000000)
	as.AMD64_CMP_RR(REG_R9, REG_R8)
	as.AMD64_JE(slowPath)
	as.AMD64_CVTTSD2SI(REG_R11, REG_X1) // R11 = int64(RHS) = divisor
	as.AMD64_CMP_RR(REG_R11, REG_R8)
	as.AMD64_JE(slowPath)

	// Sign-extend RAX for IDIV.
	as.AMD64_MOV_RR(REG_RAX, REG_R9) // RAX = dividend (LHS)
	// Guard against INT64_MIN / -1 which causes #DE hardware exception.
	// INT64_MIN % -1 == 0 in JS semantics.
	as.AMD64_MOV_RI(REG_RCX, math.MaxUint64) // RCX = -1 (all bits set)
	as.AMD64_CMP_RR(REG_R11, REG_RCX)        // divisor == -1?
	noOverflow := NewLabel()
	as.AMD64_JNE(noOverflow)
	as.AMD64_XOR_RR(REG_R9, REG_R9)           // result = 0
	as.AMD64_XOR_RR(REG_RDX, REG_RDX)         // remainder = 0
	idivDone := NewLabel()
	as.AMD64_JMP(idivDone)
	as.AMD64_Bind(noOverflow)
	// CQO: sign-extend RAX → RDX:RAX
	as.AMD64_CQO()
	// IDIV: RAX = RDX:RAX / divisor; RDX = remainder
	as.AMD64_IDIV_RR(REG_R11)        // RDX = LHS % RHS
	// Remainder in RDX.
	as.AMD64_MOV_RR(REG_R9, REG_RDX)
	as.AMD64_Bind(idivDone)

	as.AMD64_CVTSI2SD(REG_X3, REG_R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X3)

	storeResult := NewLabel()
	as.AMD64_JMP(storeResult)

	as.AMD64_Bind(zeroDiv)
	// NaN: all bits 1 for float64 NaN.
	as.AMD64_MOV_RI(REG_R9, uint64(math.Float64bits(math.NaN())))

	as.AMD64_Bind(storeResult)
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64Inc: Regs[OperandA] += 1 → Acc and Regs[OperandA] ---
func emitAMD64Inc(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(regSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(regSlot+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Add 1.0.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	fbits := math.Float64bits(1.0)
	as.AMD64_MOV_RI(REG_R11, fbits)
	as.AMD64_MOVQ_XR(REG_X1, REG_R11)
	as.AMD64_ADDSD(REG_X0, REG_X1) // X0 += 1.0
	as.AMD64_MOVQ_RX(REG_R9, REG_X0)

	// Store to Regs[reg] and Acc.
	as.AMD64_MOV_STORE(REG_R9, REG_R15, int8(regSlot+numValOff))
	as.AMD64_MOV_STORE(REG_R13, REG_R15, int8(regSlot+tagWordOff))
	// Copy full Regs[reg] → Acc.
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(regSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64Dec: Regs[OperandA] -= 1 → Acc and Regs[OperandA] ---
func emitAMD64Dec(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(regSlot+tagWordOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(regSlot+numValOff))

	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Subtract 1.0.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	fbits := math.Float64bits(1.0)
	as.AMD64_MOV_RI(REG_R11, fbits)
	as.AMD64_MOVQ_XR(REG_X1, REG_R11)
	as.AMD64_SUBSD(REG_X0, REG_X1) // X0 -= 1.0
	as.AMD64_MOVQ_RX(REG_R9, REG_X0)

	as.AMD64_MOV_STORE(REG_R9, REG_R15, int8(regSlot+numValOff))
	as.AMD64_MOV_STORE(REG_R13, REG_R15, int8(regSlot+tagWordOff))
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(regSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}

	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64JumpIfToBooleanTrue: jump if ToBoolean(acc) is true ---
func emitAMD64JumpIfToBooleanTrue(as *Assembler, instr *js.Instruction, labels map[int]*Label, deoptStub *Label) {
	target := int(instr.OperandA)
	l := labels[target]
	slowPath := NewLabel()
	nextPC := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined (0) → falsy → don't jump.
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(nextPC)

	// Null → falsy → don't jump.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(nextPC)

	// Boolean: check false.
	as.AMD64_MOV_RR(REG_R10, REG_R8)
	as.AMD64_MOV_RI(REG_R9, 0xFF00)
	as.AMD64_AND_RR(REG_R10, REG_R9)
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R10, REG_R9)
	as.AMD64_JNE(slowPath)

	// Boolean false (0x0200) → don't jump.
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(nextPC)

	// Boolean true → jump.
	as.AMD64_JMP(l)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(nextPC)
}

// --- emitAMD64JumpIfToBooleanFalse: jump if ToBoolean(acc) is false ---
func emitAMD64JumpIfToBooleanFalse(as *Assembler, instr *js.Instruction, labels map[int]*Label, deoptStub *Label) {
	target := int(instr.OperandA)
	l := labels[target]
	slowPath := NewLabel()
	nextPC := NewLabel()

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined → falsy → jump.
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(l)

	// Null → falsy → jump.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(l)

	// Boolean: check.
	as.AMD64_MOV_RR(REG_R10, REG_R8)
	as.AMD64_MOV_RI(REG_R9, 0xFF00)
	as.AMD64_AND_RR(REG_R10, REG_R9)
	as.AMD64_MOV_RI(REG_R9, 0x0200)
	as.AMD64_CMP_RR(REG_R10, REG_R9)
	as.AMD64_JNE(slowPath)

	// Boolean: if BoolVal == 0 → jump (falsy).
	as.AMD64_MOV_RI(REG_R9, 1)
	as.AMD64_TEST_RR(REG_R8, REG_R9)
	as.AMD64_JZ(l)

	// Boolean true → fall through.
	as.AMD64_JMP(nextPC)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(nextPC)
}

// --- emitAMD64JumpIfNotNullish: jump if acc is not null/undefined ---
func emitAMD64JumpIfNotNullish(as *Assembler, instr *js.Instruction, labels map[int]*Label, pc int, deoptStub *Label) {
	_ = deoptStub
	target := int(instr.OperandA)
	l := labels[target]
	nextPC := labels[pc+1] // fall through to next instruction

	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))

	// Undefined (0) → nullish → skip jump.
	as.AMD64_TEST_RR(REG_R8, REG_R8)
	as.AMD64_JZ(nextPC)

	// Null (0x0100) → nullish → skip jump.
	as.AMD64_MOV_RI(REG_R9, 0x0100)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(nextPC)

	// Not nullish → jump.
	as.AMD64_JMP(l)
}

// --- emitAMD64LdaNamedProperty: load obj[propName] → Acc (IC-backed) ---
//
// Fast path: guard obj is object, then call Go IC helper which uses FeedbackVector.
// On miss (non-object or nil obj), deoptimize to interpreter.
func emitAMD64LdaNamedProperty(as *Assembler, instr *js.Instruction, bf *js.BytecodeFunction, icSlotOffsets []int, deoptStub *Label) {
	propIdx := int(instr.OperandA)
	_ = propIdx
	slotIdx := int(instr.OperandC)
	_ = bf
	_ = icSlotOffsets

	slowPath := NewLabel()
	done := NewLabel()

	// Guard: Acc must be an object.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_RI(REG_R13, uint64(js.TagObject)|0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: Acc.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpLdaNamedPropertySlow(frame, propIdx, slotIdx).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(propIdx))
	as.AMD64_MOV_RI(REG_RCX, uint64(slotIdx))
	addr := funcToAddr(sparkplugOpLdaNamedPropertySlow)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64StaNamedProperty: store val → obj[propName] (IC-backed) ---
//
// Fast path: guard obj is object, then call Go IC helper.
func emitAMD64StaNamedProperty(as *Assembler, instr *js.Instruction, icSlotOffsets []int, deoptStub *Label) {
	propIdx := int(instr.OperandA)
	_ = propIdx
	slotIdx := int(instr.OperandC)
	valReg := int(instr.OperandB)
	_ = icSlotOffsets

	slowPath := NewLabel()
	done := NewLabel()

	// Guard: Acc must be an object.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_RI(REG_R13, uint64(js.TagObject)|0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: Acc.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpStaNamedPropertySlow(frame, propIdx, slotIdx, valReg).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(propIdx))
	as.AMD64_MOV_RI(REG_RCX, uint64(slotIdx))
	as.AMD64_MOV_RI(REG_RDI, uint64(valReg))
	addr := funcToAddr(sparkplugOpStaNamedPropertySlow)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64LdaKeyedProperty: load obj[key] → Acc ---
//
// Fast path: guard obj is object, then call Go helper for full property lookup.
func emitAMD64LdaKeyedProperty(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	objSlot := objReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Guard: obj must be an object (tag 0x0500).
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(objSlot+tagWordOff))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: obj.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(objSlot+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpLdaKeyedPropertySlow(frame, objReg).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(objReg))
	addr := funcToAddr(sparkplugOpLdaKeyedPropertySlow)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64StaKeyedProperty: store val → obj[key] ---
//
// Fast path: guard obj is object, then call Go helper for full property store.
func emitAMD64StaKeyedProperty(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	objSlot := objReg * jsValueSize
	valReg := int(instr.OperandB)

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Guard: obj must be an object (tag 0x0500).
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(objSlot+tagWordOff))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: obj.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(objSlot+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpStaKeyedPropertySlow(frame, objReg, valReg).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(objReg))
	as.AMD64_MOV_RI(REG_RCX, uint64(valReg))
	addr := funcToAddr(sparkplugOpStaKeyedPropertySlow)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64Throw: set frame.Thrown and trigger unwind ---
func emitAMD64Throw(as *Assembler, deoptStub *Label) {
	// Copy Acc → frame.Thrown (64 bytes), then deopt.
	thrownOff := int(unsafe.Offsetof(js.VMFrame{}.Thrown))
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R12, int8(accOffset+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(thrownOff+i))
	}
	as.AMD64_JMP(deoptStub)
}

// --- emitAMD64LdaGlobalSlot: load GlobalVals[slotIdx] → Acc ---
//
// Copies 64 bytes from Func.GlobalVals[slotIdx] into frame.Acc.
func emitAMD64LdaGlobalSlot(as *Assembler, instr *js.Instruction) {
	slotIdx := int(instr.OperandA)
	slotOff := slotIdx * jsValueSize

	// Load frame.Func pointer.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(funcOff))
	// Load Func.GlobalVals slice data pointer (offset 0 of slice header = data ptr).
	as.AMD64_MOV_LOAD(REG_R9, REG_R8, int8(globalValsDataOff))

	// Copy GlobalVals[slotIdx] → Acc: 8 × 8-byte MOV (64 bytes).
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R9, int8(slotOff+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}
}

// --- emitAMD64StaGlobalSlot: store Acc → GlobalVals[slotIdx] ---
//
// Copies 64 bytes from frame.Acc into Func.GlobalVals[slotIdx].
func emitAMD64StaGlobalSlot(as *Assembler, instr *js.Instruction) {
	slotIdx := int(instr.OperandA)
	slotOff := slotIdx * jsValueSize

	// Load frame.Func pointer.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(funcOff))
	// Load Func.GlobalVals slice data pointer.
	as.AMD64_MOV_LOAD(REG_R9, REG_R8, int8(globalValsDataOff))

	// Copy Acc → GlobalVals[slotIdx]: 8 × 8-byte MOV (64 bytes).
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R12, int8(accOffset+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R9, int8(slotOff+i))
	}
}

// --- emitAMD64LdaGlobal: inline fast-path for OpLdaGlobal ---
//
// Fast path: if Constants[constIdx].Tag != TagString (already cached), copy
// Constants[constIdx] → Acc. If TagString (not cached), deopt.
func emitAMD64LdaGlobal(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	constIdx := int(instr.OperandA)
	constSlot := constIdx * jsValueSize

	// Load frame.Func pointer.
	as.AMD64_MOV_LOAD(REG_R10, REG_R12, int8(funcOff))
	// Load Func.Constants slice data pointer.
	as.AMD64_MOV_LOAD(REG_R11, REG_R10, int8(constsOff))

	// Check Constants[constIdx].Tag — if still TagString, deopt.
	as.AMD64_MOV_LOAD(REG_R8, REG_R11, int8(constSlot+tagWordOff))
	as.AMD64_MOV_RI(REG_R9, uint64(js.TagString)|0x0400)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(deoptStub)

	// Copy Constants[constIdx] → Acc: 8 × 8-byte MOV (64 bytes).
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R11, int8(constSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}
}

// --- emitAMD64StaGlobal: inline fast-path for OpStaGlobal ---
//
// Fast path: if Constants[constIdx].Tag != TagString (already cached), copy
// Acc → Constants[constIdx]. If TagString (not cached), deopt.
func emitAMD64StaGlobal(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	constIdx := int(instr.OperandA)
	constSlot := constIdx * jsValueSize

	// Load frame.Func pointer.
	as.AMD64_MOV_LOAD(REG_R10, REG_R12, int8(funcOff))
	// Load Func.Constants slice data pointer.
	as.AMD64_MOV_LOAD(REG_R11, REG_R10, int8(constsOff))

	// Check Constants[constIdx].Tag — if still TagString, deopt.
	as.AMD64_MOV_LOAD(REG_R8, REG_R11, int8(constSlot+tagWordOff))
	as.AMD64_MOV_RI(REG_R9, uint64(js.TagString)|0x0400)
	as.AMD64_CMP_RR(REG_R8, REG_R9)
	as.AMD64_JE(deoptStub)

	// Copy Acc → Constants[constIdx]: 8 × 8-byte MOV (64 bytes).
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R12, int8(accOffset+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R11, int8(constSlot+i))
	}
}

// --- emitAMD64DeleteProperty: delete obj[propIdx] ---
//
// Guards that acc is an object, then calls Go helper for shape-based deletion.
func emitAMD64DeleteProperty(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	propIdx := int(instr.OperandA)

	slowPath := NewLabel()
	done := NewLabel()

	// Guard: acc must be object.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: acc.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpDelete(frame, propIdx).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(propIdx))
	addr := funcToAddr(sparkplugOpDelete)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64DeleteKeyed: delete obj[key] ---
//
// Guards that acc is an object, then calls Go helper.
func emitAMD64DeleteKeyed(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	keyReg := int(instr.OperandB)

	slowPath := NewLabel()
	done := NewLabel()

	// Guard: acc must be object.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: acc.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpDeleteKeyed(frame, objReg, keyReg).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(objReg))
	as.AMD64_MOV_RI(REG_RCX, uint64(keyReg))
	addr := funcToAddr(sparkplugOpDeleteKeyed)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64Instanceof: lhs instanceof acc → Acc ---
//
// Guards both operands are objects, then calls Go helper for prototype chain walk.
func emitAMD64Instanceof(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhsReg := int(instr.OperandA)
	lhsSlot := lhsReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Guard: lhs must be object.
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(lhsSlot+tagWordOff))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: lhs.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(lhsSlot+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Guard: acc must be object.
	as.AMD64_MOV_LOAD(REG_R10, REG_R12, int8(accTagAlign))
	as.AMD64_CMP_RR(REG_R10, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: acc.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R11, REG_R12, int8(accOffset+accObjValOff))
	as.AMD64_TEST_RR(REG_R11, REG_R11)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpInstanceof(frame, lhsReg).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(lhsReg))
	addr := funcToAddr(sparkplugOpInstanceof)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- emitAMD64In: prop in acc → Acc ---
//
// Guards acc is an object, then calls Go helper.
func emitAMD64In(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	propReg := int(instr.OperandA)

	slowPath := NewLabel()
	done := NewLabel()

	// Guard: acc must be object.
	as.AMD64_MOV_LOAD(REG_R8, REG_R12, int8(accTagAlign))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: acc.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+accObjValOff))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper: sparkplugOpIn(frame, propReg).
	as.AMD64_MOV_RR(REG_RAX, REG_R12) // RAX = frame
	as.AMD64_MOV_RI(REG_RBX, uint64(propReg))
	addr := funcToAddr(sparkplugOpIn)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
	as.AMD64_JMP(done)

	as.AMD64_Bind(slowPath)
	as.AMD64_JMP(deoptStub)
	as.AMD64_Bind(done)
}

// --- Fast Number variant native emitters (no type guards — compiler proven) ---

// emitAMD64AddNumber emits native AMD64 for OpAddNumber (both TagNumber, no guard).
// Regs[OperandA].NumVal + Acc.NumVal → Acc.
func emitAMD64AddNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc.NumVal
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))     // Regs[lhs].NumVal

	as.AMD64_MOVQ_XR(REG_X0, REG_R9)  // X0 = Acc.NumVal
	as.AMD64_MOVQ_XR(REG_X1, REG_R11) // X1 = Regs[lhs].NumVal
	as.AMD64_ADDSD(REG_X0, REG_X1)    // X0 += X1

	as.AMD64_MOVQ_RX(REG_R9, REG_X0)
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff)) // Acc.NumVal = result
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign)) // tag = TagNumber
}

// emitAMD64SubNumber emits native AMD64 for OpSubNumber (both TagNumber, no guard).
// Regs[OperandA].NumVal - Acc.NumVal → Acc.
func emitAMD64SubNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc.NumVal (RHS)
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))     // Regs[lhs].NumVal (LHS)

	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = LHS
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = RHS (Acc)
	as.AMD64_SUBSD(REG_X0, REG_X1)    // X0 = LHS - RHS

	as.AMD64_MOVQ_RX(REG_R9, REG_X0)
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
}

// emitAMD64MulNumber emits native AMD64 for OpMulNumber (both TagNumber, no guard).
// Regs[OperandA].NumVal * Acc.NumVal → Acc.
func emitAMD64MulNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_MOVQ_XR(REG_X1, REG_R11)
	as.AMD64_MULSD(REG_X0, REG_X1) // X0 *= X1

	as.AMD64_MOVQ_RX(REG_R9, REG_X0)
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
}

// emitAMD64DivNumber emits native AMD64 for OpDivNumber (both TagNumber, no guard).
// Regs[OperandA].NumVal / Acc.NumVal → Acc.
func emitAMD64DivNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc (RHS)
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))     // LHS

	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = LHS
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = RHS (Acc)
	as.AMD64_DIVSD(REG_X0, REG_X1)    // X0 = LHS / RHS

	as.AMD64_MOVQ_RX(REG_R9, REG_X0)
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
}

// emitAMD64NegateNumber emits native AMD64 for OpNegateNumber (TagNumber, no guard).
// -Acc.NumVal → Acc (XOR sign bit).
func emitAMD64NegateNumber(as *Assembler) {
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff)) // Acc.NumVal

	// XOR sign bit: 0x8000000000000000
	as.AMD64_MOV_RI(REG_R11, 0x8000000000000000)
	as.AMD64_XOR_RR(REG_R9, REG_R11) // R9 = -Acc.NumVal (sign bit flipped)

	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
}

// emitAMD64IncNumber emits native AMD64 for OpIncNumber (TagNumber reg, no guard).
// Regs[OperandA].NumVal += 1.0 → Acc and Regs[OperandA].
func emitAMD64IncNumber(as *Assembler, instr *js.Instruction) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(regSlot+numValOff))

	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	fbits := math.Float64bits(1.0)
	as.AMD64_MOV_RI(REG_R11, fbits)
	as.AMD64_MOVQ_XR(REG_X1, REG_R11)
	as.AMD64_ADDSD(REG_X0, REG_X1) // X0 += 1.0
	as.AMD64_MOVQ_RX(REG_R9, REG_X0)

	// Store to Regs[reg] and Acc.
	as.AMD64_MOV_STORE(REG_R9, REG_R15, int8(regSlot+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R15, int8(regSlot+tagWordOff))
	// Copy full Regs[reg] → Acc.
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(regSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}
}

// emitAMD64DecNumber emits native AMD64 for OpDecNumber (TagNumber reg, no guard).
// Regs[OperandA].NumVal -= 1.0 → Acc and Regs[OperandA].
func emitAMD64DecNumber(as *Assembler, instr *js.Instruction) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(regSlot+numValOff))

	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	fbits := math.Float64bits(1.0)
	as.AMD64_MOV_RI(REG_R11, fbits)
	as.AMD64_MOVQ_XR(REG_X1, REG_R11)
	as.AMD64_SUBSD(REG_X0, REG_X1) // X0 -= 1.0
	as.AMD64_MOVQ_RX(REG_R9, REG_X0)

	as.AMD64_MOV_STORE(REG_R9, REG_R15, int8(regSlot+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R15, int8(regSlot+tagWordOff))
	for i := 0; i < 64; i += 8 {
		as.AMD64_MOV_LOAD(REG_RCX, REG_R15, int8(regSlot+i))
		as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+i))
	}
}

// emitAMD64CmpNumber emits native AMD64 for OpCmpNumber (both TagNumber, no guard).
// Compare Regs[OperandA].NumVal vs Acc.NumVal with condition OperandC → boolean Acc.
func emitAMD64CmpNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	cond := int(instr.OperandC)

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc.NumVal
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))     // Regs[lhs].NumVal

	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = LHS
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = RHS (Acc)
	as.AMD64_COMISD(REG_X0, REG_X1)   // set EFLAGS from X0 vs X1

	// Set boolean result based on condition (same order as VM cond constants):
	// 0=LT, 1=LE, 2=EQ, 3=NE, 4=GT, 5=GE
	// TrueTag = 0x0201 (TagBoolean true), FalseTag = 0x0200 (TagBoolean false)
	truePath := NewLabel()
	donePath := NewLabel()

	switch cond {
	case 0: // LT: LHS < RHS
		as.AMD64_JL(truePath)
	case 1: // LE: LHS <= RHS
		as.AMD64_JLE(truePath)
	case 2: // EQ: LHS == RHS
		as.AMD64_JE(truePath)
	case 3: // NE: LHS != RHS
		as.AMD64_JNE(truePath)
	case 4: // GT: LHS > RHS
		as.AMD64_JG(truePath)
	case 5: // GE: LHS >= RHS
		as.AMD64_JGE(truePath)
	}

	// False.
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))                        // StrVal[0:8]
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))                      // StrVal[8:16]
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))              // NumVal
	as.AMD64_MOV_RI(REG_RCX, 0x0200)                                             // TagBoolean false
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))
	as.AMD64_JMP(donePath)

	as.AMD64_Bind(truePath)
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0201) // TagBoolean true
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))

	as.AMD64_Bind(donePath)
}

// --- emitAMD64CallN: OpCall0/OpCall1/OpCall2 ---
//
// nargs=0: sparkplugOpCall0(frame, calleeReg)
// nargs=1: sparkplugOpCall1(frame, calleeReg, arg1Reg)
// nargs=2: sparkplugOpCall2(frame, calleeReg, arg1Reg)
func emitAMD64CallN(as *Assembler, instr *js.Instruction, nargs int, deoptStub *Label) {
	calleeReg := int(instr.OperandA)
	calleeSlot := calleeReg * jsValueSize
	arg1Reg := int(instr.OperandB) // ignored for nargs=0

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))

	// Guard: callee must be an object.
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(calleeSlot+tagWordOff))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)

	// Guard: callee.ObjVal != nil.
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(calleeSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	// Call Go helper — full call sequence requires interpreter.
	as.AMD64_Bind(slowPath)
	switch nargs {
	case 0:
		as.AMD64_MOV_RR(REG_RAX, REG_R12)          // RAX = frame
		as.AMD64_MOV_RI(REG_RBX, uint64(calleeReg)) // RBX = calleeReg
		addr := funcToAddr(sparkplugOpCall0)
		as.AMD64_MOV_RI(REG_R11, uint64(addr))
		as.AMD64_CALL(REG_R11)
	case 1:
		as.AMD64_MOV_RR(REG_RAX, REG_R12)          // RAX = frame
		as.AMD64_MOV_RI(REG_RBX, uint64(calleeReg)) // RBX = calleeReg
		as.AMD64_MOV_RI(REG_RCX, uint64(arg1Reg))   // RCX = arg1Reg
		addr := funcToAddr(sparkplugOpCall1)
		as.AMD64_MOV_RI(REG_R11, uint64(addr))
		as.AMD64_CALL(REG_R11)
	case 2:
		as.AMD64_MOV_RR(REG_RAX, REG_R12)          // RAX = frame
		as.AMD64_MOV_RI(REG_RBX, uint64(calleeReg)) // RBX = calleeReg
		as.AMD64_MOV_RI(REG_RCX, uint64(arg1Reg))   // RCX = arg1Reg
		addr := funcToAddr(sparkplugOpCall2)
		as.AMD64_MOV_RI(REG_R11, uint64(addr))
		as.AMD64_CALL(REG_R11)
	}

	as.AMD64_Bind(done)
}

// emitAMD64CallSpread: OpCallSpread — call with spread arguments.
func emitAMD64CallSpread(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	calleeReg := int(instr.OperandA)
	spreadReg := int(instr.OperandB)
	fixedCount := int(instr.OperandC)

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R8, REG_R15, int8(calleeReg*jsValueSize+tagWordOff))
	as.AMD64_MOV_RI(REG_R13, 0x0500)
	as.AMD64_CMP_RR(REG_R8, REG_R13)
	as.AMD64_JNE(slowPath)
	as.AMD64_MOV_LOAD(REG_R9, REG_R15, int8(calleeReg*jsValueSize+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
	as.AMD64_TEST_RR(REG_R9, REG_R9)
	as.AMD64_JZ(slowPath)

	as.AMD64_Bind(slowPath)
	// sparkplugOpCallSpread(frame, calleeReg, spreadReg, fixedCount)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	as.AMD64_MOV_RI(REG_RBX, uint64(calleeReg))
	as.AMD64_MOV_RI(REG_RCX, uint64(spreadReg))
	as.AMD64_MOV_RI(REG_RDI, uint64(fixedCount))
	addr := funcToAddr(sparkplugOpCallSpread)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)

	as.AMD64_Bind(done)
}

// emitAMD64CallBuiltin: OpCallBuiltin — call registered Go builtin.
func emitAMD64CallBuiltin(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	builtinIdx := int(instr.OperandA)
	argCount := int(instr.OperandB)

	slowPath := NewLabel()
	done := NewLabel()

	// Guard: object guard not needed for builtins — just call.
	as.AMD64_Bind(slowPath)
	// sparkplugOpCallBuiltin(frame, builtinIdx, argCount)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	as.AMD64_MOV_RI(REG_RBX, uint64(builtinIdx))
	as.AMD64_MOV_RI(REG_RCX, uint64(argCount))
	addr := funcToAddr(sparkplugOpCallBuiltin)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)

	as.AMD64_Bind(done)
}

// emitAMD64CallDirect: OpCallDirect — direct call to known function.
func emitAMD64CallDirect(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	funcReg := int(instr.OperandA)
	argCount := int(instr.OperandB)

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_Bind(slowPath)
	// sparkplugOpCallDirect(frame, funcReg, argCount)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	as.AMD64_MOV_RI(REG_RBX, uint64(funcReg))
	as.AMD64_MOV_RI(REG_RCX, uint64(argCount))
	addr := funcToAddr(sparkplugOpCallDirect)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)

	as.AMD64_Bind(done)
}

// emitAMD64SuperCall: OpSuperCall — super() call in derived class constructors.
func emitAMD64SuperCall(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	argCount := int(instr.OperandA)

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_Bind(slowPath)
	// sparkplugOpSuperCall(frame, argCount)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	as.AMD64_MOV_RI(REG_RBX, uint64(argCount))
	addr := funcToAddr(sparkplugOpSuperCall)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)

	as.AMD64_Bind(done)
}

// emitAMD64New: OpNew — new constructor call.
func emitAMD64New(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	consReg := int(instr.OperandA)
	argCount := int(instr.OperandB)

	slowPath := NewLabel()
	done := NewLabel()

	as.AMD64_Bind(slowPath)
	// sparkplugOpNew(frame, consReg, argCount)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	as.AMD64_MOV_RI(REG_RBX, uint64(consReg))
	as.AMD64_MOV_RI(REG_RCX, uint64(argCount))
	addr := funcToAddr(sparkplugOpNew)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)

	as.AMD64_Bind(done)
}

// emitAMD64ThrowSuperAlreadyCalled: throw if super() was already called.
func emitAMD64ThrowSuperAlreadyCalled(as *Assembler, deoptStub *Label) {
	// sparkplugOpThrowSuperAlreadyCalled(frame)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	addr := funcToAddr(sparkplugOpThrowSuperAlreadyCalled)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
}

// emitAMD64ThrowSuperNotCalled: throw if super() was not called.
func emitAMD64ThrowSuperNotCalled(as *Assembler, deoptStub *Label) {
	// sparkplugOpThrowSuperNotCalled(frame)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	addr := funcToAddr(sparkplugOpThrowSuperNotCalled)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
}

// --- emitAMD64ModNumber: OpModNumber (no type guard — compiler proven) ---
// Regs[OperandA].NumVal % Acc.NumVal → Acc.
func emitAMD64ModNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc (RHS/divisor)
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))    // LHS (dividend)

	// Convert to int64: X0=LHS, X1=RHS
	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = LHS (dividend)
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = RHS (divisor)

	// Guard: divisor zero → NaN
	as.AMD64_XORPD(REG_X2, REG_X2)
	as.AMD64_COMISD(REG_X1, REG_X2)
	zeroDiv := NewLabel()
	as.AMD64_JE(zeroDiv)

	// Truncate to int64 (CVTTSD2SI truncates toward zero)
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0)  // R9 = int64(LHS)
	as.AMD64_CVTTSD2SI(REG_R11, REG_X1) // R11 = int64(RHS)

	// Handle INT64_MIN / -1 overflow
	as.AMD64_MOV_RR(REG_RAX, REG_R9) // RAX = dividend
	as.AMD64_MOV_RI(REG_RCX, math.MaxUint64) // RCX = -1
	as.AMD64_CMP_RR(REG_R11, REG_RCX)
	noOverflow := NewLabel()
	as.AMD64_JNE(noOverflow)
	// INT64_MIN % -1 = 0
	as.AMD64_XOR_RR(REG_R9, REG_R9)
	modDone := NewLabel()
	as.AMD64_JMP(modDone)
	as.AMD64_Bind(noOverflow)
	// CQO: sign-extend RAX → RDX:RAX
	as.AMD64_CQO()
	// IDIV: RDX = remainder
	as.AMD64_IDIV_RR(REG_R11)
	as.AMD64_MOV_RR(REG_R9, REG_RDX) // R9 = remainder
	as.AMD64_Bind(modDone)

	as.AMD64_CVTSI2SD(REG_X3, REG_R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X3)

	storeResult := NewLabel()
	as.AMD64_JMP(storeResult)

	as.AMD64_Bind(zeroDiv)
	as.AMD64_MOV_RI(REG_R9, uint64(math.Float64bits(math.NaN())))

	as.AMD64_Bind(storeResult)
	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))
}

// emitAMD64StrictEqNumber: OpStrictEqNumber (no type guard).
// Regs[OperandA].NumVal === Acc.NumVal → boolean Acc.
func emitAMD64StrictEqNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = LHS
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = RHS
	as.AMD64_COMISD(REG_X0, REG_X1)

	truePath := NewLabel()
	donePath := NewLabel()

	// NaN check: PF=1 → false
	as.AMD64_JP(donePath) // false path after donePath is set to false
	as.AMD64_JE(truePath)

	// False: Comisd ZF=0
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0200)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))
	as.AMD64_JMP(donePath)

	as.AMD64_Bind(truePath)
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0201)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))

	as.AMD64_Bind(donePath)
}

// emitAMD64StrictNotEqNumber: OpStrictNotEqNumber (no type guard).
func emitAMD64StrictNotEqNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))

	as.AMD64_MOVQ_XR(REG_X0, REG_R11)
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)
	as.AMD64_COMISD(REG_X0, REG_X1)

	truePath := NewLabel()
	donePath := NewLabel()

	// NaN (PF=1) → not equal → true
	as.AMD64_JP(truePath)
	// Equal (ZF=1) → false
	as.AMD64_JE(donePath) // will be false

	// Not equal → true
	as.AMD64_JMP(truePath)

	// False path
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0200)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))
	as.AMD64_JMP(donePath)

	as.AMD64_Bind(truePath)
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0201)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))

	as.AMD64_Bind(donePath)
}

// emitAMD64LessThanNumber: OpLessThanNumber (no type guard).
func emitAMD64LessThanNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	emitAMD64NumberCmp(as, lhsSlot, 0) // cond=0: LT
}

// emitAMD64GreaterThanNumber: OpGreaterThanNumber (no type guard).
func emitAMD64GreaterThanNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	emitAMD64NumberCmp(as, lhsSlot, 4) // cond=4: GT
}

// emitAMD64LessEqNumber: OpLessEqNumber (no type guard).
func emitAMD64LessEqNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	emitAMD64NumberCmp(as, lhsSlot, 1) // cond=1: LE
}

// emitAMD64GreaterEqNumber: OpGreaterEqNumber (no type guard).
func emitAMD64GreaterEqNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	emitAMD64NumberCmp(as, lhsSlot, 5) // cond=5: GE
}

// emitAMD64NumberCmp: shared number comparison helper (no type guard).
// cond: 0=LT, 1=LE, 2=EQ, 3=NE, 4=GT, 5=GE
func emitAMD64NumberCmp(as *Assembler, lhsSlot int, cond int) {
	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc (RHS)
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))    // LHS

	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = LHS
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = RHS
	as.AMD64_COMISD(REG_X0, REG_X1)

	truePath := NewLabel()
	donePath := NewLabel()

	// For all ordered comparisons: NaN is always false
	as.AMD64_JP(donePath) // NaN → false

	switch cond {
	case 0: // LT
		as.AMD64_JB(truePath)
	case 1: // LE
		as.AMD64_JBE(truePath)
	case 4: // GT
		as.AMD64_JA(truePath)
	case 5: // GE
		as.AMD64_JAE(truePath)
	}

	// False
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0200)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))
	as.AMD64_JMP(donePath)

	as.AMD64_Bind(truePath)
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0201)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))

	as.AMD64_Bind(donePath)
}

// emitAMD64BitAndNumber: OpBitAndNumber (no type guard).
func emitAMD64BitAndNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	emitAMD64BitwiseNumber(as, lhs, 0)
}

// emitAMD64BitOrNumber: OpBitOrNumber (no type guard).
func emitAMD64BitOrNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	emitAMD64BitwiseNumber(as, lhs, 1)
}

// emitAMD64BitXorNumber: OpBitXorNumber (no type guard).
func emitAMD64BitXorNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	emitAMD64BitwiseNumber(as, lhs, 2)
}

// emitAMD64BitwiseNumber: shared bitwise helper (no type guard).
// op: 0=AND, 1=OR, 2=XOR
func emitAMD64BitwiseNumber(as *Assembler, lhs int, op int) {
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc.NumVal
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))    // Regs[lhs].NumVal

	// Convert float64 → int64 (truncate).
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_MOVQ_XR(REG_X1, REG_R11)
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0)  // R9 = int64(Acc)
	as.AMD64_CVTTSD2SI(REG_R11, REG_X1) // R11 = int64(LHS)

	// Mask to 32-bit (JS ToInt32 semantics).
	as.AMD64_MOV_RI(REG_RDX, 0xFFFFFFFF)
	as.AMD64_AND_RR(REG_R9, REG_RDX)
	as.AMD64_AND_RR(REG_R11, REG_RDX)

	// Bitwise operation.
	switch op {
	case 0:
		as.AMD64_AND_RR(REG_R9, REG_R11)
	case 1:
		as.AMD64_OR_RR(REG_R9, REG_R11)
	case 2:
		as.AMD64_XOR_RR(REG_R9, REG_R11)
	}

	// Convert int64 → float64.
	as.AMD64_CVTSI2SD(REG_X2, REG_R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X2)

	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))

	// Zero ObjVal and StrVal.
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
}

// emitAMD64BitNotNumber: OpBitNotNumber (no type guard, unary).
func emitAMD64BitNotNumber(as *Assembler, instr *js.Instruction) {
	_ = instr

	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))

	// Convert float64 → int64.
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0)

	// Mask to 32-bit and NOT.
	as.AMD64_MOV_RI(REG_RDX, 0xFFFFFFFF)
	as.AMD64_AND_RR(REG_R9, REG_RDX)
	as.AMD64_NOT_R(REG_R9)

	// Convert back to float64.
	as.AMD64_CVTSI2SD(REG_X1, REG_R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X1)

	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))

	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
}

// emitAMD64ShiftLeftNumber: OpShiftLeftNumber (no type guard).
func emitAMD64ShiftLeftNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	emitAMD64ShiftNumber(as, lhs, 0)
}

// emitAMD64ShiftRightNumber: OpShiftRightNumber (no type guard).
func emitAMD64ShiftRightNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	emitAMD64ShiftNumber(as, lhs, 1)
}

// emitAMD64ShiftRightZeroNumber: OpShiftRightZeroNumber (no type guard).
func emitAMD64ShiftRightZeroNumber(as *Assembler, instr *js.Instruction) {
	lhs := int(instr.OperandA)
	emitAMD64ShiftNumber(as, lhs, 2)
}

// emitAMD64ShiftNumber: shared shift helper (no type guard).
// op: 0=SHL, 1=SAR, 2=SHR
func emitAMD64ShiftNumber(as *Assembler, lhs int, op int) {
	lhsSlot := lhs * jsValueSize

	as.AMD64_MOV_LOAD(REG_R15, REG_R12, int8(regsOff))
	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))   // Acc (RHS = shift count)
	as.AMD64_MOV_LOAD(REG_R11, REG_R15, int8(lhsSlot+numValOff))    // LHS (value to shift)

	// Convert to int64.
	as.AMD64_MOVQ_XR(REG_X0, REG_R11) // X0 = LHS value
	as.AMD64_MOVQ_XR(REG_X1, REG_R9)  // X1 = RHS shift count
	as.AMD64_CVTTSD2SI(REG_R9, REG_X0) // R9 = int64(LHS value)
	as.AMD64_CVTTSD2SI(REG_R11, REG_X1) // R11 = int64(RHS shift count)

	// Mask value to 32 bits (JS ToInt32 semantics).
	as.AMD64_MOV_RI(REG_RDX, 0xFFFFFFFF)
	as.AMD64_AND_RR(REG_R9, REG_RDX)

	// Mask shift count to 5 bits.
	as.AMD64_MOV_RI(REG_RCX, 0x1F)
	as.AMD64_AND_RR(REG_R11, REG_RCX)
	// Move shift count to CL.
	as.AMD64_MOV_RR(REG_RCX, REG_R11)

	switch op {
	case 0:
		as.AMD64_SHL_CL(REG_R9)
	case 1:
		as.AMD64_SAR_CL(REG_R9)
	case 2:
		as.AMD64_SHR_CL(REG_R9)
	}

	// Convert back to float64.
	as.AMD64_CVTSI2SD(REG_X2, REG_R9)
	as.AMD64_MOVQ_RX(REG_R9, REG_X2)

	as.AMD64_MOV_STORE(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_R13, 0x0300)
	as.AMD64_MOV_STORE(REG_R13, REG_R12, int8(accTagAlign))

	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+8))
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))))
}

// emitAMD64ToBooleanNumber: OpToBooleanNumber (no type guard).
// Convert number to boolean: 0/NaN → false, otherwise true.
func emitAMD64ToBooleanNumber(as *Assembler, instr *js.Instruction) {
	_ = instr

	as.AMD64_MOV_LOAD(REG_R9, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOVQ_XR(REG_X0, REG_R9)
	as.AMD64_XORPD(REG_X1, REG_X1)
	as.AMD64_COMISD(REG_X0, REG_X1)

	truePath := NewLabel()
	donePath := NewLabel()

	// NaN (PF=1) → false
	as.AMD64_JP(donePath)
	// 0.0 (ZF=1) → false
	as.AMD64_JE(donePath)
	// Non-zero → true
	as.AMD64_JMP(truePath)

	// False path (falls through from above)
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0200)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))
	as.AMD64_JMP(donePath)

	as.AMD64_Bind(truePath)
	as.AMD64_XOR_RR(REG_RCX, REG_RCX)
	// Store 1 in NumVal (for BoolVal convention: low byte of tag is BoolVal).
	as.AMD64_MOV_RI(REG_RCX, 1)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accOffset+numValOff))
	as.AMD64_MOV_RI(REG_RCX, 0x0201)
	as.AMD64_MOV_STORE(REG_RCX, REG_R12, int8(accTagAlign))

	as.AMD64_Bind(donePath)
}

// emitAMD64ToStringNumber: OpToStringNumber (no type guard).
// Convert number to string → requires Go helper for string allocation.
func emitAMD64ToStringNumber(as *Assembler, instr *js.Instruction) {
	_ = instr
	// sparkplugOpToStringNumber(frame)
	as.AMD64_MOV_RR(REG_RAX, REG_R12)
	addr := funcToAddr(sparkplugOpToStringNumber)
	as.AMD64_MOV_RI(REG_R11, uint64(addr))
	as.AMD64_CALL(REG_R11)
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
}
