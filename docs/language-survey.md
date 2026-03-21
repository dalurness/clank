# Language Survey for Clank Design

## Evaluation Axes

Each language is evaluated on:
1. **Syntax density** — semantic content per token
2. **Type system** — expressiveness and verifiability of contracts
3. **Compilation model** — what it targets, how incremental it is
4. **Agent-readability** — how easily an LLM can parse and understand programs
5. **Agent-writability** — how easily an LLM can generate correct programs
6. **Spec simplicity** — how small the full language specification is

Scale: ★ (poor) to ★★★★★ (excellent) for agent-optimized use.

---

## Tradition 1: Lambda Calculus and Functional Core

### Untyped Lambda Calculus

**What it is:** The theoretical foundation — three constructs only: variables, abstraction (λx.M), application (M N).

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | Three constructs. Nothing wasted. |
| Type system | ★ | None. Everything is a function. |
| Compilation model | ★★ | Reduction rules only. No native compilation story. |
| Agent-readability | ★★★ | Unambiguous but deeply nested for real programs. |
| Agent-writability | ★★★ | Easy to generate, hard to generate *correctly* for complex tasks. |
| Spec simplicity | ★★★★★ | Fits on one page. |

**Strengths for agents:** Absolute minimal core, zero ambiguity in semantics, trivial to parse. Church encoding means you *can* express anything.

**Weaknesses for agents:** Church-encoded arithmetic is wildly token-inefficient (the number 5 takes ~20 tokens). No built-in data types means real programs are impractical. Deep nesting overwhelms context windows. Variable binding/substitution is the #1 source of bugs in lambda calculus implementations.

**Takeaway for Clank:** The *philosophy* of radical minimalism is right. The execution model (substitution-based reduction) is wrong for production use. Clank should have a minimal core, but it must include primitive types and operations — Church encoding is an intellectual exercise, not a practical strategy.

---

### Simply Typed Lambda Calculus (STLC)

**What it is:** Lambda calculus + base types + function types (A → B). The smallest useful typed core.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★ | Type annotations add overhead but catch errors. |
| Type system | ★★★ | Prevents type errors but can't express contracts. |
| Compilation model | ★★ | Same as untyped — theoretical, not practical. |
| Agent-readability | ★★★★ | Types make intent explicit. |
| Agent-writability | ★★★★ | Type-guided generation is more reliable. |
| Spec simplicity | ★★★★★ | ~10 typing rules. |

**Takeaway for Clank:** STLC proves that even minimal type annotations massively improve agent reliability. The type→generation pipeline is key: if types constrain the space of valid programs, agents make fewer errors. But STLC is too weak — can't express "this integer is positive" or "this list is non-empty."

---

## Tradition 2: Stack-Based and Concatenative Languages

### Forth

**What it is:** Stack-based, concatenative. Programs are sequences of words that manipulate a shared stack. Defined in terms of a dictionary of words.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | No parentheses, no variables, no binding syntax. `3 4 + .` |
| Type system | ★ | None. Stack effects are implicit. |
| Compilation model | ★★★★★ | Compiles to threaded code. Tiny compiler. Incremental by nature. |
| Agent-readability | ★★★ | Linear flow is good. Stack mental model is hard to track deep. |
| Agent-writability | ★★★★ | No naming decisions. Just compose words. |
| Spec simplicity | ★★★★★ | Core spec is ~200 words. Self-hosting in <1000 lines. |

**Strengths for agents:** Concatenative composition eliminates variable binding entirely — an agent never has to decide what to name things or track scope. Programs compose by juxtaposition (putting words next to each other IS composition). The dictionary model is inherently incremental: define a word, test it, define the next. Tiny compiler means the entire toolchain fits in context.

**Weaknesses for agents:** No types means no compile-time error catching. Stack shuffling (DUP, SWAP, ROT, OVER) gets confusing for deep stacks — even agents lose track after 4-5 items. No higher-order abstractions without manual encoding.

**Takeaway for Clank:** The concatenative execution model is *extremely* agent-friendly. The absence of variable binding removes an entire class of agent errors (misspellings, wrong scope, shadowing). Clank should seriously consider a concatenative or point-free core. But it MUST add types — untyped stack manipulation is a debugging nightmare.

---

### Joy

