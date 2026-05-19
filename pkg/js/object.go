// object.go — JSObject: runtime representation of JavaScript objects.
//
// Each JSObject holds a pointer to its Shape (Hidden Class) and a flat property
// store. Fast-path lookups use the Shape's property offset; slow-path falls back
// to a dictionary when shapes diverge too far.
package js

import (
	"fmt"
)

// inlinePropsMax is the maximum number of properties stored inline.
// Objects with ≤4 properties (95%+ of all objects) avoid heap allocation.
const inlinePropsMax = 4

// InterceptorMixin provides DOM/Proxy/TypedArray property interception callbacks.
// Nil for most objects. Set on DOM elements, Proxy targets, and TypedArrays.
type InterceptorMixin struct {
	// OnPropertyGet is invoked when Get() resolves a property.
	// Returns (value, handled). If handled is true, value is used directly.
	OnPropertyGet func(obj *JSObject, key string) (JSValue, bool)
	// OnPropertySet is invoked when Set() assigns a property.
	// Returns true if the callback handled the assignment (default storage skipped).
	OnPropertySet func(obj *JSObject, key string, value JSValue) bool
	// OnHas is an optional callback invoked when Has() checks for property existence.
	// Returns (handled, result). If handled is true, result is used directly.
	OnHas func(obj *JSObject, key string) (bool, bool)
	// OnDelete is an optional callback invoked when Delete() removes a property.
	// Returns (handled, result). If handled is true, result is used directly.
	OnDelete func(obj *JSObject, key string) (bool, bool)
}

// ProxyMixin provides Proxy target and handler references.
// Nil for all objects except those created via `new Proxy(target, handler)`.
type ProxyMixin struct {
	// Target is the wrapped target object for Proxy.
	Target JSValue
	// Handler is the handler object with trap methods for Proxy.
	Handler *JSObject
}

// TypedArrayMixin provides TypedArray backing store.
// Nil for non-TypedArray objects.
type TypedArrayMixin struct {
	// ByteData stores the raw bytes backing an ArrayBuffer or TypedArray.
	ByteData []byte
}

// GeneratorMixin provides generator suspended execution state.
// Nil for all objects except suspended generator objects.
type GeneratorMixin struct {
	// State holds the generator's suspended execution state.
	State *GeneratorState
}

// JSObject is the runtime representation of a JavaScript object.
// Fields ordered by size for optimal alignment (largest first).
type JSObject struct {
	// Inline property store for objects with ≤4 properties.
	// Used when Properties is nil (inline mode). Promoted to heap slice
	// when more than inlinePropsMax properties are needed.
	inlineProps [inlinePropsMax]JSValue
	inlineCount int
	// Properties is the heap-allocated property store (nil when inline).
	Properties []JSValue
	// lastLookup caches the most recent property lookup to avoid repeated
	// Shape offset resolution on hot paths (e.g., loop variable reads).
	lastLookupValue JSValue
	// ConstructorName for display (e.g., "Object", "Array").
	ConstructorName string
	// lastLookup caches the lookup key.
	lastLookupName  string
	// Shape describes the hidden class / property layout.
	Shape *Shape
	// Dictionary for slow-path properties (used after shape divergence).
	Dictionary map[string]JSValue
	// Prototype for inheritance chain traversal.
	Prototype *JSObject
	// Callable function (set for function objects, closures, builtins).
	CallFunc func(this *JSObject, args []JSValue) JSValue
	// ConstructFunc is called when this object is used as a constructor (new.target).
	// If nil, CallFunc is used but receives a fresh object as this.
	ConstructFunc func(this *JSObject, args []JSValue, newTarget *JSObject) JSValue
	// Bytecode for user-defined functions (set by compiler, executed by VM).
	Bytecode *BytecodeFunction

	// Optional mixins (nil for most objects)
	interceptor *InterceptorMixin
	proxy       *ProxyMixin
	typedArray  *TypedArrayMixin
	generator   *GeneratorMixin

	// Seal/freeze state (bitfield: bit 0 = Frozen, bit 1 = Sealed)
	flags uint8
	// lastLookup cache validity flag.
	lastLookupValid bool
}

