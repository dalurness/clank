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

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);

// Cleanup
rmSync(TMP_DIR, { recursive: true, force: true });

process.exit(failed > 0 ? 1 : 0);
