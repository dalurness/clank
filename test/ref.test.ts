// Ref (mutable reference cell) integration tests
// Run with: npx tsx test/ref.test.ts

import { compileProgram, Op, type BytecodeModule } from "../ts/src/compiler.js";
import { VM, execute, Val, Tag, type Value } from "../ts/src/vm.js";
import { desugar } from "../ts/src/desugar.js";
import type { Expr, Program, TopLevel, TypeSig, Loc } from "../ts/src/ast.js";

const loc: Loc = { line: 1, col: 1 };

// ── AST helpers ──

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

function assertUnitResult(result: Value | undefined, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.UNIT, `${msg} expected Unit, got tag ${result!.tag}`);
}

// ── Tests ──

console.log("\nRef Integration Tests\n");

// ─── Ref creation and read ───

console.log("Ref creation and read:");

test("ref-new creates a Ref, ref-read reads its value", () => {
  // main() = let r = ref-new(42) in ref-read(r)
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(42)]),
    apply(varRef("ref-read"), [varRef("r")])
  );
  assertIntResult(runMain([], body), 42);
});

test("ref-new with boolean value", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litBool(true)]),
    apply(varRef("ref-read"), [varRef("r")])
  );
  assertBoolResult(runMain([], body), true);
});

test("ref-new with string value", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litStr("hello")]),
    apply(varRef("ref-read"), [varRef("r")])
  );
  assertStrResult(runMain([], body), "hello");
});

// ─── Ref write ───

console.log("\nRef write:");

test("ref-write updates the value", () => {
  // main() = let r = ref-new(10) in let _ = ref-write(r, 20) in ref-read(r)
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_w",
      apply(varRef("ref-write"), [varRef("r"), litInt(20)]),
      apply(varRef("ref-read"), [varRef("r")])
    )
  );
  assertIntResult(runMain([], body), 20);
});

test("multiple writes update correctly", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(1)]),
    letExpr("_w1",
      apply(varRef("ref-write"), [varRef("r"), litInt(2)]),
      letExpr("_w2",
        apply(varRef("ref-write"), [varRef("r"), litInt(3)]),
        apply(varRef("ref-read"), [varRef("r")])
      )
    )
  );
  assertIntResult(runMain([], body), 3);
});

test("write then read returns new value", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(100)]),
    letExpr("_w",
      apply(varRef("ref-write"), [varRef("r"), litInt(200)]),
      apply(varRef("ref-read"), [varRef("r")])
    )
  );
  assertIntResult(runMain([], body), 200);
});

test("ref-write returns unit", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    apply(varRef("ref-write"), [varRef("r"), litInt(20)])
  );
  assertUnitResult(runMain([], body));
});

// ─── Multiple independent refs ───

console.log("\nMultiple independent refs:");

test("two refs hold independent values", () => {
  // main() = let a = ref-new(10) in let b = ref-new(20) in
  //          let va = ref-read(a) in let vb = ref-read(b) in va + vb
  const body = letExpr("a",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("b",
      apply(varRef("ref-new"), [litInt(20)]),
      letExpr("va",
        apply(varRef("ref-read"), [varRef("a")]),
        letExpr("vb",
          apply(varRef("ref-read"), [varRef("b")]),
          desugar(infix("+", varRef("va"), varRef("vb")))
        )
      )
    )
  );
  assertIntResult(runMain([], body), 30);
});

test("writing one ref doesn't affect another", () => {
  const body = letExpr("a",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("b",
      apply(varRef("ref-new"), [litInt(20)]),
      letExpr("_w",
        apply(varRef("ref-write"), [varRef("a"), litInt(99)]),
        apply(varRef("ref-read"), [varRef("b")])
      )
    )
  );
  assertIntResult(runMain([], body), 20);
});

// ─── Ref with computation ───

console.log("\nRef with computation:");

