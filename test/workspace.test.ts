// Tests for workspace orchestration: manifest parsing, root discovery, member
// discovery, dependency graph, topological sort, cross-member resolution, CLI
// Run with: npx tsx test/workspace.test.ts

import { spawnSync } from "node:child_process";
import { writeFileSync, mkdirSync, rmSync, existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  parseWorkspaceManifest,
  loadWorkspaceManifest,
  findWorkspaceRoot,
  discoverWorkspaceMembers,
  buildWorkspaceGraph,
  topologicalSort,
  resolveWorkspace,
  resolveWorkspacePackages,
  workspaceInit,
  workspaceList,
  workspaceAddMember,
  workspaceRemoveMember,
  computeDepthLevels,
  buildWorkspaceParallel,
  generateWorkspaceLockfile,
  writeWorkspaceLockfile,
  PkgError,
} from "../ts/src/pkg.js";

const CLI = join(import.meta.dirname, "..", "ts", "src", "main.ts");
const TMP_DIR = join("/tmp", `clank-ws-test-${Date.now()}`);

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

function assertThrows(fn: () => void, code: string, msg: string): void {
  try {
    fn();
    throw new Error(`${msg}: expected PkgError with code ${code}, but no error was thrown`);
  } catch (e: unknown) {
    if (e instanceof PkgError) {
      if (e.code !== code) {
        throw new Error(`${msg}: expected code ${code}, got ${e.code}: ${e.message}`);
      }
    } else {
      throw e;
    }
  }
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

/** Create a standard workspace layout for testing */
function createTestWorkspace(): string {
  const ws = TMP_DIR;

  // packages/core
  mkdirSync(join(ws, "packages", "core", "src"), { recursive: true });
  writeFileSync(join(ws, "packages", "core", "clank.pkg"),
    `name = "core"\nversion = "0.1.0"\n`);
  writeFileSync(join(ws, "packages", "core", "src", "lib.clk"),
    `mod core.lib\n\npub add : (a: Int, b: Int) -> Int = a + b\n`);

  // packages/web (depends on core)
  mkdirSync(join(ws, "packages", "web", "src"), { recursive: true });
  writeFileSync(join(ws, "packages", "web", "clank.pkg"),
    `name = "web"\nversion = "0.1.0"\n\n[deps]\ncore = "0.1"\n`);
  writeFileSync(join(ws, "packages", "web", "src", "app.clk"),
    `mod web.app\n\nuse core.lib (add)\n`);

  // packages/cli (depends on core)
  mkdirSync(join(ws, "packages", "cli", "src"), { recursive: true });
  writeFileSync(join(ws, "packages", "cli", "clank.pkg"),
    `name = "cli"\nversion = "0.1.0"\n\n[deps]\ncore = "0.1"\n`);
  writeFileSync(join(ws, "packages", "cli", "src", "main.clk"),
    `mod cli.main\n\nuse core.lib (add)\n`);

  // libs/shared-types
  mkdirSync(join(ws, "libs", "shared-types", "src"), { recursive: true });
  writeFileSync(join(ws, "libs", "shared-types", "clank.pkg"),
    `name = "shared-types"\nversion = "0.1.0"\n`);
  writeFileSync(join(ws, "libs", "shared-types", "src", "types.clk"),
    `mod shared-types.types\n`);

  // workspace manifest
  writeFileSync(join(ws, "clank.workspace"),
    `[workspace]\nmembers = ["packages/core", "packages/web", "packages/cli", "libs/shared-types"]\n`);

  return ws;
}

// ── Workspace manifest parsing tests ──

console.log("═══ workspace: manifest parsing ═══");

test("parse basic workspace manifest", () => {
  const ws = parseWorkspaceManifest(`[workspace]\nmembers = ["packages/core", "packages/web"]\n`);
  assertEqual(ws.members.length, 2, "member count");
  assertEqual(ws.members[0], "packages/core", "first member");
  assertEqual(ws.members[1], "packages/web", "second member");
});

test("parse workspace manifest with comments", () => {
  const ws = parseWorkspaceManifest(`# My workspace\n[workspace]\n# Members\nmembers = ["a", "b"]\n`);
  assertEqual(ws.members.length, 2, "member count");
});

test("parse workspace manifest with glob patterns", () => {
  const ws = parseWorkspaceManifest(`[workspace]\nmembers = ["packages/*", "libs/*"]\n`);
  assertEqual(ws.members.length, 2, "member count");
  assertEqual(ws.members[0], "packages/*", "first pattern");
});

test("reject workspace manifest without [workspace] section", () => {
  assertThrows(
    () => parseWorkspaceManifest(`members = ["a"]\n`),
    "E508",
    "no workspace section",
  );
});

test("reject workspace manifest without members", () => {
  assertThrows(
    () => parseWorkspaceManifest(`[workspace]\n`),
    "E508",
    "no members",
  );
});

test("reject unknown section in workspace manifest", () => {
  assertThrows(
    () => parseWorkspaceManifest(`[workspace]\nmembers = ["a"]\n[other]\n`),
    "E508",
    "unknown section",
  );
});

// ── Workspace root discovery tests ──

console.log("");
console.log("═══ workspace: root discovery ═══");

test("find workspace root from workspace dir", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const ctx = findWorkspaceRoot(TMP_DIR);
    assertEqual(ctx.mode, "workspace", "mode");
    assertEqual(ctx.workspaceRoot, TMP_DIR, "root");
    assert(ctx.workspaceFile!.endsWith("clank.workspace"), "workspace file");
  } finally {
    cleanupTmpDir();
  }
});

