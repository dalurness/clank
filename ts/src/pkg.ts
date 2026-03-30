// Clank package manifest (clank.pkg) parser and local dependency resolver
// Implements: manifest parsing, local/remote dep resolution, pkg init/install/publish, workspace orchestration

import { readFileSync, writeFileSync, existsSync, mkdirSync, readdirSync, statSync, rmSync, symlinkSync } from "node:fs";
import { resolve, dirname, join, relative, basename, sep } from "node:path";
import { createHash } from "node:crypto";
import { execSync } from "node:child_process";
import { homedir } from "node:os";

// ── Manifest types ──

export type VersionConstraint = string; // e.g. "1.2", ">= 1.2.0", ">= 1.2, < 2.0"

export type Dependency = {
  name: string;
  constraint: VersionConstraint;
  path?: string; // local path dependency
  github?: string; // GitHub repo slug (e.g. "user/repo") — Go-style remote dep
};

export type Manifest = {
  name: string;
  version: string;
  entry?: string;
  description?: string;
  license?: string;
  repository?: string;
  authors: string[];
  clank?: string;
  keywords: string[];
  deps: Map<string, Dependency>;
  devDeps: Map<string, Dependency>;
  effects: Map<string, boolean>;
  exports: string[];
};

// ── Parse errors ──

export class PkgError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
    this.name = "PkgError";
  }
}

// ── Manifest parser ──

type Section = "root" | "deps" | "dev-deps" | "effects" | "exports";

export function parseManifest(source: string, filePath?: string): Manifest {
  const lines = source.split("\n");
  let section: Section = "root";

  const manifest: Manifest = {
    name: "",
    version: "",
    authors: [],
    keywords: [],
    deps: new Map(),
    devDeps: new Map(),
    effects: new Map(),
    exports: [],
  };

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const lineNum = i + 1;

    // Strip comments and whitespace
    const commentIdx = raw.indexOf("#");
    const line = (commentIdx >= 0 ? raw.slice(0, commentIdx) : raw).trim();

    if (line === "") continue;

    // Section header
    if (line.startsWith("[") && line.endsWith("]")) {
      const sectionName = line.slice(1, -1).trim();
      if (sectionName === "deps") section = "deps";
      else if (sectionName === "dev-deps") section = "dev-deps";
      else if (sectionName === "effects") section = "effects";
      else if (sectionName === "exports") section = "exports";
      else throw new PkgError("E508", `Unknown section [${sectionName}] at line ${lineNum}`);
      continue;
    }

    // Key = value
    const eqIdx = line.indexOf("=");
    if (eqIdx < 0) {
      throw new PkgError("E508", `Expected key = value at line ${lineNum}: ${line}`);
    }

    const key = line.slice(0, eqIdx).trim();
    const valueRaw = line.slice(eqIdx + 1).trim();

    switch (section) {
      case "root":
        parseRootField(manifest, key, valueRaw, lineNum);
        break;
      case "deps":
        manifest.deps.set(key, parseDep(key, valueRaw, lineNum));
        break;
      case "dev-deps":
        manifest.devDeps.set(key, parseDep(key, valueRaw, lineNum));
        break;
      case "effects":
        manifest.effects.set(key, parseBool(valueRaw, key, lineNum));
        break;
      case "exports":
        if (key === "modules") {
          manifest.exports = parseList(valueRaw, lineNum);
        } else {
          throw new PkgError("E508", `Unknown exports field '${key}' at line ${lineNum}`);
        }
        break;
    }
  }

  // Validate required fields
  if (!manifest.name) {
    throw new PkgError("E508", `Missing required field 'name' in ${filePath ?? "clank.pkg"}`);
  }
  if (!manifest.version) {
    throw new PkgError("E508", `Missing required field 'version' in ${filePath ?? "clank.pkg"}`);
  }

  // Validate name format
  if (!/^[a-z][a-z0-9-]*$/.test(manifest.name)) {
    throw new PkgError("E508", `Invalid package name '${manifest.name}': must match [a-z][a-z0-9-]* (max 64 chars)`);
  }
  if (manifest.name.length > 64) {
    throw new PkgError("E508", `Package name '${manifest.name}' exceeds 64 character limit`);
  }

  return manifest;
}

function parseRootField(manifest: Manifest, key: string, valueRaw: string, lineNum: number): void {
  switch (key) {
    case "name":
      manifest.name = parseString(valueRaw, key, lineNum);
      break;
    case "version":
      manifest.version = parseString(valueRaw, key, lineNum);
      break;
    case "entry":
      manifest.entry = parseString(valueRaw, key, lineNum);
      break;
    case "description":
      manifest.description = parseString(valueRaw, key, lineNum);
      break;
    case "license":
      manifest.license = parseString(valueRaw, key, lineNum);
      break;
    case "repository":
      manifest.repository = parseString(valueRaw, key, lineNum);
      break;
    case "authors":
      manifest.authors = parseList(valueRaw, lineNum);
      break;
    case "clank":
      manifest.clank = parseString(valueRaw, key, lineNum);
      break;
    case "keywords":
      manifest.keywords = parseList(valueRaw, lineNum);
      break;
    default:
      throw new PkgError("E508", `Unknown field '${key}' at line ${lineNum}`);
  }
}

function parseDep(name: string, valueRaw: string, lineNum: number): Dependency {
  // Check for inline table: { path = "..." } or { github = "user/repo", version = "1.2.3" }
  if (valueRaw.startsWith("{")) {
    const inner = valueRaw.slice(1, valueRaw.lastIndexOf("}")).trim();
    const fields = parseInlineTable(inner, lineNum);
    if (fields.github) {
      return {
        name,
        constraint: fields.version ?? "*",
        github: fields.github,
      };
    }
    if (fields.path) {
      return {
        name,
        constraint: fields.version ?? "*",
        path: fields.path,
      };
    }
    throw new PkgError("E508", `Dependency '${name}' missing 'path' or 'github' field at line ${lineNum}`);
  }

  // Regular version constraint
  return { name, constraint: parseString(valueRaw, name, lineNum) };
}

function parseInlineTable(source: string, lineNum: number): Record<string, string> {
  const result: Record<string, string> = {};
  const parts = source.split(",");
  for (const part of parts) {
    const trimmed = part.trim();
    if (trimmed === "") continue;
    const eqIdx = trimmed.indexOf("=");
    if (eqIdx < 0) {
      throw new PkgError("E508", `Expected key = value in inline table at line ${lineNum}`);
    }
    const k = trimmed.slice(0, eqIdx).trim();
    const v = trimmed.slice(eqIdx + 1).trim();
    result[k] = parseString(v, k, lineNum);
  }
  return result;
}

function parseString(raw: string, field: string, lineNum: number): string {
  if (raw.startsWith('"') && raw.endsWith('"')) {
    return raw.slice(1, -1);
  }
  throw new PkgError("E508", `Expected quoted string for '${field}' at line ${lineNum}, got: ${raw}`);
}

function parseBool(raw: string, field: string, lineNum: number): boolean {
  if (raw === "true") return true;
  if (raw === "false") return false;
  throw new PkgError("E508", `Expected true/false for '${field}' at line ${lineNum}, got: ${raw}`);
}

function parseList(raw: string, lineNum: number): string[] {
  if (!raw.startsWith("[") || !raw.endsWith("]")) {
    throw new PkgError("E508", `Expected list [...] at line ${lineNum}, got: ${raw}`);
  }
  const inner = raw.slice(1, -1).trim();
  if (inner === "") return [];
  return inner.split(",").map(item => {
    const trimmed = item.trim();
    if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
      return trimmed.slice(1, -1);
    }
    return trimmed;
  });
}

// ── Manifest serialization ──

function serializeDep(dep: Dependency): string {
  if (dep.github) {
    const parts = [`github = "${dep.github}"`];
    if (dep.constraint && dep.constraint !== "*") parts.push(`version = "${dep.constraint}"`);
    return `${dep.name} = { ${parts.join(", ")} }`;
  }
  if (dep.path) {
    return `${dep.name} = { path = "${dep.path}" }`;
  }
  return `${dep.name} = "${dep.constraint}"`;
}

export function serializeManifest(manifest: Manifest): string {
  const lines: string[] = [];

  lines.push(`name = "${manifest.name}"`);
  lines.push(`version = "${manifest.version}"`);
  if (manifest.entry) lines.push(`entry = "${manifest.entry}"`);
  if (manifest.description) lines.push(`description = "${manifest.description}"`);
  if (manifest.license) lines.push(`license = "${manifest.license}"`);
  if (manifest.repository) lines.push(`repository = "${manifest.repository}"`);
  if (manifest.authors.length > 0) {
    lines.push(`authors = [${manifest.authors.map(a => `"${a}"`).join(", ")}]`);
  }
  if (manifest.clank) lines.push(`clank = "${manifest.clank}"`);
  if (manifest.keywords.length > 0) {
    lines.push(`keywords = [${manifest.keywords.map(k => `"${k}"`).join(", ")}]`);
  }

  if (manifest.deps.size > 0) {
    lines.push("");
    lines.push("[deps]");
    for (const [, dep] of manifest.deps) {
      lines.push(serializeDep(dep));
    }
  }

  if (manifest.devDeps.size > 0) {
    lines.push("");
    lines.push("[dev-deps]");
    for (const [, dep] of manifest.devDeps) {
      lines.push(serializeDep(dep));
    }
  }

  if (manifest.effects.size > 0) {
    lines.push("");
    lines.push("[effects]");
    for (const [name, val] of manifest.effects) {
      lines.push(`${name} = ${val}`);
    }
  }

  if (manifest.exports.length > 0) {
    lines.push("");
    lines.push("[exports]");
    lines.push(`modules = [${manifest.exports.map(m => `"${m}"`).join(", ")}]`);
  }

  lines.push(""); // trailing newline
  return lines.join("\n");
}