// IsFrozen returns true if the object is frozen via Object.freeze().
func (obj *JSObject) IsFrozen() bool { return obj.flags&1 != 0 }

// SetFrozen marks the object as frozen.
func (obj *JSObject) SetFrozen() { obj.flags |= 1 }

// IsSealed returns true if the object is sealed via Object.seal().
func (obj *JSObject) IsSealed() bool { return obj.flags&2 != 0 }

// SetSealed marks the object as sealed.
func (obj *JSObject) SetSealed() { obj.flags |= 2 }

// ensureInterceptor returns the InterceptorMixin, allocating on first use.
func (obj *JSObject) ensureInterceptor() *InterceptorMixin {
	if obj.interceptor == nil {
		obj.interceptor = &InterceptorMixin{}
	}
	return obj.interceptor
}

// ensureProxy returns the ProxyMixin, allocating on first use.
func (obj *JSObject) ensureProxy() *ProxyMixin {
	if obj.proxy == nil {
		obj.proxy = &ProxyMixin{}
	}
	return obj.proxy
}

// ensureTypedArray returns the TypedArrayMixin, allocating on first use.
func (obj *JSObject) ensureTypedArray() *TypedArrayMixin {
	if obj.typedArray == nil {
		obj.typedArray = &TypedArrayMixin{}
	}
	return obj.typedArray
}

// ensureGenerator returns the GeneratorMixin, allocating on first use.
func (obj *JSObject) ensureGenerator() *GeneratorMixin {
	if obj.generator == nil {
		obj.generator = &GeneratorMixin{}
	}
	return obj.generator
}

// PropLen returns the number of stored own properties.
func (obj *JSObject) PropLen() int {
	if obj.Properties == nil {
		return obj.inlineCount
	}
	return len(obj.Properties)
}

// propLen returns the number of stored own properties (internal alias).
func (obj *JSObject) propLen() int { return obj.PropLen() }

// PropAt returns the property value at the given index.
func (obj *JSObject) PropAt(i int) JSValue {
	if obj.Properties == nil {
		return obj.inlineProps[i]
	}
	return obj.Properties[i]
}

// propAt returns the property value at the given index (internal alias).
func (obj *JSObject) propAt(i int) JSValue { return obj.PropAt(i) }

// PropSet stores a value at the given property index.
func (obj *JSObject) PropSet(i int, v JSValue) {
	if obj.Properties == nil {
		obj.inlineProps[i] = v
		if i >= obj.inlineCount {
			obj.inlineCount = i + 1
		}
		return
	}
	obj.Properties[i] = v
}

// propSet stores a value at the given property index (internal alias).
func (obj *JSObject) propSet(i int, v JSValue) { obj.PropSet(i, v) }

// growProperties ensures the backing store has at least n elements.
// For inline mode with n ≤ inlinePropsMax, just updates inlineCount.
// For n > inlinePropsMax, promotes to a heap slice and copies inline data.
// For heap mode with insufficient capacity, extends the slice.
//
// Slack tracking (V8-style): the first few objects sharing a Shape get extra
// inline capacity to avoid repeated reallocations. Once the construction
// counter reaches zero, the Shape finalizes its property count.
func (obj *JSObject) growProperties(n int) {
	if obj.Properties != nil {
		if n > len(obj.Properties) {
			// Extend existing heap slice.
			for len(obj.Properties) < n {
				obj.Properties = append(obj.Properties, JSValue{})
			}
		}
		return // already on heap
	}
	if n <= inlinePropsMax {
		if n > obj.inlineCount {
			obj.inlineCount = n
		}
		return // still fits inline
	}
	// Promotion from inline to heap: apply slack tracking.
	s := obj.Shape
	allocSize := n
	if !s.SlackFinal {
		if s.SlackCounter > 0 {
			s.SlackCounter--
			if inlinePropsMax > n {
				allocSize = inlinePropsMax
			}
		} else {
			s.SlackFinal = true
			s.FinalPropCount = s.PropertyCount
			allocSize = s.FinalPropCount
		}
	} else {
		allocSize = s.FinalPropCount
		if n > allocSize {
			allocSize = n
		}
	}
	obj.Properties = make([]JSValue, allocSize)
	copy(obj.Properties, obj.inlineProps[:obj.inlineCount])
}

