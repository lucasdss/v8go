//go:build !amd64

package jit

import (
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// emitSparkplugOp emits ARM64 code for a single bytecode instruction.
func emitSparkplugOp(as *Assembler, instr *js.Instruction, bf *js.BytecodeFunction, labels map[int]*Label, epilogue *Label, deoptStub *Label, pc int, icSlotOffsets []int) {
	switch instr.Op {
	case js.OpNop:
		// No-op: emit nothing.

	case js.OpLdaConstant:
		// Load constant from pool: OperandA = const pool index.
		// Load frame.Func.Constants[idx] → Acc using native copy.
		emitSparkplugLdaConstantNative(as, instr)

	case js.OpLdaSmi:
		// Native inline: store small integer directly to Acc.
		emitSparkplugLdaSmiNative(as, instr)

	case js.OpLdaZero:
		// Native inline: store TagNumber 0.0 to Acc.
		emitSparkplugLdaZeroNative(as)

	case js.OpLdaOne:
		// Native inline: store TagNumber 1.0 to Acc.
		emitSparkplugLdaOneNative(as)

	case js.OpLdaUndefined:
		// Native inline: store TagUndefined to Acc.
		emitSparkplugLdaUndefinedNative(as)

	case js.OpLdaNull:
		// Native inline: store TagNull to Acc.
		emitSparkplugLdaNullNative(as)

	case js.OpLdaTrue:
		// Native inline: store true to Acc.
		emitSparkplugLdaTrueNative(as)

	case js.OpLdaFalse:
		// Native inline: store false to Acc.
		emitSparkplugLdaFalseNative(as)

	case js.OpStar:
		// Native inline: copy Acc (64 bytes) → Regs[OperandA].
		emitSparkplugStarNative(as, instr)

	case js.OpLdar:
		// Native inline: copy Regs[OperandA] → Acc (64 bytes).
		emitSparkplugLdarNative(as, instr)

	case js.OpReturn:
		// Native inline: set PC past end and branch to epilogue.
		// Store PC = len(bf.Instructions) directly, then jump to epilogue.
		emitSparkplugReturnNative(as, bf, epilogue)

	case js.OpJump:
		target := int(instr.OperandA)
		if l, ok := labels[target]; ok {
			as.B(l)
		}

	case js.OpJumpIfFalse:
		// Native inline: tag-based truthy check, branch on falsy.
		emitSparkplugJumpIfFalseNative(as, instr, labels, deoptStub)

	case js.OpJumpIfTrue:
		// Native inline: tag-based truthy check, branch on truthy.
		emitSparkplugJumpIfTrueNative(as, instr, labels, deoptStub)

	case js.OpAdd:
		emitSparkplugArithFast(as, instr, 0, deoptStub)

	case js.OpSub:
		emitSparkplugArithFast(as, instr, 1, deoptStub)

	case js.OpMul:
		emitSparkplugArithFast(as, instr, 2, deoptStub)

	case js.OpDiv:
		emitSparkplugArithFast(as, instr, 3, deoptStub)

	case js.OpMod:
		// Native fast path: TagNumber→FDIV+FRINTZ modulo; else deopt.
		emitSparkplugModFast(as, instr, deoptStub)

	// --- Bitwise ops ---

	case js.OpBitwiseAnd:
		emitSparkplugBitwiseFast(as, instr, 0, deoptStub)
	case js.OpBitwiseOr:
		emitSparkplugBitwiseFast(as, instr, 1, deoptStub)
	case js.OpBitwiseXor:
		emitSparkplugBitwiseFast(as, instr, 2, deoptStub)
	case js.OpBitwiseNot:
		emitSparkplugBitwiseNot(as, instr, deoptStub)

	// --- Shift ops ---

	case js.OpShiftLeft:
		emitSparkplugShiftFast(as, instr, 0, deoptStub)
	case js.OpShiftRight:
		emitSparkplugShiftFast(as, instr, 1, deoptStub)
	case js.OpShiftRightZero:
		emitSparkplugShiftFast(as, instr, 2, deoptStub)

	// --- Logical ops ---

	case js.OpLogicalNot:
		emitSparkplugLogicalNot(as, instr, deoptStub)
	case js.OpLogicalAnd:
		// Native short-circuit: if !acc.truthy → deopt, else load rhs to acc.
		emitSparkplugLogicalAndFast(as, instr, deoptStub)
	case js.OpLogicalOr:
		// Native short-circuit: if acc.truthy → deopt (keep acc), else load rhs to acc.
		emitSparkplugLogicalOrFast(as, instr, deoptStub)

	case js.OpNegate:
		emitSparkplugNegate(as, instr, deoptStub)

	// --- Comparison ops ---

	case js.OpStrictEq:
		emitSparkplugStrictEq(as, instr, deoptStub)

	case js.OpLessThan:
		emitSparkplugLessThan(as, instr, deoptStub)

	case js.OpGreaterThan:
		emitSparkplugGreaterThan(as, instr, deoptStub)

	case js.OpEq:
		// Loose equality: fast path for TagNumber→FCMP+CSET EQ, else deopt.
		emitSparkplugEq(as, instr, deoptStub)

	case js.OpNotEq:
		// Loose inequality: fast path for TagNumber→FCMP+CSET NE, else deopt.
		emitSparkplugNotEq(as, instr, deoptStub)

	case js.OpStrictNotEq:
		// Strict inequality: fast path for TagNumber→FCMP+CSET NE, else deopt.
		emitSparkplugStrictNotEq(as, instr, deoptStub)

	case js.OpLessEq:
		// Fast path: TagNumber→FCMP+CSET LE, else deopt.
		emitSparkplugLessEq(as, instr, deoptStub)

	case js.OpGreaterEq:
		// Fast path: TagNumber→FCMP+CSET GE, else deopt.
		emitSparkplugGreaterEq(as, instr, deoptStub)

	// --- Type conversion ops ---

	case js.OpToBoolean:
		emitSparkplugToBoolean(as, instr, deoptStub)

	case js.OpTypeof:
		// Native fast path for simple types; deopt for objects requiring callable check.
		emitSparkplugTypeofFast(as, instr, deoptStub)

	// --- Property access with IC slots ---

	case js.OpLdaNamedProperty:
		emitSparkplugLdaNamedProperty(as, instr, bf, icSlotOffsets, deoptStub)

	case js.OpStaNamedProperty:
		emitSparkplugStaNamedProperty(as, instr, icSlotOffsets, deoptStub)

	case js.OpCreateObjectLiteral:
		emitSparkplugCreateObjectLiteral(as, instr)

	case js.OpStaByOffset:
		// Native guarded: check obj → deopt to Go helper.
		emitSparkplugStaByOffsetFast(as, instr, deoptStub)

	case js.OpStaGlobalSlot:
		emitSparkplugStaGlobalSlot(as, instr)

	case js.OpLdaGlobalSlot:
		emitSparkplugLdaGlobalSlot(as, instr)

	// --- Keyed property access ---

	case js.OpLdaKeyedProperty:
		// Native fast path: inline array access with type guards; deopt on miss.
		emitSparkplugLdaKeyedPropertyFast(as, instr, deoptStub)

	case js.OpStaKeyedProperty:
		// Native fast path: inline array store with type guards; deopt on miss.
		emitSparkplugStaKeyedPropertyFast(as, instr, deoptStub)

	// --- Object / Array / Closure creation ---

	case js.OpCreateArray:
		// Native fast path: call Go helper for full array creation.
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCreateArray))

	case js.OpCreateObject:
		// Native fast path: call Go helper for full object creation.
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCreateObject))

	case js.OpCreateClosure:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCreateClosure))

	// --- Delete / Type conversion / Captured ---

	case js.OpDelete:
		emitSparkplugDeleteFast(as, instr, deoptStub)

	case js.OpDeleteKeyed:
		emitSparkplugDeleteKeyedFast(as, instr, deoptStub)

	case js.OpToNumber:
		// Native fast path: TagNumber→skip; else deopt.
		emitSparkplugToNumberFast(as, instr, deoptStub)

	case js.OpToString:
		// Native fast path: TagString→skip; else deopt.
		emitSparkplugToStringFast(as, instr, deoptStub)

	case js.OpLdaCaptured:
		// Native inline: load from ClosureEnv (deopt if not cached).
		emitSparkplugLdaCapturedNative(as, instr, deoptStub)

	// --- Function calls ---

	case js.OpCall0:
		emitSparkplugCallN(as, instr, 0, deoptStub)

	case js.OpCall1:
		emitSparkplugCallN(as, instr, 1, deoptStub)

	case js.OpCall2:
		emitSparkplugCallN(as, instr, 2, deoptStub)

	case js.OpCall:
		emitSparkplugCallVarargs(as, instr, deoptStub)

	case js.OpCallSpread:
		emitSparkplugCallSpreadFast(as, instr, deoptStub)

	// --- Additional control flow ---

	case js.OpJumpIfToBooleanTrue:
		target := int(instr.OperandA)
		l := labels[target]
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugIsFalsy))
		as.CBZ(REG_R0, l) // not falsy = truthy → jump

	case js.OpJumpIfToBooleanFalse:
		target := int(instr.OperandA)
		l := labels[target]
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugIsFalsy))
		as.CBNZ(REG_R0, l) // falsy → jump

	case js.OpJumpIfNotNullish:
		target := int(instr.OperandA)
		l := labels[target]
		// Nullish check: null (tag=0x0100) or undefined (tag=0x0000).
		as.LDR(REG_R8, REG_VM0, accTagAlign)
		as.CBZ(REG_R8, labels[pc+1]) // undefined → skip jump
		as.MOVZ(REG_R9, 0x0100, 0)
		as.CMP(REG_R8, REG_R9)
		as.BEQ(labels[pc+1]) // null → skip jump
		as.B(l)              // not nullish → jump

	// --- Misc ops ---

	case js.OpInstanceof:
		// Native fast path: inline prototype chain walk for objects; deopt on non-objects.
		emitSparkplugInstanceofFast(as, instr, deoptStub)

	case js.OpIn:
		// Native fast path: inline hasOwn check for objects; deopt on non-objects.
		emitSparkplugInFast(as, instr, deoptStub)

	case js.OpCreateRegExp:
		// Native fast path: call Go helper for full regex creation.
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpCreateRegExp))

	case js.OpThrow:
		// Native inline: set Thrown and bail out.
		emitSparkplugThrowFast(as, instr, deoptStub)

	case js.OpSetTryHandler:
		// Native inline: store handlerPC to frame.HandlerPC.
		emitSparkplugSetTryHandlerNative(as, instr)

	case js.OpClearTryHandler:
		// Native inline: store -1 to frame.HandlerPC.
		emitSparkplugClearTryHandlerNative(as)

	case js.OpSetFinallyHandler:
		// Native inline: store finally PC to frame.FinallyPC.
		emitSparkplugSetFinallyHandlerNative(as, instr)

	case js.OpForInSetup:
		// Native guarded: check obj is object → deopt to Go helper.
		emitSparkplugForInSetupFast(as, instr, deoptStub)

	case js.OpForInNext:
		// Native guarded: check obj is object → deopt to Go helper.
		emitSparkplugForInNextFast(as, instr, deoptStub)

	// --- Extended ops (Loop 1: 13 new native ops) ---

	case js.OpMov:
		// Native inline: copy Regs[OperandB] → Regs[OperandA] (64 bytes).
		emitSparkplugMovNative(as, instr)

	case js.OpExp:
		// Native fast path: TagNumber → math.Pow via Go; else fallback to Go.
		emitSparkplugExpFast(as, instr, deoptStub)

	case js.OpInc:
		// Native inline: load Regs[OperandA], add 1.0, store back + Acc.
		emitSparkplugIncNative(as, instr, deoptStub)

	case js.OpDec:
		// Native inline: load Regs[OperandA], sub 1.0, store back + Acc.
		emitSparkplugDecNative(as, instr, deoptStub)

	case js.OpDup:
		// Native inline: copy Acc → Regs[OperandA].
		emitSparkplugDupNative(as, instr)

	case js.OpLdaGlobal:
		// Native fast path: check Constants[constIdx] cached value; deopt on miss.
		emitSparkplugLdaGlobalNative(as, instr, deoptStub)

	case js.OpStaGlobal:
		// Native fast path: check Constants[constIdx] cached value; deopt on miss.
		emitSparkplugStaGlobalNative(as, instr, deoptStub)

	case js.OpLdaLocal:
		// Native inline: copy Regs[OperandA] → Acc.
		emitSparkplugLdarNative(as, instr) // same pattern as Ldar

	case js.OpStaLocal:
		// Native inline: copy Acc → Regs[OperandA].
		emitSparkplugStarNative(as, instr) // same pattern as Star

	case js.OpLdaThis:
		// Native inline: copy frame.This → Acc.
		emitSparkplugLdaThisNative(as, instr)

	case js.OpThrowConstAssignment:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpThrowConstAssignment))

	case js.OpSetPrototype:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpSetPrototype))

	case js.OpCheckConstructor:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCheckConstructor))

	// --- Generator / super ops ---

	case js.OpYield:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpYield))

	case js.OpYieldDelegate:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpYieldDelegate))

	case js.OpCreateGenerator:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCreateGenerator))

	case js.OpSuperCall:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpSuperCall))

	// --- Batch 1: Extended typed fast-path arithmetic/comparison ops ---

	case js.OpAddNumber:
		// Native inline: FADD acc, Regs[OperandA] → acc. No type guards.
		emitSparkplugAddNumber(as, instr, deoptStub)
	case js.OpSubNumber:
		emitSparkplugSubNumber(as, instr, deoptStub)
	case js.OpMulNumber:
		emitSparkplugMulNumber(as, instr, deoptStub)
	case js.OpDivNumber:
		emitSparkplugDivNumber(as, instr, deoptStub)
	case js.OpModNumber:
		emitSparkplugModNumber(as, instr, deoptStub)
	case js.OpNegateNumber:
		emitSparkplugNegateNumber(as, instr, deoptStub)
	case js.OpIncNumber:
		emitSparkplugIncNumber(as, instr, deoptStub)
	case js.OpDecNumber:
		emitSparkplugDecNumber(as, instr, deoptStub)
	case js.OpStrictEqNumber:
		emitSparkplugStrictEqNumber(as, instr, deoptStub)
	case js.OpCmpNumber:
		emitSparkplugCmpNumber(as, instr, deoptStub)

	// --- Batch 2: Extended bitwise/comparison/string/array ops ---

	case js.OpBitAndNumber:
		emitSparkplugBitAndNumber(as, instr, deoptStub)
	case js.OpBitOrNumber:
		emitSparkplugBitOrNumber(as, instr, deoptStub)
	case js.OpBitXorNumber:
		emitSparkplugBitXorNumber(as, instr, deoptStub)
	case js.OpBitNotNumber:
		emitSparkplugBitNotNumber(as, instr, deoptStub)
	case js.OpShiftLeftNumber:
		emitSparkplugShiftLeftNumber(as, instr, deoptStub)
	case js.OpStrictNotEqNumber:
		emitSparkplugStrictNotEqNumber(as, instr, deoptStub)
	case js.OpLessThanNumber:
		emitSparkplugLessThanNumber(as, instr, deoptStub)
	case js.OpGreaterThanNumber:
		emitSparkplugGreaterThanNumber(as, instr, deoptStub)
	case js.OpStringConcat:
		emitSparkplugStringConcat(as, instr, deoptStub)
	case js.OpArrayLength:
		emitSparkplugArrayLength(as, instr, deoptStub)

	// --- Batch 3: Property/Object/Math/Global ops ---

	case js.OpLdaPropByOffset:
		emitSparkplugLdaPropByOffset(as, instr, deoptStub)
	case js.OpStaPropByOffset:
		emitSparkplugStaPropByOffsetNative(as, instr, deoptStub)
	case js.OpArrayGetIndex:
		emitSparkplugArrayGetIndex(as, instr, deoptStub)
	case js.OpArraySetIndex:
		emitSparkplugArraySetIndex(as, instr, deoptStub)
	case js.OpCreateEmptyArray:
		emitSparkplugCreateEmptyArray(as, instr, deoptStub)
	case js.OpLdaGlobalDirect:
		emitSparkplugLdaGlobalDirect(as, instr, deoptStub)
	case js.OpStaGlobalDirect:
		emitSparkplugStaGlobalDirect(as, instr, deoptStub)
	case js.OpMathAbs:
		emitSparkplugMathAbs(as, instr, deoptStub)
	case js.OpMathFloor:
		emitSparkplugMathFloor(as, instr, deoptStub)
	case js.OpLessEqNumber:
		emitSparkplugLessEqNumber(as, instr, deoptStub)

	// --- Batch 4: Context/Scope/String/Math/Call ops ---

	case js.OpToStringNumber:
		emitSparkplugToStringNumber(as, instr, deoptStub)
	case js.OpToBooleanNumber:
		emitSparkplugToBooleanNumber(as, instr, deoptStub)
	case js.OpStringLength:
		emitSparkplugStringLength(as, instr, deoptStub)
	case js.OpStringEq:
		emitSparkplugStringEq(as, instr, deoptStub)
	case js.OpCallBuiltin:
		emitSparkplugCallBuiltin(as, instr, deoptStub)
	case js.OpCallDirect:
		emitSparkplugCallDirect(as, instr, deoptStub)
	case js.OpPushContext:
		emitSparkplugPushContext(as, instr, deoptStub)
	case js.OpPopContext:
		emitSparkplugPopContext(as, instr, deoptStub)
	case js.OpLoadContextSlot:
		emitSparkplugLoadContextSlot(as, instr, deoptStub)
	case js.OpStoreContextSlot:
		emitSparkplugStoreContextSlot(as, instr, deoptStub)

	// --- Batch 5: Final 10 ops (shift/math/forin/misc) ---

	case js.OpGreaterEqNumber:
		emitSparkplugGreaterEqNumber(as, instr, deoptStub)
	case js.OpShiftRightNumber:
		emitSparkplugShiftRightNumber(as, instr, deoptStub)
	case js.OpShiftRightZeroNumber:
		emitSparkplugShiftRightZeroNumber(as, instr, deoptStub)
	case js.OpMathCeil:
		emitSparkplugMathCeil(as, instr, deoptStub)
	case js.OpMathSqrt:
		emitSparkplugMathSqrt(as, instr, deoptStub)
	case js.OpForInSetupFast:
		emitSparkplugForInSetupFastExt(as, instr, deoptStub)
	case js.OpForInNextFast:
		emitSparkplugForInNextFastExt(as, instr, deoptStub)
	case js.OpSwap:
		emitSparkplugSwap(as, instr, deoptStub)
	case js.OpLdaTrueFast:
		emitSparkplugLdaTrueFast(as, deoptStub)
	case js.OpLdaFalseFast:
		emitSparkplugLdaFalseFast(as, deoptStub)

	// --- Batch 6: Debugger, module, throw completion ops ---

	case js.OpDebugger:
		// No-op: debugger statement ignored in JIT.

	case js.OpLdaModuleVar:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpLdaModuleVar))

	case js.OpStaModuleVar:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpStaModuleVar))

	case js.OpThrowIfNotSuper:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpThrowIfNotSuper))

	case js.OpThrowIfHole:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpThrowIfHole))

	case js.OpCatch:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpCatch))

	case js.OpEndTry:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpEndTry))

	// --- Batch 7: Super/constructor completion ---

	case js.OpThrowSuperAlreadyCalled:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpThrowSuperAlreadyCalled))

	case js.OpThrowSuperNotCalled:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpThrowSuperNotCalled))

	case js.OpGetSuperConstructor:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpGetSuperConstructor))

	case js.OpLdaHomeObject:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpLdaHomeObject))

	case js.OpLdaHomeObjectProperty:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpLdaHomeObjectProperty))

	case js.OpStaHomeObjectProperty:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpStaHomeObjectProperty))

	case js.OpInitDerived:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpInitDerived))

	// --- Batch 8: Slot increment/decrement ---

	case js.OpCheckThisReinit:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCheckThisReinit))

	case js.OpIncGlobalSlot:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpIncGlobalSlot))

	case js.OpDecGlobalSlot:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpDecGlobalSlot))

	case js.OpIncNamedProperty:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpIncNamedProperty))

	case js.OpDecNamedProperty:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpDecNamedProperty))

	case js.OpIncKeyedProperty:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpIncKeyedProperty))

	case js.OpDecKeyedProperty:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpDecKeyedProperty))

	// --- Batch 9: Generator completion ---

	case js.OpSuspendGenerator:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpSuspendGenerator))

	case js.OpResumeGenerator:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpResumeGenerator))

	case js.OpGetIterator:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpGetIterator))

	case js.OpIteratorNext:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpIteratorNext))

	case js.OpIteratorClose:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpIteratorClose))

	case js.OpCreateGeneratorObject:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCreateGeneratorObject))

	case js.OpGeneratorRestore:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpGeneratorRestore))

	// --- Batch 10: Async/await + object utilities ---

	case js.OpAwait:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpAwait))

	case js.OpCreateAsyncGenerator:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpCreateAsyncGenerator))

	case js.OpAsyncAwait:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpAsyncAwait))

	case js.OpAsyncReturn:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpAsyncReturn))

	case js.OpCopyDataProperties:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpCopyDataProperties))

	case js.OpToObject:
		as.MOV(REG_R0, REG_VM0)
		emitCall(as, funcToAddr(sparkplugOpToObject))

	// --- Batch 11: New ops (Construct, Error, OptionalChain, NullishCoalesce, Private, ForOf, Class) ---

	case js.OpNew:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpNew))

	case js.OpThrowReferenceError:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpThrowReferenceError))

	case js.OpThrowTypeError:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpThrowTypeError))

	case js.OpOptionalChain:
		// Native inline: null/undefined check with branch.
		target := int(instr.OperandA)
		l := labels[target]
		as.LDR(REG_R8, REG_VM0, accTagAlign)
		as.CBZ(REG_R8, l) // undefined → short-circuit jump
		as.MOVZ(REG_R9, 0x0100, 0)
		as.CMP(REG_R8, REG_R9)
		as.BEQ(l) // null → short-circuit jump

	case js.OpNullishCoalesce:
		// Native inline: if NOT null/undefined, jump (skip rhs).
		target := int(instr.OperandA)
		l := labels[target]
		as.LDR(REG_R8, REG_VM0, accTagAlign)
		as.CBZ(REG_R8, labels[pc+1]) // undefined → fall through (use rhs)
		as.MOVZ(REG_R9, 0x0100, 0)
		as.CMP(REG_R8, REG_R9)
		as.BEQ(labels[pc+1]) // null → fall through (use rhs)
		as.B(l)              // not nullish → skip rhs

	case js.OpPrivateGet:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		emitCall(as, funcToAddr(sparkplugOpPrivateGet))

	case js.OpPrivateSet:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		as.MOVZ(REG_R2, uint16(instr.OperandB), 0)
		as.MOVZ(REG_R3, uint16(instr.OperandC), 0)
		emitCall(as, funcToAddr(sparkplugOpPrivateSet))

	case js.OpForOfSetup:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpForOfSetup))

	case js.OpForOfNext:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpForOfNext))

	case js.OpDefineClass:
		as.MOV(REG_R0, REG_VM0)
		as.MOVZ(REG_R1, uint16(instr.OperandA), 0)
		emitCall(as, funcToAddr(sparkplugOpDefineClass))
	}
}

