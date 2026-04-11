# Clank Language Specification

Clank is a strongly-typed, applicative-primary language designed for AI agent authorship. Pipeline syntax (`|>`) provides left-to-right composition sugar. All tokens are ASCII. Algebraic effects provide extensible control flow. Execution is via bytecode compiler + stack VM.

## 1. Lexical Structure

```
ident       = alpha { alpha | digit | '_' | '-' }
keywords    = let in fn if then else match do type effect affine
              handle resume perform mod use pub clone true false
              interface impl Self deriving where opaque return test
              alias discard
comment     = '#' ... newline
```

Primitives: `Int` (64-bit signed, overflow traps), `Rat` (IEEE 754 double, overflow traps), `Bool`, `Str` (UTF-8), `Byte`, `()` (unit).

Compounds: `[T]` (list), `(T, U)` (tuple), `{k: T}` (record), `T | U` (tagged union), `T -> <E> U` (function with effects E).

String interpolation: `"hello ${expr}"` desugars to `"hello " ++ show(expr)`. Escape with `\$`.

## 2. Grammar (Core)

```ebnf
program     = { top-level } ;
top-level   = definition | type-decl | effect-decl | effect-alias
            | mod-decl | use-decl | test-decl
            | interface-decl | impl-block ;

definition  = ['pub'] ident ':' type-sig '=' expr ;
type-sig    = '(' params ')' '->' effect-ann type-expr ;

type-decl   = ['pub'] ['affine'] 'type' ident [type-params] '=' variant { '|' variant }
              ['deriving' '(' ident { ',' ident } ')'] ;
variant     = ident ['(' type-expr { ',' type-expr } ')'] ;

effect-decl = ['pub'] 'effect' ident '{' { op-sig } '}' ;
effect-alias = ['pub'] 'effect' 'alias' ident '=' '<' effects '>' ;

interface-decl = ['pub'] 'interface' ident [type-params] '{' { method-sig } '}' ;
impl-block  = ['pub'] 'impl' ident [type-args] 'for' type-expr '{' { method } '}' ;

test-decl   = 'test' string '=' expr ;
```

### Expressions (precedence low→high)

```ebnf
expr        = let-expr | if-expr | match-expr | block
            | handle-expr | for-expr | pipe-expr ;

pipe-expr   = or-expr { '|>' or-expr } ;
or-expr     = and-expr { '||' and-expr } ;
and-expr    = cmp-expr { '&&' cmp-expr } ;
cmp-expr    = concat-expr [ cmp-op concat-expr ] ;
concat-expr = add-expr { '++' add-expr } ;
add-expr    = mul-expr { ('+' | '-') mul-expr } ;
mul-expr    = unary-expr { ('*' | '/' | '%') unary-expr } ;
unary-expr  = ['-' | '!'] postfix-expr ;
postfix-expr = atom { '(' args ')' | '.' ident } ;

atom        = literal | ident | lambda | '(' expr ')' | tuple | list | record
            | block | match-expr | if-expr ;
lambda      = 'fn' '(' params ')' '=>' expr ;
let-expr    = 'let' pattern '=' expr ['in' expr] ;  (* 'in' is optional in match arms and lambdas — the next expression becomes the body *)
if-expr     = 'if' expr 'then' expr 'else' expr ;
match-expr  = 'match' expr '{' { pattern '=>' expr } '}' ;
block       = '{' expr { expr } '}' ;
handle-expr = 'handle' expr '{' handler-arms '}' ;
handler-arms = handler-arm { ',' handler-arm } ;
handler-arm  = 'return' ident '->' expr
             | op-name ident* 'resume' ident '->' expr ;
for-expr    = 'for' pattern 'in' expr ['if' expr] ['fold' ident '=' expr] 'do' expr ;
```

All operators desugar to function calls: `a + b` → `add(a, b)`, `a |> f(b)` → `f(a, b)`, `a ++ b` → `concat(a, b)` (works on strings and lists).

**Unary minus** negates a numeric expression: `-x`, `-3.14`, `-(a + b)`. Use it directly — `0 - x` is unnecessary.

**Multi-statement expressions:** Lambdas, `if`/`else` branches, `for...do` bodies, and handler arms each accept a single expression. Use `{ ... }` blocks to sequence multiple expressions, or chain `let` bindings (each `let` scopes over the next expression). For example: `fn(x) => { let y = x + 1  print(show(y))  y }` or `if cond then { print("yes")  42 } else 0`. Blocks are distinguished from records by lookahead: `{ ident: ... }` is a record, everything else is a block.

## 3. Type System

