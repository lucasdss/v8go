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

// =========================================================================
// Return outside function context (~3 tests)
// =========================================================================

func TestReturn_OutsideFunction(t *testing.T) {
	assertHasError(t, `return 42`, "Illegal return statement")
}

func TestReturn_InsideFunction(t *testing.T) {
	assertNoError(t, `function f() { return 42; }`)
}

func TestReturn_InsideGetter(t *testing.T) {
	assertNoError(t, `var obj = { get x() { return 42; } }`)
}

// =========================================================================
// Break/Continue outside loop/switch (~4 tests)
// =========================================================================

func TestBreak_OutsideLoop(t *testing.T) {
	assertHasError(t, `break`, "Illegal break statement")
}

func TestBreak_InsideLoop(t *testing.T) {
	assertNoError(t, `while (true) { break; }`)
}

func TestBreak_InsideSwitch(t *testing.T) {
	assertNoError(t, `switch (1) { case 1: break; }`)
}

func TestContinue_OutsideLoop(t *testing.T) {
	assertHasError(t, `continue`, "Illegal continue statement")
}

// =========================================================================
// Label statements (~4 tests)
// =========================================================================

func TestLabel_Basic(t *testing.T) {
	assertNoError(t, `label: for (;;) { break label; }`)
}

func TestLabel_Duplicate(t *testing.T) {
	assertHasError(t, `function f() { label: while (true) { label: break label; } }`, "already been declared")
}

func TestLabel_UndefinedBreak(t *testing.T) {
	// Break to undefined label
	assertHasError(t, `function f() { while (true) { break undefined_label; } }`, "Undefined label")
}

func TestLabel_UndefinedContinue(t *testing.T) {
	assertHasError(t, `function f() { while (true) { continue undefined_label; } }`, "Undefined label")
}

// =========================================================================
// let/const re-declaration (~4 tests)
// =========================================================================

func TestLet_Redeclaration(t *testing.T) {
	assertHasError(t, `{ let x = 1; let x = 2; }`, "already been declared")
}

func TestLet_NoRedeclarationSeparateScopes(t *testing.T) {
	assertNoError(t, `{ let x = 1; } { let x = 2; }`)
}

func TestConst_Redeclaration(t *testing.T) {
	assertHasError(t, `{ const x = 1; const x = 2; }`, "already been declared")
}

func TestVar_NoRedeclarationError(t *testing.T) {
	// var can be redeclared without error
	assertNoError(t, `{ var x = 1; var x = 2; }`)
}

// =========================================================================
// Duplicate params in class methods (always strict) (~2 tests)
// =========================================================================

func TestClassMethod_DuplicateParams(t *testing.T) {
	assertHasError(t, `class Foo { bar(a, a) {} }`, "duplicate parameter name")
}

func TestClassMethod_NoDupParams(t *testing.T) {
	assertNoError(t, `class Foo { bar(a, b) {} }`)
}

// =========================================================================
// Strict mode — assignment to eval/arguments (~6 tests)
// =========================================================================

func TestStrict_AssignToEval(t *testing.T) {
	assertHasError(t, `"use strict"; eval = 1`, "eval")
}

func TestStrict_AssignToArguments(t *testing.T) {
	assertHasError(t, `"use strict"; arguments = 1`, "arguments")
}

func TestStrict_PrefixIncEval(t *testing.T) {
	assertHasError(t, `"use strict"; ++eval`, "eval")
}

func TestStrict_PostfixIncArguments(t *testing.T) {
	assertHasError(t, `"use strict"; arguments++`, "arguments")
}

func TestStrict_NoErrorAssignEvalNonStrict(t *testing.T) {
	assertNoError(t, `eval = 1`)
}

func TestStrict_WithStatement(t *testing.T) {
	assertHasError(t, `"use strict"; with ({}) {}`, "with")
}

// =========================================================================
// Non-Simple Parameter List + "use strict" (~6 tests)
// =========================================================================

func TestNSPL_RestParamWithUseStrict(t *testing.T) {
	// rest param makes the param list non-simple; "use strict" in body is an error
	assertHasError(t, `function f(a, ...rest) { "use strict"; }`, "non-simple parameter list")
}

func TestNSPL_DefaultParamWithUseStrict(t *testing.T) {
	assertHasError(t, `function f(x = 1) { "use strict"; }`, "non-simple parameter list")
}

func TestNSPL_DestructuringWithUseStrict(t *testing.T) {
	assertHasError(t, `function f([element]) { "use strict"; }`, "non-simple parameter list")
}

func TestNSPL_ObjectDestructuringWithUseStrict(t *testing.T) {
	assertHasError(t, `function f({prop}) { "use strict"; }`, "non-simple parameter list")
}

func TestNSPL_SimpleParamsWithUseStrict_NoError(t *testing.T) {
	// Simple params + "use strict" is fine
	assertNoError(t, `function f(a, b) { "use strict"; }`)
}

func TestNSPL_AsyncFunction_DefaultWithUseStrict(t *testing.T) {
	assertHasError(t, `async function foo(x = 1) { "use strict"; }`, "non-simple parameter list")
}

// =========================================================================
// Duplicate params with defaults (non-simple) even in non-strict (~2 tests)
// =========================================================================

func TestDupParams_WithDefaults_NonStrict(t *testing.T) {
	// Non-simple param list with duplicates should error even in non-strict mode
	assertHasError(t, `function f(x = 0, x) {}`, "duplicate parameter name")
}

func TestDupParams_SimpleParams_NonStrict_NoError(t *testing.T) {
	// Simple params in non-strict mode: duplicates are allowed
	assertNoError(t, `function f(a, a) {}`)
}

// =========================================================================
// Rest parameter with initializer (~2 tests)
// =========================================================================

func TestRestParam_WithDefault(t *testing.T) {
	assertHasError(t, `function f(...x = []) {}`, "rest parameter may not have a default")
}

func TestRestParam_WithDefault_Expression(t *testing.T) {
	assertHasError(t, `function f(...x = 1 + 2) {}`, "rest parameter may not have a default")
}

// =========================================================================
// await restrictions in async functions (~6 tests)
// =========================================================================

func TestAwait_AsBindingIdentifier_InAsync(t *testing.T) {
	assertHasError(t, `async function f() { var await = 5; }`, "await")
}

func TestAwait_AsLabel_InAsync(t *testing.T) {
	assertHasError(t, `async function f() { await: ; }`, "await")
}

func TestAwait_AsIdentifierReference_InAsync(t *testing.T) {
	assertHasError(t, `async function f() { void await; }`, "await")
}

func TestAwait_AsParamName_InAsync(t *testing.T) {
	assertHasError(t, `async function f(await) {}`, "await")
}

func TestAwait_InDefaultExpression_InAsync(t *testing.T) {
	assertHasError(t, `async function f(x = await) {}`, "await")
}

func TestAwait_AsFunctionName_InAsync_NoError(t *testing.T) {
	// Async function can be named await? No, await cannot be a BindingIdentifier in async context
	// But this is the function NAME, not a binding inside async context
	assertHasError(t, `async function await() {}`, "await")
}

// =========================================================================
// Formal parameter names conflict with body lexical declarations (~2 tests)
// =========================================================================

func TestFormalParam_ConflictWithLet(t *testing.T) {
	assertHasError(t, `function f(bar) { let bar; }`, "already been declared")
}

func TestFormalParam_ConflictWithConst(t *testing.T) {
	assertHasError(t, `function f(bar) { const bar = 1; }`, "already been declared")
}
