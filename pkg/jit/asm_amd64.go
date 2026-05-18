package jit

// AMD64 instruction encoding primitives. Works with Assembler's CodeBuf.
// Implements REX prefix, ModRM byte, and immediate operand encoding for
// the core instruction set: MOV, ADD, RET, NOP, JMP, JE, JNE, CMP, PUSH,
// POP, CALL.
//
// Register encoding (3-bit, extended via REX):
//   RAX=0  RCX=1  RDX=2  RBX=3  RSP=4  RBP=5  RSI=6  RDI=7
//   R8=8   R9=9   R10=10 R11=11 R12=12 R13=13 R14=14 R15=15

// ---- REX prefix ---------------------------------------------------------

// rex returns the REX prefix byte for the given flags.
// Bits: [0 1 0 0 W R X B]
//
//	W=64-bit operand, R=ModRM.reg extension, X=SIB index extension,
//	B=ModRM.r/m or opcode extension.
func rex(w, r, x, b bool) byte {
	prefix := byte(0x40)
	if w {
		prefix |= 0x08
	}
	if r {
		prefix |= 0x04
	}
	if x {
		prefix |= 0x02
	}
	if b {
		prefix |= 0x01
	}
	if prefix == 0x40 {
		return 0 // no REX needed
	}
	return prefix
}

// ---- ModRM byte ---------------------------------------------------------

// modRM builds a ModRM byte.
//
//	mod  = bits 7-6 (0-3)
//	reg  = bits 5-3 (register or opcode extension)
//	rm   = bits 2-0 (register or memory)
func modRM(mod, reg, rm byte) byte {
	return (mod << 6) | ((reg & 0x07) << 3) | (rm & 0x07)
}

// ---- Register helpers ---------------------------------------------------

// regLo returns the 3-bit encoding (0-7) of the register.
func regLo(reg int) byte { return byte(reg & 0x07) }

// regHi returns true if the register needs REX extension (R8-R15).
func regHi(reg int) bool { return reg >= 8 }

// ---- Emit helpers -------------------------------------------------------

// emitByte writes a single byte.
func (a *Assembler) emitByte(b byte) { a.buf.Write(b) }

// emitUint32 writes 4 bytes little-endian.
func (a *Assembler) emitUint32(v uint32) { a.buf.WriteUint32LE(v) }

// emitUint64 writes 8 bytes little-endian.
func (a *Assembler) emitUint64(v uint64) {
	a.emitUint32(uint32(v))
	a.emitUint32(uint32(v >> 32))
}

// ---- Instructions -------------------------------------------------------

// AMD64_RET emits a near return: C3
func (a *Assembler) AMD64_RET() { a.emitByte(0xC3) }

// AMD64_NOP emits a single-byte NOP: 90
func (a *Assembler) AMD64_NOP() { a.emitByte(0x90) }

// AMD64_NOP_MULTI emits a multi-byte NOP sled of the given length.
// Uses Intel-recommended NOP sequences for optimal decode.
func (a *Assembler) AMD64_NOP_MULTI(n int) {
	// Intel-recommended multi-byte NOP sequences (AMD64 optimization manual).
	multiNOPs := [][9]byte{
		{0x90},                                                 // 1
		{0x66, 0x90},                                           // 2
		{0x0F, 0x1F, 0x00},                                     // 3
		{0x0F, 0x1F, 0x40, 0x00},                               // 4
		{0x0F, 0x1F, 0x44, 0x00, 0x00},                         // 5
		{0x66, 0x0F, 0x1F, 0x44, 0x00, 0x00},                   // 6
		{0x0F, 0x1F, 0x80, 0x00, 0x00, 0x00, 0x00},             // 7
		{0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00},       // 8
		{0x66, 0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00}, // 9
	}
	for n > 0 {
		k := n
		if k > 9 {
			k = 9
		}
		for i := 0; i < k; i++ {
			a.emitByte(multiNOPs[k-1][i])
		}
		n -= k
	}
}

// AMD64_PUSH emits: PUSH r64  (50+rd)
func (a *Assembler) AMD64_PUSH(reg int) {
	if regHi(reg) {
		a.emitByte(rex(false, false, false, true)) // REX.B
	}
	a.emitByte(0x50 | regLo(reg))
}

