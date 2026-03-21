# Compilation Target Research: WASM vs LLVM IR vs Custom VM

**Task:** TASK-006
**Status:** Complete
**Date:** 2026-03-14

---

## Executive Summary

Clank should adopt a **two-tier strategy**: a custom stack-based bytecode VM as the primary development target, with WASM as the secondary/production target. LLVM IR should be deferred to a future phase.

This recommendation is driven by Clank's unique constraints: the toolchain must fit in an agent's context window, structured error output is mandatory, and incremental compilation is a first-class requirement.

---

## Evaluation Criteria

Each target is evaluated on seven axes derived from OVERVIEW.md and the language survey:

1. **Compile speed** — How fast from source to runnable code
2. **Runtime performance** — Execution speed of compiled output
3. **Sandboxing** — Built-in isolation and security guarantees
4. **Incremental compilation** — Can individual words/functions be recompiled independently
5. **Toolchain size** — Can the compiler source fit in an agent context window (~5000 tokens for spec, toolchain should be similarly compact)
6. **Structured error output** — Ease of emitting JSON-structured diagnostics
7. **Ecosystem interop** — Ability to call/be called by existing code

---

## Comparison Matrix

| Criterion | Custom VM | WASM | LLVM IR |
|-----------|-----------|------|---------|
| **Compile speed** | ★★★★★ | ★★★★ | ★★★ |
| **Runtime performance** | ★★★ | ★★★★ | ★★★★★ |
| **Sandboxing** | ★★★ | ★★★★★ | ★★ |
| **Incremental compilation** | ★★★★★ | ★★★ | ★★ |
| **Toolchain size** | ★★★★★ | ★★★ | ★ |
| **Structured error output** | ★★★★★ | ★★★★ | ★★★ |
| **Ecosystem interop** | ★★ | ★★★★★ | ★★★★ |

---

## Detailed Analysis

### Option A: Custom Stack-Based Bytecode VM

**What it is:** A purpose-built stack-based virtual machine with typed opcodes and a structured error channel.

**Strengths:**

- **Good alignment with Clank's execution model.** The applicative syntax compiles naturally to stack bytecode via left-to-right argument evaluation and CALL — the same strategy used by the JVM, CPython, and the CLR. There's no need to lower to register-based IR or SSA form.

- **Minimal toolchain size.** A bytecode VM can be implemented in ~1000-2000 lines. The entire compiler + VM source can fit in a single agent context load, which is a hard requirement from OVERVIEW.md.

- **Incremental compilation is native.** New functions compile independently. Redefining a function only recompiles that function's body. This maps directly to the agent workflow of "define, test, refine" described in the language survey.

- **Total control over error reporting.** Every phase of compilation and execution is under Clank's control. Structured JSON error output with source locations, stack-effect mismatches, and type errors can be emitted exactly as the agent-native tooling spec requires. No need to parse or transform errors from an external tool.

- **Fast compile times.** Bytecode compilation is straightforward — parse, type-check, emit. Compilation is O(n) in program size with small constants.

**Weaknesses:**

- **Performance ceiling.** An interpreter (even a well-optimized threaded-code or computed-goto interpreter) is 5-20x slower than native code. This matters for compute-heavy workloads but may not matter for Clank's primary use cases (agent-written utilities, data transformations, orchestration logic).

- **No ecosystem interop.** A custom VM can't call C libraries, system APIs, or existing code without building an FFI layer from scratch.

- **Must build everything.** Garbage collection (or Idris-style linear resource management), I/O, concurrency — all must be implemented. No free rides from an existing runtime.

**Mitigation:** The performance ceiling can be addressed later by adding a WASM or LLVM backend. The custom VM serves as the development and testing target; production deployment can use a more performant backend.

---

### Option B: WebAssembly (WASM)

**What it is:** Compile Clank to WASM bytecode, run on any WASM runtime (browser, Wasmtime, Wasmer, WasmEdge). WASM 3.0 (released September 2025) adds GC types, exception handling, and 64-bit memory.

**Strengths:**

