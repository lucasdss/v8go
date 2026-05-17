// Package dom implements the Document Object Model as defined in the
// WHATWG DOM Living Standard: https://dom.spec.whatwg.org/
//
// It provides the Node hierarchy (Document, Element, Text, Comment,
// DocumentType) and tree-manipulation operations used by the HTML parser and
// the scripting layer.
package dom

import (
	"fmt"
	"net/url"
	"strings"
)

// ----------------------------- NodeType --------------------------------------

// NodeType mirrors DOM Standard § 4.4 "Node – nodeType".
type NodeType uint16

const (
	// ElementNode is an Element node type.
	ElementNode NodeType = 1
	// AttributeNode is an Attr node type.
	AttributeNode NodeType = 2
	// TextNode is a Text node type.
	TextNode NodeType = 3
	// CDATASectionNode is a CDATASection node type.
	CDATASectionNode NodeType = 4
	// ProcessingInstructionNode is a ProcessingInstruction node type.
	ProcessingInstructionNode NodeType = 7
	// CommentNode is a Comment node type.
	CommentNode NodeType = 8
	// DocumentNode is a Document node type.
	DocumentNode NodeType = 9
	// DocumentTypeNode is a DocumentType node type.
	DocumentTypeNode NodeType = 10
	// DocumentFragmentNode is a DocumentFragment node type.
	DocumentFragmentNode NodeType = 11
)

func (nt NodeType) String() string {
	switch nt {
	case ElementNode:
		return "Element"
	case TextNode:
		return "Text"
	case CommentNode:
		return "Comment"
	case DocumentNode:
		return "Document"
	case DocumentTypeNode:
		return "DocumentType"
	case DocumentFragmentNode:
		return "DocumentFragment"
	case CDATASectionNode:
		return "CDATASection"
	case ProcessingInstructionNode:
		return "ProcessingInstruction"
	default:
		return fmt.Sprintf("NodeType(%d)", uint16(nt))
	}
}

// NamespaceHTML is the HTML namespace URI.
const NamespaceHTML = "http://www.w3.org/1999/xhtml"

// ----------------------------- Node ------------------------------------------

// Node is the base struct for all DOM node types.  Concrete types embed Node
// as their first field; tree-walking code casts *Node to concrete types via
// the NodeImpl interface.
//
// See DOM Standard § 4.4.
type Node struct {
	nodeType        NodeType
	ownerDocument   *Document
	parentNode      *Node
	firstChild      *Node
	lastChild       *Node
	previousSibling *Node
	nextSibling     *Node
	// self is the concrete node value that contains this embedded Node.
	// It is set by the constructors and enables safe up-casts.
	self NodeImpl
	// listeners holds event listeners registered via AddEventListener.
	// Lazily initialized to avoid memory overhead for listener-free nodes.
	listeners *listenerList
	// styleDirty is set to true when this node or its subtree needs style recalculation.
	// Cleared after ComputeStyle() processes the node.
	styleDirty bool
}

// NodeImpl is the interface implemented by every concrete DOM node type.
// It gives the tree-walking helpers a way to retrieve the typed value.
type NodeImpl interface {
	// BaseNode returns the embedded *Node so callers can walk the tree.
	BaseNode() *Node
	// NodeType returns the DOM node-type constant.
	NodeType() NodeType
}

// BaseNode satisfies NodeImpl and returns the pointer to the embedded Node.
func (n *Node) BaseNode() *Node { return n }

// NodeType returns the node-type constant.
func (n *Node) NodeType() NodeType { return n.nodeType }

// OwnerDocument returns the document this node belongs to (nil for documents).
func (n *Node) OwnerDocument() *Document { return n.ownerDocument }

// ParentNode returns the parent node, or nil.
func (n *Node) ParentNode() *Node { return n.parentNode }

// InvalidateStyle marks this node and its ancestors as needing style recalculation.
// This is called automatically by DOM mutation methods (SetAttribute, AppendChild, etc.).
// The dirty flag propagates upward since a child change can affect parent layout
// (e.g., a new child changes the parent's height).
func (n *Node) InvalidateStyle() {
	n.styleDirty = true
	// Propagate to parent: child mutations can affect parent layout.
	if n.parentNode != nil {
		n.parentNode.InvalidateStyle()
	}
}