// ── Find manifest ──

export function findManifest(startDir: string): string | null {
  let dir = resolve(startDir);
  while (true) {
    const candidate = join(dir, "clank.pkg");
    if (existsSync(candidate)) return candidate;
    const parent = dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
}

export function loadManifest(manifestPath: string): Manifest {
  const source = readFileSync(manifestPath, "utf-8");
  return parseManifest(source, manifestPath);
}

// ── Local dependency resolution ──

export type ResolvedDep = {
  name: string;
  manifest: Manifest;
  path: string; // absolute path to the dependency root
  modules: Map<string, string>; // module path -> absolute file path
};

export function resolveLocalDeps(
  manifest: Manifest,
  manifestDir: string,
  includeDev: boolean = false,
): ResolvedDep[] {
  const resolved: ResolvedDep[] = [];
  const visited = new Set<string>();

  const depsToResolve = new Map(manifest.deps);
  if (includeDev) {
    for (const [k, v] of manifest.devDeps) {
      depsToResolve.set(k, v);
    }
  }

  for (const [, dep] of depsToResolve) {
    if (!dep.path) continue; // skip registry deps — out of scope
    resolveLocalDep(dep, manifestDir, resolved, visited);
  }

  return resolved;
}

function resolveLocalDep(
  dep: Dependency,
  baseDir: string,
  resolved: ResolvedDep[],
  visited: Set<string>,
): void {
  if (!dep.path) return;

  const depPath = resolve(baseDir, dep.path);

  if (visited.has(depPath)) {
    throw new PkgError("E505", `Circular dependency detected: ${dep.name} at ${depPath}`);
  }
  visited.add(depPath);

  const depManifestPath = join(depPath, "clank.pkg");
  if (!existsSync(depManifestPath)) {
    throw new PkgError("E502", `Dependency '${dep.name}' not found: no clank.pkg at ${depPath}`);
  }

  const depManifest = loadManifest(depManifestPath);

  if (depManifest.name !== dep.name) {
    throw new PkgError(
      "E508",
      `Dependency name mismatch: expected '${dep.name}' but clank.pkg at ${depPath} declares '${depManifest.name}'`,
    );
  }

  // Discover modules in the dependency's src/ directory
  const modules = discoverModules(depPath, dep.name);

  resolved.push({
    name: dep.name,
    manifest: depManifest,
    path: depPath,
    modules,
  });

  // Recursively resolve transitive local deps
  for (const [, transitiveDep] of depManifest.deps) {
    if (transitiveDep.path) {
      resolveLocalDep(transitiveDep, depPath, resolved, visited);
    }
  }
}

// ── Module discovery ──

export function discoverModules(packageDir: string, packageName: string): Map<string, string> {
  const modules = new Map<string, string>();
  const srcDir = join(packageDir, "src");
  if (!existsSync(srcDir)) return modules;

  function walk(dir: string, prefix: string): void {
    const entries = readdirSync(dir);
    for (const entry of entries) {
      const fullPath = join(dir, entry);
      const stat = statSync(fullPath);
      if (stat.isDirectory()) {
        walk(fullPath, prefix ? `${prefix}.${entry}` : entry);
      } else if (entry.endsWith(".clk")) {
        const modName = entry.slice(0, -4); // strip .clk
        const modPath = prefix ? `${prefix}.${modName}` : modName;
        const qualifiedPath = `${packageName}.${modPath}`;
        modules.set(qualifiedPath, fullPath);
      }
    }
  }

  walk(srcDir, "");
  return modules;
}

// ── Package resolution (builds a module map for the compiler) ──

export type PackageResolution = {
  packages: ResolvedDep[];
  moduleMap: Map<string, string>; // qualified module path -> absolute file path
};

export function resolvePackages(
  manifestPath: string,
  includeDev: boolean = false,
): PackageResolution {
  const manifest = loadManifest(manifestPath);
  const manifestDir = dirname(manifestPath);
  const packages = resolveLocalDeps(manifest, manifestDir, includeDev);

  // Also pick up installed deps from .clank/deps/ (GitHub deps installed via pkg install)
  const depsDir = join(manifestDir, ".clank", "deps");
  if (existsSync(depsDir)) {
    const resolvedNames = new Set(packages.map(p => p.name));
    const entries = readdirSync(depsDir);
    for (const entry of entries) {
      if (resolvedNames.has(entry)) continue; // local dep already resolved
      const depPath = join(depsDir, entry);
      const stat = statSync(depPath);
      if (!stat.isDirectory() && !stat.isSymbolicLink()) continue;
      // Resolve symlinks to the actual path
      const realPath = resolve(depPath);
      const depManifestPath = join(realPath, "clank.pkg");
      if (!existsSync(depManifestPath)) continue;
      const depManifest = loadManifest(depManifestPath);
      const modules = discoverModules(realPath, depManifest.name);
      packages.push({
        name: depManifest.name,
        manifest: depManifest,
        path: realPath,
        modules,
      });
    }
  }

  const moduleMap = new Map<string, string>();
  for (const pkg of packages) {
    for (const [modPath, filePath] of pkg.modules) {
      if (moduleMap.has(modPath)) {
        throw new PkgError(
          "E509",
          `Module path collision: '${modPath}' is provided by multiple packages`,
        );
      }
      moduleMap.set(modPath, filePath);
    }
  }

  return { packages, moduleMap };
}

// ── pkg init command ──

export type PkgInitOptions = {
  name?: string;
  entry?: string;
  dir?: string;
};

export function pkgInit(options: PkgInitOptions): {
  ok: boolean;
  data: { package: string; created_files: string[] } | null;
  error?: string;
} {
  const dir = resolve(options.dir ?? ".");
  const name = options.name ?? basename(dir);
  const manifestPath = join(dir, "clank.pkg");

  // Validate name
  if (!/^[a-z][a-z0-9-]*$/.test(name)) {
    return {
      ok: false,
      data: null,
      error: `Invalid package name '${name}': must match [a-z][a-z0-9-]*`,
    };
  }

  if (existsSync(manifestPath)) {
    return {
      ok: false,
      data: null,
      error: `clank.pkg already exists at ${manifestPath}`,
    };
  }

  const manifest: Manifest = {
    name,
    version: "0.1.0",
    authors: [],
    keywords: [],
    deps: new Map(),
    devDeps: new Map(),
    effects: new Map(),
    exports: [],
  };

  if (options.entry) {
    manifest.entry = options.entry;
  }

  const createdFiles: string[] = [];

  // Write manifest
  writeFileSync(manifestPath, serializeManifest(manifest));
  createdFiles.push("clank.pkg");

  // Create src/ directory
  const srcDir = join(dir, "src");
  if (!existsSync(srcDir)) {
    mkdirSync(srcDir, { recursive: true });
  }

  // Generate starter file if entry point specified
  if (options.entry) {
    const entryFile = join(srcDir, `${options.entry}.clk`);
    if (!existsSync(entryFile)) {
      writeFileSync(entryFile, `mod ${name}.${options.entry}\n\nmain : () -> <> ()\nmain = fn () -> print("hello from ${name}")\n`);
      createdFiles.push(`src/${options.entry}.clk`);
    }
  }

  return {
    ok: true,
    data: {
      package: name,
      created_files: createdFiles,
    },
  };
}

// ── Lockfile ──

export type LockPackage = {
  version: string;
  resolved: string; // URL or "path:../relative" for local deps
  integrity: string; // "sha256:<hex>"
  deps: Record<string, string>; // dep name -> resolved version
  effects: string[]; // top-level effects used
};

export type Lockfile = {
  lock_version: number;
  clank_version: string;
  resolved_at: string; // ISO 8601
  packages: Record<string, LockPackage>; // "name@version" -> package record
};

// Backward-compat alias for tests that still reference LockEntry
export type LockEntry = {
  name: string;
  version: string;
  source: string;
  integrity: string;
};

function computeIntegrity(depPath: string): string {
  const hash = createHash("sha256");
  const manifestPath = join(depPath, "clank.pkg");
  if (existsSync(manifestPath)) {
    hash.update(readFileSync(manifestPath));
  }
  const srcDir = join(depPath, "src");
  if (existsSync(srcDir)) {
    hashDir(srcDir, hash);
  }
  return `sha256:${hash.digest("hex")}`;
}

function hashDir(dir: string, hash: ReturnType<typeof createHash>): void {
  const entries = readdirSync(dir).sort();
  for (const entry of entries) {
    const fullPath = join(dir, entry);
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      hashDir(fullPath, hash);
    } else {
      hash.update(entry);
      hash.update(readFileSync(fullPath));
    }
  }
}

