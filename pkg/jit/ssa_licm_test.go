//go:build !amd64

package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// =============================================================================
// LICM — Loop-Invariant Code Motion Tests
// =============================================================================

// makeLoopSSAGraph builds a simple loop SSA graph:
//
//	Block 0 (entry):    c1=Const(1), c2=Const(2) → branch to header
//	Block 1 (header):   add = c1+c2 (INVARIANT), use add → branch to body/exit
//	Block 2 (body):     back edge → header (creates the loop)
//	Block 3 (exit):     return
//
// Edges: 0→1, 1→2, 2→1 (back edge), 1→3
// LICM should hoist the SSAAdd from block 1 into a pre-header.
func makeLoopSSAGraph() *SSAGraph {
	g := NewSSAGraph()

	// Constants — defined in entry block (outside loop).
	c1 := g.newNode(SSAConst, SSAInt32)
	c1.Value = js.NewNumber(1)
	c2 := g.newNode(SSAConst, SSAInt32)
	c2.Value = js.NewNumber(2)

	// Entry block (block 0) — outside the loop.
	entry := &SSABasicBlock{ID: 0, StartPC: 0, EndPC: 0}
	entry.Nodes = append(entry.Nodes, c1, c2)

	// Loop header (block 1) — contains invariant add.
	header := &SSABasicBlock{ID: 1, StartPC: 1, EndPC: 1}
	add := g.newNode(SSAAdd, SSAInt32, c1, c2) // 1+2 → invariant
	header.Nodes = append(header.Nodes, add)

	// Loop body (block 2) — back edge to header.
	lbody := &SSABasicBlock{ID: 2, StartPC: 2, EndPC: 2}

	// Exit block (block 3).
	exit := &SSABasicBlock{ID: 3, StartPC: 3, EndPC: 3}

	// Wire edges.
	entry.Successors = append(entry.Successors, header)
	header.Predecessors = append(header.Predecessors, entry)

	header.Successors = append(header.Successors, lbody, exit)
	lbody.Predecessors = append(lbody.Predecessors, header)
	exit.Predecessors = append(exit.Predecessors, header)

	// Back edge: body → header.
	lbody.Successors = append(lbody.Successors, header)
	header.Predecessors = append(header.Predecessors, lbody) // second predecessor

	g.Blocks = []*SSABasicBlock{entry, header, lbody, exit}
	g.Entry = entry

	return g
}

func TestLICM_BasicHoist(t *testing.T) {
	g := makeLoopSSAGraph()

	// Count SSAAdd nodes before LICM.
	addBefore := countOp(g, SSAAdd)

	// Verify loop detection.
	loops := findLoops(g)
	if len(loops) == 0 {
		t.Fatal("expected at least one loop (back edge from body to header)")
	}
	t.Logf("found %d loop(s)", len(loops))

	// Verify the header is block 1.
	loop := loops[0]
	if loop.Header != 1 {
		t.Errorf("expected loop header at block 1, got block %d", loop.Header)
	}

	// Verify invariants are detected.
	invariant := markLoopInvariants(g, loop)
	if len(invariant) == 0 {
		t.Fatal("expected at least one invariant node (SSAAdd of constants)")
	}
	t.Logf("found %d invariant node(s)", len(invariant))

	hoisted := runLICM(g)
	t.Logf("hoisted: %d instructions", hoisted)

	if hoisted != 1 {
		t.Errorf("expected 1 hoisted node (the SSAAdd), got %d", hoisted)
	}

	addAfter := countOp(g, SSAAdd)
	if addAfter > addBefore {
		t.Errorf("SSAAdd count should not increase: before=%d after=%d", addBefore, addAfter)
	}

	// Verify a pre-header block was created.
	if len(g.Blocks) <= 4 {
		t.Errorf("expected pre-header block to be added: before=4, after=%d", len(g.Blocks))
	}

	// The SSAAdd should be in the pre-header.
	preHeader := g.Blocks[len(g.Blocks)-1] // last block added
	found := false
	for _, n := range preHeader.Nodes {
		if n.Op == SSAAdd {
			found = true
			break
		}
	}
	if !found {
		t.Error("SSAAdd should be in the pre-header block")
	}
}

