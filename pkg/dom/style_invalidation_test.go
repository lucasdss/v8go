package dom

import (
	"testing"
)

// TestStyleInvalidation_AttributeMutation tests that setting an attribute
// marks the element as style-dirty.
func TestStyleInvalidation_AttributeMutation(t *testing.T) {
	doc := NewDocument()
	div := NewElement("div", doc)

	if div.IsStyleDirty() {
		t.Error("new element should not be dirty")
	}

	// Setting an attribute should mark as dirty.
	div.SetAttribute("class", "foo")
	if !div.IsStyleDirty() {
		t.Error("element should be dirty after SetAttribute")
	}

	div.ClearStyleDirty()
	if div.IsStyleDirty() {
		t.Error("element should be clean after ClearStyleDirty")
	}

	// Removing an attribute should mark as dirty.
	div.RemoveAttribute("class")
	if !div.IsStyleDirty() {
		t.Error("element should be dirty after RemoveAttribute")
	}
}

// TestStyleInvalidation_DirtyPropagation tests that the dirty flag
// propagates upward to ancestors.
func TestStyleInvalidation_DirtyPropagation(t *testing.T) {
	doc := NewDocument()
	root := NewElement("html", doc)
	body := NewElement("body", doc)
	div := NewElement("div", doc)
	span := NewElement("span", doc)

	root.AppendChild(&body.Node)
	body.AppendChild(&div.Node)
	div.AppendChild(&span.Node)

	// Clear all dirty flags from AppendChild calls.
	root.ClearStyleDirty()
	body.ClearStyleDirty()
	div.ClearStyleDirty()
	span.ClearStyleDirty()

	// Mutate the leaf node.
	span.SetAttribute("style", "color: red")

	// All ancestors should be dirty.
	if !span.IsStyleDirty() {
		t.Error("span should be dirty")
	}
	if !div.IsStyleDirty() {
		t.Error("div (parent) should be dirty")
	}
	if !body.IsStyleDirty() {
		t.Error("body (grandparent) should be dirty")
	}
	if !root.IsStyleDirty() {
		t.Error("root (great-grandparent) should be dirty")
	}
}

// TestStyleInvalidation_ChildInsertion tests that inserting a child
// marks the parent as style-dirty.
func TestStyleInvalidation_ChildInsertion(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)

	parent.ClearStyleDirty()

	parent.AppendChild(&child.Node)

	if !parent.IsStyleDirty() {
		t.Error("parent should be dirty after AppendChild")
	}
}

// TestStyleInvalidation_ChildRemoval tests that removing a child
// marks the parent as style-dirty.
func TestStyleInvalidation_ChildRemoval(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)

	parent.AppendChild(&child.Node)
	parent.ClearStyleDirty()

	parent.RemoveChild(&child.Node)

	if !parent.IsStyleDirty() {
		t.Error("parent should be dirty after RemoveChild")
	}
}

// TestStyleInvalidation_InsertBefore tests that InsertBefore marks as dirty.
func TestStyleInvalidation_InsertBefore(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	first := NewElement("span", doc)
	second := NewElement("em", doc)

	parent.AppendChild(&first.Node)
	parent.ClearStyleDirty()

	parent.InsertBefore(&second.Node, &first.Node)

	if !parent.IsStyleDirty() {
		t.Error("parent should be dirty after InsertBefore")
	}
}

// TestStyleInvalidation_SubtreeInvalidation tests the bulk invalidation method.
func TestStyleInvalidation_SubtreeInvalidation(t *testing.T) {
	doc := NewDocument()
	root := NewElement("div", doc)
	child1 := NewElement("p", doc)
	child2 := NewElement("p", doc)
	grandchild := NewElement("span", doc)

	root.AppendChild(&child1.Node)
	root.AppendChild(&child2.Node)
	child1.AppendChild(&grandchild.Node)

	// Clear all.
	root.ClearStyleDirty()
	child1.ClearStyleDirty()
	child2.ClearStyleDirty()
	grandchild.ClearStyleDirty()

	root.InvalidateSubtreeStyle()

	if !root.IsStyleDirty() {
		t.Error("root should be dirty after InvalidateSubtreeStyle")
	}
	if !child1.IsStyleDirty() {
		t.Error("child1 should be dirty after InvalidateSubtreeStyle")
	}
	if !child2.IsStyleDirty() {
		t.Error("child2 should be dirty after InvalidateSubtreeStyle")
	}
	if !grandchild.IsStyleDirty() {
		t.Error("grandchild should be dirty after InvalidateSubtreeStyle")
	}
}

// TestStyleInvalidation_TextNodeMutation tests that text nodes
// can also be marked dirty.
func TestStyleInvalidation_TextNodeMutation(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	text := NewText("hello", doc)

	parent.AppendChild(&text.Node)
	parent.ClearStyleDirty()
	text.ClearStyleDirty()

	text.InvalidateStyle()
	if !parent.IsStyleDirty() {
		t.Error("parent should be dirty when text node is invalidated")
	}
}

// TestStyleInvalidation_ClearAfterComputation simulates the full cycle:
// mutate → dirty → recompute → clean.
func TestStyleInvalidation_ClearAfterComputation(t *testing.T) {
	doc := NewDocument()
	div := NewElement("div", doc)

	// Simulate mutation.
	div.SetAttribute("class", "active")
	if !div.IsStyleDirty() {
		t.Error("should be dirty after mutation")
	}

	// Simulate style computation.
	div.ClearStyleDirty()
	if div.IsStyleDirty() {
		t.Error("should be clean after computation")
	}
}

// TestStyleInvalidation_NewElementIsClean tests that newly created elements
// start with a clean slate.
func TestStyleInvalidation_NewElementIsClean(t *testing.T) {
	doc := NewDocument()
	el := NewElement("div", doc)

	if el.IsStyleDirty() {
		t.Error("new element should not be dirty")
	}
}
