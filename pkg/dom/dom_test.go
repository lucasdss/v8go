package dom_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/dom"
)

// ----------------------------- Document tests --------------------------------

func TestNewDocument(t *testing.T) {
	d := dom.NewDocument()
	if d.ContentType != "text/html" {
		t.Errorf("ContentType: got %q, want text/html", d.ContentType)
	}
	if d.CharacterSet != "UTF-8" {
		t.Errorf("CharacterSet: got %q, want UTF-8", d.CharacterSet)
	}
	if d.NodeType() != dom.DocumentNode {
		t.Errorf("NodeType: got %v, want DocumentNode", d.NodeType())
	}
}

func TestDocument_CreateElement(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("DIV")
	if el.LocalName != "div" {
		t.Errorf("LocalName: got %q, want %q", el.LocalName, "div")
	}
	if el.NodeType() != dom.ElementNode {
		t.Errorf("NodeType: got %v, want ElementNode", el.NodeType())
	}
}

func TestDocument_CreateTextNode(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("hello")
	if txt.Data != "hello" {
		t.Errorf("Data: got %q, want %q", txt.Data, "hello")
	}
	if txt.NodeType() != dom.TextNode {
		t.Errorf("NodeType: got %v, want TextNode", txt.NodeType())
	}
}

func TestDocument_CreateComment(t *testing.T) {
	d := dom.NewDocument()
	c := d.CreateComment("a comment")
	if c.Data != "a comment" {
		t.Errorf("Data: got %q, want %q", c.Data, "a comment")
	}
	if c.NodeType() != dom.CommentNode {
		t.Errorf("NodeType: got %v, want CommentNode", c.NodeType())
	}
}

func TestDocument_CreateDocumentType(t *testing.T) {
	d := dom.NewDocument()
	dt := d.CreateDocumentType("html", "", "")
	if dt.Name != "html" {
		t.Errorf("Name: got %q, want %q", dt.Name, "html")
	}
	if dt.NodeType() != dom.DocumentTypeNode {
		t.Errorf("NodeType: got %v, want DocumentTypeNode", dt.NodeType())
	}
}

// ----------------------------- Element tests ---------------------------------

func TestElement_SetGetRemoveAttribute(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("a")

	el.SetAttribute("href", "https://example.com")
	if got := el.GetAttribute("href"); got != "https://example.com" {
		t.Errorf("GetAttribute: got %q, want %q", got, "https://example.com")
	}
	if !el.HasAttribute("href") {
		t.Error("HasAttribute(href) should be true")
	}
	el.RemoveAttribute("href")
	if el.HasAttribute("href") {
		t.Error("HasAttribute(href) should be false after removal")
	}
	if got := el.GetAttribute("href"); got != "" {
		t.Errorf("GetAttribute after removal: got %q, want empty", got)
	}
}

func TestElement_AttributeCaseInsensitive(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("input")
	el.SetAttribute("TYPE", "text")
	if got := el.GetAttribute("type"); got != "text" {
		t.Errorf("GetAttribute case-insensitive: got %q, want %q", got, "text")
	}
}

func TestElement_ID_ClassName(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("div")
	el.SetAttribute("id", "main")
	el.SetAttribute("class", "foo bar baz")

	if el.ID() != "main" {
		t.Errorf("ID: got %q, want main", el.ID())
	}
	cls := el.ClassList()
	if len(cls) != 3 || cls[0] != "foo" || cls[1] != "bar" || cls[2] != "baz" {
		t.Errorf("ClassList: got %v", cls)
	}
}

func TestElement_TagName(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("div")
	if el.TagName() != "DIV" {
		t.Errorf("TagName: got %q, want DIV", el.TagName())
	}
}

func TestElement_Attributes_Copy(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("span")
	el.SetAttribute("a", "1")
	el.SetAttribute("b", "2")
	attrs := el.Attributes()
	if len(attrs) != 2 {
		t.Errorf("Attributes length: got %d, want 2", len(attrs))
	}
	// Modifying the copy should not affect the element.
	attrs[0].Value = "mutated"
	if el.GetAttribute("a") != "1" {
		t.Error("Attributes copy should be independent of element")
	}
}

