// Clank bidirectional type checker
// Validates programs after parsing/desugaring, before evaluation.
// Produces structured JSON type errors.

import type { EffectRef, Expr, Loc, Pattern, Program, TypeExpr } from "./ast.js";
import type { Type, Effect } from "./types.js";
import { tInt, tRat, tBool, tStr, tUnit, tFn, tList, tTuple, tRecord, tBorrow, tVar, eNamed, freshVar, resetVarCounter } from "./types.js";
import { BUILTIN_REGISTRY } from "./builtin-registry.js";
import { checkLiteral, checkSubrefinement, checkRefinementWithContext, substitutePredVar } from "./solver.js";

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
let _pathConditions: string[] = [];

// ── Refinement context helpers ──

/** Save current refinement state (for branching). */
function saveRefinementState(): { vars: Map<string, string>; paths: string[] } {
  return { vars: new Map(_varRefinements), paths: [..._pathConditions] };
}

/** Restore refinement state (after branch). */
function restoreRefinementState(state: { vars: Map<string, string>; paths: string[] }): void {
  _varRefinements = new Map(state.vars);
  _pathConditions = [...state.paths];
}

/** Collect all assumptions for refinement checking: variable refinements + path conditions. */
function collectAssumptions(targetVar?: string): string[] {
  const assumptions: string[] = [..._pathConditions];
  for (const [name, pred] of _varRefinements) {
    if (name === targetVar) continue; // don't include the target's own refinement as assumption
    // Substitute 'v' with the variable name in the stored predicate
    assumptions.push(substitutePredVar(pred, "v", name));
  }
  return assumptions;
}

/**
 * Try to extract a predicate string from an if-condition expression.
 * After desugaring, comparisons are `apply(gt/lt/gte/lte/eq/neq, [left, right])`.
 * Returns the predicate and its negation, or null if not extractable.
 */
function extractConditionPredicate(expr: Expr): { pos: string; neg: string } | null {
  if (expr.tag !== "apply" || expr.fn.tag !== "var" || expr.args.length !== 2) return null;
  const op = expr.fn.name;
  const cmpOps: Record<string, { sym: string; negSym: string }> = {
    "gt": { sym: ">", negSym: "<=" },
    "lt": { sym: "<", negSym: ">=" },
    "gte": { sym: ">=", negSym: "<" },
    "lte": { sym: "<=", negSym: ">" },
    "eq": { sym: "==", negSym: "!=" },
    "neq": { sym: "!=", negSym: "==" },
  };
  const entry = cmpOps[op];
  if (!entry) return null;
  const leftStr = exprToPredString(expr.args[0]);
  const rightStr = exprToPredString(expr.args[1]);
  if (!leftStr || !rightStr) return null;
  return {
    pos: `${leftStr} ${entry.sym} ${rightStr}`,
    neg: `${leftStr} ${entry.negSym} ${rightStr}`,
  };
}

/**
 * Try to convert an expression to a predicate-language string.
 * Handles: variables, integer literals, arithmetic (add/sub/mul/negate).
 */
function exprToPredString(expr: Expr): string | null {
  switch (expr.tag) {
    case "var": return expr.name;
    case "literal":
      if (expr.value.tag === "int") return String(expr.value.value);
      return null;
    case "apply":
      if (expr.fn.tag !== "var") return null;
      // Unary: negate(x) → (-x)
      if (expr.fn.name === "negate" && expr.args.length === 1) {
        const inner = exprToPredString(expr.args[0]);
        return inner ? `(-${inner})` : null;
      }
      // Binary arithmetic: add(a,b), sub(a,b), mul(a,b)
      if (expr.args.length === 2) {
        const arithOps: Record<string, string> = { "add": "+", "sub": "-", "mul": "*" };
        const opSym = arithOps[expr.fn.name];
        if (!opSym) return null;
        const left = exprToPredString(expr.args[0]);
        const right = exprToPredString(expr.args[1]);
        return left && right ? `(${left} ${opSym} ${right})` : null;
      }
      return null;
    default:
      return null;
  }
}

/**
 * Extract a predicate from a match pattern relative to the subject expression.
 * e.g., pattern `0` with subject `x` → `x == 0`
 */
function patternToPredicate(pat: Pattern, subjectStr: string): string | null {
  switch (pat.tag) {
    case "p-literal":
      if (pat.value.tag === "int") return `${subjectStr} == ${pat.value.value}`;
      return null;
    default:
      return null;
  }
}

/**
 * Check a refinement obligation using full context (path conditions + variable refinements).
 * The argExpr is the expression being checked against reqPred.
 * Returns true if a check was performed (pass or fail), false if we couldn't check.
 */
function checkRefinementObligation(
  argExpr: Expr,
  reqPred: string,
  errorCode: string,
  errorPrefix: string,
  errors: TypeError[],
): boolean {
  // Case 1: integer literal — direct evaluation
  const litVal = intLiteralValue(argExpr);
  if (litVal !== null) {
    if (!checkLiteral(litVal, reqPred)) {
      errors.push({
        code: errorCode,
        message: `${errorPrefix}: ${litVal} does not satisfy {${reqPred}}`,
        location: argExpr.loc,
      });
    }
    return true;
  }
  // Case 2: variable with known refinement
  if (argExpr.tag === "var") {
    const knownPred = _varRefinements.get(argExpr.name);
    if (knownPred) {
      // Use full context: path conditions + other variable refinements
      const assumptions = collectAssumptions();
      assumptions.push(substitutePredVar(knownPred, "v", argExpr.name));
      // Substitute the variable name for 'v' in the required predicate
      const goal = substitutePredVar(reqPred, "v", argExpr.name);
      const result = checkRefinementWithContext(assumptions, goal);
      if (result === "invalid") {
        errors.push({
          code: errorCode,
          message: `${errorPrefix}: {${knownPred}} does not imply {${reqPred}}`,
          location: argExpr.loc,
        });
      }
      return true;
    }
    // Variable without explicit refinement — check path conditions
    if (_pathConditions.length > 0) {
      const assumptions = collectAssumptions();
      const goal = substitutePredVar(reqPred, "v", argExpr.name);
      const result = checkRefinementWithContext(assumptions, goal);
      if (result === "invalid") {
        errors.push({
          code: errorCode,
          message: `${errorPrefix}: path conditions do not imply {${reqPred}}`,
          location: argExpr.loc,
        });
      }
      // If valid or unknown, don't report error
      return result !== "unknown";
    }
    return false;
  }
  // Case 3: arithmetic expression — try to convert to predicate string
  const exprStr = exprToPredString(argExpr);
  if (exprStr) {
    const assumptions = collectAssumptions();
    // Goal: substitute 'v' with the expression string in the required predicate
    const goal = substitutePredVar(reqPred, "v", exprStr);
    const result = checkRefinementWithContext(assumptions, goal);
    if (result === "invalid") {
      errors.push({
        code: errorCode,
        message: `${errorPrefix}: expression does not satisfy {${reqPred}}`,
        location: argExpr.loc,
      });
    }
    return result !== "unknown";
  }
  return false;
}

/**
 * Check return refinement per-branch for if/match bodies.
 * Walks the body structure, setting up branch-specific path conditions
 * before checking each leaf expression against the return predicate.
 */
function checkReturnRefinementPerBranch(
  body: Expr,
  retPred: string,
  errors: TypeError[],
  params: { name: string; type: TypeExpr }[],
): void {
  // Set up parameter refinements (already cleared after inference)
  const saved = saveRefinementState();
  _varRefinements = new Map();
  _pathConditions = [];
  for (const p of params) {
    if (p.type.tag === "t-refined") {
      _varRefinements.set(p.name, p.type.predicate);
    }
  }
  checkReturnBranches(body, retPred, errors);
  restoreRefinementState(saved);
}

