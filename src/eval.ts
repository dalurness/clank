// Clank tree-walking evaluator
// Evaluates desugared Core AST with persistent environment model
// Effect handlers implemented via replay-based delimited continuations

import { readFileSync, existsSync } from "node:fs";
import { resolve, join, dirname } from "node:path";
import type { Expr, HandlerArm, ImportItem, Loc, Param, Pattern, Program, TopLevel } from "./ast.js";
import { builtins, setApplyFn } from "./builtins.js";
import { lex } from "./lexer.js";
import { parse } from "./parser.js";
import { desugar } from "./desugar.js";

// ── Runtime values ──

export type Value =
  | { tag: "int"; value: number }
  | { tag: "rat"; value: number }
  | { tag: "bool"; value: boolean }
  | { tag: "str"; value: string }
  | { tag: "unit" }
  | { tag: "list"; elements: Value[] }
  | { tag: "tuple"; elements: Value[] }
  | { tag: "record"; fields: Map<string, Value> }
  | { tag: "variant"; name: string; fields: Value[] }
  | { tag: "closure"; params: Param[]; body: Expr; env: Env }
  | { tag: "builtin"; name: string; fn: (args: Value[], loc: Loc) => Value }
  | { tag: "effect-def"; name: string; ops: string[] };

// ── Runtime errors ──

export type RuntimeError = {
  code: string;
  message: string;
  location: Loc;
};

// ── Effect system: perform signal and handler stack ──

class PerformSignal {
  constructor(
    public op: string,
    public args: Value[],
    public performId: number,
  ) {}
}

type HandlerFrame = {
  arms: HandlerArm[];
  expr: Expr;
  env: Env;
  loc: Loc;
  resumeLog: Map<number, Value>;
};

// Global handler stack — used during evaluation
const handlerStack: HandlerFrame[] = [];

// Monotonic counter for identifying perform sites during replay
let performCounter = 0;

// ── Environment ──

export class Env {
  private bindings: Map<string, Value>;
  private parent: Env | null;

  constructor(parent: Env | null = null) {
    this.bindings = new Map();
    this.parent = parent;
  }

  get(name: string, loc: Loc): Value {
    const val = this.bindings.get(name);
    if (val !== undefined) return val;
    if (this.parent) return this.parent.get(name, loc);
    throw { code: "E202", message: `unbound variable '${name}'`, location: loc } as RuntimeError;
  }

  set(name: string, value: Value): void {
    this.bindings.set(name, value);
  }

  extend(): Env {
    return new Env(this);
  }
}

// ── Evaluator ──

