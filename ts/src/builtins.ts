// Clank built-in functions
// Arithmetic, comparison, logic, and string operations

import type { Loc } from "./ast.js";
import type { Value, RuntimeError } from "./eval.js";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);

function runtimeError(code: string, message: string, loc: Loc): RuntimeError {
  return { code, message, location: loc };
}

function expectNum(v: Value, loc: Loc): number {
  if (v.tag === "int" || v.tag === "rat") return v.value;
  throw runtimeError("E200", `expected number, got ${v.tag}`, loc);
}

function expectBool(v: Value, loc: Loc): boolean {
  if (v.tag === "bool") return v.value;
  throw runtimeError("E200", `expected Bool, got ${v.tag}`, loc);
}

function expectStr(v: Value, loc: Loc): string {
  if (v.tag === "str") return v.value;
  throw runtimeError("E200", `expected Str, got ${v.tag}`, loc);
}

function expectList(v: Value, loc: Loc): Value[] {
  if (v.tag === "list") return v.elements;
  throw runtimeError("E200", `expected List, got ${v.tag}`, loc);
}

function expectInt(v: Value, loc: Loc): number {
  if (v.tag === "int") return v.value;
  throw runtimeError("E200", `expected Int, got ${v.tag}`, loc);
}

// Higher-order builtins need to call user functions.
// eval.ts registers this at startup to avoid circular imports.
let _applyFn: (fn: Value, args: Value[], loc: Loc) => Value = () => {
  throw new Error("applyFn not initialized");
};

export function setApplyFn(fn: (fn: Value, args: Value[], loc: Loc) => Value): void {
  _applyFn = fn;
}

// Show converts any value to its string representation
function showValue(v: Value): string {
  switch (v.tag) {
    case "int": return String(v.value);
    case "rat": return String(v.value);
    case "bool": return v.value ? "true" : "false";
    case "str": return v.value;
    case "unit": return "()";
    case "list": return "[" + v.elements.map(showValue).join(", ") + "]";
    case "tuple": return "(" + v.elements.map(showValue).join(", ") + ")";
    case "record": return "{" + [...v.fields.entries()].map(([k, val]) => `${k}: ${showValue(val)}`).join(", ") + "}";
    case "variant": return v.fields.length > 0
      ? `${v.name}(${v.fields.map(showValue).join(", ")})`
      : v.name;
    case "closure": return "<fn>";
    case "builtin": return `<builtin:${v.name}>`;
    case "effect-def": return `<effect:${v.name}>`;
    case "future": return `<future>`;
    case "sender": return `<sender>`;
    case "receiver": return `<receiver>`;
  }
}

type BuiltinFn = (args: Value[], loc: Loc) => Value;

