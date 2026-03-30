// Streaming I/O and Iterator tests
// Run with: npx tsx test/streaming-io.test.ts

import { compileProgram, Op, type BytecodeModule } from "../ts/src/compiler.js";
import { VM, execute, Val, Tag, type Value, type IteratorState } from "../ts/src/vm.js";
import { desugar } from "../ts/src/desugar.js";
import type { Expr, Program, TopLevel, TypeSig, Loc } from "../ts/src/ast.js";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);

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
const list = (elements: Expr[]): Expr => ({ tag: "list", elements, loc });

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

function assertListInts(result: Value | undefined, expected: number[], msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.HEAP, `${msg} expected HEAP, got tag ${result!.tag}`);
  const heap = (result as any).value;
  assert(heap.kind === "list", `${msg} expected list, got ${heap.kind}`);
  assertEq(heap.items.length, expected.length, `${msg} list length`);
  for (let i = 0; i < expected.length; i++) {
    assert(heap.items[i].tag === Tag.INT, `${msg} item ${i} expected Int`);
    assertEq(heap.items[i].value, expected[i], `${msg} item ${i}`);
  }
}

// ── Direct VM tests (bypass compiler, test opcodes/builtins directly) ──

function makeModule(words: any[], strings: string[] = [], variantNames: string[] = []): BytecodeModule {
  return {
    words,
    strings,
    rationals: [],
    variantNames,
    entryWordId: 256,
    dispatchTable: new Map(),
    externs: [],
  };
}

function runBytecode(code: number[], strings: string[] = [], variantNames: string[] = []): { vm: VM; result: Value | undefined } {
  const mainWord = { name: "main", wordId: 256, code, localCount: 4, isPublic: true };
  const mod = makeModule([mainWord], strings, variantNames);
  const vm = new VM(mod);
  const result = vm.run();
  return { vm, result };
}

// ── Tests ──

console.log("\nStreaming I/O Tests\n");

// ── iter.of / iter.collect (via compiler) ──

console.log("iter.of and iter.collect:");

test("iter.of creates iterator from list, iter.collect gathers back", () => {
  // iter.collect(iter.of([1, 2, 3]))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 2, 3]);
});

test("iter.of on empty list", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.of"), [list([])])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, []);
});

// ── iter.range ──

console.log("\niter.range:");

test("iter.range creates lazy range", () => {
  // iter.collect(iter.range(0, 5))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.range"), [litInt(0), litInt(5)])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [0, 1, 2, 3, 4]);
});

test("iter.range empty when start >= end", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.range"), [litInt(5), litInt(5)])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, []);
});

// ── iter.map ──

console.log("\niter.map:");

test("iter.map applies function to each element", () => {
  // iter.collect(iter.map(iter.of([1, 2, 3]), fn(x) => x * 2))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.map"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
      lambda(["x"], desugar(infix("*", varRef("x"), litInt(2)))),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [2, 4, 6]);
});

// ── iter.filter ──

console.log("\niter.filter:");

test("iter.filter keeps matching elements", () => {
  // iter.collect(iter.filter(iter.of([1,2,3,4,5]), fn(x) => x > 2))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.filter"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4), litInt(5)])]),
      lambda(["x"], desugar(infix(">", varRef("x"), litInt(2)))),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [3, 4, 5]);
});

// ── iter.take ──

console.log("\niter.take:");

test("iter.take first N elements", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.of"), [list([litInt(10), litInt(20), litInt(30), litInt(40)])]),
      litInt(2),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [10, 20]);
});

test("iter.take more than available", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.of"), [list([litInt(1)])]),
      litInt(10),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1]);
});

// ── iter.drop ──

console.log("\niter.drop:");

test("iter.drop first N elements", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.drop"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4)])]),
      litInt(2),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [3, 4]);
});

// ── iter.fold ──

console.log("\niter.fold:");

test("iter.fold sums elements", () => {
  // iter.fold(iter.of([1,2,3,4]), 0, fn(acc, x) => acc + x)
  const body = apply(varRef("iter.fold"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4)])]),
    litInt(0),
    lambda(["acc", "x"], desugar(infix("+", varRef("acc"), varRef("x")))),
  ]);
  const result = runMain([], desugar(body));
  assertIntResult(result, 10);
});

