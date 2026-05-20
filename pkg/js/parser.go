// parser.go — Recursive descent parser for JavaScript.
//
// Hand-written parser following the ECMAScript grammar. Produces a typed AST.
// Implements operator precedence via Pratt parsing for expressions,
// and recursive descent for statements and declarations.
package js

import (
	"fmt"
)

// scopeInfo tracks let/const bindings in a block scope.
type scopeInfo struct {
	bindings map[string]bool // names declared with let/const in this scope
	parent   *scopeInfo
}

// Parser produces an AST from a token stream.
type Parser struct {
	tokens            []Token
	pos               int
	errors            []string
	Strict            bool // true when "use strict" is active
	asyncContext      bool // true when parsing inside an async function body
	inWith            bool // true when parsing inside a with statement
	classExtendsStack []bool // stack tracking whether enclosing class has extends

	// Context tracking (depths track nesting levels).
	functionDepth int
	loopDepth     int
	switchDepth   int

	// Label tracking for the current function body.
	labels          map[string]bool // all labels in current function
	iterationLabels map[string]bool // labels on enclosing iterations (for continue label validation)
	switchLabels     map[string]bool // labels on enclosing switch (for break label validation)

	// Scope stack for let/const re-declaration detection.
	scopeStack []*scopeInfo
}

// NewParser creates a parser for the given token stream.
func NewParser(tokens []Token) *Parser {
	return &Parser{tokens: tokens}
}

// Parse parses the token stream and returns the program AST.
func (p *Parser) Parse() (*Program, []string) {
	prog := &Program{}
	p.detectUseStrict(nil) // check for "use strict" at script level
	for p.pos < len(p.tokens) && p.peek().Kind != TokEOF {
		stmt := p.parseStatement()
		if stmt != nil {
			prog.Body = append(prog.Body, stmt)
		}
	}
	return prog, p.errors
}

// detectUseStrict checks for a "use strict" directive at the current position.
// If params is non-nil, also checks for duplicate parameter names (strict mode error).
func (p *Parser) detectUseStrict(params []string) {
	savedPos := p.pos
	// Skip any leading semicolons (empty statements).
	for p.pos < len(p.tokens) && p.peek().Kind == TokSemicolon {
		p.advance()
	}
	// Check for "use strict" expression statement.
	foundUseStrict := false
	if p.peek().Kind == TokString {
		val := p.peek().Value
		if val == "use strict" {
			p.Strict = true
			foundUseStrict = true
			p.advance()
			if p.peek().Kind == TokSemicolon {
				p.advance()
			}
		}
	}
	if !foundUseStrict {
		p.pos = savedPos
	}
	// Check params for duplicates (strict mode error).
	// Must happen after any "use strict" detection so the Strict flag is set.
	if params != nil && p.Strict {
		seen := make(map[string]bool)
		for _, name := range params {
			if seen[name] {
				p.addError("duplicate parameter name '" + name + "' in strict mode")
			}
			seen[name] = true
			// Strict mode: eval, arguments, and await can't be parameter names.
			if name == "eval" || name == "arguments" {
				p.addError("'" + name + "' may not be used as a parameter name in strict mode")
			}
			if name == "await" {
				p.addError("'await' may not be used as a parameter name in strict mode")
			}
		}
	}
}

// --- Token helpers ---

func (p *Parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Kind: TokEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekN(n int) Token {
	if p.pos+n >= len(p.tokens) {
		return Token{Kind: TokEOF}
	}
	return p.tokens[p.pos+n]
}

func (p *Parser) advance() Token {
	tok := p.peek()
	if tok.Kind != TokEOF {
		p.pos++
	}
	return tok
}

func (p *Parser) expect(kind TokenKind) (Token, bool) {
	tok := p.peek()
	if tok.Kind == kind {
		p.pos++
		return tok, true
	}
	p.addError(fmt.Sprintf("expected %s, got %s", kind, tok.Kind))
	return tok, false
}

func (p *Parser) consume(kind TokenKind) Token {
	tok, _ := p.expect(kind)
	return tok
}

func (p *Parser) addError(msg string) {
	p.errors = append(p.errors, fmt.Sprintf("line %d: %s", p.peek().Line, msg))
}

// --- Context tracking helpers ---

func (p *Parser) enterFunction() {
	p.functionDepth++
	// Save current label state for restoration on leave.
}

func (p *Parser) leaveFunction() {
	p.functionDepth--
}

func (p *Parser) inFunction() bool {
	return p.functionDepth > 0
}

func (p *Parser) enterLoop() {
	p.loopDepth++
}

func (p *Parser) leaveLoop() {
	p.loopDepth--
}

func (p *Parser) inLoop() bool {
	return p.loopDepth > 0
}

func (p *Parser) enterSwitch() {
	p.switchDepth++
}

func (p *Parser) leaveSwitch() {
	p.switchDepth--
}

func (p *Parser) inSwitch() bool {
	return p.switchDepth > 0
}

// addLabel registers a label and reports an error if it's a duplicate.
func (p *Parser) addLabel(name string) {
	if p.labels == nil {
		p.labels = make(map[string]bool)
	}
	if p.labels[name] {
		p.addError("Label '" + name + "' has already been declared")
		return
	}
	p.labels[name] = true
}

// addIterationLabel tracks a label attached to a loop.
func (p *Parser) addIterationLabel(name string) {
	if p.iterationLabels == nil {
		p.iterationLabels = make(map[string]bool)
	}
	p.iterationLabels[name] = true
}

// addSwitchLabel tracks a label attached to a switch.
func (p *Parser) addSwitchLabel(name string) {
	if p.switchLabels == nil {
		p.switchLabels = make(map[string]bool)
	}
	p.switchLabels[name] = true
}

// enterScope pushes a new block scope for let/const tracking.
func (p *Parser) enterScope() {
	p.scopeStack = append(p.scopeStack, &scopeInfo{bindings: make(map[string]bool)})
}

// leaveScope pops the current block scope.
func (p *Parser) leaveScope() {
	if len(p.scopeStack) > 0 {
		p.scopeStack = p.scopeStack[:len(p.scopeStack)-1]
	}
}

// declareLetConst registers a let/const binding and reports re-declaration errors.
func (p *Parser) declareLetConst(name string) {
	if len(p.scopeStack) == 0 {
		return
	}
	scope := p.scopeStack[len(p.scopeStack)-1]
	if scope.bindings[name] {
		p.addError("Identifier '" + name + "' has already been declared")
		return
	}
	scope.bindings[name] = true
}

// checkStrictBindingIdentifier reports an error if name is a restricted identifier
// in strict mode (eval, arguments, await).
func (p *Parser) checkStrictBindingIdentifier(name string) {
	if !p.Strict {
		return
	}
	if name == "eval" || name == "arguments" {
		p.addError("'" + name + "' may not be used as a binding identifier in strict mode")
	}
	if name == "await" {
		p.addError("'await' may not be used as a binding identifier in strict mode")
	}
}

// isPropertyName reports whether a token kind can be used as an unquoted
// property name in an object literal. In JavaScript, any IdentifierName
// (including reserved words) can be used as a property name.
func (p *Parser) isPropertyName(k TokenKind) bool {
	switch k {
	case TokIdentifier, TokGet, TokSet,
		TokAsync, TokAwait, TokYield,
		TokIf, TokElse, TokFor, TokWhile, TokDo,
		TokReturn, TokNew, TokThis, TokDelete,
		TokTypeof, TokVoid, TokInstanceof, TokIn,
		TokTry, TokCatch, TokFinally, TokThrow,
		TokSwitch, TokCase, TokDefault, TokBreak, TokContinue,
		TokVar, TokLet, TokConst,
		TokFunction, TokClass, TokSuper,
		TokImport, TokExport,
		TokTrue, TokFalse, TokNull:
		return true
	}
	return false
}

// --- Statement parsing ---

func (p *Parser) parseStatement() Node {
	switch p.peek().Kind {
	case TokSemicolon:
		p.advance()
		return &EmptyStatement{}
	case TokLBrace:
		p.enterScope()
		block := p.parseBlockStatement()
		p.leaveScope()
		return block
	case TokVar, TokLet, TokConst:
		return p.parseVariableDeclaration()
	case TokIf:
		return p.parseIfStatement()
	case TokWhile:
		return p.parseWhileStatement()
	case TokDo:
		return p.parseDoWhileStatement()
	case TokFor:
		return p.parseForStatement()
	case TokSwitch:
		return p.parseSwitchStatement()
	case TokTry:
		return p.parseTryStatement()
	case TokBreak:
		return p.parseBreakStatement()
	case TokContinue:
		return p.parseContinueStatement()
	case TokReturn:
		return p.parseReturnStatement()
	case TokFunction:
		return p.parseFunctionDeclaration()
	case TokAsync:
		// async function declaration: async function foo() {}
		if p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == TokFunction {
			return p.parseFunctionDeclaration()
		}
		// Otherwise treat as expression statement (await as identifier outside async context)
		expr := p.parseExpression()
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}
		return &ExpressionStatement{Expression: expr}
	case TokClass:
		return p.parseClassDeclaration()
	case TokImport:
		// import() is a dynamic import expression, not a declaration.
		if p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == TokLParen {
			expr := p.parseExpression()
			if p.peek().Kind == TokSemicolon {
				p.advance()
			}
			return &ExpressionStatement{Expression: expr}
		}
		return p.parseImportDeclaration()
	case TokExport:
		return p.parseExportDeclaration()
	case TokWith:
		return p.parseWithStatement()
	case TokThrow:
		return p.parseThrowStatement()
	case TokIdentifier, TokYield:
		// Check for label: Identifier : Statement
		// Check for label: yield : Statement (yield is only a keyword in generator context)
		if p.peekN(1).Kind == TokColon {
			return p.parseLabeledStatement()
		}
		expr := p.parseExpression()
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}
		return &ExpressionStatement{Expression: expr}
	default:
		expr := p.parseExpression()
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}
		return &ExpressionStatement{Expression: expr}
	}
}

