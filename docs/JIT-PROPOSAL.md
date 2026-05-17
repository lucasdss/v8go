# **Architectural Strategies for Implementing Just-In-Time Compilation in the Go Programming Language**

The convergence of managed, Ahead-Of-Time (AOT) compiled systems languages and dynamic, Just-In-Time (JIT) compiled runtimes represents a formidable frontier in modern software and compiler engineering. Traditionally, dynamic language engines—such as the V8 JavaScript engine embedded in Node.js and Chromium—are constructed using C or C++, operating entirely independently of the host application's memory management paradigms. However, the rise of heavily concurrent, data-driven backend architectures has catalyzed a demand for embedding dynamic execution environments directly within Go applications. For example, highly scalable real-time rule engines often require backend logic to be treated as dynamic data rather than static, compiled code to prevent infinite deployment cycles when business logic evolves. While pure-Go abstract syntax tree (AST) walkers or basic interpreters (such as the goja project) provide seamless interoperability with Go structs and eliminate Foreign Function Interface (FFI) overhead, their peak performance is fundamentally bounded by the inherent latencies of AST traversal and interpretation.

To achieve execution speeds that rival industry-standard native engines, the architectural focus must pivot from optimizing the Go runtime itself to engineering a bespoke virtual machine (VM) hierarchy hosted entirely within a Go process. This VM must target raw native machine code generation while navigating the stringent constraints of Go’s runtime environment. Adapting the architectural triumphs of the V8 engine requires precise calibration to accommodate Go’s exact memory allocation rules, continuous concurrent garbage collection (GC), and internal Application Binary Interfaces (ABI). This report delineates an exhaustive, multi-phased architectural strategy for constructing a high-performance JIT compiler within Go, exploring the multi-tier execution pipeline, speculative optimization matrices, bailout mechanisms, and the critical low-level bridging required to maintain symbiosis with the Go runtime.

## **The Multi-Tier Execution Pipeline**

The foundational architecture of a high-performance dynamic language engine relies on a multi-tier pipeline that optimizes the delicate equilibrium between startup latency and peak steady-state execution speed. Interpreting code directly from an AST is prohibitively slow due to pointer chasing and the sheer volume of memory accesses required to evaluate nodes. Conversely, aggressively optimizing and compiling all incoming source code immediately to native machine instructions incurs severe compilation latency, wasting CPU cycles and memory on code paths that may only execute once during an application's lifecycle. To navigate this dichotomy, the architecture must adopt a tiered model modeled after V8’s pipeline, escalating execution through progressively more sophisticated environments based on execution frequency and runtime profiling.

### **Tier 0: The Register-Based Bytecode Interpreter**

The initial execution phase necessitates an interpreter capable of near-instantaneous startup. Modeled after V8's Ignition, this tier parses the incoming JavaScript source into an Abstract Syntax Tree (AST) and immediately compiles it down to a highly optimized, register-based bytecode. A register-based virtual machine architecture is strictly preferred over a stack-based architecture because it drastically reduces the total number of bytecode instructions required to evaluate expressions, minimizing dispatch overhead. Furthermore, a register machine explicitly defines its inputs and outputs as virtual register operands, which aligns much more cleanly with physical CPU architectures during subsequent compilation phases.

Within this interpreter, the virtual registers are mapped to specific slots in the native machine stack memory allocated for the VM frame. While the interpreter evaluates the bytecode, it serves a critical secondary function: the collection of empirical Type Feedback. Because JavaScript is dynamically typed, the VM cannot determine ahead of time whether a binary addition operator \+ will perform integer arithmetic or string concatenation. Therefore, the interpreter attaches a "Feedback Vector" to every bytecode instruction capable of polymorphic behavior. Whenever an operation occurs, the interpreter analyzes the concrete data types—such as 32-bit integers, 64-bit floats, string references, or specific memory layouts of objects—and records this observed shape into the corresponding slot within the Feedback Vector. This continuous profiling generates the precise metadata matrix upon which all speculative JIT optimizations will rely.

