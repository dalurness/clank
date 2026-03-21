# Clank Effect System Specification

**Task:** TASK-005
**Mode:** Spec
**Date:** 2026-03-14
**Primary Influence:** Koka (Leijen, 2014–present)
**Adapted For:** Clank's terse applicative syntax

---

## 1. Overview

Clank uses an algebraic effect system to track, control, and handle computational
side effects. Every function's type includes an *effect row* describing which
effects it may perform. Effects are inferred by default and annotated only when
needed (module boundaries, documentation, disambiguation).

The effect system unifies error handling, state, IO, and async under a single
mechanism — no separate `try/catch`, `async/await`, or monad transformer stacks.

### Design Goals

1. **Inferred by default** — most code needs zero effect annotations
2. **Terse** — effect syntax adds minimal tokens
3. **Unified** — one mechanism replaces exceptions, async, state, IO
4. **Composable** — effects compose via row polymorphism; no ordering constraints
5. **Handleable** — callers decide how effects are interpreted via handlers

---

## 2. Effect Row Syntax

An effect row is an unordered set of effect labels, enclosed in angle brackets
and attached to a function's return type.

### Grammar

```
effect_row    ::= '<' effect_list '>'
               |  '<' '>'                      -- pure (no effects)
               |  '<' effect_list '|' evar '>'  -- open row (polymorphic)

effect_list   ::= effect_label (',' effect_label)*

effect_label  ::= IDENT                        -- simple effect: io, exn, state
               |  IDENT type_args              -- parameterized: exn[E], state[S]

evar          ::= IDENT                        -- effect variable (lowercase)
```

### Examples

```
-- pure function: no effects
add : (Int, Int) -> <> Int

-- performs IO and may raise exceptions
read : Str -> <io, exn> Str

-- polymorphic: may raise a typed exception
parse : Str -> <exn[ParseErr] | e> Ast

-- open row: preserves caller's effects
map : ((a -> <e> b), List a) -> <e> List b
```

---

## 3. Built-in Effects

Clank defines four built-in effects. These cover the fundamental categories
of side effects in practical programs.

### 3.1 `io` — Input/Output

Marks functions that interact with the external world: file system, network,
stdout, environment variables, system clock.

```
io : effect
```

No type parameter. A function is either `io` or not.

**Operations:**
```
print   : Str -> <io> ()
read-ln : () -> <io> Str
read-f  : Str -> <io, exn> Str
write-f : (Str, Str) -> <io, exn> ()
now     : () -> <io> Time
env     : Str -> <io> Str?
```

`io` is *not handleable* in normal code — it is a primitive effect that
represents real-world interaction. It can only be discharged at the program
entry point (the runtime provides the implicit `io` handler).

### 3.2 `exn[E]` — Exceptions

Represents operations that may fail with a value of type `E`. This replaces
try/catch/throw with a typed, tracked mechanism.

```
exn : effect (E: Type)
```

**Operations:**
```
raise : E -> <exn[E]> a           -- raise an exception (returns any type, never actually returns)
```

**Handling:** `exn` is the primary handleable effect — see Section 5.

When the type parameter is omitted, `exn` is shorthand for `exn[Err]` where
`Err` is the built-in error type.

### 3.3 `state[S]` — Mutable State

Represents read/write access to a mutable value of type `S`. This replaces
mutable variables with a tracked, handleable effect.

```
state : effect (S: Type)
```

**Operations:**
```
get : () -> <state[S]> S          -- read current state
set : S -> <state[S]> ()          -- overwrite state
mod : (S -> S) -> <state[S]> ()   -- modify state with a function
```

**Handling:** `state` handlers provide the initial value and determine
whether state is thread-local, shared, logged, etc.

### 3.4 `async` — Asynchronous Operations

Represents operations that may suspend and resume — network requests,
timers, concurrent tasks.

```
async : effect
```

