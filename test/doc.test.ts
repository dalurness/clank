// Tests for clank doc subcommand
// Run with: npx tsx test/doc.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, mkdirSync, rmSync } from "node:fs";
import { join } from "node:path";

const CLI = join(import.meta.dirname, "..", "ts", "src", "main.ts");
const TMP_DIR = "/tmp/clank-doc-test";

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

console.log("doc subcommand tests:");

// ── Search by name ──

test("search builtin by name: map", () => {
  const r = runCLI("doc search map --json");
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === true, "expected ok");
  assert(out.data.entries.length > 0, "expected entries");
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("map"), `expected 'map' in results, got ${names}`);
});

test("search builtin by name: add", () => {
  const r = runCLI("doc search add --json");
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === true, "expected ok");
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("add"), `expected 'add' in results`);
});

test("search with no results", () => {
  const r = runCLI("doc search zzzznothing --json");
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === true, "expected ok");
  assert(out.data.entries.length === 0, "expected no entries");
});

test("search is case-insensitive", () => {
  const r = runCLI("doc search MAP --json");
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("map"), "expected case-insensitive match");
});

// ── Show ──

test("show builtin: fold", () => {
  const r = runCLI("doc show fold --json");
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === true, "expected ok");
  assert(out.data.name === "fold", `expected name 'fold', got ${out.data.name}`);
  assert(out.data.kind === "builtin", `expected kind 'builtin'`);
  assert(out.data.signature.length > 0, "expected non-empty signature");
});

test("show non-existent entry", () => {
  const r = runCLI("doc show nonexistent --json");
  assert(r.exitCode === 1, `expected exit code 1, got ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === false, "expected not ok");
});

// ── Type-directed search ──

test("type search: Int -> Int", () => {
  const r = runCLI('doc search "Int -> Int" --json');
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === true, "expected ok");
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("negate"), `expected 'negate' in results, got ${names}`);
});

test("type search: Int -> Int -> Int matches arithmetic", () => {
  const r = runCLI('doc search "Int -> Int -> Int" --json');
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("add"), `expected 'add' in results, got ${names}`);
  assert(names.includes("sub"), `expected 'sub' in results`);
});

test("type search: Str -> Str", () => {
  const r = runCLI('doc search "Str -> Str" --json');
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("trim"), `expected 'trim' in results, got ${names}`);
});

test("type search with wildcard: a -> Str", () => {
  const r = runCLI('doc search "a -> Str" --json');
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("show"), `expected 'show' in results, got ${names}`);
  assert(names.includes("trim"), `expected 'trim' in results`);
});

test("type search: Bool -> Bool", () => {
  const r = runCLI('doc search "Bool -> Bool" --json');
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("not"), `expected 'not' in results, got ${names}`);
});

// ── User-defined functions ──

test("search finds user-defined functions from file", () => {
  const src = `
factorial : (n: Int) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)

main : () -> <io> () =
  print(show(factorial(5)))
`;
  const file = join(TMP_DIR, "factorial.clk");
  writeFileSync(file, src);
  const r = runCLI(`doc search factorial --json ${file}`);
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === true, "expected ok");
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("factorial"), `expected 'factorial' in results, got ${names}`);
});

test("show user-defined function with params", () => {
  const src = `
double : (x: Int) -> <> Int =
  x * 2
`;
  const file = join(TMP_DIR, "double.clk");
  writeFileSync(file, src);
  const r = runCLI(`doc show double --json ${file}`);
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  assert(out.ok === true, "expected ok");
  assert(out.data.name === "double", "expected name 'double'");
  assert(out.data.kind === "function", "expected kind 'function'");
  assert(out.data.params.length === 1, "expected 1 param");
  assert(out.data.params[0].name === "x", "expected param name 'x'");
  assert(out.data.returnType === "Int", "expected return type 'Int'");
});

test("type search finds user-defined functions", () => {
  const src = `
isEven : (n: Int) -> <> Bool =
  n == 0
`;
  const file = join(TMP_DIR, "iseven.clk");
  writeFileSync(file, src);
  const r = runCLI(`doc search "Int -> Bool" --json ${file}`);
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  const out = JSON.parse(r.stdout);
  const names = out.data.entries.map((e: any) => e.name);
  assert(names.includes("isEven"), `expected 'isEven' in results, got ${names}`);
});

// ── Plain text output ──

test("plain text search output", () => {
  const r = runCLI("doc search trim");
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  assert(r.stdout.includes("trim"), "expected 'trim' in output");
  assert(r.stdout.includes("builtin"), "expected 'builtin' tag in output");
});

test("plain text show output", () => {
  const r = runCLI("doc show print");
  assert(r.exitCode === 0, `exit code ${r.exitCode}`);
  assert(r.stdout.includes("print"), "expected 'print' in output");
  assert(r.stdout.includes("Signature"), "expected 'Signature' label in output");
});

// ── Error cases ──

test("doc with no subcommand shows error", () => {
  const r = runCLI("doc --json");
  assert(r.exitCode === 1, `expected exit code 1, got ${r.exitCode}`);
});

test("doc search with no query shows error", () => {
  const r = runCLI("doc search --json");
  assert(r.exitCode === 1, `expected exit code 1, got ${r.exitCode}`);
});

// ── Cleanup ──
rmSync(TMP_DIR, { recursive: true, force: true });

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