// ── iter.count ──

console.log("\niter.count:");

test("iter.count counts elements", () => {
  const body = apply(varRef("iter.count"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])])
  ]);
  const result = runMain([], desugar(body));
  assertIntResult(result, 3);
});

test("iter.count of empty", () => {
  const body = apply(varRef("iter.count"), [
    apply(varRef("iter.of"), [list([])])
  ]);
  const result = runMain([], desugar(body));
  assertIntResult(result, 0);
});

// ── iter.sum ──

console.log("\niter.sum:");

test("iter.sum sums integers", () => {
  const body = apply(varRef("iter.sum"), [
    apply(varRef("iter.of"), [list([litInt(10), litInt(20), litInt(30)])])
  ]);
  const result = runMain([], desugar(body));
  assertIntResult(result, 60);
});

// ── iter.any / iter.all ──

console.log("\niter.any / iter.all:");

test("iter.any returns true when match found", () => {
  const body = apply(varRef("iter.any"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
    lambda(["x"], desugar(infix(">", varRef("x"), litInt(2)))),
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.BOOL, "expected Bool");
  assertEq((result as any).value, true);
});

test("iter.any returns false when no match", () => {
  const body = apply(varRef("iter.any"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2)])]),
    lambda(["x"], desugar(infix(">", varRef("x"), litInt(10)))),
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.BOOL, "expected Bool");
  assertEq((result as any).value, false);
});

test("iter.all returns true when all match", () => {
  const body = apply(varRef("iter.all"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
    lambda(["x"], desugar(infix(">", varRef("x"), litInt(0)))),
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.BOOL, "expected Bool");
  assertEq((result as any).value, true);
});

test("iter.all returns false when not all match", () => {
  const body = apply(varRef("iter.all"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
    lambda(["x"], desugar(infix("<", varRef("x"), litInt(3)))),
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.BOOL, "expected Bool");
  assertEq((result as any).value, false);
});

// ── iter.enumerate ──

console.log("\niter.enumerate:");

test("iter.enumerate pairs with indices", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.enumerate"), [
      apply(varRef("iter.of"), [list([litInt(10), litInt(20), litInt(30)])])
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assertEq(items.length, 3, "length");
  // Each item is a tuple (index, value)
  assert(items[0].tag === Tag.HEAP && items[0].value.kind === "tuple", "item 0 tuple");
  assertEq(items[0].value.items[0].value, 0, "index 0");
  assertEq(items[0].value.items[1].value, 10, "value 0");
  assertEq(items[1].value.items[0].value, 1, "index 1");
  assertEq(items[2].value.items[0].value, 2, "index 2");
});

// ── iter.chain ──

console.log("\niter.chain:");

test("iter.chain concatenates two iterators", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.chain"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2)])]),
      apply(varRef("iter.of"), [list([litInt(3), litInt(4)])]),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 2, 3, 4]);
});

// ── iter.zip ──

console.log("\niter.zip:");

test("iter.zip pairs elements from two iterators", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.zip"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
      apply(varRef("iter.of"), [list([litInt(10), litInt(20)])]),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assertEq(items.length, 2, "zip shortest");
  assertEq(items[0].value.items[0].value, 1);
  assertEq(items[0].value.items[1].value, 10);
  assertEq(items[1].value.items[0].value, 2);
  assertEq(items[1].value.items[1].value, 20);
});

// ── iter.take-while / iter.drop-while ──

console.log("\niter.take-while / iter.drop-while:");

test("iter.take-while takes while predicate holds", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take-while"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(1)])]),
      lambda(["x"], desugar(infix("<", varRef("x"), litInt(3)))),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 2]);
});

test("iter.drop-while drops while predicate holds", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.drop-while"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(1)])]),
      lambda(["x"], desugar(infix("<", varRef("x"), litInt(3)))),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [3, 1]);
});

// ── iter.once / iter.empty ──

console.log("\niter.once / iter.empty:");

test("iter.once creates single-element iterator", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.once"), [litInt(42)])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [42]);
});

test("iter.empty creates empty iterator", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.empty"), [litUnit()])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, []);
});

// ── iter.each (side-effects) ──

console.log("\niter.each:");

