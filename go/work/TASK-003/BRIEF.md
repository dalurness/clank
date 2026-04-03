# TASK-003 — Port bytecode compiler to Go

**Task ID:** TASK-003
**Working directory:** work/TASK-003/
**Project root:** ../../
**Mode:** implementation
**Priority:** normal
**Created:** 2026-03-29T10:35:48Z

## Scope
Port ts/src/compiler.ts (~500 lines) to go/internal/compiler/. Compile AST to bytecode instructions matching the TS compiler's output. Register compiler in CLI. Add compiler unit tests.

## Out of Scope
VM runtime (separate task), optimization passes

## Context
ts/src/compiler.ts:reference TS compiler

## Expected Output
Go compiler package producing bytecode, unit tests passing
