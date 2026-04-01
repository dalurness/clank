# Clank Roadmap

Current state of the implementation and what's next.

---

## Go Port Status (Phase 39)

The Go implementation (`go/`) is the active codebase. The TypeScript reference implementation (`src/`) is archived.

### ✅ Completed in Go Port

- Core pipeline: lexer, parser, desugarer, type checker, tree-walking interpreter, bytecode compiler, stack VM
- All core language features: ADTs, pattern matching, effects/handlers, modules, records, tuples, lists, do-blocks, closures, recursion
- **Effect alias propagation with cross-module resolution** — `makeEffectAliasResolver` wired into type checker; imported aliases expand correctly across module boundaries
- **Refinement types (micro-solver)** — QF_LIA Fourier-Motzkin solver with path condition tracking, arithmetic expression inference, match-arm propagation (E310/E311)
- **Affine type enforcement** — `affineCtx` tracks move/borrow/clone; raises E600 (use-after-move), E601 (not consumed), W600 (branch inconsistency)
- **Interface constraint validation** — impl registry with `where`-clause enforcement; 7 built-in interfaces (Clone, Show, Eq, Ord, Default, Into, From); `deriving` auto-generation; blanket From→Into; cross-package type checking via `ModuleTypeResolver`
- **Structural type comparison (typeEqual)** — fixed overly-permissive `t-generic` handling; `typeEqual` now compares named generics structurally
- **STM / affine ownership fix** — `txnWriteLog` write-log with `recordTVarSnapshot()`; `builtinAtomically`/`builtinOrElse` snapshot/restore on abort; nested `atomically` merges write-log into parent transaction
- **Polymorphic builtins (HM)** — `registerPolyBuiltin` with fresh type variable instantiation per call site; HM let-polymorphism for user-defined functions
- **Pretty-print layer** — `internal/pretty/` with `transform.go` and `expansion.go`; `clank pretty`/`clank terse` subcommands; `--pretty` flag on all commands
- **Package manager** — `clank pkg init|add|remove|resolve|verify` with `clank.pkg` manifest, `clank.lock` lockfile
- **Deriving Default** — `default(None)` type-checks correctly; derivable interfaces include Default
- Row polymorphism in records (row variables in TRecord, unification support)
- Async spawn/await/cancel in tree-walking evaluator (`internal/eval/async.go`)
- FFI via `CALL_EXTERN` opcode in compiler and VM
- CLI tooling: `run`, `check`, `eval`, `fmt`, `lint`, `doc`, `test`, `pkg`, `pretty`, `terse`
- Linter, formatter, doc search/show, test runner with `--filter`

### 🔲 Remaining Gaps (Go Port)

- **Full effect row unification** — effect annotations checked as flat sets; row-variable unification not performed; effect subtyping not enforced
- **WASM compilation backend** — not started; requires WASM 3.0 GC
- **Workspace orchestration** — `clank.workspace` manifest, `clank build`, parallel member builds, workspace lockfile not in Go port
- **Full async VM runtime** — VM has opcode stubs (`OpTASK_SPAWN`, `OpTASK_AWAIT`, etc.) but cooperative goroutine scheduling is not implemented; async works in tree-walker only
- **Embedding API** — no `ClankRuntime` host-language interop equivalent in Go port
- **Extended package registry** — `pkg search`, `pkg publish`, GitHub-backed registry not in Go port
- **Composite literal type inference** — some composite literals fall through to `TAny`; Clone checking only works on annotated types
- **`ref-swap` builtin** — spec says CAS-loop but no builtin entry
- **`http.stream-lines` / `proc.stream` / `io.stdin-lines`** — true demand-driven streaming not implemented in Go port

---

## TypeScript Reference Implementation (Archived)

The following describes the TypeScript implementation history. The Go port supersedes it.

---

## What's Complete and Working