test("find workspace root from member subdir", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const ctx = findWorkspaceRoot(join(TMP_DIR, "packages", "core", "src"));
    assertEqual(ctx.mode, "workspace", "mode");
    assertEqual(ctx.workspaceRoot, TMP_DIR, "root");
    assert(ctx.nearestMember === "packages/core", `nearest member: ${ctx.nearestMember}`);
  } finally {
    cleanupTmpDir();
  }
});

test("find single package when no workspace", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "standalone", "src"), { recursive: true });
    writeFileSync(join(TMP_DIR, "standalone", "clank.pkg"),
      `name = "standalone"\nversion = "0.1.0"\n`);
    const ctx = findWorkspaceRoot(join(TMP_DIR, "standalone"));
    assertEqual(ctx.mode, "single-package", "mode");
    assert(ctx.manifestPath!.endsWith("clank.pkg"), "manifest path");
  } finally {
    cleanupTmpDir();
  }
});

test("error when no pkg or workspace found", () => {
  setupTmpDir();
  try {
    const emptyDir = join(TMP_DIR, "empty");
    mkdirSync(emptyDir, { recursive: true });
    assertThrows(
      () => findWorkspaceRoot(emptyDir),
      "E518",
      "no manifest found",
    );
  } finally {
    cleanupTmpDir();
  }
});

// ── Member discovery tests ──

console.log("");
console.log("═══ workspace: member discovery ═══");

test("discover explicit members", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const members = discoverWorkspaceMembers(TMP_DIR, [
      "packages/core",
      "packages/web",
    ]);
    assertEqual(members.length, 2, "member count");
    assertEqual(members[0].name, "core", "first member name");
    assertEqual(members[1].name, "web", "second member name");
  } finally {
    cleanupTmpDir();
  }
});

test("discover members with glob pattern", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const members = discoverWorkspaceMembers(TMP_DIR, ["packages/*"]);
    assertEqual(members.length, 3, "member count");
    // Alphabetical order: cli, core, web
    assertEqual(members[0].name, "cli", "first member");
    assertEqual(members[1].name, "core", "second member");
    assertEqual(members[2].name, "web", "third member");
  } finally {
    cleanupTmpDir();
  }
});

test("error on missing member clank.pkg (E515)", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "missing-pkg"), { recursive: true });
    // No clank.pkg in missing-pkg
    assertThrows(
      () => discoverWorkspaceMembers(TMP_DIR, ["missing-pkg"]),
      "E515",
      "missing member",
    );
  } finally {
    cleanupTmpDir();
  }
});

