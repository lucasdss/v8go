// gov8engine.go — V8Go engine wrapper implementing the same interface as
// the QuickJS-based Engine, so the browser can swap between them seamlessly.
package js

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lucasdss/v8go/pkg/dom"
)

// Gov8Engine wraps the V8Go custom VM with the same API as the QuickJS Engine,
// enabling the browser to use our custom JavaScript engine.
type Gov8Engine struct {
	mu                sync.Mutex
	vm                *VM
	doc               *dom.Document
	heap              *dom.DOMHeap
	consoleLogFn      func(args ...any)
	consoleWarnFn     func(args ...any)
	consoleErrorFn    func(args ...any)
	documentLookup    func(id string) any
	domChangeCallback    func()                                            // called when JS mutates the DOM
	styleTransitionHook  func(el *dom.Element, prop, oldVal, newVal string) // called when JS sets a style property
	eventLoop            *EventLoop
	timeoutID         int
	timeoutMu         sync.Mutex
	initialized       bool
	closed            bool // true after Close(), prevents lazy reinit
	lastError         error // last error from DispatchInlineEvent or other async operations
}

// NewGov8Engine creates a new V8Go JavaScript engine with browser API bindings.
func NewGov8Engine() *Gov8Engine {
	return &Gov8Engine{}
}

// SetDocument sets the Go DOM document that JS DOM operations will modify.
func (e *Gov8Engine) SetDocument(doc *dom.Document) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.doc = doc
	if e.vm != nil {
		e.bindDOM()
	}
}

// SetConsoleLogger configures console.log/warn/error output callbacks.
func (e *Gov8Engine) SetConsoleLogger(fn, warnFn, errFn func(args ...any)) {
	e.consoleLogFn = fn
	e.consoleWarnFn = warnFn
	e.consoleErrorFn = errFn
}

// SetLocation updates window.location seen by JavaScript.
func (e *Gov8Engine) SetLocation(url string) {
	e.init()
	e.vm.globals.M["__location__"] = NewString(url)
}

// SetDocumentBinder provides document.getElementById lookup.
func (e *Gov8Engine) SetDocumentBinder(title string, lookup func(id string) any) {
	e.init()
	e.documentLookup = lookup
	e.vm.globals.M["__docTitle__"] = NewString(title)
}

// SetDOMChangeCallback registers a callback invoked when JS mutates the DOM.
func (e *Gov8Engine) SetDOMChangeCallback(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.domChangeCallback = fn
	if e.vm != nil {
		e.vm.SetDOMChangeCallback(fn)
	}
}

// SetStyleTransitionHook registers a callback invoked when JS sets a style
// property via element.style.setProperty(). The callback receives the element,
// property name, old value, and new value so the browser can trigger CSS
// transitions through the animation engine.
func (e *Gov8Engine) SetStyleTransitionHook(fn func(el *dom.Element, prop, oldVal, newVal string)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.styleTransitionHook = fn
}

