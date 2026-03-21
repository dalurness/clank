# Clank Interface System Specification

## Overview

Clank's interface system provides bounded polymorphism through typeclasses. An
**interface** declares a set of operations any implementing type must provide.
Types opt in via `impl` blocks. Common interfaces can be auto-derived.

Design goals:
- **Minimal**: 8 typing rules total
- **Terse**: declarations fit in few tokens
- **Extensible**: new interfaces can be added without modifying existing types
- **Agent-friendly**: constraints are explicit in signatures, no implicit resolution chains

## 1. Syntax

### Interface declaration

```ebnf
interface-decl = 'interface' word [ type-params ] [ superinterfaces ] '{'
                   { method-sig }
                 '}' ;

superinterfaces = ':' word { '+' word } ;

method-sig     = word ':' type-sig ;
```

Example:

```
interface Show {
  show : (Self) -> <> Str
}

interface Eq {
  eq : (Self, Self) -> <> Bool
}

interface Ord : Eq {
  cmp : (Self, Self) -> <> Ordering
}
```

`Self` refers to the implementing type. It is only valid inside interface and
impl blocks.

### Parameterized interfaces

Interfaces may take type parameters beyond `Self`:

```
interface Into<T> {
  into : (Self) -> <> T
}
```

### Implementation

```ebnf
impl-block = 'impl' word [ '<' type-expr { ',' type-expr } '>' ]
             'for' type-expr
             [ 'where' constraints ]
             '{' { definition } '}' ;
```

Example:

```
impl Show for Int {
  show = int-to-str
}

impl Show for [a] where Show a {
  show = fn(xs) => "[" ++ join(map(xs, show), ", ") ++ "]"
}
```

Method bodies follow the same syntax as regular definitions.

### Deriving

```ebnf
derive-clause = 'deriving' '(' word { ',' word } ')' ;
type-decl     = 'type' word [ type-params ] '=' type-body [ derive-clause ] ;
```

Example:

```
type Color = Red | Green | Blue
  deriving (Eq, Show, Clone)

type Point = { x: Rat, y: Rat }
  deriving (Eq, Clone)
```

Deriving is only available for interfaces with well-defined structural
generation rules (see §5). The compiler rejects `deriving` for interfaces
that don't support it.

### Constraints in signatures

```ebnf
constraints   = constraint { ',' constraint } ;
constraint    = word [ '<' type-expr { ',' type-expr } '>' ] word ;
```

Constraints appear in `where` clauses on definitions and impl blocks:

```
print-all : ([a]) -> <io> () where Show a
  = fn(xs) => each(xs, fn(x) => print(show(x)))

sort : ([a]) -> <> [a] where Ord a
  = ...
```

Multiple constraints:

```
dedup-show : ([a]) -> <> Str where Eq a, Show a
  = fn(xs) => join(map(unique(xs), show), ", ")
```

## 2. Typing Rules

Eight rules govern the interface system. Notation: `Γ` is the typing context,
`I` is the interface table, `⊢` is the judgment relation.

### Rule 1: Interface Well-formedness (I-WF)

```
  For each method m_i in interface C:
    m_i : σ_i   where σ_i mentions Self
  ──────────────────────────────────────
  interface C { m_1 : σ_1, ..., m_n : σ_n }  well-formed
```

Each method signature must reference `Self` at least once.

### Rule 2: Superinterface Entailment (I-SUPER)

```
  interface C : D₁ + D₂ + ... + Dₖ
  Γ ⊢ T : C
  ──────────────────────
  Γ ⊢ T : Dᵢ   (for each i ∈ 1..k)
```

An impl of C implies all superinterface constraints are satisfied.

### Rule 3: Implementation Validity (I-IMPL)

```
  interface C { m_1 : σ_1, ..., m_n : σ_n }
  For each m_i: Γ ⊢ e_i : σ_i[Self := T]
  All 'where' constraints on the impl are satisfiable
  ──────────────────────────────────────────────────────
  impl C for T { m_1 = e_1, ..., m_n = e_n }  valid
  I := I ∪ { T : C }
```

Every method must be provided. The body must typecheck at the
Self-substituted signature.

### Rule 4: Method Dispatch (I-DISPATCH)

```
  I ⊢ T : C
  m : σ ∈ C
  ──────────────────────
  Γ ⊢ (v : T) m  :  σ[Self := T]
```

When method `m` of interface C is called on a value of type T, resolve to
T's implementation. Dispatch is monomorphized at compile time — no vtables
in the default path.

### Rule 5: Constrained Polymorphism (I-CONSTRAIN)

```
  Γ, (C a) ⊢ e : τ
  ──────────────────────────────
  Γ ⊢ (e where C a) : ∀a. C a => τ
```

A `where` clause introduces interface constraints into scope. The constraint
must be discharged at every call site.

### Rule 6: Constraint Satisfaction (I-SAT)

