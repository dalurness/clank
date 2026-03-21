# Clank Affine Type Semantics — Move/Clone Rules

**Task:** TASK-007
**Mode:** Spec
**Date:** 2026-03-14
**Builds On:** Type system research (TASK-002), Core syntax (TASK-004), Effect system (TASK-005)
**Design Context:** Clank uses GC (DECISION-001), so affine types exist for resource safety and protocol correctness, not memory management.

---

## 1. Overview

Clank's type system classifies every type as either **unrestricted** (can be used any number of times) or **affine** (can be used at most once). This distinction is orthogonal to the effect system and refinement types — all three compose independently.

Affine types enforce resource protocols at compile time: files must be closed, connections must be released, tokens must be redeemed. The GC handles memory; affine types handle everything else.

### Design Goals

1. **Resource safety without borrow checking** — no lifetimes, no ownership graphs
2. **Explicit cloning** — copying an affine value requires `clone`, making duplication visible
3. **Composable** — affine and unrestricted types mix freely in compound types
4. **Minimal syntax** — one keyword (`clone`), one sigil (`&` for borrows), one marker (`affine`)
5. **Inferred where possible** — affinity propagates through compound types automatically

---

## 2. Type Classification

### 2.1 Unrestricted Types (U)

Values of unrestricted type can be used zero or more times. They can be duplicated implicitly (via `dup`, `over`, multiple references in `let` bodies, etc.).

All primitive types are unrestricted:

```
Int, Rat, Bool, Str, Byte, ()   -- all unrestricted
```

Compound types composed entirely of unrestricted types are unrestricted:

```
[Int]              -- unrestricted (List of unrestricted)
(Str, Bool)        -- unrestricted (Tuple of unrestricted)
{name: Str}        -- unrestricted (Record of unrestricted)
Int | Str          -- unrestricted (Union of unrestricted)
```

### 2.2 Affine Types (A)

Values of affine type can be used at most once. After use, the value is *consumed* — further use is a compile-time error.

Types are declared affine with the `affine` marker:

```
affine type File
affine type Connection
affine type Lock[T]
affine type TxHandle
```

Affine types may also be declared inline for type aliases:

```
type Handle = affine {fd: Int, path: Str}
```

### 2.3 Affinity Propagation

A compound type is affine if any component is affine:

```
(File, Str)        -- affine (contains File)
[Connection]       -- affine (contains Connection)
{db: Connection}   -- affine (contains Connection)
File | Int         -- affine (one variant is affine)
```

This is automatic — no annotation needed. The rule is:

```
affine(T) = T is declared affine
           ∨ ∃ component C of T. affine(C)
```

### 2.4 Function Types

Function types are always unrestricted. A closure that *captures* an affine value is itself affine (see Section 5.3).

```
Int -> <> Int                    -- unrestricted (no affine captures)
File -> <io> ()                  -- unrestricted (File is a parameter, not captured)
```

---

## 3. Move Semantics

### 3.1 Definition

**Move**: When an affine value is used, it is *moved* — ownership transfers from the source binding to the consumer. The source binding becomes *dead* and cannot be used again.

### 3.2 What Constitutes "Use"

An affine value is consumed by:

1. **Passing as an argument** to a function
2. **Returning** from a function
3. **Binding** in a `let` (transfers ownership to the new binding)
4. **Matching** in a `match` (transfers ownership into the matched branch)
5. **Placing** into a compound value (tuple, list, record)
6. **Applying** a quotation that captures it

An affine value is NOT consumed by:

1. **Borrowing** via `&` (see Section 4)
2. **Existing** — unused affine values trigger a compile-time warning (resource leak)

### 3.3 Examples

