# Clank VM Instruction Set Specification

**Task:** TASK-009 (base), TASK-014 (async), TASK-088 (shared state + iterators)
**Consolidated:** TASK-091 (2026-03-19)
**Mode:** Spec
**Status:** Complete

---

## 1. Overview

This document specifies the bytecode instruction set for Clank's custom stack
VM — the Phase 1 compilation target. The VM is a stack-based interpreter that
serves as the compilation target for Clank's applicative syntax.

### Architecture Summary

- **Data stack**: Operand stack holding typed values
- **Return stack**: Call frames for word invocations and handler frames
- **Heap**: Region-managed memory for compound values (lists, records, closures)
- **Dictionary**: Word table mapping word IDs to bytecode sequences
- **Effect stack**: Active handler frames for algebraic effect dispatch
- **Module table**: Loaded modules with exported symbol tables

### Design Constraints

- Total VM implementation target: ~1500 lines
- Instruction set must be small enough to fit in agent context
- Natural compilation target for Clank's applicative syntax
- Structured error reporting at every trap point

---

## 2. Value Representation

All values on the data stack are tagged:

```
value = tag(3 bits) + payload(61 bits)

Tags:
  0b000  INT    — 61-bit signed integer (bigint if overflow)
  0b001  RAT    — heap pointer to (Int, Int) pair
  0b010  BOOL   — 0 or 1 in payload
  0b011  STR    — heap pointer to UTF-8 byte array
  0b100  BYTE   — 8-bit unsigned in payload
  0b101  UNIT   — no payload
  0b110  HEAP   — heap pointer to compound value (list, tuple, record, union, closure)
  0b111  QUOTE  — dictionary offset (pointer to bytecode sequence)
```

Heap objects carry their own type tag for runtime inspection:

```
heap_object = kind(1 byte) + size(4 bytes) + fields...

Kinds:
  0x01  LIST     — length + array of values
  0x02  TUPLE    — arity + array of values
  0x03  RECORD   — field count + (field_id, value) pairs
  0x04  UNION    — variant tag + arity + values
  0x05  CLOSURE  — captured values + bytecode offset
  0x06  BIGINT   — sign + digit count + digits
  0x07  RATIONAL — numerator(value) + denominator(value)
  0x08  FUTURE   — status byte + result slot
  0x09  SENDER   — channel_id + open flag (TASK-014)
  0x0A  RECEIVER — channel_id + open flag (TASK-014)
  0x0B  SELECT   — arm count + array of (source_type, source_ref, handler_ref) (TASK-014)
  0x0C  ITERATOR — gen_state (suspended continuation) + cleanup_fn + done_flag (TASK-088)
  0x0D  REF      — handle_count + value_slot + empty_flag + lock_word (TASK-088)
  0x0E  TVAR     — version_counter + value_slot + empty_flag (TASK-088)
  0x0F  ATOMIC   — atomic_value (Int or Bool, unboxed) (TASK-088)
```

#### GC Notes for New Heap Kinds

- **SENDER/RECEIVER/SELECT**: Managed by GC. Futures, channels, and select
  sets are heap objects. Discarding a Future does NOT cancel the child task —
  cancellation only happens through task group exit.
- **ITERATOR**: The GC traces the suspended continuation and captured state.
  Iterators are **finalization roots** — the GC must run their cleanup function
  before collecting. This ensures held resources (file handles, channel
  receivers) are released.
- **REF**: The GC traces the value slot. Handle count tracks live references.
  When all handles are closed or unreachable, the REF becomes collectible.
  For affine T, the value slot may be empty (after take); empty slots are
  not traced.
- **TVAR**: Similar to REF. The version counter is untraced. During
  transactions, read/write-set entries hold temporary TVar references as
  stack-allocated roots.
- **ATOMIC**: Contains an unboxed primitive (Int or Bool). No internal heap
  pointers to trace. Unrestricted — no handle counting or finalization.

---

## 3. Encoding Format

### Bytecode Layout

Instructions are variable-length, 1-byte opcode + 0-3 bytes operand:

```
instruction = opcode(1 byte) [ operand ]

Operand formats:
  none     — opcode alone (stack ops, arithmetic)
  u8       — 1-byte unsigned (small constants, local indices)
  u16      — 2-byte unsigned big-endian (word IDs, jump offsets)
  u32      — 4-byte unsigned big-endian (heap addresses, string table indices)
```

### Module Binary Format

```
module = header + string_table + word_table + bytecode_section

header:
  magic        4 bytes   "CLNK"
  version      2 bytes   major.minor
  word_count   2 bytes   number of words in module
  str_count    2 bytes   number of string table entries
  code_size    4 bytes   total bytecode size

string_table:
  entry*       (u16 length + UTF-8 bytes) repeated str_count times

word_table:
  entry*       (u16 name_str_idx + u32 code_offset + u16 code_length + u8 flags)
  flags:  bit 0 = public, bit 1 = has_handler

bytecode_section:
  raw bytes    code_size bytes of instructions
```

---

## 4. Instruction Set

### 4.1 Stack Manipulation

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `NOP`  | 0x00 | —       | ( -- )       | No operation |
| `DUP`  | 0x01 | —       | (a -- a a)   | Duplicate top |
| `DROP` | 0x02 | —       | (a -- )      | Discard top |
| `SWAP` | 0x03 | —       | (a b -- b a) | Swap top two |
| `ROT`  | 0x04 | —       | (a b c -- b c a) | Rotate three |
| `OVER` | 0x05 | —       | (a b -- a b a) | Copy second to top |
| `PICK` | 0x06 | u8:n    | (... -- ... v) | Copy nth element to top (0-indexed from top) |
| `ROLL` | 0x07 | u8:n    | (... -- ...) | Move nth element to top, shift others down |

### 4.2 Constants / Literals

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `PUSH_INT` | 0x10 | u8:val | ( -- Int) | Push small int (0-255) |
| `PUSH_INT16` | 0x11 | u16:val | ( -- Int) | Push 16-bit int |
| `PUSH_INT32` | 0x12 | u32:val | ( -- Int) | Push 32-bit int |
| `PUSH_TRUE` | 0x13 | — | ( -- Bool) | Push true |
| `PUSH_FALSE` | 0x14 | — | ( -- Bool) | Push false |
| `PUSH_UNIT` | 0x15 | — | ( -- ()) | Push unit |
| `PUSH_STR` | 0x16 | u16:idx | ( -- Str) | Push string from string table |
| `PUSH_BYTE` | 0x17 | u8:val | ( -- Byte) | Push byte literal |
| `PUSH_RAT` | 0x18 | u32:idx | ( -- Rat) | Push rational from constant pool |

