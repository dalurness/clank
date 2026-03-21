# Clank Shared Mutable State Primitives

**Task:** TASK-086
**Mode:** Spec
**Date:** 2026-03-19
**Builds On:** Affine types (TASK-007), Effect system (TASK-005), Async runtime (TASK-014), GC strategy (TASK-013)
**Design Context:** Clank uses task-local `state[S]` effects and channels for inter-task communication. This spec adds shared mutable state primitives that compose with affine types, effects, and structured concurrency.

---

## 1. Overview

Clank's concurrency model currently provides two mechanisms for inter-task data flow:

1. **`state[S]` effect** — task-local mutable state (per async-runtime spec §9.3)
2. **Channels** — ownership transfer via message passing

Neither supports *shared mutable state* — a single mutable cell readable and writable by multiple tasks. This spec introduces three primitives at increasing levels of power:

| Primitive | Purpose | Complexity |
|-----------|---------|------------|
| `Ref[T]` | Atomic single-cell shared reference | Low |
| `AtomicVal[T]` | Lock-free atomic for primitive types | Lowest |
| `Stm` effect + `TVar[T]` | Composable multi-variable transactions | Medium |

### Design Goals

1. **Affine-safe** — shared refs interact correctly with move/borrow/clone semantics
2. **Effect-tracked** — all shared state access is visible in the effect row
3. **Structured** — shared refs are scoped to task groups; no global mutable state
4. **Composable** — STM transactions compose without deadlock
5. **Minimal** — small surface area; complex concurrent data structures are stdlib, not builtins

---

## 2. Ref[T] — Atomic Shared Reference Cell

### 2.1 Definition

A `Ref[T]` is a heap-allocated mutable cell holding a value of type `T`. Multiple tasks can read and write it concurrently. All operations are atomic (linearizable).

```
affine type Ref[T]
```

`Ref` is affine — it must be explicitly closed when no longer needed. This prevents leaked shared state. `Ref` is cloneable — multiple tasks can hold handles to the same cell:

```
deriving Clone for Ref[T]
```

Cloning a `Ref` creates another handle to the *same* cell (not a copy of the contents). All cloned handles must eventually be closed; the underlying cell is freed when the last handle is closed.

### 2.2 The `shared` Effect

Access to shared state is tracked via a new built-in effect:

```
effect shared {
  ref-read   : Ref[T] -> T,
  ref-write  : (Ref[T], T) -> (),
  ref-swap   : (Ref[T], T) -> T,
  ref-modify : (Ref[T], T -> T) -> T
}
```

This makes shared state access visible in function signatures:

```
increment : Ref[Int] -> <shared> ()
  = \r -> ref-modify (r, \n -> n + 1); ()

-- Callers see <shared> in the effect row
run-workers : () -> <async, shared, io> ()
```

### 2.3 Operations

```
-- Create a new Ref with initial value
ref-new : T -> <shared> Ref[T]

-- Read current value (atomic snapshot)
ref-read : &Ref[T] -> <shared> T

-- Write new value (atomic replace)
ref-write : (&Ref[T], T) -> <shared> ()

-- Swap: atomically replace and return old value
ref-swap : (&Ref[T], T) -> <shared> T

-- Modify: atomically apply function, return new value
ref-modify : (&Ref[T], T -> <> T) -> <shared> T

-- Compare-and-swap: atomic CAS, returns (success, current_value)
ref-cas : (&Ref[T], T, T) -> <shared> (Bool, T)
  where T : Eq

-- Close a Ref handle
ref-close : Ref[T] -> <> ()
```

**Key design decisions:**

- `ref-read`, `ref-write`, `ref-swap`, `ref-modify`, and `ref-cas` take `&Ref[T]` (borrow). The ref handle is not consumed by access — only by `ref-close`. This allows repeated reads/writes through the same handle.
- `ref-modify` requires a **pure** modifier function (`T -> <> T`). This prevents effects inside atomic updates (no IO, no exceptions, no async inside a modify). Impure updates must use `ref-swap` or `ref-cas` with explicit read-modify-write.
- `ref-close` consumes the handle. When the last handle is closed, the cell's contents become unreachable (GC collects).

