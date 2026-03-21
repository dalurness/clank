# Clank VM — GC / Memory Management Strategy

**Task:** TASK-013
**Mode:** Research / Spec
**Date:** 2026-03-14
**Builds On:** VM instruction set (TASK-009), Affine types (TASK-007), Effect system (TASK-005), Compilation target (TASK-006)
**Design Context:** DECISION-001 chose GC over borrow checking. Affine types handle resource protocols; GC handles memory only.

---

## 1. Executive Summary

**Recommendation: Mark-sweep tracing GC with an arena allocator and optional eager drop for affine values.**

This is the simplest strategy that satisfies all constraints: fits in ~300–400 lines, handles cycles naturally, requires no compiler cooperation beyond root enumeration, and integrates cleanly with effect handler continuations. Perceus-style reuse analysis is elegant but requires significant compiler infrastructure that exceeds the 500-line budget. Pure reference counting needs cycle detection, which approaches the complexity of mark-sweep while being less general.

---

## 2. Evaluation of Strategies

### 2.1 Reference Counting

**How it works:** Each heap object carries a count of incoming references. Increment on copy, decrement on drop. Free when count reaches zero.

**Pros:**
- Deterministic deallocation — objects freed immediately when last reference dies
- Fits the affine mental model (single owner → count is usually 1)
- Incremental — no pause times

**Cons:**
- **Cycles:** Closures can capture closures, creating reference cycles. Effect handler continuations exacerbate this — a handler frame references a continuation which references stack values which may reference the handler's closure. Cycle detection (e.g., trial deletion / Bacon-Rajan) adds ~200 lines and significant complexity.
- **Overhead:** Every value copy/drop needs RC adjustment. With 3-bit tagged values, only HEAP/STR/RAT tags need RC, but the branch cost is per-operation.
- **Continuation problem:** When `EFFECT_PERFORM` captures a continuation, the entire stack snapshot becomes a single heap object. RC must be bumped for every value in the snapshot. On `RESUME`, all must be decremented. This is O(stack_depth) per effect operation.

**Verdict:** Viable but the cycle problem is a dealbreaker for a 500-line budget. Affine types reduce but don't eliminate cycles (unrestricted closures can still form them).

### 2.2 Tracing GC (Mark-Sweep)

**How it works:** Periodically pause, mark all reachable objects from roots (stack, locals, handler frames), sweep unmarked objects.

**Pros:**
- **Handles cycles naturally** — no cycle detection needed
- **Simple implementation** — mark phase is a recursive walk; sweep is a linear scan
- **No per-operation overhead** — no RC adjustments on copy/drop
- **Continuation-friendly** — captured continuations are just heap objects; traced like anything else
- **Well-understood** — decades of implementation experience

**Cons:**
- **Stop-the-world pauses** — but Clank is single-threaded (no async runtime in scope), so no concurrent mutation concern
- **Non-deterministic deallocation** — acceptable since affine types handle resource cleanup, not GC
- **Memory overhead** — dead objects persist until next collection

**Verdict:** Best fit. Simple, handles all edge cases, fits in budget.

### 2.3 Perceus-Style Reuse Analysis

**How it works:** Compiler inserts reference count operations at compile time with reuse credits. When a constructor is consumed and immediately reconstructed with the same shape, the memory is reused in-place (no allocation or deallocation).

**Pros:**
- Elegant synergy with affine types — affine values have RC=1 by construction, enabling reuse
- Near-zero GC overhead for well-typed affine programs
- Deterministic for acyclic structures

**Cons:**
- **Requires compiler cooperation:** The compiler must perform reuse analysis, insert RC operations, and track reuse tokens. This is a significant pass that doesn't exist in the Clank compiler yet.
- **Still needs cycle detection** for unrestricted values (closures)
- **Complexity:** Perceus in Koka is ~2000 lines of compiler code. Even a minimal version exceeds the 500-line budget.
- **Fragile:** Reuse analysis is sensitive to code shape — small refactors can break reuse opportunities silently.

**Verdict:** Aspirational for Phase 2 (WASM target), but too complex for Phase 1 VM.

### 2.4 Comparison Matrix

| Criterion | Ref Counting | Mark-Sweep | Perceus |
|-----------|-------------|------------|---------|
| Implementation size | ~400 lines + cycle detection | ~300 lines | ~500+ lines (compiler side) |
| Handles cycles | No (needs extra) | Yes | No (needs extra) |
| Continuation support | Complex (O(n) bump) | Free (just trace) | Complex |
| Affine synergy | Good | Moderate | Excellent |
| Deterministic free | Yes | No | Yes |
| Per-op overhead | High (RC adjustments) | None | Medium (inserted ops) |
| Fits 500-line budget | Barely | Yes | No |

