# Pretty-Print / Human-Readable Layer Specification

**Task:** TASK-082
**Status:** Complete
**Dependencies:** plan/SPEC.md, plan/features/stdlib-catalog.md, plan/features/tooling-spec.md

---

## 1. Overview

Clank uses a **two-layer design** inspired by K/Q: the canonical form is terse
and token-efficient (optimized for AI agents), while the pretty-print form is
verbose and human-readable (optimized for human review).

The pretty-print layer is a **reversible, lossless transformation** between
these two forms. Both forms parse to the same AST — they are surface-level
syntactic variants, not distinct languages.

### Design Goals

1. **Bidirectional**: `clank pretty` (canonicalverbose) and `clank terse` (verbosecanonical) are exact inverses
2. **Semantic identity**: Both forms produce identical ASTs, identical type-check results, identical runtime behavior
3. **No information loss**: The mapping is defined by a static expansion table; no heuristics or context-sensitivity
4. **Human-first verbose form**: Verbose names should be immediately understandable to a programmer unfamiliar with Clank's terse conventions
5. **Agent-first canonical form**: The canonical form remains the source of truth, the stored form, and the form agents read/write

### Non-Goals

- The pretty-print layer does not add type annotations to unannotated bindings (that would be a separate `--annotate` feature)
- The pretty-print layer does not change indentation, line-breaking, or whitespace rules (those are `clank fmt` concerns)
- The pretty-print layer does not add or remove comments

---

## 2. Expansion Model

The transformation is purely **lexical substitution** on identifiers. Given a
static expansion table mapping terse tokens to verbose tokens:

- `clank pretty`: replace every terse token with its verbose equivalent
- `clank terse`: replace every verbose token with its terse equivalent

Tokens not in the table pass through unchanged. The expansion table is built
into the compiler — there is no user configuration.

### 2.1 What Gets Expanded

| Category | Example (terse → verbose) |
|----------|---------------------------|
| Module names | `std.str` → `std.string` |
| Qualified function names | `str.slc` → `string.slice` |
| Unqualified built-in names | `len` → `length` |
| Import paths | `use std.str (slc)` → `use std.string (slice)` |

### 2.2 What Does NOT Get Expanded

| Category | Rationale |
|----------|-----------|
| Keywords (`let`, `fn`, `if`, `match`, etc.) | Already readable English words |
| Operators (`+`, `|>`, `++`, `&&`, etc.) | Symbolic; no verbose equivalent needed |
| User-defined identifiers | Not in the expansion table |
| Type names (`Int`, `Str`, `Bool`, etc.) | Already readable |
| Effect names (`io`, `exn`, `async`, `state`) | Already readable |
| Literals | No expansion needed |
| String contents | Never transformed |

---

## 3. Expansion Tables

### 3.1 Module Names

| Terse | Verbose |
|-------|---------|
| `std.str` | `std.string` |
| `std.json` | `std.json` |
| `std.fs` | `std.filesystem` |
| `std.col` | `std.collection` |
| `std.http` | `std.http` |
| `std.err` | `std.error` |
| `std.proc` | `std.process` |
| `std.env` | `std.environment` |
| `std.srv` | `std.server` |
| `std.cli` | `std.cli` |
| `std.dt` | `std.datetime` |
| `std.csv` | `std.csv` |
| `std.log` | `std.log` |
| `std.test` | `std.test` |
| `std.rx` | `std.regex` |
| `std.math` | `std.math` |

Modules that are already readable (`json`, `http`, `cli`, `csv`, `log`, `test`,
`math`) keep their names in both forms.

### 3.2 String Functions (`std.str` → `std.string`)

