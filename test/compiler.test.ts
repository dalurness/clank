// Unit tests for the bytecode compiler
// Run with: npx tsx test/compiler.test.ts

import { compileProgram, Op, type BytecodeModule } from "../src/compiler.js";
import { desugar } from "../src/desugar.js";
import type { Expr, Program, TopLevel, TypeSig, Loc } from "../src/ast.js";

const loc: Loc = { line: 1, col: 1 };

// Helpers to build AST nodes
const litInt = (n: number): Expr => ({ tag: "literal", value: { tag: "int", value: n }, loc });
const litBool = (v: boolean): Expr => ({ tag: "literal", value: { tag: "bool", value: v }, loc });
const litStr = (s: string): Expr => ({ tag: "literal", value: { tag: "str", value: s }, loc });
const litUnit = (): Expr => ({ tag: "literal", value: { tag: "unit" }, loc });
const varRef = (name: string): Expr => ({ tag: "var", name, loc });
const letExpr = (name: string, value: Expr, body: Expr | null): Expr => ({ tag: "let", name, value, body, loc });
const ifExpr = (cond: Expr, then: Expr, els: Expr): Expr => ({ tag: "if", cond, then, else: els, loc });
const apply = (fn: Expr, args: Expr[]): Expr => ({ tag: "apply", fn, args, loc });
const lambda = (params: string[], body: Expr): Expr => ({
  tag: "lambda",
  params: params.map(name => ({ name, type: null })),
  body,
  loc,
});
const infix = (op: string, left: Expr, right: Expr): Expr => ({ tag: "infix", op, left, right, loc });
const list = (elements: Expr[]): Expr => ({ tag: "list", elements, loc });
const tuple = (elements: Expr[]): Expr => ({ tag: "tuple", elements, loc });

function sig(paramNames: string[]): TypeSig {
  return {
    params: paramNames.map(name => ({ name, type: { tag: "t-name", name: "Int", loc } })),
    effects: [],
    returnType: { tag: "t-name", name: "Int", loc },
  };
}

function def(name: string, params: string[], body: Expr, pub = true): TopLevel {
  return { tag: "definition", name, sig: sig(params), body: desugar(body), pub, loc };
}

function program(...topLevels: TopLevel[]): Program {
  return { topLevels };
}

// Test runner
let passed = 0;
let failed = 0;

function test(name: string, fn: () => void): void {
  try {
    fn();
    passed++;
    console.log(`  ✓ ${name}`);
  } catch (e: any) {
    failed++;
    console.log(`  ✗ ${name}`);
    console.log(`    ${e.message}`);
  }
}

function assert(cond: boolean, msg: string): void {
  if (!cond) throw new Error(msg);
}

function assertEq<T>(a: T, b: T, msg = ""): void {
  if (a !== b) throw new Error(`${msg} Expected ${b}, got ${a}`);
}

function findWord(mod: BytecodeModule, name: string) {
  return mod.words.find(w => w.name === name);
}

function hasOpcode(code: number[], op: number): boolean {
  return code.includes(op);
}

function findOpcodeSequence(code: number[], seq: number[]): number {
  outer: for (let i = 0; i <= code.length - seq.length; i++) {
    for (let j = 0; j < seq.length; j++) {
      if (code[i + j] !== seq[j]) continue outer;
    }
    return i;
  }
  return -1;
}

// ── Tests ──

console.log("\nCompiler Tests\n");

console.log("Literals:");

test("integer literal (small)", () => {
  const mod = compileProgram(program(def("f", [], litInt(42))));
  const word = findWord(mod, "f")!;
  assert(word !== undefined, "word 'f' not found");
  assert(hasOpcode(word.code, Op.PUSH_INT), "should emit PUSH_INT");
  // PUSH_INT 42 should have 42 as the operand
  const idx = word.code.indexOf(Op.PUSH_INT);
  assertEq(word.code[idx + 1], 42, "operand");
});

test("integer literal (u16)", () => {
  const mod = compileProgram(program(def("f", [], litInt(300))));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.PUSH_INT16), "should emit PUSH_INT16");
});

test("integer literal (u32)", () => {
  const mod = compileProgram(program(def("f", [], litInt(100000))));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.PUSH_INT32), "should emit PUSH_INT32");
});

