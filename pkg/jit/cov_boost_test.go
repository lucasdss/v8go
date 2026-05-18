package jit

import (
	"testing"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// ============================================================================
// Sparkplug helper function tests — exercises all sparkplugOp* helpers directly.
// ============================================================================

func makeTestFrame(numRegs int) *js.VMFrame {
	return &js.VMFrame{
		Func: &js.BytecodeFunction{
			Name:         "test",
			NumRegisters: numRegs,
			Instructions: []js.Instruction{},
		},
		Regs: make([]js.JSValue, numRegs),
	}
}

func TestSparkplugOpLdaSmi(t *testing.T) {
	frame := makeTestFrame(1)
	sparkplugOpLdaSmi(frame, 42)
	if frame.Acc.ToNumber() != 42 {
		t.Errorf("expected 42, got %v", frame.Acc.ToNumber())
	}
}

func TestSparkplugOpLdaZero(t *testing.T) {
	frame := makeTestFrame(1)
	sparkplugOpLdaZero(frame)
	if frame.Acc.ToNumber() != 0 {
		t.Errorf("expected 0, got %v", frame.Acc.ToNumber())
	}
}

func TestSparkplugOpLdaOne(t *testing.T) {
	frame := makeTestFrame(1)
	sparkplugOpLdaOne(frame)
	if frame.Acc.ToNumber() != 1 {
		t.Errorf("expected 1, got %v", frame.Acc.ToNumber())
	}
}

func TestSparkplugOpLdaUndefined(t *testing.T) {
	frame := makeTestFrame(1)
	sparkplugOpLdaUndefined(frame)
	if !frame.Acc.IsUndefined() {
		t.Errorf("expected undefined, got %v", frame.Acc.String())
	}
}

func TestSparkplugOpLdaNull(t *testing.T) {
	frame := makeTestFrame(1)
	sparkplugOpLdaNull(frame)
	if !frame.Acc.IsNull() {
		t.Errorf("expected null, got %v", frame.Acc.String())
	}
}

func TestSparkplugOpLdaTrue(t *testing.T) {
	frame := makeTestFrame(1)
	sparkplugOpLdaTrue(frame)
	if !frame.Acc.IsBoolean() || !frame.Acc.IsTruthy() {
		t.Errorf("expected true, got %v", frame.Acc.String())
	}
}

func TestSparkplugOpLdaFalse(t *testing.T) {
	frame := makeTestFrame(1)
	sparkplugOpLdaFalse(frame)
	if !frame.Acc.IsBoolean() || frame.Acc.IsTruthy() {
		t.Errorf("expected false, got %v", frame.Acc.String())
	}
}

func TestSparkplugOpStar(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(99)
	sparkplugOpStar(frame, 1)
	if frame.Regs[1].ToNumber() != 99 {
		t.Errorf("expected reg[1]=99, got %v", frame.Regs[1].ToNumber())
	}
	// Out of bounds should not panic.
	sparkplugOpStar(frame, 999)
}

func TestSparkplugOpLdar(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Regs[2] = js.NewNumber(77)
	sparkplugOpLdar(frame, 2)
	if frame.Acc.ToNumber() != 77 {
		t.Errorf("expected acc=77, got %v", frame.Acc.ToNumber())
	}
	// Out of bounds should not panic.
	sparkplugOpLdar(frame, 999)
}

func TestSparkplugOpReturn(t *testing.T) {
	frame := makeTestFrame(2)
	frame.Func.Instructions = []js.Instruction{
		{Op: js.OpLdaSmi, OperandA: 1},
		{Op: js.OpReturn},
	}
	sparkplugOpReturn(frame)
	if frame.PC != 2 {
		t.Errorf("expected PC=2 (past end), got %d", frame.PC)
	}
}

func TestSparkplugOpAdd(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(3)
	frame.Regs[1] = js.NewNumber(4)
	sparkplugOpAdd(frame, 1)
	if frame.Acc.ToNumber() != 7 {
		t.Errorf("3+4=7, got %v", frame.Acc.ToNumber())
	}

	// String concatenation
	frame.Acc = js.NewString("hello")
	frame.Regs[1] = js.NewString("world")
	sparkplugOpAdd(frame, 1)
	if frame.Acc.ToString() != "helloworld" {
		t.Errorf("'hello'+'world'='helloworld', got %q", frame.Acc.ToString())
	}

	// Mixed types (number coercion)
	frame.Acc = js.NewNumber(10)
	frame.Regs[1] = js.NewString("5")
	sparkplugOpAdd(frame, 1)
	if frame.Acc.ToNumber() != 15 {
		t.Errorf("10+'5'=15 via coercion, got %v", frame.Acc.ToNumber())
	}

	// Out of bounds reg
	sparkplugOpAdd(frame, 999)
}

func TestSparkplugOpSub(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(10)
	frame.Regs[1] = js.NewNumber(3)
	sparkplugOpSub(frame, 1)
	if frame.Acc.ToNumber() != 7 {
		t.Errorf("10-3=7, got %v", frame.Acc.ToNumber())
	}
	sparkplugOpSub(frame, 999) // out of bounds, no panic
}

func TestSparkplugOpMul(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(6)
	frame.Regs[1] = js.NewNumber(7)
	sparkplugOpMul(frame, 1)
	if frame.Acc.ToNumber() != 42 {
		t.Errorf("6*7=42, got %v", frame.Acc.ToNumber())
	}
	sparkplugOpMul(frame, 999)
}

func TestSparkplugOpDiv(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(84)
	frame.Regs[1] = js.NewNumber(2)
	sparkplugOpDiv(frame, 1)
	if frame.Acc.ToNumber() != 42 {
		t.Errorf("84/2=42, got %v", frame.Acc.ToNumber())
	}

	// Division by zero
	frame.Acc = js.NewNumber(1)
	frame.Regs[1] = js.NewNumber(0)
	sparkplugOpDiv(frame, 1)
	nan := frame.Acc.ToNumber()
	if nan == nan {
		t.Errorf("1/0 should be NaN, got %v", nan)
	}

	sparkplugOpDiv(frame, 999)
}

func TestSparkplugIsFalsy(t *testing.T) {
	frame := makeTestFrame(1)
	frame.Acc = js.NewNumber(0)
	if !sparkplugIsFalsy(frame) {
		t.Error("0 should be falsy")
	}
	frame.Acc = js.NewNumber(1)
	if sparkplugIsFalsy(frame) {
		t.Error("1 should be truthy")
	}
	frame.Acc = js.Undefined
	if !sparkplugIsFalsy(frame) {
		t.Error("undefined should be falsy")
	}
	frame.Acc = js.Null
	if !sparkplugIsFalsy(frame) {
		t.Error("null should be falsy")
	}
}

// ============================================================================
// JS arithmetic helper tests.
// ============================================================================

func TestJsAdd(t *testing.T) {
	result := jsAdd(js.NewNumber(3), js.NewNumber(4))
	if result.ToNumber() != 7 {
		t.Errorf("3+4=7, got %v", result.ToNumber())
	}

	result = jsAdd(js.NewString("a"), js.NewString("b"))
	if result.ToString() != "ab" {
		t.Errorf("'a'+'b'='ab', got %q", result.ToString())
	}

	result = jsAdd(js.NewNumber(1), js.NewString("2"))
	if result.ToNumber() != 3 {
		t.Errorf("1+'2'=3, got %v", result.ToNumber())
	}
}

func TestJsSub(t *testing.T) {
	result := jsSub(js.NewNumber(10), js.NewNumber(3))
	if result.ToNumber() != 7 {
		t.Errorf("10-3=7, got %v", result.ToNumber())
	}
}

func TestJsMul(t *testing.T) {
	result := jsMul(js.NewNumber(6), js.NewNumber(7))
	if result.ToNumber() != 42 {
		t.Errorf("6*7=42, got %v", result.ToNumber())
	}
}

func TestJsDiv(t *testing.T) {
	result := jsDiv(js.NewNumber(84), js.NewNumber(2))
	if result.ToNumber() != 42 {
		t.Errorf("84/2=42, got %v", result.ToNumber())
	}

	result = jsDiv(js.NewNumber(1), js.NewNumber(0))
	nan := result.ToNumber()
	if nan == nan {
		t.Errorf("1/0 should be NaN, got %v", nan)
	}
}

func TestJsNaN(t *testing.T) {
	v := jsNaN()
	if v == v {
		t.Error("NaN != NaN")
	}
}

// ============================================================================
// IC patch public API tests — exercises PatchICSlotAt, PatchPolymorphicICSlotAt,
// PatchMegamorphicICSlotAt, PatchICSlotStore, PatchICSlotStoreAt.
// ============================================================================

// TestPatchICSlotAtUnregistered verifies public API paths that early-return
// (unregistered address, slot index out of range) without calling any write-protect toggle.
// Testing the full write path would require the com.apple.security.cs.allow-jit
// entitlement and is covered by integration tests.
func TestPatchICSlotAtUnregistered(t *testing.T) {
	// Unregistered address → resolveICSlot fails → returns immediately.
	PatchICSlotAt(0xDEADBEEF, 0, nil, 0)
}

// TestPatchICSlotCommon tests the internal patchICSlotCommon function directly
// without the JIT write protect toggle (avoids SIGBUS without entitlements).
func TestPatchICSlotCommon(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	// Fill with NOPs.
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	// Register without sealing (skip Seal to avoid mprotect).
	// Directly store in the registry maps.
	codeBufRegistry.Store(buf.rxAddr, buf)
	icSlotOffsets.Store(buf.rxAddr, []int{0, 32})

	// patchICSlotCommon resolves and patches via PatchICSlot.
	patchICSlotCommon(buf.rxAddr, 0, nil, 0, PatchICSlot)
	patchICSlotCommon(buf.rxAddr, 1, nil, 8, PatchICSlot)
	// Slot index out of range → should no-op.
	patchICSlotCommon(buf.rxAddr, 99, nil, 0, PatchICSlot)
	// Unregistered address → no-op.
	patchICSlotCommon(0xCAFE, 0, nil, 0, PatchICSlot)
}

// TestPatchICSlotStoreCommon tests store patching via patchICSlotCommon.
func TestPatchICSlotStoreCommon(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	codeBufRegistry.Store(buf.rxAddr, buf)
	icSlotOffsets.Store(buf.rxAddr, []int{0})

	patchICSlotCommon(buf.rxAddr, 0, nil, 16, PatchICSlotStore)
}

// TestPatchPolymorphicICSlotAtUnregistered tests the public API with unregistered address.
func TestPatchPolymorphicICSlotAtUnregistered(t *testing.T) {
	// Unregistered address → resolveICSlot fails → returns immediately.
	PatchPolymorphicICSlotAt(0xCAFE, 0, []unsafe.Pointer{nil}, []int{0})
	PatchPolymorphicICSlotAt(0xCAFE, 0, []unsafe.Pointer{}, []int{})
	PatchPolymorphicICSlotAt(0xBEEF, 0,
		[]unsafe.Pointer{nil, nil, nil, nil, nil},
		[]int{0, 0, 0, 0, 0})
}

// TestPatchMegamorphicICSlotAtUnregistered tests megamorphic API with unregistered address.
func TestPatchMegamorphicICSlotAtUnregistered(t *testing.T) {
	PatchMegamorphicICSlotAt(0xBEEF, 0)
}

// TestPatchICSlotStoreAtUnregistered tests store API with unregistered address.
func TestPatchICSlotStoreAtUnregistered(t *testing.T) {
	PatchICSlotStoreAt(0xF00D, 0, nil, 0)
}

func TestPatchICSlotStore(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	// Fill with NOPs.
	for i := 0; i < 256; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	PatchICSlotStore(buf, 0, 0xABCD0000, 8, 16)
	// Verify the sled was written (non-NOP).
	isNOP := true
	for i := 0; i < 32 && isNOP; i += 4 {
		w := uint32(buf.rwBuf[i]) | uint32(buf.rwBuf[i+1])<<8 | uint32(buf.rwBuf[i+2])<<16 | uint32(buf.rwBuf[i+3])<<24
		if w != 0xD503201F {
			isNOP = false
		}
	}
	if isNOP {
		t.Error("expected store IC slot to be patched")
	}
}

// ============================================================================
// GC Bridge tests.
// ============================================================================

func TestRegisterRegion(t *testing.T) {
	// Clear any existing regions.
	regionMu.Lock()
	registeredRegionSlice = nil
	regionMu.Unlock()

	r := Region{
		Start: 0x1000,
		End:   0x2000,
	}
	ok := RegisterRegion(r)
	if !ok {
		t.Error("expected RegisterRegion to succeed")
	}
	if NumRegions() != 1 {
		t.Errorf("expected 1 region, got %d", NumRegions())
	}
}

func TestRegisterRegionMaxLimit(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = make([]Region, MaxRegions)
	regionMu.Unlock()

	r := Region{Start: 0xFFFF0000, End: 0xFFFF1000}
	ok := RegisterRegion(r)
	if ok {
		t.Error("expected RegisterRegion to fail at MaxRegions")
	}
}

func TestUnregisterRegion(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = []Region{
		{Start: 0x1000, End: 0x2000},
		{Start: 0x3000, End: 0x4000},
		{Start: 0x5000, End: 0x6000},
	}
	regionMu.Unlock()

	// Remove the middle region.
	UnregisterRegion(0x3000, 0x4000)
	if NumRegions() != 2 {
		t.Errorf("expected 2 regions after unregister, got %d", NumRegions())
	}
}

func TestFindRegion(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = []Region{
		{Start: 0x1000, End: 0x2000},
		{Start: 0x3000, End: 0x4000},
		{Start: 0x5000, End: 0x6000},
	}
	regionMu.Unlock()

	r := findRegion(0x1500)
	if r == nil || r.Start != 0x1000 {
		t.Error("findRegion(0x1500) should find region at 0x1000")
	}

	r = findRegion(0x2500)
	if r != nil {
		t.Error("findRegion(0x2500) should return nil")
	}
}

func TestFindRegionManyEntries(t *testing.T) {
	// Test binary search path (>16 regions).
	regionMu.Lock()
	registeredRegionSlice = make([]Region, 20)
	for i := 0; i < 20; i++ {
		registeredRegionSlice[i] = Region{
			Start: uintptr((i + 1) * 0x1000),
			End:   uintptr((i+1)*0x1000 + 0x800),
		}
	}
	regionMu.Unlock()

	r := findRegion(0x5000)
	if r == nil || r.Start != 0x5000 {
		t.Error("findRegion should find entry at 0x5000 in binary search")
	}

	// Not found.
	r = findRegion(0x99999)
	if r != nil {
		t.Error("findRegion should return nil for unknown PC")
	}
}

func TestNextJITFrame(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = nil
	regionMu.Unlock()

	_, _, ok := NextJITFrame(0x1000, 0)
	if ok {
		t.Error("NextJITFrame should return false with no regions")
	}

	// Register a region with Next callback.
	regionMu.Lock()
	registeredRegionSlice = []Region{{
		Start: 0x1000,
		End:   0x2000,
		Next: func(pc, sp uintptr) (uintptr, uintptr, bool) {
			return 0x900, 0x800, true
		},
	}}
	regionMu.Unlock()

	cp, sp, ok := NextJITFrame(0x1500, 0x700)
	if !ok {
		t.Error("NextJITFrame should find registered region")
	}
	if cp != 0x900 || sp != 0x800 {
		t.Errorf("NextJITFrame: expected (0x900, 0x800), got (0x%x, 0x%x)", cp, sp)
	}
}

func TestScanJITStack(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = nil
	regionMu.Unlock()

	called := false
	ScanJITStack(0x1000, 0, func(unsafe.Pointer) {})
	// Should not call addRoot — no regions.
	_ = called

	// Register a region with ScanStack callback.
	regionMu.Lock()
	registeredRegionSlice = []Region{{
		Start: 0x1000,
		End:   0x2000,
		ScanStack: func(pc, sp uintptr, addRoot func(unsafe.Pointer)) {
			called = true
			addRoot(unsafe.Pointer(uintptr(0xDEAD)))
		},
	}}
	regionMu.Unlock()

	roots := make([]unsafe.Pointer, 0)
	ScanJITStack(0x1500, 0, func(p unsafe.Pointer) {
		roots = append(roots, p)
	})
	if !called || len(roots) != 1 {
		t.Errorf("ScanJITStack: called=%v, roots=%d", called, len(roots))
	}
}

func TestPreemptJIT(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = nil
	regionMu.Unlock()

	if PreemptJIT() {
		t.Error("PreemptJIT should return false with no regions")
	}

	regionMu.Lock()
	registeredRegionSlice = []Region{{
		Start:   0x1000,
		End:     0x2000,
		Preempt: func() bool { return true },
	}}
	regionMu.Unlock()

	if !PreemptJIT() {
		t.Error("PreemptJIT should return true when region requests preempt")
	}
}

func TestNumRegions(t *testing.T) {
	regionMu.Lock()
	registeredRegionSlice = []Region{{Start: 0, End: 0x1000}}
	regionMu.Unlock()

	if NumRegions() != 1 {
		t.Errorf("NumRegions: expected 1, got %d", NumRegions())
	}
}

// ============================================================================
// CodeBuf RWAddr test.
// ============================================================================

func TestCodeBufRWAddr(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	defer buf.Free()
	addr := buf.RWAddr()
	if addr == 0 {
		t.Error("RWAddr should be non-zero")
	}
}

// ============================================================================
// Turbofan init() hook test — exercises the TurboFan compiler via the backend.
// ============================================================================

func TestTurboFanInitHook(t *testing.T) {
	backend := NewBackend()

	// Test with a valid function.
	bf := &js.BytecodeFunction{
		Name:         "hook_test",
		NumRegisters: 2,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
	rxAddr, err := backend.CompileTurboFan(bf)
	if err != nil {
		t.Fatalf("TurboFanCompiler hook failed: %v", err)
	}
	if rxAddr == 0 {
		t.Error("expected non-zero rxAddr")
	}
}

func TestTurboFanInitHookNil(t *testing.T) {
	backend := NewBackend()
	_, err := backend.CompileTurboFan(nil)
	if err == nil {
		t.Error("expected error for nil function")
	}
}

// ============================================================================
// Comparison / slow-path sparkplug helper tests.
// ============================================================================

func TestSparkplugOpStrictEq(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(42)
	frame.Regs[1] = js.NewNumber(42)
	sparkplugOpStrictEq(frame, 1)
	if !frame.Acc.IsBoolean() || !frame.Acc.IsTruthy() {
		t.Error("42 === 42 should be true")
	}

	frame.Acc = js.NewNumber(42)
	frame.Regs[1] = js.NewNumber(43)
	sparkplugOpStrictEq(frame, 1)
	if frame.Acc.IsTruthy() {
		t.Error("42 === 43 should be false")
	}

	sparkplugOpStrictEq(frame, 999) // no panic
}

func TestSparkplugOpLessThan(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(10)
	frame.Regs[1] = js.NewNumber(5)
	sparkplugOpLessThan(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("5 < 10 should be true")
	}

	frame.Acc = js.NewNumber(5)
	frame.Regs[1] = js.NewNumber(10)
	sparkplugOpLessThan(frame, 1)
	if frame.Acc.IsTruthy() {
		t.Error("10 < 5 should be false")
	}

	sparkplugOpLessThan(frame, 999)
}

func TestSparkplugOpGreaterThan(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(5)
	frame.Regs[1] = js.NewNumber(10)
	sparkplugOpGreaterThan(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("10 > 5 should be true")
	}

	frame.Acc = js.NewNumber(10)
	frame.Regs[1] = js.NewNumber(5)
	sparkplugOpGreaterThan(frame, 1)
	if frame.Acc.IsTruthy() {
		t.Error("5 > 10 should be false")
	}

	sparkplugOpGreaterThan(frame, 999)
}

func TestSparkplugOpEq(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(42)
	frame.Regs[1] = js.NewString("42")
	sparkplugOpEq(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("42 == '42' should be true (loose equality)")
	}

	sparkplugOpEq(frame, 999)
}

func TestSparkplugOpNotEq(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(42)
	frame.Regs[1] = js.NewNumber(99)
	sparkplugOpNotEq(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("42 != 99 should be true (loose)")
	}

	frame.Acc = js.NewNumber(42)
	frame.Regs[1] = js.NewNumber(42)
	sparkplugOpNotEq(frame, 1)
	if frame.Acc.IsTruthy() {
		t.Error("42 != 42 should be false (loose)")
	}

	sparkplugOpNotEq(frame, 999)
}

func TestSparkplugOpStrictNotEq(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(42)
	frame.Regs[1] = js.NewNumber(99)
	sparkplugOpStrictNotEq(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("42 !== 99 should be true")
	}

	frame.Acc = js.NewNumber(42)
	frame.Regs[1] = js.NewString("42")
	sparkplugOpStrictNotEq(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("42 !== '42' should be true")
	}

	sparkplugOpStrictNotEq(frame, 999)
}

func TestSparkplugOpLessEq(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(10)
	frame.Regs[1] = js.NewNumber(10)
	sparkplugOpLessEq(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("10 <= 10 should be true")
	}
	sparkplugOpLessEq(frame, 999)
}

func TestSparkplugOpGreaterEq(t *testing.T) {
	frame := makeTestFrame(3)
	frame.Acc = js.NewNumber(5)
	frame.Regs[1] = js.NewNumber(5)
	sparkplugOpGreaterEq(frame, 1)
	if !frame.Acc.IsTruthy() {
		t.Error("5 >= 5 should be true")
	}
	sparkplugOpGreaterEq(frame, 999)
}

// ============================================================================
// Sparkplug emit function tests — emit code without executing it.
// ============================================================================

func TestEmitSparkplugEpilogue(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	emitSparkplugEpilogue(as, 0)
	if as.Pos() == 0 {
		t.Error("epilogue should emit at least RET")
	}
}

func TestEmitSparkplugNegate(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	as.Bind(deoptStub)
	instr := &js.Instruction{Op: js.OpNegate, OperandA: 0}
	emitSparkplugNegate(as, instr, deoptStub)
	if as.Pos() == 0 {
		t.Error("negate should emit code")
	}
}

func TestEmitSparkplugBitwiseFast(t *testing.T) {
	ops := []struct {
		name string
		op   int
	}{
		{"and", 0},
		{"or", 1},
		{"xor", 2},
	}
	for _, tc := range ops {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := NewCodeBuf(1024)
			if err != nil {
				t.Fatalf("NewCodeBuf: %v", err)
			}
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			instr := &js.Instruction{Op: js.OpBitwiseAnd, OperandA: 1}
			emitSparkplugBitwiseFast(as, instr, tc.op, deoptStub)
			if as.Pos() == 0 {
				t.Error("bitwise fast should emit code")
			}
		})
	}
}

