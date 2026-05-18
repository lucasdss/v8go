package js

import (
	"strings"
	"testing"
)

// TestAllocObjBumpAllocator verifies that the first allocObjBufSize (64)
// allocations come from the pre-allocated bump buffer (no pool fallback).
func TestAllocObjBumpAllocator(t *testing.T) {
	a := NewAllocator()

	// Allocate up to the bump buffer capacity.
	for i := 0; i < allocObjBufSize; i++ {
		obj := a.AllocObj()
		if obj == nil {
			t.Fatalf("AllocObj %d returned nil", i)
		}
		// Bump-allocated objects are zeroed; Shape is nil (only pool sets EmptyShape).
		if obj.Properties != nil {
			t.Fatalf("AllocObj %d: Properties is non-nil, expected nil (zeroed)", i)
		}
	}
	// Bump counter should be at buffer capacity.
	if a.objUsed != allocObjBufSize {
		t.Errorf("objUsed = %d, want %d", a.objUsed, allocObjBufSize)
	}
}

// TestAllocObjPoolFallback verifies that the 65th allocation falls back
// to the sync.Pool when the bump buffer is exhausted.
func TestAllocObjPoolFallback(t *testing.T) {
	a := NewAllocator()

	// Exhaust the bump buffer.
	for i := 0; i < allocObjBufSize; i++ {
		_ = a.AllocObj()
	}
	if a.objUsed != allocObjBufSize {
		t.Fatalf("expected objUsed=%d, got %d", allocObjBufSize, a.objUsed)
	}

	// The next allocation must come from the pool.
	obj := a.AllocObj()
	if obj == nil {
		t.Fatal("AllocObj from pool returned nil")
	}
	// objUsed should NOT have incremented (pool allocation doesn't touch bump).
	if a.objUsed != allocObjBufSize {
		t.Errorf("objUsed after pool alloc = %d, want %d (unchanged)", a.objUsed, allocObjBufSize)
	}
}

// TestFreeObj verifies that FreeObj properly resets mixin pointers
// and returns objects to the pool when they came from the pool.
func TestFreeObj(t *testing.T) {
	a := NewAllocator()

	// Allocate from bump and free it.
	obj1 := a.AllocObj()
	obj1.interceptor = &InterceptorMixin{}
	a.FreeObj(obj1)
	if obj1.interceptor != nil {
		t.Error("FreeObj should nil interceptor")
	}
	if a.objUsed != 0 {
		t.Errorf("after FreeObj on bump obj, objUsed = %d, want 0", a.objUsed)
	}

	// Exhaust bump, then allocate and free a pool object.
	for i := 0; i < allocObjBufSize; i++ {
		_ = a.AllocObj()
	}
	obj2 := a.AllocObj() // from pool
	obj2.proxy = &ProxyMixin{}
	obj2.typedArray = &TypedArrayMixin{}
	obj2.generator = &GeneratorMixin{}
	obj2.flags = 42

	a.FreeObj(obj2)
	if obj2.proxy != nil {
		t.Error("FreeObj should nil proxy")
	}
	if obj2.typedArray != nil {
		t.Error("FreeObj should nil typedArray")
	}
	if obj2.generator != nil {
		t.Error("FreeObj should nil generator")
	}
	if obj2.flags != 0 {
		t.Error("FreeObj should zero flags")
	}
	// objUsed should still be at capacity (pool object didn't come from bump).
	if a.objUsed != allocObjBufSize {
		t.Errorf("objUsed after FreeObj on pool obj = %d, want %d", a.objUsed, allocObjBufSize)
	}
}

// TestFreeObjLIFO verifies LIFO bump rewind works: freeing the most
// recently allocated object rewinds the bump pointer.
func TestFreeObjLIFO(t *testing.T) {
	a := NewAllocator()

	obj1 := a.AllocObj()
	obj2 := a.AllocObj()
	obj3 := a.AllocObj()

	if a.objUsed != 3 {
		t.Fatalf("objUsed after 3 allocs = %d, want 3", a.objUsed)
	}

	// Free last → rewinds to 2.
	a.FreeObj(obj3)
	if a.objUsed != 2 {
		t.Errorf("after FreeObj(obj3): objUsed = %d, want 2", a.objUsed)
	}

	// obj2 is now LIFO top (obj3 gone), rewinds to 1.
	a.FreeObj(obj2)
	if a.objUsed != 1 {
		t.Errorf("after FreeObj(obj2): objUsed = %d, want 1 (now LIFO top)", a.objUsed)
	}

	// obj1 is now LIFO top, rewinds to 0.
	a.FreeObj(obj1)
	if a.objUsed != 0 {
		t.Errorf("after FreeObj(obj1): objUsed = %d, want 0 (now LIFO top)", a.objUsed)
	}
}

