//go:build darwin

// jit_protect_darwin.go — Minimal CGO wrapper for pthread_jit_write_protect_np.
//
// On Apple Silicon, MAP_JIT pages require a per-thread toggle to switch
// between writable and executable states. This file provides that toggle.
// It replaces the old jit_darwin.go which was coupled to the single-mmap +
// mprotect architecture. With true dual-mapping on Linux, this toggle is
// only needed on Darwin.

package jit

/*
#include <pthread.h>
*/
import "C"

func jitWriteProtect(enabled bool) {
	val := C.int(0)
	if enabled {
		val = 1
	}
	C.pthread_jit_write_protect_np(val)
}
