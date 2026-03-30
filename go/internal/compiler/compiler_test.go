package compiler

import (
	"testing"

	"github.com/dalurness/clank/internal/ast"
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
func fieldAccess(obj ast.Expr, field string) ast.Expr {
	return ast.ExprFieldAccess{Object: obj, Field: field, Loc: loc}
}
func recordUpdate(base ast.Expr, fields []ast.RecordUpdateField) ast.Expr {
	return ast.ExprRecordUpdate{Base: base, Fields: fields, Loc: loc}
}
func handleExpr(expr ast.Expr, arms []ast.HandlerArm) ast.Expr {
	return ast.ExprHandle{Expr: expr, Arms: arms, Loc: loc}
}
func performExpr(expr ast.Expr) ast.Expr {
	return ast.ExprPerform{Expr: expr, Loc: loc}
}

func sig(paramNames []string) ast.TypeSig {
	params := make([]ast.TypeSigParam, len(paramNames))
	for i, n := range paramNames {
		params[i] = ast.TypeSigParam{Name: n, Type: ast.TypeName{Name: "Int", Loc: loc}}
	}
	return ast.TypeSig{
		Params:     params,
		ReturnType: ast.TypeName{Name: "Int", Loc: loc},
	}
}

func def(name string, params []string, body ast.Expr, pub bool) ast.TopLevel {
	return ast.TopDefinition{
		Name: name, Sig: sig(params), Body: body, Pub: pub, Loc: loc,
	}
}

func program(topLevels ...ast.TopLevel) *ast.Program {
	return &ast.Program{TopLevels: topLevels}
}

func handlerArm(name string, params []string, resumeName string, body ast.Expr) ast.HandlerArm {
	ps := make([]ast.Param, len(params))
	for i, n := range params {
		ps[i] = ast.Param{Name: n}
	}
	return ast.HandlerArm{Name: name, Params: ps, ResumeName: resumeName, Body: body}
}

// ── Test helpers ──

func findWord(mod *BytecodeModule, name string) *BytecodeWord {
	for i := range mod.Words {
		if mod.Words[i].Name == name {
			return &mod.Words[i]
		}
	}
	return nil
}

func hasOpcode(code []byte, op byte) bool {
	for _, b := range code {
		if b == op {
			return true
		}
	}
	return false
}

func opcodeCount(code []byte, op byte) int {
	n := 0
	for _, b := range code {
		if b == op {
			n++
		}
	}
	return n
}

func indexOf(code []byte, op byte) int {
	for i, b := range code {
		if b == op {
			return i
		}
	}
	return -1
}

func lastIndexOf(code []byte, op byte, before int) int {
	for i := before - 1; i >= 0; i-- {
		if code[i] == op {
			return i
		}
	}
	return -1
}

// ── Tests ──

func TestIntegerLiteralSmall(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litInt(42), true)))
	w := findWord(mod, "f")
	if w == nil {
		t.Fatal("word 'f' not found")
	}
	if !hasOpcode(w.Code, OpPUSH_INT) {
		t.Fatal("should emit PUSH_INT")
	}
	idx := indexOf(w.Code, OpPUSH_INT)
	if w.Code[idx+1] != 42 {
		t.Fatalf("operand: got %d, want 42", w.Code[idx+1])
	}
}

func TestIntegerLiteralU16(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litInt(300), true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpPUSH_INT16) {
		t.Fatal("should emit PUSH_INT16")
	}
}

func TestIntegerLiteralU32(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litInt(100000), true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpPUSH_INT32) {
		t.Fatal("should emit PUSH_INT32")
	}
}

func TestBooleanTrue(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litBool(true), true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpPUSH_TRUE) {
		t.Fatal("should emit PUSH_TRUE")
	}
}

func TestBooleanFalse(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litBool(false), true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpPUSH_FALSE) {
		t.Fatal("should emit PUSH_FALSE")
	}
}

