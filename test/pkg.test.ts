// Tests for clank.pkg manifest parsing, local dep resolution, and pkg commands
// Run with: npx tsx test/pkg.test.ts

import { execSync, spawnSync } from "node:child_process";
import { writeFileSync, mkdirSync, rmSync, existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { parseManifest, serializeManifest, findManifest, resolveLocalDeps, loadManifest, discoverModules, resolvePackages, pkgInit, pkgResolve, pkgAdd, pkgRemove, pkgInstall, pkgPublish, pkgSearch, pkgInfo, createGitHubRegistry, generateLockfile, serializeLockfile, parseLockfile, writeLockfile, readLockfile, verifyLockfile, versionSatisfies, selectVersion, mergeConstraints, CLANK_VERSION, PkgError } from "../src/pkg.js";
import type { LockPackage, Lockfile, RegistryPackageInfo, RegistryPublishEntry, RegistryProtocol } from "../src/pkg.js";


const CLI = join(import.meta.dirname, "..", "src", "main.ts");
const TMP_DIR = join("/tmp", `clank-pkg-test-${Date.now()}`);

let passed = 0;
let failed = 0;

function test(name: string, fn: () => void): void {
  try {
    fn();
    passed++;
    console.log(`  ok - ${name}`);
  } catch (e: unknown) {
    failed++;
    console.log(`  FAIL - ${name}`);
    console.log(`    ${(e as Error).message}`);
  }
}

function assert(cond: boolean, msg: string): void {
  if (!cond) throw new Error(msg);
}

function assertEqual(a: unknown, b: unknown, msg: string): void {
  if (a !== b) throw new Error(`${msg}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
}

function runCLI(args: string, cwd?: string): { stdout: string; stderr: string; exitCode: number } {
  const result = spawnSync("bash", ["-c", `npx tsx ${CLI} ${args}`], {
    encoding: "utf-8",
    stdio: ["pipe", "pipe", "pipe"],
    cwd: cwd ?? TMP_DIR,
  });
  return {
    stdout: (result.stdout ?? "").trim(),
    stderr: (result.stderr ?? "").trim(),
    exitCode: result.status ?? 1,
  };
}

function setupTmpDir(): void {
  if (existsSync(TMP_DIR)) rmSync(TMP_DIR, { recursive: true });
  mkdirSync(TMP_DIR, { recursive: true });
}

function cleanupTmpDir(): void {
  if (existsSync(TMP_DIR)) rmSync(TMP_DIR, { recursive: true });
}

// ── Manifest parsing tests ──

console.log("═══ pkg: manifest parsing ═══");

test("parse minimal manifest", () => {
  const m = parseManifest(`name = "my-app"\nversion = "1.0.0"\n`);
  assertEqual(m.name, "my-app", "name");
  assertEqual(m.version, "1.0.0", "version");
});

test("parse full manifest", () => {
  const source = `
name = "my-app"
version = "1.0.0"
entry = "main"
description = "A test app"
license = "MIT"
repository = "https://github.com/test/my-app"
authors = ["alice", "bob"]
clank = ">= 0.2.0"
keywords = ["http", "server"]

[deps]
net-http = "1.2"
json-schema = "0.3"

[dev-deps]
test-helpers = "0.1"

[effects]
io = true
async = true

[exports]
modules = ["main", "lib.parser"]
`;
  const m = parseManifest(source);
  assertEqual(m.name, "my-app", "name");
  assertEqual(m.version, "1.0.0", "version");
  assertEqual(m.entry, "main", "entry");
  assertEqual(m.description, "A test app", "description");
  assertEqual(m.license, "MIT", "license");
  assertEqual(m.authors.length, 2, "authors count");
  assertEqual(m.authors[0], "alice", "first author");
  assertEqual(m.keywords.length, 2, "keywords count");
  assertEqual(m.deps.size, 2, "deps count");
  assert(m.deps.has("net-http"), "has net-http dep");
  assertEqual(m.deps.get("net-http")!.constraint, "1.2", "net-http constraint");
  assertEqual(m.devDeps.size, 1, "dev-deps count");
  assertEqual(m.effects.size, 2, "effects count");
  assertEqual(m.effects.get("io"), true, "io effect");
  assertEqual(m.exports.length, 2, "exports count");
  assertEqual(m.exports[0], "main", "first export");
});

test("parse path dependency", () => {
  const m = parseManifest(`name = "app"\nversion = "0.1.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);
  assert(m.deps.has("my-lib"), "has my-lib");
  assertEqual(m.deps.get("my-lib")!.path, "../my-lib", "path");
  assertEqual(m.deps.get("my-lib")!.constraint, "*", "default constraint");
});

test("parse comments are stripped", () => {
  const m = parseManifest(`# This is a comment\nname = "app" # inline comment\nversion = "1.0.0"\n`);
  assertEqual(m.name, "app", "name");
});

test("parse empty sections", () => {
  const m = parseManifest(`name = "app"\nversion = "1.0.0"\n\n[deps]\n\n[effects]\n`);
  assertEqual(m.deps.size, 0, "no deps");
  assertEqual(m.effects.size, 0, "no effects");
});

test("error on missing name", () => {
  try {
    parseManifest(`version = "1.0.0"\n`);
    assert(false, "should have thrown");
  } catch (e) {
    assert(e instanceof PkgError, "is PkgError");
    assertEqual((e as PkgError).code, "E508", "error code");
  }
});

test("error on missing version", () => {
  try {
    parseManifest(`name = "app"\n`);
    assert(false, "should have thrown");
  } catch (e) {
    assert(e instanceof PkgError, "is PkgError");
  }
});

test("error on invalid name format", () => {
  try {
    parseManifest(`name = "MyApp"\nversion = "1.0.0"\n`);
    assert(false, "should have thrown");
  } catch (e) {
    assert(e instanceof PkgError, "is PkgError");
  }
});

test("error on unknown section", () => {
  try {
    parseManifest(`name = "app"\nversion = "1.0.0"\n\n[unknown]\n`);
    assert(false, "should have thrown");
  } catch (e) {
    assert(e instanceof PkgError, "is PkgError");
  }
});

test("error on malformed line (no equals)", () => {
  try {
    parseManifest(`name = "app"\nversion = "1.0.0"\nbadline\n`);
    assert(false, "should have thrown");
  } catch (e) {
    assert(e instanceof PkgError, "is PkgError");
  }
});

// ── Serialization tests ──

console.log("");
console.log("═══ pkg: manifest serialization ═══");

test("round-trip minimal manifest", () => {
  const source = `name = "my-app"\nversion = "1.0.0"\n`;
  const m = parseManifest(source);
  const serialized = serializeManifest(m);
  const m2 = parseManifest(serialized);
  assertEqual(m2.name, m.name, "name round-trip");
  assertEqual(m2.version, m.version, "version round-trip");
});

test("round-trip with deps", () => {
  const source = `name = "app"\nversion = "1.0.0"\n\n[deps]\nfoo = "1.0"\nbar = { path = "../bar" }\n`;
  const m = parseManifest(source);
  const serialized = serializeManifest(m);
  const m2 = parseManifest(serialized);
  assertEqual(m2.deps.size, 2, "deps preserved");
  assertEqual(m2.deps.get("bar")!.path, "../bar", "path dep preserved");
});

// ── Local dependency resolution tests ──

console.log("");
console.log("═══ pkg: local dependency resolution ═══");

test("resolve single local dep", () => {
  setupTmpDir();
  try {
    // Create root package
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "my-app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    // Create dep package
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.2.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "utils.clk"),
      `mod my-lib.utils\n`);

    const manifest = loadManifest(join(TMP_DIR, "root", "clank.pkg"));
    const resolved = resolveLocalDeps(manifest, join(TMP_DIR, "root"));

    assertEqual(resolved.length, 1, "one dep resolved");
    assertEqual(resolved[0].name, "my-lib", "dep name");
    assertEqual(resolved[0].manifest.version, "0.2.0", "dep version");
    assert(resolved[0].modules.has("my-lib.utils"), "found utils module");
  } finally {
    cleanupTmpDir();
  }
});

test("resolve transitive local deps", () => {
  setupTmpDir();
  try {
    // root -> lib-a -> lib-b
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nlib-a = { path = "../lib-a" }\n`);

    mkdirSync(join(TMP_DIR, "lib-a", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib-a", "clank.pkg"),
      `name = "lib-a"\nversion = "0.1.0"\n\n[deps]\nlib-b = { path = "../lib-b" }\n`);
    writeFileSync(join(TMP_DIR, "lib-a", "src", "a.clk"), `mod lib-a.a\n`);

    mkdirSync(join(TMP_DIR, "lib-b", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib-b", "clank.pkg"),
      `name = "lib-b"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "lib-b", "src", "b.clk"), `mod lib-b.b\n`);

    const manifest = loadManifest(join(TMP_DIR, "root", "clank.pkg"));
    const resolved = resolveLocalDeps(manifest, join(TMP_DIR, "root"));

    assertEqual(resolved.length, 2, "two deps resolved (transitive)");
    const names = resolved.map(r => r.name);
    assert(names.includes("lib-a"), "has lib-a");
    assert(names.includes("lib-b"), "has lib-b");
  } finally {
    cleanupTmpDir();
  }
});

