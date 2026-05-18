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
	"math"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/dom"
)

// nextRealmID is an atomic counter used to assign a unique RealmID to each VM.
var nextRealmID uint64

// globalPrototypeRealm maps prototype objects to the RealmID of the VM that created them.
// Used by opInstanceof to detect cross-realm prototype chain comparisons.
var (
	globalPrototypeRealmMu sync.RWMutex
	globalPrototypeRealm   = make(map[*JSObject]uint64)
)

// allocFrame obtains a VMFrame from the Allocator's pre-allocated buffer.
func (vm *VM) allocFrame() *VMFrame {
	return vm.alloc.AllocFrame()
}

// freeFrame releases a VMFrame back to the Allocator.
func (vm *VM) freeFrame(f *VMFrame) {
	vm.alloc.FreeFrame(f)
}

// equalFold performs case-insensitive string comparison without allocation.
func equalFold(a, b string) bool {
	return len(a) == len(b) && strings.EqualFold(a, b)
}

// allocRegs obtains a []JSValue slice from the Allocator's register buffer.
func (vm *VM) allocRegs(n int) []JSValue {
	return vm.alloc.AllocRegs(n)
}

// freeRegs releases a register slice back to the Allocator.
func (vm *VM) freeRegs(regs []JSValue) {
	vm.alloc.FreeRegs(regs)
}

// allocObj obtains a *JSObject from the Allocator.
func (vm *VM) allocObj() *JSObject {
	return vm.alloc.AllocObj()
}

// freeObj releases a JSObject back to the Allocator.
func (vm *VM) freeObj(obj *JSObject) {
	vm.alloc.FreeObj(obj)
}

// CallStackFrame represents a single frame in the JavaScript call stack.
type CallStackFrame struct {
	Name string
	File string
	Line int
	Col  int
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
	vm.callStack = append(vm.callStack, csFrame)
}

// popCallName removes the top entry from vm.callStack.
// Called from opReturn (bytecode functions) and from opCallFast/opCall/opCallSpread
// after callMethod returns for built-in functions.
func (vm *VM) popCallName() {
	if len(vm.callStack) > 0 {
		vm.callStack = vm.callStack[:len(vm.callStack)-1]
	}
}

// calleeGoesToBytecode returns true if calling this callee will enter executeFrame
// (as opposed to a built-in CallFunc which returns synchronously).
// Async and generator functions go through a different path (createAsyncFunction /
// createGeneratorObject) — they also need the pop in the op handler, not in opReturn.
func calleeGoesToBytecode(callee JSValue) bool {
	if !callee.IsObject() || callee.ObjVal == nil {
		return false
	}
	obj := callee.ObjVal
	// CallFunc takes precedence — if set, it's a built-in, not bytecode.
	if obj.CallFunc != nil {
		return false
	}
	// Bytecode with no CallFunc means it goes through executeFrame→opReturn.
	if obj.Bytecode != nil {
		// Async and generator functions don't go through executeFrame directly.
		if obj.Bytecode.Async || obj.Bytecode.Generator {
			return false
		}
		return true
	}
	return false
}

// GeneratorState holds the saved execution state of a paused generator.
type GeneratorState struct {
	SavedPC      int        // PC to resume from (after the yield)
	SavedRegs    []JSValue  // copy of register file at yield point
	SavedAcc     JSValue    // saved accumulator
	SavedThis    JSValue    // saved this binding
	Done         bool       // true when generator has completed
	GenObj       *JSObject  // the generator object (to return {value, done})
}

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

// opHandler is the function signature for bytecode opcode handlers.
// Used in the function table dispatch (opTable) instead of a switch statement.
type opHandler func(vm *VM, frame *VMFrame, instr Instruction)

// QueuedEvent represents a DOM event queued for dispatch in the VM.
// When the browser fires a lifecycle event (DOMContentLoaded, load), it
// enqueues the event here and the VM executes matching listeners.
type QueuedEvent struct {
	Type   string
	Target string
	Data   map[string]JSValue
}

// maxCallDepth limits recursion to prevent Go stack overflow from
// infinite JS recursion (e.g., function f(){f()};f()). Chrome V8 uses
// a similar limit; exceeding it returns Undefined.
// Set below Tier0SparkplugThreshold (100) to avoid hitting JIT code
// during deep recursion, which can crash on macOS ARM64 without JIT
// entitlement.
const maxCallDepth = 80

// maxSteps limits the total number of bytecode instructions executed
// in a single Run/Execute call. Prevents infinite loops from hanging
// the VM (e.g., while(true){} or malformed jump targets).
const maxSteps = 10_000_000

// Tier-up thresholds for multi-tier JIT compilation.
// When a function is called Tier0SparkplugThreshold times, it is compiled
// with the Sparkplug baseline JIT. At Tier1TurboFanThreshold calls, it
// becomes a candidate for TurboFan optimizing compilation.
const (
	Tier0SparkplugThreshold = 100
	Tier1TurboFanThreshold  = 1000
)

// VM is the GoV8 virtual machine.
type VM struct {
	mu sync.Mutex // protects globals, funcRegistry, builtins, consoleLog, nextICSlot, listeners, eventQueue, onloadHandler, bytecodeCache

	// RealmID uniquely identifies this VM's realm for cross-realm instanceof checks.
	RealmID uint64

	// DisableJIT prevents JIT compilation and native code dispatch.
	// Set to true in benchmarks or tests where JIT is not desired.
	DisableJIT bool

	// callDepth tracks current call stack depth to prevent infinite recursion
	// from overflowing the Go stack. Incremented on each callMethod→executeFrame,
	// decremented on return.
	callDepth int

	// callStack tracks stack frames for Error.stack traces.
	// Pushed in opCallFast/opCall/opCallSpread before callMethod;
	// popped in opReturn (bytecode) or after callMethod returns (built-ins).
	callStack []CallStackFrame

	// stepCount tracks the total bytecode instructions executed in the current
	// top-level Run/Execute. Resets at the start of each top-level call.
	stepCount int

	// alloc is the bump allocator for frames, registers, and objects.
	alloc *Allocator

	// Global object holds global variables.
	globals map[string]JSValue

	// globalSlots is a fixed-size array for fast global variable access.
	// Slot indices are assigned at compile time (see assignGlobalSlots).
	// Direct array indexing: globalSlots[slot] = value (no hash lookup).
	globalSlots []JSValue
	globalSlotsSet  []bool   // tracks which slots have been written (vs zero-value)
	globalSlotNames []string // name at each slot index, for SetGlobal write-through

	// Console output callback.
	consoleOutput func(string)
	consoleLog    []string // accumulated log lines

	// Function registry: maps function names to bytecode.
	funcRegistry map[string]*BytecodeFunction

	// Built-in functions.
	builtins map[string]func(args []JSValue) JSValue

	// Inline caching support.
	nextICSlot int

	// Bytecode cache: avoids re-parsing and re-compiling the same source.
	bytecodeCache map[string]*BytecodeFunction

	// Async work tracking: WaitGroup for goroutines spawned by async builtins.
	// Tests can call WaitAsync() to wait for all pending async work.
	asyncWg sync.WaitGroup

	// Event listener storage: maps event type → list of callbacks.
	// Preallocated at known capacity (4: DOMContentLoaded × 2, load × 2).
	listeners map[string][]JSValue

	// Event queue for deferred event dispatch.
	eventQueue []QueuedEvent

	// window.onload handler (set via property setter).
	onloadHandler JSValue

	// elementByID is a callback set by the browser to look up DOM elements by ID.
	// Used by DispatchEvent to find the target element and its inline handlers.
	elementByID func(string) *dom.Element

	// domChangeCallback is invoked when the VM dispatches an inline event handler
	// that may have mutated the DOM. The browser wires this to trigger a repaint.
	domChangeCallback func()

	// moduleRegistry is the ES module registry (nil if no module support).
	moduleRegistry *ModuleRegistry

	// globalThis is a cached reference to the global object, avoiding
	// per-execution allocations when setting up the `this` binding.
	globalThis JSValue

	// promiseReactions stores queued .then()/.catch() callbacks for
	// pending promises. Keyed by the promise object; drained on settle.
	promiseReactions map[*JSObject][]promiseReaction
}

// NewVM creates a new GoV8 virtual machine.
func NewVM() *VM {
	vm := &VM{
		RealmID:        atomic.AddUint64(&nextRealmID, 1),
		alloc:          NewAllocator(),
		globals:        make(map[string]JSValue, 128),
		globalSlots:    make([]JSValue, 0, 64),
		consoleLog:     make([]string, 0),
		funcRegistry:   make(map[string]*BytecodeFunction),
		builtins:       make(map[string]func(args []JSValue) JSValue),
		listeners:      make(map[string][]JSValue, 4),
		eventQueue:     make([]QueuedEvent, 0, 8),
		bytecodeCache:      make(map[string]*BytecodeFunction),
		promiseReactions: make(map[*JSObject][]promiseReaction),
	}
	// Pre-create and cache the global object for reuse as `this`.
	vm.globalThis = NewObject(NewJSObject())
	vm.globalThis.ObjVal.ConstructorName = "Global"
	vm.RegisterBuiltins()
	vm.tagAllPrototypes()
	return vm
}

// tagAllPrototypes walks the VM's globals and tags all built-in prototype objects
// with this VM's RealmID for cross-realm instanceof detection.
//
// Package-level prototypes (ObjectPrototype, ArrayPrototype, etc.) are shared
// across all VM instances. We intentionally skip tagging them here — each new
// VM would overwrite the previous VM's RealmID, causing false cross-realm
// detection. For shared prototypes, opInstanceof relies on pointer comparison
// (same pointer across VMs) rather than RealmID+ConstructorName fallback.
func (vm *VM) tagAllPrototypes() {
	// Skip shared package-level prototypes. These are the same *JSObject
	// pointers across all VMs, so tagging them with per-VM RealmID would
	// cause false cross-realm positives.
	// Instead, iterate globals and tag only per-VM prototypes (those created
	// during RegisterBuiltins — error prototypes, constructor prototypes, etc.).
	for _, val := range vm.globals {
		if val.IsObject() && val.ObjVal != nil {
			protoVal := val.ObjVal.Get("prototype")
			if protoVal.IsObject() && protoVal.ObjVal != nil {
				vm.tagPrototype(protoVal.ObjVal)
			}
		}
	}
}

// tagPrototype records that a prototype object belongs to this VM's realm.
func (vm *VM) tagPrototype(obj *JSObject) {
	if obj != nil {
		globalPrototypeRealmMu.Lock()
		globalPrototypeRealm[obj] = vm.RealmID
		globalPrototypeRealmMu.Unlock()
	}
}

// getObjectRealm returns the RealmID tagged on an object, or 0 if not tagged.
func (vm *VM) getObjectRealm(obj *JSObject) uint64 {
	if obj == nil {
		return 0
	}
	globalPrototypeRealmMu.RLock()
	r := globalPrototypeRealm[obj]
	globalPrototypeRealmMu.RUnlock()
	return r
}

// SetConsoleOutput sets a callback for console.log output.

// SetConsoleOutput sets a callback for console.log output.
func (vm *VM) SetConsoleOutput(fn func(string)) {
	vm.consoleOutput = fn
}

// GetModuleRegistry returns the module registry, creating one if needed.
func (vm *VM) GetModuleRegistry() *ModuleRegistry {
	if vm.moduleRegistry == nil {
		vm.moduleRegistry = NewModuleRegistry(vm, nil)
	}
	return vm.moduleRegistry
}

// Run parses and executes JavaScript source, returning the final accumulator value.
func (vm *VM) Run(source string) JSValue {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Check bytecode cache first.
	if bf, ok := vm.bytecodeCache[source]; ok {
		vm.maybePromoteTier(bf)
		return vm.execute(bf)
	}

	// Parse.
	tokens := NewLexer(source).Tokenize()
	prog, errs := NewParser(tokens).Parse()
	if len(errs) > 0 {
		vm.consoleLog = append(vm.consoleLog, "Parse error: "+strings.Join(errs, "; "))
		return Undefined
	}

	// Compile.
	bf := Compile(prog)

	// Pre-allocate constant names to avoid per-execution lookup allocations.
	bf.BuildConstantNames()

	// ICVector is allocated at compile time (Compile sets bf.ICVector).
	// No per-execution allocation — the frame simply references bf.ICVector.

	// Cache for future runs.
	vm.bytecodeCache[source] = bf

	// Execute.
	vm.maybePromoteTier(bf)
	return vm.execute(bf)
}

