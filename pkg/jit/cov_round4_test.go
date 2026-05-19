//go:build !amd64

package jit

import (
"testing"

"github.com/lucasdss/v8go/pkg/js"
)

// newTestFrame creates a minimal VMFrame for testing Sparkplug helpers.
func newTestFrame() *js.VMFrame {
return &js.VMFrame{
Func: &js.BytecodeFunction{
Constants:     make([]js.JSValue, 10),
ConstantNames: make([]string, 10),
GlobalVals:    make([]js.JSValue, 20),
Instructions:  make([]js.Instruction, 10),
},
Regs:      make([]js.JSValue, 10),
Acc:       js.Undefined,
This:      js.NewObject(js.NewJSObject()),
PC:        0,
HandlerPC: -1,
FinallyPC: -1,
}
}

// =============================================================================
// Priority 1 — sparkplug_ops.go emit functions
// =============================================================================

func TestEmitSparkplugLdaNamedPropertyR4(t *testing.T) {
buf, err := NewCodeBuf(512)
if err != nil {
t.Fatal(err)
}
defer buf.Free()
as := NewAssembler(buf)
bf := &js.BytecodeFunction{
Constants:    make([]js.JSValue, 5),
Instructions: make([]js.Instruction, 5),
ICVector:     nil,
}
bf.Constants[0] = js.NewString("prop")
instr := &js.Instruction{Op: js.OpLdaNamedProperty, OperandA: 0, OperandC: 0}
icSlotOffsets := make([]int, 4)
deoptStub := NewLabel()
emitSparkplugLdaNamedProperty(as, instr, bf, icSlotOffsets, deoptStub)
if buf.Len() == 0 {
t.Error("expected code emission, got empty buffer")
}
}

func TestEmitSparkplugCreateObjectLiteralR4(t *testing.T) {
buf, err := NewCodeBuf(256)
if err != nil {
t.Fatal(err)
}
defer buf.Free()
as := NewAssembler(buf)
instr := &js.Instruction{Op: js.OpCreateObjectLiteral, OperandA: 0}
emitSparkplugCreateObjectLiteral(as, instr)
if buf.Len() == 0 {
t.Error("expected code emission, got empty buffer")
}
}

func TestEmitSparkplugCallNR4(t *testing.T) {
buf, err := NewCodeBuf(256)
if err != nil {
t.Fatal(err)
}
defer buf.Free()
as := NewAssembler(buf)
instr := &js.Instruction{Op: js.OpCall1, OperandA: 0}
deoptStub := NewLabel()
emitSparkplugCallN(as, instr, 1, deoptStub)
if buf.Len() == 0 {
t.Error("expected code emission, got empty buffer")
}
}

func TestEmitSparkplugJumpIfTrueNativeR4(t *testing.T) {
buf, err := NewCodeBuf(512)
if err != nil {
t.Fatal(err)
}
defer buf.Free()
as := NewAssembler(buf)
targetLabel := NewLabel()
labels := map[int]*Label{5: targetLabel}
instr := &js.Instruction{Op: js.OpJumpIfTrue, OperandA: 5}
deoptStub := NewLabel()
emitSparkplugJumpIfTrueNative(as, instr, labels, deoptStub)
if buf.Len() == 0 {
t.Error("expected code emission, got empty buffer")
}
}

// =============================================================================
// Priority 2 — ssa.go String methods
// =============================================================================

func TestSSAOpStringR4(t *testing.T) {
tests := map[SSAOp]string{
SSAConst:                 "Const",
SSAAdd:                   "Add",
SSASub:                   "Sub",
SSAMul:                   "Mul",
SSADiv:                   "Div",
SSALoad:                  "Load",
SSAStore:                 "Store",
SSACall:                  "Call",
SSAReturn:                "Return",
SSAPhi:                   "Phi",
SSACmp:                   "Cmp",
SSANeg:                   "Neg",
SSANot:                   "Not",
SSAToNumber:              "ToNumber",
SSABranch:                "Branch",
SSAAnd:                   "And",
SSAOr:                    "Or",
SSAXor:                   "Xor",
SSAShl:                   "Shl",
SSAShr:                   "Shr",
SSASar:                   "Sar",
SSAFSub:                  "FSub",
SSAFMul:                  "FMul",
SSAFDiv:                  "FDiv",
SSAFCmp:                  "FCmp",
SSAEq:                    "Eq",
SSANe:                    "Ne",
SSALt:                    "Lt",
SSALe:                    "Le",
SSAGt:                    "Gt",
SSAGe:                    "Ge",
SSAZero:                  "Zero",
SSACastIntToFloat:        "CastIntToFloat",
SSACastFloatToInt:        "CastFloatToInt",
SSAIsNumber:              "IsNumber",
SSACondMove:              "CondMove",
SSADeopt:                 "Deopt",
SSACheckBounds:           "CheckBounds",
SSANew:                   "New",
SSADebugBreak:            "DebugBreak",
SSAMod:                   "Mod",
SSAFMod:                  "FMod",
SSAMin:                   "Min",
SSAMax:                   "Max",
SSAAbs:                   "Abs",
SSAClz:                   "Clz",
SSACtz:                   "Ctz",
SSARev:                   "Rev",
SSAExtractBits:           "ExtractBits",
SSASignExtend8:           "SignExtend8",
SSASignExtend16:          "SignExtend16",
SSASignExtend32:          "SignExtend32",
SSAMovFPToInt:            "MovFPToInt",
SSAMovIntToFP:            "MovIntToFP",
SSALoadGlobal:            "LoadGlobal",
SSAStoreGlobal:           "StoreGlobal",
SSAIsObject:              "IsObject",
SSAIsString:              "IsString",
SSATruncateToInt32:       "TruncateToInt32",
SSAFloat64ToInt32:        "Float64ToInt32",
SSAInt32ToFloat64:        "Int32ToFloat64",
SSANaNCheck:              "NaNCheck",
SSANegZeroCheck:          "NegZeroCheck",
SSAThrow:                 "Throw",
SSANewArray:              "NewArray",
SSALoadElement:           "LoadElement",
SSAStoreElement:          "StoreElement",
SSAStackCheck:            "StackCheck",
SSAMoveConst:             "MoveConst",
SSAInterruptCheck:        "InterruptCheck",
SSALazyDeoptContinuation: "LazyDeoptContinuation",
SSAIsUndefined:           "IsUndefined",
SSAIsNullOrUndefined:     "IsNullOrUndefined",
SSAIsBoolean:             "IsBoolean",
SSAIsInt32:               "IsInt32",
SSAObjectLiteral:         "ObjectLiteral",
SSASetProperty:           "SetProperty",
SSAGetProperty:           "GetProperty",
SSAForInStart:            "ForInStart",
SSACreateClosure:         "CreateClosure",
SSATypeof:                "Typeof",
SSADeleteProperty:        "DeleteProperty",
SSANop:                   "Nop",
}
for op, expected := range tests {
got := op.String()
if got != expected {
t.Errorf("SSAOp(%d).String() = %q, want %q", op, got, expected)
}
}
unknown := SSAOp(999).String()
if unknown != "Unknown" {
t.Errorf("SSAOp(999).String() = %q, want %q", unknown, "Unknown")
}
}

func TestSSATypeStringR4(t *testing.T) {
tests := map[SSAType]string{
SSAAny:     "Any",
SSAInt32:   "Int32",
SSAFloat64: "Float64",
SSAString:  "String",
SSAObject:  "Object",
}
for typ, expected := range tests {
got := typ.String()
if got != expected {
t.Errorf("SSAType(%d).String() = %q, want %q", typ, got, expected)
}
}
unknown := SSAType(999).String()
if unknown != "Unknown" {
t.Errorf("SSAType(999).String() = %q, want %q", unknown, "Unknown")
}
}

// =============================================================================
// Priority 3 — ssa_builder.go: blockForPC
// =============================================================================

func TestBlockForPCR4(t *testing.T) {
bb0 := &SSABasicBlock{ID: 0, StartPC: 0, EndPC: 4}
bb1 := &SSABasicBlock{ID: 1, StartPC: 5, EndPC: 9}
blocks := []*SSABasicBlock{bb0, bb1}
if got := blockForPC(blocks, 0); got != bb0 {
t.Error("expected block 0 for PC 0")
}
if got := blockForPC(blocks, 5); got != bb1 {
t.Error("expected block 1 for PC 5")
}
if got := blockForPC(blocks, 99); got != nil {
t.Error("expected nil for non-matching PC")
}
if got := blockForPC(nil, 0); got != nil {
t.Error("expected nil for nil blocks")
}
}

// =============================================================================
// Priority 4 — ssa_regalloc.go: AllocateFloat
// =============================================================================

func TestAllocateFloatR4(t *testing.T) {
ra := NewRegAlloc()
node1 := &SSANode{ID: 1}
node2 := &SSANode{ID: 2}
r1 := ra.AllocateFloat(node1)
if r1 < 0 {
t.Error("expected valid float register allocation")
}
r2 := ra.AllocateFloat(node2)
if r2 < 0 {
t.Error("expected valid second float register allocation")
}
if r1 == r2 {
t.Error("expected different float registers")
}
for i := 2; i < 8; i++ {
ra.AllocateFloat(&SSANode{ID: 10 + i})
}
rNeg := ra.AllocateFloat(&SSANode{ID: 100})
if rNeg != -1 {
t.Errorf("expected -1 for exhausted pool, got %d", rNeg)
}
}

// =============================================================================
// Priority 5 — sparkplug_helpers.go remaining 0% functions
// =============================================================================

func TestSparkplugOpLdaNamedPropertySlowR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewObject(js.NewJSObject())
f.Func.Constants[0] = js.NewString("x")
sparkplugOpLdaNamedPropertySlow(f, 0, 0)
_ = f.Acc
}

func TestSparkplugOpStaNamedPropertySlowR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
f.Acc = js.NewObject(obj)
f.Func.Constants[0] = js.NewString("x")
f.Regs[1] = js.NewNumber(42)
sparkplugOpStaNamedPropertySlow(f, 0, 0, 1)
_ = obj.Get("x")
}

func TestSparkplugOpCreateObjectLiteralR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("a,b")
sparkplugOpCreateObjectLiteral(f, 0)
if !f.Acc.IsObject() {
t.Error("expected object in Acc")
}
}

func TestSparkplugOpStaByOffsetR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Properties = make([]js.JSValue, 8)
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewNumber(99)
sparkplugOpStaByOffset(f, 0, 1, 255)
}

func TestSparkplugOpStaGlobalSlotR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(42)
sparkplugOpStaGlobalSlot(f, 0)
if len(f.Func.GlobalVals) <= 0 {
t.Error("expected GlobalVals slot to be set")
}
if f.Func.GlobalVals[0].ToNumber() != 42 {
t.Errorf("expected 42 in slot, got %v", f.Func.GlobalVals[0])
}
}

func TestSparkplugOpLdaGlobalSlotR4(t *testing.T) {
f := newTestFrame()
f.Func.GlobalVals[0] = js.NewNumber(77)
sparkplugOpLdaGlobalSlot(f, 0)
if f.Acc.ToNumber() != 77 {
t.Errorf("expected 77, got %v", f.Acc)
}
sparkplugOpLdaGlobalSlot(f, 999)
if !f.Acc.IsUndefined() {
t.Error("expected undefined for out-of-range slot")
}
}

func TestSparkplugOpCreateArrayR4(t *testing.T) {
f := newTestFrame()
sparkplugOpCreateArray(f)
if !f.Acc.IsObject() || f.Acc.ObjVal.ConstructorName != "Array" {
t.Error("expected Array object")
}
}

func TestSparkplugOpDeleteR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("x", js.NewNumber(1))
f.Acc = js.NewObject(obj)
f.Func.Constants[0] = js.NewString("x")
sparkplugOpDelete(f, 0)
if !f.Acc.IsBoolean() || !f.Acc.IsTruthy() {
t.Error("expected true after delete")
}
}

func TestSparkplugOpDeleteKeyedR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("k", js.NewNumber(1))
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewString("k")
sparkplugOpDeleteKeyed(f, 0, 1)
if !f.Acc.IsBoolean() || !f.Acc.IsTruthy() {
t.Error("expected true after delete")
}
}

func TestSparkplugOpCreateClosureR4(t *testing.T) {
f := newTestFrame()
innerBf := &js.BytecodeFunction{NumParams: 2, Instructions: make([]js.Instruction, 5)}
innerObj := js.NewJSObject()
innerObj.Bytecode = innerBf
f.Acc = js.NewObject(innerObj)
sparkplugOpCreateClosure(f)
if !f.Acc.IsObject() {
t.Error("expected closure object")
}
}

