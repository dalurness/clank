# VM Opcodes for Shared State and Iterators

**Task:** TASK-088
**Mode:** Spec
**Date:** 2026-03-19
**Builds On:** VM instruction set (TASK-009), Shared state primitives (TASK-086), Streaming iterators (TASK-085)
**Design Context:** The Clank VM has 87 opcodes across 15 categories. This spec assigns opcode numbers for shared mutable state (Ref, AtomicVal, STM) and async iterator operations, defines their encoding and operand layout, and documents GC interaction.

---

## 1. Summary of Changes

| Category | Opcodes | Range | Source |
|----------|---------|-------|--------|
| Ref operations | 6 | 0xA0–0xA5 | TASK-086 |
| AtomicVal operations | 4 | 0xA8–0xAB | TASK-086 |
| TVar operations | 3 | 0xAC–0xAE | TASK-086 |
| STM control | 3 | 0xB4–0xB6 | TASK-086 |
| Iterator operations | 3 | 0xB0–0xB2 | TASK-085 |
| **Total new** | **19** | | |

New VM total: **87 + 19 = 106 opcodes**.

---

## 2. New Heap Object Kinds

Both TASK-085 and TASK-086 independently assigned heap kind 0x0C. This spec
resolves the conflict by assigning sequential values starting at 0x09 (the
first unused slot after FUTURE at 0x08):

```
heap_object kinds (additions):
  0x09  ITERATOR  — gen_state (suspended continuation) + cleanup_fn + done_flag
  0x0A  REF       — handle_count + value_slot + empty_flag + lock_word
  0x0B  TVAR      — version_counter + value_slot + empty_flag
  0x0C  ATOMIC    — atomic_value (Int or Bool, unboxed)
```

### GC Interaction

All four new heap kinds are traced by the mark-sweep GC:

- **ITERATOR**: The GC traces the suspended continuation closure and captured
  state. When the iterator handle becomes unreachable (no stack references),
  the cleanup function runs during finalization to release held resources
  (file handles, channel receivers). Iterators are **finalization roots** —
  the GC must run their cleanup before collecting.

- **REF**: The GC traces through the value slot to keep the contained value
  alive. The handle count tracks how many live handles reference this cell.
  When all handles are closed or unreachable, the REF and its contents become
  collectible. For affine T, the value slot may be empty (after `ref-take`);
  the GC skips tracing empty slots.

- **TVAR**: Similar to REF. The version counter is an untraced integer used
  by STM validation. During transactions, read-set and write-set entries hold
  temporary references to TVars — these are stack-allocated in the STM frame
  and traced as roots during GC.

- **ATOMIC**: Contains an unboxed primitive (Int or Bool). No internal heap
  pointers to trace. Unrestricted type — no handle counting or finalization.
  Collected when unreachable.

---

## 3. New Opcodes

### 3.1 Ref Operations (0xA0–0xA5)

These opcodes implement the `shared` effect operations on `Ref[T]` cells.
The runtime checks whether T is affine at the call site (type tag on the
value slot) and dispatches accordingly: `REF_READ` performs `ref-take` for
affine T (emptying the cell), and `REF_WRITE` performs `ref-put` for affine T
(filling an empty cell). This keeps the opcode count minimal while supporting
both unrestricted and affine cell contents.

| Opcode | Hex | Operand | Stack Effect | Description |
|--------|-----|---------|--------------|-------------|
| `REF_NEW` | 0xA0 | — | (v -- Ref) | Allocate Ref cell with initial value v |
| `REF_READ` | 0xA1 | — | (Ref -- Ref v) | Read value (non-destructive, copies). For affine T: take (empties cell; traps E011 if empty) |
| `REF_WRITE` | 0xA2 | — | (v Ref -- Ref) | Write value (overwrites). For affine T: put (fills empty cell; traps E012 if full) |
| `REF_CAS` | 0xA3 | — | (expected new Ref -- Ref Bool cur) | Compare-and-swap. Pushes success flag and current value |
| `REF_MODIFY` | 0xA4 | — | (Quote Ref -- Ref v_new) | Atomic read-apply-write with pure function. Returns new value. Internally CAS-loops |
| `REF_CLOSE` | 0xA5 | — | (Ref -- ) | Close handle. Decrements handle count. Also works for TVar (dispatches on heap kind) |

#### Encoding Details

All Ref opcodes are single-byte (no operand). Operands come from the stack.
The Ref handle remains on the stack after read/write/cas/modify operations
(it is borrowed, not consumed). Only `REF_CLOSE` consumes the handle.

