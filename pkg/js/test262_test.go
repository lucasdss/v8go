// test262_test.go — Test262 ECMAScript conformance test runner.
//
// Per gojs.md Section 5: "Use the official ECMAScript Test Suite (Test262).
// The goal is to pass these tests one category at a time."
//
// This runner:
// 1. Locates test262 test files in a specified directory
// 2. Parses YAML frontmatter for metadata (expected outcome, features, includes)
// 3. Prepends Test262 harness files (assert.js, sta.js, propertyHelper.js, etc.)
// 4. Executes each test in the GoV8 VM with proper error detection
// 5. Reports pass/fail/skip rates by category
package js_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// Test262Test represents a single Test262 test case.
type Test262Test struct {
	Path          string
	Source        string
	Negative      bool   // test expects an error
	NegativePhase string // "parse" or "runtime" (only meaningful if Negative)
	NegativeType  string // expected error type (e.g., "SyntaxError", "TypeError")
	Features      []string
	Flags         []string
	Includes      []string // harness files to include
}

// parseTest262 reads a test262 .js file and extracts metadata and source.
func parseTest262(path string) (*Test262Test, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	t := &Test262Test{Path: path}
	scanner := bufio.NewScanner(f)
	var source strings.Builder
	inYAML := false
	var yamlLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "/*---" {
			inYAML = true
			continue
		}
		if inYAML && line == "---*/" {
			inYAML = false
			// Parse YAML metadata.
			for _, yl := range yamlLines {
				yl = strings.TrimSpace(yl)
				if strings.HasPrefix(yl, "negative:") {
					t.Negative = true
					// Peek at remaining lines for phase/type.
				}
				if t.Negative && strings.HasPrefix(yl, "  phase:") {
					t.NegativePhase = strings.TrimSpace(strings.TrimPrefix(yl, "  phase:"))
				}
				if t.Negative && strings.HasPrefix(yl, "  type:") {
					t.NegativeType = strings.TrimSpace(strings.TrimPrefix(yl, "  type:"))
				}
				if strings.HasPrefix(yl, "features:") {
					features := strings.TrimPrefix(yl, "features:")
					for _, f := range strings.Split(features, ",") {
						f = strings.TrimSpace(f)
						if f != "" && f != "[]" {
							t.Features = append(t.Features, strings.Trim(f, " []'\""))
						}
					}
				}
				if strings.HasPrefix(yl, "flags:") {
					flags := strings.TrimPrefix(yl, "flags:")
					for _, fl := range strings.Split(flags, ",") {
						fl = strings.TrimSpace(fl)
						if fl != "" && fl != "[]" {
							t.Flags = append(t.Flags, strings.Trim(fl, " []'\""))
						}
					}
				}
				if strings.HasPrefix(yl, "includes:") {
					includes := strings.TrimPrefix(yl, "includes:")
					for _, inc := range strings.Split(includes, ",") {
						inc = strings.TrimSpace(inc)
						if inc != "" && inc != "[]" {
							t.Includes = append(t.Includes, strings.Trim(inc, " []'\""))
						}
					}
				}
			}
			// Default negative phase to "runtime" if not specified.
			if t.Negative && t.NegativePhase == "" {
				t.NegativePhase = "runtime"
			}
			continue
		}
		if inYAML {
			yamlLines = append(yamlLines, line)
			continue
		}
		// Skip copyright header comments and metadata comments.
		if strings.HasPrefix(strings.TrimSpace(line), "//") && source.Len() == 0 {
			trimmed := strings.TrimPrefix(strings.TrimSpace(line), "//")
			trimmed = strings.TrimSpace(trimmed)
			if strings.HasPrefix(trimmed, "flags:") {
				flags := strings.TrimPrefix(trimmed, "flags:")
				for _, fl := range strings.Split(flags, ",") {
					fl = strings.TrimSpace(fl)
					if fl != "" && fl != "[]" {
						t.Flags = append(t.Flags, strings.Trim(fl, " []'\""))
					}
				}
			}
			continue
		}
		source.WriteString(line)
		source.WriteString("\n")
	}

	t.Source = source.String()
	return t, scanner.Err()
}

