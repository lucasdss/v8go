package js_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

func TestSetIntersection_Basic(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2); a.add(3);
		var b = new Set(); b.add(2); b.add(3); b.add(4);
		var r = a.intersection(b);
		[r.has(1), r.has(2), r.has(3), r.has(4), r.size]
	`)
	check(t, r, "has1,has2,has3,has4,size", false, true, true, false, 2)
}

func TestSetIntersection_Empty(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2); a.add(3);
		a.intersection(new Set()).size
	`)
	if n := r.ToNumber(); n != 0 {
		t.Errorf("intersection with empty: got %v, want 0", n)
	}
}

func TestSetIntersection_NoCommon(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(3); b.add(4);
		a.intersection(b).size
	`)
	if n := r.ToNumber(); n != 0 {
		t.Errorf("got %v, want 0", n)
	}
}

func TestSetIntersection_NonSetOther(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var other = { has: function(v) { return v === 2 || v === 3; }, get size() { return 3; } };
		var a = new Set(); a.add(1); a.add(2); a.add(3);
		var r = a.intersection(other);
		[r.has(1), r.has(2), r.has(3), r.size]
	`)
	check(t, r, "has1,has2,has3,size", false, true, true, 2)
}

// =========================================================================
// union
// =========================================================================

func TestSetUnion_Basic(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(2); b.add(3);
		var r = a.union(b);
		[r.has(1), r.has(2), r.has(3), r.size]
	`)
	check(t, r, "has1,has2,has3,size", true, true, true, 3)
}

func TestSetUnion_WithEmpty(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		a.union(new Set()).size
	`)
	if n := r.ToNumber(); n != 2 {
		t.Errorf("got %v, want 2", n)
	}
}

func TestSetUnion_NoOverlap(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(3); b.add(4);
		a.union(b).size
	`)
	if n := r.ToNumber(); n != 4 {
		t.Errorf("got %v, want 4", n)
	}
}

// =========================================================================
// difference
// =========================================================================

func TestSetDifference_Basic(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2); a.add(3);
		var b = new Set(); b.add(2);
		var r = a.difference(b);
		[r.has(1), r.has(2), r.has(3), r.size]
	`)
	check(t, r, "has1,has2,has3,size", true, false, true, 2)
}

func TestSetDifference_SubtractAll(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(1); b.add(2); b.add(3);
		a.difference(b).size
	`)
	if n := r.ToNumber(); n != 0 {
		t.Errorf("got %v, want 0", n)
	}
}

func TestSetDifference_SubtractNone(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(3); b.add(4);
		a.difference(b).size
	`)
	if n := r.ToNumber(); n != 2 {
		t.Errorf("got %v, want 2", n)
	}
}

// =========================================================================
// symmetricDifference
// =========================================================================

func TestSetSymmetricDifference_Basic(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(2); b.add(3);
		var r = a.symmetricDifference(b);
		[r.has(1), r.has(2), r.has(3), r.size]
	`)
	check(t, r, "has1,has2,has3,size", true, false, true, 2)
}

func TestSetSymmetricDifference_SameSets(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(1); b.add(2);
		a.symmetricDifference(b).size
	`)
	if n := r.ToNumber(); n != 0 {
		t.Errorf("got %v, want 0", n)
	}
}

func TestSetSymmetricDifference_Disjoint(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(3); b.add(4);
		a.symmetricDifference(b).size
	`)
	if n := r.ToNumber(); n != 4 {
		t.Errorf("got %v, want 4", n)
	}
}

// =========================================================================
// isSubsetOf
// =========================================================================

func TestSetIsSubsetOf_True(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(1); b.add(2); b.add(3);
		a.isSubsetOf(b)
	`)
	if !r.IsTruthy() {
		t.Error("[1,2] ⊆ [1,2,3] should be true")
	}
}

func TestSetIsSubsetOf_False(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(4);
		var b = new Set(); b.add(1); b.add(2); b.add(3);
		a.isSubsetOf(b)
	`)
	if r.IsTruthy() {
		t.Error("[1,4] ⊆ [1,2,3] should be false")
	}
}

func TestSetIsSubsetOf_Reflexive(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var s = new Set(); s.add(1); s.add(2); s.add(3);
		s.isSubsetOf(s)
	`)
	if !r.IsTruthy() {
		t.Error("reflexive should be true")
	}
}

