// bytecode.go — Ignition-style bytecode instruction set and supporting types.
//
// Design follows V8 Ignition: fixed-width instructions with opcode + operands,
// a side-table for constants, and register-based addressing.
package js

// Opcode enumerates all bytecode instructions in the V8Go VM.
type Opcode uint8

const (
	OpNop Opcode = iota

	// Stack / register movement
	OpLdaConstant   // Load accumulator from constant pool
	OpLdaUndefined  // Load undefined into accumulator
	OpLdaNull       // Load null into accumulator
	OpLdaTrue       // Load true into accumulator
	OpLdaFalse      // Load false into accumulator
	OpLdaZero       // Load 0 into accumulator
	OpLdaOne        // Load 1 into accumulator
	OpLdaSmi        // Load small integer (int8 operand) → acc
	OpStar          // Store accumulator → register
	OpLdar          // Load register → accumulator
	OpMov           // Move register → register

	// Arithmetic (result in accumulator)
	OpAdd           // acc + reg → acc
	OpSub           // acc - reg → acc
	OpMul           // acc * reg → acc
	OpDiv           // acc / reg → acc
	OpMod           // acc % reg → acc
	OpExp           // acc ** reg → acc (exponentiation)
	OpNegate        // -acc → acc
	OpInc           // ++acc (read-modify-write reg, result in acc)
	OpDec           // --acc

	// Comparison (result in accumulator as boolean)
	OpEq            // acc == reg → acc (loose)
	OpNotEq         // acc != reg → acc (loose)
	OpStrictEq      // acc === reg → acc
	OpStrictNotEq   // acc !== reg → acc
	OpLessThan      // acc < reg → acc
	OpGreaterThan   // acc > reg → acc
	OpLessEq        // acc <= reg → acc
	OpGreaterEq     // acc >= reg → acc

	// Logical
	OpLogicalNot    // !acc → acc
	OpLogicalAnd    // acc && reg → acc (short-circuit handled by compiler)
	OpLogicalOr     // acc || reg → acc (short-circuit handled by compiler)

	// Bitwise
	OpBitwiseAnd    // acc & reg → acc
	OpBitwiseOr     // acc | reg → acc
	OpBitwiseXor    // acc ^ reg → acc
	OpBitwiseNot    // ~acc → acc
	OpShiftLeft     // acc << reg → acc
	OpShiftRight    // acc >> reg → acc
	OpShiftRightZero // acc >>> reg → acc

	// Type conversion
	OpToNumber      // ToNumber(acc) → acc
	OpToString      // ToString(acc) → acc
	OpToBoolean     // ToBoolean(acc) → acc
	OpTypeof        // typeof acc → acc
	OpDelete        // delete obj.prop → acc
	OpDeleteKeyed   // delete obj[key] → acc: OperandA=objReg, OperandB=keyReg
	OpInstanceof    // obj instanceof constructor → acc
	OpIn            // prop in object → acc

	// Property access
	OpLdaNamedProperty     // acc = object[constant_property_name]
	OpStaNamedProperty     // object[constant_property_name] = acc
	OpLdaKeyedProperty     // acc = object[key_reg]
	OpStaKeyedProperty     // object[key_reg] = acc

	// Control flow
	OpJump          // unconditional jump
	OpJumpIfFalse   // jump if !acc (ToBoolean)
	OpJumpIfTrue    // jump if acc (ToBoolean)
	OpJumpIfToBooleanTrue  // jump if ToBoolean(acc) is true
	OpJumpIfToBooleanFalse // jump if ToBoolean(acc) is false
	OpJumpIfNotNullish     // jump if acc is NOT null or undefined (for ?? short-circuit)

	// Functions
	OpCall          // call function in reg with N args
	OpCall0         // call with 0 args
	OpCall1         // call with 1 arg
	OpCall2         // call with 2 args
	OpCallSpread    // call with spread argument(s): unpack array at calleeReg+1+fixedCount
	OpReturn        // return from function with value in acc
	OpCreateClosure // create a closure from a function template
	OpLdaCaptured   // load captured variable from closure environment → acc

	// Objects and arrays
	OpCreateObject        // create empty object → acc
	OpCreateObjectLiteral // create object with pre-built Shape: OperandA=shape key constant idx → acc
	OpCreateArray         // create empty array → acc
	OpCreateRegExp        // create RegExp object: pattern=Constants[OperandA], flags=Constants[OperandB] → acc

	// Exception handling
	OpThrow         // throw value in acc
	OpSetTryHandler // set exception handler PC to operandA
	OpClearTryHandler // clear exception handler
	OpSetFinallyHandler // set finally handler PC (for try+finally return interception)

	// Iteration
	OpForInSetup    // prepare for-in iteration on object in reg → acc = first key or undefined
	OpForInNext     // get next key from iteration → acc = next key or undefined

	// Variables
	OpLdaGlobal     // load global variable → acc (by constant-pool name index)
	OpStaGlobal     // store acc → global variable (by constant-pool name index)
	OpLdaGlobalSlot // load global variable → acc (by pre-assigned slot index)
	OpStaGlobalSlot // store acc → global variable (by pre-assigned slot index)
	OpLdaLocal      // load local variable → acc (register index)
	OpStaLocal      // store acc → local variable (register index)
	OpLdaThis       // load this → acc

	// Stack / register manipulation
	OpDup         // duplicate accumulator → register (OperandA = dest reg)
	OpStaByOffset // store acc → object[offset]: OperandA=objReg, OperandB=offset, OperandC=valReg

	// Const check
	OpThrowConstAssignment // throw TypeError on const reassignment (OperandA = name constant index)

	// Object prototype
	OpSetPrototype // set acc as prototype of object in reg (OperandA = object reg)

	// Generators
	OpYield         // pause generator, save state, return acc as {value, done:false}
	OpYieldDelegate // yield* expression: delegate to another iterable
	OpCreateGenerator // create generator object from function template in acc

	// Super
	OpSuperCall        // super() call in derived class constructor
	OpCheckConstructor // throw TypeError if acc is not null and not a constructor

	// --- Extended typed fast-path ops (compiler emits when types are known) ---
	// Arithmetic (no type guards — compiler has proven operands are numbers)
	OpAddNumber       // acc + reg → acc (both TagNumber)
	OpSubNumber       // acc - reg → acc (both TagNumber)
	OpMulNumber       // acc * reg → acc (both TagNumber)
	OpDivNumber       // acc / reg → acc (both TagNumber)
	OpModNumber       // acc % reg → acc (both TagNumber)
	OpNegateNumber    // -acc → acc (TagNumber)
	OpIncNumber       // ++reg (TagNumber) → acc & reg
	OpDecNumber       // --reg (TagNumber) → acc & reg

	// Comparison (no type guards — compiler has proven operands are numbers)
	OpCmpNumber       // acc <=> reg → acc as boolean (both TagNumber, cond in OperandC)
	OpStrictEqNumber  // acc === reg → acc (both TagNumber)
	OpStrictNotEqNumber // acc !== reg → acc (both TagNumber)
	OpLessThanNumber  // acc < reg → acc (both TagNumber)
	OpGreaterThanNumber // acc > reg → acc (both TagNumber)
	OpLessEqNumber    // acc <= reg → acc (both TagNumber)
	OpGreaterEqNumber // acc >= reg → acc (both TagNumber)

	// Bitwise (no type guards — compiler has proven operands are int32-capable numbers)
	OpBitAndNumber    // acc & reg → acc (both TagNumber, int32 result)
	OpBitOrNumber     // acc | reg → acc
	OpBitXorNumber    // acc ^ reg → acc
	OpBitNotNumber    // ~acc → acc
	OpShiftLeftNumber  // acc << reg → acc (both TagNumber, int32 result)
	OpShiftRightNumber // acc >> reg → acc
	OpShiftRightZeroNumber // acc >>> reg → acc

	// Type conversion fast paths (compiler has proven the value's type)
	OpToBooleanNumber // ToBoolean(TagNumber acc) → acc (inline, no type check)
	OpToStringNumber  // ToString on a known number → acc

	// Object/Array fast paths (compiler has resolved shape at compile time)
	OpLdaPropByOffset  // load property by fixed offset: OperandA=objReg, OperandB=byteOffset → acc
	OpStaPropByOffset  // store property by fixed offset: OperandA=objReg, OperandB=byteOffset, OperandC=valReg
	OpArrayGetIndex    // indexed array get with bounds check: OperandA=arrReg, OperandB=idxReg → acc
	OpArraySetIndex    // indexed array set with bounds check: OperandA=arrReg, OperandB=idxReg, OperandC=valReg
	OpArrayLength      // arr.length → acc: OperandA=arrReg

	// String fast paths
	OpStringLength     // str.length → acc: OperandA=strReg
	OpStringConcat     // acc + reg → acc (both strings, no coercion)
	OpStringEq         // acc == reg → acc (both strings)

	// Function/Closure fast paths
	OpCallBuiltin      // call builtin function: OperandA=builtinIdx, OperandB=argCount
	OpCallDirect       // direct call known function: OperandA=funcReg, OperandB=argCount
	OpCreateEmptyArray // create [] with preallocated capacity: OperandA=capacity → acc

	// Global variable fast paths (when slot is pre-resolved)
	OpLdaGlobalDirect  // load global by direct slot: OperandA=slotIdx → acc
	OpStaGlobalDirect  // store to global by direct slot: OperandA=slotIdx

	// Environment/Scope fast paths
	OpLoadContextSlot  // load from context slot: OperandA=contextIdx, OperandB=slotIdx → acc
	OpStoreContextSlot // store to context slot: OperandA=contextIdx, OperandB=slotIdx
	OpPushContext      // push a new block context
	OpPopContext       // pop block context

	// Math builtins (fast inline when possible)
	OpMathAbs          // Math.abs(acc) → acc
	OpMathFloor        // Math.floor(acc) → acc
	OpMathCeil         // Math.ceil(acc) → acc
	OpMathSqrt         // Math.sqrt(acc) → acc

	// Loop optimized ops
	OpForInSetupFast   // fast for-in setup with cached enumerator
	OpForInNextFast    // fast for-in next with direct property access

	// Register/Stack extended ops
	OpSwap             // swap acc with register (OperandA = reg)
	OpLdaTrueFast      // load true unconditionally → acc
	OpLdaFalseFast     // load false unconditionally → acc

	// Debugger
	OpDebugger // debugger statement (no-op in JIT)

	// Module variables
	OpLdaModuleVar // load module variable by slot index → acc
	OpStaModuleVar // store acc → module variable by slot index

	// Throw helpers
	OpThrowIfNotSuper   // throw ReferenceError if function is not a derived constructor
	OpThrowIfHole       // throw ReferenceError for TDZ (temporal dead zone) access
	OpCatch             // begin catch block: store thrown exception to register (OperandA = reg)
	OpEndTry            // mark end of try block: clear handler PC

	// Super/constructor completion
	OpThrowSuperAlreadyCalled // throw TypeError if super() already called
	OpThrowSuperNotCalled     // throw ReferenceError if super() not called before this access
	OpGetSuperConstructor     // load the super constructor ([[Prototype]] of the class) → acc
	OpLdaHomeObject           // load [[HomeObject]] of the current function → acc
	OpLdaHomeObjectProperty   // load super.prop: OperandA=prop name const idx, OperandB=home obj reg
	OpStaHomeObjectProperty   // store super.prop: OperandA=prop name const idx, OperandB=home obj reg
	OpInitDerived             // mark this as initialized in derived constructor (after super() completes)

	// Slot increment/decrement ops
	OpCheckThisReinit  // throw ReferenceError if this reinitialized in derived constructor
	OpIncGlobalSlot    // increment global slot value: OperandA=slotIdx → acc
	OpDecGlobalSlot    // decrement global slot value: OperandA=slotIdx → acc
	OpIncNamedProperty // obj.prop += 1: OperandA=propIdx, OperandB=objReg → acc
	OpDecNamedProperty // obj.prop -= 1: OperandA=propIdx, OperandB=objReg → acc
	OpIncKeyedProperty // obj[key] += 1: OperandA=objReg, OperandB=keyReg → acc
	OpDecKeyedProperty // obj[key] -= 1: OperandA=objReg, OperandB=keyReg → acc

	// Generator completion
	OpSuspendGenerator     // pause generator and save resume PC: OperandA=resumePC
	OpResumeGenerator      // resume generator from saved PC: OperandA=resumePC
	OpGetIterator          // get @@iterator from object in acc → iterator object in acc
	OpIteratorNext         // call iterator.next(): OperandA=iterator reg → acc = {value, done}
	OpIteratorClose        // call iterator.return(): OperandA=iterator reg
	OpCreateGeneratorObject // create generator object from suspended state
	OpGeneratorRestore     // restore generator frame state on resume

	// Async/await
	OpAwait                // await expression: pause async function, wait for promise
	OpCreateAsyncGenerator // create async generator object from function template
	OpAsyncAwait           // specialized await for async generators
	OpAsyncReturn          // return from async function (wrap result in promise)

	// Object utilities
	OpCopyDataProperties   // copy data properties from source to target: OperandA=src, OperandB=dst
	OpToObject             // ToObject(acc): convert primitive to wrapper object → acc

	// Construct / new
	OpNew                  // new Constructor(): OperandA=constructorReg, OperandB=argCount

	// Error throwing
	OpThrowReferenceError  // throw ReferenceError: OperandA=name constant index
	OpThrowTypeError       // throw TypeError: OperandA=message constant index

	// Optional chaining
	OpOptionalChain        // ?. short-circuit: OperandA=jump target if nullish

	// Nullish coalescing
	OpNullishCoalesce      // ?? operator: OperandA=jump target if not nullish (inverted from OpJumpIfNotNullish)

	// Private fields
	OpPrivateGet           // get #privateField: OperandA=field name const idx, OperandB=objReg
	OpPrivateSet           // set #privateField: OperandA=field name const idx, OperandB=objReg, OperandC=valReg

	// For-of iteration
	OpForOfSetup           // prepare for-of iteration on iterable in reg → acc = iterator
	OpForOfNext            // get next value from for-of iterator → acc = {value, done}

	// Class definition
	OpDefineClass          // define class: OperandA=constructor template const idx
)

