package js_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestSharedArrayBufferBasic(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(16);
		sab.byteLength === 16
	`)
	if !result.IsTruthy() {
		t.Errorf("SharedArrayBuffer: byteLength should be 16, got %v", result)
	}
}

func TestSharedArrayBufferZeroLength(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(0);
		sab.byteLength === 0
	`)
	if !result.IsTruthy() {
		t.Error("SharedArrayBuffer: zero-length should work")
	}
}

func TestSharedArrayBufferNegativeLength(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(-5);
		sab.byteLength === 0
	`)
	if !result.IsTruthy() {
		t.Error("SharedArrayBuffer: negative length should clamp to 0")
	}
}

func TestSharedArrayBufferSlice(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(16);
		var sliced = sab.slice(4, 12);
		sliced.byteLength === 8
	`)
	if !result.IsTruthy() {
		t.Errorf("SharedArrayBuffer.slice: expected 8, got %v", result)
	}
}

func TestSharedArrayBufferSlicePreservesType(t *testing.T) {
	vm := js.NewVM()
	// Verify that slice returns a SharedArrayBuffer (not an ArrayBuffer).
	result := vm.Run(`
		var sab = new SharedArrayBuffer(16);
		var sliced = sab.slice(4, 12);
		sliced instanceof SharedArrayBuffer
	`)
	if !result.IsTruthy() {
		t.Error("SharedArrayBuffer slice should return SharedArrayBuffer instance")
	}
}

func TestSharedArrayBufferWithTypedArray(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 42;
		view[0]
	`)
	if result.ToNumber() != 42 {
		t.Errorf("SharedArrayBuffer+Int32Array: expected 42, got %v", result)
	}
}

func TestAtomicsAdd(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 1;
		Atomics.add(view, 0, 2)
	`)
	if result.ToNumber() != 1 {
		t.Errorf("Atomics.add: should return old value 1, got %v", result)
	}
	// Verify the new value
	result2 := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 1;
		Atomics.add(view, 0, 2);
		view[0]
	`)
	if result2.ToNumber() != 3 {
		t.Errorf("Atomics.add: view[0] should be 3, got %v", result2)
	}
}

func TestAtomicsSub(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 10;
		Atomics.sub(view, 0, 3)
	`)
	if result.ToNumber() != 10 {
		t.Errorf("Atomics.sub: should return old value 10, got %v", result)
	}
	result2 := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 10;
		Atomics.sub(view, 0, 3);
		view[0]
	`)
	if result2.ToNumber() != 7 {
		t.Errorf("Atomics.sub: view[0] should be 7, got %v", result2)
	}
}

func TestAtomicsAnd(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 0x0F;
		Atomics.and(view, 0, 0x03)
	`)
	if result.ToNumber() != 15 {
		t.Errorf("Atomics.and: should return old value 15, got %v", result)
	}
}

func TestAtomicsOr(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 0x0F;
		Atomics.or(view, 0, 0xF0)
	`)
	if result.ToNumber() != 15 {
		t.Errorf("Atomics.or: should return old value 15, got %v", result)
	}
}

func TestAtomicsXor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 0xFF;
		Atomics.xor(view, 0, 0x0F)
	`)
	if result.ToNumber() != 255 {
		t.Errorf("Atomics.xor: should return old value 255, got %v", result)
	}
}

func TestAtomicsExchange(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 5;
		Atomics.exchange(view, 0, 42)
	`)
	if result.ToNumber() != 5 {
		t.Errorf("Atomics.exchange: should return old value 5, got %v", result)
	}
	result2 := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 5;
		Atomics.exchange(view, 0, 42);
		view[0]
	`)
	if result2.ToNumber() != 42 {
		t.Errorf("Atomics.exchange: view[0] should be 42, got %v", result2)
	}
}

func TestAtomicsCompareExchange(t *testing.T) {
	vm := js.NewVM()
	// Should exchange when values match
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 5;
		Atomics.compareExchange(view, 0, 5, 10);
		view[0]
	`)
	if result.ToNumber() != 10 {
		t.Errorf("Atomics.compareExchange: should exchange to 10, got %v", result)
	}
	// Should NOT exchange when values differ
	result2 := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 5;
		Atomics.compareExchange(view, 0, 999, 10);
		view[0]
	`)
	if result2.ToNumber() != 5 {
		t.Errorf("Atomics.compareExchange: should not exchange, got %v", result2)
	}
}

func TestAtomicsLoad(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		view[0] = 42;
		Atomics.load(view, 0)
	`)
	if result.ToNumber() != 42 {
		t.Errorf("Atomics.load: expected 42, got %v", result)
	}
}

func TestAtomicsStore(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		Atomics.store(view, 0, 99);
		Atomics.load(view, 0)
	`)
	if result.ToNumber() != 99 {
		t.Errorf("Atomics.store/load: expected 99, got %v", result)
	}
}

func TestAtomicsStoreReturnValue(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		Atomics.store(view, 0, 77)
	`)
	if result.ToNumber() != 77 {
		t.Errorf("Atomics.store: should return stored value 77, got %v", result)
	}
}

func TestAtomicsIsLockFree(t *testing.T) {
	vm := js.NewVM()

	result1 := vm.Run(`Atomics.isLockFree(1)`)
	if !result1.IsTruthy() {
		t.Error("Atomics.isLockFree(1) should be true")
	}

	result2 := vm.Run(`Atomics.isLockFree(2)`)
	if !result2.IsTruthy() {
		t.Error("Atomics.isLockFree(2) should be true")
	}

	result3 := vm.Run(`Atomics.isLockFree(4)`)
	if !result3.IsTruthy() {
		t.Error("Atomics.isLockFree(4) should be true")
	}

	result4 := vm.Run(`Atomics.isLockFree(8)`)
	if result4.IsTruthy() {
		t.Error("Atomics.isLockFree(8) should be false")
	}

	result5 := vm.Run(`Atomics.isLockFree(3)`)
	if result5.IsTruthy() {
		t.Error("Atomics.isLockFree(3) should be false")
	}
}

func TestAtomicsWaitNotifyStubs(t *testing.T) {
	vm := js.NewVM()

	// wait returns "not-equal" immediately
	result1 := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		Atomics.wait(view, 0, 0)
	`)
	if result1.ToString() != "not-equal" {
		t.Errorf("Atomics.wait: expected 'not-equal', got %v", result1)
	}

	// notify returns 0 (no waiters)
	result2 := vm.Run(`
		var sab = new SharedArrayBuffer(4);
		var view = new Int32Array(sab);
		Atomics.notify(view, 0, 1)
	`)
	if result2.ToNumber() != 0 {
		t.Errorf("Atomics.notify: expected 0, got %v", result2)
	}
}

func TestAtomicsWithFloat64Array(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var sab = new SharedArrayBuffer(8);
		var view = new Float64Array(sab);
		view[0] = 3.14;
		Atomics.add(view, 0, 1.0)
	`)
	if result.ToNumber() != 3.14 {
		t.Errorf("Atomics.add on Float64: should return old value 3.14, got %v", result)
	}
}

func TestSharedArrayBufferType(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		typeof SharedArrayBuffer
	`)
	if result.ToString() != "function" {
		t.Errorf("typeof SharedArrayBuffer: expected 'function', got %v", result)
	}
}

func TestAtomicsType(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		typeof Atomics
	`)
	if result.ToString() != "object" {
		t.Errorf("typeof Atomics: expected 'object', got %v", result)
	}
}