test("boolean true", () => {
  const mod = compileProgram(program(def("f", [], litBool(true))));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.PUSH_TRUE), "should emit PUSH_TRUE");
});

test("boolean false", () => {
  const mod = compileProgram(program(def("f", [], litBool(false))));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.PUSH_FALSE), "should emit PUSH_FALSE");
});

test("string literal", () => {
  const mod = compileProgram(program(def("f", [], litStr("hello"))));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.PUSH_STR), "should emit PUSH_STR");
  assert(mod.strings.includes("hello"), "string table should contain 'hello'");
});

test("unit literal", () => {
  const mod = compileProgram(program(def("f", [], litUnit())));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.PUSH_UNIT), "should emit PUSH_UNIT");
});

console.log("\nLet bindings:");

test("let binding", () => {
  // let x = 5 in x
  const mod = compileProgram(program(
    def("f", [], letExpr("x", litInt(5), varRef("x")))
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.LOCAL_SET), "should emit LOCAL_SET");
  assert(hasOpcode(word.code, Op.LOCAL_GET), "should emit LOCAL_GET");
});

test("nested let bindings", () => {
  // let x = 1 in let y = 2 in x
  const mod = compileProgram(program(
    def("f", [], letExpr("x", litInt(1), letExpr("y", litInt(2), varRef("x"))))
  ));
  const word = findWord(mod, "f")!;
  // Should have two LOCAL_SET ops (for x and y)
  const setCount = word.code.filter(op => op === Op.LOCAL_SET).length;
  assertEq(setCount, 2, "LOCAL_SET count");
});

console.log("\nIf/then/else:");

test("if/then/else", () => {
  // if true then 1 else 2
  const mod = compileProgram(program(
    def("f", [], ifExpr(litBool(true), litInt(1), litInt(2)))
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.JMP_UNLESS), "should emit JMP_UNLESS");
  assert(hasOpcode(word.code, Op.JMP), "should emit JMP");
});

console.log("\nArithmetic (builtin ops):");

test("addition desugars to ADD opcode", () => {
  // a + b where a, b are params
  const body = desugar(infix("+", varRef("a"), varRef("b")));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["a", "b"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.ADD), "should emit ADD");
  assert(!hasOpcode(word.code, Op.CALL), "should NOT emit CALL for builtin");
});

test("subtraction", () => {
  const body = desugar(infix("-", varRef("a"), varRef("b")));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["a", "b"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.SUB), "should emit SUB");
});

test("multiplication", () => {
  const body = desugar(infix("*", varRef("a"), varRef("b")));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["a", "b"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.MUL), "should emit MUL");
});

test("comparison operators", () => {
  for (const [op, expected] of [
    ["==", Op.EQ], ["!=", Op.NEQ], ["<", Op.LT],
    [">", Op.GT], ["<=", Op.LTE], [">=", Op.GTE],
  ] as [string, number][]) {
    const body = desugar(infix(op, varRef("a"), varRef("b")));
    const mod = compileProgram(program(
      { tag: "definition", name: "f", sig: sig(["a", "b"]), body, pub: true, loc }
    ));
    const word = findWord(mod, "f")!;
    assert(hasOpcode(word.code, expected), `should emit opcode for ${op}`);
  }
});

console.log("\nFunction calls:");

test("call known function", () => {
  // g calls f(1) — in tail position, so TAIL_CALL
  const mod = compileProgram(program(
    def("f", ["x"], varRef("x")),
    def("g", [], apply(varRef("f"), [litInt(1)])),
  ));
  const word = findWord(mod, "g")!;
  assert(hasOpcode(word.code, Op.TAIL_CALL), "should emit TAIL_CALL (tail position)");
  assert(hasOpcode(word.code, Op.PUSH_INT), "should push arg");
});

test("call known function (non-tail)", () => {
  // g calls f(1) + 1 — not in tail position
  const body = desugar(infix("+", apply(varRef("f"), [litInt(1)]), litInt(1)));
  const mod = compileProgram(program(
    def("f", ["x"], varRef("x")),
    { tag: "definition", name: "g", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "g")!;
  assert(hasOpcode(word.code, Op.CALL), "should emit CALL (non-tail)");
});

test("tail call in if branch", () => {
  // f(n) = if n == 0 then 0 else f(n - 1)
  const body = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litInt(0),
    apply(varRef("f"), [infix("-", varRef("n"), litInt(1))]),
  ));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["n"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.TAIL_CALL), "should emit TAIL_CALL for recursive call in tail position");
});

