//go:build amd64

package jit

import (
	"fmt"

	"github.com/lucasdss/v8go/pkg/js"
)

// CompileTurboFan returns an error on amd64 as TurboFan is not yet supported.
func (b *DefaultBackend) CompileTurboFan(bf *js.BytecodeFunction) (uintptr, error) {
	return 0, fmt.Errorf("turbofan: not available on amd64")
}
