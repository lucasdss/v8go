package jit

import (
	"testing"
	"time"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestTurboFanSync(t *testing.T) {
	vm := js.NewVM()
	t.Log("running 110 iterations to trigger Sparkplug...")
	vm.Run("function f(a,b){return a+b}")
	for i := 0; i < 110; i++ {
		vm.Run("f(1,2)")
	}
	t.Log("waiting for async compilation to complete...")
	time.Sleep(500 * time.Millisecond)
	t.Log("now running with Sparkplug (should be compiled)...")
	result := vm.Run("f(3,4)")
	if result.ToNumber() != 7 {
		t.Errorf("expected 7, got %v", result.ToNumber())
	}
	t.Log("PASS")
}
