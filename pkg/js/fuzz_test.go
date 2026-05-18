// fuzz_test.go — Fuzzing tests for the V8Go JS engine.
//
// Per gojs.md Section 5: feed the parser random strings to ensure
// the engine doesn't panic on malformed JavaScript.
package js_test

import (
	"testing"

	"github.com/lucasdss/v8go/pkg/js"
)

// FuzzParser feeds random byte sequences to the lexer and parser.
func FuzzParser(f *testing.F) {
	// Seed corpus with 50+ valid JS constructs covering all implemented features.
	seeds := []string{
		// Arithmetic
		"1+1", "1-2", "3*4", "6/2", "7%3", "2**8",
		// Comparison
		"1<2", "3>1", "1<=1", "1>=1", "1==1", "1!=2", "1===1", "1!==2",
		// Logical
		"!true", "true && false", "false || true",
		// Types
		"typeof 42", "typeof 'hello'", "typeof true", "typeof null",
		"typeof undefined", "typeof Symbol()", "typeof 42n", "typeof {}",
		// Arrays
		"[1,2,3].length", "[].push(1)", "[1,2].pop()", "[1,2,3].map(x=>x*2)",
		// Objects
		"({x:1}).x", "Object.keys({a:1,b:2})", "Object.assign({},{x:1})",
		// Functions
		"function f(){return 1};f()", "(function(x){return x})(42)",
		// Scope
		"var x=1;x", "let y=2;y", "const z=3;z",
		// Classes
		"class A{};new A()", "class B extends A{};new B()",
		// Async
		"async function f(){return 1};f()",
		// Generators
		"function*g(){yield 1}",
		// Template
		"var a=1;`hello ${a}`",
		// Destructure
		"var[a,b]=[1,2];a+b",
		// Spread
		"var x=[1,2];[...x,3].length",
		// Optional chaining / nullish
		"null?.x", "{x:1}?.x", "null??1", "0??1",
		// Logical assignment
		"var x=0;x||=1;x", "var y=2;y&&=0;y",
		// BigInt
		"42n+1n", "typeof 42n",
		// RegExp
		"/test/.test('test')", "/test/i.test('TEST')",
		// Text decoration
		"e=>e.trimStart()", "e=>e.trimEnd()", "e=>e.padStart(5,'0')",
		// Map/Set
		"new Map().set('k','v').get('k')",
		// Typed arrays
		"new Int32Array(4).length", "new Float64Array(2)",
		// Proxy
		"new Proxy({},{get(){return 42}}).x",
		// Reflect
		"Reflect.get({x:1},'x')",
		// JSON
		"JSON.stringify({a:1})",
		// Classic constructs
		"if(true){1}else{2}", "while(false){break}",
		"for(var i=0;i<10;i++){i}", "switch(x){case 1:break;default:break}",
		"try{throw 1}catch(e){e}", "[1,2,3]", "{a:1,b:2}",
		"a.b.c", "a?b:c", "1+2*3",
		"(a,b)=>a+b", "null==undefined",
		"function outer(){var x=1;return function(){return x}}",
		"0x1F", "null", "undefined", "true", "false", "Symbol()",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Don't let the fuzzer crash us — catch any panic.
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic on input %q: %v", string(data), r)
			}
		}()

		// Tokenize.
		tokens := js.NewLexer(string(data)).Tokenize()

		// Parse.
		_, errs := js.NewParser(tokens).Parse()

		// Parser errors are expected for malformed input — just don't panic.
		_ = errs
	})
}

