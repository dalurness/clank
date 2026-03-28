// Clank Embedding API — lets host programs instantiate and interact with Clank programs.
// See plan/features/ffi-interop.md §6 for the specification.

import { readFileSync } from "node:fs";
import { lex } from "./lexer.js";
import { parse } from "./parser.js";
import { desugar } from "./desugar.js";
import { compileProgram, type BytecodeModule, type BytecodeWord, type ExternEntry } from "./compiler.js";
import { VM, VMTrap, Val, Tag, type Value } from "./vm.js";
import type { Program, TopLevel } from "./ast.js";

// ── Public Types ──

export interface HostFn {
  sig: string;
  impl: (...args: any[]) => any;
}

export interface RuntimeOptions {
  maxSteps?: number;
}

export interface ClankModule {
  name: string;
  exports: Map<string, { wordId: number; isPublic: boolean }>;
}

export interface ClankResult {
  ok: boolean;
  value?: ClankValue;
  error?: ClankError;
  stdout: string[];
}

export interface ClankValue {
  type: string;
  value: unknown;
}

export interface ClankError {
  code: string;
  message: string;
}

// ── ClankRuntime ──

export class ClankRuntime {
  private hostFunctions: Map<string, (...args: unknown[]) => unknown> = new Map();
  private hostExterns: Map<string, { library: string; symbol: string }> = new Map();
  private modules: ClankModule[] = [];
  private currentModule: BytecodeModule | null = null;
  private currentVM: VM | null = null;
  private disposed = false;
  private maxSteps: number | undefined;

  private constructor(opts?: RuntimeOptions) {
    this.maxSteps = opts?.maxSteps;
  }

  static create(opts?: RuntimeOptions): ClankRuntime {
    return new ClankRuntime(opts);
  }

  dispose(): void {
    this.disposed = true;
    this.currentVM = null;
    this.currentModule = null;
    this.modules = [];
    this.hostFunctions.clear();
    this.hostExterns.clear();
  }

  // ── Module Loading ──

  loadModule(source: string, name?: string): ClankModule {
    this.ensureNotDisposed();

    const tokens = lex(source);
    if (!Array.isArray(tokens)) {
      throw new Error(`Lex error: ${(tokens as any).message ?? "unknown"}`);
    }

    const ast = parse(tokens);
    if ("code" in ast) {
      throw new Error(`Parse error: ${(ast as any).message ?? "unknown"}`);
    }

    const program: Program = {
      topLevels: (ast as Program).topLevels.map(tl => {
        if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
        if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
        return tl;
      }),
    };

    const compiled = compileProgram(program);
    this.currentModule = compiled;

    // Create VM and register host functions
    const vm = new VM(compiled);
    for (const [key, fn] of this.hostFunctions) {
      vm.registerHostFunction(key, fn);
    }
    this.currentVM = vm;

    // Build exports map
    const exports = new Map<string, { wordId: number; isPublic: boolean }>();
    for (const word of compiled.words) {
      if (word.isPublic) {
        exports.set(word.name, { wordId: word.wordId, isPublic: true });
      }
    }

    const moduleName = name ?? "main";
    const mod: ClankModule = { name: moduleName, exports };
    this.modules.push(mod);
    return mod;
  }

  loadFile(path: string): ClankModule {
    this.ensureNotDisposed();
    const source = readFileSync(path, "utf-8");
    const name = path.replace(/^.*[\\/]/, "").replace(/\.clk$/, "");
    return this.loadModule(source, name);
  }

  // ── Function Registration ──

  register(name: string, sig: string, impl: (...args: any[]) => any, library = "host"): void {
    this.ensureNotDisposed();
    const symbol = name.replace(/-/g, "_");
    const key = `${library}::${symbol}`;
    this.hostFunctions.set(key, impl);
    this.hostExterns.set(name, { library, symbol });

    if (this.currentVM) {
      this.currentVM.registerHostFunction(key, impl);
    }
  }

  registerModule(moduleName: string, fns: Record<string, HostFn>): void {
    this.ensureNotDisposed();
    for (const [name, { impl }] of Object.entries(fns)) {
      const symbol = name.replace(/-/g, "_");
      const key = `${moduleName}::${symbol}`;
      this.hostFunctions.set(key, impl);
      this.hostExterns.set(`${moduleName}::${name}`, { library: moduleName, symbol });
    }

    // If VM already exists, inject the new functions
    if (this.currentVM) {
      for (const [name, { impl }] of Object.entries(fns)) {
        const symbol = name.replace(/-/g, "_");
        const key = `${moduleName}::${symbol}`;
        this.currentVM.registerHostFunction(key, impl);
      }
    }
  }

  // ── Function Invocation ──

