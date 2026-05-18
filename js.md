To build a Golang implementation of the V8 JavaScript engine, an AI agent must first deeply understand the architectural brilliance of the original C++ codebase. V8 is not a simple interpreter; it is a highly complex, multi-tiered virtual machine designed for extreme performance.

Here is a deep analysis of the V8 architecture, followed by a comprehensive, phase-by-phase implementation plan designed to be consumed by an AI coding agent.

---

### Part 1: Deep Analysis of V8 Architecture

V8 is fundamentally built around optimizing execution speed and memory efficiency. To replicate it, the implementing AI must understand these five core pillars:

#### 1. The Execution Pipeline (Ignition, Maglev, and TurboFan)

V8 does not interpret JavaScript source code directly, nor does it compile it all to machine code at once. It uses a tiered compilation model:

* **Parser/Scanner:** Converts JS source strings into an Abstract Syntax Tree (AST).
* **Ignition (The Interpreter):** Walks the AST and generates V8 Bytecode. It executes this bytecode using a register-based virtual machine. It also collects profiling data (type feedback) during execution.
* **Maglev (Mid-Tier JIT):** A fast JIT compiler introduced recently to bridge the gap between Ignition and TurboFan. It generates moderately optimized machine code quickly.
* **TurboFan (Optimizing JIT):** Takes the bytecode and the type feedback collected by Ignition. If a function is "hot" (called frequently), TurboFan compiles it into highly optimized native machine code using a "Sea of Nodes" graph representation.
* **Deoptimization:** If TurboFan makes an aggressive assumption (e.g., "this variable is always an integer") and that assumption is broken (e.g., the variable is suddenly a string), the engine "deoptimizes," throwing away the machine code and falling back to Ignition bytecode.

#### 2. Object Representation (Hidden Classes / Shapes)

JavaScript is dynamically typed; objects can have properties added or removed at any time. Standard dictionary lookups (hash maps) are too slow for property access.

* **Hidden Classes (Maps):** V8 assigns a hidden C++ class to every JS object. If two objects have the same properties in the same order, they share a Hidden Class.
* **Transitions:** If you add property `x` to an empty object, V8 creates a transition path from `EmptyMap` to `Map_with_x`. This allows V8 to calculate fixed memory offsets for properties, making JS object access as fast as C++ struct access.

#### 3. Inline Caching (ICs)

Whenever a function accesses an object property, V8 caches the Hidden Class and the memory offset of that property. The next time the function runs, V8 checks if the object's Hidden Class matches the cached one. If it does, it skips the lookup and goes straight to the memory offset.

#### 4. Memory Management & Garbage Collection (Orinoco)

V8 uses a generational, mostly concurrent garbage collector.

* **Pointer Tagging:** V8 needs to distinguish between pointers to objects and actual integer values. It uses "Smis" (Small Integers). If the lowest bit of a 64-bit word is `0`, it's an integer. If it's `1`, it's a pointer to a heap object.
* **Young Generation (Scavenger):** Fast, evacuating GC for short-lived objects using a Cheney semi-space algorithm (moving survivors from a "from-space" to a "to-space").
* **Old Generation (Mark-Sweep-Compact):** For long-lived objects. It marks live objects concurrently, sweeps dead ones, and compacts memory to prevent fragmentation.

#### 5. Isolates and Contexts

* **Isolate:** An independent copy of the V8 runtime, including its own heap. Isolates can run in parallel in separate threads but cannot share memory directly.
* **Context:** A sandboxed execution environment within an Isolate. For example, each `<iframe>` in a browser gets its own Context, with its own global `window` object, but they share the same Isolate.

---

### Part 2: AI Implementation Plan (Golang)

**System Prompt Instruction for the Implementing AI:**
*"You are an expert systems programmer tasked with building 'V8Go', a clean-room implementation of the V8 JavaScript engine written in Go. You must adapt V8's C++ paradigms to idiomatic, high-performance Go, handling the constraints of Go's own Garbage Collector and memory model. Execute the following plan sequentially."*

#### Phase 1: Core Types, Memory, and Tagging

**Goal:** Establish how JavaScript values are represented in Go memory.