### **Tier 1: Baseline Compilation and Frame Mirroring**

When the execution frequency of a specific function crosses a predefined "warm" threshold, the VM escalates the code to a baseline JIT compiler, analogous to V8's Sparkplug tier. The objective of the baseline JIT is not to execute complex control-flow analysis, register allocation, or loop invariant code motion. Instead, it serves to eliminate interpreter dispatch overhead by linearly iterating over the bytecode array and emitting fixed, pre-compiled machine-code stubs for each opcode.

A profound architectural mechanism employed at this tier involves the strict maintenance of interpreter-compatible stack frames. Rather than designing a unique, highly optimized stack layout for the baseline JIT, the emitted machine code intentionally mirrors the exact geometric layout of the frame utilized by the bytecode interpreter.

In a standard VM execution environment, a stack frame generally stores arguments pushed in reverse order, followed by standard linkage data such as the caller's frame pointer, the runtime execution context, the specific function closure being executed, and the argument count. An interpreter extends this standard layout by allocating slots for the virtual registers tracking local variables, a pointer to the bytecode array currently executing, and the exact integer offset of the active bytecode instruction.

The baseline JIT precisely mirrors this layout with one strategic substitution. Because the baseline JIT is executing native machine code rather than iterating over an array, it does not need to continuously update a bytecode offset slot during execution. Instead, it maintains a separate two-way mapping table linking physical instruction pointer addresses to virtual bytecode offsets. The stack frame slot that previously held the bytecode offset is repurposed to cache the Feedback Vector.

The decision to mirror frames yields cascading architectural advantages that drastically reduce VM complexity:

1. It bypasses the need for the baseline compiler to perform complex register allocation, allowing it to rapidly map native instructions directly to the memory slots defined by the interpreter's virtual registers.  
2. It enables seamless On-Stack Replacement (OSR). Transitioning execution between the interpreter and the baseline JIT mid-function (e.g., during a long-running while loop) incurs near-zero frame translation overhead because the physical memory geometry of both frames is functionally identical.  
3. Deep system integrations—such as Go-based exception stack unwinders, profilers, and debuggers—continue to operate without modification because they perceive the baseline JIT native frame merely as a standard interpreter frame during stack walks.

### **Tier 2: The Speculative Optimizing Compiler**

Functions that persist in execution and reach a heavily profiled "hot" threshold are routed to an advanced optimizing JIT compiler, fulfilling the role of V8's TurboFan or Maglev. This compiler consumes both the original bytecode array and the densely populated Feedback Vectors accumulated during Tier 0 and Tier 1 execution to construct a highly sophisticated Static Single Assignment (SSA) Intermediate Representation (IR), commonly structured as a "Sea of Nodes" graph.

The optimizing compiler utilizes the Type Feedback metadata to perform aggressive speculative optimizations. Based on the statistical probability that future executions will encounter the exact same data types previously observed, the compiler strips away generic, slow-path dynamic type checking. If a Feedback Vector indicates that a specific loop index has only ever been manipulated as a 32-bit integer, the optimizer will eliminate dynamic dispatch, unbox the integer from its heap-allocated wrapper, and emit highly optimized, raw arithmetic machine code. Furthermore, monomorphic method calls—where a specific call site only ever invokes a single target function against a single object type—are aggressively inlined. The optimizer replaces the call overhead and context switching with the actual instructions of the target function body, guarded only by a lightweight, single-instruction type check.

| Execution Tier | V8 Equivalent | Compilation Latency | Peak Execution Speed | Primary Function within VM | Internal State Representation |
| :---- | :---- | :---- | :---- | :---- | :---- |
| Tier 0 | Ignition | Zero | Low | AST execution, exact Feedback collection | Virtual Registers, Bytecode |
| Tier 1 | Sparkplug | Low | Medium | Rapid transition to native execution | Mirrored Interpreter Frames |
| Tier 2 | TurboFan | High | Maximum | Speculative native execution, Inlining | Static Single Assignment (SSA) IR |