export const builtins: Record<string, BuiltinFn> = {
  // Arithmetic
  add(args, loc) {
    const [a, b] = [expectNum(args[0], loc), expectNum(args[1], loc)];
    const isRat = args[0].tag === "rat" || args[1].tag === "rat";
    return { tag: isRat ? "rat" : "int", value: a + b };
  },
  sub(args, loc) {
    const [a, b] = [expectNum(args[0], loc), expectNum(args[1], loc)];
    const isRat = args[0].tag === "rat" || args[1].tag === "rat";
    return { tag: isRat ? "rat" : "int", value: a - b };
  },
  mul(args, loc) {
    const [a, b] = [expectNum(args[0], loc), expectNum(args[1], loc)];
    const isRat = args[0].tag === "rat" || args[1].tag === "rat";
    return { tag: isRat ? "rat" : "int", value: a * b };
  },
  div(args, loc) {
    const [a, b] = [expectNum(args[0], loc), expectNum(args[1], loc)];
    if (b === 0) throw runtimeError("E201", "division by zero", loc);
    if (args[0].tag === "rat" || args[1].tag === "rat") {
      return { tag: "rat", value: a / b };
    }
    return { tag: "int", value: Math.trunc(a / b) };
  },
  mod(args, loc) {
    const [a, b] = [expectNum(args[0], loc), expectNum(args[1], loc)];
    if (b === 0) throw runtimeError("E201", "modulo by zero", loc);
    return { tag: "int", value: a % b };
  },

  // Comparison
  eq(args) { return { tag: "bool", value: valEqual(args[0], args[1]) }; },
  neq(args) { return { tag: "bool", value: !valEqual(args[0], args[1]) }; },
  lt(args, loc) { return { tag: "bool", value: expectNum(args[0], loc) < expectNum(args[1], loc) }; },
  gt(args, loc) { return { tag: "bool", value: expectNum(args[0], loc) > expectNum(args[1], loc) }; },
  lte(args, loc) { return { tag: "bool", value: expectNum(args[0], loc) <= expectNum(args[1], loc) }; },
  gte(args, loc) { return { tag: "bool", value: expectNum(args[0], loc) >= expectNum(args[1], loc) }; },

  // Logic
  and(args, loc) {
    return { tag: "bool", value: expectBool(args[0], loc) && expectBool(args[1], loc) };
  },
  or(args, loc) {
    return { tag: "bool", value: expectBool(args[0], loc) || expectBool(args[1], loc) };
  },
  not(args, loc) {
    return { tag: "bool", value: !expectBool(args[0], loc) };
  },
  negate(args, loc) {
    const n = expectNum(args[0], loc);
    return { tag: args[0].tag as "int" | "rat", value: -n };
  },

  // Strings
  "str.cat"(args, loc) {
    return { tag: "str", value: expectStr(args[0], loc) + expectStr(args[1], loc) };
  },

  // I/O and display
  show(args) {
    return { tag: "str", value: showValue(args[0]) };
  },
  print(args, loc) {
    const s = expectStr(args[0], loc);
    console.log(s);
    return { tag: "unit" };
  },

  // ── List operations ──

  len(args, loc) {
    const list = expectList(args[0], loc);
    return { tag: "int", value: list.length };
  },
  head(args, loc) {
    const list = expectList(args[0], loc);
    if (list.length === 0) throw runtimeError("E208", "head of empty list", loc);
    return list[0];
  },
  tail(args, loc) {
    const list = expectList(args[0], loc);
    if (list.length === 0) throw runtimeError("E208", "tail of empty list", loc);
    return { tag: "list", elements: list.slice(1) };
  },
  cons(args, loc) {
    const list = expectList(args[1], loc);
    return { tag: "list", elements: [args[0], ...list] };
  },
  cat(args, loc) {
    const a = expectList(args[0], loc);
    const b = expectList(args[1], loc);
    return { tag: "list", elements: [...a, ...b] };
  },
  rev(args, loc) {
    const list = expectList(args[0], loc);
    return { tag: "list", elements: [...list].reverse() };
  },
  get(args, loc) {
    const list = expectList(args[0], loc);
    const idx = expectInt(args[1], loc);
    if (idx < 0 || idx >= list.length) {
      throw runtimeError("E209", `index ${idx} out of bounds (length ${list.length})`, loc);
    }
    return list[idx];
  },
  map(args, loc) {
    const list = expectList(args[0], loc);
    const fn = args[1];
    return { tag: "list", elements: list.map(el => _applyFn(fn, [el], loc)) };
  },
  filter(args, loc) {
    const list = expectList(args[0], loc);
    const fn = args[1];
    return {
      tag: "list",
      elements: list.filter(el => {
        const result = _applyFn(fn, [el], loc);
        if (result.tag !== "bool") throw runtimeError("E200", `filter predicate must return Bool, got ${result.tag}`, loc);
        return result.value;
      }),
    };
  },
  fold(args, loc) {
    const list = expectList(args[0], loc);
    const init = args[1];
    const fn = args[2];
    let acc = init;
    for (const el of list) {
      acc = _applyFn(fn, [acc, el], loc);
    }
    return acc;
  },
  "flat-map"(args, loc) {
    const list = expectList(args[0], loc);
    const fn = args[1];
    const result: Value[] = [];
    for (const el of list) {
      const inner = _applyFn(fn, [el], loc);
      const innerList = expectList(inner, loc);
      result.push(...innerList);
    }
    return { tag: "list", elements: result };
  },

  // ── Tuple access ──

  "tuple.get"(args, loc) {
    if (args[0].tag !== "tuple") throw runtimeError("E200", `expected Tuple, got ${args[0].tag}`, loc);
    const idx = expectInt(args[1], loc);
    const elems = args[0].elements;
    if (idx < 0 || idx >= elems.length) {
      throw runtimeError("E209", `tuple index ${idx} out of bounds (size ${elems.length})`, loc);
    }
    return elems[idx];
  },

  // ── Range ──

  range(args, loc) {
    const start = expectInt(args[0], loc);
    const end = expectInt(args[1], loc);
    const elements: Value[] = [];
    for (let i = start; i <= end; i++) {
      elements.push({ tag: "int", value: i });
    }
    return { tag: "list", elements };
  },

  // ── Zip and tuple accessors ──

  zip(args, loc) {
    const xs = expectList(args[0], loc);
    const ys = expectList(args[1], loc);
    const len = Math.min(xs.length, ys.length);
    const elements: Value[] = [];
    for (let i = 0; i < len; i++) {
      elements.push({ tag: "tuple", elements: [xs[i], ys[i]] });
    }
    return { tag: "list", elements };
  },

  fst(args, loc) {
    if (args[0].tag !== "tuple") throw runtimeError("E200", `expected Tuple, got ${args[0].tag}`, loc);
    if (args[0].elements.length < 1) throw runtimeError("E209", "tuple is empty", loc);
    return args[0].elements[0];
  },

  snd(args, loc) {
    if (args[0].tag !== "tuple") throw runtimeError("E200", `expected Tuple, got ${args[0].tag}`, loc);
    if (args[0].elements.length < 2) throw runtimeError("E209", "tuple has fewer than 2 elements", loc);
    return args[0].elements[1];
  },

  // ── String operations ──

  split(args, loc) {
    const s = expectStr(args[0], loc);
    const sep = expectStr(args[1], loc);
    const parts = s.split(sep);
    return { tag: "list", elements: parts.map(p => ({ tag: "str" as const, value: p })) };
  },
  join(args, loc) {
    const list = expectList(args[0], loc);
    const sep = expectStr(args[1], loc);
    const strs = list.map(el => {
      if (el.tag !== "str") throw runtimeError("E200", `join expects list of Str, got ${el.tag}`, loc);
      return el.value;
    });
    return { tag: "str", value: strs.join(sep) };
  },
  trim(args, loc) {
    return { tag: "str", value: expectStr(args[0], loc).trim() };
  },
};

