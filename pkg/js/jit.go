// Package js — JIT compiler interfaces for dependency injection.
//
// These interfaces decouple the VM from concrete JIT implementations,
// replacing the previous package-level function pointer hooks. JIT
// backends implement these interfaces and are injected at VM construction
// via NewVMWithJIT.
package js

import "unsafe"

// JITCompiler compiles bytecode functions to native machine code.
type JITCompiler interface {
	CompileSparkplug(bf *BytecodeFunction) (rxAddr uintptr, err error)
	CompileTurboFan(bf *BytecodeFunction) (rxAddr uintptr, err error)
}

// ICPatcher patches inline cache slots in JIT-compiled code at runtime.
type ICPatcher interface {
	PatchMonomorphic(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int)
	PatchPolymorphic(rxAddr uintptr, slotIdx int, shapes []unsafe.Pointer, offsets []int)
	PatchMegamorphic(rxAddr uintptr, slotIdx int)
	PatchStore(rxAddr uintptr, slotIdx int, shapePtr unsafe.Pointer, propOffset int)
}

// ExecProtector manages memory protection for JIT code execution.
// On Apple Silicon, this toggles pthread_jit_write_protect_np to switch
// between writable and executable states. On Linux with dual-mapping,
// this is a no-op.
type ExecProtector interface {
	EnableExec()  // called before executing JIT code
	DisableExec() // called after executing JIT code
}
