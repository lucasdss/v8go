// builtins.go — Standard JavaScript built-in objects.
//
// Implements console, Object, Array, Math, and other ECMAScript built-ins
// as Go functions that can be called from the VM's bytecode interpreter.
package js

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	browserNet "github.com/lucasdss/v8go/pkg/net"
)

// StringPrototype holds the String.prototype object (set by RegisterBuiltins, used for autoboxing).
var StringPrototype *JSObject

// RegExpPrototype holds the RegExp.prototype object (set by registerRegExp, used for regexp literal creation).
var RegExpPrototype *JSObject

// ArrayBufferPrototype holds the ArrayBuffer.prototype object.
var ArrayBufferPrototype *JSObject

// DataViewPrototype holds the DataView.prototype object.
var DataViewPrototype *JSObject

// DatePrototype holds the Date.prototype object.
var DatePrototype *JSObject

// precompiledBuiltins holds bytecode-compiled versions of frequently-called builtins.
// These are compiled at init time and executed directly by the VM instead of
// dispatching through Go function wrappers, reducing call overhead.
var precompiledBuiltins map[string]*BytecodeFunction

// compileBuiltinSource compiles a JS function source string and returns
// the inner BytecodeFunction (the actual function body), not the wrapper program.
func compileBuiltinSource(source string) *BytecodeFunction {
	bf := CompileString(source)
	if bf == nil {
		return nil
	}
	// The compiled program wraps the function declaration in a template object
	// stored in the constant pool. Extract the inner BytecodeFunction.
	for _, c := range bf.Constants {
		if c.IsObject() && c.ObjVal != nil && c.ObjVal.Bytecode != nil {
			return c.ObjVal.Bytecode
		}
	}
	return nil
}

func init() {
	precompiledBuiltins = make(map[string]*BytecodeFunction)

	// --- Array methods (use `this`, matching JS calling convention) ---

	// Array.push(val) — push one value and return new length
	if bf := compileBuiltinSource(`function push(val) {
		var len = this.length;
		this[len] = val;
		this.length = len + 1;
		return this.length;
	}`); bf != nil {
		precompiledBuiltins["Array.push"] = bf
	}

	// Array.pop() — pop and return last element
	if bf := compileBuiltinSource(`function pop() {
		var len = this.length;
		if (len === 0) { return; }
		var last = this[len - 1];
		this.length = len - 1;
		return last;
	}`); bf != nil {
		precompiledBuiltins["Array.pop"] = bf
	}

	// Array.shift() — remove and return first element
	if bf := compileBuiltinSource(`function shift() {
		var len = this.length;
		if (len === 0) { return; }
		var first = this[0];
		for (var i = 0; i < len - 1; i = i + 1) {
			this[i] = this[i + 1];
		}
		this.length = len - 1;
		return first;
	}`); bf != nil {
		precompiledBuiltins["Array.shift"] = bf
	}

	// Array.unshift(val) — prepend value and return new length
	if bf := compileBuiltinSource(`function unshift(val) {
		var len = this.length;
		for (var i = len; i > 0; i = i - 1) {
			this[i] = this[i - 1];
		}
		this[0] = val;
		this.length = len + 1;
		return this.length;
	}`); bf != nil {
		precompiledBuiltins["Array.unshift"] = bf
	}

	// Array.indexOf(search, fromIndex) — find first index of element
	if bf := compileBuiltinSource(`function indexOf(search, fromIndex) {
		var len = this.length;
		var start = 0;
		if (fromIndex !== undefined) { start = fromIndex; }
		if (start < 0) { start = len + start; }
		if (start < 0) { start = 0; }
		for (var i = start; i < len; i = i + 1) {
			if (this[i] === search) { return i; }
		}
		return -1;
	}`); bf != nil {
		precompiledBuiltins["Array.indexOf"] = bf
	}

	// Array.join(sep) — join elements with separator
	if bf := compileBuiltinSource(`function join(sep) {
		if (sep === undefined) { sep = ","; }
		var len = this.length;
		if (len === 0) { return ""; }
		var result = "";
		for (var i = 0; i < len; i = i + 1) {
			if (i > 0) { result = result + sep; }
			var elem = this[i];
			if (elem !== undefined && elem !== null) {
				result = result + elem;
			}
		}
		return result;
	}`); bf != nil {
		precompiledBuiltins["Array.join"] = bf
	}

	// --- String methods (use `this`, matching JS calling convention) ---

	// String.slice(start, end) — substring by indices
	if bf := compileBuiltinSource(`function slice(start, end) {
		var len = this.length;
		if (start === undefined) { start = 0; }
		if (end === undefined) { end = len; }
		if (start < 0) { start = len + start; }
		if (end < 0) { end = len + end; }
		if (start < 0) { start = 0; }
		if (end > len) { end = len; }
		var result = "";
		for (var i = start; i < end; i = i + 1) {
			result = result + this[i];
		}
		return result;
	}`); bf != nil {
		precompiledBuiltins["String.slice"] = bf
	}

	// String.indexOf(search, fromIndex) — find first occurrence
	if bf := compileBuiltinSource(`function indexOf(search, fromIndex) {
		var s = this;
		if (search === undefined || search === "") { return 0; }
		var from = 0;
		if (fromIndex !== undefined) { from = fromIndex; }
		if (from < 0) { from = 0; }
		for (var i = from; i < s.length; i = i + 1) {
			var match = true;
			for (var j = 0; j < search.length; j = j + 1) {
				if (i + j >= s.length || s[i + j] !== search[j]) { match = false; break; }
			}
			if (match) { return i; }
		}
		return -1;
	}`); bf != nil {
		precompiledBuiltins["String.indexOf"] = bf
	}

	// String.charAt(index) — get character at position
	if bf := compileBuiltinSource(`function charAt(index) {
		if (index < 0 || index >= this.length) { return ""; }
		return this[index];
	}`); bf != nil {
		precompiledBuiltins["String.charAt"] = bf
	}

	// String.trim() — remove whitespace from both ends
	if bf := compileBuiltinSource(`function trim() {
		var s = this;
		var start = 0;
		var end = s.length;
		while (start < end && (s[start] === " " || s[start] === "\n" || s[start] === "\r" || s[start] === "\t")) {
			start = start + 1;
		}
		while (end > start && (s[end - 1] === " " || s[end - 1] === "\n" || s[end - 1] === "\r" || s[end - 1] === "\t")) {
			end = end - 1;
		}
		return s.slice(start, end);
	}`); bf != nil {
		precompiledBuiltins["String.trim"] = bf
	}
}

// RegisterBuiltins wires up all standard built-in objects into the VM.
func (vm *VM) RegisterBuiltins() {
	vm.registerConsole()
	vm.registerObject()
	vm.registerArray()
	vm.registerString()
	vm.registerMath()
	vm.registerNumber()
	vm.registerGlobalFunctions()
	vm.registerJSON()
	vm.registerError()
	vm.registerFunctionProto()
	vm.registerPromise()
	vm.registerMap()
	vm.registerSet()
	vm.registerWeakMap()
	vm.registerWeakSet()
	vm.registerSymbol()
	vm.registerBigInt()
	vm.registerDate()
	vm.registerBoolean()
	vm.registerRegExp()
	vm.registerDOMEvents()
	vm.registerArrayBuffer()
	vm.registerDataView()
	vm.registerTypedArrays()
	vm.registerModules()
	vm.registerProxy()
	vm.registerReflect()
	vm.registerEval()
	vm.registerWeakRef()
	vm.registerFinalizationRegistry()
}

// registerDOMEvents registers the __goDispatchEvent builtin for inline event handler dispatch.
func (vm *VM) registerDOMEvents() {
	// __goDispatchEvent(targetID, eventType)
	// Called from JS or browser bridge to dispatch an event to an element's inline handler.
	// The handler receives a synthetic event object: { type, target, preventDefault() }.
	vm.registry.Builtins["__goDispatchEvent"] = func(args []JSValue) JSValue {
		if len(args) < 2 {
			return False
		}
		targetID := args[0].ToString()
		eventType := args[1].ToString()
		vm.DispatchEvent(targetID, eventType, nil)
		// DispatchEvent already triggers domChangeCallback internally,
		// but we also fire it here to cover any VM-level mutations.
		vm.mu.Lock()
		cb := vm.events.DOMChangeCallback()
		vm.mu.Unlock()
		if cb != nil {
			cb()
		}
		return True
	}
}

func (vm *VM) registerConsole() {
	consoleObj := NewJSObject()
	consoleObj.ConstructorName = "Console"

	consoleObj.Set("log", vm.createBuiltinFunction("console.log", func(this *JSObject, args []JSValue) JSValue {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.String()
		}
		msg := strings.Join(parts, " ")
		vm.console.Log(msg)
		return Undefined
	}))

	consoleObj.Set("warn", vm.createBuiltinFunction("console.warn", func(this *JSObject, args []JSValue) JSValue {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.String()
		}
		msg := "WARN: " + strings.Join(parts, " ")
		vm.console.Log(msg)
		return Undefined
	}))

	consoleObj.Set("error", vm.createBuiltinFunction("console.error", func(this *JSObject, args []JSValue) JSValue {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.String()
		}
		msg := "ERROR: " + strings.Join(parts, " ")
		vm.console.Log(msg)
		return Undefined
	}))

	vm.globals.M["console"] = NewObject(consoleObj)
}





