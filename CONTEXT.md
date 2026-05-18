# V8Go — Domain Glossary

## Core Concepts

- **V8Go** — A clean-room Go implementation of the V8 JavaScript engine. Not a wrapper.
- **Tier** — An execution level in the multi-tier pipeline. Tier 0 = interpreter, Tier 1 = Sparkplug (baseline JIT), Tier 2 = TurboFan (optimizing JIT).
- **Sparkplug** — Baseline JIT compiler. Linearly emits native code per bytecode. No SSA, no register allocation.
- **TurboFan** — Optimizing JIT compiler. Builds SSA Sea-of-Nodes IR, type-specializes, inlines, allocates registers.
- **Deoptimization** — Bailout from optimized native code back to interpreter when type guards fail.
- **Hidden Class (Shape)** — Structural identity for JS objects. Objects with same property layout share a Shape. Enables struct-like property access via fixed offsets.
- **Inline Cache (IC)** — Runtime code patching. NOP sled replaced with guard+direct-access when monomorphic shape is observed.
- **Shadow Stack** — GC-safe object reference array for JIT frames. Native code stores opaque indices; Go GC scans the shadow stack slice.
- **W^X** — Write XOR Execute memory protection. Memory page is either writable or executable, never both simultaneously.
- **Dual Mapping** — Two virtual addresses mapping to the same physical pages: one RW for writes, one RX for execution. Eliminates mprotect syscalls.

## Design Decisions

1. **W^X**: True dual-mapping on Linux (memfd_create + dual mmap), pure Go. Darwin uses single MAP_JIT mapping with CGO pthread_jit_write_protect_np (MAP_JIT incompatible with MAP_SHARED). This is the same approach Chrome V8 uses for Apple Silicon. ARM64 i-cache flush on Linux via DC CVAU + IC IVAU assembly.

2. **WeakRef**: Use `runtime.AddCleanup` to detect target collection. `deref()` returns `undefined` after GC collects target.

