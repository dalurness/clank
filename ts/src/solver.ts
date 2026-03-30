// Clank QF_LIA micro-solver
// Lightweight decision procedure for quantifier-free linear integer arithmetic
// Used to verify refinement predicates on Int types at compile time
// Timeout-means-rejection policy: 5s per query

// ── Predicate AST ──

export type Pred =
  | { tag: "var"; name: string }
  | { tag: "lit"; value: number }
  | { tag: "add"; left: Pred; right: Pred }
  | { tag: "sub"; left: Pred; right: Pred }
  | { tag: "mul"; left: Pred; right: Pred }
  | { tag: "neg"; operand: Pred }
  | { tag: "cmp"; op: CmpOp; left: Pred; right: Pred }
  | { tag: "and"; left: Pred; right: Pred }
  | { tag: "or"; left: Pred; right: Pred }
  | { tag: "not"; operand: Pred }
  | { tag: "true" }
  | { tag: "false" };

export type CmpOp = "<" | ">" | "<=" | ">=" | "==" | "!=";

// ── Predicate tokenizer ──

type PToken =
  | { tag: "num"; value: number }
  | { tag: "ident"; value: string }
  | { tag: "op"; value: string }
  | { tag: "lparen" }
  | { tag: "rparen" }
  | { tag: "eof" };

function tokenizePred(s: string): PToken[] {
  const tokens: PToken[] = [];
  let i = 0;

  while (i < s.length) {
    const ch = s[i];

    if (ch === " " || ch === "\t" || ch === "\n") { i++; continue; }

    if (ch >= "0" && ch <= "9") {
      let num = "";
      while (i < s.length && s[i] >= "0" && s[i] <= "9") num += s[i++];
      tokens.push({ tag: "num", value: parseInt(num) });
      continue;
    }

    if ((ch >= "a" && ch <= "z") || (ch >= "A" && ch <= "Z") || ch === "_") {
      let id = "";
      while (i < s.length && ((s[i] >= "a" && s[i] <= "z") || (s[i] >= "A" && s[i] <= "Z") ||
             (s[i] >= "0" && s[i] <= "9") || s[i] === "_" || s[i] === "-")) {
        id += s[i++];
      }
      tokens.push({ tag: "ident", value: id });
      continue;
    }

    if (ch === "(") { tokens.push({ tag: "lparen" }); i++; continue; }
    if (ch === ")") { tokens.push({ tag: "rparen" }); i++; continue; }

    // Two-char operators
    if (i + 1 < s.length) {
      const two = s.slice(i, i + 2);
      if (["<=", ">=", "==", "!=", "&&", "||"].includes(two)) {
        tokens.push({ tag: "op", value: two }); i += 2; continue;
      }
    }

    // Single-char operators
    if (["+", "-", "*", "<", ">", "!"].includes(ch)) {
      tokens.push({ tag: "op", value: ch }); i++; continue;
    }

    i++; // skip unknown
  }

  tokens.push({ tag: "eof" });
  return tokens;
}

// ── Predicate parser (recursive descent) ──

class PredParser {
  private pos = 0;
  constructor(private tokens: PToken[]) {}

  private peek(): PToken { return this.tokens[this.pos] ?? { tag: "eof" }; }
  private advance(): PToken { return this.tokens[this.pos++]; }
  private atOp(v: string): boolean {
    const t = this.peek();
    return t.tag === "op" && t.value === v;
  }

  parse(): Pred {
    return this.parseOr();
  }

  private parseOr(): Pred {
    let left = this.parseAnd();
    while (this.atOp("||")) {
      this.advance();
      left = { tag: "or", left, right: this.parseAnd() };
    }
    return left;
  }

  private parseAnd(): Pred {
    let left = this.parseNot();
    while (this.atOp("&&")) {
      this.advance();
      left = { tag: "and", left, right: this.parseNot() };
    }
    return left;
  }

  private parseNot(): Pred {
    if (this.atOp("!")) {
      this.advance();
      return { tag: "not", operand: this.parseNot() };
    }
    return this.parseCmp();
  }

  private parseCmp(): Pred {
    // Handle implicit v: if first token is a comparison operator, desugar to v <op> ...
    const t = this.peek();
    if (t.tag === "op" && ["<", ">", "<=", ">=", "==", "!="].includes(t.value)) {
      const op = this.advance().value as CmpOp;
      const right = this.parseAddSub();
      return { tag: "cmp", op, left: { tag: "var", name: "v" }, right };
    }

    const left = this.parseAddSub();
    const t2 = this.peek();
    if (t2.tag === "op" && ["<", ">", "<=", ">=", "==", "!="].includes(t2.value)) {
      const op = this.advance().value as CmpOp;
      const right = this.parseAddSub();
      return { tag: "cmp", op, left, right };
    }
    return left;
  }

