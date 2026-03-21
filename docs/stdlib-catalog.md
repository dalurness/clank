# Clank Standard Library — Word Catalog (Tier 1 & Tier 2)

**Task:** TASK-008, updated by TASK-016
**Status:** Draft
**Depends on:** TASK-004 (core syntax), TASK-005 (effect system), TASK-007 (affine types)

---

## Conventions

### Signature Notation

Applicative function signatures: `(params) -> <effects> ReturnType`

- Parameters are positional types in parentheses: `(Str, Int) -> <> Bool`
- Effects in angle brackets; `<>` means pure
- `<io>` = I/O, `<exn[E]>` = may throw E, `<async>` = async, `<state[S]>` = mutable state
- `!T` = affine (move semantics, consumed on use)
- `&T` = borrowed (read-only reference)
- `{T | pred}` = refinement type with predicate
- Generic types use angle brackets: `Option<a>`, `Result<a, e>`, `Map<k, v>`
- Function-typed parameters: `(a) -> <e> b`

### Prelude Types

```
type Ordering = LT | EQ | GT

type Result<a, e> = Ok(a) | Fail(e)
type Option<a> = Some(a) | None
```

### Naming Rules

- Terse: 2-6 chars preferred, max 10
- Dot-prefixed for module-qualified: `str.pad`, `json.enc`
- No overloading: each word has exactly one meaning
- `?` suffix = returns `Option`, `!` suffix = may throw

### Module Auto-Import

All Tier 1 words are auto-imported (no `use` needed). Tier 2 requires explicit `use`.

---

## Tier 1 — Auto-Imported

### 1. Strings (`std.str`)

Core string words beyond the built-in `split`, `join`, `trim`.

The `++` operator is infix sugar for `str.cat`: `"hello" ++ " world"` desugars to `str.cat("hello", " world")`.

| Word | Signature | Description |
|------|-----------|-------------|
| `str.len` | `(Str) -> <> Int` | Length in UTF-8 codepoints |
| `str.get` | `(Str, Int) -> <> Option<Str>` | Codepoint at index (single-char string) |
| `str.slc` | `(Str, Int, Int) -> <> Str` | Substring [start, end) |
| `str.has` | `(Str, Str) -> <> Bool` | Contains substring |
| `str.idx` | `(Str, Str) -> <> Option<Int>` | Index of first occurrence |
| `str.ridx` | `(Str, Str) -> <> Option<Int>` | Index of last occurrence |
| `str.pfx` | `(Str, Str) -> <> Bool` | Starts with prefix |
| `str.sfx` | `(Str, Str) -> <> Bool` | Ends with suffix |
| `str.up` | `(Str) -> <> Str` | Uppercase |
| `str.lo` | `(Str) -> <> Str` | Lowercase |
| `str.rep` | `(Str, Str, Str) -> <> Str` | Replace all: haystack old new |
| `str.rep1` | `(Str, Str, Str) -> <> Str` | Replace first occurrence |
| `str.pad` | `(Str, Int, Str) -> <> Str` | Pad right to width with fill char |
| `str.lpad` | `(Str, Int, Str) -> <> Str` | Pad left to width with fill char |
| `str.rev` | `(Str) -> <> Str` | Reverse (codepoint-level) |
| `str.enc` | `(Str) -> <> [Byte]` | UTF-8 encode to bytes |
| `str.dec` | `([Byte]) -> <exn[Err]> Str` | UTF-8 decode from bytes |
| `str.cat` | `(Str, Str) -> <> Str` | Concatenate two strings (also `++` operator) |
| `str.fmt` | `(Str, [Str]) -> <> Str` | Template interpolation: `str.fmt("hello {}", ["world"])` |
| `str.lines` | `(Str) -> <> [Str]` | Split on newlines |
| `str.words` | `(Str) -> <> [Str]` | Split on whitespace |
| `str.chars` | `(Str) -> <> [Str]` | Explode to list of single-char strings |
| `str.int` | `(Str) -> <> Option<Int>` | Parse as integer |
| `str.rat` | `(Str) -> <> Option<Rat>` | Parse as rational |
| `str.show` | `(a) -> <> Str` | Convert any type to string repr (polymorphic) |

### 2. JSON (`std.json`)

JSON as a first-class sum type.

```
type Json
  = JNull
  | JBool(Bool)
  | JInt(Int)
  | JRat(Rat)
  | JStr(Str)
  | JArr([Json])
  | JObj([(Str, Json)])
```

