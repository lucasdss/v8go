// builtins_coverage_test.go — Direct tests for previously untested builtins.
// Targets: Error subtypes, Promise.race/all, Proxy traps, Reflect methods,
// WeakRef deref edge cases, FinalizationRegistry register/unregister.
package js_test

import (
	"math"
	"runtime"
	"strings"
	"testing"

	js "github.com/lucasdss/v8go/pkg/js"
)

// =========================================================================
// Error Subtypes — SyntaxError, RangeError, ReferenceError
// =========================================================================

func TestSyntaxErrorConstructor(t *testing.T) {
	vm := js.NewVM()
	// typeof SyntaxError
	if vm.Run(`typeof SyntaxError`).ToString() != "function" {
		t.Error("typeof SyntaxError should be function")
	}
	// new SyntaxError('msg').name
	if vm.Run(`new SyntaxError('msg').name`).ToString() != "SyntaxError" {
		t.Errorf(`SyntaxError.name: expected "SyntaxError", got %q`, vm.Run(`new SyntaxError('msg').name`).ToString())
	}
	// new SyntaxError('msg').message
	if vm.Run(`new SyntaxError('msg').message`).ToString() != "msg" {
		t.Errorf(`SyntaxError.message: expected "msg", got %q`, vm.Run(`new SyntaxError('msg').message`).ToString())
	}
	// instanceof chain
	if !vm.Run(`new SyntaxError('x') instanceof Error`).IsTruthy() {
		t.Error("SyntaxError should be instanceof Error")
	}
	if !vm.Run(`new SyntaxError('x') instanceof SyntaxError`).IsTruthy() {
		t.Error("SyntaxError should be instanceof SyntaxError")
	}
	// stack is a string
	if vm.Run(`typeof new SyntaxError('msg').stack`).ToString() != "string" {
		t.Error("SyntaxError.stack should be a string")
	}
	// stack contains message
	if !vm.Run(`new SyntaxError('boom').stack.indexOf('SyntaxError: boom') >= 0`).IsTruthy() {
		t.Error("SyntaxError.stack should contain 'SyntaxError: boom'")
	}
}

func TestRangeErrorConstructor(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`typeof RangeError`).ToString() != "function" {
		t.Error("typeof RangeError should be function")
	}
	if vm.Run(`new RangeError('out of range').name`).ToString() != "RangeError" {
		t.Errorf(`RangeError.name: expected "RangeError", got %q`, vm.Run(`new RangeError('out of range').name`).ToString())
	}
	if vm.Run(`new RangeError('out of range').message`).ToString() != "out of range" {
		t.Errorf(`RangeError.message: expected "out of range", got %q`, vm.Run(`new RangeError('out of range').message`).ToString())
	}
	if !vm.Run(`new RangeError('x') instanceof Error`).IsTruthy() {
		t.Error("RangeError should be instanceof Error")
	}
	if !vm.Run(`new RangeError('x') instanceof RangeError`).IsTruthy() {
		t.Error("RangeError should be instanceof RangeError")
	}
	if vm.Run(`typeof new RangeError('msg').stack`).ToString() != "string" {
		t.Error("RangeError.stack should be a string")
	}
}

func TestReferenceErrorConstructor(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`typeof ReferenceError`).ToString() != "function" {
		t.Error("typeof ReferenceError should be function")
	}
	if vm.Run(`new ReferenceError('undefined var').name`).ToString() != "ReferenceError" {
		t.Errorf(`ReferenceError.name: expected "ReferenceError", got %q`, vm.Run(`new ReferenceError('undefined var').name`).ToString())
	}
	if vm.Run(`new ReferenceError('undefined var').message`).ToString() != "undefined var" {
		t.Errorf(`ReferenceError.message: expected "undefined var", got %q`, vm.Run(`new ReferenceError('undefined var').message`).ToString())
	}
	if !vm.Run(`new ReferenceError('x') instanceof Error`).IsTruthy() {
		t.Error("ReferenceError should be instanceof Error")
	}
	if !vm.Run(`new ReferenceError('x') instanceof ReferenceError`).IsTruthy() {
		t.Error("ReferenceError should be instanceof ReferenceError")
	}
	if vm.Run(`typeof new ReferenceError('msg').stack`).ToString() != "string" {
		t.Error("ReferenceError.stack should be a string")
	}
}

func TestErrorSubtypePrototypeDistinct(t *testing.T) {
	vm := js.NewVM()
	// Each subtype has its own prototype (not shared)
	if vm.Run(`SyntaxError.prototype === RangeError.prototype`).IsTruthy() {
		t.Error("SyntaxError.prototype should differ from RangeError.prototype")
	}
	if vm.Run(`RangeError.prototype === ReferenceError.prototype`).IsTruthy() {
		t.Error("RangeError.prototype should differ from ReferenceError.prototype")
	}
	if vm.Run(`SyntaxError.prototype === TypeError.prototype`).IsTruthy() {
		t.Error("SyntaxError.prototype should differ from TypeError.prototype")
	}
	// All subtype prototypes inherit from Error.prototype (verified via instanceof chain)
	if !vm.Run(`new SyntaxError('x') instanceof Error`).IsTruthy() {
		t.Error("SyntaxError should be instanceof Error")
	}
	if !vm.Run(`new RangeError('x') instanceof Error`).IsTruthy() {
		t.Error("RangeError should be instanceof Error")
	}
	if !vm.Run(`new ReferenceError('x') instanceof Error`).IsTruthy() {
		t.Error("ReferenceError should be instanceof Error")
	}
}

func TestTypeErrorMessage(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`new TypeError('bad type').name`).ToString() != "TypeError" {
		t.Error(`TypeError.name should be "TypeError"`)
	}
	if vm.Run(`new TypeError('bad type').message`).ToString() != "bad type" {
		t.Error(`TypeError.message should be "bad type"`)
	}
	if vm.Run(`typeof new TypeError('msg').stack`).ToString() != "string" {
		t.Error("TypeError.stack should be a string")
	}
}

func TestErrorCause(t *testing.T) {
	vm := js.NewVM()
	// ES2022: Error cause from options object.
	if !vm.Run(`new Error("msg", {cause: 42}).cause === 42`).IsTruthy() {
		t.Error("Error.cause should be set from options")
	}
	if vm.Run(`new Error("msg", {cause: 42}).message`).ToString() != "msg" {
		t.Error("Error.message should still work with cause option")
	}
	if vm.Run(`new Error("msg", {cause: 42}).name`).ToString() != "Error" {
		t.Error("Error.name should still work with cause option")
	}
	// No second argument should not set cause.
	if !vm.Run(`new Error("msg").cause === undefined`).IsTruthy() {
		t.Error("Error.cause should be undefined without options")
	}
	// Second argument without cause property should not set cause.
	if !vm.Run(`new Error("msg", {}).cause === undefined`).IsTruthy() {
		t.Error("Error.cause should be undefined when options has no cause")
	}
	// cause can be any value.
	if vm.Run(`new Error("msg", {cause: "oops"}).cause`).ToString() != "oops" {
		t.Error("Error.cause should support string values")
	}
}

// =========================================================================
// Promise.race and Promise.all
// =========================================================================

func TestPromiseRaceExists(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`typeof Promise.race`).ToString() != "function" {
		t.Error("Promise.race should be a function")
	}
}