  private parseAddSub(): Pred {
    let left = this.parseMul();
    while (this.atOp("+") || this.atOp("-")) {
      const op = this.advance().value;
      const right = this.parseMul();
      left = op === "+" ? { tag: "add", left, right } : { tag: "sub", left, right };
    }
    return left;
  }

  private parseMul(): Pred {
    let left = this.parseUnary();
    while (this.atOp("*")) {
      this.advance();
      left = { tag: "mul", left, right: this.parseUnary() };
    }
    return left;
  }

  private parseUnary(): Pred {
    if (this.atOp("-")) {
      this.advance();
      return { tag: "neg", operand: this.parseAtom() };
    }
    return this.parseAtom();
  }

  private parseAtom(): Pred {
    const t = this.peek();

    if (t.tag === "num") {
      this.advance();
      return { tag: "lit", value: t.value };
    }

    if (t.tag === "ident") {
      this.advance();
      if (t.value === "true") return { tag: "true" };
      if (t.value === "false") return { tag: "false" };
      return { tag: "var", name: t.value };
    }

    if (t.tag === "lparen") {
      this.advance();
      const inner = this.parseOr();
      if (this.peek().tag === "rparen") this.advance();
      return inner;
    }

    return { tag: "true" };
  }
}

export function parsePredicate(s: string): Pred {
  return new PredParser(tokenizePred(s)).parse();
}

// ── Variable substitution ──

export function substituteVar(p: Pred, from: string, to: string): Pred {
  switch (p.tag) {
    case "var": return p.name === from ? { tag: "var", name: to } : p;
    case "lit": case "true": case "false": return p;
    case "add": return { tag: "add", left: substituteVar(p.left, from, to), right: substituteVar(p.right, from, to) };
    case "sub": return { tag: "sub", left: substituteVar(p.left, from, to), right: substituteVar(p.right, from, to) };
    case "mul": return { tag: "mul", left: substituteVar(p.left, from, to), right: substituteVar(p.right, from, to) };
    case "neg": return { tag: "neg", operand: substituteVar(p.operand, from, to) };
    case "cmp": return { tag: "cmp", op: p.op, left: substituteVar(p.left, from, to), right: substituteVar(p.right, from, to) };
    case "and": return { tag: "and", left: substituteVar(p.left, from, to), right: substituteVar(p.right, from, to) };
    case "or": return { tag: "or", left: substituteVar(p.left, from, to), right: substituteVar(p.right, from, to) };
    case "not": return { tag: "not", operand: substituteVar(p.operand, from, to) };
  }
}

// ── Direct evaluation (when all variables are bound to constants) ──

function evalArith(p: Pred, env: Map<string, number>): number | null {
  switch (p.tag) {
    case "lit": return p.value;
    case "var": return env.get(p.name) ?? null;
    case "add": { const l = evalArith(p.left, env), r = evalArith(p.right, env); return l !== null && r !== null ? l + r : null; }
    case "sub": { const l = evalArith(p.left, env), r = evalArith(p.right, env); return l !== null && r !== null ? l - r : null; }
    case "mul": { const l = evalArith(p.left, env), r = evalArith(p.right, env); return l !== null && r !== null ? l * r : null; }
    case "neg": { const o = evalArith(p.operand, env); return o !== null ? -o : null; }
    default: return null;
  }
}

function evalPred(p: Pred, env: Map<string, number>): boolean | null {
  switch (p.tag) {
    case "true": return true;
    case "false": return false;
    case "and": {
      const l = evalPred(p.left, env), r = evalPred(p.right, env);
      if (l === null || r === null) return null;
      return l && r;
    }
    case "or": {
      const l = evalPred(p.left, env), r = evalPred(p.right, env);
      if (l === null || r === null) return null;
      return l || r;
    }
    case "not": {
      const o = evalPred(p.operand, env);
      return o !== null ? !o : null;
    }
    case "cmp": {
      const l = evalArith(p.left, env), r = evalArith(p.right, env);
      if (l === null || r === null) return null;
      switch (p.op) {
        case "<": return l < r;
        case ">": return l > r;
        case "<=": return l <= r;
        case ">=": return l >= r;
        case "==": return l === r;
        case "!=": return l !== r;
      }
    }
    default: return null;
  }
}

