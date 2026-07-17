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

// LinkWithPackages is like Link but also accepts a package files map for
// resolving `use &pkg` imports from external packages. The packageFiles
// map is package name → list of every .clk file under that package's
// src/ tree, as produced by pkg.ResolvePackages. When a consumer writes
// `use &foo`, the loader loads every file in packageFiles["foo"] and
// merges their pub symbols into a single flat namespace qualified by
// the package name (or a user-supplied alias).
func LinkWithPackages(program *ast.Program, baseDir string, packageFiles map[string][]string) LinkResult {
	l := &linker{
		baseDir:       baseDir,
		loaded:        make(map[string]bool),
		loading:       make(map[string]bool),
		pubMap:        make(map[string]map[string]bool),
		typeMap:       make(map[string]map[string][]string),
		packageFiles:  packageFiles,
		packagePubs:   make(map[string]map[string]bool),
		packageTypes:  make(map[string]map[string][]string),
		packageLoaded: make(map[string]bool),
	}

	result, err := l.link(program, baseDir)
	if err != nil {
		return LinkResult{Error: err}
	}
	return LinkResult{Program: result}
}

type linker struct {
	baseDir       string
	loaded        map[string]bool                // absolute path → fully loaded (local files)
	loading       map[string]bool                // absolute path → currently being loaded (cycle detection)
	loadChain     []string                       // current import chain for error messages
	pubMap        map[string]map[string]bool     // absolute path → pub names (per local file)
	typeMap       map[string]map[string][]string // absolute path → type → ctors (per local file)
	packageFiles  map[string][]string            // package name → every .clk under src/
	packagePubs   map[string]map[string]bool     // package name → merged pub names across all files
	packageTypes  map[string]map[string][]string // package name → merged type → ctors across all files
	packageLoaded map[string]bool                // which external packages have already been loaded
	collected     []ast.TopLevel                 // top-levels collected from all modules
}

func (l *linker) link(program *ast.Program, currentDir string) (*ast.Program, error) {
	// First, recursively load all imported modules
	for _, tl := range program.TopLevels {
		useDecl, ok := tl.(ast.TopUseDecl)
		if !ok {
			continue
		}
		if useDecl.External {
			if err := l.loadExternalPackage(useDecl.Path[0]); err != nil {
				return nil, err
			}
			continue
		}
		if err := l.loadModule(useDecl.Path, currentDir); err != nil {
			return nil, err
		}
	}

	// Expand use declarations: importing a type name also imports its constructors
	expanded := l.expandImports(program.TopLevels, currentDir, l.baseDir)

	// Build the flat program: imported module top-levels first, then original program
	var merged []ast.TopLevel
	merged = append(merged, l.collected...)
	merged = append(merged, expanded...)

	return &ast.Program{TopLevels: merged}, nil
}