// NewJSObject allocates a new JSObject with default initialization.
// For hot paths during bytecode execution, use Allocator.AllocObj() instead
// to benefit from bump allocation and sync.Pool reuse.
func NewJSObject() *JSObject {
	obj := &JSObject{}
	obj.Shape = EmptyShape
	obj.Prototype = ObjectPrototype
	obj.ConstructorName = "Object"
	return obj
}

// NewJSObjectWithShape creates an object with the given Shape.
func NewJSObjectWithShape(shape *Shape) *JSObject {
	obj := NewJSObject()
	obj.Shape = shape
	if shape.PropertyCount > 0 {
		obj.growProperties(shape.PropertyCount)
	}
	return obj
}

// NewObjectWithCapacity creates an object with a pre-sized Properties slice.
// Use when the exact property count is known (e.g., compiler-emitted object literals).
func NewObjectWithCapacity(shape *Shape, n int) *JSObject {
	obj := NewJSObject()
	obj.Shape = shape
	if n > 0 {
		obj.growProperties(n)
	}
	return obj
}

// Get returns the value of a named property, walking the prototype chain.
func (obj *JSObject) Get(name string) JSValue {
	if obj == nil {
		return Undefined
	}
	if val, ok := obj.getOwn(name); ok {
		return val
	}
	// Virtual property interceptor (TypedArray indexed access, Proxy get trap).
	if obj.interceptor != nil && obj.interceptor.OnPropertyGet != nil {
		if val, ok := obj.interceptor.OnPropertyGet(obj, name); ok {
			return val
		}
	}
	// Walk prototype chain.
	proto := obj.Prototype
	for proto != nil {
		if val, ok := proto.getOwn(name); ok {
			return val
		}
		proto = proto.Prototype
	}
	return Undefined
}

// getOwn returns an own property value, or (Undefined, false) if not found.
func (obj *JSObject) getOwn(name string) (JSValue, bool) {
	// Fast path: last-lookup cache hit avoids Shape offset resolution.
	if obj.lastLookupValid && obj.lastLookupName == name {
		return obj.lastLookupValue, true
	}
	if obj.Shape.IsDictionary {
		if val, ok := obj.Dictionary[name]; ok {
			return val, true
		}
		return Undefined, false
	}
	offset := obj.Shape.GetOffset(name)
	if offset < 0 || offset >= obj.propLen() {
		return Undefined, false
	}
	val := obj.propAt(offset)

	// Accessor property: if getter function is stored, call it.
	if attr := obj.Shape.GetAttr(name); attr&AttrAccessor != 0 && val.IsObject() && val.ObjVal.IsCallable() {
		result := val.ObjVal.Call(obj, nil)
		// Don't cache accessor results — they may be dynamic.
		return result, true
	}

	// Cache the result for subsequent lookups of the same property name.
	obj.lastLookupName = name
	obj.lastLookupValue = val
	obj.lastLookupValid = true
	return val, true
}

// getOwnByOffset returns an own property value by direct offset, skipping
// the Shape map lookup. Used by the IC fast path when shape already matches.
// Returns (Undefined, false) if the offset is out of bounds.
func (obj *JSObject) getOwnByOffset(offset int) (JSValue, bool) {
	if offset < 0 || offset >= obj.propLen() {
		return Undefined, false
	}
	return obj.propAt(offset), true
}

