# Effect Subtraction Operator Specification

**Task:** TASK-096
**Mode:** Spec
**Date:** 2026-03-19
**Depends On:** TASK-005 (Effect System), TASK-084 (Effect Aliases)

---

## 1. Overview

The effect subtraction operator `\` removes one or more effect labels from an
effect row. This enables expressing *partial effect discharge* — the idea that
a function handles some effects from a row while passing the rest through.

```
-- "web minus exn" — a handler discharged exn, io and async remain
<web \ exn>  ≡  <io, async>

-- subtract multiple effects
<io, exn, async, log \ io, async>  ≡  <exn, log>
```

Effect subtraction is a compile-time operation on effect rows. It has no
runtime representation. After expansion and subtraction, the result is an
ordinary effect row subject to standard unification and inference rules.

### Design Goals

1. **Compositional** — subtraction works with aliases, bare effects, and open rows
2. **Transparent** — the result is a plain effect row; no new type-level concept
3. **Early resolution** — subtraction is resolved during alias expansion, before inference
4. **Safe** — subtracting an effect not present in the row is a compile-time error

---

## 2. Syntax

### 2.1 Subtraction in Effect Rows

```
effect_row    ::= '<' effect_expr '>'
               |  '<' effect_expr '|' evar '>'
               |  '<' '>'

effect_expr   ::= effect_list
               |  effect_list '\' effect_list        -- subtraction

effect_list   ::= effect_entry (',' effect_entry)*

effect_entry  ::= effect_label
               |  IDENT type_args?