// IsStyleDirty reports whether this node needs style recalculation.
func (n *Node) IsStyleDirty() bool {
	return n.styleDirty
}

// ClearStyleDirty clears the style dirty flag after recalculation.
func (n *Node) ClearStyleDirty() {
	n.styleDirty = false
}

// InvalidateSubtreeStyle marks this node and all descendants as dirty.
// Use for bulk invalidation (e.g., stylesheet change affecting the whole tree).
func (n *Node) InvalidateSubtreeStyle() {
	n.styleDirty = true
	for child := n.firstChild; child != nil; child = child.nextSibling {
		child.InvalidateSubtreeStyle()
	}
}

// FirstChild returns the first child, or nil.
func (n *Node) FirstChild() *Node { return n.firstChild }

// LastChild returns the last child, or nil.
func (n *Node) LastChild() *Node { return n.lastChild }

// PreviousSibling returns the previous sibling, or nil.
func (n *Node) PreviousSibling() *Node { return n.previousSibling }

// NextSibling returns the next sibling, or nil.
func (n *Node) NextSibling() *Node { return n.nextSibling }

// Self returns the typed interface value for this node.
func (n *Node) Self() NodeImpl { return n.self }

// AppendChild inserts newChild at the end of this node's child list.
// Pre-insertion validity is checked per DOM Standard § 4.4.2.
func (n *Node) AppendChild(newChild *Node) error {
	if err := n.preInsertCheck(newChild, nil); err != nil {
		return err
	}
	if newChild.parentNode != nil {
		if err := newChild.parentNode.removeChild(newChild); err != nil {
			return err
		}
	}
	n.insertAtEnd(newChild)
	return nil
}

// InsertBefore inserts newChild immediately before referenceChild.
// Pass referenceChild = nil to append.
func (n *Node) InsertBefore(newChild, referenceChild *Node) error {
	if referenceChild == nil {
		return n.AppendChild(newChild)
	}
	if err := n.preInsertCheck(newChild, referenceChild); err != nil {
		return err
	}
	if newChild.parentNode != nil {
		if err := newChild.parentNode.removeChild(newChild); err != nil {
			return err
		}
	}
	n.insertBefore(newChild, referenceChild)
	return nil
}

// RemoveChild removes child from the child list.
func (n *Node) RemoveChild(child *Node) error {
	if child == nil {
		return fmt.Errorf("RemoveChild: nil child")
	}
	if child.parentNode != n {
		return fmt.Errorf("RemoveChild: not a child of this node")
	}
	return n.removeChild(child)
}

// ReplaceChild replaces oldChild with newChild and returns oldChild.
// See DOM Standard § 4.4.3.
func (n *Node) ReplaceChild(newChild, oldChild *Node) (*Node, error) {
	if oldChild == nil {
		return nil, fmt.Errorf("ReplaceChild: nil oldChild")
	}
	if newChild == nil {
		return nil, fmt.Errorf("ReplaceChild: nil newChild")
	}
	if oldChild.parentNode != n {
		return nil, fmt.Errorf("ReplaceChild: oldChild is not a child of this node")
	}
	// Validate and re-parent newChild if needed.
	if err := n.preInsertCheck(newChild, oldChild); err != nil {
		return nil, err
	}
	if newChild.parentNode != nil {
		if err := newChild.parentNode.removeChild(newChild); err != nil {
			return nil, err
		}
	}
	// Insert newChild before oldChild.
	n.insertBefore(newChild, oldChild)
	// Remove oldChild.
	if err := n.removeChild(oldChild); err != nil {
		return nil, err
	}
	return oldChild, nil
}

// Contains reports whether other is a descendant of this node (inclusive).
// See DOM Standard § 4.4.3.
func (n *Node) Contains(other *Node) bool {
	if other == nil || n == nil {
		return false
	}
	for p := other; p != nil; p = p.parentNode {
		if p == n {
			return true
		}
	}
	return false
}

