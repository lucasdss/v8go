package jit

import "github.com/lucasdss/v8go/pkg/js"

// DefaultBackend implements js.JITCompiler, js.ICPatcher, and
// js.ExecProtector by delegating to the existing jit package functions.
// It is the standard JIT backend for production use.
type DefaultBackend struct{}

// NewBackend creates a new DefaultBackend.
func NewBackend() *DefaultBackend { return &DefaultBackend{} }

// CompileSparkplug compiles a bytecode function with the Sparkplug
// baseline compiler and returns the executable address.
func (b *DefaultBackend) CompileSparkplug(bf *js.BytecodeFunction) (uintptr, error) {
	return SparkplugCompile(bf)
}

// EnableExec enables memory execution for JIT code.
func (b *DefaultBackend) EnableExec() { jitWriteProtect(true) }

// DisableExec disables memory execution for JIT code.
func (b *DefaultBackend) DisableExec() { jitWriteProtect(false) }