func TestEmitSparkplugBitwiseNot(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	as.Bind(deoptStub)
	instr := &js.Instruction{Op: js.OpBitwiseNot, OperandA: 0}
	emitSparkplugBitwiseNot(as, instr, deoptStub)
	if as.Pos() == 0 {
		t.Error("bitwise not should emit code")
	}
}

func TestEmitSparkplugShiftFast(t *testing.T) {
	ops := []struct {
		name string
		op   int
	}{
		{"shl", 0},
		{"shr", 1},
		{"shrz", 2},
	}
	for _, tc := range ops {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := NewCodeBuf(1024)
			if err != nil {
				t.Fatalf("NewCodeBuf: %v", err)
			}
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			instr := &js.Instruction{Op: js.OpShiftLeft, OperandA: 1}
			emitSparkplugShiftFast(as, instr, tc.op, deoptStub)
			if as.Pos() == 0 {
				t.Error("shift fast should emit code")
			}
		})
	}
}

func TestEmitSparkplugLogicalNot(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	as.Bind(deoptStub)
	instr := &js.Instruction{Op: js.OpLogicalNot, OperandA: 0}
	emitSparkplugLogicalNot(as, instr, deoptStub)
	if as.Pos() == 0 {
		t.Error("logical not should emit code")
	}
}