// ChildNodes returns a slice snapshot of the child nodes.
func (n *Node) ChildNodes() []*Node {
	var out []*Node
	for c := n.firstChild; c != nil; c = c.nextSibling {
		out = append(out, c)
	}
	return out
}

// TextContent returns the concatenated text of all descendant Text nodes.
func (n *Node) TextContent() string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(cur *Node) {
		for c := cur.firstChild; c != nil; c = c.nextSibling {
			if c.nodeType == TextNode {
				if t, ok := c.self.(*Text); ok {
					b.WriteString(t.Data)
				}
			}
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// SetTextContent replaces all child text nodes with a single Text node
// containing the given string. Existing children are removed.
func (e *Element) SetTextContent(text string) {
	// Remove all existing children.
	for c := e.firstChild; c != nil; {
		next := c.nextSibling
		c.parentNode = nil
		c.previousSibling = nil
		c.nextSibling = nil
		c = next
	}
	e.firstChild = nil
	e.lastChild = nil

	// Create and append a new Text node.
	if text != "" {
		t := e.ownerDocument.CreateTextNode(text)
		_ = e.AppendChild(&t.Node)
	}
	e.Node.InvalidateStyle()
}

// ------- internal helpers ----------------------------------------------------

func (n *Node) preInsertCheck(newChild, ref *Node) error {
	if newChild == nil {
		return fmt.Errorf("pre-insert: nil child")
	}
	if newChild == n {
		return fmt.Errorf("pre-insert: node cannot be inserted as a child of itself")
	}
	if isAncestor(newChild, n) {
		return fmt.Errorf("pre-insert: new child is an ancestor of the parent")
	}
	if ref != nil && ref.parentNode != n {
		return fmt.Errorf("pre-insert: reference child is not a child of this node")
	}
	return nil
}

func (n *Node) insertAtEnd(child *Node) {
	child.parentNode = n
	child.previousSibling = n.lastChild
	child.nextSibling = nil
	if n.lastChild != nil {
		n.lastChild.nextSibling = child
	} else {
		n.firstChild = child
	}
	n.lastChild = child
	// Style invalidation: child insertion affects parent layout.
	n.InvalidateStyle()
}

func (n *Node) insertBefore(newChild, ref *Node) {
	newChild.parentNode = n
	newChild.nextSibling = ref
	newChild.previousSibling = ref.previousSibling
	if ref.previousSibling != nil {
		ref.previousSibling.nextSibling = newChild
	} else {
		n.firstChild = newChild
	}
	ref.previousSibling = newChild
	// Style invalidation: child insertion affects parent layout.
	n.InvalidateStyle()
}

func (n *Node) removeChild(child *Node) error {
	if child.previousSibling != nil {
		child.previousSibling.nextSibling = child.nextSibling
	} else {
		n.firstChild = child.nextSibling
	}
	if child.nextSibling != nil {
		child.nextSibling.previousSibling = child.previousSibling
	} else {
		n.lastChild = child.previousSibling
	}
	child.parentNode = nil
	child.previousSibling = nil
	child.nextSibling = nil
	// Style invalidation: child removal affects parent layout.
	n.InvalidateStyle()
	return nil
}

// isAncestor reports whether a is an ancestor of n.
func isAncestor(a, n *Node) bool {
	for p := n.parentNode; p != nil; p = p.parentNode {
		if p == a {
			return true
		}
	}
	return false
}

// ----------------------------- Attr ------------------------------------------

// Attr represents a DOM attribute.  See DOM Standard § 4.9.
type Attr struct {
	NamespaceURI string
	Prefix       string
	LocalName    string
	Value        string
}

// Name returns the qualified attribute name.
func (a *Attr) Name() string {
	if a.Prefix != "" {
		return a.Prefix + ":" + a.LocalName
	}
	return a.LocalName
}

// ----------------------------- Element ---------------------------------------

// Element represents an HTML/XML element node.  See DOM Standard § 4.9.
type Element struct {
	Node
	NamespaceURI string
	LocalName    string
	Prefix       string
	attributes   []*Attr
	// Inline event handler attributes (HTML Standard § 7.1.6.1).
	OnClick  string
	OnSubmit string
	OnInput  string
	// Pseudo-class state tracking.
	Hovered bool
	Focused bool
	Active  bool
}

// NewElement creates a new Element owned by owner.
func NewElement(localName string, owner *Document) *Element {
	e := &Element{LocalName: strings.ToLower(localName)}
	e.nodeType = ElementNode
	e.ownerDocument = owner
	e.self = e
	return e
}

// NewElementNS creates a new Element with the given namespace and local name.
func NewElementNS(namespaceURI, localName string, owner *Document) *Element {
	e := NewElement(localName, owner)
	e.NamespaceURI = namespaceURI
	return e
}

// TagName returns the serialized tag name per DOM Standard § 4.9.
func (e *Element) TagName() string {
	qn := e.LocalName
	if e.Prefix != "" {
		qn = e.Prefix + ":" + e.LocalName
	}
	if e.NamespaceURI == "" || e.NamespaceURI == NamespaceHTML {
		return strings.ToUpper(qn)
	}
	return strings.ToUpper(qn)
}

// GetAttribute returns the value of the named attribute, or "".
func (e *Element) GetAttribute(name string) string {
	name = strings.ToLower(name)
	for _, a := range e.attributes {
		if strings.ToLower(a.Name()) == name {
			return a.Value
		}
	}
	return ""
}

// HasAttribute reports whether the element carries the named attribute.
func (e *Element) HasAttribute(name string) bool {
	name = strings.ToLower(name)
	for _, a := range e.attributes {
		if strings.ToLower(a.Name()) == name {
			return true
		}
	}
	return false
}

// SetAttribute creates or updates an attribute.
func (e *Element) SetAttribute(name, value string) {
	name = strings.ToLower(name)
	for _, a := range e.attributes {
		if strings.ToLower(a.Name()) == name {
			a.Value = value
			e.syncEventHandlerField(name, value)
			e.Node.InvalidateStyle()
			return
		}
	}
	e.attributes = append(e.attributes, &Attr{LocalName: name, Value: value})
	e.syncEventHandlerField(name, value)
	e.Node.InvalidateStyle()
}

// syncEventHandlerField mirrors handler attribute changes into the struct fields.
func (e *Element) syncEventHandlerField(name, value string) {
	switch name {
	case "onclick":
		e.OnClick = value
	case "onsubmit":
		e.OnSubmit = value
	case "oninput":
		e.OnInput = value
	}
}

// RemoveAttribute removes the named attribute.
func (e *Element) RemoveAttribute(name string) {
	name = strings.ToLower(name)
	for i, a := range e.attributes {
		if strings.ToLower(a.Name()) == name {
			e.attributes = append(e.attributes[:i], e.attributes[i+1:]...)
			e.Node.InvalidateStyle()
			return
		}
	}
}

// Attributes returns a deep copy of the attribute list so callers cannot
// modify the element's internal state through the returned slice.
func (e *Element) Attributes() []*Attr {
	out := make([]*Attr, len(e.attributes))
	for i, a := range e.attributes {
		cp := *a // shallow copy of the struct value (all fields are value types)
		out[i] = &cp
	}
	return out
}

// ID returns the value of the "id" attribute.
func (e *Element) ID() string { return e.GetAttribute("id") }

// GetEventHandler returns the inline event handler code for the given event type.
// eventType is one of "click", "submit", "input" (per HTML Standard § 7.1.6.1).
func (e *Element) GetEventHandler(eventType string) string {
	switch eventType {
	case "click":
		return e.OnClick
	case "submit":
		return e.OnSubmit
	case "input":
		return e.OnInput
	}
	return ""
}

// ClassName returns the value of the "class" attribute.
func (e *Element) ClassName() string { return e.GetAttribute("class") }

// ClassList returns the class tokens split from the "class" attribute.
func (e *Element) ClassList() []string {
	cls := e.ClassName()
	if cls == "" {
		return nil
	}
	return strings.Fields(cls)
}

// Children returns child Elements (non-element children are skipped).
func (e *Element) Children() []*Element {
	var out []*Element
	for c := e.firstChild; c != nil; c = c.nextSibling {
		if c.nodeType == ElementNode {
			if child, ok := c.self.(*Element); ok {
				out = append(out, child)
			}
		}
	}
	return out
}

	// GetElementsByTagName returns all descendant Elements with the given tag.
func (e *Element) GetElementsByTagName(localName string) []*Element {
	localName = strings.ToLower(localName)
	var results []*Element
	collectByTagName(&e.Node, localName, &results)
	return results
}

// GetElementsByClassName returns all descendant Elements matching the given
// class name. See DOM Standard § 4.9.
func (e *Element) GetElementsByClassName(className string) []*Element {
	var results []*Element
	collectByClassName(&e.Node, className, &results)
	return results
}

// SetHovered sets the hovered pseudo-class state and invalidates style.
func (e *Element) SetHovered(v bool) {
	if e.Hovered == v {
		return
	}
	e.Hovered = v
	e.Node.InvalidateStyle()
}

// SetFocused sets the focused pseudo-class state and invalidates style.
func (e *Element) SetFocused(v bool) {
	if e.Focused == v {
		return
	}
	e.Focused = v
	e.Node.InvalidateStyle()
}

// SetActive sets the active pseudo-class state and invalidates style.
func (e *Element) SetActive(v bool) {
	if e.Active == v {
		return
	}
	e.Active = v
	e.Node.InvalidateStyle()
}

// SetPseudoState sets a pseudo-class state by name ("hover", "focus", "active").
func (e *Element) SetPseudoState(state string, active bool) {
	switch strings.ToLower(state) {
	case "hover":
		e.SetHovered(active)
	case "focus":
		e.SetFocused(active)
	case "active":
		e.SetActive(active)
	}
}

// SerializeForm serializes all form control descendants of form into an
// application/x-www-form-urlencoded query string. It collects <input>,
// <select>, and <textarea> elements with a name attribute, URL-encodes
// each name=value pair, and joins them with '&'.
//
// Checkbox and radio inputs are only included when they have a "checked"
// attribute. Submit buttons are excluded.
func SerializeForm(form *Element) string {
	var parts []string
	collectFormControls(&form.Node, &parts)
	return strings.Join(parts, "&")
}

func collectFormControls(n *Node, parts *[]string) {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.nodeType == ElementNode {
			elem, ok := c.self.(*Element)
			if !ok {
				collectFormControls(c, parts)
				continue
			}
			tag := elem.LocalName
			if tag == "input" {
				typ := strings.ToLower(elem.GetAttribute("type"))
				if typ == "" {
					typ = "text"
				}
				if typ == "submit" || typ == "button" || typ == "reset" || typ == "image" {
					collectFormControls(c, parts)
					continue
				}
				if typ == "checkbox" || typ == "radio" {
					if !elem.HasAttribute("checked") {
						collectFormControls(c, parts)
						continue
					}
				}
				name := elem.GetAttribute("name")
				if name == "" {
					collectFormControls(c, parts)
					continue
				}
				val := elem.GetAttribute("value")
				*parts = append(*parts, url.QueryEscape(name)+"="+url.QueryEscape(val))
			} else if tag == "select" {
				name := elem.GetAttribute("name")
				if name == "" {
					collectFormControls(c, parts)
					continue
				}
				val := selectedOptionValue(elem)
				*parts = append(*parts, url.QueryEscape(name)+"="+url.QueryEscape(val))
			} else if tag == "textarea" {
				name := elem.GetAttribute("name")
				if name == "" {
					collectFormControls(c, parts)
					continue
				}
				val := elem.TextContent()
				*parts = append(*parts, url.QueryEscape(name)+"="+url.QueryEscape(val))
			}
			collectFormControls(c, parts)
		} else {
			collectFormControls(c, parts)
		}
	}
}

// selectedOptionValue returns the value of the first <option> with a
// "selected" attribute, or the first <option>'s value, or "".
func selectedOptionValue(selectElem *Element) string {
	var firstVal string
	for c := selectElem.FirstChild(); c != nil; c = c.NextSibling() {
		if c.NodeType() != ElementNode {
			continue
		}
		opt, ok := c.Self().(*Element)
		if !ok || opt.LocalName != "option" {
			continue
		}
		val := opt.GetAttribute("value")
		if val == "" {
			val = opt.TextContent()
		}
		if firstVal == "" {
			firstVal = val
		}
		if opt.HasAttribute("selected") {
			return val
		}
	}
	return firstVal
}

func collectByClassName(n *Node, className string, results *[]*Element) {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.nodeType == ElementNode {
			if elem, ok := c.self.(*Element); ok {
				for _, cls := range elem.ClassList() {
					if cls == className {
						*results = append(*results, elem)
						break
					}
				}
				collectByClassName(c, className, results)
			}
		} else {
			collectByClassName(c, className, results)
		}
	}
}