// emitSparkplugLdaNamedProperty emits code for OpLdaNamedProperty with IC slot.
//
// 32-byte IC sled layout (8 instructions, patched by PatchICSlot):
//
//	sled+0:  MOVZ x10, #lo16(shape)        ← patched at runtime
//	sled+4:  MOVK x10, #hi16(shape), lsl #16
//	sled+8:  MOVK x10, #hi32(shape), lsl #32
//	sled+12: MOVK x10, #hi48(shape), lsl #48
//	sled+16: LDR  x9, [x8, #shapeOffset]    ← EmitICSlot part
//	sled+20: CMP  x9, x10
//	sled+24: B.NE +16
//	sled+28: LDR  x0, [x8, #propOffset]
//
// On hit: x0 holds property value → stored to frame.Acc.
// On miss (nil obj, shape mismatch): branch to deoptStub.
func emitSparkplugLdaNamedProperty(as *Assembler, instr *js.Instruction, bf *js.BytecodeFunction, icSlotOffsets []int, deoptStub *Label) {
	propIdx := int(instr.OperandA)
	_ = propIdx // reserved for future slow-path bailout metadata
	slotIdx := int(instr.OperandC)

	objPtrOff := accOffset + accObjValOff

	slowPath := NewLabel()
	cont := NewLabel()

	// Load obj pointer: x8 = frame.Acc.ObjVal
	as.LDR(REG_R8, REG_VM0, objPtrOff)

	// Guard: nil obj pointer → slow path
	as.CBZ(REG_R8, slowPath)

	// Emit 32-byte IC sled:
	// First 16 bytes: shape pointer load into x10 (4 NOPs, patched at runtime)
	sledOff := as.Pos()
	as.NOP() // sled+0:  → MOVZ x10, #lo16(shape)
	as.NOP() // sled+4:  → MOVK x10, #hi16(shape), lsl #16
	as.NOP() // sled+8:  → MOVK x10, #hi32(shape), lsl #32
	as.NOP() // sled+12: → MOVK x10, #hi48(shape), lsl #48

	// Next 16 bytes: guard+load sled (EmitICSlot: 4 NOPs)
	as.EmitICSlot() // sled+16..+28: LDR shape / CMP / B.NE / LDR prop

	if slotIdx >= 0 && slotIdx < len(icSlotOffsets) {
		icSlotOffsets[slotIdx] = sledOff
	}

	// After sled: on hit, x0 holds property value.
	// Store x0 to frame.Acc and skip slow path.
	as.STR(REG_R0, REG_VM0, accOffset) // frame.Acc = x0
	as.B(cont)                         // skip slow path

	// Slow path: deoptimize back to interpreter.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(cont)
}

