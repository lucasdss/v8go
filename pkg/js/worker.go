// Package js implements the browser's scripting infrastructure.
//
// This file adds Web Worker support, allowing off-main-thread JavaScript
// execution with message-passing (postMessage/onmessage), inspired by
// Blink's worker thread infrastructure.
//
// See:
//   - WHATWG HTML Standard § 9 "Web Workers"
//   - Blink core/workers/
package js

import (
	"context"
	"runtime"
	"sync"
)

// ----------------------------- WebWorker -------------------------------------

// WorkerMessage represents a message passed between the main thread and a
// worker via postMessage/onmessage.
type WorkerMessage struct {
	// Type is an optional message type identifier.
	Type string
	// Data is the message payload.
	Data interface{}
	// Ports holds any transferred MessagePorts.
	Ports []interface{}
}

// WorkerScript is a JavaScript source string and its base URL for a worker.
type WorkerScript struct {
	// Source is the JavaScript code to execute in the worker.
	Source string
	// BaseURL is the base URL for module resolution.
	BaseURL string
}

// WebWorker represents a single Web Worker (DedicatedWorker).
// It runs JavaScript on a separate goroutine and communicates via
// channels using the postMessage/onmessage pattern.
type WebWorker struct {
	id  string
	ctx context.Context

	// engine executes JS code in the worker.
	engine *Engine

	// inbox receives messages from the main thread.
	inbox chan WorkerMessage
	// outbox sends messages to the main thread.
	outbox chan WorkerMessage

	// onMessage is the handler registered by the worker script.
	onMessage func(WorkerMessage)

	// onError is called when the worker encounters an error.
	onError func(error)

	mu  sync.RWMutex
	done chan struct{}
}

// NewWebWorker creates a new Web Worker with a unique ID.
func NewWebWorker(id string) *WebWorker {
	return &WebWorker{
		id:      id,
		engine:  NewEngine(),
		inbox:   make(chan WorkerMessage, 64),
		outbox:  make(chan WorkerMessage, 64),
		done:    make(chan struct{}),
	}
}

// ID returns the worker's unique identifier.
func (w *WebWorker) ID() string { return w.id }

// PostMessage sends a message to the worker from the main thread.
func (w *WebWorker) PostMessage(msg WorkerMessage) {
	select {
	case w.inbox <- msg:
	case <-w.done:
	}
}

// OnMessage registers a handler for messages sent from the worker to the
// main thread via postMessage.
func (w *WebWorker) OnMessage(fn func(WorkerMessage)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onMessage = fn
}

// OnError registers an error handler for worker errors.
func (w *WebWorker) OnError(fn func(error)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onError = fn
}

// Receive returns the outbox channel for the main thread to read messages
// from the worker.
func (w *WebWorker) Receive() <-chan WorkerMessage {
	return w.outbox
}

// Start launches the worker goroutine with the given script.
// If the worker is already running, this is a no-op.
func (w *WebWorker) Start(script WorkerScript) {
	w.mu.Lock()
	if w.ctx != nil {
		w.mu.Unlock()
		return // already running
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx = ctx
	w.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			w.mu.Lock()
			w.ctx = nil
			w.mu.Unlock()
		}()

		// Execute the worker script.
		err := w.engine.Execute(script.Source)
		if err != nil {
			w.mu.RLock()
			if w.onError != nil {
				w.onError(err)
			}
			w.mu.RUnlock()
			return
		}

		// Message loop: process inbox messages.
		for {
			select {
			case msg, ok := <-w.inbox:
				if !ok {
					return
				}
				w.mu.RLock()
				handler := w.onMessage
				w.mu.RUnlock()
				if handler != nil {
					handler(msg)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// SendToMain sends a message from the worker to the main thread.
// This is called from within the worker's goroutine.
func (w *WebWorker) SendToMain(msg WorkerMessage) {
	select {
	case w.outbox <- msg:
	case <-w.done:
	}
}

// Terminate stops the worker and cleans up resources.
func (w *WebWorker) Terminate() {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.done:
		return // already terminated
	default:
		close(w.done)
		close(w.inbox)
		close(w.outbox)
	}
}

// IsRunning reports whether the worker is still running.
func (w *WebWorker) IsRunning() bool {
	select {
	case <-w.done:
		return false
	default:
		return true
	}
}

// ----------------------------- WorkerPool ------------------------------------

// WorkerPool manages a fixed-size pool of Web Workers for efficient reuse.
type WorkerPool struct {
	mu      sync.Mutex
	workers []*WebWorker
	maxSize int
	nextID  int
}

// NewWorkerPool creates a worker pool with the given maximum size.
// When maxSize is 0, it defaults to runtime.NumCPU().
func NewWorkerPool(maxSize int) *WorkerPool {
	if maxSize <= 0 {
		maxSize = runtime.NumCPU()
	}
	return &WorkerPool{
		maxSize: maxSize,
	}
}

// Spawn creates a new worker and adds it to the pool.
// If the pool is at capacity, it returns nil.
func (p *WorkerPool) Spawn(script WorkerScript) *WebWorker {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.workers) >= p.maxSize {
		return nil
	}

	p.nextID++
	id := string(rune('A' + p.nextID%26))
	w := NewWebWorker(id)
	w.Start(script)
	p.workers = append(p.workers, w)
	return w
}

// Get returns a worker by ID, or nil.
func (p *WorkerPool) Get(id string) *WebWorker {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, w := range p.workers {
		if w.id == id {
			return w
		}
	}
	return nil
}

// Remove terminates and removes a worker from the pool.
func (p *WorkerPool) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, w := range p.workers {
		if w.id == id {
			w.Terminate()
			p.workers = append(p.workers[:i], p.workers[i+1:]...)
			return
		}
	}
}

// ActiveCount returns the number of currently active workers.
func (p *WorkerPool) ActiveCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.workers)
}

// Shutdown terminates all workers in the pool.
func (p *WorkerPool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, w := range p.workers {
		w.Terminate()
	}
	p.workers = nil
}
