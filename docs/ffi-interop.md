# Clank FFI / Host Language Interop Specification

**Task:** TASK-080
**Mode:** Research / Spec
**Date:** 2026-03-19
**Dependencies:** core-syntax.md, effect-system.md, affine-types.md, module-system.md, tooling-spec.md

---

## 1. Overview

Clank's FFI system enables Clank programs to call host language functions (C, JavaScript, Python) while upholding Clank's three safety pillars — effects, affine types, and refinement
types — at the FFI boundary. Foreign functions are treated as opaque, untrusted code:
the FFI declaration is where the programmer asserts the contract that the type checker
enforces on the Clank side.

### Design Goals

1. **Safety-preserving** — FFI does not silently break effect tracking or affine ownership
2. **Terse** — minimal syntax overhead for declaring foreign functions
3. **Multi-host** — one declaration mechanism covers C, JS, Python via host-specific backends
4. **Agent-friendly** — FFI declarations are self-documenting; type signatures include effects
5. **Embedding-first** — the reference interpreter exposes a simple TypeScript API

### Non-Goals (Out of Scope)

- WASM-specific interop (deferred to WASM compilation phase)
- Implementation details (this is a design spec)
- Automatic binding generation tooling (future work)

---

## 2. FFI Declaration Syntax

### 2.1 The `extern` Keyword

Foreign functions are declared with the `extern` keyword, which introduces a binding
with a Clank type signature but no Clank implementation:

```ebnf
extern-decl  = ['pub'] 'extern' string-lit ident ':' type-sig [extern-opts] ;
extern-opts  = 'where' extern-attr { ',' extern-attr } ;
extern-attr  = 'host' '=' string-lit
             | 'symbol' '=' string-lit
             | 'unsafe' ;
```

The string literal after `extern` names the foreign module/library. The rest is a
standard Clank type signature.

### 2.2 Basic Examples

```
# Import a C function
extern "libc" c-strlen : (s: Str) -> <io> Int

# Import a JS function
extern "node:fs" read-file-sync : (path: Str) -> <io, exn> Str
  where host = "js"

# Import a Python function
extern "json" py-dumps : (obj: Str) -> <> Str
  where host = "python", symbol = "dumps"
```

### 2.3 Syntax Breakdown

| Component | Purpose |
|-----------|---------|
| `extern "lib"` | Names the foreign library/module |
| `ident : type-sig` | Clank-side name and full type signature (including effects) |
| `where host = "..."` | Target host language (`"c"`, `"js"`, `"python"`) — inferred from lib name if unambiguous |
| `where symbol = "..."` | Foreign symbol name if different from Clank identifier |
| `where unsafe` | Opts out of affine/effect safety checks (see §5.4) |

When `host` is omitted, it is inferred:
- Library names starting with `lib` → `"c"`
- Library names starting with `node:` → `"js"`
- All others → determined by the runtime backend in use

When `symbol` is omitted, the Clank identifier is used after converting hyphens to
underscores (`read-file` → `read_file`).

### 2.4 Extern Modules

For libraries with many foreign bindings, an `extern mod` block avoids repetition:

```
extern mod "libsqlite3" where host = "c" {
  pub sqlite-open  : (path: Str) -> <io, exn> DbHandle
  pub sqlite-close : (db: DbHandle) -> <io> ()
  pub sqlite-exec  : (db: &DbHandle, sql: Str) -> <io, exn> [Row]
}
```

This is sugar for individual `extern "libsqlite3"` declarations with the shared
`where` attributes. Each declaration in the block is a top-level binding in the
enclosing module.

### 2.5 Grammar Additions

New productions added to the core grammar:

```ebnf
top-level   += extern-decl | extern-mod ;

extern-decl  = ['pub'] 'extern' str-lit ident ':' type-sig [extern-where] ;
extern-mod   = 'extern' 'mod' str-lit [extern-where] '{' { extern-member } '}' ;
extern-member = ['pub'] ident ':' type-sig ;
extern-where  = 'where' extern-attr { ',' extern-attr } ;
extern-attr   = ident '=' str-lit | 'unsafe' ;
```

**New keywords:** 1 (`extern`)
**Production count impact:** +5 rules

---

## 3. Type Mapping

Foreign types must be mapped to Clank types at the FFI boundary. The mapping is
defined per host language.