func (vm *VM) createBuiltinFunction(name string, fn func(this *JSObject, args []JSValue) JSValue) JSValue {
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return NewObject(obj)
}

// createBuiltinBytecode creates a builtin function from precompiled bytecode.
// The bytecode function MUST use `this` for the receiver (matching standard JS convention).
// This is faster than Go function dispatch for simple operations since it avoids
// crossing the Go↔JS boundary.
func (vm *VM) createBuiltinBytecode(name string, bf *BytecodeFunction) JSValue {
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.Bytecode = bf
	obj.Set("length", NewNumber(float64(bf.NumParams)))
	return NewObject(obj)
}

// createBuiltinWithFallback creates a builtin function using precompiled bytecode
// if available, otherwise falls back to the Go function implementation.
// NOTE: Bytecode path is disabled — precompiled builtins need additional
// fixes beyond the local variable scoping support.
// The precompiled bytecode is kept ready in precompiledBuiltins for when
// the full bytecode path is ready.
func (vm *VM) createBuiltinWithFallback(name string, defaultFn func(this *JSObject, args []JSValue) JSValue) JSValue {
	// if bf, ok := precompiledBuiltins[name]; ok {
	// 	return vm.createBuiltinBytecode(name, bf)
	// }
	return vm.createBuiltinFunction(name, defaultFn)
}

// --- Console log retrieval for tests ---

// GetGlobal returns the value of a global variable by name.
func (vm *VM) GetGlobal(name string) JSValue {
	return vm.globals.Get(name)
}

// SetGlobal sets a global variable on the VM.
// Used by the module registry to inject import bindings before module execution.
// Also updates the slot cache so that OpLdaGlobalSlot fast-path reads the correct value.
func (vm *VM) SetGlobal(name string, val JSValue) {
	vm.globals.Set(name, val)
}

// RunAsync runs fn in a new goroutine. The VM's asyncWg tracks the goroutine
// so that WaitAsync can block until it completes. Async builtins (fetch, XHR)
// use this to avoid data races with tests.
func (vm *VM) RunAsync(fn func()) {
	vm.asyncWg.Add(1)
	go func() {
		defer vm.asyncWg.Done()
		fn()
	}()
}

// WaitAsync blocks until all pending RunAsync goroutines have completed.
func (vm *VM) WaitAsync() {
	vm.asyncWg.Wait()
}

// ConsoleLogs returns the accumulated console.log output.
func (vm *VM) ConsoleLogs() []string {
	return vm.console.Logs()
}

// ClearConsoleLogs clears the console log buffer.
func (vm *VM) ClearConsoleLogs() {
	vm.console.Clear()
}

func (vm *VM) registerGlobalFunctions() {
	// NaN and Infinity globals
	vm.globals.M["NaN"] = NewNumber(math.NaN())
	vm.globals.M["Infinity"] = NewNumber(math.Inf(1))
	vm.globals.M["undefined"] = Undefined

	// globalThis — returns the global object.
	vm.globals.M["globalThis"] = vm.globalObject()

	// parseInt(string, radix) — parses a string to integer in given radix.
	vm.globals.M["parseInt"] = vm.createBuiltinFunction("parseInt", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.NaN())
		}
		s := strings.TrimSpace(args[0].ToString())
		radix := 0 // auto-detect per spec
		if len(args) > 1 {
			radix = int(args[1].ToNumber())
		}
		// Handle hex prefix when radix is 0 or 16.
		if radix == 0 || radix == 16 {
			if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
				radix = 16
				s = s[2:]
			}
		}
		if radix == 0 {
			radix = 10
		}
		if radix < 2 || radix > 36 {
			return NewNumber(math.NaN())
		}
		n, err := strconv.ParseInt(s, radix, 64)
		if err != nil {
			return NewNumber(math.NaN())
		}
		return NewNumber(float64(n))
	})

	// parseFloat(string) — parses a string to float.
	vm.globals.M["parseFloat"] = vm.createBuiltinFunction("parseFloat", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewNumber(math.NaN())
		}
		s := strings.TrimSpace(args[0].ToString())
		var buf strings.Builder
		hasDot, hasExp, hasDigit := false, false, false
		for i := 0; i < len(s); i++ {
			r := s[i]
			if r >= '0' && r <= '9' {
				buf.WriteByte(r)
				hasDigit = true
			} else if r == '.' && !hasDot && !hasExp {
				buf.WriteByte(r)
				hasDot = true
			} else if (r == 'e' || r == 'E') && !hasExp && hasDigit {
				buf.WriteByte(r)
				hasExp = true
				// Consume optional sign.
				if i+1 < len(s) && (s[i+1] == '+' || s[i+1] == '-') {
					buf.WriteByte(s[i+1])
					i++ // skip sign
				}
			} else {
				break
			}
		}
		if !hasDigit {
			return NewNumber(math.NaN())
		}
		n, err := strconv.ParseFloat(buf.String(), 64)
		if err != nil {
			return NewNumber(math.NaN())
		}
		return NewNumber(n)
	})

	// isNaN(value) — returns true if ToNumber(value) is NaN.
	vm.globals.M["isNaN"] = vm.createBuiltinFunction("isNaN", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return True
		}
		return NewBoolean(math.IsNaN(args[0].ToNumber()))
	})

	// isFinite(value) — returns true if value is a finite number.
	vm.globals.M["isFinite"] = vm.createBuiltinFunction("isFinite", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		n := args[0].ToNumber()
		return NewBoolean(!math.IsNaN(n) && !math.IsInf(n, 0))
	})

	// encodeURIComponent(string) — percent-encodes a string.
	vm.globals.M["encodeURIComponent"] = vm.createBuiltinFunction("encodeURIComponent", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewString("undefined")
		}
		return NewString(urlEncode(args[0].ToString()))
	})

	// decodeURIComponent(string) — percent-decodes a URI component.
	vm.globals.M["decodeURIComponent"] = vm.createBuiltinFunction("decodeURIComponent", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewString("undefined")
		}
		return NewString(urlDecode(args[0].ToString()))
	})

	// --- fetch(url, options) ---
	vm.globals.M["fetch"] = vm.createBuiltinFunction("fetch", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return vm.NewPromise(func(resolve, reject func(JSValue)) {
				reject(NewString("TypeError: fetch requires at least 1 argument"))
			})
		}
		urlStr := args[0].ToString()
		method := "GET"
		headers := make(map[string]string)
		var bodyStr string

		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil {
			opts := args[1].ObjVal
			if m := opts.Get("method"); !m.IsUndefined() {
				method = strings.ToUpper(m.ToString())
			}
			if h := opts.Get("headers"); h.IsObject() && h.ObjVal != nil {
				for _, key := range objectKeys(h.ObjVal) {
					headers[key] = h.ObjVal.Get(key).ToString()
				}
			}
			if b := opts.Get("body"); !b.IsUndefined() {
				bodyStr = b.ToString()
			}
		}

		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			vm.RunAsync(func() {
				parsedURL, err := browserNet.ParseURL(urlStr)
				if err != nil {
					reject(NewString("TypeError: invalid URL: " + err.Error()))
					return
				}

				var body io.Reader
				if bodyStr != "" {
					body = strings.NewReader(bodyStr)
				}

				req := &browserNet.Request{
					Method:  method,
					URL:     parsedURL,
					Headers: headers,
					Body:    body,
				}

				resp, err := browserNet.Fetch(context.Background(), req)
				if err != nil {
					reject(NewString("TypeError: fetch failed: " + err.Error()))
					return
				}
				defer resp.Body.Close()

				bodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					reject(NewString("TypeError: failed to read response body: " + err.Error()))
					return
				}

				// Build Response object
				respObj := NewJSObject()
				respObj.ConstructorName = "Response"
				respObj.Set("status", NewNumber(float64(resp.Status)))
				respObj.Set("statusText", NewString(resp.StatusText))
				respObj.Set("ok", NewBoolean(resp.OK()))
				bodyText := string(bodyBytes)
				respObj.Set("__body__", NewString(bodyText))

				// Headers object
				headersObj := NewJSObject()
				for k, v := range resp.Headers {
					headersObj.Set(k, NewString(v))
				}
				respObj.Set("headers", NewObject(headersObj))

				// text() method — returns Promise<string>
				respObj.Set("text", vm.createBuiltinFunction("Response.text", func(thisResp *JSObject, _ []JSValue) JSValue {
					txt := thisResp.Get("__body__").ToString()
					return vm.NewPromise(func(res2, _ func(JSValue)) {
						res2(NewString(txt))
					})
				}))

				// json() method — returns Promise<parsed>
				respObj.Set("json", vm.createBuiltinFunction("Response.json", func(thisResp *JSObject, _ []JSValue) JSValue {
					txt := thisResp.Get("__body__").ToString()
					return vm.NewPromise(func(res2, rej2 func(JSValue)) {
						var result jsonNode
						if err := json.Unmarshal([]byte(txt), &result); err != nil {
							rej2(NewString("SyntaxError: " + err.Error()))
							return
						}
						res2(jsonNodeToJSValue(&result))
					})
				}))

				// blob() stub
				respObj.Set("blob", vm.createBuiltinFunction("Response.blob", func(thisResp *JSObject, _ []JSValue) JSValue {
					return vm.NewPromise(func(res2, _ func(JSValue)) {
						res2(NewObject(NewJSObject()))
					})
				}))

				resolve(NewObject(respObj))
			})
		})
	})

	// --- XMLHttpRequest ---
	vm.registerXHR()
}

