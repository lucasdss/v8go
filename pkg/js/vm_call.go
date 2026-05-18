// vm_call.go — Function call handlers and helpers.
package js

// CallStackFrame represents a single frame in the JavaScript call stack.
type CallStackFrame struct {
	Name string
	File string
	Line int
	Col  int
}


// calleeGoesToBytecode returns true if calling this callee will enter executeFrame
// (as opposed to a built-in CallFunc which returns synchronously).
// Async and generator functions go through a different path (createAsyncFunction /
// createGeneratorObject) — they also need the pop in the op handler, not in opReturn.
func calleeGoesToBytecode(callee JSValue) bool {
	if !callee.IsObject() || callee.ObjVal == nil {
		return false
	}
	obj := callee.ObjVal
	// CallFunc takes precedence — if set, it's a built-in, not bytecode.
	if obj.CallFunc != nil {
		return false
	}
	// Bytecode with no CallFunc means it goes through executeFrame→opReturn.
	if obj.Bytecode != nil {
		// Async and generator functions don't go through executeFrame directly.
		if obj.Bytecode.Async || obj.Bytecode.Generator {
			return false
		}
		return true
	}
	return false
}


// --- Function call handlers ---

// boxString wraps a string primitive in a boxed String object so that
// String.prototype methods can be called with the correct `this` binding.
func boxString(s JSValue) *JSObject {
	boxed := NewJSObject()
	boxed.ConstructorName = "String"
	boxed.Prototype = StringPrototype
	boxed.Set("__value__", s)
	return boxed
}