## **Speculative Optimization and Polymorphic Dispatch**

The baseline velocity of a dynamically typed language engine is directly proportional to its ability to circumvent dynamic dictionary lookups. In a naive implementation, JavaScript objects function as simple hash maps; accessing a property (e.g., object.x) requires hashing a string key, resolving collisions, and traversing memory structures to locate the value. To achieve execution speeds comparable to C++ or Go, the JIT architecture must artificially transition from a sparse graph memory model to contiguous, deterministic, struct-like memory layouts. This is achieved through the implementation of Hidden Classes and Inline Caches.

### **Hidden Classes and Shape Transitions**

Hidden Classes (often referred to as Shapes or Maps in engine literature) optimize property access by enforcing a strict, internal structural identity on dynamically created JavaScript objects. Under this architecture, when an object is instantiated, the VM assigns it an initial, empty Hidden Class. As properties are dynamically added, the engine does not merely insert a new key-value pair into a hash map; instead, it triggers a deterministic "transition" to a completely new Hidden Class.

Consider the instantiation of an object. Transitioning from an empty object {} to an object with one property {x: 1} shifts the object's internal pointer from HiddenClass0 to HiddenClass1. HiddenClass1 contains metadata recording that the property x is explicitly stored at memory offset 0 relative to the object's backing store. Adding a subsequent property y triggers a transition to HiddenClass2, which records y at offset 8\. Crucially, the Hidden Classes maintain a back\_pointer forming a transition tree. If a subsequent piece of code creates another object and follows the exact same property initialization sequence, it will traverse the exact same transition tree, ultimately landing on and sharing the exact same HiddenClass2.

This structural determinism ensures that objects with identical property layouts share the same memory shape identity. Consequently, the JIT compiler can replace expensive, multi-instruction hash map lookups with simple, hardcoded memory offset calculations. The underlying trend dictated by this design implies that developers who write code maintaining consistent object shapes enable the JIT to lock in massive performance gains, whereas code that deletes properties or initializes them in random orders shatters the transition tree, forcing the engine to revert to slow dictionary modes.

### **Inline Caching and Runtime Code Patching**

Inline Caches (ICs) serve as the mechanism by which the structural data mapped by Hidden Classes is embedded directly into the executing machine code. When the baseline JIT or optimizing compiler emits machine code for a property access or method call, it does not immediately know the optimal offset. Therefore, it initially emits a block of NOP (No Operation) padding instructions immediately followed by a jump to a slow, generic runtime lookup function.

During the first execution of this instruction block, the VM calls the generic implementation, which resolves the property lookup via the slow path and identifies the exact Hidden Class of the object being manipulated. Having identified the shape, the engine dynamically patches itself. It overwrites the memory block containing the NOP instructions with a highly optimized, type-specific machine code stub. This new stub contains "guard comparisons"—a fast assembly instruction that checks if the incoming object's Hidden Class pointer matches the expected, previously seen class. If the guard passes, execution bypasses the lookup entirely and directly accesses the specific memory offset.

The architecture of Inline Caches dynamically manages varying levels of data polymorphism at runtime:

1. **Monomorphic State:** The specific call site has only ever encountered one Hidden Class. The IC contains a single guard check and accesses the known offset.  
2. **Polymorphic State:** The call site has encountered a small, finite number of distinct Hidden Classes (typically between two and four). The engine patches the IC to include sequential guard checks for each known class, branching to the appropriate offset logic based on which guard succeeds.  
3. **Megamorphic State:** The call site is highly dynamic and has encountered a massive variety of Hidden Classes. At this threshold, the JIT aborts the IC patching strategy entirely. It overwrites the IC with a permanent jump to the generic dictionary lookup. This fallback mechanism prevents the engine from generating excessively large machine code stubs and eliminates instruction cache (i-cache) thrashing caused by constant code rewriting.

The broader implication of this mechanism is that the JIT compiler explicitly favors generating slightly larger code blocks for IC slots over prioritizing absolute instruction brevity, because predictable, linearly expanding conditional branches execute significantly faster on modern speculative execution hardware than indirect calls or memory lookups.