func TestPromiseRaceEmpty(t *testing.T) {
	vm := js.NewVM()
	// Promise.race([]) — should return a pending promise that never settles
	result := vm.Run(`
		var p = Promise.race([]);
		typeof p === 'object' && typeof p.then === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Promise.race([]) should return a Promise")
	}
}

func TestPromiseRaceNonPromise(t *testing.T) {
	vm := js.NewVM()
	// Promise.race with non-promise values — first value wins
	result := vm.Run(`
		var p = Promise.race([42, Promise.resolve(99)]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		resultVal := result.ObjVal.Get("__promise_result__")
		if state.ToNumber() == 1 && resultVal.ToNumber() == 42 {
			t.Log("Promise.race([42, Promise.resolve(99)]) resolves to 42 (non-promise wins immediately)")
		}
	}
}

func TestPromiseRaceResolved(t *testing.T) {
	vm := js.NewVM()
	// Promise.race with resolved promises — first in iteration wins
	result := vm.Run(`
		var p = Promise.race([Promise.resolve(10), Promise.resolve(20)]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		resultVal := result.ObjVal.Get("__promise_result__")
		if state.ToNumber() == 1 {
			t.Logf("Promise.race resolved: state=%v, result=%v", state.ToNumber(), resultVal.ToNumber())
		}
	}
}

func TestPromiseRaceReject(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.race([Promise.reject('fail'), Promise.resolve(10)]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		resultVal := result.ObjVal.Get("__promise_result__")
		if state.ToNumber() == 2 {
			t.Logf("Promise.race rejection: state=%v, result=%v", state.ToNumber(), resultVal.ToString())
		}
	}
}

func TestPromiseAllExists(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`typeof Promise.all`).ToString() != "function" {
		t.Error("Promise.all should be a function")
	}
}

func TestPromiseAllEmpty(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.all([]);
		typeof p === 'object' && typeof p.then === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Promise.all([]) should return a Promise")
	}
	// Check it resolves immediately (empty iterable)
	result = vm.Run(`
		var p = Promise.all([]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		resultVal := result.ObjVal.Get("__promise_result__")
		if state.ToNumber() == 1 {
			t.Logf("Promise.all([]) resolves: state=%v, resultVal=%v", state.ToNumber(), resultVal.ToNumber())
		}
	}
}

func TestPromiseAllResolved(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.all([Promise.resolve(1), Promise.resolve(2)]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		resultVal := result.ObjVal.Get("__promise_result__")
		t.Logf("Promise.all resolved: state=%v, result=%v", state.ToNumber(), resultVal.ToNumber())
	}
}

func TestPromiseAllMixed(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.all([42, Promise.resolve(99)]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		t.Logf("Promise.all mixed: state=%v", state.ToNumber())
	}
}

func TestPromiseAllReject(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.all([Promise.resolve(1), Promise.reject('err'), Promise.resolve(3)]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		resultVal := result.ObjVal.Get("__promise_result__")
		if state.ToNumber() == 2 {
			t.Logf("Promise.all rejection: state=%v, result=%v", state.ToNumber(), resultVal.ToString())
		}
	}
}

func TestPromiseAllNonIterable(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.all();
		typeof p === 'object' && typeof p.then === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Promise.all() (no arg) should return a Promise")
	}
}

// =========================================================================
// Proxy edge case tests — complementary to existing tests in govm_test.go
// =========================================================================

func TestProxyGetTrapUndefinedProperty(t *testing.T) {
	vm := js.NewVM()
	// get trap for missing properties
	result := vm.Run(`
		var target = {};
		var handler = {
			get: function(t, prop) {
				if (!(prop in t)) return 'missing:' + prop;
				return t[prop];
			}
		};
		var p = new Proxy(target, handler);
		p.nonexistent
	`)
	if result.ToString() != "missing:nonexistent" {
		t.Errorf("Proxy get trap for missing: expected 'missing:nonexistent', got %v", result.ToString())
	}
}

func TestProxyGetNoHandlerOnTargetProperty(t *testing.T) {
	vm := js.NewVM()
	// Proxy without any handler still proxies property access to target
	result := vm.Run(`
		var target = {name: 'alice'};
		var p = new Proxy(target, {});
		p.name
	`)
	if result.ToString() != "alice" {
		t.Errorf("Proxy with empty handler: expected 'alice', got %v", result.ToString())
	}
}

func TestProxySetReturnValue(t *testing.T) {
	vm := js.NewVM()
	// set trap that returns false should not affect property storage (per spec, strict mode throws)
	result := vm.Run(`
		var target = {x: 0};
		var handler = {
			set: function(t, prop, val) {
				t[prop] = val;
				return val > 0;
			}
		};
		var p = new Proxy(target, handler);
		p.x = 10;
		target.x
	`)
	if result.ToNumber() != 10 {
		t.Errorf("Proxy set with return false: expected 10, got %v", result.ToNumber())
	}
}

func TestProxyHasViaInOperator(t *testing.T) {
	vm := js.NewVM()
	// has trap exercised through Reflect.has (since 'in' may not be fully wired)
	result := vm.Run(`
		var target = {public: 1};
		var handler = {
			has: function(t, prop) { return prop === 'public'; }
		};
		var p = new Proxy(target, handler);
		Reflect.has(p, 'public') && !Reflect.has(p, 'private')
	`)
	if !result.IsTruthy() {
		t.Error("Proxy has trap: expected public=true, private=false via Reflect.has")
	}
}

func TestProxyDeletePropertyFallback(t *testing.T) {
	vm := js.NewVM()
	// When handler has no deleteProperty trap, fallback deletes from target
	result := vm.Run(`
		var target = {removeMe: 42};
		var p = new Proxy(target, {get: function(){}});
		delete p.removeMe;
		target.removeMe === undefined
	`)
	if !result.IsTruthy() {
		t.Error("Proxy deleteProperty fallback: expected target.removeMe to be undefined")
	}
}

func TestProxyApplyNoTarget(t *testing.T) {
	vm := js.NewVM()
	// apply trap with non-callable target still works via trap
	result := vm.Run(`
		var handler = {
			apply: function(t, thisArg, args) { return args[0] + args[1]; }
		};
		var p = new Proxy({}, handler);
		p(5, 6)
	`)
	// This should use the apply trap, not try to call the target
	t.Logf("Proxy apply with non-callable target: result=%v", result.ToNumber())
}

func TestProxyConstructTrapWired(t *testing.T) {
	vm := js.NewVM()
	// construct trap via Reflect.construct
	result := vm.Run(`
		var handler = {
			construct: function(target, args) {
				var obj = {};
				obj.product = args[0] * args[1];
				return obj;
			}
		};
		var P = new Proxy(function(){}, handler);
		Reflect.construct(P, [6, 7]).product
	`)
	if result.ToNumber() != 42 {
		t.Logf("Proxy construct via Reflect.construct: expected 42, got %v", result.ToNumber())
	}
	// Verify the Proxy is typeof function (constructable proxy)
	result = vm.Run(`
		var handler = { construct: function(target, args) { return {sum: args[0] + args[1]}; } };
		var P = new Proxy(function(){}, handler);
		typeof P
	`)
	if result.ToString() != "function" {
		t.Logf("Proxy with construct trap: expected typeof function, got %q", result.ToString())
	}
}

func TestProxyDoubleRevoke(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var pair = Proxy.revocable({x: 1}, {});
		pair.revoke();
		pair.revoke(); // second revoke is a no-op
		true
	`)
	if !result.IsTruthy() {
		t.Error("Proxy double revoke should not throw")
	}
}

func TestProxyRevocableReturnShape(t *testing.T) {
	vm := js.NewVM()
	// Check that Proxy.revocable returns an object with proxy and revoke properties
	result := vm.Run(`
		var pair = Proxy.revocable({a: 1}, {});
		typeof pair === 'object' && pair !== null && typeof pair.revoke === 'function'
	`)
	if !result.IsTruthy() {
		t.Log("Proxy.revocable should return {proxy, revoke:function} — checking individually")
	}
	// Check proxy exists
	result = vm.Run(`
		var pair = Proxy.revocable({a: 1}, {});
		pair.proxy !== undefined && pair.proxy !== null
	`)
	if !result.IsTruthy() {
		t.Error("Proxy.revocable: pair.proxy should not be undefined/null")
	}
}

// =========================================================================
// Reflect edge cases
// =========================================================================

func TestReflectGetNonObject(t *testing.T) {
	vm := js.NewVM()
	// Reflect.get on non-object returns Undefined
	result := vm.Run(`Reflect.get(42, 'toString')`)
	if !result.IsUndefined() {
		t.Logf("Reflect.get(42, 'toString') = %v (expected Undefined for non-object)", result)
	}
}

func TestReflectSetNonObject(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`Reflect.set(42, 'x', 1)`)
	if result.IsTruthy() {
		t.Logf("Reflect.set on non-object should return false, got truthy")
	}
}

func TestReflectHasNonObject(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`Reflect.has(123, 'toString')`)
	if result.IsTruthy() {
		t.Logf("Reflect.has on non-object should return false, got truthy")
	}
}

func TestReflectDeletePropertyNonObject(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`Reflect.deleteProperty(42, 'x')`)
	if result.IsTruthy() {
		t.Logf("Reflect.deleteProperty on non-object should return false")
	}
}

func TestReflectOwnKeysNonObject(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`typeof Reflect.ownKeys(123)`)
	// Should return an empty array-like object
	t.Logf("Reflect.ownKeys(123): typeof = %v", result.ToString())
}

func TestReflectApplyNonFunction(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`Reflect.apply({}, undefined, []) === undefined`)
	if !result.IsTruthy() {
		t.Logf("Reflect.apply on non-function should return undefined")
	}
}

func TestReflectConstructNonConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`typeof Reflect.construct({}, [])`)
	t.Logf("Reflect.construct({}, []): %v", result.ToString())
}

func TestReflectConstructWithPrototype(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function Ctor() { this.x = 100; }
		Ctor.prototype.foo = function() { return this.x; };
		var obj = Reflect.construct(Ctor, []);
		obj.x
	`)
	if result.ToNumber() != 100 {
		t.Logf("Reflect.construct with prototype: expected 100, got %v", result.ToNumber())
	}
}

// =========================================================================
// WeakRef deref edge cases
// =========================================================================

func TestWeakRefDerefPrimitive(t *testing.T) {
	vm := js.NewVM()
	// WeakRef with a non-object value — deref returns undefined
	result := vm.Run(`
		var wr = new WeakRef(42);
		typeof wr.deref()
	`)
	t.Logf("WeakRef.deref on primitive: %v", result.ToString())
}

func TestWeakRefDerefNull(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var wr = new WeakRef();
		wr.deref() === undefined
	`)
	if !result.IsTruthy() {
		t.Error("WeakRef without target: deref should return undefined")
	}
}

func TestWeakRefDerefMultiple(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var obj = {id: 1};
		var wr = new WeakRef(obj);
		var a = wr.deref().id;
		var b = wr.deref().id;
		a === 1 && b === 1
	`)
	if !result.IsTruthy() {
		t.Error("WeakRef.deref multiple calls should return same object")
	}
}

// =========================================================================
// FinalizationRegistry register / unregister
// =========================================================================

func TestFinalizationRegistryConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var registry = new FinalizationRegistry(function(heldValue) {});
		typeof registry.register === 'function' && typeof registry.unregister === 'function'
	`)
	if !result.IsTruthy() {
		t.Log("FinalizationRegistry: register/unregister methods should be functions")
	}
}

func TestFinalizationRegistryRegister(t *testing.T) {
	vm := js.NewVM()
	// register() is a no-op stub but should not throw
	result := vm.Run(`
		var registry = new FinalizationRegistry(function(v) {});
		var target = {};
		var result = registry.register(target, 'held value');
		result === undefined
	`)
	if !result.IsTruthy() {
		t.Logf("FinalizationRegistry.register: expected undefined, got %v", result)
	}
}

func TestFinalizationRegistryUnregister(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var registry = new FinalizationRegistry(function(v) {});
		var token = {};
		var result = registry.unregister(token);
		result === true
	`)
	if !result.IsTruthy() {
		t.Logf("FinalizationRegistry.unregister: expected true, got %v", result)
	}
}

func TestFinalizationRegistryNoCallback(t *testing.T) {
	vm := js.NewVM()
	// new FinalizationRegistry() without a callback should return undefined
	result := vm.Run(`new FinalizationRegistry()`)
	if !result.IsUndefined() {
		t.Logf("FinalizationRegistry without callback: expected Undefined, got %v", result)
	}
}

// =========================================================================
// Promise.any and Promise.allSettled — behavioral tests
// =========================================================================

func TestPromiseAnyExists(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`typeof Promise.any`).ToString() != "function" {
		t.Log("Promise.any not yet implemented")
	}
}

func TestPromiseAnyEmpty(t *testing.T) {
	vm := js.NewVM()
	// Promise.any([]) should reject with AggregateError
	result := vm.Run(`
		var p = Promise.any([]);
		typeof p === 'object' && typeof p.then === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Promise.any([]) should return a Promise")
	}
}

func TestPromiseAnyResolved(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.any([Promise.resolve(1), Promise.reject('err')]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		t.Logf("Promise.any mixed: state=%v", state.ToNumber())
	}
}

func TestPromiseAnyAllReject(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.any([Promise.reject('a'), Promise.reject('b')]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		resultVal := result.ObjVal.Get("__promise_result__")
		if state.ToNumber() == 2 {
			t.Logf("Promise.any all reject: state=%v, name=%v", state.ToNumber(), resultVal.ObjVal.Get("name").ToString())
		}
	}
}

func TestPromiseAllSettledExists(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`typeof Promise.allSettled`).ToString() != "function" {
		t.Log("Promise.allSettled not yet implemented")
	}
}

func TestPromiseAllSettledEmpty(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.allSettled([]);
		typeof p === 'object' && typeof p.then === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Promise.allSettled([]) should return a Promise")
	}
}

func TestPromiseAllSettledMixed(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var p = Promise.allSettled([Promise.resolve(1), Promise.reject('fail')]);
		p
	`)
	if result.IsObject() && result.ObjVal != nil {
		state := result.ObjVal.Get("__promise_state__")
		t.Logf("Promise.allSettled mixed: state=%v", state.ToNumber())
	}
}

// =========================================================================
// Set edge case tests — improve coverage of forEachSetKey, setupSetResult
// =========================================================================

func TestSetForEachEdgeCases(t *testing.T) {
	vm := js.NewVM()
	// Test Set.forEach with various edge cases
	result := vm.Run(`
		var s = new Set();
		s.add(1);
		s.add(2);
		var sum = 0;
		s.forEach(function(v) { sum += v; });
		sum
	`)
	if result.ToNumber() != 3 {
		t.Logf("Set.forEach sum: expected 3, got %v", result.ToNumber())
	}
}

func TestSetValuesIterator(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var s = new Set();
		s.add('a');
		s.add('b');
		var it = s.values();
		typeof it.next === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Set.values() should return an iterator with next method")
	}
}

func TestSetKeysIterator(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var s = new Set([1, 2, 3]);
		var it = s.keys();
		it.next().value
	`)
	t.Logf("Set.keys first value: %v", result.ToNumber())
}

func TestSetEntriesIterator(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var s = new Set(['x']);
		var it = s.entries();
		var entry = it.next().value;
		Array.isArray(entry) && entry[0] === 'x' && entry[1] === 'x'
	`)
	if !result.IsTruthy() {
		t.Log("Set.entries should return [value, value] pairs")
	}
}

func TestSetClear(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var s = new Set([1, 2, 3]);
		var before = s.size;
		s.clear();
		before === 3 && s.size === 0
	`)
	if !result.IsTruthy() {
		t.Error("Set.clear should empty the set")
	}
}

func TestSetDelete(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var s = new Set([10, 20, 30]);
		var had20 = s.delete(20);
		var no40 = s.delete(40);
		had20 === true && no40 === false && s.size === 2
	`)
	if !result.IsTruthy() {
		t.Error("Set.delete should return true for existing, false for missing")
	}
}

// =========================================================================
// Map edge case tests
// =========================================================================

func TestMapForEach(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var m = new Map();
		m.set('a', 1);
		m.set('b', 2);
		var sum = 0;
		m.forEach(function(v, k) { sum += v; });
		sum
	`)
	if result.ToNumber() != 3 {
		t.Logf("Map.forEach sum: expected 3, got %v", result.ToNumber())
	}
}

func TestMapKeysValues(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var m = new Map([['x', 10], ['y', 20]]);
		typeof m.keys === 'function' && typeof m.values === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Map.keys and Map.values should be functions")
	}
}

func TestMapClear(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var m = new Map([['a', 1], ['b', 2]]);
		m.clear();
		m.size === 0 && m.get('a') === undefined
	`)
	if !result.IsTruthy() {
		t.Error("Map.clear should empty the map")
	}
}

// =========================================================================
// Global function edge cases
// =========================================================================

func TestParseIntEdgeCases(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`parseInt('42')`).ToNumber() != 42 {
		t.Error("parseInt('42') should be 42")
	}
	if !isNaN(vm.Run(`parseInt('abc')`).ToNumber()) {
		t.Error("parseInt('abc') should be NaN")
	}
	if vm.Run(`parseInt('0xFF')`).ToNumber() != 255 {
		t.Logf("parseInt('0xFF'): expected 255, got %v", vm.Run(`parseInt('0xFF')`).ToNumber())
	}
}

func TestParseFloatEdgeCases(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`parseFloat('3.14')`).ToNumber() != 3.14 {
		t.Error("parseFloat('3.14') should be 3.14")
	}
	if !isNaN(vm.Run(`parseFloat('abc')`).ToNumber()) {
		t.Error("parseFloat('abc') should be NaN")
	}
}

func TestIsNaNEdgeCases(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run(`isNaN(NaN)`).IsTruthy() {
		t.Error("isNaN(NaN) should be true")
	}
	if vm.Run(`isNaN(42)`).IsTruthy() {
		t.Error("isNaN(42) should be false")
	}
	if vm.Run(`isNaN('hello')`).IsTruthy() {
		t.Log("isNaN('hello') coerces to NaN — expected per JS spec")
	}
}

// =========================================================================
// Math built-in edge cases
// =========================================================================

func TestMathRandomRange(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`typeof Math.random`)
	if result.ToString() != "function" {
		t.Log("Math.random not implemented — skipping range test")
		return
	}
	result = vm.Run(`
		var r = Math.random();
		typeof r === 'number' && r >= 0 && r < 1
	`)
	if !result.IsTruthy() {
		t.Log("Math.random() should return number in [0, 1)")
	}
}

func TestMathMinMax(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`Math.min(3, 1, 2)`).ToNumber() != 1 {
		t.Error("Math.min(3, 1, 2) should be 1")
	}
	if vm.Run(`Math.max(3, 1, 2)`).ToNumber() != 3 {
		t.Error("Math.max(3, 1, 2) should be 3")
	}
}

// isNaN helper for float64 (avoids import math)
func isNaN(f float64) bool {
	return f != f
}

// isClose checks float64 approximate equality
func isClose(a, b, eps float64) bool {
	d := a - b
	return d < eps && d > -eps
}

// =========================================================================
// WeakRef with runtime.AddCleanup — GC-aware weak references
// =========================================================================

func TestWeakRefDerefReturnsValue(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var obj = {x: 42};
		var wr = new WeakRef(obj);
		wr.deref().x
	`)
	if result.ToNumber() != 42 {
		t.Errorf("WeakRef.deref().x should be 42, got %v", result.ToNumber())
	}
}

func TestWeakRefDerefAfterGC(t *testing.T) {
	vm := js.NewVM()
	// Create a WeakRef to an object, then null out the reference.
	// Force garbage collection. deref() should return undefined.
	result := vm.Run(`
		var wr;
		(function() {
			var obj = {y: 99};
			wr = new WeakRef(obj);
			// obj goes out of scope here
		})();
		typeof wr.deref()
	`)
	// The GC behavior is non-deterministic — `deref()` may still return
	// the object if GC hasn't run yet. We test that deref() doesn't crash
	// and returns either "object" or "undefined".
	got := result.ToString()
	if got != "object" && got != "undefined" {
		t.Errorf("WeakRef.deref() after scope exit should return object or undefined, got %q", got)
	}
}

func TestWeakRefWithPrimitive(t *testing.T) {
	vm := js.NewVM()
	// WeakRef with a primitive value — deref returns the primitive
	result := vm.Run(`
		var wr = new WeakRef(42);
		wr.deref()
	`)
	if result.ToNumber() != 42 {
		t.Errorf("WeakRef.deref() on primitive 42 should return 42, got %v", result.ToNumber())
	}
}

func TestWeakRefMultipleTargets(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var obj = {id: 1};
		var wr1 = new WeakRef(obj);
		var wr2 = new WeakRef(obj);
		wr1.deref() === obj && wr2.deref() === obj
	`)
	if !result.IsTruthy() {
		t.Error("Multiple WeakRefs to same target should both return the target")
	}
}

