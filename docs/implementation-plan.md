# Phase 3 Implementation Plan: Clank Reference Interpreter

**Task:** TASK-023
**Mode:** Research / Spec
**Date:** 2026-03-14
**Status:** Complete
**Builds On:** core-syntax.md, compilation-strategy.md, vm-instruction-set.md, gc-strategy.md

---

## 1. Implementation Language Recommendation

**Recommendation: TypeScript**

### Rationale

The hard constraint from TASK-006 is that the toolchain must fit in agent context (~1500 lines). This eliminates Rust (too verbose for the budget) and makes Go awkward (no sum types for AST representation). TypeScript wins on every criterion that matters for Phase 1:

| Criterion | TypeScript | Rust | Go |
|-----------|-----------|------|-----|
| Lines for tree-walking interpreter | ~800–1200 | ~1800–2500 | ~1400–2000 |
| Sum types for AST | Discriminated unions (native) | `enum` (native, verbose) | Interface + type switch (awkward) |
| Pattern matching | `switch` on discriminant | `match` (excellent but verbose) | Type switch (weak) |
| Agent familiarity | Highest (LLMs know TS best) | High | Medium |
| Runtime availability | Node.js (ubiquitous) | Compile step required | Compile step required |
| Fits in 1500 lines? | Yes, comfortably | Tight | Tight |
| Prototype speed | Fast | Slow (fighting borrow checker) | Medium |

**Why not Rust for Phase 1?** Rust is more verbose for AST-heavy code — a struct definition + impl block for a single AST node takes ~15 lines vs ~3 in TypeScript. For the reference interpreter whose purpose is to validate the language design and run example programs, TypeScript's conciseness is decisive.

**Why not write Clank in Clank?** Self-hosting is a Phase 5+ goal. The language doesn't exist yet.

**Migration path:** TypeScript reference interpreter → [research required: static binary target] → self-hosted compiler (future). When the reference implementation is stable, a dedicated research task should evaluate the best path to a distributable static binary. That decision should be driven by Clank's own design goals — toolchain context-fit, agent writability of the toolchain source, execution performance where it matters, and deployment simplicity. The answer should not be assumed in advance.

---

## 2. Architecture

### Two-Track Strategy

The specs define two execution paths. We implement both, in order:

1. **Tree-walking interpreter** — AST-level evaluation, no bytecode. Purpose: validate the language design, run example programs, iterate on semantics quickly.
2. **Bytecode compiler + VM** — Compile AST to the 87-opcode VM from vm-instruction-set.md. Purpose: production execution, performance, eventual WASM target.

Track 1 is the focus of this plan. Track 2 reuses the lexer, parser, and type checker from Track 1.

### Pipeline

```
Source → Lexer → Tokens → Parser → AST → Desugar → Core AST → Type Check → Typed AST → Evaluate
                                                                                        ↓
                                                                          (Track 2: Compile → Bytecode → VM)
```

### Module Breakdown

```
src/
  lexer.ts        — tokenizer (~150 lines)
  parser.ts       — recursive descent parser (~350 lines)
  ast.ts          — AST type definitions (~100 lines)
  desugar.ts      — pipeline/operator/do-block desugaring (~80 lines)
  types.ts        — type representations (~60 lines)
  checker.ts      — type checker (~250 lines, Phase 2+ of implementation)
  eval.ts         — tree-walking evaluator (~300 lines)
  builtins.ts     — built-in functions (~100 lines)
  main.ts         — CLI entry point (~30 lines)
```

**Estimated total: ~1420 lines** (within the 1500-line budget).

The type checker is the most complex component and can be deferred — the interpreter runs without it for the initial phases.

---

## 3. Minimal Viable Subset (MVS)

The MVS is the smallest language subset that can run the example programs from core-syntax.md §8. This determines what to implement first.

### Example Coverage Analysis

