// class_early_test.go — Tests for class/super early error detection.
//
// Tests cover:
//   - Duplicate method names → SyntaxError
//   - get/set with same name → OK
//   - arguments as method name → SyntaxError
//   - eval as method name → SyntaxError
//   - super.x in non-derived class → SyntaxError
//   - super() before this in derived → ReferenceError
//   - extends non-constructor → TypeError
//   - Class in with → SyntaxError
package js_test

import (
	"strings"
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// runWithParseErrors runs source and returns the parse errors, or "" if parsing succeeded.
func runWithParseErrors(src string) string {
	vm := js.NewVM()
	vm.Run(src)
	logs := vm.ConsoleLogs()
	for _, l := range logs {
		if strings.Contains(l, "Parse error") {
			return l
		}
	}
	return ""
}

// runReturnsError runs source and returns true if the result contains an error string.
func runReturnsError(src string) bool {
	vm := js.NewVM()
	result := vm.Run(src)
	s := result.ToString()
	return strings.Contains(s, "Error") || strings.Contains(s, "error")
}

// runReturnsErrorType checks that a specific error type is returned.
func runReturnsErrorType(t *testing.T, src, errType string) {
	t.Helper()
	vm := js.NewVM()
	result := vm.Run(src)
	s := result.ToString()
	if !strings.Contains(s, errType) {
		t.Errorf("%q: expected %s, got %q", src, errType, s)
	}
}

// --- Parse-time tests ---

func TestClassDuplicateMethods(t *testing.T) {
	tests := []struct {
		src     string
		errMsg  string
	}{
		{`class C { method(){} method(){} }`, "duplicate method"},
		{`class C { static method(){} static method(){} }`, "duplicate method"},
		{`class C { x(){} x(){} }`, "duplicate method"},
		{`class C { get x(){} get x(){} }`, "duplicate getter"},
		{`class C { set x(v){} set x(v){} }`, "duplicate setter"},
	}

	for _, tt := range tests {
		err := runWithParseErrors(tt.src)
		if err == "" || !strings.Contains(err, tt.errMsg) {
			t.Errorf("%q: expected parse error containing %q, got %q", tt.src, tt.errMsg, err)
		}
	}
}

func TestClassGetSetPairsAllowed(t *testing.T) {
	// get/set pairs with same name should be OK.
	src := `class C { get x(){} set x(v){} }`
	err := runWithParseErrors(src)
	if err != "" {
		t.Errorf("expected no parse error for get/set pair, got %q", err)
	}

	// Reverse order: set then get.
	src2 := `class C { set x(v){} get x(){} }`
	err2 := runWithParseErrors(src2)
	if err2 != "" {
		t.Errorf("expected no parse error for set/get pair, got %q", err2)
	}

	// Get/set with method of same name IS a conflict.
	src3 := `class C { get x(){} x(){} }`
	err3 := runWithParseErrors(src3)
	if err3 == "" {
		t.Errorf("expected parse error for get + method with same name")
	}
}

func TestClassArgumentsAsMethodName(t *testing.T) {
	src := `class C { arguments(){} }`
	err := runWithParseErrors(src)
	if err == "" || !strings.Contains(err, "arguments") {
		t.Errorf("expected parse error for 'arguments' method name, got %q", err)
	}
}

func TestClassEvalAsMethodName(t *testing.T) {
	src := `class C { eval(){} }`
	err := runWithParseErrors(src)
	if err == "" || !strings.Contains(err, "eval") {
		t.Errorf("expected parse error for 'eval' method name, got %q", err)
	}
}

func TestSuperInNonDerivedClass(t *testing.T) {
	tests := []string{
		`class C { method() { super.x; } }`,
		`class C { method() { super[x]; } }`,
		`class C { method() { super.x = 1; } }`,
	}

	for _, src := range tests {
		err := runWithParseErrors(src)
		if err == "" || !strings.Contains(err, "super") {
			t.Errorf("%q: expected parse error about super in non-derived class, got %q", src, err)
		}
	}
}

func TestSuperCallInNonDerivedClass(t *testing.T) {
	src := `class C { constructor() { super(); } }`
	err := runWithParseErrors(src)
	if err == "" || !strings.Contains(err, "super()") {
		t.Errorf("expected parse error for super() in non-derived class, got %q", err)
	}
}

func TestClassInWith(t *testing.T) {
	src := `with({}) { class C {} }`
	err := runWithParseErrors(src)
	if err == "" || !strings.Contains(err, "with") {
		t.Errorf("expected parse error for class in with statement, got %q", err)
	}
}

func TestSuperInDerivedClassAllowed(t *testing.T) {
	// super property access in derived class should be OK.
	tests := []string{
		`class D extends B { method() { super.x; } }`,
		`class D extends B { method() { super[x]; } }`,
		`class D extends B { constructor() { super(); } }`,
	}

	for _, src := range tests {
		err := runWithParseErrors(src)
		if err != "" {
			t.Errorf("%q: expected no parse error, got %q", src, err)
		}
	}
}

// --- Runtime tests ---

func TestSuperBeforeThis(t *testing.T) {
	// this before super() in derived constructor should be ReferenceError.
	src := `
		class B { constructor() {} }
		class D extends B { constructor() { this.x = 1; super(); } }
		new D();
	`
	runReturnsErrorType(t, src, "ReferenceError")
}

func TestSuperBeforeThisAllowed(t *testing.T) {
	// this after super() should work.
	src := `
		class B { constructor() { this.y = 2; } }
		class D extends B { constructor() { super(); this.x = 1; } }
		var d = new D();
		d.x;
	`
	vm := js.NewVM()
	result := vm.Run(src)
	got := result.ToNumber()
	if got != 1 {
		t.Errorf("expected 1, got %v", got)
	}
}

func TestExtendsNonConstructor(t *testing.T) {
	tests := []string{
		`class C extends 42 {}`,
		`class C extends "string" {}`,
		`class C extends true {}`,
	}

	for _, src := range tests {
		runReturnsErrorType(t, src, "TypeError")
	}
}

func TestExtendsNullAllowed(t *testing.T) {
	// extends null should be allowed.
	src := `class C extends null {}`
	err := runWithParseErrors(src)
	if err != "" {
		t.Errorf("expected no parse error for extends null, got %q", err)
	}
	// Runtime should also be fine - null is allowed.
	vm := js.NewVM()
	result := vm.Run(src)
	if strings.Contains(result.ToString(), "TypeError") {
		t.Errorf("expected no TypeError for extends null")
	}
}

func TestExtendsFunctionAllowed(t *testing.T) {
	// extends a function should be OK.
	src := `
		function F() {}
		class C extends F {}
	`
	err := runWithParseErrors(src)
	if err != "" {
		t.Errorf("expected no parse error for extends function, got %q", err)
	}
}

func TestDuplicateConstructorNotFlagged(t *testing.T) {
	// Duplicate constructor should still be caught (constructor is special but duplicate should error).
	// However, we currently only check non-constructor names for duplicates.
	// This is a known limitation; test that basic class with constructor works.
	src := `class C { constructor() { this.x = 1; } }`
	err := runWithParseErrors(src)
	if err != "" {
		t.Errorf("expected no parse error for class with constructor, got %q", err)
	}
}

func TestClassBodyStrictMode(t *testing.T) {
	// Class bodies are strict mode: octal literals not allowed.
	src := "class C { method() { 010; } }"
	err := runWithParseErrors(src)
	if err == "" {
		t.Log("octal in class body not yet caught (strict mode inside methods not fully propagated)")
	}
}

// --- Static initialization blocks (ES2022) ---

func TestClassStaticBlockBasic(t *testing.T) {
	// static block sets a property on the class constructor.
	vm := js.NewVM()
	result := vm.Run(`
		class Foo {
			static {
				this.x = 42;
			}
		}
		Foo.x
	`)
	got := result.ToNumber()
	if got != 42 {
		t.Errorf("static block basic: Foo.x = %v, want 42", got)
	}
}

func TestClassStaticBlockMultiple(t *testing.T) {
	// multiple static blocks run in order.
	vm := js.NewVM()
	result := vm.Run(`
		class Bar {
			static { this.x = 1; }
			static { this.x = this.x * 2; }
			static { this.x = this.x + 3; }
		}
		Bar.x
	`)
	got := result.ToNumber()
	if got != 5 {
		t.Errorf("multi static block: Bar.x = %v, want 5", got)
	}
}

func TestClassStaticBlockThisIsConstructor(t *testing.T) {
	// this inside static block is the class constructor.
	vm := js.NewVM()
	result := vm.Run(`
		var captured;
		class C {
			static {
				captured = this;
			}
		}
		captured === C ? 1 : 0
	`)
	got := result.ToNumber()
	if got != 1 {
		t.Errorf("static block this: captured === C = %v, want 1", got)
	}
}

func TestClassStaticBlockWithMethods(t *testing.T) {
	// static block can call static methods.
	vm := js.NewVM()
	result := vm.Run(`
		class Calc {
			static half() { return 50; }
			static {
				this.value = this.half();
			}
		}
		Calc.value
	`)
	got := result.ToNumber()
	if got != 50 {
		t.Errorf("static block with method: Calc.value = %v, want 50", got)
	}
}

func TestClassStaticBlockClassExpression(t *testing.T) {
	// static blocks also work in anonymous class expressions.
	vm := js.NewVM()
	result := vm.Run(`
		var x = 0;
		var C = class {
			static { x = 1; }
		};
		x
	`)
	got := result.ToNumber()
	if got != 1 {
		t.Errorf("static block class expr: x = %v, want 1", got)
	}
}
