package bindings

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/dom"
)

func TestBindingRegistry_RegisterAndLookup(t *testing.T) {
	r := NewBindingRegistry()
	doc := dom.NewDocument()
	el := dom.NewElement("div", doc)

	w := r.RegisterNode(&el.Node)
	if w == nil {
		t.Fatal("RegisterNode returned nil")
	}
	if w.Node != &el.Node {
		t.Error("wrapper node mismatch")
	}

	lookupByNode := r.LookupByNode(&el.Node)
	if lookupByNode == nil {
		t.Fatal("LookupByNode returned nil")
	}
	if lookupByNode.JSHandle != w.JSHandle {
		t.Error("handle mismatch via LookupByNode")
	}

	lookupByHandle := r.LookupByHandle(w.JSHandle)
	if lookupByHandle == nil {
		t.Fatal("LookupByHandle returned nil")
	}
}

func TestBindingRegistry_Unregister(t *testing.T) {
	r := NewBindingRegistry()
	doc := dom.NewDocument()
	el := dom.NewElement("p", doc)

	r.RegisterNode(&el.Node)
	if r.Count() != 1 {
		t.Fatalf("expected 1, got %d", r.Count())
	}

	r.UnregisterNode(&el.Node)
	if r.Count() != 0 {
		t.Errorf("expected 0 after Unregister, got %d", r.Count())
	}
}

func TestBindingRegistry_DuplicateRegister(t *testing.T) {
	r := NewBindingRegistry()
	doc := dom.NewDocument()
	el := dom.NewElement("span", doc)

	w1 := r.RegisterNode(&el.Node)
	w2 := r.RegisterNode(&el.Node)
	if w1 != w2 {
		t.Error("duplicate RegisterNode should return the same wrapper")
	}
	if r.Count() != 1 {
		t.Errorf("expected 1, got %d", r.Count())
	}
}

func TestBindDocument(t *testing.T) {
	r := NewBindingRegistry()
	doc := dom.NewDocument()
	html := dom.NewElement("html", doc)
	head := dom.NewElement("head", doc)
	body := dom.NewElement("body", doc)
	div := dom.NewElement("div", doc)
	p := dom.NewElement("p", doc)
	txt := dom.NewText("hello", doc)

	p.AppendChild(&txt.Node)
	div.AppendChild(&p.Node)
	body.AppendChild(&div.Node)
	html.AppendChild(&head.Node)
	html.AppendChild(&body.Node)
	doc.AppendChild(&html.Node)
	doc.DocumentElement = html

	BindDocument(doc, r)

	// Should register 7 nodes: html, head, body, div, p, text, document
	if r.Count() < 5 {
		t.Errorf("expected at least 5 registered nodes, got %d", r.Count())
	}
}

func TestElementBinding(t *testing.T) {
	r := NewBindingRegistry()
	// Register a mock binding for <div>.
	binding := &ElementBinding{
		TagName:        "div",
		GetAttribute:   func(handle JSObjectRef, name string) string { return "mock" },
		SetAttribute:   func(handle JSObjectRef, name, value string) {},
		GetTextContent: func(handle JSObjectRef) string { return "mock text" },
	}
	r.RegisterElementBinding("div", binding)

	retrieved := r.GetElementBinding("div")
	if retrieved == nil {
		t.Fatal("GetElementBinding returned nil")
	}
	if retrieved.TagName != "div" {
		t.Errorf("expected div, got %s", retrieved.TagName)
	}

	// Non-existent binding.
	nilBinding := r.GetElementBinding("span")
	if nilBinding != nil {
		t.Error("expected nil for unregistered element")
	}
}

func TestJSObjectRef_String(t *testing.T) {
	ref := JSObjectRef(42)
	s := ref.String()
	if s != "JSObjectRef(42)" {
		t.Errorf("expected JSObjectRef(42), got %s", s)
	}
}
