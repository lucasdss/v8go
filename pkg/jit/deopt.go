// deopt.go — Deoptimization input data for Sparkplug JIT bailout.
//
// When Sparkplug-compiled code encounters a deoptimization point
// (e.g., type feedback mismatch, stack overflow), it uses this data
// to reconstruct the interpreter state and resume execution in the
// bytecode interpreter.

package jit

// DeoptPoint maps a native code offset to the corresponding bytecode PC.
type DeoptPoint struct {
	NativeOffset int
	BytecodePC   int
}

// DeoptimizationInputData holds all information needed to reconstruct
// an interpreter frame at a deoptimization point.
//
// Constants uses interface{} to avoid an import cycle with pkg/js.
// The values are js.JSValue instances; callers must type-assert on use.
type DeoptimizationInputData struct {
	Points    []DeoptPoint
	RegMap    [30]int       // native register → virtual register mapping
	Constants []interface{} // constant pool snapshot (js.JSValue instances)
}

// FindDeoptPoint looks up the bytecode PC for a given native code offset.
// Returns -1, false if no matching deopt point is found.
func (d *DeoptimizationInputData) FindDeoptPoint(nativeOffset int) (int, bool) {
	for _, p := range d.Points {
		if p.NativeOffset == nativeOffset {
			return p.BytecodePC, true
		}
	}
	return -1, false
}

// FrameDescription captures the native execution state at a deoptimization
// point. The emitDeoptStub saves all live registers into this struct and
// passes it to GoDeoptimize, which reconstructs the interpreter frame.
//
// Layout (offsets must match goDeoptimize in pkg/js/vm.go):
//
//	offset 0:   Regs[0..29] — 30 × uint64 = 240 bytes
//	offset 240: PC          — int (8 bytes)
//	offset 248: FP          — uintptr (8 bytes, REG_VM0 / frame pointer)
//	offset 256: SP          — uintptr (8 bytes)
//	offset 264: LR          — uint64 (8 bytes, REG_VM1 / link register)
//
// Total: 272 bytes (34 × 8, 16-byte aligned).
type FrameDescription struct {
	Regs [30]uint64
	PC   int
	FP   uintptr
	SP   uintptr
	LR   uint64
}