| Word | Signature | Description |
|------|-----------|-------------|
| `json.enc` | `(Json) -> <> Str` | Encode Json value to string |
| `json.dec` | `(Str) -> <exn[Err]> Json` | Parse string to Json |
| `json.get` | `(Json, Str) -> <> Option<Json>` | Get field from JObj |
| `json.idx` | `(Json, Int) -> <> Option<Json>` | Index into JArr |
| `json.path` | `(Json, [Str]) -> <> Option<Json>` | Navigate nested path |
| `json.set` | `(Json, Str, Json) -> <> Json` | Set field in JObj (returns new) |
| `json.del` | `(Json, Str) -> <> Json` | Remove field from JObj |
| `json.keys` | `(Json) -> <exn[Err]> [Str]` | Keys of JObj (throws if not obj) |
| `json.vals` | `(Json) -> <exn[Err]> [Json]` | Values of JObj |
| `json.typ` | `(Json) -> <> Str` | Type tag: "null", "bool", "int", "rat", "str", "arr", "obj" |
| `json.int` | `(Json) -> <> Option<Int>` | Extract Int if JInt |
| `json.str` | `(Json) -> <> Option<Str>` | Extract Str if JStr |
| `json.bool` | `(Json) -> <> Option<Bool>` | Extract Bool if JBool |
| `json.arr` | `(Json) -> <> Option<[Json]>` | Extract array if JArr |
| `json.merge` | `(Json, Json) -> <exn[Err]> Json` | Merge two JObj (right wins) |

### 3. File I/O (`std.fs`)

File handles are affine — must be consumed exactly once (via `fs.close` or a consuming read/write).

```
affine type File   # opaque
type Mode = Read | Write | Append | ReadWrite
```

| Word | Signature | Description |
|------|-----------|-------------|
| `fs.open` | `(Str, Mode) -> <io, exn[Err]> !File` | Open file at path with mode |
| `fs.close` | `(!File) -> <io> ()` | Close file handle (consumes it) |
| `fs.read` | `(Str) -> <io, exn[Err]> Str` | Read entire file to string |
| `fs.readb` | `(Str) -> <io, exn[Err]> [Byte]` | Read entire file to bytes |
| `fs.write` | `(Str, Str) -> <io, exn[Err]> ()` | Write string to path (overwrite) |
| `fs.writeb` | `(Str, [Byte]) -> <io, exn[Err]> ()` | Write bytes to path (overwrite) |
| `fs.append` | `(Str, Str) -> <io, exn[Err]> ()` | Append string to file |
| `fs.lines` | `(Str) -> <io, exn[Err]> [Str]` | Read file as list of lines |
| `fs.exists` | `(Str) -> <io> Bool` | Check if path exists |
| `fs.rm` | `(Str) -> <io, exn[Err]> ()` | Remove file |
| `fs.mv` | `(Str, Str) -> <io, exn[Err]> ()` | Move/rename: src dst |
| `fs.cp` | `(Str, Str) -> <io, exn[Err]> ()` | Copy file: src dst |
| `fs.mkdir` | `(Str) -> <io, exn[Err]> ()` | Create directory (recursive) |
| `fs.ls` | `(Str) -> <io, exn[Err]> [Str]` | List directory entries |
| `fs.stat` | `(Str) -> <io, exn[Err]> FsStat` | File metadata |
| `fs.tmp` | `() -> <io, exn[Err]> Str` | Create temp file, return path |
| `fs.cwd` | `() -> <io> Str` | Current working directory |
| `fs.abs` | `(Str) -> <io> Str` | Resolve to absolute path |
| `fs.with` | `(Str, Mode, (!File) -> <io, exn[Err] \| e> a) -> <io, exn[Err] \| e> a` | Open, run function, auto-close |

```
type FsStat = {
  size: Int,
  dir: Bool,
  mod: Int     # modification time, unix epoch seconds
}
```

### 4. Collections (`std.col`)

Extensions to built-in list ops (`map`, `filter`, `fold`, `len`, `head`, `tail`, `cons`, `cat`, `concat`).

#### Lists

