package dom

import "testing"

func TestCloneNode_Shallow(t *testing.T) {
doc := NewDocument()
el := NewElement("div", doc)
el.SetAttribute("class", "foo")
child := NewElement("span", doc)
el.AppendChild(&child.Node)

clone := el.CloneNode(false)
if clone == nil {
t.Fatal("clone is nil")
}
cloneEl, ok := clone.Self().(*Element)
if !ok {
t.Fatal("clone is not Element")
}
if cloneEl.GetAttribute("class") != "foo" {
t.Errorf("expected class=foo, got %q", cloneEl.GetAttribute("class"))
}
if cloneEl.FirstChild() != nil {
t.Error("shallow clone should not have children")
}
}

func TestCloneNode_Deep(t *testing.T) {
doc := NewDocument()
el := NewElement("div", doc)
child := NewElement("span", doc)
el.AppendChild(&child.Node)

clone := el.CloneNode(true)
if clone == nil {
t.Fatal("clone is nil")
}
if clone.FirstChild() == nil {
t.Error("deep clone should have children")
}
}

func TestIsConnected(t *testing.T) {
doc := NewDocument()
html := NewElement("html", doc)
body := NewElement("body", doc)
div := NewElement("div", doc)

html.AppendChild(&body.Node)
body.AppendChild(&div.Node)

if body.IsConnected() {
t.Error("body should not be connected without document root")
}
}

func TestGetRootNode(t *testing.T) {
doc := NewDocument()
html := NewElement("html", doc)
body := NewElement("body", doc)
div := NewElement("div", doc)

html.AppendChild(&body.Node)
body.AppendChild(&div.Node)

root := div.GetRootNode()
if root == nil {
t.Fatal("root is nil")
}
if root.Self() != html.Self() {
t.Error("root should be html element")
}
}

func TestCompareDocumentPosition_Contains(t *testing.T) {
doc := NewDocument()
parent := NewElement("div", doc)
child := NewElement("span", doc)
parent.AppendChild(&child.Node)

pos := parent.CompareDocumentPosition(&child.Node)
if pos&DocumentPositionContainedBy == 0 {
t.Error("parent contains child")
}
}

func TestInsertAdjacentHTML_BeforeEnd(t *testing.T) {
doc := NewDocument()
el := NewElement("div", doc)
el.ownerDocument = doc

el.InsertAdjacentHTML("beforeend", "<span>hello</span>")

if el.FirstChild() == nil {
t.Error("should have child after insertAdjacentHTML")
}
}

func TestBeforeAfterReplaceWithRemove(t *testing.T) {
doc := NewDocument()
parent := NewElement("div", doc)
child := NewElement("span", doc)
parent.AppendChild(&child.Node)

// Before.
beforeEl := NewElement("em", doc)
if err := child.Before(&beforeEl.Node); err != nil {
t.Fatal(err)
}
if parent.FirstChild() != &beforeEl.Node {
t.Error("before should insert before child")
}

// After.
afterEl := NewElement("strong", doc)
if err := child.After(&afterEl.Node); err != nil {
t.Fatal(err)
}

// ReplaceWith.
replaceEl := NewElement("p", doc)
if err := child.ReplaceWith(&replaceEl.Node); err != nil {
t.Fatal(err)
}

// Remove.
if err := replaceEl.Remove(); err != nil {
t.Fatal(err)
}
}
