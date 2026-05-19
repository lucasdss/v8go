// compiler.go — AST-to-bytecode compiler.
//
// Walks the parsed AST and emits bytecode instructions + constant pool entries.
// Each function body gets its own BytecodeFunction. Variables are allocated
// to virtual registers; the register count is tracked per function.
package js

import (
	"strings"
)

// Compiler transforms a Program AST into executable bytecode.
type Compiler struct {
	bf           *BytecodeFunction
	globals      map[string]int // name → constant index (for global var names)
	globalSlotIndex map[string]int // name → slot index (pre-assigned for fast array lookup)
	nextReg      int            // next available register slot
	strings      map[string]int // string interning: value → constant index
	locals       map[string]int // local variable name → register index
	breakJumps    [][]int        // stack of lists of jump positions to patch for break
	continueJumps [][]int        // stack for continue jumps (jump to loop test)
	scopes        []*Scope       // scope stack (innermost at end)
	nextFeedback int            // next available feedback slot (V8-style per-site)
	isTopLevel   bool           // true when compiling top-level (global) script, false for function bodies
	currentLine  int            // current source line (updated during compilation)
	currentCol   int            // current source column (updated during compilation)
}

// recordSourcePos records the current source position for the next emitted instruction.
func (c *Compiler) recordSourcePos() {
	if c.currentLine > 0 || c.currentCol > 0 {
		c.bf.SourcePositions = append(c.bf.SourcePositions, SourcePos{Line: c.currentLine, Col: c.currentCol})
	}
}

// emitInstruction emits an instruction and records the current source position.
func (c *Compiler) emitInstruction(op Opcode, a, b, cReg uint8) {
	c.bf.Emit(op, a, b, cReg)
	c.recordSourcePos()
}

// ensureGlobalSlot returns the slot index for a global variable name.
// Assigns a new index on first use; indices are stable across the compilation.
func (c *Compiler) ensureGlobalSlot(name string) int {
	if idx, ok := c.globalSlotIndex[name]; ok {
		return idx
	}
	idx := len(c.globalSlotIndex)
	c.globalSlotIndex[name] = idx
	return idx
}

func (c *Compiler) allocFeedbackSlot() int {
	slot := c.nextFeedback
	c.nextFeedback++
	return slot
}

// Scope represents a lexical scope for variable resolution.
type Scope struct {
	parent   *Scope
	names    map[string]scopeVar
}

type scopeVar struct {
	reg         int
	kind        string // "var", "let", "const"
	initialized bool
}

func newScope(parent *Scope) *Scope {
	return &Scope{
		parent: parent,
		names:  make(map[string]scopeVar),
	}
}

func (s *Scope) lookup(name string) (scopeVar, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if v, ok := cur.names[name]; ok {
			return v, true
		}
	}
	return scopeVar{}, false
}

func (s *Scope) declare(name string, kind string, reg int) {
	s.names[name] = scopeVar{reg: reg, kind: kind, initialized: kind == "var"}
}

func (s *Scope) functionScope() *Scope {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.parent == nil {
			return cur
		}
	}
	return s
}

// Compile produces a BytecodeFunction from a Program AST.
func Compile(prog *Program) *BytecodeFunction {
	return CompileWithSource(prog, "<input>")
}

// CompileWithSource produces a BytecodeFunction from a Program AST with a known source file name.
func CompileWithSource(prog *Program, sourceFile string) *BytecodeFunction {
	globalScope := newScope(nil)
	c := &Compiler{
		bf:              NewBytecodeFunction("<main>"),
		globals:         make(map[string]int),
		globalSlotIndex: make(map[string]int),
		strings:         make(map[string]int),
		locals:          make(map[string]int),
		scopes:          []*Scope{globalScope},
		isTopLevel:      true,
	}

	// Hoist function declarations: emit their registrations first.
	c.hoistFunctionDeclarations(prog.Body)

	c.compileStatements(prog.Body)

	hasLastExpr := false
	for i := len(prog.Body) - 1; i >= 0; i-- {
		if _, ok := prog.Body[i].(*ExpressionStatement); ok {
			hasLastExpr = true
			break
		}
		if _, ok := prog.Body[i].(*EmptyStatement); !ok {
			break
		}
	}

	if hasLastExpr {
		c.bf.Emit(OpReturn, 0, 0, 0)
	} else {
		c.bf.Emit(OpLdaUndefined, 0, 0, 0)
		c.bf.Emit(OpReturn, 0, 0, 0)
	}

	c.bf.ICVector = NewFeedbackVector(c.nextFeedback + len(c.bf.Constants) + 8)
	if c.bf.ICVector != nil {
		c.bf.NumFeedbackSlots = len(c.bf.ICVector.Slots)
	}

	// Run peephole optimizer to merge adjacent instruction patterns.
	c.bf.Instructions = optimizeBytecode(c.bf.Instructions)

	// Eliminate unreachable code after unconditional terminators.
	c.bf.Instructions = eliminateDeadCode(c.bf.Instructions)

	// Assign global slots: scan all OpLdaGlobal/OpStaGlobal, build slot index,
	// and rewrite to OpLdaGlobalSlot/OpStaGlobalSlot for fast array access.
	assignGlobalSlots(c.bf)

	// Pre-compute string names to avoid allocs during global var lookup.
	c.bf.BuildConstantNames()

	c.bf.SourceFile = sourceFile
	return c.bf
}

// CompileString parses source and compiles it into a BytecodeFunction.
// Returns nil if parsing fails.
func CompileString(source string) *BytecodeFunction {
	tokens := NewLexer(source).Tokenize()
	prog, errs := NewParser(tokens).Parse()
	if len(errs) > 0 {
		return nil
	}
	return Compile(prog)
}

// CompileMultiple compiles multiple script sources sharing a scope context.
// IC feedback slots are allocated sequentially across all scripts so they
// don't collide. Common strings are interned across all compilation units
// via a shared string table, reducing constant pool duplication.
//
// This is used for <script> tag batches where multiple inline scripts
// share the same global scope and should be compiled together.
func CompileMultiple(sources []string) []*BytecodeFunction {
	sharedStrings := make(map[string]int)
	results := make([]*BytecodeFunction, 0, len(sources))

	for _, source := range sources {
		tokens := NewLexer(source).Tokenize()
		prog, errs := NewParser(tokens).Parse()
		if len(errs) > 0 {
			continue
		}

		globalScope := newScope(nil)
		c := &Compiler{
			bf:      NewBytecodeFunction("<script>"),
			globals: make(map[string]int),
			strings: sharedStrings, // Share string interning across scripts
			locals:  make(map[string]int),
			scopes:  []*Scope{globalScope},
			isTopLevel: true,
		}
		c.bf.BuildConstantNames()

		c.compileStatements(prog.Body)
		results = append(results, c.bf)
	}

	return results
}

// hoistFunctionDeclarations emits bytecode to register all function declarations
// before any other code runs (hoisting). This includes functions wrapped in export declarations.
func (c *Compiler) hoistFunctionDeclarations(stmts []Node) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *FunctionDeclaration:
			c.hoistOneFunction(s)
		case *ExportDeclaration:
			if fd, ok := s.Declaration.(*FunctionDeclaration); ok {
				c.hoistOneFunction(fd)
			}
		}
	}
}

func (c *Compiler) hoistOneFunction(fd *FunctionDeclaration) {
	var parentScope *Scope
	if len(c.scopes) > 0 {
		parentScope = c.scopes[len(c.scopes)-1]
	}
	paramNames := paramNames(fd.Params)
	innerBF := CompileFunctionWithParent(fd.Name, paramNames, fd.Params, fd.Body, parentScope)
	if fd.Generator || fd.Async {
		innerBF.Generator = true
	}
	if fd.Async {
		innerBF.Async = true
	}
	if fd.Generator && fd.Async {
		innerBF.AsyncGenerator = true
	}
	templateObj := NewJSObject()
	templateObj.ConstructorName = "Function"
	templateObj.Bytecode = innerBF
	templateIdx := c.bf.AddConstant(NewObject(templateObj))
	nameIdx := c.stringConstant(fd.Name)
	c.bf.Emit(OpLdaConstant, uint8(templateIdx), 0, 0)
	c.bf.Emit(OpCreateClosure, 0, 0, 0)
	c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
}

// paramNames extracts just the names from DefaultParam slice.
// For destructured params, the name is empty; we extract names from the destructuring pattern.
func paramNames(params []DefaultParam) []string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		if p.Destructure != nil {
			names = append(names, collectDestructuredNames(p.Destructure)...)
		} else {
			names = append(names, p.Name)
		}
	}
	return names
}

// collectDestructuredNames extracts variable names from a destructuring pattern recursively.
func collectDestructuredNames(da *DestructuringAssignment) []string {
	names := make([]string, 0, len(da.Elements))
	for _, elem := range da.Elements {
		if elem.Nested != nil {
			names = append(names, collectDestructuredNames(elem.Nested)...)
		} else if elem.Key != "" && !elem.Rest {
			names = append(names, elem.Key)
		}
	}
	return names
}

// CompileFunction compiles a single function body.
func CompileFunction(name string, paramNames []string, params []DefaultParam, body *BlockStatement) *BytecodeFunction {
	return CompileFunctionWithParent(name, paramNames, params, body, nil)
}

func CompileFunctionWithParent(name string, paramNames []string, params []DefaultParam, body *BlockStatement, parentScope *Scope) *BytecodeFunction {
	fnScope := newScope(parentScope)
	c := &Compiler{
		bf:              NewBytecodeFunction(name),
		globals:         make(map[string]int),
		globalSlotIndex: make(map[string]int),
		strings:         make(map[string]int),
		locals:          make(map[string]int),
		scopes:          []*Scope{fnScope},
	}

	// Count non-rest params; rest param (if any) must be the last one.
	nonRestCount := len(params)
	hasRest := false
	if len(params) > 0 && params[len(params)-1].Rest {
		hasRest = true
		nonRestCount = len(params) - 1
	}
	c.bf.NumParams = nonRestCount

	// Allocate registers for parameters (caller loads values into these regs).
	// Track param registers for later default/destructuring processing.
	paramRegs := make([]int, len(params))
	for i, p := range params {
		reg := c.allocReg()
		paramRegs[i] = reg
		if p.Destructure == nil && !p.Rest {
			c.locals[p.Name] = reg
			fnScope.declare(p.Name, "var", reg)
		}
	}

	// Track rest param info so the VM can populate the rest array.
	if hasRest {
		c.bf.HasRestParam = true
		c.bf.RestParamReg = paramRegs[len(params)-1]
	}

	// Emit default parameter initialization and destructuring.
	for i, p := range params {
		reg := paramRegs[i]

		// Skip rest parameter in default/destructuring loop — VM handles it.
		if p.Rest {
			c.locals[p.Name] = reg
			fnScope.declare(p.Name, "var", reg)
			continue
		}

		// Check for default value on the whole parameter (before destructuring).
		if p.Default != nil {
			c.bf.Emit(OpLdar, uint8(reg), 0, 0)
			c.bf.Emit(OpLdaUndefined, 0, 0, 0)
			c.bf.Emit(OpStrictEq, uint8(reg), 0, 0)
			jumpPastDefault := len(c.bf.Instructions)
			c.bf.Emit(OpJumpIfFalse, 0, 0, 0)
			c.compileExpression(p.Default)
			c.bf.Emit(OpStar, uint8(reg), 0, 0)
			c.bf.Instructions[jumpPastDefault].OperandA = uint8(len(c.bf.Instructions))
		}

		// Handle destructured parameter.
		if p.Destructure != nil {
			// Set up the destructuring pattern with the param reg as source.
			// We compile it inline without using the Right field (use reg directly).
			c.compileDestructuredParam(p.Destructure, reg)
		}
	}
	_ = paramNames // used for backward compat

	c.compileStatements(body.Body)

	// Default return is undefined.
	c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	c.bf.Emit(OpReturn, 0, 0, 0)
	c.bf.ICVector = NewFeedbackVector(c.nextFeedback + len(c.bf.Constants) + 8)
	if c.bf.ICVector != nil {
		c.bf.NumFeedbackSlots = len(c.bf.ICVector.Slots)
	}
	c.bf.Instructions = optimizeBytecode(c.bf.Instructions)
	c.bf.Instructions = eliminateDeadCode(c.bf.Instructions)

	// Assign global slots for fast array access (same as main Compile path).
	assignGlobalSlots(c.bf)
	c.bf.BuildConstantNames()

	return c.bf
}

func (c *Compiler) allocReg() int {
	r := c.nextReg
	c.nextReg++
	c.bf.EnsureRegisters(c.nextReg)
	return r
}

// --- Statement compilation ---

func (c *Compiler) compileStatements(stmts []Node) {
	for _, stmt := range stmts {
		c.compileStatement(stmt)
	}
}

func (c *Compiler) compileStatement(stmt Node) {
	switch s := stmt.(type) {
	case *ExpressionStatement:
		c.compileExpression(s.Expression)
	case *VariableDeclaration:
		c.compileVariableDeclaration(s)
	case *BlockStatement:
		// Push new block scope for let/const scoping.
		if len(c.scopes) > 0 {
			curScope := c.scopes[len(c.scopes)-1]
			blockScope := newScope(curScope)
			c.scopes = append(c.scopes, blockScope)
			// Save locals before block to restore after.
			savedLocals := make(map[string]int)
			for k, v := range c.locals {
				savedLocals[k] = v
			}
			c.compileStatements(s.Body)
			// Remove block-scoped locals.
			for name := range blockScope.names {
				delete(c.locals, name)
			}
			// Restore any outer-scope locals that were shadowed.
			for name, reg := range savedLocals {
				if _, ok := blockScope.names[name]; ok {
					c.locals[name] = reg
				}
			}
			c.scopes = c.scopes[:len(c.scopes)-1]
		} else {
			c.compileStatements(s.Body)
		}
	case *IfStatement:
		c.compileIfStatement(s)
	case *WhileStatement:
		c.compileWhileStatement(s)
	case *DoWhileStatement:
		c.compileDoWhileStatement(s)
	case *ForStatement:
		c.compileForStatement(s)
	case *SwitchStatement:
		c.compileSwitchStatement(s)
	case *TryStatement:
		c.compileTryStatement(s)
	case *ForInStatement:
		c.compileForInStatement(s)
	case *ForOfStatement:
		c.compileForOfStatement(s)
	case *ReturnStatement:
		c.compileReturnStatement(s)
	case *FunctionDeclaration:
		// Already handled by hoistFunctionDeclarations; skip.
	case *ClassDeclaration:
		c.compileClassDeclaration(s)
	case *BreakStatement:
		// Jump to exit of enclosing switch/loop. Emit a placeholder jump.
		if len(c.breakJumps) > 0 {
			last := len(c.breakJumps) - 1
			c.breakJumps[last] = append(c.breakJumps[last], len(c.bf.Instructions))
			c.bf.Emit(OpJump, 0, 0, 0)
		}
	case *ThrowStatement:
		c.compileExpression(s.Argument)
		c.bf.Emit(OpThrow, 0, 0, 0)
	case *ContinueStatement:
		if len(c.continueJumps) > 0 {
			last := len(c.continueJumps) - 1
			c.continueJumps[last] = append(c.continueJumps[last], len(c.bf.Instructions))
			c.bf.Emit(OpJump, 0, 0, 0)
		}
	case *EmptyStatement:
		// no-op
	case *DestructuringAssignment:
		c.compileDestructuringAssignment(s)
	case *ImportDeclaration:
		c.compileImportDeclaration(s)
	case *ExportDeclaration:
		c.compileExportDeclaration(s)
	}
}