// ── Helper: build a record Value ──

function mkRecord(fields: Record<string, Value>): Value {
  return { tag: "record", fields: new Map(Object.entries(fields)) };
}

function expectRecord(v: Value, loc: Loc): Map<string, Value> {
  if (v.tag === "record") return v.fields;
  throw runtimeError("E200", `expected Record, got ${v.tag}`, loc);
}

// ── Tier 2: HTTP Client (std.http) ──

function httpMethodRequest(method: string, url: string, body: string | null): Value {
  // Minimal synchronous stub using Node's child_process to call curl
  // A real implementation would use fetch/async
  const { execSync } = require("child_process");
  try {
    const bodyArgs = body !== null ? ["-d", body, "-H", "Content-Type: application/json"] : [];
    const cmd = ["curl", "-s", "-X", method, "-w", "\\n%{http_code}", ...bodyArgs, url];
    const raw: string = execSync(cmd.map(a => `'${a}'`).join(" "), {
      encoding: "utf-8",
      timeout: 30000,
    });
    const lines = raw.trimEnd().split("\n");
    const statusCode = parseInt(lines[lines.length - 1], 10);
    const responseBody = lines.slice(0, -1).join("\n");
    return mkRecord({
      status: { tag: "int", value: statusCode },
      headers: { tag: "list", elements: [] },
      body: { tag: "str", value: responseBody },
    });
  } catch (e: any) {
    throw runtimeError("E300", `http.${method.toLowerCase()} failed: ${e.message}`, { line: 0, col: 0 });
  }
}

