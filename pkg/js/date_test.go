package js_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestDateNow(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run("typeof Date.now()")
	if result.StrVal != "number" {
		t.Errorf("Date.now() should return a number, got %q", result.ToString())
	}
}

func TestDateConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var d = new Date(2024, 0, 15);
		d.getFullYear() === 2024 && d.getMonth() === 0 && d.getDate() === 15
	`)
	if !result.IsTruthy() {
		t.Error("new Date(2024, 0, 15) should have year=2024, month=0, date=15")
	}
}

func TestDateGetTime(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var d = new Date(0);
		typeof d.getTime() === 'number'
	`)
	if !result.IsTruthy() {
		t.Error("Date.getTime() should return a number")
	}
}

func TestDateUTC(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var ms = Date.UTC(2024, 0, 15);
		ms === new Date(ms).getTime()
	`)
	if !result.IsTruthy() {
		t.Error("Date.UTC should match getTime() of equivalent Date")
	}
}

func TestDateStringConstructor(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var d = new Date("2024-01-15T10:30:00Z");
		d.getFullYear() === 2024 && d.getMonth() === 0 && d.getDate() === 15
	`)
	if !result.IsTruthy() {
		t.Error("new Date('2024-01-15T10:30:00Z') should parse correctly")
	}
}

func TestDateNowReturnsNumber(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var n = Date.now();
		typeof n === 'number' && n > 0
	`)
	if !result.IsTruthy() {
		t.Error("Date.now() should return a positive number")
	}
}

func TestDateNowIsCurrentTime(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var before = Date.now();
		var now = Date.now();
		var after = Date.now();
		before <= now && now <= after
	`)
	if !result.IsTruthy() {
		t.Error("Date.now() should return monotonically non-decreasing values")
	}
}

func TestDateInvalidStringNaN(t *testing.T) {
	vm := js.NewVM()
	// new Date("invalid") should produce NaN DateValue per ECMAScript §21.4.3
	result := vm.Run(`
		var d = new Date("not a date");
		isNaN(d.getTime()) && isNaN(d.getFullYear()) && isNaN(d.getMonth()) &&
		isNaN(d.getDate()) && isNaN(d.getDay()) && isNaN(d.getHours()) &&
		isNaN(d.getMinutes()) && isNaN(d.getSeconds()) && isNaN(d.getMilliseconds()) &&
		isNaN(d.valueOf()) && d.toString() === "Invalid Date"
	`)
	if !result.IsTruthy() {
		t.Error("new Date('invalid') should produce NaN DateValue with all getters returning NaN")
	}
}

func TestDateWithoutNewReturnsString(t *testing.T) {
	vm := js.NewVM()
	// Date() without new must return a string per ECMAScript §21.4.2
	result := vm.Run(`
		typeof Date() === 'string'
	`)
	if !result.IsTruthy() {
		t.Error("Date() without new should return a string")
	}
}

func TestDateMonthOverflow(t *testing.T) {
	vm := js.NewVM()
	// new Date(2024, 12, 1) should wrap to January 2025
	result := vm.Run(`
		var d = new Date(2024, 12, 1);
		d.getFullYear() === 2025 && d.getMonth() === 0 && d.getDate() === 1
	`)
	if !result.IsTruthy() {
		t.Error("new Date(2024, 12, 1) should wrap to 2025-01-01")
	}
}

func TestDateNegativeMonth(t *testing.T) {
	vm := js.NewVM()
	// new Date(2024, -1, 1) should wrap to December 2023
	result := vm.Run(`
		var d = new Date(2024, -1, 1);
		d.getFullYear() === 2023 && d.getMonth() === 11 && d.getDate() === 1
	`)
	if !result.IsTruthy() {
		t.Error("new Date(2024, -1, 1) should wrap to 2023-12-01")
	}
}
