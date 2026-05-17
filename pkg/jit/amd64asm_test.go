//go:build amd64

package jit

import "testing"

func newAMD64Buf(t *testing.T) (*CodeBuf, *Assembler) {
	t.Helper()
	buf, err := NewCodeBuf(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { buf.Free() })
	return buf, NewAssembler(buf)
}

func peekAMD64Bytes(t *testing.T, buf *CodeBuf, offset, n int) []byte {
	t.Helper()
	out := make([]byte, n)
	copy(out, buf.rwBuf[offset:offset+n])
	return out
}

func TestAMD64_MOV_RR(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_MOV_RR(REG_RAX, REG_RBX) // 48 89 D8
	if buf.Len() != 3 {
		t.Fatalf("expected 3 bytes, got %d", buf.Len())
	}
	got := peekAMD64Bytes(t, buf, 0, 3)
	if got[0] != 0x48 || got[1] != 0x89 || got[2] != 0xD8 {
		t.Errorf("expected 48 89 D8, got %02X %02X %02X", got[0], got[1], got[2])
	}
}

func TestAMD64_MOV_RI(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_MOV_RI(REG_RAX, 0xDEADBEEFCAFEBABE)
	if buf.Len() != 10 {
		t.Fatalf("expected 10 bytes, got %d", buf.Len())
	}
	if buf.rwBuf[0] != 0x48 || buf.rwBuf[1] != 0xB8 {
		t.Errorf("opcode: expected 48 B8, got %02X %02X", buf.rwBuf[0], buf.rwBuf[1])
	}
}

func TestAMD64_MOV_RI_HighReg(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_MOV_RI(REG_R8, 0x42) // 49 B8 ...
	if buf.Len() != 10 {
		t.Fatalf("expected 10 bytes, got %d", buf.Len())
	}
	if buf.rwBuf[0] != 0x49 || buf.rwBuf[1] != 0xB8 {
		t.Errorf("REX+opcode: expected 49 B8, got %02X %02X", buf.rwBuf[0], buf.rwBuf[1])
	}
}

func TestAMD64_ArithmeticRR(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_ADD_RR(REG_RAX, REG_RBX) // 48 01 D8
	as.AMD64_SUB_RR(REG_RCX, REG_RDX) // 48 29 D1
	as.AMD64_AND_RR(REG_RSI, REG_RDI) // 48 21 FE
	as.AMD64_OR_RR(REG_RBP, REG_RSP)  // 48 09 E5
	as.AMD64_XOR_RR(REG_RAX, REG_RAX) // 48 31 C0
	as.AMD64_CMP_RR(REG_RAX, REG_RBX) // 48 39 D8

	if buf.Len() != 18 {
		t.Fatalf("expected 18 bytes, got %d", buf.Len())
	}
	got := peekAMD64Bytes(t, buf, 12, 3)
	if got[0] != 0x48 || got[1] != 0x31 || got[2] != 0xC0 {
		t.Errorf("XOR RAX,RAX: expected 48 31 C0, got %02X %02X %02X", got[0], got[1], got[2])
	}
}

func TestAMD64_PushPop(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_PUSH(REG_RAX) // 50
	as.AMD64_PUSH(REG_R8)  // 41 50
	as.AMD64_POP(REG_RBP)  // 5D
	as.AMD64_POP(REG_R12)  // 41 5C

	if buf.Len() != 6 {
		t.Fatalf("expected 6 bytes, got %d", buf.Len())
	}
	if buf.rwBuf[0] != 0x50 {
		t.Errorf("PUSH RAX: expected 50, got %02X", buf.rwBuf[0])
	}
	if buf.rwBuf[1] != 0x41 || buf.rwBuf[2] != 0x50 {
		t.Errorf("PUSH R8: expected 41 50, got %02X %02X", buf.rwBuf[1], buf.rwBuf[2])
	}
}