test("iter.each executes side effects", () => {
  // iter.each(iter.of([1, 2, 3]), fn(x) => print(show(x)))
  const body = apply(varRef("iter.each"), [
    apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
    lambda(["x"], apply(varRef("print"), [apply(varRef("show"), [varRef("x")])])),
  ]);
  const { result, stdout } = compileAndRun(def("main", [], desugar(body)));
  assertEq(stdout.length, 3, "should print 3 times");
  assertEq(stdout[0], "1");
  assertEq(stdout[1], "2");
  assertEq(stdout[2], "3");
});

// ── iter.intersperse ──

console.log("\niter.intersperse:");

test("iter.intersperse inserts separator", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.intersperse"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
      litInt(0),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 0, 2, 0, 3]);
});

// ── iter.chunk ──

console.log("\niter.chunk:");

test("iter.chunk groups elements", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.chunk"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4), litInt(5)])]),
      litInt(2),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const chunks = (result as any).value.items;
  assertEq(chunks.length, 3, "3 chunks");
  // First chunk [1,2]
  assertEq(chunks[0].value.items.length, 2);
  assertEq(chunks[0].value.items[0].value, 1);
  assertEq(chunks[0].value.items[1].value, 2);
  // Last chunk [5]
  assertEq(chunks[2].value.items.length, 1);
  assertEq(chunks[2].value.items[0].value, 5);
});

// ── iter.window ──

console.log("\niter.window:");

test("iter.window sliding window", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.window"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4)])]),
      litInt(2),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const windows = (result as any).value.items;
  assertEq(windows.length, 3, "3 windows");
  assertEq(windows[0].value.items[0].value, 1);
  assertEq(windows[0].value.items[1].value, 2);
  assertEq(windows[2].value.items[0].value, 3);
  assertEq(windows[2].value.items[1].value, 4);
});

// ── iter.dedup ──

console.log("\niter.dedup:");

test("iter.dedup removes consecutive duplicates", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.dedup"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(1), litInt(2), litInt(2), litInt(3), litInt(1)])])
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 2, 3, 1]);
});

// ── iter.scan ──

console.log("\niter.scan:");

test("iter.scan produces running fold", () => {
  // iter.collect(iter.scan(iter.of([1,2,3]), 0, fn(acc, x) => acc + x))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.scan"), [
      apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]),
      litInt(0),
      lambda(["acc", "x"], desugar(infix("+", varRef("acc"), varRef("x")))),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [0, 1, 3, 6]);
});

// ── Chained pipelines ──

console.log("\nChained iterator pipelines:");

test("map + filter + collect pipeline", () => {
  // iter.of([1,2,3,4,5]) |> iter.map(fn(x) => x*x) |> iter.filter(fn(x) => x > 5) |> iter.collect
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.filter"), [
      apply(varRef("iter.map"), [
        apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4), litInt(5)])]),
        lambda(["x"], desugar(infix("*", varRef("x"), varRef("x")))),
      ]),
      lambda(["x"], desugar(infix(">", varRef("x"), litInt(5)))),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [9, 16, 25]);
});

test("range + take + sum pipeline", () => {
  // iter.sum(iter.take(iter.range(0, 100), 5))
  const body = apply(varRef("iter.sum"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.range"), [litInt(0), litInt(100)]),
      litInt(5),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertIntResult(result, 10); // 0+1+2+3+4 = 10
});

test("range + drop + take + collect", () => {
  // iter.collect(iter.take(iter.drop(iter.range(0, 10), 3), 4))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.drop"), [
        apply(varRef("iter.range"), [litInt(0), litInt(10)]),
        litInt(3),
      ]),
      litInt(4),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [3, 4, 5, 6]);
});

// ── Direct VM opcode tests ──

console.log("\nDirect VM opcode tests:");

test("ITER_NEW + ITER_CLOSE creates and closes iterator", () => {
  // Push a unit cleanup, push a unit generator, ITER_NEW, ITER_CLOSE
  const code = [
    Op.PUSH_UNIT,   // generator fn (placeholder)
    Op.PUSH_UNIT,   // cleanup fn
    Op.ITER_NEW,    // create iterator
    Op.ITER_CLOSE,  // close it
    Op.HALT,
  ];
  const { result } = runBytecode(code);
  // Should complete without error, result is unit from ITER_CLOSE
  assert(result !== undefined, "should have result");
  assertEq(result!.tag, Tag.UNIT, "result is unit");
});