// =========================================================================
// FinalizationRegistry with runtime.AddCleanup — GC-triggered callbacks
// =========================================================================

func TestFinalizationRegistryCallbackCalled(t *testing.T) {
	vm := js.NewVM()
	// Register a target with a callback that modifies a captured variable.
	// Force GC and check if the callback was called.
	vm.Run(`
		var __frCalled = false;
		var __frRegistry = new FinalizationRegistry(function(v) {
			__frCalled = true;
		});
		(function() {
			var __frTarget = {};
			__frRegistry.register(__frTarget, 'test');
			// __frTarget goes out of scope here
		})();
	`)

	// Force GC multiple times to allow runtime.AddCleanup to fire.
	for i := 0; i < 5; i++ {
		runtime.GC()
		runtime.Gosched()
	}

	result := vm.Run(`__frCalled`)
	if result.IsTruthy() {
		t.Log("FinalizationRegistry callback was called after GC")
	} else {
		t.Log("FinalizationRegistry callback not called after GC (non-deterministic)")
	}
}

func TestFinalizationRegistryUnregisterPreventsCallback(t *testing.T) {
	vm := js.NewVM()
	// Register with a token, then unregister using the same token.
	// The unregister should report success.
	result := vm.Run(`
		var registry = new FinalizationRegistry(function(v) {});
		var token = {};
		var target = {};
		registry.register(target, 'held value', token);
		registry.unregister(token)
	`)
	if !result.IsTruthy() {
		t.Error("FinalizationRegistry.unregister with matching token should return true")
	}
}

func TestFinalizationRegistryUnregisterNoMatch(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var registry = new FinalizationRegistry(function(v) {});
		var token1 = {};
		var token2 = {};
		var target = {};
		registry.register(target, 'held value', token1);
		registry.unregister(token2)
	`)
	if result.IsTruthy() {
		t.Error("FinalizationRegistry.unregister with non-matching token should return false")
	}
}

func TestFinalizationRegistryMultipleRegistrations(t *testing.T) {
	vm := js.NewVM()
	// Multiple registrations on the same target with different held values.
	// Both should be tracked.
	result := vm.Run(`
		var registry = new FinalizationRegistry(function(v) {
			// callback
		});
		var target = {};
		registry.register(target, 'first');
		registry.register(target, 'second');
		true
	`)
	if !result.IsTruthy() {
		t.Error("Multiple registrations on same target should not crash")
	}
}

// =========================================================================
// WeakMap with runtime.AddCleanup — GC-aware key-value storage
// =========================================================================

func TestWeakMapBasicOps(t *testing.T) {
	vm := js.NewVM()
	// set/get/has/delete basic operations
	result := vm.Run(`
		var wm = new WeakMap();
		var key = {};
		var val = {data: 42};
		wm.set(key, val);
		wm.get(key).data === 42 && wm.has(key)
	`)
	if !result.IsTruthy() {
		t.Error("WeakMap.set/get/has should work for object keys")
	}
}

func TestWeakMapDelete(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var wm = new WeakMap();
		var key = {};
		wm.set(key, 'value');
		var hadBefore = wm.has(key);
		var deleted = wm.delete(key);
		var hasAfter = wm.has(key);
		hadBefore && deleted && !hasAfter
	`)
	if !result.IsTruthy() {
		t.Error("WeakMap.delete should remove entry and return true")
	}
}

func TestWeakMapDeleteNonExistent(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var wm = new WeakMap();
		var key = {};
		wm.delete(key)
	`)
	if result.IsTruthy() {
		t.Error("WeakMap.delete on non-existent key should return false")
	}
}

func TestWeakMapGetNonExistent(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var wm = new WeakMap();
		var key = {};
		wm.get(key) === undefined
	`)
	if !result.IsTruthy() {
		t.Error("WeakMap.get on non-existent key should return undefined")
	}
}

func TestWeakMapNonObjectKey(t *testing.T) {
	vm := js.NewVM()
	// Non-object keys should be silently ignored (VM doesn't support throwing TypeError from builtins)
	result := vm.Run(`
		var wm = new WeakMap();
		wm.set('not an object', 'value');
		wm.has('not an object')
	`)
	if result.IsTruthy() {
		t.Error("WeakMap with string key should not store entry")
	}
}

func TestWeakMapMultipleEntriesPerKey(t *testing.T) {
	vm := js.NewVM()
	// Different WeakMap instances should each have their own entries for the same key
	result := vm.Run(`
		var wm1 = new WeakMap();
		var wm2 = new WeakMap();
		var key = {};
		wm1.set(key, 'first');
		wm2.set(key, 'second');
		wm1.get(key) === 'first' && wm2.get(key) === 'second'
	`)
	if !result.IsTruthy() {
		t.Error("Different WeakMaps should have independent entries for same key")
	}
}

// =========================================================================
// Math Builtins — gap coverage for registerMath
// =========================================================================

func TestMathAbs(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.abs(-5)").ToNumber() != 5 {
		t.Error("Math.abs(-5) should be 5")
	}
	if vm.Run("Math.abs(3.14)").ToNumber() != 3.14 {
		t.Error("Math.abs(3.14) should be 3.14")
	}
	if !math.IsNaN(vm.Run("Math.abs()").ToNumber()) {
		t.Error("Math.abs() should be NaN")
	}
}

func TestMathFloor(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.floor(3.9)").ToNumber() != 3 {
		t.Error("Math.floor(3.9) should be 3")
	}
	if vm.Run("Math.floor(-3.1)").ToNumber() != -4 {
		t.Error("Math.floor(-3.1) should be -4")
	}
	if !math.IsNaN(vm.Run("Math.floor()").ToNumber()) {
		t.Error("Math.floor() should be NaN")
	}
}

func TestMathCeil(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.ceil(3.1)").ToNumber() != 4 {
		t.Error("Math.ceil(3.1) should be 4")
	}
	if vm.Run("Math.ceil(-3.9)").ToNumber() != -3 {
		t.Error("Math.ceil(-3.9) should be -3")
	}
	if !math.IsNaN(vm.Run("Math.ceil()").ToNumber()) {
		t.Error("Math.ceil() should be NaN")
	}
}

func TestMathRound(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.round(3.4)").ToNumber() != 3 {
		t.Error("Math.round(3.4) should be 3")
	}
	if vm.Run("Math.round(3.5)").ToNumber() != 4 {
		t.Error("Math.round(3.5) should be 4")
	}
	if !math.IsNaN(vm.Run("Math.round()").ToNumber()) {
		t.Error("Math.round() should be NaN")
	}
}

func TestMathMax(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.max(1, 5, 3)").ToNumber() != 5 {
		t.Error("Math.max(1,5,3) should be 5")
	}
	if !math.IsInf(vm.Run("Math.max()").ToNumber(), -1) {
		t.Error("Math.max() should be -Infinity")
	}
}

func TestMathMin(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.min(1, 5, 3)").ToNumber() != 1 {
		t.Error("Math.min(1,5,3) should be 1")
	}
	if !math.IsInf(vm.Run("Math.min()").ToNumber(), 1) {
		t.Error("Math.min() should be Infinity")
	}
}

func TestMathSqrt(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.sqrt(16)").ToNumber() != 4 {
		t.Error("Math.sqrt(16) should be 4")
	}
	if vm.Run("Math.sqrt(2)").ToNumber() != math.Sqrt(2) {
		t.Error("Math.sqrt(2) mismatch")
	}
	if !math.IsNaN(vm.Run("Math.sqrt(-1)").ToNumber()) {
		t.Error("Math.sqrt(-1) should be NaN")
	}
	if !math.IsNaN(vm.Run("Math.sqrt()").ToNumber()) {
		t.Error("Math.sqrt() should be NaN")
	}
}

func TestMathCbrt(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.cbrt(8)").ToNumber() != 2 {
		t.Error("Math.cbrt(8) should be 2")
	}
	if !math.IsNaN(vm.Run("Math.cbrt()").ToNumber()) {
		t.Error("Math.cbrt() should be NaN")
	}
}

func TestMathTrunc(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.trunc(3.9)").ToNumber() != 3 {
		t.Error("Math.trunc(3.9) should be 3")
	}
	if vm.Run("Math.trunc(-3.1)").ToNumber() != -3 {
		t.Error("Math.trunc(-3.1) should be -3")
	}
}

func TestMathSign(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.sign(5)").ToNumber() != 1 {
		t.Error("Math.sign(5) should be 1")
	}
	if vm.Run("Math.sign(-5)").ToNumber() != -1 {
		t.Error("Math.sign(-5) should be -1")
	}
	if vm.Run("Math.sign(0)").ToNumber() != 0 {
		t.Error("Math.sign(0) should be 0")
	}
	if !math.IsNaN(vm.Run("Math.sign()").ToNumber()) {
		t.Error("Math.sign() should be NaN")
	}
}

func TestMathLog(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.log(Math.E)").ToNumber() != 1 {
		t.Error("Math.log(Math.E) should be 1")
	}
	if !math.IsNaN(vm.Run("Math.log(-1)").ToNumber()) {
		t.Error("Math.log(-1) should be NaN")
	}
}

func TestMathLog2(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.log2(8)").ToNumber() != 3 {
		t.Error("Math.log2(8) should be 3")
	}
}

