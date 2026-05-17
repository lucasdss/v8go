package js

import (
"fmt"
"strings"
)


func (vm *VM) registerString() {
	stringProto := NewJSObject()
	stringProto.ConstructorName = "String"

	stringProto.Set("charAt", vm.createBuiltinWithFallback("String.charAt", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return NewString(string(s[0]))
		}
		idx := int(args[0].ToNumber())
		if idx < 0 || idx >= len(s) {
			return NewString("")
		}
		return NewString(string(s[idx]))
	}))

	stringProto.Set("indexOf", vm.createBuiltinWithFallback("String.indexOf", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return NewNumber(-1)
		}
		search := args[0].ToString()
		fromIdx := 0
		if len(args) > 1 {
			fromIdx = int(args[1].ToNumber())
		}
		idx := strings.Index(s[fromIdx:], search)
		if idx < 0 {
			return NewNumber(-1)
		}
		return NewNumber(float64(fromIdx + idx))
	}))

	stringProto.Set("slice", vm.createBuiltinWithFallback("String.slice", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		length := len(s)
		start := 0
		end := length
		if len(args) > 0 {
			start = int(args[0].ToNumber())
		}
		if len(args) > 1 {
			end = int(args[1].ToNumber())
		}
		if start < 0 {
			start = length + start
		}
		if end < 0 {
			end = length + end
		}
		if start < 0 {
			start = 0
		}
		if end > length {
			end = length
		}
		if start >= end {
			return NewString("")
		}
		return NewString(s[start:end])
	}))

	stringProto.Set("split", vm.createBuiltinFunction("String.split", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil && args[0].ObjVal.ConstructorName == "RegExp" {
			// Delegate to RegExp's Symbol.split.
			symSplit := args[0].ObjVal.Get("[Symbol.split]")
			if symSplit.IsObject() && symSplit.ObjVal != nil && symSplit.ObjVal.CallFunc != nil {
				return symSplit.ObjVal.CallFunc(args[0].ObjVal, []JSValue{NewString(s)})
			}
		}
		sep := ""
		if len(args) > 0 {
			sep = args[0].ToString()
		}
		result := NewJSObject()
		result.ConstructorName = "Array"
		if sep == "" {
			for i, ch := range s {
				result.Set(fmt.Sprintf("%d", i), NewString(string(ch)))
			}
			result.Set("length", NewNumber(float64(len(s))))
		} else {
			parts := strings.Split(s, sep)
			for i, p := range parts {
				result.Set(fmt.Sprintf("%d", i), NewString(p))
			}
			result.Set("length", NewNumber(float64(len(parts))))
		}
		return NewObject(result)
	}))

	stringProto.Set("toUpperCase", vm.createBuiltinFunction("String.toUpperCase", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		return NewString(strings.ToUpper(s))
	}))

	stringProto.Set("toLowerCase", vm.createBuiltinFunction("String.toLowerCase", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		return NewString(strings.ToLower(s))
	}))

	stringProto.Set("trim", vm.createBuiltinWithFallback("String.trim", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		return NewString(strings.TrimSpace(s))
	}))

	stringProto.Set("trimStart", vm.createBuiltinFunction("String.trimStart", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		return NewString(strings.TrimLeft(s, " \t\n\r\v\f\u00a0\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u200b\u2028\u2029\u202f\u205f\u3000\ufeff"))
	}))

	stringProto.Set("trimEnd", vm.createBuiltinFunction("String.trimEnd", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		return NewString(strings.TrimRight(s, " \t\n\r\v\f\u00a0\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u200b\u2028\u2029\u202f\u205f\u3000\ufeff"))
	}))

	stringProto.Set("startsWith", vm.createBuiltinFunction("String.startsWith", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return False
		}
		prefix := args[0].ToString()
		pos := 0
		if len(args) > 1 {
			pos = int(args[1].ToNumber())
		}
		if pos < 0 {
			pos = 0
		}
		if pos > len(s) {
			return False
		}
		return NewBoolean(strings.HasPrefix(s[pos:], prefix))
	}))

	stringProto.Set("endsWith", vm.createBuiltinFunction("String.endsWith", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return False
		}
		suffix := args[0].ToString()
		endPos := len(s)
		if len(args) > 1 {
			endPos = int(args[1].ToNumber())
		}
		if endPos < 0 {
			endPos = 0
		}
		if endPos > len(s) {
			endPos = len(s)
		}
		return NewBoolean(strings.HasSuffix(s[:endPos], suffix))
	}))

	stringProto.Set("includes", vm.createBuiltinFunction("String.includes", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return False
		}
		search := args[0].ToString()
		pos := 0
		if len(args) > 1 {
			pos = int(args[1].ToNumber())
		}
		if pos < 0 {
			pos = 0
		}
		if pos > len(s) {
			return False
		}
		return NewBoolean(strings.Contains(s[pos:], search))
	}))

	stringProto.Set("match", vm.createBuiltinFunction("String.match", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return Null
		}
		reArg := args[0]
		if reArg.IsObject() && reArg.ObjVal != nil && reArg.ObjVal.ConstructorName == "RegExp" {
			symMatch := reArg.ObjVal.Get("[Symbol.match]")
			if symMatch.IsObject() && symMatch.ObjVal != nil && symMatch.ObjVal.CallFunc != nil {
				return symMatch.ObjVal.CallFunc(reArg.ObjVal, []JSValue{NewString(s)})
			}
		}
		// Treat non-RegExp as a string: find first occurrence.
		search := reArg.ToString()
		idx := strings.Index(s, search)
		if idx < 0 {
			return Null
		}
		arr := NewJSObject()
		arr.ConstructorName = "Array"
		arr.Set("0", NewString(search))
		arr.Set("length", NewNumber(1))
		arr.Set("index", NewNumber(float64(idx)))
		arr.Set("input", NewString(s))
		return NewObject(arr)
	}))

	stringProto.Set("matchAll", vm.createBuiltinFunction("String.matchAll", func(this *JSObject, args []JSValue) JSValue {
		_ = this.Get("__value__").ToString()
		if len(args) == 0 {
			return Null
		}
		reArg := args[0]
		var reObj *JSObject
		if reArg.IsObject() && reArg.ObjVal != nil && reArg.ObjVal.ConstructorName == "RegExp" {
			reObj = reArg.ObjVal
		} else {
			pat := reArg.ToString()
			reVal := vm.createRegExp(pat, "g")
			if reVal.IsObject() {
				reObj = reVal.ObjVal
			}
		}
		if reObj == nil {
			return Null
		}
		// Ensure global flag is set.
		reObj.Set("global", NewBoolean(true))
		flags := reObj.Get("__flags__").ToString()
		if !strings.Contains(flags, "g") {
			reObj.Set("__flags__", NewString(flags+"g"))
		}
		// Reset lastIndex.
		reObj.Set("lastIndex", NewNumber(0))
		// Return an iterator-like object (simplified: just return all matches as array).
		return NewObject(reObj)
	}))

	stringProto.Set("replace", vm.createBuiltinFunction("String.replace", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) < 2 {
			return NewString(s)
		}
		searchArg := args[0]
		replacement := args[1]
		if searchArg.IsObject() && searchArg.ObjVal != nil && searchArg.ObjVal.ConstructorName == "RegExp" {
			symReplace := searchArg.ObjVal.Get("[Symbol.replace]")
			if symReplace.IsObject() && symReplace.ObjVal != nil && symReplace.ObjVal.CallFunc != nil {
				return symReplace.ObjVal.CallFunc(searchArg.ObjVal, []JSValue{NewString(s), replacement})
			}
		}
		// String replace: replace first occurrence only.
		search := searchArg.ToString()
		if replacement.IsObject() && replacement.ObjVal != nil && replacement.ObjVal.CallFunc != nil {
			// Function replacer.
			idx := strings.Index(s, search)
			if idx < 0 {
				return NewString(s)
			}
			fnResult := replacement.ObjVal.CallFunc(nil, []JSValue{NewString(search), NewNumber(float64(idx)), NewString(s)})
			prefix := s[:idx]
			suffix := s[idx+len(search):]
			return NewString(prefix + fnResult.ToString() + suffix)
		}
		// Simple string replacement of first occurrence.
		repStr := replacement.ToString()
		idx := strings.Index(s, search)
		if idx < 0 {
			return NewString(s)
		}
		return NewString(s[:idx] + repStr + s[idx+len(search):])
	}))

	stringProto.Set("search", vm.createBuiltinFunction("String.search", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return NewNumber(-1)
		}
		reArg := args[0]
		if reArg.IsObject() && reArg.ObjVal != nil && reArg.ObjVal.ConstructorName == "RegExp" {
			symSearch := reArg.ObjVal.Get("[Symbol.search]")
			if symSearch.IsObject() && symSearch.ObjVal != nil && symSearch.ObjVal.CallFunc != nil {
				return symSearch.ObjVal.CallFunc(reArg.ObjVal, []JSValue{NewString(s)})
			}
		}
		search := reArg.ToString()
		idx := strings.Index(s, search)
		return NewNumber(float64(idx))
	}))

	stringProto.Set("repeat", vm.createBuiltinFunction("String.repeat", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return NewString("")
		}
		n := int(args[0].ToNumber())
		if n < 0 {
			return NewString("")
		}
		if n == 0 || s == "" {
			return NewString("")
		}
		return NewString(strings.Repeat(s, n))
	}))

	stringProto.Set("padStart", vm.createBuiltinFunction("String.padStart", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return NewString(s)
		}
		targetLen := int(args[0].ToNumber())
		if targetLen <= len(s) {
			return NewString(s)
		}
		padStr := " "
		if len(args) > 1 {
			padStr = args[1].ToString()
			if padStr == "" {
				padStr = " "
			}
		}
		padLen := targetLen - len(s)
		var buf strings.Builder
		for buf.Len() < padLen {
			buf.WriteString(padStr)
		}
		fullPad := buf.String()
		if len(fullPad) > padLen {
			fullPad = fullPad[:padLen]
		}
		return NewString(fullPad + s)
	}))

	stringProto.Set("padEnd", vm.createBuiltinFunction("String.padEnd", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 {
			return NewString(s)
		}
		targetLen := int(args[0].ToNumber())
		if targetLen <= len(s) {
			return NewString(s)
		}
		padStr := " "
		if len(args) > 1 {
			padStr = args[1].ToString()
			if padStr == "" {
				padStr = " "
			}
		}
		padLen := targetLen - len(s)
		var buf strings.Builder
		for buf.Len() < padLen {
			buf.WriteString(padStr)
		}
		fullPad := buf.String()
		if len(fullPad) > padLen {
			fullPad = fullPad[:padLen]
		}
		return NewString(s + fullPad)
	}))

	// String.prototype.at(index) — ES2022
	stringProto.Set("at", vm.createBuiltinFunction("String.at", func(this *JSObject, args []JSValue) JSValue {
		s := this.Get("__value__").ToString()
		if len(args) == 0 || len(s) == 0 {
			return Undefined
		}
		n := int(args[0].ToNumber())
		runes := []rune(s)
		if n < 0 {
			n = len(runes) + n
		}
		if n < 0 || n >= len(runes) {
			return Undefined
		}
		return NewString(string(runes[n]))
	}))

	// String constructor.
	stringCtor := NewJSObject()
	stringCtor.ConstructorName = "Function"
	stringCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewString("")
		}
		return NewString(args[0].ToString())
	}

	// String.raw — static method returning raw template strings.
	stringCtor.Set("raw", vm.createBuiltinFunction("String.raw", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewString("")
		}
		// args[0] is the template strings array (with a 'raw' property for raw strings)
		templateObj := args[0]
		substitutions := args[1:]
		var buf strings.Builder
		templateLen := 0
		if templateObj.IsObject() && templateObj.ObjVal != nil {
			// Use the 'raw' property if available, otherwise use the object as an array itself.
			rawArr := templateObj.ObjVal.Get("raw")
			var strArr JSValue
			if rawArr.IsObject() && rawArr.ObjVal != nil {
				strArr = rawArr
			} else {
				strArr = templateObj
			}
			if lengthVal := strArr.ObjVal.Get("length"); !lengthVal.IsUndefined() {
				templateLen = int(lengthVal.ToNumber())
			}
			for i := 0; i < templateLen; i++ {
				if i > 0 && i-1 < len(substitutions) {
					buf.WriteString(substitutions[i-1].ToString())
				}
				idxStr := fmt.Sprintf("%d", i)
				if rawStr := strArr.ObjVal.Get(idxStr); !rawStr.IsUndefined() {
					buf.WriteString(rawStr.ToString())
				}
			}
		}
		return NewString(buf.String())
	}))

	// Store for autoboxing.
	StringPrototype = stringProto

	vm.globals["String"] = NewObject(stringCtor)
}
