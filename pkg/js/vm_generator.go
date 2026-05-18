// vm_generator.go — Generator and async function support.
package js

// GeneratorState holds the saved execution state of a paused generator.
type GeneratorState struct {
	SavedPC      int        // PC to resume from (after the yield)
	SavedRegs    []JSValue  // copy of register file at yield point
	SavedAcc     JSValue    // saved accumulator
	SavedThis    JSValue    // saved this binding
	Done         bool       // true when generator has completed
	GenObj       *JSObject  // the generator object (to return {value, done})
}


// --- Generator handlers ---

func opYield(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.GenState == nil || frame.GenState.GenObj == nil {
		// Not in a generator context — treat as return.
		frame.ShouldReturn = true
		return
	}
	// Save frame state into generator state.
	gs := frame.GenState
	gs.SavedPC = frame.PC // PC already advanced past OpYield; next resume starts here
	gs.SavedAcc = frame.Acc
	// Deep-copy registers.
	gs.SavedRegs = make([]JSValue, len(frame.Regs))
	copy(gs.SavedRegs, frame.Regs)
	gs.SavedThis = frame.This
	gs.Done = false

	// Build result object {value: acc, done: false}.
	resultObj := NewJSObject()
	resultObj.Set("value", frame.Acc)
	resultObj.Set("done", False)
	frame.Acc = NewObject(resultObj)
	frame.ShouldReturn = true
}

func opYieldDelegate(vm *VM, frame *VMFrame, instr Instruction) {
	// yield* expr: delegate to another iterable.
	// For now, simplified: extract the value and yield it directly.
	// Full implementation would iterate the delegated object.
	if frame.GenState == nil || frame.GenState.GenObj == nil {
		frame.ShouldReturn = true
		return
	}
	gs := frame.GenState
	gs.SavedPC = frame.PC
	gs.SavedAcc = frame.Acc
	gs.SavedRegs = make([]JSValue, len(frame.Regs))
	copy(gs.SavedRegs, frame.Regs)
	gs.SavedThis = frame.This
	gs.Done = false

	resultObj := NewJSObject()
	resultObj.Set("value", frame.Acc)
	resultObj.Set("done", False)
	frame.Acc = NewObject(resultObj)
	frame.ShouldReturn = true
}

// opCreateGenerator creates a generator object from a function template in acc.
// Used internally; the generator function body emits this as the first instruction.
func opCreateGenerator(vm *VM, frame *VMFrame, instr Instruction) {
	// The acc holds the function template. Create a new generator object.
	bf := frame.Func
	if bf == nil {
		frame.Acc = Undefined
		return
	}
	genObj := NewJSObject()
	genObj.ConstructorName = "Generator"

	// Store the bytecode for resumption.
	genObj.Set("__bytecode__", NewObject(frame.Func.BytecodeFuncToObj(bf)))

	// Generator state: initially no saved frame.
	gs := &GeneratorState{
		GenObj: genObj,
	}
	genObj.Set("__genstate__", NewObject(genStateToObj(gs)))

	// Attach .next(), .return(), .throw() methods.
	genObj.Set("next", vm.makeGeneratorNext(genObj, gs))
	genObj.Set("return", vm.makeGeneratorReturn(genObj, gs))
	genObj.Set("throw", vm.makeGeneratorThrow(genObj, gs))

	frame.Acc = NewObject(genObj)
	frame.ShouldReturn = true
}

// BytecodeFuncToObj wraps a BytecodeFunction in a JSObject for storage.
func (bf *BytecodeFunction) BytecodeFuncToObj(original *BytecodeFunction) *JSObject {
	// Store the bytecode reference directly.
	obj := NewJSObject()
	obj.Bytecode = original
	return obj
}

// genStateToObj wraps GeneratorState in a JSObject for storage.
func genStateToObj(gs *GeneratorState) *JSObject {
	obj := NewJSObject()
	obj.ensureGenerator().State = gs
	return obj
}

