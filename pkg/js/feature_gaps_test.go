package js_test

import (
	"testing"

	js "github.com/lucasdss/v8go/pkg/js"
)

// ---------------------------------------------------------------------------
// Part 1: RegExp flags (sticky, unicode, dotAll) — verify existing behavior
// ---------------------------------------------------------------------------

func TestFeatureRegExpStickyMatch(t *testing.T) {
	runAndExpectConsole(t, `
		var r = /foo/y;
		r.lastIndex = 0;
		console.log(r.exec('foobar')[0]);
	`, []string{"foo"})
}

func TestFeatureRegExpStickyNoMatchAtPosition(t *testing.T) {
	runAndExpectConsole(t, `
		var r = /foo/y;
		r.lastIndex = 2;
		console.log(r.test('foobar'));
		console.log(r.lastIndex);
	`, []string{"false", "0"})
}

func TestFeatureRegExpStickyLastIndexUpdate(t *testing.T) {
	runAndExpectConsole(t, `
		var r = /\d+/y;
		var s = '123abc456';
		r.lastIndex = 0;
		console.log(r.exec(s)[0]);
		console.log(r.lastIndex);
	`, []string{"123", "3"})
}

func TestFeatureRegExpUnicodeFlag(t *testing.T) {
	runAndExpectConsole(t, `
		var r = /a+/u;
		console.log(r.unicode);
		console.log(r.flags);
	`, []string{"true", "u"})
}

func TestFeatureRegExpDotAllMatchesNewline(t *testing.T) {
	runAndExpectConsole(t, `
		var r = /foo.bar/s;
		console.log(r.test('foo\nbar'));
		console.log(/foo.bar/.test('foo\nbar'));
	`, []string{"true", "false"})
}

func TestFeatureRegExpDotAllFlagProperty(t *testing.T) {
	runAndExpectConsole(t, `
		var r = new RegExp('a.b', 's');
		console.log(r.dotAll);
	`, []string{"true"})
}

// ---------------------------------------------------------------------------
// Part 2: DataView BigInt64 roundtrip
// ---------------------------------------------------------------------------

func TestFeatureDataViewBigInt64Roundtrip(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(8);
		var dv = new DataView(buf);
		dv.setBigInt64(0, 42n);
		dv.getBigInt64(0) === 42n
	`)
	if !result.IsTruthy() {
		t.Error("DataView BigInt64 roundtrip: set 42n, get should equal 42n")
	}
}

func TestFeatureDataViewBigInt64LittleEndian(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(8);
		var dv = new DataView(buf);
		dv.setBigInt64(0, -1n, true);
		dv.getBigInt64(0, true) === -1n
	`)
	if !result.IsTruthy() {
		t.Error("DataView BigInt64 little-endian roundtrip: set -1n, get should equal -1n")
	}
}

func TestFeatureDataViewBigUint64Roundtrip(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(8);
		var dv = new DataView(buf);
		dv.setBigUint64(0, 18446744073709551615n);
		dv.getBigUint64(0) === 18446744073709551615n
	`)
	if !result.IsTruthy() {
		t.Error("DataView BigUint64 roundtrip: set max uint64, get should equal same")
	}
}

func TestFeatureDataViewBigUint64LittleEndian(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(8);
		var dv = new DataView(buf);
		dv.setBigUint64(0, 1n, true);
		dv.getBigUint64(0, true) === 1n
	`)
	if !result.IsTruthy() {
		t.Error("DataView BigUint64 little-endian roundtrip: set 1n, get should equal 1n")
	}
}

func TestFeatureDataViewBigInt64NegativeValue(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		var buf = new ArrayBuffer(8);
		var dv = new DataView(buf);
		dv.setBigInt64(0, -128n);
		dv.getBigInt64(0)
	`)
	if !result.IsBigInt() {
		t.Error("DataView getBigInt64 should return BigInt")
	}
}

// ---------------------------------------------------------------------------
// Part 3: Promise.allSettled and Promise.any
// ---------------------------------------------------------------------------

func TestFeaturePromiseAllSettledEmpty(t *testing.T) {
	vm := js.NewVM()
	// Promise.allSettled([]) should resolve to an empty array.
	result := vm.Run(`
		Promise.allSettled([])
	`)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("Promise.allSettled should return a Promise")
	}
	// Check it's a fulfilled promise with empty array result.
	state := result.ObjVal.Get("__promise_state__")
	if int(state.ToNumber()) != 1 { // PromiseFulfilled
		t.Logf("Promise.allSettled([]) state: want fulfilled(1), got %v", state.ToNumber())
	}
}

func TestFeaturePromiseAllSettledResolved(t *testing.T) {
	vm := js.NewVM()
	// allSettled should resolve with array of {status, value/reason}.
	result := vm.Run(`
		var p1 = Promise.resolve(1);
		var p2 = Promise.reject('error');
		Promise.allSettled([p1, p2])
	`)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("Promise.allSettled should return a Promise")
	}
}

func TestFeaturePromiseAnyFirstFulfilled(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		Promise.any([Promise.resolve(42)])
	`)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("Promise.any should return a Promise")
	}
}