---

## 3. Recommended Design: Mark-Sweep with Arena Allocation

### 3.1 Architecture Overview

```
┌─────────────────────────────────────────────────┐
│  VM                                             │
│  ┌──────────┐  ┌──────────┐  ┌───────────────┐ │
│  │ Data     │  │ Call     │  │ Handler       │ │
│  │ Stack    │  │ Frames   │  │ Frames        │ │
│  └────┬─────┘  └────┬─────┘  └───────┬───────┘ │
│       │              │                │         │
│       └──────────────┼────────────────┘         │
│                      │ roots                    │
│                      ▼                          │
│              ┌───────────────┐                  │
│              │   GC (mark-   │                  │
│              │    sweep)     │                  │
│              └───────┬───────┘                  │
│                      │                          │
│              ┌───────▼───────┐                  │
│              │  Heap Arena   │                  │
│              │  (bump alloc  │                  │
│              │   + free list)│                  │
│              └───────────────┘                  │
└─────────────────────────────────────────────────┘
```

### 3.2 Heap Layout

The heap is a contiguous byte array, subdivided into object slots. Each heap object has a uniform header:

```
┌──────────────────────────────────────────┐
│  Object Header (8 bytes)                 │
├──────┬──────┬──────┬─────────────────────┤
│ mark │ kind │ size │ (reserved/padding)  │
│ 1B   │ 1B   │ 4B   │ 2B                 │
├──────┴──────┴──────┴─────────────────────┤
│  Payload (size bytes)                    │
│  ... fields as 64-bit tagged values ...  │
└──────────────────────────────────────────┘
```

**Fields:**
- `mark` (u8): GC mark bit. 0 = white (unmarked), 1 = gray (queued), 2 = black (traced).
- `kind` (u8): Object kind (0x01–0x08, matching VM spec: LIST, TUPLE, RECORD, UNION, CLOSURE, BIGINT, RATIONAL, FUTURE).
- `size` (u32): Payload size in bytes (not including header).
- Payload: Array of 64-bit tagged values (for pointer-bearing kinds) or raw bytes (for BIGINT digits, STR content).

### 3.3 Allocator

**Primary: Bump allocator** for fast allocation. A heap pointer advances monotonically. When the heap is exhausted, trigger GC, then compact or use free list.

**Secondary: Free list** populated during sweep phase. Objects freed by sweep are added to a size-class free list for reuse. Size classes: 8, 16, 32, 64, 128, 256, 512, 1024, 2048+ bytes.

```
struct Heap {
    memory:    [u8; HEAP_SIZE],     // contiguous heap
    bump:      usize,               // next allocation point
    free_lists: [FreeNode; 9],      // one per size class
    obj_count: usize,               // live object estimate
    gc_threshold: usize,            // trigger GC when bump exceeds this
}

fn alloc(heap: &mut Heap, kind: u8, size: u32) -> HeapPtr {
    let total = 8 + size as usize;  // header + payload
    // Try free list first (best-fit within size class)
    if let Some(ptr) = free_list_pop(heap, total) {
        init_header(ptr, kind, size);
        return ptr;
    }
    // Bump allocate
    if heap.bump + total > heap.gc_threshold {
        gc_collect(heap, vm);       // triggers mark-sweep
    }
    let ptr = heap.bump;
    heap.bump += total;
    init_header(ptr, kind, size);
    ptr
}
```

### 3.4 Root Enumeration

Roots are all tagged values that could reference heap objects. A value is a root if its 3-bit tag is one of: `STR` (0b011), `HEAP` (0b110), or `RAT` (0b001).

**Root sources:**
1. **Data stack** — scan `stack[0..stack_top]`, check tag of each value
2. **Local variables** — for each call frame, scan `locals[0..locals_used]`
3. **Handler frames** — for each handler, trace `continuation` (if captured)
4. **Global dictionary** — module-level bindings

```
fn enumerate_roots(vm: &VM) -> Vec<HeapPtr> {
    let mut roots = Vec::new();
    // Data stack
    for val in &vm.stack[0..vm.sp] {
        if is_heap_ref(*val) { roots.push(heap_ptr(*val)); }
    }
    // Call frames + locals
    for frame in &vm.frames[0..vm.fp] {
        for local in &frame.locals[0..frame.locals_used] {
            if is_heap_ref(*local) { roots.push(heap_ptr(*local)); }
        }
    }
    // Handler frames (continuations)
    for handler in &vm.handlers[0..vm.hp] {
        if let Some(cont) = handler.continuation {
            roots.push(cont);
        }
    }
    // Globals
    for val in &vm.globals {
        if is_heap_ref(*val) { roots.push(heap_ptr(*val)); }
    }
    roots
}

fn is_heap_ref(val: u64) -> bool {
    let tag = val & 0x7;
    tag == 0b001 || tag == 0b011 || tag == 0b110  // RAT, STR, HEAP
}
```