function evaluate(expr: Expr, env: Env): Value {
  switch (expr.tag) {
    case "literal": {
      const lit = expr.value;
      switch (lit.tag) {
        case "int": return { tag: "int", value: lit.value };
        case "rat": return { tag: "rat", value: lit.value };
        case "bool": return { tag: "bool", value: lit.value };
        case "str": return { tag: "str", value: lit.value };
        case "unit": return { tag: "unit" };
      }
      break;
    }

    case "var":
      return env.get(expr.name, expr.loc);

    case "let": {
      const val = evaluate(expr.value, env);
      if (expr.body === null) return val;
      // _ is discard binding (used for sequencing)
      if (expr.name === "_") {
        return evaluate(expr.body, env);
      }
      const child = env.extend();
      child.set(expr.name, val);
      return evaluate(expr.body, child);
    }

    case "if": {
      const cond = evaluate(expr.cond, env);
      if (cond.tag !== "bool") {
        throw { code: "E200", message: `if condition must be Bool, got ${cond.tag}`, location: expr.loc } as RuntimeError;
      }
      return cond.value ? evaluate(expr.then, env) : evaluate(expr.else, env);
    }

    case "lambda":
      return { tag: "closure", params: expr.params, body: expr.body, env };

    case "apply": {
      const fn = evaluate(expr.fn, env);
      const args = expr.args.map(a => evaluate(a, env));

      if (fn.tag === "builtin") {
        return fn.fn(args, expr.loc);
      }

      if (fn.tag === "closure") {
        if (args.length !== fn.params.length) {
          throw {
            code: "E203",
            message: `expected ${fn.params.length} arguments, got ${args.length}`,
            location: expr.loc,
          } as RuntimeError;
        }
        const callEnv = fn.env.extend();
        for (let i = 0; i < fn.params.length; i++) {
          callEnv.set(fn.params[i].name, args[i]);
        }
        return evaluate(fn.body, callEnv);
      }

      throw { code: "E204", message: `cannot call ${fn.tag} as a function`, location: expr.loc } as RuntimeError;
    }

    // These should have been desugared away, but handle gracefully
    case "pipeline":
    case "infix":
    case "unary":
    case "do":
    case "for":
    case "range":
      throw { code: "E205", message: `'${expr.tag}' should have been desugared`, location: expr.loc } as RuntimeError;

    case "list":
      return { tag: "list", elements: expr.elements.map(e => evaluate(e, env)) };

    case "tuple":
      return { tag: "tuple", elements: expr.elements.map(e => evaluate(e, env)) };

    case "match": {
      const subject = evaluate(expr.subject, env);
      for (const arm of expr.arms) {
        const bindings = matchPattern(arm.pattern, subject);
        if (bindings !== null) {
          const armEnv = env.extend();
          for (const [name, val] of bindings) {
            armEnv.set(name, val);
          }
          return evaluate(arm.body, armEnv);
        }
      }
      throw { code: "E208", message: `no matching pattern for ${showValueBrief(subject)}`, location: expr.loc } as RuntimeError;
    }

    case "perform":
      // perform just evaluates the inner expression — the effect op builtin
      // will throw PerformSignal when called
      return evaluate(expr.expr, env);

    case "handle":
      return evaluateHandle(expr.expr, expr.arms, env, expr.loc, new Map());

    case "record": {
      const fields = new Map<string, Value>();
      for (const f of expr.fields) {
        fields.set(f.name, evaluate(f.value, env));
      }
      return { tag: "record", fields };
    }

    case "record-update": {
      const base = evaluate(expr.base, env);
      if (base.tag !== "record") {
        throw { code: "E206", message: `cannot update non-record value (got ${base.tag})`, location: expr.loc } as RuntimeError;
      }
      const fields = new Map(base.fields);
      for (const f of expr.fields) {
        if (!fields.has(f.name)) {
          throw { code: "E206", message: `record has no field '${f.name}'`, location: expr.loc } as RuntimeError;
        }
        fields.set(f.name, evaluate(f.value, env));
      }
      return { tag: "record", fields };
    }

    case "field-access": {
      // Dotted builtin name fallback (e.g. str.cat, tuple.get):
      // try the dotted name first before evaluating as record field access
      if (expr.object.tag === "var") {
        const dottedName = `${expr.object.name}.${expr.field}`;
        try {
          return env.get(dottedName, expr.loc);
        } catch {
          // not a dotted builtin — fall through to record field access
        }
      }
      const obj = evaluate(expr.object, env);
      if (obj.tag === "record") {
        const val = obj.fields.get(expr.field);
        if (val === undefined) {
          throw { code: "E206", message: `record has no field '${expr.field}'`, location: expr.loc } as RuntimeError;
        }
        return val;
      }
      throw { code: "E206", message: `cannot access field '${expr.field}' on ${obj.tag}`, location: expr.loc } as RuntimeError;
    }

    default: {
      const _: never = expr;
      throw { code: "E299", message: `unknown AST node`, location: { line: 0, col: 0 } } as RuntimeError;
    }
  }

  // Unreachable but satisfies TypeScript
  throw { code: "E299", message: "unreachable", location: { line: 0, col: 0 } } as RuntimeError;
}

// ── Handle expression evaluation ──
// Uses replay-based delimited continuations for one-shot resume.
// Deep handler semantics: the continuation re-installs the same handler.

