package checker

import (
	"testing"
	"strings"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

func TestCloneReturnsArgType(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// let x = clone(42) in add(x, 1) — should type-check without error
	// because clone(42) should return Int
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopTypeDecl{
				Name:     "MyT",
				Variants: []ast.Variant{{Name: "MyT", Fields: []ast.TypeExpr{ast.TypeName{Name: "Int"}}}},
				Deriving: []string{"Clone"},
				Loc:      loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Int"},
				},
				Body: ast.ExprLet{
					Name:  "x",
					Value: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "clone", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
						Loc:  loc,
					},
					Body: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "add", Loc: loc},
						Args: []ast.Expr{
							ast.ExprVar{Name: "x", Loc: loc},
							ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc},
						},
						Loc: loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}
	errs := TypeCheck(program)
	for _, e := range errs {
		t.Logf("Error: %s", e.Error())
	}
	for _, e := range errs {
		if e.Code == "E304" {
			t.Fatalf("clone should return Int, got type error: %s", e.Error())
		}
	}
}

func TestIntoReturnsTargetType(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// impl Into<Str> for Int { into(self) = show(self) }
	// let x = into(42)
	// str.cat(x, " hello") — should work if into returns Str
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopImplBlock{
				Interface: "Into",
				TypeArgs:  []ast.TypeExpr{ast.TypeName{Name: "Str"}},
				ForType:   ast.TypeName{Name: "Int"},
				Methods: []ast.ImplMethod{
					{Name: "into", Body: ast.ExprLambda{
						Params: []ast.Param{{Name: "self"}},
						Body:   ast.ExprApply{Fn: ast.ExprVar{Name: "show", Loc: loc}, Args: []ast.Expr{ast.ExprVar{Name: "self", Loc: loc}}, Loc: loc},
						Loc:    loc,
					}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Body: ast.ExprLet{
					Name:  "x",
					Value: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "into", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
						Loc:  loc,
					},
					Body: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "str.cat", Loc: loc},
						Args: []ast.Expr{
							ast.ExprVar{Name: "x", Loc: loc},
							ast.ExprLiteral{Value: ast.LitStr{Value: " hello"}, Loc: loc},
						},
						Loc: loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}
	errs := TypeCheck(program)
	for _, e := range errs {
		t.Logf("Error: %s", e.Error())
	}
	for _, e := range errs {
		if e.Code == "E304" && strings.Contains(e.Message, "str.cat") {
			t.Fatalf("into should return Str, got type error: %s", e.Error())
		}
	}
}

func TestFromReturnsTargetType(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// impl From<Int> for Str { from(val) = show(val) }
	// let x = from(42)
	// str.cat(x, " hello")
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopImplBlock{
				Interface: "From",
				TypeArgs:  []ast.TypeExpr{ast.TypeName{Name: "Int"}},
				ForType:   ast.TypeName{Name: "Str"},
				Methods: []ast.ImplMethod{
					{Name: "from", Body: ast.ExprLambda{
						Params: []ast.Param{{Name: "val"}},
						Body:   ast.ExprApply{Fn: ast.ExprVar{Name: "show", Loc: loc}, Args: []ast.Expr{ast.ExprVar{Name: "val", Loc: loc}}, Loc: loc},
						Loc:    loc,
					}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Body: ast.ExprLet{
					Name:  "x",
					Value: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "from", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
						Loc:  loc,
					},
					Body: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "str.cat", Loc: loc},
						Args: []ast.Expr{
							ast.ExprVar{Name: "x", Loc: loc},
							ast.ExprLiteral{Value: ast.LitStr{Value: " hello"}, Loc: loc},
						},
						Loc: loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}
	errs := TypeCheck(program)
	for _, e := range errs {
		t.Logf("Error: %s", e.Error())
	}
	for _, e := range errs {
		if e.Code == "E304" && strings.Contains(e.Message, "str.cat") {
			t.Fatalf("from should return Str, got type error: %s", e.Error())
		}
	}
}

func TestCrossPackageTypeChecking(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// use some-module (fetch)
	// let result = fetch("url")
	// Should resolve fetch's type from module resolver, not TAny
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopUseDecl{
				Path:    []string{"some", "module"},
				Imports: []ast.ImportItem{{Name: "fetch"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Body: ast.ExprLet{
					Name: "result",
					Value: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "fetch", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "url"}, Loc: loc}},
						Loc:  loc,
					},
					// Use result as Str — if resolved properly should work or fail correctly
					Body: ast.ExprVar{Name: "result", Loc: loc},
					Loc:  loc,
				},
				Loc: loc,
			},
		},
	}

	// Module resolver that returns fetch : Str -> Str
	resolver := func(path []string) map[string]Type {
		if len(path) == 2 && path[0] == "some" && path[1] == "module" {
			return map[string]Type{
				"fetch": NewTFn(TStr, TStr),
			}
		}
		return nil
	}

	errs := TypeCheckWithModules(program, resolver)
	for _, e := range errs {
		t.Logf("Error: %s", e.Error())
	}
	// Should NOT get E304 since fetch(Str) -> Str and we expect Str
	for _, e := range errs {
		if e.Code == "E304" {
			t.Fatalf("cross-package type resolution failed: %s", e.Error())
		}
	}
}