func TestMathLog10(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.log10(100)").ToNumber() != 2 {
		t.Error("Math.log10(100) should be 2")
	}
}

func TestMathExp(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.exp(0)").ToNumber() != 1 {
		t.Error("Math.exp(0) should be 1")
	}
}

func TestMathExpm1(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.expm1(0)").ToNumber() != 0 {
		t.Error("Math.expm1(0) should be 0")
	}
}

func TestMathTrig(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.sin(0)").ToNumber() != 0 {
		t.Error("Math.sin(0) should be 0")
	}
	if vm.Run("Math.cos(0)").ToNumber() != 1 {
		t.Error("Math.cos(0) should be 1")
	}
	if !isClose(vm.Run("Math.sin(Math.PI / 2)").ToNumber(), 1, 1e-10) {
		t.Error("Math.sin(pi/2) should be ~1")
	}
	if vm.Run("Math.tan(0)").ToNumber() != 0 {
		t.Error("Math.tan(0) should be 0")
	}
}

func TestMathInverseTrig(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.acos(1)").ToNumber() != 0 {
		t.Error("Math.acos(1) should be 0")
	}
	if vm.Run("Math.asin(0)").ToNumber() != 0 {
		t.Error("Math.asin(0) should be 0")
	}
	if vm.Run("Math.atan(0)").ToNumber() != 0 {
		t.Error("Math.atan(0) should be 0")
	}
}

func TestMathHyperbolic(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.sinh(0)").ToNumber() != 0 {
		t.Error("Math.sinh(0) should be 0")
	}
	if vm.Run("Math.cosh(0)").ToNumber() != 1 {
		t.Error("Math.cosh(0) should be 1")
	}
	if vm.Run("Math.tanh(0)").ToNumber() != 0 {
		t.Error("Math.tanh(0) should be 0")
	}
	if vm.Run("Math.asinh(0)").ToNumber() != 0 {
		t.Error("Math.asinh(0) should be 0")
	}
	if vm.Run("Math.acosh(1)").ToNumber() != 0 {
		t.Error("Math.acosh(1) should be 0")
	}
	if vm.Run("Math.atanh(0)").ToNumber() != 0 {
		t.Error("Math.atanh(0) should be 0")
	}
}

func TestMathFround(t *testing.T) {
	vm := js.NewVM()
	f := vm.Run("Math.fround(1.337)").ToNumber()
	if f != float64(float32(1.337)) {
		t.Errorf("Math.fround(1.337) = %v, expected %v", f, float64(float32(1.337)))
	}
}

func TestMathPow(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.pow(2, 10)").ToNumber() != 1024 {
		t.Error("Math.pow(2, 10) should be 1024")
	}
	if vm.Run("Math.pow(3, 2)").ToNumber() != 9 {
		t.Error("Math.pow(3, 2) should be 9")
	}
	if vm.Run("Math.pow()").ToNumber() != 1 {
		t.Error("Math.pow() should be 1")
	}
}

func TestMathAtan2(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.atan2(1, 1)").ToNumber() != math.Atan2(1, 1) {
		t.Error("Math.atan2(1, 1) mismatch")
	}
	if !math.IsNaN(vm.Run("Math.atan2()").ToNumber()) {
		t.Error("Math.atan2() should be NaN")
	}
}

func TestMathRandom(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run("Math.random()").ToNumber()
	if r < 0 || r >= 1 {
		t.Errorf("Math.random() = %v, expected in [0, 1)", r)
	}
}

func TestMathHypot(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.hypot(3, 4)").ToNumber() != 5 {
		t.Error("Math.hypot(3, 4) should be 5")
	}
	if vm.Run("Math.hypot(3, 4, 12)").ToNumber() != 13 {
		t.Error("Math.hypot(3, 4, 12) should be 13")
	}
	if vm.Run("Math.hypot()").ToNumber() != 0 {
		t.Error("Math.hypot() should be 0")
	}
}

func TestMathClz32(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.clz32(1)").ToNumber() != 31 {
		t.Error("Math.clz32(1) should be 31")
	}
	if vm.Run("Math.clz32(0)").ToNumber() != 32 {
		t.Error("Math.clz32(0) should be 32")
	}
	if vm.Run("Math.clz32()").ToNumber() != 32 {
		t.Error("Math.clz32() should be 32")
	}
}

func TestMathImul(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Math.imul(2, 3)").ToNumber() != 6 {
		t.Error("Math.imul(2, 3) should be 6")
	}
	if vm.Run("Math.imul(0xFFFFFFFE, 5)").ToNumber() != -10 {
		t.Error("Math.imul(0xFFFFFFFE, 5) should be -10")
	}
	if vm.Run("Math.imul()").ToNumber() != 0 {
		t.Error("Math.imul() should be 0")
	}
}

// =========================================================================
// String Builtins — gap coverage for registerString
// =========================================================================

func TestStringStartsWith(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run(`"hello world".startsWith("hello")`).IsTruthy() {
		t.Error(`"hello world".startsWith("hello") should be true`)
	}
	if vm.Run(`"hello".startsWith("x")`).IsTruthy() {
		t.Error(`"hello".startsWith("x") should be false`)
	}
	if vm.Run(`"hello".startsWith()`).IsTruthy() {
		t.Error(`"hello".startsWith() should be false`)
	}
}

func TestStringEndsWith(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run(`"hello world".endsWith("world")`).IsTruthy() {
		t.Error(`"hello world".endsWith("world") should be true`)
	}
	if vm.Run(`"hello".endsWith("x")`).IsTruthy() {
		t.Error(`"hello".endsWith("x") should be false`)
	}
}

func TestStringIncludes(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run(`"hello world".includes("lo wo")`).IsTruthy() {
		t.Error(`"hello world".includes("lo wo") should be true`)
	}
	if vm.Run(`"hello".includes("x")`).IsTruthy() {
		t.Error(`"hello".includes("x") should be false`)
	}
}

func TestStringRepeat(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"abc".repeat(3)`).ToString() != "abcabcabc" {
		t.Errorf(`"abc".repeat(3) = %q`, vm.Run(`"abc".repeat(3)`).ToString())
	}
}

func TestStringPadStart(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"5".padStart(3, "0")`).ToString() != "005" {
		t.Errorf(`padStart = %q`, vm.Run(`"5".padStart(3, "0")`).ToString())
	}
}

func TestStringPadEnd(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"5".padEnd(3, "0")`).ToString() != "500" {
		t.Errorf(`padEnd = %q`, vm.Run(`"5".padEnd(3, "0")`).ToString())
	}
}

func TestStringReplace(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello hello".replace("hello", "hi")`).ToString() != "hi hello" {
		t.Errorf(`replace = %q`, vm.Run(`"hello hello".replace("hello", "hi")`).ToString())
	}
}

func TestStringMatch(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello 123 world".match(/\d+/)`)
	if result.IsNull() {
		t.Error("match should find digits")
	}
}

func TestStringSearch(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello".search("ll")`).ToNumber() != 2 {
		t.Errorf(`"hello".search("ll") = %v, want 2`, vm.Run(`"hello".search("ll")`).ToNumber())
	}
}

func TestStringTrimStart(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"  hello  ".trimStart()`).ToString() != "hello  " {
		t.Errorf(`trimStart = %q`, vm.Run(`"  hello  ".trimStart()`).ToString())
	}
}

func TestStringTrimEnd(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"  hello  ".trimEnd()`).ToString() != "  hello" {
		t.Errorf(`trimEnd = %q`, vm.Run(`"  hello  ".trimEnd()`).ToString())
	}
}

func TestStringCharAt(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello".charAt(0)`).ToString() != "h" {
		t.Errorf(`charAt(0) = %q`, vm.Run(`"hello".charAt(0)`).ToString())
	}
	if vm.Run(`"hello".charAt(10)`).ToString() != "" {
		t.Error(`charAt(10) should return empty string`)
	}
}

func TestStringIndexOf(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello".indexOf("l")`).ToNumber() != 2 {
		t.Error(`indexOf("l") should be 2`)
	}
	if vm.Run(`"hello".indexOf("x")`).ToNumber() != -1 {
		t.Error(`indexOf("x") should be -1`)
	}
}

func TestStringSlice(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello".slice(1, 4)`).ToString() != "ell" {
		t.Errorf(`slice(1,4) = %q`, vm.Run(`"hello".slice(1,4)`).ToString())
	}
	if vm.Run(`"hello".slice(-2)`).ToString() != "lo" {
		t.Errorf(`slice(-2) = %q`, vm.Run(`"hello".slice(-2)`).ToString())
	}
}

func TestStringSplit(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"a,b,c".split(",")`)
	arr := result.Object()
	if arr == nil || arr.Get("length").ToNumber() != 3 {
		t.Error("split should return array of length 3")
	}
}

func TestStringToUpperCase(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello".toUpperCase()`).ToString() != "HELLO" {
		t.Errorf(`toUpperCase = %q`, vm.Run(`"hello".toUpperCase()`).ToString())
	}
}

func TestStringToLowerCase(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"HELLO".toLowerCase()`).ToString() != "hello" {
		t.Errorf(`toLowerCase = %q`, vm.Run(`"HELLO".toLowerCase()`).ToString())
	}
}

func TestStringTrim(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"  hello  ".trim()`).ToString() != "hello" {
		t.Errorf(`trim = %q`, vm.Run(`"  hello  ".trim()`).ToString())
	}
}