function evaluateHandle(
  expr: Expr,
  arms: HandlerArm[],
  env: Env,
  loc: Loc,
  resumeLog: Map<number, Value>,
): Value {
  const frame: HandlerFrame = { arms, expr, env, loc, resumeLog };
  handlerStack.push(frame);

  const savedCounter = performCounter;
  performCounter = 0;

  try {
    const result = evaluate(expr, env);
    handlerStack.pop();
    performCounter = savedCounter;

    // Apply return arm if present
    const returnArm = arms.find(a => a.name === "return");
    if (returnArm) {
      const armEnv = env.extend();
      armEnv.set(returnArm.params[0].name, result);
      return evaluate(returnArm.body, armEnv);
    }
    return result;
  } catch (signal) {
    handlerStack.pop();
    performCounter = savedCounter;

    if (signal instanceof PerformSignal) {
      const arm = arms.find(a => a.name === signal.op);
      if (!arm) throw signal; // propagate to outer handler

      // Build one-shot continuation
      const capturedResumeLog = new Map(resumeLog);
      const capturedPerformId = signal.performId;
      let used = false;

      const continuation: Value = {
        tag: "builtin",
        name: `<resume:${signal.op}>`,
        fn: (kArgs: Value[], kLoc: Loc): Value => {
          if (used) {
            throw {
              code: "E211",
              message: "continuation already resumed (one-shot)",
              location: kLoc,
            } as RuntimeError;
          }
          used = true;

          // Replay: re-evaluate the handled expression with the resume value
          // logged for this specific perform site. Deep handler: same handler
          // is re-installed during replay.
          const newLog = new Map(capturedResumeLog);
          newLog.set(capturedPerformId, kArgs[0] ?? { tag: "unit" });
          return evaluateHandle(expr, arms, env, loc, newLog);
        },
      };

      // Call handler arm body with op args and continuation
      const armEnv = env.extend();
      for (let i = 0; i < arm.params.length; i++) {
        armEnv.set(arm.params[i].name, signal.args[i] ?? { tag: "unit" });
      }
      if (arm.resumeName) {
        armEnv.set(arm.resumeName, continuation);
      }
      return evaluate(arm.body, armEnv);
    }

    throw signal; // not a PerformSignal, re-throw
  }
}

// ── Apply a callable value (used by higher-order builtins) ──

function applyValue(fn: Value, args: Value[], loc: Loc): Value {
  if (fn.tag === "builtin") return fn.fn(args, loc);
  if (fn.tag === "closure") {
    const callEnv = fn.env.extend();
    for (let i = 0; i < fn.params.length; i++) {
      callEnv.set(fn.params[i].name, args[i]);
    }
    return evaluate(fn.body, callEnv);
  }
  throw { code: "E204", message: `cannot call ${fn.tag} as a function`, location: loc } as RuntimeError;
}

// ── Helpers ──

function expectNum(v: Value, loc: Loc): number {
  if (v.tag === "int" || v.tag === "rat") return v.value;
  throw { code: "E200", message: `expected number, got ${v.tag}`, location: loc } as RuntimeError;
}

// ── Effect operation performer ──
// Creates a builtin function for an effect operation that either:
// - Returns a logged resume value (during replay), or
// - Throws a PerformSignal (first execution)

function makeEffectOp(opName: string, effectName: string): Value {
  return {
    tag: "builtin",
    name: opName,
    fn: (args: Value[], loc: Loc): Value => {
      const id = performCounter++;

      // Check if we're replaying and this perform has a logged resume value
      for (let i = handlerStack.length - 1; i >= 0; i--) {
        const frame = handlerStack[i];
        if (frame.resumeLog.has(id)) {
          return frame.resumeLog.get(id)!;
        }
        // Stop searching if this handler handles the operation
        if (frame.arms.some(a => a.name === opName)) break;
      }

      // Not replaying — perform the effect
      throw new PerformSignal(opName, args, id);
    },
  };
}

// ── Pattern matching ──

