# Clank Refinement Predicate Language & SMT Encoding Specification

**Task:** TASK-010
**Mode:** Spec
**Date:** 2026-03-14
**Dependencies:** core-syntax (TASK-004), effect-system (TASK-005), type-system (TASK-002)

---

## 1. Overview

Refinement types annotate base types with logical predicates that the type
checker verifies automatically via an SMT solver. A refined type `{T | P}`
denotes values of type `T` that satisfy predicate `P`. The predicate language
is deliberately restricted to a decidable fragment of first-order logic —
quantifier-free linear integer/rational arithmetic plus uninterpreted functions
(QF_LIRA + UF) — so that verification is fully automatic with no proof terms.

### Design Goals

1. **Decidable** — every well-formed predicate is decidable by an SMT solver
2. **Automatic** — no proof terms, no manual annotations beyond the predicate
3. **Terse** — predicates add minimal tokens to type signatures
4. **Composable** — refinements compose with effects and affine types without interference
5. **Inferable** — liquid type inference discovers refinements where possible

---

## 2. Predicate Language

### 2.1 Grammar

The predicate language extends the grammar from core-syntax.md with precise
semantics. All predicates are boolean-valued expressions over a fixed vocabulary.

```ebnf
(* Refined type syntax *)
refined_type  ::= '{' base_type '|' pred '}'
               |  base_type '{' pred '}'          (* shorthand when base obvious *)

(* Predicate expressions *)
pred          ::= pred_or

pred_or       ::= pred_and ( '||' pred_and )*
pred_and      ::= pred_not ( '&&' pred_not )*
pred_not      ::= '!' pred_atom
               |  pred_atom
pred_atom     ::= arith cmp_op arith               (* comparison *)
               |  qual_name '(' arg_list ')'        (* uninterpreted function *)
               |  'true'
               |  'false'
               |  '(' pred ')'

(* Arithmetic expressions *)
arith         ::= term ( ('+' | '-') term )*
term          ::= factor ( ('*' | '/' | '%') factor )*
factor        ::= INT_LIT                           (* integer literal *)
               |  RAT_LIT                           (* rational literal *)
               |  'v'                               (* the refined value itself *)
               |  qual_name                         (* bound variable or measure *)
               |  qual_name '(' arg_list ')'        (* measure application *)
               |  '(' arith ')'

arg_list      ::= arith ( ',' arith )*
               |  (* empty *)

cmp_op        ::= '==' | '!=' | '<' | '>' | '<=' | '>='

qual_name     ::= IDENT ( '.' IDENT )*             (* e.g., xs, r.width *)
```

### 2.2 Value Variable `v`

Inside a refinement predicate, `v` refers to the value being refined.

```
{Int | v > 0}           -- positive integer
{Int | v >= 0 && v < n} -- bounded integer (n from scope)
```

When a parameter is named, the name can be used instead of `v`:

```
div : (n: Int, d: {Int | v > 0}) -> <> Int
-- equivalent:
div : (n: Int, d: {Int | d > 0}) -> <> Int
```

In return-type refinements, `v` refers to the return value:

```
abs : (x: Int) -> <> {Int | v >= 0}
```

### 2.3 Measures

Measures are pure functions that can appear inside predicates. They bridge
value-level computation and the predicate language. Clank defines a fixed set
of built-in measures and allows user-defined measures with restrictions.

#### Built-in Measures

| Measure   | Type           | Description                    |
|-----------|----------------|--------------------------------|
| `len`     | `[a] -> Int`   | List length                    |
| `len`     | `Str -> Int`   | String length                  |
| `fst`     | `(a, b) -> a`  | First element of pair          |
| `snd`     | `(a, b) -> b`  | Second element of pair         |

#### User-Defined Measures

Users can declare measures with the `measure` keyword. Measures must be:
- Total (defined on all inputs of their type)
- Structurally recursive (termination guaranteed)
- Pure (`<>` effect row)

```
measure depth : Tree a -> Int
  = match {
      Leaf(_)    => 0
      Node(l, _, r) => 1 + max (depth l) (depth r)
    }
```

User measures are encoded as uninterpreted functions in SMT with axioms
derived from their definition (see Section 4.4).

### 2.4 Uninterpreted Functions

Predicates may reference uninterpreted function symbols. These are treated
as opaque by the SMT solver — only equalities and specified axioms about them
are used. This supports abstract specifications:

```
type SortedList a = {[a] | sorted(v)}
```

Here `sorted` is uninterpreted — the type checker knows nothing about its
definition, but it can track that if `xs : SortedList a` then `sorted(xs)`
holds.

### 2.5 What Is NOT Allowed

The predicate language deliberately excludes:

| Feature | Why excluded |
|---------|-------------|
| Quantifiers (`forall`, `exists`) | Moves logic out of decidable QF fragment |
| Recursive predicates | Undecidable in general; use measures instead |
| Higher-order functions in predicates | Not encodable in QF_LIRA |
| String operations beyond `len` | String theory is expensive; defer to v2 |
| Floating-point arithmetic | FP theory is complex; use `Rat` which maps to LRA |
| Bitwise operations | Defer to v2 (QF_BV theory) |

---

## 3. Type Syntax Integration

### 3.1 Refined Type Forms

```
-- Standalone refinement type
type Pos = {Int | v > 0}
type NonEmpty a = {[a] | len(v) > 0}
type Bounded = {Int | v >= 0 && v < 256}

-- In function signatures
div   : (n: Int, d: {Int | v > 0}) -> <> Int
mean  : (xs: {[Rat] | len(v) > 0}) -> <> Rat
clamp : (lo: Int, hi: {Int | v >= lo}, x: Int) -> <> {Int | v >= lo && v <= hi}
```

### 3.2 Shorthand Forms

When the base type is clear from context, shortened forms are allowed:

```
-- Full form
div : (n: Int, d: {Int | v > 0}) -> <> Int

-- Shorthand: omit base type in parameter position (inferred from name or context)
div : (n: Int, d: Int{v > 0}) -> <> Int

-- Shorthand: implicit 'v >' when predicate starts with comparison operator
div : (n: Int, d: Int{> 0}) -> <> Int
```

The implicit-`v` shorthand applies when a predicate starts with a comparison
operator. The parser inserts `v` as the left operand:

```
{> 0}        =>  {v > 0}
{>= lo}     =>  {v >= lo}
{!= 0}      =>  {v != 0}
```

This does NOT apply inside compound predicates — only at the top level of the
shorthand form.

### 3.3 Refinement Type Aliases

```
type Pos = {Int | v > 0}
type Nat = {Int | v >= 0}
type NonEmpty a = {[a] | len(v) > 0}
type Idx a = {Int | v >= 0 && v < len(a)}
```

Aliases are expanded at type-checking time. They do not create new types.

---

## 4. SMT-LIB Encoding

### 4.1 Theory Selection

Clank refinements map to SMT-LIB logic **QF_LIRA** (quantifier-free linear
integer and real arithmetic) extended with **UF** (uninterpreted functions)
for measures and abstract predicates. Combined: **QF_UFLIRA**.

| Clank type | SMT sort |
|------------|----------|
| `Int`      | `Int`    |
| `Rat`      | `Real`   |
| `Bool`     | `Bool`   |
| `Str`      | `Int` (via `len` measure only) |
| `[a]`      | `Int` (via `len` measure only) |
| User ADTs  | Uninterpreted sorts + measure axioms |

### 4.2 Encoding Rules

#### Rule 1: Subtyping as Implication

A refined type `{T | P}` is a subtype of `{T | Q}` iff `P => Q` is valid.
The type checker encodes this as:

```smt2
; Check: {Int | v > 0} <: {Int | v >= 0}
(declare-const v Int)
(assert (> v 0))         ; assume P
(assert (not (>= v 0)))  ; negate Q
(check-sat)              ; expect UNSAT (meaning P => Q is valid)
```

If the solver returns UNSAT, the subtyping holds. If SAT, the subtyping
fails and a counterexample is available for error reporting.

#### Rule 2: Function Application

When calling `f : (x: {T | P(x)}) -> ...` with argument `e`, the checker
must verify that `e`'s refinement implies `P`. Given `e : {T | Q(e)}`:

```smt2
; Environment: all in-scope refined variables
(declare-const e T_smt)
(assert Q_e)              ; what we know about e
(assert (not P_e))        ; negate the precondition (with e substituted for x)
(check-sat)               ; UNSAT means the call is safe
```

#### Rule 3: Return Type Checking

For `f : (...) -> {T | P(v)}`, at each return point with expression `e`:

```smt2
; Environment: all parameter refinements
(declare-const x1 T1_smt)
(assert P_x1)             ; parameter preconditions
...
(declare-const v T_smt)
(assert (= v e_encoded))  ; v is the return expression
(assert (not P_v))        ; negate the postcondition
(check-sat)               ; UNSAT means postcondition holds
```

#### Rule 4: Measure Encoding

Built-in measures map directly:

```smt2
; len for lists: uninterpreted function with axiom
(declare-fun len (List) Int)
(assert (>= (len xs) 0))  ; len is always non-negative (axiom)
```

#### Rule 5: Path Conditions

Conditional branches add path conditions to the environment:

```
-- In the 'then' branch of: if x > 0 then ... else ...
-- Path condition: x > 0 is asserted
```

```smt2
(assert (> x 0))  ; path condition from branch
; ... check refinements in this branch
```

#### Rule 6: Let Bindings

A `let y = e in body` introduces `y` with `e`'s refinement:

```smt2
(declare-const y T_smt)
(assert (= y e_encoded))
; y inherits all refinements provable about e
```

### 4.3 Environment Encoding

The SMT environment for a verification query consists of:

1. **Sort declarations** for all types in scope
2. **Constant declarations** for all in-scope refined variables
3. **Assertions** for all known refinements (parameter predicates, path conditions, let-binding equalities)
4. **Measure axioms** (non-negativity of `len`, measure definitions)
5. **The negated goal** (the refinement to verify)

The query is UNSAT iff the refinement holds.

### 4.4 User Measure Axiom Generation

For a user-defined measure:

```
measure depth : Tree a -> Int
  = match {
      Leaf(_)    => 0
      Node(l, _, r) => 1 + max (depth l) (depth r)
    }
```

The compiler generates axioms:

```smt2
(declare-datatypes ((Tree 0)) ((Leaf (leaf-val Int)) (Node (left Tree) (node-val Int) (right Tree))))
(declare-fun depth (Tree) Int)

; Axiom per branch
(assert (forall ((x Int)) (= (depth (Leaf x)) 0)))
(assert (forall ((l Tree) (x Int) (r Tree))
  (= (depth (Node l x r))
     (+ 1 (ite (>= (depth l) (depth r)) (depth l) (depth r))))))

; Structural axiom: depth is non-negative
(assert (forall ((t Tree)) (>= (depth t) 0)))
```

Note: these axioms use quantifiers, which moves outside QF_UFLIRA. This is
acceptable because measure axioms are structurally recursive (guaranteed
terminating) and the quantifier instantiation is guided by the E-matching
heuristics of modern SMT solvers. The checker controls instantiation depth.

### 4.5 Nonlinear Arithmetic

Multiplication of two variables (`x * y`) is outside linear arithmetic.
Clank handles this pragmatically:

- **Constant multiplication** (`3 * x`) is linear — always supported
- **Variable × variable** (`x * y`) triggers a theory upgrade to QF_NIA
  (nonlinear integer arithmetic) which is undecidable in general
- **Policy**: The checker attempts QF_NIA with a timeout. If the solver times
  out, the refinement is rejected with a "could not verify" error and the
  programmer must simplify the predicate or add an intermediate binding

This avoids unsoundness: unverifiable refinements are rejected, never silently
accepted.

---

## 5. Liquid Type Inference

### 5.1 Overview

Clank uses liquid type inference (Rondon, Kawaguchi, Jhala 2008) to
automatically infer refinement predicates where the programmer omits them.
This dramatically reduces annotation burden — most internal bindings need
no refinement annotations.

### 5.2 Algorithm Sketch

**Input:** A program with some types annotated with refinements (at module
boundaries, key function signatures) and some unannotated.

**Output:** Refinements for all unannotated types, or an error if no valid
refinement exists.

**Steps:**

1. **Template generation**: For each unannotated type, create a template
   `{T | κ}` where `κ` is a refinement variable (unknown predicate).

2. **Qualifier mining**: Extract a set of candidate predicates `Q` from:
   - Predicates in user annotations (`v > 0`, `len(v) > 0`, etc.)
   - Comparisons in branch conditions (`x > 0`, `n == 0`, etc.)
   - Arithmetic relationships between variables (`v == x + y`, etc.)
   - Built-in qualifiers (`v >= 0` for `len`, `v > 0` for divisors, etc.)

3. **Constraint generation**: Walk the program and generate subtyping
   constraints of the form `{T | P} <: {T | κ}` or `{T | κ} <: {T | Q}`.
   These become horn clauses over refinement variables.

