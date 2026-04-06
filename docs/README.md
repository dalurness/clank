# Clank Documentation

These documents describe the design and specification of Clank language features. They were written during the design phase and may reference features or implementation details that have since changed.

## Status Key

- **Implemented** — Feature exists in the Go codebase and is tested
- **Spec only** — Design document exists but feature is not yet implemented
- **Removed** — Feature was removed from the language

## Feature Documents

| Document | Feature | Status |
|----------|---------|--------|
| [core-syntax.md](core-syntax.md) | Language syntax and grammar | Implemented |
| [effect-system.md](effect-system.md) | Algebraic effects and handlers | Implemented |
| [effect-aliases.md](effect-aliases.md) | Effect alias declarations | Implemented |
| [effect-subtraction.md](effect-subtraction.md) | Effect row subtraction | Implemented |
| [affine-types.md](affine-types.md) | Affine/linear type enforcement | Implemented |
| [refinement-types.md](refinement-types.md) | Refinement type predicates | Implemented |
| [interface-system.md](interface-system.md) | Interfaces and deriving | Implemented |
| [row-polymorphism.md](row-polymorphism.md) | Record row polymorphism | Implemented |
| [module-system.md](module-system.md) | Module imports and visibility | Implemented |
| [for-in-syntax.md](for-in-syntax.md) | For-in loop syntax | Implemented |
| [range-literal-syntax.md](range-literal-syntax.md) | Range literal syntax | Implemented |
| [operator-precedence.md](operator-precedence.md) | Operator precedence rules | Implemented |
| [type-system.md](type-system.md) | Type system overview | Implemented |
| [stdlib-catalog.md](stdlib-catalog.md) | Standard library catalog | Partially implemented (see status table in file) |
| [vm-instruction-set.md](vm-instruction-set.md) | VM opcode specification | Implemented (opcode count may differ from spec) |
| [gc-strategy.md](gc-strategy.md) | Garbage collection strategy | Spec only |
| [async-runtime.md](async-runtime.md) | Async runtime design | Implemented (core), spec only (advanced scheduling) |
| [stm-runtime.md](stm-runtime.md) | Software transactional memory | Implemented |
| [shared-mutable-state.md](shared-mutable-state.md) | Ref cells and mutable state | Implemented |
| [streaming-io.md](streaming-io.md) | Streaming I/O and iterators | Partially implemented |
| [package-management.md](package-management.md) | Package manager design | Implemented (local deps), spec only (registry) |
| [tooling-spec.md](tooling-spec.md) | CLI tooling specification | Implemented |
| [pretty-print-layer.md](pretty-print-layer.md) | Terse/verbose transformation | Implemented |
| [queryable-records.md](queryable-records.md) | Record field tags and queries | Spec only |
| [compilation-strategy.md](compilation-strategy.md) | Compilation approach | Implemented (bytecode VM) |
| [compilation-target.md](compilation-target.md) | WASM target design | Spec only |
| [workspace-orchestration.md](workspace-orchestration.md) | Multi-package workspaces | Spec only |
| [local-deps-workspace.md](local-deps-workspace.md) | Local dependency resolution | Implemented |
| [implementation-plan.md](implementation-plan.md) | Phased implementation plan | Historical |
| [canonical-examples.md](canonical-examples.md) | Example programs | Reference |
| [language-survey.md](language-survey.md) | Comparison with other languages | Reference |

## For Agents

If you are an AI agent learning Clank, read these files in order:
1. **[../SPEC.md](../SPEC.md)** — Complete language specification (~3500 tokens)
2. **[stdlib-catalog.md](stdlib-catalog.md)** — Standard library reference (check implementation status table at top)
3. **[canonical-examples.md](canonical-examples.md)** — Example programs

> **Note:** FFI/extern has been removed from the language. The `ffi-interop.md` document has been deleted.
> The TypeScript implementation has been archived. All references to `src/`, `.ts` files, or "TypeScript implementation" in these documents refer to the historical prototype.