// expandImports rewrites use declarations:
// 1. For qualified imports (no parens), fills in the import list with all
//    pub names from the resolved module or package.
// 2. For any import, if a type name is imported, adds its constructors.
//
// fallbackDir is tried when a module path doesn't resolve relative to
// currentDir — the project root for local files, the package's src/ root
// for files inside an external package.
func (l *linker) expandImports(topLevels []ast.TopLevel, currentDir, fallbackDir string) []ast.TopLevel {
	result := make([]ast.TopLevel, 0, len(topLevels))
	for _, tl := range topLevels {
		useDecl, ok := tl.(ast.TopUseDecl)
		if !ok {
			result = append(result, tl)
			continue
		}

		// External packages have their pub sets merged across every file
		// in the package's src/ tree, keyed by package name.
		var pubNames map[string]bool
		var typeCtors map[string][]string
		if useDecl.External {
			pubNames = l.packagePubs[useDecl.Path[0]]
			typeCtors = l.packageTypes[useDecl.Path[0]]
		} else {
			absPath, err := ResolveModulePath(useDecl.Path, currentDir)
			if err != nil && currentDir != fallbackDir {
				absPath, err = ResolveModulePath(useDecl.Path, fallbackDir)
			}
			if err != nil {
				result = append(result, tl)
				continue
			}
			pubNames = l.pubMap[absPath]
			typeCtors = l.typeMap[absPath]
		}

		// For qualified imports, populate the import list with all pub names
		if useDecl.Qualified && len(useDecl.Imports) == 0 {
			for name := range pubNames {
				useDecl.Imports = append(useDecl.Imports, ast.ImportItem{Name: name})
			}
		}

		// Expand type imports: importing a type name includes its constructors
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
		// No fallthrough to external packages — those require the `&`
		// sigil. This keeps local and external namespaces visually
		// distinct at every call site.
		if err != nil {
			return l.enrichNotFound(err, modPath)
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
		if useDecl.External {
			if err := l.loadExternalPackage(useDecl.Path[0]); err != nil {
				return err
			}
			continue
		}
		if err := l.loadModule(useDecl.Path, modDir); err != nil {
			return err
		}
	}

	// Expand type imports in this module's use declarations
	expandedTLs := l.expandImports(program.TopLevels, modDir, l.baseDir)

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

// loadExternalPackage loads every .clk file in a package's src/ tree and
// merges their pub symbols into a single flat namespace keyed by package
// name. Collisions across files are reported as errors at link time with
// both declaration sites named.
//
// Two passes: first parse every file and register its symbols, then
// resolve each file's own imports. The split matters because package
// files may import each other in any order (all of them are merged
// anyway), and may import other external packages (`use &dep`), which
// must recurse before import expansion fills in qualified names.
func (l *linker) loadExternalPackage(pkgName string) error {
	if l.packageLoaded[pkgName] {
		return nil
	}
	// Mark before recursing so mutually-dependent packages terminate.
	l.packageLoaded[pkgName] = true

	files, ok := l.packageFiles[pkgName]
	if !ok {
		return l.unknownPackageError(pkgName)
	}

	mergedPubs := make(map[string]bool)
	mergedTypes := make(map[string][]string)
	pubOrigin := make(map[string]string) // symbol → first file that defined it, for collision messages

	inPackage := make(map[string]bool, len(files))
	for _, f := range files {
		inPackage[f] = true
	}
	// The package's src/ root — fallback for imports between package
	// files that are written relative to the source root.
	var srcRoot string
	if len(files) > 0 {
		srcRoot = packageSrcRoot(files[0])
	}

	type parsedFile struct {
		path    string
		program *ast.Program
	}
	var parsed []parsedFile

	// ── Pass 1: parse, desugar, and register every file's symbols ──
	for _, absPath := range files {
		source, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("cannot read package file '%s': %v", absPath, err)
		}
		tokens, lexErr := lexer.Lex(string(source))
		if lexErr != nil {
			return fmt.Errorf("lex error in package file '%s': %s", absPath, lexErr.Message)
		}
		program, parseErr := parser.Parse(tokens)
		if parseErr != nil {
			return fmt.Errorf("parse error in package file '%s': %s", absPath, parseErr.Message)
		}

		// Desugar bodies, same as loadModule does for local files.
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
			}
		}

		// Collect pub symbols. Any collision between two files in the
		// same package is an author error — report it with both sites.
		addPub := func(name string) error {
			if other, exists := pubOrigin[name]; exists {
				return fmt.Errorf(
					"duplicate export '%s' in package '%s'\n  first:  %s\n  second: %s\nhint: rename one, mark it private, or choose a more specific name",
					name, pkgName, other, absPath,
				)
			}
			mergedPubs[name] = true
			pubOrigin[name] = absPath
			return nil
		}
		filePubs := make(map[string]bool)
		fileTypes := make(map[string][]string)
		for _, tl := range program.TopLevels {
			switch decl := tl.(type) {
			case ast.TopDefinition:
				if decl.Pub {
					if err := addPub(decl.Name); err != nil {
						return err
					}
					filePubs[decl.Name] = true
				}
			case ast.TopTypeDecl:
				if decl.Pub {
					if err := addPub(decl.Name); err != nil {
						return err
					}
					filePubs[decl.Name] = true
					var ctors []string
					for _, v := range decl.Variants {
						if err := addPub(v.Name); err != nil {
							return err
						}
						filePubs[v.Name] = true
						ctors = append(ctors, v.Name)
					}
					mergedTypes[decl.Name] = ctors
					fileTypes[decl.Name] = ctors
				}
			case ast.TopEffectDecl:
				if decl.Pub {
					if err := addPub(decl.Name); err != nil {
						return err
					}
					filePubs[decl.Name] = true
					for _, op := range decl.Ops {
						if err := addPub(op.Name); err != nil {
							return err
						}
						filePubs[op.Name] = true
					}
				}
			case ast.TopEffectAlias:
				if decl.Pub {
					if err := addPub(decl.Name); err != nil {
						return err
					}
					filePubs[decl.Name] = true
				}
			case ast.TopInterfaceDecl:
				if decl.Pub {
					for _, m := range decl.Methods {
						if err := addPub(m.Name); err != nil {
							return err
						}
						filePubs[m.Name] = true
					}
				}
			}
		}

		// Register per-file maps so imports *between* package files
		// (e.g. `use lib.helpers` for qualified access) expand normally.
		l.pubMap[absPath] = filePubs
		l.typeMap[absPath] = fileTypes
		l.loaded[absPath] = true

		parsed = append(parsed, parsedFile{path: absPath, program: program})
	}

	// Publish the merged namespace before resolving imports — a dep that
	// circularly imports this package must see its symbols.
	l.packagePubs[pkgName] = mergedPubs
	l.packageTypes[pkgName] = mergedTypes

	// ── Pass 2: resolve each file's own imports, then collect ──
	for _, pf := range parsed {
		fileDir := filepath.Dir(pf.path)
		for _, tl := range pf.program.TopLevels {
			useDecl, ok := tl.(ast.TopUseDecl)
			if !ok {
				continue
			}
			if useDecl.External {
				// Transitive package dependency.
				if err := l.loadExternalPackage(useDecl.Path[0]); err != nil {
					return err
				}
				continue
			}
			// Imports between files of this package are already merged;
			// only load modules that resolve outside the package.
			absPath, err := ResolveModulePath(useDecl.Path, fileDir)
			if err != nil && fileDir != srcRoot {
				absPath, err = ResolveModulePath(useDecl.Path, srcRoot)
			}
			if err == nil && inPackage[absPath] {
				continue
			}
			if err := l.loadModule(useDecl.Path, fileDir); err != nil {
				return err
			}
		}

		// Expand qualified imports and type constructors, resolving
		// against the file's dir with the package src/ root as fallback.
		expanded := l.expandImports(pf.program.TopLevels, fileDir, srcRoot)

		// Append this file's top-levels to the collected pool, skipping
		// mod/test decls like loadModule does.
		for _, tl := range expanded {
			switch tl.(type) {
			case ast.TopModDecl, ast.TopTestDecl:
				continue
			default:
				l.collected = append(l.collected, tl)
			}
		}
	}

	return nil
}

