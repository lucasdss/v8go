//go:build amd64

// preempt_amd64.go — Goroutine preemption polling for AMD64 Sparkplug JIT.
//
// On AMD64, the Go runtime stores the goroutine's stack guard in
// g.stackguard0 at offset 16 in the g struct (R14 points to g).
// The sentinel value stackPreempt (0xfffffffffffffade) indicates
// that the goroutine should be preempted.
package jit

import "unsafe"

// gStackGuardOffset is the offset of stackguard0 in the g struct.
// On amd64, it's at offset 16.
const gStackGuardOffset = 16

// stackPreempt is the sentinel value for stack preemption.
const stackPreempt = uint64(0xfffffffffffffade)

// EmitPreemptCheckAMD64 emits a preemption check sequence for AMD64 Sparkplug.
// Checks g.stackguard0 == stackPreempt and branches to deoptStub if so.
func EmitPreemptCheckAMD64(as *Assembler, deoptStub *Label) {
	// CMP [R14 + gStackGuardOffset], stackPreempt
	// JE deoptStub

	// Load stackguard0 from g struct.
	as.AMD64_MOV_LOAD(REG_RCX, REG_R14, int8(gStackGuardOffset))

	// Compare with stackPreempt sentinel.
	as.AMD64_MOV_RI(REG_RDX, stackPreempt)
	as.AMD64_CMP_RR(REG_RCX, REG_RDX)
	as.AMD64_JE(deoptStub)
}

// AMD64ShadowStack holds object pointers for GC tracing during JIT execution.
// On AMD64, callee-saved registers that may hold object pointers include
// RBX, RBP, R12-R15 (but R14 is $g$, RBP is frame ptr, so mainly RBX, R12, R13, R15).
type AMD64ShadowStack struct {
	entries []unsafe.Pointer
	pos     int
}

func (s *AMD64ShadowStack) push(p unsafe.Pointer) {
	if s.pos < len(s.entries) {
		s.entries[s.pos] = p
		s.pos++
	}
}

func (s *AMD64ShadowStack) pop() {
	if s.pos > 0 {
		s.pos--
		s.entries[s.pos] = nil
	}
}

func (s *AMD64ShadowStack) clear() {
	for i := range s.entries {
		s.entries[i] = nil
	}
	s.pos = 0
}
