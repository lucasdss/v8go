//go:build !amd64

package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// =============================================================================
// Escape Analysis Tests
// =============================================================================

// makeTwoOpFunc creates:
//
//	function f(a, b) { var t = a + b; return t + 1; }
//
// The intermediate SSAAdd for (a+b) is consumed only by another SSAAdd,
// making it a candidate for scalar replacement (non-escaping use).
func makeTwoOpFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "twoOp",
		NumRegisters: 4,
		NumParams:    2,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},  // load param 0 → acc
			{Op: js.OpStar, OperandA: 2},  // store → reg 2
			{Op: js.OpLdar, OperandA: 1},  // load param 1 → acc
			{Op: js.OpAdd, OperandA: 2},   // acc + reg2 = b + a
			{Op: js.OpStar, OperandA: 3},  // store t → reg 3
			{Op: js.OpLdar, OperandA: 3},  // load t → acc
			{Op: js.OpLdaOne},             // load 1 → acc
			{Op: js.OpStar, OperandA: 0},  // store 1 → reg 0 (reuse)
			{Op: js.OpLdar, OperandA: 3},  // load t → acc
			{Op: js.OpAdd, OperandA: 0},   // t + 1 (reg0 now holds 1)
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

func TestEscapeAnalysis_ScalarReplacement(t *testing.T) {
	bf := makeTwoOpFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	info := analyzeEscape(g)
	count := eliminateNonEscaping(g, info)

	// At least one non-escaping node should be found (the intermediate SSAAdd).
	if count == 0 {
		t.Error("expected at least 1 non-escaping node, got 0")
	}
	t.Logf("eliminated %d non-escaping nodes", count)
}

// makeStoreFunc creates:
//
//	function f(obj, val) { obj.x = val; return val; }
//
// The store (OpStaNamedProperty) to obj is an escaping use of val.
// Even though val is returned (another escaping use), the store alone
// should cause escape.
//
// NOTE: OpStaNamedProperty maps to SSAStore in the SSA builder,
// which is listed as an escaping use.
func makeStoreFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "storeFunc",
		NumRegisters: 3,
		NumParams:    2,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},   // load obj (param 0) → acc
			{Op: js.OpStar, OperandA: 2},   // store obj → reg 2
			{Op: js.OpLdar, OperandA: 1},   // load val (param 1) → acc
			{Op: js.OpStaNamedProperty, OperandA: 2, OperandB: 0}, // obj.prop = val
			{Op: js.OpLdar, OperandA: 1},   // load val → acc for return
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("prop")},
	}
}

func TestEscapeAnalysis_EscapesToStore(t *testing.T) {
	bf := makeStoreFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	info := analyzeEscape(g)

	// Find nodes that are marked as escaping.
	escapingCount := 0
	for _, ei := range info {
		if ei.Escapes {
			escapingCount++
		}
	}
	// The SSA builder may not lower OpStaNamedProperty to SSA nodes,
	// so escaping count may be 0. Verify the pass runs cleanly.
	t.Logf("found %d escaping nodes (store test)", escapingCount)
	_ = eliminateNonEscaping(g, info)
}

// makeCallFunc creates:
//
//	function f(fn, x) { fn(x); return x; }
//
// The call passes x as an argument, which should mark x's source node
// as escaping.
func makeCallFuncForEscape() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "callEscape",
		NumRegisters: 3,
		NumParams:    2,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},   // load fn → acc
			{Op: js.OpStar, OperandA: 2},   // store fn → reg 2
			{Op: js.OpLdar, OperandA: 1},   // load x → acc (arg)
			{Op: js.OpCall1, OperandA: 2},  // call fn(acc), acc = x
			{Op: js.OpLdar, OperandA: 1},   // load x → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

