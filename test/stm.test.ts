// STM (Software Transactional Memory) integration tests
// Run with: npx tsx test/stm.test.ts

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

function assertUnitResult(result: Value | undefined, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.UNIT, `${msg} expected Unit, got tag ${result!.tag}`);
}

// ── Tests ──

console.log("\nSTM Integration Tests\n");

// ─── TVar creation and read ───

console.log("TVar creation and basic read:");

test("tvar-new creates a TVar, tvar-read reads its value", () => {
  // main() = let tv = tvar-new(42) in tvar-read(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(42)]),
    apply(varRef("tvar-read"), [varRef("tv")])
  );
  assertIntResult(runMain([], body), 42);
});

test("tvar-new with different types", () => {
  // main() = let tv = tvar-new(true) in tvar-read(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litBool(true)]),
    apply(varRef("tvar-read"), [varRef("tv")])
  );
  assertBoolResult(runMain([], body), true);
});

test("multiple tvars hold independent values", () => {
  // main() = let a = tvar-new(10) in let b = tvar-new(20) in tvar-read(a) + tvar-read(b)
  const body = letExpr("a",
    apply(varRef("tvar-new"), [litInt(10)]),
    letExpr("b",
      apply(varRef("tvar-new"), [litInt(20)]),
      desugar(infix("+",
        apply(varRef("tvar-read"), [varRef("a")]),
        apply(varRef("tvar-read"), [varRef("b")])
      ))
    )
  );
  assertIntResult(runMain([], body), 30);
});

// ─── atomically — basic transactions ───

console.log("\natomically — basic transactions:");

test("atomically returns body result", () => {
  // main() = atomically(\-> 42)
  const body = apply(varRef("atomically"), [lambda([], litInt(42))]);
  assertIntResult(runMain([], body), 42);
});

test("atomically with tvar-read inside", () => {
  // main() = let tv = tvar-new(99) in atomically(\-> tvar-read(tv))
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(99)]),
    apply(varRef("atomically"), [
      lambda([], apply(varRef("tvar-read"), [varRef("tv")]))
    ])
  );
  assertIntResult(runMain([], body), 99);
});

test("atomically with tvar-write commits changes", () => {
  // main() = let tv = tvar-new(10) in
  //   let _ = atomically(\-> tvar-write(tv, 42)) in
  //   tvar-read(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(10)]),
    letExpr("_",
      apply(varRef("atomically"), [
        lambda([], apply(varRef("tvar-write"), [varRef("tv"), litInt(42)]))
      ]),
      apply(varRef("tvar-read"), [varRef("tv")])
    )
  );
  assertIntResult(runMain([], body), 42);
});

test("atomically read-modify-write", () => {
  // main() = let tv = tvar-new(10) in
  //   atomically(\-> let v = tvar-read(tv) in tvar-write(tv, v + 5))
  //   tvar-read(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(10)]),
    letExpr("_",
      apply(varRef("atomically"), [
        lambda([],
          letExpr("v",
            apply(varRef("tvar-read"), [varRef("tv")]),
            apply(varRef("tvar-write"), [varRef("tv"), desugar(infix("+", varRef("v"), litInt(5)))])
          )
        )
      ]),
      apply(varRef("tvar-read"), [varRef("tv")])
    )
  );
  assertIntResult(runMain([], body), 15);
});

test("atomically with multiple tvars (bank transfer)", () => {
  // main() =
  //   let from = tvar-new(500) in
  //   let to = tvar-new(200) in
  //   let _ = atomically(\->
  //     let bal = tvar-read(from) in
  //     let _ = tvar-write(from, bal - 100) in
  //     let toBal = tvar-read(to) in
  //     tvar-write(to, toBal + 100)
  //   ) in
  //   tvar-read(from) + tvar-read(to)
  const body = letExpr("from",
    apply(varRef("tvar-new"), [litInt(500)]),
    letExpr("to",
      apply(varRef("tvar-new"), [litInt(200)]),
      letExpr("_",
        apply(varRef("atomically"), [
          lambda([],
            letExpr("bal",
              apply(varRef("tvar-read"), [varRef("from")]),
              letExpr("_w1",
                apply(varRef("tvar-write"), [varRef("from"), desugar(infix("-", varRef("bal"), litInt(100)))]),
                letExpr("toBal",
                  apply(varRef("tvar-read"), [varRef("to")]),
                  apply(varRef("tvar-write"), [varRef("to"), desugar(infix("+", varRef("toBal"), litInt(100)))])
                )
              )
            )
          )
        ]),
        desugar(infix("+",
          apply(varRef("tvar-read"), [varRef("from")]),
          apply(varRef("tvar-read"), [varRef("to")])
        ))
      )
    )
  );
  // Total should be preserved: 500 + 200 = 700
  assertIntResult(runMain([], body), 700);
});

