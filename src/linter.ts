// Clank linter — lint rules beyond type checking
// Produces structured diagnostics (warnings) for code quality issues.

import type { Expr, Program, TopLevel, Pattern, Param } from "./ast.js";
import { BUILTIN_REGISTRY } from "./builtin-registry.js";

// ── Lint diagnostic (same shape as checker TypeError) ──

export type LintDiagnostic = {
  code: string;
  message: string;
  location: { line: number; col: number };
};

// ── Rule IDs ──

export const LINT_RULES: Record<string, string> = {
  "W100": "unused-variable",
  "W101": "unused-import",
  "W102": "shadowed-binding",
  "W103": "missing-pub-annotation",
  "W104": "unreachable-match-arm",
  "W105": "empty-effect-handler",
  "W106": "builtin-shadow",
};

// Reverse map: rule name → code
const ruleNameToCode: Record<string, string> = {};
for (const [code, name] of Object.entries(LINT_RULES)) {
  ruleNameToCode[name] = code;
}

// ── Collect all free variable references in an expression ──

function collectRefs(expr: Expr, refs: Set<string>): void {
  switch (expr.tag) {
    case "var":
      refs.add(expr.name);
      break;
    case "literal":
      break;
    case "let":
      collectRefs(expr.value, refs);
      if (expr.body) collectRefs(expr.body, refs);
      break;
    case "if":
      collectRefs(expr.cond, refs);
      collectRefs(expr.then, refs);
      collectRefs(expr.else, refs);
      break;
    case "match":
      collectRefs(expr.subject, refs);
      for (const arm of expr.arms) {
        collectPatternRefs(arm.pattern, refs);
        collectRefs(arm.body, refs);
      }
      break;
    case "lambda":
      collectRefs(expr.body, refs);
      break;
    case "apply":
      collectRefs(expr.fn, refs);
      for (const a of expr.args) collectRefs(a, refs);
      break;
    case "pipeline":
    case "infix":
      collectRefs(expr.left, refs);
      collectRefs(expr.right, refs);
      break;
    case "unary":
      collectRefs(expr.operand, refs);
      break;
    case "do":
      for (const step of expr.steps) collectRefs(step.expr, refs);
      break;
    case "for":
      collectRefs(expr.collection, refs);
      if (expr.guard) collectRefs(expr.guard, refs);
      if (expr.fold) collectRefs(expr.fold.init, refs);
      collectRefs(expr.body, refs);
      break;
    case "range":
      collectRefs(expr.start, refs);
      collectRefs(expr.end, refs);
      break;
    case "handle":
      collectRefs(expr.expr, refs);
      for (const arm of expr.arms) {
        collectRefs(arm.body, refs);
      }
      break;
    case "perform":
      collectRefs(expr.expr, refs);
      break;
    case "list":
    case "tuple":
      for (const el of expr.elements) collectRefs(el, refs);
      break;
    case "record":
      for (const f of expr.fields) collectRefs(f.value, refs);
      break;
    case "record-update":
      collectRefs(expr.base, refs);
      for (const f of expr.fields) collectRefs(f.value, refs);
      break;
    case "field-access":
      collectRefs(expr.object, refs);
      break;
  }
}

function collectPatternRefs(pat: Pattern, refs: Set<string>): void {
  switch (pat.tag) {
    case "p-variant":
      refs.add(pat.name);
      for (const a of pat.args) collectPatternRefs(a, refs);
      break;
    case "p-tuple":
      for (const el of pat.elements) collectPatternRefs(el, refs);
      break;
    default:
      break;
  }
}

// ── Rule: unused variables (W100) ──
// Checks let bindings inside function bodies where the bound name
// is never referenced in the body expression.

