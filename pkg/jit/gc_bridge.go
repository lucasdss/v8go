// Package jit — GC bridge for JIT-compiled code.
//
// Implements the runtime/jit proposal (§6.3 of JIT-PROPOSAL.md):
// registers JIT code regions with the Go runtime so the GC can trace
// object pointers held in native JIT frames.
//
// The bridge provides three capabilities:
//  1. Next(pc, sp) — stack unwinding across JIT frames
//  2. ScanStack — reports GC roots in JIT frames
//  3. Preempt — cooperative scheduling for long-running JIT loops
//
// Thread safety: all exported functions are safe for concurrent use.
// Regions are assumed to be registered once at code emission time
// and rarely mutated thereafter (read-heavy workload).
package jit

import (
	"sort"
	"sync"
	"unsafe"
)

// Region represents a registered JIT code arena with GC callbacks.
// Each region maps a contiguous range of executable memory to the
// callbacks that let the runtime unwind and scan JIT frames.
//
// Regions must not overlap. Start ≤ PC < End (half-open interval).
type Region struct {
	Start uintptr // start address of executable code (inclusive)
	End   uintptr // end address (exclusive)

	// Next is called by the stack unwinder to step through a JIT frame.
	// Given the current pc and sp, returns the caller's pc, sp, and ok=true.
	// Returns ok=false if pc is not in this region's frames.
	Next func(pc, sp uintptr) (callerPC, callerSP uintptr, ok bool)

	// ScanStack reports GC roots within this JIT frame.
	// addRoot is called for each pointer the GC must mark.
	// The addRoot callback is safe to call from the GC worker.
	ScanStack func(pc, sp uintptr, addRoot func(ptr unsafe.Pointer))

	// Preempt is polled at loop back-edges. If it returns true, the JIT
	// code yields execution back to the Go scheduler.
	// May be nil if this region does not support preemption.
	Preempt func() bool
}

// MaxRegions limits the number of registered JIT regions to prevent
// unbounded growth from code cache churn.
const MaxRegions = 1024

// gcBinarySearchThreshold is the minimum number of regions at which
// findRegion switches from linear scan to binary search.
const gcBinarySearchThreshold = 16

var (
	regionMu              sync.RWMutex
	registeredRegionSlice []Region
)

// RegisterRegion adds a JIT code region to the GC's knowledge.
// Must be called after code is emitted and made executable.
// The region callbacks must remain valid for the lifetime of the code.
//
// Returns false if MaxRegions is exceeded.
func RegisterRegion(r Region) bool {
	regionMu.Lock()
	defer regionMu.Unlock()
	if len(registeredRegionSlice) >= MaxRegions {
		return false
	}
	registeredRegionSlice = append(registeredRegionSlice, r)
	return true
}

// UnregisterRegion removes all regions in the given address range.
// Called when JIT code is freed (e.g., code cache eviction).
// Uses a single-pass compaction to avoid allocation.
func UnregisterRegion(start, end uintptr) {
	regionMu.Lock()
	defer regionMu.Unlock()
	n := 0
	for _, r := range registeredRegionSlice {
		if r.Start < start || r.Start >= end {
			registeredRegionSlice[n] = r
			n++
		}
	}
	registeredRegionSlice = registeredRegionSlice[:n]
}

// findRegion returns the region containing pc, or nil.
// Uses binary search when there are many regions (>16), linear scan otherwise.
// Must be called with regionMu held (read lock sufficient).
func findRegion(pc uintptr) *Region {
	if len(registeredRegionSlice) <= gcBinarySearchThreshold {
		for i := range registeredRegionSlice {
			if pc >= registeredRegionSlice[i].Start && pc < registeredRegionSlice[i].End {
				return &registeredRegionSlice[i]
			}
		}
		return nil
	}

	// Binary search: find the region whose start ≤ pc < end.
	// Regions are ordered by Start (maintained by RegisterRegion).
	idx := sort.Search(len(registeredRegionSlice), func(i int) bool {
		return registeredRegionSlice[i].Start > pc
	})
	idx-- // back up to the last region with Start ≤ pc
	if idx >= 0 && pc < registeredRegionSlice[idx].End {
		return &registeredRegionSlice[idx]
	}
	return nil
}

// NextJITFrame implements the Next callback for Go's stack unwinder.
// Given a PC in JIT code, returns the caller's PC and SP.
// Returns ok=false if the PC is not in any registered JIT region.
//
// Called by the Go runtime during stack unwinding (e.g., for profiling
// or GC stack scanning). Must be signal-safe.
func NextJITFrame(pc, sp uintptr) (callerPC, callerSP uintptr, ok bool) {
	regionMu.RLock()
	r := findRegion(pc)
	regionMu.RUnlock()
	if r != nil && r.Next != nil {
		return r.Next(pc, sp)
	}
	return 0, 0, false
}

// ScanJITStack reports GC roots for a JIT frame at the given PC/SP.
// Calls addRoot for each pointer the GC must mark.
//
// Called by the Go GC during the mark phase. addRoot must only be called
// with valid heap pointers.
func ScanJITStack(pc, sp uintptr, addRoot func(unsafe.Pointer)) {
	regionMu.RLock()
	r := findRegion(pc)
	regionMu.RUnlock()
	if r != nil && r.ScanStack != nil {
		r.ScanStack(pc, sp, addRoot)
	}
}

// PreemptJIT checks whether any registered JIT region requests preemption.
// Returns true if execution should yield to the Go scheduler.
//
// Polled at loop back-edges by JIT-compiled code. The preemption check
// must be fast (O(regions) in the worst case, typically O(1)).
func PreemptJIT() bool {
	regionMu.RLock()
	defer regionMu.RUnlock()
	for i := range registeredRegionSlice {
		if registeredRegionSlice[i].Preempt != nil && registeredRegionSlice[i].Preempt() {
			return true
		}
	}
	return false
}

// NumRegions returns the number of registered JIT regions.
func NumRegions() int {
	regionMu.RLock()
	defer regionMu.RUnlock()
	return len(registeredRegionSlice)
}