// makeGeneratorNext creates the .next(value) method for a generator object.
// When called, resumes execution from the saved state or starts the generator.
func (vm *VM) makeGeneratorNext(genObj *JSObject, gs *GeneratorState) JSValue {
	fn := func(this *JSObject, args []JSValue) JSValue {
		if gs.Done {
			resultObj := NewJSObject()
			resultObj.Set("value", Undefined)
			resultObj.Set("done", True)
			return NewObject(resultObj)
		}

		// If we have a saved frame state, resume from it.
		if gs.SavedRegs != nil && gs.GenObj != nil {
			// Resume: restore state and continue execution.
			bfVal := genObj.Get("__bytecode__")
			if !bfVal.IsObject() || bfVal.ObjVal == nil || bfVal.ObjVal.Bytecode == nil {
				gs.Done = true
				resultObj := NewJSObject()
				resultObj.Set("value", Undefined)
				resultObj.Set("done", True)
				return NewObject(resultObj)
			}
			bytecodeFn := bfVal.ObjVal.Bytecode
			regs := make([]JSValue, len(gs.SavedRegs))
			copy(regs, gs.SavedRegs)
			// If .next() was called with an argument, pass it as the yield result.
			resumedAcc := gs.SavedAcc
			if len(args) > 0 {
				resumedAcc = args[0]
			}
			frame := &VMFrame{
				Func:      bytecodeFn,
				Regs:      regs,
				PC:        gs.SavedPC,
				Acc:       resumedAcc,
				HandlerPC: -1,
				FinallyPC: -1,
				This:      gs.SavedThis,
				GenState:  gs,
			}
			result := vm.executeFrame(frame)
			// After execution, check generator state.
			if gs.Done {
				// Generator completed (OpReturn was hit).
				if result.IsObject() && result.ObjVal != nil {
					if _, ok := result.ObjVal.getOwn("done"); ok {
						return result
					}
				}
				resultObj := NewJSObject()
				resultObj.Set("value", result)
				resultObj.Set("done", True)
				return NewObject(resultObj)
			}
			// Generator yielded: result should be the {value, done:false} object from OpYield.
			return result
		}

		// First call: start the generator.
		bfVal := genObj.Get("__bytecode__")
		if !bfVal.IsObject() || bfVal.ObjVal == nil || bfVal.ObjVal.Bytecode == nil {
			gs.Done = true
			resultObj := NewJSObject()
			resultObj.Set("value", Undefined)
			resultObj.Set("done", True)
			return NewObject(resultObj)
		}
		bytecodeFn := bfVal.ObjVal.Bytecode
		regs := make([]JSValue, bytecodeFn.NumRegisters)
		// Load initial arguments into registers.
		argsVal := genObj.Get("__args__")
		if argsVal.IsObject() && argsVal.ObjVal != nil {
			for i := 0; i < bytecodeFn.NumParams && i < len(regs); i++ {
				argKey := intKey(i)
				regs[i] = argsVal.ObjVal.Get(argKey)
			}
		}
		frame := &VMFrame{
			Func:      bytecodeFn,
			Regs:      regs,
			HandlerPC: -1,
			FinallyPC: -1,
			This:      genObj.Get("__this__"),
			GenState:  gs,
		}
		// Set this binding for the generator.
		if frame.This.IsUndefined() || frame.This.IsNull() {
			frame.This = vm.globalObject()
		}
		result := vm.executeFrame(frame)
		if gs.Done {
			if result.IsObject() && result.ObjVal != nil {
				if _, ok := result.ObjVal.getOwn("done"); ok {
					return result
				}
			}
			resultObj := NewJSObject()
			resultObj.Set("value", result)
			resultObj.Set("done", True)
			return NewObject(resultObj)
		}
		return result
	}
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return NewObject(obj)
}

// makeGeneratorReturn creates the .return(value) method for a generator object.
func (vm *VM) makeGeneratorReturn(genObj *JSObject, gs *GeneratorState) JSValue {
	fn := func(this *JSObject, args []JSValue) JSValue {
		gs.Done = true
		var value JSValue = Undefined
		if len(args) > 0 {
			value = args[0]
		}
		resultObj := NewJSObject()
		resultObj.Set("value", value)
		resultObj.Set("done", True)
		return NewObject(resultObj)
	}
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return NewObject(obj)
}

// makeGeneratorThrow creates the .throw(error) method for a generator object.
func (vm *VM) makeGeneratorThrow(genObj *JSObject, gs *GeneratorState) JSValue {
	fn := func(this *JSObject, args []JSValue) JSValue {
		gs.Done = true
		var err JSValue = NewString("Generator.throw() called")
		if len(args) > 0 {
			err = args[0]
		}
		// In a full implementation, this would resume the generator and throw.
		// For now, mark done and return a rejected-like result.
		resultObj := NewJSObject()
		resultObj.Set("value", Undefined)
		resultObj.Set("done", True)
		resultObj.Set("error", err)
		return NewObject(resultObj)
	}
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return NewObject(obj)
}

