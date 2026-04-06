package ast_test

import (
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

func TestLiteralInterface(t *testing.T) {
	literals := []ast.Literal{
		ast.LitInt{Value: 42},
		ast.LitRat{Value: 3.14},
		ast.LitBool{Value: true},
		ast.LitStr{Value: "hello"},
		ast.LitUnit{},
	}
	if len(literals) != 5 {
		t.Fatalf("expected 5 literals, got %d", len(literals))
	}
}

func TestPatternInterface(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	patterns := []ast.Pattern{
		ast.PatVar{Name: "x", Loc: loc},
		ast.PatLiteral{Value: ast.LitInt{Value: 1}, Loc: loc},
		ast.PatVariant{Name: "Some", Args: []ast.Pattern{ast.PatVar{Name: "v", Loc: loc}}, Loc: loc},
		ast.PatTuple{Elements: nil, Loc: loc},
		ast.PatRecord{Fields: nil, Loc: loc},
		ast.PatWildcard{Loc: loc},
	}
	for _, p := range patterns {
		if p.PatLoc() != loc {
			t.Errorf("expected loc %v, got %v", loc, p.PatLoc())
		}
	}
}

func TestTypeExprInterface(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	types := []ast.TypeExpr{
		ast.TypeName{Name: "Int", Loc: loc},
		ast.TypeList{Element: ast.TypeName{Name: "Int", Loc: loc}, Loc: loc},
		ast.TypeTuple{Elements: nil, Loc: loc},
		ast.TypeRecord{Fields: nil, Loc: loc},
		ast.TypeUnion{Left: ast.TypeName{Name: "A", Loc: loc}, Right: ast.TypeName{Name: "B", Loc: loc}, Loc: loc},
		ast.TypeFn{Param: ast.TypeName{Name: "Int", Loc: loc}, Result: ast.TypeName{Name: "Str", Loc: loc}, Loc: loc},
		ast.TypeGeneric{Name: "List", Args: []ast.TypeExpr{ast.TypeName{Name: "Int", Loc: loc}}, Loc: loc},
		ast.TypeRefined{Base: ast.TypeName{Name: "Int", Loc: loc}, Predicate: "> 0", Loc: loc},
		ast.TypeBorrow{Inner: ast.TypeName{Name: "T", Loc: loc}, Loc: loc},
		ast.TypeTagProject{Base: ast.TypeName{Name: "R", Loc: loc}, TagName: "active", Loc: loc},
		ast.TypeTypeFilter{Base: ast.TypeName{Name: "A", Loc: loc}, FilterType: ast.TypeName{Name: "B", Loc: loc}, Loc: loc},
		ast.TypePick{Base: ast.TypeName{Name: "R", Loc: loc}, FieldNames: []string{"x"}, Loc: loc},
		ast.TypeOmit{Base: ast.TypeName{Name: "R", Loc: loc}, FieldNames: []string{"y"}, Loc: loc},
	}
	for _, te := range types {
		if te.TypeLoc() != loc {
			t.Errorf("expected loc %v, got %v", loc, te.TypeLoc())
		}
	}
}

func TestExprInterface(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	exprs := []ast.Expr{
		ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc},
		ast.ExprVar{Name: "x", Loc: loc},
		ast.ExprLet{Name: "x", Value: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc}, Loc: loc},
		ast.ExprIf{
			Cond: ast.ExprVar{Name: "b", Loc: loc},
			Then: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc},
			Else: ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc},
			Loc:  loc,
		},
		ast.ExprLambda{Params: []ast.Param{{Name: "x"}}, Body: ast.ExprVar{Name: "x", Loc: loc}, Loc: loc},
		ast.ExprApply{Fn: ast.ExprVar{Name: "f", Loc: loc}, Args: []ast.Expr{ast.ExprVar{Name: "x", Loc: loc}}, Loc: loc},
		ast.ExprList{Elements: nil, Loc: loc},
		ast.ExprTuple{Elements: nil, Loc: loc},
		ast.ExprRecord{Fields: nil, Loc: loc},
		ast.ExprFieldAccess{Object: ast.ExprVar{Name: "r", Loc: loc}, Field: "x", Loc: loc},
	}
	for _, e := range exprs {
		if e.ExprLoc() != loc {
			t.Errorf("expected loc %v, got %v", loc, e.ExprLoc())
		}
	}
}

func TestTopLevelInterface(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	tops := []ast.TopLevel{
		ast.TopDefinition{Name: "main", Body: ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc}, Loc: loc},
		ast.TopTypeDecl{Name: "Option", TypeParams: []string{"T"}, Variants: []ast.Variant{{Name: "Some", Fields: []ast.TypeExpr{ast.TypeName{Name: "T", Loc: loc}}}, {Name: "None"}}, Loc: loc},
		ast.TopEffectDecl{Name: "IO", Loc: loc},
		ast.TopEffectAlias{Name: "Pure", Loc: loc},
		ast.TopModDecl{Path: []string{"std", "io"}, Loc: loc},
		ast.TopUseDecl{Path: []string{"std", "io"}, Imports: []ast.ImportItem{{Name: "print"}}, Loc: loc},
		ast.TopTestDecl{Name: "basic", Body: ast.ExprLiteral{Value: ast.LitBool{Value: true}, Loc: loc}, Loc: loc},
		ast.TopInterfaceDecl{Name: "Show", TypeParams: []string{"T"}, Loc: loc},
		ast.TopImplBlock{Interface: "Show", ForType: ast.TypeName{Name: "Int", Loc: loc}, Loc: loc},
	}
	for _, tl := range tops {
		if tl.TopLoc() != loc {
			t.Errorf("expected loc %v, got %v", loc, tl.TopLoc())
		}
	}
}

func TestProgram(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	prog := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{Name: "main", Body: ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc}, Loc: loc},
		},
	}
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top level, got %d", len(prog.TopLevels))
	}
}
