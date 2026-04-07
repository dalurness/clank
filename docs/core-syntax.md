# Clank Core Language Specification v0.2

**Supersedes:** v0.1 (concatenative-primary)
**Authority:** DECISION-001 — applicative-primary with concatenative pipeline sugar
**Spec target:** ≤100 production rules, ≤5000 tokens

---

## 1. Execution Model

Clank is an **applicative-primary** language. Programs are built from named
functions applied to explicit arguments. Pipeline syntax (`|>`) provides
left-to-right composition sugar for data transformation chains.

**Function application**: The default. Functions take named, typed parameters
and return typed results. `f(x, y)` calls `f` with arguments `x` and `y`.

**Pipeline operator** (`|>`): Sugar for chaining single-argument transforms.
`x |> f` is `f(x)`. `x |> f(y)` is `f(x, y)` (inserts the piped value as the
first argument). Pipelines are the idiomatic way to express data transformation:

```
input |> read |> split(" ") |> len |> show |> print
```

**Named locals**: The standard way to manage intermediate values. No implicit
stack — all values are named or piped.

```
let contents = read(path)
let words = split(contents, " ")
print(show(len(words)))
```

**Where concatenative would differ**: Pure concatenative uses an implicit stack
with juxtaposition as composition. Clank rejected this as primary because LLMs
struggle with implicit state (stack simulation), not with variable binding.

**Design principle**: Types do the heavy lifting, not absence of variable
binding. Named variables with types constrain the LLM's generation search space
just as effectively as removing binding, while remaining readable.

---

## 2. Lexical Structure

```
program     = { definition | expr } ;
token       = ident | literal | operator | delimiter | keyword ;
ident       = alpha { alpha | digit | '_' | '-' } ;
alpha       = 'a'..'z' | 'A'..'Z' ;
digit       = '0'..'9' ;
whitespace  = ' ' | '\t' | '\n' ;
comment     = '#' { any - '\n' } '\n' ;
```

All tokens are ASCII. Whitespace separates tokens. No semicolons.

### Keywords

```
let  in  fn  if  then  else  match  do  type  effect
affine  handle  resume  mod  use  pub  clone  true  false
```

---

## 3. Primitive Types

| Type    | Description              | Literal examples       |
|---------|--------------------------|------------------------|
| `Int`   | 64-bit signed integer (overflow traps) | `0` `42` `-7`         |
| `Rat`   | IEEE 754 double (overflow traps) | `3.14` `-0.5`         |
| `Bool`  | Boolean                  | `true` `false`        |
| `Str`   | UTF-8 string             | `"hello"` `""`        |
| `Byte`  | 8-bit unsigned           | `0x1F` `0b1010`       |
| `()`    | Unit (empty tuple)       | `()`                  |

Compound types:

| Form         | Description                        |
|--------------|------------------------------------|
| `[T]`        | List of T                          |
| `(T, U)`     | Tuple                              |
| `{k: T}`     | Record with field k of type T      |
| `T \| U`     | Tagged union (sum type)            |
| `T -> E U`   | Function: T to U with effects E    |

Refinement and effect annotations are part of the type system spec (TASK-005,
TASK-010). The core syntax provides these hooks:

```
type-expr   = base-type [ '{' predicate '}' ] ;
effect-ann  = '<' effect { ',' effect } '>' | '<>' ;
fn-type     = type-expr '->' effect-ann type-expr ;
```

---

## 4. Grammar (Core Productions)

