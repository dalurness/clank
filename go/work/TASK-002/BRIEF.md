# TASK-002 — Implement Ord/cmp, Default, From/Into dispatch in Go evaluator

**Task ID:** TASK-002
**Working directory:** work/TASK-002/
**Project root:** ../../
**Mode:** implementation
**Priority:** high
**Created:** 2026-03-29T10:35:44Z

## Scope
Add runtime dispatch for: (1) cmp method for Ord interface on variants, records, lists, tuples; (2) default function for Default interface; (3) from/into functions with blanket impl forwarding. Reference TS implementation in ts/src/eval.ts.

## Out of Scope
Type checker changes, new interfaces

## Context
go/internal/eval/eval.go:evaluator

## Expected Output
All 12 currently-failing phase6 integration tests pass (18d/e/f/j/o/p, 20a-e and 15-ord)