| Word | Signature | Description |
|------|-----------|-------------|
| `col.rev` | `([a]) -> <> [a]` | Reverse list |
| `col.sort` | `([a]) -> <> [a]` | Sort (requires Ord constraint) |
| `col.sortby` | `([a], (a, a) -> <> Ordering) -> <> [a]` | Sort with comparator |
| `col.uniq` | `([a]) -> <> [a]` | Remove adjacent duplicates |
| `col.zip` | `([a], [b]) -> <> [(a, b)]` | Zip two lists |
| `col.unzip` | `([(a, b)]) -> <> ([a], [b])` | Unzip to two lists |
| `col.flat` | `([[a]]) -> <> [a]` | Flatten one level |
| `col.flatmap` | `([a], (a) -> <e> [b]) -> <e> [b]` | Map then flatten |
| `col.take` | `([a], Int) -> <> [a]` | First n elements |
| `col.drop` | `([a], Int) -> <> [a]` | Drop first n elements |
| `col.nth` | `([a], Int) -> <> Option<a>` | Element at index |
| `col.find` | `([a], (a) -> <> Bool) -> <> Option<a>` | First match |
| `col.any` | `([a], (a) -> <> Bool) -> <> Bool` | Any element matches |
| `col.all` | `([a], (a) -> <> Bool) -> <> Bool` | All elements match |
| `col.count` | `([a], (a) -> <> Bool) -> <> Int` | Count matches |
| `col.enum` | `([a]) -> <> [(Int, a)]` | Pairs with indices |
| `col.chunk` | `([a], Int{> 0}) -> <> [[a]]` | Split into chunks of size n |
| `col.win` | `([a], Int{> 0}) -> <> [[a]]` | Sliding window of size n |
| `col.intersperse` | `([a], a) -> <> [a]` | Insert between elements |
| `range` | `(Int, Int) -> <> [Int]` | Integer range [start, end] |
| `col.rep` | `(a, Int) -> <> [a]` | Repeat value n times |
| `col.sum` | `([Int]) -> <> Int` | Sum of integers |
| `col.prod` | `([Int]) -> <> Int` | Product of integers |
| `col.min` | `([a]{len > 0}) -> <> a` | Minimum (requires Ord) |
| `col.max` | `([a]{len > 0}) -> <> a` | Maximum (requires Ord) |
| `col.group` | `([a], (a) -> <> k) -> <> [(k, [a])]` | Group by key function |
| `col.scan` | `([a], b, (b, a) -> <> b) -> <> [b]` | Running fold |

#### Maps (association lists / ordered maps)

```
type Map<k, v> = [(k, v)]   # ordered association list
```

| Word | Signature | Description |
|------|-----------|-------------|
| `map.new` | `() -> <> Map<k, v>` | Empty map |
| `map.of` | `([(k, v)]) -> <> Map<k, v>` | From list of pairs |
| `map.get` | `(Map<k, v>, k) -> <> Option<v>` | Lookup by key |
| `map.set` | `(Map<k, v>, k, v) -> <> Map<k, v>` | Insert or update |
| `map.del` | `(Map<k, v>, k) -> <> Map<k, v>` | Remove key |
| `map.has` | `(Map<k, v>, k) -> <> Bool` | Contains key |
| `map.keys` | `(Map<k, v>) -> <> [k]` | All keys |
| `map.vals` | `(Map<k, v>) -> <> [v]` | All values |
| `map.pairs` | `(Map<k, v>) -> <> [(k, v)]` | All key-value pairs |
| `map.len` | `(Map<k, v>) -> <> Int` | Number of entries |
| `map.merge` | `(Map<k, v>, Map<k, v>) -> <> Map<k, v>` | Merge (right wins) |
| `map.mapv` | `(Map<k, v>, (v) -> <e> w) -> <e> Map<k, w>` | Map over values |
| `map.filterv` | `(Map<k, v>, (v) -> <> Bool) -> <> Map<k, v>` | Filter by value |

#### Sets

```
type Set<a> = [a]   # deduplicated sorted list
```

| Word | Signature | Description |
|------|-----------|-------------|
| `set.new` | `() -> <> Set<a>` | Empty set |
| `set.of` | `([a]) -> <> Set<a>` | From list (dedup + sort) |
| `set.has` | `(Set<a>, a) -> <> Bool` | Membership test |
| `set.add` | `(Set<a>, a) -> <> Set<a>` | Insert element |
| `set.rm` | `(Set<a>, a) -> <> Set<a>` | Remove element |
| `set.union` | `(Set<a>, Set<a>) -> <> Set<a>` | Union |
| `set.inter` | `(Set<a>, Set<a>) -> <> Set<a>` | Intersection |
| `set.diff` | `(Set<a>, Set<a>) -> <> Set<a>` | Difference (left - right) |
| `set.len` | `(Set<a>) -> <> Int` | Cardinality |
| `set.list` | `(Set<a>) -> <> [a]` | Convert to sorted list |

### 5. HTTP Client (`std.http`)

```
type HttpReq = {
  method: Str,
  url: Str,
  headers: Map<Str, Str>,
  body: Option<Str>
}

type HttpRes = {
  status: Int,
  headers: Map<Str, Str>,
  body: Str
}
```

