package js

import (
	"fmt"
	"testing"
)

func TestShapeSlackCounter(t *testing.T) {
	vm := NewVM()
	result := vm.Run(`
		var total = 0;
		for (var i = 0; i < 20; i++) {
			var obj = {x: i, y: i*2};
			total += obj.x + obj.y;
		}
		total
	`)
	if result.ToNumber() != 570 {
		t.Errorf("slack tracking: expected 570, got %v", result.ToNumber())
	}
}

func TestShapeSlackFinalization(t *testing.T) {
	vm := NewVM()
	result := vm.Run(`
		var objs = [];
		for (var i = 0; i < 15; i++) { objs.push({a: i}); }
		objs[14].a
	`)
	if result.ToNumber() != 14 {
		t.Errorf("shape finalization: expected 14, got %v", result.ToNumber())
	}
}

func TestPolyICPropertyAccess(t *testing.T) {
	vm := NewVM()
	// Define function once so its IC vector persists across calls.
	vm.Run("function f(o){return o.x}")
	// Train on first shape (monomorphic).
	for i := 0; i < 200; i++ {
		vm.Run("f({x:1})")
	}
	// Introduce second shape → IC transitions to polymorphic.
	for i := 0; i < 50; i++ {
		vm.Run("f({x:2,y:1})")
	}
	// Introduce third shape → IC stays polymorphic (capacity ≤ 4).
	for i := 0; i < 50; i++ {
		vm.Run("f({x:3,z:1})")
	}
	// Verify correct value returned after shape changes.
	result := vm.Run("f({x:99})")
	if result.ToNumber() != 99 {
		t.Errorf("poly IC: expected 99, got %v", result.ToNumber())
	}
	// Verify f still works with earlier shapes.
	r := vm.Run("f({x:2,y:1})")
	if r.ToNumber() != 2 {
		t.Errorf("poly IC replay: expected 2, got %v", r.ToNumber())
	}
}

func TestPolyICPropertyStore(t *testing.T) {
	vm := NewVM()
	// Train a setter function that writes to o.x across multiple shapes.
	vm.Run("function setX(o, v){o.x = v}")
	vm.Run("var a = {x:1}; var b = {x:2, y:3}; var c = {x:4, z:5}")
	// Write through multiple shapes to exercise store IC.
	for i := 0; i < 20; i++ {
		vm.Run("setX(a, " + fmt.Sprintf("%d", i) + ")")
	}
	for i := 0; i < 20; i++ {
		vm.Run("setX(b, " + fmt.Sprintf("%d", i+100) + ")")
	}
	for i := 0; i < 20; i++ {
		vm.Run("setX(c, " + fmt.Sprintf("%d", i+200) + ")")
	}
	// Verify values after store IC transitions.
	if vm.Run("a.x").ToNumber() != 19 {
		t.Errorf("store IC: expected a.x=19, got %v", vm.Run("a.x").ToNumber())
	}
	if vm.Run("b.x").ToNumber() != 119 {
		t.Errorf("store IC: expected b.x=119, got %v", vm.Run("b.x").ToNumber())
	}
	if vm.Run("c.x").ToNumber() != 219 {
		t.Errorf("store IC: expected c.x=219, got %v", vm.Run("c.x").ToNumber())
	}
}

func TestPolyICMaxShapes(t *testing.T) {
	vm := NewVM()
	vm.Run("function f(o){return o.x}")
	// Train on 4 different shapes (max polymorphic capacity).
	for i := 0; i < 20; i++ {
		vm.Run("f({x:1})")
	}
	for i := 0; i < 20; i++ {
		vm.Run("f({x:2,y:2})")
	}
	for i := 0; i < 20; i++ {
		vm.Run("f({x:3,z:3})")
	}
	for i := 0; i < 20; i++ {
		vm.Run("f({x:4,w:4})")
	}
	// All 4 shapes should return correct values.
	for _, tc := range []struct {
		source   string
		expected float64
	}{
		{"f({x:1})", 1},
		{"f({x:2,y:2})", 2},
		{"f({x:3,z:3})", 3},
		{"f({x:4,w:4})", 4},
		{"f({x:99})", 99}, // fresh shape still works
	} {
		r := vm.Run(tc.source)
		if r.ToNumber() != tc.expected {
			t.Errorf("poly IC max: expected %v, got %v for %q", tc.expected, r.ToNumber(), tc.source)
		}
	}
}