func TestStringLiteral(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litStr("hello"), true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpPUSH_STR) {
		t.Fatal("should emit PUSH_STR")
	}
	found := false
	for _, s := range mod.Strings {
		if s == "hello" {
			found = true
		}
	}
	if !found {
		t.Fatal("string table should contain 'hello'")
	}
}

func TestUnitLiteral(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litUnit(), true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpPUSH_UNIT) {
		t.Fatal("should emit PUSH_UNIT")
	}
}

func TestLetBinding(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, letExpr("x", litInt(5), varRef("x")), true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpLOCAL_SET) {
		t.Fatal("should emit LOCAL_SET")
	}
	if !hasOpcode(w.Code, OpLOCAL_GET) {
		t.Fatal("should emit LOCAL_GET")
	}
}

func TestNestedLetBindings(t *testing.T) {
	body := letExpr("x", litInt(1), letExpr("y", litInt(2), varRef("x")))
	mod := CompileProgram(program(def("f", nil, body, true)))
	w := findWord(mod, "f")
	if opcodeCount(w.Code, OpLOCAL_SET) != 2 {
		t.Fatalf("LOCAL_SET count: got %d, want 2", opcodeCount(w.Code, OpLOCAL_SET))
	}
}

func TestIfThenElse(t *testing.T) {
	body := ifExpr(litBool(true), litInt(1), litInt(2))
	mod := CompileProgram(program(def("f", nil, body, true)))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpJMP_UNLESS) {
		t.Fatal("should emit JMP_UNLESS")
	}
	if !hasOpcode(w.Code, OpJMP) {
		t.Fatal("should emit JMP")
	}
}

func TestAdditionBuiltinOp(t *testing.T) {
	// Desugar: infix + becomes apply(varRef("add"), [a, b])
	body := apply(varRef("add"), []ast.Expr{varRef("a"), varRef("b")})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"a", "b"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpADD) {
		t.Fatal("should emit ADD")
	}
	if hasOpcode(w.Code, OpCALL) {
		t.Fatal("should NOT emit CALL for builtin")
	}
}

func TestSubtraction(t *testing.T) {
	body := apply(varRef("sub"), []ast.Expr{varRef("a"), varRef("b")})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"a", "b"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpSUB) {
		t.Fatal("should emit SUB")
	}
}

func TestMultiplication(t *testing.T) {
	body := apply(varRef("mul"), []ast.Expr{varRef("a"), varRef("b")})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"a", "b"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpMUL) {
		t.Fatal("should emit MUL")
	}
}

func TestComparisonOperators(t *testing.T) {
	tests := []struct {
		name string
		op   byte
	}{
		{"eq", OpEQ}, {"neq", OpNEQ}, {"lt", OpLT},
		{"gt", OpGT}, {"lte", OpLTE}, {"gte", OpGTE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := apply(varRef(tt.name), []ast.Expr{varRef("a"), varRef("b")})
			mod := CompileProgram(program(
				ast.TopDefinition{Name: "f", Sig: sig([]string{"a", "b"}), Body: body, Pub: true, Loc: loc},
			))
			w := findWord(mod, "f")
			if !hasOpcode(w.Code, tt.op) {
				t.Fatalf("should emit opcode for %s", tt.name)
			}
		})
	}
}

func TestCallKnownFunction(t *testing.T) {
	mod := CompileProgram(program(
		def("f", []string{"x"}, varRef("x"), true),
		def("g", nil, apply(varRef("f"), []ast.Expr{litInt(1)}), true),
	))
	w := findWord(mod, "g")
	if !hasOpcode(w.Code, OpTAIL_CALL) {
		t.Fatal("should emit TAIL_CALL (tail position)")
	}
	if !hasOpcode(w.Code, OpPUSH_INT) {
		t.Fatal("should push arg")
	}
}

