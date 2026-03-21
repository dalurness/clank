# Clank Package Management Specification v1.0

**Task:** TASK-079
**Mode:** Spec
**Date:** 2026-03-19
**Dependencies:** plan/features/tooling-spec.md, plan/features/module-system.md, plan/SPEC.md

---

## 1. Overview

Clank's package manager is agent-native: packages are discoverable by type signature and effect profile, resolution is deterministic via Minimal Version Selection (MVS), and every command emits structured JSON. The system comprises four components:

1. **Manifest** (`clank.pkg`) — declares package identity, dependencies, and metadata
2. **Lock file** (`clank.lock`) — records resolved versions with integrity hashes
3. **CLI** (`clank pkg <subcommand>`) — agent-facing interface for all package operations
4. **Registry protocol** — JSON API for discovery, search, and download

### Design Goals

1. **Deterministic** — given the same manifests, resolution always produces the same versions, with or without a lock file
2. **Agent-discoverable** — packages are searchable by type signature, effect usage, and semantic tags
3. **Minimal I/O** — agents can evaluate packages (type API, effects, size) without downloading source
4. **No SAT solving** — MVS avoids NP-hard resolution; conflicts are always explainable
5. **Integrity-first** — every artifact is content-addressed; no TOFU trust model

---

## 2. Package Manifest (`clank.pkg`)

The manifest uses Clank's simple key-value format. Sections are delimited by `[section]` headers. Values are strings (quoted) or bare identifiers.

### 2.1 Full Schema

```
name = "my-app"
version = "1.0.0"
entry = "main"
description = "A short description for registry display"
license = "MIT"
repository = "https://github.com/user/my-app"
authors = ["alice", "bob"]
clank = ">= 0.2.0"
keywords = ["http", "server", "async"]

[deps]
net-http = "1.2"
json-schema = "0.3"

[dev-deps]
test-helpers = "0.1"
bench = "0.2"

[effects]
io = true
async = true
exn = true

[exports]
modules = ["main", "lib.parser", "lib.types"]
```

### 2.2 Field Definitions

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `name` | yes | string | Package identifier. Lowercase ASCII, hyphens allowed. `[a-z][a-z0-9-]*`, max 64 chars. |
| `version` | yes | semver | Package version following semantic versioning (`MAJOR.MINOR.PATCH`). |
| `entry` | no | string | Module containing `main` function. Required for executable packages. |
| `description` | no | string | One-line description (max 200 chars). Displayed in search results. |
| `license` | no | string | SPDX license identifier. |
| `repository` | no | string | Source repository URL. |
| `authors` | no | list | List of author identifiers. |
| `clank` | no | version constraint | Minimum compatible Clank version. |
| `keywords` | no | list | Tags for registry search (max 10, each max 32 chars). |

### 2.3 Version Constraints

Version constraints in `[deps]` and `[dev-deps]` use a minimal syntax:

| Syntax | Meaning | Example |
|--------|---------|---------|
| `"1.2.3"` | Exactly `1.2.3` | `net-http = "1.2.3"` |
| `"1.2"` | `>= 1.2.0` and `< 2.0.0` (compatible range) | `net-http = "1.2"` |
| `"1"` | `>= 1.0.0` and `< 2.0.0` | `net-http = "1"` |
| `">= 1.2.0"` | At least `1.2.0`, no upper bound | `net-http = ">= 1.2.0"` |
| `">= 1.2, < 2.0"` | Range with explicit bounds | `net-http = ">= 1.2, < 2.0"` |

The **default interpretation** of a bare minor version like `"1.2"` is a compatible range: `>= 1.2.0, < 2.0.0`. This follows semver conventions — a major version bump signals breaking changes.

Under MVS, the resolver picks the **minimum version** satisfying the constraint. For `"1.2"`, that's `1.2.0` — not the latest `1.x`.

### 2.4 `[effects]` Section

The `[effects]` section declares the top-level effects that the package's public API uses. This is metadata — the compiler verifies it matches actual exports. It exists so the registry can index packages by effect profile without parsing source code.

### 2.5 `[exports]` Section

The optional `[exports].modules` field explicitly lists which modules are public API. If omitted, all `pub` definitions in all modules are considered public. When specified, only the listed modules' `pub` definitions are accessible to dependents.

