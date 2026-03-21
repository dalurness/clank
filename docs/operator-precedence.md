# Operator Precedence and Minor Syntax Gaps — SPEC

**Task:** TASK-019
**Status:** Complete
**Applies to:** `plan/features/core-syntax.md` §4 (Grammar) and §5 (Operators)

---

## 1. Formal Operator Precedence Table

Precedence levels from lowest (loosest binding) to highest (tightest binding).

| Level | Operator(s)                        | Associativity | Category             |
|-------|------------------------------------|---------------|----------------------|
| 1     | `\|>`                              | left          | Pipeline             |
| 2     | `\|\|`                             | left          | Logical OR           |
| 3     | `&&`                               | left          | Logical AND          |
| 4     | `==` `!=` `<` `>` `<=` `>=`       | none          | Comparison           |
| 5     | `++`                               | right         | String concatenation |
| 6     | `+` `-`                            | left          | Additive             |
| 7     | `*` `/` `%`                        | left          | Multiplicative       |
| 8     | `-` `!` (prefix)                   | prefix        | Unary                |
| 9     | `f(x)` (postfix)                   | left          | Function application |
| 10    | `.field` (postfix)                 | left          | Field access         |

### Design rationale

**Pipeline at level 1 (lowest):** The informal comment in v0.2 placed `|>` between
unary and call (very high). This spec moves it to lowest precedence, matching F#
and Elixir convention. This means `a + 1 |> f` parses as `(a + 1) |> f` = `f(a + 1)`,
which is the intuitive reading. With high precedence, it would parse as
`a + (1 |> f)` = `a + f(1)`, requiring explicit parentheses for the common case.
All existing examples in v0.2 use `|>` with simple atoms (e.g., `path |> read |> split(" ")`)
so no existing code is affected by this change.

**Comparison is non-associative:** `a < b < c` is a parse error. This prevents
ambiguity — does it mean `(a < b) < c` (comparing a Bool to an Int) or mathematical
chaining? Non-associative forces the programmer to be explicit.

**String concat `++` is right-associative:** Right-associativity is conventional for
list/string concatenation (cf. Haskell `++`). It avoids O(n²) behavior in
left-associative string building. Level 5 (between comparison and additive) means
`a ++ b == c ++ d` parses as `(a ++ b) == (c ++ d)` — the expected reading.

**All operators desugar to function calls** (per v0.2 §5): `a + b` → `add(a, b)`,
`a ++ b` → `str.cat(a, b)`, `a |> f` → `f(a)`, `a |> f(b)` → `f(a, b)`.

---

## 2. Updated Grammar Productions

Replace the existing `pipeline-expr`, `apply-expr`, and operator-related comments
in §4 with these explicit precedence-climbing productions:

```ebnf
(* ── Expressions ── *)
expr        = let-expr
            | if-expr
            | match-expr
            | do-block
            | handle-expr
            | pipe-expr ;

(* ── Binary operators (precedence climbing) ── *)
pipe-expr   = or-expr { '|>' or-expr } ;               (* level 1, left *)
or-expr     = and-expr { '||' and-expr } ;              (* level 2, left *)
and-expr    = cmp-expr { '&&' cmp-expr } ;              (* level 3, left *)
cmp-expr    = concat-expr [ cmp-op concat-expr ] ;      (* level 4, none *)
concat-expr = add-expr { '++' concat-expr } ;           (* level 5, right *)
add-expr    = mul-expr { ('+' | '-') mul-expr } ;       (* level 6, left *)
mul-expr    = unary-expr { ('*' | '/' | '%') unary-expr } ; (* level 7, left *)

(* ── Unary ── *)
unary-expr  = [ '-' | '!' ] postfix-expr ;              (* level 8, prefix *)

(* ── Postfix (call and field access) ── *)
postfix-expr = atom { '(' args ')' | '.' ident } ;     (* levels 9-10, left *)

(* ── Atoms ── *)
atom        = literal
            | ident
            | lambda
            | '(' expr ')'
            | '(' expr { ',' expr } ')'       (* tuple *)
            | '[' [ expr { ',' expr } ] ']'   (* list literal *)
            | '{' [ field-init { ',' field-init } ] '}' ; (* record literal *)

(* ── Operator terminals ── *)
cmp-op      = '==' | '!=' | '<' | '>' | '<=' | '>=' ;
bin-op      = '+' | '-' | '*' | '/' | '%'
            | '==' | '!=' | '<' | '>' | '<=' | '>='
            | '&&' | '||'
            | '++'
            | '|>' ;
unary-op    = '-' | '!' ;
```

