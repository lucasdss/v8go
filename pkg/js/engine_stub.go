//go:build !qjs

package js

// Engine is a stub for the QuickJS engine. When building without the qjs tag,
// the full Engine type (engine.go) is excluded. The stub allows other types
// (like eventloop.Agent) to reference Engine without breaking compilation.
type Engine struct{}

// NewEngine returns a nil Engine when QuickJS is not available.
func NewEngine() *Engine { return nil }

// Execute is a no-op for the stub engine.
func (e *Engine) Execute(src string) error { return nil }

// Close is a no-op for the stub engine.
func (e *Engine) Close() {}
