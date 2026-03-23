// Clank bidirectional type checker
// Validates programs after parsing/desugaring, before evaluation.
// Produces structured JSON type errors.

import type { EffectRef, Expr, Loc, Pattern, Program, TypeExpr } from "./ast.js";
import type { Type, Effect } from "./types.js";
import { tInt, tRat, tBool, tStr, tUnit, tFn, tList, tTuple, tRecord, eNamed, freshVar, resetVarCounter } from "./types.js";
import { BUILTIN_REGISTRY } from "./builtin-registry.js";
import { checkLiteral, checkSubrefinement } from "./solver.js";

// ── Integer literal extraction (handles unary negation) ──

function intLiteralValue(expr: Expr): number | null {
  if (expr.tag === "literal" && expr.value.tag === "int") return expr.value.value;
  // After desugaring, -3 becomes apply(negate, [literal(3)])
  if (expr.tag === "apply" && expr.fn.tag === "var" && expr.fn.name === "negate" && expr.args.length === 1) {
    const inner = intLiteralValue(expr.args[0]);
    return inner !== null ? -inner : null;
  }
  // Before desugaring (for completeness)
  if (expr.tag === "unary" && expr.op === "-") {
    const inner = intLiteralValue(expr.operand);
    return inner !== null ? -inner : null;
  }
  return null;
}

// ── Refinement tracking (module-level, set by typeCheck) ──

type FnRefInfo = { paramPreds: (string | null)[]; returnPred: string | null };
let _fnRefInfo: Map<string, FnRefInfo> = new Map();
let _varRefinements: Map<string, string> = new Map();

// ── Row variable substitution for row polymorphism ──

// Maps row variable IDs to their substitution: a list of fields + optional tail row var
type RowSubst = Map<number, { fields: { name: string; type: Type }[]; tail: number | null }>;
let _rowSubst: RowSubst = new Map();

// Maps named row variables (from type annotations like {name: Str | r}) to their numeric IDs
let _namedRowVars: Map<string, number> = new Map();

/** Apply row substitution to a type, resolving row variables. */
function applyRowSubst(t: Type): Type {
  switch (t.tag) {
    case "t-record": {
      const resolvedFields = t.fields.map(f => ({ name: f.name, type: applyRowSubst(f.type) }));
      if (t.rowVar !== null) {
        // Chase the substitution chain
        let allFields = [...resolvedFields];
        let tail: number | null = t.rowVar;
        while (tail !== null && _rowSubst.has(tail)) {
          const sub = _rowSubst.get(tail)!;
          allFields.push(...sub.fields.map(f => ({ name: f.name, type: applyRowSubst(f.type) })));
          tail = sub.tail;
        }
        // Sort by name for canonical form
        allFields.sort((a, b) => a.name.localeCompare(b.name));
        return { tag: "t-record", fields: allFields, rowVar: tail };
      }
      return { tag: "t-record", fields: resolvedFields, rowVar: null };
    }
    case "t-fn": return tFn(applyRowSubst(t.param), applyRowSubst(t.result), t.effects);
    case "t-list": return tList(applyRowSubst(t.element));
    case "t-tuple": return { tag: "t-tuple", elements: t.elements.map(applyRowSubst) };
    default: return t;
  }
}

/** Occurs check: does rowVarId appear in the given type? */
function rowOccursIn(rowVarId: number, t: Type): boolean {
  switch (t.tag) {
    case "t-record":
      if (t.rowVar === rowVarId) return true;
      return t.fields.some(f => rowOccursIn(rowVarId, f.type));
    case "t-fn": return rowOccursIn(rowVarId, t.param) || rowOccursIn(rowVarId, t.result);
    case "t-list": return rowOccursIn(rowVarId, t.element);
    case "t-tuple": return t.elements.some(e => rowOccursIn(rowVarId, e));
    default: return false;
  }
}

/** Freshen row variables in a type — replace all row var IDs with fresh ones.
 *  This is used at call sites to instantiate polymorphic row variables. */
function freshenRowVars(t: Type, mapping?: Map<number, number>): Type {
  const m = mapping ?? new Map<number, number>();
  function freshenRowVar(id: number): number {
    if (!m.has(id)) m.set(id, freshVar());
    return m.get(id)!;
  }
  function go(ty: Type): Type {
    switch (ty.tag) {
      case "t-record": {
        const fields = ty.fields.map(f => ({ name: f.name, type: go(f.type) }));
        const rowVar = ty.rowVar !== null ? freshenRowVar(ty.rowVar) : null;
        return tRecord(fields, rowVar);
      }
      case "t-fn": return tFn(go(ty.param), go(ty.result), ty.effects);
      case "t-list": return { tag: "t-list", element: go(ty.element) };
      case "t-tuple": return { tag: "t-tuple", elements: ty.elements.map(go) };
      default: return ty;
    }
  }
  return go(t);
}