**Operations:**
```
await  : Future a -> <async> a          -- suspend until future resolves
spawn  : (() -> <async | e> a) -> <async | e> Future a  -- launch concurrent task
sleep  : Duration -> <async> ()         -- suspend for duration
yield  : () -> <async> ()               -- cooperative yield
```

`async` interacts with `exn` — a failed async operation raises `exn`.
`spawn` propagates the caller's effect row (excluding `async` itself) to
the spawned task.

---

## 4. Effect Rows and Row Polymorphism

### 4.1 Row Algebra

Effect rows are *unordered sets* with a possible tail variable. Two rows
are equivalent if they contain the same labels (order does not matter).

```
<io, exn> ≡ <exn, io>
```

Row operations:

| Operation | Notation | Meaning |
|-----------|----------|---------|
| Empty row | `<>` | Pure — no effects |
| Extension | `<l \| r>` | Row `r` extended with label `l` |
| Row variable | `<e>` | Unknown row (polymorphic) |
| Open row | `<io \| e>` | Known `io` effect + unknown rest |

### 4.2 Row Unification

Effect rows unify using standard row unification (Remy-style):

1. `<> ≡ <>` — empty rows unify
2. `<l | r1> ≡ <l | r2>` when `r1 ≡ r2` — same head label, unify tails
3. `<l | r1> ≡ <l' | r2>` when `l ≠ l'` — rewrite: find `l` in `r2` and `l'` in `r1`
4. `<l | r> ≡ e` — unify `e := <l | e'>` with fresh `e'`, then unify `r ≡ e'`

### 4.3 Subeffecting (Effect Subsumption)

A function with fewer effects can be used where more effects are expected.
A pure function `<>` can be used anywhere.

```
<> ⊆ <io>
<exn> ⊆ <io, exn>
<e> ⊆ <l | e>     -- for any label l
```

This is implemented via row extension during unification — the type checker
widens the row by unifying the tail variable.

---

## 5. Effect Handlers

Handlers interpret effects. A handler for effect `E` provides implementations
for each of `E`'s operations and transforms a computation using `E` into one
that does not.

### 5.1 Handler Syntax

```
handle_expr ::= 'handle' expr '{' handler_clauses '}'

handler_clauses ::= handler_clause (',' handler_clause)*

handler_clause  ::= 'return' pattern '->' expr          -- return clause
                 |  operation_name pattern 'resume' IDENT '->' expr  -- operation clause
```

### 5.2 Examples

**Handling exceptions — converting to Option:**

```
-- Applicative style
safe-div : (Int, Int) -> <> Int?
  = \n d -> handle (div n d) {
      return x -> some x,
      raise _ resume _ -> none
    }
```

**Handling state — running with initial value:**

```
counter : () -> <> (Int, Str)
  = handle (build-greeting) {
      return x -> (get!, x),          -- return final state and result
      get () resume k -> k (get!),    -- thread state through
      set s resume k -> set! s; k ()  -- update state
    } with state = 0
```

Simplified form using the built-in `run-state` handler:

```
counter : () -> <> (Int, Str)
  = run-state 0 build-greeting
```

**Handling async — running an event loop:**

The runtime provides the top-level `async` handler. User code does not
typically handle `async` directly, but custom schedulers can:

```
run-sequential : (() -> <async | e> a) -> <e> a
  = \f -> handle (f ()) {
      return x -> x,
      await fut resume k -> k (force fut),
      spawn g resume k -> k (lazy g),
      yield () resume k -> k ()
    }
```

### 5.3 Handler Typing

A handler for effect `E` with operations `op1 : A1 -> E B1, ...` in context
where the handled computation has type `() -> <E | e> T` and return type `R`:

```
handle : (() -> <E | e> T) -> <e> R
  where
    return : T -> <e> R
    op_i   : A_i -> (B_i -> <e> R) -> <e> R    -- for each operation
```

