//go:build !amd64

package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// =============================================================================
// GVN Common Subexpression Elimination Tests
// =============================================================================

// makeCSEFunc creates bytecode that computes a+b twice:
//
//	function f(a, b) { var x = a + b; return a + b + x; }
//
// The first a+b (used to compute x) and the second a+b should be
// recognized as the same computation by GVN.
func makeCSEFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "cseFunc",
		NumRegisters: 5,
		NumParams:    2,
		Instructions: []js.Instruction{
			// x = a + b
			{Op: js.OpLdar, OperandA: 0},  // load a → acc
			{Op: js.OpStar, OperandA: 2},  // store a → r2
			{Op: js.OpLdar, OperandA: 1},  // load b → acc
			{Op: js.OpAdd, OperandA: 2},   // acc + r2 = b + a → acc
			{Op: js.OpStar, OperandA: 3},  // store x → r3

			// a + b again (redundant)
			{Op: js.OpLdar, OperandA: 0},  // load a → acc
			{Op: js.OpStar, OperandA: 2},  // store a → r2 (overwrite)
			{Op: js.OpLdar, OperandA: 1},  // load b → acc
			{Op: js.OpAdd, OperandA: 2},   // acc + r2 = b + a → acc
			{Op: js.OpStar, OperandA: 4},  // store → r4

			// return x + (a+b)
			{Op: js.OpLdar, OperandA: 4},  // load r4 → acc (second a+b)
			{Op: js.OpAdd, OperandA: 3},   // acc + r3
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

func TestGVN_CommonSubexpression(t *testing.T) {
	bf := makeCSEFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	// Count SSAAdd nodes before GVN.
	addBefore := countOp(g, SSAAdd)

	eliminated := runGVN(g)

	addAfter := countOp(g, SSAAdd)
	t.Logf("GVN eliminated: %d, SSAAdd before: %d, after: %d", eliminated, addBefore, addAfter)

	if eliminated == 0 {
		t.Error("GVN should have eliminated at least one redundant node")
	}
	if addAfter >= addBefore {
		t.Errorf("SSAAdd count should decrease after GVN: before=%d, after=%d", addBefore, addAfter)
	}
}

// =============================================================================
// GVN Constant Folding Tests
// =============================================================================

// makeConstFoldFunc creates bytecode that computes 2+3:
//
//	function f() { var x = 2; return x + 3; }
//
// GVN should fold the addition of two constants.
func makeConstFoldFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "constFold",
		NumRegisters: 3,
		NumParams:    0,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 2},  // load 2 → acc
			{Op: js.OpStar, OperandA: 1},    // store → r1
			{Op: js.OpLdaSmi, OperandA: 3},  // load 3 → acc
			{Op: js.OpAdd, OperandA: 1},     // 3 + 2 → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

func TestGVN_ConstantFolding(t *testing.T) {
	bf := makeConstFoldFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	// Check the graph before GVN: should have at least one SSAAdd.
	addBefore := countOp(g, SSAAdd)
	t.Logf("SSAAdd before GVN: %d", addBefore)

	eliminated := runGVN(g)

	addAfter := countOp(g, SSAAdd)
	t.Logf("GVN eliminated: %d, SSAAdd after: %d", eliminated, addAfter)

	if eliminated == 0 {
		t.Error("GVN should have folded constant addition")
	}
	if addAfter >= addBefore {
		t.Errorf("SSAAdd count should decrease after constant folding: before=%d, after=%d", addBefore, addAfter)
	}

	// Verify a constant 5 exists in the graph.
	found := false
	for _, bb := range g.Blocks {
		for _, n := range bb.Nodes {
			if n.Op == SSAConst && n.Value.IsNumber() && n.Value.NumVal == 5 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected folded constant 5 in graph after GVN")
	}
}

// =============================================================================
// Algebraic Simplification Tests
// =============================================================================

// makeAddZeroFunc creates bytecode: x + 0
//
//	function f(x) { return x + 0; }
func makeAddZeroFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "addZero",
		NumRegisters: 3,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},    // load x → acc
			{Op: js.OpStar, OperandA: 1},    // store x → r1
			{Op: js.OpLdaZero},              // load 0 → acc
			{Op: js.OpAdd, OperandA: 1},     // 0 + x → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// makeMulOneFunc creates bytecode: x * 1
//
//	function f(x) { return x * 1; }
func makeMulOneFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "mulOne",
		NumRegisters: 3,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},    // load x → acc
			{Op: js.OpStar, OperandA: 1},    // store x → r1
			{Op: js.OpLdaOne},               // load 1 → acc
			{Op: js.OpMul, OperandA: 1},     // 1 * x → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// makeSubZeroFunc creates bytecode: x - 0
