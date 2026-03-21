# Streaming I/O and Async Iterators

**Task:** TASK-085
**Mode:** Spec
**Date:** 2026-03-19
**Builds On:** Async runtime (TASK-014), Effect system (TASK-005), Affine types (TASK-007), Stdlib catalog (TASK-008/016)
**Design Context:** Clank uses structured concurrency, algebraic effects, MPSC channels, and affine types for resource safety. GC handles memory.

---

## 1. Overview

Clank's current collection model is **eager** — `map`, `filter`, `fold` all
produce complete lists in memory. This is fine for small data, but agents
routinely process log streams, HTTP response bodies, file contents line-by-line,
and channel message sequences where materializing the entire collection is
wasteful or impossible (infinite streams).

This spec introduces:

1. **Async iterator protocol** — a minimal interface for producing values
   one-at-a-time, with support for async suspension between yields
2. **Streaming I/O primitives** — file, network, and process streams built on
   the iterator protocol
3. **Stdlib streaming combinators** — lazy `map`, `filter`, `take`, `fold`,
   etc. over iterators
4. **Integration** with effects, channels, and the existing async runtime

### Design Goals

1. **Lazy** — values produced on demand, not materialized upfront
2. **Async-native** — iterators suspend at yield points, integrating with the
   scheduler and cancellation
3. **Affine-safe** — iterator handles are affine; cleanup runs deterministically
4. **Effect-compatible** — iterator bodies can perform effects; consumers see
   those effects in their rows
5. **Composable** — combinators chain without intermediate allocation
6. **Terse** — minimal syntax overhead; reuse existing `for` / pipeline idioms

---

## 2. Async Iterator Protocol

### 2.1 Core Interface

```
interface AsyncIter[a] {
  next : &Self -> <async, exn[IterDone]> a
}
```

An `AsyncIter[a]` produces values of type `a` one at a time. Each call to
`next` either:
- Returns the next value `a`
- Raises `exn[IterDone]` when the sequence is exhausted

**Why `exn[IterDone]` instead of `Option`?** The exception-based termination
lets iterator consumers use normal control flow — a `for` loop naturally
terminates when `IterDone` is raised without requiring explicit `match` on every
step. The effect handler for the loop catches `IterDone` to break. This is
idiomatic Clank: effects for control flow.

### 2.2 The `Iter` Type

```
affine type Iter[a]
```

`Iter[a]` is the concrete async iterator handle. It is **affine** because:
- It may hold resources (file descriptors, channel receivers, network sockets)
- It must be consumed (drained or explicitly closed) to avoid resource leaks
- The compiler warns on leaked iterators

```
deriving AsyncIter for Iter[a]
```

### 2.3 Iterator Lifecycle

```
-- Advance to the next value (borrows the iterator)
next : &Iter[a] -> <async, exn[IterDone]> a

-- Close an iterator early (consumes it, runs cleanup)
close-iter : Iter[a] -> <async> ()

-- Collect all remaining values into a list (consumes iterator)
collect : Iter[a] -> <async, exn | e> [a]

-- Drain without collecting (consumes iterator, runs for side effects)
drain : Iter[a] -> <async, exn | e> ()
```

`close-iter` is important for resource cleanup — closing a file iterator closes
the underlying file handle, closing a channel iterator drops the receiver.

### 2.4 Typing Rule for `next`

```
G |- it : &Iter[a] ! E ; G          -- borrow, iterator stays live
---
G |- next &it : a ! E ∪ <async, exn[IterDone]> ; G
```

The iterator is borrowed, not consumed — it can be called repeatedly until
exhausted. After exhaustion, the iterator must still be consumed (via `collect`,
`drain`, `close-iter`, or `for` which handles this automatically).

---

## 3. Iterator Construction

### 3.1 The `yield` Effect

Iterators are created using a **`gen` effect** — a generator that yields values
into the stream:

```
effect gen[a] {
  yield : a -> ()
}
```

