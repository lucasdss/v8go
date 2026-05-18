//go:build linux && !arm64

package jit

// flushICache is a no-op on non-ARM64 Linux. x86-64 has coherent
// instruction and data caches, so no explicit flush is needed.
func flushICache(addr uintptr, size int) {}
