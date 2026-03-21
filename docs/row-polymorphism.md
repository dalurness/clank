# Row Polymorphism and Record System — Formal Specification

**Task:** TASK-018
**Mode:** Spec
**Date:** 2026-03-14
**Builds On:** Queryable records research (TASK-015), Core syntax (TASK-004), Effect system (TASK-005), Type system (TASK-002), Affine types (TASK-007), Interface system (TASK-010)

---

## 1. Overview

This specification extends Clank's record types with **row polymorphism** — the
ability to write functions that operate on any record containing a required set
of fields, regardless of what other fields are present. Row polymorphism is the
foundation for Clank's queryable record system (Phase 1 of the design in
`queryable-records.md`).

### Design Goals

1. **Partial knowledge** — functions declare exactly which fields they need; callers
   can pass records with additional fields
2. **Inferred** — row variables are inferred by the type checker; explicit annotation
   is only required at module boundaries
3. **Composable** — row polymorphism composes with refinement types, effects, affine
   types, and interfaces without interference
4. **Terse** — minimal syntax additions (~5 new grammar productions)
5. **Agent-friendly** — type signatures answer "what fields does this function
   need?" without reading the implementation

### Non-Goals (Out of Scope)

- Field tags and `@tag` annotation syntax (TASK-019, Phase 2)
- Type-level field queries: tag projection, type filtering, Pick/Omit (Phase 3)
- Compile-time introspection words (`fields`, `fields-tagged`, `field-type`)

---

## 2. Concepts

### 2.1 Closed vs Open Records

A **closed record type** specifies exactly which fields are present. No additional
fields are permitted. This is the current behavior:

```
{name: Str, age: Int}   -- closed: exactly two fields
```

An **open record type** specifies a minimum set of required fields plus a **row
variable** representing zero or more additional unknown fields:

```
{name: Str | r}          -- open: at least a name field; r represents the rest
```

### 2.2 Row Variables

A **row variable** (written as a lowercase identifier after `|` in a record type)
stands for an unknown set of additional fields. It is analogous to effect
variables in the effect system — both use row polymorphism, applied to different
domains.

```
r                        -- row variable: stands for zero or more fields
```

Row variables are universally quantified at function definition boundaries, just
like type variables and effect variables. No explicit `forall` is required.

### 2.3 Rows

A **row** is an ordered-by-name, duplicate-free sequence of `label: type` pairs,
optionally terminated by a row variable. Rows are the internal representation;
the surface syntax writes them inside `{...}`.

```
Row ::= ε                              -- empty row (no fields)
      | label: Type, Row               -- field extension
      | r                              -- row variable (open tail)
```

Two rows are equivalent if they contain the same labels with the same types,
regardless of written order:

```
{name: Str, age: Int | r}  ≡  {age: Int, name: Str | r}
```

---

## 3. Syntax

### 3.1 Grammar Additions

The following productions are added or modified from the core grammar
(`core-syntax.md`). Changed productions are marked with `(*)`.

```ebnf
(* ── Record type with optional row variable ── *)
record-type  = '{' field-list '}' ;                       (* modified *)

field-list   = field { ',' field } [ '|' ident ]          (* NEW *)
             | ident                                       (* bare row variable *)
             | ε ;                                         (* empty record *)

field        = ident ':' type-expr ;                       (* unchanged *)

(* ── Record literal with optional spread ── *)
record-lit   = '{' [ field-init-list ] '}' ;               (* modified *)

field-init-list = field-init { ',' field-init }
               [ ',' '..' expr ] ;                         (* NEW: spread *)

field-init   = ident ':' expr ;                            (* unchanged *)

(* ── Record update expression ── *)
record-update = expr '{' field-init { ',' field-init } '}' ;  (* NEW *)

(* ── Field access (unchanged, but documented here) ── *)
field-access = atom '.' ident ;

(* ── Pattern matching: record destructure ── *)
pattern     += '{' pat-field { ',' pat-field } [ '|' ident ] '}' ;  (* NEW *)
pat-field    = ident [ ':' pattern ] ;                      (* NEW *)
```

