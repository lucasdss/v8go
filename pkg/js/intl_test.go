//go:build intl

package js

import (
"strings"
"testing"
)

func TestIntlNumberFormat(t *testing.T) {
vm := NewVM()
result := vm.Run(`var nf = new Intl.NumberFormat("en-US", {style: "currency", currency: "USD"}); nf.format(1234.56)`)
s := result.ToString()
if s == "" || s == "NaN" {
t.Errorf("NumberFormat should format: %v", s)
}
}

func TestIntlDateTimeFormat(t *testing.T) {
vm := NewVM()
result := vm.Run(`typeof new Intl.DateTimeFormat("en-US") === "object"`)
if !result.IsTruthy() {
t.Errorf("DateTimeFormat should be object: %v", result)
}
}

func TestIntlRelativeTimeFormat(t *testing.T) {
vm := NewVM()
result := vm.Run(`new Intl.RelativeTimeFormat("en-US").format(-3, "day")`)
if result.ToString() != "3 days ago" {
t.Errorf("RelativeTimeFormat: %v", result)
}
}

func TestIntlGetCanonicalLocales(t *testing.T) {
vm := NewVM()
result := vm.Run(`Intl.getCanonicalLocales("en")`)
if !result.IsObject() { t.Fatal("should return array") }
got := result.ObjVal.Get("0").ToString()
if !strings.HasPrefix(got, "en") {
t.Errorf("canonical: %q", got)
}
}

func TestIntlListFormat(t *testing.T) {
vm := NewVM()
result := vm.Run(`new Intl.ListFormat("en-US").format(["A", "B", "C"])`)
if result.ToString() != "A, B, and C" {
t.Errorf("ListFormat en-US: %v", result)
}
}

func TestIntlRelativeTimeFuture(t *testing.T) {
vm := NewVM()
result := vm.Run(`new Intl.RelativeTimeFormat("en-US").format(2, "day")`)
if result.ToString() != "in 2 days" {
t.Errorf("RelativeTimeFormat future: %v", result)
}
}
