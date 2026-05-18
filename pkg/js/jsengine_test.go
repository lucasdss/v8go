package js_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/dom"
	"github.com/lucasdss/v8go/pkg/js"
	"github.com/lucasdss/v8go/pkg/parser"
)

// ────────────────────── QuickJS engine bridge tests ──────────────────────

func TestEngine_QuerySelector(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><div id="a" class="x"><p class="y">hello</p></div><div id="b"></div></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	// Test querySelector
	err := engine.Execute(`
		var el = document.querySelector('div.x');
		console.log(el ? el.id : 'null');
		var missing = document.querySelector('span');
		console.log(missing ? 'found' : 'null');
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(logs) < 2 {
		t.Fatalf("logs = %v, want 2 entries", logs)
	}
	if logs[0] != "a" {
		t.Errorf("querySelector div.x: got %q, want a", logs[0])
	}
	if logs[1] != "null" {
		t.Errorf("querySelector span: got %q, want null", logs[1])
	}
}

func TestEngine_QuerySelectorAll(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><div class="x">a</div><div class="x">b</div><div>c</div></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		var all = document.querySelectorAll('div');
		console.log(all.length);
		var x = document.querySelectorAll('.x');
		console.log(x.length);
		var none = document.querySelectorAll('span');
		console.log(none.length);
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(logs) < 3 {
		t.Fatalf("logs = %v", logs)
	}
	if logs[0] != "3" {
		t.Errorf("querySelectorAll div: got %q", logs[0])
	}
	if logs[1] != "2" {
		t.Errorf("querySelectorAll .x: got %q", logs[1])
	}
	if logs[2] != "0" {
		t.Errorf("querySelectorAll span: got %q", logs[2])
	}
}

func TestEngine_ElementStyleGetSetProperty(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><p id="target" style="color: red; font-size: 12px">hello</p></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.style.getPropertyValue('color'));
			el.style.setProperty('color', 'blue');
			console.log(el.style.getPropertyValue('color'));
			console.log(el.style.getPropertyValue('font-size'));
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(logs) < 3 {
		t.Fatalf("logs = %v", logs)
	}
	if logs[0] != "red" {
		t.Errorf("color before: got %q, want red", logs[0])
	}
	if logs[1] != "blue" {
		t.Errorf("color after: got %q, want blue", logs[1])
	}
	if logs[2] != "12px" {
		t.Errorf("font-size: got %q, want 12px", logs[2])
	}

	// Verify the DOM was actually mutated.
	el := doc.GetElementByID("target")
	if el == nil {
		t.Fatal("element not found")
	}
	if el.GetAttribute("style") != "color: blue; font-size: 12px" {
		t.Errorf("style attr: got %q", el.GetAttribute("style"))
	}
}

func TestEngine_ElementClassList(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><p id="target" class="foo bar">hello</p></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.classList.contains('foo'));
			console.log(el.classList.contains('baz'));
			el.classList.add('baz');
			console.log(el.classList.contains('baz'));
			el.classList.remove('bar');
			console.log(el.classList.contains('bar'));
			console.log(el.classList.toggle('foo'));
			console.log(el.classList.contains('foo'));
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(logs) != 6 {
		t.Fatalf("logs = %v", logs)
	}
	if logs[0] != "true" {
		t.Errorf("contains foo: got %q", logs[0])
	}
	if logs[1] != "false" {
		t.Errorf("contains baz: got %q", logs[1])
	}
	if logs[2] != "true" {
		t.Errorf("contains baz after add: got %q", logs[2])
	}
	if logs[3] != "false" {
		t.Errorf("contains bar after remove: got %q", logs[3])
	}
	if logs[4] != "false" {
		t.Errorf("toggle foo: got %q, want false", logs[4])
	}
	if logs[5] != "false" {
		t.Errorf("contains foo after toggle: got %q", logs[5])
	}
}

func TestEngine_ElementSetGetAttribute(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><p id="target" data-x="hello">world</p></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.getAttribute('data-x'));
			el.setAttribute('data-x', 'changed');
			console.log(el.getAttribute('data-x'));
			console.log(el.getAttribute('missing'));
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "hello" {
		t.Errorf("getAttribute: got %q", logs[0])
	}
	if logs[1] != "changed" {
		t.Errorf("getAttribute after set: got %q", logs[1])
	}
	if logs[2] != "" {
		t.Errorf("getAttribute missing: got %q", logs[2])
	}
}

func TestEngine_ElementInnerHTML(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><div id="target"><span>hello</span></div></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.innerHTML);
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "<span>hello</span>" {
		t.Errorf("innerHTML: got %q", logs[0])
	}
}

func TestEngine_ElementOuterHTML(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><div id="target"><span>hello</span></div></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.outerHTML);
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != `<div id="target"><span>hello</span></div>` {
		t.Errorf("outerHTML: got %q", logs[0])
	}
}

func TestEngine_ElementChildrenAndParent(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><div id="parent"><p id="child1">a</p><span id="child2">b</span></div></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		var parent = document.getElementById('parent');
		if (!parent) { console.log('NO_PARENT'); } else {
			console.log(parent.children.length);
			console.log(parent.children[0].id);
			console.log(parent.children[1].tagName);
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "2" {
		t.Errorf("children length: got %q", logs[0])
	}
	if logs[1] != "child1" {
		t.Errorf("children[0].id: got %q", logs[1])
	}
	if logs[2] != "SPAN" {
		t.Errorf("children[1].tagName: got %q", logs[2])
	}
}

func TestEngine_QuerySelectorIdClassAttr(t *testing.T) {
	doc := parser.ParseHTML(`<html><head></head><body><p id="x">a</p><p class="y">b</p><p data-kind="z">c</p></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocument(doc); engine.SetDocumentBinder(doc.Title(), func(id string) any { return doc.GetElementByID(id) })

	err := engine.Execute(`
		console.log(document.querySelector('#x').textContent);
		console.log(document.querySelector('.y').textContent);
		console.log(document.querySelector('[data-kind=z]').textContent);
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "a" {
		t.Errorf("#id: got %q", logs[0])
	}
	if logs[1] != "b" {
		t.Errorf(".class: got %q", logs[1])
	}
	if logs[2] != "c" {
		t.Errorf("[attr]: got %q", logs[2])
	}
}

// ────────────────────── GoV8 engine bridge tests ──────────────────────

func TestGov8Engine_QuerySelector(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	a := d.CreateElement("div")
	a.SetAttribute("id", "a")
	a.SetAttribute("class", "x")
	b := d.CreateElement("div")
	b.SetAttribute("id", "b")
	_ = d.AppendChild(&html.Node)
	_ = html.AppendChild(&body.Node)
	_ = body.AppendChild(&a.Node)
	_ = body.AppendChild(&b.Node)

	engine := js.NewGov8Engine()
	defer engine.Close()
	engine.SetDocument(d)

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	_, err := engine.Execute(`
		var el = document.querySelector('div.x');
		console.log(el ? el.id : 'null');
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(logs) == 0 || logs[0] != "a" {
		t.Errorf("querySelector div.x: got %v, want a", logs)
	}
}

func TestGov8Engine_ElementStyleGetSet(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	p := d.CreateElement("p")
	p.SetAttribute("id", "target")
	p.SetAttribute("style", "color: red")
	_ = d.AppendChild(&html.Node)
	_ = html.AppendChild(&body.Node)
	_ = body.AppendChild(&p.Node)

	engine := js.NewGov8Engine()
	defer engine.Close()
	engine.SetDocument(d)

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	_, err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.style.getPropertyValue('color'));
			el.style.setProperty('color', 'blue');
			console.log(el.style.getPropertyValue('color'));
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "red" {
		t.Errorf("color before: got %q", logs[0])
	}
	if logs[1] != "blue" {
		t.Errorf("color after: got %q", logs[1])
	}
	// Verify mutation.
	if p.GetAttribute("style") != "color: blue" {
		t.Errorf("style attr: got %q", p.GetAttribute("style"))
	}
}

func TestGov8Engine_ClassList(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	p := d.CreateElement("p")
	p.SetAttribute("id", "target")
	p.SetAttribute("class", "foo bar")
	_ = d.AppendChild(&html.Node)
	_ = html.AppendChild(&body.Node)
	_ = body.AppendChild(&p.Node)

	engine := js.NewGov8Engine()
	defer engine.Close()
	engine.SetDocument(d)

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	_, err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.classList.contains('foo'));
			console.log(el.classList.contains('baz'));
			el.classList.add('baz');
			console.log(el.classList.contains('baz'));
			el.classList.remove('bar');
			console.log(el.classList.contains('bar'));
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "true" || logs[1] != "false" || logs[2] != "true" || logs[3] != "false" {
		t.Errorf("classList: got %v", logs)
	}
}