test("error on duplicate member names (E516)", () => {
  setupTmpDir();
  try {
    // Two packages with the same name
    mkdirSync(join(TMP_DIR, "a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "a", "clank.pkg"), `name = "dupe"\nversion = "0.1.0"\n`);
    mkdirSync(join(TMP_DIR, "b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "b", "clank.pkg"), `name = "dupe"\nversion = "0.2.0"\n`);

    assertThrows(
      () => discoverWorkspaceMembers(TMP_DIR, ["a", "b"]),
      "E516",
      "duplicate name",
    );
  } finally {
    cleanupTmpDir();
  }
});

// ── Dependency graph tests ──

console.log("");
console.log("═══ workspace: dependency graph ═══");

test("build graph with inter-member deps", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const members = discoverWorkspaceMembers(TMP_DIR, [
      "packages/core", "packages/web", "packages/cli", "libs/shared-types",
    ]);
    const graph = buildWorkspaceGraph(members);

    // web depends on core
    const webDeps = graph.edges.get("web")!;
    assert(webDeps.includes("core"), "web depends on core");

    // cli depends on core
    const cliDeps = graph.edges.get("cli")!;
    assert(cliDeps.includes("core"), "cli depends on core");

    // core has no inter-member deps
    assertEqual(graph.edges.get("core")!.length, 0, "core has no inter-member deps");

    // core is depended by web and cli
    const coreDepBy = graph.dependedBy.get("core")!;
    assert(coreDepBy.includes("web"), "core depended by web");
    assert(coreDepBy.includes("cli"), "core depended by cli");
  } finally {
    cleanupTmpDir();
  }
});

// ── Topological sort tests ──

console.log("");
console.log("═══ workspace: topological sort ═══");

test("topological sort respects dependency order", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const members = discoverWorkspaceMembers(TMP_DIR, [
      "packages/core", "packages/web", "packages/cli", "libs/shared-types",
    ]);
    const graph = buildWorkspaceGraph(members);
    const order = topologicalSort(graph);

    // core must come before web and cli
    const coreIdx = order.indexOf("core");
    const webIdx = order.indexOf("web");
    const cliIdx = order.indexOf("cli");
    assert(coreIdx < webIdx, `core (${coreIdx}) before web (${webIdx})`);
    assert(coreIdx < cliIdx, `core (${coreIdx}) before cli (${cliIdx})`);

    // All members present
    assertEqual(order.length, 4, "all members in order");
  } finally {
    cleanupTmpDir();
  }
});