The `gen` effect is a user-defined effect (not built-in) but has special
compiler support for iterator construction via the `iter` keyword.

### 3.2 The `iter` Expression

```
iter_expr ::= 'iter' type_param? block

-- Creates an Iter[a] from a generator body
iter : (() -> <gen[a], async | e> ()) -> <e> Iter[a]
```

The `iter` expression captures a generator body and returns an `Iter[a]`.
The body runs lazily — it only advances when `next` is called on the iterator.

```
-- Count from 0 to n-1
count-to : Int -> <> Iter[Int]
  = fn(n) => iter {
      let i = ref 0
      loop fn() => {
        let v = get()
        when (v >= n) (raise IterDone)
        yield v
        mod(fn(x) => x + 1)
      }
    }

-- Fibonacci sequence (infinite)
fibs : () -> <> Iter[Int]
  = fn() => iter {
      run-state (0, 1) fn() => loop fn() => {
        let (a, b) = get()
        yield a
        set((b, a + b))
      }
    }
```

### 3.3 From Existing Collections

```
-- Convert a list to an iterator
iter-of : [a] -> <> Iter[a]

-- Convert a range to an iterator (lazy, does not allocate the list)
iter-range : (Int, Int) -> <> Iter[Int]
```

### 3.4 From Channels

Channels naturally produce streams. A `Receiver[a]` can be converted to an
iterator that yields values until the channel is closed:

```
-- Convert a channel receiver to an iterator
-- The iterator yields values until ChannelClosed, then raises IterDone
-- Consumes the Receiver (ownership transfers to the iterator)
iter-recv : Receiver[a] -> <> Iter[a]
```

When the iterator is closed early via `close-iter`, the underlying `Receiver`
is also closed.

### 3.5 Async Iterators from Spawn

```
-- Create an iterator backed by a spawned task that yields through a channel
iter-spawn : (Int, (Sender[a]) -> <async | e> ()) -> <async | e> Iter[a]
  = fn(buf, producer) => {
      let (tx, rx) = channel(buf)
      spawn fn() => {
        producer(tx)
        close-sender(tx)
      }
      iter-recv(rx)
    }
```

This is the bridge between push-based producers (tasks writing to channels)
and pull-based consumers (iterator protocol).

---

## 4. `for` Expression

The `for` expression is syntactic sugar for iterating over an `AsyncIter`:

### 4.1 Syntax

```
for_expr ::= 'for' pattern 'in' expr block
```

### 4.2 Desugaring

```
for x in it { body }
```

desugars to:

```
handle {
  loop fn() => {
    let x = next(&it)
    body
    continue()
  }
} {
  return(v) => v,
  raise(IterDone) resume _ => ()
}
close-iter(it)
```

The `for` expression:
1. Calls `next` repeatedly, binding each value to `x`
2. Catches `IterDone` to terminate the loop
3. Consumes the iterator via `close-iter` when done
4. Supports `break` and `continue` (from the existing loop mechanism)

### 4.3 For with Accumulator

```
for_fold_expr ::= 'for' pattern 'in' expr 'fold' expr block

-- Example: sum all values
let total = for x in nums fold 0 {
  acc + x
}
```

Desugars to a `fold` over the iterator with an accumulator.

### 4.4 Typing

```
G |- it : Iter[a] ! E1 ; G1
G1, x :_m a |- body : () ! E2 ; G2
---
G |- for x in it { body } : () ! E1 ∪ E2 ∪ <async, exn[IterDone]> ; G2 \ it
```

The iterator `it` is consumed by `for` — it does not appear in the output
context. The effect row includes `async` (from `next`) and any effects from
the body.

---

## 5. Streaming Combinators (`std.iter`)

All combinators consume the input iterator and return a new iterator. They
are lazy — no work happens until the result iterator is advanced.

### 5.1 Transformations