// objectKeys returns the own enumerable property keys of a JSObject.
func objectKeys(obj *JSObject) []string {
	if obj == nil {
		return nil
	}
	var keys []string
	if obj.Shape.IsDictionary {
		for k := range obj.Dictionary {
			keys = append(keys, k)
		}
	} else {
		for name, entry := range obj.Shape.Properties {
			if entry.Attr&AttrEnumerable != 0 {
				keys = append(keys, name)
			}
		}
	}
	return keys
}

func (vm *VM) registerXHR() {
	xhrCtor := NewJSObject()
	xhrCtor.ConstructorName = "Function"
	xhrCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		xhr := NewJSObject()
		xhr.ConstructorName = "XMLHttpRequest"
		xhr.Set("readyState", NewNumber(0))
		xhr.Set("status", NewNumber(0))
		xhr.Set("statusText", NewString(""))
		xhr.Set("responseText", NewString(""))
		xhr.Set("UNSENT", NewNumber(0))
		xhr.Set("OPENED", NewNumber(1))
		xhr.Set("HEADERS_RECEIVED", NewNumber(2))
		xhr.Set("LOADING", NewNumber(3))
		xhr.Set("DONE", NewNumber(4))

		// open(method, url)
		xhr.Set("open", vm.createBuiltinFunction("XMLHttpRequest.open", func(thisXHR *JSObject, args []JSValue) JSValue {
			if len(args) >= 2 {
				urlStr := args[1].ToString()
				thisXHR.Set("__xhr_method__", NewString(strings.ToUpper(args[0].ToString())))
				thisXHR.Set("__xhr_url__", NewString(urlStr))
				thisXHR.Set("readyState", NewNumber(1))
			}
			return Undefined
		}))

		// setRequestHeader(name, value)
		xhr.Set("setRequestHeader", vm.createBuiltinFunction("XMLHttpRequest.setRequestHeader", func(thisXHR *JSObject, args []JSValue) JSValue {
			if len(args) >= 2 {
				hdrsVal := thisXHR.Get("__xhr_headers__")
				var hdrsObj *JSObject
				if hdrsVal.IsObject() && hdrsVal.ObjVal != nil {
					hdrsObj = hdrsVal.ObjVal
				} else {
					hdrsObj = NewJSObject()
					thisXHR.Set("__xhr_headers__", NewObject(hdrsObj))
				}
				hdrsObj.Set(args[0].ToString(), args[1])
			}
			return Undefined
		}))

		// send(body)
		xhr.Set("send", vm.createBuiltinFunction("XMLHttpRequest.send", func(thisXHR *JSObject, args []JSValue) JSValue {
			var bodyStr string
			if len(args) > 0 {
				bodyStr = args[0].ToString()
			}

			urlStr := thisXHR.Get("__xhr_url__").ToString()
			method := thisXHR.Get("__xhr_method__").ToString()
			if method == "" {
				method = "GET"
			}
			thisXHR.Set("readyState", NewNumber(2))

			vm.RunAsync(func() {
				parsedURL, err := browserNet.ParseURL(urlStr)
				if err != nil {
					thisXHR.Set("readyState", NewNumber(4))
					thisXHR.Set("status", NewNumber(0))
					return
				}

				var body io.Reader
				if bodyStr != "" {
					body = strings.NewReader(bodyStr)
				}

				hdrsVal := thisXHR.Get("__xhr_headers__")
				reqHeaders := make(map[string]string)
				if hdrsVal.IsObject() && hdrsVal.ObjVal != nil {
					for _, key := range objectKeys(hdrsVal.ObjVal) {
						reqHeaders[key] = hdrsVal.ObjVal.Get(key).ToString()
					}
				}

				resp, err := browserNet.Fetch(context.Background(), &browserNet.Request{
					Method:  method,
					URL:     parsedURL,
					Headers: reqHeaders,
					Body:    body,
				})
				if err != nil {
					thisXHR.Set("readyState", NewNumber(4))
					thisXHR.Set("status", NewNumber(0))
					return
				}
				defer resp.Body.Close()

				bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				vm.console.Log("[XHR] read error: " + err.Error())
				thisXHR.Set("readyState", NewNumber(4))
				thisXHR.Set("status", NewNumber(float64(resp.Status)))
				thisXHR.Set("statusText", NewString(resp.StatusText))
				thisXHR.Set("responseText", NewString(""))
				return
			}

				thisXHR.Set("readyState", NewNumber(4))
				thisXHR.Set("status", NewNumber(float64(resp.Status)))
				thisXHR.Set("statusText", NewString(resp.StatusText))
				thisXHR.Set("responseText", NewString(string(bodyBytes)))

				// Set response headers
				respHeadersObj := NewJSObject()
				for k, v := range resp.Headers {
					respHeadersObj.Set(k, NewString(v))
				}
				// Use __xhr_responseHeaders__ internally or just rely on getResponseHeader/getAllResponseHeaders
				_ = respHeadersObj

				// Call onload callback
				onloadVal := thisXHR.Get("onload")
				if onloadVal.IsObject() && onloadVal.ObjVal != nil && onloadVal.ObjVal.isCallable() {
					onloadVal.ObjVal.Call(thisXHR, nil)
				}
			})

			return Undefined
		}))

		return NewObject(xhr)
	}

	vm.globals.M["XMLHttpRequest"] = NewObject(xhrCtor)
}


func (vm *VM) registerError() {
	// Error prototype for instanceof checks.
	errorProto := NewJSObject()
	errorProto.ConstructorName = "Error" // override "Object" default

	// Error constructor.
	errorCtor := NewJSObject()
	errorCtor.ConstructorName = "Function"
	errorCtor.Set("prototype", NewObject(errorProto))
	errorCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		msg := ""
		if len(args) > 0 {
			msg = args[0].ToString()
		}
		err := NewJSObject()
		err.ConstructorName = "Error"
		err.Prototype = errorProto
		err.Set("name", NewString("Error"))
		err.Set("message", NewString(msg))
		// Build stack trace from calltrack (reversed — most recent call first).
		var sb strings.Builder
		sb.WriteString("Error: ")
		sb.WriteString(msg)
		stack := vm.calltrack.Stack()
		for i := len(stack) - 1; i >= 0; i-- {
			frame := stack[i]
			sb.WriteString("\n    at ")
			sb.WriteString(frame.Name)
			if frame.File != "" {
				sb.WriteString(" (")
				sb.WriteString(frame.File)
				sb.WriteString(":")
				sb.WriteString(strconv.Itoa(frame.Line))
				sb.WriteString(":")
				sb.WriteString(strconv.Itoa(frame.Col))
				sb.WriteString(")")
			}
		}
		err.Set("stack", NewString(sb.String()))
		return NewObject(err)
	}

	vm.globals.M["Error"] = NewObject(errorCtor)

	vm.registerErrorSubtype("TypeError", errorProto)
	vm.registerErrorSubtype("SyntaxError", errorProto)
	vm.registerErrorSubtype("RangeError", errorProto)
	vm.registerErrorSubtype("ReferenceError", errorProto)
}

func (vm *VM) registerErrorSubtype(name string, errorProto *JSObject) {
	ctor := NewJSObject()
	ctor.ConstructorName = "Function"

	// Create a proper subtype prototype that inherits from Error.prototype.
	// This ensures TypeError.prototype !== Error.prototype while still
	// satisfying new TypeError('x') instanceof Error.
	subProto := NewJSObject()
	subProto.ConstructorName = name // "TypeError", "SyntaxError", etc.
	subProto.Prototype = errorProto
	subProto.Set("name", NewString(name))

	ctor.Set("prototype", NewObject(subProto))

	ctor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		msg := ""
		if len(args) > 0 {
			msg = args[0].ToString()
		}
		err := NewJSObject()
		err.ConstructorName = name
		err.Prototype = subProto // inherit from subProto → errorProto
		err.Set("name", NewString(name))
		err.Set("message", NewString(msg))
		// Build stack trace from calltrack (reversed — most recent call first).
		var sb strings.Builder
		sb.WriteString(name)
		sb.WriteString(": ")
		sb.WriteString(msg)
		stack := vm.calltrack.Stack()
		for i := len(stack) - 1; i >= 0; i-- {
			frame := stack[i]
			sb.WriteString("\n    at ")
			sb.WriteString(frame.Name)
			if frame.File != "" {
				sb.WriteString(" (")
				sb.WriteString(frame.File)
				sb.WriteString(":")
				sb.WriteString(strconv.Itoa(frame.Line))
				sb.WriteString(":")
				sb.WriteString(strconv.Itoa(frame.Col))
				sb.WriteString(")")
			}
		}
		err.Set("stack", NewString(sb.String()))
		return NewObject(err)
	}
	vm.globals.M[name] = NewObject(ctor)
}