// DispatchInlineEvent finds an element's inline event handler for the given
// eventType (e.g., "click" → onclick attribute), sets up a synthetic event
// object in the VM, and executes the handler code. Returns true if a handler
// was found and executed.
func (e *Gov8Engine) DispatchInlineEvent(elem *dom.Element, eventType string) bool {
	e.mu.Lock()
	if e.vm == nil || elem == nil {
		e.mu.Unlock()
		return false
	}
	handlerCode := elem.GetEventHandler(eventType)
	if handlerCode == "" {
		e.mu.Unlock()
		return false
	}

	// Ensure the document object is available as a global so handler code
	// can use document.getElementById and other DOM APIs.
	if _, hasDoc := e.vm.globals.M["document"]; !hasDoc {
		docObj := NewJSObject()
		docObj.Set("getElementById", NewObject(builtinFunc("getElementById", func(this *JSObject, args []JSValue) JSValue {
			return e.vm.registry.Builtins["__goGetElementById"](args)
		})))
		docObj.Set("createElement", NewObject(builtinFunc("createElement", func(this *JSObject, args []JSValue) JSValue {
			return e.vm.registry.Builtins["__goCreateElement"](args)
		})))
		docObj.Set("querySelector", NewObject(builtinFunc("querySelector", func(this *JSObject, args []JSValue) JSValue {
			return e.vm.registry.Builtins["__goQuerySelector"](args)
		})))
		docObj.Set("querySelectorAll", NewObject(builtinFunc("querySelectorAll", func(this *JSObject, args []JSValue) JSValue {
			return e.vm.registry.Builtins["__goQuerySelectorAll"](args)
		})))
		e.vm.globals.M["document"] = NewObject(docObj)
	}

	// Build synthetic event object.
	eventObj := NewJSObject()
	eventObj.Set("type", NewString(eventType))
	targetID := elem.GetAttribute("id")
	if targetID == "" {
		targetID = elem.LocalName
	}
	targetVal := e.elementToGov8Value(elem)
	if targetVal.ObjVal != nil {
		eventObj.Set("target", targetVal)
	} else {
		eventObj.Set("target", NewString(targetID))
	}
	eventObj.Set("preventDefault", NewObject(builtinFunc("preventDefault", func(this *JSObject, args []JSValue) JSValue {
		return Undefined
	})))

	e.vm.globals.M["__event__"] = NewObject(eventObj)

	// Release lock before running handler code — the handler may trigger
	// notifyDOMChange which also acquires the lock.
	// Snapshot vm pointer to guard against Close() during execution.
	vm := e.vm
	e.mu.Unlock()
	if vm == nil {
		return false
	}
	result := vm.Run(handlerCode)
	// Store last error for diagnostics. Undefined result with console errors
	// indicates a parse/runtime error in the handler code.
	if result.IsUndefined() {
		vm.RLock()
		logs := vm.ConsoleLogs()
		vm.RUnlock()
		if len(logs) > 0 {
			e.lastError = fmt.Errorf("inline event handler error: %s", strings.Join(logs, "; "))
		}
	}

	// Always trigger repaint after inline event handler (DOM may have changed).
	e.notifyDOMChange()

	return true
}

// notifyDOMChange invokes the DOM change callback if set.
func (e *Gov8Engine) notifyDOMChange() {
	e.mu.Lock()
	cb := e.domChangeCallback
	e.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (e *Gov8Engine) init() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	if e.initialized {
		return
	}
	e.vm = NewVM()
	e.vm.SetConsoleOutput(func(s string) {
		if e.consoleLogFn != nil {
			e.consoleLogFn(s)
		}
	})
	e.bindDOM()
	e.initialized = true
}

func (e *Gov8Engine) bindDOM() {
// Mark the document element as JS-reachable for unified heap GC.
if e.doc != nil && e.doc.DocumentElement != nil && e.heap != nil {
e.heap.MarkJSReachable(&e.doc.DocumentElement.Node)
}
	// Wire element lookup so VM.DispatchEvent can resolve target IDs.
	if e.doc != nil {
		e.vm.SetElementLookup(func(id string) *dom.Element {
			return e.doc.GetElementByID(id)
		})
	}
	// Propagate DOM change callback to VM for inline event handler repaints.
	e.vm.SetDOMChangeCallback(e.domChangeCallback)
	// Expose document.getElementById as a global built-in function.
	e.vm.registry.Builtins["__goGetElementById"] = func(args []JSValue) JSValue {
		if len(args) == 0 {
			return Null
		}
		id := args[0].ToString()
		var elem *dom.Element
		if e.documentLookup != nil {
			matched := e.documentLookup(id)
			elem, _ = matched.(*dom.Element)
		}
		if elem == nil && e.doc != nil {
			elem = e.doc.GetElementByID(id)
		}
		if elem == nil {
			return Null
		}
		return e.elementToGov8Value(elem)
	}

	// Expose document.createElement as a global built-in.
	e.vm.registry.Builtins["__goCreateElement"] = func(args []JSValue) JSValue {
		if e.doc == nil || len(args) == 0 {
			return Null
		}
		tagName := args[0].ToString()
		el := e.doc.CreateElement(tagName)
		e.notifyDOMChange()
return e.elementToGov8Value(el)
}

// Expose document.querySelector as a global built-in.
e.vm.registry.Builtins["__goQuerySelector"] = func(args []JSValue) JSValue {
if e.doc == nil || len(args) == 0 {
return Null
}
found := e.doc.QuerySelector(args[0].ToString())
if found == nil {
return Null
}
return e.elementToGov8Value(found)
}

// Expose document.querySelectorAll as a global built-in.
e.vm.registry.Builtins["__goQuerySelectorAll"] = func(args []JSValue) JSValue {
if e.doc == nil || len(args) == 0 {
return NewObject(NewJSObject())
}
results := e.doc.QuerySelectorAll(args[0].ToString())
arr := NewJSObject()
arr.ConstructorName = "Array"
if ArrayPrototype != nil {
arr.Prototype = ArrayPrototype
}
for i, r := range results {
arr.Set(intKey(i), e.elementToGov8Value(r))
}
arr.Set("length", NewNumber(float64(len(results))))
return NewObject(arr)
}

// Expose console functions through the VM's built-in console.
}

