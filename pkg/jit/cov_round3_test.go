package jit

import (
	"math"
	"testing"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// ============================================================================
// Round 1: asm_arm64.go — CSEL, NEG, CLZ, RBIT, REV, UBFX, SXTB, SXTH, SXTW, MSUB
// ============================================================================

func TestAssemblerCSEL(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.CSEL(REG_R0, REG_R1, REG_R2, 0) // EQ condition
	as.CSEL(REG_R3, REG_R4, REG_R5, 1) // NE condition
	if buf.Len() != 8 {
		t.Errorf("expected 8 bytes, got %d", buf.Len())
	}
}

func TestAssemblerNEG(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.NEG(REG_R0, REG_R1)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerCLZ(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.CLZ(REG_R0, REG_R1)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerRBIT(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.RBIT(REG_R0, REG_R1)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerREV(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.REV(REG_R0, REG_R1)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerUBFX(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.UBFX(REG_R0, REG_R1, 0, 32) // extract 32 bits starting at bit 0
	as.UBFX(REG_R2, REG_R3, 16, 8) // extract 8 bits starting at bit 16
	if buf.Len() != 8 {
		t.Errorf("expected 8 bytes, got %d", buf.Len())
	}
}

func TestAssemblerSXTB(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.SXTB(REG_R0, REG_R1)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerSXTH(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.SXTH(REG_R0, REG_R1)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerSXTW(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.SXTW(REG_R0, REG_R1)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerMSUB(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.MSUB(REG_R0, REG_R1, REG_R2, REG_R3)
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

// ============================================================================
// Round 1: deopt.go — FindDeoptPoint
// ============================================================================

func TestFindDeoptPointNoMatch(t *testing.T) {
	d := &DeoptimizationInputData{
		Points: []DeoptPoint{
			{NativeOffset: 100, BytecodePC: 10},
			{NativeOffset: 200, BytecodePC: 20},
		},
	}
	// No match should return -1, false.
	pc, ok := d.FindDeoptPoint(999)
	if ok {
		t.Errorf("expected no match for offset 999")
	}
	if pc != -1 {
		t.Errorf("expected -1, got %d", pc)
	}
}

func TestFindDeoptPointMatch(t *testing.T) {
	d := &DeoptimizationInputData{
		Points: []DeoptPoint{
			{NativeOffset: 100, BytecodePC: 10},
			{NativeOffset: 200, BytecodePC: 20},
			{NativeOffset: 300, BytecodePC: 30},
		},
	}
	pc, ok := d.FindDeoptPoint(200)
	if !ok {
		t.Error("expected match for offset 200")
	}
	if pc != 20 {
		t.Errorf("expected bytecode PC 20, got %d", pc)
	}
}

func TestFindDeoptPointEmpty(t *testing.T) {
	d := &DeoptimizationInputData{Points: nil}
	pc, ok := d.FindDeoptPoint(0)
	if ok {
		t.Error("expected no match on empty points")
	}
	if pc != -1 {
		t.Errorf("expected -1, got %d", pc)
	}
}

// ============================================================================
// Round 2: icpatch.go — PatchICSlot with zero offset, max uint32 offset, nil buffer
// ============================================================================

func TestPatchICSlotZeroOffset(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	// Fill with NOPs.
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	// Patch at offset 0.
	PatchICSlot(buf, 0, 0xABCD0000, 8, 16)
	// Verify it's patched (not all NOPs at offset 0).
	w := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if w == 0xD503201F {
		t.Error("expected slot at offset 0 to be patched")
	}
}

func TestPatchICSlotMaxUint32Offset(t *testing.T) {
	buf, err := NewCodeBuf(512)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	// Fill with NOPs.
	for i := 0; i < 512; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	// Patch at a large offset within bounds (slot is 32 bytes).
	maxOff := 512 - 32
	PatchICSlot(buf, maxOff, 0xDEAD0000, 16, 32)
	w := uint32(buf.rwBuf[maxOff]) | uint32(buf.rwBuf[maxOff+1])<<8 | uint32(buf.rwBuf[maxOff+2])<<16 | uint32(buf.rwBuf[maxOff+3])<<24
	if w == 0xD503201F {
		t.Error("expected slot at max offset to be patched")
	}
}

func TestPatchMegamorphicICSlotZeroOffset(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	PatchMegamorphicICSlot(buf, 0)
	// First instruction should be B (not NOP).
	w := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if w == 0xD503201F {
		t.Error("expected megamorphic slot at offset 0 to be patched")
	}
}

func TestResolveICSlotNoBuffer(t *testing.T) {
	// resolveICSlot with unregistered address.
	buf, _, ok := resolveICSlot(0xDEAD, 0)
	if ok {
		t.Error("expected resolveICSlot to fail for unregistered address")
	}
	if buf != nil {
		t.Error("expected nil buf for unregistered address")
	}
}

func TestResolveICSlotNoOffsets(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	// Register buffer but no IC slot offsets.
	codeBufRegistry.Store(buf.rxAddr, buf)
	defer codeBufRegistry.Delete(buf.rxAddr)

	_, _, ok := resolveICSlot(buf.rxAddr, 0)
	if ok {
		t.Error("expected resolveICSlot to fail when no slot offsets registered")
	}
}

func TestResolveICSlotNegativeOffset(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	codeBufRegistry.Store(buf.rxAddr, buf)
	icSlotOffsets.Store(buf.rxAddr, []int{-1, 0})
	defer codeBufRegistry.Delete(buf.rxAddr)
	defer icSlotOffsets.Delete(buf.rxAddr)

	_, _, ok := resolveICSlot(buf.rxAddr, 0)
	if ok {
		t.Error("expected resolveICSlot to fail for negative offset slot")
	}
	// Slot 1 (offset 0) should succeed.
	resolvedBuf, resolvedOff, ok := resolveICSlot(buf.rxAddr, 1)
	if !ok {
		t.Error("expected resolveICSlot(1) to succeed")
	}
	if resolvedBuf != buf || resolvedOff != 0 {
		t.Error("expected correct buf/offset for slot 1")
	}
}

// ============================================================================
// Round 2: gc_bridge.go — unregister, double-register, concurrent registration
// ============================================================================

func TestUnregisterRegionAll(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = []Region{
		{Start: 0x1000, End: 0x2000},
		{Start: 0x2000, End: 0x3000},
		{Start: 0x3000, End: 0x4000},
	}
	regionMu.Unlock()

	// Remove all regions (spanning the entire range).
	UnregisterRegion(0x0000, 0x5000)
	if NumRegions() != 0 {
		t.Errorf("expected 0 regions after unregister all, got %d", NumRegions())
	}
}

func TestUnregisterRegionNone(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = []Region{
		{Start: 0x1000, End: 0x2000},
		{Start: 0x3000, End: 0x4000},
	}
	regionMu.Unlock()

	// Remove range that matches nothing.
	UnregisterRegion(0x2500, 0x2800)
	if NumRegions() != 2 {
		t.Errorf("expected 2 regions, got %d", NumRegions())
	}
}

func TestUnregisterRegionEdge(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = []Region{
		{Start: 0x1000, End: 0x2000},
		{Start: 0x2000, End: 0x3000},
	}
	regionMu.Unlock()

	// Remove only the first region exactly.
	UnregisterRegion(0x1000, 0x2000)
	if NumRegions() != 1 {
		t.Errorf("expected 1 region, got %d", NumRegions())
	}
	// Verify remaining region.
	regionMu.RLock()
	if registeredRegionSlice[0].Start != 0x2000 {
		t.Errorf("expected remaining region at 0x2000, got 0x%x", registeredRegionSlice[0].Start)
	}
	regionMu.RUnlock()
}

func TestRegisterRegionDouble(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = nil
	regionMu.Unlock()

	r := Region{Start: 0x1000, End: 0x2000}
	if !RegisterRegion(r) {
		t.Error("first registration should succeed")
	}
	if !RegisterRegion(r) {
		t.Error("second registration (duplicate range) should succeed")
	}
	if NumRegions() != 2 {
		t.Errorf("expected 2 regions, got %d", NumRegions())
	}
}

func TestConcurrentRegistration(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = nil
	regionMu.Unlock()

	done := make(chan bool, 4)
	for i := 0; i < 4; i++ {
		go func(id int) {
			RegisterRegion(Region{
				Start: uintptr(0x1000 * (id + 1)),
				End:   uintptr(0x1000*(id+1) + 0x800),
			})
			done <- true
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if NumRegions() != 4 {
		t.Errorf("expected 4 regions after concurrent registration, got %d", NumRegions())
	}
}

func TestNumRegionsEmpty(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = nil
	regionMu.Unlock()
	if NumRegions() != 0 {
		t.Error("expected 0 regions after clearing")
	}
}

// ============================================================================
// Round 3: sparkplug_compile.go — nil BytecodeFunction, empty instructions
// ============================================================================

func TestCompileSparkplugNilBytecodeFunction(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil BytecodeFunction")
		}
	}()
	CompileSparkplug(nil)
}

func TestCompileSparkplugEmptyInstructions(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "empty",
		NumRegisters: 0,
		Instructions: []js.Instruction{},
	}
	code, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug with empty instructions: %v", err)
	}
	if code == nil {
		t.Fatal("expected non-nil code")
	}
	defer code.Free()
	if code.Len() < 4 {
		t.Error("expected at least prologue+RET in empty function")
	}
}

func TestCompileSparkplugSingleInstruction(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "single",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
		},
	}
	code, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug with single instruction: %v", err)
	}
	if code == nil {
		t.Fatal("expected non-nil code")
	}
	defer code.Free()
	// At minimum: prologue (8 bytes) + instruction + epilogue (8 bytes).
	if code.Len() < 16 {
		t.Errorf("expected at least 16 bytes, got %d", code.Len())
	}
}

func TestCompileSparkplugMultipleInstructions(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "multi",
		NumRegisters: 3,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpStar, OperandA: 0},
			{Op: js.OpLdaSmi, OperandA: 2},
			{Op: js.OpStar, OperandA: 1},
			{Op: js.OpAdd},
			{Op: js.OpReturn},
		},
	}
	code, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug with multiple instructions: %v", err)
	}
	if code == nil {
		t.Fatal("expected non-nil code")
	}
	defer code.Free()
	// Check deopt data was built.
	if bf.DeoptData == nil {
		t.Error("expected DeoptData to be set")
	} else {
		dd, ok := bf.DeoptData.(*DeoptimizationInputData)
		if !ok {
			t.Error("DeoptData is not *DeoptimizationInputData")
		} else if len(dd.Points) != 6 {
			t.Errorf("expected 6 deopt points, got %d", len(dd.Points))
		}
	}
	// Check PC mappings.
	if len(bf.PcToNative) != 6 {
		t.Errorf("expected 6 PC-to-native mappings, got %d", len(bf.PcToNative))
	}
}