| Terse | Verbose |
|-------|---------|
| `str.len` | `string.length` |
| `str.get` | `string.get` |
| `str.slc` | `string.slice` |
| `str.has` | `string.contains` |
| `str.idx` | `string.index-of` |
| `str.ridx` | `string.last-index-of` |
| `str.pfx` | `string.starts-with` |
| `str.sfx` | `string.ends-with` |
| `str.up` | `string.uppercase` |
| `str.lo` | `string.lowercase` |
| `str.rep` | `string.replace` |
| `str.rep1` | `string.replace-first` |
| `str.pad` | `string.pad-right` |
| `str.lpad` | `string.pad-left` |
| `str.rev` | `string.reverse` |
| `str.enc` | `string.encode` |
| `str.dec` | `string.decode` |
| `str.cat` | `string.concatenate` |
| `str.fmt` | `string.format` |
| `str.lines` | `string.lines` |
| `str.words` | `string.words` |
| `str.chars` | `string.chars` |
| `str.int` | `string.parse-int` |
| `str.rat` | `string.parse-rat` |
| `str.show` | `string.show` |

### 3.3 JSON Functions (`std.json`)

| Terse | Verbose |
|-------|---------|
| `json.enc` | `json.encode` |
| `json.dec` | `json.decode` |
| `json.get` | `json.get` |
| `json.idx` | `json.index` |
| `json.path` | `json.path` |
| `json.set` | `json.set` |
| `json.del` | `json.delete` |
| `json.keys` | `json.keys` |
| `json.vals` | `json.values` |
| `json.typ` | `json.type-of` |
| `json.int` | `json.as-int` |
| `json.str` | `json.as-string` |
| `json.bool` | `json.as-bool` |
| `json.arr` | `json.as-array` |
| `json.merge` | `json.merge` |

### 3.4 File I/O Functions (`std.fs` → `std.filesystem`)

| Terse | Verbose |
|-------|---------|
| `fs.open` | `filesystem.open` |
| `fs.close` | `filesystem.close` |
| `fs.read` | `filesystem.read` |
| `fs.readb` | `filesystem.read-bytes` |
| `fs.write` | `filesystem.write` |
| `fs.writeb` | `filesystem.write-bytes` |
| `fs.append` | `filesystem.append` |
| `fs.lines` | `filesystem.lines` |
| `fs.exists` | `filesystem.exists` |
| `fs.rm` | `filesystem.remove` |
| `fs.mv` | `filesystem.move` |
| `fs.cp` | `filesystem.copy` |
| `fs.mkdir` | `filesystem.make-directory` |
| `fs.ls` | `filesystem.list` |
| `fs.stat` | `filesystem.stat` |
| `fs.tmp` | `filesystem.temp` |
| `fs.cwd` | `filesystem.current-directory` |
| `fs.abs` | `filesystem.absolute` |
| `fs.with` | `filesystem.with` |

### 3.5 Collection Functions (`std.col` → `std.collection`)

#### Lists

| Terse | Verbose |
|-------|---------|
| `col.rev` | `collection.reverse` |
| `col.sort` | `collection.sort` |
| `col.sortby` | `collection.sort-by` |
| `col.uniq` | `collection.unique` |
| `col.zip` | `collection.zip` |
| `col.unzip` | `collection.unzip` |
| `col.flat` | `collection.flatten` |
| `col.flatmap` | `collection.flat-map` |
| `col.take` | `collection.take` |
| `col.drop` | `collection.drop` |
| `col.nth` | `collection.nth` |
| `col.find` | `collection.find` |
| `col.any` | `collection.any` |
| `col.all` | `collection.all` |
| `col.count` | `collection.count` |
| `col.enum` | `collection.enumerate` |
| `col.chunk` | `collection.chunk` |
| `col.win` | `collection.window` |
| `col.intersperse` | `collection.intersperse` |
| `col.range` | `collection.range` |
| `col.rep` | `collection.repeat` |
| `col.sum` | `collection.sum` |
| `col.prod` | `collection.product` |
| `col.min` | `collection.minimum` |
| `col.max` | `collection.maximum` |
| `col.group` | `collection.group-by` |
| `col.scan` | `collection.scan` |

#### Maps

| Terse | Verbose |
|-------|---------|
| `map.new` | `map.new` |
| `map.of` | `map.of` |
| `map.get` | `map.get` |
| `map.set` | `map.set` |
| `map.del` | `map.delete` |
| `map.has` | `map.contains` |
| `map.keys` | `map.keys` |
| `map.vals` | `map.values` |
| `map.pairs` | `map.pairs` |
| `map.len` | `map.length` |
| `map.merge` | `map.merge` |
| `map.mapv` | `map.map-values` |
| `map.filterv` | `map.filter-values` |