// FuzzVM feeds random strings through the full VM pipeline (lex → parse → compile → execute).
func FuzzVM(f *testing.F) {
	// Seed corpus with 50+ valid JS constructs covering all implemented features.
	seeds := []string{
		// Arithmetic
		"1+1", "1-2", "3*4", "6/2", "7%3", "2**8",
		// Comparison
		"1<2", "3>1", "1<=1", "1>=1", "1==1", "1!=2", "1===1", "1!==2",
		// Logical
		"!true", "true && false", "false || true",
		// Types
		"typeof 42", "typeof 'hello'", "typeof true", "typeof null",
		"typeof undefined", "typeof Symbol()", "typeof 42n", "typeof {}",
		// Arrays
		"[1,2,3].length", "[].push(1)", "[1,2].pop()", "[1,2,3].map(x=>x*2)",
		// Objects
		"({x:1}).x", "Object.keys({a:1,b:2})", "Object.assign({},{x:1})",
		// Functions
		"function f(){return 1};f()", "(function(x){return x})(42)",
		// Scope
		"var x=1;x", "let y=2;y", "const z=3;z",
		// Classes
		"class A{};new A()", "class B extends A{};new B()",
		// Async
		"async function f(){return 1};f()",
		// Generators
		"function*g(){yield 1}",
		// Template
		"var a=1;`hello ${a}`",
		// Destructure
		"var[a,b]=[1,2];a+b",
		// Spread
		"var x=[1,2];[...x,3].length",
		// Optional chaining / nullish
		"null?.x", "{x:1}?.x", "null??1", "0??1",
		// Logical assignment
		"var x=0;x||=1;x", "var y=2;y&&=0;y",
		// BigInt
		"42n+1n", "typeof 42n",
		// RegExp
		"/test/.test('test')", "/test/i.test('TEST')",
		// Text decoration
		"e=>e.trimStart()", "e=>e.trimEnd()", "e=>e.padStart(5,'0')",
		// Map/Set
		"new Map().set('k','v').get('k')",
		// Typed arrays
		"new Int32Array(4).length", "new Float64Array(2)",
		// Proxy
		"new Proxy({},{get(){return 42}}).x",
		// Reflect
		"Reflect.get({x:1},'x')",
		// JSON
		"JSON.stringify({a:1})",
		// Classic constructs
		"null==undefined", "typeof null",
		"for(var i=0;i<10;i++);", "try{throw 1}catch(e){}",
		"/test/g", "Symbol()",
		"import('x')", "42n",
		"`hello${1}`", "{...{a:1}}", "o?.x??2",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("VM panic on input %q: %v", string(data), r)
			}
		}()
		vm := js.NewVM()
		vm.Run(string(data))
	})
}

// FuzzBytecode feeds random strings through the bytecode compiler and VM execution.
// This ensures the compiler doesn't produce invalid bytecode that could crash the VM.
func FuzzBytecode(f *testing.F) {
	// Seed corpus with 50+ valid JS constructs covering all implemented features.
	seeds := []string{
		// Arithmetic
		"1+1", "1-2", "3*4", "6/2", "7%3", "2**8",
		// Comparison
		"1<2", "3>1", "1<=1", "1>=1", "1==1", "1!=2", "1===1", "1!==2",
		// Logical
		"!true", "true && false", "false || true",
		// Types
		"typeof 42", "typeof 'hello'", "typeof true", "typeof null",
		"typeof undefined", "typeof Symbol()", "typeof 42n", "typeof {}",
		// Arrays
		"[1,2,3].length", "[].push(1)", "[1,2].pop()", "[1,2,3].map(x=>x*2)",
		// Objects
		"({x:1}).x", "Object.keys({a:1,b:2})", "Object.assign({},{x:1})",
		// Functions
		"function f(){return 1};f()", "(function(x){return x})(42)",
		// Scope
		"var x=1;x", "let y=2;y", "const z=3;z",
		// Classes
		"class A{};new A()", "class B extends A{};new B()",
		// Async
		"async function f(){return 1};f()",
		// Generators
		"function*g(){yield 1}",
		// Template
		"var a=1;`hello ${a}`",
		// Destructure
		"var[a,b]=[1,2];a+b",
		// Spread
		"var x=[1,2];[...x,3].length",
		// Optional chaining / nullish
		"null?.x", "{x:1}?.x", "null??1", "0??1",
		// Logical assignment
		"var x=0;x||=1;x", "var y=2;y&&=0;y",
		// BigInt
		"42n+1n", "typeof 42n",
		// RegExp
		"/test/.test('test')", "/test/i.test('TEST')",
		// Text decoration
		"e=>e.trimStart()", "e=>e.trimEnd()", "e=>e.padStart(5,'0')",
		// Map/Set
		"new Map().set('k','v').get('k')",
		// Typed arrays
		"new Int32Array(4).length", "new Float64Array(2)",
		// Proxy
		"new Proxy({},{get(){return 42}}).x",
		// Reflect
		"Reflect.get({x:1},'x')",
		// JSON
		"JSON.stringify({a:1})",
		// Classic constructs
		"1+2", "var x=1;x", "function f(){return 1};f()",
		"if(true){1}else{2}", "while(false){break}",
		"for(var i=0;i<10;i++){i}", "[1,2,3]", "{a:1,b:2}",
		"a.b.c", "!a&&b||c", "typeof x", "1+2*3",
		"null==undefined", "typeof null", "[1,2,3].map(x=>x*2)",
		"class A{};new A()", "Symbol()", "try{throw 1}catch(e){}",
		"/test/g", "`hello${1}`", "{...{a:1}}", "o?.x??2",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Bytecode compile/execute panic on %q: %v", string(data), r)
			}
		}()

		src := string(data)
		tokens := js.NewLexer(src).Tokenize()
		_, errs := js.NewParser(tokens).Parse()
		if len(errs) > 0 {
			return // Malformed input — skip compilation
		}

		bf := js.CompileString(src)
		if bf == nil {
			return
		}

		vm := js.NewVM()
		vm.Execute(bf)
	})
}
