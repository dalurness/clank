# Clank Language Specification v1.0

Clank is a strongly-typed, applicative-primary language designed for AI agent authorship. Pipeline syntax (`|>`) provides left-to-right composition sugar. All tokens are ASCII. Algebraic effects provide extensible control flow.

## 1. Lexical Structure

```
ident       = alpha { alpha | digit | '_' | '-' }
keywords    = let in fn if then else match do type effect
              handle resume mod use pub true false
reserved    = affine clone interface impl Self deriving where opaque
comment     = '#' ... newline
```

Reserved keywords are lexed but have no semantic meaning in the current implementation. They exist to prevent collision with planned features (see ROADMAP.md).

Primitives: `Int` (arbitrary-precision), `Rat` (rational), `Bool`, `Str` (UTF-8), `Byte`, `()` (unit).

Compounds: `[T]` (list), `(T, U)` (tuple), `{k: T}` (record), `T | U` (tagged union), `T -> <E> U` (function with effects E).

## 2. Grammar (Core)

```ebnf
program     = { top-level } ;
top-level   = definition | type-decl | effect-decl | mod-decl | use-decl ;

definition  = ['pub'] ident ':' type-sig '=' expr ;
type-sig    = '(' params ')' '->' effect-ann type-expr ;

type-decl   = ['pub'] 'type' ident [type-params] '=' type-body ;
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

### 3.1 Effect System

Every function type includes an effect annotation: `T -> <effects> U`.

```
<>                  -- pure
<io>                -- I/O
<exn[E]>            -- may raise E
```

Built-in effects: `io` (not handleable), `exn[E]`. User-defined effects via `effect Name { op : T -> U }`.

**Handlers** interpret effects:

```
handle computation {
  return(x) => ...,
  op(v, resume, k) => ... k(result) ...
}
```

Continuations are one-shot: `k` may be called 0 or 1 times. A second call raises error E211.

The type checker collects effects from expressions and warns on undeclared effects, but does not perform full row-polymorphic unification or effect subtyping. Effect annotations are checked as flat sets.

### 3.2 Records

Records are structural key-value maps:

```
{name: "Alice", age: 30}    -- record literal
rec.name                     -- field access
rec{age: 31}                 -- record update (new record with field replaced)
```

Operations: `rec.field` (access), `rec{field: val}` (update), `{f: v, ..base}` (spread). Records are structurally typed. The type checker does not enforce row polymorphism — record types are not open or closed, and there is no row-variable unification.

### 3.3 Type Inference

The type checker performs basic Hindley-Milner-style inference:

- Literal types (Int, Rat, Bool, Str, Unit)
- Variable lookup and scoping
- Let-binding with type propagation
- If-then-else branch unification
- Lambda and function application
- Match expressions with pattern binding
- List and tuple construction
- Exhaustiveness checking for match (variant registry)

The checker enforces: refinement predicates (QF_LIA micro-solver with path condition tracking), affine use-at-most-once (move/borrow/clone tracking via `affineCtx`, errors E600/E601/W600), and interface constraints (impl registry, `where`-clause validation, derived-impl verification). The checker does **not** currently enforce: full effect row unification — effect annotations are checked as flat sets; row-variable unification is not performed.

## 4. Module System

```
mod math.stats
use std.io (print)
use std.list (map, filter)

pub mean : (xs: [Rat]) -> <> Rat = ...
```

- **1:1 file mapping**: `src/math/stats.clk` → `mod math.stats`
- **Explicit imports**: every name listed, no globs
- **Visibility**: private by default, `pub` to export
- **Public functions must have explicit effect annotations**

## 5. Built-in Functions

Core operations are available without imports. These are the actually implemented builtins:

### Arithmetic
`add`, `sub`, `mul`, `div`, `mod`, `negate`

### Comparison
`eq`, `neq`, `lt`, `gt`, `lte`, `gte`

### Logic
`and`, `or`, `not`

### Strings
`str.cat` (concatenation), `show` (value to string), `split`, `trim`, `join`, `print` (I/O)

### Lists
`len`, `head`, `tail`, `cons`, `cat`, `rev`, `get`, `map`, `filter`, `fold`, `flat-map`, `range`, `zip`

### Tuples
`fst`, `snd`, `tuple.get`

### Effects
`raise` (trigger `exn` effect)

The full standard library catalog (std.str, std.json, std.fs, std.http, std.proc, etc.) described in `docs/stdlib-catalog.md` is not yet implemented. See ROADMAP.md.

## 6. Example

```
mod app.main

use std.io (print)

factorial : (n: Int) -> <> Int =
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

greet : (rec: {name: Str}) -> <io> () =
  rec.name |> str.cat("Hello, ") |> print

main : () -> <io> () =
  do {
    print(show(factorial(10)))
    greet({name: "Agent"})
  }
```