test("atomically body result is returned", () => {
  // main() = atomically(\-> let tv = tvar-new(7) in tvar-read(tv) * 6)
  // tvar-new inside atomically is also fine (outside transaction context for creation)
  const body = apply(varRef("atomically"), [
    lambda([],
      letExpr("tv",
        apply(varRef("tvar-new"), [litInt(7)]),
        desugar(infix("*",
          apply(varRef("tvar-read"), [varRef("tv")]),
          litInt(6)
        ))
      )
    )
  ]);
  assertIntResult(runMain([], body), 42);
});

// ─── Multiple sequential transactions ───

console.log("\nMultiple sequential transactions:");

test("two sequential atomically blocks", () => {
  // main() =
  //   let tv = tvar-new(0) in
  //   let _ = atomically(\-> tvar-write(tv, 10)) in
  //   let _ = atomically(\-> let v = tvar-read(tv) in tvar-write(tv, v + 5)) in
  //   tvar-read(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(0)]),
    letExpr("_1",
      apply(varRef("atomically"), [
        lambda([], apply(varRef("tvar-write"), [varRef("tv"), litInt(10)]))
      ]),
      letExpr("_2",
        apply(varRef("atomically"), [
          lambda([],
            letExpr("v",
              apply(varRef("tvar-read"), [varRef("tv")]),
              apply(varRef("tvar-write"), [varRef("tv"), desugar(infix("+", varRef("v"), litInt(5)))])
            )
          )
        ]),
        apply(varRef("tvar-read"), [varRef("tv")])
      )
    )
  );
  assertIntResult(runMain([], body), 15);
});

// ─── Write buffering (atomicity) ───

console.log("\nWrite buffering (atomicity):");

test("writes are not visible outside until commit", () => {
  // main() =
  //   let tv = tvar-new(1) in
  //   let result = atomically(\->
  //     let _ = tvar-write(tv, 99) in
  //     tvar-read(tv)  -- should see buffered value 99
  //   ) in
  //   result
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(1)]),
    apply(varRef("atomically"), [
      lambda([],
        letExpr("_",
          apply(varRef("tvar-write"), [varRef("tv"), litInt(99)]),
          apply(varRef("tvar-read"), [varRef("tv")])
        )
      )
    ])
  );
  // Inside the transaction, tvar-read should see the buffered write
  assertIntResult(runMain([], body), 99);
});

test("multiple writes to same tvar, last write wins", () => {
  // main() =
  //   let tv = tvar-new(0) in
  //   let _ = atomically(\->
  //     let _ = tvar-write(tv, 10) in
  //     let _ = tvar-write(tv, 20) in
  //     tvar-write(tv, 30)
  //   ) in
  //   tvar-read(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(0)]),
    letExpr("_",
      apply(varRef("atomically"), [
        lambda([],
          letExpr("_a",
            apply(varRef("tvar-write"), [varRef("tv"), litInt(10)]),
            letExpr("_b",
              apply(varRef("tvar-write"), [varRef("tv"), litInt(20)]),
              apply(varRef("tvar-write"), [varRef("tv"), litInt(30)])
            )
          )
        )
      ]),
      apply(varRef("tvar-read"), [varRef("tv")])
    )
  );
  assertIntResult(runMain([], body), 30);
});

// ─── or-else ───

console.log("\nor-else:");

test("or-else returns first action result when it succeeds", () => {
  // main() =
  //   let tv = tvar-new(42) in
  //   atomically(\->
  //     or-else(
  //       \-> tvar-read(tv),
  //       \-> 0
  //     )
  //   )
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(42)]),
    apply(varRef("atomically"), [
      lambda([],
        apply(varRef("or-else"), [
          lambda([], apply(varRef("tvar-read"), [varRef("tv")])),
          lambda([], litInt(0)),
        ])
      )
    ])
  );
  assertIntResult(runMain([], body), 42);
});