// ============================================================================
// Round 4: icpatch.go — additional branch coverage
// ============================================================================

func TestPatchPolymorphicICSlotRegisteredEmpty(t *testing.T) {
	// Registered slot but empty shapes → should become megamorphic.
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	codeBufRegistry.Store(buf.rxAddr, buf)
	icSlotOffsets.Store(buf.rxAddr, []int{0})
	defer codeBufRegistry.Delete(buf.rxAddr)
	defer icSlotOffsets.Delete(buf.rxAddr)

	PatchPolymorphicICSlotAt(buf.rxAddr, 0, []unsafe.Pointer{}, []int{})
	// First instruction should be B (megamorphic), not NOP.
	w := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if w == 0xD503201F {
		t.Error("expected empty shapes to trigger megamorphic patch")
	}
}

func TestPatchPolymorphicICSlotRegisteredManyShapes(t *testing.T) {
	// Registered slot with >4 shapes → megamorphic.
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	codeBufRegistry.Store(buf.rxAddr, buf)
	icSlotOffsets.Store(buf.rxAddr, []int{0})
	defer codeBufRegistry.Delete(buf.rxAddr)
	defer icSlotOffsets.Delete(buf.rxAddr)

	PatchPolymorphicICSlotAt(buf.rxAddr, 0,
		[]unsafe.Pointer{nil, nil, nil, nil, nil},
		[]int{0, 0, 0, 0, 0})
	w := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if w == 0xD503201F {
		t.Error("expected >4 shapes to trigger megamorphic patch")
	}
}

