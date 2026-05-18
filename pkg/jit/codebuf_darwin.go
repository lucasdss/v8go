//go:build darwin

package jit

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	// MAP_JIT enables Apple Silicon JIT execution without the
	// com.apple.security.cs.allow-jit entitlement.
	_MAP_JIT = 0x800
)

// newCodeBufDual allocates a code buffer for JIT compilation.
//
// On Darwin, true dual-mapping (separate RW and RX virtual addresses)
// is not possible without a file-backed shared mapping, but MAP_JIT
// (required for Apple Silicon) is incompatible with MAP_SHARED.
// Instead, we use a single MAP_ANON|MAP_JIT mapping pointed to by both
// rwBuf and rxBuf. This preserves the CodeBuf API surface while
// ensuring Apple Silicon compatibility.
//
// On Linux, true dual-mapping is implemented via memfd_create.
func newCodeBufDual(size int) (*CodeBuf, error) {
	size = (size + pageSize - 1) &^ (pageSize - 1)

	// Create an anonymous MAP_JIT mapping. Start with PROT_READ|PROT_WRITE
	// (no PROT_EXEC) so writes work in the default thread state. The
	// JITProtectHook will toggle to executable mode before execution
	// and back to writable mode afterwards.
	//
	// MAP_JIT requires both PROT_WRITE and PROT_EXEC at mmap time, but
	// on Apple Silicon the effective protection is controlled by the
	// per-thread jit_write_protect state. Starting with only WRITE
	// avoids the SIGBUS on write that occurs when EXEC is set.
	data, err := syscall.Mmap(-1, 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE|_MAP_JIT)
	if err != nil {
		return nil, fmt.Errorf("mmap: %w", err)
	}

	return &CodeBuf{
		rwBuf:  data,
		rxBuf:  data,
		rwAddr: uintptr(unsafe.Pointer(&data[0])), //nolint:gosec
		rxAddr: uintptr(unsafe.Pointer(&data[0])), //nolint:gosec
		size:   size,
		fd:     -1,
	}, nil
}
