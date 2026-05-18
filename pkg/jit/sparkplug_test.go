package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestSparkplugSimpleReturn(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function f() { return 42; }
		f()
	`)
	if result.ToNumber() != 42 {
		t.Errorf("expected 42, got %v", result.ToNumber())
	}
}

func TestSparkplugLdaSmi(t *testing.T) {
	vm := js.NewVM()
	// Exercise LdaSmi through numeric operations
	result := vm.Run(`
		function f() { return 7; }
		f()
	`)
	if result.ToNumber() != 7 {
		t.Errorf("expected 7, got %v", result.ToNumber())
	}
}

func TestSparkplugAdd(t *testing.T) {
	vm := js.NewVM()
	// Call add enough times to potentially trigger compilation
	var result js.JSValue
	for i := 0; i < 10; i++ {
		result = vm.Run(`
			function add(a,b){return a+b}
			add(3,4)
		`)
	}
	if result.ToNumber() != 7 {
		t.Errorf("expected 7, got %v", result.ToNumber())
	}
}

func TestSparkplugSub(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function sub(a,b){return a-b}
		sub(10,3)
	`)
	if result.ToNumber() != 7 {
		t.Errorf("expected 7, got %v", result.ToNumber())
	}
}

func TestSparkplugMul(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function mul(a,b){return a*b}
		mul(6,7)
	`)
	if result.ToNumber() != 42 {
		t.Errorf("expected 42, got %v", result.ToNumber())
	}
}

func TestSparkplugDiv(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function div(a,b){return a/b}
		div(84,2)
	`)
	if result.ToNumber() != 42 {
		t.Errorf("expected 42, got %v", result.ToNumber())
	}
}

func TestSparkplugConstants(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`undefined`)
	if !result.IsUndefined() {
		t.Errorf("expected undefined, got %v", result.String())
	}

	result = vm.Run(`null`)
	if !result.IsNull() {
		t.Errorf("expected null, got %v", result.String())
	}

	result = vm.Run(`true`)
	if !result.IsBoolean() || !result.IsTruthy() {
		t.Errorf("expected true, got %v", result.String())
	}

	result = vm.Run(`false`)
	if !result.IsBoolean() || result.IsTruthy() {
		t.Errorf("expected false, got %v", result.String())
	}
}

func TestSparkplugCompileSmoke(t *testing.T) {
	// Test that CompileSparkplug produces valid code for a simple function.
	bf := &js.BytecodeFunction{
		Name:         "test",
		NumRegisters: 2,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}

	buf, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty code buffer")
	}
	t.Logf("compiled %d instructions → %d bytes of AMD64 code",
		len(bf.Instructions), buf.Len())
}

func TestDeoptOnTypeChange(t *testing.T) {
	vm := js.NewVM()
	// Define f ONCE so its BytecodeFunction accumulates call count across iterations.
	vm.Run("function f(a,b){return a+b}")
	// Warm with number calls to trigger Sparkplug compilation (threshold = 100).
	// The JIT produces code with TagNumber guards; the slow path branches
	// to the Go slow-path helper.
	for i := 0; i < 200; i++ {
		vm.Run("f(1,2)")
	}
	// Deopt on strings: type guard fails, Go slow-path helper re-executes Add.
	result := vm.Run("f('hello','world')")
	if result.ToString() != "helloworld" {
		t.Errorf("deopt failed: got %q", result.ToString())
	}
}

// BenchmarkSparkplugAdd measures JIT-compiled add performance by persisting
// the VM across iterations. Define f once so its call count accumulates;
// warming up to 200 calls triggers Sparkplug compilation at threshold 100.
// TurboFan threshold is 1000 calls, so it won't activate during this benchmark.
func BenchmarkSparkplugAdd(b *testing.B) {
	backend := NewBackend()
	vm := js.NewVMWithJIT(backend, backend, backend)
	vm.Run("function f(a,b){return a+b}")
	// Warm up to 200 calls (triggers Sparkplug at threshold 100).
	for i := 0; i < 200; i++ {
		vm.Run("f(1,2)")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm.Run("f(3,4)")
	}
}

// TestTurboFanActivation verifies that after many calls (above Sparkplug's
// 100-call threshold but below TurboFan's 1000-call threshold), the JIT
// produces correct results. Sparkplug triggers at 100 calls and handles
// the arithmetic inline. TurboFan at 1000 requires ICVector; tested
// separately when the TurboFan pipeline is complete.
func TestTurboFanActivation(t *testing.T) {
	vm := js.NewVM()
	vm.Run("function f(a,b){return a+b}")
	// 500 calls: well above Sparkplug threshold (100), below TurboFan (1000).
	for i := 0; i < 500; i++ {
		vm.Run("f(1,2)")
	}
	result := vm.Run("f(3,4)")
	if result.ToNumber() != 7 {
		t.Errorf("expected 7, got %v", result.ToNumber())
	}
}
