package eval

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/token"
)

// ── AST builder helpers ──

var noLoc = token.Loc{}

func lit(v ast.Literal) ast.Expr       { return ast.ExprLiteral{Value: v, Loc: noLoc} }
func intLit(n int64) ast.Expr          { return lit(ast.LitInt{Value: n}) }
func ratLit(n float64) ast.Expr        { return lit(ast.LitRat{Value: n}) }
func boolLit(b bool) ast.Expr          { return lit(ast.LitBool{Value: b}) }
func strLit(s string) ast.Expr         { return lit(ast.LitStr{Value: s}) }
func unitLit() ast.Expr                { return lit(ast.LitUnit{}) }
func varExpr(name string) ast.Expr     { return ast.ExprVar{Name: name, Loc: noLoc} }
func apply(fn ast.Expr, args ...ast.Expr) ast.Expr {
	return ast.ExprApply{Fn: fn, Args: args, Loc: noLoc}
}
func call(name string, args ...ast.Expr) ast.Expr {
	return apply(varExpr(name), args...)
}
func letExpr(name string, value, body ast.Expr) ast.Expr {
	return ast.ExprLet{Name: name, Value: value, Body: body, Loc: noLoc}
}
func ifExpr(cond, then_, else_ ast.Expr) ast.Expr {
	return ast.ExprIf{Cond: cond, Then: then_, Else: else_, Loc: noLoc}
}
func lambda(params []string, body ast.Expr) ast.Expr {
	ps := make([]ast.Param, len(params))
	for i, p := range params {
		ps[i] = ast.Param{Name: p}
	}
	return ast.ExprLambda{Params: ps, Body: body, Loc: noLoc}
}
func matchExpr(subject ast.Expr, arms ...ast.MatchArm) ast.Expr {
	return ast.ExprMatch{Subject: subject, Arms: arms, Loc: noLoc}
}
func matchArm(pat ast.Pattern, body ast.Expr) ast.MatchArm {
	return ast.MatchArm{Pattern: pat, Body: body}
}
func pVar(name string) ast.Pattern     { return ast.PatVar{Name: name, Loc: noLoc} }
func pWild() ast.Pattern               { return ast.PatWildcard{Loc: noLoc} }
func pInt(n int64) ast.Pattern         { return ast.PatLiteral{Value: ast.LitInt{Value: n}, Loc: noLoc} }
func pBool(b bool) ast.Pattern         { return ast.PatLiteral{Value: ast.LitBool{Value: b}, Loc: noLoc} }
func pVariant(name string, args ...ast.Pattern) ast.Pattern {
	return ast.PatVariant{Name: name, Args: args, Loc: noLoc}
}
func pTuple(elems ...ast.Pattern) ast.Pattern {
	return ast.PatTuple{Elements: elems, Loc: noLoc}
}
func pRecord(fields []ast.PatField, rest string) ast.Pattern {
	return ast.PatRecord{Fields: fields, Rest: rest, Loc: noLoc}
}
func listExpr(elems ...ast.Expr) ast.Expr {
	return ast.ExprList{Elements: elems, Loc: noLoc}
}
func tupleExpr(elems ...ast.Expr) ast.Expr {
	return ast.ExprTuple{Elements: elems, Loc: noLoc}
}
func recordExpr(fields map[string]ast.Expr) ast.Expr {
	rf := make([]ast.RecordField, 0, len(fields))
	for k, v := range fields {
		rf = append(rf, ast.RecordField{Name: k, Value: v})
	}
	return ast.ExprRecord{Fields: rf, Loc: noLoc}
}
func recordExprOrdered(names []string, values []ast.Expr) ast.Expr {
	rf := make([]ast.RecordField, len(names))
	for i := range names {
		rf[i] = ast.RecordField{Name: names[i], Value: values[i]}
	}
	return ast.ExprRecord{Fields: rf, Loc: noLoc}
}
func fieldAccess(obj ast.Expr, field string) ast.Expr {
	return ast.ExprFieldAccess{Object: obj, Field: field, Loc: noLoc}
}
func recordUpdate(base ast.Expr, fields map[string]ast.Expr) ast.Expr {
	rf := make([]ast.RecordUpdateField, 0, len(fields))
	for k, v := range fields {
		rf = append(rf, ast.RecordUpdateField{Name: k, Value: v})
	}
	return ast.ExprRecordUpdate{Base: base, Fields: rf, Loc: noLoc}
}

// Sugar nodes (to test desugaring)
func pipeline(left, right ast.Expr) ast.Expr {
	return ast.ExprPipeline{Left: left, Right: right, Loc: noLoc}
}
func infix(op string, left, right ast.Expr) ast.Expr {
	return ast.ExprInfix{Op: op, Left: left, Right: right, Loc: noLoc}
}
func unary(op string, operand ast.Expr) ast.Expr {
	return ast.ExprUnary{Op: op, Operand: operand, Loc: noLoc}
}
func doBlock(steps ...ast.DoStep) ast.Expr {
	return ast.ExprDo{Steps: steps, Loc: noLoc}
}
func doStep(bind string, expr ast.Expr) ast.DoStep {
	return ast.DoStep{Bind: bind, Expr: expr}
}
func forExpr(bind ast.Pattern, collection, guard ast.Expr, fold *ast.FoldClause, body ast.Expr) ast.Expr {
	return ast.ExprFor{Bind: bind, Collection: collection, Guard: guard, Fold: fold, Body: body, Loc: noLoc}
}
func rangeExpr(start, end ast.Expr, inclusive bool) ast.Expr {
	return ast.ExprRange{Start: start, End: end, Inclusive: inclusive, Loc: noLoc}
}
func handleExpr(expr ast.Expr, arms ...ast.HandlerArm) ast.Expr {
	return ast.ExprHandle{Expr: expr, Arms: arms, Loc: noLoc}
}
func handlerArm(name string, params []string, resumeName string, body ast.Expr) ast.HandlerArm {
	ps := make([]ast.Param, len(params))
	for i, p := range params {
		ps[i] = ast.Param{Name: p}
	}
	return ast.HandlerArm{Name: name, Params: ps, ResumeName: resumeName, Body: body}
}

func performExpr(expr ast.Expr) ast.Expr {
	return ast.ExprPerform{Expr: expr, Loc: noLoc}
}