```
open : Str -> <io, exn> File
close : File -> <io> ()
read-all : &File -> <io, exn> Str

-- Correct: File is created, used once, consumed
process : Str -> <io, exn> Str
  = \path -> {
      let f = open path
      let contents = read-all &f    -- borrow: f is NOT consumed
      close f                       -- move: f IS consumed
      contents
    }

-- Error: File used after move
bad : Str -> <io, exn> Str
  = \path -> {
      let f = open path
      close f                       -- f consumed here
      read-all &f                   -- ERROR: f already consumed
    }

-- Error: File never consumed (resource leak)
leak : Str -> <io, exn> Str
  = \path -> {
      let f = open path             -- WARNING: f is never consumed
      "hello"
    }
```

### 3.4 Move in the Concatenative Model

In stack-based code, consuming a value from the stack is a move. Stack manipulation words have affine-aware behavior:

```
-- For unrestricted types:
dup  : (a -- a a)                   -- OK: duplicates freely
drop : (a -- )                      -- OK: discards freely

-- For affine types:
dup  : (a -- a a)                   -- ERROR if a is affine (use clone)
drop : (a -- )                      -- ERROR if a is affine (must consume properly)
swap : (a b -- b a)                 -- OK: reorders, no duplication
over : (a b -- a b a)              -- ERROR if a is affine (duplicates a)
rot  : (a b c -- b c a)            -- OK: reorders, no duplication
```

`drop` on an affine type is an error because it silently discards a resource. To explicitly discard an affine value without consuming it through its intended protocol, use `discard`:

```
discard : affine a -> <> ()         -- explicitly abandon a resource
```

`discard` is intentionally verbose — it marks deliberate resource abandonment and can be flagged by linters.

---

## 4. Borrowing

### 4.1 Borrow Syntax

The `&` prefix creates a *borrow* — a temporary, read-only reference to an affine value that does not consume it.

```
read-all : &File -> <io, exn> Str     -- borrows File, does not consume
file-size : &File -> <io> Int          -- borrows File, does not consume
```

### 4.2 Borrow Rules

1. **No lifetime annotations** — borrows are valid for the duration of the function call they appear in. There is no way to store a borrow in a data structure or return it.

2. **No mutation through borrows** — borrows are read-only. To mutate, consume and recreate, or use `state` effect.

3. **Multiple borrows are allowed** — an affine value can be borrowed multiple times simultaneously (since borrows are read-only).

4. **Borrow does not consume** — after all borrows end, the original value is still live and must still be consumed.

### 4.3 Borrow Typing Rule

```
G |- x : T    affine(T)
────────────────────────
G |- &x : &T             -- x remains live in G
```

A function accepting `&T` cannot move, consume, or store the value — only read it.

### 4.4 Borrow Scope

Borrows are scoped to a single expression or function call. They cannot escape:

```
-- OK: borrow lives within read-all call
let contents = read-all &f

-- ERROR: cannot return a borrow
bad-return : File -> <> &File        -- ERROR: &T cannot appear in return type
  = \f -> &f

-- ERROR: cannot store a borrow
bad-store : File -> <> {ref: &File}  -- ERROR: &T cannot appear in compound types
  = \f -> {ref: &f}
```

### 4.5 Borrow in Concatenative Model

In stack code, `&` creates a borrow that sits on the stack alongside the original:

```
-- f is on the stack (affine)
&                    -- stack: f &f (borrow pushed, original retained)
read-all             -- stack: f contents (borrow consumed by read-all)
swap close           -- stack: contents (f consumed by close)
```

The `&` word duplicates an affine value as a borrow without consuming the original. It is only valid for affine types on the stack.

---

## 5. Clone

### 5.1 The Clone Interface

Types that support explicit duplication implement `Clone`:

```
interface Clone {
  clone : &a -> a
}
```

`clone` takes a borrow and produces an independent copy. The original remains live and must still be consumed.

### 5.2 Clone Syntax

```
-- Applicative style
let f2 = clone &f          -- f still live, f2 is independent copy

-- Concatenative style
&clone                      -- shorthand: borrow + clone in one word
                            -- stack: original copy
```

`&clone` is sugar for `& clone` — borrow the top of stack, then clone it.

### 5.3 Which Types are Cloneable

