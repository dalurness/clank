// Clank package manifest (clank.pkg) parser and local dependency resolver
// Implements: manifest parsing, local path dep resolution, pkg init, pkg resolve

import { readFileSync, writeFileSync, existsSync, mkdirSync, readdirSync, statSync } from "node:fs";
import { resolve, dirname, join, relative, basename } from "node:path";

// ── Manifest types ──

export type VersionConstraint = string; // e.g. "1.2", ">= 1.2.0", ">= 1.2, < 2.0"

export type Dependency = {
  name: string;
  constraint: VersionConstraint;
  path?: string; // local path dependency
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
  // Check for path dependency: { path = "../local-pkg" }
  if (valueRaw.startsWith("{")) {
    const inner = valueRaw.slice(1, valueRaw.lastIndexOf("}")).trim();
    const fields = parseInlineTable(inner, lineNum);
    if (!fields.path) {
      throw new PkgError("E508", `Path dependency '${name}' missing 'path' field at line ${lineNum}`);
    }
    return {
      name,
      constraint: fields.version ?? "*",
      path: fields.path,
    };
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
      if (dep.path) {
        lines.push(`${dep.name} = { path = "${dep.path}" }`);
      } else {
        lines.push(`${dep.name} = "${dep.constraint}"`);
      }
    }
  }

  if (manifest.devDeps.size > 0) {
    lines.push("");
    lines.push("[dev-deps]");
    for (const [, dep] of manifest.devDeps) {
      if (dep.path) {
        lines.push(`${dep.name} = { path = "${dep.path}" }`);
      } else {
        lines.push(`${dep.name} = "${dep.constraint}"`);
      }
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