test("circular dependency detected", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "a", "clank.pkg"),
      `name = "a"\nversion = "1.0.0"\n\n[deps]\nb = { path = "../b" }\n`);

    mkdirSync(join(TMP_DIR, "b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "b", "clank.pkg"),
      `name = "b"\nversion = "1.0.0"\n\n[deps]\na = { path = "../a" }\n`);

    const manifest = loadManifest(join(TMP_DIR, "a", "clank.pkg"));
    try {
      resolveLocalDeps(manifest, join(TMP_DIR, "a"));
      assert(false, "should have thrown");
    } catch (e) {
      assert(e instanceof PkgError, "is PkgError");
      assertEqual((e as PkgError).code, "E505", "circular dep error code");
    }
  } finally {
    cleanupTmpDir();
  }
});

test("missing dependency package", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmissing = { path = "../missing" }\n`);

    const manifest = loadManifest(join(TMP_DIR, "root", "clank.pkg"));
    try {
      resolveLocalDeps(manifest, join(TMP_DIR, "root"));
      assert(false, "should have thrown");
    } catch (e) {
      assert(e instanceof PkgError, "is PkgError");
      assertEqual((e as PkgError).code, "E502", "not found error code");
    }
  } finally {
    cleanupTmpDir();
  }
});

test("name mismatch detected", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nfoo = { path = "../wrong-name" }\n`);

    mkdirSync(join(TMP_DIR, "wrong-name"), { recursive: true });
    writeFileSync(join(TMP_DIR, "wrong-name", "clank.pkg"),
      `name = "bar"\nversion = "1.0.0"\n`);

    const manifest = loadManifest(join(TMP_DIR, "root", "clank.pkg"));
    try {
      resolveLocalDeps(manifest, join(TMP_DIR, "root"));
      assert(false, "should have thrown");
    } catch (e) {
      assert(e instanceof PkgError, "is PkgError");
      assertEqual((e as PkgError).code, "E508", "name mismatch error code");
    }
  } finally {
    cleanupTmpDir();
  }
});

test("skip registry deps (no path)", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nremote-pkg = "1.0"\n`);

    const manifest = loadManifest(join(TMP_DIR, "root", "clank.pkg"));
    const resolved = resolveLocalDeps(manifest, join(TMP_DIR, "root"));
    assertEqual(resolved.length, 0, "no local deps to resolve");
  } finally {
    cleanupTmpDir();
  }
});

// ── Module discovery tests ──

console.log("");
console.log("═══ pkg: module discovery ═══");

test("discover modules in src/", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "pkg", "src", "lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "pkg", "src", "main.clk"), "");
    writeFileSync(join(TMP_DIR, "pkg", "src", "lib", "parser.clk"), "");
    writeFileSync(join(TMP_DIR, "pkg", "src", "lib", "types.clk"), "");

    const modules = discoverModules(join(TMP_DIR, "pkg"), "my-pkg");
    assertEqual(modules.size, 3, "three modules");
    assert(modules.has("my-pkg.main"), "has main");
    assert(modules.has("my-pkg.lib.parser"), "has lib.parser");
    assert(modules.has("my-pkg.lib.types"), "has lib.types");
  } finally {
    cleanupTmpDir();
  }
});

test("empty src/ returns no modules", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "pkg", "src"), { recursive: true });
    const modules = discoverModules(join(TMP_DIR, "pkg"), "my-pkg");
    assertEqual(modules.size, 0, "no modules");
  } finally {
    cleanupTmpDir();
  }
});

test("no src/ returns no modules", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "pkg"), { recursive: true });
    const modules = discoverModules(join(TMP_DIR, "pkg"), "my-pkg");
    assertEqual(modules.size, 0, "no modules");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg init tests ──

console.log("");
console.log("═══ pkg: pkg init ═══");

test("pkg init creates manifest and src/", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "new-pkg");
    mkdirSync(dir, { recursive: true });
    const result = pkgInit({ name: "my-app", dir });

    assert(result.ok, "ok");
    assertEqual(result.data!.package, "my-app", "package name");
    assert(result.data!.created_files.includes("clank.pkg"), "created clank.pkg");
    assert(existsSync(join(dir, "clank.pkg")), "manifest exists");
    assert(existsSync(join(dir, "src")), "src/ exists");

    // Verify manifest is valid
    const m = loadManifest(join(dir, "clank.pkg"));
    assertEqual(m.name, "my-app", "parsed name");
    assertEqual(m.version, "0.1.0", "default version");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg init with entry point creates starter file", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "new-pkg");
    mkdirSync(dir, { recursive: true });
    const result = pkgInit({ name: "my-app", entry: "main", dir });

    assert(result.ok, "ok");
    assert(result.data!.created_files.includes("src/main.clk"), "created main.clk");
    assert(existsSync(join(dir, "src", "main.clk")), "entry file exists");

    const m = loadManifest(join(dir, "clank.pkg"));
    assertEqual(m.entry, "main", "entry set");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg init fails if clank.pkg exists", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "existing");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"), `name = "existing"\nversion = "1.0.0"\n`);

    const result = pkgInit({ name: "existing", dir });
    assert(!result.ok, "should fail");
    assert(result.error!.includes("already exists"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg init fails on invalid name", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "bad");
    mkdirSync(dir, { recursive: true });
    const result = pkgInit({ name: "BadName", dir });
    assert(!result.ok, "should fail");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg resolve tests ──

console.log("");
console.log("═══ pkg: pkg resolve ═══");

test("pkg resolve with local deps", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "core.clk"), "");

    const result = pkgResolve(join(TMP_DIR, "root"));
    assert(result.ok, "ok");
    assertEqual(result.data!.packages.length, 1, "one package");
    assertEqual(result.data!.packages[0].name, "my-lib", "pkg name");
    assert(result.data!.packages[0].modules.includes("my-lib.core"), "module found");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg resolve no manifest", () => {
  setupTmpDir();
  try {
    const result = pkgResolve(TMP_DIR);
    assert(!result.ok, "should fail");
    assert(result.error!.includes("No clank.pkg"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI integration tests ──

console.log("");
console.log("═══ pkg: CLI integration ═══");

test("clank pkg init --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-init");
    mkdirSync(dir, { recursive: true });
    const result = runCLI(`pkg init --name "test-app" --json`, dir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.package, "test-app", "package name");
    assert(envelope.data.created_files.includes("clank.pkg"), "created files");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg init with --entry --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-init-entry");
    mkdirSync(dir, { recursive: true });
    const result = runCLI(`pkg init --name "test-app" --entry "main" --json`, dir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assert(envelope.data.created_files.includes("src/main.clk"), "entry file");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg resolve --json with no manifest", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "no-manifest");
    mkdirSync(dir, { recursive: true });
    const result = runCLI(`pkg resolve --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(!envelope.ok, "not ok");
    assert(envelope.diagnostics.length > 0, "has diagnostics");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg resolve --json with deps", () => {
  setupTmpDir();
  try {
    const rootDir = join(TMP_DIR, "resolve-root");
    mkdirSync(rootDir, { recursive: true });
    writeFileSync(join(rootDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "api.clk"), "");

    const result = runCLI(`pkg resolve --json`, rootDir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.packages.length, 1, "one package");
    assertEqual(envelope.data.packages[0].name, "my-lib", "pkg name");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg unknown subcommand", () => {
  setupTmpDir();
  try {
    const result = runCLI(`pkg badcmd --json`, TMP_DIR);
    const envelope = JSON.parse(result.stdout);
    assert(!envelope.ok, "not ok");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg with no subcommand", () => {
  setupTmpDir();
  try {
    const result = runCLI(`pkg --json`, TMP_DIR);
    const envelope = JSON.parse(result.stdout);
    assert(!envelope.ok, "not ok");
  } finally {
    cleanupTmpDir();
  }
});

// ── Cross-package import tests ──

console.log("");
console.log("═══ pkg: cross-package imports ═══");

test("run file that imports from local dependency (tree-walker)", () => {
  setupTmpDir();
  try {
    // Create library package with a pub function
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : (name: Str) -> <io> () =\n  print("hello " ++ name)\n`);

    // Create root package that depends on my-lib
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\nentry = "main"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);
    writeFileSync(join(TMP_DIR, "app", "src", "main.clk"),
      `mod app.main\n\nuse my-lib.helpers (greet)\n\nmain : () -> <io> () =\n  greet("world")\n`);

    const result = runCLI(`src/main.clk`, join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, `exit code (stderr: ${result.stderr})`);
    assertEqual(result.stdout, "hello world", "output");
  } finally {
    cleanupTmpDir();
  }
});

test("run file that imports from local dependency (VM)", () => {
  setupTmpDir();
  try {
    // Create library package with a pub function
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : (name: Str) -> <io> () =\n  print("hello " ++ name)\n`);

    // Create root package that depends on my-lib
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\nentry = "main"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);
    writeFileSync(join(TMP_DIR, "app", "src", "main.clk"),
      `mod app.main\n\nuse my-lib.helpers (greet)\n\nmain : () -> <io> () =\n  greet("world")\n`);

    const result = runCLI(`--vm src/main.clk`, join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, `exit code (stderr: ${result.stderr})`);
    assertEqual(result.stdout, "hello world", "output");
  } finally {
    cleanupTmpDir();
  }
});