The `resume` parameter (also called the continuation `k`) has type
`B_i -> <e> R` — it captures the rest of the computation after the
operation. The handler may call it zero times (abort), once (resume normally),
or multiple times (nondeterminism, backtracking).

### 5.4 Built-in Handler Combinators

These are stdlib functions, not language constructs — they're defined using
the handler mechanism:

```
-- Run exn, converting to Option
try     : (() -> <exn[E] | e> a) -> <e> a?

-- Run exn, converting to Result
catch   : (() -> <exn[E] | e> a) -> <e> Result[a, E]

-- Run state with initial value, return (final_state, result)
run-state : s -> (() -> <state[s] | e> a) -> <e> (s, a)

-- Run state, discard final state
eval-state : s -> (() -> <state[s] | e> a) -> <e> a

-- Run state, discard result
exec-state : s -> (() -> <state[s] | e> a) -> <e> s

-- Default handler: re-raise with different error type
map-exn : (E1 -> E2) -> (() -> <exn[E1] | e> a) -> <exn[E2] | e> a
```

---

## 6. Effect Polymorphism

### 6.1 Effect-Polymorphic Functions

Functions that are generic over effects use row variables. This is critical
for higher-order functions like `map`, `filter`, `fold`.

```
map    : ((a -> <e> b), List a) -> <e> List b
filter : ((a -> <e> Bool), List a) -> <e> List a
fold   : ((b, a -> <e> b), b, List a) -> <e> b
```

The effect variable `e` is universally quantified — these functions work
with any effect row. If passed a pure function, the result is pure. If
passed an IO function, the result has IO.

### 6.2 Effect Abstraction

User-defined functions are automatically generalized over effect variables
that appear free in the inferred type. No explicit `forall` syntax is needed
for effect polymorphism.

```
-- inferred type: apply : (a -> <e> b, a) -> <e> b
apply = \f x -> f x

-- inferred type: twice : (a -> <e> a, a) -> <e> a
twice = \f x -> f (f x)
```

### 6.3 Effect Constraints

When a function requires a specific effect to be present (or absent), this
is expressed via the row:

```
-- requires io to be present
log : Str -> <io | e> ()

-- requires io to be ABSENT (pure except for state)
-- achieved by listing only the allowed effects
pure-counter : () -> <state[Int]> Int
```

---

## 7. User-Defined Effects

Users can define custom effects for domain-specific contracts.

### 7.1 Effect Declaration Syntax

```
effect_decl ::= 'effect' IDENT type_params? '{' operation_decls '}'

operation_decls ::= operation_decl (','  operation_decl)*

operation_decl  ::= IDENT ':' type '->' type
```

### 7.2 Examples

```
-- A logging effect
effect log {
  info  : Str -> (),
  warn  : Str -> (),
  error : Str -> ()
}

-- A database effect (parameterized)
effect db[R] {
  query  : Str -> List R,
  insert : R -> (),
  delete : Str -> Int
}

-- A nondeterminism effect
effect nd {
  choose : List a -> a,
  fail   : () -> a
}
```

### 7.3 Using Custom Effects

```
process : (Str) -> <db[User], log, exn> ()
  = \name -> {
      info ("processing: " ++ name)
      let users = query ("name=" ++ name)
      when (empty users) (raise (not-found name))
      each users \u -> insert (update-ts u)
    }
```

The function's effect row documents exactly what side effects it performs —
no more, no less. An agent reading this signature knows immediately that
`process` touches the database, logs, and may fail.

### 7.4 Handling Custom Effects

```
-- Handle the log effect by printing to stdout
with-stdout-log : (() -> <log | e> a) -> <io | e> a
  = \f -> handle (f ()) {
      return x -> x,
      info msg resume k  -> print ("[INFO] " ++ msg); k (),
      warn msg resume k  -> print ("[WARN] " ++ msg); k (),
      error msg resume k -> print ("[ERR] " ++ msg); k ()
    }

-- Handle the log effect by collecting into a list
with-list-log : (() -> <log | e> a) -> <state[List Str] | e> a
  = \f -> handle (f ()) {
      return x -> x,
      info msg resume k  -> mod (\xs -> xs ++ [msg]); k (),
      warn msg resume k  -> mod (\xs -> xs ++ [msg]); k (),
      error msg resume k -> mod (\xs -> xs ++ [msg]); k ()
    }
```