| Word | Signature | Description |
|------|-----------|-------------|
| `http.get` | `(Str) -> <io, async, exn[Err]> HttpRes` | GET request by URL |
| `http.post` | `(Str, Str) -> <io, async, exn[Err]> HttpRes` | POST: url body |
| `http.put` | `(Str, Str) -> <io, async, exn[Err]> HttpRes` | PUT: url body |
| `http.del` | `(Str) -> <io, async, exn[Err]> HttpRes` | DELETE: url |
| `http.patch` | `(Str, Str) -> <io, async, exn[Err]> HttpRes` | PATCH: url body |
| `http.req` | `(HttpReq) -> <io, async, exn[Err]> HttpRes` | Send custom request |
| `http.hdr` | `(HttpReq, Str, Str) -> <> HttpReq` | Add header to request |
| `http.json` | `(HttpRes) -> <exn[Err]> Json` | Parse response body as JSON |
| `http.ok?` | `(HttpRes) -> <> Bool` | Status in 200-299 |

### 6. Error Types (`std.err`)

```
type Err = {
  code: Str,
  msg: Str,
  ctx: Map<Str, Str>    # structured context
}

type Result<a, e> = Ok(a) | Fail(e)
type Option<a> = Some(a) | None
```

| Word | Signature | Description |
|------|-----------|-------------|
| `err.new` | `(Str, Str) -> <> Err` | Create error: code msg |
| `err.ctx` | `(Err, Str, Str) -> <> Err` | Add context key-value |
| `err.wrap` | `(Err, Str) -> <> Err` | Prepend to message |
| `ok` | `(a) -> <> Result<a, e>` | Wrap in Ok |
| `fail` | `(e) -> <> Result<a, e>` | Wrap in Fail |
| `ok?` | `(Result<a, e>) -> <> Bool` | Is Ok? |
| `unwrap` | `(Result<a, e>) -> <exn[e]> a` | Extract Ok or throw |
| `unwrap-or` | `(Result<a, e>, a) -> <> a` | Extract Ok or use default |
| `try` | `(() -> <exn[e] \| f> a) -> <f> Result<a, e>` | Catch exceptions to Result |
| `throw` | `(e) -> <exn[e]> a` | Throw exception |
| `some` | `(a) -> <> Option<a>` | Wrap in Some |
| `none` | `() -> <> Option<a>` | None value |
| `some?` | `(Option<a>) -> <> Bool` | Is Some? |
| `unwrap?` | `(Option<a>) -> <exn[Err]> a` | Extract Some or throw |
| `or-else` | `(Option<a>, a) -> <> a` | Extract Some or use default |
| `map-opt` | `(Option<a>, (a) -> <e> b) -> <e> Option<b>` | Map over Option |
| `map-res` | `(Result<a, e>, (a) -> <f> b) -> <f> Result<b, e>` | Map over Result |
| `and-then` | `(Result<a, e>, (a) -> <f> Result<b, e>) -> <f> Result<b, e>` | Chain Results |

### 7. Process Execution (`std.proc`)

```
type ProcResult = {
  code: Int,
  out: Str,
  err: Str
}
```

| Word | Signature | Description |
|------|-----------|-------------|
| `proc.run` | `(Str, [Str]) -> <io, exn[Err]> ProcResult` | Run command with args |
| `proc.sh` | `(Str) -> <io, exn[Err]> ProcResult` | Run shell command string |
| `proc.ok` | `(Str, [Str]) -> <io, exn[Err]> Str` | Run, throw if non-zero, return stdout |
| `proc.pipe` | `(Str, [Str], Str) -> <io, exn[Err]> ProcResult` | Run with stdin: cmd args stdin |
| `proc.bg` | `(Str, [Str]) -> <io, async, exn[Err]> !ProcHandle` | Start background process |
| `proc.wait` | `(!ProcHandle) -> <io, async> ProcResult` | Wait for background process |
| `proc.kill` | `(!ProcHandle) -> <io> ()` | Kill background process |
| `proc.exit` | `(Int) -> <io> ()` | Exit current process with code |
| `proc.pid` | `() -> <io> Int` | Current process ID |

### 8. Environment Variables (`std.env`)

| Word | Signature | Description |
|------|-----------|-------------|
| `env.get` | `(Str) -> <io> Option<Str>` | Get env var |
| `env.get!` | `(Str) -> <io, exn[Err]> Str` | Get env var or throw |
| `env.set` | `(Str, Str) -> <io> ()` | Set env var: key value |
| `env.rm` | `(Str) -> <io> ()` | Unset env var |
| `env.all` | `() -> <io> Map<Str, Str>` | All env vars as map |
| `env.has` | `(Str) -> <io> Bool` | Env var exists |

---

## Tier 2 — Requires `use`

### 9. HTTP Server (`std.srv`)

```
use std.srv

type Route = {
  method: Str,
  path: Str,
  handler: (SrvReq) -> <io, async | e> SrvRes
}

type SrvReq = {
  method: Str,
  path: Str,
  headers: Map<Str, Str>,
  body: Str,
  params: Map<Str, Str>,    # path params
  query: Map<Str, Str>      # query string params
}

type SrvRes = {
  status: Int,
  headers: Map<Str, Str>,
  body: Str
}

affine type Server   # opaque
```