function checkUnusedVars(expr: Expr, diags: LintDiagnostic[]): void {
  switch (expr.tag) {
    case "let": {
      checkUnusedVars(expr.value, diags);
      if (expr.body) {
        checkUnusedVars(expr.body, diags);
        if (expr.name !== "_") {
          const refs = new Set<string>();
          collectRefs(expr.body, refs);
          if (!refs.has(expr.name)) {
            diags.push({
              code: "W100",
              message: `unused variable '${expr.name}'`,
              location: expr.loc,
            });
          }
        }
      }
      break;
    }
    case "if":
      checkUnusedVars(expr.cond, diags);
      checkUnusedVars(expr.then, diags);
      checkUnusedVars(expr.else, diags);
      break;
    case "match":
      checkUnusedVars(expr.subject, diags);
      for (const arm of expr.arms) {
        checkUnusedVarsInPattern(arm.pattern, arm.body, diags);
        checkUnusedVars(arm.body, diags);
      }
      break;
    case "lambda":
      // Check lambda params for unused (skip _ params)
      for (const p of expr.params) {
        if (p.name !== "_") {
          const refs = new Set<string>();
          collectRefs(expr.body, refs);
          if (!refs.has(p.name)) {
            diags.push({
              code: "W100",
              message: `unused variable '${p.name}'`,
              location: expr.loc,
            });
          }
        }
      }
      checkUnusedVars(expr.body, diags);
      break;
    case "apply":
      checkUnusedVars(expr.fn, diags);
      for (const a of expr.args) checkUnusedVars(a, diags);
      break;
    case "pipeline":
    case "infix":
      checkUnusedVars(expr.left, diags);
      checkUnusedVars(expr.right, diags);
      break;
    case "unary":
      checkUnusedVars(expr.operand, diags);
      break;
    case "do":
      for (let i = 0; i < expr.steps.length; i++) {
        const step = expr.steps[i];
        checkUnusedVars(step.expr, diags);
        if (step.bind && step.bind !== "_") {
          // Check if bind is used in subsequent steps
          const refs = new Set<string>();
          for (let j = i + 1; j < expr.steps.length; j++) {
            collectRefs(expr.steps[j].expr, refs);
          }
          if (!refs.has(step.bind)) {
            diags.push({
              code: "W100",
              message: `unused variable '${step.bind}'`,
              location: step.expr.loc,
            });
          }
        }
      }
      break;
    case "for":
      checkUnusedVars(expr.collection, diags);
      if (expr.guard) checkUnusedVars(expr.guard, diags);
      if (expr.fold) checkUnusedVars(expr.fold.init, diags);
      checkUnusedVars(expr.body, diags);
      break;
    case "range":
      checkUnusedVars(expr.start, diags);
      checkUnusedVars(expr.end, diags);
      break;
    case "handle":
      checkUnusedVars(expr.expr, diags);
      for (const arm of expr.arms) checkUnusedVars(arm.body, diags);
      break;
    case "perform":
      checkUnusedVars(expr.expr, diags);
      break;
    case "list":
    case "tuple":
      for (const el of expr.elements) checkUnusedVars(el, diags);
      break;
    case "record":
      for (const f of expr.fields) checkUnusedVars(f.value, diags);
      break;
    case "record-update":
      checkUnusedVars(expr.base, diags);
      for (const f of expr.fields) checkUnusedVars(f.value, diags);
      break;
    case "field-access":
      checkUnusedVars(expr.object, diags);
      break;
    default:
      break;
  }
}

function checkUnusedVarsInPattern(pat: Pattern, body: Expr, diags: LintDiagnostic[]): void {
  const refs = new Set<string>();
  collectRefs(body, refs);

  function walkPat(p: Pattern): void {
    switch (p.tag) {
      case "p-var":
        if (p.name !== "_" && !refs.has(p.name)) {
          diags.push({
            code: "W100",
            message: `unused variable '${p.name}'`,
            location: p.loc,
          });
        }
        break;
      case "p-variant":
        for (const a of p.args) walkPat(a);
        break;
      case "p-tuple":
        for (const el of p.elements) walkPat(el);
        break;
      default:
        break;
    }
  }
  walkPat(pat);
}