// ----------------------------- CharacterData ---------------------------------

// CharacterData is the base struct for Text and Comment nodes.
// See DOM Standard § 4.10.
type CharacterData struct {
	Node
	Data string
}

// Length returns the number of UTF-16 code units (approximated by rune count).
func (cd *CharacterData) Length() int { return len([]rune(cd.Data)) }

// SubstringData returns a substring of Data.
func (cd *CharacterData) SubstringData(offset, count int) (string, error) {
	runes := []rune(cd.Data)
	n := len(runes)
	if offset < 0 || offset > n {
		return "", fmt.Errorf("SubstringData: offset %d out of range [0,%d]", offset, n)
	}
	end := offset + count
	if end > n {
		end = n
	}
	return string(runes[offset:end]), nil
}

// AppendData appends s to Data.
func (cd *CharacterData) AppendData(s string) { cd.Data += s }

// InsertData inserts s at offset.
func (cd *CharacterData) InsertData(offset int, s string) error {
	runes := []rune(cd.Data)
	if offset < 0 || offset > len(runes) {
		return fmt.Errorf("InsertData: offset %d out of range", offset)
	}
	result := make([]rune, 0, len(runes)+len([]rune(s)))
	result = append(result, runes[:offset]...)
	result = append(result, []rune(s)...)
	result = append(result, runes[offset:]...)
	cd.Data = string(result)
	return nil
}