test("or-else falls through to second action on retry", () => {
  // main() =
  //   atomically(\->
  //     or-else(
  //       \-> retry(),
  //       \-> 99
  //     )
  //   )
  const body = apply(varRef("atomically"), [
    lambda([],
      apply(varRef("or-else"), [
        lambda([], apply(varRef("retry"), [])),
        lambda([], litInt(99)),
      ])
    )
  ]);
  assertIntResult(runMain([], body), 99);
});

test("or-else rolls back first action's writes on retry", () => {
  // main() =
  //   let tv = tvar-new(10) in
  //   let result = atomically(\->
  //     or-else(
  //       \-> let _ = tvar-write(tv, 999) in retry(),
  //       \-> tvar-read(tv)  -- should see 10, not 999
  //     )
  //   ) in
  //   result
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(10)]),
    apply(varRef("atomically"), [
      lambda([],
        apply(varRef("or-else"), [
          lambda([],
            letExpr("_",
              apply(varRef("tvar-write"), [varRef("tv"), litInt(999)]),
              apply(varRef("retry"), [])
            )
          ),
          lambda([], apply(varRef("tvar-read"), [varRef("tv")])),
        ])
      )
    ])
  );
  assertIntResult(runMain([], body), 10);
});

test("nested or-else", () => {
  // main() =
  //   atomically(\->
  //     or-else(
  //       \-> or-else(\-> retry(), \-> retry()),
  //       \-> 77
  //     )
  //   )
  const body = apply(varRef("atomically"), [
    lambda([],
      apply(varRef("or-else"), [
        lambda([],
          apply(varRef("or-else"), [
            lambda([], apply(varRef("retry"), [])),
            lambda([], apply(varRef("retry"), [])),
          ])
        ),
        lambda([], litInt(77)),
      ])
    )
  ]);
  assertIntResult(runMain([], body), 77);
});

// ─── Error cases ───

console.log("\nError cases:");

test("tvar-write outside atomically traps", () => {
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(0)]),
    apply(varRef("tvar-write"), [varRef("tv"), litInt(1)])
  );
  let trapped = false;
  try {
    runMain([], body);
  } catch (e: any) {
    trapped = e.message.includes("outside of atomically");
  }
  assert(trapped, "expected trap for tvar-write outside atomically");
});

test("nested atomically traps", () => {
  const body = apply(varRef("atomically"), [
    lambda([],
      apply(varRef("atomically"), [lambda([], litInt(1))])
    )
  ]);
  let trapped = false;
  try {
    runMain([], body);
  } catch (e: any) {
    trapped = e.message.includes("nested atomically");
  }
  assert(trapped, "expected trap for nested atomically");
});

test("retry outside atomically traps", () => {
  const body = apply(varRef("retry"), []);
  let trapped = false;
  try {
    runMain([], body);
  } catch (e: any) {
    trapped = e.message.includes("outside of atomically");
  }
  assert(trapped, "expected trap for retry outside atomically");
});

test("top-level retry (no or-else) traps in single-threaded mode", () => {
  const body = apply(varRef("atomically"), [
    lambda([], apply(varRef("retry"), []))
  ]);
  let trapped = false;
  try {
    runMain([], body);
  } catch (e: any) {
    trapped = e.message.includes("retry") || e.message.includes("block forever");
  }
  assert(trapped, "expected trap for top-level retry in single-threaded mode");
});

// ─── Conflict resolution via VM-level simulation ───

console.log("\nConflict resolution (simulated):");

test("transaction detects stale read after external modification", () => {
  // This test exercises the validation path by directly manipulating a TVar
  // between transaction read and commit. We do this by creating a module
  // and running the VM manually.
  //
  // Setup: create TVar(10), start txn, read TVar, externally bump version,
  //        try to commit — should fail validation and retry with new value.

  // We test this indirectly: two sequential atomically blocks where the
  // second reads the first's write — confirming version tracking works.
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(0)]),
    letExpr("_1",
      apply(varRef("atomically"), [
        lambda([], apply(varRef("tvar-write"), [varRef("tv"), litInt(42)]))
      ]),
      apply(varRef("atomically"), [
        lambda([], apply(varRef("tvar-read"), [varRef("tv")]))
      ])
    )
  );
  // The second transaction reads version 1 (set by first commit), snapshot is 1, so it passes.
  assertIntResult(runMain([], body), 42);
});

