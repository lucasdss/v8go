// events.go — Event listener storage and dispatch.
//
// EventSystem manages DOM event listeners, the event queue, window.onload,
// and callbacks for DOM element lookup and mutation notification.
package js

import "github.com/lucasdss/v8go/pkg/dom"

// EventSystem stores event listeners and manages event dispatch.
// It is NOT goroutine-safe — the caller (VM.mu) provides synchronization.
type EventSystem struct {
	listeners         map[string][]JSValue
	eventQueue        []QueuedEvent
	onloadHandler     JSValue
	elementByID       func(string) *dom.Element
	domChangeCallback func()
}

// NewEventSystem creates an EventSystem with pre-allocated listener capacity.
func NewEventSystem() *EventSystem {
	return &EventSystem{
		listeners:  make(map[string][]JSValue, 4),
		eventQueue: make([]QueuedEvent, 0, 8),
	}
}

// AddListener registers a callback for the given event type.
func (es *EventSystem) AddListener(eventType string, callback JSValue) {
	if es.listeners == nil {
		es.listeners = make(map[string][]JSValue, 4)
	}
	es.listeners[eventType] = append(es.listeners[eventType], callback)
}

// GetListeners returns a copy of the listener list for the given event type.
func (es *EventSystem) GetListeners(eventType string) []JSValue {
	if es.listeners == nil {
		return nil
	}
	listCopy := make([]JSValue, len(es.listeners[eventType]))
	copy(listCopy, es.listeners[eventType])
	return listCopy
}

// QueueEvent adds an event to the deferred event queue.
func (es *EventSystem) QueueEvent(e QueuedEvent) {
	es.eventQueue = append(es.eventQueue, e)
}

// DequeueEvents drains and returns all queued events.
func (es *EventSystem) DequeueEvents() []QueuedEvent {
	q := es.eventQueue
	es.eventQueue = es.eventQueue[:0]
	return q
}

// SetOnload stores the window.onload callback.
func (es *EventSystem) SetOnload(callback JSValue) {
	es.onloadHandler = callback
}

// Onload returns the window.onload callback.
func (es *EventSystem) Onload() JSValue {
	return es.onloadHandler
}

// SetElementLookup registers a callback for looking up DOM elements by ID.
func (es *EventSystem) SetElementLookup(fn func(string) *dom.Element) {
	es.elementByID = fn
}

// LookupElement finds a DOM element by ID using the registered callback.
func (es *EventSystem) LookupElement(id string) *dom.Element {
	if es.elementByID == nil {
		return nil
	}
	return es.elementByID(id)
}

// SetDOMChangeCallback registers a callback invoked when the VM mutates the DOM.
func (es *EventSystem) SetDOMChangeCallback(fn func()) {
	es.domChangeCallback = fn
}

// DOMChangeCallback returns the current DOM change callback.
func (es *EventSystem) DOMChangeCallback() func() {
	return es.domChangeCallback
}