const httpBuiltins: Record<string, BuiltinFn> = {
  "http.get"(args, loc) {
    return httpMethodRequest("GET", expectStr(args[0], loc), null);
  },
  "http.post"(args, loc) {
    return httpMethodRequest("POST", expectStr(args[0], loc), expectStr(args[1], loc));
  },
  "http.put"(args, loc) {
    return httpMethodRequest("PUT", expectStr(args[0], loc), expectStr(args[1], loc));
  },
  "http.del"(args, loc) {
    return httpMethodRequest("DELETE", expectStr(args[0], loc), null);
  },
  "http.patch"(args, loc) {
    return httpMethodRequest("PATCH", expectStr(args[0], loc), expectStr(args[1], loc));
  },
  "http.req"(args, loc) {
    const fields = expectRecord(args[0], loc);
    const method = (fields.get("method") as any)?.value ?? "GET";
    const url = (fields.get("url") as any)?.value ?? "";
    const body = fields.get("body");
    const bodyStr = body && body.tag === "variant" && body.name === "Some" ? (body.fields[0] as any).value : null;
    return httpMethodRequest(method, url, bodyStr);
  },
  "http.hdr"(args, loc) {
    const fields = expectRecord(args[0], loc);
    const key = expectStr(args[1], loc);
    const val = expectStr(args[2], loc);
    const newFields = new Map(fields);
    const headers = newFields.get("headers");
    const headerList: Value[] = headers && headers.tag === "list" ? [...headers.elements] : [];
    headerList.push({ tag: "tuple", elements: [{ tag: "str", value: key }, { tag: "str", value: val }] });
    newFields.set("headers", { tag: "list", elements: headerList });
    return { tag: "record", fields: newFields };
  },
  "http.json"(args, loc) {
    const fields = expectRecord(args[0], loc);
    const body = fields.get("body");
    if (!body || body.tag !== "str") throw runtimeError("E300", "http.json: response has no body", loc);
    try {
      const parsed = JSON.parse(body.value);
      return jsonToValue(parsed);
    } catch {
      throw runtimeError("E300", "http.json: invalid JSON in response body", loc);
    }
  },
  "http.ok?"(args, loc) {
    const fields = expectRecord(args[0], loc);
    const status = fields.get("status");
    if (!status || status.tag !== "int") throw runtimeError("E300", "http.ok?: response has no status", loc);
    return { tag: "bool", value: status.value >= 200 && status.value < 300 };
  },
};

// JSON ↔ Value conversion for http.json
function jsonToValue(v: any): Value {
  if (v === null) return { tag: "variant", name: "JNull", fields: [] };
  if (typeof v === "boolean") return { tag: "variant", name: "JBool", fields: [{ tag: "bool", value: v }] };
  if (typeof v === "number") return Number.isInteger(v)
    ? { tag: "variant", name: "JInt", fields: [{ tag: "int", value: v }] }
    : { tag: "variant", name: "JRat", fields: [{ tag: "rat", value: v }] };
  if (typeof v === "string") return { tag: "variant", name: "JStr", fields: [{ tag: "str", value: v }] };
  if (Array.isArray(v)) return { tag: "variant", name: "JArr", fields: [{ tag: "list", elements: v.map(jsonToValue) }] };
  if (typeof v === "object") {
    const pairs = Object.entries(v).map(([k, val]) => ({
      tag: "tuple" as const,
      elements: [{ tag: "str" as const, value: k }, jsonToValue(val)],
    }));
    return { tag: "variant", name: "JObj", fields: [{ tag: "list", elements: pairs }] };
  }
  return { tag: "unit" };
}

// ── Tier 2: HTTP Server (std.srv) ──