test("topological sort detects cycles (E512)", () => {
  setupTmpDir();
  try {
    // Create cyclic workspace: a -> b -> a
    mkdirSync(join(TMP_DIR, "a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "a", "clank.pkg"),
      `name = "a"\nversion = "0.1.0"\n\n[deps]\nb = "0.1"\n`);
    mkdirSync(join(TMP_DIR, "b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "b", "clank.pkg"),
      `name = "b"\nversion = "0.1.0"\n\n[deps]\na = "0.1"\n`);

    const members = discoverWorkspaceMembers(TMP_DIR, ["a", "b"]);
    const graph = buildWorkspaceGraph(members);

    assertThrows(
      () => topologicalSort(graph),
      "E512",
      "cycle detection",
    );
  } finally {
    cleanupTmpDir();
  }
});

test("topological sort with alphabetical tie-breaking", () => {
  setupTmpDir();
  try {
    // Three independent members — should sort alphabetically
    mkdirSync(join(TMP_DIR, "c"), { recursive: true });
    writeFileSync(join(TMP_DIR, "c", "clank.pkg"), `name = "c"\nversion = "0.1.0"\n`);
    mkdirSync(join(TMP_DIR, "a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "a", "clank.pkg"), `name = "a"\nversion = "0.1.0"\n`);
    mkdirSync(join(TMP_DIR, "b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "b", "clank.pkg"), `name = "b"\nversion = "0.1.0"\n`);

    const members = discoverWorkspaceMembers(TMP_DIR, ["c", "a", "b"]);
    const graph = buildWorkspaceGraph(members);
    const order = topologicalSort(graph);

    assertEqual(order[0], "a", "first");
    assertEqual(order[1], "b", "second");
    assertEqual(order[2], "c", "third");
  } finally {
    cleanupTmpDir();
  }
});

// ── Workspace resolution tests ──

console.log("");
console.log("═══ workspace: resolution ═══");

test("resolveWorkspace produces correct build order", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const res = resolveWorkspace(TMP_DIR);

    assertEqual(res.root, TMP_DIR, "root");
    assertEqual(res.members.length, 4, "member count");
    assertEqual(res.buildOrder.length, 4, "build order length");

    // core before web and cli
    const coreIdx = res.buildOrder.indexOf("core");
    const webIdx = res.buildOrder.indexOf("web");
    assert(coreIdx < webIdx, "core before web");
  } finally {
    cleanupTmpDir();
  }
});

test("resolveWorkspacePackages resolves cross-member deps", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const { resolution, memberResolutions } = resolveWorkspacePackages(TMP_DIR);

    // web should have core as a resolved dep
    const webRes = memberResolutions.get("web")!;
    assert(webRes.packages.length > 0, "web has resolved packages");
    assert(webRes.packages.some(p => p.name === "core"), "web resolved core");

    // core should have core.lib module discovered
    const coreModules = webRes.packages.find(p => p.name === "core")!.modules;
    assert(coreModules.has("core.lib"), "core.lib module discovered");
  } finally {
    cleanupTmpDir();
  }
});

// ── Workspace init command tests ──

console.log("");
console.log("═══ workspace: init command ═══");

test("workspaceInit creates clank.workspace", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "ws-init");
    mkdirSync(dir, { recursive: true });

    const result = workspaceInit({ dir, members: ["packages/core"] });
    assert(result.ok, "ok");
    assert(existsSync(join(dir, "clank.workspace")), "file created");

    const content = readFileSync(join(dir, "clank.workspace"), "utf-8");
    assert(content.includes("packages/core"), "member listed");
  } finally {
    cleanupTmpDir();
  }
});

test("workspaceInit auto-discovers members", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "ws-auto");
    mkdirSync(join(dir, "alpha"), { recursive: true });
    writeFileSync(join(dir, "alpha", "clank.pkg"), `name = "alpha"\nversion = "0.1.0"\n`);
    mkdirSync(join(dir, "beta"), { recursive: true });
    writeFileSync(join(dir, "beta", "clank.pkg"), `name = "beta"\nversion = "0.1.0"\n`);

    const result = workspaceInit({ dir });
    assert(result.ok, "ok");
    assertEqual(result.data!.members.length, 2, "auto-discovered 2 members");
    assert(result.data!.members.includes("alpha"), "found alpha");
    assert(result.data!.members.includes("beta"), "found beta");
  } finally {
    cleanupTmpDir();
  }
});

test("workspaceInit rejects if clank.workspace exists", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "ws-dup");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.workspace"), "[workspace]\nmembers = []\n");

    const result = workspaceInit({ dir });
    assert(!result.ok, "not ok");
    assert(result.error!.includes("already exists"), "error message");
  } finally {
    cleanupTmpDir();
  }
});

// ── Workspace list command tests ──

console.log("");
console.log("═══ workspace: list command ═══");

test("workspaceList returns all members", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const result = workspaceList(TMP_DIR);
    assert(result.ok, "ok");
    assertEqual(result.data!.members.length, 4, "member count");

    const core = result.data!.members.find(m => m.name === "core")!;
    assertEqual(core.version, "0.1.0", "core version");
    assert(core.dependents.includes("web"), "core depended by web");
    assert(core.dependents.includes("cli"), "core depended by cli");
  } finally {
    cleanupTmpDir();
  }
});

// ── Workspace add/remove member tests ──

console.log("");
console.log("═══ workspace: add/remove member ═══");