This enables packages to have internal modules (helpers, utilities) that are not part of the public API even though they use `pub` for cross-module access within the package.

### 2.6 `[dev-deps]` Semantics

Dev dependencies are available only during `clank test` and `clank build --dev`. They are not included in the dependency graph when the package is consumed as a dependency by another package.

---

## 3. Dependency Resolution (MVS)

### 3.1 Algorithm

Clank uses **Minimal Version Selection** (MVS), adapted from Go modules. The algorithm:

1. **Build requirement graph**: Starting from the root package, collect all `[deps]` entries from all packages transitively.

2. **Compute minimum versions**: For each unique package name, determine the minimum version that satisfies *all* constraints from all dependents:
   - If package A requires `json >= 0.3` and package B requires `json >= 0.4`, the selected version is `0.4.0`.
   - MVS picks the *minimum* version that satisfies the *maximum* of all lower bounds.

3. **Check upper bounds**: If any constraint specifies an upper bound (e.g., `< 2.0`), verify the selected version satisfies it. If not, emit a conflict error.

4. **Verify compatibility**: Ensure no two selected versions of the same package exist (no diamond splits).

### 3.2 Why MVS

| Property | MVS | SAT-based (npm, Cargo) |
|----------|-----|------------------------|
| Deterministic without lock file | Yes | No |
| Resolution complexity | O(n) in dependency graph | NP-hard worst case |
| Conflict explanation | Always a simple chain | May require backtracking trace |
| "Latest compatible" behavior | No — minimum by design | Yes |
| Agent-friendliness | High (predictable) | Lower (non-obvious choices) |

MVS trades "always latest" for "always predictable." An agent can reason about what version will be selected without running the resolver. The lock file provides integrity verification, not version pinning — if you delete `clank.lock` and re-resolve, you get the same versions.

### 3.3 Version Selection Rules

```
Given:
  root requires A >= 1.2
  A@1.2.0 requires B >= 0.5
  root requires B >= 0.7

Resolution:
  A = 1.2.0  (minimum satisfying >= 1.2)
  B = 0.7.0  (minimum satisfying max(>= 0.5, >= 0.7) = >= 0.7)
```

### 3.4 Conflict Reporting

When constraints are unsatisfiable, the resolver emits a structured diagnostic:

```json
{
  "ok": false,
  "diagnostics": [
    {
      "severity": "error",
      "code": "E501",
      "phase": "link",
      "message": "version conflict: my-app requires json >= 0.3, net-http@1.2.3 requires json < 0.3",
      "dependency": "json",
      "constraint_chain": [
        { "package": "my-app", "constraint": ">= 0.3", "source": "clank.pkg" },
        { "package": "net-http", "version": "1.2.3", "constraint": "< 0.3", "source": "clank.pkg" }
      ],
      "suggestion": "Upgrade net-http to a version compatible with json >= 0.3, or pin json to a version both accept."
    }
  ]
}
```

The `constraint_chain` field traces exactly why the conflict exists — no mystery.

### 3.5 Upgrade and Downgrade

`clank pkg update` selects the minimum version satisfying constraints from the *latest available* dependency manifests. This may raise versions if a dependency's own requirements have increased:

- `clank pkg update` — re-resolve all dependencies from latest manifests
- `clank pkg update json` — re-resolve only `json` and its transitive deps

The resolver never downgrades a dependency below its current lock file version unless explicitly requested via `clank pkg update --force json@0.2.0`.

---

## 4. Lock File (`clank.lock`)

### 4.1 Purpose

The lock file provides:
1. **Integrity verification** — every resolved package has a content hash
2. **Reproducible installs** — `clank pkg install` uses locked versions without re-resolving
3. **Audit trail** — records exactly what was resolved and from where

Under MVS, the lock file is *not required for version determinism* — resolution is deterministic from manifests alone. The lock file adds integrity hashes and resolved URLs that manifests don't contain.

### 4.2 Format

