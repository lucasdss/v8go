//go:build !amd64

package jit

// LICM — Loop-Invariant Code Motion.
//
// Detects natural loops in the SSA graph (via back edges in RPO), identifies
// instructions that produce the same value on every iteration, and hoists them
// into a newly created pre-header block so they execute only once.
//
// A value is loop-invariant when:
//  1. It is a constant (SSAConst, SSAZero, SSAMoveConst), or
//  2. All of its input operands are loop-invariant, or
//  3. It is defined outside the loop body.
//
// Side-effecting instructions (store, call, deopt, etc.) are never hoisted.

// naturalLoop represents a detected loop in the SSA graph.
type naturalLoop struct {
	Header   int   // index in g.Blocks of the loop header block
	BackEdge int   // index in g.Blocks of the block whose edge back to Header forms the loop
	Body     []int // indices in g.Blocks of all blocks belonging to this loop
}

// findLoops detects natural loops by scanning for back edges.
//
// Blocks are in reverse postorder (RPO): forward edges go from lower to higher
// indices. An edge A→B where index(B) ≤ index(A) is a back edge, and B is the
// loop header.
func findLoops(g *SSAGraph) []*naturalLoop {
	// Map block pointer → index in g.Blocks.
	blockIdx := make(map[*SSABasicBlock]int)
	for i, bb := range g.Blocks {
		blockIdx[bb] = i
	}

	var loops []*naturalLoop

	for srcIdx, bb := range g.Blocks {
		for _, succ := range bb.Successors {
			dstIdx := blockIdx[succ]
			// Back edge: successor appears at or before the current block.
			if dstIdx <= srcIdx {
				body := collectLoopBody(g, blockIdx, dstIdx, srcIdx)
				loops = append(loops, &naturalLoop{
					Header:   dstIdx,
					BackEdge: srcIdx,
					Body:     body,
				})
			}
		}
	}

	return loops
}

// collectLoopBody collects all blocks in the loop body.
//
// The body of a natural loop with header H and back edge B→H is:
//
//	{ H } ∪ { all blocks reachable from B by following predecessors
//	           without passing through H }
//
// This works for reducible flow graphs (single-entry loops).
func collectLoopBody(g *SSAGraph, blockIdx map[*SSABasicBlock]int, header, backEdge int) []int {
	bodySet := make(map[int]bool)
	bodySet[header] = true // header is part of the body

	// DFS through predecessors starting from the back-edge source.
	stack := []int{backEdge}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if bodySet[cur] {
			continue
		}
		bodySet[cur] = true

		bb := g.Blocks[cur]
		for _, pred := range bb.Predecessors {
			pi := blockIdx[pred]
			if !bodySet[pi] {
				stack = append(stack, pi)
			}
		}
	}

	body := make([]int, 0, len(bodySet))
	for idx := range bodySet {
		body = append(body, idx)
	}
	return body
}

// isLICMSideEffecting returns true for ops that must not be hoisted because
// they have observable side effects, control-flow implications, or memory
// semantics.
func isLICMSideEffecting(node *SSANode) bool {
	return isSideEffecting(node)
}

// markLoopInvariants runs a fixed-point analysis to identify loop-invariant
// instructions within the given loop.
//
// A node is invariant if:
//   - It is a constant (SSAConst, SSAZero, SSAMoveConst), or
//   - All its argument nodes are already marked invariant (and it has no side
//     effects), or
//   - It is defined in a block that is NOT part of the loop body.
func markLoopInvariants(g *SSAGraph, loop *naturalLoop) map[int]bool {
	bodySet := make(map[int]bool)
	for _, b := range loop.Body {
		bodySet[b] = true
	}

	// Build node→block map for quick "is this node defined outside the loop?" lookups.
	nodeBlock := buildNodeBlockMap(g)

	invariant := make(map[int]bool)

	// Seed: constants defined inside the loop are invariant.
	for _, blockIdx := range loop.Body {
		for _, node := range g.Blocks[blockIdx].Nodes {
			if node.Op == SSAConst || node.Op == SSAZero || node.Op == SSAMoveConst {
				invariant[node.ID] = true
			}
		}
	}

	// Fixed-point iteration: mark nodes whose inputs are all invariant.
	changed := true
	for changed {
		changed = false
		for _, blockIdx := range loop.Body {
			bb := g.Blocks[blockIdx]
			for _, node := range bb.Nodes {
				if invariant[node.ID] {
					continue
				}
				if isLICMSideEffecting(node) {
					continue
				}
				if nodeAllArgsInvariant(node, invariant, nodeBlock, bodySet) {
					invariant[node.ID] = true
					changed = true
				}
			}
		}
	}

	return invariant
}

