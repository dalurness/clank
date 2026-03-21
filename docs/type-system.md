# Type Systems for Agent-Optimized Contracts — Research Report

**Task:** TASK-002
**Mode:** Research
**Date:** 2026-03-13

---

## 1. Evaluation Criteria

Each type system approach is evaluated on five axes, rated 1–5 (5 = best fit for Clank):

| Criterion | What it measures |
|-----------|-----------------|
| **Contract Expressiveness** | Can it encode preconditions, postconditions, invariants, and behavioral contracts directly in type signatures? |
| **Syntax Cost** | How much syntactic overhead does it add? (5 = minimal overhead, 1 = heavy annotation burden) |
| **Inference Power** | How much can the type checker figure out without explicit annotations? |
| **Implementation Complexity** | How hard is it to build a correct type checker/solver? (5 = straightforward, 1 = research-level difficulty) |
| **Agent Comprehension** | How predictable and parseable are type signatures for an LLM agent? Can an agent reliably write correct types? |

---

## 2. Type System Approaches

### 2.1 Dependent Types (Idris, Agda, Lean)

**What they are:** Types can depend on values. A function's return type can reference its input values. This allows types like `Vec n a` (a vector of exactly `n` elements of type `a`) where `n` is a value, not just a type parameter.

**How contracts are expressed:**
```
-- Idris-style: sorted insert guarantees length increases by 1
insert : (x : Nat) -> (xs : Vect n Nat) -> Sorted xs -> Vect (S n) Nat
```
Preconditions and postconditions are encoded directly in the type. The type checker proves they hold at compile time.

**Strengths for Clank:**
- Maximum expressiveness: any computable property can be a type constraint
- No gap between "what the type says" and "what the function does" — types ARE the contract
- Eliminates entire classes of runtime errors statically
- Aligns perfectly with Principle 2 (types as machine-verifiable contracts)

**Weaknesses for Clank:**
- Type-level computation makes inference undecidable in general — programmers (agents) must supply proofs
- Proof obligations can be verbose: proving a list is sorted requires constructing proof terms
- Implementation is hard — full dependent type checking is a significant engineering effort
- Error messages from failed unification can be opaque even for agents
- The spec surface area is large, conflicting with Principle 3 (genuine simplicity)

**Precedents:** Idris 2 has a practical focus (can compile to native). Lean 4 is both a proof assistant and a general-purpose language. Agda is more research-oriented.

| Criterion | Rating | Notes |
|-----------|--------|-------|
| Contract Expressiveness | **5** | Can express anything computable as a type constraint |
| Syntax Cost | **2** | Proof terms and type-level functions add significant annotation |
| Inference Power | **2** | Undecidable in general; requires explicit proofs for interesting properties |
| Implementation Complexity | **1** | Requires unification, normalization, totality checking, universe hierarchy |
| Agent Comprehension | **3** | Signatures are information-dense but proof terms are hard to generate correctly |

---

### 2.2 Refinement Types (Liquid Haskell, F*)

**What they are:** Base types annotated with logical predicates drawn from a decidable logic (typically quantifier-free linear arithmetic + uninterpreted functions). The type checker calls an SMT solver to verify predicates automatically.

```
-- Liquid Haskell style
{-@ div :: Nat -> {v:Nat | v > 0} -> Nat @-}
div :: Int -> Int -> Int
```

**How contracts are expressed:**
Preconditions are refinements on input types. Postconditions are refinements on the return type. Invariants are expressed as refined type aliases. The SMT solver handles proof obligations automatically — no manual proof terms.

**Strengths for Clank:**
- Excellent contract expressiveness within decidable fragments — covers most practical invariants (bounds, non-null, ordering, arithmetic relationships)
- Fully automatic verification: the agent writes predicates, the SMT solver proves them
- Syntax is an annotation layer on top of standard types — incremental adoption possible
- Aligns with Principle 3: the refinement language is a constrained, predictable subset
- Agent-friendly: predicates are logical formulas, which agents handle well

**Weaknesses for Clank:**
- Limited to the decidable logic fragment — can't express arbitrary computation in types (unlike dependent types)
- Requires bundling or depending on an SMT solver (Z3) — heavyweight runtime dependency
- SMT solver behavior can be unpredictable: small changes to code can cause timeouts or different proof paths
- Error messages from SMT failures can be "unable to verify" without clear guidance
- Annotation syntax is additional surface area (though less than dependent types)

