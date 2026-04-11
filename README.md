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

The full language spec fits in ~3500 tokens — readable in a single context window. See [RATIONALE.md](RATIONALE.md) for the reasoning behind every design decision.

## Install

### One-liner (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/dalurness/clank/main/install.sh | sh
```

### From source

```bash
git clone https://github.com/dalurness/clank.git && cd clank
go build -o clank ./cmd/clank
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/dalurness/clank/main/install.ps1 | iex
```

## Quick Start

```bash
clank run test/examples/01-factorial.clk
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
  print(show(factorial(10)))
```

Every function declares its effect row. `<>` means pure; `<io>` means performs I/O.

### Sum types and pattern matching

```clank
type Shape = Circle(Rat) | Rect(Rat, Rat)

area : (s: Shape) -> <> Rat =
  match s {
    Circle(r) => r * r * 3.14159
    Rect(w, h) => w * h
  }

main : () -> <io> () =
  let shapes = [Circle(5.0), Rect(3.0, 4.0)]
  for s in shapes do print(show(area(s)))
```

### Pipelines, lambdas, and stdlib

```clank
main : () -> <io> () =
  # Read a JSON config file
  let data = fs.read("config.json")
  let config = json.dec(data)

  # Extract and display a field
  match json.get(config, "name") {
    Some(name) => print("Hello, " ++ show(name))
    None => print("no name found")
  }

  # Filter and transform a list
  let nums = range(1, 20)
  let evens = nums |> filter(fn(x) => x % 2 == 0) |> map(show)
  print(join(evens, ", "))
```

More examples in [`test/examples/`](test/examples/).

## Language Overview

### Syntax

- **Applicative-primary** with pipeline sugar (`|>`)
- All tokens are ASCII; each operator has exactly one meaning
- Short keywords: `fn`, `let`, `mod`, `pub`, `match`, `do`, `handle`
- All operators desugar to function calls (`++` = `str.cat`, `+` = `add`)
- Comments: `# line comment`

### Type System

Three orthogonal mechanisms:

**Refinement types** — value contracts verified at compile time:
```clank
div : (n: Int, d: Int{> 0}) -> <> Int
```

**Algebraic effects** (Koka-style) — behavioral contracts in signatures:
```clank
<>                  # pure
<io>                # I/O
<exn[E]>            # may raise E
<async>             # async operations
```

**Affine types** — resource protocols (files, connections must be consumed):
```clank
affine type File = File(Int)
```

Plus: row polymorphism for records, interfaces with `deriving`, pattern matching with exhaustiveness checking.

### Standard Library (implemented)

All stdlib functions are available without imports, using module-qualified names:

| Module | Functions | Purpose |
|--------|-----------|---------|
| `fs` | `read`, `write`, `exists`, `ls`, `mkdir`, `rm` | File I/O |
| `json` | `enc`, `dec`, `get`, `set`, `keys`, `merge` | JSON encode/decode |
| `env` | `get`, `set`, `has`, `all` | Environment variables |
| `proc` | `run`, `sh`, `exit` | Process execution |
| `http` | `get`, `post`, `put`, `del` | HTTP client |
| `rx` | `ok`, `find`, `replace`, `split` | Regex |
| `math` | `abs`, `min`, `max`, `floor`, `ceil`, `sqrt` | Math |

Plus core builtins: arithmetic, comparison, logic, string ops, list ops (`map`, `filter`, `fold`, `flat-map`, `range`, `zip`, etc.), tuples, effects, async (`spawn`, `await`, `channel`, `send`, `recv`).

### Module System

```clank
mod math.stats
use mathlib (double, triple)

pub mean : (xs: [Rat]) -> <> Rat = ...
```

1:1 file mapping. Explicit imports only. Private by default, `pub` to export.

## CLI

```bash
clank run <file>                # Run a .clk file
clank check <file>              # Type-check without running
clank eval <file>               # Evaluate and print result
clank fmt <file>                # Canonical formatting
clank lint <file>               # Lint source code
clank doc [target]              # List documentation for a target (default: current project)
clank doc search <q> [target]   # Search docs within target (or builtins)
clank doc show <name> [target]  # Show one entry in detail
clank test [files...]           # Run tests
clank pkg init|add|remove       # Package management
clank pretty <file>             # Expand terse identifiers
clank terse <file>              # Compress to terse form
clank version                   # Print the Clank version
clank update                    # Update to the latest version
```

All commands support `--json` for structured output.

## Project Structure

```
cmd/clank/main.go            CLI entry point
internal/
  lexer/                     Tokenizer
  parser/                    Recursive descent parser
  ast/                       AST type definitions
  desugar/                   Pipeline/operator/do-block desugaring
  checker/                   Type checker (HM, affine, refinement, interfaces, effects)
  compiler/                  AST-to-bytecode compiler
  vm/                        Stack-based bytecode VM + stdlib builtins
  loader/                    Module resolution and import linking
  pretty/                    Terse/verbose identifier transformation
  formatter/                 Source code formatter
  linter/                    Lint rules
  doc/                       Documentation extraction and search
  pkg/                       Package manifest and dependency resolution
  testrunner/                Test discovery and execution
  token/                     Token types and source locations
test/
  phase1-7/                  Language feature tests by phase
  stdlib/                    Standard library tests
  examples/                  Example programs
```

## Documentation

- **[SPEC.md](SPEC.md)** — Complete language specification (~3500 tokens, fits in one context window)
- **[RATIONALE.md](RATIONALE.md)** — Design decisions and the reasoning behind them
- **[ROADMAP.md](ROADMAP.md)** — What's done, what's next
- **[docs/](docs/)** — Feature deep-dives (effect system, affine types, refinement types, VM instruction set, etc.)

## Current Status

The Go implementation is the sole active codebase. Architecture: bytecode compiler + stack VM.

**Working:**
- Full pipeline: lexer, parser, desugarer, type checker, bytecode compiler, VM
- Algebraic data types, pattern matching, recursion, closures, pipelines
- Algebraic effects and handlers with resumptions
- Module system with imports, visibility, and transitive resolution
- Records with row polymorphism, tuples, lists, for-in loops
- Refinement type checking (QF_LIA micro-solver)
- Affine type enforcement (move/borrow/clone tracking)
- Interfaces with `deriving` (Show, Eq, Ord, Clone, Default, From, Into)
- Software transactional memory (atomically/or-else)
- Async: spawn, await, task groups, cancellation, channels
- Standard library: fs, json, env, proc, http, rx, math, str (19 string ops)
- Extended stdlib: col (26 collection ops), iter (38 lazy iterator combinators), dt, csv, log, cli, srv (HTTP server)
- Streaming I/O: fs.stream-lines, http.stream-lines, proc.stream, io.stdin-lines
- Package manager with manifest and lockfile
- CLI tooling with structured JSON diagnostics

**Not yet implemented:**
- WASM compilation backend
- `pkg search`, `pkg publish`, registry protocol

## License

MIT — see [LICENSE](LICENSE).