Same code, different handlers — the effect system enables dependency injection
and testability without any framework.

---

## 8. Inference Rules

### 8.1 Core Typing Judgement

```
G |- e : t ! E
```

Read: "Under context G, expression e has type t with effect row E."

### 8.2 Rules

**Variable:**
```
(x : t) in G
─────────────
G |- x : t ! <>
```

**Abstraction:**
```
G, x : t1 |- e : t2 ! E
────────────────────────
G |- \x -> e : (t1 -> <E> t2) ! <>
```

**Application:**
```
G |- e1 : (t1 -> <E> t2) ! E1
G |- e2 : t1 ! E2
──────────────────────────────
G |- e1 e2 : t2 ! E1 ∪ E2 ∪ E
```

**Effect operation:**
```
op : t1 -> t2  is an operation of effect L
──────────────────────────────────────
G |- op v : t2 ! <L | e>
```
(with fresh effect variable `e`)

**Handle:**
```
G |- e : t1 ! <L | E>
G |- return : t1 -> <E> t2
G |- op_i : A_i -> (B_i -> <E> t2) -> <E> t2    for each op_i in L
────────────────────────────────────────────────
G |- handle e { return ..., op_i ... } : t2 ! E
```

**Subsumption (effect widening):**
```
G |- e : t ! E1
E1 ⊆ E2
──────────────
G |- e : t ! E2
```

---

## 9. Interaction with Other Type System Features

### 9.1 Effects and Refinement Types

Effect annotations and refinement predicates are orthogonal and compose:

```
safe-div : ({Int | true}, {Int | > 0}) -> <> Int
read-pos : Str -> <io, exn> {Int | > 0}
```

A pure function with a refinement is verified both for effects (none) and
for its value contract (the predicate).

### 9.2 Effects and Affine Types

Affine (move) semantics interact with effects through resource effects:

```
open  : Str -> <io, exn> File      -- File is affine: must be consumed
close : File -> <io> ()             -- consuming the File resource
read  : &File -> <io, exn> Str     -- borrowing (does not consume)
```

The type system ensures that `File` values are consumed exactly once.
Effects track what happens during that consumption. These are independent
concerns that compose without interference.

### 9.3 Effects and the Total/Partial Distinction

A function with an empty effect row `<>` is *total* if it also terminates.
The effect system tracks *what* side effects occur but does not track
termination. A separate `div` (divergence) effect can optionally mark
potentially non-terminating functions:

```
-- total and pure
len : List a -> <> Int

-- pure but may diverge (infinite loop)
loop : () -> <div> a

-- in practice, div is usually left implicit and inferred
```

Whether `div` is tracked explicitly is a design knob — for v1, recommend
leaving it implicit (all functions may diverge) to keep the system simple.

---

## 10. Complete Examples

### 10.1 Error Handling Pipeline

```
-- Parse, validate, and store a user — effects fully tracked
create-user : Str -> <io, exn[ApiErr], db[User], log> User
  = \json -> {
      info "parsing user request"
      let req = parse json              -- may raise exn[ParseErr]
      let user = validate req           -- may raise exn[ValidationErr]
      insert user                       -- db effect
      info ("created: " ++ user.id)
      user
    }

-- Handle all effects at the boundary
main : () -> <io> ()
  = {
      let result = catch \-> {
        with-stdout-log \-> {
          with-mem-db \-> {
            create-user (read-ln ())
          }
        }
      }
      match result {
        ok user  -> print ("created " ++ user.id),
        err e    -> print ("error: " ++ show e)
      }
    }
```

### 10.2 Stateful Computation