func (p *Parser) parseBlockStatement() *BlockStatement {
	p.consume(TokLBrace)
	block := &BlockStatement{}
	for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Body = append(block.Body, stmt)
		}
	}
	p.consume(TokRBrace)
	return block
}

func (p *Parser) parseVariableDeclaration() Node {
	kind := "var"
	switch p.peek().Kind {
	case TokVar:
		kind = "var"
	case TokLet:
		kind = "let"
	case TokConst:
		kind = "const"
	}
	p.advance()

	// Destructuring: var {a, b} = expr or var [x, y] = expr
	if p.peek().Kind == TokLBrace {
		return p.parseObjectDestructuring(kind)
	}
	if p.peek().Kind == TokLBracket {
		return p.parseArrayDestructuring(kind)
	}

	nameTok := p.peek()
	// Allow keywords that are valid identifiers in certain contexts (await, yield, async, etc.).
	// In strict mode, await may not be used as an identifier.
	if nameTok.Kind == TokAwait && p.Strict && !p.asyncContext {
		p.addError("'await' may not be used as a binding identifier in strict mode")
		p.advance()
		p.sync()
		return nil
	}
	validName := nameTok.Kind == TokIdentifier ||
		(nameTok.Kind == TokAwait && !p.asyncContext && !p.Strict) ||
		nameTok.Kind == TokYield
	if !validName {
		p.addError("expected variable name")
		p.sync()
		return nil
	}
	p.advance()

	p.checkStrictBindingIdentifier(nameTok.Value)

	// Check for let/const re-declaration in the same scope.
	if kind == "let" || kind == "const" {
		p.declareLetConst(nameTok.Value)
	}

	decl := &VariableDeclaration{Kind: kind, Name: nameTok.Value}

	if p.peek().Kind == TokEq {
		p.advance()
		decl.Init = p.parseExpression()
	}

	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return decl
}

// parseObjectDestructuring parses var {a, b} = expr constructs.
// Supports:
//   {a}            — shorthand (source key = target variable)
//   {a: b}         — rename (source key ≠ target variable)
//   {a: {b, c}}    — nested object pattern
//   {a: [b, c]}    — nested array pattern
//   {a = 1}        — default value
//   {a: b = 1}     — rename with default
//   {...rest}      — rest element
func (p *Parser) parseObjectDestructuring(kind string) Node {
	p.consume(TokLBrace)
	var elements []DestructuringElement
	for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
		elem := DestructuringElement{}

		// Rest element: {...rest}
		if p.peek().Kind == TokDotDotDot {
			p.advance()
			if p.peek().Kind != TokIdentifier && !p.isPropertyName(p.peek().Kind) {
				p.addError("expected identifier after ... in destructuring")
				p.sync()
				return nil
			}
			elem.Key = p.advance().Value
			elem.Rest = true
			elements = append(elements, elem)
			break // rest must be the last element
		}

		// Property name: identifier, string, or computed [expr]
		if p.peek().Kind == TokIdentifier || p.peek().Kind == TokString || p.isPropertyName(p.peek().Kind) {
			sourceKey := p.advance().Value

			// Check for : (rename or nested pattern)
			if p.peek().Kind == TokColon {
				p.advance()
				// Nested object pattern: {key: {...}}
				if p.peek().Kind == TokLBrace {
					elem.SourceKey = sourceKey
					nested := p.parseObjectDestructuring(kind)
					if da, ok := nested.(*DestructuringAssignment); ok {
						elem.Nested = da
					}
				} else if p.peek().Kind == TokLBracket {
					// Nested array pattern: {key: [...]}
					elem.SourceKey = sourceKey
					nested := p.parseArrayDestructuring(kind)
					if da, ok := nested.(*DestructuringAssignment); ok {
						elem.Nested = da
					}
				} else {
					// Rename: {key: targetVar}
					elem.SourceKey = sourceKey
					if p.peek().Kind != TokIdentifier && !p.isPropertyName(p.peek().Kind) {
						p.addError("expected identifier after : in destructuring")
						p.sync()
						return nil
					}
					elem.Key = p.advance().Value
				}
			} else {
				// Shorthand: {key} → SourceKey="", Key=sourceKey
				elem.Key = sourceKey
			}
		} else if p.peek().Kind == TokLBracket {
			// Computed property key: {[expr]: target}
			p.advance()
			_ = p.parseExpression() // compute the key expression (ignored for now, future improvement)
			p.consume(TokRBracket)
			p.consume(TokColon)
			if p.peek().Kind != TokIdentifier && !p.isPropertyName(p.peek().Kind) {
				p.addError("expected identifier after computed key in destructuring")
				p.sync()
				return nil
			}
			elem.Key = p.advance().Value
		} else {
			p.addError("expected property name in destructuring")
			p.sync()
			return nil
		}

		// Default value: {key = defaultValue}
		if p.peek().Kind == TokEq {
			p.advance()
			elem.Default = p.parseExpression()
		}

		elements = append(elements, elem)

		if p.peek().Kind != TokComma {
			break
		}
		p.advance()
	}
	p.consume(TokRBrace)

	var right Node
	if p.peek().Kind == TokEq {
		p.advance()
		right = p.parseExpression()
	}
	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return &DestructuringAssignment{Kind: kind, Elements: elements, Right: right}
}

// parseArrayDestructuring parses var [x, y] = expr constructs.
// Supports:
//   [a]            — single element
//   [a, , b]       — elision (holes)
//   [[a, b], c]    — nested array pattern
//   [{x, y}, z]    — nested object pattern
//   [a = 1]        — default value
//   [a, ...rest]   — rest element
func (p *Parser) parseArrayDestructuring(kind string) Node {
	p.consume(TokLBracket)
	var elements []DestructuringElement
	for p.peek().Kind != TokRBracket && p.peek().Kind != TokEOF {
		elem := DestructuringElement{}

		// Rest element: [...rest]
		if p.peek().Kind == TokDotDotDot {
			p.advance()
			if p.peek().Kind != TokIdentifier && !p.isPropertyName(p.peek().Kind) {
				p.addError("expected identifier after ... in array destructuring")
				p.sync()
				return nil
			}
			elem.Key = p.advance().Value
			elem.Rest = true
			elements = append(elements, elem)
			break // rest must be the last element
		}

		// Nested array pattern: [[a, b], ...]
		if p.peek().Kind == TokLBracket {
			nested := p.parseArrayDestructuring(kind)
			if da, ok := nested.(*DestructuringAssignment); ok {
				elem.Nested = da
			}
			elements = append(elements, elem)

			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
			continue
		}

		// Nested object pattern: [{x, y}, ...]
		if p.peek().Kind == TokLBrace {
			nested := p.parseObjectDestructuring(kind)
			if da, ok := nested.(*DestructuringAssignment); ok {
				elem.Nested = da
			}
			elements = append(elements, elem)

			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
			continue
		}

		if p.peek().Kind == TokIdentifier || p.isPropertyName(p.peek().Kind) {
			elem.Key = p.advance().Value
			// Default value: [x = defaultValue]
			if p.peek().Kind == TokEq {
				p.advance()
				elem.Default = p.parseExpression()
			}
		} else if p.peek().Kind == TokComma {
			// Elision: empty slot
			elements = append(elements, elem) // empty element (key="")
			p.advance()
			continue
		} else {
			p.addError("expected identifier in array destructuring")
			p.sync()
			return nil
		}
		elements = append(elements, elem)

		if p.peek().Kind != TokComma {
			break
		}
		p.advance()
	}
	p.consume(TokRBracket)

	var right Node
	if p.peek().Kind == TokEq {
		p.advance()
		right = p.parseExpression()
	}
	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return &DestructuringAssignment{Kind: kind, Elements: elements, Right: right, ArrayMode: true}
}

// tryConvertToDestructuringTarget converts an ArrayExpression or ObjectExpression
// into a DestructuringAssignment if it represents a valid destructuring pattern.
// This handles assignment destructuring: [a, b] = arr, ({x, y} = obj).
// Returns unchanged node if conversion is not possible.
func (p *Parser) tryConvertToDestructuringTarget(left Node) Node {
	switch n := left.(type) {
	case *ArrayExpression:
		return p.arrayExprToDestructuring(n)
	case *ObjectExpression:
		return p.objectExprToDestructuring(n)
	}
	return left
}

// arrayExprToDestructuring converts an ArrayExpression to a DestructuringAssignment (array mode).
func (p *Parser) arrayExprToDestructuring(arr *ArrayExpression) Node {
	var elements []DestructuringElement
	for _, el := range arr.Elements {
		elem := DestructuringElement{}
		if el == nil {
			// Elision: empty slot
			elements = append(elements, elem)
			continue
		}
		switch e := el.(type) {
		case *Identifier:
			elem.Key = e.Name
		case *ArrayExpression:
			nested := p.arrayExprToDestructuring(e)
			if da, ok := nested.(*DestructuringAssignment); ok {
				elem.Nested = da
			}
		case *ObjectExpression:
			nested := p.objectExprToDestructuring(e)
			if da, ok := nested.(*DestructuringAssignment); ok {
				elem.Nested = da
			}
		case *SpreadExpression:
			if ident, ok := e.Argument.(*Identifier); ok {
				elem.Key = ident.Name
				elem.Rest = true
			}
		default:
			// Non-pattern element (e.g., computed expression) — not a valid destructuring target.
			return arr
		}
		elements = append(elements, elem)
	}
	return &DestructuringAssignment{Kind: "var", Elements: elements, ArrayMode: true}
}