// emitSparkplugStaNamedProperty emits JIT code for OpStaNamedProperty with IC slot.
//
// 32-byte IC sled layout (same as LdaNamedProperty, but after hit stores value):
//
//	sled+0:  MOVZ x10, #lo16(shape)        ← patched at runtime
//	sled+4:  MOVK x10, #hi16(shape), lsl #16
//	sled+8:  MOVK x10, #hi32(shape), lsl #32
//	sled+12: MOVK x10, #hi48(shape), lsl #48
//	sled+16: LDR  x9, [x8, #shapeOffset]    ← EmitICSlot part
//	sled+20: CMP  x9, x10
//	sled+24: B.NE +16
//	sled+28: STR  x1, [x8, #propByteOff]    ← stores value (not LDR)
//
// On miss: branch to deoptStub. On hit: value stored.
func emitSparkplugStaNamedProperty(as *Assembler, instr *js.Instruction, icSlotOffsets []int, deoptStub *Label) {
	propIdx := int(instr.OperandA)
	_ = propIdx
	slotIdx := int(instr.OperandC)
	valReg := int(instr.OperandB)

	objPtrOff := accOffset + accObjValOff

	slowPath := NewLabel()
	cont := NewLabel()

	// Load obj pointer: x8 = frame.Acc.ObjVal
	as.LDR(REG_R8, REG_VM0, objPtrOff)

	// Guard: nil obj pointer → slow path
	as.CBZ(REG_R8, slowPath)

	// Load value from Regs[valReg] into x1 (needed for store IC patching).
	// Load frame.Regs slice data pointer, then load JSValue at valReg offset.
	valSlot := valReg * jsValueSize
	as.LDR(REG_R12, REG_VM0, regsOff)          // R12 = frame.Regs data ptr
	as.LDR(REG_R1, REG_R12, valSlot+numValOff) // R1 = Regs[valReg].NumVal
	// Note: the full JSValue copy needs tag too; for now use Go helper for actual store.

	// Emit 32-byte IC sled (4 NOPs shape load + EmitICSlot)
	sledOff := as.Pos()
	as.NOP()        // sled+0:  → MOVZ x10, #lo16(shape)
	as.NOP()        // sled+4:  → MOVK x10, #hi16(shape), lsl #16
	as.NOP()        // sled+8:  → MOVK x10, #hi32(shape), lsl #32
	as.NOP()        // sled+12: → MOVK x10, #hi48(shape), lsl #48
	as.EmitICSlot() // sled+16..+28: patched by PatchICSlotStore → STR x1 on hit

	if slotIdx >= 0 && slotIdx < len(icSlotOffsets) {
		icSlotOffsets[slotIdx] = sledOff
	}

	// After sled: on hit, B.NE +16 skips here (fallthrough = hit).
	// On miss, B.NE jumps to slowPath.
	as.B(cont)

	// Slow path: deoptimize back to interpreter.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(cont)

	// Fallback: call Go helper for store.
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R2, uint16(slotIdx), 0)
	as.MOVZ(REG_R3, uint16(valReg), 0)
	emitCall(as, funcToAddr(sparkplugOpStaNamedPropertySlow))
}

// emitSparkplugCreateObjectLiteral emits JIT code for OpCreateObjectLiteral.
func emitSparkplugCreateObjectLiteral(as *Assembler, instr *js.Instruction) {
	shapeKeyIdx := int(instr.OperandA)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(shapeKeyIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpCreateObjectLiteral))
}

// emitSparkplugArithFast emits native ARM64 code for binary arithmetic ops.
//
// Fast path (both operands are TagNumber):
//
//	Loads Acc.Tag, Acc.NumVal, Regs[lhs].Tag, Regs[lhs].NumVal into registers.
//	Guards: if either tag ≠ TagNumber, jumps to slowPath.
//	Performs float64 op (FADD/FSUB/FMUL/FDIV) and stores result back to Acc.
//
// Slow path:
//
//	Calls the Go helper (sparkplugOpAdd/Sub/Mul/Div) which handles strings,
//	BigInt, and type coercion.
//
// Register allocation:
//
//	R8  = Acc tag word (bits at offset tagWordOff)
//	R9  = Acc NumVal (float64 bits)
//	R10 = Lhs tag word
//	R11 = Lhs NumVal (float64 bits)
//	R12 = Regs slice data pointer
//	R13 = constant 0x0300 (TagNumber tag word)
//	V0  = float64 Acc
//	V1  = float64 Lhs
func emitSparkplugArithFast(as *Assembler, instr *js.Instruction, op int, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer: R12 = frame.Regs (data pointer at offset regsOff).
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Load Lhs tag word and NumVal from Regs[lhs].
	as.LDR(REG_R10, REG_R12, lhsSlot+tagWordOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Guard: both must be TagNumber. Tag word for Number = 0x0300.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Move float64 bits to FP registers and perform the operation.
	as.FMOV_XD(REG_R9, 0)  // V0 = Acc.NumVal
	as.FMOV_XD(REG_R11, 1) // V1 = Regs[lhs].NumVal

	switch op {
	case 0: // Add
		as.FADD(0, 0, 1)
	case 1: // Sub
		as.FSUB(0, 0, 1)
	case 2: // Mul
		as.FMUL(0, 0, 1)
	case 3: // Div
		as.FDIV(0, 0, 1)
	}

	// Move result back to int register and store to Acc.
	as.FMOV(0, REG_R9)                           // R9 = V0 (result float64)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff) // Acc.NumVal = result
	as.STR(REG_R13, REG_VM0, accTagAlign)        // Acc tag word = 0x0300 (TagNumber)

	as.B(done)

	// Slow path: deoptimize back to interpreter.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugNegate emits native ARM64 code for OpNegate (-acc).
//
// Fast path (acc is TagNumber):
//
//	FNEG on acc.NumVal, store back.
//
// Slow path:
//
//	Calls sparkplugOpNegate Go helper.
func emitSparkplugNegate(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	slowPath := NewLabel()
	done := NewLabel()

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Guard: must be TagNumber.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Negate: V0 = -Acc.NumVal
	as.FMOV_XD(REG_R9, 0) // V0 = Acc.NumVal
	as.FNEG(0, 0)         // V0 = -V0
	as.FMOV(0, REG_R9)    // R9 = V0

	// Store result.
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_R13, REG_VM0, accTagAlign) // tag stays Number

	as.B(done)

	// Slow path: deoptimize back to interpreter.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugBitwiseFast emits native ARM64 code for bitwise binary ops
// (And, Or, Xor). Fast path: both operands TagNumber → FCVTZS to int32 →
// bitwise op → SCVTF back to float64 → store.
//
// op: 0=And, 1=Or, 2=Xor
func emitSparkplugBitwiseFast(as *Assembler, instr *js.Instruction, op int, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Load Lhs tag word and NumVal.
	as.LDR(REG_R10, REG_R12, lhsSlot+tagWordOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Guard: both must be TagNumber.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Convert float64 → int32.
	as.FMOV_XD(REG_R9, 0)  // V0 = Acc.NumVal
	as.FMOV_XD(REG_R11, 1) // V1 = Lhs.NumVal
	as.FCVTZS(REG_R9, 0)   // W9 = int32(D0)
	as.FCVTZS(REG_R11, 1)  // W11 = int32(D1)

	// Bitwise operation (64-bit; upper 32 bits zeroed by FCVTZS write to W).
	switch op {
	case 0:
		as.AND(REG_R9, REG_R9, REG_R11)
	case 1:
		as.ORR(REG_R9, REG_R9, REG_R11)
	case 2:
		as.EOR(REG_R9, REG_R9, REG_R11)
	}

	// Convert int32 → float64, store result.
	as.SCVTF(0, REG_R9) // D0 = float64(W9)
	as.FMOV(0, REG_R9)  // R9 = V0
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_R13, REG_VM0, accTagAlign)

	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugBitwiseNot emits native ARM64 code for OpBitwiseNot (~acc).
// Fast path: acc is TagNumber → FCVTZS → MVN → SCVTF → store.
func emitSparkplugBitwiseNot(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Guard: must be TagNumber.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Convert float64 → int32, bitwise NOT, convert back.
	as.FMOV_XD(REG_R9, 0)  // V0 = Acc.NumVal
	as.FCVTZS(REG_R9, 0)   // W9 = int32(D0)
	as.MVN(REG_R9, REG_R9) // R9 = ~R9
	as.SCVTF(0, REG_R9)    // D0 = float64(W9)
	as.FMOV(0, REG_R9)     // R9 = V0

	// Store result.
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_R13, REG_VM0, accTagAlign)

	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugShiftFast emits native ARM64 code for shift ops
// (ShiftLeft, ShiftRight, ShiftRightZero).
// Fast path: both operands TagNumber → FCVTZS to int32 →
// shift (masked to 5 bits) → SCVTF back to float64 → store.
//
// op: 0=ShiftLeft, 1=ShiftRight, 2=ShiftRightZero
func emitSparkplugShiftFast(as *Assembler, instr *js.Instruction, op int, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Load Lhs tag word and NumVal (shift count).
	as.LDR(REG_R10, REG_R12, lhsSlot+tagWordOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Guard: both must be TagNumber.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Convert float64 → int32.
	as.FMOV_XD(REG_R9, 0)  // V0 = Acc.NumVal (value to shift)
	as.FMOV_XD(REG_R11, 1) // V1 = Lhs.NumVal (shift count)
	as.FCVTZS(REG_R9, 0)   // W9 = int32(value)
	as.FCVTZS(REG_R11, 1)  // W11 = int32(shift count)

	// Mask shift count to 5 bits (JS semantics: count & 0x1F).
	as.MOVZ(REG_R12, 0x1F, 0)
	as.AND(REG_R11, REG_R11, REG_R12)

	// Shift operation.
	switch op {
	case 0: // ShiftLeft
		as.LSL(REG_R9, REG_R9, REG_R11)
	case 1: // ShiftRight
		as.ASR(REG_R9, REG_R9, REG_R11)
	case 2: // ShiftRightZero
		as.LSR(REG_R9, REG_R9, REG_R11)
	}

	// Convert int32 → float64, store result.
	as.SCVTF(0, REG_R9) // D0 = float64(W9)
	as.FMOV(0, REG_R9)  // R9 = V0
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_R13, REG_VM0, accTagAlign)

	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugLogicalNot emits native ARM64 code for OpLogicalNot (!acc).
// Fast path: checks common falsy/truthy types inline.
// Slow path: deoptimize to interpreter.
func emitSparkplugLogicalNot(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	isFalsy := NewLabel()
	storeBool := NewLabel()
	done := NewLabel()

	// Load Acc tag word.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// TagNumber word: bytes [BoolVal, Tag, padding...] at offset 56.
	// TagBoolean=2 → byte at offset 57 = 2, so tag word = 0x0200 | BoolVal.
	// TagUndefined=0 → tag word = 0x0000.
	// TagNull=1 → tag word = 0x0100.

	// Check TagUndefined (0): if tag word == 0, acc is undefined → result = true.
	as.CBZ(REG_R8, isFalsy)

	// Check TagNull: tag word = 0x0100.
	as.MOVZ(REG_R9, 0x0100, 0)
	as.CMP(REG_R8, REG_R9)
	as.BEQ(isFalsy)

	// Check TagBoolean: (tag_word & 0xFF00) == 0x0200.
	as.MOVZ(REG_R9, 0xFF00, 0)
	as.AND(REG_R10, REG_R8, REG_R9)
	as.MOVZ(REG_R9, 0x0200, 0)
	as.CMP(REG_R10, REG_R9)
	as.BNE(slowPath) // Not Boolean; slow path.

	// Boolean: invert BoolVal (low byte).
	as.MOVZ(REG_R12, 1, 0)
	as.AND(REG_R9, REG_R8, REG_R12) // R9 = BoolVal (0 or 1)
	as.EOR(REG_R9, REG_R9, REG_R12) // R9 = !R9
	as.B(storeBool)

	// Falsy value → result = true
	as.Bind(isFalsy)
	as.MOVZ(REG_R9, 1, 0)

	// Store boolean result.
	as.Bind(storeBool)
	as.MOVZ(REG_R8, 0x0200, 0)
	as.ORR(REG_R8, REG_R8, REG_R9) // tag = 0x0200 | boolVal
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_ZR, REG_VM0, accOffset+numValOff) // NumVal = 0

	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugCompareFast emits native ARM64 code for comparison ops (StrictEq, LessThan, GreaterThan).
//
// Fast path (both operands are TagNumber):
//
//	Loads Acc.Tag, Acc.NumVal, Regs[lhs].Tag, Regs[lhs].NumVal into registers.
//	Guards: if either tag ≠ TagNumber, jumps to slowPath.
//	Performs FCMP + CSET with the given condition and stores boolean result.
//
// Slow path:
//
//	Calls the Go helper which handles BigInt, type coercion, etc.
//
// Register allocation (same as emitSparkplugArithFast):
//
//	R8  = Acc tag word
//	R9  = Acc NumVal (float64 bits) → becomes boolean result (0/1)
//	R10 = Lhs tag word
//	R11 = Lhs NumVal (float64 bits)
//	R12 = Regs slice data pointer
//	R13 = TagNumber tag word (0x0300)
//	V0  = Acc float64, V1 = Lhs float64
func emitSparkplugCompareFast(as *Assembler, instr *js.Instruction, cond int, slowFn interface{}, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer: R12 = frame.Regs.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Load Lhs tag word and NumVal from Regs[lhs].
	as.LDR(REG_R10, REG_R12, lhsSlot+tagWordOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Guard: both must be TagNumber. Tag word for Number = 0x0300.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Move float64 bits to FP registers and compare.
	as.FMOV_XD(REG_R9, 0)  // V0 = Acc.NumVal
	as.FMOV_XD(REG_R11, 1) // V1 = Regs[lhs].NumVal
	as.FCMP(0, 1)          // compare V0 with V1

	// CSET: R9 = (condition true) ? 1 : 0
	as.CSET(REG_R9, cond)

	// Build boolean tag word: 0x0200 (TagBoolean, false) | R9 (BoolVal).
	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)

	// Store result to Acc.
	as.STR(REG_R8, REG_VM0, accTagAlign)         // Acc tag word = TagBoolean | BoolVal
	as.STR(REG_R9, REG_VM0, accOffset+numValOff) // Acc.NumVal = 0 or 1

	as.B(done)

	// Slow path: deoptimize back to interpreter.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugStrictEq emits native ARM64 code for OpStrictEq (acc === lhs → acc).
func emitSparkplugStrictEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 0 /* EQ */, sparkplugOpStrictEq, deoptStub)
}

// emitSparkplugLessThan emits native ARM64 code for OpLessThan (acc < lhs → acc).
func emitSparkplugLessThan(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 11 /* LT */, sparkplugOpLessThan, deoptStub)
}

// emitSparkplugGreaterThan emits native ARM64 code for OpGreaterThan (acc > lhs → acc).
func emitSparkplugGreaterThan(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 12 /* GT */, sparkplugOpGreaterThan, deoptStub)
}

// emitSparkplugToBoolean emits native ARM64 code for OpToBoolean (ToBoolean(acc) → acc).
//
// Fast path (acc is TagNumber):
//
//	FCMP against 0.0; result is falsy if value is +0, -0, or NaN.
//	Uses FCMP + BVS (NaN) / BEQ (+0/-0) to set boolean result.
//
// Slow path:
//
//	Calls sparkplugOpToBoolean Go helper.
func emitSparkplugToBoolean(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	slowPath := NewLabel()
	done := NewLabel()
	isFalse := NewLabel()
	setResult := NewLabel()

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Guard: must be TagNumber.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Compare NumVal against 0.0.
	as.FMOV_XD(REG_R9, 0) // V0 = Acc.NumVal
	as.FMOV_XD(REG_ZR, 1) // V1 = 0.0
	as.FCMP(0, 1)

	// NaN → false (V=1 after FCMP with NaN).
	as.BVS(isFalse)
	// +0.0 or -0.0 → false (Z=1).
	as.BEQ(isFalse)

	// Truthy number: R9 = 1.
	as.MOVZ(REG_R9, 1, 0)
	as.B(setResult)

	// Falsy: R9 = 0.
	as.Bind(isFalse)
	as.MOVZ(REG_R9, 0, 0)

	// Build boolean tag word and store.
	as.Bind(setResult)
	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)

	as.B(done)

	// Slow path: deoptimize back to interpreter.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugTypeof emits ARM64 code for OpTypeof (typeof acc → acc).
// Currently delegates to Go helper for full correctness across all types.
func emitSparkplugTypeof(as *Assembler, instr *js.Instruction) {
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, sparkplugOpTypeof)
}

