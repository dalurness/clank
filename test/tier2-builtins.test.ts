// Integration tests for tier-2 stdlib builtins: http, srv, csv, proc, dt
// Run with: npx tsx test/tier2-builtins.test.ts

import { compileProgram, type BytecodeModule } from "../src/compiler.js";
import { VM, execute, Val, Tag, type Value } from "../src/vm.js";
import { desugar } from "../src/desugar.js";
import type { Expr, Program, TopLevel, TypeSig, Loc } from "../src/ast.js";

const loc: Loc = { line: 1, col: 1 };

// ── AST helpers ──

const litInt = (n: number): Expr => ({ tag: "literal", value: { tag: "int", value: n }, loc });
const litBool = (v: boolean): Expr => ({ tag: "literal", value: { tag: "bool", value: v }, loc });
const litStr = (s: string): Expr => ({ tag: "literal", value: { tag: "str", value: s }, loc });
const litUnit = (): Expr => ({ tag: "literal", value: { tag: "unit" }, loc });
const varRef = (name: string): Expr => ({ tag: "var", name, loc });
const letExpr = (name: string, value: Expr, body: Expr | null): Expr => ({ tag: "let", name, value, body, loc });
const apply = (fn: Expr, args: Expr[]): Expr => ({ tag: "apply", fn, args, loc });
const lambda = (params: string[], body: Expr): Expr => ({
  tag: "lambda",
  params: params.map(name => ({ name, type: null })),
  body,
  loc,
});
const infix = (op: string, left: Expr, right: Expr): Expr => ({ tag: "infix", op, left, right, loc });
const list = (elements: Expr[]): Expr => ({ tag: "list", elements, loc });
const fieldAccess = (obj: Expr, field: string): Expr => ({ tag: "field-access", object: obj, field, loc });
const dottedCall = (ns: string, fn: string, args: Expr[]): Expr =>
  apply(fieldAccess(varRef(ns), fn), args);

function sig(paramNames: string[]): TypeSig {
  return {
    params: paramNames.map(name => ({ name, type: { tag: "t-name", name: "Int", loc } })),
    effects: [],
    returnType: { tag: "t-name", name: "Int", loc },
  };
}

function def(name: string, params: string[], body: Expr, pub = true): TopLevel {
  return { tag: "definition", name, sig: sig(params), body: desugar(body), pub, loc };
}

function program(...topLevels: TopLevel[]): Program {
  return { topLevels };
}

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

function compileAndRun(...topLevels: TopLevel[]): { result: Value | undefined; stdout: string[] } {
  const mod = compileProgram(program(...topLevels));
  return execute(mod);
}

function runMain(params: string[], body: Expr): Value | undefined {
  return compileAndRun(def("main", params, body)).result;
}

function assertIntResult(result: Value | undefined, expected: number, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.INT, `${msg} expected Int, got tag ${result!.tag}`);
  assertEq((result as any).value, expected, msg);
}

function assertBoolResult(result: Value | undefined, expected: boolean, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.BOOL, `${msg} expected Bool, got tag ${result!.tag}`);
  assertEq((result as any).value, expected, msg);
}

function assertStrResult(result: Value | undefined, expected: string, msg = ""): void {
  assert(result !== undefined, `${msg} result is undefined`);
  assert(result!.tag === Tag.STR, `${msg} expected Str, got tag ${result!.tag}`);
  assertEq((result as any).value, expected, msg);
}

function assertRecordField(result: Value | undefined, field: string, expectedTag: number, expectedValue?: any): void {
  assert(result !== undefined, "result is undefined");
  assert(result!.tag === Tag.HEAP, `expected HEAP (record), got tag ${result!.tag}`);
  const obj = (result as any).value;
  assert(obj.kind === "record", `expected record, got ${obj.kind}`);
  const f = obj.fields.get(field);
  assert(f !== undefined, `field "${field}" not found in record`);
  assert(f.tag === expectedTag, `field "${field}": expected tag ${expectedTag}, got ${f.tag}`);
  if (expectedValue !== undefined) {
    assertEq(f.value, expectedValue, `field "${field}"`);
  }
}

// ── Tests ──

console.log("\nTier 2 Builtin Tests\n");

// ── CSV ──

console.log("CSV:");

test("csv.dec parses basic CSV", () => {
  const result = runMain([], dottedCall("csv", "dec", [litStr("a,b,c\n1,2,3")]));
  assert(result !== undefined, "result is undefined");
  assert(result!.tag === Tag.HEAP, "expected HEAP (list)");
  const obj = (result as any).value;
  assert(obj.kind === "list", `expected list, got ${obj.kind}`);
  assertEq(obj.items.length, 2, "expected 2 rows");
});

test("csv.enc encodes rows to CSV string", () => {
  // Build: csv.enc([[litStr("a"), litStr("b")], [litStr("1"), litStr("2")]])
  const rows = list([list([litStr("a"), litStr("b")]), list([litStr("1"), litStr("2")])]);
  const result = runMain([], dottedCall("csv", "enc", [rows]));
  assertStrResult(result, "a,b\n1,2");
});