| Type | Cloneable? | Notes |
|------|-----------|-------|
| Primitives (`Int`, `Str`, etc.) | N/A | Already unrestricted — `dup` works |
| `File`, `Connection` | No | Resource handles cannot be meaningfully duplicated |
| `Lock[T]` | No | Locks represent unique ownership |
| `TxHandle` | No | Transaction handles are unique |
| User-defined `affine type` | Opt-in | Implement `Clone` interface if duplication is meaningful |

### 5.4 Deriving Clone

For compound affine types where all fields are either unrestricted or cloneable:

```
affine type Session {
  id: Str,
  token: Token       -- Token is affine + Clone
}
deriving Clone for Session
```

`deriving Clone` generates `clone` by cloning each affine field and copying each unrestricted field.

---

## 6. Interaction with Let Bindings

### 6.1 Let Moves

A `let` binding moves an affine value into the new name:

```
let f = open "data.txt"     -- f owns the File
let g = f                   -- f moved to g; f is now dead
close g                     -- OK
close f                     -- ERROR: f already moved
```

### 6.2 Let with Borrows

To bind a borrow without moving:

```
let f = open "data.txt"
let contents = read-all &f   -- borrow f, f stays live
close f                      -- OK: f was never moved
```

### 6.3 Let in Branches

Each branch of an `if` or `match` must consume the same affine values. An affine value consumed in one branch must be consumed in all branches:

```
-- OK: f consumed in both branches
let f = open "data.txt"
if condition
  [close f]                  -- f consumed
  [let _ = read-all &f in
   close f]                  -- f consumed
```

```
-- ERROR: f consumed in only one branch
let f = open "data.txt"
if condition
  [close f]                  -- f consumed
  ["hello"]                  -- ERROR: f not consumed in this branch
```

### 6.4 Shadowing

Shadowing an affine binding does NOT consume it — the original must still be consumed:

```
let f = open "a.txt"
let f = open "b.txt"        -- shadows old f; old f is NOT consumed
                             -- ERROR: original f is leaked
```

---

## 7. Interaction with Quotations

### 7.1 Capture Semantics

A quotation `[...]` that references an affine value *captures* it by move. The original binding becomes dead:

```
let f = open "data.txt"
let action = [read-all &f; close f]   -- f moved into quotation
-- f is now dead; action owns it
action apply                           -- executes the quotation, consuming f inside
```

### 7.2 Affine Quotations

A quotation that captures an affine value is itself affine — it can be applied at most once:

```
let f = open "data.txt"
let q = [close f]           -- q is affine (captures affine f)
q apply                     -- OK: q consumed
q apply                     -- ERROR: q already consumed
```

This prevents double-close bugs: the quotation carries the resource's affinity.

### 7.3 Quotation Cloning

If a quotation captures only cloneable affine values, the quotation itself is cloneable:

```
let t = make-token ()        -- Token is affine + Clone
let q = [redeem &clone t]    -- Wait: this clones t inside, so q can capture the clone
-- Better pattern:
let q = [redeem t]           -- q is affine
let q2 = clone &q            -- ERROR if Token is not Clone
```

### 7.4 Non-Capturing Quotations

Quotations that don't reference any affine values are unrestricted:

```
let q = [1 2 +]             -- unrestricted: no affine captures
q apply                     -- 3
q apply                     -- 3 (fine, q is unrestricted)
```

---

## 8. Compound Types and Ownership Transfer

### 8.1 Constructing Compounds

Placing an affine value into a compound type moves it:

```
let f = open "data.txt"
let pair = (f, "hello")     -- f moved into pair; pair is affine
-- f is dead; pair must be consumed
```

### 8.2 Destructuring Compounds

Destructuring an affine compound distributes ownership:

```
let (f, name) = pair         -- pair consumed; f and name are live
close f                      -- f consumed
-- name is Str (unrestricted), no constraint
```

### 8.3 Partial Destructuring

When destructuring, all affine components must be bound (or explicitly discarded):