### Core Language Pipeline
- **Lexer** (`src/lexer.ts`) — Full tokenization of all Clank syntax, all keywords including `affine`, `handle`, `resume`, `perform`, `effect`
- **Parser** (`src/parser.ts`) — Recursive descent parser producing full AST: definitions, type declarations, effect declarations, expressions, pattern matching, do-blocks, handle expressions, modules, imports
- **Desugarer** (`src/desugar.ts`) — Pipeline operator (`|>`), operator-to-function desugaring (`++` → `str.cat`), do-block expansion
- **Type Checker** (`src/checker.ts`) — Type inference, function signature checking, exhaustiveness checking for pattern matches, variant registry, affine enforcement, interface/impl dispatch, refinement micro-solving, where-constraint propagation, borrow checking
- **Tree-Walking Interpreter** (`src/eval.ts`) — Complete AST-level evaluator with closures, recursion, effects, handlers, pattern matching, records, tuples, lists
- **Bytecode Compiler** (`src/compiler.ts`) — AST to 122-opcode bytecode compilation, jump patching, closure capture, effect handler compilation, extern calls, iterator/STM opcodes
- **Stack VM** (`src/vm.ts`) — Full 122-opcode execution engine with call stack, data stack, closures, effect handler stack, continuations, structured trap errors, async scheduling, iterator heap objects, STM runtime

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
- STM runtime — atomic transactions, retry/or-else, global version clock, word ID collision fixed (26 tests, all passing)
- FFI extern — `CALL_EXTERN` opcode, extern table in BytecodeModule, value conversion helpers, `extern mod` block syntax (parser desugaring to individual extern-decl nodes)
- Tier-2 stdlib builtins — `std.http`, `std.srv`, `std.csv`, `std.proc`, `std.dt` (52 builtins, 32 tests)
- Streaming I/O — iterator heap object, 3 VM opcodes (ITER_NEW/NEXT/CLOSE), 38 iterator combinator builtins (word IDs 70-112), list-backed fast path and generator-backed general path, iter-spawn channel bridge, `for`/`in` desugar to `iter.each`/`iter.filter`/`iter.fold` for `Iter[a]` via runtime dispatch (`__for_each`/`__for_filter`/`__for_fold`), demand-driven `fs.stream-lines` (74 tests). All 38 combinators evaluate lazily via `nativeNext` pull-based generators; infinite iterators (`repeat`, `cycle`, `generate`, `unfold`) work without limits; consumer operations (`any`, `all`, `find`, `first`, `nth`) short-circuit correctly.
- Ref mutable state — REF_NEW (0xD0), REF_READ (0xD1), REF_WRITE (0xD2), REF_CAS (0xD3), REF_MODIFY (0xD4), REF_CLOSE (0xD5) opcodes in VM execution loop and compiler, with heap-allocated ref cells, affine type dispatch on REF_READ (take semantics) and REF_WRITE (put semantics) with empty-cell tracking, REF_MODIFY affine guard (E002 trap for affine values), handle counting with clone builtins for Ref (258) and TVar (259), TVar handle counting and closed-state tracking with closed checks on all TVar operations, TVar dispatch on REF_CLOSE (41 tests)
- HM type schemes — polymorphic builtin registration with fresh type variable instantiation per call site (replaces `tAny` sentinels for 20+ builtins including eq, head, tail, cons, map, filter, fold, clone, cmp, from, into), plus full let-polymorphism for user-defined functions (TypeScheme generalization with fresh t-var instantiation at call sites) (20 tests)
- Record pattern matching & spread — `{field, field | rest}` destructuring in match/let and `{field: val, ..base}` spread syntax, verified complete across all layers (parser, checker, eval, compiler/VM) (9 checker unit tests + 10 integration tests)
- Registry protocol & `pkg publish` — GitHub-backed Go-style registry protocol, `pkg search` and `pkg info` CLI subcommands with JSON output, `publish-entry.json` generation on publish (10 tests)
- Workspace orchestration — `clank.workspace` manifest parsing, workspace root discovery, member discovery with glob expansion, dependency graph with topological sort and cycle detection, cross-member resolution, parallel build execution with depth-level batching (`--jobs N`, fail-fast), workspace-level lockfile (`clank.lock` at workspace root), `--all`/`--package` flags on build/check/test, CLI subcommands (46 tests)
- Embedding API — `ClankRuntime` class for host language interop: `loadString`/`loadFile`, `register` host functions with custom library names, `call` exported Clank functions, JS↔Clank value conversion (26 tests)
- Queryable records — field tags (`@tag` annotations on record fields), tag projection (`T @tag`), `Pick<T, "f1" | "f2">` and `Omit<T, "f1">` type-level queries, all compile-time with zero runtime cost. Row polymorphism interaction verified. (15 tests)

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
- **`clank pkg add|verify|remove|search|info|publish`** — Package manager with `clank.lock` lockfile (SHA-256 integrity), stale lockfile warning on run, GitHub-backed registry protocol
- **`clank pkg workspace init|list|add|remove`** — Workspace management CLI
- **`clank build`** — Compile workspace members with `--all`, `--package`, `--jobs` flags
- **`clank.pkg`** — Package manifest parsing with local dependency resolution
- **`clank.workspace`** — Workspace manifest parsing with glob-based member discovery
- Structured JSON output by default (agent-native)
- Structured diagnostic envelopes with error codes, source locations, and phase tagging

