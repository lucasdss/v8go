// Package gov8 provides a clean, V8-inspired public API for the GoV8 JS engine.
//
// This package wraps the internal js engine and exposes only what users need:
// a simple Evaluate() function and a Configurable Engine for advanced use.
//
// Usage:
//
//	result := gov8.Evaluate("1 + 2")
//	fmt.Println(result) // 3
package v8go

import (
	"github.com/lucasdss/v8go/pkg/js"
)

// Engine is a configured JavaScript execution environment.
// Multiple engines are independent — they don't share state.
type Engine struct {
	vm      *js.VM
	console []string
}

// NewEngine creates a new JavaScript engine with console output capture.
func NewEngine() *Engine {
	e := &Engine{vm: js.NewVM()}
	e.vm.SetConsoleOutput(func(s string) {
		e.console = append(e.console, s)
	})
	return e
}

// Evaluate parses and executes JavaScript source code, returning the result.
func (e *Engine) Evaluate(src string) js.JSValue {
	return e.vm.Run(src)
}

// ConsoleOutput returns all accumulated console.log output and clears the buffer.
func (e *Engine) ConsoleOutput() []string {
	out := e.console
	e.console = nil
	return out
}

// Evaluate is a convenience function that creates a temporary engine,
// evaluates the source, and returns the result.
func Evaluate(src string) js.JSValue {
	vm := js.NewVM()
	return vm.Run(src)
}

// Version returns the GoV8 engine version.
func Version() string {
	return "GoV8/1.0 (V8-compatible JavaScript engine in Go)"
}