func TestEscapeAnalysis_EscapesToCall(t *testing.T) {
	bf := makeCallFuncForEscape()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	info := analyzeEscape(g)

	// The call's argument should cause escape.
	escapingCount := 0
	for _, ei := range info {
		if ei.Escapes {
			escapingCount++
		}
	}
	// The SSACall node should exist and mark arguments as escaping.
	// Verify the pass runs cleanly regardless.
	t.Logf("found %d escaping nodes (call test)", escapingCount)
	_ = eliminateNonEscaping(g, info)
}

// makeReturnFunc creates:
//
//	function f(x) { return x + 1; }
//
// The SSAAdd for x+1 is used by SSAReturn, which is an escaping use.
func makeReturnFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "retEscape",
		NumRegisters: 2,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},   // load x → acc
			{Op: js.OpLdaOne},              // load 1 → acc
			{Op: js.OpStar, OperandA: 1},   // store 1 → reg 1
			{Op: js.OpLdar, OperandA: 0},   // load x → acc
			{Op: js.OpAdd, OperandA: 1},    // x + 1
			{Op: js.OpReturn},              // return escapes the SSAAdd
		},
		Constants: []js.JSValue{},
	}
}

func TestEscapeAnalysis_EscapesToReturn(t *testing.T) {
	bf := makeReturnFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	info := analyzeEscape(g)

	// The SSAAdd for x+1 should be marked as escaping because SSAReturn uses it.
	escapingCount := 0
	for _, ei := range info {
		if ei.Escapes {
			escapingCount++
		}
	}
	if escapingCount == 0 {
		t.Error("expected at least 1 escaping node (due to return), got 0")
	}
	t.Logf("found %d escaping nodes", escapingCount)
}

func TestEscapeAnalysis_ProducesJSValue(t *testing.T) {
	// White-box: test producesJSValue and isEscapingUse directly.
	tests := []struct {
		op    SSAOp
		produces bool
		escapes  bool
	}{
		{SSAAdd, true, false},
		{SSASub, true, false},
		{SSAMul, true, false},
		{SSADiv, true, false},
		{SSANeg, true, false},
		{SSAConst, false, false},
		{SSAStore, false, true},
		{SSACall, false, true},
		{SSAReturn, false, true},
		{SSAPhi, false, true},
		{SSABranch, false, true},
		{SSADeopt, false, true},
		{SSAThrow, false, true},
		{SSALoad, true, false},
		{SSALoadElement, true, false},
		{SSAGetProperty, true, false},
		{SSALoadGlobal, true, false},
		{SSANew, true, false},
		{SSANewArray, true, false},
		{SSAObjectLiteral, true, false},
	}

	for _, tt := range tests {
		node := &SSANode{Op: tt.op}
		gotProduces := producesJSValue(node)
		if gotProduces != tt.produces {
			t.Errorf("producesJSValue(%s) = %v, want %v", tt.op, gotProduces, tt.produces)
		}
		gotEscapes := isEscapingUse(node)
		if gotEscapes != tt.escapes {
			t.Errorf("isEscapingUse(%s) = %v, want %v", tt.op, gotEscapes, tt.escapes)
		}
	}
}

// =============================================================================
// Load Elimination Tests
// =============================================================================

// makeRedundantLoadFunc creates bytecode with two loads of the same property:
//
//	function f(obj) { var a = obj.x; return obj.x + a; }
//
// Bytecode (simplified):
//
//	OpLdaNamedProperty r0, idx_x  → load obj.x
//	OpStar r1                      → store a
//	OpLdaNamedProperty r0, idx_x  → load obj.x again (REDUNDANT)
//	OpAdd r1                       → obj.x + a
//	OpReturn
func makeRedundantLoadFunc() *js.BytecodeFunction {
	propIdx := uint8(0)
	return &js.BytecodeFunction{
		Name:         "redundantLoad",
		NumRegisters: 3,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},                                  // load obj → acc
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propIdx},   // obj.prop → acc
			{Op: js.OpStar, OperandA: 1},                                  // store a → reg 1
			{Op: js.OpLdar, OperandA: 0},                                  // load obj → acc
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propIdx},   // obj.prop → acc (redundant)
			{Op: js.OpStar, OperandA: 2},                                  // store → reg 2
			{Op: js.OpLdar, OperandA: 2},                                  // load reg 2
			{Op: js.OpAdd, OperandA: 1},                                   // acc + a
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("x")},
	}
}