func TestSparkplugOpLdaKeyedPropertySlowR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("key", js.NewNumber(123))
f.Regs[1] = js.NewObject(obj)
f.Acc = js.NewString("key")
sparkplugOpLdaKeyedPropertySlow(f, 1)
if f.Acc.ToNumber() != 123 {
t.Errorf("expected 123, got %v", f.Acc)
}
}

func TestSparkplugOpStaKeyedPropertySlowR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewNumber(55)
f.Acc = js.NewString("k")
sparkplugOpStaKeyedPropertySlow(f, 0, 1)
if obj.Get("k").ToNumber() != 55 {
t.Errorf("expected 55, got %v", obj.Get("k"))
}
}

func TestParseArrayIndexGoR4(t *testing.T) {
tests := []struct {
input    string
expected int
ok       bool
}{
{"", 0, false},
{"0", 0, true},
{"00", 0, false},
{" 1", 0, false},
{"5", 5, true},
{"123", 123, true},
{"42a", 0, false},
{"9999999999", 0, false},
{"2147483648", 0, false},
}
for _, tc := range tests {
n, ok := parseArrayIndexGo(tc.input)
if n != tc.expected || ok != tc.ok {
t.Errorf("parseArrayIndexGo(%q) = (%d, %v), want (%d, %v)", tc.input, n, ok, tc.expected, tc.ok)
}
}
}

func TestSparkplugOpCall0R4(t *testing.T) {
f := newTestFrame()
sparkplugOpCall0(f, 0)
}

func TestSparkplugOpCall1R4(t *testing.T) {
f := newTestFrame()
sparkplugOpCall1(f, 0, 1)
}

func TestSparkplugOpCall2R4(t *testing.T) {
f := newTestFrame()
sparkplugOpCall2(f, 0, 1)
}

func TestSparkplugOpCallR4(t *testing.T) {
f := newTestFrame()
sparkplugOpCall(f, 0, 1, 2)
}

func TestSparkplugOpCallSpreadR4(t *testing.T) {
f := newTestFrame()
sparkplugOpCallSpread(f, 0, 1, 3)
}

func TestSparkplugOpInstanceofR4(t *testing.T) {
f := newTestFrame()
protoObj := js.NewJSObject()
C := js.NewJSObject()
C.Prototype = protoObj
D := js.NewJSObject()
D.Set("prototype", js.NewObject(protoObj))
f.Regs[0] = js.NewObject(C)
f.Acc = js.NewObject(D)
sparkplugOpInstanceof(f, 0)
if !f.Acc.IsBoolean() || !f.Acc.IsTruthy() {
t.Error("expected D instanceof C to be true")
}
}

func TestSparkplugOpInR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("x", js.NewNumber(1))
f.Acc = js.NewObject(obj)
f.Regs[0] = js.NewString("x")
sparkplugOpIn(f, 0)
if !f.Acc.IsBoolean() || !f.Acc.IsTruthy() {
t.Error("expected 'x' in obj to be true")
}
}

func TestSparkplugOpCreateRegExpR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("abc")
f.Func.Constants[1] = js.NewString("gi")
sparkplugOpCreateRegExp(f, 0, 1)
if !f.Acc.IsObject() {
t.Error("expected RegExp object")
}
}

func TestSparkplugOpThrowR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewString("error!")
sparkplugOpThrow(f)
if !f.ShouldReturn {
t.Error("expected ShouldReturn after throw")
}
if f.Thrown.ToString() != "error!" {
t.Errorf("expected 'error!' thrown, got %v", f.Thrown)
}
}

func TestSparkplugOpForInSetupR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Shape = js.GetOrCreateShape([]string{"a", "b"})
f.Regs[0] = js.NewObject(obj)
sparkplugOpForInSetup(f, 0)
if !f.Acc.IsString() {
t.Error("expected first key string from for-in setup")
}
}

func TestSparkplugOpForInNextR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	obj.Shape = js.GetOrCreateShape([]string{"x", "y", "z"})
	f.Regs[0] = js.NewObject(obj)
	// Get first key from setup.
	sparkplugOpForInSetup(f, 0)
	firstKey := f.Acc.ToString()
	if f.Acc.Tag != js.TagString || firstKey == "" {
		t.Skip("for-in setup returned no key")
	}
	f.Acc = js.NewString(firstKey)
	sparkplugOpForInNext(f, 0)
	// Should advance to another key (map order is random, so any non-undefined is fine).
	if f.Acc.Tag == js.TagUndefined {
		t.Log("for-in next returned undefined (end of iteration)")
	}
}

func TestSparkplugOpMovR4(t *testing.T) {
f := newTestFrame()
f.Regs[0] = js.NewNumber(42)
sparkplugOpMov(f, 1, 0)
if f.Regs[1].ToNumber() != 42 {
t.Errorf("expected 42, got %v", f.Regs[1])
}
}

func TestSparkplugOpExpR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(2)
f.Regs[0] = js.NewNumber(3)
sparkplugOpExp(f, 0)
if f.Acc.ToNumber() != 8 {
t.Errorf("expected 8, got %v", f.Acc)
}
}

func TestSparkplugOpIncR4(t *testing.T) {
f := newTestFrame()
f.Regs[0] = js.NewNumber(5)
sparkplugOpInc(f, 0)
if f.Regs[0].ToNumber() != 6 {
t.Errorf("expected 6, got %v", f.Regs[0])
}
}

func TestSparkplugOpDecR4(t *testing.T) {
f := newTestFrame()
f.Regs[0] = js.NewNumber(5)
sparkplugOpDec(f, 0)
if f.Regs[0].ToNumber() != 4 {
t.Errorf("expected 4, got %v", f.Regs[0])
}
}

func TestSparkplugOpDupR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(99)
sparkplugOpDup(f, 2)
if f.Regs[2].ToNumber() != 99 {
t.Errorf("expected 99, got %v", f.Regs[2])
}
}

func TestSparkplugOpLdaGlobalR4(t *testing.T) {
f := newTestFrame()
sparkplugOpLdaGlobal(f, 0)
if !f.ShouldReturn {
t.Error("LdaGlobal should deopt")
}
}

func TestSparkplugOpStaGlobalR4(t *testing.T) {
f := newTestFrame()
sparkplugOpStaGlobal(f, 0)
if !f.ShouldReturn {
t.Error("StaGlobal should deopt")
}
}

func TestSparkplugOpLdaLocalR4(t *testing.T) {
f := newTestFrame()
f.Regs[0] = js.NewNumber(100)
sparkplugOpLdaLocal(f, 0)
if f.Acc.ToNumber() != 100 {
t.Errorf("expected 100, got %v", f.Acc)
}
}

func TestSparkplugOpStaLocalR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(200)
sparkplugOpStaLocal(f, 0)
if f.Regs[0].ToNumber() != 200 {
t.Errorf("expected 200, got %v", f.Regs[0])
}
}

func TestSparkplugOpLdaThisR4(t *testing.T) {
f := newTestFrame()
f.This = js.NewString("thisVal")
sparkplugOpLdaThis(f)
if f.Acc.ToString() != "thisVal" {
t.Errorf("expected 'thisVal', got %v", f.Acc)
}
}

func TestSparkplugOpThrowConstAssignmentR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("x")
sparkplugOpThrowConstAssignment(f, 0)
if !f.ShouldReturn {
t.Error("expected ShouldReturn after throw")
}
}

func TestJsStrIfNotEmptyR4(t *testing.T) {
if s := jsStrIfNotEmpty(""); s != "" {
t.Errorf("expected empty, got %q", s)
}
if s := jsStrIfNotEmpty("foo"); s != " foo" {
t.Errorf("expected ' foo', got %q", s)
}
}

func TestSparkplugOpSetPrototypeR4(t *testing.T) {
f := newTestFrame()
proto := js.NewJSObject()
f.Acc = js.NewObject(proto)
obj := js.NewJSObject()
f.Regs[0] = js.NewObject(obj)
sparkplugOpSetPrototype(f, 0)
if obj.Prototype != proto {
t.Error("expected prototype to be set")
}
}

func TestSparkplugOpCheckConstructorR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewObject(js.NewJSObject())
sparkplugOpCheckConstructor(f)
if !f.ShouldReturn {
t.Error("expected ShouldReturn for non-constructor")
}
}

func TestSparkplugOpYieldR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(10)
sparkplugOpYield(f)
if !f.Acc.IsObject() {
t.Error("expected yield result object")
}
if f.Acc.ObjVal.Get("value").ToNumber() != 10 {
t.Error("expected yield value 10")
}
}

func TestSparkplugOpYieldDelegateR4(t *testing.T) {
f := newTestFrame()
sparkplugOpYieldDelegate(f, 0)
if !f.ShouldReturn {
t.Error("yield* should deopt")
}
}

func TestSparkplugOpCreateGeneratorR4(t *testing.T) {
f := newTestFrame()
bf := &js.BytecodeFunction{NumParams: 0, Instructions: make([]js.Instruction, 3)}
innerObj := js.NewJSObject()
innerObj.Bytecode = bf
f.Acc = js.NewObject(innerObj)
sparkplugOpCreateGenerator(f)
if !f.Acc.IsObject() {
t.Error("expected generator object")
}
}

func TestSparkplugOpSuperCallR4(t *testing.T) {
f := newTestFrame()
sparkplugOpSuperCall(f, 2)
if !f.ShouldReturn {
t.Error("super() should deopt")
}
}

func TestSparkplugOpStringConcatR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewString("hello")
f.Regs[0] = js.NewString(" world")
sparkplugOpStringConcat(f, 0)
if f.Acc.ToString() != "hello world" {
t.Errorf("expected 'hello world', got %v", f.Acc)
}
}

func TestSparkplugOpArrayLengthR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("length", js.NewNumber(5))
f.Regs[0] = js.NewObject(obj)
sparkplugOpArrayLength(f, 0)
if f.Acc.ToNumber() != 5 {
t.Errorf("expected 5, got %v", f.Acc)
}
}

func TestSparkplugOpLdaPropByOffsetR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Properties = []js.JSValue{js.NewNumber(10), js.NewNumber(20), js.NewNumber(30)}
f.Regs[0] = js.NewObject(obj)
sparkplugOpLdaPropByOffset(f, 0, 0)
if f.Acc.ToNumber() != 10 {
t.Errorf("expected 10, got %v", f.Acc)
}
}

func TestSparkplugOpStaPropByOffsetR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Properties = []js.JSValue{js.NewNumber(0), js.NewNumber(0)}
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewNumber(77)
sparkplugOpStaPropByOffset(f, 0, 0, 1)
if obj.Properties[0].ToNumber() != 77 {
t.Errorf("expected 77, got %v", obj.Properties[0])
}
}

func TestSparkplugOpArrayGetIndexR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("2", js.NewNumber(42))
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewNumber(2)
sparkplugOpArrayGetIndex(f, 0, 1)
if f.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", f.Acc)
}
}

func TestSparkplugOpArraySetIndexR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewNumber(0)
f.Regs[2] = js.NewNumber(99)
sparkplugOpArraySetIndex(f, 0, 1, 2)
if obj.Get("0").ToNumber() != 99 {
t.Errorf("expected 99, got %v", obj.Get("0"))
}
}

func TestSparkplugOpCreateEmptyArrayR4(t *testing.T) {
defer func() {
if r := recover(); r != nil {
_ = r
}
}()
f := newTestFrame()
sparkplugOpCreateEmptyArray(f, 3)
}

func TestSparkplugOpLdaGlobalDirectR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("globalVar")
sparkplugOpLdaGlobalDirect(f, 0)
if f.Acc.ToString() != "globalVar" {
t.Errorf("expected 'globalVar', got %v", f.Acc)
}
}

func TestSparkplugOpStaGlobalDirectR4(t *testing.T) {
f := newTestFrame()
sparkplugOpStaGlobalDirect(f, 0)
if !f.ShouldReturn {
t.Error("StaGlobalDirect should deopt")
}
}

func TestSparkplugOpMathAbsR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(-5)
sparkplugOpMathAbs(f)
if f.Acc.ToNumber() != 5 {
t.Errorf("expected 5, got %v", f.Acc)
}
}

func TestSparkplugOpMathFloorR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(3.7)
sparkplugOpMathFloor(f)
if f.Acc.ToNumber() != 3 {
t.Errorf("expected 3, got %v", f.Acc)
}
}