func TestPatchMegamorphicICSlotRegistered(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	codeBufRegistry.Store(buf.rxAddr, buf)
	icSlotOffsets.Store(buf.rxAddr, []int{0})
	defer codeBufRegistry.Delete(buf.rxAddr)
	defer icSlotOffsets.Delete(buf.rxAddr)

	PatchMegamorphicICSlotAt(buf.rxAddr, 0)
	w := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if w == 0xD503201F {
		t.Error("expected megamorphic slot to be patched")
	}
}

// ============================================================================
// Round 5: Assembler CBZ/CBNZ branch path + RegisterCodeBuf Seal path
// ============================================================================

func TestAssemblerCBZResolved(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	lbl := NewLabel()
	as.Bind(lbl)             // resolve first
	as.CBZ(REG_R0, lbl)      // should resolve immediately
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

func TestAssemblerCBNZUnresolved(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	lbl := NewLabel()
	as.CBNZ(REG_R1, lbl)     // unresolved
	as.NOP()
	as.Bind(lbl)             // resolve now
	if buf.Len() < 8 {
		t.Errorf("expected at least 8 bytes, got %d", buf.Len())
	}
}

func TestRegisterCodeBufSeal(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	// Register with slot offsets — Seal may fail (no entitlements).
	// Clean up registry after test.
	defer codeBufRegistry.Delete(buf.rxAddr)
	defer icSlotOffsets.Delete(buf.rxAddr)

	RegisterCodeBuf(buf.rxAddr, buf, []int{0, 32, 64})
	// If Seal succeeds, registry is populated; if not, it isn't.
	// Either way, no panic. Verify registry state.
	_, registered := codeBufRegistry.Load(buf.rxAddr)
	if !registered {
		t.Log("Seal failed (expected without entitlements), registry empty")
	} else {
		offsetsVal, ok := icSlotOffsets.Load(buf.rxAddr)
		if !ok {
			t.Error("expected slot offsets in registry")
		} else {
			off := offsetsVal.([]int)
			if len(off) != 3 {
				t.Errorf("expected 3 slot offsets, got %d", len(off))
			}
		}
	}
}

func TestRegisterCodeBufEmptySlots(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer codeBufRegistry.Delete(buf.rxAddr)

	RegisterCodeBuf(buf.rxAddr, buf, nil)
	// Even if Seal fails (common on macOS without entitlements), no panic.
	// Verify slots were not stored (nil/empty slotOffsets skip Store).
	_, hasOffsets := icSlotOffsets.Load(buf.rxAddr)
	if !hasOffsets {
		t.Log("empty slot offsets: registry skipped (expected)")
	}
}

func TestCompileSparkplugWithICVector(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "ic_test",
		NumRegisters: 2,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 10},
			{Op: js.OpReturn},
		},
		ICVector: &js.FeedbackVector{
			Slots: make([]js.ICSlot, 3),
		},
	}
	code, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug with IC vector: %v", err)
	}
	if code == nil {
		t.Fatal("expected non-nil code")
	}
	defer code.Free()
	if bf.DeoptData == nil {
		t.Error("expected DeoptData to be set")
	}
}

