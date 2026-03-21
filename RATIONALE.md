# Clank Design Rationale

This document explains *why* each major design decision was made in Clank. Every choice is grounded in the project's core hypothesis: a programming language designed for AI agents faces fundamentally different constraints than one designed for humans. Agent bottlenecks — context window pressure, token cost, generation reliability, and cold-start learning — replace the human concerns of readability, scannability, and naming ergonomics that shaped every prior language.

---

## 1. Applicative-Primary with Concatenative Pipeline Sugar

**Decision:** Clank uses named function application as its primary execution model, with a pipeline operator (`|>`) providing left-to-right composition sugar for data transformations.

**Alternatives considered:**
- **Pure concatenative (Forth/Joy-style):** Programs are sequences of words manipulating an implicit stack. No variable binding. Maximum terseness.
- **Pure applicative (ML/Haskell-style):** Named functions, named arguments, explicit binding.
- **Hybrid (chosen):** Applicative core with pipeline sugar for data flow.

**Why not pure concatenative?**

The language survey (TASK-001) identified concatenative programming as extremely token-efficient — no variable names, no binding syntax, composition by juxtaposition. This was the initial front-runner. However, DECISION-001 identified a critical flaw: **LLMs struggle with implicit stack state.**

When an agent writes concatenative code, it must mentally simulate the stack at every point to know what values are available. After 4-5 stack items, even sophisticated models lose track. Stack shuffling words (`dup`, `swap`, `rot`, `over`) are a frequent source of generation errors. This was confirmed by the agent code patterns research (TASK-003): the #1 friction point for agents writing code is context window pressure, but the #2 friction point is type errors from implicit state.

Named variables solve this. When a value has a name and a type, the agent's search space is constrained — it knows exactly what's available and what type it is. Variables with types act as *checkpoints* that prevent error cascading through long expressions.

**Why not pure applicative?**

Pure applicative syntax is verbose. `f(g(h(x)))` nests deeply and reads inside-out. For simple data pipelines — the most common pattern in agent-written code — this is wasteful.

**Why this hybrid?**

The pipeline operator gives concatenative terseness for simple cases: `data |> parse |> validate |> store` reads left-to-right with no nesting and no intermediate names. For complex logic with 5+ intermediate values, named `let` bindings provide clarity and type checkpoints. This matches how agents actually work — streaming transformations for simple cases, named intermediates for complex cases.

The research also showed that types constrain the generation search space more effectively than the absence of variable binding. When an agent sees `x : Int{> 0}`, it knows exactly what `x` is and what it can do. That constraint is more valuable than saving the tokens for `let x =`.

---

## 2. Refinement Types + Algebraic Effects + Affine Types

**Decision:** Clank's type system combines three orthogonal mechanisms: refinement types for value contracts, algebraic effects for behavioral contracts, and affine types for resource protocols.

**Alternatives considered:**
- **Full dependent types (Idris/Lean):** Can express any computable property as a type.
- **Simple HM types (ML):** Well-understood, excellent inference, but limited contract power.
- **Refinement types only:** Strong value contracts, no behavioral or resource tracking.
- **Hybrid (chosen):** Refinement + effects + affine.

### 2a. Why Refinement Types over Dependent Types

The type system research (TASK-002) evaluated five approaches on contract expressiveness, syntax cost, inference power, implementation complexity, and agent comprehension. Full dependent types scored highest on expressiveness (5/5) but lowest on syntax cost (2/5) and implementation complexity (1/5).

The core problem: dependent types require *proof terms*. A 5-line function can need 50 lines of proof to verify a non-trivial property. Current LLMs are not reliable proof generators — proof search is fundamentally different from code generation. This directly violates Principle 1 (terse by default) and Principle 3 (genuinely simple).

Refinement types cover the same practical ground with dramatically less syntax. Instead of constructing a proof that a list is non-empty, the agent writes `[Rat]{len > 0}` — a logical predicate that an SMT solver verifies automatically. The predicate language is restricted to decidable logic (QF_UFLIRA: quantifier-free linear integer/real arithmetic with uninterpreted functions), which means:

1. Verification is automatic — no proof terms, no proof search
2. Predicates are concise logical formulas — agents generate these reliably
3. Coverage is practical — bounds checking, non-null, ordering, arithmetic relationships cover >90% of real contracts
4. The spec fits in one context window (a hard constraint from OVERVIEW.md)

What refinement types cannot express — "this binary tree is balanced," "this sort is stable" — are deferred to runtime assertions or test-time verification. This is an acceptable tradeoff for v1: most agent-written code needs bounds checking and non-null guarantees, not theorem-level properties.

### 2b. Why Algebraic Effects over Monads or Unchecked Side Effects

Effects answer a question refinement types cannot: "what does this function *do*?" A function typed `Str -> <io, exn> Config` tells the agent it performs I/O and may fail — without reading the implementation. This is critical for agent reasoning: an agent composing functions can check effect compatibility statically.

**Why not monads?** Monads compose poorly. Monad transformer stacks (`MonadIO`, `MonadState`, `MonadError`) require explicit lifting and are notoriously confusing even for experienced human programmers. For agents, the unpredictable nesting of transformer types is a generation reliability problem. Effect rows compose freely — `<io, exn>` just works with no lifting.

**Why not unchecked side effects?** If effects are invisible (as in Python, JavaScript, Go), the agent must read every function body to know what it does. With tracked effects, the signature is sufficient. This aligns with Principle 2: types as machine-verifiable contracts, not documentation.

The Koka-inspired design uses row polymorphism for effect variables, which means higher-order functions like `map` work with any effect row — one definition serves pure and effectful callbacks. The effect system also *unifies* error handling, state, I/O, and async under a single mechanism, eliminating four separate language features (try/catch, mutable variables, IO marking, async/await) in favor of one.

### 2c. Why Affine Types (and Not Borrow Checking)

Affine types enforce resource protocols: files must be closed, connections must be released, tokens must be redeemed. Without them, resource safety depends on programmer discipline — unreliable for both humans and agents.

**Why not Rust-style borrow checking?** DECISION-001 was decisive here: borrow checking with lifetime annotations is too expensive for LLM token budgets. Lifetime parameters, lifetime elision rules, and lifetime error messages are the steepest complexity cliff in Rust. Agents generating Rust code spend disproportionate tokens fighting the borrow checker.

Clank's affine types are deliberately simpler:
- **No lifetime annotations.** Borrows are scoped to a single function call — they cannot be stored or returned. This eliminates the entire lifetime inference/annotation system.
- **No mutable borrows.** All borrows are read-only. Mutation happens through the `state` effect or by consuming and recreating values. This eliminates the exclusivity checking that drives most borrow checker complexity.
- **GC handles memory.** Affine types enforce resource protocols (close, disconnect), not memory safety. The GC is the memory safety fallback.

The cost is that some patterns require explicit `clone` where Rust would use a mutable borrow, and borrows cannot be stored in data structures. This is acceptable — resource-protocol enforcement is the goal, not zero-cost memory management.

### 2d. Why These Three Compose

The three mechanisms are orthogonal by design:

| Concern | Mechanism | Example |
|---------|-----------|---------|
| "This integer is positive" | Refinement: `Int{> 0}` | Value invariant |
| "This function is pure" | Effect: `<>` | Behavioral contract |
| "This handle must be consumed" | Affine: move semantics | Resource protocol |
| "Input and output have same length" | Refinement: `{[a] \| len == len(xs)}` | Relational contract |
| "This function may fail with ParseErr" | Effect: `<exn[ParseErr]>` | Error contract |

Each mechanism handles a distinct class of contract. Together, they cover the vast majority of practical correctness properties while remaining decidable, inferable, and specifiable within one context window.

---

## 3. TypeScript Implementation

**Decision:** The Clank reference implementation (compiler, VM, tooling) is written in TypeScript.

