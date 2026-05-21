# V8Go Performance Deep-Dive

A detailed analysis of the performance gap between V8Go and Chrome V8, and where V8Go excels.

## The ~25-30x Single-Operation Gap

Running `1 + 2` in V8Go takes ~55 ns. Chrome V8 does it in ~2 ns. Here's why the gap exists, broken down by root cause.

### Factor 1: Smi Tagging (Integer Representation)

**What V8 does:** Integers are stored as 64-bit tagged pointers. The LSB indicates "this is a Smi, not a pointer." Arithmetic is pure register operations — zero memory access.

```
V8:   1 + 2  →  3 register operations, 0 allocations
V8Go: 1 + 2  →  read 48-byte struct, compute, write 48-byte struct, 1 allocation
```

**Why V8Go can't match it:** Go's garbage collector scans every 8-byte aligned value for valid heap pointers. If we used tagged integers, Go's GC would interpret them as corrupted pointers and crash. V8 uses its own GC (Orinoco) that knows about the tagging scheme.

**V8Go mitigations:**
- `smallIntPool` — pre-allocates integers -128..127 (avoiding heap allocation)
- Bump allocator — LIFO stack allocation for register values (zero GC)
- `OpAddNumber` fast path — Sparkplug JIT skips type checks when both operands are numbers
- StrVal/SymVal merge — reduced JSValue from 64B to 48B (25% reduction, ~5% speed improvement)

### Factor 2: Pointer Compression (Cache Density)

**What V8 does:** 64-bit heap pointers are compressed to 32-bit offsets within a 4GB "heap cage." This doubles the number of objects that fit in CPU cache.

```
V8:    object reference = 4 bytes (32-bit offset from heap base)
V8Go: object reference = 8 bytes (full 64-bit Go pointer)
```

**Why V8Go can't match it:** Go's GC only traces full 64-bit pointers. A 32-bit offset is invisible to the GC, so the pointed-to object would be prematurely collected. A side-table mapping compressed offsets to full pointers defeats the purpose.

**V8Go mitigations:**
- JSValue struct size optimization (64B → 48B)
- Pointer mixins for cold JSObject fields (saves 48-72 bytes per object)

### Factor 3: Inline JIT Code (Property Access Speed)

**What V8 does:** Monomorphic property access compiles to ~2 inline machine instructions (guard + direct load). ~5 CPU cycles.

```asm
CMP [obj+shape_offset], expected_shape   ; 1 cycle guard
JE  skip_slow                            ; predicted not taken
MOV rax, [obj+prop_offset]              ; 3 cycles direct load
```

**What V8Go does:** Property access goes through Go function calls, multiple type checks, and inline cache lookups. ~100+ CPU cycles.

```go
func opLdaNamedProperty(vm *VM, frame *VMFrame, instr Instruction) {
    if !frame.Acc.IsObject() || frame.Acc.ObjVal == nil { ... }  // nil check
    obj := frame.Acc.ObjVal
    slot := frame.Func.ICVector.Slots[instr.OperandC]            // IC lookup
    if slot.State == ICMonomorphic && obj.Shape == slot.Shape {  // guard check
        frame.Acc = obj.propAt(slot.Offset)                     // direct access
    } else {
        // slow path: prototype chain walk, dictionary lookup
    }
}
```

**Why V8Go can't match it:** Go doesn't support inline assembly in function bodies. The Sparkplug JIT CAN emit inline assembly, but most property operations delegate to Go helper functions. True inline property access would require emitting the full guard chain in assembly — this is possible but not yet implemented for most property operations.

**V8Go mitigations:**
- IC patching — NOP sleds replaced with guard+direct-offset at runtime
- Sparkplug JIT — eliminates interpreter dispatch overhead
- TurboFan GVN + load elimination — removes redundant property accesses

## Where V8Go Wins

### Zero-Cost Go↔JS Interop

| Approach | Call overhead | Why |
|----------|--------------|-----|
| V8Go | **~0 ns** | JS objects are Go structs. Same heap, no marshaling. |
| CGO V8 wrapper | **~100 ns** | Thread pinning, cgo stack switch, argument copy per call. |

For workloads with frequent Go↔JS calls (>10K/sec), V8Go's zero-cost interop wins over CGO V8 wrappers. A CGO V8 wrapper spends more time crossing the FFI boundary than executing the JS.

### JIT Tier Speedups

| Tier | 100-iteration loop | Speedup |
|------|-------------------|---------|
| Interpreter | 11.8 µs | 1.0x |
| Sparkplug (Tier 1) | 6.8 µs | **1.7x** |
| TurboFan (Tier 2) | 6.8 µs | **1.7x** |

| Operation | Interpreter | JIT | Speedup |
|-----------|-------------|-----|---------|
| Object creation | 209 ns | 111 ns | **2.0x** |
| Loop (100 iter) | 11.8 µs | 6.8 µs | **1.7x** |

### Memory Efficiency

| Metric | V8Go |
|--------|------|
| JSValue struct | 48 bytes (down from 64) |
| JSObject (without mixins) | ~120 bytes |
| JSObject (with all mixins) | ~200 bytes |
| VM bump allocator | 2048 regs, 64 frames, 64 objects preallocated |
| Dependencies | 2 (golang.org/x/net, x/sys) |

## Summary

| Limitation | Root Cause | Irreducible? | Mitigation Status |
|------------|-----------|-------------|-------------------|
| Smi tagging | Go GC architecture | Yes | smallIntPool + bump allocator + Sparkplug fast paths |
| Pointer compression | Go GC architecture | Yes | JSValue size reduction (64→48B), mixins |
| Inline JIT code | Go↔assembly boundary | Partially | IC patching, Sparkplug emits assembly, TurboFan GVN |
| Go↔JS interop | Architecture | N/A — V8Go advantage | Zero-cost (shared memory, same heap) |

The 25-30x gap on single operations is structural — it's the price of Go's safe, managed runtime. The 1.7-2.0x speedups on loops and objects show where V8Go's JIT can close the gap. And for the most important real-world metric — Go↔JS interop overhead — V8Go is categorically superior to CGO-based alternatives.