// Execute runs a compiled BytecodeFunction and returns the result.
func (vm *VM) Execute(bf *BytecodeFunction) JSValue {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.maybePromoteTier(bf)
	return vm.execute(bf)
}

// SparkplugCompiler is a plugin hook set by the jit package during init().
// It bridges jit.SparkplugCompile to the VM, avoiding import cycles since
// pkg/js does not import pkg/jit. When non-nil, it compiles a
// BytecodeFunction into native code and returns the executable address.
var SparkplugCompiler func(bf *BytecodeFunction) (rxAddr uintptr, err error)

// PatchICSlotHook is a plugin hook set by the jit package to avoid import
// cycles. When non-nil, it patches a JIT IC slot with monomorphic feedback.
var PatchICSlotHook func(sparkplug uintptr, slotIdx int, shapePtr unsafe.Pointer, offset int)

// PatchICSlotStoreHook is a plugin hook for patching store IC slots.
var PatchICSlotStoreHook func(sparkplug uintptr, slotIdx int, shapePtr unsafe.Pointer, offset int)

// PatchPolyICSlotHook patches a polymorphic IC slot (2-4 shapes).
var PatchPolyICSlotHook func(sparkplug uintptr, slotIdx int, shapes []unsafe.Pointer, offsets []int)

// PatchMegaICSlotHook patches a megamorphic IC slot (permanent slow path).
var PatchMegaICSlotHook func(sparkplug uintptr, slotIdx int)

// JITProtectHook is called before/after executing JIT code to manage per-thread
// W^X protection on macOS. Set by the jit package.
var JITProtectHook func(enabled bool)


// TurboFanCompiler is the hook for the TurboFan optimizing JIT compiler.
// Set by pkg/jit during init() to avoid import cycles. When non-nil, it
// compiles a BytecodeFunction into optimized native code and returns the
// executable address.
var TurboFanCompiler func(bf *BytecodeFunction) (rxAddr uintptr, err error)

// maybePromoteTier increments the call count and triggers JIT compilation
// when thresholds are reached. Sparkplug and TurboFan compilation run in
// background goroutines to avoid blocking the interpreter.
func (vm *VM) maybePromoteTier(bf *BytecodeFunction) {
	if vm.DisableJIT {
		return
	}
	bf.CallCount++
	switch {
	case bf.CallCount == Tier0SparkplugThreshold && bf.Sparkplug == 0 && SparkplugCompiler != nil:
		go func() {
			rxAddr, err := SparkplugCompiler(bf)
			if err != nil {
			} else if rxAddr != 0 {
				bf.Sparkplug = rxAddr
			} else {
			}
		}()
	case bf.CallCount == Tier1TurboFanThreshold && bf.ICVector != nil && bf.TurboFan == 0 && TurboFanCompiler != nil:
		go func() {
			rxAddr, err := TurboFanCompiler(bf)
			if err == nil && rxAddr != 0 {
				bf.TurboFan = rxAddr
			}
		}()
	}
}

// executeOne dispatches a single instruction via function table.
// Returns true if the frame should return (OpReturn or exception was hit).
func (vm *VM) executeOne(frame *VMFrame) (shouldReturn bool) {
	vm.stepCount++
	if vm.stepCount > maxSteps {
		frame.Thrown = NewString("RangeError: Maximum execution steps exceeded")
		return true
	}
	instr := frame.Func.Instructions[frame.PC]
	frame.PC++

	// OSR check: if TurboFan became available mid-execution, transition
	// at loop back-edges (backward jumps). TurboFan takes priority over Sparkplug.
	if frame.Func.TurboFan != 0 && !frame.InTurboFan {
		if instr.Op == OpJump {
			target := int(instr.OperandA)
			if target <= frame.PC { // backward jump = loop edge
				vm.osrToTurboFan(frame)
				if !frame.InTurboFan {
					return false
				}
				return true // TurboFan handles the rest
			}
		}
	}

	// OSR check: if Sparkplug became available mid-execution, transition
	// at loop back-edges (backward jumps). This allows hot loops detected
	// during interpretation to seamlessly upgrade to native code.
	if !vm.DisableJIT && frame.Func.Sparkplug != 0 && !frame.InSparkplug {
		if instr.Op == OpJump {
			target := int(instr.OperandA)
			if target <= frame.PC { // backward jump = loop edge
				vm.osrToSparkplug(frame)
				// If deopt occurred (InSparkplug cleared), continue in
				// interpreter instead of returning.
				if !frame.InSparkplug {
					return false
				}
				return true // Sparkplug handles the rest
			}
		}
	}

	// Fast-path: inline trivial ops to skip function table dispatch overhead.
	switch instr.Op {
	case OpNop:
		// Nothing to do — skip handler call entirely.
	case OpLdaSmi:
		// Inline: small integer load — one of the most common ops.
		frame.Acc = NewNumber(float64(int8(instr.OperandA)))
	case OpStar:
		// Inline: store accumulator to register — very common.
		reg := int(instr.OperandA)
		if reg < len(frame.Regs) {
			frame.Regs[reg] = frame.Acc
		}
	case OpLdar:
		// Inline: load register to accumulator.
		reg := int(instr.OperandA)
		if reg < len(frame.Regs) {
			frame.Acc = frame.Regs[reg]
		}
	case OpReturn:
		// Inline the common "return undefined" case (OperandB==2 from peephole
		// optimizer merging LdaUndefined+Return). Skip handler for leaf functions
		// with no finally handler and no generator state.
		if instr.OperandB == 2 && frame.GenState == nil && frame.FinallyPC < 0 {
			frame.Acc = Undefined
			return true
		}
		handler := opTable[instr.Op]
		if handler != nil {
			handler(vm, frame, instr)
		}
	case OpAdd:
		// Inline fast path: both operands are numbers (dominant case).
		lhs := frame.Regs[int(instr.OperandA)]
		if lhs.Tag == TagNumber && frame.Acc.Tag == TagNumber {
			result := lhs.NumVal + frame.Acc.NumVal
			if isSmallInt(result) {
				frame.Acc = smallIntValue(int(result))
			} else {
				frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
			}
			recordBinaryFeedback(frame, instr)
		} else {
			opAdd(vm, frame, instr)
		}
	case OpLdaNamedProperty:
		// Inline fast path: monomorphic IC hit avoids function-table dispatch.
		slotIdx := int(instr.OperandC)
		hit := false
		if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
			obj := frame.Acc.ObjVal
			if slotIdx < len(frame.Func.ICVector.Slots) {
				slot := &frame.Func.ICVector.Slots[slotIdx]
				if slot.State == ICMonomorphic && obj.Shape == slot.Shape && slot.Offset < obj.propLen() {
					frame.Acc = obj.propAt(slot.Offset)
					slot.HitCount++
					hit = true
				}
			}
		}
		if !hit {
			opLdaNamedProperty(vm, frame, instr)
		}
	default:
		handler := opTable[instr.Op]
		if handler != nil {
			handler(vm, frame, instr)
		}
	}
	if frame.ShouldReturn {
		frame.ShouldReturn = false
		return true
	}
	if frame.Thrown.Tag != TagUndefined {
		return true
	}
	return false
}

// opTable maps each opcode to its handler function. Uses a fixed-size array
// for O(1) lookup with predictable branch behavior (better than switch).
// Populated in init() to avoid initialization cycles (handler functions may
// call VM methods that eventually reference opTable through executeOne).
var opTable [256]opHandler

func init() {
	opTable[OpNop] = opNop
	opTable[OpLdaConstant] = opLdaConstant
	opTable[OpLdaUndefined] = opLdaUndefined
	opTable[OpLdaNull] = opLdaNull
	opTable[OpLdaTrue] = opLdaTrue
	opTable[OpLdaFalse] = opLdaFalse
	opTable[OpLdaZero] = opLdaZero
	opTable[OpLdaOne] = opLdaOne
	opTable[OpLdaSmi] = opLdaSmi
	opTable[OpStar] = opStar
	opTable[OpLdar] = opLdar
	opTable[OpMov] = opMov
	opTable[OpAdd] = opAdd
	opTable[OpSub] = opSub
	opTable[OpMul] = opMul
	opTable[OpDiv] = opDiv
	opTable[OpMod] = opMod
	opTable[OpExp] = opExp
	opTable[OpNegate] = opNegate
	opTable[OpInc] = opInc
	opTable[OpDec] = opDec
	opTable[OpEq] = opEq
	opTable[OpNotEq] = opNotEq
	opTable[OpStrictEq] = opStrictEq
	opTable[OpStrictNotEq] = opStrictNotEq
	opTable[OpLessThan] = opLessThan
	opTable[OpGreaterThan] = opGreaterThan
	opTable[OpLessEq] = opLessEq
	opTable[OpGreaterEq] = opGreaterEq
	opTable[OpLogicalNot] = opLogicalNot
	opTable[OpLogicalAnd] = opLogicalAnd
	opTable[OpLogicalOr] = opLogicalOr
	opTable[OpBitwiseAnd] = opBitwiseAnd
	opTable[OpBitwiseOr] = opBitwiseOr
	opTable[OpBitwiseXor] = opBitwiseXor
	opTable[OpBitwiseNot] = opBitwiseNot
	opTable[OpShiftLeft] = opShiftLeft
	opTable[OpShiftRight] = opShiftRight
	opTable[OpShiftRightZero] = opShiftRightZero
	opTable[OpToNumber] = opToNumber
	opTable[OpToString] = opToString
	opTable[OpToBoolean] = opToBoolean
	opTable[OpTypeof] = opTypeof
	opTable[OpDelete] = opDelete
	opTable[OpDeleteKeyed] = opDeleteKeyed
	opTable[OpInstanceof] = opInstanceof
	opTable[OpIn] = opIn
	opTable[OpLdaNamedProperty] = opLdaNamedProperty
	opTable[OpStaNamedProperty] = opStaNamedProperty
	opTable[OpLdaKeyedProperty] = opLdaKeyedProperty
	opTable[OpStaKeyedProperty] = opStaKeyedProperty
	opTable[OpJump] = opJump
	opTable[OpJumpIfFalse] = opJumpIfFalse
	opTable[OpJumpIfTrue] = opJumpIfTrue
	opTable[OpJumpIfToBooleanTrue] = opJumpIfToBooleanTrue
	opTable[OpJumpIfToBooleanFalse] = opJumpIfToBooleanFalse
	opTable[OpJumpIfNotNullish] = opJumpIfNotNullish
	opTable[OpCall] = opCall
	opTable[OpCall0] = opCall0
	opTable[OpCall1] = opCall1
	opTable[OpCall2] = opCall2
	opTable[OpCallSpread] = opCallSpread
	opTable[OpReturn] = opReturn
	opTable[OpCreateClosure] = opCreateClosure
	opTable[OpLdaCaptured] = opLdaCaptured
	opTable[OpCreateObject] = opCreateObject
	opTable[OpCreateObjectLiteral] = opCreateObjectLiteral
	opTable[OpCreateArray] = opCreateArray
	opTable[OpCreateRegExp] = opCreateRegExp
	opTable[OpThrow] = opThrow
	opTable[OpSetTryHandler] = opSetTryHandler
	opTable[OpClearTryHandler] = opClearTryHandler
	opTable[OpSetFinallyHandler] = opSetFinallyHandler
	opTable[OpForInSetup] = opForInSetup
	opTable[OpForInNext] = opForInNext
	opTable[OpLdaGlobal] = opLdaGlobal
	opTable[OpStaGlobal] = opStaGlobal
	opTable[OpLdaGlobalSlot] = opLdaGlobalSlot
	opTable[OpStaGlobalSlot] = opStaGlobalSlot
	opTable[OpLdaLocal] = opLdaLocal
	opTable[OpStaLocal] = opStaLocal
	opTable[OpLdaThis] = opLdaThis
	opTable[OpDup] = opDup
	opTable[OpStaByOffset] = opStaByOffset
	opTable[OpThrowConstAssignment] = opThrowConstAssignment
	opTable[OpSetPrototype] = opSetPrototype
	opTable[OpYield] = opYield
	opTable[OpYieldDelegate] = opYieldDelegate
	opTable[OpCreateGenerator] = opCreateGenerator
	opTable[OpSuperCall] = opSuperCall
	opTable[OpCheckConstructor] = opCheckConstructor
}

// --- Bytecode handler functions ---

func opNop(vm *VM, frame *VMFrame, instr Instruction) {}