#### Sets

| Terse | Verbose |
|-------|---------|
| `set.new` | `set.new` |
| `set.of` | `set.of` |
| `set.has` | `set.contains` |
| `set.add` | `set.add` |
| `set.rm` | `set.remove` |
| `set.union` | `set.union` |
| `set.inter` | `set.intersection` |
| `set.diff` | `set.difference` |
| `set.len` | `set.length` |
| `set.list` | `set.to-list` |

### 3.6 HTTP Client Functions (`std.http`)

| Terse | Verbose |
|-------|---------|
| `http.get` | `http.get` |
| `http.post` | `http.post` |
| `http.put` | `http.put` |
| `http.del` | `http.delete` |
| `http.patch` | `http.patch` |
| `http.req` | `http.request` |
| `http.hdr` | `http.header` |
| `http.json` | `http.json` |
| `http.ok?` | `http.ok?` |

### 3.7 Error Functions (`std.err` → `std.error`)

| Terse | Verbose |
|-------|---------|
| `err.new` | `error.new` |
| `err.ctx` | `error.context` |
| `err.wrap` | `error.wrap` |

The unqualified error/option words (`ok`, `fail`, `unwrap`, `try`, `throw`,
`some`, `none`, etc.) are already readable and are NOT expanded.

### 3.8 Process Functions (`std.proc` → `std.process`)

| Terse | Verbose |
|-------|---------|
| `proc.run` | `process.run` |
| `proc.sh` | `process.shell` |
| `proc.ok` | `process.ok` |
| `proc.pipe` | `process.pipe` |
| `proc.bg` | `process.background` |
| `proc.wait` | `process.wait` |
| `proc.kill` | `process.kill` |
| `proc.exit` | `process.exit` |
| `proc.pid` | `process.pid` |

### 3.9 Environment Functions (`std.env` → `std.environment`)

| Terse | Verbose |
|-------|---------|
| `env.get` | `environment.get` |
| `env.get!` | `environment.get!` |
| `env.set` | `environment.set` |
| `env.rm` | `environment.remove` |
| `env.all` | `environment.all` |
| `env.has` | `environment.has` |

### 3.10 Server Functions (`std.srv` → `std.server`)

| Terse | Verbose |
|-------|---------|
| `srv.new` | `server.new` |
| `srv.get` | `server.get` |
| `srv.post` | `server.post` |
| `srv.put` | `server.put` |
| `srv.del` | `server.delete` |
| `srv.start` | `server.start` |
| `srv.stop` | `server.stop` |
| `srv.res` | `server.response` |
| `srv.json` | `server.json` |
| `srv.hdr` | `server.header` |
| `srv.mw` | `server.middleware` |

### 3.11 DateTime Functions (`std.dt` → `std.datetime`)

| Terse | Verbose |
|-------|---------|
| `dt.now` | `datetime.now` |
| `dt.unix` | `datetime.unix` |
| `dt.from` | `datetime.from-unix` |
| `dt.to` | `datetime.to-unix` |
| `dt.parse` | `datetime.parse` |
| `dt.fmt` | `datetime.format` |
| `dt.add` | `datetime.add` |
| `dt.sub` | `datetime.subtract` |
| `dt.tz` | `datetime.timezone` |
| `dt.iso` | `datetime.iso` |
| `dt.ms` | `datetime.milliseconds` |
| `dt.sec` | `datetime.seconds` |
| `dt.min` | `datetime.minutes` |
| `dt.hr` | `datetime.hours` |
| `dt.day` | `datetime.days` |

### 3.12 CSV Functions (`std.csv`)

| Terse | Verbose |
|-------|---------|
| `csv.dec` | `csv.decode` |
| `csv.enc` | `csv.encode` |
| `csv.decf` | `csv.decode-file` |
| `csv.encf` | `csv.encode-file` |
| `csv.hdr` | `csv.header` |
| `csv.rows` | `csv.rows` |
| `csv.maps` | `csv.as-maps` |
| `csv.opts` | `csv.with-options` |