/** Unify two record types with row polymorphism. Returns true on success, false on failure. */
function unifyRecords(
  a: Extract<Type, { tag: "t-record" }>,
  b: Extract<Type, { tag: "t-record" }>,
  errors: TypeError[],
  loc: Loc,
): boolean {
  const aResolved = applyRowSubst(a) as Extract<Type, { tag: "t-record" }>;
  const bResolved = applyRowSubst(b) as Extract<Type, { tag: "t-record" }>;

  const aFields = new Map(aResolved.fields.map(f => [f.name, f.type]));
  const bFields = new Map(bResolved.fields.map(f => [f.name, f.type]));

  // Fields present in both — unify their types
  for (const [name, aType] of aFields) {
    const bType = bFields.get(name);
    if (bType !== undefined) {
      if (!typeEqual(aType, bType) && !numCompatible(aType, bType)) {
        errors.push({
          code: "E302",
          message: `field type mismatch for "${name}": expected ${showType(aType)}, got ${showType(bType)}`,
          location: loc,
        });
        return false;
      }
    }
  }

  // Fields only in a (not in b)
  const onlyInA = aResolved.fields.filter(f => !bFields.has(f.name));
  // Fields only in b (not in a)
  const onlyInB = bResolved.fields.filter(f => !aFields.has(f.name));

  // If b has extra fields that a doesn't have:
  if (onlyInB.length > 0) {
    if (aResolved.rowVar !== null) {
      // Instantiate a's row var to include b's extra fields
      if (rowOccursIn(aResolved.rowVar, b)) {
        errors.push({ code: "E301", message: `infinite record type (occurs check)`, location: loc });
        return false;
      }
      const newTail = bResolved.rowVar !== null ? bResolved.rowVar : null;
      _rowSubst.set(aResolved.rowVar, { fields: onlyInB, tail: newTail });
    } else {
      // Closed record — extra fields not allowed
      const extra = onlyInB.map(f => f.name).join(", ");
      errors.push({
        code: "E303",
        message: `record has extra fields: ${extra}`,
        location: loc,
      });
      return false;
    }
  }

  // If a has extra fields that b doesn't have:
  if (onlyInA.length > 0) {
    if (bResolved.rowVar !== null) {
      if (rowOccursIn(bResolved.rowVar, a)) {
        errors.push({ code: "E301", message: `infinite record type (occurs check)`, location: loc });
        return false;
      }
      const newTail = aResolved.rowVar !== null ? aResolved.rowVar : null;
      _rowSubst.set(bResolved.rowVar, { fields: onlyInA, tail: newTail });
    } else {
      const missing = onlyInA.map(f => f.name).join(", ");
      errors.push({
        code: "E301",
        message: `record missing required field(s): ${missing}`,
        location: loc,
      });
      return false;
    }
  }

  // If no extra fields on either side, unify the tails
  if (onlyInA.length === 0 && onlyInB.length === 0) {
    if (aResolved.rowVar !== null && bResolved.rowVar !== null && aResolved.rowVar !== bResolved.rowVar) {
      _rowSubst.set(aResolved.rowVar, { fields: [], tail: bResolved.rowVar });
    }
  }

  return true;
}

// Constraint info stored per function for call-site checking
type FnConstraintInfo = {
  constraints: import("./ast.js").Constraint[];
  paramTypeExprs: import("./ast.js").TypeExpr[];
};
let _fnConstraints: Map<string, FnConstraintInfo> = new Map();
// Impl registry reference for call-site constraint checking (set by typeCheck)
let _implRegistry: Map<string, { interface_: string; forType: string; loc: Loc }[]> = new Map();

// ── Type errors ──

export type TypeError = {
  code: string;
  message: string;
  location: Loc;
};

// Sentinel for "unknown/any" — permissive in all comparisons
const tAny: Type = { tag: "t-generic", name: "?", args: [] };

// ── Affine type tracking ──

type AffineTypeSet = Set<string>; // names of types declared `affine`
type ImplEntry = { interface_: string; forType: string; typeArgs: string[]; loc: Loc };
type ImplRegistry = Map<string, ImplEntry[]>; // interface name → list of impls

const PRIMITIVE_CANONICAL: Record<string, string> = { int: "Int", rat: "Rat", bool: "Bool", str: "Str", unit: "Unit" };

function canonicalTypeName(t: Type): string | null {
  switch (t.tag) {
    case "t-primitive": return PRIMITIVE_CANONICAL[t.name] ?? null;
    case "t-generic": return t.name === "?" ? null : t.name;
    case "t-list": {
      const inner = canonicalTypeName(t.element);
      return inner !== null ? `[${inner}]` : null;
    }
    case "t-tuple": {
      const parts = t.elements.map(canonicalTypeName);
      return parts.every(p => p !== null) ? `(${parts.join(", ")})` : null;
    }
    case "t-record": {
      if (t.rowVar !== null) return null; // open records don't have a single canonical name
      const parts = t.fields.map(f => {
        const ft = canonicalTypeName(f.type);
        return ft !== null ? `${f.name}: ${ft}` : null;
      });
      return parts.every(p => p !== null) ? `{${parts.join(", ")}}` : null;
    }
    default: return null;
  }
}

function isAffine(t: Type, affineTypes: AffineTypeSet): boolean {
  switch (t.tag) {
    case "t-generic": return affineTypes.has(t.name);
    case "t-list": return isAffine(t.element, affineTypes);
    case "t-tuple": return t.elements.some(e => isAffine(e, affineTypes));
    case "t-record": return t.fields.some(f => isAffine(f.type, affineTypes));
    default: return false;
  }
}

// Tracks affine variable consumption through control flow
class AffineCtx {
  readonly affineTypes: AffineTypeSet;
  readonly cloneableTypes: Set<string>;
  readonly impls: ImplRegistry;
  // Which variables are affine (name → definition location)
  private affineBindings: Map<string, Loc> = new Map();
  // Which affine variables have been consumed (name → consumption location)
  private consumed: Map<string, Loc> = new Map();

  constructor(affineTypes: AffineTypeSet, cloneableTypes: Set<string>, impls: ImplRegistry) {
    this.affineTypes = affineTypes;
    this.cloneableTypes = cloneableTypes;
    this.impls = impls;
  }

  implementsClone(typeName: string): boolean {
    if (this.cloneableTypes.has(typeName)) return true;
    const cloneImpls = this.impls.get("Clone");
    return cloneImpls !== undefined && cloneImpls.some(e => e.forType === typeName);
  }

  isTypeCloneable(t: Type): boolean {
    switch (t.tag) {
      case "t-list": return this.isTypeCloneable(t.element);
      case "t-tuple": return t.elements.every(e => this.isTypeCloneable(e));
      case "t-record": return t.fields.every(f => this.isTypeCloneable(f.type));
      default: {
        const name = canonicalTypeName(t);
        return name !== null && this.implementsClone(name);
      }
    }
  }