// Set assigns a value to a named property, creating a new Shape transition if needed.
// If OnPropertySet is set and returns true, default storage is skipped.
func (obj *JSObject) Set(name string, value JSValue) {
	if obj.IsFrozen() {
		return
	}
	if obj.IsSealed() {
		_, exists := obj.getOwn(name)
		if !exists {
			return // can't add new properties to sealed objects
		}
	}
	// Invalidate last-lookup cache on any mutation.
	obj.lastLookupValid = false
	if obj.interceptor != nil && obj.interceptor.OnPropertySet != nil && obj.interceptor.OnPropertySet(obj, name, value) {
		return
	}
	// Accessor property: if the property is an accessor, treat assignment as setter call.
	if attr := obj.Shape.GetAttr(name); attr&AttrAccessor != 0 {
		// Call the stored function as a setter with the new value.
		existing := obj.propAt(obj.Shape.GetOffset(name))
		if existing.IsObject() && existing.ObjVal.IsCallable() {
			existing.ObjVal.Call(obj, []JSValue{value})
		}
		return
	}
	if obj.Shape.IsDictionary {
		obj.Dictionary[name] = value
		return
	}
	offset := obj.Shape.GetOffset(name)
	if offset >= 0 {
		obj.propSet(offset, value)
		return
	}
	// Property doesn't exist — transition to a new Shape.
	obj.Shape = obj.Shape.AddProperty(name)
	// Grow backing store if needed.
	if obj.propLen() < obj.Shape.PropertyCount {
		obj.growProperties(obj.Shape.PropertyCount)
	}
	newOffset := obj.Shape.GetOffset(name)
	if newOffset >= 0 && newOffset < obj.propLen() {
		obj.propSet(newOffset, value)
	}
}

// SetAccessor defines an accessor property (getter/setter) on the object.
// The getter function is stored in the property slot. On get, it is called
// with no arguments. On set, the same function is called with the new value
// as argument (setter mode). For combined getter+setter properties, call
// SetAccessor for the getter first, then SetAccessorSetter for the setter.
func (obj *JSObject) SetAccessor(name string, getter JSValue) {
	obj.lastLookupValid = false
	if obj.Shape.IsDictionary {
		obj.Dictionary[name] = getter
		return
	}
	offset := obj.Shape.GetOffset(name)
	if offset < 0 {
		// New property — transition with accessor attributes.
		obj.Shape = obj.Shape.AddPropertyWithAttr(name, AttrAccessorDefault)
		if obj.propLen() < obj.Shape.PropertyCount {
			obj.growProperties(obj.Shape.PropertyCount)
		}
		offset = obj.Shape.GetOffset(name)
	}
	if offset >= 0 && offset < obj.propLen() {
		obj.propSet(offset, getter)
	}
}

// Has returns true if the named property exists anywhere in the prototype chain.
func (obj *JSObject) Has(name string) bool {
	// Proxy intercept: handler.has(target, prop)
	if obj.interceptor != nil && obj.interceptor.OnHas != nil {
		if handled, result := obj.interceptor.OnHas(obj, name); handled {
			return result
		}
	}
	if _, ok := obj.getOwn(name); ok {
		return true
	}
	proto := obj.Prototype
	for proto != nil {
		if _, ok := proto.getOwn(name); ok {
			return true
		}
		proto = proto.Prototype
	}
	return false
}

// Delete removes an own property. Returns true if deleted or property doesn't exist,
// false only when the property is non-configurable (strict mode would throw).
// Per ECMAScript §13.5.1.2: delete returns true for non-existent properties.
func (obj *JSObject) Delete(name string) bool {
	if obj.IsFrozen() || obj.IsSealed() {
		return false
	}
	// Proxy intercept: handler.deleteProperty(target, prop)
	if obj.interceptor != nil && obj.interceptor.OnDelete != nil {
		if handled, result := obj.interceptor.OnDelete(obj, name); handled {
			return result
		}
	}
	obj.lastLookupValid = false
	if obj.Shape.IsDictionary {
		delete(obj.Dictionary, name)
		return true
	}
	offset := obj.Shape.GetOffset(name)
	if offset >= 0 && offset < obj.propLen() {
		attr := obj.Shape.GetAttr(name)
		if attr&AttrConfigurable == 0 {
			return false // not configurable (V8 DONT_DELETE)
		}
		// Convert to dictionary mode and remove the property so Has() returns false.
		obj.Shape = obj.Shape.ConvertToDictionary()
		// Copy existing properties to dictionary.
		obj.Dictionary = make(map[string]JSValue, obj.Shape.PropertyCount)
		for propName, entry := range obj.Shape.Properties {
			if entry.Offset >= 0 && entry.Offset < obj.propLen() {
				obj.Dictionary[propName] = obj.propAt(entry.Offset)
			}
		}
		// Clear property stores — dictionary mode uses obj.Dictionary for all access.
		obj.Properties = nil
		obj.inlineCount = 0
		// Remove the target property.
		delete(obj.Dictionary, name)
		return true
	}
	// Property doesn't exist — returns true per spec.
	return true
}