```ebnf
program     = { top-level } ;

top-level   = definition
            | type-decl
            | effect-decl ;

(* ── Definitions ── *)
definition  = ident ':' type-sig '=' body ;
type-sig    = '(' params ')' '->' effect-ann type-expr ;
params      = param { ',' param } | ε ;
param       = ident ':' type-expr ;
body        = expr ;

(* ── Type declarations ── *)
type-decl   = 'type' ident [ type-params ] '=' type-body
            | 'affine' 'type' ident [ type-params ] '=' type-body ;
type-params = '<' ident { ',' ident } '>' ;
type-body   = type-expr
            | variant { '|' variant } ;
variant     = ident [ '(' type-expr { ',' type-expr } ')' ] ;

(* ── Effect declarations ── *)
effect-decl = 'effect' ident '{' { op-sig } '}' ;
op-sig      = ident ':' type-sig ;

(* ── Expressions ── *)
expr        = let-expr
            | if-expr
            | match-expr
            | block-expr
            | handle-expr
            | pipeline-expr ;

pipeline-expr = apply-expr { '|>' apply-expr } ;

apply-expr  = atom [ '(' args ')' ]      (* function call *)
            | unary-op atom ;
args        = expr { ',' expr } ;

atom        = literal
            | ident
            | lambda
            | '(' expr ')'
            | '(' expr { ',' expr } ')'  (* tuple *)
            | '[' [ expr { ',' expr } ] ']' (* list literal *)
            | '{' [ field-init { ',' field-init } ] '}' (* record literal *)
            | atom '.' ident ;            (* field access *)

field-init  = ident ':' expr ;

(* ── Operators ── *)
(* Precedence low → high: ||, &&, ==|!=|<|>|<=|>=, +|-, *|/|%, unary, |>, call, . *)
(* Operators desugar to function calls; no overloading *)

unary-op    = '-' | '!' ;
bin-op      = '+' | '-' | '*' | '/' | '%'
            | '==' | '!=' | '<' | '>' | '<=' | '>='
            | '&&' | '||' ;

(* Full expression with binary operators (precedence climbing) *)
(* Omitted from EBNF for brevity; standard infix precedence applies *)

(* ── Let binding ── *)
let-expr    = 'let' ident '=' expr 'in' expr
            | 'let' ident '=' expr ;     (* top-level of body; rest follows *)

(* ── Conditional ── *)
if-expr     = 'if' expr 'then' expr 'else' expr ;

(* ── Pattern matching ── *)
match-expr  = 'match' expr '{' { match-arm } '}' ;
match-arm   = pattern '=>' expr ;
pattern     = ident                        (* variable binding *)
            | literal                      (* literal match *)
            | ident '(' pattern { ',' pattern } ')' (* variant destructure *)
            | '(' pattern { ',' pattern } ')'       (* tuple destructure *)
            | '_' ;                        (* wildcard *)

(* ── Lambda ── *)
lambda      = 'fn' '(' params ')' '=>' expr ;

(* ── Block expression: sequenced operations ── *)
block-expr  = '{' { block-step } '}' ;
block-step  = [ 'let' ident '=' ] expr ;

(* ── Effect handlers ── *)
handle-expr = 'handle' expr '{' { handler-arm } '}' ;
handler-arm = ident '(' params ')' '=>' expr ;

(* ── Literals ── *)
literal     = int-lit | rat-lit | bool-lit | str-lit | byte-lit | unit-lit ;
int-lit     = [ '-' ] digit { digit } ;
rat-lit     = [ '-' ] digit { digit } '.' digit { digit } ;
bool-lit    = 'true' | 'false' ;
str-lit     = '"' { str-char } '"' ;
byte-lit    = '0x' hex-digit hex-digit | '0b' bin-digit { bin-digit } ;
unit-lit    = '(' ')' ;

(* ── Type expressions ── *)
type-expr   = base-type
            | '[' type-expr ']'
            | '(' type-expr { ',' type-expr } ')'
            | '{' field { ',' field } '}'
            | type-expr '|' type-expr
            | type-expr '->' effect-ann type-expr
            | type-expr '{' predicate '}'
            | ident '<' type-expr { ',' type-expr } '>' ;
base-type   = 'Int' | 'Rat' | 'Bool' | 'Str' | 'Byte' | '()' | ident ;
field       = ident ':' type-expr ;

(* ── Predicates for refinement types ── *)
predicate   = pred-expr ;
pred-expr   = pred-term { ('&&' | '||') pred-term } ;
pred-term   = [ '!' ] pred-atom ;
pred-atom   = arith-expr cmp-op arith-expr
            | ident '(' arith-expr { ',' arith-expr } ')' ;
arith-expr  = arith-term { ('+' | '-') arith-term } ;
arith-term  = arith-factor { ('*' | '/' | '%') arith-factor } ;
arith-factor= int-lit | ident | 'len' | '(' arith-expr ')' ;
cmp-op      = '==' | '!=' | '<' | '>' | '<=' | '>=' ;

(* ── Effects ── *)
effect-ann  = '<' [ effect { ',' effect } ] '>' ;
effect      = ident ;
```