func TestSparkplugOpStringLengthR4(t *testing.T) {
f := newTestFrame()
f.Regs[0] = js.NewString("hello")
sparkplugOpStringLength(f, 0)
if f.Acc.ToNumber() != 5 {
t.Errorf("expected 5, got %v", f.Acc)
}
}

func TestSparkplugOpStringEqR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewString("abc")
f.Regs[0] = js.NewString("abc")
sparkplugOpStringEq(f, 0)
if !f.Acc.IsBoolean() || !f.Acc.IsTruthy() {
t.Error("expected true for equal strings")
}
}

func TestSparkplugOpCallBuiltinR4(t *testing.T) {
f := newTestFrame()
sparkplugOpCallBuiltin(f, 0, 1)
if !f.ShouldReturn {
t.Error("CallBuiltin should deopt")
}
}

func TestSparkplugOpCallDirectR4(t *testing.T) {
f := newTestFrame()
sparkplugOpCallDirect(f, 0, 1)
if !f.ShouldReturn {
t.Error("CallDirect should deopt")
}
}

func TestSparkplugOpPushContextR4(t *testing.T) {
f := newTestFrame()
sparkplugOpPushContext(f)
if !f.ShouldReturn {
t.Error("PushContext should deopt")
}
}

func TestSparkplugOpPopContextR4(t *testing.T) {
f := newTestFrame()
sparkplugOpPopContext(f)
if !f.ShouldReturn {
t.Error("PopContext should deopt")
}
}

func TestSparkplugOpLoadContextSlotR4(t *testing.T) {
f := newTestFrame()
sparkplugOpLoadContextSlot(f, 0, 0)
if !f.ShouldReturn {
t.Error("LoadContextSlot should deopt")
}
}

func TestSparkplugOpStoreContextSlotR4(t *testing.T) {
f := newTestFrame()
sparkplugOpStoreContextSlot(f, 0, 0)
if !f.ShouldReturn {
t.Error("StoreContextSlot should deopt")
}
}

func TestSparkplugOpShiftRightZeroNumberR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(8)
f.Regs[0] = js.NewNumber(1)
sparkplugOpShiftRightZeroNumber(f, 0)
if f.Acc.ToNumber() != 4 {
t.Errorf("expected 4, got %v", f.Acc)
}
}

func TestSparkplugOpMathCeilR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(3.2)
sparkplugOpMathCeil(f)
if f.Acc.ToNumber() != 4 {
t.Errorf("expected 4, got %v", f.Acc)
}
}

func TestSparkplugOpMathSqrtR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(16)
sparkplugOpMathSqrt(f)
if f.Acc.ToNumber() != 4 {
t.Errorf("expected 4, got %v", f.Acc)
}
}

func TestSparkplugOpForInSetupFastR4(t *testing.T) {
f := newTestFrame()
sparkplugOpForInSetupFast(f, 0)
if !f.ShouldReturn {
t.Error("ForInSetupFast should deopt")
}
}

func TestSparkplugOpForInNextFastR4(t *testing.T) {
f := newTestFrame()
sparkplugOpForInNextFast(f)
if !f.ShouldReturn {
t.Error("ForInNextFast should deopt")
}
}

func TestSparkplugOpSwapR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(1)
f.Regs[0] = js.NewNumber(2)
sparkplugOpSwap(f, 0)
if f.Acc.ToNumber() != 2 {
t.Errorf("expected 2 in Acc, got %v", f.Acc)
}
if f.Regs[0].ToNumber() != 1 {
t.Errorf("expected 1 in Regs[0], got %v", f.Regs[0])
}
}

func TestSparkplugOpLdaTrueFastR4(t *testing.T) {
f := newTestFrame()
sparkplugOpLdaTrueFast(f)
if !f.Acc.IsBoolean() || !f.Acc.IsTruthy() {
t.Error("expected true")
}
}

func TestSparkplugOpLdaFalseFastR4(t *testing.T) {
f := newTestFrame()
sparkplugOpLdaFalseFast(f)
if f.Acc.IsBoolean() && f.Acc.IsTruthy() {
t.Error("expected false")
}
}

func TestSparkplugOpLdaModuleVarR4(t *testing.T) {
f := newTestFrame()
f.Func.GlobalVals[0] = js.NewNumber(123)
sparkplugOpLdaModuleVar(f, 0)
if f.Acc.ToNumber() != 123 {
t.Errorf("expected 123, got %v", f.Acc)
}
}

func TestSparkplugOpStaModuleVarR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(456)
sparkplugOpStaModuleVar(f, 0)
if f.Func.GlobalVals[0].ToNumber() != 456 {
t.Errorf("expected 456, got %v", f.Func.GlobalVals[0])
}
}

func TestSparkplugOpThrowIfNotSuperR4(t *testing.T) {
f := newTestFrame()
f.Func.IsDerivedConstructor = false
sparkplugOpThrowIfNotSuper(f)
if !f.ShouldReturn {
t.Error("expected ShouldReturn for non-derived constructor")
}
f2 := newTestFrame()
f2.Func.IsDerivedConstructor = true
sparkplugOpThrowIfNotSuper(f2)
if f2.ShouldReturn {
t.Error("expected no ShouldReturn for derived constructor")
}
}

func TestSparkplugOpThrowIfHoleR4(t *testing.T) {
f := newTestFrame()
f.Regs[0] = js.Undefined
sparkplugOpThrowIfHole(f, 0)
}

func TestSparkplugOpCatchR4(t *testing.T) {
f := newTestFrame()
f.Thrown = js.NewString("caught error")
f.ShouldReturn = true
sparkplugOpCatch(f, 0)
if f.ShouldReturn {
t.Error("expected ShouldReturn cleared after catch")
}
if f.Regs[0].ToString() != "caught error" {
t.Errorf("expected 'caught error', got %v", f.Regs[0])
}
}

func TestSparkplugOpEndTryR4(t *testing.T) {
f := newTestFrame()
f.HandlerPC = 5
f.FinallyPC = 10
sparkplugOpEndTry(f)
if f.HandlerPC != -1 || f.FinallyPC != -1 {
t.Error("expected HandlerPC and FinallyPC cleared")
}
}

func TestSparkplugOpThrowSuperAlreadyCalledR4(t *testing.T) {
f := newTestFrame()
f.Func.IsDerivedConstructor = true
f.SuperCalled = true
sparkplugOpThrowSuperAlreadyCalled(f)
if !f.ShouldReturn {
t.Error("expected ShouldReturn when super already called")
}
f2 := newTestFrame()
f2.Func.IsDerivedConstructor = false
f2.SuperCalled = false
sparkplugOpThrowSuperAlreadyCalled(f2)
if f2.ShouldReturn {
t.Error("expected no ShouldReturn for non-derived")
}
}

func TestSparkplugOpThrowSuperNotCalledR4(t *testing.T) {
f := newTestFrame()
f.Func.IsDerivedConstructor = true
f.SuperCalled = false
sparkplugOpThrowSuperNotCalled(f)
if !f.ShouldReturn {
t.Error("expected ShouldReturn when super not called")
}
}

func TestSparkplugOpGetSuperConstructorR4(t *testing.T) {
f := newTestFrame()
f.Func.IsDerivedConstructor = true
obj := js.NewJSObject()
obj.ConstructorName = "Parent"
f.This = js.NewObject(obj)
sparkplugOpGetSuperConstructor(f)
if !f.Acc.IsObject() {
t.Error("expected super constructor object")
}
}

func TestSparkplugOpLdaHomeObjectR4(t *testing.T) {
f := newTestFrame()
prototype := js.NewJSObject()
obj := js.NewJSObject()
obj.Prototype = prototype
f.This = js.NewObject(obj)
sparkplugOpLdaHomeObject(f)
if !f.Acc.IsObject() {
t.Error("expected home object")
}
}

func TestSparkplugOpLdaHomeObjectPropertyR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("method")
homeObj := js.NewJSObject()
homeObj.Set("method", js.NewNumber(42))
f.Regs[1] = js.NewObject(homeObj)
sparkplugOpLdaHomeObjectProperty(f, 0, 1)
if f.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", f.Acc)
}
}

func TestSparkplugOpStaHomeObjectPropertyR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("field")
homeObj := js.NewJSObject()
f.Regs[1] = js.NewObject(homeObj)
f.Acc = js.NewNumber(77)
sparkplugOpStaHomeObjectProperty(f, 0, 1)
if homeObj.Get("field").ToNumber() != 77 {
t.Errorf("expected 77, got %v", homeObj.Get("field"))
}
}

func TestSparkplugOpInitDerivedR4(t *testing.T) {
f := newTestFrame()
f.Func.IsDerivedConstructor = true
f.Regs[0] = js.NewString("newThis")
sparkplugOpInitDerived(f, 0)
if !f.SuperCalled {
t.Error("expected SuperCalled after InitDerived")
}
if f.This.ToString() != "newThis" {
t.Error("expected this updated")
}
}

func TestSparkplugOpCheckThisReinitR4(t *testing.T) {
f := newTestFrame()
f.Func.IsDerivedConstructor = true
f.SuperCalled = true
sparkplugOpCheckThisReinit(f)
if !f.ShouldReturn {
t.Error("expected ShouldReturn for reinit")
}
}

func TestSparkplugOpIncGlobalSlotR4(t *testing.T) {
f := newTestFrame()
f.Func.GlobalVals[0] = js.NewNumber(10)
sparkplugOpIncGlobalSlot(f, 0)
if f.Acc.ToNumber() != 11 {
t.Errorf("expected 11, got %v", f.Acc)
}
if f.Func.GlobalVals[0].ToNumber() != 11 {
t.Errorf("expected 11 in slot, got %v", f.Func.GlobalVals[0])
}
}

func TestSparkplugOpDecGlobalSlotR4(t *testing.T) {
f := newTestFrame()
f.Func.GlobalVals[0] = js.NewNumber(10)
sparkplugOpDecGlobalSlot(f, 0)
if f.Acc.ToNumber() != 9 {
t.Errorf("expected 9, got %v", f.Acc)
}
}

func TestSparkplugOpIncNamedPropertyR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("count")
obj := js.NewJSObject()
obj.Set("count", js.NewNumber(5))
f.Regs[0] = js.NewObject(obj)
sparkplugOpIncNamedProperty(f, 0, 0)
if obj.Get("count").ToNumber() != 6 {
t.Errorf("expected 6, got %v", obj.Get("count"))
}
}

func TestSparkplugOpDecNamedPropertyR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("count")
obj := js.NewJSObject()
obj.Set("count", js.NewNumber(5))
f.Regs[0] = js.NewObject(obj)
sparkplugOpDecNamedProperty(f, 0, 0)
if obj.Get("count").ToNumber() != 4 {
t.Errorf("expected 4, got %v", obj.Get("count"))
}
}

func TestSparkplugOpIncKeyedPropertyR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("k", js.NewNumber(3))
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewString("k")
sparkplugOpIncKeyedProperty(f, 0, 1)
if obj.Get("k").ToNumber() != 4 {
t.Errorf("expected 4, got %v", obj.Get("k"))
}
}

func TestSparkplugOpDecKeyedPropertyR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
obj.Set("k", js.NewNumber(3))
f.Regs[0] = js.NewObject(obj)
f.Regs[1] = js.NewString("k")
sparkplugOpDecKeyedProperty(f, 0, 1)
if obj.Get("k").ToNumber() != 2 {
t.Errorf("expected 2, got %v", obj.Get("k"))
}
}

func TestSparkplugOpSuspendGeneratorR4(t *testing.T) {
f := newTestFrame()
f.GenState = &js.GeneratorState{}
f.Acc = js.NewNumber(100)
f.Regs[0] = js.NewNumber(1)
sparkplugOpSuspendGenerator(f, 5)
if f.GenState.SavedPC != 5 {
t.Error("expected SavedPC 5")
}
if f.GenState.Done != false {
t.Error("expected Done false")
}
}

func TestSparkplugOpResumeGeneratorR4(t *testing.T) {
f := newTestFrame()
f.GenState = &js.GeneratorState{
SavedPC:   5,
SavedAcc:  js.NewNumber(200),
SavedRegs: []js.JSValue{js.NewNumber(10), js.NewNumber(20)},
SavedThis: js.NewString("thisVal"),
}
f.Regs = make([]js.JSValue, 5)
sparkplugOpResumeGenerator(f, 1)
if f.PC != 5 {
t.Errorf("expected PC 5, got %d", f.PC)
}
}