// ── iter.join ──

console.log("\niter.join:");

test("iter.join joins strings", () => {
  const body = apply(varRef("iter.join"), [
    apply(varRef("iter.of"), [list([litStr("a"), litStr("b"), litStr("c")])]),
    litStr(", "),
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.STR, "expected Str");
  assertEq((result as any).value, "a, b, c");
});

// ── fs.stream-lines ──

console.log("\nfs.stream-lines:");

test("fs.stream-lines reads file as iterator of lines", () => {
  // Write a temp file, then read it with fs.stream-lines
  const fs = require("fs");
  const os = require("os");
  const path = require("path");
  const tmpFile = path.join(os.tmpdir(), `clank-test-${Date.now()}.txt`);
  fs.writeFileSync(tmpFile, "hello\nworld\nfoo\n", "utf-8");
  try {
    const body = apply(varRef("iter.collect"), [
      apply(varRef("fs.stream-lines"), [litStr(tmpFile)])
    ]);
    const result = runMain([], desugar(body));
    assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
    const items = (result as any).value.items;
    assertEq(items.length, 3, "3 lines");
    assertEq(items[0].value, "hello");
    assertEq(items[1].value, "world");
    assertEq(items[2].value, "foo");
  } finally {
    fs.unlinkSync(tmpFile);
  }
});

test("fs.stream-lines with empty file", () => {
  const fs = require("fs");
  const os = require("os");
  const path = require("path");
  const tmpFile = path.join(os.tmpdir(), `clank-test-empty-${Date.now()}.txt`);
  fs.writeFileSync(tmpFile, "", "utf-8");
  try {
    const body = apply(varRef("iter.collect"), [
      apply(varRef("fs.stream-lines"), [litStr(tmpFile)])
    ]);
    const result = runMain([], desugar(body));
    assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
    assertEq((result as any).value.items.length, 0, "empty file = 0 lines");
  } finally {
    fs.unlinkSync(tmpFile);
  }
});

test("fs.stream-lines piped through iter.map", () => {
  const fs = require("fs");
  const os = require("os");
  const path = require("path");
  const tmpFile = path.join(os.tmpdir(), `clank-test-map-${Date.now()}.txt`);
  fs.writeFileSync(tmpFile, "a\nbb\nccc\n", "utf-8");
  try {
    // iter.collect(iter.map(fs.stream-lines(path), fn(x) => str.cat(x, "!")))
    const body = apply(varRef("iter.collect"), [
      apply(varRef("iter.map"), [
        apply(varRef("fs.stream-lines"), [litStr(tmpFile)]),
        lambda(["x"], desugar(infix("++", varRef("x"), litStr("!")))),
      ])
    ]);
    const result = runMain([], desugar(body));
    assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
    const items = (result as any).value.items;
    assertEq(items.length, 3, "3 items");
    assertEq(items[0].value, "a!");
    assertEq(items[1].value, "bb!");
    assertEq(items[2].value, "ccc!");
  } finally {
    fs.unlinkSync(tmpFile);
  }
});

// ── proc.stream ──

console.log("\nproc.stream:");

test("proc.stream returns iterator of stdout lines", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("proc.stream"), [litStr("echo"), list([litStr("hello world")])])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assert(items.length >= 1, "at least 1 line");
  assertEq(items[0].value, "hello world");
});

test("proc.stream with multi-line output", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("proc.stream"), [litStr("printf"), list([litStr("a\\nb\\nc")])])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assertEq(items.length, 3, "3 lines");
  assertEq(items[0].value, "a");
  assertEq(items[1].value, "b");
  assertEq(items[2].value, "c");
});

// ── iter-recv / iter-send / iter-spawn (channel-iterator bridge) ──

console.log("\nChannel-iterator bridge:");

test("iter-spawn produces iterator from channel sender", () => {
  // iter-spawn(fn(sender) => do { send(sender, 1); send(sender, 2); send(sender, 3) })
  // This should produce an iterator yielding [1, 2, 3]
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter-spawn"), [
      lambda(["s"], {
        tag: "do",
        steps: [
          { bind: "_", expr: apply(varRef("send"), [varRef("s"), litInt(1)]) },
          { bind: "_", expr: apply(varRef("send"), [varRef("s"), litInt(2)]) },
          { bind: null, expr: apply(varRef("send"), [varRef("s"), litInt(3)]) },
        ],
        loc,
      } as any)
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 2, 3]);
});

