// Tests for clank.pkg manifest parsing, local dep resolution, and pkg commands
// Run with: npx tsx test/pkg.test.ts

import { execSync } from "node:child_process";
import { writeFileSync, mkdirSync, rmSync, existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { parseManifest, serializeManifest, findManifest, resolveLocalDeps, loadManifest, discoverModules, resolvePackages, pkgInit, pkgResolve, PkgError } from "../src/pkg.js";

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
  try {
    const stdout = execSync(`npx tsx ${CLI} ${args}`, {
      encoding: "utf-8",
      stdio: ["pipe", "pipe", "pipe"],
      cwd: cwd ?? TMP_DIR,
    });
    return { stdout: stdout.trim(), stderr: "", exitCode: 0 };
  } catch (e: unknown) {
    const err = e as { stdout?: string; stderr?: string; status?: number };
    return {
      stdout: (err.stdout ?? "").trim(),
      stderr: (err.stderr ?? "").trim(),
      exitCode: err.status ?? 1,
    };
  }
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

// ── Summary ──

console.log("");
console.log(`${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
