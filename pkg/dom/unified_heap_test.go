package dom

import "testing"

// TestUnifiedHeap_JSReachableSurvival verifies that elements marked as
// JS-reachable survive GC even when detached from the DOM tree.
func TestUnifiedHeap_JSReachableSurvival(t *testing.T) {
h := NewDOMHeap()
doc := NewDocument()
docEl := NewElement("html", doc)
body := NewElement("body", doc)
jsCreated := NewElement("div", doc) // Created by JS, detached

docEl.AppendChild(&body.Node)

h.Register(&docEl.Node)
h.Register(&body.Node)
h.Register(&jsCreated.Node)
h.AddRoot(&docEl.Node)

// Simulate JS engine marking the element as reachable.
h.MarkJSReachable(&jsCreated.Node)

h.RunPreGC()
collected := h.CollectOrphans()
h.RunPostGC()

if collected != 0 {
t.Errorf("JS-reachable element should not be collected, got %d orphans", collected)
}

if !h.IsReachable(&jsCreated.Node) {
t.Error("JS-reachable element should be reachable")
}
}

// TestUnifiedHeap_ClearJSRefsCollectsOrphans verifies that after clearing
// JS references, detached elements become orphans.
func TestUnifiedHeap_ClearJSRefsCollectsOrphans(t *testing.T) {
h := NewDOMHeap()
doc := NewDocument()
docEl := NewElement("html", doc)
jsCreated := NewElement("div", doc)

h.Register(&docEl.Node)
h.Register(&jsCreated.Node)
h.AddRoot(&docEl.Node)
h.MarkJSReachable(&jsCreated.Node)

// After clearing JS references, it should be orphaned.
h.ClearJSReachable()

h.RunPreGC()
collected := h.CollectOrphans()
h.RunPostGC()

if collected != 1 {
t.Errorf("expected 1 orphan after clearing JS refs, got %d", collected)
}
}

// TestUnifiedHeap_ParentChainReachable verifies that children of
// JS-reachable nodes are also reachable via parent chain tracing.
func TestUnifiedHeap_ParentChainReachable(t *testing.T) {
h := NewDOMHeap()
doc := NewDocument()

parent := NewElement("div", doc)
child := NewElement("span", doc)
grandchild := NewElement("em", doc)

parent.AppendChild(&child.Node)
child.AppendChild(&grandchild.Node)

h.Register(&parent.Node)
h.Register(&child.Node)
h.Register(&grandchild.Node)

// Parent is JS-reachable.
h.MarkJSReachable(&parent.Node)

// Child and grandchild should be reachable via parent.
if !h.IsReachable(&child.Node) {
t.Error("child of JS-reachable parent should be reachable")
}
if !h.IsReachable(&grandchild.Node) {
t.Error("grandchild of JS-reachable parent should be reachable")
}
}

// TestUnifiedHeap_MarkJSReachableIdempotent verifies multiple calls don't break.
func TestUnifiedHeap_MarkJSReachableIdempotent(t *testing.T) {
h := NewDOMHeap()
doc := NewDocument()
el := NewElement("div", doc)

h.Register(&el.Node)
h.MarkJSReachable(&el.Node)
h.MarkJSReachable(&el.Node) // second call
h.MarkJSReachable(&el.Node) // third call

if !h.IsReachable(&el.Node) {
t.Error("should still be reachable after multiple marks")
}

if h.Count() != 1 {
t.Errorf("expected 1 node, got %d", h.Count())
}
}
