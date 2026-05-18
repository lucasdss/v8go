// Package js — GoV8: custom JavaScript engine implementation.
//
// value.go — JSValue: the fundamental type representing all JavaScript values.
// Uses a tagged union approach compatible with Go's GC rather than V8's Smi pointer tagging.
package js

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
)

// intKeys is a precomputed pool of integer key strings for fast property access.
// Array builtins use intKeys[i] instead of fmt.Sprintf("%d", i) to avoid allocation.
// Populated in the init() below alongside smallIntPool.
var intKeys [1024]string

// intKey returns a string representation of the integer i using a precomputed
// pool for values 0-1023 and strconv.Itoa for larger values (allocates).
func intKey(i int) string {
	if i >= 0 && i < len(intKeys) {
		return intKeys[i]
	}
	return strconv.Itoa(i)
}

var stringInternMu sync.Mutex
var stringInternPool = make(map[string]string)

func internString(s string) string {
	stringInternMu.Lock()
	defer stringInternMu.Unlock()
	if existing, ok := stringInternPool[s]; ok {
		return existing
	}
	stringInternPool[s] = s
	return s
}

// TypeTag classifies a JSValue.
type TypeTag uint8

const (
	TagUndefined TypeTag = iota
	TagNull
	TagBoolean
	TagNumber
	TagString
	TagObject
	TagSymbol
	TagBigInt
)

// JSValue is the universal JavaScript value representation.
// It uses a struct with a type tag and a payload — Go-friendly equivalent
// of V8's tagged pointers, trading raw bit-tagging for clarity and GC safety.
// Fields ordered by size for optimal alignment (largest first).
type JSValue struct {
	StrVal    string    // 16 bytes (pointer + len) — also holds symbol description
	NumVal    float64   // 8 bytes
	ObjVal    *JSObject // 8 bytes (pointer)
	BigIntVal *big.Int  // 8 bytes — arbitrary-precision integer
	SymVal    string    // 16 bytes — symbol identity (unique description string)
	BoolVal   bool      // 1 byte
	Tag       TypeTag   // 1 byte
	_         [6]byte   // padding
}

// Convenience constructors.

//go:inline
func NewUndefined() JSValue { return JSValue{Tag: TagUndefined} }

//go:inline
func NewNull() JSValue { return JSValue{Tag: TagNull} }

//go:inline
func NewBoolean(b bool) JSValue {
	return JSValue{Tag: TagBoolean, BoolVal: b}
}

//go:inline
func NewNumber(n float64) JSValue {
	if isSmallInt(n) {
		return smallIntValue(int(n))
	}
	return JSValue{Tag: TagNumber, NumVal: n}
}

// smallIntPool provides pre-allocated JSValue for integers -128..127,
// avoiding a heap allocation on every arithmetic operation result.
const smallIntOffset = 128
var smallIntPool [256]JSValue

func init() {
	for i := 0; i < 256; i++ {
		smallIntPool[i] = JSValue{Tag: TagNumber, NumVal: float64(i - smallIntOffset)}
	}
	for i := 0; i < 1024; i++ {
		intKeys[i] = strconv.Itoa(i)
	}
}

func isSmallInt(n float64) bool {
	i := int(n)
	return float64(i) == n && i >= -smallIntOffset && i < smallIntOffset
}

func smallIntValue(i int) JSValue {
	return smallIntPool[i+smallIntOffset]
}

//go:inline
func NewString(s string) JSValue {
	return JSValue{Tag: TagString, StrVal: internString(s)}
}

//go:inline
func NewObject(obj *JSObject) JSValue {
	return JSValue{Tag: TagObject, ObjVal: obj}
}

// NewSymbol creates a new unique Symbol value.
// The description is used for the Symbol("...") toString form.
// Each call returns a value that is unique for strict equality purposes
// because the SymVal field includes an incrementing counter.
var symCounter int64
var symMu sync.Mutex

//go:inline
func NewSymbol(description string) JSValue {
	symMu.Lock()
	c := symCounter
	symCounter++
	symMu.Unlock()
	return JSValue{
		Tag:    TagSymbol,
		StrVal: description,
		SymVal: fmt.Sprintf("Symbol(%d)", c),
	}
}

var (
	Undefined = NewUndefined()
	Null      = NewNull()
	True      = NewBoolean(true)
	False     = NewBoolean(false)
)

// Object returns the underlying JSObject pointer if the value is an object.
// Returns nil for non-object values and for objects with a nil ObjVal (safety).
func (v JSValue) Object() *JSObject {
	if v.Tag != TagObject || v.ObjVal == nil {
		return nil
	}
	return v.ObjVal
}

// Type checks.

func (v JSValue) IsUndefined() bool { return v.Tag == TagUndefined }
func (v JSValue) IsNull() bool      { return v.Tag == TagNull }
func (v JSValue) IsBoolean() bool   { return v.Tag == TagBoolean }
func (v JSValue) IsNumber() bool    { return v.Tag == TagNumber }
func (v JSValue) IsString() bool    { return v.Tag == TagString }
func (v JSValue) IsObject() bool    { return v.Tag == TagObject }
func (v JSValue) IsSymbol() bool   { return v.Tag == TagSymbol }
func (v JSValue) IsBigInt() bool   { return v.Tag == TagBigInt }

