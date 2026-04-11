# Clank Roadmap

Current state of the implementation and what's next.

---

## Architecture

Clank is implemented in Go. The execution model is: source → lexer → parser → desugarer → type checker → bytecode compiler → stack VM. There is no tree-walking interpreter — the VM is the sole execution engine.

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
- Effect row unification (proper row-based, not flat sets)
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
| `http` | `get`, `post`, `put`, `del`, `set-timeout` | Complete |
| `rx` | `ok`, `find`, `replace`, `split` | Complete |
| `math` | `abs`, `min`, `max`, `floor`, `ceil`, `sqrt` | Complete |
| `str` | `get`, `slc`, `has`, `idx`, `ridx`, `pfx`, `sfx`, `up`, `lo`, `rep`, `rep1`, `pad`, `lpad`, `rev`, `lines`, `words`, `chars`, `int`, `rat` | Complete |
| `col` | `rev`, `sort`, `sortby`, `uniq`, `zip`, `unzip`, `flat`, `flatmap`, `take`, `drop`, `nth`, `find`, `any`, `all`, `count`, `enum`, `chunk`, `win`, `intersperse`, `rep`, `sum`, `prod`, `min`, `max`, `group`, `scan` | Complete |
| `iter` | 38 lazy iterator combinators (`of`, `range`, `collect`, `map`, `filter`, `take`, `drop`, `fold`, `count`, `sum`, `any`, `all`, `find`, `each`, `drain`, `enumerate`, `chain`, `zip`, `take-while`, `drop-while`, `flatmap`, `first`, `last`, `join`, `repeat`, `once`, `empty`, `unfold`, `scan`, `dedup`, `chunk`, `window`, `intersperse`, `cycle`, `nth`, `min`, `max`, `generate`) | Complete |
| `dt` | `now`, `unix`, `from`, `to`, `parse`, `fmt`, `add`, `sub`, `tz`, `iso`, `ms`, `sec`, `min`, `hr`, `day` | Complete |
| `csv` | `dec`, `enc`, `decf`, `encf`, `hdr`, `rows`, `maps`, `opts` | Complete |
| `log` | `trace`, `debug`, `info`, `warn`, `error`, `level`, `ctx`, `json` | Complete |
| `cli` | `args`, `parse`, `opt`, `req`, `def`, `get`, `flag`, `pos` | Complete |
| `srv` | `new`, `get`, `post`, `put`, `del`, `start`, `stop`, `res`, `json`, `hdr`, `mw` | Complete |
| streaming | `fs.stream-lines`, `http.stream-lines`, `proc.stream`, `io.stdin-lines` | Complete |

### Async & Concurrency
- Goroutine-backed `spawn` — each task runs in a real goroutine for parallel I/O
- `await`, task groups, cancellation, shielding
- Context threading — cancellation interrupts blocked I/O (sleep, HTTP, proc, channels)
- Channels: `channel`, `send`, `recv`, `try-recv`, `close-sender`, `close-receiver`
- `sleep` (interruptible via context cancellation)
- Software transactional memory: `atomically`, `or-else`, `retry`, `tvar-new/read/write/take/put`
- Refs: `ref-new`, `ref-read`, `ref-write`, `ref-cas`, `ref-modify`, `ref-close`, `ref-swap`

### Module System
- File-based module resolution (directory and flat styles)
- Import linking via `internal/loader` (resolves all `use` declarations before compilation)
- `pub`/private visibility
- Transitive imports
- Package management with GitHub dependency support

### Tooling
- `clank run` — execute programs
- `clank check` — type-check without running
- `clank eval` — evaluate and print result
- `clank fmt` — canonical formatting
- `clank lint` — 6 lint rules with enable/disable
- `clank doc [target]` — list/search/show docs. Target is any of: empty (current project), `/path` or `./path` (project-relative file or dir), `github.com/user/repo[@ref]` (remote fetch, prompts y/N), or an installed dep name `<lib>[@version]`
- `clank test` — test runner with `--filter`
- `clank spec` — print embedded language specification
- `clank pkg init|add|remove|resolve|verify` — package management
- `clank pretty`/`clank terse` — terse/verbose identifier transformation
- All commands support `--json` for structured output

### Infrastructure
- Cross-platform prebuilt binaries (linux, macOS, Windows; amd64, arm64)
- GitHub Actions CI (build + test on all platforms) and release automation
- One-line install: `curl -fsSL .../install.sh | sh`

### Testing
- 382+ integration and unit tests (Go)
- Coverage: core language, type system, refs, STM, async/concurrency, all stdlib modules, CLI commands, streaming I/O, lazy iterators
- All tests run on every push via GitHub Actions CI

---

## What's Not Yet Implemented

### Language Features
- WASM compilation backend (depends on WASM GC proposal)

### Infrastructure
- `pkg search`, `pkg publish`, package discovery website
- Workspace orchestration (`clank.workspace`, parallel builds)
- Self-hosting — Clank compiling itself

---

## Suggested Priorities

### Near Term
1. **Agent workflow examples** — End-to-end demos: fetch API → parse → transform → write, CSV pipeline, HTTP server bot
2. **Package discovery** — Website where agents can find community packages

### Medium Term
4. **Workspace orchestration** — Multi-package projects with parallel builds
5. **More string/format operations** — `str.fmt` (printf-style), `str.enc`/`str.dec` (base64, URL encoding)

### Long Term
6. **WASM backend** — Deploy Clank programs to browsers/edge
7. **Self-hosting** — Clank compiling itself
