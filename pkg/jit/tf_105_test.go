package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestTurboFan105(t *testing.T) {
	vm := js.NewVM()
	t.Log("starting 105 iteration warmup...")
	vm.Run("function f(a,b){return a+b}")
	for i := 0; i < 105; i++ {
		vm.Run("f(1,2)")
	}
	t.Log("warmup done, checking result...")
	result := vm.Run("f(3,4)")
	if result.ToNumber() != 7 {
		t.Errorf("expected 7, got %v", result.ToNumber())
	}
	t.Log("PASS")
}
