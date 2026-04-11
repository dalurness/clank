package parser_test

import (
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
)

// Helper: lex then parse, fail test on error
func mustParse(t *testing.T, source string) *ast.Program {
	t.Helper()
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		t.Fatalf("lex error: %s", lexErr.Message)
	}
	prog, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		t.Fatalf("parse error: %s (at line %d col %d)", parseErr.Message, parseErr.Location.Line, parseErr.Location.Col)
	}
	return prog
}

// Helper: lex then parse expression
func mustParseExpr(t *testing.T, source string) ast.Expr {
	t.Helper()
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		t.Fatalf("lex error: %s", lexErr.Message)
	}
	expr, parseErr := parser.ParseExpression(tokens)
	if parseErr != nil {
		t.Fatalf("parse error: %s", parseErr.Message)
	}
	return expr
}

// ── Phase 1 test programs ──

func TestPhase1_01_Arithmetic(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  print(show(10 + 5))
  print(show(4 - 7))
  print(show(6 * 7))
  print(show(10 / 3))
  print(show(17 % 5))
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
	def, ok := prog.TopLevels[0].(ast.TopDefinition)
	if !ok {
		t.Fatal("expected TopDefinition")
	}
	if def.Name != "main" {
		t.Errorf("expected name 'main', got %q", def.Name)
	}
}

func TestPhase1_02_LetBinding(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  let x = 10
  let y = 20
  print(show(x + y))
  print(show(x + 15))
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
}

func TestPhase1_03_NestedLet(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  print(show(let a = 3 in let b = 4 in a * b * 5))
  let outer = 10
  let inner = let x = outer + 1 in x + 2
  print(show(inner))
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
}

func TestPhase1_04_IfThenElse(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  print(if true then "yes" else "no")
  print(show(if 10 > 5 then 1 else 0))
  let n = -5
  print(if n > 0 then "positive" else if n == 0 then "zero" else "negative")
  print(if 0 == 0 then "zero" else "nonzero")
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
}

func TestPhase1_05_Factorial(t *testing.T) {
	prog := mustParse(t, `
factorial : (n: Int) -> <> Int =
  if n == 0 then 1
  else n * factorial(n - 1)

main : () -> <io> () =
  print(show(factorial(0)))
  print(show(factorial(1)))
  print(show(factorial(5)))
  print(show(factorial(10)))
`)
	if len(prog.TopLevels) != 2 {
		t.Fatalf("expected 2 top-levels, got %d", len(prog.TopLevels))
	}
	factDef, ok := prog.TopLevels[0].(ast.TopDefinition)
	if !ok {
		t.Fatal("expected TopDefinition for factorial")
	}
	if factDef.Name != "factorial" {
		t.Errorf("expected 'factorial', got %q", factDef.Name)
	}
	// Check type sig: one param 'n' of type Int
	if len(factDef.Sig.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(factDef.Sig.Params))
	}
	if factDef.Sig.Params[0].Name != "n" {
		t.Errorf("expected param 'n', got %q", factDef.Sig.Params[0].Name)
	}
	paramType, ok := factDef.Sig.Params[0].Type.(ast.TypeName)
	if !ok || paramType.Name != "Int" {
		t.Errorf("expected param type Int")
	}
}

func TestPhase1_06_Fibonacci(t *testing.T) {
	prog := mustParse(t, `
fib : (n: Int) -> <> Int =
  if n == 0 then 0
  else if n == 1 then 1
  else fib(n - 1) + fib(n - 2)

main : () -> <io> () =
  print(show(fib(0)))
  print(show(fib(1)))
  print(show(fib(2)))
  print(show(fib(5)))
  print(show(fib(10)))
`)
	if len(prog.TopLevels) != 2 {
		t.Fatalf("expected 2 top-levels, got %d", len(prog.TopLevels))
	}
}