**Alternatives considered:**
- **Rust:** Maximum performance, strong typing, but slow compile times and high complexity.
- **Go:** Fast compilation, simple language, but limited type expressiveness.
- **OCaml/Haskell:** Traditional PL implementation languages with ADTs and pattern matching.
- **C:** Maximum control, but manual memory management and no ADTs.
- **TypeScript (chosen):** Dynamic enough for rapid iteration, typed enough for correctness, runs everywhere.

**Why TypeScript:**

1. **Agent familiarity.** TypeScript is among the most-generated languages by current LLMs. Agents working on the Clank toolchain can read, understand, and modify TypeScript code reliably. This matters because a core design principle (elevated from TASK-006) is that the **toolchain must fit in agent context** — an agent that cannot read and modify its own compiler is not truly agent-native.

2. **Rapid iteration.** Clank is a design-first project. The research phase produced multiple spec revisions (core syntax went from v0.1 concatenative to v0.2 applicative after DECISION-001). TypeScript's dynamic typing at the boundaries, combined with its structural type system, allows fast refactoring without fighting the compiler.

3. **Ecosystem.** Node.js provides file I/O, process execution, HTTP, and JSON handling out of the box — exactly the capabilities the Clank stdlib needs to wrap. The test infrastructure (the custom test runner) is straightforward in TypeScript.

4. **Codebase size.** The full implementation is ~5000 lines across 11 source files. This fits the agent context constraint. A Rust or C implementation of equivalent functionality would likely be 2-3x larger due to explicit memory management, error handling boilerplate, and build configuration.

5. **Platform portability.** TypeScript/Node.js runs on every platform agents operate on. No cross-compilation required.

**What TypeScript is NOT for:** TypeScript is the implementation language for the reference toolchain, not a compilation target. Clank programs are compiled to custom bytecode (Phase 1) and eventually WASM (Phase 2). TypeScript's runtime performance is adequate for a tree-walking interpreter and bytecode compiler; production execution will use the VM or WASM.

---

## 4. Garbage Collection over Borrow Checking

**Decision:** Clank uses mark-sweep tracing GC for memory management, not Rust-style ownership/borrowing.

**Alternatives considered:**
- **Rust-style borrow checking:** Compile-time memory safety with zero runtime overhead.
- **Reference counting:** Deterministic deallocation, simpler than tracing GC.
- **Perceus-style reuse analysis:** Compile-time RC insertion with in-place reuse.
- **Tracing GC (chosen):** Mark-sweep with bump+free-list allocation.

**Why GC over borrow checking:**

DECISION-001 established the principle directly: borrow checking is too expensive for LLM token budgets. This deserves elaboration.

Rust's borrow checker provides memory safety through compile-time ownership tracking. The cost is lifetime annotations — explicit parameters that describe how long references live relative to each other. While Rust infers lifetimes in simple cases, complex cases (multiple references, returned references, references in structs) require explicit `'a` parameters and produce notoriously opaque error messages.

For an LLM generating code, lifetime annotations have three problems:

1. **Token cost.** Lifetime parameters appear in function signatures, struct definitions, and impl blocks. A function like `fn process<'a, 'b>(data: &'a [u8], config: &'b Config) -> &'a str` requires 6 extra tokens for lifetimes alone. Across a codebase, this compounds.

2. **Generation unreliability.** Lifetime inference is non-local — the correct lifetime annotation depends on the entire call graph. Agents generating code incrementally (function by function) frequently produce lifetime errors that require reasoning about distant code.

3. **Error recovery cost.** When a borrow checker error occurs, fixing it often requires restructuring code — moving declarations, cloning values, or reorganizing ownership. This is expensive in agent tokens and iteration cycles.

**Why tracing GC specifically:**

The GC strategy research (TASK-013) evaluated reference counting, mark-sweep, and Perceus-style reuse analysis:

- **Reference counting** was rejected because closures and effect handler continuations can create reference cycles. Cycle detection adds complexity that approaches mark-sweep while being less general. The continuation problem is acute: `EFFECT_PERFORM` captures the entire stack as a heap object, requiring O(stack_depth) RC adjustments per effect operation.

