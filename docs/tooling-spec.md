# Clank Tooling Specification v1.0

**Task:** TASK-060
**Mode:** Spec
**Date:** 2026-03-17
**Dependencies:** plan/SPEC.md, plan/features/module-system.md

---

## 1. Overview

Clank's tooling is agent-native: every tool emits structured JSON, accepts structured input, and is designed for programmatic consumption first. Human-readable output is an optional presentation layer, never the primary interface.

### Design Goals

1. **Structured I/O** — all output is JSON; all input accepts flags or stdin JSON
2. **Queryable** — docs, types, and symbols are searchable by type signature, effect, and name
3. **Incremental** — verify one function at a time; cache and reuse previous results
4. **Composable** — tools are Unix-style: small, pipeable, combinable
5. **Context-minimal** — an agent should never need to load an entire codebase to accomplish a task

---

## 2. CLI Structure

The Clank CLI is a single binary `clank` with subcommands.

```
clank <command> [flags] [args]
```

### 2.1 Global Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` | `json\|human` | `json` | Output format |
| `--quiet` | bool | false | Suppress non-error output |
| `--color` | `auto\|always\|never` | `auto` | Color in human mode |
| `--project` | path | auto-detected | Project root (walks up to find `clank.pkg`) |

Default is `json`. The `human` format is a pretty-printed rendering of the same JSON structure — it adds no information, only formatting. Tools and agents should always use `json`.

### 2.2 Subcommands

| Command | Purpose |
|---------|---------|
| `clank run <file>` | Interpret a `.clk` file |
| `clank check <file\|dir>` | Type-check without running |
| `clank build <dir>` | Compile to bytecode |
| `clank eval <expr>` | Evaluate a single expression |
| `clank query <query>` | Search docs/types/symbols |
| `clank doc <module>` | Emit module documentation |
| `clank pkg <subcommand>` | Package management |
| `clank test [glob]` | Run test modules |
| `clank fmt <file\|dir>` | Canonical formatting |
| `clank lint <file\|dir>` | Lint beyond type-checking |

---

## 3. Structured Error Format

All errors, warnings, and diagnostics share a single JSON schema. This is already partially implemented in the reference interpreter.

### 3.1 Diagnostic Schema

```json
{
  "severity": "error" | "warning" | "info",
  "code": "E301",
  "phase": "lex" | "parse" | "desugar" | "check" | "eval" | "link",
  "message": "type mismatch: expected Int, got Str",
  "location": {
    "file": "src/math/stats.clk",
    "line": 12,
    "col": 5,
    "end_line": 12,
    "end_col": 18
  },
  "context": "in definition of mean",
  "related": [
    {
      "message": "expected type declared here",
      "location": { "file": "src/math/stats.clk", "line": 11, "col": 10, "end_line": 11, "end_col": 13 }
    }
  ],
  "fix": {
    "description": "change argument type to Int",
    "edits": [
      { "file": "src/math/stats.clk", "line": 12, "col": 5, "end_col": 8, "replacement": "Int" }
    ]
  }
}
```

### 3.2 Field Definitions

| Field | Required | Description |
|-------|----------|-------------|
| `severity` | yes | `error`, `warning`, or `info` |
| `code` | yes | Stable identifier (e.g., `E301`). Never changes meaning across versions. |
| `phase` | yes | Which compiler phase produced this diagnostic |
| `message` | yes | One-line description. No ANSI codes, no line-art. |
| `location` | yes | Source span. `end_line`/`end_col` optional (default to `line`/`col`). |
| `context` | no | Enclosing scope (e.g., `in definition of foo`, `in handler for MyEffect`) |
| `related` | no | Array of related locations with explanatory messages |
| `fix` | no | Suggested fix with concrete text edits |

### 3.3 Error Code Ranges

| Range | Phase |
|-------|-------|
| E100–E199 | Lexer errors |
| E200–E299 | Parse errors |
| E300–E399 | Type errors |
| E400–E499 | Effect errors |
| E500–E599 | Module/import errors |
| E600–E699 | Affine/ownership errors |
| E700–E799 | Refinement errors (SMT) |
| W100–W499 | Warnings (same ranges, W prefix) |

### 3.4 Command Output Envelope

Every command wraps its output in a standard envelope:

```json
{
  "ok": true,
  "data": { ... },
  "diagnostics": [ ... ],
  "timing": { "total_ms": 42, "phases": { "lex": 2, "parse": 8, "check": 30, "eval": 2 } }
}
```

