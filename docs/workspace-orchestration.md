# Workspace Build Orchestration & Cross-Member Testing Specification

**Task:** TASK-087
**Mode:** Spec
**Date:** 2026-03-19
**Dependencies:** plan/features/local-deps-workspace.md, plan/features/tooling-spec.md, plan/features/package-management.md, plan/features/compilation-strategy.md
**Extends:** Local Path Dependencies & Workspace Support Specification (TASK-083)

---

## 1. Overview

This spec extends the workspace system (TASK-083 §3) with build orchestration: how Clank determines build order across workspace members, executes builds in parallel, supports incremental rebuilds, discovers the workspace root, and runs tests across all members.

### Design Goals

1. **Correct by construction** — build order respects the inter-member dependency DAG; no member compiles before its dependencies
2. **Maximally parallel** — independent members build concurrently without coordination
3. **Incremental** — only rebuild what changed, using signature stability (tooling-spec §5.2)
4. **Workspace-wide testing** — `clank test --all` runs every member's tests with correct dependency ordering
5. **Discoverable root** — any `clank` command works from any directory inside a workspace

---

## 2. Workspace Root Discovery

### 2.1 Algorithm

When any `clank` command runs, the CLI resolves the project context by walking the directory tree upward from the current working directory:

1. Look for `clank.workspace` in the current directory
2. If not found, look for `clank.pkg` in the current directory
3. Walk to the parent directory and repeat from step 1
4. Stop at the filesystem root

**Resolution rules:**

| Found | Context |
|-------|---------|
| `clank.workspace` first | Workspace mode — the directory containing `clank.workspace` is the workspace root |
| `clank.pkg` first (no `clank.workspace` above it) | Single-package mode — standard behavior |
| `clank.pkg` first, then `clank.workspace` above it | Workspace mode — continue walking to find the workspace root; the `clank.pkg` identifies the "nearest member" |
| Neither found | Error E518: "no clank.pkg or clank.workspace found in directory tree" |

### 2.2 Nearest Member

When running from inside a workspace, the CLI tracks which member directory the user is in. This is the **nearest member** — commands that operate on a single member default to it.

```
my-project/              ← clank.workspace (workspace root)
  packages/
    core/                ← clank.pkg
      src/
        parser/          ← cwd here: nearest member = "core"
    web/                 ← clank.pkg
```

If the cwd is the workspace root itself (or a non-member subdirectory), there is no nearest member. Commands requiring a target member emit error E519 unless `--package` or `--all` is specified.

### 2.3 The `--project` Override

The `--project` global flag (tooling-spec §2.1) overrides root discovery. If `--project` points to a directory containing `clank.workspace`, workspace mode activates. If it points to a directory containing `clank.pkg` (but no `clank.workspace` above), single-package mode activates.

### 2.4 Output

Root discovery results are available via:

```bash
clank pkg workspace info
```

```json
{
  "ok": true,
  "data": {
    "root": "/home/user/my-project",
    "workspace_file": "/home/user/my-project/clank.workspace",
    "nearest_member": "core",
    "nearest_member_path": "packages/core",
    "members": ["core", "web", "cli", "shared-types"]
  }
}
```

---

## 3. Dependency Graph Construction

### 3.1 Inter-Member Dependency DAG

Before any build or test operation, the CLI constructs the **workspace dependency graph**:

1. Parse `clank.workspace` to get the member list (expanding globs per TASK-083 §3.2)
2. For each member, parse its `clank.pkg` to collect `[deps]` and `[dev-deps]`
3. For each dependency name, check if it matches another workspace member name
4. Build a directed graph: edge from A to B means "A depends on B"
5. Run cycle detection — cycles are error E512 (same as TASK-083 §5.3)

The resulting DAG is used for build ordering, parallel scheduling, and incremental invalidation.

### 3.2 Graph Representation