func (vm *VM) registerFunctionProto() {
	// Function.prototype.call
	fnProto := NewJSObject()
	fnProto.ConstructorName = "Function"

	fnProto.Set("call", vm.createBuiltinFunction("Function.call", func(this *JSObject, args []JSValue) JSValue {
		// this is the function object to call.
		if !this.isCallable() && this.Bytecode == nil {
			return Undefined
		}
		var thisArg *JSObject
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil {
			thisArg = args[0].ObjVal
		}
		callArgs := args
		if len(args) > 0 {
			callArgs = args[1:]
		}
		if this.CallFunc != nil {
			return this.CallFunc(thisArg, callArgs)
		}
		if this.Bytecode != nil {
			return callBytecodeFunction(this.Bytecode, callArgs)
		}
		return Undefined
	}))

	fnProto.Set("apply", vm.createBuiltinFunction("Function.apply", func(this *JSObject, args []JSValue) JSValue {
		if !this.isCallable() && this.Bytecode == nil {
			return Undefined
		}
		var thisArg *JSObject
		callArgs := []JSValue{}
		if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil {
			thisArg = args[0].ObjVal
		}
		if len(args) > 1 && args[1].IsObject() && args[1].ObjVal != nil {
			arrObj := args[1].ObjVal
			length := int(arrObj.Get("length").ToNumber())
			for i := 0; i < length; i++ {
				callArgs = append(callArgs, arrObj.Get(intKey(i)))
			}
		}
		if this.CallFunc != nil {
			return this.CallFunc(thisArg, callArgs)
		}
		if this.Bytecode != nil {
			return callBytecodeFunction(this.Bytecode, callArgs)
		}
		return Undefined
	}))

	fnProto.Set("bind", vm.createBuiltinFunction("Function.bind", func(this *JSObject, args []JSValue) JSValue {
		bound := NewJSObject()
		bound.ConstructorName = "Function"
		bound.CallFunc = func(innerThis *JSObject, innerArgs []JSValue) JSValue {
			// Merge bound args with call args.
			allArgs := append([]JSValue{}, args[1:]...)
			allArgs = append(allArgs, innerArgs...)
			var thisArg *JSObject
			if len(args) > 0 && args[0].IsObject() && args[0].ObjVal != nil {
				thisArg = args[0].ObjVal
			}
			if this.CallFunc != nil {
				return this.CallFunc(thisArg, allArgs)
			}
			if this.Bytecode != nil {
				return callBytecodeFunction(this.Bytecode, allArgs)
			}
			return Undefined
		}
		return NewObject(bound)
	}))

	// Attach prototype to all function objects (ObjectPrototype for now).
	_ = fnProto
}


// globalSymbolRegistry is the global symbol registry for Symbol.for / Symbol.keyFor.
var globalSymbolRegistry = make(map[string]JSValue)
var symbolRegistryMu sync.Mutex

// SymbolFor returns a symbol from the global registry identified by key,
// or creates and registers a new one if none exists for that key.
func SymbolFor(key string) JSValue {
	symbolRegistryMu.Lock()
	defer symbolRegistryMu.Unlock()
	if sym, ok := globalSymbolRegistry[key]; ok {
		return sym
	}
	sym := NewSymbol(key)
	globalSymbolRegistry[key] = sym
	return sym
}

// SymbolKeyFor returns the key for a registered symbol, or "" if not found.
func SymbolKeyFor(sym JSValue) string {
	if !sym.IsSymbol() {
		return ""
	}
	symbolRegistryMu.Lock()
	defer symbolRegistryMu.Unlock()
	for key, s := range globalSymbolRegistry {
		if s.StrictEquals(sym) {
			return key
		}
	}
	return ""
}

// registerSymbol registers the Symbol() constructor, Symbol.for(), Symbol.keyFor(),
// and well-known symbols (Symbol.iterator, Symbol.toStringTag).
func (vm *VM) registerSymbol() {
	// Symbol(description) — create a new unique Symbol value.
	symCtor := NewJSObject()
	symCtor.ConstructorName = "Function"
	symCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		desc := ""
		if len(args) > 0 {
			desc = args[0].ToString()
		}
		return NewSymbol(desc)
	}

	// Symbol.for(key) — returns existing symbol from registry or creates new one.
	symCtor.Set("for", vm.createBuiltinFunction("Symbol.for", func(this *JSObject, args []JSValue) JSValue {
		key := ""
		if len(args) > 0 {
			key = args[0].ToString()
		}
		return SymbolFor(key)
	}))

	// Symbol.keyFor(sym) — returns the key for a registered symbol, or undefined.
	symCtor.Set("keyFor", vm.createBuiltinFunction("Symbol.keyFor", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsSymbol() {
			return Undefined
		}
		key := SymbolKeyFor(args[0])
		if key == "" {
			return Undefined
		}
		return NewString(key)
	}))

	// Well-known symbol: Symbol.iterator
	symCtor.Set("iterator", NewSymbol("Symbol.iterator"))

	// Well-known symbol: Symbol.toStringTag
	symCtor.Set("toStringTag", NewSymbol("Symbol.toStringTag"))

	// Additional well-known symbols
	symCtor.Set("species", NewSymbol("Symbol.species"))
	symCtor.Set("toPrimitive", NewSymbol("Symbol.toPrimitive"))
	symCtor.Set("hasInstance", NewSymbol("Symbol.hasInstance"))
	symCtor.Set("match", NewSymbol("Symbol.match"))
	symCtor.Set("replace", NewSymbol("Symbol.replace"))
	symCtor.Set("search", NewSymbol("Symbol.search"))
	symCtor.Set("split", NewSymbol("Symbol.split"))
	symCtor.Set("unscopables", NewSymbol("Symbol.unscopables"))

	vm.globals.M["Symbol"] = NewObject(symCtor)
}

// --- RegExp built-in ---

// regExpExec performs the core RegExp matching logic.
// Returns a result array (with index, input, groups) or null.



// ---------------------------------------------------------------------------
// ArrayBuffer
// ---------------------------------------------------------------------------

func (vm *VM) registerArrayBuffer() {
	ArrayBufferPrototype = NewJSObject()
	ArrayBufferPrototype.ConstructorName = "ArrayBuffer"

	// byteLength is set per-instance; the prototype value is a fallback.
	ArrayBufferPrototype.Set("byteLength", NewNumber(0))

	// ArrayBuffer.prototype.slice(begin, end)
	ArrayBufferPrototype.Set("slice", NewObject(builtinFunc("ArrayBuffer.slice", func(this *JSObject, args []JSValue) JSValue {
		if this.typedArray == nil || this.typedArray.ByteData == nil {
			return NewObject(newArrayBuffer(0))
		}
		totalLen := len(this.typedArray.ByteData)
		begin := 0
		end := totalLen
		if len(args) > 0 {
			begin = int(clampToInt(args[0].ToNumber(), totalLen))
		}
		if len(args) > 1 && !args[1].IsUndefined() {
			end = int(clampToInt(args[1].ToNumber(), totalLen))
		}
		if begin < 0 {
			begin = totalLen + begin
		}
		if end < 0 {
			end = totalLen + end
		}
		if begin < 0 {
			begin = 0
		}
		if end > totalLen {
			end = totalLen
		}
		if begin > end {
			begin = end
		}
		newLen := end - begin
		ab := newArrayBuffer(newLen)
		copy(ab.ensureTypedArray().ByteData, this.typedArray.ByteData[begin:end])
		return NewObject(ab)
	})))

	arrayBufferCtor := NewJSObject()
	arrayBufferCtor.ConstructorName = "Function"
	arrayBufferCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		byteLength := 0
		if len(args) > 0 {
			byteLength = int(args[0].ToNumber())
		}
		if byteLength < 0 {
			byteLength = 0
		}
		return NewObject(newArrayBuffer(byteLength))
	}
	arrayBufferCtor.Set("prototype", NewObject(ArrayBufferPrototype))

	vm.globals.M["ArrayBuffer"] = NewObject(arrayBufferCtor)
}

func newArrayBuffer(byteLength int) *JSObject {
	ab := NewJSObject()
	ab.ConstructorName = "ArrayBuffer"
	ab.Prototype = ArrayBufferPrototype
	ab.ensureTypedArray().ByteData = make([]byte, byteLength)
	ab.Set("byteLength", NewNumber(float64(byteLength)))
	return ab
}

// clampToInt returns an integer clamped to [0, maxLen] (or just floor).
func clampToInt(v float64, maxLen int) int {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	n := int(v)
	if n > maxLen {
		n = maxLen
	}
	if n < 0 {
		n = 0
	}
	return n
}

// ---------------------------------------------------------------------------
// DataView
// ---------------------------------------------------------------------------

