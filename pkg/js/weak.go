// weak.go — WeakRef, WeakMap, and FinalizationRegistry infrastructure.
//
// Uses runtime.AddCleanup (Go 1.24+) for GC-aware weak references.
// When a JS object is no longer reachable from Go's GC perspective,
// the registered cleanup function fires, clearing the weak table entry.
package js

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

// --- Global weak reference table (WeakRef) ---

// weakRefIDSeq provides unique IDs for WeakRef objects.
var weakRefIDSeq atomic.Uint64

// weakRefMu protects the weakRefTable.
var weakRefMu sync.Mutex

// weakRefEntry holds a weak reference to a target.
// For object targets, ptr is stored as unsafe.Pointer (NOT traced by GC).
// This is what makes the reference "weak" — the Go GC doesn't see it.
// For primitive targets, value holds the JSValue directly.
type weakRefEntry struct {
	ptr   unsafe.Pointer // *JSObject pointer (not GC-traced)
	value JSValue        // value for non-object targets
	isObj bool           // true if the target is an object
}

// weakRefTable maps WeakRef IDs to their current target entries.
// Protected by weakRefMu.
var weakRefTable = make(map[uint64]weakRefEntry)

// clearWeakRef is the AddCleanup callback for WeakRef targets.
// It receives the WeakRef ID and deletes the entry from the weak table.
// IMPORTANT: this function does NOT reference the target object itself —
// if it did, runtime.AddCleanup would never fire.
func clearWeakRef(id uint64) {
	weakRefMu.Lock()
	delete(weakRefTable, id)
	weakRefMu.Unlock()
}

// storeWeakRef registers a target in the weak table and returns a unique ID.
// For object targets, stores an unsafe pointer so Go's GC can collect the target.
// For primitive targets, stores the JSValue directly (stack values, not heap).
func storeWeakRef(target JSValue) uint64 {
	id := weakRefIDSeq.Add(1)
	entry := weakRefEntry{}
	if target.IsObject() && target.ObjVal != nil {
		entry.ptr = unsafe.Pointer(target.ObjVal)
		entry.isObj = true
	} else {
		entry.value = target
		entry.isObj = false
	}
	weakRefMu.Lock()
	weakRefTable[id] = entry
	weakRefMu.Unlock()
	return id
}

// derefWeakRef retrieves the target from the weak table.
// Returns Undefined if the target has been collected.
func derefWeakRef(id uint64) JSValue {
	weakRefMu.Lock()
	entry, ok := weakRefTable[id]
	if !ok {
		weakRefMu.Unlock()
		return Undefined
	}
	if !entry.isObj {
		result := entry.value
		weakRefMu.Unlock()
		return result
	}
	// Convert unsafe pointer back to *JSObject while holding the lock.
	// The cleanup can't run concurrently because it also takes the lock,
	// so the memory is guaranteed valid at this point.
	obj := (*JSObject)(entry.ptr)
	result := NewObject(obj)
	weakRefMu.Unlock()
	return result
}

// registerWeakRefCleanup registers an AddCleanup on an object target.
// When the target is no longer reachable by Go's GC, clearWeakRef is called.
// Does nothing for non-object targets (they are never collected).
func registerWeakRefCleanup(target JSValue, id uint64) {
	if target.IsObject() && target.ObjVal != nil {
		runtime.AddCleanup(target.ObjVal, clearWeakRef, id)
	}
}

// --- Global finalization registry table (FinalizationRegistry) ---

// finalizationEntry holds a registered cleanup callback and held value.
type finalizationEntry struct {
	callback  JSValue // cleanup callback function
	heldValue JSValue // value passed to callback
	token     JSValue // unregister token (Undefined if none)
}

// finalizationMu protects the finalizationTable.
var finalizationMu sync.Mutex

// finalizationTable maps target pointers to their cleanup entries.
// Key is uintptr(unsafe.Pointer(*JSObject)) — NOT GC-traced.
var finalizationTable = make(map[uintptr][]finalizationEntry)

// addFinalizationCleanup registers finalization entries for a target.
// Returns true if this is the first registration for this pointer.
func addFinalizationCleanup(target JSValue, entries []finalizationEntry) bool {
	if !target.IsObject() || target.ObjVal == nil {
		return false
	}
	key := uintptr(unsafe.Pointer(target.ObjVal))
	finalizationMu.Lock()
	_, exists := finalizationTable[key]
	if !exists {
		finalizationTable[key] = entries
		finalizationMu.Unlock()
		return true
	}
	finalizationTable[key] = append(finalizationTable[key], entries...)
	finalizationMu.Unlock()
	return false
}

// invokeCleanupCallbacks is the AddCleanup callback for FinalizationRegistry targets.
// It looks up all cleanup entries for the target pointer and invokes callbacks.
func invokeCleanupCallbacks(key uintptr) {
	finalizationMu.Lock()
	entries := finalizationTable[key]
	delete(finalizationTable, key)
	finalizationMu.Unlock()

	for _, entry := range entries {
		if entry.callback.IsObject() && entry.callback.ObjVal != nil && entry.callback.ObjVal.isCallable() {
			// Call the cleanup callback with the held value.
			// Note: this runs in a separate goroutine (AddCleanup behavior).
			// Errors in callbacks are silently ignored per spec.
			entry.callback.ObjVal.CallFunc(nil, []JSValue{entry.heldValue})
		}
	}
}

