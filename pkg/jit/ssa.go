// Package jit implements TurboFan-style Tier 2 JIT compilation for the GoV8 VM.
// This file defines the Static Single Assignment (SSA) intermediate representation
// used as the compilation IR between bytecode and native code generation.
package jit

import "github.com/lucasdss/v8go/pkg/js"

// SSAOp represents an SSA node operation kind.
type SSAOp int

// SSA operation codes.
const (
	SSAConst                 SSAOp = iota // constant value
	SSAAdd                                // numeric addition
	SSASub                                // numeric subtraction
	SSAMul                                // numeric multiplication
	SSADiv                                // numeric division
	SSALoad                               // load from object property
	SSAStore                              // store to object property
	SSACall                               // function call
	SSAReturn                             // function return
	SSAPhi                                // phi node (control-flow merge)
	SSACmp                                // comparison
	SSANeg                                // numeric negation
	SSANot                                // boolean/logical not
	SSAToNumber                           // type conversion to number
	SSABranch                             // conditional/unconditional branch
	SSAAnd                                // bitwise AND
	SSAOr                                 // bitwise OR
	SSAXor                                // bitwise XOR
	SSAShl                                // left shift
	SSAShr                                // logical right shift
	SSASar                                // arithmetic right shift
	SSAFSub                               // float64 subtraction
	SSAFMul                               // float64 multiplication
	SSAFDiv                               // float64 division
	SSAFCmp                               // float64 comparison
	SSAEq                                 // integer equality comparison
	SSANe                                 // integer not-equal comparison
	SSALt                                 // signed less than
	SSALe                                 // signed less or equal
	SSAGt                                 // signed greater than
	SSAGe                                 // signed greater or equal
	SSAZero                               // zero constant value
	SSACastIntToFloat                     // int32 → float64 conversion
	SSACastFloatToInt                     // float64 → int32 conversion
	SSAIsNumber                           // type guard: check if value is a number
	SSACondMove                           // conditional move (CSEL)
	SSADeopt                              // explicit deoptimization point
	SSACheckBounds                        // array bounds check with deopt
	SSANew                                // new object construction (runtime call)
	SSADebugBreak                         // debug breakpoint trap
	SSAMod                                // integer modulo (SDIV + MSUB)
	SSAFMod                               // float64 modulo (runtime call placeholder)
	SSAMin                                // signed minimum (CSEL)
	SSAMax                                // signed maximum (CSEL)
	SSAAbs                                // absolute value (CMP + CSEL negate)
	SSAClz                                // count leading zeros (CLZ)
	SSACtz                                // count trailing zeros (RBIT + CLZ)
	SSARev                                // byte reverse (REV)
	SSAExtractBits                        // unsigned bitfield extract (UBFX)
	SSASignExtend8                        // sign-extend byte to 64-bit (SXTB)
	SSASignExtend16                       // sign-extend halfword to 64-bit (SXTH)
	SSASignExtend32                       // sign-extend word to 64-bit (SXTW)
	SSAMovFPToInt                         // move float64 register bits to int register (FMOV)
	SSAMovIntToFP                         // move int register bits to float64 register (FMOV_XD)
	SSALoadGlobal                         // load global variable by name/index
	SSAStoreGlobal                        // store to global variable by name/index
	SSAIsObject                           // type guard: check if value is an object
	SSAIsString                           // type guard: check if value is a string
	SSATruncateToInt32                    // truncate float64 to int32
	SSAFloat64ToInt32                     // float64 to int32 with rounding (FCVTZS)
	SSAInt32ToFloat64                     // int32 to float64 (SCVTF)
	SSANaNCheck                           // check if float64 is NaN (FCMP + BVS)
	SSANegZeroCheck                       // check for negative zero (-0.0)
	SSAThrow                              // throw JS exception (runtime call)
	SSANewArray                           // allocate new JS array
	SSALoadElement                        // load from array element by index
	SSAStoreElement                       // store to array element by index
	SSAStackCheck                         // stack overflow guard (CMP SP, limit)
	SSAMoveConst                          // move small 16-bit constant (MOVZ, lighter than full SSAConst)
	SSAInterruptCheck                     // preemption/interrupt check point
	SSALazyDeoptContinuation              // lazy deoptimization continuation point
	SSAIsUndefined                        // type guard: check if value is undefined (tag=0)
	SSAIsNullOrUndefined                  // type guard: check if null or undefined
	SSAIsBoolean                          // type guard: check if value is boolean (tag=3)
	SSAIsInt32                            // type guard: check if value is tagged int32
	SSAObjectLiteral                      // create empty object literal (runtime call)
	SSASetProperty                        // set named property on object (IC-patched)
	SSAGetProperty                        // get named property from object (IC-patched)
	SSAForInStart                         // start for-in enumeration
	SSACreateClosure                      // create closure (function + captured context)
	SSATypeof                             // typeof operator evaluation
	SSADeleteProperty                     // delete named property from object
)