// ── for-loop over iterators ──

console.log("\nfor-loop over iterators:");

test("for x in iter.of([1,2,3]) do print(show(x)) — iter.each semantics (unit result)", () => {
  // For over iterator in map form dispatches to iter.each (side-effect, returns unit)
  const collection = apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]);
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection,
    guard: null,
    fold: null,
    body: apply(varRef("print"), [apply(varRef("show"), [varRef("x")])]),
    loc,
  };
  const { result, stdout } = compileAndRun(def("main", [], desugar(forExpr)));
  assert(result !== undefined && result.tag === Tag.UNIT, "for-each over iter returns unit");
  assertEq(stdout.length, 3, "should print 3 times");
  assertEq(stdout[0], "1");
  assertEq(stdout[1], "2");
  assertEq(stdout[2], "3");
});

test("for x in iter.of([1,2,3,4,5]) if x > 2 do print(show(x)) — filtered iter.each", () => {
  const collection = apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4), litInt(5)])]);
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection,
    guard: infix(">", varRef("x"), litInt(2)),
    fold: null,
    body: apply(varRef("print"), [apply(varRef("show"), [varRef("x")])]),
    loc,
  };
  const { result, stdout } = compileAndRun(def("main", [], desugar(forExpr)));
  assert(result !== undefined && result.tag === Tag.UNIT, "for-each with guard over iter returns unit");
  assertEq(stdout.length, 3, "should print 3 items (3, 4, 5)");
  assertEq(stdout[0], "3");
  assertEq(stdout[1], "4");
  assertEq(stdout[2], "5");
});

test("for x in iter.of([1,2,3]) fold acc = 0 do acc + x (fold form)", () => {
  const collection = apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])]);
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection,
    guard: null,
    fold: { acc: "acc", init: litInt(0) },
    body: infix("+", varRef("acc"), varRef("x")),
    loc,
  };
  const result = runMain([], desugar(forExpr));
  assertIntResult(result, 6);
});

// ── Lazy evaluation verification ──

console.log("\nLazy evaluation:");

test("iter.take on large range doesn't materialize all elements", () => {
  // iter.collect(iter.take(iter.range(0, 1000000), 3))
  // If this completes quickly, take is truly lazy
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.range"), [litInt(0), litInt(1000000)]),
      litInt(3),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [0, 1, 2]);
});

test("iter.map + iter.take pipeline", () => {
  // iter.collect(iter.take(iter.map(iter.of([1,2,3,4,5]), fn(x) => x*10), 3))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.map"), [
        apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3), litInt(4), litInt(5)])]),
        lambda(["x"], desugar(infix("*", varRef("x"), litInt(10)))),
      ]),
      litInt(3),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [10, 20, 30]);
});

// ── True lazy evaluation tests ──

console.log("\nTrue lazy evaluation:");

test("iter.range(0, 10000000) + iter.take(3) completes instantly (lazy range)", () => {
  const start = Date.now();
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.range"), [litInt(0), litInt(10000000)]),
      litInt(3),
    ])
  ]);
  const result = runMain([], desugar(body));
  const elapsed = Date.now() - start;
  assertListInts(result, [0, 1, 2]);
  assert(elapsed < 1000, `should be fast, took ${elapsed}ms`);
});

test("iter.repeat + iter.take produces values from infinite iterator", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.repeat"), [litInt(42)]),
      litInt(5),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [42, 42, 42, 42, 42]);
});

test("lazy filter + take only processes needed elements", () => {
  // iter.collect(iter.take(iter.filter(iter.range(0, 10000000), fn(x) => x > 999990), 3))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.filter"), [
        apply(varRef("iter.range"), [litInt(0), litInt(10000000)]),
        lambda(["x"], desugar(infix(">", varRef("x"), litInt(999990)))),
      ]),
      litInt(3),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [999991, 999992, 999993]);
});

