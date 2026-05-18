package jit

import "testing"

func TestNewCodeBuf(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	if buf.Len() != 0 {
		t.Error("new buffer should be empty")
	}
}

func TestCodeBufWrite(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	buf.WriteUint32LE(0xD65F03C0) // ARM64 RET
	if buf.Len() != 4 {
		t.Errorf("expected 4 bytes, got %d", buf.Len())
	}
}

// TestCodeBufDualMapping verifies the buffer works correctly.
// On Linux, rwBuf and rxBuf are different slices (true dual-mapping).
// On Darwin, they may be the same slice (MAP_JIT fallback). Both modes
// must correctly share underlying physical pages.
func TestCodeBufDualMapping(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()

	// Write via rwBuf.
	buf.WriteUint32LE(0xD65F03C0) // ARM64 RET instruction

	// Verify the written bytes appear in rwBuf.
	if buf.rwBuf[0] != 0xC0 || buf.rwBuf[1] != 0x03 ||
		buf.rwBuf[2] != 0x5F || buf.rwBuf[3] != 0xD6 {
		t.Error("rwBuf does not contain expected bytes")
	}

	// The content should be the same in both slices (same physical pages,
	// or same slice on single-mapping platforms).
	for i := 0; i < 4; i++ {
		if buf.rwBuf[i] != buf.rxBuf[i] {
			t.Errorf("byte %d: rwBuf=%02x rxBuf=%02x (should be equal)",
				i, buf.rwBuf[i], buf.rxBuf[i])
		}
	}

	// Verify addresses are valid.
	if buf.RWAddr() == 0 {
		t.Error("RWAddr should be non-zero")
	}
	if buf.RXAddr() == 0 {
		t.Error("RXAddr should be non-zero")
	}
}

// TestCodeBufPatchBounds verifies that PatchUint32LE panics on out-of-bounds.
func TestCodeBufPatchBounds(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()

	// Valid write at offset 0.
	buf.PatchUint32LE(0, 0xD65F03C0)

	// Out of bounds: negative offset should panic.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for negative offset")
			}
		}()
		buf.PatchUint32LE(-1, 0)
	}()

	// Out of bounds: offset at size-3 should panic (needs 4 bytes).
	// NewCodeBuf rounds up to page size (4096), so we test at the actual size.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for offset beyond buffer")
			}
		}()
		buf.PatchUint32LE(buf.size, 0) // exactly at size
	}()

	// Out of bounds: offset+4 past end should panic.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for offset+4 beyond buffer")
			}
		}()
		buf.PatchUint32LE(buf.size - 3, 0) // 3 bytes from end
	}()
}

// TestCodeBufWriteBounds verifies WriteUint32LE panics on overflow.
func TestCodeBufWriteBounds(t *testing.T) {
	// Use a size that's exactly page-aligned to control the test.
	pageAlignedSize := pageSize
	buf, err := NewCodeBuf(pageAlignedSize)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()

	// Fill the buffer to near capacity.
	for buf.pos+4 <= buf.size {
		buf.WriteUint32LE(0xAAAAAAAA)
	}

	// pos should now be at the last 4-byte boundary (or at size).
	// If there's room for exactly 0-3 more bytes (but not 4), WriteUint32LE should panic.
	if buf.pos+4 > buf.size {
		// Already at capacity, next WriteUint32LE panics.
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic for WriteUint32LE past end")
				}
			}()
			buf.WriteUint32LE(0xCCCCCCCC)
		}()
	}
}

// TestCodeBufCommit verifies Commit works without error after writes.
func TestCodeBufCommit(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	buf.WriteUint32LE(0xD65F03C0)
	buf.Commit()
	// After commit, buffer should still be usable for writes.
	buf.Write(0x00)
	if buf.Len() != 5 {
		t.Errorf("expected 5 bytes after commit+write, got %d", buf.Len())
	}
}

func TestCodeBufSeal(t *testing.T) {
	// Seal is removed. With dual-mapping, the RW mapping is always writable
	// and the RX mapping is always executable. No mprotect toggle needed.
	// Commit() handles platform-specific cache maintenance.
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	buf.WriteUint32LE(0xD65F03C0)
	// Commit flushes I-cache on platforms that need it.
	buf.Commit()
	// Verify we can still write after commit (RW mapping stays writable).
	buf.Write(0x00)
	if buf.Len() != 5 {
		t.Errorf("expected 5 bytes, got %d", buf.Len())
	}
}