// Execute runs a JavaScript source string and returns the result value.
// Returns an error if the engine is closed or the VM encounters a fatal error.
func (e *Gov8Engine) Execute(source string) (JSValue, error) {
	e.init()

	// Guard against Close() nilling e.vm between init and use.
	e.mu.Lock()
	if e.vm == nil {
		e.mu.Unlock()
		return Undefined, fmt.Errorf("engine closed")
	}
	e.mu.Unlock()

	// Wire setTimeout if we have an event loop.
	var setTimeoutDef string
	if e.eventLoop != nil {
		e.vm.registry.Builtins["__goSetTimeout"] = func(args []JSValue) JSValue {
			if len(args) < 2 {
				return NewNumber(-1)
			}
			callback := args[0]
			if !callback.IsObject() || callback.ObjVal == nil {
				return NewNumber(-1)
			}
			ms := int(args[1].ToNumber())
			e.timeoutMu.Lock()
			e.timeoutID++
			id := e.timeoutID
			e.timeoutMu.Unlock()
			cb := callback // capture for closure
			e.eventLoop.SetTimeout(func() {
				// Serialise with any in-progress vm.Run() to avoid
				// concurrent executeFrame data races on vm.globals /
				// vm.builtins / vm.funcRegistry.
				e.mu.Lock()
				vm := e.vm
				e.mu.Unlock()
				if vm == nil {
					return
				}
				vm.Lock()
				vm.CallMethodLocked(cb, nil, nil)
				vm.Unlock()
				// Trigger repaint after timeout callback fires.
				e.notifyDOMChange()
			}, time.Duration(ms)*time.Millisecond)
			return NewNumber(float64(id))
		}
		setTimeoutDef = `var setTimeout = __goSetTimeout; var setInterval = __goSetTimeout;`
	} else {
		setTimeoutDef = `var setTimeout = function(fn, ms) { return 0; }; var setInterval = function(fn, ms) { return 0; };`
	}

	// Wire document.addEventListener and window.onload lifecycle events.
	e.vm.registry.Builtins["__goAddEventListener"] = func(args []JSValue) JSValue {
		if len(args) < 2 {
			return Undefined
		}
		eventType := args[0].ToString()
		callback := args[1]
		if !callback.IsObject() || callback.ObjVal == nil || !callback.ObjVal.isCallable() {
			return Undefined
		}
		e.vm.AddEventListener(eventType, callback)
		return Undefined
	}
	e.vm.registry.Builtins["__goSetOnload"] = func(args []JSValue) JSValue {
		if len(args) == 0 {
			return Undefined
		}
		e.vm.SetOnloadHandler(args[0])
		return Undefined
	}
	e.vm.registry.Builtins["__goGetOnload"] = func(args []JSValue) JSValue {
		return e.vm.GetOnloadHandler()
	}

	// Wrap in try-catch for error isolation.
	wrapped := fmt.Sprintf(`
		var __console = { log: function() { globalThis.__golog(Array.prototype.join.call(arguments, ' ')); } };
		var document = { getElementById: __goGetElementById, createElement: __goCreateElement, querySelector: __goQuerySelector, querySelectorAll: __goQuerySelectorAll, title: __docTitle__ || '', addEventListener: __goAddEventListener };
		var navigator = { userAgent: 'V8Go/1.0', language: 'en-US' };
		var window = globalThis;
		%s
		try { %s } catch(e) { globalThis.__golog('Error: ' + String(e)); }
	`, setTimeoutDef, source)

	// Define window.onload as a getter/setter via Object.defineProperty.
	_ = e.vm.Run(`Object.defineProperty(globalThis, 'onload', {
		get: __goGetOnload,
		set: __goSetOnload,
		configurable: true,
		enumerable: true
	});`)

	// Register console log function (once, under lock).
	e.vm.Lock()
	e.vm.registry.Builtins["__golog"] = func(args []JSValue) JSValue {
		if len(args) > 0 && e.consoleLogFn != nil {
			e.consoleLogFn(args[0].ToString())
		}
		return Undefined
	}
	e.vm.Unlock()

	result := e.vm.Run(wrapped)

	// Always trigger repaint after JS execution (DOM may have changed).
	e.notifyDOMChange()

	return result, nil
}

