package js

func (vm *VM) registerObject() {
	// Object constructor.
	objectCtor := NewJSObject()
	objectCtor.ConstructorName = "Function"
	objectCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) > 0 && args[0].IsObject() {
			return args[0]
		}
		return NewObject(NewJSObject())
	}

	// Object.prototype.toString
	ObjectPrototype.Set("toString", vm.createBuiltinFunction("toString", func(this *JSObject, args []JSValue) JSValue {
		return NewString(this.ToString())
	}))

	// Object.prototype.hasOwnProperty
	ObjectPrototype.Set("hasOwnProperty", vm.createBuiltinFunction("hasOwnProperty", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		propName := args[0].ToString()
		_, ok := this.getOwn(propName)
		return NewBoolean(ok)
	}))

	// Set Object.prototype for instanceof checks.
	objectCtor.Set("prototype", NewObject(ObjectPrototype))

	vm.globals.M["Object"] = NewObject(objectCtor)

	// Object.defineProperty(obj, prop, descriptor)
	vm.globals.M["Object.defineProperty"] = vm.createBuiltinFunction("defineProperty", func(this *JSObject, ts []JSValue) JSValue {
		_ = this
		if len(ts) < 3 || !ts[0].IsObject() || ts[0].ObjVal == nil {
			return Undefined
		}
		obj := ts[0].ObjVal
		propName := ts[1].ToString()
		if !ts[2].IsObject() || ts[2].ObjVal == nil {
			return Undefined
		}
		desc := ts[2].ObjVal

		attr := PropertyAttr(AttrDefault)
		if val := desc.Get("writable"); val.IsBoolean() && !val.BoolVal {
			attr &^= AttrWritable
		}
		if val := desc.Get("enumerable"); val.IsBoolean() && !val.BoolVal {
			attr &^= AttrEnumerable
		}
		if val := desc.Get("configurable"); val.IsBoolean() && !val.BoolVal {
			attr &^= AttrConfigurable
		}

		// Update Shape with new attributes.
		if !obj.Shape.HasProperty(propName) {
			obj.Shape = obj.Shape.AddPropertyWithAttr(propName, attr)
			obj.growProperties(obj.Shape.PropertyCount)
		}
		offset := obj.Shape.GetOffset(propName)
		if val := desc.Get("value"); !val.IsUndefined() && offset >= 0 && offset < obj.propLen() {
			obj.propSet(offset, val)
		}
		return NewObject(obj)
	})

	// Object.assign(target, ...sources) — copy own enumerable properties from sources to target.
	objectCtor.Set("assign", vm.createBuiltinFunction("Object.assign", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Undefined
		}
		target := args[0]
		if !target.IsObject() || target.ObjVal == nil {
			return target
		}
		for i := 1; i < len(args); i++ {
			src := args[i]
			if !src.IsObject() || src.ObjVal == nil {
				continue
			}
			srcObj := src.ObjVal
			for name, entry := range srcObj.Shape.Properties {
				if entry.Attr&AttrEnumerable == 0 {
					continue
				}
				if entry.Offset >= 0 && entry.Offset < srcObj.propLen() {
					target.ObjVal.Set(name, srcObj.propAt(entry.Offset))
				}
			}
		}
		return target
	}))

	// Object.keys(obj) — returns array of own enumerable property names.
	objectCtor.Set("keys", vm.createBuiltinFunction("Object.keys", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return NewObject(NewJSObject())
		}
		obj := args[0].ObjVal
		keys := NewJSObject()
		keys.ConstructorName = "Array"
		if ArrayPrototype != nil {
			keys.Prototype = ArrayPrototype
		}
		idx := 0
		for name, entry := range obj.Shape.Properties {
			if entry.Attr&AttrEnumerable == 0 {
				continue
			}
			keys.Set(intKey(idx), NewString(name))
			idx++
		}
		keys.Set("length", NewNumber(float64(idx)))
		return NewObject(keys)
	}))

	// Object.entries(obj) — returns array of [key, value] pairs.
	objectCtor.Set("entries", vm.createBuiltinFunction("Object.entries", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return NewObject(NewJSObject())
		}
		obj := args[0].ObjVal
		entries := NewJSObject()
		entries.ConstructorName = "Array"
		if ArrayPrototype != nil {
			entries.Prototype = ArrayPrototype
		}
		idx := 0
		for name, entry := range obj.Shape.Properties {
			if entry.Attr&AttrEnumerable == 0 {
				continue
			}
			val := Undefined
			if entry.Offset >= 0 && entry.Offset < obj.propLen() {
				val = obj.propAt(entry.Offset)
			}
			pair := NewJSObject()
			pair.ConstructorName = "Array"
			if ArrayPrototype != nil {
				pair.Prototype = ArrayPrototype
			}
			pair.Set("0", NewString(name))
			pair.Set("1", val)
			pair.Set("length", NewNumber(2))
			entries.Set(intKey(idx), NewObject(pair))
			idx++
		}
		entries.Set("length", NewNumber(float64(idx)))
		return NewObject(entries)
	}))

	// Object.values(obj) — returns array of own enumerable property values.
	objectCtor.Set("values", vm.createBuiltinFunction("Object.values", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return NewObject(NewJSObject())
		}
		obj := args[0].ObjVal
		vals := NewJSObject()
		vals.ConstructorName = "Array"
		if ArrayPrototype != nil {
			vals.Prototype = ArrayPrototype
		}
		idx := 0
		for _, entry := range obj.Shape.Properties {
			if entry.Attr&AttrEnumerable == 0 {
				continue
			}
			val := Undefined
			if entry.Offset >= 0 && entry.Offset < obj.propLen() {
				val = obj.propAt(entry.Offset)
			}
			vals.Set(intKey(idx), val)
			idx++
		}
		vals.Set("length", NewNumber(float64(idx)))
		return NewObject(vals)
	}))

	// Object.fromEntries(entries) — converts [key,val] pairs to object.
	objectCtor.Set("fromEntries", vm.createBuiltinFunction("Object.fromEntries", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return NewObject(NewJSObject())
		}
		iterable := args[0].ObjVal
		result := NewJSObject()
		length := int(iterable.Get("length").ToNumber())
		for i := 0; i < length; i++ {
			pair := iterable.Get(intKey(i))
			if pair.IsObject() && pair.ObjVal != nil {
				key := pair.ObjVal.Get("0").ToString()
				val := pair.ObjVal.Get("1")
				result.Set(key, val)
			}
		}
		return NewObject(result)
	}))

	// Object.freeze(obj)
	objectCtor.Set("freeze", vm.createBuiltinFunction("Object.freeze", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return args[0]
		}
		obj := args[0].ObjVal
		obj.SetFrozen()
		obj.SetSealed()
		return NewObject(obj)
	}))

	// Object.seal(obj)
	objectCtor.Set("seal", vm.createBuiltinFunction("Object.seal", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return args[0]
		}
		args[0].ObjVal.SetSealed()
		return NewObject(args[0].ObjVal)
	}))

	// Object.isFrozen(obj)
	objectCtor.Set("isFrozen", vm.createBuiltinFunction("Object.isFrozen", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return True
		}
		return NewBoolean(args[0].ObjVal.IsFrozen())
	}))

	// Object.isSealed(obj)
	objectCtor.Set("isSealed", vm.createBuiltinFunction("Object.isSealed", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return True
		}
		return NewBoolean(args[0].ObjVal.IsSealed())
	}))

	// Object.create(proto, props) — creates object with given prototype.
	objectCtor.Set("create", vm.createBuiltinFunction("Object.create", func(this *JSObject, args []JSValue) JSValue {
		obj := NewJSObject()
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil {
			obj.Prototype = args[0].ObjVal
		}
		// Properties object (second argument) — ECMAScript §19.1.2.2
		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil {
			props := args[1].ObjVal
			keys := objectKeys(props)
			for _, key := range keys {
				descVal := props.Get(key)
				if !descVal.IsObject() || descVal.ObjVal == nil {
					continue
				}
				desc := descVal.ObjVal

				// Data property descriptor.
				if val, ok := desc.getOwn("value"); ok {
					obj.Set(key, val)
				}
				// Note: writable, enumerable, configurable attributes tracked
				// through Shape — not yet implemented. Properties are writable
				// and enumerable by default.
			}
		}
		return NewObject(obj)
	}))

}
