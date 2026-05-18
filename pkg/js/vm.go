// vm.go — Register-based bytecode virtual machine (Ignition-style).
//
// The VM executes bytecode produced by the Compiler. Each function frame
// gets a virtual register file ([]JSValue). The accumulator register (acc)
// is the implicit target for most operations.
//
// Usage:
//   vm := NewVM()
//   result := vm.Run("1 + 2")
//   fmt.Println(result.ToNumber()) // 3
package js

import (
"sync"
"sync/atomic"
"unsafe"
)

// nextRealmID is an atomic counter used to assign a unique RealmID to each VM.
var nextRealmID uint64

// globalPrototypeRealm maps prototype objects to the RealmID of the VM that created them.
// Used by opInstanceof to detect cross-realm prototype chain comparisons.
var (
	globalPrototypeRealmMu sync.RWMutex
	globalPrototypeRealm   = make(map[*JSObject]uint64)
)

// VMFrame holds execution state for a single function activation.
type VMFrame struct {
	Func     *BytecodeFunction
	Regs     []JSValue    // Virtual register file
	Acc      JSValue      // Accumulator (implicit register)
	PC       int          // Program counter (index into Instructions)
	Parent   *VMFrame     // Caller frame (for return)
	ICVector *FeedbackVector // Inline caching data for this function
	ICSlots  map[int]int  // property access site → IC slot index (populated by compiler)

	// Closure support.
	ClosureEnv *JSObject // captured environment for closures

	// this binding.
	This JSValue // current this value (JSValue for flexibility)

	// Exception handling.
	Thrown     JSValue // current exception value
	HandlerPC  int     // PC of catch handler (-1 if none)
	FinallyPC  int     // PC of finally handler (-1 if none)
	SavedAcc   JSValue // saved accumulator for finally return interception

	// For-in iteration state.
	forInObj    *JSObject // object being iterated
	forInKeys   []string  // keys to iterate over
	forInIndex  int       // current key index

	// Return signal for function table dispatch.
	ShouldReturn bool

	// Generator state (non-nil for generator frames).
	GenState *GeneratorState

	// Constructor tracking for derived classes.
	SuperCalled bool // true after super() has been called in derived constructors

	// Sparkplug / OSR tracking.
	InSparkplug bool // true when executing in Sparkplug native code
	InTurboFan  bool // true when executing in TurboFan native code

	// ShadowStack is a per-frame shadow stack for JIT-compiled native code.
	// When Sparkplug generates native ARM64/x86-64 code, JS object pointers
	// in CPU registers are invisible to Go's GC. The JIT prologue stores
	// this *jit.ShadowStack pointer and spills object pointers to it so the
	// GC can trace them. Set via jit.PrologueSetup, cleared via jit.EpilogueTeardown.
	ShadowStack unsafe.Pointer // *jit.ShadowStack, visible to Go GC scanner


}

// VM is the GoV8 virtual machine.
type VM struct {
	mu sync.RWMutex // protects globals, registry, console, calltrack, events, nextICSlot

	// RealmID uniquely identifies this VM's realm for cross-realm instanceof checks.
	RealmID uint64

	// DisableJIT prevents JIT compilation and native code dispatch.
	// Set to true in benchmarks or tests where JIT is not desired.
	DisableJIT bool

	// calltrack manages call depth and stack traces for Error.stack.
	calltrack *CallTracker

	// stepCount tracks the total bytecode instructions executed in the current
	// top-level Run/Execute. Resets at the start of each top-level call.
	stepCount int

	// alloc is the bump allocator for frames, registers, and objects.
	alloc *Allocator

	// globals stores the global scope with slot-based fast path.
	globals *GlobalStore

	// console buffers log output and forwards to optional callback.
	console *Console

	// registry stores function registrations and bytecode cache.
	registry *Registry

	// Inline caching support.
	nextICSlot int

	// Async work tracking: WaitGroup for goroutines spawned by async builtins.
	// Tests can call WaitAsync() to wait for all pending async work.
	asyncWg sync.WaitGroup

	// events manages DOM event listeners, event queue, window.onload,
	// element lookup, and DOM mutation callbacks.
	events *EventSystem

	// moduleRegistry is the ES module registry (nil if no module support).
	moduleRegistry *ModuleRegistry

	// promiseReactions stores queued .then()/.catch() callbacks for
	// pending promises. Keyed by the promise object; drained on settle.
	promiseReactions map[*JSObject][]promiseReaction

	// JIT backend interfaces (injected at construction, nil for pure interpreter).
	compiler  JITCompiler
	patcher   ICPatcher
	protector ExecProtector

	// jitErrors accumulates JIT compilation errors for diagnostics.
	jitErrors []error
}

// NewVM creates a new GoV8 virtual machine.
func NewVM() *VM {
	vm := &VM{
		RealmID:        atomic.AddUint64(&nextRealmID, 1),
		alloc:          NewAllocator(),
		globals:        NewGlobalStore(),
		calltrack:      &CallTracker{},
		console:        NewConsole(),
		events:         NewEventSystem(),
		registry:       NewRegistry(),
		promiseReactions: make(map[*JSObject][]promiseReaction),
	}
	// Pre-create and cache the global object for reuse as `this`.
	globalThis := NewObject(NewJSObject())
	globalThis.ObjVal.ConstructorName = "Global"
	vm.globals.SetGlobalThis(globalThis)
	vm.RegisterBuiltins()
	vm.tagAllPrototypes()
	return vm
}

// NewVMWithJIT creates a VM with JIT compilation support injected via
// interfaces. The same backend value can be passed for all three interfaces
// if it implements JITCompiler, ICPatcher, and ExecProtector.
//
// Pass nil for any interface to disable that JIT capability. For a pure
// interpreter VM without any JIT overhead, use NewVM().
func NewVMWithJIT(compiler JITCompiler, patcher ICPatcher, protector ExecProtector) *VM {
	vm := NewVM()
	vm.compiler = compiler
	vm.patcher = patcher
	vm.protector = protector
	return vm
}


// pushCallName resolves the callee's display name and pushes it onto vm.callStack
// for Error.stack trace construction. Called from opCallFast/opCall/opCallSpread
// before calling callMethod. frame is the current VM frame (for source position lookup).
func (vm *VM) pushCallName(callee JSValue, frame *VMFrame) {
	csFrame := CallStackFrame{}
	if callee.IsObject() && callee.ObjVal != nil {
		if callee.ObjVal.Bytecode != nil && callee.ObjVal.Bytecode.Name != "" {
			csFrame.Name = callee.ObjVal.Bytecode.Name
			csFrame.File = callee.ObjVal.Bytecode.SourceFile
		} else if callee.ObjVal.ConstructorName != "" && callee.ObjVal.ConstructorName != "Function" {
			csFrame.Name = callee.ObjVal.ConstructorName
		}
	}
	if csFrame.Name == "" {
		csFrame.Name = "<anonymous>"
	}
	// Look up source position from current VM frame's bytecode PC, if available.
	if frame != nil && frame.Func != nil {
		if len(frame.Func.SourcePositions) > frame.PC {
			pos := frame.Func.SourcePositions[frame.PC]
			csFrame.Line = pos.Line
			csFrame.Col = pos.Col
		}
		if csFrame.File == "" {
			csFrame.File = frame.Func.SourceFile
		}
	}
	vm.calltrack.Push(csFrame)
}

// popCallName removes the top entry from vm.callStack.
// Called from opReturn (bytecode functions) and from opCallFast/opCall/opCallSpread
// after callMethod returns for built-in functions.
func (vm *VM) popCallName() {
	vm.calltrack.Pop()
}
