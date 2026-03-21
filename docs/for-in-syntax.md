# for/in Grammar Integration — SPEC

**Task:** TASK-089
**Status:** Complete
**Applies to:** `plan/features/core-syntax.md` §2 (Keywords), §4 (Grammar), §6 (Control Flow)
**Dependencies:** TASK-019 (operator precedence)

---

## 1. Overview

Add a `for` expression to Clank that iterates over collections, producing a new
collection (map), filtering elements, or accumulating a result (fold). The `for`
expression desugars entirely to existing higher-order functions (`map`, `filter`,
`fold`) — it adds no new runtime semantics.

### Design Rationale

**Why add `for` when `map`/`filter` exist?**

LLM agents generate `for`-style iteration more reliably than nested higher-order
function calls. The language survey (TASK-003) found that agents produce fewer
errors with explicit iteration variable binding than with point-free or lambda-
heavy styles, especially when the body exceeds a single expression. `for`
expressions provide this familiar binding structure while desugaring to the
existing functional core — no new runtime machinery needed.

**Why not `loop`?**

`loop` already appears in the async-runtime spec as a function
(`loop \-> { ... }` with `continue`/`break`). Adding `loop` as a keyword would
break that usage. `for`/`in` is strictly bounded iteration over a collection,
which is the common case; unbounded looping remains a library function.

---

## 2. Keyword Changes

### Added keyword

```
for
```

### Reused keyword

```
in
```

`in` is already a keyword, used in `let x = expr in expr`. No new keyword needed.

### Updated keyword list (§2)

```
let  in  for  fn  if  then  else  match  do  type  effect
affine  handle  resume  mod  use  pub  clone  true  false
```

---

## 3. Syntax

### 3.1 Basic for expression (map)

```
for x in xs do f(x)
```

Iterates over `xs`, binding each element to `x`, producing a new list from the
body expression. Equivalent to `map(xs, fn(x) => f(x))`.

### 3.2 For with filter (guard clause)

```
for x in xs if p(x) do f(x)
```

Equivalent to `map(filter(xs, fn(x) => p(x)), fn(x) => f(x))`.

### 3.3 For with accumulator (fold)

```
for x in xs fold acc = init do g(acc, x)
```

Equivalent to `fold(xs, init, fn(acc, x) => g(acc, x))`.

### 3.4 For with destructuring

```
for (k, v) in pairs do f(k, v)
```

The iteration variable position accepts any pattern (same as `match` arms).

### 3.5 For with block body

```
for x in xs do {
  let y = transform(x)
  finalize(y)
}
```

The body after `do` can be a single expression or a block (`{ ... }`).

---

## 4. Grammar Productions

Add to §4 (Grammar), within the `(* ── Expressions ── *)` section:

```ebnf
(* ── Expressions ── *)
expr        = let-expr
            | if-expr
            | match-expr
            | for-expr            (* NEW *)
            | do-block
            | handle-expr
            | pipe-expr ;

(* ── For expression ── *)
for-expr    = 'for' for-bind 'in' expr [ for-guard ] for-body ;
for-bind    = pattern ;
for-guard   = 'if' expr ;
for-body    = 'do' expr                              (* map form *)
            | 'fold' ident '=' expr 'do' expr ;      (* fold form *)
```

**Production count impact:** +3 rules (for-expr, for-guard, for-body), bringing
the total from ~60 to ~63 — well within the 100-rule target.

---

## 5. Conflict Analysis

### 5.1 `in` — conflict with let-expression

**Conflict:** `in` appears in both `let x = expr in expr` and `for x in expr`.

**Resolution:** No ambiguity. The parser distinguishes by leading keyword:
- `let` → parse `let-expr` → expects `in` after the binding's value expression
- `for` → parse `for-expr` → expects `in` after the binding pattern

These are separate productions entered from `expr`. The `in` token is never
ambiguous because its role is determined by which production is active. The
parser has already committed to either `let-expr` or `for-expr` before
encountering `in`.

