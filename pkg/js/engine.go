// Package js provides JavaScript execution using QuickJS via fastschema/qjs.
// The Engine wraps a QuickJS runtime with browser Web API bindings
// (console, document, navigator, setTimeout).
package js

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/fastschema/qjs"
	"github.com/lucasdss/v8go/pkg/dom"
)

// Engine wraps a QuickJS WebAssembly runtime with browser Web API bindings.
type Engine struct {
	mu               sync.Mutex
	runtime          *qjs.Runtime
	doc              *dom.Document
	heap             *dom.DOMHeap
	consoleLogFn     func(args ...any)
	consoleWarnFn    func(args ...any)
	consoleErrorFn   func(args ...any)
	documentLookup   func(id string) any
	domChangeCallback     func()                                     // called when JS mutates the DOM
	styleTransitionHook   func(el *dom.Element, prop, oldVal, newVal string) // called when JS sets a style property
	initialized           bool
}

// NewEngine creates a new QuickJS JavaScript engine with browser API bindings.
func NewEngine() *Engine { return &Engine{} }

// SetDocument sets the Go DOM document that JS DOM operations will modify.
func (e *Engine) SetDocument(doc *dom.Document) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.doc = doc
}

func (e *Engine) init() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialized && e.runtime != nil {
		return nil
	}
	var err error
	e.runtime, err = qjs.New()
	if err != nil {
		return fmt.Errorf("qjs init: %w", err)
	}
	e.setup()
	e.initialized = true
	return nil
}

func (e *Engine) setup() {
	ctx := e.runtime.Context()
	ctx.SetFunc("__goBrowserConsoleLog", func(this *qjs.This) (*qjs.Value, error) {
		if e.consoleLogFn != nil && len(this.Args()) > 0 {
			e.consoleLogFn(this.Args()[0].String())
		}
		return this.Context().NewUndefined(), nil
	})
	ctx.SetFunc("__goBrowserConsoleWarn", func(this *qjs.This) (*qjs.Value, error) {
		if e.consoleWarnFn != nil && len(this.Args()) > 0 {
			e.consoleWarnFn(this.Args()[0].String())
		}
		return this.Context().NewUndefined(), nil
	})
	ctx.SetFunc("__goBrowserConsoleError", func(this *qjs.This) (*qjs.Value, error) {
		if e.consoleErrorFn != nil && len(this.Args()) > 0 {
			e.consoleErrorFn(this.Args()[0].String())
		}
		return this.Context().NewUndefined(), nil
	})
	// Real Go-backed DOM operations.
	ctx.SetFunc("__goBrowserCreateElement", func(this *qjs.This) (*qjs.Value, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.doc == nil || len(this.Args()) == 0 {
			return this.Context().NewNull(), nil
		}
		tagName := this.Args()[0].String()
		el := e.doc.CreateElement(tagName)
		return e.elementToJSValue(this.Context(), el), nil
})

// document.querySelector
ctx.SetFunc("__goBrowserQuerySelector", func(this *qjs.This) (*qjs.Value, error) {
if e.doc == nil || len(this.Args()) == 0 {
return this.Context().NewNull(), nil
}
found := e.doc.QuerySelector(this.Args()[0].String())
if found == nil {
return this.Context().NewNull(), nil
}
return e.elementToJSValue(this.Context(), found), nil
})
// document.querySelectorAll
ctx.SetFunc("__goBrowserQuerySelectorAll", func(this *qjs.This) (*qjs.Value, error) {
if e.doc == nil || len(this.Args()) == 0 {
arr := this.Context().NewArray()
return arr.Value, nil
}
results := e.doc.QuerySelectorAll(this.Args()[0].String())
arr := this.Context().NewArray()
for i, r := range results {
arr.Set(int64(i), e.elementToJSValue(this.Context(), r))
}
return arr.Value, nil
})
	ctx.SetFunc("__goBrowserAppendChild", func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) < 2 {
			return this.Context().NewNull(), nil
		}
		// Args[0] = parent element ID (or index), Args[1] = child element
		// For simplicity: both are passed as Go-backed JS objects
		// Just return the child as-is (stub for now)
		return this.Args()[1], nil
	})

	// TypeGlobal() ensures these are accessible across Eval calls.
	_, _ = e.runtime.Eval("setup.js", qjs.Code(`
		globalThis.__console_output = [];
		globalThis.console = {
			log: function() { var p=[]; for(var i=0;i<arguments.length;i++) p.push(String(arguments[i])); globalThis.__goBrowserConsoleLog(p.join(' ')); },
			warn: function() { var p=[]; for(var i=0;i<arguments.length;i++) p.push(String(arguments[i])); globalThis.__goBrowserConsoleWarn(p.join(' ')); },
			error: function() { var p=[]; for(var i=0;i<arguments.length;i++) p.push(String(arguments[i])); globalThis.__goBrowserConsoleError(p.join(' ')); }
		};

		// __createElement: full DOM-like element factory for JS-side manipulation.
		globalThis.__createElement = function(tagName) {
			var el = {
				tagName: (tagName||'div').toUpperCase(),
				localName: (tagName||'div').toLowerCase(),
				id: '', className: '', style: {},
				children: [], childNodes: [],
				attributes: {}, innerHTML: '', textContent: '',
				setAttribute: function(n,v){this.attributes[n]=v;if(n==='id')this.id=v;if(n==='class')this.className=v;},
				getAttribute: function(n){return this.attributes[n]||'';},
				appendChild: function(c){this.children.push(c);this.childNodes.push(c);c.parentNode=this;return c;},
				removeChild: function(c){var i=this.children.indexOf(c);if(i>=0){this.children.splice(i,1);this.childNodes.splice(i,1);}return c;},
				querySelector: function(){return null;},
				querySelectorAll: function(){return[];},
				addEventListener: function(){},
				getElementsByTagName: function(t){var r=[];for(var i=0;i<this.children.length;i++){if(this.children[i].localName===t)r.push(this.children[i]);}return r;},
				cloneNode: function(){return globalThis.__createElement(this.localName);}
			};
			return el;
		};

		globalThis.__goBrowserGetElementById = function(id) { return null; };
		globalThis.document = globalThis.__createElement('document');
		globalThis.document.title = '';
		globalThis.document.body = globalThis.__createElement('body');
		globalThis.document.head = globalThis.__createElement('head');
		globalThis.document.documentElement = globalThis.__createElement('html');
		globalThis.document.getElementById = function(id) {
			var r = globalThis.__goBrowserGetElementById(String(id));
			return r; // null if not found — callers should check
		};
		globalThis.document.createElement = function(tagName) {
			var el = globalThis.__goBrowserCreateElement(String(tagName));
			if (el !== null && el !== undefined) return el;
			return globalThis.__createElement(tagName); // fallback
};
globalThis.document.querySelector = function(sel) {
var r = globalThis.__goBrowserQuerySelector(String(sel));
if (r !== null && r !== undefined) return r;
return null;
};
globalThis.document.querySelectorAll = function(sel) {
var r = globalThis.__goBrowserQuerySelectorAll(String(sel));
if (r !== null && r !== undefined) return r;
return [];
};

		globalThis.navigator = { userAgent: 'GoBrowser/1.0', language: 'en-US' };
		globalThis.setTimeout = function(fn, ms) { return 0; };
		globalThis.location = { href: '', protocol: 'https:', hostname: '', pathname: '/', search: '', hash: '' };
		globalThis.window = globalThis;
		globalThis.self = globalThis;
	`), qjs.TypeGlobal())
}