### 2.4 Interaction with Affine Types

#### Unrestricted T

When `T` is unrestricted (e.g., `Int`, `Str`), `ref-read` freely copies the value:

```
let r = ref-new 42
let v1 = ref-read &r        -- v1 = 42
let v2 = ref-read &r        -- v2 = 42 (independent copy)
ref-close r
```

#### Affine T — The Take/Put Protocol

When `T` is affine, `ref-read` cannot simply copy the value — that would duplicate an affine resource. Instead, shared refs containing affine values use a **take/put** protocol:

```
-- Take: atomically remove the value, leaving the cell empty
ref-take : &Ref[T] -> <shared, exn[RefEmpty]> T
  where affine(T)

-- Put: atomically place a value into an empty cell
ref-put : (&Ref[T], T) -> <shared, exn[RefFull]> ()
  where affine(T)
```

`ref-take` extracts the value (the cell becomes empty). `ref-put` fills it back. This maintains single-ownership: at any moment, the affine value is either inside the ref or owned by exactly one task.

```
affine type Token

let r : Ref[Token] = ref-new (make-token ())

-- Task A: take the token, use it, put it back
let tok = ref-take &r          -- cell is now empty
let result = redeem &tok
ref-put (&r, tok)               -- cell is full again

-- Task B trying ref-take while cell is empty:
let tok = ref-take &r          -- raises exn[RefEmpty]
```

For affine `T`, `ref-read`, `ref-write`, `ref-swap`, and `ref-modify` are **not available** — the compiler rejects them. Only `ref-take`, `ref-put`, and `ref-cas` (which swaps ownership atomically) are permitted.

**Typing rule:**

```
affine(T)
────────────────────────────────
ref-read  : &Ref[T] -> ...    -- TYPE ERROR: cannot copy affine T
ref-write : (&Ref[T], T) -> ... -- TYPE ERROR: overwrites without consuming old T
ref-take  : &Ref[T] -> <shared, exn[RefEmpty]> T    -- OK
ref-put   : (&Ref[T], T) -> <shared, exn[RefFull]> () -- OK
```

#### Optional Values in Affine Refs

A common pattern wraps affine values in `Option`:

```
let r : Ref[Token?] = ref-new (some (make-token ()))

-- Atomic take:
let tok = ref-swap (&r, none)    -- returns some(token) or none
-- ref-swap is allowed here because Token? is unrestricted
-- (Option of affine is itself affine, BUT swap preserves ownership)
```

Actually, `Token?` is affine (contains affine variant). The take/put protocol applies. The `Ref[Token?]` pattern doesn't escape the restriction — use `ref-take`/`ref-put` regardless.

### 2.5 Scoping to Task Groups

Refs should not outlive their task group. A `Ref` created inside a task group must be closed before the group exits. The affine type system enforces this naturally — if the `Ref` handle is not consumed (closed), the compiler emits a resource-leak warning.

```
task-group \-> {
    let r = ref-new 0
    let f1 = spawn \-> ref-modify (&(clone &r), \n -> n + 1); ()
    let f2 = spawn \-> ref-modify (&(clone &r), \n -> n + 1); ()
    await f1
    await f2
    let final = ref-read &r
    ref-close r                  -- must close before group exits
    final
  }
```

Wait — `clone &r` creates a new handle. Those cloned handles are moved into spawned tasks and consumed there (task ends, handle becomes unreachable). The original `r` is closed explicitly. All handles accounted for.

**Correction:** cloned handles moved into spawn closures are consumed when the closure finishes (affine closure captures). But they need explicit `ref-close` too:

```
task-group \-> {
    let r = ref-new 0
    let r1 = clone &r
    let r2 = clone &r
    let f1 = spawn \-> { ref-modify (&r1, \n -> n + 1); ref-close r1; () }
    let f2 = spawn \-> { ref-modify (&r2, \n -> n + 1); ref-close r2; () }
    await f1
    await f2
    let final = ref-read &r
    ref-close r
    final
  }
```

### 2.6 Handling the `shared` Effect

