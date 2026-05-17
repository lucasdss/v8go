package js_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasdss/v8go/pkg/js"
)

// ─────────────────────────── EventLoop tests ──────────────────────────────

func TestEventLoop_EnqueueAndRunTask(t *testing.T) {
	el := js.NewEventLoop()
	var ran int32
	el.EnqueueTask(js.DOMManipulationTaskSource, func() {
		atomic.StoreInt32(&ran, 1)
	})
	el.RunUntilEmpty()
	if atomic.LoadInt32(&ran) != 1 {
		t.Error("task was not executed")
	}
}

func TestEventLoop_EnqueueMicrotask(t *testing.T) {
	el := js.NewEventLoop()
	var ran int32
	el.EnqueueMicrotask(func() {
		atomic.StoreInt32(&ran, 1)
	})
	el.RunUntilEmpty()
	if atomic.LoadInt32(&ran) != 1 {
		t.Error("microtask was not executed")
	}
}

func TestEventLoop_MicrotaskRunsBeforeNextTask(t *testing.T) {
	el := js.NewEventLoop()
	var order []string

	el.EnqueueTask(js.ScriptTaskSource, func() {
		order = append(order, "task1")
		el.EnqueueMicrotask(func() {
			order = append(order, "microtask")
		})
	})
	el.EnqueueTask(js.ScriptTaskSource, func() {
		order = append(order, "task2")
	})

	el.RunUntilEmpty()

	// After task1 runs, the microtask checkpoint should fire before task2.
	if len(order) != 3 {
		t.Fatalf("expected 3 steps, got %v", order)
	}
	if order[0] != "task1" || order[1] != "microtask" || order[2] != "task2" {
		t.Errorf("wrong order: %v", order)
	}
}

func TestEventLoop_MultipleSources(t *testing.T) {
	el := js.NewEventLoop()
	var count int32
	for _, src := range []js.TaskSource{
		js.DOMManipulationTaskSource,
		js.UserInteractionTaskSource,
		js.NetworkingTaskSource,
		js.ScriptTaskSource,
	} {
		s := src
		el.EnqueueTask(s, func() {
			atomic.AddInt32(&count, 1)
		})
	}
	el.RunUntilEmpty()
	if atomic.LoadInt32(&count) != 4 {
		t.Errorf("expected 4 tasks to run, ran %d", count)
	}
}

func TestEventLoop_TaskPanicRecovery(t *testing.T) {
	el := js.NewEventLoop()
	var errCaught int32
	el.SetErrorHandler(func(_ error) {
		atomic.StoreInt32(&errCaught, 1)
	})
	el.EnqueueTask(js.ScriptTaskSource, func() {
		panic("intentional panic")
	})
	// Should not panic the test itself.
	el.RunUntilEmpty()
	if atomic.LoadInt32(&errCaught) != 1 {
		t.Error("expected error handler to be called on task panic")
	}
}

func TestEventLoop_SetTimeout(t *testing.T) {
	el := js.NewEventLoop()
	var ran int32
	el.SetTimeout(func() {
		atomic.StoreInt32(&ran, 1)
	}, 0) // zero delay

	el.RunUntilEmpty()
	if atomic.LoadInt32(&ran) != 1 {
		t.Error("setTimeout callback was not called")
	}
}

func TestEventLoop_ClearTimeout(t *testing.T) {
	el := js.NewEventLoop()
	var ran int32
	id := el.SetTimeout(func() {
		atomic.StoreInt32(&ran, 1)
	}, 10*time.Millisecond)
	el.ClearTimeout(id)
	el.RunUntilEmpty()
	if atomic.LoadInt32(&ran) != 0 {
		t.Error("cleared timeout should not have fired")
	}
}

func TestEventLoop_Run_Stops_OnContextCancel(t *testing.T) {
	el := js.NewEventLoop()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		el.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Good: Run returned after context cancellation.
	case <-time.After(500 * time.Millisecond):
		t.Error("Run did not return after context cancellation")
	}
}

func TestEventLoop_Stop(t *testing.T) {
	el := js.NewEventLoop()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		el.Run(ctx)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	el.Stop()

	select {
	case <-done:
		// Good.
	case <-time.After(500 * time.Millisecond):
		t.Error("Run did not stop after Stop()")
	}
}

func TestEventLoop_RunNotReentrant(t *testing.T) {
	// Calling Run twice on the same loop; the second call should return immediately.
	el := js.NewEventLoop()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go el.Run(ctx)
	time.Sleep(5 * time.Millisecond)
	// Second call – should not block indefinitely.
	done := make(chan struct{})
	go func() {
		el.Run(ctx) // should return quickly since loop is already running
		close(done)
	}()
	select {
	case <-done:
		// Good.
	case <-time.After(200 * time.Millisecond):
		t.Error("second Run call blocked")
	}
}

// ─────────────────────────── Agent tests ──────────────────────────────────

func TestAgent_NewAgent(t *testing.T) {
	agent := js.NewAgent()
	if agent.Loop == nil {
		t.Error("agent should have a non-nil event loop")
	}
}

func TestAgent_QueueScript(_ *testing.T) {
	agent := js.NewAgent()
	script := &js.Script{
		Type:   js.ClassicScript,
		Source: `console.log("hello");`,
	}
	// Should not panic.
	agent.QueueScript(script)
	agent.Loop.RunUntilEmpty()
}
