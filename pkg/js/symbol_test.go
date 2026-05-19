package js

import (
	"testing"
)

func TestNewSymbol(t *testing.T) {
	sym := NewSymbol("test")
	if !sym.IsSymbol() {
		t.Fatal("NewSymbol should create a Symbol value")
	}
	if sym.Tag != TagSymbol {
		t.Fatalf("Tag = %d, want TagSymbol (%d)", sym.Tag, TagSymbol)
	}
}

func TestSymbolIsSymbol(t *testing.T) {
	sym := NewSymbol("desc")
	if !sym.IsSymbol() {
		t.Fatal("IsSymbol should return true")
	}

	num := NewNumber(42)
	if num.IsSymbol() {
		t.Fatal("IsSymbol should return false for numbers")
	}

	str := NewString("hello")
	if str.IsSymbol() {
		t.Fatal("IsSymbol should return false for strings")
	}
}

func TestSymbolUniqueness(t *testing.T) {
	sym1 := NewSymbol("foo")
	sym2 := NewSymbol("foo")

	if sym1.SymVal == sym2.SymVal {
		t.Fatal("Two calls to NewSymbol should produce distinct SymVal identities")
	}
	if !sym1.StrictEquals(sym1) {
		t.Fatal("Symbol should equal itself")
	}
	if sym1.StrictEquals(sym2) {
		t.Fatal("Two different symbols should not be strictly equal")
	}
}

func TestSymbolToString(t *testing.T) {
	sym := NewSymbol("hello")
	ts := sym.ToString()
	if ts != sym.SymVal {
		t.Fatalf("ToString() = %q, want %q", ts, sym.SymVal)
	}
}

func TestSymbolString(t *testing.T) {
	sym := NewSymbol("desc")
	s := sym.String()
	if s != sym.SymVal {
		t.Fatalf("String() = %q, want %q", s, sym.SymVal)
	}
}

func TestSymbolTypeof(t *testing.T) {
	sym := NewSymbol("test")
	typ := jsTypeof(sym)
	if typ != "symbol" {
		t.Fatalf("typeof symbol = %q, want %q", typ, "symbol")
	}
}

func TestSymbolEqualsSelf(t *testing.T) {
	sym := NewSymbol("")
	if !sym.Equals(sym) {
		t.Fatal("Symbol should equal itself via loose equality")
	}
}

func TestSymbolDoesNotEqualOther(t *testing.T) {
	sym1 := NewSymbol("a")
	sym2 := NewSymbol("a")
	if sym1.Equals(sym2) {
		t.Fatal("Distinct symbols should not be loosely equal")
	}
}

func TestSymbolIsTruthy(t *testing.T) {
	sym := NewSymbol("")
	if !sym.IsTruthy() {
		t.Fatal("Symbol should be truthy")
	}
}

func TestSymbolToNumberReturnsNaN(t *testing.T) {
	sym := NewSymbol("test")
	n := sym.ToNumber()
	if !isNaN(n) {
		t.Fatalf("Symbol.ToNumber() = %v, want NaN", n)
	}
}

func TestSymbolForAndKeyFor(t *testing.T) {
	// Test the global symbol registry directly.
	key := "global.test"
	sym1 := SymbolFor(key)
	sym2 := SymbolFor(key)

	if sym1.SymVal != sym2.SymVal {
		t.Fatal("Symbol.for should return the same symbol for the same key")
	}
	if !sym1.StrictEquals(sym2) {
		t.Fatal("Symbol.for results should be strictly equal")
	}

	foundKey := SymbolKeyFor(sym1)
	if foundKey != key {
		t.Fatalf("Symbol.keyFor = %q, want %q", foundKey, key)
	}
}

func TestSymbolKeyForUnknown(t *testing.T) {
	sym := NewSymbol("not registered")
	result := SymbolKeyFor(sym)
	if result != "" {
		t.Fatalf("Symbol.keyFor of unregistered symbol should return empty string, got %q", result)
	}
}

func TestSymbolIterator(t *testing.T) {
	// Create Symbol.iterator (well-known symbol) and verify it's a Symbol.
	iter := NewSymbol("Symbol.iterator")
	if !iter.IsSymbol() {
		t.Fatal("Symbol.iterator should be a Symbol")
	}
	if iter.StrVal != "Symbol.iterator" {
		t.Fatalf("Symbol.iterator description = %q, want %q", iter.StrVal, "Symbol.iterator")
	}
}

func TestSymbolToStringTag(t *testing.T) {
	stag := NewSymbol("Symbol.toStringTag")
	if !stag.IsSymbol() {
		t.Fatal("Symbol.toStringTag should be a Symbol")
	}
	if stag.StrVal != "Symbol.toStringTag" {
		t.Fatalf("Symbol.toStringTag description = %q, want %q", stag.StrVal, "Symbol.toStringTag")
	}
}

func TestGoStringIncludesSymVal(t *testing.T) {
	sym := NewSymbol("x")
	gs := sym.GoString()
	if len(gs) == 0 {
		t.Fatal("GoString should not be empty")
	}
}

func TestDisasmSymbolConstant(t *testing.T) {
	sym := NewSymbol("debug")
	result := formatConstant(sym)
	if len(result) == 0 {
		t.Fatal("formatConstant should handle Symbol")
	}
}

func TestJsToGoValueSymbol(t *testing.T) {
	sym := NewSymbol("test")
	result := jsToGoValue(sym)
	if result != sym.SymVal {
		t.Fatalf("jsToGoValue(Symbol) = %v, want %q", result, sym.SymVal)
	}
}

func TestSymbolAsyncIterator(t *testing.T) {
	vm := NewVM()
	vm.RegisterBuiltins()
	result := vm.Run(`typeof Symbol.asyncIterator`)
	if result.ToString() != "symbol" {
		t.Errorf("Symbol.asyncIterator should be a symbol, got %q", result.ToString())
	}
}

func TestSymbolAsyncIteratorDescription(t *testing.T) {
	sym := NewSymbol("Symbol.asyncIterator")
	if !sym.IsSymbol() {
		t.Fatal("Symbol.asyncIterator should be a Symbol")
	}
	if sym.StrVal != "Symbol.asyncIterator" {
		t.Fatalf("Symbol.asyncIterator description = %q, want %q", sym.StrVal, "Symbol.asyncIterator")
	}
}

// Helper function to test isNaN since math.IsNaN requires float64 extraction
func isNaN(f float64) bool {
	return f != f
}