test("run file with transitive cross-package import", () => {
  setupTmpDir();
  try {
    // lib-b has a pub function
    mkdirSync(join(TMP_DIR, "lib-b", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib-b", "clank.pkg"),
      `name = "lib-b"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "lib-b", "src", "core.clk"),
      `mod lib-b.core\n\npub add-one : (x: Int) -> <> Int =\n  x + 1\n`);

    // lib-a depends on lib-b, re-exports via its own function
    mkdirSync(join(TMP_DIR, "lib-a", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib-a", "clank.pkg"),
      `name = "lib-a"\nversion = "0.1.0"\n\n[deps]\nlib-b = { path = "../lib-b" }\n`);
    writeFileSync(join(TMP_DIR, "lib-a", "src", "api.clk"),
      `mod lib-a.api\n\nuse lib-b.core (add-one)\n\npub add-two : (x: Int) -> <> Int =\n  add-one(add-one(x))\n`);

    // app depends on lib-a
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\nentry = "main"\n\n[deps]\nlib-a = { path = "../lib-a" }\n`);
    writeFileSync(join(TMP_DIR, "app", "src", "main.clk"),
      `mod app.main\n\nuse lib-a.api (add-two)\n\nmain : () -> <io> () =\n  print(show(add-two(5)))\n`);

    const result = runCLI(`src/main.clk`, join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, `exit code (stderr: ${result.stderr})`);
    assertEqual(result.stdout, "7", "output");
  } finally {
    cleanupTmpDir();
  }
});

test("cross-package import JSON output", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : (name: Str) -> <io> () =\n  print("hi " ++ name)\n`);

    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\nentry = "main"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);
    writeFileSync(join(TMP_DIR, "app", "src", "main.clk"),
      `mod app.main\n\nuse my-lib.helpers (greet)\n\nmain : () -> <io> () =\n  greet("json")\n`);

    const result = runCLI(`--json src/main.clk`, join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, `exit code (stderr: ${result.stderr})`);
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assert(envelope.data.stdout.includes("hi json"), "stdout contains output");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg resolve includes module map from local deps", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "my-lib", "src", "sub"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "api.clk"), "");
    writeFileSync(join(TMP_DIR, "my-lib", "src", "sub", "deep.clk"), "");

    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    const result = pkgResolve(join(TMP_DIR, "root"));
    assert(result.ok, "ok");
    const pkg = result.data!.packages[0];
    assert(pkg.modules.includes("my-lib.api"), "has my-lib.api");
    assert(pkg.modules.includes("my-lib.sub.deep"), "has my-lib.sub.deep");
  } finally {
    cleanupTmpDir();
  }
});

// ── Lockfile tests ──

console.log("");
console.log("═══ pkg: lockfile ═══");

test("generate and parse lockfile round-trip", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.2.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "utils.clk"), `mod my-lib.utils\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assertEqual(lock.lock_version, 1, "lockfile version");
    const keys = Object.keys(lock.packages);
    assertEqual(keys.length, 1, "one package");
    const pkg = lock.packages["my-lib@0.2.0"];
    assert(pkg !== undefined, "package key is my-lib@0.2.0");
    assertEqual(pkg.version, "0.2.0", "entry version");
    assertEqual(pkg.resolved, "path:../my-lib", "entry resolved");
    assert(pkg.integrity.startsWith("sha256:"), "has integrity hash");

    // Round-trip
    const serialized = serializeLockfile(lock);
    const parsed = parseLockfile(serialized);
    assertEqual(parsed.lock_version, 1, "parsed version");
    const parsedKeys = Object.keys(parsed.packages);
    assertEqual(parsedKeys.length, 1, "parsed packages count");
    const parsedPkg = parsed.packages["my-lib@0.2.0"];
    assertEqual(parsedPkg.version, pkg.version, "version preserved");
    assertEqual(parsedPkg.resolved, pkg.resolved, "resolved preserved");
    assertEqual(parsedPkg.integrity, pkg.integrity, "integrity preserved");
  } finally {
    cleanupTmpDir();
  }
});

test("writeLockfile creates clank.lock", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const lockPath = writeLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assert(existsSync(lockPath), "clank.lock created");
    assert(lockPath.endsWith("clank.lock"), "correct filename");

    const content = readFileSync(lockPath, "utf-8");
    assert(content.includes("my-lib"), "contains dep name");
    assert(content.includes("sha256:"), "contains integrity");
  } finally {
    cleanupTmpDir();
  }
});

test("readLockfile returns null for missing file", () => {
  const result = readLockfile("/tmp/nonexistent-clank.lock");
  assertEqual(result, null, "returns null");
});

test("verifyLockfile detects stale lockfile", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "a.clk"), "mod my-lib.a\n");

    // Write lockfile
    writeLockfile(join(TMP_DIR, "root", "clank.pkg"));

    // Verify — should be ok
    let result = verifyLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assert(result.ok, "lockfile valid initially");

    // Modify the dep
    writeFileSync(join(TMP_DIR, "my-lib", "src", "a.clk"), "mod my-lib.a\n# changed\n");

    // Verify — should detect stale
    result = verifyLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assert(!result.ok, "lockfile stale after change");
    assert(result.stale.includes("my-lib"), "my-lib is stale");
  } finally {
    cleanupTmpDir();
  }
});

test("verifyLockfile detects missing lockfile", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = verifyLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assert(!result.ok, "not ok without lockfile");
    assert(result.missing.includes("clank.lock"), "missing clank.lock");
  } finally {
    cleanupTmpDir();
  }
});