| Example | Features Required |
|---------|-------------------|
| 8.1 Factorial | `fn` def, `if`/`then`/`else`, recursion, `Int`, arithmetic, comparison |
| 8.2 FizzBuzz | `fn` def, `if`/`then`/`else`, `%` operator, `print`, string literals, `use` |
| 8.3 Mean | `fn` def, `fold`, `len`, `Rat`, arithmetic, list param |
| 8.4 Safe division | `effect` decl, effect handler, `if`/`then`/`else` |
| 8.5 Word count | Pipeline (`\|>`), `read`, `split`, `len`, `show`, `print` |
| 8.6 Zip-with | Lambda, higher-order functions, generics |
| 8.7 Sum type + match | `type` decl, `match`, pattern destructuring |
| 8.8 Pipeline + lambda | Pipeline, `filter`, `map`, lambda, `show` |
| 8.9 Do-block | `do` block, `<-` binder, string concat (`++`) |

### MVS Feature Set (Phase 1 target)

**Included:**
- Primitive types: `Int`, `Rat`, `Bool`, `Str`, `()`
- Compound types: lists `[T]`, tuples `(T, U)`
- Literals: integer, rational, boolean, string, unit, list, tuple
- `let` bindings (both forms)
- `if`/`then`/`else`
- Function definitions with type signatures
- Function application
- Pipeline operator (`|>`)
- Infix operators: `+`, `-`, `*`, `/`, `%`, `==`, `!=`, `<`, `>`, `<=`, `>=`, `&&`, `||`, `++`
- Unary operators: `-`, `!`
- Lambda expressions (`fn(x) => expr`)
- Recursion (including mutual recursion)
- `type` declarations (sum types / variants)
- `match` expressions with pattern destructuring
- `do` blocks
- Built-in functions: `print`, `show`, `len`, `map`, `filter`, `fold`, `head`, `tail`, `cons`, `cat`, `split`, `join`, `trim`

**Deferred to later phases:**
- Effect declarations and handlers (complex, can run without them initially)
- Refinement type predicates (`Int{>= 0}`)
- Affine type enforcement (`affine type`, `AFFINE_MOVE`, `clone`)
- Module system (`mod`, `use`, `pub`)
- Record types and field access
- Generic type parameters
- Type inference (explicit types only in MVS)

### MVS Example Target

With the MVS, we can run examples 8.1, 8.2 (without `use`), 8.3, 8.5 (without `use`), 8.7, 8.8 (without `use`), and 8.9 (without `use`). Effect-related examples (8.4) come in Phase 3.

---

## 4. Phased Implementation Plan

### Phase 1: Core Interpreter (run factorial)

**Goal:** Parse and evaluate the simplest programs — arithmetic, let bindings, conditionals, recursion.

**Tasks:**

| Task | Description | Est. Lines | Depends On |
|------|-------------|-----------|------------|
| IMPL-001 | **AST type definitions** — Define discriminated union types for all AST nodes (expressions, types, patterns, top-level forms) | ~100 | — |
| IMPL-002 | **Lexer** — Tokenize Clank source: keywords, identifiers, literals, operators, delimiters. ASCII-only, `#` comments, no semicolons. | ~150 | IMPL-001 |
| IMPL-003 | **Parser (core)** — Recursive descent parser for: literals, let bindings, if/then/else, function definitions, function application, infix operators (precedence climbing per operator-precedence.md), parenthesized expressions | ~200 | IMPL-001, IMPL-002 |
| IMPL-004 | **Evaluator (core)** — Tree-walking eval: literal values, let bindings, if/then/else, function calls (with environment/closure model), arithmetic/comparison/logic builtins, recursion | ~150 | IMPL-001, IMPL-003 |
| IMPL-005 | **CLI entry point** — Read file, lex, parse, evaluate, print result. Structured JSON error output on failure. | ~30 | IMPL-002, IMPL-003, IMPL-004 |

**Milestone:** Run `factorial(5)` → `120`.

**Estimated total:** ~630 lines.

### Phase 2: Data Structures + Pipeline + Lambda (run most examples)

