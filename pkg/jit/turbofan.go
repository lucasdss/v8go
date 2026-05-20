//go:build !amd64

// Package jit — turbofan: TurboFan optimizing JIT compiler (Tier 2).
//
// Compiles bytecode via SSA IR into optimized ARM64 machine code.
// The pipeline: BuildSSA → specializeTypes → inlineMonomorphicCalls →
// allocateRegisters → lowerSSAToARM64.
//
// TurboFan reuses Sparkplug's prologue/epilogue patterns and helper
// calling convention, but replaces the interpreter-like dispatch with
// register-allocated native arithmetic.
package jit

import (
	"fmt"

	"github.com/lucasdss/v8go/pkg/js"
)

// CompileTurboFan compiles a BytecodeFunction into optimized ARM64 machine code
// using the TurboFan SSA pipeline. Returns a CodeBuf containing executable
// code, or an error if compilation fails at any stage.
//
// The generated code follows the Go ABI calling convention:
//
//	func turbofanEntry(frame *js.VMFrame)
//
// R0 = frame pointer on entry, matching Sparkplug's convention.
func CompileTurboFan(bf *js.BytecodeFunction) (*CodeBuf, error) {
	if bf == nil {
		return nil, fmt.Errorf("turbofan: nil function")
	}

	// Phase 1: Build SSA graph from bytecode.
	g := BuildSSA(bf)
	if g == nil {
		return nil, fmt.Errorf("turbofan: SSA build failed for %s", bf.Name)
	}

	// Phase 2: Type specialization using IC feedback.
	specializeTypes(g)

	// Phase 3: JSValue escape analysis and elimination.
	escapeInfo := analyzeEscape(g)
	eliminateNonEscaping(g, escapeInfo)

	// Phase 4: Single-basic-block redundant load elimination.
	eliminateRedundantLoads(g)

	// Phase 5: Inline monomorphic call sites.
	inlineMonomorphicCalls(g)

	// Phase 6: Inline polymorphic call sites (guard chains).
	inlinePolymorphicCalls(g)

	// Phase 7: Global Value Numbering — eliminate redundant computations.
	runGVN(g)

	// Phase 8: Algebraic simplification — peephole identity reductions.
	simplifyAlgebraic(g)

	// Phase 9: LICM — hoist loop-invariant computations out of loops.
	licmCount := runLICM(g)
	_ = licmCount

	// Phase 11: Linear-scan register allocation.
	ra := allocateRegisters(g)

	// Phase 12: Lower SSA to ARM64 machine code.
	buf, err := lowerSSAToARM64(g, ra)
	if err != nil {
		return nil, fmt.Errorf("turbofan: lowering failed: %w", err)
	}

	return buf, nil
}
