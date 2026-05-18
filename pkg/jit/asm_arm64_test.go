package jit

import "testing"

func TestAssemblerAdd(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.MOVZ(REG_R0, 3, 0)          // MOV X0, #3
	as.MOVZ(REG_R1, 4, 0)          // MOV X1, #4
	as.ADD(REG_R0, REG_R0, REG_R1) // X0 = 7
	as.RET()
	if buf.Len() != 16 {
		t.Errorf("expected 16 bytes, got %d", buf.Len())
	}
}

func TestAssemblerBranch(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	skip := NewLabel()
	end := NewLabel()
	as.MOVZ(REG_R0, 1, 0)
	as.B(skip)
	as.MOVZ(REG_R0, 99, 0)
	as.Bind(skip)
	as.MOVZ(REG_R0, 42, 0)
	as.B(end)
	as.Bind(end)
	as.RET()
	if buf.Len() == 0 {
		t.Error("should emit branch code")
	}
}

func TestICSlot(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	slow := NewLabel()
	as.EmitICSlot()
	as.Bind(slow)
	as.RET()
	// EmitICSlot emits 4 NOPs (16 bytes), then Bind+RET = 4 bytes = 20 bytes.
	if buf.Len() != 20 {
		t.Errorf("expected 20 bytes, got %d", buf.Len())
	}
}

func TestPACIASP_AUTIASP(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()
	as := NewAssembler(buf)
	as.PACIASP()
	as.AUTIASP()
	as.RET()
	// PACIASP (4) + AUTIASP (4) + RET (4) = 12 bytes.
	if buf.Len() != 12 {
		t.Errorf("expected 12 bytes, got %d", buf.Len())
	}
	// Verify encodings via the RW buffer.
	raw := buf.rwBuf
	if len(raw) < 12 {
		t.Fatal("buffer too short")
	}
	// PACIASP = 0xD503233F (little-endian: 3F 23 03 D5)
	if raw[0] != 0x3F || raw[1] != 0x23 || raw[2] != 0x03 || raw[3] != 0xD5 {
		t.Errorf("PACIASP encoding mismatch: got %02X %02X %02X %02X", raw[0], raw[1], raw[2], raw[3])
	}
	// AUTIASP = 0xD50323BF (little-endian: BF 23 03 D5)
	if raw[4] != 0xBF || raw[5] != 0x23 || raw[6] != 0x03 || raw[7] != 0xD5 {
		t.Errorf("AUTIASP encoding mismatch: got %02X %02X %02X %02X", raw[4], raw[5], raw[6], raw[7])
	}
}