**Production count: ~60 rules** (well under the 100-rule target).

---

## 5. Operators and Built-in Functions

### Arithmetic

| Function | Signature                          | Description       |
|----------|------------------------------------|-------------------|
| `add`    | `(Int, Int) -> <> Int`             | Add               |
| `sub`    | `(Int, Int) -> <> Int`             | Subtract          |
| `mul`    | `(Int, Int) -> <> Int`             | Multiply          |
| `div`    | `(Int, Int{!= 0}) -> <> Int`      | Integer divide    |
| `mod`    | `(Int, Int{!= 0}) -> <> Int`      | Modulo            |
| `neg`    | `(Int) -> <> Int`                  | Negate            |

Infix operators `+`, `-`, `*`, `/`, `%` desugar to these. Overloaded for `Rat`.

### Comparison

| Function | Signature                          | Description       |
|----------|------------------------------------|-------------------|
| `eq`     | `(a, a) -> <> Bool`                | Equal             |
| `neq`    | `(a, a) -> <> Bool`                | Not equal         |
| `lt`     | `(a, a) -> <> Bool`                | Less than         |
| `gt`     | `(a, a) -> <> Bool`                | Greater than      |
| `lte`    | `(a, a) -> <> Bool`                | Less or equal     |
| `gte`    | `(a, a) -> <> Bool`                | Greater or equal  |

Infix operators `==`, `!=`, `<`, `>`, `<=`, `>=` desugar to these.

### Logic

| Function | Signature                          | Description       |
|----------|------------------------------------|-------------------|
| `and`    | `(Bool, Bool) -> <> Bool`          | Logical and       |
| `or`     | `(Bool, Bool) -> <> Bool`          | Logical or        |
| `not`    | `(Bool) -> <> Bool`                | Logical not       |

Infix `&&`, `||` and prefix `!` desugar to these.

### List operations

| Function   | Signature                                    | Description         |
|------------|----------------------------------------------|---------------------|
| `map`      | `([a], (a) -> <e> b) -> <e> [b]`             | Map function over list |
| `filter`   | `([a], (a) -> <e> Bool) -> <e> [a]`          | Filter by predicate |
| `fold`     | `([a], b, (b, a) -> <e> b) -> <e> b`         | Left fold           |
| `len`      | `([a]) -> <> Int`                             | List length         |
| `head`     | `([a]{len > 0}) -> <> a`                      | First element       |
| `tail`     | `([a]{len > 0}) -> <> [a]`                    | All but first       |
| `cons`     | `(a, [a]) -> <> [a]`                          | Prepend element     |
| `cat`      | `([a], [a]) -> <> [a]`                        | Concatenate lists   |
| `get`      | `([a], Int) -> <> Option<a>`                  | Element at index    |
| `rev`      | `([a]) -> <> [a]`                             | Reverse list        |

### String operations

| Function   | Signature                                    | Description         |
|------------|----------------------------------------------|---------------------|
| `split`    | `(Str, Str) -> <> [Str]`                     | Split on delimiter  |
| `join`     | `([Str], Str) -> <> Str`                      | Join with delimiter |
| `trim`     | `(Str) -> <> Str`                             | Trim whitespace     |
| `show`     | `(a) -> <> Str`                               | Convert to string   |

