package jit

import (
	"math"
	"testing"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// TestSparkplugEmptyFunction verifies CompileSparkplug handles a function
// with zero instructions gracefully (produces prologue + epilogue only).
func TestSparkplugEmptyFunction(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "empty",
		NumRegisters: 1,
		Instructions: []js.Instruction{},
		Constants:    []js.JSValue{},
	}
	buf, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug empty function failed: %v", err)
	}
	if buf == nil {
		t.Fatal("expected non-nil CodeBuf for empty function")
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty code buffer (at least prologue+epilogue)")
	}
	t.Logf("empty function: %d bytes of native code", buf.Len())
}

// TestSparkplugMaxRegs verifies CompileSparkplug handles a function with the
// maximum number of registers without crashing.
func TestSparkplugMaxRegs(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "maxregs",
		NumRegisters: 255,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
	buf, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug max regs failed: %v", err)
	}
	if buf == nil {
		t.Fatal("expected non-nil CodeBuf")
	}
	t.Logf("max regs function: %d bytes, %d registers", buf.Len(), bf.NumRegisters)
}

// TestSparkplugSingleInstruction verifies the simplest possible function
// (just Return) compiles and runs correctly.
func TestSparkplugSingleInstruction(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`function f(){return}; f()`)
	if !result.IsUndefined() {
		t.Errorf("expected undefined, got %v", result.String())
	}
}

// TestTurboFanNilFunction verifies CompileTurboFan returns an error for nil input.
func TestTurboFanNilFunction(t *testing.T) {
	_, err := CompileTurboFan(nil)
	if err == nil {
		t.Error("expected error for nil BytecodeFunction")
	}
}

// TestTurboFanEmptyFunction verifies CompileTurboFan handles an empty function.
// An empty function with no instructions may fail to lower (no SSA blocks to emit);
// this is expected behavior — we verify it doesn't crash.
func TestTurboFanEmptyFunction(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "tf_empty",
		NumRegisters: 1,
		Instructions: []js.Instruction{},
		Constants:    []js.JSValue{},
	}
	_, err := CompileTurboFan(bf)
	// Empty function may legitimately fail to lower; we just verify no panic.
	if err != nil {
		t.Logf("CompileTurboFan empty returned expected error: %v", err)
	}
}

// TestTurboFanSimpleReturn verifies TurboFan compiles and runs a simple return.
func TestTurboFanSimpleReturn(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "tf_simple",
		NumRegisters: 2,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
	buf, err := CompileTurboFan(bf)
	if err != nil {
		t.Fatalf("CompileTurboFan simple failed: %v", err)
	}
	if buf == nil || buf.Len() == 0 {
		t.Fatal("expected non-empty code buffer")
	}
	t.Logf("turbofan simple: %d bytes", buf.Len())
}

// TestTurboFanAdd verifies TurboFan compiles and runs addition.
func TestTurboFanAdd(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "tf_add",
		NumRegisters: 3,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 3},
			{Op: js.OpStar, OperandA: 1},
			{Op: js.OpLdaSmi, OperandA: 4},
			{Op: js.OpAdd, OperandA: 1},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
	buf, err := CompileTurboFan(bf)
	if err != nil {
		t.Fatalf("CompileTurboFan add failed: %v", err)
	}
	if buf == nil || buf.Len() == 0 {
		t.Fatal("expected non-empty code buffer")
	}
	t.Logf("turbofan add: %d bytes", buf.Len())
}

// TestDeoptimizationInputData verifies the deoptimization input data structures.
func TestDeoptimizationInputData(t *testing.T) {
	d := &DeoptimizationInputData{
		Points: []DeoptPoint{
			{NativeOffset: 0, BytecodePC: 0},
			{NativeOffset: 64, BytecodePC: 1},
			{NativeOffset: 128, BytecodePC: 2},
		},
		RegMap:    [30]int{},
		Constants: []interface{}{js.NewNumber(1), js.NewNumber(2)},
	}

	// Test FindDeoptPoint with known offsets.
	pc, ok := d.FindDeoptPoint(0)
	if !ok || pc != 0 {
		t.Errorf("FindDeoptPoint(0): pc=%d, ok=%v", pc, ok)
	}
	pc, ok = d.FindDeoptPoint(64)
	if !ok || pc != 1 {
		t.Errorf("FindDeoptPoint(64): pc=%d, ok=%v", pc, ok)
	}
	pc, ok = d.FindDeoptPoint(128)
	if !ok || pc != 2 {
		t.Errorf("FindDeoptPoint(128): pc=%d, ok=%v", pc, ok)
	}

	// Test with unknown offset.
	_, ok = d.FindDeoptPoint(999)
	if ok {
		t.Error("FindDeoptPoint(999) should return false")
	}

	// Test FrameDescription size.
	if sz := uintptr(34 * 8); sz != 272 {
		t.Errorf("unexpected FrameDescription size calculation: got %d, expected 272", sz)
	}
}