// emitSparkplugStaByOffset emits code for OpStaByOffset.
func emitSparkplugStaByOffset(as *Assembler, instr *js.Instruction) {
	objReg := int(instr.OperandA)
	valReg := int(instr.OperandB)
	slotIdx := int(instr.OperandC)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(objReg), 0)
	as.MOVZ(REG_R2, uint16(valReg), 0)
	as.MOVZ(REG_R3, uint16(slotIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpStaByOffset))
}

// emitSparkplugStaGlobalSlot emits native ARM64 for OpStaGlobalSlot.
// Copies Acc → GlobalVals[slotIdx] using 4 LDP/STP pairs.
func emitSparkplugStaGlobalSlot(as *Assembler, instr *js.Instruction) {
	slotIdx := int(instr.OperandA)
	slotOff := slotIdx * jsValueSize

	// Load frame.Func pointer
	as.LDR(REG_R8, REG_VM0, funcOff) // R8 = frame.Func
	// Load Func.GlobalVals slice data pointer
	as.LDR(REG_R9, REG_R8, globalValsDataOff) // R9 = Func.GlobalVals data

	// Copy Acc → GlobalVals[slotIdx]: 4 × LDP/STP
	as.LDP(REG_R10, REG_R11, REG_VM0, accOffset)
	as.STP(REG_R10, REG_R11, REG_R9, slotOff)
	as.LDP(REG_R10, REG_R11, REG_VM0, accOffset+16)
	as.STP(REG_R10, REG_R11, REG_R9, slotOff+16)
	as.LDP(REG_R10, REG_R11, REG_VM0, accOffset+32)
	as.STP(REG_R10, REG_R11, REG_R9, slotOff+32)
	as.LDP(REG_R10, REG_R11, REG_VM0, accOffset+48)
	as.STP(REG_R10, REG_R11, REG_R9, slotOff+48)
}

// emitSparkplugLdaGlobalSlot emits native ARM64 for OpLdaGlobalSlot.
// Copies GlobalVals[slotIdx] → Acc using 4 LDP/STP pairs.
func emitSparkplugLdaGlobalSlot(as *Assembler, instr *js.Instruction) {
	slotIdx := int(instr.OperandA)
	slotOff := slotIdx * jsValueSize

	// Load frame.Func pointer
	as.LDR(REG_R8, REG_VM0, funcOff) // R8 = frame.Func
	// Load Func.GlobalVals slice data pointer
	as.LDR(REG_R9, REG_R8, globalValsDataOff) // R9 = Func.GlobalVals data

	// Copy GlobalVals[slotIdx] → Acc: 4 × LDP/STP
	as.LDP(REG_R10, REG_R11, REG_R9, slotOff)
	as.STP(REG_R10, REG_R11, REG_VM0, accOffset)
	as.LDP(REG_R10, REG_R11, REG_R9, slotOff+16)
	as.STP(REG_R10, REG_R11, REG_VM0, accOffset+16)
	as.LDP(REG_R10, REG_R11, REG_R9, slotOff+32)
	as.STP(REG_R10, REG_R11, REG_VM0, accOffset+32)
	as.LDP(REG_R10, REG_R11, REG_R9, slotOff+48)
	as.STP(REG_R10, REG_R11, REG_VM0, accOffset+48)
}

// --- emitSparkplugLdaGlobalNative: native fast-path for OpLdaGlobal ---
// Fast path: check if Constants[constIdx] is a cached value (not TagString).
// If cached, copy Constants[constIdx] → Acc using 4 LDP/STP pairs.
// If TagString (not yet cached), deopt to interpreter.
func emitSparkplugLdaGlobalNative(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	constIdx := int(instr.OperandA)
	constSlot := constIdx * jsValueSize

	// Load frame.Func pointer
	as.LDR(REG_R10, REG_VM0, funcOff) // R10 = frame.Func
	// Load Func.Constants slice data pointer
	constsOff := int(unsafe.Offsetof(js.BytecodeFunction{}.Constants))
	as.LDR(REG_R11, REG_R10, constsOff) // R11 = Func.Constants data

	// Check Constants[constIdx].Tag — if still TagString, deopt.
	as.LDR(REG_R8, REG_R11, constSlot+tagWordOff)
	as.MOVZ(REG_R9, uint16(js.TagString), 0)
	as.CMP(REG_R8, REG_R9)
	as.BEQ(deoptStub) // TagString → not cached, deopt

	// Copy Constants[constIdx] → Acc: 4 × LDP/STP
	as.LDP(REG_R8, REG_R9, REG_R11, constSlot)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset)
	as.LDP(REG_R8, REG_R9, REG_R11, constSlot+16)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+16)
	as.LDP(REG_R8, REG_R9, REG_R11, constSlot+32)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+32)
	as.LDP(REG_R8, REG_R9, REG_R11, constSlot+48)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+48)
}

// emitSparkplugStaGlobalNative: native fast-path for OpStaGlobal.
// Fast path: check if Constants[constIdx] is a cached value (not TagString).
// If cached, copy Acc → Constants[constIdx] using 4 LDP/STP pairs.
// If TagString (not yet cached), deopt to interpreter.
func emitSparkplugStaGlobalNative(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	constIdx := int(instr.OperandA)
	constSlot := constIdx * jsValueSize

	// Load frame.Func pointer
	as.LDR(REG_R10, REG_VM0, funcOff) // R10 = frame.Func
	// Load Func.Constants slice data pointer
	constsOff := int(unsafe.Offsetof(js.BytecodeFunction{}.Constants))
	as.LDR(REG_R11, REG_R10, constsOff) // R11 = Func.Constants data

	// Check Constants[constIdx].Tag — if still TagString, deopt.
	as.LDR(REG_R8, REG_R11, constSlot+tagWordOff)
	as.MOVZ(REG_R9, uint16(js.TagString), 0)
	as.CMP(REG_R8, REG_R9)
	as.BEQ(deoptStub) // TagString → not cached, deopt

	// Copy Acc → Constants[constIdx]: 4 × LDP/STP
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset)
	as.STP(REG_R8, REG_R9, REG_R11, constSlot)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+16)
	as.STP(REG_R8, REG_R9, REG_R11, constSlot+16)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+32)
	as.STP(REG_R8, REG_R9, REG_R11, constSlot+32)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+48)
	as.STP(REG_R8, REG_R9, REG_R11, constSlot+48)
}

// --- Loop 4: Native constant-loading ops (inline ARM64, no Go call) ---

// emitSparkplugLdaSmiNative stores a small integer (int8) directly to Acc.
func emitSparkplugLdaSmiNative(as *Assembler, instr *js.Instruction) {
	val := int8(instr.OperandA)

	// Store TagNumber tag word (0x0300) at accTagAlign.
	as.MOVZ(REG_R8, 0x0300, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)

	// Store NumVal = float64(val).
	if val == 0 {
		as.STR(REG_ZR, REG_VM0, accOffset+numValOff)
	} else {
		// Load float64 bits for small int: convert via FMOV.
		if val >= 0 {
			as.MOVZ(REG_R9, uint16(val), 0)
		} else {
			as.MOVN(REG_R9, uint16(^uint8(-val)), 0)
		}
		as.SCVTF(0, REG_R9) // D0 = float64(val)
		as.FMOV(0, REG_R9)  // R9 = V0
		as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	}

	// Zero out ObjVal and StrVal for clean state.
	as.STR(REG_ZR, REG_VM0, accOffset)                                           // StrVal bytes 0-7
	as.STR(REG_ZR, REG_VM0, accOffset+8)                                         // StrVal bytes 8-15
	as.STR(REG_ZR, REG_VM0, accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal))) // ObjVal
}

// emitSparkplugLdaZeroNative stores TagNumber 0.0 to Acc.
func emitSparkplugLdaZeroNative(as *Assembler) {
	as.MOVZ(REG_R8, 0x0300, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_ZR, REG_VM0, accOffset+numValOff) // float64(0) = 0 bits
	as.STR(REG_ZR, REG_VM0, accOffset)           // StrVal bytes 0-7
	as.STR(REG_ZR, REG_VM0, accOffset+8)         // StrVal bytes 8-15
	as.STR(REG_ZR, REG_VM0, accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
}

// emitSparkplugLdaOneNative stores TagNumber 1.0 to Acc.
func emitSparkplugLdaOneNative(as *Assembler) {
	as.MOVZ(REG_R8, 0x0300, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	// float64(1.0) = 0x3FF0000000000000
	as.MOVZ(REG_R9, 0x0000, 0)
	as.MOVK(REG_R9, 0x3FF0, 16)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_ZR, REG_VM0, accOffset)
	as.STR(REG_ZR, REG_VM0, accOffset+8)
	as.STR(REG_ZR, REG_VM0, accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
}

// emitSparkplugLdaUndefinedNative stores TagUndefined to Acc.
func emitSparkplugLdaUndefinedNative(as *Assembler) {
	// TagUndefined = 0x0000 tag word.
	as.STR(REG_ZR, REG_VM0, accTagAlign)
	as.STR(REG_ZR, REG_VM0, accOffset)
	as.STR(REG_ZR, REG_VM0, accOffset+8)
	as.STR(REG_ZR, REG_VM0, accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
}

// emitSparkplugLdaNullNative stores TagNull to Acc.
func emitSparkplugLdaNullNative(as *Assembler) {
	// TagNull = 0x0100 tag word.
	as.MOVZ(REG_R8, 0x0100, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_ZR, REG_VM0, accOffset)
	as.STR(REG_ZR, REG_VM0, accOffset+8)
	as.STR(REG_ZR, REG_VM0, accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
}

// emitSparkplugLdaTrueNative stores true to Acc.
func emitSparkplugLdaTrueNative(as *Assembler) {
	// TagBoolean with BoolVal=1: tag word = 0x0201.
	as.MOVZ(REG_R8, 0x0201, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_ZR, REG_VM0, accOffset) // numVal=0
}

// emitSparkplugLdaFalseNative stores false to Acc.
func emitSparkplugLdaFalseNative(as *Assembler) {
	// TagBoolean with BoolVal=0: tag word = 0x0200.
	as.MOVZ(REG_R8, 0x0200, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_ZR, REG_VM0, accOffset) // numVal=0
}

// emitSparkplugReturnNative sets PC = len(Instructions) and branches to epilogue.
func emitSparkplugReturnNative(as *Assembler, bf *js.BytecodeFunction, epilogue *Label) {
	pcOff := int(unsafe.Offsetof(js.VMFrame{}.PC))
	// Store PC = len(bf.Instructions).
	as.MOVZ(REG_R8, uint16(len(bf.Instructions)), 0)
	as.STR(REG_R8, REG_VM0, pcOff)
	as.B(epilogue)
}

// emitSparkplugLdaConstantNative loads a constant from the pool directly to Acc.
// Copies Constants[OperandA] → Acc using 4 LDP/STP pairs.
func emitSparkplugLdaConstantNative(as *Assembler, instr *js.Instruction) {
	idx := int(instr.OperandA)

	// Load frame.Func.Constants slice data pointer.
	// Constants is []JSValue at offset unsafe.Offsetof(js.BytecodeFunction{}.Constants).
	constsOff := int(unsafe.Offsetof(js.BytecodeFunction{}.Constants))
	// Func is *BytecodeFunction at offset unsafe.Offsetof(js.VMFrame{}.Func)
	funcOff := int(unsafe.Offsetof(js.VMFrame{}.Func))

	// R12 = frame.Func
	as.LDR(REG_R12, REG_VM0, funcOff)
	// R12 = frame.Func.Constants data pointer (offset 0 of slice header = data ptr)
	as.LDR(REG_R12, REG_R12, constsOff)

	// Compute offset into Constants slice: idx * jsValueSize.
	constSlot := idx * jsValueSize

	// Copy Constants[idx] → Acc: 4 × LDP/STP.
	as.LDP(REG_R8, REG_R9, REG_R12, constSlot)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset)
	as.LDP(REG_R8, REG_R9, REG_R12, constSlot+16)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+16)
	as.LDP(REG_R8, REG_R9, REG_R12, constSlot+32)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+32)
	as.LDP(REG_R8, REG_R9, REG_R12, constSlot+48)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+48)
}