```
  f : τ where C a
  I ⊢ T : C
  ──────────────────────
  Γ ⊢ f @T : τ[a := T]
```

At a call site, the compiler checks that the concrete type has an impl for
the required interface. Type argument inference determines T; explicit
annotation (`@T`) is available but rarely needed.

### Rule 7: Coherence (I-COHERENT)

```
  For any type T and interface C:
    at most one impl C for T exists in the program
```

No overlapping instances. Orphan impls (where neither C nor T is defined in
the current module) are a warning by default, error with `--strict-orphans`.

### Rule 8: Deriving (I-DERIVE)

```
  type T = K₁(F₁₁, ..., F₁ₘ) | ... | Kₙ(Fₙ₁, ..., Fₙₚ)
  C ∈ derivable-set
  For each field type Fᵢⱼ: I ⊢ Fᵢⱼ : C
  ──────────────────────────────────────────────
  I := I ∪ { T : C }   (with generated impl)
```

A type can derive C if C supports derivation AND all constituent field types
already implement C.

## 3. Built-in Interfaces

### Clone

```
interface Clone {
  clone : (Self) -> <> Self
}
```

Produces a deep copy. All primitive types (`Int`, `Rat`, `Bool`, `Str`,
`Byte`, `()`) have built-in Clone impls.

**Derivable**: Yes. Generated impl clones each field/variant payload.

### Show

```
interface Show {
  show : (Self) -> <> Str
}
```

Converts to a human/agent-readable string representation. All primitive
types have built-in Show impls.

**Derivable**: Yes. Generated impl produces constructor-style output:
- Variants: `"Circle(3.14)"`, `"None"`
- Records: `"{ x: 1.0, y: 2.0 }"`

### Eq

```
interface Eq {
  eq : (Self, Self) -> <> Bool
}
```

Structural equality. Built-in for all primitives.

**Derivable**: Yes. Generated impl compares each field/variant tag+payload.

Note: The built-in `eq` word dispatches to this interface for non-primitive
types. For primitives it remains a direct operation.

### Ord

```
interface Ord : Eq {
  cmp : (Self, Self) -> <> Ordering
}
```

Where `Ordering = Lt | Eq_ | Gt` is a built-in sum type.

**Derivable**: Yes (for types where all fields implement Ord). Uses
lexicographic comparison on fields, tag order for variants.

### Default

```
interface Default {
  default : () -> <> Self
}
```

Provides a zero/empty value. Built-in for primitives (`0`, `0.0`, `false`,
`""`, `0x00`, `()`).

**Derivable**: Yes, if all fields implement Default.

### Into / From

```
interface Into<T> {
  into : (Self) -> <> T
}

interface From<T> {
  from : (T) -> <> Self
}
```

Conversion interfaces. Not derivable — always explicit.

Blanket rule: `impl From<T> for U` automatically provides `impl Into<U> for T`.

## 4. Interaction with Existing Features

### Effects

Interface methods may declare effects:

```
interface Read {
  read-val : (Self) -> <io, exn> Str
}
```

The effect annotations propagate normally through the effect system.

### Refinement types

Constraints and refinements compose:

```
sort : ([a]{len > 0}) -> <> [a]{len > 0} where Ord a
```

### Pattern matching

No special interaction — match works on concrete types. If you match on a
constrained generic, the constraint propagates into each branch.

### Quotations

Quotation types can carry constraints:

```
map-show : ([a], [a -> Str]) -> <> [Str] where Show a
```

### Module system

- `interface` and `impl` blocks are top-level declarations
- `pub interface` exports the interface and all its methods
- `pub impl` is the default — impls are always public (coherence requires it)
- Import interfaces with `use`: `use std.show (Show)`

## 5. Derivation Rules

The following interfaces support `deriving`:

| Interface | Strategy |
|-----------|----------|
| `Clone`   | Clone each field recursively |
| `Show`    | Format as constructor syntax |
| `Eq`      | Compare tag + each field with `eq` |
| `Ord`     | Lexicographic on fields, declaration order for variant tags |
| `Default` | Apply `default` to each field |

Derivation requires that **all** field types already implement the target
interface. The compiler emits a clear error if a field type lacks the impl:

```
error[E201]: cannot derive Show for Point
  --> src/geo.clk:5:3
  | field `data` has type CustomBlob which does not implement Show
  | hint: add `impl Show for CustomBlob { ... }` or remove Show from deriving
```

## 6. Grammar Additions

New productions added to the core grammar:

```ebnf
top-level   += interface-decl | impl-block ;

interface-decl = 'interface' word [ type-params ] [ ':' word { '+' word } ]
                 '{' { method-sig } '}' ;

impl-block  = 'impl' word [ '<' type-expr { ',' type-expr } '>' ]
              'for' type-expr
              [ 'where' constraints ]
              '{' { definition } '}' ;

derive-clause = 'deriving' '(' word { ',' word } ')' ;
type-decl    += ... [ derive-clause ] ;

constraints  = constraint { ',' constraint } ;
constraint   = word [ '<' type-expr { ',' type-expr } '>' ] word ;

definition  += [ 'where' constraints ] ;

method-sig   = word ':' type-sig ;
```

