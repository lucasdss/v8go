// vm_exec.go — Bytecode interpreter execution loop and dispatch.
package js

import (
"strings"
)

// opHandler is the function signature for bytecode opcode handlers.
// Used in the function table dispatch (opTable) instead of a switch statement.
type opHandler func(vm *VM, frame *VMFrame, instr Instruction)

// QueuedEvent represents a DOM event queued for dispatch in the VM.
// When the browser fires a lifecycle event (DOMContentLoaded, load), it
// enqueues the event here and the VM executes matching listeners.
type QueuedEvent struct {
	Type   string
	Target string
	Data   map[string]JSValue
}

// maxCallDepth limits recursion to prevent Go stack overflow from
// infinite JS recursion (e.g., function f(){f()};f()). Chrome V8 uses
// a similar limit; exceeding it returns Undefined.
// Set below Tier0SparkplugThreshold (100) to avoid hitting JIT code
// during deep recursion, which can crash on macOS ARM64 without JIT
// entitlement.
const maxCallDepth = 80

// maxSteps limits the total number of bytecode instructions executed
// in a single Run/Execute call. Prevents infinite loops from hanging
// the VM (e.g., while(true){} or malformed jump targets).
const maxSteps = 10_000_000

// Tier-up thresholds for multi-tier JIT compilation.
// When a function is called Tier0SparkplugThreshold times, it is compiled
// with the Sparkplug baseline JIT. At Tier1TurboFanThreshold calls, it
// becomes a candidate for TurboFan optimizing compilation.
const (
	Tier0SparkplugThreshold = 100
	Tier1TurboFanThreshold  = 1000
)

// SetConsoleOutput sets a callback for console.log output.

// SetConsoleOutput sets a callback for console.log output.
func (vm *VM) SetConsoleOutput(fn func(string)) {
	vm.console.SetOutput(fn)
}

// GetModuleRegistry returns the module registry, creating one if needed.
func (vm *VM) GetModuleRegistry() *ModuleRegistry {
	if vm.moduleRegistry == nil {
		vm.moduleRegistry = NewModuleRegistry(vm, nil)
	}
	return vm.moduleRegistry
}

// Run parses and executes JavaScript source, returning the final accumulator value.
func (vm *VM) Run(source string) JSValue {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Check bytecode cache first.
	if bf, ok := vm.registry.Cache[source]; ok {
		vm.maybePromoteTier(bf)
		return vm.execute(bf)
	}

	// Parse.
	tokens := NewLexer(source).Tokenize()
	prog, errs := NewParser(tokens).Parse()
	if len(errs) > 0 {
		vm.console.Log("Parse error: " + strings.Join(errs, "; "))
		return Undefined
	}

	// Compile.
	bf := Compile(prog)

	// Pre-allocate constant names to avoid per-execution lookup allocations.
	bf.BuildConstantNames()

	// ICVector is allocated at compile time (Compile sets bf.ICVector).
	// No per-execution allocation — the frame simply references bf.ICVector.

	// Cache for future runs.
	vm.registry.Cache[source] = bf

	// Execute.
	vm.maybePromoteTier(bf)
	return vm.execute(bf)
}

// Execute runs a compiled BytecodeFunction and returns the result.
func (vm *VM) Execute(bf *BytecodeFunction) JSValue {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.maybePromoteTier(bf)
	return vm.execute(bf)
}