```json
{
  "lock_version": 1,
  "clank_version": "0.2.0",
  "resolved_at": "2026-03-19T14:00:00Z",
  "packages": {
    "net-http@1.2.3": {
      "version": "1.2.3",
      "resolved": "https://registry.clank.dev/packages/net-http/1.2.3.tar.gz",
      "integrity": "sha256:a1b2c3d4e5f6...",
      "deps": {
        "std": "0.1.0"
      },
      "effects": ["io", "async", "exn"]
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

### 4.3 Field Definitions

| Field | Description |
|-------|-------------|
| `lock_version` | Schema version for the lock file format (currently `1`). |
| `clank_version` | Clank version used to generate the lock. |
| `resolved_at` | ISO 8601 timestamp of resolution. |
| `packages` | Map of `"name@version"` → package record. |
| `packages.*.version` | Resolved version string. |
| `packages.*.resolved` | URL the archive was fetched from. |
| `packages.*.integrity` | Subresource Integrity hash (`sha256:<hex>`). |
| `packages.*.deps` | Map of dependency name → resolved version for this package. |
| `packages.*.effects` | Top-level effects used by this package (from registry index). |

### 4.4 Lock File Behavior

- `clank pkg add` / `clank pkg remove` / `clank pkg update` — re-resolves and rewrites the lock file
- `clank pkg install` — installs from lock file without re-resolving; fails if lock is stale
- `clank check` / `clank build` — verifies installed packages match lock file hashes; warns if lock is missing
- Lock files should be committed to version control

---

## 5. Package Storage

### 5.1 Local Cache

Downloaded packages are stored in a global cache:

```
~/.clank/cache/packages/
  net-http/
    1.2.3/
      clank.pkg
      src/
        ...
      integrity.sha256
```

The cache is content-addressed — if two projects use `net-http@1.2.3`, only one copy exists. The `integrity.sha256` file contains the hash verified against the lock file.

### 5.2 Project-Local Link

Within a project, resolved dependencies are linked (symlinked or copied) into:

```
.clank/deps/
  net-http -> ~/.clank/cache/packages/net-http/1.2.3/
  json-schema -> ~/.clank/cache/packages/json-schema/0.3.0/