  registerAffine(name: string, defLoc: Loc): void {
    this.affineBindings.set(name, defLoc);
  }

  isAffineVar(name: string): boolean {
    return this.affineBindings.has(name);
  }

  // Consume an affine variable. Returns error if already consumed.
  consume(name: string, useLoc: Loc, errors: TypeError[]): void {
    if (!this.affineBindings.has(name)) return;
    const prev = this.consumed.get(name);
    if (prev) {
      errors.push({
        code: "E600",
        message: `affine variable '${name}' used after move (first consumed at ${prev.line}:${prev.col})`,
        location: useLoc,
      });
    } else {
      this.consumed.set(name, useLoc);
    }
  }

  isConsumed(name: string): boolean {
    return this.consumed.has(name);
  }

  // Snapshot consumed set for branch checking
  snapshot(): Map<string, Loc> {
    return new Map(this.consumed);
  }

  restore(snap: Map<string, Loc>): void {
    this.consumed = new Map(snap);
  }

  // Get names consumed in current state but not in snapshot
  consumedSince(snap: Map<string, Loc>): Set<string> {
    const result = new Set<string>();
    for (const name of this.consumed.keys()) {
      if (!snap.has(name)) result.add(name);
    }
    return result;
  }

  // Check that branches consumed the same affine variables
  checkBranchConsistency(
    preBranch: Map<string, Loc>,
    branchConsumed: Set<string>[],
    loc: Loc,
    errors: TypeError[],
  ): void {
    if (branchConsumed.length < 2) return;
    // Find union of all consumed across branches
    const allConsumed = new Set<string>();
    for (const bc of branchConsumed) for (const n of bc) allConsumed.add(n);

    for (const name of allConsumed) {
      const branches = branchConsumed.map(bc => bc.has(name));
      if (branches.some(b => b) && branches.some(b => !b)) {
        errors.push({
          code: "E601",
          message: `affine variable '${name}' consumed in some branches but not others`,
          location: loc,
        });
      }
    }
    // After branch checking, merge: consumed = preBranch ∪ allConsumed
    this.restore(preBranch);
    for (const name of allConsumed) {
      if (!this.consumed.has(name)) {
        this.consumed.set(name, loc);
      }
    }
  }

  // Warn on unconsumed affine variables introduced in a scope
  checkUnconsumed(scopeVars: string[], loc: Loc, errors: TypeError[]): void {
    for (const name of scopeVars) {
      if (this.affineBindings.has(name) && !this.consumed.has(name)) {
        errors.push({
          code: "W600",
          message: `affine variable '${name}' is never consumed (potential resource leak)`,
          location: loc,
        });
      }
    }
  }
}

// ── Type environment ──

class TypeEnv {
  private bindings: Map<string, Type>;
  private constraintAliases: Map<string, FnConstraintInfo>;
  private parent: TypeEnv | null;