### Changes from v0.2

1. **`pipeline-expr` → `pipe-expr`**: Now the lowest-precedence binary production
   instead of sitting above `apply-expr`.

2. **`apply-expr` removed**: Split into `postfix-expr` (call + field access) and
   `unary-expr`. This eliminates the previous ambiguity where `apply-expr` mixed
   function calls and unary operators at the same level.

3. **`postfix-expr` introduced**: Combines function call `f(x)` and field access
   `.field` as postfix operations on atoms. This fixes the left-recursion in the
   old `atom '.' ident` rule (where `atom` referenced itself). Now `a.b.c` and
   `a.b(x).c` parse unambiguously via iteration.

4. **`++` added to `bin-op`**: Was missing from the production despite being used
   in examples (§8.9: `"Copied " ++ src ++ " to " ++ dst`).

5. **Explicit precedence productions**: Replaces the comment "Omitted from EBNF
   for brevity; standard infix precedence applies" with actual grammar rules.

---

## 3. Field Access (`.field`) Grammar

Field access is now handled by `postfix-expr`:

```ebnf
postfix-expr = atom { '(' args ')' | '.' ident } ;
```

This supports:
- Simple field access: `record.name` → parses as `atom . ident`
- Chained field access: `record.address.city` → `(record . address) . city`
- Method-style calls: `record.field(x)` → `(record . field) (x)`
- Mixed chains: `obj.items(0).name` → `((obj . items) (0)) . name`

Field access desugars to a function call: `record.field` → `get-field(record, :field)`,
where `:field` is a compile-time symbol (not a runtime value). The exact desugaring
mechanism is an implementation detail — the syntax is what matters here.

---

## 4. Pipeline Interaction with Other Operators

### Parsing behavior

Since `|>` has the lowest precedence among operators, any expression to its left
or right is fully evaluated before the pipe applies:

```
a + b |> f          →  f(a + b)           -- arithmetic binds tighter
!flag |> f          →  f(!flag)           -- unary binds tighter
a.b |> f            →  f(a.b)            -- field access binds tighter
a |> f |> g         →  g(f(a))           -- left-associative chaining
a |> f(b) |> g(c)   →  g(f(a, b), c)    -- partial application insertion
```

### Pipeline argument insertion

`|>` inserts the left-hand value as the **first argument** to the right-hand call:

| Expression        | Desugars to     |
|-------------------|-----------------|
| `x \|> f`         | `f(x)`          |
| `x \|> f(y)`      | `f(x, y)`      |
| `x \|> f(y, z)`   | `f(x, y, z)`   |

The right-hand side of `|>` must be either:
- A bare identifier (zero-arg call syntax): `x |> f`
- A call expression with arguments: `x |> f(y)`

It cannot be an arbitrary expression — `x |> (a + b)` is a parse error.

### Combining pipeline with other constructs

Pipeline integrates with control flow by being lower-precedence:

```
# Pipeline feeds into let binding
let result = data |> transform |> validate

# Pipeline in if condition (parens recommended for clarity)
if (x |> is-valid) then process(x) else reject(x)

# Pipeline in match scrutinee
match input |> parse {
  Ok(v) => v
  Err(e) => handle(e)
}
```

### What pipelines should NOT be used for

Pipelines are for linear data transformation chains. Do not use them when:
- The function takes multiple piped values (use named locals instead)
- The chain exceeds 5-6 steps (use named locals for readability)
- Side effects need sequencing with bindings (use `do` blocks)

---

## 5. String Concatenation `++` — Built-in Function

Add to §5 (String operations):

| Function   | Signature                          | Description          |
|------------|------------------------------------|----------------------|
| `str.cat`  | `(Str, Str) -> <> Str`             | Concatenate strings  |

Infix `++` desugars to `str.cat`. `++` is right-associative (level 5).

---

## 6. Updated Informal Comment

Replace the comment on line 154 of core-syntax.md:

```
(* Precedence low → high: |>, ||, &&, ==|!=|<|>|<=|>=, ++, +|-, *|/|%, unary -, !, call, . *)
(* Comparison operators are non-associative; ++ is right-associative; all others left *)
(* All operators desugar to function calls; no overloading *)
```
