package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
)

// writeFile creates a file (and parents) under dir.
func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// parseSource lexes and parses clank source, failing the test on error.
func parseSource(t *testing.T, source string) *ast.Program {
	t.Helper()
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		t.Fatalf("lex: %s", lexErr.Message)
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		t.Fatalf("parse: %s", parseErr.Message)
	}
	return program
}

// definitionNames returns the names of all top-level definitions.
func definitionNames(p *ast.Program) map[string]bool {
	names := make(map[string]bool)
	for _, tl := range p.TopLevels {
		if def, ok := tl.(ast.TopDefinition); ok {
			names[def.Name] = true
		}
	}
	return names
}

func TestLinkExternalPackageFlatMerge(t *testing.T) {
	tmp := t.TempDir()
	a := writeFile(t, tmp, "pkg/src/greet.clk", `
pub greet : (name: Str) -> <> Str =
  "hi " ++ name
`)
	b := writeFile(t, tmp, "pkg/src/farewell.clk", `
pub farewell : (name: Str) -> <> Str =
  "bye " ++ name
`)

	program := parseSource(t, `
use &mylib

main : () -> <> Str =
  mylib.greet("x")
`)
	result := LinkWithPackages(program, tmp, map[string][]string{"mylib": {a, b}})
	if result.Error != nil {
		t.Fatalf("link error: %v", result.Error)
	}
	defs := definitionNames(result.Program)
	if !defs["greet"] || !defs["farewell"] {
		t.Errorf("expected both package files merged, got defs %v", defs)
	}

	// The qualified use decl should have its import list filled with all
	// merged pub names so the compiler can register mylib.greet etc.
	for _, tl := range result.Program.TopLevels {
		use, ok := tl.(ast.TopUseDecl)
		if !ok || !use.External {
			continue
		}
		var names []string
		for _, imp := range use.Imports {
			names = append(names, imp.Name)
		}
		joined := strings.Join(names, ",")
		if !strings.Contains(joined, "greet") || !strings.Contains(joined, "farewell") {
			t.Errorf("qualified external use not expanded: imports = %v", names)
		}
	}
}

func TestLinkExternalPackageInternalQualifiedUse(t *testing.T) {
	// A package file uses another file of the same package with qualified
	// access (`use util` + `util.shout`). The internal use decl must be
	// expanded so the compiler can register the dotted name.
	tmp := t.TempDir()
	util := writeFile(t, tmp, "pkg/src/util.clk", `
pub shout : (s: Str) -> <> Str =
  s ++ "!"
`)
	main := writeFile(t, tmp, "pkg/src/main.clk", `
use util

pub greet : (name: Str) -> <> Str =
  util.shout("hi " ++ name)
`)

	program := parseSource(t, `
use &mylib (greet)

main : () -> <> Str =
  greet("x")
`)
	result := LinkWithPackages(program, tmp, map[string][]string{"mylib": {main, util}})
	if result.Error != nil {
		t.Fatalf("link error: %v", result.Error)
	}

	// The internal `use util` decl must survive with imports filled.
	found := false
	for _, tl := range result.Program.TopLevels {
		use, ok := tl.(ast.TopUseDecl)
		if !ok || use.External || len(use.Path) != 1 || use.Path[0] != "util" {
			continue
		}
		found = true
		if len(use.Imports) == 0 {
			t.Error("internal qualified use not expanded — util.shout would not resolve")
		}
	}
	if !found {
		t.Error("internal use decl was dropped")
	}
}

