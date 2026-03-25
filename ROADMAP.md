# Clank Roadmap

Current state of the implementation and what's next.

---

## What's Complete and Working

### Core Language Pipeline
- **Lexer** (`src/lexer.ts`) — Full tokenization of all Clank syntax, all keywords including `affine`, `handle`, `resume`, `perform`, `effect`
- **Parser** (`src/parser.ts`) — Recursive descent parser producing full AST: definitions, type declarations, effect declarations, expressions, pattern matching, do-blocks, handle expressions, modules, imports
- **Desugarer** (`src/desugar.ts`) — Pipeline operator (`|>`), operator-to-function desugaring (`++` → `str.cat`), do-block expansion
- **Type Checker** (`src/checker.ts`) — Type inference, function signature checking, exhaustiveness checking for pattern matches, variant registry, affine enforcement, interface/impl dispatch, refinement micro-solving, where-constraint propagation, borrow checking
- **Tree-Walking Interpreter** (`src/eval.ts`) — Complete AST-level evaluator with closures, recursion, effects, handlers, pattern matching, records, tuples, lists
- **Bytecode Compiler** (`src/compiler.ts`) — AST to 87-opcode bytecode compilation, jump patching, closure capture, effect handler compilation
- **Stack VM** (`src/vm.ts`) — Full 87-opcode execution engine with call stack, data stack, closures, effect handler stack, continuations, structured trap errors

### Language Features Running End-to-End
- Arithmetic (Int, Rat), booleans, strings
- `let` bindings, `if`/`then`/`else`, recursion
- Algebraic data types (`type Shape = Circle(Rat) | Rect(Rat, Rat)`)
- Pattern matching with exhaustiveness checking
- Pipeline operator (`|>`) with left-to-right data flow
- Lambda expressions (`fn(x) => ...`)
- Do-blocks for sequential composition
- Algebraic effects and effect handlers (`effect`, `handle`, `resume`)
- Module system (`mod`, `use`, `pub`) with multi-file imports
- Records with field access, update, and spread
- Tuples, lists, and collection operations
- Higher-order functions (`map`, `filter`, `flatmap`, etc.)
- String operations, concatenation (`++`)
- Range literals (`1..10`, `1..=10`) desugaring to `range()` calls
- `for`/`in` loops (4 forms: map, filter, fold, flatmap)
- Effect aliases (`alias IO = <io, exn>`) with parameterized substitution
- Effect subtraction (`<io, exn> \ io`) with alias expansion
- Affine type enforcement (`affine` keyword, move/clone/borrow/discard tracking, E600/E601/W600)
- Interface/impl system (7 built-in interfaces: Clone, Show, Eq, Ord, Default, Into, From; `where` constraints, `deriving` auto-generation)
- Refinement type micro-solver (`Int{> 0}`, QF_LIA Fourier-Motzkin, ~480-line solver) with path condition tracking, arithmetic expression inference, and match-arm propagation
- Row polymorphism inference (Rémy-style row unification, row variables in checker)
- From/Into interface with blanket impls and typeArgs dispatch
- Async runtime — `<async>` effect, `spawn`/`await`/`cancel`, structured task groups, cooperative cancellation (tree-walker + VM)
- Borrow `&T` semantic type with scope enforcement (lambda returns, compound types — E603)
- Where-constraint propagation through function parameters, return values, and pipelines
- Compiler/VM interface dispatch with derived impl codegen (Show/Eq/Clone/Ord for records)
- STM runtime — atomic transactions, retry/or-else, global version clock (26 tests)
- FFI extern — `CALL_EXTERN` opcode, extern table in BytecodeModule, value conversion helpers, `extern mod` block syntax (parser desugaring to individual extern-decl nodes)
- Tier-2 stdlib builtins — `std.http`, `std.srv`, `std.csv`, `std.proc`, `std.dt` (52 builtins, 32 tests)
- Streaming I/O — iterator heap object, 3 VM opcodes (ITER_NEW/NEXT/CLOSE), 38 iterator combinator builtins (word IDs 70-112), list-backed fast path and generator-backed general path (35 tests)
- Workspace orchestration — `clank.workspace` manifest parsing, workspace root discovery, member discovery with glob expansion, dependency graph with topological sort and cycle detection, cross-member resolution (34 tests)

