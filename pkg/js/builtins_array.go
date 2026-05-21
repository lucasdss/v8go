package js

import (
	"math"
	"strings"
)

// maxArrayLen caps array lengths and indices to prevent integer overflow.
// 1<<28 allows up to ~256MB arrays, preventing NaN/Inf → int overflow.
const maxArrayLen = 1 << 28

// safeIndex bounds-checks a JS numeric value used as an array index.
// Returns (index, ok). ok=false when v is NaN, Inf, negative, or > maxLen.
func safeIndex(v float64, maxLen int) (int, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	idx := int(v)
	if idx > maxLen {
		return 0, false
	}
	return idx, true
}


// _arrayFlat recursively flattens an array-like object up to the given depth.
func _arrayFlat(arr *JSObject, depth int) *JSObject {
	result := NewJSObject()
	result.ConstructorName = "Array"
	if ArrayPrototype != nil {
		result.Prototype = ArrayPrototype
	}
	length, _ := safeIndex(arr.Get("length").ToNumber(), maxArrayLen)
	outIdx := 0
	for i := 0; i < length; i++ {
		elem := arr.Get(intKey(i))
		if depth > 0 && elem.IsObject() && elem.ObjVal != nil && elem.ObjVal.ConstructorName == "Array" {
			sub := _arrayFlat(elem.ObjVal, depth-1)
			subLen, _ := safeIndex(sub.Get("length").ToNumber(), maxArrayLen)
			for j := 0; j < subLen; j++ {
				result.Set(intKey(outIdx), sub.Get(intKey(j)))
				outIdx++
			}
		} else {
			result.Set(intKey(outIdx), elem)
			outIdx++
		}
	}
	result.Set("length", NewNumber(float64(outIdx)))
	return result
}