### 4.3 Arithmetic

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `ADD`  | 0x20 | —       | (a b -- c) | a + b |
| `SUB`  | 0x21 | —       | (a b -- c) | a - b |
| `MUL`  | 0x22 | —       | (a b -- c) | a * b |
| `DIV`  | 0x23 | —       | (a b -- c) | a / b (traps on zero) |
| `MOD`  | 0x24 | —       | (a b -- c) | a % b (traps on zero) |
| `NEG`  | 0x25 | —       | (a -- b)   | -a |

Arithmetic is polymorphic over Int and Rat. The VM dispatches on the
tag of the top two values. Int/Rat mixed operations promote to Rat.
Division of two Ints produces a Rat.

### 4.4 Comparison

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `EQ`   | 0x28 | —       | (a b -- Bool) | Structural equality |
| `NEQ`  | 0x29 | —       | (a b -- Bool) | Not equal |
| `LT`   | 0x2A | —       | (a b -- Bool) | Less than |
| `GT`   | 0x2B | —       | (a b -- Bool) | Greater than |
| `LTE`  | 0x2C | —       | (a b -- Bool) | Less or equal |
| `GTE`  | 0x2D | —       | (a b -- Bool) | Greater or equal |

### 4.5 Logic

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `AND`  | 0x30 | —       | (Bool Bool -- Bool) | Logical and |
| `OR`   | 0x31 | —       | (Bool Bool -- Bool) | Logical or |
| `NOT`  | 0x32 | —       | (Bool -- Bool) | Logical not |

### 4.6 Control Flow

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `JMP`    | 0x38 | u16:offset | ( -- ) | Unconditional jump (relative, signed) |
| `JMP_IF` | 0x39 | u16:offset | (Bool -- ) | Jump if true |
| `JMP_UNLESS` | 0x3A | u16:offset | (Bool -- ) | Jump if false |
| `CALL`   | 0x3B | u16:word_id | ( -- ) | Call word by dictionary ID |
| `CALL_DYN` | 0x3C | — | (Quote -- ) | Call quotation from stack |
| `RET`    | 0x3D | — | ( -- ) | Return from word |
| `TAIL_CALL` | 0x3E | u16:word_id | ( -- ) | Tail call (reuse frame) |
| `TAIL_CALL_DYN` | 0x3F | — | (Quote -- ) | Tail call quotation |

#### Conditional (`if` word)

The Clank `if` word compiles to:

```
# Stack: (Bool, Quote_then, Quote_else)
ROT           # move Bool to top: (Quote_then, Quote_else, Bool)
JMP_IF +3     # if true, skip to THEN
SWAP          # put else-branch on top
CALL_DYN      # execute else quotation
JMP +2        # skip THEN
DROP          # discard else quotation
CALL_DYN      # execute then quotation
```

Alternatively, the compiler can inline quotation bodies and emit direct
jumps when the quotation is a literal (not a stack value).

#### Pattern Matching (`match`)

Match compiles to a chain of tag tests and conditional jumps:

```
# For: match { Circle(r) => ..., Rect(w, h) => ... }
DUP                    # keep value for inspection
VARIANT_TAG            # push variant tag index
PUSH_INT 0             # Circle = tag 0
EQ
JMP_UNLESS else_rect
VARIANT_FIELD 0        # extract r
<circle body bytecode>
JMP match_end
else_rect:
VARIANT_FIELD 0        # extract w
VARIANT_FIELD 1        # extract h (from original, re-extracted)
<rect body bytecode>
match_end:
```

### 4.7 Quotations and Closures

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `QUOTE` | 0x40 | u16:code_offset | ( -- Quote) | Push quotation (pointer to bytecode) |
| `CLOSURE` | 0x41 | u16:code_offset, u8:capture_count | (v1..vN -- Closure) | Capture N values + bytecode into closure |

A quotation `[1 2 +]` compiles to:

```
QUOTE offset_of_body
```

Where the body at `offset_of_body` contains: `PUSH_INT 1, PUSH_INT 2, ADD, RET`

When `CALL_DYN` encounters a `QUOTE`, it pushes a new frame and jumps to the
bytecode. When it encounters a `CLOSURE`, it first pushes the captured values
onto the stack, then jumps.

### 4.8 Local Variables

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `LOCAL_GET` | 0x48 | u8:idx | ( -- v) | Push local variable onto stack |
| `LOCAL_SET` | 0x49 | u8:idx | (v -- ) | Pop stack into local variable |

Locals are stored in the call frame. The `let x = expr in body` construct
compiles to:

```
<expr bytecode>     # leaves value on stack
LOCAL_SET 0         # store in local slot 0
<body bytecode>     # LOCAL_GET 0 wherever x appears
```

Maximum 256 locals per frame (u8 index).

### 4.9 Heap / Compound Values

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `LIST_NEW` | 0x50 | u8:count | (v1..vN -- [v1..vN]) | Create list from top N values |
| `LIST_LEN` | 0x51 | — | ([a] -- [a] Int) | Push list length (non-destructive) |
| `LIST_HEAD` | 0x52 | — | ([a] -- a) | Pop list, push first element |
| `LIST_TAIL` | 0x53 | — | ([a] -- [a]) | Pop list, push tail |
| `LIST_CONS` | 0x54 | — | (a [a] -- [a]) | Prepend element |
| `LIST_CAT`  | 0x55 | — | ([a] [a] -- [a]) | Concatenate two lists |
| `LIST_IDX`  | 0x56 | — | ([a] Int -- a) | Index into list (traps on bounds) |
| `TUPLE_NEW` | 0x58 | u8:arity | (v1..vN -- (v1,..,vN)) | Create tuple |
| `TUPLE_GET` | 0x59 | u8:idx | ((a,b,..) -- v) | Extract tuple element |
| `RECORD_NEW` | 0x5A | u8:field_count | (k1 v1 .. kN vN -- {k1:v1,..}) | Create record |
| `RECORD_GET` | 0x5B | u16:field_id | ({..} -- v) | Get record field |
| `RECORD_SET` | 0x5C | u16:field_id | (v {..} -- {..}) | Functional update (new record) |
| `UNION_NEW` | 0x5E | u8:tag, u8:arity | (v1..vN -- Union) | Create union variant |
| `VARIANT_TAG` | 0x5F | — | (Union -- Union Int) | Push variant's tag index |
| `VARIANT_FIELD` | 0x60 | u8:idx | (Union -- v) | Extract variant field |