4. **Constraint solving**: For each refinement variable `κ`, find the
   strongest conjunction of qualifiers from `Q` that satisfies all
   constraints. This is done by:
   - Start with `κ = Q1 && Q2 && ... && Qn` (all qualifiers)
   - For each constraint involving `κ`, check if it holds via SMT
   - Remove qualifiers that cause constraint violations
   - Iterate to fixpoint

5. **Solution extraction**: Replace each `κ` with the solved conjunction.

### 5.3 What Gets Inferred vs. What Requires Annotation

| Position | Inference | Annotation needed? |
|----------|-----------|-------------------|
| Local `let` bindings | Fully inferred | No |
| Lambda parameters | Inferred from call sites | No |
| Top-level function parameters | Inferred if single module | Recommended at `pub` boundaries |
| Top-level return types | Inferred from body | Recommended at `pub` boundaries |
| `pub` function signatures | Could infer but policy says don't | **Yes** — module contracts must be explicit |
| Type aliases | N/A | **Yes** — aliases define contracts |

### 5.4 Inference Limitations

- Inference only searches conjunctions of mined qualifiers — it cannot
  invent predicates not present in the source or built-in set
- Nonlinear predicates are rarely inferred (qualifiers are linear by default)
- Inference may produce weaker-than-optimal refinements (safe but imprecise)
- In ambiguous cases, the checker falls back to the trivial refinement `true`

---

## 6. Interaction with Effects

### 6.1 Orthogonality

Refinements and effects are orthogonal type system features that compose
without interference:

```
-- Refinement on return type + effect annotation
read-pos : (path: Str) -> <io, exn> {Int | v > 0}

-- Refinement on parameter + effect annotation
safe-div : (n: Int, d: {Int | v > 0}) -> <> Int

-- Both combined
bounded-read : (path: Str, max: {Int | v > 0}) -> <io, exn> {[Byte] | len(v) <= max}
```

The type checker verifies refinements and effects independently:
- **Refinement checking**: SMT queries for subtyping obligations
- **Effect checking**: Row unification for effect obligations

These two passes share the typing context but do not interfere.

### 6.2 Effects in Measures

Measures must be pure (`<>` effect row). This is enforced syntactically —
the `measure` keyword implies purity. A measure that performs effects is
a type error.

### 6.3 Effect Handlers and Refinements

When an effect handler transforms a computation, refinements on the
computation's return type flow through the handler's `return` clause:

```
-- try : (() -> <exn[E] | e> {Int | v > 0}) -> <e> {Int | v > 0}?
-- The refinement v > 0 is preserved in the Some case
```

The handler's return clause type must be compatible with the input
refinement. The type checker verifies this as a standard subtyping check.

---

## 7. Interaction with Affine Types

### 7.1 Refinements on Affine Values

Affine (move) semantics and refinements are independent concerns:

```
-- File handle with a refinement (hypothetical size tracking)
type SizedFile = {File | size(v) > 0}

open : (path: Str) -> <io, exn> File           -- File is affine
read : (&File) -> <io, exn> Str                -- borrow does not consume
close : (File) -> <io> ()                       -- consumes the File
```

Refinements describe value properties. Affine tracking describes usage
properties. They do not conflict:

- A refined affine value `{T | P}` must be used at most once (affine) AND
  satisfies predicate `P` (refinement)
- Borrowing (`&T`) preserves the refinement — a borrow of `{T | P}` has
  type `{&T | P}`
- Consuming an affine value does not affect refinement checking of other
  values in scope

### 7.2 Refinements Cannot Reference Moved Values

After a value `x` is moved (consumed by an affine operation), it leaves
scope. Any refinement that references `x` becomes invalid. The type checker
handles this by removing `x` from the SMT environment after the move point:

```
use-file : (f: File, g: File) -> <io> ()
  = close f       -- f is consumed here
    close g       -- f cannot appear in any refinement after this point
```

This is enforced by the affine checker before refinement checking — moved
variables are simply absent from the environment.

---

## 8. SMT Solver Recommendation

### 8.1 Options Evaluated

| Option | Pros | Cons |
|--------|------|------|
| **Z3** | Most powerful, best theory support, mature | Large binary (~50MB), C++ dependency, LGPL license |
| **CVC5** | Strong, active development, BSD license | Similar size to Z3, fewer users |
| **Yices2** | Fast for QF_LIA/LRA, small | Less theory coverage, GPL license |
| **Custom lightweight** | Tiny, no dependency, full control | Significant engineering, limited theories |
| **Bundled micro-solver** | Small, covers core use cases | Must implement and maintain |

