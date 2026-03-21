# Clank Module System Specification

**Task:** TASK-020
**Mode:** Spec
**Date:** 2026-03-14
**Dependencies:** core-syntax.md, effect-system.md, interface-system.md

---

## 1. Overview

Clank's module system provides namespace management, visibility control, and
separate compilation. Modules are the unit of code organization; packages are
the unit of distribution.

### Design Goals

1. **1:1 file mapping** — one file = one module, no exceptions
2. **Explicit imports** — no glob imports; every imported name is listed
3. **Effect-aware exports** — public function signatures include effect rows
4. **Coherence-preserving** — orphan impl rules enforced at module boundaries
5. **Minimal syntax** — `mod`, `use`, `pub` — three keywords, no ceremony

---

## 2. Modules

### 2.1 Module Declaration

Every source file begins with a `mod` declaration. The module name must match
the file path relative to the package root.

```
mod math.stats
```

File: `src/math/stats.clk`

If the `mod` declaration is omitted, the module name is inferred from the file
path. Explicit `mod` is required only when the module name would be ambiguous
(e.g., a `mod.clk` file serving as a directory module).

### 2.2 Module-to-File Mapping

```
<package-root>/
  clank.pkg                  # package manifest
  src/
    main.clk                 # mod main
    math.clk                 # mod math
    math/
      stats.clk              # mod math.stats
      linalg.clk             # mod math.linalg
    net/
      http.clk               # mod net.http
      http/
        client.clk           # mod net.http.client
```

Rules:
- Module path separator is `.` in source, `/` in the filesystem
- File extension is `.clk`
- All source files live under `src/`
- A file `foo.clk` and directory `foo/` may coexist — `foo.clk` is `mod foo`,
  files under `foo/` are `mod foo.child`

### 2.3 Grammar Additions

```ebnf
top-level   += mod-decl | use-decl ;

mod-decl    = 'mod' mod-path ;
mod-path    = ident { '.' ident } ;

use-decl    = 'use' mod-path '(' import-list ')' ;
import-list = import-item { ',' import-item } ;
import-item = ident [ 'as' ident ] ;
```

---

## 3. Visibility

### 3.1 Rules

Every top-level definition is **private by default**. The `pub` keyword makes
a definition visible to other modules.

`pub` applies to:
- Function definitions
- Type declarations
- Effect declarations
- Interface declarations
- Impl blocks (see §6 for special rules)

```
# private — only visible within this module
helper : (Int) -> <> Int = ...

# public — importable by other modules
pub mean : (xs: [Int]{len > 0}) -> <> Rat = ...
```

### 3.2 What `pub` Exposes

| Declaration | `pub` exposes |
|-------------|---------------|
| Function | Name + full type signature (including effect row) |
| Type (opaque) | Name only — constructors hidden |
| Type (transparent) | `pub type` exposes name + all constructors |
| Effect | Name + all operations |
| Interface | Name + all method signatures |
| Impl | Always public (see §6) |

### 3.3 Opaque vs Transparent Types

By default, `pub type` exports the type name **and** its constructors:

```
pub type Shape
  = Circle(Rat)
  | Rect(Rat, Rat)
```

To export the name without exposing constructors (opaque type), use `pub opaque`:

```
pub opaque type Handle = { fd: Int, mode: Mode }
```

Importers can pass `Handle` values around but cannot construct or destructure
them — only the defining module's public functions can do so.

### 3.4 Re-exports

A module can re-export imported names:

```
use math.stats (mean, median)
pub use math.stats (mean)    # re-export mean from this module
```

This is the only mechanism for "facade" modules that aggregate sub-module APIs.

---

## 4. Import System

### 4.1 Basic Import

```
use std.list (map, filter, fold)
use std.io (print, read)
```

Every imported name must be listed explicitly. No glob imports (`use std.list *`
is a syntax error).

### 4.2 Aliased Imports

```
use std.list (map as list-map, filter as list-filter)
```

Aliases resolve name conflicts without requiring qualified access.

### 4.3 Importing Types, Effects, and Interfaces

Types, effects, and interfaces are imported by name, same as functions:

```
use std.option (Option, Some, None)     # type + constructors
use net.http (HttpErr)                  # effect
use std.show (Show)                     # interface
```

When importing a transparent type, you may import the type name alone or the
type name plus specific constructors:

```
use std.option (Option)                 # type name only — no constructors
use std.option (Option, Some, None)     # type name + constructors
```

