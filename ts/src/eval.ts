// Clank tree-walking evaluator
// Evaluates desugared Core AST with persistent environment model
// Effect handlers implemented via replay-based delimited continuations

import { readFileSync, existsSync } from "node:fs";
import { resolve, join, dirname } from "node:path";
import { createRequire } from "node:module";
import type { Expr, HandlerArm, ImportItem, Loc, MethodSig, Param, Pattern, Program, TopLevel, TypeExpr } from "./ast.js";
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
  | { tag: "effect-def"; name: string; ops: string[] }
  | { tag: "future"; task: AsyncTask }
  | { tag: "sender"; channel: EvalChannel }
  | { tag: "receiver"; channel: EvalChannel };

// ── Channel type (tree-walker) ──

type EvalChannel = {
  id: number;
  buffer: Value[];
  capacity: number;
  senderOpen: boolean;
  receiverOpen: boolean;
};

// ── Async task types (tree-walker) ──

type AsyncTaskStatus = "pending" | "completed" | "failed" | "cancelled";

type AsyncTask = {
  id: number;
  status: AsyncTaskStatus;
  result?: Value;
  error?: string;
  body: () => Value;
  cancelFlag: boolean;
  shieldDepth: number;
  groupId: number | null;
};

type AsyncTaskGroup = {
  id: number;
  children: AsyncTask[];
};

let nextAsyncTaskId = 1;
let nextAsyncGroupId = 1;
let activeGroupStack: AsyncTaskGroup[] = [];
let currentTask: AsyncTask | null = null;

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

// ── Interface/impl dispatch table ──
// Key: "methodName" → Map<runtimeTag, Value (closure/builtin)>

type ImplTable = Map<string, Map<string, Value>>;
const implTable: ImplTable = new Map();

function registerImpl(methodName: string, typeTag: string, impl: Value): void {
  if (!implTable.has(methodName)) implTable.set(methodName, new Map());
  implTable.get(methodName)!.set(typeTag, impl);
}

function runtimeTypeTag(v: Value): string {
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
    case "future": return "Future";
    case "sender": return "Sender";
    case "receiver": return "Receiver";
  }
}

function typeExprToTag(te: TypeExpr): string {
  switch (te.tag) {
    case "t-name": return te.name;
    case "t-list": return "List";
    case "t-tuple": return "Tuple";
    case "t-record": return "Record";
    case "t-generic": return te.name;
    default: return "?";
  }
}

// Interface method names → interface name (for tracking)
const interfaceMethodNames: Map<string, string> = new Map();

