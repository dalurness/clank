// Tests for clank check subcommand
// Run with: npx tsx test/check.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, unlinkSync, mkdirSync, rmSync } from "node:fs";
import { join } from "node:path";

const CLI = join(import.meta.dirname, "..", "src", "main.ts");
const TMP_DIR = "/tmp/clank-check-test";

let passed = 0;
let failed = 0;

// Setup temp dir
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

function validateEnvelope(obj: any): void {
  assert(typeof obj === "object" && obj !== null, "envelope is an object");
  assert(typeof obj.ok === "boolean", "envelope.ok is boolean");
  assert(Array.isArray(obj.diagnostics), "envelope.diagnostics is array");
  assert(typeof obj.timing === "object", "envelope.timing is object");
  assert(typeof obj.timing.total_ms === "number", "envelope.timing.total_ms is number");
  assert(typeof obj.timing.phases === "object", "envelope.timing.phases is object");
}

// ── Tests ──

console.log("# check subcommand tests");

// Basic: valid file passes
test("valid file exits 0", () => {
  const f = writeTmp("valid.clk", `main : () -> <io> () = print("hello")\n`);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("valid.clk");
});

// Valid file with --json
test("valid file --json produces ok:true envelope", () => {
  const f = writeTmp("valid-json.clk", `main : () -> <io> () = print("hello")\n`);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, true, "ok");
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  assert(env.data !== null, "data present");
  assert(Array.isArray(env.data.files), "data.files is array");
  cleanTmp("valid-json.clk");
});

// Phase timing present
test("--json output includes phase timing", () => {
  const f = writeTmp("timing.clk", `main : () -> <io> () = print("hi")\n`);
  const { stdout } = runCLI(`--json check ${f}`);
  const env = JSON.parse(stdout);
  assert(typeof env.timing.phases.lex === "number", "lex timing present");
  assert(typeof env.timing.phases.parse === "number", "parse timing present");
  assert(typeof env.timing.phases.check === "number", "check timing present");
  cleanTmp("timing.clk");
});

// Type error causes exit 1
test("type error exits 1", () => {
  const f = writeTmp("type-err.clk", `main : () -> <io> () = print(undefined_var)\n`);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  cleanTmp("type-err.clk");
});

// Type error --json
test("type error --json produces ok:false with diagnostics", () => {
  const f = writeTmp("type-err-json.clk", `main : () -> <io> () = print(undefined_var)\n`);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  const d = env.diagnostics[0];
  assertEqual(d.severity, "error", "severity");
  assertEqual(d.phase, "check", "phase");
  assertEqual(d.code, "E300", "type error code");
  assert(d.message.includes("undefined_var"), "message mentions variable");
  cleanTmp("type-err-json.clk");
});

// Lex error
test("lex error exits 1 with diagnostic", () => {
  const f = writeTmp("lex-err.clk", `main : () -> <io> () = §\n`);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  assertEqual(env.diagnostics[0].phase, "lex", "phase is lex");
  cleanTmp("lex-err.clk");
});

// Parse error
test("parse error exits 1 with diagnostic", () => {
  const f = writeTmp("parse-err.clk", `main : () -> <io> () = if\n`);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  assertEqual(env.diagnostics[0].phase, "parse", "phase is parse");
  cleanTmp("parse-err.clk");
});

