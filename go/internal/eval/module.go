package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/token"
)

// moduleCache maps absolute file paths to their exported names.
var moduleCache map[string]map[string]Value

// resetModuleCache clears the module cache (called before each Run).
func resetModuleCache() {
	moduleCache = make(map[string]map[string]Value)
}

// resolveModulePath resolves a module path (e.g. ["utils", "strings"]) to an
// absolute file path, relative to the given base directory.
// Tries: baseDir/utils/strings.clk (directory-based), then baseDir/utils.strings.clk (flat).
func resolveModulePath(modPath []string, baseDir string) (string, error) {
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

// loadModule reads, lexes, parses, desugars, and evaluates a module file.
// Returns a map of exported (pub) names to their values.
func loadModule(absPath string, loc token.Loc) map[string]Value {
	// Check cache
	if exports, ok := moduleCache[absPath]; ok {
		return exports
	}

	// Insert empty entry to prevent infinite recursion
	moduleCache[absPath] = map[string]Value{}

	// Read source
	source, err := os.ReadFile(absPath)
	if err != nil {
		panic(runtimeError("E220", fmt.Sprintf("cannot read module '%s': %v", absPath, err), loc))
	}

	// Lex
	tokens, lexErr := lexer.Lex(string(source))
	if lexErr != nil {
		panic(runtimeError("E221", fmt.Sprintf("lex error in module '%s': %s", absPath, lexErr.Message), loc))
	}

	// Parse
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		panic(runtimeError("E222", fmt.Sprintf("parse error in module '%s': %s", absPath, parseErr.Message), loc))
	}

	// Desugar definition bodies
	for i, tl := range program.TopLevels {
		switch decl := tl.(type) {
		case ast.TopDefinition:
			decl.Body = desugar.Desugar(decl.Body)
			program.TopLevels[i] = decl
		case ast.TopImplBlock:
			for j, m := range decl.Methods {
				decl.Methods[j] = ast.ImplMethod{Name: m.Name, Body: desugar.Desugar(m.Body)}
			}
			program.TopLevels[i] = decl
		}
	}

	modDir := filepath.Dir(absPath)

	// Create module environment with builtins
	modEnv := InitGlobalEnv()

	// Track which names are public
	pubNames := map[string]bool{}

	// Process all top-level declarations
	for _, tl := range program.TopLevels {
		switch decl := tl.(type) {
		case ast.TopModDecl:
			continue

		case ast.TopUseDecl:
			// Resolve and load imported module
			importPath, err := resolveModulePath(decl.Path, modDir)
			if err != nil {
				panic(runtimeError("E220", err.Error(), decl.Loc))
			}
			exports := loadModule(importPath, decl.Loc)
			for _, imp := range decl.Imports {
				name := imp.Name
				localName := name
				if imp.Alias != "" {
					localName = imp.Alias
				}
				val, ok := exports[name]
				if !ok {
					panic(runtimeError("E223", fmt.Sprintf("'%s' is not exported by module '%s'", name, strings.Join(decl.Path, ".")), decl.Loc))
				}
				modEnv.Set(localName, val)
			}

		case ast.TopTypeDecl:
			for _, v := range decl.Variants {
				modEnv.Set(v.Name, makeVariantConstructor(v.Name, len(v.Fields)))
				if decl.Pub {
					pubNames[v.Name] = true
				}
			}
			if len(decl.Deriving) > 0 {
				registerDerivedImpls(decl.Variants, decl.Deriving, modEnv)
			}

		case ast.TopEffectAlias:
			effects := make([]string, len(decl.Effects))
			for i, e := range decl.Effects {
				effects[i] = e.Name
			}
			modEnv.Set(decl.Name, ValEffectDef{Name: decl.Name, Ops: effects})
			if decl.Pub {
				pubNames[decl.Name] = true
			}

		case ast.TopEffectDecl:
			opNames := make([]string, len(decl.Ops))
			for i, op := range decl.Ops {
				opNames[i] = op.Name
			}
			modEnv.Set(decl.Name, ValEffectDef{Name: decl.Name, Ops: opNames})
			if decl.Pub {
				pubNames[decl.Name] = true
			}
			for _, op := range decl.Ops {
				modEnv.Set(op.Name, makeEffectOp(op.Name, decl.Name))
				if decl.Pub {
					pubNames[op.Name] = true
				}
			}

		case ast.TopInterfaceDecl:
			for _, m := range decl.Methods {
				interfaceMethodNames[m.Name] = decl.Name
				modEnv.Set(m.Name, makeInterfaceDispatcher(m.Name))
				if decl.Pub {
					pubNames[m.Name] = true
				}
			}

		case ast.TopImplBlock:
			typeTag := typeExprToTag(decl.ForType)
			for _, m := range decl.Methods {
				bodyValue := Evaluate(m.Body, modEnv)
				registerImpl(m.Name, typeTag, bodyValue)
			}

		case ast.TopDefinition:
			params := make([]ast.Param, len(decl.Sig.Params))
			for i, p := range decl.Sig.Params {
				params[i] = ast.Param{Name: p.Name}
			}
			closure := ValClosure{Params: params, Body: decl.Body, Env: modEnv}
			modEnv.Set(decl.Name, closure)
			if decl.Pub {
				pubNames[decl.Name] = true
			}

		case ast.TopTestDecl:
			continue
		}
	}

	// Build export map from public names
	exports := make(map[string]Value)
	for name := range pubNames {
		if val := envLookup(modEnv, name); val != nil {
			exports[name] = val
		}
	}

	moduleCache[absPath] = exports
	return exports
}

// HandleUseDecl processes a use declaration in the given environment.
// baseDir is the directory of the file containing the use declaration.
func HandleUseDecl(decl ast.TopUseDecl, env *Env, baseDir string) {
	importPath, err := resolveModulePath(decl.Path, baseDir)
	if err != nil {
		panic(runtimeError("E220", err.Error(), decl.Loc))
	}
	exports := loadModule(importPath, decl.Loc)
	for _, imp := range decl.Imports {
		name := imp.Name
		localName := name
		if imp.Alias != "" {
			localName = imp.Alias
		}
		val, ok := exports[name]
		if !ok {
			panic(runtimeError("E223", fmt.Sprintf("'%s' is not exported by module '%s'", name, strings.Join(decl.Path, ".")), decl.Loc))
		}
		env.Set(localName, val)
	}
}