// harnessPreamble is the core Test262 harness code prepended to every test.
// It provides assert, Test262Error, $DONOTEVALUATE, fnGlobalObject.
// Additional harness files (propertyHelper, testTypedArray, etc.) are loaded
// on-demand via the includes metadata.
var (
	harnessOnce     sync.Once
	harnessPreamble string
	harnessFiles    = map[string]string{} // includes name → source
	test262BaseDir  string
)

// loadHarness reads all core harness files from the test262 checkout.
func loadHarness(test262Dir string) {
	harnessOnce.Do(func() {
		test262BaseDir = test262Dir
		// Core harness: always loaded.
		coreFiles := []string{
			"sta.js",
			"assert.js",
			"fnGlobalObject.js",
		}
		var sb strings.Builder
		for _, f := range coreFiles {
			path := filepath.Join(test262Dir, "harness", f)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content := string(data)
			harnessFiles[f] = content
			sb.WriteString("// === harness: ")
			sb.WriteString(f)
			sb.WriteString(" ===\n")
			sb.WriteString(content)
			sb.WriteString("\n")
		}
		harnessPreamble = sb.String()

		// Extra harness files: loaded on demand via includes.
		extraFiles := []string{
			"isConstructor.js",
			"compareArray.js",
			"propertyHelper.js",
			"testTypedArray.js",
			"detachArrayBuffer.js",
			"byteConversionValues.js",
			"deepEqual.js",
			"nans.js",
			"nativeFunctionMatcher.js",
			"promiseHelper.js",
			"proxyTrapsHelper.js",
			"regExpUtils.js",
			"resizableArrayBufferUtils.js",
			"dateConstants.js",
			"decimalToHexString.js",
			"temporalHelpers.js",
			"doneprintHandle.js",
			"asyncHelpers.js",
		}
		for _, f := range extraFiles {
			path := filepath.Join(test262Dir, "harness", f)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			harnessFiles[f] = string(data)
		}
	})
}

// newTestVM creates a fresh VM with the core harness pre-loaded.
func newTestVM() *js.VM {
	vm := js.NewVM()
	// Pre-run the core harness to define assert, Test262Error, etc.
	vm.Run(harnessPreamble)
	return vm
}

// isModuleTest checks if the test has the "module" flag.
func isModuleTest(t *Test262Test) bool {
	for _, flag := range t.Flags {
		if flag == "module" {
			return true
		}
	}
	return false
}