func TestLinkExternalPackageTransitiveDep(t *testing.T) {
	// pkg-a depends on pkg-b via `use &pkg-b`. Consuming pkg-a alone must
	// pull in pkg-b's definitions too.
	tmp := t.TempDir()
	aFile := writeFile(t, tmp, "a/src/a.clk", `
use &pkg-b (base)

pub top : () -> <> Str =
  base() ++ " via a"
`)
	bFile := writeFile(t, tmp, "b/src/b.clk", `
pub base : () -> <> Str =
  "b"
`)

	program := parseSource(t, `
use &pkg-a (top)

main : () -> <> Str =
  top()
`)
	result := LinkWithPackages(program, tmp, map[string][]string{
		"pkg-a": {aFile},
		"pkg-b": {bFile},
	})
	if result.Error != nil {
		t.Fatalf("link error: %v", result.Error)
	}
	defs := definitionNames(result.Program)
	if !defs["top"] || !defs["base"] {
		t.Errorf("transitive package not merged, got defs %v", defs)
	}
}

func TestLinkExternalPackageDuplicateExport(t *testing.T) {
	tmp := t.TempDir()
	a := writeFile(t, tmp, "pkg/src/a.clk", `
pub greet : (name: Str) -> <> Str =
  "hi"
`)
	b := writeFile(t, tmp, "pkg/src/b.clk", `
pub greet : (name: Str) -> <> Str =
  "hello"
`)

	program := parseSource(t, `use &mylib`)
	result := LinkWithPackages(program, tmp, map[string][]string{"mylib": {a, b}})
	if result.Error == nil {
		t.Fatal("expected duplicate export error")
	}
	if !strings.Contains(result.Error.Error(), "duplicate export 'greet'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestLinkExternalPackageUnknown(t *testing.T) {
	program := parseSource(t, `use &nope`)

	// With no packages installed at all.
	result := LinkWithPackages(program, t.TempDir(), nil)
	if result.Error == nil || !strings.Contains(result.Error.Error(), "no dependencies are installed") {
		t.Errorf("expected no-deps hint, got: %v", result.Error)
	}

	// With other packages available — should list them.
	tmp := t.TempDir()
	f := writeFile(t, tmp, "pkg/src/x.clk", `
pub x : () -> <> Int =
  1
`)
	result = LinkWithPackages(program, tmp, map[string][]string{"other": {f}})
	if result.Error == nil || !strings.Contains(result.Error.Error(), "available: other") {
		t.Errorf("expected available-packages hint, got: %v", result.Error)
	}
}

func TestLinkLocalMissingModuleHintsPackages(t *testing.T) {
	// `use foo` with no local module but an installed package named foo
	// should hint `use &foo`.
	tmp := t.TempDir()
	f := writeFile(t, tmp, "pkg/src/x.clk", `
pub x : () -> <> Int =
  1
`)
	program := parseSource(t, `use foo`)
	result := LinkWithPackages(program, tmp, map[string][]string{"foo": {f}})
	if result.Error == nil || !strings.Contains(result.Error.Error(), "use &foo") {
		t.Errorf("expected &foo hint, got: %v", result.Error)
	}
}

func TestLinkUnknownExportInPackage(t *testing.T) {
	tmp := t.TempDir()
	a := writeFile(t, tmp, "pkg/src/greet.clk", `
pub hello : () -> <> Str =
  "hi"
`)

	program := parseSource(t, `
use &mylib (greet)

main : () -> <> Str =
  greet()
`)
	result := LinkWithPackages(program, tmp, map[string][]string{"mylib": {a}})
	if result.Error == nil {
		t.Fatal("expected link error for unknown export, got none")
	}
	msg := result.Error.Error()
	if !strings.Contains(msg, "has no export 'greet'") || !strings.Contains(msg, "hello") {
		t.Errorf("error should name the missing export and list available ones, got: %s", msg)
	}
}

func TestLinkUnknownExportInLocalModule(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, tmp, "helpers.clk", `
mod helpers

pub double : (n: Int) -> <> Int =
  n * 2
`)

	program := parseSource(t, `
use helpers (triple)

main : () -> <> Int =
  triple(2)
`)
	result := Link(program, tmp)
	if result.Error == nil {
		t.Fatal("expected link error for unknown module export, got none")
	}
	if !strings.Contains(result.Error.Error(), "has no export 'triple'") {
		t.Errorf("unexpected error: %s", result.Error.Error())
	}
}