### I/O (effectful)

| Function   | Signature                                    | Effects     |
|------------|----------------------------------------------|-------------|
| `print`    | `(Str) -> <io> ()`                            | I/O         |
| `read`     | `(Str) -> <io, exn> Str`                      | I/O, may fail |
| `write`    | `(Str, Str) -> <io, exn> ()`                  | I/O, may fail |

---

## 6. Control Flow

### Conditional (`if`/`then`/`else`)

Standard expression-level conditional:

```
if n == 0 then 1 else n * factorial(n - 1)
```

Both branches must have the same type. `if` is an expression — it produces a value.

### Pattern matching (`match`)

Destructures a value by shape:

```
match shape {
  Circle(r) => r * r * 3.14159
  Rect(w, h) => w * h
}
```

Match arms are checked for exhaustiveness at compile time.

### Pipeline (`|>`)

Left-to-right function composition. Each `|>` passes the left-hand value as
the first argument to the right-hand function:

```
"hello world" |> split(" ") |> len
# Equivalent to: len(split("hello world", " "))
```

Pipelines are the idiomatic replacement for concatenative word sequences.
Use them for simple 2-5 step data transforms.

### Brace blocks (sequential expressions)

For sequences with named intermediates:

```
{
  let contents = read("config.toml")
  let config   = parse-toml(contents)
  config
}
```

The block evaluates each step in order and returns the value of the last expression.

### Lambda expressions

Anonymous functions for callbacks and higher-order usage:

```
map(xs, fn(x) => x * 2)
filter(names, fn(name) => len(name) > 3)
```

### Effect handlers

Handle effects locally, providing interpretations:

```
handle may-fail(x) {
  raise(e) => default-value
}
```

Full handler syntax is specified in the effect system spec (TASK-005).

---

## 7. Module System

```
mod math

use std.io (print, read)
use std.list (map, filter)

pub mean : (xs: [Int]{len > 0}) -> <> Rat =
  fold(xs, 0, add) / len(xs)
```

- `mod` declares the module name
- `use` imports specific names (no glob imports — explicit is agent-friendly)
- `pub` exports a definition
- Modules map 1:1 to files

---

## 8. Example Programs

### 8.1 Factorial

```
factorial : (n: Int{>= 0}) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)
```

### 8.2 FizzBuzz

```
use std.io (print)
use std.str (show)

fizzbuzz : (n: Int) -> <io> () =
  if n % 15 == 0 then print("FizzBuzz")
  else if n % 3 == 0 then print("Fizz")
  else if n % 5 == 0 then print("Buzz")
  else print(show(n))

main : () -> <io> () =
  map(range(1, 100), fizzbuzz)
```

### 8.3 Mean of a list (pipeline style)

```
mean : (xs: [Rat]{len > 0}) -> <> Rat =
  fold(xs, 0.0, add) / len(xs)
```

### 8.4 Safe division with effects

```
effect DivError {
  div-by-zero : () -> <exn> ()
}

safe-div : (a: Int, b: Int) -> <DivError> Int =
  if b == 0 then div-by-zero() else a / b
```

### 8.5 File word count (pipeline style)

```
use std.io (read, print)
use std.str (split, show)

word-count : (path: Str) -> <io, exn> () =
  path |> read |> split(" ") |> len |> show |> print
```

### 8.6 Zip-with (lambda + named locals)

```
zip-with : (xs: [a], ys: [b], f: (a, b) -> <e> c) -> <e> [c] =
  map(zip(xs, ys), fn(pair) => f(fst(pair), snd(pair)))
```

### 8.7 Sum type and pattern matching

```
type Shape
  = Circle(Rat)
  | Rect(Rat, Rat)

area : (s: Shape) -> <> Rat =
  match s {
    Circle(r) => r * r * 3.14159
    Rect(w, h) => w * h
  }
```