### 8.2 Recommendation: Z3 as External Dependency, Micro-Solver as Fallback

**Primary solver: Z3** (invoked as external process via SMT-LIB2 text protocol)

Rationale:
- Clank's predicate language maps directly to QF_UFLIRA, Z3's strongest theory
- Z3 handles all edge cases (nonlinear fallback, quantified measure axioms)
- External process invocation avoids linking — Z3 is a runtime dependency, not
  a build dependency
- The SMT-LIB2 text protocol is standardized — any conforming solver works

**Fallback: built-in micro-solver** for simple cases

For predicates that are pure linear arithmetic without measures or
uninterpreted functions, a simple Simplex-based solver can handle the
common cases without Z3:

- `v > 0`, `v >= lo && v <= hi`, `v == x + 1`
- Constant propagation and basic interval arithmetic
- Estimated implementation: ~500 lines

This allows Clank to verify the majority of refinements without Z3 installed,
with Z3 required only for complex predicates (measures, uninterpreted functions,
nonlinear arithmetic).

### 8.3 Solver Interaction Protocol

```
1. Type checker collects verification obligations (VCs) during type checking
2. VCs are batched per function (one SMT context per function)
3. Each VC is encoded as SMT-LIB2 text and sent to the solver
4. Solver responses: UNSAT (VC holds), SAT (VC fails, extract model), UNKNOWN/TIMEOUT
5. SAT model is used to construct a counterexample for the error message
6. UNKNOWN/TIMEOUT triggers "could not verify" error with the unprovable predicate
```

### 8.4 Timeout Policy

- Per-query timeout: **5 seconds** (configurable)
- If a query times out, it is treated as a verification failure, not silently accepted
- Error message indicates "verification timed out" and suggests simplifying the predicate
- Total solver time per module is bounded at **60 seconds**

---

## 9. Error Messages

Refinement type errors produce structured JSON (per Clank's agent-native
tooling principle) with counterexample values when available.

### 9.1 Subtyping Failure

```json
{
  "kind": "refinement_error",
  "subkind": "subtype_failure",
  "location": {"file": "math.clk", "line": 12, "col": 15},
  "message": "Cannot verify {Int | v >= -5} <: {Int | v > 0}",
  "expected": "v > 0",
  "actual": "v >= -5",
  "counterexample": {"v": -5},
  "hint": "The argument may be negative; add a guard or strengthen the refinement"
}
```

### 9.2 Precondition Failure

```json
{
  "kind": "refinement_error",
  "subkind": "precondition_failure",
  "location": {"file": "main.clk", "line": 7, "col": 10},
  "function": "div",
  "parameter": "d",
  "message": "Cannot verify precondition: d > 0",
  "context": "d has refinement: true (no constraint)",
  "counterexample": {"d": 0},
  "hint": "The divisor is unconstrained; it may be zero"
}
```

### 9.3 Verification Timeout

```json
{
  "kind": "refinement_error",
  "subkind": "timeout",
  "location": {"file": "crypto.clk", "line": 42, "col": 5},
  "message": "Verification timed out after 5000ms",
  "predicate": "v == x * y * z + w",
  "hint": "Nonlinear arithmetic may cause timeouts; consider introducing intermediate let-bindings"
}
```

---

## 10. Complete Examples

### 10.1 Safe Division

```
div : (n: Int, d: {Int | v > 0}) -> <> Int
  = n d /

-- Calling div safely
safe-div : (n: Int, d: Int) -> <> Int?
  = d 0 gt
    [n d div some]
    [none]
    if
```

SMT encoding for the call `n d div` inside the `then` branch:

```smt2
(set-logic QF_LIA)
(declare-const n Int)
(declare-const d Int)
; Path condition from branch: d > 0
(assert (> d 0))
; Goal: verify precondition d > 0
(assert (not (> d 0)))
(check-sat)
; => UNSAT (precondition verified)
```

### 10.2 List Head with NonEmpty

```
type NonEmpty a = {[a] | len(v) > 0}

safe-head : (xs: NonEmpty a) -> <> a
  = xs head

-- Caller must prove non-emptiness
first-or : (xs: [a], default: a) -> <> a
  = xs len 0 gt
    [xs safe-head]
    [default]
    if
```