func TestPhase1_07_MutualRecursion(t *testing.T) {
	prog := mustParse(t, `
is-even : (n: Int) -> <> Bool =
  if n == 0 then true
  else is-odd(n - 1)

is-odd : (n: Int) -> <> Bool =
  if n == 0 then false
  else is-even(n - 1)

main : () -> <io> () =
  print(show(is-even(0)))
  print(show(is-even(1)))
  print(show(is-even(4)))
  print(show(is-odd(4)))
  print(show(is-odd(7)))
`)
	if len(prog.TopLevels) != 3 {
		t.Fatalf("expected 3 top-levels, got %d", len(prog.TopLevels))
	}
	def0, ok := prog.TopLevels[0].(ast.TopDefinition)
	if !ok || def0.Name != "is-even" {
		t.Errorf("expected is-even, got %v", prog.TopLevels[0])
	}
	def1, ok := prog.TopLevels[1].(ast.TopDefinition)
	if !ok || def1.Name != "is-odd" {
		t.Errorf("expected is-odd, got %v", prog.TopLevels[1])
	}
}

func TestPhase1_08_BooleanLogic(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  print(show(true && false))
  print(show(true || false))
  print(show(!false))
  print(show(!(true || false)))
  print(show((true || false) && !(false && true)))
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
}

func TestPhase1_09_StringLiterals(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  print("hello")
  print("hello" ++ " " ++ "world")
  print("")
  print("abc" ++ show(123))
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
}

func TestPhase1_10_ComparisonOps(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  print(show(1 == 1))
  print(show(1 != 1))
  print(show(1 < 2))
  print(show(2 < 1))
  print(show(1 <= 1))
  print(show(2 >= 2))
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
}

