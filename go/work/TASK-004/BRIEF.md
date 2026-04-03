# TASK-004 — Port VM runtime to Go

**Task ID:** TASK-004
**Working directory:** work/TASK-004/
**Project root:** ../../
**Mode:** implementation
**Priority:** normal
**Created:** 2026-03-29T10:35:51Z
**Blocked by:** TASK-184

## Scope
Port ts/src/vm.ts (~530 lines) to go/internal/vm/. Execute bytecode produced by the compiler. Stack machine with tagged values, effect handler frames, builtin dispatch. Add VM unit tests.

## Out of Scope
Async VM opcodes, STM opcodes (can follow later)

## Context
ts/src/vm.ts:reference TS VM

## Expected Output
Go VM package executing bytecode, unit tests passing
