// Clank bidirectional type checker
// Validates programs after parsing/desugaring, before evaluation.
// Produces structured JSON type errors.

import type { EffectRef, Expr, Loc, Pattern, Program, TypeExpr } from "./ast.js";
import type { Type, Effect } from "./types.js";
import { tInt, tRat, tBool, tStr, tUnit, tFn, tList, tTuple, eNamed } from "./types.js";
import { BUILTIN_REGISTRY } from "./builtin-registry.js";

// ── Type errors ──

export type TypeError = {
  code: string;
  message: string;
  location: Loc;
};

// Sentinel for "unknown/any" — permissive in all comparisons
const tAny: Type = { tag: "t-generic", name: "?", args: [] };

// ── Type environment ──

class TypeEnv {
  private bindings: Map<string, Type>;
  private parent: TypeEnv | null;

  constructor(parent: TypeEnv | null = null) {
    this.bindings = new Map();
    this.parent = parent;
  }

  get(name: string): Type | undefined {
    const t = this.bindings.get(name);
    if (t !== undefined) return t;
    if (this.parent) return this.parent.get(name);
    return undefined;
  }

  set(name: string, type: Type): void {
    this.bindings.set(name, type);
  }

  extend(): TypeEnv {
    return new TypeEnv(this);
  }
}

// ── Resolve TypeExpr (syntax) → Type (semantic) ──

function resolveType(te: TypeExpr): Type {
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
    case "t-list": return tList(resolveType(te.element));
    case "t-tuple":
      if (te.elements.length === 0) return tUnit;
      return tTuple(te.elements.map(resolveType));
    case "t-fn": return tFn(resolveType(te.param), resolveType(te.result), te.effects.map(e => eNamed(e)));
    case "t-generic": return { tag: "t-generic", name: te.name, args: te.args.map(resolveType) };
    case "t-record": return { tag: "t-record", fields: te.fields.map(f => ({ name: f.name, type: resolveType(f.type) })) };
    case "t-union": case "t-refined": return tAny;
  }
}

// ── Display types for error messages ──

function showType(t: Type): string {
  switch (t.tag) {
    case "t-primitive": return t.name === "unit" ? "()" : t.name.charAt(0).toUpperCase() + t.name.slice(1);
    case "t-fn": {
      const effs = t.effects.length > 0 ? ` {${t.effects.map(e => e.tag === "e-named" ? e.name : `e${e.id}`).join(", ")}}` : "";
      return `(${showType(t.param)} ->${effs} ${showType(t.result)})`;
    }
    case "t-list": return `[${showType(t.element)}]`;
    case "t-tuple": return `(${t.elements.map(showType).join(", ")})`;
    case "t-record": return `{${t.fields.map(f => `${f.name}: ${showType(f.type)}`).join(", ")}}`;
    case "t-variant": return t.variants.map(v => v.name).join(" | ");
    case "t-var": return `t${t.id}`;
    case "t-generic": return t.args.length > 0 ? `${t.name}<${t.args.map(showType).join(", ")}>` : t.name;
  }
}

// ── Structural type equality ──

function typeEqual(a: Type, b: Type): boolean {
  if (a.tag === "t-generic" || b.tag === "t-generic") return true; // unknown/user types: permissive
  if (a.tag === "t-var" || b.tag === "t-var") return true; // type variables: permissive
  if (a.tag !== b.tag) return false;
  switch (a.tag) {
    case "t-primitive": return a.name === (b as typeof a).name;
    case "t-fn": return typeEqual(a.param, (b as typeof a).param) && typeEqual(a.result, (b as typeof a).result);
    case "t-list": return typeEqual(a.element, (b as typeof a).element);
    case "t-tuple": {
      const bt = b as typeof a;
      return a.elements.length === bt.elements.length && a.elements.every((e, i) => typeEqual(e, bt.elements[i]));
    }
    default: return true;
  }
}

// Numeric coercion: Int and Rat are compatible in arithmetic
function numCompatible(a: Type, b: Type): boolean {
  const isNum = (t: Type) => t.tag === "t-primitive" && (t.name === "int" || t.name === "rat");
  return isNum(a) && isNum(b);
}

// ── Variant registry for exhaustiveness checking ──

type VariantRegistry = Map<string, string[]>; // type name → variant constructor names