// AMD64_POP emits: POP r64  (58+rd)
func (a *Assembler) AMD64_POP(reg int) {
	if regHi(reg) {
		a.emitByte(rex(false, false, false, true)) // REX.B
	}
	a.emitByte(0x58 | regLo(reg))
}

// AMD64_MOV_RR emits: MOV r64, r64  (89 /r) — moves src into dst.
// Encoding: REX.W + 89 /r  where ModRM.reg=src, ModRM.r/m=dst (mod=11)
func (a *Assembler) AMD64_MOV_RR(dst, src int) {
	prefix := rex(true, regHi(src), false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x89)
	a.emitByte(modRM(3, regLo(src), regLo(dst)))
}

// AMD64_MOV_RI emits: MOV r64, imm64  (REX.W + B8+rd)
func (a *Assembler) AMD64_MOV_RI(reg int, imm uint64) {
	prefix := rex(true, false, false, regHi(reg))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xB8 | regLo(reg))
	a.emitUint64(imm)
}

// AMD64_ADD_RR emits: ADD r64, r64  (01 /r) — dst += src.
// Encoding: REX.W + 01 /r  where ModRM.reg=src, ModRM.r/m=dst (mod=11)
func (a *Assembler) AMD64_ADD_RR(dst, src int) {
	prefix := rex(true, regHi(src), false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x01)
	a.emitByte(modRM(3, regLo(src), regLo(dst)))
}

// AMD64_CMP_RR emits: CMP r64, r64  (39 /r) — sets flags: dst - src.
// Encoding: REX.W + 39 /r  where ModRM.reg=src, ModRM.r/m=dst (mod=11)
func (a *Assembler) AMD64_CMP_RR(dst, src int) {
	prefix := rex(true, regHi(src), false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x39)
	a.emitByte(modRM(3, regLo(src), regLo(dst)))
}

