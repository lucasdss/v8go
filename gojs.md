func TestAddInstruction(t *testing.T) {
        vm := NewVM()
        result := vm.Run("1 + 2")
        if result.ToNumber() != 3 {
            t.Errorf("Expected 3, got %v", result)
        }
    }
    ```
*   This is an ambitious engineering feat. Translating V8—a masterpiece of C++ performance—into Golang requires more than just porting code; it requires reconciling two fundamentally different memory management philosophies. 

While V8 manages its own raw memory heaps and pointer tagging, Go relies on a managed runtime and its own Garbage Collector (GC). To implement this effectively, the "AI Architect" must treat this as a systems-level translation project.

---

# V8 to Go: Implementation Blueprint & Analysis

## 1. Deep Analysis of the V8 Source Code
To build a Go implementation, the implementing AI must analyze these specific directories in the [V8 repository](https://chromium.googlesource.com/v8/v8.git):

| Component | V8 Directory (Source) | Go Implementation Focus |
| :--- | :--- | :--- |
| **Parsing** | `/src/parsing` | Lexer, Scanner, and Recursive Descent Parser. |
| **Interpreter** | `/src/interpreter` | **Ignition**: Register-based bytecode VM and the Bytecode Generator. |
| **Compiler** | `/src/compiler` | **TurboFan**: Sea-of-Nodes IR, optimization passes, and scheduling. |
| **Objects** | `/src/objects` | **Shapes/Hidden Classes**: Memory layout, transitions, and property descriptors. |
| **Heap** | `/src/heap` | Space management. *Decision:* Use Go’s GC or implement a separate heap? |
| **Built-ins** | `/src/builtins` | Implementation of standard JS objects (Array, Map, Promise) in Go. |

---

## 2. Instructions for Repo Analysis
The AI should follow these steps to extract the "DNA" of V8:

1.  **Map the Bytecode:** Analyze `src/interpreter/bytecodes.h`. Every instruction defined here must have a corresponding handler in the Go VM.
2.  **Deconstruct the Object Model:** Study `src/objects/objects.h`. Notice how V8 uses **Smi** (Small Integer) tagging. In Go, you will likely need to use `interface{}` or `unsafe.Pointer` with custom tags to mimic this without massive overhead.
3.  **Trace the Pipeline:** Follow a simple `a + b` expression from `src/parsing/parser.cc` through to the bytecode generation in `src/interpreter/bytecode-generator.cc`.
4.  **Identify Optimization Patterns:** Analyze how `src/compiler/js-operator.cc` handles type feedback. This is the secret sauce for V8's speed.

---

## 3. Implementation Plan for the AI (The "GoV8" Roadmap)

### Phase 1: The Foundation (Memory & Types)
*   **Task:** Define the `Value` type. Use a struct that can represent `String`, `Number`, `Boolean`, `Object`, and `Null/Undefined`.
*   **Challenge:** Implement **Hidden Classes (Shapes)**. Create a transition map so that objects with the same properties share the same "Shape" struct to enable fast lookups.

### Phase 2: The Frontend (Lexer & Parser)
*   **Task:** Build a hand-written recursive descent parser (mimicking `src/parsing`). 
*   **Output:** Produce a strongly-typed Abstract Syntax Tree (AST) in Go.

### Phase 3: The Virtual Machine (Ignition Clone)
*   **Task:** Implement a register-based VM. 
*   **Execution:** Create an `Execute()` loop. Use a large `switch` statement or a jump table (via function pointers) to process bytecodes.
*   **Registers:** Maintain a virtual register file for each function frame.

### Phase 4: The JIT & Optimizations (TurboFan Lite)
*   **Task:** Implement **Inline Caching (IC)**. 
*   **Execution:** For every property access, cache the `Shape` and the memory offset. On the next access, if the `Shape` matches, skip the lookup.

---

## 4. The Iterative Implementation Process
To ensure functionality matches the V8 reference, the AI must follow this **Default Iteration Loop**:

### The "Loop-to-Spec" Cycle
1.  **Implementation:** Build a single feature (e.g., `Array.prototype.map`).
2.  **V8 Alignment:** Run a small script in a standard Node.js/V8 environment and capture the output/bytecode via `--print-bytecode`.
3.  **Validation:** Run the same script in the Go implementation.
4.  **Comparison:** If outputs or side effects differ, use the `Analysis` instructions to re-read the V8 C++ source for that specific feature.
5.  **Refactor:** Adjust the Go code until the output matches.

---

## 5. Testing & Validation (Golang Standards)
All code must adhere to idiomatic Go testing patterns:

*   **Unit Tests (`*_test.go`):** Every bytecode instruction must have a unit test.
    ```go
    func TestAddInstruction(t *testing.T) {
        vm := NewVM()
        result := vm.Run("1 + 2")
        if result.ToNumber() != 3 {
            t.Errorf("Expected 3, got %v", result)
        }
    }
    ```
*   **Conformance Testing (Test262):** Use the official [ECMAScript Test Suite (Test262)](https://github.com/tc39/test2This is an ambitious engineering feat. Translating V8—a masterpiece of C++ performance—into Golang requires more than just porting code; it requires reconciling two fundamentally different memory management philosophies. 

While V8 manages its own raw memory heaps and pointer tagging, Go relies on a managed runtime and its own Garbage Collector (GC). To implement this effectively, the "AI Architect" must treat this as a systems-level translation project.

---

# V8 to Go: Implementation Blueprint & Analysis

## 1. Deep Analysis of the V8 Source Code
To build a Go implementation, the implementing AI must analyze these specific directories in the [V8 repository](https://chromium.googlesource.com/v8/v8.git):

| Component | V8 Directory (Source) | Go Implementation Focus |
| :--- | :--- | :--- |
| **Parsing** | `/src/parsing` | Lexer, Scanner, and Recursive Descent Parser. |
| **Interpreter** | `/src/interpreter` | **Ignition**: Register-based bytecode VM and the Bytecode Generator. |
| **Compiler** | `/src/compiler` | **TurboFan**: Sea-of-Nodes IR, optimization passes, and scheduling. |
| **Objects** | `/src/objects` | **Shapes/Hidden Classes**: Memory layout, transitions, and property descriptors. |
| **Heap** | `/src/heap` | Space management. *Decision:* Use Go’s GC or implement a separate heap? |
| **Built-ins** | `/src/builtins` | Implementation of standard JS objects (Array, Map, Promise) in Go. |

---

## 2. Instructions for Repo Analysis
The AI should follow these steps to extract the "DNA" of V8:

1.  **Map the Bytecode:** Analyze `src/interpreter/bytecodes.h`. Every instruction defined here must have a corresponding handler in the Go VM.
2.  **Deconstruct the Object Model:** Study `src/objects/objects.h`. Notice how V8 uses **Smi** (Small Integer) tagging. In Go, you will likely need to use `interface{}` or `unsafe.Pointer` with custom tags to mimic this without massive overhead.
3.  **Trace the Pipeline:** Follow a simple `a + b` expression from `src/parsing/parser.cc` through to the bytecode generation in `src/interpreter/bytecode-generator.cc`.
4.  **Identify Optimization Patterns:** Analyze how `src/compiler/js-operator.cc` handles type feedback. This is the secret sauce for V8's speed.

---

## 3. Implementation Plan for the AI (The "GoV8" Roadmap)

### Phase 1: The Foundation (Memory & Types)
*   **Task:** Define the `Value` type. Use a struct that can represent `String`, `Number`, `Boolean`, `Object`, and `Null/Undefined`.
*   **Challenge:** Implement **Hidden Classes (Shapes)**. Create a transition map so that objects with the same properties share the same "Shape" struct to enable fast lookups.

### Phase 2: The Frontend (Lexer & Parser)
*   **Task:** Build a hand-written recursive descent parser (mimicking `src/parsing`). 
*   **Output:** Produce a strongly-typed Abstract Syntax Tree (AST) in Go.

### Phase 3: The Virtual Machine (Ignition Clone)
*   **Task:** Implement a register-based VM. 
*   **Execution:** Create an `Execute()` loop. Use a large `switch` statement or a jump table (via function pointers) to process bytecodes.
*   **Registers:** Maintain a virtual register file for each function frame.

### Phase 4: The JIT & Optimizations (TurboFan Lite)
*   **Task:** Implement **Inline Caching (IC)**. 
*   **Execution:** For every property access, cache the `Shape` and the memory offset. On the next access, if the `Shape` matches, skip the lookup.

---

## 4. The Iterative Implementation Process
To ensure functionality matches the V8 reference, the AI must follow this **Default Iteration Loop**:

### The "Loop-to-Spec" Cycle
1.  **Implementation:** Build a single feature (e.g., `Array.prototype.map`).
2.  **V8 Alignment:** Run a small script in a standard Node.js/V8 environment and capture the output/bytecode via `--print-bytecode`.
3.  **Validation:** Run the same script in the Go implementation.
4.  **Comparison:** If outputs or side effects differ, use the `Analysis` instructions to re-read the V8 C++ source for that specific feature.
5.  **Refactor:** Adjust the Go code until the output matches.

---

## 5. Testing & Validation (Golang Standards)
All code must adhere to idiomatic Go testing patterns:

*   **Unit Tests (`*_test.go`):** Every bytecode instruction must have a unit test.
    ```go
    func TestAddInstruction(t *testing.T) {
        vm := NewVM()
        result := vm.Run("1 + 2")
        if result.ToNumber() != 3 {
            t.Errorf("Expected 3, got %v", result)
        }
    }
    ```
*   **Conformance Testing (Test262):** Use the official [ECMAScript Test Suite (Test262)](https://github.com/tc39/test262). The goal is to pass these tests one category at a time (e.g., "Language", "Built-ins").
*   **Benchmarking:** Use `testing.B` to compare the Go implementation's overhead against the C++ V8.
    *   *Instruction:* Focus on minimizing allocations in the "hot path" of the interpreter.
*   **Fuzzing:** Use `go test -fuzz`. Feed the parser random strings to ensure the engine doesn't panic on malformed JavaScript.

---

## 6. Final Instructions for the AI Agent
> "Your goal is not a 1:1 C++ translation, but a functional equivalent that respects Go's memory model. Prioritize the **Ignition Bytecode** first, as it provides the most stable path to a working engine. Do not attempt the JIT (TurboFan) until the interpreter passes 90% of the Test262 'Language' suite. Use `internal/` packages to hide implementation details and expose a clean `Execute(src string)` API to the user."

***

### Summary of the Iterative Workflow
| Step | Action | Tooling |
| :--- | :--- | :--- |
| **1. Analyze** | Read V8 `src/` for the target feature. | Git / Grep |
| **2. Implement** | Write Go code in small, testable chunks. | Go Modules |
| **3. Test** | Run against Test262 and local unit tests. | `go test` |
| **4. Profile** | Check for memory leaks or CPU bottlenecks. | `pprof` |
| **5. Repeat** | Move to the next bytecode/built-in. | N/A |