  constructor(parent: TypeEnv | null = null) {
    this.bindings = new Map();
    this.constraintAliases = new Map();
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

  getConstraint(name: string): FnConstraintInfo | undefined {
    const c = this.constraintAliases.get(name);
    if (c !== undefined) return c;
    if (this.parent) return this.parent.getConstraint(name);
    return undefined;
  }

  setConstraint(name: string, info: FnConstraintInfo): void {
    this.constraintAliases.set(name, info);
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
    case "t-record": {
      const fields = te.fields.map(f => ({ name: f.name, type: resolveType(f.type) }));
      if (te.rowVar !== null) {
        // Open record type with named row variable — allocate a fresh row var ID
        // Track named row vars so the same name maps to the same ID within a signature
        const rv = _namedRowVars.get(te.rowVar) ?? freshVar();
        _namedRowVars.set(te.rowVar, rv);
        return tRecord(fields, rv);
      }
      return tRecord(fields);
    }
    case "t-union": return tAny;
    case "t-refined": return resolveType(te.base);
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
    case "t-record": {
      const resolved = applyRowSubst(t) as Extract<Type, { tag: "t-record" }>;
      const fieldStr = resolved.fields.map(f => `${f.name}: ${showType(f.type)}`).join(", ");
      if (resolved.rowVar !== null) {
        return fieldStr ? `{${fieldStr} | r${resolved.rowVar}}` : `{r${resolved.rowVar}}`;
      }
      return `{${fieldStr}}`;
    }
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
    case "t-record": {
      const br = b as typeof a;
      const aRes = applyRowSubst(a) as Extract<Type, { tag: "t-record" }>;
      const bRes = applyRowSubst(br) as Extract<Type, { tag: "t-record" }>;
      // If either is open, be permissive on width — just check shared fields
      const aMap = new Map(aRes.fields.map(f => [f.name, f.type]));
      const bMap = new Map(bRes.fields.map(f => [f.name, f.type]));
      // Check fields present in both match
      for (const [name, aType] of aMap) {
        const bType = bMap.get(name);
        if (bType !== undefined && !typeEqual(aType, bType)) return false;
      }
      // If both closed, must have same fields
      if (aRes.rowVar === null && bRes.rowVar === null) {
        if (aRes.fields.length !== bRes.fields.length) return false;
        for (const [name] of bMap) {
          if (!aMap.has(name)) return false;
        }
      }
      return true;
    }
    default: return true;
  }
}

// Numeric coercion: Int and Rat are compatible in arithmetic
function numCompatible(a: Type, b: Type): boolean {
  const isNum = (t: Type) => t.tag === "t-primitive" && (t.name === "int" || t.name === "rat");
  return isNum(a) && isNum(b);
}

// ── Extract type parameter bindings from argument types ──

/** Given a TypeExpr (from a function signature) and a concrete Type (inferred at call site),
 *  extract bindings for type parameters. E.g., if paramExpr is [T] and argType is [Int],
 *  we get { T: "Int" }. */
function extractTypeBindings(
  paramExpr: TypeExpr,
  argType: Type,
  bindings: Map<string, string>,
): void {
  switch (paramExpr.tag) {
    case "t-name": {
      const name = paramExpr.name;
      if (argType.tag === "t-primitive") {
        const concreteName = argType.name.charAt(0).toUpperCase() + argType.name.slice(1);
        if (!bindings.has(name)) bindings.set(name, concreteName);
      } else if (argType.tag === "t-generic" && argType.name !== "?") {
        if (!bindings.has(name)) bindings.set(name, argType.name);
      }
      break;
    }
    case "t-list":
      if (argType.tag === "t-list") extractTypeBindings(paramExpr.element, argType.element, bindings);
      break;
    case "t-tuple":
      if (argType.tag === "t-tuple") {
        for (let i = 0; i < Math.min(paramExpr.elements.length, argType.elements.length); i++) {
          extractTypeBindings(paramExpr.elements[i], argType.elements[i], bindings);
        }
      }
      break;
    case "t-fn":
      if (argType.tag === "t-fn") {
        extractTypeBindings(paramExpr.param, argType.param, bindings);
        extractTypeBindings(paramExpr.result, argType.result, bindings);
      }
      break;
    case "t-generic":
      if (argType.tag === "t-generic") {
        for (let i = 0; i < Math.min(paramExpr.args.length, argType.args.length); i++) {
          extractTypeBindings(paramExpr.args[i], argType.args[i], bindings);
        }
      }
      break;
    case "t-refined":
      extractTypeBindings(paramExpr.base, argType, bindings);
      break;
  }
}

/** Check whether an impl of the given interface exists for the given type name. */
function hasImpl(interfaceName: string, typeName: string): boolean {
  const entries = _implRegistry.get(interfaceName);
  if (!entries) return false;
  return entries.some(e => e.forType === typeName);
}

/** Resolve constraint info for a callee expression (handles direct names and aliases). */
function resolveConstraintInfo(expr: Expr, env: TypeEnv): FnConstraintInfo | undefined {
  if (expr.tag === "var") {
    return _fnConstraints.get(expr.name) ?? env.getConstraint(expr.name);
  }
  return undefined;
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

function inferExpr(expr: Expr, env: TypeEnv, errors: TypeError[], registry: VariantRegistry, aff: AffineCtx): Type {
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
      // Affine tracking: consuming an affine variable
      if (aff.isAffineVar(expr.name)) {
        aff.consume(expr.name, expr.loc, errors);
      }
      return t;
    }

    case "let": {
      const valType = inferExpr(expr.value, env, errors, registry, aff);
      if (expr.body === null) return valType;
      if (expr.name === "_") return inferExpr(expr.body, env, errors, registry, aff);
      const child = env.extend();
      child.set(expr.name, valType);
      // Propagate where-constraint info through let bindings
      const cInfo = resolveConstraintInfo(expr.value, env);
      if (cInfo) child.setConstraint(expr.name, cInfo);
      // Register affine binding if value type is affine
      if (isAffine(valType, aff.affineTypes)) {
        aff.registerAffine(expr.name, expr.loc);
      }
      const bodyType = inferExpr(expr.body, child, errors, registry, aff);
      // Warn if affine binding was never consumed
      if (isAffine(valType, aff.affineTypes) && expr.name !== "_") {
        aff.checkUnconsumed([expr.name], expr.loc, errors);
      }
      return bodyType;
    }

    case "if": {
      const condType = inferExpr(expr.cond, env, errors, registry, aff);
      if (condType.tag === "t-primitive" && condType.name !== "bool") {
        errors.push({ code: "E301", message: `if condition must be Bool, got ${showType(condType)}`, location: expr.loc });
      }
      // Branch consistency: both branches must consume the same affine vars
      const preBranch = aff.snapshot();
      const thenType = inferExpr(expr.then, env, errors, registry, aff);
      const thenConsumed = aff.consumedSince(preBranch);

      aff.restore(preBranch);
      const elseType = inferExpr(expr.else, env, errors, registry, aff);
      const elseConsumed = aff.consumedSince(preBranch);

      aff.checkBranchConsistency(preBranch, [thenConsumed, elseConsumed], expr.loc, errors);

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
        if (isAffine(pt, aff.affineTypes)) {
          aff.registerAffine(p.name, expr.loc);
        }
      }
      const retType = inferExpr(expr.body, lamEnv, errors, registry, aff);
      let fnType: Type = retType;
      for (let i = paramTypes.length - 1; i >= 0; i--) {
        fnType = tFn(paramTypes[i], fnType);
      }
      return fnType;
    }