// ── Rule: unused imports (W101) ──

function checkUnusedImports(program: Program, diags: LintDiagnostic[]): void {
  // Collect all references across all definition bodies
  const allRefs = new Set<string>();
  for (const tl of program.topLevels) {
    if (tl.tag === "definition") {
      collectRefs(tl.body, allRefs);
    }
  }

  // Also collect variant constructor refs from type pattern matching
  // and names used in type signatures
  for (const tl of program.topLevels) {
    if (tl.tag === "definition") {
      for (const p of tl.sig.params) {
        collectTypeRefs(p.type, allRefs);
      }
      collectTypeRefs(tl.sig.returnType, allRefs);
    }
  }

  for (const tl of program.topLevels) {
    if (tl.tag !== "use-decl") continue;
    for (const imp of tl.imports) {
      const usedName = imp.alias ?? imp.name;
      if (!allRefs.has(usedName)) {
        diags.push({
          code: "W101",
          message: `unused import '${usedName}'`,
          location: tl.loc,
        });
      }
    }
  }
}

function collectTypeRefs(te: import("./ast.js").TypeExpr | null, refs: Set<string>): void {
  if (!te) return;
  switch (te.tag) {
    case "t-name":
      refs.add(te.name);
      break;
    case "t-list":
      collectTypeRefs(te.element, refs);
      break;
    case "t-tuple":
      for (const el of te.elements) collectTypeRefs(el, refs);
      break;
    case "t-fn":
      collectTypeRefs(te.param, refs);
      collectTypeRefs(te.result, refs);
      break;
    case "t-generic":
      refs.add(te.name);
      for (const a of te.args) collectTypeRefs(a, refs);
      break;
    case "t-record":
      for (const f of te.fields) collectTypeRefs(f.type, refs);
      break;
    case "t-union":
      collectTypeRefs(te.left, refs);
      collectTypeRefs(te.right, refs);
      break;
    case "t-refined":
      collectTypeRefs(te.base, refs);
      break;
  }
}

// ── Rule: shadowed bindings (W102) ──