### 3.1 Clank to C Type Mapping

| Clank Type | C Type | Notes |
|------------|--------|-------|
| `Int` | `int64_t` | Clank Int is arbitrary-precision; truncated to 64-bit across FFI. Overflow raises `exn`. |
| `Rat` | `double` | Lossy conversion (rational → float). |
| `Bool` | `int32_t` | 0 = false, nonzero = true |
| `Str` | `const char*` (UTF-8) | Null-terminated. Clank retains ownership unless `unsafe`. |
| `Byte` | `uint8_t` | Direct mapping |
| `()` | `void` (return only) | As parameter: omitted |
| `[T]` | `clank_list_t*` | Opaque handle; accessed via embedding API |
| `(T, U)` | `clank_tuple_t*` | Opaque handle |
| `{k: T}` | `clank_record_t*` | Opaque handle |
| `Option<T>` | `T*` (nullable) | `none` → `NULL`, `some(v)` → pointer to v |
| Function types | `clank_fn_t*` | Opaque callable; invoked via `clank_call()` |
| Affine types | `clank_handle_t*` | Opaque; see §5 for ownership rules |
| `&T` | `const clank_ref_t*` | Read-only borrow; must not be stored by C code |

### 3.2 Clank to JavaScript Type Mapping

| Clank Type | JS Type | Notes |
|------------|---------|-------|
| `Int` | `BigInt` (> 2^53) or `number` | Auto-selects based on magnitude |
| `Rat` | `number` | IEEE 754 double |
| `Bool` | `boolean` | Direct |
| `Str` | `string` | Direct (both UTF-16 in JS, UTF-8 in Clank; runtime converts) |
| `Byte` | `number` | 0-255 |
| `()` | `undefined` | |
| `[T]` | `Array` | Deep conversion; elements are converted recursively |
| `(T, U)` | `Array` (fixed-length) | `[v1, v2]` |
| `{k: T}` | `Object` | Field names preserved |
| `Option<T>` | `T \| null` | `none` → `null` |
| Function types | `Function` | Wrapped with effect/affine checks |
| Affine types | Opaque wrapper object | See §5 |
| `&T` | Frozen object (read-only proxy) | Prevents mutation |

### 3.3 Clank to Python Type Mapping

| Clank Type | Python Type | Notes |
|------------|-------------|-------|
| `Int` | `int` | Direct (both arbitrary-precision) |
| `Rat` | `fractions.Fraction` | Exact mapping |
| `Bool` | `bool` | Direct |
| `Str` | `str` | Direct (both Unicode) |
| `Byte` | `int` | 0-255, range-checked |
| `()` | `None` | |
| `[T]` | `list` | Deep conversion |
| `(T, U)` | `tuple` | Direct |
| `{k: T}` | `dict` | Field names as string keys |
| `Option<T>` | `T \| None` | `none` → `None` |
| Function types | Callable wrapper | With effect/affine checks |
| Affine types | Opaque wrapper (`ClankHandle`) | See §5 |
| `&T` | Read-only proxy | `__setattr__` raises |

### 3.4 Compound Type Conversion Strategy

Compound types (lists, tuples, records) are converted **eagerly at the boundary** for
C and Python, and **lazily via proxies** for JavaScript (where object identity matters
less and proxy overhead is low).

For large data structures, the `extern` declaration can opt into **handle mode** where
the compound value stays on the Clank heap and the foreign side receives an opaque
handle:

```
extern "analytics" process-data : (data: handle [Record]) -> <io> Stats
  where host = "python"
```

The `handle` modifier on a parameter type means: pass an opaque reference instead of
converting. The foreign function accesses fields via the embedding API.

---

## 4. Effect Safety Across the FFI Boundary

### 4.1 Principle: Foreign Functions Must Declare Effects

Every `extern` declaration includes an effect annotation. The type checker trusts
this annotation — it cannot verify what a foreign function actually does. The
annotation is the programmer's contract.

```
# Correct: declares that it performs IO and may throw
extern "node:fs" read-file : (path: Str) -> <io, exn> Str

# Incorrect: claims purity — a lie, but the type checker trusts it
extern "node:fs" read-file-bad : (path: Str) -> <> Str
```

### 4.2 Effect Annotation Rules

