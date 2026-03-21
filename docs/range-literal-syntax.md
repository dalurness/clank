# Range Literal Syntax — SPEC

**Task:** TASK-092
**Status:** Complete
**Applies to:** `plan/features/core-syntax.md` §4 (Grammar), `plan/features/operator-precedence.md` §1 (Precedence Table)
**Dependencies:** TASK-019 (operator precedence), TASK-089 (for/in syntax), TASK-085 (streaming I/O / iterators)

---

## 1. Overview

Add range literal syntax (`..` and `..=`) to Clank for constructing integer
sequences. Range literals desugar to the existing `range` builtin — they add no
new runtime semantics.

### Design Rationale

**Why add `..` when `range` exists?**

`for x in range(1, 10)` is verbose and requires remembering inclusive
semantics. `for x in 1..10` is immediately readable by both humans
and LLM agents. The language survey (TASK-003) found that agents produce
iteration code more reliably when the range is expressed inline with familiar
syntax rather than as a function call with positional arguments.

**Why two forms (`..` and `..=`)?**

Half-open ranges (`1..n`) are the common case for zero-based indexing and
length-bounded iteration. Inclusive ranges (`1..=n`) are needed for
human-friendly "1 to n" iteration and boundary values. Both forms exist in Rust
and Kotlin; agents are familiar with the distinction.

---

## 2. Syntax

### 2.1 Half-open range (exclusive end)

```
start..end
```

Produces integers from `start` up to but not including `end`.
`1..5` yields `1, 2, 3, 4`.

### 2.2 Inclusive range

```
start..=end
```

Produces integers from `start` up to and including `end`.
`1..=5` yields `1, 2, 3, 4, 5`.

### 2.3 No unbounded ranges

Clank does not support unbounded ranges (`1..`, `..5`, `..`). Unbounded
iteration uses `iter.repeat`, `iter.range`, or explicit recursion. This keeps
range literals simple — they always have a known start and end — and avoids
accidental infinite loops in agent-generated code.

---

## 3. Grammar Productions

Add `range-expr` as a new precedence level between comparison (level 4) and
string concatenation (level 5):

```ebnf
(* ── Binary operators (precedence climbing) ── *)
pipe-expr   = or-expr { '|>' or-expr } ;               (* level 1, left *)
or-expr     = and-expr { '||' and-expr } ;              (* level 2, left *)
and-expr    = cmp-expr { '&&' cmp-expr } ;              (* level 3, left *)
cmp-expr    = range-expr [ cmp-op range-expr ] ;        (* level 4, none *)
range-expr  = concat-expr [ range-op concat-expr ] ;    (* level 4.5, none — NEW *)
concat-expr = add-expr { '++' concat-expr } ;           (* level 5, right *)
add-expr    = mul-expr { ('+' | '-') mul-expr } ;       (* level 6, left *)
mul-expr    = unary-expr { ('*' | '/' | '%') unary-expr } ; (* level 7, left *)

(* ── Range operator ── *)
range-op    = '..' | '..=' ;
```

### 3.1 Precedence rationale

**Between comparison and concat (level 4.5):** This means:

- `1..n + 1` parses as `1..(n + 1)` — arithmetic binds tighter, which is the
  intuitive reading. You want `1..len(xs)` to work without parens.
- `0..n == 0..m` parses as `(0..n) == (0..m)` — comparison binds looser, which
  allows comparing two ranges (if range equality is ever defined).
- `1..5 ++ "x"` is a type error regardless of parse, so the relative precedence
  with `++` doesn't matter in practice; placing `..` above `++` is consistent
  with the principle that numeric operations bind tighter than string operations.

### 3.2 Associativity: non-associative

`1..5..10` is a parse error. Chained ranges have no sensible meaning. This
matches the non-associative design of comparison operators (level 4).

### 3.3 Production count impact

+1 rule (`range-expr`), +1 terminal (`range-op`), bringing the grammar from ~63
to ~65 — well within the 100-rule target.

---

## 4. Desugaring Rules

Range literals desugar to builtin function calls during AST lowering, before
type checking. The desugaring target depends on context.

### 4.1 Eager desugaring (default)

```
start..end       →  range(start, sub(end, 1))
start..=end      →  range(start, end)
```

The `range` builtin has signature `(Int, Int) -> <> [Int]` with **inclusive**
semantics `[start, end]` — it produces integers from `start` up to and including
`end`. The half-open form (`..`) desugars by subtracting 1 from the end bound
so that `end` is excluded. The inclusive form (`..=`) passes both bounds through
directly.