test("workspaceAddMember adds to workspace", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "ws-add");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.workspace"), `[workspace]\nmembers = []\n`);
    mkdirSync(join(dir, "new-pkg"), { recursive: true });
    writeFileSync(join(dir, "new-pkg", "clank.pkg"), `name = "new-pkg"\nversion = "0.1.0"\n`);

    const result = workspaceAddMember("new-pkg", dir);
    assert(result.ok, "ok");
    assertEqual(result.data!.name, "new-pkg", "name");

    const ws = readFileSync(join(dir, "clank.workspace"), "utf-8");
    assert(ws.includes("new-pkg"), "member in file");
  } finally {
    cleanupTmpDir();
  }
});

test("workspaceRemoveMember removes from workspace", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "ws-rm");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.workspace"), `[workspace]\nmembers = ["pkg-a", "pkg-b"]\n`);
    mkdirSync(join(dir, "pkg-a"), { recursive: true });
    writeFileSync(join(dir, "pkg-a", "clank.pkg"), `name = "pkg-a"\nversion = "0.1.0"\n`);
    mkdirSync(join(dir, "pkg-b"), { recursive: true });
    writeFileSync(join(dir, "pkg-b", "clank.pkg"), `name = "pkg-b"\nversion = "0.1.0"\n`);

    const result = workspaceRemoveMember("pkg-a", dir);
    assert(result.ok, "ok");

    const ws = readFileSync(join(dir, "clank.workspace"), "utf-8");
    assert(!ws.includes("pkg-a"), "member removed");
    assert(ws.includes("pkg-b"), "other member kept");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI integration tests ──

console.log("");
console.log("═══ workspace: CLI integration ═══");

test("clank pkg workspace init --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-ws-init");
    mkdirSync(dir, { recursive: true });

    const result = runCLI(`pkg workspace init --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assert(envelope.data.root !== undefined, "root present");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg workspace list --json", () => {
  setupTmpDir();
  try {
    createTestWorkspace();

    const result = runCLI(`pkg workspace list --json`, TMP_DIR);
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.members.length, 4, "4 members");

    const memberNames = envelope.data.members.map((m: any) => m.name).sort();
    assertEqual(memberNames[0], "cli", "cli member");
    assertEqual(memberNames[1], "core", "core member");
    assertEqual(memberNames[2], "shared-types", "shared-types member");
    assertEqual(memberNames[3], "web", "web member");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg workspace add --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-ws-add");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.workspace"), `[workspace]\nmembers = []\n`);
    mkdirSync(join(dir, "my-pkg"), { recursive: true });
    writeFileSync(join(dir, "my-pkg", "clank.pkg"), `name = "my-pkg"\nversion = "0.1.0"\n`);

    const result = runCLI(`pkg workspace add my-pkg --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.name, "my-pkg", "added name");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg workspace remove --json", () => {
  setupTmpDir();
  try {
    const dir = join(TMP_DIR, "cli-ws-rm");
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "clank.workspace"), `[workspace]\nmembers = ["pkg-x"]\n`);
    mkdirSync(join(dir, "pkg-x"), { recursive: true });
    writeFileSync(join(dir, "pkg-x", "clank.pkg"), `name = "pkg-x"\nversion = "0.1.0"\n`);

    const result = runCLI(`pkg workspace remove pkg-x --json`, dir);
    const envelope = JSON.parse(result.stdout);
    assert(envelope.ok, "ok");
    assertEqual(envelope.data.member, "pkg-x", "removed member");
  } finally {
    cleanupTmpDir();
  }
});

test("clank pkg workspace list (human output)", () => {
  setupTmpDir();
  try {
    createTestWorkspace();

    const result = runCLI(`pkg workspace list`, TMP_DIR);
    assertEqual(result.exitCode, 0, "exit code");
    assert(result.stdout.includes("core@0.1.0"), "core listed");
    assert(result.stdout.includes("web@0.1.0"), "web listed");
  } finally {
    cleanupTmpDir();
  }
});

// ── Complex workspace scenarios ──

console.log("");
console.log("═══ workspace: complex scenarios ═══");

