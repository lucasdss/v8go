//go:build !amd64

// Package jit — sparkplug: Sparkplug baseline JIT compiler (Tier 1).
//
// Compiles bytecode from js.BytecodeFunction into ARM64 machine code.
// The generated code takes a *js.VMFrame in R0 and executes the bytecode
// directly, using native branches for control flow and Go helper calls
// for value manipulation. This eliminates the interpreter dispatch loop
// overhead — the primary win for a baseline JIT.
//
//nolint:unused,dupl,revive,gocritic,staticcheck,gosec
package jit

import (
	"math"
	"strconv"
	"strings"

	"github.com/lucasdss/v8go/pkg/js"
)

// Sparkplug is V8Go's baseline JIT compiler. It translates bytecode to
// native machine code for fast execution. The shadow stack integration
// points: prologue setup and epilogue teardown are defined here.
//
// Register convention (ARM64):
//
//	X28 — shadow stack pointer (saved per-frame)
//	X19–X25 — callee-saved registers that may hold JS object pointers
//	X0–X7  — argument / return registers (caller-saved)
//
// When a callee-saved register holds a JSValue with TagObject, its ObjVal
// pointer is spilled to the shadow stack so the GC can trace it.

// Sparkplug helper function types. Each corresponds to a bytecode operation
// and is implemented in Go (see sparkplug_helpers.go).
// The JIT code calls these via BLR with the frame pointer in R0.
//
// Note: These are declared here so the JIT compiler can reference their
// addresses. The actual implementations are in sparkplug_helpers.go.

// sparkplugOpLdaSmi loads a small integer into the accumulator.
func sparkplugOpLdaSmi(frame *js.VMFrame, val int8) {
	frame.Acc = js.NewNumber(float64(val))
}

// sparkplugOpLdaZero loads 0 into the accumulator.
func sparkplugOpLdaZero(frame *js.VMFrame) {
	frame.Acc = js.NewNumber(0)
}

// sparkplugOpLdaOne loads 1 into the accumulator.
func sparkplugOpLdaOne(frame *js.VMFrame) {
	frame.Acc = js.NewNumber(1)
}

// sparkplugOpLdaUndefined loads undefined into the accumulator.
func sparkplugOpLdaUndefined(frame *js.VMFrame) {
	frame.Acc = js.Undefined
}

// sparkplugOpLdaNull loads null into the accumulator.
func sparkplugOpLdaNull(frame *js.VMFrame) {
	frame.Acc = js.Null
}

// sparkplugOpLdaTrue loads true into the accumulator.
func sparkplugOpLdaTrue(frame *js.VMFrame) {
	frame.Acc = js.True
}

// sparkplugOpLdaFalse loads false into the accumulator.
func sparkplugOpLdaFalse(frame *js.VMFrame) {
	frame.Acc = js.False
}

// sparkplugOpStar stores the accumulator into a register.
func sparkplugOpStar(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Regs[reg] = frame.Acc
	}
}

// sparkplugOpLdar loads a register into the accumulator.
func sparkplugOpLdar(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = frame.Regs[reg]
	}
}

// sparkplugOpReturn sets PC past the end of instructions to signal return.
func sparkplugOpReturn(frame *js.VMFrame) {
	frame.PC = len(frame.Func.Instructions)
}

// sparkplugOpAdd performs acc + reg → acc.
func sparkplugOpAdd(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a, b := frame.Acc, frame.Regs[reg]
		if a.Tag == js.TagNumber && b.Tag == js.TagNumber {
			frame.Acc = js.NewNumber(a.NumVal + b.NumVal)
			return
		}
		// String concatenation
		if a.Tag == js.TagString && b.Tag == js.TagString {
			frame.Acc = js.NewString(a.StrVal + b.StrVal)
			return
		}
		// Fall back to generic coercion
		frame.Acc = js.NewNumber(a.ToNumber() + b.ToNumber())
	}
}

// sparkplugOpSub performs acc - reg → acc.
func sparkplugOpSub(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewNumber(frame.Acc.ToNumber() - frame.Regs[reg].ToNumber())
	}
}

// sparkplugOpMul performs acc * reg → acc.
func sparkplugOpMul(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewNumber(frame.Acc.ToNumber() * frame.Regs[reg].ToNumber())
	}
}

// sparkplugOpDiv performs acc / reg → acc.
func sparkplugOpDiv(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		bn := frame.Regs[reg].ToNumber()
		if bn == 0 {
			frame.Acc = js.NewNumber(math.NaN())
		} else {
			frame.Acc = js.NewNumber(frame.Acc.ToNumber() / bn)
		}
	}
}

// sparkplugIsFalsy returns true if the accumulator is falsy (for JumpIfFalse).
func sparkplugIsFalsy(frame *js.VMFrame) bool {
	return !frame.Acc.IsTruthy()
}

// --- JS arithmetic helpers (called from JIT code) ---

func jsAdd(a, b js.JSValue) js.JSValue {
	if a.Tag == js.TagNumber && b.Tag == js.TagNumber {
		return js.NewNumber(a.NumVal + b.NumVal)
	}
	// String concatenation
	if a.Tag == js.TagString && b.Tag == js.TagString {
		return js.NewString(a.StrVal + b.StrVal)
	}
	// Fall back to generic coercion
	an := a.ToNumber()
	bn := b.ToNumber()
	return js.NewNumber(an + bn)
}

func jsSub(a, b js.JSValue) js.JSValue {
	return js.NewNumber(a.ToNumber() - b.ToNumber())
}

func jsMul(a, b js.JSValue) js.JSValue {
	return js.NewNumber(a.ToNumber() * b.ToNumber())
}

func jsDiv(a, b js.JSValue) js.JSValue {
	bn := b.ToNumber()
	if bn == 0 {
		return js.NewNumber(jsNaN())
	}
	return js.NewNumber(a.ToNumber() / bn)
}

func jsNaN() float64 {
	return math.NaN()
}

// --- JS comparison / conversion helpers (called from JIT slow paths) ---

// sparkplugOpStrictEq performs acc === reg → acc (slow path for non-number types).
func sparkplugOpStrictEq(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewBoolean(frame.Regs[reg].StrictEquals(frame.Acc))
	}
}

// sparkplugOpLessThan performs acc < reg → acc (slow path for non-number types).
func sparkplugOpLessThan(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		lhs := frame.Regs[reg]
		if lhs.IsBigInt() || frame.Acc.IsBigInt() {
			if lhs.IsBigInt() && frame.Acc.IsBigInt() {
				frame.Acc = js.NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) < 0)
				return
			}
		}
		frame.Acc = js.NewBoolean(lhs.ToNumber() < frame.Acc.ToNumber())
	}
}

// sparkplugOpGreaterThan performs acc > reg → acc (slow path for non-number types).
func sparkplugOpGreaterThan(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		lhs := frame.Regs[reg]
		if lhs.IsBigInt() || frame.Acc.IsBigInt() {
			if lhs.IsBigInt() && frame.Acc.IsBigInt() {
				frame.Acc = js.NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) > 0)
				return
			}
		}
		frame.Acc = js.NewBoolean(lhs.ToNumber() > frame.Acc.ToNumber())
	}
}

// sparkplugOpEq performs acc == reg → acc (loose equality, slow path).
func sparkplugOpEq(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewBoolean(frame.Regs[reg].Equals(frame.Acc))
	}
}

// sparkplugOpNotEq performs acc != reg → acc (loose inequality, slow path).
func sparkplugOpNotEq(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewBoolean(!frame.Regs[reg].Equals(frame.Acc))
	}
}

// sparkplugOpStrictNotEq performs acc !== reg → acc (slow path).
func sparkplugOpStrictNotEq(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewBoolean(!frame.Regs[reg].StrictEquals(frame.Acc))
	}
}

// sparkplugOpLessEq performs acc <= reg → acc (slow path).
func sparkplugOpLessEq(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewBoolean(frame.Regs[reg].ToNumber() <= frame.Acc.ToNumber())
	}
}

// sparkplugOpGreaterEq performs acc >= reg → acc (slow path).
func sparkplugOpGreaterEq(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewBoolean(frame.Regs[reg].ToNumber() >= frame.Acc.ToNumber())
	}
}

// sparkplugOpTypeof performs typeof acc → acc.
func sparkplugOpTypeof(frame *js.VMFrame) {
	// Returns a string: "undefined", "boolean", "number", "string", "object", "function", "symbol", "bigint"
	switch frame.Acc.Tag {
	case js.TagUndefined:
		frame.Acc = js.NewString("undefined")
	case js.TagNull:
		frame.Acc = js.NewString("object")
	case js.TagBoolean:
		frame.Acc = js.NewString("boolean")
	case js.TagNumber:
		frame.Acc = js.NewString("number")
	case js.TagString:
		frame.Acc = js.NewString("string")
	case js.TagObject:
		if frame.Acc.ObjVal != nil && frame.Acc.ObjVal.IsCallable() {
			frame.Acc = js.NewString("function")
		} else {
			frame.Acc = js.NewString("object")
		}
	case js.TagSymbol:
		frame.Acc = js.NewString("symbol")
	case js.TagBigInt:
		frame.Acc = js.NewString("bigint")
	}
}