function checkShadowedBindings(expr: Expr, scope: Set<string>, diags: LintDiagnostic[]): void {
  switch (expr.tag) {
    case "let": {
      checkShadowedBindings(expr.value, scope, diags);
      if (expr.name !== "_" && scope.has(expr.name)) {
        diags.push({
          code: "W102",
          message: `'${expr.name}' shadows an existing binding`,
          location: expr.loc,
        });
      }
      if (expr.body) {
        const inner = new Set(scope);
        if (expr.name !== "_") inner.add(expr.name);
        checkShadowedBindings(expr.body, inner, diags);
      }
      break;
    }
    case "if":
      checkShadowedBindings(expr.cond, scope, diags);
      checkShadowedBindings(expr.then, scope, diags);
      checkShadowedBindings(expr.else, scope, diags);
      break;
    case "match":
      checkShadowedBindings(expr.subject, scope, diags);
      for (const arm of expr.arms) {
        const armScope = new Set(scope);
        addPatternBindings(arm.pattern, armScope);
        checkShadowedBindings(arm.body, armScope, diags);
      }
      break;
    case "lambda": {
      const lamScope = new Set(scope);
      for (const p of expr.params) {
        if (p.name !== "_" && scope.has(p.name)) {
          diags.push({
            code: "W102",
            message: `'${p.name}' shadows an existing binding`,
            location: expr.loc,
          });
        }
        if (p.name !== "_") lamScope.add(p.name);
      }
      checkShadowedBindings(expr.body, lamScope, diags);
      break;
    }
    case "apply":
      checkShadowedBindings(expr.fn, scope, diags);
      for (const a of expr.args) checkShadowedBindings(a, scope, diags);
      break;
    case "pipeline":
    case "infix":
      checkShadowedBindings(expr.left, scope, diags);
      checkShadowedBindings(expr.right, scope, diags);
      break;
    case "unary":
      checkShadowedBindings(expr.operand, scope, diags);
      break;
    case "do": {
      let doScope = new Set(scope);
      for (const step of expr.steps) {
        checkShadowedBindings(step.expr, doScope, diags);
        if (step.bind && step.bind !== "_") {
          if (doScope.has(step.bind)) {
            diags.push({
              code: "W102",
              message: `'${step.bind}' shadows an existing binding`,
              location: step.expr.loc,
            });
          }
          doScope = new Set(doScope);
          doScope.add(step.bind);
        }
      }
      break;
    }
    case "for":
      checkShadowedBindings(expr.collection, scope, diags);
      if (expr.guard) checkShadowedBindings(expr.guard, scope, diags);
      if (expr.fold) checkShadowedBindings(expr.fold.init, scope, diags);
      checkShadowedBindings(expr.body, scope, diags);
      break;
    case "range":
      checkShadowedBindings(expr.start, scope, diags);
      checkShadowedBindings(expr.end, scope, diags);
      break;
    case "handle":
      checkShadowedBindings(expr.expr, scope, diags);
      for (const arm of expr.arms) {
        const armScope = new Set(scope);
        for (const p of arm.params) {
          if (p.name !== "_") armScope.add(p.name);
        }
        if (arm.resumeName) armScope.add(arm.resumeName);
        checkShadowedBindings(arm.body, armScope, diags);
      }
      break;
    case "perform":
      checkShadowedBindings(expr.expr, scope, diags);
      break;
    case "list":
    case "tuple":
      for (const el of expr.elements) checkShadowedBindings(el, scope, diags);
      break;
    case "record":
      for (const f of expr.fields) checkShadowedBindings(f.value, scope, diags);
      break;
    case "record-update":
      checkShadowedBindings(expr.base, scope, diags);
      for (const f of expr.fields) checkShadowedBindings(f.value, scope, diags);
      break;
    case "field-access":
      checkShadowedBindings(expr.object, scope, diags);
      break;
    default:
      break;
  }
}

function addPatternBindings(pat: Pattern, scope: Set<string>): void {
  switch (pat.tag) {
    case "p-var":
      if (pat.name !== "_") scope.add(pat.name);
      break;
    case "p-variant":
      for (const a of pat.args) addPatternBindings(a, scope);
      break;
    case "p-tuple":
      for (const el of pat.elements) addPatternBindings(el, scope);
      break;
    default:
      break;
  }
}

// ── Rule: missing type annotations on pub functions (W103) ──

function checkMissingPubAnnotations(program: Program, diags: LintDiagnostic[]): void {
  for (const tl of program.topLevels) {
    if (tl.tag !== "definition" || !tl.pub) continue;
    for (const p of tl.sig.params) {
      if (p.type.tag === "t-name" && p.type.name === "_") {
        diags.push({
          code: "W103",
          message: `pub function '${tl.name}' has untyped parameter '${p.name}'`,
          location: tl.loc,
        });
      }
    }
    if (tl.sig.returnType.tag === "t-name" && tl.sig.returnType.name === "_") {
      diags.push({
        code: "W103",
        message: `pub function '${tl.name}' is missing return type annotation`,
        location: tl.loc,
      });
    }
  }
}

// ── Rule: unreachable match arms (W104) ──

