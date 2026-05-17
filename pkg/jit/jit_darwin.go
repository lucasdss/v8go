//go:build darwin

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