**Goal:** Add the data types, pipeline sugar, lambdas, and pattern matching needed for idiomatic Clank.

**Tasks:**

| Task | Description | Est. Lines | Depends On |
|------|-------------|-----------|------------|
| IMPL-006 | **Parser: pipeline + lambda** — Add `\|>` parsing, lambda expression parsing (`fn(x) => expr`), list/tuple literals | ~60 | IMPL-003 |
| IMPL-007 | **Desugarer** — AST-to-AST pass: pipeline elimination (`x \|> f(y)` → `f(x, y)`), operator desugaring (`a + b` → `add(a, b)`), do-block flattening, unary ops, string concat | ~80 | IMPL-001 |
| IMPL-008 | **Evaluator: compound values** — Lists, tuples, list operations (`map`, `filter`, `fold`, `len`, `head`, `tail`, `cons`, `cat`, `rev`, `get`), string operations (`split`, `join`, `trim`, `show`), `print` | ~100 | IMPL-004 |
| IMPL-009 | **Type declarations + match** — Parse `type Shape = Circle(Rat) | Rect(Rat, Rat)`, evaluate variant construction, match expression with pattern destructuring (variable binding, literal match, variant destructure, tuple destructure, wildcard) | ~100 | IMPL-003, IMPL-004 |
| IMPL-010 | **Do-block evaluation** — Parse and evaluate do-blocks (desugared to let-bindings by IMPL-007) | ~20 | IMPL-007, IMPL-004 |

**Milestone:** Run examples 8.1–8.3, 8.5, 8.7–8.9 from core-syntax.md.

**Estimated total (cumulative):** ~990 lines.

### Phase 3: Effects + Handlers (run all examples)

**Goal:** Implement the algebraic effect system — effect declarations, perform, handle, resume.

**Tasks:**

| Task | Description | Est. Lines | Depends On |
|------|-------------|-----------|------------|
| IMPL-011 | **Effect declarations** — Parse `effect Name { op : sig }`, store in environment | ~30 | IMPL-003 |
| IMPL-012 | **Effect handler evaluation** — Implement `handle expr { op(params) => body }` using delimited continuations. Handler installation, effect dispatch (walk handler stack), continuation capture, resume. | ~120 | IMPL-004, IMPL-011 |
| IMPL-013 | **Built-in effects** — Wire `io` (print, read), `exn` (raise) as built-in effect handlers installed at program start | ~40 | IMPL-012 |

**Milestone:** Run example 8.4 (safe division with effects). All core-syntax.md examples pass.

**Estimated total (cumulative):** ~1180 lines.

### Phase 4: Type Checker (optional for interpreter, required for compiler)

**Goal:** Validate programs before evaluation. Catch type errors, arity mismatches, exhaustiveness failures.

**Tasks:**

| Task | Description | Est. Lines | Depends On |
|------|-------------|-----------|------------|
| IMPL-014 | **Type representations** — Types as a data structure: primitives, function types, list/tuple types, union types, type variables, effect annotations | ~60 | IMPL-001 |
| IMPL-015 | **Type checker (core)** — Bidirectional type checking: check literals, infer let bindings, check function signatures, check if/then/else branch consistency, check function application arity + types | ~150 | IMPL-014 |
| IMPL-016 | **Match exhaustiveness** — Check that match arms cover all variants of a sum type | ~40 | IMPL-015, IMPL-009 |
| IMPL-017 | **Effect type checking** — Track effect annotations on function types, verify effect handlers discharge their effects | ~60 | IMPL-015, IMPL-012 |

**Milestone:** Type errors reported as structured JSON before evaluation begins.

**Estimated total (cumulative):** ~1490 lines.

### Phase 5: Module System + Records

**Goal:** Support multi-file programs and record types.

**Tasks:**