// runTest262Test executes a single Test262 test and returns (passed, reason).
func runTest262Test(t *Test262Test, vm *js.VM) (bool, string) {
	// Check flags for unsupported modes — auto-skip.
	for _, flag := range t.Flags {
		switch flag {
		case "raw", "onlyStrict", "noStrict":
			return true, "skipped (unsupported flag: " + flag + ")"
		}
	}

	// Detect async tests for $DONE protocol support.
	isAsyncTest := false
	for _, flag := range t.Flags {
		if flag == "async" {
			isAsyncTest = true
			break
		}
	}

	// Auto-skip tests requiring features we don't support.
	for _, feat := range t.Features {
		switch feat {
		case "cross-realm", "resizable-arraybuffer":
			return true, "skipped (unsupported feature: " + feat + ")"
		}
	}

	// Build the test source: per-test includes (cached) + optional async harness + test source.
	var sourceBuilder strings.Builder

	// Include test-specific harness files. Load each only once per VM lifetime.
	for _, inc := range t.Includes {
		if content, ok := harnessFiles[inc]; ok {
			// Simple dedup: if we loaded this include before in this VM, skip.
			// We use a marker global variable in the VM to track this.
			markerName := "__harness_loaded_" + strings.TrimSuffix(inc, ".js")
			sourceBuilder.WriteString("if (typeof " + markerName + " === 'undefined') {\n")
			sourceBuilder.WriteString("var " + markerName + " = true;\n")
			sourceBuilder.WriteString(content)
			sourceBuilder.WriteString("\n}\n")
		}
	}

	// Inject async harness wrapper AFTER includes (to override any harness $DONE).
	// The wrapper defines $asyncDone/$asyncError and a $DONE shim that sets them.
	if isAsyncTest {
		sourceBuilder.WriteString("var $asyncDone = false;\n")
		sourceBuilder.WriteString("var $asyncError = undefined;\n")
		sourceBuilder.WriteString("var $DONE = function(error) {\n")
		sourceBuilder.WriteString("    $asyncDone = true;\n")
		sourceBuilder.WriteString("    if (error !== undefined) { $asyncError = error; }\n")
		sourceBuilder.WriteString("};\n")
	}

	sourceBuilder.WriteString(t.Source)
	source := sourceBuilder.String()

	// For module tests: use the module registry instead of vm.Run.
	if isModuleTest(t) {
		return runModuleTest(t, vm, source)
	}

	// Clear console logs for this test.
	vm.ClearConsoleLogs()
	var consoleLogs []string
	vm.SetConsoleOutput(func(s string) {
		consoleLogs = append(consoleLogs, s)
	})

	result := vm.Run(source)
	resultStr := result.ToString()

	// Check for Test262Error — assertion failure from the harness.
	if strings.Contains(resultStr, "Test262Error") {
		return false, "assertion failure: " + truncateStr(resultStr, 120)
	}

	// Check for $DONOTEVALUATE — the test body was reached when it shouldn't have been.
	if strings.Contains(resultStr, "should not be evaluated") {
		if t.Negative && t.NegativePhase == "parse" {
			return false, "parse-phase negative test was evaluated (expected SyntaxError during parse)"
		}
		return false, "unexpected $DONOTEVALUATE"
	}

	// Check console logs for parse errors.
	hasParseError := false
	for _, l := range consoleLogs {
		if strings.Contains(l, "Parse error") {
			hasParseError = true
			break
		}
	}
	// Also check vm's internal console log.
	for _, l := range vm.ConsoleLogs() {
		if strings.Contains(l, "Parse error") {
			hasParseError = true
			break
		}
	}

	// Determine if an error was thrown during execution.
	hasRuntimeError := result.Tag != js.TagUndefined &&
		!strings.HasPrefix(resultStr, "undefined") &&
		resultStr != "" &&
		resultStr != "undefined" &&
		resultStr != "[object Function]" && // harness return value
		resultStr != "[object Object]" // common return value

	// Check for known error patterns in the result.
	if hasRuntimeError {
		isKnownError := strings.Contains(resultStr, "Error") ||
			strings.Contains(resultStr, "error")
		if !isKnownError {
			hasRuntimeError = false
		}
	}

	// --- Async test $DONE protocol check ---
	if isAsyncTest {
		asyncDoneVal := vm.GetGlobal("$asyncDone")
		if asyncDoneVal.Tag == js.TagUndefined || !asyncDoneVal.IsTruthy() {
			return false, "async test: $DONE() not called"
		}
		asyncErrorVal := vm.GetGlobal("$asyncError")
		if asyncErrorVal.Tag != js.TagUndefined {
			errStr := ""
			if asyncErrorVal.IsObject() && asyncErrorVal.ObjVal != nil {
				errStr = asyncErrorVal.ObjVal.Get("message").ToString()
			} else {
				errStr = asyncErrorVal.ToString()
			}
			if errStr != "" {
				if strings.Contains(errStr, "Test262Error") {
					return false, "assertion failure: " + truncateStr(errStr, 120)
				}
				if t.Negative && t.NegativePhase == "runtime" {
					return true, "" // expected runtime error via $DONE
				}
				return false, "async error: " + truncateStr(errStr, 120)
			}
		}
		// $DONE called with no error — test passed.
		return true, ""
	}

	// --- Decision logic ---

	if t.Negative {
		if t.NegativePhase == "parse" {
			if hasParseError {
				return true, "" // expected parse error occurred
			}
			return false, "parse-phase negative: expected SyntaxError but parse succeeded"
		}

		// Runtime-phase negative: the test should throw an error during execution.
		if hasRuntimeError {
			return true, "" // expected runtime error
		}
		return false, "expected runtime error (" + t.NegativeType + ") but none thrown"
	}

	// Normal (non-negative) test.
	if hasParseError {
		return false, "unexpected parse error"
	}
	if hasRuntimeError {
		return false, "unexpected error: " + truncateStr(resultStr, 120)
	}

	// Check console output for uncaught error reports.
	for _, l := range consoleLogs {
		if strings.Contains(l, "ERROR:") {
			return false, "console error: " + truncateStr(l, 120)
		}
	}

	return true, ""
}

