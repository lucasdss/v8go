//go:build !amd64

package jit

// loadKey is a composite key for load-value caching within a basic block.
// Two loads are equivalent when they load from the same object and the same
// property (identified by the ICSlot name stored in Feedback).
type loadKey struct {
	obj  *SSANode
	prop string
}

// eliminateRedundantLoads performs single-basic-block load elimination.
// Within each basic block, redundant property loads from the same object
// with the same property key are replaced with the cached result of the
// first load.
//
// The cache is invalidated by:
//   - Stores to the same (object, property) pair
//   - Any SSACall, SSADeopt, or SSAThrow (which may have arbitrary side effects)
//
// Returns the number of loads eliminated.
func eliminateRedundantLoads(g *SSAGraph) int {
	eliminated := 0

	for _, bb := range g.Blocks {
		cache := make(map[loadKey]*SSANode)

		for i, node := range bb.Nodes {
			switch node.Op {
			case SSALoad, SSAGetProperty, SSALoadElement:
				if len(node.Args) == 0 {
					continue
				}
				obj := node.Args[0]
				prop := loadPropertyKey(node)

				key := loadKey{obj: obj, prop: prop}
				if cached, ok := cache[key]; ok {
					// Replace this redundant load with the cached value.
					replaceNode(g, node, cached)
					// Remove the node from the block.
					bb.Nodes = append(bb.Nodes[:i], bb.Nodes[i+1:]...)
					eliminated++
				} else {
					cache[key] = node
				}

			case SSAStore, SSASetProperty, SSAStoreElement:
				if len(node.Args) < 2 {
					// Invalidate entire cache on un-analyzable store.
					clearCache(cache)
					continue
				}
				obj := node.Args[0]
				prop := loadPropertyKey(node)
				key := loadKey{obj: obj, prop: prop}
				// Update cache: the stored value replaces any prior load.
				if len(node.Args) >= 3 {
					cache[key] = node.Args[2]
				} else {
					delete(cache, key)
				}

			case SSACall, SSADeopt, SSAThrow:
				// Conservatively clear cache: calls may mutate any object.
				clearCache(cache)
			}
		}
	}

	return eliminated
}

// loadPropertyKey extracts the property name from a load/store node's
// Feedback slot. If no feedback is available, returns an empty string
// which acts as a distinct key (no cross-object aliasing).
func loadPropertyKey(node *SSANode) string {
	if node.Feedback != nil && node.Feedback.Name != "" {
		return node.Feedback.Name
	}
	return ""
}

// clearCache removes all entries from the load cache.
func clearCache(cache map[loadKey]*SSANode) {
	for k := range cache {
		delete(cache, k)
	}
}
