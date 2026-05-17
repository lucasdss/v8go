package dom

import (
	"testing"
)

func TestDOMHeap_RegisterAndCount(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	el := NewElement("div", doc)

	h.Register(&el.Node)
	if h.Count() != 1 {
		t.Errorf("expected 1, got %d", h.Count())
	}
}

func TestDOMHeap_Unregister(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	el := NewElement("div", doc)

	h.Register(&el.Node)
	h.Unregister(&el.Node)
	if h.Count() != 0 {
		t.Errorf("expected 0, got %d", h.Count())
	}
}

func TestDOMHeap_AddRoot(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	el := NewElement("div", doc)

	h.AddRoot(&el.Node)
	if h.RootCount() != 1 {
		t.Errorf("expected 1 root, got %d", h.RootCount())
	}
	if !h.IsReachable(&el.Node) {
		t.Error("root node should be reachable")
	}
}

func TestDOMHeap_IsReachable_ParentChain(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)
	parent.AppendChild(&child.Node)

	h.Register(&parent.Node)
	h.Register(&child.Node)
	h.AddRoot(&parent.Node)

	if !h.IsReachable(&child.Node) {
		t.Error("child of root should be reachable")
	}
}

func TestDOMHeap_IsReachable_JSTrace(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	el := NewElement("div", doc)

	h.Register(&el.Node)
	h.MarkJSReachable(&el.Node)

	if !h.IsReachable(&el.Node) {
		t.Error("JS-reachable node should be reachable")
	}

	h.ClearJSReachable()
	if h.IsReachable(&el.Node) {
		t.Error("after clearing JS reachable, orphan should not be reachable")
	}
}

func TestDOMHeap_GC_Callbacks(t *testing.T) {
	h := NewDOMHeap()
	preCalled := false
	postCalled := false

	h.OnBeforeGC(func() { preCalled = true })
	h.OnAfterGC(func() { postCalled = true })

	h.RunPreGC()
	h.RunPostGC()

	if !preCalled {
		t.Error("pre-GC callback not invoked")
	}
	if !postCalled {
		t.Error("post-GC callback not invoked")
	}
}

func TestDOMHeap_CollectOrphans(t *testing.T) {
	h := NewDOMHeap()
	doc := NewDocument()
	root := NewElement("div", doc)
	orphan := NewElement("span", doc)

	h.Register(&root.Node)
	h.Register(&orphan.Node)
	h.AddRoot(&root.Node)

	collected := h.CollectOrphans()
	if collected != 1 {
		t.Errorf("expected 1 orphan collected, got %d", collected)
	}
	if h.Count() != 1 {
		t.Errorf("expected 1 node remaining, got %d", h.Count())
	}
	if !h.IsReachable(&root.Node) {
		t.Error("root should still be reachable")
	}
}

func TestTrace(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)
	parent.AppendChild(&child.Node)

	count := 0
	Trace(&parent.Node, func(n *Node) {
		count++
	})

	if count < 2 {
		t.Errorf("Trace should find at least 2 nodes, got %d", count)
	}
}
