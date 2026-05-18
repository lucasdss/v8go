package js

import (
"encoding/json"
"fmt"
"math"
"regexp"
"strconv"
"strings"
"time"
)


func (vm *VM) registerMath() {
	mathObj := NewJSObject()
	mathObj.ConstructorName = "Math"

	mathObj.Set("PI", NewNumber(3.141592653589793))
	mathObj.Set("E", NewNumber(2.718281828459045))

	mathObj.Set("abs", vm.createBuiltinFunction("Math.abs", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.NaN())
		}
		return NewNumber(math.Abs(args[0].ToNumber()))
	}))

	mathObj.Set("floor", vm.createBuiltinFunction("Math.floor", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.NaN())
		}
		return NewNumber(math.Floor(args[0].ToNumber()))
	}))

	mathObj.Set("ceil", vm.createBuiltinFunction("Math.ceil", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.NaN())
		}
		return NewNumber(math.Ceil(args[0].ToNumber()))
	}))

	mathObj.Set("round", vm.createBuiltinFunction("Math.round", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.NaN())
		}
		return NewNumber(math.Round(args[0].ToNumber()))
	}))

	mathObj.Set("max", vm.createBuiltinFunction("Math.max", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.Inf(-1))
		}
		maxVal := args[0].ToNumber()
		for i := 1; i < len(args); i++ {
			v := args[i].ToNumber()
			if v > maxVal || math.IsNaN(v) {
				maxVal = v
			}
		}
		return NewNumber(maxVal)
	}))

	mathObj.Set("min", vm.createBuiltinFunction("Math.min", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.Inf(1))
		}
		minVal := args[0].ToNumber()
		for i := 1; i < len(args); i++ {
			v := args[i].ToNumber()
			if v < minVal || math.IsNaN(v) {
				minVal = v
			}
		}
		return NewNumber(minVal)
	}))

	vm.globals["Math"] = NewObject(mathObj)
}


func (vm *VM) registerNumber() {
	numberCtor := NewJSObject()
	numberCtor.ConstructorName = "Function"
	numberCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(0)
		}
		// Number(1n) → 1 (explicit BigInt to Number conversion is allowed).
		if args[0].IsBigInt() && args[0].BigIntVal != nil {
			return NewNumber(float64(args[0].BigIntVal.Int64()))
		}
		return NewNumber(args[0].ToNumber())
	}

	// Number.isNaN(val) — true if val is exactly NaN (type-safe).
	numberCtor.Set("isNaN", vm.createBuiltinFunction("Number.isNaN", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsNumber() {
			return False
		}
		return NewBoolean(math.IsNaN(args[0].NumVal))
	}))

	// Number.isFinite(val) — true if val is a finite number (type-safe).
	numberCtor.Set("isFinite", vm.createBuiltinFunction("Number.isFinite", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsNumber() {
			return False
		}
		n := args[0].NumVal
		return NewBoolean(!math.IsNaN(n) && !math.IsInf(n, 0))
	}))

	// Number.isInteger(val) — true if val is a finite integer (type-safe).
	numberCtor.Set("isInteger", vm.createBuiltinFunction("Number.isInteger", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsNumber() {
			return False
		}
		n := args[0].NumVal
		return NewBoolean(!math.IsNaN(n) && !math.IsInf(n, 0) && n == math.Trunc(n))
	}))

	vm.globals["Number"] = NewObject(numberCtor)
}

func (vm *VM) registerJSON() {
	jsonObj := NewJSObject()
	jsonObj.ConstructorName = "JSON"

	// JSON.parse(string) — parses JSON string to JS value.
	jsonObj.Set("parse", vm.createBuiltinFunction("JSON.parse", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Undefined
		}
		s := args[0].ToString()
		var result jsonNode
		if err := json.Unmarshal([]byte(s), &result); err != nil {
			return Undefined
		}
		return jsonNodeToJSValue(&result)
	}))

	// JSON.stringify(value) — converts JS value to JSON string.
	jsonObj.Set("stringify", vm.createBuiltinFunction("JSON.stringify", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewString("undefined")
		}
		result := jsToJSON(args[0])
		return NewString(result)
	}))

	vm.globals["JSON"] = NewObject(jsonObj)
}

// --- JSON node type (safe deserialization) ---

// jsonNode represents a parsed JSON value as a tree of typed nodes.
// This avoids deserializing into interface{} which is flagged by semgrep
// as unsafe-deserialization-interface (CWE-502).
type jsonNode struct {
	Kind    string // "object", "array", "string", "number", "bool", "null"
	Object  map[string]jsonNode
	Array   []jsonNode
	String  string
	Number  float64
	Boolean bool
}

