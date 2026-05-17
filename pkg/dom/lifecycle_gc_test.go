package dom

import (
	"sync"
	"testing"
)

// TestGCLifecycle_FullCycle verifies the full GC lifecycle:
// 1. Register nodes
// 2. Add document root
// 3. Run PreGC → CollectOrphans → PostGC
// 4. Verify roots survive, orphans are collected
func TestGCLifecycle_FullCycle(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	docEl := NewElement("html", doc)

	// Simulate parsed DOM tree.
	body := NewElement("body", doc)
	div1 := NewElement("div", doc)
	div2 := NewElement("div", doc)
	span1 := NewElement("span", doc)
	span2 := NewElement("span", doc)

	docEl.AppendChild(&body.Node)
	body.AppendChild(&div1.Node)
	body.AppendChild(&div2.Node)
	div1.AppendChild(&span1.Node)
	div2.AppendChild(&span2.Node)

	// Register all nodes.
	h.Register(&docEl.Node)
	h.Register(&body.Node)
	h.Register(&div1.Node)
	h.Register(&div2.Node)
	h.Register(&span1.Node)
	h.Register(&span2.Node)

	// Mark document element as root.
	h.AddRoot(&docEl.Node)

	if h.Count() != 6 {
		t.Fatalf("expected 6 nodes, got %d", h.Count())
	}

	// Run PreGC.
	preCalled := false
	h.OnBeforeGC(func() { preCalled = true })
	h.RunPreGC()
	if !preCalled {
		t.Error("PreGC callback not called")
	}

	// All nodes should be reachable via root.
	if collected := h.CollectOrphans(); collected != 0 {
		t.Errorf("expected 0 orphans, got %d", collected)
	}

	// Run PostGC.
	postCalled := false
	h.OnAfterGC(func() { postCalled = true })
	h.RunPostGC()
	if !postCalled {
		t.Error("PostGC callback not called")
	}

	if h.Count() != 6 {
		t.Errorf("all 6 nodes should survive: got %d", h.Count())
	}
}

// TestGCLifecycle_OrphanCollection verifies that nodes detached from the DOM
// tree are collected as orphans after a GC cycle.
func TestGCLifecycle_OrphanCollection(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	docEl := NewElement("html", doc)
	body := NewElement("body", doc)
	div := NewElement("div", doc)
	orphan := NewElement("span", doc) // detached

	docEl.AppendChild(&body.Node)
	body.AppendChild(&div.Node)

	h.Register(&docEl.Node)
	h.Register(&body.Node)
	h.Register(&div.Node)
	h.Register(&orphan.Node)
	h.AddRoot(&docEl.Node)

	if h.Count() != 4 {
		t.Fatalf("expected 4 nodes, got %d", h.Count())
	}

	// Run GC cycle.
	h.RunPreGC()
	collected := h.CollectOrphans()
	h.RunPostGC()

	if collected != 1 {
		t.Errorf("expected 1 orphan collected, got %d", collected)
	}
	if h.Count() != 3 {
		t.Errorf("expected 3 nodes remaining, got %d", h.Count())
	}

	// The orphan should no longer be reachable.
	if h.IsReachable(&orphan.Node) {
		t.Error("orphan should NOT be reachable after collection")
	}
	// Root and connected nodes should still be reachable.
	if !h.IsReachable(&docEl.Node) {
		t.Error("root should still be reachable")
	}
	if !h.IsReachable(&div.Node) {
		t.Error("connected child should still be reachable")
	}
}

// TestGCLifecycle_NavigationCleanup simulates full page navigation:
// old document nodes are collected, new document is registered.
func TestGCLifecycle_NavigationCleanup(t *testing.T) {
	h := NewDOMHeap()

	// ── Old page ──────────────────────────────────────────────────
	oldDoc := NewDocument()
	oldRoot := NewElement("html", oldDoc)
	oldBody := NewElement("body", oldDoc)
	oldDiv := NewElement("div", oldDoc)
	oldRoot.AppendChild(&oldBody.Node)
	oldBody.AppendChild(&oldDiv.Node)

	h.Register(&oldRoot.Node)
	h.Register(&oldBody.Node)
	h.Register(&oldDiv.Node)
	h.AddRoot(&oldRoot.Node)

	// Navigation: create new page.
	// 1. Remove old root from GC roots.
	h.RemoveRoot(&oldRoot.Node)

	// 2. Run GC — old nodes should become orphans.
	h.RunPreGC()
	collected := h.CollectOrphans()
	h.RunPostGC()

	if collected != 3 {
		t.Errorf("expected 3 old-page orphans collected, got %d", collected)
	}

	// ── New page ──────────────────────────────────────────────────
	newDoc := NewDocument()
	newRoot := NewElement("html", newDoc)
	newBody := NewElement("body", newDoc)
	newDiv := NewElement("div", newDoc)
	newRoot.AppendChild(&newBody.Node)
	newBody.AppendChild(&newDiv.Node)

	h.Register(&newRoot.Node)
	h.Register(&newBody.Node)
	h.Register(&newDiv.Node)
	h.AddRoot(&newRoot.Node)

	if h.Count() != 3 {
		t.Errorf("expected 3 new nodes, got %d", h.Count())
	}

	// New nodes should all be reachable.
	if !h.IsReachable(&newRoot.Node) {
		t.Error("new root should be reachable")
	}
	if !h.IsReachable(&newDiv.Node) {
		t.Error("new child should be reachable")
	}
}

