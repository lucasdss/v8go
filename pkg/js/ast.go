// ast.go — Abstract Syntax Tree node types for JavaScript.
//
// Strongly-typed Go structs representing every construct the parser produces.
// Mirrors the ECMAScript AST structure for use by the bytecode compiler.
package js

// Node is the base interface for all AST nodes.
type Node interface {
	nodeMarker()
}

// Program is the root node — a list of statements.
type Program struct {
	Body []Node
}

func (*Program) nodeMarker() {}

// ExpressionStatement wraps an expression as a statement.
type ExpressionStatement struct {
	Expression Node
}

func (*ExpressionStatement) nodeMarker() {}

// Literal represents a literal value: number, string, boolean, null, undefined.
type Literal struct {
	Value JSValue
}

func (*Literal) nodeMarker() {}

// RegExpExpression represents a regular expression literal: /pattern/flags.
type RegExpExpression struct {
	Pattern string
	Flags   string
}

func (*RegExpExpression) nodeMarker() {}

// Identifier represents a variable or property name reference.
type Identifier struct {
	Name string
}

func (*Identifier) nodeMarker() {}

// BinaryExpression: left OP right (e.g., a + b, x < y).
type BinaryExpression struct {
	Operator string
	Left     Node
	Right    Node
}

func (*BinaryExpression) nodeMarker() {}

// UnaryExpression: OP argument (e.g., !x, -y, typeof z).
type UnaryExpression struct {
	Operator string
	Argument Node
}

func (*UnaryExpression) nodeMarker() {}

// VariableDeclaration: var/let/const name [= init].
type VariableDeclaration struct {
	Kind string // "var", "let", "const"
	Name string
	Init Node // nil if no initializer
}

func (*VariableDeclaration) nodeMarker() {}

// AssignmentExpression: target = value (and compound: +=, -=, etc.).
type AssignmentExpression struct {
	Operator string
	Left     Node
	Right    Node
}

func (*AssignmentExpression) nodeMarker() {}

// BlockStatement: { ... }.
type BlockStatement struct {
	Body []Node
}

func (*BlockStatement) nodeMarker() {}

// IfStatement: if (test) consequent [else alternate].
type IfStatement struct {
	Test       Node
	Consequent Node
	Alternate  Node // nil if no else clause
}

func (*IfStatement) nodeMarker() {}

// WhileStatement: while (test) body.
type WhileStatement struct {
	Test Node
	Body Node
}

func (*WhileStatement) nodeMarker() {}

// DoWhileStatement: do { body } while (test).
type DoWhileStatement struct {
	Test Node
	Body Node
}

func (*DoWhileStatement) nodeMarker() {}

// ForStatement: for (init; test; update) body.
type ForStatement struct {
	Init   Node // may be nil
	Test   Node // may be nil
	Update Node // may be nil
	Body   Node
}

func (*ForStatement) nodeMarker() {}

// ForInStatement: for (left in right) body.
type ForInStatement struct {
	Left  Node   // VariableDeclaration or Identifier
	Right Node
	Body  Node
}

func (*ForInStatement) nodeMarker() {}

// ForOfStatement: for (left of right) body.
type ForOfStatement struct {
	Left  Node // VariableDeclaration or Identifier
	Right Node
	Body  Node
}

func (*ForOfStatement) nodeMarker() {}

// ReturnStatement: return [argument].
type ReturnStatement struct {
	Argument Node // nil for bare return
}

func (*ReturnStatement) nodeMarker() {}

// FunctionDeclaration: function name(params) { body }.
type FunctionDeclaration struct {
	Name      string
	Params    []DefaultParam
	Body      *BlockStatement
	Generator bool // true for function*
	Async     bool // true for async function
}

func (*FunctionDeclaration) nodeMarker() {}

// FunctionExpression: function [name](params) { body }.
type FunctionExpression struct {
	Name      string
	Params    []DefaultParam
	Body      *BlockStatement
	Generator bool // true for function*
	Async     bool // true for async function
}

func (*FunctionExpression) nodeMarker() {}

// CallExpression: callee(args...).
type CallExpression struct {
	Callee    Node
	Arguments []Node
}

func (*CallExpression) nodeMarker() {}

// MemberExpression: object.property or object[property].
type MemberExpression struct {
	Object     Node
	Property   Node
	Computed   bool // true for obj[prop], false for obj.prop
}

func (*MemberExpression) nodeMarker() {}

// OptionalMemberExpression: object?.property or object?.[property]
type OptionalMemberExpression struct {
	Object   Node
	Property Node
	Computed bool
	StartPos int
	EndPos   int
}

