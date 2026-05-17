package idl

import (
"os"
"strings"
"testing"
)

func TestParseIDL_Element(t *testing.T) {
data, err := os.ReadFile("testdata/element.idl")
if err != nil {
t.Fatal(err)
}

iface, err := ParseIDL(string(data))
if err != nil {
t.Fatal(err)
}

if iface.Name != "Element" {
t.Errorf("expected Element, got %q", iface.Name)
}
if iface.Parent != "Node" {
t.Errorf("expected parent Node, got %q", iface.Parent)
}

if len(iface.Attributes) != 3 {
t.Fatalf("expected 3 attributes, got %d", len(iface.Attributes))
}

// id attribute.
if iface.Attributes[0].Name != "id" || iface.Attributes[0].Type != "DOMString" {
t.Errorf("attr[0]: expected id:DOMString, got %s:%s", iface.Attributes[0].Name, iface.Attributes[0].Type)
}

// tagName is readonly.
if !iface.Attributes[2].ReadOnly {
t.Error("tagName should be readonly")
}

if len(iface.Operations) < 2 {
t.Fatalf("expected at least 2 operations, got %d", len(iface.Operations))
}
}

func TestParseIDL_Document(t *testing.T) {
data, err := os.ReadFile("testdata/document.idl")
if err != nil {
t.Fatal(err)
}

iface, err := ParseIDL(string(data))
if err != nil {
t.Fatal(err)
}

if iface.Name != "Document" {
t.Errorf("expected Document, got %q", iface.Name)
}
if len(iface.Operations) < 2 {
t.Fatalf("expected at least 2 operations, got %d", len(iface.Operations))
}
}

func TestParseIDL_Inline(t *testing.T) {
src := `interface HTMLElement : Element {
  attribute DOMString title;
  attribute DOMString lang;
  readonly attribute DOMString localName;
  void click();
};`

iface, err := ParseIDL(src)
if err != nil {
t.Fatal(err)
}

if iface.Name != "HTMLElement" {
t.Errorf("expected HTMLElement, got %q", iface.Name)
}
if iface.Parent != "Element" {
t.Errorf("expected Element parent, got %q", iface.Parent)
}
if len(iface.Attributes) != 3 {
t.Errorf("expected 3 attributes, got %d", len(iface.Attributes))
}
if len(iface.Operations) != 1 {
t.Errorf("expected 1 operation, got %d", len(iface.Operations))
}
}

func TestGenerateGo_Element(t *testing.T) {
src := `interface Element {
  attribute DOMString id;
  readonly attribute DOMString tagName;
  DOMString getAttribute(DOMString name);
};`

iface, _ := ParseIDL(src)
code := GenerateGo(iface)

// Verify key pieces are in the generated code.
checks := []string{
"type ElementWrapper struct",
"func NewElementWrapper",
"func (w *ElementWrapper) SetId(val string)",
"func (w *ElementWrapper) GetId() string",
"func (w *ElementWrapper) GetTagName() string",
"func (w *ElementWrapper) getAttribute(name string) string",
}

for _, c := range checks {
if !strings.Contains(code, c) {
t.Errorf("generated code missing: %q", c)
}
}
}

func TestGenerateGo_Document(t *testing.T) {
src := `interface Document {
  attribute DOMString title;
  Element getElementById(DOMString id);
};`

iface, _ := ParseIDL(src)
code := GenerateGo(iface)

checks := []string{
"type DocumentWrapper struct",
"func NewDocumentWrapper",
"func (w *DocumentWrapper) SetTitle(val string)",
"func (w *DocumentWrapper) GetTitle() string",
}

for _, c := range checks {
if !strings.Contains(code, c) {
t.Errorf("generated code missing: %q", c)
}
}
}

func TestIDLToGoType(t *testing.T) {
tests := []struct{ idl, goType string }{
{"DOMString", "string"},
{"boolean", "bool"},
{"double", "float64"},
{"long", "int"},
{"void", ""},
{"USVString", "string"},
}
for _, tt := range tests {
got := idlToGoType(tt.idl)
if got != tt.goType {
t.Errorf("idlToGoType(%q) = %q, want %q", tt.idl, got, tt.goType)
}
}
}

func TestGenerateGo_ParentInterface(t *testing.T) {
src := `interface HTMLElement : Element {
  attribute DOMString title;
};`

iface, _ := ParseIDL(src)
code := GenerateGo(iface)

if !strings.Contains(code, "HTMLElementWrapper") {
t.Error("missing HTMLElementWrapper type")
}
}