func opLdaConstant(vm *VM, frame *VMFrame, instr Instruction) {
	idx := int(instr.OperandA)
	if idx < len(frame.Func.Constants) {
		frame.Acc = frame.Func.Constants[idx]
	}
}

func opLdaUndefined(vm *VM, frame *VMFrame, instr Instruction) { frame.Acc = Undefined }
func opLdaNull(vm *VM, frame *VMFrame, instr Instruction)      { frame.Acc = Null }
func opLdaTrue(vm *VM, frame *VMFrame, instr Instruction)      { frame.Acc = True }
func opLdaFalse(vm *VM, frame *VMFrame, instr Instruction)     { frame.Acc = False }
func opLdaZero(vm *VM, frame *VMFrame, instr Instruction)      { frame.Acc = smallIntValue(0) }
func opLdaOne(vm *VM, frame *VMFrame, instr Instruction)       { frame.Acc = smallIntValue(1) }

func opLdaSmi(vm *VM, frame *VMFrame, instr Instruction) {
	val := int8(instr.OperandA)
	frame.Acc = smallIntValue(int(val))
}

func opStar(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Regs[reg] = frame.Acc
	}
}

func opLdar(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Acc = frame.Regs[reg]
	}
}

func opMov(vm *VM, frame *VMFrame, instr Instruction) {
	src, dst := int(instr.OperandA), int(instr.OperandB)
	if src < len(frame.Regs) && dst < len(frame.Regs) {
		frame.Regs[dst] = frame.Regs[src]
	}
}

// --- Arithmetic handlers ---

func opAdd(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = jsAdd(lhs, frame.Acc)
	recordBinaryFeedback(frame, instr)
}

func opSub(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	// Fast path: both numbers — avoid IsBigInt/ToNumber overhead.
	if lhs.Tag == TagNumber && frame.Acc.Tag == TagNumber {
		result := lhs.NumVal - frame.Acc.NumVal
		if isSmallInt(result) {
			frame.Acc = smallIntValue(int(result))
		} else {
			frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
		}
		recordBinaryFeedback(frame, instr)
		return
	}
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Sub(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			recordBinaryFeedback(frame, instr)
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: lhs.ToNumber() - frame.Acc.ToNumber()}
	recordBinaryFeedback(frame, instr)
}

func opMul(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	// Fast path: both numbers — avoid IsBigInt/ToNumber overhead.
	if lhs.Tag == TagNumber && frame.Acc.Tag == TagNumber {
		result := lhs.NumVal * frame.Acc.NumVal
		if isSmallInt(result) {
			frame.Acc = smallIntValue(int(result))
		} else {
			frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
		}
		recordBinaryFeedback(frame, instr)
		return
	}
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Mul(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			recordBinaryFeedback(frame, instr)
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: lhs.ToNumber() * frame.Acc.ToNumber()}
	recordBinaryFeedback(frame, instr)
}

func opDiv(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			if frame.Acc.BigIntVal.Sign() == 0 {
				throwRangeErrorInFrame(frame, "Division by zero")
				return
			}
			result := new(big.Int).Div(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			recordBinaryFeedback(frame, instr)
			return
		}
	}
	l, r := lhs.ToNumber(), frame.Acc.ToNumber()
	var result float64
	if r == 0 {
		if l == 0 {
			result = math.NaN()
		} else {
			result = math.Inf(1)
		}
	} else {
		result = l / r
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
	recordBinaryFeedback(frame, instr)
}

func opMod(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			if frame.Acc.BigIntVal.Sign() == 0 {
				throwRangeErrorInFrame(frame, "Division by zero")
				return
			}
			result := new(big.Int).Mod(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	l, r := lhs.ToNumber(), frame.Acc.ToNumber()
	var result float64
	if r == 0 {
		result = math.NaN()
	} else {
		result = math.Mod(l, r)
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
}

func opExp(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			exp := frame.Acc.BigIntVal
			if exp.Sign() < 0 {
				throwRangeErrorInFrame(frame, "BigInt negative exponent")
				return
			}
			// Exponentiation with positive BigInt exponent.
			base := new(big.Int).Set(lhs.BigIntVal)
			e := new(big.Int).Set(exp)
			one := big.NewInt(1)
			zero := big.NewInt(0)
			if e.Cmp(zero) == 0 {
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: big.NewInt(1)}
				return
			}
			result := big.NewInt(1)
			for e.Cmp(zero) > 0 {
				if new(big.Int).And(e, one).Cmp(one) == 0 {
					result.Mul(result, base)
				}
				base.Mul(base, base)
				e.Rsh(e, 1)
			}
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: math.Pow(lhs.ToNumber(), frame.Acc.ToNumber())}
}

func opNegate(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsBigInt() {
		result := new(big.Int).Neg(frame.Acc.BigIntVal)
		frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
		return
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: -frame.Acc.ToNumber()}
}

func opInc(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		if frame.Regs[reg].IsBigInt() {
			result := new(big.Int).Add(frame.Regs[reg].BigIntVal, big.NewInt(1))
			frame.Regs[reg] = JSValue{Tag: TagBigInt, BigIntVal: result}
			frame.Acc = frame.Regs[reg]
			return
		}
		frame.Regs[reg] = JSValue{Tag: TagNumber, NumVal: frame.Regs[reg].ToNumber() + 1}
		frame.Acc = frame.Regs[reg]
	}
}

func opDec(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		if frame.Regs[reg].IsBigInt() {
			result := new(big.Int).Sub(frame.Regs[reg].BigIntVal, big.NewInt(1))
			frame.Regs[reg] = JSValue{Tag: TagBigInt, BigIntVal: result}
			frame.Acc = frame.Regs[reg]
			return
		}
		frame.Regs[reg] = JSValue{Tag: TagNumber, NumVal: frame.Regs[reg].ToNumber() - 1}
		frame.Acc = frame.Regs[reg]
	}
}

// --- Comparison handlers ---

func opEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(lhs.Equals(frame.Acc))
}

func opNotEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(!lhs.Equals(frame.Acc))
}

func opStrictEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(lhs.StrictEquals(frame.Acc))
}

func opStrictNotEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(!lhs.StrictEquals(frame.Acc))
}

func opLessThan(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) < 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() < frame.Acc.ToNumber())
}

func opGreaterThan(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) > 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() > frame.Acc.ToNumber())
}

func opLessEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) <= 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() <= frame.Acc.ToNumber())
}

func opGreaterEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) >= 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() >= frame.Acc.ToNumber())
}

// --- Logical handlers ---

func opLogicalNot(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewBoolean(!frame.Acc.IsTruthy())
}

func opLogicalAnd(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if !lhs.IsTruthy() {
		frame.Acc = lhs
	}
}

func opLogicalOr(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsTruthy() {
		frame.Acc = lhs
	}
}

// --- Bitwise handlers ---

func opBitwiseAnd(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).And(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) & int32(frame.Acc.ToNumber()))}
}

func opBitwiseOr(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Or(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) | int32(frame.Acc.ToNumber()))}
}

func opBitwiseXor(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Xor(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) ^ int32(frame.Acc.ToNumber()))}
}

func opBitwiseNot(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsBigInt() {
		// For BigInt, bitwise NOT is defined as ~x → -x - 1 (two's complement).
		// math/big Not computes the bitwise complement of the absolute value,
		// so instead we compute -x - 1 which is the correct BigInt semantics.
		result := new(big.Int).Neg(frame.Acc.BigIntVal)
		result.Sub(result, big.NewInt(1))
		frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
		return
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(^int32(frame.Acc.ToNumber()))}
}

func opShiftLeft(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			shift := frame.Acc.BigIntVal
			if shift.IsInt64() {
				n := shift.Int64()
				if n < 0 {
					// Negative shift → signed right shift by |n|
					result := new(big.Int).Rsh(lhs.BigIntVal, uint(-n))
					frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
					return
				}
				result := new(big.Int).Lsh(lhs.BigIntVal, uint(n))
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
				return
			}
			// Very large positive shift → result is 0 (or -1 if negative base and very large)
			// big.Int Lsh with huge shift → OOM risk; cap it heuristically.
			if shift.Sign() > 0 {
				result := new(big.Int).Lsh(lhs.BigIntVal, 0) // effectively 0n for huge shift
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
				return
			}
			// Negative large shift → shift right by magnitude (result is 0 or -1)
			result := new(big.Int).Rsh(lhs.BigIntVal, 0)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) << uint32(frame.Acc.ToNumber()))}
}

func opShiftRight(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			shift := frame.Acc.BigIntVal
			if shift.IsInt64() {
				n := shift.Int64()
				if n < 0 {
					// Negative shift → left shift by |n|
					result := new(big.Int).Lsh(lhs.BigIntVal, uint(-n))
					frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
					return
				}
				result := new(big.Int).Rsh(lhs.BigIntVal, uint(n))
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
				return
			}
			// Very large shift amount.
			if shift.Sign() > 0 {
				// Large positive shift → 0 or -1 for signed right shift.
				if lhs.BigIntVal.Sign() < 0 {
					result := big.NewInt(-1)
					frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
					return
				}
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: big.NewInt(0)}
				return
			}
			result := new(big.Int).Lsh(lhs.BigIntVal, 0)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) >> uint32(frame.Acc.ToNumber()))}
}

func opShiftRightZero(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		throwTypeErrorInFrame(frame, "BigInt does not support unsigned right shift")
		return
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(uint32(lhs.ToNumber()) >> uint32(frame.Acc.ToNumber()))}
}

// --- Type conversion handlers ---

func opToNumber(vm *VM, frame *VMFrame, instr Instruction) {
	n := frame.Acc.ToNumber()
	frame.Acc = NewNumber(n)
}

func opToString(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewString(frame.Acc.ToString())
}

func opToBoolean(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewBoolean(frame.Acc.IsTruthy())
}

func opTypeof(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewString(jsTypeof(frame.Acc))
}

func opDelete(vm *VM, frame *VMFrame, instr Instruction) {
	propIdx := int(instr.OperandA)
	propName := ""
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil && propName != "" {
		if frame.Acc.ObjVal.Delete(propName) {
			frame.Acc = True
		} else {
			frame.Acc = False
		}
	} else {
		frame.Acc = False
	}
}

func opDeleteKeyed(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	keyReg := int(instr.OperandB)
	objVal := frame.Regs[objReg]
	key := frame.Regs[keyReg].ToString()
	if objVal.IsObject() && objVal.ObjVal != nil {
		frame.Acc = NewBoolean(objVal.ObjVal.Delete(key))
	} else {
		frame.Acc = True // deleting from non-object returns true per spec
	}
}

func opInstanceof(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if !lhs.IsObject() || lhs.ObjVal == nil || !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
		frame.Acc = False
	} else {
		proto := lhs.ObjVal.Prototype
		rhsProto := frame.Acc.ObjVal.Get("prototype")
		found := false
		for proto != nil {
			if proto == rhsProto.ObjVal {
				found = true
				break
			}
			proto = proto.Prototype
		}
		// Cross-realm fallback: if pointer comparison failed, check whether
		// the LHS prototype chain contains entries tagged with a different
		// RealmID. Only triggered when realms differ AND the RHS prototype's
		// ConstructorName is a known built-in type.
		if !found && rhsProto.IsObject() && rhsProto.ObjVal != nil {
			crossRealm := false
			for p := lhs.ObjVal.Prototype; p != nil; p = p.Prototype {
				if r := vm.getObjectRealm(p); r != 0 && r != vm.RealmID {
					crossRealm = true
					break
				}
			}
			if crossRealm {
				rhsName := rhsProto.ObjVal.ConstructorName
				if isBuiltinPrototype(rhsName) {
					proto = lhs.ObjVal.Prototype
					for proto != nil {
						if proto.ConstructorName == rhsName {
							found = true
							break
						}
						proto = proto.Prototype
					}
				}
			}
		}
		frame.Acc = NewBoolean(found)
	}
}

