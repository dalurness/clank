// Clank built-in functions
// Arithmetic, comparison, logic, and string operations

import type { Loc } from "./ast.js";
import type { Value, RuntimeError } from "./eval.js";

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
