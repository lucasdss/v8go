package js

import (
	"testing"
)

// TestPromiseReactionQueue tests the reaction queue mechanism directly.
func TestPromiseReactionQueue(t *testing.T) {
	vm := NewVM()

	var resolveFn func(JSValue)
	p := vm.NewPromise(func(resolve, reject func(JSValue)) {
		resolveFn = resolve
	})
	pObj := p.ObjVal

	callCount := 0
	makeCallback := func() *JSObject {
		cb := NewJSObject()
		cb.ConstructorName = "Function"
		cb.CallFunc = func(this *JSObject, args []JSValue) JSValue {
			callCount++
			return args[0]
		}
		return cb
	}

	thenVal := pObj.Get("then")
	if !thenVal.IsObject() || thenVal.ObjVal == nil {
		t.Fatal("then is not callable")
	}

	for i := 0; i < 3; i++ {
		thenVal.ObjVal.Call(pObj, []JSValue{NewObject(makeCallback())})
	}

	if len(vm.promiseReactions[pObj]) != 3 {
		t.Fatalf("expected 3 reactions, got %d", len(vm.promiseReactions[pObj]))
	}

	resolveFn(NewNumber(42))

	if callCount != 3 {
		t.Errorf("expected 3 callback calls, got %d", callCount)
	}
	if len(vm.promiseReactions[pObj]) != 0 {
		t.Error("reaction queue not drained after resolve")
	}

	state := pObj.Get("__promise_state__")
	if state.ToNumber() != float64(PromiseFulfilled) {
		t.Errorf("expected Fulfilled state, got %v", state.ToNumber())
	}
}

func TestPromiseReactionQueueReject(t *testing.T) {
	vm := NewVM()

	var rejectFn func(JSValue)
	p := vm.NewPromise(func(resolve, reject func(JSValue)) {
		rejectFn = reject
	})
	pObj := p.ObjVal

	cbCalled := false
	cb := NewJSObject()
	cb.ConstructorName = "Function"
	cb.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		cbCalled = true
		return Undefined
	}

	thenVal := pObj.Get("then")
	thenVal.ObjVal.Call(pObj, []JSValue{Undefined, NewObject(cb)})

	if len(vm.promiseReactions[pObj]) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(vm.promiseReactions[pObj]))
	}

	rejectFn(NewString("error"))

	if !cbCalled {
		t.Error("reject callback was not called")
	}
}

func TestPromiseChain(t *testing.T) {
	vm := NewVM()

	var resolveFn func(JSValue)
	p := vm.NewPromise(func(resolve, reject func(JSValue)) {
		resolveFn = resolve
	})
	pObj := p.ObjVal

	doubleCb := NewJSObject()
	doubleCb.ConstructorName = "Function"
	doubleCb.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		return NewNumber(args[0].ToNumber() * 2)
	}

	thenVal := pObj.Get("then")
	chainResult := thenVal.ObjVal.Call(pObj, []JSValue{NewObject(doubleCb)})

	var finalVal JSValue
	finalCb := NewJSObject()
	finalCb.ConstructorName = "Function"
	finalCb.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		finalVal = args[0]
		return Undefined
	}

	chainObj := chainResult.ObjVal
	chainThen := chainObj.Get("then")
	chainThen.ObjVal.Call(chainObj, []JSValue{NewObject(finalCb)})

	resolveFn(NewNumber(21))

	if finalVal.ToNumber() != 42 {
		t.Errorf("expected chained result 42, got %v", finalVal.ToNumber())
	}
}

func TestPromiseAlreadySettledThen(t *testing.T) {
	vm := NewVM()

	p := vm.NewPromise(func(resolve, reject func(JSValue)) {
		resolve(NewNumber(99))
	})
	pObj := p.ObjVal

	cbCalled := false
	var cbArg JSValue
	cb := NewJSObject()
	cb.ConstructorName = "Function"
	cb.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		cbCalled = true
		cbArg = args[0]
		return Undefined
	}

	thenVal := pObj.Get("then")
	thenVal.ObjVal.Call(pObj, []JSValue{NewObject(cb)})

	if !cbCalled {
		t.Error("callback not called on already-resolved promise")
	}
	if cbArg.ToNumber() != 99 {
		t.Errorf("expected arg 99, got %v", cbArg.ToNumber())
	}
}

func TestPromiseAlreadySettledCatch(t *testing.T) {
	vm := NewVM()

	p := vm.NewPromise(func(resolve, reject func(JSValue)) {
		reject(NewString("boom"))
	})
	pObj := p.ObjVal

	cbCalled := false
	cb := NewJSObject()
	cb.ConstructorName = "Function"
	cb.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		cbCalled = true
		return Undefined
	}

	catchVal := pObj.Get("catch")
	if !catchVal.IsObject() || catchVal.ObjVal == nil {
		t.Fatal("catch is not callable")
	}
	catchVal.ObjVal.Call(pObj, []JSValue{NewObject(cb)})

	if !cbCalled {
		t.Error("catch callback not called on already-rejected promise")
	}
}