**Production count impact:** +5 rules (field-list, spread in field-init-list,
record-update, record pattern, pat-field). Total grammar stays well under 100.

### 3.2 Type Expression Integration

The `type-expr` production for records becomes:

```ebnf
type-expr   = ...
            | '{' field-list '}'          (* was: '{' field { ',' field } '}' *)
            | ... ;
```

The `field-list` production replaces the previous inline field sequence, adding
the optional `| ident` tail for row variables.

### 3.3 Syntax Examples

**Type annotations:**

```
{name: Str}                    -- closed record: exactly one field
{name: Str | r}                -- open record: at least name, plus whatever r is
{name: Str, age: Int | r}      -- open record: at least name and age
{r}                            -- open record: any record (r covers all fields)
{}                             -- unit record: no fields
```

**Record literals:**

```
{name: "Ada", age: 36}                -- standard record literal
{name: "Ada", age: 36, ..base}        -- spread: merge remaining fields from base
```

**Record update:**

```
person{age: 37}                        -- new record with age changed
person{name: "Grace", age: 37}         -- multiple field update
```

**Field access:**

```
person.name                            -- access field "name"
get-name(rec) = rec.name               -- in function body
```

**Record patterns:**

```
match person {
  {name, age} => ...                   -- closed destructure (exact match)
  {name | rest} => ...                 -- open destructure (bind remaining to rest)
  {name: n, age: a | _} => ...        -- open destructure with rename, discard rest
}
```

---

## 4. Type System

### 4.1 Kinds

Rows inhabit a separate kind from types:

```
Type  ::= the kind of value types (Int, Str, {name: Str}, etc.)
Row   ::= the kind of record rows (name: Str, age: Int, ...)
```

Row variables have kind `Row`. Record type constructors take a row and produce a
type:

```
{_} : Row -> Type
```

This separation prevents rows from appearing where types are expected and vice
versa, catching errors like `let x : r = ...` (using a row variable as a type).

### 4.2 Typing Judgement

The core typing judgement is extended to track row variable bindings:

```
Γ; Δ ⊢ e : τ ! E
```

Where:
- `Γ` is the term-level typing context (as before)
- `Δ` is the row variable context: `Δ ::= · | Δ, r : Row`
- `τ` is the type
- `E` is the effect row

In practice, row variables are managed in the same namespace as type variables
(both are lowercase identifiers resolved by kind inference). The separation is
conceptual, not syntactic.

### 4.3 Typing Rules for Records

**Record literal (closed):**

```
  Γ ⊢ e₁ : T₁ ! E₁   ...   Γ ⊢ eₙ : Tₙ ! Eₙ
  labels l₁ ... lₙ are pairwise distinct
  ──────────────────────────────────────────────────
  Γ ⊢ {l₁: e₁, ..., lₙ: eₙ} : {l₁: T₁, ..., lₙ: Tₙ} ! E₁ ∪ ... ∪ Eₙ
```

**Field access:**

```
  Γ ⊢ e : {l: T | r} ! E
  ───────────────────────────
  Γ ⊢ e.l : T ! E
```

The record must have field `l` — it may be open (with row variable `r`) or
closed. This rule works uniformly for both: a closed record `{l: T, m: U}` is
a special case where `r` is instantiated to `m: U`.

**Record literal with spread:**

```
  Γ ⊢ e₁ : T₁ ! E₁   ...   Γ ⊢ eₙ : Tₙ ! Eₙ
  Γ ⊢ ebase : {l₁: _, ..., lₙ: _, ... | r} ! Ebase
  labels l₁ ... lₙ are pairwise distinct
  ──────────────────────────────────────────────────
  Γ ⊢ {l₁: e₁, ..., lₙ: eₙ, ..ebase} : {l₁: T₁, ..., lₙ: Tₙ | r'} ! E₁ ∪ ... ∪ Eₙ ∪ Ebase
```

Where `r'` is the row `r` with fields `l₁ ... lₙ` removed (they are overridden
by the explicit field values). The spread base must be a record; explicit fields
override any matching fields from the base.

**Record update:**

