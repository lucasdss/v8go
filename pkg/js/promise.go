// promise.go — V8-aligned Promise implementation.
//
// Per gojs.md built-ins requirement and V8's promise system.
// States: pending, fulfilled, rejected.
// Supports .then() chaining, .catch(), .finally(), static methods.
package js

import (
	"runtime"
)

// PromiseState tracks the internal state of a Promise.
type PromiseState int

// PromiseState values represent the three internal states of a Promise.
const (
	PromisePending PromiseState = iota
	PromiseFulfilled
	PromiseRejected
)

// promiseReaction stores a pair of callbacks for a pending promise.
// When the promise settles, onFulfilled or onRejected is called with the result,
// and the resulting value is fed to resolve/reject of the .then()-returned promise.
type promiseReaction struct {
	onFulfilled func(JSValue) JSValue
	onRejected  func(JSValue) JSValue
	resolve     func(JSValue)
}

// PromisePrototype holds the Promise.prototype object (populated by registerPromise).
var PromisePrototype *JSObject

// NewPromise creates a new Promise object with the given executor.
func (vm *VM) NewPromise(executor func(resolve, reject func(JSValue))) JSValue {
	promise := NewJSObject()
	promise.ConstructorName = "Promise"
	if PromisePrototype != nil {
		promise.Prototype = PromisePrototype
	}

	state := PromisePending
	var result JSValue

	promise.Set("__promise_state__", NewNumber(float64(state)))
	promise.Set("__promise_result__", Undefined)

	var reject func(JSValue)
	var resolve func(JSValue)

	resolve = func(value JSValue) {
		if state != PromisePending {
			return
		}
		// If value is a thenable (has .then() method), chain resolution.
		if value.IsObject() && value.ObjVal != nil {
			thenVal := value.ObjVal.Get("then")
			if thenVal.IsObject() && thenVal.ObjVal != nil && thenVal.ObjVal.isCallable() {
				// Call then(resolve, reject) on the thenable.
				thenFn := thenVal.ObjVal
				wrappedResolve := vm.createBuiltinFunction("_resolve", func(this *JSObject, args []JSValue) JSValue {
					if len(args) > 0 {
						resolve(args[0])
					} else {
						resolve(Undefined)
					}
					return Undefined
				})
				wrappedReject := vm.createBuiltinFunction("_reject", func(this *JSObject, args []JSValue) JSValue {
					if len(args) > 0 {
						reject(args[0])
					} else {
						reject(Undefined)
					}
					return Undefined
				})
				thenFn.Call(value.ObjVal, []JSValue{NewObject(wrappedResolve.ObjVal), NewObject(wrappedReject.ObjVal)})
				return
			}
		}
		// Non-thenable value: settle normally.
		state = PromiseFulfilled
		result = value
		promise.Set("__promise_state__", NewNumber(float64(state)))
		promise.Set("__promise_result__", result)
		// Drain fulfill reactions.
		reactions := vm.promiseReactions[promise]
		delete(vm.promiseReactions, promise)
		for _, r := range reactions {
			val := r.onFulfilled(result)
			r.resolve(val)
		}
	}

	reject = func(reason JSValue) {
		if state != PromisePending {
			return
		}
		state = PromiseRejected
		result = reason
		promise.Set("__promise_state__", NewNumber(float64(state)))
		promise.Set("__promise_result__", result)
		// Drain reject reactions (calls resolve because rejection handlers can recover per ECMAScript).
		reactions := vm.promiseReactions[promise]
		delete(vm.promiseReactions, promise)
		for _, r := range reactions {
			val := r.onRejected(result)
			r.resolve(val)
		}
	}

	// Execute the executor synchronously.
	executor(resolve, reject)

	// Set a finalizer to clean up the reaction map when the promise is GC'd.
	// This prevents unbounded map growth from unsettled, unreferenced promises.
	// Must use a pointer indirection to avoid resurrecting the promise.
	promisePtr := &promise
	runtime.SetFinalizer(promise, func(obj *JSObject) {
		vm.mu.Lock()
		delete(vm.promiseReactions, obj)
		vm.mu.Unlock()
	})
	_ = promisePtr

	return NewObject(promise)
}