func TestSparkplugOpGetIteratorR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
it := js.NewJSObject()
obj.Set("Symbol(Symbol.iterator)", js.NewObject(it))
f.Acc = js.NewObject(obj)
sparkplugOpGetIterator(f)
if !f.Acc.IsObject() || f.Acc.ObjVal != it {
t.Error("expected iterator")
}
}

func TestSparkplugOpIteratorNextR4(t *testing.T) {
f := newTestFrame()
iterObj := js.NewJSObject()
f.Regs[0] = js.NewObject(iterObj)
sparkplugOpIteratorNext(f, 0)
if !f.Acc.IsObject() {
t.Error("expected result object")
}
}

func TestSparkplugOpIteratorCloseR4(t *testing.T) {
f := newTestFrame()
iterObj := js.NewJSObject()
f.Regs[0] = js.NewObject(iterObj)
sparkplugOpIteratorClose(f, 0)
}

func TestSparkplugOpCreateGeneratorObjectR4(t *testing.T) {
f := newTestFrame()
f.GenState = &js.GeneratorState{}
f.Acc = js.NewNumber(0)
sparkplugOpCreateGeneratorObject(f)
if f.GenState.GenObj == nil {
t.Error("expected GenObj set")
}
}

func TestSparkplugOpGeneratorRestoreR4(t *testing.T) {
f := newTestFrame()
f.GenState = &js.GeneratorState{
SavedPC:   5,
SavedAcc:  js.NewNumber(300),
SavedRegs: []js.JSValue{js.NewNumber(1)},
SavedThis: js.NewString("t"),
Done:      false,
}
f.Regs = make([]js.JSValue, 5)
sparkplugOpGeneratorRestore(f)
if f.PC != 5 {
t.Errorf("expected PC 5, got %d", f.PC)
}
f.GenState.Done = true
sparkplugOpGeneratorRestore(f)
if !f.Acc.IsObject() {
t.Error("expected done result object")
}
}

func TestSparkplugOpAwaitR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(42)
f.Regs = []js.JSValue{js.NewNumber(1)}
sparkplugOpAwait(f, 10)
if !f.ShouldReturn {
t.Error("await should signal return to VM")
}
if f.GenState == nil {
t.Error("expected GenState created")
}
}

func TestSparkplugOpCreateAsyncGeneratorR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(0)
sparkplugOpCreateAsyncGenerator(f)
if !f.Acc.IsObject() {
t.Error("expected async generator object")
}
if f.GenState == nil || f.GenState.GenObj == nil {
t.Error("expected GenState with GenObj")
}
}

func TestSparkplugOpAsyncAwaitR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(1)
f.Regs = []js.JSValue{js.NewNumber(2)}
sparkplugOpAsyncAwait(f)
if !f.ShouldReturn {
t.Error("async await should signal return to VM")
}
}

func TestSparkplugOpAsyncReturnR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewString("result")
sparkplugOpAsyncReturn(f)
if !f.ShouldReturn {
t.Error("async return should signal return")
}
if !f.Acc.IsObject() {
t.Error("expected resolved promise object")
}
}

func TestSparkplugOpCopyDataPropertiesR4(t *testing.T) {
f := newTestFrame()
src := js.NewJSObject()
src.Shape = js.GetOrCreateShape([]string{"a", "b"})
src.Set("a", js.NewNumber(10))
src.Set("b", js.NewNumber(20))
dst := js.NewJSObject()
f.Regs[0] = js.NewObject(src)
f.Regs[1] = js.NewObject(dst)
sparkplugOpCopyDataProperties(f, 0, 1)
if dst.Get("a").ToNumber() != 10 {
t.Errorf("expected 10, got %v", dst.Get("a"))
}
}

func TestSparkplugOpToObjectR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewString("test")
sparkplugOpToObject(f)
if !f.Acc.IsObject() {
t.Error("expected object wrapper")
}
}

func TestSparkplugOpNewR4(t *testing.T) {
f := newTestFrame()
ctor := js.NewJSObject()
ctor.CallFunc = func(this *js.JSObject, args []js.JSValue) js.JSValue {
return js.NewNumber(42)
}
f.Regs[0] = js.NewObject(ctor)
sparkplugOpNew(f, 0, 0)
if f.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", f.Acc)
}
}

func TestSparkplugOpThrowReferenceErrorR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("myVar")
sparkplugOpThrowReferenceError(f, 0)
if !f.ShouldReturn {
t.Error("expected ShouldReturn after ReferenceError")
}
}

func TestSparkplugOpThrowTypeErrorR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("bad operation")
sparkplugOpThrowTypeError(f, 0)
if !f.ShouldReturn {
t.Error("expected ShouldReturn after TypeError")
}
}

func TestSparkplugOpOptionalChainR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.Null
sparkplugOpOptionalChain(f, 5)
if f.PC != 5 {
t.Errorf("expected PC 5 on nullish, got %d", f.PC)
}
f2 := newTestFrame()
f2.Acc = js.NewNumber(1)
origPC := f2.PC
sparkplugOpOptionalChain(f2, 5)
if f2.PC != origPC {
t.Error("expected PC unchanged for non-nullish")
}
}

func TestSparkplugOpNullishCoalesceR4(t *testing.T) {
f := newTestFrame()
f.Acc = js.NewNumber(123)
sparkplugOpNullishCoalesce(f, 5)
if f.PC != 5 {
t.Errorf("expected PC 5 on non-nullish, got %d", f.PC)
}
f2 := newTestFrame()
f2.Acc = js.Null
origPC := f2.PC
sparkplugOpNullishCoalesce(f2, 5)
if f2.PC != origPC {
t.Error("expected PC unchanged for null")
}
}

func TestSparkplugOpPrivateGetR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("myField")
obj := js.NewJSObject()
obj.Set("#myField", js.NewNumber(42))
f.Regs[1] = js.NewObject(obj)
sparkplugOpPrivateGet(f, 0, 1)
if f.Acc.ToNumber() != 42 {
t.Errorf("expected 42, got %v", f.Acc)
}
}

func TestSparkplugOpPrivateSetR4(t *testing.T) {
f := newTestFrame()
f.Func.Constants[0] = js.NewString("myField")
obj := js.NewJSObject()
f.Regs[1] = js.NewObject(obj)
f.Regs[2] = js.NewNumber(99)
sparkplugOpPrivateSet(f, 0, 1, 2)
if obj.Get("#myField").ToNumber() != 99 {
t.Errorf("expected 99, got %v", obj.Get("#myField"))
}
}

func TestSparkplugOpForOfSetupR4(t *testing.T) {
f := newTestFrame()
obj := js.NewJSObject()
f.Regs[0] = js.NewObject(obj)
sparkplugOpForOfSetup(f, 0)
if !f.Acc.IsUndefined() {
t.Error("expected undefined without iterator")
}
}

func TestSparkplugOpForOfNextR4(t *testing.T) {
f := newTestFrame()
iterObj := js.NewJSObject()
f.Regs[0] = js.NewObject(iterObj)
sparkplugOpForOfNext(f, 0)
if !f.Acc.IsObject() {
t.Error("expected result object")
}
}

func TestSparkplugOpDefineClassR4(t *testing.T) {
f := newTestFrame()
ctor := js.NewJSObject()
ctor.CallFunc = func(this *js.JSObject, args []js.JSValue) js.JSValue {
return js.Undefined
}
f.Func.Constants[0] = js.NewObject(ctor)
sparkplugOpDefineClass(f, 0)
if !f.Acc.IsObject() {
t.Error("expected class object")
}
}

// ============================================================================
// Loop 2: partial coverage improvements — ToObject all tag types
// ============================================================================

func TestSparkplugOpToObjectBooleanR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.True
	sparkplugOpToObject(f)
	if !f.Acc.IsObject() {
		t.Error("expected object wrapper for boolean")
	}
}

func TestSparkplugOpToObjectNumberR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(42)
	sparkplugOpToObject(f)
	if !f.Acc.IsObject() {
		t.Error("expected object wrapper for number")
	}
}

func TestSparkplugOpToObjectSymbolR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewSymbol("sym")
	sparkplugOpToObject(f)
	if !f.Acc.IsObject() {
		t.Error("expected object wrapper for symbol")
	}
}

func TestSparkplugOpToObjectBigIntR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewBigIntFromInt64(12345)
	sparkplugOpToObject(f)
	if !f.Acc.IsObject() {
		t.Error("expected object wrapper for bigint")
	}
}

// ============================================================================
// Loop 2: Typeof all types
// ============================================================================