| Word | Signature | Description |
|------|-----------|-------------|
| `iter.map` | `(Iter[a], (a) -> <e> b) -> <e> Iter[b]` | Lazy map |
| `iter.filter` | `(Iter[a], (a) -> <e> Bool) -> <e> Iter[a]` | Lazy filter |
| `iter.flatmap` | `(Iter[a], (a) -> <e> Iter[b]) -> <e> Iter[b]` | Map then flatten |
| `iter.take` | `(Iter[a], Int) -> <> Iter[a]` | First n elements |
| `iter.drop` | `(Iter[a], Int) -> <> Iter[a]` | Skip first n elements |
| `iter.take-while` | `(Iter[a], (a) -> <e> Bool) -> <e> Iter[a]` | Take while predicate holds |
| `iter.drop-while` | `(Iter[a], (a) -> <e> Bool) -> <e> Iter[a]` | Drop while predicate holds |
| `iter.enumerate` | `(Iter[a]) -> <> Iter[(Int, a)]` | Pair with 0-based index |
| `iter.chain` | `(Iter[a], Iter[a]) -> <> Iter[a]` | Concatenate two iterators |
| `iter.zip` | `(Iter[a], Iter[b]) -> <> Iter[(a, b)]` | Zip two iterators (shortest) |
| `iter.intersperse` | `(Iter[a], a) -> <> Iter[a]` | Insert separator between elements |
| `iter.chunk` | `(Iter[a], Int{> 0}) -> <> Iter[[a]]` | Group into chunks of n |
| `iter.window` | `(Iter[a], Int{> 0}) -> <> Iter[[a]]` | Sliding window of size n |
| `iter.scan` | `(Iter[a], b, (b, a) -> <e> b) -> <e> Iter[b]` | Running fold |
| `iter.dedup` | `(Iter[a]) -> <> Iter[a]` | Remove consecutive duplicates |

### 5.2 Consumers (Terminal Operations)

These consume the iterator and produce a final value.

| Word | Signature | Description |
|------|-----------|-------------|
| `iter.collect` | `(Iter[a]) -> <async | e> [a]` | Collect into list |
| `iter.fold` | `(Iter[a], b, (b, a) -> <e> b) -> <async | e> b` | Left fold |
| `iter.count` | `(Iter[a]) -> <async | e> Int` | Count elements |
| `iter.sum` | `(Iter[Int]) -> <async | e> Int` | Sum |
| `iter.any` | `(Iter[a], (a) -> <e> Bool) -> <async | e> Bool` | Any match |
| `iter.all` | `(Iter[a], (a) -> <e> Bool) -> <async | e> Bool` | All match |
| `iter.find` | `(Iter[a], (a) -> <e> Bool) -> <async | e> Option[a]` | First match |
| `iter.first` | `(Iter[a]) -> <async | e> Option[a]` | First element |
| `iter.last` | `(Iter[a]) -> <async | e> Option[a]` | Last element |
| `iter.min` | `(Iter[a]) -> <async | e> Option[a]` | Minimum (Ord) |
| `iter.max` | `(Iter[a]) -> <async | e> Option[a]` | Maximum (Ord) |
| `iter.nth` | `(Iter[a], Int) -> <async | e> Option[a]` | Element at index |
| `iter.each` | `(Iter[a], (a) -> <e> ()) -> <async | e> ()` | Execute for side effects |
| `iter.drain` | `(Iter[a]) -> <async | e> ()` | Consume, discard values |
| `iter.join` | `(Iter[Str], Str) -> <async | e> Str` | Join strings with separator |

### 5.3 Constructors

| Word | Signature | Description |
|------|-----------|-------------|
| `iter.of` | `([a]) -> <> Iter[a]` | From list |
| `iter.range` | `(Int, Int) -> <> Iter[Int]` | Range [start, end) |
| `iter.repeat` | `(a) -> <> Iter[a]` | Infinite repetition |
| `iter.cycle` | `(Iter[a]) -> <> Iter[a]` | Infinite cycle (buffers) |
| `iter.once` | `(a) -> <> Iter[a]` | Single element |
| `iter.empty` | `() -> <> Iter[a]` | Empty iterator |
| `iter.unfold` | `(b, (b) -> <e> Option[(a, b)]) -> <e> Iter[a]` | Build from seed |
| `iter.generate` | `(() -> <e> Option[a]) -> <e> Iter[a]` | From callable |

