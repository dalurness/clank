package vm

import (
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/compiler"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/token"
)

var loc = token.Loc{Line: 1, Col: 1}

// ── AST helpers ──

func litInt(n int64) ast.Expr  { return ast.ExprLiteral{Value: ast.LitInt{Value: n}, Loc: loc} }
func litBool(v bool) ast.Expr  { return ast.ExprLiteral{Value: ast.LitBool{Value: v}, Loc: loc} }
func litStr(s string) ast.Expr { return ast.ExprLiteral{Value: ast.LitStr{Value: s}, Loc: loc} }
func litUnit() ast.Expr        { return ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc} }
func varRef(name string) ast.Expr { return ast.ExprVar{Name: name, Loc: loc} }

func letExpr(name string, value, body ast.Expr) ast.Expr {
	return ast.ExprLet{Name: name, Value: value, Body: body, Loc: loc}
}
func ifExpr(cond, then, els ast.Expr) ast.Expr {
	return ast.ExprIf{Cond: cond, Then: then, Else: els, Loc: loc}
}
func apply(fn ast.Expr, args []ast.Expr) ast.Expr {
	return ast.ExprApply{Fn: fn, Args: args, Loc: loc}
}
func lambda(params []string, body ast.Expr) ast.Expr {
	ps := make([]ast.Param, len(params))
	for i, n := range params {
		ps[i] = ast.Param{Name: n}
	}
	return ast.ExprLambda{Params: ps, Body: body, Loc: loc}
}
func listExpr(elements []ast.Expr) ast.Expr {
	return ast.ExprList{Elements: elements, Loc: loc}
}
func tupleExpr(elements []ast.Expr) ast.Expr {
	return ast.ExprTuple{Elements: elements, Loc: loc}
}
func infix(op string, left, right ast.Expr) ast.Expr {
	return ast.ExprInfix{Op: op, Left: left, Right: right, Loc: loc}
}
func handleExpr(expr ast.Expr, arms []ast.HandlerArm) ast.Expr {
	return ast.ExprHandle{Expr: expr, Arms: arms, Loc: loc}
}
func performExpr(expr ast.Expr) ast.Expr {
	return ast.ExprPerform{Expr: expr, Loc: loc}
}
func handlerArm(name string, params []string, resumeName string, body ast.Expr) ast.HandlerArm {
	ps := make([]ast.Param, len(params))
	for i, n := range params {
		ps[i] = ast.Param{Name: n}
	}
	return ast.HandlerArm{Name: name, Params: ps, ResumeName: resumeName, Body: body}
}

func sig(paramNames []string) ast.TypeSig {
	params := make([]ast.TypeSigParam, len(paramNames))
	for i, n := range paramNames {
		params[i] = ast.TypeSigParam{Name: n, Type: ast.TypeName{Name: "Int", Loc: loc}}
	}
	return ast.TypeSig{Params: params, ReturnType: ast.TypeName{Name: "Int", Loc: loc}}
}

func def(name string, params []string, body ast.Expr) ast.TopLevel {
	return ast.TopDefinition{Name: name, Sig: sig(params), Body: desugar.Desugar(body), Pub: true, Loc: loc}
}

func compileAndRun(topLevels ...ast.TopLevel) (*Value, []string, error) {
	prog := &ast.Program{TopLevels: topLevels}
	mod := compiler.CompileProgram(prog)
	return Execute(mod)
}

func runMain(params []string, body ast.Expr) (*Value, error) {
	result, _, err := compileAndRun(def("main", params, body))
	return result, err
}

func assertIntResult(t *testing.T, result *Value, expected int) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Tag != TagINT {
		t.Fatalf("expected Int, got tag %d", result.Tag)
	}
	if result.IntVal != expected {
		t.Fatalf("expected %d, got %d", expected, result.IntVal)
	}
}

func assertBoolResult(t *testing.T, result *Value, expected bool) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Tag != TagBOOL {
		t.Fatalf("expected Bool, got tag %d", result.Tag)
	}
	if result.BoolVal != expected {
		t.Fatalf("expected %v, got %v", expected, result.BoolVal)
	}
}

func assertStrResult(t *testing.T, result *Value, expected string) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Tag != TagSTR {
		t.Fatalf("expected Str, got tag %d", result.Tag)
	}
	if result.StrVal != expected {
		t.Fatalf("expected %q, got %q", expected, result.StrVal)
	}
}