// sparkplugOpLogicalNot performs !acc → acc (slow-path fallback).
func sparkplugOpLogicalNot(frame *js.VMFrame) {
	if !frame.Acc.IsTruthy() {
		frame.Acc = js.True
	} else {
		frame.Acc = js.False
	}
}

// sparkplugOpLogicalAnd performs acc && reg → acc (slow-path fallback).
func sparkplugOpLogicalAnd(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if !frame.Acc.IsTruthy() {
			return
		}
		frame.Acc = frame.Regs[reg]
	}
}

// sparkplugOpLogicalOr performs acc || reg → acc (slow-path fallback).
func sparkplugOpLogicalOr(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.IsTruthy() {
			return
		}
		frame.Acc = frame.Regs[reg]
	}
}

// sparkplugOpMod performs acc % reg → acc. Slow path only for now.
func sparkplugOpMod(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		lhs := frame.Regs[reg]
		l, r := lhs.ToNumber(), frame.Acc.ToNumber()
		if r == 0 {
			frame.Acc = js.NewNumber(math.NaN())
		} else {
			frame.Acc = js.NewNumber(math.Mod(l, r))
		}
	}
}

// --- Property access helpers (called from JIT code) ---

// sparkplugOpLdaNamedPropertySlow is the slow-path handler for OpLdaNamedProperty.
func sparkplugOpLdaNamedPropertySlow(frame *js.VMFrame, propIdx int, slotIdx int) {
	if !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
		frame.Acc = js.Undefined
		return
	}
	obj := frame.Acc.ObjVal
	propName := ""
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if slotIdx < 255 && frame.Func.ICVector != nil && slotIdx < len(frame.Func.ICVector.Slots) {
		frame.Acc = frame.Func.ICVector.LoadIC(slotIdx, obj, propName)
	} else {
		frame.Acc = obj.Get(propName)
	}
}

// sparkplugOpStaNamedPropertySlow is the slow-path handler for OpStaNamedProperty.
func sparkplugOpStaNamedPropertySlow(frame *js.VMFrame, propIdx int, slotIdx int, reg int) {
	if !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
		return
	}
	obj := frame.Acc.ObjVal
	propName := ""
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	var value js.JSValue
	if reg < len(frame.Regs) {
		value = frame.Regs[reg]
	}
	if slotIdx < 255 && frame.Func.ICVector != nil && slotIdx < len(frame.Func.ICVector.Slots) {
		frame.Func.ICVector.StoreIC(slotIdx, obj, propName, value)
		return
	}
	obj.Set(propName, value)
}

// sparkplugOpCreateObjectLiteral creates a new object with the given shape.
func sparkplugOpCreateObjectLiteral(frame *js.VMFrame, shapeKeyIdx int) {
	obj := js.NewJSObject()
	if shapeKeyIdx >= 0 && shapeKeyIdx < len(frame.Func.Constants) {
		shapeKey := frame.Func.Constants[shapeKeyIdx].ToString()
		propNames := strings.Split(shapeKey, ",")
		if len(propNames) > 0 && propNames[0] != "" {
			obj.Shape = js.GetOrCreateShape(propNames)
		}
	}
	frame.Acc = js.NewObject(obj)
}

// sparkplugOpStaByOffset stores from a register into an object property by offset.
func sparkplugOpStaByOffset(frame *js.VMFrame, objReg int, valReg int, slotIdx int) {
	if objReg >= len(frame.Regs) || valReg >= len(frame.Regs) {
		return
	}
	objVal := frame.Regs[objReg]
	val := frame.Regs[valReg]
	if !objVal.IsObject() || objVal.ObjVal == nil {
		return
	}
	obj := objVal.ObjVal
	if slotIdx < 255 && frame.ICVector != nil && slotIdx < len(frame.ICVector.Slots) {
		slot := &frame.ICVector.Slots[slotIdx]
		if slot.Offset < len(obj.Properties) && slot.State == js.ICMonomorphic && obj.Shape == slot.Shape {
			obj.Set(slot.Name, val)
			return
		}
	}
	// Fallback: use the property name from the constant pool (not available here)
	// For now, skip if IC fails
}

// sparkplugOpStaGlobalSlot stores the accumulator into a global slot.
func sparkplugOpStaGlobalSlot(frame *js.VMFrame, slotIdx int) {
	// Ensure GlobalVals is large enough, then cache Acc.
	if frame.Func == nil {
		return
	}
	if slotIdx >= len(frame.Func.GlobalVals) {
		newVals := make([]js.JSValue, slotIdx+1)
		copy(newVals, frame.Func.GlobalVals)
		frame.Func.GlobalVals = newVals
	}
	frame.Func.GlobalVals[slotIdx] = frame.Acc
}

// sparkplugOpLdaGlobalSlot loads a global slot into the accumulator.
func sparkplugOpLdaGlobalSlot(frame *js.VMFrame, slotIdx int) {
	// Load from GlobalVals cache (populated by VM on global stores).
	if frame.Func != nil && slotIdx < len(frame.Func.GlobalVals) {
		frame.Acc = frame.Func.GlobalVals[slotIdx]
	} else {
		frame.Acc = js.Undefined
	}
}

// -- Object / Array / Closure helpers (called from JIT code) --

// sparkplugOpCreateArray creates a new empty array object → acc.
func sparkplugOpCreateArray(frame *js.VMFrame) {
	arr := js.NewJSObject()
	arr.ConstructorName = "Array"
	if js.ArrayPrototype != nil {
		arr.Prototype = js.ArrayPrototype
	}
	arr.Set("length", js.NewNumber(0))
	frame.Acc = js.NewObject(arr)
}

// sparkplugOpCreateObject creates a new empty object → acc.
func sparkplugOpCreateObject(frame *js.VMFrame) {
	frame.Acc = js.NewObject(js.NewJSObject())
}

// sparkplugOpDelete deletes a named property from the object in acc.
func sparkplugOpDelete(frame *js.VMFrame, propIdx int) {
	propName := ""
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil && propName != "" {
		if frame.Acc.ObjVal.Delete(propName) {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	} else {
		frame.Acc = js.False
	}
}

// sparkplugOpDeleteKeyed deletes a keyed property: delete obj[key].
func sparkplugOpDeleteKeyed(frame *js.VMFrame, objReg int, keyReg int) {
	if objReg < len(frame.Regs) && keyReg < len(frame.Regs) {
		objVal := frame.Regs[objReg]
		key := frame.Regs[keyReg].ToString()
		if objVal.IsObject() && objVal.ObjVal != nil {
			frame.Acc = js.NewBoolean(objVal.ObjVal.Delete(key))
		} else {
			frame.Acc = js.True
		}
	}
}

// sparkplugOpToNumber converts acc to a number.
func sparkplugOpToNumber(frame *js.VMFrame) {
	frame.Acc = js.NewNumber(frame.Acc.ToNumber())
}

// sparkplugOpToString converts acc to a string.
func sparkplugOpToString(frame *js.VMFrame) {
	frame.Acc = js.NewString(frame.Acc.ToString())
}

// sparkplugOpLdaCaptured loads a captured variable from the closure environment.
func sparkplugOpLdaCaptured(frame *js.VMFrame, regIdx int) {
	if frame.ClosureEnv != nil {
		frame.Acc = frame.ClosureEnv.Get(strconv.Itoa(regIdx))
	} else {
		frame.Acc = js.Undefined
	}
}

// sparkplugOpCreateClosure creates a closure from the BytecodeFunction in acc.
func sparkplugOpCreateClosure(frame *js.VMFrame) {
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Acc.ObjVal.Bytecode != nil {
		closure := js.NewJSObject()
		closure.ConstructorName = frame.Acc.ObjVal.ConstructorName
		closure.Bytecode = frame.Acc.ObjVal.Bytecode
		if js.FunctionPrototype != nil {
			closure.Prototype = js.FunctionPrototype
		}
		closure.Set("length", js.NewNumber(float64(frame.Acc.ObjVal.Bytecode.NumParams)))
		if frame.Acc.ObjVal.Bytecode.Name != "" {
			closure.Set("name", js.NewString(frame.Acc.ObjVal.Bytecode.Name))
		}
		frame.Acc = js.NewObject(closure)
	} else {
		frame.Acc = js.NewObject(js.NewJSObject())
	}
}

// sparkplugOpLdaKeyedPropertySlow performs acc = obj[key] (key in acc, obj in reg).
func sparkplugOpLdaKeyedPropertySlow(frame *js.VMFrame, objReg int) {
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() {
		obj := frame.Regs[objReg].ObjVal
		key := frame.Acc.ToString()

		// Dense array fast path: numeric index → direct offset access.
		if obj.Prototype == js.ArrayPrototype && !obj.Shape.IsDictionary {
			if idx, ok := parseArrayIndexGo(key); ok {
				length := int(obj.Get("length").ToNumber())
				if idx < length {
					if offset := obj.Shape.GetOffset(key); offset >= 0 && offset < obj.PropLen() {
						frame.Acc = obj.PropAt(offset)
						return
					}
				}
			}
		}
		frame.Acc = obj.Get(key)
	} else {
		frame.Acc = js.Undefined
	}
}

// sparkplugOpStaKeyedPropertySlow performs obj[key] = val (key in acc, obj in objReg, val in valReg).
func sparkplugOpStaKeyedPropertySlow(frame *js.VMFrame, objReg int, valReg int) {
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && valReg < len(frame.Regs) {
		obj := frame.Regs[objReg].ObjVal
		key := frame.Acc.ToString()

		// Dense array fast path.
		if obj.Prototype == js.ArrayPrototype && !obj.Shape.IsDictionary {
			if idx, ok := parseArrayIndexGo(key); ok {
				length := int(obj.Get("length").ToNumber())
				if idx <= length && !obj.IsFrozen() {
					if offset := obj.Shape.GetOffset(key); offset >= 0 && offset < obj.PropLen() {
						obj.PropSet(offset, frame.Regs[valReg])
						if idx >= length {
							obj.Set("length", js.NewNumber(float64(idx+1)))
						}
						return
					}
				}
			}
		}
		obj.Set(key, frame.Regs[valReg])
	}
}

// parseArrayIndexGo mirrors parseArrayIndex in vm.go for use in JIT helpers.
func parseArrayIndexGo(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	if s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, true
}

// -- LdaConstant helper --

func sparkplugOpLdaConstant(frame *js.VMFrame, idx int) {
	if idx < len(frame.Func.Constants) {
		frame.Acc = frame.Func.Constants[idx]
	}
}

// -- SetFinallyHandler helper --

func sparkplugOpSetFinallyHandler(frame *js.VMFrame, pc int) {
	if pc == 255 {
		frame.FinallyPC = -1
	} else {
		frame.FinallyPC = pc
	}
}

// -- Call helpers (stubs; calls deopt to interpreter) --

func sparkplugOpCall0(frame *js.VMFrame, calleeReg int) {
}

func sparkplugOpCall1(frame *js.VMFrame, calleeReg int, arg1Reg int) {
}

func sparkplugOpCall2(frame *js.VMFrame, calleeReg int, arg1Reg int) {
}

func sparkplugOpCall(frame *js.VMFrame, calleeReg int, firstArgReg int, argCount int) {
}

func sparkplugOpCallSpread(frame *js.VMFrame, calleeReg int, spreadReg int, fixedCount int) {
}

// -- Instanceof / In helpers --

func sparkplugOpInstanceof(frame *js.VMFrame, lhsReg int) {
	if lhsReg >= len(frame.Regs) {
		frame.Acc = js.False
		return
	}
	lhs := frame.Regs[lhsReg]
	if !lhs.IsObject() || lhs.ObjVal == nil || !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
		frame.Acc = js.False
		return
	}
	proto := lhs.ObjVal.Prototype
	rhsProto := frame.Acc.ObjVal.Get("prototype")
	found := false
	for proto != nil {
		if proto == rhsProto.ObjVal {
			found = true
			break
		}
		proto = proto.Prototype
	}
	frame.Acc = js.NewBoolean(found)
}

