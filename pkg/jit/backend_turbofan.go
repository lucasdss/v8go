//go:build !amd64

package jit

import (
	"fmt"

	"github.com/lucasdss/v8go/pkg/js"
)

// CompileTurboFan compiles a bytecode function with the TurboFan
// optimizing compiler and returns the executable address.
func (b *DefaultBackend) CompileTurboFan(bf *js.BytecodeFunction) (uintptr, error) {
	if bf == nil {
		return 0, fmt.Errorf("turbofan: nil function")
	}
	code, err := CompileTurboFan(bf)
	if err != nil {
		return 0, err
	}
	if code == nil {
		return 0, fmt.Errorf("turbofan: nil code buffer")
	}
	code.Commit()
	return code.RXAddr(), nil
}