// UnmarshalJSON implements json.Unmarshaler. It uses the streaming json.Decoder
// API to build a jsonNode tree without ever deserializing into interface{}.
func (n *jsonNode) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	return n.decodeValue(dec)
}

func (n *jsonNode) decodeValue(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("json decode: %w", err)
	}
	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			n.Kind = "object"
			n.Object = make(map[string]jsonNode)
			for dec.More() {
				tok, err := dec.Token()
				if err != nil {
					return fmt.Errorf("json decode key: %w", err)
				}
				key, ok := tok.(string)
				if !ok {
					return fmt.Errorf("json: expected string key, got %T", tok)
				}
				var child jsonNode
				if err := child.decodeValue(dec); err != nil {
					return err
				}
				n.Object[key] = child
			}
			if _, err := dec.Token(); err != nil { // consume closing }
				return fmt.Errorf("json decode object close: %w", err)
			}
		case '[':
			n.Kind = "array"
			for dec.More() {
				var child jsonNode
				if err := child.decodeValue(dec); err != nil {
					return err
				}
				n.Array = append(n.Array, child)
			}
			if _, err := dec.Token(); err != nil { // consume closing ]
				return fmt.Errorf("json decode array close: %w", err)
			}
		}
	case string:
		n.Kind = "string"
		n.String = v
	case float64:
		n.Kind = "number"
		n.Number = v
	case bool:
		n.Kind = "bool"
		n.Boolean = v
	case nil:
		n.Kind = "null"
	default:
		return fmt.Errorf("json: unexpected token type %T", v)
	}
	return nil
}

// jsonNodeToJSValue converts a decoded jsonNode tree into a JSValue.
// n is passed by pointer to avoid copying the 80-byte struct per gocritic.
func jsonNodeToJSValue(n *jsonNode) JSValue {
	switch n.Kind {
	case "null":
		return Null
	case "bool":
		return NewBoolean(n.Boolean)
	case "number":
		return NewNumber(n.Number)
	case "string":
		return NewString(n.String)
	case "array":
		arr := NewJSObject()
		arr.ConstructorName = "Array"
		for i, elem := range n.Array {
			arr.Set(intKey(i), jsonNodeToJSValue(&elem))
		}
		arr.Set("length", NewNumber(float64(len(n.Array))))
		return NewObject(arr)
	case "object":
		obj := NewJSObject()
		for k, elem := range n.Object {
			obj.Set(k, jsonNodeToJSValue(&elem))
		}
		return NewObject(obj)
	default:
		return Undefined
	}
}

// --- JSON conversion helpers ---

func goToJSValue(v interface{}) JSValue {
	switch val := v.(type) {
	case nil:
		return Null
	case bool:
		return NewBoolean(val)
	case float64:
		return NewNumber(val)
	case string:
		return NewString(val)
	case []interface{}:
		arr := NewJSObject()
		arr.ConstructorName = "Array"
		for i, elem := range val {
			arr.Set(intKey(i), goToJSValue(elem))
		}
		arr.Set("length", NewNumber(float64(len(val))))
		return NewObject(arr)
	case map[string]interface{}:
		obj := NewJSObject()
		for k, elem := range val {
			obj.Set(k, goToJSValue(elem))
		}
		return NewObject(obj)
	}
	return Undefined
}

func jsToGoValue(v JSValue) interface{} {
	switch v.Tag {
	case TagUndefined:
		return nil
	case TagNull:
		return nil
	case TagBoolean:
		return v.BoolVal
	case TagNumber:
		return v.NumVal
	case TagString:
		return v.StrVal
	case TagSymbol:
		return v.SymVal
	case TagObject:
		if v.ObjVal == nil {
			return nil
		}
		// Check if it's an array by looking for length property.
		lenVal := v.ObjVal.Get("length")
		if lenVal.IsNumber() {
			length := int(lenVal.ToNumber())
			result := make([]interface{}, length)
			for i := 0; i < length; i++ {
				result[i] = jsToGoValue(v.ObjVal.Get(intKey(i)))
			}
			return result
		}
		// Regular object.
		result := make(map[string]interface{})
		// Iterate over known slots (this is a simplification).
		if !v.ObjVal.Shape.IsDictionary {
			for name, entry := range v.ObjVal.Shape.Properties {
				if entry.Offset < v.ObjVal.propLen() {
					result[name] = jsToGoValue(v.ObjVal.propAt(entry.Offset))
				}
			}
		}
		return result
	}
	return nil
}

