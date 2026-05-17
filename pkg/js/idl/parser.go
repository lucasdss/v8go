// Package idl implements a Web IDL parser and Go code generator.
// It reads .idl files and produces Go wrapper types for DOM bindings.
package idl

import (
"strings"
)

// Interface represents a parsed Web IDL interface.
type Interface struct {
Name       string
Parent     string
Attributes []Attribute
Operations []Operation
}

// Attribute represents a Web IDL attribute.
type Attribute struct {
Name     string
Type     string
ReadOnly bool
}

// Operation represents a Web IDL operation (method).
type Operation struct {
Name       string
ReturnType string
Args       []Arg
}

// Arg represents a method argument.
type Arg struct {
Name string
Type string
}

// ParseIDL parses a Web IDL string and returns the interface definition.
func ParseIDL(source string) (*Interface, error) {
iface := &Interface{}

source = stripComments(source)
tokens := tokenizeIDL(source)

i := 0
for i < len(tokens) {
tok := tokens[i]
switch {
case tok == "interface":
i++
if i < len(tokens) {
iface.Name = tokens[i]
i++
}
// Check for parent interface.
if i < len(tokens) && tokens[i] == ":" {
i++
if i < len(tokens) {
iface.Parent = tokens[i]
i++
}
}
case tok == "attribute" || tok == "readonly":
attr := Attribute{}
if tok == "readonly" {
attr.ReadOnly = true
i++
if i < len(tokens) && tokens[i] == "attribute" {
i++ // skip "attribute" after "readonly"
}
} else {
i++ // skip "attribute"
}
if i < len(tokens) {
if i < len(tokens) {
attr.Type = tokens[i]
i++
}
if i < len(tokens) {
attr.Name = strings.TrimRight(tokens[i], ";")
i++
}
}
if attr.Name != "" {
iface.Attributes = append(iface.Attributes, attr)
}
case isIDLType(tok):
// Operation: returnType name(args...);
op := Operation{ReturnType: tok}
i++
if i < len(tokens) {
op.Name = tokens[i]
i++
}
if i < len(tokens) && tokens[i] == "(" {
i++
for i < len(tokens) && tokens[i] != ")" {
arg := Arg{}
if i < len(tokens) {
arg.Type = tokens[i]
i++
}
if i < len(tokens) && tokens[i] != ")" && tokens[i] != "," {
arg.Name = tokens[i]
i++
}
if arg.Type != "" {
op.Args = append(op.Args, arg)
}
if i < len(tokens) && tokens[i] == "," {
i++
}
}
if i < len(tokens) {
i++ // skip )
}
}
// Skip ;
if i < len(tokens) && tokens[i] == ";" {
i++
}
if op.Name != "" {
iface.Operations = append(iface.Operations, op)
}
default:
i++
}
}

return iface, nil
}

func tokenizeIDL(source string) []string {
var tokens []string
source = strings.ReplaceAll(source, ";", " ; ")
source = strings.ReplaceAll(source, "(", " ( ")
source = strings.ReplaceAll(source, ")", " ) ")
source = strings.ReplaceAll(source, ",", " , ")
source = strings.ReplaceAll(source, "{", " { ")
source = strings.ReplaceAll(source, "}", " } ")
source = strings.ReplaceAll(source, ":", " : ")
fields := strings.Fields(source)
for _, f := range fields {
if f == "{" || f == "}" {
continue
}
tokens = append(tokens, f)
}
return tokens
}

func stripComments(source string) string {
var result strings.Builder
inComment := false
for i := 0; i < len(source); i++ {
if i+1 < len(source) && source[i] == '/' && source[i+1] == '/' {
for i < len(source) && source[i] != '\n' {
i++
}
if i < len(source) {
result.WriteByte('\n')
}
continue
}
if i+1 < len(source) && source[i] == '/' && source[i+1] == '*' {
inComment = true
i++
continue
}
if inComment && i+1 < len(source) && source[i] == '*' && source[i+1] == '/' {
inComment = false
i++
continue
}
if !inComment {
result.WriteByte(source[i])
}
}
return result.String()
}

func isIDLType(s string) bool {
types := map[string]bool{
"DOMString": true, "boolean": true, "double": true, "float": true,
"long": true, "short": true, "unsigned": true, "void": true,
"any": true, "object": true, "sequence": true, "Promise": true,
"ByteString": true, "USVString": true,
"Element": true, "Document": true, "Node": true, "HTMLElement": true,
"Event": true, "DOMTokenList": true, "Attr": true, "NodeList": true,
"HTMLCollection": true, "DOMRect": true, "Text": true, "Comment": true,
}
// Also match type names starting with uppercase (interfaces).
if len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z' {
return true
}
return types[s]
}