// builtinPrototypes lists ConstructorNames of built-in prototypes that
// participate in cross-realm instanceof checks. "Object" is excluded
// because NewJSObject() sets it as the default, so it would match
// everything and break standard prototype chain logic.
var builtinPrototypes = map[string]bool{
	"Array":           true,
	"String":          true,
	"Number":          true,
	"Boolean":         true,
	"Function":        true,
	"Date":            true,
	"RegExp":          true,
	"Error":           true,
	"TypeError":       true,
	"SyntaxError":     true,
	"RangeError":      true,
	"ReferenceError":  true,
	"URIError":        true,
	"EvalError":       true,
	"Map":             true,
	"Set":             true,
	"WeakMap":         true,
	"WeakSet":         true,
	"WeakRef":         true,
	"Promise":         true,
	"ArrayBuffer":     true,
	"DataView":        true,
	"Int8Array":       true,
	"Uint8Array":      true,
	"Uint8ClampedArray": true,
	"Int16Array":      true,
	"Uint16Array":     true,
	"Int32Array":      true,
	"Uint32Array":     true,
	"Float32Array":    true,
	"Float64Array":    true,
	"BigInt64Array":   true,
	"BigUint64Array":  true,
	"Symbol":          true,
	"Proxy":           true,
	"FinalizationRegistry": true,
}

func isBuiltinPrototype(name string) bool {
	return builtinPrototypes[name]
}

func opIn(vm *VM, frame *VMFrame, instr Instruction) {
	// lhs (register) = property name (typically a string from key expression)
	// acc = object to check
	propName := frame.Regs[int(instr.OperandA)].ToString()
	if !frame.Acc.IsObject() || frame.Acc.ObjVal == nil {
		throwTypeErrorInFrame(frame, "Cannot use 'in' operator to search for '"+propName+"' in "+frame.Acc.ToString())
		return
	}
	frame.Acc = NewBoolean(frame.Acc.ObjVal.Has(propName))
}

// --- Property access handlers ---

// patchPolyICIfNeeded patches the JIT IC slot if the state has transitioned to
// polymorphic and Sparkplug code is available. No-op otherwise.
// Guard: PolyCount must be ≥ 2 (monomorphic slots use PatchICSlotHook).
func patchPolyICIfNeeded(frame *VMFrame, slotIdx int, slot *ICSlot) {
	if frame == nil || frame.Func == nil {
		return
	}
	if frame.Func.Sparkplug == 0 || slot.State != ICPolymorphic || slot.Patched || PatchPolyICSlotHook == nil {
		return
	}
	if slot.PolyCount >= 2 {
		PatchPolyICSlotHook(frame.Func.Sparkplug, slotIdx, slot.PolyShapes[:slot.PolyCount], slot.PolyOffsets[:slot.PolyCount])
		slot.Patched = true
	}
}

func opLdaNamedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	propIdx := int(instr.OperandA)
	slotIdx := int(instr.OperandC)

	// Fast path: try IC without resolving the property name string.
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		obj := frame.Acc.ObjVal
		slots := frame.Func.ICVector.Slots
		if slotIdx < len(slots) {
			slot := &slots[slotIdx]
			// Monomorphic fast path: shape match → direct offset access (0 allocs).
			if slot.State == ICMonomorphic && obj.Shape == slot.Shape {
				if slot.Offset < obj.propLen() {
					frame.Acc = obj.propAt(slot.Offset)
					slot.HitCount++
					// Patch JIT IC slot once when Sparkplug code becomes available.
					if !vm.DisableJIT && frame.Func.Sparkplug != 0 && PatchICSlotHook != nil && !slot.Patched {
						PatchICSlotHook(frame.Func.Sparkplug, slotIdx, unsafe.Pointer(obj.Shape), slot.Offset)
						slot.Patched = true
					}
					return
				}
			}
			// Megamorphic fast path: cached shape re-check.
			if slot.State == ICMegamorphic && obj.Shape == slot.MegaShape && slot.MegaOffset < obj.propLen() {
				frame.Acc = obj.propAt(slot.MegaOffset)
				slot.HitCount++
				return
			}
			// Poly/mega patching: if state just became polymorphic or megamorphic,
			// patch the JIT IC slot accordingly.
			patchPolyICIfNeeded(frame, slotIdx, slot)
			if !vm.DisableJIT && frame.Func.Sparkplug != 0 && slot.State == ICMegamorphic && !slot.Patched {
				if PatchMegaICSlotHook != nil {
					PatchMegaICSlotHook(frame.Func.Sparkplug, slotIdx)
					slot.Patched = true
				}
			}
		}
	}

	// Slow path: resolve name from constant pool, then use IC or direct Get.
	// Use pre-computed ConstantNames (compiled once, 0 allocs) when available.
	// Fallback to ToString() only if ConstantNames hasn't been built.
	propName := ""
	if propIdx < len(frame.Func.ConstantNames) {
		propName = frame.Func.ConstantNames[propIdx]
	} else if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		frame.Acc = frame.Func.ICVector.LoadIC(slotIdx, frame.Acc.ObjVal, propName)
		// After LoadIC, the slot state may have transitioned to polymorphic.
		// Patch the JIT IC slot with sequential shape guards.
		slots := frame.Func.ICVector.Slots
		if slotIdx < len(slots) {
			patchPolyICIfNeeded(frame, slotIdx, &slots[slotIdx])
		}
	} else if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		frame.Acc = frame.Acc.ObjVal.Get(propName)
	} else if frame.Acc.IsString() {
		// Autobox string: wrap in a String object so prototype methods are reachable.
		boxed := NewJSObject()
		boxed.ConstructorName = "String"
		boxed.Prototype = StringPrototype
		boxed.Set("__value__", frame.Acc)
		frame.Acc = boxed.Get(propName)
	} else {
		frame.Acc = Undefined
	}
}

func opStaNamedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	propIdx := int(instr.OperandA)
	val := Undefined
	if int(instr.OperandB) < len(frame.Regs) {
		val = frame.Regs[int(instr.OperandB)]
	}
	slotIdx := int(instr.OperandC)

	// Fast path: try IC without resolving the property name string.
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		obj := frame.Acc.ObjVal
		slots := frame.Func.ICVector.Slots
		if slotIdx < len(slots) {
			slot := &slots[slotIdx]
			// Monomorphic fast path: shape match → direct offset write (0 allocs).
			if slot.State == ICMonomorphic && obj.Shape == slot.Shape {
				if !obj.Frozen && slot.Offset < obj.propLen() {
					obj.propSet(slot.Offset, val)
					return
				}
			}
		}
	}

	// Slow path: resolve name from constant pool.
	propName := ""
	if propIdx < len(frame.Func.Constants) {
		propName = frame.Func.Constants[propIdx].ToString()
	}
	if slotIdx < 255 && frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Func.ICVector != nil {
		frame.Func.ICVector.StoreIC(slotIdx, frame.Acc.ObjVal, propName, val)
	} else if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		frame.Acc.ObjVal.Set(propName, val)
	}
}

// opStaByOffset stores a value directly into an object's property slot by offset.
// OperandA = object register, OperandB = property offset, OperandC = value register.
// Used by object literal fast path to skip shape lookups when offsets are known.
func opStaByOffset(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	offset := int(instr.OperandB)
	valReg := int(instr.OperandC)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		val := Undefined
		if valReg < len(frame.Regs) {
			val = frame.Regs[valReg]
		}
		obj.lastLookupValid = false
		if !obj.Frozen && !obj.Sealed {
			obj.propSet(offset, val)
		}
	}
}

// isDenseArray returns true if obj is a dense Array (has ArrayPrototype and a valid Shape).
// Dense arrays have consecutive numeric indices that can be accessed directly by offset.
func isDenseArray(obj *JSObject) bool {
	return obj.Prototype == ArrayPrototype && obj.Shape != nil
}

func opLdaKeyedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() {
		obj := frame.Regs[objReg].ObjVal
		key := frame.Acc.ToString()

		// Dense array fast path: if the key is a numeric index, use direct offset access.
		if isDenseArray(obj) && !obj.Shape.IsDictionary {
			if idx, ok := parseArrayIndex(key); ok {
				length := int(obj.Get("length").ToNumber())
				if idx < length {
					// Compute offset for this index: find base offset of "0" once.
					// For dense arrays, properties "0", "1", ... are stored sequentially.
					if offset := obj.Shape.GetOffset(key); offset >= 0 && offset < obj.propLen() {
						frame.Acc = obj.propAt(offset)
						return
					}
				}
			}
		}

		frame.Acc = obj.Get(key)
	} else {
		frame.Acc = Undefined
	}
}

func opStaKeyedProperty(vm *VM, frame *VMFrame, instr Instruction) {
	objReg, valReg := int(instr.OperandA), int(instr.OperandB)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && valReg < len(frame.Regs) {
		obj := frame.Regs[objReg].ObjVal
		key := frame.Acc.ToString()

		// Dense array fast path: use direct offset access for numeric indices.
		if isDenseArray(obj) && !obj.Shape.IsDictionary {
			if idx, ok := parseArrayIndex(key); ok {
				length := int(obj.Get("length").ToNumber())
				if idx <= length { // allow writing one past for array growth
					if offset := obj.Shape.GetOffset(key); offset >= 0 && offset < obj.propLen() && !obj.Frozen {
						obj.propSet(offset, frame.Regs[valReg])
						// Update length if writing at or beyond current.
						if idx >= length {
							obj.Set("length", NewNumber(float64(idx + 1)))
						}
						return
					}
				}
			}
		}

		obj.Set(key, frame.Regs[valReg])
	}
}

// parseArrayIndex parses a string as a non-negative integer array index.
// Returns (index, true) for valid array indices like "0", "42".
// Returns (0, false) for non-integer keys like "length", "foo", "-1", "1.5".
func parseArrayIndex(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	// Must start with a digit and not be "0"-prefixed (except "0" itself).
	if s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		// Cap at a reasonable max to avoid overflow.
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, true
}

// --- Control flow handlers ---

func opJump(vm *VM, frame *VMFrame, instr Instruction) {
	frame.PC = int(instr.OperandA)
}

