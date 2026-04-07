# Compilation Strategy: Applicative Syntax → Stack VM

**Task:** TASK-017
**Mode:** Spec
**Date:** 2026-03-14
**Status:** Complete

---

## 1. Executive Summary

The stack-based VM (TASK-009) works as a compilation target for the applicative
syntax (DECISION-001) **with no new opcodes required**. The existing 87-opcode
instruction set is sufficient. This document specifies how each applicative
construct compiles to stack VM bytecode.

The key insight: every stack-based VM (JVM, CPython, CLR) compiles applicative
code by the same pattern — evaluate arguments left-to-right onto the stack,
then call. Clank's VM already has the infrastructure for this.

### What Changed

The VM spec (TASK-009) was written during the concatenative-primary era. Its
examples use concatenative syntax (`dup 0 eq [drop 1] [dup 1 - factorial *] if`).
The instruction set itself is syntax-agnostic — it's a stack machine with
locals, closures, and control flow. All that changes is the **compiler's
code generation strategy**, not the VM.

### Opcodes Inventory

- **Still used, same semantics:** All 87 opcodes remain valid.
- **Used differently:** `DUP`/`DROP`/`SWAP`/`ROT`/`OVER`/`PICK`/`ROLL` are now
  compiler-internal (never generated from user syntax directly, but used for
  intermediate value management). `QUOTE` is used only for thunks in block
  desugaring; user-facing quotation syntax `[...]` no longer exists.
- **New opcodes needed:** None.

---

## 2. Calling Convention

The applicative model requires a defined calling convention. The concatenative
model was implicit (values already on the stack in the right order). The
applicative model must be explicit.

### Convention

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

---

## 3. Construct-by-Construct Desugaring

### 3.1 Function Application

Source:
```
f(x, y)
```

Bytecode:
```
LOCAL_GET <x>      # push x
LOCAL_GET <y>      # push y
CALL <f_word_id>   # call f
```

If arguments are expressions:
```
add(mul(a, b), c)
```

Bytecode:
```
# Evaluate first arg: mul(a, b)
LOCAL_GET <a>
LOCAL_GET <b>
CALL <mul_id>
# Evaluate second arg: c
LOCAL_GET <c>
# Call add
CALL <add_id>
```

### 3.2 Pipeline Operator (`|>`)

Pipeline is pure syntactic sugar. Desugaring happens before code generation.

```
x |> f              →  f(x)
x |> f(y)           →  f(x, y)
x |> f |> g(y) |> h →  h(g(f(x), y))
```

The compiler desugars `|>` during AST construction: the left-hand expression
becomes the first argument to the right-hand function call. After desugaring,
code generation sees only function applications.

**Detailed example:**

Source:
```
path |> read |> split(" ") |> len |> show |> print
```

After desugaring:
```
print(show(len(split(read(path), " "))))
```

Bytecode:
```
LOCAL_GET <path>
CALL <read_id>
PUSH_STR " "
CALL <split_id>
CALL <len_id>
CALL <show_id>
CALL <print_id>
```

This is naturally left-to-right in bytecode (inner calls evaluate first),
matching the pipeline's reading order exactly.

### 3.3 Infix Operators

Operators desugar to function calls of builtins, which the compiler can
emit as direct opcodes (no actual CALL overhead).

Source:
```
a + b * c
```

After precedence parsing (AST):
```
add(a, mul(b, c))
```

Bytecode (optimized — builtins emit opcodes directly):
```
LOCAL_GET <a>
LOCAL_GET <b>
LOCAL_GET <c>
MUL
ADD
```

The compiler maintains a table of builtins that map directly to opcodes:
`add→ADD`, `sub→SUB`, `mul→MUL`, `div→DIV`, `mod→MOD`, `eq→EQ`,
`lt→LT`, `gt→GT`, `and→AND`, `or→OR`, `not→NOT`, etc.

String concatenation (`++`) compiles to `STR_CAT`.