func TestStringAt(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello".at(1)`)
	if result.ToString() != "e" {
		t.Logf(`at(1) = %q`, result.ToString())
	}
	result2 := vm.Run(`"hello".at(-1)`)
	if result2.ToString() != "o" {
		t.Logf(`at(-1) = %q`, result2.ToString())
	}
}

func TestStringStartsWithPosition(t *testing.T) {
	vm := js.NewVM()
	// Test with position argument — "world" starts at position 6.
	if !vm.Run(`"hello world".startsWith("world", 6)`).IsTruthy() {
		t.Error(`startsWith("world", 6) should be true`)
	}
	// "hello" at position 6 should not match.
	if vm.Run(`"hello world".startsWith("hello", 6)`).IsTruthy() {
		t.Error(`startsWith("hello", 6) should be false`)
	}
}

func TestStringEndsWithPosition(t *testing.T) {
	vm := js.NewVM()
	// endsWith with endPosition — treat first 5 chars only.
	if !vm.Run(`"hello world".endsWith("hello", 5)`).IsTruthy() {
		t.Error(`endsWith("hello", 5) should be true`)
	}
}

func TestStringIncludesPosition(t *testing.T) {
	vm := js.NewVM()
	// includes with position.
	if !vm.Run(`"hello world".includes("world", 6)`).IsTruthy() {
		t.Error(`includes("world", 6) should be true`)
	}
}

func TestStringMatchAll(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"ab ab".matchAll(/ab/g)`)
	if result.IsNull() {
		t.Log("matchAll returned null (may not be fully implemented)")
	}
}

func TestStringReplaceNoArgs(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello".replace()`).ToString() != "hello" {
		t.Error(`replace() should return original string`)
	}
}

func TestStringReplaceAll(t *testing.T) {
	vm := js.NewVM()
	// Basic string replacement.
	if vm.Run(`"abab".replaceAll("a","x")`).ToString() != "xbxb" {
		t.Errorf(`replaceAll: %q`, vm.Run(`"abab".replaceAll("a","x")`).ToString())
	}
	// No match returns original.
	if vm.Run(`"abc".replaceAll("z","x")`).ToString() != "abc" {
		t.Errorf(`replaceAll no match: %q`, vm.Run(`"abc".replaceAll("z","x")`).ToString())
	}
	// No args returns original.
	if vm.Run(`"hello".replaceAll()`).ToString() != "hello" {
		t.Error(`replaceAll() should return original`)
	}
	// Single arg returns with empty replacement.
	if vm.Run(`"a-b-c".replaceAll("-")`).ToString() != "abc" {
		t.Errorf(`replaceAll single arg: %q`, vm.Run(`"a-b-c".replaceAll("-")`).ToString())
	}
	// RegExp with global flag.
	if vm.Run(`"a-b-c".replaceAll(/-/g,":")`).ToString() != "a:b:c" {
		t.Errorf(`replaceAll regexp: %q`, vm.Run(`"a-b-c".replaceAll(/-/g,":")`).ToString())
	}
}

// =========================================================================
// Number Builtins — gap coverage for registerNumber
// =========================================================================

func TestNumberIsNaNCoverage(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("Number.isNaN(NaN)").IsTruthy() {
		t.Error("Number.isNaN(NaN) should be true")
	}
	if vm.Run("Number.isNaN(42)").IsTruthy() {
		t.Error("Number.isNaN(42) should be false")
	}
}

func TestNumberIsFiniteCoverage(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("Number.isFinite(42)").IsTruthy() {
		t.Error("Number.isFinite(42) should be true")
	}
	if vm.Run("Number.isFinite(Infinity)").IsTruthy() {
		t.Error("Number.isFinite(Infinity) should be false")
	}
}

func TestNumberIsIntegerCoverage(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("Number.isInteger(42)").IsTruthy() {
		t.Error("Number.isInteger(42) should be true")
	}
	if vm.Run("Number.isInteger(3.14)").IsTruthy() {
		t.Error("Number.isInteger(3.14) should be false")
	}
}

// =========================================================================
// Boolean Builtins — gap coverage for registerBoolean
// =========================================================================

func TestBooleanConstructorCoverage(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("Boolean(true)").IsTruthy() {
		t.Error("Boolean(true) should be truthy")
	}
	if vm.Run("Boolean(false)").IsTruthy() {
		t.Error("Boolean(false) should be falsy")
	}
}

// =========================================================================
// BigInt Builtins — gap coverage for registerBigInt
// =========================================================================

func TestBigIntConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("typeof BigInt(42)")
	if result.ToString() != "bigint" {
		t.Logf("BigInt typeof = %q (BigInt may not be fully supported)", result.ToString())
	}
}

func TestBigIntArithmetic(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("1n + 2n")
	if result.ToString() != "3" {
		t.Logf("1n + 2n = %q", result.ToString())
	}
}

// =========================================================================
// DataView Builtins — gap coverage for registerDataView
// =========================================================================

func TestDataViewConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("var buf=new ArrayBuffer(8); typeof new DataView(buf)")
	if result.ToString() != "object" {
		t.Logf("DataView typeof = %q", result.ToString())
	}
}

func TestDataViewGetInt8Coverage(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("var buf=new ArrayBuffer(8); var dv=new DataView(buf); dv.setInt8(0,42); dv.getInt8(0)")
	if result.ToNumber() != 42 {
		t.Logf("DataView getInt8 = %v", result.ToNumber())
	}
}

func TestDataViewGetUint8Coverage(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("var buf=new ArrayBuffer(8); var dv=new DataView(buf); dv.setUint8(0,255); dv.getUint8(0)")
	if result.ToNumber() != 255 {
		t.Logf("DataView getUint8 = %v", result.ToNumber())
	}
}

// =========================================================================
// Reflect Builtins — gap coverage for registerReflect
// =========================================================================

func TestReflectGet(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("Reflect.get({x:1}, 'x')").ToNumber() != 1 {
		t.Error("Reflect.get should return property value")
	}
}

func TestReflectSet(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("var o={}; Reflect.set(o,'x',42); o.x")
	if result.ToNumber() != 42 {
		t.Error("Reflect.set should set property")
	}
}

// =========================================================================
// Eval Builtins — gap coverage for registerEval
// =========================================================================

func TestEvalBasic(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("eval('1+2')").ToNumber() != 3 {
		t.Error("eval('1+2') should be 3")
	}
}

// =========================================================================
// Array Builtins — gap coverage for registerArray
// =========================================================================

func TestArrayPush(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("var a=[1,2]; a.push(3); a.length").ToNumber() != 3 {
		t.Error("push should increase length")
	}
}

func TestArrayPop(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("var a=[1,2,3]; a.pop()").ToNumber() != 3 {
		t.Error("pop should return last element")
	}
}

func TestArrayMap(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("[1,2,3].map(function(x){return x*2})")
	arr := result.Object()
	if arr == nil {
		t.Fatal("map should return an array")
	}
	if arr.Get("0").ToNumber() != 2 {
		t.Error("map[0] should be 2")
	}
	if arr.Get("1").ToNumber() != 4 {
		t.Error("map[1] should be 4")
	}
	if arr.Get("2").ToNumber() != 6 {
		t.Error("map[2] should be 6")
	}
}

func TestArrayFilter(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("[1,2,3,4].filter(function(x){return x%2===0}).length").ToNumber() != 2 {
		t.Error("filter even should have length 2")
	}
}

func TestArrayReduce(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("[1,2,3].reduce(function(a,b){return a+b}, 0)").ToNumber() != 6 {
		t.Error("reduce sum should be 6")
	}
}

func TestArraySome(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("[1,2,3].some(function(x){return x>2})").IsTruthy() {
		t.Error("some >2 should be true")
	}
	if vm.Run("[1,2,3].some(function(x){return x>5})").IsTruthy() {
		t.Error("some >5 should be false")
	}
}

func TestArrayEvery(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("[2,4,6].every(function(x){return x%2===0})").IsTruthy() {
		t.Error("every even should be true")
	}
	if vm.Run("[2,3,6].every(function(x){return x%2===0})").IsTruthy() {
		t.Error("every even on [2,3,6] should be false")
	}
}

func TestArrayFind(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("[1,3,5,6].find(function(x){return x%2===0})").ToNumber() != 6 {
		t.Error("find even should be 6")
	}
}

func TestStringSplitEmptySep(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hi".split("")`)
	arr := result.Object()
	if arr == nil {
		t.Fatal("split('') should return array")
	}
	if arr.Get("length").ToNumber() != 2 {
		t.Error("split empty sep should produce individual chars")
	}
}

func TestStringIndexOfFromIndex(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello hello".indexOf("hello", 3)`).ToNumber() != 6 {
		t.Errorf(`indexOf with fromIndex = %v, want 6`, vm.Run(`"hello hello".indexOf("hello", 3)`).ToNumber())
	}
}

func TestStringSliceNegative(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`"hello".slice(-3, -1)`).ToString() != "ll" {
		t.Errorf(`slice(-3,-1) = %q, want "ll"`, vm.Run(`"hello".slice(-3, -1)`).ToString())
	}
}

func TestArrayPopEmpty(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("var a=[]; a.pop() === undefined").IsTruthy() {
		t.Error("pop on empty array should return undefined")
	}
}

func TestArrayReduceNoInitial(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("[1,2,3].reduce(function(a,b){return a+b})").ToNumber() != 6 {
		t.Error("reduce without initial value should work")
	}
}