//
//	function f(x) { return x - 0; }
func makeSubZeroFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "subZero",
		NumRegisters: 3,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},    // load x → acc
			{Op: js.OpStar, OperandA: 1},    // store x → r1
			{Op: js.OpLdaZero},              // load 0 → acc
			{Op: js.OpStar, OperandA: 2},    // store 0 → r2
			{Op: js.OpLdar, OperandA: 1},    // load x → acc
			{Op: js.OpSub, OperandA: 2},     // x - 0 → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// makeMulZeroFunc creates bytecode: x * 0
//
//	function f(x) { return x * 0; }
func makeMulZeroFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "mulZero",
		NumRegisters: 3,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},    // load x → acc
			{Op: js.OpStar, OperandA: 1},    // store x → r1
			{Op: js.OpLdaZero},              // load 0 → acc
			{Op: js.OpMul, OperandA: 1},     // 0 * x → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

func TestGVN_AlgebraicSimplification_AddZero(t *testing.T) {
	bf := makeAddZeroFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	addBefore := countOp(g, SSAAdd)
	eliminated := simplifyAlgebraic(g)
	addAfter := countOp(g, SSAAdd)

	t.Logf("simplified: %d, SSAAdd before: %d, after: %d", eliminated, addBefore, addAfter)

	if eliminated == 0 {
		t.Error("expected x+0 to be simplified")
	}
	if addAfter >= addBefore {
		t.Errorf("SSAAdd count should decrease: before=%d, after=%d", addBefore, addAfter)
	}
}

func TestGVN_AlgebraicSimplification_MulOne(t *testing.T) {
	bf := makeMulOneFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	mulBefore := countOp(g, SSAMul)
	eliminated := simplifyAlgebraic(g)
	mulAfter := countOp(g, SSAMul)

	t.Logf("simplified: %d, SSAMul before: %d, after: %d", eliminated, mulBefore, mulAfter)

	if eliminated == 0 {
		t.Error("expected x*1 to be simplified")
	}
	if mulAfter >= mulBefore {
		t.Errorf("SSAMul count should decrease: before=%d, after=%d", mulBefore, mulAfter)
	}
}

func TestGVN_AlgebraicSimplification_SubZero(t *testing.T) {
	bf := makeSubZeroFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	subBefore := countOp(g, SSASub)
	eliminated := simplifyAlgebraic(g)
	subAfter := countOp(g, SSASub)

	t.Logf("simplified: %d, SSASub before: %d, after: %d", eliminated, subBefore, subAfter)

	if eliminated == 0 {
		t.Error("expected x-0 to be simplified")
	}
	if subAfter >= subBefore {
		t.Errorf("SSASub count should decrease: before=%d, after=%d", subBefore, subAfter)
	}
}

func TestGVN_AlgebraicSimplification_MulZero(t *testing.T) {
	bf := makeMulZeroFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	mulBefore := countOp(g, SSAMul)
	eliminated := simplifyAlgebraic(g)
	mulAfter := countOp(g, SSAMul)

	t.Logf("simplified: %d, SSAMul before: %d, after: %d", eliminated, mulBefore, mulAfter)

	if eliminated == 0 {
		t.Error("expected x*0 to be simplified")
	}
	if mulAfter >= mulBefore {
		t.Errorf("SSAMul count should decrease: before=%d, after=%d", mulBefore, mulAfter)
	}
}

// =============================================================================
// GVN Side-Effecting Skip Tests
// =============================================================================