func TestCallKnownFunctionNonTail(t *testing.T) {
	// g calls f(1) then adds 1 — f(1) not in tail position
	body := apply(varRef("add"), []ast.Expr{
		apply(varRef("f"), []ast.Expr{litInt(1)}),
		litInt(1),
	})
	mod := CompileProgram(program(
		def("f", []string{"x"}, varRef("x"), true),
		ast.TopDefinition{Name: "g", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "g")
	if !hasOpcode(w.Code, OpCALL) {
		t.Fatal("should emit CALL (non-tail)")
	}
}

func TestTailCallInIfBranch(t *testing.T) {
	body := ifExpr(
		apply(varRef("eq"), []ast.Expr{varRef("n"), litInt(0)}),
		litInt(0),
		apply(varRef("f"), []ast.Expr{apply(varRef("sub"), []ast.Expr{varRef("n"), litInt(1)})}),
	)
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"n"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpTAIL_CALL) {
		t.Fatal("should emit TAIL_CALL for recursive call in tail position")
	}
}

func TestFunctionParametersBoundCorrectly(t *testing.T) {
	mod := CompileProgram(program(def("f", []string{"a", "b"}, varRef("a"), true)))
	w := findWord(mod, "f")
	// Prologue: LOCAL_SET 1 (b), LOCAL_SET 0 (a) — reverse order
	if w.Code[0] != OpLOCAL_SET || w.Code[1] != 1 {
		t.Fatalf("first op: got %x %d, want LOCAL_SET 1", w.Code[0], w.Code[1])
	}
	if w.Code[2] != OpLOCAL_SET || w.Code[3] != 0 {
		t.Fatalf("second op: got %x %d, want LOCAL_SET 0", w.Code[2], w.Code[3])
	}
}

func TestLambdaNoCaptures(t *testing.T) {
	body := letExpr("f", lambda([]string{"x"}, varRef("x")), varRef("f"))
	mod := CompileProgram(program(def("g", nil, body, true)))
	w := findWord(mod, "g")
	if !hasOpcode(w.Code, OpQUOTE) {
		t.Fatal("should emit QUOTE for no-capture lambda")
	}
	if len(mod.Words) < 2 {
		t.Fatal("should have lambda body as separate word")
	}
}

func TestLambdaWithCaptures(t *testing.T) {
	body := letExpr("g",
		lambda([]string{"x"}, apply(varRef("add"), []ast.Expr{varRef("x"), varRef("n")})),
		varRef("g"),
	)
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"n"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpCLOSURE) {
		t.Fatal("should emit CLOSURE for capturing lambda")
	}
	if !hasOpcode(w.Code, OpLOCAL_GET) {
		t.Fatal("should push captured var")
	}
}

func TestListLiteral(t *testing.T) {
	mod := CompileProgram(program(
		def("f", nil, listExpr([]ast.Expr{litInt(1), litInt(2), litInt(3)}), true),
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpLIST_NEW) {
		t.Fatal("should emit LIST_NEW")
	}
	idx := indexOf(w.Code, OpLIST_NEW)
	if w.Code[idx+1] != 3 {
		t.Fatalf("list count: got %d, want 3", w.Code[idx+1])
	}
}

func TestTupleLiteral(t *testing.T) {
	mod := CompileProgram(program(
		def("f", nil, tupleExpr([]ast.Expr{litInt(1), litStr("hi")}), true),
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpTUPLE_NEW) {
		t.Fatal("should emit TUPLE_NEW")
	}
	idx := indexOf(w.Code, OpTUPLE_NEW)
	if w.Code[idx+1] != 2 {
		t.Fatalf("tuple arity: got %d, want 2", w.Code[idx+1])
	}
}

func TestStringDeduplication(t *testing.T) {
	body := letExpr("a", litStr("hello"), letExpr("b", litStr("hello"), varRef("a")))
	mod := CompileProgram(program(def("f", nil, body, true)))
	count := 0
	for _, s := range mod.Strings {
		if s == "hello" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("string should appear only once: got %d", count)
	}
}

func TestEntryWordIDForMain(t *testing.T) {
	mod := CompileProgram(program(def("main", nil, litUnit(), true)))
	if mod.EntryWordID == nil {
		t.Fatal("should have entry word ID for main")
	}
}

func TestNoEntryWordIDWithoutMain(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litUnit(), true)))
	if mod.EntryWordID != nil {
		t.Fatal("should be nil without main")
	}
}

