package js

import (
"fmt"
"strconv"
"strings"
)


func (vm *VM) registerMap() {
	mapCtor := NewJSObject()
	mapCtor.ConstructorName = "Function"
	mapCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		m := NewJSObject()
		m.ConstructorName = "Map"
		// Backing Go map for key-value storage.
		m.Set("__map_data__", NewObject(NewJSObject()))
		m.Set("size", NewNumber(0))
		m.Prototype = mapProto()
		return NewObject(m)
	}

	vm.globals.M["Map"] = NewObject(mapCtor)
}

func mapProto() *JSObject {
	proto := NewJSObject()
	proto.ConstructorName = "Map"

	proto.Set("set", NewObject(builtinFunc("Map.set", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return NewObject(this)
		}
		key := keyString(args[0])
		data := this.Get("__map_data__")
		if data.IsObject() && data.ObjVal != nil {
			data.ObjVal.Set(key, args[1])
		}
		curSize := this.Get("size").ToNumber()
		this.Set("size", NewNumber(curSize+1))
		return NewObject(this)
	})))

	proto.Set("get", NewObject(builtinFunc("Map.get", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Undefined
		}
		key := keyString(args[0])
		data := this.Get("__map_data__")
		if data.IsObject() && data.ObjVal != nil {
			return data.ObjVal.Get(key)
		}
		return Undefined
	})))

	proto.Set("has", NewObject(builtinFunc("Map.has", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		key := keyString(args[0])
		data := this.Get("__map_data__")
		if data.IsObject() && data.ObjVal != nil {
			return NewBoolean(!data.ObjVal.Get(key).IsUndefined())
		}
		return False
	})))

	proto.Set("delete", NewObject(builtinFunc("Map.delete", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		key := keyString(args[0])
		data := this.Get("__map_data__")
		had := false
		if data.IsObject() && data.ObjVal != nil {
			had = !data.ObjVal.Get(key).IsUndefined()
			if had {
				data.ObjVal.Delete(key)
				curSize := this.Get("size").ToNumber()
				this.Set("size", NewNumber(curSize-1))
			}
		}
		return NewBoolean(had)
	})))

	proto.Set("clear", NewObject(builtinFunc("Map.clear", func(this *JSObject, args []JSValue) JSValue {
		this.Set("__map_data__", NewObject(NewJSObject()))
		this.Set("size", NewNumber(0))
		return Undefined
	})))

	proto.Set("forEach", NewObject(builtinFunc("Map.forEach", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		data := this.Get("__map_data__")
		if data.IsObject() && data.ObjVal != nil && !data.ObjVal.Shape.IsDictionary {
			for name := range data.ObjVal.Shape.Properties {
				val := data.ObjVal.Get(name)
				key := decodeKey(name)
				callback.Call(nil, []JSValue{val, key, NewObject(this)})
			}
		}
		return Undefined
	})))

	// Map.prototype.keys() — returns a Map iterator yielding keys.
	proto.Set("keys", NewObject(builtinFunc("Map.keys", func(this *JSObject, args []JSValue) JSValue {
		return NewObject(createMapIterator(this, "keys"))
	})))

	// Map.prototype.values() — returns a Map iterator yielding values.
	proto.Set("values", NewObject(builtinFunc("Map.values", func(this *JSObject, args []JSValue) JSValue {
		return NewObject(createMapIterator(this, "values"))
	})))

	// Map.prototype.entries() — returns a Map iterator yielding [key, value] pairs.
	proto.Set("entries", NewObject(builtinFunc("Map.entries", func(this *JSObject, args []JSValue) JSValue {
		return NewObject(createMapIterator(this, "entries"))
	})))

	return proto
}

func keyString(v JSValue) string {
	switch v.Tag {
	case TagString:
		return "__str_" + v.StrVal
	case TagNumber:
		return fmt.Sprintf("__num_%v", v.NumVal)
	case TagBoolean:
		if v.BoolVal {
			return "__bool_true"
		}
		return "__bool_false"
	case TagObject:
		if v.ObjVal != nil {
			return fmt.Sprintf("__obj_%p", v.ObjVal)
		}
		return "__obj_null"
	case TagNull:
		return "__null"
	case TagUndefined:
		return "__undef"
	case TagSymbol:
		return "__sym_" + v.SymVal
	}
	return "__unknown"
}