test("verifyLockfile detects missing dep in lock", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    // Write lockfile with no deps
    writeLockfile(join(TMP_DIR, "root", "clank.pkg"));

    // Now add a dep to manifest
    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    const result = verifyLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assert(!result.ok, "not ok with missing dep");
    assert(result.missing.includes("my-lib"), "my-lib missing from lock");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile with multiple deps sorted alphabetically", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nzeta-lib = { path = "../zeta-lib" }\nalpha-lib = { path = "../alpha-lib" }\n`);

    mkdirSync(join(TMP_DIR, "zeta-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "zeta-lib", "clank.pkg"),
      `name = "zeta-lib"\nversion = "0.1.0"\n`);

    mkdirSync(join(TMP_DIR, "alpha-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "alpha-lib", "clank.pkg"),
      `name = "alpha-lib"\nversion = "0.2.0"\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const keys = Object.keys(lock.packages);
    assertEqual(keys.length, 2, "two packages");

    const serialized = serializeLockfile(lock);
    const alphaIdx = serialized.indexOf("alpha-lib@");
    const zetaIdx = serialized.indexOf("zeta-lib@");
    assert(alphaIdx < zetaIdx, "alpha-lib before zeta-lib in output");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg resolve writes lockfile", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const result = pkgResolve(join(TMP_DIR, "root"));
    assert(result.ok, "resolve ok");
    assert(existsSync(join(TMP_DIR, "root", "clank.lock")), "clank.lock created by resolve");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg add tests ──

console.log("");
console.log("═══ pkg: pkg add ═══");

test("pkg add version dep", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "add-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgAdd({ name: "http-client", constraint: "1.2", dir });
    assert(result.ok, "ok");
    assertEqual(result.data!.name, "http-client", "dep name");
    assertEqual(result.data!.section, "deps", "section");
    assertEqual(result.data!.constraint, "1.2", "constraint");

    // Verify manifest updated
    const m = loadManifest(join(dir, "clank.pkg"));
    assert(m.deps.has("http-client"), "dep in manifest");
    assertEqual(m.deps.get("http-client")!.constraint, "1.2", "constraint persisted");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add path dep", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "add-path-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const result = pkgAdd({ name: "my-lib", path: "../my-lib", dir });
    assert(result.ok, "ok");
    assertEqual(result.data!.path, "../my-lib", "path");

    const m = loadManifest(join(dir, "clank.pkg"));
    assert(m.deps.has("my-lib"), "dep in manifest");
    assertEqual(m.deps.get("my-lib")!.path, "../my-lib", "path persisted");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add dev dep", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "add-dev-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgAdd({ name: "test-utils", constraint: "0.1", dev: true, dir });
    assert(result.ok, "ok");
    assertEqual(result.data!.section, "dev-deps", "section");

    const m = loadManifest(join(dir, "clank.pkg"));
    assert(m.devDeps.has("test-utils"), "dev dep in manifest");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add fails for duplicate", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "add-dup-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nfoo = "1.0"\n`);

    const result = pkgAdd({ name: "foo", constraint: "2.0", dir });
    assert(!result.ok, "should fail");
    assert(result.error!.includes("already exists"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add fails for missing path dep", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "add-missing-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgAdd({ name: "missing", path: "../missing", dir });
    assert(!result.ok, "should fail");
    assert(result.error!.includes("No clank.pkg"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add fails for name mismatch on path dep", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "add-mismatch-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    mkdirSync(join(TMP_DIR, "wrong"), { recursive: true });
    writeFileSync(join(TMP_DIR, "wrong", "clank.pkg"),
      `name = "actual-name"\nversion = "0.1.0"\n`);

    const result = pkgAdd({ name: "expected-name", path: "../wrong", dir });
    assert(!result.ok, "should fail");
    assert(result.error!.includes("actual-name"), "error shows actual name");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add no manifest", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "no-manifest");
    mkdirSync(dir, { recursive: true });

    const result = pkgAdd({ name: "foo", dir });
    assert(!result.ok, "should fail");
    assert(result.error!.includes("No clank.pkg"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI integration tests for pkg add ──

console.log("");
console.log("═══ pkg: CLI pkg add ═══");

test("clank pkg add --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-add");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI(`pkg add --name "http-client" --version "1.0" --json`, dir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.name, "http-client", "name");
    assertEqual(envelope.data.section, "deps", "section");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg add --path --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-add-path");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const result = runCLI(`pkg add --name "my-lib" --path "../my-lib" --json`, dir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.path, "../my-lib", "path");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg add --dev --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-add-dev");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI(`pkg add --name "test-lib" --version "0.1" --dev --json`, dir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.section, "dev-deps", "section");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg add missing --name --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-add-noname");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI(`pkg add --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(!envelope.ok, "not ok");
    assert(envelope.diagnostics.length > 0, "has diagnostics");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg remove tests ──

console.log("");
console.log("═══ pkg: pkg remove ═══");

test("pkg remove dep", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "remove-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nfoo = "1.0"\nbar = "2.0"\n`);

    const result = pkgRemove({ name: "foo", dir });
    assert(result.ok, "ok");
    assertEqual(result.data!.name, "foo", "removed name");
    assertEqual(result.data!.section, "deps", "section");

    const m = loadManifest(join(dir, "clank.pkg"));
    assert(!m.deps.has("foo"), "foo removed");
    assert(m.deps.has("bar"), "bar preserved");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg remove dev dep", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "remove-dev-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[dev-deps]\ntest-utils = "0.1"\n`);

    const result = pkgRemove({ name: "test-utils", dev: true, dir });
    assert(result.ok, "ok");
    assertEqual(result.data!.section, "dev-deps", "section");

    const m = loadManifest(join(dir, "clank.pkg"));
    assert(!m.devDeps.has("test-utils"), "removed");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg remove nonexistent dep fails", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "remove-missing-test");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgRemove({ name: "nonexistent", dir });
    assert(!result.ok, "should fail");
    assert(result.error!.includes("not found"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg remove no manifest", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "no-manifest");
    mkdirSync(dir, { recursive: true });

    const result = pkgRemove({ name: "foo", dir });
    assert(!result.ok, "should fail");
    assert(result.error!.includes("No clank.pkg"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI integration tests for pkg remove ──

console.log("");
console.log("═══ pkg: CLI pkg remove ═══");

test("clank pkg remove --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-remove");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nfoo = "1.0"\n`);

    const result = runCLI(`pkg remove --name "foo" --json`, dir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.name, "foo", "name");
    assertEqual(envelope.data.section, "deps", "section");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg remove missing --name --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-remove-noname");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI(`pkg remove --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(!envelope.ok, "not ok");
    assert(envelope.diagnostics.length > 0, "has diagnostics");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI integration tests for pkg verify ──

console.log("");
console.log("═══ pkg: CLI pkg verify ═══");

test("clank pkg verify --json lockfile ok", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-verify-ok");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    // Write lockfile first
    writeLockfile(join(dir, "clank.pkg"));

    const result = runCLI(`pkg verify --json`, dir);
    assertEqual(result.exitCode, 0, "exit code");
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assert(envelope.data.ok, "lockfile ok");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg verify --json lockfile stale", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-verify-stale");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "a.clk"), "mod my-lib.a\n");

    // Write lockfile
    writeLockfile(join(dir, "clank.pkg"));

    // Modify the dep to make lockfile stale
    writeFileSync(join(TMP_DIR, "my-lib", "src", "a.clk"), "mod my-lib.a\n# changed\n");

    const result = runCLI(`pkg verify --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(!envelope.ok, "not ok");
    assert(envelope.data.stale.includes("my-lib"), "my-lib stale");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg verify no manifest --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-verify-nomanifest");
    mkdirSync(dir, { recursive: true });

    const result = runCLI(`pkg verify --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(!envelope.ok, "not ok");
  } finally {
    cleanupTmpDir();
  }
});

// ── Stale lockfile warning on run ──

console.log("");
console.log("═══ pkg: stale lockfile warning on run ═══");

test("stale lockfile warning on clank run", () => {
  setupTmpDir();
  try {
    // Create library
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : (name: Str) -> <io> () =\n  print("hello " ++ name)\n`);

    // Create app
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\nentry = "main"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);
    writeFileSync(join(TMP_DIR, "app", "src", "main.clk"),
      `mod app.main\n\nuse my-lib.helpers (greet)\n\nmain : () -> <io> () =\n  greet("world")\n`);

    // Write lockfile
    writeLockfile(join(TMP_DIR, "app", "clank.pkg"));

    // Modify the dep to make lockfile stale
    writeFileSync(join(TMP_DIR, "my-lib", "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : (name: Str) -> <io> () =\n  print("hello " ++ name)\n# changed\n`);

    // Run the app — should still work but emit warning to stderr
    const result = runCLI(`src/main.clk`, join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, "exit code");
    assertEqual(result.stdout, "hello world", "output still works");
    assert(result.stderr.includes("clank.lock is out of date"), "stale warning in stderr");
  } finally {
    cleanupTmpDir();
  }
});

test("no warning when lockfile is current", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : (name: Str) -> <io> () =\n  print("hi")\n`);

    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\nentry = "main"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);
    writeFileSync(join(TMP_DIR, "app", "src", "main.clk"),
      `mod app.main\n\nuse my-lib.helpers (greet)\n\nmain : () -> <io> () =\n  greet("x")\n`);

    // Write lockfile — should be current
    writeLockfile(join(TMP_DIR, "app", "clank.pkg"));

    const result = runCLI(`src/main.clk`, join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, "exit code");
    assertEqual(result.stderr, "", "no warnings");
  } finally {
    cleanupTmpDir();
  }
});

// ── GitHub dep parsing tests ──

console.log("");
console.log("═══ pkg: github dependency parsing ═══");

test("parse github dependency", () => {
  const m = parseManifest(`name = "app"\nversion = "0.1.0"\n\n[deps]\nmy-lib = { github = "user/my-lib", version = "1.2.3" }\n`);
  assert(m.deps.has("my-lib"), "has my-lib");
  assertEqual(m.deps.get("my-lib")!.github, "user/my-lib", "github slug");
  assertEqual(m.deps.get("my-lib")!.constraint, "1.2.3", "version constraint");
  assertEqual(m.deps.get("my-lib")!.path, undefined, "no path");
});

test("parse github dependency without version", () => {
  const m = parseManifest(`name = "app"\nversion = "0.1.0"\n\n[deps]\nmy-lib = { github = "user/my-lib" }\n`);
  assertEqual(m.deps.get("my-lib")!.github, "user/my-lib", "github slug");
  assertEqual(m.deps.get("my-lib")!.constraint, "*", "default constraint");
});

test("serialize github dependency roundtrip", () => {
  const m = parseManifest(`name = "app"\nversion = "0.1.0"\n\n[deps]\nmy-lib = { github = "user/my-lib", version = "1.2" }\n`);
  const serialized = serializeManifest(m);
  assert(serialized.includes(`my-lib = { github = "user/my-lib", version = "1.2" }`), "github dep serialized");
  // Re-parse to verify roundtrip
  const m2 = parseManifest(serialized);
  assertEqual(m2.deps.get("my-lib")!.github, "user/my-lib", "roundtrip github");
  assertEqual(m2.deps.get("my-lib")!.constraint, "1.2", "roundtrip constraint");
});

test("error on dep missing both path and github in inline table", () => {
  try {
    parseManifest(`name = "app"\nversion = "0.1.0"\n\n[deps]\nmy-lib = { version = "1.0" }\n`);
    assert(false, "should have thrown");
  } catch (e) {
    assert(e instanceof PkgError, "is PkgError");
  }
});

// ── Version constraint tests ──

console.log("");
console.log("═══ pkg: version constraint matching ═══");

test("versionSatisfies — wildcard", () => {
  assert(versionSatisfies("1.2.3", "*"), "wildcard matches");
});

test("versionSatisfies — exact version", () => {
  assert(versionSatisfies("1.2.3", "1.2.3"), "exact match");
  assert(!versionSatisfies("1.2.4", "1.2.3"), "no match different patch");
});

test("versionSatisfies — compatible range", () => {
  assert(versionSatisfies("1.2.0", "1.2"), "min match");
  assert(versionSatisfies("1.3.0", "1.2"), "higher minor match");
  assert(!versionSatisfies("2.0.0", "1.2"), "major bump no match");
  assert(!versionSatisfies("1.1.0", "1.2"), "lower minor no match");
});

test("versionSatisfies — major only", () => {
  assert(versionSatisfies("1.0.0", "1"), "major match");
  assert(versionSatisfies("1.9.9", "1"), "same major match");
  assert(!versionSatisfies("2.0.0", "1"), "different major no match");
});

test("versionSatisfies — >= constraint", () => {
  assert(versionSatisfies("1.2.0", ">= 1.2.0"), "exact boundary");
  assert(versionSatisfies("1.3.0", ">= 1.2.0"), "above boundary");
  assert(!versionSatisfies("1.1.0", ">= 1.2.0"), "below boundary");
});

test("versionSatisfies — < constraint", () => {
  assert(versionSatisfies("1.9.9", "< 2.0.0"), "below upper");
  assert(!versionSatisfies("2.0.0", "< 2.0.0"), "at upper");
});

test("versionSatisfies — compound constraint", () => {
  assert(versionSatisfies("1.3.0", ">= 1.2.0, < 2.0.0"), "in range");
  assert(!versionSatisfies("2.0.0", ">= 1.2.0, < 2.0.0"), "above range");
  assert(!versionSatisfies("1.1.0", ">= 1.2.0, < 2.0.0"), "below range");
});

// ── MVS version selection tests ──

console.log("");
console.log("═══ pkg: MVS version selection ═══");

test("selectVersion picks minimum satisfying", () => {
  const versions = ["0.1.0", "0.2.0", "1.0.0", "1.1.0", "1.2.0", "2.0.0"];
  assertEqual(selectVersion(versions, "1"), "1.0.0", "min for major 1");
  assertEqual(selectVersion(versions, "1.1"), "1.1.0", "min for 1.1");
  assertEqual(selectVersion(versions, ">= 1.0.0"), "1.0.0", "min for >= 1.0.0");
  assertEqual(selectVersion(versions, ">= 1.1.0, < 2.0.0"), "1.1.0", "min for range");
});

test("selectVersion returns null when no match", () => {
  const versions = ["0.1.0", "0.2.0"];
  assertEqual(selectVersion(versions, "1"), null, "no match for major 1");
  assertEqual(selectVersion(versions, ">= 1.0.0"), null, "no match for >= 1.0.0");
});

// ── pkg install tests (local only — no network) ──

console.log("");
console.log("═══ pkg: install command (local deps) ═══");

test("pkg install with local deps creates .clank/deps/ symlinks", () => {
  setupTmpDir();
  try {
    // Create a library
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : () -> <> () = fn () -> print("hi")\n`);

    // Create an app that depends on it
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    const result = pkgInstall({ dir: join(TMP_DIR, "app") });
    assert(result.ok, "install ok");
    // Local deps should be linked
    assert(existsSync(join(TMP_DIR, "app", ".clank", "deps", "my-lib")), "symlink created");
    // Lockfile should be written
    assert(existsSync(join(TMP_DIR, "app", "clank.lock")), "lockfile written");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg install with no deps succeeds with empty list", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgInstall({ dir: join(TMP_DIR, "app") });
    assert(result.ok, "install ok");
    assertEqual(result.data!.installed.length, 0, "no installed");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg publish tests (dry-run only — no git push) ──

console.log("");
console.log("═══ pkg: publish command ═══");

test("pkg publish --dry-run validates manifest fields", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const result = pkgPublish({ dir: join(TMP_DIR, "lib"), dryRun: true });
    assert(!result.ok, "should fail without description");
    assert(result.error!.includes("description"), "error mentions description");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg publish --dry-run fails without license", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\ndescription = "A lib"\n`);

    const result = pkgPublish({ dir: join(TMP_DIR, "lib"), dryRun: true });
    assert(!result.ok, "should fail without license");
    assert(result.error!.includes("license"), "error mentions license");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg publish --dry-run succeeds in git repo with complete manifest", () => {
  setupTmpDir();
  try {
    const libDir = join(TMP_DIR, "lib");
    mkdirSync(join(libDir, "src"), { recursive: true });
    writeFileSync(join(libDir, "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\ndescription = "A test lib"\nlicense = "MIT"\n`);
    writeFileSync(join(libDir, "src", "main.clk"), `mod my-lib.main\n`);

    // Initialize a git repo
    const execOpts = { cwd: libDir, encoding: "utf-8" as const, stdio: ["pipe", "pipe", "pipe"] as const };
    execSync("git init", execOpts);
    execSync('git config user.email "test@test.com"', execOpts);
    execSync('git config user.name "Test"', execOpts);
    execSync("git add -A", execOpts);
    execSync('git commit -m "init"', execOpts);

    const result = pkgPublish({ dir: libDir, dryRun: true });
    assert(result.ok, `should succeed: ${result.error}`);
    assertEqual(result.data!.package, "my-lib", "package name");
    assertEqual(result.data!.version, "0.1.0", "version");
    assertEqual(result.data!.tag, "v0.1.0", "tag");
    assert(result.data!.integrity.startsWith("sha256:"), "has integrity");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg publish fails outside git repo", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\ndescription = "A test lib"\nlicense = "MIT"\n`);

    const result = pkgPublish({ dir: join(TMP_DIR, "lib"), dryRun: true });
    assert(!result.ok, "should fail outside git repo");
    assert(result.error!.includes("git"), "error mentions git");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI integration tests for install/publish ──

console.log("");
console.log("═══ pkg: CLI install/publish ═══");

test("clank pkg install --json with no deps", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI("pkg install --json", join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, "exit code");
    const parsed = JSON.parse(result.stdout);
    assert(parsed.ok, "ok");
    assertEqual(parsed.data.installed.length, 0, "no installed");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg publish --dry-run --json validates manifest", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const result = runCLI("pkg publish --dry-run --json", join(TMP_DIR, "lib"));
    const parsed = JSON.parse(result.stdout);
    assert(!parsed.ok, "should fail");
    assert(parsed.diagnostics.length > 0, "has diagnostics");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg add --github tests ──

console.log("");
console.log("═══ pkg: pkg add with --github ═══");

test("pkg add with github option writes github dep to manifest", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgAdd({
      name: "my-lib",
      github: "user/my-lib",
      constraint: "1.2",
      dir: join(TMP_DIR, "app"),
    });
    assert(result.ok, `add should succeed: ${result.error}`);
    assertEqual(result.data!.name, "my-lib", "name");
    assertEqual(result.data!.github, "user/my-lib", "github");
    assertEqual(result.data!.constraint, "1.2", "constraint");

    // Verify manifest was updated
    const manifest = loadManifest(join(TMP_DIR, "app", "clank.pkg"));
    const dep = manifest.deps.get("my-lib");
    assert(dep !== undefined, "dep exists in manifest");
    assertEqual(dep!.github, "user/my-lib", "github slug in manifest");
    assertEqual(dep!.constraint, "1.2", "constraint in manifest");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add with github option defaults constraint to *", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgAdd({
      name: "my-lib",
      github: "user/my-lib",
      dir: join(TMP_DIR, "app"),
    });
    assert(result.ok, `add should succeed: ${result.error}`);
    assertEqual(result.data!.constraint, "*", "default constraint");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add with github --dev adds to dev-deps", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = pkgAdd({
      name: "test-helpers",
      github: "user/test-helpers",
      dev: true,
      dir: join(TMP_DIR, "app"),
    });
    assert(result.ok, `add should succeed: ${result.error}`);
    assertEqual(result.data!.section, "dev-deps", "section");

    const manifest = loadManifest(join(TMP_DIR, "app", "clank.pkg"));
    assert(manifest.devDeps.has("test-helpers"), "in dev-deps");
    assertEqual(manifest.devDeps.get("test-helpers")!.github, "user/test-helpers", "github in dev-deps");
  } finally {
    cleanupTmpDir();
  }
});

// ── resolvePackages with installed deps ──

console.log("");
console.log("═══ pkg: resolvePackages with .clank/deps ═══");

test("resolvePackages picks up installed deps from .clank/deps/", () => {
  setupTmpDir();
  try {
    // Create a "cached" package in .clank/deps/
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { github = "user/my-lib", version = "1.0" }\n`);

    // Simulate installed dep (what pkg install creates)
    const depDir = join(appDir, ".clank", "deps", "my-lib");
    mkdirSync(join(depDir, "src"), { recursive: true });
    writeFileSync(join(depDir, "clank.pkg"),
      `name = "my-lib"\nversion = "1.0.0"\n`);
    writeFileSync(join(depDir, "src", "helpers.clk"),
      `mod my-lib.helpers\n\npub greet : () -> <> () = fn () -> print("hi")\n`);

    const resolution = resolvePackages(join(appDir, "clank.pkg"));
    assert(resolution.packages.length === 1, `expected 1 package, got ${resolution.packages.length}`);
    assertEqual(resolution.packages[0].name, "my-lib", "package name");
    assert(resolution.moduleMap.has("my-lib.helpers"), "module found in map");
  } finally {
    cleanupTmpDir();
  }
});

test("resolvePackages local deps take priority over .clank/deps/", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    // Local dep
    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "2.0.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "mod.clk"),
      `mod my-lib.mod\n`);

    // Stale installed dep (should be ignored since local dep takes priority)
    const depDir = join(appDir, ".clank", "deps", "my-lib");
    mkdirSync(join(depDir, "src"), { recursive: true });
    writeFileSync(join(depDir, "clank.pkg"),
      `name = "my-lib"\nversion = "1.0.0"\n`);
    writeFileSync(join(depDir, "src", "mod.clk"),
      `mod my-lib.mod\n`);

    const resolution = resolvePackages(join(appDir, "clank.pkg"));
    // Should only have 1 package (local, not both)
    assertEqual(resolution.packages.length, 1, "one package");
    assertEqual(resolution.packages[0].manifest.version, "2.0.0", "local version wins");
  } finally {
    cleanupTmpDir();
  }
});

// ── verifyLockfile with GitHub deps ──

console.log("");
console.log("═══ pkg: verifyLockfile with GitHub deps ═══");

test("verifyLockfile does not report GitHub deps as extra", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { github = "user/my-lib", version = "1.0" }\n`);

    // Write a lockfile that includes the GitHub dep (JSON format)
    const lockContent = JSON.stringify({
      lock_version: 1,
      clank_version: "0.2.0",
      resolved_at: "2026-03-26T00:00:00Z",
      packages: {
        "my-lib@1.0.0": {
          version: "1.0.0",
          resolved: "github:user/my-lib",
          integrity: "sha256:abc123",
          deps: {},
          effects: [],
        },
      },
    });
    writeFileSync(join(appDir, "clank.lock"), lockContent);

    const result = verifyLockfile(join(appDir, "clank.pkg"));
    assertEqual(result.extra.length, 0, `no extra deps, got: ${result.extra.join(", ")}`);
  } finally {
    cleanupTmpDir();
  }
});

test("verifyLockfile reports missing GitHub dep when not in lockfile", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { github = "user/my-lib", version = "1.0" }\n`);

    // Write an empty lockfile (JSON format)
    writeFileSync(join(appDir, "clank.lock"),
      JSON.stringify({ lock_version: 1, clank_version: "0.2.0", resolved_at: "2026-03-26T00:00:00Z", packages: {} }));

    const result = verifyLockfile(join(appDir, "clank.pkg"));
    assert(!result.ok, "should not be ok");
    assert(result.missing.includes("my-lib"), `my-lib should be missing, got: ${result.missing.join(", ")}`);
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI pkg add --github tests ──

console.log("");
console.log("═══ pkg: CLI pkg add --github ═══");

test("clank pkg add --name x --github user/x --json", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI('pkg add --name my-lib --github user/my-lib --version "1.0" --json', join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, `exit code 0, stderr: ${result.stderr}`);
    const parsed = JSON.parse(result.stdout);
    assert(parsed.ok, "ok");
    assertEqual(parsed.data.name, "my-lib", "name");
    assertEqual(parsed.data.github, "user/my-lib", "github");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg add --github human-readable output", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI('pkg add --name my-lib --github user/my-lib --version "1.2"', join(TMP_DIR, "app"));
    assertEqual(result.exitCode, 0, `exit code 0, stderr: ${result.stderr}`);
    assert(result.stdout.includes("user/my-lib"), "output mentions github slug");
    assert(result.stdout.includes("[deps]"), "output mentions section");
  } finally {
    cleanupTmpDir();
  }
});

// ── MVS constraint merging tests ──

console.log("");
console.log("═══ pkg: MVS constraint merging ═══");

test("mergeConstraints with single constraint returns it", () => {
  assertEqual(mergeConstraints(["1.2"]), "1.2", "single constraint");
});

test("mergeConstraints with wildcard and version returns version", () => {
  assertEqual(mergeConstraints(["*", "1.2"]), "1.2", "wildcard + version");
});

test("mergeConstraints with all wildcards returns wildcard", () => {
  assertEqual(mergeConstraints(["*", "*"]), "*", "all wildcards");
});

test("mergeConstraints with empty array returns wildcard", () => {
  assertEqual(mergeConstraints([]), "*", "empty");
});

test("mergeConstraints picks higher lower bound from >= constraints", () => {
  const result = mergeConstraints([">= 0.5.0", ">= 0.7.0"]);
  assertEqual(result, ">= 0.7.0", "should pick higher lower bound");
});

test("mergeConstraints merges compatible ranges with >= constraints", () => {
  // "1.2" = >= 1.2.0, < 2.0.0; ">= 1.4.0" raises the lower bound
  const result = mergeConstraints(["1.2", ">= 1.4.0"]);
  assertEqual(result, ">= 1.4.0, < 2.0.0", "merged range");
});

test("mergeConstraints merges two compatible ranges", () => {
  // "1.2" = >= 1.2.0, < 2.0.0; "1.4" = >= 1.4.0, < 2.0.0
  // Result: >= 1.4.0, < 2.0.0
  const result = mergeConstraints(["1.2", "1.4"]);
  assertEqual(result, ">= 1.4.0, < 2.0.0", "two compatible ranges");
});

test("mergeConstraints with exact version returns it", () => {
  const result = mergeConstraints(["1.2", "1.2.3"]);
  assertEqual(result, "1.2.3", "exact version wins");
});

test("mergeConstraints with >= and < bounds", () => {
  const result = mergeConstraints([">= 1.0.0", "< 2.0.0"]);
  assertEqual(result, ">= 1.0.0, < 2.0.0", ">= and < combined");
});

test("mergeConstraints with major-only ranges", () => {
  // "1" = >= 1.0.0, < 2.0.0; "2" = >= 2.0.0, < 3.0.0
  // max lower bound = 2.0.0, min upper bound = 2.0.0 (from "1") — but "2" implies < 3.0.0
  // Actually min upper is min(2.0.0, 3.0.0) = 2.0.0
  // This creates >= 2.0.0, < 2.0.0 which is unsatisfiable, but mergeConstraints just builds the string
  // The version selector will find no match — correct behavior
  const result = mergeConstraints(["1", ">= 1.5.0"]);
  assertEqual(result, ">= 1.5.0, < 2.0.0", "major range + >=");
});

// ── pkg publish validation tests ──

console.log("");
console.log("═══ pkg: publish validation ═══");

test("pkgPublish requires description", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "pub-test");
    mkdirSync(join(dir, "src"), { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "pub-test"\nversion = "1.0.0"\nlicense = "MIT"\n`);
    // No description — should fail
    const result = pkgPublish({ dir, dryRun: true });
    assertEqual(result.ok, false, "should fail without description");
    assert(result.error!.includes("description"), "error mentions description");
  } finally {
    cleanupTmpDir();
  }
});

