// Clank doc subcommand — queryable documentation for builtins and user-defined functions
// Supports name search, type-directed search (T -> U patterns), and detailed show

import type { TypeExpr, TypeSig, Program } from "./ast.js";
import type { Type } from "./types.js";
import { tInt, tRat, tBool, tStr, tUnit, tFn, tList } from "./types.js";
import { BUILTIN_REGISTRY } from "./builtin-registry.js";

// ── Doc entry ──

export type DocEntry = {
  name: string;
  kind: "builtin" | "function" | "type" | "effect";
  signature: string;
  type: Type;
  description: string;
  params?: { name: string; type: string }[];
  returnType?: string;
  effects?: string[];
  pub?: boolean;
  file?: string;
};

// ── Show types for display ──

function showType(t: Type): string {
  switch (t.tag) {
    case "t-primitive":
      return t.name === "unit" ? "()" : t.name.charAt(0).toUpperCase() + t.name.slice(1);
    case "t-fn": {
      const effs = t.effects.length > 0
        ? ` {${t.effects.map(e => e.tag === "e-named" ? e.name : `e${e.id}`).join(", ")}}`
        : "";
      const paramStr = t.param.tag === "t-fn" ? `(${showType(t.param)})` : showType(t.param);
      return `${paramStr} ->${effs} ${showType(t.result)}`;
    }
    case "t-list": return `[${showType(t.element)}]`;
    case "t-tuple": return `(${t.elements.map(showType).join(", ")})`;
    case "t-record": return `{${t.fields.map(f => `${f.name}: ${showType(f.type)}`).join(", ")}}`;
    case "t-variant": return t.variants.map(v => v.name).join(" | ");
    case "t-var": return `t${t.id}`;
    case "t-generic":
      if (t.name === "?") return "a";
      return t.args.length > 0 ? `${t.name}<${t.args.map(showType).join(", ")}>` : t.name;
  }
}

function showTypeExpr(te: TypeExpr): string {
  switch (te.tag) {
    case "t-name": return te.name;
    case "t-list": return `[${showTypeExpr(te.element)}]`;
    case "t-tuple":
      if (te.elements.length === 0) return "()";
      return `(${te.elements.map(showTypeExpr).join(", ")})`;
    case "t-fn": {
      const effs = te.effects.length > 0 ? ` {${te.effects.join(", ")}}` : "";
      return `${showTypeExpr(te.param)} ->${effs} ${showTypeExpr(te.result)}`;
    }
    case "t-generic": return `${te.name}<${te.args.map(showTypeExpr).join(", ")}>`;
    case "t-record": return `{${te.fields.map(f => `${f.name}: ${showTypeExpr(f.type)}`).join(", ")}}`;
    case "t-union": return `${showTypeExpr(te.left)} | ${showTypeExpr(te.right)}`;
    case "t-refined": return `${showTypeExpr(te.base)}{${te.predicate}}`;
  }
}

function sigToString(sig: TypeSig): string {
  const params = sig.params.map(p => `${showTypeExpr(p.type)}`).join(", ");
  const effs = sig.effects.length > 0 ? ` {${sig.effects.join(", ")}}` : "";
  return `(${params}) ->${effs} ${showTypeExpr(sig.returnType)}`;
}

// ── Builtin registry ──

const tAny: Type = { tag: "t-generic", name: "?", args: [] };

function getBuiltinEntries(): DocEntry[] {
  return BUILTIN_REGISTRY
    .filter(e => e.name !== "raise" && e.name !== "exn" && e.name !== "io")
    .map(entry => ({
      name: entry.name,
      kind: "builtin" as const,
      signature: showType(entry.type),
      type: entry.type,
      description: entry.description,
    }));
}

// ── Extract entries from a parsed program ──

function resolveTypeExprToType(te: TypeExpr): Type {
  switch (te.tag) {
    case "t-name":
      switch (te.name) {
        case "Int": return tInt;
        case "Rat": return tRat;
        case "Bool": return tBool;
        case "Str": return tStr;
        case "Unit": return tUnit;
        default: return { tag: "t-generic", name: te.name, args: [] };
      }
    case "t-list": return tList(resolveTypeExprToType(te.element));
    case "t-tuple":
      if (te.elements.length === 0) return tUnit;
      return { tag: "t-tuple", elements: te.elements.map(resolveTypeExprToType) };
    case "t-fn": return tFn(
      resolveTypeExprToType(te.param),
      resolveTypeExprToType(te.result),
      te.effects.map(e => ({ tag: "e-named" as const, name: e })),
    );
    case "t-generic": return { tag: "t-generic", name: te.name, args: te.args.map(resolveTypeExprToType) };
    case "t-record": return { tag: "t-record", fields: te.fields.map(f => ({ name: f.name, type: resolveTypeExprToType(f.type) })), rowVar: null };
    default: return tAny;
  }
}

