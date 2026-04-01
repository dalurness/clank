# Clank STM Runtime Implementation Spec

**Task:** TASK-090
**Mode:** Spec
**Date:** 2026-03-19
**Builds On:** Shared mutable state (TASK-086), GC strategy (TASK-013), Async runtime (TASK-014), Effect system (TASK-005), VM instruction set (TASK-088)

---

## 1. Overview

This spec defines the runtime implementation of Clank's Software Transactional Memory (STM). The language-level semantics are defined in `shared-mutable-state.md` §4; this document covers the internal data structures, algorithms, and runtime integration needed to make those semantics work.

**Scope:** Transaction lifecycle, read/write set management, validation protocol, commit algorithm, retry/or-else runtime semantics, GC safety during transactions, and contention management.

**Out of scope:** VM opcode encoding (TASK-088), performance benchmarking, stdlib concurrent data structures.

**Go implementation note:** The Go VM (`internal/vm/vm.go`) implements a simplified STM sufficient for single-threaded execution. It uses a **write-log** (`txnWriteLog []tvarSnapshot`) that records the pre-modification value of each TVar on first write within a transaction. On abort, it restores all snapshotted TVars. There is no global version clock, no read set, and no commit-time validation — these full-concurrency mechanisms are not needed in the single-threaded Go evaluator. Nested `atomically` calls are supported via write-log save/restore/merge (see §9.4).

---

## 2. TVar Heap Object Layout

TVars are heap-allocated objects of kind `TVAR` (0x0D). Each TVar stores a versioned value cell used for optimistic conflict detection.

### 2.1 Object Layout

```
Offset  Size   Field           Description
──────  ────   ─────           ───────────
0       1B     mark            GC mark state (WHITE/GRAY/BLACK)
4       1B     kind            0x0D (TVAR)
8       4B     size            Payload size in bytes (fixed: 24)
12      2B     reserved        Alignment padding
                               ── payload begins ──
16      8B     version         Monotonic commit counter (uint64)
24      8B     value           Tagged value (current committed value)
32      1B     occupied        0x00 = empty, 0x01 = occupied (for affine take/put)
33      7B     padding         Alignment to 8-byte boundary
```

Total object size: 40 bytes (8B header + 32B payload).

### 2.2 Version Counter

Each TVar carries a monotonic `version` counter, incremented on every successful commit that writes to this TVar. Transactions record the version at read time and check it again at commit time. A mismatch means another transaction committed a write to this TVar in the interim — the current transaction must abort and retry.

The version counter is a 64-bit unsigned integer. Overflow is not a practical concern (2^64 commits per TVar).

### 2.3 Global Version Clock

A single global atomic counter (`global_clock`) provides a monotonically increasing timestamp. Each successful commit atomically increments this clock and uses the new value as the version stamp for all TVars written in that commit. This avoids per-TVar version comparison across the full read set — instead, transactions compare against a single timestamp.

```
global_clock : AtomicU64    // initialized to 0
```

**Why a global clock:** Without it, validating a read set of N TVars requires N version comparisons against individually-evolving versions, and there is no total order on commits. A global clock gives each commit a unique timestamp, enabling a simpler validation rule: "all TVars in my read set have version ≤ my snapshot timestamp."

---

## 3. Transaction Descriptor

Each active transaction is represented by a `TxnDescriptor`, a stack-allocated (or frame-local) structure that tracks the transaction's state. It is **not** a heap object — it lives in the VM's call frame for the `atomically` handler.

### 3.1 Structure

```
TxnDescriptor {
    snapshot_version : u64          // global_clock value at transaction start
    read_set         : ReadSet      // TVars read + their versions at read time
    write_set        : WriteSet     // TVars written + buffered new values
    status           : TxnStatus    // ACTIVE | COMMITTED | ABORTED
    retry_watches    : WatchSet     // TVars to watch on retry (populated on retry path)
    or_else_stack    : OrElseStack  // nested or-else checkpoints
}

enum TxnStatus { ACTIVE, COMMITTED, ABORTED }
```

### 3.2 Read Set

An array of `(tvar_ptr, read_version)` pairs. When a TVar is read during a transaction, the runtime records:
- A pointer to the TVar heap object
- The TVar's `version` field at the time of the read

