// Integration tests: compile → execute pipeline
// Run with: npx tsx test/vm.test.ts

import { compileProgram, Op, type BytecodeModule } from "../src/compiler.js";
import { VM, execute, Val, Tag, type Value } from "../src/vm.js";
import { desugar } from "../src/desugar.js";
import type { Expr, Program, TopLevel, TypeSig, Loc } from "../src/ast.js";

const loc: Loc = { line: 1, col: 1 };

// ── AST helpers (same as compiler.test.ts) ──

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

// ── Test runner ──

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
  if (a !== b) throw new Error(`${msg} Expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
}

function compileAndRun(...topLevels: TopLevel[]): { result: Value | undefined; stdout: string[] } {
  const mod = compileProgram(program(...topLevels));
  return execute(mod);
}

function runMain(params: string[], body: Expr): Value | undefined {
  return compileAndRun(def("main", params, body)).result;
}

function assertIntResult(result: Value | undefined, expected: number, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.INT, `${msg} expected Int, got tag ${result!.tag}`);
  assertEq((result as any).value, expected, msg);
}

function assertBoolResult(result: Value | undefined, expected: boolean, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.BOOL, `${msg} expected Bool, got tag ${result!.tag}`);
  assertEq((result as any).value, expected, msg);
}

function assertStrResult(result: Value | undefined, expected: string, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.STR, `${msg} expected Str, got tag ${result!.tag}`);
  assertEq((result as any).value, expected, msg);
}

// ── Tests ──

console.log("\nVM Integration Tests\n");

console.log("Literals:");

test("integer literal", () => {
  assertIntResult(runMain([], litInt(42)), 42);
});

test("boolean true", () => {
  assertBoolResult(runMain([], litBool(true)), true);
});

test("boolean false", () => {
  assertBoolResult(runMain([], litBool(false)), false);
});

test("string literal", () => {
  assertStrResult(runMain([], litStr("hello")), "hello");
});

test("unit literal", () => {
  const result = runMain([], litUnit());
  assert(result !== undefined, "result is undefined");
  assertEq(result!.tag, Tag.UNIT, "expected Unit");
});

test("large integer (u16)", () => {
  assertIntResult(runMain([], litInt(300)), 300);
});

test("large integer (u32)", () => {
  assertIntResult(runMain([], litInt(100000)), 100000);
});

console.log("\nArithmetic:");

test("addition", () => {
  const body = desugar(infix("+", litInt(3), litInt(4)));
  assertIntResult(runMain([], body), 7);
});

test("subtraction", () => {
  const body = desugar(infix("-", litInt(10), litInt(3)));
  assertIntResult(runMain([], body), 7);
});

test("multiplication", () => {
  const body = desugar(infix("*", litInt(6), litInt(7)));
  assertIntResult(runMain([], body), 42);
});

test("division (even)", () => {
  const body = desugar(infix("/", litInt(10), litInt(2)));
  assertIntResult(runMain([], body), 5);
});

test("modulo", () => {
  const body = desugar(infix("%", litInt(10), litInt(3)));
  assertIntResult(runMain([], body), 1);
});

test("nested arithmetic: (2 + 3) * 4", () => {
  const body = desugar(infix("*", infix("+", litInt(2), litInt(3)), litInt(4)));
  assertIntResult(runMain([], body), 20);
});

console.log("\nComparison:");

test("equality true", () => {
  const body = desugar(infix("==", litInt(5), litInt(5)));
  assertBoolResult(runMain([], body), true);
});

test("equality false", () => {
  const body = desugar(infix("==", litInt(5), litInt(3)));
  assertBoolResult(runMain([], body), false);
});

test("less than", () => {
  const body = desugar(infix("<", litInt(3), litInt(5)));
  assertBoolResult(runMain([], body), true);
});

test("greater than", () => {
  const body = desugar(infix(">", litInt(5), litInt(3)));
  assertBoolResult(runMain([], body), true);
});

console.log("\nLet bindings:");

test("simple let", () => {
  // let x = 10 in x
  assertIntResult(runMain([], letExpr("x", litInt(10), varRef("x"))), 10);
});

test("let with arithmetic", () => {
  // let x = 5 in let y = 3 in x + y
  const body = letExpr("x", litInt(5),
    letExpr("y", litInt(3),
      desugar(infix("+", varRef("x"), varRef("y")))));
  assertIntResult(runMain([], body), 8);
});

console.log("\nIf/then/else:");

test("if true", () => {
  assertIntResult(runMain([], ifExpr(litBool(true), litInt(1), litInt(2))), 1);
});

test("if false", () => {
  assertIntResult(runMain([], ifExpr(litBool(false), litInt(1), litInt(2))), 2);
});

test("if with condition expression", () => {
  // if 5 == 5 then 10 else 20
  const body = ifExpr(desugar(infix("==", litInt(5), litInt(5))), litInt(10), litInt(20));
  assertIntResult(runMain([], body), 10);
});

console.log("\nFunction calls:");

test("call another function", () => {
  // double(x) = x + x
  // main() = double(21)
  const { result } = compileAndRun(
    def("double", ["x"], desugar(infix("+", varRef("x"), varRef("x")))),
    def("main", [], apply(varRef("double"), [litInt(21)])),
  );
  assertIntResult(result, 42);
});

test("function with two params", () => {
  // add(a, b) = a + b
  // main() = add(10, 32)
  const { result } = compileAndRun(
    def("add-fn", ["a", "b"], desugar(infix("+", varRef("a"), varRef("b")))),
    def("main", [], apply(varRef("add-fn"), [litInt(10), litInt(32)])),
  );
  assertIntResult(result, 42);
});

test("nested function calls", () => {
  // inc(x) = x + 1
  // main() = inc(inc(inc(0)))
  const { result } = compileAndRun(
    def("inc", ["x"], desugar(infix("+", varRef("x"), litInt(1)))),
    def("main", [], apply(varRef("inc"), [apply(varRef("inc"), [apply(varRef("inc"), [litInt(0)])])])),
  );
  assertIntResult(result, 3);
});

console.log("\nRecursion:");

test("factorial(5) = 120", () => {
  // factorial(n) = if n == 0 then 1 else n * factorial(n - 1)
  const body = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litInt(1),
    infix("*", varRef("n"), apply(varRef("factorial"), [infix("-", varRef("n"), litInt(1))])),
  ));
  const { result } = compileAndRun(
    { tag: "definition", name: "factorial", sig: sig(["n"]), body, pub: true, loc },
    def("main", [], apply(varRef("factorial"), [litInt(5)])),
  );
  assertIntResult(result, 120);
});

test("factorial(0) = 1", () => {
  const body = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litInt(1),
    infix("*", varRef("n"), apply(varRef("factorial"), [infix("-", varRef("n"), litInt(1))])),
  ));
  const { result } = compileAndRun(
    { tag: "definition", name: "factorial", sig: sig(["n"]), body, pub: true, loc },
    def("main", [], apply(varRef("factorial"), [litInt(0)])),
  );
  assertIntResult(result, 1);
});

test("factorial(10) = 3628800", () => {
  const body = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litInt(1),
    infix("*", varRef("n"), apply(varRef("factorial"), [infix("-", varRef("n"), litInt(1))])),
  ));
  const { result } = compileAndRun(
    { tag: "definition", name: "factorial", sig: sig(["n"]), body, pub: true, loc },
    def("main", [], apply(varRef("factorial"), [litInt(10)])),
  );
  assertIntResult(result, 3628800);
});

console.log("\nTail recursion:");

test("tail-recursive sum", () => {
  // sum(n, acc) = if n == 0 then acc else sum(n - 1, acc + n)
  const sumSig: TypeSig = {
    params: [
      { name: "n", type: { tag: "t-name", name: "Int", loc } },
      { name: "acc", type: { tag: "t-name", name: "Int", loc } },
    ],
    effects: [],
    returnType: { tag: "t-name", name: "Int", loc },
  };
  const sumBody = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    varRef("acc"),
    apply(varRef("sum"), [
      infix("-", varRef("n"), litInt(1)),
      infix("+", varRef("acc"), varRef("n")),
    ]),
  ));
  const { result } = compileAndRun(
    { tag: "definition", name: "sum", sig: sumSig, body: sumBody, pub: true, loc },
    def("main", [], apply(varRef("sum"), [litInt(100), litInt(0)])),
  );
  // sum(100) = 5050
  assertIntResult(result, 5050);
});

test("deep tail recursion (1000 iterations)", () => {
  // countdown(n) = if n == 0 then 0 else countdown(n - 1)
  const body = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litInt(0),
    apply(varRef("countdown"), [infix("-", varRef("n"), litInt(1))]),
  ));
  const { result } = compileAndRun(
    { tag: "definition", name: "countdown", sig: sig(["n"]), body, pub: true, loc },
    def("main", [], apply(varRef("countdown"), [litInt(1000)])),
  );
  assertIntResult(result, 0);
});

console.log("\nLambda / higher-order:");

test("lambda with no captures (QUOTE)", () => {
  // main() = let f = fn(x) => x + 1 in f(41)
  const body = letExpr("f",
    lambda(["x"], desugar(infix("+", varRef("x"), litInt(1)))),
    apply(varRef("f"), [litInt(41)]),
  );
  assertIntResult(runMain([], body), 42);
});

test("lambda with capture (CLOSURE)", () => {
  // main() = let n = 10 in let f = fn(x) => x + n in f(32)
  const body = letExpr("n", litInt(10),
    letExpr("f",
      lambda(["x"], desugar(infix("+", varRef("x"), varRef("n")))),
      apply(varRef("f"), [litInt(32)]),
    ),
  );
  assertIntResult(runMain([], body), 42);
});

test("closure returned from function", () => {
  // make-adder(n) = fn(x) => x + n
  // main() = let add5 = make-adder(5) in add5(37)
  const { result } = compileAndRun(
    def("make-adder", ["n"],
      lambda(["x"], desugar(infix("+", varRef("x"), varRef("n")))),
    ),
    def("main", [],
      letExpr("add5",
        apply(varRef("make-adder"), [litInt(5)]),
        apply(varRef("add5"), [litInt(37)]),
      ),
    ),
  );
  assertIntResult(result, 42);
});

console.log("\nList operations:");

test("list literal", () => {
  const result = runMain([], list([litInt(1), litInt(2), litInt(3)]));
  assert(result !== undefined, "result is undefined");
  assert(result!.tag === Tag.HEAP, "expected HEAP");
  const obj = (result as any).value;
  assertEq(obj.kind, "list", "expected list");
  assertEq(obj.items.length, 3, "list length");
});

test("tuple literal", () => {
  const result = runMain([], tuple([litInt(1), litStr("hi")]));
  assert(result !== undefined, "result is undefined");
  assert(result!.tag === Tag.HEAP, "expected HEAP");
  const obj = (result as any).value;
  assertEq(obj.kind, "tuple", "expected tuple");
  assertEq(obj.items.length, 2, "tuple arity");
});

console.log("\nShort-circuit logic:");

test("&& true true", () => {
  const body = desugar(infix("&&", litBool(true), litBool(true)));
  assertBoolResult(runMain([], body), true);
});

test("&& true false", () => {
  const body = desugar(infix("&&", litBool(true), litBool(false)));
  assertBoolResult(runMain([], body), false);
});

test("&& false (short-circuits)", () => {
  const body = desugar(infix("&&", litBool(false), litBool(true)));
  assertBoolResult(runMain([], body), false);
});

test("|| false true", () => {
  const body = desugar(infix("||", litBool(false), litBool(true)));
  assertBoolResult(runMain([], body), true);
});

test("|| true (short-circuits)", () => {
  const body = desugar(infix("||", litBool(true), litBool(false)));
  assertBoolResult(runMain([], body), true);
});

console.log("\nI/O:");

test("IO_PRINT captures output", () => {
  // main() = print("hello world")
  // print is not a known word, so we need to use IO_PRINT directly.
  // Actually the compiler doesn't know about IO_PRINT as a word.
  // Let's build a module manually to test IO_PRINT.
  const mod: BytecodeModule = {
    words: [{
      name: "main",
      wordId: 256,
      code: [
        Op.PUSH_STR, 0, 0,  // push string at index 0
        Op.IO_PRINT,
        Op.RET,
      ],
      localCount: 0,
      isPublic: true,
    }],
    strings: ["hello world"],
    rationals: [],
    entryWordId: 256,
  };
  const { stdout } = execute(mod);
  assertEq(stdout.length, 1, "stdout length");
  assertEq(stdout[0], "hello world", "stdout content");
});

console.log("\nFibonacci:");

test("fibonacci(10) = 55", () => {
  // fib(n) = if n <= 1 then n else fib(n-1) + fib(n-2)
  const body = desugar(ifExpr(
    infix("<=", varRef("n"), litInt(1)),
    varRef("n"),
    infix("+",
      apply(varRef("fib"), [infix("-", varRef("n"), litInt(1))]),
      apply(varRef("fib"), [infix("-", varRef("n"), litInt(2))]),
    ),
  ));
  const { result } = compileAndRun(
    { tag: "definition", name: "fib", sig: sig(["n"]), body, pub: true, loc },
    def("main", [], apply(varRef("fib"), [litInt(10)])),
  );
  assertIntResult(result, 55);
});

console.log("\nMutual recursion:");

test("is-even / is-odd", () => {
  // is-even(n) = if n == 0 then true else is-odd(n - 1)
  // is-odd(n) = if n == 0 then false else is-even(n - 1)
  const evenBody = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litBool(true),
    apply(varRef("is-odd"), [infix("-", varRef("n"), litInt(1))]),
  ));
  const oddBody = desugar(ifExpr(
    infix("==", varRef("n"), litInt(0)),
    litBool(false),
    apply(varRef("is-even"), [infix("-", varRef("n"), litInt(1))]),
  ));
  const { result } = compileAndRun(
    { tag: "definition", name: "is-even", sig: sig(["n"]), body: evenBody, pub: true, loc },
    { tag: "definition", name: "is-odd", sig: sig(["n"]), body: oddBody, pub: true, loc },
    def("main", [], apply(varRef("is-even"), [litInt(10)])),
  );
  assertBoolResult(result, true);
});

console.log("\nString operations:");

test("show (TO_STR) on integer", () => {
  // show(42) → "42"
  const body = apply(varRef("show"), [litInt(42)]);
  // show is a builtin that maps to TO_STR
  assertStrResult(runMain([], body), "42");
});

test("string concatenation", () => {
  const body = desugar(infix("++", litStr("hello "), litStr("world")));
  assertStrResult(runMain([], body), "hello world");
});

console.log("\nEdge cases:");

test("zero argument function", () => {
  // f() = 42; main() = f()
  const { result } = compileAndRun(
    def("f", [], litInt(42)),
    def("main", [], apply(varRef("f"), [])),
  );
  assertIntResult(result, 42);
});

test("deeply nested if", () => {
  // if true then if false then 1 else if true then 2 else 3 else 4
  const body = ifExpr(litBool(true),
    ifExpr(litBool(false), litInt(1),
      ifExpr(litBool(true), litInt(2), litInt(3))),
    litInt(4));
  assertIntResult(runMain([], body), 2);
});

test("multiple let bindings", () => {
  // let a = 1 in let b = 2 in let c = 3 in a + b + c
  const body = letExpr("a", litInt(1),
    letExpr("b", litInt(2),
      letExpr("c", litInt(3),
        desugar(infix("+", infix("+", varRef("a"), varRef("b")), varRef("c"))))));
  assertIntResult(runMain([], body), 6);
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

// ── Effect handler tests ──

console.log("\nEffect handlers — return clause only:");

test("handle with return clause passes through value", () => {
  // handle 42 { return(x) => x }
  const body = handle(litInt(42), [
    handlerArm("return", ["x"], null, varRef("x")),
  ]);
  assertIntResult(runMain([], body), 42);
});

test("handle return clause transforms value", () => {
  // handle 10 { return(x) => x + 1 }
  const body = handle(litInt(10), [
    handlerArm("return", ["x"], null, desugar(infix("+", varRef("x"), litInt(1)))),
  ]);
  assertIntResult(runMain([], body), 11);
});

test("handle with no return clause passes through", () => {
  // handle 42 { raise(e) => 0 } — no return clause, body result stays on stack
  const body = handle(litInt(42), [
    handlerArm("raise", ["e"], null, litInt(0)),
  ]);
  assertIntResult(runMain([], body), 42);
});

test("handle wrapping function call with return clause", () => {
  // compute(x) = x * 2
  // main() = handle compute(21) { return(r) => r }
  const { result } = compileAndRun(
    def("compute", ["x"], desugar(infix("*", varRef("x"), litInt(2)))),
    def("main", [], handle(
      apply(varRef("compute"), [litInt(21)]),
      [handlerArm("return", ["r"], null, varRef("r"))],
    )),
  );
  assertIntResult(result, 42);
});

console.log("\nEffect handlers — catching effects (no resume):");

test("perform caught by handler, no resume (exception-like)", () => {
  // fail() = perform raise(99)
  // main() = handle fail() { return(x) => x, raise(e) => e + 1 }
  const { result } = compileAndRun(
    def("fail-fn", [], perform(apply(varRef("raise"), [litInt(99)]))),
    def("main", [], handle(
      apply(varRef("fail-fn"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("raise", ["e"], null, desugar(infix("+", varRef("e"), litInt(1)))),
      ],
    )),
  );
  assertIntResult(result, 100);
});

test("perform caught by handler, discard value", () => {
  // fail() = perform raise(0)
  // main() = handle fail() { return(x) => x, raise(_) => 42 }
  const { result } = compileAndRun(
    def("fail-fn", [], perform(apply(varRef("raise"), [litInt(0)]))),
    def("main", [], handle(
      apply(varRef("fail-fn"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("raise", ["_e"], null, litInt(42)),
      ],
    )),
  );
  assertIntResult(result, 42);
});

test("perform with no args caught by handler", () => {
  // ask-fn() = perform ask()
  // main() = handle ask-fn() { return(x) => x, ask() => 42 }
  const { result } = compileAndRun(
    def("ask-fn", [], perform(apply(varRef("ask"), []))),
    def("main", [], handle(
      apply(varRef("ask-fn"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("ask", [], null, litInt(42)),
      ],
    )),
  );
  assertIntResult(result, 42);
});

console.log("\nEffect handlers — resume:");

test("perform with resume returns value to perform site", () => {
  // get-val() = perform ask()
  // main() = handle get-val() { return(x) => x, ask() resume k => k(42) }
  const { result } = compileAndRun(
    def("get-val", [], perform(apply(varRef("ask"), []))),
    def("main", [], handle(
      apply(varRef("get-val"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("ask", [], "k", apply(varRef("k"), [litInt(42)])),
      ],
    )),
  );
  assertIntResult(result, 42);
});

test("resume value used in computation after perform", () => {
  // compute() = let v = perform ask() in v + 10
  // main() = handle compute() { return(x) => x, ask() resume k => k(32) }
  const { result } = compileAndRun(
    def("compute", [],
      letExpr("v", perform(apply(varRef("ask"), [])),
        desugar(infix("+", varRef("v"), litInt(10))))),
    def("main", [], handle(
      apply(varRef("compute"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("ask", [], "k", apply(varRef("k"), [litInt(32)])),
      ],
    )),
  );
  assertIntResult(result, 42);
});

test("resume with return clause transformation", () => {
  // compute() = perform ask()
  // main() = handle compute() { return(x) => x * 2, ask() resume k => k(21) }
  const { result } = compileAndRun(
    def("compute", [], perform(apply(varRef("ask"), []))),
    def("main", [], handle(
      apply(varRef("compute"), []),
      [
        handlerArm("return", ["x"], null, desugar(infix("*", varRef("x"), litInt(2)))),
        handlerArm("ask", [], "k", apply(varRef("k"), [litInt(21)])),
      ],
    )),
  );
  assertIntResult(result, 42);
});

test("perform with argument, resume uses it", () => {
  // compute() = perform put(10)
  // main() = handle compute() { return(x) => x, put(v) resume k => k(v + 1) }
  const { result } = compileAndRun(
    def("compute", [], perform(apply(varRef("put"), [litInt(10)]))),
    def("main", [], handle(
      apply(varRef("compute"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("put", ["v"], "k", apply(varRef("k"), [desugar(infix("+", varRef("v"), litInt(1)))])),
      ],
    )),
  );
  assertIntResult(result, 11);
});

test("multiple performs with resume (state-like pattern)", () => {
  // counter() = let a = perform get() in let _ = perform get() in a + 1
  // main() = handle counter() { return(x) => x, get() resume k => k(10) }
  const { result } = compileAndRun(
    def("counter", [],
      letExpr("a", perform(apply(varRef("get"), [])),
        letExpr("_b", perform(apply(varRef("get"), [])),
          desugar(infix("+", varRef("a"), litInt(1)))))),
    def("main", [], handle(
      apply(varRef("counter"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("get", [], "k", apply(varRef("k"), [litInt(10)])),
      ],
    )),
  );
  assertIntResult(result, 11);
});

console.log("\nEffect handlers — nested calls:");

test("perform in deeply nested call caught by outer handler", () => {
  // inner() = perform ask()
  // middle() = inner() + 10
  // main() = handle middle() { return(x) => x, ask() resume k => k(5) }
  const { result } = compileAndRun(
    def("inner", [], perform(apply(varRef("ask"), []))),
    def("middle", [], desugar(infix("+", apply(varRef("inner"), []), litInt(10)))),
    def("main", [], handle(
      apply(varRef("middle"), []),
      [
        handlerArm("return", ["x"], null, varRef("x")),
        handlerArm("ask", [], "k", apply(varRef("k"), [litInt(5)])),
      ],
    )),
  );
  assertIntResult(result, 15);
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
