// vm_jit.go — JIT compilation and native code execution.
package js

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"unsafe"
)

// vmJITMu serialises access to vm.jitErrors for concurrent background compilation.
var vmJITMu sync.Mutex

// jitCompileSem bounds concurrent JIT compilation goroutines to GOMAXPROCS.
var jitCompileSem = make(chan struct{}, runtime.GOMAXPROCS(0))

// maybePromoteTier increments the call count and triggers JIT compilation
// when thresholds are reached. Sparkplug and TurboFan compilation run in
// background goroutines to avoid blocking the interpreter.
func (vm *VM) maybePromoteTier(bf *BytecodeFunction) {
	if vm.DisableJIT {
		return
	}
	bf.CallCount++
	switch {
	case bf.CallCount == Tier0SparkplugThreshold && bf.Sparkplug == 0 && vm.compiler != nil:
		if bf.CompilingJIT {
			return // already being compiled
		}
		bf.CompilingJIT = true
		go func() {
			jitCompileSem <- struct{}{}
			defer func() {
				<-jitCompileSem
				bf.CompilingJIT = false
			}()
			rxAddr, err := vm.compiler.CompileSparkplug(bf)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[V8Go] Sparkplug compile error: %v\n", err)
				vmJITMu.Lock()
				vm.jitErrors = append(vm.jitErrors, fmt.Errorf("sparkplug: %w", err))
				vmJITMu.Unlock()
			} else if rxAddr != 0 {
				bf.Sparkplug = rxAddr
				bf.HasJITTier = true
			}
		}()
	case bf.CallCount == Tier1TurboFanThreshold && bf.ICVector != nil && bf.TurboFan == 0 && vm.compiler != nil:
		if bf.CompilingJIT {
			return // already being compiled
		}
		bf.CompilingJIT = true
		go func() {
			jitCompileSem <- struct{}{}
			defer func() {
				<-jitCompileSem
				bf.CompilingJIT = false
			}()
			rxAddr, err := vm.compiler.CompileTurboFan(bf)
			if err == nil && rxAddr != 0 {
				bf.TurboFan = rxAddr
				bf.HasJITTier = true
			}
		}()
	}
}

// osrToSparkplug transitions an interpreter frame to Sparkplug native execution
// at the current PC (which points to a loop back-edge target). Saves the frame
// state (PC already advanced past the jump instruction) and invokes the native
// code entry point.
func (vm *VM) osrToSparkplug(frame *VMFrame) {
	frame.InSparkplug = true
	// The PC is already set to the jump target (opJump handler set it).
	// executeSparkplug picks up at the current PC.
	vm.executeSparkplug(frame)
}

// sparkplugFunc is the native function signature for Sparkplug-compiled code.
// Takes a *VMFrame in R0 per Go ABI.
type sparkplugFunc func(frame *VMFrame)

// turbofanFunc is the native function signature for TurboFan-compiled code.
// Takes a *VMFrame in R0 per Go ABI.
type turbofanFunc func(frame *VMFrame)

// executeTurboFan invokes the TurboFan native code for the frame's function.
// The native code reads frame.PC to resume at the correct bytecode offset,
// executes until completion (or deopt bailout), and sets frame.PC past the end
// of instructions when done.
func (vm *VM) executeTurboFan(frame *VMFrame) {
	if frame == nil || frame.Func == nil || frame.Func.TurboFan == 0 {
		return
	}
	bf := frame.Func
	// Toggle per-thread JIT protection: enable exec (true), restore write (false).
	if vm.protector != nil {
		vm.protector.EnableExec()
		defer vm.protector.DisableExec()
	}
	frame.InTurboFan = true
	// Construct a Go function value from the raw code address (same pattern
	// as executeSparkplug).
	type funcval struct {
		pc uintptr
	}
	fv := &funcval{pc: bf.TurboFan}
	var fn turbofanFunc
	*(**funcval)(unsafe.Pointer(&fn)) = fv
	fn(frame)
}

// osrToTurboFan transitions an interpreter frame to TurboFan native execution
// at the current PC (which points to a loop back-edge target). Saves the frame
// state (PC already advanced past the jump instruction) and invokes the native
// code entry point.
func (vm *VM) osrToTurboFan(frame *VMFrame) {
	frame.InTurboFan = true
	// The PC is already set to the jump target (opJump handler set it).
	// executeTurboFan picks up at the current PC.
	vm.executeTurboFan(frame)
}