```
ReadSet = Vec<ReadEntry>

ReadEntry {
    tvar    : *TVar     // pointer to TVar heap object
    version : u64       // version observed at read time
}
```

**Deduplication:** If the same TVar is read multiple times, only the first read creates an entry. Subsequent reads of the same TVar return the value from the write set (if written) or the original read set value (consistent snapshot).

### 3.3 Write Set

An array of `(tvar_ptr, new_value, new_occupied)` triples. Writes are buffered here — never applied to the TVar directly until commit.

```
WriteSet = Vec<WriteEntry>

WriteEntry {
    tvar         : *TVar    // pointer to TVar heap object
    new_value    : Value    // buffered new value (tagged 64-bit)
    new_occupied : bool     // for affine take/put: new occupied state
}
```

**Lookup order for tvar-read:** write set first (most recent buffered write), then read set, then TVar's committed value.

### 3.4 Sizing

Read and write sets use dynamically-sized arrays. Initial capacity: 8 entries (covers common small transactions). Growth: double on overflow. Memory is freed on transaction commit or abort.

Since transactions cannot contain `async` or `io` effects, they are bounded by pure computation — typically short-lived. Large read/write sets indicate a code smell, not a runtime crisis.

---

## 4. Transaction Lifecycle

### 4.1 Begin (`atomically` entry)

When the VM executes `atomically`:

1. Allocate a `TxnDescriptor` in the current call frame
2. Snapshot: `descriptor.snapshot_version = atomic_load(global_clock)`
3. Set `descriptor.status = ACTIVE`
4. Install an effect handler for `stm` that routes `tvar-read` and `tvar-write` to the transaction runtime (using the existing `HANDLE_PUSH` mechanism)
5. Execute the transaction body

### 4.2 TVar Read (`tvar-read`)

When the `stm` handler intercepts a `tvar-read` operation:

```
fn txn_read(descriptor: &mut TxnDescriptor, tvar: *TVar) -> Value:
    // 1. Check write set (most recent buffered value wins)
    if let Some(entry) = descriptor.write_set.find(tvar):
        return entry.new_value

    // 2. Check read set (already read this TVar in this transaction)
    if let Some(entry) = descriptor.read_set.find(tvar):
        return entry.value  // return consistent snapshot value

    // 3. First read of this TVar — read from committed state
    let current_version = tvar.version
    let current_value = tvar.value

    // 4. Validate freshness: version must not exceed our snapshot
    //    (another transaction may have committed since we started)
    if current_version > descriptor.snapshot_version:
        // Stale snapshot — abort and restart
        txn_abort_and_restart(descriptor)

    // 5. Record in read set
    descriptor.read_set.push(ReadEntry {
        tvar: tvar,
        version: current_version,
    })

    return current_value
```

**Eager validation on read (step 4):** If a TVar's version exceeds the transaction's snapshot, the transaction is already doomed — it will fail validation at commit. Detecting this early avoids wasted work. This is an optimization, not a correctness requirement.

### 4.3 TVar Write (`tvar-write`)

When the `stm` handler intercepts a `tvar-write` operation:

```
fn txn_write(descriptor: &mut TxnDescriptor, tvar: *TVar, value: Value):
    // Ensure the TVar is in the read set (needed for validation)
    if not descriptor.read_set.contains(tvar) and not descriptor.write_set.contains(tvar):
        // Implicit read — record current version for validation
        let current_version = tvar.version
        if current_version > descriptor.snapshot_version:
            txn_abort_and_restart(descriptor)
        descriptor.read_set.push(ReadEntry {
            tvar: tvar,
            version: current_version,
        })

    // Buffer the write (overwrite if already in write set)
    descriptor.write_set.upsert(tvar, WriteEntry {
        tvar: tvar,
        new_value: value,
        new_occupied: true,
    })
```

Writes are **never** applied to the TVar until commit. This ensures atomicity — either all writes land or none do.

### 4.4 TVar Take / Put (Affine T)

For affine `T`, `tvar-take` and `tvar-put` follow the same buffering discipline:

**tvar-take:**
```
fn txn_take(descriptor: &mut TxnDescriptor, tvar: *TVar) -> Value:
    // Check write set for buffered state
    if let Some(entry) = descriptor.write_set.find(tvar):
        if not entry.new_occupied:
            raise exn[TVarEmpty]
        let val = entry.new_value
        entry.new_occupied = false
        entry.new_value = UNIT  // placeholder
        return val

    // Read committed state
    let current_version = tvar.version
    if current_version > descriptor.snapshot_version:
        txn_abort_and_restart(descriptor)

    if not tvar.occupied:
        raise exn[TVarEmpty]

    let val = tvar.value
    descriptor.read_set.push(ReadEntry { tvar, version: current_version })
    descriptor.write_set.upsert(tvar, WriteEntry {
        tvar: tvar,
        new_value: UNIT,
        new_occupied: false,
    })
    return val
```

**tvar-put:** symmetric — checks that the cell is empty (in buffered state), then buffers a write with `occupied = true`.

### 4.5 Validate

Validation checks that no TVar in the read set has been modified since the transaction began.

```
fn txn_validate(descriptor: &TxnDescriptor) -> bool:
    for entry in descriptor.read_set:
        if entry.tvar.version != entry.version:
            return false
    return true
```

Validation must be performed **while holding the commit lock** (see §4.6) to prevent TOCTTOU races between validation and write-back.

### 4.6 Commit

The commit protocol must be atomic with respect to other committing transactions. We use a lightweight global commit lock (spinlock). Since the critical section is short (validate + write-back, no allocation or IO), contention is low.

```
fn txn_commit(descriptor: &mut TxnDescriptor) -> Result<Value, Abort>:
    // Acquire global commit lock
    commit_lock.acquire()

    // Validate read set under lock
    if not txn_validate(descriptor):
        commit_lock.release()
        descriptor.status = ABORTED
        return Err(Abort)

    // Commit succeeds — increment global clock
    let new_version = atomic_increment(global_clock)

    // Write-back: apply all buffered writes to TVars
    for entry in descriptor.write_set:
        entry.tvar.value = entry.new_value
        entry.tvar.occupied = entry.new_occupied
        entry.tvar.version = new_version

    commit_lock.release()

    // Wake any transactions blocked on retry that read TVars we wrote
    txn_wake_retriers(descriptor.write_set)

    descriptor.status = COMMITTED
    return Ok(transaction_result)
```

**Why a global lock instead of per-TVar locks?** Simplicity. Per-TVar locking requires lock ordering to avoid deadlocks, which adds complexity. The global commit lock's critical section is O(read_set + write_set) — bounded and fast. If profiling reveals this as a bottleneck, the lock can be replaced with fine-grained locking or a lock-free commit protocol in a future iteration.

### 4.7 Abort and Restart

When validation fails (at read time or commit time):

1. Discard the write set (no writes were applied — nothing to undo)
2. Discard the read set
3. Reset the descriptor: new snapshot from `global_clock`, clear all sets
4. Re-execute the transaction body from the beginning

Re-execution uses the continuation captured by the `atomically` effect handler. The handler simply resumes the transaction body closure again.

**No rollback needed:** Because writes are buffered, abort is free — just discard the buffers. This is the core advantage of optimistic concurrency.

---

## 5. Retry Semantics

### 5.1 Overview

`retry()` means: "the values I've read don't satisfy my precondition — block until at least one of them changes, then re-execute."

### 5.2 Implementation

When the `stm` handler intercepts `retry`:

```
fn txn_retry(descriptor: &mut TxnDescriptor):
    // 1. Collect watch set from read set
    let watches: Set<*TVar> = descriptor.read_set.iter().map(|e| e.tvar).collect()

    // 2. Register this transaction (thread/task) on each TVar's wait queue
    let waiter = Waiter::current()
    for tvar in watches:
        tvar.wait_queue.add(waiter)

    // 3. Discard read/write sets
    descriptor.read_set.clear()
    descriptor.write_set.clear()

    // 4. Block the current task (yield to scheduler)
    waiter.park()

    // 5. On wake: remove from wait queues, restart transaction
    for tvar in watches:
        tvar.wait_queue.remove(waiter)

    txn_restart(descriptor)
```

### 5.3 TVar Wait Queue