func TestGov8Engine_SetGetAttribute(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	p := d.CreateElement("p")
	p.SetAttribute("id", "target")
	p.SetAttribute("data-x", "hello")
	_ = d.AppendChild(&html.Node)
	_ = html.AppendChild(&body.Node)
	_ = body.AppendChild(&p.Node)

	engine := js.NewGov8Engine()
	defer engine.Close()
	engine.SetDocument(d)

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	_, err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.getAttribute('data-x'));
			el.setAttribute('data-x', 'changed');
			console.log(el.getAttribute('data-x'));
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "hello" || logs[1] != "changed" {
		t.Errorf("setAttribute: got %v", logs)
	}
}

func TestGov8Engine_InnerOuterHTML(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	div := d.CreateElement("div")
	div.SetAttribute("id", "target")
	span := d.CreateElement("span")
	txt := d.CreateTextNode("hello")
	_ = d.AppendChild(&html.Node)
	_ = html.AppendChild(&body.Node)
	_ = body.AppendChild(&div.Node)
	_ = div.AppendChild(&span.Node)
	_ = span.AppendChild(&txt.Node)

	engine := js.NewGov8Engine()
	defer engine.Close()
	engine.SetDocument(d)

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	_, err := engine.Execute(`
		var el = document.getElementById('target');
		if (!el) { console.log('NO_EL'); } else {
			console.log(el.innerHTML);
			console.log(el.outerHTML);
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "<span>hello</span>" {
		t.Errorf("innerHTML: got %q", logs[0])
	}
	if logs[1] != `<div id="target"><span>hello</span></div>` {
		t.Errorf("outerHTML: got %q", logs[1])
	}
}

func TestGov8Engine_ChildrenAndParent(t *testing.T) {
	d := dom.NewDocument()
	html := d.CreateElement("html")
	body := d.CreateElement("body")
	parent := d.CreateElement("div")
	parent.SetAttribute("id", "parent")
	c1 := d.CreateElement("p")
	c1.SetAttribute("id", "child1")
	c2 := d.CreateElement("span")
	c2.SetAttribute("id", "child2")
	_ = d.AppendChild(&html.Node)
	_ = html.AppendChild(&body.Node)
	_ = body.AppendChild(&parent.Node)
	_ = parent.AppendChild(&c1.Node)
	_ = parent.AppendChild(&c2.Node)

	engine := js.NewGov8Engine()
	defer engine.Close()
	engine.SetDocument(d)

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	_, err := engine.Execute(`
		var p = document.getElementById('parent');
		if (!p) { console.log('NO_PARENT'); } else {
			console.log(p.children.length);
			console.log(p.children[0].id);
			console.log(p.children[1].tagName);
		}
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if logs[0] != "2" {
		t.Errorf("children length: %v", logs)
	}
	if logs[1] != "child1" {
		t.Errorf("children[0].id: %v", logs)
	}
	if logs[2] != "SPAN" {
		t.Errorf("children[1].tagName: %v", logs)
	}
}