**Design decision (TASK-094):** The builtin was implemented with inclusive
semantics before range literal syntax was added. Rather than changing the builtin
(which existing code depends on), the desugarer compensates: half-open `..`
subtracts 1 from end, inclusive `..=` is a direct pass-through.

### 4.2 Lazy desugaring (in iterator context)

When a range literal appears as the collection argument of a `for` expression,
or as the argument to an `iter.*` combinator, the compiler MAY desugar to
`iter.range` instead:

```
for x in 1..n do f(x)
```
MAY desugar to:
```
iter.range(1, n) |> iter.map(fn(x) => f(x))
```

This is an **optimization**, not a semantic requirement. The eager desugaring is
always correct. A future optimization pass (out of scope for this spec) can
detect range-in-for patterns and emit lazy iteration to avoid allocating an
intermediate list.

**Rationale for eager default:** The `range` builtin already exists and is
well-defined. Making the default lazy would require defining a `Range` type and
ensuring it satisfies the `Iterable` protocol everywhere a list does. The eager
path is simpler and correct; lazy optimization can be added without changing
semantics.

### 4.3 Half-open desugaring detail

The `sub(end, 1)` in `start..end → range(start, sub(end, 1))` is inserted as an
AST node `apply(sub, [end, 1])`, not as textual substitution. If `end` is a
literal, the compiler may constant-fold it. If `end` is `Int.min`, this
underflows — the same behavior as `range(0, sub(Int.min, 1))`. This is
acceptable; iterating with `Int.min` as an end bound is not a practical use
case.

The inclusive form `start..=end` requires no arithmetic transformation — `end` is
passed directly to `range(start, end)`.

---

## 5. Type Inference

### 5.1 Operand types

Both `start` and `end` must have type `Int`. Range literals are not polymorphic
— they work only with integers. This matches the `range` builtin, which has signature
`(Int, Int) -> <> [Int]`.

**Why not `Rat` or generic `Ord`?** Floating-point ranges have well-known
precision pitfalls (`0.0..1.0` by what step?). Generic ordered ranges require a
`Succ` typeclass or step parameter. Both add complexity for marginal benefit.
Agents overwhelmingly use integer ranges; non-integer sequences should use
explicit stdlib functions.

### 5.2 Result type

A range literal has type `[Int]` (list of integers), matching the `range` builtin.

### 5.3 Effects

Range construction is pure (`<>`). The desugared `range` call has no effects.

### 5.4 Empty ranges

`5..3` produces an empty list `[]` (desugars to `range(5, 2)` — the builtin
returns `[]` when `start > end`).
`5..=4` produces an empty list (desugars to `range(5, 4)` which is empty).

---

## 6. Interaction with Existing Constructs

### 6.1 For/in integration

Range literals are the expected way to iterate over integer sequences:

```
for i in 0..n do f(i)
for i in 1..=n do g(i)
for i in 0..len(xs) do xs[i]
```

These desugar per §4.1 and then per the for/in desugaring rules (TASK-089 §6):

```
for i in 0..n do f(i)
→ for i in range(0, sub(n, 1)) do f(i)
→ map(range(0, sub(n, 1)), fn(i) => f(i))
```

### 6.2 Pipeline integration

Range literals work in pipelines because they produce a list:

```
1..100 |> filter(fn(x) => x % 2 == 0) |> map(fn(x) => x * x)
```

### 6.3 Let binding

```
let indices = 0..len(items)
for i in indices do process(items, i)
```

### 6.4 `range` builtin coexistence

The `range` builtin remains available and is not deprecated. Range literals are
sugar that desugars to it. Users may still call `range` directly when they
prefer the function form (e.g., in pipelines where the start/end are computed).
Note that `range` uses **inclusive** semantics `[start, end]`:

```
range(offset, offset + count - 1)
```

Both forms are idiomatic. The pretty-print layer (TASK-082) does NOT convert
`range(a, b)` calls to `a..=b` — the literal form is only used when the
programmer writes it explicitly. This avoids surprising rewrites when `a` and
`b` are complex expressions.

### 6.5 Pattern matching

Range literals are NOT valid in pattern position. `match x { 1..5 => ... }` is
a parse error. Range-based matching, if desired, uses guards:

```
match x {
  n if n >= 1 && n < 5 => ...
}
```

---

## 7. Conflict Analysis

### 7.1 `..` — conflict with field access `.`

**Potential conflict:** `.` is the field access operator (level 10). Could
`1..x` be parsed as `1.` followed by `.x` (field access on `1.`)?

**Resolution:** No ambiguity. The lexer greedily matches `..` as a single token,
distinct from two `.` tokens. `1..x` lexes as `1` `..` `x`. The lexer rule:

```
'..' '='?    → RANGE_OP
'.'          → DOT
```