function checkUnreachableArms(expr: Expr, diags: LintDiagnostic[]): void {
  switch (expr.tag) {
    case "match": {
      checkUnreachableArms(expr.subject, diags);
      let catchAllSeen = false;
      for (const arm of expr.arms) {
        if (catchAllSeen) {
          diags.push({
            code: "W104",
            message: "unreachable match arm after catch-all pattern",
            location: arm.pattern.loc,
          });
        }
        if (arm.pattern.tag === "p-wildcard" || arm.pattern.tag === "p-var") {
          catchAllSeen = true;
        }
        checkUnreachableArms(arm.body, diags);
      }
      break;
    }
    case "let":
      checkUnreachableArms(expr.value, diags);
      if (expr.body) checkUnreachableArms(expr.body, diags);
      break;
    case "if":
      checkUnreachableArms(expr.cond, diags);
      checkUnreachableArms(expr.then, diags);
      checkUnreachableArms(expr.else, diags);
      break;
    case "lambda":
      checkUnreachableArms(expr.body, diags);
      break;
    case "apply":
      checkUnreachableArms(expr.fn, diags);
      for (const a of expr.args) checkUnreachableArms(a, diags);
      break;
    case "pipeline":
    case "infix":
      checkUnreachableArms(expr.left, diags);
      checkUnreachableArms(expr.right, diags);
      break;
    case "unary":
      checkUnreachableArms(expr.operand, diags);
      break;
    case "do":
      for (const step of expr.steps) checkUnreachableArms(step.expr, diags);
      break;
    case "for":
      checkUnreachableArms(expr.collection, diags);
      if (expr.guard) checkUnreachableArms(expr.guard, diags);
      if (expr.fold) checkUnreachableArms(expr.fold.init, diags);
      checkUnreachableArms(expr.body, diags);
      break;
    case "range":
      checkUnreachableArms(expr.start, diags);
      checkUnreachableArms(expr.end, diags);
      break;
    case "handle":
      checkUnreachableArms(expr.expr, diags);
      for (const arm of expr.arms) checkUnreachableArms(arm.body, diags);
      break;
    case "perform":
      checkUnreachableArms(expr.expr, diags);
      break;
    case "list":
    case "tuple":
      for (const el of expr.elements) checkUnreachableArms(el, diags);
      break;
    case "record":
      for (const f of expr.fields) checkUnreachableArms(f.value, diags);
      break;
    case "record-update":
      checkUnreachableArms(expr.base, diags);
      for (const f of expr.fields) checkUnreachableArms(f.value, diags);
      break;
    case "field-access":
      checkUnreachableArms(expr.object, diags);
      break;
    default:
      break;
  }
}

// ── Rule: empty effect handlers (W105) ──

function checkEmptyHandlers(expr: Expr, diags: LintDiagnostic[]): void {
  switch (expr.tag) {
    case "handle": {
      // An empty handler has no operation arms (only possibly a "return" arm)
      const opArms = expr.arms.filter(a => a.name !== "return");
      if (opArms.length === 0) {
        diags.push({
          code: "W105",
          message: "effect handler has no operation arms",
          location: expr.loc,
        });
      }
      checkEmptyHandlers(expr.expr, diags);
      for (const arm of expr.arms) checkEmptyHandlers(arm.body, diags);
      break;
    }
    case "let":
      checkEmptyHandlers(expr.value, diags);
      if (expr.body) checkEmptyHandlers(expr.body, diags);
      break;
    case "if":
      checkEmptyHandlers(expr.cond, diags);
      checkEmptyHandlers(expr.then, diags);
      checkEmptyHandlers(expr.else, diags);
      break;
    case "match":
      checkEmptyHandlers(expr.subject, diags);
      for (const arm of expr.arms) checkEmptyHandlers(arm.body, diags);
      break;
    case "lambda":
      checkEmptyHandlers(expr.body, diags);
      break;
    case "apply":
      checkEmptyHandlers(expr.fn, diags);
      for (const a of expr.args) checkEmptyHandlers(a, diags);
      break;
    case "pipeline":
    case "infix":
      checkEmptyHandlers(expr.left, diags);
      checkEmptyHandlers(expr.right, diags);
      break;
    case "unary":
      checkEmptyHandlers(expr.operand, diags);
      break;
    case "do":
      for (const step of expr.steps) checkEmptyHandlers(step.expr, diags);
      break;
    case "for":
      checkEmptyHandlers(expr.collection, diags);
      if (expr.guard) checkEmptyHandlers(expr.guard, diags);
      if (expr.fold) checkEmptyHandlers(expr.fold.init, diags);
      checkEmptyHandlers(expr.body, diags);
      break;
    case "range":
      checkEmptyHandlers(expr.start, diags);
      checkEmptyHandlers(expr.end, diags);
      break;
    case "perform":
      checkEmptyHandlers(expr.expr, diags);
      break;
    case "list":
    case "tuple":
      for (const el of expr.elements) checkEmptyHandlers(el, diags);
      break;
    case "record":
      for (const f of expr.fields) checkEmptyHandlers(f.value, diags);
      break;
    case "record-update":
      checkEmptyHandlers(expr.base, diags);
      for (const f of expr.fields) checkEmptyHandlers(f.value, diags);
      break;
    case "field-access":
      checkEmptyHandlers(expr.object, diags);
      break;
    default:
      break;
  }
}

