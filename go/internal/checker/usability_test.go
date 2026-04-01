package checker

import (
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

func TestEqWithRecords(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// eq({x: 1}, {x: 1}) should type check without errors
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprApply{
					Fn: ast.ExprVar{Name: "eq", Loc: loc},
					Args: []ast.Expr{
						ast.ExprRecord{Fields: []ast.RecordField{{Name: "x", Value: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc}}}, Loc: loc},
						ast.ExprRecord{Fields: []ast.RecordField{{Name: "x", Value: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc}}}, Loc: loc},
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}
	errs := TypeCheck(program)
	for _, e := range errs {
		if e.Code == "E304" && strings.Contains(e.Message, "argument") {
			t.Fatalf("eq with records failed: %s", e.Error())
		}
	}
}

func TestUserInterfaceSelfPoly(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	// interface Describe { describe : (Self) -> <> Str }
	// describe(42) should work if impl exists
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopInterfaceDecl{
				Name: "Describe",
				Methods: []ast.MethodSig{
					{Name: "describe", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "self", Type: ast.TypeName{Name: "Self"}}},
						ReturnType: ast.TypeName{Name: "Str"},
					}},
				},
				Loc: loc,
			},
			ast.TopImplBlock{
				Interface: "Describe",
				ForType:   ast.TypeName{Name: "Int"},
				Methods: []ast.ImplMethod{
					{Name: "describe", Body: ast.ExprLambda{
						Params: []ast.Param{{Name: "x"}},
						Body:   ast.ExprLiteral{Value: ast.LitStr{Value: "int"}, Loc: loc},
						Loc:    loc,
					}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprApply{
					Fn:   ast.ExprVar{Name: "describe", Loc: loc},
					Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
					Loc:  loc,
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
		if e.Code == "E304" && strings.Contains(e.Message, "Self") {
			t.Fatalf("Self not polymorphic: %s", e.Error())
		}
	}
}