// AMD64_JMP emits: JMP rel32  (E9 cd) — unconditional near jump.
// offset is relative to the end of this instruction (5 bytes total: E9 + 4-byte disp).
func (a *Assembler) AMD64_JMP(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0xE9)
	a.emitUint32(0) // placeholder
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JE emits: JE rel32  (0F 84 cd) — jump if equal (ZF=1).
func (a *Assembler) AMD64_JE(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x84)
	a.emitUint32(0) // placeholder
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JNE emits: JNE rel32  (0F 85 cd) — jump if not equal (ZF=0).
func (a *Assembler) AMD64_JNE(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x85)
	a.emitUint32(0) // placeholder
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JZ emits: JZ rel32  (0F 84 cd) — jump if zero (ZF=1). Same encoding as JE.
func (a *Assembler) AMD64_JZ(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x84)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JNZ emits: JNZ rel32  (0F 85 cd) — jump if not zero (ZF=0). Same encoding as JNE.
func (a *Assembler) AMD64_JNZ(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x85)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JL emits: JL rel32  (0F 8C cd) — jump if less (SF≠OF).
func (a *Assembler) AMD64_JL(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x8C)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JG emits: JG rel32  (0F 8F cd) — jump if greater (ZF=0 AND SF=OF).
func (a *Assembler) AMD64_JG(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x8F)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JLE emits: JLE rel32  (0F 8E cd) — jump if less or equal (ZF=1 OR SF≠OF).
func (a *Assembler) AMD64_JLE(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x8E)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JGE emits: JGE rel32  (0F 8D cd) — jump if greater or equal (SF=OF).
func (a *Assembler) AMD64_JGE(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x8D)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JB emits: JB rel32  (0F 82 cd) — jump if below (CF=1).
func (a *Assembler) AMD64_JB(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x82)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JBE emits: JBE rel32  (0F 86 cd) — jump if below or equal (CF=1 OR ZF=1).
func (a *Assembler) AMD64_JBE(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x86)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JA emits: JA rel32  (0F 87 cd) — jump if above (CF=0 AND ZF=0).
func (a *Assembler) AMD64_JA(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x87)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JAE emits: JAE rel32  (0F 83 cd) — jump if above or equal (CF=0).
func (a *Assembler) AMD64_JAE(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x83)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JP emits: JP rel32  (0F 8A cd) — jump if parity (PF=1).
func (a *Assembler) AMD64_JP(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x8A)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JNP emits: JNP rel32  (0F 8B cd) — jump if not parity (PF=0).
func (a *Assembler) AMD64_JNP(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x8B)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_JS emits: JS rel32  (0F 88 cd) — jump if sign (SF=1).
func (a *Assembler) AMD64_JS(l *Label) {
	pos := a.buf.Pos()
	a.emitByte(0x0F)
	a.emitByte(0x88)
	a.emitUint32(0)
	if l.offset >= 0 {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// AMD64_SUB_RR emits: SUB r64, r64  (29 /r) — dst -= src.
// Encoding: REX.W + 29 /r  where ModRM.reg=src, ModRM.r/m=dst (mod=11)
func (a *Assembler) AMD64_SUB_RR(dst, src int) {
	prefix := rex(true, regHi(src), false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x29)
	a.emitByte(modRM(3, regLo(src), regLo(dst)))
}

// AMD64_AND_RR emits: AND r64, r64  (21 /r) — dst &= src.
// Encoding: REX.W + 21 /r  where ModRM.reg=src, ModRM.r/m=dst (mod=11)
func (a *Assembler) AMD64_AND_RR(dst, src int) {
	prefix := rex(true, regHi(src), false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x21)
	a.emitByte(modRM(3, regLo(src), regLo(dst)))
}

// AMD64_OR_RR emits: OR r64, r64  (09 /r) — dst |= src.
// Encoding: REX.W + 09 /r  where ModRM.reg=src, ModRM.r/m=dst (mod=11)
func (a *Assembler) AMD64_OR_RR(dst, src int) {
	prefix := rex(true, regHi(src), false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x09)
	a.emitByte(modRM(3, regLo(src), regLo(dst)))
}

// AMD64_XOR_RR emits: XOR r64, r64  (31 /r) — dst ^= src.
// Encoding: REX.W + 31 /r  where ModRM.reg=src, ModRM.r/m=dst (mod=11)
func (a *Assembler) AMD64_XOR_RR(dst, src int) {
	prefix := rex(true, regHi(src), false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x31)
	a.emitByte(modRM(3, regLo(src), regLo(dst)))
}

// ---- Memory load/store --------------------------------------------------

// AMD64_MOV_RM emits: MOV r64, [base]  (REX.W + 8B /r, Mod=00) — load from [base].
// No displacement. For displacement variants see MOV_LOAD / MOV_LOAD32.
func (a *Assembler) AMD64_MOV_RM(dst, base int) {
	prefix := rex(true, regHi(dst), false, regHi(base))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x8B)
	a.emitByte(modRM(0, regLo(dst), regLo(base)))
}

// AMD64_MOV_MR emits: MOV [base], src  (REX.W + 89 /r, Mod=00) — store to [base].
// No displacement. For displacement variants see MOV_STORE / MOV_STORE32.
func (a *Assembler) AMD64_MOV_MR(src, base int) {
	prefix := rex(true, regHi(src), false, regHi(base))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x89)
	a.emitByte(modRM(0, regLo(src), regLo(base)))
}

// AMD64_MOV_LOAD emits: MOV r64, [base+disp8]  (REX.W + 8B /r, Mod=01)
// Loads 8 bytes from [base+disp8] into dst.
func (a *Assembler) AMD64_MOV_LOAD(dst, base int, disp8 int8) {
	prefix := rex(true, regHi(dst), false, regHi(base))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x8B)
	a.emitByte(modRM(1, regLo(dst), regLo(base)))
	a.emitByte(byte(disp8))
}

// AMD64_MOV_LOAD32 emits: MOV r64, [base+disp8]  with 32-bit displacement.
// Encoding: REX.W + 8B /r, Mod=10 (disp32)
func (a *Assembler) AMD64_MOV_LOAD32(dst, base int, disp32 int32) {
	prefix := rex(true, regHi(dst), false, regHi(base))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x8B)
	a.emitByte(modRM(2, regLo(dst), regLo(base)))
	a.emitUint32(uint32(disp32))
}

