# Clank

Clank is a strongly-typed programming language designed for AI agent authorship. It targets the bottlenecks agents actually face — context window pressure, token cost, generation reliability, and cold-start learning — rather than the human readability concerns that shaped every prior language.

The result: **concatenative-level density with dependent-type-level contracts and a toolchain small enough for an agent to hold in working memory.**

## Why Clank?

Every existing language optimizes for humans. Clank optimizes for agents:

| Human optimization | Clank's agent optimization |
|---|---|
| Verbose identifiers for scannability | Terse identifiers for density |
| Implicit side effects for convenience | Tracked effects for static reasoning |
| Runtime assertions for contracts | Refinement types for compile-time verification |
| Manual memory management or hidden GC | GC + affine types (memory is free, resources are tracked) |
| Large ecosystem with package discovery | Comprehensive stdlib eliminating dependency decisions |
| Opaque optimizing backends (LLVM) | Transparent toolchain that fits in context |
| Spec as reference manual (read on demand) | Spec fits in one load (cold-start learning) |

The full language spec fits in ~2300 tokens — readable in a single context window. See [RATIONALE.md](RATIONALE.md) for the reasoning behind every design decision.

## Quick Start

### Prerequisites

- Go 1.22+

### Build

```bash
git clone <repo-url> && cd clank/go
go build ./cmd/clank
```

### Run a program

```bash
# Tree-walking interpreter (default)
./clank test/examples/01-factorial.clk

# Bytecode compiler + VM
./clank --vm test/examples/01-factorial.clk
```

### Run the test suite

```bash
go test ./...
```

## Examples

### Factorial with type annotations

```clank
factorial : (n: Int) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)

main : () -> <io> () =
  print(show(factorial(0)))
  print(show(factorial(5)))
  print(show(factorial(10)))
```

Every function declares its effect row. `<>` means pure; `<io>` means performs I/O. The type system tracks this statically.

### Sum types and pattern matching

```clank
type Shape
  = Circle(Rat)
  | Rect(Rat, Rat)

area : (s: Shape) -> <> Rat =
  match s {
    Circle(r) => r * r * 3.14159
    Rect(w, h) => w * h
  }

main : () -> <io> () =
  print(show(area(Circle(5.0))))
  print(show(area(Rect(3.0, 4.0))))
```

### Pipelines and lambdas

```clank
evens-as-strings : (xs: [Int]) -> <> [Str] =
  xs |> filter(fn(x) => x % 2 == 0) |> map(show)

main : () -> <io> () =
  print(show(evens-as-strings([1, 2, 3, 4, 5, 6])))
```

The pipeline operator `|>` provides left-to-right composition: `a |> f(b)` desugars to `f(a, b)`. Simple data transformations read naturally without nesting.

### Do-blocks

```clank
process : (input: Str) -> <io> () = do {
  contents <- input
  length <- len(split(contents, ""))
  print("Processed: " ++ contents ++ " (length: " ++ show(length) ++ ")")
}
```

More examples in [`test/examples/`](test/examples/).

## Language Overview

### Syntax

- **Applicative-primary** with pipeline sugar (`|>`)
- All tokens are ASCII; each operator has exactly one meaning
- Short keywords: `fn`, `let`, `mod`, `pub`, `match`, `do`, `handle`
- All operators desugar to function calls (`++` = `str.cat`, `+` = addition)
- Comments: `# line comment`

### Type System

Clank combines three orthogonal mechanisms:

**Refinement types** — value contracts verified by SMT solver:
```clank
div : (n: Int, d: Int{> 0}) -> <> Int
mean : (xs: [Rat]{len > 0}) -> <> Rat
```

**Algebraic effects** (Koka-style) — behavioral contracts in function signatures:
```clank
<>                  # pure
<io>                # I/O
<exn[E]>            # may raise E
<state[S]>          # mutable state S
<async>             # async operations
<io, exn | e>       # open row (polymorphic tail)
```

User-defined effects with handlers:
```clank
effect DivError { div-by-zero : () -> <> () }

safe-div : (a: Int, b: Int) -> <DivError> Int =
  if b == 0 then div-by-zero() else a / b

handle safe-div(10, 0) {
  return(x) => Some(x),
  div-by-zero(_, k) => None
}
```

**Affine types** — resource protocols (files, connections must be consumed):
```clank
affine type File
# Using a File consumes it; further use is a compile error
# &x borrows read-only; clone &x duplicates explicitly
```

**Row polymorphism** — open records:
```clank
{name: Str | r}     # at least name, r = rest
```

**Interfaces** (typeclasses):
```clank
interface Show { show : (Self) -> <> Str }
type Color = Red | Green | Blue deriving (Eq, Show, Clone)
```

### Standard Library

Tier 1 (auto-imported, covers ~90% of agent tasks):

| Module | Key functions |
|--------|---------------|
| `std.str` | `len get slc has split join trim up lo rep cat fmt` |
| `std.json` | `enc dec get set path keys merge` |
| `std.fs` | `open close read write lines exists ls mkdir with` |
| `std.col` | `rev sort zip flat flatmap take drop find any all range group scan` |
| `std.http` | `get post put del req json` |
| `std.err` | `ok fail unwrap try throw some none` |
| `std.proc` | `run sh ok pipe bg wait kill exit` |
| `std.env` | `get set rm all has` |

Tier 2 (requires `use`): `std.srv`, `std.cli`, `std.dt`, `std.csv`, `std.log`, `std.test`, `std.rx`, `std.math`

All function names are terse (2-6 chars). All functions declare their effects. File/server/process handles are affine.

