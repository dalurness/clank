# Clank Async Runtime & Concurrency Primitives

**Task:** TASK-014
**Mode:** Spec
**Date:** 2026-03-14
**Builds On:** Effect system (TASK-005), Affine types (TASK-007), VM instruction set (TASK-009)
**Design Context:** Clank uses algebraic effects for async (`async` built-in effect) and affine types for resource safety. GC handles memory (DECISION-001).

---

## 1. Overview

Clank's concurrency model is **structured concurrency** built on the algebraic
effect system. All concurrent work is scoped to a *task group* — a parent task
cannot complete until all child tasks have finished or been cancelled. This
eliminates fire-and-forget goroutine leaks and dangling futures.

The `async` effect (already defined in the effect system spec) provides the
surface operations: `spawn`, `await`, `sleep`, `yield`. This spec defines:

1. The **task group** abstraction and its scoping rules
2. **Channels** for inter-task communication
3. **Select** for waiting on multiple sources
4. **Ownership transfer** of affine values across task boundaries
5. **Cancellation** semantics and cleanup
6. **VM opcodes** for async primitives
7. **Interaction** with effect handlers and the handler stack

### Design Goals

1. **Structured** — no orphaned tasks; every task has a parent scope
2. **Affine-safe** — ownership of affine values transfers cleanly across tasks
3. **Effect-compatible** — async operations compose with the effect system
4. **Minimal** — small opcode footprint; complex scheduling is a runtime concern
5. **Deterministic cleanup** — cancellation runs cleanup paths predictably

---

## 2. Task Model

### 2.1 Tasks

A **task** is a lightweight, cooperatively-scheduled unit of concurrent work.
Each task has:

- A unique `TaskId` (runtime-assigned, opaque)
- A parent task (except the root task)
- A local data stack and return stack
- A handler stack (inherits parent's handlers at spawn time)
- A status: `running | suspended | completed | cancelled | failed`

Tasks are not OS threads. The runtime multiplexes tasks onto one or more OS
threads (implementation detail, out of scope for this spec).

### 2.2 Task Groups

A **task group** is a scoped container for child tasks. It enforces structured
concurrency: the group blocks until all children complete or are cancelled.

```
task-group : (() -> <async | e> a) -> <async | e> a
```

Within a task group's body, `spawn` creates child tasks bound to that group.
When the body completes (or raises), the group:

1. **Cancels** all still-running children (sends cancellation signal)
2. **Awaits** all children (waits for them to actually finish)
3. **Propagates** the first child failure (if any) as an exception

```
-- Structured concurrency: both fetches are scoped
fetch-both : (Str, Str) -> <async, io, exn> (Response, Response)
  = \u1 u2 -> task-group \-> {
      let f1 = spawn \-> http-get u1
      let f2 = spawn \-> http-get u2
      (await f1, await f2)
    }
```

If `http-get u1` fails, the task group cancels the task running `http-get u2`
before propagating the exception.

### 2.3 The Root Task

The program entry point (`main`) runs in the **root task**. The runtime
installs the top-level `async` handler before invoking `main`. All spawned
tasks descend from the root.

### 2.4 Future Type

`spawn` returns a `Future[a]` — an affine handle to a pending result.

```
affine type Future[a]
```

`Future` is affine because:
- It represents a one-shot result that must be consumed (awaited or discarded)
- Ignoring a future silently drops potential errors
- The compiler warns on leaked futures

```
await   : Future[a] -> <async> a          -- consumes the future
discard : Future[a] -> <async> ()         -- cancels and discards
```

`await` blocks the current task until the future resolves, then returns the
result (or re-raises if the child failed). Consuming the future transfers
the result's ownership to the awaiting task.

---

## 3. Ownership Transfer Across Task Boundaries

### 3.1 Capture by Move

When `spawn` captures affine values from the parent scope, ownership
**moves** into the child task. The parent binding becomes dead:

```
let f = open "data.txt"                    -- f is affine (File)
let fut = spawn \-> {
    let contents = read-all &f
    close f                                -- child owns f, consumes it
    contents
  }
-- f is DEAD here — moved into the spawned task
let result = await fut                     -- result is Str (unrestricted)
```

This is the same capture-by-move rule as quotations (affine-types spec
Section 7.1), extended to spawned closures.

### 3.2 Typing Rule for Spawn

```
G |- body : () -> <async | e> a ; G'
affine_captured = {x | x in G, x not in G', affine(type_of(x))}
---
G |- spawn body : Future[a] ! <async | e> ; G'
```

Every affine binding captured by the spawn body is removed from the output
context G'. The parent cannot use them after spawning.

### 3.3 Returning Affine Values from Tasks

A child task can produce an affine result. Ownership transfers to whoever
awaits the future:

```
spawn-file : Str -> <async, io, exn> Future[File]
  = \path -> spawn \-> open path

-- The File transfers from child to parent via await
let f = await (spawn-file "data.txt")      -- f is affine (File)
close f                                     -- parent must consume f
```

### 3.4 Sharing via Clone

To use an affine value in both parent and child, clone before spawning:

```
let f = open "data.txt"
let f2 = clone &f          -- ERROR if File is not Clone
let fut = spawn \-> {
    let s = read-all &f2
    close f2
    s
  }
-- f still live in parent
close f
await fut
```

For non-cloneable affine values, the programmer must choose: either the
parent or the child owns it. This is enforced at compile time.

### 3.5 Channels and Ownership

Sending an affine value through a channel transfers ownership from sender to
receiver (see Section 4). The send operation consumes the value; the receive
operation produces it in the receiver's scope.

---

## 4. Channels

Channels are the primary inter-task communication primitive. A channel is a
typed, bounded, multi-producer single-consumer (MPSC) queue.

### 4.1 Channel Types

```
affine type Sender[a]
affine type Receiver[a]
```

Both ends are affine:
- `Sender` — can be cloned (multi-producer), sending consumes the value
- `Receiver` — cannot be cloned (single-consumer), must be consumed (closed)

```
deriving Clone for Sender[a]     -- multi-producer: senders can be cloned
-- Receiver has no Clone          -- single-consumer: one reader
```

### 4.2 Channel Operations

```
-- Create a bounded channel pair
channel : Int -> <async> (Sender[a], Receiver[a])

-- Send a value (blocks if buffer full)
send : (Sender[a], a) -> <async, exn[ChannelClosed]> ()

-- Receive a value (blocks if buffer empty)
recv : Receiver[a] -> <async, exn[ChannelClosed]> a

-- Try to receive without blocking
try-recv : Receiver[a] -> <async> a?

-- Close the sender (signals no more values)
close-sender : Sender[a] -> <> ()

-- Close the receiver (drops remaining values)
close-receiver : Receiver[a] -> <> ()
```

### 4.3 Ownership Through Channels

When an affine value is sent, ownership transfers:

```
let (tx, rx) = channel 1

-- In sender task:
let f = open "data.txt"
send (tx, f)                -- f consumed; ownership moves into channel
-- f is DEAD

-- In receiver task:
let f = recv rx             -- f now owned by receiver
close f                     -- receiver must consume
```

The typing rule for `send`:

```
G |- v : a ! E1 ; G1       -- v is consumed (if affine, removed from G1)
G1 |- tx : Sender[a] ! E2 ; G2   -- tx borrowed (still live, it's Clone)
---
G |- send (tx, v) : () ! E1 ∪ E2 ∪ <async, exn[ChannelClosed]> ; G2
```

### 4.4 Unbounded Channels

For convenience, an unbounded variant:

```
unbounded-channel : () -> <async> (Sender[a], Receiver[a])
```

This is sugar for `channel max-int`. Backpressure is the programmer's
responsibility.

### 4.5 One-Shot Channels

For single-value communication (common in request/response patterns):

```
oneshot : () -> <async> (OneshotSender[a], OneshotReceiver[a])

affine type OneshotSender[a]    -- send exactly once, then consumed
affine type OneshotReceiver[a]  -- receive exactly once, then consumed
```

A `Future[a]` is semantically equivalent to a `OneshotReceiver[a]` — the
runtime may implement futures using oneshot channels internally.

---

## 5. Select

`select` waits on multiple async sources and processes the first one that
becomes ready.

### 5.1 Select Syntax

```
select {
  recv rx1 -> \msg -> handle-msg msg,
  recv rx2 -> \msg -> handle-other msg,
  await fut -> \val -> process val,
  timeout 5s -> \-> handle-timeout ()
}
```

Each arm is a *source* (channel receive, future await, or timeout) paired with
a handler quotation. `select` blocks until at least one source is ready, then
executes the corresponding handler. Only one arm fires per invocation.

### 5.2 Select Typing

```
select : SelectSet[a] -> <async | e> a
```

Where `SelectSet[a]` is constructed from select arms that all return the same
type `a`. Each arm's handler inherits the ambient effect row `e`.

### 5.3 Select as an Effect Operation

Under the hood, `select` is desugared into the `async` effect:

```
effect async {
  spawn  : (() -> <async | e> a) -> Future[a],
  await  : Future[a] -> a,
  sleep  : Duration -> (),
  yield  : () -> (),
  select : SelectSet[a] -> a              -- NEW: added by this spec
}
```

The runtime's async handler implements `select` by registering interest in
all sources, suspending the task, and resuming when one fires.

### 5.4 Select with Channels and Timeouts

```
-- Event loop pattern
event-loop : Receiver[Msg] -> <async, io> ()
  = \rx -> loop \-> {
      select {
        recv rx -> \msg -> {
            process-msg msg
            continue ()
          },
        timeout 30s -> \-> {
            print "idle timeout"
            break ()
          }
      }
    }
```

### 5.5 Fairness

When multiple sources are ready simultaneously, `select` chooses
pseudo-randomly (not deterministically first-listed). This prevents
starvation of later arms.

---

## 6. Cancellation

### 6.1 Cancellation Model

Every task has a **cancellation token** — a boolean flag that cooperative
code checks. Cancellation is *cooperative*: the runtime sets the flag, but
the task must reach a yield point to observe it.

Yield points (where cancellation is checked):
- `await`
- `yield`
- `sleep`
- `recv` / `send` (when blocking)
- `select`

### 6.2 Cancellation Propagation

When a task group's body completes or fails:

1. All still-running children receive the cancellation signal
2. Children observe cancellation at their next yield point
3. When a child observes cancellation, it raises `exn[Cancelled]`
4. The `Cancelled` exception propagates through the child's handler stack
5. Affine cleanup code in handlers runs normally
6. The task group waits for all children to finish

This is structured cancellation — it flows down the tree, not up.

### 6.3 Cancellation and Affine Cleanup

When a task is cancelled, affine values in scope must still be consumed.
The `Cancelled` exception unwinds the stack, and effect handlers provide
cleanup:

```
process-file : Str -> <async, io, exn> Str
  = \path -> {
      let f = open path
      handle {
        let contents = read-all &f
        close f
        contents
      } {
        return x -> x,
        raise e resume _ -> {
            close f                    -- cleanup: close file on any error
            raise e                    -- re-raise (including Cancelled)
          }
      }
    }
```

When this task is cancelled:
1. `Cancelled` is raised at the next yield point (e.g., inside `read-all`)
2. The `exn` handler catches it, closes `f`, re-raises
3. The file resource is properly cleaned up

### 6.4 Explicit Cancellation Check

```
is-cancelled : () -> <async> Bool        -- check cancellation flag
check-cancel : () -> <async, exn[Cancelled]> ()  -- raise if cancelled
```

For long-running loops that don't naturally hit yield points:

```
heavy-compute : [Int] -> <async, exn[Cancelled]> Int
  = \items -> fold (\acc item -> {
      check-cancel ()              -- cooperative cancellation point
      acc + expensive-fn item
    }) 0 items
```

### 6.5 Uncancellable Sections

Critical cleanup code can temporarily shield against cancellation:

```
shield : (() -> <async | e> a) -> <async | e> a
```

Within a `shield` block, cancellation is deferred — the flag is set but
not observed until the shield exits. This is for short critical sections
(e.g., flushing a buffer, committing a transaction).

```
safe-write : (File, Str) -> <async, io> ()
  = \f data -> shield \-> {
      write-all &f data
      flush f                      -- cannot be interrupted mid-flush
    }
```

---

## 7. Structured Concurrency Patterns

### 7.1 Parallel Map

```
par-map : ((a -> <async | e> b), [a]) -> <async | e> [b]
  = \f items -> task-group \-> {
      let futures = map (\x -> spawn \-> f x) items
      map await futures
    }
```

If any `f x` fails, the task group cancels remaining tasks and propagates
the error.

### 7.2 Race (First to Complete)

```
race : [() -> <async | e> a] -> <async | e> a
  = \tasks -> task-group \-> {
      let (tx, rx) = channel 1
      each tasks \t -> spawn \-> {
          let result = t ()
          try \-> send (tx, result)    -- may fail if channel closed
          ()
        }
      let winner = recv rx
      winner                           -- task-group cancels losers on exit
    }
```

### 7.3 Worker Pool

```
worker-pool : (Int, Receiver[a], (a -> <async | e> ())) -> <async | e> ()
  = \n rx handler -> task-group \-> {
      1 .. n |> each \_ -> spawn \-> {
          loop \-> match (try-recv rx) {
            some msg -> { handler msg; continue () },
            none     -> break ()
          }
        }
    }
```

### 7.4 Timeout Wrapper

```
with-timeout : (Duration, () -> <async | e> a) -> <async, exn[Timeout] | e> a
  = \dur body -> task-group \-> {
      let f = spawn body
      select {
        await f -> \result -> result,
        timeout dur -> \-> raise (Timeout dur)
      }
    }
```

---

## 8. VM Opcodes for Async

New opcodes in the `0xA0–0xAF` range:

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `TASK_SPAWN` | 0xA0 | — | (Closure -- Future) | Spawn task from closure |
| `TASK_AWAIT` | 0xA1 | — | (Future -- value) | Block until future resolves |
| `TASK_YIELD` | 0xA2 | — | ( -- ) | Cooperative yield to scheduler |
| `TASK_SLEEP` | 0xA3 | — | (Int -- ) | Sleep for N milliseconds |
| `TASK_GROUP_ENTER` | 0xA4 | — | ( -- ) | Enter a task group scope |
| `TASK_GROUP_EXIT` | 0xA5 | — | ( -- ) | Exit task group (cancel+await children) |
| `CHAN_NEW` | 0xA6 | — | (Int -- Sender Receiver) | Create bounded channel |
| `CHAN_SEND` | 0xA7 | — | (Sender value -- ) | Send value (blocks if full) |
| `CHAN_RECV` | 0xA8 | — | (Receiver -- value) | Receive value (blocks if empty) |
| `CHAN_TRY_RECV` | 0xA9 | — | (Receiver -- Option) | Non-blocking receive |
| `CHAN_CLOSE` | 0xAA | — | (Sender\|Receiver -- ) | Close channel end |
| `SELECT_BUILD` | 0xAB | u8:arm_count | (arms.. -- SelectSet) | Build select set from arms |
| `SELECT_WAIT` | 0xAC | — | (SelectSet -- value) | Wait on select set |
| `TASK_CANCEL_CHECK` | 0xAD | — | ( -- Bool) | Check cancellation flag |
| `TASK_SHIELD_ENTER` | 0xAE | — | ( -- ) | Enter uncancellable section |
| `TASK_SHIELD_EXIT` | 0xAF | — | ( -- ) | Exit uncancellable section |

**Total: 16 opcodes** (0xA0–0xAF)

This brings the VM total from 87 to 103 opcodes.

### 8.1 Task Spawn Semantics

`TASK_SPAWN` pops a closure from the stack. The closure's captured values
are **moved** into the new task (the originating locals are marked consumed
via affine tracking). The runtime:

1. Allocates a new task with its own data stack, return stack, and handler stack
2. Copies the current handler stack into the child (handlers are inherited)
3. Pushes the closure's captured values onto the child's stack
4. Sets the child's IP to the closure's bytecode offset
5. Registers the child with the current task group
6. Pushes a `Future` heap object onto the parent's stack
7. Enqueues the child task in the scheduler's run queue

### 8.2 Task Await Semantics

`TASK_AWAIT` pops a `Future` from the stack. If the future is:

- **Resolved (success):** pushes the result value onto the stack
- **Resolved (failure):** performs `EFFECT_PERFORM exn_id` with the error
- **Pending:** suspends the current task; the scheduler resumes it when the
  future resolves

`TASK_AWAIT` is a cancellation point — before suspending, it checks the
cancellation flag.

### 8.3 Task Group Semantics

`TASK_GROUP_ENTER` pushes a group frame onto the return stack. All subsequent
`TASK_SPAWN` calls register children with this group.

`TASK_GROUP_EXIT`:

1. Sends cancellation to all still-running children in the group
2. Suspends the parent until all children complete
3. If any child failed, pops the group frame and performs
   `EFFECT_PERFORM exn_id` with the first child's error
4. Otherwise, pops the group frame and continues

### 8.4 Channel Semantics

`CHAN_NEW` pops a buffer size (Int), allocates a channel with that capacity,
and pushes `(Sender, Receiver)` — both are heap objects (kind `0x09` and
`0x0A` respectively, new heap object kinds).

```
heap_object kinds (additions):
  0x09  SENDER    — channel_id + open flag
  0x0A  RECEIVER  — channel_id + open flag
  0x0B  SELECT    — arm count + array of (source_type, source_ref, handler_ref)
```

`CHAN_SEND` and `CHAN_RECV` are cancellation points. They:
1. Check cancellation flag
2. Attempt the operation
3. If buffer is full/empty, suspend the task
4. The scheduler resumes when space/data becomes available

When a sender is closed, pending `recv` calls raise `exn[ChannelClosed]`.
When a receiver is closed, pending `send` calls raise `exn[ChannelClosed]`.

### 8.5 Select Compilation

`select { recv rx1 -> h1, await f -> h2, timeout 5s -> h3 }` compiles to:

```
-- Push select arms (source, handler pairs)
LOCAL_GET rx1_idx          -- push receiver
QUOTE h1_offset            -- push handler quotation
LOCAL_GET f_idx            -- push future
QUOTE h2_offset            -- push handler quotation
PUSH_INT 5000              -- timeout in ms
QUOTE h3_offset            -- push handler quotation
SELECT_BUILD 3             -- build SelectSet from 3 arms
SELECT_WAIT                -- block until one fires, call handler, push result
```

`SELECT_WAIT`:
1. Registers interest in all sources
2. Suspends the task
3. When a source fires, the scheduler resumes the task
4. Pushes the source's value, calls the corresponding handler quotation
5. Pushes the handler's return value

---

## 9. Interaction with Effect Handlers

### 9.1 Handler Inheritance

When a task is spawned, it inherits a **snapshot** of the parent's handler
stack. This means:

- The child sees all handlers installed at spawn time
- Handlers installed after spawn are not visible to the child
- The child can install its own handlers without affecting the parent

This is copy-on-spawn semantics for the handler stack.

### 9.2 Effects and Task Boundaries

The `spawn` function's effect row propagates:

```
spawn : (() -> <async | e> a) -> <async | e> Future[a]
```

The `| e` means the spawned body can use any effects that the parent has
handlers for. Since handlers are inherited, this is safe — the child will
find the handlers it needs.

**Exception:** if a parent installs a handler *after* spawning but *before*
the child runs, the child won't see it. The type system prevents this by
tracking the effect row at the spawn site.

### 9.3 State Effect Across Tasks

The `state` effect is **task-local** by default. Each task's handler stack
has its own state handler. To share state between tasks, use channels or
an explicit shared-state mechanism:

```
-- WRONG: state is task-local; child sees its own copy
let (_, count) = run-state 0 \-> {
    spawn \-> mod (\n -> n + 1)    -- modifies child's copy, not parent's
    sleep 1s
    get ()                          -- still 0
  }

-- RIGHT: share via channel
let (tx, rx) = channel 10
spawn \-> send (tx, 42)
let val = recv rx                   -- 42
```

### 9.4 Custom Effect Handlers in Spawned Tasks

A spawned task can handle effects locally:

```
spawn \-> handle (my-computation ()) {
    return x -> x,
    my-op v resume k -> k (transform v)
  }
```

The handler is scoped to the child task. It does not leak to the parent.

### 9.5 Continuations and Concurrency

Effect handlers capture continuations (via `RESUME`). In a concurrent context,
a continuation may cross a suspension point:

```
handle (do-async-work ()) {
    return x -> x,
    my-op v resume k -> {
        let result = await (spawn \-> expensive v)
        k result    -- resume with async result
      }
  }
```

The continuation `k` is valid across `await` — it's stored as a heap object
and survives task suspension/resumption. The runtime must ensure that
continuation captures are GC-safe.

---

## 10. Trap Codes (New)

| Code | Name | Trigger |
|------|------|---------|
| E011 | TASK_CANCELLED | Cancellation observed at yield point |
| E012 | CHANNEL_CLOSED | Send/recv on closed channel |
| E013 | FUTURE_CONSUMED | Await on already-consumed future |
| E014 | TASK_GROUP_CHILD_FAILED | Child task failed, propagated to parent |
| E015 | SELECT_EMPTY | Select with zero arms |

These map to structured JSON diagnostics following the same format as
existing trap codes (vm-instruction-set spec Section 5).

---

## 11. Complete Examples

### 11.1 Parallel HTTP Fetch with Timeout

```
fetch-urls : [Str] -> <async, io> [Result[Response, Err]]
  = \urls -> task-group \-> {
      let futures = map (\u -> spawn \-> {
          with-timeout 10s \-> http-get u
        }) urls
      map (\f -> catch \-> await f) futures
    }
```

### 11.2 Producer-Consumer Pipeline

```
pipeline : Str -> <async, io, exn> ()
  = \dir -> {
      let (tx, rx) = channel 100

      task-group \-> {
          -- Producer: list files and send paths
          spawn \-> {
              let files = list-dir dir
              each files \f -> send (tx, f)
              close-sender tx
            }

          -- Consumer pool: process files
          spawn \-> {
              loop \-> match (try-recv rx) {
                some path -> {
                    let f = open path
                    let data = read-all &f
                    close f
                    print ("processed: " ++ path)
                    continue ()
                  },
                none -> break ()
              }
            }
        }
    }
```

### 11.3 Chat Server (Channels + Select)

```
affine type Client {
  id: Int,
  inbox: Sender[Str]
}
deriving Clone for Client

chat-room : Receiver[Msg] -> <async, io> ()
  = \rx -> {
      let clients = ref []      -- using state effect internally

      run-state [] \-> loop \-> {
          select {
            recv rx -> \msg -> match msg {
              Join(client) -> mod (\cs -> cs ++ [client]),
              Leave(id)    -> mod (\cs -> filter (\c -> c.id /= id) cs),
              Say(id, text) -> {
                  let cs = get ()
                  each cs \c ->
                    when (c.id /= id)
                      (try \-> send (c.inbox, text); ())
                }
            },
            timeout 60s -> \-> {
                print "room idle"
                continue ()
              }
          }
        }
    }
```

### 11.4 Affine Ownership Transfer — Database Connection

```
affine type DbConn

query : &DbConn -> Str -> <io, exn> [Row]
close-db : DbConn -> <io> ()

-- Connection moves to child, result comes back via future
remote-query : DbConn -> Str -> <async, io, exn> (DbConn, [Row])
  = \conn sql -> {
      let fut = spawn \-> {
          let rows = query &conn sql
          (conn, rows)                     -- return conn + results
        }
      -- conn is DEAD here (moved into spawn)
      await fut                            -- get (DbConn, [Row]) back
    }

-- Usage: connection round-trips through child task
process : DbConn -> <async, io, exn> ()
  = \conn -> {
      let (conn, users) = remote-query conn "SELECT * FROM users"
      let (conn, orders) = remote-query conn "SELECT * FROM orders"
      print (show (len users) ++ " users, " ++ show (len orders) ++ " orders")
      close-db conn
    }
```

### 11.5 Concatenative Style

```
-- Parallel map in concatenative form
par-map : ([a]) (a -- <async | e> b) -- <async | e> [b]
  = task-group-enter
    swap [spawn] each        -- spawn all, stack: [Future b]
    [await] each             -- await all, stack: [b]
    task-group-exit

-- Channel ping-pong
ping-pong : ( -- <async, io> ())
  = 1 channel                -- ( -- Sender Receiver)
    over                     -- ( -- Sender Receiver Sender)
    [42 swap send] spawn     -- spawn sender
    drop                     -- discard future (fire-and-forget within group)
    recv                     -- receive 42
    show print               -- print "42"
    swap close-sender        -- cleanup
```

---

## 12. Summary of New Primitives

### Types

| Type | Affine? | Cloneable? | Description |
|------|---------|------------|-------------|
| `Future[a]` | Yes | No | Handle to pending task result |
| `Sender[a]` | Yes | Yes | Channel send end (multi-producer) |
| `Receiver[a]` | Yes | No | Channel receive end (single-consumer) |
| `OneshotSender[a]` | Yes | No | Single-value send |
| `OneshotReceiver[a]` | Yes | No | Single-value receive |
| `TaskGroup` | N/A (scoped) | No | Implicit, managed by enter/exit |

### Operations

| Operation | Type | Effect |
|-----------|------|--------|
| `task-group` | `(() -> <async \| e> a) -> <async \| e> a` | Scoped child tasks |
| `spawn` | `(() -> <async \| e> a) -> <async \| e> Future[a]` | Launch child task |
| `await` | `Future[a] -> <async> a` | Block on future |
| `sleep` | `Duration -> <async> ()` | Suspend for duration |
| `yield` | `() -> <async> ()` | Cooperative yield |
| `select` | `SelectSet[a] -> <async \| e> a` | Wait on multiple sources |
| `channel` | `Int -> <async> (Sender[a], Receiver[a])` | Create bounded channel |
| `send` | `(Sender[a], a) -> <async, exn[ChannelClosed]> ()` | Send value |
| `recv` | `Receiver[a] -> <async, exn[ChannelClosed]> a` | Receive value |
| `try-recv` | `Receiver[a] -> <async> a?` | Non-blocking receive |
| `close-sender` | `Sender[a] -> <> ()` | Close send end |
| `close-receiver` | `Receiver[a] -> <> ()` | Close receive end |
| `shield` | `(() -> <async \| e> a) -> <async \| e> a` | Defer cancellation |
| `is-cancelled` | `() -> <async> Bool` | Check cancellation flag |
| `check-cancel` | `() -> <async, exn[Cancelled]> ()` | Raise if cancelled |
| `with-timeout` | `(Duration, () -> <async \| e> a) -> <async, exn[Timeout] \| e> a` | Timeout wrapper |

### VM Opcodes (16 new)

| Opcode | Hex | Description |
|--------|-----|-------------|
| `TASK_SPAWN` | 0xA0 | Spawn task from closure |
| `TASK_AWAIT` | 0xA1 | Await future |
| `TASK_YIELD` | 0xA2 | Cooperative yield |
| `TASK_SLEEP` | 0xA3 | Sleep N ms |
| `TASK_GROUP_ENTER` | 0xA4 | Enter task group |
| `TASK_GROUP_EXIT` | 0xA5 | Exit task group |
| `CHAN_NEW` | 0xA6 | Create channel |
| `CHAN_SEND` | 0xA7 | Send to channel |
| `CHAN_RECV` | 0xA8 | Receive from channel |
| `CHAN_TRY_RECV` | 0xA9 | Non-blocking receive |
| `CHAN_CLOSE` | 0xAA | Close channel end |
| `SELECT_BUILD` | 0xAB | Build select set |
| `SELECT_WAIT` | 0xAC | Wait on select |
| `TASK_CANCEL_CHECK` | 0xAD | Check cancellation |
| `TASK_SHIELD_ENTER` | 0xAE | Enter shield |
| `TASK_SHIELD_EXIT` | 0xAF | Exit shield |

---

## 13. Design Rationale

### Why structured concurrency (not free-form spawn)?

Unstructured spawn (Go goroutines, Java threads) leads to leaked tasks,
orphaned resources, and unpredictable lifetimes. Structured concurrency
guarantees that task lifetimes nest — a parent always outlives its children.
This is especially important for Clank because:
- Affine values moved into tasks must be consumed; orphaned tasks leak them
- Effect handlers are scoped; orphaned tasks may outlive their handlers
- Agent-written code benefits from strict lifetime guarantees that prevent
  subtle concurrency bugs

### Why MPSC channels (not MPMC)?

Single-consumer channels are simpler to reason about and implement. MPMC
introduces contention on the receiver side and makes ownership transfer
semantics more complex (which of N receivers gets the affine value?). For
fan-out patterns, the programmer can create N channels and route explicitly.

### Why cooperative cancellation (not preemptive)?

Preemptive cancellation (killing a task at any point) makes affine cleanup
impossible — the task might be between acquiring a resource and storing it.
Cooperative cancellation lets the task reach a safe point before responding.
The `shield` mechanism handles the edge case of critical sections.

### Why `Sender` is Clone but `Receiver` is not?

Multi-producer is a common and safe pattern (fan-in). Multi-consumer requires
either work-stealing (complex) or broadcasting (different semantics). Keeping
`Receiver` non-Clone forces explicit routing decisions. `Sender` being Clone
means it's affine but cloneable — you must eventually close all clones.

### Why separate opcodes (not pure effect dispatch)?

The `async` effect operations (`spawn`, `await`, etc.) *could* compile to
`EFFECT_PERFORM async_id, op_idx`. Dedicated opcodes allow the VM to:
- Skip handler stack search for built-in async operations
- Directly invoke scheduler primitives
- Maintain the same optimization pattern as I/O opcodes

The effect system's `async` handler is still conceptually present — the
dedicated opcodes are the runtime's implementation of that handler, just
like `IO_PRINT` is the runtime's implementation of the `io` handler.

### Interaction with GC

Futures, channels, and select sets are heap objects managed by the GC.
When a `Future` is discarded (via `discard`), the GC will reclaim the
memory, but the child task continues running until its task group exits.
The `discard` on a Future does NOT cancel the child — cancellation only
happens through task group exit or explicit cancellation.