// AMD64_MOV_STORE emits: MOV [base+disp8], src  (REX.W + 89 /r, Mod=01)
// Stores 8 bytes from src into [base+disp8].
func (a *Assembler) AMD64_MOV_STORE(src, base int, disp8 int8) {
	prefix := rex(true, regHi(src), false, regHi(base))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x89)
	a.emitByte(modRM(1, regLo(src), regLo(base)))
	a.emitByte(byte(disp8))
}

// AMD64_MOV_STORE32 emits: MOV [base+disp32], src  with 32-bit displacement.
func (a *Assembler) AMD64_MOV_STORE32(src, base int, disp32 int32) {
	prefix := rex(true, regHi(src), false, regHi(base))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x89)
	a.emitByte(modRM(2, regLo(src), regLo(base)))
	a.emitUint32(uint32(disp32))
}

// ---- Shifts -------------------------------------------------------------

// AMD64_SHL_RR emits: SHL r64, CL  (REX.W + D3 /4) — dst <<= CL (masked to 0-63).
// CL register must hold the shift count.
func (a *Assembler) AMD64_SHL_CL(dst int) {
	prefix := rex(true, false, false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xD3)
	a.emitByte(modRM(3, 4, regLo(dst))) // /4 opcode extension = SHL
}

// AMD64_SHR_CL emits: SHR r64, CL  (REX.W + D3 /5) — dst >>= CL (logical).
func (a *Assembler) AMD64_SHR_CL(dst int) {
	prefix := rex(true, false, false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xD3)
	a.emitByte(modRM(3, 5, regLo(dst))) // /5 opcode extension = SHR
}

// AMD64_SAR_CL emits: SAR r64, CL  (REX.W + D3 /7) — dst >>= CL (arithmetic).
func (a *Assembler) AMD64_SAR_CL(dst int) {
	prefix := rex(true, false, false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xD3)
	a.emitByte(modRM(3, 7, regLo(dst))) // /7 opcode extension = SAR
}

// AMD64_SHL_IMM emits: SHL r64, imm8  (REX.W + C1 /4 ib) — dst <<= imm8.
func (a *Assembler) AMD64_SHL_IMM(dst int, imm uint8) {
	prefix := rex(true, false, false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xC1)
	a.emitByte(modRM(3, 4, regLo(dst)))
	a.emitByte(byte(imm))
}

// AMD64_SHR_IMM emits: SHR r64, imm8  (REX.W + C1 /5 ib) — dst >>= imm8 (logical).
func (a *Assembler) AMD64_SHR_IMM(dst int, imm uint8) {
	prefix := rex(true, false, false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xC1)
	a.emitByte(modRM(3, 5, regLo(dst)))
	a.emitByte(byte(imm))
}

// AMD64_SAR_IMM emits: SAR r64, imm8  (REX.W + C1 /7 ib) — dst >>= imm8 (arithmetic).
func (a *Assembler) AMD64_SAR_IMM(dst int, imm uint8) {
	prefix := rex(true, false, false, regHi(dst))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xC1)
	a.emitByte(modRM(3, 7, regLo(dst)))
	a.emitByte(byte(imm))
}

// ---- GP ↔ XMM moves ------------------------------------------------------

// AMD64_MOVQ_XR emits: MOVQ xmm, r64  (66 REX.W 0F 6E /r) — move r64 to xmm.
// Encoding: 66 + [REX.W] + 0F 6E /r  ModRM(mod=11, reg=xmm, r/m=gp)
func (a *Assembler) AMD64_MOVQ_XR(dstXMM, srcGP int) {
	a.emitByte(0x66) // mandatory prefix
	prefix := rex(true, regHi(dstXMM), false, regHi(srcGP))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x6E)
	a.emitByte(modRM(3, regLo(dstXMM), regLo(srcGP)))
}