When `ok` is `false`, `data` is `null` and `diagnostics` contains at least one error. Warnings may appear even when `ok` is `true`. The `timing` field is always present — agents use it to detect performance regressions.

---

## 4. Queryable Documentation System

### 4.1 `clank doc`

Emits structured documentation for a module or symbol.

```bash
clank doc std.list               # full module docs
clank doc std.list.map           # single symbol
clank doc --type "([a], (a) -> b) -> [b]"  # search by type
```

### 4.2 Module Documentation Output

```json
{
  "module": "std.list",
  "package": "std",
  "exports": [
    {
      "name": "map",
      "kind": "function",
      "type": "([a], (a) -> <e> b) -> <e> [b]",
      "effects": ["e"],
      "constraints": [],
      "doc": "Apply f to each element of xs."
    },
    {
      "name": "filter",
      "kind": "function",
      "type": "([a], (a) -> <> Bool) -> <> [a]",
      "effects": [],
      "constraints": [],
      "doc": "Keep elements where predicate returns true."
    }
  ],
  "types": [],
  "effects": [],
  "interfaces": [],
  "re_exports": []
}
```

Every export includes its full type signature with effect row. The `kind` field is one of: `function`, `type`, `effect`, `interface`, `constructor`.

### 4.3 `clank query` — Type-Directed Search

Agents need to find functions by what they do, not what they're called. `clank query` supports multiple search modes:

```bash
clank query --type "([a], (a) -> b) -> [b]"      # type signature search
clank query --name "map"                          # name search (substring)
clank query --effect "io"                         # functions with io effect
clank query --accepts "List<Int>"                 # functions accepting this type
clank query --returns "Option<a>"                 # functions returning this type
clank query --module "std.*"                      # restrict to modules
clank query --constraint "Ord a"                  # functions requiring constraint
```

**Type search uses unification**, not string matching. `([a], (a) -> b) -> [b]` matches `map` even though `map`'s actual signature includes an effect variable. Type variables in the query are universally quantified.

Output:

```json
{
  "results": [
    {
      "module": "std.col",
      "name": "map",
      "type": "([a], (a) -> <e> b) -> <e> [b]",
      "score": 1.0
    },
    {
      "module": "std.col",
      "name": "flatmap",
      "type": "([a], (a) -> <e> [b]) -> <e> [b]",
      "score": 0.72
    }
  ]
}
```

Results are ranked by unification closeness. Exact match = 1.0. Partial match (extra args, slightly different structure) gets lower scores.

### 4.4 Documentation Source

Docs are extracted from source annotations. Clank uses `##` comments (double-hash) as doc comments, placed directly above the definition:

```
## Apply f to each element of xs.
pub map : ([a], (a) -> <e> b) -> <e> [b] = ...
```

Single `#` is a regular comment (ignored). Double `##` is attached to the next definition. The doc system extracts these at parse time — they are part of the AST.

---

## 5. Incremental Verification Workflow

### 5.1 `clank check` — Incremental Type Checking

```bash
clank check src/math/stats.clk         # check one file
clank check src/                        # check all files under src/
clank check --function mean src/math/stats.clk  # check single function
clank check --watch src/                # re-check on file change
```

### 5.2 Incremental Caching

The checker maintains a cache at `.clank/cache/check/`. Cache keys are content-hashes of:
- The source file
- All directly imported modules' public signatures
- The compiler version

When a module's **public signature** (exported types + effect rows) hasn't changed, downstream modules are not re-checked even if the implementation changed. This is the **signature stability** property — it makes incremental checking O(changed files + dependents with changed signatures) instead of O(all transitively dependent files).

Cache layout:

```
.clank/
  cache/
    check/
      <content-hash>.json     # per-module check result
    build/
      <content-hash>.clkb     # compiled bytecode
    docs/
      <content-hash>.json     # extracted documentation
```

### 5.3 Per-Function Verification

For maximum incrementality, `--function` checks a single definition against its declared signature and the signatures of its imports. This is the tightest feedback loop: an agent editing one function can verify it in isolation without checking the rest of the module.

```bash
clank check --function safe-div src/app/main.clk
```

Output:

```json
{
  "ok": true,
  "function": "safe-div",
  "declared_type": "(Int, Int) -> <DivError> Int",
  "inferred_effects": ["DivError"],
  "diagnostics": [],
  "timing": { "total_ms": 5 }
}
```