| Task | Description | Est. Lines | Depends On |
|------|-------------|-----------|------------|
| IMPL-018 | **Module system** — Parse `mod`, `use`, `pub`. File-based module resolution. Import resolution (load, parse, merge environments). | ~80 | IMPL-003, IMPL-004 |
| IMPL-019 | **Record types** — Parse record literals `{k: v}`, record type declarations, field access (`r.field`), functional update | ~60 | IMPL-003, IMPL-004 |

**Milestone:** Multi-file Clank programs with imports.

**Note:** This phase exceeds the 1500-line budget. Either (a) accept a ~1630-line total for the feature-complete interpreter, or (b) defer modules/records to the compiler track.

### Phase 6: Bytecode Compiler + VM (Track 2)

**Goal:** Compile Typed AST to the 87-opcode VM bytecode. Implement the VM runtime with GC.

This phase reuses IMPL-001 through IMPL-017 (lexer, parser, type checker) and replaces the evaluator with a compiler + VM.

**Tasks:**

| Task | Description | Est. Lines | Depends On |
|------|-------------|-----------|------------|
| IMPL-020 | **Code generator** — Walk Typed AST, emit bytecode per compilation-strategy.md §3. Local slot allocator, word ID allocator, string table. | ~300 | IMPL-007, IMPL-014 |
| IMPL-021 | **Binary format writer** — Emit the CLNK module binary format (header, string table, word table, bytecode section) per vm-instruction-set.md §3 | ~80 | IMPL-020 |
| IMPL-022 | **VM runtime** — Fetch-decode-execute loop for 87 opcodes. Data stack, return stack, call frames, local variables. | ~400 | IMPL-021 |
| IMPL-023 | **GC implementation** — Mark-sweep with bump+free-list allocator per gc-strategy.md §3. Root enumeration, mark phase, sweep phase. | ~380 | IMPL-022 |
| IMPL-024 | **Effect handler runtime** — Handler stack, continuation capture/restore, resume semantics in VM | ~150 | IMPL-022, IMPL-023 |

**Milestone:** Compile and run all example programs via bytecode VM.

**Estimated compiler + VM total:** ~1310 lines (separate from interpreter).

---

## 5. Implementation Order Summary

```
Phase 1: AST → Lexer → Parser (core) → Evaluator (core) → CLI
          ↓ can run: factorial, basic arithmetic
Phase 2: Pipeline + Lambda → Desugarer → Compound Values → Match → Do-blocks
          ↓ can run: most examples (8.1-8.3, 8.5, 8.7-8.9)
Phase 3: Effects → Handlers → Built-in effects
          ↓ can run: all examples
Phase 4: Type representations → Type checker → Exhaustiveness → Effect checking
          ↓ type errors caught before execution
Phase 5: Modules → Records
          ↓ multi-file programs
Phase 6: Code generator → Binary format → VM → GC → Effect runtime
          ↓ production execution
```

Phases 1–3 are the critical path to a working interpreter. Phase 4 can be developed in parallel with Phase 3 (type checker is independent of effect evaluation). Phase 6 is a separate track that can begin after Phase 4.

---

## 6. Key Design Decisions

### Tree-walking before VM

The tree-walking interpreter is faster to implement (~3 tasks to first program) and easier to debug. It validates the language semantics without the complexity of bytecode generation. The VM is a separate track that builds on the same frontend.

### Desugar pass as separate phase

Pipeline (`|>`), operators (`+`, `-`), do-blocks, and string concat (`++`) are all syntactic sugar. Desugaring them to core AST (function application + let bindings) before evaluation means the evaluator only handles ~7 expression forms: literal, variable, let, if, apply, lambda, match. This keeps the evaluator simple and correct.

### Environment model for closures

The tree-walking interpreter uses a persistent (immutable) environment model:
- Each scope is a map from name → value, linked to its parent scope
- Function closures capture their defining environment
- `let` extends the current environment
- Function application creates a new environment with parameters bound

This naturally handles closures, recursion (via self-reference), and lexical scoping.

### Effect handlers via delimited continuations