Each TVar optionally maintains a wait queue of parked tasks. This is a linked list of `Waiter` pointers, protected by the TVar's own lightweight lock (or a striped lock table to save per-TVar memory).

```
TVar (extended layout):
    ...existing fields...
    wait_queue : WaiterList   // intrusive linked list of parked tasks
```

The `txn_wake_retriers` call in the commit path (§4.6) iterates the write set and wakes all waiters on each written TVar:

```
fn txn_wake_retriers(write_set: &WriteSet):
    for entry in write_set:
        let waiters = entry.tvar.wait_queue.drain()
        for waiter in waiters:
            waiter.unpark()  // schedule task for re-execution
```

### 5.4 Spurious Wakes

A woken transaction restarts from scratch with a fresh snapshot. It may discover that the new values still don't satisfy its precondition and `retry` again. This is correct — retry is idempotent. The cost is one wasted validation; this is acceptable.

### 5.5 Integration with Async Scheduler

`waiter.park()` integrates with Clank's async runtime scheduler. A parked STM task yields its execution slot (like `yield` or `await`). The scheduler treats it as a blocked task — it will not be polled until `unpark()` is called. This reuses the existing task suspension mechanism from the async runtime.

---

## 6. Or-Else Semantics

### 6.1 Overview

`or-else(action1, action2)` means: "try `action1`; if it calls `retry`, discard its effects and try `action2` instead; if `action2` also retries, the combined transaction retries."

### 6.2 Implementation

Or-else requires **checkpointing** the transaction state so that `action1`'s reads/writes can be discarded if it retries.

```
fn txn_or_else(descriptor: &mut TxnDescriptor, action1: Closure, action2: Closure) -> Value:
    // Save checkpoint
    let checkpoint = OrElseCheckpoint {
        read_set_len: descriptor.read_set.len(),
        write_set_len: descriptor.write_set.len(),
    }

    // Try action1
    match try_action(descriptor, action1):
        Ok(result) -> return result
        Retry ->
            // Rollback to checkpoint (discard action1's reads/writes)
            descriptor.read_set.truncate(checkpoint.read_set_len)
            descriptor.write_set.truncate(checkpoint.write_set_len)

            // Try action2
            match try_action(descriptor, action2):
                Ok(result) -> return result
                Retry ->
                    // Both retried — the combined transaction retries
                    // Watch set = union of TVars read by action1 AND action2
                    txn_retry(descriptor)
```

### 6.3 Checkpoint Stack

Or-else calls can nest (e.g., `or-else(or-else(a, b), c)`). The `TxnDescriptor` maintains an `or_else_stack` of checkpoints. Each nested `or-else` pushes a checkpoint; on retry of the first branch, it pops and rolls back.

```
OrElseCheckpoint {
    read_set_len  : usize   // snapshot of read_set length
    write_set_len : usize   // snapshot of write_set length
}
```

This works because reads/writes are append-only during a transaction. Truncating back to the saved length discards exactly the entries added by the retried branch.

### 6.4 Retry in Or-Else Context

When `retry` is called inside an or-else branch:
- If there is a pending second branch (or-else checkpoint on stack): rollback and try the second branch
- If there is no checkpoint (top-level retry): block on read set TVars as in §5.2

This distinction is handled by checking the `or_else_stack` depth when processing a `retry`.

---

## 7. GC Interaction

### 7.1 GC Safety During Transactions

Transactions create temporary state (read/write sets, buffered values) that must be visible to the GC to prevent premature collection of referenced heap objects.

**Root enumeration:** The `TxnDescriptor` is stack-allocated in the `atomically` handler's call frame. The GC's root enumeration already walks the data stack and local variables. Transaction state must be enumerable:

- **Read set entries** contain `tvar_ptr` (heap pointer — must be traced) and `version` (non-pointer — ignored)
- **Write set entries** contain `tvar_ptr` (traced) and `new_value` (tagged value — traced if HEAP-tagged)

**Implementation:** The transaction descriptor registers its read/write set arrays as supplementary GC roots for the duration of the transaction. This uses the same mechanism as handler frames — the GC already supports walking a chain of root sources.

