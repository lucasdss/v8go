// Package jit provides a dual-mapped code buffer for JIT compilation.
// It enforces W^X (write XOR execute) memory protection by mapping the
// same physical pages at two different virtual addresses: one RW for
// writes, one RX for execution. This eliminates the need for mprotect
// toggling and provides true hardware-enforced separation.
//
// Platform support:
//   - Darwin: shm_open + dual mmap with MAP_JIT for Apple Silicon.
//   - Linux:   memfd_create + dual mmap. ARM64 requires explicit I-cache flush.
package jit

import (
	"fmt"
	"syscall"
)

const pageSize = 4096

// CodeBuf is a dual-mapped code buffer supporting simultaneous write and
// execute access through separate virtual address mappings of the same
// physical pages.
type CodeBuf struct {
	rwBuf  []byte // writable mapping (PROT_READ|PROT_WRITE)
	rxBuf  []byte // executable mapping (PROT_READ|PROT_EXEC)
	rwAddr uintptr
	rxAddr uintptr
	size   int
	pos    int
	fd     int // closed file descriptor (-1 after close)
}

// NewCodeBuf allocates a dual-mapped buffer of the given size (page-aligned).
// The buffer maps the same physical pages at two different virtual addresses:
// one RW for writes, one RX for execution.
func NewCodeBuf(size int) (*CodeBuf, error) {
	return newCodeBufDual(size)
}

// Write appends a single byte to the buffer. Uses the RW mapping.
func (c *CodeBuf) Write(b byte) {
	if c.rwBuf == nil {
		return
	}
	if c.pos < c.size {
		c.rwBuf[c.pos] = b
		c.pos++
	}
}

// WriteUint32LE writes a 32-bit value in little-endian order.
// Uses the RW mapping.
func (c *CodeBuf) WriteUint32LE(v uint32) {
	if c.rwBuf == nil {
		return
	}
	if c.pos > c.size-4 {
		panic(fmt.Sprintf("CodeBuf.WriteUint32LE: write past end (pos=%d, size=%d)", c.pos, c.size))
	}
	c.rwBuf[c.pos] = byte(v)
	c.rwBuf[c.pos+1] = byte(v >> 8)
	c.rwBuf[c.pos+2] = byte(v >> 16)
	c.rwBuf[c.pos+3] = byte(v >> 24)
	c.pos += 4
}

// PatchUint32LE writes a 32-bit little-endian value at the given offset.
// Uses the RW mapping. Panics if the offset+4 exceeds the buffer size.
func (c *CodeBuf) PatchUint32LE(offset int, v uint32) {
	if c.rwBuf == nil {
		return
	}
	if offset < 0 || offset > c.size-4 {
		panic(fmt.Sprintf("CodeBuf.PatchUint32LE: offset out of bounds (offset=%d, size=%d)", offset, c.size))
	}
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

// Commit performs platform-specific cache maintenance after writing JIT
// code. On ARM64 Linux, this flushes the D-cache and invalidates the
// I-cache so that writes via the RW mapping are visible when executing
// via the RX mapping. On other platforms, this is a no-op.
func (c *CodeBuf) Commit() {
	if c.rwBuf == nil {
		return
	}
	flushICache(c.rwAddr, c.pos)
}

// Free unmaps both the RW and RX mappings.
// On platforms where rwBuf and rxBuf are the same slice (Darwin single
// MAP_JIT mapping), the second unmap is skipped to avoid EINVAL.
// After a successful free, all fields are zeroed to prevent use-after-free.
func (c *CodeBuf) Free() error {
	if c.rwBuf == nil {
		return nil
	}
	// If both slices share the same backing array, only unmap once.
	if &c.rwBuf[0] == &c.rxBuf[0] {
		if err := syscall.Munmap(c.rwBuf); err != nil {
			return fmt.Errorf("munmap: %w", err)
		}
		c.rwBuf = nil
		c.rxBuf = nil
		c.rwAddr = 0
		c.rxAddr = 0
		c.size = 0
		c.pos = 0
		return nil
	}
	var lastErr error
	if err := syscall.Munmap(c.rwBuf); err != nil {
		lastErr = fmt.Errorf("munmap rw: %w", err)
	}
	if err := syscall.Munmap(c.rxBuf); err != nil && lastErr == nil {
		lastErr = fmt.Errorf("munmap rx: %w", err)
	}
	if lastErr == nil {
		c.rwBuf = nil
		c.rxBuf = nil
		c.rwAddr = 0
		c.rxAddr = 0
		c.size = 0
		c.pos = 0
	}
	return lastErr
}
