// Tests for type checker hardening (TASK-157)
// Composite literal inference, HM let-polymorphism, builtin method types
// Run with: npx tsx test/checker-hardening.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, unlinkSync, mkdirSync } from "node:fs";
import { join } from "node:path";

const CLI = join(import.meta.dirname, "..", "ts", "src", "main.ts");
const TMP_DIR = "/tmp/clank-checker-hardening-test";

let passed = 0;
let failed = 0;

mkdirSync(TMP_DIR, { recursive: true });

function test(name: string, fn: () => void): void {
  try {
    fn();
    passed++;
    console.log(`  ok - ${name}`);
  } catch (e: unknown) {
    failed++;
    console.log(`  FAIL - ${name}`);
    console.log(`    ${(e as Error).message}`);
  }
}

function assert(cond: boolean, msg: string): void {
  if (!cond) throw new Error(msg);
}

function assertEqual(a: unknown, b: unknown, msg: string): void {
  if (a !== b) throw new Error(`${msg}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
}

function runCLI(args: string): { stdout: string; stderr: string; exitCode: number } {
  try {
    const stdout = execSync(`npx tsx ${CLI} ${args}`, {
      encoding: "utf-8",
      stdio: ["pipe", "pipe", "pipe"],
    });
    return { stdout: stdout.trim(), stderr: "", exitCode: 0 };
  } catch (e: any) {
    return {
      stdout: (e.stdout ?? "").trim(),
      stderr: (e.stderr ?? "").trim(),
      exitCode: e.status ?? 1,
    };
  }
}

function writeTmp(name: string, content: string): string {
  const path = join(TMP_DIR, name);
  writeFileSync(path, content);
  return path;
}

function cleanTmp(name: string): void {
  try { unlinkSync(join(TMP_DIR, name)); } catch {}
}

console.log("# checker hardening tests (TASK-157)");

// ── Composite literal inference: for comprehension ──

test("for comprehension infers list element type", () => {
  const src = `
double-all : (xs: [Int]) -> <> [Int] =
  for x in xs do mul(x, 2)

main : () -> <io> () = print(show(len(double-all([1, 2, 3]))))
`;
  const f = writeTmp("for-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("for-infer.clk");
});

test("for-fold infers accumulator type", () => {
  const src = `
sum-list : (xs: [Int]) -> <> Int =
  for x in xs fold acc = 0 do add(acc, x)

main : () -> <io> () = print(show(sum-list([1, 2, 3])))
`;
  const f = writeTmp("for-fold-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("for-fold-infer.clk");
});

// ── Composite literal inference: range ──

test("range infers [Int]", () => {
  const src = `
first-ten : () -> <> [Int] =
  range(1, 10)

main : () -> <io> () = print(show(len(first-ten())))
`;
  const f = writeTmp("range-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("range-infer.clk");
});

// ── Composite literal inference: do block ──

test("do block infers type of last step", () => {
  const src = `
greet : (name: Str) -> <io> Str = do {
  print(str.cat("hello ", name))
  str.cat("greeted: ", name)
}

main : () -> <io> () = print(greet("world"))
`;
  const f = writeTmp("do-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("do-infer.clk");
});

// ── Composite literal inference: let-pattern ──

test("let-pattern infers destructured types", () => {
  const src = `
get-first : (pair: (Int, Str)) -> <> Int =
  let (a, b) = pair
  a

main : () -> <io> () = print(show(get-first((1, "hello"))))
`;
  const f = writeTmp("let-pat-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("let-pat-infer.clk");
});

// ── Composite return refinement: map infers element type ──

test("map infers return list element type from callback", () => {
  const src = `
double-all : (xs: [Int]) -> <> [Int] =
  map(xs, fn(x) => mul(x, 2))

main : () -> <io> () = print(show(len(double-all([1, 2, 3]))))
`;
  const f = writeTmp("map-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("map-infer.clk");
});

test("fold infers return type from init value", () => {
  const src = `
sum-list : (xs: [Int]) -> <> Int =
  fold(xs, 0, fn(acc, x) => add(acc, x))

main : () -> <io> () = print(show(sum-list([1, 2, 3])))
`;
  const f = writeTmp("fold-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("fold-infer.clk");
});

test("zip infers tuple element types", () => {
  const src = `
pair-up : (as: [Int], bs: [Str]) -> <> [(Int, Str)] =
  zip(as, bs)

main : () -> <io> () = print(show(len(pair-up([1, 2], ["a", "b"]))))
`;
  const f = writeTmp("zip-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("zip-infer.clk");
});

// ── HM let-polymorphism ──

test("let-polymorphism: identity used at multiple types", () => {
  const src = `
test-poly : () -> <io> () =
  let id = fn(x) => x
  let a = id(42)
  let b = id("hello")
  print(str.cat(show(a), b))

main : () -> <io> () = test-poly()
`;
  const f = writeTmp("let-poly.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("let-poly.clk");
});

test("let-polymorphism: const function used at multiple types", () => {
  const src = `
test-const : () -> <io> () =
  let const = fn(x, y) => x
  let a = const(1, "ignored")
  let b = const("hello", 42)
  print(str.cat(show(a), b))

main : () -> <io> () = test-const()
`;
  const f = writeTmp("let-poly-const.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("let-poly-const.clk");
});

// ── Builtin interface method types ──

test("clone returns same type as argument", () => {
  const src = `
clone-int : (x: Int) -> <> Int = clone(x)

main : () -> <io> () = print(show(clone-int(42)))
`;
  const f = writeTmp("clone-type.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("clone-type.clk");
});

test("clone of Str returns Str (not tAny)", () => {
  const src = `
clone-str : (s: Str) -> <> Str = clone(s)

main : () -> <io> () = print(clone-str("hi"))
`;
  const f = writeTmp("clone-str.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("clone-str.clk");
});

// ── HM type variable unification through builtins ──

test("eq unifies both arguments to same type via HM", () => {
  const src = `
same-type : (a: Int, b: Int) -> <> Bool = eq(a, b)

main : () -> <io> () = print(show(same-type(1, 2)))
`;
  const f = writeTmp("eq-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("eq-hm.clk");
});

test("head infers element type through HM unification", () => {
  const src = `
first : (xs: [Int]) -> <> Int = head(xs)

main : () -> <io> () = print(show(first([1, 2, 3])))
`;
  const f = writeTmp("head-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("head-hm.clk");
});

test("head of list literal infers Int through unification", () => {
  const src = `
first-lit : () -> <> Int = head([10, 20, 30])

main : () -> <io> () = print(show(first-lit()))
`;
  const f = writeTmp("head-lit-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("head-lit-hm.clk");
});

test("cons preserves element type through HM", () => {
  const src = `
prepend : (x: Int, xs: [Int]) -> <> [Int] = cons(x, xs)

main : () -> <io> () = print(show(len(prepend(1, [2, 3]))))
`;
  const f = writeTmp("cons-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("cons-hm.clk");
});

test("filter preserves list element type through HM", () => {
  const src = `
positives : (xs: [Int]) -> <> [Int] =
  filter(xs, fn(x) => gt(x, 0))

main : () -> <io> () = print(show(len(positives([1, 2, 3]))))
`;
  const f = writeTmp("filter-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("filter-hm.clk");
});

test("fst infers first tuple element type", () => {
  const src = `
get-name : (pair: (Str, Int)) -> <> Str = fst(pair)

main : () -> <io> () = print(get-name(("Alice", 30)))
`;
  const f = writeTmp("fst-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("fst-hm.clk");
});

test("snd infers second tuple element type", () => {
  const src = `
get-age : (pair: (Str, Int)) -> <> Int = snd(pair)

main : () -> <io> () = print(show(get-age(("Alice", 30))))
`;
  const f = writeTmp("snd-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("snd-hm.clk");
});

// ── HM let-polymorphism advanced ──

test("let-polymorphism: apply-fn used at multiple types", () => {
  const src = `
test-apply : () -> <io> () =
  let apply-fn = fn(f, x) => f(x)
  let a = apply-fn(fn(x) => add(x, 1), 42)
  let b = apply-fn(fn(x) => str.cat(x, "!"), "hello")
  print(str.cat(show(a), b))

main : () -> <io> () = test-apply()
`;
  const f = writeTmp("let-poly-apply.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("let-poly-apply.clk");
});

test("let-polymorphism: compose used at multiple types", () => {
  const src = `
test-compose : () -> <io> () =
  let twice = fn(f, x) => f(f(x))
  let a = twice(fn(x) => add(x, 1), 0)
  let b = twice(fn(x) => str.cat(x, "!"), "hi")
  print(str.cat(show(a), b))

main : () -> <io> () = test-compose()
`;
  const f = writeTmp("let-poly-compose.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("let-poly-compose.clk");
});

// ── Composite inference: chained operations ──

test("chained list ops preserve type: filter then map", () => {
  const src = `
transform : (xs: [Int]) -> <> [Int] =
  map(filter(xs, fn(x) => gt(x, 0)), fn(x) => mul(x, 2))

main : () -> <io> () = print(show(len(transform([1, 2, 3]))))
`;
  const f = writeTmp("chain-ops.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("chain-ops.clk");
});

test("rev preserves element type", () => {
  const src = `
reversed : (xs: [Int]) -> <> [Int] = rev(xs)

main : () -> <io> () = print(show(len(reversed([1, 2, 3]))))
`;
  const f = writeTmp("rev-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("rev-hm.clk");
});

test("cat preserves element type", () => {
  const src = `
combined : (a: [Int], b: [Int]) -> <> [Int] = cat(a, b)

main : () -> <io> () = print(show(len(combined([1], [2]))))
`;
  const f = writeTmp("cat-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("cat-hm.clk");
});

test("get infers element type from list", () => {
  const src = `
second : (xs: [Int]) -> <> Int = get(xs, 1)

main : () -> <io> () = print(show(second([10, 20, 30])))
`;
  const f = writeTmp("get-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("get-hm.clk");
});

test("flat-map infers nested list element type", () => {
  const src = `
expand : (xs: [Int]) -> <> [Int] =
  flat-map(xs, fn(x) => [x, mul(x, 2)])

main : () -> <io> () = print(show(len(expand([1, 2]))))
`;
  const f = writeTmp("flatmap-hm.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("flatmap-hm.clk");
});

// ── Summary ──

console.log(`\nResults: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
