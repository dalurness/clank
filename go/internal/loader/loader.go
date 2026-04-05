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
	l := &linker{
		baseDir: baseDir,
		loaded:  make(map[string]bool),
		pubMap:  make(map[string]map[string]bool),
	}

	result, err := l.link(program, baseDir)
	if err != nil {
		return LinkResult{Error: err}
	}
	return LinkResult{Program: result}
}

type linker struct {
	baseDir    string
	loaded     map[string]bool            // absolute path → already loaded
	pubMap     map[string]map[string]bool // absolute path → set of pub names
	collected  []ast.TopLevel             // top-levels collected from all modules
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

	// Build the flat program: imported module top-levels first, then original program
	var merged []ast.TopLevel
	merged = append(merged, l.collected...)
	merged = append(merged, program.TopLevels...)

	return &ast.Program{TopLevels: merged}, nil
}

func (l *linker) loadModule(modPath []string, currentDir string) error {
	absPath, err := ResolveModulePath(modPath, currentDir)
	if err != nil {
		return err
	}

	// Already loaded — skip
	if l.loaded[absPath] {
		return nil
	}
	l.loaded[absPath] = true

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

	// Track pub names for this module
	pubNames := make(map[string]bool)
	for _, tl := range program.TopLevels {
		switch decl := tl.(type) {
		case ast.TopDefinition:
			if decl.Pub {
				pubNames[decl.Name] = true
			}
		case ast.TopTypeDecl:
			if decl.Pub {
				for _, v := range decl.Variants {
					pubNames[v.Name] = true
				}
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

	// Add this module's top-levels (skip mod/use/test declarations)
	for _, tl := range program.TopLevels {
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

	return nil
}