Importing only the type name (without constructors) means you can use it in
type annotations but cannot pattern-match on or construct its variants.

### 4.4 Importing Interface Implementations

Impl blocks are **not imported by name** — they are imported implicitly when
either the interface or the type is imported. See §6 for full rules.

### 4.5 Resolution Order

Name resolution proceeds:
1. Local definitions (current module)
2. Imported names (via `use`)
3. Built-in names (primitives, built-in effects, built-in interfaces)

Shadowing between imports is a compile error — if two `use` declarations bring
the same name into scope, the compiler rejects it. Use `as` to resolve.

Shadowing a built-in with a local definition is a warning.

---

## 5. Effect Export and Import Rules

### 5.1 Public Effect Declarations

An effect declared with `pub` exports the effect name and all its operations:

```
pub effect log {
  info  : (Str) -> <> (),
  warn  : (Str) -> <> (),
  error : (Str) -> <> ()
}
```

Importing the effect brings all operations into scope:

```
use logging (log)
# info, warn, error are now available
```

There is no partial export of effect operations — an effect is either fully
public or fully private. Rationale: effect handlers must handle all operations,
so partial visibility would create unhandleable effects.

### 5.2 Effect Signatures at Module Boundaries

Public function signatures **must** include explicit effect annotations:

```
# OK — explicit effect row
pub read-config : (Str) -> <io, exn> Config = ...

# ERROR — public function without effect annotation
pub read-config : (Str) -> Config = ...
```

Private functions may omit effect annotations (inferred by the type checker).

Rationale: effect rows are part of a function's contract. At module boundaries,
contracts must be explicit so that importers know what effects to expect without
reading the implementation.

### 5.3 Effect Row Composition Across Modules

When a function from module A calls a function from module B, the effect rows
compose by union, as specified in the effect system (§4.1 of effect-system.md):

```
# Module net.http
pub fetch : (Str) -> <io, exn[HttpErr]> Response = ...

# Module app.main
use net.http (fetch)

load-data : (Str) -> <io, exn[HttpErr], log> Data =
  ...
  let resp = fetch(url)    # adds <io, exn[HttpErr]> to this function's row
  info("fetched")          # adds <log>
  parse(resp.body)
```

No special mechanism is needed — the standard effect row algebra handles
cross-module composition.

### 5.4 Handling Imported Effects

Effects imported from other modules can be handled normally:

```
use logging (log)
use logging.handlers (with-stdout-log)

main : () -> <io> () =
  with-stdout-log(fn() => process-data())
  # log effect is discharged by with-stdout-log
```

---

## 6. Interface Coherence Across Modules

### 6.1 Impl Visibility

All `impl` blocks are **always public**. There is no `pub impl` vs private
`impl` — if an impl exists, it is visible to all modules that can see both the
interface and the type.

Rationale: coherence (Rule 7 of the interface system — at most one impl per
type-interface pair) is a global property. Hidden impls would make coherence
uncheckable.

### 6.2 Orphan Rule

An **orphan impl** is an `impl C for T` where neither `C` nor `T` is defined
in the current module (or its package — see §6.3).

Orphan impls are:
- **Warning** by default
- **Error** with `--strict-orphans` compiler flag

```
# In module my.app — neither Show nor HttpResponse is defined here
# This is an orphan impl → warning (or error with --strict-orphans)
impl Show for HttpResponse {
  show = ...
}
```

### 6.3 Package-Level Orphan Relaxation

The orphan check is relaxed within a package: an impl is **not** considered
orphan if the interface or type is defined anywhere in the same package, even
if not in the same module.

```
# Package: my-http
# Module my-http.types — defines HttpResponse
# Module my-http.show  — impl Show for HttpResponse
# → NOT an orphan, because HttpResponse is in the same package
```

### 6.4 Coherence Checking

The compiler enforces coherence in two phases:

1. **Intra-package**: Check all impls within the current package for conflicts.
   This is done at compile time with full visibility.

2. **Inter-package**: When linking packages, check that no two packages provide
   the same impl. Conflicting impls are a hard error regardless of
   `--strict-orphans`.

### 6.5 Impl Import Behavior

When you import a type or interface, all visible impls for that type-interface
pair come into scope automatically:

```
use std.option (Option, Some, None)
use std.show (Show)
# impl Show for Option is now in scope — no explicit import needed
```

This is required for coherence: the set of impls must be consistent regardless
of which modules happen to be imported.

---

