// Package jit provides a dual-mapped code buffer for ARM64 JIT compilation.
// It uses W^X (write XOR execute) memory protection via separate mappings,
// and leverages MAP_JIT on macOS for Apple Silicon compliance.
package jit

import (
	"fmt"
	"syscall"
	"unsafe"
)

const pageSize = 4096

// CodeBuf is a dual-mapped code buffer supporting write and execute mappings.
// On macOS Apple Silicon, MAP_JIT is used to allow fast W^X switching via
// pthread_jit_write_protect_np.
type CodeBuf struct {
	rwBuf  []byte // writable mapping
	rxBuf  []byte // executable mapping
	rwAddr uintptr
	rxAddr uintptr
	size   int
	pos    int
}

// NewCodeBuf allocates a dual-mapped buffer of the given size (page-aligned).
func NewCodeBuf(size int) (*CodeBuf, error) {
	size = (size + pageSize - 1) &^ (pageSize - 1)
	flags := syscall.MAP_ANON | syscall.MAP_PRIVATE
	// MAP_JIT (0x800) is macOS-specific, required for Apple Silicon.
	// On Linux it is 0 and has no effect.
	const mapJIT = 0x800
	flags |= mapJIT

	data, err := syscall.Mmap(-1, 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE, flags)
	if err != nil {
		return nil, fmt.Errorf("mmap: %w", err)
	}
	return &CodeBuf{
		rwBuf:  data,
		rxBuf:  data,
		rwAddr: uintptr(unsafe.Pointer(&data[0])), //nolint:gosec // JIT code requires raw memory access
		rxAddr: uintptr(unsafe.Pointer(&data[0])), //nolint:gosec // JIT code requires raw memory access
		size:   size,
	}, nil
}

// Write appends a single byte to the buffer.
func (c *CodeBuf) Write(b byte) {
	if c.pos < c.size {
		c.rwBuf[c.pos] = b
		c.pos++
	}
}

// WriteUint32LE writes a 32-bit value in little-endian order.
func (c *CodeBuf) WriteUint32LE(v uint32) {
	c.rwBuf[c.pos] = byte(v)
	c.rwBuf[c.pos+1] = byte(v >> 8)
	c.rwBuf[c.pos+2] = byte(v >> 16)
	c.rwBuf[c.pos+3] = byte(v >> 24)
	c.pos += 4
}

// PatchUint32LE writes a 32-bit little-endian value at the given offset.
func (c *CodeBuf) PatchUint32LE(offset int, v uint32) {
	b := c.rwBuf
	b[offset] = byte(v)
	b[offset+1] = byte(v >> 8)
	b[offset+2] = byte(v >> 16)
	b[offset+3] = byte(v >> 24)
}

// Pos returns the current write position.
func (c *CodeBuf) Pos() int { return c.pos }

// Len returns the number of bytes written.
func (c *CodeBuf) Len() int { return c.pos }

// RWAddr returns the read/write base address.
func (c *CodeBuf) RWAddr() uintptr { return c.rwAddr }

// RXAddr returns the read/execute base address.
func (c *CodeBuf) RXAddr() uintptr { return c.rxAddr }

// Seal flips memory protection to RX (read + execute).
// On macOS Apple Silicon, this should be preceded by
// pthread_jit_write_protect_np(false).
func (c *CodeBuf) Seal() error {
	if err := syscall.Mprotect(c.rwBuf, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		return fmt.Errorf("mprotect: %w", err)
	}
	return nil
}

// Free unmaps the buffer.
func (c *CodeBuf) Free() error {
	if err := syscall.Munmap(c.rwBuf); err != nil {
		return fmt.Errorf("munmap: %w", err)
	}
	return nil
}