### 4.10 String Operations

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `STR_CAT` | 0x62 | — | (Str Str -- Str) | Concatenate strings |
| `STR_LEN` | 0x63 | — | (Str -- Int) | String length (UTF-8 codepoints) |
| `STR_SPLIT` | 0x64 | — | (Str Str -- [Str]) | Split on delimiter |
| `STR_JOIN` | 0x65 | — | ([Str] Str -- Str) | Join with delimiter |
| `STR_TRIM` | 0x66 | — | (Str -- Str) | Trim whitespace |
| `TO_STR` | 0x67 | — | (a -- Str) | Convert any value to string |

### 4.11 Effect Dispatch

Effects are handled via a handler stack. When an effect operation is
performed, the VM searches the handler stack for a matching handler.

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `HANDLE_PUSH` | 0x70 | u16:effect_id, u16:handler_offset | ( -- ) | Install handler frame |
| `HANDLE_POP` | 0x71 | — | ( -- ) | Remove topmost handler frame |
| `EFFECT_PERFORM` | 0x72 | u16:effect_id, u8:op_idx | (args.. -- results..) | Perform effect operation |
| `RESUME` | 0x73 | — | (value -- ) | Resume captured continuation |
| `RESUME_DISCARD` | 0x74 | — | ( -- ) | Discard continuation (abort) |

#### Handler Frame Structure

```
handler_frame:
  effect_id     u16    — which effect this handles
  return_clause u16    — bytecode offset for return clause
  op_clauses    array  — (op_idx → bytecode_offset) for each operation
  saved_stack   ptr    — stack pointer at handler installation
  continuation  ptr    — saved return address (for resume)
```

#### Effect Dispatch Semantics

When `EFFECT_PERFORM(eid, op)` executes:

1. Walk the handler stack from top to bottom
2. Find the first frame where `effect_id == eid`
3. If not found: **trap** (unhandled effect)
4. Capture the continuation: save the current data stack (above the
   handler's `saved_stack`) and the return stack frames above the handler
5. Push the continuation as a value (for `resume` parameter)
6. Push the operation's arguments
7. Jump to `op_clauses[op]` in the handler

When `RESUME` executes:

1. Pop the continuation value and the resume argument
2. Restore the saved stack and return frames
3. Push the resume argument as the "return value" of the effect operation
4. Continue execution from where the effect was performed

When `RESUME_DISCARD` executes:

1. Pop and discard the continuation
2. Continue in the handler (used for abort/fail semantics)

#### Example: `handle (div n d) { return x -> some x, raise _ resume _ -> none }`

```
# Install handler for exn effect
HANDLE_PUSH exn_id, handler_code
# Execute body
LOCAL_GET 0           # n
LOCAL_GET 1           # d
CALL div_word_id
# Return clause (reached if body completes normally)
HANDLE_POP
CALL some_word_id     # wrap in Some
JMP end

handler_code:
# op clause for raise (op_idx=0)
.op_raise:
  DROP                # discard error value (_)
  DROP                # discard continuation (_)
  CALL none_word_id   # push None
  # falls through to end

end:
```

### 4.12 Module Loading

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `MOD_LOAD` | 0x80 | u16:mod_name_idx | ( -- ) | Load module by name (from string table) |
| `MOD_CALL` | 0x81 | u16:mod_id, u16:word_id | ( -- ) | Call word in external module |
| `MOD_IMPORT` | 0x82 | u16:mod_id, u16:word_id, u16:local_id | ( -- ) | Bind external word to local dictionary slot |

Module loading is lazy — `MOD_LOAD` registers the module path but does not
execute its body. The first `MOD_CALL` to a word in that module triggers
compilation if needed.

`use std.io (print)` compiles to:

```
MOD_LOAD str_idx("std.io")        # register module
MOD_IMPORT mod_id, print_id, 42   # bind print to local word slot 42
```

After import, `print` compiles to `CALL 42` (the local slot), which
the dictionary resolves to the imported word's bytecode.

### 4.13 Type/Runtime Checks

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `TYPE_CHECK` | 0x88 | u8:tag | (v -- v) | Assert top value has given type tag; trap if not |
| `REFINE_CHECK` | 0x89 | u16:pred_offset | (v -- v) | Run refinement predicate; trap if false |
| `ASSERT_STACK` | 0x8A | u8:min_depth | ( -- ) | Assert stack has at least N elements |
| `AFFINE_MOVE` | 0x8B | u8:local_idx | ( -- v) | Move affine value from local (invalidates slot) |
| `AFFINE_CLONE` | 0x8C | — | (a -- a a) | Clone value (only if type implements Clone) |

`AFFINE_MOVE` reads a local and marks the slot as consumed. Any subsequent
access to that slot traps with "use after move." This enforces affine
semantics at runtime (the type checker catches most violations at compile
time; the VM check is a safety net).

### 4.14 I/O (Primitives)

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `IO_PRINT` | 0x90 | — | (Str -- ()) | Print string to stdout |
| `IO_READ_LN` | 0x91 | — | ( -- Str) | Read line from stdin |
| `IO_READ_F` | 0x92 | — | (Str -- Str) | Read file contents |
| `IO_WRITE_F` | 0x93 | — | (Str Str -- ()) | Write string to file (path, contents) |
| `IO_ENV` | 0x94 | — | (Str -- Str?) | Read environment variable |
| `IO_NOW` | 0x95 | — | ( -- Int) | Current unix timestamp |

I/O instructions check that an `io` capability is in scope (installed by
the runtime at program entry). Without it, they trap. This enforces the
effect system's guarantee that non-`io` code cannot perform I/O.

### 4.15 VM Control

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `HALT` | 0xF0 | — | ( -- ) | Stop execution (normal exit) |
| `TRAP` | 0xF1 | u16:err_code | ( -- ) | Abort with structured error |
| `DEBUG` | 0xF2 | — | ( -- ) | Emit stack snapshot as JSON diagnostic |

### 4.16 Async / Concurrency (TASK-014)

Structured concurrency primitives: task groups, channels, select, cancellation.
Async opcodes are the runtime's implementation of the `async` built-in effect,
using dedicated opcodes (like I/O) to skip handler-stack search overhead.

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `TASK_SPAWN` | 0xA0 | — | (Closure -- Future) | Spawn task from closure; captured affine values move into child |
| `TASK_AWAIT` | 0xA1 | — | (Future -- value) | Block until future resolves; cancellation point |
| `TASK_YIELD` | 0xA2 | — | ( -- ) | Cooperative yield to scheduler; cancellation point |
| `TASK_SLEEP` | 0xA3 | — | (Int -- ) | Sleep for N milliseconds; cancellation point |
| `TASK_GROUP_ENTER` | 0xA4 | — | ( -- ) | Enter a task group scope (push group frame on return stack) |
| `TASK_GROUP_EXIT` | 0xA5 | — | ( -- ) | Exit task group (cancel+await children; propagate first child failure) |
| `CHAN_NEW` | 0xA6 | — | (Int -- Sender Receiver) | Create bounded channel with given capacity |
| `CHAN_SEND` | 0xA7 | — | (Sender value -- ) | Send value (blocks if full); cancellation point |
| `CHAN_RECV` | 0xA8 | — | (Receiver -- value) | Receive value (blocks if empty); cancellation point |
| `CHAN_TRY_RECV` | 0xA9 | — | (Receiver -- Option) | Non-blocking receive; returns None if empty |
| `CHAN_CLOSE` | 0xAA | — | (Sender\|Receiver -- ) | Close channel end; pending ops on other end raise exn[ChannelClosed] |
| `SELECT_BUILD` | 0xAB | u8:arm_count | (arms.. -- SelectSet) | Build select set from arm pairs (source, handler) |
| `SELECT_WAIT` | 0xAC | — | (SelectSet -- value) | Wait on select set; fire first ready arm; cancellation point |
| `TASK_CANCEL_CHECK` | 0xAD | — | ( -- Bool) | Check cancellation flag without raising |
| `TASK_SHIELD_ENTER` | 0xAE | — | ( -- ) | Enter uncancellable section (defer cancellation) |
| `TASK_SHIELD_EXIT` | 0xAF | — | ( -- ) | Exit uncancellable section (observe deferred cancellation) |

### 4.17 Ref Operations (TASK-088)

Shared mutable state cells. `REF_READ`/`REF_WRITE` dispatch on affine type
at runtime: for affine T, read performs take (empties cell) and write performs
put (fills empty cell). For unrestricted T, read copies and write overwrites.

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `REF_NEW` | 0xB0 | — | (v -- Ref) | Allocate Ref cell with initial value |
| `REF_READ` | 0xB1 | — | (Ref -- Ref v) | Read value (copy for unrestricted; take for affine, traps E016 if empty) |
| `REF_WRITE` | 0xB2 | — | (v Ref -- Ref) | Write value (overwrite for unrestricted; put for affine, traps E017 if full) |
| `REF_CAS` | 0xB3 | — | (expected new Ref -- Ref Bool cur) | Compare-and-swap; pushes success flag and current value |
| `REF_MODIFY` | 0xB4 | — | (Quote Ref -- Ref v_new) | Atomic read-apply-write with pure function; internally CAS-loops |
| `REF_CLOSE` | 0xB5 | — | (Ref -- ) | Close handle; decrements handle count. Also works for TVar (dispatches on heap kind) |

Ref handles stay on the stack after read/write/cas/modify (borrowed, not consumed).
Only `REF_CLOSE` consumes the handle. `ref-swap` compiles as a `REF_READ` + `REF_CAS` loop.

### 4.18 AtomicVal Operations (TASK-088)

Lock-free atomic operations on primitive values (Int, Bool). All operations use
sequential consistency. AtomicVal is unrestricted — no handle counting or close.

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `ATOMIC_NEW` | 0xB8 | — | (v -- AtomicVal) | Allocate AtomicVal with initial value |
| `ATOMIC_LOAD` | 0xB9 | — | (AtomicVal -- AtomicVal v) | Atomic load (sequentially consistent) |
| `ATOMIC_STORE` | 0xBA | — | (v AtomicVal -- AtomicVal) | Atomic store (sequentially consistent) |
| `ATOMIC_RMW` | 0xBB | u8:op | varies | Atomic read-modify-write (op: 0=CAS, 1=ADD, 2=SUB) |

`ATOMIC_RMW` sub-operations:

| op | Name | Stack Effect | Description |
|----|------|--------------|-------------|
| 0 | CAS | (expected new AtomicVal -- AtomicVal Bool cur) | Compare-and-swap |
| 1 | ADD | (delta AtomicVal -- AtomicVal old) | Fetch-and-add (Int only) |
| 2 | SUB | (delta AtomicVal -- AtomicVal old) | Fetch-and-subtract (Int only) |

### 4.19 TVar Operations (TASK-088)

Transactional variable reads/writes. Only valid inside an STM transaction
(between `STM_ATOMIC` and end of transaction body); executing outside traps.
TVar handles use the same borrow convention as Ref. `REF_CLOSE` (0xB5)
also accepts TVar handles (dispatches on heap kind 0x0E).

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `TVAR_NEW` | 0xBC | — | (v -- TVar) | Allocate TVar with initial value (valid outside transactions) |
| `TVAR_READ` | 0xBD | — | (TVar -- TVar v) | Read TVar; records in read-set. For affine T: take (traps E018 if empty) |
| `TVAR_WRITE` | 0xBE | — | (v TVar -- TVar) | Write TVar; buffers in write-set. For affine T: put (traps E019 if full) |

### 4.20 Iterator Operations (TASK-088)

Async iterator protocol with dedicated opcodes for hot-path performance
(bypasses handler-stack search). `ITER_NEXT` is a cancellation point.

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `ITER_NEW` | 0xC0 | — | (Closure Cleanup -- Iter) | Create iterator from generator closure + cleanup function |
| `ITER_NEXT` | 0xC1 | — | (Iter -- Iter value) | Advance iterator; push next value. Traps E021 if exhausted |
| `ITER_CLOSE` | 0xC2 | — | (Iter -- ) | Close iterator, run cleanup function; consumes handle |

#### `for` Loop Compilation

```
for x in expr { body }
```

compiles to:

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

### 4.21 STM Control (TASK-088)

Software Transactional Memory transaction lifecycle. `STM_ATOMIC` uses
`body_len` (not handler push/pop) because transactions have different
semantics: silent restarts on validation failure, `retry` suspends the
entire body, and transaction metadata has a separate lifecycle.

| Opcode | Hex  | Operand | Stack Effect | Description |
|--------|------|---------|--------------|-------------|
| `STM_ATOMIC` | 0xC4 | u16:body_len | ( -- ) | Begin transaction; execute body, validate and commit on completion |
| `STM_RETRY` | 0xC5 | — | ( -- ) | Abort transaction; re-execute when any read-set TVar changes |
| `STM_OR_ELSE` | 0xC6 | u16:alt_offset | ( -- ) | Mark alternative path; if primary retries, jump to alt_offset |

#### STM Frame Structure

```
stm_frame:
  read_set      []     — (tvar_ptr, version, value) entries
  write_set     []     — (tvar_ptr, new_value) entries
  retry_set     []     — tvar_ptrs to watch (populated on retry)
  body_start    u32    — IP of transaction body start
  body_len      u16    — length of body bytecode
  or_else_alt   u32?   — optional alternative body IP (from STM_OR_ELSE)
```

#### `atomically` Compilation Example

```
atomically { tvar-write(&v, tvar-read(&v) + 1) }
```

compiles to:

```
STM_ATOMIC 10               # begin transaction (body = 10 bytes)
  LOCAL_GET 0                # push TVar (local slot 0)
  TVAR_READ                  # (TVar -- TVar val)
  PUSH_INT 1
  ADD                        # (TVar new_val)
  SWAP                       # (new_val TVar)
  TVAR_WRITE                 # (new_val TVar -- TVar)
  DROP                       # consume TVar from stack
                             # end of body — validate and commit
```

---

## 5. Trap Codes (Structured Errors)

Every trap emits a JSON diagnostic:

```json
{
  "code": "E003",
  "message": "division by zero",
  "location": {"module": "main", "word": "safe-div", "offset": 12},
  "stack": ["Int(5)", "Int(0)"]
}
```

| Code | Name | Trigger | Source |
|------|------|---------|--------|
| E001 | STACK_UNDERFLOW | Pop from empty stack | TASK-009 |
| E002 | TYPE_MISMATCH | TYPE_CHECK fails or arithmetic on wrong types | TASK-009 |
| E003 | DIV_ZERO | DIV or MOD with zero divisor | TASK-009 |
| E004 | INDEX_BOUNDS | LIST_IDX out of range | TASK-009 |
| E005 | UNHANDLED_EFFECT | EFFECT_PERFORM with no matching handler | TASK-009 |
| E006 | USE_AFTER_MOVE | AFFINE_MOVE on consumed local | TASK-009 |
| E007 | REFINEMENT_FAIL | REFINE_CHECK predicate returns false | TASK-009 |
| E008 | STACK_OVERFLOW | Return stack exceeds depth limit | TASK-009 |
| E009 | MOD_NOT_FOUND | MOD_LOAD cannot locate module | TASK-009 |
| E010 | WORD_NOT_FOUND | CALL references undefined word | TASK-009 |
| E011 | TASK_CANCELLED | Cancellation observed at yield point | TASK-014 |
| E012 | CHANNEL_CLOSED | Send/recv on closed channel | TASK-014 |
| E013 | FUTURE_CONSUMED | Await on already-consumed future | TASK-014 |
| E014 | TASK_GROUP_CHILD_FAILED | Child task failed, propagated to parent | TASK-014 |
| E015 | SELECT_EMPTY | Select with zero arms | TASK-014 |
| E016 | REF_EMPTY | REF_READ (take mode) on empty affine cell | TASK-088 |
| E017 | REF_FULL | REF_WRITE (put mode) on non-empty affine cell | TASK-088 |
| E018 | TVAR_EMPTY | TVAR_READ (take mode) on empty affine TVar | TASK-088 |
| E019 | TVAR_FULL | TVAR_WRITE (put mode) on non-empty affine TVar | TASK-088 |
| E020 | STM_CONFLICT | Transaction validation failed (internal; triggers automatic retry) | TASK-088 |
| E021 | ITER_DONE | Iterator exhausted (normal termination signal) | TASK-088 |
| E022 | ITER_CLOSED | Operation on already-closed iterator | TASK-088 |

---

## 6. Execution Semantics

### 6.1 Calling Convention

The applicative syntax requires a defined calling convention.

```
Arguments are pushed left-to-right. The callee pops them.
Return values are pushed onto the stack.
```

For `f(a, b, c)`:
```
<evaluate a>     # push a
<evaluate b>     # push b
<evaluate c>     # push c
CALL f_word_id   # f pops (a, b, c), pushes result
```

Inside the callee, arguments are available as the top N stack values at frame
entry. The compiler assigns them to locals immediately:

```
# Entry to f(x, y, z):
LOCAL_SET 2      # z (top of stack → local 2)
LOCAL_SET 1      # y → local 1
LOCAL_SET 0      # x → local 0
<body>           # uses LOCAL_GET 0/1/2 for x/y/z
RET              # result is on top of stack
```

**Note:** Arguments are popped in reverse order (last arg first) because the
stack is LIFO. The compiler reverses the assignment indices accordingly.

### Startup

1. Load the main module binary
2. Resolve `MOD_LOAD` / `MOD_IMPORT` directives
3. Install the runtime's implicit `io` handler on the handler stack
4. Find the `main` word; push a call frame; begin execution
5. On `HALT` or `RET` from `main`: exit with top-of-stack as exit code

### Call Frame Layout

```
frame:
  return_ip      u32    — instruction pointer to return to
  local_slots    [value; 256]  — local variable storage
  locals_used    u8     — number of active locals
  stack_base     u32    — data stack pointer at frame entry
```

`CALL` pushes a frame, sets IP to the word's code offset.
`RET` pops the frame, restores IP to `return_ip`.
`TAIL_CALL` reuses the current frame (resets locals, sets IP).

### Dictionary

The dictionary maps word IDs (u16) to entries:

```
dict_entry:
  name        str    — word name (for debugging/errors)
  code_offset u32    — start of bytecode
  code_length u16    — length of bytecode
  module_id   u16    — owning module
  flags       u8     — public, builtin, has_handler
```

Word IDs 0-255 are reserved for builtins. User words start at 256.

---

## 7. Example Bytecode

### 7.1 Factorial

Source:
```
factorial : (n: Int{>= 0}) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)
```

Bytecode (word ID 256, `factorial`):
```
# Prologue: bind argument
0000  LOCAL_SET 0           # n → local 0

# Condition: n == 0
0002  LOCAL_GET 0           # push n
0004  PUSH_INT 0
0006  EQ
0007  JMP_UNLESS +4         # jump to else

# Then: 1
0009  PUSH_INT 1
000B  RET

# Else: n * factorial(n - 1)
000D  LOCAL_GET 0           # push n (for multiply)
000F  LOCAL_GET 0           # push n (for n - 1)
0011  PUSH_INT 1
0013  SUB                   # n - 1
0014  CALL 256              # factorial(n - 1)
0016  MUL                   # n * factorial(n - 1)
0017  RET
```

### 7.2 FizzBuzz

Source:
```
fizzbuzz : (n: Int) -> <io> () =
  if n % 15 == 0 then print("FizzBuzz")
  else if n % 3 == 0 then print("Fizz")
  else if n % 5 == 0 then print("Buzz")
  else print(show(n))
```

Bytecode (word ID 257, `fizzbuzz`):
```
0000  LOCAL_SET 0              # n → local 0

# Check n % 15 == 0
0002  LOCAL_GET 0
0004  PUSH_INT 15
0006  MOD
0007  PUSH_INT 0
0009  EQ
000A  JMP_UNLESS check_3
000C  PUSH_STR "FizzBuzz"
000E  IO_PRINT
000F  RET

check_3:
0010  LOCAL_GET 0
0012  PUSH_INT 3
0014  MOD
0015  PUSH_INT 0
0017  EQ
0018  JMP_UNLESS check_5
001A  PUSH_STR "Fizz"
001C  IO_PRINT
001D  RET

check_5:
001E  LOCAL_GET 0
0020  PUSH_INT 5
0022  MOD
0023  PUSH_INT 0
0025  EQ
0026  JMP_UNLESS show_num
0028  PUSH_STR "Buzz"
002A  IO_PRINT
002B  RET

show_num:
002C  LOCAL_GET 0
002E  TO_STR
002F  IO_PRINT
0030  RET
```

### 7.3 Pipeline with Higher-Order Functions

Source:
```
evens-as-strings : (xs: [Int]) -> <> [Str] =
  xs |> filter(fn(x) => x % 2 == 0) |> map(show)
```

After pipeline desugaring:
```
map(filter(xs, fn(x) => x % 2 == 0), show)
```

Bytecode (word ID 258):
```
# Prologue
0000  LOCAL_SET 0              # xs → local 0

# filter(xs, fn(x) => x % 2 == 0)
0002  LOCAL_GET 0              # push xs
0004  QUOTE <is_even_body>     # push lambda (no captures)
0006  CALL <filter_id>

# map(result, show)
0008  QUOTE <show_body>        # push show function
000A  CALL <map_id>
000C  RET

# Lambda body: fn(x) => x % 2 == 0
is_even_body:
0020  LOCAL_SET 0              # x → local 0
0022  LOCAL_GET 0
0024  PUSH_INT 2
0026  MOD
0027  PUSH_INT 0
0029  EQ
002A  RET
```

### 7.4 Effect Handling — safe division with exception recovery

Source:
```
safe-div : (a: Int, b: Int) -> <> Int? =
  handle div(a, b) {
    return(x) => some(x)
    raise(_) => none
  }
```

Bytecode (word ID 259):
```
# Prologue
0000  LOCAL_SET 1              # b → local 1
0002  LOCAL_SET 0              # a → local 0

# Install handler
0004  HANDLE_PUSH exn_id, handler_at_0014

# Body: div(a, b)
0007  LOCAL_GET 0
0009  LOCAL_GET 1
000B  CALL <div_id>

# Return clause: some(result)
000D  HANDLE_POP
000E  UNION_NEW 0, 1          # Some(result)
0010  RET

# Handler clauses
handler_at_0014:
# raise handler: (error_val, continuation)
0014  DROP                    # discard continuation
0015  DROP                    # discard error value (_)
0016  UNION_NEW 1, 0          # None
0019  RET
```

### 7.5 Closure Example

Source:
```
make-adder : (n: Int) -> <> (Int) -> <> Int =
  fn(x) => x + n
```

Bytecode (word ID 260):
```
# Prologue
0000  LOCAL_SET 0              # n → local 0

# Create closure capturing n
0002  LOCAL_GET 0              # push n (to be captured)
0004  CLOSURE <adder_body>, 1  # capture 1 value
0006  RET

# Closure body: fn(x) => x + n
adder_body:
0020  # CALL_DYN pushes captured values before args
0020  LOCAL_SET 1              # n (captured) → local 1
0022  LOCAL_SET 0              # x (argument) → local 0
0024  LOCAL_GET 0
0026  LOCAL_GET 1
0028  ADD
0029  RET
```

---

## 8. Opcode Summary Table

| Range | Category | Count | Source |
|-------|----------|-------|--------|
| 0x00–0x07 | Stack manipulation | 8 | TASK-009 |
| 0x10–0x18 | Constants/literals | 9 | TASK-009 |
| 0x20–0x25 | Arithmetic | 6 | TASK-009 |
| 0x28–0x2D | Comparison | 6 | TASK-009 |
| 0x30–0x32 | Logic | 3 | TASK-009 |
| 0x38–0x3F | Control flow | 8 | TASK-009 |
| 0x40–0x41 | Quotations/closures | 2 | TASK-009 |
| 0x48–0x49 | Local variables | 2 | TASK-009 |
| 0x50–0x60 | Heap/compound values | 15 | TASK-009 |
| 0x62–0x67 | Strings | 6 | TASK-009 |
| 0x70–0x74 | Effect dispatch | 5 | TASK-009 |
| 0x80–0x82 | Module loading | 3 | TASK-009 |
| 0x88–0x8C | Type/runtime checks | 5 | TASK-009 |
| 0x90–0x95 | I/O primitives | 6 | TASK-009 |
| 0xA0–0xAF | Async/concurrency | 16 | TASK-014 |
| 0xB0–0xB5 | Ref operations | 6 | TASK-088 |
| 0xB8–0xBB | AtomicVal operations | 4 | TASK-088 |
| 0xBC–0xBE | TVar operations | 3 | TASK-088 |
| 0xC0–0xC2 | Iterator operations | 3 | TASK-088 |
| 0xC4–0xC6 | STM control | 3 | TASK-088 |
| 0xF0–0xF2 | VM control | 3 | TASK-009 |
| **Total** | | **122** | |

122 opcodes total. Sparse encoding (max 0xF2) still leaves ~45% of the
opcode space free for future extensions (optimization hints, debugging,
networking primitives).

---

## 8a. Opcode Registry

Authoritative per-opcode registry. All hex assignments are final.

| Hex  | Mnemonic | Operand | Section |
|------|----------|---------|---------|
| 0x00 | NOP | — | 4.1 |
| 0x01 | DUP | — | 4.1 |
| 0x02 | DROP | — | 4.1 |
| 0x03 | SWAP | — | 4.1 |
| 0x04 | ROT | — | 4.1 |
| 0x05 | OVER | — | 4.1 |
| 0x06 | PICK | u8:n | 4.1 |
| 0x07 | ROLL | u8:n | 4.1 |
| 0x10 | PUSH_INT | u8:val | 4.2 |
| 0x11 | PUSH_INT16 | u16:val | 4.2 |
| 0x12 | PUSH_INT32 | u32:val | 4.2 |
| 0x13 | PUSH_TRUE | — | 4.2 |
| 0x14 | PUSH_FALSE | — | 4.2 |
| 0x15 | PUSH_UNIT | — | 4.2 |
| 0x16 | PUSH_STR | u16:idx | 4.2 |
| 0x17 | PUSH_BYTE | u8:val | 4.2 |
| 0x18 | PUSH_RAT | u32:idx | 4.2 |
| 0x20 | ADD | — | 4.3 |
| 0x21 | SUB | — | 4.3 |
| 0x22 | MUL | — | 4.3 |
| 0x23 | DIV | — | 4.3 |
| 0x24 | MOD | — | 4.3 |
| 0x25 | NEG | — | 4.3 |
| 0x28 | EQ | — | 4.4 |
| 0x29 | NEQ | — | 4.4 |
| 0x2A | LT | — | 4.4 |
| 0x2B | GT | — | 4.4 |
| 0x2C | LTE | — | 4.4 |
| 0x2D | GTE | — | 4.4 |
| 0x30 | AND | — | 4.5 |
| 0x31 | OR | — | 4.5 |
| 0x32 | NOT | — | 4.5 |
| 0x38 | JMP | u16:offset | 4.6 |
| 0x39 | JMP_IF | u16:offset | 4.6 |
| 0x3A | JMP_UNLESS | u16:offset | 4.6 |
| 0x3B | CALL | u16:word_id | 4.6 |
| 0x3C | CALL_DYN | — | 4.6 |
| 0x3D | RET | — | 4.6 |
| 0x3E | TAIL_CALL | u16:word_id | 4.6 |
| 0x3F | TAIL_CALL_DYN | — | 4.6 |
| 0x40 | QUOTE | u16:code_offset | 4.7 |
| 0x41 | CLOSURE | u16:code_offset, u8:capture_count | 4.7 |
| 0x48 | LOCAL_GET | u8:idx | 4.8 |
| 0x49 | LOCAL_SET | u8:idx | 4.8 |
| 0x50 | LIST_NEW | u8:count | 4.9 |
| 0x51 | LIST_LEN | — | 4.9 |
| 0x52 | LIST_HEAD | — | 4.9 |
| 0x53 | LIST_TAIL | — | 4.9 |
| 0x54 | LIST_CONS | — | 4.9 |
| 0x55 | LIST_CAT | — | 4.9 |
| 0x56 | LIST_IDX | — | 4.9 |
| 0x58 | TUPLE_NEW | u8:arity | 4.9 |
| 0x59 | TUPLE_GET | u8:idx | 4.9 |
| 0x5A | RECORD_NEW | u8:field_count | 4.9 |
| 0x5B | RECORD_GET | u16:field_id | 4.9 |
| 0x5C | RECORD_SET | u16:field_id | 4.9 |
| 0x5E | UNION_NEW | u8:tag, u8:arity | 4.9 |
| 0x5F | VARIANT_TAG | — | 4.9 |
| 0x60 | VARIANT_FIELD | u8:idx | 4.9 |
| 0x62 | STR_CAT | — | 4.10 |
| 0x63 | STR_LEN | — | 4.10 |
| 0x64 | STR_SPLIT | — | 4.10 |
| 0x65 | STR_JOIN | — | 4.10 |
| 0x66 | STR_TRIM | — | 4.10 |
| 0x67 | TO_STR | — | 4.10 |
| 0x70 | HANDLE_PUSH | u16:effect_id, u16:handler_offset | 4.11 |
| 0x71 | HANDLE_POP | — | 4.11 |
| 0x72 | EFFECT_PERFORM | u16:effect_id, u8:op_idx | 4.11 |
| 0x73 | RESUME | — | 4.11 |
| 0x74 | RESUME_DISCARD | — | 4.11 |
| 0x80 | MOD_LOAD | u16:mod_name_idx | 4.12 |
| 0x81 | MOD_CALL | u16:mod_id, u16:word_id | 4.12 |
| 0x82 | MOD_IMPORT | u16:mod_id, u16:word_id, u16:local_id | 4.12 |
| 0x88 | TYPE_CHECK | u8:tag | 4.13 |
| 0x89 | REFINE_CHECK | u16:pred_offset | 4.13 |
| 0x8A | ASSERT_STACK | u8:min_depth | 4.13 |
| 0x8B | AFFINE_MOVE | u8:local_idx | 4.13 |
| 0x8C | AFFINE_CLONE | — | 4.13 |
| 0x90 | IO_PRINT | — | 4.14 |
| 0x91 | IO_READ_LN | — | 4.14 |
| 0x92 | IO_READ_F | — | 4.14 |
| 0x93 | IO_WRITE_F | — | 4.14 |
| 0x94 | IO_ENV | — | 4.14 |
| 0x95 | IO_NOW | — | 4.14 |
| 0xA0 | TASK_SPAWN | — | 4.16 |
| 0xA1 | TASK_AWAIT | — | 4.16 |
| 0xA2 | TASK_YIELD | — | 4.16 |
| 0xA3 | TASK_SLEEP | — | 4.16 |
| 0xA4 | TASK_GROUP_ENTER | — | 4.16 |
| 0xA5 | TASK_GROUP_EXIT | — | 4.16 |
| 0xA6 | CHAN_NEW | — | 4.16 |
| 0xA7 | CHAN_SEND | — | 4.16 |
| 0xA8 | CHAN_RECV | — | 4.16 |
| 0xA9 | CHAN_TRY_RECV | — | 4.16 |
| 0xAA | CHAN_CLOSE | — | 4.16 |
| 0xAB | SELECT_BUILD | u8:arm_count | 4.16 |
| 0xAC | SELECT_WAIT | — | 4.16 |
| 0xAD | TASK_CANCEL_CHECK | — | 4.16 |
| 0xAE | TASK_SHIELD_ENTER | — | 4.16 |
| 0xAF | TASK_SHIELD_EXIT | — | 4.16 |
| 0xB0 | REF_NEW | — | 4.17 |
| 0xB1 | REF_READ | — | 4.17 |
| 0xB2 | REF_WRITE | — | 4.17 |
| 0xB3 | REF_CAS | — | 4.17 |
| 0xB4 | REF_MODIFY | — | 4.17 |
| 0xB5 | REF_CLOSE | — | 4.17 |
| 0xB8 | ATOMIC_NEW | — | 4.18 |
| 0xB9 | ATOMIC_LOAD | — | 4.18 |
| 0xBA | ATOMIC_STORE | — | 4.18 |
| 0xBB | ATOMIC_RMW | u8:op | 4.18 |
| 0xBC | TVAR_NEW | — | 4.19 |
| 0xBD | TVAR_READ | — | 4.19 |
| 0xBE | TVAR_WRITE | — | 4.19 |
| 0xC0 | ITER_NEW | — | 4.20 |
| 0xC1 | ITER_NEXT | — | 4.20 |
| 0xC2 | ITER_CLOSE | — | 4.20 |
| 0xC4 | STM_ATOMIC | u16:body_len | 4.21 |
| 0xC5 | STM_RETRY | — | 4.21 |
| 0xC6 | STM_OR_ELSE | u16:alt_offset | 4.21 |
| 0xF0 | HALT | — | 4.15 |
| 0xF1 | TRAP | u16:err_code | 4.15 |
| 0xF2 | DEBUG | — | 4.15 |

**Free ranges:** 0x08–0x0F, 0x19–0x1F, 0x26–0x27, 0x2E–0x2F, 0x33–0x37,
0x42–0x47, 0x4A–0x4F, 0x57, 0x5D, 0x61, 0x68–0x6F, 0x75–0x7F, 0x83–0x87,
0x8D–0x8F, 0x96–0x9F, 0xB6–0xB7, 0xBF, 0xC3, 0xC7–0xEF, 0xF3–0xFF.

---

## 9. Design Rationale

### Why variable-length encoding?

Fixed-width instructions waste bytes on zero-operand stack ops (which
dominate concatenative code). Variable-length keeps bytecode compact —
important for agent context consumption. The tradeoff is slightly more
complex decode, but the opcode set is small enough that a switch-dispatch
interpreter is sufficient.

### Why effect dispatch via handler stack (not vtable)?

Handler stack search is O(depth) per effect operation, but handler
depth is typically 1-3 in practice. The handler-stack model preserves
the algebraic effect semantics exactly: handlers are scoped, nestable,
and can intercept or delegate. A vtable approach would require
re-architecting for resumable continuations.

### Why runtime affine checks?

The type checker handles most affine violations at compile time. The
`AFFINE_MOVE` runtime check is a safety net for cases that slip through
(dynamic dispatch, effects that transport values across handler boundaries).
Cost is one flag check per move — negligible.

### Why I/O as dedicated opcodes (not effect operations)?

I/O is a *primitive* effect — the runtime provides the handler, and it
cannot be overridden by user code. Dedicated opcodes avoid the overhead
of the handler-stack search for every print/read call. The `io`
capability check is a single flag test.

### Why 122 opcodes?

The original 87 opcodes covered core language operations. The 35 new
opcodes (16 async + 19 shared state/iterators) were added because each
is either a hot-path operation where handler-stack search overhead is
unacceptable (ITER_NEXT, async ops) or requires VM-level support that
cannot be expressed as library code (STM transactions, atomic RMW).
The sparse encoding (max 0xF2) still leaves ~45% of the opcode space
free for extensions.

### TASK-091 Collision Resolution

TASK-014 (async, 2026-03-14) and TASK-088 (shared state, 2026-03-19) both
independently assigned opcodes starting at 0xA0. This consolidation
resolved the conflict by:

1. **TASK-014 keeps 0xA0–0xAF** (created first, already referenced by
   compilation-strategy.md and other specs)
2. **TASK-088 relocated** to 0xB0–0xC6 with semantic grouping:
   - Ref: 0xB0–0xB5, Atomic: 0xB8–0xBB, TVar: 0xBC–0xBE,
   - Iterators: 0xC0–0xC2, STM: 0xC4–0xC6
3. **Heap kinds**: TASK-014 gets 0x09–0x0B (SENDER, RECEIVER, SELECT);
   TASK-088 gets 0x0C–0x0F (ITERATOR, REF, TVAR, ATOMIC)
4. **Trap codes**: TASK-014 keeps E011–E015; TASK-088 relocated to E016–E022