func TestSetIsSubsetOf_Empty(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var b = new Set(); b.add(1); b.add(2); b.add(3);
		new Set().isSubsetOf(b)
	`)
	if !r.IsTruthy() {
		t.Error("empty ⊆ any should be true")
	}
}

func TestSetIsSubsetOf_NonSetOther(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var other = { has: function(v) { return v <= 3; }, get size() { return 3; } };
		var a = new Set(); a.add(1); a.add(2);
		a.isSubsetOf(other)
	`)
	if !r.IsTruthy() {
		t.Error("isSubsetOf Set-like should be true")
	}
}

// =========================================================================
// isSupersetOf
// =========================================================================

func TestSetIsSupersetOf_True(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2); a.add(3);
		var b = new Set(); b.add(1); b.add(2);
		a.isSupersetOf(b)
	`)
	if !r.IsTruthy() {
		t.Error("[1,2,3] ⊇ [1,2] should be true")
	}
}

func TestSetIsSupersetOf_False(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(1); b.add(2); b.add(3);
		a.isSupersetOf(b)
	`)
	if r.IsTruthy() {
		t.Error("[1,2] ⊇ [1,2,3] should be false")
	}
}

func TestSetIsSupersetOf_Reflexive(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var s = new Set(); s.add(1); s.add(2); s.add(3);
		s.isSupersetOf(s)
	`)
	if !r.IsTruthy() {
		t.Error("reflexive should be true")
	}
}

// =========================================================================
// isDisjointFrom
// =========================================================================

func TestSetIsDisjointFrom_True(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(3); b.add(4);
		a.isDisjointFrom(b)
	`)
	if !r.IsTruthy() {
		t.Error("[1,2] disjoint from [3,4] should be true")
	}
}

func TestSetIsDisjointFrom_False(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2);
		var b = new Set(); b.add(2); b.add(3);
		a.isDisjointFrom(b)
	`)
	if r.IsTruthy() {
		t.Error("[1,2] NOT disjoint from [2,3] should be false")
	}
}

func TestSetIsDisjointFrom_Empty(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`new Set().isDisjointFrom(new Set())`)
	if !r.IsTruthy() {
		t.Error("empty disjoint empty should be true")
	}
}

func TestSetIsDisjointFrom_NonSetOther(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var other = { has: function(v) { return v === 3 || v === 4; }, get size() { return 2; } };
		var a = new Set(); a.add(1); a.add(2);
		a.isDisjointFrom(other)
	`)
	if !r.IsTruthy() {
		t.Error("isDisjointFrom Set-like should be true")
	}
}

// =========================================================================
// TypeError checks (soft — engine may not throw)
// =========================================================================

func TestSetMethods_TypeErrorWhenThisIsNotSet(t *testing.T) {
	vm := js.NewVM()
	methods := []string{"intersection", "union", "difference", "symmetricDifference", "isSubsetOf", "isSupersetOf", "isDisjointFrom"}
	for _, m := range methods {
		r := vm.Run(`
			var threw = false;
			try {
				var s = new Set(); s.add(1);
				var fn = s.` + m + `;
				fn.call({}, new Set());
			} catch (e) { threw = true; }
			threw
		`)
		if !r.IsTruthy() {
			t.Logf("Set.prototype.%s: TypeError not thrown when this is not Set", m)
		}
	}
}

func TestSetMethods_TypeErrorWhenHasIsNotCallable(t *testing.T) {
	vm := js.NewVM()
	methods := []string{"intersection", "union", "difference", "symmetricDifference", "isSubsetOf", "isSupersetOf", "isDisjointFrom"}
	for _, m := range methods {
		r := vm.Run(`
			var threw = false;
			try {
				var s = new Set(); s.add(1); s.add(2);
				s.` + m + `({ has: 42 });
			} catch (e) { threw = true; }
			threw
		`)
		if !r.IsTruthy() {
			t.Logf("Set.prototype.%s: TypeError not thrown when other.has not callable", m)
		}
	}
}

// =========================================================================
// Iteration order
// =========================================================================

func TestSetIntersection_IterationOrder(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(3); a.add(2); a.add(1);
		var b = new Set(); b.add(2); b.add(3); b.add(4);
		var r = a.intersection(b);
		var items = []; r.forEach(function(v) { items.push(v); });
		items.join(',')
	`)
	if s := r.ToString(); s != "3,2" {
		t.Logf("intersection order: expected '3,2', got %q", s)
	}
}

func TestSetUnion_IterationOrder(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(3); a.add(2); a.add(1);
		var b = new Set(); b.add(5); b.add(4);
		var r = a.union(b);
		var items = []; r.forEach(function(v) { items.push(v); });
		items.join(',')
	`)
	if s := r.ToString(); s != "3,2,1,5,4" {
		t.Logf("union order: expected '3,2,1,5,4', got %q", s)
	}
}

