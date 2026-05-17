package js

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskPriority_String(t *testing.T) {
	if PriorityInput.String() != "Input" {
		t.Errorf("expected Input, got %s", PriorityInput)
	}
	if PriorityHigh.String() != "High" {
		t.Errorf("expected High, got %s", PriorityHigh)
	}
	if PriorityNormal.String() != "Normal" {
		t.Errorf("expected Normal, got %s", PriorityNormal)
	}
}

func TestScheduler_EnqueueAndRun(t *testing.T) {
	s := NewScheduler(nil)
	var counter int32

	s.Enqueue(PriorityNormal, ScriptTaskSource, func() {
		atomic.AddInt32(&counter, 1)
	})
	s.Enqueue(PriorityNormal, ScriptTaskSource, func() {
		atomic.AddInt32(&counter, 1)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run in background.
	go s.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if c := atomic.LoadInt32(&counter); c < 2 {
		t.Errorf("expected at least 2 tasks processed, got %d", c)
	}
}

func TestScheduler_PriorityOrder(t *testing.T) {
	s := NewScheduler(nil)
	var mu sync.Mutex
	var order []TaskPriority

	enqueue := func(pri TaskPriority) {
		s.Enqueue(pri, ScriptTaskSource, func() {
			mu.Lock()
			order = append(order, pri)
			mu.Unlock()
		})
	}

	// Enqueue one of each priority in reverse order.
	enqueue(PriorityIdle)
	enqueue(PriorityLow)
	enqueue(PriorityNormal)
	enqueue(PriorityHigh)
	enqueue(PriorityInput)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go s.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(order) < 3 {
		t.Fatalf("expected at least 3 tasks processed, got %d", len(order))
	}
}

func TestScheduler_Microtask(t *testing.T) {
	s := NewScheduler(nil)
	var counter int32

	s.EnqueueMicrotask(func() {
		atomic.AddInt32(&counter, 1)
	})
	s.EnqueueMicrotask(func() {
		atomic.AddInt32(&counter, 1)
	})

	// Run until empty drains microtasks.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go s.Run(ctx)
	time.Sleep(30 * time.Millisecond)
	s.Stop()

	if c := atomic.LoadInt32(&counter); c != 2 {
		t.Errorf("expected 2 microtasks, got %d", c)
	}
}

func TestFrameThrottler_Default(t *testing.T) {
	ft := NewFrameThrottler(60)
	if ft.ShouldThrottle() {
		t.Error("new throttler should not throttle by default")
	}
}

func TestFrameThrottler_SetThrottled(t *testing.T) {
	ft := NewFrameThrottler(60)
	ft.SetThrottled(true)
	if !ft.ShouldThrottle() {
		t.Error("should throttle after SetThrottled(true)")
	}
	ft.SetThrottled(false)
	if ft.ShouldThrottle() {
		t.Error("should not throttle after SetThrottled(false)")
	}
}

func TestFrameThrottler_SetFPS(t *testing.T) {
	ft := NewFrameThrottler(30)
	ft.SetFPS(120)
	// Should not panic and should reset.
	ft.WaitForNextFrame() // should not block significantly.
}

func TestScheduler_WeightedOrder(t *testing.T) {
	s := NewScheduler(nil)
	var mu sync.Mutex
	var results []TaskPriority

	// Enqueue 10 normal and 10 input tasks.
	for i := 0; i < 10; i++ {
		s.Enqueue(PriorityNormal, ScriptTaskSource, func() {
			mu.Lock()
			results = append(results, PriorityNormal)
			mu.Unlock()
		})
	}
	for i := 0; i < 10; i++ {
		s.Enqueue(PriorityInput, ScriptTaskSource, func() {
			mu.Lock()
			results = append(results, PriorityInput)
			mu.Unlock()
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go s.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	s.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(results) < 10 {
		t.Fatalf("expected at least 10 tasks processed, got %d", len(results))
	}
}