export function serializeLockfile(lock: Lockfile): string {
  // Sort packages by key for deterministic output
  const sortedPackages: Record<string, LockPackage> = {};
  const keys = Object.keys(lock.packages).sort();
  for (const key of keys) {
    sortedPackages[key] = lock.packages[key];
  }
  return JSON.stringify(
    { ...lock, packages: sortedPackages },
    null,
    2,
  ) + "\n";
}

export function parseLockfile(source: string): Lockfile {
  const parsed = JSON.parse(source);
  return {
    lock_version: parsed.lock_version ?? 1,
    clank_version: parsed.clank_version ?? "0.1.0",
    resolved_at: parsed.resolved_at ?? new Date().toISOString(),
    packages: parsed.packages ?? {},
  };
}

export const CLANK_VERSION = "0.2.0";

export function generateLockfile(
  manifestPath: string,
  includeDev: boolean = false,
): Lockfile {
  const manifest = loadManifest(manifestPath);
  const manifestDir = dirname(manifestPath);
  const packages = resolveLocalDeps(manifest, manifestDir, includeDev);

  const lockPackages: Record<string, LockPackage> = {};
  for (const pkg of packages) {
    const key = `${pkg.name}@${pkg.manifest.version}`;
    // Collect deps from the resolved package's manifest
    const deps: Record<string, string> = {};
    for (const [depName, dep] of pkg.manifest.deps) {
      deps[depName] = dep.constraint;
    }
    // Collect effects
    const effects: string[] = [];
    for (const [effectName, enabled] of pkg.manifest.effects) {
      if (enabled) effects.push(effectName);
    }
    lockPackages[key] = {
      version: pkg.manifest.version,
      resolved: `path:${relative(manifestDir, pkg.path)}`,
      integrity: computeIntegrity(pkg.path),
      deps,
      effects,
    };
  }

  return {
    lock_version: 1,
    clank_version: CLANK_VERSION,
    resolved_at: new Date().toISOString(),
    packages: lockPackages,
  };
}

export function writeLockfile(manifestPath: string, includeDev: boolean = false): string {
  const lock = generateLockfile(manifestPath, includeDev);
  const lockPath = join(dirname(manifestPath), "clank.lock");
  writeFileSync(lockPath, serializeLockfile(lock));
  return lockPath;
}

export function readLockfile(lockPath: string): Lockfile | null {
  if (!existsSync(lockPath)) return null;
  return parseLockfile(readFileSync(lockPath, "utf-8"));
}

export function verifyLockfile(
  manifestPath: string,
  includeDev: boolean = false,
): { ok: boolean; stale: string[]; missing: string[]; extra: string[] } {
  const manifestDir = dirname(manifestPath);
  const lockPath = join(manifestDir, "clank.lock");
  const lock = readLockfile(lockPath);

  if (!lock) {
    return { ok: false, stale: [], missing: ["clank.lock"], extra: [] };
  }

  const current = generateLockfile(manifestPath, includeDev);

  // Build maps by package name (extracted from "name@version" keys)
  function buildNameMap(packages: Record<string, LockPackage>): Map<string, { key: string; pkg: LockPackage }> {
    const map = new Map<string, { key: string; pkg: LockPackage }>();
    for (const [key, pkg] of Object.entries(packages)) {
      const name = key.split("@")[0];
      map.set(name, { key, pkg });
    }
    return map;
  }

  const lockMap = buildNameMap(lock.packages);
  const currentMap = buildNameMap(current.packages);

  // Also account for GitHub deps declared in the manifest
  const manifest = loadManifest(manifestPath);
  const allDeps = new Map(manifest.deps);
  if (includeDev) {
    for (const [k, v] of manifest.devDeps) allDeps.set(k, v);
  }
  const expectedGithubDeps = new Set<string>();
  for (const [name, dep] of allDeps) {
    if (dep.github) expectedGithubDeps.add(name);
  }

  const stale: string[] = [];
  const missing: string[] = [];
  const extra: string[] = [];

  for (const [name, entry] of currentMap) {
    const locked = lockMap.get(name);
    if (!locked) {
      missing.push(name);
    } else if (locked.pkg.integrity !== entry.pkg.integrity || locked.pkg.version !== entry.pkg.version) {
      stale.push(name);
    }
  }

  // Check that all expected GitHub deps are present in lock file
  for (const name of expectedGithubDeps) {
    if (!lockMap.has(name) && !currentMap.has(name)) {
      missing.push(name);
    }
  }

  for (const name of lockMap.keys()) {
    if (!currentMap.has(name) && !expectedGithubDeps.has(name)) {
      extra.push(name);
    }
  }

  return {
    ok: stale.length === 0 && missing.length === 0 && extra.length === 0,
    stale,
    missing,
    extra,
  };
}

// ── pkg add command ──

export type PkgAddOptions = {
  name: string;
  constraint?: string; // version constraint, defaults to "*"
  path?: string; // local path dependency
  github?: string; // GitHub repo slug (e.g. "user/repo")
  dev?: boolean; // add to [dev-deps] instead of [deps]
  dir?: string;
};

export function pkgAdd(options: PkgAddOptions): {
  ok: boolean;
  data: { name: string; section: string; constraint: string; path?: string; github?: string } | null;
  error?: string;
} {
  const startDir = resolve(options.dir ?? ".");
  const manifestPath = findManifest(startDir);

  if (!manifestPath) {
    return { ok: false, data: null, error: "No clank.pkg found in current directory or any parent" };
  }

  const manifest = loadManifest(manifestPath);
  const section = options.dev ? "dev-deps" : "deps";
  const targetMap = options.dev ? manifest.devDeps : manifest.deps;

  if (targetMap.has(options.name)) {
    return {
      ok: false,
      data: null,
      error: `Dependency '${options.name}' already exists in [${section}]`,
    };
  }

  const dep: Dependency = {
    name: options.name,
    constraint: options.constraint ?? "*",
    path: options.path,
    github: options.github,
  };

  // If it's a path dep, validate the target exists
  if (dep.path) {
    const depDir = resolve(dirname(manifestPath), dep.path);
    const depManifestPath = join(depDir, "clank.pkg");
    if (!existsSync(depManifestPath)) {
      return {
        ok: false,
        data: null,
        error: `No clank.pkg found at ${depDir}`,
      };
    }
    const depManifest = loadManifest(depManifestPath);
    if (depManifest.name !== options.name) {
      return {
        ok: false,
        data: null,
        error: `Package at ${dep.path} declares name '${depManifest.name}', expected '${options.name}'`,
      };
    }
  }

  targetMap.set(options.name, dep);
  writeFileSync(manifestPath, serializeManifest(manifest));

  // Re-resolve and write lockfile after adding dep
  try {
    writeLockfile(manifestPath, options.dev ?? false);
  } catch {
    // Non-fatal — lockfile write failure shouldn't block the add
  }

  return {
    ok: true,
    data: {
      name: options.name,
      section,
      constraint: dep.constraint,
      path: dep.path,
      github: dep.github,
    },
  };
}

// ── pkg remove command ──

export type PkgRemoveOptions = {
  name: string;
  dev?: boolean; // remove from [dev-deps] instead of [deps]
  dir?: string;
};

export function pkgRemove(options: PkgRemoveOptions): {
  ok: boolean;
  data: { name: string; section: string } | null;
  error?: string;
} {
  const startDir = resolve(options.dir ?? ".");
  const manifestPath = findManifest(startDir);

  if (!manifestPath) {
    return { ok: false, data: null, error: "No clank.pkg found in current directory or any parent" };
  }

  const manifest = loadManifest(manifestPath);
  const section = options.dev ? "dev-deps" : "deps";
  const targetMap = options.dev ? manifest.devDeps : manifest.deps;

  if (!targetMap.has(options.name)) {
    return {
      ok: false,
      data: null,
      error: `Dependency '${options.name}' not found in [${section}]`,
    };
  }

  targetMap.delete(options.name);
  writeFileSync(manifestPath, serializeManifest(manifest));

  return {
    ok: true,
    data: {
      name: options.name,
      section,
    },
  };
}

// ── pkg resolve command ──

