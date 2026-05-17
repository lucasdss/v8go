package dom

import (
"fmt"
"strings"
)

// CloneNode returns a deep or shallow copy of the node.
func (n *Node) CloneNode(deep bool) *Node {
impl := n.Self()
switch v := impl.(type) {
case *Element:
return cloneElement(v, deep).BaseNode()
case *Text:
return cloneText(v).BaseNode()
case *Comment:
return cloneComment(v).BaseNode()
case *Document:
return cloneDocument(v, deep).BaseNode()
case *DocumentFragment:
return cloneDocumentFragment(v, deep).BaseNode()
default:
return nil
}
}

func cloneElement(el *Element, deep bool) *Element {
copy := NewElement(el.LocalName, el.OwnerDocument())
for _, a := range el.Attributes() {
copy.SetAttribute(a.LocalName, a.Value)
}
if deep {
for child := el.FirstChild(); child != nil; child = child.NextSibling() {
clone := child.CloneNode(true)
if clone != nil {
_ = copy.AppendChild(clone)
}
}
}
return copy
}

func cloneText(t *Text) *Text {
return NewText(t.Data, t.OwnerDocument())
}

func cloneComment(c *Comment) *Comment {
return NewComment(c.Data, c.OwnerDocument())
}

func cloneDocument(d *Document, deep bool) *Document {
copy := NewDocument()
if deep {
for child := d.FirstChild(); child != nil; child = child.NextSibling() {
clone := child.CloneNode(true)
if clone != nil {
_ = copy.AppendChild(clone)
}
}
}
return copy
}

func cloneDocumentFragment(df *DocumentFragment, deep bool) *DocumentFragment {
copy := &DocumentFragment{}
copy.Node = Node{nodeType: DocumentFragmentNode, self: copy}
copy.ownerDocument = df.OwnerDocument()
if deep {
for child := df.FirstChild(); child != nil; child = child.NextSibling() {
clone := child.CloneNode(true)
if clone != nil {
_ = copy.AppendChild(clone)
}
}
}
return copy
}

// ImportNode imports a node from another document.
func (d *Document) ImportNode(n *Node, deep bool) *Node {
clone := n.CloneNode(deep)
if clone != nil {
clone.ownerDocument = d
}
return clone
}

// IsConnected reports whether the node is connected to a document.
func (n *Node) IsConnected() bool {
for p := n; p != nil; p = p.ParentNode() {
if p.NodeType() == DocumentNode {
return true
}
}
return false
}

// GetRootNode returns the root of the tree (the document or shadow root).
func (n *Node) GetRootNode() *Node {
root := n.Self().BaseNode()
for p := n.ParentNode(); p != nil; p = p.ParentNode() {
root = p
}
return root
}

// CompareDocumentPosition compares the position of n relative to other.
func (n *Node) CompareDocumentPosition(other *Node) int {
if n == other {
return 0
}
// Check if n contains other.
if n.contains(other) {
return DocumentPositionContainedBy | DocumentPositionFollowing
}
if other.contains(n) {
return DocumentPositionContains | DocumentPositionPreceding
}
// Tree order.
return DocumentPositionDisconnected
}

func (n *Node) contains(other *Node) bool {
for p := other; p != nil; p = p.ParentNode() {
if p == n {
return true
}
}
return false
}

const (
DocumentPositionDisconnected    = 0x01
DocumentPositionPreceding       = 0x02
DocumentPositionFollowing       = 0x04
DocumentPositionContains        = 0x08
DocumentPositionContainedBy     = 0x10
)

// InsertAdjacentHTML parses HTML and inserts the resulting nodes.
func (e *Element) InsertAdjacentHTML(position, html string) error {
doc := e.OwnerDocument()
if doc == nil {
return fmt.Errorf("no owner document")
}

// Parse HTML fragment.
fragment := ParseHTMLFragment(html, doc)

switch strings.ToLower(position) {
case "beforebegin":
if e.ParentNode() != nil {
for child := fragment.FirstChild(); child != nil; {
next := child.NextSibling()
_ = e.ParentNode().InsertBefore(child, &e.Node)
child = next
}
}
case "afterbegin":
for child := fragment.FirstChild(); child != nil; {
next := child.NextSibling()
_ = e.InsertBefore(child, e.FirstChild())
child = next
}
case "beforeend":
for child := fragment.FirstChild(); child != nil; {
next := child.NextSibling()
_ = e.AppendChild(child)
child = next
}
case "afterend":
if e.ParentNode() != nil {
for child := fragment.LastChild(); child != nil; {
prev := child.PreviousSibling()
_ = e.ParentNode().InsertBefore(child, e.NextSibling())
child = prev
}
}
default:
return fmt.Errorf("invalid position: %s", position)
}
return nil
}