### Testing
- 9 canonical spec examples (`test/examples/`)
- 300+ test files across implementation phases + tooling suites
- Phase-organized `.clk` test suites (phase1–phase6, phase8, fmt, examples, pretty)
- Phase6 type system tests (affine, interface, refinement, row polymorphism, derived impls)
- Phase8 extern tests (extern decl, extern mod blocks)
- TypeScript test suites: compiler (49), VM (61), checker (14), linter (14), fmt (29), doc (18), arg-parsing (8), test-runner (12)
- Streaming I/O tests (`streaming-io.test.ts`, 35 tests — iterator creation, combinators, close semantics)
- Workspace tests (`workspace.test.ts`, 46 tests — manifest parsing, discovery, topo-sort, cycle detection, parallel builds, workspace lockfile, CLI)
- Embedding tests (`embedding.test.ts`, 26 tests — runtime lifecycle, host function registration, value conversion, loadFile)
- Package manager tests (`pkg.test.ts`, 57 tests) integrated into main test runner
- Pretty-print tests (50 tests — terse/verbose expansion, scope handling)

---

## What's Specced but Not Implemented

Each of these has a complete specification document in `docs/`. They are designed but no source code exists for them yet.

### WASM Backend — referenced in `docs/compilation-target.md`
Compilation to WebAssembly as a secondary production target. Depends on WASM 3.0 GC support. Not started.

---

## What's Partially Implemented

### Type Checker Coverage
The checker (`src/checker.ts`) handles inference, signature checking, exhaustiveness, affine enforcement (E600/E601/W600), interface/impl dispatch with `where` constraints, refinement type micro-solving (QF_LIA with path conditions and arithmetic expression tracking), row polymorphism inference, effect alias/subtraction, borrow scope enforcement, where-constraint propagation through pipelines/params/returns, HM type schemes for builtins (20+ polymorphic builtins with fresh type variable instantiation per call site), and full HM let-polymorphism for user-defined functions (TypeScheme generalization/instantiation). Remaining gaps:
- Composite literal type inference falls through to tAny — Clone checking only works on annotated types
- Parameterized interface constraint type arg not validated at call sites
- Row variable subtraction detection in effect rows
- typeEqual overly permissive for t-generic types

### Package Manager
`clank.pkg` manifest parsing, local dependency resolution, `clank.lock` lockfile (SHA-256 integrity), `clank pkg add|verify|remove` CLI subcommands, GitHub-backed registry protocol (Go-style, no central registry), `pkg search|info|publish` CLI subcommands with JSON output, and `publish-entry.json` generation are all implemented. Remaining gaps: no persistent "known repos" configuration (each search requires `--repo` flags), no `[registries]` section in `clank.pkg` or global config.

### FFI / Interop
`CALL_EXTERN` opcode implemented for VM path with extern table and value conversion. `extern mod` block syntax implemented (parser desugars to individual extern-decl nodes with shared library and per-member `where` attributes). Embedding API (`ClankRuntime`) implemented with `loadString`/`loadFile`, `register`, `call`, and value conversion. Remaining gaps: `callAsync()` (requires async/Promise VM support), `ClankResult.effects`/`diagnostics` (needs deeper checker integration), `ClankValue.affine`/`consumed` tracking (requires affine system in VM), `RuntimeOptions.maxSteps` accepted but not enforced.

### Shared Mutable State (Ref)
All 6 Ref opcodes (REF_NEW/READ/WRITE/CAS/MODIFY/CLOSE) are implemented with affine type dispatch (take/put semantics on REF_READ/REF_WRITE with empty-cell tracking), REF_MODIFY affine guard (E002 trap for affine values), handle counting with clone builtins for Ref and TVar, TVar handle counting and closed-state tracking (closed checks on all TVar operations), and TVar dispatch on REF_CLOSE. Remaining gaps: structured scoping for ref cell lifetimes, `ref-swap` builtin (spec says CAS loop but no builtin entry exists).