// jsToJSON serializes a JSValue to a JSON string, skipping functions, undefined, and Symbol.
func jsToJSON(v JSValue) string {
	switch v.Tag {
	case TagUndefined:
		return "undefined"
	case TagNull:
		return "null"
	case TagBoolean:
		if v.BoolVal {
			return "true"
		}
		return "false"
	case TagNumber:
		return strconv.FormatFloat(v.NumVal, 'f', -1, 64)
	case TagString:
		b, _ := json.Marshal(v.StrVal)
		return string(b)
	case TagSymbol:
		return "" // Symbols are ignored in JSON
	case TagObject:
		if v.ObjVal == nil {
			return "null"
		}
		// Skip callable objects (functions).
		if v.ObjVal.isCallable() {
			return "undefined"
		}
		// Check if it's an array.
		lenVal := v.ObjVal.Get("length")
		if lenVal.IsNumber() {
			length := int(lenVal.ToNumber())
			parts := make([]string, 0, length)
			for i := 0; i < length; i++ {
				elem := v.ObjVal.Get(intKey(i))
				val := jsToJSON(elem)
				if val == "undefined" {
					val = "null"
				}
				parts = append(parts, val)
			}
			return "[" + strings.Join(parts, ",") + "]"
		}
		// Regular object: iterate own properties, skip functions/undefined.
		parts := make([]string, 0)
		if !v.ObjVal.Shape.IsDictionary {
			for name, entry := range v.ObjVal.Shape.Properties {
				if entry.Offset < v.ObjVal.propLen() {
					propVal := v.ObjVal.propAt(entry.Offset)
					// Skip functions and undefined.
					if propVal.IsUndefined() {
						continue
					}
					if propVal.IsObject() && propVal.ObjVal != nil && propVal.ObjVal.isCallable() {
						continue
					}
					key, _ := json.Marshal(name)
					valStr := jsToJSON(propVal)
					if valStr != "undefined" {
						parts = append(parts, string(key)+":"+valStr)
					}
				}
			}
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return "null"
}

// Simple URL encoding (basic version, sufficient for common cases).
func urlEncode(s string) string {
	var buf strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '!' || r == '~' || r == '*' || r == '\'' || r == '(' || r == ')' {
			buf.WriteRune(r)
		} else {
			b := []byte(string(r))
			for _, c := range b {
				buf.WriteString(fmt.Sprintf("%%%02X", c))
			}
		}
	}
	return buf.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

// urlDecode percent-decodes a URI component per ECMAScript decodeURIComponent.
// Does NOT convert + to space (unlike url.QueryUnescape).
func urlDecode(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := unhex(s[i+1])
			lo, ok2 := unhex(s[i+2])
			if ok1 && ok2 {
				buf.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		buf.WriteByte(s[i])
	}
	return buf.String()
}

func regExpExec(reObj *JSObject, str string) JSValue {
	pat := reObj.Get("__pattern__").ToString()
	flags := reObj.Get("__flags__").ToString()
	lastIndex := int(reObj.Get("lastIndex").ToNumber())
	global := strings.Contains(flags, "g")
	sticky := strings.Contains(flags, "y")

	if global || sticky {
		if lastIndex < 0 {
			lastIndex = 0
		}
		if lastIndex > len(str) {
			if global || sticky {
				reObj.Set("lastIndex", NewNumber(0))
			}
			return Null
		}
	}

	compiled, err := compileJSRegexp(pat, flags)
	if err != nil {
		return Null
	}

	var loc []int
	if sticky {
		if lastIndex > 0 && lastIndex < len(str) {
			substr := str[lastIndex:]
			loc = compiled.FindStringSubmatchIndex(substr)
			if loc != nil {
				for i := range loc {
					if loc[i] >= 0 {
						loc[i] += lastIndex
					}
				}
			}
		} else if lastIndex == 0 {
			loc = compiled.FindStringSubmatchIndex(str)
		}
	} else if global {
		loc = compiled.FindStringSubmatchIndex(str[lastIndex:])
		if loc != nil {
			for i := range loc {
				if loc[i] >= 0 {
					loc[i] += lastIndex
				}
			}
		}
	} else {
		loc = compiled.FindStringSubmatchIndex(str)
	}

	if loc == nil {
		reObj.Set("lastIndex", NewNumber(0))
		return Null
	}

	result := NewJSObject()
	result.ConstructorName = "Array"

	// Build match array.
	matchLen := len(loc) / 2
	for i := 0; i < matchLen; i++ {
		start, end := loc[i*2], loc[i*2+1]
		if start < 0 || start > len(str) || end < 0 || end > len(str) {
			result.Set(intKey(i), Undefined)
		} else {
			result.Set(intKey(i), NewString(str[start:end]))
		}
	}
	result.Set("length", NewNumber(float64(matchLen)))

	// index and input properties.
	if len(loc) >= 2 {
		result.Set("index", NewNumber(float64(loc[0])))
	}
	result.Set("input", NewString(str))

	// Named capture groups.
	{
		groupNames := compiled.SubexpNames()
		hasNamed := false
		for _, name := range groupNames {
			if name != "" {
				hasNamed = true
				break
			}
		}
		if hasNamed {
			groupsObj := NewJSObject()
			for i, name := range groupNames {
				if name == "" || i == 0 {
					continue
				}
				if i*2+1 < len(loc) {
					start, end := loc[i*2], loc[i*2+1]
					if start < 0 || end < 0 {
						groupsObj.Set(name, Undefined)
					} else {
						groupsObj.Set(name, NewString(str[start:end]))
					}
				} else {
					groupsObj.Set(name, Undefined)
				}
			}
			result.Set("groups", NewObject(groupsObj))
		}
	}

	// Update lastIndex for global/sticky.
	if global || sticky {
		if len(loc) >= 2 && loc[1] > lastIndex {
			reObj.Set("lastIndex", NewNumber(float64(loc[1])))
		} else {
			reObj.Set("lastIndex", NewNumber(0))
		}
	}

	return NewObject(result)
}

// compileJSRegexp compiles a JavaScript regexp pattern with flags into a Go regexp.
func compileJSRegexp(pattern, flags string) (*regexp.Regexp, error) {
	// Handle dotAll flag: . should match newlines.
	if strings.Contains(flags, "s") {
		// Replace . with [\s\S] to match any character including newlines.
		// We need to be careful not to replace . inside character classes.
		pattern = replaceDotOutsideCharClass(pattern)
	}

	// Handle unicode flag: support \u{...} escapes in pattern.
	if strings.Contains(flags, "u") {
		pattern = expandUnicodeEscapes(pattern)
	}

	// Build Go regexp from pattern.
	compilePattern := pattern
	// If Go regexp doesn't support named groups with (?<name>), convert to (?P<name>).
	compilePattern = convertNamedGroups(compilePattern)

	return regexp.Compile(compilePattern)
}

// replaceDotOutsideCharClass replaces . with [\s\S] when not inside [...].
func replaceDotOutsideCharClass(pattern string) string {
	var buf strings.Builder
	inClass := false
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '\\' && i+1 < len(pattern) {
			buf.WriteByte(ch)
			i++
			buf.WriteByte(pattern[i])
			continue
		}
		if ch == '[' {
			inClass = true
			buf.WriteByte(ch)
			continue
		}
		if ch == ']' {
			inClass = false
			buf.WriteByte(ch)
			continue
		}
		if ch == '.' && !inClass {
			buf.WriteString(`[\s\S]`)
			continue
		}
		buf.WriteByte(ch)
	}
	return buf.String()
}

// expandUnicodeEscapes expands \u{...} sequences to actual Unicode characters.
func expandUnicodeEscapes(pattern string) string {
	var buf strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) && pattern[i+1] == 'u' && i+2 < len(pattern) && pattern[i+2] == '{' {
			// Find closing brace.
			end := strings.IndexByte(pattern[i+3:], '}')
			if end >= 0 {
				hexStr := pattern[i+3 : i+3+end]
				if codePoint, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
					buf.WriteRune(rune(codePoint))
					i += 3 + end + 1
					continue
				}
			}
		}
		buf.WriteByte(pattern[i])
	}
	return buf.String()
}