```
fn txn_begin(descriptor: &mut TxnDescriptor):
    ...
    gc_register_roots(&descriptor.read_set)
    gc_register_roots(&descriptor.write_set)

fn txn_end(descriptor: &mut TxnDescriptor):
    gc_unregister_roots(&descriptor.read_set)
    gc_unregister_roots(&descriptor.write_set)
```

### 7.2 GC During Retry Blocking

When a transaction is parked on `retry`:
- The read/write sets have been cleared (§5.2 step 3)
- The watch set (list of TVar pointers) must remain a GC root — otherwise the TVars could be collected while the task is waiting on them
- The `Waiter` structure holds strong references to the watched TVars, keeping them alive

When the GC runs while a task is parked:
1. The parked task's stack is scanned (as with any suspended task)
2. The `Waiter`'s TVar references are traced, keeping the TVars and their contents alive
3. The parked task itself is kept alive by the task group structure (structured concurrency — parent holds reference to child tasks)

### 7.3 GC During Transaction Abort/Restart

On abort:
- Buffered `new_value` entries in the write set become garbage. They will be collected by the next GC cycle.
- For affine values that were `tvar-take`'d and buffered: the buffered value is discarded on abort. **This does not violate affine safety** — the value was never actually removed from the TVar (writes are buffered). The committed TVar still holds the original value. The buffered copy was a speculative read, not an ownership transfer.

**Key invariant:** Affine ownership transfer only happens at commit. Before commit, the TVar's committed `value` and `occupied` fields are unchanged. The write set holds speculative copies that have no ownership significance until commit.

### 7.4 GC and TVar Lifetime

TVars are heap objects traced by the mark-sweep GC like any other. A TVar is collectible when:
1. All handles (`TVar[T]` values in user code) are closed or unreachable
2. No transaction's read/write/watch set references it
3. No wait queue references it

Condition (1) is enforced by affine handle discipline. Conditions (2) and (3) are transient — once in-flight transactions complete, the references are released.

### 7.5 No STM-Specific GC Logic

The mark-sweep GC does **not** need special STM awareness. It traces through:
- TVar heap objects (kind 0x0D) — traces the `value` field like any heap object field
- Transaction descriptors — registered as supplementary roots
- Waiter structures — hold traced TVar pointers

No write barriers, no version-aware tracing, no special TVar finalization. The existing GC infrastructure is sufficient.

---

## 8. Contention Management

### 8.1 Problem

Under high contention (many transactions accessing the same TVars), transactions may repeatedly abort and restart, causing livelock — no transaction makes progress.

### 8.2 Strategy: Exponential Backoff

On abort, the transaction waits before restarting. The wait duration increases exponentially with consecutive aborts:

```
fn txn_abort_and_restart(descriptor: &mut TxnDescriptor):
    descriptor.abort_count += 1

    if descriptor.abort_count > 1:
        let backoff_us = min(
            BASE_BACKOFF_US * (1 << (descriptor.abort_count - 1)),
            MAX_BACKOFF_US
        )
        // Add jitter: ±25%
        let jitter = random_range(backoff_us * 3/4, backoff_us * 5/4)
        yield_for(jitter)  // cooperative yield to scheduler

    // Reset transaction state and re-execute
    descriptor.read_set.clear()
    descriptor.write_set.clear()
    descriptor.snapshot_version = atomic_load(global_clock)
    descriptor.status = ACTIVE
    re_execute_body()
```

**Constants:**
- `BASE_BACKOFF_US = 10` (10 microseconds)
- `MAX_BACKOFF_US = 1000` (1 millisecond)
- `MAX_RETRIES = 64` — after 64 consecutive aborts, raise a runtime trap (`TRAP_STM_LIVELOCK`)

### 8.3 Why Not Priority-Based or Timestamp-Based?

More sophisticated contention managers (e.g., "older transactions win," karma-based) add complexity for marginal benefit in typical workloads. Clank's STM targets correctness and simplicity. Exponential backoff with jitter is well-understood, effective against transient contention, and trivial to implement.

If real workloads demonstrate persistent livelock, a future iteration can introduce a priority scheme where transactions accumulate "karma" (work done) and higher-karma transactions win conflicts. This requires no structural change — only the abort-and-restart path would differ.