func TestEmitSparkplugStrictEq(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	as.Bind(deoptStub)
	instr := &js.Instruction{Op: js.OpStrictEq, OperandA: 1}
	emitSparkplugStrictEq(as, instr, deoptStub)
	if as.Pos() == 0 {
		t.Error("strict eq should emit code")
	}
}

func TestEmitSparkplugGreaterThan(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	as.Bind(deoptStub)
	instr := &js.Instruction{Op: js.OpGreaterThan, OperandA: 1}
	emitSparkplugGreaterThan(as, instr, deoptStub)
	if as.Pos() == 0 {
		t.Error("greater than should emit code")
	}
}

func TestEmitSparkplugToBoolean(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	as.Bind(deoptStub)
	instr := &js.Instruction{Op: js.OpToBoolean, OperandA: 0}
	emitSparkplugToBoolean(as, instr, deoptStub)
	if as.Pos() == 0 {
		t.Error("to boolean should emit code")
	}
}

func TestEmitSparkplugTypeof(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	instr := &js.Instruction{Op: js.OpTypeof, OperandA: 0}
	emitSparkplugTypeof(as, instr)
	if as.Pos() == 0 {
		t.Error("typeof should emit code")
	}
}

func TestEmitSparkplugStaNamedProperty(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	as.Bind(deoptStub)
	icSlotOffsets := make([]int, 16)
	instr := &js.Instruction{Op: js.OpStaNamedProperty, OperandA: 0, OperandB: 1, OperandC: 0}
	emitSparkplugStaNamedProperty(as, instr, icSlotOffsets, deoptStub)
	if as.Pos() == 0 {
		t.Error("sta named property should emit code")
	}
}