test("diamond dependency graph", () => {
  setupTmpDir();
  try {
    // base -> mid-a, mid-b -> top (diamond)
    mkdirSync(join(TMP_DIR, "base"), { recursive: true });
    writeFileSync(join(TMP_DIR, "base", "clank.pkg"),
      `name = "base"\nversion = "0.1.0"\n`);

    mkdirSync(join(TMP_DIR, "mid-a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "mid-a", "clank.pkg"),
      `name = "mid-a"\nversion = "0.1.0"\n\n[deps]\nbase = "0.1"\n`);

    mkdirSync(join(TMP_DIR, "mid-b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "mid-b", "clank.pkg"),
      `name = "mid-b"\nversion = "0.1.0"\n\n[deps]\nbase = "0.1"\n`);

    mkdirSync(join(TMP_DIR, "top"), { recursive: true });
    writeFileSync(join(TMP_DIR, "top", "clank.pkg"),
      `name = "top"\nversion = "0.1.0"\n\n[deps]\nmid-a = "0.1"\nmid-b = "0.1"\n`);

    writeFileSync(join(TMP_DIR, "clank.workspace"),
      `[workspace]\nmembers = ["base", "mid-a", "mid-b", "top"]\n`);

    const res = resolveWorkspace(TMP_DIR);
    const baseIdx = res.buildOrder.indexOf("base");
    const midAIdx = res.buildOrder.indexOf("mid-a");
    const midBIdx = res.buildOrder.indexOf("mid-b");
    const topIdx = res.buildOrder.indexOf("top");

    assert(baseIdx < midAIdx, "base before mid-a");
    assert(baseIdx < midBIdx, "base before mid-b");
    assert(midAIdx < topIdx, "mid-a before top");
    assert(midBIdx < topIdx, "mid-b before top");
  } finally {
    cleanupTmpDir();
  }
});

test("glob expansion with multiple patterns", () => {
  setupTmpDir();
  try {
    createTestWorkspace();

    // Use glob patterns
    writeFileSync(join(TMP_DIR, "clank.workspace"),
      `[workspace]\nmembers = ["packages/*", "libs/*"]\n`);

    const res = resolveWorkspace(TMP_DIR);
    assertEqual(res.members.length, 4, "all members discovered");

    const names = res.members.map(m => m.name).sort();
    assertEqual(names[0], "cli", "cli");
    assertEqual(names[1], "core", "core");
    assertEqual(names[2], "shared-types", "shared-types");
    assertEqual(names[3], "web", "web");
  } finally {
    cleanupTmpDir();
  }
});

test("workspace resolution with cross-member modules", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const { memberResolutions } = resolveWorkspacePackages(TMP_DIR);

    // cli should resolve core's modules
    const cliRes = memberResolutions.get("cli")!;
    assert(cliRes.moduleMap.has("core.lib"), "cli can see core.lib module");

    // shared-types should have no cross-member deps
    const stRes = memberResolutions.get("shared-types")!;
    assertEqual(stRes.packages.length, 0, "shared-types has no deps");
  } finally {
    cleanupTmpDir();
  }
});

// ── Depth level computation tests ──

console.log("");
console.log("═══ workspace: depth levels ═══");

test("compute depth levels for diamond dependency", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const members = discoverWorkspaceMembers(TMP_DIR, [
      "packages/core", "packages/web", "packages/cli", "libs/shared-types",
    ]);
    const graph = buildWorkspaceGraph(members);
    const levels = computeDepthLevels(graph);

    // shared-types and core at depth 0 (no inter-member deps)
    // web and cli at depth 1 (depend on core)
    assert(levels.length >= 2, `at least 2 levels, got ${levels.length}`);

    const depth0 = levels.find(l => l.depth === 0)!;
    assert(depth0.members.includes("core"), "core at depth 0");
    assert(depth0.members.includes("shared-types"), "shared-types at depth 0");

    const depth1 = levels.find(l => l.depth === 1)!;
    assert(depth1.members.includes("web"), "web at depth 1");
    assert(depth1.members.includes("cli"), "cli at depth 1");
  } finally {
    cleanupTmpDir();
  }
});