// DeleteData removes count code units starting at offset.
func (cd *CharacterData) DeleteData(offset, count int) error {
	runes := []rune(cd.Data)
	n := len(runes)
	if offset < 0 || offset > n {
		return fmt.Errorf("DeleteData: offset %d out of range", offset)
	}
	end := offset + count
	if end > n {
		end = n
	}
	cd.Data = string(append(runes[:offset:offset], runes[end:]...))
	return nil
}

// ----------------------------- Text ------------------------------------------

// Text represents a text node.  See DOM Standard § 4.11.
type Text struct {
	CharacterData
}

// NewText creates a Text node with the given data.
func NewText(data string, owner *Document) *Text {
	t := &Text{}
	t.Data = data
	t.nodeType = TextNode
	t.ownerDocument = owner
	t.self = t
	return t
}

// ----------------------------- Comment ---------------------------------------

// Comment represents a comment node.  See DOM Standard § 4.13.
type Comment struct {
	CharacterData
}

// NewComment creates a Comment node.
func NewComment(data string, owner *Document) *Comment {
	c := &Comment{}
	c.Data = data
	c.nodeType = CommentNode
	c.ownerDocument = owner
	c.self = c
	return c
}

// ----------------------------- DocumentType ----------------------------------

// DocumentType represents the <!DOCTYPE> declaration.
// See DOM Standard § 4.12.
type DocumentType struct {
	Node
	Name     string
	PublicID string
	SystemID string
}

