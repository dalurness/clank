package linter

import (
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

func loc(line, col int) token.Loc {
	return token.Loc{Line: line, Col: col}
}

// helper: make a simple program with one definition
func defProgram(name string, sig ast.TypeSig, body ast.Expr, pub bool) *ast.Program {
	return &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{
				Name: name,
				Sig:  sig,
				Body: body,
				Pub:  pub,
				Loc:  loc(1, 1),
			},
		},
	}
}

func simpleSig() ast.TypeSig {
	return ast.TypeSig{
		Params:     nil,
		ReturnType: ast.TypeName{Name: "Int", Loc: loc(1, 1)},
	}
}

func sigWithParams(params ...ast.TypeSigParam) ast.TypeSig {
	return ast.TypeSig{
		Params:     params,
		ReturnType: ast.TypeName{Name: "Int", Loc: loc(1, 1)},
	}
}

// ── W100: unused variable ──

func TestW100_UnusedLetBinding(t *testing.T) {
	// let unused = 42 in print("hello")
	body := ast.ExprLet{
		Name:  "unused",
		Value: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(2, 7)},
		Body: ast.ExprApply{
			Fn:   ast.ExprVar{Name: "print", Loc: loc(3, 3)},
			Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "hello"}, Loc: loc(3, 9)}},
			Loc:  loc(3, 3),
		},
		Loc: loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W100" && d.Message == "unused variable 'unused'" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W100 for unused variable, got %v", diags)
	}
}

func TestW100_UsedVariable(t *testing.T) {
	// let x = 42 in show(x)
	body := ast.ExprLet{
		Name:  "x",
		Value: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(2, 11)},
		Body: ast.ExprApply{
			Fn:   ast.ExprVar{Name: "show", Loc: loc(3, 3)},
			Args: []ast.Expr{ast.ExprVar{Name: "x", Loc: loc(3, 8)}},
			Loc:  loc(3, 3),
		},
		Loc: loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{})

	for _, d := range diags {
		if d.Code == "W100" {
			t.Errorf("unexpected W100 for used variable: %v", d)
		}
	}
}

func TestW100_UnderscoreIgnored(t *testing.T) {
	// let _ = 42 in print("hello")
	body := ast.ExprLet{
		Name:  "_",
		Value: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(2, 11)},
		Body: ast.ExprApply{
			Fn:   ast.ExprVar{Name: "print", Loc: loc(3, 3)},
			Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "hello"}, Loc: loc(3, 9)}},
			Loc:  loc(3, 3),
		},
		Loc: loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{})

	for _, d := range diags {
		if d.Code == "W100" {
			t.Errorf("unexpected W100 for underscore: %v", d)
		}
	}
}

func TestW100_UnusedLambdaParam(t *testing.T) {
	// fn(x) => 42
	body := ast.ExprLambda{
		Params: []ast.Param{{Name: "x"}},
		Body:   ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(2, 13)},
		Loc:    loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W100" && d.Message == "unused variable 'x'" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W100 for unused lambda param")
	}
}

func TestW100_UnusedBlockBinding(t *testing.T) {
	// { let result = 1  2 }
	body := ast.ExprBlock{
		Exprs: []ast.Expr{
			ast.ExprLet{Name: "result", Value: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(2, 3)}, Loc: loc(2, 1)},
			ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc(3, 3)},
		},
		Loc: loc(1, 1),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W100" && d.Message == "unused variable 'result'" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W100 for unused block binding")
	}
}

// ── W101: unused imports ──

func TestW101_UnusedImport(t *testing.T) {
	prog := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopUseDecl{
				Path:    []string{"std", "math"},
				Imports: []ast.ImportItem{{Name: "sqrt"}},
				Loc:     loc(1, 1),
			},
			ast.TopDefinition{
				Name: "main",
				Sig:  simpleSig(),
				Body: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(3, 3)},
				Loc:  loc(3, 1),
			},
		},
	}
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W101" && d.Message == "unused import 'sqrt'" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W101 for unused import, got %v", diags)
	}
}

// ── W102: shadowed bindings ──

func TestW102_ShadowedBinding(t *testing.T) {
	// let x = 1 in (let x = 2 in show(x))
	body := ast.ExprLet{
		Name:  "x",
		Value: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(2, 11)},
		Body: ast.ExprLet{
			Name:  "x",
			Value: ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc(3, 11)},
			Body: ast.ExprApply{
				Fn:   ast.ExprVar{Name: "show", Loc: loc(4, 3)},
				Args: []ast.Expr{ast.ExprVar{Name: "x", Loc: loc(4, 8)}},
				Loc:  loc(4, 3),
			},
			Loc: loc(3, 3),
		},
		Loc: loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W102" && d.Message == "'x' shadows an existing binding" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W102 for shadowed binding, got %v", diags)
	}
}

// ── W103: missing pub annotation ──

func TestW103_MissingReturnType(t *testing.T) {
	sig := ast.TypeSig{
		Params:     nil,
		ReturnType: ast.TypeName{Name: "_", Loc: loc(1, 1)},
	}
	prog := defProgram("myFn", sig, ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(2, 3)}, true)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W103" && d.Message == "pub function 'myFn' is missing return type annotation" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W103, got %v", diags)
	}
}

func TestW103_MissingParamType(t *testing.T) {
	sig := sigWithParams(ast.TypeSigParam{Name: "x", Type: ast.TypeName{Name: "_", Loc: loc(1, 1)}})
	prog := defProgram("myFn", sig, ast.ExprVar{Name: "x", Loc: loc(2, 3)}, true)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W103" && d.Message == "pub function 'myFn' has untyped parameter 'x'" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W103 for untyped param, got %v", diags)
	}
}

