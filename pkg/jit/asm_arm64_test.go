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