#### `REF_READ` Semantics

1. Pop Ref handle from stack
2. Check heap object kind = REF (0x0A); trap E002 TYPE_MISMATCH if not
3. If value slot is marked affine:
   a. Check empty flag; trap E011 REF_EMPTY if set
   b. Move value out of slot, set empty flag
   c. Push Ref handle, then push value
4. If value slot is unrestricted:
   a. Copy value from slot
   b. Push Ref handle, then push value copy

#### `REF_WRITE` Semantics

1. Pop Ref handle, then pop value from stack
2. Check heap object kind = REF (0x0A)
3. If value is affine:
   a. Check empty flag is set; trap E012 REF_FULL if not
   b. Move value into slot, clear empty flag
   c. Push Ref handle
4. If value is unrestricted:
   a. Overwrite slot with new value (old value becomes garbage)
   b. Push Ref handle

#### `REF_CAS` Semantics

1. Pop Ref handle, pop `new`, pop `expected`
2. Atomically: read current value; if `current == expected`, write `new`
3. Push Ref handle, push Bool (success), push current value
4. For affine T: CAS operates on identity (pointer equality), not structural equality

#### `REF_MODIFY` Semantics

1. Pop Ref handle, pop Quote (pure function)
2. Loop:
   a. Read current value
   b. Apply Quote to value (call, get result)
   c. CAS old → new; if success, break
   d. On failure, retry from (a)
3. Push Ref handle, push new value
4. The Quote must be pure (`<>` effect row). The compiler enforces this;
   the VM does not re-check (the function has already been type-checked).

#### `REF_CLOSE` Semantics

1. Pop handle from stack
2. Check heap object kind is REF (0x0A) or TVAR (0x0B); trap E002 if neither
3. Decrement handle count
4. If handle count reaches 0 and the object holds an affine value, run
   the value's destructor (if any) before the cell becomes collectible

### 3.2 AtomicVal Operations (0xA8–0xAB)

These opcodes provide lock-free atomic operations on primitive values (Int,
Bool). AtomicVal is unrestricted — no handle counting or close protocol.

| Opcode | Hex | Operand | Stack Effect | Description |
|--------|-----|---------|--------------|-------------|
| `ATOMIC_NEW` | 0xA8 | — | (v -- AtomicVal) | Allocate AtomicVal with initial value |
| `ATOMIC_LOAD` | 0xA9 | — | (AtomicVal -- AtomicVal v) | Atomic load (sequentially consistent) |
| `ATOMIC_STORE` | 0xAA | — | (v AtomicVal -- AtomicVal) | Atomic store (sequentially consistent) |
| `ATOMIC_RMW` | 0xAB | u8:op | varies (see below) | Atomic read-modify-write |

#### `ATOMIC_RMW` Sub-operations

The `u8:op` operand selects the read-modify-write operation:

| op | Name | Stack Effect | Description |
|----|------|--------------|-------------|
| 0 | CAS | (expected new AtomicVal -- AtomicVal Bool cur) | Compare-and-swap |
| 1 | ADD | (delta AtomicVal -- AtomicVal old) | Fetch-and-add (Int only) |
| 2 | SUB | (delta AtomicVal -- AtomicVal old) | Fetch-and-subtract (Int only) |

All AtomicVal operations use sequential consistency memory ordering. Relaxed
ordering is deferred to a future spec (see TASK-086 §10 known gaps).

The AtomicVal stays on the stack after load/store/rmw (it is not consumed).
Since AtomicVal is unrestricted, there is no close operation.

### 3.3 TVar Operations (0xAC–0xAE)

These opcodes implement transactional reads and writes on `TVar[T]` cells.
They are only valid inside an STM transaction block (between `STM_ATOMIC`
and the end of the transaction). Executing a TVar operation outside a
transaction traps with E002 TYPE_MISMATCH.

| Opcode | Hex | Operand | Stack Effect | Description |
|--------|-----|---------|--------------|-------------|
| `TVAR_NEW` | 0xAC | — | (v -- TVar) | Allocate TVar with initial value. Valid outside transactions |
| `TVAR_READ` | 0xAD | — | (TVar -- TVar v) | Read TVar in transaction. Records in read-set. For affine T: take (traps E013 if empty) |
| `TVAR_WRITE` | 0xAE | — | (v TVar -- TVar) | Write TVar in transaction. Buffers in write-set. For affine T: put (traps E014 if full) |

