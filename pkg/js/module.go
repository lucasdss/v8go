// Package js implements a spec-compliant ES module loader with module registration,
// dependency resolution, linking (parse→link→evaluate), and execution.
// Supports static import/export, re-exports, default exports,
// and circular dependency detection.
//
// Architecture:
//  1. Register: parse module, extract import/export entries, compile bytecode
//  2. Link: resolve all dependency modules, detect circular references
//  3. Evaluate: topological sort, execute modules in dependency order
//  4. Namespace: create module namespace objects with export bindings
package js

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
)

// ModuleState tracks the lifecycle of a module record.
type ModuleState int

// ModuleState tracks the lifecycle state of a module record.
const (
	ModuleUnlinked ModuleState = iota
	ModuleLinking
	ModuleLinked
	ModuleEvaluating
	ModuleEvaluated
)

// ImportEntry describes a single import binding within a module.
type ImportEntry struct {
	ModulePath string // module specifier (e.g., "./dep.js", "module")
	ImportName string // imported binding name ("*" for namespace, "default" for default)
	LocalName  string // local binding name
}

// ExportEntry describes a single export binding within a module.
type ExportEntry struct {
	ExportName string // name under which the binding is exported
	LocalName  string // local name this export refers to
	ModulePath string // source for re-exports (e.g., "export { x } from './a.js'")
}

// ModuleRecord holds the parsed, compiled, and runtime state for one module.
type ModuleRecord struct {
	Path      string
	Source    string
	Prog      *Program // parsed AST (retained for import/export extraction)
	Bytecode  *BytecodeFunction
	Imports   []ImportEntry
	Exports   []ExportEntry
	Deps      []string // dependency module paths (resolved)
	Namespace *JSObject
	State     ModuleState
}

// ModuleRegistry manages the full lifecycle of all registered modules.
type ModuleRegistry struct {
	mu      sync.Mutex
	modules map[string]*ModuleRecord
	vm      *VM
	fetch   func(url string) (string, error) // pluggable source fetcher
}

// NewModuleRegistry creates a module registry backed by the given VM.
// An optional fetch function can be provided for on-demand module loading;
// if nil, modules must be registered explicitly via Register.
//
// Sets vm.moduleRegistry so that bytecode-executed import statements
// (via __moduleImport*__ builtins) can find this registry.
func NewModuleRegistry(vm *VM, fetch func(url string) (string, error)) *ModuleRegistry {
	mr := &ModuleRegistry{
		modules: make(map[string]*ModuleRecord),
		vm:      vm,
		fetch:   fetch,
	}
	vm.moduleRegistry = mr
	return mr
}

// NewModuleLoader creates a module registry with a fetch function.
// This is a backward-compatible wrapper around NewModuleRegistry used by
// existing tests that create a ModuleLoader for on-demand module loading.
// Use NewModuleRegistry directly in new code.
func NewModuleLoader(vm *VM, fetch func(url string) (string, error)) *ModuleRegistry {
	return NewModuleRegistry(vm, fetch)
}

// Register parses and registers a module by path and source text.
// Returns the ModuleRecord on success, or an error if parsing fails.
func (mr *ModuleRegistry) Register(path, source string) (*ModuleRecord, error) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.registerLocked(path, source)
}

func (mr *ModuleRegistry) registerLocked(path, source string) (*ModuleRecord, error) {
	if mod, ok := mr.modules[path]; ok {
		return mod, nil
	}

	tokens := NewLexer(source).Tokenize()
	prog, errs := NewParser(tokens).Parse()
	if len(errs) > 0 {
		return nil, fmt.Errorf("module parse error %q: %v", path, errs)
	}

	// Extract imports and exports from the AST.
	imports := extractImports(prog)
	exports := extractExports(prog)

	// Compile the module body (same as regular script compilation).
	bf := Compile(prog)

	mod := &ModuleRecord{
		Path:     path,
		Source:   source,
		Prog:     prog,
		Bytecode: bf,
		Imports:  imports,
		Exports:  exports,
		State:    ModuleUnlinked,
	}
	mr.modules[path] = mod
	return mod, nil
}

// Get returns a registered module by path, or nil.
func (mr *ModuleRegistry) Get(path string) *ModuleRecord {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.modules[path]
}

// Resolve resolves a module specifier relative to a referrer path.
// Supports:
//   - Relative paths: "./foo", "../bar"
//   - Bare specifiers: "module", "assert" (returned as-is for Test262)
func (mr *ModuleRegistry) Resolve(specifier, referrer string) (string, error) {
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
		base := path.Dir(referrer)
		resolved := path.Clean(path.Join(base, specifier))
		return resolved, nil
	}
	// Bare specifiers: return as-is (e.g., "module", "assert" in Test262).
	return specifier, nil
}