// makeSideEffectFunc creates bytecode with a call:
//
//	function f(x) { var t = x + 1; g(t); return t + 1; }
//
// The store/call should not be eliminated by GVN.
func makeSideEffectFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "sideEffect",
		NumRegisters: 4,
		NumParams:    1,
		Instructions: []js.Instruction{
			// t = x + 1
			{Op: js.OpLdar, OperandA: 0},    // load x → acc
			{Op: js.OpStar, OperandA: 2},    // store → r2
			{Op: js.OpLdaOne},               // load 1 → acc
			{Op: js.OpAdd, OperandA: 2},     // 1 + x → acc
			{Op: js.OpStar, OperandA: 3},    // store t → r3

			// g(t) - call with t as arg
			{Op: js.OpLdar, OperandA: 3},    // load t → acc
			{Op: js.OpCall0, OperandA: 0},   // call g (arg in acc)

			// return t + 1
			{Op: js.OpLdar, OperandA: 3},    // load t → acc
			{Op: js.OpStar, OperandA: 2},    // store t → r2
			{Op: js.OpLdaOne},               // load 1 → acc
			{Op: js.OpAdd, OperandA: 2},     // 1 + t → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

func TestGVN_SideEffecting_Skip(t *testing.T) {
	bf := makeSideEffectFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	// Count call and store nodes before.
	callBefore := countOp(g, SSACall)
	storeBefore := countOp(g, SSAStore)

	eliminated := runGVN(g)

	callAfter := countOp(g, SSACall)
	storeAfter := countOp(g, SSAStore)

	t.Logf("GVN eliminated: %d", eliminated)
	if callAfter != callBefore {
		t.Errorf("call nodes changed: before=%d, after=%d", callBefore, callAfter)
	}
	if storeAfter > storeBefore {
		t.Errorf("store nodes changed: before=%d, after=%d", storeBefore, storeAfter)
	}
}

// =============================================================================
// White-box: helper function tests
// =============================================================================

func TestGVN_IsSideEffecting(t *testing.T) {
	tests := []struct {
		op          SSAOp
		sideEffects bool
	}{
		{SSAAdd, false},
		{SSASub, false},
		{SSAMul, false},
		{SSADiv, false},
		{SSANeg, false},
		{SSANot, false},
		{SSAConst, false},
		{SSAAnd, false},
		{SSAOr, false},
		{SSAXor, false},
		{SSAShl, false},
		{SSAShr, false},
		{SSASar, false},
		{SSAEq, false},
		{SSANe, false},
		{SSALt, false},
		{SSAGt, false},
		{SSALe, false},
		{SSAGe, false},
		{SSACmp, false},
		{SSAStore, true},
		{SSAStoreGlobal, true},
		{SSAStoreElement, true},
		{SSASetProperty, true},
		{SSACall, true},
		{SSANew, true},
		{SSANewArray, true},
		{SSAThrow, true},
		{SSADeopt, true},
		{SSABranch, true},
		{SSAPhi, true},
		{SSAReturn, true},
		{SSAInterruptCheck, true},
		{SSAStackCheck, true},
		{SSADebugBreak, true},
		{SSANop, true},
	}

	for _, tt := range tests {
		node := &SSANode{Op: tt.op}
		got := isSideEffecting(node)
		if got != tt.sideEffects {
			t.Errorf("isSideEffecting(%s) = %v, want %v", tt.op, got, tt.sideEffects)
		}
	}
}

func TestGVN_IsZeroNode(t *testing.T) {
	// SSAZero node.
	zero := &SSANode{Op: SSAZero}
	if !isZeroNode(zero) {
		t.Error("SSAZero should be zero")
	}

	// SSAConst with value 0.
	constZero := &SSANode{Op: SSAConst, Value: js.NewNumber(0)}
	if !isZeroNode(constZero) {
		t.Error("SSAConst(0) should be zero")
	}

	// SSAConst with value 1.
	one := &SSANode{Op: SSAConst, Value: js.NewNumber(1)}
	if isZeroNode(one) {
		t.Error("SSAConst(1) should not be zero")
	}

	// Random node.
	add := &SSANode{Op: SSAAdd}
	if isZeroNode(add) {
		t.Error("SSAAdd should not be zero")
	}
}

func TestGVN_IsOneNode(t *testing.T) {
	// SSAConst with value 1.
	one := &SSANode{Op: SSAConst, Value: js.NewNumber(1)}
	if !isOneNode(one) {
		t.Error("SSAConst(1) should be one")
	}

	// SSAConst with value 0.
	zero := &SSANode{Op: SSAConst, Value: js.NewNumber(0)}
	if isOneNode(zero) {
		t.Error("SSAConst(0) should not be one")
	}

	// SSAZero.
	zeroOp := &SSANode{Op: SSAZero}
	if isOneNode(zeroOp) {
		t.Error("SSAZero should not be one")
	}
}

func TestGVN_ConstNumVal(t *testing.T) {
	// SSAConst with number.
	n := &SSANode{Op: SSAConst, Value: js.NewNumber(42)}
	v, ok := constNumVal(n)
	if !ok {
		t.Error("constNumVal should succeed for SSAConst with number")
	}
	if v != 42 {
		t.Errorf("constNumVal = %f, want 42", v)
	}

	// SSAZero.
	z := &SSANode{Op: SSAZero}
	v, ok = constNumVal(z)
	if !ok {
		t.Error("constNumVal should succeed for SSAZero")
	}
	if v != 0 {
		t.Errorf("constNumVal = %f, want 0", v)
	}

	// Non-const.
	add := &SSANode{Op: SSAAdd}
	_, ok = constNumVal(add)
	if ok {
		t.Error("constNumVal should fail for SSAAdd")
	}
}

// =============================================================================
// Helpers
// =============================================================================

// countOp returns the count of nodes with the given op in the graph.
func countOp(g *SSAGraph, op SSAOp) int {
	count := 0
	for _, bb := range g.Blocks {
		for _, n := range bb.Nodes {
			if n.Op == op {
				count++
			}
		}
	}
	return count
}