#### Transaction Buffering

`TVAR_READ` and `TVAR_WRITE` do not modify the TVar directly:

- **Read**: Records `(tvar_ptr, version, value)` in the transaction's read-set.
  Returns the buffered value (write-set value if previously written in this
  transaction, otherwise the committed value).
- **Write**: Records `(tvar_ptr, new_value)` in the write-set. The value is
  not committed until `STM_ATOMIC` completes successfully.

TVar handles follow the same borrow convention as Ref — the handle stays on
the stack after read/write. `REF_CLOSE` (0xA5) also accepts TVar handles
(dispatches on heap kind 0x0B).

### 3.4 STM Control (0xB4–0xB6)

These opcodes manage Software Transactional Memory transaction lifecycle.

| Opcode | Hex | Operand | Stack Effect | Description |
|--------|-----|---------|--------------|-------------|
| `STM_ATOMIC` | 0xB4 | u16:body_len | ( -- ) | Begin transaction. Execute body; validate and commit on completion |
| `STM_RETRY` | 0xB5 | — | ( -- ) | Abort transaction; re-execute when any read-set TVar changes |
| `STM_OR_ELSE` | 0xB6 | u16:alt_offset | ( -- ) | Mark alternative path; if primary retries, jump to alt_offset |

#### `STM_ATOMIC` Semantics

`STM_ATOMIC` is a block-scoped instruction. The `u16:body_len` operand
specifies the byte length of the transaction body.

1. Push an STM frame onto the return stack:
   ```
   stm_frame:
     read_set      []     — (tvar_ptr, version, value) entries
     write_set     []     — (tvar_ptr, new_value) entries
     retry_set     []     — tvar_ptrs to watch (populated on retry)
     body_start    u32    — IP of transaction body start
     body_len      u16    — length of body bytecode
     or_else_alt   u32?   — optional alternative body IP (from STM_OR_ELSE)
   ```
2. Execute the body (IP advances through body_len bytes)
3. On reaching the end of the body, **validate**:
   a. For each `(tvar, version, _)` in read-set: check that tvar's current
      version matches. If any mismatch → discard write-set, restart body.
   b. If all versions match: **commit** — for each `(tvar, value)` in
      write-set, atomically update the tvar and increment its version.
   c. Pop the STM frame.
4. On exception during body: propagate exception (abort transaction, discard
   read/write sets). Exception-based abort is intentional — `exn` is allowed
   inside transactions.

#### `STM_RETRY` Semantics

1. Collect all TVars in the current transaction's read-set
2. Discard read-set and write-set
3. Suspend the current task, registering interest in the collected TVars
4. When any registered TVar is modified (by another transaction committing),
   wake the task and re-execute the transaction body from `body_start`
5. If inside an `STM_OR_ELSE` block: instead of suspending, jump to the
   alternative body at `or_else_alt`

#### `STM_OR_ELSE` Semantics

1. Record `alt_offset` (relative to current IP) in the STM frame as `or_else_alt`
2. Execute normally — the primary body follows immediately
3. If the primary body calls `STM_RETRY`:
   a. Discard read/write sets from the primary attempt
   b. Jump to `or_else_alt` and re-execute as a fresh transaction body
   c. If the alternative also retries, perform a true retry (suspend + watch)

#### `atomically` Compilation Example

```
atomically { tvar-write(&v, tvar-read(&v) + 1) }
```

compiles to:

```
STM_ATOMIC <body_len>      # begin transaction
  LOCAL_GET v_idx           # push &v (TVar)
  TVAR_READ                 # read current value
  SWAP                      # (v TVar -- TVar v)... actually:
```

Let me redo this more carefully:

```
STM_ATOMIC 12               # begin transaction (body = 12 bytes)
  LOCAL_GET 0                # push TVar v
  DUP                        # duplicate for read and later write
  TVAR_READ                  # (TVar -- TVar val)
  PUSH_INT 1
  ADD                        # val + 1
  SWAP                       # (new_val TVar)...
```

Actually, the stack threading is:

```
STM_ATOMIC 14                # begin transaction
  LOCAL_GET 0                 # push TVar handle
  DUP                         # (TVar TVar)
  TVAR_READ                   # (TVar TVar val) — read returns (TVar val)
                               # wait: TVAR_READ pops TVar, pushes TVar val
                               # so after DUP+TVAR_READ: (TVar TVar val)
                               # no — DUP gives (TVar TVar), TVAR_READ pops top TVar,
                               # pushes (TVar val), so stack is (TVar TVar val)
                               # Hmm, that's wrong. Let me re-read the stack effect.
```

