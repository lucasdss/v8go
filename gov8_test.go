package v8go_test

import (
	"testing"

	"github.com/lucasdss/v8go"
	"github.com/lucasdss/v8go/pkg/js"
)

func TestEvaluateSimple(t *testing.T) {
	result := v8go.Evaluate("1 + 2")
	if result.ToNumber() != 3 {
		t.Errorf("expected 3, got %v", result.ToNumber())
	}
}

func TestEvaluateString(t *testing.T) {
	result := v8go.Evaluate(`"hello " + "world"`)
	if result.ToString() != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.ToString())
	}
}

func TestEngineConsoleLog(t *testing.T) {
	engine := v8go.NewEngine()
	engine.Evaluate(`console.log("test")`)
	logs := engine.ConsoleOutput()
	if len(logs) != 1 || logs[0] != "test" {
		t.Errorf("expected ['test'], got %v", logs)
	}
}

func TestEvaluateComplex(t *testing.T) {
	result := v8go.Evaluate(`
		function factorial(n) {
			if (n <= 1) { return 1; }
			return n * factorial(n - 1);
		}
		factorial(5)
	`)
	if result.ToNumber() != 120 {
		t.Errorf("expected 120, got %v", result.ToNumber())
	}
}

func TestFunctionScopedVar(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		function f() {
			var x = 10;
			return x;
		}
		f()
	`)
	if result.ToNumber() != 10 {
		t.Errorf("function-scoped var: expected 10, got %v", result.ToNumber())
	}
}

func TestVersion(t *testing.T) {
	v := v8go.Version()
	if v == "" {
		t.Error("version should not be empty")
	}
}