// convertNamedGroups converts JavaScript (?<name>...) to Go (?P<name>...) named capture groups.
// Also handles \k<name> backreferences.
func convertNamedGroups(pattern string) string {
	// Convert (?<name>...) to (?P<name>...)
	var buf strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '(' && i+2 < len(pattern) && pattern[i+1] == '?' && pattern[i+2] == '<' {
			// Find closing >
			closeBracket := strings.IndexByte(pattern[i+3:], '>')
			if closeBracket >= 0 {
				buf.WriteString("(?P<")
				buf.WriteString(pattern[i+3 : i+3+closeBracket])
				buf.WriteByte('>')
				i += 3 + closeBracket
				continue
			}
		}
		// Handle \k<name> backreferences — convert to \k<name> (Go supports these natively)
		buf.WriteByte(pattern[i])
	}
	return buf.String()
}

// createRegExp creates a RegExp object from a pattern and flags string.
// Called by the VM for regexp literals and the RegExp constructor.
func (vm *VM) createRegExp(pattern, flags string) JSValue {
	reObj := NewJSObject()
	reObj.ConstructorName = "RegExp"
	if RegExpPrototype != nil {
		reObj.Prototype = RegExpPrototype
	}

	reObj.Set("__pattern__", NewString(pattern))
	reObj.Set("__flags__", NewString(flags))
	reObj.Set("lastIndex", NewNumber(0))

	// Flag properties.
	reObj.Set("global", NewBoolean(strings.Contains(flags, "g")))
	reObj.Set("ignoreCase", NewBoolean(strings.Contains(flags, "i")))
	reObj.Set("multiline", NewBoolean(strings.Contains(flags, "m")))
	reObj.Set("dotAll", NewBoolean(strings.Contains(flags, "s")))
	reObj.Set("unicode", NewBoolean(strings.Contains(flags, "u")))
	reObj.Set("sticky", NewBoolean(strings.Contains(flags, "y")))
	reObj.Set("flags", NewString(flags))

	// Source property (pattern without slashes).
	reObj.Set("source", NewString(pattern))

	return NewObject(reObj)
}