// executeOne dispatches a single instruction via function table.
// Returns true if the frame should return (OpReturn or exception was hit).
func (vm *VM) executeOne(frame *VMFrame) (shouldReturn bool) {
	vm.stepCount++
	if vm.stepCount > maxSteps {
		frame.Thrown = NewString("RangeError: Maximum execution steps exceeded")
		return true
	}
	instr := frame.Func.Instructions[frame.PC]
	frame.PC++

	// OSR check: gated by HasJITTier to avoid two wasted loads+comparisons
	// per jump instruction for functions that never tier up (the common case).
	if frame.Func.HasJITTier {
		// OSR check: if TurboFan became available mid-execution, transition
		// at loop back-edges (backward jumps). TurboFan takes priority over Sparkplug.
		if frame.Func.TurboFan != 0 && !frame.InTurboFan {
			if instr.Op == OpJump {
				target := int(instr.OperandA)
				if target <= frame.PC { // backward jump = loop edge
					vm.osrToTurboFan(frame)
					if !frame.InTurboFan {
						return false
					}
					return true // TurboFan handles the rest
				}
			}
		}

		// OSR check: if Sparkplug became available mid-execution, transition
		// at loop back-edges (backward jumps). This allows hot loops detected
		// during interpretation to seamlessly upgrade to native code.
		if !vm.DisableJIT && frame.Func.Sparkplug != 0 && !frame.InSparkplug {
			if instr.Op == OpJump {
				target := int(instr.OperandA)
				if target <= frame.PC { // backward jump = loop edge
					vm.osrToSparkplug(frame)
					// If deopt occurred (InSparkplug cleared), continue in
					// interpreter instead of returning.
					if !frame.InSparkplug {
						return false
					}
					return true // Sparkplug handles the rest
				}
			}
		}
	}

	// Fast-path: inline trivial ops to skip function table dispatch overhead.
	switch instr.Op {
	case OpNop:
		// Nothing to do — skip handler call entirely.
	case OpLdaSmi:
		// Inline: small integer load — one of the most common ops.
		frame.Acc = NewNumber(float64(int8(instr.OperandA)))
	case OpStar:
		// Inline: store accumulator to register — very common.
		reg := int(instr.OperandA)
		if reg < len(frame.Regs) {
			frame.Regs[reg] = frame.Acc
		}
	case OpLdar:
		// Inline: load register to accumulator.
		reg := int(instr.OperandA)
		if reg < len(frame.Regs) {
			frame.Acc = frame.Regs[reg]
		}
	case OpReturn:
		// Inline the common "return undefined" case (OperandB==2 from peephole
		// optimizer merging LdaUndefined+Return). Skip handler for leaf functions
		// with no finally handler and no generator state.
		if instr.OperandB == 2 && frame.GenState == nil && frame.FinallyPC < 0 {
			frame.Acc = Undefined
			return true
		}
		handler := opTable[instr.Op]
		if handler != nil {
			handler(vm, frame, instr)
		}
	case OpAdd:
		// Inline fast path: both operands are numbers (dominant case).
		lhs := frame.Regs[int(instr.OperandA)]
		if lhs.Tag == TagNumber && frame.Acc.Tag == TagNumber {
			result := lhs.NumVal + frame.Acc.NumVal
			if isSmallInt(result) {
				frame.Acc = smallIntValue(int(result))
			} else {
				frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
			}
			recordBinaryFeedback(frame, instr)
		} else {
			opAdd(vm, frame, instr)
		}
	case OpLdaNamedProperty:
		// Inline fast path: monomorphic IC hit avoids function-table dispatch.
		slotIdx := int(instr.OperandC)
		hit := false
		if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
			obj := frame.Acc.ObjVal
			if slotIdx < len(frame.Func.ICVector.Slots) {
				slot := &frame.Func.ICVector.Slots[slotIdx]
				if slot.State == ICMonomorphic && obj.Shape == slot.Shape && slot.Offset < obj.propLen() {
					val := obj.propAt(slot.Offset)
					// Accessor property: call the getter function.
					if attr := obj.Shape.GetAttr(slot.Name); attr&AttrAccessor != 0 && val.IsObject() && val.ObjVal.IsCallable() {
						frame.Acc = val.ObjVal.Call(obj, nil)
					} else {
						frame.Acc = val
					}
					slot.HitCount++
					hit = true
				}
			}
		}
		if !hit {
			opLdaNamedProperty(vm, frame, instr)
		}
	default:
		handler := opTable[instr.Op]
		if handler != nil {
			handler(vm, frame, instr)
		}
	}
	if frame.ShouldReturn {
		frame.ShouldReturn = false
		return true
	}
	if frame.Thrown.Tag != TagUndefined {
		return true
	}
	return false
}