func decodeKey(s string) JSValue {
	if strings.HasPrefix(s, "__str_") {
		return NewString(strings.TrimPrefix(s, "__str_"))
	}
	if strings.HasPrefix(s, "__num_") {
		n, _ := strconv.ParseFloat(strings.TrimPrefix(s, "__num_"), 64)
		return NewNumber(n)
	}
	if s == "__bool_true" {
		return True
	}
	if s == "__bool_false" {
		return False
	}
	if s == "__null" {
		return Null
	}
	if s == "__undef" {
		return Undefined
	}
	return NewString(s)
}

// createMapIterator creates a Map iterator object that yields entries according to kind
// ("keys", "values", or "entries"). It snapshots the keys at creation time so that
// mutations to the Map during iteration do not affect the iterator.
func createMapIterator(mapObj *JSObject, kind string) *JSObject {
	iter := NewJSObject()
	iter.ConstructorName = "MapIterator"

	// Snapshot keys from the backing data object.
	var keyNames []string
	data := mapObj.Get("__map_data__")
	if data.IsObject() && data.ObjVal != nil {
		if !data.ObjVal.Shape.IsDictionary {
			for name := range data.ObjVal.Shape.Properties {
				keyNames = append(keyNames, name)
			}
		} else if data.ObjVal.Dictionary != nil {
			for name := range data.ObjVal.Dictionary {
				keyNames = append(keyNames, name)
			}
		}
	}

	// Store the snapshot as an Array of key strings.
	keysArr := NewJSObject()
	keysArr.ConstructorName = "Array"
	if ArrayPrototype != nil {
		keysArr.Prototype = ArrayPrototype
	}
	for i, k := range keyNames {
		keysArr.Set(intKey(i), NewString(k))
	}
	keysArr.Set("length", NewNumber(float64(len(keyNames))))

	iter.Set("__kind__", NewString(kind))
	iter.Set("__keys__", NewObject(keysArr))
	iter.Set("__index__", NewNumber(0))

	// Store a reference to the original Map for live value access.
	iter.Set("__map_ref__", NewObject(mapObj))

	iter.Set("next", NewObject(builtinFunc("MapIterator.next", func(this *JSObject, args []JSValue) JSValue {
		kind := this.Get("__kind__").ToString()
		idx := int(this.Get("__index__").ToNumber())
		keysRef := this.Get("__keys__")
		mapRef := this.Get("__map_ref__")

		if !keysRef.IsObject() || keysRef.ObjVal == nil {
			return createIteratorResultObject(Undefined, true)
		}
		length := int(keysRef.ObjVal.Get("length").ToNumber())

		// Skip keys that have been deleted from the Map since iterator creation.
		var keyStr string
		for idx < length {
			keyStr = keysRef.ObjVal.Get(intKey(idx)).ToString()
			if mapRef.IsObject() && mapRef.ObjVal != nil {
				data := mapRef.ObjVal.Get("__map_data__")
				if data.IsObject() && data.ObjVal != nil {
					if !data.ObjVal.Get(keyStr).IsUndefined() {
						break // key still exists in the Map
					}
				}
			}
			// Key was deleted, move to next.
			idx++
		}

		if idx >= length {
			this.Set("__index__", NewNumber(float64(length)))
			return createIteratorResultObject(Undefined, true)
		}

		this.Set("__index__", NewNumber(float64(idx+1)))

		// Get the live value from the Map for this key.
		val := Undefined
		if mapRef.IsObject() && mapRef.ObjVal != nil {
			data := mapRef.ObjVal.Get("__map_data__")
			if data.IsObject() && data.ObjVal != nil {
				val = data.ObjVal.Get(keyStr)
			}
		}
		decodedKey := decodeKey(keyStr)

		switch kind {
		case "keys":
			return createIteratorResultObject(decodedKey, false)
		case "values":
			return createIteratorResultObject(val, false)
		case "entries":
			entry := NewJSObject()
			entry.ConstructorName = "Array"
			if ArrayPrototype != nil {
				entry.Prototype = ArrayPrototype
			}
			entry.Set("0", decodedKey)
			entry.Set("1", val)
			entry.Set("length", NewNumber(2))
			return createIteratorResultObject(NewObject(entry), false)
		}
		return createIteratorResultObject(Undefined, true)
	})))

	return iter
}

