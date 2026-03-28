// Embedding API integration tests
// Run with: npx tsx test/embedding.test.ts

import { writeFileSync, unlinkSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { ClankRuntime } from "../src/embedding.js";

const __dirname = dirname(fileURLToPath(import.meta.url));

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

// ── Tests ──

console.log("Embedding API tests:");

test("create and dispose runtime", () => {
  const rt = ClankRuntime.create();
  rt.dispose();
  assert(true, "should not throw");
});

test("disposed runtime throws on use", () => {
  const rt = ClankRuntime.create();
  rt.dispose();
  let threw = false;
  try {
    rt.loadModule("pub main : () -> <> Int = 42");
  } catch (e: any) {
    threw = true;
    assert(e.message.includes("disposed"), "should mention disposed");
  }
  assert(threw, "should have thrown");
});

test("load and call a pure function returning Int", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub add-two : (x: Int) -> <> Int = x + 2
  `);
  const result = rt.call("add-two", rt.toClank(5));
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 7, "5 + 2 = 7");
  rt.dispose();
});

test("load and call a function returning Str", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub greet : (name: Str) -> <> Str = "hello " ++ name
  `);
  const result = rt.call("greet", rt.toClank("world"));
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, "hello world");
  rt.dispose();
});

test("load and call a function returning Bool", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub is-positive : (n: Int) -> <> Bool = n > 0
  `);
  const result = rt.call("is-positive", rt.toClank(5));
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, true);

  const result2 = rt.call("is-positive", rt.toClank(-3));
  assert(result2.ok, `call should succeed: ${result2.error?.message}`);
  assertEq(result2.value?.value, false);
  rt.dispose();
});

test("call nonexistent function returns error", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`pub main : () -> <> Int = 1`);
  const result = rt.call("nonexistent");
  assert(!result.ok, "should fail");
  assert(result.error!.message.includes("not found"), "should say not found");
  rt.dispose();
});

test("call without loading module returns error", () => {
  const rt = ClankRuntime.create();
  const result = rt.call("main");
  assert(!result.ok, "should fail");
  assert(result.error!.message.includes("No module"), "should say no module");
  rt.dispose();
});

test("toClank and toJS roundtrip for primitives", () => {
  const rt = ClankRuntime.create();

  const intVal = rt.toClank(42);
  assertEq(intVal.type, "Int");
  assertEq(rt.toJS(intVal), 42);

  const strVal = rt.toClank("hello");
  assertEq(strVal.type, "Str");
  assertEq(rt.toJS(strVal), "hello");

  const boolVal = rt.toClank(true);
  assertEq(boolVal.type, "Bool");
  assertEq(rt.toJS(boolVal), true);

  const ratVal = rt.toClank(3.14, "Rat");
  assertEq(ratVal.type, "Rat");
  assertEq(rt.toJS(ratVal), 3.14);

  const unitVal = rt.toClank(null);
  assertEq(unitVal.type, "()");

  rt.dispose();
});

test("register host function and call from Clank", () => {
  const rt = ClankRuntime.create();

  rt.registerModule("mylib", {
    "greet": {
      sig: "(name: Str) -> <> Str",
      impl: (name: string) => `Hello, ${name}!`,
    },
  });

  rt.loadModule(`
    extern "mylib" greet : (name: Str) -> <> Str
    pub main : () -> <> Str = greet("World")
  `);

  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, "Hello, World!");
  rt.dispose();
});

test("register host function with numeric return", () => {
  const rt = ClankRuntime.create();

  rt.registerModule("math", {
    "double": {
      sig: "(n: Int) -> <> Int",
      impl: (n: number) => n * 2,
    },
  });

  rt.loadModule(`
    extern "math" double : (n: Int) -> <> Int
    pub main : () -> <> Int = double(21)
  `);

  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 42);
  rt.dispose();
});

test("host function that throws produces error result", () => {
  const rt = ClankRuntime.create();

  rt.registerModule("mylib", {
    "fail": {
      sig: "() -> <> Int",
      impl: () => { throw new Error("host error"); },
    },
  });

  rt.loadModule(`
    extern "mylib" fail : () -> <> Int
    pub main : () -> <> Int = fail()
  `);

  const result = rt.call("main");
  assert(!result.ok, "call should fail");
  assert(result.error!.message.includes("host error"), "should contain error message");
  rt.dispose();
});

test("multiple function calls on same runtime", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub double : (x: Int) -> <> Int = x * 2
    pub triple : (x: Int) -> <> Int = x * 3
  `);

  const r1 = rt.call("double", rt.toClank(5));
  assert(r1.ok, `double should succeed: ${r1.error?.message}`);
  assertEq(r1.value?.value, 10);

  const r2 = rt.call("triple", rt.toClank(5));
  assert(r2.ok, `triple should succeed: ${r2.error?.message}`);
  assertEq(r2.value?.value, 15);

  rt.dispose();
});

test("stdout capture from print calls", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub main : () -> <io> () = print("hello from clank")
  `);
  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assert(result.stdout.length > 0, "should have stdout output");
  assert(result.stdout.some(l => l.includes("hello from clank")), "should contain message");
  rt.dispose();
});

test("host function with multiple args", () => {
  const rt = ClankRuntime.create();

  rt.registerModule("mylib", {
    "add": {
      sig: "(a: Int, b: Int) -> <> Int",
      impl: (a: number, b: number) => a + b,
    },
  });

  rt.loadModule(`
    extern "mylib" add : (a: Int, b: Int) -> <> Int
    pub main : () -> <> Int = add(10, 32)
  `);

  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 42);
  rt.dispose();
});

test("calling exported function with args from host", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub multiply : (a: Int, b: Int) -> <> Int = a * b
  `);

  const result = rt.call("multiply", rt.toClank(6), rt.toClank(7));
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 42);
  rt.dispose();
});

