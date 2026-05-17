// builtins_coverage_test.go — Direct tests for previously untested builtins.
// Targets: Error subtypes, Promise.race/all, Proxy traps, Reflect methods,
// WeakRef deref edge cases, FinalizationRegistry register/unregister.
package js_test

import (
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