func (c *Compiler) compileVariableDeclaration(decl *VariableDeclaration) {
	nameIdx := c.stringConstant(decl.Name)
	if decl.Init != nil {
		c.compileExpression(decl.Init)
	} else {
		c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	}
	// let/const always use locals. var uses locals inside functions, globals at top-level.
	if decl.Kind == "let" || decl.Kind == "const" || (!c.isTopLevel && decl.Kind == "var") {
		reg := c.allocReg()
		c.locals[decl.Name] = reg
		if len(c.scopes) > 0 {
			// Function-scoped var should be declared in the function scope (scopes[0]),
			// not the current block scope. For let/const, use the innermost scope.
			if decl.Kind == "var" && !c.isTopLevel {
				c.scopes[0].declare(decl.Name, decl.Kind, reg)
			} else {
				c.scopes[len(c.scopes)-1].declare(decl.Name, decl.Kind, reg)
			}
		}
		c.bf.Emit(OpStar, uint8(reg), 0, 0)
	} else {
		c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
	}
}

func (c *Compiler) compileIfStatement(stmt *IfStatement) {
	// Compile test, then JumpIfFalse to else/end.
	c.compileExpression(stmt.Test)

	// Emit placeholder jump for JumpIfFalse.
	jumpFalsePos := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfFalse, 0, 0, 0)

	// Consequent body.
	c.compileStatement(stmt.Consequent)

	if stmt.Alternate != nil {
		// Jump over else block.
		jumpEndPos := len(c.bf.Instructions)
		c.bf.Emit(OpJump, 0, 0, 0)

		// Patch JumpIfFalse to point to start of alternate.
		endOfConsequent := len(c.bf.Instructions)
		c.bf.Instructions[jumpFalsePos].OperandA = uint8(endOfConsequent)

		// Alternate body.
		c.compileStatement(stmt.Alternate)

		// Patch jump-to-end to point after alternate.
		c.bf.Instructions[jumpEndPos].OperandA = uint8(len(c.bf.Instructions))
	} else {
		// Patch JumpIfFalse to point past end of consequent.
		c.bf.Instructions[jumpFalsePos].OperandA = uint8(len(c.bf.Instructions))
	}
}

func (c *Compiler) compileWhileStatement(stmt *WhileStatement) {
	c.breakJumps = append(c.breakJumps, nil)
	c.continueJumps = append(c.continueJumps, nil)

	loopStart := len(c.bf.Instructions)

	// Compile test.
	c.compileExpression(stmt.Test)

	jumpFalsePos := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfFalse, 0, 0, 0)

	// Compile body.
	c.compileStatement(stmt.Body)

	// Patch continue jumps to loop start.
	continueTarget := uint8(loopStart)
	for _, j := range c.continueJumps[len(c.continueJumps)-1] {
		c.bf.Instructions[j].OperandA = continueTarget
	}

	// Jump back to loop start.
	c.bf.Emit(OpJump, uint8(loopStart), 0, 0)

	// Patch JumpIfFalse and break jumps to exit.
	exitTarget := uint8(len(c.bf.Instructions))
	c.bf.Instructions[jumpFalsePos].OperandA = exitTarget
	for _, j := range c.breakJumps[len(c.breakJumps)-1] {
		c.bf.Instructions[j].OperandA = exitTarget
	}

	c.breakJumps = c.breakJumps[:len(c.breakJumps)-1]
	c.continueJumps = c.continueJumps[:len(c.continueJumps)-1]
}

func (c *Compiler) compileDoWhileStatement(stmt *DoWhileStatement) {
	// Body executes at least once, then test.
	loopStart := len(c.bf.Instructions)
	c.compileStatement(stmt.Body)
	c.compileExpression(stmt.Test)
	// Jump back if test is truthy.
	c.bf.Emit(OpJumpIfTrue, uint8(loopStart), 0, 0)
}

func (c *Compiler) compileForStatement(stmt *ForStatement) {
	c.breakJumps = append(c.breakJumps, nil)
	c.continueJumps = append(c.continueJumps, nil)

	if stmt.Init != nil {
		c.compileStatement(stmt.Init)
	}

	loopStart := len(c.bf.Instructions)

	if stmt.Test != nil {
		c.compileExpression(stmt.Test)
	} else {
		c.bf.Emit(OpLdaTrue, 0, 0, 0)
	}

	jumpFalsePos := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfFalse, 0, 0, 0)

	c.compileStatement(stmt.Body)

	// Update is the continue target.
	updateStart := len(c.bf.Instructions)
	if stmt.Update != nil {
		c.compileExpression(stmt.Update)
	}

	// Patch continue jumps to update (or test if no update).
	continueTarget := uint8(updateStart)
	for _, j := range c.continueJumps[len(c.continueJumps)-1] {
		c.bf.Instructions[j].OperandA = continueTarget
	}

	c.bf.Emit(OpJump, uint8(loopStart), 0, 0)

	exitTarget := uint8(len(c.bf.Instructions))
	c.bf.Instructions[jumpFalsePos].OperandA = exitTarget
	for _, j := range c.breakJumps[len(c.breakJumps)-1] {
		c.bf.Instructions[j].OperandA = exitTarget
	}

	c.breakJumps = c.breakJumps[:len(c.breakJumps)-1]
	c.continueJumps = c.continueJumps[:len(c.continueJumps)-1]
}

func (c *Compiler) compileSwitchStatement(stmt *SwitchStatement) {
	c.compileExpression(stmt.Discriminant)
	discReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(discReg), 0, 0)

	// Push break target context.
	c.breakJumps = append(c.breakJumps, nil)
	exitJumps := &c.breakJumps[len(c.breakJumps)-1]

	for _, kase := range stmt.Cases {
		if kase.Test != nil {
			c.bf.Emit(OpLdar, uint8(discReg), 0, 0)
			testReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(testReg), 0, 0)
			c.compileExpression(kase.Test)
			c.bf.Emit(OpStrictEq, uint8(testReg), 0, 0)
			jumpFalseIdx := len(c.bf.Instructions)
			c.bf.Emit(OpJumpIfFalse, 0, 0, 0)
			c.compileStatements(kase.Consequent)
			// Case fall-through: jump to exit.
			*exitJumps = append(*exitJumps, len(c.bf.Instructions))
			c.bf.Emit(OpJump, 0, 0, 0)
			c.bf.Instructions[jumpFalseIdx].OperandA = uint8(len(c.bf.Instructions))
		} else {
			c.compileStatements(kase.Consequent)
		}
	}

	// Patch all break/exit jumps.
	exitTarget := uint8(len(c.bf.Instructions))
	for _, j := range *exitJumps {
		c.bf.Instructions[j].OperandA = exitTarget
	}

	// Pop break context.
	c.breakJumps = c.breakJumps[:len(c.breakJumps)-1]
}

func (c *Compiler) compileTryStatement(stmt *TryStatement) {
	if stmt.Finalizer != nil && stmt.Handler == nil {
		// try { ... } finally { ... } (no catch)
		// Set finally handler for return interception only.
		setFinallyIdx := len(c.bf.Instructions)
		c.bf.Emit(OpSetFinallyHandler, 0, 0, 0) // placeholder — patched below

		// Set try handler pointing to finally (for exceptions).
		setTryIdx := len(c.bf.Instructions)
		c.bf.Emit(OpSetTryHandler, 0, 0, 0) // placeholder — patched below

		// Compile try body.
		c.compileStatements(stmt.Block.Body)

		// Normal exit: clear handlers, then inline the finally body.
		c.bf.Emit(OpClearTryHandler, 0, 0, 0)
		c.bf.Emit(OpSetFinallyHandler, 255, 0, 0) // clear finally handler for normal path
		// Inline finally body for normal path.
		c.compileStatements(stmt.Finalizer.Body)
		c.bf.Emit(OpSetFinallyHandler, 255, 0, 0) // clear after finally
		jumpToEnd := len(c.bf.Instructions)
		c.bf.Emit(OpJump, 0, 0, 0) // jump past exception-handler entry

		// Exception/return entry point: re-execute finally body.
		finallyHandlerStart := len(c.bf.Instructions)
		c.bf.Emit(OpClearTryHandler, 0, 0, 0)

		// Patch the handler PCs.
		c.bf.Instructions[setTryIdx].OperandA = uint8(finallyHandlerStart)
		c.bf.Instructions[setFinallyIdx].OperandA = uint8(finallyHandlerStart)

		// Emit the same finally body again (or jump to the normal-path copy).
		// For simplicity, re-emit the finally body.
		c.compileStatements(stmt.Finalizer.Body)

		c.bf.Instructions[jumpToEnd].OperandA = uint8(len(c.bf.Instructions))
		return
	}

	if stmt.Handler != nil {
		// try { ... } catch(e) { ... } [finally { ... }]
		handlerSetupIdx := len(c.bf.Instructions)
		c.bf.Emit(OpSetTryHandler, 0, 0, 0)

		// Compile try body.
		c.compileStatements(stmt.Block.Body)

		// Clear handler on normal exit.
		c.bf.Emit(OpClearTryHandler, 0, 0, 0)

		// If there's a finally, inline it on the normal path before jumping past catch.
		var jumpOverCatch int
		var finallySetupIdx int
		if stmt.Finalizer != nil {
			finallySetupIdx = len(c.bf.Instructions)
			c.bf.Emit(OpSetFinallyHandler, 0, 0, 0) // placeholder for return interception
			c.compileStatements(stmt.Finalizer.Body)
			c.bf.Emit(OpSetFinallyHandler, 255, 0, 0) // clear finally handler
		}
		jumpOverCatch = len(c.bf.Instructions)
		c.bf.Emit(OpJump, 0, 0, 0)

		// Catch handler entry.
		catchStart := len(c.bf.Instructions)
		c.bf.Instructions[handlerSetupIdx].OperandA = uint8(catchStart)

		// Push block scope so the catch variable is scoped to the catch block.
		var catchReg int
		if len(c.scopes) > 0 {
			curScope := c.scopes[len(c.scopes)-1]
			blockScope := newScope(curScope)
			c.scopes = append(c.scopes, blockScope)
			savedLocals := make(map[string]int)
			for k, v := range c.locals {
				savedLocals[k] = v
			}

			// Catch param: store exception value from acc.
			if stmt.CatchDestructure != nil {
				// Destructuring catch: catch ({a, b}) { ... }
				// Exception value is in acc. Store it in a temp register for destructuring.
				catchReg = c.allocReg()
				c.bf.Emit(OpStar, uint8(catchReg), 0, 0)
				c.compileDestructuredParam(stmt.CatchDestructure, catchReg)
			} else if stmt.CatchParam != "" {
				catchReg = c.allocReg()
				c.locals[stmt.CatchParam] = catchReg
				blockScope.declare(stmt.CatchParam, "let", catchReg)
				c.bf.Emit(OpStar, uint8(catchReg), 0, 0)
			}
			c.compileStatements(stmt.Handler.Body)

			// Remove block-scoped locals.
			for name := range blockScope.names {
				delete(c.locals, name)
			}
			// Restore any outer-scope locals that were shadowed.
			for name, reg := range savedLocals {
				if _, ok := blockScope.names[name]; ok {
					c.locals[name] = reg
				}
			}
			c.scopes = c.scopes[:len(c.scopes)-1]
		} else {
			// Fallback (no scopes): catch param without block scope.
			if stmt.CatchDestructure != nil {
				catchReg = c.allocReg()
				c.bf.Emit(OpStar, uint8(catchReg), 0, 0)
				c.compileDestructuredParam(stmt.CatchDestructure, catchReg)
			} else if stmt.CatchParam != "" {
				catchReg = c.allocReg()
				c.locals[stmt.CatchParam] = catchReg
				c.bf.Emit(OpStar, uint8(catchReg), 0, 0)
			}
			c.compileStatements(stmt.Handler.Body)
		}
		c.bf.Emit(OpClearTryHandler, 0, 0, 0)

		// Finally block after catch.
		if stmt.Finalizer != nil {
			c.bf.Emit(OpSetFinallyHandler, 0, 0, 0) // placeholder for return interception in catch
			finallyAfterCatch := len(c.bf.Instructions)
			c.compileStatements(stmt.Finalizer.Body)
			c.bf.Emit(OpSetFinallyHandler, 255, 0, 0)
			// Patch the first finally handler (for try's return interception).
			c.bf.Instructions[finallySetupIdx].OperandA = uint8(finallyAfterCatch)
		}

		c.bf.Instructions[jumpOverCatch].OperandA = uint8(len(c.bf.Instructions))
	} else {
		// No catch and no finally — shouldn't reach here (first branch handles try+finally).
		// Fallback: just compile try body.
		c.compileStatements(stmt.Block.Body)
	}
}

func (c *Compiler) compileForInStatement(stmt *ForInStatement) {
	// Compile right-hand side (the object to iterate).
	c.compileExpression(stmt.Right)
	objReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(objReg), 0, 0)

	// For-in iterates over object keys. We need a way to enumerate keys.
	// Emit a built-in call: iterate over object keys via the VM.
	// For simplicity, we compile as a loop over Shape.Properties keys.

	// Load all keys into a temporary array-like structure.
	// Use OpForInSetup to prepare iteration.
	// For now, emit a ForInSetup that the VM handles natively.
	c.bf.Emit(OpForInSetup, uint8(objReg), 0, 0)  // acc = first key, or undefined if done

	loopStart := len(c.bf.Instructions)

	// Check if iteration is done (acc is undefined).
	c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)     // placeholder: jump to exit
	exitJumpIdx := len(c.bf.Instructions) - 1

	// Store current key to left variable.
	keyReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(keyReg), 0, 0)

	// Assign key to left variable.
	switch left := stmt.Left.(type) {
	case *VariableDeclaration:
		c.locals[left.Name] = keyReg
	case *Identifier:
		c.locals[left.Name] = keyReg
	}

	// Push break context.
	c.breakJumps = append(c.breakJumps, nil)
	bj := &c.breakJumps[len(c.breakJumps)-1]

	// Compile body.
	c.compileStatement(stmt.Body)

	// Get next key.
	c.bf.Emit(OpForInNext, uint8(objReg), 0, 0)    // acc = next key or undefined
	c.bf.Emit(OpJump, uint8(loopStart), 0, 0)       // jump back to test

	// Patch exit jump.
	exitTarget := uint8(len(c.bf.Instructions))
	c.bf.Instructions[exitJumpIdx].OperandA = exitTarget

	// Patch break jumps.
	for _, j := range *bj {
		c.bf.Instructions[j].OperandA = exitTarget
	}
	c.breakJumps = c.breakJumps[:len(c.breakJumps)-1]
}