// AMD64_MOVQ_RX emits: MOVQ r64, xmm  (66 REX.W 0F 7E /r) — move xmm to r64.
// Encoding: 66 + [REX.W] + 0F 7E /r  ModRM(mod=11, reg=xmm, r/m=gp)
func (a *Assembler) AMD64_MOVQ_RX(dstGP, srcXMM int) {
	a.emitByte(0x66)
	prefix := rex(true, regHi(srcXMM), false, regHi(dstGP))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x7E)
	a.emitByte(modRM(3, regLo(srcXMM), regLo(dstGP)))
}

// AMD64_XORPD emits: XORPD xmm, xmm  (66 0F 57 /r) — zero xmm register.
// Encoding: 66 + [REX] + 0F 57 /r  ModRM(mod=11, reg=dst, r/m=src)
func (a *Assembler) AMD64_XORPD(dstXMM, srcXMM int) {
	a.emitByte(0x66)
	prefix := rex(false, regHi(dstXMM), false, regHi(srcXMM))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x57)
	a.emitByte(modRM(3, regLo(dstXMM), regLo(srcXMM)))
}

// ---- XMM scalar double-precision ----------------------------------------

// AMD64_MOVSD emits: MOVSD XMMdst, XMMsrc  (F2 0F 10 /r) — move scalar double.
// Encoding: F2 + [REX] + 0F 10 /r  ModRM(mod=11, reg=dst, r/m=src)
func (a *Assembler) AMD64_MOVSD(dst, src int) {
	a.emitByte(0xF2) // mandatory prefix
	prefix := rex(false, regHi(dst), false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x10)
	a.emitByte(modRM(3, regLo(dst), regLo(src)))
}

// AMD64_ADDSD emits: ADDSD XMMdst, XMMsrc  (F2 0F 58 /r) — dst += src (scalar double).
func (a *Assembler) AMD64_ADDSD(dst, src int) {
	a.emitByte(0xF2)
	prefix := rex(false, regHi(dst), false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x58)
	a.emitByte(modRM(3, regLo(dst), regLo(src)))
}

// AMD64_SUBSD emits: SUBSD XMMdst, XMMsrc  (F2 0F 5C /r) — dst -= src (scalar double).
func (a *Assembler) AMD64_SUBSD(dst, src int) {
	a.emitByte(0xF2)
	prefix := rex(false, regHi(dst), false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x5C)
	a.emitByte(modRM(3, regLo(dst), regLo(src)))
}

// AMD64_MULSD emits: MULSD XMMdst, XMMsrc  (F2 0F 59 /r) — dst *= src (scalar double).
func (a *Assembler) AMD64_MULSD(dst, src int) {
	a.emitByte(0xF2)
	prefix := rex(false, regHi(dst), false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x59)
	a.emitByte(modRM(3, regLo(dst), regLo(src)))
}

// AMD64_DIVSD emits: DIVSD XMMdst, XMMsrc  (F2 0F 5E /r) — dst /= src (scalar double).
func (a *Assembler) AMD64_DIVSD(dst, src int) {
	a.emitByte(0xF2)
	prefix := rex(false, regHi(dst), false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x5E)
	a.emitByte(modRM(3, regLo(dst), regLo(src)))
}

// AMD64_COMISD emits: COMISD xmm1, xmm2  (66 0F 2F /r) — compare scalar double, set flags.
// Sets ZF, PF, CF: ZF=1,PF=0,CF=0 if equal; ZF=0,PF=0,CF=1 if below;
// ZF=0,PF=0,CF=0 if above; ZF=1,PF=1,CF=1 if unordered (NaN).
func (a *Assembler) AMD64_COMISD(xmm1, xmm2 int) {
	a.emitByte(0x66)
	prefix := rex(false, regHi(xmm1), false, regHi(xmm2))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x2F)
	a.emitByte(modRM(3, regLo(xmm1), regLo(xmm2)))
}

// AMD64_CVTTSD2SI emits: CVTTSD2SI r32, xmm  (F2 0F 2C /r) — truncate float64 → int32.
// Encoding: F2 + [REX.W] + 0F 2C /r  — REX.W for 64-bit dest; omit for 32-bit.
func (a *Assembler) AMD64_CVTTSD2SI(dstGP, srcXMM int) {
	a.emitByte(0xF2)
	prefix := rex(true, regHi(srcXMM), false, regHi(dstGP))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x2C)
	a.emitByte(modRM(3, regLo(srcXMM), regLo(dstGP)))
}