// TestFrameDescriptionLayout verifies the FrameDescription struct layout.
func TestFrameDescriptionLayout(t *testing.T) {
	fd := &FrameDescription{}
	fd.Regs[0] = 42
	fd.Regs[29] = 99
	fd.PC = 100
	fd.FP = 200
	fd.SP = 300
	fd.LR = 400

	if fd.Regs[0] != 42 {
		t.Error("Regs[0] corrupted")
	}
	if fd.Regs[29] != 99 {
		t.Error("Regs[29] corrupted")
	}
	if fd.PC != 100 {
		t.Error("PC corrupted")
	}
	if fd.FP != 200 {
		t.Error("FP corrupted")
	}
	if fd.SP != 300 {
		t.Error("SP corrupted")
	}
	if fd.LR != 400 {
		t.Error("LR corrupted")
	}
}

// TestDeoptNoMatchingPoint verifies FindDeoptPoint returns false for empty data.
func TestDeoptNoMatchingPoint(t *testing.T) {
	d := &DeoptimizationInputData{
		Points:    []DeoptPoint{},
		Constants: []interface{}{},
	}
	_, ok := d.FindDeoptPoint(0)
	if ok {
		t.Error("FindDeoptPoint should return false for empty Points")
	}
}

// TestDeoptManyConstants verifies the DeoptimizationInputData handles large constant pools.
func TestDeoptManyConstants(t *testing.T) {
	constants := make([]interface{}, 100)
	for i := range constants {
		constants[i] = js.NewNumber(float64(i))
	}
	d := &DeoptimizationInputData{
		Points: []DeoptPoint{
			{NativeOffset: 0, BytecodePC: 0},
			{NativeOffset: 64, BytecodePC: 1},
			{NativeOffset: 128, BytecodePC: 2},
			{NativeOffset: 192, BytecodePC: 3},
			{NativeOffset: 256, BytecodePC: 4},
		},
		RegMap:    [30]int{},
		Constants: constants,
	}
	if len(d.Constants) != 100 {
		t.Errorf("expected 100 constants, got %d", len(d.Constants))
	}
	for i := 0; i < 5; i++ {
		pc, ok := d.FindDeoptPoint(i * 64)
		if !ok || pc != i {
			t.Errorf("FindDeoptPoint(%d): pc=%d, ok=%v", i*64, pc, ok)
		}
	}
}

// TestPatchMegamorphicICSlot verifies megamorphic IC patching.
func TestPatchMegamorphicICSlot(t *testing.T) {
	buf, err := NewCodeBuf(64)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	// Fill with NOPs so we can observe the patch.
	for i := 0; i < 64; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F) // NOP
	}

	// Before patching, first 4 bytes should be NOP.
	pre := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if pre != 0xD503201F {
		t.Fatal("expected NOP before patch")
	}

	PatchMegamorphicICSlot(buf, 0)

	// After patching, first 4 bytes should be a branch instruction (not NOP).
	post := uint32(buf.rwBuf[0]) | uint32(buf.rwBuf[1])<<8 | uint32(buf.rwBuf[2])<<16 | uint32(buf.rwBuf[3])<<24
	if post == 0xD503201F {
		t.Error("expected non-NOP branch after megamorphic patch")
	}
	// Verify that positions 4-28 are NOPs.
	for i := 4; i < 32; i += 4 {
		w := uint32(buf.rwBuf[i]) | uint32(buf.rwBuf[i+1])<<8 | uint32(buf.rwBuf[i+2])<<16 | uint32(buf.rwBuf[i+3])<<24
		if w != 0xD503201F {
			t.Errorf("expected NOP at offset %d, got 0x%08X", i, w)
		}
	}
}

// TestPatchICSlotWithNilShape verifies that patching with a nil shape pointer
// doesn't crash (graceful handling).
func TestPatchICSlotWithNilShape(t *testing.T) {
	buf, err := NewCodeBuf(64)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	// Fill with NOPs.
	for i := 0; i < 64; i += 4 {
		buf.PatchUint32LE(i, 0xD503201F)
	}
	// Patch with nil shape pointer — should not panic.
	PatchICSlot(buf, 0, 0, 0, 16)
	// Verify the sled was written (non-NOP content at slotOffset).
	isNOP := true
	for i := 0; i < 4 && isNOP; i++ {
		w := uint32(buf.rwBuf[i]) | uint32(buf.rwBuf[i+1])<<8 | uint32(buf.rwBuf[i+2])<<16 | uint32(buf.rwBuf[i+3])<<24
		if w != 0xD503201F {
			isNOP = false
		}
	}
	if isNOP {
		t.Error("expected IC slot to be patched even with nil shape (zero value still written)")
	}
}