- **Perceus-style reuse** is elegant (compile-time RC insertion with in-place memory reuse for affine values) but requires significant compiler infrastructure that exceeded the implementation budget. It remains a future optimization.

- **Mark-sweep** handles cycles naturally, has no per-operation overhead, and integrates cleanly with effect handler continuations (captured continuations are just heap objects). Implementation: ~380 lines, well within the agent context budget.

**The division of labor:** GC handles memory. Affine types handle resources. This separation is clean — the GC reclaims bytes; affine types ensure `close` and `disconnect` are called. If an affine value is leaked (via `discard` or bug), the GC reclaims memory but file descriptors/connections may leak. The compiler warns about unconsumed affine values, making leaks visible.

---

## 5. Terse-by-Default Philosophy

**Decision:** Token count is a first-class constraint. Every syntactic element must earn its place. Verbosity that exists solely for human scannability is eliminated or opt-in.

**Why this matters:**

The OVERVIEW.md hypothesis identifies the fundamental asymmetry: every existing language optimizes for human readability. Humans scan code visually — indentation, whitespace, verbose identifiers, and comments all help humans orient themselves. Agents don't share these constraints. Their bottlenecks are:

1. **Context window size.** An agent's "working memory" is fixed. Verbose code fills it with syntactic noise, leaving less room for semantic content. The language survey found that Python programs are 2-3x more tokens than equivalent K programs — that's 2-3x less code an agent can hold in context simultaneously.

2. **Token cost.** Every token has a computational and financial cost. A program that requires 500 tokens instead of 1200 is cheaper to generate, cheaper to check, and cheaper to iterate on.

3. **Generation reliability.** Shorter programs have fewer opportunities for errors. An agent generating a 10-token function makes fewer mistakes than one generating a 30-token function that does the same thing.

**What "terse" means concretely:**

- **Short keywords:** `fn`, `let`, `mod`, `pub` instead of `function`, `const`, `module`, `public`.
- **Short stdlib names:** `str.len`, `col.rev`, `fs.read` instead of `string.length`, `collections.reverse`, `filesystem.readFile`. The stdlib targets 2-6 character names.
- **No ceremony:** No semicolons, no mandatory braces (except where structurally necessary), no redundant type annotations (inference handles the common case).
- **Pipeline syntax:** `data |> f |> g` instead of `g(f(data))` — same semantics, left-to-right reading, no nesting.
- **Operator desugaring:** All operators desugar to function calls. `a ++ b` = `str.cat(a, b)`. Minimal syntax, maximum uniformity.

**What "terse" does NOT mean:**

The language survey's key finding from Brainfuck and Unlambda was that **semantic density matters more than syntactic minimality**. A small number of high-level operations beats a tiny number of low-level ones. Clank does not minimize the character count of programs — it minimizes the token count while maximizing the semantic content per token. `Int{> 0}` is not the shortest way to write "positive integer," but it is the most semantically dense way that is also machine-verifiable.

The target is **40-60% fewer tokens than Python/TypeScript** for equivalent programs, while maintaining or exceeding their contract expressiveness. The language survey placed Clank's density target "between Factor and Lean" — concatenative-level density with dependent-type-level contracts.

**The two-layer architecture:**

Following K/Q's precedent, Clank supports a terse canonical form (what agents read/write) and a human-readable pretty-print mode (for when humans need to review agent-written code). The canonical form is optimized for density; the pretty-print is optimized for scannability. This resolves the tension between agent optimization and human oversight.

---

## 6. Comprehensive Standard Library (240 Words)

**Decision:** Clank ships a comprehensive standard library covering 16 categories and 240 functions, organized into Tier 1 (auto-imported) and Tier 2 (explicit import).

**Alternatives considered:**
- **Minimal stdlib (Rust/Go approach):** Ship only primitives; rely on ecosystem packages.
- **Comprehensive stdlib (Python approach):** Ship everything an agent commonly needs.
- **No stdlib (Forth approach):** Everything is user-defined; agent builds up from primitives.

**Why comprehensive:**