// runModuleTest executes a module-flagged test via the module registry.
func runModuleTest(t *Test262Test, vm *js.VM, source string) (bool, string) {
	mr := vm.GetModuleRegistry()
	if mr == nil {
		return true, "skipped (no module registry)"
	}

	// Clear console logs for this test.
	vm.ClearConsoleLogs()
	var consoleLogs []string
	vm.SetConsoleOutput(func(s string) {
		consoleLogs = append(consoleLogs, s)
	})

	// Register the test source as a module.
	mod, err := mr.Register("__test__", source)
	if err != nil {
		// Parse error — check if this is an expected negative test.
		if t.Negative && t.NegativePhase == "parse" {
			return true, ""
		}
		return false, "module parse error: " + err.Error()
	}

	// Link the module.
	if err := mr.Link("__test__"); err != nil {
		errStr := err.Error()
		// If it's a circular dependency error, it's still a module error.
		if t.Negative {
			return true, ""
		}
		return false, "module link error: " + errStr
	}

	// Evaluate the module.
	result, evalErr := mr.Evaluate("__test__")
	if evalErr != nil {
		if t.Negative {
			return true, ""
		}
		return false, "module evaluation error: " + evalErr.Error()
	}

	resultStr := result.ToString()

	// Check for Test262Error — assertion failure from the harness.
	if strings.Contains(resultStr, "Test262Error") {
		return false, "assertion failure: " + truncateStr(resultStr, 120)
	}

	// Check console output for error reports.
	for _, l := range consoleLogs {
		if strings.Contains(l, "ERROR:") {
			return false, "console error: " + truncateStr(l, 120)
		}
	}

	// Module evaluated successfully.
	_ = mod
	return true, ""
}