// NewDocumentType creates a DocumentType node.
func NewDocumentType(name, publicID, systemID string, owner *Document) *DocumentType {
	dt := &DocumentType{Name: name, PublicID: publicID, SystemID: systemID}
	dt.nodeType = DocumentTypeNode
	dt.ownerDocument = owner
	dt.self = dt
	return dt
}

// ----------------------------- DocumentFragment ------------------------------

// DocumentFragment is a minimal document object with no parent.
// See DOM Standard § 4.6.
type DocumentFragment struct {
	Node
}

// NewDocumentFragment creates a new DocumentFragment.
func NewDocumentFragment(owner *Document) *DocumentFragment {
	df := &DocumentFragment{}
	df.nodeType = DocumentFragmentNode
	df.ownerDocument = owner
	df.self = df
	return df
}

// ----------------------------- Document --------------------------------------

// Document is the root of the DOM tree.  See DOM Standard § 4.5.
type Document struct {
	Node
	ContentType     string
	URL             string
	CharacterSet    string
	Doctype         *DocumentType
	DocumentElement *Element
}

// NewDocument creates an empty HTML Document.
func NewDocument() *Document {
	d := &Document{
		ContentType:  "text/html",
		CharacterSet: "UTF-8",
	}
	d.nodeType = DocumentNode
	d.self = d
	return d
}