var opcodeNames = map[Opcode]string{
	OpNop:           "Nop",
	OpLdaConstant:   "LdaConstant",
	OpLdaUndefined:  "LdaUndefined",
	OpLdaNull:       "LdaNull",
	OpLdaTrue:       "LdaTrue",
	OpLdaFalse:      "LdaFalse",
	OpLdaZero:       "LdaZero",
	OpLdaOne:        "LdaOne",
	OpLdaSmi:        "LdaSmi",
	OpStar:          "Star",
	OpLdar:          "Ldar",
	OpMov:           "Mov",
	OpAdd:           "Add",
	OpSub:           "Sub",
	OpMul:           "Mul",
	OpDiv:           "Div",
	OpMod:           "Mod",
	OpExp:           "Exp",
	OpNegate:        "Negate",
	OpInc:           "Inc",
	OpDec:           "Dec",
	OpEq:            "Eq",
	OpNotEq:         "NotEq",
	OpStrictEq:      "StrictEq",
	OpStrictNotEq:   "StrictNotEq",
	OpLessThan:      "LessThan",
	OpGreaterThan:   "GreaterThan",
	OpLessEq:        "LessEq",
	OpGreaterEq:     "GreaterEq",
	OpLogicalNot:    "LogicalNot",
	OpLogicalAnd:    "LogicalAnd",
	OpLogicalOr:     "LogicalOr",
	OpBitwiseAnd:    "BitwiseAnd",
	OpBitwiseOr:     "BitwiseOr",
	OpBitwiseXor:    "BitwiseXor",
	OpBitwiseNot:    "BitwiseNot",
	OpShiftLeft:     "ShiftLeft",
	OpShiftRight:    "ShiftRight",
	OpShiftRightZero: "ShiftRightZero",
	OpToNumber:      "ToNumber",
	OpToString:      "ToString",
	OpToBoolean:     "ToBoolean",
	OpTypeof:        "Typeof",
	OpDelete:        "Delete",
	OpDeleteKeyed:   "DeleteKeyed",
	OpInstanceof:    "Instanceof",
	OpIn:            "In",
	OpLdaNamedProperty:   "LdaNamedProperty",
	OpStaNamedProperty:   "StaNamedProperty",
	OpLdaKeyedProperty:   "LdaKeyedProperty",
	OpStaKeyedProperty:   "StaKeyedProperty",
	OpJump:          "Jump",
	OpJumpIfFalse:   "JumpIfFalse",
	OpJumpIfTrue:    "JumpIfTrue",
	OpJumpIfToBooleanTrue:  "JumpIfToBooleanTrue",
	OpJumpIfToBooleanFalse: "JumpIfToBooleanFalse",
	OpJumpIfNotNullish:     "JumpIfNotNullish",
	OpCall:          "Call",
	OpCall0:         "Call0",
	OpCall1:         "Call1",
	OpCall2:         "Call2",
	OpCallSpread:    "CallSpread",
	OpReturn:        "Return",
	OpCreateClosure: "CreateClosure",
	OpLdaCaptured:   "LdaCaptured",
	OpCreateObject:        "CreateObject",
	OpCreateObjectLiteral: "CreateObjectLiteral",
	OpCreateArray:         "CreateArray",
	OpCreateRegExp:        "CreateRegExp",
	OpForInSetup:    "ForInSetup",
	OpForInNext:     "ForInNext",
	OpThrow:         "Throw",
	OpSetTryHandler: "SetTryHandler",
	OpClearTryHandler: "ClearTryHandler",
	OpSetFinallyHandler: "SetFinallyHandler",
	OpLdaGlobal:     "LdaGlobal",
	OpStaGlobal:     "StaGlobal",
	OpLdaGlobalSlot: "LdaGlobalSlot",
	OpStaGlobalSlot: "StaGlobalSlot",
	OpLdaLocal:      "LdaLocal",
	OpStaLocal:      "StaLocal",
	OpLdaThis:       "LdaThis",
	OpDup:           "Dup",
	OpStaByOffset:   "StaByOffset",
	OpThrowConstAssignment: "ThrowConstAssignment",
	OpSetPrototype:   "SetPrototype",
	OpYield:          "Yield",
	OpYieldDelegate:  "YieldDelegate",
	OpCreateGenerator: "CreateGenerator",
	OpSuperCall:      "SuperCall",
	OpCheckConstructor: "CheckConstructor",

	// Extended typed fast-path ops
	OpAddNumber:       "AddNumber",
	OpSubNumber:       "SubNumber",
	OpMulNumber:       "MulNumber",
	OpDivNumber:       "DivNumber",
	OpModNumber:       "ModNumber",
	OpNegateNumber:    "NegateNumber",
	OpIncNumber:       "IncNumber",
	OpDecNumber:       "DecNumber",
	OpCmpNumber:       "CmpNumber",
	OpStrictEqNumber:  "StrictEqNumber",
	OpStrictNotEqNumber: "StrictNotEqNumber",
	OpLessThanNumber:  "LessThanNumber",
	OpGreaterThanNumber: "GreaterThanNumber",
	OpLessEqNumber:    "LessEqNumber",
	OpGreaterEqNumber: "GreaterEqNumber",
	OpBitAndNumber:    "BitAndNumber",
	OpBitOrNumber:     "BitOrNumber",
	OpBitXorNumber:    "BitXorNumber",
	OpBitNotNumber:    "BitNotNumber",
	OpShiftLeftNumber:  "ShiftLeftNumber",
	OpShiftRightNumber: "ShiftRightNumber",
	OpShiftRightZeroNumber: "ShiftRightZeroNumber",
	OpToBooleanNumber: "ToBooleanNumber",
	OpToStringNumber:  "ToStringNumber",
	OpLdaPropByOffset: "LdaPropByOffset",
	OpStaPropByOffset: "StaPropByOffset",
	OpArrayGetIndex:   "ArrayGetIndex",
	OpArraySetIndex:   "ArraySetIndex",
	OpArrayLength:     "ArrayLength",
	OpStringLength:    "StringLength",
	OpStringConcat:    "StringConcat",
	OpStringEq:        "StringEq",
	OpCallBuiltin:     "CallBuiltin",
	OpCallDirect:      "CallDirect",
	OpCreateEmptyArray: "CreateEmptyArray",
	OpLdaGlobalDirect: "LdaGlobalDirect",
	OpStaGlobalDirect: "StaGlobalDirect",
	OpLoadContextSlot: "LoadContextSlot",
	OpStoreContextSlot: "StoreContextSlot",
	OpPushContext:     "PushContext",
	OpPopContext:      "PopContext",
	OpMathAbs:         "MathAbs",
	OpMathFloor:       "MathFloor",
	OpMathCeil:        "MathCeil",
	OpMathSqrt:        "MathSqrt",
	OpForInSetupFast:  "ForInSetupFast",
	OpForInNextFast:   "ForInNextFast",
	OpSwap:            "Swap",
	OpLdaTrueFast:     "LdaTrueFast",
	OpLdaFalseFast:    "LdaFalseFast",
	OpDebugger:        "Debugger",
	OpLdaModuleVar:    "LdaModuleVar",
	OpStaModuleVar:    "StaModuleVar",
	OpThrowIfNotSuper: "ThrowIfNotSuper",
	OpThrowIfHole:     "ThrowIfHole",
	OpCatch:           "Catch",
	OpEndTry:          "EndTry",
	OpThrowSuperAlreadyCalled: "ThrowSuperAlreadyCalled",
	OpThrowSuperNotCalled:     "ThrowSuperNotCalled",
	OpGetSuperConstructor:     "GetSuperConstructor",
	OpLdaHomeObject:           "LdaHomeObject",
	OpLdaHomeObjectProperty:   "LdaHomeObjectProperty",
	OpStaHomeObjectProperty:   "StaHomeObjectProperty",
	OpInitDerived:             "InitDerived",
	OpCheckThisReinit:  "CheckThisReinit",
	OpIncGlobalSlot:    "IncGlobalSlot",
	OpDecGlobalSlot:    "DecGlobalSlot",
	OpIncNamedProperty: "IncNamedProperty",
	OpDecNamedProperty: "DecNamedProperty",
	OpIncKeyedProperty: "IncKeyedProperty",
	OpDecKeyedProperty: "DecKeyedProperty",
	OpSuspendGenerator:     "SuspendGenerator",
	OpResumeGenerator:      "ResumeGenerator",
	OpGetIterator:          "GetIterator",
	OpIteratorNext:         "IteratorNext",
	OpIteratorClose:        "IteratorClose",
	OpCreateGeneratorObject: "CreateGeneratorObject",
	OpGeneratorRestore:     "GeneratorRestore",
	OpAwait:                "Await",
	OpCreateAsyncGenerator: "CreateAsyncGenerator",
	OpAsyncAwait:           "AsyncAwait",
	OpAsyncReturn:          "AsyncReturn",
	OpCopyDataProperties:   "CopyDataProperties",
	OpToObject:             "ToObject",
	OpNew:                  "New",
	OpThrowReferenceError:  "ThrowReferenceError",
	OpThrowTypeError:       "ThrowTypeError",
	OpOptionalChain:        "OptionalChain",
	OpNullishCoalesce:      "NullishCoalesce",
	OpPrivateGet:           "PrivateGet",
	OpPrivateSet:           "PrivateSet",
	OpForOfSetup:           "ForOfSetup",
	OpForOfNext:            "ForOfNext",
	OpDefineClass:          "DefineClass",
}