// ── Literal tests ──

func TestIntLiteral(t *testing.T) {
	result, err := runMain(nil, litInt(42))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestBoolTrue(t *testing.T) {
	result, err := runMain(nil, litBool(true))
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, true)
}

func TestBoolFalse(t *testing.T) {
	result, err := runMain(nil, litBool(false))
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, false)
}

func TestStringLiteral(t *testing.T) {
	result, err := runMain(nil, litStr("hello"))
	if err != nil {
		t.Fatal(err)
	}
	assertStrResult(t, result, "hello")
}

func TestUnitLiteral(t *testing.T) {
	result, err := runMain(nil, litUnit())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Tag != TagUNIT {
		t.Fatal("expected Unit")
	}
}

func TestLargeIntU16(t *testing.T) {
	result, err := runMain(nil, litInt(300))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 300)
}

func TestLargeIntU32(t *testing.T) {
	result, err := runMain(nil, litInt(100000))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 100000)
}

// ── Arithmetic tests ──

func TestAddition(t *testing.T) {
	body := desugar.Desugar(infix("+", litInt(3), litInt(4)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 7)
}

func TestSubtraction(t *testing.T) {
	body := desugar.Desugar(infix("-", litInt(10), litInt(3)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 7)
}

func TestMultiplication(t *testing.T) {
	body := desugar.Desugar(infix("*", litInt(6), litInt(7)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestDivision(t *testing.T) {
	body := desugar.Desugar(infix("/", litInt(10), litInt(2)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 5)
}

func TestModulo(t *testing.T) {
	body := desugar.Desugar(infix("%", litInt(10), litInt(3)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 1)
}

func TestNestedArithmetic(t *testing.T) {
	body := desugar.Desugar(infix("*", infix("+", litInt(2), litInt(3)), litInt(4)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 20)
}

// ── Comparison tests ──

func TestEqualityTrue(t *testing.T) {
	body := desugar.Desugar(infix("==", litInt(5), litInt(5)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, true)
}

func TestEqualityFalse(t *testing.T) {
	body := desugar.Desugar(infix("==", litInt(5), litInt(3)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, false)
}

func TestLessThan(t *testing.T) {
	body := desugar.Desugar(infix("<", litInt(3), litInt(5)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, true)
}

func TestGreaterThan(t *testing.T) {
	body := desugar.Desugar(infix(">", litInt(5), litInt(3)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, true)
}

// ── Let binding tests ──

func TestSimpleLet(t *testing.T) {
	result, err := runMain(nil, letExpr("x", litInt(10), varRef("x")))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 10)
}

func TestLetWithArithmetic(t *testing.T) {
	body := letExpr("x", litInt(5),
		letExpr("y", litInt(3),
			desugar.Desugar(infix("+", varRef("x"), varRef("y")))))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 8)
}

// ── If/then/else tests ──

func TestIfTrue(t *testing.T) {
	result, err := runMain(nil, ifExpr(litBool(true), litInt(1), litInt(2)))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 1)
}

func TestIfFalse(t *testing.T) {
	result, err := runMain(nil, ifExpr(litBool(false), litInt(1), litInt(2)))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 2)
}

func TestIfWithCondition(t *testing.T) {
	body := ifExpr(desugar.Desugar(infix("==", litInt(5), litInt(5))), litInt(10), litInt(20))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 10)
}

// ── Function call tests ──

func TestCallFunction(t *testing.T) {
	result, _, err := compileAndRun(
		def("double", []string{"x"}, desugar.Desugar(infix("+", varRef("x"), varRef("x")))),
		def("main", nil, apply(varRef("double"), []ast.Expr{litInt(21)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestFunctionTwoParams(t *testing.T) {
	result, _, err := compileAndRun(
		def("add-fn", []string{"a", "b"}, desugar.Desugar(infix("+", varRef("a"), varRef("b")))),
		def("main", nil, apply(varRef("add-fn"), []ast.Expr{litInt(10), litInt(32)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestNestedFunctionCalls(t *testing.T) {
	result, _, err := compileAndRun(
		def("inc", []string{"x"}, desugar.Desugar(infix("+", varRef("x"), litInt(1)))),
		def("main", nil, apply(varRef("inc"), []ast.Expr{
			apply(varRef("inc"), []ast.Expr{
				apply(varRef("inc"), []ast.Expr{litInt(0)}),
			}),
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 3)
}

// ── Recursion tests ──

func TestFactorial5(t *testing.T) {
	body := desugar.Desugar(ifExpr(
		infix("==", varRef("n"), litInt(0)),
		litInt(1),
		infix("*", varRef("n"), apply(varRef("factorial"), []ast.Expr{infix("-", varRef("n"), litInt(1))})),
	))
	result, _, err := compileAndRun(
		ast.TopDefinition{Name: "factorial", Sig: sig([]string{"n"}), Body: body, Pub: true, Loc: loc},
		def("main", nil, apply(varRef("factorial"), []ast.Expr{litInt(5)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 120)
}

func TestFactorial10(t *testing.T) {
	body := desugar.Desugar(ifExpr(
		infix("==", varRef("n"), litInt(0)),
		litInt(1),
		infix("*", varRef("n"), apply(varRef("factorial"), []ast.Expr{infix("-", varRef("n"), litInt(1))})),
	))
	result, _, err := compileAndRun(
		ast.TopDefinition{Name: "factorial", Sig: sig([]string{"n"}), Body: body, Pub: true, Loc: loc},
		def("main", nil, apply(varRef("factorial"), []ast.Expr{litInt(10)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 3628800)
}

// ── Tail recursion tests ──

func TestTailRecursiveSum(t *testing.T) {
	sumSig := ast.TypeSig{
		Params: []ast.TypeSigParam{
			{Name: "n", Type: ast.TypeName{Name: "Int", Loc: loc}},
			{Name: "acc", Type: ast.TypeName{Name: "Int", Loc: loc}},
		},
		ReturnType: ast.TypeName{Name: "Int", Loc: loc},
	}
	sumBody := desugar.Desugar(ifExpr(
		infix("==", varRef("n"), litInt(0)),
		varRef("acc"),
		apply(varRef("sum"), []ast.Expr{
			infix("-", varRef("n"), litInt(1)),
			infix("+", varRef("acc"), varRef("n")),
		}),
	))
	result, _, err := compileAndRun(
		ast.TopDefinition{Name: "sum", Sig: sumSig, Body: sumBody, Pub: true, Loc: loc},
		def("main", nil, apply(varRef("sum"), []ast.Expr{litInt(100), litInt(0)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 5050)
}

func TestDeepTailRecursion(t *testing.T) {
	body := desugar.Desugar(ifExpr(
		infix("==", varRef("n"), litInt(0)),
		litInt(0),
		apply(varRef("countdown"), []ast.Expr{infix("-", varRef("n"), litInt(1))}),
	))
	result, _, err := compileAndRun(
		ast.TopDefinition{Name: "countdown", Sig: sig([]string{"n"}), Body: body, Pub: true, Loc: loc},
		def("main", nil, apply(varRef("countdown"), []ast.Expr{litInt(1000)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 0)
}

// ── Lambda / higher-order tests ──

func TestLambdaNoCaptures(t *testing.T) {
	body := letExpr("f",
		lambda([]string{"x"}, desugar.Desugar(infix("+", varRef("x"), litInt(1)))),
		apply(varRef("f"), []ast.Expr{litInt(41)}),
	)
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestLambdaWithCapture(t *testing.T) {
	body := letExpr("n", litInt(10),
		letExpr("f",
			lambda([]string{"x"}, desugar.Desugar(infix("+", varRef("x"), varRef("n")))),
			apply(varRef("f"), []ast.Expr{litInt(32)}),
		),
	)
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestClosureReturnedFromFunction(t *testing.T) {
	result, _, err := compileAndRun(
		def("make-adder", []string{"n"},
			lambda([]string{"x"}, desugar.Desugar(infix("+", varRef("x"), varRef("n")))),
		),
		def("main", nil,
			letExpr("add5",
				apply(varRef("make-adder"), []ast.Expr{litInt(5)}),
				apply(varRef("add5"), []ast.Expr{litInt(37)}),
			),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

// ── List operations ──

func TestListLiteral(t *testing.T) {
	result, err := runMain(nil, listExpr([]ast.Expr{litInt(1), litInt(2), litInt(3)}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Tag != TagHEAP || result.Heap.Kind != KindList {
		t.Fatal("expected list")
	}
	if len(result.Heap.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Heap.Items))
	}
}

func TestTupleLiteral(t *testing.T) {
	result, err := runMain(nil, tupleExpr([]ast.Expr{litInt(1), litStr("hi")}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Tag != TagHEAP || result.Heap.Kind != KindTuple {
		t.Fatal("expected tuple")
	}
	if len(result.Heap.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Heap.Items))
	}
}

// ── Short-circuit logic ──

func TestAndTrueTrue(t *testing.T) {
	body := desugar.Desugar(infix("&&", litBool(true), litBool(true)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, true)
}

func TestAndTrueFalse(t *testing.T) {
	body := desugar.Desugar(infix("&&", litBool(true), litBool(false)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, false)
}

func TestOrFalseTrue(t *testing.T) {
	body := desugar.Desugar(infix("||", litBool(false), litBool(true)))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, true)
}

// ── I/O ──

func TestIOPrint(t *testing.T) {
	mod := &compiler.BytecodeModule{
		Words: []compiler.BytecodeWord{{
			Name: "main", WordID: 256,
			Code: []byte{
				compiler.OpPUSH_STR, 0, 0,
				compiler.OpIO_PRINT,
				compiler.OpRET,
			},
			LocalCount: 0, IsPublic: true,
		}},
		Strings:     []string{"hello world"},
		Rationals:   nil,
		EntryWordID: intPtr(256),
	}
	_, stdout, err := Execute(mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(stdout) != 1 || stdout[0] != "hello world" {
		t.Fatalf("expected [hello world], got %v", stdout)
	}
}

// ── Fibonacci ──

func TestFibonacci10(t *testing.T) {
	body := desugar.Desugar(ifExpr(
		infix("<=", varRef("n"), litInt(1)),
		varRef("n"),
		infix("+",
			apply(varRef("fib"), []ast.Expr{infix("-", varRef("n"), litInt(1))}),
			apply(varRef("fib"), []ast.Expr{infix("-", varRef("n"), litInt(2))}),
		),
	))
	result, _, err := compileAndRun(
		ast.TopDefinition{Name: "fib", Sig: sig([]string{"n"}), Body: body, Pub: true, Loc: loc},
		def("main", nil, apply(varRef("fib"), []ast.Expr{litInt(10)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 55)
}

// ── Mutual recursion ──

func TestMutualRecursion(t *testing.T) {
	evenBody := desugar.Desugar(ifExpr(
		infix("==", varRef("n"), litInt(0)),
		litBool(true),
		apply(varRef("is-odd"), []ast.Expr{infix("-", varRef("n"), litInt(1))}),
	))
	oddBody := desugar.Desugar(ifExpr(
		infix("==", varRef("n"), litInt(0)),
		litBool(false),
		apply(varRef("is-even"), []ast.Expr{infix("-", varRef("n"), litInt(1))}),
	))
	result, _, err := compileAndRun(
		ast.TopDefinition{Name: "is-even", Sig: sig([]string{"n"}), Body: evenBody, Pub: true, Loc: loc},
		ast.TopDefinition{Name: "is-odd", Sig: sig([]string{"n"}), Body: oddBody, Pub: true, Loc: loc},
		def("main", nil, apply(varRef("is-even"), []ast.Expr{litInt(10)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBoolResult(t, result, true)
}

// ── String operations ──

func TestShowInt(t *testing.T) {
	body := apply(varRef("show"), []ast.Expr{litInt(42)})
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertStrResult(t, result, "42")
}

func TestStringConcat(t *testing.T) {
	body := desugar.Desugar(infix("++", litStr("hello "), litStr("world")))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertStrResult(t, result, "hello world")
}

// ── Edge cases ──

func TestZeroArgFunction(t *testing.T) {
	result, _, err := compileAndRun(
		def("f", nil, litInt(42)),
		def("main", nil, apply(varRef("f"), nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestDeeplyNestedIf(t *testing.T) {
	body := ifExpr(litBool(true),
		ifExpr(litBool(false), litInt(1),
			ifExpr(litBool(true), litInt(2), litInt(3))),
		litInt(4))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 2)
}

func TestMultipleLetBindings(t *testing.T) {
	body := letExpr("a", litInt(1),
		letExpr("b", litInt(2),
			letExpr("c", litInt(3),
				desugar.Desugar(infix("+", infix("+", varRef("a"), varRef("b")), varRef("c"))))))
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 6)
}

// ── Effect handler tests ──

func TestHandleReturnPassthrough(t *testing.T) {
	body := handleExpr(litInt(42), []ast.HandlerArm{
		handlerArm("return", []string{"x"}, "", varRef("x")),
	})
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestHandleReturnTransform(t *testing.T) {
	body := handleExpr(litInt(10), []ast.HandlerArm{
		handlerArm("return", []string{"x"}, "", desugar.Desugar(infix("+", varRef("x"), litInt(1)))),
	})
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 11)
}

func TestHandleNoReturnClause(t *testing.T) {
	body := handleExpr(litInt(42), []ast.HandlerArm{
		handlerArm("raise", []string{"e"}, "", litInt(0)),
	})
	result, err := runMain(nil, body)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestPerformCaughtNoResume(t *testing.T) {
	result, _, err := compileAndRun(
		def("fail-fn", nil, performExpr(apply(varRef("raise"), []ast.Expr{litInt(99)}))),
		def("main", nil, handleExpr(
			apply(varRef("fail-fn"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("raise", []string{"e"}, "", desugar.Desugar(infix("+", varRef("e"), litInt(1)))),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 100)
}

func TestPerformCaughtDiscard(t *testing.T) {
	result, _, err := compileAndRun(
		def("fail-fn", nil, performExpr(apply(varRef("raise"), []ast.Expr{litInt(0)}))),
		def("main", nil, handleExpr(
			apply(varRef("fail-fn"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("raise", []string{"_e"}, "", litInt(42)),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestPerformNoArgs(t *testing.T) {
	result, _, err := compileAndRun(
		def("ask-fn", nil, performExpr(apply(varRef("ask"), nil))),
		def("main", nil, handleExpr(
			apply(varRef("ask-fn"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("ask", nil, "", litInt(42)),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestPerformWithResume(t *testing.T) {
	result, _, err := compileAndRun(
		def("get-val", nil, performExpr(apply(varRef("ask"), nil))),
		def("main", nil, handleExpr(
			apply(varRef("get-val"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("ask", nil, "k", apply(varRef("k"), []ast.Expr{litInt(42)})),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestResumeValueUsedInComputation(t *testing.T) {
	result, _, err := compileAndRun(
		def("compute", nil,
			letExpr("v", performExpr(apply(varRef("ask"), nil)),
				desugar.Desugar(infix("+", varRef("v"), litInt(10))))),
		def("main", nil, handleExpr(
			apply(varRef("compute"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("ask", nil, "k", apply(varRef("k"), []ast.Expr{litInt(32)})),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestResumeWithReturnTransform(t *testing.T) {
	result, _, err := compileAndRun(
		def("compute", nil, performExpr(apply(varRef("ask"), nil))),
		def("main", nil, handleExpr(
			apply(varRef("compute"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", desugar.Desugar(infix("*", varRef("x"), litInt(2)))),
				handlerArm("ask", nil, "k", apply(varRef("k"), []ast.Expr{litInt(21)})),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestPerformWithArgResumeUsesIt(t *testing.T) {
	result, _, err := compileAndRun(
		def("compute", nil, performExpr(apply(varRef("put"), []ast.Expr{litInt(10)}))),
		def("main", nil, handleExpr(
			apply(varRef("compute"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("put", []string{"v"}, "k", apply(varRef("k"), []ast.Expr{desugar.Desugar(infix("+", varRef("v"), litInt(1)))})),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 11)
}

func TestMultiplePerforms(t *testing.T) {
	result, _, err := compileAndRun(
		def("counter", nil,
			letExpr("a", performExpr(apply(varRef("get"), nil)),
				letExpr("_b", performExpr(apply(varRef("get"), nil)),
					desugar.Desugar(infix("+", varRef("a"), litInt(1)))))),
		def("main", nil, handleExpr(
			apply(varRef("counter"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("get", nil, "k", apply(varRef("k"), []ast.Expr{litInt(10)})),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 11)
}

func TestNestedCallPerform(t *testing.T) {
	result, _, err := compileAndRun(
		def("inner", nil, performExpr(apply(varRef("ask"), nil))),
		def("middle", nil, desugar.Desugar(infix("+", apply(varRef("inner"), nil), litInt(10)))),
		def("main", nil, handleExpr(
			apply(varRef("middle"), nil),
			[]ast.HandlerArm{
				handlerArm("return", []string{"x"}, "", varRef("x")),
				handlerArm("ask", nil, "k", apply(varRef("k"), []ast.Expr{litInt(5)})),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 15)
}

func intPtr(n int) *int {
	return &n
}