**What it is:** Pure concatenative language. Like Forth but with quoted programs as first-class values and combinators instead of stack manipulation.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | Even terser than Forth. Combinators replace stack shuffling. |
| Type system | ★ | None. |
| Compilation model | ★★★ | Interpreted. Reduction-based. |
| Agent-readability | ★★★★ | Cleaner than Forth — combinators are more semantic than DUP/SWAP. |
| Agent-writability | ★★★★★ | Quotation + combinators = highly composable generation. |
| Spec simplicity | ★★★★★ | Smaller than Forth. |

**Strengths for agents:** Joy's key insight: replace stack shuffling with *combinators* that operate on quoted programs. `[dup *] map` instead of manual stack manipulation. This is more compositional AND more readable. Programs are just compositions of transformations — which is exactly how agents reason about code generation.

**Weaknesses for agents:** No types. Rarely implemented in production. Limited ecosystem.

**Takeaway for Clank:** Joy's combinator approach is the best version of concatenative programming for agents. Clank should study Joy's combinators closely. The idea of quoted programs (code as data, homoiconicity) also enables powerful metaprogramming — agents could manipulate Clank programs as Clank data structures.

---

### Factor

**What it is:** Modern concatenative language with a static type system ("stack effect declarations"), garbage collection, an optimizing compiler, and a large standard library.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★ | Concatenative core but type declarations add weight. |
| Type system | ★★★ | Stack effect declarations. Better than nothing, weaker than ML. |
| Compilation model | ★★★★ | Optimizing native compiler. Real production language. |
| Agent-readability | ★★★★ | Stack effects document what each word does to the stack. |
| Agent-writability | ★★★★ | Stack effects constrain generation. Good incremental testing. |
| Spec simplicity | ★★★ | Much bigger than Forth/Joy due to standard library. |

**Strengths for agents:** Proves concatenative can be production-grade. Stack effect declarations are exactly the kind of contract that helps agents: `( x y -- z )` tells you precisely what a word consumes and produces. The interactive development model (define, test, refine in REPL) maps well to agent workflows.

**Weaknesses for agents:** Stack effects are weaker than full types — they count elements but don't describe what the elements ARE. The standard library is large and human-oriented.

**Takeaway for Clank:** Factor proves concatenative + types + production compiler is viable. Clank should adopt Factor's stack effect notation but extend it with real types: `( x:Int y:Int -- z:Int )` or richer. Factor's interactive development story is also a template for Clank's agent-native tooling.

---

## Tradition 3: Array/Tacit Languages

### APL

**What it is:** Array-oriented, symbolic. Uses special Unicode glyphs for operations. Operates on entire arrays implicitly.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | Legendary density. Game of Life in one line. |
| Type system | ★ | Dynamic. Arrays of numbers/characters. |
| Compilation model | ★★ | Interpreted. Specialized array runtime. |
| Agent-readability | ★★ | Unicode symbols require memorization. Order of operations is right-to-left. |
| Agent-writability | ★★ | Token representation of APL glyphs is inconsistent across tokenizers. |
| Spec simplicity | ★★★ | Core is small. Extended APL is not. |

**Strengths for agents:** Extreme information density. One APL expression replaces 10-50 lines of Python. The array model is mathematically clean — operations lift over dimensions automatically. No loops, no iteration — everything is implicit mapping/reduction.

**Weaknesses for agents:** The Unicode glyphs are a serious problem for LLM tokenizers — they tokenize inconsistently and agents frequently generate wrong glyphs. Right-to-left evaluation is counterintuitive even for agents trained primarily on left-to-right code. The lack of types means array shape errors are caught at runtime only.

**Takeaway for Clank:** APL proves that extreme density IS achievable and CAN be coherent — it's not just code golf. The key insight is *implicit lifting*: operations that work on scalars automatically work on arrays. Clank should adopt this principle. But Clank should NOT use Unicode symbols — ASCII-representable operators are essential for reliable LLM generation.

---

### K / Q (kdb+)

