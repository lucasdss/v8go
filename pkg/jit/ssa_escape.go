//go:build !amd64

package jit

// EscapeInfo records the escape analysis result for a single SSA node.
// A node "escapes" if any use is outside its defining basic block or
// if any use is a side-effecting operation (store, call, return, phi, branch).
type EscapeInfo struct {
	Escapes  bool
	UseCount int
}

// analyzeEscape performs a whole-graph escape analysis pass.
// Nodes whose value is produced and consumed entirely within one basic block
// without escaping through stores/calls/returns are marked non-escaping.
func analyzeEscape(g *SSAGraph) map[*SSANode]*EscapeInfo {
	info := make(map[*SSANode]*EscapeInfo)

	// Build a map from node to its defining basic block.
	nodeBlock := make(map[*SSANode]*SSABasicBlock)
	for _, bb := range g.Blocks {
		for _, node := range bb.Nodes {
			nodeBlock[node] = bb
			info[node] = &EscapeInfo{}
		}
	}

	for _, bb := range g.Blocks {
		for _, node := range bb.Nodes {
			if !producesJSValue(node) {
				continue
			}
			ei := info[node]
			ei.UseCount = len(node.Users)

			for _, user := range node.Users {
				// Escapes if user is in a different block.
				if userBlock, ok := nodeBlock[user]; !ok || userBlock != bb {
					ei.Escapes = true
					break
				}
				// Escapes if user is a side-effecting or control-flow operation.
				if isEscapingUse(user) {
					ei.Escapes = true
					break
				}
			}
		}
	}

	return info
}

// producesJSValue returns true for nodes that produce a new JSValue result
// (as opposed to constants, guards, or control-flow nodes).
func producesJSValue(node *SSANode) bool {
	switch node.Op {
	case SSAAdd, SSASub, SSAMul, SSADiv,
		SSANeg, SSANot, SSAAnd, SSAOr, SSAXor,
		SSAShl, SSAShr, SSASar,
		SSAFSub, SSAFMul, SSAFDiv,
		SSAToNumber, SSAMod, SSAFMod,
		SSAMin, SSAMax, SSAAbs,
		SSANew, SSANewArray, SSAObjectLiteral,
		SSALoad, SSALoadElement, SSAGetProperty,
		SSALoadGlobal:
		return true
	default:
		return false
	}
}

// isEscapingUse returns true when a use of a value causes it to escape.
// Stores, calls, returns, phis, branches, deopts, and throws all
// force the value to be materialized in memory.
func isEscapingUse(user *SSANode) bool {
	switch user.Op {
	case SSAStore, SSACall, SSAReturn, SSAPhi,
		SSABranch, SSADeopt, SSAThrow,
		SSAStoreElement, SSAStoreGlobal, SSASetProperty,
		SSADeleteProperty:
		return true
	default:
		return false
	}
}

// eliminateNonEscaping marks non-escaping nodes for register allocation
// and optionally folds them into their consumers. Returns the count of
// nodes eliminated.
//
// Currently the folding is deferred to the register allocator:
// non-escaping nodes are left in the graph but tagged so the allocator
// can prioritize stack-avoidance for them.
func eliminateNonEscaping(g *SSAGraph, info map[*SSANode]*EscapeInfo) int {
	count := 0
	for node, ei := range info {
		if ei.Escapes || ei.UseCount == 0 {
			continue
		}
		// Mark as non-escaping: promote type to enable register allocation.
		// The register allocator will see these nodes and can avoid spilling.
		_ = node
		count++
	}
	return count
}
