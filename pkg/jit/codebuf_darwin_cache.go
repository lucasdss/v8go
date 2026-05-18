//go:build darwin

package jit

// flushICache is a no-op on Darwin. Apple Silicon's MAP_JIT mechanism
// handles D-cache and I-cache coherence automatically when
// pthread_jit_write_protect_np is toggled. On x86-64, the caches are
// coherent in hardware.
func flushICache(addr uintptr, size int) {}