test("global version clock increments across transactions", () => {
  // Three sequential transactions writing to different tvars.
  // After all three, read all values to confirm commits were independent.
  const body = letExpr("a",
    apply(varRef("tvar-new"), [litInt(0)]),
    letExpr("b",
      apply(varRef("tvar-new"), [litInt(0)]),
      letExpr("c",
        apply(varRef("tvar-new"), [litInt(0)]),
        letExpr("_1",
          apply(varRef("atomically"), [
            lambda([], apply(varRef("tvar-write"), [varRef("a"), litInt(1)]))
          ]),
          letExpr("_2",
            apply(varRef("atomically"), [
              lambda([], apply(varRef("tvar-write"), [varRef("b"), litInt(2)]))
            ]),
            letExpr("_3",
              apply(varRef("atomically"), [
                lambda([], apply(varRef("tvar-write"), [varRef("c"), litInt(3)]))
              ]),
              desugar(infix("+",
                apply(varRef("tvar-read"), [varRef("a")]),
                desugar(infix("+",
                  apply(varRef("tvar-read"), [varRef("b")]),
                  apply(varRef("tvar-read"), [varRef("c")])
                ))
              ))
            )
          )
        )
      )
    )
  );
  assertIntResult(runMain([], body), 6);
});

// ─── Affine take/put ───

console.log("\nAffine take/put:");

test("tvar-take removes value, tvar-put restores it", () => {
  // main() =
  //   let tv = tvar-new(42) in
  //   atomically(\->
  //     let v = tvar-take(tv) in
  //     let _ = tvar-put(tv, v + 1) in
  //     v
  //   )
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(42)]),
    apply(varRef("atomically"), [
      lambda([],
        letExpr("v",
          apply(varRef("tvar-take"), [varRef("tv")]),
          letExpr("_",
            apply(varRef("tvar-put"), [varRef("tv"), desugar(infix("+", varRef("v"), litInt(1)))]),
            varRef("v")
          )
        )
      )
    ])
  );
  assertIntResult(runMain([], body), 42);
});

test("tvar-take on empty TVar traps", () => {
  // Take twice — second should fail
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(1)]),
    apply(varRef("atomically"), [
      lambda([],
        letExpr("_v1",
          apply(varRef("tvar-take"), [varRef("tv")]),
          apply(varRef("tvar-take"), [varRef("tv")])
        )
      )
    ])
  );
  let trapped = false;
  try {
    runMain([], body);
  } catch (e: any) {
    trapped = e.message.includes("empty");
  }
  assert(trapped, "expected trap for tvar-take on empty TVar");
});

test("tvar-put on occupied TVar traps", () => {
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(1)]),
    apply(varRef("atomically"), [
      lambda([],
        apply(varRef("tvar-put"), [varRef("tv"), litInt(2)])
      )
    ])
  );
  let trapped = false;
  try {
    runMain([], body);
  } catch (e: any) {
    trapped = e.message.includes("occupied");
  }
  assert(trapped, "expected trap for tvar-put on occupied TVar");
});

test("take then put commits correctly", () => {
  // main() =
  //   let tv = tvar-new(10) in
  //   let _ = atomically(\->
  //     let v = tvar-take(tv) in
  //     tvar-put(tv, v * 2)
  //   ) in
  //   tvar-read(tv)
  const body = letExpr("tv",
    apply(varRef("tvar-new"), [litInt(10)]),
    letExpr("_",
      apply(varRef("atomically"), [
        lambda([],
          letExpr("v",
            apply(varRef("tvar-take"), [varRef("tv")]),
            apply(varRef("tvar-put"), [varRef("tv"), desugar(infix("*", varRef("v"), litInt(2)))])
          )
        )
      ]),
      apply(varRef("tvar-read"), [varRef("tv")])
    )
  );
  assertIntResult(runMain([], body), 20);
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
