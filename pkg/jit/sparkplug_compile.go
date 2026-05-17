//go:build !amd64

package jit

import (
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

func init() {
	// Register SparkplugCompile in the jit API, then bridge to the VM's
	// plugin hook to avoid pkg/js ↔ pkg/jit import cycles.
	SparkplugCompile = func(bf interface{}) (uintptr, error) {
		code, err := CompileSparkplug(bf.(*js.BytecodeFunction))
		if err != nil {
			return 0, err
		}
		if code == nil {
			return 0, nil
		}
		return code.RXAddr(), nil
	}

	// Bridge to VM plugin hook. Calls through SparkplugCompile so the
	// implementation stays DRY.
	js.SparkplugCompiler = func(bf *js.BytecodeFunction) (uintptr, error) {
		return SparkplugCompile(bf)
	}
}

// CompileSparkplug compiles a BytecodeFunction into ARM64 machine code.
// Returns a CodeBuf containing the executable code, or an error.
//
// The generated code follows the Go ABI calling convention:
//
//	func sparkplugEntry(frame *js.VMFrame)
//
// R0 = frame pointer on entry.
func CompileSparkplug(bf *js.BytecodeFunction) (*CodeBuf, error) {
	// Estimate code size: each bytecode → ~64 bytes ARM64 on average
	estSize := len(bf.Instructions)*96 + 256
	buf, err := NewCodeBuf(estSize)
	if err != nil {
		return nil, err
	}
	as := NewAssembler(buf)

	// --- Prologue: set up native frame ---
	// R0 holds the *js.VMFrame argument from the Go caller.
	// Save LR in callee-saved R20 (REG_VM1) so BLR calls don't corrupt it.
	// Store frame pointer in callee-saved R19 (REG_VM0).
	as.MOV(REG_VM1, REG_LR) // R20 = LR (save return address)
	as.MOV(REG_VM0, REG_R0) // R19 = frame*

	// --- Pre-create labels for all PC targets ---
	labels := make(map[int]*Label)
	for pc := range bf.Instructions {
		labels[pc] = NewLabel()
	}
	epilogue := NewLabel()
	deoptStub := NewLabel() // pre-create; bound after epilogue

	// --- IC slot offset tracking ---
	numSlots := 0
	if bf.ICVector != nil {
		numSlots = len(bf.ICVector.Slots)
	}
	icSlotOffsets := make([]int, numSlots)
	for i := range icSlotOffsets {
		icSlotOffsets[i] = -1
	}

	// --- PC mapping for deoptimization ---
	pcToNative := make(map[int]int)
	nativeToPc := make(map[int]int)

	// --- Compile each bytecode instruction ---
	for pc, instr := range bf.Instructions {
		as.Bind(labels[pc])
		nativeOff := as.Pos()
		pcToNative[pc] = nativeOff
		nativeToPc[nativeOff] = pc
		emitSparkplugOp(as, &instr, bf, labels, epilogue, deoptStub, pc, icSlotOffsets)
	}

	// Store PC mapping on the BytecodeFunction for deoptimization.
	bf.PcToNative = pcToNative
	bf.NativeToPc = nativeToPc

	// --- Epilogue: restore LR and return to caller ---
	as.Bind(epilogue)
	as.MOV(REG_LR, REG_VM1) // LR = R20 (restore return address)
	as.RET()

	// --- Deoptimization stub (bound after epilogue; only reached via branch) ---
	emitDeoptStubAt(as, deoptStub)

	// Build DeoptimizationInputData with RegMap: hardware reg → virtual reg.
	// For Sparkplug, the mapping is 1:1 for callee-saved VM registers;
	// scratch regs (R0-R17) may hold spilled virtual regs.
	deoptData := &DeoptimizationInputData{
		Points: make([]DeoptPoint, 0, len(bf.Instructions)),
	}
	for pc, nativeOff := range pcToNative {
		deoptData.Points = append(deoptData.Points, DeoptPoint{
			NativeOffset: nativeOff,
			BytecodePC:   pc,
		})
	}
	// Default RegMap: identity mapping (hwReg → vmReg = hwReg for scratch,
	// mapping callee-saved VM regs to virtual regs 0-7).
	for i := range deoptData.RegMap {
		deoptData.RegMap[i] = i
	}
	// VM registers in callee-saved R19-R26 map to virtual regs 0-7
	for i := 0; i < 8; i++ {
		deoptData.RegMap[REG_VM0+i] = i
	}
	bf.DeoptData = deoptData

	// Register code buffer for runtime IC patching.
	rxAddr := buf.RXAddr()
	RegisterCodeBuf(rxAddr, buf, icSlotOffsets)

	return buf, nil
}

// --- Deoptimization stub ---

// emitDeoptStub emits a deoptimization bailout point. When a type guard
// fails in JIT code, the slow path branches here. The stub clears the
// InSparkplug flag and returns to the Go caller. The interpreter resumes
// from the current frame.PC, re-executing the failing instruction with
// the correct frame state.
//
// No FrameDescription or GoDeoptimize call is needed because:
//   - For arithmetic slow paths: frame.Acc is unmodified (fast path only
//     stores after successful float op).
//   - For call ops: the interpreter handles call setup via the normal
//     dispatch loop.
//   - For other ops: frame.PC was set before native dispatch; the
//     interpreter picks up from there.
func emitDeoptStubAt(as *Assembler, stub *Label) {
	as.Bind(stub)

	// Clear frame.InSparkplug (1 byte bool) so the Go caller falls through
	// to the interpreter. Use STRB (store byte) to avoid overwriting
	// adjacent fields (InTurboFan and ShadowStack pointer).
	as.STRB(REG_ZR, REG_VM0, inSparkplugOff)

	// Restore LR from REG_VM1 and return to Go caller.
	as.MOV(REG_LR, REG_VM1)
	as.RET()
}

// --- Call helper: load function address and emit BLR ---

// emitCall loads the address of a Go function into a register and calls it.
// On ARM64 macOS/Linux, Go functions use the standard AAPCS64 calling convention.
func emitCall(as *Assembler, fn interface{}) {
	addr := funcToAddr(fn)
	// Load the 64-bit address into R16 (scratch) using MOVZ + MOVK sequence.
	as.MOVZ(REG_R16, uint16(addr&0xFFFF), 0)
	as.MOVK(REG_R16, uint16((addr>>16)&0xFFFF), 16)
	as.MOVK(REG_R16, uint16((addr>>32)&0xFFFF), 32)
	as.MOVK(REG_R16, uint16((addr>>48)&0xFFFF), 48)
	as.BLR(REG_R16)
}

// emitSparkplugEpilogue emits the standard Sparkplug function epilogue:
// restores callee-saved registers and returns to the caller.
// frameSize is reserved for future use (stack frame teardown).
func emitSparkplugEpilogue(as *Assembler, frameSize int) {
	_ = frameSize // reserved for future stack frame teardown
	as.RET()
}

// funcToAddr extracts the underlying function pointer from a Go function value.
// A Go function value (funcval) is a pointer to a structure containing the
// actual code pointer at offset 0. We use unsafe to extract it.
func funcToAddr(fn interface{}) uintptr {
	// A Go interface is a two-word structure: type pointer + data pointer.
	// For a func value stored in an interface{}, the data pointer points to
	// a funcval struct whose first word is the code pointer.
	type funcval struct {
		pc uintptr
	}
	return (*funcval)((*[2]unsafe.Pointer)(unsafe.Pointer(&fn))[1]).pc
}
