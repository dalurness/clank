# Effect Aliases and Named Effect Sets

**Task:** TASK-084
**Mode:** Spec
**Date:** 2026-03-19
**Depends On:** TASK-005 (Effect System Specification)

---

## 1. Overview

Effect aliases let programmers name common combinations of effects, reducing
repetition and improving readability. An effect alias is a compile-time
abbreviation — it expands to its constituent effects during type checking and
has no runtime representation.

```
effect alias web = io, exn, async
effect alias app = io, exn, async, log, db[User]
```

Functions can then use the alias in place of the expanded row:

```
serve : Request -> <web> Response
-- equivalent to: serve : Request -> <io, exn, async> Response
```

### Design Goals

1. **Zero overhead** — aliases are purely syntactic; no new types or runtime cost
2. **Transparent** — aliased and expanded forms are interchangeable in all contexts
3. **Composable** — aliases can include other aliases and mix with bare effects
4. **Simple** — no new type-checking machinery beyond expansion

---

## 2. Syntax

### 2.1 Declaration

```
effect_alias_decl ::= 'effect' 'alias' IDENT type_params? '=' effect_list

effect_list       ::= effect_entry (',' effect_entry)*

effect_entry      ::= effect_label          -- bare effect: io, exn[E]
                    |  IDENT type_args?      -- another alias: web, app[T]
```

The keyword sequence `effect alias` introduces an alias (vs. `effect` alone for
a new effect declaration). This avoids ambiguity with user-defined effects.

### 2.2 Examples

```
-- Simple grouping
effect alias web = io, exn, async

-- Parameterized alias
effect alias app[E] = io, exn[E], async, log

-- Alias referencing another alias
effect alias full-stack[E, R] = web, db[R], log, exn[E]

-- Single-effect alias (renaming)
effect alias fail = exn
```

### 2.3 Usage in Type Annotations

Aliases appear anywhere an effect label can appear in an effect row:

```
-- In function signatures
handle-req : Request -> <web, log> Response

-- In open rows
middleware : Request -> <app[ApiErr] | e> Response

-- Mixed with bare effects
query-user : Str -> <web, db[User], state[Cache]> User
```

### 2.4 Restrictions

- Alias names occupy the same namespace as effect names. An alias cannot
  shadow an existing effect or type in the same scope.
- Aliases cannot be recursive (directly or transitively). The compiler
  rejects cycles during alias expansion.
- Aliases cannot appear in `effect` declarations (you cannot define an
  effect whose operations reference an alias — use the expanded form).
- Aliases cannot appear in `handle` expressions — you must handle specific
  effects, not aliases (see Section 4.3).

---

## 3. Type Checking Rules

### 3.1 Alias Expansion

Alias expansion is the first phase of effect row processing, before any
unification or inference. When the type checker encounters an effect label
that resolves to an alias, it replaces it with the alias's definition (after
substituting any type arguments).

**Expansion is recursive:** if alias `A` references alias `B`, both are
expanded until only primitive and user-defined effects remain.

**Expansion rule:**

```
expand(web)       = {io, exn, async}         -- given: effect alias web = io, exn, async
expand(app[E])    = {io, exn[E], async, log} -- given: effect alias app[E] = io, exn[E], async, log
expand(io)        = {io}                     -- not an alias, identity
expand(<web, db[User]>) = <io, exn, async, db[User]>
```

### 3.2 Deduplication

After expansion, duplicate effect labels are collapsed. This makes aliases
safe to combine even when they overlap:

```
effect alias web   = io, exn, async
effect alias svc   = io, exn, log

-- <web, svc> expands to <io, exn, async, io, exn, log>
-- deduplicates to <io, exn, async, log>
```

Deduplication compares labels structurally: `exn[A]` and `exn[B]` are
**not** duplicates (they have different type arguments and both appear in
the row).

### 3.3 Equivalence

After expansion, the standard row unification rules from the effect system
spec (Section 4.2) apply unchanged. An alias and its expansion are identical
— there is no way to distinguish them at the type level.

```
<web>  ≡  <io, exn, async>  ≡  <async, io, exn>
```

### 3.4 Inference

Effect inference proceeds on expanded rows. The inference engine never
produces aliases in inferred types — inferred effect rows always contain
primitive/user-defined effect labels.

When displaying types to the user (error messages, IDE tooltips), the
compiler **may** re-collapse expanded rows back into aliases for readability.
This is a presentation concern, not a type-level operation.

### 3.5 Subsumption

Subsumption works on expanded rows. A function typed `<web | e>` subsumes
to any row containing `io`, `exn`, and `async` (plus whatever `e` unifies
with).

```
-- f : A -> <web> B  can be used where  A -> <io, exn, async, log> B  is expected
-- because <web> = <io, exn, async>  ⊆  <io, exn, async, log>
```

### 3.6 Formal Expansion Rule

```
alias A[T1..Tn] = L1, L2, ..., Lm   in scope

G |- e : t ! <A[S1..Sn] | E>
─────────────────────────────────────
G |- e : t ! <L1[T:=S], L2[T:=S], ..., Lm[T:=S] | E>
```

Where `[T:=S]` denotes substitution of type parameters.

---

## 4. Interaction with Existing Effect System

### 4.1 Row Polymorphism

