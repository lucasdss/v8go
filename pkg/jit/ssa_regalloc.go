//go:build !amd64

package jit

// RegAlloc is a simple linear-scan register allocator for the SSA-to-ARM64
// lowering pass. It assigns integer and floating-point registers to SSA nodes
// in program order without live-range analysis.
type RegAlloc struct {
	nodeToReg  map[*SSANode]int
	freeInts   []int
	freeFloats []int
}

// NewRegAlloc creates a register allocator with the full pool of
// caller-saved ARM64 registers available for JIT use.
func NewRegAlloc() *RegAlloc {
	return &RegAlloc{
		nodeToReg: make(map[*SSANode]int),
		freeInts: []int{
			REG_R8, REG_R9, REG_R10, REG_R11,
			REG_R12, REG_R13, REG_R14, REG_R15,
			REG_R16, REG_R17,
		},
		freeFloats: []int{8, 9, 10, 11, 12, 13, 14, 15},
	}
}

// AllocateInt assigns the next available integer register to the node.
// Returns -1 when the register pool is exhausted.
func (ra *RegAlloc) AllocateInt(node *SSANode) int {
	if len(ra.freeInts) == 0 {
		return -1
	}
	reg := ra.freeInts[0]
	ra.freeInts = ra.freeInts[1:]
	ra.nodeToReg[node] = reg
	return reg
}

// AllocateFloat assigns the next available floating-point register to the node.
// Returns -1 when the register pool is exhausted.
func (ra *RegAlloc) AllocateFloat(node *SSANode) int {
	if len(ra.freeFloats) == 0 {
		return -1
	}
	reg := ra.freeFloats[0]
	ra.freeFloats = ra.freeFloats[1:]
	ra.nodeToReg[node] = reg
	return reg
}

// allocateRegisters performs a linear-scan allocation across all basic blocks.
// Integer-typed nodes receive integer registers; float64-typed nodes receive
// floating-point registers.
func allocateRegisters(g *SSAGraph) *RegAlloc {
	ra := NewRegAlloc()
	for _, bb := range g.Blocks {
		for _, node := range bb.Nodes {
			if node.Type == SSAFloat64 {
				ra.AllocateFloat(node)
			} else {
				ra.AllocateInt(node)
			}
		}
	}
	return ra
}