// NewBigIntFromString creates a BigInt JSValue from a decimal string.
func NewBigIntFromString(s string) JSValue {
	bi := new(big.Int)
	if _, ok := bi.SetString(s, 0); !ok {
		bi.SetInt64(0)
	}
	return JSValue{Tag: TagBigInt, BigIntVal: bi}
}

// NewBigIntFromInt64 creates a BigInt JSValue from an int64.
func NewBigIntFromInt64(n int64) JSValue {
	return JSValue{Tag: TagBigInt, BigIntVal: big.NewInt(n)}
}

// NewBigIntFromUint64 creates a BigInt JSValue from a uint64.
func NewBigIntFromUint64(n uint64) JSValue {
	bi := new(big.Int).SetUint64(n)
	return JSValue{Tag: TagBigInt, BigIntVal: bi}
}

// IsNil returns true for undefined and null.
func (v JSValue) IsNil() bool { return v.Tag == TagUndefined || v.Tag == TagNull }

// IsTruthy follows ECMAScript ToBoolean abstract operation.
func (v JSValue) IsTruthy() bool {
	switch v.Tag {
	case TagUndefined, TagNull:
		return false
	case TagBoolean:
		return v.BoolVal
	case TagNumber:
		return v.NumVal != 0 && !math.IsNaN(v.NumVal)
	case TagString:
		return v.StrVal != ""
	case TagObject:
		return true
	case TagSymbol:
		return true
	case TagBigInt:
		return v.BigIntVal != nil && v.BigIntVal.Sign() != 0
	}
	return false
}

// ToNumber follows ECMAScript ToNumber abstraction.
func (v JSValue) ToNumber() float64 {
	switch v.Tag {
	case TagUndefined:
		return math.NaN()
	case TagNull:
		return 0
	case TagBoolean:
		if v.BoolVal {
			return 1
		}
		return 0
	case TagNumber:
		return v.NumVal
	case TagString:
		// Trim whitespace per spec.
		s := strings.TrimSpace(v.StrVal)
		if s == "" {
			return 0
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return math.NaN()
		}
		return n
	case TagObject:
		if obj := v.Object(); obj != nil {
			return obj.ToPrimitiveNumber().ToNumber()
		}
		return 0
	case TagSymbol:
		return math.NaN() // TypeError in strict ES; NaN for coercion safety
	case TagBigInt:
		return math.NaN() // BigInt cannot be coerced to Number
	}
	return 0
}

// ToString follows ECMAScript ToString abstraction.
func (v JSValue) ToString() string {
	switch v.Tag {
	case TagUndefined:
		return "undefined"
	case TagNull:
		return "null"
	case TagBoolean:
		if v.BoolVal {
			return "true"
		}
		return "false"
	case TagNumber:
		if math.IsNaN(v.NumVal) {
			return "NaN"
		}
		if math.IsInf(v.NumVal, 1) {
			return "Infinity"
		}
		if math.IsInf(v.NumVal, -1) {
			return "-Infinity"
		}
		// Format as JS would: avoid scientific notation for integers.
		if v.NumVal == math.Trunc(v.NumVal) {
			return strconv.FormatFloat(v.NumVal, 'f', 0, 64)
		}
		return strconv.FormatFloat(v.NumVal, 'f', -1, 64)
	case TagString:
		return v.StrVal
	case TagSymbol:
		return v.SymVal
	case TagObject:
		if v.ObjVal == nil {
			return "null"
		}
		return v.ObjVal.ToPrimitiveString().ToString()
	case TagBigInt:
		if v.BigIntVal == nil {
			return "0"
		}
		return v.BigIntVal.String()
	}
	return ""
}

// String returns a debug-friendly representation.
func (v JSValue) String() string {
	switch v.Tag {
	case TagUndefined:
		return "undefined"
	case TagNull:
		return "null"
	case TagBoolean:
		return strconv.FormatBool(v.BoolVal)
	case TagNumber:
		return strconv.FormatFloat(v.NumVal, 'f', -1, 64)
	case TagString:
		return v.StrVal
	case TagSymbol:
		return v.SymVal
	case TagObject:
		if v.ObjVal == nil {
			return "null"
		}
		return "[object Object]"
	case TagBigInt:
		if v.BigIntVal == nil {
			return "0n"
		}
		return v.BigIntVal.String() + "n"
	}
	return "<unknown>"
}

// GoString for %#v formatting.
func (v JSValue) GoString() string {
	bi := "<nil>"
	if v.BigIntVal != nil {
		bi = v.BigIntVal.String()
	}
	return fmt.Sprintf("JSValue{Tag:%d, NumVal:%f, StrVal:%q, SymVal:%q, BigInt:%s}", v.Tag, v.NumVal, v.StrVal, v.SymVal, bi)
}