// objectExprToDestructuring converts an ObjectExpression to a DestructuringAssignment (object mode).
func (p *Parser) objectExprToDestructuring(obj *ObjectExpression) Node {
	var elements []DestructuringElement
	for _, prop := range obj.Properties {
		elem := DestructuringElement{}
		if prop.Key == "..." {
			// Spread property: {...rest}
			if ident, ok := prop.Value.(*SpreadExpression); ok {
				if innerIdent, ok := ident.Argument.(*Identifier); ok {
					elem.Key = innerIdent.Name
					elem.Rest = true
					elements = append(elements, elem)
					break
				}
			}
			return obj // not a valid destructuring pattern
		}
		if prop.Shorthand {
			// Shorthand: {x} → DestructuringElement{Key: "x"}
			elem.Key = prop.Key
		} else if prop.Computed {
			// Computed property keys are not valid in destructuring targets.
			return obj
		} else {
			// Non-shorthand: {key: value}
			// Value must be an identifier (rename), array (nested array), or object (nested object).
			switch v := prop.Value.(type) {
			case *Identifier:
				elem.SourceKey = prop.Key
				elem.Key = v.Name
			case *ArrayExpression:
				elem.SourceKey = prop.Key
				nested := p.arrayExprToDestructuring(v)
				if da, ok := nested.(*DestructuringAssignment); ok {
					elem.Nested = da
				} else {
					return obj
				}
			case *ObjectExpression:
				elem.SourceKey = prop.Key
				nested := p.objectExprToDestructuring(v)
				if da, ok := nested.(*DestructuringAssignment); ok {
					elem.Nested = da
				} else {
					return obj
				}
			default:
				// Value is not a valid destructuring target (e.g., literal)
				return obj
			}
		}
		elements = append(elements, elem)
	}
	return &DestructuringAssignment{Kind: "var", Elements: elements, ArrayMode: false}
}

func (p *Parser) parseIfStatement() *IfStatement {
	p.consume(TokIf)
	p.consume(TokLParen)
	test := p.parseExpression()
	p.consume(TokRParen)
	consequent := p.parseStatement()

	var alternate Node
	if p.peek().Kind == TokElse {
		p.advance()
		alternate = p.parseStatement()
	}

	return &IfStatement{Test: test, Consequent: consequent, Alternate: alternate}
}

func (p *Parser) parseWhileStatement() *WhileStatement {
	p.consume(TokWhile)
	p.consume(TokLParen)
	test := p.parseExpression()
	p.consume(TokRParen)
	p.enterLoop()
	body := p.parseStatement()
	p.leaveLoop()
	return &WhileStatement{Test: test, Body: body}
}

func (p *Parser) parseDoWhileStatement() *DoWhileStatement {
	p.consume(TokDo)
	p.enterLoop()
	body := p.parseStatement()
	p.leaveLoop()
	p.consume(TokWhile)
	p.consume(TokLParen)
	test := p.parseExpression()
	p.consume(TokRParen)
	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return &DoWhileStatement{Test: test, Body: body}
}

func (p *Parser) parseForStatement() Node {
	p.consume(TokFor)
	p.consume(TokLParen)

	// Check for for-in and for-of loops.
	var init Node
	if p.peek().Kind == TokVar || p.peek().Kind == TokLet || p.peek().Kind == TokConst {
		init = p.parseVariableDeclaration()
		// Support comma-separated variable declarations: for (let a = 1, b = 2; ...)
		for p.peek().Kind == TokComma {
			p.advance()
			nameTok := p.peek()
			validName := nameTok.Kind == TokIdentifier ||
				(nameTok.Kind == TokAwait && !p.asyncContext && !p.Strict) ||
				nameTok.Kind == TokYield
			if !validName {
				p.addError("expected variable name")
				break
			}
			p.advance()
			p.checkStrictBindingIdentifier(nameTok.Value)
			if p.peek().Kind == TokEq {
				p.advance()
				p.parseExpression() // parse and discard init expression
			}
		}
		// If we consumed extra declarations, the ; hasn't been consumed yet.
		// Consume it now so the for-loop code below proceeds correctly.
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}
	} else if p.peek().Kind != TokSemicolon {
		init = &ExpressionStatement{Expression: p.parseExpression()}
		p.consume(TokSemicolon)
	} else {
		p.consume(TokSemicolon)
	}

	if p.peek().Kind == TokIn {
		p.advance()
		right := p.parseExpression()
		p.consume(TokRParen)
		p.enterLoop()
		body := p.parseStatement()
		p.leaveLoop()
		return &ForInStatement{Left: init, Right: right, Body: body}
	}

	if p.peek().Kind == TokOf {
		p.advance()
		right := p.parseExpression()
		p.consume(TokRParen)
		p.enterLoop()
		body := p.parseStatement()
		p.leaveLoop()
		return &ForOfStatement{Left: init, Right: right, Body: body}
	}

	var test Node
	if p.peek().Kind != TokSemicolon {
		test = p.parseExpression()
	}
	p.consume(TokSemicolon)
	var update Node
	if p.peek().Kind != TokRParen {
		update = p.parseExpression()
	}
	p.consume(TokRParen)
	p.enterLoop()
	body := p.parseStatement()
	p.leaveLoop()
	return &ForStatement{Init: init, Test: test, Update: update, Body: body}
}

func (p *Parser) parseSwitchStatement() *SwitchStatement {
	p.consume(TokSwitch)
	p.consume(TokLParen)
	discriminant := p.parseExpression()
	p.consume(TokRParen)
	p.consume(TokLBrace)

	p.enterSwitch()
	sw := &SwitchStatement{Discriminant: discriminant}
	var defaultCase *SwitchCase

	for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
		if p.peek().Kind == TokCase {
			p.advance()
			test := p.parseExpression()
			p.consume(TokColon)
			sc := SwitchCase{Test: test}
			for p.peek().Kind != TokCase && p.peek().Kind != TokDefault && p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
				sc.Consequent = append(sc.Consequent, p.parseStatement())
			}
			sw.Cases = append(sw.Cases, sc)
		} else if p.peek().Kind == TokDefault {
			p.advance()
			p.consume(TokColon)
			defaultCase = &SwitchCase{}
			for p.peek().Kind != TokCase && p.peek().Kind != TokDefault && p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
				defaultCase.Consequent = append(defaultCase.Consequent, p.parseStatement())
			}
		} else {
			p.advance()
		}
	}

	if defaultCase != nil {
		sw.Cases = append(sw.Cases, *defaultCase)
	}
	p.leaveSwitch()
	p.consume(TokRBrace)
	return sw
}

func (p *Parser) parseTryStatement() *TryStatement {
	p.consume(TokTry)
	block := p.parseBlockStatement()

	ts := &TryStatement{Block: block}

	if p.peek().Kind == TokCatch {
		p.advance()
		if p.peek().Kind == TokLParen {
			p.advance()
			if p.peek().Kind == TokLBrace {
				// Catch clause destructuring: catch ({a, b}) { ... }
				da := p.parseObjectDestructuring("var")
				if daObj, ok := da.(*DestructuringAssignment); ok {
					ts.CatchDestructure = daObj
				}
			} else if p.peek().Kind == TokLBracket {
				// Catch clause destructuring: catch ([a, b]) { ... }
				da := p.parseArrayDestructuring("var")
				if daObj, ok := da.(*DestructuringAssignment); ok {
					ts.CatchDestructure = daObj
				}
			} else if p.peek().Kind == TokIdentifier {
				ts.CatchParam = p.advance().Value
				p.checkStrictBindingIdentifier(ts.CatchParam)
			}
			p.consume(TokRParen)
		}
		ts.Handler = p.parseBlockStatement()
	}

	if p.peek().Kind == TokFinally {
		p.advance()
		ts.Finalizer = p.parseBlockStatement()
	}

	return ts
}

func (p *Parser) parseReturnStatement() *ReturnStatement {
	p.consume(TokReturn)
	if !p.inFunction() {
		p.addError("Illegal return statement")
	}
	var arg Node
	if p.peek().Kind != TokSemicolon && p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
		arg = p.parseExpression()
	}
	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return &ReturnStatement{Argument: arg}
}

func (p *Parser) parseBreakStatement() *BreakStatement {
	p.consume(TokBreak)
	var label string
	if p.peek().Kind == TokIdentifier || p.peek().Kind == TokYield {
		label = p.advance().Value
	}
	if !p.inLoop() && !p.inSwitch() {
		p.addError("Illegal break statement")
	}
	if label != "" {
		// Check that the label exists on an enclosing iteration or switch.
		inIter := p.iterationLabels != nil && p.iterationLabels[label]
		inSw := p.switchLabels != nil && p.switchLabels[label]
		if !inIter && !inSw {
			p.addError("Undefined label '" + label + "'")
		}
	}
	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return &BreakStatement{}
}

func (p *Parser) parseContinueStatement() *ContinueStatement {
	p.consume(TokContinue)
	var label string
	if p.peek().Kind == TokIdentifier || p.peek().Kind == TokYield {
		label = p.advance().Value
	}
	if !p.inLoop() {
		p.addError("Illegal continue statement")
	}
	if label != "" {
		// For continue with a label, the label must reference an iteration statement.
		if p.iterationLabels == nil || !p.iterationLabels[label] {
			p.addError("Undefined label '" + label + "'")
		}
	}
	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return &ContinueStatement{}
}