func (vm *VM) registerBoolean() {
	boolProto := NewJSObject()
	boolProto.ConstructorName = "Boolean"

	boolProto.Set("toString", vm.createBuiltinFunction("Boolean.toString", func(this *JSObject, args []JSValue) JSValue {
		data := this.Get("[[BooleanData]]")
		if data.IsBoolean() && data.BoolVal {
			return NewString("true")
		}
		return NewString("false")
	}))

	boolProto.Set("valueOf", vm.createBuiltinFunction("Boolean.valueOf", func(this *JSObject, args []JSValue) JSValue {
		return this.Get("[[BooleanData]]")
	}))

	boolCtor := vm.createBuiltinFunction("Boolean", func(this *JSObject, args []JSValue) JSValue {
		val := False
		if len(args) > 0 {
			val = NewBoolean(args[0].IsTruthy())
		}
		// When called with new, 'this' is the pre-created wrapper object
		// (ConstructorName is empty, not "Function" like the constructor itself).
		if this.ConstructorName != "Function" {
			this.ConstructorName = "Boolean"
			this.Set("[[BooleanData]]", val)
			return NewObject(this)
		}
		return val
	})

	boolCtor.ObjVal.Set("prototype", NewObject(boolProto))
	vm.globals["Boolean"] = boolCtor
}

