// Package js implements the browser's scripting infrastructure, including the
// event loop, task queues, and microtask checkpoint, as defined in the
// WHATWG HTML Living Standard § 8.1 "Scripting":
// https://html.spec.whatwg.org/multipage/webappapis.html#scripting
//
// Note: This package provides the event loop framework.  Actual JS execution
// would be delegated to an embedded JS engine (e.g. goja).  The interfaces
// here define the host hooks and scheduling primitives needed to integrate
// such an engine.
package js

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ----------------------------- Script types ----------------------------------

// ScriptType mirrors the WHATWG HTML Standard § 8.1.3.1 "Script".
type ScriptType int

const (
	// ClassicScript is a traditional JS script loaded with a <script> tag.
	ClassicScript ScriptType = iota
	// ModuleScript is an ES module (import/export syntax).
	ModuleScript
)

// Script represents a script resource that the engine can execute.
// See HTML Standard § 8.1.3.1.
type Script struct {
	// Type is ClassicScript or ModuleScript.
	Type ScriptType
	// Source is the source text of the script.
	Source string
	// BaseURL is the base URL for module resolution.
	BaseURL string
	// FetchOptions holds the script fetch options.
	FetchOptions ScriptFetchOptions
}

// ScriptFetchOptions corresponds to the "script fetch options" struct in the
// HTML Standard § 8.1.5.1.
type ScriptFetchOptions struct {
	// Nonce is the cryptographic nonce value (for CSP).
	Nonce string
	// Integrity is the Subresource Integrity hash.
	Integrity string
	// CrossOrigin is the CORS settings attribute value.
	CrossOrigin string
	// ReferrerPolicy is the referrer policy for the fetch.
	ReferrerPolicy string
}

// ----------------------------- Task ------------------------------------------

// TaskSource identifies the queue a task was enqueued into.
// See HTML Standard § 8.1.7.2 "Task source".
type TaskSource string

const (
	// DOMManipulationTaskSource is used for DOM operations.
	DOMManipulationTaskSource TaskSource = "dom-manipulation"
	// UserInteractionTaskSource is used for user input events.
	UserInteractionTaskSource TaskSource = "user-interaction"
	// NetworkingTaskSource is used for network callbacks.
	NetworkingTaskSource TaskSource = "networking"
	// ScriptTaskSource is used for script execution tasks.
	ScriptTaskSource TaskSource = "script"
	// TimerTaskSource is used for setTimeout/setInterval callbacks.
	TimerTaskSource TaskSource = "timer"
	// HistoryTaskSource is for history API tasks.
	HistoryTaskSource TaskSource = "history"
)

// Task is a unit of work scheduled on the event loop.
// See HTML Standard § 8.1.7.1.
type Task struct {
	// Source identifies the task queue this task belongs to.
	Source TaskSource
	// Steps is the callback to execute when the task is processed.
	Steps func()
	// ID is a monotonically increasing identifier.
	ID uint64
}

// Microtask is a task enqueued on the microtask queue.
// See HTML Standard § 8.1.7.4.
type Microtask struct {
	Steps func()
}

// ----------------------------- Event Loop ------------------------------------

// EventLoop implements the HTML event loop algorithm defined in
// HTML Standard § 8.1.7.  It manages task queues and the microtask checkpoint.
type EventLoop struct {
	mu sync.Mutex

	// taskQueues holds one queue per TaskSource.
	taskQueues map[TaskSource][]*Task
	// microtaskQueue holds microtasks to run at the next checkpoint.
	microtaskQueue []*Microtask
	// nextID is the counter for task IDs.
	nextID uint64
	// done is closed when the event loop is stopped.
	done chan struct{}
	// running is true while the loop is spinning.
	running bool
	// timers holds pending timer callbacks (setTimeout/setInterval).
	timers []*timerEntry
	// onError is called when a task panics.
	onError func(error)
}

type timerEntry struct {
	id       int
	at       time.Time
	repeat   time.Duration
	callback func()
	active   bool
}

// NewEventLoop creates a new event loop with empty queues.
func NewEventLoop() *EventLoop {
	el := &EventLoop{
		taskQueues: make(map[TaskSource][]*Task),
		done:       make(chan struct{}),
	}
	return el
}

// SetErrorHandler registers a function to be called whenever a task or
// microtask panics during execution.
func (el *EventLoop) SetErrorHandler(fn func(error)) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.onError = fn
}

// EnqueueTask adds a task to the queue for source.
// See HTML Standard § 8.1.7.2.
func (el *EventLoop) EnqueueTask(source TaskSource, steps func()) *Task {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.nextID++
	t := &Task{Source: source, Steps: steps, ID: el.nextID}
	el.taskQueues[source] = append(el.taskQueues[source], t)
	return t
}

// EnqueueMicrotask adds a microtask to the microtask queue.
// See HTML Standard § 8.1.7.4.
func (el *EventLoop) EnqueueMicrotask(steps func()) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.microtaskQueue = append(el.microtaskQueue, &Microtask{Steps: steps})
}

// SetTimeout schedules callback to be called after delay.
// Returns a timer ID that can be passed to clearTimeout.
func (el *EventLoop) SetTimeout(callback func(), delay time.Duration) int {
	el.mu.Lock()
	defer el.mu.Unlock()
	id := len(el.timers) + 1
	el.timers = append(el.timers, &timerEntry{
		id:       id,
		at:       time.Now().Add(delay),
		callback: callback,
		active:   true,
	})
	return id
}