func (c *Compiler) compileForOfStatement(stmt *ForOfStatement) {
	// Compile right-hand side (the iterable).
	c.compileExpression(stmt.Right)
	iterReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(iterReg), 0, 0)

	// Get length of iterable.
	lenIdx := c.stringConstant("length")
	c.bf.Emit(OpLdar, uint8(iterReg), 0, 0)
	slot := c.allocFeedbackSlot()
	c.bf.Emit(OpLdaNamedProperty, uint8(lenIdx), 0, uint8(slot))
	lenReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(lenReg), 0, 0)

	// Initialize index counter to 0.
	c.bf.Emit(OpLdaZero, 0, 0, 0)
	idxReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(idxReg), 0, 0)

	// Push break/continue context.
	c.breakJumps = append(c.breakJumps, nil)
	c.continueJumps = append(c.continueJumps, nil)

	// Jump to test (skip update on first iteration).
	jumpToTest := len(c.bf.Instructions)
	c.bf.Emit(OpJump, 0, 0, 0)

	// Update (continue target): increment index.
	updateStart := len(c.bf.Instructions)
	c.bf.Emit(OpLdar, uint8(idxReg), 0, 0)
	idxtmpReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(idxtmpReg), 0, 0)
	c.bf.Emit(OpLdaOne, 0, 0, 0)
	c.bf.Emit(OpAdd, uint8(idxtmpReg), 0, 0)
	c.bf.Emit(OpStar, uint8(idxReg), 0, 0)

	// Test: index < length.
	loopStart := len(c.bf.Instructions)
	c.bf.Instructions[jumpToTest].OperandA = uint8(loopStart)
	c.bf.Emit(OpLdar, uint8(idxReg), 0, 0)
	idxTmpReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(idxTmpReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(lenReg), 0, 0)
	c.bf.Emit(OpLessThan, uint8(idxTmpReg), 0, 0)
	jumpFalsePos := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfFalse, 0, 0, 0)

	// Load iterable[idx] into acc.
	c.bf.Emit(OpLdar, uint8(idxReg), 0, 0)
	elemSlot := c.allocFeedbackSlot()
	c.bf.Emit(OpLdaKeyedProperty, uint8(iterReg), uint8(elemSlot), 0)

	// Assign to left variable.
	switch left := stmt.Left.(type) {
	case *VariableDeclaration:
		elemReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(elemReg), 0, 0)
		c.locals[left.Name] = elemReg
		if len(c.scopes) > 0 {
			c.scopes[len(c.scopes)-1].declare(left.Name, left.Kind, elemReg)
		}
	case *Identifier:
		elemReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(elemReg), 0, 0)
		c.locals[left.Name] = elemReg
	case *ExpressionStatement:
		if ident, ok := left.Expression.(*Identifier); ok {
			elemReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(elemReg), 0, 0)
			c.locals[ident.Name] = elemReg
		}
	case *DestructuringAssignment:
		// Save the iterable element to a register for destructuring.
		elemReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(elemReg), 0, 0)
		for idx, elem := range left.Elements {
			if elem.Key == "" && elem.Nested == nil {
				continue // elision
			}
			if elem.Nested != nil {
				// Nested destructuring in for-of: load nested source from elemReg.
				c.bf.Emit(OpLdar, uint8(elemReg), 0, 0)
				if left.ArrayMode {
					idxStr := c.stringConstant(intKey(idx))
					c.bf.Emit(OpLdaConstant, uint8(idxStr), 0, 0)
					slot := c.allocFeedbackSlot()
					c.bf.Emit(OpLdaKeyedProperty, uint8(elemReg), uint8(slot), 0)
				} else {
					sourceKey := elem.SourceKey
					if sourceKey == "" {
						sourceKey = elem.Key
					}
					propIdx := c.stringConstant(sourceKey)
					slot := c.allocFeedbackSlot()
					c.bf.Emit(OpLdaNamedProperty, uint8(propIdx), 0, uint8(slot))
				}
				nestedReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(nestedReg), 0, 0)
				c.compileNestedDestructuring(elem.Nested, nestedReg)
				continue
			}
			if left.ArrayMode {
				// Array destructuring: element[idx] → key
				idxStr := c.stringConstant(intKey(idx))
				c.bf.Emit(OpLdaConstant, uint8(idxStr), 0, 0) // key → acc
				keyReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(keyReg), 0, 0)       // save key
				c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)       // key → acc
				slot := c.allocFeedbackSlot()
				c.bf.Emit(OpLdaKeyedProperty, uint8(elemReg), uint8(slot), 0) // elemReg[acc] → acc
				valReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(valReg), 0, 0)       // save value
				if left.Kind == "let" || left.Kind == "const" || (!c.isTopLevel && left.Kind == "var") {
					c.locals[elem.Key] = valReg
					if len(c.scopes) > 0 {
						if left.Kind == "var" && !c.isTopLevel {
							c.scopes[0].declare(elem.Key, left.Kind, valReg)
						} else {
							c.scopes[len(c.scopes)-1].declare(elem.Key, left.Kind, valReg)
						}
					}
				} else {
					// var at top-level — store as global
					nameIdx := c.stringConstant(elem.Key)
					c.bf.Emit(OpLdar, uint8(valReg), 0, 0)
					c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
				}
			} else {
				// Object destructuring: element[key] → var key
				sourceKey := elem.SourceKey
				if sourceKey == "" {
					sourceKey = elem.Key
				}
				propIdx := c.stringConstant(sourceKey)
				c.bf.Emit(OpLdaConstant, uint8(propIdx), 0, 0) // key → acc
				keyReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(keyReg), 0, 0)         // save key
				c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)         // key → acc
				slot := c.allocFeedbackSlot()
				c.bf.Emit(OpLdaKeyedProperty, uint8(elemReg), uint8(slot), 0)
				valReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(valReg), 0, 0)
				if left.Kind == "let" || left.Kind == "const" || (!c.isTopLevel && left.Kind == "var") {
					c.locals[elem.Key] = valReg
					if len(c.scopes) > 0 {
						if left.Kind == "var" && !c.isTopLevel {
							c.scopes[0].declare(elem.Key, left.Kind, valReg)
						} else {
							c.scopes[len(c.scopes)-1].declare(elem.Key, left.Kind, valReg)
						}
					}
				} else {
					nameIdx := c.stringConstant(elem.Key)
					c.bf.Emit(OpLdar, uint8(valReg), 0, 0)
					c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
				}
			}
		}
	}

	// Compile body.
	c.compileStatement(stmt.Body)

	// Patch continue jumps to update section.
	continueTarget := uint8(updateStart)
	for _, j := range c.continueJumps[len(c.continueJumps)-1] {
		c.bf.Instructions[j].OperandA = continueTarget
	}

	// Jump back to update.
	c.bf.Emit(OpJump, uint8(updateStart), 0, 0)

	// Patch exit and break jumps.
	exitTarget := uint8(len(c.bf.Instructions))
	c.bf.Instructions[jumpFalsePos].OperandA = exitTarget
	for _, j := range c.breakJumps[len(c.breakJumps)-1] {
		c.bf.Instructions[j].OperandA = exitTarget
	}

	c.breakJumps = c.breakJumps[:len(c.breakJumps)-1]
	c.continueJumps = c.continueJumps[:len(c.continueJumps)-1]
}

func (c *Compiler) compileReturnStatement(stmt *ReturnStatement) {
	if stmt.Argument != nil {
		c.compileExpression(stmt.Argument)
	} else {
		c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	}
	c.bf.Emit(OpReturn, 0, 0, 0)
}

func (c *Compiler) compileFunctionDeclaration(decl *FunctionDeclaration) {
	// Compile function body.
	paramNamesList := paramNames(decl.Params)
	innerBF := CompileFunction(decl.Name, paramNamesList, decl.Params, decl.Body)

	if decl.Generator || decl.Async {
		innerBF.Generator = true
	}
	if decl.Async {
		innerBF.Async = true
	}
	if decl.Generator && decl.Async {
		innerBF.AsyncGenerator = true
	}

	// Register the function in the VM by storing bytecode on a global.
	// The VM will look up the bytecode when the function is called.
	nameIdx := c.stringConstant(decl.Name)

	// Create a function template object and store the bytecode on it.
	// We use a sentinel approach: store the bytecode reference in the constant pool
	// as a special object that the VM recognizes.
	templateObj := NewJSObject()
	templateObj.ConstructorName = "Function"
	templateObj.Bytecode = innerBF
	templateIdx := c.bf.AddConstant(NewObject(templateObj))

	c.bf.Emit(OpLdaConstant, uint8(templateIdx), 0, 0)
	c.bf.Emit(OpCreateClosure, 0, 0, 0)
	c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
}

func (c *Compiler) compileClassDeclaration(decl *ClassDeclaration) {
	var parentScope *Scope
	if len(c.scopes) > 0 {
		parentScope = c.scopes[len(c.scopes)-1]
	}

	// 1. Compile the constructor method (or create empty constructor if none).
	var constructorBF *BytecodeFunction
	if decl.Constructor != nil {
		// Add implicit "return this" at the end of the constructor body.
		ctorBody := &BlockStatement{
			Body: append(append([]Node{}, decl.Constructor.Body.Body...), &ReturnStatement{Argument: &ThisExpression{}}),
		}
		paramNamesList := paramNames(decl.Constructor.Params)
		constructorBF = CompileFunctionWithParent(decl.Name, paramNamesList, decl.Constructor.Params, ctorBody, parentScope)
	} else {
		// Default empty constructor with implicit "return this".
		ctorBody := &BlockStatement{
			Body: []Node{&ReturnStatement{Argument: &ThisExpression{}}},
		}
		constructorBF = CompileFunctionWithParent(decl.Name, nil, nil, ctorBody, parentScope)
	}
	// Mark if this is a derived class constructor (this uninitialized until super()).
	constructorBF.IsDerivedConstructor = decl.HasExtends

	// 2. Compile method functions.
	methodBFs := make([]*BytecodeFunction, len(decl.Methods))
	for i, m := range decl.Methods {
		paramNamesList := paramNames(m.Params)
		methodBFs[i] = CompileFunctionWithParent(m.Name, paramNamesList, m.Params, m.Body, parentScope)
	}

	// 2.5 If extends clause present, compile extends expression for runtime check.
	if decl.HasExtends && decl.ExtendsExpr != nil {
		c.compileExpression(decl.ExtendsExpr)
		c.bf.Emit(OpCheckConstructor, 0, 0, 0)
	}

	// 3. Create constructor template, store bytecode, emit closure.
	ctorTemplate := NewJSObject()
	ctorTemplate.ConstructorName = "Function"
	ctorTemplate.Bytecode = constructorBF
	ctorTemplateIdx := c.bf.AddConstant(NewObject(ctorTemplate))
	c.bf.Emit(OpLdaConstant, uint8(ctorTemplateIdx), 0, 0)
	c.bf.Emit(OpCreateClosure, 0, 0, 0)
	ctorReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(ctorReg), 0, 0)

	// 4. Create prototype object and attach methods.
	c.bf.Emit(OpCreateObject, 0, 0, 0)
	protoReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(protoReg), 0, 0)

	for i, m := range decl.Methods {
		// Compile method into bytecode, embed in template, create closure.
		mTemplate := NewJSObject()
		mTemplate.ConstructorName = "Function"
		mTemplate.Bytecode = methodBFs[i]
		mTemplateIdx := c.bf.AddConstant(NewObject(mTemplate))
		c.bf.Emit(OpLdaConstant, uint8(mTemplateIdx), 0, 0)
		c.bf.Emit(OpCreateClosure, 0, 0, 0)
		methodReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(methodReg), 0, 0)

		// Static methods → attach to constructor; instance methods → attach to prototype.
		targetReg := protoReg
		if m.Static {
			targetReg = ctorReg
		}
		c.bf.Emit(OpLdar, uint8(targetReg), 0, 0)
		methodNameIdx := c.stringConstant(m.Name)
		c.bf.Emit(OpStaNamedProperty, uint8(methodNameIdx), uint8(methodReg), 255)
	}

	// 5. Set prototype.constructor = ctorFn
	c.bf.Emit(OpLdar, uint8(protoReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
	ctorValReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(ctorValReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(protoReg), 0, 0)
	constructorIdx := c.stringConstant("constructor")
	c.bf.Emit(OpStaNamedProperty, uint8(constructorIdx), uint8(ctorValReg), 255)

	// 6. Set constructor's prototype = proto (so instances delegate to proto).
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(protoReg), 0, 0)
	protoValReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(protoValReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
	prototypeIdx := c.stringConstant("prototype")
	c.bf.Emit(OpStaNamedProperty, uint8(prototypeIdx), uint8(protoValReg), 255)

	// 6.5 Compile and execute static initialization blocks (ES2022).
	for _, stmts := range decl.StaticBlocks {
		blockBody := &BlockStatement{Body: stmts}
		blockBF := CompileFunctionWithParent("", nil, nil, blockBody, parentScope)

		blockTemplate := NewJSObject()
		blockTemplate.ConstructorName = "Function"
		blockTemplate.Bytecode = blockBF
		blockTemplateIdx := c.bf.AddConstant(NewObject(blockTemplate))
		c.bf.Emit(OpLdaConstant, uint8(blockTemplateIdx), 0, 0)
		c.bf.Emit(OpCreateClosure, 0, 0, 0)
		blockFuncReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(blockFuncReg), 0, 0)

		// Call with this = ctorReg, 0 args.
		c.bf.Emit(OpCall, uint8(blockFuncReg), 0, uint8(ctorReg))
	}

	// 7. Store constructor as class name in global scope.
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
	nameIdx := c.stringConstant(decl.Name)
	c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
}

// compileClassExpression compiles an anonymous class expression (class { ... }).
// Same as compileClassDeclaration but leaves the constructor value in acc
// instead of storing to a global variable.
func (c *Compiler) compileClassExpression(decl *ClassDeclaration) {
	var parentScope *Scope
	if len(c.scopes) > 0 {
		parentScope = c.scopes[len(c.scopes)-1]
	}

	// 1. Compile the constructor method (or create empty constructor if none).
	var constructorBF *BytecodeFunction
	if decl.Constructor != nil {
		ctorBody := &BlockStatement{
			Body: append(append([]Node{}, decl.Constructor.Body.Body...), &ReturnStatement{Argument: &ThisExpression{}}),
		}
		paramNamesList := paramNames(decl.Constructor.Params)
		constructorBF = CompileFunctionWithParent(decl.Name, paramNamesList, decl.Constructor.Params, ctorBody, parentScope)
	} else {
		ctorBody := &BlockStatement{
			Body: []Node{&ReturnStatement{Argument: &ThisExpression{}}},
		}
		constructorBF = CompileFunctionWithParent(decl.Name, nil, nil, ctorBody, parentScope)
	}
	constructorBF.IsDerivedConstructor = decl.HasExtends

	// 2. Compile method functions.
	methodBFs := make([]*BytecodeFunction, len(decl.Methods))
	for i, m := range decl.Methods {
		paramNamesList := paramNames(m.Params)
		methodBFs[i] = CompileFunctionWithParent(m.Name, paramNamesList, m.Params, m.Body, parentScope)
	}

	// 2.5 If extends clause present, compile extends expression for runtime check.
	if decl.HasExtends && decl.ExtendsExpr != nil {
		c.compileExpression(decl.ExtendsExpr)
		c.bf.Emit(OpCheckConstructor, 0, 0, 0)
	}

	// 3. Create constructor template, emit closure.
	ctorTemplate := NewJSObject()
	ctorTemplate.ConstructorName = "Function"
	ctorTemplate.Bytecode = constructorBF
	ctorTemplateIdx := c.bf.AddConstant(NewObject(ctorTemplate))
	c.bf.Emit(OpLdaConstant, uint8(ctorTemplateIdx), 0, 0)
	c.bf.Emit(OpCreateClosure, 0, 0, 0)
	ctorReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(ctorReg), 0, 0)

	// 4. Create prototype object and attach methods.
	c.bf.Emit(OpCreateObject, 0, 0, 0)
	protoReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(protoReg), 0, 0)

	for i, m := range decl.Methods {
		mTemplate := NewJSObject()
		mTemplate.ConstructorName = "Function"
		mTemplate.Bytecode = methodBFs[i]
		mTemplateIdx := c.bf.AddConstant(NewObject(mTemplate))
		c.bf.Emit(OpLdaConstant, uint8(mTemplateIdx), 0, 0)
		c.bf.Emit(OpCreateClosure, 0, 0, 0)
		methodReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(methodReg), 0, 0)

		// Static methods → attach to constructor; instance methods → attach to prototype.
		targetReg := protoReg
		if m.Static {
			targetReg = ctorReg
		}
		c.bf.Emit(OpLdar, uint8(targetReg), 0, 0)
		methodNameIdx := c.stringConstant(m.Name)
		c.bf.Emit(OpStaNamedProperty, uint8(methodNameIdx), uint8(methodReg), 255)
	}

	// 5. Set prototype.constructor = ctorFn
	c.bf.Emit(OpLdar, uint8(protoReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
	ctorValReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(ctorValReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(protoReg), 0, 0)
	constructorIdx := c.stringConstant("constructor")
	c.bf.Emit(OpStaNamedProperty, uint8(constructorIdx), uint8(ctorValReg), 255)

	// 6. Set constructor's prototype = proto.
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(protoReg), 0, 0)
	protoValReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(protoValReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
	prototypeIdx := c.stringConstant("prototype")
	c.bf.Emit(OpStaNamedProperty, uint8(prototypeIdx), uint8(protoValReg), 255)

	// 6.5 Compile and execute static initialization blocks (ES2022).
	for _, stmts := range decl.StaticBlocks {
		blockBody := &BlockStatement{Body: stmts}
		blockBF := CompileFunctionWithParent("", nil, nil, blockBody, parentScope)

		blockTemplate := NewJSObject()
		blockTemplate.ConstructorName = "Function"
		blockTemplate.Bytecode = blockBF
		blockTemplateIdx := c.bf.AddConstant(NewObject(blockTemplate))
		c.bf.Emit(OpLdaConstant, uint8(blockTemplateIdx), 0, 0)
		c.bf.Emit(OpCreateClosure, 0, 0, 0)
		blockFuncReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(blockFuncReg), 0, 0)

		// Call with this = ctorReg, 0 args.
		c.bf.Emit(OpCall, uint8(blockFuncReg), 0, uint8(ctorReg))
	}

	// 7. Leave constructor in acc — no global store for class expression.
	c.bf.Emit(OpLdar, uint8(ctorReg), 0, 0)
}