function checkMatchExhaustiveness(
  expr: Extract<Expr, { tag: "match" }>,
  subjectType: Type,
  registry: VariantRegistry,
  errors: TypeError[],
): void {
  // Only check when the subject is a known sum type
  if (subjectType.tag !== "t-generic" || subjectType.name === "?") return;
  const allVariants = registry.get(subjectType.name);
  if (!allVariants || allVariants.length === 0) return;

  // A wildcard or variable pattern covers everything
  for (const arm of expr.arms) {
    if (arm.pattern.tag === "p-wildcard" || arm.pattern.tag === "p-var") return;
  }

  const covered = new Set(
    expr.arms
      .filter(a => a.pattern.tag === "p-variant")
      .map(a => (a.pattern as Extract<Pattern, { tag: "p-variant" }>).name),
  );
  const missing = allVariants.filter(v => !covered.has(v));
  if (missing.length > 0) {
    errors.push({
      code: "W400",
      message: `non-exhaustive match: missing variant${missing.length > 1 ? "s" : ""} ${missing.join(", ")}`,
      location: expr.loc,
    });
  }
}

// ── Infer expression type ──

function inferExpr(expr: Expr, env: TypeEnv, errors: TypeError[], registry: VariantRegistry): Type {
  switch (expr.tag) {
    case "literal":
      switch (expr.value.tag) {
        case "int": return tInt;
        case "rat": return tRat;
        case "bool": return tBool;
        case "str": return tStr;
        case "unit": return tUnit;
      }
      break;

    case "var": {
      const t = env.get(expr.name);
      if (!t) {
        errors.push({ code: "E300", message: `unbound variable '${expr.name}'`, location: expr.loc });
        return tAny;
      }
      return t;
    }

    case "let": {
      const valType = inferExpr(expr.value, env, errors, registry);
      if (expr.body === null) return valType;
      if (expr.name === "_") return inferExpr(expr.body, env, errors, registry);
      const child = env.extend();
      child.set(expr.name, valType);
      return inferExpr(expr.body, child, errors, registry);
    }

    case "if": {
      const condType = inferExpr(expr.cond, env, errors, registry);
      if (condType.tag === "t-primitive" && condType.name !== "bool") {
        errors.push({ code: "E301", message: `if condition must be Bool, got ${showType(condType)}`, location: expr.loc });
      }
      const thenType = inferExpr(expr.then, env, errors, registry);
      const elseType = inferExpr(expr.else, env, errors, registry);
      if (!typeEqual(thenType, elseType) && !numCompatible(thenType, elseType)) {
        errors.push({ code: "E302", message: `if branches have different types: ${showType(thenType)} vs ${showType(elseType)}`, location: expr.loc });
      }
      return thenType;
    }

    case "lambda": {
      const lamEnv = env.extend();
      const paramTypes: Type[] = [];
      for (const p of expr.params) {
        const pt = p.type ? resolveType(p.type) : tAny;
        lamEnv.set(p.name, pt);
        paramTypes.push(pt);
      }
      const retType = inferExpr(expr.body, lamEnv, errors, registry);
      let fnType: Type = retType;
      for (let i = paramTypes.length - 1; i >= 0; i--) {
        fnType = tFn(paramTypes[i], fnType);
      }
      return fnType;
    }

    case "apply": {
      const fnType = inferExpr(expr.fn, env, errors, registry);
      if (fnType.tag === "t-fn") {
        // 0-arg call on Unit -> T (thunk call)
        if (expr.args.length === 0 && fnType.param.tag === "t-primitive" && fnType.param.name === "unit") {
          return fnType.result;
        }
        // Uncurry to count expected params
        const paramTypes: Type[] = [];
        let cur: Type = fnType;
        while (cur.tag === "t-fn" && paramTypes.length < expr.args.length) {
          paramTypes.push(cur.param);
          cur = cur.result;
        }
        if (expr.args.length > paramTypes.length) {
          errors.push({
            code: "E303",
            message: `expected ${paramTypes.length} arguments, got ${expr.args.length}`,
            location: expr.loc,
          });
        }
        for (let i = 0; i < Math.min(expr.args.length, paramTypes.length); i++) {
          const argType = inferExpr(expr.args[i], env, errors, registry);
          if (!typeEqual(argType, paramTypes[i]) && !numCompatible(argType, paramTypes[i])) {
            errors.push({
              code: "E304",
              message: `argument ${i + 1}: expected ${showType(paramTypes[i])}, got ${showType(argType)}`,
              location: expr.args[i].loc,
            });
          }
        }
        return cur;
      }
      // Non-function type being called — infer args for nested checking, return unknown
      for (const arg of expr.args) inferExpr(arg, env, errors, registry);
      return tAny;
    }

    case "match": {
      const subjectType = inferExpr(expr.subject, env, errors, registry);
      let resultType: Type | null = null;
      for (const arm of expr.arms) {
        const armEnv = env.extend();
        bindPatternVars(arm.pattern, armEnv);
        const armType = inferExpr(arm.body, armEnv, errors, registry);
        if (resultType === null) {
          resultType = armType;
        } else if (!typeEqual(resultType, armType) && !numCompatible(resultType, armType)) {
          errors.push({ code: "E305", message: `match arms have inconsistent types: ${showType(resultType)} vs ${showType(armType)}`, location: expr.loc });
        }
      }
      checkMatchExhaustiveness(expr, subjectType, registry, errors);
      return resultType ?? tAny;
    }

    case "list": {
      if (expr.elements.length === 0) return tList(tAny);
      const elemType = inferExpr(expr.elements[0], env, errors, registry);
      for (let i = 1; i < expr.elements.length; i++) {
        const et = inferExpr(expr.elements[i], env, errors, registry);
        if (!typeEqual(elemType, et) && !numCompatible(elemType, et)) {
          errors.push({ code: "E306", message: `list element ${i + 1} has type ${showType(et)}, expected ${showType(elemType)}`, location: expr.elements[i].loc });
        }
      }
      return tList(elemType);
    }

    case "tuple":
      return tTuple(expr.elements.map(e => inferExpr(e, env, errors, registry)));

    case "handle": {
      inferExpr(expr.expr, env, errors, registry);
      let resultType: Type = tAny;
      for (const arm of expr.arms) {
        const armEnv = env.extend();
        for (const p of arm.params) armEnv.set(p.name, tAny);
        if (arm.resumeName) armEnv.set(arm.resumeName, tFn(tAny, tAny));
        const armType = inferExpr(arm.body, armEnv, errors, registry);
        // The return arm determines the overall handle type
        if (arm.name === "return") resultType = armType;
      }
      return resultType;
    }

    case "perform":
      return inferExpr(expr.expr, env, errors, registry);

    case "pipeline": case "infix": case "unary": case "do": case "for": case "range":
    case "record": case "field-access":
      return tAny;
  }

  return tAny;
}

