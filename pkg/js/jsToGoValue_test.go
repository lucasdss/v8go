// jsToGoValue_test.go — direct tests for jsToGoValue (unexported, internal use).
package js

import (
	"strings"
	"testing"
)

func TestJsToGoValueScalars(t *testing.T) {
	// Undefined → nil
	if result := jsToGoValue(Undefined); result != nil {
		t.Errorf("jsToGoValue(Undefined) = %v, want nil", result)
	}
	// Null → nil
	if result := jsToGoValue(Null); result != nil {
		t.Errorf("jsToGoValue(Null) = %v, want nil", result)
	}
	// Boolean true
	if result := jsToGoValue(NewBoolean(true)); result != true {
		t.Errorf("jsToGoValue(true) = %v, want true", result)
	}
	// Boolean false
	if result := jsToGoValue(NewBoolean(false)); result != false {
		t.Errorf("jsToGoValue(false) = %v, want false", result)
	}
	// Number
	if result := jsToGoValue(NewNumber(42.5)); result != 42.5 {
		t.Errorf("jsToGoValue(42.5) = %v, want 42.5", result)
	}
	// String
	if result := jsToGoValue(NewString("hello")); result != "hello" {
		t.Errorf("jsToGoValue(\"hello\") = %v, want \"hello\"", result)
	}
	// Symbol
	sym := NewSymbol("test")
	if result := jsToGoValue(sym); result != sym.SymVal {
		t.Errorf("jsToGoValue(Symbol(\"test\")) = %v, want %q", result, sym.SymVal)
	}
}

func TestJsToGoValueNilObject(t *testing.T) {
	// Object with nil ObjVal → nil
	v := JSValue{Tag: TagObject, ObjVal: nil}
	if result := jsToGoValue(v); result != nil {
		t.Errorf("jsToGoValue(nil object) = %v, want nil", result)
	}
}

func TestJsToGoValueArray(t *testing.T) {
	// Create an array-like object with length and indexed properties.
	arr := NewJSObject()
	arr.ConstructorName = "Array"
	arr.Set("length", NewNumber(3))
	arr.Set("0", NewNumber(1))
	arr.Set("1", NewString("two"))
	arr.Set("2", NewBoolean(true))

	result := jsToGoValue(NewObject(arr))
	slice, ok := result.([]interface{})
	if !ok {
		t.Fatalf("jsToGoValue(array) type = %T, want []interface{}", result)
	}
	if len(slice) != 3 {
		t.Fatalf("jsToGoValue(array) len = %d, want 3", len(slice))
	}
	if slice[0] != float64(1) {
		t.Errorf("arr[0] = %v, want 1", slice[0])
	}
	if slice[1] != "two" {
		t.Errorf("arr[1] = %v, want \"two\"", slice[1])
	}
	if slice[2] != true {
		t.Errorf("arr[2] = %v, want true", slice[2])
	}
}

func TestJsToGoValueObject(t *testing.T) {
	// Create a regular object with known properties.
	obj := NewJSObject()
	obj.Shape.IsDictionary = false
	obj.Set("name", NewString("Alice"))
	obj.Set("age", NewNumber(30))

	result := jsToGoValue(NewObject(obj))
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("jsToGoValue(object) type = %T, want map[string]interface{}", result)
	}
	if m["name"] != "Alice" {
		t.Errorf("obj.name = %v, want \"Alice\"", m["name"])
	}
	if m["age"] != float64(30) {
		t.Errorf("obj.age = %v, want 30", m["age"])
	}
}

func TestJsToGoValueUnknownTag(t *testing.T) {
	// Unknown tag → nil. TypeTag is uint8, so use 255 as an invalid tag.
	v := JSValue{Tag: 255}
	if result := jsToGoValue(v); result != nil {
		t.Errorf("jsToGoValue(unknown tag) = %v, want nil", result)
	}
}

// --- goToJSValue tests ---

func TestGoToJSValueNil(t *testing.T) {
	result := goToJSValue(nil)
	if !result.IsNull() {
		t.Errorf("goToJSValue(nil) should be Null, got %v", result)
	}
}

func TestGoToJSValueScalars(t *testing.T) {
	if r := goToJSValue(true); !r.IsTruthy() || !r.IsBoolean() {
		t.Errorf("goToJSValue(true) = %v", r)
	}
	if r := goToJSValue(false); r.IsTruthy() {
		t.Errorf("goToJSValue(false) should be falsy, got %v", r)
	}
	if r := goToJSValue(42.5); r.ToNumber() != 42.5 {
		t.Errorf("goToJSValue(42.5) = %v", r)
	}
	if r := goToJSValue("hello"); r.ToString() != "hello" {
		t.Errorf("goToJSValue(\"hello\") = %v", r)
	}
}