// --- Expression compilation ---

func (c *Compiler) compileExpression(expr Node) {
	switch e := expr.(type) {
	case *Literal:
		c.compileLiteral(e)
	case *Identifier:
		c.compileIdentifier(e)
	case *BinaryExpression:
		c.compileBinaryExpression(e)
	case *UnaryExpression:
		c.compileUnaryExpression(e)
	case *AssignmentExpression:
		c.compileAssignmentExpression(e)
	case *CallExpression:
		c.compileCallExpression(e)
	case *MemberExpression:
		c.compileMemberExpression(e)
	case *OptionalMemberExpression:
		c.compileOptionalMemberExpression(e)
	case *OptionalCallExpression:
		c.compileOptionalCallExpression(e)
	case *ThisExpression:
		c.bf.Emit(OpLdaThis, 0, 0, 0)
	case *SuperExpression:
		// super in expressions: load this.__proto__ for basic prototype chain access.
		// Full spec compliance requires tracking [[HomeObject]], but this gives
		// correct behavior for common super.prop patterns.
		c.bf.Emit(OpLdaThis, 0, 0, 0)
		// Access __proto__ on this.
		protoIdx := c.stringConstant("__proto__")
		c.bf.Emit(OpLdaNamedProperty, uint8(protoIdx), 0, 255)
	case *TaggedTemplateExpression:
		c.compileTaggedTemplate(e)
	case *UpdateExpression:
		c.compileUpdateExpression(e)
	case *ConditionalExpression:
		c.compileConditionalExpression(e)
	case *ObjectExpression:
		c.compileObjectExpression(e)
	case *ArrayExpression:
		c.compileArrayExpression(e)
	case *NewExpression:
		c.compileNewExpression(e)
	case *FunctionExpression:
		c.compileFunctionExpression(e)
	case *ArrowFunctionExpression:
		c.compileArrowFunction(e)
	case *SequenceExpression:
		for _, sub := range e.Expressions {
			c.compileExpression(sub)
		}
	case *TemplateLiteral:
		c.compileTemplateLiteral(e)
	case *YieldExpression:
		c.compileYieldExpression(e)
	case *AwaitExpression:
		c.compileAwaitExpression(e)
	case *SpreadExpression:
		c.compileSpreadExpression(e)
case *RegExpExpression:
c.compileRegExpExpression(e)
	case *ImportExpression:
		c.compileImportExpression(e)
	case *ClassDeclaration:
		c.compileClassExpression(e)
	}
}

func (c *Compiler) compileLiteral(lit *Literal) {
	switch lit.Value.Tag {
	case TagNumber:
		n := lit.Value.NumVal
		if n == 0 {
			c.bf.Emit(OpLdaZero, 0, 0, 0)
		} else if n == 1 {
			c.bf.Emit(OpLdaOne, 0, 0, 0)
		} else if n == float64(int8(n)) && n >= -128 && n <= 127 {
			// Small integer: use LdaSmi (V8 optimization).
			c.bf.Emit(OpLdaSmi, uint8(int8(n)), 0, 0)
		} else {
			idx := c.bf.AddConstant(lit.Value)
			c.bf.Emit(OpLdaConstant, uint8(idx), 0, 0)
		}
	case TagString:
		idx := c.stringConstant(lit.Value.StrVal)
		c.bf.Emit(OpLdaConstant, uint8(idx), 0, 0)
	case TagBoolean:
		if lit.Value.BoolVal {
			c.bf.Emit(OpLdaTrue, 0, 0, 0)
		} else {
			c.bf.Emit(OpLdaFalse, 0, 0, 0)
		}
	case TagNull:
		c.bf.Emit(OpLdaNull, 0, 0, 0)
	case TagUndefined:
		c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	case TagSymbol:
		idx := c.bf.AddConstant(lit.Value)
		c.bf.Emit(OpLdaConstant, uint8(idx), 0, 0)
	case TagBigInt:
		idx := c.bf.AddConstant(lit.Value)
		c.bf.Emit(OpLdaConstant, uint8(idx), 0, 0)
	}
}

func (c *Compiler) compileIdentifier(ident *Identifier) {
	// Check local scope first.
	if reg, ok := c.locals[ident.Name]; ok {
		c.bf.Emit(OpLdar, uint8(reg), 0, 0)
		return
	}
	// Check scope chain.
	if len(c.scopes) > 0 {
		curScope := c.scopes[len(c.scopes)-1]
		// Only check the current scope's own names (not parent chain).
		if sv, ok := curScope.names[ident.Name]; ok {
			c.bf.Emit(OpLdar, uint8(sv.reg), 0, 0)
			return
		}
		// Check parent scope (full chain) for captured variables.
		if curScope.parent != nil {
			if sv, ok := curScope.parent.lookup(ident.Name); ok {
				c.bf.Captured = append(c.bf.Captured, ident.Name)
				c.bf.Emit(OpLdaCaptured, uint8(sv.reg), 0, 0)
				return
			}
		}
	}
	// Fall back to global.
	nameIdx := c.stringConstant(ident.Name)
	c.bf.Emit(OpLdaGlobal, uint8(nameIdx), 0, 0)
}

func (c *Compiler) compileBinaryExpression(expr *BinaryExpression) {
	// ?? has short-circuit semantics: only evaluate RHS if LHS is null/undefined.
	if expr.Operator == "??" {
		c.compileExpression(expr.Left)
		reg := c.allocReg()
		c.bf.Emit(OpStar, uint8(reg), 0, 0) // save left; acc still has it
		jumpIdx := len(c.bf.Instructions)
		c.bf.Emit(OpJumpIfNotNullish, 0, 0, 0) // if left not nullish, skip RHS
		c.compileExpression(expr.Right)
		c.bf.Instructions[jumpIdx].OperandA = uint8(len(c.bf.Instructions))
		return
	}

	// Compile left, store to reg, compile right, then operate.
	c.compileExpression(expr.Left)
	reg := c.allocReg()
	c.bf.Emit(OpStar, uint8(reg), 0, 0)
	c.compileExpression(expr.Right)

	slot := c.allocFeedbackSlot()
	switch expr.Operator {
	case "+":
		c.bf.Emit(OpAdd, uint8(reg), 0, uint8(slot))
	case "-":
		c.bf.Emit(OpSub, uint8(reg), 0, uint8(slot))
	case "*":
		c.bf.Emit(OpMul, uint8(reg), 0, uint8(slot))
	case "/":
		c.bf.Emit(OpDiv, uint8(reg), 0, uint8(slot))
	case "%":
		c.bf.Emit(OpMod, uint8(reg), 0, uint8(slot))
	case "**":
		c.bf.Emit(OpExp, uint8(reg), 0, uint8(slot))
	case "==":
		c.bf.Emit(OpEq, uint8(reg), 0, uint8(slot))
	case "!=":
		c.bf.Emit(OpNotEq, uint8(reg), 0, uint8(slot))
	case "===":
		c.bf.Emit(OpStrictEq, uint8(reg), 0, uint8(slot))
	case "!==":
		c.bf.Emit(OpStrictNotEq, uint8(reg), 0, uint8(slot))
	case "<":
		c.bf.Emit(OpLessThan, uint8(reg), 0, uint8(slot))
	case ">":
		c.bf.Emit(OpGreaterThan, uint8(reg), 0, uint8(slot))
	case "<=":
		c.bf.Emit(OpLessEq, uint8(reg), 0, uint8(slot))
	case ">=":
		c.bf.Emit(OpGreaterEq, uint8(reg), 0, uint8(slot))
	case "&&":
		c.bf.Emit(OpLogicalAnd, uint8(reg), 0, uint8(slot))
	case "||":
		c.bf.Emit(OpLogicalOr, uint8(reg), 0, uint8(slot))
	case "&":
		c.bf.Emit(OpBitwiseAnd, uint8(reg), 0, uint8(slot))
	case "|":
		c.bf.Emit(OpBitwiseOr, uint8(reg), 0, uint8(slot))
	case "^":
		c.bf.Emit(OpBitwiseXor, uint8(reg), 0, uint8(slot))
	case "<<":
		c.bf.Emit(OpShiftLeft, uint8(reg), 0, uint8(slot))
	case ">>":
		c.bf.Emit(OpShiftRight, uint8(reg), 0, uint8(slot))
	case ">>>":
		c.bf.Emit(OpShiftRightZero, uint8(reg), 0, uint8(slot))
	case "instanceof":
		c.bf.Emit(OpInstanceof, uint8(reg), 0, uint8(slot))
	case "in":
		c.bf.Emit(OpIn, uint8(reg), 0, uint8(slot))
	}
}

func (c *Compiler) compileUnaryExpression(expr *UnaryExpression) {
	c.compileExpression(expr.Argument)
	switch expr.Operator {
	case "!":
		c.bf.Emit(OpLogicalNot, 0, 0, 0)
	case "-":
		c.bf.Emit(OpNegate, 0, 0, 0)
	case "+":
		c.bf.Emit(OpToNumber, 0, 0, 0)
	case "~":
		c.bf.Emit(OpBitwiseNot, 0, 0, 0)
	case "typeof":
		c.bf.Emit(OpTypeof, 0, 0, 0)
	case "void":
		c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	case "delete":
		if member, ok := expr.Argument.(*MemberExpression); ok {
			if member.Computed {
				// delete obj[expr] → OpDeleteKeyed
				c.compileExpression(member.Object)
				objReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(objReg), 0, 0)
				c.compileExpression(member.Property)
				keyReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(keyReg), 0, 0)
				c.bf.Emit(OpDeleteKeyed, uint8(objReg), uint8(keyReg), 0)
			} else {
				// delete obj.prop → OpDelete
				c.compileExpression(member.Object)
				propName := c.stringConstant(member.Property.(*Identifier).Name)
				c.bf.Emit(OpDelete, uint8(propName), 0, 0)
			}
		} else {
			// delete identifier → always true
			c.compileExpression(expr.Argument)
			c.bf.Emit(OpLdaTrue, 0, 0, 0)
		}
	}
}

// compoundAssignOps maps compound assignment operators to their binary opcodes.
var compoundAssignOps = map[string]Opcode{
	"+=":   OpAdd,
	"-=":   OpSub,
	"*=":   OpMul,
	"/=":   OpDiv,
	"%=":   OpMod,
	"**=":  OpExp,
	"<<=":  OpShiftLeft,
	">>=":  OpShiftRight,
	">>>=": OpShiftRightZero,
	"&=":   OpBitwiseAnd,
	"|=":   OpBitwiseOr,
	"^=":   OpBitwiseXor,
}

func (c *Compiler) compileAssignmentExpression(expr *AssignmentExpression) {
	// Logical assignment operators (||=, &&=, ??=) need short-circuit:
	// they only evaluate and assign the RHS if the LHS meets a condition.
	switch expr.Operator {
	case "||=":
		c.compileLogicalAssignment(expr, "truthy")
		return
	case "&&=":
		c.compileLogicalAssignment(expr, "falsy")
		return
	case "??=":
		c.compileLogicalAssignment(expr, "nullish")
		return
	}

	// Compound assignment (e.g., +=, -=): load LHS, apply binary op with RHS, store back.
	if binOp, ok := compoundAssignOps[expr.Operator]; ok {
		// Load current LHS value.
		c.compileExpression(expr.Left)
		lhsReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(lhsReg), 0, 0)
		// Compile RHS.
		c.compileExpression(expr.Right)
		// Apply binary operation: acc = lhsReg OP acc.
		c.bf.Emit(binOp, uint8(lhsReg), 0, uint8(c.allocFeedbackSlot()))
		// Store result back to LHS.
		c.compileAssignmentStore(expr.Left)
		return
	}

	// Regular assignment: compile right-hand side, store to variable.
	c.compileExpression(expr.Right)
	c.compileAssignmentStore(expr.Left)
}

// compileAssignmentStore stores the accumulator into the assignment target.
func (c *Compiler) compileAssignmentStore(left Node) {
	switch target := left.(type) {
	case *Identifier:
		c.compileIdentifierStore(target)
	case *MemberExpression:
		c.compileMemberAssignment(target)
	case *DestructuringAssignment:
		rhsReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(rhsReg), 0, 0)
		c.compileDestructuredParam(target, rhsReg)
		c.bf.Emit(OpLdar, uint8(rhsReg), 0, 0)
	}
}