func TestPhase1_11_OperatorPrecedence(t *testing.T) {
	prog := mustParse(t, `
main : () -> <io> () =
  print(show(2 + 3 * 4))
  print(show((2 + 3) * 4))
  print(show(1 + 1 == 2))
  print(show(1 + 1 == 3 || false && true))
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}

	// Verify precedence: 2 + 3 * 4 should be 2 + (3 * 4)
	// The body is a chain of let _ = print(...) expressions
}

func TestPhase1_12_ShortCircuit(t *testing.T) {
	prog := mustParse(t, `
bomb : () -> <> Bool =
  1 / 0 == 0

main : () -> <io> () =
  print(show(false && bomb()))
  print(show(true || bomb()))
  let x = {
    let a = false && bomb()
    "safe-and"
  }
  print(x)
  let y = {
    let b = true || bomb()
    "safe-or"
  }
  print(y)
  print(show(true && true))
  print(show(false || false))
`)
	if len(prog.TopLevels) != 2 {
		t.Fatalf("expected 2 top-levels, got %d", len(prog.TopLevels))
	}
}

// ── Expression parsing tests ──

func TestParseExprLiteral(t *testing.T) {
	expr := mustParseExpr(t, "42")
	lit, ok := expr.(ast.ExprLiteral)
	if !ok {
		t.Fatal("expected ExprLiteral")
	}
	litVal, ok := lit.Value.(ast.LitInt)
	if !ok {
		t.Fatalf("expected LitInt, got %T", lit.Value)
	}
	if litVal.Value != 42 {
		t.Errorf("expected int 42, got %v", litVal.Value)
	}
}

func TestParseExprBoolLiteral(t *testing.T) {
	expr := mustParseExpr(t, "true")
	lit, ok := expr.(ast.ExprLiteral)
	if !ok {
		t.Fatal("expected ExprLiteral")
	}
	litVal, ok := lit.Value.(ast.LitBool)
	if !ok {
		t.Fatalf("expected LitBool, got %T", lit.Value)
	}
	if !litVal.Value {
		t.Errorf("expected bool true")
	}
}

func TestParseExprStringLiteral(t *testing.T) {
	expr := mustParseExpr(t, `"hello"`)
	lit, ok := expr.(ast.ExprLiteral)
	if !ok {
		t.Fatal("expected ExprLiteral")
	}
	litVal, ok := lit.Value.(ast.LitStr)
	if !ok {
		t.Fatalf("expected LitStr, got %T", lit.Value)
	}
	if litVal.Value != "hello" {
		t.Errorf("expected str 'hello', got %q", litVal.Value)
	}
}

func TestParseExprInfix(t *testing.T) {
	expr := mustParseExpr(t, "2 + 3 * 4")
	// Should be: (+ 2 (* 3 4))
	add, ok := expr.(ast.ExprInfix)
	if !ok || add.Op != "+" {
		t.Fatalf("expected + infix, got %T", expr)
	}
	mul, ok := add.Right.(ast.ExprInfix)
	if !ok || mul.Op != "*" {
		t.Fatalf("expected * on right side, got %T", add.Right)
	}
}

func TestParseExprUnary(t *testing.T) {
	expr := mustParseExpr(t, "-5")
	un, ok := expr.(ast.ExprUnary)
	if !ok || un.Op != "-" {
		t.Fatalf("expected unary -, got %T", expr)
	}
}

func TestParseExprFunctionCall(t *testing.T) {
	expr := mustParseExpr(t, "f(1, 2)")
	app, ok := expr.(ast.ExprApply)
	if !ok {
		t.Fatalf("expected ExprApply, got %T", expr)
	}
	if len(app.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(app.Args))
	}
}

func TestParseExprLetIn(t *testing.T) {
	expr := mustParseExpr(t, "let x = 5 in x + 1")
	let, ok := expr.(ast.ExprLet)
	if !ok {
		t.Fatalf("expected ExprLet, got %T", expr)
	}
	if let.Name != "x" {
		t.Errorf("expected name 'x', got %q", let.Name)
	}
	if let.Body == nil {
		t.Fatal("expected let body")
	}
}

func TestParseExprIfThenElse(t *testing.T) {
	expr := mustParseExpr(t, "if true then 1 else 0")
	ifExpr, ok := expr.(ast.ExprIf)
	if !ok {
		t.Fatalf("expected ExprIf, got %T", expr)
	}
	_ = ifExpr
}

func TestParseExprLambda(t *testing.T) {
	expr := mustParseExpr(t, "fn(x) => x + 1")
	lam, ok := expr.(ast.ExprLambda)
	if !ok {
		t.Fatalf("expected ExprLambda, got %T", expr)
	}
	if len(lam.Params) != 1 || lam.Params[0].Name != "x" {
		t.Errorf("expected param 'x'")
	}
}

func TestParseExprList(t *testing.T) {
	expr := mustParseExpr(t, "[1, 2, 3]")
	list, ok := expr.(ast.ExprList)
	if !ok {
		t.Fatalf("expected ExprList, got %T", expr)
	}
	if len(list.Elements) != 3 {
		t.Errorf("expected 3 elements, got %d", len(list.Elements))
	}
}

func TestParseExprTuple(t *testing.T) {
	expr := mustParseExpr(t, "(1, 2)")
	tup, ok := expr.(ast.ExprTuple)
	if !ok {
		t.Fatalf("expected ExprTuple, got %T", expr)
	}
	if len(tup.Elements) != 2 {
		t.Errorf("expected 2 elements, got %d", len(tup.Elements))
	}
}

func TestParseExprUnit(t *testing.T) {
	expr := mustParseExpr(t, "()")
	lit, ok := expr.(ast.ExprLiteral)
	if !ok {
		t.Fatalf("expected ExprLiteral for unit, got %T", expr)
	}
	_, ok = lit.Value.(ast.LitUnit)
	if !ok {
		t.Fatalf("expected LitUnit, got %T", lit.Value)
	}
}

func TestParseExprRecord(t *testing.T) {
	expr := mustParseExpr(t, "{x: 1, y: 2}")
	rec, ok := expr.(ast.ExprRecord)
	if !ok {
		t.Fatalf("expected ExprRecord, got %T", expr)
	}
	if len(rec.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(rec.Fields))
	}
}

func TestParseExprFieldAccess(t *testing.T) {
	expr := mustParseExpr(t, "r.x")
	fa, ok := expr.(ast.ExprFieldAccess)
	if !ok {
		t.Fatalf("expected ExprFieldAccess, got %T", expr)
	}
	if fa.Field != "x" {
		t.Errorf("expected field 'x', got %q", fa.Field)
	}
}

func TestParseExprFieldAccessKeyword(t *testing.T) {
	// Keywords should be valid field names after a dot
	for _, tc := range []struct{ input, field string }{
		{"rx.match", "match"},
		{"foo.type", "type"},
		{"bar.in", "in"},
		{"x.for", "for"},
		{"y.if", "if"},
	} {
		expr := mustParseExpr(t, tc.input)
		fa, ok := expr.(ast.ExprFieldAccess)
		if !ok {
			t.Fatalf("%s: expected ExprFieldAccess, got %T", tc.input, expr)
		}
		if fa.Field != tc.field {
			t.Errorf("%s: expected field %q, got %q", tc.input, tc.field, fa.Field)
		}
	}
}

func TestParseInlineMatchInExpr(t *testing.T) {
	// match should work as an operand in binary expressions
	expr := mustParseExpr(t, "4 + match x { 0 => 1 _ => 2 }")
	infix, ok := expr.(ast.ExprInfix)
	if !ok {
		t.Fatalf("expected ExprInfix, got %T", expr)
	}
	if infix.Op != "+" {
		t.Errorf("expected op '+', got %q", infix.Op)
	}
	// Left should be literal 4
	lit, ok := infix.Left.(ast.ExprLiteral)
	if !ok {
		t.Fatalf("expected left to be ExprLiteral, got %T", infix.Left)
	}
	if lit.Value.(ast.LitInt).Value != 4 {
		t.Errorf("expected left value 4, got %v", lit.Value)
	}
	// Right should be ExprMatch
	_, ok = infix.Right.(ast.ExprMatch)
	if !ok {
		t.Fatalf("expected right to be ExprMatch, got %T", infix.Right)
	}
}

func TestParseInlineIfInExpr(t *testing.T) {
	expr := mustParseExpr(t, `"a" ++ if b then "yes" else "no"`)
	infix, ok := expr.(ast.ExprInfix)
	if !ok {
		t.Fatalf("expected ExprInfix, got %T", expr)
	}
	if infix.Op != "++" {
		t.Errorf("expected op '++', got %q", infix.Op)
	}
	_, ok = infix.Right.(ast.ExprIf)
	if !ok {
		t.Fatalf("expected right to be ExprIf, got %T", infix.Right)
	}
}

func TestParseExprPipeline(t *testing.T) {
	expr := mustParseExpr(t, "x |> f |> g")
	pipe, ok := expr.(ast.ExprPipeline)
	if !ok {
		t.Fatalf("expected ExprPipeline, got %T", expr)
	}
	// Should be left-assoc: (x |> f) |> g
	_, ok = pipe.Left.(ast.ExprPipeline)
	if !ok {
		t.Fatal("expected nested pipeline on left")
	}
}

func TestParseExprMatch(t *testing.T) {
	expr := mustParseExpr(t, "match x { 0 => 1 _ => 0 }")
	m, ok := expr.(ast.ExprMatch)
	if !ok {
		t.Fatalf("expected ExprMatch, got %T", expr)
	}
	if len(m.Arms) != 2 {
		t.Errorf("expected 2 arms, got %d", len(m.Arms))
	}
}

func TestParseExprBlock(t *testing.T) {
	expr := mustParseExpr(t, `{ let x = 1 x + 2 }`)
	blk, ok := expr.(ast.ExprBlock)
	if !ok {
		t.Fatalf("expected ExprBlock, got %T", expr)
	}
	if len(blk.Exprs) != 2 {
		t.Errorf("expected 2 exprs, got %d", len(blk.Exprs))
	}
}

func TestParseExprConcat(t *testing.T) {
	expr := mustParseExpr(t, `"a" ++ "b" ++ "c"`)
	// ++ is right-assoc: "a" ++ ("b" ++ "c")
	concat, ok := expr.(ast.ExprInfix)
	if !ok || concat.Op != "++" {
		t.Fatalf("expected ++ infix, got %T", expr)
	}
	rightConcat, ok := concat.Right.(ast.ExprInfix)
	if !ok || rightConcat.Op != "++" {
		t.Fatal("expected right-assoc ++")
	}
}

func TestParseExprNot(t *testing.T) {
	expr := mustParseExpr(t, "!true")
	un, ok := expr.(ast.ExprUnary)
	if !ok || un.Op != "!" {
		t.Fatalf("expected unary !, got %T", expr)
	}
}

func TestParseExprParens(t *testing.T) {
	expr := mustParseExpr(t, "(2 + 3) * 4")
	mul, ok := expr.(ast.ExprInfix)
	if !ok || mul.Op != "*" {
		t.Fatalf("expected * at top, got %T", expr)
	}
	add, ok := mul.Left.(ast.ExprInfix)
	if !ok || add.Op != "+" {
		t.Fatal("expected + on left")
	}
}

func TestParseExprNestedIf(t *testing.T) {
	expr := mustParseExpr(t, "if true then 1 else if false then 2 else 3")
	ifExpr, ok := expr.(ast.ExprIf)
	if !ok {
		t.Fatalf("expected ExprIf, got %T", expr)
	}
	nestedIf, ok := ifExpr.Else.(ast.ExprIf)
	if !ok {
		t.Fatalf("expected nested ExprIf in else, got %T", ifExpr.Else)
	}
	_ = nestedIf
}

func TestParseExprComparisonNonAssoc(t *testing.T) {
	tokens, _ := lexer.Lex("1 < 2 < 3")
	_, err := parser.ParseExpression(tokens)
	if err == nil {
		t.Fatal("expected parse error for chained comparison")
	}
}

// ── Type declarations ──

func TestParseTypeDecl(t *testing.T) {
	prog := mustParse(t, `
type Option<A> = Some(A) | None
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
	td, ok := prog.TopLevels[0].(ast.TopTypeDecl)
	if !ok {
		t.Fatalf("expected TopTypeDecl, got %T", prog.TopLevels[0])
	}
	if td.Name != "Option" {
		t.Errorf("expected 'Option', got %q", td.Name)
	}
	if len(td.TypeParams) != 1 || td.TypeParams[0] != "A" {
		t.Errorf("expected type param A")
	}
	if len(td.Variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(td.Variants))
	}
	if td.Variants[0].Name != "Some" || len(td.Variants[0].Fields) != 1 {
		t.Errorf("expected Some(A)")
	}
	if td.Variants[1].Name != "None" || len(td.Variants[1].Fields) != 0 {
		t.Errorf("expected None")
	}
}

// ── Effect declarations ──

func TestParseEffectDecl(t *testing.T) {
	prog := mustParse(t, `
effect State {
  get : () -> Int,
  put : Int -> ()
}
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
	ed, ok := prog.TopLevels[0].(ast.TopEffectDecl)
	if !ok {
		t.Fatalf("expected TopEffectDecl, got %T", prog.TopLevels[0])
	}
	if ed.Name != "State" {
		t.Errorf("expected 'State', got %q", ed.Name)
	}
	if len(ed.Ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(ed.Ops))
	}
}

// ── Pub modifier ──

func TestParsePubDefinition(t *testing.T) {
	prog := mustParse(t, `
pub add : (a: Int, b: Int) -> <> Int =
  a + b
`)
	if len(prog.TopLevels) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(prog.TopLevels))
	}
	def, ok := prog.TopLevels[0].(ast.TopDefinition)
	if !ok {
		t.Fatalf("expected TopDefinition, got %T", prog.TopLevels[0])
	}
	if !def.Pub {
		t.Error("expected pub=true")
	}
}