func letPatternExpr(pat ast.Pattern, value, body ast.Expr) ast.Expr {
	return ast.ExprLetPattern{Pattern: pat, Value: value, Body: body, Loc: noLoc}
}

// ── Program builder ──

func defn(name string, paramNames []string, body ast.Expr) ast.TopLevel {
	params := make([]ast.TypeSigParam, len(paramNames))
	for i, p := range paramNames {
		params[i] = ast.TypeSigParam{Name: p}
	}
	return ast.TopDefinition{
		Name: name,
		Sig:  ast.TypeSig{Params: params},
		Body: body,
		Loc:  noLoc,
	}
}

func typeDecl(name string, variants []ast.Variant, deriving []string) ast.TopLevel {
	return ast.TopTypeDecl{Name: name, Variants: variants, Deriving: deriving, Loc: noLoc}
}

func effectDecl(name string, ops []string) ast.TopLevel {
	opSigs := make([]ast.OpSig, len(ops))
	for i, op := range ops {
		opSigs[i] = ast.OpSig{Name: op}
	}
	return ast.TopEffectDecl{Name: name, Ops: opSigs, Loc: noLoc}
}

// ── Test helper: capture stdout ──

func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// ── Helper: desugar + eval expression ──

func evalExpr(t *testing.T, expr ast.Expr) Value {
	t.Helper()
	desugared := desugar.Desugar(expr)
	result, err := RunExpr(desugared)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	return result
}

// ── Helper: desugar + run program, return captured output ──

func runProgram(t *testing.T, topLevels ...ast.TopLevel) string {
	t.Helper()
	// Desugar all definition bodies
	desugared := make([]ast.TopLevel, len(topLevels))
	for i, tl := range topLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			desugared[i] = ast.TopDefinition{
				Name:        d.Name,
				Sig:         d.Sig,
				Body:        desugar.Desugar(d.Body),
				Pub:         d.Pub,
				Constraints: d.Constraints,
				Loc:         d.Loc,
			}
		default:
			desugared[i] = tl
		}
	}
	program := &ast.Program{TopLevels: desugared}
	output := captureOutput(func() {
		_, err := Run(program)
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
	})
	return output
}