func (o Opcode) String() string {
	if name, ok := opcodeNames[o]; ok {
		return name
	}
	return "Unknown"
}

// Instruction is a single bytecode instruction.
// Uses a compact representation: Opcode (1 byte) + up to 3 operand bytes.
type Instruction struct {
	Op       Opcode
	OperandA uint8 // often a register index
	OperandB uint8 // often a constant index or second register
	OperandC uint8 // opcode-specific (e.g., arg count for Call)
}

// String returns a human-readable representation for debugging.
func (instr Instruction) String() string {
	return instr.Op.String()
}

// BytecodeFunction is the compiled form of a function — equivalent to V8's
// SharedFunctionInfo. Created once during compilation and shared across all
// closures of the same function. Contains bytecode, constant pool, and metadata.
// Fields ordered by size for optimal alignment (largest first).
type BytecodeFunction struct {
	Instructions  []Instruction // Bytecode array — 24 bytes
	Constants     []JSValue     // Constant pool — 24 bytes
	ConstantNames []string      // Pre-computed string versions (for global access) — 24 bytes
	Captured     []string      // names of variables captured from enclosing scope — 24 bytes
	GlobalSlots   []string      // slot index → global variable name (compiled once, used by VM) — 24 bytes
	GlobalVals    []JSValue     // slot index → pre-resolved global value (populated by VM) — 24 bytes
	ShapePropNames [][]string   // pre-split prop names for OpCreateObjectLiteral (indexed by shape key constant idx)
	Shapes         []*Shape     // pre-built Shape pointers (parallel to ShapePropNames, compiled once)
	Name         string        // Function name — 16 bytes
	NumRegisters int           // Number of virtual registers needed — 8 bytes
	NumParams    int           // Number of formal parameters (non-rest) — 8 bytes
	ICVector         *FeedbackVector // Inline caching slots (nil if not used) — 8 bytes
	NumFeedbackSlots int             // number of pre-allocated feedback slots
	Sparkplug        uintptr         // rxAddr of Sparkplug native code (0 if not compiled)
	TurboFan         uintptr         // rxAddr of TurboFan native code (0 if not compiled)
	HasJITTier       bool            // true if Sparkplug or TurboFan compilation completed; gates OSR checks
	PcToNative       map[int]int     // bytecode PC → native code offset (populated by Sparkplug)
	NativeToPc       map[int]int     // native code offset → bytecode PC (reverse lookup for deopt)
	DeoptData        interface{}     // *jit.DeoptimizationInputData stored after Sparkplug compilation
	CallCount        int             // invocation counter for tier-up decisions
	Generator        bool            // true for generator functions (function*) — 1 byte
	Async            bool            // true for async functions — 1 byte
	IsDerivedConstructor bool        // true for derived class constructors (this uninitialized until super()) — 1 byte
	HasRestParam     bool            // true if function has a rest parameter — 1 byte
	RestParamReg     int             // register index for the rest array (valid only if HasRestParam) — 8 bytes
	SourceFile       string          // source file name for Error.stack display
	SourcePositions  []SourcePos     // per-instruction source position (parallel to Instructions)
}