// ── Linear expressions ──
// LinExpr represents: sum of (coefficient * variable) + constant
// Semantics: LinExpr <= 0

type LinExpr = { vars: Map<string, number>; constant: number };

function linConst(c: number): LinExpr {
  return { vars: new Map(), constant: c };
}

function linVar(name: string): LinExpr {
  const vars = new Map<string, number>();
  vars.set(name, 1);
  return { vars, constant: 0 };
}

function linAdd(a: LinExpr, b: LinExpr): LinExpr {
  const vars = new Map(a.vars);
  for (const [v, c] of b.vars) {
    vars.set(v, (vars.get(v) ?? 0) + c);
  }
  return { vars, constant: a.constant + b.constant };
}

function linSub(a: LinExpr, b: LinExpr): LinExpr {
  const vars = new Map(a.vars);
  for (const [v, c] of b.vars) {
    vars.set(v, (vars.get(v) ?? 0) - c);
  }
  return { vars, constant: a.constant - b.constant };
}

function linScale(a: LinExpr, k: number): LinExpr {
  const vars = new Map<string, number>();
  for (const [v, c] of a.vars) vars.set(v, c * k);
  return { vars, constant: a.constant * k };
}

function toLinExpr(p: Pred): LinExpr | null {
  switch (p.tag) {
    case "lit": return linConst(p.value);
    case "var": return linVar(p.name);
    case "add": {
      const l = toLinExpr(p.left), r = toLinExpr(p.right);
      return l && r ? linAdd(l, r) : null;
    }
    case "sub": {
      const l = toLinExpr(p.left), r = toLinExpr(p.right);
      return l && r ? linSub(l, r) : null;
    }
    case "neg": {
      const o = toLinExpr(p.operand);
      return o ? linScale(o, -1) : null;
    }
    case "mul": {
      const l = toLinExpr(p.left), r = toLinExpr(p.right);
      if (!l || !r) return null;
      if (l.vars.size === 0) return linScale(r, l.constant);
      if (r.vars.size === 0) return linScale(l, r.constant);
      return null; // non-linear
    }
    default: return null;
  }
}

// ── Constraint normalization ──
// All constraints normalized to: LinExpr <= 0

type Constraint = { expr: LinExpr };

function cmpToConstraints(op: CmpOp, left: Pred, right: Pred): Constraint[] | null {
  const l = toLinExpr(left), r = toLinExpr(right);
  if (!l || !r) return null;

  const diff = linSub(l, r); // left - right

  switch (op) {
    case "<=": // left - right <= 0
      return [{ expr: diff }];
    case "<": // left < right → left - right <= -1 (integers) → (left - right + 1) <= 0
      return [{ expr: linAdd(diff, linConst(1)) }];
    case ">=": // right - left <= 0
      return [{ expr: linSub(r, l) }];
    case ">": // right < left → (right - left + 1) <= 0
      return [{ expr: linAdd(linSub(r, l), linConst(1)) }];
    case "==": // left <= right AND right <= left
      return [{ expr: diff }, { expr: linSub(r, l) }];
    case "!=":
      return null; // handled by preprocessing
  }
}

// ── Predicate normalization ──

function negatePred(p: Pred): Pred {
  switch (p.tag) {
    case "true": return { tag: "false" };
    case "false": return { tag: "true" };
    case "not": return p.operand;
    case "and": return { tag: "or", left: negatePred(p.left), right: negatePred(p.right) };
    case "or": return { tag: "and", left: negatePred(p.left), right: negatePred(p.right) };
    case "cmp": {
      const negOps: Record<CmpOp, CmpOp> = {
        "<": ">=", ">": "<=", "<=": ">", ">=": "<", "==": "!=", "!=": "=="
      };
      return { tag: "cmp", op: negOps[p.op], left: p.left, right: p.right };
    }
    default: return { tag: "not", operand: p };
  }
}

// Push negations to atoms and normalize
function normalize(p: Pred): Pred {
  switch (p.tag) {
    case "not": return negateNormalized(normalize(p.operand));
    case "and": return { tag: "and", left: normalize(p.left), right: normalize(p.right) };
    case "or": return { tag: "or", left: normalize(p.left), right: normalize(p.right) };
    default: return p;
  }
}