```
  Γ ⊢ e : {l₁: T₁, ..., lₙ: Tₙ | r} ! E₁
  Γ ⊢ eᵢ : Tᵢ ! Eᵢ    for each updated field lᵢ
  ──────────────────────────────────────────────────
  Γ ⊢ e{lᵢ: eᵢ, ...} : {l₁: T₁, ..., lₙ: Tₙ | r} ! E₁ ∪ Eᵢ ∪ ...
```

Record update produces a new record of the same type. Updated fields must exist
in the original record and must have the same type. The row variable is preserved.

**Record pattern (closed):**

```
  Γ ⊢ e : {l₁: T₁, ..., lₙ: Tₙ}
  Γ, x₁: T₁, ..., xₙ: Tₙ ⊢ body : τ
  ──────────────────────────────────────
  Γ ⊢ match e { {l₁: x₁, ..., lₙ: xₙ} => body } : τ
```

**Record pattern (open):**

```
  Γ ⊢ e : {l₁: T₁, ..., lₙ: Tₙ | r}
  Γ, x₁: T₁, ..., xₙ: Tₙ, rest: {r} ⊢ body : τ
  ──────────────────────────────────────────────────
  Γ ⊢ match e { {l₁: x₁, ..., lₙ: xₙ | rest} => body } : τ
```

The `rest` variable binds a record containing all fields not explicitly
destructured. Its type is `{r}` — the residual row packaged as a record.

### 4.4 Row Unification

Row unification determines when two record types are compatible. It follows
Rémy-style row unification, adapted from the effect system's row algebra.

**Rule 1 — Empty rows unify:**

```
  ε ≡ ε
```

**Rule 2 — Same head label:**

```
  l: T₁ ≡ l: T₂    when T₁ ≡ T₂
  (l: T, R₁) ≡ (l: T, R₂)    when R₁ ≡ R₂
```

**Rule 3 — Different head labels (row rewriting):**

```
  (l₁: T₁, R₁) ≡ (l₂: T₂, R₂)    when l₁ ≠ l₂
  ⟹  find l₁ in (l₂: T₂, R₂), producing R₂'
      find l₂ in (l₁: T₁, R₁), producing R₁'
      unify T₁ with type of l₁ in R₂
      unify T₂ with type of l₂ in R₁
      unify R₁' with R₂'
```

**Rule 4 — Row variable instantiation:**

```
  (l: T, R) ≡ r    where r is a row variable
  ⟹  r := (l: T, r')    with fresh row variable r'
      unify R with r'
```

**Rule 5 — Row variable to row variable:**

```
  r₁ ≡ r₂    where both are row variables
  ⟹  unify r₁ := r₂    (standard variable unification)
```

**Duplicate label restriction:** A row may not contain the same label twice.
During unification, if instantiating a row variable would introduce a duplicate
label, unification fails:

```
  {name: Str | r} ≡ {name: Int | s}
  ⟹  unification of Str and Int fails (same label, incompatible types)

  {name: Str | r}    where r is instantiated to (name: Int, ...)
  ⟹  ERROR: duplicate label "name" in row
```

### 4.5 Row Polymorphism and Generalization

Row variables are generalized at definition boundaries, following the same
rules as type variables (let-generalization in Hindley-Milner):

```
get-name : ({name: Str | r}) -> <> Str
  = fn(rec) => rec.name
```

The inferred type is `∀r. {name: Str | r} -> <> Str`. The `∀r` is implicit —
Clank does not require explicit quantification.

At each call site, the row variable is instantiated to the concrete residual:

```
let person = {name: "Ada", age: 36, title: "Countess"}
get-name(person)
-- r instantiated to (age: Int, title: Str)
```

### 4.6 Interaction with Type Variables

Row variables and type variables occupy the same namespace (lowercase
identifiers). Kind inference disambiguates:

```
identity : (a) -> <> a                     -- a : Type
get-name : ({name: Str | r}) -> <> Str     -- r : Row
```

A variable appearing in row position has kind `Row`; in type position, kind
`Type`. If the same variable appears in both positions, that is a kind error:

```
-- ERROR: r used as both Row and Type
bad : ({name: r | r}) -> <> r              -- kind conflict
```

### 4.7 Subsumption (Record Width Subtyping)

An open record type is a supertype of any record type that has (at least) the
required fields with compatible types:

```
{name: Str, age: Int, title: Str}  <:  {name: Str | r}
```

This is implemented through row variable instantiation during unification, not
through a separate subtyping relation. When a closed record is passed where an
open record is expected, the row variable is instantiated to the extra fields.

Closed records do NOT subsume open records in the other direction:

```
{name: Str}  ≢  {name: Str | r}    -- unless r = ε
```

A function requiring `{name: Str}` (closed) rejects records with extra fields.
A function requiring `{name: Str | r}` accepts any record with at least `name`.

---

## 5. Record Operations

### 5.1 Creation

Records are created via record literal syntax:

```
{name: "Ada", age: 36}
```

The type is inferred as the closed record `{name: Str, age: Int}`.

Records can also be created with spread to merge fields:

```
let base = {name: "Ada", age: 36}
let extended = {title: "Countess", ..base}
-- type: {title: Str, name: Str, age: Int}
```

Spread copies all fields from the base record into the new record. Explicit
fields override base fields with the same name:

```
let updated = {age: 37, ..base}
-- type: {age: Int, name: Str}   (age overridden)
```

### 5.2 Field Access

Field access uses dot syntax:

```
person.name      -- access field "name"
```

The type checker verifies that the record type contains the accessed field.
For open records, this works through the row unification rules:

```
get-name : ({name: Str | r}) -> <> Str
  = fn(rec) => rec.name
```

Chained access is supported:

```
company.ceo.name     -- access nested field
```

### 5.3 Record Update

Record update creates a new record with specified fields changed:

```
let older = person{age: person.age + 1}
```

The update syntax `expr{field: value, ...}` produces a new record. The original
is not mutated. Updated fields must exist in the record type and the new values
must match the field types.

Record update preserves row polymorphism:

```
increment-age : ({age: Int | r}) -> <> {age: Int | r}
  = fn(rec) => rec{age: rec.age + 1}
```

The return type has the same row variable — the function preserves all fields
it doesn't modify.

### 5.4 Destructuring

Records can be destructured in `let` bindings and `match` arms:

```
let {name, age} = person                   -- closed: exact match
let {name | rest} = person                 -- open: bind rest

match config {
  {host, port | _} => connect(host, port)  -- open: discard rest
}
```

In a closed destructure, all fields must be bound (or wildcard-matched). In an
open destructure, unmatched fields are collected into the rest variable.

Short-hand: when the binding name matches the field name, the `: pattern` part
can be omitted:

```
let {name, age} = person
-- equivalent to: let {name: name, age: age} = person
```

---

## 6. Inference Behavior

### 6.1 Inference of Open vs Closed

The type checker infers **closed** record types for record literals and
**open** record types for function parameters that use field access:

```
-- Inferred closed:
let p = {name: "Ada", age: 36}
-- p : {name: Str, age: Int}         (closed)

-- Inferred open:
f = fn(x) => x.name
-- f : ({name: a | r}) -> <> a       (open, with fresh type variable for field type)
```

This default is correct for the common case: literals create known-shape
records, and functions should accept any record with the fields they use.

### 6.2 Annotation at Module Boundaries

At `pub` definition boundaries, types must be annotated (this is existing Clank
policy). Row-polymorphic signatures use explicit row variables:

```
pub get-name : ({name: Str | r}) -> <> Str
  = fn(rec) => rec.name
```

If the programmer writes a closed type at a module boundary, the function only
accepts exact-match records:

```
pub get-name : ({name: Str}) -> <> Str
  = fn(rec) => rec.name
-- only accepts {name: Str}, not {name: Str, age: Int}
```

This is intentional — it lets module authors control the width contract.

### 6.3 Inference with Multiple Field Accesses

When a function accesses multiple fields, the inferred type accumulates all
required fields in the row:

```
full-name = fn(rec) => rec.first ++ " " ++ rec.last
-- inferred: ({first: Str, last: Str | r}) -> <> Str
```