The greedy match ensures `..` is always a range operator. To access a field
after a numeric literal (which would be a type error anyway, since `Int` has no
fields), a space or parentheses would be required: `(1).field`.

### 7.2 `..=` — no conflicts

`..=` is a three-character token not used elsewhere. No ambiguity.

### 7.3 Rational literals

**Potential conflict:** Rational literals use `.` as decimal point: `3.14`.
Could `3..5` be parsed as `3.` `.5` (rational `3.0` followed by rational `0.5`)?

**Resolution:** No ambiguity. The lexer rule for rational literals requires a
digit on both sides of the decimal point: `digit+ '.' digit+`. The sequence
`3..5` lexes as `3` `..` `5` because after consuming `3`, the lexer sees `..`
which matches the range operator (greedy), not `.` followed by `5`.

Edge case: `3.0..5.0` — this lexes as `3.0` `..` `5.0`, which is two `Rat`
literals with a range operator. This is a **type error** (range requires `Int`
operands), caught during type checking. No lexer ambiguity.

### 7.4 Negative bounds

`-1..5` parses as `(-1)..5` because unary `-` (level 8) binds tighter than
`..` (level 4.5). This is the correct reading.

`1..-5` also parses correctly as `1..(-5)`, producing an empty range.

---

## 8. Formatter Rules

### 8.1 No spaces around `..` / `..=`

Range operators are formatted without surrounding spaces, like field access:

```
1..10       # correct
1 .. 10     # reformatted to 1..10

1..=10      # correct
1 ..= 10    # reformatted to 1..=10
```

### 8.2 Parenthesized complex bounds

When bounds are complex expressions, parentheses are recommended for clarity but
not required:

```
0..len(xs)              # fine — function call binds tighter
(offset)..(offset + n)  # parens for clarity, but not required
offset..offset + n      # equivalent, parses as offset..(offset + n)
```

The formatter does NOT add or remove parentheses around range bounds.

---

## 9. Pretty-Print Layer

The pretty-print layer (TASK-082) maps range literals to their verbose form:

| Terse | Verbose |
|-------|---------|
| `1..10` | `1..10` |
| `1..=10` | `1..=10` |

Range literal syntax is already readable. No expansion to `range(...)` calls is
performed — the literal form is the canonical representation. The pretty-print
layer passes `..` and `..=` through unchanged.

---

## 10. Examples

### 10.1 Basic iteration

```
for i in 1..=10 do print(show(i))
# Prints 1 through 10
# Desugars to: map(range(1, 10), fn(i) => print(show(i)))
```

### 10.2 Zero-based indexing

```
for i in 0..len(items) do
  process(items, i)
```

### 10.3 FizzBuzz with range

```
for n in 1..=100 do
  if n % 15 == 0 then "FizzBuzz"
  else if n % 3 == 0 then "Fizz"
  else if n % 5 == 0 then "Buzz"
  else show(n)
```

### 10.4 Sum with fold

```
for x in 1..=100 fold total = 0 do total + x
# => 5050
```

### 10.5 Pipeline with range

```
1..=20
  |> filter(fn(x) => x % 2 == 0)
  |> map(fn(x) => x * x)
# => [4, 16, 36, 64, 100, 144, 196, 256, 324, 400]
```

### 10.6 Nested ranges

```
for i in 1..=3 do
  for j in 1..=3 do
    (i, j)
# => [[(1,1), (1,2), (1,3)], [(2,1), (2,2), (2,3)], [(3,1), (3,2), (3,3)]]
```

### 10.7 Range in let binding

```
let evens = for x in 0..50 if x % 2 == 0 do x
in sum(evens)
# => 600
```

---

## 11. What Is NOT Included

- **Step/stride ranges:** `1..2..10` (every other number) is not supported. Use
  `for x in 0..n do start + x * step` or a stdlib function. Step ranges add
  grammar complexity and a third operand for a rare use case.
- **Unbounded/infinite ranges:** `1..` is not supported. Use `iter.repeat` or
  `iter.range` from the streaming stdlib.
- **Non-integer ranges:** `'a'..'z'`, `0.0..1.0` are not supported. Range
  literals are `Int`-only.
- **Range patterns:** `match x { 1..5 => ... }` is not supported. Use guards.
- **Range type:** There is no first-class `Range` type. Range literals desugar
  directly to `[Int]` via the `range` builtin. A lazy `Range` type may be introduced
  alongside the iterator protocol (TASK-085) in a future spec.
- **Automatic lazy optimization:** The compiler MAY optimize range-in-for to
  lazy iteration (§4.2), but this spec does not require it. It is a future
  optimization concern.