- **Best-in-class sandboxing.** WASM provides memory-safe execution with no ambient access to the host. All I/O goes through explicitly imported capabilities (WASI). This is a "deny-by-default" security model — ideal for running agent-generated code safely.

- **Strong ecosystem interop.** WASM modules can import/export functions across language boundaries. WASI provides standardized system interfaces. Any language that compiles to WASM can interop with Clank.

- **Good runtime performance.** WASM runtimes use JIT or AOT compilation to native code. Performance is typically within 1.5-2x of native C for compute workloads. Cold start times are measured in microseconds.

- **Portability.** Runs everywhere — browsers, servers, edge, embedded. One compilation target, universal deployment.

- **WASM 3.0 GC support.** The new GC proposal means Clank wouldn't need to implement its own garbage collector if it uses WASM's struct/array types. This is significant — GC is one of the hardest parts of a runtime.

**Weaknesses:**

- **Stack-machine mismatch is partial.** WASM IS a stack machine, but its stack is a *validation* stack — it enforces structured control flow (blocks, loops, if/else), not arbitrary stack manipulation. Clank's data stack semantics would need to be mapped through WASM locals or explicit memory, not WASM's operand stack directly. This adds a compilation layer.

- **Incremental compilation is awkward.** WASM modules are the unit of compilation. You can't easily recompile a single function and patch it into a running module. Workarounds exist (dynamic linking, multiple modules) but add complexity.

- **Toolchain is external.** The WASM runtime is a separate binary (Wasmtime is ~30MB). The compiler must emit valid WASM binary format, which requires a WASM encoder library or manual binary emission. This is more complex than emitting custom bytecode.

- **Error reporting indirection.** Compile-time errors are still under Clank's control, but runtime errors come from the WASM runtime, not from Clank. Trapping, stack overflow, and type errors surface as WASM-level diagnostics that must be mapped back to Clank source locations.

- **Structured control flow requirement.** WASM requires structured control flow (no arbitrary gotos). While this is fine for most code, it means tail-call optimization and certain control flow patterns require the tail-call proposal (now in WASM 3.0) or CPS transformation.

---

### Option C: LLVM IR

**What it is:** Emit LLVM IR from the Clank compiler, then use LLVM's optimization passes and code generators to produce native machine code for any supported architecture.

**Strengths:**

- **Best runtime performance.** LLVM's optimization pipeline (decades of engineering) produces code competitive with hand-tuned C. For compute-heavy Clank programs, this is unbeatable.