func TestPromisePendingState(t *testing.T) {
	vm := NewVM()

	p := vm.NewPromise(func(resolve, reject func(JSValue)) {})
	pObj := p.ObjVal

	state := pObj.Get("__promise_state__")
	if state.ToNumber() != float64(PromisePending) {
		t.Errorf("expected Pending (0), got %v", state.ToNumber())
	}
}

// JS-level tests using property access patterns (which work reliably).

func TestPromiseResolveThenJS(t *testing.T) {
	vm := NewVM()

	result := vm.Run(`
		var p = Promise.resolve(42);
		var obj = {val: 0};
		p.then(function(v) { obj.val = v; });
		obj.val
	`)
	if result.ToNumber() != 42 {
		t.Errorf("Promise.resolve(42).then(): expected obj.val=42, got %v", result.ToNumber())
	}
}

func TestPromiseRejectCatchJS(t *testing.T) {
	vm := NewVM()

	result := vm.Run(`
		var p = Promise.reject('fail');
		var obj = {val: 'ok'};
		p.catch(function(e) { obj.val = e; });
		obj.val
	`)
	if result.StrVal != "fail" {
		t.Errorf("Promise.reject().catch(): expected 'fail', got %q", result.StrVal)
	}
}

func TestPromisePendingResolveThenJS(t *testing.T) {
	vm := NewVM()

	result := vm.Run(`
		var resolveFn;
		var p = new Promise(function(resolve, reject) {
			resolveFn = resolve;
		});
		var obj = {val: 0};
		p.then(function(v) { obj.val = v; });
		resolveFn(42);
		obj.val
	`)
	if result.ToNumber() != 42 {
		t.Errorf("pending .then(): expected obj.val=42, got %v", result.ToNumber())
	}
}

func TestPromisePendingMultipleThenJS(t *testing.T) {
	vm := NewVM()

	result := vm.Run(`
		var resolveFn;
		var p = new Promise(function(resolve, reject) {
			resolveFn = resolve;
		});
		var obj = {count: 0};
		p.then(function(v) { obj.count = obj.count + 1; });
		p.then(function(v) { obj.count = obj.count + 1; });
		p.then(function(v) { obj.count = obj.count + 1; });
		resolveFn(1);
		obj.count
	`)
	if result.ToNumber() != 3 {
		t.Errorf("pending multiple .then(): expected count=3, got %v", result.ToNumber())
	}
}

func TestPromisePendingChainJS(t *testing.T) {
	vm := NewVM()

	result := vm.Run(`
		var resolveFn;
		var p = new Promise(function(resolve, reject) {
			resolveFn = resolve;
		});
		var obj = {chainResult: null};
		p.then(function(v) { obj.chainResult = v * 2; return obj.chainResult; })
		 .then(function(v) { obj.chainResult = v; });
		resolveFn(21);
		obj.chainResult
	`)
	if result.ToNumber() != 42 {
		t.Errorf("pending chain: expected chainResult=42, got %v", result.ToNumber())
	}
}

func TestPromisePendingCatchJS(t *testing.T) {
	vm := NewVM()

	result := vm.Run(`
		var rejectFn;
		var p = new Promise(function(resolve, reject) {
			rejectFn = reject;
		});
		var obj = {reason: null};
		p.catch(function(e) { obj.reason = e; });
		rejectFn('fail-reason');
		obj.reason
	`)
	if result.StrVal != "fail-reason" {
		t.Errorf("pending .catch(): expected reason='fail-reason', got %q", result.StrVal)
	}
}

func TestCatchRecoveryChain(t *testing.T) {
	vm := NewVM()
	result := vm.Run(`
		var obj = {final: null};
		var p = Promise.reject('bad');
		p.catch(function(e) { return 'recovered'; })
		 .then(function(v) { obj.final = v; });
		obj.final
	`)
	if result.StrVal != "recovered" {
		t.Errorf("Expected 'recovered' from catch recovery chain, got %q", result.StrVal)
	}
}

func TestPromiseResolveThenablePending(t *testing.T) {
	vm := NewVM()
	// Promise.resolve(thenable) should unwrap the thenable.
	// In our implementation this may resolve synchronously since the inner
	// promise is already settled. The key invariant: result must be 42, not NaN.
	result := vm.Run(`
		var p = Promise.resolve(Promise.resolve(42));
		p.__promise_result__
	`)
	if result.ToNumber() != 42 {
		t.Errorf("Promise.resolve(thenable): expected result=42, got %v (state may be 0=pending or 1=fulfilled)", result.ToNumber())
	}
}