// ── Rule: builtin name shadowing (W106) ──

const BUILTIN_NAMES: Set<string> = new Set(
  BUILTIN_REGISTRY.map(e => e.name)
);

function checkBuiltinShadow(program: Program, diags: LintDiagnostic[]): void {
  for (const tl of program.topLevels) {
    if (tl.tag !== "definition") continue;
    if (BUILTIN_NAMES.has(tl.name)) {
      diags.push({
        code: "W106",
        message: `function '${tl.name}' shadows builtin '${tl.name}' — this may cause infinite recursion when the corresponding operator dispatches to '${tl.name}'`,
        location: tl.loc,
      });
    }
  }
}

// ── Public API ──

export type LintOptions = {
  enabledRules?: Set<string>;  // rule codes (W100, W101, ...) or rule names
  disabledRules?: Set<string>;
};

function isRuleEnabled(code: string, opts: LintOptions): boolean {
  const name = LINT_RULES[code];
  if (opts.enabledRules) {
    return opts.enabledRules.has(code) || opts.enabledRules.has(name);
  }
  if (opts.disabledRules) {
    return !opts.disabledRules.has(code) && !opts.disabledRules.has(name);
  }
  return true;
}

export function lint(program: Program, opts: LintOptions = {}): LintDiagnostic[] {
  const diags: LintDiagnostic[] = [];

  // Build initial scope of top-level names (function defs, type constructors, builtins)
  const topScope = new Set<string>();
  for (const tl of program.topLevels) {
    if (tl.tag === "definition") topScope.add(tl.name);
    if (tl.tag === "type-decl") {
      for (const v of tl.variants) topScope.add(v.name);
    }
    if (tl.tag === "effect-decl") {
      topScope.add(tl.name);
      for (const op of tl.ops) topScope.add(op.name);
    }
    if (tl.tag === "use-decl") {
      for (const imp of tl.imports) topScope.add(imp.alias ?? imp.name);
    }
  }

  // Per-definition checks
  for (const tl of program.topLevels) {
    if (tl.tag !== "definition") continue;

    // Build scope for this function (top-level names + params)
    const fnScope = new Set(topScope);
    for (const p of tl.sig.params) {
      if (p.name !== "_") fnScope.add(p.name);
    }

    if (isRuleEnabled("W100", opts)) checkUnusedVars(tl.body, diags);
    if (isRuleEnabled("W102", opts)) checkShadowedBindings(tl.body, fnScope, diags);
    if (isRuleEnabled("W104", opts)) checkUnreachableArms(tl.body, diags);
    if (isRuleEnabled("W105", opts)) checkEmptyHandlers(tl.body, diags);
  }

  // Program-wide checks
  if (isRuleEnabled("W101", opts)) checkUnusedImports(program, diags);
  if (isRuleEnabled("W103", opts)) checkMissingPubAnnotations(program, diags);
  if (isRuleEnabled("W106", opts)) checkBuiltinShadow(program, diags);

  return diags;
}