// Warnings don't cause failure
test("warnings only still exits 0", () => {
  const src = `
type Color = Red | Green | Blue

check-red : (c: Color) -> <> Str =
  match c {
    Red => "red"
  }

main : () -> <io> () = print(check-red(Red))
`;
  const f = writeTmp("warn.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.ok, true, "ok");
  const warnings = env.diagnostics.filter((d: any) => d.severity === "warning");
  assert(warnings.length >= 1, "at least one warning");
  cleanTmp("warn.clk");
});

// No input file
test("no input file exits 1", () => {
  const { exitCode } = runCLI("check");
  assertEqual(exitCode, 1, "exit code");
});

test("no input file --json produces structured error", () => {
  const { stdout, exitCode } = runCLI("--json check");
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
});

// Directory walking
test("directory walking finds .clk files", () => {
  const subdir = join(TMP_DIR, "subdir");
  mkdirSync(subdir, { recursive: true });
  writeFileSync(join(subdir, "a.clk"), `main : () -> <io> () = print("a")\n`);
  writeFileSync(join(subdir, "b.clk"), `main : () -> <io> () = print("b")\n`);
  const { stdout, exitCode } = runCLI(`--json check ${subdir}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.ok, true, "ok");
  assert(env.data.files.length === 2, `found 2 files, got ${env.data.files.length}`);
  rmSync(subdir, { recursive: true });
});

// Multiple files, one with error
test("multiple files with one error exits 1", () => {
  const good = writeTmp("multi-good.clk", `main : () -> <io> () = print("ok")\n`);
  const bad = writeTmp("multi-bad.clk", `main : () -> <io> () = print(nope)\n`);
  const { stdout, exitCode } = runCLI(`--json check ${good} ${bad}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  // Both files should appear in data.files
  assert(env.data.files.length === 2, `2 file results, got ${env.data.files.length}`);
  cleanTmp("multi-good.clk");
  cleanTmp("multi-bad.clk");
});

// Does NOT execute the program (no side effects)
test("check does not execute the program", () => {
  const f = writeTmp("no-exec.clk", `main : () -> <io> () = print("should-not-appear")\n`);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  // data should not have stdout from execution
  assert(!env.data?.stdout, "no stdout from execution");
  cleanTmp("no-exec.clk");
});

// Non-JSON mode: errors go to stderr
test("without --json, errors go to stderr", () => {
  const f = writeTmp("stderr-err.clk", `main : () -> <io> () = print(nope)\n`);
  const { stderr, exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  assert(stderr.includes("unbound variable"), "stderr contains error message");
  cleanTmp("stderr-err.clk");
});

// ── Where-constraint enforcement at call sites (TASK-112) ──

test("where constraint satisfied (Ord Int) passes", () => {
  const src = `
max-val : (a: T, b: T) -> <> T where Ord T =
  if gt(a, b) then a else b

main : () -> <io> () = print(show(max-val(3, 5)))
`;
  const f = writeTmp("constraint-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("constraint-ok.clk");
});

test("where constraint violated emits E205", () => {
  const src = `
interface Sortable {
  sort-key : (Self) -> <> Int
}

max-by : (a: T, b: T) -> <> T where Sortable T =
  if gt(sort-key(a), sort-key(b)) then a else b

main : () -> <io> () = print(show(max-by(3, 5)))
`;
  const f = writeTmp("constraint-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  assertEqual(env.diagnostics[0].code, "E205", "constraint error code");
  assert(env.diagnostics[0].message.includes("Sortable"), "message mentions interface");
  assert(env.diagnostics[0].message.includes("Int"), "message mentions concrete type");
  cleanTmp("constraint-fail.clk");
});

test("where constraint satisfied with custom impl passes", () => {
  const src = `
interface Sortable {
  sort-key : (Self) -> <> Int
}

impl Sortable for Int {
  sort-key = fn(n) => n
}

max-by : (a: T, b: T) -> <> T where Sortable T =
  if gt(sort-key(a), sort-key(b)) then a else b

main : () -> <io> () = print(show(max-by(3, 5)))
`;
  const f = writeTmp("constraint-impl-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("constraint-impl-ok.clk");
});

test("where constraint on list element type is checked", () => {
  const src = `
type Color = Red | Green | Blue

sort-list : (xs: [T]) -> <> [T] where Ord T =
  xs

main : () -> <io> () = print(show(len(sort-list([Red, Green]))))
`;
  const f = writeTmp("constraint-list.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 diagnostic present");
  assert(env.diagnostics.some((d: any) => d.message.includes("Color")), "message mentions Color");
  cleanTmp("constraint-list.clk");
});

// ── Where-constraint enforcement for higher-order / indirect calls (TASK-117) ──

test("where constraint via let-bound alias emits E205", () => {
  const src = `
interface Sortable {
  sort-key : (Self) -> <> Int
}

max-by : (a: T, b: T) -> <> T where Sortable T =
  if gt(sort-key(a), sort-key(b)) then a else b

main : () -> <io> () =
  let f = max-by
  print(show(f(3, 5)))
`;
  const f = writeTmp("constraint-alias-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 diagnostic present");
  assert(env.diagnostics.some((d: any) => d.message.includes("Int")), "message mentions Int");
  cleanTmp("constraint-alias-fail.clk");
});

test("where constraint via let-bound alias passes when satisfied", () => {
  const src = `
max-val : (a: T, b: T) -> <> T where Ord T =
  if gt(a, b) then a else b

main : () -> <io> () =
  let f = max-val
  print(show(f(3, 5)))
`;
  const f = writeTmp("constraint-alias-ok.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assert(!env.diagnostics || env.diagnostics.length === 0, "no diagnostics");
  cleanTmp("constraint-alias-ok.clk");
});

test("where constraint via chained let aliases emits E205", () => {
  const src = `
interface Sortable {
  sort-key : (Self) -> <> Int
}

max-by : (a: T, b: T) -> <> T where Sortable T =
  if gt(sort-key(a), sort-key(b)) then a else b

main : () -> <io> () =
  let f = max-by
  let g = f
  print(show(g(3, 5)))
`;
  const f = writeTmp("constraint-chain-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 diagnostic present");
  cleanTmp("constraint-chain-fail.clk");
});

// ── Row polymorphism (TASK-120) ──

test("record literal infers closed record type", () => {
  const src = `
greet : (name: Str) -> <> Str = name

main : () -> <io> () =
  let rec = {name: "Ada", age: 36}
  print(greet(rec.name))
`;
  const f = writeTmp("record-lit.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("record-lit.clk");
});

test("field access on record literal succeeds", () => {
  const src = `
main : () -> <io> () =
  let rec = {name: "Ada", age: 36}
  print(rec.name)
`;
  const f = writeTmp("field-access-ok.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("field-access-ok.clk");
});

test("field access on missing field emits error", () => {
  const src = `
main : () -> <io> () =
  let rec = {name: "Ada"}
  print(rec.age)
`;
  const f = writeTmp("field-access-missing.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E301"), "E301 diagnostic for missing field");
  assert(env.diagnostics.some((d: any) => d.message.includes("age")), "message mentions missing field name");
  cleanTmp("field-access-missing.clk");
});

test("field access infers correct type for type checking", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let rec = {name: "Ada", age: 36}
  print(show(add-one(rec.age)))
`;
  const f = writeTmp("field-type-infer.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("field-type-infer.clk");
});

test("field access type mismatch emits error", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let rec = {name: "Ada", age: 36}
  print(show(add-one(rec.name)))
`;
  const f = writeTmp("field-type-mismatch.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E304"), "E304 type mismatch");
  cleanTmp("field-type-mismatch.clk");
});

test("record update type checks correctly", () => {
  const src = `
main : () -> <io> () =
  let rec = {name: "Ada", age: 36}
  let updated = {rec | age: 37}
  print(updated.name)
`;
  const f = writeTmp("record-update-ok.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("record-update-ok.clk");
});

test("record update field type mismatch emits error", () => {
  const src = `
main : () -> <io> () =
  let rec = {name: "Ada", age: 36}
  let updated = {rec | age: "old"}
  print(updated.name)
`;
  const f = writeTmp("record-update-mismatch.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E302"), "E302 field type mismatch");
  cleanTmp("record-update-mismatch.clk");
});

test("record update nonexistent field emits error on closed record", () => {
  const src = `
main : () -> <io> () =
  let rec = {name: "Ada"}
  let updated = {rec | age: 37}
  print(updated.name)
`;
  const f = writeTmp("record-update-nofield.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E301"), "E301 missing field");
  cleanTmp("record-update-nofield.clk");
});

test("duplicate field in record literal emits error", () => {
  const src = `
main : () -> <io> () =
  let rec = {name: "Ada", name: "Grace"}
  print(rec.name)
`;
  const f = writeTmp("record-dup-field.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E304"), "E304 duplicate field");
  cleanTmp("record-dup-field.clk");
});

test("empty record literal type checks", () => {
  const src = `
main : () -> <io> () =
  let rec = {}
  print("ok")
`;
  const f = writeTmp("record-empty.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("record-empty.clk");
});

test("nested field access works", () => {
  const src = `
main : () -> <io> () =
  let inner = {x: 1}
  let outer = {child: inner}
  print(show(outer.child.x))
`;
  const f = writeTmp("nested-field.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("nested-field.clk");
});

test("record update preserves all fields for further access", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let rec = {name: "Ada", age: 36}
  let updated = {rec | age: 37}
  print(show(add-one(updated.age)))
`;
  const f = writeTmp("record-update-access.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("record-update-access.clk");
});

// ── From/Into interface and blanket impl checking (TASK-121) ──

test("From impl passes checker", () => {
  const src = `
type Box = Box(Str)

impl From<Str> for Box {
  from = fn(s) => Box(s)
}

main : () -> <io> () = print("ok")
`;
  const f = writeTmp("from-ok.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("from-ok.clk");
});

test("multiple From impls with different type args pass coherence", () => {
  const src = `
type Box = BoxStr(Str) | BoxInt(Int)

impl From<Str> for Box {
  from = fn(s) => BoxStr(s)
}

impl From<Int> for Box {
  from = fn(n) => BoxInt(n)
}

main : () -> <io> () = print("ok")
`;
  const f = writeTmp("from-multi-ok.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("from-multi-ok.clk");
});

test("duplicate From<same type> for same target emits E202", () => {
  const src = `
type Box = Box(Str)

impl From<Str> for Box {
  from = fn(s) => Box(s)
}

impl From<Str> for Box {
  from = fn(s) => Box(s)
}

main : () -> <io> () = print("ok")
`;
  const f = writeTmp("from-dup.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E202"), "E202 coherence violation");
  cleanTmp("from-dup.clk");
});

test("From impl missing method emits E203", () => {
  const src = `
type Box = Box(Str)

impl From<Str> for Box {
}

main : () -> <io> () = print("ok")
`;
  const f = writeTmp("from-missing.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E203"), "E203 missing method");
  cleanTmp("from-missing.clk");
});

// ── Parameterized interface constraint type args at call sites (TASK-128) ──

test("parameterized constraint type args validated at call site", () => {
  const src = `
impl From<Int> for Str {
  from = fn(n) => show(n)
}

convert : (x: T) -> <> Str where Into<Str> T = into(x)

main : () -> <io> () = print(convert(42))
`;
  const f = writeTmp("param-constraint-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("param-constraint-ok.clk");
});

test("parameterized constraint with wrong type args emits E205", () => {
  const src = `
impl From<Str> for Int {
  from = fn(s) => len(s)
}

convert-to-str : (x: T) -> <> Str where Into<Str> T = into(x)

main : () -> <io> () = print(convert-to-str("hello"))
`;
  const f = writeTmp("param-constraint-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 diagnostic present");
  assert(env.diagnostics.some((d: any) => d.message.includes("Into<Str>")), "message mentions parameterized interface");
  cleanTmp("param-constraint-fail.clk");
});

// ── HM type variable unification for generic inference (TASK-128) ──

test("generic function return type is unified with argument type", () => {
  const src = `
identity : (x: T) -> <> T = x
double : (n: Int) -> <> Int = add(n, n)

main : () -> <io> () = print(show(double(identity(3))))
`;
  const f = writeTmp("generic-unify-ok.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("generic-unify-ok.clk");
});

test("generic function return type mismatch detected after unification", () => {
  const src = `
identity : (x: T) -> <> T = x
double : (n: Int) -> <> Int = add(n, n)

main : () -> <io> () = print(show(double(identity("hello"))))
`;
  const f = writeTmp("generic-unify-mismatch.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E304"), "E304 type mismatch");
  cleanTmp("generic-unify-mismatch.clk");
});

test("generic list function preserves element type through inference", () => {
  const src = `
first : (xs: [T]) -> <> T = head(xs)
double : (n: Int) -> <> Int = add(n, n)

main : () -> <io> () = print(show(double(first([10, 20, 30]))))
`;
  const f = writeTmp("generic-list-infer.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("generic-list-infer.clk");
});

// ── Where-constraint propagation through function return values (TASK-128) ──

test("where constraint propagated through let-bound function call result", () => {
  const src = `
max-val : (a: T, b: T) -> <> T where Ord T =
  if gt(a, b) then a else b

use-max : (x: Int) -> <> Int =
  let f = max-val
  f(x, 0)

main : () -> <io> () = print(show(use-max(5)))
`;
  const f = writeTmp("constraint-prop-let.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("constraint-prop-let.clk");
});

test("where constraint propagated through function call result detects violation", () => {
  const src = `
type Color = Red | Green | Blue

max-val : (a: T, b: T) -> <> T where Ord T =
  if gt(a, b) then a else b

main : () -> <io> () =
  let f = max-val
  print(show(f(Red, Green)))
`;
  const f = writeTmp("constraint-prop-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 constraint violation through propagation");
  cleanTmp("constraint-prop-fail.clk");
});

// ── Borrow &T type annotations (TASK-127) ──

test("borrow &T in function signature parses and checks", () => {
  const src = `
affine type File = File(Str)

read-all : (f: &File) -> <> Str = "contents"

main : () -> <io> () = print("ok")
`;
  const f = writeTmp("borrow-sig.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("borrow-sig.clk");
});

test("borrow &T in function type annotation is accepted", () => {
  const src = `
affine type Handle = Handle(Int)

inspect : (h: &Handle) -> <> Int = 42

use-handle : (h: Handle) -> <> Int =
  let result = inspect(&h)
  discard h
  result

main : () -> <io> () = print(show(use-handle(Handle(1))))
`;
  const f = writeTmp("borrow-use.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("borrow-use.clk");
});

// ── Match arm affine tracking (TASK-127) ──

test("affine variable from match arm pattern destructuring is tracked", () => {
  const src = `
affine type Token = Token(Str)

type Packet
  = Data(Token)
  | Empty()

burn : (t: Token) -> <> () = discard t

process : (p: Packet) -> <> Str =
  match p {
    Data(t) => let _ = burn(t) in "data"
    Empty() => "empty"
  }

main : () -> <io> () = print(process(Data(Token("x"))))
`;
  const f = writeTmp("match-affine-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("match-affine-ok.clk");
});

test("affine variable from match arm used twice emits E600", () => {
  const src = `
affine type Token = Token(Str)

type Packet
  = Data(Token)
  | Empty()

burn : (t: Token) -> <> () = discard t

process : (p: Packet) -> <> Str =
  match p {
    Data(t) => let _ = burn(t) in let _ = burn(t) in "data"
    Empty() => "empty"
  }

main : () -> <io> () = print(process(Data(Token("x"))))
`;
  const f = writeTmp("match-affine-double.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E600"), "E600 use-after-move");
  cleanTmp("match-affine-double.clk");
});

// ── Borrow scope & match arm affine tracking (TASK-154) ──

test("match arm pattern-bound affine var consumed correctly passes", () => {
  const src = `
affine type Token = Token(Str)

type Packet
  = Data(Token)
  | Empty()

burn : (t: Token) -> <> () = discard t

process : (p: Packet) -> <> Str =
  match p {
    Data(t) => let _ = burn(t) in "data"
    Empty() => "empty"
  }

main : () -> <io> () = print(process(Data(Token("x"))))
`;
  const f = writeTmp("match-arm-consume-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("match-arm-consume-ok.clk");
});

test("match arm pattern-bound affine var unconsumed emits W600", () => {
  const src = `
affine type Token = Token(Str)

type Packet
  = Data(Token)
  | Empty()

process : (p: Packet) -> <> Str =
  match p {
    Data(t) => "data"
    Empty() => "empty"
  }

main : () -> <io> () = print("x")
`;
  const f = writeTmp("match-arm-leak.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "W600" && d.message.includes("'t'")), "W600 unconsumed pattern var");
  cleanTmp("match-arm-leak.clk");
});

test("borrow stored in record compound type emits E603", () => {
  const src = `
affine type Token = Token

store-in-record : (t: Token) -> <> () =
  let r = {ref: &t}
  discard t

main : () -> <io> () = print("x")
`;
  const f = writeTmp("borrow-record-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E603"), "E603 borrow in record");
  cleanTmp("borrow-record-fail.clk");
});

test("function returning borrow type in signature emits E603", () => {
  const src = `
affine type File = File(Str)

bad-return : (f: File) -> <> &File = &f
`;
  const f = writeTmp("fn-borrow-return.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E603"), "E603 borrow in return");
  cleanTmp("fn-borrow-return.clk");
});

test("match arms with outer affine var must consume consistently", () => {
  const src = `
affine type Handle = Handle(Int)

consume : (h: Handle) -> <> () = discard h

test-fn : (h: Handle, x: Int) -> <> Str =
  match x {
    1 => let _ = consume(h) in "one"
    _ => "other"
  }

main : () -> <io> () = print("x")
`;
  const f = writeTmp("match-branch-consistency.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E601"), "E601 branch inconsistency");
  cleanTmp("match-branch-consistency.clk");
});

// ── Pipeline where-constraint checking (TASK-127) ──

test("pipeline checks where-constraints (satisfied)", () => {
  const src = `
sort-list : (xs: [T]) -> <> [T] where Ord T =
  xs

main : () -> <io> () =
  let result = [3, 1, 2] |> sort-list
  print(show(len(result)))
`;
  const f = writeTmp("pipe-constraint-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("pipe-constraint-ok.clk");
});

test("pipeline checks where-constraints (violated) emits E205", () => {
  const src = `
type Color = Red | Green | Blue

sort-list : (xs: [T]) -> <> [T] where Ord T =
  xs

main : () -> <io> () =
  let result = [Red, Green] |> sort-list
  print(show(len(result)))
`;
  const f = writeTmp("pipe-constraint-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 constraint violation via pipeline");
  assert(env.diagnostics.some((d: any) => d.message.includes("Color")), "message mentions Color");
  cleanTmp("pipe-constraint-fail.clk");
});

test("pipeline type checks argument against function param", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let result = "hello" |> add-one
  print(show(result))
`;
  const f = writeTmp("pipe-type-mismatch.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E304"), "E304 type mismatch via pipeline");
  cleanTmp("pipe-type-mismatch.clk");
});

test("pipeline infers result type correctly", () => {
  const src = `
to-str : (x: Int) -> <> Str = show(x)

main : () -> <io> () =
  let result = 5 |> to-str
  print(result)
`;
  const f = writeTmp("pipe-result-type.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("pipe-result-type.clk");
});

// ── TASK-128: Pipeline parameterized constraint type arg validation ──

test("pipeline parameterized constraint type args validated", () => {
  const src = `
impl From<Int> for Str {
  from = fn(n) => show(n)
}

convert : (x: T) -> <> Str where Into<Str> T = into(x)

main : () -> <io> () =
  let result = 42 |> convert
  print(result)
`;
  const f = writeTmp("pipe-param-constraint-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("pipe-param-constraint-ok.clk");
});

test("pipeline parameterized constraint with wrong type args emits E205", () => {
  const src = `
impl From<Str> for Int {
  from = fn(s) => len(s)
}

convert-to-str : (x: T) -> <> Str where Into<Str> T = into(x)

main : () -> <io> () =
  let result = "hello" |> convert-to-str
  print(result)
`;
  const f = writeTmp("pipe-param-constraint-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 parameterized constraint via pipeline");
  assert(env.diagnostics.some((d: any) => d.message.includes("Into<Str>")), "message mentions Into<Str>");
  cleanTmp("pipe-param-constraint-fail.clk");
});

// ── TASK-128: HM type variable unification ──

test("lambda parameter type inferred via unification with function param", () => {
  const src = `
apply-int : (f: (Int) -> <> Int, x: Int) -> <> Int = f(x)

main : () -> <io> () = print(show(apply-int(fn(n) => add(n, 1), 5)))
`;
  const f = writeTmp("hm-lambda-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("hm-lambda-infer.clk");
});

test("type variable unification catches mismatched generic chain", () => {
  const src = `
identity : (x: T) -> <> T = x
negate-int : (n: Int) -> <> Int = sub(0, n)

main : () -> <io> () = print(show(negate-int(identity("not-an-int"))))
`;
  const f = writeTmp("hm-mismatch-chain.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E304"), "E304 mismatch through unification chain");
  cleanTmp("hm-mismatch-chain.clk");
});

test("fresh type vars for lambda params do not leak across calls", () => {
  const src = `
apply-fn : (f: (Int) -> <> Str, x: Int) -> <> Str = f(x)

main : () -> <io> () =
  let r1 = apply-fn(fn(n) => show(n), 1)
  let r2 = apply-fn(fn(n) => show(add(n, 1)), 2)
  print(r1)
`;
  const f = writeTmp("hm-no-leak.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("hm-no-leak.clk");
});

// ── TASK-128: Where-constraint propagation through function params/returns ──

test("where constraint propagated through function return value", () => {
  const src = `
max-val : (a: T, b: T) -> <> T where Ord T =
  if gt(a, b) then a else b

get-max : () -> <> Int =
  max-val(3, 5)

main : () -> <io> () = print(show(get-max()))
`;
  const f = writeTmp("constraint-prop-return.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("constraint-prop-return.clk");
});

test("where constraint on function passed as argument is checked", () => {
  const src = `
type Color = Red | Green | Blue

sort-list : (xs: [T]) -> <> [T] where Ord T =
  xs

main : () -> <io> () =
  let sorter = sort-list
  let result = sorter([Red, Green])
  print(show(len(result)))
`;
  const f = writeTmp("constraint-prop-arg.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 constraint propagated through argument");
  cleanTmp("constraint-prop-arg.clk");
});

test("where constraint propagated through pipeline to parameterized interface", () => {
  const src = `
impl From<Int> for Str {
  from = fn(n) => show(n)
}

convert : (x: T) -> <> Str where Into<Str> T = into(x)

process : (n: Int) -> <> Str =
  n |> convert

main : () -> <io> () = print(process(42))
`;
  const f = writeTmp("constraint-prop-pipe-param.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("constraint-prop-pipe-param.clk");
});

test("where constraint violation through double let-binding detected", () => {
  const src = `
interface Hashable {
  hash-code : (Self) -> <> Int
}

hash-it : (x: T) -> <> Int where Hashable T = hash-code(x)

main : () -> <io> () =
  let h = hash-it
  let h2 = h
  print(show(h2(42)))
`;
  const f = writeTmp("constraint-double-let-fail.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E205"), "E205 through double let binding");
  cleanTmp("constraint-double-let-fail.clk");
});

// ── TASK-141: Borrow types enforced in return position and compound types ──

test("borrow &T in return position emits E603", () => {
  const src = `
affine type File = File(Str)

bad-return : (f: File) -> <> &File = &f
`;
  const f = writeTmp("borrow-return.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E603"), "E603 borrow in return type");
  cleanTmp("borrow-return.clk");
});

test("borrow &T in compound return type emits E603", () => {
  const src = `
affine type File = File(Str)

bad-compound : (f: File) -> <> [&File] = [&f]
`;
  const f = writeTmp("borrow-compound-return.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E603"), "E603 borrow in compound return type");
  cleanTmp("borrow-compound-return.clk");
});

test("borrow &T in compound parameter type emits E603", () => {
  const src = `
affine type File = File(Str)

bad-param : (xs: [&File]) -> <> Int = len(xs)
`;
  const f = writeTmp("borrow-compound-param.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E603"), "E603 borrow in compound param type");
  cleanTmp("borrow-compound-param.clk");
});

test("bare borrow &T parameter is still allowed", () => {
  const src = `
affine type File = File(Str)
  deriving (Clone)

read-it : (f: &File) -> <> Str = "contents"

main : () -> <io> () =
  let f = File("x")
  let result = read-it(&f)
  discard f
  print(result)
`;
  const f = writeTmp("borrow-param-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("borrow-param-ok.clk");
});

// ── TASK-141: Composite literal type inference ──

test("head of list literal infers element type", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let xs = [10, 20, 30]
  let first = head(xs)
  print(show(add-one(first)))
`;
  const f = writeTmp("composite-head.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("composite-head.clk");
});

test("fst of tuple literal infers element type", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let pair = (42, "hello")
  let n = fst(pair)
  print(show(add-one(n)))
`;
  const f = writeTmp("composite-fst.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("composite-fst.clk");
});

test("tail of list preserves element type", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let rest = tail([1, 2, 3])
  print(show(add-one(head(rest))))
`;
  const f = writeTmp("composite-tail.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("composite-tail.clk");
});

// ── Zero-param lambda inference ──

test("zero-param lambda inferred as () -> T, not T", () => {
  const src = `
main : () -> <io> () =
  let thunk = fn() => 42
  print(show(thunk()))
`;
  const f = writeTmp("zero-param-lambda.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("zero-param-lambda.clk");
});

// ── Clone call-site type inference ──

test("clone function returns same type as argument (not tAny)", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let xs = [1, 2, 3]
  let ys = clone(xs)
  print(show(add-one(head(ys))))
`;
  const f = writeTmp("clone-type-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("clone-type-infer.clk");
});

// ── cmp call-site type inference ──

test("cmp returns Ordering, not tAny", () => {
  const src = `
main : () -> <io> () =
  match cmp(1, 2) {
    Lt => print("less")
    Gt => print("greater")
    Eq_ => print("equal")
  }
`;
  const f = writeTmp("cmp-type-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("cmp-type-infer.clk");
});

// ── TASK-147: List/Tuple Ord where-constraint checking ──

test("where constraint Ord satisfied for List<Int>", () => {
  const src = `
min-of : (a: T, b: T) -> <> T where Ord T =
  match cmp(a, b) {
    Lt => a
    _ => b
  }

main : () -> <io> () =
  let x = min-of([1, 2], [1, 3])
  print(show(head(x)))
`;
  const f = writeTmp("list-ord-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("list-ord-ok.clk");
});

test("where constraint Ord satisfied for Tuple", () => {
  const src = `
min-of : (a: T, b: T) -> <> T where Ord T =
  match cmp(a, b) {
    Lt => a
    _ => b
  }

main : () -> <io> () =
  let x = min-of((1, 2), (1, 3))
  print(show(fst(x)))
`;
  const f = writeTmp("tuple-ord-ok.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("tuple-ord-ok.clk");
});

test("clone of list literal preserves element type", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let xs = clone([10, 20, 30])
  print(show(add-one(head(xs))))
`;
  const f = writeTmp("list-clone-elem.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("list-clone-elem.clk");
});

// ── TASK-147: from/into per-call-site type inference ──

test("from infers return type from unique impl", () => {
  const src = `
impl From<Int> for Str {
  from = fn(n) => show(n)
}

main : () -> <io> () =
  let s = from(42)
  print(s)
`;
  const f = writeTmp("from-infer.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("from-infer.clk");
});

// ── TASK-147: Composite type constraint propagation ──

test("list cmp inferred as Ordering for match", () => {
  const src = `
main : () -> <io> () =
  match cmp([1, 2], [3, 4]) {
    Lt => print("less")
    Gt => print("greater")
    Eq_ => print("equal")
  }
`;
  const f = writeTmp("list-cmp-match.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("list-cmp-match.clk");
});

test("tuple cmp inferred as Ordering for match", () => {
  const src = `
main : () -> <io> () =
  match cmp((1, "a"), (2, "b")) {
    Lt => print("less")
    Gt => print("greater")
    Eq_ => print("equal")
  }
`;
  const f = writeTmp("tuple-cmp-match.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("tuple-cmp-match.clk");
});

// ── TASK-153: Refinement solver extensions ──

test("path condition: branch guard satisfies refined param", () => {
  const src = `
positive : (x: Int{v > 0}) -> <> Int = x

safe-call : (x: Int) -> <> Int
  = if gt(x, 0) then positive(x) else 0

main : () -> <io> () = print(show(safe-call(5)))
`;
  const f = writeTmp("ref-path-cond.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("ref-path-cond.clk");
});

test("arithmetic expression tracking: add(x,1) where x>=0 satisfies v>0", () => {
  const src = `
positive : (x: Int{v > 0}) -> <> Int = x

inc-and-check : (x: Int{v >= 0}) -> <> Int = positive(add(x, 1))

main : () -> <io> () = print(show(inc-and-check(3)))
`;
  const f = writeTmp("ref-arith-expr.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("ref-arith-expr.clk");
});

test("pattern-to-predicate: match arm literal propagates equality", () => {
  const src = `
zero-check : (x: Int{v == 0}) -> <> String = "ok"

classify : (x: Int) -> <> String =
  match x {
    0 => zero-check(x)
    _ => "other"
  }

main : () -> <io> () = print(classify(0))
`;
  const f = writeTmp("ref-match-pred.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("ref-match-pred.clk");
});

test("path condition: else branch gets negated condition", () => {
  const src = `
non-positive : (x: Int{v <= 0}) -> <> Int = x

check : (x: Int) -> <> Int
  = if gt(x, 0) then 1 else non-positive(x)

main : () -> <io> () = print(show(check(-3)))
`;
  const f = writeTmp("ref-else-neg.clk", src);
  const { exitCode } = runCLI(`check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  cleanTmp("ref-else-neg.clk");
});

test("refinement failure: return literal violating return refinement", () => {
  const src = `
bad-return : (x: Int) -> <> Int{v > 0} =
  if gt(x, 0) then x else 0

main : () -> <io> () = print(show(bad-return(5)))
`;
  const f = writeTmp("ref-fail-return.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.length > 0, "should have diagnostics");
  assert(env.diagnostics[0].message.includes("v > 0"), "error mentions refinement predicate");
  cleanTmp("ref-fail-return.clk");
});

// ── TASK-158: Record pattern matching and spread operator ──

test("record pattern destructuring in match type checks", () => {
  const src = `
main : () -> <io> () =
  let person = {name: "Ada", age: 36}
  match person {
    {name, age} => print(name ++ " " ++ show(age))
  }
`;
  const f = writeTmp("rec-pat-match.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("rec-pat-match.clk");
});

test("record pattern with rest captures remaining fields", () => {
  const src = `
main : () -> <io> () =
  let person = {name: "Ada", age: 36}
  match person {
    {name | rest} => print(name ++ " " ++ show(rest))
  }
`;
  const f = writeTmp("rec-pat-rest.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("rec-pat-rest.clk");
});

test("record pattern with rename binds correctly", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let person = {name: "Ada", age: 36}
  match person {
    {name: n, age: a | _} => print(n ++ " " ++ show(add-one(a)))
  }
`;
  const f = writeTmp("rec-pat-rename.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("rec-pat-rename.clk");
});

test("let record destructuring type checks", () => {
  const src = `
main : () -> <io> () =
  let person = {name: "Ada", age: 36}
  let {name, age} = person
  print(name ++ " " ++ show(age))
`;
  const f = writeTmp("let-rec-destr.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("let-rec-destr.clk");
});

test("let record destructuring with rest type checks", () => {
  const src = `
main : () -> <io> () =
  let person = {name: "Ada", age: 36}
  let {name: n | rest} = person
  print(n ++ " " ++ show(rest))
`;
  const f = writeTmp("let-rec-rest.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("let-rec-rest.clk");
});

test("record spread operator type checks", () => {
  const src = `
main : () -> <io> () =
  let base = {name: "Ada", age: 36}
  let extended = {title: "Countess", ..base}
  print(extended.title ++ " " ++ extended.name)
`;
  const f = writeTmp("rec-spread.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("rec-spread.clk");
});

test("record spread with field override type checks", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let base = {name: "Ada", age: 36}
  let updated = {age: 37, ..base}
  print(show(add-one(updated.age)))
`;
  const f = writeTmp("rec-spread-override.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("rec-spread-override.clk");
});

test("nested record pattern in match type checks", () => {
  const src = `
main : () -> <io> () =
  let person = {name: {first: "Ada", last: "Lovelace"}, age: 36}
  match person {
    {name: {first, last}, age} => print(first ++ " " ++ last)
  }
`;
  const f = writeTmp("rec-pat-nested.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("rec-pat-nested.clk");
});

test("record pattern inside variant destructure type checks", () => {
  const src = `
type Shape = Circle({radius: Int}) | Rect({width: Int, height: Int})

area : (s: Shape) -> <> Int =
  match s {
    Circle({radius}) => radius * radius
    Rect({width, height}) => width * height
  }

main : () -> <io> () =
  print(show(area(Rect({width: 5, height: 3}))))
`;
  const f = writeTmp("rec-pat-variant.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("rec-pat-variant.clk");
});

// ── HM let-polymorphism for user functions (TASK-169) ──

test("user generic function used at multiple types in same scope", () => {
  const src = `
identity : (x: T) -> <> T = x

main : () -> <io> () =
  let a = identity(42)
  let b = identity("hello")
  let c = add(identity(1), identity(2))
  print(show(c))
`;
  const f = writeTmp("hm-poly-multi.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("hm-poly-multi.clk");
});

test("user generic function catches type mismatch after instantiation", () => {
  const src = `
identity : (x: T) -> <> T = x

main : () -> <io> () =
  let a = add(identity("oops"), 1)
  print(show(a))
`;
  const f = writeTmp("hm-poly-mismatch.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E304"), "E304 mismatch through instantiated generic");
  cleanTmp("hm-poly-mismatch.clk");
});

test("multi-param user generic function instantiates independently", () => {
  const src = `
swap : (a: A, b: B) -> <> (B, A) = (b, a)

main : () -> <io> () =
  let p1 = swap(1, "hello")
  let p2 = swap(true, 42)
  print(show(fst(p1)))
`;
  const f = writeTmp("hm-poly-swap.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("hm-poly-swap.clk");
});

test("user generic list function with fresh instantiation per call", () => {
  const src = `
wrap : (x: T) -> <> [T] = [x]

main : () -> <io> () =
  let xs = wrap(42)
  let ys = wrap("hello")
  let n = head(xs)
  print(show(add(n, 1)))
`;
  const f = writeTmp("hm-poly-wrap.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("hm-poly-wrap.clk");
});

test("user generic function with where constraint still checks constraint", () => {
  const src = `
max-val : (a: T, b: T) -> <> T where Ord T =
  if gt(a, b) then a else b

main : () -> <io> () =
  let m1 = max-val(3, 5)
  let m2 = max-val("a", "b")
  print(show(add(m1, 1)))
`;
  const f = writeTmp("hm-poly-constraint.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("hm-poly-constraint.clk");
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);

// Cleanup
rmSync(TMP_DIR, { recursive: true, force: true });

process.exit(failed > 0 ? 1 : 0);