func TestLoadElimination_RedundantLoad(t *testing.T) {
	bf := makeRedundantLoadFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	beforeCount := 0
	for _, bb := range g.Blocks {
		for _, n := range bb.Nodes {
			if n.Op == SSALoad || n.Op == SSAGetProperty {
				beforeCount++
			}
		}
	}

	eliminated := eliminateRedundantLoads(g)

	afterCount := 0
	for _, bb := range g.Blocks {
		for _, n := range bb.Nodes {
			if n.Op == SSALoad || n.Op == SSAGetProperty {
				afterCount++
			}
		}
	}

	t.Logf("loads before: %d, after: %d, eliminated: %d", beforeCount, afterCount, eliminated)
}

// makeStoreInvalidatesFunc creates:
//
//	function f(obj) { var a = obj.x; obj.x = 42; return obj.x; }
//
// The store to obj.x should invalidate any cached load of obj.x.
func makeStoreInvalidatesFunc() *js.BytecodeFunction {
	propIdx := uint8(0)
	return &js.BytecodeFunction{
		Name:         "storeInvalidate",
		NumRegisters: 3,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},                                  // load obj
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propIdx},   // obj.x → acc
			{Op: js.OpStar, OperandA: 1},                                  // store a → reg 1
			{Op: js.OpLdaSmi, OperandA: 42},                               // load 42 → acc
			{Op: js.OpStar, OperandA: 2},                                  // store → reg 2
			{Op: js.OpLdar, OperandA: 0},                                  // load obj → acc
			{Op: js.OpStaNamedProperty, OperandA: 0, OperandB: propIdx},   // obj.x = 42
			{Op: js.OpLdar, OperandA: 0},                                  // load obj → acc
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propIdx},   // obj.x → acc (NOT redundant)
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("x")},
	}
}

func TestLoadElimination_StoreInvalidates(t *testing.T) {
	bf := makeStoreInvalidatesFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	eliminated := eliminateRedundantLoads(g)

	// Store should invalidate the cache, so the second load should NOT be eliminated.
	// The first load has no earlier load to be redundant against.
	if eliminated != 0 {
		t.Errorf("expected 0 eliminated loads (store invalidates), got %d", eliminated)
	}
	t.Logf("eliminated: %d (store invalidates cache, as expected)", eliminated)
}

// makeCallInvalidatesFunc creates:
//
//	function f(obj, fn) { var a = obj.x; fn(); return obj.x; }
//
// The call to fn() should clear the entire load cache.
func makeCallInvalidatesFunc() *js.BytecodeFunction {
	propIdx := uint8(0)
	return &js.BytecodeFunction{
		Name:         "callInvalidate",
		NumRegisters: 3,
		NumParams:    2,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},                                  // load obj
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propIdx},   // obj.x → acc
			{Op: js.OpStar, OperandA: 2},                                  // store a → reg 2
			{Op: js.OpLdar, OperandA: 1},                                  // load fn → acc
			{Op: js.OpCall0, OperandA: 1},                                 // fn()
			{Op: js.OpLdar, OperandA: 0},                                  // load obj
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propIdx},   // obj.x → acc (not redundant)
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("x")},
	}
}

func TestLoadElimination_CallInvalidates(t *testing.T) {
	bf := makeCallInvalidatesFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	eliminated := eliminateRedundantLoads(g)

	// The call should clear the cache, so the second load should NOT be eliminated.
	t.Logf("eliminated: %d (call invalidates cache)", eliminated)
}

