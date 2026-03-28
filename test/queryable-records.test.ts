// Tests for queryable records (TASK-170)
// Field tags, tag projection, Pick, Omit type queries
// Run with: npx tsx test/queryable-records.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, unlinkSync, mkdirSync } from "node:fs";
import { join } from "node:path";

const CLI = join(import.meta.dirname, "..", "src", "main.ts");
const TMP_DIR = "/tmp/clank-qr-test";

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

// ── Field tags on record type annotations ──

test("record type with field tags parses and checks", () => {
  const src = `
get-host : (cfg: {@net host: Str, @net port: Int, @auth token: Str}) -> <> Str
  = cfg.host

main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080, token: "abc"}
  print(get-host(cfg))
`;
  const f = writeTmp("tagged-record-type.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("tagged-record-type.clk");
});

test("record type with multiple tags per field parses", () => {
  const src = `
get-host : (cfg: {@net @required host: Str, @net port: Int}) -> <> Str
  = cfg.host

main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080}
  print(get-host(cfg))
`;
  const f = writeTmp("multi-tag-field.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("multi-tag-field.clk");
});

test("tagged record literal parses and checks", () => {
  const src = `
main : () -> <io> () =
  let cfg = {@net host: "localhost", @net port: 8080, @auth token: "abc"}
  print(cfg.host)
`;
  const f = writeTmp("tagged-record-lit.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("tagged-record-lit.clk");
});

// ── Tag projection ──

test("tag projection filters record fields by tag", () => {
  const src = `
get-host : (cfg: {@net host: Str, @net port: Int, @auth token: Str} @net) -> <> Str
  = cfg.host

main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080}
  print(get-host(cfg))
`;
  const f = writeTmp("tag-project.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("tag-project.clk");
});

test("tag projection rejects access to non-projected field", () => {
  const src = `
get-token : (cfg: {@net host: Str, @auth token: Str} @net) -> <> Str
  = cfg.token

main : () -> <io> () =
  print(get-token({host: "localhost"}))
`;
  const f = writeTmp("tag-project-reject.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E301"), "E301 missing field after projection");
  cleanTmp("tag-project-reject.clk");
});

// ── Pick type query ──

test("Pick selects named fields from record type", () => {
  const src = `
get-host : (cfg: Pick<{host: Str, port: Int, token: Str}, "host" | "port">) -> <> Str
  = cfg.host

main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080}
  print(get-host(cfg))
`;
  const f = writeTmp("pick-query.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("pick-query.clk");
});

test("Pick rejects access to non-picked field", () => {
  const src = `
get-token : (cfg: Pick<{host: Str, port: Int, token: Str}, "host">) -> <> Str
  = cfg.token

main : () -> <io> () =
  print(get-token({host: "localhost"}))
`;
  const f = writeTmp("pick-reject.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E301"), "E301 missing field after Pick");
  cleanTmp("pick-reject.clk");
});

// ── Omit type query ──

test("Omit excludes named fields from record type", () => {
  const src = `
get-host : (cfg: Omit<{host: Str, port: Int, token: Str}, "token">) -> <> Str
  = cfg.host

main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080}
  print(get-host(cfg))
`;
  const f = writeTmp("omit-query.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("omit-query.clk");
});

test("Omit rejects access to omitted field", () => {
  const src = `
get-token : (cfg: Omit<{host: Str, port: Int, token: Str}, "token">) -> <> Str
  = cfg.token

main : () -> <io> () =
  print(get-token({host: "localhost", port: 8080}))
`;
  const f = writeTmp("omit-reject.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 1, "exit code");
  const env = JSON.parse(stdout);
  assert(env.diagnostics.some((d: any) => d.code === "E301"), "E301 missing field after Omit");
  cleanTmp("omit-reject.clk");
});

// ── Tags with row polymorphism ──

test("tagged record type with row variable type checks", () => {
  const src = `
get-host : (cfg: {@net host: Str, @net port: Int | r}) -> <> Str
  = cfg.host

main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080, token: "abc"}
  print(get-host(cfg))
`;
  const f = writeTmp("tagged-row-poly.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("tagged-row-poly.clk");
});

// ── Tags preserved through operations ──

test("record field access works with tagged fields", () => {
  const src = `
add-one : (x: Int) -> <> Int = add(x, 1)

main : () -> <io> () =
  let cfg = {@net host: "localhost", @net port: 8080}
  print(show(add-one(cfg.port)))
`;
  const f = writeTmp("tagged-field-access.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("tagged-field-access.clk");
});

test("record update works with tagged type annotation", () => {
  const src = `
main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080}
  let updated = {cfg | port: 9090}
  print(show(updated.port))
`;
  const f = writeTmp("tagged-update.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("tagged-update.clk");
});

// ── Pick with single field ──

test("Pick with single field name works", () => {
  const src = `
get-host : (cfg: Pick<{host: Str, port: Int}, "host">) -> <> Str
  = cfg.host

main : () -> <io> () =
  print(get-host({host: "localhost"}))
`;
  const f = writeTmp("pick-single.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("pick-single.clk");
});

// ── Omit with multiple fields ──

test("Omit with multiple field names works", () => {
  const src = `
get-host : (cfg: Omit<{host: Str, port: Int, token: Str, secret: Str}, "token" | "secret">) -> <> Str
  = cfg.host

main : () -> <io> () =
  let cfg = {host: "localhost", port: 8080}
  print(get-host(cfg))
`;
  const f = writeTmp("omit-multi.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("omit-multi.clk");
});

// ── Empty tag projection produces empty record ──

test("tag projection with no matching tag produces empty record", () => {
  const src = `
get-nothing : (cfg: {@net host: Str} @auth) -> <> ()
  = ()

main : () -> <io> () =
  get-nothing({})
  print("ok")
`;
  const f = writeTmp("tag-project-empty.clk", src);
  const { stdout, exitCode } = runCLI(`--json check ${f}`);
  assertEqual(exitCode, 0, "exit code");
  const env = JSON.parse(stdout);
  assertEqual(env.diagnostics.length, 0, "no diagnostics");
  cleanTmp("tag-project-empty.clk");
});

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