3. **WeakMap**: Use `runtime.AddCleanup` on keys for entry cleanup. Ephemeron property (value doesn't keep key alive) is NOT guaranteed — documented limitation.

4. **FinalizationRegistry**: Use `runtime.AddCleanup` on registered targets. Callbacks are best-effort (async, no ordering guarantee).

5. **Error.stack**: Level 1 adds source position to interpreter stack. Level 2 wires JIT frames into call stack tracking.

6. **Instanceof cross-realm**: Objects from different VM instances require separate RealmID tracking.

## ECMAScript Compatibility Boundaries

- WeakMap, WeakRef, FinalizationRegistry: Limited by Go GC architecture. Spec-compliant ephemeron semantics are not achievable in pure Go. Use `runtime.AddCleanup` for best-effort weak behavior.
- Error.stack: Source positions (file:line:col) tracked via parser AST positions and compiler instruction emission. JIT frame tracking is planned (Level 2).
- Instanceof: RealmID tracking with shared prototype exclusion. Cross-realm comparisons use ConstructorName fallback when prototypes differ across VM instances.

## VM Sub-Modules (Phase 7)

- **Allocator** — Bump allocators (LIFO) for VMFrame, JSValue registers, and JSObject. Owns the sync.Pool for JSObject fallback. Strict contract: overflow panics, underflow logs.
- **GlobalStore** — Three-path variable storage: named map, slot-indexed array, and fast-path typed array. Caller chooses path at compile time for performance.
- **CallTracker** — Execution state: call depth, call stack frames, step counter. Push/Pop/Reset methods.
- **Console** — Output sink: console log buffer and output callback function.
- **EventSystem** — DOM/browser event bridge: listener registration, event queue, onload handler, element lookup, DOM change callback.
- **Registry** — Function and bytecode cache: compiled function registry, builtin function table, bytecode result cache.
- **AsyncState** — Concurrency state: WaitGroup for async completion, promise reaction queue.

## JIT Integration (Phase 8)

- **JITCompiler** — Interface for compiling bytecode to native code. Two methods: CompileSparkplug (Tier 1 baseline), CompileTurboFan (Tier 2 optimizing).
- **ICPatcher** — Interface for runtime inline cache patching. Four methods: PatchMonomorphic, PatchPolymorphic, PatchMegamorphic, PatchStore.
- **ExecProtector** — Interface for memory protection toggling during JIT execution. Two methods: EnableExec, DisableExec.
- **DefaultBackend** — Single struct in pkg/jit implementing all three interfaces. Injected at VM construction via NewVMWithJIT.
- **NewVM** — No-arg constructor, creates VM without JIT (tests, scripting).
- **NewVMWithJIT** — Constructor accepting JITCompiler, ICPatcher, ExecProtector. Used in production.

## Object Model (Phase 9)

- **JSObject** — Core JavaScript object representation. 15 flat fields for the hot path (property storage, callable dispatch, shape, prototype) plus 4 pointer mixins for cold-path features.
- **InterceptorMixin** — Property interception callbacks (OnPropertyGet, OnPropertySet, OnHas, OnDelete). Used by DOM elements and Proxy targets (~5% of objects).
- **ProxyMixin** — Proxy target and handler references. Used only by `new Proxy()` (~0.01% of objects).
- **TypedArrayMixin** — ByteData backing store. Used by TypedArray objects (~5% of objects).
- **GeneratorMixin** — Suspended generator execution state. Used by generator objects (~2% of objects).
- **flags** — uint8 bitfield: bit 0 = Frozen, bit 1 = Sealed. Replaces two separate bool fields.

## Code Organization (Phase 10)

- **vm.go** — VM struct, constructors, call stack helpers. 204 lines (was 3222).
- **vm_exec.go** — Interpreter loop: executeOne, execute, executeFrame, opTable dispatch.
- **vm_jit.go** — JIT execution and tier promotion: executeSparkplug, executeTurboFan, maybePromoteTier.
- **vm_call.go** — Call dispatch: callMethod, opCall variants, function object construction.
- **vm_ops_arithmetic.go** — Arithmetic, bitwise, comparison, and logical opcode handlers.
- **vm_ops_property.go** — Property access, global variable, IC patching, and realm opcode handlers.
- **vm_ops_control.go** — Control flow: jump, return, throw, try/catch, for-in opcode handlers.
- **vm_generator.go** — Generator and async function execution.
- **vm_exceptions.go** — Error creation and exception throwing helpers.
- **vm_events.go** — DOM event dispatch handlers.

## Performance Optimizations (Phase 11)

- **RWMutex** — VM uses `sync.RWMutex` instead of `sync.Mutex`. Read paths (global variable access, event reads) use `RLock`. Write paths use `Lock`.
- **String Interning** — 16-shard FNV-32a hashed maps with per-shard mutexes. Replaces single global `stringInternMu` to reduce contention.
- **intKeys Pool** — Precomputed `[1024]string` slice ("0" through "1023"). Replaces `fmt.Sprintf("%d")` in array builtins for zero-allocation property key access.
- **JIT Throttling** — `CompilingJIT` atomic flag per function prevents duplicate compilation. Global semaphore limits concurrent compilations to `GOMAXPROCS`.
- **Deopt Counter** — `DeoptCount` tracks consecutive deoptimizations. After 5 deopts, JIT tiers are reset (Sparkplug, TurboFan, HasJITTier cleared) and the function is reinterpreted with fresh type feedback.
- **Shadow Stack** — Dynamic capacity doubling on overflow. Previously silently dropped pointers (use-after-free risk).
- **Peephole Optimizer** — Re-enabled LdaGlobal+Dup and StaGlobal+Dup patterns with backward-branch guard. Added LdaZero+Ldar dead code elimination. Star+Ldar and Ldar+Star excluded as unsafe (require register liveness analysis).

## JIT Roadmap (grill-with-docs session)

Performance improvements planned vs Chrome V8 reference:

- **P-1 AMD64 Completion** — 109 remaining Sparkplug ops in batches: property→call→object→exceptions→generator→misc. Goal: full ARM64 parity.
- **P-2 JSValue Escape Elimination** — Remove intermediate JSValues that feed directly into other ops without escaping the expression. Go-managed heap limits full object escape analysis.
- **P-3 Load Elimination** — Single-basic-block redundant property load elimination. Catches `obj.x + obj.x` pattern.
- **P-4 Polymorphic Inlining** — Guard chain for 2-4 target shapes, fall through to call. Currently only monomorphic inlining.
- **P-5 Concurrent Compilation** — Implemented: goroutine + semaphore throttling to GOMAXPROCS. No further work needed.
- **P-6 Recompilation** — Reset IC vector on deopt (after 5 deopts) to collect fresh type feedback. Recompile with new data.

Security hardening vs Chrome V8 reference:

- **SEC-1 Constant Blinding** — XOR immediate values with random cookie at compile time, XOR back at execution. Both Sparkplug and TurboFan tiers. Prevents JIT spraying attacks.
- **SEC-2 ARM64 PAC** — `paciasp` in prologue, `autiasp` before `ret`. Hardware pointer authentication on Apple Silicon (ARMv8.3+). Conditional on CPU feature.

## Removed Components

- **QuickJS** (formerly `pkg/js/engine.go`, `jsengine.go`, `eventloop.go`, `worker.go`, `scheduler.go`) — Removed entirely. Was a browser comparison engine with heavy CGO dependency (qjs, wazero). V8Go is the sole engine.
- **setTimeout/EventLoop** — Stub implementations only (`setTimeout`/`setInterval` return 0). Full event loop support can be built externally.