## 7. Package Structure

### 7.1 Package Manifest

Each package has a `clank.pkg` file at its root:

```
name = "my-app"
version = "0.1.0"
entry = "main"

[deps]
std = "0.1"
net-http = "1.2"
```

Fields:
- `name`: package identifier (lowercase, hyphens allowed)
- `version`: semver version
- `entry`: module containing `main` function (for executables)
- `deps`: dependency map (name → version constraint)

### 7.2 Directory Layout

```
my-app/
  clank.pkg
  src/
    main.clk
    lib/
      parser.clk
      types.clk
  test/
    parser-test.clk
```

- `src/` — source modules
- `test/` — test modules (can import from `src/` but not vice versa)

### 7.3 Package Namespacing

Modules within a package are referenced by their path relative to `src/`.
Modules from other packages are prefixed by the package name:

```
# Import from same package
use lib.parser (parse)

# Import from dependency
use net-http.client (fetch)
```

The package name acts as the top-level namespace. There is no aliasing of
package names at the import site — the package name in `clank.pkg` is
authoritative.

### 7.4 Standard Library

The standard library is package `std`. It is implicitly available to all
packages without a `deps` entry. Its modules include:

```
std.list      # list operations
std.str       # string operations
std.io        # IO operations
std.option    # Option type
std.result    # Result type
std.show      # Show interface
std.eq        # Eq interface
std.ord       # Ord, Ordering
std.clone     # Clone interface
std.default   # Default interface
std.convert   # Into, From interfaces
```

---

## 8. Formal Rules

### Rule M1: Module Identity

```
  File at <pkg-root>/src/a/b/c.clk
  ─────────────────────────────────
  Module identity = a.b.c
```

### Rule M2: Visibility

```
  Definition d in module M
  d is NOT marked pub
  Reference to d from module M' where M ≠ M'
  ──────────────────────────────────────────
  Error: d is private to M
```

### Rule M3: Import Resolution

```
  use P.Q (x)
  x is pub in module P.Q
  No other use in scope brings name x
  ──────────────────────────────────
  x resolves to P.Q.x in current module
```

### Rule M4: Effect Export Completeness

```
  pub effect E { op1 : σ1, ..., opN : σN }
  use M (E)
  ──────────────────────────────────────────
  op1, ..., opN are all in scope
```

### Rule M5: Public Effect Annotation Requirement

```
  pub f : σ  in module M
  σ = (params) -> τ           (no effect annotation)
  ──────────────────────────────────────────────────
  Error: public function f must have explicit effect annotation
```

### Rule M6: Impl Global Visibility

```
  impl C for T  in module M
  Module M' imports C or T
  ─────────────────────────
  impl C for T is visible in M'
```

### Rule M7: Coherence (Global)

```
  impl C for T  in module M1
  impl C for T  in module M2
  M1 ≠ M2
  ──────────────────────────
  Error: conflicting implementations of C for T
```

### Rule M8: Orphan Check

```
  impl C for T  in module M
  C is not defined in package(M)
  T is not defined in package(M)
  ──────────────────────────────
  Warning: orphan impl of C for T in M
  (Error if --strict-orphans)
```

### Rule M9: Import Conflict

```
  use P (x)
  use Q (x)
  ──────────
  Error: ambiguous import x (from P and Q)
```

### Rule M10: No Circular Dependencies

```
  Module A imports from module B (directly or transitively)
  Module B imports from module A (directly or transitively)
  ──────────────────────────────────────────────────────────
  Error: circular dependency between A and B
```

The module dependency graph must be a DAG (directed acyclic graph). Circular
imports are a compile-time error. This ensures deterministic compilation order
and prevents initialization-order issues.

To break circular dependencies, extract shared types or interfaces into a
third module that both modules can import from.

---

## 9. Grammar Summary

New/modified productions for the module system:

```ebnf
top-level   += mod-decl | use-decl ;

mod-decl     = 'mod' mod-path ;
mod-path     = ident { '.' ident } ;

use-decl     = [ 'pub' ] 'use' mod-path '(' import-list ')' ;
import-list  = import-item { ',' import-item } ;
import-item  = ident [ 'as' ident ] ;

definition   = [ 'pub' ] ident ':' type-sig '=' body ;
type-decl    = [ 'pub' [ 'opaque' ] ] 'type' ident ... ;
effect-decl  = [ 'pub' ] 'effect' ident '{' ... '}' ;
interface-decl = [ 'pub' ] 'interface' ident ... ;
```