func opJumpIfFalse(vm *VM, frame *VMFrame, instr Instruction) {
	if !frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfTrue(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfToBooleanTrue(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfToBooleanFalse(vm *VM, frame *VMFrame, instr Instruction) {
	if !frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfNotNullish(vm *VM, frame *VMFrame, instr Instruction) {
	if !frame.Acc.IsNull() && !frame.Acc.IsUndefined() {
		frame.PC = int(instr.OperandA)
	}
}

// --- Function call handlers ---

// boxString wraps a string primitive in a boxed String object so that
// String.prototype methods can be called with the correct `this` binding.
func boxString(s JSValue) *JSObject {
	boxed := NewJSObject()
	boxed.ConstructorName = "String"
	boxed.Prototype = StringPrototype
	boxed.Set("__value__", s)
	return boxed
}

// opCallFast is the shared fast-path for OpCall0/OpCall1/OpCall2.
func (vm *VM) opCallFast(frame *VMFrame, calleeReg, thisReg, argCount int) {
	var args []JSValue
	if argCount > 0 {
		if argCount <= 8 {
			// Stack-allocated array for common arg counts (0-8).
			var arr [8]JSValue
			args = arr[:argCount]
		} else {
			args = make([]JSValue, argCount)
		}
		for i := 0; i < argCount; i++ {
			argReg := calleeReg + 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	var thisObj *JSObject
	if thisReg < 255 && thisReg < len(frame.Regs) {
		if frame.Regs[thisReg].IsObject() {
			thisObj = frame.Regs[thisReg].ObjVal
		} else if frame.Regs[thisReg].IsString() {
			thisObj = boxString(frame.Regs[thisReg])
		}
	}
	if calleeReg < len(frame.Regs) {
		callee := frame.Regs[calleeReg]
		vm.pushCallName(callee, frame)
		frame.Acc = vm.callMethod(callee, thisObj, args)
		// Built-in functions (CallFunc) return directly; bytecode functions
		// go through executeFrame→opReturn which handles the pop.
		if !calleeGoesToBytecode(callee) {
			vm.popCallName()
		}
	} else {
		frame.Acc = Undefined
	}
}

func opCall0(vm *VM, frame *VMFrame, instr Instruction) {
	vm.opCallFast(frame, int(instr.OperandA), int(instr.OperandB), 0)
}

func opCall1(vm *VM, frame *VMFrame, instr Instruction) {
	vm.opCallFast(frame, int(instr.OperandA), int(instr.OperandB), 1)
}

func opCall2(vm *VM, frame *VMFrame, instr Instruction) {
	vm.opCallFast(frame, int(instr.OperandA), int(instr.OperandB), 2)
}

func opCall(vm *VM, frame *VMFrame, instr Instruction) {
	calleeReg, argCount := int(instr.OperandA), int(instr.OperandB)
	thisReg := int(instr.OperandC)
	var args []JSValue
	if argCount > 0 {
		if argCount <= 8 {
			// Stack-allocated array for common arg counts (0-8).
			var arr [8]JSValue
			args = arr[:argCount]
		} else {
			args = make([]JSValue, argCount)
		}
		for i := 0; i < argCount; i++ {
			argReg := calleeReg + 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	var thisObj *JSObject
	if thisReg < 255 && thisReg < len(frame.Regs) {
		if frame.Regs[thisReg].IsObject() {
			thisObj = frame.Regs[thisReg].ObjVal
		} else if frame.Regs[thisReg].IsString() {
			thisObj = boxString(frame.Regs[thisReg])
		}
	}
	if calleeReg < len(frame.Regs) {
		callee := frame.Regs[calleeReg]
		vm.pushCallName(callee, frame)
		frame.Acc = vm.callMethod(callee, thisObj, args)
		// Built-in functions (CallFunc) return directly; bytecode functions
		// go through executeFrame→opReturn which handles the pop.
		if !calleeGoesToBytecode(callee) {
			vm.popCallName()
		}
	} else {
		frame.Acc = Undefined
	}
}

func opCallSpread(vm *VM, frame *VMFrame, instr Instruction) {
	calleeReg, fixedCount := int(instr.OperandA), int(instr.OperandB)
	thisReg := int(instr.OperandC)
	var args []JSValue
	// Read fixed (non-spread) arguments from consecutive registers.
	if fixedCount > 0 {
		if fixedCount <= 8 {
			var arr [8]JSValue
			args = arr[:fixedCount]
		} else {
			args = make([]JSValue, fixedCount)
		}
		for i := 0; i < fixedCount; i++ {
			argReg := calleeReg + 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	// Read spread array from calleeReg + 1 + fixedCount.
	spreadReg := calleeReg + 1 + fixedCount
	if spreadReg < len(frame.Regs) {
		spreadVal := frame.Regs[spreadReg]
		if spreadVal.IsObject() && spreadVal.ObjVal != nil {
			lengthVal := spreadVal.ObjVal.Get("length")
			arrLen := int(lengthVal.ToNumber())
			for i := 0; i < arrLen; i++ {
				elem := spreadVal.ObjVal.Get(intKey(i))
				args = append(args, elem)
			}
		}
	}
	var thisObj *JSObject
	if thisReg < 255 && thisReg < len(frame.Regs) {
		if frame.Regs[thisReg].IsObject() {
			thisObj = frame.Regs[thisReg].ObjVal
		} else if frame.Regs[thisReg].IsString() {
			thisObj = boxString(frame.Regs[thisReg])
		}
	}
	if calleeReg < len(frame.Regs) {
		callee := frame.Regs[calleeReg]
		vm.pushCallName(callee, frame)
		frame.Acc = vm.callMethod(callee, thisObj, args)
		// Built-in functions (CallFunc) return directly; bytecode functions
		// go through executeFrame→opReturn which handles the pop.
		if !calleeGoesToBytecode(callee) {
			vm.popCallName()
		}
	} else {
		frame.Acc = Undefined
	}
}

func opSuperCall(vm *VM, frame *VMFrame, instr Instruction) {
	// super() call: load parent constructor from __proto__ on this.
	// The parent constructor is at this.__proto__.constructor.
	argCount := int(instr.OperandB)
	var args []JSValue
	if argCount > 0 {
		if argCount <= 8 {
			var arr [8]JSValue
			args = arr[:argCount]
		} else {
			args = make([]JSValue, argCount)
		}
		for i := 0; i < argCount; i++ {
			// Args start at register 1 (after the callee register which is 0).
			argReg := 1 + i
			if argReg < len(frame.Regs) {
				args[i] = frame.Regs[argReg]
			}
		}
	}
	// Load parent constructor: this.__proto__.constructor
	thisObj := frame.This.ObjVal
	if thisObj != nil {
		protoVal := thisObj.Get("__proto__")
		if protoVal.IsObject() && protoVal.ObjVal != nil {
			parentCtor := protoVal.ObjVal.Get("constructor")
			frame.Acc = vm.callMethod(parentCtor, thisObj, args)
		} else {
			frame.Acc = Undefined
		}
	} else {
		frame.Acc = Undefined
	}
	// Mark that super() has been called so this can be accessed.
	frame.SuperCalled = true
}

func opCheckConstructor(vm *VM, frame *VMFrame, instr Instruction) {
	// Check that acc is null or a constructor (object with CallFunc or Bytecode).
	if frame.Acc.IsNull() || frame.Acc.IsUndefined() {
		return // null/undefined are allowed as extends value
	}
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
		obj := frame.Acc.ObjVal
		if obj.CallFunc != nil || obj.Bytecode != nil || obj.ConstructorName == "Function" {
			return // is a constructor
		}
	}
	throwTypeErrorInFrame(frame, "Class extends value is not a constructor or null")
}

func opReturn(vm *VM, frame *VMFrame, instr Instruction) {
	// Pop this function's name from the call stack (pushed in opCall/opCallFast).
	vm.popCallName()

	// OperandB==1 is set by the peephole optimizer to indicate OperandA
	// holds a constant pool index that should be loaded before returning.
	if instr.OperandB == 1 {
		idx := int(instr.OperandA)
		if idx < len(frame.Func.Constants) {
			frame.Acc = frame.Func.Constants[idx]
		}
	}
	// OperandB==2: return undefined (merged LdaUndefined+Return).
	if instr.OperandB == 2 {
		frame.Acc = Undefined
	}
	// Generator return: mark done.
	if frame.GenState != nil {
		frame.GenState.Done = true
		// Build result object {value: acc, done: true}.
		resultObj := NewJSObject()
		resultObj.Set("value", frame.Acc)
		resultObj.Set("done", True)
		frame.Acc = NewObject(resultObj)
	}
	// Finally interception: jump to finally instead of returning.
	if frame.FinallyPC >= 0 {
		frame.SavedAcc = frame.Acc
		frame.PC = frame.FinallyPC
		frame.FinallyPC = -1
		return // don't set ShouldReturn — let execution continue at finally
	}
	frame.ShouldReturn = true
}

func opCreateClosure(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsObject() && frame.Acc.ObjVal != nil && frame.Acc.ObjVal.Bytecode != nil {
		closure := NewJSObject()
		closure.ConstructorName = frame.Acc.ObjVal.ConstructorName
		closure.Bytecode = frame.Acc.ObjVal.Bytecode
		closure.Set("length", NewNumber(float64(frame.Acc.ObjVal.Bytecode.NumParams)))
		env := NewJSObject()
		for i, val := range frame.Regs {
			env.Set(intKey(i), val)
		}
		closure.Set("__env__", NewObject(env))
		frame.Acc = NewObject(closure)
	} else {
		frame.Acc = NewObject(NewJSObject())
	}
}

func opLdaCaptured(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.ClosureEnv != nil {
		regIdx := int(instr.OperandA)
		frame.Acc = frame.ClosureEnv.Get(intKey(regIdx))
	} else {
		frame.Acc = Undefined
	}
}

// --- Object / Array handlers ---

func opCreateObject(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewObject(NewJSObject())
}

func opCreateObjectLiteral(vm *VM, frame *VMFrame, instr Instruction) {
	// OperandA = constant pool index; Shapes[A] holds the pre-built Shape pointer.
	shapeKeyIdx := int(instr.OperandA)
	var shape *Shape
	if shapeKeyIdx < len(frame.Func.Shapes) {
		shape = frame.Func.Shapes[shapeKeyIdx]
	}
	if shape == nil {
		// Fallback: use prop names from ShapePropNames.
		var propNames []string
		if shapeKeyIdx < len(frame.Func.ShapePropNames) {
			propNames = frame.Func.ShapePropNames[shapeKeyIdx]
		}
		shape = GetOrCreateShape(propNames)
	}
	obj := vm.allocObj()
	obj.Shape = shape
	obj.Prototype = ObjectPrototype
	obj.ConstructorName = "Object"
	if shape.PropertyCount > 0 {
		obj.growProperties(shape.PropertyCount)
	}
	frame.Acc = JSValue{Tag: TagObject, ObjVal: obj}
}

func opCreateArray(vm *VM, frame *VMFrame, instr Instruction) {
	arr := NewJSObject()
	arr.ConstructorName = "Array"
	if ArrayPrototype != nil {
		arr.Prototype = ArrayPrototype
	}
	arr.Set("length", NewNumber(0))
	frame.Acc = NewObject(arr)
}

func opCreateRegExp(vm *VM, frame *VMFrame, instr Instruction) {
	patIdx, flagsIdx := int(instr.OperandA), int(instr.OperandB)
	var pattern, flags string
	if patIdx < len(frame.Func.Constants) {
		pattern = frame.Func.Constants[patIdx].ToString()
	}
	if flagsIdx < len(frame.Func.Constants) {
		flags = frame.Func.Constants[flagsIdx].ToString()
	}
	frame.Acc = vm.createRegExp(pattern, flags)
}

func opSetPrototype(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		if frame.Acc.IsObject() && frame.Acc.ObjVal != nil {
			frame.Regs[objReg].ObjVal.Prototype = frame.Acc.ObjVal
		}
	}
}

// --- Generator handlers ---

func opYield(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.GenState == nil || frame.GenState.GenObj == nil {
		// Not in a generator context — treat as return.
		frame.ShouldReturn = true
		return
	}
	// Save frame state into generator state.
	gs := frame.GenState
	gs.SavedPC = frame.PC // PC already advanced past OpYield; next resume starts here
	gs.SavedAcc = frame.Acc
	// Deep-copy registers.
	gs.SavedRegs = make([]JSValue, len(frame.Regs))
	copy(gs.SavedRegs, frame.Regs)
	gs.SavedThis = frame.This
	gs.Done = false

	// Build result object {value: acc, done: false}.
	resultObj := NewJSObject()
	resultObj.Set("value", frame.Acc)
	resultObj.Set("done", False)
	frame.Acc = NewObject(resultObj)
	frame.ShouldReturn = true
}

func opYieldDelegate(vm *VM, frame *VMFrame, instr Instruction) {
	// yield* expr: delegate to another iterable.
	// For now, simplified: extract the value and yield it directly.
	// Full implementation would iterate the delegated object.
	if frame.GenState == nil || frame.GenState.GenObj == nil {
		frame.ShouldReturn = true
		return
	}
	gs := frame.GenState
	gs.SavedPC = frame.PC
	gs.SavedAcc = frame.Acc
	gs.SavedRegs = make([]JSValue, len(frame.Regs))
	copy(gs.SavedRegs, frame.Regs)
	gs.SavedThis = frame.This
	gs.Done = false

	resultObj := NewJSObject()
	resultObj.Set("value", frame.Acc)
	resultObj.Set("done", False)
	frame.Acc = NewObject(resultObj)
	frame.ShouldReturn = true
}

// opCreateGenerator creates a generator object from a function template in acc.
// Used internally; the generator function body emits this as the first instruction.
func opCreateGenerator(vm *VM, frame *VMFrame, instr Instruction) {
	// The acc holds the function template. Create a new generator object.
	bf := frame.Func
	if bf == nil {
		frame.Acc = Undefined
		return
	}
	genObj := NewJSObject()
	genObj.ConstructorName = "Generator"

	// Store the bytecode for resumption.
	genObj.Set("__bytecode__", NewObject(frame.Func.BytecodeFuncToObj(bf)))

	// Generator state: initially no saved frame.
	gs := &GeneratorState{
		GenObj: genObj,
	}
	genObj.Set("__genstate__", NewObject(genStateToObj(gs)))

	// Attach .next(), .return(), .throw() methods.
	genObj.Set("next", vm.makeGeneratorNext(genObj, gs))
	genObj.Set("return", vm.makeGeneratorReturn(genObj, gs))
	genObj.Set("throw", vm.makeGeneratorThrow(genObj, gs))

	frame.Acc = NewObject(genObj)
	frame.ShouldReturn = true
}

// BytecodeFuncToObj wraps a BytecodeFunction in a JSObject for storage.
func (bf *BytecodeFunction) BytecodeFuncToObj(original *BytecodeFunction) *JSObject {
	// Store the bytecode reference directly.
	obj := NewJSObject()
	obj.Bytecode = original
	return obj
}

// genStateToObj wraps GeneratorState in a JSObject for storage.
func genStateToObj(gs *GeneratorState) *JSObject {
	obj := NewJSObject()
	obj.generatorState = gs
	return obj
}

// makeGeneratorNext creates the .next(value) method for a generator object.
// When called, resumes execution from the saved state or starts the generator.
func (vm *VM) makeGeneratorNext(genObj *JSObject, gs *GeneratorState) JSValue {
	fn := func(this *JSObject, args []JSValue) JSValue {
		if gs.Done {
			resultObj := NewJSObject()
			resultObj.Set("value", Undefined)
			resultObj.Set("done", True)
			return NewObject(resultObj)
		}

		// If we have a saved frame state, resume from it.
		if gs.SavedRegs != nil && gs.GenObj != nil {
			// Resume: restore state and continue execution.
			bfVal := genObj.Get("__bytecode__")
			if !bfVal.IsObject() || bfVal.ObjVal == nil || bfVal.ObjVal.Bytecode == nil {
				gs.Done = true
				resultObj := NewJSObject()
				resultObj.Set("value", Undefined)
				resultObj.Set("done", True)
				return NewObject(resultObj)
			}
			bytecodeFn := bfVal.ObjVal.Bytecode
			regs := make([]JSValue, len(gs.SavedRegs))
			copy(regs, gs.SavedRegs)
			// If .next() was called with an argument, pass it as the yield result.
			resumedAcc := gs.SavedAcc
			if len(args) > 0 {
				resumedAcc = args[0]
			}
			frame := &VMFrame{
				Func:      bytecodeFn,
				Regs:      regs,
				PC:        gs.SavedPC,
				Acc:       resumedAcc,
				HandlerPC: -1,
				FinallyPC: -1,
				This:      gs.SavedThis,
				GenState:  gs,
			}
			result := vm.executeFrame(frame)
			// After execution, check generator state.
			if gs.Done {
				// Generator completed (OpReturn was hit).
				if result.IsObject() && result.ObjVal != nil {
					if _, ok := result.ObjVal.getOwn("done"); ok {
						return result
					}
				}
				resultObj := NewJSObject()
				resultObj.Set("value", result)
				resultObj.Set("done", True)
				return NewObject(resultObj)
			}
			// Generator yielded: result should be the {value, done:false} object from OpYield.
			return result
		}

		// First call: start the generator.
		bfVal := genObj.Get("__bytecode__")
		if !bfVal.IsObject() || bfVal.ObjVal == nil || bfVal.ObjVal.Bytecode == nil {
			gs.Done = true
			resultObj := NewJSObject()
			resultObj.Set("value", Undefined)
			resultObj.Set("done", True)
			return NewObject(resultObj)
		}
		bytecodeFn := bfVal.ObjVal.Bytecode
		regs := make([]JSValue, bytecodeFn.NumRegisters)
		// Load initial arguments into registers.
		argsVal := genObj.Get("__args__")
		if argsVal.IsObject() && argsVal.ObjVal != nil {
			for i := 0; i < bytecodeFn.NumParams && i < len(regs); i++ {
				argKey := intKey(i)
				regs[i] = argsVal.ObjVal.Get(argKey)
			}
		}
		frame := &VMFrame{
			Func:      bytecodeFn,
			Regs:      regs,
			HandlerPC: -1,
			FinallyPC: -1,
			This:      genObj.Get("__this__"),
			GenState:  gs,
		}
		// Set this binding for the generator.
		if frame.This.IsUndefined() || frame.This.IsNull() {
			frame.This = vm.globalObject()
		}
		result := vm.executeFrame(frame)
		if gs.Done {
			if result.IsObject() && result.ObjVal != nil {
				if _, ok := result.ObjVal.getOwn("done"); ok {
					return result
				}
			}
			resultObj := NewJSObject()
			resultObj.Set("value", result)
			resultObj.Set("done", True)
			return NewObject(resultObj)
		}
		return result
	}
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return NewObject(obj)
}

// makeGeneratorReturn creates the .return(value) method for a generator object.
func (vm *VM) makeGeneratorReturn(genObj *JSObject, gs *GeneratorState) JSValue {
	fn := func(this *JSObject, args []JSValue) JSValue {
		gs.Done = true
		var value JSValue = Undefined
		if len(args) > 0 {
			value = args[0]
		}
		resultObj := NewJSObject()
		resultObj.Set("value", value)
		resultObj.Set("done", True)
		return NewObject(resultObj)
	}
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return NewObject(obj)
}

// makeGeneratorThrow creates the .throw(error) method for a generator object.
func (vm *VM) makeGeneratorThrow(genObj *JSObject, gs *GeneratorState) JSValue {
	fn := func(this *JSObject, args []JSValue) JSValue {
		gs.Done = true
		var err JSValue = NewString("Generator.throw() called")
		if len(args) > 0 {
			err = args[0]
		}
		// In a full implementation, this would resume the generator and throw.
		// For now, mark done and return a rejected-like result.
		resultObj := NewJSObject()
		resultObj.Set("value", Undefined)
		resultObj.Set("done", True)
		resultObj.Set("error", err)
		return NewObject(resultObj)
	}
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return NewObject(obj)
}

// --- Exception handling ---

func opThrow(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Thrown = frame.Acc
	if frame.HandlerPC >= 0 {
		frame.PC = frame.HandlerPC
		// Don't reset HandlerPC — let execute/executeFrame loop consume it.
		// Don't set ShouldReturn — let executeOne detect Thrown != Undefined.
		return
	}
	// Unhandled exception: check finally handler.
	if frame.FinallyPC >= 0 {
		frame.PC = frame.FinallyPC
		// Don't reset FinallyPC yet — executeFrame will consume it.
		return
	}
	frame.ShouldReturn = true
}

func opSetTryHandler(vm *VM, frame *VMFrame, instr Instruction) {
	frame.HandlerPC = int(instr.OperandA)
}

func opClearTryHandler(vm *VM, frame *VMFrame, instr Instruction) {
	frame.HandlerPC = -1
	frame.Thrown = Undefined
}

func opSetFinallyHandler(vm *VM, frame *VMFrame, instr Instruction) {
	// OperandA=255 signals "clear/disable" the finally handler.
	if instr.OperandA == 255 {
		frame.FinallyPC = -1
		return
	}
	frame.FinallyPC = int(instr.OperandA)
}

// --- Iteration handlers ---

func opForInSetup(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		frame.forInObj = obj
		frame.forInKeys = nil
		if !obj.Shape.IsDictionary {
			for name, entry := range obj.Shape.Properties {
				if entry.Attr&AttrEnumerable != 0 {
					frame.forInKeys = append(frame.forInKeys, name)
				}
			}
		}
		if obj.Shape.IsDictionary && obj.Dictionary != nil {
			for name := range obj.Dictionary {
				frame.forInKeys = append(frame.forInKeys, name)
			}
		}
		frame.forInIndex = 0
		if len(frame.forInKeys) > 0 {
			frame.Acc = NewString(frame.forInKeys[0])
			frame.forInIndex = 1
		} else {
			frame.Acc = Undefined
		}
	} else {
		frame.Acc = Undefined
	}
}

func opForInNext(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.forInObj != nil && frame.forInIndex < len(frame.forInKeys) {
		frame.Acc = NewString(frame.forInKeys[frame.forInIndex])
		frame.forInIndex++
	} else {
		frame.Acc = Undefined
		frame.forInObj = nil
		frame.forInKeys = nil
	}
}

// --- Variable handlers ---

func opLdaGlobal(vm *VM, frame *VMFrame, instr Instruction) {
	nameIdx := int(instr.OperandA)
	if nameIdx < len(frame.Func.ConstantNames) {
		name := frame.Func.ConstantNames[nameIdx]
		if val, ok := vm.globals[name]; ok {
			frame.Acc = val
			// Cache the value in Constants pool for JIT fast-path.
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = val
			}
		} else if builtinFn, ok := vm.builtins[name]; ok {
			frame.Acc = vm.makeBuiltinFunction(name, builtinFn)
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = frame.Acc
			}
		} else if funcBF, ok := vm.funcRegistry[name]; ok {
			frame.Acc = vm.makeFunctionObject(name, funcBF)
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = frame.Acc
			}
		} else {
			frame.Acc = Undefined
			if nameIdx < len(frame.Func.Constants) {
				frame.Func.Constants[nameIdx] = Undefined
			}
		}
	} else if nameIdx < len(frame.Func.Constants) {
		name := frame.Func.Constants[nameIdx].ToString()
		if val, ok := vm.globals[name]; ok {
			frame.Acc = val
			frame.Func.Constants[nameIdx] = val
		}
	}
}

func opStaGlobal(vm *VM, frame *VMFrame, instr Instruction) {
	nameIdx := int(instr.OperandA)
	if nameIdx < len(frame.Func.ConstantNames) {
		vm.globals[frame.Func.ConstantNames[nameIdx]] = frame.Acc
		// Cache the value in Constants pool for JIT fast-path.
		if nameIdx < len(frame.Func.Constants) {
			frame.Func.Constants[nameIdx] = frame.Acc
		}
	} else if nameIdx < len(frame.Func.Constants) {
		vm.globals[frame.Func.Constants[nameIdx].ToString()] = frame.Acc
		// Cache the value in Constants pool for JIT fast-path.
		frame.Func.Constants[nameIdx] = frame.Acc
	}
}

func opLdaGlobalSlot(vm *VM, frame *VMFrame, instr Instruction) {
	slot := int(instr.OperandA)
	// Fast path: direct array lookup, but verify the slot name matches.
	// Different compilation units may assign different slot indices for the
	// same name, so we must check the slot name to avoid reading stale values.
	if slot < len(vm.globalSlots) && vm.globalSlotsSet[slot] &&
		slot < len(vm.globalSlotNames) && slot < len(frame.Func.GlobalSlots) &&
		vm.globalSlotNames[slot] == frame.Func.GlobalSlots[slot] {
		frame.Acc = vm.globalSlots[slot]
		// Update GlobalVals cache for JIT fast-path reads.
		if slot < len(frame.Func.GlobalVals) {
			frame.Func.GlobalVals[slot] = frame.Acc
		}
		return
	}
	// Fallback: name-based lookup for builtins, funcRegistry, and globals map.
	if slot < len(frame.Func.GlobalSlots) {
		name := frame.Func.GlobalSlots[slot]
		if val, ok := vm.globals[name]; ok {
			// Cache in slot for subsequent fast-path reads, and record
			// the name so SetGlobal can update this slot too.
			if slot < len(vm.globalSlots) {
				vm.globalSlots[slot] = val
				vm.globalSlotsSet[slot] = true
				if slot < len(vm.globalSlotNames) {
					vm.globalSlotNames[slot] = name
				}
			}
			// Update GlobalVals cache for JIT fast-path reads.
			if slot < len(frame.Func.GlobalVals) {
				frame.Func.GlobalVals[slot] = val
			}
			frame.Acc = val
			return
		}
		if builtinFn, ok := vm.builtins[name]; ok {
			frame.Acc = vm.makeBuiltinFunction(name, builtinFn)
			// Cache in GlobalVals for JIT fast-path reads.
			if slot < len(frame.Func.GlobalVals) {
				frame.Func.GlobalVals[slot] = frame.Acc
			}
			return
		}
		if funcBF, ok := vm.funcRegistry[name]; ok {
			frame.Acc = vm.makeFunctionObject(name, funcBF)
			// Cache in GlobalVals for JIT fast-path reads.
			if slot < len(frame.Func.GlobalVals) {
				frame.Func.GlobalVals[slot] = frame.Acc
			}
			return
		}
	}
	frame.Acc = Undefined
}

func opStaGlobalSlot(vm *VM, frame *VMFrame, instr Instruction) {
	slot := int(instr.OperandA)
	if slot >= len(vm.globalSlots) {
		vm.ensureGlobalSlots(slot+16, frame.Func)
	}
	vm.globalSlots[slot] = frame.Acc
	vm.globalSlotsSet[slot] = true
	// Record the name at this slot for SetGlobal write-through.
	if slot < len(frame.Func.GlobalSlots) {
		name := frame.Func.GlobalSlots[slot]
		vm.globalSlotNames[slot] = name
		vm.globals[name] = frame.Acc
	}
	// Update the function's GlobalVals cache for JIT fast-path reads.
	if slot < len(frame.Func.GlobalVals) {
		frame.Func.GlobalVals[slot] = frame.Acc
	}
}

func opLdaLocal(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Acc = frame.Regs[reg]
	}
}

func opLdaThis(vm *VM, frame *VMFrame, instr Instruction) {
	// In derived class constructors, this is uninitialized until super() is called.
	if frame.Func != nil && frame.Func.IsDerivedConstructor && !frame.SuperCalled {
		throwReferenceErrorInFrame(frame, "Must call super constructor before accessing 'this' in derived class")
		return
	}
	frame.Acc = frame.This
}

func opStaLocal(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Regs[reg] = frame.Acc
	}
}

func opDup(vm *VM, frame *VMFrame, instr Instruction) {
	// Duplicate accumulator into destination register without modifying acc.
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		frame.Regs[reg] = frame.Acc
	}
}

func opThrowConstAssignment(vm *VM, frame *VMFrame, instr Instruction) {
	nameIdx := int(instr.OperandA)
	var name string
	if nameIdx < len(frame.Func.Constants) {
		name = frame.Func.Constants[nameIdx].ToString()
	}
	msg := "Assignment to constant variable"
	if name != "" {
		msg = "Assignment to constant variable '" + name + "'"
	}
	frame.Thrown = NewString("TypeError: " + msg)
	if frame.HandlerPC >= 0 {
		frame.PC = frame.HandlerPC
		return
	}
	if frame.FinallyPC >= 0 {
		frame.PC = frame.FinallyPC
		return
	}
}

// ensureGlobalSlots grows the globalSlots array to accommodate at least n slots.
// Called before executing a BytecodeFunction that uses slot-based global ops.
// Also ensures bf.GlobalVals is sized correctly and copies current slot values.
func (vm *VM) ensureGlobalSlots(n int, bf *BytecodeFunction) {
	if n > len(vm.globalSlots) {
		newSlots := make([]JSValue, n)
		copy(newSlots, vm.globalSlots)
		vm.globalSlots = newSlots
		newSet := make([]bool, n)
		copy(newSet, vm.globalSlotsSet)
		vm.globalSlotsSet = newSet
		newNames := make([]string, n)
		copy(newNames, vm.globalSlotNames)
		vm.globalSlotNames = newNames
	}
	// Ensure bf.GlobalVals is large enough and synced with current slot values.
	if bf != nil && n > len(bf.GlobalVals) {
		newVals := make([]JSValue, n)
		copy(newVals, bf.GlobalVals)
		bf.GlobalVals = newVals
	}
}

func (vm *VM) execute(bf *BytecodeFunction) JSValue {
	// Reset step counter and allocator bump pointers for this top-level execution.
	vm.stepCount = 0
	vm.alloc.Reset()
	// Ensure global slot array is large enough for this function's globals.
	if len(bf.GlobalSlots) > 0 {
		vm.ensureGlobalSlots(len(bf.GlobalSlots), bf)
	}

	regs := vm.allocRegs(bf.NumRegisters)
	frame := vm.allocFrame()
	frame.Func = bf
	frame.Regs = regs
	frame.HandlerPC = -1
	frame.FinallyPC = -1
	frame.This = vm.globalObject()
	frame.ICVector = bf.ICVector

	// If TurboFan native code is already compiled, dispatch directly (highest tier).
	if !vm.DisableJIT && bf.TurboFan != 0 {
		vm.executeTurboFan(frame)
		vm.freeRegs(regs)
		vm.freeFrame(frame)
		return frame.Acc
	}

	// If Sparkplug native code is already compiled, dispatch directly.
	if !vm.DisableJIT && bf.Sparkplug != 0 {
		_ = bf.Sparkplug // Sparkplug active
		vm.executeSparkplug(frame)
		// If a deoptimization occurred (type guard failed), the deopt stub
		// called GoDeoptimize which cleared frame.InSparkplug. Fall through
		// to the interpreter loop to re-execute the failing instruction.
		if frame.InSparkplug {
			vm.freeRegs(regs)
			vm.freeFrame(frame)
			return frame.Acc
		}
		// Deopt: continue in interpreter below.
	}

	for frame.PC < len(frame.Func.Instructions) {
		if vm.executeOne(frame) {
			// Exception handling: jump to catch handler if set.
			if frame.Thrown.Tag != TagUndefined && frame.HandlerPC >= 0 {
				frame.PC = frame.HandlerPC
				frame.HandlerPC = -1
				frame.Thrown = Undefined
				frame.ShouldReturn = false
				continue
			}
			// Exception with finally (no catch): jump to finally, then re-throw.
			if frame.Thrown.Tag != TagUndefined && frame.FinallyPC >= 0 {
				frame.PC = frame.FinallyPC
				frame.FinallyPC = -1
				continue
			}
			vm.freeRegs(regs)
			vm.freeFrame(frame)
			return frame.Acc
		}
	}
	vm.freeRegs(regs)
	vm.freeFrame(frame)
	return frame.Acc
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
	if JITProtectHook != nil {
		JITProtectHook(true)
		defer JITProtectHook(false)
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
	if JITProtectHook != nil {
		JITProtectHook(true)
		defer JITProtectHook(false)
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
}

// globalObject returns a reference to the VM's cached global object.
// The global object is created once in NewVM and reused across all calls.
func (vm *VM) globalObject() JSValue {
	return vm.globalThis
}

// Lock / Unlock expose vm.mu so external callers (e.g. event-loop timer
// callbacks) can serialise with vm.Run / vm.Execute.
func (vm *VM) Lock()   { vm.mu.Lock() }
func (vm *VM) Unlock() { vm.mu.Unlock() }

// CallMethodLocked invokes a callable JSValue with an explicit receiver.
// The caller MUST hold vm.mu (via Lock()) to serialise with vm.Run.
func (vm *VM) CallMethodLocked(callee JSValue, thisObj *JSObject, args []JSValue) JSValue {
	return vm.callMethod(callee, thisObj, args)
}

// callMethod handles method invocation with explicit receiver (this).
func (vm *VM) callMethod(callee JSValue, thisObj *JSObject, args []JSValue) JSValue {
	// Resolve string callee names to built-in functions.
	if callee.IsString() {
		name := callee.ToString()
		if fn, ok := vm.builtins[name]; ok {
			return fn(args)
		}
		return Undefined
	}
	if !callee.IsObject() || callee.ObjVal == nil {
		return Undefined
	}
	obj := callee.ObjVal

	// Built-in Go function: pass the receiver as `this`.
	if obj.CallFunc != nil {
		if thisObj == nil {
			thisObj = obj
		}
		return obj.CallFunc(thisObj, args)
	}

	// User-defined bytecode function.
	if obj.Bytecode != nil {
		if obj.Bytecode.Async {
			return vm.createAsyncFunction(obj.Bytecode, thisObj, args)
		}
		if obj.Bytecode.Generator {
			return vm.createGeneratorObject(obj.Bytecode, thisObj, args)
		}
		if vm.callDepth > maxCallDepth {
			return Undefined
		}
		vm.callDepth++
		regs := vm.allocRegs(obj.Bytecode.NumRegisters)
		newFrame := vm.allocFrame()
		newFrame.Func = obj.Bytecode
		newFrame.Regs = regs
		newFrame.HandlerPC = -1
		newFrame.FinallyPC = -1
		if thisObj != nil {
			newFrame.This = NewObject(thisObj)
		} else {
			newFrame.This = vm.globalObject()
		}
		if obj.Bytecode.IsDerivedConstructor {
			newFrame.SuperCalled = false
		}
		if envVal := obj.Get("__env__"); envVal.IsObject() && envVal.ObjVal != nil {
			newFrame.ClosureEnv = envVal.ObjVal
		}
		if obj.Bytecode.ICVector != nil {
			newFrame.ICVector = obj.Bytecode.ICVector
		}
		for i, arg := range args {
			if i < len(newFrame.Regs) {
				newFrame.Regs[i] = arg
			}
		}
		if obj.Bytecode.HasRestParam {
			restReg := obj.Bytecode.RestParamReg
			numNonRest := obj.Bytecode.NumParams
			if int(numNonRest) < len(args) {
				restArgs := args[numNonRest:]
				restArray := NewJSObject()
				restArray.ConstructorName = "Array"
				restArray.Prototype = ArrayPrototype
				restArray.growProperties(len(restArgs))
				restArray.Set("length", NewNumber(float64(len(restArgs))))
				for j, a := range restArgs {
					restArray.Set(intKey(j), a)
				}
				if restReg < len(newFrame.Regs) {
					newFrame.Regs[restReg] = NewObject(restArray)
				}
			}
		}
		result := vm.executeFrame(newFrame)
		vm.freeFrame(newFrame)
		vm.freeRegs(regs)
		vm.callDepth--
		return result
	}

	return Undefined
}

// createGeneratorObject creates a generator object for a generator function.
// This is called when a generator function is invoked — the body does NOT execute yet.
func (vm *VM) createGeneratorObject(bf *BytecodeFunction, thisObj *JSObject, args []JSValue) JSValue {
	genObj := NewJSObject()
	genObj.ConstructorName = "Generator"

	// Store the bytecode for later resumption.
	bfObj := NewJSObject()
	bfObj.Bytecode = bf
	genObj.Set("__bytecode__", NewObject(bfObj))

	// Store the initial `this` and arguments.
	thisVal := vm.globalObject()
	if thisObj != nil {
		thisVal = NewObject(thisObj)
	}
	genObj.Set("__this__", thisVal)

	// Store initial args for first .next() call.
	argsArr := NewJSObject()
	argsArr.ConstructorName = "Array"
	for i, arg := range args {
		argsArr.Set(intKey(i), arg)
	}
	genObj.Set("__args__", NewObject(argsArr))

	// Create generator state.
	gs := &GeneratorState{
		GenObj: genObj,
	}
	// Store gs in genObj for internal access.
	gsObj := NewJSObject()
	gsObj.generatorState = gs
	genObj.Set("__genstate__", NewObject(gsObj))

	// Attach .next(), .return(), .throw() methods.
	genObj.Set("next", vm.makeGeneratorNext(genObj, gs))
	genObj.Set("return", vm.makeGeneratorReturn(genObj, gs))
	genObj.Set("throw", vm.makeGeneratorThrow(genObj, gs))

	return NewObject(genObj)
}

// createAsyncFunction wraps an async bytecode function to return a Promise.
// Async functions are compiled as generators (function*). This function creates
// the generator, then drives it via a recursive Promise chain (the "spawn" pattern).
// Each yield in the async function corresponds to an await — the spawner
// calls Promise.resolve(yieldedValue).then(resume) to chain continuations.
func (vm *VM) createAsyncFunction(bf *BytecodeFunction, thisObj *JSObject, args []JSValue) JSValue {
	// Create the underlying generator.
	genVal := vm.createGeneratorObject(bf, thisObj, args)
	if !genVal.IsObject() || genVal.ObjVal == nil {
		return Undefined
	}
	genObj := genVal.ObjVal

	// Extract generator methods.
	genNext := genObj.Get("next")
	genThrow := genObj.Get("throw")

	// Create the promise that the async function returns.
	return vm.NewPromise(func(resolve func(JSValue), reject func(JSValue)) {
		var step func(nextFn JSValue, prevValue JSValue)

		step = func(nextFn JSValue, prevValue JSValue) {
			var result JSValue
			if nextFn.IsObject() && nextFn.ObjVal != nil && nextFn.ObjVal.isCallable() {
				callArgs := []JSValue{}
				if prevValue.Tag != TagUndefined {
					callArgs = append(callArgs, prevValue)
				}
				result = nextFn.ObjVal.Call(genObj, callArgs)
			} else {
				reject(NewString("Async function: generator.next is not callable"))
				return
			}

			if !result.IsObject() || result.ObjVal == nil {
				reject(NewString("Async function: generator.next returned non-object"))
				return
			}

			doneVal := result.ObjVal.Get("done")
			valueVal := result.ObjVal.Get("value")

			if doneVal.IsTruthy() {
				resolve(valueVal)
				return
			}

			// Chain: Promise.resolve(value).then(
			//   resolved => step(genNext, resolved),
			//   rejected  => step(genThrow, rejected))
			promiseResolve := vm.GetGlobal("Promise")
			if !promiseResolve.IsObject() || promiseResolve.ObjVal == nil {
				reject(NewString("Async function: Promise is not available"))
				return
			}
			resolveFn := promiseResolve.ObjVal.Get("resolve")
			if !resolveFn.IsObject() || resolveFn.ObjVal == nil || !resolveFn.ObjVal.isCallable() {
				reject(NewString("Async function: Promise.resolve is not callable"))
				return
			}
			resolvedPromiseVal := resolveFn.ObjVal.Call(nil, []JSValue{valueVal})
			if !resolvedPromiseVal.IsObject() || resolvedPromiseVal.ObjVal == nil {
				reject(NewString("Async function: Promise.resolve returned non-object"))
				return
			}

			thenFn := resolvedPromiseVal.ObjVal.Get("then")
			if !thenFn.IsObject() || thenFn.ObjVal == nil || !thenFn.ObjVal.isCallable() {
				reject(NewString("Async function: .then is not callable"))
				return
			}

			onFulfilled := vm.createBuiltinFunction("", func(this *JSObject, a []JSValue) JSValue {
				nextArg := Undefined
				if len(a) > 0 {
					nextArg = a[0]
				}
				step(genNext, nextArg)
				return Undefined
			})
			onRejected := vm.createBuiltinFunction("", func(this *JSObject, a []JSValue) JSValue {
				errArg := Undefined
				if len(a) > 0 {
					errArg = a[0]
				}
				step(genThrow, errArg)
				return Undefined
			})

			thenFn.ObjVal.Call(resolvedPromiseVal.ObjVal, []JSValue{onFulfilled, onRejected})
		}

		step(genNext, Undefined)
	})
}

// callFunction handles function invocation (backward compat).
func (vm *VM) callFunction(frame *VMFrame, callee JSValue, args []JSValue) JSValue {
	return vm.callMethod(callee, nil, args)
}

// makeFunctionObject creates a callable JSObject from bytecode.
func (vm *VM) makeFunctionObject(name string, bf *BytecodeFunction) JSValue {
	obj := NewJSObject()
	obj.ConstructorName = "Function"

	// Async functions must return a Promise; generator functions must return
	// a generator object.  Delegate through callMethod so the Async/Generator
	// flags on the BytecodeFunction are respected (callMethod checks
	// obj.Bytecode before obj.CallFunc, so we set Bytecode here and use a
	// shim CallFunc that enters callMethod).
	if bf.Async || bf.Generator {
		obj.Bytecode = bf
		obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
			return vm.callMethod(NewObject(obj), this, args)
		}
	} else {
		obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
			regs := vm.allocRegs(bf.NumRegisters)
			defer vm.freeRegs(regs)
			newFrame := vm.allocFrame()
			newFrame.Func = bf
			newFrame.Regs = regs
			newFrame.HandlerPC = -1
			newFrame.FinallyPC = -1
			if this != nil {
				newFrame.This = NewObject(this)
			} else {
				newFrame.This = vm.globalObject()
			}
			for i, arg := range args {
				if i < len(newFrame.Regs) {
					newFrame.Regs[i] = arg
				}
			}
			result := vm.executeFrame(newFrame)
			vm.freeFrame(newFrame)
			return result
		}
	}
	vm.funcRegistry[name] = bf
	return NewObject(obj)
}

func (vm *VM) makeBuiltinFunction(name string, fn func(args []JSValue) JSValue) JSValue {
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		return fn(args)
	}
	vm.builtins[name] = fn
	return NewObject(obj)
}

