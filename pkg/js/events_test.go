// events_test.go — Internal tests for EventSystem (queue, element lookup, DOM callback).
package js

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/dom"
)

func TestEventSystem_QueueDequeue(t *testing.T) {
	es := NewEventSystem()

	es.QueueEvent(QueuedEvent{Type: "click", Target: "btn1"})
	es.QueueEvent(QueuedEvent{Type: "load", Target: "window"})

	events := es.DequeueEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "click" {
		t.Errorf("first event type: expected 'click', got %q", events[0].Type)
	}
	if events[0].Target != "btn1" {
		t.Errorf("first event target: expected 'btn1', got %q", events[0].Target)
	}
	if events[1].Type != "load" {
		t.Errorf("second event type: expected 'load', got %q", events[1].Type)
	}

	// Queue should be drained after dequeue.
	remaining := es.DequeueEvents()
	if len(remaining) != 0 {
		t.Errorf("queue should be empty after dequeue, got %d events", len(remaining))
	}
}

func TestEventSystem_DequeueEmpty(t *testing.T) {
	es := NewEventSystem()
	events := es.DequeueEvents()
	if events == nil {
		t.Error("DequeueEvents on empty queue should return empty slice, not nil")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events from empty queue, got %d", len(events))
	}
}

func TestEventSystem_SetElementLookup(t *testing.T) {
	es := NewEventSystem()
	doc := dom.NewDocument()
	expected := dom.NewElement("div", doc)

	es.SetElementLookup(func(id string) *dom.Element {
		if id == "test" {
			return expected
		}
		return nil
	})

	elem := es.LookupElement("test")
	if elem != expected {
		t.Error("LookupElement should return the registered element")
	}

	missing := es.LookupElement("nonexistent")
	if missing != nil {
		t.Error("LookupElement should return nil for unknown ids")
	}
}

func TestEventSystem_LookupElementNilCallback(t *testing.T) {
	es := NewEventSystem()
	elem := es.LookupElement("anything")
	if elem != nil {
		t.Error("LookupElement with nil callback should return nil")
	}
}

func TestEventSystem_SetDOMChangeCallback(t *testing.T) {
	es := NewEventSystem()
	called := false
	es.SetDOMChangeCallback(func() { called = true })

	cb := es.DOMChangeCallback()
	if cb == nil {
		t.Fatal("DOMChangeCallback should return non-nil after SetDOMChangeCallback")
	}
	cb()
	if !called {
		t.Error("DOMChangeCallback should have been called")
	}
}

func TestEventSystem_DOMChangeCallbackNilDefault(t *testing.T) {
	es := NewEventSystem()
	cb := es.DOMChangeCallback()
	if cb != nil {
		t.Error("DOMChangeCallback should return nil by default")
	}
}

// TestGoDispatchEventBuiltin exercises the __goDispatchEvent builtin directly
// (bypassing vm.Run which holds the write lock and would deadlock with
// DispatchEvent's RLock).
func TestGoDispatchEventBuiltin(t *testing.T) {
	vm := NewVM()

	doc := dom.NewDocument()
	btn := dom.NewElement("button", doc)
	btn.OnClick = "__builtin_test_ok = true"

	vm.SetElementLookup(func(id string) *dom.Element {
		if id == "btn" {
			return btn
		}
		return nil
	})

	builtin := vm.registry.Builtins["__goDispatchEvent"]
	if builtin == nil {
		t.Fatal("__goDispatchEvent builtin not registered")
	}

	result := builtin([]JSValue{NewString("btn"), NewString("click")})
	if !result.IsTruthy() {
		t.Error("__goDispatchEvent should return true")
	}

	// Verify the handler was executed via the builtin.
	clicked := vm.Run("typeof __builtin_test_ok !== 'undefined' && __builtin_test_ok")
	if !clicked.IsTruthy() {
		t.Error("onclick handler should have set __builtin_test_ok to true")
	}
}

func TestGoDispatchEventBuiltinInsufficientArgs(t *testing.T) {
	vm := NewVM()
	builtin := vm.registry.Builtins["__goDispatchEvent"]
	if builtin == nil {
		t.Fatal("__goDispatchEvent builtin not registered")
	}
	result := builtin([]JSValue{NewString("onlyOneArg")})
	if result.IsTruthy() {
		t.Error("__goDispatchEvent with 1 arg should return false")
	}
}