**New keywords:** `opaque` (1 new keyword)
**Modified keywords:** `pub` extended to `use` declarations for re-export
**Production count impact:** +6 rules
**Formal rules:** 10 (M1–M10)

---

## 10. Examples

### 10.1 Library Module

```
mod math.stats

use std.list (fold, len, map)

pub mean : (xs: [Rat]{len > 0}) -> <> Rat =
  fold(xs, 0.0, add) / len(xs)

pub variance : (xs: [Rat]{len > 0}) -> <> Rat =
  let m = mean(xs)
  let diffs = map(xs, fn(x) => (x - m) * (x - m))
  mean(diffs)

# private helper — not exported
square-diff : (x: Rat, m: Rat) -> <> Rat =
  (x - m) * (x - m)
```

### 10.2 Effect Export

```
mod app.logging

pub effect log {
  info  : (Str) -> <> (),
  warn  : (Str) -> <> (),
  error : (Str) -> <> ()
}

pub with-stdout-log : (() -> <log | e> a) -> <io | e> a =
  fn(f) => handle f() {
    return(x) => x,
    info(msg, resume, k)  => do { print("[INFO] " ++ msg); k(()) },
    warn(msg, resume, k)  => do { print("[WARN] " ++ msg); k(()) },
    error(msg, resume, k) => do { print("[ERR] " ++ msg); k(()) }
  }
```

### 10.3 Using Effects from Another Module

```
mod app.main

use std.io (print)
use app.logging (log, with-stdout-log)

main : () -> <io> () =
  with-stdout-log(fn() => do {
    info("starting")
    process()
    info("done")
  })

process : () -> <log> () =
  info("processing data")
```

### 10.4 Opaque Type

```
mod data.set

pub opaque type Set<a> = { items: [a], cmp: (a, a) -> <> Bool }

pub empty : (() -> <> Set<a>) where Eq a =
  fn() => { items: [], cmp: eq }

pub insert : (Set<a>, a) -> <> Set<a> =
  fn(s, x) =>
    if contains(s, x) then s
    else { items: cons(x, s.items), cmp: s.cmp }

pub contains : (Set<a>, a) -> <> Bool =
  fn(s, x) => fold(s.items, false, fn(acc, item) => acc || s.cmp(item, x))
```

### 10.5 Re-export Facade

```
mod math

pub use math.stats (mean, variance)
pub use math.linalg (dot, cross, normalize)
pub use math.trig (sin, cos, tan)
```

### 10.6 Cross-Package Import

```
mod app.api

use net-http.client (fetch, Response)
use net-http (HttpErr)
use std.result (Result, ok, err)

pub get-user : (Int) -> <io, exn[HttpErr]> User =
  fn(id) =>
    let resp = fetch("/users/" ++ show(id))
    parse-user(resp.body)
```

---

## 11. Design Rationale

### Why no glob imports?

Glob imports (`use std.list *`) import an unpredictable set of names that
changes as the upstream module evolves. For LLM-based agents, explicit imports
serve as a local manifest of available names — the agent does not need to
resolve what `*` expands to. This directly serves Clank's "no magic" principle.

### Why require effect annotations on public functions?

Effect rows are part of a function's behavioral contract. Within a module,
inference is convenient. At module boundaries, explicit annotations ensure that
importers can read the contract without consulting the implementation. This
mirrors the convention in Koka and aligns with Clank's explicit-is-better
philosophy.

### Why are impls always public?

Interface coherence is a global property — the compiler must see all impls to
verify uniqueness. A private impl that satisfies a constraint in one module but
is invisible in another would break coherence or cause confusing failures.
Making all impls public keeps the system simple and predictable.

### Why package-level orphan relaxation?

A strict per-module orphan rule would force all impls into the same file as
the type or interface definition, leading to bloated files. Relaxing to package
scope lets authors organize impls across files while still preventing
cross-package orphan chaos.

### Why opaque types?

Opaque types enable encapsulation — a module can expose a type for use in
signatures without exposing its internal representation. This is essential for
maintaining invariants (e.g., a `Set` that guarantees no duplicates). The
`pub opaque` syntax makes the intent explicit.

### Why no qualified imports (e.g., `list.map`)?

Qualified access adds syntax complexity and is rarely needed when imports are
explicit. If two modules export `map`, the `as` alias mechanism resolves the
conflict with less syntax than a qualification system. This keeps the grammar
smaller.
