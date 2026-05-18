//go:build linux && arm64

#include "textflag.h"

// flushICache cleans D-cache and invalidates I-cache for the given range.
// Equivalent to __builtin___clear_cache(start, end) in C.
//
// On ARM64:
//   DC CVAU, Xn  — Clean data cache by VA to point of unification
//   DSB ISH       — Data synchronization barrier, inner shareable
//   IC IVAU, Xn   — Invalidate instruction cache by VA to point of unification
//   DSB ISH       — Data synchronization barrier
//   ISB           — Instruction synchronization barrier
//
// We iterate over 64-byte cache lines (the minimum line size on ARM64).
TEXT ·flushICache(SB), NOSPLIT, $0-16
    MOVD    addr+0(FP), R0
    MOVD    size+8(FP), R1
    ADD     R0, R1, R2          // R2 = end address

    // Align start down to cache line boundary (64 bytes).
    AND     $~63, R0

loop:
    CMP     R0, R2
    BGE     done

    // DC CVAU, X0 — Clean data cache by VA to point of unification.
    WORD    $0xd50b7b20        // DC CVAU, X0

    ADD     $64, R0
    B       loop

done:
    // DSB ISH — Data synchronization barrier.
    WORD    $0xd5033bbf        // DSB ISH

    // Recalculate start for I-cache invalidation.
    MOVD    addr+0(FP), R0
    AND     $~63, R0

ic_loop:
    CMP     R0, R2
    BGE     ic_done

    // IC IVAU, X0 — Invalidate instruction cache by VA to point of unification.
    WORD    $0xd50b7520        // IC IVAU, X0

    ADD     $64, R0
    B       ic_loop

ic_done:
    // DSB ISH
    WORD    $0xd5033bbf        // DSB ISH

    // ISB — Instruction synchronization barrier.
    WORD    $0xd5033fdf        // ISB

    RET