    case "apply": {
      const rawFnType = inferExpr(expr.fn, env, errors, registry, aff);
      // Freshen row variables so each call site gets independent row vars
      const fnType = freshenRowVars(rawFnType);
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
        const argTypes: Type[] = [];
        for (let i = 0; i < Math.min(expr.args.length, paramTypes.length); i++) {
          const argType = inferExpr(expr.args[i], env, errors, registry, aff);
          argTypes.push(argType);
          const resolvedArg = applyRowSubst(argType);
          const resolvedParam = applyRowSubst(paramTypes[i]);
          // Use row unification for record types to support row polymorphism
          if (resolvedArg.tag === "t-record" && resolvedParam.tag === "t-record") {
            unifyRecords(resolvedParam, resolvedArg, errors, expr.args[i].loc);
          } else if (!typeEqual(argType, paramTypes[i]) && !numCompatible(argType, paramTypes[i])) {
            errors.push({
              code: "E304",
              message: `argument ${i + 1}: expected ${showType(paramTypes[i])}, got ${showType(argType)}`,
              location: expr.args[i].loc,
            });
          }
        }
        // Refinement checking at call sites
        if (expr.fn.tag === "var") {
          const refInfo = _fnRefInfo.get(expr.fn.name);
          if (refInfo) {
            for (let i = 0; i < Math.min(expr.args.length, refInfo.paramPreds.length); i++) {
              const reqPred = refInfo.paramPreds[i];
              if (!reqPred) continue;
              const arg = expr.args[i];
              const litVal = intLiteralValue(arg);
              if (litVal !== null) {
                if (!checkLiteral(litVal, reqPred)) {
                  errors.push({
                    code: "E310",
                    message: `refinement not satisfied: ${litVal} does not satisfy {${reqPred}}`,
                    location: arg.loc,
                  });
                }
              } else if (arg.tag === "var") {
                const knownPred = _varRefinements.get(arg.name);
                if (knownPred) {
                  const result = checkSubrefinement(knownPred, reqPred);
                  if (result === "invalid") {
                    errors.push({
                      code: "E310",
                      message: `refinement not satisfied: {${knownPred}} does not imply {${reqPred}}`,
                      location: arg.loc,
                    });
                  }
                }
              }
            }
          }
        }
        // Where-constraint checking at call sites (direct and indirect/higher-order)
        {
          const cInfo = resolveConstraintInfo(expr.fn, env);
          if (cInfo && cInfo.constraints.length > 0) {
            // Extract type parameter bindings from argument types
            const bindings = new Map<string, string>();
            for (let i = 0; i < Math.min(argTypes.length, cInfo.paramTypeExprs.length); i++) {
              extractTypeBindings(cInfo.paramTypeExprs[i], argTypes[i], bindings);
            }
            // Check each constraint
            for (const c of cInfo.constraints) {
              const concreteType = bindings.get(c.typeParam);
              if (concreteType && concreteType !== "?") {
                if (!hasImpl(c.interface_, concreteType)) {
                  errors.push({
                    code: "E205",
                    message: `where constraint not satisfied: '${concreteType}' does not implement '${c.interface_}'`,
                    location: expr.loc,
                  });
                }
              }
            }
          }
        }
        return cur;
      }
      // Non-function type being called — infer args for nested checking, return unknown
      for (const arg of expr.args) inferExpr(arg, env, errors, registry, aff);
      return tAny;
    }

    case "match": {
      const subjectType = inferExpr(expr.subject, env, errors, registry, aff);
      let resultType: Type | null = null;
      const preBranch = aff.snapshot();
      const branchConsumed: Set<string>[] = [];
      for (const arm of expr.arms) {
        aff.restore(preBranch);
        const armEnv = env.extend();
        bindPatternVars(arm.pattern, armEnv);
        const armType = inferExpr(arm.body, armEnv, errors, registry, aff);
        branchConsumed.push(aff.consumedSince(preBranch));
        if (resultType === null) {
          resultType = armType;
        } else if (!typeEqual(resultType, armType) && !numCompatible(resultType, armType)) {
          errors.push({ code: "E305", message: `match arms have inconsistent types: ${showType(resultType)} vs ${showType(armType)}`, location: expr.loc });
        }
      }
      if (branchConsumed.length >= 2) {
        aff.checkBranchConsistency(preBranch, branchConsumed, expr.loc, errors);
      }
      checkMatchExhaustiveness(expr, subjectType, registry, errors);
      return resultType ?? tAny;
    }

    case "list": {
      if (expr.elements.length === 0) return tList(tAny);
      const elemType = inferExpr(expr.elements[0], env, errors, registry, aff);
      for (let i = 1; i < expr.elements.length; i++) {
        const et = inferExpr(expr.elements[i], env, errors, registry, aff);
        if (!typeEqual(elemType, et) && !numCompatible(elemType, et)) {
          errors.push({ code: "E306", message: `list element ${i + 1} has type ${showType(et)}, expected ${showType(elemType)}`, location: expr.elements[i].loc });
        }
      }
      return tList(elemType);
    }

    case "tuple":
      return tTuple(expr.elements.map(e => inferExpr(e, env, errors, registry, aff)));

    case "handle": {
      inferExpr(expr.expr, env, errors, registry, aff);
      let resultType: Type = tAny;
      for (const arm of expr.arms) {
        const armEnv = env.extend();
        for (const p of arm.params) armEnv.set(p.name, tAny);
        if (arm.resumeName) armEnv.set(arm.resumeName, tFn(tAny, tAny));
        const armType = inferExpr(arm.body, armEnv, errors, registry, aff);
        // The return arm determines the overall handle type
        if (arm.name === "return") resultType = armType;
      }
      return resultType;
    }

    case "perform":
      return inferExpr(expr.expr, env, errors, registry, aff);

    // Borrow: &x — read-only access without consuming
    case "borrow": {
      const innerType = inferExpr(expr.expr, env, errors, registry, aff);
      // Undo the consumption that inferExpr(expr.expr) may have done
      // if the inner expr is a variable, un-consume it (borrow doesn't consume)
      if (expr.expr.tag === "var" && aff.isAffineVar(expr.expr.name)) {
        // Borrow should not consume — restore the var
        // We do this by checking if it was consumed by this expr and undoing
        if (aff.isConsumed(expr.expr.name)) {
          // Snapshot and check: if this was the consumption, undo it
          // Simple approach: borrows un-consume
          aff.restore((() => {
            const snap = aff.snapshot();
            snap.delete(expr.expr.name);
            return snap;
          })());
        }
      }
      return innerType;
    }

    // Clone: clone expr — explicitly duplicate (expr should be a borrow)
    case "clone": {
      const innerType = inferExpr(expr.expr, env, errors, registry, aff);
      const typeName = canonicalTypeName(innerType);
      if (typeName && !aff.isTypeCloneable(innerType)) {
        errors.push({
          code: "E602",
          message: `type '${typeName}' does not implement Clone`,
          location: expr.loc,
        });
      }
      return innerType;
    }

    // Discard: discard expr — explicitly abandon an affine value
    case "discard": {
      inferExpr(expr.expr, env, errors, registry, aff);
      return tUnit;
    }

    case "record": {
      // Infer closed record type from literal fields
      const fields: { name: string; type: Type }[] = [];
      const seen = new Set<string>();
      for (const f of expr.fields) {
        if (seen.has(f.name)) {
          errors.push({
            code: "E304",
            message: `duplicate field "${f.name}" in record literal`,
            location: expr.loc,
          });
        }
        seen.add(f.name);
        const ft = inferExpr(f.value, env, errors, registry, aff);
        fields.push({ name: f.name, type: ft });
      }
      return tRecord(fields);
    }

    case "field-access": {
      // Try dotted builtin name first (e.g., "str.cat", "tuple.get")
      if (expr.object.tag === "var") {
        const dottedName = `${expr.object.name}.${expr.field}`;
        const dottedType = env.get(dottedName);
        if (dottedType) return dottedType;
      }
      const objType = inferExpr(expr.object, env, errors, registry, aff);
      const resolved = applyRowSubst(objType);
      if (resolved.tag === "t-record") {
        const field = resolved.fields.find(f => f.name === expr.field);
        if (field) {
          return applyRowSubst(field.type);
        }
        // Field not found in known fields — check if open record
        if (resolved.rowVar !== null) {
          // The record is open; the field might be in the unknown part.
          // Create a fresh type variable for the field's type and constrain the row var.
          // Use tAny as field type (we don't have full HM unification for type vars yet)
          const fieldType = tAny;
          const newTail = freshVar();
          _rowSubst.set(resolved.rowVar, {
            fields: [{ name: expr.field, type: fieldType }],
            tail: newTail,
          });
          return fieldType;
        }
        errors.push({
          code: "E301",
          message: `record missing required field "${expr.field}" — record type is ${showType(resolved)}`,
          location: expr.loc,
        });
        return tAny;
      }
      // Not a record type (or tAny) — permissive fallback
      return tAny;
    }

    case "record-update": {
      const baseType = inferExpr(expr.base, env, errors, registry, aff);
      const resolved = applyRowSubst(baseType);
      if (resolved.tag === "t-record") {
        const baseFields = new Map(resolved.fields.map(f => [f.name, f.type]));
        for (const f of expr.fields) {
          const valType = inferExpr(f.value, env, errors, registry, aff);
          const existingType = baseFields.get(f.name);
          if (existingType === undefined) {
            if (resolved.rowVar === null) {
              errors.push({
                code: "E301",
                message: `cannot update field "${f.name}" — not present in record type ${showType(resolved)}`,
                location: expr.loc,
              });
            }
            // For open records, the field might be in the row var — permissive
          } else {
            if (!typeEqual(existingType, valType) && !numCompatible(existingType, valType)) {
              errors.push({
                code: "E302",
                message: `field "${f.name}" update type mismatch: expected ${showType(existingType)}, got ${showType(valType)}`,
                location: expr.loc,
              });
            }
          }
        }
        // Return same type as base (update preserves type)
        return resolved;
      }
      // Not a record — infer fields anyway for error checking
      for (const f of expr.fields) inferExpr(f.value, env, errors, registry, aff);
      return tAny;
    }

    case "pipeline": case "infix": case "unary": case "do": case "for": case "range":
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
      case "borrow": case "clone": case "discard": walk(e.expr); break;
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