// Bind pattern variables as unknown types
function bindPatternVars(pat: Pattern, env: TypeEnv): void {
  switch (pat.tag) {
    case "p-var": env.set(pat.name, tAny); break;
    case "p-tuple": pat.elements.forEach(p => bindPatternVars(p, env)); break;
    case "p-variant": pat.args.forEach(p => bindPatternVars(p, env)); break;
    case "p-wildcard": case "p-literal": break;
  }
}

// ── Register builtin function types ──

function registerBuiltins(env: TypeEnv): void {
  for (const entry of BUILTIN_REGISTRY) {
    env.set(entry.name, entry.type);
  }
}

// ── Effect collection ──

type EffectOpMap = Map<string, string>; // operation name → effect name

function collectEffects(expr: Expr, opMap: EffectOpMap, handled: Set<string>): Set<string> {
  const effects = new Set<string>();
  function walk(e: Expr): void {
    switch (e.tag) {
      case "perform": {
        const inner = e.expr.tag === "apply" ? e.expr.fn : e.expr;
        if (inner.tag === "var") {
          const eff = opMap.get(inner.name);
          if (eff && !handled.has(eff)) effects.add(eff);
        }
        walk(e.expr);
        break;
      }
      case "handle": {
        const innerHandled = new Set(handled);
        for (const arm of e.arms) {
          if (arm.name !== "return") {
            const eff = opMap.get(arm.name);
            if (eff) innerHandled.add(eff);
          }
        }
        for (const eff of collectEffects(e.expr, opMap, innerHandled)) effects.add(eff);
        for (const arm of e.arms) walk(arm.body);
        break;
      }
      case "let": walk(e.value); if (e.body) walk(e.body); break;
      case "if": walk(e.cond); walk(e.then); walk(e.else); break;
      case "lambda": walk(e.body); break;
      case "apply": walk(e.fn); for (const a of e.args) walk(a); break;
      case "match": walk(e.subject); for (const arm of e.arms) walk(arm.body); break;
      case "list": case "tuple": for (const el of e.elements) walk(el); break;
      case "do": for (const step of e.steps) walk(step.expr); break;
      case "for": walk(e.collection); if (e.guard) walk(e.guard); if (e.fold) walk(e.fold.init); walk(e.body); break;
      case "range": walk(e.start); walk(e.end); break;
      case "pipeline": walk(e.left); walk(e.right); break;
      case "infix": walk(e.left); walk(e.right); break;
      case "unary": walk(e.operand); break;
      case "record": for (const f of e.fields) walk(f.value); break;
      case "record-update": walk(e.base); for (const f of e.fields) walk(f.value); break;
      case "field-access": walk(e.object); break;
      default: break;
    }
  }
  walk(expr);
  return effects;
}