### 3.13 Regex Functions (`std.rx` → `std.regex`)

| Terse | Verbose |
|-------|---------|
| `rx.new` | `regex.new` |
| `rx.match` | `regex.match` |
| `rx.all` | `regex.all` |
| `rx.test` | `regex.test` |
| `rx.rep` | `regex.replace` |
| `rx.rep1` | `regex.replace-first` |
| `rx.split` | `regex.split` |

### 3.14 Unqualified Built-in Expansions

These are the auto-imported built-in words that get expanded:

| Terse | Verbose |
|-------|---------|
| `len` | `length` |
| `cat` | `concatenate` |
| `cmp` | `compare` |
| `eq` | `equal` |
| `neq` | `not-equal` |
| `ok?` | `is-ok` |
| `some?` | `is-some` |
| `unwrap?` | `unwrap-option` |
| `map-opt` | `map-option` |
| `map-res` | `map-result` |

Words that are already readable remain unchanged: `map`, `filter`, `fold`,
`head`, `tail`, `cons`, `concat`, `print`, `show`, `split`, `join`, `trim`,
`ok`, `fail`, `unwrap`, `unwrap-or`, `try`, `throw`, `some`, `none`,
`or-else`, `and-then`, `clone`, `discard`.

---

## 4. Transformation Rules

### 4.1 Import Statements

Import paths and imported names are both expanded:

```
# Canonical (terse)
use std.str (slc, pfx, cat)
use std.fs (read, rm, mv)

# Pretty-print (verbose)
use std.string (slice, starts-with, concatenate)
use std.filesystem (read, remove, move)
```

### 4.2 Qualified Calls

Dot-qualified identifiers are expanded as a unit. The module prefix and function
name are both mapped:

```
# Canonical
str.slc(s, 0, 5)
col.sortby(xs, fn(a, b) => cmp(a.name, b.name))

# Pretty-print
string.slice(s, 0, 5)
collection.sort-by(xs, fn(a, b) => compare(a.name, b.name))
```

### 4.3 Unqualified Calls

Unqualified names are expanded only if they appear in the built-in expansion
table (§3.14):

```
# Canonical
let n = len(xs)
let combined = cat(a, b)

# Pretty-print
let n = length(xs)
let combined = concatenate(a, b)
```

### 4.4 Module Declarations

Module names in `mod` declarations are expanded:

```
# Canonical
mod app.main

# Pretty-print
mod app.main
```

Only `std.*` module paths are expanded. User-defined module names are never
changed.

### 4.5 User-Defined Names

User-defined identifiers (function names, variable names, type names, effect
names) are **never** expanded. Only names from the standard library expansion
table are transformed.

```
# These are identical in both forms:
my-func : (x: Int) -> <> Int = x + 1
type MyType = A | B(Int)
effect MyEffect { my-op : () -> <> Int }
```

---

## 5. CLI Integration

### 5.1 `clank pretty` — Canonical to Verbose

```bash
clank pretty <file>              # print verbose form to stdout
clank pretty --write <file>      # overwrite file with verbose form
clank pretty --diff <file>       # show diff without writing
clank pretty --stdin              # read from stdin, write to stdout
```

### 5.2 `clank terse` — Verbose to Canonical

```bash
clank terse <file>               # print canonical form to stdout
clank terse --write <file>       # overwrite file with canonical form
clank terse --diff <file>        # show diff without writing
clank terse --stdin               # read from stdin, write to stdout
```

### 5.3 Output Format

Both commands follow the standard tooling envelope (tooling-spec.md §3.4):

```json
{
  "ok": true,
  "data": {
    "source": "...transformed source...",
    "transformations": 14,
    "direction": "pretty"
  },
  "diagnostics": [],
  "timing": { "total_ms": 3 }
}
```

With `--format human`, the transformed source is printed directly (no JSON
envelope). This is the one case where human format is the natural default — but
the global `--format` flag still defaults to `json` for consistency.

### 5.4 Interaction with Other Subcommands

