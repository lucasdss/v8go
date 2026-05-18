// ic.go — Inline Caching for fast property access.
//
// Implements the V8-style Inline Caching mechanism. Each function's
// FeedbackVector tracks the Shape and offset seen at each property
// access site, enabling fast-path lookups when the Shape matches
// and falling back to dictionary lookup otherwise.
//
// States:
//   - Uninitialized (slot unused)
//   - Monomorphic (one Shape cached)
//   - Polymorphic (up to 4 Shapes cached)
//   - Megamorphic (too many Shapes → always slow path)
package js

import "unsafe"

// ICState represents the state of an inline cache slot.
type ICState uint8

const (
	ICUninitialized ICState = iota
	ICMonomorphic
	ICPolymorphic
	ICMegamorphic
)

// ICSlot holds cached type feedback for a single property access site.
type ICSlot struct {
	State ICState
	// Interned property name (stored on first access, avoids ToString alloc on fast path).
	Name string
	// For monomorphic: one shape + offset.
	Shape  *Shape
	Offset int
	// For polymorphic: multiple shapes (up to 4).
	Shapes  [4]*Shape
	Offsets [4]int
	Count   int // number of entries in polymorphic cache
	// PolyShapes/PolyOffsets hold the same data as Shapes/Offsets but as
	// unsafe.Pointer slices for direct JIT IC slot patching without allocation.
	PolyShapes  [4]unsafe.Pointer
	PolyOffsets [4]int
	PolyCount   int
	// Megamorphic LRU cache: last-seen shape+offset for quick re-check.
	MegaShape  *Shape
	MegaOffset int
	// Feedback recording for JIT recompilation.
	HitCount    int                // number of times this slot was hit
	Patched     bool               // true if JIT IC slot has been patched
	ObservedTag TypeTag            // dominant operand type for binary ops
	Callee      *BytecodeFunction  // callee target for call sites
}

// FeedbackVector holds inline caching data for all property access sites
// in a compiled function. Each site gets a slot index.
type FeedbackVector struct {
	Slots []ICSlot
}

// NewFeedbackVector creates a FeedbackVector with the given number of slots.
// Returns nil if numSlots <= 0 (no feedback needed).
func NewFeedbackVector(numSlots int) *FeedbackVector {
	if numSlots <= 0 {
		return nil
	}
	return &FeedbackVector{
		Slots: make([]ICSlot, numSlots),
	}
}

// Reset clears all inline cache slots back to Uninitialized state.
// Called on deoptimization to force fresh type feedback collection
// before the next JIT recompilation.
func (fv *FeedbackVector) Reset() {
	for i := range fv.Slots {
		fv.Slots[i].State = ICUninitialized
		fv.Slots[i].Shape = nil
		fv.Slots[i].Offset = 0
		fv.Slots[i].HitCount = 0
		fv.Slots[i].Callee = nil
	}
}