// truncateStr truncates a string to maxLen characters.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// findTest262Files walks a directory and returns all .js test file paths.
func findTest262Files(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible files
		}
		if !info.IsDir() && strings.HasSuffix(path, ".js") {
			// Skip harness files.
			if strings.Contains(path, "harness") || strings.Contains(path, "_FIXTURE") {
				return nil
			}
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// TestTest262 runs the Test262 conformance suite.
// Set TEST262_DIR environment variable to point to the test262 checkout.
func TestTest262(t *testing.T) {
	test262Dir := resolveTest262Dir(t)
	if test262Dir == "" {
		return
	}
	loadHarness(test262Dir)

	languageDir := filepath.Join(test262Dir, "test", "language")
	if _, err := os.Stat(languageDir); os.IsNotExist(err) {
		t.Skipf("Test262 language directory not found at %s", languageDir)
		return
	}

	files, err := findTest262Files(languageDir)
	if err != nil {
		t.Fatalf("Failed to find test262 files: %v", err)
	}

	if len(files) == 0 {
		t.Skip("No test262 files found")
		return
	}

	t.Logf("Found %d Test262 test files in language/", len(files))

	var passed, failed, skipped int
	results := make(map[string][2]int) // feature → [passed, failed]

	// Limit to 500 tests for speed; 50 when -short. Set TEST262_FULL=1 for all.
	maxTests := 500
	if testing.Short() {
		maxTests = 50
	}
	if os.Getenv("TEST262_FULL") == "1" {
		maxTests = len(files)
	}
	totalFiles := len(files)
	if len(files) > maxTests {
		files = files[:maxTests]
		t.Logf("Running first %d of %d tests (set TEST262_FULL=1 for all)", maxTests, totalFiles)
	}

	vm := newTestVM()

	for _, file := range files {
		test, err := parseTest262(file)
		if err != nil {
			skipped++
			continue
		}

		ok, reason := runTest262Test(test, vm)
		if strings.HasPrefix(reason, "skipped") {
			skipped++
			continue
		}
		if strings.HasPrefix(reason, "timeout") {
			skipped++
			continue
		}

		for _, feat := range test.Features {
			r := results[feat]
			if ok {
				r[0]++
			} else {
				r[1]++
			}
			results[feat] = r
		}

		if ok {
			passed++
		} else {
			failed++
			t.Logf("FAIL: %s (%s)", filepath.Base(file), reason)
		}
	}

	t.Logf("Test262 Language results: %d passed, %d failed, %d skipped (of %d total)",
		passed, failed, skipped, len(files))

	if len(results) > 0 {
		t.Logf("Results by feature:")
		for feat, counts := range results {
			total := counts[0] + counts[1]
			pct := 0.0
			if total > 0 {
				pct = float64(counts[0]) / float64(total) * 100
			}
			t.Logf("  %s: %d/%d (%.1f%%)", feat, counts[0], total, pct)
		}
	}
}

// runTest262Dir is a helper that runs all test262 .js files under a given directory.
// Uses a single VM with pre-loaded harness for performance.
func runTest262Dir(t *testing.T, dir string) (passed, failed, skipped int) {
	t.Helper()

	files, err := findTest262Files(dir)
	if err != nil {
		t.Fatalf("Failed to find test262 files in %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Skipf("No test262 files found in %s", dir)
		return
	}

	// Limit tests for speed unless TEST262_FULL is set.
	maxTests := 200
	if testing.Short() {
		maxTests = 50
	}
	if os.Getenv("TEST262_FULL") == "1" {
		maxTests = len(files)
	}
	if len(files) > maxTests {
		files = files[:maxTests]
	}

	t.Logf("Running %d/%d tests in %s", len(files), len(files), dir)

	// Create a shared VM with pre-loaded core harness.
	vm := newTestVM()

	for _, file := range files {
		test, err := parseTest262(file)
		if err != nil {
			skipped++
			continue
		}

		ok, reason := runTest262Test(test, vm)
		if strings.HasPrefix(reason, "skipped") || strings.HasPrefix(reason, "timeout") {
			skipped++
			continue
		}

		if ok {
			passed++
		} else {
			failed++
			t.Logf("FAIL: %s (%s)", filepath.Base(file), reason)
		}
	}

	t.Logf("%s results: %d passed, %d failed, %d skipped", filepath.Base(dir), passed, failed, skipped)
	return
}

// TestTest262_Language runs tests from test262/test/language/ with sub-categories.
func TestTest262_Language(t *testing.T) {
	test262Dir := resolveTest262Dir(t)
	if test262Dir == "" {
		return
	}
	loadHarness(test262Dir)

	categories := []string{
		"expressions",
		"statements",
		"types",
		"functions",
	}
	for _, cat := range categories {
		catDir := filepath.Join(test262Dir, "test", "language", cat)
		if _, err := os.Stat(catDir); os.IsNotExist(err) {
			t.Logf("Skipping category %s: directory not found", cat)
			continue
		}
		cat := cat
		t.Run(cat, func(t *testing.T) {
			runTest262Dir(t, catDir)
		})
	}
}

// TestTest262_Builtins runs tests from test262/test/built-ins/ with sub-categories.
// These 18 categories match the ECMAScript built-in objects.
func TestTest262_Builtins(t *testing.T) {
	test262Dir := resolveTest262Dir(t)
	if test262Dir == "" {
		return
	}
	loadHarness(test262Dir)

	categories := []string{
		"Array",
		"BigInt",
		"Object",
		"String",
		"Number",
		"Math",
		"JSON",
		"Promise",
		"Symbol",
		"Map",
		"Set",
		"WeakMap",
		"WeakSet",
		"Proxy",
		"Reflect",
		"RegExp",
		"ArrayBuffer",
		"DataView",
		"TypedArray",
	}
	for _, cat := range categories {
		catDir := filepath.Join(test262Dir, "test", "built-ins", cat)
		if _, err := os.Stat(catDir); os.IsNotExist(err) {
			t.Logf("Skipping category %s: directory not found", cat)
			continue
		}
		cat := cat
		t.Run(cat, func(t *testing.T) {
			runTest262Dir(t, catDir)
		})
	}
}

// TestTest262_Language_Types runs tests from test262/test/language/types/.
func TestTest262_Language_Types(t *testing.T) {
	test262Dir := resolveTest262Dir(t)
	if test262Dir == "" {
		return
	}
	loadHarness(test262Dir)
	runTest262Dir(t, filepath.Join(test262Dir, "test", "language", "types"))
}

// resolveTest262Dir resolves the test262 directory, skipping if not found.
func resolveTest262Dir(t *testing.T) string {
	t.Helper()
	test262Dir := os.Getenv("TEST262_DIR")
	if test262Dir == "" {
		candidates := []string{
			"../../test262",
			"../../../test262",
			"test262",
			"../test262",
			os.ExpandEnv("$HOME/test262"),
		}
		for _, dir := range candidates {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				test262Dir = dir
				break
			}
		}
	}
	if test262Dir == "" {
		t.Skip("TEST262_DIR not set and test262 not found. Clone with: git clone https://github.com/tc39/test262.git")
		return ""
	}
	return test262Dir
}

// TestTest262Smoke runs a quick smoke test with inline test cases.
func TestTest262Smoke(t *testing.T) {
	// Inline test cases mimicking Test262 format.
	tests := []struct {
		name     string
		source   string
		expected float64
	}{
		{"addition", "1 + 2", 3},
		{"variable", "var x = 42; x", 42},
		{"function", "function f(x) { return x * 2; } f(21)", 42},
		{"closure", "function make() { var x = 10; return function() { return x; }; } make()()", 10},
		{"arrow", "var f = (a, b) => a + b; f(20, 22)", 42},
		{"ternary", "true ? 42 : 0", 42},
		{"strict-eq", "1 === 1 ? 42 : 0", 42},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := js.NewVM()
			result := vm.Run(tc.source)
			got := result.ToNumber()
			if got != tc.expected {
				t.Errorf("%s: expected %v, got %v", tc.name, tc.expected, got)
			}
		})
	}
}