// SourcePos represents a source code position (line, column).
type SourcePos struct {
	Line int
	Col  int
}

// NewBytecodeFunction creates an empty compiled function template.
func NewBytecodeFunction(name string) *BytecodeFunction {
	return &BytecodeFunction{
		Name:         name,
		Instructions: make([]Instruction, 0, 64),
		Constants:    make([]JSValue, 0, 16),
		NumRegisters: 0,
		NumParams:    0,
	}
}

// AddConstant appends a constant value to the pool, deduplicating existing values.
// Uses loose equality (==) for comparison. Returns the index of the constant.
func (bf *BytecodeFunction) AddConstant(val JSValue) int {
	for i, c := range bf.Constants {
		if c.Equals(val) {
			return i
		}
	}
	bf.Constants = append(bf.Constants, val)
	return len(bf.Constants) - 1
}

// Emit appends an instruction.
func (bf *BytecodeFunction) Emit(op Opcode, a, b, c uint8) {
	bf.Instructions = append(bf.Instructions, Instruction{Op: op, OperandA: a, OperandB: b, OperandC: c})
	// Append a zero source position if we're tracking positions.
	// Callers that have position info should use EmitWithPos instead.
	if bf.SourcePositions != nil {
		bf.SourcePositions = append(bf.SourcePositions, SourcePos{})
	}
}