| Rule | Description |
|------|-------------|
| **E-FFI-1** | All `extern` declarations must have explicit effect annotations (same as `pub` functions) |
| **E-FFI-2** | Foreign functions that perform any I/O must include `<io>` |
| **E-FFI-3** | Foreign functions that may throw/fail must include `<exn>` or `<exn[E]>` |
| **E-FFI-4** | Foreign functions may NOT declare user-defined Clank effects (they cannot perform `resume`) |
| **E-FFI-5** | A foreign function declared `<>` (pure) is trusted as pure; misuse is programmer error |

### 4.3 Rule E-FFI-4: No User-Defined Effects

A foreign function cannot perform a Clank algebraic effect operation because effect
operations require cooperation with the Clank runtime's handler/continuation mechanism.
A C function has no way to invoke `resume` or interact with effect handlers.

```
# ERROR: Foreign function cannot perform user-defined effect
extern "mylib" do-thing : (x: Int) -> <MyEffect> Int

# OK: Built-in effects are fine (io, exn are handled by the runtime)
extern "mylib" do-thing : (x: Int) -> <io, exn> Int
```

**Exception:** If a foreign function receives a Clank callback that performs effects,
the callback's effects propagate through the foreign function's type:

```
# OK: the callback may perform effects; they propagate
extern "mylib" with-resource : (cb: (Handle) -> <e> a) -> <io, exn | e> a
```

Here `with-resource` itself performs `<io, exn>`, and the callback's effects `<e>` flow
through. The foreign function calls the callback via the embedding API, which manages
the effect stack.

### 4.4 Effect Wrapping at the Boundary

When a foreign function is called from Clank:

1. The runtime pushes an `<exn>` handler frame that catches host exceptions and converts
   them to Clank `exn` values (if `<exn>` is in the effect row)
