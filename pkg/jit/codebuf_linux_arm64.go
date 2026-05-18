//go:build linux && arm64

package jit

// flushICache performs D-cache clean + I-cache invalidate for the given
// address range on ARM64 Linux. Required because D-cache and I-cache are
// not coherent on ARM64 — writes via the RW mapping may not be visible
// to the I-cache when executing via the RX mapping.
func flushICache(addr uintptr, size int)

//go:noescape
func _flushICache(addr uintptr, size int)