1. **Value Representation:** Since Go has its own GC, true V8 pointer tagging (using bitwise hacks on physical memory addresses) is unsafe and fights the Go runtime.
* *Directive:* Implement a `JSValue` interface or a specialized struct containing a `type` tag and a union-like payload (using Go's `unsafe.Pointer` or an `any` wrapper, though `unsafe` is preferred for GC performance).


2. **Hidden Classes (Maps):**
* *Directive:* Implement a `Shape` (Hidden Class) struct that maps string keys to integer offsets. Implement a Transition Tree to share Shapes between objects with identical structures.


3. **The JS Object:**
* *Directive:* Implement `JSObject` as a struct containing a pointer to its `Shape` and a flat slice (`[]JSValue`) for inline properties, falling back to a hash map only for dictionary-mode objects.



#### Phase 2: Lexing, Parsing, and AST

**Goal:** Parse ECMAScript specification code into an Abstract Syntax Tree.

1. **Scanner:**
* *Directive:* Build a zero-allocation lexer that processes `[]byte` source code into tokens. Use Go's fast UTF-8 decoding capabilities.


2. **Parser:**
* *Directive:* Implement a Recursive Descent parser. Output a strictly typed AST.


3. **Scopes & Hoisting:**
* *Directive:* Implement an AST traversal pass that resolves lexical scope, handles `var`/`let`/`const` hoisting, and identifies closures.



#### Phase 3: The Interpreter (Ignition Port)

**Goal:** Compile the AST to Bytecode and execute it in a Register VM.

1. **Bytecode Design:**
* *Directive:* Define a fixed-width bytecode instruction set (e.g., `LdaConstant`, `Star`, `Add`, `Call`).


2. **Bytecode Generator:**
* *Directive:* Write a compiler that walks the AST and emits `[]byte` bytecode and a side-table for constants/strings.


3. **The Register VM:**
* *Directive:* Build the execution loop. Unlike stack VMs, use a virtual register file (an array of `JSValue` allocated per function frame). Implement an `Execute(bytecode []byte)` loop with a large `switch` statement for opcodes.



#### Phase 4: Inline Caching (IC) and Profiling

**Goal:** Speed up the interpreter by caching property lookups.

1. **Type Feedback Vector:**
* *Directive:* Attach a `FeedbackVector` slice to each compiled function.


2. **IC Implementation:**
* *Directive:* When the `LoadIC` opcode executes, check the target object's `Shape`. If it matches the cached `Shape` in the `FeedbackVector`, return the value at the cached index. If not, perform a slow dictionary lookup and update the cache (monomorphic -> polymorphic -> megamorphic states).



#### Phase 5: The JIT Compiler (TurboFan/Maglev Port)

*Note: Generating raw machine code in Go requires allocating memory with `mmap` and `PROT_EXEC`, which is highly OS-specific. This is the hardest phase.*
**Goal:** Compile hot bytecode to native machine code (x86_64/ARM64).

1. **Profiler:**
* *Directive:* Add a call counter to the VM loop. When a function crosses a threshold (e.g., 10,000 calls), queue it for JIT compilation.


2. **Intermediate Representation (IR):**
* *Directive:* Translate Bytecode into a Static Single Assignment (SSA) control-flow graph.


3. **Machine Code Emission:**
* *Directive:* Write a basic assembler in Go that lowers the SSA graph into raw x86_64/ARM64 instructions. Allocate executable memory using syscalls, copy the instructions, and cast the memory address to a Go function pointer using `unsafe`.


4. **Deoptimization Bailouts:**
* *Directive:* Insert guard instructions in the native code. If a guard fails (e.g., expected an int, got a string), the native code must push its state to the Go stack, rewrite the instruction pointer, and jump back into the Go-based Bytecode VM interpreter.



#### Phase 6: Runtime and APIs (Isolates)

**Goal:** Make the engine usable by host applications.

1. **Isolate & Context:**
* *Directive:* Wrap the heap, compiler, and VM state into an `Isolate` struct. Implement `Context` to hold global variables (`globalThis`).


2. **Go/JS Interop:**
* *Directive:* Create an API similar to V8's C++ API (e.g., `v8.NewFunctionTemplate()`) allowing Go developers to bind native Go functions to JavaScript, seamlessly converting between Go `string`/`int` and V8Go `JSValue`.