func TestGoToJSValueArray(t *testing.T) {
	result := goToJSValue([]interface{}{float64(1), "two", true})
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("goToJSValue(array) should be an Object")
	}
	if result.ObjVal.Get("length").ToNumber() != 3 {
		t.Errorf("array length = %v, want 3", result.ObjVal.Get("length"))
	}
	if result.ObjVal.Get("0").ToNumber() != 1 {
		t.Errorf("arr[0] = %v, want 1", result.ObjVal.Get("0"))
	}
}

func TestGoToJSValueMap(t *testing.T) {
	result := goToJSValue(map[string]interface{}{"a": float64(1), "b": "two"})
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("goToJSValue(map) should be an Object")
	}
	if result.ObjVal.Get("a").ToNumber() != 1 {
		t.Errorf("obj.a = %v, want 1", result.ObjVal.Get("a"))
	}
	if result.ObjVal.Get("b").ToString() != "two" {
		t.Errorf("obj.b = %v, want 'two'", result.ObjVal.Get("b"))
	}
}

func TestGoToJSValueUnknown(t *testing.T) {
	result := goToJSValue(int64(42))
	if !result.IsUndefined() {
		t.Errorf("goToJSValue(int64) should be Undefined, got %v", result)
	}
}

// --- jsToJSON tests ---

func TestJsToJSONScalars(t *testing.T) {
	if r := jsToJSON(Undefined); r != "undefined" {
		t.Errorf("jsToJSON(Undefined) = %q, want \"undefined\"", r)
	}
	if r := jsToJSON(Null); r != "null" {
		t.Errorf("jsToJSON(Null) = %q, want \"null\"", r)
	}
	if r := jsToJSON(NewBoolean(true)); r != "true" {
		t.Errorf("jsToJSON(true) = %q, want \"true\"", r)
	}
	if r := jsToJSON(NewBoolean(false)); r != "false" {
		t.Errorf("jsToJSON(false) = %q, want \"false\"", r)
	}
	if r := jsToJSON(NewNumber(42.5)); r != "42.5" {
		t.Errorf("jsToJSON(42.5) = %q, want \"42.5\"", r)
	}
	if r := jsToJSON(NewString("hello")); !strings.Contains(r, "hello") {
		t.Errorf("jsToJSON(\"hello\") = %q, want json-encoded \"hello\"", r)
	}
}

func TestJsToJSONSymbol(t *testing.T) {
	sym := NewSymbol("test")
	if r := jsToJSON(sym); r != "" {
		t.Errorf("jsToJSON(Symbol) = %q, want \"\"", r)
	}
}

func TestJsToJSONNilObject(t *testing.T) {
	v := JSValue{Tag: TagObject, ObjVal: nil}
	if r := jsToJSON(v); r != "null" {
		t.Errorf("jsToJSON(nil object) = %q, want \"null\"", r)
	}
}

func TestJsToJSONCallable(t *testing.T) {
	// A callable object should return "undefined" in JSON.
	obj := NewJSObject()
	obj.CallFunc = func(this *JSObject, args []JSValue) JSValue { return Undefined }
	if r := jsToJSON(NewObject(obj)); r != "undefined" {
		t.Errorf("jsToJSON(callable) = %q, want \"undefined\"", r)
	}
}

func TestJsToJSONArray(t *testing.T) {
	arr := NewJSObject()
	arr.ConstructorName = "Array"
	arr.Set("length", NewNumber(3))
	arr.Set("0", NewNumber(1))
	arr.Set("1", NewString("two"))
	arr.Set("2", NewBoolean(true))
	r := jsToJSON(NewObject(arr))
	if r != `[1,"two",true]` {
		t.Errorf("jsToJSON(array) = %q, want [1,\"two\",true]", r)
	}
}

func TestJsToJSONObject(t *testing.T) {
	obj := NewJSObject()
	obj.Shape.IsDictionary = false
	obj.Set("name", NewString("Alice"))
	obj.Set("age", NewNumber(30))
	r := jsToJSON(NewObject(obj))
	if !strings.Contains(r, `"name":"Alice"`) || !strings.Contains(r, `"age":30`) {
		t.Errorf("jsToJSON(object) = %q", r)
	}
}