The `shared` effect is handled by the runtime (like `io` and `async`). It is not user-handleable in normal code — shared state operations map directly to atomic VM operations.

For testing, a mock handler can intercept `shared` operations:

```
-- NOT available in v1; noted as future extension
mock-shared : (() -> <shared | e> a) -> <e> a
```

---

## 3. AtomicVal[T] — Lock-Free Primitive Atomics

### 3.1 Definition

For primitive unrestricted types (`Int`, `Bool`), `AtomicVal[T]` provides lightweight atomic operations without the overhead of a full `Ref`.

```
type AtomicVal[T] where T : Atomic
  -- T must be a type that supports atomic operations
```

The `Atomic` interface is satisfied by: `Int`, `Bool`.

`AtomicVal` is **not affine** — it is unrestricted. It can be freely shared and does not need explicit closing. This is safe because it holds only unrestricted primitive values.

### 3.2 Operations

```
atomic-new   : T -> <shared> AtomicVal[T]
atomic-load  : AtomicVal[T] -> <shared> T
atomic-store : (AtomicVal[T], T) -> <shared> ()
atomic-cas   : (AtomicVal[T], T, T) -> <shared> (Bool, T)
atomic-add   : (AtomicVal[Int], Int) -> <shared> Int    -- returns old value
atomic-sub   : (AtomicVal[Int], Int) -> <shared> Int
```

### 3.3 When to Use AtomicVal vs Ref

| Use Case | Primitive |
|----------|-----------|
| Shared counter | `AtomicVal[Int]` |
| Shared flag | `AtomicVal[Bool]` |
| Shared compound value | `Ref[T]` |
| Shared affine resource | `Ref[T]` with take/put |
| Multiple values updated together | STM (Section 4) |

---

## 4. STM — Software Transactional Memory

### 4.1 Motivation

`Ref[T]` provides atomic single-cell operations. But many concurrent algorithms need to update *multiple* cells atomically. Without composable transactions, programmers resort to lock ordering, which is error-prone and deadlock-susceptible.

STM provides **composable atomic transactions** over multiple transactional variables.

### 4.2 TVar[T] — Transactional Variable

```
affine type TVar[T]
deriving Clone for TVar[T]
```

Like `Ref`, `TVar` is an affine handle to a shared mutable cell. Unlike `Ref`, reads and writes to `TVar` are only valid inside an STM transaction.

### 4.3 The `stm` Effect

```
effect stm {
  tvar-read  : TVar[T] -> T,
  tvar-write : (TVar[T], T) -> ()
}
```

STM operations produce the `stm` effect, which is distinct from `shared`. The `stm` effect can only be discharged by the `atomically` handler.

### 4.4 Operations

```
-- Create a new TVar (outside transactions)
tvar-new : T -> <shared> TVar[T]

-- Read a TVar (inside transactions only)
tvar-read : &TVar[T] -> <stm> T

-- Write a TVar (inside transactions only)
tvar-write : (&TVar[T], T) -> <stm> ()

-- Execute a transaction atomically
atomically : (() -> <stm | e> a) -> <shared | e> a

-- Retry: abort and restart when any read TVar changes
retry : () -> <stm> a

-- Choice: try first, if it retries, try second
or-else : (() -> <stm> a, () -> <stm> a) -> <stm> a

-- Close a TVar handle
tvar-close : TVar[T] -> <> ()
```

### 4.5 Transaction Semantics

`atomically` executes its body optimistically:

1. **Read phase:** `tvar-read` operations record the TVar and its value in a read set
2. **Write phase:** `tvar-write` operations buffer writes in a write set (not applied yet)
3. **Commit:** At the end, validate that all read values are still current. If valid, apply all writes atomically. If invalid, discard writes and retry the entire transaction.

This is standard optimistic STM (as in Haskell's `STM` monad / GHC's implementation).

**Key properties:**
- **No deadlocks** — no locks are held; conflicts are resolved by retry
- **Composable** — two STM actions can be sequenced inside `atomically` and the whole thing is atomic
- **No side effects inside transactions** — the `stm` effect row prevents `io`, `async`, or other effects inside a transaction body (they would be replayed on retry)