test("pkgPublish requires license", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "pub-test");
    mkdirSync(join(dir, "src"), { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "pub-test"\nversion = "1.0.0"\ndescription = "test"\n`);
    // No license — should fail
    const result = pkgPublish({ dir, dryRun: true });
    assertEqual(result.ok, false, "should fail without license");
    assert(result.error!.includes("license"), "error mentions license");
  } finally {
    cleanupTmpDir();
  }
});

test("pkgPublish dry run succeeds with valid manifest in git repo", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "pub-ok");
    mkdirSync(join(dir, "src"), { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "pub-ok"\nversion = "0.1.0"\ndescription = "A test package"\nlicense = "MIT"\n`);
    // Initialize git repo
    execSync("git init && git config user.email 'test@test.com' && git config user.name 'Test' && git add -A && git commit -m 'init'", {
      cwd: dir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
    });
    const result = pkgPublish({ dir, dryRun: true });
    assertEqual(result.ok, true, `should succeed, got error: ${result.error}`);
    assertEqual(result.data!.package, "pub-ok", "package name");
    assertEqual(result.data!.version, "0.1.0", "version");
    assertEqual(result.data!.tag, "v0.1.0", "tag");
  } finally {
    cleanupTmpDir();
  }
});

// ── JSON lockfile format tests ──