func (vm *VM) registerRegExp() {
	// RegExp constructor.
	regExpCtor := NewJSObject()
	regExpCtor.ConstructorName = "Function"

	// When called as constructor: new RegExp(pattern [, flags])
	regExpCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		pattern := ""
		flags := ""
		if len(args) > 0 {
			// If first arg is a RegExp, extract its pattern/flags.
			if args[0].IsObject() && args[0].ObjVal != nil && args[0].ObjVal.ConstructorName == "RegExp" {
				pattern = args[0].ObjVal.Get("__pattern__").ToString()
				flags = args[0].ObjVal.Get("__flags__").ToString()
			} else {
				pattern = args[0].ToString()
			}
		}
		if len(args) > 1 {
			flags = args[1].ToString()
		}
		return vm.createRegExp(pattern, flags)
	}

	// RegExp.prototype
	regExpProto := NewJSObject()
	regExpProto.ConstructorName = "RegExp"
	regExpProto.Prototype = ObjectPrototype
	RegExpPrototype = regExpProto

	// exec(str) — perform a single match, return result array or null.
	regExpProto.Set("exec", vm.createBuiltinFunction("RegExp.exec", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Null
		}
		str := args[0].ToString()
		return regExpExec(this, str)
	}))

	// test(str) — returns true if match, false otherwise.
	regExpProto.Set("test", vm.createBuiltinFunction("RegExp.test", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		str := args[0].ToString()
		result := regExpExec(this, str)
		return NewBoolean(!result.IsNull())
	}))

	// toString() — returns /pattern/flags
	regExpProto.Set("toString", vm.createBuiltinFunction("RegExp.toString", func(this *JSObject, args []JSValue) JSValue {
		pat := this.Get("__pattern__").ToString()
		flags := this.Get("__flags__").ToString()
		return NewString("/" + pat + "/" + flags)
	}))

	// Symbol.match — called by String.prototype.match
	regExpProto.Set("[Symbol.match]", vm.createBuiltinFunction("RegExp.[Symbol.match]", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Null
		}
		str := args[0].ToString()
		global := this.Get("global").BoolVal
		if !global {
			return regExpExec(this, str)
		}
		// Global match: find all matches.
		this.Set("lastIndex", NewNumber(0))
		results := NewJSObject()
		results.ConstructorName = "Array"
		idx := 0
		for {
			match := regExpExec(this, str)
			if match.IsNull() {
				break
			}
			arr := match.ObjVal
			if arr.Get("length").ToNumber() > 0 {
				results.Set(intKey(idx), arr.Get("0"))
				idx++
			}
			lastIdx := int(this.Get("lastIndex").ToNumber())
			if lastIdx <= 0 {
				break
			}
		}
		results.Set("length", NewNumber(float64(idx)))
		if idx == 0 {
			return Null
		}
		return NewObject(results)
	}))

	// Symbol.replace — called by String.prototype.replace
	regExpProto.Set("[Symbol.replace]", vm.createBuiltinFunction("RegExp.[Symbol.replace]", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return args[0]
		}
		str := args[0].ToString()
		replacement := args[1]
		global := this.Get("global").BoolVal

		if !global {
			match := regExpExec(this, str)
			if match.IsNull() {
				return NewString(str)
			}
			return NewString(doFullReplace(str, match, replacement))
		}

		// Global replace: replace all matches.
		this.Set("lastIndex", NewNumber(0))
		result := str
		var offset int
		for {
			match := regExpExec(this, str)
			if match.IsNull() {
				break
			}
			matchPos := int(match.ObjVal.Get("index").ToNumber())
			matchBV := match.ObjVal.Get("0")
			matchLen := 0
			if matchBV.IsString() {
				matchLen = len(matchBV.ToString())
			}
			adjustedPos := matchPos + offset
			replaced := doReplace(str[matchPos:matchPos+matchLen], match, replacement)
			prefix := ""
			if adjustedPos > 0 && adjustedPos <= len(result) {
				prefix = result[:adjustedPos]
			}
			suffix := ""
			if adjustedPos+matchLen <= len(result) {
				suffix = result[adjustedPos+matchLen:]
			}
			result = prefix + replaced + suffix
			offset += len(replaced) - matchLen
			lastIdx := int(this.Get("lastIndex").ToNumber())
			if lastIdx <= 0 {
				break
			}
		}
		return NewString(result)
	}))

	// Symbol.search — called by String.prototype.search
	regExpProto.Set("[Symbol.search]", vm.createBuiltinFunction("RegExp.[Symbol.search]", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(-1)
		}
		str := args[0].ToString()
		compiled, err := compileJSRegexp(this.Get("__pattern__").ToString(), this.Get("__flags__").ToString())
		if err != nil {
			return NewNumber(-1)
		}
		loc := compiled.FindStringIndex(str)
		if loc == nil {
			return NewNumber(-1)
		}
		return NewNumber(float64(loc[0]))
	}))

	// Symbol.split — called by String.prototype.split
	regExpProto.Set("[Symbol.split]", vm.createBuiltinFunction("RegExp.[Symbol.split]", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			arr := NewJSObject()
			arr.ConstructorName = "Array"
			arr.Set("0", args[0])
			arr.Set("length", NewNumber(1))
			return NewObject(arr)
		}
		str := args[0].ToString()
		limit := int(^uint(0) >> 1) // max int
		if len(args) > 1 {
			limit = int(args[1].ToNumber())
		}
		compiled, err := compileJSRegexp(this.Get("__pattern__").ToString(), this.Get("__flags__").ToString())
		if err != nil {
			arr := NewJSObject()
			arr.ConstructorName = "Array"
			arr.Set("0", NewString(str))
			arr.Set("length", NewNumber(1))
			return NewObject(arr)
		}
		parts := compiled.Split(str, limit)
		arr := NewJSObject()
		arr.ConstructorName = "Array"
		for i, p := range parts {
			arr.Set(intKey(i), NewString(p))
		}
		arr.Set("length", NewNumber(float64(len(parts))))
		return NewObject(arr)
	}))

	regExpCtor.Set("prototype", NewObject(regExpProto))
	vm.globals["RegExp"] = NewObject(regExpCtor)
}