The agent code patterns research (TASK-003) analyzed what real programs agents write most often and identified the #3 friction point: **boilerplate and dependency decisions**. When an agent needs to parse JSON, make an HTTP request, or read a file, it must either:

1. Know the language's stdlib API (if the capability exists), or
2. Search for, evaluate, and import a third-party package.

Option 2 is extremely expensive for agents. Package discovery requires searching registries, reading documentation, evaluating quality/maintenance, resolving version conflicts, and adding build configuration. Each step consumes context and tokens. Human developers amortize this cost over months of familiarity; agents pay it fresh every session.

A comprehensive stdlib eliminates dependency decisions for the 16 most common categories:

**Tier 1 (auto-imported, covers ~90% of agent tasks):**
- `std.str` (24 words) — string manipulation
- `std.json` (15 words) — JSON encode/decode/navigate
- `std.fs` (19 words) — file I/O with affine handles
- `std.col` (49 words) — collections (list, map, set)
- `std.http` (9 words) — HTTP client
- `std.err` (18 words) — Result/Option/error handling
- `std.proc` (9 words) — process execution
- `std.env` (6 words) — environment variables

**Tier 2 (explicit import, covers the next ~9%):**
- `std.srv` (HTTP server), `std.cli` (arg parsing), `std.dt` (datetime), `std.csv`, `std.log`, `std.test`, `std.rx` (regex), `std.math`

**Why these categories?** TASK-003 ranked agent coding tasks by frequency. The top categories — string manipulation, JSON handling, file I/O, HTTP requests, and collection operations — account for the vast majority of agent-written utilities, data transformations, and orchestration scripts. Clank's stdlib covers them all without requiring any external packages.

**Naming strategy:** All stdlib functions use terse, dot-namespaced names (2-6 characters preferred). `str.len` not `string.length`. `col.rev` not `collections.reverse`. Each name has exactly one meaning (no overloading). This serves both density (fewer tokens) and reliability (unambiguous names reduce generation errors).

**Effect annotations:** Every stdlib function declares its effects. An agent can determine at a glance whether `fs.read` performs I/O (`<io, exn>`) or whether `col.sort` is pure (`<>`). This is not documentation — it's machine-verified contract information that enables static composition checking.

---

## 7. Custom Stack VM as Primary Target

**Decision:** Clank compiles to a custom stack-based bytecode VM first, with WASM as a secondary production target. LLVM is deferred indefinitely.

**Alternatives considered:**
- **WASM only:** Portable, sandboxed, good performance.
- **LLVM IR:** Best native performance, battle-tested backends.
- **Custom VM only:** Maximum control, minimal toolchain.
- **Custom VM + WASM (chosen):** Development speed first, production deployment second.

**Why custom VM first:**

The compilation target research (TASK-006) identified a hard constraint that dominates all others: **the toolchain must fit in agent context.** From OVERVIEW.md: "The language spec should be completeable in one context window." The toolchain should follow the same principle.

| Target | Compiler source size | Fits in agent context? |
|--------|---------------------|----------------------|
| Custom VM | ~1,500 lines | Yes |
| WASM encoder | ~3,000-5,000 lines | Marginally |
| LLVM binding | ~500 lines + millions of LLVM source | No |

**An agent that cannot read, understand, and modify its own compiler is not truly agent-native.** This constraint alone nearly justifies the decision.

Beyond context fit, the custom VM provides:

1. **Natural compilation target.** Applicative syntax compiles naturally to stack bytecode via left-to-right argument evaluation and CALL — the same strategy used by the JVM, CPython, and the CLR. No lowering to SSA form or register allocation required.

2. **Incremental compilation.** New functions compile independently. Redefining a function recompiles only that function's body. This maps directly to the agent workflow of "define, test, refine."

3. **Total control over error reporting.** Every phase of compilation and execution is under Clank's control. Structured JSON diagnostics with source locations and type errors can be emitted exactly as the agent-native tooling spec requires. No parsing or translating errors from an external tool.

4. **Fast compile times.** Bytecode compilation is O(n) with small constants. An agent doing rapid define-test-refine cycles gets millisecond feedback.

