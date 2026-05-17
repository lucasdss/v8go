package dom

import (
	"testing"
)

func TestNewEvent(t *testing.T) {
	ev := NewEvent("click", true, true)
	if ev.Type != "click" {
		t.Errorf("expected click, got %s", ev.Type)
	}
	if !ev.Bubbles {
		t.Error("expected bubbles true")
	}
	if !ev.Cancelable {
		t.Error("expected cancelable true")
	}
	if !ev.IsTrusted {
		t.Error("expected trusted event")
	}
}

func TestEvent_StopPropagation(t *testing.T) {
	ev := NewEvent("click", true, true)
	ev.StopPropagation()
	if !ev.IsPropagationStopped() {
		t.Error("expected propagation stopped")
	}
}

func TestEvent_StopImmediatePropagation(t *testing.T) {
	ev := NewEvent("click", true, true)
	ev.StopImmediatePropagation()
	if !ev.IsImmediatePropagationStopped() {
		t.Error("expected immediate propagation stopped")
	}
	if !ev.IsPropagationStopped() {
		t.Error("immediate also stops propagation")
	}
}

func TestEvent_PreventDefault(t *testing.T) {
	ev := NewEvent("click", true, true)
	ev.PreventDefault()
	if !ev.DefaultPrevented {
		t.Error("expected default prevented")
	}
}

func TestEvent_PreventDefaultNonCancelable(t *testing.T) {
	ev := NewEvent("click", true, false)
	ev.PreventDefault()
	if ev.DefaultPrevented {
		t.Error("non-cancelable event should not prevent default")
	}
}

func TestAddEventListener(t *testing.T) {
	doc := NewDocument()
	el := NewElement("div", doc)
	called := false

	el.AddEventListener("click", func(e *Event) {
		called = true
	})

	ev := NewEvent("click", true, true)
	el.DispatchEvent(ev)

	if !called {
		t.Error("event listener was not called")
	}
}

func TestRemoveEventListener(t *testing.T) {
	doc := NewDocument()
	el := NewElement("div", doc)
	called := false

	listener := func(e *Event) {
		called = true
	}

	el.AddEventListener("click", listener)
	el.RemoveEventListener("click", listener)

	ev := NewEvent("click", true, true)
	el.DispatchEvent(ev)

	if called {
		t.Error("listener should have been removed")
	}
}

func TestEvent_Bubbling(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)
	parent.AppendChild(&child.Node)

	var callOrder []string

	parent.AddEventListener("click", func(e *Event) {
		callOrder = append(callOrder, "parent")
	})
	child.AddEventListener("click", func(e *Event) {
		callOrder = append(callOrder, "child")
	})

	ev := NewEvent("click", true, true)
	child.DispatchEvent(ev)

	if len(callOrder) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(callOrder))
	}
	if callOrder[0] != "child" || callOrder[1] != "parent" {
		t.Errorf("expected child→parent order, got %v", callOrder)
	}
}

func TestEvent_BubblingStopped(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)
	parent.AppendChild(&child.Node)

	parentCalled := false

	child.AddEventListener("click", func(e *Event) {
		e.StopPropagation()
	})
	parent.AddEventListener("click", func(e *Event) {
		parentCalled = true
	})

	ev := NewEvent("click", true, true)
	child.DispatchEvent(ev)

	if parentCalled {
		t.Error("parent should not be called when propagation stopped")
	}
}

func TestEvent_NonBubbling(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)
	parent.AppendChild(&child.Node)

	parentCalled := false
	childCalled := false

	parent.AddEventListener("focus", func(e *Event) {
		parentCalled = true
	})
	child.AddEventListener("focus", func(e *Event) {
		childCalled = true
	})

	ev := NewEvent("focus", false, false)
	child.DispatchEvent(ev)

	if !childCalled {
		t.Error("child should be called for non-bubbling event")
	}
	if parentCalled {
		t.Error("parent should not receive non-bubbling event")
	}
}

func TestEvent_CapturePhase(t *testing.T) {
	doc := NewDocument()
	parent := NewElement("div", doc)
	child := NewElement("span", doc)
	parent.AppendChild(&child.Node)

	var callOrder []string

	parent.AddEventListener("click", func(e *Event) {
		callOrder = append(callOrder, "parent-capture")
	}, true) // capture phase
	child.AddEventListener("click", func(e *Event) {
		callOrder = append(callOrder, "child")
	})

	ev := NewEvent("click", true, true)
	child.DispatchEvent(ev)

	if len(callOrder) < 2 {
		t.Fatalf("expected at least 2 calls, got %d: %v", len(callOrder), callOrder)
	}
	if callOrder[0] != "parent-capture" {
		t.Errorf("capture should fire first, got %v", callOrder)
	}
}

func TestEvent_Once(t *testing.T) {
	doc := NewDocument()
	el := NewElement("div", doc)
	count := 0

	el.AddEventListener("click", func(e *Event) {
		count++
	}, false, true) // once

	el.DispatchEvent(NewEvent("click", true, true))
	el.DispatchEvent(NewEvent("click", true, true))

	if count != 1 {
		t.Errorf("once listener should fire exactly once, got %d", count)
	}
}

func TestEvent_DispatchReturnValue(t *testing.T) {
	doc := NewDocument()
	el := NewElement("div", doc)

	el.AddEventListener("click", func(e *Event) {
		e.PreventDefault()
	})

	result := el.DispatchEvent(NewEvent("click", true, true))
	if result {
		t.Error("dispatch should return false when preventDefault called")
	}

	result = el.DispatchEvent(NewEvent("click", true, false))
	if !result {
		t.Error("non-cancelable event dispatch should return true")
	}
}
