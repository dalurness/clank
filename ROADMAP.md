# Clank Roadmap

Current state of the implementation and what's next.

---

## What's Complete and Working

### Core Language Pipeline
- **Lexer** (`src/lexer.ts`) — Full tokenization of all Clank syntax, all keywords including `affine`, `handle`, `resume`, `perform`, `effect`
- **Parser** (`src/parser.ts`) — Recursive descent parser producing full AST: definitions, type declarations, effect declarations, expressions, pattern matching, do-blocks, handle expressions, modules, imports
- **Desugarer** (`src/desugar.ts`) — Pipeline operator (`|>`), operator-to-function desugaring (`++` → `str.cat`), do-block expansion
- **Type Checker** (`src/checker.ts`) — Type inference, function signature checking, exhaustiveness checking for pattern matches, variant registry
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

### CLI Tooling
- **`clank <file>`** — Run a program (tree-walker)
- **`clank --vm <file>`** — Run via bytecode compiler + VM
- **`clank eval`** — Expression evaluation with persistent sessions (`--session`)
- **`clank check`** — Type-check without execution
- **`clank lint`** — Linting with selective rule enable/disable
- **`clank fmt`** — Canonical formatting with `--check`, `--diff`, `--stdin` modes
- **`clank doc search|show`** — Documentation extraction and search
- **`clank test`** — Test runner with `--filter` pattern support
- Structured JSON output by default (agent-native)
- Structured diagnostic envelopes with error codes, source locations, and phase tagging

### Testing
- 9 canonical spec examples (`test/examples/`)
- 50+ test files across 5 implementation phases
- Phase-organized test suites (phase1–phase5, fmt, examples)
- TypeScript test suites for compiler, checker, linter, doc, arg-parsing

---

## What's Specced but Not Implemented

Each of these has a complete specification document in `docs/`. They are designed but no source code exists for them yet.

### Refinement Types — `docs/refinement-types.md`
SMT solver integration for compile-time verification of value predicates (`Int{> 0}`, `[Rat]{len > 0}`). Includes liquid type inference for unannotated bindings, user-defined measures, and QF_UFLIRA decidable predicates. The parser accepts refinement syntax; the checker does not verify predicates against an SMT backend.

### Affine Type Enforcement — `docs/affine-types.md`
Move semantics, borrow checking (`&x`), explicit clone, and discard tracking. The lexer recognizes `affine` and `clone` keywords; the runtime does not enforce use-at-most-once or track resource consumption.

### Async Runtime — `docs/async-runtime.md`
The `<async>` effect, task spawning, structured concurrency, and cancellation. Fully specced with integration into the effect system. Not wired into the VM or interpreter.

### Software Transactional Memory — `docs/stm-runtime.md`
Runtime support for the `<state[S]>` effect via STM: atomic transactions, retry/conflict resolution, composable state. Specced at 30K words. Not implemented in the VM.

### Shared Mutable State & Iterators — `docs/shared-mutable-state.md`, `docs/vm-shared-state-iterators.md`
VM opcodes and runtime semantics for mutable state and lazy iterators. Specced but not wired into the VM execution loop.

### Streaming I/O — `docs/streaming-io.md`
Backpressure-aware streaming with pull-based and push-based models, integration with the effect system. Specced at 28K words.

### FFI / Interop — `docs/ffi-interop.md`
`extern` declarations for calling C, JavaScript, and Python from Clank, and embedding the Clank interpreter in host programs. Multi-host support with safety-preserving type mapping. No `extern` handling in parser or runtime.

### Package Manager — `docs/package-management.md`
`clank.pkg` manifest format, `clank.lock` lockfile, `clank pkg` CLI subcommands, registry protocol with type-signature search. Uses Minimal Version Selection (no SAT solving). Not implemented.

### Pretty-Print Layer — `docs/pretty-print-layer.md`
Bidirectional terse/verbose conversion (`clank pretty` / `clank terse`). Lexical substitution via static expansion table (e.g., `str.slc` ↔ `string.slice`). The formatter (`src/formatter.ts`) handles canonical formatting but not terse/verbose transformation.