// SetEventLoop registers an event loop for setTimeout/setInterval support.
func (e *Gov8Engine) SetEventLoop(loop *EventLoop) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventLoop = loop
}

// Close releases V8Go resources.
func (e *Gov8Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vm = nil
	e.initialized = false
	e.closed = true
}

// LastError returns the last error encountered during async operations
// such as DispatchInlineEvent. Returns nil if no error has occurred.
func (e *Gov8Engine) LastError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastError
}

// SetDOMHeap registers the DOM heap for GC coordination.
func (e *Gov8Engine) SetDOMHeap(heap *dom.DOMHeap) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.heap = heap
}

// CollectGarbage triggers a full GC cycle: DOM pre-GC → JS GC → DOM post-GC.
func (e *Gov8Engine) CollectGarbage() {
	e.mu.Lock()
	heap := e.heap
	e.mu.Unlock()

	if heap != nil {
		heap.RunPreGC()
	}

	// JS-side GC: reset the VM state (reinitialize on next use).
	e.mu.Lock()
	if e.vm != nil {
		// The VM doesn't have a built-in GC — resetting purges accumulated state.
		e.vm = nil
		e.initialized = false
	}
	e.mu.Unlock()

	if heap != nil {
		heap.RunPostGC()
	}
}

// ConsoleLogs returns accumulated console output from the V8Go VM.
func (e *Gov8Engine) ConsoleLogs() []string {
	e.mu.Lock()
	vm := e.vm
	e.mu.Unlock()
	if vm == nil {
		return nil
	}
	vm.RLock()
	defer vm.RUnlock()
	return vm.ConsoleLogs()
}

// GetGlobal returns the value of a global variable from the JS VM.
// Returns Undefined if the variable does not exist or the VM is not initialized.
func (e *Gov8Engine) GetGlobal(name string) JSValue {
	e.mu.Lock()
	vm := e.vm
	e.mu.Unlock()
	if vm == nil {
		return Undefined
	}
	vm.RLock()
	defer vm.RUnlock()
	return vm.GetGlobal(name)
}

// FireEvent dispatches a lifecycle event to all registered JS listeners.
// eventType is one of "DOMContentLoaded" or "load" per the HTML Standard.
// Called by the browser after DOM parse and after subresource loading complete.
func (e *Gov8Engine) FireEvent(eventType string, data map[string]JSValue) {
	e.init()
	e.mu.Lock()
	vm := e.vm
	e.mu.Unlock()
	if vm == nil {
		return
	}
	vm.FireEvent(eventType, data)
	e.notifyDOMChange()
}