func (vm *VM) registerDataView() {
	DataViewPrototype = NewJSObject()
	DataViewPrototype.ConstructorName = "DataView"

	DataViewPrototype.Set("getInt8", NewObject(builtinFunc("DataView.getInt8", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 1, false, "int8")
	})))
	DataViewPrototype.Set("getUint8", NewObject(builtinFunc("DataView.getUint8", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 1, false, "uint8")
	})))
	DataViewPrototype.Set("getInt16", NewObject(builtinFunc("DataView.getInt16", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 2, isLittleEndian(args), "int16")
	})))
	DataViewPrototype.Set("getUint16", NewObject(builtinFunc("DataView.getUint16", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 2, isLittleEndian(args), "uint16")
	})))
	DataViewPrototype.Set("getInt32", NewObject(builtinFunc("DataView.getInt32", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 4, isLittleEndian(args), "int32")
	})))
	DataViewPrototype.Set("getUint32", NewObject(builtinFunc("DataView.getUint32", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 4, isLittleEndian(args), "uint32")
	})))
	DataViewPrototype.Set("getFloat32", NewObject(builtinFunc("DataView.getFloat32", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 4, isLittleEndian(args), "float32")
	})))
	DataViewPrototype.Set("getFloat64", NewObject(builtinFunc("DataView.getFloat64", func(this *JSObject, args []JSValue) JSValue {
		return dvGet(this, args, 8, isLittleEndian(args), "float64")
	})))

	DataViewPrototype.Set("setInt8", NewObject(builtinFunc("DataView.setInt8", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 1, false, "int8")
	})))
	DataViewPrototype.Set("setUint8", NewObject(builtinFunc("DataView.setUint8", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 1, false, "uint8")
	})))
	DataViewPrototype.Set("setInt16", NewObject(builtinFunc("DataView.setInt16", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 2, isLittleEndian(args), "int16")
	})))
	DataViewPrototype.Set("setUint16", NewObject(builtinFunc("DataView.setUint16", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 2, isLittleEndian(args), "uint16")
	})))
	DataViewPrototype.Set("setInt32", NewObject(builtinFunc("DataView.setInt32", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 4, isLittleEndian(args), "int32")
	})))
	DataViewPrototype.Set("setUint32", NewObject(builtinFunc("DataView.setUint32", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 4, isLittleEndian(args), "uint32")
	})))
	DataViewPrototype.Set("setFloat32", NewObject(builtinFunc("DataView.setFloat32", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 4, isLittleEndian(args), "float32")
	})))
	DataViewPrototype.Set("setFloat64", NewObject(builtinFunc("DataView.setFloat64", func(this *JSObject, args []JSValue) JSValue {
		return dvSet(this, args, 8, isLittleEndian(args), "float64")
	})))

	// BigInt64 methods.
	DataViewPrototype.Set("getBigInt64", NewObject(builtinFunc("DataView.getBigInt64", func(this *JSObject, args []JSValue) JSValue {
		return dvGetBigInt(this, args, 8, isLittleEndian(args), true)
	})))
	DataViewPrototype.Set("getBigUint64", NewObject(builtinFunc("DataView.getBigUint64", func(this *JSObject, args []JSValue) JSValue {
		return dvGetBigInt(this, args, 8, isLittleEndian(args), false)
	})))
	DataViewPrototype.Set("setBigInt64", NewObject(builtinFunc("DataView.setBigInt64", func(this *JSObject, args []JSValue) JSValue {
		return dvSetBigInt(this, args, 8, isLittleEndian(args), true)
	})))
	DataViewPrototype.Set("setBigUint64", NewObject(builtinFunc("DataView.setBigUint64", func(this *JSObject, args []JSValue) JSValue {
		return dvSetBigInt(this, args, 8, isLittleEndian(args), false)
	})))

	dataViewCtor := NewJSObject()
	dataViewCtor.ConstructorName = "Function"
	dataViewCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return NewObject(newDataView(nil, 0, 0))
		}
		buffer := args[0].ObjVal
		if buffer.typedArray == nil || buffer.typedArray.ByteData == nil {
			return NewObject(newDataView(nil, 0, 0))
		}
		byteOffset := 0
		if len(args) > 1 {
			byteOffset = int(args[1].ToNumber())
		}
		if byteOffset < 0 || byteOffset > len(buffer.typedArray.ByteData) {
			byteOffset = 0
		}
		byteLength := len(buffer.typedArray.ByteData) - byteOffset
		if len(args) > 2 && !args[2].IsUndefined() {
			byteLength = int(args[2].ToNumber())
		}
		if byteOffset+byteLength > len(buffer.typedArray.ByteData) {
			byteLength = len(buffer.typedArray.ByteData) - byteOffset
		}
		if byteLength < 0 {
			byteLength = 0
		}
		return NewObject(newDataView(buffer.typedArray.ByteData, byteOffset, byteLength))
	}
	dataViewCtor.Set("prototype", NewObject(DataViewPrototype))

	vm.globals.M["DataView"] = NewObject(dataViewCtor)
}

func newDataView(bytes []byte, byteOffset, byteLength int) *JSObject {
	dv := NewJSObject()
	dv.ConstructorName = "DataView"
	dv.Prototype = DataViewPrototype
	if bytes != nil {
		dv.ensureTypedArray().ByteData = bytes[byteOffset : byteOffset+byteLength]
	} else {
		dv.ensureTypedArray().ByteData = nil
	}
	dv.Set("byteOffset", NewNumber(float64(byteOffset)))
	dv.Set("byteLength", NewNumber(float64(byteLength)))
	return dv
}

// isLittleEndian extracts the endianness flag from the last argument.
// getters have args [byteOffset, littleEndian?]
// setters have args [byteOffset, value, littleEndian?]
func isLittleEndian(args []JSValue) bool {
	n := len(args)
	if n > 0 && args[n-1].IsBoolean() {
		return args[n-1].BoolVal
	}
	return false
}

func dvGet(dv *JSObject, args []JSValue, size int, littleEndian bool, kind string) JSValue {
	if len(args) == 0 {
		return Undefined
	}
	offset := int(args[0].ToNumber())
	bd := dv.typedArray.ByteData
	if dv.typedArray == nil || bd == nil || offset < 0 || offset+size > len(bd) {
		return Undefined
	}
	b := bd[offset : offset+size]
	switch kind {
	case "int8":
		return NewNumber(float64(int8(b[0])))
	case "uint8":
		return NewNumber(float64(b[0]))
	case "int16":
		var v int16
		if littleEndian {
			v = int16(binary.LittleEndian.Uint16(b))
		} else {
			v = int16(binary.BigEndian.Uint16(b))
		}
		return NewNumber(float64(v))
	case "uint16":
		var v uint16
		if littleEndian {
			v = binary.LittleEndian.Uint16(b)
		} else {
			v = binary.BigEndian.Uint16(b)
		}
		return NewNumber(float64(v))
	case "int32":
		var v int32
		if littleEndian {
			v = int32(binary.LittleEndian.Uint32(b))
		} else {
			v = int32(binary.BigEndian.Uint32(b))
		}
		return NewNumber(float64(v))
	case "uint32":
		var v uint32
		if littleEndian {
			v = binary.LittleEndian.Uint32(b)
		} else {
			v = binary.BigEndian.Uint32(b)
		}
		return NewNumber(float64(v))
	case "float32":
		var bits uint32
		if littleEndian {
			bits = binary.LittleEndian.Uint32(b)
		} else {
			bits = binary.BigEndian.Uint32(b)
		}
		return NewNumber(float64(math.Float32frombits(bits)))
	case "float64":
		var bits uint64
		if littleEndian {
			bits = binary.LittleEndian.Uint64(b)
		} else {
			bits = binary.BigEndian.Uint64(b)
		}
		return NewNumber(math.Float64frombits(bits))
	}
	return Undefined
}

func dvSet(dv *JSObject, args []JSValue, size int, littleEndian bool, kind string) JSValue {
	if len(args) < 2 {
		return Undefined
	}
	offset := int(args[0].ToNumber())
	value := args[1].ToNumber()
	bd := dv.typedArray.ByteData
	if dv.typedArray == nil || bd == nil || offset < 0 || offset+size > len(bd) {
		return Undefined
	}
	b := bd[offset : offset+size]
	switch kind {
	case "int8":
		b[0] = byte(int8(value))
	case "uint8":
		b[0] = byte(value)
	case "int16":
		if littleEndian {
			binary.LittleEndian.PutUint16(b, uint16(int16(value)))
		} else {
			binary.BigEndian.PutUint16(b, uint16(int16(value)))
		}
	case "uint16":
		if littleEndian {
			binary.LittleEndian.PutUint16(b, uint16(value))
		} else {
			binary.BigEndian.PutUint16(b, uint16(value))
		}
	case "int32":
		if littleEndian {
			binary.LittleEndian.PutUint32(b, uint32(int32(value)))
		} else {
			binary.BigEndian.PutUint32(b, uint32(int32(value)))
		}
	case "uint32":
		if littleEndian {
			binary.LittleEndian.PutUint32(b, uint32(value))
		} else {
			binary.BigEndian.PutUint32(b, uint32(value))
		}
	case "float32":
		bits := math.Float32bits(float32(value))
		if littleEndian {
			binary.LittleEndian.PutUint32(b, bits)
		} else {
			binary.BigEndian.PutUint32(b, bits)
		}
	case "float64":
		bits := math.Float64bits(value)
		if littleEndian {
			binary.LittleEndian.PutUint64(b, bits)
		} else {
			binary.BigEndian.PutUint64(b, bits)
		}
	}
	return Undefined
}

// dvGetBigInt reads an 8-byte BigInt (signed or unsigned) from the DataView buffer.
func dvGetBigInt(dv *JSObject, args []JSValue, size int, littleEndian bool, signed bool) JSValue {
	if len(args) == 0 {
		return Undefined
	}
	offset := int(args[0].ToNumber())
	bd := dv.typedArray.ByteData
	if dv.typedArray == nil || bd == nil || offset < 0 || offset+size > len(bd) {
		return Undefined
	}
	b := bd[offset : offset+size]
	var val uint64
	if littleEndian {
		val = binary.LittleEndian.Uint64(b)
	} else {
		val = binary.BigEndian.Uint64(b)
	}
	if signed {
		return NewBigIntFromInt64(int64(val))
	}
	return NewBigIntFromUint64(val)
}