// parseLabeledStatement parses LabelIdentifier : Statement
func (p *Parser) parseLabeledStatement() Node {
	nameTok := p.advance() // consume the label identifier (or yield)
	p.consume(TokColon)    // consume :
	label := nameTok.Value
	p.addLabel(label)

	// Look ahead to determine if the labeled statement is a loop or switch.
	// Register the label in iterationLabels/switchLabels BEFORE parsing the body
	// so that break/continue statements inside the body can reference it.
	kind := p.peek().Kind
	if kind == TokWhile || kind == TokDo || kind == TokFor {
		p.addIterationLabel(label)
	} else if kind == TokSwitch {
		p.addSwitchLabel(label)
	}

	stmt := p.parseStatement()
	return &LabeledStatement{Label: label, Body: stmt}
}

func (p *Parser) parseFunctionDeclaration() *FunctionDeclaration {
	async := false
	if p.peek().Kind == TokAsync {
		async = true
		p.advance()
	}
	p.consume(TokFunction)
	generator := false
	if p.peek().Kind == TokStar {
		generator = true
		p.advance()
	}
	name := ""
	if p.peek().Kind == TokIdentifier || (p.peek().Kind == TokAwait && !p.asyncContext && !p.Strict) {
		name = p.advance().Value
		p.checkStrictBindingIdentifier(name)
	}
	p.consume(TokLParen)
	params := p.parseFormalParameters()
	p.consume(TokRParen)

	// Detect strict mode inside function body before parsing it.
	savedStrict := p.Strict
	savedAsync := p.asyncContext
	p.asyncContext = async
	savedPos := p.pos
	if p.peek().Kind == TokLBrace {
		p.advance() // skip {
		p.detectUseStrict(paramNames(params))
		p.pos = savedPos // rewind to parse block normally
	}

	// Push function context.
	p.enterFunction()
	// Save and reset label state for the new function.
	savedLabels := p.labels
	savedIterLabels := p.iterationLabels
	savedSwitchLabels := p.switchLabels
	p.labels = nil
	p.iterationLabels = nil
	p.switchLabels = nil

	body := p.parseBlockStatement()

	// Restore outer context.
	p.labels = savedLabels
	p.iterationLabels = savedIterLabels
	p.switchLabels = savedSwitchLabels
	p.leaveFunction()
	p.Strict = savedStrict
	p.asyncContext = savedAsync
	return &FunctionDeclaration{Name: name, Params: params, Body: body, Generator: generator, Async: async}
}

func (p *Parser) parseClassDeclaration() *ClassDeclaration {
	p.consume(TokClass)

	// Class declarations are not allowed inside with statements (strict mode).
	if p.inWith {
		p.addError("class declaration not allowed inside with statement in strict mode")
	}

	name := ""
	if p.peek().Kind == TokIdentifier {
		name = p.advance().Value
	}
	hasExtends := false
	var extendsExpr Node
	if p.peek().Kind == TokExtends {
		p.advance() // extends
		hasExtends = true
		extendsExpr = p.parseExpression() // super class
	}
	p.consume(TokLBrace)

	// Push extends state for super-in-non-derived check.
	p.classExtendsStack = append(p.classExtendsStack, hasExtends)

	decl := &ClassDeclaration{Name: name, HasExtends: hasExtends, ExtendsExpr: extendsExpr}
	seenMethods := make(map[string]bool) // tracks regular method names
	seenGetters := make(map[string]bool)  // tracks getter names
	seenSetters := make(map[string]bool)  // tracks setter names

	for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
		// static { ... } — static initialization block (ES2022).
		if p.peek().Kind == TokIdentifier && p.peek().Value == "static" && p.peekN(1).Kind == TokLBrace {
			p.advance() // consume 'static'
			p.advance() // consume '{'
			var stmts []Node
			for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
				stmt := p.parseStatement()
				if stmt != nil {
					stmts = append(stmts, stmt)
				}
			}
			p.consume(TokRBrace)
			decl.StaticBlocks = append(decl.StaticBlocks, stmts)
			continue
		}

		method := p.parseClassMethod()

		// Skip field declarations (#field = value or #field;) — not methods.
		if method.Body == nil {
			continue
		}

		// Reject 'arguments' and 'eval' as method names (strict mode).
		if !method.Computed && (method.Name == "arguments" || method.Name == "eval") {
			p.addError("'" + method.Name + "' may not be used as a method name in strict mode")
		}

		// Check for duplicate method names (get/set pairs with same name are allowed).
		// Private methods (#foo) don't conflict with public methods (foo).
		if !method.Computed && method.Name != "" && method.Name != "constructor" && !method.IsPrivate {
			if method.Getter {
				if seenGetters[method.Name] {
					p.addError("duplicate getter method '" + method.Name + "' in class")
				}
				// Getter conflicts with regular method of same name.
				if seenMethods[method.Name] {
					p.addError("duplicate method '" + method.Name + "' in class")
				}
				seenGetters[method.Name] = true
				// setter pair is allowed: get x() + set x(v)
			} else if method.Setter {
				if seenSetters[method.Name] {
					p.addError("duplicate setter method '" + method.Name + "' in class")
				}
				// Setter conflicts with regular method of same name.
				if seenMethods[method.Name] {
					p.addError("duplicate method '" + method.Name + "' in class")
				}
				seenSetters[method.Name] = true
				// getter pair is allowed: set x(v) + get x()
			} else {
				// Regular method conflicts with getter or setter of same name.
				if seenMethods[method.Name] || seenGetters[method.Name] || seenSetters[method.Name] {
					p.addError("duplicate method '" + method.Name + "' in class")
				}
				seenMethods[method.Name] = true
			}
		}

		if method.Name == "constructor" {
			decl.Constructor = &method
		} else {
			decl.Methods = append(decl.Methods, method)
		}
	}
	p.consume(TokRBrace)

	// Pop extends state.
	p.classExtendsStack = p.classExtendsStack[:len(p.classExtendsStack)-1]
	return decl
}

func (p *Parser) parseClassMethod() ClassMethod {
	method := ClassMethod{Name: ""}

	// Optional "static" keyword.
	if p.peek().Kind == TokIdentifier && p.peek().Value == "static" {
		p.advance()
		method.Static = true
	}

	// Check for getter/setter: get foo() or set foo(v) or get #foo() or set #foo(v).
	if p.peek().Kind == TokGet && p.peekN(1).Kind != TokLParen {
		p.advance()
		method.Getter = true
	} else if p.peek().Kind == TokSet && p.peekN(1).Kind != TokLParen {
		p.advance()
		method.Setter = true
	}

	// Private method or getter/setter: #identifier or get #identifier or set #identifier.
	// Also handles private field declarations (#identifier = value or #identifier;).
	if p.peek().Kind == TokHash {
		p.advance() // consume #
		if p.peek().Kind != TokIdentifier {
			p.addError("expected private name after #")
			return method
		}
		method.Name = p.advance().Value
		method.IsPrivate = true
		// If the private name is followed by '=', it's a field declaration, not a method.
		// Private fields are stored as properties with '#' prefix; we skip the initializer
		// here since field initialization requires a different compiler pipeline.
		if p.peek().Kind == TokEq || p.peek().Kind == TokSemicolon {
			// Field declaration: #field = value; or #field;
			// Consume the initializer expression and semicolon if present.
			if p.peek().Kind == TokEq {
				p.advance()              // consume =
				p.parseExpression()      // skip initializer
			}
			if p.peek().Kind == TokSemicolon {
				p.advance() // consume ;
			}
			// Return method with empty body so the caller knows to skip it.
			method.Body = nil
			return method
		}
	} else if p.peek().Kind == TokIdentifier {
		method.Name = p.advance().Value
	} else if p.peek().Kind == TokString {
		method.Name = p.advance().Value
	} else if p.peek().Kind == TokLBracket {
		// Computed method name: [expr]() { ... }
		p.advance()
		method.ComputedKey = p.parseExpression()
		method.Computed = true
		p.consume(TokRBracket)
	}

	p.consume(TokLParen)
	// Class methods are always in strict mode; set Strict before parsing
	// parameters so duplicate param checks apply.
	savedStrict := p.Strict
	p.Strict = true
	params := p.parseFormalParameters()
	method.Params = params
	p.consume(TokRParen)

	p.enterFunction()
	savedLabels := p.labels
	savedIterLabels := p.iterationLabels
	savedSwitchLabels := p.switchLabels
	p.labels = nil
	p.iterationLabels = nil
	p.switchLabels = nil

	body := p.parseBlockStatement()

	p.labels = savedLabels
	p.iterationLabels = savedIterLabels
	p.switchLabels = savedSwitchLabels
	p.leaveFunction()
	p.Strict = savedStrict
	method.Body = body

	return method
}

// isInDerivedClass returns true if we're currently inside a class with an extends clause.
func (p *Parser) isInDerivedClass() bool {
	if len(p.classExtendsStack) == 0 {
		return false
	}
	return p.classExtendsStack[len(p.classExtendsStack)-1]
}

func (p *Parser) parseWithStatement() Node {
	p.consume(TokWith)
	if p.Strict {
		p.addError("'with' statement is not allowed in strict mode")
	}
	p.consume(TokLParen)
	p.parseExpression()
	p.consume(TokRParen)

	prevInWith := p.inWith
	p.inWith = true
	body := p.parseStatement()
	p.inWith = prevInWith
	return body
}

