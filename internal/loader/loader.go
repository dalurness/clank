// Package loader resolves module imports and links them into a flat program
// suitable for single-pass compilation to the VM.
package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
)

// ResolveModulePath resolves a module path (e.g. ["utils", "strings"]) to an
// absolute file path, relative to the given base directory.
// Tries: baseDir/utils/strings.clk (directory-based), then baseDir/utils.strings.clk (flat).
func ResolveModulePath(modPath []string, baseDir string) (string, error) {
	// Directory-based: utils/strings.clk
	parts := append(modPath[:len(modPath)-1:len(modPath)-1], modPath[len(modPath)-1]+".clk")
	dirBased := filepath.Join(append([]string{baseDir}, parts...)...)
	if _, err := os.Stat(dirBased); err == nil {
		abs, _ := filepath.Abs(dirBased)
		return abs, nil
	}

	// Flat file: utils.strings.clk
	flatBased := filepath.Join(baseDir, strings.Join(modPath, ".")+".clk")
	if _, err := os.Stat(flatBased); err == nil {
		abs, _ := filepath.Abs(flatBased)
		return abs, nil
	}

	return "", fmt.Errorf("module '%s' not found (tried %s and %s)", strings.Join(modPath, "."), dirBased, flatBased)
}

// LinkResult is the output of linking: a flat program with all imports resolved.
type LinkResult struct {
	Program *ast.Program
	Error   error
}

// Link resolves all `use` declarations in a program by loading imported modules
// and merging their top-levels into a single flat program. The baseDir is used
// to resolve relative module paths.
func Link(program *ast.Program, baseDir string) LinkResult {
	return LinkWithPackages(program, baseDir, nil)
}

// LinkWithPackages is like Link but also accepts a package module map for
// resolving imports from external packages. The packageModules map is
// qualified module path → absolute file path, as produced by pkg.ResolvePackages.
func LinkWithPackages(program *ast.Program, baseDir string, packageModules map[string]string) LinkResult {
	l := &linker{
		baseDir:        baseDir,
		loaded:         make(map[string]bool),
		loading:        make(map[string]bool),
		pubMap:         make(map[string]map[string]bool),
		typeMap:        make(map[string]map[string][]string),
		packageModules: packageModules,
	}

	result, err := l.link(program, baseDir)
	if err != nil {
		return LinkResult{Error: err}
	}
	return LinkResult{Program: result}
}

type linker struct {
	baseDir        string
	loaded         map[string]bool            // absolute path → fully loaded
	loading        map[string]bool            // absolute path → currently being loaded (cycle detection)
	loadChain      []string                   // current import chain for error messages
	pubMap         map[string]map[string]bool // absolute path → set of pub names
	typeMap        map[string]map[string][]string // absolute path → type name → constructor names
	packageModules map[string]string          // qualified module path → abs file path (from pkg system)
	collected      []ast.TopLevel             // top-levels collected from all modules
}

func (l *linker) link(program *ast.Program, currentDir string) (*ast.Program, error) {
	// First, recursively load all imported modules
	for _, tl := range program.TopLevels {
		useDecl, ok := tl.(ast.TopUseDecl)
		if !ok {
			continue
		}
		if err := l.loadModule(useDecl.Path, currentDir); err != nil {
			return nil, err
		}
	}

	// Expand use declarations: importing a type name also imports its constructors
	expanded := l.expandImports(program.TopLevels, currentDir)

	// Build the flat program: imported module top-levels first, then original program
	var merged []ast.TopLevel
	merged = append(merged, l.collected...)
	merged = append(merged, expanded...)

	return &ast.Program{TopLevels: merged}, nil
}

// expandImports rewrites use declarations:
// 1. For qualified imports (no parens), fills in the import list with all pub names
// 2. For any import, if a type name is imported, adds its constructors
func (l *linker) expandImports(topLevels []ast.TopLevel, currentDir string) []ast.TopLevel {
	result := make([]ast.TopLevel, 0, len(topLevels))
	for _, tl := range topLevels {
		useDecl, ok := tl.(ast.TopUseDecl)
		if !ok {
			result = append(result, tl)
			continue
		}

		// Resolve the module path
		absPath, err := ResolveModulePath(useDecl.Path, currentDir)
		if err != nil && currentDir != l.baseDir {
			absPath, err = ResolveModulePath(useDecl.Path, l.baseDir)
		}
		if err != nil && l.packageModules != nil {
			qualifiedPath := strings.Join(useDecl.Path, ".")
			if pkgPath, ok := l.packageModules[qualifiedPath]; ok {
				absPath = pkgPath
				err = nil
			}
		}
		if err != nil {
			result = append(result, tl)
			continue
		}

		// For qualified imports, populate the import list with all pub names
		if useDecl.Qualified && len(useDecl.Imports) == 0 {
			pubNames := l.pubMap[absPath]
			for name := range pubNames {
				useDecl.Imports = append(useDecl.Imports, ast.ImportItem{Name: name})
			}
		}

		// Expand type imports: importing a type name includes its constructors
		typeCtors := l.typeMap[absPath]
		if typeCtors != nil {
			existing := make(map[string]bool)
			for _, imp := range useDecl.Imports {
				existing[imp.Name] = true
			}

			var expanded []ast.ImportItem
			for _, imp := range useDecl.Imports {
				expanded = append(expanded, imp)
				if ctors, ok := typeCtors[imp.Name]; ok {
					for _, ctor := range ctors {
						if !existing[ctor] {
							expanded = append(expanded, ast.ImportItem{Name: ctor})
							existing[ctor] = true
						}
					}
				}
			}
			useDecl.Imports = expanded
		}

		result = append(result, useDecl)
	}
	return result
}