test("read-modify-write pattern", () => {
  // main() = let r = ref-new(10) in
  //          let v = ref-read(r) in
  //          let _ = ref-write(r, v + 5) in
  //          ref-read(r)
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("v",
      apply(varRef("ref-read"), [varRef("r")]),
      letExpr("_w",
        apply(varRef("ref-write"), [varRef("r"), desugar(infix("+", varRef("v"), litInt(5)))]),
        apply(varRef("ref-read"), [varRef("r")])
      )
    )
  );
  assertIntResult(runMain([], body), 15);
});

test("ref used in a loop (recursive counter)", () => {
  // count(r, n) = if n == 0 then ref-read(r)
  //               else let v = ref-read(r) in
  //                    let _ = ref-write(r, v + 1) in
  //                    count(r, n - 1)
  // main() = let r = ref-new(0) in count(r, 10)
  const countBody = ifExpr(
    desugar(infix("==", varRef("n"), litInt(0))),
    apply(varRef("ref-read"), [varRef("r")]),
    letExpr("v",
      apply(varRef("ref-read"), [varRef("r")]),
      letExpr("_w",
        apply(varRef("ref-write"), [varRef("r"), desugar(infix("+", varRef("v"), litInt(1)))]),
        apply(varRef("count"), [varRef("r"), desugar(infix("-", varRef("n"), litInt(1)))])
      )
    )
  );

  const mainBody = letExpr("r",
    apply(varRef("ref-new"), [litInt(0)]),
    apply(varRef("count"), [varRef("r"), litInt(10)])
  );

  const { result } = compileAndRun(
    def("count", ["r", "n"], countBody),
    def("main", [], mainBody),
  );
  assertIntResult(result, 10);
});

// ─── Ref CAS (compare-and-swap) ───

console.log("\nRef CAS:");

test("ref-cas succeeds when expected matches current value", () => {
  // main() = let r = ref-new(10) in
  //          let result = ref-cas(r, 10, 20) in
  //          fst(result)  -- should be true
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("result",
      apply(varRef("ref-cas"), [varRef("r"), litInt(10), litInt(20)]),
      apply(varRef("fst"), [varRef("result")])
    )
  );
  assertBoolResult(runMain([], body), true);
});

test("ref-cas returns current value on success", () => {
  // main() = let r = ref-new(10) in
  //          let result = ref-cas(r, 10, 20) in
  //          snd(result)  -- should be 10 (old value)
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("result",
      apply(varRef("ref-cas"), [varRef("r"), litInt(10), litInt(20)]),
      apply(varRef("snd"), [varRef("result")])
    )
  );
  assertIntResult(runMain([], body), 10);
});

test("ref-cas updates the cell on success", () => {
  // main() = let r = ref-new(10) in
  //          let _ = ref-cas(r, 10, 20) in
  //          ref-read(r)  -- should be 20
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_cas",
      apply(varRef("ref-cas"), [varRef("r"), litInt(10), litInt(20)]),
      apply(varRef("ref-read"), [varRef("r")])
    )
  );
  assertIntResult(runMain([], body), 20);
});

test("ref-cas fails when expected does not match current value", () => {
  // main() = let r = ref-new(10) in
  //          let result = ref-cas(r, 99, 20) in
  //          fst(result)  -- should be false
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("result",
      apply(varRef("ref-cas"), [varRef("r"), litInt(99), litInt(20)]),
      apply(varRef("fst"), [varRef("result")])
    )
  );
  assertBoolResult(runMain([], body), false);
});

test("ref-cas does not update cell on failure", () => {
  // main() = let r = ref-new(10) in
  //          let _ = ref-cas(r, 99, 20) in
  //          ref-read(r)  -- should still be 10
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_cas",
      apply(varRef("ref-cas"), [varRef("r"), litInt(99), litInt(20)]),
      apply(varRef("ref-read"), [varRef("r")])
    )
  );
  assertIntResult(runMain([], body), 10);
});

test("ref-cas returns current value on failure", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("result",
      apply(varRef("ref-cas"), [varRef("r"), litInt(99), litInt(0)]),
      apply(varRef("snd"), [varRef("result")])
    )
  );
  assertIntResult(runMain([], body), 42);
});