func TestAMD64_ShiftInstructions(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_SHL_CL(REG_RAX)     // 48 D3 E0
	as.AMD64_SHR_CL(REG_RBX)     // 48 D3 EB
	as.AMD64_SAR_CL(REG_RCX)     // 48 D3 F9
	as.AMD64_SHL_IMM(REG_R8, 4)  // 49 C1 E0 04
	as.AMD64_SHR_IMM(REG_R9, 2)  // 49 C1 E9 02
	as.AMD64_SAR_IMM(REG_R10, 1) // 49 C1 FA 01

	if buf.Len() != 21 {
		t.Fatalf("expected 21 bytes, got %d", buf.Len())
	}
	got := peekAMD64Bytes(t, buf, 0, 3)
	if got[0] != 0x48 || got[1] != 0xD3 || got[2] != 0xE0 {
		t.Errorf("SHL RAX,CL: expected 48 D3 E0, got %02X %02X %02X", got[0], got[1], got[2])
	}
}

func TestAMD64_BranchResolution(t *testing.T) {
	buf, as := newAMD64Buf(t)
	skip := NewLabel()
	end := NewLabel()

	as.AMD64_MOV_RI(REG_RAX, 1)  // 10 bytes
	as.AMD64_JMP(skip)           // 5 bytes
	as.AMD64_MOV_RI(REG_RAX, 99) // 10 bytes (skipped)
	as.AMD64_Bind(skip)          // offset 25
	as.AMD64_MOV_RI(REG_RBX, 42) // 10 bytes
	as.AMD64_JMP(end)            // 5 bytes
	as.AMD64_Bind(end)
	as.AMD64_RET() // 1 byte

	if buf.Len() != 41 {
		t.Fatalf("expected 41 bytes, got %d", buf.Len())
	}
	// Verify JMP at offset 10 resolves to skip (offset 25): disp = 25-10-5 = 10
	disp := int32(uint32(buf.rwBuf[11]) | uint32(buf.rwBuf[12])<<8 |
		uint32(buf.rwBuf[13])<<16 | uint32(buf.rwBuf[14])<<24)
	if disp != 10 {
		t.Errorf("JMP displacement: expected 10, got %d", disp)
	}
}

func TestAMD64_ConditionalJumps(t *testing.T) {
	buf, as := newAMD64Buf(t)
	target := NewLabel()

	as.AMD64_JE(target)
	as.AMD64_JNE(target)
	as.AMD64_JL(target)
	as.AMD64_JG(target)
	as.AMD64_JLE(target)
	as.AMD64_JGE(target)
	as.AMD64_JZ(target)
	as.AMD64_JNZ(target)

	as.AMD64_Bind(target)
	as.AMD64_RET()

	if buf.Len() != 8*6+1 {
		t.Fatalf("expected 49 bytes, got %d", buf.Len())
	}
	opcodes := []byte{0x84, 0x85, 0x8C, 0x8F, 0x8E, 0x8D, 0x84, 0x85}
	for i, expectedOp := range opcodes {
		off := i * 6
		if buf.rwBuf[off] != 0x0F || buf.rwBuf[off+1] != expectedOp {
			t.Errorf("jump %d: expected 0F %02X, got %02X %02X",
				i, expectedOp, buf.rwBuf[off], buf.rwBuf[off+1])
		}
	}
}

func TestAMD64_XMMFloat(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_MOVSD(REG_X0, REG_X1) // F2 0F 10 C1
	as.AMD64_ADDSD(REG_X1, REG_X2) // F2 0F 58 CA
	as.AMD64_SUBSD(REG_X2, REG_X3) // F2 0F 5C D3
	as.AMD64_MULSD(REG_X3, REG_X4) // F2 0F 59 DC
	as.AMD64_DIVSD(REG_X4, REG_X5) // F2 0F 5E E5

	if buf.Len() != 20 {
		t.Fatalf("expected 20 bytes, got %d", buf.Len())
	}
	got := peekAMD64Bytes(t, buf, 0, 4)
	if got[0] != 0xF2 || got[1] != 0x0F || got[2] != 0x10 || got[3] != 0xC1 {
		t.Errorf("MOVSD X0,X1: expected F2 0F 10 C1, got %02X %02X %02X %02X",
			got[0], got[1], got[2], got[3])
	}
}