| Word | Signature | Description |
|------|-----------|-------------|
| `srv.new` | `() -> <> [Route]` | Empty route list |
| `srv.get` | `([Route], Str, (SrvReq) -> <io, async \| e> SrvRes) -> <> [Route]` | Add GET route |
| `srv.post` | `([Route], Str, (SrvReq) -> <io, async \| e> SrvRes) -> <> [Route]` | Add POST route |
| `srv.put` | `([Route], Str, (SrvReq) -> <io, async \| e> SrvRes) -> <> [Route]` | Add PUT route |
| `srv.del` | `([Route], Str, (SrvReq) -> <io, async \| e> SrvRes) -> <> [Route]` | Add DELETE route |
| `srv.start` | `([Route], Int) -> <io, async, exn[Err]> !Server` | Start on port |
| `srv.stop` | `(!Server) -> <io> ()` | Stop server |
| `srv.res` | `(Int, Str) -> <> SrvRes` | Create response: status body |
| `srv.json` | `(Int, Json) -> <> SrvRes` | JSON response: status json |
| `srv.hdr` | `(SrvRes, Str, Str) -> <> SrvRes` | Add header to response |
| `srv.mw` | `([Route], (SrvReq, (SrvReq) -> <io, async \| e> SrvRes) -> <io, async \| e> SrvRes) -> <> [Route]` | Add middleware |

### 10. CLI Arguments (`std.cli`)

```
use std.cli

type CliOpt = {
  name: Str,
  short: Option<Str>,     # single char flag
  desc: Str,
  required: Bool,
  default: Option<Str>
}

type CliArgs = {
  cmd: Str,
  args: [Str],
  opts: Map<Str, Str>,
  flags: Set<Str>
}
```

| Word | Signature | Description |
|------|-----------|-------------|
| `cli.args` | `() -> <io> [Str]` | Raw argument list |
| `cli.parse` | `([CliOpt]) -> <io, exn[Err]> CliArgs` | Parse with schema |
| `cli.opt` | `(Str, Str, Str) -> <> CliOpt` | Create option: name short desc |
| `cli.req` | `(CliOpt) -> <> CliOpt` | Mark option required |
| `cli.def` | `(CliOpt, Str) -> <> CliOpt` | Set default value |
| `cli.get` | `(CliArgs, Str) -> <> Option<Str>` | Get option value |
| `cli.flag` | `(CliArgs, Str) -> <> Bool` | Check if flag set |
| `cli.pos` | `(CliArgs, Int) -> <> Option<Str>` | Positional arg by index |

### 11. Date/Time (`std.dt`)

```
use std.dt

type Dt = {
  year: Int, month: Int, day: Int,
  hour: Int, min: Int, sec: Int,
  tz: Str    # IANA timezone or "UTC"
}

type Duration = Int   # milliseconds
```

| Word | Signature | Description |
|------|-----------|-------------|
| `dt.now` | `() -> <io> Dt` | Current datetime UTC |
| `dt.unix` | `() -> <io> Int` | Current unix timestamp (seconds) |
| `dt.from` | `(Int) -> <> Dt` | Datetime from unix timestamp |
| `dt.to` | `(Dt) -> <> Int` | Datetime to unix timestamp |
| `dt.parse` | `(Str, Str) -> <exn[Err]> Dt` | Parse: value format |
| `dt.fmt` | `(Dt, Str) -> <> Str` | Format: dt format-string |
| `dt.add` | `(Dt, Duration) -> <> Dt` | Add duration |
| `dt.sub` | `(Dt, Dt) -> <> Duration` | Difference between datetimes |
| `dt.tz` | `(Dt, Str) -> <exn[Err]> Dt` | Convert timezone |
| `dt.iso` | `(Dt) -> <> Str` | Format as ISO 8601 |
| `dt.ms` | `(Int) -> <> Duration` | Milliseconds to duration |
| `dt.sec` | `(Int) -> <> Duration` | Seconds to duration |
| `dt.min` | `(Int) -> <> Duration` | Minutes to duration |
| `dt.hr` | `(Int) -> <> Duration` | Hours to duration |
| `dt.day` | `(Int) -> <> Duration` | Days to duration |

### 12. CSV (`std.csv`)

```
use std.csv

type CsvOpts = {
  delim: Str,       # default ","
  quote: Str,       # default "\""
  header: Bool      # default true
}
```