function negateNormalized(p: Pred): Pred {
  switch (p.tag) {
    case "true": return { tag: "false" };
    case "false": return { tag: "true" };
    case "and": return { tag: "or", left: negateNormalized(p.left), right: negateNormalized(p.right) };
    case "or": return { tag: "and", left: negateNormalized(p.left), right: negateNormalized(p.right) };
    case "cmp": {
      const negOps: Record<CmpOp, CmpOp> = {
        "<": ">=", ">": "<=", "<=": ">", ">=": "<", "==": "!=", "!=": "=="
      };
      return { tag: "cmp", op: negOps[p.op], left: p.left, right: p.right };
    }
    case "not": return normalize(p.operand);
    default: return { tag: "not", operand: p };
  }
}

// Eliminate != by converting to (< || >)
function eliminateNe(p: Pred): Pred {
  switch (p.tag) {
    case "cmp":
      if (p.op === "!=") {
        return {
          tag: "or",
          left: { tag: "cmp", op: "<", left: p.left, right: p.right },
          right: { tag: "cmp", op: ">", left: p.left, right: p.right },
        };
      }
      return p;
    case "and": return { tag: "and", left: eliminateNe(p.left), right: eliminateNe(p.right) };
    case "or": return { tag: "or", left: eliminateNe(p.left), right: eliminateNe(p.right) };
    case "not": return { tag: "not", operand: eliminateNe(p.operand) };
    default: return p;
  }
}

// ── Disjunctive Normal Form ──

type Conjunct = Pred[]; // conjunction of atomic predicates

function toDNF(p: Pred): Conjunct[] {
  switch (p.tag) {
    case "true": return [[]];
    case "false": return [];
    case "and": {
      const leftDNF = toDNF(p.left);
      const rightDNF = toDNF(p.right);
      const result: Conjunct[] = [];
      for (const l of leftDNF) {
        for (const r of rightDNF) {
          result.push([...l, ...r]);
        }
      }
      return result;
    }
    case "or":
      return [...toDNF(p.left), ...toDNF(p.right)];
    case "cmp":
      return [[p]];
    default:
      return [[p]];
  }
}

// ── Fourier-Motzkin variable elimination ──

function cleanExpr(e: LinExpr): LinExpr {
  const vars = new Map<string, number>();
  for (const [v, c] of e.vars) {
    if (c !== 0) vars.set(v, c);
  }
  return { vars, constant: e.constant };
}

function getVariables(constraints: Constraint[]): Set<string> {
  const vars = new Set<string>();
  for (const c of constraints) {
    for (const [v, coeff] of c.expr.vars) {
      if (coeff !== 0) vars.add(v);
    }
  }
  return vars;
}

function fourierMotzkin(constraints: Constraint[], deadline: number): boolean | null {
  if (Date.now() > deadline) return null;

  constraints = constraints.map(c => ({ expr: cleanExpr(c.expr) }));

  const vars = getVariables(constraints);
  if (vars.size === 0) {
    return constraints.every(c => c.expr.constant <= 0);
  }

  const x = vars.values().next().value!;

  const upper: { coeff: number; rest: LinExpr }[] = [];
  const lower: { coeff: number; rest: LinExpr }[] = [];
  const noX: Constraint[] = [];

  for (const c of constraints) {
    const a = c.expr.vars.get(x) ?? 0;
    if (a === 0) {
      noX.push(c);
    } else {
      const rest: LinExpr = { vars: new Map(c.expr.vars), constant: c.expr.constant };
      rest.vars.delete(x);
      if (a > 0) {
        upper.push({ coeff: a, rest });
      } else {
        lower.push({ coeff: -a, rest });
      }
    }
  }

  if (upper.length === 0 || lower.length === 0) {
    return fourierMotzkin(noX, deadline);
  }

  // Combine: for upper a*x + R_u <= 0 and lower (-b)*x + R_l <= 0
  // derive: a*R_l + b*R_u <= 0
  const newConstraints: Constraint[] = [...noX];
  for (const u of upper) {
    for (const l of lower) {
      if (Date.now() > deadline) return null;
      const combined = linAdd(linScale(l.rest, u.coeff), linScale(u.rest, l.coeff));
      newConstraints.push({ expr: combined });
    }
  }

  // Guard against combinatorial explosion
  if (newConstraints.length > 10000) return null;

  return fourierMotzkin(newConstraints, deadline);
}

// Check if a conjunction of comparison predicates is satisfiable
function isConjunctSatisfiable(preds: Pred[], deadline: number): boolean | null {
  const constraints: Constraint[] = [];

  for (const p of preds) {
    if (p.tag !== "cmp") return null;

    const cs = cmpToConstraints(p.op, p.left, p.right);
    if (!cs) return null;
    constraints.push(...cs);
  }

  const result = fourierMotzkin(constraints, deadline);
  return result;
}