// CreateElement creates an Element in the HTML namespace.
func (d *Document) CreateElement(localName string) *Element {
	return NewElement(localName, d)
}

// CreateElementNS creates an Element in the given namespace.
func (d *Document) CreateElementNS(ns, localName string) *Element {
	return NewElementNS(ns, localName, d)
}

// CreateTextNode creates a Text node.
func (d *Document) CreateTextNode(data string) *Text {
	return NewText(data, d)
}

// CreateComment creates a Comment node.
func (d *Document) CreateComment(data string) *Comment {
	return NewComment(data, d)
}

// CreateDocumentType creates a DocumentType node.
func (d *Document) CreateDocumentType(name, publicID, systemID string) *DocumentType {
	return NewDocumentType(name, publicID, systemID, d)
}

// GetElementsByTagName returns all descendant elements matching localName.
// A wildcard "*" matches all elements.
func (d *Document) GetElementsByTagName(localName string) []*Element {
	localName = strings.ToLower(localName)
	var results []*Element
	collectByTagName(&d.Node, localName, &results)
	return results
}

// GetElementByID returns the first element with the given id, or nil.
func (d *Document) GetElementByID(id string) *Element {
	return findByID(&d.Node, id)
}

// Title returns the text content of the first <title> element.
func (d *Document) Title() string {
	titles := d.GetElementsByTagName("title")
	if len(titles) == 0 {
		return ""
	}
	return titles[0].TextContent()
}

// Head returns the first <head> element, or nil.
func (d *Document) Head() *Element {
	heads := d.GetElementsByTagName("head")
	if len(heads) == 0 {
		return nil
	}
	return heads[0]
}

// Body returns the first <body> element, or nil.
func (d *Document) Body() *Element {
	bodies := d.GetElementsByTagName("body")
	if len(bodies) == 0 {
		return nil
	}
	return bodies[0]
}

// ------- tree helpers --------------------------------------------------------

// collectByTagName does a depth-first traversal collecting Elements by tag.
func collectByTagName(n *Node, localName string, results *[]*Element) {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.nodeType == ElementNode {
			if elem, ok := c.self.(*Element); ok {
				if localName == "*" || elem.LocalName == localName {
					*results = append(*results, elem)
				}
				collectByTagName(c, localName, results)
			}
		} else {
			collectByTagName(c, localName, results)
		}
	}
}

// findByID does a depth-first search for element by id attribute.
func findByID(n *Node, id string) *Element {
	for c := n.firstChild; c != nil; c = c.nextSibling {
		if c.nodeType == ElementNode {
			if elem, ok := c.self.(*Element); ok {
				if elem.ID() == id {
					return elem
				}
				if found := findByID(c, id); found != nil {
					return found
				}
			}
		} else {
			if found := findByID(c, id); found != nil {
				return found
			}
		}
	}
	return nil
}