### Streaming I/O
Iterator protocol, 38 combinators, fully lazy evaluation via `nativeNext` pull-based generators, channel-iterator bridge (`iter-spawn`), `for`/`in` desugar to iterator operations via runtime dispatch (`__for_each`/`__for_filter`/`__for_fold`), and demand-driven `fs.stream-lines` are all implemented and verified. All combinators evaluate lazily; infinite iterators (`repeat`, `cycle`, `generate`, `unfold`) work without limits; consumer operations (`any`, `all`, `find`, `first`, `nth`) short-circuit correctly. Remaining gaps:
- `http.stream-lines`, `proc.stream`, `io.stdin-lines` still read all data upfront — true demand-driven streaming for these requires async I/O integration with VM scheduler

### Workspace Orchestration
Manifest parsing, member discovery, dependency graph, topo-sort, CLI, parallel build execution (depth-level batching with `--jobs N`, fail-fast), workspace-level lockfile, and `--all`/`--package` flags on build/check/test are all implemented. Remaining gaps:
- `--keep-going` flag (continue after member failure)
- `--watch` mode (file-change-triggered rebuilds)
- Incremental rebuilds with signature stability (spec §5)
- `[deps.local]` as separate section (spec §2.1) — currently uses inline `{ path = "..." }` in `[deps]`

---

## Suggested Next Priorities

### Phase 2 Remaining Work

#### 1. Type Checker Hardening (Medium Impact)
HM type schemes for builtins, HM let-polymorphism for user-defined functions (TypeScheme generalization/instantiation), List/Tuple Ord dispatch, and record pattern matching are all complete. Remaining gaps: composite literal type inference (falls through to tAny), parameterized interface constraint validation at call sites, row variable subtraction detection in effect rows, typeEqual overly permissive for t-generic types. These are edge-case gaps — the checker handles all major type system features end-to-end.

#### 2. Streaming I/O Completion (Low Impact)
Lazy evaluation, channel-iterator bridge, `for`/`in` iterator desugar, and demand-driven `fs.stream-lines` are all complete. Remaining: `http.stream-lines`, `proc.stream`, `io.stdin-lines` still read all data upfront — true demand-driven streaming for these requires async I/O integration with VM scheduler.

#### 3. Package Registry Configuration (Low Impact)
Registry protocol, `pkg publish` with `publish-entry.json` generation, and `pkg search|info` with JSON output are all implemented. Remaining: persistent "known repos" configuration (currently requires `--repo` flags per search), potential `[registries]` section in `clank.pkg` or global config.

#### 4. Embedding API Polish (Low Impact)
Core embedding API (`ClankRuntime` with `loadString`/`loadFile`/`register`/`call`) is implemented. Remaining: `callAsync()`, `ClankResult.effects`/`diagnostics`, `ClankValue.affine`/`consumed` tracking, `RuntimeOptions.maxSteps` enforcement.

### Phase 3 — Static Binary (Ready to Begin Research)

All specced language features are now implemented. The TypeScript reference implementation covers: full Ref mutable state (6 opcodes with affine dispatch, REF_MODIFY affine guard, handle counting with clone builtins, TVar handle counting/closed-state tracking), queryable records (tags, projection, Pick/Omit — all compile-time), HM let-polymorphism for user-defined functions, streaming I/O with for-iter desugar and demand-driven fs.stream-lines, STM, workspace orchestration, embedding API, and the complete type system. Phase 2 remaining work is limited to checker edge-case hardening and ecosystem polish — none of it blocks Phase 3 research.

#### 5. Static Binary Research (Next Major Milestone)
Evaluate candidate approaches against Clank design goals: Deno compile (zero rewrite, bundled V8), Go rewrite (GC built-in, good AST ergonomics), Rust rewrite (max performance, no GC), self-hosting (Clank compiling itself). See OVERVIEW.md for evaluation criteria.

#### 6. WASM Backend (Future)
Secondary compilation target for deployment. Depends on WASM 3.0 GC. Lower priority than getting the static binary story resolved.