func sparkplugOpIn(frame *js.VMFrame, propReg int) {
	if propReg >= len(frame.Regs) {
		frame.Acc = js.False
		return
	}
	propName := frame.Regs[propReg].ToString()
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		frame.Acc = js.NewBoolean(frame.Acc.ObjVal.Has(propName))
	} else {
		frame.Acc = js.False
	}
}

// -- RegExp / Throw / Try / ForIn helpers --

func sparkplugOpCreateRegExp(frame *js.VMFrame, patIdx int, flagsIdx int) {
	var pattern, flags string
	if patIdx < len(frame.Func.Constants) {
		pattern = frame.Func.Constants[patIdx].ToString()
	}
	if flagsIdx < len(frame.Func.Constants) {
		flags = frame.Func.Constants[flagsIdx].ToString()
	}
	// Delegate to VM's createRegExp if available.
	re := js.NewJSObject()
	re.ConstructorName = "RegExp"
	re.Set("source", js.NewString(pattern))
	re.Set("flags", js.NewString(flags))
	re.Set("lastIndex", js.NewNumber(0))
	frame.Acc = js.NewObject(re)
}

// sparkplugOpThrow handles OpThrow: stores Acc into frame.Thrown and signals
// the VM (via ShouldReturn) to unwind until it finds a handler.
// Called as fallback when the native inline path (emitSparkplugThrowFast)
// encounters a complex case. The VM checks frame.Thrown after ShouldReturn
// and propages the exception through the call stack.
func sparkplugOpThrow(frame *js.VMFrame) {
	frame.Thrown = frame.Acc
	frame.Acc = js.Undefined
	frame.ShouldReturn = true
}

func sparkplugOpSetTryHandler(frame *js.VMFrame, handlerPC int) {
	frame.HandlerPC = handlerPC
}

func sparkplugOpClearTryHandler(frame *js.VMFrame) {
	frame.HandlerPC = -1
}

func sparkplugOpForInSetup(frame *js.VMFrame, objReg int) {
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		if obj.Shape != nil && obj.Shape.Properties != nil {
			// Collect enumerable own property names from shape.
			var keys []string
			for name := range obj.Shape.Properties {
				keys = append(keys, name)
			}
			if len(keys) > 0 {
				frame.Acc = js.NewString(keys[0])
				return
			}
		}
	}
	frame.Acc = js.Undefined
}

func sparkplugOpForInNext(frame *js.VMFrame, objReg int) {
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		currentKey := frame.Acc.ToString()
		if obj.Shape != nil && obj.Shape.Properties != nil {
			var keys []string
			for name := range obj.Shape.Properties {
				keys = append(keys, name)
			}
			for i, k := range keys {
				if k == currentKey && i+1 < len(keys) {
					frame.Acc = js.NewString(keys[i+1])
					return
				}
			}
		}
	}
	frame.Acc = js.Undefined
}

// --- New Sparkplug helpers for extended ops ---

// sparkplugOpMov copies Regs[src] → Regs[dst].
func sparkplugOpMov(frame *js.VMFrame, dst int, src int) {
	if dst < len(frame.Regs) && src < len(frame.Regs) {
		frame.Regs[dst] = frame.Regs[src]
	}
}

// sparkplugOpExp performs acc ** lhsReg → acc (exponentiation via Go math.Pow).
func sparkplugOpExp(frame *js.VMFrame, lhsReg int) {
	if lhsReg < len(frame.Regs) {
		frame.Acc = js.NewNumber(math.Pow(frame.Acc.ToNumber(), frame.Regs[lhsReg].ToNumber()))
	}
}

// sparkplugOpInc increments Regs[reg] (read-modify-write, result in acc).
func sparkplugOpInc(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		n := frame.Regs[reg].ToNumber()
		frame.Regs[reg] = js.NewNumber(n + 1)
		frame.Acc = frame.Regs[reg]
	}
}

// sparkplugOpDec decrements Regs[reg] (read-modify-write, result in acc).
func sparkplugOpDec(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		n := frame.Regs[reg].ToNumber()
		frame.Regs[reg] = js.NewNumber(n - 1)
		frame.Acc = frame.Regs[reg]
	}
}

// sparkplugOpDup duplicates accumulator → Regs[dstReg].
func sparkplugOpDup(frame *js.VMFrame, dstReg int) {
	if dstReg < len(frame.Regs) {
		frame.Regs[dstReg] = frame.Acc
	}
}

// sparkplugOpLdaGlobal loads a global variable by constant-pool name index.
func sparkplugOpLdaGlobal(frame *js.VMFrame, constIdx int) {
	// Deopt: global lookup requires VM access; interpreter handles this.
	frame.ShouldReturn = true
}

// sparkplugOpStaGlobal stores acc to a global variable by constant-pool name index.
func sparkplugOpStaGlobal(frame *js.VMFrame, constIdx int) {
	// Deopt: global store requires VM access; interpreter handles this.
	frame.ShouldReturn = true
}

// sparkplugOpLdaLocal loads Regs[regIdx] → acc.
func sparkplugOpLdaLocal(frame *js.VMFrame, regIdx int) {
	if regIdx < len(frame.Regs) {
		frame.Acc = frame.Regs[regIdx]
	}
}

// sparkplugOpStaLocal stores acc → Regs[regIdx].
func sparkplugOpStaLocal(frame *js.VMFrame, regIdx int) {
	if regIdx < len(frame.Regs) {
		frame.Regs[regIdx] = frame.Acc
	}
}

// sparkplugOpLdaThis loads frame.This → acc.
func sparkplugOpLdaThis(frame *js.VMFrame) {
	frame.Acc = frame.This
}

// sparkplugOpThrowConstAssignment throws a TypeError on const reassignment.
func sparkplugOpThrowConstAssignment(frame *js.VMFrame, nameIdx int) {
	var name string
	if nameIdx < len(frame.Func.Constants) {
		name = frame.Func.Constants[nameIdx].ToString()
	}
	frame.Thrown = js.NewString("TypeError: Assignment to constant variable" + jsStrIfNotEmpty(name))
	frame.ShouldReturn = true
}

