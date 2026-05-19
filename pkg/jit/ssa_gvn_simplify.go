//go:build !amd64

package jit

// simplifyAlgebraic applies peephole algebraic simplifications to the SSA graph.
// These transformations reduce instruction count without changing semantics:
//
//	x + 0 → x      0 + x → x
//	x - 0 → x      x - x → 0
//	x * 1 → x      1 * x → x
//	x * 0 → 0      0 * x → 0
//	x / 1 → x
//	x << 0 → x     x >> 0 → x
//
// Returns the number of nodes simplified.
func simplifyAlgebraic(g *SSAGraph) int {
	count := 0

	for _, bb := range g.Blocks {
		for i, node := range bb.Nodes {
			if simplified := trySimplify(node, g); simplified != nil {
				replaceNode(g, node, simplified)
				// If the simplified node is a new node (e.g., zero for x-x),
				// insert it into the block so it's visible.
				if simplified != node.Args[0] && simplified != node.Args[1] {
					bb.Nodes[i] = simplified
				} else {
					bb.Nodes[i].Op = SSANop
				}
				count++
			}
		}
	}

	if count > 0 {
		removeNops(g)
	}

	return count
}

// trySimplify attempts to simplify a node using algebraic identities.
// Returns a replacement node if simplification is possible, nil otherwise.
func trySimplify(node *SSANode, g *SSAGraph) *SSANode {
	if len(node.Args) < 2 {
		return nil
	}

	left, right := node.Args[0], node.Args[1]

	switch node.Op {
	case SSAAdd:
		// x + 0 = x
		if isZeroNode(right) {
			return left
		}
		// 0 + x = x
		if isZeroNode(left) {
			return right
		}

	case SSASub:
		// x - 0 = x
		if isZeroNode(right) {
			return left
		}
		// x - x = 0
		if left == right {
			return newZeroNode(g)
		}

	case SSAMul:
		// x * 1 = x
		if isOneNode(right) {
			return left
		}
		// 1 * x = x
		if isOneNode(left) {
			return right
		}
		// x * 0 = 0, 0 * x = 0
		if isZeroNode(left) || isZeroNode(right) {
			return newZeroNode(g)
		}

	case SSADiv:
		// x / 1 = x
		if isOneNode(right) {
			return left
		}

	case SSAShl, SSAShr, SSASar:
		// x << 0 = x, x >> 0 = x, x >>> 0 = x
		if isZeroNode(right) {
			return left
		}
	}

	return nil
}

// isZeroNode returns true if the node represents the constant 0.
func isZeroNode(node *SSANode) bool {
	if node.Op == SSAZero {
		return true
	}
	if node.Op == SSAConst && node.Value.IsNumber() && node.Value.NumVal == 0 {
		return true
	}
	return false
}

// isOneNode returns true if the node represents the constant 1.
func isOneNode(node *SSANode) bool {
	if node.Op == SSAConst && node.Value.IsNumber() && node.Value.NumVal == 1 {
		return true
	}
	return false
}

// newZeroNode creates a fresh SSAZero node in the graph.
func newZeroNode(g *SSAGraph) *SSANode {
	return g.newNode(SSAZero, SSAInt32)
}