### 6.4 Principal Types

Row polymorphism with Rémy-style unification preserves principal types — every
well-typed expression has a unique most-general type. This means:

- Type inference is decidable and complete
- The inferred type is always the most permissive (most polymorphic)
- No need for the programmer to choose between competing valid types

---

## 7. Interaction with Existing Features

### 7.1 Effect System

Row polymorphism for records is **independent** of row polymorphism for effects.
Both use the same mathematical framework (Rémy-style row variables) but operate
on different domains:

- Effect rows: sets of effect labels in `<...>`
- Record rows: sets of field labels in `{...}`

They compose orthogonally:

```
read-config : ({path: Str | r}) -> <io, exn> Str
  = fn(rec) => read(rec.path)
-- row variable r: record row (extra fields)
-- effect row: <io, exn> (concrete, no effect variable)

map-field : ({val: a | r}, (a) -> <e> b) -> <e> {val: b | r}
  = fn(rec, f) => rec{val: f(rec.val)}
-- r: record row variable
-- e: effect row variable
-- a, b: type variables
```

### 7.2 Refinement Types

Refinement predicates compose with record types and row polymorphism:

```
type Config = {
  port: Int{> 0 && < 65536},
  host: Str,
  max-retries: Int{>= 0}
}
```

Row polymorphism preserves refinements through open record types:

```
get-port : ({port: Int{> 0 && < 65536} | r}) -> <> Int{> 0 && < 65536}
  = fn(rec) => rec.port
```

The refinement is part of the field's type and is carried through unification.

### 7.3 Affine Types

Records containing affine fields are affine (per affine-types.md §2.3). Row
polymorphism does not change this rule:

```
affine type Handle = {fd: Int, path: Str}

-- This function's parameter is affine because the concrete record may be affine
use-handle : ({fd: Int | r}) -> <io> ()
  = fn(rec) => write-fd(rec.fd)
```

**Affinity and row variables:** A row variable `r` may be instantiated to a row
containing affine fields. The affinity of the concrete record is determined at
the call site, not at the definition site. The definition is polymorphic over
affinity.

**Borrowing open records:**

```
read-name : (&{name: Str | r}) -> <> Str
  = fn(rec) => rec.name
```

Borrowing works as specified in affine-types.md — `&` borrows the entire record,
and field access on a borrowed record returns a copy of the field value (for
unrestricted field types) or a borrow (for affine field types).

### 7.4 Interface System

Interfaces can be implemented for record types, including open records via
constrained impls:

```
impl Show for {name: Str, age: Int} {
  show = fn(self) => "{name: " ++ show(self.name) ++ ", age: " ++ show(self.age) ++ "}"
}
```

Interfaces can also constrain row-polymorphic functions:

```
show-name : ({name: a | r}) -> <> Str where Show a
  = fn(rec) => show(rec.name)
```

**Deriving for records:** The existing `deriving` mechanism works with closed
record types. For open records, deriving is not applicable (you cannot derive
an impl for an unknown set of fields).

### 7.5 Generics (Type Parameters)

Row polymorphism composes with parametric polymorphism:

```
-- Polymorphic in both the field type and the row
get-val : ({val: a | r}) -> <> a
  = fn(rec) => rec.val

-- Parameterized record type
type Labeled<a> = {label: Str, value: a}

-- Row-polymorphic function on parameterized records
relabel : ({label: Str | r}, Str) -> <> {label: Str | r}
  = fn(rec, new-label) => rec{label: new-label}
```

Type parameters and row variables are both universally quantified and resolved
through unification. They do not interfere.

### 7.6 Pattern Matching

Record patterns integrate with existing match semantics:

```
type Result<a, e> = Ok(a) | Err(e)

process : (Result<{name: Str, age: Int}, Str>) -> <> Str
  = fn(r) => match r {
      Ok({name, age}) => name ++ " is " ++ show(age)
      Err(msg) => "Error: " ++ msg
    }
```

Record patterns can be open:

```
extract-name : ({name: Str | r}) -> <> Str
  = fn(rec) => match rec {
      {name | _} => name
    }
```