// buildNodeBlockMap returns a map from node pointer to the index of its
// defining basic block in g.Blocks, or -1 if not found.
func buildNodeBlockMap(g *SSAGraph) map[*SSANode]int {
	m := make(map[*SSANode]int)
	for i, bb := range g.Blocks {
		for _, node := range bb.Nodes {
			m[node] = i
		}
	}
	return m
}

// nodeAllArgsInvariant returns true when every non-nil argument of node is
// either in the invariant set or defined in a block outside the loop body.
// Arguments defined outside the loop are inherently loop-invariant.
func nodeAllArgsInvariant(node *SSANode, invariant map[int]bool, nodeBlock map[*SSANode]int, bodySet map[int]bool) bool {
	for _, arg := range node.Args {
		if arg == nil {
			continue
		}
		// Already marked invariant.
		if invariant[arg.ID] {
			continue
		}
		// Defined outside the loop body → positionally invariant.
		blockIdx, ok := nodeBlock[arg]
		if ok && !bodySet[blockIdx] {
			continue
		}
		return false
	}
	return true
}

// hoistLoopInvariants creates a pre-header block, moves all invariant
// instructions from the loop body into it, and rewires the CFG so that
// outside predecessors branch to the pre-header instead of directly to
// the loop header. Returns the number of instructions hoisted.
func hoistLoopInvariants(g *SSAGraph, loop *naturalLoop, invariant map[int]bool) int {
	if len(invariant) == 0 {
		return 0
	}

	// Filter: only hoist invariants that are defined inside the loop body.
	// (Nodes defined outside the body are already invariant by position and
	// don't need to be moved.)
	bodySet := make(map[int]bool)
	for _, b := range loop.Body {
		bodySet[b] = true
	}

	// Create pre-header block.
	preHeader := &SSABasicBlock{
		ID:      len(g.Blocks),
		StartPC: -1,
		EndPC:   -1,
	}
	g.Blocks = append(g.Blocks, preHeader)

	header := g.Blocks[loop.Header]

	// Identify outside predecessors of the header (not in loop body).
	var outsidePredIndices []int
	for _, pred := range header.Predecessors {
		predIdx := blockIndexByPtr(g, pred)
		if predIdx >= 0 && !bodySet[predIdx] {
			outsidePredIndices = append(outsidePredIndices, predIdx)
		}
	}

	// Collect and move invariant nodes.
	count := 0
	for _, blockIdx := range loop.Body {
		bb := g.Blocks[blockIdx]
		filtered := bb.Nodes[:0]
		for _, node := range bb.Nodes {
			if invariant[node.ID] {
				preHeader.Nodes = append(preHeader.Nodes, node)
				count++
			} else {
				filtered = append(filtered, node)
			}
		}
		bb.Nodes = filtered
	}

	// Re-wire CFG: outside preds → preHeader → header.
	for _, predIdx := range outsidePredIndices {
		pred := g.Blocks[predIdx]
		for i, succ := range pred.Successors {
			if succ == header {
				pred.Successors[i] = preHeader
			}
		}
		preHeader.Predecessors = append(preHeader.Predecessors, pred)
	}
	preHeader.Successors = append(preHeader.Successors, header)

	// Remove outside predecessors from header's predecessor list, add preHeader.
	newPreds := make([]*SSABasicBlock, 0, len(header.Predecessors))
	for _, pred := range header.Predecessors {
		isOutside := false
		for _, opIdx := range outsidePredIndices {
			if g.Blocks[opIdx] == pred {
				isOutside = true
				break
			}
		}
		if !isOutside {
			newPreds = append(newPreds, pred)
		}
	}
	newPreds = append(newPreds, preHeader)
	header.Predecessors = newPreds

	return count
}

// blockIndexByPtr returns the position of bb in g.Blocks, or -1 if not found.
func blockIndexByPtr(g *SSAGraph, target *SSABasicBlock) int {
	for i, bb := range g.Blocks {
		if bb == target {
			return i
		}
	}
	return -1
}

// runLICM performs loop-invariant code motion on all detected loops.
// Returns the number of instructions hoisted.
func runLICM(g *SSAGraph) int {
	loops := findLoops(g)
	total := 0
	for _, loop := range loops {
		invariant := markLoopInvariants(g, loop)
		total += hoistLoopInvariants(g, loop, invariant)
	}
	return total
}