// registerPromise wires Promise into the VM globals.
func (vm *VM) registerPromise() {
	promiseCtor := NewJSObject()
	promiseCtor.ConstructorName = "Function"
	promiseCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		executor := args[0]
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			vm.callMethod(executor, nil, []JSValue{
				NewObject(makePromiseCallback(resolve)),
				NewObject(makePromiseCallback(reject)),
			})
		})
	}

	// Promise.resolve
	promiseCtor.Set("resolve", vm.createBuiltinFunction("Promise.resolve", func(this *JSObject, args []JSValue) JSValue {
		val := Undefined
		if len(args) > 0 {
			val = args[0]
		}
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			resolve(val)
		})
	}))

	// Promise.reject
	promiseCtor.Set("reject", vm.createBuiltinFunction("Promise.reject", func(this *JSObject, args []JSValue) JSValue {
		reason := Undefined
		if len(args) > 0 {
			reason = args[0]
		}
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			reject(reason)
		})
	}))

	// Promise.prototype.then
	promiseProto := NewJSObject()
	promiseProto.ConstructorName = "Promise"

	promiseProto.Set("then", vm.createBuiltinFunction("Promise.then", func(this *JSObject, args []JSValue) JSValue {
		var onFulfilled, onRejected func(JSValue) JSValue

		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil && args[0].ObjVal.isCallable() {
			cb := args[0]
			onFulfilled = func(v JSValue) JSValue { return vm.callMethod(cb, vm.globals.GlobalThis().ObjVal, []JSValue{v}) }
		} else {
			onFulfilled = func(v JSValue) JSValue { return v }
		}

		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil && args[1].ObjVal.isCallable() {
			cb := args[1]
			onRejected = func(v JSValue) JSValue { return vm.callMethod(cb, vm.globals.GlobalThis().ObjVal, []JSValue{v}) }
		} else {
			onRejected = func(v JSValue) JSValue { return v }
		}

		// Read current state.
		stateVal := this.Get("__promise_state__")
		state := PromiseState(int(stateVal.ToNumber()))
		resultVal := this.Get("__promise_result__")

		if state == PromiseFulfilled {
			return vm.NewPromise(func(resolve, reject func(JSValue)) {
				resolve(onFulfilled(resultVal))
			})
		}
		if state == PromiseRejected {
			return vm.NewPromise(func(resolve, reject func(JSValue)) {
				resolve(onRejected(resultVal))
			})
		}
		// Pending: enqueue the reaction pair.
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			reaction := promiseReaction{
				onFulfilled: onFulfilled,
				onRejected:  onRejected,
				resolve:     resolve,
			}
			vm.promiseReactions[this] = append(vm.promiseReactions[this], reaction)
		})
	}))

	promiseProto.Set("catch", vm.createBuiltinFunction("Promise.catch", func(this *JSObject, args []JSValue) JSValue {
		// .catch(onRejected) is equivalent to .then(undefined, onRejected)
		var thenArgs []JSValue
		thenArgs = append(thenArgs, Undefined)
		if len(args) > 0 {
			thenArgs = append(thenArgs, args[0])
		} else {
			thenArgs = append(thenArgs, Undefined)
		}
		thenFn := this.Get("then")
		if thenFn.IsObject() && thenFn.ObjVal != nil && thenFn.ObjVal.isCallable() {
			return thenFn.ObjVal.Call(this, thenArgs)
		}
		return Undefined
	}))

	promiseProto.Set("finally", vm.createBuiltinFunction("Promise.finally", func(this *JSObject, args []JSValue) JSValue {
		// .finally(onFinally) — execute onFinally regardless of outcome.
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil && args[0].ObjVal.isCallable() {
			args[0].ObjVal.Call(nil, nil)
		}
		return NewObject(this)
	}))

	// Promise.all(iterable) — resolves when all promises resolve, rejects on first rejection.
	promiseCtor.Set("all", vm.createBuiltinFunction("Promise.all", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return vm.NewPromise(func(resolve, reject func(JSValue)) { resolve(NewObject(NewJSObject())) })
		}
		arr := args[0].ObjVal
		lengthVal := arr.Get("length")
		count := int(lengthVal.ToNumber())
		if count == 0 {
			return vm.NewPromise(func(resolve, reject func(JSValue)) {
				resolve(NewObject(newArray(0)))
			})
		}
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			results := make([]JSValue, count)
			remaining := count
			rejected := false
			for i := 0; i < count; i++ {
				idx := i
				promiseVal := arr.Get(intKey(i))
				if promiseVal.IsObject() && promiseVal.ObjVal != nil {
					thenFn := promiseVal.ObjVal.Get("then")
					if thenFn.IsObject() && thenFn.ObjVal != nil && thenFn.ObjVal.isCallable() {
						onFulfilled := func(v JSValue) JSValue {
							if rejected {
								return Undefined
							}
							results[idx] = v
							remaining--
							if remaining == 0 {
								resolve(NewObject(newArrayFromValues(results)))
							}
							return Undefined
						}
						onRejected := func(v JSValue) JSValue {
							if !rejected {
								rejected = true
								reject(v)
							}
							return Undefined
						}
						thenFn.ObjVal.Call(promiseVal.ObjVal, []JSValue{
							NewObject(makePromiseCallbackWithResult(onFulfilled)),
							NewObject(makePromiseCallbackWithResult(onRejected)),
						})
						continue
					}
				}
				// Non-promise value: treat as resolved.
				results[idx] = promiseVal
				remaining--
				if remaining == 0 {
					resolve(NewObject(newArrayFromValues(results)))
				}
			}
		})
	}))

	// Promise.race(iterable) — resolves/rejects with the first settled promise.
	promiseCtor.Set("race", vm.createBuiltinFunction("Promise.race", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return vm.NewPromise(func(resolve, reject func(JSValue)) {})
		}
		arr := args[0].ObjVal
		lengthVal := arr.Get("length")
		count := int(lengthVal.ToNumber())
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			settled := false
			for i := 0; i < count; i++ {
				promiseVal := arr.Get(intKey(i))
				if promiseVal.IsObject() && promiseVal.ObjVal != nil {
					thenFn := promiseVal.ObjVal.Get("then")
					if thenFn.IsObject() && thenFn.ObjVal != nil && thenFn.ObjVal.isCallable() {
						onFulfilled := func(v JSValue) JSValue {
							if !settled {
								settled = true
								resolve(v)
							}
							return Undefined
						}
						onRejected := func(v JSValue) JSValue {
							if !settled {
								settled = true
								reject(v)
							}
							return Undefined
						}
						thenFn.ObjVal.Call(promiseVal.ObjVal, []JSValue{
							NewObject(makePromiseCallbackWithResult(onFulfilled)),
							NewObject(makePromiseCallbackWithResult(onRejected)),
						})
						continue
					}
				}
				// Non-promise value: resolve immediately.
				if !settled {
					settled = true
					resolve(promiseVal)
				}
			}
		})
	}))

	// Promise.any(iterable) — resolves with first fulfilled; rejects with AggregateError if all reject.
	promiseCtor.Set("any", vm.createBuiltinFunction("Promise.any", func(_ *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return vm.NewPromise(func(_, reject func(JSValue)) {
				reject(vm.newAggregateError("All promises were rejected"))
			})
		}
		arr := args[0].ObjVal
		count := int(arr.Get("length").ToNumber())
		if count == 0 {
			return vm.NewPromise(func(_, reject func(JSValue)) {
				reject(vm.newAggregateError("All promises were rejected"))
			})
		}
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			errors := make([]JSValue, count)
			remaining := count
			settled := false
			for i := 0; i < count; i++ {
				idx := i
				promiseVal := arr.Get(intKey(i))
				if promiseVal.IsObject() && promiseVal.ObjVal != nil {
					thenFn := promiseVal.ObjVal.Get("then")
					if thenFn.IsObject() && thenFn.ObjVal != nil && thenFn.ObjVal.isCallable() {
						onFulfilled := func(v JSValue) JSValue {
							if !settled {
								settled = true
								resolve(v)
							}
							return Undefined
						}
						onRejected := func(v JSValue) JSValue {
							errors[idx] = v
							remaining--
							if remaining == 0 && !settled {
								reject(vm.newAggregateErrorFromErrors(errors, "All promises were rejected"))
							}
							return Undefined
						}
						thenFn.ObjVal.Call(promiseVal.ObjVal, []JSValue{
							NewObject(makePromiseCallbackWithResult(onFulfilled)),
							NewObject(makePromiseCallbackWithResult(onRejected)),
						})
						continue
					}
				}
				// Non-promise value: resolve immediately.
				if !settled {
					settled = true
					resolve(promiseVal)
				}
			}
		})
	}))

	// Promise.allSettled(iterable) — resolves with array of {status, value/reason} when all settle.
	promiseCtor.Set("allSettled", vm.createBuiltinFunction("Promise.allSettled", func(_ *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return vm.NewPromise(func(resolve, _ func(JSValue)) {
				resolve(NewObject(newArray(0)))
			})
		}
		arr := args[0].ObjVal
		count := int(arr.Get("length").ToNumber())
		if count == 0 {
			return vm.NewPromise(func(resolve, _ func(JSValue)) {
				resolve(NewObject(newArray(0)))
			})
		}
		return vm.NewPromise(func(resolve, _ func(JSValue)) {
			results := make([]JSValue, count)
			remaining := count
			for i := 0; i < count; i++ {
				idx := i
				promiseVal := arr.Get(intKey(i))
				makeResult := func(status string, value JSValue) JSValue {
					entry := NewJSObject()
					entry.ConstructorName = "Object"
					entry.Set("status", NewString(status))
					if status == "fulfilled" {
						entry.Set("value", value)
					} else {
						entry.Set("reason", value)
					}
					return NewObject(entry)
				}
				if promiseVal.IsObject() && promiseVal.ObjVal != nil {
					thenFn := promiseVal.ObjVal.Get("then")
					if thenFn.IsObject() && thenFn.ObjVal != nil && thenFn.ObjVal.isCallable() {
						onFulfilled := func(v JSValue) JSValue {
							results[idx] = makeResult("fulfilled", v)
							remaining--
							if remaining == 0 {
								resolve(NewObject(newArrayFromValues(results)))
							}
							return Undefined
						}
						onRejected := func(v JSValue) JSValue {
							results[idx] = makeResult("rejected", v)
							remaining--
							if remaining == 0 {
								resolve(NewObject(newArrayFromValues(results)))
							}
							return Undefined
						}
						thenFn.ObjVal.Call(promiseVal.ObjVal, []JSValue{
							NewObject(makePromiseCallbackWithResult(onFulfilled)),
							NewObject(makePromiseCallbackWithResult(onRejected)),
						})
						continue
					}
				}
				// Non-promise value: treat as fulfilled.
				results[idx] = makeResult("fulfilled", promiseVal)
				remaining--
				if remaining == 0 {
					resolve(NewObject(newArrayFromValues(results)))
				}
			}
		})
	}))

	promiseCtor.Prototype = promiseProto
	PromisePrototype = promiseProto
	vm.globals.M["Promise"] = NewObject(promiseCtor)
}