// compileLogicalAssignment compiles x ||= y, x &&= y, x ??= y with short-circuit.
// mode: "truthy" (||=), "falsy" (&&=), "nullish" (??=)
func (c *Compiler) compileLogicalAssignment(expr *AssignmentExpression, mode string) {
	switch left := expr.Left.(type) {
	case *Identifier:
		// 1. Load current value of the variable.
		c.compileIdentifier(left)
		// 2. Save in temp register.
		regVal := c.allocReg()
		c.bf.Emit(OpStar, uint8(regVal), 0, 0)
		// 3. Jump to end if short-circuit condition met.
		jumpIdx := len(c.bf.Instructions)
		switch mode {
		case "truthy":
			c.bf.Emit(OpJumpIfToBooleanTrue, 0, 0, 0) // jump if truthy → short-circuit
		case "falsy":
			c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0) // jump if falsy → short-circuit
		case "nullish":
			c.bf.Emit(OpJumpIfNotNullish, 0, 0, 0) // jump if NOT null/undefined → short-circuit
		}
		// 4. Evaluate RHS and store.
		c.compileExpression(expr.Right)
		c.compileIdentifierStore(left)
		// 5. Patch jump target.
		c.bf.Instructions[jumpIdx].OperandA = uint8(len(c.bf.Instructions))
		// 6. Reload the value (result of assignment) into acc from the variable.
		c.compileIdentifier(left)

	case *MemberExpression:
		if left.Computed {
			// Computed: obj[expr] ||= val
			// 1. Compile object → regObj
			c.compileExpression(left.Object)
			regObj := c.allocReg()
			c.bf.Emit(OpStar, uint8(regObj), 0, 0)
			// 2. Compile key → regKey
			c.compileExpression(left.Property)
			regKey := c.allocReg()
			c.bf.Emit(OpStar, uint8(regKey), 0, 0)
			// 3. Load obj[key] → regVal
			c.bf.Emit(OpLdar, uint8(regKey), 0, 0) // acc = key
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaKeyedProperty, uint8(regObj), uint8(slot), 0) // acc = obj[key]
			regVal := c.allocReg()
			c.bf.Emit(OpStar, uint8(regVal), 0, 0)
			// 4. Short-circuit check: jump to SHORT if condition met.
			jumpShortIdx := len(c.bf.Instructions)
			switch mode {
			case "truthy":
				c.bf.Emit(OpJumpIfToBooleanTrue, 0, 0, 0)
			case "falsy":
				c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
			case "nullish":
				c.bf.Emit(OpJumpIfNotNullish, 0, 0, 0)
			}
			// 5. Evaluate RHS → regRhs, store to obj[key].
			c.compileExpression(expr.Right)
			regRhs := c.allocReg()
			c.bf.Emit(OpStar, uint8(regRhs), 0, 0)
			c.bf.Emit(OpLdar, uint8(regKey), 0, 0) // acc = key
			c.bf.Emit(OpStaKeyedProperty, uint8(regObj), uint8(regRhs), 0)
			// 6. Load RHS result → acc, jump to END.
			c.bf.Emit(OpLdar, uint8(regRhs), 0, 0)
			jumpEndIdx := len(c.bf.Instructions)
			c.bf.Emit(OpJump, 0, 0, 0)
			// 7. SHORT: restore original value.
			shortPC := len(c.bf.Instructions)
			c.bf.Instructions[jumpShortIdx].OperandA = uint8(shortPC)
			c.bf.Emit(OpLdar, uint8(regVal), 0, 0)
			// 8. END: patch jump.
			c.bf.Instructions[jumpEndIdx].OperandA = uint8(len(c.bf.Instructions))
		} else {
			// Non-computed: obj.prop ||= val
			// 1. Compile object → regObj
			c.compileExpression(left.Object)
			regObj := c.allocReg()
			c.bf.Emit(OpStar, uint8(regObj), 0, 0)
			// 2. Load obj.prop → regVal
			propName := c.stringConstant(left.Property.(*Identifier).Name)
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaNamedProperty, uint8(propName), 0, uint8(slot))
			regVal := c.allocReg()
			c.bf.Emit(OpStar, uint8(regVal), 0, 0)
			// 3. Short-circuit check: jump to SHORT if condition met.
			jumpShortIdx := len(c.bf.Instructions)
			switch mode {
			case "truthy":
				c.bf.Emit(OpJumpIfToBooleanTrue, 0, 0, 0)
			case "falsy":
				c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
			case "nullish":
				c.bf.Emit(OpJumpIfNotNullish, 0, 0, 0)
			}
			// 4. Evaluate RHS → regRhs, store to obj.prop.
			c.compileExpression(expr.Right)
			regRhs := c.allocReg()
			c.bf.Emit(OpStar, uint8(regRhs), 0, 0)
			c.bf.Emit(OpLdar, uint8(regObj), 0, 0) // acc = object
			c.bf.Emit(OpStaNamedProperty, uint8(propName), uint8(regRhs), uint8(slot))
			// 5. Load RHS result → acc, jump to END.
			c.bf.Emit(OpLdar, uint8(regRhs), 0, 0)
			jumpEndIdx := len(c.bf.Instructions)
			c.bf.Emit(OpJump, 0, 0, 0)
			// 6. SHORT: restore original value.
			shortPC := len(c.bf.Instructions)
			c.bf.Instructions[jumpShortIdx].OperandA = uint8(shortPC)
			c.bf.Emit(OpLdar, uint8(regVal), 0, 0)
			// 7. END: patch jump.
			c.bf.Instructions[jumpEndIdx].OperandA = uint8(len(c.bf.Instructions))
		}
	}
}

func (c *Compiler) compileIdentifierStore(ident *Identifier) {
	if len(c.scopes) > 0 {
		if sv, ok := c.scopes[len(c.scopes)-1].lookup(ident.Name); ok {
			if sv.kind == "const" {
				nameIdx := c.stringConstant(ident.Name)
				c.bf.Emit(OpThrowConstAssignment, uint8(nameIdx), 0, 0)
				return
			}
			if sv.kind == "let" || sv.kind == "const" || sv.kind == "var" {
				c.bf.Emit(OpStar, uint8(sv.reg), 0, 0)
				return
			}
		}
	}
	if reg, ok := c.locals[ident.Name]; ok {
		c.bf.Emit(OpStar, uint8(reg), 0, 0)
		return
	}
	nameIdx := c.stringConstant(ident.Name)
	c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
}

func (c *Compiler) compileMemberAssignment(member *MemberExpression) {
	// acc contains the value to assign.
	// Compile object, store property.
	if member.Computed {
		regVal := c.allocReg()
		c.bf.Emit(OpStar, uint8(regVal), 0, 0)
		c.compileExpression(member.Object)
		regObj := c.allocReg()
		c.bf.Emit(OpStar, uint8(regObj), 0, 0)
		c.compileExpression(member.Property) // key in acc
		c.bf.Emit(OpStaKeyedProperty, uint8(regObj), uint8(regVal), 0)
	} else {
		regVal := c.allocReg()
		c.bf.Emit(OpStar, uint8(regVal), 0, 0)
		c.compileExpression(member.Object)
		propName := c.stringConstant(member.Property.(*Identifier).Name)
		slot := c.allocFeedbackSlot()
		c.bf.Emit(OpStaNamedProperty, uint8(propName), uint8(regVal), uint8(slot))
	}
}

func (c *Compiler) compileCallExpression(expr *CallExpression) {
	// Detect spread arguments.
	hasSpread := false
	for _, arg := range expr.Arguments {
		if _, ok := arg.(*SpreadExpression); ok {
			hasSpread = true
			break
		}
	}

	// Member expression calls always use the new convention (with thisReg).
	if member, ok := expr.Callee.(*MemberExpression); ok {
		c.compileExpression(member.Object)
		thisReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(thisReg), 0, 0)
		c.bf.Emit(OpLdar, uint8(thisReg), 0, 0)
		if member.Computed {
			c.compileExpression(member.Property)
			c.bf.Emit(OpLdaKeyedProperty, uint8(thisReg), 0, 0)
		} else {
			propName := c.stringConstant(member.Property.(*Identifier).Name)
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaNamedProperty, uint8(propName), 0, uint8(slot))
		}
		calleeReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)
		if hasSpread {
			c.compileCallArgsWithSpread(expr.Arguments, calleeReg, uint8(thisReg))
		} else {
			argRegs := make([]int, len(expr.Arguments))
			for i := range expr.Arguments {
				argRegs[i] = c.allocReg()
			}
			for i, arg := range expr.Arguments {
				c.compileExpression(arg)
				c.bf.Emit(OpStar, uint8(argRegs[i]), 0, 0)
			}
			c.bf.Emit(OpCall, uint8(calleeReg), uint8(len(expr.Arguments)), uint8(thisReg))
		}
		return
	}

	// super() call: emit OpSuperCall instead of OpCall.
	// Do NOT compile the callee normally (which would emit OpLdaThis
	// and trigger the this-before-super check prematurely). Instead,
	// OpSuperCall handler will load the parent constructor from the frame.
	if _, ok := expr.Callee.(*SuperExpression); ok {
		argRegs := make([]int, len(expr.Arguments))
		for i := range expr.Arguments {
			argRegs[i] = c.allocReg()
		}
		for i, arg := range expr.Arguments {
			c.compileExpression(arg)
			c.bf.Emit(OpStar, uint8(argRegs[i]), 0, 0)
		}
		c.bf.Emit(OpSuperCall, 0, uint8(len(expr.Arguments)), 0)
		return
	}

	// Non-member call: compile callee first, then args after.
	c.compileExpression(expr.Callee)
	calleeReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)
	if hasSpread {
		c.compileCallArgsWithSpread(expr.Arguments, calleeReg, 0)
	} else {
		argRegs := make([]int, len(expr.Arguments))
		for i := range expr.Arguments {
			argRegs[i] = c.allocReg()
		}
		for i, arg := range expr.Arguments {
			c.compileExpression(arg)
			c.bf.Emit(OpStar, uint8(argRegs[i]), 0, 0)
		}
		c.bf.Emit(OpCall, uint8(calleeReg), uint8(len(expr.Arguments)), 0)
	}
}

// compileCallArgsWithSpread handles call arguments that contain at least one
// SpreadExpression. Fixed (non-spread) args are compiled into consecutive
// registers starting at calleeReg+1, then the spread expression is compiled
// and stored in the next register. Emits OpCallSpread with the fixed arg count
// so the VM can unpack the array at runtime.
func (c *Compiler) compileCallArgsWithSpread(args []Node, calleeReg int, thisReg uint8) {
	// Collect fixed (non-spread) args and find the spread arg.
	fixedArgs := make([]Node, 0, len(args))
	var spreadArg *SpreadExpression
	for _, arg := range args {
		if sp, ok := arg.(*SpreadExpression); ok {
			spreadArg = sp
			break // handle first spread; subsequent ones would need more complex logic
		} else {
			fixedArgs = append(fixedArgs, arg)
		}
	}
	// Allocate registers for fixed args and compile them.
	for _, arg := range fixedArgs {
		argReg := c.allocReg()
		c.compileExpression(arg)
		c.bf.Emit(OpStar, uint8(argReg), 0, 0)
	}
	// Compile the spread expression: load the array and store it next.
	if spreadArg != nil {
		c.compileExpression(spreadArg.Argument)
		spreadReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(spreadReg), 0, 0)
	}
	c.bf.Emit(OpCallSpread, uint8(calleeReg), uint8(len(fixedArgs)), thisReg)
}

func (c *Compiler) compileMemberExpression(expr *MemberExpression) {
	c.compileExpression(expr.Object)
	if expr.Computed {
		regObj := c.allocReg()
		c.bf.Emit(OpStar, uint8(regObj), 0, 0)
		c.compileExpression(expr.Property)
		slot := c.allocFeedbackSlot()
		c.bf.Emit(OpLdaKeyedProperty, uint8(regObj), uint8(slot), 0)
	} else {
		propName := c.stringConstant(expr.Property.(*Identifier).Name)
		slot := c.allocFeedbackSlot()
		c.bf.Emit(OpLdaNamedProperty, uint8(propName), 0, uint8(slot))
	}
}

func (c *Compiler) compileOptionalMemberExpression(expr *OptionalMemberExpression) {
	c.compileExpression(expr.Object)
	objReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(objReg), 0, 0)
	// Null check: if obj == null, jump to undefined short-circuit.
	c.bf.Emit(OpLdaNull, 0, 0, 0)
	c.bf.Emit(OpStrictEq, uint8(objReg), 0, 0)
	scIdx := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfTrue, 0, 0, 0) // jump to short-circuit if null
	// Check undefined too: obj === undefined → short-circuit.
	c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	c.bf.Emit(OpStrictEq, uint8(objReg), 0, 0)
	scIdx2 := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfTrue, 0, 0, 0) // jump to short-circuit if undefined
	// Not nullish: do property access.
	c.bf.Emit(OpLdar, uint8(objReg), 0, 0)
	if !expr.Computed {
		nameIdx := c.stringConstant(expr.Property.(*Identifier).Name)
		slot := c.allocFeedbackSlot()
		c.bf.Emit(OpLdaNamedProperty, uint8(nameIdx), 0, uint8(slot))
	} else {
		// Save object from acc, then compile key expression.
		objReg2 := c.allocReg()
		c.bf.Emit(OpStar, uint8(objReg2), 0, 0)
		c.compileExpression(expr.Property)
		slot := c.allocFeedbackSlot()
		c.bf.Emit(OpLdaKeyedProperty, uint8(objReg2), uint8(slot), 0)
	}
	endIdx := len(c.bf.Instructions)
	c.bf.Emit(OpJump, 0, 0, 0) // jump to end
	// Short-circuit: emit undefined and patch jumps.
	scTarget := uint8(len(c.bf.Instructions))
	c.bf.Instructions[scIdx].OperandA = scTarget
	c.bf.Instructions[scIdx2].OperandA = scTarget
	c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	c.bf.Instructions[endIdx].OperandA = uint8(len(c.bf.Instructions))
}

func (c *Compiler) compileOptionalCallExpression(expr *OptionalCallExpression) {
	c.compileExpression(expr.Callee)
	calleeReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)
	// Null check.
	c.bf.Emit(OpLdaNull, 0, 0, 0)
	c.bf.Emit(OpStrictEq, uint8(calleeReg), 0, 0)
	scIdx := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfTrue, 0, 0, 0) // jump to short-circuit if null
	// Check undefined too.
	c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	c.bf.Emit(OpStrictEq, uint8(calleeReg), 0, 0)
	scIdx2 := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfTrue, 0, 0, 0) // jump to short-circuit if undefined
	// Not nullish: compile call.
	c.bf.Emit(OpLdar, uint8(calleeReg), 0, 0)
	argRegs := make([]int, len(expr.Arguments))
	for i := range expr.Arguments {
		argRegs[i] = c.allocReg()
	}
	for i, arg := range expr.Arguments {
		c.compileExpression(arg)
		c.bf.Emit(OpStar, uint8(argRegs[i]), 0, 0)
	}
	c.bf.Emit(OpCall, uint8(calleeReg), uint8(len(expr.Arguments)), 0)
	endIdx := len(c.bf.Instructions)
	c.bf.Emit(OpJump, 0, 0, 0) // jump to end
	// Short-circuit.
	scTarget := uint8(len(c.bf.Instructions))
	c.bf.Instructions[scIdx].OperandA = scTarget
	c.bf.Instructions[scIdx2].OperandA = scTarget
	c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	c.bf.Instructions[endIdx].OperandA = uint8(len(c.bf.Instructions))
}

func (c *Compiler) compileUpdateExpression(expr *UpdateExpression) {
	// If the argument is a simple Identifier bound to a local/scope register,
	// emit OpInc/OpDec directly on that register. This avoids the register
	// allocation conflict where the loop body reads from the variable's
	// original register but the update writes to a new one.
	if ident, ok := expr.Argument.(*Identifier); ok {
		if reg, ok := c.locals[ident.Name]; ok {
			c.compileUpdateLocal(expr, reg)
			return
		}
		// Check scope chain.
		if len(c.scopes) > 0 {
			curScope := c.scopes[len(c.scopes)-1]
			if sv, ok := curScope.names[ident.Name]; ok {
				c.compileUpdateLocal(expr, sv.reg)
				return
			}
		}
	}

	// Fallback: global variable path (or member expression).
	varName := ""
	switch arg := expr.Argument.(type) {
	case *Identifier:
		varName = arg.Name
	}

	nameIdx := c.stringConstant(varName)
	c.bf.Emit(OpLdaGlobal, uint8(nameIdx), 0, 0)

	// Save old value for postfix return.
	oldReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(oldReg), 0, 0)

	// Copy to working register for OpInc/OpDec.
	workReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(workReg), 0, 0)

	// Increment/decrement working copy.
	if expr.Operator == "++" {
		c.bf.Emit(OpInc, uint8(workReg), 0, 0)
	} else {
		c.bf.Emit(OpDec, uint8(workReg), 0, 0)
	}

	// Store new value from workReg back to global.
	c.bf.Emit(OpLdar, uint8(workReg), 0, 0)
	c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)

	// Return appropriate value.
	if expr.Prefix {
		c.bf.Emit(OpLdar, uint8(workReg), 0, 0)
	} else {
		c.bf.Emit(OpLdar, uint8(oldReg), 0, 0)
	}
}

// compileUpdateLocal emits OpInc/OpDec directly on a local variable register.
func (c *Compiler) compileUpdateLocal(expr *UpdateExpression, reg int) {
	op := OpInc
	if expr.Operator == "--" {
		op = OpDec
	}
	if expr.Prefix {
		// ++i: increment in place, result is new value (already in acc).
		c.bf.Emit(op, uint8(reg), 0, 0)
	} else {
		// i++: save old value, increment in place, return old.
		c.bf.Emit(OpLdar, uint8(reg), 0, 0) // load old value → acc
		oldReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(oldReg), 0, 0) // acc → oldReg
		c.bf.Emit(op, uint8(reg), 0, 0)         // OpInc/OpDec reg in place
		c.bf.Emit(OpLdar, uint8(oldReg), 0, 0) // return old value
	}
}