// doReplace performs replacement pattern substitution (handles $&, $1, etc.).
// Returns the processed replacement string without prefix/suffix.
func doReplace(str string, match JSValue, replacement JSValue) string {
	m := match.ObjVal
	if m == nil {
		return str
	}
	matched := m.Get("0").ToString()
	if replacement.IsObject() && replacement.ObjVal != nil && replacement.ObjVal.CallFunc != nil {
		// Function replacer.
		args := []JSValue{NewString(matched)}
		mLen := int(m.Get("length").ToNumber())
		for i := 1; i < mLen; i++ {
			args = append(args, m.Get(intKey(i)))
		}
		args = append(args, NewNumber(m.Get("index").ToNumber()))
		args = append(args, NewString(m.Get("input").ToString()))
		result := replacement.ObjVal.CallFunc(nil, args)
		return result.ToString()
	}
	repStr := replacement.ToString()
	// Handle $&, $`, $', $1..$9, $$ replacement patterns.
	repStr = strings.ReplaceAll(repStr, "$$", "\x00") // placeholder
	repStr = strings.ReplaceAll(repStr, "$&", matched)
	backtick := ""
	idxVal := m.Get("index").ToNumber()
	if idxVal > 0 && int(idxVal) <= len(str) {
		backtick = str[:int(idxVal)]
	}
	repStr = strings.ReplaceAll(repStr, "$`", backtick)
	singleQuote := ""
	if int(idxVal)+len(matched) < len(str) {
		singleQuote = str[int(idxVal)+len(matched):]
	}
	repStr = strings.ReplaceAll(repStr, "$'", singleQuote)
	// $1..$9 group references.
	for i := 1; i <= 9; i++ {
		group := m.Get(intKey(i))
		groupStr := ""
		if !group.IsUndefined() {
			groupStr = group.ToString()
		}
		repStr = strings.ReplaceAll(repStr, fmt.Sprintf("$%d", i), groupStr)
	}
	repStr = strings.ReplaceAll(repStr, "\x00", "$")
	return repStr
}

// doFullReplace performs a complete replacement including prefix and suffix.
func doFullReplace(str string, match JSValue, replacement JSValue) string {
	m := match.ObjVal
	if m == nil {
		return str
	}
	matchPos := int(m.Get("index").ToNumber())
	matched := m.Get("0").ToString()
	matchLen := len(matched)
	replaced := doReplace(str[matchPos:matchPos+matchLen], match, replacement)
	prefix := ""
	if matchPos > 0 {
		prefix = str[:matchPos]
	}
	suffix := ""
	if matchPos+matchLen < len(str) {
		suffix = str[matchPos+matchLen:]
	}
	return prefix + replaced + suffix
}