test("csv.hdr extracts header row", () => {
  const result = runMain([], dottedCall("csv", "hdr", [
    dottedCall("csv", "dec", [litStr("name,age\nalice,30")])
  ]));
  assert(result !== undefined, "result is undefined");
  assert(result!.tag === Tag.HEAP, "expected HEAP (list)");
  const items = (result as any).value.items;
  assertEq(items.length, 2, "expected 2 cells in header");
  assertEq(items[0].value, "name", "first header cell");
  assertEq(items[1].value, "age", "second header cell");
});

test("csv.rows skips header", () => {
  const result = runMain([], dottedCall("csv", "rows", [
    dottedCall("csv", "dec", [litStr("name,age\nalice,30\nbob,25")])
  ]));
  assert(result !== undefined, "result is undefined");
  const items = (result as any).value.items;
  assertEq(items.length, 2, "expected 2 data rows");
});

test("csv.maps returns rows as records keyed by header", () => {
  const result = runMain([], dottedCall("csv", "maps", [
    dottedCall("csv", "dec", [litStr("name,age\nalice,30")])
  ]));
  assert(result !== undefined, "result is undefined");
  const items = (result as any).value.items;
  assertEq(items.length, 1, "expected 1 data row");
  // First map should have "name" and "age" fields
  const fields = items[0].value.fields;
  assertEq(fields.get("name").value, "alice");
  assertEq(fields.get("age").value, "30");
});

// ── srv (HTTP Server) ──

console.log("\nHTTP Server (srv):");

test("srv.new creates empty route list", () => {
  const result = runMain([], dottedCall("srv", "new", [litUnit()]));
  assert(result !== undefined, "result is undefined");
  assert(result!.tag === Tag.HEAP, "expected HEAP");
  assertEq((result as any).value.kind, "list");
  assertEq((result as any).value.items.length, 0, "expected empty list");
});

test("srv.get adds a GET route", () => {
  const routes = dottedCall("srv", "new", [litUnit()]);
  const handler = lambda(["req"], litStr("hello"));
  const result = runMain([], dottedCall("srv", "get", [routes, litStr("/hello"), handler]));
  assert(result !== undefined, "result is undefined");
  const items = (result as any).value.items;
  assertEq(items.length, 1, "expected 1 route");
  const routeFields = items[0].value.fields;
  assertEq(routeFields.get("method").value, "GET", "method should be GET");
  assertEq(routeFields.get("path").value, "/hello", "path should be /hello");
});

test("srv.res creates response record", () => {
  const result = runMain([], dottedCall("srv", "res", [litInt(200), litStr("OK")]));
  assertRecordField(result, "status", Tag.INT, 200);
  assertRecordField(result, "body", Tag.STR, "OK");
});

test("srv.start returns server handle", () => {
  const routes = dottedCall("srv", "new", [litUnit()]);
  const result = runMain([], dottedCall("srv", "start", [routes, litInt(8080)]));
  assertRecordField(result, "port", Tag.INT, 8080);
  assertRecordField(result, "running", Tag.BOOL, true);
});

test("srv.stop returns unit", () => {
  const routes = dottedCall("srv", "new", [litUnit()]);
  const server = dottedCall("srv", "start", [routes, litInt(8080)]);
  const result = runMain([], dottedCall("srv", "stop", [server]));
  assert(result !== undefined, "result is undefined");
  assertEq(result!.tag, Tag.UNIT, "expected unit");
});

// ── proc (Process) ──

console.log("\nProcess (proc):");

test("proc.run executes command and returns ProcResult", () => {
  const result = runMain([], dottedCall("proc", "run", [litStr("echo"), list([litStr("hello")])]));
  assertRecordField(result, "code", Tag.INT, 0);
  // stdout should contain "hello\n"
  assert(result !== undefined, "result is undefined");
  const out = (result as any).value.fields.get("out");
  assert(out.tag === Tag.STR, "expected string output");
  assert(out.value.includes("hello"), `expected 'hello' in output, got '${out.value}'`);
});

test("proc.sh runs shell command", () => {
  const result = runMain([], dottedCall("proc", "sh", [litStr("echo test123")]));
  assertRecordField(result, "code", Tag.INT, 0);
  const out = (result as any).value.fields.get("out");
  assert(out.value.includes("test123"), "expected 'test123' in output");
});

test("proc.ok returns stdout on success", () => {
  const result = runMain([], dottedCall("proc", "ok", [litStr("echo"), list([litStr("success")])]));
  assertStrResult(result, "success\n");
});

test("proc.pipe passes stdin to command", () => {
  const result = runMain([], dottedCall("proc", "pipe", [litStr("cat"), list([]), litStr("piped input")]));
  assertRecordField(result, "code", Tag.INT, 0);
  const out = (result as any).value.fields.get("out");
  assertEq(out.value, "piped input", "expected piped input in output");
});

test("proc.pid returns a number", () => {
  const result = runMain([], dottedCall("proc", "pid", [litUnit()]));
  assert(result !== undefined, "result is undefined");
  assertEq(result!.tag, Tag.INT, "expected Int");
  assert((result as any).value > 0, "PID should be positive");
});

