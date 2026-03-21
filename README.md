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

- Node.js (v18+)

### Install

```bash
git clone <repo-url> && cd clank
npm install
```

### Run a program

```bash
# Tree-walking interpreter (default)
npx tsx src/main.ts test/examples/01-factorial.clk

# Bytecode compiler + VM
npx tsx src/main.ts --vm test/examples/01-factorial.clk
```

### Run the test suite

```bash
npm test

# Or run with the VM backend
bash test/run-tests.sh "npx tsx src/main.ts" --vm
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
clank eval "<expr>"                  # Evaluate an expression
clank eval --session repl "<expr>"   # Persistent eval session
clank check <file|dir>               # Type-check without running
clank lint <file|dir>                # Lint beyond type-checking
clank lint --rule +name <file>       # Enable/disable specific rules
clank fmt <file|dir>                 # Canonical formatting
clank fmt --check <file>             # Check formatting (no write)
clank doc search <query>             # Search documentation
clank doc show <name>                # Show docs for a name
clank test [glob]                    # Run test modules
clank test --filter <pattern>        # Filter tests by pattern
```

All output is structured JSON by default (agent-native). Use `--json` / no flag to control format.

## Project Structure

```
src/
  main.ts          CLI entry point
  lexer.ts         Tokenizer
  parser.ts        Recursive descent parser
  ast.ts           AST type definitions
  desugar.ts       Pipeline/operator/do-block desugaring
  types.ts         Type representations
  checker.ts       Type checker
  eval.ts          Tree-walking interpreter
  compiler.ts      AST-to-bytecode compiler
  vm.ts            Stack-based bytecode VM (87 opcodes)
  builtins.ts      Built-in functions
  builtin-registry.ts  Shared builtin registry
  linter.ts        Linting rules
  formatter.ts     Code formatter
  doc.ts           Documentation extraction/search
  diagnostics.ts   Structured error formatting
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

**Working:**
- Full pipeline: lexer, parser, desugarer, type checker, tree-walking interpreter
- Bytecode compiler + 87-opcode stack VM (via `--vm` flag)
- Algebraic data types, pattern matching, recursion, closures
- Pipeline operator (`|>`) and operator desugaring
- Algebraic effects and effect handlers
- Module system with imports and visibility
- Records, tuples, lists
- Do-blocks
- CLI tooling: eval (with sessions), check, lint, fmt, doc, test
- Structured JSON diagnostics throughout
- 50+ test files across 5 implementation phases

**Specced but not yet implemented:**
- Refinement type checking (SMT solver integration)
- Affine type enforcement (move/borrow/clone tracking)
- Async runtime and `<async>` effect
- Software transactional memory (`<state>` effect at runtime)
- FFI (`extern` declarations)
- Package manager (`clank pkg`)
- Pretty-print layer (terse/verbose conversion)
- WASM compilation backend
- Row polymorphism inference
- Workspace orchestration

See [ROADMAP.md](ROADMAP.md) for details and priorities.

## License

Private — see package.json.
