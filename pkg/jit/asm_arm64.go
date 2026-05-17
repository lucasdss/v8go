//nolint:revive // assembler methods use concise register parameter names
package jit

// -- Integer arithmetic ---------------------------------------------------

// ADD emits: ADD Xdst, Xsrc1, Xsrc2
func (a *Assembler) ADD(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x8B000000 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// SUB emits: SUB Xdst, Xsrc1, Xsrc2
func (a *Assembler) SUB(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0xCB000000 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// MUL emits: MUL Xdst, Xsrc1, Xsrc2
func (a *Assembler) MUL(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x9B007C00 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// SDIV emits: SDIV Xdst, Xsrc1, Xsrc2
func (a *Assembler) SDIV(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x9AC00C00 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// -- Moves -----------------------------------------------------------------

// MOV emits: MOV Xdst, Xsrc  (ORR Xdst, XZR, Xsrc)
func (a *Assembler) MOV(dst, src int) {
	a.buf.WriteUint32LE(0xAA0003E0 | uint32(src<<16) | uint32(dst))
}

// MOVZ emits: MOVZ Xdst, #imm, LSL #shift
func (a *Assembler) MOVZ(dst int, imm uint16, shift int) {
	a.buf.WriteUint32LE(0xD2800000 | uint32(shift<<17) | uint32(imm)<<5 | uint32(dst))
}

// MOVK emits: MOVK Xdst, #imm, LSL #shift
func (a *Assembler) MOVK(dst int, imm uint16, shift int) {
	a.buf.WriteUint32LE(0xF2800000 | uint32(shift<<17) | uint32(imm)<<5 | uint32(dst))
}

// MOVN emits: MOVN Xdst, #imm, LSL #shift
func (a *Assembler) MOVN(dst int, imm uint16, shift int) {
	a.buf.WriteUint32LE(0x92800000 | uint32(shift<<17) | uint32(imm)<<5 | uint32(dst))
}

// -- Load / Store -----------------------------------------------------------

// LDR emits: LDR Xdst, [Xbase, #offset]  (unsigned offset, 64-bit)
func (a *Assembler) LDR(dst, base int, offset int) {
	a.buf.WriteUint32LE(0xF9400000 | uint32(offset/8)<<10 | uint32(base)<<5 | uint32(dst))
}

// STR emits: STR Xsrc, [Xbase, #offset]  (unsigned offset, 64-bit)
func (a *Assembler) STR(src, base int, offset int) {
	a.buf.WriteUint32LE(0xF9000000 | uint32(offset/8)<<10 | uint32(base)<<5 | uint32(src))
}

// STRW emits: STR Wsrc, [Xbase, #offset]  (unsigned offset, 32-bit)
func (a *Assembler) STRW(src, base int, offset int) {
	a.buf.WriteUint32LE(0xB9000000 | uint32(offset/4)<<10 | uint32(base)<<5 | uint32(src))
}

// STRB emits: STRB Wsrc, [Xbase, #offset]  (unsigned offset, 8-bit)
func (a *Assembler) STRB(src, base int, offset int) {
	a.buf.WriteUint32LE(0x39000000 | uint32(offset)<<10 | uint32(base)<<5 | uint32(src))
}

// -- Compare / Branch / Return ----------------------------------------------

// CMP emits: CMP Xsrc1, Xsrc2  (SUBS XZR, Xsrc1, Xsrc2)
func (a *Assembler) CMP(src1, src2 int) {
	a.buf.WriteUint32LE(0xEB00001F | uint32(src2<<16) | uint32(src1<<5))
}

// RET emits: RET  (RET X30)
func (a *Assembler) RET() { a.buf.WriteUint32LE(0xD65F03C0) }

// NOP emits: NOP
func (a *Assembler) NOP() { a.buf.WriteUint32LE(0xD503201F) }

// B emits an unconditional branch to the given label.
func (a *Assembler) B(l *Label) { a.emitBranch(0x14000000, l) }

// BEQ emits a conditional branch (EQ) to the given label.
func (a *Assembler) BEQ(l *Label) { a.emitBranch(0x54000000, l) }

// BNE emits a conditional branch (NE) to the given label.
func (a *Assembler) BNE(l *Label) { a.emitBranch(0x54000001, l) }

// BVS emits a conditional branch (VS / overflow set) to the given label.
// Used to detect NaN after FCMP (sets V=1 for unordered).
func (a *Assembler) BVS(l *Label) { a.emitBranch(0x54000006, l) }

// BVC emits a conditional branch (VC / overflow clear) to the given label.
func (a *Assembler) BVC(l *Label) { a.emitBranch(0x54000007, l) }

// CBZ emits: CBZ Xreg, label
func (a *Assembler) CBZ(reg int, l *Label) {
	pos := a.buf.Pos()
	a.buf.WriteUint32LE(0xB4000000 | uint32(reg))
	if l.offset >= 0 {
		a.resolveCBZ(pos, reg, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// CBNZ emits: CBNZ Xreg, label
func (a *Assembler) CBNZ(reg int, l *Label) {
	pos := a.buf.Pos()
	a.buf.WriteUint32LE(0xB5000000 | uint32(reg))
	if l.offset >= 0 {
		a.resolveCBZ(pos, reg, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

// -- Pair load / store -----------------------------------------------------

// STP emits: STP Xr1, Xr2, [Xbase, #offset]
func (a *Assembler) STP(r1, r2, base int, offset int) {
	imm7 := (offset / 8) & 0x7F
	a.buf.WriteUint32LE(0xA9000000 | uint32(imm7)<<15 | uint32(r2)<<10 | uint32(base)<<5 | uint32(r1))
}

// LDP emits: LDP Xr1, Xr2, [Xbase, #offset]
func (a *Assembler) LDP(r1, r2, base int, offset int) {
	imm7 := (offset / 8) & 0x7F
	a.buf.WriteUint32LE(0xA9400000 | uint32(imm7)<<15 | uint32(r2)<<10 | uint32(base)<<5 | uint32(r1))
}

// -- Branch with link -------------------------------------------------------

// BLR emits: BLR Xreg
func (a *Assembler) BLR(reg int) {
	a.buf.WriteUint32LE(0xD63F0000 | uint32(reg)<<5)
}

// -- Floating-point (double-precision) ------------------------------------

// FADD emits: FADD Ddst, Dsrc1, Dsrc2  (64-bit float add)
func (a *Assembler) FADD(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x1E602800 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// FSUB emits: FSUB Ddst, Dsrc1, Dsrc2  (64-bit float sub)
func (a *Assembler) FSUB(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x1E603800 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// FMUL emits: FMUL Ddst, Dsrc1, Dsrc2  (64-bit float mul)
func (a *Assembler) FMUL(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x1E600800 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// FDIV emits: FDIV Ddst, Dsrc1, Dsrc2  (64-bit float div)
func (a *Assembler) FDIV(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x1E601800 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// FMOV emits: FMOV Xdst, Vsrc  (float register to int register, 64-bit)
func (a *Assembler) FMOV(src, dst int) {
	a.buf.WriteUint32LE(0x9E660000 | uint32(src<<5) | uint32(dst))
}

// FMOV_XD emits: FMOV Vdst, Xsrc  (int register to float register, 64-bit)
func (a *Assembler) FMOV_XD(src, dst int) {
	a.buf.WriteUint32LE(0x9E670000 | uint32(src<<5) | uint32(dst))
}

// FNEG emits: FNEG Ddst, Dsrc  (64-bit float negate)
func (a *Assembler) FNEG(dst, src int) {
	a.buf.WriteUint32LE(0x1E614000 | uint32(src<<5) | uint32(dst))
}

// FCMP emits: FCMP Dn, Dm  — compare two double-precision floats, set NZCV flags.
func (a *Assembler) FCMP(r1, r2 int) {
	a.buf.WriteUint32LE(0x1E602000 | uint32(r2<<16) | uint32(r1<<5))
}

// CSET emits: CSET Xd, cond  — set dst to 1 if condition true, else 0.
// Condition codes: EQ=0, NE=1, CS=2, CC=3, MI=4, PL=5, VS=6, VC=7,
// HI=8, LS=9, GE=10, LT=11, GT=12, LE=13, AL=14, NV=15.
func (a *Assembler) CSET(dst, cond int) {
	a.buf.WriteUint32LE(0x9A9F07E0 | uint32(cond)<<12 | uint32(dst))
}

// CSEL emits: CSEL Xd, Xn, Xm, cond  — Xd = cond ? Xn : Xm.
func (a *Assembler) CSEL(dst, src1, src2, cond int) {
	a.buf.WriteUint32LE(0x9A800000 | uint32(cond)<<12 | uint32(src2)<<16 | uint32(src1)<<5 | uint32(dst))
}

// ORR emits: ORR Xdst, Xsrc1, Xsrc2  (bitwise OR).
func (a *Assembler) ORR(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0xAA000000 | uint32(src2)<<16 | uint32(src1)<<5 | uint32(dst))
}

// AND emits: AND Xdst, Xsrc1, Xsrc2  (bitwise AND).
func (a *Assembler) AND(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x8A000000 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// EOR emits: EOR Xdst, Xsrc1, Xsrc2  (bitwise XOR).
func (a *Assembler) EOR(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0xCA000000 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// MVN emits: MVN Xdst, Xsrc  (bitwise NOT via ORN with XZR).
func (a *Assembler) MVN(dst, src int) {
	a.buf.WriteUint32LE(0xAA2003E0 | uint32(src<<16) | uint32(dst))
}

// LSL emits: LSL Xdst, Xsrc1, Xsrc2  (logical shift left, variable).
func (a *Assembler) LSL(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x9AC02000 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// LSR emits: LSR Xdst, Xsrc1, Xsrc2  (logical shift right, variable).
func (a *Assembler) LSR(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x9AC02400 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// ASR emits: ASR Xdst, Xsrc1, Xsrc2  (arithmetic shift right, variable).
func (a *Assembler) ASR(dst, src1, src2 int) {
	a.buf.WriteUint32LE(0x9AC02800 | uint32(src2<<16) | uint32(src1<<5) | uint32(dst))
}

// FCVTZS emits: FCVTZS Wdst, Dsrc  (double-precision float to 32-bit signed int).
func (a *Assembler) FCVTZS(dst, src int) {
	a.buf.WriteUint32LE(0x1E780000 | uint32(src<<5) | uint32(dst))
}

// SCVTF emits: SCVTF Ddst, Wsrc  (32-bit signed int to double-precision float).
func (a *Assembler) SCVTF(dst, src int) {
	a.buf.WriteUint32LE(0x1E620000 | uint32(src<<5) | uint32(dst))
}

// NEG emits: NEG Xdst, Xsrc  (SUB Xdst, XZR, Xsrc)
func (a *Assembler) NEG(dst, src int) {
	a.buf.WriteUint32LE(0xCB0003E0 | uint32(src<<16) | uint32(dst))
}

// -- Bit manipulation -----------------------------------------------------

// CLZ emits: CLZ Xdst, Xsrc  (count leading zeros)
func (a *Assembler) CLZ(dst, src int) {
	a.buf.WriteUint32LE(0xDAC01000 | uint32(src)<<5 | uint32(dst))
}

// RBIT emits: RBIT Xdst, Xsrc  (reverse bits)
func (a *Assembler) RBIT(dst, src int) {
	a.buf.WriteUint32LE(0xDAC00000 | uint32(src)<<5 | uint32(dst))
}

// REV emits: REV Xdst, Xsrc  (reverse bytes in register)
func (a *Assembler) REV(dst, src int) {
	a.buf.WriteUint32LE(0xDAC00C00 | uint32(src)<<5 | uint32(dst))
}

// UBFX emits: UBFX Xdst, Xsrc, #lsb, #width  (unsigned bitfield extract)
func (a *Assembler) UBFX(dst, src int, lsb, width uint16) {
	a.buf.WriteUint32LE(0xD3400000 | uint32(lsb)<<10 | uint32(width-1)<<16 | uint32(src)<<5 | uint32(dst))
}

// SXTB emits: SXTB Xdst, Wsrc  (sign-extend byte)
func (a *Assembler) SXTB(dst, src int) {
	a.buf.WriteUint32LE(0x93401C00 | uint32(src)<<5 | uint32(dst))
}

// SXTH emits: SXTH Xdst, Wsrc  (sign-extend halfword)
func (a *Assembler) SXTH(dst, src int) {
	a.buf.WriteUint32LE(0x93403C00 | uint32(src)<<5 | uint32(dst))
}

// SXTW emits: SXTW Xdst, Wsrc  (sign-extend word)
func (a *Assembler) SXTW(dst, src int) {
	a.buf.WriteUint32LE(0x93407C00 | uint32(src)<<5 | uint32(dst))
}

// MSUB emits: MSUB Xdst, Xsrc1, Xsrc2, Xsrc3  (dst = src3 - src1*src2)
func (a *Assembler) MSUB(dst, src1, src2, src3 int) {
	a.buf.WriteUint32LE(0x9B008000 | uint32(src3)<<10 | uint32(src2)<<16 | uint32(src1)<<5 | uint32(dst))
}

// -- IC slot ---------------------------------------------------------------

// EmitICSlot emits a 16-byte NOP sled for an inline cache slot.
// Returns the byte offset of the sled within the code buffer.
// The sled is later patched by PatchICSlot when monomorphic feedback is available.
//
// Layout (16 bytes, 4 instructions):
//
//	slot+0:  NOP → LDR x9, [x8, #shapeOffset]
//	slot+4:  NOP → CMP x9, x10
//	slot+8:  NOP → B.NE +16 (skip 4 instrs to slowPath)
//	slot+12: NOP → LDR x0, [x8, #propOffset]
//
// After the sled, the compiler must emit:
//
//	STR x0, [R19, #accOffset]   // store property to frame.Acc
//	B continueLabel
//	slowPath:
//	<helper call>
//	continueLabel:
func (a *Assembler) EmitICSlot() int {
	offset := a.buf.Pos()
	a.NOP() // slot+0
	a.NOP() // slot+4
	a.NOP() // slot+8
	a.NOP() // slot+12
	return offset
}