- **Mature multi-architecture support.** x86, ARM, RISC-V, WASM (via LLVM's WASM backend) — one compiler frontend, many targets.

- **Battle-tested.** Swift, Rust, Clang, Julia, and dozens of other production languages use LLVM. The backend is extremely well-tested.

**Weaknesses:**

- **Massive toolchain size.** This is the dealbreaker for Clank. LLVM libraries are tens of megabytes. The LLVM source is millions of lines of C++. Even a minimal LLVM dependency adds enormous weight. An agent cannot reason about or carry the LLVM toolchain in context. The Clank compiler would become a thin frontend to an opaque, enormous backend.

- **Impedance mismatch.** LLVM IR is SSA-form register-based IR. Translating from a stack-based VM to SSA form requires additional lowering work — stack operations must be converted to register assignments, phi nodes must be inserted at control flow joins.

- **Poor incremental compilation.** LLVM modules are compiled as a unit. LTO (link-time optimization) operates on the whole program. There is no built-in support for "recompile one function and patch it in."

- **Slow compilation.** LLVM optimization passes are thorough but not fast. Debug builds are faster, but optimized builds can take seconds per module. For an agent doing rapid define-test-refine cycles, this latency is significant.

- **Error reporting is a translation problem.** LLVM's diagnostic infrastructure is designed for C/C++/Rust-style languages. Mapping Clank's semantics through LLVM's error model requires significant adapter code.

---

## Recommendation: Custom VM (primary) + WASM (secondary)

### Phase 1: Custom Stack VM (implement first)

Build a purpose-designed stack VM for Clank:

- **Instruction set:** Direct opcodes for stack operations, arithmetic, function calls, locals, closures, and type-checked operations. Each Clank function compiles to a bytecode sequence.
- **Dictionary model:** Functions compile to dictionary entries. Redefining a function only recompiles that entry. Incremental by design.
- **Structured error channel:** Every error (compile-time type mismatch, stack underflow, contract violation) emits a JSON diagnostic with source location, expected vs actual types/stack effects, and suggested fix.
- **Sandboxing:** Implement capability-based I/O — the VM provides no ambient access. All side effects go through explicitly provided capability tokens. Simpler than WASM's WASI but follows the same principle.
- **Implementation budget:** Target ~1500 lines for VM + compiler. This fits in 1-2 agent context loads.

**Why first:** The custom VM gives Clank the fastest path to a working, testable language. Agents can write Clank programs, compile them in milliseconds, get structured errors, and iterate. The entire toolchain is inspectable and modifiable by agents. This is the "agent-native" experience described in OVERVIEW.md.

### Phase 2: WASM Backend (implement second)

Add a WASM code generation backend alongside the custom VM:

- **Use case:** Production deployment, ecosystem interop, browser execution, sandboxed cloud execution.
- **Approach:** Compile Clank's typed IR (intermediate representation from Phase 1's compiler frontend) to WASM binary format. The data stack maps to WASM linear memory or locals. Function calls map to WASM function calls.
- **Leverage WASM 3.0 GC:** If Clank uses managed types, WASM 3.0's GC types avoid building a custom collector for the WASM backend.
- **Incremental compilation workaround:** Use WASM dynamic linking (multiple modules) for development, single-module compilation for production.

**Why second:** WASM adds deployment flexibility and interop but doesn't help with the rapid development loop agents need. It's a production optimization, not a development necessity.

### Phase 3: LLVM Backend (defer)

Only pursue if Clank programs have proven compute-performance bottlenecks that the WASM JIT can't handle. The toolchain size cost is only justified by demonstrated need.

---

## Rationale Summary

| Decision | Rationale |
|----------|-----------|
| Custom VM first | Toolchain fits in context, natural compilation target for applicative syntax, fastest iteration loop, total control over error output |
| WASM second | Best sandboxing story, ecosystem interop, portable deployment. WASM 3.0 GC reduces implementation burden |
| LLVM deferred | Toolchain too large for agent context, SSA mismatch with stack VM, poor incremental compilation. Performance gains don't justify cost at this stage |
| Not WASM-only | Incremental compilation friction and runtime error indirection make WASM a poor primary development target |
| Not custom-VM-only | No ecosystem interop path; WASM needed for production deployment |

---

## Key Design Constraint: Agent Context Budget

The single most differentiating constraint is **toolchain size**. From OVERVIEW.md: "The language spec should be completeable in one context window." The toolchain should follow the same principle.

| Target | Compiler source size (estimated) | Fits in agent context? |
|--------|--------------------------------|----------------------|
| Custom VM | ~1,500 lines | Yes — single load |
| WASM encoder | ~3,000-5,000 lines (with binary format handling) | Yes — 2-3 loads |
| LLVM binding | ~500 lines of bindings + millions of lines of LLVM | No — opaque dependency |

This constraint alone is nearly sufficient to justify the recommendation. An agent that cannot read, understand, and modify its own compiler is not truly "agent-native."

---

## Open Questions for Follow-up

1. **VM instruction set design:** What opcodes does the custom VM need? This should be driven by the core language spec (TASK pending).
2. **WASM data stack mapping:** Should the Clank data stack map to WASM linear memory (simpler, slower) or WASM locals (faster, harder to compile)?
3. **Capability model design:** How should the VM's capability-based I/O model work? Should it mirror WASI or diverge?
4. **JIT for the custom VM:** If Phase 1 performance is insufficient, should the custom VM get a simple JIT before reaching for WASM/LLVM? (A basic method-JIT for hot functions could close much of the performance gap.)
