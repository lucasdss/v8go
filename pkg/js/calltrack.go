// calltrack.go — Call stack tracking and console output buffering.
//
// CallTracker manages call depth (infinite recursion prevention) and the
// call stack for Error.stack traces. Console buffers log/warn/error output
// and forwards it through an optional callback.
package js

// CallTracker manages JavaScript call depth and stack traces.
// It is NOT goroutine-safe — the caller (VM.mu) provides synchronization.
type CallTracker struct {
	depth int
	stack []CallStackFrame
}

// Push adds a frame to the call stack.
func (ct *CallTracker) Push(frame CallStackFrame) {
	ct.stack = append(ct.stack, frame)
}

// Pop removes the top frame from the call stack.
func (ct *CallTracker) Pop() {
	if len(ct.stack) > 0 {
		ct.stack = ct.stack[:len(ct.stack)-1]
	}
}

// Depth returns the current call depth.
func (ct *CallTracker) Depth() int {
	return ct.depth
}

// IncDepth increments the call depth.
func (ct *CallTracker) IncDepth() {
	ct.depth++
}

// DecDepth decrements the call depth.
func (ct *CallTracker) DecDepth() {
	ct.depth--
}

// Stack returns the current call stack (for Error.stack traces).
func (ct *CallTracker) Stack() []CallStackFrame {
	return ct.stack
}

// Reset clears the call stack and depth.
func (ct *CallTracker) Reset() {
	ct.depth = 0
	ct.stack = ct.stack[:0]
}

// Console buffers log output and forwards it to an optional callback.
// It is NOT goroutine-safe — the caller (VM.mu) provides synchronization.
type Console struct {
	output func(string)
	logs   []string
}

// NewConsole creates a Console with pre-allocated log buffer.
func NewConsole() *Console {
	return &Console{
		logs: make([]string, 0),
	}
}

// SetOutput sets the callback for console output forwarding.
func (c *Console) SetOutput(fn func(string)) {
	c.output = fn
}

// Log appends a line to the console log buffer and forwards it to the
// output callback if set.
func (c *Console) Log(msg string) {
	c.logs = append(c.logs, msg)
	if c.output != nil {
		c.output(msg)
	}
}

// Logs returns all accumulated log lines.
func (c *Console) Logs() []string {
	return c.logs
}

// Clear clears the accumulated log buffer.
func (c *Console) Clear() {
	c.logs = nil
}