// createIteratorResultObject returns { value: ..., done: bool } per the iterator protocol.
func createIteratorResultObject(value JSValue, done bool) JSValue {
	result := NewJSObject()
	result.ConstructorName = "Object"
	result.Set("value", value)
	result.Set("done", NewBoolean(done))
	return NewObject(result)
}

func (vm *VM) registerSet() {
	setProtoObj := setProto()
	setCtor := NewJSObject()
	setCtor.ConstructorName = "Function"
	setCtor.Set("prototype", NewObject(setProtoObj))
	setCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		s := NewJSObject()
		s.ConstructorName = "Set"
		s.Set("__set_data__", NewObject(NewJSObject()))
		s.Set("size", NewNumber(0))
		s.Prototype = setProtoObj
		// Optionally seed with values from an iterable argument.
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil {
			iterable := args[0].ObjVal
			// Case 1: Seed from another Set's internal data.
			if iterVal := iterable.Get("__set_data__"); iterVal.IsObject() && iterVal.ObjVal != nil {
				if !iterVal.ObjVal.Shape.IsDictionary {
					for name := range iterVal.ObjVal.Shape.Properties {
						s.Get("__set_data__").ObjVal.Set(name, True)
					}
				}
				s.Set("size", NewNumber(float64(iterVal.ObjVal.Shape.PropertyCount)))
			} else {
				// Case 2: Seed from an array-like iterable (has "length" property).
				lengthVal := iterable.Get("length")
				if lengthVal.Tag == TagNumber {
					count := int(lengthVal.ToNumber())
					for i := 0; i < count; i++ {
						val := iterable.Get(intKey(i))
						addToSetData(s, val)
					}
				}
			}
		}
		return NewObject(s)
	}

	vm.globals.M["Set"] = NewObject(setCtor)
}

// --- Helpers for Set methods (TC39 proposal) ---

// newEmptySet creates an empty Set object with the given prototype.
func newEmptySet(proto *JSObject) *JSObject {
	s := NewJSObject()
	s.ConstructorName = "Set"
	s.Prototype = proto
	s.Set("__set_data__", NewObject(NewJSObject()))
	s.Set("size", NewNumber(0))
	return s
}

// addToSetData adds a value to a Set's internal __set_data__ store and bumps size.
func addToSetData(setObj *JSObject, val JSValue) {
	key := keyString(val)
	data := setObj.Get("__set_data__")
	if data.IsObject() && data.ObjVal != nil {
		if data.ObjVal.Get(key).IsUndefined() {
			data.ObjVal.Set(key, True)
			curSize := setObj.Get("size").ToNumber()
			setObj.Set("size", NewNumber(curSize+1))
		}
	}
}

// forEachSetKey iterates over all keys stored in a Set's internal data object.
// The dataObj parameter should be the __set_data__ JSObject.
func forEachSetKey(dataObj *JSObject, fn func(val JSValue)) {
	if dataObj == nil {
		return
	}
	if !dataObj.Shape.IsDictionary {
		for name := range dataObj.Shape.Properties {
			fn(decodeKey(name))
		}
	} else if dataObj.Dictionary != nil {
		for name := range dataObj.Dictionary {
			fn(decodeKey(name))
		}
	}
}

// setupSetResult validates that this is a Set and returns (dataObj, newEmptySet).
// Returns (nil, emptySet) if this has no data, so the caller can return the empty set.
func setupSetResult(this *JSObject) (*JSObject, *JSObject) {
	result := newEmptySet(this.Prototype)
	thisData := this.Get("__set_data__")
	if !thisData.IsObject() || thisData.ObjVal == nil {
		return nil, result
	}
	return thisData.ObjVal, result
}

