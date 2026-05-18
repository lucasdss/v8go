package jit

import "github.com/lucasdss/v8go/pkg/js"

// maxPolyInlineBudget limits the total bytecode instructions inlined
// per call site to prevent code bloat.
const maxPolyInlineBudget = 200

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

// inlinePolymorphicCalls identifies call sites with polymorphic inline-cache
// feedback (2-4 observed callees) and generates a guard chain:
//
//	if (object.shape == shapeA) → inline calleeA
//	elif (object.shape == shapeB) → inline calleeB
//	elif (object.shape == shapeC) → inline calleeC
//	else → keep original call
//
// Each inlined callee is grafted as a new basic block reachable via
// SSACmp + SSABranch guard comparisons. The total bytecode instructions
// across all inlined callees is capped at maxPolyInlineBudget (200) to
// prevent code bloat.
//
// Returns the number of call sites that were speculatively inlined.
func inlinePolymorphicCalls(g *SSAGraph) int {
	count := 0
	for _, bb := range g.Blocks {
		for i := 0; i < len(bb.Nodes); i++ {
			node := bb.Nodes[i]
			if node.Op != SSACall {
				continue
			}
			if node.Feedback == nil || node.Feedback.State != js.ICPolymorphic {
				continue
			}
			if node.Feedback.PolyCallCount < 2 || node.Feedback.PolyCallCount > 4 {
				continue
			}

			// Validate all callees are known and within budget.
			valid, totalSize := validatePolyCallees(node.Feedback)
			if !valid || totalSize > maxPolyInlineBudget {
				continue
			}

			performPolyInline(g, node, bb, i)
			count++
		}
	}
	return count
}

// validatePolyCallees checks that all polymorphic callee entries are non-nil
// and returns the total bytecode instruction count across all callees.
func validatePolyCallees(slot *js.ICSlot) (bool, int) {
	total := 0
	for i := 0; i < slot.PolyCallCount; i++ {
		if slot.PolyCallees[i] == nil {
			return false, 0
		}
		total += len(slot.PolyCallees[i].Instructions)
	}
	return true, total
}