// dvSetBigInt writes an 8-byte BigInt (signed or unsigned) to the DataView buffer.
func dvSetBigInt(dv *JSObject, args []JSValue, size int, littleEndian bool, signed bool) JSValue {
	if len(args) < 2 {
		return Undefined
	}
	offset := int(args[0].ToNumber())
	bd := dv.typedArray.ByteData
	if dv.typedArray == nil || bd == nil || offset < 0 || offset+size > len(bd) {
		return Undefined
	}
	b := bd[offset : offset+size]

	// Extract uint64 bits from the BigInt argument.
	var bits uint64
	arg := args[1]
	switch {
	case arg.Tag == TagBigInt && arg.BigIntVal != nil:
		if signed {
			bits = uint64(arg.BigIntVal.Int64())
		} else {
			bits = arg.BigIntVal.Uint64()
		}
	case arg.Tag == TagNumber:
		bits = uint64(int64(arg.NumVal))
	case arg.Tag == TagString:
		if bi, ok := new(big.Int).SetString(arg.StrVal, 0); ok {
			if signed {
				bits = uint64(bi.Int64())
			} else {
				bits = bi.Uint64()
			}
		}
	}

	if littleEndian {
		binary.LittleEndian.PutUint64(b, bits)
	} else {
		binary.BigEndian.PutUint64(b, bits)
	}
	return Undefined
}

// ---------------------------------------------------------------------------
// TypedArrays
// ---------------------------------------------------------------------------

type typedArrayKind int

const (
	taInt8 typedArrayKind = iota
	taUint8
	taUint8Clamped
	taInt16
	taUint16
	taInt32
	taUint32
	taFloat32
	taFloat64
)

func (vm *VM) registerTypedArrays() {
	types := []struct {
		name string
		kind typedArrayKind
	}{
		{"Int8Array", taInt8},
		{"Uint8Array", taUint8},
		{"Uint8ClampedArray", taUint8Clamped},
		{"Int16Array", taInt16},
		{"Uint16Array", taUint16},
		{"Int32Array", taInt32},
		{"Uint32Array", taUint32},
		{"Float32Array", taFloat32},
		{"Float64Array", taFloat64},
	}

	for _, t := range types {
		vm.globals.M[t.name] = NewObject(makeTypedArrayCtor(t.name, t.kind))
	}
}

func makeTypedArrayCtor(name string, kind typedArrayKind) *JSObject {
	byteSize := typedArrayByteSize(kind)

	proto := NewJSObject()
	proto.ConstructorName = name
	proto.Set("length", NewNumber(0))

	ctor := NewJSObject()
	ctor.ConstructorName = "Function"
	ctor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		ta := NewJSObject()
		ta.ConstructorName = name
		ta.Prototype = proto
		taTyped := ta.ensureTypedArray()

		if len(args) == 0 {
			taTyped.ByteData = make([]byte, 0)
		} else if args[0].IsNumber() {
			// new TypedArray(length)
			length := int(args[0].ToNumber())
			if length < 0 {
				length = 0
			}
			taTyped.ByteData = make([]byte, length*byteSize)
		} else if args[0].IsObject() && args[0].ObjVal != nil && args[0].ObjVal.typedArray != nil && args[0].ObjVal.typedArray.ByteData != nil {
			// new TypedArray(buffer, byteOffset?, length?)
			buffer := args[0].ObjVal
			srcData := buffer.typedArray.ByteData
			byteOffset := 0
			length := len(srcData) / byteSize
			if len(args) > 1 {
				byteOffset = int(args[1].ToNumber())
			}
			if len(args) > 2 && !args[2].IsUndefined() {
				length = int(args[2].ToNumber())
			}
			if byteOffset < 0 || byteOffset > len(srcData) {
				byteOffset = 0
			}
			elemBytes := length * byteSize
			if byteOffset+elemBytes > len(srcData) {
				elemBytes = len(srcData) - byteOffset
				length = elemBytes / byteSize
			}
			if elemBytes < 0 {
				elemBytes = 0
				length = 0
			}
			taTyped.ByteData = make([]byte, elemBytes)
			copy(taTyped.ByteData, srcData[byteOffset:byteOffset+elemBytes])
		} else if args[0].IsObject() && args[0].ObjVal != nil {
			// new TypedArray(typedArray) — copy from another typed array
			src := args[0].ObjVal
			if src.typedArray != nil && src.typedArray.ByteData != nil {
				taTyped.ByteData = make([]byte, len(src.typedArray.ByteData))
				copy(taTyped.ByteData, src.typedArray.ByteData)
			} else {
				taTyped.ByteData = make([]byte, 0)
			}
		} else {
			taTyped.ByteData = make([]byte, 0)
		}

		n := len(taTyped.ByteData) / byteSize
		ta.Set("length", NewNumber(float64(n)))

		// Virtual property interceptor for indexed access.
		taIntercept := ta.ensureInterceptor()
		taIntercept.OnPropertyGet = func(obj *JSObject, prop string) (JSValue, bool) {
			idx, ok := parseArrayIndex(prop)
			if !ok {
				return Undefined, false
			}
			bd := obj.typedArray.ByteData
			if bd == nil || idx*byteSize+byteSize > len(bd) {
				return Undefined, true
			}
			return typedArrayGet(bd, idx, byteSize, kind), true
		}
		taIntercept.OnPropertySet = func(obj *JSObject, prop string, val JSValue) bool {
			idx, ok := parseArrayIndex(prop)
			if !ok {
				return false
			}
			bd := obj.typedArray.ByteData
			if bd == nil || idx*byteSize+byteSize > len(bd) {
				return true
			}
			typedArraySet(bd, idx, byteSize, kind, val.ToNumber())
			return true
		}

		bufObj := NewJSObject()
		bufObj.ConstructorName = "ArrayBuffer"
		bufObj.ensureTypedArray().ByteData = taTyped.ByteData
		ta.Set("buffer", NewObject(bufObj))
		ta.Set("byteOffset", NewNumber(0))
		ta.Set("byteLength", NewNumber(float64(len(taTyped.ByteData))))

		return NewObject(ta)
	}

	ctor.Set("prototype", NewObject(proto))
	return ctor
}

func typedArrayByteSize(kind typedArrayKind) int {
	switch kind {
	case taInt8, taUint8, taUint8Clamped:
		return 1
	case taInt16, taUint16:
		return 2
	case taInt32, taUint32, taFloat32:
		return 4
	case taFloat64:
		return 8
	}
	return 1
}

func typedArrayGet(bytes []byte, idx int, byteSize int, kind typedArrayKind) JSValue {
	offset := idx * byteSize
	if offset+byteSize > len(bytes) {
		return Undefined
	}
	b := bytes[offset : offset+byteSize]
	switch kind {
	case taInt8:
		return NewNumber(float64(int8(b[0])))
	case taUint8, taUint8Clamped:
		return NewNumber(float64(b[0]))
	case taInt16:
		return NewNumber(float64(int16(binary.LittleEndian.Uint16(b))))
	case taUint16:
		return NewNumber(float64(binary.LittleEndian.Uint16(b)))
	case taInt32:
		return NewNumber(float64(int32(binary.LittleEndian.Uint32(b))))
	case taUint32:
		return NewNumber(float64(binary.LittleEndian.Uint32(b)))
	case taFloat32:
		bits := binary.LittleEndian.Uint32(b)
		return NewNumber(float64(math.Float32frombits(bits)))
	case taFloat64:
		bits := binary.LittleEndian.Uint64(b)
		return NewNumber(math.Float64frombits(bits))
	}
	return Undefined
}

func typedArraySet(bytes []byte, idx int, byteSize int, kind typedArrayKind, value float64) {
	offset := idx * byteSize
	if offset+byteSize > len(bytes) {
		return
	}
	b := bytes[offset : offset+byteSize]
	switch kind {
	case taInt8:
		b[0] = byte(int8(value))
	case taUint8:
		b[0] = byte(value)
	case taUint8Clamped:
		if value < 0 {
			b[0] = 0
		} else if value > 255 {
			b[0] = 255
		} else {
			// Round to nearest even.
			v := value
			rounded := math.Floor(v + 0.5)
			if v-math.Floor(v) == 0.5 && int(rounded)%2 != 0 {
				rounded = rounded - 1
			}
			b[0] = byte(rounded)
		}
	case taInt16:
		binary.LittleEndian.PutUint16(b, uint16(int16(value)))
	case taUint16:
		binary.LittleEndian.PutUint16(b, uint16(value))
	case taInt32:
		binary.LittleEndian.PutUint32(b, uint32(int32(value)))
	case taUint32:
		binary.LittleEndian.PutUint32(b, uint32(value))
	case taFloat32:
		binary.LittleEndian.PutUint32(b, math.Float32bits(float32(value)))
	case taFloat64:
		binary.LittleEndian.PutUint64(b, math.Float64bits(value))
	}
}

func builtinFunc(name string, fn func(this *JSObject, args []JSValue) JSValue) *JSObject {
	obj := NewJSObject()
	obj.ConstructorName = "Function"
	obj.CallFunc = fn
	return obj
}