New keywords: `interface`, `impl`, `Self`, `deriving`, `where`.

**Production count impact**: +8 rules (well within budget).

## 7. Examples

### Defining and implementing a custom interface

```
interface Hash {
  hash : (Self) -> <> Int
}

impl Hash for Str {
  hash = fn(s) => fold(bytes(s), 0, fn(acc, b) => acc * 31 + b)
}

impl Hash for Int {
  hash = fn(n) => n    # identity hash for ints
}
```

### Generic function with constraints

```
deduplicate : ([a]) -> <> [a] where Eq a
  = fn(xs) => fold(xs, [], fn(acc, x) =>
      if not(elem(x, acc)) then cons(x, acc) else acc
    ) |> rev

elem : (a, [a]) -> <> Bool where Eq a
  = fn(x, xs) => fold(xs, false, fn(acc, y) => acc || eq(x, y))
```

### Deriving on a complex type

```
type Expr
  = Lit(Int)
  | Add(Expr, Expr)
  | Mul(Expr, Expr)
  deriving (Eq, Clone, Show)

eval : (Expr) -> <> Int
  = fn(e) => match e {
      Lit(n)    => n
      Add(l, r) => eval(l) + eval(r)
      Mul(l, r) => eval(l) * eval(r)
    }
```

### Constrained impl (Show for lists)

```
impl Show for [a] where Show a {
  show = fn(xs) => "[" ++ join(map(xs, show), ", ") ++ "]"
}
```

### Conversion interfaces

```
impl From<Int> for Rat {
  from = int-to-rat
}

# Now this works (via blanket Into):
let r : Rat = into(42)   # 42.0 (as Rat)
```

### Interface with superinterface

```
type Timestamp = { epoch: Int }
  deriving (Eq)

impl Ord for Timestamp {
  cmp = fn(a, b) =>
    if a.epoch < b.epoch then Lt
    else if a.epoch == b.epoch then Eq_
    else Gt
}

latest : ([Timestamp]{len > 0}) -> <> Timestamp where Ord Timestamp
  = fn(ts) => fold(tail(ts), head(ts), fn(best, t) =>
      match cmp(t, best) { Gt => t; _ => best }
    )
```

## 8. Design Rationale

### Why typeclasses over structural interfaces (Go-style)?

Structural interfaces (implicit satisfaction) create ambiguity: does a type
with a `show` method intend to implement Show, or is it coincidence? Explicit
`impl` blocks are unambiguous — agents can verify interface compliance by
checking for the impl, not by analyzing method signatures. This aligns with
Clank's "no magic" principle.

### Why not trait objects / dynamic dispatch by default?

Clank monomorphizes by default. This means:
- No runtime overhead for interface calls
- Full type information at compile time (enables refinement checking)
- Simpler mental model — `show` on an `Int` compiles to `int-to-str`, period

Dynamic dispatch (`dyn Show`) is a future extension if needed. The 8 rules
above don't preclude it, but we don't specify it yet.

### Why `Self` instead of an explicit type parameter?

`Self` is shorter (1 token vs 3+ for `interface Show<T> { show: T -> Str }`)
and makes the common case — methods operating on the implementing type —
terser. Parameterized interfaces (`Into<T>`) handle the multi-type case.

### Why `where` clauses instead of inline constraints?

`where` keeps constraints separate from the type signature, which is
already information-dense. Compare:

```
# where clause (clear)
sort : ([a]) -> <> [a] where Ord a

# inline (cluttered)
sort : ([a : Ord]) -> <> [a]
```

The `where` form is also consistent with Haskell and Rust, which agents are
likely trained on.

### Why limit derivable interfaces?

Unrestricted deriving (Haskell's `GeneralizedNewtypeDeriving`,
`DerivingVia`) adds complexity that doesn't pay off at this stage. The 5
derivable interfaces cover the most common boilerplate. Additional derivation
strategies can be added later without changing the core rules.

### Coherence without orphan ban

A hard orphan ban would prevent useful patterns (e.g., a serialization
library providing `Show` for third-party types). Instead, orphans are
warnings — agents can suppress with intent. `--strict-orphans` is available
for projects that want the guarantee.

## 9. Future Extensions (Out of Scope)

These are noted for compatibility — the current design does not preclude them:

- **Associated types**: `interface Collection { type Item; ... }`
- **Dynamic dispatch**: `dyn Show` for heterogeneous collections
- **Default methods**: method bodies in the interface declaration
- **Deriving strategies**: `deriving via Wrapper (Show)`
- **Higher-kinded interfaces**: `interface Functor<F<_>> { map : ... }`
  (requires HKT support — explicitly out of scope per BRIEF)
