// vm_ops_property.go — Property access, global variable, and object-creation handlers.
package js

import (
	"unsafe"
)

func opTypeof(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewString(jsTypeof(frame.Acc))
}

func opDelete(vm *VM, frame *VMFrame, instr Instruction) {
	propIdx := int(instr.OperandA)
	propName := ""
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil && propName != "" {
		if frame.Acc.ObjVal.Delete(propName) {
			frame.Acc = True
		} else {
			frame.Acc = False
		}
	} else {
		frame.Acc = False
	}
}

func opDeleteKeyed(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	keyReg := int(instr.OperandB)
	objVal := frame.Regs[objReg]
	key := frame.Regs[keyReg].ToString()
	if objVal.IsObject() && objVal.ObjVal != nil {
		frame.Acc = NewBoolean(objVal.ObjVal.Delete(key))
	} else {
		frame.Acc = True // deleting from non-object returns true per spec
	}
}

func opInstanceof(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if !lhs.IsObject() || lhs.ObjVal == nil || !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
		frame.Acc = False
	} else {
		proto := lhs.ObjVal.Prototype
		rhsProto := frame.Acc.ObjVal.Get("prototype")
		found := false
		for proto != nil {
			if proto == rhsProto.ObjVal {
				found = true
				break
			}
			proto = proto.Prototype
		}
		// Cross-realm fallback: if pointer comparison failed, check whether
		// the LHS prototype chain contains entries tagged with a different
		// RealmID. Only triggered when realms differ AND the RHS prototype's
		// ConstructorName is a known built-in type.
		if !found && rhsProto.IsObject() && rhsProto.ObjVal != nil {
			crossRealm := false
			for p := lhs.ObjVal.Prototype; p != nil; p = p.Prototype {
				if r := vm.getObjectRealm(p); r != 0 && r != vm.RealmID {
					crossRealm = true
					break
				}
			}
			if crossRealm {
				rhsName := rhsProto.ObjVal.ConstructorName
				if isBuiltinPrototype(rhsName) {
					proto = lhs.ObjVal.Prototype
					for proto != nil {
						if proto.ConstructorName == rhsName {
							found = true
							break
						}
						proto = proto.Prototype
					}
				}
			}
		}
		frame.Acc = NewBoolean(found)
	}
}

// builtinPrototypes lists ConstructorNames of built-in prototypes that
// participate in cross-realm instanceof checks. "Object" is excluded
// because NewJSObject() sets it as the default, so it would match
// everything and break standard prototype chain logic.
var builtinPrototypes = map[string]bool{
	"Array":                true,
	"String":               true,
	"Number":               true,
	"Boolean":              true,
	"Function":             true,
	"Date":                 true,
	"RegExp":               true,
	"Error":                true,
	"TypeError":            true,
	"SyntaxError":          true,
	"RangeError":           true,
	"ReferenceError":       true,
	"URIError":             true,
	"EvalError":            true,
	"Map":                  true,
	"Set":                  true,
	"WeakMap":              true,
	"WeakSet":              true,
	"WeakRef":              true,
	"Promise":              true,
	"ArrayBuffer":          true,
	"DataView":             true,
	"Int8Array":            true,
	"Uint8Array":           true,
	"Uint8ClampedArray":    true,
	"Int16Array":           true,
	"Uint16Array":          true,
	"Int32Array":           true,
	"Uint32Array":          true,
	"Float32Array":         true,
	"Float64Array":         true,
	"BigInt64Array":        true,
	"BigUint64Array":       true,
	"Symbol":               true,
	"Proxy":                true,
	"FinalizationRegistry": true,
}

func isBuiltinPrototype(name string) bool {
	return builtinPrototypes[name]
}

func opIn(vm *VM, frame *VMFrame, instr Instruction) {
	// lhs (register) = property name (typically a string from key expression)
	// acc = object to check
	propName := frame.Regs[int(instr.OperandA)].ToString()
	if !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
		throwTypeErrorInFrame(frame, "Cannot use 'in' operator to search for '"+propName+"' in "+frame.Acc.ToString())
		return
	}
	frame.Acc = NewBoolean(frame.Acc.ObjVal.Has(propName))
}

// --- Property access handlers ---