// packageSrcRoot walks up from a package source file to find the src/
// directory that contains it. Falls back to the file's own directory when
// no src/ segment exists (shouldn't happen for discovered package files).
func packageSrcRoot(file string) string {
	dir := filepath.Dir(file)
	for d := dir; ; {
		if filepath.Base(d) == "src" {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

// unknownPackageError returns a helpful error when an external use cannot
// find the named package, listing what packages the loader does know about.
func (l *linker) unknownPackageError(pkgName string) error {
	if len(l.packageFiles) == 0 {
		return fmt.Errorf(
			"external package '%s' not resolved — no dependencies are installed\nhint: run `clank pkg add <url>` to install a package",
			pkgName,
		)
	}
	var available []string
	for name := range l.packageFiles {
		available = append(available, name)
	}
	return fmt.Errorf(
		"external package '%s' not found\n  available: %s\nhint: run `clank pkg add <url>` to install it, or check the spelling",
		pkgName, strings.Join(available, ", "),
	)
}

// enrichNotFound wraps a plain "module not found" error with a list of
// available dep packages, so a user who meant `use &foo` gets a concrete
// hint instead of just "we tried these two files and neither existed."
func (l *linker) enrichNotFound(err error, modPath []string) error {
	if len(l.packageFiles) == 0 {
		return err
	}
	var available []string
	for name := range l.packageFiles {
		available = append(available, name)
	}
	return fmt.Errorf(
		"%v\n  available packages: %s\nhint: did you mean `use &%s`?",
		err, strings.Join(available, ", "), modPath[0],
	)
}