export function pkgResolve(dir?: string): {
  ok: boolean;
  data: {
    root: string;
    packages: { name: string; version: string; path: string; modules: string[] }[];
  } | null;
  error?: string;
} {
  const startDir = resolve(dir ?? ".");
  const manifestPath = findManifest(startDir);

  if (!manifestPath) {
    return {
      ok: false,
      data: null,
      error: "No clank.pkg found in current directory or any parent",
    };
  }

  try {
    const resolution = resolvePackages(manifestPath, false);

    // Write lockfile on successful resolve
    writeLockfile(manifestPath, false);

    return {
      ok: true,
      data: {
        root: dirname(manifestPath),
        packages: resolution.packages.map(pkg => ({
          name: pkg.name,
          version: pkg.manifest.version,
          path: pkg.path,
          modules: Array.from(pkg.modules.keys()),
        })),
      },
    };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}

// ── Workspace types ──

export type WorkspaceManifest = {
  members: string[]; // raw member paths/globs from clank.workspace
};

export type WorkspaceMember = {
  name: string;
  path: string; // absolute path to member directory
  relativePath: string; // relative to workspace root
  manifest: Manifest;
};

export type WorkspaceGraph = {
  members: Map<string, WorkspaceMember>;
  edges: Map<string, string[]>; // member name -> names of members it depends on
  dependedBy: Map<string, string[]>; // member name -> names of members that depend on it
};

export type WorkspaceResolution = {
  root: string; // absolute workspace root
  members: WorkspaceMember[];
  buildOrder: string[]; // topologically sorted member names
  graph: WorkspaceGraph;
};

// ── Workspace manifest parsing ──

export function parseWorkspaceManifest(source: string, filePath?: string): WorkspaceManifest {
  const lines = source.split("\n");
  let inWorkspace = false;
  let members: string[] | null = null;

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const lineNum = i + 1;
    const commentIdx = raw.indexOf("#");
    const line = (commentIdx >= 0 ? raw.slice(0, commentIdx) : raw).trim();
    if (line === "") continue;

    if (line === "[workspace]") {
      inWorkspace = true;
      continue;
    }

    if (line.startsWith("[") && line.endsWith("]")) {
      throw new PkgError("E508", `Unknown section ${line} in ${filePath ?? "clank.workspace"} at line ${lineNum}`);
    }

    if (!inWorkspace) {
      throw new PkgError("E508", `Expected [workspace] section in ${filePath ?? "clank.workspace"} at line ${lineNum}`);
    }

    const eqIdx = line.indexOf("=");
    if (eqIdx < 0) {
      throw new PkgError("E508", `Expected key = value at line ${lineNum}: ${line}`);
    }

    const key = line.slice(0, eqIdx).trim();
    const valueRaw = line.slice(eqIdx + 1).trim();

    if (key === "members") {
      members = parseList(valueRaw, lineNum);
    } else {
      throw new PkgError("E508", `Unknown workspace field '${key}' at line ${lineNum}`);
    }
  }

  if (!members) {
    throw new PkgError("E508", `Missing required 'members' field in ${filePath ?? "clank.workspace"}`);
  }

  return { members };
}

export function loadWorkspaceManifest(wsPath: string): WorkspaceManifest {
  const source = readFileSync(wsPath, "utf-8");
  return parseWorkspaceManifest(source, wsPath);
}

// ── Workspace root discovery ──

export type WorkspaceContext = {
  mode: "workspace" | "single-package";
  workspaceRoot?: string; // directory containing clank.workspace
  workspaceFile?: string; // path to clank.workspace
  nearestMember?: string; // path to nearest clank.pkg (relative to workspace root)
  nearestMemberDir?: string; // absolute directory of nearest clank.pkg
  manifestPath?: string; // path to nearest clank.pkg (absolute)
};

export function findWorkspaceRoot(startDir: string): WorkspaceContext {
  let dir = resolve(startDir);
  let nearestPkg: string | null = null;

  while (true) {
    const wsCandidate = join(dir, "clank.workspace");
    const pkgCandidate = join(dir, "clank.pkg");

    if (existsSync(wsCandidate)) {
      // Found workspace
      const ctx: WorkspaceContext = {
        mode: "workspace",
        workspaceRoot: dir,
        workspaceFile: wsCandidate,
      };
      if (nearestPkg) {
        ctx.manifestPath = nearestPkg;
        ctx.nearestMemberDir = dirname(nearestPkg);
        ctx.nearestMember = relative(dir, dirname(nearestPkg));
      }
      return ctx;
    }

    if (!nearestPkg && existsSync(pkgCandidate)) {
      nearestPkg = pkgCandidate;
    }

    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }

  // No workspace found
  if (nearestPkg) {
    return {
      mode: "single-package",
      manifestPath: nearestPkg,
      nearestMemberDir: dirname(nearestPkg),
    };
  }

  throw new PkgError("E518", "No clank.pkg or clank.workspace found in directory tree");
}

// ── Member discovery (with glob expansion) ──

export function discoverWorkspaceMembers(
  workspaceRoot: string,
  memberPatterns: string[],
): WorkspaceMember[] {
  const members: WorkspaceMember[] = [];
  const namesSeen = new Map<string, string>(); // name -> relativePath

  for (const pattern of memberPatterns) {
    const expandedPaths = expandMemberPattern(workspaceRoot, pattern);

    for (const relPath of expandedPaths) {
      const absPath = resolve(workspaceRoot, relPath);
      const pkgPath = join(absPath, "clank.pkg");

      if (!existsSync(pkgPath)) {
        throw new PkgError("E515", `Workspace member not found: no clank.pkg at ${relPath}`);
      }

      const manifest = loadManifest(pkgPath);

      if (namesSeen.has(manifest.name)) {
        throw new PkgError(
          "E516",
          `Workspace member name conflict: '${manifest.name}' declared by both '${namesSeen.get(manifest.name)}' and '${relPath}'`,
        );
      }
      namesSeen.set(manifest.name, relPath);

      members.push({
        name: manifest.name,
        path: absPath,
        relativePath: relPath,
        manifest,
      });
    }
  }

  return members;
}

function expandMemberPattern(root: string, pattern: string): string[] {
  // If pattern contains *, expand as glob (immediate subdirectories only)
  if (pattern.includes("*")) {
    const parts = pattern.split("/");
    const starIdx = parts.findIndex(p => p === "*");
    if (starIdx < 0 || starIdx !== parts.length - 1) {
      // Only support trailing /* glob for simplicity
      throw new PkgError("E508", `Unsupported glob pattern '${pattern}': only trailing /* is supported`);
    }

    const baseDir = resolve(root, parts.slice(0, starIdx).join("/"));
    if (!existsSync(baseDir)) return [];

    const results: string[] = [];
    const entries = readdirSync(baseDir);
    for (const entry of entries) {
      const fullPath = join(baseDir, entry);
      const stat = statSync(fullPath);
      if (stat.isDirectory()) {
        const pkgPath = join(fullPath, "clank.pkg");
        if (existsSync(pkgPath)) {
          results.push(relative(root, fullPath));
        }
      }
    }
    return results.sort();
  }

  // Direct path — return as-is
  return [pattern];
}

// ── Dependency graph construction ──

export function buildWorkspaceGraph(members: WorkspaceMember[]): WorkspaceGraph {
  const memberMap = new Map<string, WorkspaceMember>();
  for (const m of members) {
    memberMap.set(m.name, m);
  }

  const edges = new Map<string, string[]>();
  const dependedBy = new Map<string, string[]>();

  for (const m of members) {
    edges.set(m.name, []);
    dependedBy.set(m.name, []);
  }

  for (const m of members) {
    const allDeps = new Map(m.manifest.deps);
    for (const [k, v] of m.manifest.devDeps) {
      allDeps.set(k, v);
    }

    for (const [depName] of allDeps) {
      if (memberMap.has(depName)) {
        edges.get(m.name)!.push(depName);
        dependedBy.get(depName)!.push(m.name);
      }
    }
  }

  return { members: memberMap, edges, dependedBy };
}

// ── Topological sort with cycle detection ──

export function topologicalSort(graph: WorkspaceGraph): string[] {
  const visited = new Set<string>();
  const visiting = new Set<string>(); // for cycle detection
  const result: string[] = [];

  const names = Array.from(graph.members.keys()).sort(); // alphabetical tie-breaking

  function visit(name: string, path: string[]): void {
    if (visited.has(name)) return;
    if (visiting.has(name)) {
      const cycle = [...path.slice(path.indexOf(name)), name];
      throw new PkgError("E512", `Circular dependency in workspace: ${cycle.join(" -> ")}`);
    }

    visiting.add(name);
    path.push(name);

    const deps = graph.edges.get(name) ?? [];
    const sortedDeps = [...deps].sort(); // alphabetical tie-breaking
    for (const dep of sortedDeps) {
      visit(dep, path);
    }

    visiting.delete(name);
    path.pop();
    visited.add(name);
    result.push(name);
  }

  for (const name of names) {
    visit(name, []);
  }

  return result;
}

// ── Workspace resolution ──

export function resolveWorkspace(workspaceRoot: string): WorkspaceResolution {
  const wsPath = join(workspaceRoot, "clank.workspace");
  if (!existsSync(wsPath)) {
    throw new PkgError("E518", `No clank.workspace found at ${workspaceRoot}`);
  }

  const wsMf = loadWorkspaceManifest(wsPath);
  const members = discoverWorkspaceMembers(workspaceRoot, wsMf.members);
  const graph = buildWorkspaceGraph(members);
  const buildOrder = topologicalSort(graph);

  return { root: workspaceRoot, members, buildOrder, graph };
}

// ── Workspace-aware package resolution ──

export function resolveWorkspacePackages(
  workspaceRoot: string,
  includeDev: boolean = false,
): {
  resolution: WorkspaceResolution;
  memberResolutions: Map<string, PackageResolution>;
} {
  const resolution = resolveWorkspace(workspaceRoot);
  const memberResolutions = new Map<string, PackageResolution>();

  // Build a set of workspace member names for cross-member resolution
  const memberNames = new Set(resolution.members.map(m => m.name));

  for (const memberName of resolution.buildOrder) {
    const member = resolution.graph.members.get(memberName)!;
    const memberDir = member.path;
    const manifestPath = join(memberDir, "clank.pkg");

    // For cross-member deps, temporarily treat them as local path deps
    const manifest = loadManifest(manifestPath);
    const depsToResolve = new Map(manifest.deps);
    if (includeDev) {
      for (const [k, v] of manifest.devDeps) {
        depsToResolve.set(k, v);
      }
    }

    // Inject workspace members as path deps for resolution
    for (const [depName, dep] of depsToResolve) {
      if (memberNames.has(depName) && !dep.path) {
        const depMember = resolution.graph.members.get(depName)!;
        dep.path = relative(memberDir, depMember.path);
      }
    }

    const packages = resolveLocalDeps(manifest, memberDir, includeDev);
    const moduleMap = new Map<string, string>();
    for (const pkg of packages) {
      for (const [modPath, filePath] of pkg.modules) {
        moduleMap.set(modPath, filePath);
      }
    }

    memberResolutions.set(memberName, { packages, moduleMap });
  }

  return { resolution, memberResolutions };
}