// ── loadFile ──

test("loadFile loads and runs a .clk file", () => {
  const tmp = join(__dirname, "_embedding_test_tmp.clk");
  writeFileSync(tmp, `pub square : (n: Int) -> <> Int = n * n`);
  try {
    const rt = ClankRuntime.create();
    const mod = rt.loadFile(tmp);
    assertEq(mod.name, "_embedding_test_tmp");
    const result = rt.call("square", rt.toClank(9));
    assert(result.ok, `call should succeed: ${result.error?.message}`);
    assertEq(result.value?.value, 81);
    rt.dispose();
  } finally {
    unlinkSync(tmp);
  }
});

// ── register() single function ──

test("register single host function", () => {
  const rt = ClankRuntime.create();
  rt.register("negate", "(n: Int) -> <> Int", (n: number) => -n);
  rt.loadModule(`
    extern "host" negate : (n: Int) -> <> Int
    pub main : () -> <> Int = negate(42)
  `);
  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, -42);
  rt.dispose();
});

test("register single host function with custom library", () => {
  const rt = ClankRuntime.create();
  rt.register("add-one", "(n: Int) -> <> Int", (n: number) => n + 1, "utils");
  rt.loadModule(`
    extern "utils" add-one : (n: Int) -> <> Int
    pub main : () -> <> Int = add-one(99)
  `);
  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 100);
  rt.dispose();
});

// ── Recursive Clank function called from host ──

test("call recursive Clank function from host", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub factorial : (n: Int) -> <> Int =
      if n == 0 then 1 else n * factorial(n - 1)
  `);
  const result = rt.call("factorial", rt.toClank(10));
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 3628800);
  rt.dispose();
});

// ── Value conversion: lists ──

test("pass list from host to Clank and get list back", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub sum-list : (xs: [Int]) -> <> Int =
      fold(xs, 0, fn(acc, x) => acc + x)
  `);
  const result = rt.call("sum-list", rt.toClank([1, 2, 3, 4, 5]));
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 15);
  rt.dispose();
});

test("Clank function returning list converts to JS array", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub make-list : () -> <> [Int] = [10, 20, 30]
  `);
  const result = rt.call("make-list");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  const arr = result.value?.value as number[];
  assert(Array.isArray(arr), "should be array");
  assertEq(arr.length, 3);
  assertEq(arr[0], 10);
  assertEq(arr[1], 20);
  assertEq(arr[2], 30);
  rt.dispose();
});

// ── Value conversion: records ──

test("Clank function returning record converts to JS object", () => {
  const rt = ClankRuntime.create();
  rt.loadModule(`
    pub make-pair : () -> <> {x: Int, y: Int} = {x: 1, y: 2}
  `);
  const result = rt.call("make-pair");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  const obj = result.value?.value as Record<string, number>;
  assertEq(obj.x, 1);
  assertEq(obj.y, 2);
  rt.dispose();
});

// ── Host function receiving values from Clank and returning them ──

test("host function processes Clank values and returns compound result", () => {
  const rt = ClankRuntime.create();
  rt.registerModule("transform", {
    "reverse-str": {
      sig: "(s: Str) -> <> Str",
      impl: (s: string) => s.split("").reverse().join(""),
    },
  });
  rt.loadModule(`
    extern "transform" reverse-str : (s: Str) -> <> Str
    pub main : () -> <> Str = reverse-str("abcdef")
  `);
  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, "fedcba");
  rt.dispose();
});

// ── Chaining: Clank calls host which feeds back into Clank logic ──

test("Clank code calls host function and uses result in computation", () => {
  const rt = ClankRuntime.create();
  rt.registerModule("env", {
    "get-multiplier": {
      sig: "() -> <> Int",
      impl: () => 7,
    },
  });
  rt.loadModule(`
    extern "env" get-multiplier : () -> <> Int
    pub compute : (x: Int) -> <> Int = x * get-multiplier()
  `);
  const result = rt.call("compute", rt.toClank(6));
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, 42);
  rt.dispose();
});

// ── Multiple host modules ──

test("multiple host modules registered simultaneously", () => {
  const rt = ClankRuntime.create();
  rt.registerModule("math", {
    "add": { sig: "(a: Int, b: Int) -> <> Int", impl: (a: number, b: number) => a + b },
  });
  rt.registerModule("str-utils", {
    "upper": { sig: "(s: Str) -> <> Str", impl: (s: string) => s.toUpperCase() },
  });
  rt.loadModule(`
    extern "math" add : (a: Int, b: Int) -> <> Int
    extern "str-utils" upper : (s: Str) -> <> Str
    pub main : () -> <> Str = upper("result: " ++ show(add(20, 22)))
  `);
  const result = rt.call("main");
  assert(result.ok, `call should succeed: ${result.error?.message}`);
  assertEq(result.value?.value, "RESULT: 42");
  rt.dispose();
});

// ── toClank array type hint ──

test("toClank with array preserves values", () => {
  const rt = ClankRuntime.create();
  const cv = rt.toClank([1, 2, 3]);
  assertEq(cv.type, "[Int]");
  const arr = rt.toJS(cv) as number[];
  assert(Array.isArray(arr), "should be array");
  assertEq(arr.length, 3);
  rt.dispose();
});

// ── Summary ──

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