// Equals performs loose equality (==) per ECMAScript Abstract Equality Comparison.
func (v JSValue) Equals(other JSValue) bool {
	// Symbol comparison: same type → strict (pointer-based via SymVal).
	if v.Tag == TagSymbol && other.Tag == TagSymbol {
		return v.StrictEquals(other)
	}
	// BigInt comparison: same type → strict.
	if v.Tag == TagBigInt && other.Tag == TagBigInt {
		return v.StrictEquals(other)
	}
	// Same type → use strict equality.
	if v.Tag == other.Tag {
		return v.StrictEquals(other)
	}
	// null == undefined is true.
	if (v.Tag == TagNull && other.Tag == TagUndefined) ||
		(v.Tag == TagUndefined && other.Tag == TagNull) {
		return true
	}
	// Number vs String: convert string to number.
	if v.Tag == TagNumber && other.Tag == TagString {
		return v.NumVal == other.ToNumber()
	}
	if v.Tag == TagString && other.Tag == TagNumber {
		return v.ToNumber() == other.NumVal
	}
	// BigInt vs String: convert string to BigInt.
	if v.Tag == TagBigInt && other.Tag == TagString {
		bi := NewBigIntFromString(other.StrVal)
		return v.StrictEquals(bi)
	}
	if v.Tag == TagString && other.Tag == TagBigInt {
		bi := NewBigIntFromString(v.StrVal)
		return bi.StrictEquals(other)
	}
	// BigInt vs Number: comparison per ECMAScript spec.
	// If either is NaN or ±Infinity, return false.
	if v.Tag == TagBigInt && other.Tag == TagNumber {
		if math.IsNaN(other.NumVal) || math.IsInf(other.NumVal, 0) {
			return false
		}
		bi := NewBigIntFromInt64(int64(other.NumVal))
		// If Number doesn't represent an integer, return false.
		if float64(int64(other.NumVal)) != other.NumVal {
			return false
		}
		return v.StrictEquals(bi)
	}
	if v.Tag == TagNumber && other.Tag == TagBigInt {
		if math.IsNaN(v.NumVal) || math.IsInf(v.NumVal, 0) {
			return false
		}
		bi := NewBigIntFromInt64(int64(v.NumVal))
		if float64(int64(v.NumVal)) != v.NumVal {
			return false
		}
		return bi.StrictEquals(other)
	}
	// BigInt vs Boolean: convert Boolean to BigInt.
	if v.Tag == TagBigInt && other.Tag == TagBoolean {
		bi := NewBigIntFromInt64(0)
		if other.BoolVal {
			bi = NewBigIntFromInt64(1)
		}
		return v.StrictEquals(bi)
	}
	if v.Tag == TagBoolean && other.Tag == TagBigInt {
		bi := NewBigIntFromInt64(0)
		if v.BoolVal {
			bi = NewBigIntFromInt64(1)
		}
		return bi.StrictEquals(other)
	}
	// Boolean vs anything: convert boolean to number.
	if v.Tag == TagBoolean {
		return NewNumber(v.ToNumber()).Equals(other)
	}
	if other.Tag == TagBoolean {
		return v.Equals(NewNumber(other.ToNumber()))
	}
	// Object vs String/Number: convert object to primitive.
	if v.Tag == TagObject && (other.Tag == TagString || other.Tag == TagNumber) {
		if obj := v.Object(); obj != nil {
			return obj.ToPrimitiveDefault().Equals(other)
		}
		return false
	}
	if other.Tag == TagObject && (v.Tag == TagString || v.Tag == TagNumber) {
		if obj := other.Object(); obj != nil {
			return v.Equals(obj.ToPrimitiveDefault())
		}
		return false
	}
	return false
}

// StrictEquals performs strict equality (===).
func (v JSValue) StrictEquals(other JSValue) bool {
	if v.Tag != other.Tag {
		return false
	}
	switch v.Tag {
	case TagUndefined, TagNull:
		return true
	case TagBoolean:
		return v.BoolVal == other.BoolVal
	case TagNumber:
		// NaN !== NaN.
		if math.IsNaN(v.NumVal) && math.IsNaN(other.NumVal) {
			return false
		}
		return v.NumVal == other.NumVal
	case TagString:
		return v.StrVal == other.StrVal
	case TagObject:
		return v.ObjVal == other.ObjVal // pointer equality
	case TagSymbol:
		return v.SymVal == other.SymVal // unique identity check
	case TagBigInt:
		if v.BigIntVal == nil || other.BigIntVal == nil {
			return v.BigIntVal == other.BigIntVal
		}
		return v.BigIntVal.Cmp(other.BigIntVal) == 0
	}
	return false
}

// concatStrings efficiently concatenates JS values into a single string using a Builder.
func concatStrings(vals ...JSValue) JSValue {
	var b strings.Builder
	for _, v := range vals {
		b.WriteString(v.ToString())
	}
	return NewString(b.String())
}
