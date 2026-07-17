package linter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePkgFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLintPackage_DuplicatePubFunction(t *testing.T) {
	dir := t.TempDir()
	a := writePkgFile(t, dir, "a.clk", `
pub greet : (name: Str) -> <> Str =
  "hi " ++ name
`)
	b := writePkgFile(t, dir, "b.clk", `
pub greet : (who: Str) -> <> Str =
  "hello " ++ who
`)
	diags := LintPackage([]string{a, b})
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %+v", len(diags), diags)
	}
	if diags[0].Code != "W220" {
		t.Errorf("code = %q, want W220", diags[0].Code)
	}
	if !strings.Contains(diags[0].Message, "greet") {
		t.Errorf("message missing symbol name: %q", diags[0].Message)
	}
	// Both file paths should appear so the author can navigate.
	if !strings.Contains(diags[0].Message, "a.clk") || !strings.Contains(diags[0].Message, "b.clk") {
		t.Errorf("message missing file paths: %q", diags[0].Message)
	}
}

func TestLintPackage_DuplicateAcrossKinds(t *testing.T) {
	// A `pub foo` function and a `pub type foo` both try to own the name.
	dir := t.TempDir()
	a := writePkgFile(t, dir, "fn.clk", `
pub foo : (x: Int) -> <> Int =
  x
`)
	b := writePkgFile(t, dir, "type.clk", `
pub type foo = Foo1 | Foo2
`)
	diags := LintPackage([]string{a, b})
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %+v", len(diags), diags)
	}
	if diags[0].Code != "W220" {
		t.Errorf("code = %q, want W220", diags[0].Code)
	}
}

func TestLintPackage_NoDuplicates(t *testing.T) {
	dir := t.TempDir()
	a := writePkgFile(t, dir, "a.clk", `
pub greet : (name: Str) -> <> Str =
  "hi " ++ name
`)
	b := writePkgFile(t, dir, "b.clk", `
pub farewell : (name: Str) -> <> Str =
  "bye " ++ name
`)
	diags := LintPackage([]string{a, b})
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics, got %d: %+v", len(diags), diags)
	}
}

func TestLintPackage_SingleFileSkipped(t *testing.T) {
	// Single-file packages can't have cross-file collisions — LintPackage
	// should bail out without scanning.
	dir := t.TempDir()
	a := writePkgFile(t, dir, "solo.clk", `
pub greet : (name: Str) -> <> Str = name
`)
	diags := LintPackage([]string{a})
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics, got %d", len(diags))
	}
}