// opCallFast is the shared fast-path for OpCall0/OpCall1/OpCall2.
func (vm *VM) opCallFast(frame *VMFrame, calleeReg, thisReg, argCount int) {
	var args []JSValue
	if argCount > 0 {
		if argCount <= 8 {
			// Stack-allocated array for common arg counts (0-8).
			var arr [8]JSValue
			args = arr[:argCount]
		} else {
			args = make([]JSValue, argCount)
		}
		for i := 0; i < argCount; i++ {
			argReg := calleeReg + 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	var thisObj *JSObject
	if thisReg < 255 && thisReg < len(frame.Regs) {
		if frame.Regs[thisReg].IsObject() {
			thisObj = frame.Regs[thisReg].ObjVal
		} else if frame.Regs[thisReg].IsString() {
			thisObj = boxString(frame.Regs[thisReg])
		}
	}
	if calleeReg < len(frame.Regs) {
		callee := frame.Regs[calleeReg]
		vm.pushCallName(callee, frame)
		frame.Acc = vm.callMethod(callee, thisObj, args)
		// Built-in functions (CallFunc) return directly; bytecode functions
		// go through executeFrame→opReturn which handles the pop.
		if !calleeGoesToBytecode(callee) {
			vm.popCallName()
		}
	} else {
		frame.Acc = Undefined
	}
}

func opCall0(vm *VM, frame *VMFrame, instr Instruction) {
	vm.opCallFast(frame, int(instr.OperandA), int(instr.OperandB), 0)
}

func opCall1(vm *VM, frame *VMFrame, instr Instruction) {
	vm.opCallFast(frame, int(instr.OperandA), int(instr.OperandB), 1)
}

func opCall2(vm *VM, frame *VMFrame, instr Instruction) {
	vm.opCallFast(frame, int(instr.OperandA), int(instr.OperandB), 2)
}

func opCall(vm *VM, frame *VMFrame, instr Instruction) {
	calleeReg, argCount := int(instr.OperandA), int(instr.OperandB)
	thisReg := int(instr.OperandC)
	var args []JSValue
	if argCount > 0 {
		if argCount <= 8 {
			// Stack-allocated array for common arg counts (0-8).
			var arr [8]JSValue
			args = arr[:argCount]
		} else {
			args = make([]JSValue, argCount)
		}
		for i := 0; i < argCount; i++ {
			argReg := calleeReg + 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	var thisObj *JSObject
	if thisReg < 255 && thisReg < len(frame.Regs) {
		if frame.Regs[thisReg].IsObject() {
			thisObj = frame.Regs[thisReg].ObjVal
		} else if frame.Regs[thisReg].IsString() {
			thisObj = boxString(frame.Regs[thisReg])
		}
	}
	if calleeReg < len(frame.Regs) {
		callee := frame.Regs[calleeReg]
		vm.pushCallName(callee, frame)
		frame.Acc = vm.callMethod(callee, thisObj, args)
		// Built-in functions (CallFunc) return directly; bytecode functions
		// go through executeFrame→opReturn which handles the pop.
		if !calleeGoesToBytecode(callee) {
			vm.popCallName()
		}
	} else {
		frame.Acc = Undefined
	}
}

func opCallSpread(vm *VM, frame *VMFrame, instr Instruction) {
	calleeReg, fixedCount := int(instr.OperandA), int(instr.OperandB)
	thisReg := int(instr.OperandC)
	var args []JSValue
	// Read fixed (non-spread) arguments from consecutive registers.
	if fixedCount > 0 {
		if fixedCount <= 8 {
			var arr [8]JSValue
			args = arr[:fixedCount]
		} else {
			args = make([]JSValue, fixedCount)
		}
		for i := 0; i < fixedCount; i++ {
			argReg := calleeReg + 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	// Read spread array from calleeReg + 1 + fixedCount.
	spreadReg := calleeReg + 1 + fixedCount
	if spreadReg < len(frame.Regs) {
		spreadVal := frame.Regs[spreadReg]
		if spreadVal.IsObject() && spreadVal.ObjVal != nil {
			lengthVal := spreadVal.ObjVal.Get("length")
			arrLen := int(lengthVal.ToNumber())
			for i := 0; i < arrLen; i++ {
				elem := spreadVal.ObjVal.Get(intKey(i))
				args = append(args, elem)
			}
		}
	}
	var thisObj *JSObject
	if thisReg < 255 && thisReg < len(frame.Regs) {
		if frame.Regs[thisReg].IsObject() {
			thisObj = frame.Regs[thisReg].ObjVal
		} else if frame.Regs[thisReg].IsString() {
			thisObj = boxString(frame.Regs[thisReg])
		}
	}
	if calleeReg < len(frame.Regs) {
		callee := frame.Regs[calleeReg]
		vm.pushCallName(callee, frame)
		frame.Acc = vm.callMethod(callee, thisObj, args)
		// Built-in functions (CallFunc) return directly; bytecode functions
		// go through executeFrame→opReturn which handles the pop.
		if !calleeGoesToBytecode(callee) {
			vm.popCallName()
		}
	} else {
		frame.Acc = Undefined
	}
}

func opSuperCall(vm *VM, frame *VMFrame, instr Instruction) {
	// super() call: load parent constructor from __proto__ on this.
	// The parent constructor is at this.__proto__.constructor.
	argCount := int(instr.OperandB)
	var args []JSValue
	if argCount > 0 {
		if argCount <= 8 {
			var arr [8]JSValue
			args = arr[:argCount]
		} else {
			args = make([]JSValue, argCount)
		}
		for i := 0; i < argCount; i++ {
			// Args start at register 1 (after the callee register which is 0).
			argReg := 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	// Load parent constructor: this.__proto__.constructor
	thisObj := frame.This.ObjVal
	if thisObj != nil {
		protoVal := thisObj.Get("__proto__")
		if protoVal.IsObject() && protoVal.ObjVal != nil {
			parentCtor := protoVal.ObjVal.Get("constructor")
			frame.Acc = vm.callMethod(parentCtor, thisObj, args)
		} else {
			frame.Acc = Undefined
		}
	} else {
		frame.Acc = Undefined
	}
	// Mark that super() has been called so this can be accessed.
	frame.SuperCalled = true
}

func opCheckConstructor(vm *VM, frame *VMFrame, instr Instruction) {
	// Check that acc is null or a constructor (object with CallFunc or Bytecode).
	if frame.Acc.IsNull() || frame.Acc.IsUndefined() {
		return // null/undefined are allowed as extends value
	}
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		obj := frame.Acc.ObjVal
		if obj.CallFunc != nil || obj.Bytecode != nil || obj.ConstructorName == "Function" {
			return // is a constructor
		}
	}
	throwTypeErrorInFrame(frame, "Class extends value is not a constructor or null")
}

func opCreateClosure(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Acc.ObjVal.Bytecode != nil {
		closure := NewJSObject()
		closure.ConstructorName = frame.Acc.ObjVal.ConstructorName
		closure.Bytecode = frame.Acc.ObjVal.Bytecode
		closure.Set("length", NewNumber(float64(frame.Acc.ObjVal.Bytecode.NumParams)))
		env := NewJSObject()
		for i, val := range frame.Regs {
			env.Set(intKey(i), val)
		}
		closure.Set("__env__", NewObject(env))
		frame.Acc = NewObject(closure)
	} else {
		frame.Acc = NewObject(NewJSObject())
	}
}

func opLdaCaptured(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.ClosureEnv != nil {
		regIdx := int(instr.OperandA)
		frame.Acc = frame.ClosureEnv.Get(intKey(regIdx))
	} else {
		frame.Acc = Undefined
	}
}

// Lock / Unlock expose vm.mu so external callers (e.g. event-loop timer
// callbacks) can serialise with vm.Run / vm.Execute.
func (vm *VM) Lock()    { vm.mu.Lock() }
func (vm *VM) Unlock()  { vm.mu.Unlock() }

// RLock / RUnlock expose vm.mu read lock so external callers can safely read
// globals, console logs, and other read-only state without blocking other readers.
func (vm *VM) RLock()   { vm.mu.RLock() }
func (vm *VM) RUnlock() { vm.mu.RUnlock() }

// CallMethodLocked invokes a callable JSValue with an explicit receiver.
// The caller MUST hold vm.mu (via Lock()) to serialise with vm.Run.
func (vm *VM) CallMethodLocked(callee JSValue, thisObj *JSObject, args []JSValue) JSValue {
	return vm.callMethod(callee, thisObj, args)
}

// callMethod handles method invocation with explicit receiver (this).
func (vm *VM) callMethod(callee JSValue, thisObj *JSObject, args []JSValue) JSValue {
	// Resolve string callee names to built-in functions.
	if callee.IsString() {
		name := callee.ToString()
		if fn, ok := vm.registry.Builtins[name]; ok {
			return fn(args)
		}
		return Undefined
	}
	if !callee.IsObject() || callee.ObjVal == nil {
		return Undefined
	}
	obj := callee.ObjVal

	// Built-in Go function: pass the receiver as `this`.
	if obj.CallFunc != nil {
		if thisObj == nil {
			thisObj = obj
		}
		return obj.CallFunc(thisObj, args)
	}

	// User-defined bytecode function.
	if obj.Bytecode != nil {
		if obj.Bytecode.Async {
			return vm.createAsyncFunction(obj.Bytecode, thisObj, args)
		}
		if obj.Bytecode.Generator {
			return vm.createGeneratorObject(obj.Bytecode, thisObj, args)
		}
		if vm.calltrack.Depth() > maxCallDepth {
			return Undefined
		}
		vm.calltrack.IncDepth()
		regs := vm.allocRegs(obj.Bytecode.NumRegisters)
		newFrame := vm.allocFrame()
		newFrame.Func = obj.Bytecode
		newFrame.Regs = regs
		newFrame.HandlerPC = -1
		newFrame.FinallyPC = -1
		if thisObj != nil {
			newFrame.This = NewObject(thisObj)
		} else {
			newFrame.This = vm.globalObject()
		}
		if obj.Bytecode.IsDerivedConstructor {
			newFrame.SuperCalled = false
		}
		if envVal := obj.Get("__env__"); envVal.IsObject() && envVal.ObjVal != nil {
			newFrame.ClosureEnv = envVal.ObjVal
		}
		if obj.Bytecode.ICVector != nil {
			newFrame.ICVector = obj.Bytecode.ICVector
		}
		for i, arg := range args {
			if i < len(newFrame.Regs) {
				newFrame.Regs[i] = arg
			}
		}
		if obj.Bytecode.HasRestParam {
			restReg := obj.Bytecode.RestParamReg
			numNonRest := obj.Bytecode.NumParams
			if int(numNonRest) < len(args) {
				restArgs := args[numNonRest:]
				restArray := NewJSObject()
				restArray.ConstructorName = "Array"
				restArray.Prototype = ArrayPrototype
				restArray.growProperties(len(restArgs))
				restArray.Set("length", NewNumber(float64(len(restArgs))))
				for j, a := range restArgs {
					restArray.Set(intKey(j), a)
				}
				if restReg < len(newFrame.Regs) {
					newFrame.Regs[restReg] = NewObject(restArray)
				}
			}
		}
		result := vm.executeFrame(newFrame)
		vm.freeFrame(newFrame)
		vm.freeRegs(regs)
		vm.calltrack.DecDepth()
		return result
	}

	return Undefined
}

// callFunction handles function invocation (backward compat).
func (vm *VM) callFunction(frame *VMFrame, callee JSValue, args []JSValue) JSValue {
	return vm.callMethod(callee, nil, args)
}

// makeFunctionObject creates a callable JSObject from bytecode.
func (vm *VM) makeFunctionObject(name string, bf *BytecodeFunction) JSValue {
	obj := NewJSObject()
	obj.ConstructorName = "Function"

	// Async functions must return a Promise; generator functions must return
	// a generator object.  Delegate through callMethod so the Async/Generator
	// flags on the BytecodeFunction are respected (callMethod checks
	// obj.Bytecode before obj.CallFunc, so we set Bytecode here and use a
	// shim CallFunc that enters callMethod).
	if bf.Async || bf.Generator {
		obj.Bytecode = bf
		obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
			return vm.callMethod(NewObject(obj), this, args)
		}
	} else {
		obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
			regs := vm.allocRegs(bf.NumRegisters)
			defer vm.freeRegs(regs)
			newFrame := vm.allocFrame()
			newFrame.Func = bf
			newFrame.Regs = regs
			newFrame.HandlerPC = -1
			newFrame.FinallyPC = -1
			if this != nil {
				newFrame.This = NewObject(this)
			} else {
				newFrame.This = vm.globalObject()
			}
			for i, arg := range args {
				if i < len(newFrame.Regs) {
					newFrame.Regs[i] = arg
				}
			}
			result := vm.executeFrame(newFrame)
			vm.freeFrame(newFrame)
			return result
		}
	}
	vm.registry.Funcs[name] = bf
	return NewObject(obj)
}

func (vm *VM) makeBuiltinFunction(name string, fn func(args []JSValue) JSValue) JSValue {
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		return fn(args)
	}
	vm.registry.Builtins[name] = fn
	return NewObject(obj)
}