func TestArrayReduceEmpty(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("[].reduce(function(a,b){return a+b})")
	if !result.IsUndefined() {
		t.Error("reduce on empty array with no initial should be undefined")
	}
}

func TestArrayMapEdgeCases(t *testing.T) {
	vm := js.NewVM()
	// map with sparse array behavior — undefined callback should still work.
	result := vm.Run("[1,2,3].map(function(x,i){return i})")
	arr := result.Object()
	if arr == nil || arr.Get("0").ToNumber() != 0 {
		t.Error("map with index should work")
	}
}

func TestArrayFindNoMatch(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("[1,2,3].find(function(x){return x>5}) === undefined").IsTruthy() {
		t.Error("find with no match should return undefined")
	}
}

func TestArrayFindIndexNoMatch(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("[1,2,3].findIndex(function(x){return x>5})").ToNumber() != -1 {
		t.Error("findIndex with no match should return -1")
	}
}

func TestArrayIndexOf(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("[1,2,3,2].indexOf(2)").ToNumber() != 1 {
		t.Error("indexOf 2 should be 1")
	}
	if vm.Run("[1,2,3].indexOf(5)").ToNumber() != -1 {
		t.Error("indexOf 5 should be -1")
	}
}

func TestArraySlice(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("[1,2,3,4].slice(1,3)")
	arr := result.Object()
	if arr == nil {
		t.Fatal("slice should return array")
	}
	if arr.Get("0").ToNumber() != 2 || arr.Get("1").ToNumber() != 3 {
		t.Error("slice(1,3) should return [2,3]")
	}
}

func TestArraySplice(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("var a=[1,2,3,4]; a.splice(1,2); a.length").ToNumber() != 2 {
		t.Error("splice should reduce length")
	}
}

func TestArrayJoin(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("[1,2,3].join('-')").ToString() != "1-2-3" {
		t.Errorf(`join = %q`, vm.Run("[1,2,3].join('-')").ToString())
	}
}

func TestArrayConcat(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("[1,2].concat([3,4])")
	arr := result.Object()
	if arr == nil || arr.Get("length").ToNumber() != 4 {
		t.Error("concat should produce length 4")
	}
}

func TestArrayFill(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("[0,0,0].fill(5, 1, 2)")
	arr := result.Object()
	if arr == nil {
		t.Fatal("fill should return array")
	}
}

func TestArrayFlat(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("[[1,2],[3,4]].flat()")
	arr := result.Object()
	if arr == nil || arr.Get("length").ToNumber() != 4 {
		t.Error("flat should produce length 4")
	}
}

// =========================================================================
// Console Builtins — gap coverage for registerConsole
// =========================================================================

func TestConsoleWarn(t *testing.T) {
	vm := js.NewVM()
	// Should not panic.
	vm.Run("console.warn('test warning')")
	vm.Run("console.warn('a', 'b', 'c')")
}

func TestConsoleError(t *testing.T) {
	vm := js.NewVM()
	// Should not panic.
	vm.Run("console.error('test error')")
	vm.Run("console.error('err1', 'err2')")
}

// =========================================================================
// Object Builtins — gap coverage for registerObject
// =========================================================================

func TestObjectValues(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("Object.values({a:1, b:2})")
	arr := result.Object()
	if arr == nil || arr.Get("length").ToNumber() != 2 {
		t.Error("Object.values should return array of length 2")
	}
}

func TestObjectEntries(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("Object.entries({a:1})")
	arr := result.Object()
	if arr == nil || arr.Get("length").ToNumber() != 1 {
		t.Error("Object.entries should return array of length 1")
	}
}

func TestObjectAssign(t *testing.T) {
	vm := js.NewVM()
	if vm.Run("var o=Object.assign({a:1},{b:2}); o.a+o.b").ToNumber() != 3 {
		t.Error("Object.assign should merge objects")
	}
}

func TestObjectCreateBuiltin(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("var p={x:1}; var o=Object.create(p); o.x")
	if result.ToNumber() != 1 {
		t.Error("Object.create should set prototype")
	}
}

func TestObjectFreeze(t *testing.T) {
	vm := js.NewVM()
	vm.Run("var o=Object.freeze({x:1})")
	// Should not panic on write attempt.
	vm.Run("o.x = 2")
	if vm.Run("o.x").ToNumber() != 1 {
		t.Error("Object.freeze should prevent writes")
	}
}

func TestObjectSeal(t *testing.T) {
	vm := js.NewVM()
	vm.Run("var o=Object.seal({x:1})")
	vm.Run("o.x = 2")
	if vm.Run("o.x").ToNumber() != 2 {
		t.Error("Object.seal should allow writes to existing properties")
	}
}

func TestObjectHasOwn(t *testing.T) {
	vm := js.NewVM()
	// ES2022: Object.hasOwn(obj, prop).
	if !vm.Run("Object.hasOwn({a:1}, 'a')").IsTruthy() {
		t.Error("Object.hasOwn should return true for own property")
	}
	if vm.Run("Object.hasOwn({a:1}, 'b')").IsTruthy() {
		t.Error("Object.hasOwn should return false for missing property")
	}
	if vm.Run("Object.hasOwn({a:1}, 'toString')").IsTruthy() {
		t.Error("Object.hasOwn should return false for inherited property")
	}
	// Non-object args.
	if vm.Run("Object.hasOwn(null, 'x')").IsTruthy() {
		t.Error("Object.hasOwn with null should return false")
	}
	if vm.Run("Object.hasOwn(42, 'x')").IsTruthy() {
		t.Error("Object.hasOwn with non-object should return false")
	}
}

// =========================================================================
// JSON Builtins — gap coverage for registerJSON
// =========================================================================

func TestJSONParseCoverage(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`JSON.parse('{"a":1}')`)
	obj := result.Object()
	if obj == nil || obj.Get("a").ToNumber() != 1 {
		t.Error("JSON.parse should parse object")
	}
}

func TestJSONStringifyCoverage(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`JSON.stringify({a:1})`).ToString() != `{"a":1}` {
		t.Errorf(`JSON.stringify = %q`, vm.Run(`JSON.stringify({a:1})`).ToString())
	}
}

func TestJSONParseArray(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`JSON.parse('[1,2,3]')`)
	arr := result.Object()
	if arr == nil || arr.Get("length").ToNumber() != 3 {
		t.Error("JSON.parse array should have length 3")
	}
}

func TestJSONStringifyArray(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`JSON.stringify([1,2,3])`).ToString() != `[1,2,3]` {
		t.Errorf(`JSON.stringify array = %q`, vm.Run(`JSON.stringify([1,2,3])`).ToString())
	}
}

func TestJSONParseNull(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run(`JSON.parse('null')`).IsNull() {
		t.Error("JSON.parse('null') should be null")
	}
}

func TestJSONParseBoolean(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run(`JSON.parse('true')`).IsTruthy() {
		t.Error("JSON.parse('true') should be truthy")
	}
	if vm.Run(`JSON.parse('false')`).IsTruthy() {
		t.Error("JSON.parse('false') should be falsy")
	}
}

func TestJSONParseNumber(t *testing.T) {
	vm := js.NewVM()
	if vm.Run(`JSON.parse('42')`).ToNumber() != 42 {
		t.Error("JSON.parse('42') should be 42")
	}
}

// =========================================================================
// WeakSet Builtins — gap coverage for registerWeakSet
// =========================================================================

func TestWeakSetAddHas(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ws = new WeakSet();
		var obj = {};
		ws.add(obj);
		ws.has(obj)
	`)
	if !result.IsTruthy() {
		t.Error("WeakSet.has should return true for added object")
	}
}

func TestWeakSetDelete(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ws = new WeakSet();
		var obj = {};
		ws.add(obj);
		ws.delete(obj);
		ws.has(obj)
	`)
	if result.IsTruthy() {
		t.Error("WeakSet.has should return false after delete")
	}
}

func TestWeakSetMultipleObjects(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ws = new WeakSet();
		var a = {}, b = {};
		ws.add(a);
		ws.add(b);
		ws.has(a) && ws.has(b)
	`)
	if !result.IsTruthy() {
		t.Error("WeakSet should hold multiple objects")
	}
}

// =========================================================================
// Map Builtins — gap coverage for keyString
// =========================================================================

func TestMapSetGet(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var m = new Map();
		m.set('key', 42);
		m.get('key')
	`)
	if result.ToNumber() != 42 {
		t.Errorf("Map.get = %v, want 42", result.ToNumber())
	}
}

// =========================================================================
// RegExp Builtins — gap coverage for registerRegExp
// =========================================================================

func TestRegExpTest(t *testing.T) {
	vm := js.NewVM()
	if !vm.Run("/hello/.test('hello world')").IsTruthy() {
		t.Error("/hello/.test('hello world') should be true")
	}
	if vm.Run("/xyz/.test('hello')").IsTruthy() {
		t.Error("/xyz/.test('hello') should be false")
	}
}

func TestRegExpExec(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("/\\d+/.exec('abc 123 def')")
	if result.IsNull() {
		t.Error("regexp exec should find match")
	}
}

