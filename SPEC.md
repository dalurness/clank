# Clank Language Specification v1.0

Clank is a strongly-typed, applicative-primary language designed for AI agent authorship. Pipeline syntax (`|>`) provides left-to-right composition sugar. All tokens are ASCII. Types do the heavy lifting — refinement types, algebraic effects, affine types, row polymorphism, and interfaces compose orthogonally.

## 1. Lexical Structure

```
ident       = alpha { alpha | digit | '_' | '-' }
keywords    = let in fn if then else match do type effect affine
              handle resume mod use pub clone true false interface
              impl Self deriving where opaque
comment     = '#' ... newline
```

Primitives: `Int` (arbitrary-precision), `Rat` (rational), `Bool`, `Str` (UTF-8), `Byte`, `()` (unit).

Compounds: `[T]` (list), `(T, U)` (tuple), `{k: T}` (record), `T | U` (tagged union), `T -> <E> U` (function with effects E).

## 2. Grammar (Core)

```ebnf
program     = { top-level } ;
top-level   = definition | type-decl | effect-decl | interface-decl
            | impl-block | mod-decl | use-decl ;

definition  = ['pub'] ident ':' type-sig '=' expr ;
type-sig    = '(' params ')' '->' effect-ann type-expr ;

type-decl   = ['pub' ['opaque']] 'type' ident [type-params] '=' type-body
              [deriving] ;
            | 'affine' 'type' ident [type-params] '=' type-body ;
type-body   = type-expr | variant { '|' variant } ;
variant     = ident ['(' type-expr { ',' type-expr } ')'] ;

effect-decl = ['pub'] 'effect' ident [type-params] '{' { op-sig } '}' ;
```

### Expressions (precedence low→high)

```ebnf
expr        = let-expr | if-expr | match-expr | do-block
            | handle-expr | pipe-expr ;

pipe-expr   = or-expr { '|>' or-expr } ;
or-expr     = and-expr { '||' and-expr } ;
and-expr    = cmp-expr { '&&' cmp-expr } ;
cmp-expr    = concat-expr [ cmp-op concat-expr ] ;      (* non-assoc *)
concat-expr = add-expr { '++' concat-expr } ;           (* right-assoc *)
add-expr    = mul-expr { ('+' | '-') mul-expr } ;
mul-expr    = unary-expr { ('*' | '/' | '%') unary-expr } ;
unary-expr  = ['-' | '!'] postfix-expr ;
postfix-expr = atom { '(' args ')' | '.' ident } ;

atom        = literal | ident | lambda | '(' expr ')' | tuple | list | record ;
lambda      = 'fn' '(' params ')' '=>' expr ;
let-expr    = 'let' ident '=' expr ['in' expr] ;
if-expr     = 'if' expr 'then' expr 'else' expr ;
match-expr  = 'match' expr '{' { pattern '=>' expr } '}' ;
do-block    = 'do' '{' { [ident '<-'] expr } '}' ;
handle-expr = 'handle' expr '{' { handler-arm } '}' ;
```

All operators desugar to function calls. `a |> f(b)` = `f(a, b)`. `a ++ b` = `str.cat(a, b)`. Comparison is non-associative.

## 3. Type System

### 3.1 Effect System (Koka-style)

Every function type includes an effect row: `T -> <effects> U`.

```
<>                  -- pure
<io>                -- I/O
<exn[E]>            -- may raise E
<state[S]>          -- mutable state S
<async>             -- async operations
<io, exn | e>       -- open row (polymorphic tail)
```

Built-in effects: `io` (not handleable), `exn[E]`, `state[S]`, `async`. User-defined effects via `effect Name { op : T -> U }`. Effect rows are unordered sets with row-polymorphic unification (Rémy-style).

**Handlers** interpret effects:

```
handle computation {
  return(x) => ...,
  op(v, resume, k) => ... k(result) ...
}
```

The continuation `k` may be called 0, 1, or many times. Effect-polymorphic functions use row variables: `map : ([a], (a) -> <e> b) -> <e> [b]`.

### 3.2 Refinement Types

`{T | P}` — values of type T satisfying predicate P. Predicates are QF_UFLIRA (decidable, verified by SMT solver). `v` refers to the refined value.

```
div : (n: Int, d: Int{> 0}) -> <> Int
mean : (xs: [Rat]{len > 0}) -> <> Rat
clamp : (lo: Int, hi: Int{>= lo}, x: Int) -> <> Int{>= lo && <= hi}
```