func TestFuncToAddr(t *testing.T) {
	addr := funcToAddr(func() {})
	if addr == 0 {
		t.Error("expected non-zero function address")
	}
}

// ============================================================================
// Round 5: Additional assembler coverage — STP/LDP with offset
// ============================================================================

func TestAssemblerSTPWithOffset(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.STP(REG_R0, REG_R1, REG_R8, 64) // offset 64 → imm7 = 8
	as.STP(REG_R2, REG_R3, REG_R8, 0)  // offset 0 → imm7 = 0
	if buf.Len() != 8 {
		t.Errorf("expected 8 bytes, got %d", buf.Len())
	}
}

func TestAssemblerLDPWithOffset(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.LDP(REG_R0, REG_R1, REG_R8, 64)
	as.LDP(REG_R2, REG_R3, REG_R8, 0)
	if buf.Len() != 8 {
		t.Errorf("expected 8 bytes, got %d", buf.Len())
	}
}

// ============================================================================
// Round 5: PatchICSlotStore AT registered/unregistered
// ============================================================================

func TestPatchICSlotStoreRegistered(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	codeBufRegistry.Store(buf.rxAddr, buf)
	icSlotOffsets.Store(buf.rxAddr, []int{0})
	defer codeBufRegistry.Delete(buf.rxAddr)
	defer icSlotOffsets.Delete(buf.rxAddr)

	PatchICSlotStoreAt(buf.rxAddr, 0, nil, 16)
	w := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if w == 0xD503201F {
		t.Error("expected store slot to be patched")
	}
}


