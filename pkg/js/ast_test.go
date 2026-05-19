package js

import (
	"testing"
)

// TestASTNodeConstructibility verifies that all AST node types can be
// constructed and satisfy the Node interface. This exercises the
// nodeMarker() methods that are required for interface satisfaction.
func TestASTNodeConstructibility(t *testing.T) {
	// Collect all constructed nodes and verify they satisfy the Node interface.
	lit := &Literal{Value: NewNumber(42)}
	ident := &Identifier{Name: "x"}
	bin := &BinaryExpression{Operator: "+", Left: ident, Right: lit}
	un := &UnaryExpression{Operator: "!", Argument: ident}
	assign := &AssignmentExpression{Operator: "=", Left: ident, Right: lit}

	nodes := []Node{
		&Program{Body: []Node{&ExpressionStatement{Expression: lit}}},
		&ExpressionStatement{Expression: lit},
		lit,
		&RegExpExpression{Pattern: "abc", Flags: "gi"},
		ident,
		bin,
		un,
		&VariableDeclaration{Kind: "var", Name: "x", Init: lit},
		assign,
		&BlockStatement{Body: []Node{&EmptyStatement{}}},
		&IfStatement{Test: lit, Consequent: &BlockStatement{}, Alternate: nil},
		&WhileStatement{Test: lit, Body: &BlockStatement{}},
		&DoWhileStatement{Test: lit, Body: &BlockStatement{}},
		&ForStatement{Init: nil, Test: lit, Update: nil, Body: &BlockStatement{}},
		&ForInStatement{Left: ident, Right: ident, Body: &BlockStatement{}},
		&ForOfStatement{Left: ident, Right: ident, Body: &BlockStatement{}},
		&ReturnStatement{Argument: lit},
		&FunctionDeclaration{Name: "f", Params: nil, Body: &BlockStatement{}},
		&FunctionExpression{Name: "", Params: nil, Body: &BlockStatement{}},
		&CallExpression{Callee: ident, Arguments: []Node{lit}},
		&MemberExpression{Object: ident, Property: ident, Computed: false},
		&OptionalMemberExpression{Object: ident, Property: ident, Computed: false},
		&OptionalCallExpression{Callee: ident, Arguments: []Node{lit}},
		&ArrowFunctionExpression{Params: nil, Body: lit},
		&ObjectExpression{Properties: []ObjectProperty{
			{Key: "a", Value: lit},
		}},
		&ArrayExpression{Elements: []Node{lit, ident}},
		&NewExpression{Callee: ident, Arguments: []Node{lit}},
		&ConditionalExpression{Test: lit, Consequent: ident, Alternate: lit},
		&UpdateExpression{Operator: "++", Argument: ident, Prefix: true},
		&SwitchStatement{Discriminant: ident, Cases: []SwitchCase{
			{Test: lit, Consequent: []Node{&BreakStatement{}}},
		}},
		&BreakStatement{},
		&ContinueStatement{},
		&ThisExpression{},
		&TryStatement{
			Block:      &BlockStatement{Body: []Node{}},
			CatchParam: "e",
			Handler:    &BlockStatement{Body: []Node{}},
			Finalizer:  nil,
		},
		&ThrowStatement{Argument: lit},
		&EmptyStatement{},
		&SequenceExpression{Expressions: []Node{lit, ident}},
		&TemplateLiteral{Quasis: []string{"hello", ""}, Expressions: []Node{ident}},
		&TaggedTemplateExpression{Tag: ident, Quasis: []string{"", ""}, Expressions: []Node{lit}},
		&DestructuringAssignment{
			Kind: "const",
			Elements: []DestructuringElement{
				{Key: "x", Default: lit},
			},
			Right: ident,
		},
		&YieldExpression{Argument: lit, Delegate: false},
		&SpreadExpression{Argument: ident},
		&AwaitExpression{Argument: ident},
		&ImportDeclaration{
			Specifiers: []ImportSpecifier{{Local: "foo", Imported: "foo"}},
			Source:     "./module.js",
		},
		&ImportExpression{Source: lit},
		&ExportDeclaration{
			Declaration: &VariableDeclaration{Kind: "const", Name: "x", Init: lit},
			IsDefault:   false,
		},
		&SuperExpression{},
		&ClassDeclaration{
			Name: "MyClass",
			Constructor: &ClassMethod{
				Name: "constructor",
				Params: []DefaultParam{
					{Name: "x"},
				},
				Body: &BlockStatement{},
			},
			Methods: []ClassMethod{
				{Name: "foo", Params: nil, Body: &BlockStatement{}},
				{Name: "bar", Static: true, Getter: true, Body: &BlockStatement{}},
				{Name: "baz", Setter: true, Body: &BlockStatement{}},
			},
		},
	}

	for i, n := range nodes {
		if n == nil {
			t.Errorf("node %d is nil", i)
		}
	}
}