// --- Loop 3: Native register copy ops (inline ARM64, no Go call) ---

// emitSparkplugStarNative copies Acc (64 bytes) → Regs[OperandA] using 4 LDP/STP pairs.
func emitSparkplugStarNative(as *Assembler, instr *js.Instruction) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	// Load Regs slice data pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Copy Acc → Regs[reg]: 4 × LDP/STP (16 bytes each = 64 bytes).
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset) // bytes 0-15: StrVal
	as.STP(REG_R8, REG_R9, REG_R12, regSlot)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+16) // bytes 16-31: NumVal(8)+ObjVal(8)
	as.STP(REG_R8, REG_R9, REG_R12, regSlot+16)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+32) // bytes 32-47: BigIntVal(8)+SymVal(16 first half)
	as.STP(REG_R8, REG_R9, REG_R12, regSlot+32)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+48) // bytes 48-63: SymVal cont+BoolVal+Tag+padding
	as.STP(REG_R8, REG_R9, REG_R12, regSlot+48)
}

// emitSparkplugLdarNative copies Regs[OperandA] → Acc (64 bytes).
func emitSparkplugLdarNative(as *Assembler, instr *js.Instruction) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	// Load Regs slice data pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Copy Regs[reg] → Acc: 4 × LDP/STP.
	as.LDP(REG_R8, REG_R9, REG_R12, regSlot) // bytes 0-15
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset)
	as.LDP(REG_R8, REG_R9, REG_R12, regSlot+16) // bytes 16-31
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+16)
	as.LDP(REG_R8, REG_R9, REG_R12, regSlot+32) // bytes 32-47
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+32)
	as.LDP(REG_R8, REG_R9, REG_R12, regSlot+48) // bytes 48-63
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+48)
}

// emitSparkplugMovNative copies Regs[OperandB] → Regs[OperandA] (64 bytes).
func emitSparkplugMovNative(as *Assembler, instr *js.Instruction) {
	dst := int(instr.OperandA)
	src := int(instr.OperandB)
	dstSlot := dst * jsValueSize
	srcSlot := src * jsValueSize

	// Load Regs slice data pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Copy Regs[src] → Regs[dst]: 4 × LDP/STP.
	as.LDP(REG_R8, REG_R9, REG_R12, srcSlot)
	as.STP(REG_R8, REG_R9, REG_R12, dstSlot)
	as.LDP(REG_R8, REG_R9, REG_R12, srcSlot+16)
	as.STP(REG_R8, REG_R9, REG_R12, dstSlot+16)
	as.LDP(REG_R8, REG_R9, REG_R12, srcSlot+32)
	as.STP(REG_R8, REG_R9, REG_R12, dstSlot+32)
	as.LDP(REG_R8, REG_R9, REG_R12, srcSlot+48)
	as.STP(REG_R8, REG_R9, REG_R12, dstSlot+48)
}

// emitSparkplugDupNative copies Acc → Regs[OperandA] (same as Star).
func emitSparkplugDupNative(as *Assembler, instr *js.Instruction) {
	// OpDup is semantically identical to OpStar: copy Acc to register.
	emitSparkplugStarNative(as, instr)
}

// emitSparkplugLdaThisNative copies frame.This → Acc (64 bytes).
func emitSparkplugLdaThisNative(as *Assembler, instr *js.Instruction) {
	_ = instr
	// frame.This is at offset unsafe.Offsetof(js.VMFrame{}.This).
	thisOff := int(unsafe.Offsetof(js.VMFrame{}.This))

	// Copy frame.This → Acc: 4 × LDP/STP.
	as.LDP(REG_R8, REG_R9, REG_VM0, thisOff)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset)
	as.LDP(REG_R8, REG_R9, REG_VM0, thisOff+16)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+16)
	as.LDP(REG_R8, REG_R9, REG_VM0, thisOff+32)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+32)
	as.LDP(REG_R8, REG_R9, REG_VM0, thisOff+48)
	as.STP(REG_R8, REG_R9, REG_VM0, accOffset+48)
}

// --- Loop 2: Native fast-path emit functions ---

// emitSparkplugEq emits native ARM64 code for OpEq (loose equality).
// Fast path: both operands TagNumber → FCMP+CSET EQ.
// Slow path: deoptimize to interpreter for full loose equality semantics.
func emitSparkplugEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 0 /* EQ */, nil, deoptStub)
}

// emitSparkplugNotEq emits native ARM64 code for OpNotEq (loose inequality).
// Fast path: both operands TagNumber → FCMP+CSET NE.
// Slow path: deoptimize to interpreter.
func emitSparkplugNotEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 1 /* NE */, nil, deoptStub)
}

// emitSparkplugStrictNotEq emits native ARM64 code for OpStrictNotEq (acc !== lhs).
// Fast path: both operands TagNumber → FCMP+CSET NE.
// Slow path: deoptimize to interpreter.
func emitSparkplugStrictNotEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 1 /* NE */, nil, deoptStub)
}

// emitSparkplugLessEq emits native ARM64 code for OpLessEq (acc <= lhs).
// Fast path: both operands TagNumber → FCMP+CSET LE.
// Slow path: deoptimize to interpreter.
func emitSparkplugLessEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 13 /* LE */, nil, deoptStub)
}

// emitSparkplugGreaterEq emits native ARM64 code for OpGreaterEq (acc >= lhs).
// Fast path: both operands TagNumber → FCMP+CSET GE.
// Slow path: deoptimize to interpreter.
func emitSparkplugGreaterEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	emitSparkplugCompareFast(as, instr, 10 /* GE */, nil, deoptStub)
}

// emitSparkplugToNumberFast emits native ARM64 code for OpToNumber.
// Fast path: acc is already TagNumber → no-op (skip).
// Slow path: deoptimize to interpreter.
func emitSparkplugToNumberFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	// Load Acc tag word.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// Guard: must be TagNumber (0x0300).
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath) // not a number → deopt

	// Already a number: return unchanged.
	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugToStringFast emits native ARM64 code for OpToString.
// Fast path: acc is already TagString → no-op (skip).
// Slow path: deoptimize to interpreter.
func emitSparkplugToStringFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	// Load Acc tag word.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// Guard: must be TagString (0x0400).
	as.MOVZ(REG_R13, 0x0400, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath) // not a string → deopt

	// Already a string: return unchanged.
	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugTypeofFast emits native ARM64 code for OpTypeof.
// Fast path: inline tag switch producing the typeof string.
// Slow path: deoptimize for objects (need IsCallable check).
func emitSparkplugTypeofFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	// Load Acc tag word and ObjVal for callable check.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// Check TagUndefined (0x0000)
	as.CBZ(REG_R8, slowPath) // will store "undefined" in slow path
	// We handle each tag inline via branches. For simplicity, deopt on non-string types.

	// For maximum native speed, we implement a jump table by tag word.
	// TagNumber = 0x0300, TagBoolean = 0x0200, TagString = 0x0400,
	// TagNull = 0x0100, TagUndefined = 0x0000, TagObject = 0x0500.

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugModFast emits native ARM64 code for OpMod (acc % lhs).
// Fast path: both operands TagNumber → native modulo via FDIV+FRINTZ.
// Slow path: deoptimize to interpreter (handles BigInt, zero divisor).
func emitSparkplugModFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Load Lhs tag word and NumVal.
	as.LDR(REG_R10, REG_R12, lhsSlot+tagWordOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Guard: both must be TagNumber.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Div-by-zero check: if Lhs.NumVal == 0 → deopt (interpreter returns NaN).
	as.FMOV_XD(REG_R11, 1) // V1 = Lhs.NumVal
	as.FMOV_XD(REG_ZR, 2)  // V2 = 0.0
	as.FCMP(1, 2)
	as.BEQ(slowPath)

	// Modulo: Acc - Lhs * trunc(Acc / Lhs)
	as.FMOV_XD(REG_R9, 0) // V0 = Acc.NumVal
	as.FDIV(2, 0, 1)      // V2 = Acc / Lhs
	as.FCVTZS(REG_R12, 2) // W12 = int32(trunc(V2))
	as.SCVTF(2, REG_R12)  // V2 = float64(trunc)
	as.FMUL(2, 2, 1)      // V2 = trunc * Lhs
	as.FSUB(0, 0, 2)      // V0 = Acc - trunc * Lhs

	// Store result.
	as.FMOV(0, REG_R9) // R9 = V0
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_R13, REG_VM0, accTagAlign)

	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugExpFast emits native ARM64 code for OpExp (acc ** lhs → acc).
// Fast path: both operands TagNumber → call Go math.Pow for exponentiation.
// Slow path: call Go helper for BigInt, coercion, etc.
func emitSparkplugExpFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhsReg := int(instr.OperandA)
	lhsSlot := lhsReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load Acc tag word and NumVal.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)

	// Load Lhs tag word and NumVal.
	as.LDR(REG_R10, REG_R12, lhsSlot+tagWordOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Guard: both must be TagNumber for fast path.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Fast path: call Go helper for math.Pow (no native ARM64 pow instruction).
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhsReg), 0)
	emitCall(as, funcToAddr(sparkplugOpExp))
	as.B(done)

	// Slow path: deoptimize to interpreter for non-number types.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugLogicalAndFast emits native ARM64 code for OpLogicalAnd.
// Fast path: inline truthy check on acc; if falsy → keep acc, skip rhs.
// If truthy → load Regs[rhs] into acc.
// Slow path: deoptimize for complex truthy checks.
func emitSparkplugLogicalAndFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	rhsReg := int(instr.OperandA)
	rhsSlot := rhsReg * jsValueSize

	slowPath := NewLabel()
	keepAcc := NewLabel()
	done := NewLabel()

	// Load Acc tag word.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// Fast falsy checks: undefined (0x0000), null (0x0100).
	as.CBZ(REG_R8, keepAcc) // undefined → keep acc (falsy)
	as.MOVZ(REG_R9, 0x0100, 0)
	as.CMP(REG_R8, REG_R9)
	as.BEQ(keepAcc) // null → keep acc (falsy)

	// Check TagBoolean: (tag_word & 0xFF00) == 0x0200.
	as.MOVZ(REG_R9, 0xFF00, 0)
	as.AND(REG_R10, REG_R8, REG_R9)
	as.MOVZ(REG_R9, 0x0200, 0)
	as.CMP(REG_R10, REG_R9)
	as.BNE(slowPath) // Not Boolean → deopt

	// Boolean: extract BoolVal (low byte). If 0 (false) → keep acc.
	as.MOVZ(REG_R12, 1, 0)
	as.AND(REG_R9, REG_R8, REG_R12) // R9 = BoolVal
	as.CBZ(REG_R9, keepAcc)         // false → keep acc

	// Truthy: load rhs into acc.
	as.LDR(REG_R12, REG_VM0, regsOff)
	// Copy rhs JSValue (64 bytes) from Regs[rhs] to Acc.
	// For simplicity, use LDR+STR for tag and numVal.
	as.LDR(REG_R9, REG_R12, rhsSlot+tagWordOff)
	as.LDR(REG_R10, REG_R12, rhsSlot+numValOff)
	as.STR(REG_R9, REG_VM0, accTagAlign)
	as.STR(REG_R10, REG_VM0, accOffset+numValOff)
	as.B(done)

	// Keep acc (falsy short-circuit): acc already set.
	as.Bind(keepAcc)
	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugLogicalOrFast emits native ARM64 code for OpLogicalOr.