**What it is:** APL's spiritual descendant. ASCII-only, extremely terse. K is the core language; Q is a thin readable layer on top. Used in finance for high-performance data processing.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | Even denser than APL in practice. `+/` is sum. `#` is count. |
| Type system | ★★ | Dynamic but with atomic types (int, float, symbol, etc.). |
| Compilation model | ★★★★ | Compiles to highly optimized vector operations. Very fast. |
| Agent-readability | ★★★ | ASCII, but overloaded operators (meaning depends on arity/type). |
| Agent-writability | ★★★ | Terse enough to generate in few tokens. Overloading causes errors. |
| Spec simplicity | ★★★★ | K's core fits on one page. Q adds a readable layer. |

**Strengths for agents:** K demonstrates that APL-level density is achievable in pure ASCII. The operator overloading (same symbol means different things as monad vs. dyad) actually *increases* density while keeping the symbol set tiny. The two-layer architecture (K core + Q surface) is instructive — Clank could have a terse core with an optional readable surface.

**Weaknesses for agents:** Context-dependent operator meaning is a reliability hazard. Agents generate `#` meaning "count" when the context requires "take" (or vice versa). The lack of real types means shape/type errors surface at runtime.

**Takeaway for Clank:** K's two-layer design (terse core + readable surface) directly addresses the OVERVIEW.md question about "human-readable pretty-print mode." Clank should have this. But operator overloading based on arity is too error-prone for agents — each operation should have one meaning.

---

### J

**What it is:** Kenneth Iverson's ASCII successor to APL. Tacit (point-free) programming with forks and hooks.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | Tacit definitions are astonishingly compact. `mean =: +/ % #` |
| Type system | ★★ | Dynamic. Boxed arrays for heterogeneous data. |
| Compilation model | ★★★ | Interpreted with JIT for hot paths. |
| Agent-readability | ★★★ | Tacit style reads like a pipeline. Explicit style is clearer. |
| Agent-writability | ★★★★ | Point-free = no variable naming. Compositional. |
| Spec simplicity | ★★★★ | Small core. Forks and hooks are elegant. |

**Strengths for agents:** J's tacit programming is point-free composition: `mean =: +/ % #` means "mean is: sum divided by count." No variables, no naming, no binding. This is the array-language equivalent of concatenative composition. Forks (f g h → f(x) g(h(x))) and hooks (f g → x f(g(x))) are powerful composition primitives.

**Weaknesses for agents:** Fork/hook semantics require careful tracking of implied argument threading. "Rank" (how operations distribute over array dimensions) is powerful but complex. Debugging is hard — intermediate values aren't named.

**Takeaway for Clank:** J's fork/hook combinators are worth studying alongside Joy's combinators. Both achieve point-free composition but from different angles. The lesson: if Clank goes concatenative or tacit, it needs a SMALL set of well-chosen composition primitives, not a large combinator library.

---

## Tradition 4: Proof Assistants and Dependently Typed Languages

### Coq

**What it is:** Proof assistant based on the Calculus of Inductive Constructions. Programs ARE proofs (Curry-Howard correspondence). Extracts verified code.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★ | Verbose. Proofs take more tokens than the program they verify. |
| Type system | ★★★★★ | Full dependent types. Can express any property. |
| Compilation model | ★★★ | Extracts to OCaml, Haskell, or Scheme. |
| Agent-readability | ★★★★ | Highly structured. Types tell you everything. |
| Agent-writability | ★★ | Proof generation is an active research problem. Agents struggle. |
| Spec simplicity | ★★ | CIC is mathematically elegant but large. |

**Strengths for agents:** The type system can encode ANY property — "this function always returns a sorted list," "this index is in bounds," "this protocol follows exactly these state transitions." If Clank had this, agents could verify correctness without running tests. The extraction model (write verified spec → get correct code) is appealing.

**Weaknesses for agents:** Proofs are EXPENSIVE in tokens. A 5-line function might need 50 lines of proof. Current LLMs are not good at generating Coq proofs — proof search is fundamentally different from code generation. The tactic language adds a whole second language to learn.

**Takeaway for Clank:** Full dependent types are too expensive in syntax overhead for a terse language. But the IDEA — types as machine-checkable contracts — is exactly what Clank needs. The question is finding the right point on the expressiveness/verbosity spectrum. Coq is the ceiling; Clank should aim lower.

---

### Lean 4

