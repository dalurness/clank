package testrunner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

var loc = token.Loc{Line: 1, Col: 1}

func litExpr(v int64) ast.Expr {
	return ast.ExprLiteral{Value: ast.LitInt{Value: v}, Loc: loc}
}

func TestDiscoverTests_TestDecl(t *testing.T) {
	prog := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopTestDecl{Name: "one plus one", Body: litExpr(2), Loc: loc},
			ast.TopTestDecl{Name: "string value", Body: litExpr(0), Loc: loc},
		},
	}
	tests := DiscoverTests(prog, "test.pass")
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	if tests[0].Name != "one plus one" {
		t.Errorf("expected 'one plus one', got %q", tests[0].Name)
	}
	if tests[0].Module != "test.pass" {
		t.Errorf("expected module 'test.pass', got %q", tests[0].Module)
	}
}

func TestDiscoverTests_TestFunctions(t *testing.T) {
	prog := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{
				Name: "test_add",
				Sig:  ast.TypeSig{ReturnType: ast.TypeName{Name: "Int", Loc: loc}},
				Body: litExpr(5),
				Loc:  loc,
			},
			ast.TopDefinition{
				Name: "test_sub",
				Sig:  ast.TypeSig{ReturnType: ast.TypeName{Name: "Int", Loc: loc}},
				Body: litExpr(3),
				Loc:  loc,
			},
			ast.TopDefinition{
				Name: "helper",
				Sig:  ast.TypeSig{ReturnType: ast.TypeName{Name: "Int", Loc: loc}},
				Body: litExpr(42),
				Loc:  loc,
			},
		},
	}
	tests := DiscoverTests(prog, "test.fn")
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests (helper excluded), got %d", len(tests))
	}
}

func TestDiscoverTests_Mixed(t *testing.T) {
	prog := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopTestDecl{Name: "declarative test", Body: litExpr(2), Loc: loc},
			ast.TopDefinition{
				Name: "test_functional",
				Sig:  ast.TypeSig{ReturnType: ast.TypeName{Name: "Int", Loc: loc}},
				Body: litExpr(4),
				Loc:  loc,
			},
		},
	}
	tests := DiscoverTests(prog, "test.mixed")
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
}

func TestFilterTests(t *testing.T) {
	tests := []TestCase{
		{Name: "math addition"},
		{Name: "math subtraction"},
		{Name: "string concat"},
	}
	filtered := FilterTests(tests, "math")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered tests, got %d", len(filtered))
	}
	for _, tc := range filtered {
		if !contains(tc.Name, "math") {
			t.Errorf("test %q should contain 'math'", tc.Name)
		}
	}
}

func TestFilterTests_Empty(t *testing.T) {
	tests := []TestCase{{Name: "a"}, {Name: "b"}}
	filtered := FilterTests(tests, "")
	if len(filtered) != 2 {
		t.Fatalf("empty filter should return all tests, got %d", len(filtered))
	}
}

func TestRunTests_AllPass(t *testing.T) {
	tests := []TestCase{
		{Name: "test1", Module: "mod"},
		{Name: "test2", Module: "mod"},
	}
	eval := func(expr ast.Expr) error { return nil }
	result := RunTests(tests, eval)
	if !result.Ok {
		t.Error("expected ok=true")
	}
	if result.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", result.Summary.Total)
	}
	if result.Summary.Passed != 2 {
		t.Errorf("expected passed=2, got %d", result.Summary.Passed)
	}
	if result.Summary.Failed != 0 {
		t.Errorf("expected failed=0, got %d", result.Summary.Failed)
	}
}

func TestRunTests_WithFailure(t *testing.T) {
	tests := []TestCase{
		{Name: "passes", Module: "mod"},
		{Name: "fails", Module: "mod"},
	}
	eval := func(expr ast.Expr) error {
		// First call succeeds, second fails (we use the test name via closure)
		return nil
	}
	callCount := 0
	eval = func(expr ast.Expr) error {
		callCount++
		if callCount == 2 {
			return errors.New("division by zero")
		}
		return nil
	}
	result := RunTests(tests, eval)
	if result.Ok {
		t.Error("expected ok=false")
	}
	if result.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", result.Summary.Total)
	}
	if result.Summary.Passed != 1 {
		t.Errorf("expected passed=1, got %d", result.Summary.Passed)
	}
	if result.Summary.Failed != 1 {
		t.Errorf("expected failed=1, got %d", result.Summary.Failed)
	}
	// Check failure info
	var failedTest *TestResult
	for i := range result.Tests {
		if result.Tests[i].Status == "fail" {
			failedTest = &result.Tests[i]
			break
		}
	}
	if failedTest == nil {
		t.Fatal("expected a failed test")
	}
	if failedTest.Failure == nil {
		t.Fatal("expected failure field")
	}
	if failedTest.Failure.Message == "" {
		t.Error("expected non-empty failure message")
	}
}

func TestRunTests_NoTests(t *testing.T) {
	result := RunTests(nil, func(expr ast.Expr) error { return nil })
	if result.Ok {
		t.Error("expected ok=false for empty test set")
	}
	if result.Summary.Total != 0 {
		t.Errorf("expected total=0, got %d", result.Summary.Total)
	}
}

func TestRunTests_ResultFields(t *testing.T) {
	tests := []TestCase{
		{Name: "check fields", Module: "test.structure"},
	}
	result := RunTests(tests, func(expr ast.Expr) error { return nil })
	if len(result.Tests) != 1 {
		t.Fatalf("expected 1 test result, got %d", len(result.Tests))
	}
	tr := result.Tests[0]
	if tr.Name != "check fields" {
		t.Errorf("expected name 'check fields', got %q", tr.Name)
	}
	if tr.Module != "test.structure" {
		t.Errorf("expected module 'test.structure', got %q", tr.Module)
	}
	if tr.Status != "pass" {
		t.Errorf("expected status 'pass', got %q", tr.Status)
	}
}

func TestExtractModule(t *testing.T) {
	prog := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopModDecl{Path: []string{"test", "structure"}, Loc: loc},
			ast.TopTestDecl{Name: "a test", Body: litExpr(1), Loc: loc},
		},
	}
	mod := ExtractModule(prog)
	if mod != "test.structure" {
		t.Errorf("expected 'test.structure', got %q", mod)
	}
}

func TestExtractModule_NoMod(t *testing.T) {
	prog := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopTestDecl{Name: "a test", Body: litExpr(1), Loc: loc},
		},
	}
	mod := ExtractModule(prog)
	if mod != "" {
		t.Errorf("expected empty string, got %q", mod)
	}
}

func TestDiscoverTestFiles(t *testing.T) {
	dir := t.TempDir()
	// Create some .clk files
	os.WriteFile(filepath.Join(dir, "a-test.clk"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(dir, "b-test.clk"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not clank"), 0644)

	files, err := DiscoverTestFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 .clk files, got %d", len(files))
	}
}

func TestDiscoverTestFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	files, err := DiscoverTestFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if !IsDirectory(dir) {
		t.Error("expected true for directory")
	}
	f := filepath.Join(dir, "file.txt")
	os.WriteFile(f, []byte("x"), 0644)
	if IsDirectory(f) {
		t.Error("expected false for file")
	}
	if IsDirectory("/nonexistent/path") {
		t.Error("expected false for nonexistent path")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsStr(s, sub)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