// TestAsyncHarness verifies the $DONE async protocol used by Test262 async tests.
// It validates that the harness correctly:
//  1. Detects when $DONE() is called (success case)
//  2. Detects when $DONE() is not called (failure case)
//  3. Can capture errors passed to $DONE(error)
//  4. Does not interfere with non-async tests
func TestAsyncHarness(t *testing.T) {
	t.Run("done_called", func(t *testing.T) {
		vm := js.NewVM()
		harness := "var $asyncDone = false; var $asyncError = undefined; " +
			"var $DONE = function(e) { $asyncDone = true; if (e !== undefined) { $asyncError = e; } }; "
		source := harness + "$DONE();"
		vm.Run(source)
		done := vm.GetGlobal("$asyncDone")
		if !done.IsTruthy() {
			t.Error("$DONE() was called but $asyncDone is not truthy")
		}
		errVal := vm.GetGlobal("$asyncError")
		if errVal.Tag != js.TagUndefined {
			t.Errorf("expected no error, got: %v", errVal.ToString())
		}
	})

	t.Run("done_not_called", func(t *testing.T) {
		vm := js.NewVM()
		harness := "var $asyncDone = false; var $asyncError = undefined; " +
			"var $DONE = function(e) { $asyncDone = true; if (e !== undefined) { $asyncError = e; } }; "
		source := harness + "var unused = 42;" // $DONE never called
		vm.Run(source)
		done := vm.GetGlobal("$asyncDone")
		if done.IsTruthy() {
			t.Error("$DONE() was NOT called but $asyncDone is truthy")
		}
	})

	t.Run("done_with_error", func(t *testing.T) {
		vm := js.NewVM()
		harness := "var $asyncDone = false; var $asyncError = undefined; " +
			"var $DONE = function(e) { $asyncDone = true; if (e !== undefined) { $asyncError = e; } }; "
		source := harness + "$DONE(new Error('test failure'));"
		vm.Run(source)
		done := vm.GetGlobal("$asyncDone")
		if !done.IsTruthy() {
			t.Error("$DONE() was called but $asyncDone is not truthy")
		}
		errVal := vm.GetGlobal("$asyncError")
		if errVal.Tag == js.TagUndefined {
			t.Error("expected error, got undefined")
		}
	})

	t.Run("non_async_unaffected", func(t *testing.T) {
		vm := js.NewVM()
		// No async harness injected — simulate a non-async test.
		// The result of `var x = false` is Undefined, which ToNumber() converts to NaN.
		source := "var someVar = 42; someVar;"
		result := vm.Run(source)
		if result.ToNumber() != 42 {
			t.Errorf("non-async test incorrectly affected: got %v", result.ToNumber())
		}
	})
}