// executeSparkplug invokes the Sparkplug native code for the frame's function.
// The native code reads frame.PC to resume at the correct bytecode offset,
// executes until completion (or OSR bailout), and sets frame.PC past the end
// of instructions when done.
func (vm *VM) executeSparkplug(frame *VMFrame) {
	if frame == nil || frame.Func == nil || frame.Func.Sparkplug == 0 {
		return
	}
	bf := frame.Func
	// Toggle per-thread JIT protection: enable exec (true), restore write (false).
	if vm.protector != nil {
		vm.protector.EnableExec()
		defer vm.protector.DisableExec()
	}
	// Construct a Go function value from the raw code address.
	type funcval struct {
		pc uintptr
	}
	fv := &funcval{pc: bf.Sparkplug}
	var fn sparkplugFunc
	*(**funcval)(unsafe.Pointer(&fn)) = fv
	fn(frame)
}

// GoDeoptimize is called from the JIT deoptimization stub when a type guard
// fails. It reconstructs the interpreter frame from the FrameDescription
// saved on the native stack, clears the InSparkplug flag so the caller
// re-enters the interpreter loop, and restores the correct bytecode PC
// from the deoptimization point data.
//
// desc points to a *jit.FrameDescription. We use unsafe.Pointer with
// known offsets to avoid a pkg/js → pkg/jit import cycle.
//
// FrameDescription layout (must match pkg/jit/deopt.go):
//
//	offset 0:   Regs[0..29] — 30 × uint64 = 240 bytes
//	offset 240: PC          — int (8 bytes)
//	offset 248: FP          — uintptr (*VMFrame)
//	offset 256: SP          — uintptr
//	offset 264: LR          — uint64
//
// Total: 272 bytes (34 × 8).
func GoDeoptimize(desc unsafe.Pointer) {
	// Full FrameDescription layout.
	type fdFull struct {
		regs [30]uint64
		pc   int
		fp   unsafe.Pointer
		sp   uintptr
		lr   uint64
	}
	fd := (*fdFull)(desc)
	frame := (*VMFrame)(fd.fp)

	// Recover the Acc value from saved registers.
	// R0 (fd.regs[0]) holds the Acc JSValue as 8 consecutive uint64 values
	// because our deopt stub saves all scratch regs including those holding
	// the accumulator value. Copy the first 8 uint64s into frame.Acc.
	accPtr := (*[8]uint64)(unsafe.Pointer(&frame.Acc))
	for i := 0; i < 8 && i < len(fd.regs); i++ {
		accPtr[i] = fd.regs[i]
	}

	// Look up the bytecode PC from the deoptimization data.
	// The deopt stub stores a placeholder PC; we resolve from the native offset
	// back to bytecode PC via the PcToNative/NativeToPc maps.
	if frame.Func != nil {
		// Try NativeToPc reverse lookup using the native PC from fd.pc.
		if frame.Func.NativeToPc != nil {
			if bcPC, ok := frame.Func.NativeToPc[fd.pc]; ok {
				frame.PC = bcPC
			}
		}
		// Also try DeoptData for structured deopt point resolution.
		if frame.Func.DeoptData != nil {
			type deoptResolver interface {
				FindDeoptPoint(nativeOffset int) (int, bool)
			}
			if d, ok := frame.Func.DeoptData.(deoptResolver); ok {
				if bcPC, found := d.FindDeoptPoint(fd.pc); found {
					frame.PC = bcPC
				}
			}
		}
	}

	// Clear InSparkplug so the caller (execute / executeFrame) falls back
	// to the bytecode interpreter.
	frame.InSparkplug = false

	// Deoptimization counter: if the function deopts too many times, reset
	// all JIT tiers so it can be recompiled with fresh type feedback.
	if frame.Func != nil {
		frame.Func.DeoptCount++
		if frame.Func.DeoptCount >= 5 {
			frame.Func.Sparkplug = 0
			frame.Func.TurboFan = 0
			frame.Func.HasJITTier = false
			frame.Func.DeoptCount = 0
			frame.Func.CompilingJIT = false
		}
	}
}
