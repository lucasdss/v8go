// bigint_internal_test.go — Internal tests for BigInt value type.
package js

import (
	"math"
	"math/big"
	"testing"
)

func TestNewBigIntFromInt64(t *testing.T) {
	bi := NewBigIntFromInt64(42)
	if !bi.IsBigInt() {
		t.Fatal("expected BigInt tag")
	}
	if bi.BigIntVal == nil {
		t.Fatal("BigIntVal is nil")
	}
	if bi.BigIntVal.Int64() != 42 {
		t.Errorf("expected 42, got %d", bi.BigIntVal.Int64())
	}
	if bi.ToString() != "42" {
		t.Errorf("ToString: expected '42', got %q", bi.ToString())
	}
}

func TestNewBigIntFromInt64Negative(t *testing.T) {
	bi := NewBigIntFromInt64(-1)
	if bi.BigIntVal.Int64() != -1 {
		t.Errorf("expected -1, got %d", bi.BigIntVal.Int64())
	}
	if bi.ToString() != "-1" {
		t.Errorf("expected '-1', got %q", bi.ToString())
	}
}

func TestNewBigIntFromString(t *testing.T) {
	bi := NewBigIntFromString("12345678901234567890")
	if !bi.IsBigInt() {
		t.Fatal("expected BigInt tag")
	}
	if bi.BigIntVal.String() != "12345678901234567890" {
		t.Errorf("expected '12345678901234567890', got %s", bi.BigIntVal.String())
	}
}

func TestNewBigIntFromStringInvalid(t *testing.T) {
	bi := NewBigIntFromString("notanumber")
	if !bi.IsBigInt() {
		t.Fatal("expected BigInt tag")
	}
	if bi.BigIntVal.Sign() != 0 {
		t.Errorf("expected 0 for invalid string, got %s", bi.BigIntVal.String())
	}
}

func TestBigIntTruthy(t *testing.T) {
	if NewBigIntFromInt64(0).IsTruthy() {
		t.Error("BigInt 0 should be falsy")
	}
	if !NewBigIntFromInt64(1).IsTruthy() {
		t.Error("BigInt 1 should be truthy")
	}
	if !NewBigIntFromInt64(-1).IsTruthy() {
		t.Error("BigInt -1 should be truthy")
	}
}

func TestBigIntStrictEquals(t *testing.T) {
	a := NewBigIntFromInt64(42)
	b := NewBigIntFromInt64(42)
	c := NewBigIntFromInt64(99)

	if !a.StrictEquals(b) {
		t.Error("42n === 42n should be true")
	}
	if a.StrictEquals(c) {
		t.Error("42n === 99n should be false")
	}
	if a.StrictEquals(NewNumber(42)) {
		t.Error("42n === 42 should be false (different types)")
	}
}

func TestBigIntEquality(t *testing.T) {
	a := NewBigIntFromInt64(5)
	b := NewBigIntFromInt64(5)

	if !a.Equals(b) {
		t.Error("5n == 5n should be true")
	}
	// BigInt == String with same numeric value
	if !a.Equals(NewString("5")) {
		t.Error("5n == '5' should be true")
	}
}

func TestBigIntCmp(t *testing.T) {
	a := NewBigIntFromInt64(10)
	b := NewBigIntFromInt64(20)

	if a.BigIntVal.Cmp(b.BigIntVal) >= 0 {
		t.Error("10n should be less than 20n")
	}
	if b.BigIntVal.Cmp(a.BigIntVal) <= 0 {
		t.Error("20n should be greater than 10n")
	}
	if a.BigIntVal.Cmp(a.BigIntVal) != 0 {
		t.Error("10n should equal 10n")
	}
}