### 4.6 Interaction with Affine Types

For affine `T` inside `TVar[T]`, the same take/put discipline applies:

```
tvar-take : &TVar[T] -> <stm, exn[TVarEmpty]> T
  where affine(T)

tvar-put : (&TVar[T], T) -> <stm, exn[TVarFull]> ()
  where affine(T)
```

`tvar-read` and `tvar-write` are rejected for affine `T`, just as with `Ref`.

### 4.7 STM Examples

#### Bank Transfer (Classic)

```
transfer : (&TVar[Int], &TVar[Int], Int) -> <stm, exn> ()
  = \from to amount -> {
      let balance = tvar-read from
      when (balance < amount) (raise (InsufficientFunds balance))
      tvar-write (from, balance - amount)
      tvar-write (to, (tvar-read to) + amount)
    }

-- Execute atomically
do-transfer : (TVar[Int], TVar[Int]) -> <shared, exn> ()
  = \acct1 acct2 -> atomically \-> transfer (&acct1, &acct2, 100)
```

Both accounts update atomically — no observer sees an intermediate state.

#### Blocking Queue with Retry

```
bounded-queue-take : &TVar[[a]] -> <stm> a
  = \q -> {
      let items = tvar-read q
      match items {
        [] -> retry (),              -- block until items available
        [x, ..rest] -> {
            tvar-write (q, rest)
            x
          }
      }
    }

bounded-queue-put : (&TVar[[a]], &TVar[Int], a) -> <stm> ()
  = \q max-ref item -> {
      let items = tvar-read q
      let max = tvar-read max-ref
      when (len items >= max) (retry ())  -- block until space
      tvar-write (q, items ++ [item])
    }
```

`retry` causes the transaction to block until one of the TVars it read changes, then re-execute. This is declarative blocking — no condition variables or manual signaling.

#### Choice with or-else

```
-- Take from whichever queue has items
take-either : (&TVar[[a]], &TVar[[a]]) -> <stm> a
  = \q1 q2 -> or-else (
      \-> bounded-queue-take q1,
      \-> bounded-queue-take q2
    )
```

If `q1` retries (empty), try `q2`. If both retry, the combined transaction retries.

### 4.8 Restrictions Inside Transactions

The `stm` effect does **not** compose with:

- `io` — I/O cannot be rolled back on retry
- `async` — spawning/awaiting inside a transaction is nonsensical
- `shared` (Ref operations) — mixing STM and non-transactional atomics breaks composability

The type system enforces this: `atomically` requires `() -> <stm | e> a` where `e` must not contain `io`, `async`, or `shared`. Pure computation and `exn` are allowed.

```
-- OK: pure + stm
atomically \-> { tvar-write (&v, tvar-read &v + 1) }

-- OK: stm + exn (exceptions abort the transaction)
atomically \-> { when (tvar-read &v < 0) (raise NegativeBalance) }

-- ERROR: stm + io
atomically \-> { print "inside tx"; tvar-write (&v, 1) }
-- TYPE ERROR: <stm, io> is not a valid transaction body
```

---

## 5. Safety Guarantees

### 5.1 No Data Races

All shared state access goes through atomic primitives (`Ref` operations or STM transactions). There is no raw shared memory. The effect system ensures that shared access is always visible in the type signature — a function without `<shared>` or `<stm>` in its effect row cannot access shared state.

### 5.2 No Deadlocks (STM)

STM uses optimistic concurrency — no locks, no lock ordering. Conflicts are resolved by automatic retry. `Ref` operations are individually atomic and do not hold locks across operations, so they cannot deadlock either.

**Caveat:** `Ref`-based code can *livelock* if multiple tasks repeatedly CAS-conflict. This is inherent to lock-free programming and is the programmer's responsibility.

### 5.3 Affine Safety

- Affine values inside `Ref[T]` or `TVar[T]` use take/put, preserving single ownership
- `Ref` and `TVar` handles are themselves affine, preventing resource leaks
- Cloned handles share the cell but each must be individually closed

### 5.4 Effect Safety