// LoadIC performs a property load with inline caching.
// name should be the interned property name (from the constant pool).
// Returns the property value.
func (fv *FeedbackVector) LoadIC(slotIdx int, obj *JSObject, name string) JSValue {
	if obj == nil {
		return Undefined
	}
	if slotIdx >= len(fv.Slots) {
		return obj.Get(name)
	}

	slot := &fv.Slots[slotIdx]

	switch slot.State {
	case ICUninitialized:
		// Store interned name for future fast-path use.
		slot.Name = name
		offset := obj.Shape.GetOffset(name)
		if offset >= 0 && offset < obj.propLen() {
			slot.Shape = obj.Shape
			slot.Offset = offset
			slot.State = ICMonomorphic
			return obj.propAt(offset)
		}
		// Property not found inline (prototype or dictionary). Try slow path.
		// Leave slot uninitialized — future accesses may hit inline.
		return obj.Get(name)

	case ICMonomorphic:
		// Fast path: shape matches — direct offset access, zero allocations.
		if obj.Shape == slot.Shape {
			if slot.Offset < obj.propLen() {
				return obj.propAt(slot.Offset)
			}
			// Properties array shrunk (e.g., delete). Fall through to miss.
		}
		// Shape miss: promote to polymorphic.
		slot.Shapes[0] = slot.Shape
		slot.Offsets[0] = slot.Offset
		slot.Count = 1
		slot.PolyShapes[0] = unsafe.Pointer(slot.Shape)
		slot.PolyOffsets[0] = slot.Offset
		slot.PolyCount = 1
		offset := obj.Shape.GetOffset(slot.Name)
		if offset >= 0 && offset < obj.propLen() {
			slot.Shapes[1] = obj.Shape
			slot.Offsets[1] = offset
			slot.Count = 2
			slot.PolyShapes[1] = unsafe.Pointer(obj.Shape)
			slot.PolyOffsets[1] = offset
			slot.PolyCount = 2
			slot.State = ICPolymorphic
			// LRU: move new shape to position 0 for faster future lookups.
			return obj.propAt(offset)
		}
		slot.State = ICPolymorphic
		return obj.Get(name)

	case ICPolymorphic:
		// Check cached shapes (most-recent first for LRU-like behavior).
		for i := 0; i < slot.Count; i++ {
			if obj.Shape == slot.Shapes[i] {
				if slot.Offsets[i] < obj.propLen() {
					return obj.propAt(slot.Offsets[i])
				}
				// Stale offset, fall through.
				break
			}
		}
		// Add new shape if there's room.
		if slot.Count < 4 {
			offset := obj.Shape.GetOffset(slot.Name)
			if offset >= 0 && offset < obj.propLen() {
				slot.Shapes[slot.Count] = obj.Shape
				slot.Offsets[slot.Count] = offset
				slot.PolyShapes[slot.Count] = unsafe.Pointer(obj.Shape)
				slot.PolyOffsets[slot.Count] = offset
				slot.PolyCount = slot.Count + 1
				slot.Count++
				return obj.propAt(offset)
			}
		} else {
			// Too many shapes → megamorphic. Cache last shape for re-check.
			slot.MegaShape = obj.Shape
			offset := obj.Shape.GetOffset(slot.Name)
			if offset >= 0 {
				slot.MegaOffset = offset
			}
			slot.State = ICMegamorphic
		}
		return obj.Get(name)

	case ICMegamorphic:
		// Try the megamorphic LRU cache before full slow path.
		if obj.Shape == slot.MegaShape && slot.MegaOffset < obj.propLen() {
			return obj.propAt(slot.MegaOffset)
		}
		// Update megamorphic cache on miss.
		offset := obj.Shape.GetOffset(slot.Name)
		if offset >= 0 && offset < obj.propLen() {
			slot.MegaShape = obj.Shape
			slot.MegaOffset = offset
			return obj.propAt(offset)
		}
		return obj.Get(name)
	}

	return Undefined
}

// StoreIC performs a property store with inline caching.
func (fv *FeedbackVector) StoreIC(slotIdx int, obj *JSObject, name string, value JSValue) {
	if obj == nil {
		return
	}
	if slotIdx >= len(fv.Slots) {
		obj.Set(name, value)
		return
	}

	slot := &fv.Slots[slotIdx]

	switch slot.State {
	case ICUninitialized:
		slot.Name = name
		obj.Set(name, value)
		slot.Shape = obj.Shape
		offset := obj.Shape.GetOffset(name)
		if offset >= 0 {
			slot.Offset = offset
		}
		slot.State = ICMonomorphic

	case ICMonomorphic:
		if obj.Shape == slot.Shape {
			// Fast path: direct offset write.
			if !obj.IsFrozen() && !obj.IsSealed() && slot.Offset < obj.propLen() {
				obj.propSet(slot.Offset, value)
				return
			}
		}
		obj.Set(name, value)
		// Update cached shape after Set (shape may have changed).
		slot.Shape = obj.Shape
		offset := obj.Shape.GetOffset(slot.Name)
		if offset >= 0 {
			slot.Offset = offset
		}

	default:
		// Polymorphic/megamorphic: just write, update megamorphic cache.
		obj.Set(name, value)
		if slot.State == ICMegamorphic {
			slot.MegaShape = obj.Shape
			offset := obj.Shape.GetOffset(slot.Name)
			if offset >= 0 {
				slot.MegaOffset = offset
			}
		}
	}
}