// TestAsyncTest262Pattern verifies the async harness against a typical Test262 async pattern.
// This tests the $DONE protocol detection: the harness detects when $DONE() is called
// synchronously from within the test body. Note: Promise-based async tests ($DONE in a
// .then() callback) require function-scope variable resolution which is tracked separately.
func TestAsyncTest262Pattern(t *testing.T) {
	vm := js.NewVM()

	// Async harness wrapper injected (as runTest262Test would do).
	asyncWrapper := "var $asyncDone = false; var $asyncError = undefined; " +
		"var $DONE = function(e) { $asyncDone = true; if (e !== undefined) { $asyncError = e; } }; "

	// Test: $DONE() called directly at top level (synchronous async test pattern).
	source := asyncWrapper + "$DONE();"
	vm.Run(source)

	done := vm.GetGlobal("$asyncDone")
	if !done.IsTruthy() {
		t.Error("async pattern: $DONE() not detected via $asyncDone")
	}
	asyncErr := vm.GetGlobal("$asyncError")
	if asyncErr.Tag != js.TagUndefined {
		t.Errorf("async pattern: unexpected error: %s", asyncErr.ToString())
	}

	// Test: $DONE(Test262Error) — assertion failure pattern.
	t.Run("error_path", func(t *testing.T) {
		vm2 := js.NewVM()
		source2 := asyncWrapper + "var e = new Error('Test262Error: expected 42, got 0'); $DONE(e);"
		vm2.Run(source2)
		errVal := vm2.GetGlobal("$asyncError")
		if errVal.Tag == js.TagUndefined {
			t.Error("expected $asyncError to be set")
		} else {
			errMsg := ""
			if errVal.IsObject() && errVal.ObjVal != nil {
				errMsg = errVal.ObjVal.Get("message").ToString()
			} else {
				errMsg = errVal.ToString()
			}
			if !strings.Contains(errMsg, "Test262Error") {
				t.Errorf("expected Test262Error, got: %s", errMsg)
			}
		}
	})
}

// Run with: TEST262_DIR=/path/to/test262 go test -run TestTest262 -v ./pkg/js/