// patchPolyICIfNeeded patches the JIT IC slot if the state has transitioned to
// polymorphic and Sparkplug code is available. No-op otherwise.
// Guard: PolyCount must be ≥ 2 (monomorphic slots use vm.patcher.PatchMonomorphic).
func (vm *VM) patchPolyICIfNeeded(frame *VMFrame, slotIdx int, slot *ICSlot) {
	if frame == nil || frame.Func == nil {
		return
	}
	if frame.Func.Sparkplug == 0 || slot.State != ICPolymorphic || slot.Patched || vm.patcher == nil {
		return
	}
	if slot.PolyCount >= 2 {
		vm.patcher.PatchPolymorphic(frame.Func.Sparkplug, slotIdx, slot.PolyShapes[:slot.PolyCount], slot.PolyOffsets[:slot.PolyCount])
		slot.Patched = true
	}
}

func opLdaNamedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	propIdx := int(instr.OperandA)
	slotIdx := int(instr.OperandC)

	// Fast path: try IC without resolving the property name string.
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		obj := frame.Acc.ObjVal
		slots := frame.Func.ICVector.Slots
		if slotIdx < len(slots) {
			slot := &slots[slotIdx]
			// Monomorphic fast path: shape match → direct offset access (0 allocs).
			if slot.State == ICMonomorphic && obj.Shape == slot.Shape {
				if slot.Offset < obj.propLen() {
					frame.Acc = obj.propAt(slot.Offset)
					slot.HitCount++
					// Patch JIT IC slot once when Sparkplug code becomes available.
					if !vm.DisableJIT && frame.Func.Sparkplug != 0 && vm.patcher != nil && !slot.Patched {
						vm.patcher.PatchMonomorphic(frame.Func.Sparkplug, slotIdx, unsafe.Pointer(obj.Shape), slot.Offset)
						slot.Patched = true
					}
					return
				}
			}
			// Megamorphic fast path: cached shape re-check.
			if slot.State == ICMegamorphic && obj.Shape == slot.MegaShape && slot.MegaOffset < obj.propLen() {
				frame.Acc = obj.propAt(slot.MegaOffset)
				slot.HitCount++
				return
			}
			// Poly/mega patching: if state just became polymorphic or megamorphic,
			// patch the JIT IC slot accordingly.
			vm.patchPolyICIfNeeded(frame, slotIdx, slot)
			if !vm.DisableJIT && frame.Func.Sparkplug != 0 && slot.State == ICMegamorphic && !slot.Patched {
				if vm.patcher != nil {
					vm.patcher.PatchMegamorphic(frame.Func.Sparkplug, slotIdx)
					slot.Patched = true
				}
			}
		}
	}

	// Slow path: resolve name from constant pool, then use IC or direct Get.
	// Use pre-computed ConstantNames (compiled once, 0 allocs) when available.
	// Fallback to ToString() only if ConstantNames hasn't been built.
	propName := ""
	if propIdx < len(frame.Func.ConstantNames) {
		propName = frame.Func.ConstantNames[propIdx]
	} else if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		frame.Acc = frame.Func.ICVector.LoadIC(slotIdx, frame.Acc.ObjVal, propName)
		// After LoadIC, the slot state may have transitioned to polymorphic.
		// Patch the JIT IC slot with sequential shape guards.
		slots := frame.Func.ICVector.Slots
		if slotIdx < len(slots) {
			vm.patchPolyICIfNeeded(frame, slotIdx, &slots[slotIdx])
		}
	} else if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		frame.Acc = frame.Acc.ObjVal.Get(propName)
	} else if frame.Acc.IsString() {
		// Autobox string: wrap in a String object so prototype methods are reachable.
		boxed := NewJSObject()
		boxed.ConstructorName = "String"
		boxed.Prototype = StringPrototype
		boxed.Set("__value__", frame.Acc)
		frame.Acc = boxed.Get(propName)
	} else {
		frame.Acc = Undefined
	}
}

func opStaNamedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	propIdx := int(instr.OperandA)
	val := Undefined
	if int(instr.OperandB) < len(frame.Regs) {
		val = frame.Regs[int(instr.OperandB)]
	}
	slotIdx := int(instr.OperandC)

	// Fast path: try IC without resolving the property name string.
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		obj := frame.Acc.ObjVal
		slots := frame.Func.ICVector.Slots
		if slotIdx < len(slots) {
			slot := &slots[slotIdx]
			// Monomorphic fast path: shape match → direct offset write (0 allocs).
			if slot.State == ICMonomorphic && obj.Shape == slot.Shape {
				if !obj.IsFrozen() && slot.Offset < obj.propLen() {
					obj.propSet(slot.Offset, val)
					return
				}
			}
		}
	}

	// Slow path: resolve name from constant pool.
	propName := ""
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		frame.Func.ICVector.StoreIC(slotIdx, frame.Acc.ObjVal, propName, val)
	} else if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		frame.Acc.ObjVal.Set(propName, val)
	}
}