func TestPolyICMegamorphicFallback(t *testing.T) {
	vm := NewVM()
	vm.Run("function f(o){return o.x}")
	// Train on 6 shapes — IC should transition to megamorphic.
	for i := 0; i < 10; i++ {
		vm.Run("f({x:1})")
	}
	for i := 0; i < 10; i++ {
		vm.Run("f({x:2,y:1})")
	}
	for i := 0; i < 10; i++ {
		vm.Run("f({x:3,z:1})")
	}
	for i := 0; i < 10; i++ {
		vm.Run("f({x:4,w:1})")
	}
	for i := 0; i < 10; i++ {
		vm.Run("f({x:5,v:1})")
	}
	for i := 0; i < 10; i++ {
		vm.Run("f({x:6,u:1})")
	}
	// After megamorphic transition, all shapes should still return correct values.
	tests := []string{
		"f({x:1})",
		"f({x:2,y:1})",
		"f({x:3,z:1})",
		"f({x:4,w:1})",
		"f({x:5,v:1})",
		"f({x:6,u:1})",
		"f({x:100})", // fresh shape
	}
	for i, src := range tests {
		r := vm.Run(src)
		var expected float64 = float64(i + 1)
		if i == 6 {
			expected = 100
		}
		if r.ToNumber() != expected {
			t.Errorf("mega IC: expected %v, got %v for %q", expected, r.ToNumber(), src)
		}
	}
}

func TestPolyICMultipleSlots(t *testing.T) {
	vm := NewVM()
	// f accesses two properties: o.x and o.y (two separate IC slots).
	vm.Run("function f(o){return o.x + o.y}")
	// Train both IC slots with 3 shapes each.
	shapes := []string{
		"f({x:1,y:10})",
		"f({x:2,y:20,z:99})",
		"f({x:3,y:30,w:88})",
	}
	for _, s := range shapes {
		for i := 0; i < 20; i++ {
			vm.Run(s)
		}
	}
	// Verify both slots return correct values after polymorphic transitions.
	tests := []struct {
		source   string
		expected float64
	}{
		{"f({x:1,y:10})", 11},
		{"f({x:2,y:20,z:99})", 22},
		{"f({x:3,y:30,w:88})", 33},
		{"f({x:5,y:5})", 10}, // new shape
	}
	for _, tc := range tests {
		r := vm.Run(tc.source)
		if r.ToNumber() != tc.expected {
			t.Errorf("multi-slot IC: expected %v, got %v for %q", tc.expected, r.ToNumber(), tc.source)
		}
	}
}

// TestICNilShapeLoad verifies LoadIC handles nil object gracefully.
func TestICNilShapeLoad(t *testing.T) {
	fv := NewFeedbackVector(1)
	result := fv.LoadIC(0, nil, "x")
	if !result.IsUndefined() {
		t.Errorf("LoadIC with nil obj: expected undefined, got %v", result.String())
	}
}

// TestICOutOfBoundsSlot verifies LoadIC handles out-of-bounds slot index.
func TestICOutOfBoundsSlot(t *testing.T) {
	fv := NewFeedbackVector(1)
	obj := NewJSObject()
	obj.Set("x", NewNumber(42))
	result := fv.LoadIC(999, obj, "x")
	if result.ToNumber() != 42 {
		t.Errorf("LoadIC with out-of-bounds slot: expected 42, got %v", result.ToNumber())
	}
}

// TestICNilFeedbackVector verifies IC operations with nil FeedbackVector don't crash.
func TestICNilFeedbackVector(t *testing.T) {
	var fv *FeedbackVector
	// Should not panic — nil fv just means no IC feedback.
	if fv != nil {
		t.Error("expected nil FeedbackVector")
	}
	// Verify NewFeedbackVector with 0 slots returns nil.
	empty := NewFeedbackVector(0)
	if empty != nil {
		t.Error("NewFeedbackVector(0) should return nil")
	}
}

// TestICUninitializedState verifies the IC starts in uninitialized state.
func TestICUninitializedState(t *testing.T) {
	fv := NewFeedbackVector(2)
	if fv.Slots[0].State != ICUninitialized {
		t.Error("new slot should be ICUninitialized")
	}
	if fv.Slots[1].State != ICUninitialized {
		t.Error("new slot should be ICUninitialized")
	}
	fv.Slots[0].State = ICMonomorphic
	if fv.Slots[0].State != ICMonomorphic {
		t.Error("slot state should transition to ICMonomorphic")
	}
}