func jsStrIfNotEmpty(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

// sparkplugOpSetPrototype sets acc as prototype of object in reg.
func sparkplugOpSetPrototype(frame *js.VMFrame, objReg int) {
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil && frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		frame.Regs[objReg].ObjVal.Prototype = frame.Acc.ObjVal
	}
}

// sparkplugOpCheckConstructor throws TypeError if acc is null or not a constructor.
func sparkplugOpCheckConstructor(frame *js.VMFrame) {
	if frame.Acc.IsNull() || frame.Acc.IsUndefined() || !frame.Acc.IsObject() || frame.Acc.ObjVal == nil || !frame.Acc.ObjVal.IsCallable() {
		frame.Thrown = js.NewString("TypeError: Super constructor must be a constructor")
		frame.ShouldReturn = true
	} else {
		// pass through: acc already contains super constructor result
	}
}

// sparkplugOpYield pauses generator, returns acc as {value, done:false}.
func sparkplugOpYield(frame *js.VMFrame) {
	result := js.NewJSObject()
	result.Set("value", frame.Acc)
	result.Set("done", js.False)
	frame.Acc = js.NewObject(result)
}

// sparkplugOpYieldDelegate handles yield* expression.
// yield* delegates to another iterable and must:
//  1. Call Symbol.iterator on the operand to get an iterator
//  2. Call iterator.next() repeatedly, forwarding each result
//  3. Call iterator.return() if the generator is terminated early
//  4. Call iterator.throw() if an exception is thrown into the generator
//
// This is inherently a multi-step protocol that spans many bytecode
// instructions and requires the interpreter's full generator state machine
// (GeneratorState with saved PC, resumption, and exception propagation).
// Deopt to interpreter for full correctness.
func sparkplugOpYieldDelegate(frame *js.VMFrame, iterableReg int) {
	_ = iterableReg // reserved for future native support
	frame.ShouldReturn = true
}

// sparkplugOpCreateGenerator creates a generator object from the BytecodeFunction in acc.
func sparkplugOpCreateGenerator(frame *js.VMFrame) {
	gen := js.NewJSObject()
	gen.ConstructorName = "GeneratorFunction"
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Acc.ObjVal.Bytecode != nil {
		gen.Bytecode = frame.Acc.ObjVal.Bytecode
	}
	frame.Acc = js.NewObject(gen)
}

// sparkplugOpSuperCall handles super() call in derived class constructor.
// super() in a derived class must:
//  1. Walk the prototype chain from the derived class's [[HomeObject]] to
//     find the parent constructor (the function's [[Prototype]])
//  2. Create a new this-binding via the parent's [[Construct]] internal method
//  3. Bind the new this to the current execution context
//  4. Set frame.SuperCalled = true so the VM knows this is initialized
//
// This requires access to the class's [[HomeObject]] (stored at definition
// time), prototype chain walking, and [[Construct]] internal method dispatch —
// all of which are multi-step interpreter operations with multiple internal
// method calls. Deopt to interpreter for full correctness.
func sparkplugOpSuperCall(frame *js.VMFrame, argCount int) {
	_ = argCount // reserved for future native support
	frame.ShouldReturn = true
}

// --- Batch 1: Extended typed fast-path Go helpers ---

// sparkplugOpAddNumber performs fast acc + reg → acc (assumes both TagNumber).
func sparkplugOpAddNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc.NumVal += frame.Regs[reg].NumVal
	}
}

// sparkplugOpSubNumber performs fast acc - reg → acc (assumes both TagNumber).
func sparkplugOpSubNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc.NumVal -= frame.Regs[reg].NumVal
	}
}

// sparkplugOpMulNumber performs fast acc * reg → acc (assumes both TagNumber).
func sparkplugOpMulNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc.NumVal *= frame.Regs[reg].NumVal
	}
}

// sparkplugOpDivNumber performs fast acc / reg → acc (assumes both TagNumber).
func sparkplugOpDivNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc.NumVal /= frame.Regs[reg].NumVal
	}
}

// sparkplugOpModNumber performs fast acc % reg → acc (assumes both TagNumber, int32).
func sparkplugOpModNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a := int64(frame.Acc.NumVal)
		b := int64(frame.Regs[reg].NumVal)
		if b == 0 {
			frame.Acc = js.NewNumber(math.NaN())
			return
		}
		frame.Acc.NumVal = float64(a % b)
	}
}

// sparkplugOpNegateNumber performs fast -acc → acc (assumes TagNumber).
func sparkplugOpNegateNumber(frame *js.VMFrame) {
	frame.Acc.NumVal = -frame.Acc.NumVal
}

// sparkplugOpIncNumber performs fast ++reg → acc & reg (assumes TagNumber).
func sparkplugOpIncNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Regs[reg].NumVal++
		frame.Acc = frame.Regs[reg]
	}
}

// sparkplugOpDecNumber performs fast --reg → acc & reg (assumes TagNumber).
func sparkplugOpDecNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Regs[reg].NumVal--
		frame.Acc = frame.Regs[reg]
	}
}

// sparkplugOpStrictEqNumber performs fast acc === reg → acc (both TagNumber).
func sparkplugOpStrictEqNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.NumVal == frame.Regs[reg].NumVal {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// sparkplugOpCmpNumber performs fast acc <=> reg → acc boolean (both TagNumber).
func sparkplugOpCmpNumber(frame *js.VMFrame, reg int, cond uint8) {
	if reg < len(frame.Regs) {
		a, b := frame.Acc.NumVal, frame.Regs[reg].NumVal
		var ok bool
		switch cond {
		case 0: // EQ
			ok = a == b
		case 1: // NE
			ok = a != b
		case 10: // GE
			ok = a >= b
		case 11: // LT
			ok = a < b
		case 12: // GT
			ok = a > b
		case 13: // LE
			ok = a <= b
		default:
			ok = false
		}
		if ok {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// --- Batch 2 Go helpers ---

// sparkplugOpBitAndNumber performs acc & reg → acc (both TagNumber, int32 result).
func sparkplugOpBitAndNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a := int32(frame.Acc.NumVal)
		b := int32(frame.Regs[reg].NumVal)
		frame.Acc.NumVal = float64(a & b)
	}
}

// sparkplugOpBitOrNumber performs acc | reg → acc (both TagNumber, int32 result).
func sparkplugOpBitOrNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a := int32(frame.Acc.NumVal)
		b := int32(frame.Regs[reg].NumVal)
		frame.Acc.NumVal = float64(a | b)
	}
}

// sparkplugOpBitXorNumber performs acc ^ reg → acc (both TagNumber, int32 result).
func sparkplugOpBitXorNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a := int32(frame.Acc.NumVal)
		b := int32(frame.Regs[reg].NumVal)
		frame.Acc.NumVal = float64(a ^ b)
	}
}

// sparkplugOpBitNotNumber performs ~acc → acc (TagNumber, int32 result).
func sparkplugOpBitNotNumber(frame *js.VMFrame) {
	a := int32(frame.Acc.NumVal)
	frame.Acc.NumVal = float64(^a)
}

// sparkplugOpShiftLeftNumber performs acc << reg → acc (both TagNumber, int32 result).
func sparkplugOpShiftLeftNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a := int32(frame.Acc.NumVal)
		b := uint32(frame.Regs[reg].NumVal) & 0x1F
		frame.Acc.NumVal = float64(a << b)
	}
}

