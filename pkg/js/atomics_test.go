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

// TestAtomicsValidationErrors exercises the error paths in validateAtomicsArgs:
// too few arguments, non-object first arg, and non-typed-array first arg.
func TestAtomicsValidationErrors(t *testing.T) {
	vm := js.NewVM()
	// Too few args: Atomics.add() with only 0 or 1 arg returns 0.
	if r := vm.Run("Atomics.add()"); r.ToNumber() != 0 {
		t.Errorf("Atomics.add() = %v, want 0", r)
	}
	if r := vm.Run("Atomics.add(undefined)"); r.ToNumber() != 0 {
		t.Errorf("Atomics.add(undefined) = %v, want 0", r)
	}
	// Non-object first arg
	if r := vm.Run("Atomics.add(42, 0, 1)"); r.ToNumber() != 0 {
		t.Errorf("Atomics.add(42, 0, 1) = %v, want 0", r)
	}
	// Non-typed-array object (plain object)
	if r := vm.Run("Atomics.add({}, 0, 1)"); r.ToNumber() != 0 {
		t.Errorf("Atomics.add({}, 0, 1) = %v, want 0", r)
	}
	// Atomics.add with null
	if r := vm.Run("Atomics.add(null, 0, 1)"); r.ToNumber() != 0 {
		t.Errorf("Atomics.add(null, 0, 1) = %v, want 0", r)
	}
}

// TestAtomicsDetectKindVariants exercises detectKind through all TypedArray
// variants used with Atomics operations. Each TypedArray constructor name maps
// to a different typedArrayKind constant, so this covers all switch branches
// in detectKind.
func TestAtomicsDetectKindVariants(t *testing.T) {
	vm := js.NewVM()
	tests := []struct {
		ta      string
		byteLen int
		initial string
		delta   string
		wantOld string
	}{
		{"Int8Array", 1, "1", "1", "1"},
		{"Uint8Array", 1, "1", "1", "1"},
		{"Uint8ClampedArray", 1, "1", "1", "1"},
		{"Int16Array", 2, "1", "1", "1"},
		{"Uint16Array", 2, "1", "1", "1"},
		{"Int32Array", 4, "1", "1", "1"},
		{"Uint32Array", 4, "1", "1", "1"},
		{"Float32Array", 4, "1.5", "0.5", "1.5"},
		{"Float64Array", 8, "1.5", "0.5", "1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.ta, func(t *testing.T) {
			src := "var sab = new SharedArrayBuffer(" + itoa(tt.byteLen) + ");" +
				"var view = new " + tt.ta + "(sab);" +
				"view[0] = " + tt.initial + ";" +
				"Atomics.add(view, 0, " + tt.delta + ")"
			result := vm.Run(src)
			got := result.ToNumber()
			want := float64(0)
			if tt.wantOld == "1.5" {
				want = 1.5
			} else {
				want = 1
			}
			if got != want {
				t.Errorf("%s Atomics.add: expected old=%v, got %v", tt.ta, want, got)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