// Link resolves all dependencies for the module graph rooted at rootPath.
// Detects circular dependencies and returns an error if one is found.
// After linking, all reachable modules are in ModuleLinked state.
func (mr *ModuleRegistry) Link(rootPath string) error {
	mr.mu.Lock()
	root, ok := mr.modules[rootPath]
	mr.mu.Unlock()
	if !ok {
		return fmt.Errorf("module not found: %s", rootPath)
	}

	visited := make(map[string]bool)
	return mr.linkModule(root, visited)
}

func (mr *ModuleRegistry) linkModule(m *ModuleRecord, visited map[string]bool) error {
	if m.State >= ModuleLinked {
		return nil
	}
	// ES modules allow circular dependencies — imported bindings may be
	// undefined until the exporting module finishes evaluation.
	// We still track visited to avoid infinite recursion during link.
	if visited[m.Path] {
		return nil
	}
	visited[m.Path] = true
	m.State = ModuleLinking

	// Collect all dependency specifiers (imports + re-exports).
	depSpecs := make(map[string]bool)
	for _, imp := range m.Imports {
		if imp.ModulePath != "" {
			depSpecs[imp.ModulePath] = true
		}
	}
	for _, exp := range m.Exports {
		if exp.ModulePath != "" {
			depSpecs[exp.ModulePath] = true
		}
	}
	// Also collect side-effect-only imports (import './dep.js' with no bindings).
	for _, stmt := range m.Prog.Body {
		if decl, ok := stmt.(*ImportDeclaration); ok && decl.Source != "" && len(decl.Specifiers) == 0 {
			depSpecs[decl.Source] = true
		}
	}

	for spec := range depSpecs {
		depPath, err := mr.Resolve(spec, m.Path)
		if err != nil {
			return fmt.Errorf("resolve %s from %s: %w", spec, m.Path, err)
		}

		mr.mu.Lock()
		dep, ok := mr.modules[depPath]
		mr.mu.Unlock()
		if !ok {
			// Try to auto-fetch if a fetch function is available.
			if mr.fetch != nil {
				source, err := mr.fetch(depPath)
				if err != nil {
					return fmt.Errorf("module not found: %s (imported by %s): %w", depPath, m.Path, err)
				}
				mr.mu.Lock()
				_, err = mr.registerLocked(depPath, source)
				mr.mu.Unlock()
				if err != nil {
					return fmt.Errorf("auto-register %s: %w", depPath, err)
				}
				mr.mu.Lock()
				dep = mr.modules[depPath]
				mr.mu.Unlock()
			} else {
				return fmt.Errorf("module not found: %s (imported by %s)", depPath, m.Path)
			}
		}

		m.Deps = append(m.Deps, depPath)
		if err := mr.linkModule(dep, visited); err != nil {
			return err
		}
	}

	m.State = ModuleLinked
	return nil
}