func (p *Parser) parseThrowStatement() Node {
	p.consume(TokThrow)
	expr := p.parseExpression()
	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return &ThrowStatement{Argument: expr}
}

// --- Import/Export parsing ---

func (p *Parser) parseImportDeclaration() Node {
	p.consume(TokImport)

	decl := &ImportDeclaration{}

	switch {
	case p.peek().Kind == TokString:
		// import './module.js' — side-effect only, no bindings.
		decl.Source = p.advance().Value

	case p.peek().Kind == TokStar:
		// import * as name from '...'
		p.advance() // *
		if p.peek().Kind == TokAs {
			p.advance() // as
		}
		localName := p.peek().Value
		p.consume(TokIdentifier)
		decl.Specifiers = append(decl.Specifiers, ImportSpecifier{Local: localName, IsNamespace: true})

		if p.peek().Kind == TokFrom {
			p.advance() // from
		}
		if p.peek().Kind == TokString {
			decl.Source = p.advance().Value
		}

	case p.peek().Kind == TokLBrace:
		// import { foo, bar as baz } from '...'
		p.advance() // {
		for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
			imported := p.peek().Value
			p.consume(TokIdentifier)
			local := imported
			if p.peek().Kind == TokAs {
				p.advance() // as
				local = p.peek().Value
				p.consume(TokIdentifier)
			}
			decl.Specifiers = append(decl.Specifiers, ImportSpecifier{Local: local, Imported: imported})
			if p.peek().Kind == TokComma {
				p.advance()
			}
		}
		p.consume(TokRBrace)

		if p.peek().Kind == TokFrom {
			p.advance() // from
		}
		if p.peek().Kind == TokString {
			decl.Source = p.advance().Value
		}

	case p.peek().Kind == TokIdentifier:
		// import foo from '...' — default import
		localName := p.peek().Value
		p.consume(TokIdentifier)
		decl.Specifiers = append(decl.Specifiers, ImportSpecifier{Local: localName, IsDefault: true})

		if p.peek().Kind == TokComma {
			p.advance() // ,
			// Could be followed by * as ns from or { named } from
			if p.peek().Kind == TokStar {
				p.advance() // *
				if p.peek().Kind == TokAs {
					p.advance() // as
				}
				nsName := p.peek().Value
				p.consume(TokIdentifier)
				decl.Specifiers = append(decl.Specifiers, ImportSpecifier{Local: nsName, IsNamespace: true})
			} else if p.peek().Kind == TokLBrace {
				p.advance() // {
				for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
					imported := p.peek().Value
					p.consume(TokIdentifier)
					local := imported
					if p.peek().Kind == TokAs {
						p.advance() // as
						local = p.peek().Value
						p.consume(TokIdentifier)
					}
					decl.Specifiers = append(decl.Specifiers, ImportSpecifier{Local: local, Imported: imported})
					if p.peek().Kind == TokComma {
						p.advance()
					}
				}
				p.consume(TokRBrace)
			}
		}

		if p.peek().Kind == TokFrom {
			p.advance() // from
		}
		if p.peek().Kind == TokString {
			decl.Source = p.advance().Value
		}
	}

	if p.peek().Kind == TokSemicolon {
		p.advance()
	}
	return decl
}

func (p *Parser) parseExportDeclaration() Node {
	p.consume(TokExport)

	decl := &ExportDeclaration{}

	switch {
	case p.peek().Kind == TokDefault:
		// export default <expr|function|class>
		p.advance() // default
		decl.IsDefault = true
		switch p.peek().Kind {
		case TokFunction:
			decl.Declaration = p.parseFunctionDeclaration()
		case TokClass:
			decl.Declaration = p.parseClassDeclaration()
		default:
			// export default <expression>
			decl.Declaration = &ExpressionStatement{Expression: p.parseExpression()}
			if p.peek().Kind == TokSemicolon {
				p.advance()
			}
		}

	case p.peek().Kind == TokFunction:
		decl.Declaration = p.parseFunctionDeclaration()

	case p.peek().Kind == TokClass:
		decl.Declaration = p.parseClassDeclaration()

	case p.peek().Kind == TokVar, p.peek().Kind == TokLet, p.peek().Kind == TokConst:
		decl.Declaration = p.parseVariableDeclaration()
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}

	case p.peek().Kind == TokLBrace:
		// export { foo, bar as baz } [from '...']
		p.advance() // {
		for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
			local := p.peek().Value
			p.consume(TokIdentifier)
			exported := local
			if p.peek().Kind == TokAs {
				p.advance() // as
				exported = p.peek().Value
				p.consume(TokIdentifier)
			}
			decl.Specifiers = append(decl.Specifiers, ExportSpecifier{Local: local, Exported: exported})
			if p.peek().Kind == TokComma {
				p.advance()
			}
		}
		p.consume(TokRBrace)

		if p.peek().Kind == TokFrom {
			p.advance() // from
			if p.peek().Kind == TokString {
				decl.Source = p.advance().Value
			}
		}
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}

	case p.peek().Kind == TokStar:
		// export * from '...'
		p.advance() // *
		if p.peek().Kind == TokFrom {
			p.advance() // from
		}
		if p.peek().Kind == TokString {
			decl.Source = p.advance().Value
		}
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}

	default:
		// export <expression> (treated as default export shorthand)
		decl.IsDefault = true
		decl.Declaration = &ExpressionStatement{Expression: p.parseExpression()}
		if p.peek().Kind == TokSemicolon {
			p.advance()
		}
	}

	return decl
}

// parseFormalParameters parses function parameters with optional defaults
// and optional destructuring patterns.
func (p *Parser) parseFormalParameters() []DefaultParam {
	var params []DefaultParam
	if p.peek().Kind != TokRParen {
		for {
			tok := p.peek()

			// Rest parameter: ...name
			if tok.Kind == TokDotDotDot {
				p.advance() // consume ...
				next := p.peek()
				if next.Kind == TokIdentifier || (p.isPropertyName(next.Kind) && (next.Kind != TokAwait || p.asyncContext || !p.Strict)) {
					p.advance()
					param := DefaultParam{Name: next.Value, Rest: true}
					if p.peek().Kind == TokEq {
						p.advance()
						param.Default = p.parseExpression()
					}
					params = append(params, param)
					break // rest parameter must be the last one
				}
				break
			}

			// Destructuring pattern in parameter: function f([a, b]) or function f({a, b})
			if tok.Kind == TokLBracket {
				da := p.parseArrayDestructuring("var")
				if da != nil {
					params = append(params, DefaultParam{Destructure: da.(*DestructuringAssignment)})
				}
			} else if tok.Kind == TokLBrace {
				da := p.parseObjectDestructuring("var")
				if da != nil {
					params = append(params, DefaultParam{Destructure: da.(*DestructuringAssignment)})
				}
			} else if tok.Kind == TokIdentifier || (p.isPropertyName(tok.Kind) && (tok.Kind != TokAwait || p.asyncContext || !p.Strict)) {
				p.advance()
				param := DefaultParam{Name: tok.Value}
				if p.peek().Kind == TokEq {
					p.advance()
					param.Default = p.parseExpression()
				}
				params = append(params, param)
			} else {
				break
			}

			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
		}
	}

	// Check for duplicate parameter names in strict mode.
	if p.Strict {
		seen := make(map[string]bool)
		for _, param := range params {
			name := param.Name
			if name == "" {
				continue // destructured params have empty name
			}
			if seen[name] {
				p.addError("duplicate parameter name '" + name + "' in strict mode")
			}
			seen[name] = true
			if name == "eval" || name == "arguments" {
				p.addError("'" + name + "' may not be used as a parameter name in strict mode")
			}
			if name == "await" {
				p.addError("'await' may not be used as a parameter name in strict mode")
			}
		}
	}

	return params
}

// --- Expression parsing (Pratt parser) ---

// Precedence levels (higher = binds tighter).
const (
	precLowest       = 0
	precComma        = 1
	precAssignment   = 2
	precArrow        = 2
	precConditional  = 3
	precLogicalOr    = 4
	precLogicalAnd   = 5
	precBitwiseOr    = 6
	precBitwiseXor   = 7
	precBitwiseAnd   = 8
	precEquality     = 9
	precRelational   = 10
	precShift        = 11
	precAdditive     = 12
	precMultiplicative = 13
	precExponentiation = 14 // right-associative
	precUnary        = 14
	precPostfix      = 15
	precCall         = 16
	precMember       = 17
	precPrimary      = 18
)

func (p *Parser) parseExpression() Node {
	return p.parseExpressionPrecedence(precLowest)
}

func (p *Parser) parseExpressionPrecedence(minPrec int) Node {
	left := p.parsePrefix()

	for {
		tok := p.peek()
		prec := p.infixPrecedence(tok)
		if prec < minPrec {
			break
		}
		left = p.parseInfix(left, tok)
	}

	return left
}

