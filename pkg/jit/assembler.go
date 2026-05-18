package jit

// Label represents a branch target for backpatching during assembly.
type Label struct {
	offset   int   // -1 if unresolved
	patchPos []int // positions needing backpatch
}

// NewLabel creates an unresolved label.
func NewLabel() *Label { return &Label{offset: -1} }

// Assembler is a two-pass ARM64 assembler with label backpatching.
type Assembler struct {
	buf            *CodeBuf
	labels         map[*Label]int
	BlindingCookie uint64 // per-compilation random cookie for constant blinding
}

// NewAssembler creates an assembler for the given code buffer.
func NewAssembler(buf *CodeBuf) *Assembler {
	return &Assembler{buf: buf, labels: make(map[*Label]int)}
}

// Bind resolves a label at the current position, backpatching all pending references.
func (a *Assembler) Bind(l *Label) {
	l.offset = a.buf.Pos()
	for _, pos := range l.patchPos {
		existing := uint32(a.buf.rwBuf[pos]) |
			uint32(a.buf.rwBuf[pos+1])<<8 |
			uint32(a.buf.rwBuf[pos+2])<<16 |
			uint32(a.buf.rwBuf[pos+3])<<24
		offset := a.buf.Pos() - pos
		a.resolveBranch(pos, existing, offset)
	}
}

func (a *Assembler) resolveBranch(pos int, existing uint32, offset int) {
	switch {
	case existing&0xFC000000 == 0x14000000: // B
		a.buf.PatchUint32LE(pos, existing|uint32(offset/4)&0x3FFFFFF)
	case existing&0xFF000000 == 0x54000000: // B.cond (B.EQ, B.NE, etc.)
		a.buf.PatchUint32LE(pos, existing|uint32(offset/4&0x7FFFF)<<5)
	case existing&0xFF000000 == 0xB4000000 || existing&0xFF000000 == 0xB5000000: // CBZ/CBNZ
		a.resolveCBZ(pos, int(existing&0x1F), offset)
	}
}

func (a *Assembler) emitBranch(opcode uint32, l *Label) {
	pos := a.buf.Pos()
	a.buf.WriteUint32LE(opcode)
	if l.offset >= 0 {
		a.resolveBranch(pos, opcode, l.offset-pos)
	} else {
		l.patchPos = append(l.patchPos, pos)
	}
}

func (a *Assembler) resolveCBZ(pos int, reg int, offset int) {
	existing := uint32(a.buf.rwBuf[pos]) |
		uint32(a.buf.rwBuf[pos+1])<<8 |
		uint32(a.buf.rwBuf[pos+2])<<16 |
		uint32(a.buf.rwBuf[pos+3])<<24
	// Preserve opcode bits 31-24 and register bits 4-0; patch immediate at bits 23-5.
	a.buf.PatchUint32LE(pos, (existing&0xFF00001F)|uint32(offset/4&0x7FFFF)<<5|uint32(reg))
}