### 8.8 Pipeline with lambda

```
use std.list (map, filter)
use std.str (show)

evens-as-strings : (xs: [Int]) -> <> [Str] =
  xs |> filter(fn(x) => x % 2 == 0) |> map(show)
```

### 8.9 Effectful pipeline with block

```
use std.io (read, write, print)

copy-file : (src: Str, dst: Str) -> <io, exn> () = {
  let contents = read(src)
  write(dst, contents)
  print("Copied " ++ src ++ " to " ++ dst)
}
```

---

## 9. Design Notes

### Applicative-primary rationale (DECISION-001)

The v0.1 spec used concatenative (stack-based) as the primary execution model.
DECISION-001 reversed this: applicative is primary, concatenative is sugar.

**Why**: LLMs struggle with *implicit state* — to understand what's available at
step N of a concatenative program, you must trace every prior step (stack
simulation). Named variables with types are self-documenting: the LLM can read
what a value is and what type it has without simulating execution.

**Pipeline as the best of both**: `x |> f |> g |> h` reads left-to-right like
concatenative `x f g h`, but each step is an explicit function call. No implicit
stack, no ambiguity about arity, no stack shuffling combinators.

### What was removed from v0.1

- **Implicit stack**: No stack as execution model; no stack effects notation
- **Stack manipulation words**: `dup`, `drop`, `swap`, `rot`, `over` removed
- **Quotations**: `[expr]` as deferred code removed; lambdas (`fn(x) => expr`)
  replace them with explicit parameters
- **Quotation-based `if`**: `bool [then] [else] if` replaced with
  `if cond then expr else expr`
- **Quotation-based `apply`**: Removed; use direct function application

### What was added

- **Pipeline operator** (`|>`): Left-to-right composition sugar
- **Lambda expressions** (`fn`): Named-parameter anonymous functions
- **Infix operators**: `+`, `-`, `*`, `/`, `%`, `==`, `!=`, `<`, `>`, `<=`,
  `>=`, `&&`, `||` with standard precedence
- **`if`/`then`/`else`**: Standard conditional expression syntax
- **Field access** (`.`): `record.field` syntax
- **List literals**: `[1, 2, 3]`
- **Record literals**: `{name: "Ada", age: 36}`
- **String concatenation**: `++` operator

### What was kept

- **Function signatures**: Named, typed parameters at definition boundaries
- **Pattern matching**: `match` with exhaustiveness checking
- **Brace blocks**: `{ let x = expr  ... }` for sequential expressions
- **Effect annotations**: `<io, exn>` on function types
- **Refinement type hooks**: `Int{>= 0}`, `[T]{len > 0}`
- **Module system**: `mod`, `use`, `pub`
- **Type declarations**: `type Shape = Circle(Rat) | Rect(Rat, Rat)`
- **Effect declarations**: `effect Name { ... }`

### Token efficiency

Compared to Python equivalents, Clank targets 40-60% fewer tokens:

```
# Python: mean (11 tokens)
def mean(xs: list[int]) -> float:
    assert len(xs) > 0
    return sum(xs) / len(xs)

# Clank: mean (applicative, stronger guarantees)
mean : (xs: [Int]{len > 0}) -> <> Rat =
  fold(xs, 0, add) / len(xs)
```

The refinement `{len > 0}` replaces the runtime assert with a compile-time
guarantee — fewer tokens AND stronger safety.

Pipeline style for transforms:

```
# Python
def word_count(path: str) -> None:
    words = open(path).read().split(" ")
    print(str(len(words)))

# Clank (pipeline)
word-count : (path: Str) -> <io, exn> () =
  path |> read |> split(" ") |> len |> show |> print
```

### No operator overloading

Each word has exactly one meaning regardless of context. `+` is always
arithmetic addition (overloaded for numeric types only). List concatenation is
`cat`. String concatenation is `++`. This eliminates ambiguity for LLM code
generation.