```

The compiler resolves cross-package imports (`use net-http.client (fetch)`) by looking up `net-http` in `.clank/deps/`.

---

## 6. CLI Subcommands

All subcommands follow the standard output envelope (§3.4 of tooling-spec.md). Default output is JSON. All accept `--format human` for pretty-printed output.

### 6.1 `clank pkg init`

Initialize a new package.

```bash
clank pkg init                                    # interactive (human)
clank pkg init --name "my-app" --entry "main"     # non-interactive (agent)
clank pkg init --name "my-lib"                    # library (no entry point)
```

Creates `clank.pkg` and `src/` directory. If `--entry` is specified, generates a starter `src/<entry>.clk`.

**Output:**

```json
{
  "ok": true,
  "data": {
    "package": "my-app",
    "created_files": ["clank.pkg", "src/main.clk"]
  }
}
```

### 6.2 `clank pkg add`

Add a dependency.

```bash
clank pkg add net-http                  # latest compatible (adds "net-http = \"X.Y\"")
clank pkg add net-http "1.2"            # specific constraint
clank pkg add net-http --dev            # add to [dev-deps]
```

Adds the entry to `clank.pkg`, re-resolves dependencies, downloads the package, and updates `clank.lock`.

**Output:**

```json
{
  "ok": true,
  "data": {
    "added": "net-http",
    "version": "1.2.3",
    "constraint": "1.2",
    "new_deps": ["net-http@1.2.3"],
    "effects_introduced": ["io", "async", "exn"]
  }
}
```

The `effects_introduced` field tells the agent what new effects are now available transitively — critical for understanding the impact of adding a dependency.

### 6.3 `clank pkg remove`

Remove a dependency.

```bash
clank pkg remove net-http
```

Removes from `clank.pkg`, re-resolves, prunes unused transitive deps, updates `clank.lock`.

**Output:**

```json
{
  "ok": true,
  "data": {
    "removed": "net-http",
    "pruned": ["net-http@1.2.3"],
    "effects_removed": ["async"]
  }
}
```

`effects_removed` lists effects that are no longer reachable from any remaining dependency. Effects still reachable via other deps are not listed.

### 6.4 `clank pkg update`

Update dependencies to latest compatible versions.

```bash
clank pkg update                        # update all
clank pkg update net-http               # update one
clank pkg update --dry-run              # show what would change
```

**Output:**

```json
{
  "ok": true,
  "data": {
    "updates": [
      {
        "package": "net-http",
        "from": "1.2.3",
        "to": "1.3.0",
        "changelog_url": "https://registry.clank.dev/packages/net-http/changelog#1.3.0"
      }
    ],
    "no_update": ["json-schema"]
  }
}
```

### 6.5 `clank pkg list`

List all dependencies (direct and transitive).

```bash
clank pkg list                          # all deps
clank pkg list --direct                 # only direct deps
clank pkg list --tree                   # dependency tree
```

**Output (flat):**

```json
{
  "ok": true,
  "data": {
    "packages": [
      {
        "name": "net-http",
        "version": "1.2.3",
        "direct": true,
        "deps": ["std@0.1.0"],
        "effects": ["io", "async", "exn"]
      },
      {
        "name": "json-schema",
        "version": "0.3.0",
        "direct": true,
        "deps": [],
        "effects": []
      }
    ]
  }
}
```

**Output (tree):**

```json
{
  "ok": true,
  "data": {
    "tree": {
      "my-app@1.0.0": {
        "net-http@1.2.3": {
          "std@0.1.0": {}
        },
        "json-schema@0.3.0": {}
      }
    }
  }
}
```

### 6.6 `clank pkg search`

Search the registry. Supports name, type signature, and effect queries.

```bash
clank pkg search --name "http"
clank pkg search --type "([a]) -> Option<a>"
clank pkg search --effect "async"
clank pkg search --keyword "server"
clank pkg search --name "http" --effect "async"    # combinable
```

**Output:**

```json
{
  "ok": true,
  "data": {
    "results": [
      {
        "name": "net-http",
        "version": "1.2.3",
        "description": "HTTP client and server",
        "keywords": ["http", "server", "client", "async"],
        "exports_summary": { "functions": 15, "types": 4, "effects": 2, "interfaces": 1 },
        "effects_used": ["io", "async", "exn"],
        "downloads": 1200,
        "score": 0.95,
        "updated": "2026-03-10"
      }
    ],
    "total": 1,
    "query": { "name": "http" }
  }
}
```

Type-based search uses unification (same as `clank query --type`), applied across all packages' exported function signatures in the registry index. This lets agents find packages by the shape of the functions they need.

### 6.7 `clank pkg info`

Deep inspection of a package without downloading source.

```bash
clank pkg info net-http
clank pkg info net-http --version "1.2.0"
```

**Output:**

```json
{
  "ok": true,
  "data": {
    "name": "net-http",
    "version": "1.2.3",
    "description": "HTTP client and server",
    "license": "MIT",
    "repository": "https://github.com/clank-lang/net-http",
    "clank_version": ">= 0.2.0",
    "exports": [
      { "module": "net-http.client", "name": "fetch", "type": "(Str) -> <io, async, exn[HttpErr]> Response", "doc": "Fetch a URL and return the response." },
      { "module": "net-http.client", "name": "get", "type": "(Str) -> <io, async, exn[HttpErr]> Str", "doc": "Fetch a URL and return the body as a string." },
      { "module": "net-http.server", "name": "serve", "type": "(Int, (Request) -> <io, async> Response) -> <io, async> ()", "doc": "Start an HTTP server on the given port." }
    ],
    "types": [
      { "module": "net-http.types", "name": "Response", "kind": "record", "fields": ["status: Int", "body: Str", "headers: Map<Str, Str>"] },
      { "module": "net-http.types", "name": "Request", "kind": "record", "fields": ["method: Str", "path: Str", "headers: Map<Str, Str>", "body: Str"] }
    ],
    "effects": [
      { "module": "net-http", "name": "HttpErr", "ops": ["timeout: () -> ()", "status: (Int) -> ()", "connection: () -> ()"] }
    ],
    "interfaces": [
      { "module": "net-http.types", "name": "ToResponse", "methods": ["to-response: (Self) -> <> Response"] }
    ],
    "deps": { "std": "0.1" },
    "versions": ["1.2.3", "1.2.2", "1.2.1", "1.2.0", "1.1.0", "1.0.0"]
  }
}
```

This is the agent's primary tool for evaluating whether to add a dependency. It provides the full type-level API (function signatures, types, effects, interfaces) without requiring source download or context window pollution.

### 6.8 `clank pkg publish`

Publish the current package to the registry.

```bash
clank pkg publish                       # publish current version
clank pkg publish --dry-run             # validate without publishing
clank pkg publish --token <auth-token>  # explicit auth
```

**Pre-publish checks** (run automatically, also available via `--dry-run`):

1. `clank.pkg` is valid and complete (name, version, description, license)
2. `clank check` passes with no errors
3. Version does not already exist in registry
4. No `[dev-deps]` are imported by non-test modules
5. All `pub` functions have explicit effect annotations (enforced by module system Rule M5)
6. Package size is within registry limits

**Output:**

```json
{
  "ok": true,
  "data": {
    "package": "my-lib",
    "version": "0.2.0",
    "registry": "https://registry.clank.dev",
    "integrity": "sha256:abc123...",
    "exports_indexed": { "functions": 8, "types": 3, "effects": 1 }
  }
}
```

At publish time, the registry extracts and indexes:
- All exported function signatures (for type-based search)
- All exported type definitions
- All declared and used effects
- Keywords and description (for text search)

### 6.9 `clank pkg audit`

Check installed dependencies for known issues.

```bash
clank pkg audit
clank pkg audit --json                  # (default)
```

**Output:**

```json
{
  "ok": false,
  "data": {
    "vulnerabilities": [
      {
        "package": "old-crypto",
        "version": "0.1.2",
        "severity": "high",
        "id": "CLANK-2026-001",
        "description": "Timing side-channel in signature verification",
        "fixed_in": "0.1.3",
        "url": "https://registry.clank.dev/advisories/CLANK-2026-001"
      }
    ],
    "summary": { "high": 1, "medium": 0, "low": 0 }
  }
}
```

---

## 7. Registry Protocol

The registry is a JSON API. All endpoints return the standard Clank output envelope (`{ ok, data, diagnostics }`).

### 7.1 Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/packages/{name}` | Package metadata (latest version) |
| `GET` | `/v1/packages/{name}/{version}` | Package metadata (specific version) |
| `GET` | `/v1/packages/{name}/{version}/download` | Download package archive |
| `GET` | `/v1/search?q={query}` | Text search (name, description, keywords) |
| `POST` | `/v1/search/type` | Type signature search (body: `{ "type": "([a]) -> Option<a>" }`) |
| `POST` | `/v1/search/effect` | Effect search (body: `{ "effects": ["async", "io"] }`) |
| `GET` | `/v1/packages/{name}/versions` | List all versions |
| `PUT` | `/v1/packages/{name}/{version}` | Publish (requires auth) |
| `GET` | `/v1/advisories` | Security advisories |
| `GET` | `/v1/index` | Full registry index (for offline resolution) |