func (p *Parser) parsePrefix() Node {
	tok := p.peek()
	switch tok.Kind {
	case TokNumber:
		p.advance()
		if p.Strict && tok.IsLegacyOctal {
			p.addError("octal literals are not allowed in strict mode")
		}
		return &Literal{Value: NewNumber(tok.NumVal)}
	case TokBigInt:
		p.advance()
		return &Literal{Value: NewBigIntFromString(tok.Value)}
	case TokRegExp:
		p.advance()
		pat, flags := parseRegExpToken(tok.Value)
		return &RegExpExpression{Pattern: pat, Flags: flags}
	case TokString:
		p.advance()
		return &Literal{Value: NewString(tok.Value)}
	case TokTrue:
		p.advance()
		return &Literal{Value: True}
	case TokFalse:
		p.advance()
		return &Literal{Value: False}
	case TokNull:
		p.advance()
		return &Literal{Value: Null}
	case TokUndefined:
		p.advance()
		return &Literal{Value: Undefined}
	case TokIdentifier:
		p.advance()
		return &Identifier{Name: tok.Value}
	case TokThis:
		p.advance()
		return &ThisExpression{}
	case TokSuper:
		p.advance()
		return &SuperExpression{}
	case TokLParen:
		// Check for arrow function: (params) => ...
		return p.parseParenthesizedOrArrow()
	case TokLBracket:
		return p.parseArrayExpression()
	case TokLBrace:
		return p.parseObjectExpression()
	case TokFunction:
		return p.parseFunctionExpression()
	case TokAsync:
		return p.parseAsyncPrefix()
	case TokTemplateStart:
		p.advance()
		return p.parseTemplateLiteral()
	case TokBang, TokMinus, TokPlus, TokTilde, TokTypeof, TokVoid, TokDelete:
		p.advance()
		arg := p.parseExpressionPrecedence(precUnary)
		return &UnaryExpression{Operator: tokenOpString(tok.Kind), Argument: arg}
	case TokPlusPlus, TokMinusMinus:
		p.advance()
		arg := p.parseExpressionPrecedence(precUnary)
		if p.Strict {
			if id, ok := arg.(*Identifier); ok && (id.Name == "eval" || id.Name == "arguments") {
				p.addError("Assignment to '" + id.Name + "' is not allowed in strict mode")
			}
		}
		return &UpdateExpression{Operator: tokenOpString(tok.Kind), Argument: arg, Prefix: true}
	case TokNew:
		return p.parseNewExpression()
	case TokYield:
		return p.parseYieldExpression()
	case TokAwait:
		// Only parse as await expression inside async functions.
		// Outside async context, await is a valid identifier.
		if p.asyncContext {
			return p.parseAwaitExpression()
		}
		p.advance()
		return &Identifier{Name: tok.Value}
	case TokImport:
		// import() — dynamic import expression
		p.advance()
		p.consume(TokLParen)
		source := p.parseExpression()
		p.consume(TokRParen)
		return &ImportExpression{Source: source}
	case TokClass:
		// class expression: class { ... } or class Name { ... }
		return p.parseClassDeclaration()
	case TokHash:
		p.advance()
		if p.peek().Kind == TokIdentifier {
			name := p.advance().Value
			p.addError("private name '#" + name + "' is not allowed outside a class body")
		} else {
			p.addError("'#' not expected")
		}
		return &Literal{Value: Undefined}
	default:
		// Skip unknown token and return a placeholder.
		p.advance()
		return &Literal{Value: Undefined}
	}
}

func (p *Parser) parseInfix(left Node, op Token) Node {
	switch op.Kind {
	case TokPlus, TokMinus, TokStar, TokSlash, TokPercent,
		TokEqEq, TokNotEq, TokEqEqEq, TokNotEqEq,
		TokLt, TokGt, TokLtEq, TokGtEq,
		TokAndAnd, TokPipePipe, TokQuestionQuestion,
		TokAnd, TokPipe, TokCaret, TokLtLt, TokGtGt, TokGtGtGt,
		TokInstanceof, TokIn:
		prec := p.infixPrecedence(op)
		p.advance()
		right := p.parseExpressionPrecedence(prec + 1) // left-associative
		return &BinaryExpression{Operator: tokenOpString(op.Kind), Left: left, Right: right}
	case TokStarStar:
		// Right-associative: parse right at same precedence.
		prec := p.infixPrecedence(op)
		p.advance()
		right := p.parseExpressionPrecedence(prec) // right-associative
		return &BinaryExpression{Operator: "**", Left: left, Right: right}
	case TokEq, TokPlusEq, TokMinusEq, TokStarEq, TokSlashEq, TokPercentEq,
		TokAndAndEq, TokPipePipeEq, TokQuestionQuestionEq:
		assocPrec := p.infixPrecedence(op)
		p.advance()
		right := p.parseExpressionPrecedence(assocPrec)
		// Convert array/object literal to destructuring pattern if it's a valid assignment target.
		left = p.tryConvertToDestructuringTarget(left)
		// In strict mode, assignment to eval or arguments is a SyntaxError.
		if p.Strict {
			if id, ok := left.(*Identifier); ok && (id.Name == "eval" || id.Name == "arguments") {
				p.addError("Assignment to '" + id.Name + "' is not allowed in strict mode")
			}
		}
		return &AssignmentExpression{Operator: tokenOpString(op.Kind), Left: left, Right: right}
	case TokLParen:
		_ = p.infixPrecedence(op)
		// super() call is only valid in derived class constructors.
		if _, ok := left.(*SuperExpression); ok {
			if !p.isInDerivedClass() {
				p.addError("'super()' call is only valid in derived class constructors")
			}
		}
		p.advance()
		var args []Node
		if p.peek().Kind != TokRParen {
			for {
				if p.peek().Kind == TokDotDotDot {
					p.advance()
					args = append(args, &SpreadExpression{Argument: p.parseExpression()})
				} else {
					args = append(args, p.parseExpression())
				}
				if p.peek().Kind != TokComma {
					break
				}
				p.advance()
			}
		}
		p.consume(TokRParen)
		return &CallExpression{Callee: left, Arguments: args}
	case TokQuestion:
		prec := p.infixPrecedence(op)
		p.advance()
		consequent := p.parseExpressionPrecedence(prec - 1)
		p.consume(TokColon)
		alternate := p.parseExpressionPrecedence(prec - 1)
		return &ConditionalExpression{Test: left, Consequent: consequent, Alternate: alternate}
	case TokDot:
		p.advance()
		// super.x in non-derived class is a SyntaxError.
		if _, ok := left.(*SuperExpression); ok {
			if !p.isInDerivedClass() {
				p.addError("'super' property access is only valid in derived classes")
			}
		}
		propTok := p.peek()
		// Private member access: obj.#name
		if propTok.Kind == TokHash {
			p.advance() // consume #
			identTok := p.peek()
			if identTok.Kind != TokIdentifier && identTok.Value == "" {
				p.addError("expected private name after #")
				return left
			}
			p.advance()
			return &PrivateMemberExpression{Object: left, Property: identTok.Value}
		}
		// After ., any token with a string value can be a property name
		// (e.g., Array.of, obj.class, obj.async — keywords become identifiers in dot access).
		if propTok.Kind != TokIdentifier && propTok.Value == "" {
			p.addError("expected property name after .")
			return left
		}
		p.advance()
		return &MemberExpression{Object: left, Property: &Identifier{Name: propTok.Value}, Computed: false}
	case TokLBracket:
		p.advance()
		// super[x] in non-derived class is a SyntaxError.
		if _, ok := left.(*SuperExpression); ok {
			if !p.isInDerivedClass() {
				p.addError("'super' property access is only valid in derived classes")
			}
		}
		prop := p.parseExpression()
		p.consume(TokRBracket)
		return &MemberExpression{Object: left, Property: prop, Computed: true}
	case TokQuestionDot:
		p.advance()
		startPos := op.StartPos
		// Private optional access: obj?.#member
		if p.peek().Kind == TokHash {
			p.advance() // consume #
			identTok := p.peek()
			if identTok.Kind == TokIdentifier {
				p.advance()
			}
			return &OptionalMemberExpression{Object: left, Property: &Identifier{Name: identTok.Value}, Computed: false, StartPos: startPos, EndPos: identTok.EndPos}
		}
		if p.peek().Kind == TokLBracket {
			p.advance()
			prop := p.parseExpression()
			rbTok := p.consume(TokRBracket)
			return &OptionalMemberExpression{Object: left, Property: prop, Computed: true, StartPos: startPos, EndPos: rbTok.EndPos}
		}
		if p.peek().Kind == TokLParen {
			p.advance()
			var args []Node
			if p.peek().Kind != TokRParen {
				for {
					if p.peek().Kind == TokDotDotDot {
						p.advance()
						args = append(args, &SpreadExpression{Argument: p.parseExpression()})
					} else {
						args = append(args, p.parseExpression())
					}
					if p.peek().Kind != TokComma {
						break
					}
					p.advance()
				}
			}
			rpTok := p.consume(TokRParen)
			return &OptionalCallExpression{Callee: left, Arguments: args, StartPos: startPos, EndPos: rpTok.EndPos}
		}
		propTok := p.peek()
		if propTok.Kind != TokIdentifier && propTok.Value == "" {
			p.addError("expected property name after ?.")
			return left
		}
		p.advance()
		prop := &Identifier{Name: propTok.Value}
		if p.peek().Kind == TokLParen {
			// a?.b() — emit OptionalCallExpression with member as callee (ES2020 spec)
			p.advance() // consume (
			var args []Node
			if p.peek().Kind != TokRParen {
				for {
					if p.peek().Kind == TokDotDotDot {
						p.advance()
						args = append(args, &SpreadExpression{Argument: p.parseExpression()})
					} else {
						args = append(args, p.parseExpression())
					}
					if p.peek().Kind != TokComma {
						break
					}
					p.advance()
				}
			}
			rpTok := p.consume(TokRParen)
			return &OptionalCallExpression{
				Callee:    &OptionalMemberExpression{Object: left, Property: prop, Computed: false, StartPos: startPos, EndPos: propTok.EndPos},
				Arguments: args,
				StartPos:  startPos,
				EndPos:    rpTok.EndPos,
			}
		}
		return &OptionalMemberExpression{Object: left, Property: prop, Computed: false, StartPos: startPos, EndPos: propTok.EndPos}
	case TokPlusPlus, TokMinusMinus:
		p.advance()
		if p.Strict {
			if id, ok := left.(*Identifier); ok && (id.Name == "eval" || id.Name == "arguments") {
				p.addError("Assignment to '" + id.Name + "' is not allowed in strict mode")
			}
		}
		return &UpdateExpression{Operator: tokenOpString(op.Kind), Argument: left, Prefix: false}
	case TokArrow:
		// Arrow function: left is either an identifier or a list of params (from parenthesized).
		p.advance()
		var params []DefaultParam
		switch l := left.(type) {
		case *Identifier:
			params = []DefaultParam{{Name: l.Name}}
		default:
			// If left was a parenthesized expression, it should be handled differently.
			// For now, treat as empty params.
		}
		var body Node
		p.enterFunction()
		if p.peek().Kind == TokLBrace {
			body = p.parseBlockStatement()
		} else {
			body = p.parseExpression()
		}
		p.leaveFunction()
		return &ArrowFunctionExpression{Params: params, Body: body}
	case TokTemplateStart:
		// Tagged template: expr`quasi ${...} quasi`
		p.advance()
		tl := p.parseTemplateLiteral().(*TemplateLiteral)
		return &TaggedTemplateExpression{
			Tag:         left,
			Quasis:      tl.Quasis,
			Expressions: tl.Expressions,
		}
	default:
		return left
	}
}