// opStaByOffset stores a value directly into an object's property slot by offset.
// OperandA = object register, OperandB = property offset, OperandC = value register.
// Used by object literal fast path to skip shape lookups when offsets are known.
func opStaByOffset(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	offset := int(instr.OperandB)
	valReg := int(instr.OperandC)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		val := Undefined
		if valReg < len(frame.Regs) {
			val = frame.Regs[valReg]
		}
		obj.lastLookupValid = false
		if !obj.IsFrozen() && !obj.IsSealed() {
			obj.propSet(offset, val)
		}
	}
}

// isDenseArray returns true if obj is a dense Array (has ArrayPrototype and a valid Shape).
// Dense arrays have consecutive numeric indices that can be accessed directly by offset.
func isDenseArray(obj *JSObject) bool {
	return obj.Prototype == ArrayPrototype && obj.Shape != nil
}

func opLdaKeyedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() {
		obj := frame.Regs[objReg].ObjVal
		key := frame.Acc.ToString()

		// Dense array fast path: if the key is a numeric index, use direct offset access.
		if isDenseArray(obj) && !obj.Shape.IsDictionary {
			if idx, ok := parseArrayIndex(key); ok {
				length := int(obj.Get("length").ToNumber())
				if idx < length {
					// Compute offset for this index: find base offset of "0" once.
					// For dense arrays, properties "0", "1", ... are stored sequentially.
					if offset := obj.Shape.GetOffset(key); offset >= 0 && offset < obj.propLen() {
						frame.Acc = obj.propAt(offset)
						return
					}
				}
			}
		}

		frame.Acc = obj.Get(key)
	} else {
		frame.Acc = Undefined
	}
}

func opStaKeyedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	objReg, valReg := int(instr.OperandA), int(instr.OperandB)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && valReg < len(frame.Regs) {
		obj := frame.Regs[objReg].ObjVal
		key := frame.Acc.ToString()

		// Dense array fast path: use direct offset access for numeric indices.
		if isDenseArray(obj) && !obj.Shape.IsDictionary {
			if idx, ok := parseArrayIndex(key); ok {
				length := int(obj.Get("length").ToNumber())
				if idx <= length { // allow writing one past for array growth
					if offset := obj.Shape.GetOffset(key); offset >= 0 && offset < obj.propLen() && !obj.IsFrozen() {
						obj.propSet(offset, frame.Regs[valReg])
						// Update length if writing at or beyond current.
						if idx >= length {
							obj.Set("length", NewNumber(float64(idx+1)))
						}
						return
					}
				}
			}
		}

		obj.Set(key, frame.Regs[valReg])
	}
}

// parseArrayIndex parses a string as a non-negative integer array index.
// Returns (index, true) for valid array indices like "0", "42".
// Returns (0, false) for non-integer keys like "length", "foo", "-1", "1.5".
func parseArrayIndex(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	// Must start with a digit and not be "0"-prefixed (except "0" itself).
	if s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		// Cap at a reasonable max to avoid overflow.
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, true
}

// --- Object / Array handlers ---

func opCreateObject(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewObject(NewJSObject())
}

func opCreateObjectLiteral(vm *VM, frame *VMFrame, instr Instruction) {
	// OperandA = constant pool index; Shapes[A] holds the pre-built Shape pointer.
	shapeKeyIdx := int(instr.OperandA)
	var shape *Shape
	if shapeKeyIdx < len(frame.Func.Shapes) {
		shape = frame.Func.Shapes[shapeKeyIdx]
	}
	if shape == nil {
		// Fallback: use prop names from ShapePropNames.
		var propNames []string
		if shapeKeyIdx < len(frame.Func.ShapePropNames) {
			propNames = frame.Func.ShapePropNames[shapeKeyIdx]
		}
		shape = GetOrCreateShape(propNames)
	}
	obj := vm.allocObj()
	obj.Shape = shape
	obj.Prototype = ObjectPrototype
	obj.ConstructorName = "Object"
	if shape.PropertyCount > 0 {
		obj.growProperties(shape.PropertyCount)
	}
	frame.Acc = JSValue{Tag: TagObject, ObjVal: obj}
}

func opCreateArray(vm *VM, frame *VMFrame, instr Instruction) {
	arr := NewJSObject()
	arr.ConstructorName = "Array"
	if ArrayPrototype != nil {
		arr.Prototype = ArrayPrototype
	}
	arr.Set("length", NewNumber(0))
	frame.Acc = NewObject(arr)
}