// TestGCLifecycle_ConcurrentRegistration verifies that node registration
// and GC collection are safe for concurrent use.
func TestGCLifecycle_ConcurrentRegistration(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()

	// Register many nodes concurrently.
	var wg sync.WaitGroup
	const numGoros = 10
	const nodesPerGoro = 100

	for g := 0; g < numGoros; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < nodesPerGoro; i++ {
				el := NewElement("div", doc)
				h.Register(&el.Node)
			}
		}()
	}
	wg.Wait()

	if h.Count() != numGoros*nodesPerGoro {
		t.Errorf("expected %d nodes, got %d", numGoros*nodesPerGoro, h.Count())
	}

	// All are orphans — collect them concurrently.
	var collectedTotal int
	var mu sync.Mutex
	for g := 0; g < numGoros; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.RunPreGC()
			c := h.CollectOrphans()
			h.RunPostGC()
			mu.Lock()
			collectedTotal += c
			mu.Unlock()
		}()
	}
	wg.Wait()

	// After all goroutines, all nodes should be collected.
	if h.Count() != 0 {
		t.Errorf("expected 0 nodes after concurrent collection, got %d", h.Count())
	}
}

// TestGCLifecycle_PrePostGCCallbacksOrder verifies callbacks execute in order.
func TestGCLifecycle_PrePostGCCallbacksOrder(t *testing.T) {
	h := NewDOMHeap()

	var order []string
	h.OnBeforeGC(func() { order = append(order, "pre1") })
	h.OnBeforeGC(func() { order = append(order, "pre2") })
	h.OnAfterGC(func() { order = append(order, "post1") })
	h.OnAfterGC(func() { order = append(order, "post2") })

	h.RunPreGC()
	h.RunPostGC()

	if len(order) != 4 {
		t.Fatalf("expected 4 callbacks, got %d: %v", len(order), order)
	}
	if order[0] != "pre1" || order[1] != "pre2" {
		t.Errorf("pre-GC order wrong: %v", order[:2])
	}
	if order[2] != "post1" || order[3] != "post2" {
		t.Errorf("post-GC order wrong: %v", order[2:])
	}
}

// TestGCLifecycle_JSReachableSurvival verifies that nodes marked as
// reachable from JavaScript survive GC even when not connected to the DOM tree.
func TestGCLifecycle_JSReachableSurvival(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	docEl := NewElement("html", doc)
	body := NewElement("body", doc)
	jsEl := NewElement("div", doc) // created by JS, not attached to DOM

	docEl.AppendChild(&body.Node)

	h.Register(&docEl.Node)
	h.Register(&body.Node)
	h.Register(&jsEl.Node)
	h.AddRoot(&docEl.Node)

	// Simulate JS holding a reference.
	h.MarkJSReachable(&jsEl.Node)

	h.RunPreGC()
	collected := h.CollectOrphans()
	h.RunPostGC()

	if collected != 0 {
		t.Errorf("expected 0 orphans (JS-reachable element should survive), got %d", collected)
	}
	if !h.IsReachable(&jsEl.Node) {
		t.Error("JS-reachable element should still be reachable")
	}

	// Clear JS references — now it should become orphan.
	h.ClearJSReachable()
	h.RunPreGC()
	collected = h.CollectOrphans()
	h.RunPostGC()

	if collected != 1 {
		t.Errorf("expected 1 orphan after clearing JS refs, got %d", collected)
	}
	if h.IsReachable(&jsEl.Node) {
		t.Error("element should NOT be reachable after clearing JS refs")
	}
}

// TestGCLifecycle_DocumentCloseCleanup simulates closing a document.
func TestGCLifecycle_DocumentCloseCleanup(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	root := NewElement("html", doc)
	body := NewElement("body", doc)
	div1 := NewElement("div", doc)
	div2 := NewElement("div", doc)
	span := NewElement("span", doc)

	root.AppendChild(&body.Node)
	body.AppendChild(&div1.Node)
	body.AppendChild(&div2.Node)
	div1.AppendChild(&span.Node)

	allNodes := []*Node{&root.Node, &body.Node, &div1.Node, &div2.Node, &span.Node}
	for _, n := range allNodes {
		h.Register(n)
	}
	h.AddRoot(&root.Node)

	if h.Count() != 5 {
		t.Fatalf("expected 5 nodes, got %d", h.Count())
	}

	// Document close: remove root.
	h.RemoveRoot(&root.Node)

	// Full GC cycle.
	h.RunPreGC()
	collected := h.CollectOrphans()
	h.RunPostGC()

	if collected != 5 {
		t.Errorf("expected 5 nodes collected on doc close, got %d", collected)
	}
	if h.Count() != 0 {
		t.Errorf("expected 0 nodes after doc close, got %d", h.Count())
	}
}

// TestGCLifecycle_ClearJSReachableDoesNotAffectRoots verifies that
// ClearJSReachable only clears JS references, not roots.
func TestGCLifecycle_ClearJSReachableDoesNotAffectRoots(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	root := NewElement("html", doc)
	jsEl := NewElement("div", doc)

	h.Register(&root.Node)
	h.Register(&jsEl.Node)
	h.AddRoot(&root.Node)
	h.MarkJSReachable(&jsEl.Node)

	// Clear JS references.
	h.ClearJSReachable()

	// Root should still be reachable.
	if !h.IsReachable(&root.Node) {
		t.Error("root should still be reachable after ClearJSReachable")
	}
	// jsEl should now only be reachable via JS reference — which was cleared.
	if h.IsReachable(&jsEl.Node) {
		t.Error("JS-only element should NOT be reachable after ClearJSReachable")
	}
}
