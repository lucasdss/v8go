package jit

import "github.com/lucasdss/v8go/pkg/js"

// specializeTypes performs a type-specialization pass over the SSA graph.
// It uses runtime feedback (IC slot observed tags) to refine node types.
// Arithmetic ops with consistent numeric feedback are narrowed to SSAFloat64.
func specializeTypes(g *SSAGraph) {
	for _, bb := range g.Blocks {
		for _, node := range bb.Nodes {
			if node.Feedback == nil {
				continue
			}
			switch node.Op {
			case SSAAdd, SSASub, SSAMul, SSADiv:
				if node.Feedback.ObservedTag == js.TagNumber {
					node.Type = SSAFloat64
				}
			}
		}
	}
}