console.log("");
console.log("═══ pkg: JSON lockfile format ═══");

test("lockfile has lock_version, clank_version, resolved_at fields", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assertEqual(lock.lock_version, 1, "lock_version");
    assertEqual(lock.clank_version, CLANK_VERSION, "clank_version");
    assert(lock.resolved_at.length > 0, "resolved_at is non-empty");
    assert(lock.resolved_at.includes("T"), "resolved_at is ISO 8601");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile packages keyed by name@version", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.3.0"\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assert("my-lib@0.3.0" in lock.packages, "package key is name@version");
    assertEqual(lock.packages["my-lib@0.3.0"].version, "0.3.0", "version field");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile package includes deps from manifest", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n\n[deps]\nutil = "0.2"\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const pkg = lock.packages["my-lib@0.1.0"];
    assert(pkg !== undefined, "package exists");
    assertEqual(pkg.deps["util"], "0.2", "dep constraint captured");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile package includes effects from manifest", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n\n[effects]\nio = true\nasync = true\nexn = false\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const pkg = lock.packages["my-lib@0.1.0"];
    assert(pkg !== undefined, "package exists");
    assert(pkg.effects.includes("io"), "io effect included");
    assert(pkg.effects.includes("async"), "async effect included");
    assert(!pkg.effects.includes("exn"), "exn=false excluded");
    assertEqual(pkg.effects.length, 2, "only enabled effects");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile serializes to valid JSON", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const serialized = serializeLockfile(lock);

    // Should be valid JSON
    const parsed = JSON.parse(serialized);
    assertEqual(parsed.lock_version, 1, "lock_version in JSON");
    assert("packages" in parsed, "packages field in JSON");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile round-trip preserves all fields", () => {
  const lock: Lockfile = {
    lock_version: 1,
    clank_version: "0.2.0",
    resolved_at: "2026-03-26T12:00:00.000Z",
    packages: {
      "net-http@1.2.3": {
        version: "1.2.3",
        resolved: "https://registry.clank.dev/packages/net-http/1.2.3.tar.gz",
        integrity: "sha256:a1b2c3d4e5f6",
        deps: { std: "0.1.0" },
        effects: ["io", "async", "exn"],
      },
    },
  };

  const serialized = serializeLockfile(lock);
  const parsed = parseLockfile(serialized);

  assertEqual(parsed.lock_version, 1, "lock_version");
  assertEqual(parsed.clank_version, "0.2.0", "clank_version");
  assertEqual(parsed.resolved_at, "2026-03-26T12:00:00.000Z", "resolved_at");
  const pkg = parsed.packages["net-http@1.2.3"];
  assert(pkg !== undefined, "package exists");
  assertEqual(pkg.version, "1.2.3", "version");
  assertEqual(pkg.resolved, "https://registry.clank.dev/packages/net-http/1.2.3.tar.gz", "resolved");
  assertEqual(pkg.integrity, "sha256:a1b2c3d4e5f6", "integrity");
  assertEqual(pkg.deps["std"], "0.1.0", "deps.std");
  assertEqual(pkg.effects.length, 3, "effects count");
  assert(pkg.effects.includes("io"), "io effect");
});