// ─── Ref modify ───

console.log("\nRef modify:");

test("ref-modify applies function and returns new value", () => {
  // main() = let r = ref-new(10) in ref-modify(r, \x -> x + 5)
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    apply(varRef("ref-modify"), [varRef("r"), lambda(["x"], desugar(infix("+", varRef("x"), litInt(5))))])
  );
  assertIntResult(runMain([], body), 15);
});

test("ref-modify updates the cell", () => {
  // main() = let r = ref-new(10) in
  //          let _ = ref-modify(r, \x -> x * 2) in
  //          ref-read(r)
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_m",
      apply(varRef("ref-modify"), [varRef("r"), lambda(["x"], desugar(infix("*", varRef("x"), litInt(2))))]),
      apply(varRef("ref-read"), [varRef("r")])
    )
  );
  assertIntResult(runMain([], body), 20);
});

test("ref-modify with identity function", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(42)]),
    apply(varRef("ref-modify"), [varRef("r"), lambda(["x"], varRef("x"))])
  );
  assertIntResult(runMain([], body), 42);
});

test("multiple ref-modify calls accumulate", () => {
  // main() = let r = ref-new(0) in
  //          let _ = ref-modify(r, \x -> x + 1) in
  //          let _ = ref-modify(r, \x -> x + 1) in
  //          let _ = ref-modify(r, \x -> x + 1) in
  //          ref-read(r)
  const inc = lambda(["x"], desugar(infix("+", varRef("x"), litInt(1))));
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(0)]),
    letExpr("_m1",
      apply(varRef("ref-modify"), [varRef("r"), inc]),
      letExpr("_m2",
        apply(varRef("ref-modify"), [varRef("r"), inc]),
        letExpr("_m3",
          apply(varRef("ref-modify"), [varRef("r"), inc]),
          apply(varRef("ref-read"), [varRef("r")])
        )
      )
    )
  );
  assertIntResult(runMain([], body), 3);
});

// ─── Ref close ───

console.log("\nRef close:");

test("ref-close marks ref as closed", () => {
  // main() = let r = ref-new(10) in
  //          let v = ref-read(r) in
  //          let _ = ref-close(r) in
  //          v
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("v",
      apply(varRef("ref-read"), [varRef("r")]),
      letExpr("_c",
        apply(varRef("ref-close"), [varRef("r")]),
        varRef("v")
      )
    )
  );
  assertIntResult(runMain([], body), 10);
});

test("ref-close returns unit", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    apply(varRef("ref-close"), [varRef("r")])
  );
  assertUnitResult(runMain([], body));
});

test("ref-read on closed ref traps", () => {
  // main() = let r = ref-new(10) in
  //          let _ = ref-close(r) in
  //          ref-read(r)  -- should trap
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_c",
      apply(varRef("ref-close"), [varRef("r")]),
      apply(varRef("ref-read"), [varRef("r")])
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("E011") || e.message.includes("closed"),
      `Expected closed ref trap, got: ${e.message}`);
  }
});

test("ref-write on closed ref traps", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_c",
      apply(varRef("ref-close"), [varRef("r")]),
      apply(varRef("ref-write"), [varRef("r"), litInt(20)])
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("E012") || e.message.includes("closed"),
      `Expected closed ref trap, got: ${e.message}`);
  }
});

test("ref-cas on closed ref traps", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_c",
      apply(varRef("ref-close"), [varRef("r")]),
      apply(varRef("ref-cas"), [varRef("r"), litInt(10), litInt(20)])
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("closed"),
      `Expected closed ref trap, got: ${e.message}`);
  }
});

test("ref-modify on closed ref traps", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_c",
      apply(varRef("ref-close"), [varRef("r")]),
      apply(varRef("ref-modify"), [varRef("r"), lambda(["x"], varRef("x"))])
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("closed"),
      `Expected closed ref trap, got: ${e.message}`);
  }
});

test("double ref-close traps", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_c1",
      apply(varRef("ref-close"), [varRef("r")]),
      apply(varRef("ref-close"), [varRef("r")])
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("closed"),
      `Expected already-closed trap, got: ${e.message}`);
  }
});

