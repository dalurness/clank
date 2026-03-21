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

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);

// Cleanup
rmSync(TMP_DIR, { recursive: true, force: true });

process.exit(failed > 0 ? 1 : 0);