2. If `<io>` is declared, no special wrapping is needed (it's a permission marker)
3. If the foreign function throws and `<exn>` is NOT in its declared effects, the
   exception propagates as an unhandled runtime panic

### 4.5 Passing Clank Callbacks to Foreign Functions

When a Clank function (closure) is passed to a foreign function as a callback:

1. The closure is wrapped in a host-language callable that re-enters the Clank runtime
2. The Clank runtime restores the effect handler stack before executing the callback
3. Effects performed inside the callback are handled normally by enclosing Clank handlers
4. If the callback raises an unhandled exception, it propagates back through the
   foreign function as a host-language exception

```
# Foreign function that calls a callback
extern "mylib" for-each-item : (cb: (Item) -> <io> ()) -> <io> ()

# Usage in Clank — the callback's effects are tracked
process : () -> <io> () =
  for-each-item(fn(item) => print(show(item)))
```

### 4.6 Formal Typing Rule

```
extern "lib" f : (T1, ..., Tn) -> <E> R
E ⊆ {io, exn, exn[X], async}  ∪  effect-vars
───────────────────────────────────────────────
Γ |- f : (T1, ..., Tn) -> <E> R ! <>
```

The `extern` declaration introduces `f` into the typing context with the declared
signature. The constraint on `E` ensures only built-in effects and effect variables
(from callback parameters) appear in the row.

---

## 5. Affine Type Safety Across the FFI Boundary

### 5.1 Principle: Affine Values Crossing the Boundary

Affine types enforce resource protocols (open/close, acquire/release). When affine
values cross the FFI boundary, ownership must be explicitly tracked.

### 5.2 Ownership Rules

| Direction | Rule | Behavior |
|-----------|------|----------|
| Clank → Foreign (move) | **A-FFI-1** | Value is consumed in Clank; foreign code owns it. Clank cannot use it again. |
| Clank → Foreign (borrow) | **A-FFI-2** | `&T` parameter: foreign code gets read-only access. Clank retains ownership. Foreign code must not store the reference. |
| Foreign → Clank (return) | **A-FFI-3** | Returned affine value is owned by Clank. Must be consumed according to normal affine rules. |
| Foreign → Clank (callback arg) | **A-FFI-4** | Affine values passed to callbacks follow the callback's parameter affinity. |

### 5.3 Foreign-Created Affine Handles

A common pattern: a foreign function creates a resource (file descriptor, database
connection, etc.) and returns it as an affine type in Clank.

```
affine type DbConn

extern "libpq" pg-connect : (connstr: Str) -> <io, exn> DbConn
extern "libpq" pg-query   : (db: &DbConn, sql: Str) -> <io, exn> [Row]
extern "libpq" pg-close   : (db: DbConn) -> <io> ()
```

The Clank type system enforces that `DbConn`:
- Is used at most once (affine)
- Can be borrowed for queries (`&DbConn`)
- Must be consumed by `pg-close` (or explicitly `discard`ed)

The foreign library allocates/frees the actual resource. Clank's affine tracking
ensures the protocol is followed on the Clank side.

### 5.4 The `unsafe` Escape Hatch

For performance-critical code or legacy interop where affine/effect tracking is
too restrictive, `where unsafe` opts out of safety checks:

```
extern "mylib" raw-ptr-op : (p: Int) -> <> Int
  where unsafe
```

With `unsafe`:
- Effect annotation is not enforced (the function may perform undeclared effects)
- Affine values passed to the function are consumed but the foreign side is not
  required to respect the protocol
- The linter emits `W700` for every `unsafe` extern

`unsafe` is deliberately verbose and linter-flaggable — same philosophy as `discard`.

### 5.5 Affine Values in Compound Types Across FFI

When a compound type containing affine components crosses the FFI boundary:

```
# Tuple containing an affine File
extern "mylib" process-pair : (pair: (File, Str)) -> <io> Str
```

- **Move**: The entire compound is consumed. All affine components within it are
  considered consumed.
- **Borrow**: Not supported for compound types containing affine components across FFI.
  Decompose first.

This restriction keeps the boundary simple. The programmer must destructure, borrow
individual affine components, and reconstruct if needed.

### 5.6 GC Interaction

Foreign code may hold references to Clank heap objects (via opaque handles). The
runtime must prevent the GC from collecting these objects:

- **Handle table**: The runtime maintains a table of objects referenced by foreign
  code. Objects in this table are GC roots.
- **Release protocol**: When foreign code is done with a handle, it calls
  `clank_release(handle)` (embedding API) to remove the GC root.
- For affine types, `clank_release` is called automatically when the foreign function
  returns (unless the return type indicates continued ownership).

---

## 6. Refinement Types Across FFI

### 6.1 Refinement Checking at the Boundary

Refinement predicates cannot be verified for values coming from foreign code.
The runtime inserts **dynamic checks** at the FFI boundary for refined types:

```
extern "mylib" get-port : () -> <io> Int{> 0 && <= 65535}
```

When `get-port` returns, the runtime checks `v > 0 && v <= 65535` at runtime.
If the check fails, it raises `exn` with a refinement violation error.

### 6.2 Refinement Rules

| Direction | Behavior |
|-----------|----------|
| Clank → Foreign | Refinements are already satisfied (verified by Clank's type checker). No runtime check needed. |
| Foreign → Clank | Runtime check inserted. Violation raises `exn`. |
| `where unsafe` | Runtime checks are skipped. |

### 6.3 Dynamic Check Cost

Runtime refinement checks are an unavoidable cost of FFI safety. For hot paths,
the programmer can:
1. Use `where unsafe` to skip checks (at their own risk)
2. Use unrefined types at the boundary and refine inside Clank
3. Use the `handle` modifier to avoid conversion overhead for compound types

---

## 7. Error Code Additions

| Code | Description |
|------|-------------|
| E800 | FFI type mismatch: Clank value cannot be converted to foreign type |
| E801 | FFI refinement violation: foreign return value fails refinement check |
| E802 | FFI affine violation: attempted to pass consumed affine value to foreign function |
| E803 | FFI effect violation: foreign function declared user-defined effect (E-FFI-4) |
| E804 | FFI host mismatch: declared host does not match active runtime backend |
| W700 | `unsafe` extern declaration (linter warning) |
| W701 | Extern function declared pure `<>` — verify manually |
| W702 | Affine value passed to foreign function without matching consume on foreign side |

---

## 8. Complete Examples

### 8.1 SQLite Bindings

```
mod db.sqlite

affine type DbHandle
type Row = {cols: [Str], vals: [Str]}

extern mod "libsqlite3" where host = "c" {
  pub sqlite-open  : (path: Str) -> <io, exn> DbHandle
  pub sqlite-close : (db: DbHandle) -> <io> ()
  pub sqlite-exec  : (db: &DbHandle, sql: Str) -> <io, exn> [Row]
}

## Execute a query and return results, ensuring the db is closed.
pub with-db : (path: Str, query: Str) -> <io, exn> [Row] =
  fn(path, query) =>
    let db = sqlite-open(path)
    let rows = sqlite-exec(&db, query)
    sqlite-close(db)
    rows
```

### 8.2 Node.js Crypto

```
mod util.crypto

extern mod "node:crypto" where host = "js" {
  pub random-bytes  : (n: Int{> 0}) -> <io> Str
    where symbol = "randomBytes"
  pub create-hash   : (algo: Str) -> <> Str
    where symbol = "createHash"
}

pub sha256 : (input: Str) -> <> Str =
  create-hash("sha256")
```

### 8.3 Python ML Integration

```
mod ml.predict

extern mod "sklearn.linear_model" where host = "python" {
  pub fit     : (X: [[Rat]], y: [Rat]) -> <io> Model
    where symbol = "LinearRegression().fit"
  pub predict : (model: &Model, X: [[Rat]]) -> <io> [Rat]
}

affine type Model

pub train-and-predict : ([[Rat]], [Rat], [[Rat]]) -> <io, exn> [Rat] =
  fn(X-train, y-train, X-test) =>
    let model = fit(X-train, y-train)
    let preds = predict(&model, X-test)
    discard model
    preds
```

### 8.4 Callback Pattern: Foreign Iterator

```
affine type CsvReader

extern mod "csv-parser" where host = "js" {
  pub open-csv   : (path: Str) -> <io, exn> CsvReader
  pub close-csv  : (r: CsvReader) -> <io> ()
  pub each-row   : (r: &CsvReader, cb: ({cols: [Str]}) -> <e> ()) -> <io | e> ()
}

pub count-rows : (path: Str) -> <io, exn> Int =
  fn(path) =>
    let reader = open-csv(path)
    let count = handle each-row(&reader, fn(row) => tick()) {
      return(_) => get(),
      tick() resume k => do { mod(fn(n) => n + 1); k(()) }
    } with state = 0
    close-csv(reader)
    count

effect counter {
  tick : () -> ()
}
```

---

## 9. Design Rationale

### Why `extern` instead of extending `use`?

`use` imports Clank modules — the compiler resolves types, checks effects, and verifies
implementations. `extern` imports opaque foreign bindings — the compiler trusts the
declared signature. Using a different keyword makes the trust boundary explicit: every
`extern` is a site where Clank's guarantees depend on programmer-asserted contracts.

### Why require effect annotations on all externs?

Foreign functions are the most likely source of undeclared effects. Requiring explicit
annotations forces the programmer to think about what the foreign function does. An
`extern` without effects is a conscious assertion of purity, not an oversight.

### Why not auto-generate bindings?

Auto-generation (like Rust's `bindgen`) requires parsing foreign headers and mapping
types automatically. This is valuable but complex and host-specific. The manual
declaration approach is simpler, fits in the spec budget, and is sufficient for v1.
Auto-generation can be layered on top later as a `clank ffi-gen` tool.

### Why eager conversion for C/Python but lazy for JS?

C and Python have strong FFI conventions (ctypes, cffi, PyO3) where values are
copied across the boundary. JavaScript's prototype-based objects and proxies make
lazy wrapping natural and cheap. Eager conversion for JS would require deep-cloning
arrays and objects unnecessarily.

### Why no user-defined effects across FFI (E-FFI-4)?

Algebraic effects require the runtime to manage a handler stack and delimited
continuations. A foreign function written in C/JS/Python cannot participate in this
protocol — it has no way to capture a continuation, call `resume`, or interact with
the handler stack. Only the built-in effects (`io`, `exn`, `async`) have host-level
equivalents (system calls, exceptions, promises).

The exception is callbacks: when Clank code is called back from foreign code, the
Clank runtime resumes control and the full effect system is available within the
callback body.

### Why the `unsafe` escape hatch?

Real-world FFI sometimes requires bypassing safety checks — legacy C libraries with
unusual ownership conventions, performance-critical inner loops, or host APIs that
don't fit Clank's model. `unsafe` makes these sites explicit and linter-detectable,
following the same philosophy as `discard` for affine values.