### 3.4 Let Bindings

Source:
```
let x = expr1
let y = expr2
body
```

Bytecode:
```
<evaluate expr1>
LOCAL_SET 0          # x = local 0
<evaluate expr2>
LOCAL_SET 1          # y = local 1
<evaluate body>     # uses LOCAL_GET 0/1 for x/y
```

`let x = e in body` (expression form) compiles identically — the `in` keyword
is syntactic, not semantic.

### 3.5 If / Then / Else

Source:
```
if cond then expr_t else expr_f
```

Bytecode:
```
<evaluate cond>       # pushes Bool
JMP_UNLESS else_lbl   # jump to else if false
<evaluate expr_t>     # then branch
JMP end_lbl           # skip else
else_lbl:
<evaluate expr_f>     # else branch
end_lbl:
```

Both branches leave exactly one value on the stack (enforced by the type
checker). This replaces the concatenative `Bool [then] [else] if` pattern —
no quotations, no `ROT`/`SWAP`, no `CALL_DYN`.

### 3.6 Lambda Expressions

Source:
```
fn(x) => x * 2
```

The compiler generates:
1. A bytecode body for the lambda at a separate code offset
2. A `CLOSURE` instruction capturing any free variables

**No free variables (combinable):**
```
fn(x) => x * 2
```

Body at offset `body_off`:
```
LOCAL_SET 0        # x = arg
LOCAL_GET 0
PUSH_INT 2
MUL
RET
```

At the use site:
```
QUOTE body_off     # no captures needed → use QUOTE (cheaper than CLOSURE)
```

**With free variables (closure):**
```
let factor = 3
let scale = fn(x) => x * factor
```

Body at offset `body_off`:
```
# On entry, captured values are pushed by CALL_DYN before args
LOCAL_SET 1        # factor (captured, pushed first)
LOCAL_SET 0        # x (argument)
LOCAL_GET 0
LOCAL_GET 1
MUL
RET
```

At the use site:
```
LOCAL_GET <factor>
CLOSURE body_off, 1    # capture 1 value (factor)
LOCAL_SET <scale>       # store closure in local
```

### 3.7 Higher-Order Function Calls

Source:
```
map(xs, fn(x) => x * 2)
```

Bytecode:
```
LOCAL_GET <xs>          # push list
QUOTE <lambda_body>     # push lambda (no captures)
CALL <map_id>           # map calls CALL_DYN on the lambda for each element
```

Inside `map`'s implementation, the function argument is called via `CALL_DYN`:
```
# map implementation (simplified)
# stack: (list, fn)
LOCAL_SET 1              # fn → local 1
LOCAL_SET 0              # list → local 0
# ... loop over list elements ...
LOCAL_GET <element>      # push current element
LOCAL_GET 1              # push fn
CALL_DYN                 # call fn(element)
# ... collect result ...
```

### 3.8 Pattern Matching

Source:
```
match shape {
  Circle(r) => r * r * 3.14159
  Rect(w, h) => w * h
}
```

Bytecode (unchanged from VM spec §4.6, but with applicative body compilation):
```
LOCAL_GET <shape>
DUP                       # keep for field extraction
VARIANT_TAG               # push tag index
PUSH_INT 0                # Circle = tag 0
EQ
JMP_UNLESS check_rect

# Circle(r) arm
VARIANT_FIELD 0           # extract r → stack
LOCAL_SET <r>
LOCAL_GET <r>
LOCAL_GET <r>
MUL
PUSH_RAT <3.14159_idx>
MUL
JMP match_end

check_rect:
# Rect(w, h) arm
DUP
VARIANT_FIELD 0           # extract w
LOCAL_SET <w>
VARIANT_FIELD 1           # extract h
LOCAL_SET <h>
LOCAL_GET <w>
LOCAL_GET <h>
MUL

match_end:
```

The pattern matching compilation is the same as before — the difference is
that arm bodies compile as applicative expressions (using locals) rather than
concatenative word sequences.