### CLI Tooling
- **`clank <file>`** — Run a program (tree-walker)
- **`clank --vm <file>`** — Run via bytecode compiler + VM
- **`clank eval`** — Expression evaluation with persistent sessions (`--session`)
- **`clank check`** — Type-check without execution
- **`clank lint`** — Linting with selective rule enable/disable (W100-W106)
- **`clank fmt`** — Canonical formatting with `--check`, `--diff`, `--stdin` modes
- **`clank doc search|show`** — Documentation extraction and search
- **`clank test`** — Test runner with `--filter` pattern support
- **`clank pretty`** — Terse-to-verbose pretty-print expansion
- **`clank terse`** — Verbose-to-terse compression
- **`clank pkg add|verify|remove`** — Package manager with `clank.lock` lockfile (SHA-256 integrity), stale lockfile warning on run
- **`clank pkg workspace init|list|add|remove`** — Workspace management CLI
- **`clank.pkg`** — Package manifest parsing with local dependency resolution
- **`clank.workspace`** — Workspace manifest parsing with glob-based member discovery
- Structured JSON output by default (agent-native)
- Structured diagnostic envelopes with error codes, source locations, and phase tagging

### Testing
- 9 canonical spec examples (`test/examples/`)
- 230+ test files across 7 implementation phases
- Phase-organized test suites (phase1–phase7, fmt, examples, pkg, streaming, workspace)
- Phase6 type system tests (affine, interface, refinement, row polymorphism, derived impls)
- Phase7 async runtime tests (spawn/await, task groups, cancellation, nested groups)
- Streaming I/O tests (35 tests — iterator creation, combinators, close semantics)
- Workspace tests (34 tests — manifest parsing, discovery, topo-sort, cycle detection, CLI)
- TypeScript test suites for compiler, checker, linter, doc, arg-parsing
- Package manager tests (57 tests) integrated into main test runner

---

## What's Specced but Not Implemented

Each of these has a complete specification document in `docs/`. They are designed but no source code exists for them yet.

### Shared Mutable State (Ref opcodes) — `docs/shared-mutable-state.md`, `docs/vm-shared-state-iterators.md`
VM opcodes for Ref mutable state primitives. STM runtime and iterator opcodes are now implemented; remaining Ref opcodes (REF_NEW/GET/SET) not yet wired into the VM execution loop.

### Queryable Records — `docs/queryable-records.md`
SQL-like record querying syntax. Specced but not implemented.

### WASM Backend — referenced in `docs/compilation-target.md`
Compilation to WebAssembly as a secondary production target. Depends on WASM 3.0 GC support. Not started.

---

## What's Partially Implemented

### Type Checker Coverage
The checker (`src/checker.ts`) handles inference, signature checking, exhaustiveness, affine enforcement (E600/E601/W600), interface/impl dispatch with `where` constraints, refinement type micro-solving (QF_LIA with path conditions and arithmetic expression tracking), row polymorphism inference, effect alias/subtraction, borrow scope enforcement, and where-constraint propagation through pipelines/params/returns. Remaining gaps:
- Composite literal type inference falls through to tAny — Clone checking only works on annotated types
- Parameterized interface constraint type arg not validated at call sites
- Full HM let-polymorphism not implemented (type vars are module-scoped, no generalization/instantiation)
- Row variable subtraction detection in effect rows
- Builtin interface method types (clone, into, from) use tAny — need per-call-site instantiation
- typeEqual overly permissive for t-generic types
- No List/Tuple cmp (Ord) VM dispatch

### Package Manager
`clank.pkg` manifest parsing, local dependency resolution, `clank.lock` lockfile (SHA-256 integrity), and `clank pkg add|verify|remove` CLI subcommands are implemented. Registry protocol and `clank pkg publish` are not yet implemented.

### FFI / Interop
`CALL_EXTERN` opcode implemented for VM path with extern table and value conversion. `extern mod` block syntax implemented (parser desugars to individual extern-decl nodes with shared library and per-member where attributes). Embedding API not started.

---

## Suggested Next Priorities

### 1. Type Checker Hardening (High Impact)
Remaining gaps: composite literal type inference (falls through to tAny), full HM let-polymorphism (generalization/instantiation), parameterized interface constraint validation at call sites, List/Tuple Ord dispatch, builtin interface method type instantiation, zero-param lambda inference.

### 2. Record Pattern Matching & Spread Operator (High Impact)
`{field, field | rest}` destructuring in match/let and `{field: val, ..base}` spread syntax for records. Row polymorphism infrastructure is in place; this completes the record ergonomics story.

### 3. Registry Protocol & `pkg publish` (Medium Impact)
Complete the package ecosystem with remote registry support. Local package management and workspace orchestration are fully functional.

### 4. Ref Mutable State Opcodes (Medium Impact)
Wire remaining REF_NEW/GET/SET opcodes into VM execution loop. STM and iterator opcodes are implemented; this completes the mutable state story.

### 5. Embedding API (Medium Impact)
Host language embedding for FFI. `CALL_EXTERN` opcode and `extern mod` block syntax are both implemented; this adds the host-side API for embedding Clank in other runtimes.

### 6. WASM Backend (Future)
Secondary compilation target for deployment. Depends on WASM 3.0 GC. Lower priority than getting the ecosystem features working end-to-end.

### 7. Queryable Records (Future)
SQL-like record querying. Specced but lower priority than type system hardening and package ecosystem.
