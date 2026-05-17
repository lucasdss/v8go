package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestTurboFan400(t *testing.T) {
	vm := js.NewVM()
	vm.Run("function f(a,b){return a+b}")
	for i := 0; i < 400; i++ {
		vm.Run("f(1,2)")
	}
	result := vm.Run("f(3,4)")
	if result.ToNumber() != 7 {
		t.Errorf("expected 7, got %v", result.ToNumber())
	}
}