| Word | Signature | Description |
|------|-----------|-------------|
| `csv.dec` | `(Str) -> <exn[Err]> [[Str]]` | Parse CSV string to rows |
| `csv.enc` | `([[Str]]) -> <> Str` | Encode rows to CSV string |
| `csv.decf` | `(Str) -> <io, exn[Err]> [[Str]]` | Parse CSV file |
| `csv.encf` | `(Str, [[Str]]) -> <io, exn[Err]> ()` | Write CSV file |
| `csv.hdr` | `([[Str]]{len > 0}) -> <> [Str]` | Extract header row |
| `csv.rows` | `([[Str]]{len > 0}) -> <> [[Str]]` | Extract data rows (skip header) |
| `csv.maps` | `([[Str]]{len > 0}) -> <> [Map<Str, Str>]` | Rows as maps keyed by header |
| `csv.opts` | `(CsvOpts, Str) -> <exn[Err]> [[Str]]` | Parse with custom options |

### 13. Logging (`std.log`)

```
use std.log

type LogLevel = Trace | Debug | Info | Warn | Error
```

| Word | Signature | Description |
|------|-----------|-------------|
| `log.trace` | `(Str) -> <io> ()` | Log at trace level |
| `log.debug` | `(Str) -> <io> ()` | Log at debug level |
| `log.info` | `(Str) -> <io> ()` | Log at info level |
| `log.warn` | `(Str) -> <io> ()` | Log at warn level |
| `log.error` | `(Str) -> <io> ()` | Log at error level |
| `log.level` | `(LogLevel) -> <io> ()` | Set minimum log level |
| `log.ctx` | `(Str, Str) -> <io> ()` | Add persistent context key-value |
| `log.json` | `(Bool) -> <io> ()` | Enable/disable JSON output format |

Logs are structured JSON by default (agent-optimized):
```json
{"ts":"2026-03-14T10:00:00Z","level":"info","msg":"...","ctx":{}}
```

### 14. Testing (`std.test`)

```
use std.test

type TestResult = Pass | Fail(Str) | Skip(Str)
```

| Word | Signature | Description |
|------|-----------|-------------|
| `test.run` | `([Test]) -> <io> ()` | Run all tests, print report |
| `test.def` | `(Str, () -> <io \| e> ()) -> <> Test` | Define test: name body |
| `test.eq` | `(a, a) -> <exn[Err]> ()` | Assert equal |
| `test.neq` | `(a, a) -> <exn[Err]> ()` | Assert not equal |
| `test.ok` | `(Bool) -> <exn[Err]> ()` | Assert true |
| `test.fail` | `(Str) -> <exn[Err]> ()` | Fail with message |
| `test.throws` | `(() -> <exn[e]> a) -> <> Bool` | Function throws? |
| `test.near` | `(Rat, Rat, Rat) -> <exn[Err]> ()` | Assert within tolerance |
| `test.skip` | `(Str) -> <exn[Err]> ()` | Skip test with reason |
| `test.bench` | `(Str, Int, () -> <io \| e> ()) -> <io> ()` | Benchmark: name iterations body |
| `test.group` | `(Str, [Test]) -> <> [Test]` | Group tests under label |

Test output is structured JSON for agent consumption:
```json
{"suite":"...","tests":[{"name":"...","status":"pass","ms":1}],"pass":5,"fail":0,"skip":0}
```

### 15. Regex (`std.rx`)

```
use std.rx

type Rx        # compiled regex, opaque
type RxMatch = {
  full: Str,
  groups: [Option<Str>],
  start: Int,
  end: Int
}
```

| Word | Signature | Description |
|------|-----------|-------------|
| `rx.new` | `(Str) -> <exn[Err]> Rx` | Compile regex pattern |
| `rx.match` | `(&Rx, Str) -> <> Option<RxMatch>` | First match |
| `rx.all` | `(&Rx, Str) -> <> [RxMatch]` | All matches |
| `rx.test` | `(&Rx, Str) -> <> Bool` | Test if matches |
| `rx.rep` | `(&Rx, Str, Str) -> <> Str` | Replace all matches: rx input replacement |
| `rx.rep1` | `(&Rx, Str, Str) -> <> Str` | Replace first match |
| `rx.split` | `(&Rx, Str) -> <> [Str]` | Split by pattern |

### 16. Math (`std.math`)

All pure — no effects.