// String returns a human-readable name for the SSA operation.
func (op SSAOp) String() string {
	switch op {
	case SSAConst:
		return "Const"
	case SSAAdd:
		return "Add"
	case SSASub:
		return "Sub"
	case SSAMul:
		return "Mul"
	case SSADiv:
		return "Div"
	case SSALoad:
		return "Load"
	case SSAStore:
		return "Store"
	case SSACall:
		return "Call"
	case SSAReturn:
		return "Return"
	case SSAPhi:
		return "Phi"
	case SSACmp:
		return "Cmp"
	case SSANeg:
		return "Neg"
	case SSANot:
		return "Not"
	case SSAToNumber:
		return "ToNumber"
	case SSABranch:
		return "Branch"
	case SSAAnd:
		return "And"
	case SSAOr:
		return "Or"
	case SSAXor:
		return "Xor"
	case SSAShl:
		return "Shl"
	case SSAShr:
		return "Shr"
	case SSASar:
		return "Sar"
	case SSAFSub:
		return "FSub"
	case SSAFMul:
		return "FMul"
	case SSAFDiv:
		return "FDiv"
	case SSAFCmp:
		return "FCmp"
	case SSAEq:
		return "Eq"
	case SSANe:
		return "Ne"
	case SSALt:
		return "Lt"
	case SSALe:
		return "Le"
	case SSAGt:
		return "Gt"
	case SSAGe:
		return "Ge"
	case SSAZero:
		return "Zero"
	case SSACastIntToFloat:
		return "CastIntToFloat"
	case SSACastFloatToInt:
		return "CastFloatToInt"
	case SSAIsNumber:
		return "IsNumber"
	case SSACondMove:
		return "CondMove"
	case SSADeopt:
		return "Deopt"
	case SSACheckBounds:
		return "CheckBounds"
	case SSANew:
		return "New"
	case SSADebugBreak:
		return "DebugBreak"
	case SSAMod:
		return "Mod"
	case SSAFMod:
		return "FMod"
	case SSAMin:
		return "Min"
	case SSAMax:
		return "Max"
	case SSAAbs:
		return "Abs"
	case SSAClz:
		return "Clz"
	case SSACtz:
		return "Ctz"
	case SSARev:
		return "Rev"
	case SSAExtractBits:
		return "ExtractBits"
	case SSASignExtend8:
		return "SignExtend8"
	case SSASignExtend16:
		return "SignExtend16"
	case SSASignExtend32:
		return "SignExtend32"
	case SSAMovFPToInt:
		return "MovFPToInt"
	case SSAMovIntToFP:
		return "MovIntToFP"
	case SSALoadGlobal:
		return "LoadGlobal"
	case SSAStoreGlobal:
		return "StoreGlobal"
	case SSAIsObject:
		return "IsObject"
	case SSAIsString:
		return "IsString"
	case SSATruncateToInt32:
		return "TruncateToInt32"
	case SSAFloat64ToInt32:
		return "Float64ToInt32"
	case SSAInt32ToFloat64:
		return "Int32ToFloat64"
	case SSANaNCheck:
		return "NaNCheck"
	case SSANegZeroCheck:
		return "NegZeroCheck"
	case SSAThrow:
		return "Throw"
	case SSANewArray:
		return "NewArray"
	case SSALoadElement:
		return "LoadElement"
	case SSAStoreElement:
		return "StoreElement"
	case SSAStackCheck:
		return "StackCheck"
	case SSAMoveConst:
		return "MoveConst"
	case SSAInterruptCheck:
		return "InterruptCheck"
	case SSALazyDeoptContinuation:
		return "LazyDeoptContinuation"
	case SSAIsUndefined:
		return "IsUndefined"
	case SSAIsNullOrUndefined:
		return "IsNullOrUndefined"
	case SSAIsBoolean:
		return "IsBoolean"
	case SSAIsInt32:
		return "IsInt32"
	case SSAObjectLiteral:
		return "ObjectLiteral"
	case SSASetProperty:
		return "SetProperty"
	case SSAGetProperty:
		return "GetProperty"
	case SSAForInStart:
		return "ForInStart"
	case SSACreateClosure:
		return "CreateClosure"
	case SSATypeof:
		return "Typeof"
	case SSADeleteProperty:
		return "DeleteProperty"
	default:
		return "Unknown"
	}
}

