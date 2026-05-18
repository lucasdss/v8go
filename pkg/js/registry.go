// registry.go — Function and bytecode registry with compilation cache.
//
// The Registry stores user-defined functions (by name → bytecode), built-in
// functions (by name → Go function), and a bytecode cache keyed by source
// string to avoid re-parsing/re-compiling the same JavaScript source.
package js

// BuiltinFn is the signature for built-in JavaScript functions implemented in Go.
type BuiltinFn func(args []JSValue) JSValue

// Registry stores function registrations and bytecode compilation cache.
// It is NOT goroutine-safe — the caller (VM.mu) provides synchronization.
type Registry struct {
	// Funcs maps user-defined function names to bytecode.
	Funcs map[string]*BytecodeFunction
	// Builtins maps built-in function names to Go implementations.
	Builtins map[string]BuiltinFn
	// Cache stores compiled bytecode keyed by source string.
	Cache map[string]*BytecodeFunction
}

// NewRegistry creates a Registry with pre-allocated capacity.
func NewRegistry() *Registry {
	return &Registry{
		Funcs:    make(map[string]*BytecodeFunction),
		Builtins: make(map[string]BuiltinFn),
		Cache:    make(map[string]*BytecodeFunction),
	}
}

// RegisterFunc stores a user-defined function's bytecode.
func (r *Registry) RegisterFunc(name string, bf *BytecodeFunction) {
	r.Funcs[name] = bf
}

// LookupFunc returns the bytecode for a user-defined function, or nil.
func (r *Registry) LookupFunc(name string) *BytecodeFunction {
	return r.Funcs[name]
}

// RegisterBuiltin stores a built-in function implementation.
func (r *Registry) RegisterBuiltin(name string, fn BuiltinFn) {
	r.Builtins[name] = fn
}

// LookupBuiltin returns the Go implementation for a built-in, or nil.
func (r *Registry) LookupBuiltin(name string) BuiltinFn {
	return r.Builtins[name]
}

// CacheResult stores compiled bytecode keyed by source string.
func (r *Registry) CacheResult(source string, bf *BytecodeFunction) {
	r.Cache[source] = bf
}

// CachedLookup returns cached bytecode for a source string, or nil.
func (r *Registry) CachedLookup(source string) *BytecodeFunction {
	return r.Cache[source]
}