func (p *Parser) parseParenthesizedOrArrow() Node {
	p.consume(TokLParen)
	// Empty parens: () or () =>
	if p.peek().Kind == TokRParen {
		p.consume(TokRParen)
		if p.peek().Kind == TokArrow {
			p.advance()
			var body Node
			p.enterFunction()
			if p.peek().Kind == TokLBrace {
				body = p.parseBlockStatement()
			} else {
				body = p.parseExpression()
			}
			p.leaveFunction()
			return &ArrowFunctionExpression{Body: body}
		}
		return &Literal{Value: Undefined}
	}

	// Try to parse as arrow params: id (, id)* ) =>
	savedPos := p.pos
	var params []DefaultParam

	if p.peek().Kind == TokIdentifier || (p.peek().Kind == TokAwait && !p.asyncContext && !p.Strict) || p.peek().Kind == TokYield {
		name := p.peek().Value
		p.advance()
		p.checkStrictBindingIdentifier(name)
		var def Node
		if p.peek().Kind == TokEq {
			p.advance()
			def = p.parseExpression()
		}
		params = append(params, DefaultParam{Name: name, Default: def})
		for p.peek().Kind == TokComma {
			p.advance()
			if p.peek().Kind == TokIdentifier || (p.peek().Kind == TokAwait && !p.asyncContext && !p.Strict) || p.peek().Kind == TokYield {
				name := p.peek().Value
				p.advance()
				p.checkStrictBindingIdentifier(name)
				var def Node
				if p.peek().Kind == TokEq {
					p.advance()
					def = p.parseExpression()
				}
				params = append(params, DefaultParam{Name: name, Default: def})
			} else {
				// Not valid arrow params: restore and parse as expression.
				p.pos = savedPos
				expr := p.parseExpression()
				p.consume(TokRParen)
				return expr
			}
		}
		if p.peek().Kind == TokRParen {
			p.advance()
			if p.peek().Kind == TokArrow {
				p.advance()
				var body Node
				p.enterFunction()
				if p.peek().Kind == TokLBrace {
					body = p.parseBlockStatement()
				} else {
					body = p.parseExpression()
				}
				p.leaveFunction()
				return &ArrowFunctionExpression{Params: params, Body: body}
			}
			// Parenthesized identifier: return it.
			if len(params) == 1 && params[0].Default == nil {
				return &Identifier{Name: params[0].Name}
			}
		}
	}

	// Not an arrow function: parse as expression.
	p.pos = savedPos
	expr := p.parseExpression()
	p.consume(TokRParen)
	return expr
}

func (p *Parser) parseArrayExpression() Node {
	p.consume(TokLBracket)
	arr := &ArrayExpression{}
	for p.peek().Kind != TokRBracket && p.peek().Kind != TokEOF {
		if p.peek().Kind == TokComma {
			// Elision (hole): consecutive or leading/trailing comma creates a hole.
			arr.Elements = append(arr.Elements, nil)
			p.advance()
			continue
		}
		if p.peek().Kind == TokDotDotDot {
			p.advance()
			arg := p.parseExpression()
			arr.Elements = append(arr.Elements, &SpreadExpression{Argument: arg})
		} else {
			arr.Elements = append(arr.Elements, p.parseExpression())
		}
		// After element or spread, skip comma if present. Trailing comma is allowed.
		if p.peek().Kind == TokComma {
			p.advance()
		}
	}
	p.consume(TokRBracket)
	return arr
}

func (p *Parser) parseObjectExpression() Node {
	p.consume(TokLBrace)
	obj := &ObjectExpression{}
	for p.peek().Kind != TokRBrace && p.peek().Kind != TokEOF {
		// Spread: { ...obj }
		if p.peek().Kind == TokDotDotDot {
			p.advance()
			obj.Properties = append(obj.Properties, ObjectProperty{Key: "...", Value: &SpreadExpression{Argument: p.parseExpression()}})
			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
			continue
		}
		key := ""
		tok := p.peek()
		if tok.Kind == TokString {
			key = tok.Value
			p.advance()
		} else if tok.Kind == TokNumber {
			key = tok.Value
			p.advance()
			if p.Strict && tok.IsLegacyOctal {
				p.addError("octal literals are not allowed in strict mode")
			}
		} else if p.isPropertyName(tok.Kind) {
			key = tok.Value
			p.advance()
		} else if tok.Kind == TokLBracket {
			// Computed property name.
			// Supports: { [expr]: value } and { [expr]() { ... } } (method shorthand).
			p.advance()
			keyExpr := p.parseExpression()
			p.consume(TokRBracket)
			if p.peek().Kind == TokLParen {
				// Computed method shorthand: { [expr]() { ... } }
				p.consume(TokLParen)
				params := p.parseFormalParameters()
				p.consume(TokRParen)
				p.enterFunction()
				body := p.parseBlockStatement()
				p.leaveFunction()
				fn := &FunctionExpression{Params: params, Body: body}
				obj.Properties = append(obj.Properties, ObjectProperty{
					Key:         "",
					Value:       fn,
					Computed:    true,
					ComputedKey: keyExpr,
				})
			} else {
				// Computed property: { [expr]: value }
				p.consume(TokColon)
				val := p.parseExpression()
				obj.Properties = append(obj.Properties, ObjectProperty{
					Key:         "",
					Value:       val,
					Computed:    true,
					ComputedKey: keyExpr,
				})
			}
			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
			continue
		} else {
			p.addError("expected property key")
			p.advance()
			continue
		}
		// Getter/setter in object literal: get foo() { ... } or set foo(v) { ... }
		// Only valid if key is "get"/"set" and next token is a valid property name.
		if (key == "get" || key == "set") && p.isPropertyName(p.peek().Kind) {
			isGetter := key == "get"
			isSetter := key == "set"
			actualKey := p.advance().Value // consume the actual property name
			p.consume(TokLParen)
			params := p.parseFormalParameters()
			p.consume(TokRParen)
			p.enterFunction()
			body := p.parseBlockStatement()
			p.leaveFunction()
			fn := &FunctionExpression{Name: actualKey, Params: params, Body: body}
			obj.Properties = append(obj.Properties, ObjectProperty{Key: actualKey, Value: fn, IsGetter: isGetter, IsSetter: isSetter})
			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
			continue
		}
		// Shorthand: {x} or method: { foo() {} }
		if p.peek().Kind == TokComma || p.peek().Kind == TokRBrace || p.peek().Kind == TokEOF || p.peek().Kind == TokLParen {
			// If followed by (, it's a method shorthand.
			// If followed by , or }, it's a property shorthand.
			if p.peek().Kind == TokLParen {
				// Method shorthand: { foo() { ... } }
				// Treat as a function expression assigned to the key.
				p.consume(TokLParen)
				params := p.parseFormalParameters()
				p.consume(TokRParen)
				p.enterFunction()
				body := p.parseBlockStatement()
				p.leaveFunction()
				fn := &FunctionExpression{Name: key, Params: params, Body: body}
				obj.Properties = append(obj.Properties, ObjectProperty{Key: key, Value: fn})
			} else {
				// Shorthand property: {x} equivalent to {x: x}
				obj.Properties = append(obj.Properties, ObjectProperty{
					Key:       key,
					Value:     &Identifier{Name: key},
					Shorthand: true,
				})
			}
			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
			continue
		}
		p.consume(TokColon)
		val := p.parseExpression()
		obj.Properties = append(obj.Properties, ObjectProperty{Key: key, Value: val})
		if p.peek().Kind != TokComma {
			break
		}
		p.advance()
	}
	p.consume(TokRBrace)
	return obj
}