// TestASTNestedStructs verifies nested struct construction.
func TestASTNestedStructs(t *testing.T) {
	// Complex nested program.
	prog := &Program{
		Body: []Node{
			&FunctionDeclaration{
				Name: "compute",
				Params: []DefaultParam{
					{Name: "a"},
					{Name: "b", Default: &Literal{Value: NewNumber(0)}},
				},
				Body: &BlockStatement{
					Body: []Node{
						&ReturnStatement{
							Argument: &BinaryExpression{
								Operator: "+",
								Left:     &Identifier{Name: "a"},
								Right:    &Identifier{Name: "b"},
							},
						},
					},
				},
			},
			&ExpressionStatement{
				Expression: &CallExpression{
					Callee:    &Identifier{Name: "compute"},
					Arguments: []Node{&Literal{Value: NewNumber(1)}, &Literal{Value: NewNumber(2)}},
				},
			},
		},
	}

	if len(prog.Body) != 2 {
		t.Fatalf("program body length = %d, want 2", len(prog.Body))
	}
}

// TestASTDefaultParamRest tests DefaultParam with Rest flag.
func TestASTDefaultParamRest(t *testing.T) {
	p := DefaultParam{Name: "args", Rest: true}
	if !p.Rest {
		t.Fatal("DefaultParam.Rest should be true")
	}
}

// TestASTDestructuringElementNested tests nested destructuring patterns.
func TestASTDestructuringElementNested(t *testing.T) {
	nested := &DestructuringAssignment{
		Kind: "let",
		Elements: []DestructuringElement{
			{Key: "x"},
			{Key: "y"},
		},
		Right:     &Identifier{Name: "obj"},
		ArrayMode: false,
	}

	outer := DestructuringElement{
		SourceKey: "data",
		Key:       "",
		Nested:    nested,
	}

	// Verify SourceKey aliasing.
	if outer.SourceKey != "data" {
		t.Errorf("SourceKey = %q, want %q", outer.SourceKey, "data")
	}
	if outer.Nested == nil {
		t.Fatal("nested destructuring should not be nil")
	}
}

// TestASTClassMethodComputed verifies ComputedKey on ClassMethod.
func TestASTClassMethodComputed(t *testing.T) {
	m := ClassMethod{
		Name:        "",
		Computed:    true,
		ComputedKey: &Identifier{Name: "Symbol.iterator"},
		Body:        &BlockStatement{},
	}
	if !m.Computed {
		t.Fatal("ClassMethod.Computed should be true")
	}
	if m.ComputedKey == nil {
		t.Fatal("ComputedKey should not be nil")
	}
}

// TestASTExportReExport verifies export-from syntax constructs.
func TestASTExportReExport(t *testing.T) {
	e := &ExportDeclaration{
		Specifiers: []ExportSpecifier{
			{Local: "foo", Exported: "bar"},
		},
		Source:    "./other.js",
		IsDefault: false,
	}
	if e.Source != "./other.js" {
		t.Errorf("Source = %q, want %q", e.Source, "./other.js")
	}
	if len(e.Specifiers) != 1 {
		t.Fatalf("specifiers = %d, want 1", len(e.Specifiers))
	}
}