**What it is:** Dependently typed language designed to be both a proof assistant AND a general-purpose programming language. Much more "normal" syntax than Coq.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★ | More compact than Coq but still verbose vs. Forth/K. |
| Type system | ★★★★★ | Full dependent types with excellent inference. |
| Compilation model | ★★★★ | Compiles to C. Lean 4 is self-hosting. Real performance. |
| Agent-readability | ★★★★★ | Clean syntax. Types are documentation. Excellent tooling. |
| Agent-writability | ★★★ | Better than Coq but proof obligations still hard. |
| Spec simplicity | ★★★ | Larger than desired but well-structured. |

**Strengths for agents:** Lean 4 is the best existence proof that dependent types + real programming language is viable. Its type inference is excellent — you write fewer annotations than you'd expect. The tactic framework is extensible and more agent-friendly than Coq's. Compiles to real native code.

**Weaknesses for agents:** Still requires explicit proofs for non-trivial properties. The metaprogramming system is powerful but complex. Standard library is growing but not production-complete.

**Takeaway for Clank:** Lean 4 is the closest existing language to what Clank's type system should aspire to, but Clank should use LESS of the type system — only the parts that can be inferred or stated concisely. Key lesson: good type inference reduces syntax overhead of strong types to near zero for common cases.

---

### Idris 2

**What it is:** Dependently typed, purely functional language with first-class support for side effects via "quantitative type theory" (linear types baked in).

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★ | Haskell-like syntax. Not terse. |
| Type system | ★★★★★ | Dependent + linear types. Resource tracking built in. |
| Compilation model | ★★★★ | Compiles to Scheme (Chez, Racket) or RefC. |
| Agent-readability | ★★★★ | Clear and predictable. Quantities on bindings are novel but consistent. |
| Agent-writability | ★★★ | Same challenges as Lean for proof generation. |
| Spec simplicity | ★★★ | QTT adds complexity but solves real problems. |

**Strengths for agents:** Idris 2's linear types solve a critical problem: resource management without garbage collection complexity. If a value is used exactly once, the compiler can stack-allocate it. For agents writing systems code, this means memory safety without Rust's borrow checker complexity. The quantity annotations (0 = erased, 1 = linear, ω = unrestricted) are simple and powerful.

**Weaknesses for agents:** Haskell-style syntax is not terse. Dependent pattern matching is hard for agents to generate reliably. Ecosystem is small.

**Takeaway for Clank:** Idris 2's quantitative types are the most agent-friendly approach to resource management I've seen. Three annotations (0, 1, ω) cover all cases. Clank should consider adopting this or something similar rather than Rust-style lifetimes or traditional garbage collection.

---

## Tradition 5: Esoteric and Minimal Languages

### Brainfuck

**What it is:** 8 instructions operating on a tape. Turing complete. The canonical "minimal" language.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | 8 symbols. Maximum density in theory. |
| Type system | ★ | None. Bytes on a tape. |
| Compilation model | ★★★★★ | Trivially compilable. |
| Agent-readability | ★ | Practically unreadable at any scale. No semantic structure. |
| Agent-writability | ★ | Generating correct BF for non-trivial tasks is nearly impossible. |
| Spec simplicity | ★★★★★ | Fits in a tweet. |

**Strengths for agents:** Proves Turing completeness with 8 instructions. Trivially parseable and compilable. Useful as a theoretical lower bound.

**Weaknesses for agents:** Useless in practice. The semantic gap between intent and instruction is so large that BF programs are effectively opaque. No agent can reliably generate BF for anything beyond trivial programs.

**Takeaway for Clank:** BF is the existence proof that "minimal" is not sufficient — you also need *semantic* density, not just *syntactic* minimality. A small number of high-level operations beats a tiny number of low-level ones. Clank should minimize the number of constructs but each construct should carry significant semantic weight.

---

### Unlambda

**What it is:** Minimal functional language based on combinatory logic (S and K combinators). No variables, no lambda.

| Axis | Rating | Notes |
|------|--------|-------|
| Syntax density | ★★★★★ | Two combinators + application. |
| Type system | ★ | None. |
| Compilation model | ★★★ | Graph reduction. |
| Agent-readability | ★ | Deeply nested application trees. Opaque. |
| Agent-writability | ★ | Generating SKI combinator terms is harder than lambda terms. |
| Spec simplicity | ★★★★★ | Two combinators. |