// registerBigInt registers the BigInt() constructor.
// BigInt(value) — converts a value to BigInt (number, string, or boolean).
func (vm *VM) registerBigInt() {
	bigIntCtor := NewJSObject()
	bigIntCtor.ConstructorName = "Function"
	bigIntCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewBigIntFromInt64(0)
		}
		switch args[0].Tag {
		case TagNumber:
			return NewBigIntFromInt64(int64(args[0].NumVal))
		case TagString:
			return NewBigIntFromString(args[0].StrVal)
		case TagBoolean:
			if args[0].BoolVal {
				return NewBigIntFromInt64(1)
			}
			return NewBigIntFromInt64(0)
		case TagBigInt:
			return args[0]
		default:
			return NewBigIntFromInt64(0)
		}
	}

	// BigInt.asIntN(bits, bigint)
	bigIntCtor.Set("asIntN", vm.createBuiltinFunction("BigInt.asIntN", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 || !args[0].IsNumber() || !args[1].IsBigInt() {
			return NewBigIntFromInt64(0)
		}
		bits := int(args[0].NumVal)
		if bits < 0 {
			return NewString("RangeError: BigInt.asIntN requires a non-negative number of bits")
		}
		if bits == 0 {
			return NewBigIntFromInt64(0)
		}
		val := new(big.Int).Set(args[1].BigIntVal)
		// Compute 2^bits
		modulus := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		half := new(big.Int).Rsh(modulus, 1)
		// Truncate to bits
		val.Mod(val, modulus)
		// Sign-extend
		if val.Cmp(half) >= 0 {
			val.Sub(val, modulus)
		}
		return JSValue{Tag: TagBigInt, BigIntVal: val}
	}))

	// BigInt.asUintN(bits, bigint)
	bigIntCtor.Set("asUintN", vm.createBuiltinFunction("BigInt.asUintN", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 || !args[0].IsNumber() || !args[1].IsBigInt() {
			return NewBigIntFromInt64(0)
		}
		bits := int(args[0].NumVal)
		if bits < 0 {
			return NewString("RangeError: BigInt.asUintN requires a non-negative number of bits")
		}
		if bits == 0 {
			return NewBigIntFromInt64(0)
		}
		val := new(big.Int).Set(args[1].BigIntVal)
		modulus := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		val.Mod(val, modulus)
		return JSValue{Tag: TagBigInt, BigIntVal: val}
	}))

	vm.globals.M["BigInt"] = NewObject(bigIntCtor)
}

// registerDate registers the Date constructor, static methods, and prototype.

// registerModules registers builtins for ES module support.
func (vm *VM) registerModules() {
	// __import__(url) — dynamic import, returns Promise<module namespace>
	vm.registry.Builtins["__import__"] = func(args []JSValue) JSValue {
		if len(args) < 1 {
			return vm.NewPromise(func(resolve, reject func(JSValue)) {
				reject(NewString("TypeError: import() requires a module specifier"))
			})
		}
		url := args[0].ToString()
		return vm.NewPromise(func(resolve, reject func(JSValue)) {
			mr := vm.moduleRegistry
			if mr == nil {
				reject(NewString("Error: no module loader configured"))
				return
			}
			val, err := mr.Import(url)
			if err != nil {
				reject(NewString(err.Error()))
				return
			}
			resolve(val)
		})
	}

	// __moduleImportNamespace__(url) — synchronous import of namespace (for static import * as)
	vm.registry.Builtins["__moduleImportNamespace__"] = func(args []JSValue) JSValue {
		if len(args) < 1 {
			return Undefined
		}
		mr := vm.moduleRegistry
		if mr == nil {
			return Undefined
		}
		url := args[0].ToString()
		val, _ := mr.Import(url)
		return val
	}

	// __moduleImportDefault__(url) — synchronous import of default export
	vm.registry.Builtins["__moduleImportDefault__"] = func(args []JSValue) JSValue {
		if len(args) < 1 {
			return Undefined
		}
		mr := vm.moduleRegistry
		if mr == nil {
			return Undefined
		}
		url := args[0].ToString()
		val, _ := mr.Import(url)
		if val.IsObject() && val.ObjVal != nil {
			defVal := val.ObjVal.Get("default")
			return defVal
		}
		return Undefined
	}

	// __moduleImportNamed__(url, name) — synchronous import of named export
	vm.registry.Builtins["__moduleImportNamed__"] = func(args []JSValue) JSValue {
		if len(args) < 2 {
			return Undefined
		}
		mr := vm.moduleRegistry
		if mr == nil {
			return Undefined
		}
		url := args[0].ToString()
		name := args[1].ToString()
		val, _ := mr.Import(url)
		if val.IsObject() && val.ObjVal != nil {
			return val.ObjVal.Get(name)
		}
		return Undefined
	}
}

// registerProxy registers the Proxy constructor.
// new Proxy(target, handler) creates a proxy that intercepts operations on target
// via handler traps: get, set, has, deleteProperty, apply, construct.
func (vm *VM) registerProxy() {
	proxyCtor := NewJSObject()
	proxyCtor.ConstructorName = "Function"
	proxyCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return Undefined
		}
		target := args[0]
		if !target.IsObject() || target.ObjVal == nil {
			return Undefined
		}
		handler := args[1]
		if !handler.IsObject() || handler.ObjVal == nil {
			return Undefined
		}
		handlerObj := handler.ObjVal
		proxy := this
		proxy.ensureProxy().Target = target
		proxy.ensureProxy().Handler = handlerObj
		proxy.ConstructorName = "Proxy"

		// Property get trap: handler.get(target, prop, receiver)
		proxy.ensureInterceptor().OnPropertyGet = func(obj *JSObject, name string) (JSValue, bool) {
			getTrap := handlerObj.Get("get")
			if getTrap.IsObject() && getTrap.ObjVal != nil && getTrap.ObjVal.IsCallable() {
				receiver := NewObject(proxy)
				result := getTrap.ObjVal.Call(handlerObj, []JSValue{target, NewString(name), receiver})
				return result, true
			}
			// Fallback: read from target directly.
			if target.ObjVal != nil {
				return target.ObjVal.Get(name), true
			}
			return Undefined, true
		}

		// Property set trap: handler.set(target, prop, value, receiver) → boolean
		proxy.ensureInterceptor().OnPropertySet = func(obj *JSObject, name string, value JSValue) bool {
			setTrap := handlerObj.Get("set")
			if setTrap.IsObject() && setTrap.ObjVal != nil && setTrap.ObjVal.IsCallable() {
				receiver := NewObject(proxy)
				result := setTrap.ObjVal.Call(handlerObj, []JSValue{target, NewString(name), value, receiver})
				return result.IsTruthy()
			}
			// Fallback: set on target directly.
			if target.ObjVal != nil {
				target.ObjVal.Set(name, value)
			}
			return true
		}

		// has trap: handler.has(target, prop) → boolean
		proxy.ensureInterceptor().OnHas = func(obj *JSObject, name string) (bool, bool) {
			hasTrap := handlerObj.Get("has")
			if hasTrap.IsObject() && hasTrap.ObjVal != nil && hasTrap.ObjVal.IsCallable() {
				result := hasTrap.ObjVal.Call(handlerObj, []JSValue{target, NewString(name)})
				return true, result.IsTruthy()
			}
			return false, false // not handled, fall through to normal behavior
		}

		// deleteProperty trap: handler.deleteProperty(target, prop) → boolean
		proxy.ensureInterceptor().OnDelete = func(obj *JSObject, name string) (bool, bool) {
			deleteTrap := handlerObj.Get("deleteProperty")
			if deleteTrap.IsObject() && deleteTrap.ObjVal != nil && deleteTrap.ObjVal.IsCallable() {
				result := deleteTrap.ObjVal.Call(handlerObj, []JSValue{target, NewString(name)})
				return true, result.IsTruthy()
			}
			// Fallback: delete from target directly.
			if target.ObjVal != nil {
				return true, target.ObjVal.Delete(name)
			}
			return true, false
		}

		// apply trap: handler.apply(target, thisArg, args) — set on CallFunc
		proxy.CallFunc = func(thisArg *JSObject, callArgs []JSValue) JSValue {
			applyTrap := handlerObj.Get("apply")
			if applyTrap.IsObject() && applyTrap.ObjVal != nil && applyTrap.ObjVal.IsCallable() {
				argsArr := NewJSObject()
				argsArr.ConstructorName = "Array"
				if ArrayPrototype != nil {
					argsArr.Prototype = ArrayPrototype
				}
				for i, a := range callArgs {
					argsArr.Set(intKey(i), a)
				}
				argsArr.Set("length", NewNumber(float64(len(callArgs))))
				var thisVal JSValue
				if thisArg != nil {
					thisVal = NewObject(thisArg)
				} else {
					thisVal = Undefined
				}
				return applyTrap.ObjVal.Call(handlerObj, []JSValue{target, thisVal, NewObject(argsArr)})
			}
			// Fallback: call target directly.
			if target.ObjVal != nil && target.ObjVal.IsCallable() {
				return target.ObjVal.Call(thisArg, callArgs)
			}
			return Undefined
		}

		return NewObject(proxy)
	}

	// Proxy.revocable(target, handler) — returns {proxy, revoke: function}
	proxyCtor.Set("revocable", vm.createBuiltinFunction("Proxy.revocable", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return Undefined
		}
		target := args[0]
		handler := args[1]
		proxyVal := proxyCtor.CallFunc(NewJSObject(), []JSValue{target, handler})
		revoked := false
		result := NewJSObject()
		result.Set("proxy", proxyVal)
		result.Set("revoke", vm.createBuiltinFunction("revoke", func(this *JSObject, args []JSValue) JSValue {
			if revoked {
				return Undefined
			}
			revoked = true
			if proxyVal.IsObject() && proxyVal.ObjVal != nil {
				p := proxyVal.ObjVal
				if p.interceptor != nil {
					p.interceptor.OnPropertyGet = nil
					p.interceptor.OnPropertySet = nil
					p.interceptor.OnHas = nil
					p.interceptor.OnDelete = nil
				}
				p.CallFunc = nil
				if p.proxy != nil {
					p.proxy.Target = JSValue{}
					p.proxy.Handler = nil
				}
			}
			return Undefined
		}))
		return NewObject(result)
	}))

	vm.globals.M["Proxy"] = NewObject(proxyCtor)
}