// SSAType represents the known or inferred type of an SSA value.
type SSAType int

// SSA type kinds.
const (
	SSAAny     SSAType = iota // unknown or dynamic type
	SSAInt32                  // 32-bit signed integer
	SSAFloat64                // 64-bit IEEE 754 float
	SSAString                 // JavaScript string
	SSAObject                 // JavaScript object (includes arrays, functions)
)

// String returns a human-readable name for the SSA type.
func (t SSAType) String() string {
	switch t {
	case SSAAny:
		return "Any"
	case SSAInt32:
		return "Int32"
	case SSAFloat64:
		return "Float64"
	case SSAString:
		return "String"
	case SSAObject:
		return "Object"
	default:
		return "Unknown"
	}
}

// SSANode is a single node in the SSA graph. Each node produces exactly one value.
type SSANode struct {
	ID       int        // unique identifier within the graph
	Op       SSAOp      // operation kind
	Type     SSAType    // known or inferred type
	Args     []*SSANode // input operands (use-def edges in SSA)
	Users    []*SSANode // reverse edges: all nodes that consume this value
	Value    js.JSValue // constant value (valid only for SSAConst)
	BCPC     int        // originating bytecode PC for debug/trace
	Feedback *js.ICSlot // feedback slot for speculative optimizations (nil when not available)
	Labels   []*Label   // branch target labels (SSABranch only)
}

// SSABasicBlock represents a single-entry, single-exit straight-line sequence of SSANodes.
type SSABasicBlock struct {
	ID           int              // unique block identifier
	Nodes        []*SSANode       // SSA instructions in this block (program order)
	Predecessors []*SSABasicBlock // control-flow predecessors
	Successors   []*SSABasicBlock // control-flow successors
	StartPC      int              // bytecode PC of the first instruction in this block
	EndPC        int              // bytecode PC of the last instruction in this block
}

// SSAGraph is the top-level container for an SSA-form function body.
type SSAGraph struct {
	Blocks []*SSABasicBlock // all basic blocks in reverse postorder (block 0 is entry)
	Entry  *SSABasicBlock   // the entry block (always Blocks[0])
	Params []*SSANode       // function parameters (SSAConst nodes at function entry)
	nextID int              // monotonically increasing node ID counter
}

// NewSSAGraph creates an empty SSA graph ready for building.
func NewSSAGraph() *SSAGraph {
	return &SSAGraph{nextID: 1}
}

// newNode allocates a new SSANode with the given operation, type, and input operands.
// User lists on argument nodes are automatically updated to include the new node.
func (g *SSAGraph) newNode(op SSAOp, typ SSAType, args ...*SSANode) *SSANode {
	n := &SSANode{ID: g.nextID, Op: op, Type: typ, Args: args}
	g.nextID++
	for _, a := range args {
		if a != nil {
			a.Users = append(a.Users, n)
		}
	}
	return n
}