### 7.2 Package Metadata Response

```
GET /v1/packages/net-http/1.2.3
```

```json
{
  "ok": true,
  "data": {
    "name": "net-http",
    "version": "1.2.3",
    "description": "HTTP client and server",
    "license": "MIT",
    "repository": "https://github.com/clank-lang/net-http",
    "keywords": ["http", "server", "client"],
    "clank_version": ">= 0.2.0",
    "deps": { "std": "0.1" },
    "dev_deps": {},
    "integrity": "sha256:a1b2c3d4e5f6...",
    "download_url": "https://registry.clank.dev/packages/net-http/1.2.3.tar.gz",
    "exports": {
      "functions": [
        { "module": "net-http.client", "name": "fetch", "type": "(Str) -> <io, async, exn[HttpErr]> Response", "doc": "Fetch a URL." }
      ],
      "types": [
        { "module": "net-http.types", "name": "Response", "kind": "record", "fields": ["status: Int", "body: Str", "headers: Map<Str, Str>"] }
      ],
      "effects": [
        { "module": "net-http", "name": "HttpErr", "ops": ["timeout: () -> ()", "status: (Int) -> ()"] }
      ],
      "interfaces": []
    },
    "published_at": "2026-03-10T12:00:00Z",
    "downloads": 1200
  }
}
```

### 7.3 Type-Directed Search

```
POST /v1/search/type
Content-Type: application/json

{
  "type": "([a], (a) -> b) -> [b]",
  "limit": 10
}
```

The registry maintains a precomputed index of all exported function signatures. Search uses type unification — the query `([a], (a) -> b) -> [b]` matches functions with extra effect variables, additional constraints, or minor structural variations. Results are ranked by unification closeness (1.0 = exact match).

Response:

```json
{
  "ok": true,
  "data": {
    "results": [
      {
        "package": "std",
        "version": "0.1.0",
        "module": "std.col",
        "name": "map",
        "type": "([a], (a) -> <e> b) -> <e> [b]",
        "score": 1.0
      },
      {
        "package": "parallel",
        "version": "0.2.0",
        "module": "parallel.list",
        "name": "pmap",
        "type": "([a], (a) -> <async | e> b) -> <async | e> [b]",
        "score": 0.9
      }
    ]
  }
}
```

### 7.4 Registry Index

The full registry index is a compact JSON file listing every package with its version constraints and export summaries. This enables **offline resolution** — an agent can download the index once and resolve dependencies locally.

```
GET /v1/index
```

```json
{
  "ok": true,
  "data": {
    "updated_at": "2026-03-19T14:00:00Z",
    "packages": {
      "net-http": {
        "versions": {
          "1.2.3": {
            "deps": { "std": "0.1" },
            "integrity": "sha256:a1b2c3d4e5f6...",
            "effects": ["io", "async", "exn"],
            "exports_summary": { "functions": 15, "types": 4, "effects": 2, "interfaces": 1 }
          }
        }
      }
    }
  }
}
```

The index is incrementally updatable — a `since` parameter returns only packages modified after a timestamp.

### 7.5 Authentication

Publishing requires an API token passed via:
1. `--token` flag on `clank pkg publish`
2. `CLANK_REGISTRY_TOKEN` environment variable
3. `~/.clank/credentials.json` (stored by `clank pkg login`)

Tokens are scoped per-package or global. The registry uses standard bearer token auth.

---

## 8. Agent-Optimized Discovery

### 8.1 The Problem

An agent needs to find the right package without loading thousands of README files into its context window. Traditional package discovery (browsing npm, reading GitHub READMEs) is context-expensive and imprecise.

### 8.2 Discovery Workflow

An agent searching for functionality follows this flow:

1. **Type search**: "I need a function `([a], (a) -> Bool) -> [a]`" → `clank pkg search --type "([a], (a) -> Bool) -> [a]"` returns matching functions across all packages.

2. **Effect filter**: "I need it to be pure" → add `--effect "<>"` or filter results where `effects_used` is empty.

3. **Info check**: `clank pkg info <candidate>` returns the full type-level API. The agent can evaluate whether the package fits without reading source.

4. **Add**: `clank pkg add <name>` — the output includes `effects_introduced`, so the agent knows immediately what new effects it's bringing into scope.

This workflow is entirely structured JSON — no README parsing, no documentation scraping, no context window pollution.

### 8.3 Semantic Tags

Packages declare `keywords` in their manifest. The registry also auto-generates semantic tags from:
- Effect usage (packages using `io` + `async` are tagged `networking`)
- Type patterns (packages exporting `Parser<a>` types are tagged `parsing`)
- Dependency patterns (packages depending on `std.test` heavily are tagged `testing`)

These tags are available in search results and `pkg info` output.

### 8.4 Compatibility Matrix

`clank pkg info` includes a `clank_version` field. The CLI warns if a package requires a newer Clank version than what's installed. The registry API also supports a `?clank_version=X.Y.Z` query parameter to filter search results to compatible packages.

---

## 9. Package Archive Format

### 9.1 Structure

Published packages are `.tar.gz` archives containing:

```
net-http-1.2.3/
  clank.pkg
  src/
    client.clk
    server.clk
    types.clk
  index.json
```

### 9.2 `index.json`

The `index.json` file is generated at publish time and contains the precomputed export index used by the registry for search and `pkg info`:

```json
{
  "name": "net-http",
  "version": "1.2.3",
  "exports": { ... },
  "types": { ... },
  "effects": { ... },
  "interfaces": { ... }
}
```

This is the same data served by the registry's metadata endpoint. It's included in the archive so that the data is verifiable against the source — the registry doesn't invent metadata, it serves what the publisher computed.

### 9.3 Excluded Files

The following are excluded from published archives:
- `test/` directory
- `.clank/` directory
- `clank.lock`
- Files matching `.clankignore` patterns (same format as `.gitignore`)

---

## 10. Security Model

### 10.1 Integrity

Every package archive has a SHA-256 content hash. This hash is:
- Computed at publish time and stored in the registry
- Recorded in `clank.lock` for each resolved package
- Verified on download — mismatched hashes are a hard error

### 10.2 No TOFU