### 8.4 Abort Counter Reset

The `abort_count` resets to 0 on successful commit. It does not carry over across calls to `atomically` — each `atomically` invocation starts fresh.

---

## 9. Effect Handler Integration

### 9.1 How `atomically` Works as an Effect Handler

The `stm` effect is discharged by `atomically`, which installs a handler using the existing effect system machinery:

```
atomically(body):
    HANDLE_PUSH handler_table for <stm>
    let descriptor = TxnDescriptor::new(atomic_load(global_clock))
    gc_register_roots(descriptor)

    loop:
        let result = try execute body()
        match result:
            Ok(value):
                match txn_commit(descriptor):
                    Ok(_):
                        gc_unregister_roots(descriptor)
                        HANDLE_POP
                        return value
                    Err(Abort):
                        txn_abort_and_restart(descriptor)
                        continue

            Retry:
                txn_retry(descriptor)  // blocks, then restarts
                continue

            Exception(e):
                // Exceptions abort the transaction and propagate
                gc_unregister_roots(descriptor)
                HANDLE_POP
                raise e
```

### 9.2 Handler Table

The `stm` handler table maps effect operations to transaction runtime functions:

| Operation | Handler |
|-----------|---------|
| `tvar-read` | `txn_read(descriptor, tvar)` |
| `tvar-write` | `txn_write(descriptor, tvar, value)` |
| `tvar-take` | `txn_take(descriptor, tvar)` |
| `tvar-put` | `txn_put(descriptor, tvar, value)` |
| `retry` | `txn_retry(descriptor)` — triggers restart or or-else fallback |

### 9.3 Continuation Capture

Unlike general effect handlers, the `stm` handler does **not** capture one-shot continuations for each operation. Instead, `tvar-read` and `tvar-write` are handled inline — they execute transaction runtime code and return directly to the caller. This avoids the overhead of continuation allocation for every TVar access.

The only continuation-like behavior is transaction restart (abort → re-execute body), which is implemented as a loop in the `atomically` handler, not as a captured continuation.

**Exception:** `retry` and or-else do use the effect system's ability to abort the current computation and restart from the handler. This is implemented as a non-local return (longjmp-style) to the `atomically` handler's loop, discarding the current execution. The GC handles any abandoned stack frames during the next collection.

### 9.4 Nested `atomically`

**Go implementation note:** The Go VM (`internal/vm/vm.go`) **does support** nested `atomically` calls. A nested call saves the parent write-log (`prevLog`), starts a fresh `txnWriteLog`, and on commit merges its entries back into `prevLog`. On abort it restores TVars from the nested write-log and restores `prevLog` unchanged. This provides flat nesting (inner commits are provisional until the outermost transaction commits).

**Original spec (v1 design):** Nested `atomically` calls are **not supported** in v1. If a transaction body calls `atomically`, this is a runtime error (`TRAP_STM_NESTED`). The type system should prevent this statically (the body has effect `<stm | e>` where `e` excludes `shared`, and `atomically` requires `<shared>`), but the runtime check is a safety net.

**Rationale for original design:** Nested transactions require either flat nesting (inner commits are provisional) or closed nesting (inner transactions are sub-transactions). Both add significant complexity. Clank's STM is composable — instead of nesting `atomically`, compose STM actions within a single `atomically` block.

**Implementation divergence:** The Go port chose flat nesting for simplicity and composability. The v1 spec's restriction may be revisited.

---

## 10. TVar Creation and Closing

### 10.1 tvar-new

`tvar-new` runs **outside** transactions (it produces `<shared>`, not `<stm>`):

```
fn tvar_new(initial_value: Value) -> *TVar:
    let tvar = heap_alloc(TVAR, 32)  // 32 bytes payload
    tvar.version = 0
    tvar.value = initial_value
    tvar.occupied = true
    tvar.wait_queue = empty
    return tagged(HEAP, tvar)
```

### 10.2 tvar-close

`tvar-close` consumes the TVar handle. Like `Ref`, TVar uses a handle reference count (separate from GC — this is a logical count of live handles):