console.log("\nFunction parameters:");

test("function parameters bound correctly", () => {
  // f(a, b) = a
  const mod = compileProgram(program(def("f", ["a", "b"], varRef("a"))));
  const word = findWord(mod, "f")!;
  // Prologue: LOCAL_SET 1 (b), LOCAL_SET 0 (a) — reverse order
  assertEq(word.code[0], Op.LOCAL_SET, "first op should be LOCAL_SET");
  assertEq(word.code[1], 1, "b popped first → slot 1");
  assertEq(word.code[2], Op.LOCAL_SET, "second op should be LOCAL_SET");
  assertEq(word.code[3], 0, "a popped second → slot 0");
});

console.log("\nLambdas:");

test("lambda with no captures uses QUOTE", () => {
  // let f = fn(x) => x in f
  const body = letExpr("f", lambda(["x"], varRef("x")), varRef("f"));
  const mod = compileProgram(program(def("g", [], body)));
  const word = findWord(mod, "g")!;
  assert(hasOpcode(word.code, Op.QUOTE), "should emit QUOTE for no-capture lambda");
  // Lambda body should be a separate word
  assert(mod.words.length >= 2, "should have lambda body as separate word");
});

test("lambda with captures uses CLOSURE", () => {
  // f(n) = let g = fn(x) => x + n in g
  const body = letExpr("g",
    lambda(["x"], desugar(infix("+", varRef("x"), varRef("n")))),
    varRef("g"),
  );
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["n"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.CLOSURE), "should emit CLOSURE for capturing lambda");
  assert(hasOpcode(word.code, Op.LOCAL_GET), "should push captured var");
});

console.log("\nLists and tuples:");

test("list literal", () => {
  const mod = compileProgram(program(
    def("f", [], list([litInt(1), litInt(2), litInt(3)]))
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.LIST_NEW), "should emit LIST_NEW");
  const idx = word.code.indexOf(Op.LIST_NEW);
  assertEq(word.code[idx + 1], 3, "list count should be 3");
});

test("tuple literal", () => {
  const mod = compileProgram(program(
    def("f", [], tuple([litInt(1), litStr("hi")]))
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.TUPLE_NEW), "should emit TUPLE_NEW");
  const idx = word.code.indexOf(Op.TUPLE_NEW);
  assertEq(word.code[idx + 1], 2, "tuple arity should be 2");
});

console.log("\nShort-circuit desugaring:");

test("&& desugars to if/then/else", () => {
  // a && b → if a then b else false
  const body = desugar(infix("&&", varRef("a"), varRef("b")));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["a", "b"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.JMP_UNLESS), "should use conditional jump");
  assert(hasOpcode(word.code, Op.PUSH_FALSE), "should push false for else branch");
});

test("|| desugars to if/then/else", () => {
  const body = desugar(infix("||", varRef("a"), varRef("b")));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["a", "b"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.JMP_UNLESS), "should use conditional jump");
  assert(hasOpcode(word.code, Op.PUSH_TRUE), "should push true for then branch");
});

console.log("\nString table:");

test("string deduplication", () => {
  // Two uses of the same string
  const body = letExpr("a", litStr("hello"),
    letExpr("b", litStr("hello"), varRef("a")));
  const mod = compileProgram(program(def("f", [], body)));
  const count = mod.strings.filter(s => s === "hello").length;
  assertEq(count, 1, "string should appear only once in table");
});

console.log("\nModule structure:");

test("entry word ID for main", () => {
  const mod = compileProgram(program(def("main", [], litUnit())));
  assert(mod.entryWordId !== null, "should have entry word ID for main");
});

test("no entry word ID without main", () => {
  const mod = compileProgram(program(def("f", [], litUnit())));
  assertEq(mod.entryWordId, null, "should be null without main");
});