The first time a package is installed, its integrity hash comes from the registry. On subsequent installs (from lock file), the hash is verified against the lock file. This is not TOFU (Trust On First Use) — the registry is the trust anchor, and the lock file is a local cache of that trust.

If the registry is compromised and serves a different archive for the same version+hash, the lock file detects the mismatch. If the registry updates the hash (which it shouldn't — versions are immutable), `clank pkg install` fails and requires explicit `clank pkg update` to accept the new hash.

### 10.3 Version Immutability

Once a version is published, it cannot be modified or replaced. The only remediation for a bad version is:
- Publish a new version with the fix
- File a security advisory (surfaces in `clank pkg audit`)
- Yank the version (removes from resolution but doesn't delete — existing lock files still work)

### 10.4 Yanked Versions

A yanked version:
- Is not selected during resolution (as if it doesn't exist)
- Can still be downloaded if referenced by an existing lock file
- Emits a warning when installed from a lock file: `"net-http@1.2.3 is yanked: use >= 1.2.4"`

---

## 11. Interaction with Module System

### 11.1 Cross-Package Imports

When a package `my-app` depends on `net-http`, modules in `my-app` can import from `net-http` using the package name as a prefix:

```
use net-http.client (fetch, Response)
```

The compiler resolves `net-http` by looking up the package in `.clank/deps/`, then resolves `client` as a module path within that package's `src/` directory.

### 11.2 Coherence Across Packages

Interface coherence (Rule M7 from module-system.md) is enforced across package boundaries at link time. If two packages both provide `impl Show for MyType`, the linker emits a conflict error. The orphan rule (Rule M8) applies per-package: impls are not considered orphan if the type or interface is defined within the same package.

### 11.3 `std` as Implicit Dependency

The `std` package is always available without being listed in `[deps]`. It is resolved as part of the Clank installation, not from the registry. Its version is tied to the Clank compiler version.

---

## 12. Error Codes

Package management errors use the E500 range (module/import errors):

| Code | Description |
|------|-------------|
| E501 | Version conflict — incompatible constraints |
| E502 | Package not found in registry |
| E503 | Version not found for package |
| E504 | Integrity check failed — hash mismatch |
| E505 | Circular dependency between packages |
| E506 | Yanked version in lock file (warning, promoted to error with `--strict`) |
| E507 | Incompatible Clank version |
| E508 | Stale lock file — manifest changed but lock not updated |
| E509 | Registry unreachable |
| E510 | Publish validation failed |

All errors follow the standard diagnostic schema (§3.1 of tooling-spec.md).

---

## 13. Design Rationale

### Why a custom key-value format for `clank.pkg`?

JSON is verbose for a manifest (quotes everywhere, no comments). TOML is better but adds a parser dependency and a learning curve. Clank's key-value format is minimal — fewer tokens for agents to generate and parse. The format has three constructs: `key = "value"`, `[section]`, and lists (`[a, b, c]`). That's it.

### Why not "latest compatible" as default?

MVS selects the minimum satisfying version, not the latest. This means builds are reproducible from manifests alone — no lock file needed for version determinism. "Latest" semantics require a SAT solver and make resolution depend on registry state at resolution time. MVS makes resolution a pure function of the dependency graph.

### Why include effects in lock file and search results?

Effects are the most important behavioral signal in Clank. Knowing that adding `net-http` introduces `<io, async, exn>` effects tells an agent more about the package's runtime behavior than any README. Including effects in metadata makes package evaluation a structured operation, not a reading comprehension task.

### Why type-directed search in the registry?

Agents reason about APIs by type shape: "I need something that takes a list and a predicate and returns a filtered list." Name-based search requires knowing the name. Type-based search lets agents discover packages by the computational shape they need — the same insight behind Hoogle, elevated to a first-class registry feature.

### Why precomputed index.json in archives?

The registry's metadata must match the actual package contents. By computing the export index at publish time (from the type-checked source) and including it in the archive, we ensure the registry serves verified metadata. The alternative — having the registry parse and index source code — is a larger attack surface and a correctness risk.

### Why version immutability?

Mutable versions break MVS's determinism guarantee. If `net-http@1.2.3` could change after resolution, the lock file's integrity hash becomes the only correctness mechanism. Immutability means the version string alone is a stable reference — the hash is defense in depth, not the primary correctness mechanism.