Exhaustiveness: a single open record pattern `{fields | _}` is always
exhaustive for any record with those fields. A closed record pattern is
exhaustive only for records of that exact shape.

### 7.7 Pipeline Operator

Row-polymorphic functions work naturally with pipelines:

```
person |> get-name |> len |> show |> print

-- Equivalent to:
print(show(len(get-name(person))))
```

Record update can also be pipelined:

```
person |> fn(p) => p{age: p.age + 1} |> fn(p) => p{name: "Grace"}
```

---

## 8. Complete Examples

### 8.1 Basic Row Polymorphism

```
-- Function accepting any record with a "name" field
get-name : ({name: Str | r}) -> <> Str
  = fn(rec) => rec.name

-- Works with different record shapes
let person = {name: "Ada", age: 36}
let company = {name: "Acme", founded: 1920, public: true}

get-name(person)     -- "Ada"
get-name(company)    -- "Acme"
```

### 8.2 Composing Row-Polymorphic Functions

```
-- Each function requires different fields
get-name  : ({name: Str | r}) -> <> Str = fn(rec) => rec.name
get-age   : ({age: Int | r}) -> <> Int = fn(rec) => rec.age

-- Combining: inferred type requires both fields
greet : ({name: Str, age: Int | r}) -> <> Str
  = fn(rec) => get-name(rec) ++ " is " ++ show(get-age(rec))
```

When `greet` calls `get-name(rec)`, the row variable in `get-name`'s type is
instantiated to `(age: Int | r)`. When it calls `get-age(rec)`, the row
variable is instantiated to `(name: Str | r)`. The combined constraint is
`{name: Str, age: Int | r}`.

### 8.3 Record Update Preserving Extra Fields

```
birthday : ({age: Int | r}) -> <> {age: Int | r}
  = fn(rec) => rec{age: rec.age + 1}

let person = {name: "Ada", age: 36, title: "Countess"}
let older = birthday(person)
-- older : {name: Str, age: Int, title: Str}
-- older = {name: "Ada", age: 37, title: "Countess"}
```

### 8.4 With Effects and Refinements

```
type ServerConfig = {
  host: Str,
  port: Int{> 0 && < 65536},
  timeout-ms: Int{> 0}
}

connect : ({host: Str, port: Int{> 0 && < 65536} | r}) -> <io, exn> Connection
  = fn(cfg) => tcp-connect(cfg.host, cfg.port)

-- Works with ServerConfig and any other record with host + port
let conn = connect({host: "localhost", port: 8080})
let conn2 = connect({host: "prod.example.com", port: 443, tls: true, retry: 3})
```

### 8.5 Record Destructuring in Match

```
type Shape
  = Circle({radius: Rat})
  | Rect({width: Rat, height: Rat})

area : (Shape) -> <> Rat
  = fn(s) => match s {
      Circle({radius}) => radius * radius * 3.14159
      Rect({width, height}) => width * height
    }
```

### 8.6 Spread for Record Extension

```
type Base = {name: Str, age: Int}
type Employee = {name: Str, age: Int, dept: Str, salary: Int}

hire : ({name: Str, age: Int | r}, Str, Int) -> <> Employee
  = fn(person, dept, salary) =>
      {dept: dept, salary: salary, ..person}
```

### 8.7 Open Destructuring

```
-- Extract one field, keep the rest
pop-name : ({name: Str | r}) -> <> (Str, {r})
  = fn(rec) => match rec {
      {name | rest} => (name, rest)
    }

let person = {name: "Ada", age: 36, title: "Countess"}
let (name, rest) = pop-name(person)
-- name = "Ada"
-- rest = {age: 36, title: "Countess"}
```

---

## 9. Error Messages

The type checker should produce clear errors for common row-related mistakes.

### 9.1 Missing Field

```
get-name({age: 36})

error[E301]: record missing required field "name"
  --> src/main.clk:5:10
  | get-name expects a record with field "name: Str"
  | but the argument has type {age: Int}
  | hint: add field "name" to the record
```