func (c *Compiler) compileConditionalExpression(expr *ConditionalExpression) {
	// test ? consequent : alternate
	c.compileExpression(expr.Test)
	jumpFalsePos := len(c.bf.Instructions)
	c.bf.Emit(OpJumpIfFalse, 0, 0, 0)

	// Consequent.
	c.compileExpression(expr.Consequent)
	jumpEndPos := len(c.bf.Instructions)
	c.bf.Emit(OpJump, 0, 0, 0)

	// Patch JumpIfFalse to point to alternate.
	c.bf.Instructions[jumpFalsePos].OperandA = uint8(len(c.bf.Instructions))

	// Alternate.
	c.compileExpression(expr.Alternate)

	// Patch jump-to-end to point past alternate.
	c.bf.Instructions[jumpEndPos].OperandA = uint8(len(c.bf.Instructions))
}

func (c *Compiler) compileObjectExpression(expr *ObjectExpression) {
	// Fast path: if no spread properties, no computed keys, no shorthands,
	// and no getters/setters, pre-build the Shape and use OpCreateObjectLiteral
	// to skip per-property shape transitions.
	hasSpread := false
	hasComputed := false
	hasAccessor := false
	for _, prop := range expr.Properties {
		if _, ok := prop.Value.(*SpreadExpression); ok {
			hasSpread = true
		}
		if prop.Computed {
			hasComputed = true
		}
		if prop.IsGetter || prop.IsSetter {
			hasAccessor = true
		}
	}

	if !hasSpread && !hasComputed && !hasAccessor && len(expr.Properties) > 0 {
		// Collect property names and pre-build the Shape.
		propNames := make([]string, len(expr.Properties))
		for i, prop := range expr.Properties {
			propNames[i] = prop.Key
		}
		shape := GetOrCreateShape(propNames) // cache the shape

		// Store the shape key string in the constant pool and the pre-split
		// prop names in ShapePropNames so the VM can avoid strings.Split.
		shapeKeyIdx := len(c.bf.Constants)
		c.bf.AddConstant(NewString(strings.Join(propNames, ",")))
		// Grow ShapePropNames and Shapes to index by constant pool index.
		for len(c.bf.ShapePropNames) <= shapeKeyIdx {
			c.bf.ShapePropNames = append(c.bf.ShapePropNames, nil)
			c.bf.Shapes = append(c.bf.Shapes, nil)
		}
		c.bf.ShapePropNames[shapeKeyIdx] = propNames
		c.bf.Shapes[shapeKeyIdx] = shape
		c.bf.Emit(OpCreateObjectLiteral, uint8(shapeKeyIdx), 0, 0)
		objReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(objReg), 0, 0)

		// Set properties by offset — skip Set() and shape lookups entirely.
		for _, prop := range expr.Properties {
			if prop.Shorthand {
				// Shorthand: {x} → load x from scope
				c.compileExpression(&Identifier{Name: prop.Key})
			} else {
				c.compileExpression(prop.Value)
			}
			valReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)
			offset := shape.GetOffset(prop.Key)
			c.bf.Emit(OpStaByOffset, uint8(objReg), uint8(offset), uint8(valReg))
		}

		c.bf.Emit(OpLdar, uint8(objReg), 0, 0)
		return
	}

	// Slow path: spread properties, computed keys, or empty object.
	c.bf.Emit(OpCreateObject, 0, 0, 0)
	objReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(objReg), 0, 0)

	for _, prop := range expr.Properties {
		// Check for spread: { ...src }
		if spread, ok := prop.Value.(*SpreadExpression); ok {
			// Merge properties from source object.
			c.compileExpression(spread.Argument)
			srcReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(srcReg), 0, 0)

			// Iterate source keys.
			c.bf.Emit(OpForInSetup, uint8(srcReg), 0, 0)
			mergeLoopStart := len(c.bf.Instructions)
			c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
			exitMergeJump := len(c.bf.Instructions) - 1

			keyReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(keyReg), 0, 0)

			// Get source[key]: key in acc, obj in operandA.
			c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)  // key → acc
			c.bf.Emit(OpLdaKeyedProperty, uint8(srcReg), 0, 0) // src[acc] → acc
			valReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)

			// Set obj[key] = val: key in acc, obj in operandA, val in operandB.
			c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)  // key → acc
			c.bf.Emit(OpStaKeyedProperty, uint8(objReg), uint8(valReg), 0)

			c.bf.Emit(OpForInNext, uint8(srcReg), 0, 0)
			c.bf.Emit(OpJump, uint8(mergeLoopStart), 0, 0)
			c.bf.Instructions[exitMergeJump].OperandA = uint8(len(c.bf.Instructions))
			continue
		}

		// Compute the value.
		if prop.Shorthand {
			c.compileExpression(&Identifier{Name: prop.Key})
		} else {
			c.compileExpression(prop.Value)
		}
		valReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(valReg), 0, 0)

		c.bf.Emit(OpLdar, uint8(objReg), 0, 0)

		if prop.Computed {
			// Computed key: obj[expr] = val
			c.compileExpression(prop.ComputedKey)
			keyReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(keyReg), 0, 0)
			c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)
			c.bf.Emit(OpStaKeyedProperty, uint8(objReg), uint8(valReg), 0)
		} else if prop.IsGetter || prop.IsSetter {
			// Accessor property: define getter/setter via SetAccessor.
			propNameIdx := c.stringConstant(prop.Key)
			// OperandC: 0 = getter, 1 = setter
			accessorFlag := uint8(0)
			if prop.IsSetter {
				accessorFlag = 1
			}
			c.bf.Emit(OpDefineAccessorProperty, uint8(propNameIdx), uint8(valReg), accessorFlag)
		} else {
			// Regular property.
			propNameIdx := c.stringConstant(prop.Key)
			c.bf.Emit(OpStaNamedProperty, uint8(propNameIdx), uint8(valReg), 255)
		}
	}

	// Leave object in accumulator.
	c.bf.Emit(OpLdar, uint8(objReg), 0, 0)
}

func (c *Compiler) compileArrayExpression(expr *ArrayExpression) {
	c.bf.Emit(OpCreateArray, 0, 0, 0)
	arrReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(arrReg), 0, 0)

	elemIndex := 0
	for _, elem := range expr.Elements {
		if elem == nil {
			// Hole (elision): skip this index, leaving the slot empty.
			elemIndex++
			continue
		}
		if spread, ok := elem.(*SpreadExpression); ok {
			// [...source]: iterate source using ForInSetup/ForInNext.
			c.compileExpression(spread.Argument)
			srcReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(srcReg), 0, 0)

			// Iterate source keys using ForInSetup.
			c.bf.Emit(OpForInSetup, uint8(srcReg), 0, 0)
			spreadLoopStart := len(c.bf.Instructions)
			c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
			exitSpreadJump := len(c.bf.Instructions) - 1

			// Current key is in acc; store it.
			keyReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(keyReg), 0, 0)

			// Get source[key]: key in acc, obj in operandA.
			c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)  // key → acc
			c.bf.Emit(OpLdaKeyedProperty, uint8(srcReg), 0, 0) // src[acc] → acc
			valReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)

			// Set target[key] = val: key in acc, obj in operandA, val in operandB.
			c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)  // key → acc
			c.bf.Emit(OpStaKeyedProperty, uint8(arrReg), uint8(valReg), 0)

			c.bf.Emit(OpForInNext, uint8(srcReg), 0, 0)
			c.bf.Emit(OpJump, uint8(spreadLoopStart), 0, 0)

			// Patch exit jump.
			c.bf.Instructions[exitSpreadJump].OperandA = uint8(len(c.bf.Instructions))

			// Skip elemIndex tracking; set length at end separately.
			elemIndex++ // approximate — actual count from ForIn iteration
		} else {
			// Store element value in a register.
			c.compileExpression(elem)
			valReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)
			// Load array object into acc.
			c.bf.Emit(OpLdar, uint8(arrReg), 0, 0)
			// Set key in acc to the index string.
			keyIdx := c.stringConstant(intKey(elemIndex))
			c.bf.Emit(OpLdaConstant, uint8(keyIdx), 0, 0)
			c.bf.Emit(OpStaKeyedProperty, uint8(arrReg), uint8(valReg), 0)
			elemIndex++
		}
	}

	// Set length and leave array in acc.
	c.bf.Emit(OpLdar, uint8(arrReg), 0, 0)
	lenValIdx := c.bf.AddConstant(NewNumber(float64(elemIndex)))
	c.bf.Emit(OpLdaConstant, uint8(lenValIdx), 0, 0)
	lenValReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(lenValReg), 0, 0)
	c.bf.Emit(OpLdar, uint8(arrReg), 0, 0)
	lenIdx := c.stringConstant("length")
	c.bf.Emit(OpStaNamedProperty, uint8(lenIdx), uint8(lenValReg), 255)
	c.bf.Emit(OpLdar, uint8(arrReg), 0, 0)
}

func (c *Compiler) compileFunctionExpression(expr *FunctionExpression) {
	var parentScope *Scope
	if len(c.scopes) > 0 {
		parentScope = c.scopes[len(c.scopes)-1]
	}
	paramNamesList := paramNames(expr.Params)
	innerBF := CompileFunctionWithParent(expr.Name, paramNamesList, expr.Params, expr.Body, parentScope)
	if expr.Generator || expr.Async {
		innerBF.Generator = true
	}
	if expr.Async {
		innerBF.Async = true
	}
	if expr.Generator && expr.Async {
		innerBF.AsyncGenerator = true
	}
	templateObj := NewJSObject()
	templateObj.ConstructorName = "Function"
	templateObj.Bytecode = innerBF
	templateIdx := c.bf.AddConstant(NewObject(templateObj))
	c.bf.Emit(OpLdaConstant, uint8(templateIdx), 0, 0)
	c.bf.Emit(OpCreateClosure, 0, 0, 0)
}

func (c *Compiler) compileArrowFunction(expr *ArrowFunctionExpression) {
	var parentScope *Scope
	if len(c.scopes) > 0 {
		parentScope = c.scopes[len(c.scopes)-1]
	}
	// Build a BlockStatement from the body for consistency.
	var bodyStmts *BlockStatement
	if block, ok := expr.Body.(*BlockStatement); ok {
		bodyStmts = block
	} else {
		// Expression body: wrap in return statement.
		bodyStmts = &BlockStatement{Body: []Node{
			&ReturnStatement{Argument: expr.Body},
		}}
	}
	paramNamesList := paramNames(expr.Params)
	innerBF := CompileFunctionWithParent("", paramNamesList, expr.Params, bodyStmts, parentScope)
	if expr.Async {
		innerBF.Generator = true
		innerBF.Async = true
	}
	templateObj := NewJSObject()
	templateObj.ConstructorName = "Function"
	templateObj.Bytecode = innerBF
	templateIdx := c.bf.AddConstant(NewObject(templateObj))
	c.bf.Emit(OpLdaConstant, uint8(templateIdx), 0, 0)
	c.bf.Emit(OpCreateClosure, 0, 0, 0)
}

func (c *Compiler) compileNewExpression(expr *NewExpression) {
	// Compile constructor first so calleeReg is known, then args placed right after.
	c.compileExpression(expr.Callee)
	calleeReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)

	// Pre-allocate argument registers at calleeReg+1, calleeReg+2, ...
	// This MUST happen before compiling arguments because compileExpression
	// for inline objects/functions may internally allocate registers, which
	// would shift the positions if we allocated after compilation.
	argRegs := make([]int, len(expr.Arguments))
	for i := range expr.Arguments {
		argRegs[i] = c.allocReg()
	}

	// Compile arguments and store into pre-allocated register slots.
	for i, arg := range expr.Arguments {
		c.compileExpression(arg)
		c.bf.Emit(OpStar, uint8(argRegs[i]), 0, 0)
	}

	// Create new object.
	c.bf.Emit(OpCreateObject, 0, 0, 0)
	newObjReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(newObjReg), 0, 0)

	// Read constructor.prototype and set it as new object's prototype.
	c.bf.Emit(OpLdar, uint8(calleeReg), 0, 0)
	protoIdx := c.stringConstant("prototype")
	protoSlot := c.allocFeedbackSlot()
	c.bf.Emit(OpLdaNamedProperty, uint8(protoIdx), 0, uint8(protoSlot))
	c.bf.Emit(OpSetPrototype, uint8(newObjReg), 0, 0)

	// Call constructor with newObj as this.
	c.bf.Emit(OpCall, uint8(calleeReg), uint8(len(expr.Arguments)), uint8(newObjReg))
}

