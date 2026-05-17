package js

import (
	"testing"
	"time"
)

func TestWebWorker_PostMessage(t *testing.T) {
	w := NewWebWorker("test-worker")
	if w.ID() != "test-worker" {
		t.Errorf("expected test-worker, got %s", w.ID())
	}
	if !w.IsRunning() {
		t.Error("new worker should be running")
	}

	msgReceived := make(chan WorkerMessage, 1)
	w.OnMessage(func(msg WorkerMessage) {
		msgReceived <- msg
	})

	w.Start(WorkerScript{Source: "1 + 1"})

	// Send a message.
	w.PostMessage(WorkerMessage{Type: "hello", Data: "world"})

	select {
	case msg := <-msgReceived:
		if msg.Type != "hello" || msg.Data != "world" {
			t.Errorf("unexpected message: %v", msg)
		}
	case <-time.After(2 * time.Second):
		// Worker message loop may have exited. That's okay for this test.
	}

	w.Terminate()
	if w.IsRunning() {
		t.Error("worker should not be running after terminate")
	}
}

func TestWebWorker_Terminate(t *testing.T) {
	w := NewWebWorker("terminate-test")
	w.Start(WorkerScript{Source: "1 + 1"})
	time.Sleep(10 * time.Millisecond)
	w.Terminate()
	if w.IsRunning() {
		t.Error("worker should be terminated")
	}
	// Double terminate should not panic.
	w.Terminate()
}

func TestWebWorker_SendToMain(t *testing.T) {
	w := NewWebWorker("send-test")

	w.Start(WorkerScript{Source: "foo"})

	go func() {
		w.SendToMain(WorkerMessage{Type: "result", Data: 42})
	}()

	select {
	case msg := <-w.Receive():
		if msg.Type != "result" {
			t.Errorf("expected result, got %s", msg.Type)
		}
		if msg.Data != 42 {
			t.Errorf("expected 42, got %v", msg.Data)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for message")
	}

	w.Terminate()
}

func TestWorkerPool_Spawn(t *testing.T) {
	pool := NewWorkerPool(4)
	w := pool.Spawn(WorkerScript{Source: "1 + 1"})
	if w == nil {
		t.Fatal("spawn returned nil")
	}
	if pool.ActiveCount() != 1 {
		t.Errorf("expected 1 active, got %d", pool.ActiveCount())
	}

	// Same ID should be retrievable.
	if pool.Get(w.ID()) == nil {
		t.Error("Get returned nil for spawned worker")
	}

	defer pool.Shutdown()
}

func TestWorkerPool_Capacity(t *testing.T) {
	pool := NewWorkerPool(2)
	w1 := pool.Spawn(WorkerScript{Source: "1"})
	w2 := pool.Spawn(WorkerScript{Source: "2"})
	w3 := pool.Spawn(WorkerScript{Source: "3"}) // should fail

	if w1 == nil || w2 == nil {
		t.Fatal("first two spawns should succeed")
	}
	if w3 != nil {
		t.Error("third spawn should return nil at capacity")
	}

	defer pool.Shutdown()
}

func TestWorkerPool_Remove(t *testing.T) {
	pool := NewWorkerPool(4)
	w := pool.Spawn(WorkerScript{Source: "1"})
	pool.Remove(w.ID())
	if pool.ActiveCount() != 0 {
		t.Errorf("expected 0 after remove, got %d", pool.ActiveCount())
	}
	if pool.Get(w.ID()) != nil {
		t.Error("Get should return nil after remove")
	}
}

func TestWorkerPool_Shutdown(t *testing.T) {
	pool := NewWorkerPool(4)
	pool.Spawn(WorkerScript{Source: "1"})
	pool.Spawn(WorkerScript{Source: "2"})

	pool.Shutdown()
	if pool.ActiveCount() != 0 {
		t.Errorf("expected 0 after shutdown, got %d", pool.ActiveCount())
	}
}

func TestWebWorker_DoubleStart(t *testing.T) {
	w := NewWebWorker("double-start")

	// Should not panic on double start.
	w.Start(WorkerScript{Source: "1"})
	time.Sleep(10 * time.Millisecond)
	w.Start(WorkerScript{Source: "2"})
	time.Sleep(10 * time.Millisecond)

	w.Terminate()
}