### 3.5 Mark Phase

Iterative mark using an explicit work stack (avoids recursion depth issues):

```
fn mark(heap: &mut Heap, roots: &[HeapPtr]) {
    let mut worklist: Vec<HeapPtr> = Vec::new();
    // Seed with roots
    for &ptr in roots {
        if get_mark(heap, ptr) == WHITE {
            set_mark(heap, ptr, GRAY);
            worklist.push(ptr);
        }
    }
    // Process worklist
    while let Some(ptr) = worklist.pop() {
        set_mark(heap, ptr, BLACK);
        // Trace children based on object kind
        let kind = get_kind(heap, ptr);
        match kind {
            LIST | TUPLE => {
                for child in get_value_fields(heap, ptr) {
                    if is_heap_ref(child) {
                        let cp = heap_ptr(child);
                        if get_mark(heap, cp) == WHITE {
                            set_mark(heap, cp, GRAY);
                            worklist.push(cp);
                        }
                    }
                }
            }
            RECORD => {
                // Records store (field_id: u16, value: u64) pairs
                for (_, child) in get_record_fields(heap, ptr) {
                    if is_heap_ref(child) {
                        let cp = heap_ptr(child);
                        if get_mark(heap, cp) == WHITE {
                            set_mark(heap, cp, GRAY);
                            worklist.push(cp);
                        }
                    }
                }
            }
            UNION => {
                // variant tag + values
                for child in get_union_fields(heap, ptr) {
                    if is_heap_ref(child) {
                        let cp = heap_ptr(child);
                        if get_mark(heap, cp) == WHITE {
                            set_mark(heap, cp, GRAY);
                            worklist.push(cp);
                        }
                    }
                }
            }
            CLOSURE => {
                // Captured values are tagged values
                for child in get_closure_captures(heap, ptr) {
                    if is_heap_ref(child) {
                        let cp = heap_ptr(child);
                        if get_mark(heap, cp) == WHITE {
                            set_mark(heap, cp, GRAY);
                            worklist.push(cp);
                        }
                    }
                }
            }
            FUTURE => {
                // Result slot (if resolved)
                if let Some(child) = get_future_result(heap, ptr) {
                    if is_heap_ref(child) {
                        let cp = heap_ptr(child);
                        if get_mark(heap, cp) == WHITE {
                            set_mark(heap, cp, GRAY);
                            worklist.push(cp);
                        }
                    }
                }
            }
            BIGINT | RATIONAL | STR_DATA => {
                // No child pointers — leaf objects
            }
        }
    }
}
```

### 3.6 Sweep Phase

Linear scan of the heap, freeing unmarked objects:

```
fn sweep(heap: &mut Heap) {
    let mut ptr = 0;
    let mut live_count = 0;
    while ptr < heap.bump {
        let mark = get_mark(heap, ptr);
        let total = 8 + get_size(heap, ptr) as usize;
        if mark == BLACK {
            // Live — reset mark for next cycle
            set_mark(heap, ptr, WHITE);
            live_count += 1;
        } else {
            // Dead — add to free list
            free_list_push(heap, ptr, total);
        }
        ptr += total;
    }
    heap.obj_count = live_count;
    // Adjust threshold: next GC when heap is 2x live data
    heap.gc_threshold = (heap.bump * 2).min(HEAP_MAX);
}
```

### 3.7 GC Trigger Policy

GC is triggered when allocation fails or when the heap usage exceeds a dynamic threshold:

- **Initial threshold:** 64 KB (small, since Clank programs are typically short scripts)
- **Growth factor:** 2x — after each collection, threshold = 2 × live_data_size
- **Maximum heap:** Configurable, default 16 MB
- **Emergency GC:** If free list and bump allocator both fail, run GC and retry once. If still OOM, emit `TRAP` with structured error.

---

## 4. Integration with Affine Types

### 4.1 Eager Drop Optimization

While the GC handles memory, affine type information can be used as a **hint** for eager deallocation. When the compiler knows a value is affine and has been consumed (via `AFFINE_MOVE` opcode), it can emit an `EAGER_DROP` hint:

```
AFFINE_MOVE  local_idx    // moves value out of local (sets to UNIT)
EAGER_DROP                // hint: top of stack was sole reference, can free immediately
```

`EAGER_DROP` is **advisory** — the VM checks if the value is a heap pointer and if the object appears to have no other references (heuristic: the value doesn't appear elsewhere on the stack). If the check passes, the object is freed immediately without waiting for GC. If the check fails or is too expensive, the hint is ignored and GC handles it later.

This optimization is optional and can be omitted in the first implementation. It exists as a defined extension point.

### 4.2 Clone and Allocation

`AFFINE_CLONE` allocates a new heap object (deep copy of the cloned value). This uses the normal allocator path and may trigger GC. The clone is a fresh allocation — no shared structure.

### 4.3 Discard

`discard` on an affine value is semantically equivalent to drop — the value becomes unreachable and will be collected. No finalizer runs. The `EAGER_DROP` optimization applies here too.

---

## 5. Integration with Effect Handler Continuations

### 5.1 The Problem

When `EFFECT_PERFORM` fires, the VM captures a continuation: a snapshot of the data stack (from handler's `saved_stack` to current `sp`) plus the return address. This continuation is a heap object of kind CLOSURE (or a new kind CONTINUATION).

The continuation contains tagged values from the stack — any of which may be heap pointers. These must remain live as long as the continuation exists.

### 5.2 Continuation as Heap Object

```
CONTINUATION object:
  kind:     0x09 (new kind, or reuse CLOSURE)
  size:     8 * captured_count + 4 (for return_ip)
  payload:  [return_ip: u32, captured_values: [Value; n]]
```

When `EFFECT_PERFORM` executes:
1. Allocate a CONTINUATION object on the heap
2. Copy `stack[handler.saved_stack..vm.sp]` into the continuation's payload
3. Store `return_ip` in the continuation
4. The continuation is now a heap object — GC traces it like any other

When `RESUME` executes:
1. Read the continuation object
2. Restore stack values from payload
3. Jump to `return_ip`
4. The continuation object is now unreferenced (unless captured again) and will be collected

### 5.3 Multi-Shot Continuations

If a handler invokes `k` multiple times (multi-shot), each `RESUME` reads from the same continuation object. The continuation must not be freed until all references are gone — which mark-sweep handles naturally (it's still reachable from the handler's scope).

### 5.4 Discarded Continuations

When a handler doesn't invoke `k` (aborting), the continuation becomes unreachable after the handler returns. GC collects it, along with any heap objects it references. No special handling needed — this is the key advantage of tracing GC over RC for this use case.

If the continuation captured affine values, those are leaked (resource-wise). The type checker emits a warning at compile time (see affine-types.md §10.3). GC reclaims memory; resource cleanup is the programmer's responsibility via `discard k`.

---

## 6. Data Structures Summary

### 6.1 Core Types

```
const HEAP_INIT: usize = 65536;        // 64 KB initial
const HEAP_MAX: usize = 16777216;      // 16 MB max
const SIZE_CLASSES: [usize; 9] = [8, 16, 32, 64, 128, 256, 512, 1024, 2048];

// GC mark states
const WHITE: u8 = 0;  // unmarked (potentially dead)
const GRAY: u8 = 1;   // queued for tracing
const BLACK: u8 = 2;  // traced (definitely live)

struct Heap {
    memory: Vec<u8>,                    // backing store
    bump: usize,                        // bump pointer
    free_lists: [Option<usize>; 9],     // head of free list per size class
    obj_count: usize,                   // estimate of live objects
    gc_threshold: usize,                // trigger threshold
    collections: usize,                 // stats: total GC runs
    bytes_freed: usize,                 // stats: total bytes reclaimed
}

struct ObjectHeader {
    mark: u8,       // WHITE, GRAY, BLACK
    kind: u8,       // LIST, TUPLE, RECORD, UNION, CLOSURE, BIGINT, RATIONAL, FUTURE, CONTINUATION
    size: u32,      // payload size in bytes
    _pad: u16,      // alignment padding
}

struct FreeNode {
    next: Option<usize>,   // pointer to next free block in this size class
    total_size: usize,     // header + payload
}
```

### 6.2 Object Kind Constants

```
const OBJ_LIST: u8        = 0x01;
const OBJ_TUPLE: u8       = 0x02;
const OBJ_RECORD: u8      = 0x03;
const OBJ_UNION: u8       = 0x04;
const OBJ_CLOSURE: u8     = 0x05;
const OBJ_BIGINT: u8      = 0x06;
const OBJ_RATIONAL: u8    = 0x07;
const OBJ_FUTURE: u8      = 0x08;
const OBJ_CONTINUATION: u8 = 0x09;  // new: captured effect continuation
```

---

## 7. API Surface

The GC exposes a minimal API to the VM:

```
// Allocation
fn heap_alloc(heap: &mut Heap, vm: &VM, kind: u8, size: u32) -> Result<HeapPtr, TrapError>

// Read/write object fields (bounds-checked)
fn heap_get_field(heap: &Heap, ptr: HeapPtr, index: usize) -> Value
fn heap_set_field(heap: &mut Heap, ptr: HeapPtr, index: usize, val: Value)
fn heap_get_kind(heap: &Heap, ptr: HeapPtr) -> u8
fn heap_get_size(heap: &Heap, ptr: HeapPtr) -> u32

// GC control
fn gc_collect(heap: &mut Heap, vm: &VM)         // force collection
fn gc_stats(heap: &Heap) -> GcStats              // for DEBUG opcode

// Optional: eager drop hint
fn heap_try_eager_free(heap: &mut Heap, val: Value) -> bool
```

The VM calls `heap_alloc` when executing: `PUSH_STR`, `LIST_NEW`, `TUPLE_NEW`, `RECORD_NEW`, `UNION_NEW`, `CLOSURE`, `EFFECT_PERFORM` (for continuation capture), and arithmetic that overflows to BIGINT/RATIONAL.

---

## 8. Estimated Line Count

| Component | Lines |
|-----------|-------|
| Heap struct + allocator (bump + free list) | ~80 |
| Object header read/write helpers | ~40 |
| Root enumeration | ~50 |
| Mark phase (worklist-based) | ~70 |
| Sweep phase + free list population | ~40 |
| GC trigger + threshold adjustment | ~20 |
| Continuation capture/restore | ~30 |
| Eager drop hint (optional) | ~30 |
| Stats + debug support | ~20 |
| **Total** | **~380** |

Well within the 500-line budget, with room for error handling and edge cases.

---

## 9. Future Extensions (Phase 2)

These are explicitly **out of scope** for Phase 1 but noted as natural evolution paths:

1. **Generational collection:** Partition heap into young/old generations. Most Clank objects are short-lived (affine values consumed quickly). A nursery with copying collection would reduce pause times.

2. **Perceus-style reuse analysis:** Once the compiler matures, compile-time RC insertion with reuse credits could eliminate most GC pauses for well-typed affine programs. The mark-sweep GC becomes a backup for unrestricted cyclic structures only.

3. **Compaction:** The current design uses free lists for reuse but doesn't compact. Fragmentation could become an issue for long-running programs. A compacting phase (with pointer fixup) would address this.

4. **WASM GC integration:** When targeting WASM (Phase 2), Clank can use the WASM GC proposal instead of managing its own heap. The mark-sweep design maps cleanly to WASM's managed references.

---

## 10. Design Rationale

### Why not reference counting?

RC is attractive for affine types (RC=1 by default), but the killer problem is cycles. Closures in Clank are first-class and can reference each other. Effect handler continuations compound this — a continuation captures stack values that may include closures that reference other continuations. Cycle detection (Bacon-Rajan or trial deletion) adds ~200 lines of subtle code and doesn't simplify the overall design compared to mark-sweep.

### Why not Perceus?

Perceus requires the compiler to emit precise RC operations and track reuse opportunities. Clank's compiler doesn't exist yet — adding reuse analysis to a compiler that's still being designed is premature. Mark-sweep lets the GC work independently of compiler sophistication. Perceus is the right Phase 2 optimization.

### Why mark-sweep over mark-compact?

Compaction requires pointer fixup (every reference to a moved object must be updated). With 3-bit tagged values spread across stack, locals, handler frames, and heap objects, the fixup logic is complex and error-prone. Free-list reuse provides adequate fragmentation mitigation for Phase 1 program sizes.

### Why a bump allocator?

Bump allocation is the fastest possible allocator — a single pointer increment. It pairs naturally with mark-sweep: allocate fast, batch-free during sweep. The free list provides reuse of swept slots, reducing heap growth.

### Why an explicit worklist instead of recursive marking?

Recursive mark can overflow the call stack for deep object graphs (e.g., a long linked list). An explicit worklist uses heap memory (bounded by object count) and is immune to stack overflow.

### Why 64 KB initial heap?

Clank targets agent-generated scripts that are typically short-lived and small. 64 KB accommodates hundreds of objects. The 2x growth factor adapts quickly to larger programs. The 16 MB cap prevents runaway allocation.