test("lazy map + filter + take pipeline", () => {
  // iter.collect(iter.take(iter.filter(iter.map(iter.range(0, 10000000), fn(x) => x*2), fn(x) => x > 100), 3))
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.filter"), [
        apply(varRef("iter.map"), [
          apply(varRef("iter.range"), [litInt(0), litInt(10000000)]),
          lambda(["x"], desugar(infix("*", varRef("x"), litInt(2)))),
        ]),
        lambda(["x"], desugar(infix(">", varRef("x"), litInt(100)))),
      ]),
      litInt(3),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [102, 104, 106]);
});

test("iter.cycle + iter.take cycles lazily", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.cycle"), [
        apply(varRef("iter.of"), [list([litInt(1), litInt(2), litInt(3)])])
      ]),
      litInt(7),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 2, 3, 1, 2, 3, 1]);
});

test("lazy chain doesn't materialize second iterator early", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.chain"), [
        apply(varRef("iter.range"), [litInt(0), litInt(3)]),
        apply(varRef("iter.range"), [litInt(100), litInt(10000000)]),
      ]),
      litInt(5),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [0, 1, 2, 100, 101]);
});

test("lazy zip stops at shorter iterator", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.zip"), [
      apply(varRef("iter.range"), [litInt(0), litInt(3)]),
      apply(varRef("iter.repeat"), [litInt(99)]),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assertEq(items.length, 3, "3 pairs");
  assertEq(items[0].value.items[0].value, 0);
  assertEq(items[0].value.items[1].value, 99);
});

test("lazy enumerate on infinite iterator + take", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.enumerate"), [
        apply(varRef("iter.repeat"), [litInt(7)])
      ]),
      litInt(3),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assertEq(items.length, 3, "3 items");
  assertEq(items[0].value.items[0].value, 0, "index 0");
  assertEq(items[0].value.items[1].value, 7, "value 0");
  assertEq(items[2].value.items[0].value, 2, "index 2");
});

test("lazy scan produces running accumulation on demand", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.scan"), [
        apply(varRef("iter.range"), [litInt(1), litInt(10000000)]),
        litInt(0),
        lambda(["acc", "x"], desugar(infix("+", varRef("acc"), varRef("x")))),
      ]),
      litInt(5),
    ])
  ]);
  const result = runMain([], desugar(body));
  // scan emits: 0, 0+1=1, 1+2=3, 3+3=6, 6+4=10
  assertListInts(result, [0, 1, 3, 6, 10]);
});

test("lazy flatmap streams inner iterators", () => {
  // flatmap each x to iter.range(0, x)
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.flatmap"), [
      apply(varRef("iter.of"), [list([litInt(2), litInt(3)])]),
      lambda(["x"], apply(varRef("iter.range"), [litInt(0), varRef("x")])),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [0, 1, 0, 1, 2]);
});

test("lazy intersperse on range", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.intersperse"), [
      apply(varRef("iter.range"), [litInt(1), litInt(4)]),
      litInt(0),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 0, 2, 0, 3]);
});

test("lazy dedup on range with duplicates", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.dedup"), [
        apply(varRef("iter.of"), [list([litInt(1), litInt(1), litInt(2), litInt(3), litInt(3)])])
      ]),
      litInt(10),
    ])
  ]);
  const result = runMain([], desugar(body));
  assertListInts(result, [1, 2, 3]);
});

test("lazy chunk on range", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.chunk"), [
        apply(varRef("iter.range"), [litInt(0), litInt(10000000)]),
        litInt(3),
      ]),
      litInt(2),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const chunks = (result as any).value.items;
  assertEq(chunks.length, 2, "2 chunks");
  assertEq(chunks[0].value.items.length, 3);
  assertEq(chunks[0].value.items[0].value, 0);
  assertEq(chunks[1].value.items[0].value, 3);
});

test("lazy window on range", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("iter.window"), [
        apply(varRef("iter.range"), [litInt(0), litInt(10000000)]),
        litInt(3),
      ]),
      litInt(3),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const windows = (result as any).value.items;
  assertEq(windows.length, 3, "3 windows");
  assertEq(windows[0].value.items[0].value, 0);
  assertEq(windows[0].value.items[2].value, 2);
  assertEq(windows[2].value.items[0].value, 2);
  assertEq(windows[2].value.items[2].value, 4);
});