- `shared` effect is tracked in the type system; pure functions cannot access shared state
- `stm` effect prevents I/O and async inside transactions
- The `ref-modify` function requires a pure modifier, preventing effect re-execution

### 5.5 Structured Concurrency Compatibility

Refs and TVars are heap objects managed by GC. They are scoped by affine handle lifetime, which is tied to task group scope via structured concurrency. A ref whose handles are all closed becomes unreachable and is collected.

---

## 6. Interaction with Existing Systems

### 6.1 Effect System

The `shared` and `stm` effects integrate as new built-in effects alongside `io`, `exn`, `state`, and `async`:

```
effect shared    -- atomic ref operations (not user-handleable)
effect stm       -- transactional memory operations (handled by atomically)
```

Effect rows compose normally:

```
concurrent-update : Ref[Int] -> <async, shared> Int
stm-transfer : (TVar[Int], TVar[Int]) -> <stm, exn> ()
```

### 6.2 Affine Type System

| Operation | Unrestricted T | Affine T |
|-----------|---------------|----------|
| `ref-read` | OK (copies) | TYPE ERROR |
| `ref-write` | OK (overwrites) | TYPE ERROR |
| `ref-swap` | OK | TYPE ERROR |
| `ref-modify` | OK | TYPE ERROR |
| `ref-take` | N/A | OK (empties cell) |
| `ref-put` | N/A | OK (fills cell) |
| `ref-cas` | OK (with Eq) | OK (swaps ownership) |

### 6.3 GC Integration

`Ref`, `TVar`, and `AtomicVal` are heap objects. New heap object kinds:

```
0x0C  REF       — ref_count (handle count), value slot, lock word
0x0D  TVAR      — version counter, value slot
0x0E  ATOMIC    — atomic value (Int or Bool, unboxed)
```

The GC traces through these objects to keep their contents alive. When all handles to a `Ref`/`TVar` are closed (or unreachable), the object and its contents become collectible.

### 6.4 Structured Concurrency

Refs are created and shared within task groups. The typical pattern:

```
task-group \-> {
    let r = ref-new initial-value
    -- clone and distribute to child tasks
    -- await children
    -- read final value
    ref-close r
  }
```

Refs can escape a task group only if returned (ownership transfers to the parent scope). This is consistent with affine semantics — the handle is moved, not leaked.

---

## 7. Formal Typing Rules

### 7.1 Ref Operations

**ref-new:**
```
G |- v : T ! E ; G'
─────────────────────────────────
G |- ref-new v : Ref[T] ! E ∪ <shared> ; G'
```

**ref-read (unrestricted T only):**
```
G |- r : &Ref[T] ! E ; G'    unrestricted(T)
─────────────────────────────────
G |- ref-read r : T ! E ∪ <shared> ; G'
```

**ref-take (affine T only):**
```
G |- r : &Ref[T] ! E ; G'    affine(T)
─────────────────────────────────
G |- ref-take r : T ! E ∪ <shared, exn[RefEmpty]> ; G'
```

**ref-modify (unrestricted T, pure modifier):**
```
G |- r : &Ref[T] ! E ; G'    unrestricted(T)
G |- f : T -> <> T
─────────────────────────────────
G |- ref-modify (r, f) : T ! E ∪ <shared> ; G'
```

### 7.2 STM Operations

**atomically:**
```
G |- body : () -> <stm | e> a    e ∩ {io, async, shared} = ∅
─────────────────────────────────
G |- atomically body : a ! <shared | e>
```

Note: `atomically` discharges `stm` and introduces `shared`. The restriction `e ∩ {io, async, shared} = ∅` prevents non-retryable effects inside transactions.

**retry:**
```
─────────────────
G |- retry () : a ! <stm>
```

`retry` has return type `a` (any type) because it never actually returns — it aborts the current transaction.

---

## 8. Complete Examples

### 8.1 Shared Counter

```
shared-counter : Int -> <async, shared, io> Int
  = \n -> task-group \-> {
      let counter = ref-new 0
      let handles = 1 .. n |> map \_ -> clone &counter
      let futures = handles |> map \h -> spawn \-> {
          ref-modify (&h, \x -> x + 1)
          ref-close h
          ()
        }
      futures |> each await
      let result = ref-read &counter
      ref-close counter
      result
    }

-- shared-counter 100 => 100 (always, atomically)
```

