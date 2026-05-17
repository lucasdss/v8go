// Package bindings provides the DOM ↔ JavaScript binding framework, inspired
// by Blink's Web IDL code-generated wrapper layer. It bridges Go-side DOM
// objects with JS-side handles so that script code can manipulate the DOM.
//
// References:
//   - Blink bindings/ (V8 wrapper generation, DOMWrapper)
//   - Web IDL Standard: https://webidl.spec.whatwg.org/
package bindings

import (
	"fmt"
	"sync"

	"github.com/lucasdss/v8go/pkg/dom"
)

// JSObjectRef is an opaque handle to a JavaScript object managed by the JS
// engine. The binding layer stores these to keep DOM-backed JS wrappers alive.
type JSObjectRef uint64

// DOMWrapper pairs a Go-side DOM node with its corresponding JS-side handle.
// When the JS engine creates a wrapper for a DOM node, both the DOM node and
// the JSObjectRef are registered so that:
//  1. JS code can access DOM nodes through the wrapper.
//  2. The DOM side can dispatch events to JS listeners.
//  3. GC coordination can prevent collection of nodes reachable from JS.
type DOMWrapper struct {
	Node     *dom.Node
	JSHandle JSObjectRef
	// Properties is a cache of JS-exposed DOM properties (id, className, etc.).
	Properties map[string]interface{}
}

// BindingRegistry maps DOM element types to lists of wrapped DOM nodes.
// It also tracks constructor functions and accessor callbacks registered
// on the JS side.
type BindingRegistry struct {
	mu sync.RWMutex

	// byNode maps a DOM node to its wrapper.
	byNode map[*dom.Node]*DOMWrapper
	// byHandle maps a JS object ref to its DOM wrapper.
	byHandle map[JSObjectRef]*DOMWrapper
	// nextHandle is a monotonically increasing handle counter.
	nextHandle JSObjectRef

	// elementBindings maps DOM element type names to JS constructor functions
	// and property accessors registered on the engine.
	elementBindings map[string]*ElementBinding
}

// ElementBinding holds the registered JS-side callbacks for a DOM element type.
type ElementBinding struct {
	TagName        string
	Constructor    func(tagName string) JSObjectRef
	GetAttribute   func(handle JSObjectRef, name string) string
	SetAttribute   func(handle JSObjectRef, name, value string)
	GetTextContent func(handle JSObjectRef) string
	SetTextContent func(handle JSObjectRef, text string)
	AddChild       func(parentHandle, childHandle JSObjectRef)
}

// NewBindingRegistry creates an empty binding registry.
func NewBindingRegistry() *BindingRegistry {
	return &BindingRegistry{
		byNode:          make(map[*dom.Node]*DOMWrapper),
		byHandle:        make(map[JSObjectRef]*DOMWrapper),
		elementBindings: make(map[string]*ElementBinding),
	}
}

// RegisterNode creates a DOMWrapper for a DOM node and assigns it a JS handle.
// Returns the wrapper so the caller can set properties on it.
func (r *BindingRegistry) RegisterNode(node *dom.Node) *DOMWrapper {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.byNode[node]; ok {
		return existing
	}

	r.nextHandle++
	w := &DOMWrapper{
		Node:       node,
		JSHandle:   r.nextHandle,
		Properties: make(map[string]interface{}),
	}
	r.byNode[node] = w
	r.byHandle[w.JSHandle] = w
	return w
}

// UnregisterNode removes a DOM node from the registry.
func (r *BindingRegistry) UnregisterNode(node *dom.Node) {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.byNode[node]
	if !ok {
		return
	}
	delete(r.byHandle, w.JSHandle)
	delete(r.byNode, node)
}

// LookupByNode returns the wrapper for a DOM node, or nil.
func (r *BindingRegistry) LookupByNode(node *dom.Node) *DOMWrapper {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byNode[node]
}

// LookupByHandle returns the wrapper for a JS object ref, or nil.
func (r *BindingRegistry) LookupByHandle(ref JSObjectRef) *DOMWrapper {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byHandle[ref]
}

// RegisterElementBinding stores the JS-side callbacks for an element type.
func (r *BindingRegistry) RegisterElementBinding(tagName string, binding *ElementBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.elementBindings[tagName] = binding
}

// GetElementBinding returns the binding for a tag, or nil.
func (r *BindingRegistry) GetElementBinding(tagName string) *ElementBinding {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.elementBindings[tagName]
}

// Count returns the number of registered DOM wrappers.
func (r *BindingRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byNode)
}

// HandleCount returns the number of JS handles tracked.
func (r *BindingRegistry) HandleCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byHandle)
}

// ----------------------------- BindDocument -----------------------------------

// BindDocument recursively registers all DOM nodes in the document with the
// binding registry, assigning JS object refs so that JavaScript can reference
// every DOM node.
//
// This is called after HTML parsing completes and before any scripts execute.
func BindDocument(doc *dom.Document, registry *BindingRegistry) {
	if doc == nil {
		return
	}
	bindNode(&doc.Node, registry)
}

func bindNode(node *dom.Node, registry *BindingRegistry) {
	if node == nil {
		return
	}
	registry.RegisterNode(node)
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		bindNode(child, registry)
	}
}

// ----------------------------- String conversion ------------------------------

func (r JSObjectRef) String() string {
	return fmt.Sprintf("JSObjectRef(%d)", uint64(r))
}