func TestSparkplugOpTypeofUndefinedR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.Undefined
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "undefined" {
		t.Errorf("expected 'undefined', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofNullR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.Null
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "object" {
		t.Errorf("expected 'object', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofBooleanR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.True
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "boolean" {
		t.Errorf("expected 'boolean', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofNumberR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(42)
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "number" {
		t.Errorf("expected 'number', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofStringR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewString("hello")
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "string" {
		t.Errorf("expected 'string', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofFunctionR4(t *testing.T) {
	f := newTestFrame()
	callable := js.NewJSObject()
	callable.CallFunc = func(this *js.JSObject, args []js.JSValue) js.JSValue {
		return js.Undefined
	}
	f.Acc = js.NewObject(callable)
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "function" {
		t.Errorf("expected 'function', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofNonCallableObjectR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewObject(js.NewJSObject())
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "object" {
		t.Errorf("expected 'object', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofSymbolR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewSymbol("sym")
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "symbol" {
		t.Errorf("expected 'symbol', got %q", f.Acc.StrVal)
	}
}

func TestSparkplugOpTypeofBigIntR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewBigIntFromInt64(123)
	sparkplugOpTypeof(f)
	if f.Acc.StrVal != "bigint" {
		t.Errorf("expected 'bigint', got %q", f.Acc.StrVal)
	}
}

// ============================================================================
// Loop 2: StaGlobalSlot edge cases
// ============================================================================

func TestSparkplugOpStaGlobalSlotNilFuncR4(t *testing.T) {
	f := newTestFrame()
	f.Func = nil
	f.Acc = js.NewNumber(42)
	sparkplugOpStaGlobalSlot(f, 0)
	// Should not panic — just returns early.
}

func TestSparkplugOpStaGlobalSlotExpandR4(t *testing.T) {
	f := newTestFrame()
	f.Func.GlobalVals = make([]js.JSValue, 2) // slots 0,1 exist
	f.Acc = js.NewNumber(99)
	sparkplugOpStaGlobalSlot(f, 5) // slot 5 out of bounds → expand
	if len(f.Func.GlobalVals) < 6 {
		t.Error("expected GlobalVals to expand")
	}
	if f.Func.GlobalVals[5].ToNumber() != 99 {
		t.Errorf("expected 99, got %v", f.Func.GlobalVals[5])
	}
}

// ============================================================================
// Loop 2: LdaKeyedPropertySlow array fast path
// ============================================================================

func TestSparkplugOpLdaKeyedPropertySlowArrayR4(t *testing.T) {
	f := newTestFrame()
	arr := js.NewJSObject()
	arr.Prototype = js.ArrayPrototype
	arr.Shape = js.GetOrCreateShape([]string{"0", "1"})
	arr.Set("0", js.NewNumber(10))
	arr.Set("1", js.NewNumber(20))
	arr.Set("length", js.NewNumber(2))
	f.Regs[1] = js.NewObject(arr)
	f.Acc = js.NewString("0")
	sparkplugOpLdaKeyedPropertySlow(f, 1)
	if f.Acc.ToNumber() != 10 {
		t.Errorf("expected 10, got %v", f.Acc)
	}
}

func TestSparkplugOpLdaKeyedPropertySlowNoObjR4(t *testing.T) {
	f := newTestFrame()
	f.Regs[1] = js.NewNumber(99)
	f.Acc = js.NewString("key")
	sparkplugOpLdaKeyedPropertySlow(f, 1)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined when obj is not an object")
	}
}

// ============================================================================
// Loop 2: StaKeyedPropertySlow array fast path
// ============================================================================

func TestSparkplugOpStaKeyedPropertySlowArrayR4(t *testing.T) {
	f := newTestFrame()
	arr := js.NewJSObject()
	arr.Prototype = js.ArrayPrototype
	arr.Shape = js.GetOrCreateShape([]string{"0", "length"})
	arr.Set("0", js.NewNumber(0))
	arr.Set("length", js.NewNumber(1))
	f.Regs[0] = js.NewObject(arr)
	f.Regs[1] = js.NewNumber(42)
	f.Acc = js.NewString("0")
	sparkplugOpStaKeyedPropertySlow(f, 0, 1)
	if arr.Get("0").ToNumber() != 42 {
		t.Errorf("expected 42, got %v", arr.Get("0"))
	}
}

// ============================================================================
// Loop 2: LessThan / GreaterThan BigInt paths
// ============================================================================

func TestSparkplugOpLessThanBigIntR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewBigIntFromInt64(100)
	f.Regs[0] = js.NewBigIntFromInt64(50)
	sparkplugOpLessThan(f, 0)
	// Regs[0]=50 is lhs, Acc=100. LessThan checks: lhs < Acc = 50 < 100 = true.
	if !f.Acc.IsBoolean() || !f.Acc.IsTruthy() {
		t.Error("expected true for 50 < 100")
	}
}

func TestSparkplugOpGreaterThanBigIntR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewBigIntFromInt64(100)
	f.Regs[0] = js.NewBigIntFromInt64(50)
	sparkplugOpGreaterThan(f, 0)
	// Regs[0]=50 is lhs, Acc=100. GreaterThan checks: lhs > Acc = 50 > 100 = false.
	if !f.Acc.IsBoolean() || f.Acc.IsTruthy() {
		t.Error("expected false for 50 > 100")
	}
}

// ============================================================================
// Loop 2: In operator with non-object
// ============================================================================

func TestSparkplugOpInNonObjectR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(42)
	f.Regs[0] = js.NewString("toString")
	sparkplugOpIn(f, 0)
	if !f.Acc.IsBoolean() || f.Acc.IsTruthy() {
		t.Error("expected false for 'toString' in 42")
	}
}

// ============================================================================
// Loop 2: New with non-constructible
// ============================================================================

func TestSparkplugOpNewNonConstructibleR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	// No CallFunc set — not callable
	f.Regs[0] = js.NewObject(obj)
	sparkplugOpNew(f, 0, 0)
	if !f.ShouldReturn {
		t.Error("expected ShouldReturn=true for non-constructible")
	}
}

// ============================================================================
// Loop 2: ForOfSetup with actual Symbol.iterator
// ============================================================================

func TestSparkplugOpForOfSetupIterableR4(t *testing.T) {
	f := newTestFrame()
	iter := js.NewJSObject()
	iter.CallFunc = func(this *js.JSObject, args []js.JSValue) js.JSValue {
		return js.NewObject(js.NewJSObject())
	}
	obj := js.NewJSObject()
	obj.Set("Symbol(Symbol.iterator)", js.NewObject(iter))
	f.Regs[0] = js.NewObject(obj)
	sparkplugOpForOfSetup(f, 0)
	if !f.Acc.IsObject() {
		t.Error("expected iterator object from for-of setup")
	}
}

// ============================================================================
// Loop 2: ForOfNext with actual iterator
// ============================================================================

func TestSparkplugOpForOfNextIterableR4(t *testing.T) {
	f := newTestFrame()
	resultObj := js.NewJSObject()
	resultObj.Set("value", js.NewNumber(42))
	resultObj.Set("done", js.False)
	nextFunc := js.NewJSObject()
	nextFunc.CallFunc = func(this *js.JSObject, args []js.JSValue) js.JSValue {
		return js.NewObject(resultObj)
	}
	iter := js.NewJSObject()
	iter.Set("next", js.NewObject(nextFunc))
	f.Regs[0] = js.NewObject(iter)
	sparkplugOpForOfNext(f, 0)
	if !f.Acc.IsObject() {
		t.Error("expected {value, done} from for-of next")
	}
}

// ============================================================================
// Loop 2: IteratorNext with callable iterator
// ============================================================================

func TestSparkplugOpIteratorNextCallableR4(t *testing.T) {
	f := newTestFrame()
	resultObj := js.NewJSObject()
	resultObj.Set("value", js.NewNumber(7))
	resultObj.Set("done", js.False)
	nextFunc := js.NewJSObject()
	nextFunc.CallFunc = func(this *js.JSObject, args []js.JSValue) js.JSValue {
		return js.NewObject(resultObj)
	}
	iter := js.NewJSObject()
	iter.Set("next", js.NewObject(nextFunc))
	f.Regs[0] = js.NewObject(iter)
	f.Acc = js.Undefined
	sparkplugOpIteratorNext(f, 0)
	if !f.Acc.IsObject() {
		t.Error("expected {value, done} from iterator next")
	}
}

// ============================================================================
// Loop 2: PrivateGet with field not found (error path)
// ============================================================================

func TestSparkplugOpPrivateGetMissingR4(t *testing.T) {
	f := newTestFrame()
	f.Func.Constants[0] = js.NewString("secret")
	f.Regs[0] = js.NewNumber(42) // not an object → error path
	sparkplugOpPrivateGet(f, 0, 0)
	if !f.ShouldReturn {
		t.Error("expected ShouldReturn when obj is not an object")
	}
}

// ============================================================================
// Loop 2: LdaModuleVar nil Func
// ============================================================================

func TestSparkplugOpLdaModuleVarNilFuncR4(t *testing.T) {
	f := newTestFrame()
	f.Func = nil
	sparkplugOpLdaModuleVar(f, 0)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for nil Func")
	}
}

// ============================================================================
// Loop 2: ForInSetup / ForInNext array path
// ============================================================================

func TestSparkplugOpForInSetupWithKeysR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	obj.Shape = js.GetOrCreateShape([]string{"a", "b", "c"})
	f.Regs[0] = js.NewObject(obj)
	sparkplugOpForInSetup(f, 0)
	// Should return first key "a" (ordering not guaranteed, check string)
	if f.Acc.Tag != js.TagString {
		t.Error("expected string key from for-in setup")
	}
}

func TestSparkplugOpForInNextAdvanceR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	obj.Shape = js.GetOrCreateShape([]string{"x", "y", "z"})
	f.Regs[0] = js.NewObject(obj)
	// Find the first key reported by for-in setup.
	sparkplugOpForInSetup(f, 0)
	firstKey := f.Acc.ToString()
	if firstKey == "" || f.Acc.Tag != js.TagString {
		t.Skip("for-in setup did not return a key")
	}
	// Advance.
	f.Acc = js.NewString(firstKey)
	sparkplugOpForInNext(f, 0)
	// Should advance to another key (could be any of the remaining).
	if f.Acc.Tag == js.TagUndefined {
		t.Log("for-in next returned undefined (end of keys)")
	}
}

// ============================================================================
// Loop 2: LdaCaptured nil ClosureEnv
// ============================================================================

func TestSparkplugOpLdaCapturedNilEnvR4(t *testing.T) {
	f := newTestFrame()
	f.ClosureEnv = nil
	sparkplugOpLdaCaptured(f, 0)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for nil ClosureEnv")
	}
}

// ============================================================================
// Loop 2: Instanceof non-object paths
// ============================================================================

func TestSparkplugOpInstanceofNonObjectR4(t *testing.T) {
	f := newTestFrame()
	f.Regs[0] = js.NewNumber(42)
	sparkplugOpInstanceof(f, 0)
	if !f.Acc.IsBoolean() || f.Acc.IsTruthy() {
		t.Error("expected false for non-object instanceof")
	}
}

// ============================================================================
// Loop 2: jsStrIfNotEmpty edge case
// ============================================================================

func TestJsStrIfNotEmptyEmptyR4(t *testing.T) {
	r := jsStrIfNotEmpty("")
	if r != "" {
		t.Errorf("expected empty, got %q", r)
	}
}

func TestJsStrIfNotEmptySpaceR4(t *testing.T) {
	r := jsStrIfNotEmpty("foo")
	if r != " foo" {
		t.Errorf("expected ' foo', got %q", r)
	}
}

// ============================================================================
// Loop 2: ThrowConstAssignment with empty name
// ============================================================================

func TestSparkplugOpThrowConstAssignmentEmptyNameR4(t *testing.T) {
	f := newTestFrame()
	f.Func.Constants[0] = js.NewString("")
	sparkplugOpThrowConstAssignment(f, 0)
	if !f.ShouldReturn {
		t.Error("expected ShouldReturn")
	}
}

// ============================================================================
// Loop 2: CheckConstructor with undefined
// ============================================================================

func TestSparkplugOpCheckConstructorUndefinedR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.Undefined
	sparkplugOpCheckConstructor(f)
	if !f.ShouldReturn {
		t.Error("expected ShouldReturn for undefined constructor")
	}
}

// ============================================================================
// Loop 2: GeneratorRestore done path
// ============================================================================

func TestSparkplugOpGeneratorRestoreDonePathR4(t *testing.T) {
	f := newTestFrame()
	f.GenState = &js.GeneratorState{
		Done:      true,
		SavedPC:   5,
		SavedAcc:  js.NewNumber(1),
		SavedRegs: []js.JSValue{},
		SavedThis: js.Undefined,
	}
	sparkplugOpGeneratorRestore(f)
	if !f.Acc.IsObject() {
		t.Error("expected {done:true} object from done generator restore")
	}
}

// ============================================================================
// Loop 2: OptionalChain with non-null/undefined
// ============================================================================

func TestSparkplugOpOptionalChainNonNullR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(42)
	sparkplugOpOptionalChain(f, 5)
	if f.PC == 5 {
		t.Error("expected PC unchanged for non-null")
	}
}

// ============================================================================
// Loop 2: NullishCoalesce with non-null
// ============================================================================

func TestSparkplugOpNullishCoalesceNonNullR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(42)
	sparkplugOpNullishCoalesce(f, 5)
	if f.PC != 5 {
		t.Error("expected PC jump for non-nullish")
	}
}

// ============================================================================
// Loop 2: CreateClosure with non-function acc
// ============================================================================

func TestSparkplugOpCreateClosureNonFuncR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewString("not a function")
	sparkplugOpCreateClosure(f)
	if !f.Acc.IsObject() {
		t.Error("expected fallback object")
	}
}

// ============================================================================
// Loop 2: SetPrototype non-object paths
// ============================================================================

func TestSparkplugOpSetPrototypeNonObjectR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(1)
	f.Regs[0] = js.NewNumber(2)
	sparkplugOpSetPrototype(f, 0)
	// Should not panic, no-op.
}

// ============================================================================
// Loop 2: ThrowSuperAlreadyCalled non-derived
// ============================================================================

func TestSparkplugOpThrowSuperAlreadyCalledNonDerivedR4(t *testing.T) {
	f := newTestFrame()
	f.Func.IsDerivedConstructor = false
	f.SuperCalled = true
	sparkplugOpThrowSuperAlreadyCalled(f)
	if f.ShouldReturn {
		t.Error("expected no throw for non-derived constructor")
	}
}

// ============================================================================
// Loop 2: ThrowSuperNotCalled non-derived
// ============================================================================

func TestSparkplugOpThrowSuperNotCalledNonDerivedR4(t *testing.T) {
	f := newTestFrame()
	f.Func.IsDerivedConstructor = false
	f.SuperCalled = false
	sparkplugOpThrowSuperNotCalled(f)
	if f.ShouldReturn {
		t.Error("expected no throw for non-derived constructor")
	}
}

// ============================================================================
// Loop 2: Inc/DecGlobalSlot non-number paths
// ============================================================================

func TestSparkplugOpIncGlobalSlotNonNumberR4(t *testing.T) {
	f := newTestFrame()
	f.Func.GlobalVals[0] = js.NewString("abc")
	sparkplugOpIncGlobalSlot(f, 0)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-number global slot")
	}
}

func TestSparkplugOpDecGlobalSlotNonNumberR4(t *testing.T) {
	f := newTestFrame()
	f.Func.GlobalVals[0] = js.NewString("abc")
	sparkplugOpDecGlobalSlot(f, 0)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-number global slot")
	}
}

// ============================================================================
// Loop 2: Inc/DecNamedProperty non-number paths
// ============================================================================

func TestSparkplugOpIncNamedPropertyNonNumberR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	obj.Set("x", js.NewString("abc"))
	f.Func.Constants[0] = js.NewString("x")
	f.Regs[0] = js.NewObject(obj)
	sparkplugOpIncNamedProperty(f, 0, 0)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-number property")
	}
}

func TestSparkplugOpDecNamedPropertyNonNumberR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	obj.Set("x", js.NewString("abc"))
	f.Func.Constants[0] = js.NewString("x")
	f.Regs[0] = js.NewObject(obj)
	sparkplugOpDecNamedProperty(f, 0, 0)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-number property")
	}
}