function matchPattern(pattern: Pattern, value: Value): Map<string, Value> | null {
  switch (pattern.tag) {
    case "p-wildcard":
      return new Map();

    case "p-var":
      return new Map([[pattern.name, value]]);

    case "p-literal": {
      const lit = pattern.value;
      switch (lit.tag) {
        case "int": if (value.tag === "int" && value.value === lit.value) return new Map(); break;
        case "rat": if (value.tag === "rat" && value.value === lit.value) return new Map(); break;
        case "bool": if (value.tag === "bool" && value.value === lit.value) return new Map(); break;
        case "str": if (value.tag === "str" && value.value === lit.value) return new Map(); break;
        case "unit": if (value.tag === "unit") return new Map(); break;
      }
      return null;
    }

    case "p-variant": {
      if (value.tag !== "variant" || value.name !== pattern.name) return null;
      if (value.fields.length !== pattern.args.length) return null;
      const bindings = new Map<string, Value>();
      for (let i = 0; i < pattern.args.length; i++) {
        const sub = matchPattern(pattern.args[i], value.fields[i]);
        if (sub === null) return null;
        for (const [k, v] of sub) bindings.set(k, v);
      }
      return bindings;
    }

    case "p-tuple": {
      if (value.tag !== "tuple") return null;
      if (value.elements.length !== pattern.elements.length) return null;
      const bindings = new Map<string, Value>();
      for (let i = 0; i < pattern.elements.length; i++) {
        const sub = matchPattern(pattern.elements[i], value.elements[i]);
        if (sub === null) return null;
        for (const [k, v] of sub) bindings.set(k, v);
      }
      return bindings;
    }
  }
}

function showValueBrief(v: Value): string {
  switch (v.tag) {
    case "int": return String(v.value);
    case "rat": return String(v.value);
    case "bool": return v.value ? "true" : "false";
    case "str": return `"${v.value}"`;
    case "unit": return "()";
    case "list": return `[...${v.elements.length} elements]`;
    case "tuple": return `(${v.elements.map(showValueBrief).join(", ")})`;
    case "record": return `{${[...v.fields.entries()].map(([k, val]) => `${k}: ${showValueBrief(val)}`).join(", ")}}`;
    case "variant": return v.fields.length > 0 ? `${v.name}(...)` : v.name;
    case "closure": return "<fn>";
    case "builtin": return `<builtin:${v.name}>`;
    case "effect-def": return `<effect:${v.name}>`;
  }
}

// ── Module system ──

// Cache: absolute file path → map of exported (pub) names
const moduleCache = new Map<string, Map<string, Value>>();

/**
 * Resolve a module path (e.g. ["math", "stats"]) to an absolute file path,
 * relative to the given base directory.
 * Tries: baseDir/math/stats.clk, then baseDir/math.stats.clk (flat).
 */
function resolveModulePath(modPath: string[], baseDir: string): string {
  // Primary: directory-based — math/stats.clk
  const dirBased = join(baseDir, ...modPath.slice(0, -1), modPath[modPath.length - 1] + ".clk");
  if (existsSync(dirBased)) return resolve(dirBased);

  // Fallback: flat file — math.stats.clk
  const flatBased = join(baseDir, modPath.join(".") + ".clk");
  if (existsSync(flatBased)) return resolve(flatBased);

  // Not found
  throw {
    code: "E220",
    message: `module '${modPath.join(".")}' not found (tried ${dirBased} and ${flatBased})`,
    location: { line: 0, col: 0 },
  } as RuntimeError;
}

/**
 * Load a module: read, lex, parse, desugar, evaluate.
 * Returns a map of pub-exported names → values.
 */