```
fn tvar_close(tvar: *TVar):
    let remaining = atomic_decrement(tvar.handle_count)
    if remaining == 0:
        // No more handles — TVar becomes eligible for GC collection
        // (actual collection happens when GC finds it unreachable)
        // Wake any retrying transactions so they abort cleanly
        txn_wake_retriers_for_close(tvar)
```

The GC will collect the TVar object when it is unreachable (no handles, no transaction references, no wait queue entries).

### 10.3 TVar Clone

Cloning a TVar handle increments the handle count:

```
fn tvar_clone(tvar: *TVar) -> *TVar:
    atomic_increment(tvar.handle_count)
    return tvar  // same pointer, new handle
```

---

## 11. Complete Transaction Example (Runtime Trace)

To illustrate the runtime behavior, here is a trace of the bank transfer example:

```
-- Source:
atomically \-> {
    let bal = tvar-read &from
    tvar-write (&from, bal - 100)
    tvar-write (&to, (tvar-read &to) + 100)
}
```

**Runtime trace (no contention):**

```
1. atomically: push stm handler, alloc descriptor, snapshot_version = 42
2. tvar-read &from:
   - write_set miss, read_set miss
   - read from.version=40, from.value=500 (40 ≤ 42, ok)
   - read_set: [(from, v=40)]
   - return 500
3. tvar-write (&from, 400):
   - from already in read_set, skip implicit read
   - write_set: [(from, 400)]
4. tvar-read &to:
   - write_set miss, read_set miss
   - read to.version=41, to.value=200 (41 ≤ 42, ok)
   - read_set: [(from, v=40), (to, v=41)]
   - return 200
5. tvar-write (&to, 300):
   - to already in read_set
   - write_set: [(from, 400), (to, 300)]
6. commit:
   - acquire commit_lock
   - validate: from.version==40 ✓, to.version==41 ✓
   - new_version = atomic_increment(global_clock) = 43
   - write-back: from.value=400, from.version=43; to.value=300, to.version=43
   - release commit_lock
   - wake retriers on from, to (none waiting)
   - return result
```

**Runtime trace (contention — concurrent commit between steps 5 and 6):**

```
1-5. Same as above
6. commit:
   - acquire commit_lock
   - validate: from.version==40 ✓, to.version==44 ✗ (another txn committed to `to`)
   - release commit_lock
   - abort: clear read_set, write_set
   - backoff: abort_count=1, no delay on first abort
   - snapshot_version = atomic_load(global_clock) = 44
   - re-execute body from step 2
```

---

## 12. Summary of Runtime Components

| Component | Allocation | Lifetime | GC Interaction |
|-----------|-----------|----------|----------------|
| `TVar` heap object | Heap (GC-managed) | Until all handles closed + unreachable | Traced like any heap object |
| `TxnDescriptor` | Stack (call frame) | Duration of `atomically` call | Registered as supplementary GC root |
| Read set | Dynamic array (stack-owned) | Cleared on commit/abort/retry | Entries traced as GC roots |
| Write set | Dynamic array (stack-owned) | Cleared on commit/abort/retry | Entries traced as GC roots |
| Wait queue entries | Intrusive list (task-owned) | Parked duration | TVar pointers traced |
| Global clock | Static atomic | Program lifetime | Not a GC concern |
| Commit lock | Static spinlock | Program lifetime | Not a GC concern |

---

## 13. Design Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Concurrency control | Optimistic (no locks during execution) | Composability, no deadlocks |
| Version scheme | Global version clock | Simpler validation than per-TVar independent versions |
| Commit serialization | Global spinlock | Simple; critical section is short (no alloc/IO) |
| Write strategy | Buffered (write-back at commit) | Free abort, no undo log needed |
| Read validation | At read time (eager) + at commit time | Fail fast on doomed transactions |
| Retry blocking | TVar wait queues + async scheduler integration | Efficient (no polling); reuses existing task suspension |
| Contention management | Exponential backoff with jitter | Simple, effective, well-understood |
| GC integration | Supplementary root registration | No special GC logic; existing mark-sweep suffices |
| Nested atomically | Supported in Go port (flat nesting, write-log merge); original v1 spec disallowed it | Go port chose composability; original rationale was complexity |
| Or-else | Checkpoint + truncate | Append-only sets make rollback trivial |