**Precedents:** Liquid Haskell is the most mature. F* (used in Project Everest for verified crypto) combines refinement types with effects. Dafny uses similar ideas in an imperative setting.

| Criterion | Rating | Notes |
|-----------|--------|-------|
| Contract Expressiveness | **4** | Covers most practical contracts; limited by decidable logic |
| Syntax Cost | **4** | Predicates are concise; sit alongside standard types |
| Inference Power | **4** | SMT solver handles proofs automatically; liquid type inference infers many refinements |
| Implementation Complexity | **2** | Requires SMT solver integration; encoding types as SMT queries is nontrivial |
| Agent Comprehension | **5** | Predicates are simple logical formulas — ideal for agents |

---

### 2.3 Linear Types (Rust, Linear Haskell)

**What they are:** Values with linear types must be used exactly once (linear) or at most once (affine, as in Rust). This encodes resource management — memory, file handles, connections — into the type system.

```
// Rust-style: ownership transfer prevents use-after-free
fn consume(file: File) -> Result<Data> { ... }
// `file` is moved; caller can't use it after this call
```

**How contracts are expressed:**
Linear types encode resource lifecycle contracts: "this value must be consumed," "this handle must be closed," "this token can only be used once." They don't directly express value-level invariants (e.g., "this integer is positive") but excel at structural/protocol contracts.