// Before inserts nodes before this element.
func (e *Element) Before(nodes ...*Node) error {
parent := e.ParentNode()
if parent == nil {
return fmt.Errorf("no parent")
}
for _, node := range nodes {
if err := parent.InsertBefore(node, &e.Node); err != nil {
return err
}
}
return nil
}

// After inserts nodes after this element.
func (e *Element) After(nodes ...*Node) error {
parent := e.ParentNode()
if parent == nil {
return fmt.Errorf("no parent")
}
ref := e.NextSibling()
if ref == nil {
for _, node := range nodes {
if err := parent.AppendChild(node); err != nil {
return err
}
}
} else {
for _, node := range nodes {
if err := parent.InsertBefore(node, ref); err != nil {
return err
}
}
}
return nil
}

// ReplaceWith replaces this element with the given nodes.
func (e *Element) ReplaceWith(nodes ...*Node) error {
parent := e.ParentNode()
if parent == nil {
return fmt.Errorf("no parent")
}
ref := e.NextSibling()
if err := parent.RemoveChild(&e.Node); err != nil {
	return err
}
if ref == nil {
for _, node := range nodes {
if err := parent.AppendChild(node); err != nil {
return err
}
}
} else {
for _, node := range nodes {
if err := parent.InsertBefore(node, ref); err != nil {
return err
}
}
}
return nil
}

// Remove removes this element from the DOM.
func (e *Element) Remove() error {
parent := e.ParentNode()
if parent == nil {
return nil
}
return parent.RemoveChild(&e.Node)
}

// ParseHTMLFragment parses a simple HTML fragment. This is a simplified parser.
func ParseHTMLFragment(html string, doc *Document) *DocumentFragment {
f := &DocumentFragment{}
f.Node = Node{nodeType: DocumentFragmentNode, self: f, ownerDocument: doc}

// Simple tag parsing for fragment.
i := 0
runes := []rune(html)
var currentParent *Node = &f.Node

for i < len(runes) {
// Find next '<'
lt := -1
for j := i; j < len(runes); j++ {
if runes[j] == '<' {
lt = j
break
}
}

// Text before tag.
if lt > i {
text := strings.TrimSpace(string(runes[i:lt]))
if text != "" {
tn := NewText(text, doc)
_ = currentParent.AppendChild(&tn.Node)
}
}
if lt < 0 {
break
}

i = lt + 1
if i >= len(runes) {
break
}

if runes[i] == '/' {
// End tag — pop up.
end := i + 1
for end < len(runes) && runes[end] != '>' {
end++
}
if currentParent.ParentNode() != nil {
currentParent = currentParent.ParentNode()
}
i = end + 1
continue
}

// Start tag: read name.
start := i
for i < len(runes) && runes[i] != '>' && runes[i] != ' ' && runes[i] != '/' {
i++
}
tagName := strings.ToLower(string(runes[start:i]))

// Skip attributes to '>'.
selfClosing := false
for i < len(runes) && runes[i] != '>' {
if runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '>' {
selfClosing = true
i += 2
break
}
i++
}
if i < len(runes) && runes[i] == '>' {
i++
}

el := NewElement(tagName, doc)
_ = currentParent.AppendChild(&el.Node)

if !selfClosing && !isVoidElement(tagName) {
currentParent = &el.Node
}
}

return f
}

func isVoidElement(tag string) bool {
voids := map[string]bool{
"area": true, "base": true, "br": true, "col": true, "embed": true,
"hr": true, "img": true, "input": true, "link": true, "meta": true,
"param": true, "source": true, "track": true, "wbr": true,
}
return voids[tag]
}
