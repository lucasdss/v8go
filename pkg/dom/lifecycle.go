package dom

import (
	"sync"
)

// ----------------------------- DOMHeap (Oilpan equivalent) --------------------
//
// DOMHeap is the central registry of all live DOM nodes with weak-reference
// semantics. It coordinates with the JavaScript engine's GC so that DOM nodes
// reachable from JS are not collected, and orphan nodes (unreachable from both
// the DOM tree and JS) can be cleaned up.
//
// See Blink platform/heap/ (Oilpan GC) for the corresponding C++ design.

// DOMHeap tracks all live DOM nodes and provides GC coordination.
type DOMHeap struct {
	mu sync.RWMutex

	// nodes is the set of all live nodes registered with the heap.
	nodes map[*Node]struct{}
	// roots contains nodes that are explicitly pinned (document roots, etc.).
	roots map[*Node]struct{}
	// gcCallbacks are called before and after JS GC runs.
	preGC  []func()
	postGC []func()
	// jsReachable is populated before GC by tracing JS references.
	jsReachable map[*Node]struct{}
}

// NewDOMHeap creates an empty DOM heap.
func NewDOMHeap() *DOMHeap {
	return &DOMHeap{
		nodes:       make(map[*Node]struct{}),
		roots:       make(map[*Node]struct{}),
		jsReachable: make(map[*Node]struct{}),
	}
}

// Register adds a node to the heap. Called when a node is created.
func (h *DOMHeap) Register(node *Node) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes[node] = struct{}{}
}

// Unregister removes a node from the heap. Called when a node is removed
// from the DOM tree and is no longer reachable.
func (h *DOMHeap) Unregister(node *Node) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.nodes, node)
	delete(h.roots, node)
	delete(h.jsReachable, node)
}

// AddRoot marks a node as a GC root (e.g., document.documentElement).
// Roots are always considered reachable.
func (h *DOMHeap) AddRoot(node *Node) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.roots[node] = struct{}{}
	h.nodes[node] = struct{}{}
}

// RemoveRoot removes a node from the root set.
func (h *DOMHeap) RemoveRoot(node *Node) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.roots, node)
}

// MarkJSReachable records that a node is reachable from JavaScript.
// Called during the tracing phase when the JS engine reports which DOM
// wrappers are still referenced.
func (h *DOMHeap) MarkJSReachable(node *Node) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jsReachable[node] = struct{}{}
}

// ClearJSReachable resets the JS-reachable set. Called before tracing.
func (h *DOMHeap) ClearJSReachable() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jsReachable = make(map[*Node]struct{})
}

// IsReachable reports whether a node is reachable from either:
//  1. The GC root set (document roots), or
//  2. A JavaScript reference, or
//  3. A parent node that is itself reachable.
//
// The function includes a cycle guard (max 10000 iterations) to prevent
// infinite loops in case of DOM tree corruption.
func (h *DOMHeap) IsReachable(node *Node) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Roots are always reachable.
	if _, ok := h.roots[node]; ok {
		return true
	}

	// JS reachable.
	if _, ok := h.jsReachable[node]; ok {
		return true
	}

	// Walk up the DOM tree with a cycle guard.
	visited := make(map[*Node]bool)
	const maxDepth = 10000
	for i := 0; i < maxDepth && node != nil; i, node = i+1, node.ParentNode() {
		if visited[node] {
			break // cycle detected
		}
		visited[node] = true
		if _, ok := h.roots[node]; ok {
			return true
		}
		if _, ok := h.jsReachable[node]; ok {
			return true
		}
	}

	return false
}

// Count returns the number of registered nodes.
func (h *DOMHeap) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes)
}

// RootCount returns the number of root nodes.
func (h *DOMHeap) RootCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.roots)
}

// OnBeforeGC registers a callback invoked before the JS engine runs GC.
// DOM wrappers should trace their references here.
func (h *DOMHeap) OnBeforeGC(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.preGC = append(h.preGC, fn)
}

// OnAfterGC registers a callback invoked after the JS engine runs GC.
// Orphan nodes can be cleaned up here.
func (h *DOMHeap) OnAfterGC(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.postGC = append(h.postGC, fn)
}

// RunPreGC invokes all pre-GC callbacks. Called by the JS engine.
func (h *DOMHeap) RunPreGC() {
	h.mu.RLock()
	callbacks := make([]func(), len(h.preGC))
	copy(callbacks, h.preGC)
	h.mu.RUnlock()

	for _, fn := range callbacks {
		fn()
	}
}

// RunPostGC invokes all post-GC callbacks. Called by the JS engine.
func (h *DOMHeap) RunPostGC() {
	h.mu.RLock()
	callbacks := make([]func(), len(h.postGC))
	copy(callbacks, h.postGC)
	h.mu.RUnlock()

	for _, fn := range callbacks {
		fn()
	}
}

// CollectOrphans removes nodes that are not reachable from roots or JS.
// Returns the count of collected nodes.
func (h *DOMHeap) CollectOrphans() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	var orphans []*Node
	for node := range h.nodes {
		if !h.isReachableLocked(node) {
			orphans = append(orphans, node)
		}
	}

	for _, node := range orphans {
		delete(h.nodes, node)
	}
	return len(orphans)
}

// isReachableLocked is the internal version without locking.
func (h *DOMHeap) isReachableLocked(node *Node) bool {
	if _, ok := h.roots[node]; ok {
		return true
	}
	if _, ok := h.jsReachable[node]; ok {
		return true
	}
	for p := node.ParentNode(); p != nil; p = p.ParentNode() {
		if _, ok := h.roots[p]; ok {
			return true
		}
		if _, ok := h.jsReachable[p]; ok {
			return true
		}
	}
	return false
}

// ----------------------------- Trace support ---------------------------------

// Trace walks the DOM subtree rooted at node and calls fn for each node found.
// This is the equivalent of Blink's Trace() member function, used by the GC
// to discover the object graph.
func Trace(node *Node, fn func(*Node)) {
	if node == nil {
		return
	}
	fn(node)
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		Trace(child, fn)
	}
}

// TraceAll walks node and all its descendants, plus siblings and ancestors.
func TraceAll(node *Node, fn func(*Node)) {
	if node == nil {
		return
	}
	// Go up to root.
	var root *Node
	for n := node; n != nil; n = n.ParentNode() {
		root = n
	}
	// Trace entire tree from root.
	Trace(root, fn)
}