// ============================================================================
// Additional sparkplug emit function tests (compare ops, number arithmetic).
// ============================================================================

func TestEmitSparkplugCompareOpsBatch(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
		op   js.Opcode
	}{
		{"eq", emitSparkplugEq, js.OpEq},
		{"notEq", emitSparkplugNotEq, js.OpNotEq},
		{"strictNotEq", emitSparkplugStrictNotEq, js.OpStrictNotEq},
		{"lessEq", emitSparkplugLessEq, js.OpLessEq},
		{"greaterEq", emitSparkplugGreaterEq, js.OpGreaterEq},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: tc.op, OperandA: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugNumberArithBatch(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
		op   js.Opcode
	}{
		{"addNumber", emitSparkplugAddNumber, js.OpAdd},
		{"subNumber", emitSparkplugSubNumber, js.OpSub},
		{"mulNumber", emitSparkplugMulNumber, js.OpMul},
		{"divNumber", emitSparkplugDivNumber, js.OpDiv},
		{"modNumber", emitSparkplugModNumber, js.OpMod},
		{"negateNumber", emitSparkplugNegateNumber, js.OpNegate},
		{"incNumber", emitSparkplugIncNumber, js.OpInc},
		{"decNumber", emitSparkplugDecNumber, js.OpDec},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: tc.op, OperandA: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugNativeOpsBatch(t *testing.T) {
	buf, _ := NewCodeBuf(1024)
	as := NewAssembler(buf)
	deoptStub := NewLabel()

	emitSparkplugLdaUndefinedNative(as)
	emitSparkplugLdaNullNative(as)
	emitSparkplugLdaTrueNative(as)
	emitSparkplugLdaFalseNative(as)
	emitSparkplugLdaTrueFast(as, deoptStub)
	emitSparkplugLdaFalseFast(as, deoptStub)

	instr := &js.Instruction{Op: js.OpMov, OperandA: 0, OperandB: 1}
	emitSparkplugMovNative(as, instr)
	emitSparkplugDupNative(as, instr)
	emitSparkplugLdaThisNative(as, instr)

	if as.Pos() == 0 {
		t.Error("native ops should emit code")
	}
}

func TestEmitSparkplugFastPathBatch(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
		op   js.Opcode
	}{
		{"toNumber", emitSparkplugToNumberFast, js.OpToNumber},
		{"toString", emitSparkplugToStringFast, js.OpToString},
		{"typeof", emitSparkplugTypeofFast, js.OpTypeof},
		{"exp", emitSparkplugExpFast, js.OpExp},
		{"logicalAnd", emitSparkplugLogicalAndFast, js.OpLogicalAnd},
		{"logicalOr", emitSparkplugLogicalOrFast, js.OpLogicalOr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: tc.op, OperandA: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugMiscOpsBatch(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
		op   js.Opcode
	}{
		{"instanceof", emitSparkplugInstanceofFast, js.OpInstanceof},
		{"in", emitSparkplugInFast, js.OpIn},
		{"stringConcat", emitSparkplugStringConcat, js.OpAdd},
		{"arrayLength", emitSparkplugArrayLength, js.OpLdaNamedProperty},
		{"ldaPropByOffset", emitSparkplugLdaPropByOffset, js.OpLdaNamedProperty},
		{"callBuiltin", emitSparkplugCallBuiltin, js.OpCall},
		{"callDirect", emitSparkplugCallDirect, js.OpCall},
		{"ldaKeyedProp", emitSparkplugLdaKeyedPropertyFast, js.OpLdaKeyedProperty},
		{"staKeyedProp", emitSparkplugStaKeyedPropertyFast, js.OpStaKeyedProperty},
		{"deleteFast", emitSparkplugDeleteFast, js.OpDelete},
		{"deleteKeyed", emitSparkplugDeleteKeyedFast, js.OpDeleteKeyed},
		{"staPropByOffset", emitSparkplugStaPropByOffsetNative, js.OpStaNamedProperty},
		{"arrayGetIndex", emitSparkplugArrayGetIndex, js.OpLdaKeyedProperty},
		{"arraySetIndex", emitSparkplugArraySetIndex, js.OpStaKeyedProperty},
		{"createEmptyArray", emitSparkplugCreateEmptyArray, js.OpCreateArray},
		{"ldaGlobalDirect", emitSparkplugLdaGlobalDirect, js.OpLdaGlobal},
		{"staGlobalDirect", emitSparkplugStaGlobalDirect, js.OpStaGlobal},
		{"mathAbs", emitSparkplugMathAbs, js.OpLdaNamedProperty},
		{"mathFloor", emitSparkplugMathFloor, js.OpLdaNamedProperty},
		{"mathCeil", emitSparkplugMathCeil, js.OpLdaNamedProperty},
		{"mathSqrt", emitSparkplugMathSqrt, js.OpLdaNamedProperty},
		{"stringLength", emitSparkplugStringLength, js.OpLdaNamedProperty},
		{"stringEq", emitSparkplugStringEq, js.OpStrictEq},
		{"pushContext", emitSparkplugPushContext, js.OpCreateClosure},
		{"popContext", emitSparkplugPopContext, js.OpReturn},
		{"loadContextSlot", emitSparkplugLoadContextSlot, js.OpLdaCaptured},
		{"storeContextSlot", emitSparkplugStoreContextSlot, js.OpStaNamedProperty},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: tc.op, OperandA: 0, OperandB: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugNumberCompareBatch(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
		op   js.Opcode
	}{
		{"strictEqNumber", emitSparkplugStrictEqNumber, js.OpStrictEq},
		{"strictNotEqNumber", emitSparkplugStrictNotEqNumber, js.OpStrictNotEq},
		{"lessThanNumber", emitSparkplugLessThanNumber, js.OpLessThan},
		{"greaterThanNumber", emitSparkplugGreaterThanNumber, js.OpGreaterThan},
		{"lessEqNumber", emitSparkplugLessEqNumber, js.OpLessEq},
		{"greaterEqNumber", emitSparkplugGreaterEqNumber, js.OpGreaterEq},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: tc.op, OperandA: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugBitShiftBatch(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
		op   js.Opcode
	}{
		{"bitAndNumber", emitSparkplugBitAndNumber, js.OpBitwiseAnd},
		{"bitOrNumber", emitSparkplugBitOrNumber, js.OpBitwiseOr},
		{"bitXorNumber", emitSparkplugBitXorNumber, js.OpBitwiseXor},
		{"bitNotNumber", emitSparkplugBitNotNumber, js.OpBitwiseNot},
		{"shiftLeftNumber", emitSparkplugShiftLeftNumber, js.OpShiftLeft},
		{"shiftRightNumber", emitSparkplugShiftRightNumber, js.OpShiftRight},
		{"shiftRightZeroNumber", emitSparkplugShiftRightZeroNumber, js.OpShiftRightZero},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: tc.op, OperandA: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugMiscFast2(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
		op   js.Opcode
	}{
		{"toStringNumber", emitSparkplugToStringNumber, js.OpToString},
		{"toBooleanNumber", emitSparkplugToBooleanNumber, js.OpToBoolean},
		{"cmpNumber", emitSparkplugCmpNumber, js.OpLessThan},
		{"swap", emitSparkplugSwap, js.OpMov},
		{"ldaCapturedNative", emitSparkplugLdaCapturedNative, js.OpLdaCaptured},
		{"lessThan", emitSparkplugLessThan, js.OpLessThan},
		{"modFast", emitSparkplugModFast, js.OpMod},
		{"incNative", emitSparkplugIncNative, js.OpInc},
		{"decNative", emitSparkplugDecNative, js.OpDec},
		{"staByOffsetFast", emitSparkplugStaByOffsetFast, js.OpStaNamedProperty},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: tc.op, OperandA: 0, OperandB: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugSimpleNativeOps(t *testing.T) {
	buf, _ := NewCodeBuf(1024)
	as := NewAssembler(buf)

	instr := &js.Instruction{Op: js.OpLdaSmi, OperandA: 42}
	emitSparkplugLdaSmiNative(as, instr)
	emitSparkplugLdaZeroNative(as)
	emitSparkplugLdaOneNative(as)
	emitSparkplugLdaConstantNative(as, instr)
	emitSparkplugLdarNative(as, instr)

	emitSparkplugLdaGlobalSlot(as, instr)
	emitSparkplugStaGlobalSlot(as, instr)

	emitSparkplugSetTryHandlerNative(as, instr)
	emitSparkplugClearTryHandlerNative(as)
	emitSparkplugSetFinallyHandlerNative(as, instr)

	if as.Pos() == 0 {
		t.Error("simple native ops should emit code")
	}
}

func TestEmitSparkplugCallThrow(t *testing.T) {
	t.Run("callSpread", func(t *testing.T) {
		buf, _ := NewCodeBuf(1024)
		as := NewAssembler(buf)
		deoptStub := NewLabel()
		as.Bind(deoptStub)
		emitSparkplugCallSpreadFast(as, &js.Instruction{Op: js.OpCallSpread, OperandA: 1, OperandB: 1}, deoptStub)
		if as.Pos() == 0 {
			t.Error("callSpread should emit code")
		}
	})
	t.Run("throw", func(t *testing.T) {
		buf, _ := NewCodeBuf(1024)
		as := NewAssembler(buf)
		deoptStub := NewLabel()
		as.Bind(deoptStub)
		emitSparkplugThrowFast(as, &js.Instruction{Op: js.OpThrow, OperandA: 0}, deoptStub)
		if as.Pos() == 0 {
			t.Error("throw should emit code")
		}
	})
}

func TestEmitSparkplugForIn(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Assembler, *js.Instruction, *Label)
	}{
		{"forInSetup", emitSparkplugForInSetupFast},
		{"forInNext", emitSparkplugForInNextFast},
		{"forInSetupExt", emitSparkplugForInSetupFastExt},
		{"forInNextExt", emitSparkplugForInNextFastExt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := NewCodeBuf(1024)
			as := NewAssembler(buf)
			deoptStub := NewLabel()
			as.Bind(deoptStub)
			tc.fn(as, &js.Instruction{Op: js.OpForInSetup, OperandA: 1}, deoptStub)
			if as.Pos() == 0 {
				t.Errorf("%s should emit code", tc.name)
			}
		})
	}
}

func TestEmitSparkplugStaByOffset(t *testing.T) {
	buf, _ := NewCodeBuf(1024)
	as := NewAssembler(buf)
	instr := &js.Instruction{Op: js.OpStaNamedProperty, OperandA: 0, OperandB: 1}
	emitSparkplugStaByOffset(as, instr)
	if as.Pos() == 0 {
		t.Error("staByOffset should emit code")
	}
}

func TestEmitSparkplugGlobalNative(t *testing.T) {
	buf, _ := NewCodeBuf(1024)
	as := NewAssembler(buf)
	deoptStub := NewLabel()
	emitSparkplugLdaGlobalNative(as, &js.Instruction{Op: js.OpLdaGlobal, OperandA: 0}, deoptStub)
	emitSparkplugStaGlobalNative(as, &js.Instruction{Op: js.OpStaGlobal, OperandA: 0}, deoptStub)
	if as.Pos() == 0 {
		t.Error("global native should emit code")
	}
}