```

The `\` operator binds tighter than `|` (the row extension operator), so
`<web \ exn | e>` parses as `<(web \ exn) | e>`.

### 2.2 Subtraction in Alias Definitions

```
effect_alias_decl ::= 'effect' 'alias' IDENT type_params? '=' effect_expr
```

Aliases can use subtraction in their definitions:

```
effect alias quiet-web = web \ log
-- expands to: io, exn, async   (given web = io, exn, async, log...
-- wait, web = io, exn, async — log isn't in web, so this would error)

-- Correct example:
effect alias app = io, exn, async, log, db[User]
effect alias app-pure = app \ io, async
-- expands to: exn, log, db[User]
```

### 2.3 Examples in Function Signatures

```
-- A handler that discharges exn from web, returns remaining effects
handle-errors : (() -> <web | e> a) -> <web \ exn | e> a?

-- After expansion (web = io, exn, async):
-- handle-errors : (() -> <io, exn, async | e> a) -> <io, async | e> a?

-- Subtract a user-defined effect
silence-log : (() -> <log, e> a) -> <e> a

-- Subtract from an alias
effect alias app = io, exn, async, log
run-pure : (() -> <app \ io, async> a) -> <> a
-- equivalent to: (() -> <exn, log> a) -> <> a
```

---

## 3. Semantics

### 3.1 Expansion and Resolution

Subtraction is resolved in the alias expansion phase — *after* alias expansion
and *before* row unification. The resolution steps are:

1. **Expand aliases** on both sides of `\` to primitive/user-defined effect labels
2. **Compute set difference**: result = LHS labels − RHS labels
3. **Check coverage**: every label on the RHS must appear in the LHS (otherwise error)
4. **Flatten**: the result replaces the subtraction expression as a plain effect list

After step 4, the effect row contains only ordinary labels and row variables.
The rest of type checking proceeds unchanged.

### 3.2 Formal Rule

```
expand(A) = {L1, L2, ..., Lm}
expand(B) = {R1, R2, ..., Rn}
{R1, ..., Rn} ⊆ {L1, ..., Lm}
───────────────────────────────────────
expand(A \ B) = {L1, ..., Lm} \ {R1, ..., Rn}
```

Where set difference on effect labels uses structural equality (including
type parameters): `exn[A]` and `exn[B]` are distinct labels.

### 3.3 Interaction with Row Variables

Subtraction operates on the *concrete* portion of a row. The row variable is
preserved:

```
<web \ exn | e>
→ expand web: <io, exn, async \ exn | e>
→ subtract: <io, async | e>
```

Subtracting from a bare row variable is **not allowed** — the variable's
contents are unknown at alias-expansion time:

```
-- ERROR: cannot subtract from row variable
<e \ io>   -- ill-formed
```

This restriction keeps subtraction a purely static operation resolvable
before inference begins.

### 3.4 Parameterized Effects

Subtraction matches parameterized effects by structural equality on the
full label including type arguments:

```
<exn[ParseErr], exn[IoErr], io \ exn[ParseErr]>
→ <exn[IoErr], io>

-- Bare exn does NOT match exn[ParseErr]:
<exn[ParseErr], io \ exn>
→ ERROR: exn is not in {exn[ParseErr], io}
   (exn without a parameter is shorthand for exn[Err],
    which is structurally distinct from exn[ParseErr])
```

---

## 4. Interaction with Effect Aliases

### 4.1 Aliases on the Left

The most common use: subtract specific effects from an alias.

```
effect alias web = io, exn, async

<web \ io>        → <exn, async>
<web \ io, exn>   → <async>
<web \ io, exn, async>  → <>     -- fully discharged
```

### 4.2 Aliases on the Right

Aliases can also appear on the right side of `\`:

```
effect alias net = io, async
effect alias app = io, exn, async, log

<app \ net>       → <exn, log>
```

### 4.3 Nested Aliases

When aliases reference other aliases, all are expanded before subtraction:

```
effect alias web = io, exn, async
effect alias full = web, log, db[User]

<full \ web>      → expand full: <io, exn, async, log, db[User]>
                  → expand web:  {io, exn, async}
                  → subtract:    <log, db[User]>
```

### 4.4 Aliases Defined with Subtraction

Aliases can use subtraction in their definition. Subtraction is resolved
at alias definition time:

```
effect alias app = io, exn, async, log
effect alias app-pure = app \ io, async
-- app-pure expands to: exn, log
-- subsequent uses of app-pure see only {exn, log}
```

This means subtraction does not "follow" redefinitions — if `app` is later
shadowed in a different scope, `app-pure` retains its resolved form from its
definition site. (This matches standard lexical scoping.)

---

## 5. Typing Rules

### 5.1 Subtraction in Annotations

Effect subtraction in type annotations is desugared before type checking:

```
f : A -> <E1 \ E2 | e> B
── desugar (given expand(E1\E2) = E3) ──
f : A -> <E3 | e> B
```

The type checker never sees `\` — it is fully resolved during parsing/expansion.

### 5.2 Handler Return Types

The primary use case for subtraction is expressing what a handler returns:

```
handle-exn : (() -> <exn[E] | e> a) -> <e> a?
```

Without subtraction, the caller must mentally compute what remains after
handling. With subtraction, a handler's type can be expressed relative to
its input:

```
-- "I handle exn from whatever you give me"
handle-exn : (() -> <e> a) -> <e \ exn | r> a?
```

However, this form subtracts from a row variable `e`, which is disallowed
(Section 3.3). The established pattern — using `<exn[E] | e>` on input and
`<e>` on output — already expresses this naturally. Subtraction is most
useful in annotations where the *full* input row is known or aliased:

```
-- Subtract from a known alias
run-web : (() -> <web> a) -> <web \ exn> a?
-- expands to: (() -> <io, exn, async> a) -> <io, async> a?
```

### 5.3 Equivalence

A subtracted row and its expansion are identical after resolution:

```
<web \ exn>  ≡  <io, async>
```

No special equivalence rules are needed — standard row equivalence applies
to the resolved form.

---

## 6. Error Messages

### 6.1 Subtracting Absent Effect

```
effect alias web = io, exn, async

<web \ log>
──
Error: cannot subtract effect 'log' from row <web> (= <io, exn, async>)
  'log' is not present in the row.
```

### 6.2 Subtracting from Empty Row

```
<> \ io
──
Error: cannot subtract effect 'io' from empty row <>
```

### 6.3 Subtracting from Row Variable

```
<e \ io>
──
Error: cannot subtract from row variable 'e'
  Effect subtraction requires a concrete effect row.
  Hint: use <io | e> on the input and <e> on the output to express
  effect discharge with row polymorphism.
```

### 6.4 Parameterized Mismatch

```
<exn[ParseErr], io \ exn>
──
Error: cannot subtract 'exn' (= exn[Err]) from row <exn[ParseErr], io>
  'exn' (shorthand for exn[Err]) is not present.
  Did you mean: exn[ParseErr]?
```

---

## 7. Complete Examples

### 7.1 Layered Handler Stack

```
effect alias app = io, exn[ApiErr], async, log, db[User]

-- Each handler peels off one effect
serve : Request -> <app> Response
  = \req -> ...

-- Handle logging → remaining: io, exn[ApiErr], async, db[User]
with-file-log : (() -> <app | e> a) -> <app \ log | e> a
  = \f -> handle (f ()) {
      return x -> x,
      info msg resume k  -> write-f log-path msg; k (),
      warn msg resume k  -> write-f log-path msg; k (),
      error msg resume k -> write-f log-path msg; k ()
    }

-- Handle db → remaining: io, exn[ApiErr], async
with-pg : (() -> <app \ log | e> a) -> <app \ log, db[User] | e> a
  = \f -> handle (f ()) {
      return x -> x,
      query q resume k  -> k (pg-query conn q),
      insert r resume k -> pg-insert conn r; k (),
      delete q resume k -> k (pg-delete conn q)
    }

-- Compose at main
main : () -> <io> ()
  = {
      let result = catch \->
        with-file-log \->
          with-pg \->
            serve (read-request ())
      match result {
        ok resp  -> send resp,
        err e    -> send (error-response e)
      }
    }
```

### 7.2 Effect Algebra

```
effect alias full = io, exn, async, log, state[S]

-- progressive discharge
<full>                          -- io, exn, async, log, state[S]
<full \ log>                    -- io, exn, async, state[S]
<full \ log, state[S]>         -- io, exn, async
<full \ log, state[S], exn>    -- io, async
<full \ log, state[S], exn, async>  -- io
<full \ log, state[S], exn, async, io>  -- <>  (pure!)
```

### 7.3 Alias Defined via Subtraction

```
effect alias app = io, exn, async, log, db[User]
effect alias testable = app \ io, async
-- testable = exn, log, db[User]
-- These effects are all handleable — good for testing

test-create-user : () -> <> Result[User, ApiErr]
  = catch \->
      with-list-log \->
        with-mem-db \->
          create-user test-json
```

---

## 8. Grammar Summary

Modified productions (changes in **bold**):

```
effect_row    ::= '<' effect_expr '>'
               |  '<' effect_expr '|' evar '>'
               |  '<' '>'

effect_expr   ::= effect_list
               |  effect_list '\' effect_list        -- NEW

effect_list   ::= effect_entry (',' effect_entry)*

effect_entry  ::= effect_label
               |  IDENT type_args?

effect_alias_decl ::= 'effect' 'alias' IDENT type_params? '=' effect_expr  -- MODIFIED (was effect_list)
```

New token: `\` in effect row context (already used as lambda prefix — see
Section 9 for disambiguation).

---

## 9. Disambiguation: `\` as Subtraction vs Lambda

Clank uses `\` for lambda expressions. In effect rows (inside `< >`), `\` is
the subtraction operator. These contexts are syntactically disjoint:

- `\` as **lambda**: appears at expression level, followed by a pattern/binder
- `\` as **subtraction**: appears inside `< >` angle brackets, between effect lists

The parser can distinguish by context: inside `< >` in a type annotation, `\`
is always subtraction. At expression level, `\` is always lambda. No ambiguity
arises.

```
-- lambda
\x -> x + 1

-- effect subtraction (inside angle brackets in a type)
f : A -> <web \ io> B
```

---

## 10. Design Rationale

### Why `\` and not `-`?

`\` is the standard set-difference operator in mathematics. Using `-` risks
confusion with arithmetic negation or type-level subtraction (if ever added).
The overlap with lambda syntax is not a problem because the contexts are
disjoint (Section 9).

### Why not allow subtraction from row variables?

Row variables represent *unknown* effect sets. Subtracting a known effect
from an unknown set cannot be resolved statically — it would require the
type checker to track "row variable `e` minus `io`" as a first-class
constraint. This adds significant complexity to unification for marginal
benefit. The existing idiom `<io | e>` (input) → `<e>` (output) already
expresses the same concept via row polymorphism.

### Why require all subtracted effects to be present?

Silent no-ops (subtracting an absent effect succeeds and does nothing) would
mask typos and logical errors. If you write `<web \ logging>` but `web`
doesn't contain `logging`, you probably made a mistake. Compile-time errors
catch this immediately.

### Why resolve at expansion time?

Resolving subtraction before inference means the type checker sees only
ordinary effect rows. No changes to unification, subsumption, or inference
are needed. This follows the same strategy as alias expansion (TASK-084)
and keeps the core type checker simple.

### Why not provide a union operator too?

Effect rows already support union implicitly — listing multiple effects or
aliases in a row is union. `<web, log>` is `web ∪ {log}`. An explicit `∪`
or `+` operator would be redundant.