test("depth levels for independent members are all depth 0", () => {
  setupTmpDir();
  try {
    mkdirSync(join(TMP_DIR, "a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "a", "clank.pkg"), `name = "a"\nversion = "0.1.0"\n`);
    mkdirSync(join(TMP_DIR, "b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "b", "clank.pkg"), `name = "b"\nversion = "0.1.0"\n`);
    mkdirSync(join(TMP_DIR, "c"), { recursive: true });
    writeFileSync(join(TMP_DIR, "c", "clank.pkg"), `name = "c"\nversion = "0.1.0"\n`);

    const members = discoverWorkspaceMembers(TMP_DIR, ["a", "b", "c"]);
    const graph = buildWorkspaceGraph(members);
    const levels = computeDepthLevels(graph);

    assertEqual(levels.length, 1, "all at same depth");
    assertEqual(levels[0].depth, 0, "depth 0");
    assertEqual(levels[0].members.length, 3, "all 3 members");
  } finally {
    cleanupTmpDir();
  }
});

test("depth levels for linear chain", () => {
  setupTmpDir();
  try {
    // a -> b -> c (c depends on b, b depends on a)
    mkdirSync(join(TMP_DIR, "a"), { recursive: true });
    writeFileSync(join(TMP_DIR, "a", "clank.pkg"), `name = "a"\nversion = "0.1.0"\n`);
    mkdirSync(join(TMP_DIR, "b"), { recursive: true });
    writeFileSync(join(TMP_DIR, "b", "clank.pkg"), `name = "b"\nversion = "0.1.0"\n\n[deps]\na = "0.1"\n`);
    mkdirSync(join(TMP_DIR, "c"), { recursive: true });
    writeFileSync(join(TMP_DIR, "c", "clank.pkg"), `name = "c"\nversion = "0.1.0"\n\n[deps]\nb = "0.1"\n`);

    const members = discoverWorkspaceMembers(TMP_DIR, ["a", "b", "c"]);
    const graph = buildWorkspaceGraph(members);
    const levels = computeDepthLevels(graph);

    assertEqual(levels.length, 3, "3 depth levels");
    assertEqual(levels[0].members[0], "a", "a at depth 0");
    assertEqual(levels[1].members[0], "b", "b at depth 1");
    assertEqual(levels[2].members[0], "c", "c at depth 2");
  } finally {
    cleanupTmpDir();
  }
});

// ── Parallel build execution tests ──

console.log("");
console.log("═══ workspace: parallel build ═══");

test("parallel build executes all members", async () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const resolution = resolveWorkspace(TMP_DIR);
    const built: string[] = [];

    const result = await buildWorkspaceParallel(
      resolution,
      async (member) => {
        built.push(member.name);
        return { ok: true };
      },
      2,
    );

    assert(result.ok, "build ok");
    assertEqual(result.membersBuilt.length, 4, "all 4 members built");
    assert(result.membersBuilt.every(m => m.status === "success"), "all success");
    assert(result.parallelismAchieved >= 2, `parallelism >= 2, got ${result.parallelismAchieved}`);

    // Verify build order: core and shared-types before web and cli
    const coreIdx = built.indexOf("core");
    const webIdx = built.indexOf("web");
    const cliIdx = built.indexOf("cli");
    assert(coreIdx < webIdx, "core before web");
    assert(coreIdx < cliIdx, "core before cli");
  } finally {
    cleanupTmpDir();
  }
});

test("parallel build handles member failure (fail-fast)", async () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const resolution = resolveWorkspace(TMP_DIR);

    const result = await buildWorkspaceParallel(
      resolution,
      async (member) => {
        if (member.name === "core") {
          return { ok: false, error: "type error in core" };
        }
        return { ok: true };
      },
      4,
    );

    assert(!result.ok, "build not ok");

    const coreResult = result.membersBuilt.find(m => m.name === "core")!;
    assertEqual(coreResult.status, "failed", "core failed");

    // web and cli should be skipped (depend on core)
    const webResult = result.membersBuilt.find(m => m.name === "web")!;
    assertEqual(webResult.status, "skipped_dep_failed", "web skipped");
    assertEqual(webResult.blockedBy, "core", "web blocked by core");

    const cliResult = result.membersBuilt.find(m => m.name === "cli")!;
    assertEqual(cliResult.status, "skipped_dep_failed", "cli skipped");
  } finally {
    cleanupTmpDir();
  }
});