func opCreateRegExp(vm *VM, frame *VMFrame, instr Instruction) {
	patIdx, flagsIdx := int(instr.OperandA), int(instr.OperandB)
	var pattern, flags string
	if patIdx < len(frame.Func.Constants) {
		pattern = frame.Func.Constants[patIdx].ToString()
	}
	if flagsIdx < len(frame.Func.Constants) {
		flags = frame.Func.Constants[flagsIdx].ToString()
	}
	frame.Acc = vm.createRegExp(pattern, flags)
}

func opSetPrototype(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
			frame.Regs[objReg].ObjVal.Prototype = frame.Acc.ObjVal
		}
	}
}

// --- Variable handlers ---

func opLdaGlobal(vm *VM, frame *VMFrame, instr Instruction) {
	nameIdx := int(instr.OperandA)
	if nameIdx < len(frame.Func.ConstantNames) {
		name := frame.Func.ConstantNames[nameIdx]
		if val, ok := vm.globals.Lookup(name); ok {
			frame.Acc = val
			// Cache the value in Constants pool for JIT fast-path.
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = val
			}
		} else if builtinFn, ok := vm.registry.Builtins[name]; ok {
			frame.Acc = vm.makeBuiltinFunction(name, builtinFn)
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = frame.Acc
			}
		} else if funcBF, ok := vm.registry.Funcs[name]; ok {
			frame.Acc = vm.makeFunctionObject(name, funcBF)
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = frame.Acc
			}
		} else {
			frame.Acc = Undefined
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = Undefined
			}
		}
	} else if nameIdx < len(frame.Func.Constants) {
		name := frame.Func.Constants[nameIdx].ToString()
		if val, ok := vm.globals.Lookup(name); ok {
			frame.Acc = val
			frame.Func.Constants[nameIdx] = val
		}
	}
}

func opStaGlobal(vm *VM, frame *VMFrame, instr Instruction) {
	nameIdx := int(instr.OperandA)
	if nameIdx < len(frame.Func.ConstantNames) {
		vm.globals.Set(frame.Func.ConstantNames[nameIdx], frame.Acc)
		// Cache the value in Constants pool for JIT fast-path.
		if nameIdx < len(frame.Func.Constants) {
			frame.Func.Constants[nameIdx] = frame.Acc
		}
	} else if nameIdx < len(frame.Func.Constants) {
		vm.globals.Set(frame.Func.Constants[nameIdx].ToString(), frame.Acc)
		// Cache the value in Constants pool for JIT fast-path.
		frame.Func.Constants[nameIdx] = frame.Acc
	}
}

func opLdaGlobalSlot(vm *VM, frame *VMFrame, instr Instruction) {
	slot := int(instr.OperandA)
	// Fast path: direct array lookup, but verify the slot name matches.
	// Different compilation units may assign different slot indices for the
	// same name, so we must check the slot name to avoid reading stale values.
	if slot < vm.globals.SlotCount() && vm.globals.HasSlot(slot) &&
		slot < len(frame.Func.GlobalSlots) &&
		vm.globals.SlotNameMatches(slot, frame.Func.GlobalSlots[slot]) {
		frame.Acc = vm.globals.GetSlot(slot)
		// Update GlobalVals cache for JIT fast-path reads.
		if slot < len(frame.Func.GlobalVals) {
			frame.Func.GlobalVals[slot] = frame.Acc
		}
		return
	}
	// Fallback: name-based lookup for builtins, funcRegistry, and globals map.
	if slot < len(frame.Func.GlobalSlots) {
		name := frame.Func.GlobalSlots[slot]
		if val, ok := vm.globals.Lookup(name); ok {
			// Cache in slot for subsequent fast-path reads.
			vm.globals.EnsureSlots(slot + 1)
			vm.globals.SetSlotWithName(slot, val, name)
			// Update GlobalVals cache for JIT fast-path reads.
			if slot < len(frame.Func.GlobalVals) {
				frame.Func.GlobalVals[slot] = val
			}
			frame.Acc = val
			return
		}
		if builtinFn, ok := vm.registry.Builtins[name]; ok {
			frame.Acc = vm.makeBuiltinFunction(name, builtinFn)
			// Cache in GlobalVals for JIT fast-path reads.
			if slot < len(frame.Func.GlobalVals) {
				frame.Func.GlobalVals[slot] = frame.Acc
			}
			return
		}
		if funcBF, ok := vm.registry.Funcs[name]; ok {
			frame.Acc = vm.makeFunctionObject(name, funcBF)
			// Cache in GlobalVals for JIT fast-path reads.
			if slot < len(frame.Func.GlobalVals) {
				frame.Func.GlobalVals[slot] = frame.Acc
			}
			return
		}
	}
	frame.Acc = Undefined
}