func TestShowReturnsStr(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// let x = show(42) in str.cat(x, "!") — show returns Str
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Body: ast.ExprLet{
					Name: "x",
					Value: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "show", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
						Loc:  loc,
					},
					Body: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "str.cat", Loc: loc},
						Args: []ast.Expr{
							ast.ExprVar{Name: "x", Loc: loc},
							ast.ExprLiteral{Value: ast.LitStr{Value: "!"}, Loc: loc},
						},
						Loc: loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}
	errs := TypeCheck(program)
	for _, e := range errs {
		if e.Code == "E304" {
			t.Fatalf("show should return Str: %s", e.Error())
		}
	}
}

func TestCrossPackageTypeChecksArgs(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// use some-module (fetch)
	// fetch(42) where fetch : Str -> Str — should error on arg type
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopUseDecl{
				Path:    []string{"some", "module"},
				Imports: []ast.ImportItem{{Name: "fetch"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Body: ast.ExprApply{
					Fn:   ast.ExprVar{Name: "fetch", Loc: loc},
					Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
					Loc:  loc,
				},
				Loc: loc,
			},
		},
	}

	resolver := func(path []string) map[string]Type {
		if len(path) == 2 && path[0] == "some" && path[1] == "module" {
			return map[string]Type{
				"fetch": NewTFn(TStr, TStr),
			}
		}
		return nil
	}

	errs := TypeCheckWithModules(program, resolver)
	found := false
	for _, e := range errs {
		if e.Code == "E304" && strings.Contains(e.Message, "Int") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected type error for Int arg to fetch(Str), but got none")
	}
}

func TestCrossPackageWithAlias(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// use some-module (fetch as get)
	// get("url") should resolve to Str
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopUseDecl{
				Path:    []string{"some", "module"},
				Imports: []ast.ImportItem{{Name: "fetch", Alias: "get"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Body: ast.ExprApply{
					Fn:   ast.ExprVar{Name: "get", Loc: loc},
					Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "url"}, Loc: loc}},
					Loc:  loc,
				},
				Loc: loc,
			},
		},
	}

	resolver := func(path []string) map[string]Type {
		if len(path) == 2 && path[0] == "some" && path[1] == "module" {
			return map[string]Type{
				"fetch": NewTFn(TStr, TStr),
			}
		}
		return nil
	}

	errs := TypeCheckWithModules(program, resolver)
	for _, e := range errs {
		if e.Code == "E304" {
			t.Fatalf("aliased import should resolve type: %s", e.Error())
		}
	}
}

func TestCrossPackageTypeMismatch(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// use some-module (fetch)
	// let result = fetch("url") in add(result, 1)
	// fetch returns Str, add expects Int — should error
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopUseDecl{
				Path:    []string{"some", "module"},
				Imports: []ast.ImportItem{{Name: "fetch"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "Int"},
				},
				Body: ast.ExprLet{
					Name: "result",
					Value: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "fetch", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "url"}, Loc: loc}},
						Loc:  loc,
					},
					Body: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "add", Loc: loc},
						Args: []ast.Expr{
							ast.ExprVar{Name: "result", Loc: loc},
							ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc},
						},
						Loc: loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}

	resolver := func(path []string) map[string]Type {
		if len(path) == 2 && path[0] == "some" && path[1] == "module" {
			return map[string]Type{
				"fetch": NewTFn(TStr, TStr),
			}
		}
		return nil
	}

	errs := TypeCheckWithModules(program, resolver)
	found := false
	for _, e := range errs {
		if e.Code == "E304" && strings.Contains(e.Message, "Str") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected type error for Str passed to add(Int,Int), but got none")
	}
}