func (vm *VM) executeFrame(frame *VMFrame) JSValue {
	frame.HandlerPC = -1
	frame.FinallyPC = -1
	// Note: frame.This must be set by the caller (execute, callMethod, makeFunctionObject).

	// Tier promotion: increment call count and trigger JIT compilation
	// at thresholds. This is the hot path for all function calls.
	vm.maybePromoteTier(frame.Func)

	// If TurboFan native code is already compiled, dispatch directly (highest tier).
	if !vm.DisableJIT && frame.Func.TurboFan != 0 {
		vm.executeTurboFan(frame)
		return frame.Acc
	}

	// If Sparkplug native code is already compiled, dispatch directly.
	if !vm.DisableJIT && frame.Func.Sparkplug != 0 {
		vm.executeSparkplug(frame)
		// If deopt occurred (InSparkplug cleared), fall through to interpreter.
		if frame.InSparkplug {
			return frame.Acc
		}
	}

	for frame.PC < len(frame.Func.Instructions) {
		if vm.executeOne(frame) {
			// Exception handling: jump to catch handler if set.
			if frame.Thrown.Tag != TagUndefined && frame.HandlerPC >= 0 {
				frame.PC = frame.HandlerPC
				frame.HandlerPC = -1
				frame.Thrown = Undefined
				frame.ShouldReturn = false
				continue
			}
			// Exception with finally (no catch): jump to finally, then re-throw.
			if frame.Thrown.Tag != TagUndefined && frame.FinallyPC >= 0 {
				frame.PC = frame.FinallyPC
				frame.FinallyPC = -1
				continue
			}
			return frame.Acc
		}
	}
	return frame.Acc
}