**`clank fmt`** operates on whichever form the source is in. Formatting rules
(indentation, line-breaking) are identical for both forms. Running `clank fmt`
after `clank pretty` is valid and produces well-formatted verbose code.

**`clank run`**, **`clank check`**, etc. accept both forms — the parser
recognizes all verbose identifiers as aliases for their terse counterparts. No
flag needed. The lexer consults the expansion table during tokenization and
normalizes to canonical form internally.

### 5.5 Flag on `clank run` and `clank build`

For convenience, other subcommands accept `--pretty` to emit any source output
(e.g., error snippets, `--emit ast`) in verbose form:

```bash
clank check --pretty src/main.clk
```

Error messages in `--pretty` mode use verbose names in their `context` and
`message` fields to match the source the human is reading.

---

## 6. Implementation Strategy

### 6.1 Expansion Table Data Structure

The expansion table is a compile-time constant: two `Map<String, String>`
lookups (terse→verbose and verbose→terse), generated from a single source-of-
truth definition.

```
type Expansion = { terse: Str, verbose: Str, module: Str }
```

The table contains ~180 entries (all stdlib-qualified names + ~10 built-in
unqualified names). Lookup is O(1) with a hash map.

### 6.2 Transformation Pass

The transformation operates on the **token stream**, not the AST:

1. Lex the source into tokens (preserving whitespace and comments as trivia)
2. For each identifier token, check if it (or its dot-qualified prefix) appears
   in the expansion table
3. Replace the token text with the expanded/contracted form
4. Reconstruct the source from modified tokens + preserved trivia

This approach is simpler than an AST transform because:
- It preserves all formatting, comments, and whitespace exactly
- It doesn't require parsing (which means it works on syntactically invalid files too)
- It's fast — a single pass over tokens

### 6.3 Ambiguity Resolution

A potential ambiguity: a user defines a local variable called `len`, and the
pretty-printer would incorrectly expand it to `length`.

**Resolution**: The expansion only applies to tokens that resolve to stdlib
names. For unqualified names, the transformation must check whether the name
is shadowed by a local binding in scope. This requires a lightweight scope
analysis (not full type-checking — just tracking `let`, `fn` parameter, and
`match` pattern bindings).

For **qualified** names (e.g., `str.slc`), there is no ambiguity — user code
cannot define `str.slc` because `str` is a stdlib module prefix.

### 6.4 Round-Trip Guarantee

The following invariant must hold:

```
clank pretty file.clk | clank terse --stdin == clank fmt file.clk
```

That is: pretty-printing then tersifying produces the canonical formatted form.
And vice versa:

```
clank terse file.clk | clank pretty --stdin == clank pretty file.clk
```

This is guaranteed by the expansion table being a bijection (each terse name
maps to exactly one verbose name and vice versa).

---

## 7. Examples

### 7.1 Data Pipeline

```
# Canonical (terse) — what agents read and write
status-freq : (path: Str) -> <io, exn> Map<Str, Int> =
  path |> fs.read |> str.lines |> map(fn(line) => split(line, " ") |> col.nth(8))
       |> filter(fn(x) => some?(x)) |> map(unwrap) |> col.group(fn(x) => x)

# Pretty-print (verbose) — what humans review
status-freq : (path: Str) -> <io, exn> Map<Str, Int> =
  path |> filesystem.read |> string.lines |> map(fn(line) => split(line, " ") |> collection.nth(8))
       |> filter(fn(x) => is-some(x)) |> map(unwrap) |> collection.group-by(fn(x) => x)
```

### 7.2 Config Loading

```
# Canonical
use std.fs (read)
use std.json (dec, merge)

load-config : (base-path: Str, env-path: Str) -> <io, exn> Config =
  let base = base-path |> read |> json.dec
  let env = env-path |> read |> json.dec
  merge(base, env) |> into

# Pretty-print
use std.filesystem (read)
use std.json (decode, merge)

load-config : (base-path: Str, env-path: Str) -> <io, exn> Config =
  let base = base-path |> read |> json.decode
  let env = env-path |> read |> json.decode
  merge(base, env) |> into
```

