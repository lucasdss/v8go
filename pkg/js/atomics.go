// atomics.go — SharedArrayBuffer and Atomics compatibility stubs.
// V8Go runs single-threaded per VM; operations are non-atomic but correct.

package js

// SharedArrayBufferPrototype holds the SharedArrayBuffer.prototype object.
var SharedArrayBufferPrototype *JSObject

// registerAtomics registers SharedArrayBuffer and Atomics on the global scope.
func (vm *VM) registerAtomics() {
	vm.registerSharedArrayBuffer()
	vm.registerAtomicsObj()
}

// detectKind maps a TypedArray constructor name to its typedArrayKind.
func detectKind(name string) typedArrayKind {
	switch name {
	case "Int8Array":
		return taInt8
	case "Uint8Array":
		return taUint8
	case "Uint8ClampedArray":
		return taUint8Clamped
	case "Int16Array":
		return taInt16
	case "Uint16Array":
		return taUint16
	case "Int32Array":
		return taInt32
	case "Uint32Array":
		return taUint32
	case "Float32Array":
		return taFloat32
	case "Float64Array":
		return taFloat64
	default:
		return taInt8 // fallback
	}
}

// ---------------------------------------------------------------------------
// SharedArrayBuffer
// ---------------------------------------------------------------------------

func (vm *VM) registerSharedArrayBuffer() {
	SharedArrayBufferPrototype = NewJSObject()
	SharedArrayBufferPrototype.ConstructorName = "SharedArrayBuffer"
	SharedArrayBufferPrototype.Set("byteLength", NewNumber(0))

	// SharedArrayBuffer.prototype.slice(begin, end)
	SharedArrayBufferPrototype.Set("slice", NewObject(builtinFunc("SharedArrayBuffer.slice", func(this *JSObject, args []JSValue) JSValue {
		if this.typedArray == nil || this.typedArray.ByteData == nil {
			return NewObject(newSharedArrayBuffer(0))
		}
		totalLen := len(this.typedArray.ByteData)
		begin := 0
		end := totalLen
		if len(args) > 0 {
			begin = int(clampToInt(args[0].ToNumber(), totalLen))
		}
		if len(args) > 1 && !args[1].IsUndefined() {
			end = int(clampToInt(args[1].ToNumber(), totalLen))
		}
		if begin < 0 {
			begin = totalLen + begin
		}
		if end < 0 {
			end = totalLen + end
		}
		if begin < 0 {
			begin = 0
		}
		if end > totalLen {
			end = totalLen
		}
		if begin > end {
			begin = end
		}
		newLen := end - begin
		sab := newSharedArrayBuffer(newLen)
		copy(sab.ensureTypedArray().ByteData, this.typedArray.ByteData[begin:end])
		return NewObject(sab)
	})))

	sabCtor := NewJSObject()
	sabCtor.ConstructorName = "Function"
	sabCtor.Set("prototype", NewObject(SharedArrayBufferPrototype))
	sabCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		byteLength := 0
		if len(args) > 0 {
			byteLength = int(args[0].ToNumber())
		}
		if byteLength < 0 {
			byteLength = 0
		}
		sab := newSharedArrayBuffer(byteLength)
		// Per spec: if called without new, returns a new instance anyway (same as ArrayBuffer).
		return NewObject(sab)
	}

	vm.globals.M["SharedArrayBuffer"] = NewObject(sabCtor)
}

func newSharedArrayBuffer(byteLength int) *JSObject {
	sab := NewJSObject()
	sab.ConstructorName = "SharedArrayBuffer"
	sab.Prototype = SharedArrayBufferPrototype
	sab.ensureTypedArray().ByteData = make([]byte, byteLength)
	sab.Set("byteLength", NewNumber(float64(byteLength)))
	return sab
}

// ---------------------------------------------------------------------------
// Atomics
// ---------------------------------------------------------------------------