Aliases interact naturally with row polymorphism. An open row containing an
alias expands normally:

```
log-request : Request -> <web | e> ()
-- expands to: Request -> <io, exn, async | e> ()
```

### 4.2 Effect Operations

Operations are defined on effects, not aliases. Using `print` (an `io`
operation) inside a function annotated `<web>` works because `web` expands
to include `io`.

### 4.3 Handlers

**Handlers target specific effects, not aliases.** You cannot write
`handle expr { ... }` for an alias — you must handle each constituent
effect individually. This keeps the handler mechanism simple and explicit.

```
-- VALID: handle specific effects
serve : Request -> <io> Response
  = \req -> handle (handle-req req) {
      return x -> x,
      raise e resume _ -> error-response e
    }
-- handles exn, leaving io and async for outer handlers

-- INVALID: cannot handle an alias
serve = handle (handle-req req) {
    web ...   -- ERROR: 'web' is an alias, not an effect
  }
```

**Rationale:** An alias may contain effects with different operation
signatures and different handler shapes. Requiring explicit per-effect
handling avoids ambiguity about which operations are being handled.

### 4.4 User-Defined Effects

User-defined effects can appear in aliases:

```
effect log {
  info  : Str -> (),
  warn  : Str -> (),
  error : Str -> ()
}

effect alias observable = log, state[Metrics]
```

### 4.5 Parameterized Effects in Aliases

Aliases can contain parameterized effects. The alias may either fix the
parameter or propagate it:

```
-- Fixed parameter
effect alias safe-io = io, exn[IoErr]

-- Propagated parameter
effect alias failable[E] = io, exn[E]

-- Mixed
effect alias db-app[R] = io, exn[DbErr], db[R], log
```

---

## 5. Standard Library Aliases

The following aliases are predefined in the standard library for common
patterns. Users can define their own — these are not special-cased.

```
-- Pure computation that may fail
effect alias fallible = exn

-- IO that may fail
effect alias io-exn = io, exn

-- Stateful computation that may fail
effect alias stateful[S] = state[S], exn

-- Full async stack
effect alias net = io, exn, async
```

These are deliberately minimal. Project-specific aliases (like `app`,
`web`, `svc`) are expected to be user-defined.

---

## 6. Error Messages

### 6.1 Alias in Error Context

When reporting type errors, the compiler should mention the alias name
alongside its expansion:

```
Type error: function 'serve' has effect <web> (= <io, exn, async>)
  but the handler only discharges <exn>.
  Remaining effects: <io, async>
```

### 6.2 Common Errors

| Error | Message |
|-------|---------|
| Recursive alias | `effect alias 'A' is recursive (A -> B -> A)` |
| Shadowing effect | `effect alias 'io' conflicts with built-in effect 'io'` |
| Handling alias | `cannot handle alias 'web' — handle its effects individually: io, exn, async` |
| Unknown alias | `'web' is not a known effect or effect alias` |

---

## 7. Grammar Summary

New productions added to the Clank grammar:

```
effect_alias_decl ::= 'effect' 'alias' IDENT type_params? '=' effect_list

effect_list       ::= effect_entry (',' effect_entry)*

effect_entry      ::= effect_label
                    |  IDENT type_args?
```

New keyword: `alias` (contextual — only a keyword after `effect`).

No changes to effect row syntax, handler syntax, or existing grammar rules.

---

## 8. Design Rationale

### Why not `type` aliases?

Clank could use a general `type alias` mechanism for effect rows. However,
effect rows are not ordinary types — they are unordered sets with row
variables. A dedicated `effect alias` makes the intent clear and avoids
questions about whether the alias can appear in non-effect contexts.

A general `type alias` feature may be added later; `effect alias` would
then become syntactic sugar for `type alias web = <io, exn, async>`.

### Why can't you handle an alias?

Handlers must name each operation they intercept. An alias is a bag of
effects with potentially unrelated operations. Forcing per-effect handling
keeps handler clauses unambiguous and explicit. A future "handle all effects
in alias" shorthand could be added, but the simple approach is to list them.

### Why expand early?

Expanding aliases before inference keeps the inference engine unchanged —
it only sees primitive effect labels and row variables. This means aliases
are guaranteed to introduce zero new type-checking complexity.

### Why allow aliases in aliases?

Composability. Teams can layer aliases: a `web` alias for framework effects,
an `app` alias that includes `web` plus domain effects. Without nesting,
users would have to manually duplicate effect lists.

### Why comma-separated instead of `+`?

The existing effect row syntax uses commas: `<io, exn, async>`. Using
commas in alias definitions (`io, exn, async`) is consistent. A `+`
operator was considered but adds a new token for no semantic benefit, and
could be confused with type-level union operations.

---

## 9. Future Extensions

These are explicitly **out of scope** for this spec but noted for
completeness:

- **Effect subtraction in aliases:** `effect alias quiet-web = web - log`
  (removing an effect from an alias). Useful but adds complexity.
- **Handle-all-in-alias shorthand:** `handle expr { web { ... } }` to
  handle all effects in an alias at once with generated clauses.
- **Conditional aliases:** aliases that include/exclude effects based on
  compile-time flags (e.g., debug vs. release builds).
- **Alias export/visibility:** controlling whether an alias is public or
  module-private. For now, aliases follow the same visibility rules as
  other declarations.