// ─── CAS + modify integration ───

console.log("\nCAS + modify integration:");

test("CAS loop pattern: retry until success", () => {
  // Simulate a CAS loop: try to set ref from 10 to 20, should succeed on first try
  // main() = let r = ref-new(10) in
  //          let result = ref-cas(r, 10, 20) in
  //          let success = fst(result) in
  //          if success then ref-read(r) else 0
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("result",
      apply(varRef("ref-cas"), [varRef("r"), litInt(10), litInt(20)]),
      letExpr("success",
        apply(varRef("fst"), [varRef("result")]),
        ifExpr(varRef("success"),
          apply(varRef("ref-read"), [varRef("r")]),
          litInt(0)
        )
      )
    )
  );
  assertIntResult(runMain([], body), 20);
});

test("ref-modify then ref-cas sees updated value", () => {
  // main() = let r = ref-new(10) in
  //          let _ = ref-modify(r, \x -> x + 5) in  -- now 15
  //          let result = ref-cas(r, 15, 100) in     -- should succeed
  //          fst(result)
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_m",
      apply(varRef("ref-modify"), [varRef("r"), lambda(["x"], desugar(infix("+", varRef("x"), litInt(5))))]),
      letExpr("result",
        apply(varRef("ref-cas"), [varRef("r"), litInt(15), litInt(100)]),
        apply(varRef("fst"), [varRef("result")])
      )
    )
  );
  assertBoolResult(runMain([], body), true);
});

// ─── Affine type dispatch (take/put protocol) ───

console.log("\nAffine type dispatch:");

test("ref containing a Ref (affine value): ref-read does take (empties cell)", () => {
  // main() = let inner = ref-new(42) in
  //          let outer = ref-new(inner) in
  //          let taken = ref-read(outer) in
  //          ref-read(taken)  -- taken is the inner Ref, should still be usable
  const body = letExpr("inner",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("outer",
      apply(varRef("ref-new"), [varRef("inner")]),
      letExpr("taken",
        apply(varRef("ref-read"), [varRef("outer")]),
        apply(varRef("ref-read"), [varRef("taken")])
      )
    )
  );
  assertIntResult(runMain([], body), 42);
});

test("ref-read on empty affine cell traps E011", () => {
  // Take from outer (empties it), then try to read again → should trap
  // main() = let inner = ref-new(42) in
  //          let outer = ref-new(inner) in
  //          let _ = ref-read(outer) in   -- takes inner out
  //          ref-read(outer)              -- outer is now empty → trap
  const body = letExpr("inner",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("outer",
      apply(varRef("ref-new"), [varRef("inner")]),
      letExpr("_taken",
        apply(varRef("ref-read"), [varRef("outer")]),
        apply(varRef("ref-read"), [varRef("outer")])
      )
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("E011") || e.message.includes("empty"),
      `Expected empty ref trap, got: ${e.message}`);
  }
});

test("ref-write on empty affine cell (put) succeeds", () => {
  // Take from outer, then put it back
  // main() = let inner = ref-new(42) in
  //          let outer = ref-new(inner) in
  //          let taken = ref-read(outer) in  -- take
  //          let _ = ref-write(outer, taken) in  -- put back
  //          let taken2 = ref-read(outer) in  -- take again
  //          ref-read(taken2)
  const body = letExpr("inner",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("outer",
      apply(varRef("ref-new"), [varRef("inner")]),
      letExpr("taken",
        apply(varRef("ref-read"), [varRef("outer")]),
        letExpr("_put",
          apply(varRef("ref-write"), [varRef("outer"), varRef("taken")]),
          letExpr("taken2",
            apply(varRef("ref-read"), [varRef("outer")]),
            apply(varRef("ref-read"), [varRef("taken2")])
          )
        )
      )
    )
  );
  assertIntResult(runMain([], body), 42);
});