// opTable maps each opcode to its handler function. Uses a fixed-size array
// for O(1) lookup with predictable branch behavior (better than switch).
// Populated in init() to avoid initialization cycles (handler functions may
// call VM methods that eventually reference opTable through executeOne).
var opTable [256]opHandler

func init() {
	opTable[OpNop] = opNop
	opTable[OpLdaConstant] = opLdaConstant
	opTable[OpLdaUndefined] = opLdaUndefined
	opTable[OpLdaNull] = opLdaNull
	opTable[OpLdaTrue] = opLdaTrue
	opTable[OpLdaFalse] = opLdaFalse
	opTable[OpLdaZero] = opLdaZero
	opTable[OpLdaOne] = opLdaOne
	opTable[OpLdaSmi] = opLdaSmi
	opTable[OpStar] = opStar
	opTable[OpLdar] = opLdar
	opTable[OpMov] = opMov
	opTable[OpAdd] = opAdd
	opTable[OpSub] = opSub
	opTable[OpMul] = opMul
	opTable[OpDiv] = opDiv
	opTable[OpMod] = opMod
	opTable[OpExp] = opExp
	opTable[OpNegate] = opNegate
	opTable[OpInc] = opInc
	opTable[OpDec] = opDec
	opTable[OpEq] = opEq
	opTable[OpNotEq] = opNotEq
	opTable[OpStrictEq] = opStrictEq
	opTable[OpStrictNotEq] = opStrictNotEq
	opTable[OpLessThan] = opLessThan
	opTable[OpGreaterThan] = opGreaterThan
	opTable[OpLessEq] = opLessEq
	opTable[OpGreaterEq] = opGreaterEq
	opTable[OpLogicalNot] = opLogicalNot
	opTable[OpLogicalAnd] = opLogicalAnd
	opTable[OpLogicalOr] = opLogicalOr
	opTable[OpBitwiseAnd] = opBitwiseAnd
	opTable[OpBitwiseOr] = opBitwiseOr
	opTable[OpBitwiseXor] = opBitwiseXor
	opTable[OpBitwiseNot] = opBitwiseNot
	opTable[OpShiftLeft] = opShiftLeft
	opTable[OpShiftRight] = opShiftRight
	opTable[OpShiftRightZero] = opShiftRightZero
	opTable[OpToNumber] = opToNumber
	opTable[OpToString] = opToString
	opTable[OpToBoolean] = opToBoolean
	opTable[OpTypeof] = opTypeof
	opTable[OpDelete] = opDelete
	opTable[OpDeleteKeyed] = opDeleteKeyed
	opTable[OpInstanceof] = opInstanceof
	opTable[OpIn] = opIn
	opTable[OpLdaNamedProperty] = opLdaNamedProperty
	opTable[OpStaNamedProperty] = opStaNamedProperty
	opTable[OpDefineAccessorProperty] = opDefineAccessorProperty
	opTable[OpLdaKeyedProperty] = opLdaKeyedProperty
	opTable[OpStaKeyedProperty] = opStaKeyedProperty
	opTable[OpJump] = opJump
	opTable[OpJumpIfFalse] = opJumpIfFalse
	opTable[OpJumpIfTrue] = opJumpIfTrue
	opTable[OpJumpIfToBooleanTrue] = opJumpIfToBooleanTrue
	opTable[OpJumpIfToBooleanFalse] = opJumpIfToBooleanFalse
	opTable[OpJumpIfNotNullish] = opJumpIfNotNullish
	opTable[OpCall] = opCall
	opTable[OpCall0] = opCall0
	opTable[OpCall1] = opCall1
	opTable[OpCall2] = opCall2
	opTable[OpCallSpread] = opCallSpread
	opTable[OpReturn] = opReturn
	opTable[OpCreateClosure] = opCreateClosure
	opTable[OpLdaCaptured] = opLdaCaptured
	opTable[OpCreateObject] = opCreateObject
	opTable[OpCreateObjectLiteral] = opCreateObjectLiteral
	opTable[OpCreateArray] = opCreateArray
	opTable[OpCreateRegExp] = opCreateRegExp
	opTable[OpThrow] = opThrow
	opTable[OpSetTryHandler] = opSetTryHandler
	opTable[OpClearTryHandler] = opClearTryHandler
	opTable[OpSetFinallyHandler] = opSetFinallyHandler
	opTable[OpForInSetup] = opForInSetup
	opTable[OpForInNext] = opForInNext
	opTable[OpLdaGlobal] = opLdaGlobal
	opTable[OpStaGlobal] = opStaGlobal
	opTable[OpLdaGlobalSlot] = opLdaGlobalSlot
	opTable[OpStaGlobalSlot] = opStaGlobalSlot
	opTable[OpLdaLocal] = opLdaLocal
	opTable[OpStaLocal] = opStaLocal
	opTable[OpLdaThis] = opLdaThis
	opTable[OpDup] = opDup
	opTable[OpStaByOffset] = opStaByOffset
	opTable[OpThrowConstAssignment] = opThrowConstAssignment
	opTable[OpSetPrototype] = opSetPrototype
	opTable[OpYield] = opYield
	opTable[OpYieldDelegate] = opYieldDelegate
	opTable[OpCreateGenerator] = opCreateGenerator
	opTable[OpSuperCall] = opSuperCall
	opTable[OpCheckConstructor] = opCheckConstructor
}

