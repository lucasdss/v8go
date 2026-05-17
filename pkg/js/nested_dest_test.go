package js_test

import (
    "testing"
    "github.com/lucasdss/v8go/pkg/js"
)

func TestNestedObjectDestructuring(t *testing.T) {
    vm := js.NewVM()
    // {a: {b, c}} - nested object destructuring
    result := vm.Run("var {a: {b, c}} = {a: {b: 1, c: 2}}; b + c")
    if result.ToNumber() != 3 {
        t.Errorf("nested object destructuring: expected 3, got %v", result.ToNumber())
    }
}

func TestNestedArrayDestructuring(t *testing.T) {
    vm := js.NewVM()
    // [[a, b], c] - nested array destructuring
    result := vm.Run("var [[a, b], c] = [[1, 2], 3]; a + b + c")
    if result.ToNumber() != 6 {
        t.Errorf("nested array destructuring: expected 6, got %v", result.ToNumber())
    }
}

func TestMixedNestedDestructuring(t *testing.T) {
    vm := js.NewVM()
    // [{x, y}, z] - mixed nested
    result := vm.Run("var [{x, y}, z] = [{x: 10, y: 20}, 30]; x + y + z")
    if result.ToNumber() != 60 {
        t.Errorf("mixed nested destructuring: expected 60, got %v", result.ToNumber())
    }
}

func TestArrayRestDestructuring(t *testing.T) {
    vm := js.NewVM()
    // [a, ...rest] - rest element in array
    result := vm.Run("var [a, ...rest] = [1, 2, 3, 4]; a + rest.length")
    if result.ToNumber() != 4 {
        t.Errorf("array rest destructuring: expected 4, got %v", result.ToNumber())
    }
}

func TestDestructuringRename(t *testing.T) {
    vm := js.NewVM()
    // {a: x} - rename
    result := vm.Run("var {a: x} = {a: 42}; x")
    if result.ToNumber() != 42 {
        t.Errorf("destructuring rename: expected 42, got %v", result.ToNumber())
    }
}

func TestDestructuringDefaults(t *testing.T) {
    vm := js.NewVM()
    // [a = 1, b = 2] = [] - defaults in array
    result := vm.Run("var [a = 1, b = 2] = []; a + b")
    if result.ToNumber() != 3 {
        t.Errorf("array defaults: expected 3, got %v", result.ToNumber())
    }
    // {a = 1, b = 2} = {} - defaults in object
    result2 := vm.Run("var {a = 10, b = 20} = {}; a + b")
    if result2.ToNumber() != 30 {
        t.Errorf("object defaults: expected 30, got %v", result2.ToNumber())
    }
    // [a = 1] = [5] - default not applied when value exists
    result3 := vm.Run("var [a = 1] = [5]; a")
    if result3.ToNumber() != 5 {
        t.Errorf("array defaults override: expected 5, got %v", result3.ToNumber())
    }
}

func TestDestructuringNestedDefaults(t *testing.T) {
    vm := js.NewVM()
    // {a: {b = 5}} = {} - nested with default
    result := vm.Run("var {a: {b = 5}} = {a: {}}; b")
    if result.ToNumber() != 5 {
        t.Errorf("nested defaults: expected 5, got %v", result.ToNumber())
    }
}

func TestDeeplyNestedDestructuring(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("var [[[a]]] = [[[42]]]; a")
    if result.ToNumber() != 42 {
        t.Errorf("deeply nested: expected 42, got %v", result.ToNumber())
    }
}

func TestObjectRestDestructuring(t *testing.T) {
    vm := js.NewVM()
    // {...rest} - rest element in object
    result := vm.Run("var {a, ...rest} = {a: 1, b: 2, c: 3}; rest.b + rest.c")
    if result.ToNumber() != 5 {
        t.Errorf("object rest destructuring: expected 5, got %v", result.ToNumber())
    }
}

func TestDestructuringForOf(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("var sum = 0; var arr = [[1, 2], [3, 4]]; for (var [a, b] of arr) { sum = sum + a + b; } sum")
    if result.ToNumber() != 10 {
        t.Errorf("for-of destructuring: expected 10, got %v", result.ToNumber())
    }
}

func TestDestructuringForOfNested(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("var sum = 0; var arr = [{x: 1, y: 2}, {x: 3, y: 4}]; for (var {x, y} of arr) { sum = sum + x + y; } sum")
    if result.ToNumber() != 10 {
        t.Errorf("for-of nested destructuring: expected 10, got %v", result.ToNumber())
    }
}

func TestFunctionParamDestructuringArray(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("function f([a, b]) { return a + b; } f([1, 2])")
    if result.ToNumber() != 3 {
        t.Errorf("function param array destructuring: expected 3, got %v", result.ToNumber())
    }
}

func TestFunctionParamDestructuringObject(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("function f({x, y}) { return x + y; } f({x: 10, y: 20})")
    if result.ToNumber() != 30 {
        t.Errorf("function param object destructuring: expected 30, got %v", result.ToNumber())
    }
}

func TestFunctionParamDestructuringNested(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("function f([a, [b, c]]) { return a + b + c; } f([1, [2, 3]])")
    if result.ToNumber() != 6 {
        t.Errorf("function param nested destructuring: expected 6, got %v", result.ToNumber())
    }
}

func TestFunctionParamDestructuringDefault(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("function f([a = 1, b = 2] = []) { return a + b; } f()")
    if result.ToNumber() != 3 {
        t.Errorf("function param destructuring default: expected 3, got %v", result.ToNumber())
    }
}

func TestFunctionParamDestructuringRest(t *testing.T) {
    vm := js.NewVM()
    result := vm.Run("function f([a, ...rest]) { return rest.length; } f([1, 2, 3, 4])")
    if result.ToNumber() != 3 {
        t.Errorf("function param destructuring rest: expected 3, got %v", result.ToNumber())
    }
}