// AMD64_CVTSI2SD emits: CVTSI2SD xmm, r32  (F2 0F 2A /r) — convert int32 → float64.
// Encoding: F2 + [REX.W] + 0F 2A /r
func (a *Assembler) AMD64_CVTSI2SD(dstXMM, srcGP int) {
	a.emitByte(0xF2)
	prefix := rex(true, regHi(dstXMM), false, regHi(srcGP))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x2A)
	a.emitByte(modRM(3, regLo(dstXMM), regLo(srcGP)))
}

// AMD64_MOVD_XR emits: MOVD xmm, r32  (66 0F 7E /r) — move r32 to xmm (low 32 bits).
// Used for moving int32 into xmm before CVTSI2SD alternative.
func (a *Assembler) AMD64_MOVD_XR(dstXMM, srcGP int) {
	a.emitByte(0x66)
	prefix := rex(false, regHi(dstXMM), false, regHi(srcGP))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0x6E)
	a.emitByte(modRM(3, regLo(dstXMM), regLo(srcGP)))
}

// ---- Stack alignment ----------------------------------------------------

// AMD64_SUB_RI emits: SUB r64, imm32  (REX.W + 81 /5 id) — r64 -= imm32.
// Encoding: REX.W + 81 /5  ModRM(mod=11, reg=5, r/m=reg) + imm32 LE
func (a *Assembler) AMD64_SUB_RI(reg int, imm uint32) {
	prefix := rex(true, false, false, regHi(reg))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x81)
	a.emitByte(modRM(3, 5, regLo(reg)))
	a.emitUint32(imm)
}

// AMD64_ADDRSP emits: AND rsp, imm8  (REX.W + 83 /4 ib) — align stack pointer.
// Common pattern: AND RSP, -16 to enforce 16-byte alignment before calls.
func (a *Assembler) AMD64_AND_RSP_IMM(imm int8) {
	// RSP needs SIB byte when used as ModRM.r/m with Mod!=11, but Mod=11
	// is invalid for RSP. For AND rsp, imm8: use opcode 83 with /4.
	a.emitByte(0x48) // REX.W (no R, no X, no B since RSP=4 is low reg)
	a.emitByte(0x83)
	a.emitByte(modRM(3, 4, 4)) // Mod=11 (register), reg=/4, r/m=RSP
	a.emitByte(byte(imm))
}

// AMD64_LEA emits: LEA r64, [base+disp8]  (REX.W + 8D /r, Mod=01)
// Loads effective address base+disp8 into dst.
func (a *Assembler) AMD64_LEA(dst, base int, disp8 int8) {
	prefix := rex(true, regHi(dst), false, regHi(base))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x8D)
	a.emitByte(modRM(1, regLo(dst), regLo(base)))
	a.emitByte(byte(disp8))
}

// AMD64_IMUL_RR emits: IMUL r64, r64  (0F AF /r) — dst *= src (signed).
// Encoding: REX.W + 0F AF /r  where ModRM.reg=dst, ModRM.r/m=src (mod=11)
func (a *Assembler) AMD64_IMUL_RR(dst, src int) {
	prefix := rex(true, regHi(dst), false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x0F)
	a.emitByte(0xAF)
	a.emitByte(modRM(3, regLo(dst), regLo(src)))
}

// AMD64_MUL_RR emits: MUL r64  (F7 /4) — unsigned multiply: RDX:RAX = RAX * src.
// Encoding: REX.W + F7 /4  ModRM(mod=11, reg=4, r/m=src)
func (a *Assembler) AMD64_MUL_RR(src int) {
	prefix := rex(true, false, false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xF7)
	a.emitByte(modRM(3, 4, regLo(src)))
}

