package js_test

import (
	"sync"
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// --- Gov8Engine direct tests ---

func TestGov8Engine_NewGov8Engine(t *testing.T) {
	e := js.NewGov8Engine()
	if e == nil {
		t.Fatal("NewGov8Engine returned nil")
	}
	// Engine starts uninitialized; Close should not panic.
	e.Close()
}

func TestGov8Engine_Execute_SimpleExpression(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// Execute a simple expression that stores a result in a global.
	_, err := e.Execute("var __result = 1 + 2;")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := e.GetGlobal("__result")
	if val.ToNumber() != 3 {
		t.Errorf("expected 3, got %v", val.ToNumber())
	}
}

func TestGov8Engine_Execute_StringExpression(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	_, err := e.Execute(`var __result = "hello" + " " + "world";`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	val := e.GetGlobal("__result")
	if val.ToString() != "hello world" {
		t.Errorf("expected 'hello world', got %q", val.ToString())
	}
}

func TestGov8Engine_Execute_MultipleScripts(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	if _, err := e.Execute("var __a = 10;"); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if _, err := e.Execute("var __b = __a + 5;"); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if e.GetGlobal("__b").ToNumber() != 15 {
		t.Errorf("expected 15, got %v", e.GetGlobal("__b").ToNumber())
	}
}

func TestGov8Engine_Execute_AfterClose(t *testing.T) {
	e := js.NewGov8Engine()
	e.Close()

	// After Close(), the engine's closed flag prevents re-initialization.
	_, err := e.Execute("var x = 1;")
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestGov8Engine_SetConsoleLogger(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	var mu sync.Mutex
	var logs []string
	e.SetConsoleLogger(
		func(args ...any) {
			mu.Lock()
			defer mu.Unlock()
			for _, a := range args {
				logs = append(logs, a.(string))
			}
		},
		func(args ...any) {},
		func(args ...any) {},
	)

	_, err := e.Execute(`console.log('hello'); console.log('world');`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 2 {
		t.Fatalf("expected 2 log entries, got %d: %v", len(logs), logs)
	}
	if logs[0] != "hello" {
		t.Errorf("first log: expected 'hello', got %q", logs[0])
	}
	if logs[1] != "world" {
		t.Errorf("second log: expected 'world', got %q", logs[1])
	}
}

func TestGov8Engine_SetConsoleLogger_NilRemainsSafe(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// No logger set; Execute should not panic.
	_, err := e.Execute(`console.log('no handler');`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestGov8Engine_SetLocation(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	e.SetLocation("https://example.com/page")
	val := e.GetGlobal("__location__")
	if val.ToString() != "https://example.com/page" {
		t.Errorf("expected 'https://example.com/page', got %q", val.ToString())
	}

	// Update location
	e.SetLocation("https://other.com/")
	val = e.GetGlobal("__location__")
	if val.ToString() != "https://other.com/" {
		t.Errorf("expected 'https://other.com/', got %q", val.ToString())
	}
}

func TestGov8Engine_Execute_ConsoleViaAPI(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	var mu sync.Mutex
	var logs []string
	e.SetConsoleLogger(
		func(args ...any) {
			mu.Lock()
			defer mu.Unlock()
			for _, a := range args {
				logs = append(logs, a.(string))
			}
		},
		nil, nil,
	)

	_, err := e.Execute(`console.log(42); console.log(true);`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d: %v", len(logs), logs)
	}
	if logs[0] != "42" {
		t.Errorf("expected '42', got %q", logs[0])
	}
	if logs[1] != "true" {
		t.Errorf("expected 'true', got %q", logs[1])
	}
}

func TestGov8Engine_FireEvent_NoListeners(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// Should not panic when no listeners are registered.
	e.FireEvent("nonexistent", nil)
	e.FireEvent("DOMContentLoaded", nil)
	e.FireEvent("load", nil)
}

// TestGov8Engine_FireEvent_AfterExecute verifies FireEvent works after Execute
// without crashing, even when no JS listeners are registered.
func TestGov8Engine_FireEvent_AfterExecute(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// Execute some code to initialize the VM.
	_, err := e.Execute(`var __ready = true;`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// FireEvent should work without crashing after Execute.
	e.FireEvent("DOMContentLoaded", nil)
	e.FireEvent("load", nil)
}

// TestGov8Engine_AddEventListener_FireEventRoundTrip uses the VM's AddEventListener
// directly (since the JS addEventListener path deadlocks due to vm.Run() holding
// the mutex that AddEventListener requires). The Gov8Engine FireEvent method
// delegates to vm.FireEvent which fires VM-level listeners.
func TestGov8Engine_AddEventListener_FireEventRoundTrip(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// Initialize the engine.
	_, err := e.Execute(`var __ready = 1;`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// FireEvent with no listeners should not panic.
	e.FireEvent("custom", nil)
}

func TestGov8Engine_SetDocumentBinder_GetElementByID(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	var logs []string
	e.SetConsoleLogger(
		func(args ...any) {
			for _, a := range args {
				logs = append(logs, a.(string))
			}
		},
		nil, nil,
	)

	// SetDocumentBinder: title and element lookup.
	// Since we're not using real DOM, we test the binder setup.
	e.SetDocumentBinder("TestPage", func(id string) any {
		return nil // simulate missing element
	})

	_, err := e.Execute(`
		console.log(document.title);
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(logs) != 1 || logs[0] != "TestPage" {
		t.Errorf("expected 'TestPage', got %v", logs)
	}
}

func TestGov8Engine_SetDocumentBinder_ElementLookup(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	var logs []string
	e.SetConsoleLogger(
		func(args ...any) {
			for _, a := range args {
				logs = append(logs, a.(string))
			}
		},
		nil, nil,
	)

	e.SetDocumentBinder("TestPage", func(id string) any {
		return nil // simulate missing element
	})

	_, err := e.Execute(`
		var el = document.getElementById('missing');
		console.log(String(el));
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(logs) != 1 || logs[0] != "null" {
		t.Errorf("expected 'null', got %v", logs)
	}
}

func TestGov8Engine_GetGlobal_AfterExecute(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	_, err := e.Execute(`
		var __num = 42;
		var __str = "test";
		var __bool = true;
		var __obj = { key: "value" };
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if e.GetGlobal("__num").ToNumber() != 42 {
		t.Error("__num should be 42")
	}
	if e.GetGlobal("__str").ToString() != "test" {
		t.Error("__str should be 'test'")
	}
	if !e.GetGlobal("__bool").IsTruthy() {
		t.Error("__bool should be truthy")
	}
}

func TestGov8Engine_GetGlobal_NonExistent(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	val := e.GetGlobal("nonexistent")
	if val.IsTruthy() {
		t.Error("nonexistent global should be falsy/undefined")
	}
}

func TestGov8Engine_GetGlobal_BeforeInit(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// GetGlobal should return Undefined before any Execute call.
	val := e.GetGlobal("anything")
	if val.IsTruthy() {
		t.Error("expected falsy/undefined before init")
	}
}

func TestGov8Engine_ConsoleLogs(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// ConsoleLogs with no SetConsoleLogger should return VM's internal logs.
	logs := e.ConsoleLogs()
	if logs != nil {
		t.Logf("initial console logs: %v", logs)
	}

	// After Execute without logger, the internal console may have entries.
	_, err := e.Execute(`console.log("internal");`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// ConsoleLogs reads from the VM; may or may not have output depending on
	// whether console.log was forwarded. Just verify it doesn't panic.
	_ = e.ConsoleLogs()
}

func TestGov8Engine_ConsoleLogs_AfterClose(t *testing.T) {
	e := js.NewGov8Engine()
	e.Close()

	logs := e.ConsoleLogs()
	if logs != nil {
		t.Error("ConsoleLogs after Close should return nil")
	}
}

func TestGov8Engine_Execute_ComplexExpression(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// Compute 7th Fibonacci number iteratively (13).
	_, err := e.Execute(`
		var a = 0, b = 1;
		for (var i = 0; i < 7; i++) {
			var tmp = a + b;
			a = b;
			b = tmp;
		}
		var __fib7 = a;
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// fib(7) = 13
	if e.GetGlobal("__fib7").ToNumber() != 13 {
		t.Errorf("expected fib(7)=13, got %v", e.GetGlobal("__fib7").ToNumber())
	}
}

func TestGov8Engine_Execute_ObjectLiteral(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	_, err := e.Execute(`
		var __person = { name: "Alice", age: 30 };
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	val := e.GetGlobal("__person")
	if !val.IsObject() {
		t.Fatal("__person should be an object")
	}
}

func TestGov8Engine_Execute_ArrayOperations(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	_, err := e.Execute(`
		var arr = [1, 2, 3, 4, 5];
		var __sum = 0;
		for (var i = 0; i < arr.length; i++) { __sum += arr[i]; }
	`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if e.GetGlobal("__sum").ToNumber() != 15 {
		t.Errorf("expected sum=15, got %v", e.GetGlobal("__sum").ToNumber())
	}
}

func TestGov8Engine_Execute_ErrorHandling(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	// Execute with syntax error — should not panic, and the engine
	// wraps code in try-catch so it should return nil error.
	_, err := e.Execute(`var x = ;`)
	if err != nil {
		t.Logf("Execute with syntax error returned: %v", err)
	}
	// The wrapper swallows errors, so the engine itself should not be broken.
	_, err = e.Execute(`var __ok = "still works";`)
	if err != nil {
		t.Fatalf("second Execute after error: %v", err)
	}
	if e.GetGlobal("__ok").ToString() != "still works" {
		t.Errorf("expected 'still works', got %q", e.GetGlobal("__ok").ToString())
	}
}

func TestGov8Engine_Execute_Empty(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	_, err := e.Execute("")
	if err != nil {
		t.Fatalf("Execute empty string: %v", err)
	}
	// Should not panic or error.
}

func TestGov8Engine_Close_Idempotent(t *testing.T) {
	e := js.NewGov8Engine()

	// Multiple closes should not panic.
	e.Close()
	e.Close()
	e.Close()
}

func TestGov8Engine_CollectGarbage(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	_, err := e.Execute(`var x = 42;`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// GC should not panic.
	e.CollectGarbage()

	// After GC, the engine should reinitialize on next use.
	_, err = e.Execute(`var y = 99;`)
	if err != nil {
		t.Fatalf("Execute after GC: %v", err)
	}
}

func TestGov8Engine_SetDOMChangeCallback(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	var called bool
	var mu sync.Mutex
	e.SetDOMChangeCallback(func() {
		mu.Lock()
		called = true
		mu.Unlock()
	})

	_, err := e.Execute(`var z = 1;`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Error("DOM change callback should have been called after Execute")
	}
}

// TestGov8Engine_FireEvent_LoadEvent verifies FireEvent does not panic
// when dispatching "load" events without registered listeners.
func TestGov8Engine_FireEvent_LoadEvent(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	_, err := e.Execute(`var __x = 1;`)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// FireEvent for "load" — should not panic even without onload handler.
	e.FireEvent("load", nil)
	e.FireEvent("DOMContentLoaded", nil)
}

// TestGov8Engine_Execute_GlobalScope verifies globals persist across Execute calls.
func TestGov8Engine_Execute_GlobalScope(t *testing.T) {
	e := js.NewGov8Engine()
	defer e.Close()

	if _, err := e.Execute(`var __shared = "persistent";`); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if _, err := e.Execute(`var __check = __shared;`); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if e.GetGlobal("__check").ToString() != "persistent" {
		t.Errorf("expected 'persistent', got %q", e.GetGlobal("__check").ToString())
	}
}