// EmitWithPos emits an instruction with explicit source position.
func (bf *BytecodeFunction) EmitWithPos(op Opcode, a, b, c uint8, line, col int) {
	bf.Instructions = append(bf.Instructions, Instruction{Op: op, OperandA: a, OperandB: b, OperandC: c})
	if bf.SourcePositions == nil {
		bf.SourcePositions = make([]SourcePos, 0, len(bf.Instructions)+64)
		// Backfill positions for previously emitted instructions.
		for i := 0; i < len(bf.Instructions)-1; i++ {
			bf.SourcePositions = append(bf.SourcePositions, SourcePos{})
		}
	}
	bf.SourcePositions = append(bf.SourcePositions, SourcePos{Line: line, Col: col})
}

// BuildConstantNames pre-computes string versions of all constants
// to avoid allocations during global variable lookup.
func (bf *BytecodeFunction) BuildConstantNames() {
	bf.ConstantNames = make([]string, len(bf.Constants))
	for i, c := range bf.Constants {
		bf.ConstantNames[i] = c.ToString()
	}
}

// EnsureRegisters guarantees at least N virtual registers exist.
func (bf *BytecodeFunction) EnsureRegisters(n int) int {
	if n > bf.NumRegisters {
		bf.NumRegisters = n
	}
	return n
}