// ── Public API ──

/**
 * Check if an integer literal satisfies a predicate.
 * The predicate uses 'v' to refer to the value.
 */
export function checkLiteral(value: number, predicate: string): boolean {
  const pred = parsePredicate(predicate);
  const result = evalPred(pred, new Map([["v", value]]));
  return result === true;
}

/**
 * Check if one refinement implies another.
 * Both predicates use 'v' to refer to the value.
 * Returns "valid" if assumption implies conclusion, "invalid" if not,
 * "unknown" if the solver can't determine.
 */
export function checkSubrefinement(
  assumption: string,
  conclusion: string,
  timeoutMs: number = 5000,
): "valid" | "invalid" | "unknown" {
  const deadline = Date.now() + timeoutMs;

  const assump = parsePredicate(assumption);
  const concl = parsePredicate(conclusion);

  // Check: assumption && !conclusion is unsatisfiable?
  // If unsat → implication holds (valid)
  // If sat → implication fails (invalid)
  const negConcl = normalize(negatePred(concl));

  let combined: Pred = { tag: "and", left: assump, right: negConcl };
  combined = normalize(combined);
  combined = eliminateNe(combined);

  const dnf = toDNF(combined);

  if (Date.now() > deadline) return "unknown";
  if (dnf.length > 1000) return "unknown";

  for (const conjunct of dnf) {
    if (Date.now() > deadline) return "unknown";

    const result = isConjunctSatisfiable(conjunct, deadline);
    if (result === null) return "unknown";
    if (result === true) return "invalid";
  }

  return "valid";
}

/**
 * Check if a conclusion predicate (using 'v') holds given multiple string assumptions.
 * Each assumption is a predicate string that may use arbitrary variable names.
 * The conclusion uses 'v' to refer to the value being checked.
 */
export function checkRefinementWithContext(
  assumptions: string[],
  conclusion: string,
  timeoutMs: number = 5000,
): "valid" | "invalid" | "unknown" {
  const assumPreds = assumptions.map(a => parsePredicate(a));
  const conclPred = parsePredicate(conclusion);
  return checkImplication(assumPreds, conclPred, timeoutMs);
}

/**
 * Build an equality predicate string: `varName == expr`.
 * Used for let-binding refinements (e.g., `let y = x + 1` → `y == x + 1`).
 */
export function buildEqualityPred(varName: string, exprStr: string): string {
  return `${varName} == ${exprStr}`;
}

/**
 * Substitute all occurrences of a variable name in a predicate string.
 * Returns a new predicate string with the substitution applied.
 */
export function substitutePredVar(predicate: string, from: string, to: string): string {
  const pred = parsePredicate(predicate);
  const subst = substituteVar(pred, from, to);
  return predToString(subst);
}

/** Convert a Pred AST back to a string representation. */
export function predToString(p: Pred): string {
  switch (p.tag) {
    case "var": return p.name;
    case "lit": return String(p.value);
    case "true": return "true";
    case "false": return "false";
    case "add": return `(${predToString(p.left)} + ${predToString(p.right)})`;
    case "sub": return `(${predToString(p.left)} - ${predToString(p.right)})`;
    case "mul": return `(${predToString(p.left)} * ${predToString(p.right)})`;
    case "neg": return `(-${predToString(p.operand)})`;
    case "cmp": return `(${predToString(p.left)} ${p.op} ${predToString(p.right)})`;
    case "and": return `(${predToString(p.left)} && ${predToString(p.right)})`;
    case "or": return `(${predToString(p.left)} || ${predToString(p.right)})`;
    case "not": return `(!${predToString(p.operand)})`;
  }
}

/**
 * Check refinement implication with multiple assumptions.
 */
export function checkImplication(
  assumptions: Pred[],
  conclusion: Pred,
  timeoutMs: number = 5000,
): "valid" | "invalid" | "unknown" {
  const deadline = Date.now() + timeoutMs;

  const negConcl = normalize(negatePred(conclusion));

  let combined: Pred = negConcl;
  for (const a of assumptions) {
    combined = { tag: "and", left: combined, right: a };
  }
  combined = normalize(combined);
  combined = eliminateNe(combined);

  const dnf = toDNF(combined);

  if (Date.now() > deadline) return "unknown";
  if (dnf.length > 1000) return "unknown";

  for (const conjunct of dnf) {
    if (Date.now() > deadline) return "unknown";

    const result = isConjunctSatisfiable(conjunct, deadline);
    if (result === null) return "unknown";
    if (result === true) return "invalid";
  }

  return "valid";
}