// TestLICM_SideEffectNotHoisted verifies that side-effecting instructions
// (store, call, etc.) are never hoisted out of loops.
func TestLICM_SideEffectNotHoisted(t *testing.T) {
	g := NewSSAGraph()

	// Constants.
	c1 := g.newNode(SSAConst, SSAInt32)
	c1.Value = js.NewNumber(1)

	// Entry block.
	entry := &SSABasicBlock{ID: 0, StartPC: 0, EndPC: 0}
	entry.Nodes = append(entry.Nodes, c1)

	// Loop header — contains a store (side-effecting).
	header := &SSABasicBlock{ID: 1, StartPC: 1, EndPC: 1}
	store := g.newNode(SSAStore, SSAAny, c1) // store is side-effecting
	header.Nodes = append(header.Nodes, store)

	// Loop body — back edge.
	lbody := &SSABasicBlock{ID: 2, StartPC: 2, EndPC: 2}

	// Exit block.
	exit := &SSABasicBlock{ID: 3, StartPC: 3, EndPC: 3}

	// Wire edges.
	entry.Successors = append(entry.Successors, header)
	header.Predecessors = append(header.Predecessors, entry)

	header.Successors = append(header.Successors, lbody, exit)
	lbody.Predecessors = append(lbody.Predecessors, header)
	exit.Predecessors = append(exit.Predecessors, header)

	// Back edge: body → header.
	lbody.Successors = append(lbody.Successors, header)
	header.Predecessors = append(header.Predecessors, lbody)

	g.Blocks = []*SSABasicBlock{entry, header, lbody, exit}
	g.Entry = entry

	storeBefore := countOp(g, SSAStore)
	hoisted := runLICM(g)
	storeAfter := countOp(g, SSAStore)

	t.Logf("hoisted: %d, SSAStore before=%d after=%d", hoisted, storeBefore, storeAfter)

	if hoisted != 0 {
		t.Errorf("side-effecting store should not be hoisted, got %d hoisted", hoisted)
	}
	if storeAfter != storeBefore {
		t.Errorf("SSAStore count changed: before=%d after=%d", storeBefore, storeAfter)
	}
}

// TestLICM_DefinedOutsideLoop tests that nodes defined outside the loop body
// are recognized as invariant and values flowing in from outside are properly
// handled (their defining nodes are NOT moved since they are already outside).
func TestLICM_DefinedOutsideLoop(t *testing.T) {
	g := NewSSAGraph()

	// Constants defined in entry (outside loop).
	c1 := g.newNode(SSAConst, SSAInt32)
	c1.Value = js.NewNumber(3)
	c2 := g.newNode(SSAConst, SSAInt32)
	c2.Value = js.NewNumber(4)

	// Entry block.
	entry := &SSABasicBlock{ID: 0, StartPC: 0, EndPC: 0}
	mul := g.newNode(SSAMul, SSAInt32, c1, c2) // 3*4 = 12, outside loop
	entry.Nodes = append(entry.Nodes, c1, c2, mul)

	// Loop header — uses mul (defined outside).
	header := &SSABasicBlock{ID: 1, StartPC: 1, EndPC: 1}
	add := g.newNode(SSAAdd, SSAInt32, mul, c1) // mul + c1 → invariant
	header.Nodes = append(header.Nodes, add)

	// Loop body.
	lbody := &SSABasicBlock{ID: 2, StartPC: 2, EndPC: 2}

	// Exit.
	exit := &SSABasicBlock{ID: 3, StartPC: 3, EndPC: 3}

	// Wire.
	entry.Successors = append(entry.Successors, header)
	header.Predecessors = append(header.Predecessors, entry)
	header.Successors = append(header.Successors, lbody, exit)
	lbody.Predecessors = append(lbody.Predecessors, header)
	exit.Predecessors = append(exit.Predecessors, header)
	lbody.Successors = append(lbody.Successors, header)
	header.Predecessors = append(header.Predecessors, lbody)

	g.Blocks = []*SSABasicBlock{entry, header, lbody, exit}
	g.Entry = entry

	// The add in the header uses mul (defined outside loop) and c1 (constant).
	// Both are invariant, so add is invariant and should be hoisted.
	loops := findLoops(g)
	if len(loops) == 0 {
		t.Fatal("expected to find a loop")
	}

	invariant := markLoopInvariants(g, loops[0])
	// c1 is constant (invariant), add uses mul (outside body) and c1 (constant).
	// add should be detected as invariant.
	if len(invariant) == 0 {
		t.Fatal("expected invariant nodes for add using external def")
	}
	t.Logf("invariant nodes found: %d", len(invariant))

	hoisted := runLICM(g)
	t.Logf("hoisted: %d", hoisted)

	if hoisted == 0 {
		t.Error("expected at least 1 hoisted node (add using external def)")
	}
}