### 9.2 Field Type Mismatch

```
get-name({name: 42})

error[E302]: field type mismatch for "name"
  --> src/main.clk:5:10
  | expected "name: Str"
  | but found "name: Int"
```

### 9.3 Extra Fields on Closed Record

```
let f : ({name: Str}) -> <> Str = fn(r) => r.name
f({name: "Ada", age: 36})

error[E303]: record has extra fields
  --> src/main.clk:6:3
  | function expects exactly {name: Str}
  | but the argument also has field "age"
  | hint: use an open record type {name: Str | r} to accept extra fields
```

### 9.4 Duplicate Label

```
{name: "Ada", name: "Grace"}

error[E304]: duplicate field "name" in record
  --> src/main.clk:5:15
  | field "name" already defined at column 2
```

---

## 10. Implementation Notes

### 10.1 Representation

At compile time, record types are represented as sorted maps from labels to
types, plus an optional row variable tail. Sorting ensures that structurally
equivalent rows have the same representation regardless of source order.

At runtime, records can be represented as:
- **Ordered arrays** indexed by field position (determined at compile time for
  closed records)
- **Hash maps** for row-polymorphic code where the full set of fields isn't
  known until instantiation

The representation choice is an implementation concern — this spec does not
mandate a specific runtime layout.

### 10.2 Monomorphization

Row-polymorphic functions can be monomorphized at call sites, just like
type-polymorphic functions. At each call site, the row variable is instantiated
to a concrete set of fields, and the function is compiled for that specific
record layout.

This eliminates the runtime cost of row polymorphism — monomorphized code
accesses fields at fixed offsets, identical to closed records.

### 10.3 Unification Algorithm

The row unification algorithm is an extension of standard HM unification:

1. Maintain a substitution map for both type variables and row variables
2. When unifying two record types, extract their sorted field lists and tails
3. Walk the sorted field lists in parallel:
   - Same label: unify the field types
   - Different labels: record the label present in only one side
4. After walking, unify residual labels against the other side's tail:
   - If the tail is a row variable, instantiate it to include the residual labels
   - If the tail is empty (closed), fail with "missing field" error
5. Unify the two tails (both may be row variables, both empty, or one of each)

The occurs check must be extended to row variables to prevent infinite types:
`r = (name: Str, r)` is rejected.

---

## 11. Summary

### New Grammar Productions

| Production | Description |
|-----------|-------------|
| `field-list` | Field sequence with optional `\| ident` row variable tail |
| `'..' expr` in record literal | Spread operator for merging records |
| `expr '{' field-init ... '}'` | Record update expression |
| `'{' pat-field ... [ '\|' ident ] '}'` in patterns | Record destructure pattern |
| `pat-field` | Pattern field with optional rename |

### New Concepts

| Concept | Description |
|---------|-------------|
| Row variable | Lowercase identifier after `\|` in record type; represents unknown fields |
| Open record | Record type with a row variable tail: `{name: Str \| r}` |
| Closed record | Record type without a row variable: `{name: Str}` (unchanged from current) |
| Record spread | `{field: val, ..base}` — merge base record fields into new record |
| Record update | `rec{field: val}` — create new record with field changed |
| Open pattern | `{field \| rest}` — destructure record binding remaining fields |

### Interactions

| Feature | Interaction | Notes |
|---------|------------|-------|
| Effects | Orthogonal | Record rows and effect rows are independent |
| Refinements | Compositional | Refinements are part of field types, preserved through rows |
| Affine types | Propagated | Affinity determined by concrete fields at instantiation |
| Interfaces | Compatible | `where` constraints work on row-polymorphic type variables |
| Generics | Compositional | Row variables and type variables coexist via kind inference |
| Pattern matching | Extended | New record pattern syntax with open/closed variants |

### Design Properties

- **Decidable** — Rémy-style row unification is decidable
- **Principal types** — every expression has a unique most-general type
- **Backward compatible** — existing closed record syntax is unchanged
- **Terse** — ~5 new grammar productions, one new syntactic concept (`| r`)
- **Consistent** — uses the same row polymorphism framework as the effect system
