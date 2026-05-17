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

func TestCodeBufSeal(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	buf.WriteUint32LE(0xD65F03C0)
	if err := buf.Seal(); err != nil {
		t.Skipf("Seal not supported in this environment: %v", err)
	}
}
