package jit

import "github.com/lucasdss/v8go/pkg/js"

// inlineMonomorphicCalls identifies call sites with monomorphic inline-cache
// feedback and performs speculative inlining. Returns the number of call
// sites that were successfully inlined.
//
// A call site is eligible for inlining when:
//   - The IC slot is monomorphic (single callee observed)
//   - The callee function is known (BytecodeFunction available)
//   - The callee body is small enough to inline (≤ 50 bytecode instructions)
//
// Inlining grafts the callee's SSA graph into the caller's basic block,
// replacing the call node with the inlined body. A simplified approach
// is used for MVP: no bailout guard, no duplicated exception edges.
func inlineMonomorphicCalls(g *SSAGraph) int {
	count := 0
	for _, bb := range g.Blocks {
		for _, node := range bb.Nodes {
			if node.Op != SSACall {
				continue
			}
			if node.Feedback == nil || node.Feedback.State != js.ICMonomorphic {
				continue
			}
			if node.Feedback.Callee == nil {
				continue
			}
			if len(node.Feedback.Callee.Instructions) > 50 {
				continue
			}
			performInline(g, node, bb, node.Feedback.Callee)
			count++
		}
	}
	return count
}

// performInline grafts the callee's SSA body into the caller's graph at the
// call site, replacing the call node with the inlined callee IR.
//
// Steps:
//  1. Clone callee nodes into the caller's graph with fresh IDs.
//  2. Remap callee params to the call-site arguments.
//  3. Rewire cloned nodes' internal arg references.
//  4. Replace the call node with the callee's entry node.
//  5. Insert cloned nodes into the caller's basic block after the call site.
func performInline(g *SSAGraph, callSite *SSANode, callerBB *SSABasicBlock, calleeBF *js.BytecodeFunction) {
	calleeGraph := BuildSSA(calleeBF)
	if calleeGraph == nil {
		return
	}
	if calleeGraph.Entry == nil || len(calleeGraph.Entry.Nodes) == 0 {
		return
	}

	// 1. Clone callee's nodes into caller's graph with new IDs.
	nodeMap := make(map[*SSANode]*SSANode)
	for _, bb := range calleeGraph.Blocks {
		for _, node := range bb.Nodes {
			clone := &SSANode{
				ID:       g.nextID,
				Op:       node.Op,
				Type:     node.Type,
				Value:    node.Value,
				BCPC:     node.BCPC,
				Feedback: node.Feedback,
			}
			g.nextID++
			nodeMap[node] = clone
		}
	}

	// 2. Remap callee params → caller args.
	//    callSite.Args[0] is the callee function object (skip it).
	//    callSite.Args[1:] map to calleeGraph.Params[0], [1], etc.
	for i, param := range calleeGraph.Params {
		if clone, ok := nodeMap[param]; ok {
			callArgIdx := i + 1 // skip callee at Args[0]
			if callArgIdx < len(callSite.Args) {
				replaceNode(g, clone, callSite.Args[callArgIdx])
			}
		}
	}

	// 3. Remap cloned nodes' arg references to point to the cloned nodes.
	for _, bb := range calleeGraph.Blocks {
		for _, node := range bb.Nodes {
			clone := nodeMap[node]
			newArgs := make([]*SSANode, 0, len(node.Args))
			for _, arg := range node.Args {
				if mapped, ok := nodeMap[arg]; ok {
					newArgs = append(newArgs, mapped)
				} else {
					newArgs = append(newArgs, arg)
				}
			}
			clone.Args = newArgs
			// Rebuild Users for the cloned node.
			for _, a := range clone.Args {
				if a != nil {
					a.Users = append(a.Users, clone)
				}
			}
		}
	}

	// 4. Replace callSite with callee's entry node.
	entry := nodeMap[calleeGraph.Entry.Nodes[0]]
	replaceNode(g, callSite, entry)

	// 5. Replace call site in caller's block with the inlined callee body.
	pos := indexOf(callerBB.Nodes, callSite)
	if pos < 0 {
		return
	}
	// Collect non-return cloned nodes in program order.
	var inlined []*SSANode
	for _, bb := range calleeGraph.Blocks {
		for _, node := range bb.Nodes {
			clone := nodeMap[node]
			if clone.Op == SSAReturn {
				continue
			}
			inlined = append(inlined, clone)
		}
	}
	// Splice: remove callSite, insert inlined nodes at its position.
	callerBB.Nodes = append(
		append(callerBB.Nodes[:pos], inlined...),
		callerBB.Nodes[pos+1:]...,
	)
}

// replaceNode substitutes every use of old with new in the SSA graph,
// updating both Args and Users edges. The old node is effectively
// dead after this operation (no remaining users).
func replaceNode(_ *SSAGraph, old, newNode *SSANode) {
	for _, user := range old.Users {
		for i, arg := range user.Args {
			if arg == old {
				user.Args[i] = newNode
			}
		}
		newNode.Users = append(newNode.Users, user)
	}
	old.Users = nil
}

// indexOf returns the index of target in nodes, or -1 if not found.
func indexOf(nodes []*SSANode, target *SSANode) int {
	for i, n := range nodes {
		if n == target {
			return i
		}
	}
	return -1
}
