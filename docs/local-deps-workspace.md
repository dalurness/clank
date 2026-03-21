# Local Path Dependencies & Workspace Support Specification

**Task:** TASK-083
**Mode:** Spec
**Date:** 2026-03-19
**Dependencies:** plan/features/package-management.md, plan/features/module-system.md
**Extends:** Package Management Specification v1.0

---

## 1. Overview

This spec extends Clank's package management with two features:

1. **Local path dependencies** — depend on a package by filesystem path instead of registry version
2. **Workspaces** — multiple packages in one repository sharing resolution and tooling

Both features support development workflows where packages are co-developed before publishing. They integrate with the existing MVS resolver, lock file, and CLI without changing the registry protocol.

### Design Goals

1. **Zero-config monorepo** — `clank pkg` commands work from any package in a workspace
2. **Local deps override registry** — path deps always win over registry versions during development
3. **Publishable** — path deps must be replaced with registry versions before `clank pkg publish`
4. **Deterministic** — workspace resolution is still MVS; local paths don't introduce ambiguity

---

## 2. Local Path Dependencies

### 2.1 Manifest Syntax

A new `[deps.local]` section in `clank.pkg` declares dependencies resolved from the filesystem instead of the registry:

```
name = "my-app"
version = "1.0.0"

[deps]
json-schema = "0.3"

[deps.local]
my-lib = "../my-lib"
shared-types = "../shared/types"
```

### 2.2 Field Rules

| Field | Type | Description |
|-------|------|-------------|
| Key | package name | Must match the `name` field in the target package's `clank.pkg` |
| Value | relative path | Path to the directory containing the target's `clank.pkg`, relative to the depending package's root |

**Constraints:**

- The path must point to a directory containing a valid `clank.pkg`
- The target's `name` field must match the key in `[deps.local]`
- Paths are always relative (no absolute paths) — this keeps manifests portable
- A package name cannot appear in both `[deps]` and `[deps.local]` — this is error E511

### 2.3 Resolution Behavior

Local path dependencies participate in MVS resolution with special rules:

1. **Version is always "local"** — path deps have no version constraint. The resolver treats them as satisfying any version requirement from other packages in the graph.

2. **Transitive local deps** — if `my-lib` (a local dep) depends on `shared-types` via `[deps.local]`, that path is resolved relative to `my-lib`'s directory, not the root package's.

3. **Mixed graphs** — a dependency graph can contain both local and registry packages. Local packages may depend on registry packages (via `[deps]`) and vice versa is handled by workspaces (§3).

4. **No cycles** — circular local dependencies are error E512. The resolver detects cycles during graph construction.

### 2.4 Lock File Representation

Local path deps are recorded in `clank.lock` with a `path` source instead of a `resolved` URL:

```json
{
  "lock_version": 1,
  "clank_version": "0.2.0",
  "resolved_at": "2026-03-19T14:00:00Z",
  "packages": {
    "my-lib@local": {
      "version": "local",
      "source": "path",
      "path": "../my-lib",
      "integrity": null,
      "deps": {
        "json-schema": "0.3.0"
      },
      "effects": ["io"]
    },
    "json-schema@0.3.0": {
      "version": "0.3.0",
      "resolved": "https://registry.clank.dev/packages/json-schema/0.3.0.tar.gz",
      "integrity": "sha256:f6e5d4c3b2a1...",
      "deps": {},
      "effects": []
    }
  }
}
```

Key differences from registry entries:
- Key is `"name@local"` instead of `"name@version"`
- `"source": "path"` distinguishes from registry deps
- `"integrity": null` — local deps are not content-hashed (they change constantly)
- `"path"` field records the relative path from the lock file's directory

### 2.5 Project-Local Link

Local path deps are symlinked directly from source rather than from the cache:

```
.clank/deps/
  my-lib -> ../my-lib/
  json-schema -> ~/.clank/cache/packages/json-schema/0.3.0/
```

Changes to `my-lib`'s source are immediately visible without re-resolving.

### 2.6 Publishing Constraint

`clank pkg publish` rejects packages with `[deps.local]` entries — error E513:

```json
{
  "ok": false,
  "diagnostics": [
    {
      "severity": "error",
      "code": "E513",
      "phase": "publish",
      "message": "cannot publish with local path dependencies: my-lib",
      "suggestion": "Replace [deps.local] entries with [deps] version constraints before publishing."
    }
  ]
}
```

This ensures published packages are self-contained and reproducible from the registry alone.

---

## 3. Workspaces

### 3.1 Workspace Manifest

A workspace is declared by a `clank.workspace` file at the repository root:

```
[workspace]
members = [
  "packages/core",
  "packages/cli",
  "packages/web",
  "libs/shared-types"
]
```

Each member path points to a directory containing a `clank.pkg`. The workspace file is not a package manifest — it only configures the workspace.

### 3.2 `clank.workspace` Schema

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `members` | yes | list of paths | Directories containing member packages |

Glob patterns are supported in `members`:

```
[workspace]
members = [
  "packages/*",
  "libs/*"
]
```

This expands to all immediate subdirectories of `packages/` and `libs/` that contain a `clank.pkg` file.

### 3.3 Unified Resolution

All workspace members share a single dependency resolution. The resolver:

1. Collects `[deps]` from all member packages
2. Resolves a single version for each external dependency using MVS across all members' constraints
3. Produces one `clank.lock` at the workspace root (not in individual members)

This prevents version skew — if `packages/core` requires `json >= 0.3` and `packages/web` requires `json >= 0.4`, the workspace resolves `json@0.4.0` for both.

### 3.4 Inter-Member Dependencies

Workspace members can depend on each other without `[deps.local]`. The workspace resolver automatically treats member packages as local path deps:

```
# packages/web/clank.pkg
name = "web"
version = "0.1.0"

[deps]
core = "0.1"
json = "0.4"
```

If `core` is a workspace member, the resolver uses the local source at `packages/core/` instead of fetching `core@0.1.x` from the registry. The version constraint in `[deps]` is still checked against the member's declared version — if `packages/core/clank.pkg` declares `version = "0.1.0"` and the constraint is `"0.1"`, it matches. If the member's version doesn't satisfy the constraint, it's error E514.

### 3.5 Workspace Lock File

The workspace produces a single `clank.lock` at the workspace root:

```json
{
  "lock_version": 1,
  "clank_version": "0.2.0",
  "resolved_at": "2026-03-19T14:00:00Z",
  "workspace_members": ["core", "web", "cli", "shared-types"],
  "packages": {
    "core@workspace": {
      "version": "0.1.0",
      "source": "workspace",
      "path": "packages/core",
      "integrity": null,
      "deps": { "json": "0.4.0" },
      "effects": ["io"]
    },
    "json@0.4.0": {
      "version": "0.4.0",
      "resolved": "https://registry.clank.dev/packages/json/0.4.0.tar.gz",
      "integrity": "sha256:...",
      "deps": {},
      "effects": []
    }
  }
}
```

Key additions:
- `"workspace_members"` lists all member package names
- Workspace member entries use `"source": "workspace"` and `"name@workspace"` keys
- Individual member directories do not have their own `clank.lock` files

### 3.6 Workspace-Aware CLI

All `clank pkg` commands are workspace-aware when run from anywhere inside a workspace (the CLI walks up to find `clank.workspace`):

| Command | Workspace Behavior |
|---------|--------------------|
| `clank pkg install` | Installs all deps for all members; writes workspace-level lock |
| `clank pkg add json "0.4" --package web` | Adds dep to a specific member |
| `clank pkg add json "0.4"` | Adds dep to the nearest member (cwd) |
| `clank pkg list` | Lists all deps across workspace |
| `clank pkg list --package core` | Lists deps for a specific member |
| `clank pkg update` | Updates all external deps across workspace |
| `clank pkg build` | Builds the nearest member (cwd) |
| `clank pkg build --all` | Builds all members |

The `--package <name>` flag selects a specific member. If omitted, the CLI uses the nearest `clank.pkg` relative to the current working directory.

#### `clank pkg workspace` Subcommand

New subcommand for workspace management:

```bash
clank pkg workspace init                    # create clank.workspace in cwd
clank pkg workspace list                    # list all members
clank pkg workspace add packages/new-pkg    # add a member
clank pkg workspace remove packages/old-pkg # remove a member
```