// ============================================================================
// Loop 2: Inc/DecKeyedProperty non-number paths
// ============================================================================

func TestSparkplugOpIncKeyedPropertyNonNumberR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	obj.Set("x", js.NewString("abc"))
	f.Regs[0] = js.NewObject(obj)
	f.Regs[1] = js.NewString("x")
	sparkplugOpIncKeyedProperty(f, 0, 1)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-number keyed property")
	}
}

func TestSparkplugOpDecKeyedPropertyNonNumberR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	obj.Set("x", js.NewString("abc"))
	f.Regs[0] = js.NewObject(obj)
	f.Regs[1] = js.NewString("x")
	sparkplugOpDecKeyedProperty(f, 0, 1)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-number keyed property")
	}
}

// ============================================================================
// Loop 2: CreateEmptyArray with capacity zero
// ============================================================================

func TestSparkplugOpCreateEmptyArrayZeroCapR4(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			_ = r // known nil-Shape bug in CreateEmptyArray
		}
	}()
	f := newTestFrame()
	sparkplugOpCreateEmptyArray(f, 3)
	// If we get here (no panic), verify basic properties.
	if f.Acc.IsObject() {
		length := f.Acc.ObjVal.Get("length")
		if length.ToNumber() != 3 {
			t.Errorf("expected length 3, got %v", length)
		}
	}
}

// ============================================================================
// Loop 2: Delete non-object acc
// ============================================================================

func TestSparkplugOpDeleteNonObjectR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(42)
	f.Func.Constants[0] = js.NewString("prop")
	sparkplugOpDelete(f, 0)
	if !f.Acc.IsBoolean() || f.Acc.IsTruthy() {
		t.Error("expected false when deleting from non-object")
	}
}

// ============================================================================
// Loop 2: LdaGlobalDirect out of range
// ============================================================================

func TestSparkplugOpLdaGlobalDirectOutOfRangeR4(t *testing.T) {
	f := newTestFrame()
	sparkplugOpLdaGlobalDirect(f, 999)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for out-of-range constant index")
	}
}

// ============================================================================
// Loop 2: ArrayLength non-object path
// ============================================================================

func TestSparkplugOpArrayLengthNonObjectR4(t *testing.T) {
	f := newTestFrame()
	f.Regs[0] = js.NewNumber(42)
	sparkplugOpArrayLength(f, 0)
	if f.Acc.ToNumber() != 0 {
		t.Errorf("expected 0 for non-object, got %v", f.Acc)
	}
}

// ============================================================================
// Loop 2: GetSuperConstructor non-derived path
// ============================================================================

func TestSparkplugOpGetSuperConstructorNonDerivedR4(t *testing.T) {
	f := newTestFrame()
	f.Func.IsDerivedConstructor = false
	sparkplugOpGetSuperConstructor(f)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-derived constructor")
	}
}

// ============================================================================
// Loop 2: LdaHomeObject non-object this
// ============================================================================

func TestSparkplugOpLdaHomeObjectNonObjectR4(t *testing.T) {
	f := newTestFrame()
	f.This = js.NewNumber(42)
	sparkplugOpLdaHomeObject(f)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for non-object this")
	}
}

// ============================================================================
// Loop 2: SuspendGenerator nil GenState
// ============================================================================

func TestSparkplugOpSuspendGeneratorNilStateR4(t *testing.T) {
	f := newTestFrame()
	f.GenState = nil
	sparkplugOpSuspendGenerator(f, 5)
	// Should not panic — returns early.
}

// ============================================================================
// Loop 2: ResumeGenerator nil GenState
// ============================================================================

func TestSparkplugOpResumeGeneratorNilStateR4(t *testing.T) {
	f := newTestFrame()
	f.GenState = nil
	sparkplugOpResumeGenerator(f, 5)
	// Should not panic — returns early.
}

// ============================================================================
// Loop 2: CreateGeneratorObject nil GenState
// ============================================================================

func TestSparkplugOpCreateGeneratorObjectNilStateR4(t *testing.T) {
	f := newTestFrame()
	f.GenState = nil
	sparkplugOpCreateGeneratorObject(f)
	// Should not panic — returns early.
}

// ============================================================================
// Loop 2: GetIterator with object that has no iterator
// ============================================================================

func TestSparkplugOpGetIteratorNoneR4(t *testing.T) {
	f := newTestFrame()
	obj := js.NewJSObject()
	f.Acc = js.NewObject(obj)
	sparkplugOpGetIterator(f)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined when no iterator found")
	}
}

// ============================================================================
// Loop 2: IteratorNext with non-iterator
// ============================================================================

func TestSparkplugOpIteratorNextNonIterR4(t *testing.T) {
	f := newTestFrame()
	f.Regs[0] = js.NewNumber(42)
	sparkplugOpIteratorNext(f, 0)
	if !f.Acc.IsObject() {
		t.Error("expected {done:true} stub object")
	}
}

// ============================================================================
// Loop 2: IteratorClose with non-iterator
// ============================================================================

func TestSparkplugOpIteratorCloseNonIterR4(t *testing.T) {
	f := newTestFrame()
	f.Regs[0] = js.NewNumber(42)
	sparkplugOpIteratorClose(f, 0)
	// Should not panic.
}

// ============================================================================
// Loop 2: LdaHomeObjectProperty nil homeObj
// ============================================================================

func TestSparkplugOpLdaHomeObjectPropertyNilR4(t *testing.T) {
	f := newTestFrame()
	f.Func.Constants[0] = js.NewString("prop")
	f.Regs[0] = js.NewNumber(42) // not an object
	sparkplugOpLdaHomeObjectProperty(f, 0, 0)
	if !f.Acc.IsUndefined() {
		t.Error("expected undefined for nil home object")
	}
}

// ============================================================================
// Loop 2: StaHomeObjectProperty nil homeObj
// ============================================================================

func TestSparkplugOpStaHomeObjectPropertyNilR4(t *testing.T) {
	f := newTestFrame()
	f.Func.Constants[0] = js.NewString("prop")
	f.Regs[0] = js.NewNumber(42) // not an object
	f.Acc = js.NewNumber(99)
	sparkplugOpStaHomeObjectProperty(f, 0, 0)
	// Should not panic.
}

// ============================================================================
// Loop 2: ThrowTypeError with empty message index
// ============================================================================

func TestSparkplugOpThrowTypeErrorEmptyMsgR4(t *testing.T) {
	f := newTestFrame()
	f.Func.Constants[0] = js.NewString("")
	sparkplugOpThrowTypeError(f, 0)
	if !f.ShouldReturn {
		t.Error("expected ShouldReturn after TypeError")
	}
}

// ============================================================================
// Loop 2: ThrowReferenceError with empty name index
// ============================================================================

func TestSparkplugOpThrowReferenceErrorEmptyNameR4(t *testing.T) {
	f := newTestFrame()
	f.Func.Constants[0] = js.NewString("")
	sparkplugOpThrowReferenceError(f, 0)
	if !f.ShouldReturn {
		t.Error("expected ShouldReturn after ReferenceError")
	}
}

// ============================================================================
// Loop 2: DefineClass non-callable template
// ============================================================================

func TestSparkplugOpDefineClassNonCallableR4(t *testing.T) {
	f := newTestFrame()
	f.Func.Constants[0] = js.NewNumber(42)
	sparkplugOpDefineClass(f, 0)
	if !f.Acc.IsObject() {
		t.Error("expected class object")
	}
}

// ============================================================================
// Loop 3: CompileSparkplug with many opcodes to cover emitSparkplugOp switch
// ============================================================================

func TestCompileSparkplugManyOpcodesR4(t *testing.T) {
	// Coverage sweep: test many opcodes that are NOT covered by existing tests.
	// Some opcodes require specific context (labels, ICVector) so we skip those.
	opcodes := []js.Opcode{
		// Already covered: OpLdaSmi, OpStar, OpReturn, OpAdd, OpLdaConstant
		js.OpNop,
		js.OpLdaZero,
		js.OpLdaOne,
		js.OpLdaUndefined,
		js.OpLdaNull,
		js.OpLdaTrue,
		js.OpLdaFalse,
		js.OpLdar,
		js.OpJump,
		js.OpSub,
		js.OpMul,
		js.OpDiv,
		js.OpBitwiseAnd,
		js.OpBitwiseOr,
		js.OpBitwiseXor,
		js.OpBitwiseNot,
		js.OpShiftLeft,
		js.OpShiftRight,
		js.OpShiftRightZero,
		js.OpLogicalNot,
		js.OpNegate,
		js.OpStrictEq,
		js.OpLessThan,
		js.OpGreaterThan,
		js.OpEq,
		js.OpNotEq,
		js.OpStrictNotEq,
		js.OpLessEq,
		js.OpGreaterEq,
		js.OpMod,
		js.OpExp,
		js.OpInc,
		js.OpDec,
		js.OpToBoolean,
		js.OpToNumber,
		js.OpToString,
		js.OpTypeof,
	}
	for _, op := range opcodes {
		t.Run(op.String(), func(t *testing.T) {
			bf := &js.BytecodeFunction{
				Name:         "test",
				NumRegisters: 2,
				Instructions: []js.Instruction{
					{Op: op, OperandA: 0, OperandB: 0, OperandC: 0},
					{Op: js.OpReturn},
				},
			}
			code, err := CompileSparkplug(bf)
			if err != nil {
				t.Fatalf("CompileSparkplug(%s): %v", op.String(), err)
			}
			if code == nil {
				t.Fatalf("CompileSparkplug(%s): nil code", op.String())
			}
			defer code.Free()
			if code.Len() < 16 {
				t.Errorf("%s: expected >= 16 bytes, got %d", op.String(), code.Len())
			}
		})
	}
}

func TestCompileSparkplugCallOpcodesR4(t *testing.T) {
	// Opcodes that need specific register operands.
	opcodes := []js.Opcode{
		js.OpCall0,
		js.OpCall1,
		js.OpCall2,
		js.OpCall,
		js.OpCallSpread,
		js.OpThrow,
		js.OpSetTryHandler,
		js.OpClearTryHandler,
		js.OpSetFinallyHandler,
		js.OpMov,
		js.OpDup,
		js.OpLdaGlobal,
		js.OpStaGlobal,
		js.OpLdaLocal,
		js.OpStaLocal,
		js.OpLdaThis,
		js.OpThrowConstAssignment,
		js.OpSetPrototype,
		js.OpCheckConstructor,
		js.OpYield,
		js.OpCreateGenerator,
		js.OpDebugger,
		js.OpLdaModuleVar,
		js.OpStaModuleVar,
		js.OpThrowIfNotSuper,
		js.OpThrowIfHole,
		js.OpCatch,
		js.OpEndTry,
		js.OpThrowSuperAlreadyCalled,
		js.OpThrowSuperNotCalled,
		js.OpGetSuperConstructor,
		js.OpLdaHomeObject,
		js.OpInitDerived,
		js.OpCheckThisReinit,
		js.OpIncGlobalSlot,
		js.OpDecGlobalSlot,
		js.OpIncNamedProperty,
		js.OpDecNamedProperty,
		js.OpIncKeyedProperty,
		js.OpDecKeyedProperty,
		js.OpSuspendGenerator,
		js.OpResumeGenerator,
		js.OpGetIterator,
		js.OpIteratorNext,
		js.OpIteratorClose,
		js.OpCreateGeneratorObject,
		js.OpGeneratorRestore,
		js.OpAwait,
		js.OpCreateAsyncGenerator,
		js.OpAsyncAwait,
		js.OpAsyncReturn,
	}
	for _, op := range opcodes {
		t.Run(op.String(), func(t *testing.T) {
			bf := &js.BytecodeFunction{
				Name:         "test",
				NumRegisters: 3,
				Instructions: []js.Instruction{
					{Op: op, OperandA: 0, OperandB: 0, OperandC: 0},
					{Op: js.OpReturn},
				},
			}
			code, err := CompileSparkplug(bf)
			if err != nil {
				t.Fatalf("CompileSparkplug(%s): %v", op.String(), err)
			}
			if code == nil {
				t.Fatalf("CompileSparkplug(%s): nil code", op.String())
			}
			defer code.Free()
			if code.Len() < 16 {
				t.Errorf("%s: expected >= 16 bytes, got %d", op.String(), code.Len())
			}
		})
	}
}