func (*OptionalMemberExpression) nodeMarker() {}

// OptionalCallExpression: callee?.(args...)
type OptionalCallExpression struct {
	Callee    Node
	Arguments []Node
	StartPos  int
	EndPos    int
}

func (*OptionalCallExpression) nodeMarker() {}

// ArrowFunctionExpression: (params) => body or param => body.
type ArrowFunctionExpression struct {
	Params []DefaultParam
	Body   Node // block statement or expression
	Async  bool // true for async arrow
}

func (*ArrowFunctionExpression) nodeMarker() {}

// ObjectExpression: { key: value, ... }.
type ObjectExpression struct {
	Properties []ObjectProperty
}

type ObjectProperty struct {
	Key         string
	Value       Node
	Shorthand   bool // true for {x} shorthand (equivalent to {x: x})
	Computed    bool // true for { [expr]: value }
	ComputedKey Node // the expression for computed key (when Computed is true)
	IsGetter    bool // get prop() { ... }
	IsSetter    bool // set prop(v) { ... }
}

func (*ObjectExpression) nodeMarker() {}

// ArrayExpression: [ elements... ].
type ArrayExpression struct {
	Elements []Node
}

func (*ArrayExpression) nodeMarker() {}

// NewExpression: new Constructor(args).
type NewExpression struct {
	Callee    Node
	Arguments []Node
}

func (*NewExpression) nodeMarker() {}

// ConditionalExpression: test ? consequent : alternate.
type ConditionalExpression struct {
	Test       Node
	Consequent Node
	Alternate  Node
}

func (*ConditionalExpression) nodeMarker() {}

// UpdateExpression: ++x, x++, --x, x--.
type UpdateExpression struct {
	Operator string
	Argument Node
	Prefix   bool
}

func (*UpdateExpression) nodeMarker() {}

// SwitchStatement: switch (discriminant) { cases... }.
type SwitchStatement struct {
	Discriminant Node
	Cases        []SwitchCase
}

type SwitchCase struct {
	Test       Node // nil for default case
	Consequent []Node
}

func (*SwitchStatement) nodeMarker() {}

// BreakStatement: break [label] (exits innermost loop or switch).
type BreakStatement struct {
	Label string // optional label name, empty if no label
}

func (*BreakStatement) nodeMarker() {}

// ContinueStatement: continue [label] (exits current loop iteration).
type ContinueStatement struct {
	Label string // optional label name, empty if no label
}

func (*ContinueStatement) nodeMarker() {}

// LabeledStatement: label: statement
type LabeledStatement struct {
	Label string
	Body  Node
}

func (*LabeledStatement) nodeMarker() {}
type ThisExpression struct{}

func (*ThisExpression) nodeMarker() {}

// TryStatement: try { body } catch (param) { handler } [finally { finalizer }].
type TryStatement struct {
	Block            *BlockStatement
	CatchParam       string                   // simple catch param name (empty if no catch or destructured)
	CatchDestructure *DestructuringAssignment // destructuring pattern for catch param (nil if simple name or no catch)
	Handler          *BlockStatement          // nil if no catch clause
	Finalizer        *BlockStatement          // nil if no finally clause
}

func (*TryStatement) nodeMarker() {}

// ThrowStatement: throw expression.
type ThrowStatement struct {
	Argument Node
}

func (*ThrowStatement) nodeMarker() {}

// EmptyStatement is a no-op statement (bare semicolon).
type EmptyStatement struct{}

func (*EmptyStatement) nodeMarker() {}

// SequenceExpression: a, b, c (comma operator).
type SequenceExpression struct {
	Expressions []Node
}

func (*SequenceExpression) nodeMarker() {}

// TemplateLiteral: `quasi0 ${expr0} quasi1 ${expr1} quasi2`
type TemplateLiteral struct {
	Quasis      []string // literal string segments
	Expressions []Node   // interpolated expressions
}

// TaggedTemplateExpression: tagFn`quasi0 ${expr0} quasi1`
type TaggedTemplateExpression struct {
	Tag         Node
	Quasis      []string
	Expressions []Node
}

func (*TaggedTemplateExpression) nodeMarker() {}

func (*TemplateLiteral) nodeMarker() {}