// AMD64_DIV_RR emits: DIV r64  (F7 /6) — unsigned divide: RAX = RDX:RAX / src, RDX = remainder.
// Encoding: REX.W + F7 /6  ModRM(mod=11, reg=6, r/m=src)
func (a *Assembler) AMD64_DIV_RR(src int) {
	prefix := rex(true, false, false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xF7)
	a.emitByte(modRM(3, 6, regLo(src)))
}

// AMD64_NOT_R emits: NOT r64  (F7 /2) — bitwise NOT of register.
// Encoding: REX.W + F7 /2  ModRM(mod=11, reg=2, r/m=reg)
func (a *Assembler) AMD64_NOT_R(reg int) {
	prefix := rex(true, false, false, regHi(reg))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xF7)
	a.emitByte(modRM(3, 2, regLo(reg)))
}

// AMD64_CQO emits: CQO  (REX.W + 99) — sign-extend RAX into RDX:RAX.
// Used before IDIV for signed division.
func (a *Assembler) AMD64_CQO() {
	a.emitByte(0x48) // REX.W
	a.emitByte(0x99)
}

// AMD64_IDIV_RR emits: IDIV r64  (REX.W + F7 /7) — signed divide: RDX:RAX / src.
// Quotient → RAX, remainder → RDX.
func (a *Assembler) AMD64_IDIV_RR(src int) {
	prefix := rex(true, false, false, regHi(src))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xF7)
	a.emitByte(modRM(3, 7, regLo(src)))
}

// AMD64_NEG_R emits: NEG r64  (F7 /3) — two's complement negation of register.
// Encoding: REX.W + F7 /3  ModRM(mod=11, reg=3, r/m=reg)
func (a *Assembler) AMD64_NEG_R(reg int) {
	prefix := rex(true, false, false, regHi(reg))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0xF7)
	a.emitByte(modRM(3, 3, regLo(reg)))
}

// AMD64_TEST_RR emits: TEST r64, r64  (85 /r) — logical AND, sets ZF/SF/PF, discards result.
// Encoding: REX.W + 85 /r  where ModRM.reg=src2, ModRM.r/m=src1 (mod=11)
func (a *Assembler) AMD64_TEST_RR(r1, r2 int) {
	prefix := rex(true, regHi(r2), false, regHi(r1))
	if prefix != 0 {
		a.emitByte(prefix)
	}
	a.emitByte(0x85)
	a.emitByte(modRM(3, regLo(r2), regLo(r1)))
}

// AMD64_CALL emits: CALL r/m64  (FF /2) — indirect call through register.
// Encoding: REX.B + FF /2  ModRM(mod=11, reg=2, r/m=reg)
func (a *Assembler) AMD64_CALL(reg int) {
	if regHi(reg) {
		a.emitByte(rex(false, false, false, true)) // REX.B
	}
	a.emitByte(0xFF)
	a.emitByte(modRM(3, 2, regLo(reg)))
}

// ---- AMD64 label resolution ---------------------------------------------

// AMD64_Bind resolves an AMD64 branch label at the current position.
// Backpatches all pending rel32 references. Call this at the jump target.
func (a *Assembler) AMD64_Bind(l *Label) {
	l.offset = a.buf.Pos()
	for _, pos := range l.patchPos {
		a.resolveAMD64Rel32(pos, l.offset-pos)
	}
}

// resolveAMD64Rel32 patches a 32-bit relative displacement at pos.
// The displacement is relative to the end of the instruction, which is pos+5
// for E9 and 0F 8x forms, or pos+6 for 2-byte opcode+ModRM forms.
// For simplicity, we compute the length from the opcode at pos.
func (a *Assembler) AMD64_ResolveRel32(pos int, targetOffset int) {
	a.resolveAMD64Rel32(pos, targetOffset-pos)
}

func (a *Assembler) resolveAMD64Rel32(pos int, offset int) {
	// Determine instruction length from opcode at pos.
	op := a.buf.rwBuf[pos]
	instrLen := 5 // default: E9 + 4-byte imm
	if op == 0x0F {
		instrLen = 6 // 0F 8x + 4-byte imm
	}
	// offset is from instruction start; displacement is from instruction end.
	disp := offset - instrLen
	a.buf.PatchUint32LE(pos+instrLen-4, uint32(int32(disp)))
}