For the tree-walking interpreter, effect handlers are implemented using JavaScript's built-in exception mechanism + continuation-passing:
- `handle expr { ... }` installs a handler on a handler stack
- `EFFECT_PERFORM` throws a special exception caught by the nearest matching handler
- One-shot continuations are callbacks; multi-shot continuations require CPS transform

For Phase 1, one-shot continuations (the common case) suffice. Multi-shot can be deferred.

### Structured JSON errors throughout

Every error (lex, parse, type, runtime) produces structured JSON matching the VM spec's trap format:
```json
{"code": "E002", "message": "type mismatch", "location": {"line": 5, "col": 12}, "context": "..."}
```
This is a core Clank design principle (agent-native tooling).

---

## 7. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Type checker exceeds line budget | Medium | Medium | Defer advanced type features (refinements, generics) to Phase 3b |
| Effect handlers too complex for tree-walker | Low | High | Start with one-shot continuations only; CPS transform is well-understood |
| 1500-line budget too tight | Medium | Low | Budget is a guideline, not a hard wall; 1600–1700 is acceptable |
| Parser complexity from precedence climbing | Low | Low | Well-documented algorithm; operator-precedence.md already specifies the 9-level table |
| GC in TypeScript (for VM track) | Medium | Medium | TypeScript VM manages its own heap as an ArrayBuffer; host GC handles TS objects |

---

## 8. Task Dependencies (DAG)

```
IMPL-001 (AST) ─────────┬──────────────────────────────────────┐
                         │                                      │
IMPL-002 (Lexer) ───────┤                                      │
                         │                                      │
IMPL-003 (Parser) ──────┼──── IMPL-006 (Pipeline/Lambda)       │
                         │         │                            │
IMPL-004 (Eval) ────────┼─────────┼──── IMPL-008 (Compounds)   │
                         │         │         │                  │
IMPL-005 (CLI) ─────────┘         │    IMPL-009 (Match)        │
                                  │         │                   │
                    IMPL-007 (Desugar) ─────┼──── IMPL-010 (Do)│
                                            │                   │
                              IMPL-011 (Effect decl) ───────────┤
                                       │                        │
                              IMPL-012 (Effect eval) ───────────┤
                                       │                        │
                              IMPL-013 (Built-in effects)       │
                                                                │
                              IMPL-014 (Type repr) ─────────────┘
                                       │
                              IMPL-015 (Type checker)
                                       │
                              IMPL-016 (Exhaustiveness)
                              IMPL-017 (Effect types)
                                       │
                              IMPL-020 (Codegen) ── IMPL-021 (Binary)
                                                         │
                                                    IMPL-022 (VM)
                                                         │
                                                    IMPL-023 (GC)
                                                    IMPL-024 (Effect RT)
```

---

## 9. Testing Strategy

Each phase produces testable output. Tests are Clank source files with expected output:

```
# test/factorial.clank
factorial : (n: Int) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)

# Expected: 120
factorial(5)
```

Test runner: simple script that runs each `.clank` file, compares stdout to expected output.

**Test suite structure:**
- `test/phase1/` — arithmetic, let, if, recursion (~10 programs)
- `test/phase2/` — pipeline, lambda, match, lists, tuples (~10 programs)
- `test/phase3/` — effects, handlers (~5 programs)
- `test/examples/` — all 9 examples from core-syntax.md §8
- `test/errors/` — expected error cases (type mismatches, parse errors, unbound variables)

---

## 10. Conclusion

The implementation plan follows a "working software at every phase" principle. Phase 1 delivers a running interpreter in ~630 lines (4–5 implementation tasks). Each subsequent phase adds features incrementally while keeping the total within or near the 1500-line budget.

**Critical path to first running program:** IMPL-001 → IMPL-002 → IMPL-003 → IMPL-004 → IMPL-005 (5 tasks).

**Critical path to all examples running:** Add IMPL-006 through IMPL-013 (8 more tasks, 13 total).

**Implementation language:** TypeScript for the reference interpreter. The static binary strategy is a future research task — see migration path note above.
