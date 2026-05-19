//go:build !amd64

package jit

import (
	"fmt"

	"github.com/lucasdss/v8go/pkg/js"
)

// Global Value Numbering — eliminates redundant computations within basic blocks
// using hash-based value numbering. Processes blocks in reverse postorder
// (the order they appear in g.Blocks).

// gvnContext holds state for a GVN pass over a single SSA graph.
type gvnContext struct {
	exprHash map[string]*SSANode // expression signature → canonical node
}

// runGVN performs global value numbering on the SSA graph.
// Returns the number of redundant nodes eliminated.
//
// For each block, GVN computes a deterministic signature for every pure
// (non-side-effecting) expression node. If the same signature has been
// seen before in the current block, the node is a duplicate and its uses
// are redirected to the canonical node.
//
// This handles:
//   - Common subexpression elimination: a+b computed twice → reuse first
//   - Constant folding: arithmetic on constants → folded constant
func runGVN(g *SSAGraph) int {
	eliminated := 0

	for _, bb := range g.Blocks {
		ctx := &gvnContext{
			exprHash: make(map[string]*SSANode),
		}
		eliminated += processBlockGVN(g, bb, ctx)
	}

	// Remove any nop nodes from all blocks.
	if eliminated > 0 {
		removeNops(g)
	}

	return eliminated
}

// processBlockGVN runs GVN within a single basic block.
func processBlockGVN(g *SSAGraph, bb *SSABasicBlock, ctx *gvnContext) int {
	eliminated := 0

	for i := 0; i < len(bb.Nodes); i++ {
		node := bb.Nodes[i]

		// Skip side-effecting nodes and phi/branch/return.
		if isSideEffecting(node) {
			continue
		}

		// Attempt constant folding first.
		if folded := tryFold(node, g); folded != nil {
			replaceNode(g, node, folded)
			// Insert folded node into block so it's visible to later passes
			// and doesn't get stripped by removeNops.
			bb.Nodes[i] = folded
			eliminated++
			continue
		}

		// Compute expression signature.
		sig := nodeSignature(node)
		if sig == "" {
			continue
		}

		// Check if we've seen this expression before in this block.
		if canonical, ok := ctx.exprHash[sig]; ok && canonical != node {
			// Duplicate found — replace with canonical node.
			replaceNode(g, node, canonical)
			bb.Nodes[i].Op = SSANop
			eliminated++
		} else {
			ctx.exprHash[sig] = node
		}
	}

	return eliminated
}

// nodeSignature creates a deterministic hash string for a pure expression node.
// For commutative operations (Add, Mul, And, Or, Xor), operands are sorted
// by node ID so that a+b and b+a produce the same signature.
func nodeSignature(node *SSANode) string {
	if len(node.Args) == 0 {
		return ""
	}

	switch node.Op {
	// Commutative binary ops.
	case SSAAdd, SSAMul, SSAAnd, SSAOr, SSAXor:
		a, b := node.Args[0].ID, node.Args[1].ID
		if a > b {
			a, b = b, a
		}
		return fmt.Sprintf("%d(%d,%d)", node.Op, a, b)

	// Non-commutative binary ops.
	case SSASub, SSADiv, SSAMod, SSAShl, SSAShr, SSASar:
		return fmt.Sprintf("%d(%d,%d)", node.Op, node.Args[0].ID, node.Args[1].ID)

	// Comparison ops (commutative for Eq/Ne, non-commutative for ordered).
	case SSAEq, SSANe:
		a, b := node.Args[0].ID, node.Args[1].ID
		if a > b {
			a, b = b, a
		}
		return fmt.Sprintf("%d(%d,%d)", node.Op, a, b)

	case SSALt, SSALe, SSAGt, SSAGe:
		return fmt.Sprintf("%d(%d,%d)", node.Op, node.Args[0].ID, node.Args[1].ID)

	// Float ops.
	case SSAFSub, SSAFMul, SSAFDiv, SSAFCmp:
		return fmt.Sprintf("%d(%d,%d)", node.Op, node.Args[0].ID, node.Args[1].ID)

	// Unary ops.
	case SSANeg:
		return fmt.Sprintf("neg(%d)", node.Args[0].ID)
	case SSANot:
		return fmt.Sprintf("not(%d)", node.Args[0].ID)

	// Type casts.
	case SSACastIntToFloat, SSACastFloatToInt, SSATruncateToInt32,
		SSAFloat64ToInt32, SSAInt32ToFloat64:
		return fmt.Sprintf("cast(%d,%d)", node.Op, node.Args[0].ID)

	// Bit manipulation ops with two args.
	case SSAMin, SSAMax:
		a, b := node.Args[0].ID, node.Args[1].ID
		if a > b {
			a, b = b, a
		}
		return fmt.Sprintf("%d(%d,%d)", node.Op, a, b)

	// Single-arg bit manipulation.
	case SSAAbs, SSAClz, SSACtz, SSARev, SSASignExtend8,
		SSASignExtend16, SSASignExtend32, SSAExtractBits:
		return fmt.Sprintf("%d(%d)", node.Op, node.Args[0].ID)

	default:
		return ""
	}
}

