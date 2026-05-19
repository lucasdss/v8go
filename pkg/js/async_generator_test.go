package js

import (
	"testing"
)

func TestAsyncGeneratorBasic(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	result := vm.Run(`
		async function* gen() { yield 1; yield 2; }
		var g = gen();
		typeof g.next === "function" && typeof g.return === "function" && typeof g.throw === "function"
	`)
	if !result.IsTruthy() {
		t.Errorf("async generator should have next/return/throw methods: %v", result)
	}
}

func TestAsyncGeneratorNextReturnsPromise(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	result := vm.Run(`
		async function* gen() { yield 42; }
		var g = gen();
		var v = g.next();
		typeof v.then === "function"
	`)
	if !result.IsTruthy() {
		t.Errorf("async generator .next() should return a Promise: %v", result)
	}
}

func TestAsyncGeneratorSymbolAsyncIterator(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	result := vm.Run(`
		async function* gen() { yield 1; }
		var g = gen();
		var it = g[Symbol.asyncIterator]();
		it === g
	`)
	if !result.IsTruthy() {
		t.Errorf("async generator[Symbol.asyncIterator]() should return itself: %v", result)
	}
}

func TestAsyncGeneratorForAwaitPattern(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	result := vm.Run(`
		var collected = [];
		async function* gen() { yield 1; yield 2; yield 3; }
		var g = gen();
		var it = g[Symbol.asyncIterator]();
		typeof it.next === "function" && typeof it.return === "function"
	`)
	if !result.IsTruthy() {
		t.Errorf("async iterator from Symbol.asyncIterator should have next/return: %v", result)
	}
}

func TestAsyncGeneratorSymbolAsyncIteratorExists(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	result := vm.Run(`
		typeof Symbol.asyncIterator === "symbol"
	`)
	if !result.IsTruthy() {
		t.Errorf("Symbol.asyncIterator should be a symbol: %v", result)
	}
}

func TestAsyncGeneratorPrototypeFallbackNext(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	if AsyncGeneratorPrototype == nil {
		t.Fatal("AsyncGeneratorPrototype not registered")
	}
	nextVal := AsyncGeneratorPrototype.Get("next")
	if !nextVal.IsObject() || nextVal.ObjVal == nil || !nextVal.ObjVal.IsCallable() {
		t.Fatal("AsyncGeneratorPrototype.next should be callable")
	}
	result := nextVal.ObjVal.Call(AsyncGeneratorPrototype, nil)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("AsyncGeneratorPrototype.next() should return an object (Promise)")
	}
	thenVal := result.ObjVal.Get("then")
	if !thenVal.IsObject() || thenVal.ObjVal == nil || !thenVal.ObjVal.IsCallable() {
		t.Error("AsyncGeneratorPrototype.next() should return a Promise with .then")
	}
}

func TestAsyncGeneratorPrototypeFallbackReturn(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	if AsyncGeneratorPrototype == nil {
		t.Fatal("AsyncGeneratorPrototype not registered")
	}
	retVal := AsyncGeneratorPrototype.Get("return")
	if !retVal.IsObject() || retVal.ObjVal == nil || !retVal.ObjVal.IsCallable() {
		t.Fatal("AsyncGeneratorPrototype.return should be callable")
	}
	result := retVal.ObjVal.Call(AsyncGeneratorPrototype, nil)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("AsyncGeneratorPrototype.return() should return an object (Promise)")
	}
	thenVal := result.ObjVal.Get("then")
	if !thenVal.IsObject() || thenVal.ObjVal == nil || !thenVal.ObjVal.IsCallable() {
		t.Error("AsyncGeneratorPrototype.return() should return a Promise with .then")
	}
}

func TestAsyncGeneratorPrototypeFallbackThrow(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	if AsyncGeneratorPrototype == nil {
		t.Fatal("AsyncGeneratorPrototype not registered")
	}
	throwVal := AsyncGeneratorPrototype.Get("throw")
	if !throwVal.IsObject() || throwVal.ObjVal == nil || !throwVal.ObjVal.IsCallable() {
		t.Fatal("AsyncGeneratorPrototype.throw should be callable")
	}
	result := throwVal.ObjVal.Call(AsyncGeneratorPrototype, nil)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("AsyncGeneratorPrototype.throw() should return an object (Promise)")
	}
	thenVal := result.ObjVal.Get("then")
	if !thenVal.IsObject() || thenVal.ObjVal == nil || !thenVal.ObjVal.IsCallable() {
		t.Error("AsyncGeneratorPrototype.throw() should return a Promise with .then")
	}
}

func TestAsyncGeneratorPrototypeSymbolAsyncIterator(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	if AsyncGeneratorPrototype == nil {
		t.Fatal("AsyncGeneratorPrototype not registered")
	}
	itFn := AsyncGeneratorPrototype.Get(AsyncIteratorSymbol.SymVal)
	if !itFn.IsObject() || itFn.ObjVal == nil || !itFn.ObjVal.IsCallable() {
		t.Fatal("AsyncGeneratorPrototype[Symbol.asyncIterator] should be callable")
	}
	result := itFn.ObjVal.Call(AsyncGeneratorPrototype, nil)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("AsyncGeneratorPrototype[Symbol.asyncIterator]() should return an object")
	}
}