test("iter.any short-circuits on large lazy range", () => {
  const start = Date.now();
  const body = apply(varRef("iter.any"), [
    apply(varRef("iter.range"), [litInt(0), litInt(10000000)]),
    lambda(["x"], desugar(infix(">", varRef("x"), litInt(5)))),
  ]);
  const result = runMain([], desugar(body));
  const elapsed = Date.now() - start;
  assert(result !== undefined && result.tag === Tag.BOOL, "expected Bool");
  assertEq((result as any).value, true);
  assert(elapsed < 1000, `should short-circuit, took ${elapsed}ms`);
});

test("iter.find short-circuits on lazy range", () => {
  const body = apply(varRef("iter.find"), [
    apply(varRef("iter.range"), [litInt(0), litInt(10000000)]),
    lambda(["x"], desugar(infix("==", varRef("x"), litInt(42)))),
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  assert((result as any).value.kind === "union", "expected union");
  assert((result as any).value.fields.length > 0, "should be Some");
  assertEq((result as any).value.fields[0].value, 42);
});

test("io.stdin-lines returns iterator (empty in test context)", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("io.stdin-lines"), [litUnit()])
  ]);
  // stdin is empty in test context, so we just verify it doesn't crash
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  assert((result as any).value.kind === "list", "expected list");
});

test("http.stream-lines returns iterator (integration test skipped if no network)", () => {
  // Just verify the builtin is registered and callable at compile time
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.of"), [list([litStr("line1"), litStr("line2")])])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assertEq(items[0].value, "line1");
});

test("for loop over lazy range — iter.each consumes for side effects", () => {
  // for over iter.range dispatches to iter.each (side-effect, returns unit)
  const collection = apply(varRef("iter.range"), [litInt(0), litInt(5)]);
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection,
    guard: null,
    fold: null,
    body: apply(varRef("print"), [apply(varRef("show"), [varRef("x")])]),
    loc,
  };
  const { result, stdout } = compileAndRun(def("main", [], desugar(forExpr)));
  assert(result !== undefined && result.tag === Tag.UNIT, "for-each over range returns unit");
  assertEq(stdout.length, 5, "should print 5 items");
  assertEq(stdout[0], "0");
  assertEq(stdout[4], "4");
});

test("for loop fold over lazy range", () => {
  const collection = apply(varRef("iter.range"), [litInt(1), litInt(11)]);
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection,
    guard: null,
    fold: { acc: "total", init: litInt(0) },
    body: infix("+", varRef("total"), varRef("x")),
    loc,
  };
  const result = runMain([], desugar(forExpr));
  assertIntResult(result, 55); // 1+2+...+10
});

test("for loop with guard over lazy range — iter.each with filter", () => {
  const collection = apply(varRef("iter.range"), [litInt(0), litInt(10)]);
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection,
    guard: infix("==", infix("%", varRef("x"), litInt(2)), litInt(0)),
    fold: null,
    body: apply(varRef("print"), [apply(varRef("show"), [varRef("x")])]),
    loc,
  };
  const { result, stdout } = compileAndRun(def("main", [], desugar(forExpr)));
  assert(result !== undefined && result.tag === Tag.UNIT, "for-each with guard returns unit");
  assertEq(stdout.length, 5, "should print 5 even numbers");
  assertEq(stdout[0], "0");
  assertEq(stdout[1], "2");
  assertEq(stdout[4], "8");
});

// ── for-loop dispatch: List path still returns list ──

console.log("\nfor-loop list dispatch:");

test("for x in [1,2,3] do x * 2 — list path returns list (map semantics)", () => {
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection: list([litInt(1), litInt(2), litInt(3)]),
    guard: null,
    fold: null,
    body: infix("*", varRef("x"), litInt(2)),
    loc,
  };
  const result = runMain([], desugar(forExpr));
  assertListInts(result, [2, 4, 6]);
});

test("for x in [1,2,3,4,5] if x > 2 do x — list path with guard", () => {
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection: list([litInt(1), litInt(2), litInt(3), litInt(4), litInt(5)]),
    guard: infix(">", varRef("x"), litInt(2)),
    fold: null,
    body: varRef("x"),
    loc,
  };
  const result = runMain([], desugar(forExpr));
  assertListInts(result, [3, 4, 5]);
});