function loadModule(absPath: string, baseDir: string): Map<string, Value> {
  // Check cache
  if (moduleCache.has(absPath)) return moduleCache.get(absPath)!;

  // Prevent infinite recursion by inserting empty cache entry
  moduleCache.set(absPath, new Map());

  const source = readFileSync(absPath, "utf-8");
  const tokens = lex(source);
  if (!Array.isArray(tokens)) {
    throw { code: "E221", message: `lex error in module '${absPath}': ${tokens.message}`, location: tokens.location } as RuntimeError;
  }

  const ast = parse(tokens);
  if ("code" in ast) {
    throw { code: "E222", message: `parse error in module '${absPath}': ${(ast as any).message}`, location: (ast as any).location } as RuntimeError;
  }

  const program: Program = {
    topLevels: (ast as Program).topLevels.map(tl => {
      if (tl.tag === "definition") {
        return { ...tl, body: desugar(tl.body) };
      }
      return tl;
    }),
  };

  const modDir = dirname(absPath);

  // Create module environment with builtins
  const modEnv = new Env();
  for (const [name, fn] of Object.entries(builtins)) {
    modEnv.set(name, { tag: "builtin", name, fn });
  }
  modEnv.set("exn", { tag: "effect-def", name: "exn", ops: ["raise"] });
  modEnv.set("raise", makeEffectOp("raise", "exn"));
  modEnv.set("io", { tag: "effect-def", name: "io", ops: ["print", "read-ln"] });

  // Track which names are public
  const pubNames = new Set<string>();

  // Register all top-level forms
  for (const tl of program.topLevels) {
    if (tl.tag === "mod-decl") continue; // informational only

    if (tl.tag === "use-decl") {
      // Load imported module
      const importPath = resolveModulePath(tl.path, modDir);
      const exports = loadModule(importPath, modDir);
      for (const imp of tl.imports) {
        const name = imp.name;
        const localName = imp.alias ?? name;
        const val = exports.get(name);
        if (!val) {
          throw {
            code: "E223",
            message: `'${name}' is not exported by module '${tl.path.join(".")}'`,
            location: tl.loc,
          } as RuntimeError;
        }
        modEnv.set(localName, val);
      }
      continue;
    }

    if (tl.tag === "type-decl") {
      for (const variant of tl.variants) {
        const val = makeVariantConstructor(variant.name, variant.fields.length);
        modEnv.set(variant.name, val);
        if (tl.pub) pubNames.add(variant.name);
      }
      continue;
    }

    if (tl.tag === "effect-alias") {
      // Store alias as an effect-def so it can be exported/imported
      modEnv.set(tl.name, { tag: "effect-def", name: tl.name, ops: tl.effects });
      if (tl.pub) pubNames.add(tl.name);
      continue;
    }

    if (tl.tag === "effect-decl") {
      const opNames = tl.ops.map(op => op.name);
      modEnv.set(tl.name, { tag: "effect-def", name: tl.name, ops: opNames });
      if (tl.pub) pubNames.add(tl.name);
      for (const op of tl.ops) {
        modEnv.set(op.name, makeEffectOp(op.name, tl.name));
        if (tl.pub) pubNames.add(op.name);
      }
      continue;
    }

    if (tl.tag === "definition") {
      const params: Param[] = tl.sig.params.map(p => ({ name: p.name, type: null }));
      const closure: Value = { tag: "closure", params, body: tl.body, env: modEnv };
      modEnv.set(tl.name, closure);
      if (tl.pub) pubNames.add(tl.name);
    }
  }

  // Build export map
  const exports = new Map<string, Value>();
  for (const name of pubNames) {
    try {
      exports.set(name, modEnv.get(name, { line: 0, col: 0 }));
    } catch {
      // skip if not found (shouldn't happen)
    }
  }

  moduleCache.set(absPath, exports);
  return exports;
}

/** Create a variant constructor value */
function makeVariantConstructor(vname: string, arity: number): Value {
  if (arity === 0) {
    return { tag: "variant", name: vname, fields: [] };
  }
  return {
    tag: "builtin",
    name: vname,
    fn: (args: Value[], loc: Loc): Value => {
      if (args.length !== arity) {
        throw { code: "E203", message: `${vname} expects ${arity} arguments, got ${args.length}`, location: loc } as RuntimeError;
      }
      return { tag: "variant", name: vname, fields: args };
    },
  };
}

// ── Environment setup (shared by run and evalExpr) ──

