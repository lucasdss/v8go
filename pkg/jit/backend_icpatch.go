//go:build !amd64

package jit

import "unsafe"

// PatchMonomorphic patches a JIT IC slot with a single (monomorphic) shape guard.
func (b *DefaultBackend) PatchMonomorphic(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int) {
	PatchICSlotAt(rxAddr, slotIdx, shapePtr, propOffset)
}

// PatchPolymorphic patches a JIT IC slot with 2-4 shape guards (polymorphic).
func (b *DefaultBackend) PatchPolymorphic(rxAddr uintptr, slotIdx int, shapes []unsafe.Pointer, offsets []int) {
	PatchPolymorphicICSlotAt(rxAddr, slotIdx, shapes, offsets)
}

// PatchMegamorphic patches a JIT IC slot to permanently jump to the slow path.
func (b *DefaultBackend) PatchMegamorphic(rxAddr uintptr, slotIdx int) {
	PatchMegamorphicICSlotAt(rxAddr, slotIdx)
}

// PatchStore patches a JIT IC slot for store operations with a monomorphic guard.
func (b *DefaultBackend) PatchStore(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int) {
	PatchICSlotStoreAt(rxAddr, slotIdx, shapePtr, propOffset)
}