| Word | Signature | Description |
|------|-----------|-------------|
| `math.abs` | `(Int) -> <> Int` | Absolute value |
| `math.absr` | `(Rat) -> <> Rat` | Absolute value (rational) |
| `math.max` | `(Int, Int) -> <> Int` | Maximum of two |
| `math.min` | `(Int, Int) -> <> Int` | Minimum of two |
| `math.clamp` | `(Int, Int, Int) -> <> Int` | Clamp: value min max |
| `math.pow` | `(Int, Int{>= 0}) -> <> Int` | Integer exponentiation |
| `math.sqrt` | `(Rat{>= 0}) -> <> Rat` | Square root |
| `math.log` | `(Rat{> 0}) -> <> Rat` | Natural logarithm |
| `math.log2` | `(Rat{> 0}) -> <> Rat` | Base-2 logarithm |
| `math.log10` | `(Rat{> 0}) -> <> Rat` | Base-10 logarithm |
| `math.floor` | `(Rat) -> <> Int` | Floor to integer |
| `math.ceil` | `(Rat) -> <> Int` | Ceiling to integer |
| `math.round` | `(Rat) -> <> Int` | Round to nearest integer |
| `math.gcd` | `(Int, Int) -> <> Int` | Greatest common divisor |
| `math.lcm` | `(Int, Int) -> <> Int` | Least common multiple |
| `math.pi` | `() -> <> Rat` | Pi approximation |
| `math.e` | `() -> <> Rat` | Euler's number approximation |
| `math.sin` | `(Rat) -> <> Rat` | Sine (radians) |
| `math.cos` | `(Rat) -> <> Rat` | Cosine (radians) |
| `math.tan` | `(Rat) -> <> Rat` | Tangent (radians) |
| `math.rand` | `() -> <io> Rat` | Random rational in [0, 1) |
| `math.randi` | `(Int, Int) -> <io> Int` | Random integer in [low, high) |

---

## Usage Examples

### Example 1: Read JSON config, extract field

```clank
# Tier 1: auto-imported
let port = fs.read("config.json")
  |> json.dec
  |> json.get("port")
  |> unwrap?
  |> str.int
  |> unwrap?
# port : Int
```

### Example 2: HTTP GET, parse JSON, extract data

```clank
let data = http.get("https://api.example.com/users")
  |> http.json
  |> json.get("data")
  |> unwrap?
# data : Json (the data array)
```

### Example 3: File processing pipeline

```clank
let rows = fs.read("input.csv")
  |> csv.dec
  |> csv.rows
  |> map(fn(row) => col.nth(row, 0) |> unwrap?)
  |> col.sort
  |> col.uniq

fs.write("output.txt", join(rows, "\n"))
```

### Example 4: Simple HTTP server

```clank
use std.srv

let server = srv.new()
  |> srv.get("/health", fn(_req) => srv.res(200, "ok"))
  |> srv.post("/users", fn(req) =>
    let body = json.dec(req.body)
    # process user...
    srv.res(201, "created")
  )
  |> srv.start(8080)
```

### Example 5: CLI tool with arg parsing

```clank
use std.cli

let opts = [
  cli.opt("output", "o", "Output file path") |> cli.req,
  cli.opt("verbose", "v", "Enable verbose output")
]

let args = cli.parse(opts)
let outpath = cli.get(args, "output") |> unwrap?

if cli.flag(args, "verbose")
  then log.level(Debug)
  else log.level(Info)

log.info("Processing...")
# do work, write to outpath...
```

### Example 6: Test suite

```clank
use std.test

test.run([
  test.def("addition", fn() => test.eq(1 + 1, 2)),
  test.def("string concat", fn() => test.eq("hello " ++ "world", "hello world")),
  test.def("division by zero", fn() => test.ok(test.throws(fn() => 1 / 0))),
  test.def("list ops", fn() => do {
    test.eq(col.rev([1, 2, 3]), [3, 2, 1])
    test.eq(filter([1, 2, 3, 4, 5], fn(x) => x % 2 == 0), [2, 4])
  })
])
```

### Example 7: Regex extraction

```clank
use std.rx

let datepat = rx.new("\\d{4}-\\d{2}-\\d{2}")
let date = rx.match(datepat, "Born on 1990-05-21 in NYC")
  |> unwrap?
  |> fn(m) => m.full
# date : "1990-05-21"
```

---

## Design Decisions & Rationale

### D1: Dot-namespaced word names
Words use `module.word` naming (e.g., `fs.read`, `json.dec`). This avoids collisions, enables discovery ("what can `fs.` do?"), and keeps names terse (3-8 chars typical). Single tokens parse faster than multi-word identifiers.

### D2: Convenience vs. composability split
High-frequency operations get shortcut words (e.g., `fs.read` reads entire file as string) alongside lower-level composable words (e.g., `fs.open` + `fs.close`). This matches agent usage: most agent programs use the shortcut; rare cases compose from primitives.

### D3: Association lists for maps
Maps are `[(k, v)]` association lists rather than hash tables. Simpler semantics, composable with existing list words, and sufficient for agent-scale programs (rarely millions of entries). If performance becomes critical, the VM can optimize internally while preserving the logical model.