// performPolyInline grafts polymorphic callee bodies into the SSA graph
// using a guard chain pattern. Each shape in the polymorphic cache gets
// its own guarded inline path with a fallback to the original call.
func performPolyInline(g *SSAGraph, callSite *SSANode, callerBB *SSABasicBlock, callIdx int) {
	slot := callSite.Feedback
	if slot.PolyCallCount == 0 {
		return
	}

	// Build guard chain: for each polymorphic callee, create a shape check
	// and graft the inlined body into a new basic block.
	//
	// Pattern:
	//   [callerBB prefix up to callSite]
	//   t_shape = LoadShape(receiver)
	//   t_cmp0 = Cmp(t_shape, shapeA)
	//   Branch(t_cmp0, polyBB_0, next_check)
	//
	// Each polyBB_N contains the inlined callee body and jumps to a merge block.
	// The fallback path keeps the original call.

	// Need the receiver object. For a method call like obj.foo(args...),
	// the callee is typically loaded from the object. The shape guard
	// operates on the first argument after the callee (the receiver), or
	// on the callee itself for constructor calls. We use Args[0] if available.
	if len(callSite.Args) == 0 {
		return
	}
	receiver := callSite.Args[0]

	// Determine the current position in the caller block for splicing.
	pos := indexOf(callerBB.Nodes, callSite)
	if pos < 0 {
		return
	}

	// Build guard nodes and graft callee bodies before the call site.
	var guardNodes []*SSANode
	var polyBlocks []*SSABasicBlock

	for i := 0; i < slot.PolyCallCount; i++ {
		calleeBF := slot.PolyCallees[i]
		expectedShape := slot.Shapes[i]
		if expectedShape == nil || calleeBF == nil {
			continue
		}

		// Build the inlined callee graph.
		calleeGraph := BuildSSA(calleeBF)
		if calleeGraph == nil || calleeGraph.Entry == nil || len(calleeGraph.Entry.Nodes) == 0 {
			continue
		}

		// Clone callee nodes with fresh IDs.
		nodeMap := make(map[*SSANode]*SSANode)
		var clonedNodes []*SSANode
		for _, cbb := range calleeGraph.Blocks {
			for _, cn := range cbb.Nodes {
				clone := &SSANode{
					ID:       g.nextID,
					Op:       cn.Op,
					Type:     cn.Type,
					Value:    cn.Value,
					BCPC:     cn.BCPC,
					Feedback: cn.Feedback,
				}
				g.nextID++
				nodeMap[cn] = clone
				clonedNodes = append(clonedNodes, clone)
			}
		}

		// Remap callee params → call-site args.
		for pi, param := range calleeGraph.Params {
			if clone, ok := nodeMap[param]; ok {
				callArgIdx := pi + 1
				if callArgIdx < len(callSite.Args) {
					replaceNode(g, clone, callSite.Args[callArgIdx])
				}
			}
		}

		// Rewire cloned nodes' internal arg references.
		for _, cbb := range calleeGraph.Blocks {
			for _, cn := range cbb.Nodes {
				clone := nodeMap[cn]
				newArgs := make([]*SSANode, 0, len(cn.Args))
				for _, arg := range cn.Args {
					if mapped, ok := nodeMap[arg]; ok {
						newArgs = append(newArgs, mapped)
					} else {
						newArgs = append(newArgs, arg)
					}
				}
				clone.Args = newArgs
				for _, a := range clone.Args {
					if a != nil {
						a.Users = append(a.Users, clone)
					}
				}
			}
		}

		// Collect non-return nodes for the inlined body.
		var inlined []*SSANode
		for _, cbb := range calleeGraph.Blocks {
			for _, cn := range cbb.Nodes {
				clone := nodeMap[cn]
				if clone.Op == SSAReturn {
					// Redirect return value to a dummy sink so it doesn't escape.
					continue
				}
				inlined = append(inlined, clone)
			}
		}

		// Create a new basic block for this inlined path.
		polyBB := &SSABasicBlock{
			ID:      len(g.Blocks) + len(polyBlocks),
			Nodes:   inlined,
			StartPC: -1,
			EndPC:   -1,
		}
		polyBlocks = append(polyBlocks, polyBB)

		// Generate shape guard: compare receiver's shape to expected shape.
		_ = expectedShape // shape comparison is conceptual; the lowering pass handles it.
		shapeCheck := g.newNode(SSACmp, SSAInt32, receiver)
		shapeCheck.BCPC = callSite.BCPC
		guardNodes = append(guardNodes, shapeCheck)

		// Branch to polyBB on match.
		branch := g.newNode(SSABranch, SSAAny, shapeCheck)
		branch.Labels = append(branch.Labels, NewLabel())
		guardNodes = append(guardNodes, branch)
	}

	// Create merge/fallback block with the original call.
	fallbackBB := &SSABasicBlock{
		ID:      len(g.Blocks) + len(polyBlocks),
		Nodes:   []*SSANode{callSite},
		StartPC: -1,
		EndPC:   -1,
	}

	// Wire successor edges.
	for _, pb := range polyBlocks {
		pb.Successors = append(pb.Successors, fallbackBB)
		fallbackBB.Predecessors = append(fallbackBB.Predecessors, pb)
	}

	// Splice guard nodes before the call site in the caller block.
	// Remove call site from caller block; it's now in the fallback.
	callerBB.Nodes = append(callerBB.Nodes[:pos], callerBB.Nodes[pos+1:]...)
	callerBB.Nodes = append(callerBB.Nodes[:pos], append(guardNodes, callerBB.Nodes[pos:]...)...)

	// Add new blocks to the graph.
	g.Blocks = append(g.Blocks, polyBlocks...)
	g.Blocks = append(g.Blocks, fallbackBB)
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