## **Implementing Deoptimization Mechanisms**

The central paradox of speculative JIT compilation is that its performance gains are predicated on assumptions that are fundamentally fragile. Because JavaScript is unconstrained by static types, an application can abruptly violate the assumptions that the Tier 2 compiler has hardcoded into the native machine code. If a function aggressively optimized exclusively for continuous arrays of 32-bit integers suddenly receives an array containing a string, the raw, unboxed machine code will either produce catastrophic memory corruption, crash the Go process via illegal instruction access, or return mathematically incorrect logic.

To preserve the safety and correctness of the execution environment, the VM must implement an uncompromising Deoptimization (commonly referred to as "bailout") mechanism. Deoptimization allows the runtime to instantly halt the execution of the flawed optimized machine code, reconstruct the exact virtual execution state, and resume execution gracefully within the unoptimized, safe Tier 0 interpreter.

### **The Architecture of a Bailout**

The sequence of events during a deoptimization must be atomically precise, as the engine is transitioning between two radically different abstractions of the program state.

| Step | Action Performed | Implementation Mechanics in Go/Assembly |
| :---- | :---- | :---- |
| **1\. Guards** | Verification of speculative assumptions prior to executing optimized operations. | The JIT emits low-level assembly instructions (e.g., CMP followed by JNE) that branch directly to an out-of-line deoptimization stub if the Hidden Class or type check fails. |
| **2\. State Capture** | Preservation of the exact hardware execution state at the moment of failure. | The deoptimization stub executes a block of instructions to save all current general-purpose CPU registers and floating-point registers into a contiguous FrameDescription structure allocated in memory. |
| **3\. Frame Reconstruction** | Translation of physical hardware state back into virtual interpreter state. | The runtime utilizes DeoptimizationInputData side-tables generated during compilation to map the captured hardware register values back to the interpreter's original virtual stack registers. |
| **4\. Resumption** | Re-entry into the safe execution tier. | The engine constructs a new interpreter frame, updates the program counter, and jumps back to the interpreter loop at the exact bytecode offset where the speculative failure occurred. |

### **Metadata Mapping and Frame Translation**

The primary engineering challenge of deoptimization lies in the State Capture and Frame Reconstruction phases. The Tier 2 optimizing compiler relies heavily on physical CPU registers and deeply nested native stack frames to achieve peak performance. Values that the Tier 0 interpreter would normally store in explicit virtual stack slots might currently exist exclusively in hardware registers (e.g., RAX or R0), or they might have been spilled to arbitrary locations on the native stack due to register pressure.

To resume execution in the interpreter, this physical machine state must be accurately translated back into the virtual register state. This translation is governed by a complex metadata side-table generated entirely during the Tier 2 compilation phase, often represented internally as DeoptimizationInputData.

This metadata maps every single checkpoint in the optimized machine code where a deoptimization could theoretically occur to the corresponding exact offset in the original bytecode array. More critically, it provides a deterministic translation matrix: it dictates to the deoptimizer that virtual register V1 currently resides in hardware register RAX, virtual register V2 is located at physical stack offset \-16, and virtual register V3 has been optimized away completely because the compiler proved it was a constant, meaning its value must be reconstructed from the metadata itself.

When a bailout is triggered, the Go-based VM utilizes this metadata to construct a completely new, standard interpreter stack frame on the fly. It populates the virtual registers with the values extracted from the hardware state, discards the optimized machine code, and seamlessly resumes interpreting the bytecode. This design ensures that the end-user or developer remains completely unaware that the program briefly transitioned to native code and back, preserving the illusion of continuous execution.

## **Memory Management and $W \\oplus X$ Constraints**

Constructing a JIT compiler hosted entirely within Go requires direct interaction with the host operating system's low-level memory management APIs. The Go runtime's native allocator (accessed via new or make) provisions memory exclusively for data structures. By default, pages allocated by the Go runtime are marked as read-write but strictly non-executable by the OS kernel to prevent buffer overflow attacks from executing malicious payloads. Emitting JIT machine code directly into standard Go-allocated byte slices and attempting to jump the instruction pointer into that slice will trigger an immediate segmentation fault (SIGSEGV) under all modern hardware security protocols.