func expectOutput(t *testing.T, got, want string) {
	t.Helper()
	got = strings.TrimRight(got, "\n")
	want = strings.TrimRight(want, "\n")
	if got != want {
		t.Errorf("output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// ════════════════════════════════════════════════════════════════════
// Phase 1: Basic Language Features
// ════════════════════════════════════════════════════════════════════

func TestPhase1_Arithmetic(t *testing.T) {
	// 10 + 5, 4 - 7, 6 * 7, 10 / 3, 17 % 5
	body := letExpr("_", call("print", call("show", infix("+", intLit(10), intLit(5)))),
		letExpr("_", call("print", call("show", infix("-", intLit(4), intLit(7)))),
			letExpr("_", call("print", call("show", infix("*", intLit(6), intLit(7)))),
				letExpr("_", call("print", call("show", infix("/", intLit(10), intLit(3)))),
					call("print", call("show", infix("%", intLit(17), intLit(5))))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "15\n-3\n42\n3\n2")
}

func TestPhase1_LetBinding(t *testing.T) {
	// let x = 10; let y = 20; print(show(x + y))
	body := letExpr("x", intLit(10),
		letExpr("y", intLit(20),
			call("print", call("show", infix("+", varExpr("x"), varExpr("y"))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "30")
}

func TestPhase1_NestedLet(t *testing.T) {
	// let x = 5; let y = let a = 3 in a + x; print(show(y))
	body := letExpr("x", intLit(5),
		letExpr("y", letExpr("a", intLit(3), infix("+", varExpr("a"), varExpr("x"))),
			call("print", call("show", varExpr("y")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "8")
}

func TestPhase1_IfThenElse(t *testing.T) {
	body := letExpr("_", call("print", call("show", ifExpr(boolLit(true), intLit(1), intLit(2)))),
		call("print", call("show", ifExpr(boolLit(false), intLit(1), intLit(2)))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "1\n2")
}

func TestPhase1_Factorial(t *testing.T) {
	// factorial(n) = if n == 0 then 1 else n * factorial(n - 1)
	factBody := ifExpr(
		infix("==", varExpr("n"), intLit(0)),
		intLit(1),
		infix("*", varExpr("n"), call("factorial", infix("-", varExpr("n"), intLit(1)))))

	mainBody := letExpr("_", call("print", call("show", call("factorial", intLit(0)))),
		letExpr("_", call("print", call("show", call("factorial", intLit(1)))),
			letExpr("_", call("print", call("show", call("factorial", intLit(5)))),
				call("print", call("show", call("factorial", intLit(10)))))))

	output := runProgram(t,
		defn("factorial", []string{"n"}, factBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "1\n1\n120\n3628800")
}

func TestPhase1_Fibonacci(t *testing.T) {
	fibBody := ifExpr(
		infix("<=", varExpr("n"), intLit(1)),
		varExpr("n"),
		infix("+",
			call("fib", infix("-", varExpr("n"), intLit(1))),
			call("fib", infix("-", varExpr("n"), intLit(2)))))

	mainBody := letExpr("_", call("print", call("show", call("fib", intLit(0)))),
		letExpr("_", call("print", call("show", call("fib", intLit(1)))),
			letExpr("_", call("print", call("show", call("fib", intLit(5)))),
				call("print", call("show", call("fib", intLit(10)))))))

	output := runProgram(t,
		defn("fib", []string{"n"}, fibBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "0\n1\n5\n55")
}

func TestPhase1_MutualRecursion(t *testing.T) {
	isEvenBody := ifExpr(
		infix("==", varExpr("n"), intLit(0)),
		boolLit(true),
		call("is-odd", infix("-", varExpr("n"), intLit(1))))

	isOddBody := ifExpr(
		infix("==", varExpr("n"), intLit(0)),
		boolLit(false),
		call("is-even", infix("-", varExpr("n"), intLit(1))))

	mainBody := letExpr("_", call("print", call("show", call("is-even", intLit(4)))),
		call("print", call("show", call("is-odd", intLit(4)))))

	output := runProgram(t,
		defn("is-even", []string{"n"}, isEvenBody),
		defn("is-odd", []string{"n"}, isOddBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "true\nfalse")
}

func TestPhase1_BooleanLogic(t *testing.T) {
	body := letExpr("_", call("print", call("show", infix("&&", boolLit(true), boolLit(false)))),
		letExpr("_", call("print", call("show", infix("||", boolLit(true), boolLit(false)))),
			call("print", call("show", unary("!", boolLit(true))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "false\ntrue\nfalse")
}

func TestPhase1_StringConcat(t *testing.T) {
	body := call("print", infix("++", strLit("hello"), strLit(" world")))
	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "hello world")
}

func TestPhase1_Comparison(t *testing.T) {
	body := letExpr("_", call("print", call("show", infix("<", intLit(3), intLit(5)))),
		letExpr("_", call("print", call("show", infix(">", intLit(3), intLit(5)))),
			letExpr("_", call("print", call("show", infix("==", intLit(5), intLit(5)))),
				call("print", call("show", infix("!=", intLit(3), intLit(5)))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "true\nfalse\ntrue\ntrue")
}

func TestPhase1_ShortCircuit(t *testing.T) {
	// false && (1/0 == 0) should not evaluate the division
	body := letExpr("_", call("print", call("show", infix("&&", boolLit(false), infix("==", infix("/", intLit(1), intLit(0)), intLit(0))))),
		// true || (1/0 == 0) should not evaluate the division
		call("print", call("show", infix("||", boolLit(true), infix("==", infix("/", intLit(1), intLit(0)), intLit(0))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "false\ntrue")
}

// ════════════════════════════════════════════════════════════════════
// Phase 2: Functional Programming & Collections
// ════════════════════════════════════════════════════════════════════

func TestPhase2_Pipeline(t *testing.T) {
	// 5 |> show |> print  →  (5 |> show) |> print
	body := letExpr("_", pipeline(pipeline(intLit(5), varExpr("show")), varExpr("print")),
		// 6 |> mul(7) → mul(6, 7) = 42
		letExpr("result", pipeline(intLit(6), call("mul", intLit(7))),
			call("print", call("show", varExpr("result")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "5\n42")
}

func TestPhase2_Lambda(t *testing.T) {
	// let double = fn(x) => x * 2; print(show(double(5)))
	body := letExpr("double", lambda([]string{"x"}, infix("*", varExpr("x"), intLit(2))),
		call("print", call("show", apply(varExpr("double"), intLit(5)))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "10")
}

func TestPhase2_ListOps(t *testing.T) {
	body := letExpr("xs", listExpr(intLit(1), intLit(2), intLit(3)),
		letExpr("_", call("print", call("show", call("len", varExpr("xs")))),
			letExpr("_", call("print", call("show", call("head", varExpr("xs")))),
				letExpr("_", call("print", call("show", call("tail", varExpr("xs")))),
					letExpr("_", call("print", call("show", call("rev", varExpr("xs")))),
						call("print", call("show",
							call("map", varExpr("xs"), lambda([]string{"x"}, infix("*", varExpr("x"), intLit(2)))))))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "3\n1\n[2, 3]\n[3, 2, 1]\n[2, 4, 6]")
}

func TestPhase2_Tuples(t *testing.T) {
	body := letExpr("t", tupleExpr(intLit(1), strLit("hello")),
		letExpr("_", call("print", call("show", varExpr("t"))),
			letExpr("_", call("print", call("show", call("fst", varExpr("t")))),
				call("print", call("snd", varExpr("t"))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "(1, hello)\n1\nhello")
}

func TestPhase2_MatchVariants(t *testing.T) {
	// type Shape = Circle(Int) | Rect(Int, Int)
	shapeDecl := typeDecl("Shape", []ast.Variant{
		{Name: "Circle", Fields: []ast.TypeExpr{ast.TypeName{Name: "Int"}}},
		{Name: "Rect", Fields: []ast.TypeExpr{ast.TypeName{Name: "Int"}, ast.TypeName{Name: "Int"}}},
	}, nil)

	describeBody := matchExpr(varExpr("s"),
		matchArm(pVariant("Circle", pVar("r")),
			call("print", infix("++", strLit("circle with radius "), call("show", varExpr("r"))))),
		matchArm(pVariant("Rect", pVar("w"), pVar("h")),
			call("print", infix("++", strLit("area: "), call("show", infix("*", varExpr("w"), varExpr("h")))))))

	mainBody := letExpr("_", call("describe", call("Circle", intLit(5))),
		call("describe", call("Rect", intLit(3), intLit(4))))

	output := runProgram(t,
		shapeDecl,
		defn("describe", []string{"s"}, describeBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "circle with radius 5\narea: 12")
}

func TestPhase2_MatchPatterns(t *testing.T) {
	// match x { 0 => "zero", _ => "other" }
	body := letExpr("_",
		call("print", matchExpr(intLit(0),
			matchArm(pInt(0), strLit("zero")),
			matchArm(pWild(), strLit("other")))),
		call("print", matchExpr(intLit(5),
			matchArm(pInt(0), strLit("zero")),
			matchArm(pWild(), strLit("other")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "zero\nother")
}

func TestPhase2_DoBlock(t *testing.T) {
	// do { x <- 10; y <- 20; x + y }
	expr := doBlock(
		doStep("x", intLit(10)),
		doStep("y", intLit(20)),
		doStep("", infix("+", varExpr("x"), varExpr("y"))))

	result := evalExpr(t, expr)
	if v, ok := result.(ValInt); !ok || v.Val != 30 {
		t.Errorf("expected 30, got %v", result)
	}
}

func TestPhase2_DottedBuiltins(t *testing.T) {
	body := call("print", call("str.cat", strLit("hello"), strLit(" world")))
	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "hello world")
}

func TestPhase2_ForIn(t *testing.T) {
	xs := listExpr(intLit(1), intLit(2), intLit(3), intLit(4), intLit(5))

	// map form: for x in xs do x * 2
	mapForm := forExpr(pVar("x"), xs, nil, nil, infix("*", varExpr("x"), intLit(2)))

	// filter+map: for x in xs if x % 2 == 0 do x * x
	filterMapForm := forExpr(pVar("x"), xs,
		infix("==", infix("%", varExpr("x"), intLit(2)), intLit(0)),
		nil,
		infix("*", varExpr("x"), varExpr("x")))

	// fold: for x in xs fold total = 0 do total + x
	foldForm := forExpr(pVar("x"), xs, nil,
		&ast.FoldClause{Acc: "total", Init: intLit(0)},
		infix("+", varExpr("total"), varExpr("x")))

	// filter+fold: for x in xs if x > 2 fold total = 0 do total + x
	filterFoldForm := forExpr(pVar("x"), xs,
		infix(">", varExpr("x"), intLit(2)),
		&ast.FoldClause{Acc: "total", Init: intLit(0)},
		infix("+", varExpr("total"), varExpr("x")))

	body := letExpr("xs", xs,
		letExpr("_", call("print", call("show", mapForm)),
			letExpr("_", call("print", call("show", filterMapForm)),
				letExpr("_", call("print", call("show", foldForm)),
					call("print", call("show", filterFoldForm))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[2, 4, 6, 8, 10]\n[4, 16]\n15\n12")
}

func TestPhase2_RangeLiterals(t *testing.T) {
	// 1..5 → [1, 2, 3, 4]
	// 1..=5 → [1, 2, 3, 4, 5]
	body := letExpr("_", call("print", call("show", rangeExpr(intLit(1), intLit(5), false))),
		letExpr("_", call("print", call("show", rangeExpr(intLit(1), intLit(5), true))),
			// empty ranges
			letExpr("_", call("print", call("show", rangeExpr(intLit(5), intLit(3), false))),
				call("print", call("show", rangeExpr(intLit(5), intLit(4), true))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[1, 2, 3, 4]\n[1, 2, 3, 4, 5]\n[]\n[]")
}

func TestPhase2_HigherOrder(t *testing.T) {
	// let apply_twice = fn(f, x) => f(f(x))
	// let inc = fn(x) => x + 1
	// print(show(apply_twice(inc, 5))) → 7
	body := letExpr("apply_twice",
		lambda([]string{"f", "x"}, apply(varExpr("f"), apply(varExpr("f"), varExpr("x")))),
		letExpr("inc", lambda([]string{"x"}, infix("+", varExpr("x"), intLit(1))),
			call("print", call("show", apply(varExpr("apply_twice"), varExpr("inc"), intLit(5))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "7")
}

func TestPhase2_ListRecursive(t *testing.T) {
	// sum(xs) = if len(xs) == 0 then 0 else head(xs) + sum(tail(xs))
	sumBody := ifExpr(
		infix("==", call("len", varExpr("xs")), intLit(0)),
		intLit(0),
		infix("+", call("head", varExpr("xs")), call("sum", call("tail", varExpr("xs")))))

	mainBody := call("print", call("show", call("sum", listExpr(intLit(1), intLit(2), intLit(3), intLit(4), intLit(5)))))

	output := runProgram(t,
		defn("sum", []string{"xs"}, sumBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "15")
}

func TestPhase2_FlatMap(t *testing.T) {
	// flat-map([1, 2, 3], fn(x) => [x, x * 10])
	body := call("print", call("show",
		call("flat-map", listExpr(intLit(1), intLit(2), intLit(3)),
			lambda([]string{"x"}, listExpr(varExpr("x"), infix("*", varExpr("x"), intLit(10)))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[1, 10, 2, 20, 3, 30]")
}

func TestPhase2_StringOps(t *testing.T) {
	body := letExpr("_", call("print", call("show", call("split", strLit("a,b,c"), strLit(",")))),
		letExpr("_", call("print", call("join", listExpr(strLit("x"), strLit("y"), strLit("z")), strLit("-"))),
			call("print", call("trim", strLit("  hello  ")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[a, b, c]\nx-y-z\nhello")
}

func TestPhase2_PipelineChain(t *testing.T) {
	// [1, 2, 3] |> map(fn(x) => x * 2) |> show |> print
	// Left-associative: ((xs |> map(f)) |> show) |> print
	body := pipeline(
		pipeline(
			pipeline(
				listExpr(intLit(1), intLit(2), intLit(3)),
				call("map", lambda([]string{"x"}, infix("*", varExpr("x"), intLit(2))))),
			varExpr("show")),
		varExpr("print"))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[2, 4, 6]")
}

func TestPhase2_FnTypeAnnotations(t *testing.T) {
	// Just test that annotated params work — type annotations are ignored at runtime
	body := call("print", call("show",
		apply(lambda([]string{"x"}, infix("+", varExpr("x"), intLit(1))), intLit(41))))
	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "42")
}

// ════════════════════════════════════════════════════════════════════
// Phase 3: Effect System
// ════════════════════════════════════════════════════════════════════

func TestPhase3_EffectDecl(t *testing.T) {
	// effect ask { get-value : () -> Int }
	askEffect := effectDecl("ask", []string{"get-value"})

	// compute() = let x = get-value(); x + 42
	computeBody := letExpr("x", call("get-value"), infix("+", varExpr("x"), intLit(42)))

	// handle compute() { return x -> x, get-value _ resume k -> k(100) }
	mainBody := letExpr("result",
		handleExpr(call("compute"),
			handlerArm("return", []string{"x"}, "", varExpr("x")),
			handlerArm("get-value", []string{"_"}, "k", apply(varExpr("k"), intLit(100)))),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t,
		askEffect,
		defn("compute", nil, computeBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "142")
}

func TestPhase3_HandleAbort(t *testing.T) {
	// effect fail { abort : (Str) -> () }
	failEffect := effectDecl("fail", []string{"abort"})

	// risky() = let _ = abort("oops"); 42
	riskyBody := letExpr("_", call("abort", strLit("oops")), intLit(42))

	// handle risky() { abort msg _ _ -> print("caught: " ++ msg) }
	mainBody := handleExpr(call("risky"),
		handlerArm("abort", []string{"msg"}, "", call("print", infix("++", strLit("caught: "), varExpr("msg")))))

	output := runProgram(t,
		failEffect,
		defn("risky", nil, riskyBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "caught: oops")
}

func TestPhase3_HandleMultipleResumes(t *testing.T) {
	// Test: multiple resumes in sequence
	askEffect := effectDecl("ask", []string{"get-value"})

	// compute() = let x = get-value(); let y = get-value(); x + y
	computeBody := letExpr("x", call("get-value"),
		letExpr("y", call("get-value"),
			infix("+", varExpr("x"), varExpr("y"))))

	mainBody := letExpr("result",
		handleExpr(call("compute"),
			handlerArm("return", []string{"x"}, "", varExpr("x")),
			handlerArm("get-value", []string{"_"}, "k", apply(varExpr("k"), intLit(10)))),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t,
		askEffect,
		defn("compute", nil, computeBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "20")
}

func TestPhase3_HandleNested(t *testing.T) {
	// Two effects, nested handlers
	askEffect := effectDecl("ask", []string{"get-value"})
	logEffect := effectDecl("log", []string{"log-msg"})

	// work() = let x = get-value(); let _ = log-msg("got it"); x * 2
	workBody := letExpr("x", call("get-value"),
		letExpr("_", call("log-msg", strLit("got it")),
			infix("*", varExpr("x"), intLit(2))))

	mainBody := letExpr("result",
		handleExpr(
			handleExpr(call("work"),
				handlerArm("get-value", []string{"_"}, "k", apply(varExpr("k"), intLit(5)))),
			handlerArm("log-msg", []string{"msg"}, "k",
				letExpr("_", call("print", infix("++", strLit("LOG: "), varExpr("msg"))),
					apply(varExpr("k"), unitLit())))),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t,
		askEffect, logEffect,
		defn("work", nil, workBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "LOG: got it\n10")
}

func TestPhase3_SafeDiv(t *testing.T) {
	// Safe division using built-in exn effect
	// handle (10 / 0) { raise msg _ _ -> print("error: " ++ msg) }
	mainBody := handleExpr(
		letExpr("result", infix("/", intLit(10), intLit(0)),
			call("print", call("show", varExpr("result")))),
		handlerArm("raise", []string{"msg"}, "",
			call("print", infix("++", strLit("error: "), varExpr("msg")))))

	output := runProgram(t, defn("main", nil, mainBody))
	expectOutput(t, output, "error: division by zero")
}

// ════════════════════════════════════════════════════════════════════
// Phase 5: Records & Modules
// ════════════════════════════════════════════════════════════════════

func TestPhase5_RecordCreate(t *testing.T) {
	// let person = {name: "Ada", age: 36}
	body := letExpr("person", recordExprOrdered(
		[]string{"name", "age"},
		[]ast.Expr{strLit("Ada"), intLit(36)}),
		letExpr("_", call("print", fieldAccess(varExpr("person"), "name")),
			letExpr("_", call("print", call("show", fieldAccess(varExpr("person"), "age"))),
				call("print", call("show", varExpr("person"))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "Ada\n36\n{name: Ada, age: 36}")
}

func TestPhase5_RecordUpdate(t *testing.T) {
	body := letExpr("p", recordExprOrdered(
		[]string{"name", "age"},
		[]ast.Expr{strLit("Ada"), intLit(36)}),
		letExpr("p2", recordUpdate(varExpr("p"), map[string]ast.Expr{"age": intLit(37)}),
			call("print", call("show", varExpr("p2")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "{name: Ada, age: 37}")
}

func TestPhase5_RecordPatternClosed(t *testing.T) {
	body := letExpr("p", recordExprOrdered(
		[]string{"x", "y"},
		[]ast.Expr{intLit(1), intLit(2)}),
		matchExpr(varExpr("p"),
			matchArm(
				pRecord([]ast.PatField{{Name: "x"}, {Name: "y"}}, ""),
				call("print", call("show", infix("+", varExpr("x"), varExpr("y")))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "3")
}

func TestPhase5_RecordPatternOpen(t *testing.T) {
	body := letExpr("p", recordExprOrdered(
		[]string{"x", "y", "z"},
		[]ast.Expr{intLit(1), intLit(2), intLit(3)}),
		matchExpr(varExpr("p"),
			matchArm(
				pRecord([]ast.PatField{{Name: "x"}}, "rest"),
				letExpr("_", call("print", call("show", varExpr("x"))),
					call("print", call("show", varExpr("rest")))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "1\n{y: 2, z: 3}")
}

func TestPhase5_RecordSpread(t *testing.T) {
	spreadRec := ast.ExprRecord{
		Fields: []ast.RecordField{{Name: "z", Value: intLit(3)}},
		Spread: recordExprOrdered([]string{"x", "y"}, []ast.Expr{intLit(1), intLit(2)}),
		Loc:    noLoc,
	}
	body := call("print", call("show", spreadRec))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "{x: 1, y: 2, z: 3}")
}

func TestPhase5_LetTupleDestructure(t *testing.T) {
	// let (a, b) = (1, "hello")
	body := letPatternExpr(
		pTuple(pVar("a"), pVar("b")),
		tupleExpr(intLit(1), strLit("hello")),
		letExpr("_", call("print", call("show", varExpr("a"))),
			call("print", varExpr("b"))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "1\nhello")
}

func TestPhase5_LetRecordDestructure(t *testing.T) {
	// let {x, y} = {x: 10, y: 20}
	body := letPatternExpr(
		pRecord([]ast.PatField{{Name: "x"}, {Name: "y"}}, ""),
		recordExprOrdered([]string{"x", "y"}, []ast.Expr{intLit(10), intLit(20)}),
		call("print", call("show", infix("+", varExpr("x"), varExpr("y")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "30")
}

func TestPhase5_RecordEquality(t *testing.T) {
	body := letExpr("a", recordExprOrdered([]string{"x", "y"}, []ast.Expr{intLit(1), intLit(2)}),
		letExpr("b", recordExprOrdered([]string{"x", "y"}, []ast.Expr{intLit(1), intLit(2)}),
			letExpr("c", recordExprOrdered([]string{"x", "y"}, []ast.Expr{intLit(1), intLit(3)}),
				letExpr("_", call("print", call("show", infix("==", varExpr("a"), varExpr("b")))),
					call("print", call("show", infix("==", varExpr("a"), varExpr("c"))))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "true\nfalse")
}

func TestPhase5_RecordInVariant(t *testing.T) {
	// type Result = Ok(Int) | Err(Str)
	resultDecl := typeDecl("Result", []ast.Variant{
		{Name: "Ok", Fields: []ast.TypeExpr{ast.TypeName{Name: "Int"}}},
		{Name: "Err", Fields: []ast.TypeExpr{ast.TypeName{Name: "Str"}}},
	}, nil)

	body := matchExpr(call("Ok", intLit(42)),
		matchArm(pVariant("Ok", pVar("v")), call("print", call("show", varExpr("v")))),
		matchArm(pVariant("Err", pVar("msg")), call("print", varExpr("msg"))))

	output := runProgram(t, resultDecl, defn("main", nil, body))
	expectOutput(t, output, "42")
}

// ════════════════════════════════════════════════════════════════════
// Additional coverage: edge cases and cross-phase features
// ════════════════════════════════════════════════════════════════════

func TestPhase1_NegateAndPrecedence(t *testing.T) {
	// -(3 + 4) = -7, 2 + 3 * 4 = 14 (but we test as explicit AST, no precedence issues)
	body := letExpr("_", call("print", call("show", unary("-", infix("+", intLit(3), intLit(4))))),
		call("print", call("show", infix("+", intLit(2), infix("*", intLit(3), intLit(4))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "-7\n14")
}

func TestPhase2_FilterFold(t *testing.T) {
	// filter([1, 2, 3, 4], fn(x) => x > 2) → [3, 4]
	// fold([1, 2, 3], 0, fn(a, x) => a + x) → 6
	body := letExpr("_", call("print", call("show",
		call("filter", listExpr(intLit(1), intLit(2), intLit(3), intLit(4)),
			lambda([]string{"x"}, infix(">", varExpr("x"), intLit(2)))))),
		call("print", call("show",
			call("fold", listExpr(intLit(1), intLit(2), intLit(3)),
				intLit(0),
				lambda([]string{"a", "x"}, infix("+", varExpr("a"), varExpr("x")))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[3, 4]\n6")
}

func TestPhase2_ConsAndCat(t *testing.T) {
	body := letExpr("_", call("print", call("show", call("cons", intLit(0), listExpr(intLit(1), intLit(2))))),
		call("print", call("show", call("cat", listExpr(intLit(1), intLit(2)), listExpr(intLit(3), intLit(4))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[0, 1, 2]\n[1, 2, 3, 4]")
}

func TestPhase2_Zip(t *testing.T) {
	body := call("print", call("show",
		call("zip", listExpr(intLit(1), intLit(2)), listExpr(strLit("a"), strLit("b")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[(1, a), (2, b)]")
}

func TestPhase2_TupleGet(t *testing.T) {
	body := letExpr("t", tupleExpr(intLit(10), intLit(20), intLit(30)),
		call("print", call("show", call("tuple.get", varExpr("t"), intLit(1)))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "20")
}

func TestPhase3_EffectPerformSyntax(t *testing.T) {
	// Explicit perform syntax
	askEffect := effectDecl("ask", []string{"get-value"})

	computeBody := letExpr("x", performExpr(call("get-value")), infix("+", varExpr("x"), intLit(1)))

	mainBody := letExpr("result",
		handleExpr(call("compute"),
			handlerArm("return", []string{"x"}, "", varExpr("x")),
			handlerArm("get-value", []string{"_"}, "k", apply(varExpr("k"), intLit(99)))),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t,
		askEffect,
		defn("compute", nil, computeBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "100")
}

func TestPhase5_RecordNestedAccess(t *testing.T) {
	// let p = {inner: {x: 42}}; print(show(p.inner.x))
	body := letExpr("p",
		recordExprOrdered([]string{"inner"},
			[]ast.Expr{recordExprOrdered([]string{"x"}, []ast.Expr{intLit(42)})}),
		call("print", call("show", fieldAccess(fieldAccess(varExpr("p"), "inner"), "x"))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "42")
}

func TestPhase5_RecordFunctions(t *testing.T) {
	getNameBody := fieldAccess(varExpr("r"), "name")

	mainBody := letExpr("p", recordExprOrdered(
		[]string{"name", "age"},
		[]ast.Expr{strLit("Ada"), intLit(36)}),
		call("print", call("get-name", varExpr("p"))))

	output := runProgram(t,
		defn("get-name", []string{"r"}, getNameBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "Ada")
}

func TestPhase2_RangeWithFold(t *testing.T) {
	// sum 1..=10 using for/fold = 55
	body := call("print", call("show",
		forExpr(pVar("i"), rangeExpr(intLit(1), intLit(10), true), nil,
			&ast.FoldClause{Acc: "t", Init: intLit(0)},
			infix("+", varExpr("t"), varExpr("i")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "55")
}

func TestDerivedShow(t *testing.T) {
	// type Color = Red | Green | Blue deriving (Show)
	colorDecl := typeDecl("Color", []ast.Variant{
		{Name: "Red"},
		{Name: "Green"},
		{Name: "Blue"},
	}, []string{"Show"})

	body := letExpr("_", call("print", call("show", varExpr("Red"))),
		call("print", call("show", varExpr("Green"))))

	output := runProgram(t, colorDecl, defn("main", nil, body))
	expectOutput(t, output, "Red\nGreen")
}

func TestDerivedEq(t *testing.T) {
	colorDecl := typeDecl("Color", []ast.Variant{
		{Name: "Red"},
		{Name: "Green"},
	}, []string{"Eq"})

	body := letExpr("_", call("print", call("show", infix("==", varExpr("Red"), varExpr("Red")))),
		call("print", call("show", infix("==", varExpr("Red"), varExpr("Green")))))

	output := runProgram(t, colorDecl, defn("main", nil, body))
	expectOutput(t, output, "true\nfalse")
}

func TestPhase5_RecordSpreadOverride(t *testing.T) {
	// {..base, x: 99} where base = {x: 1, y: 2}
	body := letExpr("base", recordExprOrdered([]string{"x", "y"}, []ast.Expr{intLit(1), intLit(2)}),
		letExpr("updated", ast.ExprRecord{
			Fields: []ast.RecordField{{Name: "x", Value: intLit(99)}},
			Spread: varExpr("base"),
			Loc:    noLoc,
		}, call("print", call("show", varExpr("updated")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "{x: 99, y: 2}")
}

func TestPhase2_NestedRange(t *testing.T) {
	// flat-map(1..=2, fn(i) => for j in 1..=2 do (i, j))
	body := call("print", call("show",
		call("flat-map",
			rangeExpr(intLit(1), intLit(2), true),
			lambda([]string{"i"},
				forExpr(pVar("j"), rangeExpr(intLit(1), intLit(2), true), nil, nil,
					tupleExpr(varExpr("i"), varExpr("j")))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "[(1, 1), (1, 2), (2, 1), (2, 2)]")
}

func TestPhase5_RecordPatternNested(t *testing.T) {
	// match {a: {x: 1}} { {a: {x}} => print(show(x)) }
	body := letExpr("r",
		recordExprOrdered([]string{"a"}, []ast.Expr{recordExprOrdered([]string{"x"}, []ast.Expr{intLit(42)})}),
		matchExpr(varExpr("r"),
			matchArm(
				pRecord([]ast.PatField{{Name: "a", Pattern: pRecord([]ast.PatField{{Name: "x"}}, "")}}, ""),
				call("print", call("show", varExpr("x"))))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "42")
}

func TestPhase3_HandleReturn(t *testing.T) {
	// handle 42 { return x -> x + 1 }
	body := letExpr("result",
		handleExpr(intLit(42),
			handlerArm("return", []string{"x"}, "", infix("+", varExpr("x"), intLit(1)))),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "43")
}

// ── Phase 3 additional: tests ported from test/phase3/ ──

func TestPhase3_MultipleEffects(t *testing.T) {
	// 02-multiple-effects: declare two effects, just print
	logEffect := effectDecl("log", []string{"info"})
	storeEffect := effectDecl("store", []string{"get-val", "set-val"})

	mainBody := call("print", strLit("two effects declared"))

	output := runProgram(t, logEffect, storeEffect, defn("main", nil, mainBody))
	expectOutput(t, output, "two effects declared")
}

func TestPhase3_EffectWithFunctions(t *testing.T) {
	// 03-effect-with-functions: effect alongside a regular function
	counterEffect := effectDecl("counter", []string{"increment", "get-count"})

	doubleBody := infix("*", varExpr("x"), intLit(2))

	mainBody := letExpr("result", call("double", intLit(21)),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t, counterEffect,
		defn("double", []string{"x"}, doubleBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "42")
}

func TestPhase3_EffectBeforeType(t *testing.T) {
	// 05-effect-before-type: effect, then type, then match on type
	notifyEffect := effectDecl("notify", []string{"send"})
	priorityDecl := typeDecl("Priority", []ast.Variant{
		{Name: "High"},
		{Name: "Medium"},
		{Name: "Low"},
	}, nil)

	mainBody := letExpr("p", varExpr("High"),
		matchExpr(varExpr("p"),
			matchArm(pVariant("High"), call("print", strLit("high priority"))),
			matchArm(pVariant("Medium"), call("print", strLit("medium"))),
			matchArm(pVariant("Low"), call("print", strLit("low")))))

	output := runProgram(t, notifyEffect, priorityDecl, defn("main", nil, mainBody))
	expectOutput(t, output, "high priority")
}

func TestPhase3_HandleResume(t *testing.T) {
	// 08-handle-resume: handler provides value 100 via resume
	askEffect := effectDecl("ask", []string{"get-value"})

	computeBody := letExpr("x", call("get-value"), infix("+", varExpr("x"), intLit(42)))

	mainBody := letExpr("result",
		handleExpr(call("compute"),
			handlerArm("return", []string{"x"}, "", varExpr("x")),
			handlerArm("get-value", []string{"_"}, "k", apply(varExpr("k"), intLit(100)))),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t, askEffect,
		defn("compute", nil, computeBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "142")
}

func TestPhase3_HandleMultipleOps(t *testing.T) {
	// 09-handle-multiple-ops: handler with two operations
	configEffect := effectDecl("config", []string{"get-name", "get-port"})

	makeMsgBody := letExpr("name", call("get-name"),
		letExpr("port", call("get-port"),
			infix("++", varExpr("name"), infix("++", strLit(" "), call("show", varExpr("port"))))))

	mainBody := letExpr("result",
		handleExpr(call("make-msg"),
			handlerArm("return", []string{"x"}, "", varExpr("x")),
			handlerArm("get-name", []string{"_"}, "k", apply(varExpr("k"), strLit("hello"))),
			handlerArm("get-port", []string{"_"}, "k", apply(varExpr("k"), intLit(42)))),
		call("print", varExpr("result")))

	output := runProgram(t, configEffect,
		defn("make-msg", nil, makeMsgBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "hello 42")
}

func TestPhase3_HandlePerform(t *testing.T) {
	// 12-handle-perform: explicit perform triggers effect
	valEffect := effectDecl("val", []string{"ask"})

	mainBody := letExpr("result",
		handleExpr(performExpr(call("ask")),
			handlerArm("return", []string{"x"}, "", varExpr("x")),
			handlerArm("ask", []string{"_"}, "k", apply(varExpr("k"), intLit(99)))),
		call("print", call("show", varExpr("result"))))

	output := runProgram(t, valEffect, defn("main", nil, mainBody))
	expectOutput(t, output, "99")
}

func TestPhase3_BuiltinRaise(t *testing.T) {
	// 14-builtin-raise: raise without explicit effect declaration
	mainBody := letExpr("r1",
		handleExpr(call("raise", strLit("something went wrong")),
			handlerArm("return", []string{"x"}, "", infix("++", strLit("ok: "), call("show", varExpr("x")))),
			handlerArm("raise", []string{"msg"}, "", infix("++", strLit("caught: "), varExpr("msg")))),
		letExpr("_", call("print", varExpr("r1")),
			letExpr("r2",
				handleExpr(intLit(42),
					handlerArm("return", []string{"x"}, "", infix("++", strLit("ok: "), call("show", varExpr("x")))),
					handlerArm("raise", []string{"msg"}, "", infix("++", strLit("caught: "), varExpr("msg")))),
				call("print", varExpr("r2")))))

	output := runProgram(t, defn("main", nil, mainBody))
	expectOutput(t, output, "caught: something went wrong\nok: 42")
}

// ── Phase 5 additional: record pattern rename ──

func TestPhase5_RecordPatternRename(t *testing.T) {
	// 23-record-pattern-rename: {name: n, age: a | _} binds n and a
	body := letExpr("person", recordExprOrdered(
		[]string{"name", "age"},
		[]ast.Expr{strLit("Ada"), intLit(36)}),
		matchExpr(varExpr("person"),
			matchArm(
				pRecord([]ast.PatField{
					{Name: "name", Pattern: pVar("n")},
					{Name: "age", Pattern: pVar("a")},
				}, "_"),
				call("print", varExpr("n")))))

	output := runProgram(t, defn("main", nil, body))
	expectOutput(t, output, "Ada")
}

// Verify that running a complete multi-phase program works end-to-end.
func TestEndToEnd_FullProgram(t *testing.T) {
	// type Option = Some(a) | None
	optDecl := typeDecl("Option", []ast.Variant{
		{Name: "Some", Fields: []ast.TypeExpr{ast.TypeName{Name: "a"}}},
		{Name: "None"},
	}, []string{"Show"})

	// safe-head(xs) = if len(xs) == 0 then None else Some(head(xs))
	safeHeadBody := ifExpr(
		infix("==", call("len", varExpr("xs")), intLit(0)),
		varExpr("None"),
		call("Some", call("head", varExpr("xs"))))

	// main: pipeline + for + match + effects
	mainBody := letExpr("xs", listExpr(intLit(1), intLit(2), intLit(3)),
		letExpr("doubled", forExpr(pVar("x"), varExpr("xs"), nil, nil, infix("*", varExpr("x"), intLit(2))),
			letExpr("_", pipeline(pipeline(varExpr("doubled"), varExpr("show")), varExpr("print")),
				letExpr("h", call("safe-head", varExpr("doubled")),
					matchExpr(varExpr("h"),
						matchArm(pVariant("Some", pVar("v")), call("print", call("show", varExpr("v")))),
						matchArm(pVariant("None"), call("print", strLit("empty"))))))))

	output := runProgram(t,
		optDecl,
		defn("safe-head", []string{"xs"}, safeHeadBody),
		defn("main", nil, mainBody))
	expectOutput(t, output, "[2, 4, 6]\n2")
}

// ── Desugarer unit tests ──

func TestDesugar_Pipeline(t *testing.T) {
	// x |> f(y) → f(x, y)
	expr := pipeline(varExpr("x"), call("f", varExpr("y")))
	result := desugar.Desugar(expr)
	app, ok := result.(ast.ExprApply)
	if !ok {
		t.Fatalf("expected ExprApply, got %T", result)
	}
	if len(app.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(app.Args))
	}
}

func TestDesugar_Infix(t *testing.T) {
	// a + b → add(a, b)
	expr := infix("+", varExpr("a"), varExpr("b"))
	result := desugar.Desugar(expr)
	app, ok := result.(ast.ExprApply)
	if !ok {
		t.Fatalf("expected ExprApply, got %T", result)
	}
	fn, ok := app.Fn.(ast.ExprVar)
	if !ok || fn.Name != "add" {
		t.Errorf("expected fn name 'add', got %v", app.Fn)
	}
}

func TestDesugar_ShortCircuit(t *testing.T) {
	// a && b → if a then b else false
	expr := infix("&&", varExpr("a"), varExpr("b"))
	result := desugar.Desugar(expr)
	if _, ok := result.(ast.ExprIf); !ok {
		t.Fatalf("expected ExprIf, got %T", result)
	}
}

func TestDesugar_DoBlock(t *testing.T) {
	// do { x <- 1; x }
	expr := doBlock(doStep("x", intLit(1)), doStep("", varExpr("x")))
	result := desugar.Desugar(expr)
	letE, ok := result.(ast.ExprLet)
	if !ok {
		t.Fatalf("expected ExprLet, got %T", result)
	}
	if letE.Name != "x" {
		t.Errorf("expected name 'x', got '%s'", letE.Name)
	}
}

func TestDesugar_Range(t *testing.T) {
	// 1..5 → range(1, sub(5, 1))
	expr := rangeExpr(intLit(1), intLit(5), false)
	result := desugar.Desugar(expr)
	app, ok := result.(ast.ExprApply)
	if !ok {
		t.Fatalf("expected ExprApply, got %T", result)
	}
	fn, ok := app.Fn.(ast.ExprVar)
	if !ok || fn.Name != "range" {
		t.Errorf("expected fn name 'range', got %v", app.Fn)
	}
	// Second arg should be sub(5, 1)
	subApp, ok := app.Args[1].(ast.ExprApply)
	if !ok {
		t.Fatalf("expected sub apply, got %T", app.Args[1])
	}
	subFn, ok := subApp.Fn.(ast.ExprVar)
	if !ok || subFn.Name != "sub" {
		t.Errorf("expected fn name 'sub', got %v", subApp.Fn)
	}
}

func TestDesugar_RangeInclusive(t *testing.T) {
	// 1..=5 → range(1, 5)
	expr := rangeExpr(intLit(1), intLit(5), true)
	result := desugar.Desugar(expr)
	app, ok := result.(ast.ExprApply)
	if !ok {
		t.Fatalf("expected ExprApply, got %T", result)
	}
	// Second arg should be just 5 (no sub)
	if _, ok := app.Args[1].(ast.ExprLiteral); !ok {
		t.Errorf("expected literal for inclusive range end, got %T", app.Args[1])
	}
}

func TestDesugar_For(t *testing.T) {
	// for x in xs do x * 2 → __for_each(xs, fn(x) => x * 2)
	expr := forExpr(pVar("x"), varExpr("xs"), nil, nil, infix("*", varExpr("x"), intLit(2)))
	result := desugar.Desugar(expr)
	app, ok := result.(ast.ExprApply)
	if !ok {
		t.Fatalf("expected ExprApply, got %T", result)
	}
	fn, ok := app.Fn.(ast.ExprVar)
	if !ok || fn.Name != "__for_each" {
		t.Errorf("expected fn name '__for_each', got %v", app.Fn)
	}
}

func TestDesugar_Unary(t *testing.T) {
	// !x → not(x)
	expr := unary("!", varExpr("x"))
	result := desugar.Desugar(expr)
	app, ok := result.(ast.ExprApply)
	if !ok {
		t.Fatalf("expected ExprApply, got %T", result)
	}
	fn, ok := app.Fn.(ast.ExprVar)
	if !ok || fn.Name != "not" {
		t.Errorf("expected fn name 'not', got %v", app.Fn)
	}
}

// Suppress unused import warning
var _ = fmt.Sprint