// TestLICM_NoLoopNoHoisting verifies that functions without loops
// produce zero hoisted instructions.
func TestLICM_NoLoopNoHoisting(t *testing.T) {
	g := NewSSAGraph()

	c1 := g.newNode(SSAConst, SSAInt32)
	c1.Value = js.NewNumber(1)
	c2 := g.newNode(SSAConst, SSAInt32)
	c2.Value = js.NewNumber(2)

	bb := &SSABasicBlock{ID: 0, StartPC: 0, EndPC: 2}
	add := g.newNode(SSAAdd, SSAInt32, c1, c2)
	bb.Nodes = append(bb.Nodes, c1, c2, add)

	g.Blocks = []*SSABasicBlock{bb}
	g.Entry = bb

	hoisted := runLICM(g)
	if hoisted != 0 {
		t.Errorf("expected 0 hoisted nodes for loop-free graph, got %d", hoisted)
	}
}

// TestLICM_FindLoops_NoBackEdge verifies that a DAG (no back edges)
// produces zero loops.
func TestLICM_FindLoops_NoBackEdge(t *testing.T) {
	g := NewSSAGraph()

	bb0 := &SSABasicBlock{ID: 0, StartPC: 0, EndPC: 0}
	bb1 := &SSABasicBlock{ID: 1, StartPC: 1, EndPC: 1}
	bb2 := &SSABasicBlock{ID: 2, StartPC: 2, EndPC: 2}

	// Forward edges only: 0→1, 0→2.
	bb0.Successors = append(bb0.Successors, bb1, bb2)
	bb1.Predecessors = append(bb1.Predecessors, bb0)
	bb2.Predecessors = append(bb2.Predecessors, bb0)

	g.Blocks = []*SSABasicBlock{bb0, bb1, bb2}
	g.Entry = bb0

	loops := findLoops(g)
	if len(loops) != 0 {
		t.Errorf("expected 0 loops for DAG, got %d", len(loops))
	}
}

// TestLICM_FindLoops_BackEdge verifies that a back edge produces exactly one loop.
func TestLICM_FindLoops_BackEdge(t *testing.T) {
	g := NewSSAGraph()

	bb0 := &SSABasicBlock{ID: 0, StartPC: 0, EndPC: 0}
	bb1 := &SSABasicBlock{ID: 1, StartPC: 1, EndPC: 1}

	// Edge 0→1 (forward), 1→0 (back edge).
	bb0.Successors = append(bb0.Successors, bb1)
	bb1.Predecessors = append(bb1.Predecessors, bb0)
	bb1.Successors = append(bb1.Successors, bb0) // back edge
	bb0.Predecessors = append(bb0.Predecessors, bb1)

	g.Blocks = []*SSABasicBlock{bb0, bb1}
	g.Entry = bb0

	loops := findLoops(g)
	if len(loops) != 1 {
		t.Fatalf("expected 1 loop, got %d", len(loops))
	}
	if loops[0].Header != 0 {
		t.Errorf("expected header at block 0, got %d", loops[0].Header)
	}
	if loops[0].BackEdge != 1 {
		t.Errorf("expected back edge source at block 1, got %d", loops[0].BackEdge)
	}
}

// TestLICM_IsLICMSideEffecting tests the whitelist/blocklist of ops.
func TestLICM_IsLICMSideEffecting(t *testing.T) {
	// Pure ops should NOT be side-effecting.
	pureOps := []SSAOp{SSAAdd, SSASub, SSAMul, SSADiv, SSANeg, SSAConst, SSAAnd, SSAOr, SSAXor}
	for _, op := range pureOps {
		node := &SSANode{Op: op}
		if isLICMSideEffecting(node) {
			t.Errorf("%s should not be side-effecting for LICM", op)
		}
	}

	// Side-effecting ops should be detected.
	sideOps := []SSAOp{SSAStore, SSACall, SSAReturn, SSABranch, SSADeopt, SSAThrow}
	for _, op := range sideOps {
		node := &SSANode{Op: op}
		if !isLICMSideEffecting(node) {
			t.Errorf("%s should be side-effecting for LICM", op)
		}
	}
}