### **System Calls and Allocation Strategies**

To provision memory capable of executing instructions, the Go application must bypass the Go allocator and utilize the POSIX mmap system call to request page-aligned memory regions directly from the operating system kernel. This process generally involves allocating an anonymous, private memory map.

However, a critical security paradigm enforced by modern operating systems and CPU hardware is $W \\oplus X$ (Write XOR Execute), often referred to as Data Execution Prevention (DEP). This principle strictly mandates that a memory page can either be writable or executable, but never simultaneously both, thereby mitigating the risk of arbitrary code execution exploits by ensuring that an attacker cannot write shellcode to a page and subsequently execute it.

The standard architectural protocol for navigating $W \\oplus X$ constraints within a JIT compiler entails a heavily synchronized, multi-step orchestration:

1. **Allocation:** The JIT invokes mmap with the flags PROT\_READ | PROT\_WRITE to acquire a block of writable memory from the kernel.  
2. **Emission:** The Go-based assembler writes the generated x86-64 or ARM64 opcodes, along with any necessary padding, directly into this writable region.  
3. **Protection Shift:** The engine issues a system call to mprotect targeting the specific page boundary, shifting the memory permissions from writable to PROT\_READ | PROT\_EXEC.  
4. **Execution:** With the page now marked executable, the VM can safely branch the instruction pointer into this region to execute the native code.

### **Advanced Dual-Mapping Techniques**

While the standard mprotect toggling strategy is functionally secure, it introduces substantial execution latency. Every transition between writing code (e.g., dynamically patching an Inline Cache or generating a deoptimization stub) and executing that code necessitates a heavy context switch to the kernel. Furthermore, in strict operating environments (such as macOS on Apple Silicon hardware), rapid toggling of page permissions is heavily restricted, gated behind specific entitlements (like jit-write-allowlist and the MAP\_JIT flag), and subjected to intense kernel scrutiny.

To circumvent this latency, an advanced architectural bypass known as the dual-mapping strategy is frequently implemented. This technique involves creating an anonymous shared memory object (e.g., via shm\_open or memfd\_create on Linux systems) and calling mmap twice on the exact same file descriptor. The first mapping requests PROT\_READ | PROT\_WRITE permissions, while the second mapping requests PROT\_READ | PROT\_EXEC.

This operation results in two completely distinct virtual address ranges within the Go process's memory space that resolve to the exact same underlying physical memory pages. The JIT compiler retains the read-write pointer internally to emit new instructions and dynamically patch Inline Caches without ever triggering a system call. Concurrently, the execution engine utilizes the read-execute pointer to branch into the code.

While dual mapping resolves the performance bottleneck of mprotect, it shifts the complexity burden to cache coherency. Modern CPUs utilize separate caches for data (d-cache) and instructions (i-cache). When the JIT writes instructions to the RW pointer, they enter the d-cache. The CPU's i-cache remains entirely unaware of these modifications and may attempt to execute stale or zero-filled instructions from the RX pointer. Particularly on ARM64 architectures, this requires the JIT implementation in Go to explicitly issue flush\_cache\_range or equivalent low-level cache invalidation instructions across the modified memory block before allowing execution to proceed, ensuring the i-cache correctly fetches the newly emitted opcodes.

## **Go Runtime Integration and ABI Constraints**

Because the JIT engine is physically hosted within a Go application, the machine code it generates cannot operate in total isolation. Dynamic languages rely heavily on host environment features; JavaScript code will inevitably need to invoke built-in runtime functions (such as console.log, filesystem I/O, or asynchronous network request handlers) that are implemented natively in Go. To accomplish this, the raw machine code emitted by the JIT must bridge the gap back into the Go runtime safely. Doing so requires uncompromising adherence to Go's internal Application Binary Interface, known as ABIInternal.