function checkReturnBranches(body: Expr, retPred: string, errors: TypeError[]): void {
  if (body.tag === "if") {
    const condPred = extractConditionPredicate(body.cond);
    const preRefState = saveRefinementState();

    // Then branch
    if (condPred) _pathConditions.push(condPred.pos);
    checkReturnBranches(body.then, retPred, errors);
    restoreRefinementState(preRefState);

    // Else branch
    if (condPred) _pathConditions.push(condPred.neg);
    checkReturnBranches(body.else, retPred, errors);
    restoreRefinementState(preRefState);
    return;
  }

  if (body.tag === "match") {
    const subjectStr = exprToPredString(body.subject);
    const preRefState = saveRefinementState();
    for (const arm of body.arms) {
      restoreRefinementState(preRefState);
      if (subjectStr) {
        const patPred = patternToPredicate(arm.pattern, subjectStr);
        if (patPred) _pathConditions.push(patPred);
      }
      checkReturnBranches(arm.body, retPred, errors);
    }
    restoreRefinementState(preRefState);
    return;
  }

  // Leaf expression: check refinement obligation
  checkRefinementObligation(body, retPred, "E311", "return refinement not satisfied", errors);
}

// ── Row variable substitution for row polymorphism ──

// Maps row variable IDs to their substitution: a list of fields + optional tail row var
type RowSubst = Map<number, { fields: { name: string; tags?: string[]; type: Type }[]; tail: number | null }>;
let _rowSubst: RowSubst = new Map();

// Maps named row variables (from type annotations like {name: Str | r}) to their numeric IDs
let _namedRowVars: Map<string, number> = new Map();
// ── HM type variable substitution ──

// Maps type variable IDs to their unified types
let _typeSubst: Map<number, Type> = new Map();

/** Apply type variable substitution, chasing through the substitution map. */
function applyTypeSubst(t: Type): Type {
  switch (t.tag) {
    case "t-var": {
      const sub = _typeSubst.get(t.id);
      if (sub) return applyTypeSubst(sub);
      return t;
    }
    case "t-fn": return tFn(applyTypeSubst(t.param), applyTypeSubst(t.result), t.effects);
    case "t-list": return tList(applyTypeSubst(t.element));
    case "t-tuple": return { tag: "t-tuple", elements: t.elements.map(applyTypeSubst) };
    case "t-record": return {
      tag: "t-record",
      fields: t.fields.map(f => ({ name: f.name, tags: f.tags, type: applyTypeSubst(f.type) })),
      rowVar: t.rowVar,
    };
    case "t-generic": return {
      tag: "t-generic",
      name: t.name,
      args: t.args.map(applyTypeSubst),
    };
    default: return t;
  }
}

/** Occurs check: does varId appear in type t? */
function typeVarOccursIn(varId: number, t: Type): boolean {
  const resolved = applyTypeSubst(t);
  switch (resolved.tag) {
    case "t-var": return resolved.id === varId;
    case "t-fn": return typeVarOccursIn(varId, resolved.param) || typeVarOccursIn(varId, resolved.result);
    case "t-list": return typeVarOccursIn(varId, resolved.element);
    case "t-tuple": return resolved.elements.some(e => typeVarOccursIn(varId, e));
    case "t-record": return resolved.fields.some(f => typeVarOccursIn(varId, f.type));
    case "t-generic": return resolved.args.some(a => typeVarOccursIn(varId, a));
    default: return false;
  }
}

/** Unify two types, updating _typeSubst. Returns true on success. */
function unifyTypes(a: Type, b: Type): boolean {
  const ra = applyTypeSubst(a);
  const rb = applyTypeSubst(b);

  // Same node
  if (ra === rb) return true;

  // Type variable on either side — bind it
  if (ra.tag === "t-var") {
    if (rb.tag === "t-var" && ra.id === rb.id) return true;
    if (typeVarOccursIn(ra.id, rb)) return false; // occurs check
    _typeSubst.set(ra.id, rb);
    return true;
  }
  if (rb.tag === "t-var") {
    if (typeVarOccursIn(rb.id, ra)) return false;
    _typeSubst.set(rb.id, ra);
    return true;
  }

  // Generic with name "?" is the old tAny sentinel — still permissive for backward compat
  if ((ra.tag === "t-generic" && ra.name === "?") || (rb.tag === "t-generic" && rb.name === "?")) return true;

  // Named generics (user types like Ordering, Option): require same name
  // Type parameters should have been replaced with t-var by HM generalization
  if (ra.tag === "t-generic" && rb.tag === "t-generic") {
    if (ra.name !== rb.name) return false;
    if (ra.args.length !== rb.args.length) return false;
    return ra.args.every((a, i) => unifyTypes(a, rb.args[i]));
  }
  // One side is t-generic, other is not — permissive for user-defined types (e.g. Ordering)
  if (ra.tag === "t-generic" || rb.tag === "t-generic") return true;

  // Same tag — structural unification
  if (ra.tag !== rb.tag) return false;

  switch (ra.tag) {
    case "t-primitive": return ra.name === (rb as typeof ra).name;
    case "t-fn": return unifyTypes(ra.param, (rb as typeof ra).param) && unifyTypes(ra.result, (rb as typeof ra).result);
    case "t-list": return unifyTypes(ra.element, (rb as typeof ra).element);
    case "t-tuple": {
      const bt = rb as typeof ra;
      if (ra.elements.length !== bt.elements.length) return false;
      return ra.elements.every((e, i) => unifyTypes(e, bt.elements[i]));
    }
    default: return true;
  }
}

/** Create a fresh type variable. */
function freshTypeVar(): Type {
  return { tag: "t-var", id: freshVar() };
}

// ── HM let-polymorphism: generalization and instantiation ──

/** Collect free type variable IDs in a type (after applying substitution). */
function freeTypeVarsInType(t: Type): Set<number> {
  const result = new Set<number>();
  function walk(ty: Type): void {
    const resolved = applyTypeSubst(ty);
    switch (resolved.tag) {
      case "t-var": result.add(resolved.id); break;
      case "t-fn": walk(resolved.param); walk(resolved.result); break;
      case "t-list": walk(resolved.element); break;
      case "t-tuple": resolved.elements.forEach(walk); break;
      case "t-record": resolved.fields.forEach(f => walk(f.type)); break;
      case "t-generic": resolved.args.forEach(walk); break;
    }
  }
  walk(t);
  return result;
}

/** Generalize a type into a TypeScheme by quantifying type vars not in envFreeVars. */
function generalizeType(envFreeVars: Set<number>, t: Type): import("./types.js").TypeScheme {
  const resolved = applyTypeSubst(t);
  const varsInType = freeTypeVarsInType(resolved);
  const quantified: number[] = [];
  for (const v of varsInType) {
    if (!envFreeVars.has(v)) quantified.push(v);
  }
  return { typeVars: quantified, effectVars: [], body: resolved };
}

/** Instantiate a TypeScheme by replacing quantified vars with fresh type variables. */
function instantiateScheme(scheme: import("./types.js").TypeScheme): Type {
  if (scheme.typeVars.length === 0) return scheme.body;
  const mapping = new Map<number, Type>();
  for (const v of scheme.typeVars) {
    mapping.set(v, freshTypeVar());
  }
  function go(t: Type): Type {
    switch (t.tag) {
      case "t-var": return mapping.get(t.id) ?? t;
      case "t-fn": return tFn(go(t.param), go(t.result), t.effects);
      case "t-list": return tList(go(t.element));
      case "t-tuple": return tTuple(t.elements.map(go));
      case "t-record": return tRecord(t.fields.map(f => ({ name: f.name, tags: f.tags, type: go(f.type) })), t.rowVar);
      case "t-generic": return { tag: "t-generic", name: t.name, args: t.args.map(go) };
      default: return t;
    }
  }
  return go(scheme.body);
}