func setProto() *JSObject {
	proto := NewJSObject()
	proto.ConstructorName = "Set"

	proto.Set("add", NewObject(builtinFunc("Set.add", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewObject(this)
		}
		key := keyString(args[0])
		data := this.Get("__set_data__")
		if data.IsObject() && data.ObjVal != nil {
			if data.ObjVal.Get(key).IsUndefined() {
				data.ObjVal.Set(key, True)
				curSize := this.Get("size").ToNumber()
				this.Set("size", NewNumber(curSize+1))
			}
		}
		return NewObject(this)
	})))

	proto.Set("has", NewObject(builtinFunc("Set.has", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		key := keyString(args[0])
		data := this.Get("__set_data__")
		if data.IsObject() && data.ObjVal != nil {
			return NewBoolean(!data.ObjVal.Get(key).IsUndefined())
		}
		return False
	})))

	proto.Set("delete", NewObject(builtinFunc("Set.delete", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		key := keyString(args[0])
		data := this.Get("__set_data__")
		had := false
		if data.IsObject() && data.ObjVal != nil {
			had = !data.ObjVal.Get(key).IsUndefined()
			if had {
				data.ObjVal.Delete(key)
				curSize := this.Get("size").ToNumber()
				this.Set("size", NewNumber(curSize-1))
			}
		}
		return NewBoolean(had)
	})))

	proto.Set("clear", NewObject(builtinFunc("Set.clear", func(this *JSObject, args []JSValue) JSValue {
		this.Set("__set_data__", NewObject(NewJSObject()))
		this.Set("size", NewNumber(0))
		return Undefined
	})))

	proto.Set("values", NewObject(builtinFunc("Set.values", func(this *JSObject, args []JSValue) JSValue {
		return NewObject(createSetIterator(this, "values"))
	})))

	proto.Set("forEach", NewObject(builtinFunc("Set.forEach", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		callback := args[0].ObjVal
		data := this.Get("__set_data__")
		if data.IsObject() && data.ObjVal != nil && !data.ObjVal.Shape.IsDictionary {
			for name := range data.ObjVal.Shape.Properties {
				val := decodeKey(name)
				callback.Call(nil, []JSValue{val, val, NewObject(this)})
			}
		}
		return Undefined
	})))

	// --- Set Methods Proposal (TC39) ---

	// Set.prototype.intersection(other)
	proto.Set("intersection", NewObject(builtinFunc("Set.intersection", func(this *JSObject, args []JSValue) JSValue {
		thisData, result := setupSetResult(this)
		if thisData == nil {
			return NewObject(result)
		}
		if len(args) == 0 {
			return NewObject(result)
		}
		other := args[0]
		if !other.IsObject() || other.ObjVal == nil {
			return NewObject(result)
		}
		hasFnVal := other.ObjVal.Get("has")
		if !hasFnVal.IsObject() || hasFnVal.ObjVal == nil || !hasFnVal.ObjVal.isCallable() {
			return NewObject(result)
		}
		hasFn := hasFnVal.ObjVal
		otherObj := other.ObjVal

		forEachSetKey(thisData, func(val JSValue) {
			hasResult := hasFn.Call(otherObj, []JSValue{val})
			if hasResult.IsTruthy() {
				addToSetData(result, val)
			}
		})
		return NewObject(result)
	})))

	// Set.prototype.union(other)
	proto.Set("union", NewObject(builtinFunc("Set.union", func(this *JSObject, args []JSValue) JSValue {
		thisData, result := setupSetResult(this)
		if thisData == nil {
			return NewObject(result)
		}
		// Add all from this.
		forEachSetKey(thisData, func(val JSValue) {
			addToSetData(result, val)
		})

		if len(args) == 0 {
			return NewObject(result)
		}
		other := args[0]
		if !other.IsObject() || other.ObjVal == nil {
			return NewObject(result)
		}
		// Try to iterate other's data (native Set).
		otherDataVal := other.ObjVal.Get("__set_data__")
		if otherDataVal.IsObject() && otherDataVal.ObjVal != nil {
			forEachSetKey(otherDataVal.ObjVal, func(val JSValue) {
				addToSetData(result, val)
			})
		}
		return NewObject(result)
	})))

	// Set.prototype.difference(other)
	proto.Set("difference", NewObject(builtinFunc("Set.difference", func(this *JSObject, args []JSValue) JSValue {
		thisData, result := setupSetResult(this)
		if thisData == nil {
			return NewObject(result)
		}
		var hasFn *JSObject
		var otherObj *JSObject
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil {
			otherObj = args[0].ObjVal
			hf := otherObj.Get("has")
			if hf.IsObject() && hf.ObjVal != nil && hf.ObjVal.isCallable() {
				hasFn = hf.ObjVal
			}
		}

		forEachSetKey(thisData, func(val JSValue) {
			inOther := false
			if hasFn != nil {
				inOther = hasFn.Call(otherObj, []JSValue{val}).IsTruthy()
			}
			if !inOther {
				addToSetData(result, val)
			}
		})
		return NewObject(result)
	})))

	// Set.prototype.symmetricDifference(other)
	proto.Set("symmetricDifference", NewObject(builtinFunc("Set.symmetricDifference", func(this *JSObject, args []JSValue) JSValue {
		thisData, result := setupSetResult(this)
		if thisData == nil {
			return NewObject(result)
		}
		var hasFn *JSObject
		var otherObj *JSObject
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil {
			otherObj = args[0].ObjVal
			hf := otherObj.Get("has")
			if hf.IsObject() && hf.ObjVal != nil && hf.ObjVal.isCallable() {
				hasFn = hf.ObjVal
			}
		}

		// Elements in this but not in other.
		forEachSetKey(thisData, func(val JSValue) {
			inOther := false
			if hasFn != nil {
				inOther = hasFn.Call(otherObj, []JSValue{val}).IsTruthy()
			}
			if !inOther {
				addToSetData(result, val)
			}
		})

		// Elements in other but not in this (if other is a native Set).
		if otherObj != nil {
			otherDataVal := otherObj.Get("__set_data__")
			if otherDataVal.IsObject() && otherDataVal.ObjVal != nil {
				forEachSetKey(otherDataVal.ObjVal, func(val JSValue) {
					key := keyString(val)
					if thisData.Get(key).IsUndefined() {
						addToSetData(result, val)
					}
				})
			}
		}
		return NewObject(result)
	})))

	// Set.prototype.isSubsetOf(other)
	proto.Set("isSubsetOf", NewObject(builtinFunc("Set.isSubsetOf", func(this *JSObject, args []JSValue) JSValue {
		thisData := this.Get("__set_data__")
		if !thisData.IsObject() || thisData.ObjVal == nil {
			return True
		}
		if len(args) == 0 {
			return NewBoolean(thisData.ObjVal.Shape.PropertyCount == 0)
		}
		other := args[0]
		if !other.IsObject() || other.ObjVal == nil {
			return False
		}
		hasFnVal := other.ObjVal.Get("has")
		if !hasFnVal.IsObject() || hasFnVal.ObjVal == nil || !hasFnVal.ObjVal.isCallable() {
			return False
		}
		hasFn := hasFnVal.ObjVal
		otherObj := other.ObjVal

		allIn := true
		forEachSetKey(thisData.ObjVal, func(val JSValue) {
			if allIn && !hasFn.Call(otherObj, []JSValue{val}).IsTruthy() {
				allIn = false
			}
		})
		return NewBoolean(allIn)
	})))

	// Set.prototype.isSupersetOf(other)
	proto.Set("isSupersetOf", NewObject(builtinFunc("Set.isSupersetOf", func(this *JSObject, args []JSValue) JSValue {
		thisData := this.Get("__set_data__")
		if !thisData.IsObject() || thisData.ObjVal == nil {
			return False
		}
		dataObj := thisData.ObjVal
		if len(args) == 0 {
			return True
		}
		other := args[0]
		if !other.IsObject() || other.ObjVal == nil {
			return False
		}
		// Try to iterate other's data (native Set).
		otherDataVal := other.ObjVal.Get("__set_data__")
		if otherDataVal.IsObject() && otherDataVal.ObjVal != nil {
			allIn := true
			forEachSetKey(otherDataVal.ObjVal, func(val JSValue) {
				if allIn {
					key := keyString(val)
					if dataObj.Get(key).IsUndefined() {
						allIn = false
					}
				}
			})
			return NewBoolean(allIn)
		}
		// For non-native Set-likes, check size as a heuristic.
		thisSize := int(this.Get("size").ToNumber())
		otherSizeVal := other.ObjVal.Get("size")
		if otherSizeVal.IsNumber() && int(otherSizeVal.ToNumber()) > thisSize {
			return False
		}
		return True
	})))

	// Set.prototype.isDisjointFrom(other)
	proto.Set("isDisjointFrom", NewObject(builtinFunc("Set.isDisjointFrom", func(this *JSObject, args []JSValue) JSValue {
		thisData := this.Get("__set_data__")
		if !thisData.IsObject() || thisData.ObjVal == nil {
			return True
		}
		if len(args) == 0 {
			return True
		}
		other := args[0]
		if !other.IsObject() || other.ObjVal == nil {
			return True
		}
		hasFnVal := other.ObjVal.Get("has")
		if !hasFnVal.IsObject() || hasFnVal.ObjVal == nil || !hasFnVal.ObjVal.isCallable() {
			return True
		}
		hasFn := hasFnVal.ObjVal
		otherObj := other.ObjVal

		disjoint := true
		forEachSetKey(thisData.ObjVal, func(val JSValue) {
			if disjoint && hasFn.Call(otherObj, []JSValue{val}).IsTruthy() {
				disjoint = false
			}
		})
		return NewBoolean(disjoint)
	})))

	return proto
}

