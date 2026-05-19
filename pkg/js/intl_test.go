package js

import (
	"testing"
)

func TestIntlGetCanonicalLocales(t *testing.T) {
	vm := NewVM()

	result := vm.Run(`Intl.getCanonicalLocales("en-US")`)
	if !result.IsObject() || result.ObjVal == nil {
		t.Fatal("should return array")
	}
	if result.ObjVal.Get("0").ToString() != "en-US" {
		t.Errorf("canonical: %v", result.ObjVal.Get("0"))
	}

	// Invalid locale falls back to "en-US"
	result = vm.Run(`Intl.getCanonicalLocales("zzz-invalid")`)
	if result.ObjVal.Get("0").ToString() != "en-US" {
		t.Errorf("invalid locale canonical: %v", result.ObjVal.Get("0"))
	}
}

func TestIntlNumberFormat(t *testing.T) {
	vm := NewVM()

	// Currency formatting
	result := vm.Run(`
		var nf = new Intl.NumberFormat("en-US", {style: "currency", currency: "USD"});
		nf.format(1234.56)
	`)
	if result.ToString() == "" || result.ToString() == "NaN" {
		t.Errorf("NumberFormat USD: %v", result)
	}

	// Decimal formatting
	result = vm.Run(`
		var nf = new Intl.NumberFormat("en-US", {style: "decimal"});
		nf.format(42)
	`)
	if result.ToString() != "42" {
		t.Errorf("NumberFormat decimal: %v", result)
	}

	// Percent formatting
	result = vm.Run(`
		var nf = new Intl.NumberFormat("en-US", {style: "percent"});
		nf.format(0.75)
	`)
	if result.ToString() != "75%" {
		t.Errorf("NumberFormat percent: got %q, want %q", result.ToString(), "75%")
	}

	// NaN handling
	result = vm.Run(`
		var nf = new Intl.NumberFormat("en-US");
		nf.format(NaN)
	`)
	if result.ToString() != "NaN" {
		t.Errorf("NumberFormat NaN: got %q", result.ToString())
	}

	// resolvedOptions
	result = vm.Run(`
		var nf = new Intl.NumberFormat("de-DE", {style: "currency", currency: "EUR"});
		var opts = nf.resolvedOptions();
		opts.locale + "|" + opts.style + "|" + opts.currency
	`)
	if result.ToString() != "de-DE|currency|EUR" {
		t.Errorf("NumberFormat resolvedOptions: got %q", result.ToString())
	}
}

func TestIntlDateTimeFormat(t *testing.T) {
	vm := NewVM()

	// Basic format
	result := vm.Run(`
		var dtf = new Intl.DateTimeFormat("en-US");
		var s = dtf.format(Date.UTC(2024, 0, 15));
		typeof s === "string" && s !== ""
	`)
	if !result.IsTruthy() {
		t.Errorf("DateTimeFormat: got %v", result)
	}

	// Different locale patterns
	result = vm.Run(`
		var dtf = new Intl.DateTimeFormat("de-DE");
		dtf.format(Date.UTC(2024, 0, 15))
	`)
	if result.ToString() != "15.01.2024" {
		t.Errorf("DateTimeFormat de-DE: got %q", result.ToString())
	}

	// resolvedOptions
	result = vm.Run(`
		var dtf = new Intl.DateTimeFormat("ja-JP");
		var opts = dtf.resolvedOptions();
		opts.locale
	`)
	if result.ToString() != "ja-JP" {
		t.Errorf("DateTimeFormat resolvedOptions: got %q", result.ToString())
	}
}

func TestIntlListFormat(t *testing.T) {
	vm := NewVM()

	// English conjunction
	result := vm.Run(`
		var lf = new Intl.ListFormat("en-US");
		lf.format(["A", "B", "C"])
	`)
	if result.ToString() != "A, B, and C" {
		t.Errorf("ListFormat en-US: got %q", result.ToString())
	}

	// Two items
	result = vm.Run(`
		var lf = new Intl.ListFormat("en-US");
		lf.format(["A", "B"])
	`)
	if result.ToString() != "A and B" {
		t.Errorf("ListFormat en-US two: got %q", result.ToString())
	}

	// Single item
	result = vm.Run(`
		var lf = new Intl.ListFormat("en-US");
		lf.format(["A"])
	`)
	if result.ToString() != "A" {
		t.Errorf("ListFormat en-US one: got %q", result.ToString())
	}

	// Empty
	result = vm.Run(`
		var lf = new Intl.ListFormat("en-US");
		lf.format([])
	`)
	if result.ToString() != "" {
		t.Errorf("ListFormat empty: got %q", result.ToString())
	}

	// Japanese
	result = vm.Run(`
		var lf = new Intl.ListFormat("ja-JP");
		lf.format(["A", "B", "C"])
	`)
	if result.ToString() != "A、B、C" {
		t.Errorf("ListFormat ja-JP: got %q", result.ToString())
	}

	// resolvedOptions
	result = vm.Run(`
		var lf = new Intl.ListFormat("en-US", {style: "short", type: "unit"});
		var opts = lf.resolvedOptions();
		opts.locale + "|" + opts.style + "|" + opts.type
	`)
	if result.ToString() != "en-US|short|unit" {
		t.Errorf("ListFormat resolvedOptions: got %q", result.ToString())
	}
}

func TestIntlRelativeTimeFormat(t *testing.T) {
	vm := NewVM()

	// Past
	result := vm.Run(`
		var rtf = new Intl.RelativeTimeFormat("en-US");
		rtf.format(-3, "day")
	`)
	if result.ToString() != "3 days ago" {
		t.Errorf("RelativeTimeFormat past: got %q", result.ToString())
	}

	// Future
	result = vm.Run(`
		var rtf = new Intl.RelativeTimeFormat("en-US");
		rtf.format(2, "week")
	`)
	if result.ToString() != "in 2 weeks" {
		t.Errorf("RelativeTimeFormat future: got %q", result.ToString())
	}

	// Singular (1 hour)
	result = vm.Run(`
		var rtf = new Intl.RelativeTimeFormat("en-US");
		rtf.format(-1, "hour")
	`)
	if result.ToString() != "1 hours ago" {
		t.Errorf("RelativeTimeFormat singular: got %q", result.ToString())
	}

	// resolvedOptions
	result = vm.Run(`
		var rtf = new Intl.RelativeTimeFormat("en-US");
		var opts = rtf.resolvedOptions();
		opts.locale + "|" + opts.numeric
	`)
	if result.ToString() != "en-US|always" {
		t.Errorf("RelativeTimeFormat resolvedOptions: got %q", result.ToString())
	}
}