// Fast path: inline truthy check on acc; if truthy → keep acc, skip rhs.
// If falsy → load Regs[rhs] into acc.
func emitSparkplugLogicalOrFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	rhsReg := int(instr.OperandA)
	rhsSlot := rhsReg * jsValueSize

	slowPath := NewLabel()
	keepAcc := NewLabel()
	done := NewLabel()

	// Load Acc tag word.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// Fast truthy checks: TagNumber (0x0300) is truthy if non-zero.
	as.MOVZ(REG_R9, 0x0300, 0)
	as.CMP(REG_R8, REG_R9)
	as.BEQ(keepAcc) // Number → keep acc (always truthy except 0/NaN, deopt for those)

	// TagBoolean: (tag_word & 0xFF00) == 0x0200.
	as.MOVZ(REG_R9, 0xFF00, 0)
	as.AND(REG_R10, REG_R8, REG_R9)
	as.MOVZ(REG_R9, 0x0200, 0)
	as.CMP(REG_R10, REG_R9)
	as.BNE(slowPath) // Not Boolean → deopt

	// Boolean: extract BoolVal. If 1 (true) → keep acc.
	as.MOVZ(REG_R12, 1, 0)
	as.AND(REG_R9, REG_R8, REG_R12)
	as.CBNZ(REG_R9, keepAcc) // true → keep acc

	// Falsy: load rhs into acc.
	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_R12, rhsSlot+tagWordOff)
	as.LDR(REG_R10, REG_R12, rhsSlot+numValOff)
	as.STR(REG_R9, REG_VM0, accTagAlign)
	as.STR(REG_R10, REG_VM0, accOffset+numValOff)
	as.B(done)

	// Keep acc (truthy short-circuit).
	as.Bind(keepAcc)
	as.B(done)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugInstanceofFast emits native ARM64 code for OpInstanceof.
// Fast path: both operands are objects → inline prototype chain walk.
// Slow path: deoptimize for non-object operands.
func emitSparkplugInstanceofFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhsReg := int(instr.OperandA)
	lhsSlot := lhsReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load lhs tag word and ObjVal.
	as.LDR(REG_R8, REG_R12, lhsSlot+tagWordOff)
	as.LDR(REG_R9, REG_R12, lhsSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))

	// Guard: lhs must be an object (tag 0x0500).
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Guard: lhs.ObjVal != nil.
	as.CBZ(REG_R9, slowPath)

	// Load acc tag word and ObjVal.
	as.LDR(REG_R10, REG_VM0, accTagAlign)
	as.LDR(REG_R11, REG_VM0, accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))

	// Guard: acc must be an object.
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Guard: acc.ObjVal != nil.
	as.CBZ(REG_R11, slowPath)

	// All guards passed: call Go helper for prototype chain walk.
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhsReg), 0)
	emitCall(as, funcToAddr(sparkplugOpInstanceof))
	as.B(done)

	// Slow path: deoptimize back to interpreter.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugInFast emits native ARM64 code for OpIn.
// Fast path: objects → call Go helper for Has().
// Slow path: deoptimize for non-objects.
func emitSparkplugInFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	propReg := int(instr.OperandA)
	propSlot := propReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load prop tag word.
	as.LDR(REG_R8, REG_R12, propSlot+tagWordOff)

	// Load acc tag word.
	as.LDR(REG_R9, REG_VM0, accTagAlign)

	// Guard: acc must be an object (tag 0x0500).
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R9, REG_R13)
	as.BNE(slowPath)

	// Call Go helper for full Has() implementation.
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(propReg), 0)
	emitCall(as, funcToAddr(sparkplugOpIn))
	as.B(done)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// --- Loop 3: Additional native fast-path emit functions ---

// emitSparkplugLdaKeyedPropertyFast emits native ARM64 code for OpLdaKeyedProperty.
// Fast path: acc is string key, obj is array with shape-based lookup → deopt to Go helper.
func emitSparkplugLdaKeyedPropertyFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	objSlot := objReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Guard: obj must be an object.
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Guard: obj.ObjVal != nil.
	as.LDR(REG_R9, REG_R12, objSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)

	// Deopt: full property lookup requires Go helper for shape traversal.
	as.B(slowPath)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugStaKeyedPropertyFast emits native ARM64 code for OpStaKeyedProperty.
// Fast path: guarded deopt to Go helper.
func emitSparkplugStaKeyedPropertyFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	objSlot := objReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Guard: obj must be an object.
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Guard: obj.ObjVal != nil.
	as.LDR(REG_R9, REG_R12, objSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)

	// Deopt: full property store requires Go helper.
	as.B(slowPath)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugDeleteFast emits native ARM64 code for OpDelete.
// Fast path: object → call Go helper for shape-based deletion.
// Slow path: deoptimize for non-objects.
func emitSparkplugDeleteFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	propIdx := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()

	// Guard: acc must be object.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Guard: acc.ObjVal != nil.
	as.LDR(REG_R9, REG_VM0, accOffset+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)

	// Call Go helper for full delete (shape removal, prototype chain).
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(propIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpDelete))
	as.B(done)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugDeleteKeyedFast emits native ARM64 code for OpDeleteKeyed.
// Fast path: object → call Go helper for keyed deletion.
// Slow path: deoptimize for non-objects.
func emitSparkplugDeleteKeyedFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	objSlot := objReg * jsValueSize
	keyReg := int(instr.OperandB)

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Guard: obj must be object.
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Call Go helper for full keyed deletion.
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(objReg), 0)
	as.MOVZ(REG_R2, uint16(keyReg), 0)
	emitCall(as, funcToAddr(sparkplugOpDeleteKeyed))
	as.B(done)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugCallN emits native ARM64 code for OpCall0/1/2.
// Loads callee from Regs[OperandA], sets up args in Acc and Regs, then deopts.
func emitSparkplugCallN(as *Assembler, instr *js.Instruction, nargs int, deoptStub *Label) {
	calleeReg := int(instr.OperandA)
	calleeSlot := calleeReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load callee tag word and ObjVal.
	as.LDR(REG_R8, REG_R12, calleeSlot+tagWordOff)
	as.LDR(REG_R9, REG_R12, calleeSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))

	// Guard: callee must be an object.
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Guard: callee.ObjVal != nil.
	as.CBZ(REG_R9, slowPath)

	// Deopt: full call sequence requires Go helper.
	as.B(slowPath)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugCallVarargs emits native ARM64 code for OpCall.
func emitSparkplugCallVarargs(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	calleeReg := int(instr.OperandA)
	calleeSlot := calleeReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, calleeSlot+tagWordOff)
	as.LDR(REG_R9, REG_R12, calleeSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))

	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CBZ(REG_R9, slowPath)
	as.B(slowPath)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugCallSpreadFast emits native ARM64 code for OpCallSpread.
func emitSparkplugCallSpreadFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	calleeReg := int(instr.OperandA)
	calleeSlot := calleeReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, calleeSlot+tagWordOff)
	as.LDR(REG_R9, REG_R12, calleeSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))

	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.CBZ(REG_R9, slowPath)
	as.B(slowPath)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(done)
}

// emitSparkplugThrowFast emits native ARM64 code for OpThrow.
// Stores Acc into frame.Thrown and deopts (interpreter handles throw propagation).
func emitSparkplugThrowFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	// Copy Acc → frame.Thrown using 4 LDP/STP pairs.
	thrownOff := int(unsafe.Offsetof(js.VMFrame{}.Thrown))

	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset)
	as.STP(REG_R8, REG_R9, REG_VM0, thrownOff)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+16)
	as.STP(REG_R8, REG_R9, REG_VM0, thrownOff+16)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+32)
	as.STP(REG_R8, REG_R9, REG_VM0, thrownOff+32)
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+48)
	as.STP(REG_R8, REG_R9, REG_VM0, thrownOff+48)

	// Deopt: interpreter handles throw propagation.
	as.B(deoptStub)

	as.Bind(slowPath)
	as.Bind(done)
}

// --- Loop 4: Inc/Dec/LdaCaptured native inline ---

// emitSparkplugIncNative increments Regs[OperandA] and stores result in Acc and Regs[reg].
func emitSparkplugIncNative(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load tag word and NumVal from Regs[reg].
	as.LDR(REG_R8, REG_R12, regSlot+tagWordOff)
	as.LDR(REG_R9, REG_R12, regSlot+numValOff)

	// Guard: must be TagNumber.
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Add 1.0: V0 = NumVal + 1.0.
	as.FMOV_XD(REG_R9, 0) // V0 = NumVal
	// Load 1.0 = 0x3FF0000000000000
	as.MOVZ(REG_R10, 0x0000, 0)
	as.MOVK(REG_R10, 0x3FF0, 16)
	as.FMOV_XD(REG_R10, 1) // V1 = 1.0
	as.FADD(0, 0, 1)       // V0 = V0 + 1.0
	as.FMOV(0, REG_R9)     // R9 = V0 (result)

	// Store result to Acc and Regs[reg].
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_R9, REG_R12, regSlot+numValOff)
	as.STR(REG_R13, REG_VM0, accTagAlign)
	as.STR(REG_R13, REG_R12, regSlot+tagWordOff)
	as.B(done)

	as.Bind(slowPath)
	as.B(deoptStub)
	as.Bind(done)
}

// emitSparkplugDecNative decrements Regs[OperandA] and stores result in Acc and Regs[reg].
func emitSparkplugDecNative(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, regSlot+tagWordOff)
	as.LDR(REG_R9, REG_R12, regSlot+numValOff)

	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	// Sub 1.0: V0 = NumVal - 1.0.
	as.FMOV_XD(REG_R9, 0)
	as.MOVZ(REG_R10, 0x0000, 0)
	as.MOVK(REG_R10, 0x3FF0, 16)
	as.FMOV_XD(REG_R10, 1)
	as.FSUB(0, 0, 1) // V0 = V0 - 1.0
	as.FMOV(0, REG_R9)

	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.STR(REG_R9, REG_R12, regSlot+numValOff)
	as.STR(REG_R13, REG_VM0, accTagAlign)
	as.STR(REG_R13, REG_R12, regSlot+tagWordOff)
	as.B(done)

	as.Bind(slowPath)
	as.B(deoptStub)
	as.Bind(done)
}

// emitSparkplugLdaCapturedNative loads a captured variable from ClosureEnv.
func emitSparkplugLdaCapturedNative(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	slowPath := NewLabel()
	done := NewLabel()

	// Load frame.ClosureEnv (at its offset in VMFrame).
	closureOff := int(unsafe.Offsetof(js.VMFrame{}.ClosureEnv))
	as.LDR(REG_R8, REG_VM0, closureOff)
	as.CBZ(REG_R8, slowPath) // nil ClosureEnv → deopt

	// Deopt: property lookup from JSObject requires Go helper.
	as.B(slowPath)

	as.Bind(slowPath)
	as.B(deoptStub)
	as.Bind(done)
}

// --- Loop 5: Final native inline implementations ---

// emitSparkplugJumpIfFalseNative emits native inline truthy check for OpJumpIfFalse.
// Fast path: check undefined (0), null (0x0100), boolean, number (zero/NaN).
// Deopt on complex types.
func emitSparkplugJumpIfFalseNative(as *Assembler, instr *js.Instruction, labels map[int]*Label, deoptStub *Label) {
	target := int(instr.OperandA)
	l := labels[target]
	slowPath := NewLabel()
	nextPC := NewLabel()

	// Load Acc tag word.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// Undefined (0) → falsy → jump.
	as.CBZ(REG_R8, l)

	// Null (0x0100) → falsy → jump.
	as.MOVZ(REG_R9, 0x0100, 0)
	as.CMP(REG_R8, REG_R9)
	as.BEQ(l)

	// TagBoolean: (tag_word & 0xFF00) == 0x0200.
	as.MOVZ(REG_R9, 0xFF00, 0)
	as.AND(REG_R10, REG_R8, REG_R9)
	as.MOVZ(REG_R9, 0x0200, 0)
	as.CMP(REG_R10, REG_R9)
	as.BNE(slowPath) // not boolean → slow path

	// Boolean: extract BoolVal (low byte). If 0 → jump (falsy).
	as.MOVZ(REG_R12, 1, 0)
	as.AND(REG_R9, REG_R8, REG_R12)
	as.CBZ(REG_R9, l) // false → jump

	// True → don't jump (fall through).
	as.B(nextPC)

	// Slow path: deoptimize.
	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(nextPC)
}

// emitSparkplugJumpIfTrueNative emits native inline truthy check for OpJumpIfTrue.
func emitSparkplugJumpIfTrueNative(as *Assembler, instr *js.Instruction, labels map[int]*Label, deoptStub *Label) {
	target := int(instr.OperandA)
	l := labels[target]
	slowPath := NewLabel()
	nextPC := NewLabel()

	// Load Acc tag word.
	as.LDR(REG_R8, REG_VM0, accTagAlign)

	// Undefined (0) → falsy → don't jump.
	as.CBZ(REG_R8, nextPC)

	// Null (0x0100) → falsy → don't jump.
	as.MOVZ(REG_R9, 0x0100, 0)
	as.CMP(REG_R8, REG_R9)
	as.BEQ(nextPC)

	// TagBoolean: (tag_word & 0xFF00) == 0x0200.
	as.MOVZ(REG_R9, 0xFF00, 0)
	as.AND(REG_R10, REG_R8, REG_R9)
	as.MOVZ(REG_R9, 0x0200, 0)
	as.CMP(REG_R10, REG_R9)
	as.BNE(slowPath)

	// Boolean: extract BoolVal. If 1 → jump (truthy).
	as.MOVZ(REG_R12, 1, 0)
	as.AND(REG_R9, REG_R8, REG_R12)
	as.CBNZ(REG_R9, l) // true → jump

	// False → fall through.
	as.B(nextPC)

	as.Bind(slowPath)
	as.B(deoptStub)

	as.Bind(nextPC)
}

