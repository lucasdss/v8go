// Package jit provides baseline and optimizing JIT compilers for the
// V8Go JavaScript engine. The Sparkplug baseline compiler (Tier 1)
// translates bytecode to ARM64 machine code. TurboFan (Tier 2) uses SSA
// optimization passes for peak performance.
//
// api.go defines the public API surface for compiler hooks. Function
// pointer parameter types use interface{} to avoid import cycles with
// pkg/js — concrete implementations in sparkplug.go cast internally.
package jit

// SparkplugCompileFunc compiles bytecode to native ARM64 code and returns
// the executable memory address. The bf parameter is *js.BytecodeFunction
// but uses interface{} to break the jit→js import dependency in api.go.
type SparkplugCompileFunc func(bf interface{}) (uintptr, error)

// SparkplugCompile is set by sparkplug.go init() to provide baseline
// JIT compilation. The VM reads this function pointer to trigger
// compilation when hot function thresholds are reached.
var SparkplugCompile SparkplugCompileFunc

// TurboFanCompileFunc compiles bytecode to optimized native ARM64 code
// using SSA-based optimizations. Parameter uses interface{} for the
// same import-cycle-breaking reason as SparkplugCompileFunc.
type TurboFanCompileFunc func(bf interface{}) (uintptr, error)

// TurboFanCompile is set by turbofan.go init() to provide optimizing
// JIT compilation. Reserved for Tier 2 compilation integration.
var TurboFanCompile TurboFanCompileFunc
