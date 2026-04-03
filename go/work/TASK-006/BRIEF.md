# TASK-006 — Sync Go rewrite to git repo, restructure per DECISION-003

**Task ID:** TASK-006
**Working directory:** work/TASK-006/
**Project root:** ../../
**Mode:** implementation
**Priority:** high
**Created:** 2026-03-30T07:43:11Z

## Scope
1. Copy consolidated Go code from cowork go/ to ~/code/clank/go/ (the actual git repo). 2. Move ~/code/clank/src/ to ~/code/clank/ts/ per DECISION-003 (preserve TS reference impl). 3. Update any path references (package.json, tsconfig, test runner, integration_test.go test dir path). 4. Commit the restructured repo. 5. Remove stale Go copies from deliverables/go/ in cowork dir to prevent worker confusion.

## Out of Scope
New Go features, modifying Go code logic, touching plan/ files. Do NOT delete work/ directories (they may still be needed for TASK-182 merge).

## Context
work/TASK-178/OUTPUT.md:consolidation details, go/:canonical Go source, ~/code/clank/:target git repo

## Expected Output
Git repo at ~/code/clank/ with go/ and ts/ directories per DECISION-003 architecture. All existing tests still pass. Stale Go copies removed from deliverables/.