### 5.4 Refinement Verification

Refinement type checks invoke the SMT solver. These are cached aggressively:

```json
{
  "ok": false,
  "diagnostics": [
    {
      "severity": "error",
      "code": "E701",
      "phase": "check",
      "message": "refinement unsatisfiable: cannot prove v > 0 for argument d",
      "location": { "file": "src/math.clk", "line": 5, "col": 30 },
      "context": "in call to div",
      "related": [
        { "message": "refinement declared here", "location": { "file": "std/arith.clk", "line": 3, "col": 20 } }
      ],
      "smt_query": "(assert (not (> v 0)))",
      "smt_result": "sat",
      "smt_model": { "v": 0 }
    }
  ]
}
```

The `smt_query`, `smt_result`, and `smt_model` fields are refinement-specific extensions. They let an agent understand exactly what the solver tried and why it failed — no guessing.

---

## 6. Package Manager

### 6.1 `clank pkg` Subcommands

```bash
clank pkg init                    # create clank.pkg in current dir
clank pkg add <name> [version]    # add dependency
clank pkg remove <name>           # remove dependency
clank pkg update [name]           # update to latest compatible version
clank pkg list                    # list all dependencies (direct + transitive)
clank pkg search <query>          # search registry
clank pkg info <name>             # package metadata
clank pkg publish                 # publish to registry
clank pkg audit                   # check for known issues
```

### 6.2 Package Manifest (`clank.pkg`)

The manifest uses a simple key-value format (not JSON, not TOML — minimal syntax):

```
name = "my-app"
version = "0.1.0"
entry = "main"
license = "MIT"
repository = "https://github.com/user/my-app"

[deps]
std = "0.1"
net-http = "1.2"
json-schema = "0.3"

[dev-deps]
test-helpers = "0.1"
```

### 6.3 Lock File (`clank.lock`)

The lock file is JSON — designed for machine consumption:

```json
{
  "version": 1,
  "packages": {
    "net-http@1.2.3": {
      "resolved": "https://registry.clank.dev/net-http/1.2.3.tar.gz",
      "integrity": "sha256-abc123...",
      "deps": { "std": "0.1.0" }
    }
  }
}
```

### 6.4 Registry Protocol

The package registry exposes a JSON API. All responses follow the standard envelope.

```bash
# Search
clank pkg search --type "([a]) -> Option<a>"     # search by type signature
clank pkg search --name "http"                    # search by name
clank pkg search --effect "async"                 # packages using async effects
```

Search output:

```json
{
  "results": [
    {
      "name": "net-http",
      "version": "1.2.3",
      "description": "HTTP client and server",
      "exports_summary": { "functions": 15, "types": 4, "effects": 2 },
      "effects_used": ["io", "async", "exn"],
      "downloads": 1200,
      "score": 0.95
    }
  ]
}
```

Agents can evaluate packages by their type-level API summary and effect usage without downloading or reading source code. The `exports_summary` and `effects_used` fields are computed at publish time and stored in the registry index.

### 6.5 `clank pkg info` — Deep Package Inspection

```bash
clank pkg info net-http
```

Returns full export list, dependency tree, effect profile, and compatibility info:

```json
{
  "name": "net-http",
  "version": "1.2.3",
  "exports": [
    { "module": "net-http.client", "name": "fetch", "type": "(Str) -> <io, async, exn[HttpErr]> Response" },
    { "module": "net-http.client", "name": "get", "type": "(Str) -> <io, async, exn[HttpErr]> Str" }
  ],
  "types": [
    { "module": "net-http.types", "name": "Response", "kind": "record", "fields": ["status: Int", "body: Str", "headers: Map<Str, Str>"] }
  ],
  "effects": [
    { "module": "net-http", "name": "HttpErr", "ops": ["timeout: () -> ()", "status: (Int) -> ()"] }
  ],
  "deps": { "std": "0.1.0" },
  "clank_version": ">= 0.2.0"
}
```

This lets an agent fully understand a package's API without downloading it or reading its source.

### 6.6 Dependency Resolution

Resolution uses **minimal version selection** (MVS, as in Go modules). Given a version constraint like `"1.2"`, the resolver picks the lowest version satisfying the constraint. This makes builds reproducible without a lock file (the lock file provides integrity verification, not version pinning).

Conflict resolution: if two packages require incompatible versions of the same dependency, the resolver emits a structured error:

```json
{
  "severity": "error",
  "code": "E501",
  "phase": "link",
  "message": "version conflict: my-app requires json >= 0.3, net-http requires json < 0.3",
  "packages": ["my-app", "net-http"],
  "dependency": "json",
  "constraints": [">= 0.3", "< 0.3"]
}
```

---

## 7. REPL / Eval Surface

### 7.1 `clank eval` — Single Expression Evaluation

```bash
clank eval "2 + 3"
clank eval "map([1,2,3], fn(x) => x * 2)"
clank eval --type "map"                          # print type of symbol
clank eval --file src/app/main.clk "mean([1,2,3])"  # eval with file's definitions in scope
```

Output:

```json
{
  "ok": true,
  "data": {
    "value": 5,
    "type": "Int",
    "effects": []
  },
  "diagnostics": [],
  "timing": { "total_ms": 3 }
}
```

### 7.2 Session Mode

For multi-step exploration, `clank eval --session` maintains state across invocations via a session file:

```bash
clank eval --session my-session "let x = 42"
clank eval --session my-session "x + 1"
# => { "ok": true, "data": { "value": 43, "type": "Int", "effects": [] } }

clank eval --session my-session --bindings
# => { "bindings": [{ "name": "x", "type": "Int", "value": 42 }] }

clank eval --session my-session --reset
```

Session state is stored at `.clank/sessions/<name>.json`. This lets an agent build up definitions incrementally, test hypotheses, and explore behavior — without managing a persistent REPL process.

### 7.3 `clank eval --type`

Print the type of an expression or symbol without evaluating:

```bash
clank eval --type "fn(x: Int) => x + 1"
```

```json
{
  "ok": true,
  "data": {
    "type": "(Int) -> <> Int",
    "effects": [],
    "constraints": []
  }
}
```

---

## 8. Test Runner

### 8.1 `clank test`

```bash
clank test                        # run all tests under test/
clank test test/math/*            # glob filter
clank test --filter "mean"        # name filter
clank test --module std.list      # test a specific module's tests
```

Test files use `std.test`:

```
mod test.math-stats

use std.test (test, assert-eq, assert)
use math.stats (mean, variance)

test "mean of single element" =
  assert-eq(mean([3]), 3)

test "mean of multiple" =
  assert-eq(mean([1, 2, 3]), 2)

test "variance of uniform" =
  assert-eq(variance([5, 5, 5]), 0)
```

### 8.2 Test Output

```json
{
  "ok": false,
  "summary": { "total": 12, "passed": 11, "failed": 1, "skipped": 0 },
  "tests": [
    {
      "name": "mean of single element",
      "module": "test.math-stats",
      "status": "pass",
      "duration_ms": 2
    },
    {
      "name": "variance of uniform",
      "module": "test.math-stats",
      "status": "fail",
      "duration_ms": 3,
      "failure": {
        "message": "assert-eq failed: expected 0, got 1",
        "location": { "file": "test/math-stats.clk", "line": 10, "col": 3 },
        "expected": "0",
        "actual": "1"
      }
    }
  ],
  "timing": { "total_ms": 45 }
}
```

### 8.3 Test Discovery

Tests are discovered by:
1. Files under `test/` matching `*-test.clk` or `test-*.clk`
2. Any file containing `use std.test` with `test "..." = ...` definitions
3. Module-level test blocks (inline tests in source files, gated behind `--include-inline`)

---

## 9. Formatter

### 9.1 `clank fmt`

```bash
clank fmt src/app/main.clk       # format file in place
clank fmt --check src/            # check formatting, exit 1 if unformatted
clank fmt --diff src/app/main.clk # show diff without writing
clank fmt --stdin                 # read from stdin, write to stdout
```

The formatter enforces a single canonical style. There are **no configuration options** — this is deliberate. One style means agents never waste tokens on style decisions or formatting flags.

Formatting rules:
- 2-space indentation
- One blank line between top-level definitions
- Imports sorted alphabetically by module path
- Parameters one-per-line if signature exceeds 80 characters
- Match arms aligned

### 9.2 Format Output (--check mode)

```json
{
  "ok": false,
  "files": [
    {
      "file": "src/app/main.clk",
      "formatted": false,
      "diff": "--- a/src/app/main.clk\n+++ b/src/app/main.clk\n@@ -3,2 +3,2 @@\n-use std.io(print)\n+use std.io (print)\n"
    }
  ]
}
```