---

## 6. Streaming I/O Primitives

### 6.1 File Streaming

The stdlib currently has `fs.read` (read entire file) and `fs.lines` (read all
lines into a list). Streaming equivalents process files lazily:

```
-- Stream lines from a file (lazy, reads on demand)
-- The returned iterator is affine; closing it closes the file handle
fs.stream-lines : (Str) -> <io, exn[Err]> Iter[Str]

-- Stream bytes in chunks from a file
fs.stream-bytes : (Str, Int{> 0}) -> <io, exn[Err]> Iter[[Byte]]

-- Stream from an open file handle (borrows handle, caller manages close)
fs.iter-lines : (&File) -> <io, exn[Err]> Iter[Str]

-- Write an iterator of strings to a file (one per line)
fs.write-iter : (Str, Iter[Str]) -> <io, async, exn[Err]> ()

-- Write an iterator of byte chunks to a file
fs.writeb-iter : (Str, Iter[[Byte]]) -> <io, async, exn[Err]> ()
```

`fs.stream-lines` opens the file internally and returns an `Iter[Str]` that
reads one line per `next` call. Closing the iterator closes the file. This is
the primary lazy file reading primitive.

### 6.2 HTTP Streaming

For large response bodies, streaming avoids buffering the entire body:

```
-- Stream response body in chunks
http.stream : (Str) -> <io, async, exn[Err]> (HttpRes, Iter[[Byte]])

-- Stream response body as lines (for SSE, NDJSON, etc.)
http.stream-lines : (Str) -> <io, async, exn[Err]> (HttpRes, Iter[Str])

-- Send request body from an iterator
http.send-stream : (HttpReq, Iter[[Byte]]) -> <io, async, exn[Err]> HttpRes
```

`http.stream` returns a tuple of the response headers and a body iterator. The
iterator is affine — it holds the connection open. Closing the iterator closes
the connection.

### 6.3 Process Streaming

For long-running processes, stream stdout/stderr:

```
-- Start process and stream stdout lines
proc.stream : (Str, [Str]) -> <io, async, exn[Err]> (!ProcHandle, Iter[Str])

-- Stream both stdout and stderr as tagged values
type ProcLine = Stdout(Str) | Stderr(Str)
proc.stream-both : (Str, [Str]) -> <io, async, exn[Err]> (!ProcHandle, Iter[ProcLine])

-- Pipe an iterator as stdin to a process
proc.pipe-iter : (Str, [Str], Iter[Str]) -> <io, async, exn[Err]> ProcResult
```

### 6.4 Standard Input Streaming

```
-- Stream lines from stdin (infinite until EOF)
io.stdin-lines : () -> <io> Iter[Str]
```

---

## 7. Interaction with Effects

### 7.1 Effectful Iterators

Iterator bodies can perform arbitrary effects. These effects appear in the
iterator's type and propagate to consumers:

```
-- An iterator that performs IO
let lines = fs.stream-lines("data.txt")
-- lines : Iter[Str], creation had <io, exn> effects

-- Consuming it inherits those effects
for line in lines {
  print(line)           -- <io> from print
}
-- Total effect: <io, async, exn>
```

### 7.2 Effect Rows in Combinators

Combinator functions are effect-polymorphic:

```
iter.map : (Iter[a], (a) -> <e> b) -> <e> Iter[b]
```

If the mapping function performs IO, the resulting iterator carries `<io>`:

```
let urls = iter.of(["url1", "url2", "url3"])
let responses = iter.map(urls, fn(u) => http.get(u))
-- responses : Iter[HttpRes], carries <io, async, exn>
```