### Module System

```clank
mod math.stats
use std.io (print, read)
use std.list (map, filter)

pub mean : (xs: [Rat]{len > 0}) -> <> Rat = ...
```

1:1 file mapping (`src/math/stats.clk` = `mod math.stats`). Explicit imports only, no globs. Private by default, `pub` to export.

## CLI Usage

```bash
clank <file>                         # Run a .clk file (tree-walker)
clank --vm <file>                    # Run via bytecode compiler + VM
clank eval <file>                    # Evaluate and print the result
clank check <file>                   # Type-check without running
clank lint <file>                    # Lint beyond type-checking
clank lint --rule +name <file>       # Enable/disable specific rules
clank fmt <file>                     # Canonical formatting
clank fmt --check <file>             # Check formatting (no write)
clank doc search <query>             # Search documentation
clank doc show <name>                # Show docs for a name
clank test [files...]                # Run test modules
clank test --filter <pattern>        # Filter tests by pattern
clank pretty <file>                  # Expand terse identifiers to verbose form
clank terse <file>                   # Compress verbose identifiers to terse form
clank pkg init|add|remove|resolve|verify  # Package management
```

Use `--json` for structured JSON output (agent-native). Use `--pretty` flag to expand terse source before processing.

## Project Structure

```
go/
  cmd/clank/main.go          CLI entry point (subcommands: run, check, eval, fmt, lint, doc, test, pkg, pretty, terse)
  internal/
    lexer/                   Tokenizer
    parser/                  Recursive descent parser
    ast/                     AST type definitions
    desugar/                 Pipeline/operator/do-block desugaring
    checker/                 Type checker (HM inference, affine, refinement, interfaces, effect aliases)
    eval/                    Tree-walking interpreter (including async spawn/await/cancel)
    compiler/                AST-to-bytecode compiler
    vm/                      Stack-based bytecode VM with STM, Ref, TVar, channel/async stubs
    pretty/                  Terse/verbose identifier transformation
    formatter/               Source code formatter
    linter/                  Lint rules
    doc/                     Documentation extraction and search
    pkg/                     Package manifest parsing and local dependency resolution
    testrunner/              Test discovery and execution
    token/                   Token types and source locations
```

## Documentation

- **[SPEC.md](SPEC.md)** — Complete language specification (~2300 tokens, fits in one context window)
- **[RATIONALE.md](RATIONALE.md)** — Design decisions and the reasoning behind them
- **[docs/](docs/)** — Feature deep-dives:
  - [effect-system.md](docs/effect-system.md), [affine-types.md](docs/affine-types.md), [refinement-types.md](docs/refinement-types.md) — Type system details
  - [vm-instruction-set.md](docs/vm-instruction-set.md) — Complete 87-opcode VM spec
  - [gc-strategy.md](docs/gc-strategy.md) — Mark-sweep GC design
  - [module-system.md](docs/module-system.md) — Module and visibility rules
  - [stdlib-catalog.md](docs/stdlib-catalog.md) — Full standard library catalog
  - [tooling-spec.md](docs/tooling-spec.md) — Agent-native CLI design
  - [implementation-plan.md](docs/implementation-plan.md) — Phased build plan
- **[ROADMAP.md](ROADMAP.md)** — What's done, what's next

## Current Status

The Go implementation (`go/`) is the active codebase. All items below refer to it.

**Working:**
- Full pipeline: lexer, parser, desugarer, type checker, tree-walking interpreter
- Bytecode compiler + stack VM (via `--vm` flag)
- Algebraic data types, pattern matching, recursion, closures
- Pipeline operator (`|>`) and operator desugaring
- Algebraic effects and effect handlers
- Effect aliases with cross-module resolution (`makeEffectAliasResolver`)
- Module system with imports and visibility
- Records, tuples, lists
- Do-blocks
- **Refinement type checking** — QF_LIA micro-solver with path condition tracking and arithmetic expression inference (E310/E311)
- **Affine type enforcement** — move/borrow/clone tracking with `affineCtx`; use-after-move and resource-leak errors (E600/E601/W600)
- **Interface constraints** — impl registry, `where`-clause validation, 7 built-in interfaces (Clone, Show, Eq, Ord, Default, Into, From), `deriving` auto-generation, blanket From→Into impls
- **Software transactional memory** — `atomically`/`or-else` with write-log snapshot/restore; nested transactions merge into parent write-log
- **FFI** — `extern` declarations compiled to `CALL_EXTERN` opcode
- **Package manager** — `clank pkg init|add|remove|resolve|verify` with `clank.pkg` manifest and lockfile
- **Pretty-print layer** — `clank pretty`/`clank terse` bidirectional terse/verbose transformation; `--pretty` flag on all commands
- Row polymorphism in records (row variables unified in TRecord types)
- HM type schemes — polymorphic builtin registration with fresh type variable instantiation per call site
- Async spawn/await/cancel (tree-walking evaluator)
- CLI tooling: run, eval, check, lint, fmt, doc, test, pkg, pretty, terse
- Structured JSON diagnostics throughout

**Specced but not yet implemented (Go port):**
- WASM compilation backend
- Workspace orchestration (`clank.workspace`, `clank build`)
- Full async runtime in the bytecode VM (VM has opcode stubs; full scheduling requires cooperative goroutine support)
- Full effect row unification (effects are currently checked as flat sets; row-variable unification not performed)
- Embedding API (`ClankRuntime` host-language interop)
- `pkg search`, `pkg publish`, GitHub-backed registry

See [ROADMAP.md](ROADMAP.md) for details and priorities.

## License

Private — see package.json.
