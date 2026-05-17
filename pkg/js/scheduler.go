// Package js implements the browser's scripting infrastructure, including the
// event loop, task queues, and microtask checkpoint.
//
// This file adds Blink-inspired task scheduling with priority-based
// weighted round-robin. See Blink platform/scheduler/ for the C++ design.
package js

import (
	"context"
	"sync"
	"time"
)

// ----------------------------- Task Priority ----------------------------------

// TaskPriority assigns a priority level to tasks, mirroring Blink's
// prioritization: Input (highest), High, Normal, Low, Idle (lowest).
type TaskPriority int

const (
	// PriorityInput is for user input events (clicks, keypress) — must run ASAP.
	PriorityInput TaskPriority = iota
	// PriorityHigh is for critical rendering tasks and compositing.
	PriorityHigh
	// PriorityNormal is the default for most DOM and network tasks.
	PriorityNormal
	// PriorityLow is for non-critical work like analytics.
	PriorityLow
	// PriorityIdle is for idle-callbacks and background work.
	PriorityIdle
)

// String implements fmt.Stringer.
func (p TaskPriority) String() string {
	switch p {
	case PriorityInput:
		return "Input"
	case PriorityHigh:
		return "High"
	case PriorityNormal:
		return "Normal"
	case PriorityLow:
		return "Low"
	case PriorityIdle:
		return "Idle"
	default:
		return "Unknown"
	}
}

// ----------------------------- Weighted Queue --------------------------------

// weightedQueue holds tasks for a single priority level.
type weightedQueue struct {
	tasks []*Task
	// weight controls how many tasks this queue can dequeue per round.
	weight int
}

// ----------------------------- Scheduler -------------------------------------

// Scheduler wraps the EventLoop with priority-based weighted round-robin
// scheduling. Higher-priority queues get more execution weight, mimicking
// Blink's MainThreadScheduler.
type Scheduler struct {
	mu     sync.Mutex
	queues map[TaskPriority]*weightedQueue
	loop   *EventLoop

	// totalTasks tracks the total number enqueued for starvation detection.
	totalTasks uint64
	// normalBudget is the maximum number of Normal tasks before yielding to
	// higher priority.
	normalBudget int
}

// NewScheduler creates a Scheduler backed by an existing EventLoop.
// If loop is nil, a new one is created.
func NewScheduler(loop *EventLoop) *Scheduler {
	if loop == nil {
		loop = NewEventLoop()
	}
	s := &Scheduler{
		queues: make(map[TaskPriority]*weightedQueue),
		loop:   loop,
	}
	// Set default weights: Input → 8, High → 4, Normal → 2, Low → 1, Idle → 1.
	s.queues[PriorityInput]  = &weightedQueue{weight: 8}
	s.queues[PriorityHigh]   = &weightedQueue{weight: 4}
	s.queues[PriorityNormal] = &weightedQueue{weight: 2}
	s.queues[PriorityLow]    = &weightedQueue{weight: 1}
	s.queues[PriorityIdle]   = &weightedQueue{weight: 1}
	return s
}

// Enqueue schedules a task with the given priority and source.
func (s *Scheduler) Enqueue(priority TaskPriority, source TaskSource, steps func()) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalTasks++
	t := &Task{Source: source, Steps: steps, ID: s.totalTasks}
	q := s.queues[priority]
	q.tasks = append(q.tasks, t)
	return t
}

// EnqueueMicrotask delegates to the underlying event loop.
func (s *Scheduler) EnqueueMicrotask(steps func()) {
	s.loop.EnqueueMicrotask(steps)
}

// Run starts the scheduler loop. It blocks until ctx is cancelled.
// This follows the weighted round-robin algorithm:
// each priority level's queue can dequeue up to `weight` tasks per round.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(4 * time.Millisecond)
	defer ticker.Stop()

	priorities := []TaskPriority{
		PriorityInput, PriorityHigh, PriorityNormal, PriorityLow, PriorityIdle,
	}
	// Per-round counters: how many from each priority have been processed.
	counters := make(map[TaskPriority]int, len(priorities))

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.loop.done:
			return
		case <-ticker.C:
			anyProcessed := false
			for _, pri := range priorities {
				s.mu.Lock()
				q := s.queues[pri]
				weight := q.weight
				// Reset counter if we've exhausted the weight budget.
				if counters[pri] >= weight {
					counters[pri] = 0
				}
				// Dequeue up to (weight - already processed) tasks.
				toProcess := weight - counters[pri]
				if toProcess > len(q.tasks) {
					toProcess = len(q.tasks)
				}
				var tasks []*Task
				if toProcess > 0 {
					tasks = make([]*Task, toProcess)
					copy(tasks, q.tasks[:toProcess])
					q.tasks = q.tasks[toProcess:]
				}
				s.mu.Unlock()

				for _, t := range tasks {
					s.loop.safeRun(t.Steps)
					counters[pri]++
					anyProcessed = true
					// Perform microtask checkpoint after each task per spec.
					s.loop.performMicrotaskCheckpoint()
				}
			}
			// Always drain microtasks each tick, even if no regular tasks ran.
			if !anyProcessed {
				s.loop.performMicrotaskCheckpoint()
			}
			// Fire expired timers.
			s.loop.processTimers()
		}
	}
}

// Stop signals the scheduler to stop.
func (s *Scheduler) Stop() {
	s.loop.Stop()
}

// Loop returns the underlying event loop.
func (s *Scheduler) Loop() *EventLoop { return s.loop }

// ----------------------------- FrameThrottler --------------------------------

// FrameThrottler rate-limits rendering to a target frame rate (default 60fps).
// Background tabs can be throttled to lower rates.
type FrameThrottler struct {
	targetFPS  int
	minFrameMs time.Duration
	lastFrame  time.Time
	mu         sync.Mutex
	throttled  bool
}

// NewFrameThrottler creates a throttler targeting the given FPS.
func NewFrameThrottler(fps int) *FrameThrottler {
	return &FrameThrottler{
		targetFPS:  fps,
		minFrameMs: time.Second / time.Duration(fps),
	}
}

// ShouldThrottle reports whether a new frame should be delayed.
// Returns true if the throttler is active (e.g., background tab).
func (ft *FrameThrottler) ShouldThrottle() bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return ft.throttled
}

// SetThrottled enables or disables background throttling.
func (ft *FrameThrottler) SetThrottled(v bool) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.throttled = v
}

// WaitForNextFrame blocks until the next frame interval has elapsed since the
// last recorded frame. Returns immediately if not throttled.
func (ft *FrameThrottler) WaitForNextFrame() {
	ft.mu.Lock()
	target := ft.lastFrame.Add(ft.minFrameMs)
	ft.mu.Unlock()

	if ft.ShouldThrottle() {
		// Double the interval when throttled (background tab).
		target = target.Add(ft.minFrameMs)
	}

	sleep := time.Until(target)
	if sleep > 0 {
		time.Sleep(sleep)
	}

	ft.mu.Lock()
	ft.lastFrame = time.Now()
	ft.mu.Unlock()
}

// SetFPS changes the target frame rate.
func (ft *FrameThrottler) SetFPS(fps int) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.targetFPS = fps
	ft.minFrameMs = time.Second / time.Duration(fps)
}
