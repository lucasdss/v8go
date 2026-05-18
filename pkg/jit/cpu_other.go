//go:build !arm64

package jit

// hasARM64PAC is always false on non-ARM64 architectures.
var hasARM64PAC = false