function initGlobalEnv(): Env {
  setApplyFn(applyValue);
  handlerStack.length = 0;
  performCounter = 0;
  moduleCache.clear();

  const global = new Env();
  for (const [name, fn] of Object.entries(builtins)) {
    global.set(name, { tag: "builtin", name, fn });
  }

  // Built-in effects
  global.set("exn", { tag: "effect-def", name: "exn", ops: ["raise"] });
  global.set("raise", makeEffectOp("raise", "exn"));
  global.set("io", { tag: "effect-def", name: "io", ops: ["print", "read-ln"] });

  // Override div to raise on division by zero (via effect system)
  global.set("div", {
    tag: "builtin",
    name: "div",
    fn: (args: Value[], loc: Loc): Value => {
      const a = expectNum(args[0], loc);
      const b = expectNum(args[1], loc);
      if (b === 0) {
        const id = performCounter++;
        for (let i = handlerStack.length - 1; i >= 0; i--) {
          const frame = handlerStack[i];
          if (frame.resumeLog.has(id)) return frame.resumeLog.get(id)!;
          if (frame.arms.some(arm => arm.name === "raise")) break;
        }
        throw new PerformSignal("raise", [{ tag: "str", value: "division by zero" }], id);
      }
      if (args[0].tag === "rat" || args[1].tag === "rat") {
        return { tag: "rat", value: a / b };
      }
      return { tag: "int", value: Math.trunc(a / b) };
    },
  });

  // Override mod to raise on modulo by zero
  global.set("mod", {
    tag: "builtin",
    name: "mod",
    fn: (args: Value[], loc: Loc): Value => {
      const a = expectNum(args[0], loc);
      const b = expectNum(args[1], loc);
      if (b === 0) {
        const id = performCounter++;
        for (let i = handlerStack.length - 1; i >= 0; i--) {
          const frame = handlerStack[i];
          if (frame.resumeLog.has(id)) return frame.resumeLog.get(id)!;
          if (frame.arms.some(arm => arm.name === "raise")) break;
        }
        throw new PerformSignal("raise", [{ tag: "str", value: "modulo by zero" }], id);
      }
      return { tag: "int", value: a % b };
    },
  });

  return global;
}

function loadTopLevels(env: Env, program: Program, baseDir: string): void {
  for (const tl of program.topLevels) {
    if (tl.tag === "mod-decl") continue;

    if (tl.tag === "use-decl") {
      const importPath = resolveModulePath(tl.path, baseDir);
      const exports = loadModule(importPath, baseDir);
      for (const imp of tl.imports) {
        const name = imp.name;
        const localName = imp.alias ?? name;
        const val = exports.get(name);
        if (!val) {
          throw {
            code: "E223",
            message: `'${name}' is not exported by module '${tl.path.join(".")}'`,
            location: tl.loc,
          } as RuntimeError;
        }
        env.set(localName, val);
      }
      continue;
    }

    if (tl.tag === "type-decl") {
      for (const variant of tl.variants) {
        env.set(variant.name, makeVariantConstructor(variant.name, variant.fields.length));
      }
      continue;
    }

    if (tl.tag === "effect-alias") {
      // Store alias as an effect-def so it's available in the environment
      env.set(tl.name, { tag: "effect-def", name: tl.name, ops: tl.effects });
      continue;
    }

    if (tl.tag === "effect-decl") {
      const opNames = tl.ops.map(op => op.name);
      env.set(tl.name, { tag: "effect-def", name: tl.name, ops: opNames });
      for (const op of tl.ops) {
        env.set(op.name, makeEffectOp(op.name, tl.name));
      }
      continue;
    }

    if (tl.tag === "definition") {
      const params: Param[] = tl.sig.params.map(p => ({
        name: p.name,
        type: null,
      }));
      const closure: Value = {
        tag: "closure",
        params,
        body: tl.body,
        env,
      };
      env.set(tl.name, closure);
    }
  }
}

// ── Program runner ──