const srvBuiltins: Record<string, BuiltinFn> = {
  "srv.new"(_args, _loc) {
    return { tag: "list", elements: [] };
  },
  "srv.get"(args, loc) {
    const routes = expectList(args[0], loc);
    const path = expectStr(args[1], loc);
    const handler = args[2];
    return { tag: "list", elements: [...routes, mkRecord({
      method: { tag: "str", value: "GET" }, path: { tag: "str", value: path }, handler,
    })] };
  },
  "srv.post"(args, loc) {
    const routes = expectList(args[0], loc);
    const path = expectStr(args[1], loc);
    const handler = args[2];
    return { tag: "list", elements: [...routes, mkRecord({
      method: { tag: "str", value: "POST" }, path: { tag: "str", value: path }, handler,
    })] };
  },
  "srv.put"(args, loc) {
    const routes = expectList(args[0], loc);
    const path = expectStr(args[1], loc);
    const handler = args[2];
    return { tag: "list", elements: [...routes, mkRecord({
      method: { tag: "str", value: "PUT" }, path: { tag: "str", value: path }, handler,
    })] };
  },
  "srv.del"(args, loc) {
    const routes = expectList(args[0], loc);
    const path = expectStr(args[1], loc);
    const handler = args[2];
    return { tag: "list", elements: [...routes, mkRecord({
      method: { tag: "str", value: "DELETE" }, path: { tag: "str", value: path }, handler,
    })] };
  },
  "srv.start"(args, loc) {
    const _routes = expectList(args[0], loc);
    const _port = expectInt(args[1], loc);
    // Stub: return a server handle record
    return mkRecord({
      port: { tag: "int", value: _port },
      running: { tag: "bool", value: true },
    });
  },
  "srv.stop"(_args, _loc) {
    // Stub: no-op
    return { tag: "unit" };
  },
  "srv.res"(args, loc) {
    const status = expectInt(args[0], loc);
    const body = expectStr(args[1], loc);
    return mkRecord({
      status: { tag: "int", value: status },
      headers: { tag: "list", elements: [] },
      body: { tag: "str", value: body },
    });
  },
  "srv.json"(args, loc) {
    const status = expectInt(args[0], loc);
    const json = args[1];
    return mkRecord({
      status: { tag: "int", value: status },
      headers: { tag: "list", elements: [
        { tag: "tuple", elements: [{ tag: "str", value: "Content-Type" }, { tag: "str", value: "application/json" }] },
      ] },
      body: { tag: "str", value: showValue(json) },
    });
  },
  "srv.hdr"(args, loc) {
    const fields = expectRecord(args[0], loc);
    const key = expectStr(args[1], loc);
    const val = expectStr(args[2], loc);
    const newFields = new Map(fields);
    const headers = newFields.get("headers");
    const headerList: Value[] = headers && headers.tag === "list" ? [...headers.elements] : [];
    headerList.push({ tag: "tuple", elements: [{ tag: "str", value: key }, { tag: "str", value: val }] });
    newFields.set("headers", { tag: "list", elements: headerList });
    return { tag: "record", fields: newFields };
  },
  "srv.mw"(args, loc) {
    const routes = expectList(args[0], loc);
    // Stub: middleware is stored but not applied in this minimal impl
    return { tag: "list", elements: [...routes] };
  },
};

// ── Tier 2: CSV (std.csv) ──

function csvParse(input: string, delim = ","): Value[] {
  const rows: Value[] = [];
  const lines = input.split("\n");
  for (const line of lines) {
    if (line.trim() === "") continue;
    const cells: Value[] = [];
    let current = "";
    let inQuotes = false;
    for (let i = 0; i < line.length; i++) {
      const ch = line[i];
      if (inQuotes) {
        if (ch === '"' && line[i + 1] === '"') { current += '"'; i++; }
        else if (ch === '"') { inQuotes = false; }
        else { current += ch; }
      } else {
        if (ch === '"') { inQuotes = true; }
        else if (ch === delim) { cells.push({ tag: "str", value: current }); current = ""; }
        else { current += ch; }
      }
    }
    cells.push({ tag: "str", value: current });
    rows.push({ tag: "list", elements: cells });
  }
  return rows;
}

function csvEncode(rows: Value[]): string {
  return rows.map(row => {
    if (row.tag !== "list") return "";
    return row.elements.map(cell => {
      const s = cell.tag === "str" ? cell.value : showValue(cell);
      return s.includes(",") || s.includes('"') || s.includes("\n")
        ? `"${s.replace(/"/g, '""')}"` : s;
    }).join(",");
  }).join("\n");
}

