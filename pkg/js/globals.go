// globals.go — Global variable storage with slot-based fast path.
//
// The GlobalStore maintains the JavaScript global scope: a name→value map
// for slow-path lookups, plus a fixed-size slot array for compile-time
// resolved fast-path access (direct array indexing instead of hash lookup).
package js

// GlobalStore holds global variables with a slot-based fast path.
// Slot indices are assigned at compile time (see assignGlobalSlots).
// Direct array indexing: globalSlots[slot] = value (no hash lookup).
//
// The M field provides direct map access for backward compatibility with
// code that uses vm.globals["key"] patterns. Use Set() when slot cache
// updates are needed.
//
// The GlobalStore is NOT goroutine-safe — the caller (VM.mu) provides
// synchronization.
type GlobalStore struct {
	M           map[string]JSValue
	slots       []JSValue
	slotsSet    []bool
	slotNames   []string
	globalThis  JSValue
}

// NewGlobalStore creates a GlobalStore with pre-allocated capacity.
func NewGlobalStore() *GlobalStore {
	return &GlobalStore{
		M:     make(map[string]JSValue, 128),
		slots: make([]JSValue, 0, 64),
	}
}

// Get returns the value of a global variable by name, or Undefined if not found.
func (gs *GlobalStore) Get(name string) JSValue {
	if val, ok := gs.M[name]; ok {
		return val
	}
	return Undefined
}

// Lookup returns the value and whether it exists.
func (gs *GlobalStore) Lookup(name string) (JSValue, bool) {
	val, ok := gs.M[name]
	return val, ok
}

// Set stores a global variable and updates any slot that maps to this name.
func (gs *GlobalStore) Set(name string, val JSValue) {
	gs.M[name] = val
	for slot, slotName := range gs.slotNames {
		if slotName == name {
			gs.slots[slot] = val
			gs.slotsSet[slot] = true
		}
	}
}

// Has returns true if the named global variable exists.
func (gs *GlobalStore) Has(name string) bool {
	_, ok := gs.M[name]
	return ok
}

// SlotCount returns the current number of global slots.
func (gs *GlobalStore) SlotCount() int {
	return len(gs.slots)
}

// HasSlot returns true if the given slot index has been written.
func (gs *GlobalStore) HasSlot(idx int) bool {
	return idx < len(gs.slots) && gs.slotsSet[idx]
}

// GetSlot returns the value at the given slot index.
// Caller must ensure idx is within bounds.
func (gs *GlobalStore) GetSlot(idx int) JSValue {
	return gs.slots[idx]
}

// SetSlot sets the value at the given slot index and marks it as written.
func (gs *GlobalStore) SetSlot(idx int, val JSValue) {
	gs.slots[idx] = val
	gs.slotsSet[idx] = true
}

// SetSlotWithName sets the value, marks the slot, records the name, and
// writes through to the globals map. Used by opStaGlobalSlot.
func (gs *GlobalStore) SetSlotWithName(idx int, val JSValue, name string) {
	gs.slots[idx] = val
	gs.slotsSet[idx] = true
	gs.slotNames[idx] = name
	gs.M[name] = val
}

// GetSlotName returns the name recorded for a slot, and whether the slot
// exists within bounds.
func (gs *GlobalStore) GetSlotName(idx int) (string, bool) {
	if idx < len(gs.slotNames) {
		return gs.slotNames[idx], true
	}
	return "", false
}

// SlotNameMatches returns true if the slot exists and its recorded name matches.
func (gs *GlobalStore) SlotNameMatches(idx int, name string) bool {
	return idx < len(gs.slotNames) && gs.slotNames[idx] == name
}

// EnsureSlots grows the slots/slotsSet/slotNames arrays to accommodate at
// least n slots. Existing values are preserved.
func (gs *GlobalStore) EnsureSlots(n int) {
	if n > len(gs.slots) {
		newSlots := make([]JSValue, n)
		copy(newSlots, gs.slots)
		gs.slots = newSlots
		newSet := make([]bool, n)
		copy(newSet, gs.slotsSet)
		gs.slotsSet = newSet
		newNames := make([]string, n)
		copy(newNames, gs.slotNames)
		gs.slotNames = newNames
	}
}

// GlobalThis returns the cached global object reference.
func (gs *GlobalStore) GlobalThis() JSValue {
	return gs.globalThis
}

// SetGlobalThis stores the global object reference.
func (gs *GlobalStore) SetGlobalThis(v JSValue) {
	gs.globalThis = v
}