### 8.2 Affine Resource Pool

```
affine type Connection

affine type Pool {
  slots: [Ref[Connection?]]
}

acquire : &Pool -> <shared, async> Connection
  = \pool -> {
      -- Spin over slots, try to take
      loop \-> {
          let conn = (&pool).slots |> find-map \slot -> {
              match (ref-swap (&slot, none)) {
                some c -> some c,
                none   -> none
              }
            }
          match conn {
            some c -> break c,
            none   -> { yield (); continue () }
          }
        }
    }

release : (&Pool, Connection) -> <shared> ()
  = \pool conn -> {
      let slot = (&pool).slots |> find \s -> match (ref-read &s) { none -> true, _ -> false }
      match slot {
        some s -> ref-write (&s, some conn),
        none   -> discard conn    -- pool full, abandon connection
      }
    }
```

### 8.3 STM Bounded Channel

```
affine type StmChan[a] {
  items: TVar[[a]],
  capacity: TVar[Int]
}

stm-chan-new : Int -> <shared> StmChan[a]
  = \cap -> {
      let items = tvar-new []
      let capacity = tvar-new cap
      {items: items, capacity: capacity}
    }

stm-send : (&StmChan[a], a) -> <stm> ()
  = \ch v -> {
      let xs = tvar-read (&ch).items
      let cap = tvar-read (&ch).capacity
      when (len xs >= cap) (retry ())
      tvar-write ((&ch).items, xs ++ [v])
    }

stm-recv : &StmChan[a] -> <stm> a
  = \ch -> {
      let xs = tvar-read (&ch).items
      match xs {
        [] -> retry (),
        [x, ..rest] -> { tvar-write ((&ch).items, rest); x }
      }
    }

-- Usage: composable with other STM operations
transfer-via-chan : (&StmChan[Int], &TVar[Int]) -> <stm> ()
  = \ch balance -> {
      let amount = stm-recv ch        -- blocks until item available
      tvar-write (balance, tvar-read balance + amount)
    }
    -- The whole thing is one atomic transaction!
```

### 8.4 Concatenative Style

```
-- Atomic increment in concatenative form
ref-inc : (Ref[Int] -- <shared> Int)
  = &                          -- Ref -- Ref &Ref
    [\x -> x + 1] ref-modify   -- Ref Int (new value)
    swap ref-close             -- Int
```

---

## 9. Summary of New Primitives

### Types

| Type | Affine? | Cloneable? | Description |
|------|---------|------------|-------------|
| `Ref[T]` | Yes | Yes | Shared mutable cell (clone = same cell) |
| `AtomicVal[T]` | No | N/A | Lock-free atomic for primitives |
| `TVar[T]` | Yes | Yes | Transactional variable (clone = same cell) |

### Effects

| Effect | Handleable? | Description |
|--------|-------------|-------------|
| `shared` | No (runtime) | Atomic ref + TVar creation + AtomicVal ops |
| `stm` | Yes (`atomically`) | Transactional reads/writes |

### Operations

