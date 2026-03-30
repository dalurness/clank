package formatter

import (
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/token"
)

func loc(line, col int) token.Loc {
	return token.Loc{Line: line, Col: col}
}

// roundTrip parses source, formats it, and returns the result.
func roundTrip(t *testing.T, source string) string {
	t.Helper()
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		t.Fatalf("lex error: %s", lexErr.Message)
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		t.Fatalf("parse error: %s", parseErr.Message)
	}
	return Format(program, source)
}

// ── ExtractComments ──

func TestExtractComments(t *testing.T) {
	source := "# comment 1\nfoo = 1\n# comment 2\n"
	comments := ExtractComments(source)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].Line != 1 || comments[0].Text != "# comment 1" {
		t.Errorf("comment 0: %+v", comments[0])
	}
	if comments[1].Line != 3 || comments[1].Text != "# comment 2" {
		t.Errorf("comment 1: %+v", comments[1])
	}
}

// ── Idempotency tests ──

func TestIdempotency_Simple(t *testing.T) {
	source := `main : () -> <io> () = print("hello")
`
	result := roundTrip(t, source)
	result2 := roundTrip(t, result)
	if result != result2 {
		t.Errorf("not idempotent:\nfirst:  %q\nsecond: %q", result, result2)
	}
}

func TestIdempotency_LetBinding(t *testing.T) {
	source := `main : () -> <io> () =
  let x = 42
  print(show(x))
`
	result := roundTrip(t, source)
	result2 := roundTrip(t, result)
	if result != result2 {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", result, result2)
	}
}

// ── Formatting correctness ──

func TestFormat_TrailingNewline(t *testing.T) {
	source := `main : () -> <io> () = print("hi")
`
	result := roundTrip(t, source)
	if !strings.HasSuffix(result, "\n") {
		t.Error("result should end with newline")
	}
}

func TestFormat_BlankLineBetweenTopLevels(t *testing.T) {
	source := `foo : () -> <> Int = 1

bar : () -> <> Int = 2
`
	result := roundTrip(t, source)
	if !strings.Contains(result, "\n\n") {
		t.Error("expected blank line between top-level definitions")
	}
}

func TestFormat_TypeDecl(t *testing.T) {
	source := `type Color = Red | Green | Blue
`
	result := roundTrip(t, source)
	if !strings.Contains(result, "type Color = Red | Green | Blue") {
		t.Errorf("unexpected type decl format: %s", result)
	}
}

func TestFormat_MatchAlignment(t *testing.T) {
	source := `check : (c: Int) -> <> Str =
  match c {
    0 => "zero"
    _ => "other"
  }
`
	result := roundTrip(t, source)
	if !strings.Contains(result, "match c {") {
		t.Errorf("expected match expression: %s", result)
	}
	if !strings.Contains(result, "=>") {
		t.Errorf("expected => in match arms: %s", result)
	}
}

// ── Direct AST formatting tests ──

func TestFormatTypeExpr_Name(t *testing.T) {
	result := formatTypeExpr(ast.TypeName{Name: "Int", Loc: loc(1, 1)})
	if result != "Int" {
		t.Errorf("expected Int, got %s", result)
	}
}

func TestFormatTypeExpr_List(t *testing.T) {
	result := formatTypeExpr(ast.TypeList{Element: ast.TypeName{Name: "Int", Loc: loc(1, 1)}, Loc: loc(1, 1)})
	if result != "[Int]" {
		t.Errorf("expected [Int], got %s", result)
	}
}

func TestFormatTypeExpr_Tuple(t *testing.T) {
	result := formatTypeExpr(ast.TypeTuple{
		Elements: []ast.TypeExpr{
			ast.TypeName{Name: "Int", Loc: loc(1, 1)},
			ast.TypeName{Name: "Str", Loc: loc(1, 1)},
		},
		Loc: loc(1, 1),
	})
	if result != "(Int, Str)" {
		t.Errorf("expected (Int, Str), got %s", result)
	}
}

func TestFormatTypeExpr_Record(t *testing.T) {
	result := formatTypeExpr(ast.TypeRecord{
		Fields: []ast.RecordTypeField{
			{Name: "x", Type: ast.TypeName{Name: "Int", Loc: loc(1, 1)}},
			{Name: "y", Type: ast.TypeName{Name: "Str", Loc: loc(1, 1)}},
		},
		Loc: loc(1, 1),
	})
	if result != "{x: Int, y: Str}" {
		t.Errorf("expected {x: Int, y: Str}, got %s", result)
	}
}

func TestFormatTypeExpr_Fn(t *testing.T) {
	result := formatTypeExpr(ast.TypeFn{
		Param:  ast.TypeName{Name: "Int", Loc: loc(1, 1)},
		Result: ast.TypeName{Name: "Str", Loc: loc(1, 1)},
		Loc:    loc(1, 1),
	})
	if result != "Int -> Str" {
		t.Errorf("expected Int -> Str, got %s", result)
	}
}