// --- Bytecode handler functions ---

func opNop(vm *VM, frame *VMFrame, instr Instruction) {}

func opLdaConstant(vm *VM, frame *VMFrame, instr Instruction) {
	idx := int(instr.OperandA)
	if idx < len(frame.Func.Constants) {
		frame.Acc = frame.Func.Constants[idx]
	}
}

func opLdaUndefined(vm *VM, frame *VMFrame, instr Instruction) { frame.Acc = Undefined }
func opLdaNull(vm *VM, frame *VMFrame, instr Instruction)      { frame.Acc = Null }
func opLdaTrue(vm *VM, frame *VMFrame, instr Instruction)      { frame.Acc = True }
func opLdaFalse(vm *VM, frame *VMFrame, instr Instruction)     { frame.Acc = False }
func opLdaZero(vm *VM, frame *VMFrame, instr Instruction)      { frame.Acc = smallIntValue(0) }
func opLdaOne(vm *VM, frame *VMFrame, instr Instruction)       { frame.Acc = smallIntValue(1) }

func opLdaSmi(vm *VM, frame *VMFrame, instr Instruction) {
	val := int8(instr.OperandA)
	frame.Acc = smallIntValue(int(val))
}

func opStar(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Regs[reg] = frame.Acc
	}
}

func opLdar(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Acc = frame.Regs[reg]
	}
}

func opMov(vm *VM, frame *VMFrame, instr Instruction) {
	src, dst := int(instr.OperandA), int(instr.OperandB)
	if src < len(frame.Regs) && dst < len(frame.Regs) {
		frame.Regs[dst] = frame.Regs[src]
	}
}

