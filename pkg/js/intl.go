//go:build intl

package js

import (
"fmt"
"math"
"strings"
"time"
)

// registerIntl registers the Intl Lite builtins (ECMA-402 subset).
// Uses Go standard library only — no CGO, no ICU, no external deps.
func (vm *VM) registerIntl() {
intlObj := NewJSObject()
intlObj.ConstructorName = "Intl"

// Intl.getCanonicalLocales(locales)
intlObj.Set("getCanonicalLocales", vm.createBuiltinFunction("Intl.getCanonicalLocales", func(this *JSObject, args []JSValue) JSValue {
tag := "en-US"
if len(args) > 0 {
raw := strings.ToLower(args[0].ToString())
// Simple canonicalization of common locale tags
switch {
case strings.Contains(raw, "en") && strings.Contains(raw, "us"):
tag = "en-US"
case strings.Contains(raw, "en") && strings.Contains(raw, "gb"):
tag = "en-GB"
case strings.Contains(raw, "de"):
tag = "de-DE"
case strings.Contains(raw, "ja"):
tag = "ja-JP"
case strings.Contains(raw, "pt") && strings.Contains(raw, "br"):
tag = "pt-BR"
case strings.Contains(raw, "fr"):
tag = "fr-FR"
default:
tag = raw
}
}
arr := NewJSObject()
arr.ConstructorName = "Array"
arr.Set("0", NewString(tag))
arr.Set("length", NewNumber(1))
return NewObject(arr)
}))

// Intl.NumberFormat(locales, options)
intlObj.Set("NumberFormat", vm.createBuiltinFunction("Intl.NumberFormat", func(this *JSObject, args []JSValue) JSValue {
locale := "en-US"
style := "decimal"
currency := "USD"
if len(args) > 0 { locale = canonicalLocale(args[0].ToString()) }
if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil {
opts := args[1].ObjVal
if s := opts.Get("style"); !s.IsUndefined() { style = s.ToString() }
if c := opts.Get("currency"); !c.IsUndefined() { currency = c.ToString() }
}
nf := NewJSObject()
nf.ConstructorName = "NumberFormat"
nf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
opts := NewJSObject()
opts.Set("locale", NewString(locale))
opts.Set("style", NewString(style))
if style == "currency" { opts.Set("currency", NewString(currency)) }
return NewObject(opts)
}))
nf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
if len(args) == 0 { return NewString("NaN") }
x := args[0].ToNumber()
if math.IsNaN(x) || math.IsInf(x, 0) { return NewString("NaN") }
var result string
switch style {
case "currency":
sym := currencySymbol(currency, locale)
result = fmt.Sprintf("%s%.2f", sym, x)
case "percent":
result = fmt.Sprintf("%.0f%%", x*100)
default:
result = fmt.Sprintf("%g", x)
}
return NewString(formatNumber(result, locale))
}))
return NewObject(nf)
}))

// Intl.DateTimeFormat(locales, options)
intlObj.Set("DateTimeFormat", vm.createBuiltinFunction("Intl.DateTimeFormat", func(this *JSObject, args []JSValue) JSValue {
locale := "en-US"
if len(args) > 0 { locale = canonicalLocale(args[0].ToString()) }
patterns := map[string]string{
"en-US": "1/2/2006", "en-GB": "02/01/2006", "de-DE": "02.01.2006",
"ja-JP": "2006/01/02", "pt-BR": "02/01/2006", "fr-FR": "02/01/2006",
}
pattern := patterns[locale]
if pattern == "" { pattern = "2006-01-02" }
dtf := NewJSObject()
dtf.ConstructorName = "DateTimeFormat"
dtf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
opts := NewJSObject(); opts.Set("locale", NewString(locale)); return NewObject(opts)
}))
dtf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
if len(args) == 0 { return NewString("Invalid Date") }
t := time.UnixMilli(int64(args[0].ToNumber()))
return NewString(t.Format(pattern))
}))
return NewObject(dtf)
}))

// Intl.ListFormat(locales, options)
intlObj.Set("ListFormat", vm.createBuiltinFunction("Intl.ListFormat", func(this *JSObject, args []JSValue) JSValue {
locale := "en-US"
if len(args) > 0 { locale = canonicalLocale(args[0].ToString()) }
lf := NewJSObject()
lf.ConstructorName = "ListFormat"
lf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
opts := NewJSObject(); opts.Set("locale", NewString(locale))
opts.Set("style", NewString("long")); opts.Set("type", NewString("conjunction"))
return NewObject(opts)
}))
lf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil { return NewString("") }
arr := args[0].ObjVal
length := int(arr.Get("length").ToNumber())
if length == 0 { return NewString("") }
var words []string
for i := 0; i < length; i++ { words = append(words, arr.Get(intKey(i)).ToString()) }
switch locale {
case "ja-JP":
return NewString(strings.Join(words, "、"))
default:
if length == 1 { return NewString(words[0]) }
if length == 2 { return NewString(words[0] + " and " + words[1]) }
return NewString(strings.Join(words[:length-1], ", ") + ", and " + words[length-1])
}
}))
return NewObject(lf)
}))

// Intl.RelativeTimeFormat(locales, options)
intlObj.Set("RelativeTimeFormat", vm.createBuiltinFunction("Intl.RelativeTimeFormat", func(this *JSObject, args []JSValue) JSValue {
locale := "en-US"
if len(args) > 0 { locale = canonicalLocale(args[0].ToString()) }
forms := map[string]map[bool]string{
"year": {true: "in %d years", false: "%d years ago"},
"month": {true: "in %d months", false: "%d months ago"},
"week": {true: "in %d weeks", false: "%d weeks ago"},
"day": {true: "in %d days", false: "%d days ago"},
"hour": {true: "in %d hours", false: "%d hours ago"},
"minute": {true: "in %d minutes", false: "%d minutes ago"},
"second": {true: "in %d seconds", false: "%d seconds ago"},
}
rtf := NewJSObject()
rtf.ConstructorName = "RelativeTimeFormat"
rtf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
opts := NewJSObject(); opts.Set("locale", NewString(locale)); opts.Set("numeric", NewString("always"))
return NewObject(opts)
}))
rtf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
if len(args) < 2 { return NewString("") }
value := int(args[0].ToNumber())
unit := args[1].ToString()
f, ok := forms[unit]
if !ok { return NewString("") }
absValue := value
if absValue < 0 { absValue = -absValue }
if value > 0 { return NewString(fmt.Sprintf(f[true], value)) }
return NewString(fmt.Sprintf(f[false], absValue))
}))
return NewObject(rtf)
}))

vm.globals.M["Intl"] = NewObject(intlObj)
}

func canonicalLocale(s string) string {
switch strings.ToLower(s) {
case "en-us", "en": return "en-US"
case "en-gb": return "en-GB"
case "de-de", "de": return "de-DE"
case "ja-jp", "ja": return "ja-JP"
case "pt-br": return "pt-BR"
case "fr-fr", "fr": return "fr-FR"
default: return "en-US"
}
}

func currencySymbol(currency, locale string) string {
switch { case currency == "USD": return "$"
case currency == "EUR": return "€"
case currency == "GBP": return "£"
case currency == "JPY": return "¥"
default: return currency + " "
}
}

func formatNumber(s, locale string) string {
switch locale {
case "de-DE", "fr-FR", "pt-BR":
s = strings.Replace(s, ".", ",", 1)
default: // en-US, en-GB, ja-JP keep dots
}
return s
}