func TestWordIDsStartAt260(t *testing.T) {
	mod := CompileProgram(program(def("f", nil, litUnit(), true)))
	w := findWord(mod, "f")
	if w.WordID < 260 {
		t.Fatalf("user word IDs should start at 260, got %d", w.WordID)
	}
}

func TestFactorialLikeRecursion(t *testing.T) {
	body := ifExpr(
		apply(varRef("eq"), []ast.Expr{varRef("n"), litInt(0)}),
		litInt(1),
		apply(varRef("mul"), []ast.Expr{
			varRef("n"),
			apply(varRef("factorial"), []ast.Expr{
				apply(varRef("sub"), []ast.Expr{varRef("n"), litInt(1)}),
			}),
		}),
	)
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "factorial", Sig: sig([]string{"n"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "factorial")
	if w == nil {
		t.Fatal("factorial word should exist")
	}
	if !hasOpcode(w.Code, OpEQ) {
		t.Fatal("should test equality")
	}
	if !hasOpcode(w.Code, OpJMP_UNLESS) {
		t.Fatal("should have conditional")
	}
	if !hasOpcode(w.Code, OpSUB) {
		t.Fatal("should subtract")
	}
	if !hasOpcode(w.Code, OpMUL) {
		t.Fatal("should multiply")
	}
	if !hasOpcode(w.Code, OpCALL) {
		t.Fatal("should call recursively")
	}
	if !hasOpcode(w.Code, OpRET) {
		t.Fatal("should return")
	}
}

func TestNestedFunctionCalls(t *testing.T) {
	mod := CompileProgram(program(
		def("f", []string{"x"}, varRef("x"), true),
		def("g", []string{"a", "b"}, varRef("a"), true),
		def("h", nil,
			apply(varRef("g"), []ast.Expr{
				apply(varRef("f"), []ast.Expr{litInt(1)}),
				apply(varRef("f"), []ast.Expr{litInt(2)}),
			}), true),
	))
	w := findWord(mod, "h")
	callCount := opcodeCount(w.Code, OpCALL)
	if callCount != 2 {
		t.Fatalf("should have 2 CALL instructions, got %d", callCount)
	}
	if !hasOpcode(w.Code, OpTAIL_CALL) {
		t.Fatal("should have TAIL_CALL for outer g call")
	}
}

func TestEveryWordEndsWithRET(t *testing.T) {
	mod := CompileProgram(program(
		def("f", []string{"x"}, varRef("x"), true),
		def("g", nil, litInt(42), true),
	))
	for _, w := range mod.Words {
		if len(w.Code) > 0 && w.Code[len(w.Code)-1] != OpRET {
			t.Fatalf("%s should end with RET", w.Name)
		}
	}
}

func TestRecordFieldAccess(t *testing.T) {
	body := fieldAccess(varRef("r"), "name")
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"r"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpRECORD_GET) {
		t.Fatal("should emit RECORD_GET")
	}
	found := false
	for _, s := range mod.Strings {
		if s == "name" {
			found = true
		}
	}
	if !found {
		t.Fatal("string table should contain field name")
	}
}

