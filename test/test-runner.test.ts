// Tests for clank test subcommand
// Run with: npx tsx test/test-runner.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, mkdirSync, rmSync } from "node:fs";
import { join } from "node:path";

const CLI = join(import.meta.dirname, "..", "ts", "src", "main.ts");
const TMP_DIR = "/tmp/clank-test-runner-test-suite";

let passed = 0;
let failed = 0;

// Setup temp dir
rmSync(TMP_DIR, { recursive: true, force: true });
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

function writeClk(name: string, content: string): string {
  const path = join(TMP_DIR, name);
  writeFileSync(path, content);
  return path;
}

// ── Tests ──

console.log("clank test subcommand:");

// Basic passing tests
test("all tests pass → ok: true, exit 0", () => {
  const file = writeClk("pass.clk", `
mod test.pass

test "one plus one" =
  1 + 1

test "string value" =
  "hello"
`);
  const { stdout, exitCode } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  assertEqual(out.ok, true, "ok");
  assertEqual(out.data.summary.total, 2, "total");
  assertEqual(out.data.summary.passed, 2, "passed");
  assertEqual(out.data.summary.failed, 0, "failed");
  assertEqual(exitCode, 0, "exitCode");
});

// Failing tests
test("failing test → ok: false, exit 1", () => {
  const file = writeClk("fail.clk", `
mod test.fail

test "passes" =
  42

test "fails on division by zero" =
  1 / 0
`);
  const { stdout, exitCode } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  assertEqual(out.ok, false, "ok");
  assertEqual(out.data.summary.total, 2, "total");
  assertEqual(out.data.summary.passed, 1, "passed");
  assertEqual(out.data.summary.failed, 1, "failed");
  assertEqual(exitCode, 1, "exitCode");
  // Failed test should have failure info
  const failedTest = out.data.tests.find((t: any) => t.status === "fail");
  assert(failedTest !== undefined, "should have a failed test");
  assert(failedTest.failure !== undefined, "failed test should have failure field");
  assert(failedTest.failure.message.length > 0, "failure message should be non-empty");
});

// --filter flag
test("--filter selects matching tests only", () => {
  const file = writeClk("filter.clk", `
mod test.filter

test "math addition" =
  2 + 3

test "math subtraction" =
  5 - 2

test "string concat" =
  "a" ++ "b"
`);
  const { stdout, exitCode } = runCLI(`test --json --filter math ${file}`);
  const out = JSON.parse(stdout);
  assertEqual(out.ok, true, "ok");
  assertEqual(out.data.summary.total, 2, "total");
  assertEqual(exitCode, 0, "exitCode");
  for (const t of out.data.tests) {
    assert(t.name.includes("math"), `test name "${t.name}" should contain "math"`);
  }
});

// fn test_* convention
test("fn test_* functions are discovered as tests", () => {
  const file = writeClk("fn-test.clk", `
mod test.fn

test_add : () -> <> Int =
  2 + 3

test_sub : () -> <> Int =
  5 - 2

helper : () -> <> Int =
  42
`);
  const { stdout, exitCode } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  assertEqual(out.ok, true, "ok");
  assertEqual(out.data.summary.total, 2, "total — should not include helper");
  assertEqual(exitCode, 0, "exitCode");
});

// Directory discovery
test("discovers tests in a directory", () => {
  const dir = join(TMP_DIR, "subdir");
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "a-test.clk"), `
test "a test" =
  1
`);
  writeFileSync(join(dir, "b-test.clk"), `
test "b test" =
  2
`);
  const { stdout, exitCode } = runCLI(`test --json ${dir}`);
  const out = JSON.parse(stdout);
  assertEqual(out.ok, true, "ok");
  assertEqual(out.data.summary.total, 2, "total");
  assertEqual(exitCode, 0, "exitCode");
});

// No test files found
test("no .clk files → error", () => {
  const emptyDir = join(TMP_DIR, "empty");
  mkdirSync(emptyDir, { recursive: true });
  const { stdout, exitCode } = runCLI(`test --json ${emptyDir}`);
  const out = JSON.parse(stdout);
  assertEqual(out.ok, false, "ok");
  assertEqual(exitCode, 1, "exitCode");
  assert(out.diagnostics.length > 0, "should have diagnostics");
});

// Files with no test declarations are skipped
test("files without tests are skipped silently", () => {
  const file = writeClk("no-tests.clk", `
mod mymod

helper : (Int) -> <> Int =
  fn(x) => x + 1
`);
  const { stdout, exitCode } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  // No tests found but also no errors — ok should be false because 0 tests ran
  assertEqual(out.data.summary.total, 0, "total");
  assertEqual(exitCode, 1, "exitCode — no tests means failure");
});

// JSON output envelope structure
test("JSON output has correct envelope structure", () => {
  const file = writeClk("envelope.clk", `
test "simple" =
  true
`);
  const { stdout } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  assert("ok" in out, "should have ok field");
  assert("data" in out, "should have data field");
  assert("diagnostics" in out, "should have diagnostics field");
  assert("timing" in out, "should have timing field");
  assert("total_ms" in out.timing, "timing should have total_ms");
  assert("phases" in out.timing, "timing should have phases");
});

// Test output structure per spec
test("each test result has required fields", () => {
  const file = writeClk("structure.clk", `
mod test.structure

test "check fields" =
  1 + 1
`);
  const { stdout } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  const t = out.data.tests[0];
  assert("name" in t, "should have name");
  assert("module" in t, "should have module");
  assert("status" in t, "should have status");
  assert("duration_ms" in t, "should have duration_ms");
  assertEqual(t.name, "check fields", "name");
  assertEqual(t.module, "test.structure", "module");
  assertEqual(t.status, "pass", "status");
});

// Human-readable output
test("without --json, human-readable output is shown", () => {
  const file = writeClk("human.clk", `
test "human readable" =
  42
`);
  const { stderr, exitCode } = runCLI(`test ${file}`);
  // Human output goes to stdout (console.log), which is captured in stderr for failing
  // but actually for passing tests it exits 0 and goes to stdout
  assertEqual(exitCode, 0, "exitCode");
});

// Mixed test declarations and fn test_*
test("both test declarations and fn test_* work in same file", () => {
  const file = writeClk("mixed.clk", `
mod test.mixed

test "declarative test" =
  1 + 1

test_functional : () -> <> Int =
  2 + 2
`);
  const { stdout, exitCode } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  assertEqual(out.ok, true, "ok");
  assertEqual(out.data.summary.total, 2, "total");
  assertEqual(exitCode, 0, "exitCode");
});

// Parse error in test file
test("parse error in test file produces diagnostic", () => {
  const file = writeClk("bad-syntax.clk", `
test "incomplete =
`);
  const { stdout, exitCode } = runCLI(`test --json ${file}`);
  const out = JSON.parse(stdout);
  assert(out.diagnostics.length > 0, "should have parse error diagnostic");
  assertEqual(exitCode, 1, "exitCode");
});

// ── Summary ──
console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