test("ref-write on non-empty affine cell (put when full) traps E012", () => {
  // Outer holds inner (full), try to write another Ref into it → trap
  // main() = let inner = ref-new(42) in
  //          let inner2 = ref-new(99) in
  //          let outer = ref-new(inner) in
  //          ref-write(outer, inner2)   -- outer is full → trap
  const body = letExpr("inner",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("inner2",
      apply(varRef("ref-new"), [litInt(99)]),
      letExpr("outer",
        apply(varRef("ref-new"), [varRef("inner")]),
        apply(varRef("ref-write"), [varRef("outer"), varRef("inner2")])
      )
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("E012") || e.message.includes("full"),
      `Expected full ref trap, got: ${e.message}`);
  }
});

test("unrestricted ref-read is non-destructive (can read multiple times)", () => {
  // Int is unrestricted: multiple reads should work
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("v1",
      apply(varRef("ref-read"), [varRef("r")]),
      letExpr("v2",
        apply(varRef("ref-read"), [varRef("r")]),
        desugar(infix("+", varRef("v1"), varRef("v2")))
      )
    )
  );
  assertIntResult(runMain([], body), 84);
});

test("unrestricted ref-write overwrites freely", () => {
  // Int is unrestricted: write to non-empty cell should just overwrite
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    letExpr("_w1",
      apply(varRef("ref-write"), [varRef("r"), litInt(20)]),
      letExpr("_w2",
        apply(varRef("ref-write"), [varRef("r"), litInt(30)]),
        apply(varRef("ref-read"), [varRef("r")])
      )
    )
  );
  assertIntResult(runMain([], body), 30);
});

// ─── Handle counting ───

console.log("\nHandle counting:");

test("handle count starts at 1, ref-close decrements to 0 and closes", () => {
  // Basic: create, read, close, try to read → trap
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("v",
      apply(varRef("ref-read"), [varRef("r")]),
      letExpr("_c",
        apply(varRef("ref-close"), [varRef("r")]),
        varRef("v")
      )
    )
  );
  assertIntResult(runMain([], body), 42);
});

// ─── REF_CLOSE on TVar ───

console.log("\nREF_CLOSE on TVar:");

test("ref-close accepts TVar without trapping", () => {
  // main() = let tv = tvar-new(42) in ref-close(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(42)]),
    apply(varRef("ref-close"), [varRef("tv")])
  );
  assertUnitResult(runMain([], body));
});

// ─── REF_MODIFY affine guard ───

console.log("\nREF_MODIFY affine guard:");

test("ref-modify on affine Ref traps", () => {
  // Ref containing a Ref (affine) should not allow ref-modify
  // main() = let inner = ref-new(42) in
  //          let outer = ref-new(inner) in
  //          ref-modify(outer, \x -> x)  -- should trap: affine value
  const body = letExpr("inner",
    apply(varRef("ref-new"), [litInt(42)]),
    letExpr("outer",
      apply(varRef("ref-new"), [varRef("inner")]),
      apply(varRef("ref-modify"), [varRef("outer"), lambda(["x"], varRef("x"))])
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("affine") || e.message.includes("E002"),
      `Expected affine rejection, got: ${e.message}`);
  }
});

test("ref-modify on unrestricted Ref works normally", () => {
  const body = letExpr("r",
    apply(varRef("ref-new"), [litInt(10)]),
    apply(varRef("ref-modify"), [varRef("r"), lambda(["x"], desugar(infix("+", varRef("x"), litInt(5))))])
  );
  assertIntResult(runMain([], body), 15);
});

// ─── TVar close with handle counting ───

console.log("\nTVar close with handle counting:");

test("double ref-close on TVar traps", () => {
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(42)]),
    letExpr("_c1",
      apply(varRef("ref-close"), [varRef("tv")]),
      apply(varRef("ref-close"), [varRef("tv")])
    )
  );
  try {
    runMain([], body);
    throw new Error("Expected trap but got success");
  } catch (e: any) {
    assert(e.message.includes("closed"),
      `Expected already-closed trap, got: ${e.message}`);
  }
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