// ── dt (DateTime) ──

console.log("\nDateTime (dt):");

test("dt.unix returns current unix timestamp", () => {
  const result = runMain([], dottedCall("dt", "unix", [litUnit()]));
  assert(result !== undefined, "result is undefined");
  assertEq(result!.tag, Tag.INT, "expected Int");
  // Should be a reasonable unix timestamp (after 2020)
  assert((result as any).value > 1577836800, "timestamp should be after 2020");
});

test("dt.from creates datetime from unix timestamp", () => {
  // 2025-01-01 00:00:00 UTC = 1735689600
  const result = runMain([], dottedCall("dt", "from", [litInt(1735689600)]));
  assertRecordField(result, "year", Tag.INT, 2025);
  assertRecordField(result, "month", Tag.INT, 1);
  assertRecordField(result, "day", Tag.INT, 1);
});

test("dt.to converts datetime back to unix timestamp", () => {
  const dt = dottedCall("dt", "from", [litInt(1735689600)]);
  const result = runMain([], dottedCall("dt", "to", [dt]));
  assertIntResult(result, 1735689600);
});

test("dt.iso formats as ISO 8601", () => {
  const dt = dottedCall("dt", "from", [litInt(1735689600)]);
  const result = runMain([], dottedCall("dt", "iso", [dt]));
  assertStrResult(result, "2025-01-01T00:00:00.000Z");
});

test("dt.fmt formats with pattern", () => {
  const dt = dottedCall("dt", "from", [litInt(1735689600)]);
  const result = runMain([], dottedCall("dt", "fmt", [dt, litStr("YYYY-MM-DD")]));
  assertStrResult(result, "2025-01-01");
});

test("dt.add adds duration to datetime", () => {
  const dt = dottedCall("dt", "from", [litInt(1735689600)]);
  // Add 1 day in ms = 86400000
  const result = runMain([], dottedCall("dt", "to", [
    dottedCall("dt", "add", [dt, dottedCall("dt", "day", [litInt(1)])])
  ]));
  assertIntResult(result, 1735689600 + 86400);
});

test("dt.sub returns difference between datetimes (ms)", () => {
  const dt1 = dottedCall("dt", "from", [litInt(1735776000)]); // 2025-01-02
  const dt2 = dottedCall("dt", "from", [litInt(1735689600)]); // 2025-01-01
  const result = runMain([], dottedCall("dt", "sub", [dt1, dt2]));
  assertIntResult(result, 86400000); // 1 day in ms
});

test("dt.sec converts seconds to ms", () => {
  const result = runMain([], dottedCall("dt", "sec", [litInt(5)]));
  assertIntResult(result, 5000);
});

test("dt.min converts minutes to ms", () => {
  const result = runMain([], dottedCall("dt", "min", [litInt(2)]));
  assertIntResult(result, 120000);
});

test("dt.hr converts hours to ms", () => {
  const result = runMain([], dottedCall("dt", "hr", [litInt(1)]));
  assertIntResult(result, 3600000);
});

test("dt.day converts days to ms", () => {
  const result = runMain([], dottedCall("dt", "day", [litInt(1)]));
  assertIntResult(result, 86400000);
});

test("dt.ms is identity for milliseconds", () => {
  const result = runMain([], dottedCall("dt", "ms", [litInt(42)]));
  assertIntResult(result, 42);
});

test("dt.now returns a record with year/month/day fields", () => {
  const result = runMain([], dottedCall("dt", "now", [litUnit()]));
  assert(result !== undefined, "result is undefined");
  assertEq(result!.tag, Tag.HEAP, "expected HEAP (record)");
  const fields = (result as any).value.fields;
  assert(fields.has("year"), "expected year field");
  assert(fields.has("month"), "expected month field");
  assert(fields.has("day"), "expected day field");
  assert(fields.get("year").value >= 2025, "year should be >= 2025");
});

// ── http.ok? ──

console.log("\nHTTP helpers:");

test("http.ok? returns true for 200 status", () => {
  // Build a record with status: 200
  const res = dottedCall("srv", "res", [litInt(200), litStr("OK")]); // reuse srv.res to create a response-like record
  const result = runMain([], dottedCall("http", "ok?", [res]));
  assertBoolResult(result, true);
});

test("http.ok? returns false for 404 status", () => {
  const res = dottedCall("srv", "res", [litInt(404), litStr("Not Found")]);
  const result = runMain([], dottedCall("http", "ok?", [res]));
  assertBoolResult(result, false);
});

test("http.ok? returns true for 201 status", () => {
  const res = dottedCall("srv", "res", [litInt(201), litStr("Created")]);
  const result = runMain([], dottedCall("http", "ok?", [res]));
  assertBoolResult(result, true);
});

test("http.ok? returns false for 500 status", () => {
  const res = dottedCall("srv", "res", [litInt(500), litStr("Error")]);
  const result = runMain([], dottedCall("http", "ok?", [res]));
  assertBoolResult(result, false);
});

// ── Summary ──

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
