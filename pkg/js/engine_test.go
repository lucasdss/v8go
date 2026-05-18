//go:build qjs

package js_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
	"github.com/lucasdss/v8go/pkg/parser"
)

func TestEngine_DocumentGetElementByIDSnapshot(t *testing.T) {
	doc := parser.ParseHTML(`<html><head><title>Spec</title></head><body><p id="target" data-kind="greeting">Hello</p></body></html>`)
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)
	engine.SetDocumentBinder(doc.Title(), func(id string) any {
		return doc.GetElementByID(id)
	})

	err := engine.Execute(`
		var el = document.getElementById('target');
		console.log(document.title + ':' + el.id + ':' + el.tagName + ':' + el.textContent + ':' + el.getAttribute('data-kind'));
		console.log(String(document.getElementById('missing')));
	`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs = %v, want 2 entries", logs)
	}
	if logs[0] != "Spec:target:P:Hello:greeting" {
		t.Fatalf("first log = %q", logs[0])
	}
	if logs[1] != "null" {
		t.Fatalf("missing element log = %q", logs[1])
	}
}

func TestEngine_ExecuteCanRunMultipleScripts(t *testing.T) {
	engine := js.NewEngine()
	defer engine.Close()

	var logs []string
	engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	if err := engine.Execute(`console.log('first');`); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if err := engine.Execute(`console.log('second');`); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if len(logs) != 2 || logs[0] != "first" || logs[1] != "second" {
		t.Fatalf("logs = %v", logs)
	}
}

func TestAgent_QueueScriptExecutesClassicScript(t *testing.T) {
	agent := js.NewAgent()
	defer agent.Engine.Close()

	var logs []string
	agent.Engine.SetConsoleLogger(func(args ...any) {
		for _, arg := range args {
			logs = append(logs, arg.(string))
		}
	}, nil, nil)

	agent.QueueScript(&js.Script{Type: js.ClassicScript, Source: `console.log('queued script ran');`})
	agent.Loop.RunUntilEmpty()

	if len(logs) != 1 || logs[0] != "queued script ran" {
		t.Fatalf("logs = %v", logs)
	}
}