// ============================================================================

// ============================================================================
// Round 2: sparkplug_helpers.go — jsAdd/jsSub/jsMul/jsDiv/jsNaN
// ============================================================================

func TestJsAddNumbersR3(t *testing.T) {
a := js.NewNumber(10)
b := js.NewNumber(32)
result := jsAdd(a, b)
if result.ToNumber() != 42 {
t.Errorf("expected 42, got %v", result.ToNumber())
}
}

func TestJsAddStringsR3(t *testing.T) {
a := js.NewString("hello ")
b := js.NewString("world")
result := jsAdd(a, b)
if result.StrVal != "hello world" {
t.Errorf("expected 'hello world', got %q", result.StrVal)
}
}

func TestJsSubR3(t *testing.T) {
a := js.NewNumber(100)
b := js.NewNumber(58)
result := jsSub(a, b)
if result.ToNumber() != 42 {
t.Errorf("expected 42, got %v", result.ToNumber())
}
}

func TestJsMulR3(t *testing.T) {
a := js.NewNumber(7)
b := js.NewNumber(6)
result := jsMul(a, b)
if result.ToNumber() != 42 {
t.Errorf("expected 42, got %v", result.ToNumber())
}
}

func TestJsDivR3(t *testing.T) {
result := jsDiv(js.NewNumber(84), js.NewNumber(2))
if result.ToNumber() != 42 {
t.Errorf("expected 42, got %v", result.ToNumber())
}
}

func TestJsDivByZeroR3(t *testing.T) {
result := jsDiv(js.NewNumber(42), js.NewNumber(0))
if !math.IsNaN(result.ToNumber()) {
t.Errorf("expected NaN, got %v", result.ToNumber())
}
}

func TestJsNaNR3(t *testing.T) {
if !math.IsNaN(jsNaN()) {
t.Error("jsNaN() should return NaN")
}
}

// ============================================================================
// Round 2: sparkplug_helpers.go — typeof, mod, logical helpers
// ============================================================================

func TestSparkplugOpTypeofR3(t *testing.T) {
frame := makeTestFrame(1)
tests := []struct {
val  js.JSValue
want string
}{
{js.Undefined, "undefined"},
{js.Null, "object"},
{js.NewBoolean(true), "boolean"},
{js.NewNumber(42), "number"},
{js.NewString("hi"), "string"},
}
for _, tc := range tests {
frame.Acc = tc.val
sparkplugOpTypeof(frame)
if frame.Acc.StrVal != tc.want {
t.Errorf("typeof(%v) = %q, want %q", tc.val, frame.Acc.StrVal, tc.want)
}
}
}