| Operation | Type | Effect |
|-----------|------|--------|
| `ref-new` | `T -> Ref[T]` | `<shared>` |
| `ref-read` | `&Ref[T] -> T` | `<shared>` (unrestricted T) |
| `ref-write` | `(&Ref[T], T) -> ()` | `<shared>` (unrestricted T) |
| `ref-swap` | `(&Ref[T], T) -> T` | `<shared>` (unrestricted T) |
| `ref-modify` | `(&Ref[T], T -> <> T) -> T` | `<shared>` (unrestricted T) |
| `ref-cas` | `(&Ref[T], T, T) -> (Bool, T)` | `<shared>` (T : Eq) |
| `ref-take` | `&Ref[T] -> T` | `<shared, exn[RefEmpty]>` (affine T) |
| `ref-put` | `(&Ref[T], T) -> ()` | `<shared, exn[RefFull]>` (affine T) |
| `ref-close` | `Ref[T] -> ()` | `<>` |
| `atomic-new` | `T -> AtomicVal[T]` | `<shared>` (T : Atomic) |
| `atomic-load` | `AtomicVal[T] -> T` | `<shared>` |
| `atomic-store` | `(AtomicVal[T], T) -> ()` | `<shared>` |
| `atomic-cas` | `(AtomicVal[T], T, T) -> (Bool, T)` | `<shared>` |
| `atomic-add` | `(AtomicVal[Int], Int) -> Int` | `<shared>` |
| `atomic-sub` | `(AtomicVal[Int], Int) -> Int` | `<shared>` |
| `tvar-new` | `T -> TVar[T]` | `<shared>` |
| `tvar-read` | `&TVar[T] -> T` | `<stm>` (unrestricted T) |
| `tvar-write` | `(&TVar[T], T) -> ()` | `<stm>` (unrestricted T) |
| `tvar-take` | `&TVar[T] -> T` | `<stm, exn[TVarEmpty]>` (affine T) |
| `tvar-put` | `(&TVar[T], T) -> ()` | `<stm, exn[TVarFull]>` (affine T) |
| `tvar-close` | `TVar[T] -> ()` | `<>` |
| `atomically` | `(() -> <stm \| e> a) -> a` | `<shared \| e>` |
| `retry` | `() -> a` | `<stm>` |
| `or-else` | `(() -> <stm> a, () -> <stm> a) -> a` | `<stm>` |

### Heap Object Kinds (3 new)

| Kind | Hex | Description |
|------|-----|-------------|
| `REF` | 0x0C | Shared reference cell |
| `TVAR` | 0x0D | Transactional variable |
| `ATOMIC` | 0x0E | Atomic primitive value |

---

## 10. Design Rationale

### Why both Ref and STM?

Ref is simple and sufficient for single-variable atomicity (counters, flags, caches). STM is needed for multi-variable consistency (bank transfers, bounded queues). Providing only STM would impose transaction overhead on simple cases; providing only Ref would force error-prone CAS loops for multi-variable updates.

### Why a separate `shared` effect (not reusing `state`)?

`state[S]` is task-local and handleable — users can intercept `get`/`set`/`mod`. Shared state is fundamentally different: it crosses task boundaries and must be atomic. Conflating them would either make `state` non-handleable (breaking existing code) or make `shared` handleable (unsafe — user handlers can't provide atomicity guarantees). Separate effects make the distinction visible in types.

### Why is `ref-modify` restricted to pure functions?

If the modifier could perform effects (IO, exceptions), those effects would execute inside the atomic section. On retry (for CAS-based implementation), the effects would re-execute. Pure modifiers are idempotent and safe to retry. For effectful updates, use the explicit read-modify-write pattern with `ref-cas`.

### Why take/put for affine T instead of just forbidding Ref[affine]?

Shared ownership of affine resources is a real need (connection pools, token caches). The take/put protocol preserves single-ownership semantics while allowing the cell to be shared. Forbidding `Ref[affine]` entirely would force all inter-task resource sharing through channels, which is cumbersome for "borrow and return" patterns.

### Why is `shared` not user-handleable?

Shared state atomicity requires hardware-level guarantees (atomic instructions, memory barriers). A user-defined handler cannot provide these. Making `shared` non-handleable (like `io`) ensures that the runtime's atomic implementation is always used. Testing support (mock handlers) can be added as a controlled extension.

### Why is AtomicVal unrestricted (not affine)?

`AtomicVal` holds only primitive unrestricted values and has no cleanup protocol. Making it unrestricted avoids the ceremony of `ref-close` for simple atomic counters. Since it holds no affine resources and requires no cleanup, there's no resource-safety reason to make it affine.

### STM and the GC

STM's optimistic concurrency creates temporary copies of values during transactions. These copies are short-lived and become garbage when the transaction commits or aborts. The mark-sweep GC handles this naturally — no special STM-aware GC logic is needed. Transaction metadata (read sets, write sets) are stack-allocated per-transaction and freed on commit/abort.