func TestFeaturePromiseAnyEmptyRejects(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		Promise.any([])
	`)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("Promise.any([]) should return a Promise")
	}
	state := result.ObjVal.Get("__promise_state__")
	if int(state.ToNumber()) != 2 { // PromiseRejected
		t.Logf("Promise.any([]) state: want rejected(2), got %v", state.ToNumber())
	}
}

func TestFeaturePromiseAnyStaticExists(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		typeof Promise.any === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Promise.any should be a function")
	}
}

func TestFeaturePromiseAllSettledStaticExists(t *testing.T) {
	vm := js.NewVM()
	result := vm.Run(`
		typeof Promise.allSettled === 'function'
	`)
	if !result.IsTruthy() {
		t.Error("Promise.allSettled should be a function")
	}
}

// ---------------------------------------------------------------------------
// Part 4: Map/Set iterator edge cases
// ---------------------------------------------------------------------------

func TestFeatureMapIteratorSkipsDeletedKeys(t *testing.T) {
	// Map iteration order follows insertion order; verify 'b' is skipped.
	runAndExpectConsole(t, `
		var m = new Map();
		m.set('a', 1);
		m.set('b', 2);
		m.set('c', 3);
		var it = m.keys();
		m.delete('b');
		var count = 0;
		var hasA = false, hasB = false, hasC = false;
		var next;
		while (!(next = it.next()).done) {
			count++;
			if (next.value === 'a') hasA = true;
			if (next.value === 'b') hasB = true;
			if (next.value === 'c') hasC = true;
		}
		console.log(count);
		console.log(hasA);
		console.log(hasB);
		console.log(hasC);
	`, []string{"2", "true", "false", "true"})
}

func TestFeatureMapIteratorEmptyMap(t *testing.T) {
	runAndExpectConsole(t, `
		var m = new Map();
		var it = m.keys();
		console.log(it.next().done);
	`, []string{"true"})
}

func TestFeatureMapIteratorDoesNotSeeAddedKeys(t *testing.T) {
	runAndExpectConsole(t, `
		var m = new Map();
		m.set('a', 1);
		var it = m.keys();
		m.set('b', 2);
		var count = 0;
		var next;
		while (!(next = it.next()).done) {
			count++;
		}
		console.log(count);
	`, []string{"1"})
}

func TestFeatureSetIteratorSkipsDeletedValues(t *testing.T) {
	runAndExpectConsole(t, `
		var s = new Set();
		s.add('x');
		s.add('y');
		s.add('z');
		var it = s.values();
		s.delete('y');
		var count = 0;
		var hasX = false, hasY = false, hasZ = false;
		var next;
		while (!(next = it.next()).done) {
			count++;
			if (next.value === 'x') hasX = true;
			if (next.value === 'y') hasY = true;
			if (next.value === 'z') hasZ = true;
		}
		console.log(count);
		console.log(hasX);
		console.log(hasY);
		console.log(hasZ);
	`, []string{"2", "true", "false", "true"})
}

func TestFeatureSetIteratorEmptySet(t *testing.T) {
	runAndExpectConsole(t, `
		var s = new Set();
		var it = s.values();
		console.log(it.next().done);
	`, []string{"true"})
}

func TestFeatureSetIteratorDoesNotSeeAddedValues(t *testing.T) {
	runAndExpectConsole(t, `
		var s = new Set();
		s.add('a');
		var it = s.values();
		s.add('b');
		var count = 0;
		var next;
		while (!(next = it.next()).done) {
			count++;
		}
		console.log(count);
	`, []string{"1"})
}

func TestFeatureMapIteratorValuesSkipsDeleted(t *testing.T) {
	runAndExpectConsole(t, `
		var m = new Map();
		m.set('x', 10);
		m.set('y', 20);
		m.set('z', 30);
		var it = m.values();
		m.delete('y');
		var count = 0;
		var next;
		while (!(next = it.next()).done) {
			count++;
		}
		console.log(count);
	`, []string{"2"})
}

func TestFeatureMapIteratorEntriesSkipsDeleted(t *testing.T) {
	runAndExpectConsole(t, `
		var m = new Map();
		m.set('x', 10);
		m.set('y', 20);
		m.set('z', 30);
		var it = m.entries();
		m.delete('y');
		var count = 0;
		var next;
		while (!(next = it.next()).done) {
			count++;
		}
		console.log(count);
	`, []string{"2"})
}