The graph is computed on every invocation (it's cheap — just manifest parsing). It is not cached to disk, ensuring it always reflects the current state of manifests.

```json
{
  "members": {
    "core":         { "deps_on_members": [],              "depended_by": ["web", "cli"] },
    "shared-types": { "deps_on_members": [],              "depended_by": ["core", "web"] },
    "web":          { "deps_on_members": ["core"],        "depended_by": [] },
    "cli":          { "deps_on_members": ["core"],        "depended_by": [] }
  }
}
```

### 3.3 Topological Sort

Build order is a topological sort of the DAG. Members with no inter-member dependencies (leaves) come first. When multiple valid orderings exist, the sort is stable (alphabetical tie-breaking) for reproducibility.

For the example above, one valid order: `shared-types`, `core`, `cli`, `web`.

---

## 4. Build Orchestration

### 4.1 `clank build` in Workspace Context

| Command | Behavior |
|---------|----------|
| `clank build` | Build the nearest member and its workspace dependencies (transitively) |
| `clank build --all` | Build all workspace members in dependency order |
| `clank build --package web` | Build `web` and its workspace dependencies |
| `clank build --package web --no-deps` | Build only `web` (assume dependencies are up-to-date) |

All variants respect the dependency DAG — a member is never compiled before its workspace dependencies.

### 4.2 Parallel Build Execution

Members at the same depth in the DAG (no dependencies between them) build in parallel. The scheduler uses a **task pool** model:

1. Compute the topological sort with depth levels
2. Start all depth-0 members (no inter-member deps) in parallel
3. When a member completes, check if any new members have all their dependencies satisfied
4. Start newly-unblocked members immediately
5. Continue until all members are built or an error occurs

**Concurrency control:**

| Flag | Default | Description |
|------|---------|-------------|
| `--jobs N` | number of CPU cores | Maximum concurrent member builds |
| `--jobs 1` | — | Sequential build (for debugging) |

### 4.3 Build Output

```bash
clank build --all
```

```json
{
  "ok": true,
  "data": {
    "members_built": [
      {
        "name": "shared-types",
        "path": "libs/shared-types",
        "output": ".clank/build/shared-types/shared-types.clkb",
        "modules_compiled": 3,
        "cached": false
      },
      {
        "name": "core",
        "path": "packages/core",
        "output": ".clank/build/core/core.clkb",
        "modules_compiled": 12,
        "cached": false
      },
      {
        "name": "cli",
        "path": "packages/cli",
        "output": ".clank/build/cli/cli.clkb",
        "modules_compiled": 5,
        "cached": true
      },
      {
        "name": "web",
        "path": "packages/web",
        "output": ".clank/build/web/web.clkb",
        "modules_compiled": 8,
        "cached": false
      }
    ],
    "build_order": ["shared-types", "core", ["cli", "web"]],
    "parallelism_achieved": 2
  },
  "diagnostics": [],
  "timing": {
    "total_ms": 340,
    "per_member": {
      "shared-types": 45,
      "core": 120,
      "cli": 5,
      "web": 170
    }
  }
}
```

The `build_order` field shows the execution plan. Arrays within the array indicate parallel groups: `["cli", "web"]` means both built concurrently after `core` completed.

### 4.4 Error Handling During Parallel Builds

When a member build fails:

1. **Fail-fast (default):** Cancel all in-progress builds. Report the failure. Members that depend on the failed member are skipped with status `"skipped_dep_failed"`.
2. **`--keep-going`:** Continue building members that don't depend on the failed member. Collect all errors and report at the end.

```json
{
  "ok": false,
  "data": {
    "members_built": [
      { "name": "shared-types", "status": "success" },
      { "name": "core", "status": "failed" },
      { "name": "web", "status": "skipped_dep_failed", "blocked_by": "core" },
      { "name": "cli", "status": "skipped_dep_failed", "blocked_by": "core" }
    ]
  },
  "diagnostics": [
    {
      "severity": "error",
      "code": "E301",
      "phase": "check",
      "message": "type mismatch in packages/core/src/parser.clk:42",
      "location": { "file": "packages/core/src/parser.clk", "line": 42, "col": 10 }
    }
  ]
}
```

### 4.5 Build Artifacts Layout

Per TASK-083 §3.7, workspace members share a single `.clank/` directory at the workspace root. Build artifacts are organized per-member:

```
.clank/
  build/
    shared-types/
      shared-types.clkb           # bytecode bundle
      modules/
        types.clkb                # per-module bytecode
    core/
      core.clkb
      modules/
        parser.clkb
        types.clkb
    web/
      web.clkb
      modules/
        ...
  cache/
    check/
      <hash>.json                 # type-check cache (shared across members)
    signatures/
      shared-types.sig.json       # exported signature snapshot
      core.sig.json
      web.sig.json
```

The `signatures/` directory stores each member's public signature (exported types + effect rows). This is the key to incremental rebuilds (§5).

---

## 5. Incremental Rebuilds

### 5.1 Change Detection

When `clank build` runs in a workspace, the build system determines what needs rebuilding:

1. **Source hash check:** For each member, hash all source files under its `src/` directory. Compare against the stored hash from the last build.
2. **Signature stability check:** If a dependency member was rebuilt, compare its new exported signature against the cached signature. If unchanged, downstream members skip rebuilding.
3. **External dep check:** If `clank.lock` changed (external dep update), affected members are rebuilt.

### 5.2 Signature Stability

This extends tooling-spec §5.2 to the workspace level. The key insight: if member `core` changes internally but its public signature (exported function types, type definitions, effect declarations) remains identical, members that depend on `core` do not need recompilation.

**Signature file contents:**

```json
{
  "member": "core",
  "version": "0.1.0",
  "signature_hash": "sha256:abc123...",
  "exports": {
    "functions": [
      { "module": "core.parser", "name": "parse", "type": "(Str) -> <exn[ParseErr]> AST" }
    ],
    "types": [
      { "module": "core.types", "name": "AST", "kind": "variant", "hash": "sha256:def456..." }
    ],
    "effects": [
      { "module": "core", "name": "ParseErr", "hash": "sha256:789abc..." }
    ]
  }
}
```

**Rebuild decision matrix:**

| Member source changed? | Dependency signature changed? | Action |
|------------------------|------------------------------|--------|
| No | No | Skip (fully cached) |
| No | Yes | Rebuild (dependency contract changed) |
| Yes | No | Rebuild member only; check if own signature changed |
| Yes | Yes | Rebuild member and potentially propagate |

### 5.3 Incremental Build Output

```json
{
  "ok": true,
  "data": {
    "members_built": [
      { "name": "core", "cached": false, "reason": "source_changed", "signature_changed": false },
      { "name": "web", "cached": true, "reason": "dependency_signature_stable" },
      { "name": "cli", "cached": true, "reason": "dependency_signature_stable" }
    ]
  },
  "timing": { "total_ms": 130 }
}
```

The `reason` field tells the agent why each member was or wasn't rebuilt — critical for debugging build performance.

### 5.4 Cache Invalidation

The build cache is invalidated when:

- **Always:** Clank compiler version changes (full rebuild)
- **Per-member:** Source files change (content hash mismatch)
- **Transitive:** Dependency member's signature changes
- **Global:** `clank.lock` changes (external dep version shift)
- **Manual:** `clank build --clean` deletes the `.clank/build/` and `.clank/cache/` directories

```bash
clank build --clean              # clean all members
clank build --clean --package core  # clean one member (and invalidate dependents)
```

---

## 6. Cross-Member Testing

### 6.1 `clank test` in Workspace Context

| Command | Behavior |
|---------|----------|
| `clank test` | Run tests for the nearest member |
| `clank test --all` | Run tests for all workspace members |
| `clank test --package web` | Run tests for `web` only |
| `clank test --all --filter "parse"` | Run tests matching "parse" across all members |

### 6.2 Test Execution Order

Tests respect the same dependency DAG as builds. A member's tests run only after:

1. The member itself is built successfully
2. All workspace dependencies are built successfully

Test execution order follows the same parallel scheduling as builds (§4.2):

1. Build all members (in dependency order, with parallelism)
2. Run tests for depth-0 members in parallel
3. As dependency members' tests pass, run dependent members' tests
4. Collect all results

**Why order tests by dependency?** If `core`'s tests fail, `web`'s tests (which depend on `core`) are likely to fail for the same reason. Running in order surfaces the root cause first.

The `--no-dep-tests` flag skips this ordering and runs only the specified member's tests (assuming dependencies are correct):

```bash
clank test --package web --no-dep-tests   # run web's tests only, skip core's tests
```

### 6.3 Test Output (Workspace-Wide)

```bash
clank test --all
```

```json
{
  "ok": false,
  "data": {
    "members": [
      {
        "name": "shared-types",
        "summary": { "total": 8, "passed": 8, "failed": 0, "skipped": 0 },
        "status": "pass",
        "duration_ms": 25
      },
      {
        "name": "core",
        "summary": { "total": 34, "passed": 33, "failed": 1, "skipped": 0 },
        "status": "fail",
        "duration_ms": 120,
        "failures": [
          {
            "name": "parse nested records",
            "module": "test.core.parser-test",
            "failure": {
              "message": "assert-eq failed: expected Rec([...]), got Err(\"unexpected }\")",
              "location": { "file": "packages/core/test/parser-test.clk", "line": 45, "col": 3 }
            }
          }
        ]
      },
      {
        "name": "web",
        "summary": { "total": 20, "passed": 20, "failed": 0, "skipped": 0 },
        "status": "pass",
        "duration_ms": 85
      },
      {
        "name": "cli",
        "summary": { "total": 15, "passed": 15, "failed": 0, "skipped": 0 },
        "status": "pass",
        "duration_ms": 60
      }
    ],
    "workspace_summary": { "total": 77, "passed": 76, "failed": 1, "skipped": 0 }
  },
  "timing": { "total_ms": 290 }
}
```

### 6.4 Dev Dependencies in Workspace Context

Per TASK-083, the workspace produces a single `clank.lock`. Dev dependencies (`[dev-deps]`) are resolved workspace-wide:

- Member A's `[dev-deps]` are available only when building/testing member A
- If two members have conflicting `[dev-deps]` constraints, the resolver applies MVS across all members' dev-deps (same as regular deps) to select one version
- `[dev-deps.local]` follows the same rules as `[deps.local]` (TASK-083 §6.2)

### 6.5 Cross-Member Test Helpers

A common workspace pattern: a shared test utilities member that other members use as a dev dependency.

```
# packages/web/clank.pkg
[dev-deps]
test-utils = "0.1"        # test-utils is a workspace member
```

The workspace resolver treats `test-utils` as a workspace member (local source) even in `[dev-deps]`. The test utilities member is built (but not tested) before dependent members' tests run.

---

## 7. `clank check` in Workspace Context

Type-checking follows the same orchestration model as building:

| Command | Behavior |
|---------|----------|
| `clank check` | Type-check the nearest member (and dependencies if needed) |
| `clank check --all` | Type-check all members |
| `clank check --package core` | Type-check `core` and its dependencies |

`clank check --all` is faster than `clank build --all` because it skips code generation. It uses the same parallel scheduling and signature stability optimization.

---

## 8. Workspace-Aware `--watch` Mode

```bash
clank build --all --watch
clank test --all --watch
clank check --all --watch
```

In watch mode, the file watcher monitors all member `src/` and `test/` directories. When a file changes:

1. Determine which member owns the changed file
2. Rebuild that member
3. If its signature changed, rebuild dependents (transitively)
4. For `test --watch`, re-run affected tests

The watcher emits events as structured JSON:

```json
{
  "event": "rebuild_triggered",
  "changed_file": "packages/core/src/parser.clk",
  "affected_member": "core",
  "rebuild_set": ["core", "web"],
  "reason": "source_changed"
}
```

---

## 9. Error Codes (Additions)

| Code | Description |
|------|-------------|
| E518 | No `clank.pkg` or `clank.workspace` found in directory tree |
| E519 | Workspace command requires `--package` or `--all` (no nearest member) |
| E520 | Workspace build failed — member compilation error (wraps inner diagnostic) |
| E521 | Workspace dependency cycle detected during build scheduling |

These extend the E511-E517 range from TASK-083.

---

## 10. CLI Flags Summary

New flags introduced by this spec:

| Flag | Applies to | Description |
|------|-----------|-------------|
| `--all` | `build`, `test`, `check` | Operate on all workspace members |
| `--package <name>` | `build`, `test`, `check` | Operate on a specific member |
| `--no-deps` | `build` | Skip building workspace dependencies |
| `--no-dep-tests` | `test` | Skip running dependency members' tests |
| `--keep-going` | `build`, `test` | Continue past failures in non-dependent members |
| `--jobs N` | `build`, `test`, `check` | Maximum parallel member operations |
| `--clean` | `build` | Delete build cache before building |
| `--watch` | `build`, `test`, `check` | Re-run on file changes |

---

## 11. Design Rationale

### Why recompute the dependency graph on every invocation?

Parsing `clank.pkg` files is fast (milliseconds for dozens of members). Caching the graph introduces staleness risk — if a developer adds a new inter-member dependency and the cached graph doesn't reflect it, the build order is wrong. The cost of correctness here is negligible.

### Why topological sort with alphabetical tie-breaking?

Deterministic build order aids debugging. If two builds produce different results, alphabetical tie-breaking eliminates ordering as a variable. This matches Cargo's workspace behavior.

### Why signature-based incremental rebuilds instead of file-timestamp-based?

Timestamps are fragile (git checkout changes them, CI environments may not preserve them). Content hashing is reliable. Signature stability adds a second optimization layer: even if `core` is rebuilt (source changed), `web` doesn't rebuild if `core`'s public API is unchanged. This matters in large workspaces where internal refactoring is frequent.

### Why run tests in dependency order?

When `core` has a bug, every member depending on `core` will likely fail too. Running `core`'s tests first and reporting its failure before running `web`'s tests saves the agent from debugging cascading failures. The `--no-dep-tests` escape hatch exists for when you know the dependency is fine and want faster iteration.

### Why workspace-wide MVS for dev-deps?

Split dev-dep resolution (per-member) would allow version skew between test utilities across members. If `core` tests use `test-helpers@0.1.0` and `web` tests use `test-helpers@0.2.0`, shared test patterns diverge. Unified resolution keeps the workspace consistent.

### Why `--keep-going` instead of always continuing?

Fail-fast is the safer default. In most cases, a failure in a foundational member means downstream work is wasted. `--keep-going` is opt-in for CI environments that want maximum diagnostic coverage in a single run.

### Why not cache the dependency graph to disk?

See "Why recompute" above. Additionally, the graph is a derived artifact — it's a pure function of the set of `clank.pkg` files. Caching derived artifacts that are cheap to compute adds complexity (invalidation logic) without meaningful performance benefit.