  call(fn: string, ...args: ClankValue[]): ClankResult {
    this.ensureNotDisposed();
    if (!this.currentVM || !this.currentModule) {
      return {
        ok: false,
        error: { code: "E010", message: "No module loaded" },
        stdout: [],
      };
    }

    const vm = this.currentVM;

    // Find the word by name
    const word = this.currentModule.words.find(w => w.name === fn);
    if (!word) {
      return {
        ok: false,
        error: { code: "E010", message: `Function '${fn}' not found` },
        stdout: [],
      };
    }

    try {
      const vmArgs = args.map(arg => this.clankValueToVm(arg));
      const result = vm.callWordWithArgs(word.wordId, vmArgs);
      const stdout = [...vm.stdout];
      vm.stdout.length = 0;

      if (result === undefined) {
        return { ok: true, stdout };
      }

      return {
        ok: true,
        value: this.vmToClankValue(result),
        stdout,
      };
    } catch (e: unknown) {
      const stdout = [...vm.stdout];
      vm.stdout.length = 0;

      if (e instanceof VMTrap) {
        return {
          ok: false,
          error: { code: e.code, message: e.message },
          stdout,
        };
      }
      return {
        ok: false,
        error: { code: "E000", message: e instanceof Error ? e.message : String(e) },
        stdout,
      };
    }
  }

  // ── Value Conversion ──

  toClank(jsValue: unknown, typeHint?: string): ClankValue {
    this.ensureNotDisposed();
    return this.jsToClankValue(jsValue, typeHint);
  }

  toJS(clankValue: ClankValue): unknown {
    return clankValue.value;
  }

  // ── Internal Helpers ──

  private ensureNotDisposed(): void {
    if (this.disposed) {
      throw new Error("ClankRuntime has been disposed");
    }
  }

  private jsToClankValue(v: unknown, typeHint?: string): ClankValue {
    if (v === undefined || v === null) {
      return { type: "()", value: undefined };
    }
    if (typeof v === "number") {
      if (typeHint === "Rat" || !Number.isInteger(v)) {
        return { type: "Rat", value: v };
      }
      return { type: "Int", value: v };
    }
    if (typeof v === "bigint") {
      return { type: "Int", value: Number(v) };
    }
    if (typeof v === "boolean") {
      return { type: "Bool", value: v };
    }
    if (typeof v === "string") {
      return { type: "Str", value: v };
    }
    if (Array.isArray(v)) {
      const items = v.map(i => this.jsToClankValue(i));
      return { type: `[${items[0]?.type ?? "unknown"}]`, value: items.map(i => i.value) };
    }
    if (typeof v === "object") {
      const fields: Record<string, unknown> = {};
      const fieldTypes: string[] = [];
      for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
        const cv = this.jsToClankValue(val);
        fields[k] = cv.value;
        fieldTypes.push(`${k}: ${cv.type}`);
      }
      return { type: `{${fieldTypes.join(", ")}}`, value: fields };
    }
    return { type: "Str", value: String(v) };
  }

  private clankValueToVm(cv: ClankValue): Value {
    const v = cv.value;
    if (v === undefined || v === null) return Val.unit();
    if (typeof v === "number") {
      return Number.isInteger(v) && cv.type !== "Rat" ? Val.int(v) : Val.rat(v);
    }
    if (typeof v === "bigint") return Val.int(Number(v));
    if (typeof v === "boolean") return Val.bool(v);
    if (typeof v === "string") return Val.str(v);
    if (Array.isArray(v)) {
      return Val.list(v.map(i => {
        if (typeof i === "object" && i !== null && "type" in i && "value" in i) {
          return this.clankValueToVm(i as ClankValue);
        }
        return this.clankValueToVm(this.jsToClankValue(i));
      }));
    }
    if (typeof v === "object") {
      const fields = new Map<string, Value>();
      for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
        if (typeof val === "object" && val !== null && "type" in val && "value" in val) {
          fields.set(k, this.clankValueToVm(val as ClankValue));
        } else {
          fields.set(k, this.clankValueToVm(this.jsToClankValue(val)));
        }
      }
      return Val.record(fields);
    }
    return Val.str(String(v));
  }

  private vmToClankValue(v: Value): ClankValue {
    switch (v.tag) {
      case Tag.INT: return { type: "Int", value: v.value };
      case Tag.RAT: return { type: "Rat", value: v.value };
      case Tag.BOOL: return { type: "Bool", value: v.value };
      case Tag.STR: return { type: "Str", value: v.value };
      case Tag.BYTE: return { type: "Byte", value: v.value };
      case Tag.UNIT: return { type: "()", value: undefined };
      case Tag.HEAP: {
        const obj = v.value;
        switch (obj.kind) {
          case "list":
            return {
              type: "[...]",
              value: obj.items.map((i: Value) => this.vmToClankValue(i).value),
            };
          case "tuple":
            return {
              type: "(...)",
              value: obj.items.map((i: Value) => this.vmToClankValue(i).value),
            };
          case "record": {
            const result: Record<string, unknown> = {};
            for (const [k, val] of obj.fields) result[k] = this.vmToClankValue(val).value;
            return { type: "{...}", value: result };
          }
          case "union": {
            return { type: "union", value: { tag: obj.variantTag, fields: obj.fields.map((f: Value) => this.vmToClankValue(f).value) } };
          }
          default:
            return { type: "opaque", value: undefined };
        }
      }
      default:
        return { type: "unknown", value: undefined };
    }
  }
}