func TestElement_GetElementsByTagName(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("div")
	child1 := d.CreateElement("p")
	child2 := d.CreateElement("p")
	nested := d.CreateElement("span")

	_ = parent.Node.AppendChild(&child1.Node)
	_ = parent.Node.AppendChild(&child2.Node)
	_ = child1.Node.AppendChild(&nested.Node)

	ps := parent.GetElementsByTagName("p")
	if len(ps) != 2 {
		t.Errorf("GetElementsByTagName(p): got %d, want 2", len(ps))
	}
}

// ----------------------------- Node tree tests -------------------------------

func TestNode_AppendChild(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("div")
	child := d.CreateElement("span")

	if err := parent.Node.AppendChild(&child.Node); err != nil {
		t.Fatalf("AppendChild: %v", err)
	}
	if parent.Node.FirstChild() != &child.Node {
		t.Error("FirstChild should be the appended child")
	}
	if child.Node.ParentNode() != &parent.Node {
		t.Error("child's parent should be the parent element")
	}
}

func TestNode_AppendChild_NilChild(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("div")
	if err := parent.Node.AppendChild(nil); err == nil {
		t.Error("AppendChild(nil) should return error")
	}
}

func TestNode_AppendChild_SelfCycle(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("div")
	if err := el.Node.AppendChild(&el.Node); err == nil {
		t.Error("AppendChild of self should return error")
	}
}

func TestNode_AppendChild_AncestorCycle(t *testing.T) {
	d := dom.NewDocument()
	grandparent := d.CreateElement("div")
	parent := d.CreateElement("div")
	_ = grandparent.Node.AppendChild(&parent.Node)

	// Trying to make grandparent a child of parent should fail.
	if err := parent.Node.AppendChild(&grandparent.Node); err == nil {
		t.Error("AppendChild of ancestor should return error")
	}
}

func TestNode_AppendChild_ReparentsChild(t *testing.T) {
	d := dom.NewDocument()
	parent1 := d.CreateElement("div")
	parent2 := d.CreateElement("div")
	child := d.CreateElement("span")

	_ = parent1.Node.AppendChild(&child.Node)
	_ = parent2.Node.AppendChild(&child.Node) // should reparent

	if child.Node.ParentNode() != &parent2.Node {
		t.Error("child should have been reparented to parent2")
	}
	if parent1.Node.FirstChild() != nil {
		t.Error("parent1 should have no children after reparenting")
	}
}

func TestNode_InsertBefore(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("ul")
	li1 := d.CreateElement("li")
	li2 := d.CreateElement("li")
	li3 := d.CreateElement("li")

	_ = parent.Node.AppendChild(&li1.Node)
	_ = parent.Node.AppendChild(&li3.Node)
	_ = parent.Node.InsertBefore(&li2.Node, &li3.Node)

	children := parent.Node.ChildNodes()
	if len(children) != 3 {
		t.Fatalf("ChildNodes count: got %d, want 3", len(children))
	}
	if children[1] != &li2.Node {
		t.Error("li2 should be the middle child")
	}
}

func TestNode_InsertBefore_NilRef(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("ul")
	li := d.CreateElement("li")
	if err := parent.Node.InsertBefore(&li.Node, nil); err != nil {
		t.Errorf("InsertBefore(nil) should append: %v", err)
	}
	if parent.Node.FirstChild() != &li.Node {
		t.Error("li should have been appended")
	}
}

func TestNode_RemoveChild(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("div")
	child := d.CreateElement("span")

	_ = parent.Node.AppendChild(&child.Node)
	if err := parent.Node.RemoveChild(&child.Node); err != nil {
		t.Fatalf("RemoveChild: %v", err)
	}
	if parent.Node.FirstChild() != nil {
		t.Error("parent should have no children after RemoveChild")
	}
	if child.Node.ParentNode() != nil {
		t.Error("removed child's parent should be nil")
	}
}

func TestNode_RemoveChild_NotAChild(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("div")
	stranger := d.CreateElement("span")
	if err := parent.Node.RemoveChild(&stranger.Node); err == nil {
		t.Error("RemoveChild of non-child should return error")
	}
}

