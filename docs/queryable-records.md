# Queryable Data Structures for Clank — Research Spec

**Task:** TASK-015
**Mode:** Research
**Status:** Complete
**Date:** 2026-03-14

---

## 1. Problem Statement

DECISION-001 hypothesizes that a **typed queryable record/map with built-in
metadata** — queryable by field type, interface, and tag — would let agents
navigate data structures without loading documentation into context.

Current Clank records (`{k: T}`) are simple structural types with no metadata,
no query mechanism, and no way to express "any record containing field X of
type Y." An agent encountering an unfamiliar record must read its definition
and all downstream usage to understand it. This contradicts the LLM-first
design principle: **types should do the heavy lifting.**

---

## 2. Prior Art Survey

### 2.1 Structural Typing (TypeScript, Go)

**Mechanism:** Types are compatible based on structure, not declaration. A value
of type `{name: Str, age: Int}` satisfies any type requiring `{name: Str}`.

**Strengths:** Agents don't need to track nominal type hierarchies. If you can
see the shape, you know the compatibility.

**Weaknesses:** No metadata, no query mechanism. Structural compatibility is
passive — the agent still needs to inspect each type to discover what fields
exist. TypeScript's `keyof` and mapped types add query capability, but they're
complex type-level metaprogramming, not data-level queries.

**Relevance to Clank:** Clank's records are already structurally typed. This is
the baseline, not the solution.

### 2.2 Row Polymorphism (PureScript, OCaml objects, Ur/Web)

**Mechanism:** Records carry a *row variable* representing "the rest of the
fields." A function typed `{ name: Str | r } -> Str` accepts any record with
at least a `name` field, regardless of other fields.

```
# PureScript-style
getName : forall r. { name :: String | r } -> String
getName rec = rec.name
```

**Strengths:**
- Functions express exactly which fields they need — agents read the type
  signature, not the implementation
- Extensible: new fields don't break existing code
- Row variables compose: `{ a: Int | r } & { b: Str | r }` = "has both a and b"

**Weaknesses:**
- Row unification can produce complex inferred types that are hard to read
- No metadata/tag system — queries are limited to "has field X of type Y"

**Relevance to Clank:** **High.** Row polymorphism directly addresses the
"navigate without docs" problem for field-level access. This should be the
foundation.

### 2.3 TypeScript Utility Types and Mapped Types

**Mechanism:** Type-level operations that transform record types:
- `Pick<T, K>` — select fields
- `Omit<T, K>` — exclude fields
- `Record<K, V>` — construct from keys and value type
- `Partial<T>`, `Required<T>` — toggle optionality
- Mapped types: `{ [K in keyof T]: F<T[K]> }` — transform each field
- Conditional types: `T extends U ? A : B` — type-level conditionals
- `Extract<T, U>` — filter union members matching U

**Strengths:** Extremely expressive type-level queries. An agent can express
"all fields of T whose type extends Serializable" as a type.

**Weaknesses:** Turing-complete type-level computation is a complexity trap.
TypeScript's type system is famously hard to reason about at the edges.
LLMs struggle with complex conditional/mapped type chains.

**Relevance to Clank:** Cherry-pick the simple, high-value operations (Pick,
Omit, fields-by-type). Reject the Turing-complete type-level programming.

### 2.4 Datalog-Style Structural Queries (Datomic, DataScript, Alloy)

**Mechanism:** Data is stored as facts (entity, attribute, value). Queries are
pattern-matching over relations:

```datalog
[:find ?field ?type
 :where [Config ?field ?type]
        [?type :kind :numeric]]
```

**Strengths:**
- Queries are declarative and composable
- Natural fit for "find all fields matching X"
- Schema-as-data: the structure itself is queryable

**Weaknesses:**
- Impedance mismatch with static type systems — Datalog operates on runtime
  facts, not compile-time types
- Unfamiliar syntax for most developers and LLMs
- Heavy runtime overhead

**Relevance to Clank:** The *concept* of schema-as-queryable-data is valuable,
but the Datalog execution model doesn't fit a statically typed compiled
language. Borrow the idea, not the mechanism.

### 2.5 Clojure Spec and Schema Libraries

**Mechanism:** Data shapes described by predicates and specs:

```clojure
(s/def ::port (s/and int? #(< 0 % 65536)))
(s/def ::config (s/keys :req [::host ::port] :opt [::timeout]))
```

**Strengths:** Specs are data — queryable, composable, generatable. An agent
can ask "what keys does this spec require?" at runtime.

**Weaknesses:** Dynamic typing only. No compile-time guarantees. Verbose.

**Relevance to Clank:** Clank's refinement types already cover the predicate
aspect (`Int{> 0 && < 65536}`). The queryable spec concept is interesting but
is better served by row polymorphism + tags in a static context.

### 2.6 GraphQL Type System

**Mechanism:** Schema defines types with fields, and queries select exactly the
fields needed. Introspection is built in — agents can query the schema itself.

**Strengths:** Agents naturally navigate GraphQL APIs by querying the schema.
The "ask for exactly what you need" model is the gold standard for
agent-friendly data access.

**Weaknesses:** Purpose-built for API boundaries, not general computation.

**Relevance to Clank:** The *introspection* concept is directly applicable.
Records should be introspectable at compile time.

---

## 3. Synthesis: What Agents Actually Need

From the survey, three capabilities emerge that would let agents navigate data
without loading docs:

| Capability | What it solves | Prior art |
|-----------|---------------|-----------|
| **Row polymorphism** | "What fields does this need?" is answered by the type signature | PureScript, Ur/Web |
| **Field tags/annotations** | Semantic grouping — "all auth fields", "all metrics" | GraphQL directives, Java annotations |
| **Type-level field queries** | "Which fields are of type X?" answered at compile time | TypeScript mapped types (simplified) |

What agents do NOT need:
- Turing-complete type-level computation (too complex to generate reliably)
- Runtime reflection (conflicts with static typing philosophy)
- Datalog-style query engine (wrong abstraction level)

---

## 4. Proposed Design

### 4.1 Row Polymorphism for Records

Extend Clank's record types with row variables:

```
# Function that works on any record with a "name" field
get-name : ({name: Str | r}) -> <> Str
  = .name

# Works with any record that has both fields
full-name : ({first: Str, last: Str | r}) -> <> Str
  = .first " " .last cat cat
```

**Row variable `r`** represents "zero or more additional fields." The compiler
infers it. At call sites, the concrete record type is known.

**Syntax addition to grammar:**

```ebnf
field-list  = field { ',' field } [ '|' word ]  ;
```

The `| r` is optional. Without it, the record is closed (exact match required).

**Why this matters for agents:** An agent reading `get-name : ({name: Str | r})
-> <> Str` knows exactly what the function needs without reading the body. It
also knows it can pass *any* record with a `name: Str` field.

### 4.2 Field Tags (Annotations)

Add compile-time metadata annotations to record fields:

```
type Config = {
  @net host: Str,
  @net port: Int{> 0 && < 65536},
  @auth token: Str,
  @auth secret: Str,
  @tuning max-retries: Int{>= 0},
  @tuning timeout-ms: Int{> 0}
}
```

Tags are compile-time only — zero runtime cost. Multiple tags per field are
allowed: `@net @required host: Str`.

**Syntax addition:**

```ebnf
field       = { '@' word } word ':' type-expr ;
```

**Why this matters for agents:** An agent working on networking code can query
"fields tagged @net" and get `{host: Str, port: Int}` without reading the
full Config definition. Tags create semantic namespaces within records.

### 4.3 Type-Level Field Queries (Restricted)

A small, non-Turing-complete set of type-level operations:

```
# Select fields by tag
type NetConfig = Config @net          # {host: Str, port: Int}

# Select fields by type
type ConfigInts = Config : Int        # {port: Int, max-retries: Int, timeout-ms: Int}

# Pick/Omit (TypeScript-inspired)
type Minimal = Pick<Config, "host" | "port">
type Extended = Omit<Config, "secret">

# Combine with row polymorphism
log-config : (Config @net) -> <io> ()
  = ...   # only sees host and port
```

**Restricted to:**
- Tag projection: `T @tag` -> sub-record of tagged fields
- Type filtering: `T : U` -> sub-record of fields with type U
- Pick/Omit by field name
- No conditional types, no mapped types, no recursion