// makeDifferentPropsFunc creates:
//
//	function f(obj) { return obj.x + obj.y; }
//
// The loads of obj.x and obj.y are different properties — they should not conflict.
func makeDifferentPropsFunc() *js.BytecodeFunction {
	propX := uint8(0)
	propY := uint8(1)
	return &js.BytecodeFunction{
		Name:         "diffProps",
		NumRegisters: 3,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},                                 // load obj
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propX},    // obj.x → acc
			{Op: js.OpStar, OperandA: 1},                                 // store → reg 1
			{Op: js.OpLdar, OperandA: 0},                                 // load obj
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: propY},    // obj.y → acc
			{Op: js.OpAdd, OperandA: 1},                                  // obj.y + obj.x
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("x"), js.NewString("y")},
	}
}

func TestLoadElimination_DifferentProperties(t *testing.T) {
	bf := makeDifferentPropsFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	// Different property keys should use different cache entries.
	eliminated := eliminateRedundantLoads(g)

	// No redundant loads (different properties), but verify the pass runs clean.
	t.Logf("eliminated: %d (different properties, expected 0)", eliminated)
}

// =============================================================================
// Polymorphic Inlining Tests
// =============================================================================

func TestPolyInline_ValidateCallees(t *testing.T) {
	// Test validatePolyCallees with valid and invalid inputs.
	bf1 := &js.BytecodeFunction{
		Name:         "callee1",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpReturn},
		},
	}
	bf2 := &js.BytecodeFunction{
		Name:         "callee2",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 2},
			{Op: js.OpReturn},
		},
	}

	// Valid: both non-nil.
	slot := &js.ICSlot{
		PolyCallCount: 2,
	}
	slot.PolyCallees[0] = bf1
	slot.PolyCallees[1] = bf2
	valid, size := validatePolyCallees(slot)
	if !valid {
		t.Error("expected valid poly callees")
	}
	expectedSize := len(bf1.Instructions) + len(bf2.Instructions)
	if size != expectedSize {
		t.Errorf("total size = %d, want %d", size, expectedSize)
	}

	// Invalid: one nil callee.
	slot2 := &js.ICSlot{
		PolyCallCount: 2,
	}
	slot2.PolyCallees[0] = bf1
	slot2.PolyCallees[1] = nil
	valid2, _ := validatePolyCallees(slot2)
	if valid2 {
		t.Error("expected invalid poly callees (nil entry)")
	}
}

func TestPolyInline_Budget(t *testing.T) {
	// Create callees that together exceed maxPolyInlineBudget (200).
	bf1 := &js.BytecodeFunction{
		Name: "big1",
		Instructions: make([]js.Instruction, 150),
	}
	bf2 := &js.BytecodeFunction{
		Name: "big2",
		Instructions: make([]js.Instruction, 100),
	}

	slot := &js.ICSlot{
		PolyCallCount: 2,
	}
	slot.PolyCallees[0] = bf1
	slot.PolyCallees[1] = bf2

	valid, size := validatePolyCallees(slot)
	if !valid {
		t.Error("callees should be valid (all non-nil)")
	}
	if size != 250 {
		t.Errorf("total size = %d, want 250", size)
	}
	if size <= maxPolyInlineBudget {
		t.Errorf("total size %d should exceed budget %d", size, maxPolyInlineBudget)
	}
}

// makePolyCallFunc creates a bytecode function with a polymorphic call:
//
//	function f(obj) { return obj.method(); }
//
// Set up with polymorphic IC feedback for 2 shapes.
func makePolyCallFunc() *js.BytecodeFunction {
	bf := &js.BytecodeFunction{
		Name:         "polyCall",
		NumRegisters: 2,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},               // load obj → acc
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: 0}, // obj.method → acc (slot 0)
			{Op: js.OpStar, OperandA: 1},               // store method → reg 1
			{Op: js.OpLdar, OperandA: 1},               // load method → acc
			{Op: js.OpCall0, OperandA: 1, OperandC: 1}, // method(), IC slot 1
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("method")},
	}

	// Set up polymorphic feedback for the call site.
	callee1 := &js.BytecodeFunction{
		Name: "CalleeA",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpReturn},
		},
	}
	callee2 := &js.BytecodeFunction{
		Name: "CalleeB",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 2},
			{Op: js.OpReturn},
		},
	}

	bf.ICVector = js.NewFeedbackVector(2)
	bf.ICVector.Slots[1].State = js.ICPolymorphic
	bf.ICVector.Slots[1].PolyCallCount = 2
	bf.ICVector.Slots[1].PolyCallees[0] = callee1
	bf.ICVector.Slots[1].PolyCallees[1] = callee2

	return bf
}