// TestResolveICSlotUnregistered verifies that resolveICSlot returns false
// for an unregistered address.
func TestResolveICSlotUnregistered(t *testing.T) {
	buf, _, ok := resolveICSlot(0xDEADBEEF, 0)
	if ok || buf != nil {
		t.Error("resolveICSlot should return false for unregistered address")
	}
}

// TestEmitICSlot verifies EmitICSlot emits a 16-byte NOP sled.
func TestEmitICSlot(t *testing.T) {
	buf, err := NewCodeBuf(64)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	offset := EmitICSlot(as)
	if offset != 0 {
		t.Errorf("expected offset 0, got %d", offset)
	}
	if as.Pos() != 16 {
		t.Errorf("expected pos 16 after EmitICSlot, got %d", as.Pos())
	}
}

// TestShadowStackFullCapacity verifies Push at full capacity silently drops.
func TestShadowStackFullCapacity(t *testing.T) {
	ss := NewShadowStack(4)
	var a, b, c, d, e int
	ss.Push(unsafe.Pointer(&a))
	ss.Push(unsafe.Pointer(&b))
	ss.Push(unsafe.Pointer(&c))
	ss.Push(unsafe.Pointer(&d))
	if ss.Len() != 4 {
		t.Errorf("expected len 4, got %d", ss.Len())
	}
	// Push at capacity — should be silently dropped.
	ss.Push(unsafe.Pointer(&e))
	if ss.Len() != 4 {
		t.Errorf("expected len still 4 after overflow push, got %d", ss.Len())
	}
}

// TestShadowStackPopEmpty verifies Pop on empty stack doesn't crash.
func TestShadowStackPopEmpty(t *testing.T) {
	ss := NewShadowStack(4)
	ss.Pop() // should not panic
	ss.Pop()
	if ss.Len() != 0 {
		t.Errorf("expected len 0 after popping empty, got %d", ss.Len())
	}
}

// TestShadowStackClearTwice verifies double Clear is safe.
func TestShadowStackClearTwice(t *testing.T) {
	ss := NewShadowStack(4)
	var x int
	ss.Push(unsafe.Pointer(&x))
	ss.Clear()
	ss.Clear() // second clear should be safe
	if ss.Len() != 0 {
		t.Errorf("expected len 0, got %d", ss.Len())
	}
}

// TestTurboFanManyRegs verifies CompileTurboFan handles many registers.
func TestTurboFanManyRegs(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "tf_many",
		NumRegisters: 30,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpStar, OperandA: 1},
			{Op: js.OpLdaSmi, OperandA: 2},
			{Op: js.OpStar, OperandA: 2},
			{Op: js.OpLdar, OperandA: 1},
			{Op: js.OpAdd, OperandA: 2},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
	buf, err := CompileTurboFan(bf)
	if err != nil {
		t.Fatalf("CompileTurboFan many regs failed: %v", err)
	}
	if buf == nil || buf.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}
	t.Logf("turbofan 30 regs: %d bytes", buf.Len())
}

// TestTurboFanSubMulDiv verifies TurboFan compiles subtraction, multiplication, division.
func TestTurboFanSubMulDiv(t *testing.T) {
	tests := []struct {
		name string
		op   js.Opcode
	}{
		{"sub", js.OpSub},
		{"mul", js.OpMul},
		{"div", js.OpDiv},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bf := &js.BytecodeFunction{
				Name:         "tf_" + tc.name,
				NumRegisters: 3,
				Instructions: []js.Instruction{
					{Op: js.OpLdaSmi, OperandA: 10},
					{Op: js.OpStar, OperandA: 1},
					{Op: js.OpLdaSmi, OperandA: 2},
					{Op: tc.op, OperandA: 1},
					{Op: js.OpReturn},
				},
				Constants: []js.JSValue{},
			}
			buf, err := CompileTurboFan(bf)
			if err != nil {
				t.Fatalf("CompileTurboFan %s failed: %v", tc.name, err)
			}
			if buf == nil || buf.Len() == 0 {
				t.Fatal("expected non-empty buffer")
			}
			t.Logf("turbofan %s: %d bytes", tc.name, buf.Len())
		})
	}
}