func TestFormatTypeExpr_Generic(t *testing.T) {
	result := formatTypeExpr(ast.TypeGeneric{
		Name: "List",
		Args: []ast.TypeExpr{ast.TypeName{Name: "Int", Loc: loc(1, 1)}},
		Loc:  loc(1, 1),
	})
	if result != "List<Int>" {
		t.Errorf("expected List<Int>, got %s", result)
	}
}

func TestFormatTypeExpr_Borrow(t *testing.T) {
	result := formatTypeExpr(ast.TypeBorrow{
		Inner: ast.TypeName{Name: "Int", Loc: loc(1, 1)},
		Loc:   loc(1, 1),
	})
	if result != "&Int" {
		t.Errorf("expected &Int, got %s", result)
	}
}

// ── Expression formatting ──

func TestFormatExpr_Literal(t *testing.T) {
	tests := []struct {
		lit  ast.Literal
		want string
	}{
		{ast.LitInt{Value: 42}, "42"},
		{ast.LitBool{Value: true}, "true"},
		{ast.LitBool{Value: false}, "false"},
		{ast.LitStr{Value: "hello"}, `"hello"`},
		{ast.LitUnit{}, "()"},
		{ast.LitRat{Value: 3.14}, "3.14"},
	}
	for _, tt := range tests {
		result := formatExpr(ast.ExprLiteral{Value: tt.lit, Loc: loc(1, 1)}, 0)
		if result != tt.want {
			t.Errorf("formatLiteral(%T): expected %q, got %q", tt.lit, tt.want, result)
		}
	}
}

func TestFormatExpr_Var(t *testing.T) {
	result := formatExpr(ast.ExprVar{Name: "foo", Loc: loc(1, 1)}, 0)
	if result != "foo" {
		t.Errorf("expected foo, got %s", result)
	}
}

func TestFormatExpr_Apply(t *testing.T) {
	result := formatExpr(ast.ExprApply{
		Fn: ast.ExprVar{Name: "f", Loc: loc(1, 1)},
		Args: []ast.Expr{
			ast.ExprVar{Name: "x", Loc: loc(1, 3)},
			ast.ExprVar{Name: "y", Loc: loc(1, 5)},
		},
		Loc: loc(1, 1),
	}, 0)
	if result != "f(x, y)" {
		t.Errorf("expected f(x, y), got %s", result)
	}
}

func TestFormatExpr_Infix(t *testing.T) {
	result := formatExpr(ast.ExprInfix{
		Op:    "+",
		Left:  ast.ExprVar{Name: "a", Loc: loc(1, 1)},
		Right: ast.ExprVar{Name: "b", Loc: loc(1, 5)},
		Loc:   loc(1, 3),
	}, 0)
	if result != "a + b" {
		t.Errorf("expected a + b, got %s", result)
	}
}

func TestFormatExpr_InfixPrecedence(t *testing.T) {
	// (2 + 3) * 4 should preserve parens
	inner := ast.ExprInfix{
		Op:    "+",
		Left:  ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc(1, 2)},
		Right: ast.ExprLiteral{Value: ast.LitInt{Value: 3}, Loc: loc(1, 6)},
		Loc:   loc(1, 4),
	}
	outer := ast.ExprInfix{
		Op:    "*",
		Left:  inner,
		Right: ast.ExprLiteral{Value: ast.LitInt{Value: 4}, Loc: loc(1, 11)},
		Loc:   loc(1, 9),
	}
	result := formatExpr(outer, 0)
	if result != "(2 + 3) * 4" {
		t.Errorf("expected (2 + 3) * 4, got %s", result)
	}
}

func TestFormatExpr_Lambda(t *testing.T) {
	result := formatExpr(ast.ExprLambda{
		Params: []ast.Param{{Name: "x"}},
		Body:   ast.ExprVar{Name: "x", Loc: loc(1, 10)},
		Loc:    loc(1, 1),
	}, 0)
	if result != "fn(x) => x" {
		t.Errorf("expected fn(x) => x, got %s", result)
	}
}

func TestFormatExpr_List(t *testing.T) {
	result := formatExpr(ast.ExprList{
		Elements: []ast.Expr{
			ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(1, 2)},
			ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc(1, 5)},
			ast.ExprLiteral{Value: ast.LitInt{Value: 3}, Loc: loc(1, 8)},
		},
		Loc: loc(1, 1),
	}, 0)
	if result != "[1, 2, 3]" {
		t.Errorf("expected [1, 2, 3], got %s", result)
	}
}