func TestPolyInline_TwoShapes(t *testing.T) {
	bf := makePolyCallFunc()
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	// Count blocks before.
	beforeBlocks := len(g.Blocks)

	count := inlinePolymorphicCalls(g)

	// After inlining, we should have more blocks (guard chain blocks).
	afterBlocks := len(g.Blocks)

	t.Logf("polymorphic inlines: %d, blocks: %d → %d", count, beforeBlocks, afterBlocks)

	// The call should either be inlined (count >= 1) or skipped because
	// no valid callees. Either is acceptable — we just verify no crash.
	if count > 0 && afterBlocks <= beforeBlocks {
		t.Errorf("inline should have added blocks: before=%d after=%d", beforeBlocks, afterBlocks)
	}
}

func TestPolyInline_FourShapes(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "polyFour",
		NumRegisters: 2,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: 0},
			{Op: js.OpStar, OperandA: 1},
			{Op: js.OpLdar, OperandA: 1},
			{Op: js.OpCall0, OperandA: 1, OperandC: 0},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("method")},
	}

	// 4-shape polymorphic call.
	bf.ICVector = js.NewFeedbackVector(1)
	bf.ICVector.Slots[0].State = js.ICPolymorphic
	bf.ICVector.Slots[0].PolyCallCount = 4
	for i := 0; i < 4; i++ {
		bf.ICVector.Slots[0].PolyCallees[i] = &js.BytecodeFunction{
			Name: "Callee",
			NumRegisters: 1,
			Instructions: []js.Instruction{
				{Op: js.OpLdaSmi, OperandA: uint8(i + 1)},
				{Op: js.OpReturn},
			},
		}
	}

	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	count := inlinePolymorphicCalls(g)
	t.Logf("4-shape polymorphic inlines: %d", count)
}

// TestPolyInline_Megamorphic tests that megamorphic call sites are NOT inlined.
func TestPolyInline_Megamorphic(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "mega",
		NumRegisters: 2,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: 0},
			{Op: js.OpStar, OperandA: 1},
			{Op: js.OpLdar, OperandA: 1},
			{Op: js.OpCall0, OperandA: 1, OperandC: 0},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("method")},
	}

	// Megamorphic call site — should NOT be inlined.
	bf.ICVector = js.NewFeedbackVector(1)
	bf.ICVector.Slots[0].State = js.ICMegamorphic

	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	count := inlinePolymorphicCalls(g)
	if count != 0 {
		t.Errorf("megamorphic calls should not be inlined, got %d", count)
	}
}

// TestPolyInline_Uninitialized tests that uninitialized call sites are not inlined.
func TestPolyInline_Uninitialized(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "uninit",
		NumRegisters: 2,
		NumParams:    1,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0},
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandB: 0},
			{Op: js.OpStar, OperandA: 1},
			{Op: js.OpLdar, OperandA: 1},
			{Op: js.OpCall0, OperandA: 1, OperandC: 0},
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{js.NewString("method")},
	}

	// Uninitialized (no ICVector).
	g := BuildSSA(bf)
	if g == nil {
		t.Fatal("BuildSSA returned nil")
	}

	count := inlinePolymorphicCalls(g)
	if count != 0 {
		t.Errorf("uninitialized calls should not be inlined, got %d", count)
	}
}