---

## 10. Linter

### 10.1 `clank lint`

The linter catches issues beyond what the type checker reports:

```bash
clank lint src/                   # lint all source files
clank lint --rule W201 src/       # check specific rule
clank lint --fix src/             # auto-fix where possible
```

### 10.2 Lint Rules

| Code | Description |
|------|-------------|
| W201 | Unused import |
| W202 | Unused local definition |
| W203 | Shadowing a built-in name |
| W204 | Orphan impl (see module system §6) |
| W301 | Missing type annotation on public function |
| W302 | Effect annotation on private function (unnecessary) |
| W401 | Unreachable match arm |
| W402 | Non-exhaustive match (promoted to error with `--strict`) |
| W501 | `discard` on affine value (potential resource leak) |
| W502 | Clone of non-affine value (unnecessary) |
| W601 | Refinement predicate always true (tautology) |
| W602 | Refinement predicate always false (dead code) |

Lint output uses the same diagnostic schema (§3.1). The `fix` field provides auto-fix edits where applicable.

---

## 11. Build and Compilation

### 11.1 `clank build`

```bash
clank build .                     # compile current package
clank build --target vm           # bytecode for Clank VM (default)
clank build --target wasm         # WebAssembly output
clank build --emit ast            # emit desugared AST as JSON
clank build --emit ir             # emit intermediate representation
clank build --emit bytecode       # emit bytecode as JSON (human-inspectable)
```

### 11.2 Build Output

```json
{
  "ok": true,
  "data": {
    "output": "build/my-app.clkb",
    "modules_compiled": 8,
    "bytecode_size": 4096,
    "type_check_cached": 5
  },
  "diagnostics": [],
  "timing": { "total_ms": 120, "phases": { "parse": 15, "check": 60, "compile": 40, "link": 5 } }
}
```

### 11.3 `--emit` Modes

The `--emit` flag lets agents inspect intermediate representations. This is critical for debugging and understanding compiler behavior:

- `ast`: Desugared AST as JSON. Every node includes source location.
- `ir`: Post-type-check intermediate representation with resolved types.
- `bytecode`: Bytecode as JSON array of `{ opcode, args, source_loc }` objects.

---

## 12. Project Initialization

### 12.1 `clank pkg init`

```bash
clank pkg init                    # interactive (for humans)
clank pkg init --name "my-app" --entry "main"  # non-interactive (for agents)
```

Creates:

```
my-app/
  clank.pkg
  src/
    main.clk
```

The generated `main.clk`:

```
mod main

use std.io (print)

main : () -> <io> () =
  print("hello")
```

---

## 13. Design Rationale

### Why JSON as default output?

Agents parse JSON natively. Human-readable output requires regex parsing or heuristic extraction — fragile and lossy. JSON-first means every tool is immediately composable with agent workflows. The `--format human` flag exists for the rare case where a human is reading terminal output directly.

### Why no formatter config?

Configuration is a token sink. Every style option is a decision an agent must make, a flag it must remember, and a divergence it must handle when reading unfamiliar code. One canonical style eliminates all of this. The style itself doesn't matter much — consistency does.

### Why minimal version selection?

MVS is deterministic without a lock file, simpler to reason about, and avoids the NP-hard resolution problem of SAT-based solvers. For agents, predictability is more valuable than "latest compatible" semantics.

### Why per-function checking?

Agents edit code one function at a time. Checking the entire module after each edit wastes cycles. Per-function checking gives tight feedback loops — change a function, verify it, move on. The signature stability property means this doesn't cascade.

### Why session-based eval instead of a persistent REPL?

Agents don't maintain long-running processes well. A session file is stateless from the agent's perspective — it invokes `clank eval --session X "expr"` and gets a JSON response. No process management, no stdin/stdout coordination, no hanging connections.

### Why type-directed search in the package registry?

"Find me a function that takes a list and returns an option" is how agents think about APIs. Name-based search requires knowing the name first. Type-based search lets an agent discover functions by their shape — the same way Hoogle works for Haskell, but as a first-class registry feature.

### Why include SMT details in refinement errors?

When a refinement check fails, the agent needs to understand *why* — not just that it failed. The SMT query shows exactly what was asked, the result shows whether it was satisfiable/unsatisfiable/timeout, and the model (if sat) provides a counterexample. This turns "refinement error" from an opaque failure into actionable debugging information.