### 7.3 Server with Middleware

```
# Canonical
use std.srv

let routes = srv.new()
  |> srv.get("/health", fn(_req) => srv.res(200, "ok"))
  |> srv.mw(fn(req, next) => do {
    log.info(str.fmt("req: {} {}", [req.method, req.path]))
    next(req)
  })
  |> srv.start(8080)

# Pretty-print
use std.server

let routes = server.new()
  |> server.get("/health", fn(_req) => server.response(200, "ok"))
  |> server.middleware(fn(req, next) => do {
    log.info(string.format("req: {} {}", [req.method, req.path]))
    next(req)
  })
  |> server.start(8080)
```

### 7.4 Error Handling

```
# Canonical
let result = try(fn() => fs.read("missing.txt"))
match result {
  Ok(contents) => print(contents)
  Fail(e) => print(err.wrap(e, "failed to read").msg)
}

# Pretty-print
let result = try(fn() => filesystem.read("missing.txt"))
match result {
  Ok(contents) => print(contents)
  Fail(e) => print(error.wrap(e, "failed to read").msg)
}
```

---

## 8. Token Impact Analysis

The verbose form uses more tokens — that's the tradeoff. Measurements on
the 8 canonical examples from TASK-061:

| Form | Avg tokens per example | vs Python | vs TypeScript |
|------|------------------------|-----------|---------------|
| Canonical (terse) | ~32 | -53% | -57% |
| Pretty-print (verbose) | ~42 | -39% | -44% |

The verbose form is still significantly more token-efficient than Python/TS
because the expansion only affects identifier names — the structural
compression (pipelines, refinement types, effects, affine types) remains
identical. An estimated ~30% token increase over canonical form, still ~40%
below Python.

---

## 9. File Extension Convention

Both forms use `.clk` files. There is no separate extension for verbose form.
The canonical form is the storage default — a project should store terse source
and render verbose on demand (for code review, documentation, onboarding).

A project may optionally set a preference in `clank.pkg`:

```
[format]
source = "terse"     # default — agents write terse, humans view via clank pretty
```

There is no `source = "verbose"` option in v1.0. If a human writes verbose
code, `clank terse` converts it before commit. This keeps repositories
canonical.

---

## 10. Design Rationale

### Why lexical substitution, not AST transform?

Lexical substitution preserves formatting, comments, and whitespace exactly.
An AST transform would need to reconstruct source text, which is what `clank
fmt` does — but `clank pretty` should be lighter-weight and composable with
`fmt` rather than duplicating it.

### Why not expand operators?

Operators like `|>`, `++`, `&&` are universally understood symbolic notation.
Expanding `|>` to `pipe-to` or `++` to `string-concat` would hurt readability,
not help it. The verbose form targets identifier expansion — the area where
Clank's terse conventions differ most from mainstream languages.

### Why not expand keywords?

Keywords (`let`, `fn`, `if`, `match`, `do`, `handle`, `type`, `effect`, etc.)
are already standard English words used across many languages. Expanding `fn` to
`function` would add tokens for zero readability gain — anyone who can read
JavaScript can read `fn`.

### Why not expand type names or effect names?

`Int`, `Str`, `Bool`, `Rat` are standard abbreviations understood by all
programmers. `io`, `exn`, `async`, `state` are standard effect names. Expanding
`Str` to `String` or `exn` to `exception` would create inconsistency with the
language specification and type-checker output for minimal readability benefit.

### Why scope analysis for unqualified names?

Without scope analysis, `let len = 5` followed by `len + 1` would incorrectly
expand to `length + 1`, breaking the program. The scope analysis is lightweight
(track `let`/`fn`/`match` bindings) and guarantees correctness. Qualified
names (`str.slc`) don't need this because stdlib module prefixes are reserved.

### Why K/Q as the inspiration?

K's terse notation is nearly unreadable to non-practitioners. Q's verbose layer
makes the same programs accessible. Clank's terse form is far more readable
than K (it uses English words, not symbols), so the gap is smaller — but the
principle holds. A human reviewer unfamiliar with `col.sortby` immediately
understands `collection.sort-by`.