**Output for `clank pkg workspace list`:**

```json
{
  "ok": true,
  "data": {
    "root": "/home/user/my-project",
    "members": [
      {
        "name": "core",
        "path": "packages/core",
        "version": "0.1.0",
        "deps_count": 3,
        "dependents": ["web", "cli"]
      },
      {
        "name": "web",
        "path": "packages/web",
        "version": "0.1.0",
        "deps_count": 5,
        "dependents": []
      }
    ]
  }
}
```

### 3.7 Shared Build Artifacts

Workspace members share a single `.clank/` directory at the workspace root:

```
my-project/
  clank.workspace
  clank.lock
  .clank/
    deps/                  # shared dep symlinks
      json -> ~/.clank/cache/packages/json/0.4.0/
    build/                 # per-member build output
      core/
      web/
      cli/
  packages/
    core/
      clank.pkg
      src/
    web/
      clank.pkg
      src/
```

This avoids downloading and storing the same dependency multiple times for different members.

### 3.8 Publishing from a Workspace

When publishing a workspace member:

1. `clank pkg publish --package core` publishes `packages/core`
2. Inter-member deps (source: workspace) must have corresponding `[deps]` entries with version constraints — these are what get published
3. The publish check verifies the member's `[deps]` constraints are satisfiable from the registry (ignoring workspace resolution)

This is why workspace members declare version constraints on each other in `[deps]` even though the workspace resolver uses local sources — the constraints become the published dependency requirements.

---

## 4. CLI Command Extensions

### 4.1 `clank pkg link`

Convenience command to add a local path dependency:

```bash
clank pkg link ../my-lib                  # adds to [deps.local]
clank pkg link ../my-lib --name my-lib    # explicit name (if different from clank.pkg name)
clank pkg unlink my-lib                   # removes from [deps.local]
```

**Output:**

```json
{
  "ok": true,
  "data": {
    "linked": "my-lib",
    "path": "../my-lib",
    "version": "0.2.0",
    "effects_introduced": ["io"]
  }
}
```

`clank pkg link` reads the target's `clank.pkg` to determine the package name and version, then adds the entry to the current package's `[deps.local]`.

### 4.2 `clank pkg link --replace`

Temporarily replace a registry dependency with a local path for development:

```bash
clank pkg link ../my-fork/net-http --replace net-http
```

This adds to `[deps.local]` while keeping the `[deps]` entry intact. During resolution, the local path takes priority. This is useful for debugging or patching a dependency without modifying `[deps]`.

The `--replace` flag sets a `replaces` annotation in the manifest:

```
[deps]
net-http = "1.2"

[deps.local]
net-http = "../my-fork/net-http"   # replaces = "net-http"
```

When a name appears in both `[deps]` and `[deps.local]`, it is only valid if the `[deps.local]` entry is a replacement (i.e., added via `--replace`). Otherwise, it's error E511.

### 4.3 Workspace Init

```bash
clank pkg workspace init
```

Creates a `clank.workspace` file in the current directory. If `clank.pkg` files exist in subdirectories, it offers to add them as members (in interactive mode) or requires `--members` in agent mode:

```bash
clank pkg workspace init --members "packages/*,libs/*"
```

---

## 5. Resolution Algorithm Extensions

### 5.1 Unified Resolution with Local Sources

The MVS resolver is extended with a **source priority** rule:

1. **Workspace members** — always resolve to local source (highest priority)
2. **`[deps.local]` entries** — resolve to local path
3. **`[deps.local]` replacements** — local path overrides registry for the named dep
4. **`[deps]` entries** — resolve from registry (default)

For each dependency, the resolver checks sources in this order. The first match wins. Version constraints from `[deps]` are still checked against the local package's declared version (for workspace members and replacements).

### 5.2 Constraint Validation

When a workspace member or local dep satisfies a version constraint from `[deps]`:

- The local package's `version` field in `clank.pkg` is compared against the constraint
- If the local version doesn't satisfy the constraint, the resolver emits error E514 with a clear diagnostic:

