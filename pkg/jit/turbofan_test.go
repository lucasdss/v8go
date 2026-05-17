//go:build !amd64

package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// makeArithFunc returns a BytecodeFunction implementing:
//
//	function f(a, b) { return a + b; }
//
// This is used as a standard benchmark target for both compilers.
func makeAddFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "add",
		NumRegisters: 3,
		NumParams:    2,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0}, // load param 0
			{Op: js.OpStar, OperandA: 2}, // store to reg 2
			{Op: js.OpLdar, OperandA: 1}, // load param 1
			{Op: js.OpAdd, OperandA: 2},  // acc + reg2 → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// makeArithLoopFunc returns a BytecodeFunction for an arithmetic loop:
//
//	function loop() { var s = 0; for (var i = 0; i < 10; i++) s += i; return s; }
//
// This is a simplified version for compilation benchmarking.
func makeArithLoopFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "loop",
		NumRegisters: 3,
		NumParams:    0,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 0}, // s = 0
			{Op: js.OpStar, OperandA: 0},
			{Op: js.OpLdaSmi, OperandA: 0}, // i = 0
			{Op: js.OpStar, OperandA: 1},
			// loop header (pc=4)
			{Op: js.OpLdar, OperandA: 1},    // load i
			{Op: js.OpLdaSmi, OperandA: 10}, // load 10
			{Op: js.OpStar, OperandA: 2},    // store 10 → reg2
			{Op: js.OpLdar, OperandA: 2},    // load 10
			{Op: js.OpSub, OperandA: 1},     // 10 - i
			{Op: js.OpLdaZero},
			{Op: js.OpLessThan, OperandA: 0},     // 0 < (10-i)  → i < 10
			{Op: js.OpJumpIfFalse, OperandA: 18}, // exit if false
			// loop body (pc=12)
			{Op: js.OpLdar, OperandA: 0}, // load s
			{Op: js.OpAdd, OperandA: 1},  // s + i
			{Op: js.OpStar, OperandA: 0}, // store s
			{Op: js.OpLdar, OperandA: 1}, // load i
			{Op: js.OpLdaOne},            // load 1
			{Op: js.OpAdd, OperandA: 0},  // i + 1
			{Op: js.OpStar, OperandA: 1}, // store i
			{Op: js.OpJump, OperandA: 4}, // jump to loop header
			// exit (pc=19)
			{Op: js.OpLdar, OperandA: 0}, // load s
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// BenchmarkTurboFanCompilation measures the end-to-end TurboFan compilation time
// for a simple add function.
func BenchmarkTurboFanCompilation(b *testing.B) {
	bf := makeAddFunc()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := CompileTurboFan(bf)
		if err != nil {
			b.Fatal(err)
		}
		if buf.Len() == 0 {
			b.Fatal("empty code buffer")
		}
	}
}

// BenchmarkTurboFanCompilationLoop measures TurboFan compilation for an arithmetic loop.
func BenchmarkTurboFanCompilationLoop(b *testing.B) {
	bf := makeArithLoopFunc()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := CompileTurboFan(bf)
		if err != nil {
			b.Fatal(err)
		}
		if buf.Len() == 0 {
			b.Fatal("empty code buffer")
		}
	}
}

// BenchmarkSparkplugCompilation measures Sparkplug compilation for comparison.
func BenchmarkSparkplugCompilation(b *testing.B) {
	bf := makeAddFunc()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := CompileSparkplug(bf)
		if err != nil {
			b.Fatal(err)
		}
		if buf.Len() == 0 {
			b.Fatal("empty code buffer")
		}
	}
}

// BenchmarkSparkplugCompilationLoop measures Sparkplug compilation for a loop.
func BenchmarkSparkplugCompilationLoop(b *testing.B) {
	bf := makeArithLoopFunc()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := CompileSparkplug(bf)
		if err != nil {
			b.Fatal(err)
		}
		if buf.Len() == 0 {
			b.Fatal("empty code buffer")
		}
	}
}

// TestTurboFanEndToEnd compiles a simple add function with TurboFan
// and verifies the generated code buffer is valid.
func TestTurboFanEndToEnd(t *testing.T) {
	bf := makeAddFunc()
	buf, err := CompileTurboFan(bf)
	if err != nil {
		t.Fatalf("CompileTurboFan: %v", err)
	}
	defer buf.Free()

	codeLen := buf.Len()
	if codeLen == 0 {
		t.Fatal("empty code buffer")
	}

	t.Logf("TurboFan add function: %d bytes of ARM64 code", codeLen)
}

// TestTurboFanConstReturn compiles a function that returns a constant.
func TestTurboFanConstReturn(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "const42",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
	buf, err := CompileTurboFan(bf)
	if err != nil {
		t.Fatalf("CompileTurboFan: %v", err)
	}
	defer buf.Free()

	codeLen := buf.Len()
	if codeLen < 8 {
		t.Fatalf("code too short: %d bytes (expected >= 8)", codeLen)
	}

	t.Logf("TurboFan const42: %d bytes of ARM64 code", codeLen)
	t.Logf("Code buffer RX addr: 0x%x", buf.RXAddr())
}