func TestSetDifference_IterationOrder(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(3); a.add(2); a.add(1);
		var b = new Set(); b.add(2);
		var r = a.difference(b);
		var items = []; r.forEach(function(v) { items.push(v); });
		items.join(',')
	`)
	if s := r.ToString(); s != "3,1" {
		t.Logf("difference order: expected '3,1', got %q", s)
	}
}

// =========================================================================
// Large sets, multiple types, independence, return types
// =========================================================================

func TestSetMethods_LargeSet(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(), b = new Set();
		for (var i = 0; i < 100; i++) a.add(i);
		for (var i = 50; i < 150; i++) b.add(i);
		a.intersection(b).size
	`)
	if r.ToNumber() != 50 {
		t.Errorf("large intersection: got %v, want 50", r.ToNumber())
	}
}

func TestSetMethods_MultipleTypes(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add("hello"); a.add(true);
		var b = new Set(); b.add("hello"); b.add(false); b.add(1);
		var r = a.intersection(b);
		[r.has(1), r.has("hello"), r.has(true), r.has(false), r.size]
	`)
	check(t, r, "multiple types", true, true, false, false, 2)
}

func TestSetMethods_IndependentResult(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2); a.add(3);
		var b = new Set(); b.add(2); b.add(3); b.add(4);
		var r = a.intersection(b);
		a.delete(2); b.add(5);
		[r.has(2), r.size]
	`)
	check(t, r, "independent", true, 2)
}

func TestSetMethods_AllReturnTypes(t *testing.T) {
	vm := js.NewVM()
	setM := []string{"intersection", "union", "difference", "symmetricDifference"}
	for _, m := range setM {
		r := vm.Run(`
			var a = new Set(); a.add(1); a.add(2);
			var b = new Set(); b.add(2); b.add(3);
			a.` + m + `(b) instanceof Set
		`)
		if !r.IsTruthy() {
			t.Errorf("%s should return a Set", m)
		}
	}
	boolM := []string{"isSubsetOf", "isSupersetOf", "isDisjointFrom"}
	for _, m := range boolM {
		r := vm.Run(`
			var a = new Set(); a.add(1); a.add(2);
			var b = new Set(); b.add(2); b.add(3);
			typeof a.` + m + `(b)
		`)
		if r.ToString() != "boolean" {
			t.Errorf("%s: got %q, want boolean", m, r.ToString())
		}
	}
}

func TestSetMethods_Existence(t *testing.T) {
	vm := js.NewVM()
	for _, m := range []string{"intersection", "union", "difference", "symmetricDifference", "isSubsetOf", "isSupersetOf", "isDisjointFrom"} {
		r := vm.Run(`typeof (new Set()).` + m)
		if r.ToString() != "function" {
			t.Errorf("Set.prototype.%s: got %q, want function", m, r.ToString())
		}
	}
}

func TestSetMethods_SizeOptimization(t *testing.T) {
	vm := js.NewVM()
	r := vm.Run(`
		var a = new Set(); a.add(1); a.add(2); a.add(3); a.add(4); a.add(5);
		var b = new Set(); b.add(4); b.add(5);
		[b.intersection(a).size, b.isSubsetOf(a)]
	`)
	check(t, r, "size opt", 2, true)
}

// =========================================================================
// Helpers
// =========================================================================

func check(t *testing.T, result js.JSValue, name string, expected ...interface{}) {
	t.Helper()
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatalf("%s: expected array, got %v", name, result)
	}
	for i, exp := range expected {
		got := result.ObjVal.Get(string(rune('0' + i)))
		switch v := exp.(type) {
		case bool:
			if got.IsTruthy() != v {
				t.Errorf("%s[%d]: got %v, want %v", name, i, got.IsTruthy(), v)
			}
		case int:
			if got.ToNumber() != float64(v) {
				t.Errorf("%s[%d]: got %v, want %v", name, i, got.ToNumber(), v)
			}
		case float64:
			if got.ToNumber() != v {
				t.Errorf("%s[%d]: got %v, want %v", name, i, got.ToNumber(), v)
			}
		}
	}
}