export function run(program: Program, baseDir?: string): Value | RuntimeError {
  try {
    const global = initGlobalEnv();
    const resolveDir = baseDir ?? ".";
    loadTopLevels(global, program, resolveDir);

    // Find and call main()
    const main = global.get("main", { line: 0, col: 0 });
    if (main.tag !== "closure" && main.tag !== "builtin") {
      return { code: "E207", message: "'main' is not a function", location: { line: 0, col: 0 } };
    }

    return evaluate({ tag: "apply", fn: { tag: "var", name: "main", loc: { line: 0, col: 0 } }, args: [], loc: { line: 0, col: 0 } }, global);
  } catch (e: unknown) {
    if (e instanceof PerformSignal) {
      return {
        code: "E210",
        message: `unhandled effect operation '${e.op}'`,
        location: { line: 0, col: 0 },
      };
    }
    if (e && typeof e === "object" && "code" in e) return e as RuntimeError;
    throw e;
  }
}

// ── Expression evaluator (for eval command) ──

export type EvalResult =
  | { ok: true; value: Value }
  | { ok: false; error: RuntimeError };

/** Serialize a Value to a JSON-safe representation */
export function valueToJSON(v: Value): unknown {
  switch (v.tag) {
    case "int": return v.value;
    case "rat": return v.value;
    case "bool": return v.value;
    case "str": return v.value;
    case "unit": return null;
    case "list": return v.elements.map(valueToJSON);
    case "tuple": return v.elements.map(valueToJSON);
    case "record": {
      const obj: Record<string, unknown> = {};
      for (const [k, val] of v.fields) obj[k] = valueToJSON(val);
      return obj;
    }
    case "variant":
      return v.fields.length > 0
        ? { [v.name]: v.fields.map(valueToJSON) }
        : v.name;
    case "closure": return "<fn>";
    case "builtin": return `<builtin:${v.name}>`;
    case "effect-def": return `<effect:${v.name}>`;
  }
}

/** Get the type name of a runtime value */
export function valueTypeName(v: Value): string {
  switch (v.tag) {
    case "int": return "Int";
    case "rat": return "Rat";
    case "bool": return "Bool";
    case "str": return "Str";
    case "unit": return "Unit";
    case "list": return "List";
    case "tuple": return "Tuple";
    case "record": return "Record";
    case "variant": return v.name;
    case "closure": return "Fn";
    case "builtin": return "Fn";
    case "effect-def": return "Effect";
  }
}

/**
 * Evaluate a standalone expression, optionally with definitions from a loaded program.
 * Returns a structured result with the value and its type.
 */
export function evalExpr(
  expr: Expr,
  program?: Program,
  baseDir?: string,
): EvalResult {
  try {
    const global = initGlobalEnv();
    if (program) {
      loadTopLevels(global, program, baseDir ?? ".");
    }
    const result = evaluate(expr, global);
    return { ok: true, value: result };
  } catch (e: unknown) {
    if (e instanceof PerformSignal) {
      return {
        ok: false,
        error: {
          code: "E210",
          message: `unhandled effect operation '${e.op}'`,
          location: { line: 0, col: 0 },
        },
      };
    }
    if (e && typeof e === "object" && "code" in e) {
      return { ok: false, error: e as RuntimeError };
    }
    throw e;
  }
}

/**
 * Evaluate a standalone expression with a pre-built environment.
 * Used by session mode to maintain state across invocations.
 */
export function evalExprWithEnv(
  expr: Expr,
  env: Env,
): EvalResult {
  try {
    setApplyFn(applyValue);
    const result = evaluate(expr, env);
    return { ok: true, value: result };
  } catch (e: unknown) {
    if (e instanceof PerformSignal) {
      return {
        ok: false,
        error: {
          code: "E210",
          message: `unhandled effect operation '${e.op}'`,
          location: { line: 0, col: 0 },
        },
      };
    }
    if (e && typeof e === "object" && "code" in e) {
      return { ok: false, error: e as RuntimeError };
    }
    throw e;
  }
}

/**
 * Create a fresh global environment, optionally loading a program's definitions.
 * Used by session mode to build a reusable environment.
 */
export function createEnv(program?: Program, baseDir?: string): Env {
  const global = initGlobalEnv();
  if (program) {
    loadTopLevels(global, program, baseDir ?? ".");
  }
  return global;
}