Unlike C or C++ compilers, which generally adhere to standard platform-specific operating system ABIs (such as System V AMD64 ABI or ARM AAPCS), the Go toolchain implements a bespoke, register-based calling convention. This ABI is designed specifically to support Go's unique architectural features, specifically its lightweight concurrency model (goroutines) and its mechanism for dynamic stack growth.

### **Register Architecture and Mapping Layouts**

To successfully call a Go function from JIT-emitted machine code, the JIT must carefully marshal arguments into specific hardware registers dictated by ABIInternal, rather than pushing them onto the stack (as was the standard in the legacy ABI0 specification).

**x86-64 (amd64) Architecture Mapping:** On AMD64 processors, Go's ABIInternal utilizes a specific sequence of nine integer registers (RAX, RBX, RCX, RDI, RSI, R8, R9, R10, R11) for passing integer arguments and receiving results. Floating-point arguments are routed exclusively through SIMD registers X0 through X14.

Crucially, the JIT must respect several registers that have fixed, special-purpose meanings within the Go runtime:

* RSP (Stack Pointer): Must remain 8-byte aligned and always grows downward.  
* RBP (Frame Pointer): Must be correctly maintained to allow Go's tracebacks and profilers to unwind the stack across the JIT boundary.  
* R14: Permanently reserved to hold the pointer to the current goroutine (g struct). The JIT compiler must never clobber this register under any circumstances. Any Go code invoked will immediately rely on R14 to interact with the scheduler, the memory allocator, or to check stack bounds.  
* X15: Treated by Go as a fixed zero register for rapid memory clearing.

**ARM64 Architecture Mapping:** On the ARM64 instruction set, the convention shifts to accommodate the wider register file. Go utilizes R0 through R15 for passing integer arguments and F0 through F15 for floating-point arguments.

The special-purpose register requirements on ARM64 under ABIInternal are equally strict:

* R22: Permanently reserved to hold the pointer to the current goroutine (g struct).  
* R29: Designated as the Frame pointer.  
* R30: Designated as the Link register, explicitly holding the return address for function calls.  
* RSP (Stack Pointer): Must be strictly 16-byte aligned at all times, complying with the strict alignment fault requirements of the underlying ARM64 hardware.

| Architecture | Int Arguments | Float Arguments | Goroutine Pointer (g) | Stack Pointer (Align) | Frame/Link Registers |
| :---- | :---- | :---- | :---- | :---- | :---- |
| **AMD64** | RAX, RBX, RCX, RDI, RSI, R8-R11 | X0 \- X14 | R14 | RSP (8-byte) | RBP (Frame) |
| **ARM64** | R0 \- R15 | F0 \- F15 | R22 | RSP (16-byte) | R29 (Frame), R30 (Link) |

When the JIT code intends to execute a call into a Go function, it must meticulously simulate the exact stack setup sequence that the Go compiler would generate. This includes subtracting from the stack pointer to open the frame, explicitly saving the link register and frame pointer to the stack, mapping the arguments into the correct sequence of integer or float registers, and reserving necessary "spill space" on the stack. The reservation of spill space is critical because Go functions expect adequate stack capacity to spill register-based arguments to memory during deep call chains or when dynamic stack growth is triggered.

The preservation of the goroutine pointer (R14 on AMD64, R22 on ARM64) is an absolute, non-negotiable architectural requirement. If the JIT optimizer inadvertently allocates these registers for temporary scratch space, any subsequent operation that traps back into the Go runtime—such as an implicit memory allocation triggering the garbage collector, or a channel operation—will attempt to dereference the corrupted register, resulting in an immediate, unrecoverable runtime panic.

## **Garbage Collection Safety and Pointer Tracking**

The most formidable engineering barrier to successfully hosting a native JIT compiler within Go is achieving synchronization between the untyped, dynamically generated machine code and Go's precise, concurrent garbage collector (GC). Go utilizes a highly optimized tri-color concurrent mark-and-sweep garbage collector. This collector operates on the strict assumption that it possesses total omniscience regarding the location of every active memory pointer within the entire system.