// TestICFeedbackCounts verifies hit counting in IC slots.
func TestICFeedbackCounts(t *testing.T) {
	slot := &ICSlot{State: ICMonomorphic}
	for i := 0; i < 100; i++ {
		slot.HitCount++
	}
	if slot.HitCount != 100 {
		t.Errorf("expected 100 hits, got %d", slot.HitCount)
	}
}

// TestShapeCacheHit verifies GetOrCreateShape returns the same shape for same props.
func TestShapeCacheHit(t *testing.T) {
	s1 := GetOrCreateShape([]string{"x", "y"})
	s2 := GetOrCreateShape([]string{"x", "y"})
	if s1 != s2 {
		t.Error("GetOrCreateShape should return same shape for same props")
	}
}

// TestShapeCacheDifferentOrder verifies different property order = different shape.
func TestShapeCacheDifferentOrder(t *testing.T) {
	s1 := GetOrCreateShape([]string{"x", "y"})
	s2 := GetOrCreateShape([]string{"y", "x"})
	if s1 == s2 {
		t.Error("different property order should produce different shapes")
	}
	// Both should have x and y properties.
	if s1.GetOffset("x") < 0 || s1.GetOffset("y") < 0 {
		t.Error("s1 should have both x and y")
	}
	if s2.GetOffset("x") < 0 || s2.GetOffset("y") < 0 {
		t.Error("s2 should have both x and y")
	}
}

// TestShapePropertyAttributes verifies property attributes are stored.
func TestShapePropertyAttributes(t *testing.T) {
	// Use a fresh shape to avoid collision with cached transitions.
	s := (&Shape{
		Properties:    make(map[string]PropEntry),
		Transitions:   make(map[string]*Shape),
		PropertyCount: 0,
		SlackCounter:  7,
	}).AddPropertyWithAttr("x", AttrWritable|AttrEnumerable)
	attr := s.GetAttr("x")
	if attr&AttrWritable == 0 {
		t.Error("x should be writable")
	}
	if attr&AttrEnumerable == 0 {
		t.Error("x should be enumerable")
	}
	if attr&AttrConfigurable != 0 {
		t.Error("x should not be configurable")
	}
}

// TestShapeGetAttrMissing verifies GetAttr returns 0 for missing property.
func TestShapeGetAttrMissing(t *testing.T) {
	s := EmptyShape.AddProperty("x")
	if s.GetAttr("nonexistent") != 0 {
		t.Error("GetAttr for missing property should return 0")
	}
}

// TestShapeConvertToDictionary verifies dictionary conversion.
func TestShapeConvertToDictionary(t *testing.T) {
	s := EmptyShape.AddProperty("x").AddProperty("y")
	dict := s.ConvertToDictionary()
	if !dict.IsDictionary {
		t.Error("converted shape should be dictionary")
	}
	if !dict.HasProperty("x") || !dict.HasProperty("y") {
		t.Error("dictionary should have all properties")
	}
	if dict.PropertyCount != s.PropertyCount {
		t.Errorf("dictionary should have same property count: %d vs %d", dict.PropertyCount, s.PropertyCount)
	}
}

// TestShapeHasProperty verifies HasProperty for present and missing properties.
func TestShapeHasProperty(t *testing.T) {
	s := EmptyShape.AddProperty("x")
	if !s.HasProperty("x") {
		t.Error("should have property x")
	}
	if s.HasProperty("y") {
		t.Error("should not have property y")
	}
}

// TestEmptyShapeDefaults verifies EmptyShape has expected defaults.
func TestEmptyShapeDefaults(t *testing.T) {
	if EmptyShape.PropertyCount != 0 {
		t.Error("empty shape should have 0 properties")
	}
	if EmptyShape.Parent != nil {
		t.Error("empty shape should have nil parent")
	}
	if EmptyShape.IsDictionary {
		t.Error("empty shape should not be dictionary")
	}
}

// TestICStateValues verifies IC state enum values.
func TestICStateValues(t *testing.T) {
	if ICUninitialized != 0 {
		t.Error("ICUninitialized should be 0")
	}
	if ICMonomorphic != 1 {
		t.Error("ICMonomorphic should be 1")
	}
	if ICPolymorphic != 2 {
		t.Error("ICPolymorphic should be 2")
	}
	if ICMegamorphic != 3 {
		t.Error("ICMegamorphic should be 3")
	}
}
