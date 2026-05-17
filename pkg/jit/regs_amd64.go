//nolint:revive // ALL_CAPS register names follow AMD64 ISA convention
package jit

// AMD64 registers per Go ABI (abi-internal.md).
//
//	RAX, RBX, RCX, RDI, RSI, R8-R11 = int args (9 regs)
//	R12, R13 = scratch
//	R14 = $g$ goroutine pointer (NEVER clobber)
//	R15 = GOT ref (scratch in static builds)
//	RBP = frame pointer
//	RSP = stack pointer (8-byte aligned)
//	RDX = closure context pointer
//	X0-X14 = float args
//	X15 = zero register
//
// Register encoding for ModRM and opcode fields (3-bit):
//
//	0=RAX, 1=RCX, 2=RDX, 3=RBX, 4=RSP, 5=RBP, 6=RSI, 7=RDI
//	R8-R15 use base encoding 0-7 with REX.B/R bit extension.
const (
	// Integer argument registers
	REG_RAX = 0
	REG_RBX = 3
	REG_RCX = 1
	REG_RDI = 7
	REG_RSI = 6
	REG_R8  = 8
	REG_R9  = 9
	REG_R10 = 10
	REG_R11 = 11

	// Scratch registers
	REG_R12 = 12
	REG_R13 = 13

	// Special registers
	REG_R14 = 14 // $g$ goroutine pointer — NEVER clobber
	REG_R15 = 15 // GOT ref (scratch in static builds)
	REG_RBP = 5  // Frame pointer
	REG_RSP = 4  // Stack pointer (8-byte aligned)
	REG_RDX = 2  // Closure context pointer

	// Floating-point argument registers (encoded 0-15)
	REG_X0  = 0
	REG_X1  = 1
	REG_X2  = 2
	REG_X3  = 3
	REG_X4  = 4
	REG_X5  = 5
	REG_X6  = 6
	REG_X7  = 7
	REG_X8  = 8
	REG_X9  = 9
	REG_X10 = 10
	REG_X11 = 11
	REG_X12 = 12
	REG_X13 = 13
	REG_X14 = 14

	REG_X15 = 15 // Zero register (XORPS destination)
)