```
-- OK: all components bound
let (f, g) = open-pair ()    -- both affine
close f
close g

-- ERROR: affine component ignored
let (f, _) = open-pair ()    -- ERROR: second component is affine, _ discards it
                              -- Use: let (f, g) = ...; discard g
```

### 8.4 Affine Lists

A list of affine values is itself affine. Consuming the list requires consuming each element:

```
let files = [open "a.txt", open "b.txt"]   -- [File] is affine

-- Consume by closing each
files |> each close

-- ERROR: dropping the list leaks all files
drop files                   -- ERROR: [File] is affine
```

List operations on affine lists consume the list:

```
-- map consumes the input list and produces a new one
files |> map (\f -> read-all &f |> (\s -> close f; s))
-- files is consumed; result is [Str] (unrestricted)
```

### 8.5 Affine Records

Records containing affine fields are affine. Field access on affine records works through borrows or destructuring:

```
affine type DbPool {
  conn: Connection,
  name: Str
}

-- Borrow a field: read-only access, pool stays live
let n = (&pool).name        -- borrow pool, access name

-- Destructure: consumes pool, distributes ownership
let {conn, name} = pool     -- pool consumed
close conn                   -- conn consumed
```

### 8.6 Union Types

For unions containing affine variants, each match branch receives ownership of the matched variant's data:

```
type Resource = File(File) | Conn(Connection) | None

consume-resource : Resource -> <io> ()
  = match {
      File(f)  => close f,          -- f consumed
      Conn(c)  => disconnect c,     -- c consumed
      None     => ()                -- nothing to consume
    }
```

---

## 9. Formal Typing Rules

### 9.1 Extended Typing Judgement

```
G |- e : t ! E ; G'
```

Read: "Under context G, expression e has type t with effect row E, producing output context G'."

The output context G' reflects which bindings have been consumed. A binding present in G but absent in G' has been moved.

### 9.2 Context Operations

```
G, x :_U t      -- x has unrestricted type t (can be used 0+ times)
G, x :_A t      -- x has affine type t (can be used 0 or 1 times)
G \ x            -- context with x removed (consumed)
```

### 9.3 Core Rules

**Variable (unrestricted):**
```
(x :_U t) ∈ G
──────────────────
G |- x : t ! <> ; G          -- G unchanged: x still available
```

**Variable (affine):**
```
(x :_A t) ∈ G
──────────────────
G |- x : t ! <> ; G \ x      -- x consumed: removed from output context
```

**Borrow:**
```
(x :_A t) ∈ G
──────────────────
G |- &x : &t ! <> ; G        -- x NOT consumed: remains in output context
```

**Let binding:**
```
G |- e1 : t1 ! E1 ; G1
G1, x :_m t1 |- e2 : t2 ! E2 ; G2       (m = U if unrestricted(t1), A if affine(t1))
──────────────────────────────────────
G |- let x = e1 in e2 : t2 ! E1 ∪ E2 ; G2 \ x
```

**Abstraction:**
```
G, x :_m t1 |- e : t2 ! E ; G'
all affine bindings in G are in G'       -- closure cannot capture and leave unconsumed
──────────────────────────────────
G |- \x -> e : (t1 -> <E> t2) ! <> ; G'
```