// --- Lazy Function Compilation ---

// LazyFunction defers bytecode compilation until the function is first called.
// Stores the AST + scope needed to compile on demand, avoiding upfront
// compilation cost for functions that are never executed.
type LazyFunction struct {
	Name       string
	Params     []DefaultParam
	Body       *BlockStatement
	ParentScope *Scope
	compiled   *BytecodeFunction
}

// Compile compiles the lazy function on first access, then caches the result.
func (lf *LazyFunction) Compile() *BytecodeFunction {
	if lf.compiled == nil {
		paramNames := make([]string, len(lf.Params))
		for i, p := range lf.Params {
			paramNames[i] = p.Name
		}
		lf.compiled = CompileFunctionWithParent(lf.Name, paramNames, lf.Params, lf.Body, lf.ParentScope)
	}
	return lf.compiled
}

// IsCompiled returns true if the function has already been compiled.
func (lf *LazyFunction) IsCompiled() bool {
	return lf.compiled != nil
}

// --- Type Feedback Loop (Hot Opcode Profiling) ---

// HotOpInfo tracks execution frequency and operand type hints for a single
// bytecode site. The VM updates Count at runtime; when Count exceeds a
// threshold, the compiler can regenerate the bytecode with type-specialized
// fast paths (e.g., OpAdd → numeric-only fast path, OpLdaKeyedProperty →
// dense-array direct indexing).
type HotOpInfo struct {
	Op       Opcode
	Count    uint64
	TypeHint TypeTag // dominant operand type (TagNumber, TagString, etc.)
}