func (vm *VM) registerAtomicsObj() {
	obj := NewJSObject()
	obj.ConstructorName = "Atomics"

	obj.Set("add", vm.createAtomicsOp("add", func(old, val float64) float64 {
		return old + val
	}))
	obj.Set("sub", vm.createAtomicsOp("sub", func(old, val float64) float64 {
		return old - val
	}))
	obj.Set("and", vm.createAtomicsOp("and", func(old, val float64) float64 {
		return float64(int32(old) & int32(val))
	}))
	obj.Set("or", vm.createAtomicsOp("or", func(old, val float64) float64 {
		return float64(int32(old) | int32(val))
	}))
	obj.Set("xor", vm.createAtomicsOp("xor", func(old, val float64) float64 {
		return float64(int32(old) ^ int32(val))
	}))
	obj.Set("exchange", vm.createAtomicsOp("exchange", func(old, val float64) float64 {
		return val
	}))

	// Atomics.load(typedArray, index)
	obj.Set("load", vm.createBuiltinFunction("Atomics.load", func(this *JSObject, args []JSValue) JSValue {
		ta, idx, kind, ok := validateAtomicsArgs(args, 2)
		if !ok {
			return NewNumber(0)
		}
		byteSize := typedArrayByteSize(kind)
		if idx*byteSize+byteSize > len(ta.typedArray.ByteData) {
			return NewNumber(0)
		}
		return typedArrayGet(ta.typedArray.ByteData, idx, byteSize, kind)
	}))

	// Atomics.store(typedArray, index, value)
	obj.Set("store", vm.createBuiltinFunction("Atomics.store", func(this *JSObject, args []JSValue) JSValue {
		ta, idx, kind, ok := validateAtomicsArgs(args, 3)
		if !ok {
			return NewNumber(0)
		}
		byteSize := typedArrayByteSize(kind)
		if idx*byteSize+byteSize > len(ta.typedArray.ByteData) {
			return NewNumber(0)
		}
		val := args[2].ToNumber()
		typedArraySet(ta.typedArray.ByteData, idx, byteSize, kind, val)
		return NewNumber(val)
	}))

	// Atomics.compareExchange(typedArray, index, expected, replacement)
	obj.Set("compareExchange", vm.createBuiltinFunction("Atomics.compareExchange", func(this *JSObject, args []JSValue) JSValue {
		ta, idx, kind, ok := validateAtomicsArgs(args, 4)
		if !ok {
			return NewNumber(0)
		}
		byteSize := typedArrayByteSize(kind)
		if idx*byteSize+byteSize > len(ta.typedArray.ByteData) {
			return NewNumber(0)
		}
		expected := args[2].ToNumber()
		replacement := args[3].ToNumber()
		old := typedArrayGet(ta.typedArray.ByteData, idx, byteSize, kind).ToNumber()
		if old == expected {
			typedArraySet(ta.typedArray.ByteData, idx, byteSize, kind, replacement)
		}
		return NewNumber(old)
	}))

	// Atomics.isLockFree(size)
	obj.Set("isLockFree", vm.createBuiltinFunction("Atomics.isLockFree", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		size := int(args[0].ToNumber())
		// Per spec: 1, 2, and 4 byte atomics are lock-free on all known architectures.
		return NewBoolean(size == 1 || size == 2 || size == 4)
	}))

	// Atomics.wait — stub: returns immediately in single-threaded context.
	obj.Set("wait", vm.createBuiltinFunction("Atomics.wait", func(this *JSObject, args []JSValue) JSValue {
		return NewString("not-equal")
	}))

	// Atomics.notify — stub: no waiters in single-threaded context.
	obj.Set("notify", vm.createBuiltinFunction("Atomics.notify", func(this *JSObject, args []JSValue) JSValue {
		return NewNumber(0)
	}))

	vm.globals.M["Atomics"] = NewObject(obj)
}

// createAtomicsOp creates an Atomics read-modify-write operation.
func (vm *VM) createAtomicsOp(name string, op func(old, val float64) float64) JSValue {
	return vm.createBuiltinFunction("Atomics."+name, func(this *JSObject, args []JSValue) JSValue {
		ta, idx, kind, ok := validateAtomicsArgs(args, 3)
		if !ok {
			return NewNumber(0)
		}
		byteSize := typedArrayByteSize(kind)
		if idx*byteSize+byteSize > len(ta.typedArray.ByteData) {
			return NewNumber(0)
		}
		val := args[2].ToNumber()
		old := typedArrayGet(ta.typedArray.ByteData, idx, byteSize, kind).ToNumber()
		newVal := op(old, val)
		typedArraySet(ta.typedArray.ByteData, idx, byteSize, kind, newVal)
		return NewNumber(old)
	})
}

// validateAtomicsArgs validates common Atomics method arguments.
// Returns the typed array object, index, kind, and whether args are valid.
func validateAtomicsArgs(args []JSValue, minArgs int) (*JSObject, int, typedArrayKind, bool) {
	if len(args) < minArgs {
		return nil, 0, taInt8, false
	}
	if !args[0].IsObject() || args[0].ObjVal == nil {
		return nil, 0, taInt8, false
	}
	ta := args[0].ObjVal
	if ta.typedArray == nil || ta.typedArray.ByteData == nil {
		return nil, 0, taInt8, false
	}
	idx := int(args[1].ToNumber())
	kind := detectKind(ta.ConstructorName)
	return ta, idx, kind, true
}