**Takeaway for Clank:** Combinatory logic (no variables at all) is theoretically appealing but practically unworkable. Even agents need SOME way to name intermediate results. The lesson: point-free is good, but fully variable-free is too far.

---

## Cross-Cutting Analysis

### What each tradition got RIGHT for agents

| Tradition | Key Insight for Clank |
|-----------|----------------------|
| Lambda calculus | Minimal core with clear semantics. Types guide generation. |
| Concatenative | No variable binding. Composition by juxtaposition. Incremental development. |
| Array/tacit | Extreme density. Implicit lifting. Point-free composition. Two-layer design. |
| Proof assistants | Types as contracts. Machine-verified correctness. Type inference reduces annotation burden. |
| Esoteric | Semantic density matters more than syntactic minimality. |

### What each tradition got WRONG for agents

| Tradition | Pitfall to Avoid |
|-----------|-----------------|
| Lambda calculus | No primitive types. Deep nesting. Substitution bugs. |
| Concatenative | No types. Stack depth tracking is error-prone. |
| Array/tacit | Operator overloading. Unicode symbols. Implicit argument threading. |
| Proof assistants | Proof verbosity. Tactic languages add a second language. |
| Esoteric | Low semantic density. Impractical for real programs. |

### The Syntax Density Sweet Spot

Plotting languages on an axis from "most terse" to "most verbose":

```
BF/Unlambda  K/J/APL  Forth/Joy  Factor  Lean/Idris  Coq  Python  Java
|____________|_________|__________|________|___________|_____|_______|
 Too low-level  Good density  Good balance   Good types   Too verbose
              but no types   but no types   but not terse
```

**Clank's target zone: between Factor and Lean.** Concatenative density with dependent-type-level contracts. Nothing in this zone exists today.

---

## Recommendations for Clank

### Primary design influences (ranked)

1. **Joy/Factor** (execution model) — Concatenative, point-free core. Eliminates variable binding. Composition by juxtaposition. Factor proves this scales to production. Joy's combinator approach is cleaner than Forth's stack shuffling.

2. **Lean 4 / Idris 2** (type system) — Dependent types with excellent inference for the common case. Idris's quantitative types (0/1/ω) for resource management. But: Clank should default to inferred types and only require annotations at module boundaries.

3. **K/J** (density philosophy + two-layer architecture) — ASCII-only operators with high semantic density. Each operator does one thing. A terse core with an optional readable surface for human review. Implicit lifting of operations over data structures.

4. **STLC** (theoretical foundation) — The type system should be grounded in well-understood type theory, not ad hoc. This ensures soundness and makes formal verification of the type checker feasible.

### Specific design recommendations

**Execution model:** Concatenative (stack-based with combinators). Programs are pipelines of transformations. No explicit variable binding in the common case; named locals available as opt-in sugar.

**Type system:** Refinement types as the default (`Int{>0}`, `List{len>0}`), with full dependent types available for module-level contracts. Quantitative annotations (0/1/ω) for resource tracking. Aggressive type inference — annotations required only at function boundaries.

**Syntax:** ASCII-only. Single-character operators where possible. Whitespace-separated tokens (like Forth). No semicolons, no braces for blocks — use indentation or explicit delimiters that are easy to count. Each operator has exactly one meaning (no K-style arity overloading).

**Compilation:** Target WASM as primary (portable, sandboxed, fast). LLVM IR as secondary (native performance). The compiler should be small enough that its source fits in a context window.

**Spec size:** Target ≤100 production rules for the core grammar. ≤50 typing rules. The complete spec should be ≤5000 tokens — readable in a single agent context load.

**Tooling:** Structured JSON errors from the compiler. Stack-effect + type signatures as the primary documentation format. Incremental compilation at the word/function level (like Forth's dictionary model).

### What Clank would look like (illustrative, not prescriptive)

A function to compute the mean of a list, showing the concatenative + typed style:

```
# Hypothetical Clank — NOT a design decision, just illustration
mean : List{len>0} Int -> Rat
  = dup sum swap len /
```

Compare Python:
```python
def mean(xs: list[int]) -> float:
    assert len(xs) > 0
    return sum(xs) / len(xs)
```

The Clank version: fewer tokens, same information, *more* type safety (the precondition is in the type, not a runtime assert).