TVAR_READ: `(TVar -- TVar v)` — it *keeps* the TVar on stack and pushes the value.

So: `LOCAL_GET 0` → `(TVar)`, `TVAR_READ` → `(TVar val)`, `PUSH_INT 1` → `(TVar val 1)`, `ADD` → `(TVar new_val)`, `SWAP` → `(new_val TVar)`, `TVAR_WRITE` → `(TVar)`, `DROP` → `()`.

```
STM_ATOMIC 10               # begin transaction (body = 10 bytes)
  LOCAL_GET 0                # push TVar (local slot 0)
  TVAR_READ                  # (TVar -- TVar val)
  PUSH_INT 1                 #
  ADD                        # (TVar new_val)
  SWAP                       # (new_val TVar)
  TVAR_WRITE                 # (new_val TVar -- TVar)
  DROP                       # consume TVar from stack
                             # end of body — validate and commit
```

#### `or-else` Compilation Example

```
or-else(
  { bounded-queue-take(&q1) },
  { bounded-queue-take(&q2) }
)
```

compiles to:

```
STM_OR_ELSE +<alt_offset>   # mark alternative
  LOCAL_GET 0                # push &q1
  CALL <bounded_queue_take>  # may call STM_RETRY internally
  JMP +<end>                 # skip alternative
alt:
  LOCAL_GET 1                # push &q2
  CALL <bounded_queue_take>  # if this retries too, true retry
end:
```

### 3.5 Iterator Operations (0xB0–0xB2)

These opcodes support the async iterator protocol from TASK-085. They
provide optimized paths for iterator creation, advancement, and cleanup
that bypass the handler stack search overhead of the general effect
dispatch mechanism.

| Opcode | Hex | Operand | Stack Effect | Description |
|--------|-----|---------|--------------|-------------|
| `ITER_NEW` | 0xB0 | — | (Closure Cleanup -- Iter) | Create iterator from generator closure + cleanup function |
| `ITER_NEXT` | 0xB1 | — | (Iter -- Iter value) | Advance iterator; push next value. Traps E016 if exhausted |
| `ITER_CLOSE` | 0xB2 | — | (Iter -- ) | Close iterator, run cleanup function. Consumes handle |

#### `ITER_NEW` Semantics

1. Pop cleanup function (Closure or Quote) and generator closure from stack
2. Allocate ITERATOR heap object (kind 0x09):
   ```
   iterator_object:
     gen_state     ptr    — suspended generator continuation
     cleanup_fn    ptr    — resource release function
     done_flag     bool   — true when generator has returned
   ```
3. The generator closure is suspended — it has not begun execution yet.
   The first `ITER_NEXT` call starts it.
4. Push the Iterator handle onto the stack.

#### `ITER_NEXT` Semantics

1. Peek at Iterator handle on stack (borrow — handle stays)
2. Check done flag; if set, trap E016 ITER_DONE
3. **Cancellation check**: if the current task has a pending cancellation
   signal, run cleanup and trap E016 (iterator is a cancellation point)
4. Resume the generator continuation:
   a. If first call: start executing the generator closure
   b. If subsequent: resume from the last yield point
5. When `yield(v)` is hit (the generator performs `gen.yield`):
   a. Suspend the generator (save continuation)
   b. Push the yielded value onto the caller's stack
6. When the generator body returns (no more yields):
   a. Set done flag = true
   b. Trap E016 ITER_DONE

#### `ITER_CLOSE` Semantics

1. Pop Iterator handle from stack (consumes it)
2. If not already done: cancel the suspended generator
3. Execute the cleanup function (e.g., close file handle, close channel receiver)
4. Mark the iterator object as closed (E017 on any subsequent access)

#### `for` Loop Compilation

```
for x in expr { body }
```

compiles to:

```
<eval expr>               # push Iterator
loop_top:
  DUP                     # keep Iterator for next iteration
  ITER_NEXT               # (Iter -- Iter value) or trap E016
  LOCAL_SET x_idx         # bind value to x
  <body bytecode>
  JMP loop_top
iter_done_handler:        # E016 handler (installed via HANDLE_PUSH for exn[IterDone])
  DROP                    # discard Iterator copy from DUP
  ITER_CLOSE              # close the iterator
```