const csvBuiltins: Record<string, BuiltinFn> = {
  "csv.dec"(args, loc) {
    const input = expectStr(args[0], loc);
    return { tag: "list", elements: csvParse(input) };
  },
  "csv.enc"(args, loc) {
    const rows = expectList(args[0], loc);
    return { tag: "str", value: csvEncode(rows) };
  },
  "csv.decf"(args, loc) {
    const path = expectStr(args[0], loc);
    try {
      const { readFileSync } = require("fs");
      const content: string = readFileSync(path, "utf-8");
      return { tag: "list", elements: csvParse(content) };
    } catch (e: any) {
      throw runtimeError("E300", `csv.decf: ${e.message}`, loc);
    }
  },
  "csv.encf"(args, loc) {
    const path = expectStr(args[0], loc);
    const rows = expectList(args[1], loc);
    try {
      const { writeFileSync } = require("fs");
      writeFileSync(path, csvEncode(rows), "utf-8");
      return { tag: "unit" };
    } catch (e: any) {
      throw runtimeError("E300", `csv.encf: ${e.message}`, loc);
    }
  },
  "csv.hdr"(args, loc) {
    const rows = expectList(args[0], loc);
    if (rows.length === 0) throw runtimeError("E300", "csv.hdr: empty data", loc);
    return rows[0];
  },
  "csv.rows"(args, loc) {
    const rows = expectList(args[0], loc);
    if (rows.length === 0) throw runtimeError("E300", "csv.rows: empty data", loc);
    return { tag: "list", elements: rows.slice(1) };
  },
  "csv.maps"(args, loc) {
    const rows = expectList(args[0], loc);
    if (rows.length === 0) throw runtimeError("E300", "csv.maps: empty data", loc);
    const header = rows[0];
    if (header.tag !== "list") throw runtimeError("E300", "csv.maps: header is not a list", loc);
    const keys = header.elements.map(h => h.tag === "str" ? h.value : showValue(h));
    const result: Value[] = [];
    for (let i = 1; i < rows.length; i++) {
      if (rows[i].tag !== "list") continue;
      const fields = new Map<string, Value>();
      for (let j = 0; j < keys.length; j++) {
        fields.set(keys[j], rows[i].elements[j] ?? { tag: "str", value: "" });
      }
      result.push({ tag: "record", fields });
    }
    return { tag: "list", elements: result };
  },
  "csv.opts"(args, loc) {
    const fields = expectRecord(args[0], loc);
    const input = expectStr(args[1], loc);
    const delim = (fields.get("delim") as any)?.value ?? ",";
    return { tag: "list", elements: csvParse(input, delim) };
  },
};

// ── Tier 2: Process (std.proc) ──

