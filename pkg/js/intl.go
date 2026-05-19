package js

import (
	"fmt"
	"math"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// registerIntl registers the Intl Lite builtins (ECMA-402 subset).
// Provides getCanonicalLocales, NumberFormat, DateTimeFormat, ListFormat,
// and RelativeTimeFormat without CGO or ICU.
func (vm *VM) registerIntl() {
	intlObj := NewJSObject()
	intlObj.ConstructorName = "Intl"

	// Intl.getCanonicalLocales(locales)
	intlObj.Set("getCanonicalLocales", vm.createBuiltinFunction("Intl.getCanonicalLocales", func(this *JSObject, args []JSValue) JSValue {
		tag := "en-US"
		if len(args) > 0 {
			raw := args[0].ToString()
			parsed, err := language.Parse(raw)
			if err == nil {
				tag = parsed.String()
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
		locale := mustParseLocale(args, 0)
		style := "decimal"
		currency := "USD"

		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil {
			opts := args[1].ObjVal
			if s := opts.Get("style"); !s.IsUndefined() {
				style = s.ToString()
			}
			if c := opts.Get("currency"); !c.IsUndefined() {
				currency = c.ToString()
			}
		}

		tag := language.Make(locale)

		nf := NewJSObject()
		nf.ConstructorName = "NumberFormat"
		nf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
			opts := NewJSObject()
			opts.Set("locale", NewString(tag.String()))
			opts.Set("style", NewString(style))
			if style == "currency" {
				opts.Set("currency", NewString(currency))
			}
			return NewObject(opts)
		}))

		nf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
			if len(args) == 0 {
				return NewString("NaN")
			}
			x := args[0].ToNumber()
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return NewString("NaN")
			}

			p := message.NewPrinter(tag)
			var result string
			switch style {
			case "currency":
				result = p.Sprintf("%.2f", x)
				switch tag.String() {
				case "en-US":
					result = "$" + result
				case "de-DE":
					result = result + " €"
				case "ja-JP":
					result = "¥" + result
				case "en-GB":
					result = "£" + result
				default:
					result = currency + " " + result
				}
			case "percent":
				result = p.Sprintf("%.0f%%", x*100)
			default:
				result = p.Sprintf("%v", x)
			}
			return NewString(result)
		}))

		return NewObject(nf)
	}))

	// Intl.DateTimeFormat(locales, options)
	intlObj.Set("DateTimeFormat", vm.createBuiltinFunction("Intl.DateTimeFormat", func(this *JSObject, args []JSValue) JSValue {
		locale := mustParseLocale(args, 0)

		tag := language.Make(locale)

		datePatterns := map[string]string{
			"en-US": "1/2/2006",
			"en-GB": "02/01/2006",
			"de-DE": "02.01.2006",
			"ja-JP": "2006/01/02",
			"pt-BR": "02/01/2006",
			"fr-FR": "02/01/2006",
		}
		pattern, ok := datePatterns[tag.String()]
		if !ok {
			pattern = "2006-01-02"
		}

		dtf := NewJSObject()
		dtf.ConstructorName = "DateTimeFormat"
		dtf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
			opts := NewJSObject()
			opts.Set("locale", NewString(tag.String()))
			return NewObject(opts)
		}))
		dtf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
			if len(args) == 0 {
				return NewString("Invalid Date")
			}
			t := time.UnixMilli(int64(args[0].ToNumber()))
			return NewString(t.Format(pattern))
		}))

		return NewObject(dtf)
	}))

	// Intl.ListFormat(locales, options)
	intlObj.Set("ListFormat", vm.createBuiltinFunction("Intl.ListFormat", func(this *JSObject, args []JSValue) JSValue {
		locale := mustParseLocale(args, 0)
		style := "long"
		typ := "conjunction"

		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil {
			opts := args[1].ObjVal
			if s := opts.Get("style"); !s.IsUndefined() {
				style = s.ToString()
			}
			if t := opts.Get("type"); !t.IsUndefined() {
				typ = t.ToString()
			}
		}

		tag := language.Make(locale)

		lf := NewJSObject()
		lf.ConstructorName = "ListFormat"
		lf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
			opts := NewJSObject()
			opts.Set("locale", NewString(tag.String()))
			opts.Set("style", NewString(style))
			opts.Set("type", NewString(typ))
			return NewObject(opts)
		}))
		lf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
			if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
				return NewString("")
			}
			arr := args[0].ObjVal
			length := int(arr.Get("length").ToNumber())
			if length == 0 {
				return NewString("")
			}

			words := make([]string, length)
			for i := 0; i < length; i++ {
				words[i] = arr.Get(intKey(i)).ToString()
			}

			switch locale {
			case "en-US", "en-GB":
				switch {
				case length == 1:
					return NewString(words[0])
				case length == 2:
					return NewString(words[0] + " and " + words[1])
				default:
					return NewString(strings.Join(words[:length-1], ", ") + ", and " + words[length-1])
				}
			case "ja-JP":
				return NewString(strings.Join(words, "、"))
			default:
				switch {
				case length == 1:
					return NewString(words[0])
				case length == 2:
					return NewString(words[0] + " and " + words[1])
				default:
					return NewString(strings.Join(words[:length-1], ", ") + ", and " + words[length-1])
				}
			}
		}))

		return NewObject(lf)
	}))

	// Intl.RelativeTimeFormat(locales, options)
	intlObj.Set("RelativeTimeFormat", vm.createBuiltinFunction("Intl.RelativeTimeFormat", func(this *JSObject, args []JSValue) JSValue {
		locale := mustParseLocale(args, 0)

		type unitForms struct {
			future string
			past   string
		}

		relativeMap := map[string]map[string]unitForms{
			"en-US": {
				"year":   {future: "in %d years", past: "%d years ago"},
				"month":  {future: "in %d months", past: "%d months ago"},
				"week":   {future: "in %d weeks", past: "%d weeks ago"},
				"day":    {future: "in %d days", past: "%d days ago"},
				"hour":   {future: "in %d hours", past: "%d hours ago"},
				"minute": {future: "in %d minutes", past: "%d minutes ago"},
				"second": {future: "in %d seconds", past: "%d seconds ago"},
			},
		}

		tagStr := locale
		if _, ok := relativeMap[tagStr]; !ok {
			tagStr = "en-US"
		}

		rtf := NewJSObject()
		rtf.ConstructorName = "RelativeTimeFormat"
		rtf.Set("resolvedOptions", vm.createBuiltinFunction("resolvedOptions", func(this *JSObject, args []JSValue) JSValue {
			opts := NewJSObject()
			opts.Set("locale", NewString(tagStr))
			opts.Set("numeric", NewString("always"))
			return NewObject(opts)
		}))
		rtf.Set("format", vm.createBuiltinFunction("format", func(this *JSObject, args []JSValue) JSValue {
			if len(args) < 2 {
				return NewString("")
			}
			value := int(args[0].ToNumber())
			unit := args[1].ToString()

			units := relativeMap[tagStr]
			forms, ok := units[unit]
			if !ok {
				return NewString("")
			}

			absValue := value
			if absValue < 0 {
				absValue = -absValue
			}

			if value > 0 {
				return NewString(fmt.Sprintf(forms.future, value))
			}
			return NewString(fmt.Sprintf(forms.past, absValue))
		}))

		return NewObject(rtf)
	}))

	vm.globals.M["Intl"] = NewObject(intlObj)
}

// mustParseLocale extracts a locale string from args at index, falling back
// to "en-US" on missing/invalid input.
func mustParseLocale(args []JSValue, idx int) string {
	if len(args) <= idx {
		return "en-US"
	}
	raw := args[idx].ToString()
	parsed, err := language.Parse(raw)
	if err != nil {
		return "en-US"
	}
	return parsed.String()
}