test("lockfile with no deps produces empty packages object", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    assertEqual(Object.keys(lock.packages).length, 0, "no packages");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile integrity is sha256 hash", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "a.clk"), "mod my-lib.a\n");

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const pkg = lock.packages["my-lib@0.1.0"];
    assert(pkg.integrity.startsWith("sha256:"), "sha256 prefix");
    // sha256 hex digest is 64 chars
    assertEqual(pkg.integrity.slice(7).length, 64, "sha256 hex length");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile integrity changes when source changes", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "a.clk"), "mod my-lib.a\n");

    const lock1 = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const hash1 = lock1.packages["my-lib@0.1.0"].integrity;

    // Modify source
    writeFileSync(join(TMP_DIR, "my-lib", "src", "a.clk"), "mod my-lib.a\n# changed\n");

    const lock2 = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const hash2 = lock2.packages["my-lib@0.1.0"].integrity;

    assert(hash1 !== hash2, "integrity changes with source modification");
  } finally {
    cleanupTmpDir();
  }
});

test("writeLockfile produces valid JSON on disk", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const lockPath = writeLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const content = readFileSync(lockPath, "utf-8");
    const parsed = JSON.parse(content);
    assertEqual(parsed.lock_version, 1, "lock_version on disk");
    assert("my-lib@0.1.0" in parsed.packages, "package key on disk");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg add writes lockfile tests ──

console.log("");
console.log("═══ pkg: pkg add writes lockfile ═══");

test("pkg add with path dep writes lockfile", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    mkdirSync(join(TMP_DIR, "my-lib", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.5.0"\n`);
    writeFileSync(join(TMP_DIR, "my-lib", "src", "util.clk"), "mod my-lib.util\n");

    const result = pkgAdd({ name: "my-lib", path: "../my-lib", dir: appDir });
    assertEqual(result.ok, true, `add succeeded: ${result.error}`);

    // Lockfile should have been written
    const lockPath = join(appDir, "clank.lock");
    assert(existsSync(lockPath), "clank.lock created by pkg add");
    const lockContent = JSON.parse(readFileSync(lockPath, "utf-8"));
    assert("my-lib@0.5.0" in lockContent.packages, "lock contains added dep");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add updates existing lockfile with new dep", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });

    mkdirSync(join(TMP_DIR, "lib-a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib-a", "clank.pkg"),
      `name = "lib-a"\nversion = "0.1.0"\n`);

    mkdirSync(join(TMP_DIR, "lib-b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "lib-b", "clank.pkg"),
      `name = "lib-b"\nversion = "0.2.0"\n`);

    // Add first dep
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);
    pkgAdd({ name: "lib-a", path: "../lib-a", dir: appDir });

    // Add second dep
    pkgAdd({ name: "lib-b", path: "../lib-b", dir: appDir });

    const lockContent = JSON.parse(readFileSync(join(appDir, "clank.lock"), "utf-8"));
    assert("lib-a@0.1.0" in lockContent.packages, "lib-a in lock");
    assert("lib-b@0.2.0" in lockContent.packages, "lib-b in lock");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg add --dev writes lockfile with dev dep", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    mkdirSync(join(TMP_DIR, "test-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "test-lib", "clank.pkg"),
      `name = "test-lib"\nversion = "0.1.0"\n`);

    const result = pkgAdd({ name: "test-lib", path: "../test-lib", dev: true, dir: appDir });
    assertEqual(result.ok, true, `add succeeded: ${result.error}`);

    const lockPath = join(appDir, "clank.lock");
    assert(existsSync(lockPath), "clank.lock created by pkg add --dev");
    const lockContent = JSON.parse(readFileSync(lockPath, "utf-8"));
    assert("test-lib@0.1.0" in lockContent.packages, "dev dep in lock");
  } finally {
    cleanupTmpDir();
  }
});

