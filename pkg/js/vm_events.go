// vm_events.go — DOM event dispatch and lifecycle handlers.
package js

import (
	"github.com/lucasdss/v8go/pkg/dom"
)

// --- Event handling ---

// AddEventListener registers a callback for the given event type.
// Callbacks are stored in the EventSystem and invoked by FireEvent.
func (vm *VM) AddEventListener(eventType string, callback JSValue) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.events.AddListener(eventType, callback)
}

// FireEvent dispatches an event to all registered listeners for eventType.
// Additionally, for "load" events, the window.onload handler is invoked.
// Called by the browser after DOM parse (DOMContentLoaded) and after
// subresource loading (load), per the HTML Standard § 8.1.7 "Event loops".
func (vm *VM) FireEvent(eventType string, data map[string]JSValue) {
	vm.mu.Lock()
	// Snapshot listeners to avoid holding lock during callback execution.
	callbacks := vm.events.GetListeners(eventType)
	onloadCB := vm.events.Onload()
	vm.mu.Unlock()

	// Fire registered addEventListener callbacks.
	for _, cb := range callbacks {
		if cb.IsObject() && cb.ObjVal != nil && cb.ObjVal.isCallable() {
			// Build event object with type and data.
			eventObj := NewJSObject()
			eventObj.Set("type", NewString(eventType))
			if data != nil {
				for k, v := range data {
					eventObj.Set(k, v)
				}
			}
			cb.ObjVal.Call(nil, []JSValue{NewObject(eventObj)})
		}
	}

	// For "load" events, also fire window.onload if set.
	if eventType == "load" && onloadCB.IsObject() && onloadCB.ObjVal != nil && onloadCB.ObjVal.isCallable() {
		eventObj := NewJSObject()
		eventObj.Set("type", NewString("load"))
		onloadCB.ObjVal.Call(nil, []JSValue{NewObject(eventObj)})
	}
}

// SetOnloadHandler stores the window.onload callback (set via property setter).
func (vm *VM) SetOnloadHandler(callback JSValue) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.events.SetOnload(callback)
}

// GetOnloadHandler returns the current window.onload callback.
func (vm *VM) GetOnloadHandler() JSValue {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.events.Onload()
}

// SetElementLookup registers a callback for looking up DOM elements by ID.
// Called by the browser/Gov8Engine to bridge VM event dispatch with the DOM tree.
func (vm *VM) SetElementLookup(fn func(string) *dom.Element) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.events.SetElementLookup(fn)
}

// SetDOMChangeCallback registers a callback invoked when the VM executes an
// inline event handler that may have mutated the DOM (triggering a repaint).
func (vm *VM) SetDOMChangeCallback(fn func()) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.events.SetDOMChangeCallback(fn)
}

// DispatchEvent finds an element by targetID, reads its inline event handler
// for eventType (e.g., "click" → onclick attribute), and executes the handler
// code in the VM with a synthetic event object { type, target, preventDefault }.
func (vm *VM) DispatchEvent(targetID, eventType string, eventData map[string]JSValue) {
	vm.mu.Lock()
	elem := vm.events.LookupElement(targetID)
	vm.mu.Unlock()
	if elem == nil {
		return
	}

	handlerCode := elem.GetEventHandler(eventType)
	if handlerCode == "" {
		return
	}

	// Build event object visible to the handler code.
	// The event object has: type, target (element ID), preventDefault().
	eventObj := NewJSObject()
	eventObj.Set("type", NewString(eventType))
	eventObj.Set("target", NewString(targetID))
	if eventData != nil {
		for k, v := range eventData {
			eventObj.Set(k, v)
		}
	}
	defaultPrevented := false
	eventObj.Set("preventDefault", NewObject(builtinFunc("preventDefault", func(this *JSObject, args []JSValue) JSValue {
		defaultPrevented = true
		return Undefined
	})))

	// Store event object as a global for handler code to access.
	vm.mu.Lock()
	vm.globals.Set("__event__", NewObject(eventObj))
	vm.mu.Unlock()

	// Run the handler code in the VM.
	_ = vm.Run(handlerCode)

	// If preventDefault was called, notify the browser via a flag on the VM.
	// The caller can check via event return, but for now the handler runs in-process.
	_ = defaultPrevented

	// Trigger DOM change callback after inline handler execution (may have mutated DOM).
	vm.mu.Lock()
	cb := vm.events.DOMChangeCallback()
	vm.mu.Unlock()
	if cb != nil {
		cb()
	}
}