const procBuiltins: Record<string, BuiltinFn> = {
  "proc.run"(args, loc) {
    const cmd = expectStr(args[0], loc);
    const argList = expectList(args[1], loc).map(a => expectStr(a, loc));
    try {
      const { spawnSync } = require("child_process");
      const result = spawnSync(cmd, argList, { encoding: "utf-8", timeout: 30000 });
      return mkRecord({
        code: { tag: "int", value: result.status ?? -1 },
        out: { tag: "str", value: result.stdout ?? "" },
        err: { tag: "str", value: result.stderr ?? "" },
      });
    } catch (e: any) {
      throw runtimeError("E300", `proc.run: ${e.message}`, loc);
    }
  },
  "proc.sh"(args, loc) {
    const cmd = expectStr(args[0], loc);
    try {
      const { execSync } = require("child_process");
      const out: string = execSync(cmd, { encoding: "utf-8", timeout: 30000 });
      return mkRecord({
        code: { tag: "int", value: 0 },
        out: { tag: "str", value: out },
        err: { tag: "str", value: "" },
      });
    } catch (e: any) {
      return mkRecord({
        code: { tag: "int", value: e.status ?? 1 },
        out: { tag: "str", value: e.stdout ?? "" },
        err: { tag: "str", value: e.stderr ?? "" },
      });
    }
  },
  "proc.ok"(args, loc) {
    const cmd = expectStr(args[0], loc);
    const argList = expectList(args[1], loc).map(a => expectStr(a, loc));
    try {
      const { spawnSync } = require("child_process");
      const result = spawnSync(cmd, argList, { encoding: "utf-8", timeout: 30000 });
      if (result.status !== 0) {
        throw runtimeError("E300", `proc.ok: command exited with code ${result.status}: ${result.stderr}`, loc);
      }
      return { tag: "str", value: result.stdout ?? "" };
    } catch (e: any) {
      if (e.code) throw e; // re-throw runtime errors
      throw runtimeError("E300", `proc.ok: ${e.message}`, loc);
    }
  },
  "proc.pipe"(args, loc) {
    const cmd = expectStr(args[0], loc);
    const argList = expectList(args[1], loc).map(a => expectStr(a, loc));
    const stdin = expectStr(args[2], loc);
    try {
      const { spawnSync } = require("child_process");
      const result = spawnSync(cmd, argList, { encoding: "utf-8", input: stdin, timeout: 30000 });
      return mkRecord({
        code: { tag: "int", value: result.status ?? -1 },
        out: { tag: "str", value: result.stdout ?? "" },
        err: { tag: "str", value: result.stderr ?? "" },
      });
    } catch (e: any) {
      throw runtimeError("E300", `proc.pipe: ${e.message}`, loc);
    }
  },
  "proc.bg"(args, loc) {
    const cmd = expectStr(args[0], loc);
    const argList = expectList(args[1], loc).map(a => expectStr(a, loc));
    try {
      const { spawn } = require("child_process");
      const child = spawn(cmd, argList, { stdio: "pipe" });
      return mkRecord({
        pid: { tag: "int", value: child.pid ?? 0 },
        _handle: { tag: "str", value: `proc:${child.pid}` },
      });
    } catch (e: any) {
      throw runtimeError("E300", `proc.bg: ${e.message}`, loc);
    }
  },
  "proc.wait"(_args, _loc) {
    // Stub: background process wait not fully implemented
    return mkRecord({
      code: { tag: "int", value: 0 },
      out: { tag: "str", value: "" },
      err: { tag: "str", value: "" },
    });
  },
  "proc.kill"(_args, _loc) {
    // Stub
    return { tag: "unit" };
  },
  "proc.exit"(args, loc) {
    const code = expectInt(args[0], loc);
    if (typeof process !== "undefined") process.exit(code);
    return { tag: "unit" };
  },
  "proc.pid"(_args, _loc) {
    return { tag: "int", value: typeof process !== "undefined" ? process.pid : 0 };
  },
};

// ── Tier 2: DateTime (std.dt) ──

function dateToRecord(d: Date): Value {
  return mkRecord({
    year:  { tag: "int", value: d.getUTCFullYear() },
    month: { tag: "int", value: d.getUTCMonth() + 1 },
    day:   { tag: "int", value: d.getUTCDate() },
    hour:  { tag: "int", value: d.getUTCHours() },
    min:   { tag: "int", value: d.getUTCMinutes() },
    sec:   { tag: "int", value: d.getUTCSeconds() },
    tz:    { tag: "str", value: "UTC" },
  });
}

function recordToDate(v: Value, loc: Loc): Date {
  const fields = expectRecord(v, loc);
  const get = (k: string): number => {
    const f = fields.get(k);
    return f && f.tag === "int" ? f.value : 0;
  };
  return new Date(Date.UTC(get("year"), get("month") - 1, get("day"), get("hour"), get("min"), get("sec")));
}

