# Clank Roadmap

Current state of the implementation and what's next.

---

## Architecture

Clank is implemented in Go (`go/`). The execution model is: source → lexer → parser → desugarer → type checker → bytecode compiler → stack VM. There is no tree-walking interpreter — the VM is the sole execution engine.

The TypeScript reference implementation has been archived and removed from the repository. Its history is preserved in git.

---

## What's Complete and Working

### Core Language
- Full pipeline: lexer, parser, desugarer, type checker, bytecode compiler, stack VM
- Algebraic data types with pattern matching and exhaustiveness checking
- Pipeline operator (`|>`) and operator desugaring
- Lambda expressions, closures, recursion (with tail-call optimization)
- Do-blocks, let-bindings, if/then/else, for-in loops (map, filter, fold forms)
- Records with field access, update, spread, row polymorphism, pattern matching
- Tuples, lists, and collection operations (map, filter, fold, flat-map, range, zip, etc.)
- String operations, concatenation (`++`)
- Range literals (`1..10`, `1..=10`)

### Type System
- Hindley-Milner type inference with let-polymorphism
- Algebraic effects and handlers with resumptions
- Effect aliases with cross-module resolution
- Refinement types — QF_LIA micro-solver with path condition tracking
- Affine type enforcement — move/borrow/clone tracking (E600/E601/W600)
- Interface system — 7 built-in interfaces (Show, Eq, Ord, Clone, Default, From, Into)
- `deriving` auto-generation for variants and records
- Row polymorphism in records

### Standard Library (implemented as VM builtins)
| Module | Functions | Status |
|--------|-----------|--------|
| Core | arithmetic, comparison, logic, strings, lists, tuples, effects | Complete |
| `fs` | `read`, `write`, `exists`, `ls`, `mkdir`, `rm` | Complete |
| `json` | `enc`, `dec`, `get`, `set`, `keys`, `merge` | Complete |
| `env` | `get`, `set`, `has`, `all` | Complete |
| `proc` | `run`, `sh`, `exit` | Complete |
| `http` | `get`, `post`, `put`, `del` | Complete |
| `rx` | `ok`, `find`, `replace`, `split` | Complete |
| `math` | `abs`, `min`, `max`, `floor`, `ceil`, `sqrt` | Complete |

### Async & Concurrency
- `spawn`, `await`, task groups, cancellation, shielding
- Channels: `channel`, `send`, `recv`, `try-recv`, `close-sender`, `close-receiver`
- `sleep` (actual delay, not stub)
- Software transactional memory: `atomically`, `or-else`

### Module System
- File-based module resolution (directory and flat styles)
- Import linking via `internal/loader` (resolves all `use` declarations before compilation)
- `pub`/private visibility
- Transitive imports

### Tooling
- `clank run` — execute programs
- `clank check` — type-check without running
- `clank eval` — evaluate and print result
- `clank fmt` — canonical formatting
- `clank lint` — 6 lint rules with enable/disable
- `clank doc search|show` — documentation extraction and search
- `clank test` — test runner with `--filter`
- `clank pkg init|add|remove|resolve|verify` — package management
- `clank pretty`/`clank terse` — terse/verbose identifier transformation
- All commands support `--json` for structured output

### Testing
- 213+ integration tests across phases 1-7, examples, and stdlib
- All tests pass on the VM execution path
- Test phases: arithmetic, data structures, effects, modules/records, interfaces/refinements/affine, async/channels, stdlib

---

## What's Specced but Not Yet Implemented

### Tier 2 Stdlib (documented in `docs/stdlib-catalog.md`)
- `std.srv` — HTTP server
- `std.cli` — Argument parsing
- `std.dt` — DateTime
- `std.csv` — CSV encode/decode
- `std.log` — Structured logging
- `std.col` — Extended collection operations (sort, group, chunk, window, etc.)

### Language Features
- Full effect row unification (effects currently checked as flat sets)
- Streaming I/O (`fs.stream-lines`, `http.stream-lines`, `proc.stream`)
- Iterator combinators (38 declared but most not yet implemented in VM)

### Infrastructure
- WASM compilation backend (depends on WASM 3.0 GC)
- `pkg search`, `pkg publish`, GitHub-backed registry protocol
- Workspace orchestration (`clank.workspace`, parallel builds)

---

## Suggested Priorities

### Near Term
1. **Tier 2 stdlib** — `std.srv` (HTTP server) and `std.cli` (arg parsing) are highest value for agent workflows
2. **Iterator combinators** — Word IDs 73-107 are allocated but dispatch not implemented
3. **Effect row unification** — Would improve type error quality significantly

### Medium Term
4. **Package registry** — Enable agents to discover and use community packages
5. **Workspace orchestration** — Multi-package projects

### Long Term
6. **WASM backend** — Deploy Clank programs to browsers/edge
7. **Self-hosting** — Clank compiling itself (stretch goal)