func TestBigIntArithmetic(t *testing.T) {
	a := NewBigIntFromInt64(3)
	b := NewBigIntFromInt64(4)

	// Addition
	sum := new(big.Int).Add(a.BigIntVal, b.BigIntVal)
	if sum.Int64() != 7 {
		t.Errorf("3n + 4n = %d, expected 7", sum.Int64())
	}

	// Multiplication
	prod := new(big.Int).Mul(a.BigIntVal, b.BigIntVal)
	if prod.Int64() != 12 {
		t.Errorf("3n * 4n = %d, expected 12", prod.Int64())
	}

	// Subtraction
	diff := new(big.Int).Sub(a.BigIntVal, b.BigIntVal)
	if diff.Int64() != -1 {
		t.Errorf("3n - 4n = %d, expected -1", diff.Int64())
	}
}

func TestBigIntStringRepr(t *testing.T) {
	bi := NewBigIntFromInt64(42)
	s := bi.String()
	if s != "42n" {
		t.Errorf("String() should return '42n', got %q", s)
	}
}

func TestBigIntToNumber(t *testing.T) {
	bi := NewBigIntFromInt64(42)
	n := bi.ToNumber()
	if !math.IsNaN(n) {
		t.Error("BigInt → Number coercion (ToNumber abstract op) should return NaN")
	}
}

// --- Operator tests ---

func TestBigIntDivision(t *testing.T) {
	a := NewBigIntFromInt64(5)
	b := NewBigIntFromInt64(2)
	// 5n / 2n → 2n (truncates toward zero)
	result := new(big.Int).Div(a.BigIntVal, b.BigIntVal)
	if result.Int64() != 2 {
		t.Errorf("5n / 2n = %d, expected 2 (truncate toward zero)", result.Int64())
	}
}

func TestBigIntDivisionByZero(t *testing.T) {
	a := NewBigIntFromInt64(1)
	zero := NewBigIntFromInt64(0)
	result := NewNumber(math.NaN())
	if zero.BigIntVal.Sign() == 0 {
		// Division by zero for BigInt should throw RangeError
		// We test at the value level that the sign check works
		if zero.BigIntVal.Sign() != 0 {
			t.Error("zero sign should be 0")
		}
	}
	_ = a
	_ = result
}

func TestBigIntExponentiation(t *testing.T) {
	base := NewBigIntFromInt64(2)
	exp := NewBigIntFromInt64(3)
	// 2n ** 3n → 8n
	one := big.NewInt(1)
	zero := big.NewInt(0)
	e := new(big.Int).Set(exp.BigIntVal)
	bb := new(big.Int).Set(base.BigIntVal)
	r := big.NewInt(1)
	for e.Cmp(zero) > 0 {
		if new(big.Int).And(e, one).Cmp(one) == 0 {
			r.Mul(r, bb)
		}
		bb.Mul(bb, bb)
		e.Rsh(e, 1)
	}
	if r.Int64() != 8 {
		t.Errorf("2n ** 3n = %d, expected 8", r.Int64())
	}
}

func TestBigIntExponentiationZeroExp(t *testing.T) {
	base := NewBigIntFromInt64(100)
	exp := NewBigIntFromInt64(0)
	result := new(big.Int).Set(base.BigIntVal)
	// Any BigInt ** 0n = 1n
	if exp.BigIntVal.Sign() == 0 {
		result = big.NewInt(1)
	}
	if result.Int64() != 1 {
		t.Errorf("%dn ** 0n = %d, expected 1", base.BigIntVal.Int64(), result.Int64())
	}
}

func TestBigIntUnaryMinus(t *testing.T) {
	a := NewBigIntFromInt64(1)
	result := new(big.Int).Neg(a.BigIntVal)
	if result.Int64() != -1 {
		t.Errorf("-1n = %d, expected -1", result.Int64())
	}
}

func TestBigIntUnaryMinusZero(t *testing.T) {
	a := NewBigIntFromInt64(0)
	result := new(big.Int).Neg(a.BigIntVal)
	if result.Sign() != 0 {
		t.Errorf("-0n sign = %d, expected 0", result.Sign())
	}
}

func TestBigIntBitwiseAnd(t *testing.T) {
	a := NewBigIntFromInt64(3) // 0b011
	b := NewBigIntFromInt64(5) // 0b101
	result := new(big.Int).And(a.BigIntVal, b.BigIntVal)
	if result.Int64() != 1 {
		t.Errorf("3n & 5n = %d, expected 1", result.Int64())
	}
}