// createSetIterator creates a Set iterator object that yields values.
// It snapshots the keys at creation time.
func createSetIterator(setObj *JSObject, kind string) *JSObject {
	iter := NewJSObject()
	iter.ConstructorName = "SetIterator"

	var keyNames []string
	data := setObj.Get("__set_data__")
	if data.IsObject() && data.ObjVal != nil {
		if !data.ObjVal.Shape.IsDictionary {
			for name := range data.ObjVal.Shape.Properties {
				keyNames = append(keyNames, name)
			}
		} else if data.ObjVal.Dictionary != nil {
			for name := range data.ObjVal.Dictionary {
				keyNames = append(keyNames, name)
			}
		}
	}

	keysArr := NewJSObject()
	keysArr.ConstructorName = "Array"
	if ArrayPrototype != nil {
		keysArr.Prototype = ArrayPrototype
	}
	for i, k := range keyNames {
		keysArr.Set(intKey(i), NewString(k))
	}
	keysArr.Set("length", NewNumber(float64(len(keyNames))))

	iter.Set("__kind__", NewString(kind))
	iter.Set("__keys__", NewObject(keysArr))
	iter.Set("__index__", NewNumber(0))
	iter.Set("__set_ref__", NewObject(setObj))

	iter.Set("next", NewObject(builtinFunc("SetIterator.next", func(this *JSObject, args []JSValue) JSValue {
		idx := int(this.Get("__index__").ToNumber())
		keysRef := this.Get("__keys__")
		setRef := this.Get("__set_ref__")

		if !keysRef.IsObject() || keysRef.ObjVal == nil {
			return createIteratorResultObject(Undefined, true)
		}
		length := int(keysRef.ObjVal.Get("length").ToNumber())

		// Skip values that have been deleted from the Set since iterator creation.
		var keyStr string
		for idx < length {
			keyStr = keysRef.ObjVal.Get(intKey(idx)).ToString()
			if setRef.IsObject() && setRef.ObjVal != nil {
				data := setRef.ObjVal.Get("__set_data__")
				if data.IsObject() && data.ObjVal != nil {
					if !data.ObjVal.Get(keyStr).IsUndefined() {
						break // value still exists in the Set
					}
				}
			}
			idx++
		}

		if idx >= length {
			this.Set("__index__", NewNumber(float64(length)))
			return createIteratorResultObject(Undefined, true)
		}

		this.Set("__index__", NewNumber(float64(idx+1)))
		decodedKey := decodeKey(keyStr)
		return createIteratorResultObject(decodedKey, false)
	})))

	return iter
}

