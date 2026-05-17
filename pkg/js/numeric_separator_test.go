package js

import "testing"

func TestNumericSeparatorDecimal(t *testing.T) {
	vm := NewVM()
	if int(vm.Run("1_000_000").ToNumber()) != 1000000 {
		t.Error("1_000_000 should be 1000000")
	}
}

func TestNumericSeparatorHex(t *testing.T) {
	vm := NewVM()
	if int(vm.Run("0xFF_FF").ToNumber()) != 65535 {
		t.Error("0xFF_FF should be 65535")
	}
}

func TestNumericSeparatorBinary(t *testing.T) {
	vm := NewVM()
	if int(vm.Run("0b1010_0101").ToNumber()) != 165 {
		t.Error("0b1010_0101 should be 165")
	}
}

func TestNumericSeparatorFloat(t *testing.T) {
	vm := NewVM()
	r := vm.Run("1_000.500_123").ToNumber()
	if r < 1000.5 || r > 1000.51 {
		t.Errorf("1_000.500_123 should be ~1000.500123, got %v", r)
	}
}

func TestNumericSeparatorLegacyOctal(t *testing.T) {
	vm := NewVM()
	// 0_77 should parse as legacy octal 077 = 63 (not decimal 77)
	if int(vm.Run("0_77").ToNumber()) != 63 {
		t.Error("0_77 should be 63 (legacy octal)")
	}
}