func TestAMD64_MulDivNotNegTest(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_MUL_RR(REG_RBX)           // 48 F7 E3
	as.AMD64_DIV_RR(REG_RCX)           // 48 F7 F1
	as.AMD64_NOT_R(REG_RAX)            // 48 F7 D0
	as.AMD64_NEG_R(REG_RDX)            // 48 F7 DA
	as.AMD64_TEST_RR(REG_RSI, REG_RDI) // 48 85 FE

	if buf.Len() != 15 {
		t.Fatalf("expected 15 bytes, got %d", buf.Len())
	}
	got := peekAMD64Bytes(t, buf, 12, 3)
	if got[0] != 0x48 || got[1] != 0x85 || got[2] != 0xFE {
		t.Errorf("TEST RSI,RDI: expected 48 85 FE, got %02X %02X %02X", got[0], got[1], got[2])
	}
}

func TestAMD64_MemoryOps(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_MOV_RM(REG_RAX, REG_RBX)        // 48 8B 03
	as.AMD64_MOV_MR(REG_RDX, REG_RCX)        // 48 89 11
	as.AMD64_MOV_LOAD(REG_R8, REG_R9, 8)     // 4D 8B 41 08
	as.AMD64_MOV_STORE(REG_R11, REG_R10, 16) // 4D 89 5A 10

	if buf.Len() != 14 {
		t.Fatalf("expected 14 bytes, got %d", buf.Len())
	}
	got := peekAMD64Bytes(t, buf, 0, 3)
	if got[0] != 0x48 || got[1] != 0x8B || got[2] != 0x03 {
		t.Errorf("MOV RAX,[RBX]: expected 48 8B 03, got %02X %02X %02X", got[0], got[1], got[2])
	}
}

func TestAMD64_ForwardBackpatch(t *testing.T) {
	buf, as := newAMD64Buf(t)
	loop := NewLabel()
	done := NewLabel()

	as.AMD64_Bind(loop)
	as.AMD64_CMP_RR(REG_RAX, REG_RAX) // 3 bytes
	as.AMD64_JE(done)                 // 6 bytes (forward)
	as.AMD64_JMP(loop)                // 5 bytes (backward)
	as.AMD64_Bind(done)
	as.AMD64_RET() // 1 byte

	if buf.Len() != 15 {
		t.Fatalf("expected 15 bytes, got %d", buf.Len())
	}
	// JE forward: offset 3 → target 14, disp = 14-3-6 = 5
	disp := int32(uint32(buf.rwBuf[5]) | uint32(buf.rwBuf[6])<<8 |
		uint32(buf.rwBuf[7])<<16 | uint32(buf.rwBuf[8])<<24)
	if disp != 5 {
		t.Errorf("JE forward: expected disp 5, got %d", disp)
	}
	// JMP backward: offset 9 → target 0, disp = 0-9-5 = -14
	disp = int32(uint32(buf.rwBuf[10]) | uint32(buf.rwBuf[11])<<8 |
		uint32(buf.rwBuf[12])<<16 | uint32(buf.rwBuf[13])<<24)
	if disp != -14 {
		t.Errorf("JMP backward: expected disp -14, got %d", disp)
	}
}

func TestAMD64_LEAandCALL(t *testing.T) {
	buf, as := newAMD64Buf(t)
	as.AMD64_LEA(REG_RAX, REG_RBP, 16) // 48 8D 45 10
	as.AMD64_CALL(REG_R8)              // 41 FF D0
	as.AMD64_NOP()                     // 90
	as.AMD64_RET()                     // C3

	if buf.Len() != 9 {
		t.Fatalf("expected 9 bytes, got %d", buf.Len())
	}
	got := peekAMD64Bytes(t, buf, 0, 4)
	if got[0] != 0x48 || got[1] != 0x8D || got[2] != 0x45 || got[3] != 0x10 {
		t.Errorf("LEA: expected 48 8D 45 10, got %02X %02X %02X %02X",
			got[0], got[1], got[2], got[3])
	}
	if buf.rwBuf[4] != 0x41 || buf.rwBuf[5] != 0xFF || buf.rwBuf[6] != 0xD0 {
		t.Errorf("CALL R8: expected 41 FF D0, got %02X %02X %02X",
			buf.rwBuf[4], buf.rwBuf[5], buf.rwBuf[6])
	}
}