test("word IDs start at 256", () => {
  const mod = compileProgram(program(def("f", [], litUnit())));
  const word = findWord(mod, "f")!;
  assert(word.wordId >= 256, "user word IDs should start at 256");
});

console.log("\nComplex expressions:");

test("factorial-like recursion", () => {
  // factorial(n) = if n == 0 then 1 else n * factorial(n - 1)
  const body = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litInt(1),
    infix("*", varRef("n"), apply(varRef("factorial"), [infix("-", varRef("n"), litInt(1))])),
  ));
  const mod = compileProgram(program(
    { tag: "definition", name: "factorial", sig: sig(["n"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "factorial")!;
  assert(word !== undefined, "factorial word should exist");
  assert(hasOpcode(word.code, Op.EQ), "should test equality");
  assert(hasOpcode(word.code, Op.JMP_UNLESS), "should have conditional");
  assert(hasOpcode(word.code, Op.SUB), "should subtract");
  assert(hasOpcode(word.code, Op.MUL), "should multiply");
  assert(hasOpcode(word.code, Op.CALL), "should call recursively");
  assert(hasOpcode(word.code, Op.RET), "should return");
});

test("nested function calls", () => {
  // g(f(1), f(2)) — outer call is tail, inner calls are not
  const mod = compileProgram(program(
    def("f", ["x"], varRef("x")),
    def("g", ["a", "b"], varRef("a")),
    def("h", [],
      apply(varRef("g"), [apply(varRef("f"), [litInt(1)]), apply(varRef("f"), [litInt(2)])])
    ),
  ));
  const word = findWord(mod, "h")!;
  // Two CALL to f (non-tail), one TAIL_CALL to g (tail position)
  const callCount = word.code.filter(op => op === Op.CALL).length;
  assertEq(callCount, 2, "should have 2 CALL instructions (f calls)");
  assert(hasOpcode(word.code, Op.TAIL_CALL), "should have TAIL_CALL for outer g call");
});

test("every word ends with RET", () => {
  const mod = compileProgram(program(
    def("f", ["x"], varRef("x")),
    def("g", [], litInt(42)),
  ));
  for (const word of mod.words) {
    assertEq(word.code[word.code.length - 1], Op.RET, `${word.name} should end with RET`);
  }
});

console.log("\nField access:");

test("record field access", () => {
  // r.name where r is a param
  const body: Expr = { tag: "field-access", object: varRef("r"), field: "name", loc };
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["r"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.RECORD_GET), "should emit RECORD_GET");
  assert(mod.strings.includes("name"), "string table should contain field name");
});

console.log("\nRecord update:");

const record = (fields: { name: string; value: Expr }[], spread: Expr | null = null): Expr => ({ tag: "record", fields, spread, loc });
const recordUpdate = (base: Expr, fields: { name: string; value: Expr }[]): Expr => ({
  tag: "record-update", base, fields, loc,
});

test("record update single field", () => {
  // { r | name = "bob" } where r is a param
  const body = recordUpdate(varRef("r"), [{ name: "name", value: litStr("bob") }]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["r"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.RECORD_SET), "should emit RECORD_SET");
  assert(hasOpcode(word.code, Op.SWAP), "should emit SWAP before RECORD_SET");
  assert(mod.strings.includes("name"), "string table should contain field name");
});

test("record update multiple fields", () => {
  // { r | name = "bob", age = 30 }
  const body = recordUpdate(varRef("r"), [
    { name: "name", value: litStr("bob") },
    { name: "age", value: litInt(30) },
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["r"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  // Should have two RECORD_SET ops (one per field)
  const setCount = word.code.filter(op => op === Op.RECORD_SET).length;
  assertEq(setCount, 2, "RECORD_SET count");
  assert(hasOpcode(word.code, Op.SWAP), "should emit SWAP");
  assert(mod.strings.includes("name"), "string table should contain 'name'");
  assert(mod.strings.includes("age"), "string table should contain 'age'");
});

test("record update with computed value", () => {
  // { r | x = a + b }
  const body = recordUpdate(varRef("r"), [
    { name: "x", value: desugar(infix("+", varRef("a"), varRef("b"))) },
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["r", "a", "b"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.ADD), "should compile the value expression");
  assert(hasOpcode(word.code, Op.SWAP), "should emit SWAP");
  assert(hasOpcode(word.code, Op.RECORD_SET), "should emit RECORD_SET");
});

test("record update does not emit TRAP", () => {
  const body = recordUpdate(varRef("r"), [{ name: "x", value: litInt(1) }]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["r"]), body, pub: true, loc }
  ));
  const word = findWord(mod, "f")!;
  assert(!hasOpcode(word.code, Op.TRAP), "should NOT emit TRAP for record-update");
});

// ── Effect handler helpers ──

import type { HandlerArm, Param } from "../src/ast.js";

function handlerArm(name: string, params: string[], resumeName: string | null, body: Expr): HandlerArm {
  return {
    name,
    params: params.map(n => ({ name: n, type: null } as Param)),
    resumeName,
    body,
  };
}

const handle = (expr: Expr, arms: HandlerArm[]): Expr => ({ tag: "handle", expr, arms, loc });
const perform = (expr: Expr): Expr => ({ tag: "perform", expr, loc });

console.log("\nEffect handlers:");

test("handle emits HANDLE_PUSH and HANDLE_POP", () => {
  // handle f(x) { return(r) => r, raise(e) => 0 }
  const body = handle(
    apply(varRef("f"), [varRef("x")]),
    [
      handlerArm("return", ["r"], null, varRef("r")),
      handlerArm("raise", ["e"], null, litInt(0)),
    ],
  );
  const mod = compileProgram(program(
    def("f", ["x"], varRef("x")),
    { tag: "definition", name: "g", sig: sig(["x"]), body, pub: true, loc },
  ));
  const word = findWord(mod, "g")!;
  assert(hasOpcode(word.code, Op.HANDLE_PUSH), "should emit HANDLE_PUSH");
  assert(hasOpcode(word.code, Op.HANDLE_POP), "should emit HANDLE_POP");
  assert(!hasOpcode(word.code, Op.TRAP), "should NOT emit TRAP");
});

test("handle does not emit TRAP", () => {
  const body = handle(litInt(1), [
    handlerArm("return", ["r"], null, varRef("r")),
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  assert(!hasOpcode(word.code, Op.TRAP), "handle should not emit TRAP");
});

test("handle with return clause binds result", () => {
  // handle expr { return(x) => x + 1 }
  const body = handle(litInt(42), [
    handlerArm("return", ["x"], null, desugar(infix("+", varRef("x"), litInt(1)))),
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.HANDLE_PUSH), "should emit HANDLE_PUSH");
  assert(hasOpcode(word.code, Op.HANDLE_POP), "should emit HANDLE_POP");
  assert(hasOpcode(word.code, Op.ADD), "return clause should compile x + 1");
});

test("handle operation arm without resume emits DROP for continuation", () => {
  // handle expr { raise(e) => 0 } — no resume, so continuation is dropped
  const body = handle(litInt(1), [
    handlerArm("raise", ["e"], null, litInt(0)),
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  // The handler clause should have DROP (discard continuation)
  assert(hasOpcode(word.code, Op.DROP), "should DROP continuation when no resumeName");
});

test("handle operation arm with resume binds continuation", () => {
  // handle expr { raise(e) resume k => k(0) }
  const body = handle(litInt(1), [
    handlerArm("raise", ["e"], "k", apply(varRef("k"), [litInt(0)])),
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.RESUME), "should emit RESUME for k(0)");
  assert(!hasOpcode(word.code, Op.DROP), "should NOT drop continuation when resume is used");
});

test("handle bytecode structure: PUSH → body → POP → return → JMP → handlers", () => {
  const body = handle(
    litInt(42),
    [
      handlerArm("return", ["r"], null, varRef("r")),
      handlerArm("raise", ["e"], null, litInt(0)),
    ],
  );
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;

  // Verify ordering: HANDLE_PUSH comes before HANDLE_POP
  const pushIdx = word.code.indexOf(Op.HANDLE_PUSH);
  const popIdx = word.code.indexOf(Op.HANDLE_POP);
  assert(pushIdx < popIdx, "HANDLE_PUSH should come before HANDLE_POP");

  // HANDLE_POP should come before JMP (skip handler code)
  const jmpIdx = word.code.indexOf(Op.JMP, popIdx);
  assert(popIdx < jmpIdx, "HANDLE_POP should come before JMP to end");
});

console.log("\nPerform:");

test("perform emits EFFECT_PERFORM", () => {
  // perform raise(e) where e is a param
  const body = perform(apply(varRef("raise"), [varRef("e")]));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["e"]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.EFFECT_PERFORM), "should emit EFFECT_PERFORM");
  assert(!hasOpcode(word.code, Op.TRAP), "should NOT emit TRAP");
});

test("perform compiles arguments before EFFECT_PERFORM", () => {
  // perform raise(42)
  const body = perform(apply(varRef("raise"), [litInt(42)]));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  const pushIdx = word.code.indexOf(Op.PUSH_INT);
  const perfIdx = word.code.indexOf(Op.EFFECT_PERFORM);
  assert(pushIdx < perfIdx, "argument should be pushed before EFFECT_PERFORM");
});

test("perform with multiple arguments", () => {
  // perform put(key, value)
  const body = perform(apply(varRef("put"), [varRef("k"), varRef("v")]));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig(["k", "v"]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.EFFECT_PERFORM), "should emit EFFECT_PERFORM");
  // Should have LOCAL_GET for both k and v before EFFECT_PERFORM
  const perfIdx = word.code.indexOf(Op.EFFECT_PERFORM);
  const gets = word.code.slice(0, perfIdx).filter(op => op === Op.LOCAL_GET).length;
  assert(gets >= 2, "should push both arguments before EFFECT_PERFORM");
});

test("perform interns operation name as effect ID", () => {
  const body = perform(apply(varRef("raise"), [litInt(0)]));
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  assert(mod.strings.includes("raise"), "string table should contain operation name 'raise'");
});

console.log("\nResume:");

test("resume in handler body emits RESUME opcode", () => {
  // handle expr { ask() resume k => k(42) }
  const body = handle(litInt(1), [
    handlerArm("ask", [], "k", apply(varRef("k"), [litInt(42)])),
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  assert(hasOpcode(word.code, Op.RESUME), "should emit RESUME");
  // The resume value (42) should be pushed before RESUME
  const resumeIdx = word.code.indexOf(Op.RESUME);
  const pushIdx = word.code.lastIndexOf(Op.PUSH_INT, resumeIdx);
  assert(pushIdx < resumeIdx, "resume value should be pushed before RESUME");
});

test("handler without resume does not emit RESUME", () => {
  // handle expr { raise(e) => 0 }  — no resume
  const body = handle(litInt(1), [
    handlerArm("raise", ["e"], null, litInt(0)),
  ]);
  const mod = compileProgram(program(
    { tag: "definition", name: "f", sig: sig([]), body, pub: true, loc },
  ));
  const word = findWord(mod, "f")!;
  assert(!hasOpcode(word.code, Op.RESUME), "should NOT emit RESUME when no resumeName");
});

console.log("\nIntegration — safe-div example:");

test("safe-div compiles with full handler structure", () => {
  // handle compute(a, b) {
  //   return(x) => x    (pass through)
  //   raise(_) => 0     (default on error)
  // }
  const body = handle(
    apply(varRef("compute"), [varRef("a"), varRef("b")]),
    [
      handlerArm("return", ["x"], null, varRef("x")),
      handlerArm("raise", ["_err"], null, litInt(0)),
    ],
  );
  const mod = compileProgram(program(
    def("compute", ["a", "b"], varRef("a")),
    { tag: "definition", name: "safe_div", sig: sig(["a", "b"]), body, pub: true, loc },
  ));
  const word = findWord(mod, "safe_div")!;
  assert(word !== undefined, "safe_div should exist");
  assert(hasOpcode(word.code, Op.HANDLE_PUSH), "should have HANDLE_PUSH");
  assert(hasOpcode(word.code, Op.CALL), "should CALL compute");
  assert(hasOpcode(word.code, Op.HANDLE_POP), "should have HANDLE_POP");
  assert(hasOpcode(word.code, Op.DROP), "should DROP continuation (no resume)");
  assert(hasOpcode(word.code, Op.RET), "should have RET");
  assert(!hasOpcode(word.code, Op.TRAP), "should have no TRAP");
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