// ── Effect alias registry ──

type EffectAliasEntry = { params: string[]; effects: EffectRef[] };
type EffectAliasMap = Map<string, EffectAliasEntry>;

/** Serialize an EffectRef to a canonical string for deduplication/comparison. */
function effectRefKey(ref: EffectRef): string {
  return ref.args.length > 0 ? `${ref.name}<${ref.args.join(", ")}>` : ref.name;
}

/** Substitute type parameters in an EffectRef list.
 *  E.g., given params=["S"], args=["Int"], and effects=[{name:"state",args:["S"]},{name:"exn",args:[]}],
 *  returns [{name:"state",args:["Int"]},{name:"exn",args:[]}]. */
function substituteEffectRefs(effects: EffectRef[], params: string[], args: string[]): EffectRef[] {
  const subst = new Map<string, string>();
  for (let i = 0; i < params.length && i < args.length; i++) {
    subst.set(params[i], args[i]);
  }
  return effects.map(ref => ({
    name: subst.get(ref.name) ?? ref.name,
    args: ref.args.map(a => subst.get(a) ?? a),
  }));
}

/** Expand a list of effect refs, replacing aliases with their constituent effects.
 *  Performs type parameter substitution for parameterized aliases.
 *  Recursively expands nested aliases. */
function expandEffects(effects: EffectRef[], aliases: EffectAliasMap): EffectRef[] {
  const resultKeys = new Set<string>();
  const result: EffectRef[] = [];
  const seen = new Set<string>(); // prevent infinite recursion
  function expand(ref: EffectRef): void {
    const alias = aliases.get(ref.name);
    if (alias && !seen.has(ref.name)) {
      seen.add(ref.name);
      const substituted = substituteEffectRefs(alias.effects, alias.params, ref.args);
      for (const eff of substituted) expand(eff);
      seen.delete(ref.name);
    } else {
      const key = effectRefKey(ref);
      if (!resultKeys.has(key)) {
        resultKeys.add(key);
        result.push(ref);
      }
    }
  }
  for (const eff of effects) expand(eff);
  return result;
}

// ── Effect subtraction resolution ──

/** Resolve effect subtraction: compute LHS - RHS after alias expansion.
 *  Emits errors for subtracting absent effects or from empty rows. */
function resolveEffectSubtraction(
  effects: EffectRef[],
  subtracted: EffectRef[],
  errors: TypeError[],
  loc: Loc,
): EffectRef[] {
  if (subtracted.length === 0) return effects;
  if (effects.length === 0) {
    for (const eff of subtracted) {
      errors.push({
        code: "E500",
        message: `cannot subtract effect '${effectRefKey(eff)}' from empty row <>`,
        location: loc,
      });
    }
    return [];
  }
  const effectKeySet = new Set(effects.map(effectRefKey));
  for (const eff of subtracted) {
    if (!effectKeySet.has(effectRefKey(eff))) {
      errors.push({
        code: "E501",
        message: `cannot subtract effect '${effectRefKey(eff)}' from row <${effects.map(effectRefKey).join(", ")}> ('${effectRefKey(eff)}' is not present)`,
        location: loc,
      });
    }
  }
  const subtractKeySet = new Set(subtracted.map(effectRefKey));
  return effects.filter(eff => !subtractKeySet.has(effectRefKey(eff)));
}