func opStaGlobalSlot(vm *VM, frame *VMFrame, instr Instruction) {
	slot := int(instr.OperandA)
	if slot >= vm.globals.SlotCount() {
		vm.ensureGlobalSlots(slot+16, frame.Func)
	}
	// Record the name at this slot for SetGlobal write-through.
	if slot < len(frame.Func.GlobalSlots) {
		name := frame.Func.GlobalSlots[slot]
		vm.globals.SetSlotWithName(slot, frame.Acc, name)
	} else {
		vm.globals.SetSlot(slot, frame.Acc)
	}
	// Update the function's GlobalVals cache for JIT fast-path reads.
	if slot < len(frame.Func.GlobalVals) {
		frame.Func.GlobalVals[slot] = frame.Acc
	}
}

func opLdaLocal(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Acc = frame.Regs[reg]
	}
}

func opLdaThis(vm *VM, frame *VMFrame, instr Instruction) {
	// In derived class constructors, this is uninitialized until super() is called.
	if frame.Func != nil && frame.Func.IsDerivedConstructor && !frame.SuperCalled {
		throwReferenceErrorInFrame(frame, "Must call super constructor before accessing 'this' in derived class")
		return
	}
	frame.Acc = frame.This
}

func opStaLocal(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Regs[reg] = frame.Acc
	}
}

func opDup(vm *VM, frame *VMFrame, instr Instruction) {
	// Duplicate accumulator into destination register without modifying acc.
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Regs[reg] = frame.Acc
	}
}

func opThrowConstAssignment(vm *VM, frame *VMFrame, instr Instruction) {
	nameIdx := int(instr.OperandA)
	var name string
	if nameIdx < len(frame.Func.Constants) {
		name = frame.Func.Constants[nameIdx].ToString()
	}
	msg := "Assignment to constant variable"
	if name != "" {
		msg = "Assignment to constant variable '" + name + "'"
	}
	frame.Thrown = NewString("TypeError: " + msg)
	if frame.HandlerPC >= 0 {
		frame.PC = frame.HandlerPC
		return
	}
	if frame.FinallyPC >= 0 {
		frame.PC = frame.FinallyPC
		return
	}
}

// ensureGlobalSlots grows the global slot arrays to accommodate at least n slots.
// Also ensures bf.GlobalVals is sized correctly and copies current slot values.
func (vm *VM) ensureGlobalSlots(n int, bf *BytecodeFunction) {
	vm.globals.EnsureSlots(n)
	// Ensure bf.GlobalVals is large enough and synced with current slot values.
	if bf != nil && n > len(bf.GlobalVals) {
		newVals := make([]JSValue, n)
		copy(newVals, bf.GlobalVals)
		bf.GlobalVals = newVals
	}
}

// --- Realm management ---

// tagAllPrototypes walks the VM's globals and tags all built-in prototype objects
// with this VM's RealmID for cross-realm instanceof detection.
//
// Package-level prototypes (ObjectPrototype, ArrayPrototype, etc.) are shared
// across all VM instances. We intentionally skip tagging them here — each new
// VM would overwrite the previous VM's RealmID, causing false cross-realm
// detection. For shared prototypes, opInstanceof relies on pointer comparison
// (same pointer across VMs) rather than RealmID+ConstructorName fallback.
func (vm *VM) tagAllPrototypes() {
	// Skip shared package-level prototypes. These are the same *JSObject
	// pointers across all VMs, so tagging them with per-VM RealmID would
	// cause false cross-realm positives.
	// Instead, iterate globals and tag only per-VM prototypes (those created
	// during RegisterBuiltins — error prototypes, constructor prototypes, etc.).
	for _, val := range vm.globals.M {
		if val.IsObject() && val.ObjVal != nil {
			protoVal := val.ObjVal.Get("prototype")
			if protoVal.IsObject() && protoVal.ObjVal != nil {
				vm.tagPrototype(protoVal.ObjVal)
			}
		}
	}
}

// tagPrototype records that a prototype object belongs to this VM's realm.
func (vm *VM) tagPrototype(obj *JSObject) {
	if obj != nil {
		globalPrototypeRealmMu.Lock()
		globalPrototypeRealm[obj] = vm.RealmID
		globalPrototypeRealmMu.Unlock()
	}
}

// getObjectRealm returns the RealmID tagged on an object, or 0 if not tagged.
func (vm *VM) getObjectRealm(obj *JSObject) uint64 {
	if obj == nil {
		return 0
	}
	globalPrototypeRealmMu.RLock()
	r := globalPrototypeRealm[obj]
	globalPrototypeRealmMu.RUnlock()
	return r
}
