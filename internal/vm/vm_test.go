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

// ── STM Tests ──

func TestTVarNewAndRead(t *testing.T) {
	// let tv = tvar-new 42 in tvar-read tv
	result, err := runMain(nil,
		letExpr("tv", apply(varRef("tvar-new"), []ast.Expr{litInt(42)}),
			apply(varRef("tvar-read"), []ast.Expr{varRef("tv")})))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 42)
}

func TestTVarWrite(t *testing.T) {
	// let tv = tvar-new 10 in
	//   let _ = tvar-write tv 99 in
	//     tvar-read tv
	result, err := runMain(nil,
		letExpr("tv", apply(varRef("tvar-new"), []ast.Expr{litInt(10)}),
			letExpr("_w", apply(varRef("tvar-write"), []ast.Expr{varRef("tv"), litInt(99)}),
				apply(varRef("tvar-read"), []ast.Expr{varRef("tv")}))))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 99)
}

func TestTVarTakeAndPut(t *testing.T) {
	// let tv = tvar-new 7 in
	//   let val = tvar-take tv in    -- val=7, tv is now empty
	//     let _ = tvar-put tv 100 in -- tv=100
	//       val + tvar-read tv       -- 7 + 100 = 107
	result, err := runMain(nil,
		letExpr("tv", apply(varRef("tvar-new"), []ast.Expr{litInt(7)}),
			letExpr("val", apply(varRef("tvar-take"), []ast.Expr{varRef("tv")}),
				letExpr("_p", apply(varRef("tvar-put"), []ast.Expr{varRef("tv"), litInt(100)}),
					desugar.Desugar(infix("+", varRef("val"),
						apply(varRef("tvar-read"), []ast.Expr{varRef("tv")})))))))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 107)
}

func TestTVarTakeEmptyTraps(t *testing.T) {
	// let tv = tvar-new 1 in
	//   let _ = tvar-take tv in  -- tv now empty
	//     tvar-take tv           -- should trap
	_, err := runMain(nil,
		letExpr("tv", apply(varRef("tvar-new"), []ast.Expr{litInt(1)}),
			letExpr("_", apply(varRef("tvar-take"), []ast.Expr{varRef("tv")}),
				apply(varRef("tvar-take"), []ast.Expr{varRef("tv")}))))
	if err == nil {
		t.Fatal("expected trap on tvar-take of empty TVar")
	}
}

func TestTVarPutOccupiedTraps(t *testing.T) {
	// tvar-put (tvar-new 1) 2   -- should trap, already occupied
	_, err := runMain(nil,
		apply(varRef("tvar-put"), []ast.Expr{
			apply(varRef("tvar-new"), []ast.Expr{litInt(1)}),
			litInt(2)}))
	if err == nil {
		t.Fatal("expected trap on tvar-put to occupied TVar")
	}
}

func TestAtomicallyBasic(t *testing.T) {
	// let tv = tvar-new 10 in
	//   atomically \-> {
	//     let v = tvar-read tv in
	//     let _ = tvar-write tv (v + 5) in
	//     tvar-read tv
	//   }
	result, err := runMain(nil,
		letExpr("tv", apply(varRef("tvar-new"), []ast.Expr{litInt(10)}),
			apply(varRef("atomically"), []ast.Expr{
				lambda(nil,
					letExpr("v", apply(varRef("tvar-read"), []ast.Expr{varRef("tv")}),
						letExpr("_w", apply(varRef("tvar-write"), []ast.Expr{varRef("tv"),
							desugar.Desugar(infix("+", varRef("v"), litInt(5)))}),
							apply(varRef("tvar-read"), []ast.Expr{varRef("tv")}))))})))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 15)
}