// SetConsoleLogger configures console.log/warn/error output callbacks.
func (e *Engine) SetConsoleLogger(fn, warnFn, errFn func(args ...any)) {
	e.consoleLogFn = fn
	e.consoleWarnFn = warnFn
	e.consoleErrorFn = errFn
}

// SetLocation updates window.location seen by JavaScript.
func (e *Engine) SetLocation(url string) {
	_ = e.init()
	_, _ = e.runtime.Eval("loc.js", qjs.Code(`globalThis.location = `+strconv.Quote(url)+`;`), qjs.TypeGlobal())
}

// SetDocumentBinder updates the minimal document object exposed to JavaScript.
// The binder backs document.getElementById for simple DOM inspection from
// scripts. It returns lightweight snapshots of DOM elements; mutating returned
// objects does not yet mutate the Go DOM tree.
func (e *Engine) SetDocumentBinder(title string, lookup func(id string) any) {
	_ = e.init()
	e.mu.Lock()
	defer e.mu.Unlock()

	e.documentLookup = lookup
	e.runtime.Context().SetFunc("__goBrowserGetElementById", func(this *qjs.This) (*qjs.Value, error) {
		if e.documentLookup == nil || len(this.Args()) == 0 {
			return this.Context().NewNull(), nil
		}
		id := this.Args()[0].String()
		matched := e.documentLookup(id)
		elem, ok := matched.(*dom.Element)
		if !ok || elem == nil {
			return this.Context().NewNull(), nil
		}
		return e.elementToJSValue(this.Context(), elem), nil
	})

	_, _ = e.runtime.Eval("doc.js", qjs.Code(`globalThis.document.title = `+strconv.Quote(title)+`;`), qjs.TypeGlobal())
}