func TestNode_ChildNodes_Multiple(t *testing.T) {
	d := dom.NewDocument()
	parent := d.CreateElement("div")
	for i := 0; i < 5; i++ {
		child := d.CreateElement("span")
		_ = parent.Node.AppendChild(&child.Node)
	}
	children := parent.Node.ChildNodes()
	if len(children) != 5 {
		t.Errorf("ChildNodes: got %d, want 5", len(children))
	}
}

// ----------------------------- CharacterData tests ---------------------------

func TestCharacterData_SubstringData(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("Hello, World!")
	sub, err := txt.SubstringData(7, 5)
	if err != nil {
		t.Fatalf("SubstringData: %v", err)
	}
	if sub != "World" {
		t.Errorf("SubstringData: got %q, want %q", sub, "World")
	}
}

func TestCharacterData_SubstringData_OutOfRange(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("Hi")
	_, err := txt.SubstringData(10, 3)
	if err == nil {
		t.Error("SubstringData with out-of-range offset should return error")
	}
}

func TestCharacterData_AppendData(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("Hello")
	txt.AppendData(", World!")
	if txt.Data != "Hello, World!" {
		t.Errorf("AppendData: got %q", txt.Data)
	}
}

func TestCharacterData_InsertData(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("Hello!")
	if err := txt.InsertData(5, " World"); err != nil {
		t.Fatalf("InsertData: %v", err)
	}
	if txt.Data != "Hello World!" {
		t.Errorf("InsertData: got %q, want %q", txt.Data, "Hello World!")
	}
}

func TestCharacterData_DeleteData(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("Hello, World!")
	if err := txt.DeleteData(5, 7); err != nil {
		t.Fatalf("DeleteData: %v", err)
	}
	if txt.Data != "Hello!" {
		t.Errorf("DeleteData: got %q, want %q", txt.Data, "Hello!")
	}
}

func TestCharacterData_DeleteData_BeyondLength(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("Hi")
	if err := txt.DeleteData(1, 100); err != nil {
		t.Fatalf("DeleteData should clamp, not error: %v", err)
	}
	if txt.Data != "H" {
		t.Errorf("DeleteData clamped: got %q, want %q", txt.Data, "H")
	}
}

func TestCharacterData_Length_Unicode(t *testing.T) {
	d := dom.NewDocument()
	txt := d.CreateTextNode("日本語")
	if txt.Length() != 3 {
		t.Errorf("Length of 3-rune string: got %d, want 3", txt.Length())
	}
}

// ----------------------------- Document queries ------------------------------

func TestDocument_GetElementsByTagName(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	p1 := d.CreateElement("p")
	p2 := d.CreateElement("p")

	_ = d.Node.AppendChild(&html.Node)
	_ = html.Node.AppendChild(&body.Node)
	_ = body.Node.AppendChild(&p1.Node)
	_ = body.Node.AppendChild(&p2.Node)

	ps := d.GetElementsByTagName("p")
	if len(ps) != 2 {
		t.Errorf("GetElementsByTagName(p): got %d, want 2", len(ps))
	}
}

func TestDocument_GetElementsByTagName_Wildcard(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	div := d.CreateElement("div")

	_ = d.Node.AppendChild(&html.Node)
	_ = html.Node.AppendChild(&body.Node)
	_ = body.Node.AppendChild(&div.Node)

	all := d.GetElementsByTagName("*")
	if len(all) != 3 {
		t.Errorf("GetElementsByTagName(*): got %d, want 3", len(all))
	}
}

func TestDocument_GetElementByID(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	section := d.CreateElement("section")
	section.SetAttribute("id", "main")

	_ = d.Node.AppendChild(&html.Node)
	_ = html.Node.AppendChild(&body.Node)
	_ = body.Node.AppendChild(&section.Node)

	found := d.GetElementByID("main")
	if found == nil {
		t.Fatal("GetElementByID: expected to find element")
	}
	if found.LocalName != "section" {
		t.Errorf("GetElementByID: got %q, want section", found.LocalName)
	}
}

func TestDocument_GetElementByID_NotFound(t *testing.T) {
	d := dom.NewDocument()
	if found := d.GetElementByID("missing"); found != nil {
		t.Error("GetElementByID should return nil for missing id")
	}
}

// ----------------------------- TextContent -----------------------------------