// removeFinalizationToken removes entries matching a specific unregister token.
// Returns true if at least one entry was removed.
func removeFinalizationToken(target JSValue, token JSValue) bool {
	if !target.IsObject() || target.ObjVal == nil {
		return false
	}
	key := uintptr(unsafe.Pointer(target.ObjVal))
	finalizationMu.Lock()
	entries, ok := finalizationTable[key]
	if !ok {
		finalizationMu.Unlock()
		return false
	}
	newEntries := make([]finalizationEntry, 0, len(entries))
	removed := false
	for _, e := range entries {
		if sameJSValue(e.token, token) {
			removed = true
		} else {
			newEntries = append(newEntries, e)
		}
	}
	if removed {
		if len(newEntries) == 0 {
			delete(finalizationTable, key)
		} else {
			finalizationTable[key] = newEntries
		}
	}
	finalizationMu.Unlock()
	return removed
}

// --- WeakMap internal table ---

// weakMapMu protects the weakMapStore.
var weakMapMu sync.Mutex

// weakMapStore maps key pointer → (internal key → value).
// The outer key is uintptr(unsafe.Pointer(*JSObject)) — NOT GC-traced.
// The inner map uses a string key derived from the WeakMap instance identity.
var weakMapStore = make(map[uintptr]map[string]JSValue)

// weakMapCleanupRegistered tracks which target pointers already have AddCleanup.
var weakMapCleanupRegistered = make(map[uintptr]bool)

// clearWeakMapEntries is the AddCleanup callback for WeakMap keys.
// When the key object is collected, all its entries across all WeakMaps are removed.
func clearWeakMapEntries(key uintptr) {
	weakMapMu.Lock()
	delete(weakMapStore, key)
	delete(weakMapCleanupRegistered, key)
	weakMapMu.Unlock()
}

// weakMapSet stores a key-value pair for a WeakMap instance.
// Registers AddCleanup on the key if not already done.
func weakMapSet(instanceID string, key JSValue, value JSValue) {
	if !key.IsObject() || key.ObjVal == nil {
		return // non-object keys are silently ignored (spec: TypeError, but VM can't throw from builtins)
	}
	kptr := uintptr(unsafe.Pointer(key.ObjVal))

	weakMapMu.Lock()
	entries, ok := weakMapStore[kptr]
	if !ok {
		entries = make(map[string]JSValue, 4)
		weakMapStore[kptr] = entries
	}
	entries[instanceID] = value

	firstTime := !weakMapCleanupRegistered[kptr]
	if firstTime {
		weakMapCleanupRegistered[kptr] = true
	}
	weakMapMu.Unlock()

	if firstTime {
		runtime.AddCleanup(key.ObjVal, clearWeakMapEntries, kptr)
	}
}

// weakMapGet retrieves a value from a WeakMap instance by key.
func weakMapGet(instanceID string, key JSValue) JSValue {
	if !key.IsObject() || key.ObjVal == nil {
		return Undefined
	}
	kptr := uintptr(unsafe.Pointer(key.ObjVal))

	weakMapMu.Lock()
	entries, ok := weakMapStore[kptr]
	if !ok {
		weakMapMu.Unlock()
		return Undefined
	}
	val, ok := entries[instanceID]
	weakMapMu.Unlock()
	if !ok {
		return Undefined
	}
	return val
}

// weakMapHas checks if a key exists in a WeakMap instance.
func weakMapHas(instanceID string, key JSValue) bool {
	if !key.IsObject() || key.ObjVal == nil {
		return false
	}
	kptr := uintptr(unsafe.Pointer(key.ObjVal))

	weakMapMu.Lock()
	entries, ok := weakMapStore[kptr]
	if !ok {
		weakMapMu.Unlock()
		return false
	}
	_, ok = entries[instanceID]
	weakMapMu.Unlock()
	return ok
}

// weakMapDelete removes a key-value pair from a WeakMap instance.
// Returns true if the entry existed.
func weakMapDelete(instanceID string, key JSValue) bool {
	if !key.IsObject() || key.ObjVal == nil {
		return false
	}
	kptr := uintptr(unsafe.Pointer(key.ObjVal))

	weakMapMu.Lock()
	entries, ok := weakMapStore[kptr]
	if !ok {
		weakMapMu.Unlock()
		return false
	}
	_, ok = entries[instanceID]
	if ok {
		delete(entries, instanceID)
		if len(entries) == 0 {
			delete(weakMapStore, kptr)
		}
	}
	weakMapMu.Unlock()
	return ok
}

// sameJSValue compares two JSValues for identity-based equality.
// Used for unregister token matching.
func sameJSValue(a, b JSValue) bool {
	if a.Tag != b.Tag {
		return false
	}
	switch a.Tag {
	case TagUndefined, TagNull:
		return true
	case TagBoolean:
		return a.BoolVal == b.BoolVal
	case TagNumber:
		return a.NumVal == b.NumVal
	case TagString:
		return a.StrVal == b.StrVal
	case TagObject:
		return a.ObjVal == b.ObjVal
	default:
		return false
	}
}