func (vm *VM) registerArray() {
	arrayCtor := NewJSObject()
	arrayCtor.ConstructorName = "Function"
	arrayCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		arr := NewJSObject()
		arr.ConstructorName = "Array"
		if ArrayPrototype != nil {
			arr.Prototype = ArrayPrototype
		}
		arr.Set("length", NewNumber(float64(len(args))))
		for i, arg := range args {
			arr.Set(intKey(i), arg)
		}
		return NewObject(arr)
	}

	arrayProto := NewJSObject()
	arrayProto.ConstructorName = "Array"

	arrayProto.Set("push", vm.createBuiltinWithFallback("Array.push", func(this *JSObject, args []JSValue) JSValue {
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		for i, arg := range args {
			this.Set(intKey(length+i), arg)
		}
		newLen := length + len(args)
		this.Set("length", NewNumber(float64(newLen)))
		return NewNumber(float64(newLen))
	}))

	arrayProto.Set("pop", vm.createBuiltinWithFallback("Array.pop", func(this *JSObject, args []JSValue) JSValue {
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		if length == 0 {
			this.Set("length", NewNumber(0))
			return Undefined
		}
		lastIdx := intKey(length-1)
		val := this.Get(lastIdx)
		this.Delete(lastIdx)
		this.Set("length", NewNumber(float64(length-1)))
		return val
	}))

	arrayProto.Set("map", vm.createBuiltinFunction("Array.map", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		result := NewJSObject()
		result.ConstructorName = "Array"
		if ArrayPrototype != nil {
			result.Prototype = ArrayPrototype
		}
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			mapped := callback.Call(nil, []JSValue{elem, NewNumber(float64(i)), NewObject(this)})
			result.Set(intKey(i), mapped)
		}
		result.Set("length", NewNumber(float64(length)))
		return NewObject(result)
	}))

	arrayProto.Set("filter", vm.createBuiltinFunction("Array.filter", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		result := NewJSObject()
		result.ConstructorName = "Array"
		if ArrayPrototype != nil {
			result.Prototype = ArrayPrototype
		}
		outIdx := 0
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			passed := callback.Call(nil, []JSValue{elem, NewNumber(float64(i)), NewObject(this)})
			if passed.IsTruthy() {
				result.Set(intKey(outIdx), elem)
				outIdx++
			}
		}
		result.Set("length", NewNumber(float64(outIdx)))
		return NewObject(result)
	}))

	arrayProto.Set("reduce", vm.createBuiltinFunction("Array.reduce", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		if length == 0 && len(args) < 2 {
			return Undefined
		}
		startIdx := 0
		accumulator := Undefined
		if len(args) >= 2 {
			accumulator = args[1]
		} else {
			accumulator = this.Get("0")
			startIdx = 1
		}
		for i := startIdx; i < length; i++ {
			elem := this.Get(intKey(i))
			accumulator = callback.Call(nil, []JSValue{accumulator, elem, NewNumber(float64(i)), NewObject(this)})
		}
		return accumulator
	}))

	arrayProto.Set("forEach", vm.createBuiltinFunction("Array.forEach", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			callback.Call(nil, []JSValue{elem, NewNumber(float64(i)), NewObject(this)})
		}
		return Undefined
	}))

	arrayProto.Set("indexOf", vm.createBuiltinWithFallback("Array.indexOf", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(-1)
		}
		search := args[0]
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		startFrom, ok := safeIndex(0, length)
		if len(args) > 1 {
			v := args[1].ToNumber()
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return NewNumber(-1)
			}
			startFrom = int(v)
			ok = true
		}
		if !ok {
			return NewNumber(-1)
		}
		for i := startFrom; i < length; i++ {
			if this.Get(intKey(i)).StrictEquals(search) {
				return NewNumber(float64(i))
			}
		}
		return NewNumber(-1)
	}))

	arrayProto.Set("join", vm.createBuiltinWithFallback("Array.join", func(this *JSObject, args []JSValue) JSValue {
		sep := ","
		if len(args) > 0 {
			sep = args[0].ToString()
		}
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		parts := make([]string, length)
		for i := 0; i < length; i++ {
			val := this.Get(intKey(i))
			if val.IsUndefined() || val.IsNull() {
				parts[i] = ""
			} else {
				parts[i] = val.ToString()
			}
		}
		return NewString(strings.Join(parts, sep))
	}))

	arrayProto.Set("slice", vm.createBuiltinFunction("Array.slice", func(this *JSObject, args []JSValue) JSValue {
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		start := 0
		end := length
		if len(args) > 0 {
			v := args[0].ToNumber()
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				start = int(v)
			}
		}
		if len(args) > 1 {
			v := args[1].ToNumber()
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				end = int(v)
			}
		}
		if start < 0 {
			start = length + start
		}
		if end < 0 {
			end = length + end
		}
		if start < 0 {
			start = 0
		}
		if end > length {
			end = length
		}
		result := NewJSObject()
		result.ConstructorName = "Array"
		if ArrayPrototype != nil {
			result.Prototype = ArrayPrototype
		}
		outIdx := 0
		for i := start; i < end; i++ {
			result.Set(intKey(outIdx), this.Get(intKey(i)))
			outIdx++
		}
		result.Set("length", NewNumber(float64(outIdx)))
		return NewObject(result)
	}))

	arrayProto.Set("splice", vm.createBuiltinFunction("Array.splice", func(this *JSObject, args []JSValue) JSValue {
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		start := 0
		if len(args) > 0 {
			v := args[0].ToNumber()
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				start = int(v)
			}
		}
		if start < 0 {
			start = length + start
		}
		if start < 0 {
			start = 0
		}
		deleteCount := length - start
		if len(args) > 1 {
			v := args[1].ToNumber()
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				deleteCount = int(v)
			}
		}
		if deleteCount > length-start {
			deleteCount = length - start
		}
		// Collect deleted elements.
		deleted := NewJSObject()
		deleted.ConstructorName = "Array"
		if ArrayPrototype != nil {
			deleted.Prototype = ArrayPrototype
		}
		for i := 0; i < deleteCount; i++ {
			deleted.Set(intKey(i), this.Get(intKey(start+i)))
		}
		deleted.Set("length", NewNumber(float64(deleteCount)))
		// Shift remaining elements.
		insertItems := args[2:]
		shift := len(insertItems) - deleteCount
		if shift > 0 {
			for i := length - 1; i >= start+deleteCount; i-- {
				this.Set(intKey(i+shift), this.Get(intKey(i)))
			}
		} else if shift < 0 {
			for i := start + deleteCount; i < length; i++ {
				this.Set(intKey(i+shift), this.Get(intKey(i)))
			}
		}
		// Insert new items.
		for i, item := range insertItems {
			this.Set(intKey(start+i), item)
		}
		newLength := length + shift
		this.Set("length", NewNumber(float64(newLength)))
		return NewObject(deleted)
	}))

	arrayProto.Set("concat", vm.createBuiltinFunction("Array.concat", func(this *JSObject, args []JSValue) JSValue {
		result := NewJSObject()
		result.ConstructorName = "Array"
		if ArrayPrototype != nil {
			result.Prototype = ArrayPrototype
		}
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		outIdx := 0
		for i := 0; i < length; i++ {
			result.Set(intKey(outIdx), this.Get(intKey(i)))
			outIdx++
		}
		for _, arg := range args {
			if arg.IsObject() && arg.ObjVal != nil {
				argLen, _ := safeIndex(arg.ObjVal.Get("length").ToNumber(), maxArrayLen)
				for i := 0; i < argLen; i++ {
					result.Set(intKey(outIdx), arg.ObjVal.Get(intKey(i)))
					outIdx++
				}
			} else {
				result.Set(intKey(outIdx), arg)
				outIdx++
			}
		}
		result.Set("length", NewNumber(float64(outIdx)))
		return NewObject(result)
	}))

	arrayProto.Set("find", vm.createBuiltinFunction("Array.find", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			if callback.Call(nil, []JSValue{elem, NewNumber(float64(i)), NewObject(this)}).IsTruthy() {
				return elem
			}
		}
		return Undefined
	}))

	arrayProto.Set("some", vm.createBuiltinFunction("Array.some", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return False
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			if callback.Call(nil, []JSValue{elem, NewNumber(float64(i)), NewObject(this)}).IsTruthy() {
				return True
			}
		}
		return False
	}))

	arrayProto.Set("every", vm.createBuiltinFunction("Array.every", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return True
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			if !callback.Call(nil, []JSValue{elem, NewNumber(float64(i)), NewObject(this)}).IsTruthy() {
				return False
			}
		}
		return True
	}))

	arrayProto.Set("findIndex", vm.createBuiltinFunction("Array.findIndex", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return NewNumber(-1)
		}
		callback := args[0].ObjVal
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			if callback.Call(nil, []JSValue{elem, NewNumber(float64(i)), NewObject(this)}).IsTruthy() {
				return NewNumber(float64(i))
			}
		}
		return NewNumber(-1)
	}))

	arrayProto.Set("fill", vm.createBuiltinFunction("Array.fill", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewObject(this)
		}
		value := args[0]
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		start := 0
		end := length
		if len(args) > 1 {
			v := args[1].ToNumber()
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				start = int(v)
			}
			if start < 0 {
				start = length + start
			}
			if start < 0 {
				start = 0
			}
		}
		if len(args) > 2 {
			v := args[2].ToNumber()
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				end = int(v)
			}
			if end < 0 {
				end = length + end
			}
		}
		if end > length {
			end = length
		}
		for i := start; i < end; i++ {
			this.Set(intKey(i), value)
		}
		return NewObject(this)
	}))

	arrayProto.Set("flat", vm.createBuiltinFunction("Array.flat", func(this *JSObject, args []JSValue) JSValue {
		depth := 1.0
		if len(args) > 0 {
			v := args[0].ToNumber()
			if math.IsNaN(v) || math.IsInf(v, -1) {
				depth = 0
			} else if math.IsInf(v, 1) {
				depth = float64(maxArrayLen) // effectively infinite
			} else {
				depth = v
			}
		}
		flattened := _arrayFlat(this, int(depth))
		return NewObject(flattened)
	}))

	arrayProto.Set("flatMap", vm.createBuiltinFunction("Array.flatMap", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		thisArg := (*JSObject)(nil)
		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil {
			thisArg = args[1].ObjVal
		}
		length, _ := safeIndex(this.Get("length").ToNumber(), maxArrayLen)
		mapped := NewJSObject()
		mapped.ConstructorName = "Array"
		if ArrayPrototype != nil {
			mapped.Prototype = ArrayPrototype
		}
		for i := 0; i < length; i++ {
			elem := this.Get(intKey(i))
			mapped.Set(intKey(i), callback.Call(thisArg, []JSValue{elem, NewNumber(float64(i)), NewObject(this)}))
		}
		mapped.Set("length", NewNumber(float64(length)))
		flattened := _arrayFlat(mapped, 1)
		return NewObject(flattened)
	}))

	// Array.prototype.at(index) — ES2022
	arrayProto.Set("at", vm.createBuiltinFunction("Array.at", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Undefined
		}
		v := args[0].ToNumber()
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Undefined
		}
		n := int(v)
		lengthVal := this.Get("length")
		length := 0
		if lengthVal.Tag == TagNumber {
			length = int(lengthVal.NumVal)
		}
		if length == 0 {
			return Undefined
		}
		if n < 0 {
			n = length + n
		}
		if n < 0 || n >= length {
			return Undefined
		}
		idxStr := intKey(n)
		return this.Get(idxStr)
	}))

	// Static methods on Array constructor.
	arrayCtor.Set("from", vm.createBuiltinFunction("Array.from", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			result := NewJSObject()
			result.ConstructorName = "Array"
			result.Set("length", NewNumber(0))
			return NewObject(result)
		}
		source := args[0]
		var mapFn func(*JSObject, []JSValue) JSValue
		var thisArg *JSObject
		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil && args[1].ObjVal.isCallable() {
			mapFn = args[1].ObjVal.Call
			if len(args) > 2 && args[2].IsObject() && args[2].ObjVal != nil {
				thisArg = args[2].ObjVal
			}
		}
		result := NewJSObject()
		result.ConstructorName = "Array"
		if ArrayPrototype != nil {
			result.Prototype = ArrayPrototype
		}
		if source.IsObject() && source.ObjVal != nil {
			length, _ := safeIndex(source.ObjVal.Get("length").ToNumber(), maxArrayLen)
			for i := 0; i < length; i++ {
				elem := source.ObjVal.Get(intKey(i))
				if mapFn != nil {
					elem = mapFn(thisArg, []JSValue{elem, NewNumber(float64(i))})
				}
				result.Set(intKey(i), elem)
			}
			result.Set("length", NewNumber(float64(length)))
		} else if source.IsString() {
			s := source.String()
			for i, ch := range s {
				result.Set(intKey(i), NewString(string(ch)))
			}
			result.Set("length", NewNumber(float64(len(s))))
		}
		return NewObject(result)
	}))

	arrayCtor.Set("of", vm.createBuiltinFunction("Array.of", func(this *JSObject, args []JSValue) JSValue {
		result := NewJSObject()
		result.ConstructorName = "Array"
		if ArrayPrototype != nil {
			result.Prototype = ArrayPrototype
		}
		for i, arg := range args {
			result.Set(intKey(i), arg)
		}
		result.Set("length", NewNumber(float64(len(args))))
		return NewObject(result)
	}))

	// Array.fromAsync — ES2024 (Stage 4)