function makeInterfaceDispatcher(methodName: string, paramCount: number): Value {
  return {
    tag: "builtin",
    name: methodName,
    fn: (args: Value[], loc: Loc): Value => {
      // Dispatch on the runtime type of the first argument
      const dispatchArg = args[0];
      if (!dispatchArg) {
        throw { code: "E212", message: `interface method '${methodName}' called with no arguments`, location: loc } as RuntimeError;
      }

      // For variant types, check both the variant name and the type name
      const tag = runtimeTypeTag(dispatchArg);
      const methodImpls = implTable.get(methodName);
      if (methodImpls) {
        const impl = methodImpls.get(tag);
        if (impl) {
          if (impl.tag === "builtin") return impl.fn(args, loc);
          if (impl.tag === "closure") {
            const callEnv = impl.env.extend();
            for (let i = 0; i < impl.params.length; i++) {
              callEnv.set(impl.params[i].name, args[i] ?? { tag: "unit" });
            }
            return evaluate(impl.body, callEnv);
          }
        }
      }

      throw { code: "E212", message: `no impl of method '${methodName}' for type '${tag}'`, location: loc } as RuntimeError;
    },
  };
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
    case "let-pattern":
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
      // If there's a spread, start with base record fields
      if (expr.spread) {
        const base = evaluate(expr.spread, env);
        if (base.tag !== "record") {
          throw { code: "E206", message: `spread requires a record value (got ${base.tag})`, location: expr.loc } as RuntimeError;
        }
        for (const [k, v] of base.fields) {
          fields.set(k, v);
        }
      }
      // Explicit fields override spread fields
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

    // Affine nodes — at runtime, borrow/clone just pass through the value
    case "borrow":
      return evaluate(expr.expr, env);
    case "clone":
      return evaluate(expr.expr, env);
    case "discard":
      evaluate(expr.expr, env);
      return { tag: "unit" } as Value;

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

// ── Async task execution ──

function runAsyncTask(task: AsyncTask, loc: Loc): void {
  const savedTask = currentTask;
  currentTask = task;
  task.status = "pending"; // still pending until we execute
  try {
    if (task.cancelFlag && task.shieldDepth === 0) {
      task.status = "cancelled";
    } else {
      const result = task.body();
      task.result = result;
      task.status = "completed";
    }
  } catch (e: any) {
    if (e && e.code === "E011") {
      task.status = "cancelled";
    } else {
      task.status = "failed";
      task.error = e?.message ?? String(e);
    }
  }
  currentTask = savedTask;
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

    case "p-record": {
      if (value.tag !== "record") return null;
      const bindings = new Map<string, Value>();
      const matchedNames = new Set<string>();
      for (const pf of pattern.fields) {
        const fieldVal = value.fields.get(pf.name);
        if (fieldVal === undefined) return null;
        matchedNames.add(pf.name);
        if (pf.pattern) {
          const sub = matchPattern(pf.pattern, fieldVal);
          if (sub === null) return null;
          for (const [k, v] of sub) bindings.set(k, v);
        } else {
          bindings.set(pf.name, fieldVal);
        }
      }
      if (pattern.rest === null) {
        if (value.fields.size !== pattern.fields.length) return null;
      } else if (pattern.rest !== "_") {
        const restFields = new Map<string, Value>();
        for (const [k, v] of value.fields) {
          if (!matchedNames.has(k)) restFields.set(k, v);
        }
        bindings.set(pattern.rest, { tag: "record", fields: restFields });
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
    case "future": return `<future:${v.task.id}>`;
    case "sender": return `<sender:${v.channel.id}>`;
    case "receiver": return `<receiver:${v.channel.id}>`;
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
function resolveModulePath(modPath: string[], baseDir: string, packageModuleMap?: Map<string, string>): string {
  // Check package module map first (for cross-package imports)
  if (packageModuleMap) {
    const qualifiedName = modPath.join(".");
    const pkgPath = packageModuleMap.get(qualifiedName);
    if (pkgPath) return resolve(pkgPath);
  }

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
function loadModule(absPath: string, baseDir: string, packageModuleMap?: Map<string, string>): Map<string, Value> {
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
      if (tl.tag === "impl-block") {
        return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
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
      const importPath = resolveModulePath(tl.path, modDir, packageModuleMap);
      const exports = loadModule(importPath, modDir, packageModuleMap);
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
      // Generate impls for deriving clauses
      if (tl.deriving && tl.deriving.length > 0) {
        registerDerivedImpls(tl.variants, tl.deriving, modEnv);
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

    if (tl.tag === "interface-decl") {
      for (const m of tl.methods) {
        interfaceMethodNames.set(m.name, tl.name);
        const dispatcher = makeInterfaceDispatcher(m.name, m.sig.params.length);
        modEnv.set(m.name, dispatcher);
        if (tl.pub) pubNames.add(m.name);
      }
      continue;
    }

    if (tl.tag === "impl-block") {
      const typeTag = typeExprToTag(tl.forType);
      for (const m of tl.methods) {
        const bodyValue = evaluate(m.body, modEnv);
        registerImpl(m.name, typeTag, bodyValue);
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
  implTable.clear();
  interfaceMethodNames.clear();
  registerBuiltinImpls();

  // Reset async state
  nextAsyncTaskId = 1;
  nextAsyncGroupId = 1;
  activeGroupStack = [];
  currentTask = null;

  const global = new Env();
  for (const [name, fn] of Object.entries(builtins)) {
    global.set(name, { tag: "builtin", name, fn });
  }

  // Built-in Ordering type for Ord interface
  global.set("Lt", { tag: "variant", name: "Lt", fields: [] });
  global.set("Gt", { tag: "variant", name: "Gt", fields: [] });
  global.set("Eq_", { tag: "variant", name: "Eq_", fields: [] });

  // Built-in interface method dispatchers
  // Override show/eq with dispatchers that check the impl table first, then fall back to builtins
  const builtinShow = global.get("show", { line: 0, col: 0 });
  global.set("show", {
    tag: "builtin",
    name: "show",
    fn: (args: Value[], loc: Loc): Value => {
      const tag = runtimeTypeTag(args[0]);
      const methodImpls = implTable.get("show");
      if (methodImpls) {
        const impl = methodImpls.get(tag);
        if (impl) {
          if (impl.tag === "builtin") return impl.fn(args, loc);
          if (impl.tag === "closure") {
            const callEnv = impl.env.extend();
            for (let i = 0; i < impl.params.length; i++) callEnv.set(impl.params[i].name, args[i] ?? { tag: "unit" });
            return evaluate(impl.body, callEnv);
          }
        }
      }
      return builtinShow.tag === "builtin" ? builtinShow.fn(args, loc) : { tag: "str", value: "?" };
    },
  });
  const builtinEq = global.get("eq", { line: 0, col: 0 });
  global.set("eq", {
    tag: "builtin",
    name: "eq",
    fn: (args: Value[], loc: Loc): Value => {
      const tag = runtimeTypeTag(args[0]);
      const methodImpls = implTable.get("eq");
      if (methodImpls) {
        const impl = methodImpls.get(tag);
        if (impl) {
          if (impl.tag === "builtin") return impl.fn(args, loc);
          if (impl.tag === "closure") {
            const callEnv = impl.env.extend();
            for (let i = 0; i < impl.params.length; i++) callEnv.set(impl.params[i].name, args[i] ?? { tag: "unit" });
            return evaluate(impl.body, callEnv);
          }
        }
      }
      return builtinEq.tag === "builtin" ? builtinEq.fn(args, loc) : { tag: "bool", value: false };
    },
  });
  // Register cmp, clone, default, into, from as dispatchers
  global.set("cmp", makeInterfaceDispatcher("cmp", 2));
  global.set("clone", makeInterfaceDispatcher("clone", 1));
  global.set("default", makeInterfaceDispatcher("default", 0));
  global.set("into", makeInterfaceDispatcher("into", 1));
  global.set("from", makeInterfaceDispatcher("from", 1));

  // Built-in effects
  global.set("exn", { tag: "effect-def", name: "exn", ops: ["raise"] });
  global.set("raise", makeEffectOp("raise", "exn"));
  global.set("io", { tag: "effect-def", name: "io", ops: ["print", "read-ln"] });
  global.set("async", { tag: "effect-def", name: "async", ops: ["spawn", "await", "task-yield", "sleep", "task-group-enter", "task-group-exit", "is-cancelled", "check-cancel", "shield-enter", "shield-exit"] });

  // ── Async builtins ──

  global.set("spawn", {
    tag: "builtin",
    name: "spawn",
    fn: (args: Value[], loc: Loc): Value => {
      const body = args[0];
      if (!body || (body.tag !== "closure" && body.tag !== "builtin")) {
        throw { code: "E204", message: "spawn expects a function", location: loc } as RuntimeError;
      }
      const taskId = nextAsyncTaskId++;
      const group = activeGroupStack.length > 0 ? activeGroupStack[activeGroupStack.length - 1] : null;
      const task: AsyncTask = {
        id: taskId,
        status: "pending",
        body: () => applyValue(body, [], loc),
        cancelFlag: false,
        shieldDepth: 0,
        groupId: group?.id ?? null,
      };
      if (group) group.children.push(task);
      return { tag: "future", task };
    },
  });

  global.set("await", {
    tag: "builtin",
    name: "await",
    fn: (args: Value[], loc: Loc): Value => {
      const futVal = args[0];
      if (!futVal || futVal.tag !== "future") {
        throw { code: "E200", message: "await expects a Future", location: loc } as RuntimeError;
      }
      const task = futVal.task;
      // Check cancellation of current task
      if (currentTask && currentTask.cancelFlag && currentTask.shieldDepth === 0) {
        currentTask.status = "cancelled";
        throw { code: "E011", message: "task cancelled", location: loc } as RuntimeError;
      }
      // Run the task if it hasn't been run yet
      if (task.status === "pending") {
        runAsyncTask(task, loc);
      }
      if (task.status === "completed") return task.result ?? { tag: "unit" };
      if (task.status === "failed") {
        throw { code: "E014", message: task.error ?? "task failed", location: loc } as RuntimeError;
      }
      if (task.status === "cancelled") {
        throw { code: "E011", message: "awaited task was cancelled", location: loc } as RuntimeError;
      }
      return { tag: "unit" };
    },
  });

  global.set("task-group", {
    tag: "builtin",
    name: "task-group",
    fn: (args: Value[], loc: Loc): Value => {
      const body = args[0];
      if (!body || (body.tag !== "closure" && body.tag !== "builtin")) {
        throw { code: "E204", message: "task-group expects a function", location: loc } as RuntimeError;
      }
      const group: AsyncTaskGroup = { id: nextAsyncGroupId++, children: [] };
      activeGroupStack.push(group);
      let result: Value;
      let bodyError: any = null;
      try {
        result = applyValue(body, [], loc);
      } catch (e) {
        bodyError = e;
        result = { tag: "unit" };
      }
      activeGroupStack.pop();

      // Cancel still-pending children
      for (const child of group.children) {
        if (child.status === "pending") {
          child.cancelFlag = true;
        }
      }
      // Run remaining children (they'll observe cancellation)
      for (const child of group.children) {
        if (child.status === "pending") {
          runAsyncTask(child, loc);
        }
      }
      // Check for child failures
      let firstChildError: any = null;
      for (const child of group.children) {
        if (child.status === "failed" && !firstChildError) {
          firstChildError = { code: "E014", message: child.error ?? "child task failed", location: loc };
        }
      }

      if (bodyError) throw bodyError;
      if (firstChildError) throw firstChildError;
      return result;
    },
  });

  global.set("task-yield", {
    tag: "builtin",
    name: "task-yield",
    fn: (_args: Value[], loc: Loc): Value => {
      if (currentTask && currentTask.cancelFlag && currentTask.shieldDepth === 0) {
        currentTask.status = "cancelled";
        throw { code: "E011", message: "task cancelled", location: loc } as RuntimeError;
      }
      return { tag: "unit" };
    },
  });

  global.set("sleep", {
    tag: "builtin",
    name: "sleep",
    fn: (args: Value[], loc: Loc): Value => {
      // In synchronous tree-walker, sleep is a cancellation check point
      if (currentTask && currentTask.cancelFlag && currentTask.shieldDepth === 0) {
        currentTask.status = "cancelled";
        throw { code: "E011", message: "task cancelled", location: loc } as RuntimeError;
      }
      return { tag: "unit" };
    },
  });

  global.set("is-cancelled", {
    tag: "builtin",
    name: "is-cancelled",
    fn: (_args: Value[], _loc: Loc): Value => {
      const cancelled = currentTask ? currentTask.cancelFlag && currentTask.shieldDepth === 0 : false;
      return { tag: "bool", value: cancelled };
    },
  });

  global.set("check-cancel", {
    tag: "builtin",
    name: "check-cancel",
    fn: (_args: Value[], loc: Loc): Value => {
      if (currentTask && currentTask.cancelFlag && currentTask.shieldDepth === 0) {
        currentTask.status = "cancelled";
        throw { code: "E011", message: "task cancelled", location: loc } as RuntimeError;
      }
      return { tag: "unit" };
    },
  });

  global.set("shield", {
    tag: "builtin",
    name: "shield",
    fn: (args: Value[], loc: Loc): Value => {
      const body = args[0];
      if (!body || (body.tag !== "closure" && body.tag !== "builtin")) {
        throw { code: "E204", message: "shield expects a function", location: loc } as RuntimeError;
      }
      if (currentTask) currentTask.shieldDepth++;
      try {
        const result = applyValue(body, [], loc);
        if (currentTask) currentTask.shieldDepth--;
        return result;
      } catch (e) {
        if (currentTask) currentTask.shieldDepth--;
        throw e;
      }
    },
  });

  // ── Channel builtins ──

  let nextChannelId = 1;

  global.set("channel", {
    tag: "builtin",
    name: "channel",
    fn: (args: Value[], loc: Loc): Value => {
      const cap = args[0];
      if (!cap || cap.tag !== "int") {
        throw { code: "E204", message: "channel expects an Int capacity", location: loc } as RuntimeError;
      }
      const chan: EvalChannel = {
        id: nextChannelId++,
        buffer: [],
        capacity: cap.value,
        senderOpen: true,
        receiverOpen: true,
      };
      return { tag: "tuple", elements: [{ tag: "sender", channel: chan }, { tag: "receiver", channel: chan }] };
    },
  });

  global.set("send", {
    tag: "builtin",
    name: "send",
    fn: (args: Value[], loc: Loc): Value => {
      const sender = args[0];
      if (!sender || sender.tag !== "sender") {
        throw { code: "E204", message: "send expects a Sender", location: loc } as RuntimeError;
      }
      const chan = sender.channel;
      if (!chan.receiverOpen) {
        throw { code: "E012", message: "send: channel receiver is closed", location: loc } as RuntimeError;
      }
      if (!chan.senderOpen) {
        throw { code: "E012", message: "send: sender is closed", location: loc } as RuntimeError;
      }
      chan.buffer.push(args[1]);
      return { tag: "unit" };
    },
  });

  global.set("recv", {
    tag: "builtin",
    name: "recv",
    fn: (args: Value[], loc: Loc): Value => {
      const receiver = args[0];
      if (!receiver || receiver.tag !== "receiver") {
        throw { code: "E204", message: "recv expects a Receiver", location: loc } as RuntimeError;
      }
      const chan = receiver.channel;
      if (chan.buffer.length > 0) {
        return chan.buffer.shift()!;
      }
      if (!chan.senderOpen) {
        throw { code: "E012", message: "recv: channel is closed and empty", location: loc } as RuntimeError;
      }
      throw { code: "E012", message: "recv: channel is empty", location: loc } as RuntimeError;
    },
  });

  global.set("try-recv", {
    tag: "builtin",
    name: "try-recv",
    fn: (args: Value[], loc: Loc): Value => {
      const receiver = args[0];
      if (!receiver || receiver.tag !== "receiver") {
        throw { code: "E204", message: "try-recv expects a Receiver", location: loc } as RuntimeError;
      }
      const chan = receiver.channel;
      if (chan.buffer.length > 0) {
        return { tag: "variant", name: "Some", fields: [chan.buffer.shift()!] };
      }
      return { tag: "variant", name: "None", fields: [] };
    },
  });

  global.set("close-sender", {
    tag: "builtin",
    name: "close-sender",
    fn: (args: Value[], loc: Loc): Value => {
      const sender = args[0];
      if (!sender || sender.tag !== "sender") {
        throw { code: "E204", message: "close-sender expects a Sender", location: loc } as RuntimeError;
      }
      sender.channel.senderOpen = false;
      return { tag: "unit" };
    },
  });

  global.set("close-receiver", {
    tag: "builtin",
    name: "close-receiver",
    fn: (args: Value[], loc: Loc): Value => {
      const receiver = args[0];
      if (!receiver || receiver.tag !== "receiver") {
        throw { code: "E204", message: "close-receiver expects a Receiver", location: loc } as RuntimeError;
      }
      receiver.channel.receiverOpen = false;
      return { tag: "unit" };
    },
  });

  // ── Runtime-dispatched for-loop builtins ──

  global.set("__for_each", {
    tag: "builtin",
    name: "__for_each",
    fn: (args: Value[], loc: Loc): Value => {
      const collection = args[0];
      const fn = args[1];
      if (collection.tag === "list") {
        const results: Value[] = [];
        for (const el of collection.elements) {
          results.push(applyValue(fn, [el], loc));
        }
        return { tag: "list", elements: results };
      }
      throw { code: "E204", message: "__for_each: expected List", location: loc } as RuntimeError;
    },
  });

  global.set("__for_filter", {
    tag: "builtin",
    name: "__for_filter",
    fn: (args: Value[], loc: Loc): Value => {
      const collection = args[0];
      const fn = args[1];
      if (collection.tag === "list") {
        const results: Value[] = [];
        for (const el of collection.elements) {
          const result = applyValue(fn, [el], loc);
          if (result.tag === "bool" && result.value) {
            results.push(el);
          }
        }
        return { tag: "list", elements: results };
      }
      throw { code: "E204", message: "__for_filter: expected List", location: loc } as RuntimeError;
    },
  });

  global.set("__for_fold", {
    tag: "builtin",
    name: "__for_fold",
    fn: (args: Value[], loc: Loc): Value => {
      const collection = args[0];
      const init = args[1];
      const fn = args[2];
      if (collection.tag === "list") {
        let acc = init;
        for (const el of collection.elements) {
          acc = applyValue(fn, [acc, el], loc);
        }
        return acc;
      }
      throw { code: "E204", message: "__for_fold: expected List", location: loc } as RuntimeError;
    },
  });

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

function loadTopLevels(env: Env, program: Program, baseDir: string, packageModuleMap?: Map<string, string>): void {
  for (const tl of program.topLevels) {
    if (tl.tag === "mod-decl") continue;

    if (tl.tag === "use-decl") {
      const importPath = resolveModulePath(tl.path, baseDir, packageModuleMap);
      const exports = loadModule(importPath, baseDir, packageModuleMap);
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
      // Generate impls for deriving clauses
      if (tl.deriving && tl.deriving.length > 0) {
        registerDerivedImpls(tl.variants, tl.deriving, env);
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

    if (tl.tag === "interface-decl") {
      // Register dispatcher functions for each interface method
      for (const m of tl.methods) {
        interfaceMethodNames.set(m.name, tl.name);
        const dispatcher = makeInterfaceDispatcher(m.name, m.sig.params.length);
        env.set(m.name, dispatcher);
      }
      continue;
    }

    if (tl.tag === "impl-block") {
      const typeTag = typeExprToTag(tl.forType);
      for (const m of tl.methods) {
        // Create closure in the current environment
        const implEnv = env.extend();
        // Determine arity from the interface method signature if available
        const ifaceName = tl.interface_;
        let paramCount = 1;
        // Try to get the interface's method sig to know the param names
        for (const otherTl of program.topLevels) {
          if (otherTl.tag === "interface-decl" && otherTl.name === ifaceName) {
            const methodSig = otherTl.methods.find(ms => ms.name === m.name);
            if (methodSig) paramCount = methodSig.sig.params.length;
            break;
          }
        }
        // If the body is already a closure/function, register it directly
        const bodyValue = evaluate(m.body, implEnv);
        // For From<T>, dispatch `from` on the source type (T), not Self
        if (tl.interface_ === "From" && m.name === "from" && tl.typeArgs.length > 0) {
          const sourceTypeTag = typeExprToTag(tl.typeArgs[0]);
          registerImpl(m.name, sourceTypeTag, bodyValue);
        } else {
          registerImpl(m.name, typeTag, bodyValue);
        }
      }
      // Blanket rule: impl From<A> for B  →  register into for type A
      if (tl.interface_ === "From" && tl.typeArgs.length > 0) {
        const sourceTypeTag = typeExprToTag(tl.typeArgs[0]);
        const fromImpl = implTable.get("from")?.get(sourceTypeTag);
        if (fromImpl) {
          registerImpl("into", sourceTypeTag, {
            tag: "builtin",
            name: `into:${sourceTypeTag}->${typeTag}`,
            fn: (args: Value[], loc: Loc): Value => {
              if (fromImpl.tag === "builtin") return fromImpl.fn(args, loc);
              if (fromImpl.tag === "closure") {
                const callEnv = fromImpl.env.extend();
                for (let i = 0; i < fromImpl.params.length; i++) {
                  callEnv.set(fromImpl.params[i].name, args[i] ?? { tag: "unit" as const });
                }
                return evaluate(fromImpl.body, callEnv);
              }
              throw { code: "E212", message: `no impl of method 'into' for type '${sourceTypeTag}'`, location: loc } as RuntimeError;
            },
          });
        }
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

    if (tl.tag === "extern-decl") {
      const externName = tl.name;
      const library = (tl as any).library as string;
      const host = (tl as any).host as string | null;
      const foreignSymbol = (tl as any).symbol as string | null;
      const paramNames = tl.sig.params.map(p => p.name);

      // Resolve the foreign symbol name (convert hyphens to underscores)
      const jsFnName = foreignSymbol ?? externName.replace(/-/g, "_");

      const externBuiltin: Value = {
        tag: "builtin",
        name: `extern:${externName}`,
        fn: (args: Value[], loc: Loc): Value => {
          // Only JS/Node interop supported for now
          if (host !== "js" && host !== null) {
            throw {
              code: "E804",
              message: `extern host '${host}' is not supported (only 'js' is available in the Node runtime)`,
              location: loc,
            } as RuntimeError;
          }

          // Convert Clank values to JS values
          const jsArgs = args.map(v => clankToJs(v));

          try {
            // Resolve the module and function
            let mod: any;
            try {
              const require = createRequire(import.meta.url);
              mod = require(library);
            } catch {
              throw {
                code: "E800",
                message: `failed to load extern module '${library}'`,
                location: loc,
              } as RuntimeError;
            }

            // Look up the function
            const fn = mod[jsFnName];
            if (typeof fn !== "function") {
              throw {
                code: "E800",
                message: `'${jsFnName}' is not a function in module '${library}'`,
                location: loc,
              } as RuntimeError;
            }

            // Call the foreign function
            const result = fn(...jsArgs);

            // Convert JS result back to Clank value
            return jsToClank(result);
          } catch (e: unknown) {
            if (e && typeof e === "object" && "code" in e) throw e; // Re-throw Clank errors
            throw {
              code: "E800",
              message: `extern call '${externName}' threw: ${e instanceof Error ? e.message : String(e)}`,
              location: loc,
            } as RuntimeError;
          }
        },
      };
      env.set(externName, externBuiltin);
    }
  }
}

// ── FFI value conversion helpers ──

function clankToJs(v: Value): unknown {
  switch (v.tag) {
    case "int": return v.value;
    case "rat": return v.value;
    case "bool": return v.value;
    case "str": return v.value;
    case "unit": return undefined;
    case "list": return v.elements.map(clankToJs);
    case "tuple": return v.elements.map(clankToJs);
    case "record": {
      const obj: Record<string, unknown> = {};
      for (const [k, val] of v.fields) obj[k] = clankToJs(val);
      return obj;
    }
    case "variant": {
      if (v.name === "None" && v.fields.length === 0) return null;
      if (v.name === "Some" && v.fields.length === 1) return clankToJs(v.fields[0]);
      return { tag: v.name, fields: v.fields.map(clankToJs) };
    }
    default: return v;
  }
}

function jsToClank(v: unknown): Value {
  if (v === undefined || v === null) return { tag: "unit" };
  if (typeof v === "number") {
    return Number.isInteger(v) ? { tag: "int", value: v } : { tag: "rat", value: v };
  }
  if (typeof v === "bigint") return { tag: "int", value: Number(v) };
  if (typeof v === "boolean") return { tag: "bool", value: v };
  if (typeof v === "string") return { tag: "str", value: v };
  if (Array.isArray(v)) return { tag: "list", elements: v.map(jsToClank) };
  if (typeof v === "object") {
    if (v instanceof Buffer) return { tag: "str", value: v.toString("utf-8") };
    const fields = new Map<string, Value>();
    for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
      fields.set(k, jsToClank(val));
    }
    return { tag: "record", fields };
  }
  return { tag: "str", value: String(v) };
}

// ── Auto-derive impl generation ──

function registerDerivedImpls(variants: { name: string; fields: { tag: string }[] }[], deriving: string[], env: Env): void {
  const noLoc: Loc = { line: 0, col: 0 };

  // Helper: call an interface method on a value by looking it up from the env
  function callMethod(methodName: string, args: Value[]): Value {
    const fn = env.get(methodName, noLoc);
    return applyValue(fn, args, noLoc);
  }

  for (const ifaceName of deriving) {
    if (ifaceName === "Show") {
      for (const v of variants) {
        registerImpl("show", v.name, {
          tag: "builtin",
          name: `show:${v.name}`,
          fn: (args, _loc) => {
            const val = args[0] as Value & { tag: "variant" };
            if (val.fields.length === 0) {
              return { tag: "str", value: val.name };
            }
            const fieldStrs = val.fields.map(f => {
              const shown = callMethod("show", [f]);
              return (shown as any).value as string;
            });
            return { tag: "str", value: `${val.name}(${fieldStrs.join(", ")})` };
          },
        });
      }
    }

    if (ifaceName === "Eq") {
      for (const v of variants) {
        registerImpl("eq", v.name, {
          tag: "builtin",
          name: `eq:${v.name}`,
          fn: (args, _loc) => {
            const a = args[0] as Value & { tag: "variant" };
            const b = args[1] as Value & { tag: "variant" };
            if (b.tag !== "variant" || a.name !== b.name) return { tag: "bool", value: false };
            for (let i = 0; i < a.fields.length; i++) {
              const result = callMethod("eq", [a.fields[i], b.fields[i]]);
              if (result.tag === "bool" && !result.value) return { tag: "bool", value: false };
            }
            return { tag: "bool", value: true };
          },
        });
      }
    }

    if (ifaceName === "Clone") {
      for (const v of variants) {
        registerImpl("clone", v.name, {
          tag: "builtin",
          name: `clone:${v.name}`,
          fn: (args, _loc) => {
            const val = args[0] as Value & { tag: "variant" };
            if (val.fields.length === 0) return val;
            const clonedFields = val.fields.map(f => callMethod("clone", [f]));
            return { tag: "variant", name: val.name, fields: clonedFields };
          },
        });
      }
    }

    if (ifaceName === "Ord") {
      for (let vi = 0; vi < variants.length; vi++) {
        const v = variants[vi];
        registerImpl("cmp", v.name, {
          tag: "builtin",
          name: `cmp:${v.name}`,
          fn: (args, _loc) => {
            const a = args[0] as Value & { tag: "variant" };
            const b = args[1] as Value & { tag: "variant" };
            // Compare by variant ordinal first
            const ai = variants.findIndex(vv => vv.name === a.name);
            const bi = variants.findIndex(vv => vv.name === b.name);
            if (ai < bi) return { tag: "variant", name: "Lt", fields: [] };
            if (ai > bi) return { tag: "variant", name: "Gt", fields: [] };
            // Same variant: compare fields lexicographically
            for (let i = 0; i < a.fields.length; i++) {
              const result = callMethod("cmp", [a.fields[i], b.fields[i]]);
              if (result.tag === "variant" && result.name !== "Eq_") return result;
            }
            return { tag: "variant", name: "Eq_", fields: [] };
          },
        });
      }
    }

    if (ifaceName === "Default") {
      // Default uses the first nullary variant, or the first variant with default fields
      const nullary = variants.find(v => v.fields.length === 0);
      if (nullary) {
        registerImpl("default", nullary.name, {
          tag: "builtin",
          name: `default:${nullary.name}`,
          fn: (_args, _loc) => ({ tag: "variant", name: nullary.name, fields: [] }),
        });
        // Also register under the type name for when default is called without a value to dispatch on
      }
    }
  }
}

// ── Register built-in interface impls ──

function registerBuiltinImpls(): void {
  // show for primitives
  registerImpl("show", "Int", { tag: "builtin", name: "show:Int", fn: (args, _loc) => ({ tag: "str", value: String((args[0] as any).value) }) });
  registerImpl("show", "Rat", { tag: "builtin", name: "show:Rat", fn: (args, _loc) => ({ tag: "str", value: String((args[0] as any).value) }) });
  registerImpl("show", "Bool", { tag: "builtin", name: "show:Bool", fn: (args, _loc) => ({ tag: "str", value: (args[0] as any).value ? "true" : "false" }) });
  registerImpl("show", "Str", { tag: "builtin", name: "show:Str", fn: (args, _loc) => args[0] });
  registerImpl("show", "Unit", { tag: "builtin", name: "show:Unit", fn: (_args, _loc) => ({ tag: "str", value: "()" }) });

  // eq for primitives
  registerImpl("eq", "Int", { tag: "builtin", name: "eq:Int", fn: (args, _loc) => ({ tag: "bool", value: (args[0] as any).value === (args[1] as any).value }) });
  registerImpl("eq", "Rat", { tag: "builtin", name: "eq:Rat", fn: (args, _loc) => ({ tag: "bool", value: (args[0] as any).value === (args[1] as any).value }) });
  registerImpl("eq", "Bool", { tag: "builtin", name: "eq:Bool", fn: (args, _loc) => ({ tag: "bool", value: (args[0] as any).value === (args[1] as any).value }) });
  registerImpl("eq", "Str", { tag: "builtin", name: "eq:Str", fn: (args, _loc) => ({ tag: "bool", value: (args[0] as any).value === (args[1] as any).value }) });

  // clone for primitives (identity — primitives are value types)
  for (const prim of ["Int", "Rat", "Bool", "Str", "Unit"]) {
    registerImpl("clone", prim, { tag: "builtin", name: `clone:${prim}`, fn: (args, _loc) => args[0] });
  }

  // default for primitives
  registerImpl("default", "Int", { tag: "builtin", name: "default:Int", fn: (_args, _loc) => ({ tag: "int", value: 0 }) });
  registerImpl("default", "Rat", { tag: "builtin", name: "default:Rat", fn: (_args, _loc) => ({ tag: "rat", value: 0.0 }) });
  registerImpl("default", "Bool", { tag: "builtin", name: "default:Bool", fn: (_args, _loc) => ({ tag: "bool", value: false }) });
  registerImpl("default", "Str", { tag: "builtin", name: "default:Str", fn: (_args, _loc) => ({ tag: "str", value: "" }) });
  registerImpl("default", "Unit", { tag: "builtin", name: "default:Unit", fn: (_args, _loc) => ({ tag: "unit" }) });

  // cmp for Int, Rat, Str
  const cmpFn = (a: number | string, b: number | string): Value => {
    if (a < b) return { tag: "variant", name: "Lt", fields: [] };
    if (a > b) return { tag: "variant", name: "Gt", fields: [] };
    return { tag: "variant", name: "Eq_", fields: [] };
  };
  registerImpl("cmp", "Int", { tag: "builtin", name: "cmp:Int", fn: (args, _loc) => cmpFn((args[0] as any).value, (args[1] as any).value) });
  registerImpl("cmp", "Rat", { tag: "builtin", name: "cmp:Rat", fn: (args, _loc) => cmpFn((args[0] as any).value, (args[1] as any).value) });
  registerImpl("cmp", "Str", { tag: "builtin", name: "cmp:Str", fn: (args, _loc) => cmpFn((args[0] as any).value, (args[1] as any).value) });

  // cmp for List — lexicographic element-wise comparison
  registerImpl("cmp", "List", {
    tag: "builtin",
    name: "cmp:List",
    fn: (args, loc) => {
      const a = args[0] as Value & { tag: "list"; elements: Value[] };
      const b = args[1] as Value & { tag: "list"; elements: Value[] };
      const minLen = Math.min(a.elements.length, b.elements.length);
      for (let i = 0; i < minLen; i++) {
        const cmpImpl = implTable.get("cmp")?.get(runtimeTypeTag(a.elements[i]));
        if (!cmpImpl) {
          throw { code: "E212", message: `no impl of method 'cmp' for type '${runtimeTypeTag(a.elements[i])}'`, location: loc } as RuntimeError;
        }
        const result = cmpImpl.tag === "builtin" ? cmpImpl.fn([a.elements[i], b.elements[i]], loc) : { tag: "variant" as const, name: "Eq_", fields: [] as Value[] };
        if (result.tag === "variant" && result.name !== "Eq_") return result;
      }
      if (a.elements.length < b.elements.length) return { tag: "variant", name: "Lt", fields: [] };
      if (a.elements.length > b.elements.length) return { tag: "variant", name: "Gt", fields: [] };
      return { tag: "variant", name: "Eq_", fields: [] };
    },
  });

  // cmp for Tuple — element-wise comparison
  registerImpl("cmp", "Tuple", {
    tag: "builtin",
    name: "cmp:Tuple",
    fn: (args, loc) => {
      const a = args[0] as Value & { tag: "tuple"; elements: Value[] };
      const b = args[1] as Value & { tag: "tuple"; elements: Value[] };
      const minLen = Math.min(a.elements.length, b.elements.length);
      for (let i = 0; i < minLen; i++) {
        const cmpImpl = implTable.get("cmp")?.get(runtimeTypeTag(a.elements[i]));
        if (!cmpImpl) {
          throw { code: "E212", message: `no impl of method 'cmp' for type '${runtimeTypeTag(a.elements[i])}'`, location: loc } as RuntimeError;
        }
        const result = cmpImpl.tag === "builtin" ? cmpImpl.fn([a.elements[i], b.elements[i]], loc) : { tag: "variant" as const, name: "Eq_", fields: [] as Value[] };
        if (result.tag === "variant" && result.name !== "Eq_") return result;
      }
      if (a.elements.length < b.elements.length) return { tag: "variant", name: "Lt", fields: [] };
      if (a.elements.length > b.elements.length) return { tag: "variant", name: "Gt", fields: [] };
      return { tag: "variant", name: "Eq_", fields: [] };
    },
  });

  // cmp for Record — compare fields lexicographically by sorted key order
  registerImpl("cmp", "Record", {
    tag: "builtin",
    name: "cmp:Record",
    fn: (args, loc) => {
      const a = args[0] as Value & { tag: "record"; fields: Map<string, Value> };
      const b = args[1] as Value & { tag: "record"; fields: Map<string, Value> };
      const keys = [...a.fields.keys()].sort();
      for (const key of keys) {
        const av = a.fields.get(key);
        const bv = b.fields.get(key);
        if (!av || !bv) continue;
        const cmpImpl = implTable.get("cmp")?.get(runtimeTypeTag(av));
        if (!cmpImpl) {
          throw { code: "E212", message: `no impl of method 'cmp' for type '${runtimeTypeTag(av)}'`, location: loc } as RuntimeError;
        }
        const result = cmpImpl.tag === "builtin" ? cmpImpl.fn([av, bv], loc) : { tag: "variant" as const, name: "Eq_", fields: [] as Value[] };
        if (result.tag === "variant" && result.name !== "Eq_") return result;
      }
      return { tag: "variant", name: "Eq_", fields: [] };
    },
  });
}

// ── Program runner ──

export function run(program: Program, baseDir?: string, packageModuleMap?: Map<string, string>): Value | RuntimeError {
  try {
    const global = initGlobalEnv();
    const resolveDir = baseDir ?? ".";
    loadTopLevels(global, program, resolveDir, packageModuleMap);

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
    case "future": return `<future:${v.task.id}>`;
    case "sender": return `<sender:${v.channel.id}>`;
    case "receiver": return `<receiver:${v.channel.id}>`;
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
    case "future": return "Future";
    case "sender": return "Sender";
    case "receiver": return "Receiver";
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