// ── Parallel build scheduling ──

export type DepthLevel = {
  depth: number;
  members: string[]; // member names at this depth (can run in parallel)
};

/**
 * Compute depth levels from the workspace graph.
 * Members at the same depth have no dependencies between them and can build concurrently.
 */
export function computeDepthLevels(graph: WorkspaceGraph): DepthLevel[] {
  const depths = new Map<string, number>();

  function getDepth(name: string, visiting: Set<string>): number {
    if (depths.has(name)) return depths.get(name)!;
    if (visiting.has(name)) return 0; // cycle (handled elsewhere)
    visiting.add(name);

    const deps = graph.edges.get(name) ?? [];
    const depth = deps.length === 0
      ? 0
      : Math.max(...deps.map(d => getDepth(d, visiting))) + 1;

    depths.set(name, depth);
    visiting.delete(name);
    return depth;
  }

  for (const name of graph.members.keys()) {
    getDepth(name, new Set());
  }

  // Group by depth
  const levelMap = new Map<number, string[]>();
  for (const [name, depth] of depths) {
    if (!levelMap.has(depth)) levelMap.set(depth, []);
    levelMap.get(depth)!.push(name);
  }

  // Sort levels and members within levels (alphabetical)
  const levels: DepthLevel[] = [];
  const sortedDepths = Array.from(levelMap.keys()).sort((a, b) => a - b);
  for (const depth of sortedDepths) {
    levels.push({ depth, members: levelMap.get(depth)!.sort() });
  }

  return levels;
}

export type MemberBuildResult = {
  name: string;
  path: string;
  status: "success" | "failed" | "skipped_dep_failed";
  blockedBy?: string;
  duration_ms: number;
  error?: string;
};

export type WorkspaceBuildResult = {
  ok: boolean;
  membersBuilt: MemberBuildResult[];
  buildOrder: (string | string[])[]; // arrays within = parallel groups
  parallelismAchieved: number;
  totalMs: number;
};

/**
 * Execute parallel workspace builds using a task pool model.
 * buildMember is called for each member — it should perform the actual build
 * (lex/parse/desugar/check/compile) and return success/failure.
 */
export async function buildWorkspaceParallel(
  resolution: WorkspaceResolution,
  buildMember: (member: WorkspaceMember) => Promise<{ ok: boolean; error?: string }>,
  maxJobs: number = 4,
): Promise<WorkspaceBuildResult> {
  const startTime = Date.now();
  const levels = computeDepthLevels(resolution.graph);
  const results: MemberBuildResult[] = [];
  const failedMembers = new Set<string>();
  let maxParallelism = 0;

  // Build order representation for output
  const buildOrderRepr: (string | string[])[] = [];

  for (const level of levels) {
    const eligible = level.members.filter(name => {
      // Check if any dependency failed
      const deps = resolution.graph.edges.get(name) ?? [];
      const failedDep = deps.find(d => failedMembers.has(d));
      if (failedDep) {
        results.push({
          name,
          path: resolution.graph.members.get(name)!.relativePath,
          status: "skipped_dep_failed",
          blockedBy: failedDep,
          duration_ms: 0,
        });
        return false;
      }
      return true;
    });

    if (eligible.length === 0) continue;

    if (eligible.length === 1) {
      buildOrderRepr.push(eligible[0]);
    } else {
      buildOrderRepr.push([...eligible]);
    }

    maxParallelism = Math.max(maxParallelism, eligible.length);

    // Execute in batches of maxJobs
    for (let i = 0; i < eligible.length; i += maxJobs) {
      const batch = eligible.slice(i, i + maxJobs);
      const batchResults = await Promise.all(
        batch.map(async (name) => {
          const member = resolution.graph.members.get(name)!;
          const memberStart = Date.now();
          try {
            const result = await buildMember(member);
            return {
              name,
              path: member.relativePath,
              status: (result.ok ? "success" : "failed") as "success" | "failed",
              duration_ms: Date.now() - memberStart,
              error: result.error,
            };
          } catch (err) {
            return {
              name,
              path: member.relativePath,
              status: "failed" as const,
              duration_ms: Date.now() - memberStart,
              error: err instanceof Error ? err.message : String(err),
            };
          }
        }),
      );

      for (const r of batchResults) {
        results.push(r);
        if (r.status === "failed") {
          failedMembers.add(r.name);
        }
      }
    }
  }

  const ok = results.every(r => r.status === "success");
  return {
    ok,
    membersBuilt: results,
    buildOrder: buildOrderRepr,
    parallelismAchieved: maxParallelism,
    totalMs: Date.now() - startTime,
  };
}

// ── Workspace lockfile ──

/**
 * Generate a single workspace-level lockfile at the workspace root.
 * Merges all member dependencies into one clank.lock.
 */
export function generateWorkspaceLockfile(workspaceRoot: string): Lockfile {
  const resolution = resolveWorkspace(workspaceRoot);
  const lockPackages: Record<string, LockPackage> = {};

  // Collect all member packages as entries
  for (const member of resolution.members) {
    const key = `${member.name}@${member.manifest.version}`;
    const deps: Record<string, string> = {};
    for (const [depName, dep] of member.manifest.deps) {
      deps[depName] = dep.constraint;
    }
    const effects: string[] = [];
    for (const [effectName, enabled] of member.manifest.effects) {
      if (enabled) effects.push(effectName);
    }
    lockPackages[key] = {
      version: member.manifest.version,
      resolved: `workspace:${member.relativePath}`,
      integrity: computeIntegrity(member.path),
      deps,
      effects,
    };
  }

  // Also resolve each member's external deps and merge
  for (const member of resolution.members) {
    const manifestPath = join(member.path, "clank.pkg");
    try {
      const memberLock = generateLockfile(manifestPath, true);
      for (const [key, pkg] of Object.entries(memberLock.packages)) {
        if (!lockPackages[key]) {
          lockPackages[key] = pkg;
        }
      }
    } catch {
      // Member may have no resolvable external deps — that's fine
    }
  }

  return {
    lock_version: 1,
    clank_version: CLANK_VERSION,
    resolved_at: new Date().toISOString(),
    packages: lockPackages,
  };
}

/**
 * Write a workspace-level lockfile (single clank.lock at workspace root).
 */
export function writeWorkspaceLockfile(workspaceRoot: string): string {
  const lock = generateWorkspaceLockfile(workspaceRoot);
  const lockPath = join(workspaceRoot, "clank.lock");
  writeFileSync(lockPath, serializeLockfile(lock));
  return lockPath;
}

// ── Workspace init command ──

export function workspaceInit(options: {
  dir?: string;
  members?: string[];
}): { ok: boolean; data: { root: string; members: string[] } | null; error?: string } {
  const dir = resolve(options.dir ?? ".");
  const wsPath = join(dir, "clank.workspace");

  if (existsSync(wsPath)) {
    return { ok: false, data: null, error: `clank.workspace already exists at ${wsPath}` };
  }

  // Auto-discover members if none specified
  let memberPaths = options.members ?? [];
  if (memberPaths.length === 0) {
    // Look for clank.pkg in immediate subdirectories
    if (existsSync(dir)) {
      const entries = readdirSync(dir);
      for (const entry of entries) {
        const fullPath = join(dir, entry);
        const stat = statSync(fullPath);
        if (stat.isDirectory() && existsSync(join(fullPath, "clank.pkg"))) {
          memberPaths.push(entry);
        }
      }
      memberPaths.sort();
    }
  }

  const lines: string[] = [];
  lines.push("[workspace]");
  lines.push(`members = [${memberPaths.map(m => `"${m}"`).join(", ")}]`);
  lines.push("");

  writeFileSync(wsPath, lines.join("\n"));

  return {
    ok: true,
    data: { root: dir, members: memberPaths },
  };
}

// ── Workspace list command ──