// isSideEffecting returns true for ops that have side effects, control flow,
// or memory semantics that prevent safe GVN.
func isSideEffecting(node *SSANode) bool {
	switch node.Op {
	case SSAStore, SSAStoreGlobal, SSAStoreElement, SSASetProperty,
		SSACall, SSANew, SSANewArray, SSAThrow,
		SSADeopt, SSABranch, SSAPhi, SSAReturn,
		SSAInterruptCheck, SSAStackCheck, SSADebugBreak,
		SSANop:
		return true
	}
	return false
}

// tryFold attempts constant folding for binary ops with constant operands.
// Returns a new constant node if folding succeeds, nil otherwise.
func tryFold(node *SSANode, g *SSAGraph) *SSANode {
	if len(node.Args) < 2 {
		return nil
	}

	left, right := node.Args[0], node.Args[1]

	// Both operands must be constants with numeric values.
	lv, lok := constNumVal(left)
	rv, rok := constNumVal(right)
	if !lok || !rok {
		return nil
	}

	var result float64

	switch node.Op {
	case SSAAdd:
		result = lv + rv
	case SSASub:
		result = lv - rv
	case SSAMul:
		result = lv * rv
	case SSADiv:
		if rv == 0 {
			return nil // leave division by zero for runtime
		}
		result = lv / rv
	case SSAAnd:
		result = float64(int64(lv) & int64(rv))
	case SSAOr:
		result = float64(int64(lv) | int64(rv))
	case SSAXor:
		result = float64(int64(lv) ^ int64(rv))
	case SSAShl:
		result = float64(int64(lv) << uint64(int64(rv)))
	case SSAShr:
		result = float64(uint64(int64(lv)) >> uint64(int64(rv)))
	case SSASar:
		result = float64(int64(lv) >> uint64(int64(rv)))
	case SSAMod:
		if rv == 0 {
			return nil
		}
		result = float64(int64(lv) % int64(rv))
	default:
		return nil
	}

	folded := g.newNode(SSAConst, SSAInt32)
	folded.Value = js.NewNumber(result)
	return folded
}

// constNumVal extracts the numeric value from a constant node.
// Returns (value, true) for SSAConst and SSAZero with number values.
func constNumVal(node *SSANode) (float64, bool) {
	switch node.Op {
	case SSAZero:
		return 0, true
	case SSAConst:
		if node.Value.IsNumber() {
			return node.Value.NumVal, true
		}
	}
	return 0, false
}

// removeNops removes all SSANop nodes from every basic block.
func removeNops(g *SSAGraph) {
	for _, bb := range g.Blocks {
		filtered := bb.Nodes[:0]
		for _, n := range bb.Nodes {
			if n.Op != SSANop {
				filtered = append(filtered, n)
			}
		}
		bb.Nodes = filtered
	}
}