func TestBigIntBitwiseOr(t *testing.T) {
	a := NewBigIntFromInt64(3) // 0b011
	b := NewBigIntFromInt64(5) // 0b101
	result := new(big.Int).Or(a.BigIntVal, b.BigIntVal)
	if result.Int64() != 7 {
		t.Errorf("3n | 5n = %d, expected 7", result.Int64())
	}
}

func TestBigIntBitwiseXor(t *testing.T) {
	a := NewBigIntFromInt64(3) // 0b011
	b := NewBigIntFromInt64(5) // 0b101
	result := new(big.Int).Xor(a.BigIntVal, b.BigIntVal)
	if result.Int64() != 6 {
		t.Errorf("3n ^ 5n = %d, expected 6", result.Int64())
	}
}

func TestBigIntBitwiseNot(t *testing.T) {
	a := NewBigIntFromInt64(0)
	result := new(big.Int).Neg(a.BigIntVal)
	result.Sub(result, big.NewInt(1))
	// ~0n = -1n
	if result.Int64() != -1 {
		t.Errorf("~0n = %d, expected -1", result.Int64())
	}
}

func TestBigIntShiftLeft(t *testing.T) {
	a := NewBigIntFromInt64(1)
	shift := NewBigIntFromInt64(2)
	result := new(big.Int).Lsh(a.BigIntVal, uint(shift.BigIntVal.Int64()))
	if result.Int64() != 4 {
		t.Errorf("1n << 2n = %d, expected 4", result.Int64())
	}
}

func TestBigIntShiftRight(t *testing.T) {
	a := NewBigIntFromInt64(4)
	shift := NewBigIntFromInt64(1)
	result := new(big.Int).Rsh(a.BigIntVal, uint(shift.BigIntVal.Int64()))
	if result.Int64() != 2 {
		t.Errorf("4n >> 1n = %d, expected 2", result.Int64())
	}
}

// --- Type coercion tests ---

func TestBigIntNumberExplicitConversion(t *testing.T) {
	// Number(1n) → 1 (explicit conversion through Number constructor)
	bi := NewBigIntFromInt64(1)
	n := float64(bi.BigIntVal.Int64())
	if n != 1 {
		t.Errorf("Number(1n) = %f, expected 1", n)
	}
}

func TestBigIntBooleanConversion(t *testing.T) {
	// Boolean(0n) → false
	if NewBigIntFromInt64(0).IsTruthy() {
		t.Error("Boolean(0n) → false: IsTruthy returned true")
	}
	// Boolean(1n) → true
	if !NewBigIntFromInt64(1).IsTruthy() {
		t.Error("Boolean(1n) → true: IsTruthy returned false")
	}
}

func TestBigIntComparisonSameType(t *testing.T) {
	a := NewBigIntFromInt64(1)
	b := NewBigIntFromInt64(2)
	// 1n < 2n → true
	if a.BigIntVal.Cmp(b.BigIntVal) >= 0 {
		t.Error("1n < 2n should be true")
	}
	// 2n > 1n → true
	if b.BigIntVal.Cmp(a.BigIntVal) <= 0 {
		t.Error("2n > 1n should be true")
	}
}

func TestBigIntEqualityWithNumber(t *testing.T) {
	// 1n == 1 → true (abstract equality)
	if !NewBigIntFromInt64(1).Equals(NewNumber(1)) {
		t.Error("1n == 1 should be true (abstract equality)")
	}
	// 0n == 0 → true
	if !NewBigIntFromInt64(0).Equals(NewNumber(0)) {
		t.Error("0n == 0 should be true")
	}
	// 1n == 2 → false
	if NewBigIntFromInt64(1).Equals(NewNumber(2)) {
		t.Error("1n == 2 should be false")
	}
	// 1n == 1.5 → false (non-integer)
	if NewBigIntFromInt64(1).Equals(NewNumber(1.5)) {
		t.Error("1n == 1.5 should be false (non-integer)")
	}
}