```json
{
  "ok": false,
  "diagnostics": [
    {
      "severity": "error",
      "code": "E514",
      "phase": "resolve",
      "message": "workspace member core@0.1.0 does not satisfy constraint >= 0.2 required by web",
      "suggestion": "Update core's version in packages/core/clank.pkg, or relax the constraint in packages/web/clank.pkg."
    }
  ]
}
```

### 5.3 Cycle Detection

Local deps and workspace members can create dependency cycles that aren't possible with registry packages (since you can't publish cycles). The resolver detects cycles during graph construction and reports error E512:

```json
{
  "ok": false,
  "diagnostics": [
    {
      "severity": "error",
      "code": "E512",
      "phase": "resolve",
      "message": "circular dependency: core -> utils -> core",
      "cycle": ["core", "utils", "core"]
    }
  ]
}
```

---

## 6. Interaction with Existing Features

### 6.1 Module System

Cross-package imports work identically for local and registry deps:

```
use my-lib.parser (parse)
```

The compiler resolves `my-lib` via `.clank/deps/my-lib/` whether that's a symlink to a local directory or a cached registry download.

### 6.2 Dev Dependencies

`[dev-deps]` can also use local paths via `[dev-deps.local]`:

```
[dev-deps.local]
test-helpers = "../test-helpers"
```

Same rules as `[deps.local]` — paths are relative, name must match, no publishing with local dev-deps.

### 6.3 Effect Tracking

Local deps' effects are tracked the same as registry deps. `clank pkg link` reports `effects_introduced` just like `clank pkg add`. Workspace `clank pkg list` shows effects per member.

### 6.4 Integrity and Caching

- Local path deps have `"integrity": null` in the lock file — they're mutable by nature
- Workspace members have `"integrity": null` for the same reason
- The compiler always reads local deps from source (no caching) to pick up changes immediately
- `clank check` skips integrity verification for local/workspace deps

---

## 7. Error Codes (Additions)

| Code | Description |
|------|-------------|
| E511 | Duplicate dependency — same name in `[deps]` and `[deps.local]` without `--replace` |
| E512 | Circular dependency in local/workspace dependency graph |
| E513 | Cannot publish package with `[deps.local]` entries |
| E514 | Local/workspace package version does not satisfy constraint |
| E515 | Workspace member not found — path in `clank.workspace` has no `clank.pkg` |
| E516 | Workspace member name conflict — two members declare the same `name` |
| E517 | Local path target not found — `[deps.local]` path has no `clank.pkg` |

---

## 8. Design Rationale

### Why `[deps.local]` instead of inlining paths in `[deps]`?

Separating local deps into their own section makes it visually obvious which deps are local and which are from the registry. It also simplifies the publishing check — reject if `[deps.local]` is non-empty. Inlining paths in `[deps]` (e.g., `my-lib = { path = "../my-lib" }`) requires richer value syntax and makes it harder to distinguish local from registry deps at a glance.

### Why a separate `clank.workspace` file instead of a section in `clank.pkg`?

A workspace root may not be a package itself — it's just a container for member packages. Using a separate file avoids requiring a `clank.pkg` at the workspace root when there's no root package. It also makes workspace membership explicit and discoverable.

### Why unified resolution across workspace members?

Split resolution (each member resolves independently) leads to version skew — `core` might use `json@0.3.0` while `web` uses `json@0.4.0`. This causes link-time conflicts when both are used together. Unified resolution guarantees one version per external dep across the workspace, matching Go workspaces and Cargo workspaces.

### Why check version constraints against local packages?

If `web` declares `core = "0.2"` but the local `core` is at version `0.1.0`, silently ignoring the mismatch would make local development diverge from what happens when published. Checking constraints early catches these issues before they become publish-time surprises.

### Why `clank pkg link --replace` instead of just allowing duplicates?

Explicit replacement intent prevents accidental shadowing. If a developer has `net-http = "1.2"` in `[deps]` and adds `net-http` to `[deps.local]` without `--replace`, it's likely a mistake. Requiring `--replace` makes the override intentional and auditable.

### Why no integrity hashes for local deps?

Local deps change constantly during development. Computing and checking hashes on every build would add latency without security benefit — the developer controls both sides. Integrity checking applies at the publish/registry boundary where trust matters.
