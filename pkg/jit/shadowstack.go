// Package jit provides JIT compilation infrastructure for GoV8, including
// the shadow stack for GC-safe native code execution.
//
// When Sparkplug (the baseline JIT) compiles JavaScript bytecode to native
// ARM64 / x86-64 machine code, JavaScript object pointers can end up living
// exclusively in CPU registers — invisible to Go's garbage collector. If a
// GC cycle runs while those pointers are only in registers, the collector
// may prematurely free the objects, causing use-after-free bugs.
//
// The shadow stack solves this by maintaining a side table of unsafe.Pointer
// values that the JIT prologue/explicit spills populate. Because the shadow
// stack is a Go heap-allocated slice, the GC can scan it and mark every
// object pointer held by native code.
package jit

import "unsafe"

// ShadowStack holds JS object pointers that are currently live in native
// (JIT-compiled) CPU registers. The Go GC scans the refs slice and marks
// every pointer it contains, preventing premature collection.
//
// Typical usage in JIT prologue:
//
//	// Load shadow stack pointer from VMFrame
//	ss := frame.ShadowStack
//	// During execution, when regX holds TagObject:
//	ss.Push(regX.ObjVal)
//
// Epilogue:
//
//	ss.Clear()
type ShadowStack struct {
	refs []unsafe.Pointer
	top  int
}

// NewShadowStack allocates a shadow stack with the given capacity.
// Capacity should be at least the maximum number of object pointers that
// can be simultaneously live in JIT registers during a single frame.
func NewShadowStack(capacity int) *ShadowStack {
	return &ShadowStack{
		refs: make([]unsafe.Pointer, capacity),
	}
}

// Push adds a pointer to the shadow stack. The GC will see this pointer.
// If the shadow stack is full, the pointer is silently dropped — capacity
// should be sized to avoid this. The caller must ensure p is a valid
// heap-allocated Go pointer (typically *JSObject).
func (s *ShadowStack) Push(p unsafe.Pointer) {
	if s.top < len(s.refs) {
		s.refs[s.top] = p
		s.top++
	}
}

// Pop removes the topmost pointer from the shadow stack. Used when a JIT
// register is overwritten or the value is no longer live.
func (s *ShadowStack) Pop() {
	if s.top > 0 {
		s.top--
		s.refs[s.top] = nil
	}
}

// Clear zeros all entries and resets the top pointer. Called in the JIT
// epilogue after all native registers are saved/returned, ensuring no
// stale pointers remain that could keep objects alive longer than needed.
func (s *ShadowStack) Clear() {
	for i := 0; i < s.top; i++ {
		s.refs[i] = nil
	}
	s.top = 0
}

// Len returns the current number of live entries.
func (s *ShadowStack) Len() int { return s.top }

// Cap returns the maximum capacity of the shadow stack.
func (s *ShadowStack) Cap() int { return len(s.refs) }