func TestJsToJSONUnknownTag(t *testing.T) {
	v := JSValue{Tag: 255}
	if r := jsToJSON(v); r != "null" {
		t.Errorf("jsToJSON(unknown tag) = %q, want \"null\"", r)
	}
}

// --- formatConstant tests ---

func TestFormatConstantNumber(t *testing.T) {
	r := formatConstant(NewNumber(42.5))
	if r != "Number: 42.5" {
		t.Errorf("formatConstant(42.5) = %q", r)
	}
}

func TestFormatConstantString(t *testing.T) {
	r := formatConstant(NewString("hello"))
	if r != `String: "hello"` {
		t.Errorf("formatConstant(\"hello\") = %q", r)
	}
}

func TestFormatConstantBoolean(t *testing.T) {
	if r := formatConstant(NewBoolean(true)); r != "Boolean: true" {
		t.Errorf("formatConstant(true) = %q", r)
	}
	if r := formatConstant(NewBoolean(false)); r != "Boolean: false" {
		t.Errorf("formatConstant(false) = %q", r)
	}
}

func TestFormatConstantUndefined(t *testing.T) {
	if r := formatConstant(Undefined); r != "Undefined" {
		t.Errorf("formatConstant(Undefined) = %q", r)
	}
}

func TestFormatConstantNull(t *testing.T) {
	if r := formatConstant(Null); r != "Null" {
		t.Errorf("formatConstant(Null) = %q", r)
	}
}

func TestFormatConstantFunction(t *testing.T) {
	fn := NewJSObject()
	fn.ConstructorName = "Function"
	r := formatConstant(NewObject(fn))
	if r != "Function: [object Function]" {
		t.Errorf("formatConstant(Function) = %q", r)
	}
}

func TestFormatConstantPlainObject(t *testing.T) {
	obj := NewJSObject()
	obj.ConstructorName = "MyClass"
	r := formatConstant(NewObject(obj))
	if r != "Object" {
		t.Errorf("formatConstant(MyClass) = %q", r)
	}
}

func TestFormatConstantSymbol(t *testing.T) {
	sym := NewSymbol("test")
	r := formatConstant(sym)
	if !strings.Contains(r, "Symbol:") {
		t.Errorf("formatConstant(Symbol) = %q, want prefix 'Symbol:'", r)
	}
}

// --- createBuiltinBytecode tests ---

func TestCreateBuiltinBytecode(t *testing.T) {
	vm := NewVM()
	bf := &BytecodeFunction{
		NumParams:    2,
		NumRegisters: 4,
		Instructions: []Instruction{},
		Constants:    []JSValue{},
	}
	result := vm.createBuiltinBytecode("testFn", bf)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("createBuiltinBytecode should return an Object")
	}
	if result.ObjVal.ConstructorName != "Function" {
		t.Errorf("ConstructorName = %q, want \"Function\"", result.ObjVal.ConstructorName)
	}
	if result.ObjVal.Get("length").ToNumber() != 2 {
		t.Errorf("length = %v, want 2", result.ObjVal.Get("length"))
	}
	if result.ObjVal.Bytecode != bf {
		t.Error("Bytecode field should be set")
	}
}

// --- objectKeys tests ---

func TestObjectKeysNil(t *testing.T) {
	keys := objectKeys(nil)
	if keys != nil {
		t.Errorf("objectKeys(nil) = %v, want nil", keys)
	}
}

func TestObjectKeysDictionary(t *testing.T) {
	obj := NewJSObject()
	// Clone shape before modifying to avoid corrupting shared EmptyShape.
	shape := *EmptyShape
	shape.IsDictionary = true
	obj.Shape = &shape
	obj.Dictionary = map[string]JSValue{
		"a": NewNumber(1),
		"b": NewNumber(2),
	}
	keys := objectKeys(obj)
	if len(keys) != 2 {
		t.Fatalf("objectKeys(dict) len = %d, want 2", len(keys))
	}
}

// --- formatInstruction tests ---