func (l *linker) loadModule(modPath []string, currentDir string) error {
	absPath, err := ResolveModulePath(modPath, currentDir)
	if err != nil {
		// Fall back to resolving from the project root (entry file's directory)
		if currentDir != l.baseDir {
			absPath, err = ResolveModulePath(modPath, l.baseDir)
		}
		// Fall back to the package module map (external packages)
		if err != nil && l.packageModules != nil {
			qualifiedPath := strings.Join(modPath, ".")
			if pkgPath, ok := l.packageModules[qualifiedPath]; ok {
				absPath = pkgPath
				err = nil
			}
		}
		if err != nil {
			return err
		}
	}

	// Already fully loaded — skip
	if l.loaded[absPath] {
		return nil
	}

	// Currently being loaded — circular import
	if l.loading[absPath] {
		chain := append(l.loadChain, filepath.Base(absPath))
		return fmt.Errorf("circular import detected: %s", strings.Join(chain, " → "))
	}

	// Mark as loading and track the chain
	l.loading[absPath] = true
	l.loadChain = append(l.loadChain, filepath.Base(absPath))

	// Read, lex, parse
	source, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("cannot read module '%s': %v", absPath, err)
	}

	tokens, lexErr := lexer.Lex(string(source))
	if lexErr != nil {
		return fmt.Errorf("lex error in module '%s': %s", absPath, lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return fmt.Errorf("parse error in module '%s': %s", absPath, parseErr.Message)
	}

	// Desugar definition bodies
	for i, tl := range program.TopLevels {
		switch decl := tl.(type) {
		case ast.TopDefinition:
			decl.Body = desugar.Desugar(decl.Body)
			program.TopLevels[i] = decl
		case ast.TopImplBlock:
			methods := make([]ast.ImplMethod, len(decl.Methods))
			for j, m := range decl.Methods {
				methods[j] = ast.ImplMethod{Name: m.Name, Body: desugar.Desugar(m.Body)}
			}
			decl.Methods = methods
			program.TopLevels[i] = decl
		case ast.TopTestDecl:
			decl.Body = desugar.Desugar(decl.Body)
			program.TopLevels[i] = decl
		}
	}

	modDir := filepath.Dir(absPath)

	// Track pub names and type→constructor mappings for this module
	pubNames := make(map[string]bool)
	typeConstructors := make(map[string][]string) // type name → constructor names
	for _, tl := range program.TopLevels {
		switch decl := tl.(type) {
		case ast.TopDefinition:
			if decl.Pub {
				pubNames[decl.Name] = true
			}
		case ast.TopTypeDecl:
			if decl.Pub {
				pubNames[decl.Name] = true // export the type name itself
				var ctors []string
				for _, v := range decl.Variants {
					pubNames[v.Name] = true
					ctors = append(ctors, v.Name)
				}
				typeConstructors[decl.Name] = ctors
			}
		case ast.TopEffectDecl:
			if decl.Pub {
				pubNames[decl.Name] = true
				for _, op := range decl.Ops {
					pubNames[op.Name] = true
				}
			}
		case ast.TopEffectAlias:
			if decl.Pub {
				pubNames[decl.Name] = true
			}
		case ast.TopInterfaceDecl:
			if decl.Pub {
				for _, m := range decl.Methods {
					pubNames[m.Name] = true
				}
			}
		}
	}
	l.pubMap[absPath] = pubNames
	l.typeMap[absPath] = typeConstructors

	// Recursively load this module's imports first
	for _, tl := range program.TopLevels {
		useDecl, ok := tl.(ast.TopUseDecl)
		if !ok {
			continue
		}
		if err := l.loadModule(useDecl.Path, modDir); err != nil {
			return err
		}
	}

	// Expand type imports in this module's use declarations
	expandedTLs := l.expandImports(program.TopLevels, modDir)

	// Add this module's top-levels (skip mod/test declarations)
	for _, tl := range expandedTLs {
		switch tl.(type) {
		case ast.TopModDecl:
			continue
		case ast.TopUseDecl:
			// Keep use declarations — the compiler handles name aliasing
			l.collected = append(l.collected, tl)
		case ast.TopTestDecl:
			continue
		default:
			l.collected = append(l.collected, tl)
		}
	}

	// Done loading — move from loading to loaded, pop the chain
	delete(l.loading, absPath)
	l.loadChain = l.loadChain[:len(l.loadChain)-1]
	l.loaded[absPath] = true

	return nil
}
