// Tests for --json structured error output
// Run with: npx tsx test/json-output.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, unlinkSync } from "node:fs";
import { join } from "node:path";

const CLI = join(import.meta.dirname, "..", "ts", "src", "main.ts");
const TMP_DIR = "/tmp";

let passed = 0;
let failed = 0;

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

// ── Envelope schema validation ──

function validateEnvelope(obj: any): void {
  assert(typeof obj === "object" && obj !== null, "envelope is an object");
  assert(typeof obj.ok === "boolean", "envelope.ok is boolean");
  assert(Array.isArray(obj.diagnostics), "envelope.diagnostics is array");
  assert(typeof obj.timing === "object", "envelope.timing is object");
  assert(typeof obj.timing.total_ms === "number", "envelope.timing.total_ms is number");
  assert(typeof obj.timing.phases === "object", "envelope.timing.phases is object");
}

function validateDiagnostic(d: any): void {
  assert(["error", "warning", "info"].includes(d.severity), `severity is valid: ${d.severity}`);
  assert(typeof d.code === "string" && d.code.length > 0, "code is non-empty string");
  assert(["lex", "parse", "desugar", "check", "eval", "link"].includes(d.phase), `phase is valid: ${d.phase}`);
  assert(typeof d.message === "string" && d.message.length > 0, "message is non-empty string");
  assert(typeof d.location === "object", "location is object");
  assert(typeof d.location.file === "string", "location.file is string");
  assert(typeof d.location.line === "number", "location.line is number");
  assert(typeof d.location.col === "number", "location.col is number");
}

// ── Tests ──

console.log("# JSON output tests");

// Successful execution
test("successful execution produces ok:true envelope", () => {
  const f = writeTmp("json-test-ok.clk", `main : () -> <io> () = print("hello")\n`);
  const { stdout, exitCode } = runCLI(`--json ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, true, "ok");
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  assert(env.data !== undefined, "data present");
  assert(Array.isArray(env.data.stdout), "data.stdout is array");
  assert(env.data.stdout.includes("hello"), "stdout contains hello");
  cleanTmp("json-test-ok.clk");
});

test("successful execution has phase timing", () => {
  const f = writeTmp("json-test-timing.clk", `main : () -> <io> () = print("hi")\n`);
  const { stdout } = runCLI(`--json ${f}`);
  const env = JSON.parse(stdout);
  assert(typeof env.timing.phases.lex === "number", "lex timing present");
  assert(typeof env.timing.phases.parse === "number", "parse timing present");
  assert(typeof env.timing.phases.check === "number", "check timing present");
  assert(env.timing.total_ms >= 0, "total_ms >= 0");
  cleanTmp("json-test-timing.clk");
});

// Lexer error
test("lexer error produces structured diagnostic", () => {
  const f = writeTmp("json-test-lex.clk", `main : () -> <io> () = §\n`);
  const { stdout, exitCode } = runCLI(`--json ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, false, "ok");
  assertEqual(env.data, null, "data is null");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  const d = env.diagnostics[0];
  validateDiagnostic(d);
  assertEqual(d.severity, "error", "severity");
  assertEqual(d.code, "E001", "code");
  assertEqual(d.phase, "lex", "phase");
  assert(d.message.includes("§"), "message mentions the character");
  assert(d.location.file.includes("json-test-lex.clk"), "file in location");
  assertEqual(d.location.line, 1, "line");
  cleanTmp("json-test-lex.clk");
});

// Parser error
test("parser error produces structured diagnostic", () => {
  const f = writeTmp("json-test-parse.clk", `main : () -> <io> () = if\n`);
  const { stdout, exitCode } = runCLI(`--json ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  const d = env.diagnostics[0];
  validateDiagnostic(d);
  assertEqual(d.severity, "error", "severity");
  assertEqual(d.phase, "parse", "phase");
  assert(d.code.startsWith("E1"), "parse error code");
  cleanTmp("json-test-parse.clk");
});

// Type error
test("type error produces structured diagnostic", () => {
  const f = writeTmp("json-test-type.clk", `main : () -> <io> () = print(undefined_var)\n`);
  const { stdout, exitCode } = runCLI(`--json ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  const d = env.diagnostics[0];
  validateDiagnostic(d);
  assertEqual(d.severity, "error", "severity");
  assertEqual(d.phase, "check", "phase");
  assertEqual(d.code, "E300", "type error code");
  assert(d.message.includes("undefined_var"), "message mentions the variable");
  cleanTmp("json-test-type.clk");
});

// Warning (non-exhaustive match)
test("warnings appear in diagnostics with severity warning", () => {
  const src = `
type Color = Red | Green | Blue

check-red : (c: Color) -> <> Str =
  match c {
    Red => "red"
  }

main : () -> <io> () = print(check-red(Red))
`;
  const f = writeTmp("json-test-warn.clk", src);
  const { stdout } = runCLI(`--json ${f}`);
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  // Should succeed (warnings don't block) but have a warning diagnostic
  const warnings = env.diagnostics.filter((d: any) => d.severity === "warning");
  assert(warnings.length >= 1, "at least one warning");
  const w = warnings[0];
  validateDiagnostic(w);
  assertEqual(w.severity, "warning", "severity is warning");
  assert(w.code.startsWith("W"), "warning code starts with W");
  cleanTmp("json-test-warn.clk");
});

// Multiple errors
test("multiple type errors all appear in diagnostics array", () => {
  const src = `
foo : (x: Int) -> <> Int = unknown1
bar : (x: Int) -> <> Int = unknown2
main : () -> <io> () = print("ok")
`;
  const f = writeTmp("json-test-multi.clk", src);
  const { stdout, exitCode } = runCLI(`--json ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assert(env.diagnostics.length >= 2, `at least 2 diagnostics, got ${env.diagnostics.length}`);
  for (const d of env.diagnostics) {
    validateDiagnostic(d);
  }
  cleanTmp("json-test-multi.clk");
});

// Without --json flag, errors go to stderr as before
test("without --json, errors go to stderr as raw JSON objects", () => {
  const f = writeTmp("json-test-noflag.clk", `main : () -> <io> () = §\n`);
  const { stderr, exitCode } = runCLI(f);
  assertEqual(exitCode, 1, "exit code");
  const err = JSON.parse(stderr);
  assertEqual(err.code, "E001", "raw error code");
  assert(!("severity" in err), "no severity field in legacy mode");
  assert(!("phase" in err), "no phase field in legacy mode");
  cleanTmp("json-test-noflag.clk");
});

// No input file with --json
test("no input file with --json returns structured error", () => {
  const { stdout, exitCode } = runCLI("--json");
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
