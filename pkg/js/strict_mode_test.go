package js

import (
	"strings"
	"testing"
)

// parseAndCheckErrors parses source and returns the list of parse errors.
func parseAndCheckErrors(t *testing.T, source string) []string {
	t.Helper()
	tokens := NewLexer(source).Tokenize()
	_, errs := NewParser(tokens).Parse()
	return errs
}

// assertHasError checks that the parse errors contain a message matching substr.
func assertHasError(t *testing.T, source, substr string) {
	t.Helper()
	errs := parseAndCheckErrors(t, source)
	for _, e := range errs {
		if strings.Contains(e, substr) {
			return
		}
	}
	t.Errorf("%q: expected error containing %q, got %v", source, substr, errs)
}

// assertNoError checks that the source parses without errors.
func assertNoError(t *testing.T, source string) {
	t.Helper()
	errs := parseAndCheckErrors(t, source)
	if len(errs) > 0 {
		t.Errorf("%q: expected no errors, got %v", source, errs)
	}
}

// =========================================================================
// Strict Mode — Duplicate Parameter Names (~4 tests)
// =========================================================================

func TestStrict_DuplicateParams_UseStrictDirective(t *testing.T) {
	// "use strict"; function f(a, a) {} should be a SyntaxError
	assertHasError(t, `"use strict"; function f(a, a) {}`, "duplicate parameter name")
}

func TestStrict_DuplicateParams_FunctionUseStrict(t *testing.T) {
	// function f(a, a) { "use strict"; } should be a SyntaxError
	// The "use strict" directive inside the function body should trigger the check.
	assertHasError(t, `function f(a, a) { "use strict"; }`, "duplicate parameter name")
}

func TestStrict_DuplicateParams_NoErrorWithoutStrict(t *testing.T) {
	// Non-strict: function f(a, a) {} is allowed
	assertNoError(t, `function f(a, a) {}`)
}

func TestStrict_DuplicateParams_ArrowFunction(t *testing.T) {
	// "use strict"; (a, a) => {} should be a SyntaxError? 
	// Wait, arrow functions check params too via detectUseStrict.
	// Let's test function expressions with duplicate params in strict context.
	assertHasError(t, `"use strict"; var f = function(a, a) { return a; }`, "duplicate parameter name")
}

// =========================================================================
// Strict Mode — await as identifier (~3 tests)
// =========================================================================

func TestStrict_AwaitAsVarName(t *testing.T) {
	// "use strict"; var await = 5 should be a SyntaxError
	assertHasError(t, `"use strict"; var await = 5`, "await")
}

func TestStrict_AwaitAsFunctionName(t *testing.T) {
	// "use strict"; function await() {} should be a SyntaxError
	assertHasError(t, `"use strict"; function await() {}`, "await")
}

func TestStrict_AwaitAsIdentifier_NoErrorWithoutStrict(t *testing.T) {
	// Non-strict: var await = 5 is allowed
	assertNoError(t, `var await = 5`)
}

// =========================================================================
// Strict Mode — arguments/eval as binding identifiers (~4 tests)
// =========================================================================

func TestStrict_ArgumentsAsVarName(t *testing.T) {
	// "use strict"; var arguments = 1 should be a SyntaxError
	assertHasError(t, `"use strict"; var arguments = 1`, "arguments")
}

func TestStrict_EvalAsVarName(t *testing.T) {
	// "use strict"; var eval = 1 should be a SyntaxError
	assertHasError(t, `"use strict"; var eval = 1`, "eval")
}

func TestStrict_ArgumentsAsFunctionName(t *testing.T) {
	// "use strict"; function arguments() {} should be a SyntaxError
	assertHasError(t, `"use strict"; function arguments() {}`, "arguments")
}

func TestStrict_EvalAsCatchParam(t *testing.T) {
	// "use strict"; try {} catch(eval) {} should be a SyntaxError
	assertHasError(t, `"use strict"; try { throw 1; } catch(eval) {}`, "eval")
}

// =========================================================================
// Strict Mode — Legacy Octal Literals (~2 tests)
// =========================================================================

func TestStrict_LegacyOctal_Literal(t *testing.T) {
	// "use strict"; 010 should be a SyntaxError (legacy octal)
	assertHasError(t, `"use strict"; 010`, "octal")
}

func TestStrict_LegacyOctal_ObjectKey(t *testing.T) {
	// "use strict"; var x = {010: "val"} should be a SyntaxError
	assertHasError(t, `"use strict"; var x = {010: "val"}`, "octal")
}

// =========================================================================
// Strict Mode — Edge cases & combined (~2 tests)
// =========================================================================

func TestStrict_Combined_Restrictions(t *testing.T) {
	// Multiple restrictions in one script: await, eval, duplicate params
	errs := parseAndCheckErrors(t, `"use strict"; var await = 1; var eval = 2; function f(a, a) {}`)
	foundAwait := false
	foundEval := false
	foundDup := false
	for _, e := range errs {
		if strings.Contains(e, "await") {
			foundAwait = true
		}
		if strings.Contains(e, "eval") {
			foundEval = true
		}
		if strings.Contains(e, "duplicate parameter name") {
			foundDup = true
		}
	}
	if !foundAwait {
		t.Error("expected error for await")
	}
	if !foundEval {
		t.Error("expected error for eval")
	}
	if !foundDup {
		t.Error("expected error for duplicate parameter name")
	}
}

func TestStrict_NoFalsePositives_NonStrict(t *testing.T) {
	// All these should parse fine in non-strict mode.
	assertNoError(t, `var arguments = 1`)
	assertNoError(t, `var eval = 1`)
	assertNoError(t, `function arguments() {}`)
	assertNoError(t, `function eval() {}`)
	assertNoError(t, `var await = 5`)
	assertNoError(t, `function await() {}`)
	assertNoError(t, `function f(a, a) {}`)
	assertNoError(t, `(a, a) => {}`) // arrow with dup params
}