**Strengths for Clank:**
- Eliminates resource leaks and use-after-free at compile time — important for agent-written system code
- Enables memory safety without garbage collection (Rust's approach)
- Relatively simple to implement compared to dependent/refinement types
- Clear, predictable rules: ownership and borrowing have few special cases
- Agents can reason about linearity mechanically — the rules are algorithmic

**Weaknesses for Clank:**
- Narrow scope: only contracts about resource usage, not arbitrary value invariants
- Not sufficient alone for Clank's goal of encoding preconditions/postconditions in signatures
- Borrow checker complexity (if Rust-style) — lifetime annotations add syntax cost and are notoriously difficult
- Linear Haskell's approach (multiplicity annotations) is simpler but less proven for systems programming

**Precedents:** Rust is the dominant success story. Linear Haskell extends GHC. ATS combines linear types with dependent types.

| Criterion | Rating | Notes |
|-----------|--------|-------|
| Contract Expressiveness | **2** | Resource contracts only; can't express value-level invariants |
| Syntax Cost | **3** | Ownership is often implicit; lifetimes add cost when explicit |
| Inference Power | **4** | Rust infers lifetimes in most cases; linearity is largely implicit |
| Implementation Complexity | **3** | Borrow checker is nontrivial but well-understood; simpler than dependent types |
| Agent Comprehension | **4** | Rules are mechanical and predictable; lifetime errors can be confusing |

---

### 2.4 Session Types

**What they are:** Types that describe communication protocols between concurrent processes. A session type specifies the sequence, direction, and types of messages in a channel.

```
-- A protocol: send Int, receive Bool, done
type Server = !Int.?Bool.End
type Client = ?Int.!Bool.End  -- dual of Server
```

**How contracts are expressed:**
Session types encode behavioral contracts for communication: "this endpoint sends an Int then receives a Bool then terminates." The type checker ensures both sides of a channel follow the protocol. Branching and recursion in protocols are supported.

**Strengths for Clank:**
- Perfect for encoding inter-agent communication protocols
- Catches protocol violations at compile time — deadlock freedom in some formulations
- Directly relevant if Clank programs involve message-passing concurrency
- Small, elegant theory — few concepts, big payoff for concurrent code

**Weaknesses for Clank:**
- Very narrow scope: only communication protocols, not general invariants
- Not useful for sequential code or value-level contracts
- Implementation requires channel-aware type checking
- Limited practical deployment — most implementations are research prototypes
- Composition of session types with other type features is an active research area

**Precedents:** Links (language by Wadler et al.), Scribble (protocol description language), various Haskell/OCaml embeddings. Rust's `session-types` crate demonstrates embedding in a mainstream language.

| Criterion | Rating | Notes |
|-----------|--------|-------|
| Contract Expressiveness | **2** | Communication protocols only; no value-level contracts |
| Syntax Cost | **3** | Protocol descriptions are concise |
| Inference Power | **2** | Limited inference; protocols must be specified |
| Implementation Complexity | **3** | Core theory is clean; integration with other features is harder |
| Agent Comprehension | **4** | Protocol descriptions are structured and predictable |

---

### 2.5 Effect Systems (Koka, Eff, Unison)

**What they are:** The type of a function includes which side effects it may perform. Effects are tracked, controlled, and can be handled (similar to exceptions but first-class and typed).

```
// Koka-style
fun readFile(path: string) : <io,exn> string
// This function performs IO and may raise exceptions — encoded in the type
```

**How contracts are expressed:**
Effect types encode behavioral contracts about what a function *does* — not just what values it transforms. "This function is pure," "this function performs IO," "this function may fail with these error types." Effect handlers allow callers to decide how effects are interpreted.

**Strengths for Clank:**
- Encodes purity and side-effect contracts directly in signatures — critical for agent reasoning
- Effect handlers unify error handling, async, state, and IO under one mechanism (Principle 3: simplicity)
- Excellent inference: Koka infers effects automatically in most cases
- Enables local reasoning: if a function's type says it's pure, it IS pure
- Well-suited to agent verification: an agent can check effect constraints without running code
- Moderate implementation complexity — Koka demonstrates this is buildable

**Weaknesses for Clank:**
- Effect types describe *what side effects occur*, not value-level invariants
- Cannot alone express "this integer is positive" or "this list is sorted"
- Row-based effect typing adds type system complexity
- Effect polymorphism (functions generic over effects) requires careful design
- Still relatively new — fewer engineers have built effect system implementations

**Precedents:** Koka (Microsoft Research) is the most mature effect-typed language. Eff is a research language. Unison tracks effects. OCaml 5 added effect handlers (though not typed effects). Effekt is a newer research language exploring effect system design.

| Criterion | Rating | Notes |
|-----------|--------|-------|
| Contract Expressiveness | **3** | Effect contracts are powerful; value-level contracts absent |
| Syntax Cost | **4** | Effects are inferred in most cases; explicit when needed |
| Inference Power | **5** | Koka-style effect inference is excellent |
| Implementation Complexity | **3** | Nontrivial but demonstrated to be practical (Koka) |
| Agent Comprehension | **5** | Effect annotations are structured, predictable, machine-readable |

---

## 3. Comparison Matrix

| Approach | Contract Expressiveness | Syntax Cost | Inference Power | Implementation Complexity | Agent Comprehension | **Weighted Total** |
|----------|:---:|:---:|:---:|:---:|:---:|:---:|
| **Dependent Types** | 5 | 2 | 2 | 1 | 3 | 13 |
| **Refinement Types** | 4 | 4 | 4 | 2 | 5 | 19 |
| **Linear Types** | 2 | 3 | 4 | 3 | 4 | 16 |
| **Session Types** | 2 | 3 | 2 | 3 | 4 | 14 |
| **Effect Systems** | 3 | 4 | 5 | 3 | 5 | 20 |

(Totals are unweighted sums; see recommendation for priority-weighted analysis.)

---

## 4. Hybrid Approaches — What Existing Languages Combine

No production language uses just one of these systems. The interesting question for Clank is which combination gives the best tradeoff:

| Combination | Example | Tradeoff |
|-------------|---------|----------|
| Dependent + Linear | ATS | Maximum power, maximum complexity |
| Refinement + Effects | F* | Strong contracts + effect tracking; SMT dependency |
| Linear + Effects | Rust + hypothetical | Resource safety + effect clarity; no value contracts |
| Refinement + Linear + Effects | — (novel) | Best coverage; implementation cost is the sum of parts |
| Simplified Refinement + Effects | — (proposed for Clank) | Practical sweet spot; see recommendation |

---

## 5. Analysis Against Clank's Principles

### Principle 2: "Strongly typed with enforced contracts"
- **Dependent types** satisfy this maximally but at extreme cost
- **Refinement types** satisfy this for practical contracts with automatic verification
- **Linear types** satisfy this only for resource contracts
- **Effect systems** satisfy this for behavioral/purity contracts
- **Best fit:** Refinement types + effect system covers value contracts AND behavioral contracts

### Principle 3: "Simple — genuinely simple"
- **Dependent types** fail here: proof terms, universe hierarchies, totality checking
- **Refinement types** are an annotation layer — simple if the predicate language is constrained
- **Linear types** are simple conceptually but Rust-style lifetimes add complexity
- **Effect systems** are simple if the effect row algebra is kept small
- **Best fit:** Refinement types (with a restricted predicate language) + effects (with inference)

### "Language spec completeable in one context window"
This is the decisive constraint. Full dependent types cannot fit in one context window — the theory alone fills multiple papers. Refinement types with a fixed predicate language and an effect system with a small set of built-in effects CAN fit in one context window.

---

## 6. Recommendation

### Primary: Refinement Types + Algebraic Effect System

Clank should combine:

1. **A refinement type system** with a deliberately restricted predicate language:
   - Integer arithmetic comparisons (`x > 0`, `len(xs) == len(ys)`)
   - Simple logical connectives (`&&`, `||`, `!`)
   - Uninterpreted function symbols for user-defined abstractions
   - NO quantifiers, NO recursive predicates — keeps the logic decidable and SMT-friendly
   - Refinements are optional annotations on a standard Hindley-Milner base

2. **An algebraic effect system** (Koka-inspired):
   - Effects inferred by default, annotated when needed
   - Small built-in effect set: `io`, `exn`, `state`, `async`
   - User-defined effects for domain-specific contracts
   - Effect handlers for controlling interpretation

3. **Affine types** (simplified linear types) for resource management:
   - Move semantics by default (like Rust) but WITHOUT lifetime annotations
   - Values are used at most once unless explicitly cloned
   - No borrow checker — simpler model at the cost of more explicit cloning
   - Sufficient for resource safety; avoids Rust's steepest complexity cliff

### Why this combination:

| What it covers | Mechanism |
|----------------|-----------|
| "This integer is positive" | Refinement type: `{v: Int \| v > 0}` |
| "This function is pure" | Effect type: `() -> <> Int` (empty effect row) |
| "This function may fail" | Effect type: `() -> <exn> Int` |
| "This handle must be consumed" | Affine type: move semantics |
| "Input and output lists have same length" | Refinement: `{v: List a \| len(v) == len(xs)}` |

This covers the vast majority of practical contracts while remaining:
- **Decidable** (SMT-solvable predicates + effect row unification)
- **Inferable** (HM inference for base types, liquid inference for refinements, row inference for effects)
- **Specifiable** (fits in one context window)
- **Implementable** (each component has proven implementations)

### What this does NOT cover (and why that's OK):

- **Arbitrary dependent types** (e.g., "this binary tree is balanced"): These require proof terms. For Clank v1, such properties should be checked by runtime assertions or test-time verification, not the type system. This avoids the proof-term authoring burden.
- **Session types**: If Clank adds channel-based concurrency, session types can be layered on later as a library-level encoding over effects. Not needed in the core.

### Suggested syntax sketch:

```
-- Function with refinement and effect annotations
div : (n: Int, d: {Int | > 0}) -> <> Int

-- Function with inferred effects (IO inferred from body)
readConfig : (path: Str) -> <io, exn> Config

-- Affine resource: File must be consumed
open : (path: Str) -> <io, exn> File
close : (f: File) -> <io> ()

-- Refinement alias for reuse
type Pos = {Int | > 0}
type NonEmpty a = {List a | len > 0}
```

### Implementation path:

1. Start with Hindley-Milner type inference (well-understood, many implementations)
2. Add algebraic effects with row polymorphism (follow Koka's design)
3. Add refinement types with SMT integration (follow Liquid Types paper)
4. Add affine/move semantics (simpler than Rust — no lifetimes)

Each layer is independently useful and testable. An agent can use Clank productively after step 1 and gain contract power incrementally.

---

## 7. Key References

- **Liquid Types** (Rondon, Kawaguchi, Jhala, 2008) — foundational paper on refinement type inference via SMT
- **Koka** (Leijen, 2014–present) — practical algebraic effect system with inference
- **Idris 2** (Brady, 2021) — practical dependent types; useful as a ceiling reference
- **F*** (Swamy et al., 2016) — combines refinement types, effects, and proofs
- **Rust ownership** — affine types in practice; lessons on what to simplify
- **Liquid Haskell** (Vazou et al., 2014) — refinement types retrofitted onto a real language