```
-- Count words using state effect
count-words : List Str -> <state[Map Str Int]> ()
  = \words -> each words \w -> {
      let counts = get ()
      set (update counts w (\n -> n + 1) 0)
    }

-- Run it purely
word-freq : List Str -> <> Map Str Int
  = \words -> exec-state {} \-> count-words words
```

### 10.3 Custom Effect — Nondeterminism

```
effect nd {
  choose : List a -> a,
  fail   : () -> a
}

-- Pythagorean triples
pytriples : Int -> <nd> (Int, Int, Int)
  = \n -> {
      let a = choose (1 .. n)
      let b = choose (a .. n)
      let c = choose (b .. n)
      when (a*a + b*b /= c*c) (fail ())
      (a, b, c)
    }

-- Handler: collect all solutions
all-solutions : (() -> <nd | e> a) -> <e> List a
  = \f -> handle (f ()) {
      return x -> [x],
      choose xs resume k -> concat-map k xs,
      fail () resume _ -> []
    }

-- Usage
triples = all-solutions \-> pytriples 20
-- => [(3,4,5), (5,12,13), (6,8,10), ...]
```

### 10.4 Async with Error Handling

```
fetch-all : List Str -> <async, exn, io> List Response
  = \urls -> {
      let futures = map (\u -> spawn \-> http-get u) urls
      map await futures
    }

-- With timeout and error recovery
fetch-safe : List Str -> <async, io> List Response?
  = \urls -> map (\u -> try \-> {
      let f = spawn \-> http-get u
      with-timeout 5s (await f)
    }) urls
```

---

## 11. Summary of Syntax Tokens

| Token | Meaning | Context |
|-------|---------|---------|
| `<>` | Empty effect row (pure) | Type annotations |
| `<io>` | IO effect | Type annotations |
| `<exn>` | Exception effect (default Err type) | Type annotations |
| `<exn[E]>` | Typed exception effect | Type annotations |
| `<state[S]>` | State effect | Type annotations |
| `<async>` | Async effect | Type annotations |
| `<e>` | Effect variable (polymorphic) | Type annotations |
| `<io \| e>` | Open row (io + unknown) | Type annotations |
| `effect` | Declare custom effect | Declarations |
| `handle` | Handle effects | Expressions |
| `resume` | Name the continuation in handler | Handler clauses |
| `return` | Return clause in handler | Handler clauses |
| `raise` | Raise exception | Expressions |
| `get` | Read state | Expressions |
| `set` | Write state | Expressions |
| `mod` | Modify state | Expressions |
| `await` | Await future | Expressions |
| `spawn` | Launch async task | Expressions |

Total new keywords: 4 (`effect`, `handle`, `resume`, `return`)
Total new syntax: angle brackets for effect rows, `|` for row extension

---

## 12. Design Rationale

### Why Koka-style over monads?
Monads compose poorly (monad transformer stacks) and require explicit lifting.
Effects with row polymorphism compose freely — `<io, exn>` just works without
`MonadIO` or `lift`. For agents, effect rows are more predictable and parseable
than monad transformer types.

### Why row polymorphism?
Without row polymorphism, higher-order functions like `map` would need separate
versions for pure and effectful functions. Row variables allow one definition
to serve all use cases. This directly serves Principle 3 (simplicity).

### Why not track divergence (div) by default?
Tracking termination requires totality checking, which significantly increases
type checker complexity. For v1, all functions may diverge. This can be added
later as an optional effect without breaking existing code.

### Why is io not handleable?
Handling `io` would mean intercepting real-world interactions — this is useful
for testing (mock IO) but dangerous if misused. For v1, `io` is primitive.
A future `mock-io` handler for testing can be added as a controlled extension.

### Why four built-in effects?
`io`, `exn`, `state`, and `async` cover >95% of side effects in practical
programs. Keeping the built-in set small serves the spec-size constraint
(≤5000 tokens). User-defined effects handle domain-specific needs.