During the mark phase, the Go GC pauses briefly to scan the roots of the system, which includes unwinding the execution stack of every active goroutine. To accomplish this, the unwinder cross-references the current instruction pointer (PC) against internal stack maps (moduledata) generated statically during AOT compilation. These stack maps explicitly dictate which bytes on the stack represent active pointers to heap memory and which bytes represent raw scalar data (like integers or booleans).

When the execution flow jumps into JIT-generated machine code, the stack frames are populated with dynamic runtime data. Because this JIT code was emitted dynamically via memory mapping rather than compiled by the Go toolchain, there is no corresponding moduledata or static stack map for these physical frames. If the Go GC activates and attempts to scan the stack while execution is inside the JIT payload, the unwinder encounters an unmapped PC. Lacking instructions on how to parse the stack frame, the runtime immediately throws a fatal exception: runtime: unexpected return pc... fatal error: unknown caller pc.

To circumvent this architectural mismatch, the VM must ensure that the JIT frames are either completely invisible to the GC (while still maintaining the necessary reachability of JavaScript objects to prevent premature sweeping) or explicitly documented to the runtime via advanced interfaces.

### **The Software Shadow Stack Architecture**

The most robust architectural solution available in stable releases of the Go toolchain is the implementation of a Software Shadow Stack. In this model, the raw native JIT frames are designed to never hold any actual hardware pointers to JavaScript objects allocated on the Go heap. Instead, they operate exclusively on scalar integer indices or raw memory addresses that are mathematically opaque to the garbage collector.

Concurrently, the engine maintains an explicit array or slice of unsafe.Pointer on the Go heap—this serves as the Shadow Stack. Whenever the JIT code creates or manipulates a JavaScript object, it stores the true memory reference in this Go slice, and retains only the integer index representing that slice entry within its physical machine registers.

Because the Shadow Stack is standard Go data structure, it remains perfectly visible to the Go GC as a managed root. As long as an object's reference resides within the Shadow Stack, the tri-color marker will trace it, ensuring it is not prematurely collected. When the JIT scope closes and it no longer requires the object, it explicitly nullifies the slot in the Shadow Stack. While this architecture introduces a layer of indirection—requiring the JIT to dereference an index to reach the pointer before manipulating object memory—it guarantees absolute GC safety and eliminates crashes without requiring any modifications to the Go runtime.

While hardware-accelerated shadow stacks (such as Intel CET or ARM PAC) offer future avenues for optimizing stack unwinding, integrating these hardware features with Go's cooperative user-space scheduler remains an unresolved kernel-level challenge.

### **Experimental Interface: The runtime/jit Proposal**

For implementations willing to leverage experimental branches of the Go toolchain, the proposed runtime/jit package represents a profound paradigm shift. This standard library extension is designed explicitly for programs embedding WebAssembly engines, language VMs, or custom JIT compilers, allowing them to legally declare dynamic memory to the unwinder.

The runtime/jit package permits the developer to register regions of dynamically generated executable memory with the Go runtime utilizing a Region structure that provides vital callback hooks :

1. **Next(pc, sp)**: This callback enables the Go stack unwinder to skip cleanly over the native JIT frames. When the unwinder hits an unknown PC, it queries the registered regions. The JIT responds with the exact PC and SP of the last known Go caller, effectively creating a bridge that allows tracebacks to bypass the unmapped JIT memory entirely.  
2. **ScanStack**: A critical callback allowing the JIT engine to explicitly report the exact locations of GC roots currently held within the JIT's physical stack or registers. This dynamic reporting eliminates the need for an indirect Software Shadow Stack, allowing the JIT to hold raw pointers directly in hardware registers.  
3. **Preempt() bool**: A cooperative scheduling coordination function. Go's scheduler requires goroutines to yield periodically to prevent starvation. A long-running optimized JIT loop could trap the processor indefinitely. To resolve this, the JIT emits specific machine code that periodically polls this Preempt function (typically at loop back-edges or function entry points). If the function returns true, the JIT immediately jumps back to the Go runtime to yield execution context, satisfying the scheduler.