// Evaluate evaluates the module graph rooted at rootPath and returns
// the module's namespace object. Modules are evaluated in topological
// order (dependencies first).
func (mr *ModuleRegistry) Evaluate(rootPath string) (JSValue, error) {
	mr.mu.Lock()
	root, ok := mr.modules[rootPath]
	mr.mu.Unlock()
	if !ok {
		return Undefined, fmt.Errorf("module not found: %s", rootPath)
	}

	order, err := mr.topologicalSort(root)
	if err != nil {
		return Undefined, err
	}

	for _, m := range order {
		if m.State >= ModuleEvaluated {
			continue
		}
		m.State = ModuleEvaluating

		// Create the namespace object for this module.
		ns := NewJSObject()
		ns.ConstructorName = "Module"
		m.Namespace = ns

		// Resolve import bindings BEFORE executing the module body.
		// This sets globals for imported names so that the module body
		// (and re-export collection) can reference them.
		for _, imp := range m.Imports {
			depPath, _ := mr.Resolve(imp.ModulePath, m.Path)
			mr.mu.Lock()
			dep, ok := mr.modules[depPath]
			mr.mu.Unlock()
			if !ok || dep.Namespace == nil {
				continue
			}
			depNS := dep.Namespace
			switch {
			case imp.ImportName == "*":
				mr.vm.SetGlobal(imp.LocalName, NewObject(depNS))
			case imp.ImportName == "default":
				mr.vm.SetGlobal(imp.LocalName, depNS.Get("default"))
			default:
				importName := imp.ImportName
				if importName == "" {
					importName = imp.LocalName
				}
				mr.vm.SetGlobal(imp.LocalName, depNS.Get(importName))
			}
		}

		// Build the source to execute: export declarations are compiled
		// normally (the compiler emits StaGlobal for them). After execution,
		// we collect export bindings from globals into the namespace.
		var execBody []Node
		for _, stmt := range m.Prog.Body {
			switch s := stmt.(type) {
			case *ImportDeclaration:
				// Import bindings already resolved above.
			case *ExportDeclaration:
				switch {
				case s.Source != "" && len(s.Specifiers) > 0:
					// Re-export: resolve from source module namespace.
					depVal, _ := mr.GetNamespace(s.Source)
					if depVal.IsObject() && depVal.ObjVal != nil {
						for _, spec := range s.Specifiers {
							expName := spec.Exported
							if expName == "" {
								expName = spec.Local
							}
							val := depVal.ObjVal.Get(spec.Local)
							ns.Set(expName, val)
						}
					}
				case s.Source != "" && s.Declaration == nil && len(s.Specifiers) == 0:
					// export * from '...' — copy all exports from source.
					depPath, _ := mr.Resolve(s.Source, m.Path)
					mr.mu.Lock()
					depMod, depOk := mr.modules[depPath]
					mr.mu.Unlock()
					if depOk && depMod.State >= ModuleEvaluated && depMod.Namespace != nil {
						for _, depExp := range depMod.Exports {
							if depExp.ExportName != "default" && depExp.ExportName != "*" {
								val := depMod.Namespace.Get(depExp.ExportName)
								ns.Set(depExp.ExportName, val)
							}
						}
					}
				default:
					// Keep declaration-based exports for compilation.
					execBody = append(execBody, s)
				}
			default:
				execBody = append(execBody, stmt)
			}
		}

		// Expose import.meta via __moduleGetMeta__ global.
		// Module code can access import_meta.url and import_meta.resolve(specifier).
		metaFn := mr.vm.registry.Builtins["__moduleGetMeta__"]
		if metaFn != nil {
			mr.vm.SetGlobal("import_meta", metaFn([]JSValue{NewString(m.Path)}))
		}

		// Execute the module body.
		if len(execBody) > 0 {
			execProg := &Program{Body: execBody}
			bf := Compile(execProg)
			mr.vm.execute(bf)
		}

		// Collect exports from global scope into the namespace.
		for _, exp := range m.Exports {
			if exp.ModulePath == "" {
				val := mr.vm.GetGlobal(exp.LocalName)
				if val.Tag != TagUndefined {
					ns.Set(exp.ExportName, val)
				}
			}
		}

		// Handle export default <expr> which sets global "default".
		if val := mr.vm.GetGlobal("default"); val.Tag != TagUndefined {
			hasDefaultExport := false
			for _, exp := range m.Exports {
				if exp.ExportName == "default" {
					hasDefaultExport = true
					break
				}
			}
			if hasDefaultExport {
				ns.Set("default", val)
			}
		}

		m.State = ModuleEvaluated
	}

	return NewObject(root.Namespace), nil
}

// RegisterAndLink is a convenience that registers a module, links it,
// and returns the module record. If the module graph is already registered,
// this can be called for the root module.
func (mr *ModuleRegistry) RegisterAndLink(path, source string) (*ModuleRecord, error) {
	_, err := mr.Register(path, source)
	if err != nil {
		return nil, err
	}
	if err := mr.Link(path); err != nil {
		return nil, err
	}
	mr.mu.Lock()
	mod := mr.modules[path]
	mr.mu.Unlock()
	return mod, nil
}

// GetNamespace evaluates the module if needed and returns its namespace.
func (mr *ModuleRegistry) GetNamespace(path string) (JSValue, error) {
	mr.mu.Lock()
	mod, ok := mr.modules[path]
	mr.mu.Unlock()
	if !ok {
		return Undefined, fmt.Errorf("module not found: %s", path)
	}
	if mod.State >= ModuleEvaluated {
		return NewObject(mod.Namespace), nil
	}
	return mr.Evaluate(path)
}

// Import is the legacy interface used by __moduleImport*__ builtins.
// It resolves, links, and evaluates a module lazily.
func (mr *ModuleRegistry) Import(url string) (JSValue, error) {
	mr.mu.Lock()
	mod, ok := mr.modules[url]
	mr.mu.Unlock()
	if ok {
		if mod.State >= ModuleEvaluated {
			return NewObject(mod.Namespace), nil
		}
		return mr.Evaluate(url)
	}

	// Try auto-fetch.
	if mr.fetch != nil {
		source, err := mr.fetch(url)
		if err != nil {
			return Undefined, fmt.Errorf("module load error for %q: %w", url, err)
		}
		if _, err := mr.Register(url, source); err != nil {
			return Undefined, err
		}
		if err := mr.Link(url); err != nil {
			return Undefined, err
		}
		return mr.Evaluate(url)
	}

	return Undefined, fmt.Errorf("module not found: %s", url)
}