func (e *Engine) elementToJSValue(ctx *qjs.Context, elem *dom.Element) *qjs.Value {
	// Capture the DOM change callback so mutation handlers can trigger repaint.
	// Read under no extra lock — caller already holds e.mu during elementToJSValue.
	domCB := e.domChangeCallback

	obj := ctx.NewObject()
	obj.SetPropertyStr("id", ctx.NewString(elem.GetAttribute("id")))
	obj.SetPropertyStr("localName", ctx.NewString(elem.LocalName))
	obj.SetPropertyStr("tagName", ctx.NewString(strings.ToUpper(elem.LocalName)))
	obj.SetPropertyStr("textContent", ctx.NewString(elem.TextContent()))
	obj.SetPropertyStr("className", ctx.NewString(elem.ClassName()))
	obj.SetPropertyStr("innerHTML", ctx.NewString(elem.InnerHTML()))
	obj.SetPropertyStr("outerHTML", ctx.NewString(elem.OuterHTML()))

	// getAttribute / setAttribute
	obj.SetPropertyStr("getAttribute", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) == 0 {
			return this.Context().NewString(""), nil
		}
		return this.Context().NewString(elem.GetAttribute(this.Args()[0].String())), nil
	}))
	obj.SetPropertyStr("setAttribute", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) >= 2 {
			elem.SetAttribute(this.Args()[0].String(), this.Args()[1].String())
		}
		if domCB != nil {
			domCB()
		}
		return this.Context().NewUndefined(), nil
	}))

	// children – shallow array (id+tag only to avoid circular recursion)
	childrenArr := ctx.NewArray()
	for i, c := range elem.Children() {
		childObj := ctx.NewObject()
		childObj.SetPropertyStr("id", ctx.NewString(c.GetAttribute("id")))
		childObj.SetPropertyStr("tagName", ctx.NewString(strings.ToUpper(c.LocalName)))
		childObj.SetPropertyStr("localName", ctx.NewString(c.LocalName))
		childrenArr.Set(int64(i), childObj)
	}
	obj.SetPropertyStr("children", childrenArr.Value)

	// parentElement – shallow ref only
	if parent := elem.ParentElement(); parent != nil {
		parentObj := ctx.NewObject()
		parentObj.SetPropertyStr("id", ctx.NewString(parent.GetAttribute("id")))
		parentObj.SetPropertyStr("tagName", ctx.NewString(strings.ToUpper(parent.LocalName)))
		parentObj.SetPropertyStr("localName", ctx.NewString(parent.LocalName))
		obj.SetPropertyStr("parentElement", parentObj)
	} else {
		obj.SetPropertyStr("parentElement", ctx.NewNull())
	}

	// style object
	styleObj := ctx.NewObject()
	styleObj.SetPropertyStr("cssText", ctx.NewString(elem.GetAttribute("style")))
	styleObj.SetPropertyStr("getPropertyValue", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) == 0 {
			return this.Context().NewString(""), nil
		}
		return this.Context().NewString(elem.StyleProperty(this.Args()[0].String())), nil
	}))
	styleObj.SetPropertyStr("setProperty", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) >= 2 {
			prop := this.Args()[0].String()
			newVal := this.Args()[1].String()
			// Capture old value before applying new, for transition trigger.
			oldVal := elem.StyleProperty(prop)
			elem.SetStyleProperty(prop, newVal)
			// Notify style transition hook so the browser can trigger CSS transitions.
			if hook := e.styleTransitionHook; hook != nil {
				hook(elem, prop, oldVal, newVal)
			}
		} else if len(this.Args()) == 1 {
			elem.SetStyleProperty(this.Args()[0].String(), "")
		}
		if domCB != nil {
			domCB()
		}
		return this.Context().NewUndefined(), nil
	}))
	obj.SetPropertyStr("style", styleObj)

	// classList object
	classListObj := ctx.NewObject()
	classListObj.SetPropertyStr("add", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		for _, arg := range this.Args() {
			elem.ClassListAdd(arg.String())
		}
		if domCB != nil {
			domCB()
		}
		return this.Context().NewUndefined(), nil
	}))
	classListObj.SetPropertyStr("remove", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		for _, arg := range this.Args() {
			elem.ClassListRemove(arg.String())
		}
		if domCB != nil {
			domCB()
		}
		return this.Context().NewUndefined(), nil
	}))
	classListObj.SetPropertyStr("toggle", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) == 0 {
			return this.Context().NewBool(false), nil
		}
		result := elem.ClassListToggle(this.Args()[0].String())
		if domCB != nil {
			domCB()
		}
		return this.Context().NewBool(result), nil
	}))
	classListObj.SetPropertyStr("contains", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) == 0 {
			return this.Context().NewBool(false), nil
		}
		return this.Context().NewBool(elem.ClassListContains(this.Args()[0].String())), nil
	}))
	obj.SetPropertyStr("classList", classListObj)

	// querySelector / querySelectorAll
	obj.SetPropertyStr("querySelector", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) == 0 {
			return this.Context().NewNull(), nil
		}
		found := elem.QuerySelector(this.Args()[0].String())
		if found == nil {
			return this.Context().NewNull(), nil
		}
		return e.elementToJSValue(this.Context(), found), nil
	}))
	obj.SetPropertyStr("querySelectorAll", ctx.Function(func(this *qjs.This) (*qjs.Value, error) {
		if len(this.Args()) == 0 {
			arr := this.Context().NewArray()
			return arr.Value, nil
		}
		results := elem.QuerySelectorAll(this.Args()[0].String())
		arr := this.Context().NewArray()
		for i, r := range results {
			arr.Set(int64(i), e.elementToJSValue(this.Context(), r))
		}
		return arr.Value, nil
	}))

	return obj
}