func makePromiseCallback(fn func(JSValue)) *JSObject {
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		val := Undefined
		if len(args) > 0 {
			val = args[0]
		}
		fn(val)
		return Undefined
	}
	return obj
}

// makePromiseCallbackWithResult is like makePromiseCallback but preserves the return value.
func makePromiseCallbackWithResult(fn func(JSValue) JSValue) *JSObject {
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		val := Undefined
		if len(args) > 0 {
			val = args[0]
		}
		return fn(val)
	}
	return obj
}

// newArray creates an empty array with given length.
func newArray(length int) *JSObject {
	arr := NewJSObject()
	arr.ConstructorName = "Array"
	if ArrayPrototype != nil {
		arr.Prototype = ArrayPrototype
	}
	arr.Set("length", NewNumber(float64(length)))
	return arr
}

// newArrayFromValues creates an array from a slice of JSValue.
func newArrayFromValues(values []JSValue) *JSObject {
	arr := newArray(len(values))
	for i, v := range values {
		arr.Set(intKey(i), v)
	}
	return arr
}

// newAggregateError creates a simple AggregateError-like object with the given message.
func (vm *VM) newAggregateError(message string) JSValue {
	err := NewJSObject()
	err.ConstructorName = "AggregateError"
	err.Set("name", NewString("AggregateError"))
	err.Set("message", NewString(message))
	err.Set("errors", NewObject(newArray(0)))
	return NewObject(err)
}

// newAggregateErrorFromErrors creates an AggregateError with an errors array.
func (vm *VM) newAggregateErrorFromErrors(errors []JSValue, message string) JSValue {
	err := NewJSObject()
	err.ConstructorName = "AggregateError"
	err.Set("name", NewString("AggregateError"))
	err.Set("message", NewString(message))
	err.Set("errors", NewObject(newArrayFromValues(errors)))
	return NewObject(err)
}
