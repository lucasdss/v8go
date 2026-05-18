//go:build !amd64

// Package jit — icpatch: Runtime Inline Cache Patching for ARM64.
//
// When a monomorphic IC slot is observed in the interpreter (via FeedbackVector),
// the JIT code's corresponding NOP sled is patched at runtime to emit a guard
// sequence: LDR shape, CMP guard, B.NE slowPath, LDR offset.
//
// The IC slot layout in the code buffer (16 bytes, 4 instructions):
//
//	offset+0:  LDR  x9, [x8, #shapeOffset]   // load shape from object
//	offset+4:  CMP  x9, x10                   // compare with cached shape
//	offset+8:  B.NE +4                        // miss → skip next instruction
//	offset+12: LDR  x0, [x8, #propOffset]     // fast path: load property
//
// Register convention before the IC sled:
//
//	x8  = object pointer (JSObject*)
//	x10 = expected Shape pointer (set up by caller)
//
// On hit, x0 holds the property value. On miss, execution continues at offset+16.
//
// On Apple Silicon, the RW mapping is used for writes. The CPU's instruction
// cache is flushed after patching by toggling pthread_jit_write_protect_np.
package jit

import (
	"sync"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// codeBufRegistry maps rxAddr → *CodeBuf for runtime IC patching.
var codeBufRegistry sync.Map

// icSlotOffsets maps rxAddr → []int (slotIdx → byte offset within code buffer).
var icSlotOffsets sync.Map

// RegisterCodeBuf registers a code buffer for runtime IC patching.
// With dual-mapping, no Seal is needed — writes always go through the RW
// mapping, and execution through the RX mapping.
func RegisterCodeBuf(rxAddr uintptr, buf *CodeBuf, slotOffsets []int) {
	buf.Commit() // flush I-cache on platforms that need it (ARM64 Linux)
	codeBufRegistry.Store(rxAddr, buf)
	if len(slotOffsets) > 0 {
		icSlotOffsets.Store(rxAddr, slotOffsets)
	}
}

// patchICShapeLoad emits the 16-byte MOVZ/MOVK sequence to load shapePtr into x10.
func patchICShapeLoad(buf *CodeBuf, slotOffset int, shapePtr uintptr) {
	s := uint64(shapePtr)
	buf.PatchUint32LE(slotOffset, 0xD2800000|uint32(s&0xFFFF)<<5|uint32(REG_R10))
	buf.PatchUint32LE(slotOffset+4, 0xF2800000|uint32(16<<17)|uint32((s>>16)&0xFFFF)<<5|uint32(REG_R10))
	buf.PatchUint32LE(slotOffset+8, 0xF2800000|uint32(32<<17)|uint32((s>>32)&0xFFFF)<<5|uint32(REG_R10))
	buf.PatchUint32LE(slotOffset+12, 0xF2800000|uint32(48<<17)|uint32((s>>48)&0xFFFF)<<5|uint32(REG_R10))
}

// patchICGuardAccess emits the 16-byte guard+access sequence (LDR/CMP/B.NE/load-or-store).
// isStore=true emits STR x1; false emits LDR x0.
func patchICGuardAccess(buf *CodeBuf, slotOffset int, objShapeOffset, propOffset int, isStore bool) {
	buf.PatchUint32LE(slotOffset+16, 0xF9400000|uint32(objShapeOffset/8)<<10|uint32(REG_R8)<<5|uint32(REG_R9))
	buf.PatchUint32LE(slotOffset+20, 0xEB00001F|uint32(REG_R9)<<16|uint32(REG_R10)<<5)
	buf.PatchUint32LE(slotOffset+24, 0x54000001|uint32(4<<5))
	pbo := propOffset * 64
	if isStore {
		buf.PatchUint32LE(slotOffset+28, 0xF9000000|uint32(pbo/8)<<10|uint32(REG_R8)<<5|uint32(REG_R1))
	} else {
		buf.PatchUint32LE(slotOffset+28, 0xF9400000|uint32(pbo/8)<<10|uint32(REG_R8)<<5|uint32(REG_R0))
	}
}

// PatchICSlot patches an inline cache slot for load operations.
// Full 32-byte layout: shape load (16B) + guard+LDR (16B).
func PatchICSlot(buf *CodeBuf, slotOffset int, shapePtr uintptr, propOffset int, objShapeOffset int) {
	patchICShapeLoad(buf, slotOffset, shapePtr)
	patchICGuardAccess(buf, slotOffset, objShapeOffset, propOffset, false)
	buf.Commit()
}

// resolveICSlot looks up the CodeBuf and byte offset for the given slot.
// Returns (buf, slotByteOff, true) on success, or (nil, 0, false) on failure.
func resolveICSlot(rxAddr uintptr, slotIdx int) (*CodeBuf, int, bool) {
	bufVal, ok := codeBufRegistry.Load(rxAddr)
	if !ok {
		return nil, 0, false
	}
	buf := bufVal.(*CodeBuf)
	offsetsVal, ok := icSlotOffsets.Load(rxAddr)
	if !ok {
		return nil, 0, false
	}
	offsets := offsetsVal.([]int)
	if slotIdx >= len(offsets) || offsets[slotIdx] < 0 {
		return nil, 0, false
	}
	return buf, offsets[slotIdx], true
}

// patchICSlotCommon resolves and patches a slot with the given patch function.
func patchICSlotCommon(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int,
	patchFn func(*CodeBuf, int, uintptr, int, int)) {
	buf, slotByteOff, ok := resolveICSlot(rxAddr, slotIdx)
	if !ok {
		return
	}
	objShapeOffset := int(unsafe.Offsetof(js.JSObject{}.Shape))
	patchFn(buf, slotByteOff, uintptr(shapePtr), propOffset, objShapeOffset)
}

// PatchICSlotAt patches the JIT IC slot from the interpreter's feedback.
// With dual-mapping, writes go directly through the RW mapping — no
// write-protect toggle is needed.
func PatchICSlotAt(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int) {
	patchICSlotCommon(rxAddr, slotIdx, shapePtr, propOffset, PatchICSlot)
}

// PatchPolymorphicICSlotAt patches a JIT IC slot with 2-4 polymorphic shapes.
// Patches the first (most recent) shape monomorphically for best-effort fast path;
// the remaining shapes are handled by the interpreter slow path on miss.
// If count > 4, transitions to megamorphic (permanent jump to slow path).
func PatchPolymorphicICSlotAt(rxAddr uintptr, slotIdx int, shapes []unsafe.Pointer, offsets []int) {
	buf, slotByteOff, ok := resolveICSlot(rxAddr, slotIdx)
	if !ok {
		return
	}
	if len(shapes) > 4 || len(shapes) == 0 {
		PatchMegamorphicICSlot(buf, slotByteOff)
		return
	}
	// Patch most recent shape (index 0) as monomorphic fast path.
	objShapeOffset := int(unsafe.Offsetof(js.JSObject{}.Shape))
	PatchICSlot(buf, slotByteOff, uintptr(shapes[0]), offsets[0], objShapeOffset)
}

// PatchMegamorphicICSlotAt patches a slot to permanently jump to the slow path.
func PatchMegamorphicICSlotAt(rxAddr uintptr, slotIdx int) {
	buf, slotByteOff, ok := resolveICSlot(rxAddr, slotIdx)
	if !ok {
		return
	}
	PatchMegamorphicICSlot(buf, slotByteOff)
}

// PatchMegamorphicICSlot patches the IC slot to an unconditional branch
// to the slow path, bypassing all shape checks permanently.
// The unconditional B jumps past the 32-byte sled (8 instructions).
func PatchMegamorphicICSlot(buf *CodeBuf, slotOffset int) {
	// B +32: jump over the entire 32-byte IC sled to the slow path handler.
	// The sled is 32 bytes = 8 instructions; B immediate is in instructions (32/4=8).
	buf.PatchUint32LE(slotOffset, 0x14000000|uint32(8)&0x3FFFFFF)
	// NOP out the rest of the sled so stale code doesn't execute.
	for i := 4; i < 32; i += 4 {
		buf.PatchUint32LE(slotOffset+i, 0xD503201F)
	}
	buf.Commit()
}

// EmitICSlot emits a 16-byte NOP sled for an inline cache slot.
// The sled is later patched by PatchICSlot when monomorphic feedback is available.
func EmitICSlot(as *Assembler) int {
	offset := as.Pos()
	as.NOP() // slotOffset+0: will become LDR shape
	as.NOP() // slotOffset+4: will become CMP
	as.NOP() // slotOffset+8: will become B.NE
	as.NOP() // slotOffset+12: will become LDR offset
	return offset
}

// Pos returns the current write position from the assembler's buffer.
func (a *Assembler) Pos() int {
	return a.buf.Pos()
}

// PatchICSlotStore patches an IC slot for store operations.
// Same 32-byte layout as PatchICSlot, but emits STR x1 instead of LDR x0.
func PatchICSlotStore(buf *CodeBuf, slotOffset int, shapePtr uintptr, propOffset int, objShapeOffset int) {
	patchICShapeLoad(buf, slotOffset, shapePtr)
	patchICGuardAccess(buf, slotOffset, objShapeOffset, propOffset, true)
	buf.Commit()
}

// PatchICSlotStoreAt patches a store IC slot from runtime feedback.
func PatchICSlotStoreAt(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int) {
	patchICSlotCommon(rxAddr, slotIdx, shapePtr, propOffset, PatchICSlotStore)
}

func init() {
	// Register the IC patch hook with the VM to avoid import cycles.
	js.PatchICSlotHook = PatchICSlotAt
	js.PatchICSlotStoreHook = PatchICSlotStoreAt
	js.PatchPolyICSlotHook = PatchPolymorphicICSlotAt
	js.PatchMegaICSlotHook = PatchMegamorphicICSlotAt
	// On Darwin, jitWriteProtect toggles pthread_jit_write_protect_np
	// which is required for MAP_JIT page execution on Apple Silicon.
	// On Linux with true dual-mapping, this is a no-op.
	js.JITProtectHook = jitWriteProtect
}