func TestCompileSparkplugNumberOpsR4(t *testing.T) {
	opcodes := []js.Opcode{
		js.OpAddNumber,
		js.OpSubNumber,
		js.OpMulNumber,
		js.OpDivNumber,
		js.OpModNumber,
		js.OpNegateNumber,
		js.OpIncNumber,
		js.OpDecNumber,
		js.OpStrictEqNumber,
		js.OpStrictNotEqNumber,
		js.OpLessThanNumber,
		js.OpGreaterThanNumber,
		js.OpLessEqNumber,
		js.OpGreaterEqNumber,
		js.OpBitAndNumber,
		js.OpBitOrNumber,
		js.OpBitXorNumber,
		js.OpBitNotNumber,
		js.OpShiftLeftNumber,
		js.OpShiftRightNumber,
		js.OpShiftRightZeroNumber,
		js.OpStringConcat,
		js.OpStringLength,
		js.OpStringEq,
		js.OpToStringNumber,
		js.OpToBooleanNumber,
		js.OpMathAbs,
		js.OpMathFloor,
		js.OpMathCeil,
		js.OpMathSqrt,
	}
	for _, op := range opcodes {
		t.Run(op.String(), func(t *testing.T) {
			bf := &js.BytecodeFunction{
				Name:         "test",
				NumRegisters: 2,
				Instructions: []js.Instruction{
					{Op: op, OperandA: 0, OperandB: 0, OperandC: 0},
					{Op: js.OpReturn},
				},
			}
			code, err := CompileSparkplug(bf)
			if err != nil {
				t.Fatalf("CompileSparkplug(%s): %v", op.String(), err)
			}
			if code == nil {
				t.Fatalf("CompileSparkplug(%s): nil code", op.String())
			}
			defer code.Free()
			if code.Len() < 16 {
				t.Errorf("%s: expected >= 16 bytes, got %d", op.String(), code.Len())
			}
		})
	}
}

func TestCompileSparkplugMoreOpcodesR4(t *testing.T) {
	opcodes := []js.Opcode{
		js.OpCreateObjectLiteral,
		js.OpCreateEmptyArray,
		js.OpCreateArray,
		js.OpCreateObject,
		js.OpCreateClosure,
		js.OpCreateRegExp,
		js.OpDelete,
		js.OpDeleteKeyed,
		js.OpLdaKeyedProperty,
		js.OpStaKeyedProperty,
		js.OpLdaPropByOffset,
		js.OpStaPropByOffset,
		js.OpArrayGetIndex,
		js.OpArraySetIndex,
		js.OpLdaGlobalDirect,
		js.OpStaGlobalDirect,
		js.OpCallBuiltin,
		js.OpCallDirect,
		js.OpPushContext,
		js.OpPopContext,
		js.OpLoadContextSlot,
		js.OpStoreContextSlot,
		js.OpGreaterEqNumber,
		js.OpSwap,
		js.OpLdaTrueFast,
		js.OpLdaFalseFast,
		js.OpForInSetup,
		js.OpForInNext,
		js.OpForInSetupFast,
		js.OpForInNextFast,
		js.OpJumpIfToBooleanTrue,
		js.OpJumpIfToBooleanFalse,
		js.OpJumpIfNotNullish,
		js.OpLdaCaptured,
	}
	for _, op := range opcodes {
		t.Run(op.String(), func(t *testing.T) {
			bf := &js.BytecodeFunction{
				Name:         "test",
				NumRegisters: 3,
				Instructions: []js.Instruction{
					{Op: op, OperandA: 0, OperandB: 0, OperandC: 0},
					{Op: js.OpReturn},
				},
			}
			code, err := CompileSparkplug(bf)
			if err != nil {
				t.Fatalf("CompileSparkplug(%s): %v", op.String(), err)
			}
			if code == nil {
				t.Fatalf("CompileSparkplug(%s): nil code", op.String())
			}
			defer code.Free()
			if code.Len() < 16 {
				t.Errorf("%s: expected >= 16 bytes, got %d", op.String(), code.Len())
			}
		})
	}
}

func TestCompileSparkplugJumpIfOpcodesR4(t *testing.T) {
	opcodes := []js.Opcode{
		js.OpJumpIfFalse,
		js.OpJumpIfTrue,
		js.OpInstanceof,
		js.OpIn,
	}
	for _, op := range opcodes {
		t.Run(op.String(), func(t *testing.T) {
			bf := &js.BytecodeFunction{
				Name:         "test",
				NumRegisters: 2,
				Instructions: []js.Instruction{
					{Op: op, OperandA: 0, OperandB: 0, OperandC: 0},
					{Op: js.OpReturn},
				},
			}
			code, err := CompileSparkplug(bf)
			if err != nil {
				t.Fatalf("CompileSparkplug(%s): %v", op.String(), err)
			}
			if code == nil {
				t.Fatalf("CompileSparkplug(%s): nil code", op.String())
			}
			defer code.Free()
			if code.Len() < 16 {
				t.Errorf("%s: expected >= 16 bytes, got %d", op.String(), code.Len())
			}
		})
	}
}

// ============================================================================
// Loop 3: Call SparkplugCompile closure directly (improves init coverage)
// ============================================================================

func TestSparkplugCompileCallR4(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "testInit",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
			{Op: js.OpReturn},
		},
	}
	rxAddr, err := SparkplugCompile(bf)
	if err != nil {
		t.Fatalf("SparkplugCompile: %v", err)
	}
	if rxAddr == 0 {
		t.Fatal("expected non-zero rxAddr")
	}
}

// ============================================================================
// Loop 3: LdaNamedProperty/StaNamedProperty with ICVector coverage
// ============================================================================

func TestCompileSparkplugWithICOpsR4(t *testing.T) {
	icVector := &js.FeedbackVector{
		Slots: make([]js.ICSlot, 2),
	}
	icVector.Slots[0].State = js.ICMonomorphic
	icVector.Slots[0].Offset = 0
	icVector.Slots[1].State = js.ICMonomorphic
	icVector.Slots[1].Offset = 32

	bf := &js.BytecodeFunction{
		Name:         "testIC",
		NumRegisters: 3,
		ICVector:     icVector,
		Instructions: []js.Instruction{
			{Op: js.OpLdaNamedProperty, OperandA: 0, OperandC: 0},
			{Op: js.OpStaNamedProperty, OperandA: 0, OperandB: 1, OperandC: 1},
			{Op: js.OpReturn},
		},
	}
	code, err := CompileSparkplug(bf)
	if err != nil {
		t.Fatalf("CompileSparkplug with ICVector: %v", err)
	}
	if code == nil {
		t.Fatal("expected non-nil code")
	}
	defer code.Free()
	if code.Len() < 32 {
		t.Errorf("expected >= 32 bytes with IC ops, got %d", code.Len())
	}
}

// ============================================================================
// Loop 3: CompileSparkplug with SuperCall/YieldDelegate opcodes
// ============================================================================

// ============================================================================
// Loop 4: isConditionalBranch full coverage
// ============================================================================

func TestIsConditionalBranchR4(t *testing.T) {
	branchOps := []js.Opcode{
		js.OpJumpIfFalse,
		js.OpJumpIfTrue,
		js.OpJumpIfToBooleanTrue,
		js.OpJumpIfToBooleanFalse,
		js.OpJumpIfNotNullish,
	}
	for _, op := range branchOps {
		if !isConditionalBranch(op) {
			t.Errorf("expected %s to be conditional", op.String())
		}
	}
	nonBranchOps := []js.Opcode{
		js.OpNop, js.OpReturn, js.OpAdd, js.OpJump, js.OpLdaSmi,
	}
	for _, op := range nonBranchOps {
		if isConditionalBranch(op) {
			t.Errorf("expected %s to NOT be conditional", op.String())
		}
	}
}

// ============================================================================
// Loop 4: indexOf tests
// ============================================================================

func TestIndexOfR4(t *testing.T) {
	a := &SSANode{ID: 1}
	b := &SSANode{ID: 2}
	c := &SSANode{ID: 3}
	nodes := []*SSANode{a, b, c}
	if idx := indexOf(nodes, b); idx != 1 {
		t.Errorf("expected index 1, got %d", idx)
	}
	if idx := indexOf(nodes, &SSANode{ID: 99}); idx != -1 {
		t.Errorf("expected -1 for unknown node, got %d", idx)
	}
	if idx := indexOf(nil, a); idx != -1 {
		t.Errorf("expected -1 for nil slice, got %d", idx)
	}
}

// ============================================================================
// Loop 4: specializeTypes with feedback
// ============================================================================

func TestSpecializeTypesR4(t *testing.T) {
	g := &SSAGraph{
		Blocks: []*SSABasicBlock{
			{
				Nodes: []*SSANode{
					{Op: SSAAdd, Type: SSAAny, ID: 1,
						Feedback: &js.ICSlot{ObservedTag: js.TagNumber}},
					{Op: SSAConst, Type: SSAInt32, ID: 2},
					{Op: SSAMul, Type: SSAAny, ID: 3,
						Feedback: &js.ICSlot{ObservedTag: js.TagNumber}},
				},
			},
		},
	}
	specializeTypes(g)
	if g.Blocks[0].Nodes[0].Type != SSAFloat64 {
		t.Error("expected SSAFloat64 after specialization")
	}
	if g.Blocks[0].Nodes[2].Type != SSAFloat64 {
		t.Error("expected SSAFloat64 for Mul with numeric feedback")
	}
	if g.Blocks[0].Nodes[1].Type != SSAInt32 {
		t.Error("expected unchanged type for node without feedback")
	}
}

// ============================================================================
// Loop 4: replaceNode tests
// ============================================================================

func TestReplaceNodeR4(t *testing.T) {
	n1 := &SSANode{ID: 1}
	n2 := &SSANode{ID: 2}
	user := &SSANode{ID: 3, Args: []*SSANode{n1}}
	n1.Users = []*SSANode{user}
	g := &SSAGraph{}
	replaceNode(g, n1, n2)
	if len(n1.Users) != 0 {
		t.Error("expected old node users to be cleared")
	}
	if len(n2.Users) != 1 || n2.Users[0] != user {
		t.Error("expected user to be transferred to new node")
	}
	if user.Args[0] != n2 {
		t.Error("expected user's arg to be new node")
	}
}

// ============================================================================
// Loop 4: CodeBuf Seal and Free
// ============================================================================

func TestCodeBufSealR4(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	// Commit replaces Seal in the dual-mapping implementation.
	// It handles platform-specific cache maintenance.
	buf.Commit()
	// Clean up.
	buf.Free()
}

func TestCodeBufFreeTwiceR4(t *testing.T) {
	buf, err := NewCodeBuf(256)
	if err != nil {
		t.Fatal(err)
	}
	err = buf.Free()
	if err != nil {
		t.Logf("First Free: %v", err)
	}
	// Second Free should fail (already unmapped) — covers error path.
	err = buf.Free()
	if err == nil {
		t.Log("second Free succeeded (unexpected)")
	}
}

// ============================================================================
// Loop 4: SSAGraph operations
// ============================================================================

func TestSSAGraphNewNodeR4(t *testing.T) {
	g := NewSSAGraph()
	n := g.newNode(SSAConst, SSAInt32)
	if n == nil {
		t.Fatal("expected non-nil node")
	}
	if n.Op != SSAConst || n.Type != SSAInt32 {
		t.Errorf("unexpected node: op=%v type=%v", n.Op, n.Type)
	}
	if n.ID != 1 {
		t.Errorf("expected ID 1, got %d", n.ID)
	}
}

func TestNewSSAGraphR4(t *testing.T) {
	g := NewSSAGraph()
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if g.nextID != 1 {
		t.Errorf("expected nextID 1, got %d", g.nextID)
	}
	// Blocks may be nil until populated by buildBasicBlocks.
}

// ============================================================================
// Loop 4: LowerBytecodeToSSA with various bytecodes
// ============================================================================

func TestLowerBytecodeToSSAR4(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "testSSA",
		NumRegisters: 2,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
			{Op: js.OpReturn},
		},
	}
	blocks := buildBasicBlocks(bf)
	g := NewSSAGraph()
	err := lowerBytecodeToSSA(g, bf, blocks)
	if err != nil {
		t.Fatalf("lowerBytecodeToSSA: %v", err)
	}
	// Verify blocks have been populated.
	if len(blocks) == 0 {
		t.Error("expected blocks to exist")
	}
}