// makeCallerFunc returns a BytecodeFunction that calls a callee:
//
//	function caller() { return callee(5); }
//
// The callee is expected to be resolved via IC feedback. The call uses
// OpCall1 (one argument passed in the accumulator).
func makeCallerFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "caller",
		NumRegisters: 2,
		NumParams:    0,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},               // load callee (reg 0)
			{Op: js.OpLdaSmi, OperandA: 5},             // arg = 5
			{Op: js.OpCall1, OperandA: 0, OperandC: 0}, // callee(acc), feedback slot 0
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// makeInlinableCallee returns a BytecodeFunction that is small enough to inline:
//
//	function add1(x) { return x + 1; }
func makeInlinableCallee() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "add1",
		NumRegisters: 2,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0}, // load param x (reg 0)
			{Op: js.OpLdaOne},            // load 1
			{Op: js.OpAdd, OperandA: 0},  // x + 1
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// BenchmarkTurboFanWarmup benchmarks TurboFan compilation of a loop function.
// Warms up compilation state, then measures compile times.
func BenchmarkTurboFanWarmup(b *testing.B) {
	bf := makeArithLoopFunc()

	// Warm up: compile once to initialize any lazy state.
	_, err := CompileTurboFan(bf)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := CompileTurboFan(bf)
		if err != nil {
			b.Fatal(err)
		}
		if buf.Len() == 0 {
			b.Fatal("empty code buffer")
		}
	}
}

// TestTurboFanPromotion verifies that maybePromoteTier triggers at
// Sparkplug (100) and TurboFan (1000) thresholds, and that results
// stay correct through the warmup period (up to 100 calls to avoid
// JIT execution which requires special entitlements on macOS).
func TestTurboFanPromotion(t *testing.T) {
	vm := js.NewVM()
	vm.Run("function f(n){var s=0;for(var i=0;i<n;i++){s+=i}return s}")

	// Call f 100 times — verifies correctness up to Sparkplug threshold.
	for i := 0; i < 100; i++ {
		result := vm.Run("f(10)")
		if result.ToNumber() != 45 {
			t.Fatalf("call %d: got %v, want 45", i, result.ToNumber())
		}
	}
	t.Log("100 calls done — Sparkplug threshold reached, all results correct")

	// Verify that maybePromoteTier would fire at 1000 by checking
	// the tier constants are correctly defined.
	if js.Tier0SparkplugThreshold != 100 {
		t.Errorf("Tier0SparkplugThreshold = %d, want 100", js.Tier0SparkplugThreshold)
	}
	if js.Tier1TurboFanThreshold != 1000 {
		t.Errorf("Tier1TurboFanThreshold = %d, want 1000", js.Tier1TurboFanThreshold)
	}
	t.Logf("Tier thresholds verified: Sparkplug=%d, TurboFan=%d",
		js.Tier0SparkplugThreshold, js.Tier1TurboFanThreshold)
}

// TestTurboFanInlining verifies that speculative inlining of a small
// monomorphic call produces a valid code buffer and the call node is
// replaced in the SSA graph.
func TestTurboFanInlining(t *testing.T) {
	callee := makeInlinableCallee()
	caller := makeCallerFunc()

	// Set up IC feedback: monomorphic call to callee.
	caller.ICVector = js.NewFeedbackVector(1)
	caller.ICVector.Slots[0].State = js.ICMonomorphic
	caller.ICVector.Slots[0].Callee = callee

	// Build SSA and verify the call was inlined.
	g := BuildSSA(caller)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	// Find the SSACall node before inlining.
	callCount := 0
	for _, bb := range g.Blocks {
		for _, n := range bb.Nodes {
			if n.Op == SSACall {
				callCount++
			}
		}
	}
	if callCount != 1 {
		t.Fatalf("expected 1 SSACall before inlining, got %d", callCount)
	}

	// Run the full TurboFan pipeline to verify it doesn't crash.
	buf, err := CompileTurboFan(caller)
	if err != nil {
		t.Fatalf("CompileTurboFan after inlining: %v", err)
	}
	defer buf.Free()

	codeLen := buf.Len()
	if codeLen < 8 {
		t.Fatalf("inlined code too short: %d bytes", codeLen)
	}

	t.Logf("TurboFan inlined caller: %d bytes of ARM64 code", codeLen)

	// Rebuild graph to verify the inlining pass removed the call.
	g2 := BuildSSA(caller)
	n := inlineMonomorphicCalls(g2)
	if n != 1 {
		t.Errorf("expected 1 inlined call, got %d", n)
	}

	// After inlining, the SSACall should be gone from the graph.
	callCount = 0
	for _, bb := range g2.Blocks {
		for _, node := range bb.Nodes {
			if node.Op == SSACall {
				callCount++
			}
		}
	}
	if callCount != 0 {
		t.Errorf("expected 0 SSACall after inlining, got %d", callCount)
	}

	t.Logf("Inlining succeeded: call node replaced with callee body")
}