The `for` expression installs an E016 handler before the loop. When
`ITER_NEXT` traps E016, control transfers to `iter_done_handler`, which
closes the iterator and continues execution after the `for`.

More precisely, compilation wraps the loop in a handler frame:

```
<eval expr>                     # push Iterator
HANDLE_PUSH exn_id, iter_done   # install IterDone handler
loop_top:
  ITER_NEXT                     # (Iter -- Iter value)
  LOCAL_SET x_idx
  <body>
  JMP loop_top
iter_done:                      # handler clause for IterDone
  DROP                          # discard exception value
  DROP                          # discard continuation
  HANDLE_POP
  ITER_CLOSE                    # cleanup
```

---

## 4. New Trap Codes

| Code | Name | Trigger |
|------|------|---------|
| E011 | REF_EMPTY | `REF_READ` (take mode) on empty affine cell |
| E012 | REF_FULL | `REF_WRITE` (put mode) on non-empty affine cell |
| E013 | TVAR_EMPTY | `TVAR_READ` (take mode) on empty affine TVar |
| E014 | TVAR_FULL | `TVAR_WRITE` (put mode) on non-empty affine TVar |
| E015 | STM_CONFLICT | Transaction validation failed (internal; triggers automatic retry, not surfaced to user code) |
| E016 | ITER_DONE | Iterator exhausted (normal termination signal) |
| E017 | ITER_CLOSED | Operation on already-closed iterator |

---

## 5. Updated Opcode Summary Table

| Range | Category | Count |
|-------|----------|-------|
| 0x00–0x07 | Stack manipulation | 8 |
| 0x10–0x18 | Constants/literals | 9 |
| 0x20–0x25 | Arithmetic | 6 |
| 0x28–0x2D | Comparison | 6 |
| 0x30–0x32 | Logic | 3 |
| 0x38–0x3F | Control flow | 8 |
| 0x40–0x41 | Quotations/closures | 2 |
| 0x48–0x49 | Local variables | 2 |
| 0x50–0x60 | Heap/compound values | 15 |
| 0x62–0x67 | Strings | 6 |
| 0x70–0x74 | Effect dispatch | 5 |
| 0x80–0x82 | Module loading | 3 |
| 0x88–0x8C | Type/runtime checks | 5 |
| 0x90–0x95 | I/O primitives | 6 |
| **0xA0–0xA5** | **Ref operations** | **6** |
| **0xA8–0xAB** | **AtomicVal operations** | **4** |
| **0xAC–0xAE** | **TVar operations** | **3** |
| **0xB0–0xB2** | **Iterator operations** | **3** |
| **0xB4–0xB6** | **STM control** | **3** |
| 0xF0–0xF2 | VM control | 3 |
| **Total** | | **106** |

106 opcodes total. Sparse encoding still leaves ~55% of the opcode space
free (0xB7–0xEF, plus gaps in existing ranges) for future extensions.

---

## 6. Design Rationale

### Why 16 shared-state opcodes instead of 24+ (one per language operation)?

Several TASK-086 operations are compiled as library functions rather than
dedicated opcodes:

- **`ref-swap`**: Compiled as a CAS loop (`REF_READ` + `REF_CAS`). The
  atomic swap is expressible in 5-6 bytecode instructions. A dedicated
  opcode would save ~4 bytes per call site but adds decode complexity.
- **`tvar-take`/`tvar-put`**: `TVAR_READ`/`TVAR_WRITE` dispatch on affine
  type at runtime. Same opcode, different behavior — keeps the instruction
  set small.
- **`ref-take`/`ref-put`**: Same approach — `REF_READ`/`REF_WRITE` dispatch
  on affine type.
- **`atomic-sub`**: `ATOMIC_RMW` with op=2. Sharing the opcode with CAS
  and ADD saves two opcode slots.
- **`tvar-close`**: `REF_CLOSE` dispatches on heap object kind, handling
  both Ref and TVar. One opcode for two types.

### Why `REF_CLOSE` also handles TVar?

Both Ref and TVar have identical close semantics: decrement handle count,
run destructor if last handle. The only difference is the heap object kind
tag, which the VM already inspects. A single `REF_CLOSE` opcode that
dispatches on kind (0x0A vs 0x0B) saves an opcode slot without adding
meaningful complexity.

### Why dedicated iterator opcodes instead of pure effect dispatch?