// ── W104: unreachable match arm ──

func TestW104_UnreachableArm(t *testing.T) {
	body := ast.ExprMatch{
		Subject: ast.ExprVar{Name: "c", Loc: loc(2, 9)},
		Arms: []ast.MatchArm{
			{Pattern: ast.PatVar{Name: "x", Loc: loc(3, 5)}, Body: ast.ExprLiteral{Value: ast.LitStr{Value: "a"}, Loc: loc(3, 10)}},
			{Pattern: ast.PatWildcard{Loc: loc(4, 5)}, Body: ast.ExprLiteral{Value: ast.LitStr{Value: "b"}, Loc: loc(4, 10)}},
			{Pattern: ast.PatVariant{Name: "Blue", Loc: loc(5, 5)}, Body: ast.ExprLiteral{Value: ast.LitStr{Value: "c"}, Loc: loc(5, 13)}},
		},
		Loc: loc(2, 3),
	}
	sig := sigWithParams(ast.TypeSigParam{Name: "c", Type: ast.TypeName{Name: "Color", Loc: loc(1, 1)}})
	prog := defProgram("check", sig, body, false)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W104" && d.Message == "unreachable match arm after catch-all pattern" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W104 for unreachable arm, got %v", diags)
	}
}

// ── W105: empty effect handler ──

func TestW105_EmptyHandler(t *testing.T) {
	body := ast.ExprHandle{
		Expr: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(2, 10)},
		Arms: []ast.HandlerArm{
			{Name: "return", Params: []ast.Param{{Name: "x"}}, Body: ast.ExprVar{Name: "x", Loc: loc(3, 18)}},
		},
		Loc: loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W105" && d.Message == "effect handler has no operation arms" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W105 for empty handler, got %v", diags)
	}
}

// ── W106: builtin shadow ──

func TestW106_BuiltinShadow(t *testing.T) {
	sig := sigWithParams(
		ast.TypeSigParam{Name: "a", Type: ast.TypeName{Name: "Int", Loc: loc(1, 1)}},
		ast.TypeSigParam{Name: "b", Type: ast.TypeName{Name: "Int", Loc: loc(1, 1)}},
	)
	body := ast.ExprInfix{
		Op:    "+",
		Left:  ast.ExprVar{Name: "a", Loc: loc(2, 3)},
		Right: ast.ExprVar{Name: "b", Loc: loc(2, 7)},
		Loc:   loc(2, 5),
	}
	prog := defProgram("add", sig, body, false)
	diags := Lint(prog, LintOptions{})

	found := false
	for _, d := range diags {
		if d.Code == "W106" && d.Message == "function 'add' shadows builtin 'add' — this may cause infinite recursion when the corresponding operator dispatches to 'add'" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected W106 for builtin shadow, got %v", diags)
	}
}

func TestW106_NonBuiltinNoWarning(t *testing.T) {
	sig := sigWithParams(
		ast.TypeSigParam{Name: "a", Type: ast.TypeName{Name: "Int", Loc: loc(1, 1)}},
		ast.TypeSigParam{Name: "b", Type: ast.TypeName{Name: "Int", Loc: loc(1, 1)}},
	)
	body := ast.ExprInfix{
		Op:    "+",
		Left:  ast.ExprVar{Name: "a", Loc: loc(2, 3)},
		Right: ast.ExprVar{Name: "b", Loc: loc(2, 7)},
		Loc:   loc(2, 5),
	}
	prog := defProgram("myAdd", sig, body, false)
	diags := Lint(prog, LintOptions{})

	for _, d := range diags {
		if d.Code == "W106" {
			t.Errorf("unexpected W106 for non-builtin: %v", d)
		}
	}
}

// ── Rule filtering ──

func TestDisableRule(t *testing.T) {
	body := ast.ExprLet{
		Name:  "unused",
		Value: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(2, 7)},
		Body: ast.ExprApply{
			Fn:   ast.ExprVar{Name: "print", Loc: loc(3, 3)},
			Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "hello"}, Loc: loc(3, 9)}},
			Loc:  loc(3, 3),
		},
		Loc: loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{
		DisabledRules: map[string]bool{"unused-variable": true},
	})

	for _, d := range diags {
		if d.Code == "W100" {
			t.Errorf("W100 should be disabled: %v", d)
		}
	}
}

func TestEnableOnlyRule(t *testing.T) {
	// This program would normally trigger both W100 (unused) and W102 (shadow)
	body := ast.ExprLet{
		Name:  "x",
		Value: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(2, 11)},
		Body: ast.ExprLet{
			Name:  "x",
			Value: ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc(3, 11)},
			Body: ast.ExprApply{
				Fn:   ast.ExprVar{Name: "print", Loc: loc(4, 3)},
				Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "hello"}, Loc: loc(4, 9)}},
				Loc:  loc(4, 3),
			},
			Loc: loc(3, 3),
		},
		Loc: loc(2, 3),
	}
	prog := defProgram("main", simpleSig(), body, false)
	diags := Lint(prog, LintOptions{
		EnabledRules: map[string]bool{"unused-variable": true},
	})

	hasW100 := false
	for _, d := range diags {
		if d.Code == "W100" {
			hasW100 = true
		}
		if d.Code == "W102" {
			t.Errorf("W102 should not be present when only W100 enabled: %v", d)
		}
	}
	if !hasW100 {
		t.Errorf("expected W100 to still be present")
	}
}