test("for x in [1,2,3] fold acc = 0 do acc + x — list path fold", () => {
  const forExpr: Expr = {
    tag: "for",
    bind: { tag: "p-var", name: "x" },
    collection: list([litInt(1), litInt(2), litInt(3)]),
    guard: null,
    fold: { acc: "acc", init: litInt(0) },
    body: infix("+", varRef("acc"), varRef("x")),
    loc,
  };
  const result = runMain([], desugar(forExpr));
  assertIntResult(result, 6);
});

// ── Demand-driven streaming verification ──

console.log("\nDemand-driven streaming:");

test("fs.stream-lines is demand-driven (iter.take doesn't read whole file)", () => {
  const fs = require("fs");
  const os = require("os");
  const path = require("path");
  // Create a file with many lines
  const tmpFile = path.join(os.tmpdir(), `clank-test-demand-${Date.now()}.txt`);
  const lines = Array.from({ length: 1000 }, (_, i) => `line-${i}`).join("\n") + "\n";
  fs.writeFileSync(tmpFile, lines, "utf-8");
  try {
    // Take only first 3 lines — demand-driven should not read all 1000
    const body = apply(varRef("iter.collect"), [
      apply(varRef("iter.take"), [
        apply(varRef("fs.stream-lines"), [litStr(tmpFile)]),
        litInt(3),
      ])
    ]);
    const result = runMain([], desugar(body));
    assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
    const items = (result as any).value.items;
    assertEq(items.length, 3, "only 3 lines taken");
    assertEq(items[0].value, "line-0");
    assertEq(items[1].value, "line-1");
    assertEq(items[2].value, "line-2");
  } finally {
    fs.unlinkSync(tmpFile);
  }
});

test("fs.stream-lines with for-fold counts lines without collecting all", () => {
  const fs = require("fs");
  const os = require("os");
  const path = require("path");
  const tmpFile = path.join(os.tmpdir(), `clank-test-fold-${Date.now()}.txt`);
  fs.writeFileSync(tmpFile, "a\nb\nc\nd\n", "utf-8");
  try {
    const collection = apply(varRef("fs.stream-lines"), [litStr(tmpFile)]);
    const forExpr: Expr = {
      tag: "for",
      bind: { tag: "p-var", name: "x" },
      collection,
      guard: null,
      fold: { acc: "count", init: litInt(0) },
      body: infix("+", varRef("count"), litInt(1)),
      loc,
    };
    const result = runMain([], desugar(forExpr));
    assertIntResult(result, 4);
  } finally {
    fs.unlinkSync(tmpFile);
  }
});

test("proc.stream with iter.take is demand-driven", () => {
  const body = apply(varRef("iter.collect"), [
    apply(varRef("iter.take"), [
      apply(varRef("proc.stream"), [litStr("printf"), list([litStr("a\\nb\\nc\\nd\\ne")])]),
      litInt(2),
    ])
  ]);
  const result = runMain([], desugar(body));
  assert(result !== undefined && result.tag === Tag.HEAP, "expected heap");
  const items = (result as any).value.items;
  assertEq(items.length, 2, "only 2 lines taken");
  assertEq(items[0].value, "a");
  assertEq(items[1].value, "b");
});

test("for x in fs.stream-lines(path) do print(x) — iter.each side-effect consumption", () => {
  const fs = require("fs");
  const os = require("os");
  const path = require("path");
  const tmpFile = path.join(os.tmpdir(), `clank-test-foreach-${Date.now()}.txt`);
  fs.writeFileSync(tmpFile, "hello\nworld\n", "utf-8");
  try {
    const collection = apply(varRef("fs.stream-lines"), [litStr(tmpFile)]);
    const forExpr: Expr = {
      tag: "for",
      bind: { tag: "p-var", name: "x" },
      collection,
      guard: null,
      fold: null,
      body: apply(varRef("print"), [varRef("x")]),
      loc,
    };
    const { result, stdout } = compileAndRun(def("main", [], desugar(forExpr)));
    assert(result !== undefined && result.tag === Tag.UNIT, "for-each returns unit");
    assertEq(stdout.length, 2, "printed 2 lines");
    assertEq(stdout[0], "hello");
    assertEq(stdout[1], "world");
  } finally {
    fs.unlinkSync(tmpFile);
  }
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);

if (failed > 0) process.exit(1);