test("pkg resolve writes JSON lockfile", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const result = pkgResolve(appDir);
    assertEqual(result.ok, true, `resolve succeeded: ${result.error}`);

    const lockPath = join(appDir, "clank.lock");
    assert(existsSync(lockPath), "clank.lock exists");
    const lockContent = JSON.parse(readFileSync(lockPath, "utf-8"));
    assertEqual(lockContent.lock_version, 1, "lock_version");
    assert("my-lib@0.1.0" in lockContent.packages, "package in lock");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI lockfile tests ──

console.log("");
console.log("═══ pkg: CLI lockfile JSON output ═══");

test("clank pkg add --json writes JSON lockfile", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.4.0"\n`);

    const result = runCLI('pkg add --name my-lib --path ../my-lib --json', appDir);
    assertEqual(result.exitCode, 0, `exit code: ${result.stderr}`);

    const lockContent = readFileSync(join(appDir, "clank.lock"), "utf-8");
    const lock = JSON.parse(lockContent);
    assertEqual(lock.lock_version, 1, "lock_version");
    assert("my-lib@0.4.0" in lock.packages, "package in lockfile");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg resolve --json writes JSON lockfile", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n\n[effects]\nio = true\n`);

    const result = runCLI('pkg resolve --json', appDir);
    assertEqual(result.exitCode, 0, `exit code: ${result.stderr}`);

    const lockContent = readFileSync(join(appDir, "clank.lock"), "utf-8");
    const lock = JSON.parse(lockContent);
    const pkg = lock.packages["my-lib@0.1.0"];
    assert(pkg !== undefined, "package in lockfile");
    assert(pkg.effects.includes("io"), "effects recorded");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg verify --json works with JSON lockfile", () => {
  setupTmpDir();
  try {
    const appDir = join(TMP_DIR, "app");
    mkdirSync(join(appDir, "src"), { recursive: true });
    writeFileSync(join(appDir, "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    // Write lockfile via resolve
    runCLI('pkg resolve --json', appDir);

    // Verify should pass
    const result = runCLI('pkg verify --json', appDir);
    assertEqual(result.exitCode, 0, `exit code: ${result.stderr}`);
    const output = JSON.parse(result.stdout);
    assertEqual(output.ok, true, "verify ok");
    assertEqual(output.data.ok, true, "data.ok");
  } finally {
    cleanupTmpDir();
  }
});

test("lockfile resolved field uses path: prefix for local deps", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "root"), { recursive: true });
    writeFileSync(join(TMP_DIR, "root", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n\n[deps]\nmy-lib = { path = "../my-lib" }\n`);

    mkdirSync(join(TMP_DIR, "my-lib"), { recursive: true });
    writeFileSync(join(TMP_DIR, "my-lib", "clank.pkg"),
      `name = "my-lib"\nversion = "0.1.0"\n`);

    const lock = generateLockfile(join(TMP_DIR, "root", "clank.pkg"));
    const pkg = lock.packages["my-lib@0.1.0"];
    assert(pkg.resolved.startsWith("path:"), "resolved uses path: prefix");
    assertEqual(pkg.resolved, "path:../my-lib", "relative path");
  } finally {
    cleanupTmpDir();
  }
});

// ── Registry protocol tests ──

console.log("");
console.log("═══ pkg: registry protocol ═══");

test("createGitHubRegistry returns a protocol with all methods", () => {
  const registry = createGitHubRegistry();
  assert(typeof registry.versions === "function", "has versions method");
  assert(typeof registry.info === "function", "has info method");
  assert(typeof registry.search === "function", "has search method");
  assert(typeof registry.publishEntry === "function", "has publishEntry method");
});

test("registry publishEntry builds correct entry from manifest", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "reg-pub");
    mkdirSync(join(dir, "src"), { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "my-pkg"\nversion = "1.0.0"\ndescription = "A test package"\nlicense = "MIT"\nauthors = ["alice"]\nkeywords = ["test", "util"]\n\n[deps]\nstd = "0.1"\n\n[effects]\nio = true\nasync = false\n\n[exports]\nmodules = ["main"]\n`);
    writeFileSync(join(dir, "src", "main.clk"), "mod my-pkg.main\n");

    const manifest = loadManifest(join(dir, "clank.pkg"));
    const registry = createGitHubRegistry();
    const entry = registry.publishEntry(manifest, dir);

    assertEqual(entry.name, "my-pkg", "name");
    assertEqual(entry.version, "1.0.0", "version");
    assertEqual(entry.description, "A test package", "description");
    assertEqual(entry.license, "MIT", "license");
    assertEqual(entry.authors.length, 1, "authors count");
    assertEqual(entry.authors[0], "alice", "author");
    assertEqual(entry.keywords.length, 2, "keywords count");
    assertEqual(entry.deps["std"], "0.1", "dep constraint");
    assertEqual(entry.effects.length, 1, "only enabled effects");
    assertEqual(entry.effects[0], "io", "io effect");
    assertEqual(entry.exports.length, 1, "exports count");
    assertEqual(entry.exports[0], "main", "export");
    assert(entry.integrity.startsWith("sha256:"), "has integrity");
  } finally {
    cleanupTmpDir();
  }
});

test("registry publishEntry with no optional fields", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "reg-minimal");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "minimal"\nversion = "0.1.0"\n`);

    const manifest = loadManifest(join(dir, "clank.pkg"));
    const registry = createGitHubRegistry();
    const entry = registry.publishEntry(manifest, dir);

    assertEqual(entry.name, "minimal", "name");
    assertEqual(entry.version, "0.1.0", "version");
    assertEqual(entry.description, "", "empty description");
    assertEqual(entry.repository, "", "empty repository");
    assertEqual(entry.authors.length, 0, "no authors");
    assertEqual(entry.keywords.length, 0, "no keywords");
    assertEqual(Object.keys(entry.deps).length, 0, "no deps");
    assertEqual(entry.effects.length, 0, "no effects");
    assertEqual(entry.exports.length, 0, "no exports");
  } finally {
    cleanupTmpDir();
  }
});

// ── pkg search validation tests ──

console.log("");
console.log("═══ pkg: pkg search validation ═══");

test("pkgSearch fails with empty query", () => {
  const result = pkgSearch({ repos: ["user/repo"], query: "" });
  assertEqual(result.ok, false, "should fail");
  assert(result.error!.includes("query"), "error mentions query");
});

test("pkgSearch fails with no repos", () => {
  const result = pkgSearch({ repos: [], query: "test" });
  assertEqual(result.ok, false, "should fail");
  assert(result.error!.includes("repositories"), "error mentions repositories");
});

// ── pkg info validation tests ──

console.log("");
console.log("═══ pkg: pkg info validation ═══");

test("pkgInfo fails with empty repo", () => {
  const result = pkgInfo({ repo: "" });
  assertEqual(result.ok, false, "should fail");
  assert(result.error!.includes("--repo"), "error mentions --repo");
});

// ── publish writes registry entry tests ──

console.log("");
console.log("═══ pkg: publish registry entry ═══");

test("pkg publish --dry-run does not write publish-entry.json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "pub-dry");
    mkdirSync(join(dir, "src"), { recursive: true });
    writeFileSync(join(dir, "clank.pkg"),
      `name = "pub-dry"\nversion = "0.1.0"\ndescription = "A test"\nlicense = "MIT"\n`);
    // Init git repo
    execSync("git init && git config user.email 'test@test.com' && git config user.name 'Test' && git add -A && git commit -m 'init'", {
      cwd: dir, encoding: "utf-8", stdio: ["pipe", "pipe", "pipe"],
    });

    const result = pkgPublish({ dir, dryRun: true });
    assertEqual(result.ok, true, `should succeed: ${result.error}`);
    // Dry run should NOT write the entry file
    assert(!existsSync(join(dir, ".clank", "publish-entry.json")), "no publish-entry.json on dry run");
  } finally {
    cleanupTmpDir();
  }
});

test("registry protocol types are exported correctly", () => {
  // Verify the type exports work at runtime by checking the factory
  const registry: RegistryProtocol = createGitHubRegistry();
  assert(registry !== null, "registry created");
  assert(registry !== undefined, "registry defined");
});

// ── CLI integration tests for search/info ──

console.log("");
console.log("═══ pkg: CLI search/info ═══");

test("clank pkg search --json fails with no repos", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI("pkg search test --json", join(TMP_DIR, "app"));
    const parsed = JSON.parse(result.stdout);
    assertEqual(parsed.ok, false, "should fail");
    assert(parsed.diagnostics.length > 0, "has diagnostics");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg info --json fails with no repo", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "app"), { recursive: true });
    writeFileSync(join(TMP_DIR, "app", "clank.pkg"),
      `name = "app"\nversion = "1.0.0"\n`);

    const result = runCLI("pkg info --json", join(TMP_DIR, "app"));
    const parsed = JSON.parse(result.stdout);
    assertEqual(parsed.ok, false, "should fail");
    assert(parsed.diagnostics.length > 0, "has diagnostics");
  } finally {
    cleanupTmpDir();
  }
});

// ── Summary ──

console.log("");
console.log(`${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