func TestFormatInstructionVariants(t *testing.T) {
	bf := &BytecodeFunction{
		Constants:     []JSValue{NewNumber(42), NewString("prop")},
		ConstantNames: []string{"42", "prop"},
		GlobalSlots:   []string{"globalVar"},
		Captured:      []string{"capturedVar"},
	}

	tests := []struct {
		name  string
		instr Instruction
	}{
		{"LdaConstant", Instruction{Op: OpLdaConstant, OperandA: 0}},
		{"Star", Instruction{Op: OpStar, OperandA: 3}},
		{"Ldar", Instruction{Op: OpLdar, OperandA: 1}},
		{"Mov", Instruction{Op: OpMov, OperandA: 2, OperandB: 5}},
		{"Add", Instruction{Op: OpAdd, OperandA: 0}},
		{"Sub", Instruction{Op: OpSub, OperandA: 1}},
		{"Mul", Instruction{Op: OpMul, OperandA: 2}},
		{"Eq", Instruction{Op: OpEq, OperandA: 0}},
		{"NotEq", Instruction{Op: OpNotEq, OperandA: 1}},
		{"StrictEq", Instruction{Op: OpStrictEq, OperandA: 2}},
		{"LessThan", Instruction{Op: OpLessThan, OperandA: 0}},
		{"GreaterThan", Instruction{Op: OpGreaterThan, OperandA: 1}},
		{"LessEq", Instruction{Op: OpLessEq, OperandA: 2}},
		{"Instanceof", Instruction{Op: OpInstanceof, OperandA: 0}},
		{"In", Instruction{Op: OpIn, OperandA: 1}},
		{"BitwiseAnd", Instruction{Op: OpBitwiseAnd, OperandA: 2}},
		{"BitwiseOr", Instruction{Op: OpBitwiseOr, OperandA: 0}},
		{"BitwiseXor", Instruction{Op: OpBitwiseXor, OperandA: 1}},
		{"ShiftLeft", Instruction{Op: OpShiftLeft, OperandA: 2}},
		{"ShiftRight", Instruction{Op: OpShiftRight, OperandA: 0}},
		{"ShiftRightZero", Instruction{Op: OpShiftRightZero, OperandA: 1}},
		{"Jump", Instruction{Op: OpJump, OperandA: 10}},
		{"JumpIfFalse", Instruction{Op: OpJumpIfFalse, OperandA: 5}},
		{"JumpIfTrue", Instruction{Op: OpJumpIfTrue, OperandA: 8}},
		{"Call", Instruction{Op: OpCall, OperandA: 1, OperandB: 2, OperandC: 3}},
		{"Call0", Instruction{Op: OpCall0, OperandA: 1, OperandB: 2}},
		{"Call1", Instruction{Op: OpCall1, OperandA: 3, OperandB: 4}},
		{"LdaNamedProperty", Instruction{Op: OpLdaNamedProperty, OperandA: 1}},
		{"StaNamedProperty", Instruction{Op: OpStaNamedProperty, OperandA: 0}},
		{"LdaKeyedProperty", Instruction{Op: OpLdaKeyedProperty, OperandA: 1, OperandB: 2}},
		{"StaKeyedProperty", Instruction{Op: OpStaKeyedProperty, OperandA: 3, OperandB: 4}},
		{"LdaGlobal", Instruction{Op: OpLdaGlobal, OperandA: 1}},
		{"StaGlobal", Instruction{Op: OpStaGlobal, OperandA: 0}},
		{"LdaGlobalSlot", Instruction{Op: OpLdaGlobalSlot, OperandA: 0}},
		{"StaGlobalSlot", Instruction{Op: OpStaGlobalSlot, OperandA: 1}},
		{"LdaLocal", Instruction{Op: OpLdaLocal, OperandA: 3}},
		{"StaLocal", Instruction{Op: OpStaLocal, OperandA: 2}},
		{"Dup", Instruction{Op: OpDup, OperandA: 2}},
		{"ThrowConstAssignment", Instruction{Op: OpThrowConstAssignment, OperandA: 0}},
		{"LdaCaptured", Instruction{Op: OpLdaCaptured, OperandA: 0}},
		{"ForInSetup", Instruction{Op: OpForInSetup, OperandA: 0}},
		{"ForInNext", Instruction{Op: OpForInNext, OperandA: 1}},
		{"Throw", Instruction{Op: OpThrow}},
		{"SetTryHandler", Instruction{Op: OpSetTryHandler, OperandA: 5}},
		{"ClearTryHandler", Instruction{Op: OpClearTryHandler}},
		{"Delete", Instruction{Op: OpDelete, OperandA: 1}},
		{"DeleteKeyed", Instruction{Op: OpDeleteKeyed, OperandA: 1, OperandB: 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatInstruction(0, tt.instr, bf)
			if result == "" {
				t.Errorf("formatInstruction(%s) returned empty string", tt.name)
			}
		})
	}
}