// TestAllocFrameBump verifies AllocFrame returns frames from the bump buffer.
func TestAllocFrameBump(t *testing.T) {
	a := NewAllocator()

	f1 := a.AllocFrame()
	if f1 == nil {
		t.Fatal("AllocFrame returned nil")
	}
	if a.frameUsed != 1 {
		t.Errorf("frameUsed = %d, want 1", a.frameUsed)
	}

	// Ensure frame is zeroed.
	if f1.PC != 0 {
		t.Errorf("new frame PC = %d, want 0", f1.PC)
	}
}

// TestAllocFrameOverflow verifies that after exhausting the bump buffer
// AllocFrame falls back to heap allocation.
func TestAllocFrameOverflow(t *testing.T) {
	a := NewAllocator()

	for i := 0; i < allocFrameBufSize; i++ {
		_ = a.AllocFrame()
	}
	if a.frameUsed != allocFrameBufSize {
		t.Fatalf("frameUsed = %d, want %d", a.frameUsed, allocFrameBufSize)
	}

	f := a.AllocFrame() // heap-allocated
	if f == nil {
		t.Fatal("AllocFrame overflow returned nil")
	}
	// frameUsed should not have changed.
	if a.frameUsed != allocFrameBufSize {
		t.Errorf("frameUsed after overflow = %d, want %d", a.frameUsed, allocFrameBufSize)
	}
}

// TestFreeFrameErrorPath verifies that FreeFrame handles edge cases
// gracefully: freeing frames that are not in the buffer, frames
// with GenState set, and frames when the buffer is empty.
func TestFreeFrameErrorPath(t *testing.T) {
	// Case 1: Frame with GenState set → early return (no processing).
	a := NewAllocator()
	f := a.AllocFrame()
	f.GenState = &GeneratorState{}
	a.FreeFrame(f) // should not panic, should not rewind
	// GenState prevents any processing — frameUsed stays at 1.

	// Case 2: Heap-allocated frame (not in buffer range).
	a2 := NewAllocator()
	heapF := &VMFrame{} // heap-allocated, not in a2.frameBuf
	a2.FreeFrame(heapF) // should not panic
	if a2.frameUsed != 0 {
		t.Errorf("FreeFrame on heap frame: frameUsed = %d, want 0", a2.frameUsed)
	}

	// Case 3: Frame from a different allocator's buffer.
	a3 := NewAllocator()
	f3 := a3.AllocFrame()
	if a3.frameUsed != 1 {
		t.Fatalf("setup: frameUsed = %d, want 1", a3.frameUsed)
	}
	a.FreeFrame(f3) // f3 belongs to a3, freeing in 'a' should no-op
	if a.frameUsed != 1 { // a's frameUsed should not change
		t.Errorf("FreeFrame on foreign frame: a.frameUsed = %d, want 1", a.frameUsed)
	}
}

// TestFreeRegsErrorPath verifies FreeRegs handles edge cases gracefully.
func TestFreeRegsErrorPath(t *testing.T) {
	a := NewAllocator()

	// Case 1: Empty slice → no-op.
	a.FreeRegs(nil)
	a.FreeRegs([]JSValue{})
	if a.regUsed != 0 {
		t.Errorf("FreeRegs on empty: regUsed = %d, want 0", a.regUsed)
	}

	// Case 2: Heap-allocated slice (not in buffer).
	heapRegs := make([]JSValue, 10)
	a.FreeRegs(heapRegs)
	if a.regUsed != 0 {
		t.Errorf("FreeRegs on heap slice: regUsed = %d, want 0", a.regUsed)
	}

	// Case 3: Slice from different allocator.
	a2 := NewAllocator()
	regs2 := a2.AllocRegs(5)
	if a2.regUsed != 5 {
		t.Fatalf("setup: a2.regUsed = %d, want 5", a2.regUsed)
	}
	a.FreeRegs(regs2) // freeing in 'a' should no-op
	if a.regUsed != 0 {
		t.Errorf("FreeRegs on foreign regs: regUsed = %d, want 0", a.regUsed)
	}
}