### 3.1 Effects
Every function type includes an effect annotation: `T -> <effects> U`.

```
<>              -- pure
<io>            -- I/O (print, fs, http, proc, env)
<exn[E]>        -- may raise E
<async>         -- async operations (spawn, await, channels)
```

Built-in effects: `io`, `exn[E]`, `async`. User-defined via `effect Name { op : T -> U }`. Effect aliases: `effect alias Pure = <>`.

Handlers interpret effects. The `return` clause transforms the final value. Operation clauses bind the operation's arguments, then the `resume` keyword, then a continuation identifier `k`. Use `k(value)` to resume the computation, or omit it to abort:
```
handle computation {
  return x -> result-expr,
  op-name arg resume k -> k(value),   # resume with value
  other-op arg resume _ -> default    # abort (don't call k)
}
```

Note: `k(value)` transfers control back into the handled computation — it does not return a value to the handler arm. The result of the entire `handle` expression flows through the `return` clause. To perform side effects before resuming, use `let ... in`: `let _ = print("log") in k(value)`.

### 3.2 Records
Structural key-value maps with row polymorphism:
```
{name: "Ada", age: 30}         -- literal
rec.name                        -- access
{age: 31, ..rec}                -- spread (new record, override age)
```

### 3.3 Interfaces & Deriving
```
interface Show { show : (Self) -> Str }
impl Show for MyType { show = fn(self) => ... }
type Color = Red | Green | Blue deriving (Show, Eq, Ord)
```
Built-in interfaces: `Show`, `Eq`, `Ord`, `Clone`, `Default`, `From`, `Into`.

### 3.4 Refinement Types
Predicates on numeric types verified at compile time (QF_LIA micro-solver):
```
div : (n: Int, d: Int{> 0}) -> <> Int
```

### 3.5 Affine Types
Resources that must be used at most once:
```
affine type File = File(Int)
```
Tracked via move semantics. Explicit `clone` required for reuse.

### 3.6 Type Inference
Hindley-Milner with: literal types, let-binding, if/match branch unification, lambda/application, exhaustiveness checking, path-sensitive refinement tracking.

## 4. Module System

```
mod math.stats
use mathlib (double, triple)       # unqualified: call as double(x)
use utils.format                    # qualified: call as format.func-name(x)
use utils.format as fmt             # aliased: call as fmt.func-name(x)

pub mean : (xs: [Rat]) -> <> Rat = ...
```