func (vm *VM) registerDate() {
	// Date constructor
	dateCtor := vm.createBuiltinFunction("Date", func(this *JSObject, args []JSValue) JSValue {
		// If called without new (this is the Date function object itself),
		// return date string per ECMAScript §21.4.2.
		if this == nil || this.ConstructorName == "Function" {
			return NewString(time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)"))
		}
		var t time.Time
		invalidDate := false
		switch {
		case len(args) == 0:
			t = time.Now()
		case len(args) == 1:
			val := args[0]
			if val.IsString() {
				parsed, err := parseDateString(val.StrVal)
				if err != nil {
					// Invalid date: store NaN per ECMAScript §21.4.3.
					invalidDate = true
				} else {
					t = parsed
				}
			} else {
				ms := int64(val.ToNumber())
				t = time.Unix(ms/1000, (ms%1000)*1e6).UTC()
			}
		default:
			year := int(args[0].ToNumber())
			month := time.Month(0)
			day, hour, minute, sec, ms := 1, 0, 0, 0, 0
			if len(args) > 1 {
				month = time.Month(int(args[1].ToNumber()))
			}
			if len(args) > 2 {
				day = int(args[2].ToNumber())
			}
			if len(args) > 3 {
				hour = int(args[3].ToNumber())
			}
			if len(args) > 4 {
				minute = int(args[4].ToNumber())
			}
			if len(args) > 5 {
				sec = int(args[5].ToNumber())
			}
			if len(args) > 6 {
				ms = int(args[6].ToNumber())
			}
			t = time.Date(year, month+1, day, hour, minute, sec, ms*1e6, time.UTC)
		}
		this.ConstructorName = "Date"
		if invalidDate {
			this.Set("__date_value__", NewNumber(math.NaN()))
		} else {
			this.Set("__date_value__", NewNumber(float64(t.UnixMilli())))
		}
		return NewObject(this)
	})

	dateCtor.ObjVal.Set("now", vm.createBuiltinFunction("Date.now", func(_ *JSObject, _ []JSValue) JSValue {
		return NewNumber(float64(time.Now().UnixMilli()))
	}))

	dateCtor.ObjVal.Set("parse", vm.createBuiltinFunction("Date.parse", func(_ *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsString() {
			return NewNumber(math.NaN())
		}
		t, err := parseDateString(args[0].StrVal)
		if err != nil {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(t.UnixMilli()))
	}))

	dateCtor.ObjVal.Set("UTC", vm.createBuiltinFunction("Date.UTC", func(_ *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return NewNumber(math.NaN())
		}
		year := int(args[0].ToNumber())
		month := int(args[1].ToNumber())
		day, hour, minute, sec, ms := 1, 0, 0, 0, 0
		if len(args) > 2 {
			day = int(args[2].ToNumber())
		}
		if len(args) > 3 {
			hour = int(args[3].ToNumber())
		}
		if len(args) > 4 {
			minute = int(args[4].ToNumber())
		}
		if len(args) > 5 {
			sec = int(args[5].ToNumber())
		}
		if len(args) > 6 {
			ms = int(args[6].ToNumber())
		}
		t := time.Date(year, time.Month(month+1), day, hour, minute, sec, ms*1e6, time.UTC)
		return NewNumber(float64(t.UnixMilli()))
	}))

	// Date prototype
	dateProto := NewJSObject()
	dateProto.ConstructorName = "Date"
	DatePrototype = dateProto

	getDateMs := func(this *JSObject) float64 {
		return this.Get("__date_value__").ToNumber()
	}
	isInvalidDate := func(ms float64) bool {
		return math.IsNaN(ms)
	}
	getDateValue := func(this *JSObject) time.Time {
		ms := getDateMs(this)
		return time.Unix(int64(ms)/1000, (int64(ms)%1000)*1e6).UTC()
	}

	dateProto.Set("getTime", vm.createBuiltinFunction("Date.getTime", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(ms)
	}))
	dateProto.Set("getFullYear", vm.createBuiltinFunction("Date.getFullYear", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(getDateValue(this).Year()))
	}))
	dateProto.Set("getMonth", vm.createBuiltinFunction("Date.getMonth", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(getDateValue(this).Month() - 1))
	}))
	dateProto.Set("getDate", vm.createBuiltinFunction("Date.getDate", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(getDateValue(this).Day()))
	}))
	dateProto.Set("getDay", vm.createBuiltinFunction("Date.getDay", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(getDateValue(this).Weekday()))
	}))
	dateProto.Set("getHours", vm.createBuiltinFunction("Date.getHours", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(getDateValue(this).Hour()))
	}))
	dateProto.Set("getMinutes", vm.createBuiltinFunction("Date.getMinutes", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(getDateValue(this).Minute()))
	}))
	dateProto.Set("getSeconds", vm.createBuiltinFunction("Date.getSeconds", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(getDateValue(this).Second()))
	}))
	dateProto.Set("getMilliseconds", vm.createBuiltinFunction("Date.getMilliseconds", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(int64(ms) % 1000))
	}))
	dateProto.Set("toString", vm.createBuiltinFunction("Date.toString", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewString("Invalid Date")
		}
		return NewString(getDateValue(this).Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)"))
	}))
	dateProto.Set("toISOString", vm.createBuiltinFunction("Date.toISOString", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewString("Invalid Date")
		}
		return NewString(getDateValue(this).Format("2006-01-02T15:04:05.000Z"))
	}))
	dateProto.Set("valueOf", vm.createBuiltinFunction("Date.valueOf", func(this *JSObject, _ []JSValue) JSValue {
		ms := getDateMs(this)
		if isInvalidDate(ms) {
			return NewNumber(math.NaN())
		}
		return NewNumber(ms)
	}))

	dateCtor.ObjVal.Set("prototype", NewObject(dateProto))
	vm.globals["Date"] = dateCtor
}

// parseDateString parses common date formats: ISO 8601, RFC 3339, date-only.
func parseDateString(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02",
		time.RFC1123,
		time.RFC1123Z,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable date: %s", s)
}