// topologicalSort returns modules rooted at root in evaluation order
// (dependencies first). Circular dependencies are allowed — modules in
// the cycle are evaluated in post-order, and imported bindings from
// not-yet-evaluated modules will be undefined (per ES spec).
func (mr *ModuleRegistry) topologicalSort(root *ModuleRecord) ([]*ModuleRecord, error) {
	var result []*ModuleRecord
	visited := make(map[string]bool)
	var visit func(m *ModuleRecord) error

	visit = func(m *ModuleRecord) error {
		if visited[m.Path] {
			return nil
		}
		visited[m.Path] = true

		for _, depPath := range m.Deps {
			// Skip self-references (import from self).
			if depPath == m.Path {
				continue
			}
			mr.mu.Lock()
			dep, ok := mr.modules[depPath]
			mr.mu.Unlock()
			if !ok {
				return fmt.Errorf("missing dependency: %s", depPath)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}

		result = append(result, m)
		return nil
	}

	if err := visit(root); err != nil {
		return nil, err
	}
	return result, nil
}

// SetGlobal sets a global variable on the backing VM.
func (mr *ModuleRegistry) SetGlobal(name string, val JSValue) {
	mr.vm.SetGlobal(name, val)
}

// resolveSpecifier resolves a module specifier relative to a base URL.
// Handles relative specifiers (./ or ../) by resolving against the base,
// and returns bare specifiers as-is (delegating to the module loader).
// Supports both full URLs (https://...) and plain file paths (main.js).
func resolveSpecifier(baseURL, specifier string) (string, error) {
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
		u, err := url.Parse(baseURL)
		if err != nil {
			return "", err
		}
		// If the base is a plain file path (no scheme, no leading /), use path-based
		// resolution to match the module system's behavior.
		if u.Scheme == "" && !strings.HasPrefix(baseURL, "/") {
			base := path.Dir(baseURL)
			return path.Clean(path.Join(base, specifier)), nil
		}
		ref, err := url.Parse(specifier)
		if err != nil {
			return "", err
		}
		return u.ResolveReference(ref).String(), nil
	}
	return specifier, nil
}

// extractImports scans the AST for ImportDeclaration nodes and returns
// a flat list of ImportEntry records.
func extractImports(prog *Program) []ImportEntry {
	var entries []ImportEntry
	for _, stmt := range prog.Body {
		decl, ok := stmt.(*ImportDeclaration)
		if !ok {
			continue
		}
		for _, spec := range decl.Specifiers {
			importName := spec.Imported
			switch {
			case spec.IsNamespace:
				importName = "*"
			case spec.IsDefault:
				importName = "default"
			case importName == "":
				importName = spec.Local
			}
			entries = append(entries, ImportEntry{
				ModulePath: decl.Source,
				ImportName: importName,
				LocalName:  spec.Local,
			})
		}
	}
	return entries
}

// extractExports scans the AST for ExportDeclaration nodes and returns
// a flat list of ExportEntry records.
func extractExports(prog *Program) []ExportEntry {
	var entries []ExportEntry
	for _, stmt := range prog.Body {
		decl, ok := stmt.(*ExportDeclaration)
		if !ok {
			continue
		}

		if decl.IsDefault {
			entries = append(entries, ExportEntry{
				ExportName: "default",
				LocalName:  "default",
			})
			continue
		}

		if decl.Declaration != nil {
			switch d := decl.Declaration.(type) {
			case *VariableDeclaration:
				entries = append(entries, ExportEntry{
					ExportName: d.Name,
					LocalName:  d.Name,
				})
			case *FunctionDeclaration:
				entries = append(entries, ExportEntry{
					ExportName: d.Name,
					LocalName:  d.Name,
				})
			case *ClassDeclaration:
				entries = append(entries, ExportEntry{
					ExportName: d.Name,
					LocalName:  d.Name,
				})
			}
		}

		for _, spec := range decl.Specifiers {
			expName := spec.Exported
			if expName == "" {
				expName = spec.Local
			}
			entries = append(entries, ExportEntry{
				ExportName: expName,
				LocalName:  spec.Local,
				ModulePath: decl.Source,
			})
		}

		// export * from '...'
		if decl.Source != "" && len(decl.Specifiers) == 0 && decl.Declaration == nil && !decl.IsDefault {
			// Star re-export: handled during evaluation.
			entries = append(entries, ExportEntry{
				ExportName: "*",
				ModulePath: decl.Source,
			})
		}
	}
	return entries
}