export function extractProgramEntries(program: Program, file?: string): DocEntry[] {
  const entries: DocEntry[] = [];
  for (const tl of program.topLevels) {
    if (tl.tag === "definition") {
      const retType = resolveTypeExprToType(tl.sig.returnType);
      let fnType: Type;
      if (tl.sig.params.length === 0) {
        fnType = tFn(tUnit, retType);
      } else {
        fnType = retType;
        for (let i = tl.sig.params.length - 1; i >= 0; i--) {
          fnType = tFn(resolveTypeExprToType(tl.sig.params[i].type), fnType);
        }
      }
      entries.push({
        name: tl.name,
        kind: "function",
        signature: sigToString(tl.sig),
        type: fnType,
        description: `User-defined function`,
        params: tl.sig.params.map(p => ({ name: p.name, type: showTypeExpr(p.type) })),
        returnType: showTypeExpr(tl.sig.returnType),
        effects: tl.sig.effects,
        pub: tl.pub,
        file,
      });
    } else if (tl.tag === "type-decl") {
      entries.push({
        name: tl.name,
        kind: "type",
        signature: tl.typeParams.length > 0
          ? `type ${tl.name}<${tl.typeParams.join(", ")}> = ${tl.variants.map(v => v.fields.length > 0 ? `${v.name}(${v.fields.map(showTypeExpr).join(", ")})` : v.name).join(" | ")}`
          : `type ${tl.name} = ${tl.variants.map(v => v.fields.length > 0 ? `${v.name}(${v.fields.map(showTypeExpr).join(", ")})` : v.name).join(" | ")}`,
        type: { tag: "t-generic", name: tl.name, args: [] },
        description: `User-defined type`,
        pub: tl.pub,
        file,
      });
    } else if (tl.tag === "effect-decl") {
      entries.push({
        name: tl.name,
        kind: "effect",
        signature: `effect ${tl.name} { ${tl.ops.map(op => `${op.name}: ${sigToString(op.sig)}`).join("; ")} }`,
        type: { tag: "t-generic", name: "effect", args: [] },
        description: `User-defined effect`,
        pub: tl.pub,
        file,
      });
    }
  }
  return entries;
}

// ── Type pattern matching ──

// Parse a simple type pattern like "Int -> Bool", "[Int] -> Int", "a -> Str"
// Lowercase single letters are treated as type variables (match anything)
function parseTypePattern(pattern: string): Type | null {
  const trimmed = pattern.trim();
  if (!trimmed) return null;

  // Arrow type: split on ->
  const arrowIdx = findTopLevelArrow(trimmed);
  if (arrowIdx !== -1) {
    const left = parseTypePattern(trimmed.slice(0, arrowIdx));
    const right = parseTypePattern(trimmed.slice(arrowIdx + 2));
    if (!left || !right) return null;
    return tFn(left, right);
  }

  // List type: [T]
  if (trimmed.startsWith("[") && trimmed.endsWith("]")) {
    const inner = parseTypePattern(trimmed.slice(1, -1));
    if (!inner) return null;
    return tList(inner);
  }

  // Parenthesized type
  if (trimmed.startsWith("(") && trimmed.endsWith(")")) {
    return parseTypePattern(trimmed.slice(1, -1));
  }

  // Primitive names
  switch (trimmed) {
    case "Int": return tInt;
    case "Rat": return tRat;
    case "Bool": return tBool;
    case "Str": return tStr;
    case "Unit": case "()": return tUnit;
  }

  // Single lowercase letter = type variable (wildcard)
  if (/^[a-z]$/.test(trimmed)) return tAny;

  // Named type
  if (/^[A-Z][A-Za-z0-9]*$/.test(trimmed)) {
    return { tag: "t-generic", name: trimmed, args: [] };
  }

  return null;
}