// ── Entry point ──

export function typeCheck(program: Program): TypeError[] {
  const errors: TypeError[] = [];
  const env = new TypeEnv();
  const registry: VariantRegistry = new Map();
  const opMap: EffectOpMap = new Map();
  const effectAliases: EffectAliasMap = new Map();
  registerBuiltins(env);

  // First pass: register all type declarations, effect aliases, and function signatures
  for (const tl of program.topLevels) {
    if (tl.tag === "mod-decl") continue;
    if (tl.tag === "use-decl") {
      for (const imp of tl.imports) {
        env.set(imp.alias ?? imp.name, tAny);
      }
      continue;
    }
    if (tl.tag === "type-decl") {
      registry.set(tl.name, tl.variants.map(v => v.name));
      for (const v of tl.variants) {
        if (v.fields.length === 0) {
          env.set(v.name, { tag: "t-generic", name: tl.name, args: [] });
        } else {
          let ct: Type = { tag: "t-generic", name: tl.name, args: [] };
          for (let i = v.fields.length - 1; i >= 0; i--) {
            ct = tFn(resolveType(v.fields[i]), ct);
          }
          env.set(v.name, ct);
        }
      }
    } else if (tl.tag === "effect-alias") {
      // Resolve subtraction at alias definition time
      const expandedBase = expandEffects(tl.effects, effectAliases);
      const expandedSub = expandEffects(tl.subtracted, effectAliases);
      const resolved = resolveEffectSubtraction(expandedBase, expandedSub, errors, tl.loc);
      effectAliases.set(tl.name, { params: tl.params, effects: resolved });
    } else if (tl.tag === "effect-decl") {
      env.set(tl.name, { tag: "t-generic", name: "effect", args: [] });
      for (const op of tl.ops) {
        const paramType = op.sig.params.length > 0
          ? resolveType(op.sig.params[0].type)
          : tUnit;
        const retType = resolveType(op.sig.returnType);
        env.set(op.name, tFn(paramType, retType));
        opMap.set(op.name, tl.name);
      }
    } else if (tl.tag === "definition") {
      const retType = resolveType(tl.sig.returnType);
      let fnType: Type;
      if (tl.sig.params.length === 0) {
        fnType = tFn(tUnit, retType);
      } else {
        fnType = retType;
        for (let i = tl.sig.params.length - 1; i >= 0; i--) {
          fnType = tFn(resolveType(tl.sig.params[i].type), fnType);
        }
      }
      env.set(tl.name, fnType);
    }
  }

  // Second pass: check function bodies against their signatures
  for (const tl of program.topLevels) {
    if (tl.tag !== "definition") continue;
    const bodyEnv = env.extend();
    for (const p of tl.sig.params) {
      bodyEnv.set(p.name, resolveType(p.type));
    }
    const bodyType = inferExpr(tl.body, bodyEnv, errors, registry);
    const expectedRet = resolveType(tl.sig.returnType);
    // When return type is Unit, allow any body type — result is discarded (statement position)
    const unitReturn = expectedRet.tag === "t-primitive" && expectedRet.name === "unit";
    if (!unitReturn && !typeEqual(bodyType, expectedRet) && !numCompatible(bodyType, expectedRet)) {
      errors.push({
        code: "E307",
        message: `function '${tl.name}' returns ${showType(bodyType)}, expected ${showType(expectedRet)}`,
        location: tl.loc,
      });
    }
    // Effect subtraction resolution: expand aliases, then subtract
    const expandedEffects = expandEffects(tl.sig.effects, effectAliases);
    const expandedSubtracted = expandEffects(tl.sig.subtracted, effectAliases);
    const resolvedEffects = resolveEffectSubtraction(expandedEffects, expandedSubtracted, errors, tl.loc);

    // Effect checking: warn on unhandled effects not declared in signature
    const bodyEffects = collectEffects(tl.body, opMap, new Set());
    const declaredEffects = new Set(resolvedEffects.map(r => r.name));
    for (const eff of bodyEffects) {
      if (!declaredEffects.has(eff)) {
        errors.push({
          code: "W401",
          message: `function '${tl.name}' performs effect '${eff}' not declared in signature`,
          location: tl.loc,
        });
      }
    }
  }

  return errors;
}