/** Apply row substitution to a type, resolving row variables. */
function applyRowSubst(t: Type): Type {
  switch (t.tag) {
    case "t-record": {
      const resolvedFields = t.fields.map(f => ({ name: f.name, tags: f.tags, type: applyRowSubst(f.type) }));
      if (t.rowVar !== null) {
        // Chase the substitution chain
        let allFields = [...resolvedFields];
        let tail: number | null = t.rowVar;
        while (tail !== null && _rowSubst.has(tail)) {
          const sub: { fields: { name: string; tags?: string[]; type: Type }[]; tail: number | null } = _rowSubst.get(tail)!;
          allFields.push(...sub.fields.map(f => ({ name: f.name, tags: f.tags ?? [] as string[], type: applyRowSubst(f.type) })));
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
    case "t-borrow": return tBorrow(applyRowSubst(t.inner));
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
    case "t-borrow": return rowOccursIn(rowVarId, t.inner);
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
        const fields = ty.fields.map(f => ({ name: f.name, tags: f.tags, type: go(f.type) }));
        const rowVar = ty.rowVar !== null ? freshenRowVar(ty.rowVar) : null;
        return tRecord(fields, rowVar);
      }
      case "t-fn": return tFn(go(ty.param), go(ty.result), ty.effects);
      case "t-list": return { tag: "t-list", element: go(ty.element) };
      case "t-tuple": return { tag: "t-tuple", elements: ty.elements.map(go) };
      case "t-borrow": return tBorrow(go(ty.inner));
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
// Param type expressions for all functions (for generic return type substitution)
let _fnParamTypeExprs: Map<string, TypeExpr[]> = new Map();
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

// Known type names — populated during first pass to distinguish type params from user types
let _knownTypeNames: Set<string> = new Set();

// Per-function type param → t-var mappings (for second pass body checking)
let _fnTypeParamVars: Map<string, Map<string, Type>> = new Map();

// ── HM let-polymorphism for user-defined functions ──

const PRIMITIVE_TYPE_NAMES = new Set(["Int", "Rat", "Bool", "Str", "Unit"]);

/** Check if a TypeExpr name is a type parameter (not a known type). */
function isTypeParam(name: string): boolean {
  if (PRIMITIVE_TYPE_NAMES.has(name)) return false;
  if (_knownTypeNames.has(name)) return false;
  // Type parameters: single uppercase letter or uppercase-starting name not in known types
  return name.length > 0 && name[0] >= "A" && name[0] <= "Z";
}

/** Collect type parameter names from a TypeExpr tree. */
function collectTypeParams(te: TypeExpr): Set<string> {
  const params = new Set<string>();
  function walk(t: TypeExpr): void {
    switch (t.tag) {
      case "t-name":
        if (isTypeParam(t.name)) params.add(t.name);
        break;
      case "t-list": walk(t.element); break;
      case "t-tuple": t.elements.forEach(walk); break;
      case "t-fn": walk(t.param); walk(t.result); break;
      case "t-generic":
        if (isTypeParam(t.name) && t.args.length === 0) params.add(t.name);
        t.args.forEach(walk);
        break;
      case "t-record": t.fields.forEach(f => walk(f.type)); break;
      case "t-refined": walk(t.base); break;
      case "t-borrow": walk(t.inner); break;
    }
  }
  walk(te);
  return params;
}

/** Resolve a TypeExpr to a Type, replacing type parameter names with t-var from mapping. */
function resolveTypeWithVars(te: TypeExpr, varMapping: Map<string, Type>): Type {
  switch (te.tag) {
    case "t-name": {
      const mapped = varMapping.get(te.name);
      if (mapped) return mapped;
      return resolveType(te);
    }
    case "t-list": return tList(resolveTypeWithVars(te.element, varMapping));
    case "t-tuple":
      if (te.elements.length === 0) return tUnit;
      return tTuple(te.elements.map(e => resolveTypeWithVars(e, varMapping)));
    case "t-fn": return tFn(
      resolveTypeWithVars(te.param, varMapping),
      resolveTypeWithVars(te.result, varMapping),
      te.effects.map(e => eNamed(e.name)),
    );
    case "t-generic": {
      const mapped = varMapping.get(te.name);
      if (mapped && te.args.length === 0) return mapped;
      return { tag: "t-generic", name: te.name, args: te.args.map(a => resolveTypeWithVars(a, varMapping)) };
    }
    case "t-record": {
      const fields = te.fields.map(f => ({ name: f.name, tags: f.tags, type: resolveTypeWithVars(f.type, varMapping) }));
      if (te.rowVar !== null) {
        const rv = _namedRowVars.get(te.rowVar) ?? freshVar();
        _namedRowVars.set(te.rowVar, rv);
        return tRecord(fields, rv);
      }
      return tRecord(fields);
    }
    case "t-refined": return resolveTypeWithVars(te.base, varMapping);
    case "t-borrow": return tBorrow(resolveTypeWithVars(te.inner, varMapping));
    default: return resolveType(te);
  }
}

// ── Affine type tracking ──

type AffineTypeSet = Set<string>; // names of types declared `affine`
type ImplEntry = { interface_: string; forType: string; typeArgs: string[]; loc: Loc };
type ImplRegistry = Map<string, ImplEntry[]>; // interface name → list of impls

const PRIMITIVE_CANONICAL: Record<string, string> = { int: "Int", rat: "Rat", bool: "Bool", str: "Str", unit: "Unit" };
const CANONICAL_TO_TYPE: Record<string, Type> = { Int: tInt, Rat: tRat, Bool: tBool, Str: tStr, Unit: tUnit };

/** Convert a canonical type name back to a Type (primitives only, others return tAny). */
function typeFromCanonicalName(name: string): Type {
  return CANONICAL_TO_TYPE[name] ?? tAny;
}

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
    case "t-borrow": {
      const inner = canonicalTypeName(t.inner);
      return inner !== null ? `&${inner}` : null;
    }
    default: return null;
  }
}

/** Check if a type contains &T anywhere (borrows cannot escape scope per spec 4.4) */
function containsBorrow(t: Type): boolean {
  switch (t.tag) {
    case "t-borrow": return true;
    case "t-list": return containsBorrow(t.element);
    case "t-tuple": return t.elements.some(containsBorrow);
    case "t-record": return t.fields.some(f => containsBorrow(f.type));
    case "t-fn": return containsBorrow(t.param) || containsBorrow(t.result);
    default: return false;
  }
}

/** Check if a data structure type (list, tuple, record) contains &T.
 *  Unlike containsBorrow, this skips function types (fn types can accept borrows). */
function containsBorrowInData(t: Type): boolean {
  switch (t.tag) {
    case "t-list": return containsBorrow(t.element);
    case "t-tuple": return t.elements.some(containsBorrow);
    case "t-record": return t.fields.some(f => containsBorrow(f.type));
    default: return false;
  }
}

function isAffine(t: Type, affineTypes: AffineTypeSet): boolean {
  switch (t.tag) {
    case "t-generic": return affineTypes.has(t.name);
    case "t-list": return isAffine(t.element, affineTypes);
    case "t-tuple": return t.elements.some(e => isAffine(e, affineTypes));
    case "t-record": return t.fields.some(f => isAffine(f.type, affineTypes));
    case "t-borrow": return false; // borrows are not affine — they don't consume
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

  // Snapshot current affine binding names (for scoping pattern-bound vars)
  affineBindingNames(): Set<string> {
    return new Set(this.affineBindings.keys());
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

  // Snapshot affine bindings (for scoping pattern-bound vars in match arms)
  snapshotBindings(): Map<string, Loc> {
    return new Map(this.affineBindings);
  }

  restoreBindings(snap: Map<string, Loc>): void {
    this.affineBindings = new Map(snap);
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
  private schemes: Map<string, import("./types.js").TypeScheme>;
  private constraintAliases: Map<string, FnConstraintInfo>;
  private parent: TypeEnv | null;

  constructor(parent: TypeEnv | null = null) {
    this.bindings = new Map();
    this.schemes = new Map();
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

  getScheme(name: string): import("./types.js").TypeScheme | undefined {
    const s = this.schemes.get(name);
    if (s !== undefined) return s;
    if (this.parent) return this.parent.getScheme(name);
    return undefined;
  }

  setScheme(name: string, scheme: import("./types.js").TypeScheme): void {
    this.schemes.set(name, scheme);
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

  /** Collect free type variables across all bindings (for generalization). */
  freeTypeVars(): Set<number> {
    const result = new Set<number>();
    for (const t of this.bindings.values()) {
      for (const v of freeTypeVarsInType(t)) result.add(v);
    }
    if (this.parent) {
      for (const v of this.parent.freeTypeVars()) result.add(v);
    }
    return result;
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
      const fields = te.fields.map(f => ({ name: f.name, tags: f.tags, type: resolveType(f.type) }));
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
    case "t-borrow": return tBorrow(resolveType(te.inner));
    case "t-tag-project": {
      const baseType = resolveType(te.base);
      if (baseType.tag === "t-record") {
        const filtered = baseType.fields.filter(f => f.tags.includes(te.tagName));
        return tRecord(filtered);
      }
      return tAny; // non-record base: permissive fallback
    }
    case "t-type-filter": {
      const baseType = resolveType(te.base);
      const filterType = resolveType(te.filterType);
      if (baseType.tag === "t-record") {
        const filtered = baseType.fields.filter(f => typeEqual(f.type, filterType));
        return tRecord(filtered);
      }
      return tAny;
    }
    case "t-pick": {
      const baseType = resolveType(te.base);
      if (baseType.tag === "t-record") {
        const nameSet = new Set(te.fieldNames);
        const filtered = baseType.fields.filter(f => nameSet.has(f.name));
        return tRecord(filtered);
      }
      return tAny;
    }
    case "t-omit": {
      const baseType = resolveType(te.base);
      if (baseType.tag === "t-record") {
        const nameSet = new Set(te.fieldNames);
        const filtered = baseType.fields.filter(f => !nameSet.has(f.name));
        return tRecord(filtered);
      }
      return tAny;
    }
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
      const fieldStr = resolved.fields.map(f => {
        const tagPrefix = f.tags.length > 0 ? f.tags.map(t => `@${t} `).join("") : "";
        return `${tagPrefix}${f.name}: ${showType(f.type)}`;
      }).join(", ");
      if (resolved.rowVar !== null) {
        return fieldStr ? `{${fieldStr} | r${resolved.rowVar}}` : `{r${resolved.rowVar}}`;
      }
      return `{${fieldStr}}`;
    }
    case "t-borrow": return `&${showType(t.inner)}`;
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
    case "t-borrow": return b.tag === "t-borrow" && typeEqual(a.inner, (b as typeof a).inner);
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

// ── Substitute type parameter bindings into a Type ──

/** Apply type parameter bindings to a Type, replacing named generics with concrete types.
 *  E.g., given bindings { T: "Int" }, replaces t-generic "T" with t-primitive "int". */
function substituteTypeParams(t: Type, bindings: Map<string, string>): Type {
  switch (t.tag) {
    case "t-generic": {
      const bound = bindings.get(t.name);
      if (bound) {
        switch (bound) {
          case "Int": return tInt;
          case "Rat": return tRat;
          case "Bool": return tBool;
          case "Str": return tStr;
          case "Unit": return tUnit;
          default: return { tag: "t-generic", name: bound, args: t.args.map(a => substituteTypeParams(a, bindings)) };
        }
      }
      return { tag: "t-generic", name: t.name, args: t.args.map(a => substituteTypeParams(a, bindings)) };
    }
    case "t-fn": return tFn(substituteTypeParams(t.param, bindings), substituteTypeParams(t.result, bindings), t.effects);
    case "t-list": return tList(substituteTypeParams(t.element, bindings));
    case "t-tuple": return { tag: "t-tuple", elements: t.elements.map(e => substituteTypeParams(e, bindings)) };
    case "t-record": return { tag: "t-record", fields: t.fields.map(f => ({ name: f.name, tags: f.tags, type: substituteTypeParams(f.type, bindings) })), rowVar: t.rowVar };
    case "t-borrow": return tBorrow(substituteTypeParams(t.inner, bindings));
    default: return t;
  }
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
      } else {
        // Composite types: extract canonical name so constraint checking works
        const canonical = canonicalTypeName(argType);
        if (canonical && !bindings.has(name)) bindings.set(name, canonical);
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
    case "t-borrow":
      extractTypeBindings(paramExpr.inner, argType, bindings);
      break;
  }
}

/** Refine return type of builtin list/tuple operations based on argument types.
 *  Without this, operations like head, get, fst, snd return tAny. */
function refineCompositeReturn(fnName: string, argTypes: Type[]): Type | null {
  const arg0 = argTypes[0];
  switch (fnName) {
    // List element extraction: [T] -> T
    case "head":
    case "get":
      if (arg0.tag === "t-list" && arg0.element.tag !== "t-generic") return arg0.element;
      return null;
    // List -> List preserving element type
    case "tail":
    case "rev":
    case "filter":
      if (arg0.tag === "t-list") return tList(arg0.element);
      return null;
    case "cons":
      // cons : T -> [T] -> [T]; arg0 is the element, arg1 is the list
      if (argTypes.length >= 2 && argTypes[1].tag === "t-list") return tList(argTypes[1].element);
      if (arg0.tag !== "t-generic") return tList(arg0);
      return null;
    case "cat":
      if (arg0.tag === "t-list") return tList(arg0.element);
      return null;
    case "map":
      // map : [T] -> (T -> U) -> [U]; infer U from callback return type
      if (argTypes.length >= 2 && argTypes[1].tag === "t-fn") {
        return tList(applyTypeSubst(argTypes[1].result));
      }
      if (arg0.tag === "t-list") return tList(arg0.element);
      return null;
    case "flat-map":
      // flat-map : [T] -> (T -> [U]) -> [U]; infer from callback return
      if (argTypes.length >= 2 && argTypes[1].tag === "t-fn") {
        const cbRet = applyTypeSubst(argTypes[1].result);
        if (cbRet.tag === "t-list") return cbRet;
      }
      if (arg0.tag === "t-list") return tList(tAny);
      return null;
    case "fold":
      // fold : [T] -> U -> (U -> T -> U) -> U; return type = init type (arg1)
      if (argTypes.length >= 2) return argTypes[1];
      return null;
    case "zip":
      // zip : [A] -> [B] -> [(A, B)]
      if (argTypes.length >= 2 && arg0.tag === "t-list" && argTypes[1].tag === "t-list") {
        return tList(tTuple([arg0.element, argTypes[1].element]));
      }
      return null;
    case "range":
      return tList(tInt);
    case "split":
      return tList(tStr);
    // Tuple element extraction
    case "fst":
      if (arg0.tag === "t-tuple" && arg0.elements.length >= 1) return arg0.elements[0];
      return null;
    case "snd":
      if (arg0.tag === "t-tuple" && arg0.elements.length >= 2) return arg0.elements[1];
      return null;
    case "tuple.get":
      // tuple.get : T -> Int -> element; we can't know the index statically in general
      return null;
    default:
      return null;
  }
}

/** Check whether an impl of the given interface exists for the given type name.
 *  If constraintTypeArgs are provided, also verifies the impl's type args match.
 *  For composite types (List, Tuple), derivable interfaces are satisfied if all
 *  element types implement the same interface. */
function hasImpl(interfaceName: string, typeName: string, constraintTypeArgs?: string[]): boolean {
  const entries = _implRegistry.get(interfaceName);
  if (!entries) return false;
  // Direct impl lookup
  const directMatch = !constraintTypeArgs || constraintTypeArgs.length === 0
    ? entries.some(e => e.forType === typeName)
    : entries.some(e =>
        e.forType === typeName &&
        e.typeArgs.length === constraintTypeArgs.length &&
        e.typeArgs.every((a, i) => a === constraintTypeArgs[i])
      );
  if (directMatch) return true;
  // Composite type structural check for derivable interfaces
  if (DERIVABLE_INTERFACES.has(interfaceName)) {
    // List: [T] has Iface if T has Iface (Ord excluded for Bool)
    if (typeName.startsWith("[") && typeName.endsWith("]")) {
      const elemType = typeName.slice(1, -1);
      return hasImpl(interfaceName, elemType);
    }
    // Tuple: (A, B, ...) has Iface if all elements have Iface
    if (typeName.startsWith("(") && typeName.endsWith(")")) {
      const inner = typeName.slice(1, -1);
      const elemTypes = splitTopLevelComma(inner);
      return elemTypes.every(et => hasImpl(interfaceName, et.trim()));
    }
    // Record: {a: A, b: B} has Iface if all field types have Iface
    if (typeName.startsWith("{") && typeName.endsWith("}")) {
      const inner = typeName.slice(1, -1);
      const fieldParts = splitTopLevelComma(inner);
      return fieldParts.every(fp => {
        const colonIdx = fp.indexOf(":");
        if (colonIdx === -1) return false;
        return hasImpl(interfaceName, fp.slice(colonIdx + 1).trim());
      });
    }
  }
  return false;
}

/** Split a string by commas at the top level (not inside brackets/parens). */
function splitTopLevelComma(s: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === "(" || c === "[" || c === "{") depth++;
    else if (c === ")" || c === "]" || c === "}") depth--;
    else if (c === "," && depth === 0) {
      parts.push(s.slice(start, i));
      start = i + 1;
    }
  }
  parts.push(s.slice(start));
  return parts;
}

/** Resolve constraint info for a callee expression (handles direct names, aliases,
 *  partial application / function-returning calls, and pipeline expressions). */
function resolveConstraintInfo(expr: Expr, env: TypeEnv): FnConstraintInfo | undefined {
  if (expr.tag === "var") {
    return _fnConstraints.get(expr.name) ?? env.getConstraint(expr.name);
  }
  // When calling the result of another function call (e.g., `getF()(x)`),
  // propagate constraints from the outer function if the return is a function.
  if (expr.tag === "apply" && expr.fn.tag === "var") {
    return _fnConstraints.get(expr.fn.name) ?? env.getConstraint(expr.fn.name);
  }
  // Pipeline: x |> f — propagate constraints from f
  if (expr.tag === "pipeline") {
    return resolveConstraintInfo(expr.right, env);
  }
  // Lambda that wraps a constrained call: fn(x) => f(x)
  if (expr.tag === "lambda" && expr.body.tag === "apply") {
    return resolveConstraintInfo(expr.body.fn, env);
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
      // HM let-polymorphism: instantiate polymorphic schemes with fresh type vars
      const scheme = env.getScheme(expr.name);
      if (scheme && scheme.typeVars.length > 0) {
        if (aff.isAffineVar(expr.name)) {
          aff.consume(expr.name, expr.loc, errors);
        }
        return instantiateScheme(scheme);
      }
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
      // Borrow scope enforcement (spec 4.4): borrows cannot be stored in data structures
      if (containsBorrowInData(valType)) {
        errors.push({
          code: "E603",
          message: `let binding '${expr.name}' contains a borrow in a compound type '${showType(valType)}' (borrows cannot be stored in data structures)`,
          location: expr.loc,
        });
      }
      if (expr.body === null) return valType;
      if (expr.name === "_") return inferExpr(expr.body, env, errors, registry, aff);
      const child = env.extend();
      const resolvedVal = applyTypeSubst(valType);
      child.set(expr.name, resolvedVal);
      // HM let-polymorphism: generalize lambda values (value restriction)
      if (expr.value.tag === "lambda") {
        const envFreeVars = env.freeTypeVars();
        const scheme = generalizeType(envFreeVars, resolvedVal);
        if (scheme.typeVars.length > 0) {
          child.setScheme(expr.name, scheme);
        }
      }
      // Propagate where-constraint info through let bindings
      const cInfo = resolveConstraintInfo(expr.value, env);
      if (cInfo) child.setConstraint(expr.name, cInfo);
      // Propagate refinement info through let bindings
      const preLetRefState = saveRefinementState();
      if (expr.value.tag === "var" && _varRefinements.has(expr.value.name)) {
        // let y = x where x has refinement P(v) → y gets the same refinement
        _varRefinements.set(expr.name, _varRefinements.get(expr.value.name)!);
      } else {
        // Try to express the value as a predicate string for equality tracking
        const valPredStr = exprToPredString(expr.value);
        if (valPredStr) {
          // let y = expr → add path condition y == expr
          _pathConditions.push(`${expr.name} == ${valPredStr}`);
        }
      }
      // Register affine binding if value type is affine
      if (isAffine(valType, aff.affineTypes)) {
        aff.registerAffine(expr.name, expr.loc);
      }
      const bodyType = inferExpr(expr.body, child, errors, registry, aff);
      // Restore refinement state after let body
      restoreRefinementState(preLetRefState);
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
      // Extract branch condition for refinement tracking
      const condPred = extractConditionPredicate(expr.cond);

      // Branch consistency: both branches must consume the same affine vars
      const preBranch = aff.snapshot();
      const preRefState = saveRefinementState();

      // Then branch: add positive condition
      if (condPred) _pathConditions.push(condPred.pos);
      const thenType = inferExpr(expr.then, env, errors, registry, aff);
      const thenConsumed = aff.consumedSince(preBranch);

      aff.restore(preBranch);
      restoreRefinementState(preRefState);

      // Else branch: add negated condition
      if (condPred) _pathConditions.push(condPred.neg);
      const elseType = inferExpr(expr.else, env, errors, registry, aff);
      const elseConsumed = aff.consumedSince(preBranch);

      // Restore refinement state after both branches
      restoreRefinementState(preRefState);

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
        const pt = p.type ? resolveType(p.type) : freshTypeVar();
        lamEnv.set(p.name, pt);
        paramTypes.push(pt);
        if (isAffine(pt, aff.affineTypes)) {
          aff.registerAffine(p.name, expr.loc);
        }
      }
      const retType = inferExpr(expr.body, lamEnv, errors, registry, aff);
      // Borrow scope enforcement (spec 4.4): lambdas cannot return borrow types
      if (containsBorrow(retType)) {
        errors.push({
          code: "E603",
          message: `lambda cannot return a borrow type '${showType(retType)}' (borrows cannot escape their scope)`,
          location: expr.loc,
        });
      }
      // Check parameters for borrows in compound types (bare &T is allowed)
      for (const pt of paramTypes) {
        if (pt.tag !== "t-borrow" && containsBorrow(pt)) {
          errors.push({
            code: "E603",
            message: `lambda parameter contains a borrow in a compound type '${showType(pt)}' (borrows cannot be stored in data structures)`,
            location: expr.loc,
          });
        }
      }
      let fnType: Type = retType;
      if (paramTypes.length === 0) {
        // Zero-param lambda: () -> T, not just T
        fnType = tFn(tUnit, fnType);
      } else {
        for (let i = paramTypes.length - 1; i >= 0; i--) {
          fnType = tFn(paramTypes[i], fnType);
        }
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
          } else if (!unifyTypes(argType, paramTypes[i]) && !numCompatible(argType, paramTypes[i])) {
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
              checkRefinementObligation(
                expr.args[i], reqPred, "E310", "refinement not satisfied", errors,
              );
            }
          }
        }
        // Extract type parameter bindings for generic return type substitution and constraint checking
        const typeBindings = new Map<string, string>();
        // Get param type expressions — from constraints or from the general registry
        const cInfo = resolveConstraintInfo(expr.fn, env);
        const paramTEs = cInfo?.paramTypeExprs
          ?? (expr.fn.tag === "var" ? _fnParamTypeExprs.get(expr.fn.name) : undefined);
        if (paramTEs) {
          for (let i = 0; i < Math.min(argTypes.length, paramTEs.length); i++) {
            extractTypeBindings(paramTEs[i], argTypes[i], typeBindings);
          }
        }
        if (cInfo) {
          // Where-constraint checking at call sites (direct and indirect/higher-order)
          if (cInfo.constraints.length > 0) {
            for (const c of cInfo.constraints) {
              const concreteType = typeBindings.get(c.typeParam);
              if (concreteType && concreteType !== "?") {
                const resolvedTypeArgs = c.typeArgs.length > 0
                  ? c.typeArgs.map(ta => {
                      const name = typeExprName(ta);
                      return typeBindings.get(name) ?? name;
                    })
                  : undefined;
                const typeArgStr = resolvedTypeArgs && resolvedTypeArgs.length > 0
                  ? `<${resolvedTypeArgs.join(", ")}>`
                  : "";
                if (!hasImpl(c.interface_, concreteType, resolvedTypeArgs)) {
                  errors.push({
                    code: "E205",
                    message: `where constraint not satisfied: '${concreteType}' does not implement '${c.interface_}${typeArgStr}'`,
                    location: expr.loc,
                  });
                }
              }
            }
          }
        }
        // Substitute type parameter bindings into return type for generic inference
        const returnType = typeBindings.size > 0 ? substituteTypeParams(cur, typeBindings) : cur;
        const resolved = applyTypeSubst(returnType);
        // Per-call-site instantiation for builtin interface methods and composite refinement
        if (expr.fn.tag === "var" && argTypes.length > 0) {
          const fnName = expr.fn.name;
          if (resolved.tag === "t-generic" && resolved.name === "?") {
            // clone : (T) -> T — return type matches argument type
            if (fnName === "clone" && argTypes.length === 1) return argTypes[0];
            // cmp : (T, T) -> Ordering
            if (fnName === "cmp" && argTypes.length === 2) return { tag: "t-generic", name: "Ordering", args: [] };
            // from : (T) -> Self — look up unique From<ArgType> impl to determine return type
            if (fnName === "from" && argTypes.length === 1) {
              const argName = canonicalTypeName(argTypes[0]);
              if (argName) {
                const fromImpls = _implRegistry.get("From");
                if (fromImpls) {
                  const matches = fromImpls.filter(e => e.typeArgs.length > 0 && e.typeArgs[0] === argName);
                  if (matches.length === 1) {
                    return typeFromCanonicalName(matches[0].forType);
                  }
                }
              }
            }
            // into : (Self) -> T — look up unique Into impl (from blanket From) to determine return type
            if (fnName === "into" && argTypes.length === 1) {
              const argName = canonicalTypeName(argTypes[0]);
              if (argName) {
                const intoImpls = _implRegistry.get("Into");
                if (intoImpls) {
                  const matches = intoImpls.filter(e => e.forType === argName);
                  if (matches.length === 1 && matches[0].typeArgs.length > 0) {
                    return typeFromCanonicalName(matches[0].typeArgs[0]);
                  }
                }
              }
            }
          }
          // Refine composite type inference: propagate element types through list/tuple builtins
          // This fires for any return type containing tAny (not just top-level tAny)
          const refined = refineCompositeReturn(fnName, argTypes);
          if (refined) return refined;
        }
        return resolved;
      }
      // Non-function type being called — infer args for nested checking, return unknown
      for (const arg of expr.args) inferExpr(arg, env, errors, registry, aff);
      return tAny;
    }

    case "match": {
      const subjectType = inferExpr(expr.subject, env, errors, registry, aff);
      let resultType: Type | null = null;
      const preBranch = aff.snapshot();
      const preMatchBindings = aff.affineBindingNames();
      const preRefState = saveRefinementState();
      const branchConsumed: Set<string>[] = [];
      // Convert subject to predicate string for match-arm refinement
      const subjectPredStr = exprToPredString(expr.subject);
      const preMatchAffineBindings = aff.snapshotBindings();
      for (const arm of expr.arms) {
        aff.restore(preBranch);
        aff.restoreBindings(preMatchAffineBindings);
        restoreRefinementState(preRefState);
        const armEnv = env.extend();
        bindPatternVars(arm.pattern, armEnv, aff, subjectType);
        // Add pattern-derived path conditions for this arm
        if (subjectPredStr) {
          const patPred = patternToPredicate(arm.pattern, subjectPredStr);
          if (patPred) _pathConditions.push(patPred);
        }
        const armType = inferExpr(arm.body, armEnv, errors, registry, aff);
        // Check that pattern-bound affine variables were consumed in this arm
        const postArmBindings = aff.affineBindingNames();
        const patternBound: string[] = [];
        for (const name of postArmBindings) {
          if (!preMatchBindings.has(name)) patternBound.push(name);
        }
        if (patternBound.length > 0) {
          aff.checkUnconsumed(patternBound, arm.pattern.loc, errors);
        }
        // Only track consumption of variables that existed before the match
        // (pattern-bound vars are local to their arm)
        const consumed = aff.consumedSince(preBranch);
        const scopedConsumed = new Set<string>();
        for (const name of consumed) {
          if (preMatchBindings.has(name)) scopedConsumed.add(name);
        }
        branchConsumed.push(scopedConsumed);
        if (resultType === null) {
          resultType = armType;
        } else if (!typeEqual(resultType, armType) && !numCompatible(resultType, armType)) {
          errors.push({ code: "E305", message: `match arms have inconsistent types: ${showType(resultType)} vs ${showType(armType)}`, location: expr.loc });
        }
      }
      restoreRefinementState(preRefState);
      aff.restoreBindings(preMatchAffineBindings);
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
      return tBorrow(innerType);
    }

    // Clone: clone expr — explicitly duplicate (expr should be a borrow)
    case "clone": {
      const innerType = inferExpr(expr.expr, env, errors, registry, aff);
      // Unwrap borrow to get the owned type
      const ownedType = innerType.tag === "t-borrow" ? innerType.inner : innerType;
      const typeName = canonicalTypeName(ownedType);
      if (typeName && !aff.isTypeCloneable(ownedType)) {
        errors.push({
          code: "E602",
          message: `type '${typeName}' does not implement Clone`,
          location: expr.loc,
        });
      }
      return ownedType;
    }

    // Discard: discard expr — explicitly abandon an affine value
    case "discard": {
      inferExpr(expr.expr, env, errors, registry, aff);
      return tUnit;
    }

    case "record": {
      // Infer record type from literal fields, with optional spread
      const fields: { name: string; tags: string[]; type: Type }[] = [];
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
        fields.push({ name: f.name, tags: f.tags ?? [], type: ft });
      }
      if (expr.spread) {
        const baseType = inferExpr(expr.spread, env, errors, registry, aff);
        const resolved = applyRowSubst(baseType);
        if (resolved.tag === "t-record") {
          // Merge base fields (excluding overridden ones) into the result
          for (const bf of resolved.fields) {
            if (!seen.has(bf.name)) {
              fields.push({ name: bf.name, tags: bf.tags, type: bf.type });
            }
          }
          return tRecord(fields, resolved.rowVar);
        }
      }
      return tRecord(fields);
    }

    case "field-access": {
      // Check for dotted builtin (e.g. str.cat, tuple.get) before inferring object
      if (expr.object.tag === "var") {
        const dottedName = `${expr.object.name}.${expr.field}`;
        // Check for polymorphic scheme first (HM instantiation)
        const dottedScheme = env.getScheme(dottedName);
        if (dottedScheme && dottedScheme.typeVars.length > 0) {
          return instantiateScheme(dottedScheme);
        }
        const builtinType = env.get(dottedName);
        if (builtinType) return builtinType;
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
          // Use fresh type variable for field type — unified through HM unification
          const fieldType = freshTypeVar();
          const newTail = freshVar();
          _rowSubst.set(resolved.rowVar, {
            fields: [{ name: expr.field, tags: [], type: fieldType }],
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

    case "pipeline": {
      // left |> right  ≡  right(left)
      const leftType = inferExpr(expr.left, env, errors, registry, aff);
      const rawRightType = inferExpr(expr.right, env, errors, registry, aff);
      const rightType = freshenRowVars(rawRightType);
      if (rightType.tag === "t-fn") {
        // Check argument type
        const resolvedArg = applyRowSubst(leftType);
        const resolvedParam = applyRowSubst(rightType.param);
        if (resolvedArg.tag === "t-record" && resolvedParam.tag === "t-record") {
          unifyRecords(resolvedParam, resolvedArg, errors, expr.loc);
        } else if (!typeEqual(leftType, rightType.param) && !numCompatible(leftType, rightType.param)) {
          errors.push({
            code: "E304",
            message: `pipeline argument: expected ${showType(rightType.param)}, got ${showType(leftType)}`,
            location: expr.loc,
          });
        }
        // Where-constraint checking for pipeline RHS
        const cInfo = resolveConstraintInfo(expr.right, env);
        if (cInfo && cInfo.constraints.length > 0) {
          const bindings = new Map<string, string>();
          if (cInfo.paramTypeExprs.length > 0) {
            extractTypeBindings(cInfo.paramTypeExprs[0], leftType, bindings);
          }
          for (const c of cInfo.constraints) {
            const concreteType = bindings.get(c.typeParam);
            if (concreteType && concreteType !== "?") {
              const resolvedTypeArgs = c.typeArgs.length > 0
                ? c.typeArgs.map(ta => {
                    const name = typeExprName(ta);
                    return bindings.get(name) ?? name;
                  })
                : undefined;
              const typeArgStr = resolvedTypeArgs && resolvedTypeArgs.length > 0
                ? `<${resolvedTypeArgs.join(", ")}>`
                : "";
              if (!hasImpl(c.interface_, concreteType, resolvedTypeArgs)) {
                errors.push({
                  code: "E205",
                  message: `where constraint not satisfied: '${concreteType}' does not implement '${c.interface_}${typeArgStr}'`,
                  location: expr.loc,
                });
              }
            }
          }
        }
        return rightType.result;
      }
      return tAny;
    }

    case "for": {
      const collType = inferExpr(expr.collection, env, errors, registry, aff);
      const forEnv = env.extend();
      let elemType: Type = tAny;
      if (collType.tag === "t-list") elemType = collType.element;
      bindPatternVars(expr.bind, forEnv, aff, elemType);
      if (expr.guard) inferExpr(expr.guard, forEnv, errors, registry, aff);
      if (expr.fold) {
        const initType = inferExpr(expr.fold.init, env, errors, registry, aff);
        forEnv.set(expr.fold.acc, initType);
        const bodyType = inferExpr(expr.body, forEnv, errors, registry, aff);
        // fold body should return same type as accumulator
        if (!typeEqual(bodyType, initType) && !numCompatible(bodyType, initType)) {
          errors.push({ code: "E302", message: `for-fold body type ${showType(bodyType)} doesn't match accumulator type ${showType(initType)}`, location: expr.loc });
        }
        return initType;
      }
      const bodyType = inferExpr(expr.body, forEnv, errors, registry, aff);
      return tList(bodyType);
    }

    case "range": {
      inferExpr(expr.start, env, errors, registry, aff);
      inferExpr(expr.end, env, errors, registry, aff);
      return tList(tInt);
    }

    case "do": {
      let lastType: Type = tUnit;
      const doEnv = env.extend();
      for (const step of expr.steps) {
        lastType = inferExpr(step.expr, doEnv, errors, registry, aff);
        if (step.bind) doEnv.set(step.bind, lastType);
      }
      return lastType;
    }

    case "let-pattern": {
      const valType = inferExpr(expr.value, env, errors, registry, aff);
      if (expr.body === null) return valType;
      const child = env.extend();
      bindPatternVars(expr.pattern, child, aff, valType);
      return inferExpr(expr.body, child, errors, registry, aff);
    }

    case "infix": case "unary":
      return tAny;
  }

  return tAny;
}

// Bind pattern variables with types from subject, registering affine bindings
function bindPatternVars(pat: Pattern, env: TypeEnv, aff?: AffineCtx, subjectType?: Type): void {
  switch (pat.tag) {
    case "p-var": {
      const t = subjectType ?? tAny;
      env.set(pat.name, t);
      if (aff && isAffine(t, aff.affineTypes)) {
        aff.registerAffine(pat.name, pat.loc);
      }
      break;
    }
    case "p-tuple": {
      pat.elements.forEach((p, i) => {
        const elemType = subjectType?.tag === "t-tuple" && i < subjectType.elements.length
          ? subjectType.elements[i] : undefined;
        bindPatternVars(p, env, aff, elemType);
      });
      break;
    }
    case "p-variant": {
      // Look up the constructor type in env to get field types
      const ctorType = env.get(pat.name);
      if (ctorType && ctorType.tag === "t-fn") {
        const fieldTypes: Type[] = [];
        let cur: Type = ctorType;
        while (cur.tag === "t-fn") {
          fieldTypes.push(cur.param);
          cur = cur.result;
        }
        pat.args.forEach((p, i) => {
          bindPatternVars(p, env, aff, i < fieldTypes.length ? fieldTypes[i] : undefined);
        });
      } else {
        pat.args.forEach(p => bindPatternVars(p, env, aff));
      }
      break;
    }
    case "p-record": {
      const resolved = subjectType ? applyRowSubst(subjectType) : undefined;
      if (resolved && resolved.tag === "t-record") {
        const fieldMap = new Map(resolved.fields.map(f => [f.name, f.type]));
        for (const pf of pat.fields) {
          const fieldType = fieldMap.get(pf.name);
          if (pf.pattern) {
            bindPatternVars(pf.pattern, env, aff, fieldType);
          } else {
            // Shorthand: {name} binds name to the field value
            const t = fieldType ?? tAny;
            env.set(pf.name, t);
            if (aff && isAffine(t, aff.affineTypes)) {
              aff.registerAffine(pf.name, pat.loc);
            }
          }
        }
        if (pat.rest && pat.rest !== "_") {
          // Bind rest to a record with remaining fields
          const matchedNames = new Set(pat.fields.map(f => f.name));
          const restFields = resolved.fields.filter(f => !matchedNames.has(f.name));
          env.set(pat.rest, tRecord(restFields, resolved.rowVar));
        }
      } else {
        // No type info — bind fields as tAny
        for (const pf of pat.fields) {
          if (pf.pattern) {
            bindPatternVars(pf.pattern, env, aff);
          } else {
            env.set(pf.name, tAny);
          }
        }
        if (pat.rest && pat.rest !== "_") {
          env.set(pat.rest, tAny);
        }
      }
      break;
    }
    case "p-wildcard": case "p-literal": break;
  }
}

// ── Register builtin function types ──

/** Register a polymorphic builtin with a proper HM type scheme.
 *  Each use site gets fresh type variables via instantiation. */
function registerPolyBuiltin(
  env: TypeEnv,
  name: string,
  varCount: number,
  buildType: (...vars: Type[]) => Type,
): void {
  const ids: number[] = [];
  const typeVars: Type[] = [];
  for (let i = 0; i < varCount; i++) {
    const id = freshVar();
    ids.push(id);
    typeVars.push(tVar(id));
  }
  const bodyType = buildType(...typeVars);
  env.set(name, bodyType);
  if (varCount > 0) {
    env.setScheme(name, { typeVars: ids, effectVars: [], body: bodyType });
  }
}

function registerBuiltins(env: TypeEnv): void {
  // Register all builtins with their registry types first (fallback for non-polymorphic ones)
  for (const entry of BUILTIN_REGISTRY) {
    env.set(entry.name, entry.type);
  }

  // Override polymorphic builtins with proper HM type schemes.
  // Each call site instantiates fresh type variables that participate in unification,
  // replacing the old tAny ("?") sentinel that suppressed type propagation.

  // Comparison: ∀a. a → a → Bool
  registerPolyBuiltin(env, "eq", 1, (a) => tFn(a, tFn(a, tBool)));
  registerPolyBuiltin(env, "neq", 1, (a) => tFn(a, tFn(a, tBool)));

  // Display: ∀a. a → Str
  registerPolyBuiltin(env, "show", 1, (a) => tFn(a, tStr));

  // List operations with element type propagation
  registerPolyBuiltin(env, "len", 1, (a) => tFn(tList(a), tInt));
  registerPolyBuiltin(env, "head", 1, (a) => tFn(tList(a), a));
  registerPolyBuiltin(env, "tail", 1, (a) => tFn(tList(a), tList(a)));
  registerPolyBuiltin(env, "cons", 1, (a) => tFn(a, tFn(tList(a), tList(a))));
  registerPolyBuiltin(env, "cat", 1, (a) => tFn(tList(a), tFn(tList(a), tList(a))));
  registerPolyBuiltin(env, "rev", 1, (a) => tFn(tList(a), tList(a)));
  registerPolyBuiltin(env, "get", 1, (a) => tFn(tList(a), tFn(tInt, a)));
  registerPolyBuiltin(env, "map", 2, (a, b) => tFn(tList(a), tFn(tFn(a, b), tList(b))));
  registerPolyBuiltin(env, "filter", 1, (a) => tFn(tList(a), tFn(tFn(a, tBool), tList(a))));
  registerPolyBuiltin(env, "fold", 2, (a, b) => tFn(tList(a), tFn(b, tFn(tFn(b, tFn(a, b)), b))));
  registerPolyBuiltin(env, "flat-map", 2, (a, b) => tFn(tList(a), tFn(tFn(a, tList(b)), tList(b))));
  registerPolyBuiltin(env, "zip", 2, (a, b) => tFn(tList(a), tFn(tList(b), tList(tTuple([a, b])))));

  // Tuple operations
  registerPolyBuiltin(env, "fst", 2, (a, b) => tFn(tTuple([a, b]), a));
  registerPolyBuiltin(env, "snd", 2, (a, b) => tFn(tTuple([a, b]), b));

  // Error handling: ∀a b. a → b
  registerPolyBuiltin(env, "raise", 2, (a, b) => tFn(a, b));
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
    case "t-borrow": return `&${typeExprName(te.inner)}`;
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
  _pathConditions = [];
  _fnConstraints = new Map();
  _fnParamTypeExprs = new Map();
  _rowSubst = new Map();
  _namedRowVars = new Map();
  _typeSubst = new Map();
  _knownTypeNames = new Set();
  _fnTypeParamVars = new Map();
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
  // Register built-in interface method names with proper HM type schemes
  // clone : ∀a. a → a
  registerPolyBuiltin(env, "clone", 1, (a) => tFn(a, a));
  // cmp : ∀a. a → a → Ordering
  registerPolyBuiltin(env, "cmp", 1, (a) => tFn(a, tFn(a, { tag: "t-generic", name: "Ordering", args: [] })));
  // default : ∀a. () → a
  registerPolyBuiltin(env, "default", 1, (a) => tFn(tUnit, a));
  // into : ∀a b. a → b (needs impl resolution at call site)
  registerPolyBuiltin(env, "into", 2, (a, b) => tFn(a, b));
  // from : ∀a b. a → b (needs impl resolution at call site)
  registerPolyBuiltin(env, "from", 2, (a, b) => tFn(a, b));

  // Pre-pass: collect known type names (for distinguishing type params from user types)
  for (const tl of program.topLevels) {
    if (tl.tag === "type-decl") {
      _knownTypeNames.add(tl.name);
      for (const v of tl.variants) _knownTypeNames.add(v.name);
    } else if (tl.tag === "interface-decl") {
      _knownTypeNames.add(tl.name);
    } else if (tl.tag === "effect-decl") {
      _knownTypeNames.add(tl.name);
    } else if (tl.tag === "effect-alias") {
      _knownTypeNames.add(tl.name);
    }
  }
  // Also add well-known non-primitive type names
  _knownTypeNames.add("Ordering");
  _knownTypeNames.add("Self");

  // First pass: register all type declarations, effect aliases, interfaces, impls, and function signatures
  for (const tl of program.topLevels) {
    if (tl.tag === "mod-decl") continue;
    if (tl.tag === "use-decl") {
      for (const imp of tl.imports) {
        env.set(imp.alias ?? imp.name, tAny);
        // If the import is an effect alias that was already registered (from
        // module top-levels prepended by processUseDecls), register the local
        // name so the checker resolves renamed aliases correctly.
        if (imp.alias && effectAliases.has(imp.name)) {
          effectAliases.set(imp.alias, effectAliases.get(imp.name)!);
        }
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
      // Collect type parameters from the entire signature
      const sigTypeParams = new Set<string>();
      for (const p of tl.sig.params) {
        for (const tp of collectTypeParams(p.type)) sigTypeParams.add(tp);
      }
      for (const tp of collectTypeParams(tl.sig.returnType)) sigTypeParams.add(tp);
      // Also collect from constraint type args
      for (const c of tl.constraints) {
        if (isTypeParam(c.typeParam)) sigTypeParams.add(c.typeParam);
      }

      if (sigTypeParams.size > 0) {
        // HM let-polymorphism: create fresh type variables for type params
        const varMapping = new Map<string, Type>();
        const varIds: number[] = [];
        for (const tp of sigTypeParams) {
          const id = freshVar();
          varIds.push(id);
          varMapping.set(tp, tVar(id));
        }
        _fnTypeParamVars.set(tl.name, varMapping);

        const retType = resolveTypeWithVars(tl.sig.returnType, varMapping);
        let fnType: Type;
        if (tl.sig.params.length === 0) {
          fnType = tFn(tUnit, retType);
        } else {
          fnType = retType;
          for (let i = tl.sig.params.length - 1; i >= 0; i--) {
            fnType = tFn(resolveTypeWithVars(tl.sig.params[i].type, varMapping), fnType);
          }
        }
        env.set(tl.name, fnType);
        env.setScheme(tl.name, { typeVars: varIds, effectVars: [], body: fnType });
      } else {
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
      // Extract refinement info for function parameters and return type
      const paramPreds = tl.sig.params.map(p =>
        p.type.tag === "t-refined" ? p.type.predicate : null
      );
      const returnPred = tl.sig.returnType.tag === "t-refined"
        ? tl.sig.returnType.predicate : null;
      if (paramPreds.some(p => p !== null) || returnPred !== null) {
        _fnRefInfo.set(tl.name, { paramPreds, returnPred });
      }
      // Register param type expressions for generic return type substitution
      const paramTypeExprs = tl.sig.params.map(p => p.type);
      _fnParamTypeExprs.set(tl.name, paramTypeExprs);
      // Register where-constraints for call-site checking
      if (tl.constraints.length > 0) {
        _fnConstraints.set(tl.name, {
          constraints: tl.constraints,
          paramTypeExprs,
        });
      }
    } else if (tl.tag === "extern-decl") {
      _namedRowVars = new Map();
      // Validate effect annotations: only built-in effects allowed (E-FFI-4)
      const BUILTIN_EFFECTS = new Set(["io", "exn", "async"]);
      for (const eff of tl.sig.effects) {
        if (!BUILTIN_EFFECTS.has(eff.name) && !effectAliases.has(eff.name)) {
          // Allow effect variables from callback parameters (lowercase single-letter names)
          if (eff.name.length === 1 && eff.name >= "a" && eff.name <= "z") continue;
          errors.push({ code: "E803", message: `extern function '${tl.name}' cannot declare user-defined effect '${eff.name}' (only io, exn, async allowed)`, location: tl.loc });
        }
      }
      // Register the function type in the environment (same as definition)
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
      // Register refinement info for extern return types
      const paramPreds = tl.sig.params.map(p =>
        p.type.tag === "t-refined" ? p.type.predicate : null
      );
      const returnPred = tl.sig.returnType.tag === "t-refined"
        ? tl.sig.returnType.predicate : null;
      if (paramPreds.some(p => p !== null) || returnPred !== null) {
        _fnRefInfo.set(tl.name, { paramPreds, returnPred });
      }
      const paramTypeExprs = tl.sig.params.map(p => p.type);
      _fnParamTypeExprs.set(tl.name, paramTypeExprs);
    }
  }

  // Second pass: check function bodies against their signatures
  for (const tl of program.topLevels) {
    if (tl.tag !== "definition") continue;
    const bodyEnv = env.extend();
    const aff = new AffineCtx(affineTypes, cloneableTypes, impls);
    const affineParams: string[] = [];
    // Propagate this function's where-constraints into the body environment
    // so constrained type params are available for nested constraint checking
    const fnCInfo = _fnConstraints.get(tl.name);
    if (fnCInfo) {
      bodyEnv.setConstraint(tl.name, fnCInfo);
      // Also propagate constraints for any parameter that shares a constrained type variable
      for (const p of tl.sig.params) {
        const pName = typeExprName(p.type);
        for (const c of fnCInfo.constraints) {
          if (pName === c.typeParam || (p.type.tag === "t-list" && typeExprName(p.type.element) === c.typeParam)) {
            // This parameter's type is constrained — make constraint info available
            // when the param is used as an argument to another constrained function
          }
        }
      }
    }
    // Set up refinement environment for this function body
    _varRefinements = new Map();
    _pathConditions = [];
    // Use type param var mapping if this function has type params (fresh vars for body)
    const bodyVarMapping = _fnTypeParamVars.get(tl.name);
    // For the body, create fresh vars so body checking is independent of scheme vars
    const bodyResolve = (te: TypeExpr): Type =>
      bodyVarMapping ? resolveTypeWithVars(te, bodyVarMapping) : resolveType(te);
    for (const p of tl.sig.params) {
      const pt = bodyResolve(p.type);
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
    const expectedRet = bodyResolve(tl.sig.returnType);
    // Borrow scope enforcement (spec 4.4): &T cannot appear in return types or compound types
    if (containsBorrow(expectedRet)) {
      errors.push({
        code: "E603",
        message: `function '${tl.name}' cannot return a borrow type '${showType(expectedRet)}' (borrows cannot escape their scope)`,
        location: tl.loc,
      });
    }
    // Also check parameters for borrows in compound types (but not bare &T which is allowed)
    for (const p of tl.sig.params) {
      const pt = bodyResolve(p.type);
      if (pt.tag !== "t-borrow" && containsBorrow(pt)) {
        errors.push({
          code: "E603",
          message: `parameter '${p.name}' contains a borrow in a compound type '${showType(pt)}' (borrows cannot be stored in data structures)`,
          location: tl.loc,
        });
      }
    }
    // When return type is Unit, allow any body type — result is discarded (statement position)
    const unitReturn = expectedRet.tag === "t-primitive" && expectedRet.name === "unit";
    if (!unitReturn) {
      const resolvedBody = applyTypeSubst(applyRowSubst(bodyType));
      const resolvedExpected = applyTypeSubst(applyRowSubst(expectedRet));
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
    // For if/match bodies, check per-branch with branch-specific path conditions
    if (tl.sig.returnType.tag === "t-refined") {
      const retPred = tl.sig.returnType.predicate;
      checkReturnRefinementPerBranch(tl.body, retPred, errors, tl.sig.params);
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