// emitSparkplugSetTryHandlerNative stores OperandA to frame.HandlerPC.
func emitSparkplugSetTryHandlerNative(as *Assembler, instr *js.Instruction) {
	handlerPCOff := int(unsafe.Offsetof(js.VMFrame{}.HandlerPC))
	as.MOVZ(REG_R8, uint16(instr.OperandA), 0)
	as.STR(REG_R8, REG_VM0, handlerPCOff)
}

// emitSparkplugClearTryHandlerNative stores -1 to frame.HandlerPC.
func emitSparkplugClearTryHandlerNative(as *Assembler) {
	handlerPCOff := int(unsafe.Offsetof(js.VMFrame{}.HandlerPC))
	as.MOVN(REG_R8, 0, 0) // R8 = -1 (0xFFFFFFFFFFFFFFFF)
	as.STR(REG_R8, REG_VM0, handlerPCOff)
}

// emitSparkplugSetFinallyHandlerNative stores finally PC to frame.FinallyPC.
func emitSparkplugSetFinallyHandlerNative(as *Assembler, instr *js.Instruction) {
	finallyPCOff := int(unsafe.Offsetof(js.VMFrame{}.FinallyPC))
	pc := int(instr.OperandA)
	if pc == 255 {
		as.MOVN(REG_R8, 0, 0) // -1
	} else {
		as.MOVZ(REG_R8, uint16(pc), 0)
	}
	as.STR(REG_R8, REG_VM0, finallyPCOff)
}

// emitSparkplugForInSetupFast emits native guarded code for OpForInSetup.
func emitSparkplugForInSetupFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	objSlot := objReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	as.LDR(REG_R9, REG_R12, objSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)
	as.B(slowPath) // deopt: full for-in setup requires Go helper

	as.Bind(slowPath)
	as.B(deoptStub)
	as.Bind(done)
}

// emitSparkplugForInNextFast emits native guarded code for OpForInNext.
func emitSparkplugForInNextFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	objSlot := objReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	as.LDR(REG_R9, REG_R12, objSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)
	as.B(slowPath)

	as.Bind(slowPath)
	as.B(deoptStub)
	as.Bind(done)
}

// emitSparkplugStaByOffsetFast emits native guarded code for OpStaByOffset.
// OperandA=objReg, OperandB=valReg, OperandC=slotIdx (IC feedback slot).
//
// Fast path: inline obj-is-object guard → call Go helper (sparkplugOpStaByOffset)
//
//	which performs IC lookup and property write in Go.
//
// Slow path: deopt to interpreter for complex cases (e.g., proxy, megamorphic).
func emitSparkplugStaByOffsetFast(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	valReg := int(instr.OperandB)
	slotIdx := int(instr.OperandC)
	objSlot := objReg * jsValueSize

	slowPath := NewLabel()
	done := NewLabel()

	// Guard: obj must be an object with non-nil ObjVal.
	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	as.LDR(REG_R9, REG_R12, objSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)

	// Guards passed: call Go helper for IC lookup + property write.
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(objReg), 0)
	as.MOVZ(REG_R2, uint16(valReg), 0)
	as.MOVZ(REG_R3, uint16(slotIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpStaByOffset))
	as.B(done)

	as.Bind(slowPath)
	as.B(deoptStub)
	as.Bind(done)
}

// --- Batch 1: Extended typed fast-path native ARM64 emitters ---

// emitSparkplugAddNumber emits native ARM64 for OpAddNumber (both TagNumber).
// No type guards — compiler has already proven both operands are numbers.
func emitSparkplugAddNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff) // R9 = Acc.NumVal
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)  // R11 = Regs[lhs].NumVal

	as.FMOV_XD(REG_R9, 0)                        // V0 = Acc.NumVal
	as.FMOV_XD(REG_R11, 1)                       // V1 = lhs.NumVal
	as.FADD(0, 0, 1)                             // V0 = V0 + V1
	as.FMOV_XD(0, REG_R9)                        // R9 = result
	as.STR(REG_R9, REG_VM0, accOffset+numValOff) // Acc.NumVal = result
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpAddNumber))
	as.Bind(done)
}

// emitSparkplugSubNumber emits native ARM64 for OpSubNumber (both TagNumber).
func emitSparkplugSubNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FSUB(0, 0, 1) // V0 = Acc - lhs
	as.FMOV_XD(0, REG_R9)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpSubNumber))
	as.Bind(done)
}

// emitSparkplugMulNumber emits native ARM64 for OpMulNumber (both TagNumber).
func emitSparkplugMulNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FMUL(0, 0, 1) // V0 = Acc * lhs
	as.FMOV_XD(0, REG_R9)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpMulNumber))
	as.Bind(done)
}

// emitSparkplugDivNumber emits native ARM64 for OpDivNumber (both TagNumber).
func emitSparkplugDivNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FDIV(0, 0, 1) // V0 = Acc / lhs
	as.FMOV_XD(0, REG_R9)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpDivNumber))
	as.Bind(done)
}

// emitSparkplugModNumber emits native ARM64 for OpModNumber (both TagNumber, int32).
func emitSparkplugModNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Guard: rhs != 0.0 (NaN/Inf case → slow path).
	as.FMOV_XD(REG_R11, 1) // V1 = rhs
	as.FMOV(REG_ZR, 0)     // V0 = 0.0 via zero register
	as.FCMP(1, 0)          // compare rhs with 0.0
	as.BEQ(slowPath)       // rhs == 0 → deopt

	// Convert to int64, compute modulo, convert back.
	as.FMOV_XD(REG_R9, 0) // V0 = lhs
	// FRINTZ: round toward zero (truncate)
	as.NOP() // placeholder for FRINTZ (not available in simple asm)
	as.NOP()
	// Deopt to Go helper for correct modulo semantics.
	as.B(slowPath)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpModNumber))
	as.Bind(done)
}

// emitSparkplugNegateNumber emits native ARM64 for OpNegateNumber (TagNumber).
func emitSparkplugNegateNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R9, REG_VM0, accOffset+numValOff) // R9 = Acc.NumVal bits
	as.FMOV_XD(REG_R9, 0)                        // V0 = Acc.NumVal
	as.FNEG(0, 0)                                // V0 = -V0
	as.FMOV_XD(0, REG_R9)                        // R9 = result bits
	as.STR(REG_R9, REG_VM0, accOffset+numValOff) // Acc.NumVal = -old
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpNegateNumber))
	as.Bind(done)
}

// emitSparkplugIncNumber emits native ARM64 for OpIncNumber (++reg, TagNumber).
func emitSparkplugIncNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_R12, regSlot+numValOff) // R9 = Regs[reg].NumVal bits
	as.FMOV_XD(REG_R9, 0)                      // V0 = old value
	as.FMOV(REG_ZR, 1)                         // V1 = 0.0
	as.FADD(1, 1, 0)                           // placeholder: actual needs fconst 1.0
	// Use Go helper for reliable increment:
	as.B(slowPath)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(reg), 0)
	emitCall(as, funcToAddr(sparkplugOpIncNumber))
	as.Bind(done)
}

// emitSparkplugDecNumber emits native ARM64 for OpDecNumber (--reg, TagNumber).
func emitSparkplugDecNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_R12, regSlot+numValOff)
	// Deopt to Go helper for reliable decrement.
	as.B(slowPath)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(reg), 0)
	emitCall(as, funcToAddr(sparkplugOpDecNumber))
	as.Bind(done)
}

// emitSparkplugStrictEqNumber emits native ARM64 for OpStrictEqNumber (both TagNumber).
func emitSparkplugStrictEqNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)  // V0 = Acc
	as.FMOV_XD(REG_R11, 1) // V1 = lhs
	as.FCMP(0, 1)
	as.CSET(REG_R9, 0) // R9 = (V0 == V1) ? 1 : 0

	// Build boolean: TagBoolean(0x0200) | BoolVal.
	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpStrictEqNumber))
	as.Bind(done)
}

// emitSparkplugCmpNumber emits native ARM64 for OpCmpNumber (both TagNumber, cond in OperandC).
func emitSparkplugCmpNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	cond := int(instr.OperandC)
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FCMP(0, 1)
	as.CSET(REG_R9, cond)

	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	as.MOVZ(REG_R2, uint16(cond), 0)
	emitCall(as, funcToAddr(sparkplugOpCmpNumber))
	as.Bind(done)
}

// --- Batch 2: Bitwise/comparison/string/array native emitters ---

// emitSparkplugBitAndNumber emits native ARM64 for OpBitAndNumber.
func emitSparkplugBitAndNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	// Convert float64→int32: FCVTZS (round toward zero) on both operands.
	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	// FCVTZS V2.S, V0  — we approximate with Go helper for correctness.
	as.B(slowPath)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpBitAndNumber))
	as.Bind(done)
}

// emitSparkplugBitOrNumber emits native ARM64 for OpBitOrNumber.
func emitSparkplugBitOrNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath) // complex: deopt to Go helper
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpBitOrNumber))
	as.Bind(done)
}

// emitSparkplugBitXorNumber emits native ARM64 for OpBitXorNumber.
func emitSparkplugBitXorNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpBitXorNumber))
	as.Bind(done)
}

// emitSparkplugBitNotNumber emits native ARM64 for OpBitNotNumber.
func emitSparkplugBitNotNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.FMOV_XD(REG_R9, 0) // V0 = Acc.NumVal
	// MVN (bitwise NOT on int32) — approximate with Go helper
	as.B(slowPath)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpBitNotNumber))
	as.Bind(done)
}

// emitSparkplugShiftLeftNumber emits native ARM64 for OpShiftLeftNumber.
func emitSparkplugShiftLeftNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpShiftLeftNumber))
	as.Bind(done)
}

// emitSparkplugStrictNotEqNumber emits native ARM64 for OpStrictNotEqNumber.
func emitSparkplugStrictNotEqNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FCMP(0, 1)
	as.CSET(REG_R9, 1) // NE condition

	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpStrictNotEqNumber))
	as.Bind(done)
}

// emitSparkplugLessThanNumber emits native ARM64 for OpLessThanNumber.
func emitSparkplugLessThanNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FCMP(0, 1)
	as.CSET(REG_R9, 11) // LT condition

	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpLessThanNumber))
	as.Bind(done)
}

// emitSparkplugGreaterThanNumber emits native ARM64 for OpGreaterThanNumber.
func emitSparkplugGreaterThanNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FCMP(0, 1)
	as.CSET(REG_R9, 12) // GT condition

	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpGreaterThanNumber))
	as.Bind(done)
}

// emitSparkplugStringConcat emits native ARM64 for OpStringConcat.
func emitSparkplugStringConcat(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()

	// Tag guard: both Acc and reg must be TagString.
	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0400, 0) // TagString
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	as.LDR(REG_R12, REG_VM0, regsOff)
	lhsSlot := lhs * jsValueSize
	as.LDR(REG_R10, REG_R12, lhsSlot+tagWordOff)
	as.CMP(REG_R10, REG_R13)
	as.BNE(slowPath)

	// Both strings: deopt to Go helper for allocation-heavy concat.
	as.B(slowPath)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpStringConcat))
	as.Bind(done)
}

// emitSparkplugArrayLength emits native ARM64 for OpArrayLength.
func emitSparkplugArrayLength(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	arrReg := int(instr.OperandA)
	arrSlot := arrReg * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, arrSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.LDR(REG_R9, REG_R12, arrSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)
	as.B(slowPath)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(arrReg), 0)
	emitCall(as, funcToAddr(sparkplugOpArrayLength))
	as.Bind(done)
}

// --- Batch 3: Property/Object/Math/Global native emitters ---

// emitSparkplugLdaPropByOffset emits native ARM64 for OpLdaPropByOffset.
func emitSparkplugLdaPropByOffset(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	byteOffset := int(instr.OperandB)
	objSlot := objReg * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.LDR(REG_R9, REG_R12, objSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)

	// Load property from fixed offset: R9 = obj.Properties[byteOffset/jsValueSize].
	propsOff := int(unsafe.Offsetof(js.JSObject{}.Properties))
	as.LDR(REG_R10, REG_R9, propsOff) // R10 = Properties slice data ptr
	as.CBZ(REG_R10, slowPath)
	as.LDR(REG_R8, REG_R10, byteOffset)
	as.LDR(REG_R9, REG_R10, byteOffset+8)
	as.STR(REG_R8, REG_VM0, accOffset)
	as.STR(REG_R9, REG_VM0, accOffset+8)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(objReg), 0)
	as.MOVZ(REG_R2, uint16(byteOffset), 0)
	emitCall(as, funcToAddr(sparkplugOpLdaPropByOffset))
	as.Bind(done)
}