// elementToGov8Value converts a DOM Element to a V8Go JSValue object.
func (e *Gov8Engine) elementToGov8Value(elem *dom.Element) JSValue {
// Mark element as JS-reachable (unified heap).
// The caller should ensure the engine's heap is available.
	obj := NewJSObject()
	obj.Set("id", NewString(elem.GetAttribute("id")))
	obj.Set("tagName", NewString(strings.ToUpper(elem.LocalName)))
	obj.Set("localName", NewString(elem.LocalName))
	obj.Set("textContent", NewString(elem.TextContent()))
	obj.Set("className", NewString(elem.GetAttribute("class")))
	obj.Set("innerHTML", NewString(elem.InnerHTML()))
	obj.Set("outerHTML", NewString(elem.OuterHTML()))

	// Wire OnPropertySet to intercept JS assignments to mutable properties.
	// When JS does element.textContent = 'x', this mutates the Go DOM element
	// and fires the DOM change callback for repaint.
	obj.ensureInterceptor().OnPropertySet = func(o *JSObject, name string, value JSValue) bool {
		switch name {
		case "textContent":
			elem.SetTextContent(value.ToString())
			e.notifyDOMChange()
			return true
		case "innerHTML":
			_ = elem.SetInnerHTML(value.ToString())
			e.notifyDOMChange()
			return true
		case "outerHTML":
			return true // ignore: would require parser
		case "value":
			elem.SetAttribute("value", value.ToString())
			e.notifyDOMChange()
			return true
		default:
			return false
		}
	}

	// setAttribute/getAttribute
	obj.Set("setAttribute", NewObject(builtinFunc("setAttribute", func(this *JSObject, args []JSValue) JSValue {
		if len(args) > 1 {
			elem.SetAttribute(args[0].ToString(), args[1].ToString())
			e.notifyDOMChange()
		}
		return Undefined
	})))

	obj.Set("getAttribute", NewObject(builtinFunc("getAttribute", func(this *JSObject, args []JSValue) JSValue {
		if len(args) > 0 {
			return NewString(elem.GetAttribute(args[0].ToString()))
		}
		return NewString("")
	})))

	// children — shallow array (id+tag only to avoid circular recursion)
	childrenArr := NewJSObject()
	childrenArr.ConstructorName = "Array"
	if ArrayPrototype != nil {
		childrenArr.Prototype = ArrayPrototype
	}
	children := elem.Children()
	for i, child := range children {
		childObj := NewJSObject()
		childObj.Set("id", NewString(child.GetAttribute("id")))
		childObj.Set("tagName", NewString(strings.ToUpper(child.LocalName)))
		childObj.Set("localName", NewString(child.LocalName))
		childrenArr.Set(intKey(i), NewObject(childObj))
	}
	childrenArr.Set("length", NewNumber(float64(len(children))))
	obj.Set("children", NewObject(childrenArr))

	// parentElement — shallow ref only
	if parent := elem.ParentElement(); parent != nil {
		parentObj := NewJSObject()
		parentObj.Set("id", NewString(parent.GetAttribute("id")))
		parentObj.Set("tagName", NewString(strings.ToUpper(parent.LocalName)))
		parentObj.Set("localName", NewString(parent.LocalName))
		obj.Set("parentElement", NewObject(parentObj))
	} else {
		obj.Set("parentElement", Null)
	}

	// style object
	styleObj := NewJSObject()
	styleObj.Set("cssText", NewString(elem.GetAttribute("style")))
	styleObj.Set("getPropertyValue", NewObject(builtinFunc("getPropertyValue", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewString("")
		}
		return NewString(elem.StyleProperty(args[0].ToString()))
	})))
	styleObj.Set("setProperty", NewObject(builtinFunc("setProperty", func(this *JSObject, args []JSValue) JSValue {
		if len(args) >= 2 {
			prop := args[0].ToString()
			newVal := args[1].ToString()
			// Capture old value before applying new, for transition trigger.
			oldVal := elem.StyleProperty(prop)
			elem.SetStyleProperty(prop, newVal)
			// Notify style transition hook so the browser can trigger CSS transitions.
			if hook := e.styleTransitionHook; hook != nil {
				hook(elem, prop, oldVal, newVal)
			}
			e.notifyDOMChange()
		} else if len(args) == 1 {
			elem.SetStyleProperty(args[0].ToString(), "")
			e.notifyDOMChange()
		}
		return Undefined
	})))
	obj.Set("style", NewObject(styleObj))

	// classList object
	classListObj := NewJSObject()
	classListObj.Set("add", NewObject(builtinFunc("add", func(this *JSObject, args []JSValue) JSValue {
		for _, arg := range args {
			elem.ClassListAdd(arg.ToString())
		}
		e.notifyDOMChange()
		return Undefined
	})))
	classListObj.Set("remove", NewObject(builtinFunc("remove", func(this *JSObject, args []JSValue) JSValue {
		for _, arg := range args {
			elem.ClassListRemove(arg.ToString())
		}
		e.notifyDOMChange()
		return Undefined
	})))
	classListObj.Set("toggle", NewObject(builtinFunc("toggle", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		result := elem.ClassListToggle(args[0].ToString())
		e.notifyDOMChange()
		return NewBoolean(result)
	})))
	classListObj.Set("contains", NewObject(builtinFunc("contains", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return False
		}
		return NewBoolean(elem.ClassListContains(args[0].ToString()))
	})))
	obj.Set("classList", NewObject(classListObj))

	// querySelector / querySelectorAll
	obj.Set("querySelector", NewObject(builtinFunc("querySelector", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return Null
		}
		found := elem.QuerySelector(args[0].ToString())
		if found == nil {
			return Null
		}
		return e.elementToGov8Value(found)
	})))
	obj.Set("querySelectorAll", NewObject(builtinFunc("querySelectorAll", func(this *JSObject, args []JSValue) JSValue {
		if len(args) == 0 {
			return NewObject(NewJSObject())
		}
		results := elem.QuerySelectorAll(args[0].ToString())
		arr := NewJSObject()
		arr.ConstructorName = "Array"
		if ArrayPrototype != nil {
			arr.Prototype = ArrayPrototype
		}
		for i, r := range results {
			arr.Set(intKey(i), e.elementToGov8Value(r))
		}
		arr.Set("length", NewNumber(float64(len(results))))
		return NewObject(arr)
	})))

	return NewObject(obj)
}