// ToPrimitiveDefault returns the default primitive value of this object.
// For plain objects, this is "[object Object]".
func (obj *JSObject) ToPrimitiveDefault() JSValue {
	if obj == nil {
		return Null
	}
	return NewString(obj.ToString())
}

// ToPrimitiveNumber returns the numeric primitive (ToPrimitive with hint "number").
func (obj *JSObject) ToPrimitiveNumber() JSValue {
	if obj == nil {
		return Null
	}
	return NewString(obj.ToString())
}

// ToPrimitiveString returns the string primitive.
func (obj *JSObject) ToPrimitiveString() JSValue {
	if obj == nil {
		return Null
	}
	return NewString(obj.ToString())
}

// ToString returns a debug-friendly string for the object.
func (obj *JSObject) ToString() string {
	if obj == nil {
		return "null"
	}
	if obj.ConstructorName != "" {
		return fmt.Sprintf("[object %s]", obj.ConstructorName)
	}
	return "[object Object]"
}

// isCallable returns true if this object can be called as a function.
func (obj *JSObject) isCallable() bool {
	return obj.CallFunc != nil || obj.Bytecode != nil
}

// IsCallable returns true if this object can be called as a function (exported).
func (obj *JSObject) IsCallable() bool {
	return obj.isCallable()
}

// Call invokes this object as a function.
func (obj *JSObject) Call(this *JSObject, args []JSValue) JSValue {
	if obj.CallFunc != nil {
		return obj.CallFunc(this, args)
	}
	if obj.Bytecode != nil {
		return callBytecodeFunction(obj.Bytecode, this, args)
	}
	return Undefined
}

// callBytecodeFunction executes bytecode with given arguments and this binding.
func callBytecodeFunction(bf *BytecodeFunction, thisArg *JSObject, args []JSValue) JSValue {
	// Create a minimal VM to execute the bytecode properly.
	vm := &VM{
		alloc:      NewAllocator(),
		globals:    NewGlobalStore(),
		calltrack:  &CallTracker{},
		console:    NewConsole(),
		registry:   NewRegistry(),
	}
	vm.RegisterBuiltins()
	regs := vm.allocRegs(bf.NumRegisters)
	defer vm.freeRegs(regs)
	frame := &VMFrame{
		Func:      bf,
		Regs:      regs,
		HandlerPC: -1,
	}
	if thisArg != nil {
		frame.This = NewObject(thisArg)
	} else {
		frame.This = Undefined
	}
	for i, arg := range args {
		if i < len(frame.Regs) {
			frame.Regs[i] = arg
		}
	}
	for frame.PC < len(frame.Func.Instructions) {
		if vm.executeOne(frame) {
			if frame.Thrown.Tag != TagUndefined && frame.HandlerPC >= 0 {
				frame.PC = frame.HandlerPC
				frame.HandlerPC = -1
				continue
			}
			return frame.Acc
		}
	}
	return frame.Acc
}

// ObjectPrototype is the root prototype for all plain objects.
var ObjectPrototype = &JSObject{
	Shape:           EmptyShape,
	ConstructorName: "Object",
}

// ArrayPrototype holds the Array.prototype object (populated by RegisterBuiltins).
var ArrayPrototype *JSObject

// FunctionPrototype holds the Function.prototype object with call/apply/bind/toString.
var FunctionPrototype *JSObject