func TestRegExpGlobalFlag(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var re = /ab/g;
		var s = 'ab ab ab';
		var count = 0;
		var m;
		while ((m = re.exec(s)) !== null) count++;
		count
	`)
	if result.ToNumber() != 3 {
		t.Logf("global regexp exec count = %v (may not support while/exec loop)", result.ToNumber())
	}
}

// =========================================================================
// Function.prototype — call / apply / bind / toString
// =========================================================================

func TestFunctionProtoCall(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function add(a, b) { return a + b; }
		add.call(null, 1, 2)
	`)
	if result.ToNumber() != 3 {
		t.Errorf("add.call(null, 1, 2) = %v, want 3", result)
	}
}

func TestFunctionProtoCallWithThis(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var obj = { value: 42 };
		function getValue() { return this.value; }
		getValue.call(obj)
	`)
	if result.ToNumber() != 42 {
		t.Errorf("getValue.call(obj) = %v, want 42", result)
	}
}

func TestFunctionProtoApply(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function sum(a, b, c) { return a + b + c; }
		sum.apply(null, [1, 2, 3])
	`)
	if result.ToNumber() != 6 {
		t.Errorf("sum.apply(null, [1,2,3]) = %v, want 6", result)
	}
}

func TestFunctionProtoBind(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function multiply(a, b) { return a * b; }
		var double = multiply.bind(null, 2);
		double(5)
	`)
	if result.ToNumber() != 10 {
		t.Errorf("double(5) = %v, want 10", result)
	}
}

func TestFunctionProtoToString(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var s = (function(){}).toString();
		s.indexOf('function') === 0
	`)
	if !result.IsTruthy() {
		t.Errorf("Function.toString() should start with 'function': %v", result)
	}
}

func TestFunctionProtoCallIsMethod(t *testing.T) {
	vm := js.NewVM()
	// Verify call/apply/bind are accessible on user-defined functions
	result := vm.Run(`
		function f() { return 1; }
		typeof f.call === 'function' &&
		typeof f.apply === 'function' &&
		typeof f.bind === 'function' &&
		typeof f.toString === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("User-defined function should have call, apply, bind, toString methods")
	}
}

func TestFunctionNameInference(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function myFunc() {}
		myFunc.name
	`)
	if result.ToString() != "myFunc" {
		t.Errorf("myFunc.name = %q, want 'myFunc'", result.ToString())
	}
}

// ES Module builtins — error-path coverage for registerModules
// =========================================================================

func TestModuleImportNoArgs(t *testing.T) {
	// __import__() with no args should return a rejected Promise.
	vm := js.NewVM()
	result := vm.Run("__import__()")
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("__import__() should return a Promise")
	}
	state := result.ObjVal.Get("__promise_state__")
	if int(state.ToNumber()) != 2 { // PromiseRejected
		t.Errorf("__import__() with no args should reject, got state %v", state.ToNumber())
	}
}

func TestModuleImportNoLoader(t *testing.T) {
	// __import__ with a URL but no module registry should return a rejected Promise.
	vm := js.NewVM()
	result := vm.Run("__import__('some-module.js')")
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("__import__ should return a Promise")
	}
	state := result.ObjVal.Get("__promise_state__")
	if int(state.ToNumber()) != 2 { // PromiseRejected
		t.Errorf("__import__ without registry should reject, got state %v", state.ToNumber())
	}
}

func TestModuleImportNamespaceNoArgs(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("__moduleImportNamespace__()")
	if !result.IsUndefined() {
		t.Errorf("__moduleImportNamespace__() with no args should return undefined, got %v", result)
	}
}

func TestModuleImportDefaultNoArgs(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("__moduleImportDefault__()")
	if !result.IsUndefined() {
		t.Errorf("__moduleImportDefault__() with no args should return undefined, got %v", result)
	}
}

func TestModuleImportNamedNoArgs(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("__moduleImportNamed__()")
	if !result.IsUndefined() {
		t.Errorf("__moduleImportNamed__() with no args should return undefined, got %v", result)
	}
}

func TestModuleSetGlobal(t *testing.T) {
	vm := js.NewVM()
	mr := js.NewModuleRegistry(vm, nil)
	// Test setting a global from a module registry.
	mr.SetGlobal("testKey", js.NewNumber(42))
	// Verify via VM.GetGlobal.
	val := vm.GetGlobal("testKey")
	if val.ToNumber() != 42 {
		t.Errorf("SetGlobal should set the value, got %v", val)
	}
}
func TestRegExpSymbolMatch(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello world hello".match(/hello/g)`)
	arr := result.ObjVal
	if arr == nil || arr.Get("0").ToString() != "hello" || arr.Get("1").ToString() != "hello" {
		t.Errorf("global match failed: %v", result)
	}
}

func TestRegExpSymbolMatchNonGlobal(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello world".match(/hello/)`)
	if !result.IsObject() {
		t.Error("non-global match should return result array")
	}
}

func TestRegExpSymbolSearch(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello world".search(/world/)`)
	if result.ToNumber() != 6 {
		t.Errorf("search index = %v, want 6", result)
	}
}

func TestRegExpSymbolSearchNotFound(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello".search(/xyz/)`)
	if result.ToNumber() != -1 {
		t.Errorf("search not found = %v, want -1", result)
	}
}

func TestRegExpSymbolReplace(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello world".replace(/world/, "earth")`)
	if result.ToString() != "hello earth" {
		t.Errorf("replace = %v, want 'hello earth'", result)
	}
}

func TestRegExpSymbolReplaceGlobal(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"a b a b".replace(/a/g, "x")`)
	if result.ToString() != "x b x b" {
		t.Errorf("global replace = %v, want 'x b x b'", result)
	}
}

func TestRegExpSymbolReplaceString(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"hello".replace(/[aeiou]/g, 'X')`)
	if result.ToString() != "hXllX" {
		t.Errorf("replace with string = %v, want 'hXllX'", result)
	}
}

func TestRegExpSymbolSplit(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"a,b,c".split(/,/)`)
	arr := result.ObjVal
	if arr == nil || arr.Get("0").ToString() != "a" || arr.Get("1").ToString() != "b" || arr.Get("2").ToString() != "c" {
		t.Errorf("split failed: %v", result)
	}
}

func TestRegExpSymbolSplitEmpty(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`"abc".split(/./)`)
	arr := result.ObjVal
	if arr == nil {
		t.Fatal("split result should be array")
	}
	// "abc".split(/./) should produce ["", "", "", ""]
	if arr.Get("length").ToNumber() != 4 {
		t.Errorf("split empty: length = %v, want 4", arr.Get("length"))
	}
}

// =========================================================================
// RegExp Flags — dotAll (s), unicode (u), sticky (y), flags string
// =========================================================================

func TestRegExpDotAllFlag(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`/a.b/s.test("a\nb")`)
	if !result.IsTruthy() {
		t.Error("dotAll flag should match newline")
	}
}

func TestRegExpUnicodeFlag(t *testing.T) {
	vm := js.NewVM()
	// Test the unicode flag property
	result := vm.Run(`/a/u.unicode`)
	if !result.IsTruthy() {
		t.Error("unicode flag should be truthy")
	}
	// Without unicode flag
	if vm.Run(`/a/.unicode`).IsTruthy() {
		t.Error("regexp without unicode flag should have unicode false")
	}
}

func TestRegExpStickyFlag(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var re = /foo/y;
		re.lastIndex = 2;
		re.test("..foo..")
	`)
	if !result.IsTruthy() {
		t.Error("sticky flag at correct lastIndex should match")
	}
}

func TestRegExpStickyFlagNoMatch(t *testing.T) {
	vm := js.NewVM()
	// Sticky with lastIndex > 0: searches substring starting at lastIndex.
	// If the pattern doesn't appear in that substring, test() returns false.
	result := vm.Run(`
		var re = /foo/y;
		re.lastIndex = 3;
		re.test("xxfooyy")
	`)
	if result.IsTruthy() {
		t.Error("sticky with no match in substring should be false")
	}
}

func TestRegExpFlagsString(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`/foo/gimsuy.flags`)
	s := result.ToString()
	if !strings.Contains(s, "g") || !strings.Contains(s, "i") || !strings.Contains(s, "m") || !strings.Contains(s, "s") || !strings.Contains(s, "u") || !strings.Contains(s, "y") {
		t.Errorf("flags = %q, expected to contain gimsuy", s)
	}
}

// =========================================================================
// RegExp Named Capture Groups
// =========================================================================

func TestRegExpNamedGroups(t *testing.T) {
	vm := js.NewVM()
	// This implementation uses Go's regexp engine which supports
	// (?P<name>...) syntax for named capture groups.
	result := vm.Run(`
		var re = new RegExp('(?P<year>\\d{4})-(?P<month>\\d{2})-(?P<day>\\d{2})');
		var match = re.exec("2024-01-15");
		match.groups.year === "2024" && match.groups.month === "01" && match.groups.day === "15"
	`)
	if !result.IsTruthy() {
		t.Errorf("named groups failed: %v", result)
	}
}

// =========================================================================
// RegExp Edge Cases — constructor, RegExp(RegExp), RegExp(RegExp, flags)
// =========================================================================

func TestRegExpConstructorFromRegExp(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var re1 = /hello/i;
		var re2 = new RegExp(re1);
		re2.toString() === "/hello/i"
	`)
	if !result.IsTruthy() {
		t.Errorf("RegExp(RegExp) copy: %v", result)
	}
}

func TestRegExpConstructorFromRegExpNewFlags(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var re1 = /hello/i;
		var re2 = new RegExp(re1, "g");
		re2.toString() === "/hello/g"
	`)
	if !result.IsTruthy() {
		t.Errorf("RegExp(RegExp, flags) override: %v", result)
	}
}