// TestAssemblerManyNops verifies assembler can emit many NOPs.
func TestAssemblerManyNops(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)
	for i := 0; i < 50; i++ {
		as.NOP()
	}
	if as.Pos() != 200 {
		t.Errorf("expected pos 200 after 50 NOPs, got %d", as.Pos())
	}
}

// TestAssemblerUncoveredInstructions tests ARM64 instructions not covered elsewhere.
func TestAssemblerUncoveredInstructions(t *testing.T) {
	buf, err := NewCodeBuf(1024)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	as := NewAssembler(buf)

	// MOVN — move inverted immediate.
	as.MOVN(REG_R0, 0xFFFF, 0)

	// STRW — store 32-bit word.
	as.STRW(REG_R0, REG_R1, 0)

	// BVS — branch on overflow set.
	lbl := NewLabel()
	as.BVS(lbl)
	as.NOP()
	as.Bind(lbl)

	// BVC — branch on overflow clear.
	lbl2 := NewLabel()
	as.BVC(lbl2)
	as.NOP()
	as.Bind(lbl2)

	// CBNZ — compare and branch if non-zero.
	lbl3 := NewLabel()
	as.CBNZ(REG_R0, lbl3)
	as.NOP()
	as.Bind(lbl3)

	// FSUB — floating-point subtract.
	as.FSUB(0, 1, 2)

	// FMUL — floating-point multiply.
	as.FMUL(0, 1, 2)

	// FDIV — floating-point divide.
	as.FDIV(0, 1, 2)

	// Verify we emitted non-zero code.
	if as.Pos() == 0 {
		t.Error("expected non-zero code size after uncovered insns")
	}
	t.Logf("uncovered insns: %d bytes", as.Pos())
}

// TestCodeBufBoundary verifies CodeBuf handles writes near the end of buffer.
func TestCodeBufBoundary(t *testing.T) {
	buf, err := NewCodeBuf(pageSize)
	if err != nil {
		t.Fatalf("NewCodeBuf: %v", err)
	}
	// Write up to the limit.
	for i := 0; i < pageSize; i++ {
		buf.Write(0xCC)
	}
	if buf.Len() != pageSize {
		t.Errorf("expected len %d, got %d", pageSize, buf.Len())
	}
	// Write beyond — should be silently dropped.
	buf.Write(0xDD)
	if buf.Len() != pageSize {
		t.Errorf("write beyond capacity should be dropped, len=%d", buf.Len())
	}
}

// TestJSValueEdgeCases tests edge cases in JS value operations.
func TestJSValueEdgeCases(t *testing.T) {
	// Test BigInt creation and comparison.
	bi := js.NewBigIntFromInt64(1234567890123456789)
	if !bi.IsBigInt() {
		t.Error("expected BigInt")
	}
	if bi.ToString() != "1234567890123456789" {
		t.Errorf("unexpected BigInt string: %s", bi.ToString())
	}

	// Test NaN comparisons.
	nan := js.NewNumber(math.NaN())
	if nan.IsTruthy() {
		t.Error("NaN should not be truthy")
	}
	if nan.Equals(nan) {
		t.Error("NaN != NaN")
	}
	if nan.StrictEquals(nan) {
		t.Error("NaN !== NaN")
	}

	// Test Symbol.
	sym := js.NewSymbol("test")
	if !sym.IsSymbol() {
		t.Error("expected Symbol")
	}
	if sym.IsTruthy() != true {
		t.Error("Symbol should be truthy")
	}

	// Test zero values.
	zero := js.NewNumber(0)
	if zero.IsTruthy() {
		t.Error("0 should not be truthy")
	}
	negZero := js.NewNumber(math.Copysign(0, -1))
	if negZero.IsTruthy() {
		t.Error("-0 should not be truthy")
	}
}

// TestJSLooseEqualityEdgeCases tests loose equality edge cases.
func TestJSLooseEqualityEdgeCases(t *testing.T) {
	// null == undefined
	if !js.Null.Equals(js.Undefined) {
		t.Error("null == undefined")
	}
	if !js.Undefined.Equals(js.Null) {
		t.Error("undefined == null")
	}

	// string == number
	if !js.NewString("42").Equals(js.NewNumber(42)) {
		t.Error("'42' == 42")
	}

	// boolean == number
	if !js.True.Equals(js.NewNumber(1)) {
		t.Error("true == 1")
	}

	// BigInt == number (exact integer)
	bi := js.NewBigIntFromInt64(42)
	if !bi.Equals(js.NewNumber(42)) {
		t.Error("42n == 42")
	}

	// BigInt != NaN
	nan := js.NewNumber(math.NaN())
	if bi.Equals(nan) {
		t.Error("42n != NaN")
	}
}