If the abstraction captures affine bindings from G (i.e., some affine x in G is absent in G'), the resulting function type is itself affine.

**Application:**
```
G |- e1 : (t1 -> <E> t2) ! E1 ; G1
G1 |- e2 : t1 ! E2 ; G2
──────────────────────────────────
G |- e1 e2 : t2 ! E1 ∪ E2 ∪ E ; G2
```

**Clone:**
```
G |- e : &t ! E ; G'       Clone(t)
──────────────────────────────────
G |- clone e : t ! E ; G'
```

**Discard:**
```
G |- e : t ! E ; G'        affine(t)
──────────────────────────────────
G |- discard e : () ! E ; G'
```

**Branch (if/match) — affine consistency:**
```
G |- cond : Bool ! E0 ; G0
G0 |- e1 : t ! E1 ; G1
G0 |- e2 : t ! E2 ; G2
G1 ≡_A G2                              -- same affine bindings consumed in both branches
──────────────────────────────────────
G |- if cond e1 e2 : t ! E0 ∪ E1 ∪ E2 ; G1
```

`G1 ≡_A G2` means: for every affine binding in G0, it is present in G1 iff it is present in G2. Both branches must consume the same set of affine resources.

### 9.4 Concatenative Extensions

**Composition with affine tracking:**
```
G |- w1 : (S1 -- <E1> S2) ; G1
G1 |- w2 : (S2 -- <E2> S3) ; G2
──────────────────────────────
G |- w1 w2 : (S1 -- <E1 ∪ E2> S3) ; G2
```

**Quotation (capture):**
```
G |- w : (S1 -- <E> S2) ; G'
──────────────────────────────
G |- [w] : (S -- <> S (S1 -- <E> S2)) ; G'
```

If affine bindings were consumed by `w` (present in G, absent in G'), they are captured into the quotation. The quotation value inherits affinity if it captures any affine values.

**Dup (unrestricted only):**
```
unrestricted(t)
──────────────────────────
G |- dup : (t -- <> t t) ; G
```

**Dup (affine — rejected):**
```
affine(t)
──────────────────────────
G |- dup : (t -- <> t t)      -- TYPE ERROR: cannot duplicate affine value
```

---

## 10. Interaction with Effects

### 10.1 Independence

Affine tracking and effect tracking are independent concerns in the typing judgement:

```
G |- e : t ! E ; G'
         │   │    │
         │   │    └── affine tracking (which bindings were consumed)
         │   └─────── effect tracking (which side effects occurred)
         └─────────── value typing (what type the expression has)
```

### 10.2 Resource Effects Pattern

Affine types and effects work together for resource management:

```
open  : Str -> <io, exn> File          -- creates affine resource, performs IO
read  : &File -> <io, exn> Str         -- borrows resource, performs IO
close : File -> <io> ()                -- consumes resource, performs IO
```

The type system enforces *what* (File must be consumed); the effect system tracks *how* (IO occurs during creation, use, and consumption).

### 10.3 Effects and Handlers with Affine Values

Effect handlers must respect affinity. The continuation `k` in a handler may be affine if it captures affine state:

```
handle (computation-using-file f) {
  return x -> x,
  op v resume k -> k (process v)       -- k may be affine if f is in scope
}
```

When a handler invokes `k` zero times (aborting), any affine resources captured in the continuation are leaked. The type checker emits a warning. To handle this safely, the handler should explicitly `discard` or clean up:

```
handle (risky-op f) {
  return x -> x,
  raise e resume k -> {
    discard k          -- explicitly abandons the continuation and its resources
    default-value
  }
}
```

---

## 11. Summary of Syntax Additions

| Syntax | Meaning | Context |
|--------|---------|---------|
| `affine type T` | Declare T as affine | Type declarations |
| `affine {fields}` | Inline affine compound | Type expressions |
| `&x` | Borrow x (read-only, non-consuming) | Expressions |
| `&T` | Borrow type | Type signatures |
| `clone &x` | Explicitly duplicate an affine value | Expressions |
| `&clone` | Borrow-then-clone (concatenative sugar) | Stack expressions |
| `discard x` | Explicitly abandon an affine value | Expressions |
| `Clone` | Interface for cloneable types | Interface declarations |
| `deriving Clone` | Auto-generate clone for compound types | Type declarations |

Total new keywords: 3 (`affine`, `clone`, `discard`)
Total new syntax: `&` prefix for borrows, `deriving` clause

---

## 12. Complete Examples

### 12.1 File Processing Pipeline

```
use std.io (open, close, read-all)

process-file : Str -> <io, exn> [Str]
  = \path -> {
      let f = open path
      let text = read-all &f
      close f
      text |> lines |> filter (\l -> len l > 0)
    }
```

### 12.2 Connection Pool (Affine Container)

```
affine type Pool {
  conns: [Connection],
  max: Int
}

create-pool : Int -> <io, exn> Pool
  = \n -> {
      let conns = 1 .. n |> map (\_ -> connect "localhost:5432")
      {conns: conns, max: n}
    }

drain-pool : Pool -> <io> ()
  = \pool -> {
      let {conns, max} = pool     -- pool consumed, conns now live
      conns |> each disconnect    -- each Connection consumed
    }
```

### 12.3 Clone for Retryable Operations

```
affine type Request {
  body: Str,
  headers: [(Str, Str)]
}
deriving Clone for Request

retry : (Request, Int) -> <io, exn> Response
  = \req n -> {
      if (n <= 0)
        [send req]                    -- last attempt: consume req
        [{
          let req2 = clone &req       -- clone for this attempt
          let resp = try (\-> send req2)
          match resp {
            Some(r) => { discard req; r },
            None    => retry (req, n - 1)
          }
        }]
    }
```

### 12.4 Quotation with Affine Capture

```
-- Build a cleanup action, pass it around, execute later
make-cleanup : File -> <> (() -> <io> ())
  = \f -> \-> close f          -- f captured by move; closure is affine

process-with-cleanup : Str -> <io, exn> Str
  = \path -> {
      let f = open path
      let cleanup = make-cleanup (clone &f)   -- ERROR if File not Clone
      -- Better: don't clone, just defer
      let f = open path
      let result = read-all &f
      close f                                  -- consume directly
      result
    }
```

### 12.5 Concatenative Style

```
-- Stack-based file reading
read-file : (Str -- <io, exn> Str)
  = open              -- Str -- File
    &                  -- File -- File &File
    read-all           -- File &File -- File Str
    swap               -- File Str -- Str File
    close              -- Str File -- Str
    swap               -- Str -- Str

-- With pipeline sugar
read-file : Str -> <io, exn> Str
  = \path -> open path |> \f -> {
      let s = read-all &f
      close f
      s
    }
```

---

## 13. Design Rationale

### Why affine (at most once) not linear (exactly once)?

Linear types require every value to be consumed exactly once — you must use it, no exceptions. Affine types allow *not using* a value (with a warning for resources). This is more practical:
- Dead code paths may leave values unconsumed during development
- Error handling may skip cleanup (handled by GC as fallback)
- Warnings are sufficient to catch leaks without blocking compilation

### Why no lifetimes?

DECISION-001 established that borrow checking with lifetime tracking is too expensive for LLM token budgets. Clank's borrows are scoped to a single expression — no lifetime parameters, no lifetime inference, no lifetime errors. The cost is that borrows cannot be stored in data structures, which is an acceptable tradeoff for simplicity.

### Why explicit clone instead of implicit copy?

Making duplication visible serves two purposes:
1. It documents intent — "I know I'm duplicating this resource"
2. It creates a grep-able signal — all resource duplication sites are findable

For unrestricted types, implicit duplication (`dup`, multiple use) works fine. The explicit `clone` only applies to affine types.

### Why `discard` instead of `drop`?

`drop` is already a stack word (remove top of stack) and works for unrestricted types. `discard` is intentionally distinct and verbose — it means "I am deliberately abandoning this resource without consuming it through its intended protocol." Linters can flag all `discard` calls for review.

### Why are borrows read-only?

Mutable borrows (Rust's `&mut`) require the borrow checker to enforce exclusivity — exactly the complexity Clank avoids. Read-only borrows are simple: any number can coexist, no ordering constraints, no data races. Mutation happens through the `state` effect or by consuming and recreating values.

### Interaction with GC

The GC handles memory for all values, including affine ones. If an affine value is leaked (e.g., via `discard` or due to a bug), the GC will eventually reclaim the memory. What the GC does NOT do is run cleanup logic — `close`, `disconnect`, etc. are explicit protocol steps that affine types enforce. This separation means:
- Memory: always safe (GC)
- Resources: safe when affine types are respected (type system)
- Fallback: GC reclaims memory, but file descriptors / connections may leak if `discard` is used carelessly