func TestFormatExpr_EmptyList(t *testing.T) {
	result := formatExpr(ast.ExprList{Loc: loc(1, 1)}, 0)
	if result != "[]" {
		t.Errorf("expected [], got %s", result)
	}
}

func TestFormatExpr_Tuple(t *testing.T) {
	result := formatExpr(ast.ExprTuple{
		Elements: []ast.Expr{
			ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(1, 2)},
			ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc(1, 5)},
		},
		Loc: loc(1, 1),
	}, 0)
	if result != "(1, 2)" {
		t.Errorf("expected (1, 2), got %s", result)
	}
}

func TestFormatExpr_Record(t *testing.T) {
	result := formatExpr(ast.ExprRecord{
		Fields: []ast.RecordField{
			{Name: "x", Value: ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(1, 5)}},
			{Name: "y", Value: ast.ExprLiteral{Value: ast.LitInt{Value: 2}, Loc: loc(1, 12)}},
		},
		Loc: loc(1, 1),
	}, 0)
	if result != "{x: 1, y: 2}" {
		t.Errorf("expected {x: 1, y: 2}, got %s", result)
	}
}

func TestFormatExpr_FieldAccess(t *testing.T) {
	result := formatExpr(ast.ExprFieldAccess{
		Object: ast.ExprVar{Name: "r", Loc: loc(1, 1)},
		Field:  "x",
		Loc:    loc(1, 1),
	}, 0)
	if result != "r.x" {
		t.Errorf("expected r.x, got %s", result)
	}
}

func TestFormatExpr_Range(t *testing.T) {
	result := formatExpr(ast.ExprRange{
		Start:     ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(1, 1)},
		End:       ast.ExprLiteral{Value: ast.LitInt{Value: 10}, Loc: loc(1, 4)},
		Inclusive: false,
		Loc:       loc(1, 1),
	}, 0)
	if result != "1..10" {
		t.Errorf("expected 1..10, got %s", result)
	}
}

func TestFormatExpr_RangeInclusive(t *testing.T) {
	result := formatExpr(ast.ExprRange{
		Start:     ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: loc(1, 1)},
		End:       ast.ExprLiteral{Value: ast.LitInt{Value: 10}, Loc: loc(1, 5)},
		Inclusive: true,
		Loc:       loc(1, 1),
	}, 0)
	if result != "1..=10" {
		t.Errorf("expected 1..=10, got %s", result)
	}
}

// ── Pattern formatting ──

func TestFormatPattern(t *testing.T) {
	tests := []struct {
		pat  ast.Pattern
		want string
	}{
		{ast.PatVar{Name: "x", Loc: loc(1, 1)}, "x"},
		{ast.PatWildcard{Loc: loc(1, 1)}, "_"},
		{ast.PatLiteral{Value: ast.LitInt{Value: 42}, Loc: loc(1, 1)}, "42"},
		{ast.PatVariant{Name: "Some", Args: []ast.Pattern{ast.PatVar{Name: "v", Loc: loc(1, 6)}}, Loc: loc(1, 1)}, "Some(v)"},
		{ast.PatVariant{Name: "None", Loc: loc(1, 1)}, "None"},
		{ast.PatTuple{Elements: []ast.Pattern{ast.PatVar{Name: "a", Loc: loc(1, 2)}, ast.PatVar{Name: "b", Loc: loc(1, 5)}}, Loc: loc(1, 1)}, "(a, b)"},
	}
	for _, tt := range tests {
		result := formatPattern(tt.pat)
		if result != tt.want {
			t.Errorf("formatPattern: expected %q, got %q", tt.want, result)
		}
	}
}

// ── Indentation ──

func TestFormat_Indentation(t *testing.T) {
	result := formatExpr(ast.ExprVar{Name: "x", Loc: loc(1, 1)}, 4)
	if result != "    x" {
		t.Errorf("expected 4-space indent, got %q", result)
	}
}

// ── Sort imports ──

func TestFormat_SortedImports(t *testing.T) {
	result := formatUseDecl(ast.TopUseDecl{
		Path: []string{"std"},
		Imports: []ast.ImportItem{
			{Name: "Zebra"},
			{Name: "Alpha"},
			{Name: "Mid"},
		},
		Loc: loc(1, 1),
	})
	if result != "use std (Alpha, Mid, Zebra)" {
		t.Errorf("expected sorted imports, got %s", result)
	}
}

func TestFormat_ImportAlias(t *testing.T) {
	result := formatUseDecl(ast.TopUseDecl{
		Path: []string{"std"},
		Imports: []ast.ImportItem{
			{Name: "Foo", Alias: "Bar"},
		},
		Loc: loc(1, 1),
	})
	if result != "use std (Foo as Bar)" {
		t.Errorf("expected alias, got %s", result)
	}
}