// ClearTimeout cancels a pending setTimeout.
func (el *EventLoop) ClearTimeout(id int) {
	el.mu.Lock()
	defer el.mu.Unlock()
	for _, t := range el.timers {
		if t.id == id {
			t.active = false
			return
		}
	}
}

// SetInterval schedules callback to fire repeatedly every interval.
func (el *EventLoop) SetInterval(callback func(), interval time.Duration) int {
	el.mu.Lock()
	defer el.mu.Unlock()
	id := len(el.timers) + 1
	el.timers = append(el.timers, &timerEntry{
		id:       id,
		at:       time.Now().Add(interval),
		repeat:   interval,
		callback: callback,
		active:   true,
	})
	return id
}

// Run starts the event loop and blocks until ctx is cancelled or Stop is called.
// It follows the processing model from HTML Standard § 8.1.7.3.
func (el *EventLoop) Run(ctx context.Context) {
	el.mu.Lock()
	if el.running {
		el.mu.Unlock()
		return
	}
	el.running = true
	el.mu.Unlock()

	defer func() {
		el.mu.Lock()
		el.running = false
		el.mu.Unlock()
	}()

	ticker := time.NewTicker(4 * time.Millisecond) // ~250 Hz "task starvation" tick
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-el.done:
			return
		case <-ticker.C:
			el.processTimers()
			el.runOneTask()
			el.performMicrotaskCheckpoint()
		}
	}
}

// Stop signals the event loop to exit its Run loop.
func (el *EventLoop) Stop() {
	select {
	case <-el.done:
		// already closed
	default:
		close(el.done)
	}
}

// RunUntilEmpty processes all currently enqueued tasks and microtasks and then
// returns.  Useful for testing and for synchronous flush scenarios.
func (el *EventLoop) RunUntilEmpty() {
	for {
		el.processTimers()
		if !el.runOneTask() {
			break
		}
		el.performMicrotaskCheckpoint()
	}
	el.performMicrotaskCheckpoint()
}

// processTimers fires any expired timers by enqueuing their callbacks as tasks.
func (el *EventLoop) processTimers() {
	el.mu.Lock()
	now := time.Now()
	var fired []*timerEntry
	for _, t := range el.timers {
		if t.active && !now.Before(t.at) {
			fired = append(fired, t)
		}
	}
	el.mu.Unlock()

	for _, t := range fired {
		cb := t.callback
		el.EnqueueTask(TimerTaskSource, cb)

		el.mu.Lock()
		if t.repeat > 0 {
			t.at = time.Now().Add(t.repeat)
		} else {
			t.active = false
		}
		el.mu.Unlock()
	}
}

// runOneTask picks the oldest task from any non-empty queue and runs it.
// Returns true if a task was executed.
func (el *EventLoop) runOneTask() bool {
	el.mu.Lock()
	var oldest *Task
	for _, queue := range el.taskQueues {
		if len(queue) == 0 {
			continue
		}
		t := queue[0]
		if oldest == nil || t.ID < oldest.ID {
			oldest = t
		}
	}

	if oldest == nil {
		el.mu.Unlock()
		return false
	}

	// Remove the task from its queue.
	src := oldest.Source
	queue := el.taskQueues[src]
	el.taskQueues[src] = queue[1:]
	el.mu.Unlock()

	el.safeRun(oldest.Steps)
	return true
}

// performMicrotaskCheckpoint drains the microtask queue.
// See HTML Standard § 8.1.7.4.
func (el *EventLoop) performMicrotaskCheckpoint() {
	for {
		el.mu.Lock()
		if len(el.microtaskQueue) == 0 {
			el.mu.Unlock()
			return
		}
		mt := el.microtaskQueue[0]
		el.microtaskQueue = el.microtaskQueue[1:]
		el.mu.Unlock()

		el.safeRun(mt.Steps)
	}
}

// safeRun executes fn, recovering from panics and forwarding them to the
// error handler.
func (el *EventLoop) safeRun(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			el.mu.Lock()
			handler := el.onError
			el.mu.Unlock()
			if handler != nil {
				var err error
				switch v := r.(type) {
				case error:
					err = v
				default:
					err = fmt.Errorf("task panic: %v", v)
				}
				handler(err)
			}
		}
	}()
	fn()
}

// ----------------------------- Agent / Host hooks ----------------------------

// Agent represents a JavaScript agent as defined in HTML Standard § 8.1.2.
// In a browser, each browsing context has an associated agent that manages
// script execution for that context.
type Agent struct {
	// Loop is the event loop that drives this agent.
	Loop *EventLoop
	// Engine executes classic scripts queued on this agent.
	Engine *Engine
}

// NewAgent creates a new Agent with its own event loop.
func NewAgent() *Agent {
	return &Agent{Loop: NewEventLoop(), Engine: NewEngine()}
}

// QueueScript schedules a script for execution in this agent's event loop.
// The actual execution would be delegated to an embedded JS engine.
func (a *Agent) QueueScript(script *Script) {
	src := script.Source
	a.Loop.EnqueueTask(ScriptTaskSource, func() {
		if a.Engine == nil {
			a.Engine = NewEngine()
		}
		_ = a.Engine.Execute(src)
	})
}