### 7.3 Interaction with `gen` Effect and Handlers

The `gen` effect is handled by the `iter` constructor, which installs a handler
that captures the continuation at each `yield` point:

```
iter { body }

-- Equivalent to:
make-iter(fn() => handle body {
  return(_) => raise(IterDone),
  yield(v) resume k => suspend-with(v, k)
})
```

Each `next` call resumes the captured continuation `k` until the next `yield`
or return. This is the standard coroutine-via-effects pattern.

### 7.4 Interaction with `state` Effect

State within an iterator body is scoped to the iterator:

```
let it = iter {
  run-state(0, fn() => loop fn() => {
    let n = get()
    yield n
    mod(fn(x) => x + 1)
  })
}
-- Each next() advances the internal state
-- State is encapsulated; caller cannot see it
```

### 7.5 Interaction with `async` Effect

Async iterators naturally integrate with the scheduler. `next` is an async
operation — calling it may suspend the current task if the iterator needs to
wait for I/O or channel data:

```
-- Reading from a network stream suspends until data arrives
let (res, body) = http.stream("https://example.com/large")
for chunk in body {
  process(chunk)        -- each chunk may require async I/O to fetch
}
```

`next` is a **cancellation point** — if the consuming task is cancelled, the
iterator receives the cancellation signal and runs cleanup.

---

## 8. Interaction with Channels

### 8.1 Channel-to-Iterator Bridge

`iter-recv` converts a `Receiver[a]` into an `Iter[a]`:

```
let (tx, rx) = channel(10)

-- Producer (in another task)
spawn fn() => {
  for i in iter.range(0, 100) {
    send(tx, i)
  }
  close-sender(tx)
}

-- Consumer (via iterator)
let it = iter-recv(rx)
for val in it {
  print(show(val))
}
```

The iterator blocks on `next` when the channel is empty (async suspension)
and terminates when the channel is closed (raises `IterDone`).

### 8.2 Iterator-to-Channel Bridge

```
-- Send all values from an iterator through a channel
iter-send : (Iter[a], Sender[a]) -> <async, exn[ChannelClosed] | e> ()
  = fn(it, tx) => {
      for val in it {
        send(tx, val)
      }
    }
```

### 8.3 Ownership

When `iter-recv` consumes a `Receiver[a]`, the receiver's ownership transfers
to the iterator. The iterator is affine; closing it closes the receiver.
Similarly, the affine file handle inside `fs.stream-lines` is owned by the
iterator.

```
let rx = ...                    -- rx : Receiver[Msg], affine
let it = iter-recv(rx)          -- rx consumed, it owns the receiver
-- rx is DEAD here
for msg in it { process(msg) }  -- it consumed by for
```

---

## 9. Interaction with Structured Concurrency

### 9.1 Iterators in Task Groups

Iterators respect task group boundaries. If a task group is cancelled:
1. All iterators held by child tasks receive cancellation
2. Iterator cleanup runs (closing underlying resources)
3. No resource leaks from abandoned iterators

```
task-group fn() => {
  let it = fs.stream-lines("large.txt")
  let fut = spawn fn() => {
    for line in it {
      process(line)
    }
  }
  -- If task-group exits early (e.g., timeout), the spawn is cancelled
  -- Cancellation flows into the for loop, which closes the iterator
  -- The file handle is closed
  await(fut)
}
```

### 9.2 Parallel Consumption