### 3.9 Brace Blocks

Source:
```
{
  let contents = read("config.toml")
  let config   = parse-toml(contents)
  config
}
```

Brace blocks desugar to sequential let-bindings at the compiler level:

```
let contents = read("config.toml")
let config = parse-toml(contents)
config
```

Bytecode:
```
PUSH_STR "config.toml"
CALL <read_id>
LOCAL_SET 0              # contents

LOCAL_GET 0
CALL <parse_toml_id>
LOCAL_SET 1              # config

LOCAL_GET 1              # result
```

Brace blocks are syntactic sugar for sequential let-bindings. No monadic
desugaring or CPS transform is needed because effects are handled by the VM's
effect dispatch mechanism, not by value-level plumbing.

### 3.10 Effect Handlers

Source:
```
handle may-fail(x) {
  raise(e) => default-value
}
```

Compilation is unchanged from VM spec §4.11. The handler body and operation
clauses compile as applicative expressions using the same local-variable
strategy. The `handle` expression:

1. Emits `HANDLE_PUSH` with the effect ID and handler code offset
2. Compiles the body expression
3. Emits `HANDLE_POP` + return clause
4. Emits handler clause bodies at the handler code offset

The handler parameters (e.g., `e` in `raise(e)`) are bound via `LOCAL_SET`
at handler entry, just like function arguments.

### 3.11 Record and Tuple Literals

Source:
```
{name: "Ada", age: 36}
(1, "hello", true)
[1, 2, 3]
```

Bytecode:
```
# Record
PUSH_STR "Ada"           # value for name
PUSH_INT 36              # value for age
RECORD_NEW 2             # field IDs resolved at compile time

# Tuple
PUSH_INT 1
PUSH_STR "hello"
PUSH_TRUE
TUPLE_NEW 3

# List
PUSH_INT 1
PUSH_INT 2
PUSH_INT 3
LIST_NEW 3
```

### 3.12 Field Access

Source:
```
record.name
```

Bytecode:
```
LOCAL_GET <record>
RECORD_GET <name_field_id>
```

---

## 4. Tail Call Optimization

In applicative style, tail calls are explicit:

```
factorial : (n: Int{>= 0}, acc: Int) -> <> Int =
  if n == 0 then acc else factorial(n - 1, acc * n)
```

The compiler detects when a `CALL` is in tail position (the result of the call
is the result of the enclosing function — nothing else happens after it) and
emits `TAIL_CALL` instead:

```
LOCAL_GET <n>
PUSH_INT 0
EQ
JMP_UNLESS else_lbl
LOCAL_GET <acc>
RET

else_lbl:
LOCAL_GET <n>
PUSH_INT 1
SUB
LOCAL_GET <acc>
LOCAL_GET <n>
MUL
TAIL_CALL <factorial_id>    # reuses frame
```

Tail position is easier to detect in applicative code than in concatenative
code: a call is in tail position if it's the outermost expression of a function
body, or the outermost expression of an if/match arm whose parent is in tail
position.

---

## 5. Stack Management: Compiler vs User

In the concatenative model, the user explicitly manages the stack with
`dup`/`drop`/`swap`. In the applicative model, the **compiler** manages the
stack — the user never thinks about it.

### Compiler stack usage

The compiler may emit stack manipulation ops internally:

- `DUP` — when a value is used multiple times and hasn't been assigned to a
  local (optimization: avoid redundant `LOCAL_GET`)
- `DROP` — to discard unused results (e.g., calling a function for side effects
  when the result type is `()`)
- `SWAP`/`ROT` — rarely, for specific compound-value construction patterns

In practice, the compiler's primary strategy is **locals-based**: every named
value gets a local slot, and `LOCAL_GET`/`LOCAL_SET` handle all value routing.
Stack manipulation ops are an optimization the compiler *may* use for
temporaries that don't have names.

### Guideline