function findTopLevelArrow(s: string): number {
  let depth = 0;
  for (let i = 0; i < s.length - 1; i++) {
    if (s[i] === "(" || s[i] === "[") depth++;
    else if (s[i] === ")" || s[i] === "]") depth--;
    else if (depth === 0 && s[i] === "-" && s[i + 1] === ">") return i;
  }
  return -1;
}

// Check if a type matches a pattern (pattern variables match anything)
function typeMatchesPattern(ty: Type, pattern: Type): boolean {
  // Pattern is a wildcard (tAny / type variable)
  if (pattern.tag === "t-generic" && pattern.name === "?") return true;

  if (pattern.tag === "t-fn" && ty.tag === "t-fn") {
    return typeMatchesPattern(ty.param, pattern.param) &&
           typeMatchesPattern(ty.result, pattern.result);
  }

  if (pattern.tag === "t-list" && ty.tag === "t-list") {
    return typeMatchesPattern(ty.element, pattern.element);
  }

  if (pattern.tag === "t-primitive" && ty.tag === "t-primitive") {
    return pattern.name === ty.name;
  }

  if (pattern.tag === "t-generic" && ty.tag === "t-generic") {
    return pattern.name === ty.name;
  }

  // Also check if pattern matches anywhere in a curried chain
  // e.g. pattern "Int -> Bool" should match "Int -> Int -> Bool"
  if (pattern.tag === "t-fn" && ty.tag === "t-fn") {
    // Already handled above, but also try matching the result part
    return typeMatchesPattern(ty.result, pattern);
  }

  return false;
}

// For curried functions, also try matching the pattern against sub-arrows
function typeMatchesPatternDeep(ty: Type, pattern: Type): boolean {
  if (typeMatchesPattern(ty, pattern)) return true;
  // Try skipping one param in a curried function
  if (ty.tag === "t-fn") {
    return typeMatchesPatternDeep(ty.result, pattern);
  }
  return false;
}

// ── Search ──

export function searchEntries(
  entries: DocEntry[],
  query: string,
): DocEntry[] {
  // Try type-directed search if query contains ->
  if (query.includes("->")) {
    const pattern = parseTypePattern(query);
    if (pattern) {
      return entries.filter(e => typeMatchesPatternDeep(e.type, pattern));
    }
  }

  // Name search (case-insensitive substring)
  const lowerQuery = query.toLowerCase();
  return entries.filter(e => e.name.toLowerCase().includes(lowerQuery));
}

// ── Show ──

export function findEntry(entries: DocEntry[], name: string): DocEntry | undefined {
  return entries.find(e => e.name === name);
}

// ── Format for display ──

export function formatEntryShort(entry: DocEntry): string {
  return `${entry.name}: ${entry.signature}  [${entry.kind}]`;
}

export function formatEntryDetailed(entry: DocEntry): string {
  const lines: string[] = [];
  lines.push(`${entry.name}`);
  lines.push(`  Kind: ${entry.kind}`);
  lines.push(`  Signature: ${entry.signature}`);
  lines.push(`  Description: ${entry.description}`);
  if (entry.params && entry.params.length > 0) {
    lines.push(`  Parameters:`);
    for (const p of entry.params) {
      lines.push(`    ${p.name}: ${p.type}`);
    }
  }
  if (entry.returnType) {
    lines.push(`  Returns: ${entry.returnType}`);
  }
  if (entry.effects && entry.effects.length > 0) {
    lines.push(`  Effects: ${entry.effects.join(", ")}`);
  }
  if (entry.file) {
    lines.push(`  File: ${entry.file}`);
  }
  if (entry.pub !== undefined) {
    lines.push(`  Public: ${entry.pub}`);
  }
  return lines.join("\n");
}

export function entryToJSON(entry: DocEntry): Record<string, unknown> {
  const obj: Record<string, unknown> = {
    name: entry.name,
    kind: entry.kind,
    signature: entry.signature,
    description: entry.description,
  };
  if (entry.params) obj.params = entry.params;
  if (entry.returnType) obj.returnType = entry.returnType;
  if (entry.effects && entry.effects.length > 0) obj.effects = entry.effects;
  if (entry.file) obj.file = entry.file;
  if (entry.pub !== undefined) obj.pub = entry.pub;
  return obj;
}

// ── Public API for CLI ──

export function getAllBuiltinEntries(): DocEntry[] {
  return getBuiltinEntries();
}