`ITER_NEXT` is hot-path code — it executes once per element in every `for`
loop. Routing through the handler stack (EFFECT_PERFORM → search → dispatch)
adds overhead per element. Dedicated opcodes skip the search and directly
invoke the generator resume/yield machinery. This mirrors the rationale
for dedicated I/O opcodes (§9 of the original spec).

### Why `STM_ATOMIC` uses body_len instead of handler-style push/pop?

STM transactions have different semantics than effect handlers:
- Transactions may restart silently (on validation failure)
- `retry` suspends and resumes the entire body, not just a handler clause
- Transaction metadata (read/write sets) has a different lifecycle than
  handler frames

A dedicated `STM_ATOMIC` instruction makes these semantics explicit in the
bytecode rather than overloading the effect handler machinery. The
`body_len` operand lets the VM know the transaction boundary without
scanning for a matching pop instruction.

### Why reassign heap object kinds (not use 0x0C for both)?

TASK-085 and TASK-086 independently assigned 0x0C for ITERATOR and REF
respectively. Since heap kind tags must be unique (the GC and type system
dispatch on them), this spec assigns sequential values starting at 0x09.
The ordering (ITERATOR < REF < TVAR < ATOMIC) groups related kinds but
has no semantic significance.

### Memory ordering for AtomicVal

All AtomicVal operations use sequential consistency. This is the simplest
and safest default — it matches the mental model of "one operation at a
time" and avoids subtle reordering bugs. Relaxed ordering (acquire/release)
is a future optimization noted in TASK-086 as out of scope.

---

## 7. Interaction Summary

### Effect → Opcode Mapping

| Language Operation | Effect | Opcode(s) |
|-------------------|--------|-----------|
| `ref-new(v)` | `<shared>` | `REF_NEW` |
| `ref-read(&r)` | `<shared>` | `REF_READ` |
| `ref-write(&r, v)` | `<shared>` | `REF_WRITE` |
| `ref-swap(&r, v)` | `<shared>` | `REF_READ` + `REF_CAS` loop |
| `ref-modify(&r, f)` | `<shared>` | `REF_MODIFY` |
| `ref-cas(&r, old, new)` | `<shared>` | `REF_CAS` |
| `ref-take(&r)` | `<shared, exn>` | `REF_READ` (affine dispatch) |
| `ref-put(&r, v)` | `<shared, exn>` | `REF_WRITE` (affine dispatch) |
| `ref-close(r)` | `<>` | `REF_CLOSE` |
| `atomic-new(v)` | `<shared>` | `ATOMIC_NEW` |
| `atomic-load(a)` | `<shared>` | `ATOMIC_LOAD` |
| `atomic-store(a, v)` | `<shared>` | `ATOMIC_STORE` |
| `atomic-cas(a, old, new)` | `<shared>` | `ATOMIC_RMW` op=0 |
| `atomic-add(a, n)` | `<shared>` | `ATOMIC_RMW` op=1 |
| `atomic-sub(a, n)` | `<shared>` | `ATOMIC_RMW` op=2 |
| `tvar-new(v)` | `<shared>` | `TVAR_NEW` |
| `tvar-read(&t)` | `<stm>` | `TVAR_READ` |
| `tvar-write(&t, v)` | `<stm>` | `TVAR_WRITE` |
| `tvar-take(&t)` | `<stm, exn>` | `TVAR_READ` (affine dispatch) |
| `tvar-put(&t, v)` | `<stm, exn>` | `TVAR_WRITE` (affine dispatch) |
| `tvar-close(t)` | `<>` | `REF_CLOSE` (kind dispatch) |
| `atomically(body)` | `<shared>` | `STM_ATOMIC` |
| `retry()` | `<stm>` | `STM_RETRY` |
| `or-else(a, b)` | `<stm>` | `STM_OR_ELSE` |
| `next(&it)` | `<async, exn>` | `ITER_NEXT` |
| `close-iter(it)` | `<async>` | `ITER_CLOSE` |
| `iter { body }` | `<>` | `ITER_NEW` |

### Shared Effect Capability Check

Like `io` operations check for the `io` capability, `shared` operations
check that a `shared` capability is in scope. The runtime installs this
capability at program entry (alongside `io`). Functions without `<shared>`
in their effect row cannot compile calls to Ref/AtomicVal operations —
the compiler rejects them before they reach the VM.

STM operations (`TVAR_READ`, `TVAR_WRITE`, `STM_RETRY`) additionally
check for an active STM frame on the return stack. Executing them outside
a transaction traps immediately.