SMT encoding for `xs safe-head` in the `then` branch:

```smt2
(set-logic QF_UFLIA)
(declare-fun len (List) Int)
(assert (forall ((x List)) (>= (len x) 0)))  ; len axiom
(declare-const xs List)
; Path condition: len(xs) > 0
(assert (> (len xs) 0))
; Goal: verify len(xs) > 0 (NonEmpty precondition)
(assert (not (> (len xs) 0)))
(check-sat)
; => UNSAT
```

### 10.3 Postcondition: Clamp

```
clamp : (lo: Int, hi: {Int | v >= lo}, x: Int) -> <> {Int | v >= lo && v <= hi}
  = x lo lt [lo] [x hi gt [hi] [x] if] if
```

The type checker verifies three return points:

1. Return `lo` when `x < lo`: need `lo >= lo && lo <= hi` — trivially `lo >= lo`, and `hi >= lo` from precondition.
2. Return `hi` when `x > hi`: need `hi >= lo && hi <= hi` — `hi >= lo` from precondition, `hi <= hi` trivially.
3. Return `x` otherwise: need `x >= lo && x <= hi` — from path conditions `!(x < lo)` i.e. `x >= lo`, and `!(x > hi)` i.e. `x <= hi`.

### 10.4 Refinements with Effects

```
read-positive : (path: Str) -> <io, exn> {Int | v > 0}
  = path read parse-int
    dup 0 lte
    [drop "expected positive" raise]
    []
    if
```

The type checker verifies:
- In the `then` branch: `raise` is called, so no return value to check (bottom type)
- In the `else` branch: path condition `!(v <= 0)` i.e. `v > 0` — postcondition holds

### 10.5 Measure-Based Refinement

```
measure depth : Tree a -> Int
  = match {
      Leaf(_)       => 0
      Node(l, _, r) => 1 + max (depth l) (depth r)
    }

type Balanced a = {Tree a | balanced(v)}

-- Insert preserves balance (abstract specification)
insert : (x: a, t: Balanced a) -> <> {Tree a | depth(v) <= depth(t) + 1}
```

---

## 11. Verification Condition Generation Summary

| Source construct | VC generated |
|------------------|-------------|
| Function call `f(e)` where `f` expects `{T \| P}` | `Q_e => P[e/v]` |
| Function return `e` with return type `{T \| P}` | `Env => P[e/v]` |
| Let binding `let x = e` | `x` enters env with refinement of `e` |
| Branch `if c then e1 else e2` | `c` added to env in `e1`; `!c` in `e2` |
| Match branch `C(x1,...,xn) => body` | Constructor constraints added to env |
| Type alias expansion `Pos` → `{Int \| v > 0}` | Substituted inline |

`Env` includes: all parameter predicates + path conditions + let-binding
equalities + measure axioms.

---

## 12. Design Rationale

### Why QF_UFLIRA and not a richer logic?

QF_UFLIRA covers the vast majority of practical refinements (bounds checking,
non-null, arithmetic relationships, length constraints) while remaining
decidable and fast. Richer logics (arrays, bitvectors, strings) add
verification power but increase solver unpredictability and timeout risk.
Clank prioritizes predictable verification over maximum expressiveness.

### Why liquid type inference?

Liquid types are the only known refinement type inference algorithm that
is both sound and practical. The qualifier-based approach bounds the search
space — inference cannot invent predicates, only discover combinations of
known ones. This matches Clank's "predictable" philosophy.

### Why Z3 as external process?

In-process linking to Z3 creates build complexity (C++ toolchain, platform
binaries). External process invocation via SMT-LIB2 text protocol is:
- Portable (any OS with Z3 installed)
- Solver-agnostic (swap Z3 for CVC5 without recompilation)
- Debuggable (SMT-LIB2 queries can be saved and replayed)
- Isolated (solver crash doesn't crash the type checker)

### Why a micro-solver fallback?

Agent environments may not have Z3 installed. A 500-line Simplex solver
for QF_LIA handles `v > 0`, `v >= lo && v <= hi`, and similar predicates
that constitute ~80% of refinements in practice. This gives a useful
baseline without external dependencies.

### Why reject on timeout instead of accepting?

Accepting unverifiable refinements would break soundness — the type system
would no longer guarantee that refinements hold. Clank's position:
**unverifiable = rejected**. This is conservative but safe. The error message
guides the user toward a simpler predicate.