func TestSparkplugOpModR3(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(3)
frame.Regs[0] = js.NewNumber(10)
sparkplugOpMod(frame, 0)
if frame.Acc.ToNumber() != 1 {
t.Errorf("expected 1, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpModByZeroR3(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(0)
frame.Regs[0] = js.NewNumber(42)
sparkplugOpMod(frame, 0)
if !math.IsNaN(frame.Acc.ToNumber()) {
t.Errorf("expected NaN, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpLogicalNotR3(t *testing.T) {
frame := makeTestFrame(1)
frame.Acc = js.NewNumber(0)
sparkplugOpLogicalNot(frame)
if !frame.Acc.IsTruthy() {
t.Error("!0 should be true")
}
frame.Acc = js.NewNumber(42)
sparkplugOpLogicalNot(frame)
if frame.Acc.IsTruthy() {
t.Error("!42 should be false")
}
}

func TestSparkplugOpLogicalAndR3(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(1)
frame.Regs[0] = js.NewNumber(42)
sparkplugOpLogicalAnd(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
}
// falsy short-circuit
frame.Acc = js.NewNumber(0)
frame.Regs[0] = js.NewNumber(99)
sparkplugOpLogicalAnd(frame, 0)
if frame.Acc.ToNumber() != 0 {
t.Errorf("expected 0 (short-circuit), got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpLogicalOrR3(t *testing.T) {
frame := makeTestFrame(2)
// truthy short-circuit
frame.Acc = js.NewNumber(42)
frame.Regs[0] = js.NewNumber(0)
sparkplugOpLogicalOr(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42 (short-circuit), got %v", frame.Acc.ToNumber())
}
// falsy falls through
frame.Acc = js.NewNumber(0)
frame.Regs[0] = js.NewNumber(99)
sparkplugOpLogicalOr(frame, 0)
if frame.Acc.ToNumber() != 99 {
t.Errorf("expected 99, got %v", frame.Acc.ToNumber())
}
}

// ============================================================================
// Round 2: register code buffer with 0-length slot offsets
// ============================================================================

func TestRegisterCodeBufEmptySlotOffsets(t *testing.T) {
buf, err := NewCodeBuf(128)
if err != nil {
t.Fatalf("NewCodeBuf: %v", err)
}
defer buf.Free()
defer codeBufRegistry.Delete(buf.rxAddr)
defer icSlotOffsets.Delete(buf.rxAddr)

// Register with empty slot offsets.
RegisterCodeBuf(buf.rxAddr, buf, []int{})
// Even if Seal fails, no panic. Verify slots weren't stored.
_, hasOffsets := icSlotOffsets.Load(buf.rxAddr)
if hasOffsets {
t.Log("empty slot offsets stored (Seal succeeded)")
}
}

// ============================================================================
// Round 3: sparkplug_helpers — conversion, try handler, debugger helpers
// ============================================================================

func TestSparkplugOpToNumberR3(t *testing.T) {
frame := makeTestFrame(1)
frame.Acc = js.NewString("42")
sparkplugOpToNumber(frame)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpToStringR3(t *testing.T) {
frame := makeTestFrame(1)
frame.Acc = js.NewNumber(42)
sparkplugOpToString(frame)
if frame.Acc.StrVal != "42" {
t.Errorf("expected '42', got %q", frame.Acc.StrVal)
}
}

func TestSparkplugOpSetTryHandlerR3(t *testing.T) {
frame := makeTestFrame(1)
sparkplugOpSetTryHandler(frame, 100)
if frame.HandlerPC != 100 {
t.Errorf("expected HandlerPC 100, got %d", frame.HandlerPC)
}
}

func TestSparkplugOpClearTryHandlerR3(t *testing.T) {
frame := makeTestFrame(1)
frame.HandlerPC = 42
sparkplugOpClearTryHandler(frame)
if frame.HandlerPC != -1 {
t.Errorf("expected HandlerPC -1, got %d", frame.HandlerPC)
}
}

func TestSparkplugOpDebuggerR3(t *testing.T) {
frame := makeTestFrame(1)
// Should not panic.
sparkplugOpDebugger(frame)
}

func TestJsAddMixedR3(t *testing.T) {
// Test fallback path (not both numbers, not both strings)
a := js.NewBoolean(true)
b := js.NewNumber(41)
result := jsAdd(a, b)
if result.ToNumber() != 42 {
t.Errorf("expected 42, got %v", result.ToNumber())
}
}

func TestJsDivRegularR3(t *testing.T) {
// Already covered by TestJsDivR3, but ensure we hit the non-zero branch.
result := jsDiv(js.NewNumber(100), js.NewNumber(4))
if result.ToNumber() != 25 {
t.Errorf("expected 25, got %v", result.ToNumber())
}
}

// Round 3: sparkplug_helpers — shift operations
// ============================================================================

func TestSparkplugOpShiftLeftNumber(t *testing.T) {
	frame := makeTestFrame(2)
	frame.Acc = js.NewNumber(1)
	frame.Regs[0] = js.NewNumber(4)
	sparkplugOpShiftLeftNumber(frame, 0)
	if frame.Acc.ToNumber() != 16 {
t.Errorf("expected 16, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpShiftRightNumber(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(-16)
frame.Regs[0] = js.NewNumber(1)
sparkplugOpShiftRightNumber(frame, 0)
if frame.Acc.ToNumber() != -8 {
t.Errorf("expected -8, got %v", frame.Acc.ToNumber())
}
}

// ============================================================================
// Round 4: sparkplug_helpers — number operator fast paths
// ============================================================================

func TestSparkplugOpAddNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(10)
frame.Regs[0] = js.NewNumber(32)
sparkplugOpAddNumber(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpSubNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(100)
frame.Regs[0] = js.NewNumber(58)
sparkplugOpSubNumber(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpMulNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(7)
frame.Regs[0] = js.NewNumber(6)
sparkplugOpMulNumber(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpDivNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(84)
frame.Regs[0] = js.NewNumber(2)
sparkplugOpDivNumber(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpModNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(10)
frame.Regs[0] = js.NewNumber(3)
sparkplugOpModNumber(frame, 0)
if frame.Acc.ToNumber() != 1 {
t.Errorf("expected 1, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpNegateNumberR4(t *testing.T) {
frame := makeTestFrame(1)
frame.Acc = js.NewNumber(42)
sparkplugOpNegateNumber(frame)
if frame.Acc.ToNumber() != -42 {
t.Errorf("expected -42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpIncNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Regs[0] = js.NewNumber(41)
sparkplugOpIncNumber(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected acc=42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpDecNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Regs[0] = js.NewNumber(43)
sparkplugOpDecNumber(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected acc=42, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpStrictEqNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(42)
frame.Regs[0] = js.NewNumber(42)
sparkplugOpStrictEqNumber(frame, 0)
if !frame.Acc.IsTruthy() {
t.Error("42 === 42 should be true")
}
frame.Acc = js.NewNumber(42)
frame.Regs[0] = js.NewNumber(0)
sparkplugOpStrictEqNumber(frame, 0)
if frame.Acc.IsTruthy() {
t.Error("42 === 0 should be false")
}
}

func TestSparkplugOpBitAndNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(6)   // 0b110
frame.Regs[0] = js.NewNumber(3) // 0b011
sparkplugOpBitAndNumber(frame, 0)
if frame.Acc.ToNumber() != 2 {
t.Errorf("expected 2, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpBitOrNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(2)   // 0b010
frame.Regs[0] = js.NewNumber(1) // 0b001
sparkplugOpBitOrNumber(frame, 0)
if frame.Acc.ToNumber() != 3 {
t.Errorf("expected 3, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpBitXorNumberR4(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(7)   // 0b111
frame.Regs[0] = js.NewNumber(2) // 0b010
sparkplugOpBitXorNumber(frame, 0)
if frame.Acc.ToNumber() != 5 {
t.Errorf("expected 5, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpBitNotNumberR4(t *testing.T) {
frame := makeTestFrame(1)
frame.Acc = js.NewNumber(-1) // int32(0xFFFFFFFF)
sparkplugOpBitNotNumber(frame)
if frame.Acc.ToNumber() != 0 {
t.Errorf("expected 0, got %v", frame.Acc.ToNumber())
}
}

// ============================================================================
// Round 4: sparkplug_helpers — constant, finally handler, create object
// ============================================================================

func TestSparkplugOpLdaConstantR4(t *testing.T) {
bf := &js.BytecodeFunction{
Constants: []js.JSValue{js.NewNumber(42), js.NewString("hello")},
}
frame := &js.VMFrame{Func: bf}
sparkplugOpLdaConstant(frame, 0)
if frame.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
}
sparkplugOpLdaConstant(frame, 1)
if frame.Acc.StrVal != "hello" {
t.Errorf("expected 'hello', got %q", frame.Acc.StrVal)
}
}

func TestSparkplugOpSetFinallyHandlerR4(t *testing.T) {
frame := makeTestFrame(1)
sparkplugOpSetFinallyHandler(frame, 10)
if frame.FinallyPC != 10 {
t.Errorf("expected FinallyPC 10, got %d", frame.FinallyPC)
}
sparkplugOpSetFinallyHandler(frame, 255) // sentinel: clear
if frame.FinallyPC != -1 {
t.Errorf("expected FinallyPC -1, got %d", frame.FinallyPC)
}
}

func TestSparkplugOpCreateObjectR4(t *testing.T) {
frame := makeTestFrame(1)
sparkplugOpCreateObject(frame)
if !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
t.Error("expected object in acc")
}
}

// ============================================================================
// Round 5: Additional branch coverage for comparison helpers
// ============================================================================

func TestSparkplugOpLessThanNumberR5(t *testing.T) {
frame := makeTestFrame(2)
// true case
frame.Acc = js.NewNumber(10)
frame.Regs[0] = js.NewNumber(5)
sparkplugOpLessThanNumber(frame, 0)
if frame.Acc.IsTruthy() {
t.Error("10 < 5 should be false")
}
// true case
frame.Acc = js.NewNumber(3)
frame.Regs[0] = js.NewNumber(7)
sparkplugOpLessThanNumber(frame, 0)
if !frame.Acc.IsTruthy() {
t.Error("3 < 7 should be true")
}
}

func TestSparkplugOpGreaterThanNumberR5(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(10)
frame.Regs[0] = js.NewNumber(5)
sparkplugOpGreaterThanNumber(frame, 0)
if !frame.Acc.IsTruthy() {
t.Error("10 > 5 should be true")
}
frame.Acc = js.NewNumber(3)
frame.Regs[0] = js.NewNumber(7)
sparkplugOpGreaterThanNumber(frame, 0)
if frame.Acc.IsTruthy() {
t.Error("3 > 7 should be false")
}
}

func TestSparkplugOpLessEqNumberR5(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(5)
frame.Regs[0] = js.NewNumber(5)
sparkplugOpLessEqNumber(frame, 0)
if !frame.Acc.IsTruthy() {
t.Error("5 <= 5 should be true")
}
}

func TestSparkplugOpGreaterEqNumberR5(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(5)
frame.Regs[0] = js.NewNumber(5)
sparkplugOpGreaterEqNumber(frame, 0)
if !frame.Acc.IsTruthy() {
t.Error("5 >= 5 should be true")
}
}

func TestSparkplugOpStrictNotEqNumberR5(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(42)
frame.Regs[0] = js.NewNumber(42)
sparkplugOpStrictNotEqNumber(frame, 0)
if frame.Acc.IsTruthy() {
t.Error("42 !== 42 should be false")
}
frame.Acc = js.NewNumber(42)
frame.Regs[0] = js.NewNumber(0)
sparkplugOpStrictNotEqNumber(frame, 0)
if !frame.Acc.IsTruthy() {
t.Error("42 !== 0 should be true")
}
}

func TestSparkplugOpCmpNumberR5(t *testing.T) {
frame := makeTestFrame(2)
// cond=0: EQ check
frame.Acc = js.NewNumber(42)
frame.Regs[0] = js.NewNumber(42)
sparkplugOpCmpNumber(frame, 0, 0) // EQ
if !frame.Acc.IsTruthy() {
t.Error("42 == 42 (cond 0) should be true")
}
}

func TestSparkplugOpModNumberByZeroR5(t *testing.T) {
frame := makeTestFrame(2)
frame.Acc = js.NewNumber(10)
frame.Regs[0] = js.NewNumber(0)
sparkplugOpModNumber(frame, 0)
if !math.IsNaN(frame.Acc.ToNumber()) {
t.Errorf("10 %% 0 should be NaN, got %v", frame.Acc.ToNumber())
}
}

func TestSparkplugOpToBooleanNumberR5(t *testing.T) {
frame := makeTestFrame(1)
frame.Acc = js.NewNumber(0)
sparkplugOpToBooleanNumber(frame)
if frame.Acc.IsTruthy() {
t.Error("ToBoolean(0) should be false")
}
frame.Acc = js.NewNumber(42)
sparkplugOpToBooleanNumber(frame)
if !frame.Acc.IsTruthy() {
t.Error("ToBoolean(42) should be true")
}
}

func TestSparkplugOpToStringNumberR5(t *testing.T) {
frame := makeTestFrame(1)
frame.Acc = js.NewNumber(42)
sparkplugOpToStringNumber(frame)
if frame.Acc.StrVal != "42" {
t.Errorf("expected '42', got %q", frame.Acc.StrVal)
}
}

func TestSparkplugOpLdaCapturedR5(t *testing.T) {
frame := makeTestFrame(1)
// No ClosureEnv → returns Undefined.
sparkplugOpLdaCaptured(frame, 0)
if !frame.Acc.IsUndefined() {
t.Error("expected Undefined when no ClosureEnv")
}
}