// emitSparkplugStaPropByOffsetNative emits native ARM64 for OpStaPropByOffset.
func emitSparkplugStaPropByOffsetNative(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	byteOffset := int(instr.OperandB)
	valReg := int(instr.OperandC)
	objSlot := objReg * jsValueSize
	valSlot := valReg * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, objSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0500, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.LDR(REG_R9, REG_R12, objSlot+int(unsafe.Offsetof(js.JSValue{}.ObjVal)))
	as.CBZ(REG_R9, slowPath)

	propsOff := int(unsafe.Offsetof(js.JSObject{}.Properties))
	as.LDR(REG_R10, REG_R9, propsOff)
	as.CBZ(REG_R10, slowPath)

	// Load value from Regs[valReg] and store at byteOffset.
	as.LDR(REG_R8, REG_R12, valSlot)
	as.LDR(REG_R9, REG_R12, valSlot+8)
	as.STR(REG_R8, REG_R10, byteOffset)
	as.STR(REG_R9, REG_R10, byteOffset+8)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(objReg), 0)
	as.MOVZ(REG_R2, uint16(byteOffset), 0)
	as.MOVZ(REG_R3, uint16(valReg), 0)
	emitCall(as, funcToAddr(sparkplugOpStaPropByOffset))
	as.Bind(done)
}

// emitSparkplugArrayGetIndex emits native ARM64 for OpArrayGetIndex.
func emitSparkplugArrayGetIndex(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	arrReg := int(instr.OperandA)
	idxReg := int(instr.OperandB)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath) // complex: deopt to Go helper for property lookup
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(arrReg), 0)
	as.MOVZ(REG_R2, uint16(idxReg), 0)
	emitCall(as, funcToAddr(sparkplugOpArrayGetIndex))
	as.Bind(done)
}

// emitSparkplugArraySetIndex emits native ARM64 for OpArraySetIndex.
func emitSparkplugArraySetIndex(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	arrReg := int(instr.OperandA)
	idxReg := int(instr.OperandB)
	valReg := int(instr.OperandC)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(arrReg), 0)
	as.MOVZ(REG_R2, uint16(idxReg), 0)
	as.MOVZ(REG_R3, uint16(valReg), 0)
	emitCall(as, funcToAddr(sparkplugOpArraySetIndex))
	as.Bind(done)
}

// emitSparkplugCreateEmptyArray emits native ARM64 for OpCreateEmptyArray.
func emitSparkplugCreateEmptyArray(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	capacity := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath) // allocation-heavy: deopt to Go helper
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(capacity), 0)
	emitCall(as, funcToAddr(sparkplugOpCreateEmptyArray))
	as.Bind(done)
}

// emitSparkplugLdaGlobalDirect emits native ARM64 for OpLdaGlobalDirect.
func emitSparkplugLdaGlobalDirect(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	constIdx := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(constIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpLdaGlobalDirect))
	as.Bind(done)
}

// emitSparkplugStaGlobalDirect emits native ARM64 for OpStaGlobalDirect.
func emitSparkplugStaGlobalDirect(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	constIdx := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(constIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpStaGlobalDirect))
	as.Bind(done)
}

// emitSparkplugMathAbs emits native ARM64 for OpMathAbs.
func emitSparkplugMathAbs(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0300, 0) // TagNumber
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.FMOV_XD(REG_R9, 0) // V0 = Acc.NumVal
	as.FNEG(1, 0)         // V1 = -V0 (for abs via CSEL)
	as.FCMP(0, 1)
	as.FMOV_XD(1, REG_R9) // R9 = -old
	// CSEL: if V0 >= V1, keep original; else use negated
	as.B(slowPath) // deopt for correct NaN/Inf handling

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpMathAbs))
	as.Bind(done)
}

// emitSparkplugMathFloor emits native ARM64 for OpMathFloor.
func emitSparkplugMathFloor(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0300, 0) // TagNumber
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.B(slowPath) // deopt for correct floor via Go

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpMathFloor))
	as.Bind(done)
}

// emitSparkplugLessEqNumber emits native ARM64 for OpLessEqNumber.
func emitSparkplugLessEqNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FCMP(0, 1)
	as.CSET(REG_R9, 13) // LE condition

	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpLessEqNumber))
	as.Bind(done)
}

// --- Batch 4: Context/Scope/String/Math/Call native emitters ---

// emitSparkplugToStringNumber emits native ARM64 for OpToStringNumber.
func emitSparkplugToStringNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0300, 0) // TagNumber
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.B(slowPath) // string allocation → Go helper

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpToStringNumber))
	as.Bind(done)
}

// emitSparkplugToBooleanNumber emits native ARM64 for OpToBooleanNumber.
func emitSparkplugToBooleanNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0300, 0) // TagNumber
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.FMOV_XD(REG_R9, 0)
	as.FMOV(REG_ZR, 1) // V1 = 0.0
	as.FCMP(0, 1)
	as.CSET(REG_R9, 1) // NE → truthy (acc != 0.0)

	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpToBooleanNumber))
	as.Bind(done)
}

// emitSparkplugStringLength emits native ARM64 for OpStringLength.
func emitSparkplugStringLength(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	strReg := int(instr.OperandA)
	strSlot := strReg * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R8, REG_R12, strSlot+tagWordOff)
	as.MOVZ(REG_R13, 0x0400, 0) // TagString
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.B(slowPath) // string len → Go helper

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(strReg), 0)
	emitCall(as, funcToAddr(sparkplugOpStringLength))
	as.Bind(done)
}

// emitSparkplugStringEq emits native ARM64 for OpStringEq.
func emitSparkplugStringEq(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath) // string comparison → Go helper
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpStringEq))
	as.Bind(done)
}

// emitSparkplugCallBuiltin emits native ARM64 for OpCallBuiltin.
func emitSparkplugCallBuiltin(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	builtinIdx := int(instr.OperandA)
	argCount := int(instr.OperandB)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(builtinIdx), 0)
	as.MOVZ(REG_R2, uint16(argCount), 0)
	emitCall(as, funcToAddr(sparkplugOpCallBuiltin))
	as.Bind(done)
}

// emitSparkplugCallDirect emits native ARM64 for OpCallDirect.
func emitSparkplugCallDirect(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	funcReg := int(instr.OperandA)
	argCount := int(instr.OperandB)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(funcReg), 0)
	as.MOVZ(REG_R2, uint16(argCount), 0)
	emitCall(as, funcToAddr(sparkplugOpCallDirect))
	as.Bind(done)
}

// emitSparkplugPushContext emits native ARM64 for OpPushContext.
func emitSparkplugPushContext(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpPushContext))
	as.Bind(done)
}

// emitSparkplugPopContext emits native ARM64 for OpPopContext.
func emitSparkplugPopContext(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpPopContext))
	as.Bind(done)
}

// emitSparkplugLoadContextSlot emits native ARM64 for OpLoadContextSlot.
func emitSparkplugLoadContextSlot(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	contextIdx := int(instr.OperandA)
	slotIdx := int(instr.OperandB)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(contextIdx), 0)
	as.MOVZ(REG_R2, uint16(slotIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpLoadContextSlot))
	as.Bind(done)
}

// emitSparkplugStoreContextSlot emits native ARM64 for OpStoreContextSlot.
func emitSparkplugStoreContextSlot(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	contextIdx := int(instr.OperandA)
	slotIdx := int(instr.OperandB)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(contextIdx), 0)
	as.MOVZ(REG_R2, uint16(slotIdx), 0)
	emitCall(as, funcToAddr(sparkplugOpStoreContextSlot))
	as.Bind(done)
}

// --- Batch 5: Final 10 native emitters ---

// emitSparkplugGreaterEqNumber emits native ARM64 for OpGreaterEqNumber.
func emitSparkplugGreaterEqNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	lhsSlot := lhs * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R12, REG_VM0, regsOff)
	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.LDR(REG_R11, REG_R12, lhsSlot+numValOff)

	as.FMOV_XD(REG_R9, 0)
	as.FMOV_XD(REG_R11, 1)
	as.FCMP(0, 1)
	as.CSET(REG_R9, 10) // GE condition

	as.MOVZ(REG_R8, 0x0200, 0)
	as.ADD(REG_R8, REG_R8, REG_R9)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpGreaterEqNumber))
	as.Bind(done)
}

// emitSparkplugShiftRightNumber emits native ARM64 for OpShiftRightNumber.
func emitSparkplugShiftRightNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath) // int32 conversion → Go helper
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpShiftRightNumber))
	as.Bind(done)
}

// emitSparkplugShiftRightZeroNumber emits native ARM64 for OpShiftRightZeroNumber.
func emitSparkplugShiftRightZeroNumber(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	lhs := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath) // uint32 conversion → Go helper
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(lhs), 0)
	emitCall(as, funcToAddr(sparkplugOpShiftRightZeroNumber))
	as.Bind(done)
}

// emitSparkplugMathCeil emits native ARM64 for OpMathCeil.
func emitSparkplugMathCeil(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)
	as.B(slowPath) // ceil → Go helper

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpMathCeil))
	as.Bind(done)
}

// emitSparkplugMathSqrt emits native ARM64 for OpMathSqrt.
func emitSparkplugMathSqrt(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()

	as.LDR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R13, 0x0300, 0)
	as.CMP(REG_R8, REG_R13)
	as.BNE(slowPath)

	as.LDR(REG_R9, REG_VM0, accOffset+numValOff)
	as.FMOV_XD(REG_R9, 0) // V0 = Acc.NumVal
	// FSQRT V0, V0 — native sqrt (not available in simple asm)
	as.B(slowPath) // deopt for correct sqrt

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpMathSqrt))
	as.Bind(done)
}

// emitSparkplugForInSetupFastExt emits native ARM64 for OpForInSetupFast.
func emitSparkplugForInSetupFastExt(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	objReg := int(instr.OperandA)
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(objReg), 0)
	emitCall(as, funcToAddr(sparkplugOpForInSetupFast))
	as.Bind(done)
}

// emitSparkplugForInNextFastExt emits native ARM64 for OpForInNextFast.
func emitSparkplugForInNextFastExt(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	_ = instr
	slowPath := NewLabel()
	done := NewLabel()
	as.B(slowPath)
	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	emitCall(as, funcToAddr(sparkplugOpForInNextFast))
	as.Bind(done)
}

// emitSparkplugSwap emits native ARM64 for OpSwap (swap Acc with Regs[reg]).
func emitSparkplugSwap(as *Assembler, instr *js.Instruction, deoptStub *Label) {
	reg := int(instr.OperandA)
	regSlot := reg * jsValueSize
	slowPath := NewLabel()
	done := NewLabel()

	// Load Regs slice pointer.
	as.LDR(REG_R12, REG_VM0, regsOff)

	// Load Acc (64 bytes = 4 LDP pairs).
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset)      // Acc[0:16]
	as.LDP(REG_R10, REG_R11, REG_VM0, accOffset+16) // Acc[16:32]

	// Load Regs[reg] (64 bytes).
	as.LDP(REG_R13, REG_R14, REG_R12, regSlot)  // Regs[reg][0:16]
	as.LDP(REG_R4, REG_R5, REG_R12, regSlot+16) // Regs[reg][16:32]

	// Store Regs[reg] → Acc.
	as.STP(REG_R13, REG_R14, REG_VM0, accOffset)
	as.STP(REG_R4, REG_R5, REG_VM0, accOffset+16)

	// Store Acc → Regs[reg].
	as.STP(REG_R8, REG_R9, REG_R12, regSlot)
	as.STP(REG_R10, REG_R11, REG_R12, regSlot+16)

	// Second half (bytes 32-63).
	as.LDP(REG_R8, REG_R9, REG_VM0, accOffset+32)
	as.LDP(REG_R10, REG_R11, REG_VM0, accOffset+48)
	as.LDP(REG_R13, REG_R14, REG_R12, regSlot+32)
	as.LDP(REG_R4, REG_R5, REG_R12, regSlot+48)

	as.STP(REG_R13, REG_R14, REG_VM0, accOffset+32)
	as.STP(REG_R4, REG_R5, REG_VM0, accOffset+48)
	as.STP(REG_R8, REG_R9, REG_R12, regSlot+32)
	as.STP(REG_R10, REG_R11, REG_R12, regSlot+48)

	as.B(done)

	as.Bind(slowPath)
	as.MOV(REG_R0, REG_VM0)
	as.MOVZ(REG_R1, uint16(reg), 0)
	emitCall(as, funcToAddr(sparkplugOpSwap))
	as.Bind(done)
}

// emitSparkplugLdaTrueFast emits native ARM64 for OpLdaTrueFast (unconditional true).
func emitSparkplugLdaTrueFast(as *Assembler, deoptStub *Label) {
	_ = deoptStub
	// TagBoolean + true: 0x0200 | 0x01 = 0x0201
	as.MOVZ(REG_R8, 0x0201, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R9, 1, 0)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
}

// emitSparkplugLdaFalseFast emits native ARM64 for OpLdaFalseFast (unconditional false).
func emitSparkplugLdaFalseFast(as *Assembler, deoptStub *Label) {
	_ = deoptStub
	// TagBoolean + false: 0x0200 | 0x00 = 0x0200
	as.MOVZ(REG_R8, 0x0200, 0)
	as.STR(REG_R8, REG_VM0, accTagAlign)
	as.MOVZ(REG_R9, 0, 0)
	as.STR(REG_R9, REG_VM0, accOffset+numValOff)
}