## **Implementation Roadmap for a Go-Hosted JIT**

Transitioning from theoretical architecture to a functional, high-performance implementation requires a highly structured, phased roadmap. Building an optimized JIT engine is a massive systems engineering undertaking; therefore, incremental verification of each execution tier and runtime boundary is mandatory.

### **Phase 1: Go-Based Interpreter and Data Structures**

The foundation necessitates an AST parser and a bytecode interpreter implemented purely in Go, conceptually similar to the existing goja project. However, unlike typical AST-walking interpreters, this implementation must be architected specifically as a register machine from day one. Furthermore, the architecture must implement the Hidden Class transition tree matrix immediately. Every object property lookup must utilize this structural layout internally, ensuring that the necessary deterministic data infrastructure for Inline Caches exists before a single line of assembly is written.

### **Phase 2: Assembly Infrastructure and Memory Mapping**

The engine requires a robust mechanism to emit machine code dynamically at runtime. Relying on external C/C++ JIT libraries via cgo introduces unacceptable cross-boundary call overhead, defeating the latency goals of a fast JIT. Instead, the engine must utilize pure-Go assembly generation libraries, such as akyoto/asm or avo. These libraries empower Go programs to construct complex x86-64 or ARM64 opcodes programmatically directly in memory. Concurrently, the memory allocation wrapper handling the $W \\oplus X$ bypass (either via mprotect toggling or the advanced dual mapping technique) must be rigorously tested across target operating systems to ensure stability.

### **Phase 3: Sparkplug-Style Baseline JIT**

With the dynamic assembler functional, the engine introduces the Tier 1 compiler. This phase focuses purely on iterating over the bytecode array and emitting 1:1 machine code macros without attempting flow optimization. The critical milestone in this phase is validating the mirrored stack frame hypothesis. The engineering team must prove mathematically and empirically that the engine can jump from Go into the emitted JIT code, manipulate the Software Shadow Stack to preserve GC integrity during execution, and successfully return to Go using the strict ABIInternal conventions without crashing the unwinder.

### **Phase 4: Feedback Vectors and SSA Optimization**

The final, most complex phase introduces the Tier 2 compiler. The Tier 0 interpreter is augmented to record Type Feedback during its execution. A secondary background compiler thread is implemented to ingest this feedback, converting the linear bytecode into a Static Single Assignment (SSA) node graph. At this stage, speculative type guards are emitted, and property lookups are replaced with dynamically patched Inline Caches. Finally, the DeoptimizationInputData tables are formalized and linked to the bailout stubs, ensuring that when the highly specialized, unboxed code encounters an unexpected object shape, it can cleanly capture the machine state, reconstruct the virtual register frame, and hand control seamlessly back to the Go-based interpreter.

## **Conclusion**

Implementing a JavaScript Just-In-Time compiler entirely within Go fundamentally alters the operational relationship between the host runtime and the guest language. By abandoning the paradigm of treating Go purely as an abstract application layer and instead leveraging it as a low-level host environment—carefully mapping internal ABIInternal registers, navigating rigid kernel-level memory constraints, and orchestrating shadow stacks—engineers can achieve execution velocities that rival native C++ engines without sacrificing the intrinsic memory safety of Go.

The integration of V8's tiered architecture—register-based interpreters, frame-mirroring baseline compilers, and aggressively speculative optimizers powered by deterministic Hidden Classes—provides a proven, scalable blueprint. While the friction between raw, untyped machine code and Go's precise, concurrent garbage collector remains the architecture's primary tension point, methodologies such as dual memory mapping and software shadow stacks effectively bridge this divide. Ultimately, mastering these cross-boundary implementations unlocks the ability to build massive-scale, dynamically configurable systems where backend application logic evaluates at extreme velocities, combining the execution speed of raw JIT compilation with the operational simplicity, safety, and concurrency supremacy of the Go ecosystem.