**Why WASM second:**

WASM adds deployment flexibility (browser, server, edge), ecosystem interop (any WASM-targeting language can call Clank), and sandboxing (deny-by-default capability model). WASM 3.0's GC support means Clank wouldn't need a separate GC implementation for the WASM backend. But WASM's incremental compilation story is weak (modules are the compilation unit) and runtime errors come from the WASM runtime rather than Clank — both friction points for the agent development loop.

**Why LLVM is deferred:**

LLVM's toolchain is millions of lines of C++. An agent cannot reason about, carry, or modify LLVM in context. The LLVM backend would make Clank's compiler a thin, opaque frontend — the opposite of agent-native. Additionally, LLVM IR is SSA-form register-based, requiring a non-trivial lowering pass from stack-based bytecode. The performance gain (1.5-2x over WASM JIT) doesn't justify the complexity unless compute-heavy workloads become a proven bottleneck.

---

## 8. ASCII-Only Syntax, No Operator Overloading

**Decision:** All Clank tokens are ASCII. Each operator has exactly one meaning. No Unicode symbols, no context-dependent operators.

**Why ASCII-only:**

The language survey found that APL's Unicode glyphs are a serious problem for LLM tokenizers — they tokenize inconsistently and agents frequently generate wrong glyphs. K avoids Unicode but introduces context-dependent operators (same symbol means different things depending on arity), which causes a different class of generation errors.

ASCII characters are reliably tokenized by every LLM tokenizer. An agent generating Clank code never needs to worry about whether a glyph will round-trip correctly through tokenization.

**Why no operator overloading:**

Each operator in Clank desugars to exactly one function call. `++` always means `str.cat`. `+` always means integer/rational addition. There is no mechanism for user-defined operator meanings.

This serves agent reliability: when an agent sees `a ++ b`, it knows unambiguously what function is being called and what types are expected. In languages with operator overloading (C++, Scala, Haskell), `+` might mean integer addition, string concatenation, vector addition, or a domain-specific operation — the agent must resolve the overload based on types, which is a frequent source of generation errors.

---

## 9. Spec Size Constraint: One Context Window

**Decision:** The complete language specification must fit in ~5000 tokens — readable in a single agent context load.

**Why this matters:**

This is the enabling constraint for "cold-start" learning. When an agent encounters Clank for the first time with no prior training, it should be able to read the entire spec in one context load and immediately start writing correct programs. No language in existence achieves this — Python's spec is tens of thousands of tokens, Rust's is hundreds of thousands, and even Go's "small" spec exceeds a single context window.

The spec size constraint cascades into every other design decision:
- The type system must be expressible in ~50 typing rules (not hundreds)
- The grammar must be ~60 production rules (not thousands)
- The keyword set must be small (~20, not ~50)
- Built-in effects are limited to 4 (not open-ended)
- The stdlib is comprehensive but uses terse names to minimize documentation size

The current spec (SPEC.md) fits in approximately 2300 tokens and covers lexical structure, grammar, type system (effects, refinements, affine types, row polymorphism, interfaces), module system, and standard library summary.

---

## Summary

Every design decision in Clank follows from one principle: **optimize for the agent, not the human.** This means:

| Human optimization | Clank's agent optimization |
|-------------------|---------------------------|
| Verbose identifiers for scannability | Terse identifiers for density |
| Implicit side effects for convenience | Tracked effects for static reasoning |
| Runtime assertions for contracts | Refinement types for compile-time verification |
| Manual memory management or hidden GC | GC + affine types (memory is free, resources are tracked) |
| Large ecosystem with package discovery | Comprehensive stdlib eliminating dependency decisions |
| Opaque optimizing backends (LLVM) | Transparent toolchain that fits in context |
| Spec as reference manual (read on demand) | Spec fits in one load (cold-start learning) |

Clank occupies a design space that no existing language targets: **concatenative-level density with dependent-type-level contracts and a toolchain small enough for an agent to hold in working memory.** The decisions above are the minimal, mutually-reinforcing set of choices that make this possible.