const dtBuiltins: Record<string, BuiltinFn> = {
  "dt.now"(_args, _loc) {
    return dateToRecord(new Date());
  },
  "dt.unix"(_args, _loc) {
    return { tag: "int", value: Math.floor(Date.now() / 1000) };
  },
  "dt.from"(args, loc) {
    const ts = expectInt(args[0], loc);
    return dateToRecord(new Date(ts * 1000));
  },
  "dt.to"(args, loc) {
    const d = recordToDate(args[0], loc);
    return { tag: "int", value: Math.floor(d.getTime() / 1000) };
  },
  "dt.parse"(args, loc) {
    const value = expectStr(args[0], loc);
    // Simple: ignore format, use Date.parse
    const ms = Date.parse(value);
    if (isNaN(ms)) throw runtimeError("E300", `dt.parse: invalid date "${value}"`, loc);
    return dateToRecord(new Date(ms));
  },
  "dt.fmt"(args, loc) {
    const d = recordToDate(args[0], loc);
    const fmt = expectStr(args[1], loc);
    // Minimal format: replace YYYY, MM, DD, HH, mm, ss
    return { tag: "str", value: fmt
      .replace("YYYY", String(d.getUTCFullYear()))
      .replace("MM", String(d.getUTCMonth() + 1).padStart(2, "0"))
      .replace("DD", String(d.getUTCDate()).padStart(2, "0"))
      .replace("HH", String(d.getUTCHours()).padStart(2, "0"))
      .replace("mm", String(d.getUTCMinutes()).padStart(2, "0"))
      .replace("ss", String(d.getUTCSeconds()).padStart(2, "0"))
    };
  },
  "dt.add"(args, loc) {
    const d = recordToDate(args[0], loc);
    const ms = expectInt(args[1], loc);
    return dateToRecord(new Date(d.getTime() + ms));
  },
  "dt.sub"(args, loc) {
    const a = recordToDate(args[0], loc);
    const b = recordToDate(args[1], loc);
    return { tag: "int", value: a.getTime() - b.getTime() };
  },
  "dt.tz"(args, loc) {
    // Stub: just update tz field, no real conversion
    const fields = expectRecord(args[0], loc);
    const tz = expectStr(args[1], loc);
    const newFields = new Map(fields);
    newFields.set("tz", { tag: "str", value: tz });
    return { tag: "record", fields: newFields };
  },
  "dt.iso"(args, loc) {
    const d = recordToDate(args[0], loc);
    return { tag: "str", value: d.toISOString() };
  },
  "dt.ms"(args, loc) {
    return { tag: "int", value: expectInt(args[0], loc) };
  },
  "dt.sec"(args, loc) {
    return { tag: "int", value: expectInt(args[0], loc) * 1000 };
  },
  "dt.min"(args, loc) {
    return { tag: "int", value: expectInt(args[0], loc) * 60000 };
  },
  "dt.hr"(args, loc) {
    return { tag: "int", value: expectInt(args[0], loc) * 3600000 };
  },
  "dt.day"(args, loc) {
    return { tag: "int", value: expectInt(args[0], loc) * 86400000 };
  },
};

// Merge all tier-2 builtins into main exports
Object.assign(builtins, httpBuiltins, srvBuiltins, csvBuiltins, procBuiltins, dtBuiltins);

function valEqual(a: Value, b: Value): boolean {
  if (a.tag !== b.tag) return false;
  switch (a.tag) {
    case "int": case "rat": return a.value === (b as typeof a).value;
    case "bool": return a.value === (b as typeof a).value;
    case "str": return a.value === (b as typeof a).value;
    case "unit": return true;
    case "list": {
      const bList = b as typeof a;
      return a.elements.length === bList.elements.length &&
        a.elements.every((el, i) => valEqual(el, bList.elements[i]));
    }
    case "tuple": {
      const bTuple = b as typeof a;
      return a.elements.length === bTuple.elements.length &&
        a.elements.every((el, i) => valEqual(el, bTuple.elements[i]));
    }
    case "record": {
      const bRec = b as typeof a;
      if (a.fields.size !== bRec.fields.size) return false;
      for (const [k, v] of a.fields) {
        const bv = bRec.fields.get(k);
        if (!bv || !valEqual(v, bv)) return false;
      }
      return true;
    }
    case "variant": {
      const bVar = b as typeof a;
      return a.name === bVar.name &&
        a.fields.length === bVar.fields.length &&
        a.fields.every((f, i) => valEqual(f, bVar.fields[i]));
    }
    default: return false;
  }
}
