//go:build linux

package jit

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// newCodeBufDual allocates a dual-mapped buffer using memfd_create.
// An anonymous in-memory file is created and mapped twice at different
// virtual addresses: one RW for writes, one RX for execution. This
// provides true W^X enforcement without mprotect toggling.
func newCodeBufDual(size int) (*CodeBuf, error) {
	size = (size + pageSize - 1) &^ (pageSize - 1)

	// memfd_create: create an anonymous in-memory file.
	fd, err := unix.MemfdCreate("v8go-jit", 0)
	if err != nil {
		return nil, fmt.Errorf("memfd_create: %w", err)
	}

	// ftruncate to set the file size.
	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("ftruncate: %w", err)
	}

	// mmap the RW mapping: PROT_READ|PROT_WRITE, MAP_SHARED.
	rwData, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("mmap rw: %w", err)
	}

	// mmap the RX mapping: PROT_READ|PROT_EXEC, MAP_SHARED.
	rxData, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_EXEC, syscall.MAP_SHARED)
	if err != nil {
		syscall.Munmap(rwData)
		unix.Close(fd)
		return nil, fmt.Errorf("mmap rx: %w", err)
	}

	// Close the file descriptor; mappings keep the pages alive.
	unix.Close(fd)

	return &CodeBuf{
		rwBuf:  rwData,
		rxBuf:  rxData,
		rwAddr: uintptr(unsafe.Pointer(&rwData[0])), //nolint:gosec
		rxAddr: uintptr(unsafe.Pointer(&rxData[0])), //nolint:gosec
		size:   size,
		fd:     -1, // fd already closed
	}, nil
}
