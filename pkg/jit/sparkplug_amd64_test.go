//go:build amd64

package jit

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// makeAMD64AddFunc returns a BytecodeFunction implementing:
//
//	function add(a, b) { return a + b; }
//
// Used as a standard benchmark target for AMD64 Sparkplug compilation.
func makeAMD64AddFunc() *js.BytecodeFunction {
	return &js.BytecodeFunction{
		Name:         "add",
		NumRegisters: 3,
		NumParams:    2,
		Instructions: []js.Instruction{
			{Op: js.OpLdar, OperandA: 0}, // load param 0
			{Op: js.OpStar, OperandA: 2}, // store to reg 2
			{Op: js.OpLdar, OperandA: 1}, // load param 1
			{Op: js.OpAdd, OperandA: 2},  // acc + reg2 → acc
			{Op: js.OpReturn},
		},
		Constants: []js.JSValue{},
	}
}

// BenchmarkAMD64SparkplugCompilation measures AMD64 Sparkplug codegen speed.
// This benchmark only runs on amd64 builds (GOARCH=amd64).
// The AMD64 assembler emits machine code bytes — compilation works on any
// architecture that can cross-compile to amd64.
//
// Run with: GOARCH=amd64 go test -bench=BenchmarkAMD64 -benchmem
func BenchmarkAMD64SparkplugCompilation(b *testing.B) {
	bf := makeAMD64AddFunc()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := CompileSparkplug(bf)
		if err != nil {
			b.Fatal(err)
		}
		buf.Free()
	}
}