**Why restricted:** TypeScript's type system proves that Turing-complete
type-level computation is a footgun. LLMs generate unreliable code when type
manipulation is complex. Four operations cover the use cases; more would add
complexity without proportional value.

### 4.4 Introspection Word

A built-in word that exposes record structure to the program:

```
# fields: returns field names as a list of strings
Config fields    # => ["host", "port", "token", "secret", "max-retries", "timeout-ms"]

# fields-tagged: returns field names matching a tag
Config fields-tagged "net"    # => ["host", "port"]

# field-type: returns the type of a named field (as a type value)
Config field-type "port"      # => Int{> 0 && < 65536}
```

These are **compile-time evaluated** when the record type is statically known.
When the type is a row-polymorphic variable, they produce a compile error (you
can't introspect what you don't know).

**Open question:** Whether `fields` and `fields-tagged` should be compile-time
only or also available at runtime. Compile-time-only is simpler and sufficient
for the agent use case. Runtime introspection adds complexity and GC pressure.

**Recommendation:** Compile-time only for v1. If a runtime use case emerges,
it can be added later without breaking existing code.

---

## 5. Interaction with Existing Clank Features

### Refinement Types

Tags and refinements are orthogonal. A field can have both:

```
@net port: Int{> 0 && < 65536}
```

Tag projection preserves refinements: `Config @net` yields
`{host: Str, port: Int{> 0 && < 65536}}`.

### Effect System

Field queries are pure compile-time operations — no effect annotations needed.
The `fields` introspection word has effect `<>` (pure).

### Pattern Matching

Tag projections can be used in pattern contexts:

```
configure-net : (Config @net) -> <io> ()
  = match {
      {host, port} => host port connect
    }
```

### Module System

Tags are scoped to the defining module. `@net` in module A is distinct from
`@net` in module B unless explicitly re-exported. This prevents tag collision
across libraries.

---

## 6. Example: Agent-Friendly Record Navigation

An agent tasked with "add retry logic to the HTTP client" encounters:

```
type HttpConfig = {
  @conn base-url: Str,
  @conn timeout-ms: Int{> 0},
  @retry max-retries: Int{>= 0},
  @retry backoff-ms: Int{> 0},
  @auth api-key: Str
}
```

The agent can:
1. See `HttpConfig @retry` -> `{max-retries: Int{>= 0}, backoff-ms: Int{> 0}}`
2. Write a function that only takes what it needs:
   ```
   with-retry : ({max-retries: Int{>= 0}, backoff-ms: Int{> 0} | r}, [() -> <exn> a]) -> <> a
   ```
3. The type signature documents exactly what the function uses — no doc lookup
4. Row polymorphism means it works with HttpConfig AND any other config with
   retry fields

---

## 7. Evaluation of the Hypothesis

**Verdict: Accept, with scoping.**

The hypothesis is sound. A typed queryable record with metadata *does* let
agents navigate data without loading docs, provided:

1. **Row polymorphism** is the foundation (type signatures tell you what's needed)
2. **Tags** provide semantic grouping (agents query by domain, not by position)
3. **Type-level queries are restricted** (four operations, not Turing-complete)
4. **Introspection is compile-time** (no runtime reflection complexity)

The design adds approximately 5-8 grammar rules and stays well under the
100-rule target. It composes cleanly with refinement types, effects, and
pattern matching.

**Risks:**
- Row polymorphism inference can produce verbose inferred types (mitigate: require
  annotation at module boundaries, which Clank already does)
- Tag proliferation without conventions (mitigate: stdlib establishes standard
  tags; linter warns on unused tags)
- Scope creep toward TypeScript-style type gymnastics (mitigate: hard cap at
  four query operations in v1; no conditional types ever)

---

## 8. Recommendation

Adopt the design in three phases:

1. **Phase 1 (core-syntax update):** Add row polymorphism to records. This is
   the highest-value change and prerequisite for everything else.
2. **Phase 2 (field tags):** Add `@tag` annotation syntax. Define stdlib
   conventions for common tags.
3. **Phase 3 (type-level queries):** Add tag projection, type filtering,
   Pick/Omit. Add compile-time `fields` introspection.

Each phase is independently useful and can be spec'd/implemented as a separate
task.