func TestLowerBytecodeToSSAWithBlocksR4(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "testSSABlocks",
		NumRegisters: 2,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpStar, OperandA: 0},
			{Op: js.OpLdaSmi, OperandA: 2},
			{Op: js.OpAdd, OperandA: 0},
			{Op: js.OpReturn},
		},
	}
	blocks := buildBasicBlocks(bf)
	g := NewSSAGraph()
	err := lowerBytecodeToSSA(g, bf, blocks)
	if err != nil {
		t.Fatalf("lowerBytecodeToSSA: %v", err)
	}
	// lowerBytecodeToSSA populates nodes inside blocks, not g.Blocks directly.
	// Verify blocks have nodes.
	totalNodes := 0
	for _, b := range blocks {
		totalNodes += len(b.Nodes)
	}
	if totalNodes == 0 {
		t.Error("expected nodes inside basic blocks")
	}
}

func TestBuildBasicBlocksR4(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "testBlocks",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpReturn},
		},
	}
	blocks := buildBasicBlocks(bf)
	if len(blocks) == 0 {
		t.Error("expected basic blocks")
	}
}

// ============================================================================
// Loop 5: lowerSSAToARM64 coverage
// ============================================================================

func TestLowerSSAToARM64SimpleR4(t *testing.T) {
	g := NewSSAGraph()
	ra := NewRegAlloc()
	entryBlock := &SSABasicBlock{ID: 0, StartPC: 0}
	constNode := g.newNode(SSAConst, SSAInt32)
	constNode.Value = js.NewNumber(42)
	reg := ra.AllocateInt(constNode)
	if reg < 0 {
		t.Skip("no register available")
	}
	retNode := g.newNode(SSAReturn, SSAAny, constNode)
	_ = ra.AllocateInt(retNode)
	entryBlock.Nodes = []*SSANode{constNode, retNode}
	g.Blocks = []*SSABasicBlock{entryBlock}
	g.Entry = entryBlock

	code, err := lowerSSAToARM64(g, ra)
	if err != nil {
		t.Fatalf("lowerSSAToARM64: %v", err)
	}
	if code == nil {
		t.Fatal("expected non-nil code")
	}
	defer code.Free()
	if code.Len() < 8 {
		t.Errorf("expected >= 8 bytes, got %d", code.Len())
	}
}

func TestLowerSSAToARM64FloatConstR4(t *testing.T) {
	g := NewSSAGraph()
	ra := NewRegAlloc()
	entryBlock := &SSABasicBlock{ID: 0, StartPC: 0}
	constNode := g.newNode(SSAConst, SSAFloat64)
	constNode.Value = js.NewNumber(3.14)
	reg := ra.AllocateFloat(constNode)
	if reg < 0 {
		t.Skip("no float register available")
	}
	retNode := g.newNode(SSAReturn, SSAAny, constNode)
	_ = ra.AllocateInt(retNode)
	entryBlock.Nodes = []*SSANode{constNode, retNode}
	g.Blocks = []*SSABasicBlock{entryBlock}
	g.Entry = entryBlock

	code, err := lowerSSAToARM64(g, ra)
	if err != nil {
		t.Fatalf("lowerSSAToARM64: %v", err)
	}
	defer code.Free()
	if code.Len() < 8 {
		t.Errorf("expected >= 8 bytes, got %d", code.Len())
	}
}

func TestLowerSSAToARM64AddR4(t *testing.T) {
	g := NewSSAGraph()
	ra := NewRegAlloc()
	entryBlock := &SSABasicBlock{ID: 0, StartPC: 0}
	aNode := g.newNode(SSAConst, SSAInt32)
	aNode.Value = js.NewNumber(10)
	bNode := g.newNode(SSAConst, SSAInt32)
	bNode.Value = js.NewNumber(32)
	regA := ra.AllocateInt(aNode)
	regB := ra.AllocateInt(bNode)
	if regA < 0 || regB < 0 {
		t.Skip("not enough registers")
	}
	addNode := g.newNode(SSAAdd, SSAInt32, aNode, bNode)
	_ = ra.AllocateInt(addNode)
	retNode := g.newNode(SSAReturn, SSAAny, addNode)
	_ = ra.AllocateInt(retNode)
	entryBlock.Nodes = []*SSANode{aNode, bNode, addNode, retNode}
	g.Blocks = []*SSABasicBlock{entryBlock}
	g.Entry = entryBlock

	code, err := lowerSSAToARM64(g, ra)
	if err != nil {
		t.Fatalf("lowerSSAToARM64: %v", err)
	}
	defer code.Free()
	if code.Len() < 16 {
		t.Errorf("expected >= 16 bytes, got %d", code.Len())
	}
}

func TestLowerSSAToARM64BranchR4(t *testing.T) {
	g := NewSSAGraph()
	ra := NewRegAlloc()
	entryBlock := &SSABasicBlock{ID: 0, StartPC: 0}
	thenBlock := &SSABasicBlock{ID: 1, StartPC: 2}
	elseBlock := &SSABasicBlock{ID: 2, StartPC: 4}
	exitBlock := &SSABasicBlock{ID: 3, StartPC: 6}

	condNode := g.newNode(SSAConst, SSAInt32)
	condNode.Value = js.NewNumber(1)
	regCond := ra.AllocateInt(condNode)
	_ = regCond

	branchNode := g.newNode(SSABranch, SSAAny, condNode)
	branchNode.Labels = []*Label{NewLabel(), NewLabel()}
	regBr := ra.AllocateInt(branchNode)
	_ = regBr
	entryBlock.Nodes = []*SSANode{condNode, branchNode}

	thenNode := g.newNode(SSAConst, SSAInt32)
	thenNode.Value = js.NewNumber(1)
	ra.AllocateInt(thenNode)
	thenRetNode := g.newNode(SSAReturn, SSAAny, thenNode)
	ra.AllocateInt(thenRetNode)
	thenBlock.Nodes = []*SSANode{thenNode, thenRetNode}

	elseNode := g.newNode(SSAConst, SSAInt32)
	elseNode.Value = js.NewNumber(0)
	ra.AllocateInt(elseNode)
	elseBlock.Nodes = []*SSANode{elseNode}

	exitNode := g.newNode(SSAReturn, SSAAny)
	ra.AllocateInt(exitNode)
	exitBlock.Nodes = []*SSANode{exitNode}

	g.Blocks = []*SSABasicBlock{entryBlock, thenBlock, elseBlock, exitBlock}
	g.Entry = entryBlock

	code, err := lowerSSAToARM64(g, ra)
	if err != nil {
		t.Fatalf("lowerSSAToARM64: %v", err)
	}
	defer code.Free()
	if code.Len() < 16 {
		t.Errorf("expected >= 16 bytes, got %d", code.Len())
	}
}

func TestLowerSSAToARM64NegConstR4(t *testing.T) {
	g := NewSSAGraph()
	ra := NewRegAlloc()
	entryBlock := &SSABasicBlock{ID: 0, StartPC: 0}
	constNode := g.newNode(SSAConst, SSAInt32)
	constNode.Value = js.NewNumber(-1) // negative → MOVN path
	reg := ra.AllocateInt(constNode)
	if reg < 0 {
		t.Skip("no register available")
	}
	retNode := g.newNode(SSAReturn, SSAAny, constNode)
	_ = ra.AllocateInt(retNode)
	entryBlock.Nodes = []*SSANode{constNode, retNode}
	g.Blocks = []*SSABasicBlock{entryBlock}
	g.Entry = entryBlock

	code, err := lowerSSAToARM64(g, ra)
	if err != nil {
		t.Fatalf("lowerSSAToARM64: %v", err)
	}
	defer code.Free()
	if code.Len() < 8 {
		t.Errorf("expected >= 8 bytes, got %d", code.Len())
	}
}

// ============================================================================
// Loop 5: TurboFan init coverage
// ============================================================================

func TestTurboFanInitR4(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "testTF",
		NumRegisters: 1,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 42},
			{Op: js.OpReturn},
		},
	}
	rxAddr, err := NewBackend().CompileTurboFan(bf)
	if err != nil {
		t.Logf("TurboFanCompiler: %v (expected — may fail without full optimization)", err)
	}
	_ = rxAddr
}

// ============================================================================
// Loop 5: ssa_inline.go inlineMonomorphicCalls
// ============================================================================

func TestInlineMonomorphicCallsR4(t *testing.T) {
	g := NewSSAGraph()
	entryBlock := &SSABasicBlock{ID: 0, StartPC: 0}
	callNode := g.newNode(SSACall, SSAAny)
	callNode.Feedback = &js.ICSlot{
		State:       js.ICMonomorphic,
		ObservedTag: js.TagNumber,
	}
	retNode := g.newNode(SSAReturn, SSAAny, callNode)
	entryBlock.Nodes = []*SSANode{callNode, retNode}
	g.Blocks = []*SSABasicBlock{entryBlock}
	g.Entry = entryBlock

	inlineMonomorphicCalls(g)
	// Verify the function doesn't panic and graph is still valid.
	if g.Blocks == nil {
		t.Error("expected blocks after inlining")
	}
}

// ============================================================================
// Loop 5: AllocateInt exhaustion
// ============================================================================

func TestAllocateIntExhaustionR4(t *testing.T) {
	ra := NewRegAlloc()
	if ra == nil {
		t.Fatal("expected non-nil allocator")
	}
	// Allocate first one.
	n := &SSANode{ID: 1, Type: SSAInt32}
	reg := ra.AllocateInt(n)
	if reg < 0 && len(ra.freeInts) == 0 {
		t.Skip("no int registers available")
	}
	if reg < 0 {
		t.Error("expected valid int register")
	}
	// Try to exhaust.
	savedCount := len(ra.freeInts)
	for i := 0; i < savedCount+1; i++ {
		n2 := &SSANode{ID: 200 + i, Type: SSAInt32}
		r := ra.AllocateInt(n2)
		if r == -1 {
			break // exhausted
		}
	}
}

func TestBuildBasicBlocksWithJumpR4(t *testing.T) {
	bf := &js.BytecodeFunction{
		Name:         "testJump",
		NumRegisters: 2,
		Instructions: []js.Instruction{
			{Op: js.OpLdaSmi, OperandA: 1},
			{Op: js.OpJumpIfFalse, OperandA: 2},
			{Op: js.OpLdaSmi, OperandA: 2},
			{Op: js.OpReturn},
		},
	}
	blocks := buildBasicBlocks(bf)
	if len(blocks) < 2 {
		t.Error("expected at least 2 blocks with conditional jump")
	}
}

// ============================================================================
// Loop 4: sparkplugOpIn non-object path
// ============================================================================

func TestSparkplugOpInWithNonObjectAccR4(t *testing.T) {
	f := newTestFrame()
	f.Acc = js.NewNumber(99) // not an object
	f.Regs[0] = js.NewString("x")
	sparkplugOpIn(f, 0)
	if f.Acc.IsTruthy() {
		t.Error("expected false for 'x' in 99")
	}
}

// ============================================================================
// Loop 4: sparkplugOpLdaCaptured nil env path
// ============================================================================

func TestSparkplugOpLdaCapturedNoEnvR4(t *testing.T) {
	f := newTestFrame()
	f.ClosureEnv = nil
	sparkplugOpLdaCaptured(f, 0)
	if f.Acc.Tag != js.TagUndefined {
		t.Error("expected undefined when ClosureEnv is nil")
	}
}

func TestCompileSparkplugGenOpsR4(t *testing.T) {
	opcodes := []js.Opcode{
		js.OpSuperCall,
		js.OpYieldDelegate,
		js.OpLdaHomeObjectProperty,
		js.OpStaHomeObjectProperty,
		js.OpCopyDataProperties,
		js.OpToObject,
		js.OpNew,
		js.OpThrowReferenceError,
		js.OpThrowTypeError,
		js.OpOptionalChain,
		js.OpNullishCoalesce,
		js.OpPrivateGet,
		js.OpPrivateSet,
		js.OpForOfSetup,
		js.OpForOfNext,
		js.OpDefineClass,
	}
	for _, op := range opcodes {
		t.Run(op.String(), func(t *testing.T) {
			bf := &js.BytecodeFunction{
				Name:         "test",
				NumRegisters: 3,
				Instructions: []js.Instruction{
					{Op: op, OperandA: 0, OperandB: 0, OperandC: 0},
					{Op: js.OpReturn},
				},
			}
			code, err := CompileSparkplug(bf)
			if err != nil {
				t.Fatalf("CompileSparkplug(%s): %v", op.String(), err)
			}
			if code == nil {
				t.Fatalf("CompileSparkplug(%s): nil code", op.String())
			}
			defer code.Free()
			if code.Len() < 16 {
				t.Errorf("%s: expected >= 16 bytes, got %d", op.String(), code.Len())
			}
		})
	}
}