// compileDestructuringAssignment compiles var {a, b} = expr or var [x, y] = arr.
// Supports nested patterns, rest elements, default values, and rename.
func (c *Compiler) compileDestructuringAssignment(da *DestructuringAssignment) {
	// Compile the right-hand side and store it in a register.
	if da.Right != nil {
		c.compileExpression(da.Right)
	} else {
		c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	}
	objReg := c.allocReg()
	c.bf.Emit(OpStar, uint8(objReg), 0, 0)

	// Track if we need to create a rest object (for {...rest} in object patterns).
	restIdx := -1
	var restVarName string

	for idx, elem := range da.Elements {
		// Handle rest element: [...rest] or {...rest}
		if elem.Rest {
			restIdx = idx
			restVarName = elem.Key
			continue // rest element is processed after the loop
		}

		// Elision: empty slot in array destructuring
		if elem.Key == "" && elem.Nested == nil {
			continue
		}

		// Nested destructuring pattern
		if elem.Nested != nil {
			// Load the nested source value first.
			c.bf.Emit(OpLdar, uint8(objReg), 0, 0)

			// Access the nested source property.
			if da.ArrayMode {
				// Array: access by numeric index
				idxStr := c.stringConstant(intKey(idx))
				c.bf.Emit(OpLdaConstant, uint8(idxStr), 0, 0)
				slot := c.allocFeedbackSlot()
				c.bf.Emit(OpLdaKeyedProperty, uint8(objReg), uint8(slot), 0)
			} else {
				// Object: access by source key or element key
				sourceKey := elem.SourceKey
				if sourceKey == "" {
					sourceKey = elem.Key
				}
				propIdx := c.stringConstant(sourceKey)
				slot := c.allocFeedbackSlot()
				c.bf.Emit(OpLdaNamedProperty, uint8(propIdx), 0, uint8(slot))
			}

			// Store nested source in a temp register and recurse.
			nestedReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(nestedReg), 0, 0)
			// Temporarily swap the Right to signal the compiler to use the register.
			// Recurse: compileDestructuringAssignment on the nested pattern.
			// We inline the nested compilation by emitting the same pattern of bytecode,
			// but using nestedReg as the source.
			c.compileNestedDestructuring(elem.Nested, nestedReg)
			continue
		}

		// Leaf element: load source and extract value.
		c.bf.Emit(OpLdar, uint8(objReg), 0, 0)

		// Access property: for object mode use named property, for array mode use index.
		if da.ArrayMode {
			// Array destructuring: access by numeric index.
			idxStr := c.stringConstant(intKey(idx))
			c.bf.Emit(OpLdaConstant, uint8(idxStr), 0, 0)
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaKeyedProperty, uint8(objReg), uint8(slot), 0)
		} else {
			// Object destructuring: access by named property.
			// For rename {srcKey: targetVar}, use srcKey as the property name.
			sourceKey := elem.SourceKey
			if sourceKey == "" {
				sourceKey = elem.Key
			}
			propIdx := c.stringConstant(sourceKey)
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaNamedProperty, uint8(propIdx), 0, uint8(slot))
		}

		// Check for default value: if the extracted value is undefined and a default exists, use it.
		if elem.Default != nil {
			valReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)
			// Compare valReg with undefined.
			c.bf.Emit(OpLdaUndefined, 0, 0, 0)
			c.bf.Emit(OpStrictEq, uint8(valReg), 0, 0)
			// Jump past default evaluation if not strictly equal to undefined.
			jumpIdx := len(c.bf.Instructions)
			c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
			// Evaluate default expression and store over valReg.
			c.compileExpression(elem.Default)
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)
			// Patch the jump: skip past the default evaluation.
			c.bf.Instructions[jumpIdx].OperandA = uint8(len(c.bf.Instructions))
			// Load the (possibly defaulted) value back into acc.
			c.bf.Emit(OpLdar, uint8(valReg), 0, 0)
		}

		// Store to variable.
		varName := elem.Key
		if varName == "" {
			continue // should not happen for leaf elements
		}
		c.storeDestructuredBinding(varName, da.Kind)
	}

	// Handle rest element: create array/object with remaining elements.
	if restIdx >= 0 {
		if da.ArrayMode {
			// [...rest]: call source.slice(restIdx).
			// Load "slice" method name.
			sliceIdx := c.stringConstant("slice")
			c.bf.Emit(OpLdar, uint8(objReg), 0, 0) // source array → acc
			sourceReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(sourceReg), 0, 0) // save source

			// Get source.slice
			c.bf.Emit(OpLdar, uint8(sourceReg), 0, 0)
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaNamedProperty, uint8(sliceIdx), 0, uint8(slot)) // source.slice → acc
			calleeReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)

			// Arg: restIdx as number
			restIdxConst := c.stringConstant(intKey(restIdx))
			c.bf.Emit(OpLdaConstant, uint8(restIdxConst), 0, 0)
			c.bf.Emit(OpStar, uint8(calleeReg)+1, 0, 0) // arg0 at callee+1

			// Call: slice.call(source, restIdx)
			c.bf.Emit(OpLdar, uint8(sourceReg), 0, 0)    // source as `this`
			c.bf.Emit(OpCall, uint8(calleeReg), 1, uint8(sourceReg))

			// Result is in acc, ready for store.
		} else {
			// {...rest}: use Object.assign-like approach. Create empty object,
			// iterate source keys, skip extracted keys, copy rest.
			c.bf.Emit(OpCreateObject, 0, 0, 0)
			restObjReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(restObjReg), 0, 0)

			// Collect extracted property names to skip.
			extractedKeys := make([]string, 0, len(da.Elements))
			for _, e := range da.Elements {
				if e.Rest {
					continue
				}
				k := e.SourceKey
				if k == "" {
					k = e.Key
				}
				if k != "" {
					extractedKeys = append(extractedKeys, k)
				}
			}

			// ForIn over source: standard pattern.
			// OpForInSetup puts first key (or Undefined) in acc.
			c.bf.Emit(OpForInSetup, uint8(objReg), 0, 0)

			loopStart := len(c.bf.Instructions)
			// If acc is falsy (Undefined), exit loop.
			c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
			jumpEndIdx := len(c.bf.Instructions) - 1

			// Save current key.
			keyReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(keyReg), 0, 0)

			// Skip if key matches any extracted key.
			shouldSkip := false
			for _, ek := range extractedKeys {
				ekIdx := c.stringConstant(ek)
				// Store current key in a temp reg for comparison.
				c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)
				cmpReg := c.allocReg()
				c.bf.Emit(OpStar, uint8(cmpReg), 0, 0)
				// Load constant, then compare.
				c.bf.Emit(OpLdaConstant, uint8(ekIdx), 0, 0) // acc = constant
				c.bf.Emit(OpStrictEq, uint8(cmpReg), 0, 0)    // cmpReg === acc ?
				eqJumpIdx := len(c.bf.Instructions)
				c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
				// Found extracted key → skip to next iteration after getting next key.
				c.bf.Emit(OpForInNext, uint8(objReg), 0, 0)
				c.bf.Emit(OpJump, uint8(loopStart), 0, 0)
				shouldSkip = true
				// Patch: if not equal, continue to copy or next check.
				c.bf.Instructions[eqJumpIdx].OperandA = uint8(len(c.bf.Instructions))
			}
			_ = shouldSkip

			// Copy: restObj[key] = source[key]
			c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)
			c.bf.Emit(OpLdaKeyedProperty, uint8(objReg), 0, 0) // source[key] → acc
			valReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)
			c.bf.Emit(OpLdar, uint8(restObjReg), 0, 0)
			c.bf.Emit(OpLdar, uint8(keyReg), 0, 0) // key → acc
			c.bf.Emit(OpStaKeyedProperty, uint8(restObjReg), uint8(valReg), 0)

			// Next key.
			c.bf.Emit(OpForInNext, uint8(objReg), 0, 0)
			c.bf.Emit(OpJump, uint8(loopStart), 0, 0)

			// Patch exit jump.
			c.bf.Instructions[jumpEndIdx].OperandA = uint8(len(c.bf.Instructions))

			c.bf.Emit(OpLdar, uint8(restObjReg), 0, 0)
		}
		// Store rest variable.
		c.storeDestructuredBinding(restVarName, da.Kind)
	}
}

// compileDestructuredParam compiles a destructuring pattern for a function parameter.
// The parameter value is in sourceReg; we destructure it into individual variables.
func (c *Compiler) compileDestructuredParam(da *DestructuringAssignment, sourceReg int) {
	restIdx := -1
	var restVarName string

	for idx, elem := range da.Elements {
		if elem.Rest {
			restIdx = idx
			restVarName = elem.Key
			continue
		}

		if elem.Key == "" && elem.Nested == nil {
			continue // elision
		}

		if elem.Nested != nil {
			// Load source and access nested property.
			c.bf.Emit(OpLdar, uint8(sourceReg), 0, 0)
			if da.ArrayMode {
				idxStr := c.stringConstant(intKey(idx))
				c.bf.Emit(OpLdaConstant, uint8(idxStr), 0, 0)
				slot := c.allocFeedbackSlot()
				c.bf.Emit(OpLdaKeyedProperty, uint8(sourceReg), uint8(slot), 0)
			} else {
				sourceKey := elem.SourceKey
				if sourceKey == "" {
					sourceKey = elem.Key
				}
				propIdx := c.stringConstant(sourceKey)
				slot := c.allocFeedbackSlot()
				c.bf.Emit(OpLdaNamedProperty, uint8(propIdx), 0, uint8(slot))
			}
			nestedReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(nestedReg), 0, 0)
			c.compileDestructuredParam(elem.Nested, nestedReg)
			continue
		}

		// Leaf element.
		c.bf.Emit(OpLdar, uint8(sourceReg), 0, 0)
		if da.ArrayMode {
			idxStr := c.stringConstant(intKey(idx))
			c.bf.Emit(OpLdaConstant, uint8(idxStr), 0, 0)
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaKeyedProperty, uint8(sourceReg), uint8(slot), 0)
		} else {
			sourceKey := elem.SourceKey
			if sourceKey == "" {
				sourceKey = elem.Key
			}
			propIdx := c.stringConstant(sourceKey)
			slot := c.allocFeedbackSlot()
			c.bf.Emit(OpLdaNamedProperty, uint8(propIdx), 0, uint8(slot))
		}

		if elem.Default != nil {
			valReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)
			c.bf.Emit(OpLdaUndefined, 0, 0, 0)
			c.bf.Emit(OpStrictEq, uint8(valReg), 0, 0)
			jumpIdx := len(c.bf.Instructions)
			c.bf.Emit(OpJumpIfToBooleanFalse, 0, 0, 0)
			c.compileExpression(elem.Default)
			c.bf.Emit(OpStar, uint8(valReg), 0, 0)
			c.bf.Instructions[jumpIdx].OperandA = uint8(len(c.bf.Instructions))
			c.bf.Emit(OpLdar, uint8(valReg), 0, 0)
		}

		if elem.Key != "" {
			c.storeDestructuredBinding(elem.Key, da.Kind)
		}
	}

	// Handle rest element for function params.
	if restIdx >= 0 {
		// Call source.slice(restIdx) — same approach as compileDestructuringAssignment.
		sliceIdx := c.stringConstant("slice")
		c.bf.Emit(OpLdar, uint8(sourceReg), 0, 0)
		srcCopyReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(srcCopyReg), 0, 0)

		c.bf.Emit(OpLdar, uint8(srcCopyReg), 0, 0)
		slot := c.allocFeedbackSlot()
		c.bf.Emit(OpLdaNamedProperty, uint8(sliceIdx), 0, uint8(slot))
		calleeReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)

		restIdxConst := c.stringConstant(intKey(restIdx))
		c.bf.Emit(OpLdaConstant, uint8(restIdxConst), 0, 0)
		c.bf.Emit(OpStar, uint8(calleeReg)+1, 0, 0)

		c.bf.Emit(OpLdar, uint8(srcCopyReg), 0, 0)
		c.bf.Emit(OpCall, uint8(calleeReg), 1, uint8(srcCopyReg))

		c.storeDestructuredBinding(restVarName, da.Kind)
	}
}

// compileNestedDestructuring compiles a nested destructuring pattern using a source register.
// Delegates to compileDestructuredParam which handles the same logic.
func (c *Compiler) compileNestedDestructuring(da *DestructuringAssignment, sourceReg int) {
	c.compileDestructuredParam(da, sourceReg)
}

// storeDestructuredBinding stores the value in the accumulator to a variable.
// In function scopes, "var" creates a local binding; at the top level, it creates a global.
func (c *Compiler) storeDestructuredBinding(name string, kind string) {
	nameIdx := c.stringConstant(name)
	if kind == "let" || kind == "const" || len(c.scopes) > 0 {
		// In a function scope, use local register binding (including var).
		reg := c.allocReg()
		c.locals[name] = reg
		if len(c.scopes) > 0 {
			c.scopes[len(c.scopes)-1].declare(name, kind, reg)
		}
		c.bf.Emit(OpStar, uint8(reg), 0, 0)
		_ = nameIdx
	} else {
		c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
	}
}

// compileImportDeclaration handles import declarations at the bytecode level.
// In script mode, imports are a no-op (modules are handled by ModuleLoader).
// When the VM has a registered module loader, this emits a __moduleImport__ call chain.
func (c *Compiler) compileImportDeclaration(decl *ImportDeclaration) {
	// Register the module import request as a constant.
	// The VM will resolve imports via the module loader at execution time.
	sourceIdx := c.stringConstant(decl.Source)

	for _, spec := range decl.Specifiers {
		nameIdx := c.stringConstant(spec.Local)
		if spec.IsNamespace {
			// Load source URL.
			c.bf.Emit(OpLdaConstant, uint8(sourceIdx), 0, 0)
			// Call __moduleImportNamespace__ which returns the namespace object.
			importNsIdx := c.stringConstant("__moduleImportNamespace__")
			c.bf.Emit(OpLdaConstant, uint8(importNsIdx), 0, 0)
			calleeNsReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(calleeNsReg), 0, 0)
			argReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(argReg), 0, 0)
			c.bf.Emit(OpCall, uint8(calleeNsReg), 1, uint8(argReg))
		} else if spec.IsDefault {
			c.bf.Emit(OpLdaConstant, uint8(sourceIdx), 0, 0)
			importDefIdx := c.stringConstant("__moduleImportDefault__")
			c.bf.Emit(OpLdaConstant, uint8(importDefIdx), 0, 0)
			calleeDefReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(calleeDefReg), 0, 0)
			argReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(argReg), 0, 0)
			c.bf.Emit(OpCall, uint8(calleeDefReg), 1, uint8(argReg))
		} else {
			importedName := spec.Imported
			if importedName == "" {
				importedName = spec.Local
			}
			importedIdx := c.stringConstant(importedName)
			c.bf.Emit(OpLdaConstant, uint8(sourceIdx), 0, 0)
			c.bf.Emit(OpLdaConstant, uint8(importedIdx), 0, 0)
			importNamedIdx := c.stringConstant("__moduleImportNamed__")
			c.bf.Emit(OpLdaConstant, uint8(importNamedIdx), 0, 0)
			calleeReg := c.allocReg()
			c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)
			arg1Reg := c.allocReg()
			c.bf.Emit(OpStar, uint8(arg1Reg), 0, 0)
			arg2Reg := c.allocReg()
			c.bf.Emit(OpStar, uint8(arg2Reg), 0, 0)
			c.bf.Emit(OpCall, uint8(calleeReg), 2, uint8(arg1Reg))
		}
		// Store result to global with the local binding name.
		c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
	}
}

// compileExportDeclaration handles export declarations at the bytecode level.
// The exported declaration is compiled normally, then the exported name is also
// stored as a global so the module loader can find it.
func (c *Compiler) compileExportDeclaration(decl *ExportDeclaration) {
	if decl.Declaration != nil {
		c.compileStatement(decl.Declaration)
		// Also store the exported binding as a global for module loader collection.
		switch d := decl.Declaration.(type) {
		case *VariableDeclaration:
			nameIdx := c.stringConstant(d.Name)
			// Load back from local register (for let/const) and store to global.
			if d.Kind == "let" || d.Kind == "const" {
				if reg, ok := c.locals[d.Name]; ok {
					c.bf.Emit(OpLdar, uint8(reg), 0, 0)
					c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
				}
			}
		case *FunctionDeclaration:
			nameIdx := c.stringConstant(d.Name)
			// Functions are already registered in funcRegistry and globals via hoisting.
			// Ensure the global is set.
			c.bf.Emit(OpLdaGlobal, uint8(nameIdx), 0, 0)
			c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
		case *ClassDeclaration:
			nameIdx := c.stringConstant(d.Name)
			c.bf.Emit(OpLdaGlobal, uint8(nameIdx), 0, 0)
			c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
		}
	}
	// For export { local as exported } — ensure local variables are stored as globals.
	// const/let variables are stored in local registers, so we need to spill them
	// to globals for the module loader's export collection.
	if decl.Declaration == nil && !decl.IsDefault && len(decl.Specifiers) > 0 {
		for _, spec := range decl.Specifiers {
			if reg, ok := c.locals[spec.Local]; ok {
				nameIdx := c.stringConstant(spec.Local)
				c.bf.Emit(OpLdar, uint8(reg), 0, 0)
				c.bf.Emit(OpStaGlobal, uint8(nameIdx), 0, 0)
			}
		}
	}
	// For default exports, the value is already in the accumulator from the expression/declaration.
	if decl.IsDefault {
		defaultIdx := c.stringConstant("default")
		c.bf.Emit(OpStaGlobal, uint8(defaultIdx), 0, 0)
	}
}