### D4: Structured output for testing and logging
Both `test.run` and logging emit structured JSON. Agents parse JSON reliably; human-readable prose requires heuristic extraction. This is a core Clank principle: machine-readable by default.

### D5: Affine resources throughout I/O
File handles, server handles, and process handles are affine (`!T`). This eliminates resource leak bugs at compile time — the type checker ensures every handle is consumed exactly once. `fs.with` provides a bracket pattern for the common case.

### D6: Effect annotations everywhere
Every word declares its effects. Pure words (`<>`) compose freely. Effectful words (`<io>`, `<exn>`, etc.) propagate effects to callers. No hidden side effects means agents can reason about what a word does from its signature alone.

### D7: Terse but not cryptic
Names like `str.has`, `col.rev`, `dt.fmt` are shorter than `string.contains`, `collection.reverse`, `datetime.format` but remain guessable. An agent encountering `str.pfx` for the first time can infer "string prefix" from context + type signature.

### D8: Refinement types for preconditions
Words like `col.min` require `[a]{len > 0}` (non-empty list). `math.sqrt` requires `Rat{>= 0}`. These are checked at compile time by the SMT solver, eliminating a class of runtime errors that agents commonly encounter.

### D9: Ordering type for comparisons
`col.sortby` takes a comparator returning `Ordering` (LT | EQ | GT) rather than an integer. This is more type-safe — the comparator's intent is unambiguous, and pattern matching on `Ordering` is exhaustive. The `Ordering` type is in the prelude since it's needed by any code that does custom sorting.

### D10: String concatenation operator (`++`)
The `++` operator is dedicated to string concatenation, separate from `+` (arithmetic) and `cat` (list concatenation). This follows the no-overloading principle: each operator has exactly one meaning. `++` desugars to `str.cat`.

---

## Built-in Words (Not Module-Qualified)

The following words are built-in primitives available without any import. In
addition to the arithmetic operators (`+`, `-`, `*`, `/`, `%`), comparison
operators (`==`, `!=`, `<`, `>`, `<=`, `>=`), logic operators (`&&`, `||`,
`!`), and the string concatenation operator (`++`), the following named
built-ins are available:

| Word | Signature | Description |
|------|-----------|-------------|
| `map` | `([a], (a) -> <e> b) -> <e> [b]` | Apply function to each element |
| `filter` | `([a], (a) -> <e> Bool) -> <e> [a]` | Keep elements matching predicate |
| `fold` | `([a], b, (b, a) -> <e> b) -> <e> b` | Left fold |
| `len` | `([a]) -> <> Int` | List length |
| `head` | `([a]{len > 0}) -> <> a` | First element |
| `tail` | `([a]{len > 0}) -> <> [a]` | All elements except first |
| `cons` | `(a, [a]) -> <> [a]` | Prepend element |
| `cat` | `([a], [a]) -> <> [a]` | Concatenate two lists |
| `concat` | `([[a]]) -> <> [a]` | Flatten one level of nesting (equivalent to `col.flat`) |
| `cmp` | `(a, a) -> <> Ordering` | Three-way comparison (requires `Ord` constraint) |
| `print` | `(Str) -> <io> ()` | Print string to stdout |
| `show` | `(a) -> <> Str` | Convert any value to string (requires `Show` constraint) |
| `eq` | `(a, a) -> <> Bool` | Structural equality (requires `Eq` constraint) |
| `split` | `(Str, Str) -> <> [Str]` | Split string on delimiter |
| `join` | `([Str], Str) -> <> Str` | Join strings with delimiter |
| `trim` | `(Str) -> <> Str` | Trim whitespace |

---

## Word Count Summary

| Category | Module | Tier | Words |
|----------|--------|------|-------|
| Strings | `std.str` | 1 | 24 |
| JSON | `std.json` | 1 | 15 |
| File I/O | `std.fs` | 1 | 19 |
| Collections | `std.col` | 1 | 26 + 13 + 10 = 49 |
| HTTP Client | `std.http` | 1 | 9 |
| Error Types | `std.err` | 1 | 18 |
| Process Exec | `std.proc` | 1 | 9 |
| Env Vars | `std.env` | 1 | 6 |
| HTTP Server | `std.srv` | 2 | 11 |
| CLI Args | `std.cli` | 2 | 8 |
| Date/Time | `std.dt` | 2 | 15 |
| CSV | `std.csv` | 2 | 8 |
| Logging | `std.log` | 2 | 8 |
| Testing | `std.test` | 2 | 11 |
| Regex | `std.rx` | 2 | 7 |
| Math | `std.math` | 2 | 22 |
| **Total** | | | **240** |

Plus ~37 built-in words (arithmetic, comparison, logic, list/string/IO, `cmp`, `concat`, `++` operator) = **~277 total words** in the language.