Shorthand: `Int{> 0}` means `{Int | v > 0}`. Built-in measures: `len` (lists, strings), `fst`, `snd`. User measures via `measure` keyword (must be total, pure, structurally recursive). Liquid type inference discovers refinements for unannotated bindings.

### 3.3 Affine Types

Types are unrestricted (use freely) or affine (use at most once). Primitives are unrestricted. Compound types inherit affinity from components.

```
affine type File
affine type Connection
```

- **Move**: using an affine value consumes it; further use is a compile error
- **Borrow**: `&x` creates a read-only reference without consuming; scoped to one call, cannot be stored or returned
- **Clone**: `clone &x` explicitly duplicates (requires `Clone` interface)
- **Discard**: `discard x` explicitly abandons a resource (linter-flaggable)
- Branches must consume the same affine values on all paths
- GC handles memory; affine types enforce resource protocols (close, disconnect)

### 3.4 Row Polymorphism (Records)

Records support open types via row variables:

```
{name: Str}              -- closed: exactly these fields
{name: Str | r}          -- open: at least name, r = rest
```

Operations: `rec.field` (access), `rec{field: val}` (update), `{f: v, ..base}` (spread), `{f | rest}` (open destructure). Row unification is Rémy-style, preserves principal types. Closed records reject extra fields; open records accept them.

### 3.5 Interfaces (Typeclasses)

```
interface Show { show : (Self) -> <> Str }
interface Ord : Eq { cmp : (Self, Self) -> <> Ordering }
interface Into<T> { into : (Self) -> <> T }

impl Show for Int { show = int-to-str }
type Color = Red | Green | Blue deriving (Eq, Show, Clone)
```

Constraints via `where`: `sort : ([a]) -> <> [a] where Ord a`. Built-in interfaces: `Clone`, `Show`, `Eq`, `Ord`, `Default`, `Into`/`From`. Monomorphized at compile time. Coherence: one impl per type-interface pair; orphan impls are warnings.

## 4. Module System

```
mod math.stats
use std.io (print, read)
use std.list (map, filter)

pub mean : (xs: [Rat]{len > 0}) -> <> Rat = ...
```

- **1:1 file mapping**: `src/math/stats.clk` → `mod math.stats`
- **Explicit imports**: every name listed, no globs
- **Visibility**: private by default, `pub` to export
- **`pub opaque type`**: exports name without constructors
- **Public functions must have explicit effect annotations**
- **Impls are always public** (coherence requires it)
- **Packages**: `clank.pkg` manifest, `std` always available

## 5. Standard Library (240 words)

### Tier 1 (auto-imported)

| Module | Words | Key functions |
|--------|-------|---------------|
| `std.str` | 24 | `len get slc has split join trim up lo rep cat fmt` |
| `std.json` | 15 | `enc dec get set path keys merge` |
| `std.fs` | 19 | `open close read write lines exists ls mkdir with` |
| `std.col` | 49 | `rev sort zip flat flatmap take drop find any all range group scan` + Map/Set ops |
| `std.http` | 9 | `get post put del req json` |
| `std.err` | 18 | `ok fail unwrap try throw some none` + Option/Result ops |
| `std.proc` | 9 | `run sh ok pipe bg wait kill exit` |
| `std.env` | 6 | `get set rm all has` |

### Tier 2 (requires `use`)

`std.srv` (HTTP server, 11), `std.cli` (arg parsing, 8), `std.dt` (datetime, 15), `std.csv` (8), `std.log` (structured JSON logging, 8), `std.test` (11), `std.rx` (regex, 7), `std.math` (22).

Key types: `Json` (sum type), `Result<a, e>`, `Option<a>`, `Ordering`, `Err`, `Map<k, v>` (association list). File/server/process handles are affine. All functions declare effects. Test and log output is structured JSON.

## 6. Example

```
mod app.main

use std.io (print)
use std.fs (read)
use std.str (split, show)

word-count : (path: Str) -> <io, exn> () =
  path |> read |> split(" ") |> len |> show |> print

factorial : (n: Int{>= 0}) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)

effect DivError { div-by-zero : () -> <> () }

safe-div : (a: Int, b: Int) -> <DivError> Int =
  if b == 0 then div-by-zero() else a / b

type Shape = Circle(Rat) | Rect(Rat, Rat)

area : (s: Shape) -> <> Rat =
  match s {
    Circle(r) => r * r * 3.14159
    Rect(w, h) => w * h
  }
```