// compileImportExpression compiles dynamic import(): import('./module.js')
// Returns a Promise<module namespace>.
func (c *Compiler) compileImportExpression(expr *ImportExpression) {
	// Look up __import__ builtin first so its register is allocated before the arg.
	// OpCall reads args from consecutive registers starting at calleeReg+1.
	// Pre-allocate argReg immediately after calleeReg to ensure consecutive
	// register layout even when compileExpression allocates temp registers.
	importIdx := c.stringConstant("__import__")
	c.bf.Emit(OpLdaGlobal, uint8(importIdx), 0, 0)
	calleeReg := c.allocReg()
	argReg := c.allocReg() // pre-allocate before compileExpression
	c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)

	// Compile the source expression (the URL argument).
	c.compileExpression(expr.Source)
	c.bf.Emit(OpStar, uint8(argReg), 0, 0)

	// Call __import__ with 1 argument, this=0 (non-member call convention).
	c.bf.Emit(OpCall, uint8(calleeReg), 1, 0)
}
// OpAdd does reg[operandA] + acc → acc.
func (c *Compiler) compileTemplateLiteral(tl *TemplateLiteral) {
	if len(tl.Expressions) == 0 && len(tl.Quasis) == 1 {
		idx := c.stringConstant(tl.Quasis[0])
		c.bf.Emit(OpLdaConstant, uint8(idx), 0, 0)
		return
	}

	// Start with the first quasi in acc.
	firstIdx := c.stringConstant(tl.Quasis[0])
	c.bf.Emit(OpLdaConstant, uint8(firstIdx), 0, 0)
	// accumulated is now in acc.

	for i := 0; i < len(tl.Expressions); i++ {
		// Save accumulated to a register.
		accumReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(accumReg), 0, 0)

		// Compile expression, ToString → acc = expr string.
		c.compileExpression(tl.Expressions[i])
		c.bf.Emit(OpToString, 0, 0, 0)

		// OpAdd: reg + acc → acc. We want accumulated + expr.
		// accumulated is in accumReg (reg), expr string is in acc.
		// So: OpAdd accumReg: accumReg + acc = accumulated + expr ✓
		c.bf.Emit(OpAdd, uint8(accumReg), 0, 0)

		// Now add the next quasi. acc = accumulated+expr.
		// Save to register.
		accumReg2 := c.allocReg()
		c.bf.Emit(OpStar, uint8(accumReg2), 0, 0)

		// Load quasi into acc.
		quasiIdx := c.stringConstant(tl.Quasis[i+1])
		c.bf.Emit(OpLdaConstant, uint8(quasiIdx), 0, 0)

		// OpAdd accumReg2 → reg + acc = (accumulated+expr) + quasi ✓
		c.bf.Emit(OpAdd, uint8(accumReg2), 0, 0)
	}
}

// compileTaggedTemplate compiles tagFn`template`.
// The tag function is called with the cooked quasi strings as an array
// followed by the evaluated expressions.
func (c *Compiler) compileTaggedTemplate(tt *TaggedTemplateExpression) {
	// Allocate callee register first (will be consumed by OpCall).
	calleeReg := c.allocReg()

	// Allocate argument registers NOW (must be consecutive: calleeReg+1, +2, ...).
	// Then compile callee and args into these pre-allocated registers.
	quasiArgReg := c.allocReg() // arg0 = quasis array
	numExpressions := len(tt.Expressions)
	exprArgRegs := make([]int, numExpressions)
	for i := 0; i < numExpressions; i++ {
		exprArgRegs[i] = c.allocReg()
	}

	// 1. Compile tag function → calleeReg.
	c.compileExpression(tt.Tag)
	c.bf.Emit(OpStar, uint8(calleeReg), 0, 0)

	// 2. Create quasis array → quasiArgReg.
	c.bf.Emit(OpCreateArray, 0, 0, 0)
	c.bf.Emit(OpStar, uint8(quasiArgReg), 0, 0)

	// Populate array with quasi strings (use temp registers).
	for i, q := range tt.Quasis {
		c.bf.Emit(OpLdar, uint8(quasiArgReg), 0, 0)
		idxStr := c.stringConstant(intKey(i))
		c.bf.Emit(OpLdaConstant, uint8(idxStr), 0, 0)
		keyReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(keyReg), 0, 0)
		quasiStrIdx := c.stringConstant(q)
		c.bf.Emit(OpLdaConstant, uint8(quasiStrIdx), 0, 0)
		valReg := c.allocReg()
		c.bf.Emit(OpStar, uint8(valReg), 0, 0)
		c.bf.Emit(OpLdar, uint8(keyReg), 0, 0)
		c.bf.Emit(OpStaKeyedProperty, uint8(quasiArgReg), uint8(valReg), 0)
	}

	// 3. Compile expression arguments → pre-allocated registers.
	for i, expr := range tt.Expressions {
		c.compileExpression(expr)
		c.bf.Emit(OpStar, uint8(exprArgRegs[i]), 0, 0)
	}

	// 4. Call: arguments are in calleeReg+1 through calleeReg+1+totalArgs.
	totalArgs := 1 + numExpressions
	c.bf.Emit(OpCall, uint8(calleeReg), uint8(totalArgs), 0)
}

// compileYieldExpression compiles yield [expr] and yield* expr.
func (c *Compiler) compileYieldExpression(ye *YieldExpression) {
	if ye.Delegate {
		if ye.Argument != nil {
			c.compileExpression(ye.Argument)
		} else {
			c.bf.Emit(OpLdaUndefined, 0, 0, 0)
		}
		c.bf.Emit(OpYieldDelegate, 0, 0, 0)
	} else {
		if ye.Argument != nil {
			c.compileExpression(ye.Argument)
		} else {
			c.bf.Emit(OpLdaUndefined, 0, 0, 0)
		}
		c.bf.Emit(OpYield, 0, 0, 0)
	}
}

// compileAwaitExpression compiles await expr.
// Await is compiled identically to yield — the async function runs as a generator,
// and the spawner chains promise resolutions on the yielded values.
func (c *Compiler) compileAwaitExpression(ae *AwaitExpression) {
	if ae.Argument != nil {
		c.compileExpression(ae.Argument)
	} else {
		c.bf.Emit(OpLdaUndefined, 0, 0, 0)
	}
	c.bf.Emit(OpYield, 0, 0, 0)
}

// compileSpreadExpression compiles ...expr for arrays, objects, and call args.
// For arrays: spread source elements into the new array.
// For objects: merge source properties into the new object.
// For calls: handled in compileCallExpression by flattening.
func (c *Compiler) compileSpreadExpression(se *SpreadExpression) {
	// Spread is a contextual operation — the context (array/object/call) handles it.
	// For now, just load the spread argument. The caller knows the context.
	c.compileExpression(se.Argument)
}

// --- Helpers ---

func (c *Compiler) stringConstant(s string) int {
	if idx, ok := c.strings[s]; ok {
		return idx
	}
	idx := c.bf.AddConstant(NewString(s))
	c.strings[s] = idx
	return idx
}

// truncateToUint8 safely converts int to uint8, clamping to 0-255.
func truncateToUint8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// assignGlobalSlots scans all OpLdaGlobal/OpStaGlobal instructions, builds a
// slot index mapping each global variable name to a stable integer slot, then
// rewrites the instructions to OpLdaGlobalSlot/OpStaGlobalSlot for fast array
// access. This mirrors V8's approach where global slots are assigned during
// parsing/pre-compilation.
func assignGlobalSlots(bf *BytecodeFunction) {
	slotIndex := make(map[string]int)
	// Preallocate for typical case: slots ≤ unique globals ≤ instructions / 2.
	slots := make([]string, 0, len(bf.Instructions)/4+1)

	// First pass: assign slot indices to all unique global variable names.
	for _, instr := range bf.Instructions {
		if instr.Op != OpLdaGlobal && instr.Op != OpStaGlobal {
			continue
		}
		nameIdx := int(instr.OperandA)
		if nameIdx >= len(bf.Constants) {
			continue
		}
		name := bf.Constants[nameIdx].ToString()
		if _, ok := slotIndex[name]; !ok {
			slotIndex[name] = len(slots)
			slots = append(slots, name)
		}
	}

	// Store the slot table on the BytecodeFunction for the VM to allocate.
	bf.GlobalSlots = slots

	// Second pass: rewrite OpLdaGlobal → OpLdaGlobalSlot, OpStaGlobal → OpStaGlobalSlot.
	for i, instr := range bf.Instructions {
		switch instr.Op {
		case OpLdaGlobal:
			nameIdx := int(instr.OperandA)
			if nameIdx < len(bf.Constants) {
				name := bf.Constants[nameIdx].ToString()
				if slot, ok := slotIndex[name]; ok {
					bf.Instructions[i] = Instruction{
						Op:       OpLdaGlobalSlot,
						OperandA: truncateToUint8(slot),
					}
				}
			}
		case OpStaGlobal:
			nameIdx := int(instr.OperandA)
			if nameIdx < len(bf.Constants) {
				name := bf.Constants[nameIdx].ToString()
				if slot, ok := slotIndex[name]; ok {
					bf.Instructions[i] = Instruction{
						Op:       OpStaGlobalSlot,
						OperandA: truncateToUint8(slot),
					}
				}
			}
		}
	}
}

// optimizeBytecode runs a peephole optimization pass over compiled instructions.
// Merges adjacent patterns like LdaConstant+Return → ReturnConstant to reduce
// instruction count and improve dispatch throughput.
func optimizeBytecode(instructions []Instruction) []Instruction {
	out := make([]Instruction, 0, len(instructions))
	for i := 0; i < len(instructions); i++ {
		curr := instructions[i]
		next := Instruction{}
		hasNext := i+1 < len(instructions)
		if hasNext {
			next = instructions[i+1]
		}

		// Merge LdaConstant + Return → Return with constant index in OperandA.
		if hasNext && curr.Op == OpLdaConstant && next.Op == OpReturn {
			out = append(out, Instruction{
				Op:       OpReturn,
				OperandA: curr.OperandA,
				OperandB: 1, // flag: load constant from OperandA before returning
			})
			i++ // skip the Return
			continue
		}

		// Merge LdaUndefined + Return → Return with OperandB=2 flag.
		if hasNext && curr.Op == OpLdaUndefined && next.Op == OpReturn {
			out = append(out, Instruction{
				Op:       OpReturn,
				OperandB: 2, // flag: return undefined
			})
			i++ // skip the Return
			continue
		}

		// LdaGlobal X + LdaGlobal X → LdaGlobal X + Dup 0
		// Only safe when pc+1 is not a backward jump target (loop entry point).
		if hasNext && curr.Op == OpLdaGlobal && next.Op == OpLdaGlobal &&
			curr.OperandA == next.OperandA && !isBackwardJumpTarget(instructions, i+1) {
			out = append(out, curr)
			out = append(out, Instruction{Op: OpDup, OperandA: 0})
			i++ // skip the second LdaGlobal
			continue
		}

		// StaGlobal X + LdaGlobal X → StaGlobal X + Dup 0
		// Only safe when pc+1 is not a backward jump target (loop entry point).
		if hasNext && curr.Op == OpStaGlobal && next.Op == OpLdaGlobal &&
			curr.OperandA == next.OperandA && !isBackwardJumpTarget(instructions, i+1) {
			out = append(out, curr)
			out = append(out, Instruction{Op: OpDup, OperandA: 0})
			i++ // skip the LdaGlobal
			continue
		}

		// ToNumber + ToString → skip both (no-op chain)
		if hasNext && curr.Op == OpToNumber && next.Op == OpToString {
			i++ // skip both
			continue
		}

		// LdaZero + Add R → Ldar R (x + 0 = x)
		if hasNext && curr.Op == OpLdaZero && next.Op == OpAdd {
			out = append(out, Instruction{Op: OpLdar, OperandA: next.OperandA})
			i++
			continue
		}

		// LdaOne + Mul R → Ldar R (x * 1 = x)
		if hasNext && curr.Op == OpLdaOne && next.Op == OpMul {
			out = append(out, Instruction{Op: OpLdar, OperandA: next.OperandA})
			i++
			continue
		}

		// LdaZero + Sub R → Ldar R (x - 0 = x)
		if hasNext && curr.Op == OpLdaZero && next.Op == OpSub {
			out = append(out, Instruction{Op: OpLdar, OperandA: next.OperandA})
			i++
			continue
		}

		out = append(out, curr)
	}
	return out
}

// isBackwardJumpTarget returns true if pc is the target of any backward branch
// (a jump where the target index is less than the jump instruction's index).
// This is used by the peephole optimizer to avoid eliminating instructions
// that serve as loop entry points.
func isBackwardJumpTarget(instrs []Instruction, pc int) bool {
	for i, instr := range instrs {
		switch instr.Op {
		case OpJump, OpJumpIfFalse, OpJumpIfTrue,
			OpJumpIfToBooleanTrue, OpJumpIfToBooleanFalse:
			if int(instr.OperandA) == pc && int(instr.OperandA) < i {
				return true
			}
		}
	}
	return false
}

// eliminateDeadCode removes unreachable instructions after unconditional
// terminators (OpReturn, OpThrow, OpJump) and remaps jump targets accordingly.
// Instructions that are jump targets are preserved even if they appear after
// a terminator.
func eliminateDeadCode(instrs []Instruction) []Instruction {
	if len(instrs) == 0 {
		return instrs
	}

	// Build jump-target set from all instructions that reference a PC.
	targets := make(map[int]bool)
	for _, instr := range instrs {
		switch instr.Op {
		case OpJump, OpJumpIfFalse, OpJumpIfTrue,
			OpJumpIfToBooleanTrue, OpJumpIfToBooleanFalse:
			targets[int(instr.OperandA)] = true
		case OpSetTryHandler, OpSetFinallyHandler:
			targets[int(instr.OperandA)] = true
		}
	}
	targets[0] = true // entry point is always reachable

	// Mark dead: everything after an unconditional terminator up to the next target.
	dead := make(map[int]bool)
	for i := 0; i < len(instrs); i++ {
		if dead[i] {
			continue
		}
		if instrs[i].Op == OpReturn || instrs[i].Op == OpThrow || instrs[i].Op == OpJump {
			for j := i + 1; j < len(instrs); j++ {
				if targets[j] {
					break
				}
				dead[j] = true
			}
		}
	}

	// Build old→new PC mapping.
	remap := make(map[int]int)
	newIdx := 0
	for i := range instrs {
		if dead[i] {
			remap[i] = -1 // dead
		} else {
			remap[i] = newIdx
			newIdx++
		}
	}

	// Filter out dead instructions and remap jump targets.
	out := make([]Instruction, 0, newIdx)
	for i, instr := range instrs {
		if dead[i] {
			continue
		}

		// Remap OperandA for instructions that carry a PC target.
		switch instr.Op {
		case OpJump, OpJumpIfFalse, OpJumpIfTrue,
			OpJumpIfToBooleanTrue, OpJumpIfToBooleanFalse,
			OpSetTryHandler, OpSetFinallyHandler:
			if newTarget, ok := remap[int(instr.OperandA)]; ok && newTarget >= 0 {
				instr.OperandA = uint8(newTarget)
			}
		}

		out = append(out, instr)
	}
	return out
}

// specializeBytecode applies type-feedback-driven specialization to bytecode.
// For each hot opcode site in the profile, replaces generic opcodes with
// type-specialized fast-path variants when the dominant type hint is stable.
//
// Specializations applied:
//   - OpAdd with TagNumber hint → inline numeric fast path (no ToPrimitive/ToString)
//   - OpLdaKeyedProperty with dense-array hint → bypass prototype chain walk
//
// NOTE: The actual fast-path dispatch is in the VM; this pass annotates
// instructions so the VM can select the fast path without runtime type checks.
func specializeBytecode(instrs []Instruction, profile *HotOpProfile) []Instruction {
	if profile == nil || len(profile.Sites) == 0 {
		return instrs
	}
	out := make([]Instruction, len(instrs))
	copy(out, instrs)
	for offset, site := range profile.Sites {
		if site.Count <= profile.Threshold {
			continue
		}
		if offset >= len(out) {
			continue
		}
		switch site.Op {
		case OpAdd:
			if site.TypeHint == TagNumber {
				// Annotate: use OperandC as a type-hint flag for the VM fast path.
				// OperandC=1 → numeric-only fast path (skip ToPrimitive/ToString).
				out[offset].OperandC = 1
			} else if site.TypeHint == TagString {
				// OperandC=2 → string concat fast path.
				out[offset].OperandC = 2
			}
		case OpLdaKeyedProperty:
			if site.TypeHint == TagObject {
				// OperandC=1 → dense-array fast path hint.
				out[offset].OperandC = 1
			}
		}
	}
	return out
}

func (c *Compiler) compileRegExpExpression(re *RegExpExpression) {
patIdx := c.stringConstant(re.Pattern)
flagsIdx := c.stringConstant(re.Flags)
c.bf.Emit(OpCreateRegExp, uint8(patIdx), uint8(flagsIdx), 0)
}