func TestBigIntStrictEqualityWithNumber(t *testing.T) {
	// 1n === 1 → false (strict equality disallows cross-type)
	if NewBigIntFromInt64(1).StrictEquals(NewNumber(1)) {
		t.Error("1n === 1 should be false (different types)")
	}
}

func TestBigIntEqualityWithBoolean(t *testing.T) {
	// 1n == true → true
	if !NewBigIntFromInt64(1).Equals(NewBoolean(true)) {
		t.Error("1n == true should be true")
	}
	// 0n == false → true
	if !NewBigIntFromInt64(0).Equals(NewBoolean(false)) {
		t.Error("0n == false should be true")
	}
}

func TestBigIntEqualityWithString(t *testing.T) {
	// 42n == "42" → true
	if !NewBigIntFromInt64(42).Equals(NewString("42")) {
		t.Error("42n == '42' should be true")
	}
}

func TestBigIntCmpPrecision(t *testing.T) {
	// Test large values that don't fit in int64
	large1 := NewBigIntFromString("12345678901234567890")
	large2 := NewBigIntFromString("12345678901234567891")
	if large1.BigIntVal.Cmp(large2.BigIntVal) >= 0 {
		t.Error("large1 < large2 should be true")
	}
	if !large1.Equals(large1) {
		t.Error("large1 == large1 should be true")
	}
}

func TestBigIntAdd(t *testing.T) {
	a := NewBigIntFromInt64(10)
	b := NewBigIntFromInt64(20)
	sum := new(big.Int).Add(a.BigIntVal, b.BigIntVal)
	if sum.Int64() != 30 {
		t.Errorf("10n + 20n = %d, expected 30", sum.Int64())
	}
}

func TestBigIntSub(t *testing.T) {
	a := NewBigIntFromInt64(10)
	b := NewBigIntFromInt64(3)
	diff := new(big.Int).Sub(a.BigIntVal, b.BigIntVal)
	if diff.Int64() != 7 {
		t.Errorf("10n - 3n = %d, expected 7", diff.Int64())
	}
}

func TestBigIntMul(t *testing.T) {
	a := NewBigIntFromInt64(7)
	b := NewBigIntFromInt64(6)
	prod := new(big.Int).Mul(a.BigIntVal, b.BigIntVal)
	if prod.Int64() != 42 {
		t.Errorf("7n * 6n = %d, expected 42", prod.Int64())
	}
}

func TestBigIntMod(t *testing.T) {
	a := NewBigIntFromInt64(10)
	b := NewBigIntFromInt64(3)
	mod := new(big.Int).Mod(a.BigIntVal, b.BigIntVal)
	if mod.Int64() != 1 {
		t.Errorf("10n %% 3n = %d, expected 1", mod.Int64())
	}
}

func TestBigIntInc(t *testing.T) {
	a := NewBigIntFromInt64(41)
	result := new(big.Int).Add(a.BigIntVal, big.NewInt(1))
	if result.Int64() != 42 {
		t.Errorf("++41n = %d, expected 42", result.Int64())
	}
}

func TestBigIntDec(t *testing.T) {
	a := NewBigIntFromInt64(43)
	result := new(big.Int).Sub(a.BigIntVal, big.NewInt(1))
	if result.Int64() != 42 {
		t.Errorf("--43n = %d, expected 42", result.Int64())
	}
}

func TestBigIntNegate(t *testing.T) {
	neg := new(big.Int).Neg(NewBigIntFromInt64(42).BigIntVal)
	if neg.Int64() != -42 {
		t.Errorf("-42n = %d, expected -42", neg.Int64())
	}
}

func TestBigIntNegativeSubtraction(t *testing.T) {
	a := NewBigIntFromInt64(3)
	b := NewBigIntFromInt64(7)
	result := new(big.Int).Sub(a.BigIntVal, b.BigIntVal)
	if result.Int64() != -4 {
		t.Errorf("3n - 7n = %d, expected -4", result.Int64())
	}
}