func TestRecordUpdateSingleField(t *testing.T) {
	body := recordUpdate(varRef("r"), []ast.RecordUpdateField{
		{Name: "name", Value: litStr("bob")},
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"r"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpRECORD_SET) {
		t.Fatal("should emit RECORD_SET")
	}
	if !hasOpcode(w.Code, OpSWAP) {
		t.Fatal("should emit SWAP before RECORD_SET")
	}
}

func TestRecordUpdateMultipleFields(t *testing.T) {
	body := recordUpdate(varRef("r"), []ast.RecordUpdateField{
		{Name: "name", Value: litStr("bob")},
		{Name: "age", Value: litInt(30)},
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"r"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if opcodeCount(w.Code, OpRECORD_SET) != 2 {
		t.Fatal("should have 2 RECORD_SET ops")
	}
}

func TestRecordUpdateNoTrap(t *testing.T) {
	body := recordUpdate(varRef("r"), []ast.RecordUpdateField{
		{Name: "x", Value: litInt(1)},
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"r"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if hasOpcode(w.Code, OpTRAP) {
		t.Fatal("should NOT emit TRAP for record-update")
	}
}

func TestHandleEmitsHandlePushAndPop(t *testing.T) {
	body := handleExpr(
		apply(varRef("f"), []ast.Expr{varRef("x")}),
		[]ast.HandlerArm{
			handlerArm("return", []string{"r"}, "", varRef("r")),
			handlerArm("raise", []string{"e"}, "", litInt(0)),
		},
	)
	mod := CompileProgram(program(
		def("f", []string{"x"}, varRef("x"), true),
		ast.TopDefinition{Name: "g", Sig: sig([]string{"x"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "g")
	if !hasOpcode(w.Code, OpHANDLE_PUSH) {
		t.Fatal("should emit HANDLE_PUSH")
	}
	if !hasOpcode(w.Code, OpHANDLE_POP) {
		t.Fatal("should emit HANDLE_POP")
	}
	if hasOpcode(w.Code, OpTRAP) {
		t.Fatal("should NOT emit TRAP")
	}
}

func TestHandleNoTrap(t *testing.T) {
	body := handleExpr(litInt(1), []ast.HandlerArm{
		handlerArm("return", []string{"r"}, "", varRef("r")),
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if hasOpcode(w.Code, OpTRAP) {
		t.Fatal("handle should not emit TRAP")
	}
}

func TestHandleReturnBindsResult(t *testing.T) {
	body := handleExpr(litInt(42), []ast.HandlerArm{
		handlerArm("return", []string{"x"}, "",
			apply(varRef("add"), []ast.Expr{varRef("x"), litInt(1)})),
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpHANDLE_PUSH) {
		t.Fatal("should emit HANDLE_PUSH")
	}
	if !hasOpcode(w.Code, OpHANDLE_POP) {
		t.Fatal("should emit HANDLE_POP")
	}
	if !hasOpcode(w.Code, OpADD) {
		t.Fatal("return clause should compile x + 1")
	}
}

func TestHandleOpWithoutResumeDropsContinuation(t *testing.T) {
	body := handleExpr(litInt(1), []ast.HandlerArm{
		handlerArm("raise", []string{"e"}, "", litInt(0)),
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpDROP) {
		t.Fatal("should DROP continuation when no resumeName")
	}
}

func TestHandleOpWithResumeBindsContinuation(t *testing.T) {
	body := handleExpr(litInt(1), []ast.HandlerArm{
		handlerArm("raise", []string{"e"}, "k",
			apply(varRef("k"), []ast.Expr{litInt(0)})),
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpRESUME) {
		t.Fatal("should emit RESUME for k(0)")
	}
	if hasOpcode(w.Code, OpDROP) {
		t.Fatal("should NOT drop continuation when resume is used")
	}
}

func TestHandleBytecodeStructure(t *testing.T) {
	body := handleExpr(litInt(42), []ast.HandlerArm{
		handlerArm("return", []string{"r"}, "", varRef("r")),
		handlerArm("raise", []string{"e"}, "", litInt(0)),
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	pushIdx := indexOf(w.Code, OpHANDLE_PUSH)
	popIdx := indexOf(w.Code, OpHANDLE_POP)
	if pushIdx >= popIdx {
		t.Fatal("HANDLE_PUSH should come before HANDLE_POP")
	}
	// HANDLE_POP should come before JMP
	jmpIdx := -1
	for i := popIdx; i < len(w.Code); i++ {
		if w.Code[i] == OpJMP {
			jmpIdx = i
			break
		}
	}
	if popIdx >= jmpIdx {
		t.Fatal("HANDLE_POP should come before JMP to end")
	}
}

func TestPerformEmitsEffectPerform(t *testing.T) {
	body := performExpr(apply(varRef("raise"), []ast.Expr{varRef("e")}))
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"e"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpEFFECT_PERFORM) {
		t.Fatal("should emit EFFECT_PERFORM")
	}
	if hasOpcode(w.Code, OpTRAP) {
		t.Fatal("should NOT emit TRAP")
	}
}

func TestPerformCompilesArgsBefore(t *testing.T) {
	body := performExpr(apply(varRef("raise"), []ast.Expr{litInt(42)}))
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	pushIdx := indexOf(w.Code, OpPUSH_INT)
	perfIdx := indexOf(w.Code, OpEFFECT_PERFORM)
	if pushIdx >= perfIdx {
		t.Fatal("argument should be pushed before EFFECT_PERFORM")
	}
}

func TestPerformMultipleArgs(t *testing.T) {
	body := performExpr(apply(varRef("put"), []ast.Expr{varRef("k"), varRef("v")}))
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig([]string{"k", "v"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpEFFECT_PERFORM) {
		t.Fatal("should emit EFFECT_PERFORM")
	}
	perfIdx := indexOf(w.Code, OpEFFECT_PERFORM)
	gets := 0
	for i := 0; i < perfIdx; i++ {
		if w.Code[i] == OpLOCAL_GET {
			gets++
		}
	}
	if gets < 2 {
		t.Fatal("should push both arguments before EFFECT_PERFORM")
	}
}

func TestPerformInternsOperationName(t *testing.T) {
	body := performExpr(apply(varRef("raise"), []ast.Expr{litInt(0)}))
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	found := false
	for _, s := range mod.Strings {
		if s == "raise" {
			found = true
		}
	}
	if !found {
		t.Fatal("string table should contain operation name 'raise'")
	}
}

func TestResumeEmitsResumeOpcode(t *testing.T) {
	body := handleExpr(litInt(1), []ast.HandlerArm{
		handlerArm("ask", nil, "k",
			apply(varRef("k"), []ast.Expr{litInt(42)})),
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if !hasOpcode(w.Code, OpRESUME) {
		t.Fatal("should emit RESUME")
	}
	resumeIdx := indexOf(w.Code, OpRESUME)
	pushIdx := lastIndexOf(w.Code, OpPUSH_INT, resumeIdx)
	if pushIdx >= resumeIdx {
		t.Fatal("resume value should be pushed before RESUME")
	}
}

func TestHandlerWithoutResumeNoResume(t *testing.T) {
	body := handleExpr(litInt(1), []ast.HandlerArm{
		handlerArm("raise", []string{"e"}, "", litInt(0)),
	})
	mod := CompileProgram(program(
		ast.TopDefinition{Name: "f", Sig: sig(nil), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "f")
	if hasOpcode(w.Code, OpRESUME) {
		t.Fatal("should NOT emit RESUME when no resumeName")
	}
}

func TestSafeDivIntegration(t *testing.T) {
	body := handleExpr(
		apply(varRef("compute"), []ast.Expr{varRef("a"), varRef("b")}),
		[]ast.HandlerArm{
			handlerArm("return", []string{"x"}, "", varRef("x")),
			handlerArm("raise", []string{"_err"}, "", litInt(0)),
		},
	)
	mod := CompileProgram(program(
		def("compute", []string{"a", "b"}, varRef("a"), true),
		ast.TopDefinition{Name: "safe_div", Sig: sig([]string{"a", "b"}), Body: body, Pub: true, Loc: loc},
	))
	w := findWord(mod, "safe_div")
	if w == nil {
		t.Fatal("safe_div should exist")
	}
	if !hasOpcode(w.Code, OpHANDLE_PUSH) {
		t.Fatal("should have HANDLE_PUSH")
	}
	if !hasOpcode(w.Code, OpCALL) {
		t.Fatal("should CALL compute")
	}
	if !hasOpcode(w.Code, OpHANDLE_POP) {
		t.Fatal("should have HANDLE_POP")
	}
	if !hasOpcode(w.Code, OpDROP) {
		t.Fatal("should DROP continuation (no resume)")
	}
	if !hasOpcode(w.Code, OpRET) {
		t.Fatal("should have RET")
	}
	if hasOpcode(w.Code, OpTRAP) {
		t.Fatal("should have no TRAP")
	}
}