// Returns a Promise that resolves to a new Array from an async iterable,
// sync iterable, or array-like object. If a mapFn is provided, each element
// is passed through it before insertion.
arrayCtor.Set("fromAsync", vm.createBuiltinFunction("Array.fromAsync", func(this *JSObject, args []JSValue) JSValue {
// Array.fromAsync returns a Promise that resolves to a new Array.
return vm.NewPromise(func(resolve, reject func(JSValue)) {
var items []JSValue
if len(args) > 0 {
source := args[0]
var mapFn func(*JSObject, []JSValue) JSValue
var thisArg *JSObject
if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil && args[1].ObjVal.isCallable() {
mapFn = args[1].ObjVal.Call
if len(args) > 2 && args[2].IsObject() && args[2].ObjVal != nil {
thisArg = args[2].ObjVal
}
}
if source.IsObject() && source.ObjVal != nil {
obj := source.ObjVal
// Try to call @@asyncIterator method if present.
asyncIterFn := obj.Get(AsyncIteratorSymbol.StrVal)
if asyncIterFn.IsObject() && asyncIterFn.ObjVal != nil && asyncIterFn.ObjVal.isCallable() {
itResult := asyncIterFn.ObjVal.Call(obj, nil)
if itResult.IsObject() && itResult.ObjVal != nil {
iter := itResult.ObjVal
for {
nextFn := iter.Get("next")
if nextFn.IsObject() && nextFn.ObjVal != nil && nextFn.ObjVal.isCallable() {
nv := nextFn.ObjVal.Call(iter, nil)
if nv.IsObject() && nv.ObjVal != nil {
if nv.ObjVal.Get("done").IsTruthy() {
break
}
elem := nv.ObjVal.Get("value")
if mapFn != nil {
elem = mapFn(thisArg, []JSValue{elem, NewNumber(float64(len(items)))})
}
items = append(items, elem)
}
} else {
break
}
}
}
} else {
// Array-like: iterate 0..length-1.
lengthVal := obj.Get("length")
if lengthVal.Tag == TagNumber {
length := int(lengthVal.NumVal)
if length < 0 {
length = 0
}
if length > maxArrayLen {
length = maxArrayLen
}
for i := 0; i < length; i++ {
elem := obj.Get(intKey(i))
if mapFn != nil {
elem = mapFn(thisArg, []JSValue{elem, NewNumber(float64(i))})
}
items = append(items, elem)
}
}
}
}
}
// Build result array.
result := NewJSObject()
result.ConstructorName = "Array"
if ArrayPrototype != nil {
result.Prototype = ArrayPrototype
}
for i, item := range items {
result.Set(intKey(i), item)
}
result.Set("length", NewNumber(float64(len(items))))
resolve(NewObject(result))
})
}))

arrayCtor.Set("isArray", vm.createBuiltinFunction("Array.isArray", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		arg := args[0]
		if arg.IsObject() && arg.ObjVal != nil && arg.ObjVal.ConstructorName == "Array" {
			return True
		}
		return False
	}))

	// Set prototype chain: Array.prototype → Object.prototype
	arrayProto.Prototype = ObjectPrototype

	// Store prototype for use by array instances.
	ArrayPrototype = arrayProto

	// Set prototype on Array constructor for instanceof checks.
	arrayCtor.Set("prototype", NewObject(arrayProto))

	vm.globals.M["Array"] = NewObject(arrayCtor)
}