// HotOpProfile collects type feedback across all monitored opcode sites in a
// function. Keyed by instruction offset within the bytecode array.
type HotOpProfile struct {
	Sites     map[int]*HotOpInfo // instruction offset → hot op data
	Threshold uint64             // recompile when Count > Threshold
}

// NewHotOpProfile creates a profiling structure for a function.
func NewHotOpProfile(threshold uint64) *HotOpProfile {
	return &HotOpProfile{
		Sites:     make(map[int]*HotOpInfo),
		Threshold: threshold,
	}
}

// RecordHit increments the execution count for an instruction at the given
// offset and optionally updates the type hint. Returns true if the site
// just crossed the hot threshold (trigger recompilation).
func (hp *HotOpProfile) RecordHit(offset int, op Opcode, typeTag TypeTag) bool {
	site, ok := hp.Sites[offset]
	if !ok {
		site = &HotOpInfo{Op: op}
		hp.Sites[offset] = site
	}
	site.Count++
	if typeTag != 0 {
		site.TypeHint = typeTag
	}
	return site.Count == hp.Threshold+1 // crossed threshold this hit
}

// HotSites returns all opcode sites that have exceeded the hot threshold.
func (hp *HotOpProfile) HotSites() []*HotOpInfo {
	var hot []*HotOpInfo
	for _, site := range hp.Sites {
		if site.Count > hp.Threshold {
			hot = append(hot, site)
		}
	}
	return hot
}