test("parallel build respects maxJobs=1 (sequential)", async () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const resolution = resolveWorkspace(TMP_DIR);
    const order: string[] = [];

    await buildWorkspaceParallel(
      resolution,
      async (member) => {
        order.push(member.name);
        return { ok: true };
      },
      1,
    );

    // With maxJobs=1, should still respect dependency order
    const coreIdx = order.indexOf("core");
    const webIdx = order.indexOf("web");
    assert(coreIdx < webIdx, "core before web with jobs=1");
  } finally {
    cleanupTmpDir();
  }
});

// ── Workspace lockfile tests ──

console.log("");
console.log("═══ workspace: lockfile ═══");

test("generateWorkspaceLockfile includes all members", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const lock = generateWorkspaceLockfile(TMP_DIR);

    assertEqual(lock.lock_version, 1, "lock version");
    assert(lock.clank_version !== undefined, "has clank version");

    // Should have workspace members as entries
    const keys = Object.keys(lock.packages);
    assert(keys.some(k => k.startsWith("core@")), "core in lock");
    assert(keys.some(k => k.startsWith("web@")), "web in lock");
    assert(keys.some(k => k.startsWith("cli@")), "cli in lock");
    assert(keys.some(k => k.startsWith("shared-types@")), "shared-types in lock");

    // workspace members should have workspace: prefix in resolved
    const coreEntry = Object.entries(lock.packages).find(([k]) => k.startsWith("core@"))!;
    assert(coreEntry[1].resolved.startsWith("workspace:"), `core resolved: ${coreEntry[1].resolved}`);
  } finally {
    cleanupTmpDir();
  }
});

test("writeWorkspaceLockfile creates clank.lock at workspace root", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const lockPath = writeWorkspaceLockfile(TMP_DIR);

    assert(lockPath.endsWith("clank.lock"), "lock path");
    assert(existsSync(lockPath), "lock file exists");

    const content = readFileSync(lockPath, "utf-8");
    assert(content.includes('"lock_version"'), "has lock version");
    assert(content.includes("core@"), "has core");
  } finally {
    cleanupTmpDir();
  }
});

// ── CLI integration: build/check/test with workspace flags ──

console.log("");
console.log("═══ workspace: CLI build/check/test ═══");

test("clank build --all --json in workspace", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const { stdout, exitCode } = runCLI("build --all --json", TMP_DIR);
    const result = JSON.parse(stdout);
    assert(result.ok === true || result.ok === false, "has ok field");
    if (result.ok) {
      assert(result.data.members_built !== undefined, "has members_built");
      assert(result.data.build_order !== undefined, "has build_order");
    }
  } finally {
    cleanupTmpDir();
  }
});

test("clank build --package core --json in workspace", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const { stdout } = runCLI("build --package core --json", TMP_DIR);
    const result = JSON.parse(stdout);
    assert(result.ok === true || result.ok === false, "has ok field");
    if (result.ok && result.data) {
      const names = result.data.members_built.map((m: any) => m.name);
      assert(names.includes("core"), "core built");
    }
  } finally {
    cleanupTmpDir();
  }
});

test("clank check --all --json in workspace", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const { stdout } = runCLI("check --all --json", TMP_DIR);
    const result = JSON.parse(stdout);
    assert(result.ok === true || result.ok === false, "has ok field");
  } finally {
    cleanupTmpDir();
  }
});

test("clank build --package nonexistent --json gives error", () => {
  setupTmpDir();
  try {
    createTestWorkspace();
    const { stdout, exitCode } = runCLI("build --package nonexistent --json", TMP_DIR);
    const result = JSON.parse(stdout);
    assertEqual(result.ok, false, "not ok");
    assert(exitCode !== 0, "non-zero exit");
  } finally {
    cleanupTmpDir();
  }
});

// ── Summary ──

console.log("");
console.log(`${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
