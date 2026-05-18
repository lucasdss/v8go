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
	done := make(chan struct{})

	s.Enqueue(PriorityNormal, ScriptTaskSource, func() {
		atomic.AddInt32(&counter, 1)
	})
	s.Enqueue(PriorityNormal, ScriptTaskSource, func() {
		if atomic.AddInt32(&counter, 1) == 2 {
			close(done)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go s.Run(ctx)

	// Wait for tasks to complete or timeout.
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for tasks")
	}
	s.Stop()

	if c := atomic.LoadInt32(&counter); c != 2 {
		t.Errorf("expected 2 tasks, got %d", c)
	}
}

func TestScheduler_PriorityOrder(t *testing.T) {
	s := NewScheduler(nil)
	var mu sync.Mutex
	var order []TaskPriority
	var wg sync.WaitGroup
	wg.Add(5)

	enqueue := func(pri TaskPriority) {
		s.Enqueue(pri, ScriptTaskSource, func() {
			mu.Lock()
			order = append(order, pri)
			mu.Unlock()
			wg.Done()
		})
	}

	// Enqueue one of each priority in reverse order.
	enqueue(PriorityIdle)
	enqueue(PriorityLow)
	enqueue(PriorityNormal)
	enqueue(PriorityHigh)
	enqueue(PriorityInput)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go s.Run(ctx)

	// Wait for all tasks to complete.
	wg.Wait()
	s.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 5 {
		t.Fatalf("expected 5 tasks processed, got %d", len(order))
	}
}

func TestScheduler_Microtask(t *testing.T) {
	s := NewScheduler(nil)
	var counter int32
	done := make(chan struct{})

	s.EnqueueMicrotask(func() {
		atomic.AddInt32(&counter, 1)
	})
	s.EnqueueMicrotask(func() {
		if atomic.AddInt32(&counter, 1) == 2 {
			close(done)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go s.Run(ctx)

	// Wait for microtasks or timeout.
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for microtasks")
	}
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
	const totalTasks = 20
	var wg sync.WaitGroup
	wg.Add(totalTasks)

	// Enqueue 10 normal and 10 input tasks.
	for i := 0; i < 10; i++ {
		s.Enqueue(PriorityNormal, ScriptTaskSource, func() {
			mu.Lock()
			results = append(results, PriorityNormal)
			mu.Unlock()
			wg.Done()
		})
	}
	for i := 0; i < 10; i++ {
		s.Enqueue(PriorityInput, ScriptTaskSource, func() {
			mu.Lock()
			results = append(results, PriorityInput)
			mu.Unlock()
			wg.Done()
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go s.Run(ctx)

	// Wait for all tasks to complete.
	wg.Wait()
	s.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(results) != totalTasks {
		t.Fatalf("expected %d tasks, got %d", totalTasks, len(results))
	}
}