// TestAllocRegsOverflow verifies AllocRegs falls back to make()
// when requesting more than the bump buffer can provide.
func TestAllocRegsOverflow(t *testing.T) {
	a := NewAllocator()

	// Allocate 256 regs to fill the first 256-slot chunk.
	regs1 := a.AllocRegs(256)
	if len(regs1) != 256 {
		t.Fatalf("AllocRegs(256) len = %d, want 256", len(regs1))
	}
	if a.regUsed != 256 {
		t.Fatalf("regUsed after AllocRegs(256) = %d, want 256", a.regUsed)
	}

	// Allocate more than remaining buffer capacity.
	// total buf size is allocRegBufSize = 256*8 = 2048
	// Remaining: 2048 - 256 = 1792. Request 2000 triggers heap fallback.
	regs2 := a.AllocRegs(2000)
	if len(regs2) != 2000 {
		t.Fatalf("AllocRegs(2000) len = %d, want 2000", len(regs2))
	}
	// regUsed should not have changed (heap allocation).
	if a.regUsed != 256 {
		t.Errorf("regUsed after overflow = %d, want 256", a.regUsed)
	}

	// Allocate > 256 always goes to heap.
	regs3 := a.AllocRegs(257)
	if len(regs3) != 257 {
		t.Fatalf("AllocRegs(257) len = %d, want 257", len(regs3))
	}
	if a.regUsed != 256 {
		t.Errorf("regUsed after 257-slot alloc = %d, want 256", a.regUsed)
	}
}

// TestAllocRegsLIFO verifies LIFO rewind for register allocation.
func TestAllocRegsLIFO(t *testing.T) {
	a := NewAllocator()

	_ = a.AllocRegs(10)
	r2 := a.AllocRegs(20)
	r3 := a.AllocRegs(30)

	if a.regUsed != 60 {
		t.Fatalf("regUsed after allocs = %d, want 60", a.regUsed)
	}

	// Free last → rewinds to 30.
	a.FreeRegs(r3)
	if a.regUsed != 30 {
		t.Errorf("after FreeRegs(r3): regUsed = %d, want 30", a.regUsed)
	}

	// Free middle → now LIFO top (r3 gone), rewinds to 10.
	a.FreeRegs(r2)
	if a.regUsed != 10 {
		t.Errorf("after FreeRegs(r2): regUsed = %d, want 10 (now LIFO top)", a.regUsed)
	}

	// Re-allocate from freed position.
	r4 := a.AllocRegs(10)
	if a.regUsed != 20 {
		t.Errorf("after re-alloc: regUsed = %d, want 20", a.regUsed)
	}
	_ = r4
}

// TestEqualFold verifies case-insensitive string comparison.
func TestEqualFold(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"ABC", "abc", true},
		{"AbC", "aBc", true},
		{"abc", "ab", false},
		{"", "", true},
		{"hello", "HELLO", true},
		{"Hello World", "hello world", true},
		{"", "x", false},
		{"abc", "abcd", false},
	}

	for _, tt := range tests {
		got := equalFold(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("equalFold(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// TestAllocatorReset verifies Reset rewinds all bump allocators to zero.
func TestAllocatorReset(t *testing.T) {
	a := NewAllocator()

	_ = a.AllocFrame()
	_ = a.AllocRegs(10)
	_ = a.AllocObj()

	a.Reset()
	if a.frameUsed != 0 {
		t.Errorf("frameUsed after Reset = %d, want 0", a.frameUsed)
	}
	if a.regUsed != 0 {
		t.Errorf("regUsed after Reset = %d, want 0", a.regUsed)
	}
	if a.objUsed != 0 {
		t.Errorf("objUsed after Reset = %d, want 0", a.objUsed)
	}
}

// TestEqualFoldUsesStrings verifies equalFold delegates to strings.EqualFold.
func TestEqualFoldUsesStrings(t *testing.T) {
	// Proves that equalFold call path works and returns same as strings.EqualFold.
	if got := equalFold("GoLang", "golang"); got != strings.EqualFold("GoLang", "golang") {
		t.Errorf("equalFold diverged from strings.EqualFold")
	}
}