// Execute runs a JavaScript source string and forwards console output.
func (e *Engine) Execute(source string) error {
	if err := e.init(); err != nil {
		return err
	}

	// Execute in global scope with try-catch for error isolation.
	// NOTE: e.mu is NOT held during runtime.Eval because DOM binding
	// callbacks (__goBrowserCreateElement, etc.) acquire e.mu internally.
	// Holding e.mu here would cause a self-deadlock on those callbacks.
	w := fmt.Sprintf(`try {
const console = {
log: function() { var p=[]; for(var i=0;i<arguments.length;i++) p.push(String(arguments[i])); globalThis.__goBrowserConsoleLog(p.join(' ')); },
warn: function() { var p=[]; for(var i=0;i<arguments.length;i++) p.push(String(arguments[i])); globalThis.__goBrowserConsoleWarn(p.join(' ')); },
error: function() { var p=[]; for(var i=0;i<arguments.length;i++) p.push(String(arguments[i])); globalThis.__goBrowserConsoleError(p.join(' ')); }
};
const document = globalThis.document;
const navigator = globalThis.navigator;
const setTimeout = globalThis.setTimeout;
const window = globalThis;
const self = globalThis;
const location = globalThis.location;
%s
} catch(e) { globalThis.__goBrowserConsoleError(String(e)); }`, source)
	_, evalErr := e.runtime.Eval("user.js", qjs.Code(w), qjs.TypeGlobal())

	// Drain accumulated console output and trigger repaint under lock
	// to serialise against concurrent SetDocument / SetDocumentBinder calls.
	e.mu.Lock()
	e.drain()
	cb := e.domChangeCallback
	e.mu.Unlock()

	if cb != nil {
		cb()
	}

	return evalErr
}

func (e *Engine) drain() {
	result, err := e.runtime.Eval("drain.js", qjs.Code(`
		(function(){
			var a = __console_output;
			if (!a || a.length === 0) return "";
			var s = String(a[0]);
			for (var i = 1; i < a.length; i++) s += "\n" + String(a[i]);
			a.length = 0;
			return s;
		})()
	`), qjs.TypeGlobal())
	if err != nil || result == nil {
		return
	}
	for _, line := range strings.Split(result.String(), "\n") {
		if line != "" && e.consoleLogFn != nil {
			e.consoleLogFn(line)
		}
	}
}

// Close releases the QuickJS runtime resources.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtime != nil {
		e.runtime.Close()
		e.runtime = nil
	}
	e.initialized = false
}

// SetDOMHeap registers the DOM heap for GC coordination.
func (e *Engine) SetDOMHeap(heap *dom.DOMHeap) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.heap = heap
}

// SetDOMChangeCallback registers a callback invoked when JS mutates the DOM.
// The QuickJS engine fires this through the JS-side mutation wrappers.
func (e *Engine) SetDOMChangeCallback(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.domChangeCallback = fn
}

// SetStyleTransitionHook registers a callback invoked when JS sets a style
// property via element.style.setProperty(). The callback receives the element,
// property name, old value, and new value so the browser can trigger CSS
// transitions through the animation engine.
func (e *Engine) SetStyleTransitionHook(fn func(el *dom.Element, prop, oldVal, newVal string)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.styleTransitionHook = fn
}

// CollectGarbage triggers a full GC cycle: DOM pre-GC → JS GC → DOM post-GC.
func (e *Engine) CollectGarbage() {
	e.mu.Lock()
	heap := e.heap
	e.mu.Unlock()

	if heap != nil {
		heap.RunPreGC()
	}

	// JS-side GC: QuickJS has runtime.RunGC() but it's not exposed in the binding.
	// The runtime will free memory naturally as contexts are closed.
	// We reset the runtime to simulate a major GC cycle.
	e.mu.Lock()
	if e.runtime != nil {
		e.runtime.Close()
		e.runtime = nil
		e.initialized = false
	}
	e.mu.Unlock()

	if heap != nil {
		heap.RunPostGC()
	}
}
