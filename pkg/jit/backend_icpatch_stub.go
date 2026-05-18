//go:build amd64

package jit

import "unsafe"

// PatchMonomorphic is a no-op on amd64 (IC patching not yet supported).
func (b *DefaultBackend) PatchMonomorphic(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int) {
}

// PatchPolymorphic is a no-op on amd64.
func (b *DefaultBackend) PatchPolymorphic(rxAddr uintptr, slotIdx int, shapes []unsafe.Pointer, offsets []int) {
}

// PatchMegamorphic is a no-op on amd64.
func (b *DefaultBackend) PatchMegamorphic(rxAddr uintptr, slotIdx int) {
}

// PatchStore is a no-op on amd64.
func (b *DefaultBackend) PatchStore(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int) {
}