An iterator cannot be shared between tasks (it's affine). For parallel
processing, use the channel bridge:

```
-- Fan-out: one iterator, N parallel workers
par-process : (Iter[a], Int, (a) -> <async | e> ()) -> <async | e> ()
  = fn(it, n, handler) => {
      let (tx, rx) = channel(n * 2)
      task-group fn() => {
        -- Distributor
        spawn fn() => {
          iter-send(it, tx)
          close-sender(tx)
        }
        -- Workers
        worker-pool(n, rx, handler)
      }
    }
```

---

## 10. `for` as First-Class Syntax

### 10.1 New Grammar Productions

```
for_expr      ::= 'for' pattern 'in' expr block
                |  'for' pattern 'in' expr 'fold' expr block

-- 'for' is added to the keyword list
```

### 10.2 New Keyword

`for` is a new keyword. `in` is contextual (only a keyword after `for`).

### 10.3 Pipeline Integration

`for` works naturally with pipelines:

```
fs.stream-lines("data.csv")
  |> iter.drop(1)                    -- skip header
  |> iter.map(fn(l) => split(l, ","))
  |> iter.filter(fn(r) => len(r) > 3)
  |> iter.take(100)
  |> iter.collect
```

---

## 11. VM Support

### 11.1 New Heap Object Kind

```
heap_object kinds (addition):
  0x0C  ITERATOR  -- gen_state (suspended continuation) + cleanup_fn + done_flag
```

An iterator heap object holds:
- The suspended generator continuation (a closure + captured state)
- A cleanup function (for resource release)
- A done flag (set when `IterDone` is raised)

### 11.2 New Opcodes

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `ITER_NEW` | 0xB0 | — | (Closure Cleanup -- Iterator) | Create iterator from generator + cleanup |
| `ITER_NEXT` | 0xB1 | — | (Iterator -- value) | Advance iterator, push value or raise IterDone |
| `ITER_CLOSE` | 0xB2 | — | (Iterator -- ) | Close iterator, run cleanup |

**Total: 3 opcodes** (0xB0–0xB2)

This brings the VM total from 103 to 106 opcodes.

### 11.3 `ITER_NEXT` Semantics

`ITER_NEXT` borrows the iterator (does not consume it from the stack in the
logical model, though the VM may move it temporarily):

1. Check if iterator is done → raise `IterDone`
2. Resume the generator continuation
3. When `yield` is hit, suspend the continuation and push the yielded value
4. When the generator body returns, set done flag and raise `IterDone`
5. This is a **cancellation point** — checks cancellation before resuming

### 11.4 `for` Compilation

```
for x in expr { body }
```

compiles to:

```
<eval expr>                -- push Iterator
ITER_FOR_SETUP:            -- (implementation: push handler frame for IterDone)
  ITER_NEXT                -- push next value (or jump to done)
  LOCAL_SET x_idx          -- bind to x
  <body>                   -- execute body
  JMP ITER_FOR_SETUP       -- loop
ITER_FOR_DONE:
  ITER_CLOSE               -- cleanup
```

The `IterDone` exception handler is compiled inline — when `ITER_NEXT` raises
`IterDone`, control jumps to `ITER_FOR_DONE`.

---

## 12. New Trap Codes

| Code | Name | Trigger |
|------|------|---------|
| E016 | ITER_DONE | Iterator exhausted (normal termination signal) |
| E017 | ITER_CLOSED | Operation on already-closed iterator |

---

## 13. Complete Examples

### 13.1 Lazy File Processing Pipeline

```
-- Count non-empty lines in a large file without loading it all
count-nonempty : Str -> <io, async, exn> Int
  = fn(path) =>
    fs.stream-lines(path)
      |> iter.filter(fn(l) => len(trim(l)) > 0)
      |> iter.count
```

### 13.2 Streaming HTTP Response

```
-- Process Server-Sent Events line by line
process-sse : Str -> <io, async, exn> ()
  = fn(url) => {
      let (res, lines) = http.stream-lines(url)
      for line in lines {
        when (str.pfx(line, "data:")) {
          let payload = str.slc(line, 5, str.len(line))
          let event = json.dec(payload)
          handle-event(event)
        }
      }
    }
```

### 13.3 Producer-Consumer with Iterators

```
-- Transform a file, line by line, writing results to another file
transform-file : (Str, Str, (Str) -> <e> Str) -> <io, async, exn | e> ()
  = fn(src, dst, transform) =>
    fs.stream-lines(src)
      |> iter.map(transform)
      |> fs.write-iter(dst)
```

### 13.4 Infinite Sequence with Take

```
-- First 10 Fibonacci numbers
first-fibs : () -> <async> [Int]
  = fn() => fibs() |> iter.take(10) |> iter.collect
```

### 13.5 Channel-Backed Iterator with Workers

```
-- Parallel URL fetching, results as a stream
fetch-stream : [Str] -> <async, io, exn> Iter[Result[HttpRes, Err]]
  = fn(urls) =>
    iter-spawn(10, fn(tx) => {
      task-group fn() => {
        each(urls, fn(u) => spawn fn() => {
          let result = catch fn() => http.get(u)
          send(tx, result)
        })
      }
    })
```

### 13.6 Stdin Processing (Agent-Typical Pattern)

```
-- Read JSON objects from stdin, one per line (NDJSON)
process-ndjson : () -> <io, async, exn> ()
  = fn() => {
      for line in io.stdin-lines() {
        let obj = json.dec(line)
        let result = process(obj)
        print(json.enc(result))
      }
    }
```

### 13.7 For-Fold Accumulator

```
-- Compute running statistics over a stream
stream-stats : Iter[Int] -> <async> {sum: Int, count: Int, max: Int}
  = fn(it) =>
    for x in it fold {sum: 0, count: 0, max: 0} {
      {sum: acc.sum + x, count: acc.count + 1, max: math.max(acc.max, x)}
    }
```

---

## 14. Summary of New Primitives

### Types

| Type | Affine? | Cloneable? | Description |
|------|---------|------------|-------------|
| `Iter[a]` | Yes | No | Async iterator handle |
| `IterDone` | No | N/A | Exception type for iterator exhaustion |
| `ProcLine` | No | N/A | Tagged stdout/stderr line |

### Interfaces

| Interface | Operations | Description |
|-----------|-----------|-------------|
| `AsyncIter[a]` | `next` | Async iteration protocol |

### Effects

| Effect | Operations | Description |
|--------|-----------|-------------|
| `gen[a]` | `yield` | Generator effect for iterator construction |

### Core Operations

| Operation | Type | Description |
|-----------|------|-------------|
| `next` | `&Iter[a] -> <async, exn[IterDone]> a` | Advance iterator |
| `close-iter` | `Iter[a] -> <async> ()` | Close and cleanup |
| `collect` | `Iter[a] -> <async \| e> [a]` | Collect to list |
| `drain` | `Iter[a] -> <async \| e> ()` | Consume, discard |
| `iter-of` | `[a] -> <> Iter[a]` | List to iterator |
| `iter-range` | `(Int, Int) -> <> Iter[Int]` | Lazy range |
| `iter-recv` | `Receiver[a] -> <> Iter[a]` | Channel to iterator |
| `iter-send` | `(Iter[a], Sender[a]) -> <async, exn[ChannelClosed] \| e> ()` | Iterator to channel |
| `iter-spawn` | `(Int, (Sender[a]) -> <async \| e> ()) -> <async \| e> Iter[a]` | Spawned producer |

### Streaming I/O

| Operation | Type | Description |
|-----------|------|-------------|
| `fs.stream-lines` | `(Str) -> <io, exn> Iter[Str]` | Lazy file lines |
| `fs.stream-bytes` | `(Str, Int{> 0}) -> <io, exn> Iter[[Byte]]` | Lazy file bytes |
| `fs.iter-lines` | `(&File) -> <io, exn> Iter[Str]` | Lines from handle |
| `fs.write-iter` | `(Str, Iter[Str]) -> <io, async, exn> ()` | Write from iterator |
| `fs.writeb-iter` | `(Str, Iter[[Byte]]) -> <io, async, exn> ()` | Write bytes from iterator |
| `http.stream` | `(Str) -> <io, async, exn> (HttpRes, Iter[[Byte]])` | Stream HTTP body |
| `http.stream-lines` | `(Str) -> <io, async, exn> (HttpRes, Iter[Str])` | Stream HTTP lines |
| `http.send-stream` | `(HttpReq, Iter[[Byte]]) -> <io, async, exn> HttpRes` | Stream request body |
| `proc.stream` | `(Str, [Str]) -> <io, async, exn> (!ProcHandle, Iter[Str])` | Stream process stdout |
| `proc.stream-both` | `(Str, [Str]) -> <io, async, exn> (!ProcHandle, Iter[ProcLine])` | Stream stdout+stderr |
| `proc.pipe-iter` | `(Str, [Str], Iter[Str]) -> <io, async, exn> ProcResult` | Pipe iterator to stdin |
| `io.stdin-lines` | `() -> <io> Iter[Str]` | Stream stdin |

### Combinators (15 transformations + 15 consumers + 8 constructors = 38 words)

See Section 5 for the full table.

### VM Opcodes (3 new)

| Opcode | Hex | Description |
|--------|-----|-------------|
| `ITER_NEW` | 0xB0 | Create iterator |
| `ITER_NEXT` | 0xB1 | Advance iterator |
| `ITER_CLOSE` | 0xB2 | Close iterator |

### Keywords (1 new + 1 contextual)

| Keyword | Context | Description |
|---------|---------|-------------|
| `for` | Expression | Iterate over async iterator |
| `in` | After `for` | Contextual keyword in for expression |

---

## 15. Design Rationale

### Why exception-based termination (`IterDone`) instead of `Option`?

Returning `Option[a]` from `next` forces every consumer to `match` on every
step. Exception-based termination lets the `for` loop handle termination via
effect handlers — the normal path is just `let x = next(&it)`, and the loop
construct installs the `IterDone` handler. This is idiomatic Clank: use the
effect system for control flow. It's also more terse — no `match Some(x) => ...`
wrapping on every iteration.

### Why affine iterators?

Iterators often hold resources (file handles, network connections, channel
receivers). Making `Iter[a]` affine ensures the compiler enforces cleanup.
Without affinity, a forgotten iterator could leak a file descriptor or keep a
connection open indefinitely. The `for` expression automatically consumes the
iterator, so the common case requires no explicit cleanup.

### Why `gen` as a user-defined effect (not built-in)?

The generator pattern (yield values via an effect, handle with continuation
capture) is a natural consequence of Clank's effect system. Making `gen` a
user-defined effect keeps the built-in effect count at 4 (`io`, `exn`, `state`,
`async`) and demonstrates that the effect system is powerful enough for
non-trivial control flow patterns.

### Why dedicated VM opcodes (not pure effect dispatch)?

Like async operations, iterator operations are hot-path — every element goes
through `ITER_NEXT`. Dedicated opcodes skip handler stack search and directly
invoke the generator machinery. The `gen` effect handler is conceptually
present, but `ITER_NEXT` is the runtime's optimized implementation.

### Why `for` syntax instead of just combinators?

Combinators (`iter.map`, `iter.filter`) are great for pure transformations.
But effectful iteration with mutable state, early exit, and complex branching
is clearer with `for`. Having both covers the spectrum: pipelines for simple
transforms, `for` for complex logic. This matches the existing Clank philosophy
(pipeline sugar for simple cases, named locals for complex cases).

### Why 3 opcodes instead of more?

`ITER_NEW`, `ITER_NEXT`, `ITER_CLOSE` are the minimal set. `for` compiles to
a loop using `ITER_NEXT` + jump + `ITER_CLOSE`. Combinators like `iter.map`
are implemented as library functions using `iter` + `next` — they don't need
opcodes. This keeps the opcode budget tight (106 total).

### Why not MPMC for parallel consumption?

Iterators are single-consumer (affine, non-cloneable). For parallel processing,
the spec provides `iter-send` to push values through a channel to a worker
pool. This reuses the existing MPSC channel infrastructure and keeps iterator
semantics simple. MPMC iteration would require complex fairness guarantees
and ownership transfer rules.