// --- JavaScript operations ---

// throwTypeErrorInFrame sets up a TypeError exception in the current frame.
// It sets frame.Thrown and frame.ShouldReturn so the VM dispatch loop
// catches it and routes through exception handlers.
// recordBinaryFeedback records type feedback for binary operation sites
// to enable JIT specialization based on observed operand types.
func recordBinaryFeedback(frame *VMFrame, instr Instruction) {
	slotIdx := int(instr.OperandC)
	if frame.Func.ICVector != nil && slotIdx < len(frame.Func.ICVector.Slots) {
		slot := &frame.Func.ICVector.Slots[slotIdx]
		slot.ObservedTag = frame.Acc.Tag
		slot.HitCount++
	}
}

func throwTypeErrorInFrame(frame *VMFrame, msg string) {
	frame.Thrown = NewString("TypeError: " + msg)
	frame.Acc = frame.Thrown
	frame.ShouldReturn = true
}

// throwReferenceErrorInFrame sets up a ReferenceError exception in the current frame.
func throwReferenceErrorInFrame(frame *VMFrame, msg string) {
	frame.Thrown = NewString("ReferenceError: " + msg)
	frame.Acc = frame.Thrown
	frame.ShouldReturn = true
}

// throwRangeErrorInFrame sets up a RangeError exception in the current frame.
func throwRangeErrorInFrame(frame *VMFrame, msg string) {
	frame.Thrown = NewString("RangeError: " + msg)
	frame.Acc = frame.Thrown
	frame.ShouldReturn = true
}