**Edge case — `for` inside `let`:**
```
let result = for x in xs do f(x) in use(result)
```
The first `in` is consumed by `for-expr` (after the pattern `x`). The second
`in` is consumed by `let-expr` (after the `for-expr` completes as the value
expression). No ambiguity: `for-expr` is a complete subexpression of `let-expr`.

### 5.2 `if` — conflict with if-expression

**Conflict:** `if` appears in both `if-expr` and `for-guard`.

**Resolution:** No ambiguity. The `for-guard` `if` is only parsed inside a
`for-expr`, after the collection expression. The parser is inside the `for-expr`
production when it encounters the optional `if` guard, so it cannot be confused
with a top-level `if-expr`.

**Edge case — `if` in collection position:**
```
for x in (if cond then xs else ys) do f(x)
```
The collection expression is parsed as a full `expr`, which can be an `if-expr`.
The parentheses are required here because without them:
```
for x in if cond then xs else ys do f(x)
```
The `if` would be captured as a `for-guard`, causing a parse error when `cond`
is not followed by `do`. This is a **deliberate** restriction: requiring parens
for conditional collections is clearer and avoids grammar complexity. In
practice, conditional collection selection should use a `let` binding anyway:
```
let source = if cond then xs else ys
for x in source do f(x)
```

### 5.3 `match` — no conflict

`match` is a keyword for pattern matching expressions. `for-expr` and
`match-expr` are parallel alternatives in the `expr` production. The leading
keyword (`for` vs `match`) fully disambiguates. No interaction.

### 5.4 `do` — conflict with do-block

**Conflict:** `do` appears in both `do-block` and `for-body`.

**Resolution:** No ambiguity. In `for-expr`, `do` is expected after the
collection (and optional guard/fold). The parser is inside the `for-expr`
production, so `do` is consumed as the body delimiter, not as the start of a
standalone `do-block`.

**Edge case — do-block as for body:**
```
for x in xs do do {
  y <- effectful(x)
  pure(y)
}
```
The first `do` is the `for-body` delimiter. The second `do` starts a `do-block`
as the body expression. This is syntactically valid but stylistically
discouraged — prefer:
```
for x in xs do {
  y <- effectful(x)
  pure(y)
}
```
(A bare block `{ ... }` in for-body position would need the effect binding `<-`
to be supported directly. If not, the `do` block form is required. See §8
Observations.)

### 5.5 `fold` — new contextual keyword

`fold` is NOT added to the keyword list. It is a **contextual keyword** that is
only recognized inside a `for-body` production. Outside of `for` expressions,
`fold` remains a valid identifier (the existing `fold` function in the stdlib).

**Rationale:** Adding `fold` as a keyword would shadow the existing `fold`
function in `std.list`, breaking code like `fold(xs, 0, add)`. Instead, `fold`
is only special after `for x in expr`, where no identifier would be valid anyway
(the parser expects either `do` or `fold` at that position).

---

## 6. Desugaring Rules

All `for` expressions desugar to calls to existing stdlib functions. The
desugaring is performed during AST lowering, before type checking.

### 6.1 Map form

```
for P in E do B
```
desugars to:
```
map(E, fn(P) => B)
```

### 6.2 Filter + map form

```
for P in E if G do B
```
desugars to:
```
map(filter(E, fn(P) => G), fn(P) => B)
```

Note: The pattern `P` is bound in both the guard `G` and the body `B`.

### 6.3 Fold form

```
for P in E fold A = I do B
```
desugars to:
```
fold(E, I, fn(A, P) => B)
```

### 6.4 Filter + fold form

```
for P in E if G fold A = I do B
```
desugars to:
```
fold(filter(E, fn(P) => G), I, fn(A, P) => B)
```

### 6.5 Destructuring

Pattern `P` in desugaring follows the same rules as lambda parameter patterns
and match-arm patterns. Tuple destructuring works directly:

```
for (k, v) in pairs do k ++ ": " ++ v
```
desugars to:
```
map(pairs, fn((k, v)) => k ++ ": " ++ v)
```

### 6.6 Type and effect propagation

Since `for` desugars to `map`/`filter`/`fold`, type inference and effect
propagation follow existing rules:

- **Map form:** `for x in E do B` has type `[B_type]` where `B_type` is the
  type of the body expression. Effects from `E` and `B` propagate.
- **Fold form:** `for x in E fold acc = I do B` has type matching the
  accumulator type. `B` must return the same type as `I`.
- **Filter guard:** The guard expression must have type `Bool`.

---

## 7. Formatter Rules

### 7.1 Single-line form

When the body is a single short expression (< 60 characters total), format on
one line:

```
for x in xs do f(x)
for x in xs if p(x) do g(x)
for x in xs fold acc = 0 do acc + x
```

### 7.2 Multi-line form

When the body is long or is a block, break after `do`:

```
for x in xs do
  long-function-name(x, some-other-arg)

for x in xs if predicate(x) do
  transform(x)
```

### 7.3 Block body

Block bodies use standard indentation:

```
for x in xs do {
  let y = transform(x)
  finalize(y)
}
```

### 7.4 Guard on separate line

When the guard is long, break before `if`:

```
for x in xs
  if complex-predicate(x, threshold)
  do transform(x)
```

### 7.5 Fold form multi-line

```
for x in xs
  fold acc = initial-value
  do combine(acc, x)
```

### 7.6 Nested for expressions

Nested `for` expressions indent normally:

```
for x in xs do
  for y in ys do
    (x, y)
```

This desugars to `map(xs, fn(x) => map(ys, fn(y) => (x, y)))` and produces
`[[T]]` (a list of lists). This is intentional — Clank does not have list
comprehension flattening. Use `flat-map` or `cat` explicitly if a flat list is
needed.

### 7.7 Pretty-print layer

The pretty-print layer (TASK-082) passes `for`/`in`/`do`/`fold` through
unchanged — these are already human-readable keywords with no terse equivalent.

---

## 8. Examples

### 8.1 Double each element

```
for x in [1, 2, 3] do x * 2
# => [2, 4, 6]
# desugars to: map([1, 2, 3], fn(x) => x * 2)
```

### 8.2 Filter and transform

```
for x in numbers if x > 0 do x * x
# desugars to: map(filter(numbers, fn(x) => x > 0), fn(x) => x * x)
```

### 8.3 Sum with fold

```
for x in xs fold total = 0 do total + x
# desugars to: fold(xs, 0, fn(total, x) => total + x)
```

### 8.4 Destructure pairs

```
for (name, score) in students if score >= 90 do
  name ++ " — honors"
```

### 8.5 Effectful body

```
for path in files do {
  contents <- read(path)
  parse(contents)
}
```

Note: The `<-` binding requires the body to be inside a `do` block context for
effect sequencing. This works because `do { ... }` is a valid expression in body
position.

### 8.6 Pipeline integration

```
data |> for x in _ do transform(x)  # NOT valid — for is not a function
```

`for` expressions are not pipeable. Use `map` directly in pipelines:

```
data |> map(fn(x) => transform(x))
```

This is a deliberate design choice: `for` is for readability in multi-line
contexts; pipelines are for terse single-line chains.

### 8.7 With let binding

```
let squared-evens = for x in numbers if x % 2 == 0 do x * x
in process(squared-evens)
```

---

## 9. What Is NOT Included

- **`for` with ranges:** `for x in 1..10` requires a range literal or range
  type. Use `for x in col.range(1, 10)` with the existing stdlib function.
  Range literals (`..`) may be added separately.
- **`for` with index:** `for (i, x) in enumerate(xs)` works via the existing
  `enumerate` stdlib function + destructuring. No special syntax needed.
- **`for` as statement (side-effects only):** All `for` expressions produce a
  value. For side-effect iteration, use `each` from the stdlib or wrap in a
  `do` block. `for` without using the result will trigger an unused-value
  warning.
- **`break`/`continue`:** These belong to unbounded `loop`, not bounded `for`.
  `for` always processes every element (after filtering).
- **Parallel `for`:** No `par-for` or parallel iteration. Use `par-map` from
  the async stdlib if needed.