// ── Interface and impl registries ──

type InterfaceEntry = {
  name: string;
  typeParams: string[];
  supers: string[];
  methods: { name: string; sig: import("./ast.js").TypeSig }[];
};

type InterfaceRegistry = Map<string, InterfaceEntry>;

function typeExprName(te: import("./ast.js").TypeExpr): string {
  switch (te.tag) {
    case "t-name": return te.name;
    case "t-list": return `[${typeExprName(te.element)}]`;
    case "t-tuple": return `(${te.elements.map(typeExprName).join(", ")})`;
    case "t-generic": return te.args.length > 0 ? `${te.name}<${te.args.map(typeExprName).join(", ")}>` : te.name;
    default: return "?";
  }
}

const BUILTIN_INTERFACES: InterfaceEntry[] = [
  { name: "Clone", typeParams: [], supers: [], methods: [{ name: "clone", sig: { params: [{ name: "self", type: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } }], effects: [], subtracted: [], returnType: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } } }] },
  { name: "Show", typeParams: [], supers: [], methods: [{ name: "show", sig: { params: [{ name: "self", type: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } }], effects: [], subtracted: [], returnType: { tag: "t-name", name: "Str", loc: { line: 0, col: 0 } } } }] },
  { name: "Eq", typeParams: [], supers: [], methods: [{ name: "eq", sig: { params: [{ name: "a", type: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } }, { name: "b", type: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } }], effects: [], subtracted: [], returnType: { tag: "t-name", name: "Bool", loc: { line: 0, col: 0 } } } }] },
  { name: "Ord", typeParams: [], supers: ["Eq"], methods: [{ name: "cmp", sig: { params: [{ name: "a", type: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } }, { name: "b", type: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } }], effects: [], subtracted: [], returnType: { tag: "t-name", name: "Ordering", loc: { line: 0, col: 0 } } } }] },
  { name: "Default", typeParams: [], supers: [], methods: [{ name: "default", sig: { params: [], effects: [], subtracted: [], returnType: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } } }] },
  { name: "Into", typeParams: ["T"], supers: [], methods: [{ name: "into", sig: { params: [{ name: "self", type: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } }], effects: [], subtracted: [], returnType: { tag: "t-name", name: "T", loc: { line: 0, col: 0 } } } }] },
  { name: "From", typeParams: ["T"], supers: [], methods: [{ name: "from", sig: { params: [{ name: "val", type: { tag: "t-name", name: "T", loc: { line: 0, col: 0 } } }], effects: [], subtracted: [], returnType: { tag: "t-name", name: "Self", loc: { line: 0, col: 0 } } } }] },
];