func TestAtomicallyRollbackOnError(t *testing.T) {
	// Verify affine safety: if atomically body traps after tvar-take,
	// the TVar is restored via the transaction write-log (not stack scan).
	tv := &TVar{ID: 1, Version: 0, Val: ValInt(42), Occupied: true, HandleCount: 1}
	vm2 := New(&compiler.BytecodeModule{})

	// Enable transaction tracking and simulate tvar-take with write-log
	vm2.inTxn = true
	vm2.txnWriteLog = nil
	vm2.recordTVarSnapshot(tv)

	// Simulate tvar-take
	tv.Val = ValUnit()
	tv.Occupied = false
	tv.Version++

	// Verify it's empty now
	if tv.Occupied {
		t.Fatal("TVar should be empty after take")
	}

	// Abort: restore from write-log
	restoreTVars(vm2.txnWriteLog)

	// Verify restored
	if !tv.Occupied {
		t.Fatal("TVar should be occupied after rollback")
	}
	if tv.Val.Tag != TagINT || tv.Val.IntVal != 42 {
		t.Fatalf("TVar value should be 42 after rollback, got %v", tv.Val)
	}
	if tv.Version != 0 {
		t.Fatalf("TVar version should be 0 after rollback, got %d", tv.Version)
	}
}

func TestAtomicallyRollbackTVarNotOnStack(t *testing.T) {
	// Regression: TVar only in a closure/local (not on the data stack)
	// must still be rolled back. The old stack-scan approach missed this.
	tv := &TVar{ID: 1, Version: 0, Val: ValInt(100), Occupied: true, HandleCount: 1}
	vm2 := New(&compiler.BytecodeModule{})

	// TVar is NOT on the data stack — simulates it being captured in a closure
	vm2.dataStack = nil
	vm2.inTxn = true
	vm2.txnWriteLog = nil

	// Simulate tvar-write via the opcode path (which calls recordTVarSnapshot)
	vm2.recordTVarSnapshot(tv)
	tv.Val = ValInt(999)
	tv.Version++

	// Abort
	restoreTVars(vm2.txnWriteLog)

	if tv.Val.Tag != TagINT || tv.Val.IntVal != 100 {
		t.Fatalf("expected TVar value 100 after rollback, got %v", tv.Val)
	}
}

func TestOrElseFirstBranchSucceeds(t *testing.T) {
	// or-else (\-> 10) (\-> 20) should return 10
	result, err := runMain(nil,
		apply(varRef("or-else"), []ast.Expr{
			lambda(nil, litInt(10)),
			lambda(nil, litInt(20)),
		}))
	if err != nil {
		t.Fatal(err)
	}
	assertIntResult(t, result, 10)
}

func TestOrElseTVarRollbackOnFirstBranchFailure(t *testing.T) {
	// Verify that if the first branch of or-else modifies a TVar then fails,
	// the TVar is rolled back before the second branch runs.
	tv := &TVar{ID: 1, Version: 0, Val: ValInt(1), Occupied: true, HandleCount: 1}
	vm2 := New(&compiler.BytecodeModule{})

	// Simulate or-else first branch with write-log
	vm2.inTxn = true
	vm2.txnWriteLog = nil
	vm2.recordTVarSnapshot(tv)

	// First branch modifies TVar
	tv.Val = ValInt(999)
	tv.Version++

	// First branch fails → restore from write-log
	restoreTVars(vm2.txnWriteLog)

	// TVar should be back to original
	if tv.Val.Tag != TagINT || tv.Val.IntVal != 1 {
		t.Fatalf("expected TVar value 1 after or-else rollback, got %v", tv.Val)
	}
	if tv.Version != 0 {
		t.Fatalf("expected TVar version 0 after rollback, got %d", tv.Version)
	}
}

func TestWriteLogDeduplicatesMultipleModifications(t *testing.T) {
	// Only the first modification per TVar should be recorded.
	tv := &TVar{ID: 1, Version: 0, Val: ValInt(1), Occupied: true, HandleCount: 1}
	vm2 := New(&compiler.BytecodeModule{})
	vm2.inTxn = true
	vm2.txnWriteLog = nil

	vm2.recordTVarSnapshot(tv)
	tv.Val = ValInt(100)
	tv.Version++

	vm2.recordTVarSnapshot(tv) // second record — should be ignored
	tv.Val = ValInt(200)
	tv.Version++

	if len(vm2.txnWriteLog) != 1 {
		t.Fatalf("expected 1 write-log entry, got %d", len(vm2.txnWriteLog))
	}

	// Restore should go back to original (1), not intermediate (100)
	restoreTVars(vm2.txnWriteLog)
	if tv.Val.Tag != TagINT || tv.Val.IntVal != 1 {
		t.Fatalf("expected TVar value 1, got %v", tv.Val)
	}
}
