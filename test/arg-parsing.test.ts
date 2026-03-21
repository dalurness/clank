// Tests for CLI arg parsing — --flag <value> pairs must not leak into positional args
// Run with: npx tsx test/arg-parsing.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, unlinkSync, mkdirSync } from "node:fs";
import { join } from "node:path";

const CLI = join(import.meta.dirname, "..", "src", "main.ts");
const TMP_DIR = "/tmp/clank-argparse-test";

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

console.log("── arg-parsing tests ──");

// ── eval --file <value> should not leak value into positional args ──

test("eval --file value does not leak into positional args", () => {
  const file = writeTmp("lib.clk", `x : () -> <> Int = 42\n`);
  const result = runCLI(`eval --file ${file} "x()"`);
  assert(result.exitCode === 0, `expected exit 0, got ${result.exitCode}: stderr=${result.stderr}`);
  assert(result.stdout.includes("42"), `expected 42 in stdout, got: ${result.stdout}`);
  cleanTmp("lib.clk");
});

test("eval --file value with --json does not leak", () => {
  const file = writeTmp("lib2.clk", `x : () -> <> Int = 42\n`);
  const result = runCLI(`eval --json --file ${file} "x()"`);
  assert(result.exitCode === 0, `expected exit 0, got ${result.exitCode}: stderr=${result.stderr}`);
  const envelope = JSON.parse(result.stdout);
  assert(envelope.ok === true, `expected ok=true, got ${envelope.ok}`);
  cleanTmp("lib2.clk");
});

test("eval --session value does not leak into positional args", () => {
  const result = runCLI(`eval --session test-sess "1 + 1"`);
  assert(result.exitCode === 0, `expected exit 0, got ${result.exitCode}: stderr=${result.stderr}`);
  assert(result.stdout.includes("2"), `expected 2, got: ${result.stdout}`);
});

// ── fmt with boolean flags should still work ──

test("fmt --check with file arg works", () => {
  const file = writeTmp("formatted.clk", `x : () -> <> Int = 1\n`);
  const result = runCLI(`fmt --check ${file}`);
  // Exit code 0 = already formatted, 1 = needs formatting — both are valid
  assert(result.exitCode === 0 || result.exitCode === 1,
    `expected exit 0 or 1, got ${result.exitCode}`);
  cleanTmp("formatted.clk");
});

// ── check subcommand with --json flag ──

test("check --json with file arg does not leak flag value", () => {
  const file = writeTmp("valid.clk", `x : () -> <> Int = 1\n`);
  const result = runCLI(`check --json ${file}`);
  assert(result.exitCode === 0, `expected exit 0, got ${result.exitCode}: stderr=${result.stderr}`);
  const envelope = JSON.parse(result.stdout);
  assert(envelope.ok === true, `expected ok=true`);
  cleanTmp("valid.clk");
});

// ── lint --rule <value> should not leak into positional args ──

test("lint --rule value does not leak into targets", () => {
  const file = writeTmp("lintme.clk", `x : () -> <> Int = 1\n`);
  const result = runCLI(`lint --rule unused-binding ${file}`);
  // Should not try to open "unused-binding" as a file
  assert(result.exitCode === 0 || result.exitCode === 1,
    `expected exit 0 or 1, got ${result.exitCode}: stderr=${result.stderr}`);
  cleanTmp("lintme.clk");
});

// ── Mixed flags ──

test("eval with multiple flag-value pairs", () => {
  const file = writeTmp("multi.clk", `y : () -> <> Int = 10\n`);
  const result = runCLI(`eval --file ${file} --session multi-test "y()"`);
  assert(result.exitCode === 0, `expected exit 0, got ${result.exitCode}: stderr=${result.stderr}`);
  assert(result.stdout.includes("10"), `expected 10, got: ${result.stdout}`);
  cleanTmp("multi.clk");
});

test("flags after positional args are handled correctly", () => {
  const file = writeTmp("after.clk", `z : () -> <> Int = 5\n`);
  const result = runCLI(`eval "z()" --file ${file}`);
  assert(result.exitCode === 0, `expected exit 0, got ${result.exitCode}: stderr=${result.stderr}`);
  assert(result.stdout.includes("5"), `expected 5, got: ${result.stdout}`);
  cleanTmp("after.clk");
});

// ── Summary ──

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