// createGeneratorObject creates a generator object for a generator function.
// This is called when a generator function is invoked — the body does NOT execute yet.
func (vm *VM) createGeneratorObject(bf *BytecodeFunction, thisObj *JSObject, args []JSValue) JSValue {
	genObj := NewJSObject()
	genObj.ConstructorName = "Generator"

	// Store the bytecode for later resumption.
	bfObj := NewJSObject()
	bfObj.Bytecode = bf
	genObj.Set("__bytecode__", NewObject(bfObj))

	// Store the initial `this` and arguments.
	thisVal := vm.globalObject()
	if thisObj != nil {
		thisVal = NewObject(thisObj)
	}
	genObj.Set("__this__", thisVal)

	// Store initial args for first .next() call.
	argsArr := NewJSObject()
	argsArr.ConstructorName = "Array"
	for i, arg := range args {
		argsArr.Set(intKey(i), arg)
	}
	genObj.Set("__args__", NewObject(argsArr))

	// Create generator state.
	gs := &GeneratorState{
		GenObj: genObj,
	}
	// Store gs in genObj for internal access.
	gsObj := NewJSObject()
	gsObj.ensureGenerator().State = gs
	genObj.Set("__genstate__", NewObject(gsObj))

	// Attach .next(), .return(), .throw() methods.
	genObj.Set("next", vm.makeGeneratorNext(genObj, gs))
	genObj.Set("return", vm.makeGeneratorReturn(genObj, gs))
	genObj.Set("throw", vm.makeGeneratorThrow(genObj, gs))

	return NewObject(genObj)
}

// createAsyncFunction wraps an async bytecode function to return a Promise.
// Async functions are compiled as generators (function*). This function creates
// the generator, then drives it via a recursive Promise chain (the "spawn" pattern).
// Each yield in the async function corresponds to an await — the spawner
// calls Promise.resolve(yieldedValue).then(resume) to chain continuations.
func (vm *VM) createAsyncFunction(bf *BytecodeFunction, thisObj *JSObject, args []JSValue) JSValue {
	// Create the underlying generator.
	genVal := vm.createGeneratorObject(bf, thisObj, args)
	if !genVal.IsObject() || genVal.ObjVal == nil {
		return Undefined
	}
	genObj := genVal.ObjVal

	// Extract generator methods.
	genNext := genObj.Get("next")
	genThrow := genObj.Get("throw")

	// Create the promise that the async function returns.
	return vm.NewPromise(func(resolve func(JSValue), reject func(JSValue)) {
		var step func(nextFn JSValue, prevValue JSValue)

		step = func(nextFn JSValue, prevValue JSValue) {
			var result JSValue
			if nextFn.IsObject() && nextFn.ObjVal != nil && nextFn.ObjVal.isCallable() {
				callArgs := []JSValue{}
				if prevValue.Tag != TagUndefined {
					callArgs = append(callArgs, prevValue)
				}
				result = nextFn.ObjVal.Call(genObj, callArgs)
			} else {
				reject(NewString("Async function: generator.next is not callable"))
				return
			}

			if !result.IsObject() || result.ObjVal == nil {
				reject(NewString("Async function: generator.next returned non-object"))
				return
			}

			doneVal := result.ObjVal.Get("done")
			valueVal := result.ObjVal.Get("value")

			if doneVal.IsTruthy() {
				resolve(valueVal)
				return
			}

			// Chain: Promise.resolve(value).then(
			//   resolved => step(genNext, resolved),
			//   rejected  => step(genThrow, rejected))
			promiseResolve := vm.GetGlobal("Promise")
			if !promiseResolve.IsObject() || promiseResolve.ObjVal == nil {
				reject(NewString("Async function: Promise is not available"))
				return
			}
			resolveFn := promiseResolve.ObjVal.Get("resolve")
			if !resolveFn.IsObject() || resolveFn.ObjVal == nil || !resolveFn.ObjVal.isCallable() {
				reject(NewString("Async function: Promise.resolve is not callable"))
				return
			}
			resolvedPromiseVal := resolveFn.ObjVal.Call(nil, []JSValue{valueVal})
			if !resolvedPromiseVal.IsObject() || resolvedPromiseVal.ObjVal == nil {
				reject(NewString("Async function: Promise.resolve returned non-object"))
				return
			}

			thenFn := resolvedPromiseVal.ObjVal.Get("then")
			if !thenFn.IsObject() || thenFn.ObjVal == nil || !thenFn.ObjVal.isCallable() {
				reject(NewString("Async function: .then is not callable"))
				return
			}

			onFulfilled := vm.createBuiltinFunction("", func(this *JSObject, a []JSValue) JSValue {
				nextArg := Undefined
				if len(a) > 0 {
					nextArg = a[0]
				}
				step(genNext, nextArg)
				return Undefined
			})
			onRejected := vm.createBuiltinFunction("", func(this *JSObject, a []JSValue) JSValue {
				errArg := Undefined
				if len(a) > 0 {
					errArg = a[0]
				}
				step(genThrow, errArg)
				return Undefined
			})

			thenFn.ObjVal.Call(resolvedPromiseVal.ObjVal, []JSValue{onFulfilled, onRejected})
		}

		step(genNext, Undefined)
	})
}