### Workspace Orchestration — `docs/workspace-orchestration.md`, `docs/local-deps-workspace.md`
Multi-package workspaces, local dependency resolution, coordinated builds. Specced but depends on the package manager.

### Interface / Impl System (Typeclasses)
Interface declarations, impl blocks, `where` constraints, `deriving` clauses, `Self` type, and `pub opaque type`. Keywords are reserved in the lexer but have no parser rules, type checker logic, or runtime dispatch. Planned features: `Clone`, `Show`, `Eq`, `Ord`, `Default`, `Into`/`From` built-in interfaces; monomorphization; coherence checking; orphan impl warnings.

### Row Polymorphism Inference — `docs/row-polymorphism.md`
Rémy-style row unification for records and effect rows. The parser and checker handle records, but full row-polymorphic unification (open records, row variables, principal types) is not implemented in the type checker.

### Queryable Records — `docs/queryable-records.md`
SQL-like record querying syntax. Specced but not implemented.

### Range Literals and For-In — `docs/range-literal-syntax.md`, `docs/for-in-syntax.md`
Syntactic sugar for ranges (`1..10`) and for-in loops. Specced but not in the parser.

### Effect Aliases — `docs/effect-aliases.md`
Named aliases for common effect combinations. Specced but not in the checker.

### WASM Backend — referenced in `docs/compilation-target.md`
Compilation to WebAssembly as a secondary production target. Depends on WASM 3.0 GC support. Not started.

---

## What's Partially Implemented

### Effect Subtraction — `docs/effect-subtraction.md`
The effect system supports handlers that interpret effects, and the test suite includes effect subtraction tests (`test/phase3/`). Full row-polymorphic effect subtraction with open tails may have edge cases not yet covered.

### Type Checker Coverage
The checker (`src/checker.ts`) handles inference, signature checking, and exhaustiveness, but does not yet enforce:
- Refinement predicates (accepts syntax, doesn't verify)
- Affine use-at-most-once rules
- Full effect row unification with row variables
- Interface constraint solving (`where Ord a`)

### Standard Library
Built-in functions are registered in `src/builtin-registry.ts` and `src/builtins.ts`. Core operations (arithmetic, string, list, I/O) are implemented. The full Tier 1/Tier 2 stdlib catalog from the spec (`docs/stdlib-catalog.md`) is not fully wired — notably `std.http`, `std.srv`, `std.csv`, `std.proc` subprocess operations, and `std.dt` datetime functions.

---

## Suggested Next Priorities

### 1. Refinement Types (High Impact)
The defining feature of Clank's type system. Integrating an SMT solver (or a lightweight decision procedure for QF_UFLIRA) would enable compile-time verification of `Int{> 0}`, `[T]{len > 0}`, and relational predicates. This is the single feature most differentiating Clank from existing languages.

### 2. Affine Type Enforcement (High Impact)
Move/borrow/clone tracking in the checker. The syntax and semantics are fully specced. Enforcement would catch resource leaks (unclosed files, unreleased connections) at compile time — a core promise of the language.

### 3. Package Manager — Minimal Viable (Medium Impact)
`clank.pkg` manifest parsing, local dependency resolution, and `clank pkg init|add|resolve`. Even without a registry, local/workspace package management would enable multi-file projects beyond the module system.

### 4. Pretty-Print Layer (Medium Impact)
Terse/verbose conversion is a key agent UX feature — agents write terse, humans review verbose. The transformation is purely lexical (static expansion table), making it straightforward to implement.

### 5. Async Runtime (Medium Impact)
Wiring the `<async>` effect into the VM with task spawning and structured concurrency. Required for real-world agent programs that make concurrent HTTP requests or run background tasks.

### 6. FFI (Lower Priority for v1)
`extern` declarations and host function calling. Important for ecosystem integration but not blocking for self-contained Clank programs.

### 7. WASM Backend (Future)
Secondary compilation target for deployment. Depends on WASM 3.0 GC. Lower priority than getting the type system features working end-to-end.

### 8. Stdlib Expansion
Filling in the specced Tier 1 and Tier 2 modules — particularly `std.http`, `std.proc`, `std.srv`, and `std.dt` — to match the stdlib catalog in `docs/stdlib-catalog.md`.
