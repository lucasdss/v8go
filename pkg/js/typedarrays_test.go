// typedarrays_test.go — Tests for ArrayBuffer, DataView, and TypedArrays.
package js_test

import (
	"math"
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// --- ArrayBuffer tests ---

func TestArrayBufferConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(8);
		buf.byteLength;
	`)
	if result.ToNumber() != 8 {
		t.Errorf("expected byteLength 8, got %v", result.ToNumber())
	}
}

func TestArrayBufferSlice(t *testing.T) {
	vm := js.NewVM()
	vm.Run(`
		var buf = new ArrayBuffer(8);
		var sliced = buf.slice(2, 6);
		console.log(sliced.byteLength);
	`)
	logs := vm.ConsoleLogs()
	if len(logs) != 1 || logs[0] != "4" {
		t.Errorf("expected ['4'], got %v", logs)
	}
}

// --- DataView tests ---

func TestDataViewGetInt8(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(4);
		var dv = new DataView(buf);
		dv.setInt8(0, -42);
		dv.getInt8(0);
	`)
	if result.ToNumber() != -42 {
		t.Errorf("expected -42, got %v", result.ToNumber())
	}
}

func TestDataViewGetUint8(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(4);
		var dv = new DataView(buf);
		dv.setUint8(0, 255);
		dv.getUint8(0);
	`)
	if result.ToNumber() != 255 {
		t.Errorf("expected 255, got %v", result.ToNumber())
	}
}

func TestDataViewGetInt16BE(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(4);
		var dv = new DataView(buf);
		dv.setInt16(0, -300, false); // big-endian
		dv.getInt16(0, false);
	`)
	if result.ToNumber() != -300 {
		t.Errorf("expected -300, got %v", result.ToNumber())
	}
}

func TestDataViewGetFloat32LE(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(4);
		var dv = new DataView(buf);
		dv.setFloat32(0, 3.14, true); // little-endian
		dv.getFloat32(0, true);
	`)
	got := result.ToNumber()
	if math.Abs(got-3.14) > 0.001 {
		t.Errorf("expected ~3.14, got %v", got)
	}
}

func TestDataViewGetFloat64(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(8);
		var dv = new DataView(buf);
		dv.setFloat64(0, 2.718281828, true);
		dv.getFloat64(0, true);
	`)
	got := result.ToNumber()
	if math.Abs(got-2.718281828) > 0.0000001 {
		t.Errorf("expected ~2.718281828, got %v", got)
	}
}

// --- TypedArray tests ---

func TestInt8ArrayConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ta = new Int8Array(3);
		ta[0] = 10;
		ta[1] = -20;
		ta[2] = 127;
		ta[0] + ta[1] + ta[2];
	`)
	if result.ToNumber() != 117 {
		t.Errorf("expected 117, got %v", result.ToNumber())
	}
}

func TestUint8ArrayIndexedAccess(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ta = new Uint8Array(4);
		ta[0] = 0;
		ta[1] = 100;
		ta[2] = 200;
		ta[3] = 255;
		ta.length;
	`)
	if result.ToNumber() != 4 {
		t.Errorf("expected length 4, got %v", result.ToNumber())
	}
}

func TestUint16Array(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ta = new Uint16Array(3);
		ta[0] = 1;
		ta[1] = 2;
		ta[2] = 3;
		ta[0] + ta[1] + ta[2];
	`)
	if result.ToNumber() != 6 {
		t.Errorf("expected 6, got %v", result.ToNumber())
	}
}

func TestInt32Array(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ta = new Int32Array(2);
		ta[0] = -2147483648;
		ta[1] = 2147483647;
		ta[0] + ta[1];
	`)
	if result.ToNumber() != -1 {
		t.Errorf("expected -1, got %v", result.ToNumber())
	}
}

func TestFloat64Array(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ta = new Float64Array(2);
		ta[0] = 3.14159;
		ta[1] = 2.71828;
		ta[0] + ta[1];
	`)
	got := result.ToNumber()
	if math.Abs(got-5.85987) > 0.0001 {
		t.Errorf("expected ~5.85987, got %v", got)
	}
}

func TestUint8ClampedArray(t *testing.T) {
	vm := js.NewVM()
	vm.Run(`
		var ta = new Uint8ClampedArray(4);
		ta[0] = -10;
		ta[1] = 300;
		ta[2] = 128.5;
		ta[3] = 64.5;
		console.log(ta[0], ta[1], ta[2], ta[3]);
	`)
	logs := vm.ConsoleLogs()
	if len(logs) != 1 || logs[0] != "0 255 128 64" {
		t.Errorf("expected '0 255 128 64', got %v", logs)
	}
}

func TestTypedArrayFromBuffer(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(4);
		var ta = new Uint8Array(buf);
		ta[0] = 42;
		ta[1] = 43;
		ta[2] = 44;
		ta[3] = 45;
		ta[0] + ta[1] + ta[2] + ta[3];
	`)
	if result.ToNumber() != 174 {
		t.Errorf("expected 174, got %v", result.ToNumber())
	}
}

func TestTypedArrayByteLength(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ta = new Int16Array(5); // 5 * 2 = 10 bytes
		ta.byteLength;
	`)
	if result.ToNumber() != 10 {
		t.Errorf("expected byteLength 10, got %v", result.ToNumber())
	}
}

func TestDataViewUint32(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(4);
		var dv = new DataView(buf);
		dv.setUint32(0, 0xDEADBEEF, true);
		dv.getUint32(0, true);
	`)
	if result.ToNumber() != 0xDEADBEEF {
		t.Errorf("expected 0xDEADBEEF (%v), got %v", float64(0xDEADBEEF), result.ToNumber())
	}
}

func TestFloat32Array(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ta = new Float32Array(3);
		ta[0] = 1.5;
		ta[1] = 2.5;
		ta[2] = 3.5;
		ta[0] + ta[1] + ta[2];
	`)
	got := result.ToNumber()
	if math.Abs(got-7.5) > 0.01 {
		t.Errorf("expected ~7.5, got %v", got)
	}
}