export function workspaceList(dir?: string): {
  ok: boolean;
  data: {
    root: string;
    members: {
      name: string;
      path: string;
      version: string;
      deps_count: number;
      dependents: string[];
    }[];
  } | null;
  error?: string;
} {
  const startDir = resolve(dir ?? ".");
  try {
    const ctx = findWorkspaceRoot(startDir);
    if (ctx.mode !== "workspace" || !ctx.workspaceRoot) {
      return { ok: false, data: null, error: "Not inside a workspace" };
    }

    const resolution = resolveWorkspace(ctx.workspaceRoot);
    const memberData = resolution.members.map(m => ({
      name: m.name,
      path: m.relativePath,
      version: m.manifest.version,
      deps_count: m.manifest.deps.size + m.manifest.devDeps.size,
      dependents: resolution.graph.dependedBy.get(m.name) ?? [],
    }));

    return {
      ok: true,
      data: { root: ctx.workspaceRoot, members: memberData },
    };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}

// ── Workspace add/remove member ──

export function workspaceAddMember(memberPath: string, dir?: string): {
  ok: boolean; data: { member: string; name: string } | null; error?: string;
} {
  const startDir = resolve(dir ?? ".");
  try {
    const ctx = findWorkspaceRoot(startDir);
    if (ctx.mode !== "workspace" || !ctx.workspaceRoot || !ctx.workspaceFile) {
      return { ok: false, data: null, error: "Not inside a workspace" };
    }

    const absPath = resolve(ctx.workspaceRoot, memberPath);
    const pkgPath = join(absPath, "clank.pkg");
    if (!existsSync(pkgPath)) {
      return { ok: false, data: null, error: `No clank.pkg found at ${memberPath}` };
    }

    const manifest = loadManifest(pkgPath);
    const ws = loadWorkspaceManifest(ctx.workspaceFile);

    if (ws.members.includes(memberPath)) {
      return { ok: false, data: null, error: `Member '${memberPath}' already in workspace` };
    }

    ws.members.push(memberPath);
    const lines: string[] = [];
    lines.push("[workspace]");
    lines.push(`members = [${ws.members.map(m => `"${m}"`).join(", ")}]`);
    lines.push("");
    writeFileSync(ctx.workspaceFile, lines.join("\n"));

    return { ok: true, data: { member: memberPath, name: manifest.name } };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}

export function workspaceRemoveMember(memberPath: string, dir?: string): {
  ok: boolean; data: { member: string } | null; error?: string;
} {
  const startDir = resolve(dir ?? ".");
  try {
    const ctx = findWorkspaceRoot(startDir);
    if (ctx.mode !== "workspace" || !ctx.workspaceRoot || !ctx.workspaceFile) {
      return { ok: false, data: null, error: "Not inside a workspace" };
    }

    const ws = loadWorkspaceManifest(ctx.workspaceFile);
    const idx = ws.members.indexOf(memberPath);
    if (idx < 0) {
      return { ok: false, data: null, error: `Member '${memberPath}' not found in workspace` };
    }

    ws.members.splice(idx, 1);
    const lines: string[] = [];
    lines.push("[workspace]");
    lines.push(`members = [${ws.members.map(m => `"${m}"`).join(", ")}]`);
    lines.push("");
    writeFileSync(ctx.workspaceFile, lines.join("\n"));

    return { ok: true, data: { member: memberPath } };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}

// ── Registry protocol ──
// Defines a pluggable registry interface. The default implementation uses GitHub
// as the registry (Go-style: no central server, tags are versions, repos are packages).

export type RegistryPackageInfo = {
  name: string;
  description: string;
  repository: string; // GitHub slug or URL
  versions: string[];
  latest: string;
  license?: string;
  authors: string[];
  keywords: string[];
};

export type RegistrySearchResult = {
  packages: {
    name: string;
    description: string;
    repository: string;
    latest: string;
    keywords: string[];
  }[];
};

export type RegistryPublishEntry = {
  name: string;
  version: string;
  description: string;
  repository: string;
  integrity: string;
  license?: string;
  authors: string[];
  keywords: string[];
  exports: string[];
  deps: Record<string, string>; // dep name -> constraint
  effects: string[]; // enabled effects
};

export type RegistryProtocol = {
  /** List available versions for a package */
  versions(repo: string): string[];
  /** Get full package info from a repository */
  info(repo: string): RegistryPackageInfo | null;
  /** Search packages by query (matches name, description, keywords) */
  search(repos: string[], query: string): RegistrySearchResult;
  /** Build a publish entry from a manifest (for index generation) */
  publishEntry(manifest: Manifest, manifestDir: string): RegistryPublishEntry;
};

/** GitHub-backed registry: repos are packages, git tags are versions */
export function createGitHubRegistry(): RegistryProtocol {
  return {
    versions(repo: string): string[] {
      return listGithubVersions(repo);
    },

    info(repo: string): RegistryPackageInfo | null {
      try {
        const versions = listGithubVersions(repo);
        if (versions.length === 0) return null;
        const latest = versions[versions.length - 1];
        // Fetch the latest version to read its manifest
        const cachePath = fetchGithubPackage(repo, latest);
        const manifestPath = join(cachePath, "clank.pkg");
        if (!existsSync(manifestPath)) return null;
        const manifest = loadManifest(manifestPath);
        return {
          name: manifest.name,
          description: manifest.description ?? "",
          repository: repo,
          versions,
          latest,
          license: manifest.license,
          authors: manifest.authors,
          keywords: manifest.keywords,
        };
      } catch {
        return null;
      }
    },

    search(repos: string[], query: string): RegistrySearchResult {
      const q = query.toLowerCase();
      const results: RegistrySearchResult["packages"] = [];
      for (const repo of repos) {
        try {
          const info = this.info(repo);
          if (!info) continue;
          const haystack = `${info.name} ${info.description} ${info.keywords.join(" ")}`.toLowerCase();
          if (haystack.includes(q)) {
            results.push({
              name: info.name,
              description: info.description,
              repository: info.repository,
              latest: info.latest,
              keywords: info.keywords,
            });
          }
        } catch {
          // skip unreachable repos
        }
      }
      return { packages: results };
    },

    publishEntry(manifest: Manifest, manifestDir: string): RegistryPublishEntry {
      const deps: Record<string, string> = {};
      for (const [name, dep] of manifest.deps) {
        deps[name] = dep.constraint;
      }
      const effects: string[] = [];
      for (const [name, enabled] of manifest.effects) {
        if (enabled) effects.push(name);
      }
      return {
        name: manifest.name,
        version: manifest.version,
        description: manifest.description ?? "",
        repository: manifest.repository ?? "",
        integrity: computeIntegrity(manifestDir),
        license: manifest.license,
        authors: manifest.authors,
        keywords: manifest.keywords,
        exports: manifest.exports,
        deps,
        effects,
      };
    },
  };
}

// ── GitHub remote dependency resolution ──
// Go-style: no central registry, fetch directly from GitHub repos using tarballs

const CACHE_DIR = join(homedir(), ".clank", "cache", "packages");

function ensureCacheDir(): void {
  mkdirSync(CACHE_DIR, { recursive: true });
}

/** Parse a semver-ish version string into comparable parts */
function parseSemver(v: string): { major: number; minor: number; patch: number } | null {
  const m = v.match(/^v?(\d+)\.(\d+)\.(\d+)$/);
  if (!m) return null;
  return { major: parseInt(m[1]), minor: parseInt(m[2]), patch: parseInt(m[3]) };
}

/** Check if a version satisfies a constraint */
export function versionSatisfies(version: string, constraint: string): boolean {
  if (constraint === "*") return true;

  const ver = parseSemver(version);
  if (!ver) return false;

  // Handle compound constraints: ">= 1.2, < 2.0"
  if (constraint.includes(",")) {
    return constraint.split(",").every(c => versionSatisfies(version, c.trim()));
  }

  // Handle ">= X.Y.Z"
  if (constraint.startsWith(">=")) {
    const target = parseSemver(constraint.slice(2).trim());
    if (!target) return false;
    return compareSemver(ver, target) >= 0;
  }

  // Handle "< X.Y.Z"
  if (constraint.startsWith("<")) {
    const target = parseSemver(constraint.slice(1).trim());
    if (!target) return false;
    return compareSemver(ver, target) < 0;
  }

  // Exact version: "1.2.3"
  const exact = parseSemver(constraint);
  if (exact && exact.patch !== undefined) {
    // Full semver — exact match
    if (constraint.match(/^\d+\.\d+\.\d+$/)) {
      return compareSemver(ver, exact) === 0;
    }
  }

  // Compatible range: "1.2" means >= 1.2.0, < 2.0.0
  const parts = constraint.split(".");
  if (parts.length === 2) {
    const major = parseInt(parts[0]);
    const minor = parseInt(parts[1]);
    if (isNaN(major) || isNaN(minor)) return false;
    return ver.major === major && ver.minor >= minor;
  }

  // Major only: "1" means >= 1.0.0, < 2.0.0
  if (parts.length === 1) {
    const major = parseInt(parts[0]);
    if (isNaN(major)) return false;
    return ver.major === major;
  }

  return false;
}

function compareSemver(
  a: { major: number; minor: number; patch: number },
  b: { major: number; minor: number; patch: number },
): number {
  if (a.major !== b.major) return a.major - b.major;
  if (a.minor !== b.minor) return a.minor - b.minor;
  return a.patch - b.patch;
}

/** List available versions from a GitHub repo by listing git tags */
export function listGithubVersions(githubSlug: string): string[] {
  try {
    const output = execSync(
      `git ls-remote --tags "https://github.com/${githubSlug}.git"`,
      { encoding: "utf-8", timeout: 30000, stdio: ["pipe", "pipe", "pipe"] },
    );
    const versions: string[] = [];
    for (const line of output.split("\n")) {
      const match = line.match(/refs\/tags\/v?(\d+\.\d+\.\d+)$/);
      if (match) versions.push(match[1]);
    }
    return versions.sort((a, b) => {
      const av = parseSemver(a)!;
      const bv = parseSemver(b)!;
      return compareSemver(av, bv);
    });
  } catch {
    throw new PkgError("E509", `Failed to list versions from github.com/${githubSlug}`);
  }
}

/** Select minimum version satisfying constraint (MVS) */
export function selectVersion(versions: string[], constraint: string): string | null {
  for (const v of versions) {
    if (versionSatisfies(v, constraint)) return v;
  }
  return null;
}

/** Download a GitHub repo at a specific version tag to the cache */
export function fetchGithubPackage(githubSlug: string, version: string): string {
  ensureCacheDir();
  const name = githubSlug.split("/").pop()!;
  const cacheDir = join(CACHE_DIR, name, version);

  // Already cached
  if (existsSync(join(cacheDir, "clank.pkg"))) {
    return cacheDir;
  }

  mkdirSync(cacheDir, { recursive: true });

  // Try with 'v' prefix tag first, then without
  const tags = [`v${version}`, version];
  let fetched = false;

  for (const tag of tags) {
    try {
      execSync(
        `curl -sL "https://github.com/${githubSlug}/archive/refs/tags/${tag}.tar.gz" | tar xz --strip-components=1 -C "${cacheDir}"`,
        { encoding: "utf-8", timeout: 60000, stdio: ["pipe", "pipe", "pipe"] },
      );
      // Verify we actually got a clank.pkg
      if (existsSync(join(cacheDir, "clank.pkg"))) {
        fetched = true;
        break;
      }
    } catch {
      // try next tag format
    }
  }

  if (!fetched) {
    // Clean up failed attempt
    rmSync(cacheDir, { recursive: true, force: true });
    throw new PkgError("E502", `Failed to fetch package '${name}' from github.com/${githubSlug}@${version}`);
  }

  // Compute and store integrity hash
  const integrity = computeIntegrity(cacheDir);
  writeFileSync(join(cacheDir, "integrity.sha256"), integrity);

  return cacheDir;
}

/** Resolve a single GitHub dependency: select version via MVS, fetch to cache */
export function resolveGithubDep(dep: Dependency): {
  name: string;
  version: string;
  cachePath: string;
  integrity: string;
} {
  if (!dep.github) throw new PkgError("E508", `Dependency '${dep.name}' has no github field`);

  const versions = listGithubVersions(dep.github);
  if (versions.length === 0) {
    throw new PkgError("E503", `No versions found for github.com/${dep.github}`);
  }

  const selected = selectVersion(versions, dep.constraint);
  if (!selected) {
    throw new PkgError("E503", `No version of github.com/${dep.github} satisfies constraint '${dep.constraint}'`);
  }

  const cachePath = fetchGithubPackage(dep.github, selected);
  const integrityFile = join(cachePath, "integrity.sha256");
  const integrity = existsSync(integrityFile)
    ? readFileSync(integrityFile, "utf-8").trim()
    : computeIntegrity(cachePath);

  return { name: dep.name, version: selected, cachePath, integrity };
}

// ── MVS constraint aggregation for GitHub deps ──

type GithubDepConstraint = {
  github: string; // e.g. "user/repo"
  constraints: string[]; // all version constraints from different dependents
};

/**
 * Collect all GitHub dep constraints transitively, then resolve each using MVS.
 * This ensures a shared transitive dep gets the minimum version satisfying ALL constraints.
 */
export function resolveGithubDepsMVS(
  rootDeps: Dependency[],
): {
  installed: { name: string; version: string; github: string; cached: string }[];
  lockEntries: LockEntry[];
} {
  // Phase 1: Collect all constraints by walking the dep graph
  const constraintMap = new Map<string, GithubDepConstraint>();
  const queue: Dependency[] = [...rootDeps];
  const visited = new Set<string>(); // track which deps we've expanded

  while (queue.length > 0) {
    const dep = queue.shift()!;
    if (!dep.github) continue;

    // Add constraint for this dep
    let entry = constraintMap.get(dep.name);
    if (!entry) {
      entry = { github: dep.github, constraints: [] };
      constraintMap.set(dep.name, entry);
    }
    entry.constraints.push(dep.constraint);

    // Only expand transitive deps once per package
    const expandKey = `${dep.github}`;
    if (visited.has(expandKey)) continue;
    visited.add(expandKey);

    // We need to peek at this package's manifest to find transitive deps.
    // First, check if we have a cached version that satisfies ANY constraint so far.
    // If not, we need to fetch one to read its manifest. Use the minimum satisfying
    // the current constraint as a speculative fetch — it may be re-fetched if a
    // tighter constraint is found later, but that's rare.
    try {
      const versions = listGithubVersions(dep.github);
      const selected = selectVersion(versions, dep.constraint);
      if (selected) {
        const cachePath = fetchGithubPackage(dep.github, selected);
        const depManifestPath = join(cachePath, "clank.pkg");
        if (existsSync(depManifestPath)) {
          const depManifest = loadManifest(depManifestPath);
          for (const [, transitiveDep] of depManifest.deps) {
            if (transitiveDep.github) {
              queue.push(transitiveDep);
            }
          }
        }
      }
    } catch {
      // If we can't fetch to explore transitive deps, we'll error during Phase 2
    }
  }

  // Phase 2: For each collected dep, merge constraints and select minimum version
  const installed: { name: string; version: string; github: string; cached: string }[] = [];
  const lockEntries: LockEntry[] = [];

  for (const [name, entry] of constraintMap) {
    const mergedConstraint = mergeConstraints(entry.constraints);
    const dep: Dependency = { name, constraint: mergedConstraint, github: entry.github };
    const resolved = resolveGithubDep(dep);

    installed.push({
      name: resolved.name,
      version: resolved.version,
      github: entry.github,
      cached: resolved.cachePath,
    });
    lockEntries.push({
      name: resolved.name,
      version: resolved.version,
      source: `github:${entry.github}`,
      integrity: resolved.integrity,
    });
  }

  return { installed, lockEntries };
}

/**
 * Merge multiple version constraints into one that satisfies all of them.
 * MVS: the effective constraint is the maximum of all lower bounds.
 * For compatible ranges like "1.2", the lower bound is 1.2.0.
 */
export function mergeConstraints(constraints: string[]): string {
  if (constraints.length === 0) return "*";
  if (constraints.length === 1) return constraints[0];

  // If any constraint is "*", it doesn't restrict — skip it
  const effective = constraints.filter(c => c !== "*");
  if (effective.length === 0) return "*";
  if (effective.length === 1) return effective[0];

  // Extract lower bounds from each constraint and take the maximum
  let maxLower: { major: number; minor: number; patch: number } | null = null;
  let upperBound: { major: number; minor: number; patch: number } | null = null;
  let hasExact = false;
  let exactVersion = "";

  for (const c of effective) {
    // Handle compound constraints by processing each part
    const parts = c.includes(",") ? c.split(",").map(s => s.trim()) : [c];

    for (const part of parts) {
      if (part.startsWith(">=")) {
        const ver = parseSemver(part.slice(2).trim());
        if (ver && (!maxLower || compareSemver(ver, maxLower) > 0)) {
          maxLower = ver;
        }
      } else if (part.startsWith("<")) {
        const ver = parseSemver(part.slice(1).trim());
        if (ver && (!upperBound || compareSemver(ver, upperBound) < 0)) {
          upperBound = ver;
        }
      } else {
        // Bare version or compatible range
        const semver = parseSemver(part);
        if (semver) {
          // Exact version: "1.2.3"
          hasExact = true;
          exactVersion = part;
          if (!maxLower || compareSemver(semver, maxLower) > 0) {
            maxLower = semver;
          }
        } else {
          // Compatible range: "1.2" -> lower bound 1.2.0
          const rangeParts = part.split(".");
          if (rangeParts.length === 2) {
            const major = parseInt(rangeParts[0]);
            const minor = parseInt(rangeParts[1]);
            if (!isNaN(major) && !isNaN(minor)) {
              const lower = { major, minor, patch: 0 };
              if (!maxLower || compareSemver(lower, maxLower) > 0) {
                maxLower = lower;
              }
              // Compatible range implies < (major+1).0.0
              const impliedUpper = { major: major + 1, minor: 0, patch: 0 };
              if (!upperBound || compareSemver(impliedUpper, upperBound) < 0) {
                upperBound = impliedUpper;
              }
            }
          } else if (rangeParts.length === 1) {
            const major = parseInt(rangeParts[0]);
            if (!isNaN(major)) {
              const lower = { major, minor: 0, patch: 0 };
              if (!maxLower || compareSemver(lower, maxLower) > 0) {
                maxLower = lower;
              }
              const impliedUpper = { major: major + 1, minor: 0, patch: 0 };
              if (!upperBound || compareSemver(impliedUpper, upperBound) < 0) {
                upperBound = impliedUpper;
              }
            }
          }
        }
      }
    }
  }

  // If we have an exact version and it's the max, use it
  if (hasExact && maxLower) {
    return exactVersion;
  }

  // Build merged constraint string
  if (!maxLower) return effective[0]; // fallback

  const lowerStr = `${maxLower.major}.${maxLower.minor}.${maxLower.patch}`;
  if (upperBound) {
    const upperStr = `${upperBound.major}.${upperBound.minor}.${upperBound.patch}`;
    return `>= ${lowerStr}, < ${upperStr}`;
  }
  return `>= ${lowerStr}`;
}

// ── pkg install command ──

export type PkgInstallResult = {
  ok: boolean;
  data: {
    installed: { name: string; version: string; github: string; cached: string }[];
    linked: string;
  } | null;
  error?: string;
};

export function pkgInstall(options?: { dir?: string; dev?: boolean }): PkgInstallResult {
  const startDir = resolve(options?.dir ?? ".");
  const manifestPath = findManifest(startDir);

  if (!manifestPath) {
    return { ok: false, data: null, error: "No clank.pkg found in current directory or any parent" };
  }

  try {
    const manifest = loadManifest(manifestPath);
    const manifestDir = dirname(manifestPath);
    const depsDir = join(manifestDir, ".clank", "deps");

    // Collect all GitHub deps (direct)
    const githubDeps: Dependency[] = [];
    for (const [, dep] of manifest.deps) {
      if (dep.github) githubDeps.push(dep);
    }
    if (options?.dev) {
      for (const [, dep] of manifest.devDeps) {
        if (dep.github) githubDeps.push(dep);
      }
    }

    // Resolve GitHub deps using MVS constraint aggregation
    const mvsResult = resolveGithubDepsMVS(githubDeps);
    const installed = mvsResult.installed;
    const lockEntries: LockEntry[] = mvsResult.lockEntries;

    // Also include local deps in lockfile
    const localDeps = resolveLocalDeps(manifest, manifestDir, options?.dev ?? false);
    for (const pkg of localDeps) {
      lockEntries.push({
        name: pkg.name,
        version: pkg.manifest.version,
        source: `path:${relative(manifestDir, pkg.path)}`,
        integrity: computeIntegrity(pkg.path),
      });
    }

    // Create .clank/deps/ with symlinks to cached packages
    if (installed.length > 0 || localDeps.length > 0) {
      mkdirSync(depsDir, { recursive: true });
    }

    for (const inst of installed) {
      const linkPath = join(depsDir, inst.name);
      if (existsSync(linkPath)) rmSync(linkPath, { recursive: true, force: true });
      symlinkSync(inst.cached, linkPath);
    }

    for (const pkg of localDeps) {
      const linkPath = join(depsDir, pkg.name);
      if (existsSync(linkPath)) rmSync(linkPath, { recursive: true, force: true });
      symlinkSync(pkg.path, linkPath);
    }

    // Write lockfile
    if (lockEntries.length > 0) {
      const lockPackages: Record<string, LockPackage> = {};
      for (const entry of lockEntries) {
        const key = `${entry.name}@${entry.version}`;
        lockPackages[key] = {
          version: entry.version,
          resolved: entry.source,
          integrity: entry.integrity,
          deps: {},
          effects: [],
        };
      }
      const lock: Lockfile = {
        lock_version: 1,
        clank_version: CLANK_VERSION,
        resolved_at: new Date().toISOString(),
        packages: lockPackages,
      };
      const lockPath = join(manifestDir, "clank.lock");
      writeFileSync(lockPath, serializeLockfile(lock));
    }

    return {
      ok: true,
      data: { installed, linked: depsDir },
    };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}

// ── pkg publish command ──

export type PkgPublishResult = {
  ok: boolean;
  data: {
    package: string;
    version: string;
    tag: string;
    repository: string;
    integrity: string;
  } | null;
  error?: string;
};

export function pkgPublish(options?: { dir?: string; dryRun?: boolean }): PkgPublishResult {
  const startDir = resolve(options?.dir ?? ".");
  const manifestPath = findManifest(startDir);

  if (!manifestPath) {
    return { ok: false, data: null, error: "No clank.pkg found in current directory or any parent" };
  }

  try {
    const manifest = loadManifest(manifestPath);
    const manifestDir = dirname(manifestPath);

    // Validate required fields for publish
    if (!manifest.name) {
      return { ok: false, data: null, error: "Missing required field 'name' in clank.pkg" };
    }
    if (!manifest.version) {
      return { ok: false, data: null, error: "Missing required field 'version' in clank.pkg" };
    }
    if (!manifest.description) {
      return { ok: false, data: null, error: "Missing required field 'description' for publish" };
    }
    if (!manifest.license) {
      return { ok: false, data: null, error: "Missing required field 'license' for publish" };
    }

    // Run clank check on src/ if it exists
    const srcDir = join(manifestDir, "src");
    if (existsSync(srcDir)) {
      try {
        const checkResult = execSync(
          `npx tsx "${join(manifestDir, "..", "ts", "src", "main.ts")}" check src/ --json 2>/dev/null || npx tsx ts/src/main.ts check "${srcDir}" --json 2>/dev/null || true`,
          { cwd: manifestDir, encoding: "utf-8", timeout: 30000, stdio: ["pipe", "pipe", "pipe"] },
        ).trim();
        if (checkResult) {
          try {
            const parsed = JSON.parse(checkResult);
            if (parsed.ok === false) {
              const errCount = parsed.diagnostics?.filter((d: any) => d.severity === "error")?.length ?? 0;
              if (errCount > 0) {
                return { ok: false, data: null, error: `Pre-publish check failed with ${errCount} error(s) — fix type errors before publishing` };
              }
            }
          } catch {
            // If JSON parsing fails, check output is not structured — skip
          }
        }
      } catch {
        // If check command fails entirely, don't block publish — check may not be available
      }
    }

    // Verify we're in a git repo
    try {
      execSync("git rev-parse --is-inside-work-tree", {
        cwd: manifestDir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
      });
    } catch {
      return { ok: false, data: null, error: "Not inside a git repository — publish requires git" };
    }

    // Check for uncommitted changes
    try {
      const status = execSync("git status --porcelain", {
        cwd: manifestDir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
      }).trim();
      if (status !== "") {
        return { ok: false, data: null, error: "Uncommitted changes — commit before publishing" };
      }
    } catch {
      return { ok: false, data: null, error: "Failed to check git status" };
    }

    const tag = `v${manifest.version}`;

    // Check if tag already exists
    try {
      const existingTags = execSync("git tag --list", {
        cwd: manifestDir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
      }).trim();
      if (existingTags.split("\n").includes(tag)) {
        return { ok: false, data: null, error: `Tag '${tag}' already exists — version ${manifest.version} is already published` };
      }
    } catch {
      // If we can't list tags, continue — tag creation will fail if it exists
    }

    // Get the remote URL for reporting
    let repository = manifest.repository ?? "";
    if (!repository) {
      try {
        repository = execSync("git remote get-url origin", {
          cwd: manifestDir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
        }).trim();
      } catch {
        repository = "unknown";
      }
    }

    const integrity = computeIntegrity(manifestDir);

    if (options?.dryRun) {
      return {
        ok: true,
        data: {
          package: manifest.name,
          version: manifest.version,
          tag,
          repository,
          integrity,
        },
      };
    }

    // Create the version tag
    try {
      execSync(`git tag -a "${tag}" -m "Release ${manifest.name}@${manifest.version}"`, {
        cwd: manifestDir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
      });
    } catch {
      return { ok: false, data: null, error: `Failed to create git tag '${tag}'` };
    }

    // Push the tag
    try {
      execSync(`git push origin "${tag}"`, {
        cwd: manifestDir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
      });
    } catch {
      return { ok: false, data: null, error: `Tag '${tag}' created locally but failed to push — run 'git push origin ${tag}' manually` };
    }

    // Write registry index entry alongside the publish
    const registry = createGitHubRegistry();
    const entry = registry.publishEntry(manifest, manifestDir);
    const indexDir = join(manifestDir, ".clank");
    mkdirSync(indexDir, { recursive: true });
    writeFileSync(
      join(indexDir, "publish-entry.json"),
      JSON.stringify(entry, null, 2) + "\n",
    );

    return {
      ok: true,
      data: {
        package: manifest.name,
        version: manifest.version,
        tag,
        repository,
        integrity,
      },
    };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}

// ── pkg search command ──

export type PkgSearchResult = {
  ok: boolean;
  data: RegistrySearchResult | null;
  error?: string;
};

export function pkgSearch(options: { repos: string[]; query: string }): PkgSearchResult {
  if (!options.query) {
    return { ok: false, data: null, error: "Missing search query" };
  }
  if (options.repos.length === 0) {
    return { ok: false, data: null, error: "No repositories to search — add GitHub repos to search or pass --repo flags" };
  }
  try {
    const registry = createGitHubRegistry();
    const result = registry.search(options.repos, options.query);
    return { ok: true, data: result };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}

// ── pkg info command ──

export type PkgInfoResult = {
  ok: boolean;
  data: RegistryPackageInfo | null;
  error?: string;
};

export function pkgInfo(options: { repo: string }): PkgInfoResult {
  if (!options.repo) {
    return { ok: false, data: null, error: "Missing --repo flag (GitHub slug, e.g. user/repo)" };
  }
  try {
    const registry = createGitHubRegistry();
    const info = registry.info(options.repo);
    if (!info) {
      return { ok: false, data: null, error: `No package found at github.com/${options.repo}` };
    }
    return { ok: true, data: info };
  } catch (err) {
    if (err instanceof PkgError) {
      return { ok: false, data: null, error: err.message };
    }
    throw err;
  }
}
