// Tests for clank lint subcommand
// Run with: npx tsx test/lint.test.ts

import { execSync, spawnSync } from "node:child_process";
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

function validateEnvelope(obj: any): void {
  assert(typeof obj === "object" && obj !== null, "envelope is an object");
  assert(typeof obj.ok === "boolean", "envelope.ok is boolean");
  assert(Array.isArray(obj.diagnostics), "envelope.diagnostics is array");
  assert(typeof obj.timing === "object", "envelope.timing is object");
}

// ── Tests ──

console.log("# Lint subcommand tests");

// No input
test("lint with no file returns error", () => {
  const { exitCode } = runCLI("lint --json");
  assertEqual(exitCode, 1, "exit code");
});

// Clean file produces no warnings
test("clean file produces no lint warnings", () => {
  const src = `main : () -> <io> () = print("hello")\n`;
  const f = writeTmp("lint-clean.clk", src);
  const { stdout, exitCode } = runCLI(`lint --json ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, true, "ok");
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("lint-clean.clk");
});

// W100: unused variable
test("W100: unused variable detected", () => {
  const src = `
main : () -> <io> () =
  let unused = 42
  print("hello")
`;
  const f = writeTmp("lint-unused-var.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  const w100 = env.diagnostics.filter((d: any) => d.code === "W100");
  assert(w100.length >= 1, `expected W100 diagnostic, got ${JSON.stringify(env.diagnostics)}`);
  assert(w100[0].message.includes("unused"), "message mentions unused");
  cleanTmp("lint-unused-var.clk");
});

// W100: _ variables should not trigger
test("W100: underscore variables are ignored", () => {
  const src = `
main : () -> <io> () =
  let _ = 42
  print("hello")
`;
  const f = writeTmp("lint-underscore.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  const w100 = env.diagnostics.filter((d: any) => d.code === "W100");
  assertEqual(w100.length, 0, "no W100 for underscore");
  cleanTmp("lint-underscore.clk");
});

// W102: shadowed binding
test("W102: shadowed binding detected", () => {
  const src = `
main : () -> <io> () =
  let x = 1
  let x = 2
  print(show(x))
`;
  const f = writeTmp("lint-shadow.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  const w102 = env.diagnostics.filter((d: any) => d.code === "W102");
  assert(w102.length >= 1, `expected W102 diagnostic, got ${JSON.stringify(env.diagnostics)}`);
  assert(w102[0].message.includes("shadows"), "message mentions shadow");
  cleanTmp("lint-shadow.clk");
});

// W104: unreachable match arm
test("W104: unreachable match arm after wildcard", () => {
  const src = `
type Color = Red | Green | Blue

check : (c: Color) -> <> Str =
  match c {
    Red => "red"
    _ => "other"
    Blue => "blue"
  }

main : () -> <io> () = print(check(Red))
`;
  const f = writeTmp("lint-unreachable.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  const w104 = env.diagnostics.filter((d: any) => d.code === "W104");
  assert(w104.length >= 1, `expected W104 diagnostic, got ${JSON.stringify(env.diagnostics)}`);
  assert(w104[0].message.includes("unreachable"), "message mentions unreachable");
  cleanTmp("lint-unreachable.clk");
});

// W105: empty effect handler
test("W105: empty effect handler detected", () => {
  const src = `
main : () -> <io> () =
  let result = handle 42 {
    return x -> x
  }
  print(show(result))
`;
  const f = writeTmp("lint-empty-handler.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  const w105 = env.diagnostics.filter((d: any) => d.code === "W105");
  assert(w105.length >= 1, `expected W105 diagnostic, got ${JSON.stringify(env.diagnostics)}`);
  assert(w105[0].message.includes("no operation arms"), "message mentions no operation arms");
  cleanTmp("lint-empty-handler.clk");
});

// --rule flag: disable a specific rule
test("--rule -unused-variable disables W100", () => {
  const src = `
main : () -> <io> () =
  let unused = 42
  print("hello")
`;
  const f = writeTmp("lint-disable-rule.clk", src);
  const { stdout } = runCLI(`lint --json --rule -unused-variable ${f}`);
  const env = JSON.parse(stdout);
  const w100 = env.diagnostics.filter((d: any) => d.code === "W100");
  assertEqual(w100.length, 0, "W100 should be disabled");
  cleanTmp("lint-disable-rule.clk");
});

// --rule flag: enable only a specific rule
test("--rule +unused-variable enables only W100", () => {
  const src = `
main : () -> <io> () =
  let x = 1
  let x = 2
  print("hello")
`;
  const f = writeTmp("lint-enable-rule.clk", src);
  const { stdout } = runCLI(`lint --json --rule +unused-variable ${f}`);
  const env = JSON.parse(stdout);
  // Should have W100 but not W102 (shadowed)
  const w100 = env.diagnostics.filter((d: any) => d.code === "W100");
  const w102 = env.diagnostics.filter((d: any) => d.code === "W102");
  assert(w100.length >= 1, "W100 should be present");
  assertEqual(w102.length, 0, "W102 should not be present when only W100 is enabled");
  cleanTmp("lint-enable-rule.clk");
});

// JSON envelope structure
test("lint --json produces valid envelope", () => {
  const src = `main : () -> <io> () = print("ok")\n`;
  const f = writeTmp("lint-envelope.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, true, "ok");
  assert(env.data !== null, "data is present");
  assert(Array.isArray(env.data.files), "data.files is array");
  assert(typeof env.timing.phases.lint === "number", "lint timing present");
  cleanTmp("lint-envelope.clk");
});

// Non-JSON mode outputs to stderr
test("lint without --json outputs warnings to stderr", () => {
  const src = `
main : () -> <io> () =
  let unused = 42
  print("hello")
`;
  const f = writeTmp("lint-stderr.clk", src);
  const result = spawnSync("npx", ["tsx", CLI, "lint", f], {
    encoding: "utf-8",
    stdio: ["pipe", "pipe", "pipe"],
  });
  assertEqual(result.status, 0, "exit code");
  assert(result.stderr.includes("W100"), "stderr contains warning code");
  assert(result.stderr.includes("unused"), "stderr contains warning message");
  cleanTmp("lint-stderr.clk");
});

// Directory scanning
test("lint accepts a directory and finds .clk files", () => {
  const src = `main : () -> <io> () = print("ok")\n`;
  const f = writeTmp("lint-dir-test.clk", src);
  // Lint the /tmp directory — it should find at least our file
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, true, "ok");
  cleanTmp("lint-dir-test.clk");
});

// Lex error in lint mode
test("lint handles lex errors gracefully", () => {
  const f = writeTmp("lint-lex-err.clk", `main : () -> <io> () = §\n`);
  const { stdout, exitCode } = runCLI(`lint --json ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  assertEqual(env.ok, false, "ok");
  assert(env.diagnostics.length >= 1, "at least one diagnostic");
  cleanTmp("lint-lex-err.clk");
});

// W100: used variable should not trigger
test("W100: used variable does not trigger warning", () => {
  const src = `
main : () -> <io> () =
  let x = 42
  print(show(x))
`;
  const f = writeTmp("lint-used-var.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  const w100 = env.diagnostics.filter((d: any) => d.code === "W100");
  assertEqual(w100.length, 0, "no W100 for used variable");
  cleanTmp("lint-used-var.clk");
});

// W106: builtin name shadowing
test("W106: function shadowing builtin 'add' detected", () => {
  const src = `
add : (a: Int, b: Int) -> <> Int =
  a + b

main : () -> <io> () = print(show(add(1, 2)))
`;
  const f = writeTmp("lint-builtin-shadow-add.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  const w106 = env.diagnostics.filter((d: any) => d.code === "W106");
  assert(w106.length >= 1, `expected W106 diagnostic, got ${JSON.stringify(env.diagnostics)}`);
  assert(w106[0].message.includes("shadows builtin"), "message mentions shadows builtin");
  cleanTmp("lint-builtin-shadow-add.clk");
});

test("W106: function shadowing builtin 'sub' detected", () => {
  const src = `
sub : (a: Int, b: Int) -> <> Int = a

main : () -> <io> () = print(show(sub(5, 3)))
`;
  const f = writeTmp("lint-builtin-shadow-sub.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  validateEnvelope(env);
  const w106 = env.diagnostics.filter((d: any) => d.code === "W106");
  assert(w106.length >= 1, `expected W106 diagnostic, got ${JSON.stringify(env.diagnostics)}`);
  cleanTmp("lint-builtin-shadow-sub.clk");
});

test("W106: non-builtin function name does not trigger", () => {
  const src = `
myAdd : (a: Int, b: Int) -> <> Int = a + b

main : () -> <io> () = print(show(myAdd(1, 2)))
`;
  const f = writeTmp("lint-no-builtin-shadow.clk", src);
  const { stdout } = runCLI(`lint --json ${f}`);
  const env = JSON.parse(stdout);
  const w106 = env.diagnostics.filter((d: any) => d.code === "W106");
  assertEqual(w106.length, 0, "no W106 for non-builtin name");
  cleanTmp("lint-no-builtin-shadow.clk");
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