// registerWeakMap registers the WeakMap constructor and prototype.
// WeakMap keys must be objects; primitives throw TypeError.
// Methods: get, set, has, delete. No size, iteration, or clear.
// Uses runtime.AddCleanup for GC-aware weak key semantics.
func (vm *VM) registerWeakMap() {
	// Unique instance counter for weak map instances.
	var weakMapInstanceSeq uint64

	weakMapCtor := NewJSObject()
	weakMapCtor.ConstructorName = "Function"
	weakMapCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		weakMapInstanceSeq++
		instanceID := fmt.Sprintf("__wm_%d__", weakMapInstanceSeq)

		wm := NewJSObject()
		wm.ConstructorName = "WeakMap"
		wm.Set("__weakmap_instance__", NewString(instanceID))
		wm.Prototype = weakMapProto()
		return NewObject(wm)
	}

	vm.globals.M["WeakMap"] = NewObject(weakMapCtor)
}

func weakMapProto() *JSObject {
	proto := NewJSObject()
	proto.ConstructorName = "WeakMap"

	proto.Set("set", NewObject(builtinFunc("WeakMap.set", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return NewObject(this)
		}
		// Note: spec requires TypeError for non-object keys.
		// Current VM doesn't support throwing from builtins; non-object keys silently fail.
		instanceID := this.Get("__weakmap_instance__").ToString()
		weakMapSet(instanceID, args[0], args[1])
		return NewObject(this)
	})))

	proto.Set("get", NewObject(builtinFunc("WeakMap.get", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Undefined
		}
		instanceID := this.Get("__weakmap_instance__").ToString()
		return weakMapGet(instanceID, args[0])
	})))

	proto.Set("has", NewObject(builtinFunc("WeakMap.has", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		instanceID := this.Get("__weakmap_instance__").ToString()
		return NewBoolean(weakMapHas(instanceID, args[0]))
	})))

	proto.Set("delete", NewObject(builtinFunc("WeakMap.delete", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		instanceID := this.Get("__weakmap_instance__").ToString()
		return NewBoolean(weakMapDelete(instanceID, args[0]))
	})))

	return proto
}