func (p *Parser) parseFunctionExpression() Node {
	async := false
	if p.peek().Kind == TokAsync {
		async = true
		p.advance()
	}
	p.consume(TokFunction)
	generator := false
	if p.peek().Kind == TokStar {
		generator = true
		p.advance()
	}
	name := ""
	if p.peek().Kind == TokIdentifier || (p.peek().Kind == TokAwait && !p.asyncContext && !p.Strict) {
		name = p.advance().Value
		p.checkStrictBindingIdentifier(name)
	}
	p.consume(TokLParen)
	params := p.parseFormalParameters()
	p.consume(TokRParen)

	savedStrict := p.Strict
	savedAsync := p.asyncContext
	p.asyncContext = async
	savedPos := p.pos
	if p.peek().Kind == TokLBrace {
		p.advance() // skip {
		p.detectUseStrict(paramNames(params))
		p.pos = savedPos
	}

	// Push function context.
	p.enterFunction()
	savedLabels := p.labels
	savedIterLabels := p.iterationLabels
	savedSwitchLabels := p.switchLabels
	p.labels = nil
	p.iterationLabels = nil
	p.switchLabels = nil

	body := p.parseBlockStatement()

	// Restore.
	p.labels = savedLabels
	p.iterationLabels = savedIterLabels
	p.switchLabels = savedSwitchLabels
	p.leaveFunction()
	p.Strict = savedStrict
	p.asyncContext = savedAsync
	return &FunctionExpression{Name: name, Params: params, Body: body, Generator: generator, Async: async}
}

func (p *Parser) parseYieldExpression() Node {
	p.consume(TokYield)
	delegate := false
	if p.peek().Kind == TokStar {
		delegate = true
		p.advance()
	}
	var arg Node
	// yield without argument yields undefined.
	// yield expr or yield* expr: parse the argument.
	if !delegate && (p.peek().Kind == TokSemicolon || p.peek().Kind == TokRBrace || p.peek().Kind == TokEOF) {
		// bare yield; => yield undefined
	} else {
		arg = p.parseExpression()
	}
	return &YieldExpression{Argument: arg, Delegate: delegate}
}

func (p *Parser) parseAsyncPrefix() Node {
	// async function expression: async function [name](params) { body }
	// Delegate to parseFunctionExpression without consuming TokAsync so it
	// can correctly set Async=true and p.asyncContext for await inside the body.
	if p.peek().Kind == TokAsync && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].Kind == TokFunction {
		return p.parseFunctionExpression()
	}
	p.consume(TokAsync)
	// async arrow function: async (params) => body or async param => body
	savedAsync := p.asyncContext
	p.asyncContext = true
	var result Node
	if p.peek().Kind == TokLParen {
		arrow := p.parseParenthesizedOrArrow()
		if ae, ok := arrow.(*ArrowFunctionExpression); ok {
			ae.Async = true
			result = ae
		} else {
			result = arrow
		}
	} else if p.peek().Kind == TokIdentifier || (p.peek().Kind == TokAwait && !p.asyncContext && !p.Strict) || p.peek().Kind == TokYield {
		// async param => body
		name := p.advance().Value
		p.checkStrictBindingIdentifier(name)
		if p.peek().Kind == TokArrow {
			p.advance()
			var body Node
			savedCtx := p.asyncContext
			p.asyncContext = true
			p.enterFunction()
			if p.peek().Kind == TokLBrace {
				body = p.parseBlockStatement()
			} else {
				body = p.parseExpression()
			}
			p.leaveFunction()
			p.asyncContext = savedCtx
			result = &ArrowFunctionExpression{Params: []DefaultParam{{Name: name}}, Body: body, Async: true}
		} else {
			// Not an arrow: async used as identifier reference
			result = p.parseInfix(&Identifier{Name: name}, p.peek())
		}
	} else {
		result = &Literal{Value: Undefined}
	}
	p.asyncContext = savedAsync
	return result
}

func (p *Parser) parseAwaitExpression() Node {
	p.consume(TokAwait)
	arg := p.parseExpressionPrecedence(precUnary)
	return &AwaitExpression{Argument: arg}
}

func (p *Parser) parseNewExpression() Node {
	p.consume(TokNew)
	callee := p.parseExpressionPrecedence(precMember) // higher than call, so Map() isn't consumed
	var args []Node
	if p.peek().Kind == TokLParen {
		p.advance()
		for p.peek().Kind != TokRParen && p.peek().Kind != TokEOF {
			// Check for spread in call args: fn(...args)
			if p.peek().Kind == TokDotDotDot {
				p.advance()
				args = append(args, &SpreadExpression{Argument: p.parseExpression()})
			} else {
				args = append(args, p.parseExpression())
			}
			if p.peek().Kind != TokComma {
				break
			}
			p.advance()
		}
		p.consume(TokRParen)
	}
	return &NewExpression{Callee: callee, Arguments: args}
}

// parseTemplateLiteral parses template literal expressions: `quasi ${expr} quasi`
func (p *Parser) parseTemplateLiteral() Node {
	tl := &TemplateLiteral{}
	for {
		// Expect a quasi string (may be empty).
		if p.peek().Kind == TokString {
			tl.Quasis = append(tl.Quasis, p.advance().Value)
		} else {
			break
		}
		// After a quasi, either we have an expression (followed by TokTemplateExprEnd)
		// or the template ends (TokTemplateEnd).
		if p.peek().Kind == TokTemplateEnd {
			p.advance()
			return tl
		}
		// Parse the interpolated expression.
		expr := p.parseExpression()
		tl.Expressions = append(tl.Expressions, expr)
		// Expect closing } for the ${expr}
		if p.peek().Kind == TokTemplateExprEnd {
			p.advance()
		} else {
			p.addError("expected } to close template expression")
		}
	}
	return tl
}

// --- Precedence helpers ---

func (p *Parser) infixPrecedence(tok Token) int {
	switch tok.Kind {
	case TokArrow:
		return precArrow
	case TokQuestion:
		return precConditional
	case TokLParen:
		return precCall
	case TokDot, TokLBracket, TokQuestionDot:
		return precMember
	case TokTemplateStart:
		return precCall // tagged template: expr`template`
	case TokPlusPlus, TokMinusMinus:
		return precPostfix
	case TokStar, TokSlash, TokPercent:
		return precMultiplicative
	case TokStarStar:
		return precExponentiation
	case TokPlus, TokMinus:
		return precAdditive
	case TokLtLt, TokGtGt, TokGtGtGt:
		return precShift
	case TokLt, TokGt, TokLtEq, TokGtEq, TokIn, TokInstanceof:
		return precRelational
	case TokEqEq, TokNotEq, TokEqEqEq, TokNotEqEq:
		return precEquality
	case TokAnd:
		return precBitwiseAnd
	case TokCaret:
		return precBitwiseXor
	case TokPipe:
		return precBitwiseOr
	case TokAndAnd:
		return precLogicalAnd
	case TokPipePipe:
		return precLogicalOr
	case TokQuestionQuestion:
		return precLogicalOr // same precedence as || (cannot be mixed without parens)
	case TokEq, TokPlusEq, TokMinusEq, TokStarEq, TokSlashEq, TokPercentEq,
		TokAndAndEq, TokPipePipeEq, TokQuestionQuestionEq:
		return precAssignment
	default:
		return -1 // not an infix operator (or EOF)
	}
}

func tokenOpString(kind TokenKind) string {
	switch kind {
	case TokPlus:
		return "+"
	case TokMinus:
		return "-"
	case TokStar:
		return "*"
	case TokStarStar:
		return "**"
	case TokSlash:
		return "/"
	case TokPercent:
		return "%"
	case TokEqEq:
		return "=="
	case TokNotEq:
		return "!="
	case TokEqEqEq:
		return "==="
	case TokNotEqEq:
		return "!=="
	case TokLt:
		return "<"
	case TokGt:
		return ">"
	case TokLtEq:
		return "<="
	case TokGtEq:
		return ">="
	case TokAndAnd:
		return "&&"
	case TokPipePipe:
		return "||"
	case TokQuestionQuestion:
		return "??"
	case TokBang:
		return "!"
	case TokAnd:
		return "&"
	case TokPipe:
		return "|"
	case TokCaret:
		return "^"
	case TokTilde:
		return "~"
	case TokLtLt:
		return "<<"
	case TokGtGt:
		return ">>"
	case TokGtGtGt:
		return ">>>"
	case TokEq:
		return "="
	case TokPlusEq:
		return "+="
	case TokMinusEq:
		return "-="
	case TokStarEq:
		return "*="
	case TokSlashEq:
		return "/="
	case TokPercentEq:
		return "%="
	case TokAndAndEq:
		return "&&="
	case TokPipePipeEq:
		return "||="
	case TokQuestionQuestionEq:
		return "??="
	case TokPlusPlus:
		return "++"
	case TokMinusMinus:
		return "--"
	case TokTypeof:
		return "typeof"
	case TokVoid:
		return "void"
	case TokDelete:
		return "delete"
	case TokInstanceof:
		return "instanceof"
	case TokIn:
		return "in"
	default:
		return ""
	}
}

func (p *Parser) sync() {
	// Skip tokens until we find a statement boundary.
	for p.peek().Kind != TokEOF {
		switch p.peek().Kind {
		case TokSemicolon, TokRBrace:
			p.advance()
			return
		}
		p.advance()
	}
}

// parseRegExpToken extracts pattern and flags from a /pattern/flags token value.
func parseRegExpToken(value string) (pattern, flags string) {
	// value is like "/foo/gi" — strip leading /, then find last / and split
	if len(value) < 2 || value[0] != '/' {
		return "", ""
	}
	// Find the closing slash (last slash in the string).
	lastSlash := -1
	for i := len(value) - 1; i >= 1; i-- {
		if value[i] == '/' {
			lastSlash = i
			break
		}
	}
	if lastSlash <= 0 {
		return "", ""
	}
	pattern = value[1:lastSlash]
	flags = value[lastSlash+1:]
	return
}