- **1:1 file mapping**: `math/stats.clk` → `mod math.stats`
- **Unqualified import**: `use foo (x, y)` — call as `x()`, `y()`
- **Qualified import**: `use foo` — call as `foo.x()`, `foo.y()`
- **Aliased import**: `use foo as f` — call as `f.x()`, `f.y()`
- **Importing a type** automatically includes its constructors
- **Visibility**: private by default, `pub` to export
- **Builtins need no import**: `print`, `show`, `map`, `filter`, etc. and all `std.*` modules are always available
- **Cross-directory resolution**: modules resolve relative to the importing file, then fall back to the project root (entry file's directory)

## 5. Built-in Functions (always available)

### Core
| Category | Functions |
|----------|-----------|
| Arithmetic | `add`, `sub`, `mul`, `div`, `mod`, `negate` |
| Comparison | `eq`, `neq`, `lt`, `gt`, `lte`, `gte` |
| Logic | `and`, `or`, `not` |
| Strings | `str.cat`, `show`, `print`, `split`, `join`, `trim`, `str.get`, `str.slc`, `str.has`, `str.idx`, `str.ridx`, `str.pfx`, `str.sfx`, `str.up`, `str.lo`, `str.rep`, `str.rep1`, `str.pad`, `str.lpad`, `str.rev`, `str.lines`, `str.words`, `str.chars`, `str.int`, `str.rat` |
| Lists | `len`, `head`, `tail`, `cons`, `cat`, `rev`, `get`, `map`, `filter`, `fold`, `flat-map`, `range`, `zip` |
| Tuples | `fst`, `snd`, `tuple.get` |
| Concat | `concat` (`++` operator — works on strings and lists) |
| Effects | `raise` |
| For-loops | `for x in list do expr`, `for x in list if pred do expr`, `for x in list fold acc = init do expr` |

### Standard Library (module-qualified, always available)

| Module | Functions | Purpose |
|--------|-----------|---------|
| `fs` | `fs.read`, `fs.write`, `fs.exists`, `fs.ls`, `fs.mkdir`, `fs.rm` | File I/O |
| `json` | `json.enc`, `json.dec`, `json.get`, `json.set`, `json.keys`, `json.merge` | JSON encode/decode |
| `env` | `env.get`, `env.set`, `env.has`, `env.all` | Environment variables |
| `proc` | `proc.run`, `proc.sh`, `proc.exit` | Process execution |
| `http` | `http.get`, `http.post`, `http.put`, `http.del`, `http.set-timeout` | HTTP client |
| `rx` | `rx.ok`, `rx.find`, `rx.replace`, `rx.split` | Regex |
| `math` | `math.abs`, `math.min`, `math.max`, `math.floor`, `math.ceil`, `math.sqrt` | Math |
| `col` | `col.rev`, `col.sort`, `col.sortby`, `col.uniq`, `col.zip`, `col.unzip`, `col.flat`, `col.flatmap`, `col.take`, `col.drop`, `col.nth`, `col.find`, `col.any`, `col.all`, `col.count`, `col.enum`, `col.chunk`, `col.win`, `col.intersperse`, `col.rep`, `col.sum`, `col.prod`, `col.min`, `col.max`, `col.group`, `col.scan` | Collection operations |
| `iter` | `iter.of`, `iter.range`, `iter.collect`, `iter.map`, `iter.filter`, `iter.take`, `iter.drop`, `iter.fold`, `iter.count`, `iter.sum`, `iter.any`, `iter.all`, `iter.find`, `iter.each`, `iter.drain`, `iter.enumerate`, `iter.chain`, `iter.zip`, `iter.take-while`, `iter.drop-while`, `iter.flatmap`, `iter.first`, `iter.last`, `iter.join`, `iter.repeat`, `iter.once`, `iter.empty`, `iter.unfold`, `iter.scan`, `iter.dedup`, `iter.chunk`, `iter.window`, `iter.intersperse`, `iter.cycle`, `iter.nth`, `iter.min`, `iter.max`, `iter.generate` | Lazy iterators |
| `dt` | `dt.now`, `dt.unix`, `dt.from`, `dt.to`, `dt.parse`, `dt.fmt`, `dt.add`, `dt.sub`, `dt.tz`, `dt.iso`, `dt.ms`, `dt.sec`, `dt.min`, `dt.hr`, `dt.day` | Date/time |
| `csv` | `csv.dec`, `csv.enc`, `csv.decf`, `csv.encf`, `csv.hdr`, `csv.rows`, `csv.maps`, `csv.opts` | CSV encode/decode |
| `log` | `log.trace`, `log.debug`, `log.info`, `log.warn`, `log.error`, `log.level`, `log.ctx`, `log.json` | Structured logging (stderr) |
| `cli` | `cli.args`, `cli.parse`, `cli.opt`, `cli.req`, `cli.def`, `cli.get`, `cli.flag`, `cli.pos` | CLI argument parsing |
| `srv` | `srv.new`, `srv.get`, `srv.post`, `srv.put`, `srv.del`, `srv.start`, `srv.stop`, `srv.res`, `srv.json`, `srv.hdr`, `srv.mw` | HTTP server |
| streaming | `fs.stream-lines`, `http.stream-lines`, `proc.stream`, `io.stdin-lines` | Lazy line iterators |

### Async & Concurrency

Each `spawn` launches a real goroutine — spawned tasks run in parallel. This means I/O-bound work (HTTP requests, file reads, process execution) in separate tasks executes concurrently. Cancellation (via `task-group` failure or explicit cancel) interrupts blocked I/O operations (sleep, HTTP, process execution, channel recv/send) immediately.

| Function | Signature | Purpose |
|----------|-----------|---------|
| `spawn` | `(() -> a) -> <async> Future[a]` | Spawn task (runs in parallel goroutine) |
| `await` | `Future[a] -> <async> a` | Wait for result |
| `channel` | `Int -> <async> (Sender[a], Receiver[a])` | Create channel |
| `send` | `(Sender[a], a) -> <async> ()` | Send value |
| `recv` | `Receiver[a] -> <async> a` | Receive value (blocks until available) |
| `try-recv` | `Receiver[a] -> <async> Option[a]` | Non-blocking receive |
| `close-sender` | `Sender[a] -> <async> ()` | Close sender half |
| `close-receiver` | `Receiver[a] -> <async> ()` | Close receiver half |
| `sleep` | `Int -> <async> ()` | Sleep milliseconds (interruptible) |
| `task-group` | `(() -> a) -> <async> a` | Structured concurrency (cancels children on failure) |
| `shield` | `(() -> a) -> <async> a` | Cancellation protection |
| `http.set-timeout` | `Int -> <io> ()` | Set HTTP timeout in ms (default: 30000) |

### Mutable State (Refs & TVars)

Refs are mutable reference cells. TVars are transactional variables for STM.

| Function | Signature | Purpose |
|----------|-----------|---------|
| `ref-new` | `a -> Ref[a]` | Create mutable ref |
| `ref-read` | `Ref[a] -> a` | Read current value |
| `ref-write` | `(Ref[a], a) -> ()` | Overwrite value |
| `ref-cas` | `(Ref[a], a, a) -> (Bool, a)` | Compare-and-swap: if current == expected, set new; returns (success, old) |
| `ref-modify` | `(Ref[a], (a) -> a) -> a` | Apply function to ref value, return new value |
| `ref-close` | `Ref[a] -> ()` | Close ref (affine cleanup) |
| `ref-swap` | `(Ref[a], a) -> a` | Swap value, return old |
| `tvar-new` | `a -> TVar[a]` | Create transactional variable |
| `tvar-read` | `TVar[a] -> a` | Read (inside `atomically`) |
| `tvar-write` | `(TVar[a], a) -> ()` | Write (inside `atomically`) |
| `tvar-take` | `TVar[a] -> a` | Take value (empties cell) |
| `tvar-put` | `(TVar[a], a) -> ()` | Put value (cell must be empty) |
| `atomically` | `(() -> a) -> a` | Run STM transaction |
| `or-else` | `(() -> a, () -> a) -> a` | Try first, fall back to second on `retry()` |
| `retry` | `() -> a` | Abort current STM branch |

### Common Types

`Option` and `Ordering` are used by many builtins but must be declared in your program if you pattern match on them:

```
type Option<a> = Some(a) | None
type Ordering = Lt | Eq_ | Gt
```

Functions that return `Option`: `json.get`, `env.get`, `col.nth`, `col.find`, `iter.find`, `iter.first`, `iter.last`, `iter.nth`, `iter.min`, `iter.max`, `str.int`, `str.rat`, `cli.get`, `cli.pos`, `try-recv`.

### Error Handling

Clank uses algebraic effects for errors, not try/catch. The `raise` effect propagates errors; `handle` blocks catch them:

```
# Safe division — returns Option
safe-div : (n: Int, d: Int) -> <> Option<Int> =
  handle div(n, d) {
    return x -> Some(x),
    raise _ resume _ -> None
  }

# HTTP with error recovery
fetch : (url: Str) -> <io> Str =
  handle http.get(url) {
    return resp -> resp.body,
    raise msg resume _ -> "error: " ++ msg
  }
```

### Package Management

Packages are declared in `clank.pkg` at the project root:

```
[package]
name = my-project
version = 0.1.0

[deps]
utils = { github = "user/clank-utils", version = ">=1.0.0" }
local-lib = { path = "../lib" }
```

Commands: `clank pkg init`, `clank pkg add <name>`, `clank pkg remove <name>`, `clank pkg resolve`, `clank pkg verify`.

Dependencies are resolved from GitHub repositories or local paths. Use imported packages with `use`:

```
use utils.helpers (format-date, parse-url)
```

## 6. CLI

```bash
clank <file>                    # Run a .clk file
clank check <file>              # Type-check only
clank eval <file>               # Evaluate and print result
clank fmt <file>                # Format source code
clank lint <file>               # Lint source code
clank doc [target]              # List docs for a target (default: current project)
clank doc search <q> [target]   # Search docs; target is optional
clank doc show <name> [target]  # Show one entry in detail
clank test [dir]                # Run tests
clank pkg <init|add|remove>     # Package management
clank spec                      # Print this specification
```

All commands support `--json` for structured output.

## 7. Example

```
type Shape = Circle(Rat) | Rect(Rat, Rat)

area : (s: Shape) -> <> Rat =
  match s {
    Circle(r) => r * r * 3.14159
    Rect(w, h) => w * h
  }

main : () -> <io> () =
  let shapes = [Circle(5.0), Rect(3.0, 4.0)]
  let areas = map(shapes, fn(s) => area(s))
  for a in areas do print("area: ${a}")

  let data = fs.read("config.json")
  let config = json.dec(data)
  match json.get(config, "name") {
    Some(name) => print("Hello, ${name}")
    None => print("no name found")
  }
```

## 8. Contributing

If you encounter a bug, missing feature, or have an improvement to suggest for the Clank language, you are encouraged to implement the fix or feature yourself and open a pull request at https://github.com/dalurness/clank. Include tests for any new functionality. The codebase is Go — see `internal/` for the compiler and VM, and `test/` for integration tests.