func (vm *VM) execute(bf *BytecodeFunction) JSValue {
	// Reset step counter and allocator bump pointers for this top-level execution.
	vm.stepCount = 0
	vm.alloc.Reset()
	// Ensure global slot array is large enough for this function's globals.
	if len(bf.GlobalSlots) > 0 {
		vm.ensureGlobalSlots(len(bf.GlobalSlots), bf)
	}

	regs := vm.allocRegs(bf.NumRegisters)
	frame := vm.allocFrame()
	frame.Func = bf
	frame.Regs = regs
	frame.HandlerPC = -1
	frame.FinallyPC = -1
	frame.This = vm.globalObject()
	frame.ICVector = bf.ICVector

	// If TurboFan native code is already compiled, dispatch directly (highest tier).
	if !vm.DisableJIT && bf.TurboFan != 0 {
		vm.executeTurboFan(frame)
		vm.freeRegs(regs)
		vm.freeFrame(frame)
		return frame.Acc
	}

	// If Sparkplug native code is already compiled, dispatch directly.
	if !vm.DisableJIT && bf.Sparkplug != 0 {
		_ = bf.Sparkplug // Sparkplug active
		vm.executeSparkplug(frame)
		// If a deoptimization occurred (type guard failed), the deopt stub
		// called GoDeoptimize which cleared frame.InSparkplug. Fall through
		// to the interpreter loop to re-execute the failing instruction.
		if frame.InSparkplug {
			vm.freeRegs(regs)
			vm.freeFrame(frame)
			return frame.Acc
		}
		// Deopt: continue in interpreter below.
	}

	for frame.PC < len(frame.Func.Instructions) {
		if vm.executeOne(frame) {
			// Exception handling: jump to catch handler if set.
			if frame.Thrown.Tag != TagUndefined && frame.HandlerPC >= 0 {
				frame.PC = frame.HandlerPC
				frame.HandlerPC = -1
				frame.Thrown = Undefined
				frame.ShouldReturn = false
				continue
			}
			// Exception with finally (no catch): jump to finally, then re-throw.
			if frame.Thrown.Tag != TagUndefined && frame.FinallyPC >= 0 {
				frame.PC = frame.FinallyPC
				frame.FinallyPC = -1
				continue
			}
			vm.freeRegs(regs)
			vm.freeFrame(frame)
			return frame.Acc
		}
	}
	vm.freeRegs(regs)
	vm.freeFrame(frame)
	return frame.Acc
}

// globalObject returns a reference to the VM's cached global object.
// The global object is created once in NewVM and reused across all calls.
func (vm *VM) globalObject() JSValue {
	return vm.globals.GlobalThis()
}

func (vm *VM) executeFrame(frame *VMFrame) JSValue {
	frame.HandlerPC = -1
	frame.FinallyPC = -1
	// Note: frame.This must be set by the caller (execute, callMethod, makeFunctionObject).

	// Tier promotion: increment call count and trigger JIT compilation
	// at thresholds. This is the hot path for all function calls.
	vm.maybePromoteTier(frame.Func)

	// If TurboFan native code is already compiled, dispatch directly (highest tier).
	if !vm.DisableJIT && frame.Func.TurboFan != 0 {
		vm.executeTurboFan(frame)
		return frame.Acc
	}

	// If Sparkplug native code is already compiled, dispatch directly.
	if !vm.DisableJIT && frame.Func.Sparkplug != 0 {
		vm.executeSparkplug(frame)
		// If deopt occurred (InSparkplug cleared), fall through to interpreter.
		if frame.InSparkplug {
			return frame.Acc
		}
	}

	for frame.PC < len(frame.Func.Instructions) {
		if vm.executeOne(frame) {
			// Exception handling: jump to catch handler if set.
			if frame.Thrown.Tag != TagUndefined && frame.HandlerPC >= 0 {
				frame.PC = frame.HandlerPC
				frame.HandlerPC = -1
				frame.Thrown = Undefined
				frame.ShouldReturn = false
				continue
			}
			// Exception with finally (no catch): jump to finally, then re-throw.
			if frame.Thrown.Tag != TagUndefined && frame.FinallyPC >= 0 {
				frame.PC = frame.FinallyPC
				frame.FinallyPC = -1
				continue
			}
			return frame.Acc
		}
	}
	return frame.Acc
}