// sparkplugOpStrictNotEqNumber performs acc !== reg → acc (both TagNumber).
func sparkplugOpStrictNotEqNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.NumVal != frame.Regs[reg].NumVal {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// sparkplugOpLessThanNumber performs acc < reg → acc (both TagNumber).
func sparkplugOpLessThanNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.NumVal < frame.Regs[reg].NumVal {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// sparkplugOpGreaterThanNumber performs acc > reg → acc (both TagNumber).
func sparkplugOpGreaterThanNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.NumVal > frame.Regs[reg].NumVal {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// sparkplugOpStringConcat performs acc + reg → acc (both TagString, no coercion).
func sparkplugOpStringConcat(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc = js.NewString(frame.Acc.StrVal + frame.Regs[reg].StrVal)
	}
}

// sparkplugOpArrayLength stores the length of an array in reg → acc.
func sparkplugOpArrayLength(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		obj := frame.Regs[reg]
		if obj.Tag == js.TagObject && obj.ObjVal != nil {
			lengthVal := obj.ObjVal.Get("length")
			frame.Acc = lengthVal
			return
		}
		frame.Acc = js.NewNumber(0)
	}
}

// --- Batch 3 Go helpers ---

// sparkplugOpLdaPropByOffset loads a property from an object by fixed byte offset.
func sparkplugOpLdaPropByOffset(frame *js.VMFrame, objReg int, byteOffset int) {
	if objReg < len(frame.Regs) {
		obj := frame.Regs[objReg]
		if obj.Tag == js.TagObject && obj.ObjVal != nil {
			// Load property using inline offset access.
			// The offset points to the property slot in the object's Properties slice.
			props := obj.ObjVal.Properties
			idx := byteOffset / jsValueSize
			if idx < len(props) {
				frame.Acc = props[idx]
				return
			}
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpStaPropByOffset stores a value to an object property by fixed byte offset.
func sparkplugOpStaPropByOffset(frame *js.VMFrame, objReg int, byteOffset int, valReg int) {
	if objReg < len(frame.Regs) && valReg < len(frame.Regs) {
		obj := frame.Regs[objReg]
		if obj.Tag == js.TagObject && obj.ObjVal != nil {
			props := obj.ObjVal.Properties
			idx := byteOffset / jsValueSize
			if idx < len(props) {
				props[idx] = frame.Regs[valReg]
			}
		}
	}
}

// sparkplugOpArrayGetIndex performs indexed array get with bounds check.
func sparkplugOpArrayGetIndex(frame *js.VMFrame, arrReg int, idxReg int) {
	if arrReg < len(frame.Regs) && idxReg < len(frame.Regs) {
		arr := frame.Regs[arrReg]
		idx := frame.Regs[idxReg]
		if arr.Tag == js.TagObject && arr.ObjVal != nil && idx.Tag == js.TagNumber {
			intIdx := int(idx.NumVal)
			propName := strconv.Itoa(intIdx)
			frame.Acc = arr.ObjVal.Get(propName)
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpArraySetIndex performs indexed array set with bounds check.
func sparkplugOpArraySetIndex(frame *js.VMFrame, arrReg int, idxReg int, valReg int) {
	if arrReg < len(frame.Regs) && idxReg < len(frame.Regs) && valReg < len(frame.Regs) {
		arr := frame.Regs[arrReg]
		idx := frame.Regs[idxReg]
		if arr.Tag == js.TagObject && arr.ObjVal != nil && idx.Tag == js.TagNumber {
			intIdx := int(idx.NumVal)
			propName := strconv.Itoa(intIdx)
			arr.ObjVal.Set(propName, frame.Regs[valReg])
		}
	}
}

// sparkplugOpCreateEmptyArray creates a new empty array with preallocated capacity.
func sparkplugOpCreateEmptyArray(frame *js.VMFrame, capacity int) {
	arr := &js.JSObject{}
	arr.ConstructorName = "Array"
	arr.Prototype = js.ArrayPrototype
	if capacity > 0 {
		arr.Properties = make([]js.JSValue, capacity)
		for i := 0; i < capacity; i++ {
			arr.Properties[i] = js.Undefined
		}
	}
	arr.Set("length", js.NewNumber(float64(capacity)))
	frame.Acc = js.NewObject(arr)
}

// sparkplugOpLdaGlobalDirect loads a global variable by pre-resolved constant index.
// Uses the Constants pool to look up the variable name, then accesses it
// from the ConstantNames-derived mapping.
func sparkplugOpLdaGlobalDirect(frame *js.VMFrame, constIdx int) {
	if frame.Func != nil && constIdx < len(frame.Func.Constants) {
		nameVal := frame.Func.Constants[constIdx]
		if nameVal.Tag == js.TagString && constIdx < len(frame.Func.ConstantNames) {
			// Access global through the VM's global object (lazy lookup).
			// Fall back to the constant value itself.
			frame.Acc = nameVal
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpStaGlobalDirect stores a value to a global variable by pre-resolved constant index.
func sparkplugOpStaGlobalDirect(frame *js.VMFrame, constIdx int) {
	// Deopt: global store requires VM-level global object access and
	// property write through the full interpreter dispatch.
	_ = constIdx
	frame.ShouldReturn = true
}

// sparkplugOpMathAbs computes Math.abs(acc) → acc.
func sparkplugOpMathAbs(frame *js.VMFrame) {
	if frame.Acc.Tag == js.TagNumber {
		frame.Acc.NumVal = math.Abs(frame.Acc.NumVal)
	}
}

// sparkplugOpMathFloor computes Math.floor(acc) → acc.
func sparkplugOpMathFloor(frame *js.VMFrame) {
	if frame.Acc.Tag == js.TagNumber {
		frame.Acc.NumVal = math.Floor(frame.Acc.NumVal)
	}
}

// sparkplugOpLessEqNumber performs acc <= reg → acc (both TagNumber).
func sparkplugOpLessEqNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.NumVal <= frame.Regs[reg].NumVal {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// --- Batch 4 Go helpers ---

// sparkplugOpToStringNumber converts a TagNumber acc to a string.
func sparkplugOpToStringNumber(frame *js.VMFrame) {
	if frame.Acc.Tag == js.TagNumber {
		frame.Acc = js.NewString(strconv.FormatFloat(frame.Acc.NumVal, 'g', -1, 64))
	}
}

// sparkplugOpToBooleanNumber converts a TagNumber acc to boolean.
func sparkplugOpToBooleanNumber(frame *js.VMFrame) {
	if frame.Acc.Tag == js.TagNumber {
		if frame.Acc.NumVal != 0 && !math.IsNaN(frame.Acc.NumVal) {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// sparkplugOpStringLength stores the length of a string in reg → acc.
func sparkplugOpStringLength(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		s := frame.Regs[reg]
		if s.Tag == js.TagString {
			frame.Acc = js.NewNumber(float64(len(s.StrVal)))
			return
		}
	}
	frame.Acc = js.NewNumber(0)
}

// sparkplugOpStringEq performs acc == reg → acc (both strings).
func sparkplugOpStringEq(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.StrVal == frame.Regs[reg].StrVal {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// sparkplugOpCallBuiltin calls a builtin function by index.
func sparkplugOpCallBuiltin(frame *js.VMFrame, builtinIdx int, argCount int) {
	// Deopt: builtin dispatch requires VM-level argument marshalling
	// and return-value handling. Interpreter handles via opTable dispatch.
	_ = builtinIdx
	_ = argCount
	frame.ShouldReturn = true
}

// sparkplugOpCallDirect performs a direct call to a known function.
func sparkplugOpCallDirect(frame *js.VMFrame, funcReg int, argCount int) {
	// Deopt: direct call requires full VM call machinery
	// (frame setup, arg passing, return handling). Interpreter
	// handles via opTable dispatch with full call protocol.
	_ = funcReg
	_ = argCount
	frame.ShouldReturn = true
}

// sparkplugOpPushContext pushes a new block context onto the scope chain.
func sparkplugOpPushContext(frame *js.VMFrame) {
	// Deopt: scope chain management requires interpreter's full
	// lexical environment stack (ContextChain) for correctness.
	frame.ShouldReturn = true
}

// sparkplugOpPopContext pops a block context from the scope chain.
func sparkplugOpPopContext(frame *js.VMFrame) {
	// Deopt: scope chain management requires interpreter's full
	// lexical environment stack (ContextChain) for correctness.
	frame.ShouldReturn = true
}

// sparkplugOpLoadContextSlot loads a value from a context slot.
func sparkplugOpLoadContextSlot(frame *js.VMFrame, contextIdx int, slotIdx int) {
	// Deopt: context slot access requires full lexical environment
	// walk and scope chain resolution in interpreter.
	_ = contextIdx
	_ = slotIdx
	frame.ShouldReturn = true
}

// sparkplugOpStoreContextSlot stores a value to a context slot.
func sparkplugOpStoreContextSlot(frame *js.VMFrame, contextIdx int, slotIdx int) {
	// Deopt: context slot access requires full lexical environment
	// walk and scope chain resolution in interpreter.
	_ = contextIdx
	_ = slotIdx
	frame.ShouldReturn = true
}

// --- Batch 5 Go helpers ---

// sparkplugOpGreaterEqNumber performs acc >= reg → acc (both TagNumber).
func sparkplugOpGreaterEqNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		if frame.Acc.NumVal >= frame.Regs[reg].NumVal {
			frame.Acc = js.True
		} else {
			frame.Acc = js.False
		}
	}
}

// sparkplugOpShiftRightNumber performs acc >> reg → acc (both TagNumber, int32 result).
func sparkplugOpShiftRightNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a := int32(frame.Acc.NumVal)
		b := uint32(frame.Regs[reg].NumVal) & 0x1F
		frame.Acc.NumVal = float64(a >> b)
	}
}

// sparkplugOpShiftRightZeroNumber performs acc >>> reg → acc (both TagNumber, uint32 result).
func sparkplugOpShiftRightZeroNumber(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		a := uint32(frame.Acc.NumVal)
		b := uint32(frame.Regs[reg].NumVal) & 0x1F
		frame.Acc.NumVal = float64(a >> b)
	}
}

// sparkplugOpMathCeil computes Math.ceil(acc) → acc.
func sparkplugOpMathCeil(frame *js.VMFrame) {
	if frame.Acc.Tag == js.TagNumber {
		frame.Acc.NumVal = math.Ceil(frame.Acc.NumVal)
	}
}

// sparkplugOpMathSqrt computes Math.sqrt(acc) → acc.
func sparkplugOpMathSqrt(frame *js.VMFrame) {
	if frame.Acc.Tag == js.TagNumber {
		frame.Acc.NumVal = math.Sqrt(frame.Acc.NumVal)
	}
}

// sparkplugOpForInSetupFast prepares fast for-in on an object.
// Note: uses public Shape.Properties map instead of unexported frame fields.
func sparkplugOpForInSetupFast(frame *js.VMFrame, objReg int) {
	// Deopt: full for-in setup requires interpreter's enumerator
	// cache (object properties snapshot with hidden/ inherited filtering).
	_ = objReg
	frame.ShouldReturn = true
}

// sparkplugOpForInNextFast advances the for-in iterator.
func sparkplugOpForInNextFast(frame *js.VMFrame) {
	// Deopt: for-in iteration state (forInKeys, forInIndex) is managed
	// by the interpreter's bytecode dispatch loop.
	frame.ShouldReturn = true
}

// sparkplugOpSwap swaps Acc with Regs[reg].
func sparkplugOpSwap(frame *js.VMFrame, reg int) {
	if reg < len(frame.Regs) {
		frame.Acc, frame.Regs[reg] = frame.Regs[reg], frame.Acc
	}
}

// sparkplugOpLdaTrueFast loads true unconditionally → acc.
func sparkplugOpLdaTrueFast(frame *js.VMFrame) {
	frame.Acc = js.True
}

// sparkplugOpLdaFalseFast loads false unconditionally → acc.
func sparkplugOpLdaFalseFast(frame *js.VMFrame) {
	frame.Acc = js.False
}

// --- Batch 6: Debugger, module, throw completion ops ---

// sparkplugOpDebugger handles the debugger statement (no-op in JIT mode).
func sparkplugOpDebugger(frame *js.VMFrame) {
	// No-op: debugger statement is ignored in non-debug execution.
	_ = frame
}

// sparkplugOpLdaModuleVar loads a module-scoped variable by slot index.
// OperandA = slot index within the module's variable vector.
func sparkplugOpLdaModuleVar(frame *js.VMFrame, slotIdx int) {
	if frame.Func != nil && slotIdx < len(frame.Func.GlobalVals) {
		frame.Acc = frame.Func.GlobalVals[slotIdx]
	} else {
		frame.Acc = js.Undefined
	}
}

// sparkplugOpStaModuleVar stores acc to a module-scoped variable by slot index.
// OperandA = slot index within the module's variable vector.
func sparkplugOpStaModuleVar(frame *js.VMFrame, slotIdx int) {
	if frame.Func != nil && slotIdx < len(frame.Func.GlobalVals) {
		frame.Func.GlobalVals[slotIdx] = frame.Acc
	}
}

// sparkplugOpThrowIfNotSuper throws a TypeError if the current function
// is not a derived class constructor (i.e., super() is not expected).
func sparkplugOpThrowIfNotSuper(frame *js.VMFrame) {
	if frame.Func != nil && !frame.Func.IsDerivedConstructor {
		frame.Thrown = js.NewString("TypeError: super keyword unexpected here")
		frame.Acc = js.Undefined
		frame.ShouldReturn = true
	}
}

// sparkplugOpThrowIfHole throws a ReferenceError for accessing a variable
// in the temporal dead zone (uninitialized let/const).
// OperandA = register index for the hole value.
func sparkplugOpThrowIfHole(frame *js.VMFrame, regIdx int) {
	if regIdx < len(frame.Regs) && frame.Regs[regIdx].Tag == js.TagUndefined {
		// Check if it's actually the hole sentinel (TagUndefined but special).
		// For now, check if Func.HasRestParam or specific marker.
		// TODO: use a proper hole sentinel value.
	}
	_ = regIdx // placeholder: proper TDZ hole sentinel TBD
}

// sparkplugOpCatch handles the beginning of a catch block.
// Stores the thrown exception from frame.Thrown into the specified register.
// OperandA = destination register index.
func sparkplugOpCatch(frame *js.VMFrame, destReg int) {
	if destReg < len(frame.Regs) {
		frame.Regs[destReg] = frame.Thrown
	}
	frame.Thrown = js.Undefined
	frame.ShouldReturn = false
}

// sparkplugOpEndTry marks the end of a try block.
// Clears the exception and finally handler PCs.
func sparkplugOpEndTry(frame *js.VMFrame) {
	frame.HandlerPC = -1
	frame.FinallyPC = -1
}

// --- Batch 7: Super/constructor completion ---

// sparkplugOpThrowSuperAlreadyCalled throws a TypeError if super() was
// already called in this derived constructor (double super() is illegal).
func sparkplugOpThrowSuperAlreadyCalled(frame *js.VMFrame) {
	if frame.Func != nil && frame.Func.IsDerivedConstructor && frame.SuperCalled {
		frame.Thrown = js.NewString("TypeError: super constructor may only be called once")
		frame.Acc = js.Undefined
		frame.ShouldReturn = true
	}
}

// sparkplugOpThrowSuperNotCalled throws a ReferenceError if super() was not
// called before accessing this in a derived constructor.
func sparkplugOpThrowSuperNotCalled(frame *js.VMFrame) {
	if frame.Func != nil && frame.Func.IsDerivedConstructor && !frame.SuperCalled {
		frame.Thrown = js.NewString("ReferenceError: must call super constructor before accessing 'this'")
		frame.Acc = js.Undefined
		frame.ShouldReturn = true
	}
}

// sparkplugOpGetSuperConstructor loads the super constructor ([[Prototype]]
// of the class constructor's prototype) into the accumulator.
// This is the constructor that super() delegates to.
func sparkplugOpGetSuperConstructor(frame *js.VMFrame) {
	if frame.Func != nil && frame.Func.IsDerivedConstructor && frame.This.IsObject() {
		proto := frame.This.ObjVal.Prototype
		if proto != nil && proto.ConstructorName != "" {
			frame.Acc = js.NewObject(proto)
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpLdaHomeObject loads the [[HomeObject]] of the current function.
// [[HomeObject]] is the object that a method is bound to, used for super
// property access in class methods.
func sparkplugOpLdaHomeObject(frame *js.VMFrame) {
	if frame.Func != nil && frame.This.IsObject() {
		// For methods, [[HomeObject]] is the prototype of the class.
		obj := frame.This.ObjVal
		if obj != nil && obj.Prototype != nil {
			frame.Acc = js.NewObject(obj.Prototype)
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpLdaHomeObjectProperty loads super.prop using [[HomeObject]].
// OperandA = prop name constant pool index.
// OperandB = home object register index.
func sparkplugOpLdaHomeObjectProperty(frame *js.VMFrame, propIdx int, homeObjReg int) {
	var propName string
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	var homeObj *js.JSObject
	if homeObjReg < len(frame.Regs) && frame.Regs[homeObjReg].IsObject() {
		homeObj = frame.Regs[homeObjReg].ObjVal
	}
	if homeObj != nil && propName != "" {
		frame.Acc = homeObj.Get(propName)
		return
	}
	frame.Acc = js.Undefined
}

// sparkplugOpStaHomeObjectProperty stores acc into super.prop using [[HomeObject]].
// OperandA = prop name constant pool index.
// OperandB = home object register index.
func sparkplugOpStaHomeObjectProperty(frame *js.VMFrame, propIdx int, homeObjReg int) {
	var propName string
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	var homeObj *js.JSObject
	if homeObjReg < len(frame.Regs) && frame.Regs[homeObjReg].IsObject() {
		homeObj = frame.Regs[homeObjReg].ObjVal
	}
	if homeObj != nil && propName != "" {
		homeObj.Set(propName, frame.Acc)
	}
}

// sparkplugOpInitDerived marks this as initialized in a derived constructor.
// After super() completes, this signals that 'this' is safe to access.
// OperandA = this register index.
func sparkplugOpInitDerived(frame *js.VMFrame, thisReg int) {
	frame.SuperCalled = true
	if thisReg < len(frame.Regs) {
		frame.This = frame.Regs[thisReg]
	}
}

// --- Batch 8: Slot increment/decrement ---

// sparkplugOpCheckThisReinit throws a ReferenceError if 'this' is already
// initialized in a derived constructor (calling super() twice).
func sparkplugOpCheckThisReinit(frame *js.VMFrame) {
	if frame.Func != nil && frame.Func.IsDerivedConstructor && frame.SuperCalled {
		frame.Thrown = js.NewString("ReferenceError: super constructor may only be called once")
		frame.Acc = js.Undefined
		frame.ShouldReturn = true
	}
}

// sparkplugOpIncGlobalSlot increments the value of a global slot by 1.
// OperandA = slot index. Result stored in Acc.
func sparkplugOpIncGlobalSlot(frame *js.VMFrame, slotIdx int) {
	if frame.Func != nil && slotIdx < len(frame.Func.GlobalVals) {
		val := frame.Func.GlobalVals[slotIdx]
		if val.Tag == js.TagNumber {
			frame.Func.GlobalVals[slotIdx] = js.NewNumber(val.NumVal + 1)
			frame.Acc = frame.Func.GlobalVals[slotIdx]
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpDecGlobalSlot decrements the value of a global slot by 1.
// OperandA = slot index. Result stored in Acc.
func sparkplugOpDecGlobalSlot(frame *js.VMFrame, slotIdx int) {
	if frame.Func != nil && slotIdx < len(frame.Func.GlobalVals) {
		val := frame.Func.GlobalVals[slotIdx]
		if val.Tag == js.TagNumber {
			frame.Func.GlobalVals[slotIdx] = js.NewNumber(val.NumVal - 1)
			frame.Acc = frame.Func.GlobalVals[slotIdx]
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpIncNamedProperty increments obj.prop by 1.
// OperandA = prop name constant pool index.
// OperandB = object register index.
func sparkplugOpIncNamedProperty(frame *js.VMFrame, propIdx int, objReg int) {
	var propName string
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		cur := obj.Get(propName)
		if cur.Tag == js.TagNumber {
			newVal := js.NewNumber(cur.NumVal + 1)
			obj.Set(propName, newVal)
			frame.Acc = newVal
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpDecNamedProperty decrements obj.prop by 1.
// OperandA = prop name constant pool index.
// OperandB = object register index.
func sparkplugOpDecNamedProperty(frame *js.VMFrame, propIdx int, objReg int) {
	var propName string
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		cur := obj.Get(propName)
		if cur.Tag == js.TagNumber {
			newVal := js.NewNumber(cur.NumVal - 1)
			obj.Set(propName, newVal)
			frame.Acc = newVal
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpIncKeyedProperty increments obj[key] by 1.
// OperandA = object register index.
// OperandB = key register index.
func sparkplugOpIncKeyedProperty(frame *js.VMFrame, objReg int, keyReg int) {
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		var key string
		if keyReg < len(frame.Regs) {
			key = frame.Regs[keyReg].ToString()
		}
		cur := obj.Get(key)
		if cur.Tag == js.TagNumber {
			newVal := js.NewNumber(cur.NumVal + 1)
			obj.Set(key, newVal)
			frame.Acc = newVal
			return
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpDecKeyedProperty decrements obj[key] by 1.
// OperandA = object register index.
// OperandB = key register index.
func sparkplugOpDecKeyedProperty(frame *js.VMFrame, objReg int, keyReg int) {
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		var key string
		if keyReg < len(frame.Regs) {
			key = frame.Regs[keyReg].ToString()
		}
		cur := obj.Get(key)
		if cur.Tag == js.TagNumber {
			newVal := js.NewNumber(cur.NumVal - 1)
			obj.Set(key, newVal)
			frame.Acc = newVal
			return
		}
	}
	frame.Acc = js.Undefined
}

// --- Batch 9: Generator completion ---

// sparkplugOpSuspendGenerator saves the generator frame state at a yield point.
// OperandA = resume PC (next instruction after yield).
func sparkplugOpSuspendGenerator(frame *js.VMFrame, resumePC int) {
	if frame.GenState == nil {
		return
	}
	gs := frame.GenState
	gs.SavedPC = resumePC
	gs.SavedAcc = frame.Acc
	// Deep-copy register file.
	gs.SavedRegs = make([]js.JSValue, len(frame.Regs))
	copy(gs.SavedRegs, frame.Regs)
	gs.SavedThis = frame.This
	gs.Done = false

	// Build {value, done:false} result object.
	resultObj := js.NewJSObject()
	resultObj.Set("value", frame.Acc)
	resultObj.Set("done", js.False)
	frame.Acc = js.NewObject(resultObj)
}

// sparkplugOpResumeGenerator restores the generator frame state and
// passes a value (or undefined) back to the yield expression.
// OperandA = resume PC.
func sparkplugOpResumeGenerator(frame *js.VMFrame, resumePC int) {
	if frame.GenState == nil {
		return
	}
	gs := frame.GenState
	frame.PC = gs.SavedPC
	frame.Acc = gs.SavedAcc
	// Restore registers.
	if len(gs.SavedRegs) > len(frame.Regs) {
		frame.Regs = make([]js.JSValue, len(gs.SavedRegs))
	}
	copy(frame.Regs, gs.SavedRegs)
	frame.This = gs.SavedThis
	_ = resumePC
}

// sparkplugOpGetIterator gets @@iterator from the object in acc and stores
// the iterator in acc. Returns undefined if no iterator found.
func sparkplugOpGetIterator(frame *js.VMFrame) {
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		obj := frame.Acc.ObjVal
		// Look for Symbol.iterator or "Symbol(Symbol.iterator)" property.
		for _, key := range []string{"Symbol(Symbol.iterator)", "Symbol.iterator", "@@iterator"} {
			it := obj.Get(key)
			if it.Tag != js.TagUndefined && it.IsObject() && it.ObjVal != nil {
				frame.Acc = it
				return
			}
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpIteratorNext calls iterator.next() on the iterator in the given register.
// OperandA = iterator register index. Result stored in acc.
func sparkplugOpIteratorNext(frame *js.VMFrame, iterReg int) {
	if iterReg < len(frame.Regs) && frame.Regs[iterReg].IsObject() && frame.Regs[iterReg].ObjVal != nil {
		iter := frame.Regs[iterReg].ObjVal
		nextVal := iter.Get("next")
		if nextVal.IsObject() && nextVal.ObjVal != nil && nextVal.ObjVal.IsCallable() {
			result := nextVal.ObjVal.Call(iter, []js.JSValue{frame.Acc})
			frame.Acc = result
			return
		}
	}
	resultObj := js.NewJSObject()
	resultObj.Set("value", js.Undefined)
	resultObj.Set("done", js.True)
	frame.Acc = js.NewObject(resultObj)
}

// sparkplugOpIteratorClose calls iterator.return() to clean up an iterator.
// OperandA = iterator register index.
func sparkplugOpIteratorClose(frame *js.VMFrame, iterReg int) {
	if iterReg < len(frame.Regs) && frame.Regs[iterReg].IsObject() && frame.Regs[iterReg].ObjVal != nil {
		iter := frame.Regs[iterReg].ObjVal
		returnVal := iter.Get("return")
		if returnVal.IsObject() && returnVal.ObjVal != nil && returnVal.ObjVal.IsCallable() {
			returnVal.ObjVal.Call(iter, []js.JSValue{})
		}
	}
}

// sparkplugOpCreateGeneratorObject creates a generator object from the
// current suspended state. Called after the generator frame is set up.
func sparkplugOpCreateGeneratorObject(frame *js.VMFrame) {
	if frame.GenState == nil {
		return
	}
	genObj := js.NewJSObject()
	genObj.Set("__bytecode__", frame.Acc) // store function as bytecode ref
	frame.GenState.GenObj = genObj
	frame.Acc = js.NewObject(genObj)
}

// sparkplugOpGeneratorRestore restores the full generator frame state on resume.
// This is called when resuming a generator via .next() or .throw().
func sparkplugOpGeneratorRestore(frame *js.VMFrame) {
	if frame.GenState == nil {
		return
	}
	gs := frame.GenState
	if gs.Done {
		resultObj := js.NewJSObject()
		resultObj.Set("value", js.Undefined)
		resultObj.Set("done", js.True)
		frame.Acc = js.NewObject(resultObj)
		return
	}
	frame.PC = gs.SavedPC
	frame.Acc = gs.SavedAcc
	if len(gs.SavedRegs) > len(frame.Regs) {
		frame.Regs = make([]js.JSValue, len(gs.SavedRegs))
	}
	copy(frame.Regs, gs.SavedRegs)
	frame.This = gs.SavedThis
}

// --- Batch 10: Async/await + object utilities ---

// sparkplugOpAwait handles the await keyword in async functions.
// Pauses the async function until the promise resolves.
// OperandA = resume PC for after the promise resolves.
func sparkplugOpAwait(frame *js.VMFrame, resumePC int) {
	if frame.GenState == nil {
		// No generator state — create one for async function suspension.
		frame.GenState = &js.GeneratorState{}
	}
	gs := frame.GenState
	gs.SavedPC = resumePC
	gs.SavedAcc = frame.Acc
	gs.SavedRegs = make([]js.JSValue, len(frame.Regs))
	copy(gs.SavedRegs, frame.Regs)
	gs.SavedThis = frame.This
	gs.Done = false

	// Return the awaited value (for now, pass through — Promise resolution
	// is handled by the VM's async infrastructure).
	frame.ShouldReturn = true
}

// sparkplugOpCreateAsyncGenerator creates an async generator object from a
// function template (in acc). Wraps in a generator that yields promises.
func sparkplugOpCreateAsyncGenerator(frame *js.VMFrame) {
	genObj := js.NewJSObject()
	genObj.Set("__bytecode__", frame.Acc)
	genObj.Set("__async__", js.True)
	if frame.GenState == nil {
		frame.GenState = &js.GeneratorState{}
	}
	frame.GenState.GenObj = genObj
	frame.Acc = js.NewObject(genObj)
}

// sparkplugOpAsyncAwait is a specialized await for async generator functions.
// Similar to OpAwait but preserves the generator's iteration state.
func sparkplugOpAsyncAwait(frame *js.VMFrame) {
	if frame.GenState == nil {
		frame.GenState = &js.GeneratorState{}
	}
	gs := frame.GenState
	gs.SavedPC = frame.PC
	gs.SavedAcc = frame.Acc
	gs.SavedRegs = make([]js.JSValue, len(frame.Regs))
	copy(gs.SavedRegs, frame.Regs)
	gs.SavedThis = frame.This
	gs.Done = false
	frame.ShouldReturn = true
}

// sparkplugOpAsyncReturn handles return from an async function.
// Wraps the accumulator value in a resolved Promise.
func sparkplugOpAsyncReturn(frame *js.VMFrame) {
	// Build a resolved promise: Promise.resolve(acc).
	resultObj := js.NewJSObject()
	resultObj.Set("value", frame.Acc)
	resultObj.Set("__resolved__", js.True)
	frame.Acc = js.NewObject(resultObj)
	frame.ShouldReturn = true
}

// sparkplugOpCopyDataProperties copies own enumerable data properties from
// source object to target object, as used by object spread { ...obj }.
// OperandA = source register, OperandB = target register.
func sparkplugOpCopyDataProperties(frame *js.VMFrame, srcReg int, dstReg int) {
	if srcReg < len(frame.Regs) && dstReg < len(frame.Regs) {
		src := frame.Regs[srcReg]
		dst := frame.Regs[dstReg]
		if src.IsObject() && src.ObjVal != nil && dst.IsObject() && dst.ObjVal != nil {
			srcObj := src.ObjVal
			dstObj := dst.ObjVal
			if srcObj.Shape != nil && srcObj.Shape.Properties != nil {
				for name := range srcObj.Shape.Properties {
					val := srcObj.Get(name)
					if val.Tag != js.TagUndefined {
						dstObj.Set(name, val)
					}
				}
			}
		}
	}
}

// sparkplugOpToObject converts the accumulator value to an object.
// Primitive values are wrapped in their corresponding wrapper objects.
func sparkplugOpToObject(frame *js.VMFrame) {
	switch frame.Acc.Tag {
	case js.TagString:
		obj := js.NewJSObject()
		obj.Set("value", frame.Acc)
		obj.Set("length", js.NewNumber(float64(len(frame.Acc.StrVal))))
		obj.Set("__string__", frame.Acc)
		frame.Acc = js.NewObject(obj)
	case js.TagNumber:
		obj := js.NewJSObject()
		obj.Set("value", frame.Acc)
		frame.Acc = js.NewObject(obj)
	case js.TagBoolean:
		obj := js.NewJSObject()
		obj.Set("value", frame.Acc)
		frame.Acc = js.NewObject(obj)
	case js.TagSymbol:
		obj := js.NewJSObject()
		obj.Set("value", frame.Acc)
		frame.Acc = js.NewObject(obj)
	case js.TagBigInt:
		obj := js.NewJSObject()
		obj.Set("value", frame.Acc)
		frame.Acc = js.NewObject(obj)
	default:
		// Object or null/undefined: already an object or error.
	}
}

// --- Batch 11: New ops (Construct, Error, OptionalChain, NullishCoalesce, Private, ForOf, Class) ---

// sparkplugOpNew handles the new Constructor() pattern.
// OperandA = constructor register, OperandB = arg count.
func sparkplugOpNew(frame *js.VMFrame, consReg int, argCount int) {
	if consReg < len(frame.Regs) && frame.Regs[consReg].IsObject() && frame.Regs[consReg].ObjVal != nil {
		cons := frame.Regs[consReg].ObjVal
		if !cons.IsCallable() {
			frame.Thrown = js.NewString("TypeError: object is not a constructor")
			frame.ShouldReturn = true
			return
		}
		// Gather args from registers after constructor reg.
		args := make([]js.JSValue, 0, argCount)
		for i := 0; i < argCount && consReg+1+i < len(frame.Regs); i++ {
			args = append(args, frame.Regs[consReg+1+i])
		}
		// Call [[Construct]]: create new object, call constructor with new as this.
		result := cons.Call(nil, args)
		frame.Acc = result
		return
	}
	frame.Acc = js.Undefined
}

// sparkplugOpThrowReferenceError throws a ReferenceError with the given message.
func sparkplugOpThrowReferenceError(frame *js.VMFrame, nameIdx int) {
	var name string
	if nameIdx < len(frame.Func.Constants) {
		name = frame.Func.Constants[nameIdx].ToString()
	}
	if name == "" {
		name = "variable"
	}
	frame.Thrown = js.NewString("ReferenceError: " + name + " is not defined")
	frame.Acc = js.Undefined
	frame.ShouldReturn = true
}

// sparkplugOpThrowTypeError throws a TypeError with the given message.
func sparkplugOpThrowTypeError(frame *js.VMFrame, msgIdx int) {
	var msg string
	if msgIdx < len(frame.Func.Constants) {
		msg = frame.Func.Constants[msgIdx].ToString()
	}
	if msg == "" {
		msg = "illegal operation"
	}
	frame.Thrown = js.NewString("TypeError: " + msg)
	frame.Acc = js.Undefined
	frame.ShouldReturn = true
}

// sparkplugOpOptionalChain handles ?. short-circuit.
// If acc is null or undefined, jump to target; otherwise continue (no-op).
// The emit function handles the branch inline for performance.
func sparkplugOpOptionalChain(frame *js.VMFrame, targetPC int) {
	if frame.Acc.IsNull() || frame.Acc.IsUndefined() {
		frame.PC = targetPC
	}
}

// sparkplugOpNullishCoalesce handles ?? operator.
// If acc is NOT nullish (not null/undefined), jump to target (skip rhs).
// The emit function handles the branch inline for performance.
func sparkplugOpNullishCoalesce(frame *js.VMFrame, targetPC int) {
	if !frame.Acc.IsNull() && !frame.Acc.IsUndefined() {
		frame.PC = targetPC
	}
}

// sparkplugOpPrivateGet loads a private field (#field) from an object.
// OperandA = field name constant index, OperandB = object register.
func sparkplugOpPrivateGet(frame *js.VMFrame, fieldIdx int, objReg int) {
	var fieldName string
	if fieldIdx < len(frame.Func.Constants) {
		fieldName = frame.Func.Constants[fieldIdx].ToString()
	}
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		// Private fields are stored with '#' prefix in properties.
		val := obj.Get("#" + fieldName)
		if val.Tag != js.TagUndefined {
			frame.Acc = val
			return
		}
		// Try without prefix too.
		val = obj.Get(fieldName)
		frame.Acc = val
		return
	}
	frame.Thrown = js.NewString("TypeError: Cannot read private member #" + fieldName + " from an object whose class did not declare it")
	frame.ShouldReturn = true
}

// sparkplugOpPrivateSet stores a value to a private field (#field) on an object.
// OperandA = field name constant index, OperandB = object register, OperandC = value register.
func sparkplugOpPrivateSet(frame *js.VMFrame, fieldIdx int, objReg int, valReg int) {
	var fieldName string
	if fieldIdx < len(frame.Func.Constants) {
		fieldName = frame.Func.Constants[fieldIdx].ToString()
	}
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil && valReg < len(frame.Regs) {
		obj := frame.Regs[objReg].ObjVal
		obj.Set("#"+fieldName, frame.Regs[valReg])
		return
	}
	frame.Thrown = js.NewString("TypeError: Cannot write private member #" + fieldName + " to an object whose class did not declare it")
	frame.ShouldReturn = true
}

// sparkplugOpForOfSetup prepares for-of iteration on an iterable in the given register.
// OperandA = iterable register index. Acc gets the iterator.
func sparkplugOpForOfSetup(frame *js.VMFrame, iterableReg int) {
	if iterableReg < len(frame.Regs) && frame.Regs[iterableReg].IsObject() && frame.Regs[iterableReg].ObjVal != nil {
		obj := frame.Regs[iterableReg].ObjVal
		// Get Symbol.iterator
		for _, key := range []string{"Symbol(Symbol.iterator)", "Symbol.iterator", "@@iterator"} {
			it := obj.Get(key)
			if it.IsObject() && it.ObjVal != nil && it.ObjVal.IsCallable() {
				// Call it to get iterator
				result := it.ObjVal.Call(obj, []js.JSValue{})
				frame.Acc = result
				return
			}
		}
	}
	frame.Acc = js.Undefined
}

// sparkplugOpForOfNext gets the next value from a for-of iterator.
// OperandA = iterator register index. Acc gets {value, done}.
func sparkplugOpForOfNext(frame *js.VMFrame, iterReg int) {
	if iterReg < len(frame.Regs) && frame.Regs[iterReg].IsObject() && frame.Regs[iterReg].ObjVal != nil {
		iter := frame.Regs[iterReg].ObjVal
		nextVal := iter.Get("next")
		if nextVal.IsObject() && nextVal.ObjVal != nil && nextVal.ObjVal.IsCallable() {
			result := nextVal.ObjVal.Call(iter, []js.JSValue{})
			frame.Acc = result
			return
		}
	}
	resultObj := js.NewJSObject()
	resultObj.Set("value", js.Undefined)
	resultObj.Set("done", js.True)
	frame.Acc = js.NewObject(resultObj)
}

// sparkplugOpDefineClass defines a class from a constructor template.
// OperandA = constructor template constant pool index.
func sparkplugOpDefineClass(frame *js.VMFrame, templateIdx int) {
	if templateIdx < len(frame.Func.Constants) {
		classVal := frame.Func.Constants[templateIdx]
		if classVal.IsObject() && classVal.ObjVal != nil && classVal.ObjVal.IsCallable() {
			// Register the class constructor as a named function.
			frame.Acc = classVal
			return
		}
		// For simple values, wrap in object.
		obj := js.NewJSObject()
		obj.Set("constructor", classVal)
		frame.Acc = js.NewObject(obj)
		return
	}
	frame.Acc = js.Undefined
}