func TestNode_TextContent(t *testing.T) {
	d := dom.NewDocument()
	div := d.CreateElement("div")
	t1 := d.CreateTextNode("Hello ")
	t2 := d.CreateTextNode("World")
	_ = div.Node.AppendChild(&t1.Node)
	_ = div.Node.AppendChild(&t2.Node)

	tc := div.Node.TextContent()
	if tc != "Hello World" {
		t.Errorf("TextContent: got %q, want %q", tc, "Hello World")
	}
}

// ----------------------------- DocumentFragment ------------------------------

func TestDocumentFragment(t *testing.T) {
	d := dom.NewDocument()
	frag := dom.NewDocumentFragment(d)
	if frag.NodeType() != dom.DocumentFragmentNode {
		t.Errorf("NodeType: got %v, want DocumentFragmentNode", frag.NodeType())
	}
}

func TestNode_ReplaceChild_NotAChild(t *testing.T) {
doc := dom.NewDocument()
parent := dom.NewElement("div", doc)
other := dom.NewElement("span", doc)
newChild := dom.NewElement("p", doc)

_, err := parent.ReplaceChild(&newChild.Node, &other.Node)
if err == nil {
t.Error("expected error when oldChild is not a child")
}
}

// ----------------------------- Pseudo-Class State Tests -----------------------

func TestElement_SetHovered(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("a")

	if el.Hovered {
		t.Error("new element should not be hovered")
	}
	el.SetHovered(true)
	if !el.Hovered {
		t.Error("element should be hovered after SetHovered(true)")
	}
	el.SetHovered(false)
	if el.Hovered {
		t.Error("element should not be hovered after SetHovered(false)")
	}
}

func TestElement_SetFocused(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("input")

	if el.Focused {
		t.Error("new element should not be focused")
	}
	el.SetFocused(true)
	if !el.Focused {
		t.Error("element should be focused after SetFocused(true)")
	}
	el.SetFocused(false)
	if el.Focused {
		t.Error("element should not be focused after SetFocused(false)")
	}
}

func TestElement_SetActive(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("button")

	if el.Active {
		t.Error("new element should not be active")
	}
	el.SetActive(true)
	if !el.Active {
		t.Error("element should be active after SetActive(true)")
	}
	el.SetActive(false)
	if el.Active {
		t.Error("element should not be active after SetActive(false)")
	}
}

func TestElement_SetPseudoState(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("div")

	el.SetPseudoState("hover", true)
	if !el.Hovered {
		t.Error("SetPseudoState(hover, true) should set Hovered")
	}
	if el.Focused || el.Active {
		t.Error("SetPseudoState(hover) should not affect focus or active")
	}

	el.SetPseudoState("focus", true)
	if !el.Focused {
		t.Error("SetPseudoState(focus, true) should set Focused")
	}

	el.SetPseudoState("active", true)
	if !el.Active {
		t.Error("SetPseudoState(active, true) should set Active")
	}

	el.SetPseudoState("hover", false)
	if el.Hovered {
		t.Error("SetPseudoState(hover, false) should clear Hovered")
	}
}

func TestElement_PseudoStateInvalidatesStyle(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("a")

	if el.IsStyleDirty() {
		t.Error("new element should not be style-dirty")
	}

	el.SetHovered(true)
	if !el.IsStyleDirty() {
		t.Error("SetHovered should invalidate style (mark dirty)")
	}

	el.ClearStyleDirty()
	el.SetFocused(true)
	if !el.IsStyleDirty() {
		t.Error("SetFocused should invalidate style")
	}

	el.ClearStyleDirty()
	el.SetActive(true)
	if !el.IsStyleDirty() {
		t.Error("SetActive should invalidate style")
	}
}

func TestElement_PseudoStateNoOpSkipsInvalidation(t *testing.T) {
	d := dom.NewDocument()
	el := d.CreateElement("div")

	// Setting same value should not mark dirty.
	el.ClearStyleDirty()
	el.SetHovered(false) // already false
	if el.IsStyleDirty() {
		t.Error("no-op SetHovered should not invalidate style")
	}

	el.SetHovered(true)
	el.ClearStyleDirty()
	el.SetHovered(true) // already true
	if el.IsStyleDirty() {
		t.Error("no-op SetHovered(true) should not invalidate style")
	}
}