const PRIMITIVE_IMPLS = ["Int", "Rat", "Bool", "Str", "Unit"];
const DERIVABLE_INTERFACES = new Set(["Clone", "Show", "Eq", "Ord", "Default"]);

// ── Entry point ──

export function typeCheck(program: Program): TypeError[] {
  const errors: TypeError[] = [];
  const env = new TypeEnv();
  const registry: VariantRegistry = new Map();
  const opMap: EffectOpMap = new Map();
  const effectAliases: EffectAliasMap = new Map();
  const interfaces: InterfaceRegistry = new Map();
  const impls: ImplRegistry = new Map();
  const affineTypes: AffineTypeSet = new Set();
  const cloneableTypes: Set<string> = new Set();
  _fnRefInfo = new Map();
  _varRefinements = new Map();
  _fnConstraints = new Map();
  _rowSubst = new Map();
  _namedRowVars = new Map();
  resetVarCounter();
  _implRegistry = impls;
  registerBuiltins(env);

  // Register built-in interfaces
  for (const iface of BUILTIN_INTERFACES) {
    interfaces.set(iface.name, iface);
    impls.set(iface.name, []);
  }
  // Register primitive impls for Clone, Show, Eq, Default
  for (const prim of PRIMITIVE_IMPLS) {
    for (const iface of ["Clone", "Show", "Eq", "Default"]) {
      impls.get(iface)!.push({ interface_: iface, forType: prim, typeArgs: [], loc: { line: 0, col: 0 } });
    }
  }
  // Ord for Int, Rat, Str
  for (const prim of ["Int", "Rat", "Str"]) {
    impls.get("Ord")!.push({ interface_: "Ord", forType: prim, typeArgs: [], loc: { line: 0, col: 0 } });
  }
  // Register built-in interface method names in the type env
  // cmp : (a, a) -> Ordering
  env.set("cmp", tFn(tAny, tFn(tAny, tAny)));
  // clone : (a) -> a
  env.set("clone", tFn(tAny, tAny));
  // default : () -> a  (already a builtin? just ensure it's set)
  env.set("default", tFn(tUnit, tAny));
  // into : (a) -> b  (From/Into conversion)
  env.set("into", tFn(tAny, tAny));
  // from : (a) -> b  (From/Into conversion)
  env.set("from", tFn(tAny, tAny));

  // First pass: register all type declarations, effect aliases, interfaces, impls, and function signatures
  for (const tl of program.topLevels) {
    if (tl.tag === "mod-decl") continue;
    if (tl.tag === "use-decl") {
      for (const imp of tl.imports) {
        env.set(imp.alias ?? imp.name, tAny);
      }
      continue;
    }
    if (tl.tag === "test-decl") continue;
    if (tl.tag === "type-decl") {
      registry.set(tl.name, tl.variants.map(v => v.name));
      // Register affine types
      if (tl.affine) {
        affineTypes.add(tl.name);
      }
      // Register cloneable types from deriving Clone
      if (tl.deriving && tl.deriving.includes("Clone")) {
        cloneableTypes.add(tl.name);
      }
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
      // Process deriving clauses
      if (tl.deriving) {
        for (const ifaceName of tl.deriving) {
          if (!DERIVABLE_INTERFACES.has(ifaceName)) {
            errors.push({ code: "E201", message: `cannot derive '${ifaceName}': not a derivable interface`, location: tl.loc });
            continue;
          }
          if (!interfaces.has(ifaceName)) {
            errors.push({ code: "E201", message: `unknown interface '${ifaceName}'`, location: tl.loc });
            continue;
          }
          if (!impls.has(ifaceName)) impls.set(ifaceName, []);
          impls.get(ifaceName)!.push({ interface_: ifaceName, forType: tl.name, typeArgs: [], loc: tl.loc });
        }
      }
    } else if (tl.tag === "interface-decl") {
      if (interfaces.has(tl.name)) {
        errors.push({ code: "E200", message: `duplicate interface declaration '${tl.name}'`, location: tl.loc });
      } else {
        // Validate superinterfaces exist
        for (const sup of tl.supers) {
          if (!interfaces.has(sup)) {
            errors.push({ code: "E200", message: `unknown superinterface '${sup}' in interface '${tl.name}'`, location: tl.loc });
          }
        }
        interfaces.set(tl.name, { name: tl.name, typeParams: tl.typeParams, supers: tl.supers, methods: tl.methods });
        if (!impls.has(tl.name)) impls.set(tl.name, []);
        // Register method names as functions in the environment
        for (const m of tl.methods) {
          const retType = resolveType(m.sig.returnType);
          let fnType: Type;
          if (m.sig.params.length === 0) {
            fnType = tFn(tUnit, retType);
          } else {
            fnType = retType;
            for (let i = m.sig.params.length - 1; i >= 0; i--) {
              fnType = tFn(resolveType(m.sig.params[i].type), fnType);
            }
          }
          env.set(m.name, fnType);
        }
      }
    } else if (tl.tag === "impl-block") {
      const ifaceName = tl.interface_;
      if (!interfaces.has(ifaceName)) {
        errors.push({ code: "E200", message: `unknown interface '${ifaceName}'`, location: tl.loc });
      } else {
        const forTypeName = typeExprName(tl.forType);
        if (!impls.has(ifaceName)) impls.set(ifaceName, []);
        // Coherence check: no duplicate impls (for parameterized interfaces, type args must also match)
        const existing = impls.get(ifaceName)!;
        const implTypeArgs = tl.typeArgs.map(typeExprName);
        const isDuplicate = existing.some(e =>
          e.forType === forTypeName &&
          e.typeArgs.length === implTypeArgs.length &&
          e.typeArgs.every((a, i) => a === implTypeArgs[i])
        );
        if (isDuplicate) {
          const typeArgStr = implTypeArgs.length > 0 ? `<${implTypeArgs.join(", ")}>` : "";
          errors.push({ code: "E202", message: `duplicate impl of '${ifaceName}${typeArgStr}' for '${forTypeName}' (coherence violation)`, location: tl.loc });
        } else {
          existing.push({ interface_: ifaceName, forType: forTypeName, typeArgs: implTypeArgs, loc: tl.loc });
        }
        // Check all methods are provided
        const iface = interfaces.get(ifaceName)!;
        const provided = new Set(tl.methods.map(m => m.name));
        for (const m of iface.methods) {
          if (!provided.has(m.name)) {
            errors.push({ code: "E203", message: `impl of '${ifaceName}' for '${forTypeName}' is missing method '${m.name}'`, location: tl.loc });
          }
        }
        // Check for extra methods
        const expected = new Set(iface.methods.map(m => m.name));
        for (const m of tl.methods) {
          if (!expected.has(m.name)) {
            errors.push({ code: "E203", message: `impl of '${ifaceName}' for '${forTypeName}' provides unexpected method '${m.name}'`, location: tl.loc });
          }
        }
        // Check superinterface impls exist
        for (const sup of iface.supers) {
          const supImpls = impls.get(sup);
          if (!supImpls || !supImpls.some(e => e.forType === forTypeName)) {
            errors.push({ code: "E204", message: `impl of '${ifaceName}' for '${forTypeName}' requires impl of superinterface '${sup}'`, location: tl.loc });
          }
        }
        // Blanket rule: impl From<A> for B  →  impl Into<B> for A
        if (ifaceName === "From" && tl.typeArgs.length > 0) {
          const sourceType = typeExprName(tl.typeArgs[0]);
          if (!impls.has("Into")) impls.set("Into", []);
          const intoImpls = impls.get("Into")!;
          if (!intoImpls.some(e => e.forType === sourceType)) {
            intoImpls.push({ interface_: "Into", forType: sourceType, typeArgs: [forTypeName], loc: tl.loc });
          }
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
      _namedRowVars = new Map(); // Reset per-definition row variable scope
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
      // Extract refinement info for function parameters and return type
      const paramPreds = tl.sig.params.map(p =>
        p.type.tag === "t-refined" ? p.type.predicate : null
      );
      const returnPred = tl.sig.returnType.tag === "t-refined"
        ? tl.sig.returnType.predicate : null;
      if (paramPreds.some(p => p !== null) || returnPred !== null) {
        _fnRefInfo.set(tl.name, { paramPreds, returnPred });
      }
      // Register where-constraints for call-site checking
      if (tl.constraints.length > 0) {
        _fnConstraints.set(tl.name, {
          constraints: tl.constraints,
          paramTypeExprs: tl.sig.params.map(p => p.type),
        });
      }
    }
  }

  // Second pass: check function bodies against their signatures
  for (const tl of program.topLevels) {
    if (tl.tag !== "definition") continue;
    const bodyEnv = env.extend();
    const aff = new AffineCtx(affineTypes, cloneableTypes, impls);
    const affineParams: string[] = [];
    // Set up refinement environment for this function body
    _varRefinements = new Map();
    for (const p of tl.sig.params) {
      const pt = resolveType(p.type);
      bodyEnv.set(p.name, pt);
      if (isAffine(pt, affineTypes)) {
        aff.registerAffine(p.name, tl.loc);
        affineParams.push(p.name);
      }
      if (p.type.tag === "t-refined") {
        _varRefinements.set(p.name, p.type.predicate);
      }
    }
    const bodyType = inferExpr(tl.body, bodyEnv, errors, registry, aff);
    // Check that affine parameters were consumed
    aff.checkUnconsumed(affineParams, tl.loc, errors);
    const expectedRet = resolveType(tl.sig.returnType);
    // When return type is Unit, allow any body type — result is discarded (statement position)
    const unitReturn = expectedRet.tag === "t-primitive" && expectedRet.name === "unit";
    if (!unitReturn) {
      const resolvedBody = applyRowSubst(bodyType);
      const resolvedExpected = applyRowSubst(expectedRet);
      if (resolvedBody.tag === "t-record" && resolvedExpected.tag === "t-record") {
        unifyRecords(resolvedExpected, resolvedBody, errors, tl.loc);
      } else if (!typeEqual(bodyType, expectedRet) && !numCompatible(bodyType, expectedRet)) {
        errors.push({
          code: "E307",
          message: `function '${tl.name}' returns ${showType(bodyType)}, expected ${showType(expectedRet)}`,
          location: tl.loc,
        });
      }
    }
    // Return refinement checking: verify body satisfies return predicate
    if (tl.sig.returnType.tag === "t-refined") {
      const retPred = tl.sig.returnType.predicate;
      const retLitVal = intLiteralValue(tl.body);
      if (retLitVal !== null) {
        if (!checkLiteral(retLitVal, retPred)) {
          errors.push({
            code: "E311",
            message: `return refinement not satisfied: ${retLitVal} does not satisfy {${retPred}}`,
            location: tl.body.loc,
          });
        }
      } else if (tl.body.tag === "var") {
        const knownPred = _varRefinements.get(tl.body.name);
        if (knownPred) {
          const result = checkSubrefinement(knownPred, retPred);
          if (result === "invalid") {
            errors.push({
              code: "E311",
              message: `return refinement not satisfied: {${knownPred}} does not imply {${retPred}}`,
              location: tl.body.loc,
            });
          }
        }
      }
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
