package jit

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestShadowStackBasic(t *testing.T) {
	ss := NewShadowStack(16)
	if ss.Len() != 0 {
		t.Errorf("new shadow stack len=%d, want 0", ss.Len())
	}
	if ss.Cap() != 16 {
		t.Errorf("cap=%d, want 16", ss.Cap())
	}

	// Push some pointers.
	var x, y, z int
	ss.Push(unsafe.Pointer(&x))
	ss.Push(unsafe.Pointer(&y))
	ss.Push(unsafe.Pointer(&z))

	if ss.Len() != 3 {
		t.Errorf("after 3 pushes, len=%d, want 3", ss.Len())
	}

	// Pop one.
	ss.Pop()
	if ss.Len() != 2 {
		t.Errorf("after pop, len=%d, want 2", ss.Len())
	}
	// Verify the popped slot was zeroed.
	if ss.refs[2] != nil {
		t.Error("popped slot not nil'd")
	}

	// Clear.
	ss.Clear()
	if ss.Len() != 0 {
		t.Errorf("after clear, len=%d, want 0", ss.Len())
	}
	for i := range ss.refs {
		if ss.refs[i] != nil {
			t.Errorf("slot[%d] not nil after clear", i)
		}
	}
}

func TestShadowStackOverflow(t *testing.T) {
	ss := NewShadowStack(2)
	var a, b, c int
	ss.Push(unsafe.Pointer(&a))
	ss.Push(unsafe.Pointer(&b))
	ss.Push(unsafe.Pointer(&c)) // should be silently dropped

	if ss.Len() != 2 {
		t.Errorf("overflow len=%d, want 2", ss.Len())
	}
}

func TestShadowStackGC(t *testing.T) {
	// This test verifies that object pointers held in the shadow stack
	// prevent GC from collecting those objects prematurely.

	vm := js.NewVM()

	// Run a function many times to stress allocation and GC.
	vm.Run("function f(){var a={x:1}; return a.x};f()")

	// Force multiple GC cycles while running JIT-style code that uses
	// the shadow stack.
	ss := NewShadowStack(64)
	for i := 0; i < 200; i++ {
		// Allocate objects and keep them in shadow stack during "JIT" execution.
		obj := js.NewJSObject()
		obj.Set("x", js.NewNumber(float64(i)))

		// Simulate JIT holding the object pointer in a register.
		ss.Push(unsafe.Pointer(obj))

		// Trigger GC — the shadow stack should keep the object alive.
		runtime.GC()

		// Verify the object is still alive.
		if obj.Get("x").ToNumber() != float64(i) {
			t.Errorf("iteration %d: object collected or corrupted", i)
		}

		// Clear for next iteration.
		ss.Clear()
	}

	// Now test with the VM: many object allocations in a loop.
	for i := 0; i < 200; i++ {
		result := vm.Run("function f(){var a={x:1}; return a.x};f()")
		if result.ToNumber() != 1 {
			t.Errorf("iteration %d: got %v, want 1 — object may have been collected prematurely", i, result.ToNumber())
		}
	}
}

func TestSparkplugPrologueEpilogue(t *testing.T) {
	ss := NewShadowStack(MaxObjectRegs)

	// Simulate prologue: setup shadow stack for this frame.
	frameSS := PrologueSetup(unsafe.Pointer(ss))
	if frameSS != ss {
		t.Fatal("PrologueSetup did not return the same shadow stack")
	}

	// Simulate JIT execution: spill an object pointer.
	obj := js.NewJSObject()
	obj.Set("key", js.NewNumber(42))
	SpillObjectPtr(frameSS, unsafe.Pointer(obj))

	if ss.Len() != 1 {
		t.Errorf("after spill, len=%d, want 1", ss.Len())
	}
	if ss.refs[0] != unsafe.Pointer(obj) {
		t.Error("spilled pointer does not match")
	}

	// GC should not collect obj while it's in shadow stack.
	runtime.GC()
	if obj.Get("key").ToNumber() != 42 {
		t.Error("object collected despite being in shadow stack")
	}

	// Epilogue: clear shadow stack.
	EpilogueTeardown(frameSS)
	if ss.Len() != 0 {
		t.Errorf("after epilogue, len=%d, want 0", ss.Len())
	}
}

func TestSpillJSValue(t *testing.T) {
	ss := NewShadowStack(16)

	// Object value should be spilled.
	obj := js.NewJSObject()
	obj.Set("a", js.NewNumber(1))
	val := js.NewObject(obj)
	SpillJSValue(ss, val)
	if ss.Len() != 1 {
		t.Errorf("object value not spilled, len=%d", ss.Len())
	}

	// Non-object value should not be spilled.
	numVal := js.NewNumber(42)
	SpillJSValue(ss, numVal)
	if ss.Len() != 1 {
		t.Errorf("non-object value spilled, len=%d", ss.Len())
	}

	ss.Clear()
}

func TestSpillAllJSValues(t *testing.T) {
	ss := NewShadowStack(16)

	obj1 := js.NewJSObject()
	obj2 := js.NewJSObject()
	vals := []js.JSValue{
		js.NewNumber(1),
		js.NewObject(obj1),
		js.NewString("hello"),
		js.NewObject(obj2),
		js.Undefined,
	}

	SpillAllJSValues(ss, vals)
	if ss.Len() != 2 {
		t.Errorf("spilled %d values, want 2 (only objects)", ss.Len())
	}

	ss.Clear()
}

func TestShadowStackNilSafety(t *testing.T) {
	// All operations should be safe on nil shadow stack.
	var ss *ShadowStack
	if PrologueSetup(unsafe.Pointer(ss)) != nil {
		t.Error("PrologueSetup with nil should return nil")
	}
	EpilogueTeardown(ss)    // should not panic
	SpillObjectPtr(ss, nil) // should not panic

	var x int
	SpillObjectPtr(ss, unsafe.Pointer(&x))              // should not panic
	SpillJSValue(ss, js.NewNumber(1))                   // should not panic
	SpillAllJSValues(ss, []js.JSValue{js.NewNumber(1)}) // should not panic
}
