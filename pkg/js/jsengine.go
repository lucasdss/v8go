//go:build qjs

// jsengine.go — shared interface for JavaScript engines in the browser.
//
// Both the QuickJS-based Engine and the V8Go-based Gov8Engine implement
// this interface, allowing the browser to swap between them seamlessly.
package js

import "github.com/lucasdss/v8go/pkg/dom"

// JSEngine is the interface that both JS engine implementations satisfy.
// Used by the browser to execute scripts without coupling to a specific engine.
type JSEngine interface {
	Execute(source string) error
	SetConsoleLogger(fn, warnFn, errFn func(args ...any))
	SetDocument(doc *dom.Document)
	SetDocumentBinder(title string, lookup func(id string) any)
	SetLocation(url string)
	SetDOMHeap(heap *dom.DOMHeap)
	// SetDOMChangeCallback registers a callback invoked when JS mutates the DOM.
	// The browser uses this to schedule a repaint on the next frame.
	SetDOMChangeCallback(fn func())
	// SetStyleTransitionHook registers a callback invoked when JS sets a style
	// property via element.style.setProperty(). The browser uses this to
	// trigger CSS transitions: it receives the element, property name, old
	// value (before the set), and new value.
	SetStyleTransitionHook(fn func(el *dom.Element, prop, oldVal, newVal string))
	CollectGarbage()
	Close()
}