// DestructuringElement represents a single element in a destructuring pattern.
// Example patterns:
//   {a}           → SourceKey:"", Key:"a"
//   {a: b}        → SourceKey:"a", Key:"b" (rename)
//   {a: {b, c}}   → SourceKey:"a", Key:"", Nested:{Elements:[{b},{c}]}
//   {a = 1}       → SourceKey:"", Key:"a", Default:Literal(1)
//   {...rest}     → SourceKey:"", Key:"rest", Rest:true
//   [a]           → Key:"a"
//   [,a]          → Key:"" (elision), then Key:"a"
//   [[a, b]]      → Nested:{Elements:[{a},{b}]}, ArrayMode:true
//   [a, ...rest]  → Key:"a", then Key:"rest", Rest:true
type DestructuringElement struct {
	SourceKey string                   // source property name (for {srcKey: targetVar}), empty = same as Key
	Key       string                   // target variable name (empty for elision in arrays)
	Default   Node                     // default value expression (nil if none)
	Nested    *DestructuringAssignment // nested destructuring pattern (nil if leaf)
	Rest      bool                     // true for ...rest element
}

// DestructuringAssignment: var {a, b} = obj or var [x, y] = arr
type DestructuringAssignment struct {
	Kind      string                // "var", "let", "const"
	Elements  []DestructuringElement // the pattern elements
	Right     Node                  // the source object/array
	ArrayMode bool                  // true for array destructuring [x, y], false for object {a, b}
}

func (*DestructuringAssignment) nodeMarker() {}

// YieldExpression: yield [argument] or yield* argument.
type YieldExpression struct {
	Argument Node // nil for bare yield (yields undefined)
	Delegate bool // true for yield*
}

func (*YieldExpression) nodeMarker() {}

// SpreadExpression: ...expr (in array literals, object literals, or call args)
type SpreadExpression struct {
	Argument Node
}

func (*SpreadExpression) nodeMarker() {}

// AwaitExpression: await expr
type AwaitExpression struct {
	Argument Node
}

func (*AwaitExpression) nodeMarker() {}

// DefaultParam represents a function parameter with optional default value
// and optional destructuring pattern.
type DefaultParam struct {
	Name        string                  // simple param name (empty if destructured)
	Default     Node                    // default value expression (nil if no default)
	Destructure *DestructuringAssignment // destructuring pattern (nil if simple param)
	Rest        bool                    // true for rest parameter (...name)
}

// ClassDeclaration: class Name { constructor(...) { ... } method() { ... } }
type ClassDeclaration struct {
	Name         string
	HasExtends   bool     // true if class has an extends clause
	ExtendsExpr  Node     // the extends expression (nil if no extends)
	Constructor  *ClassMethod
	Methods      []ClassMethod
	StaticBlocks [][]Node // static { ... } initialization blocks (ES2022), each inner slice is one block's body
}

func (*ClassDeclaration) nodeMarker() {}

// ClassMethod represents a method inside a class body.
type ClassMethod struct {
	Name        string
	Static      bool
	Getter      bool  // get foo() { ... }
	Setter      bool  // set foo(v) { ... }
	Computed    bool  // [expr]() { ... }
	ComputedKey Node  // expression for computed key
	IsPrivate   bool  // #method() { ... }
	Params      []DefaultParam
	Body        *BlockStatement
}

// ImportSpecifier represents a single import binding.
type ImportSpecifier struct {
	Local       string // local binding name
	Imported    string // imported name (equals Local for non-aliased imports)
	IsDefault   bool   // import foo from '...'
	IsNamespace bool   // import * as foo from '...'
}

// ImportDeclaration: import ... from '...'
type ImportDeclaration struct {
	Specifiers []ImportSpecifier
	Source     string // module URL (empty for bare import without bindings)
}

func (*ImportDeclaration) nodeMarker() {}

// ExportSpecifier represents a single export binding.
type ExportSpecifier struct {
	Local    string // local name
	Exported string // exported name (equals Local for non-aliased exports)
}

// ExportDeclaration: export ... (declaration, default, or specifier list)
type ExportDeclaration struct {
	Declaration Node             // the exported declaration (nil for re-exports)
	Specifiers  []ExportSpecifier // for export { ... } or export { ... } from '...'
	Source      string           // module URL for re-exports
	IsDefault   bool             // export default ...
}

func (*ExportDeclaration) nodeMarker() {}

// ImportExpression: import('./module.js') — dynamic import
type ImportExpression struct {
	Source Node // expression evaluating to a module URL string
}

func (*ImportExpression) nodeMarker() {}

// SuperExpression: super (used in super(), super.x, super[x])
type SuperExpression struct{}

func (*SuperExpression) nodeMarker() {}

// PrivateMemberExpression: obj.#name (ES2022 private field/method access).
type PrivateMemberExpression struct {
	Object   Node
	Property string // private name without # prefix
}

func (*PrivateMemberExpression) nodeMarker() {}