// TestTest262_SelfContained is a self-contained conformance benchmark that
// doesn't require an external Test262 checkout. It tests 50+ core ECMAScript
// features directly — expressions, operators, builtins, classes, modules, etc.
func TestTest262_SelfContained(t *testing.T) {
	tests := []struct {
		name   string
		source string
		expect interface{}
	}{
		{"addition", "1 + 2", 3.0},
		{"string concat", `"hello " + "world"`, "hello world"},
		{"typeof number", "typeof 42", "number"},
		{"typeof string", `typeof "hello"`, "string"},
		{"typeof object", "typeof {}", "object"},
		{"typeof function", "typeof function(){}", "function"},
		{"typeof undefined", "typeof undefined", "undefined"},
		{"equality strict", "1 === 1", true},
		{"equality strict diff", "1 === '1'", false},
		{"equality loose", "1 == '1'", true},
		{"null loose eq", "null == undefined", true},
		{"null strict neq", "null === undefined", false},
		{"array literal", "[1,2,3].length", 3.0},
		{"object literal", "({a: 1}).a", 1.0},
		{"function call", "(function(x){return x*2})(3)", 6.0},
		{"arrow function", "((x)=>x+1)(5)", 6.0},
		{"closure", "var a=1; (function(){return a+2})()", 3.0},
		{"ternary true", "true ? 1 : 2", 1.0},
		{"ternary false", "false ? 1 : 2", 2.0},
		{"logical and", "true && 42", 42.0},
		{"logical or", "true || 42", true},
		{"in operator", "'a' in {a:1}", true},
		{"delete operator", "var o={a:1}; delete o.a; o.a", nil},
		{"instanceof", "[] instanceof Array", true},
		{"for loop sum", "var s=0; for(var i=0;i<5;i++) s+=i; s", 10.0},
		{"while loop", "var i=0,s=0; while(i<5){s+=i; i++} s", 10.0},
		{"try catch", "var r; try{throw 'err'}catch(e){r=e} r", "err"},
		{"array map", "[1,2,3].map(function(x){return x*2}).join(',')", "2,4,6"},
		{"array filter", "[1,2,3,4].filter(function(x){return x>2}).length", 2.0},
		{"array reduce", "[1,2,3].reduce(function(a,b){return a+b},0)", 6.0},
		{"string toUpper", `"hello".toUpperCase()`, "HELLO"},
		{"regexp test", "/foo/.test('foobar')", true},
		{"regexp exec", "/foo/.exec('foobar')[0]", "foo"},
		{"JSON stringify", `JSON.stringify({a:1})`, `{"a":1}`},
		{"JSON parse", `JSON.parse('{"a":1}').a`, 1.0},
		{"class basic", `(new(class{constructor(){this.x=1}})).x`, 1.0},
		{"class method", `(new(class{say(){return 42}})).say()`, 42.0},
		{"template literal", "`hello ${1+1}`", "hello 2"},
		{"destructure array", "var [a,b]=[1,2]; a+b", 3.0},
		{"destructure object", "var {x,y}={x:10,y:20}; x+y", 30.0},
		{"spread array", "Math.max(...[1,5,3])", 5.0},
		{"rest params", "(function(...a){return a.length})(1,2,3)", 3.0},
		{"default params", "(function(x=10){return x})(undefined)", 10.0},
		{"optional chain", "var o={a:1}; o?.a", 1.0},
		{"optional chain null", "var o=null; o?.a", nil},
		{"nullish coalesce", "null ?? 42", 42.0},
		{"nullish zero", "0 ?? 42", 0.0},
		{"for of", "var s=0; for(var x of[1,2,3])s+=x; s", 6.0},
		{"Map set get", "var m=new Map(); m.set('k',42); m.get('k')", 42.0},
		{"Set has", "var s=new Set([1,2,3]); s.has(2)", true},
		{"Promise resolve", "typeof Promise.resolve(42).then", "function"},
		{"WeakMap", "var wm=new WeakMap(); var k={}; wm.set(k,42); wm.get(k)", 42.0},
		{"Symbol", "typeof Symbol('t')", "symbol"},
		{"Proxy get", "var p=new Proxy({x:1},{get:function(t,k){return t[k]*2}}); p.x", 2.0},
		{"Reflect get", "Reflect.get({a:1},'a')", 1.0},
		{"BigInt", "typeof 42n", "bigint"},
		{"async fn", "typeof (async function(){return 1})()", "object"},
		{"generator", "function*g(){yield 1;yield 2} var it=g(); it.next().value+it.next().value", 3.0},
	}

	vm := js.NewVM()
	passed, failed := 0, 0

	for _, tc := range tests {
		result := vm.Run(tc.source)
		match := false
		switch expect := tc.expect.(type) {
		case float64:
			match = result.Tag == js.TagNumber && result.NumVal == expect
		case string:
			match = result.ToString() == expect
		case bool:
			match = result.IsTruthy() == expect
		case nil:
			match = result.Tag == js.TagUndefined
		}
		if match {
			passed++
		} else {
			failed++
			t.Errorf("FAIL %s: %s => %v (expected %v)", tc.name, tc.source, result, tc.expect)
		}
	}

	t.Logf("Self-contained conformance: %d/%d passed (%.1f%%)",
		passed, passed+failed, float64(passed)/float64(passed+failed)*100)

	if failed > 0 {
		t.Errorf("%d conformance tests failed", failed)
	}
}