// jsAdd implements the ECMAScript addition operator (+).
func jsAdd(a, b JSValue) JSValue {
	// Fast path: both numbers — avoid ToNumber/NewNumber overhead.
	if a.Tag == TagNumber && b.Tag == TagNumber {
		result := a.NumVal + b.NumVal
		if isSmallInt(result) {
			return smallIntValue(int(result))
		}
		return JSValue{Tag: TagNumber, NumVal: result}
	}
	// If either operand is a string, do string concatenation.
	if a.IsString() || b.IsString() {
		return NewString(a.ToString() + b.ToString())
	}
	// BigInt addition.
	if a.IsBigInt() || b.IsBigInt() {
		if a.IsBigInt() && b.IsBigInt() {
			result := new(big.Int).Add(a.BigIntVal, b.BigIntVal)
			return JSValue{Tag: TagBigInt, BigIntVal: result}
		}
		// Mixed BigInt + Number → TypeError per spec.
		return NewNumber(math.NaN())
	}
	// Otherwise, numeric addition.
	result := a.ToNumber() + b.ToNumber()
	if isSmallInt(result) {
		return smallIntValue(int(result))
	}
	return JSValue{Tag: TagNumber, NumVal: result}
}

// bigIntMixedTypeError checks if exactly one of a, b is BigInt and the other
// is Number. Returns true if a TypeError should be thrown.
func bigIntMixedTypeError(a, b JSValue) bool {
	bigIntA, bigIntB := a.IsBigInt(), b.IsBigInt()
	numA, numB := a.IsNumber(), b.IsNumber()
	return (bigIntA && numB) || (bigIntB && numA)
}

// jsTypeof implements the ECMAScript typeof operator.
func jsTypeof(v JSValue) string {
	switch v.Tag {
	case TagUndefined: return "undefined"
	case TagNull: return "object"
	case TagBoolean: return "boolean"
	case TagNumber: return "number"
	case TagString: return "string"
	case TagObject:
		if v.ObjVal != nil && (v.ObjVal.isCallable() || v.ObjVal.Bytecode != nil) {
			return "function"
		}
		return "object"
	case TagSymbol:
		return "symbol"
	case TagBigInt:
		return "bigint"
	}
	return "undefined"
}

// --- Event handling ---

// AddEventListener registers a callback for the given event type.
// Callbacks are stored in the VM's listener map and invoked by FireEvent.
func (vm *VM) AddEventListener(eventType string, callback JSValue) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if vm.listeners == nil {
		vm.listeners = make(map[string][]JSValue, 4)
	}
	vm.listeners[eventType] = append(vm.listeners[eventType], callback)
}

// FireEvent dispatches an event to all registered listeners for eventType.
// Additionally, for "load" events, the window.onload handler is invoked.
// Called by the browser after DOM parse (DOMContentLoaded) and after
// subresource loading (load), per the HTML Standard § 8.1.7 "Event loops".
func (vm *VM) FireEvent(eventType string, data map[string]JSValue) {
	vm.mu.Lock()
	// Snapshot listeners to avoid holding lock during callback execution.
	var callbacks []JSValue
	if vm.listeners != nil {
		listCopy := make([]JSValue, len(vm.listeners[eventType]))
		copy(listCopy, vm.listeners[eventType])
		callbacks = listCopy
	}
	onloadCB := vm.onloadHandler
	vm.mu.Unlock()

	// Fire registered addEventListener callbacks.
	for _, cb := range callbacks {
		if cb.IsObject() && cb.ObjVal != nil && cb.ObjVal.isCallable() {
			// Build event object with type and data.
			eventObj := NewJSObject()
			eventObj.Set("type", NewString(eventType))
			if data != nil {
				for k, v := range data {
					eventObj.Set(k, v)
				}
			}
			cb.ObjVal.Call(nil, []JSValue{NewObject(eventObj)})
		}
	}

	// For "load" events, also fire window.onload if set.
	if eventType == "load" && onloadCB.IsObject() && onloadCB.ObjVal != nil && onloadCB.ObjVal.isCallable() {
		eventObj := NewJSObject()
		eventObj.Set("type", NewString("load"))
		onloadCB.ObjVal.Call(nil, []JSValue{NewObject(eventObj)})
	}
}

// SetOnloadHandler stores the window.onload callback (set via property setter).
func (vm *VM) SetOnloadHandler(callback JSValue) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.onloadHandler = callback
}

// GetOnloadHandler returns the current window.onload callback.
func (vm *VM) GetOnloadHandler() JSValue {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.onloadHandler
}

// SetElementLookup registers a callback for looking up DOM elements by ID.
// Called by the browser/Gov8Engine to bridge VM event dispatch with the DOM tree.
func (vm *VM) SetElementLookup(fn func(string) *dom.Element) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.elementByID = fn
}

// SetDOMChangeCallback registers a callback invoked when the VM executes an
// inline event handler that may have mutated the DOM (triggering a repaint).
func (vm *VM) SetDOMChangeCallback(fn func()) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.domChangeCallback = fn
}

// DispatchEvent finds an element by targetID, reads its inline event handler
// for eventType (e.g., "click" → onclick attribute), and executes the handler
// code in the VM with a synthetic event object { type, target, preventDefault }.
func (vm *VM) DispatchEvent(targetID, eventType string, eventData map[string]JSValue) {
	vm.mu.Lock()
	lookup := vm.elementByID
	vm.mu.Unlock()

	if lookup == nil {
		return
	}

	elem := lookup(targetID)
	if elem == nil {
		return
	}

	handlerCode := elem.GetEventHandler(eventType)
	if handlerCode == "" {
		return
	}

	// Build event object visible to the handler code.
	// The event object has: type, target (element ID), preventDefault().
	eventObj := NewJSObject()
	eventObj.Set("type", NewString(eventType))
	eventObj.Set("target", NewString(targetID))
	if eventData != nil {
		for k, v := range eventData {
			eventObj.Set(k, v)
		}
	}
	defaultPrevented := false
	eventObj.Set("preventDefault", NewObject(builtinFunc("preventDefault", func(this *JSObject, args []JSValue) JSValue {
		defaultPrevented = true
		return Undefined
	})))

	// Store event object as a global for handler code to access.
	vm.mu.Lock()
	vm.globals["__event__"] = NewObject(eventObj)
	vm.mu.Unlock()

	// Run the handler code in the VM.
	_ = vm.Run(handlerCode)

	// If preventDefault was called, notify the browser via a flag on the VM.
	// The caller can check via event return, but for now the handler runs in-process.
	_ = defaultPrevented

	// Trigger DOM change callback after inline handler execution (may have mutated DOM).
	vm.mu.Lock()
	cb := vm.domChangeCallback
	vm.mu.Unlock()
	if cb != nil {
		cb()
	}
}