// registerWeakSet registers the WeakSet constructor and prototype.
// WeakSet values must be objects; primitives throw TypeError.
// Methods: add, has, delete. No size, iteration, or clear.
func (vm *VM) registerWeakSet() {
	weakSetCtor := NewJSObject()
	weakSetCtor.ConstructorName = "Function"
	weakSetCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		ws := NewJSObject()
		ws.ConstructorName = "WeakSet"
		ws.Set("__weakset_data__", NewObject(NewJSObject()))
		ws.Prototype = weakSetProto()
		return NewObject(ws)
	}

	vm.globals.M["WeakSet"] = NewObject(weakSetCtor)
}

func weakSetProto() *JSObject {
	proto := NewJSObject()
	proto.ConstructorName = "WeakSet"

	proto.Set("add", NewObject(builtinFunc("WeakSet.add", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewObject(this)
		}
		// Note: spec requires TypeError for non-object values.
		// Current VM doesn't support throwing from builtins; values are stringified like Set.
		key := keyString(args[0])
		data := this.Get("__weakset_data__")
		if data.IsObject() && data.ObjVal != nil {
			data.ObjVal.Set(key, True)
		}
		return NewObject(this)
	})))

	proto.Set("has", NewObject(builtinFunc("WeakSet.has", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		key := keyString(args[0])
		data := this.Get("__weakset_data__")
		if data.IsObject() && data.ObjVal != nil {
			return NewBoolean(!data.ObjVal.Get(key).IsUndefined())
		}
		return False
	})))

	proto.Set("delete", NewObject(builtinFunc("WeakSet.delete", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		key := keyString(args[0])
		data := this.Get("__weakset_data__")
		had := false
		if data.IsObject() && data.ObjVal != nil {
			had = !data.ObjVal.Get(key).IsUndefined()
			if had {
				data.ObjVal.Delete(key)
			}
		}
		return NewBoolean(had)
	})))

	return proto
}
