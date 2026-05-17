//nolint:revive // ALL_CAPS register names follow ARM64 ISA convention
package jit

// ARM64 registers per Go ABI and wazero arm64/abi.go conventions.
//
//	R0-R7   : Go ABI int argument/return registers
//	R8-R17  : Caller-saved scratch (free for JIT use)
//	R18     : RESERVED (macOS platform register)
//	R19-R26 : Callee-saved — used as VM registers
//	R27     : RESERVED (assembler tmpReg in Go/wazero)
//	R28     : $g$ goroutine pointer (NEVER clobber)
//	R29     : Frame pointer
//	R30     : Link register (return address)
//	R31     : Stack pointer (16-byte aligned) / Zero register
const (
	// Go ABI int argument/return registers
	REG_R0 = 0
	REG_R1 = 1
	REG_R2 = 2
	REG_R3 = 3
	REG_R4 = 4
	REG_R5 = 5
	REG_R6 = 6
	REG_R7 = 7

	// Caller-saved scratch (free for JIT use)
	REG_R8  = 8
	REG_R9  = 9
	REG_R10 = 10
	REG_R11 = 11
	REG_R12 = 12
	REG_R13 = 13
	REG_R14 = 14
	REG_R15 = 15
	REG_R16 = 16
	REG_R17 = 17

	// R18 = RESERVED (macOS platform register)

	// Callee-saved — used as VM registers r0-r7
	REG_VM0 = 19
	REG_VM1 = 20
	REG_VM2 = 21
	REG_VM3 = 22
	REG_VM4 = 23
	REG_VM5 = 24
	REG_VM6 = 25
	REG_VM7 = 26

	// R27 = RESERVED (assembler tmpReg in Go/wazero)

	REG_G  = 28 // $g$ goroutine pointer — NEVER clobber
	REG_FP = 29 // Frame pointer
	REG_LR = 30 // Link register (return address)
	REG_SP = 31 // Stack pointer (16-byte aligned)
	REG_ZR = 31 // Zero register (same encoding as SP)

	// D-registers (64-bit floating-point). Encoded as Vn in instructions.
	REG_D0 = 0
	REG_D1 = 1
	REG_D2 = 2
	REG_D3 = 3
)