A correct but unoptimized compiler can emit zero `DUP`/`DROP`/`SWAP`/`ROT`
instructions. All value management goes through locals. Stack manipulation
is a peephole optimization, not a requirement.

---

## 6. QUOTE vs CLOSURE: Usage in Applicative Model

The applicative model removes user-visible quotation syntax (`[...]`). However,
both `QUOTE` and `CLOSURE` opcodes remain useful:

| Opcode | Used for |
|--------|----------|
| `QUOTE` | Lambdas with no free variables (e.g., `fn(x) => x + 1`). Cheaper than `CLOSURE` because no capture array is allocated. |
| `CLOSURE` | Lambdas that capture free variables from the enclosing scope. |
| `CALL_DYN` | Calling either a `QUOTE` or `CLOSURE` value (used by higher-order functions like `map`, `filter`, `fold`). |

The `QUOTE` opcode is also used internally for thunks if the compiler needs
deferred evaluation (e.g., lazy effect handler clause references).

---

## 7. Updated Examples

These replace the concatenative-style examples in the VM spec (§7) with their
applicative equivalents.

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

### 7.4 Closure Example

Source:
```
make-adder : (n: Int) -> <> (Int) -> <> Int =
  fn(x) => x + n
```

Bytecode (word ID 259):
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

### 7.5 Effect Handling (Applicative Style)

Source:
```
safe-div : (a: Int, b: Int) -> <> Int? =
  handle div(a, b) {
    return(x) => some(x)
    raise(_) => none
  }
```

Bytecode (word ID 260):
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

---

## 8. Compilation Phases

The compiler pipeline for applicative Clank:

```
Source → Parse → AST → Desugar → Typed IR → Bytecode
```

### Phase 1: Parse
Produce AST from grammar (§4 of core-syntax.md).

### Phase 2: Desugar (AST → AST)
- `x |> f(y)` → `f(x, y)` (pipeline elimination)
- `a + b` → `add(a, b)` (operator desugaring)
- `{ let x = e1; e2 }` → `let x = e1 in e2` (block flattening)
- `a ++ b` → `str-cat(a, b)` (string concat)
- `!x` → `not(x)` (unary ops)

After desugaring, the AST contains only: function application, let bindings,
if/then/else, match, lambda, handle, and literals.

### Phase 3: Type Check (AST → Typed IR)
Standard type checking with effect inference. Out of scope for this doc.

### Phase 4: Code Generation (Typed IR → Bytecode)
Emit bytecode using the strategies documented in §3 above. Each function
becomes a word in the dictionary. The compiler maintains:
- A **local slot allocator** per function (maps variable names → u8 indices)
- A **word ID allocator** (maps function names → u16 IDs)
- A **string table** for string literals
- A **code buffer** for emitting bytecode

---

## 9. Conclusion: No VM Changes Needed

The stack VM instruction set is fully compatible with the applicative syntax.
The transition from concatenative-primary to applicative-primary affects only
the **compiler's code generation**, not the VM's execution semantics.

| Concern | Status |
|---------|--------|
| Function application | ✅ Compiles to push-args + CALL |
| Pipeline (`\|>`) | ✅ Desugared before codegen |
| Infix operators | ✅ Desugar to builtins → direct opcodes |
| Let bindings | ✅ LOCAL_SET / LOCAL_GET (already in spec) |
| If/then/else | ✅ JMP_IF / JMP_UNLESS (already in spec) |
| Lambdas | ✅ QUOTE (no captures) or CLOSURE (with captures) |
| Higher-order calls | ✅ CALL_DYN (already in spec) |
| Pattern matching | ✅ Unchanged from spec |
| Brace blocks | ✅ Desugar to let-bindings |
| Effect handlers | ✅ Unchanged from spec |
| Tail calls | ✅ TAIL_CALL (easier to detect in applicative) |
| Record/tuple/list literals | ✅ Unchanged from spec |

No new opcodes, no new VM architecture, no changes to the binary format.