// registerReflect registers the Reflect built-in object with static methods
// for default object operations: get, set, has, deleteProperty, ownKeys, apply, construct.
func (vm *VM) registerReflect() {
	reflectObj := NewJSObject()
	reflectObj.ConstructorName = "Reflect"

	// Reflect.get(target, prop[, receiver])
	reflectObj.Set("get", vm.createBuiltinFunction("Reflect.get", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return Undefined
		}
		target := args[0]
		if !target.IsObject() || target.ObjVal == nil {
			return Undefined
		}
		prop := args[1].ToString()
		return target.ObjVal.Get(prop)
	}))

	// Reflect.set(target, prop, value[, receiver])
	reflectObj.Set("set", vm.createBuiltinFunction("Reflect.set", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 3 {
			return False
		}
		target := args[0]
		if !target.IsObject() || target.ObjVal == nil {
			return False
		}
		prop := args[1].ToString()
		value := args[2]
		target.ObjVal.Set(prop, value)
		return True
	}))

	// Reflect.has(target, prop)
	reflectObj.Set("has", vm.createBuiltinFunction("Reflect.has", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return False
		}
		target := args[0]
		if !target.IsObject() || target.ObjVal == nil {
			return False
		}
		prop := args[1].ToString()
		return NewBoolean(target.ObjVal.Has(prop))
	}))

	// Reflect.deleteProperty(target, prop)
	reflectObj.Set("deleteProperty", vm.createBuiltinFunction("Reflect.deleteProperty", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return False
		}
		target := args[0]
		if !target.IsObject() || target.ObjVal == nil {
			return False
		}
		prop := args[1].ToString()
		return NewBoolean(target.ObjVal.Delete(prop))
	}))

	// Reflect.ownKeys(target) → array of own property keys
	reflectObj.Set("ownKeys", vm.createBuiltinFunction("Reflect.ownKeys", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil {
			return NewObject(NewJSObject())
		}
		obj := args[0].ObjVal
		keys := NewJSObject()
		keys.ConstructorName = "Array"
		if ArrayPrototype != nil {
			keys.Prototype = ArrayPrototype
		}
		idx := 0
		for name := range obj.Shape.Properties {
			keys.Set(intKey(idx), NewString(name))
			idx++
		}
		keys.Set("length", NewNumber(float64(idx)))
		return NewObject(keys)
	}))

	// Reflect.apply(fn, thisArg, argsArray)
	reflectObj.Set("apply", vm.createBuiltinFunction("Reflect.apply", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 3 {
			return Undefined
		}
		fn := args[0]
		if !fn.IsObject() || fn.ObjVal == nil || !fn.ObjVal.IsCallable() {
			return Undefined
		}
		var thisObj *JSObject
		if args[1].IsObject() {
			thisObj = args[1].ObjVal
		}
		argsArray := args[2]
		callArgs := []JSValue{}
		if argsArray.IsObject() && argsArray.ObjVal != nil {
			length := int(argsArray.ObjVal.Get("length").ToNumber())
			for i := 0; i < length; i++ {
				callArgs = append(callArgs, argsArray.ObjVal.Get(intKey(i)))
			}
		}
		return fn.ObjVal.Call(thisObj, callArgs)
	}))

	// Reflect.construct(target, argsArray[, newTarget])
	reflectObj.Set("construct", vm.createBuiltinFunction("Reflect.construct", func(this *JSObject, args []JSValue) JSValue {
		if len(args) < 2 {
			return Undefined
		}
		target := args[0]
		if !target.IsObject() || target.ObjVal == nil {
			return Undefined
		}
		argsArray := args[1]
		callArgs := []JSValue{}
		if argsArray.IsObject() && argsArray.ObjVal != nil {
			length := int(argsArray.ObjVal.Get("length").ToNumber())
			for i := 0; i < length; i++ {
				callArgs = append(callArgs, argsArray.ObjVal.Get(intKey(i)))
			}
		}
		// Create a new object with the target's prototype.
		newObj := NewJSObject()
		if protoVal := target.ObjVal.Get("prototype"); protoVal.IsObject() && protoVal.ObjVal != nil {
			newObj.Prototype = protoVal.ObjVal
		}
		// Call the target's CallFunc with newObj as this.
		if target.ObjVal.CallFunc != nil {
			target.ObjVal.CallFunc(newObj, callArgs)
		}
		return NewObject(newObj)
	}))

	vm.globals.M["Reflect"] = NewObject(reflectObj)
}

func (vm *VM) registerEval() {
	vm.globals.M["eval"] = vm.createBuiltinFunction("eval", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Undefined
		}
		// Per ECMAScript §18.2.1.1: if x is not a String, return x unchanged.
		if args[0].Tag != TagString {
			return args[0]
		}
		source := args[0].ToString()
		if source == "" {
			return Undefined
		}
		tokens := NewLexer(source).Tokenize()
		prog, errs := NewParser(tokens).Parse()
		if len(errs) > 0 {
			vm.console.Log("eval parse error: " + strings.Join(errs, "; "))
			return Undefined
		}
		bf := Compile(prog)
		if bf == nil {
			return Undefined
		}
		return vm.execute(bf)
	})
}

func (vm *VM) registerWeakRef() {
	weakRefCtor := NewJSObject()
	weakRefCtor.ConstructorName = "Function"
	weakRefCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		wr := NewJSObject()
		wr.ConstructorName = "WeakRef"
		var target JSValue
		if len(args) > 0 {
			target = args[0]
		} else {
			target = Undefined
		}
		// Store target in the weak reference table (uses unsafe.Pointer
		// for objects so Go GC can collect them). Primitive values are
		// stored directly as they are value types, not heap objects.
		id := storeWeakRef(target)
		registerWeakRefCleanup(target, id)
		// Store the weakRef ID on the object so deref() can look it up.
		wr.Set("__weakref_id__", NewNumber(float64(id)))
		wr.Set("deref", vm.createBuiltinFunction("WeakRef.deref", func(thisWR *JSObject, _ []JSValue) JSValue {
			idVal := thisWR.Get("__weakref_id__")
			if idVal.Tag != TagNumber {
				return Undefined
			}
			id := uint64(idVal.NumVal)
			return derefWeakRef(id)
		}))
		return NewObject(wr)
	}
	vm.globals.M["WeakRef"] = NewObject(weakRefCtor)
}

func (vm *VM) registerFinalizationRegistry() {
	frCtor := NewJSObject()
	frCtor.ConstructorName = "Function"
	// NOTE: FinalizationRegistry callbacks capture the VM pointer through
	// CallFunc closures. VMs with outstanding registered targets will not be
	// GC'd until all targets are collected. To avoid VM leaks, call
	// finalizationRegistry.unregister() for all targets before discarding
	// the VM. This is an inherent limitation of the Go runtime — there is no
	// mechanism to weak-reference the VM from a GC cleanup callback.
	frCtor.CallFunc = func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 || !args[0].IsObject() || args[0].ObjVal == nil || !args[0].ObjVal.isCallable() {
			return Undefined
		}
		fr := NewJSObject()
		fr.ConstructorName = "FinalizationRegistry"
		fr.Set("__cleanup_callback__", args[0])

		fr.Set("register", vm.createBuiltinFunction("FinalizationRegistry.register", func(thisFR *JSObject, regArgs []JSValue) JSValue {
			// register(target, heldValue [, unregisterToken])
			if len(regArgs) < 2 {
				return Undefined
			}
			target := regArgs[0]
			heldValue := regArgs[1]
			var token JSValue = Undefined
			if len(regArgs) >= 3 {
				token = regArgs[2]
			}
			callback := thisFR.Get("__cleanup_callback__")
			entry := finalizationEntry{
				callback:  callback,
				heldValue: heldValue,
				token:     token,
			}
			isNew := addFinalizationCleanup(target, []finalizationEntry{entry})
			if isNew && target.IsObject() && target.ObjVal != nil {
				key := uintptr(unsafe.Pointer(target.ObjVal))
				runtime.AddCleanup(target.ObjVal, invokeCleanupCallbacks, key)
			}
			return Undefined
		}))

		fr.Set("unregister", vm.createBuiltinFunction("FinalizationRegistry.unregister", func(thisFR *JSObject, unregArgs []JSValue) JSValue {
			if len(unregArgs) == 0 {
				return False
			}
			token := unregArgs[0]
			// Search all targets for matching token entries and remove them.
			// Note: this is O(n) in registered entries, matching spec behavior.
			finalizationMu.Lock()
			removed := false
			for key, entries := range finalizationTable {
				newEntries := make([]finalizationEntry, 0, len(entries))
				for _, e := range entries {
					if sameJSValue(e.token, token) {
						removed = true
					} else {
						newEntries = append(newEntries, e)
					}
				}
				if len(newEntries) == 0 {
					delete(finalizationTable, key)
				} else {
					finalizationTable[key] = newEntries
				}
			}
			finalizationMu.Unlock()
			return NewBoolean(removed)
		}))

		return NewObject(fr)
	}
	vm.globals.M["FinalizationRegistry"] = NewObject(frCtor)
}
