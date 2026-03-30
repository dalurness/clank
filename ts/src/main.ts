// Clank CLI entry point
// Usage: npx tsx ts/src/main.ts [--vm] [--json] <file.clk>
//        npx tsx ts/src/main.ts eval [--file <file.clk>] [--session <name>] [--type] [--json] "<expr>"

import { readFileSync, writeFileSync, existsSync, mkdirSync, readdirSync, statSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { lex } from "./lexer.js";
import { parse, parseExpression } from "./parser.js";
import { desugar } from "./desugar.js";
import { typeCheck } from "./checker.js";
import { run, evalExpr, evalExprWithEnv, createEnv, valueToJSON, valueTypeName } from "./eval.js";
import type { Value, EvalResult } from "./eval.js";
import { compileProgram } from "./compiler.js";
import { execute } from "./vm.js";
import type { Program, TopLevel } from "./ast.js";
import { toDiagnostic, makeEnvelope } from "./diagnostics.js";
import type { Diagnostic } from "./diagnostics.js";
import { format } from "./formatter.js";
import { lint, LINT_RULES } from "./linter.js";
import type { LintOptions } from "./linter.js";
import { getAllBuiltinEntries, extractProgramEntries, searchEntries, findEntry, formatEntryShort, formatEntryDetailed, entryToJSON } from "./doc.js";
import { pkgInit, pkgResolve, pkgAdd, pkgRemove, pkgInstall, pkgPublish, pkgSearch, pkgInfo, findManifest, resolvePackages, verifyLockfile, findWorkspaceRoot, resolveWorkspace, workspaceInit, workspaceList, workspaceAddMember, workspaceRemoveMember, buildWorkspaceParallel, computeDepthLevels, writeWorkspaceLockfile } from "./pkg.js";
import type { WorkspaceMember, WorkspaceResolution, MemberBuildResult } from "./pkg.js";
import { transform } from "./pretty.js";
import type { Direction } from "./expansion.js";

// Parse CLI flags
const args = process.argv.slice(2);
const useVM = args.includes("--vm");
const jsonMode = args.includes("--json");

// Flags that consume the next argument as a value
const valueFlagSet = new Set(["--file", "--session", "--rule", "--filter", "--name", "--entry", "--path", "--version", "--github", "--members", "--package", "--jobs"]);

// Filter positional args, skipping --flag and --flag <value> pairs
const nonFlagArgs: string[] = [];
for (let i = 0; i < args.length; i++) {
  if (args[i].startsWith("--")) {
    if (valueFlagSet.has(args[i])) i++; // skip the value too
    continue;
  }
  nonFlagArgs.push(args[i]);
}

if (nonFlagArgs[0] === "fmt") {
  runFmt();
} else if (nonFlagArgs[0] === "eval") {
  runEval();
} else if (nonFlagArgs[0] === "build") {
  runBuild();
} else if (nonFlagArgs[0] === "check") {
  runCheck();
} else if (nonFlagArgs[0] === "lint") {
  runLint();
} else if (nonFlagArgs[0] === "doc") {
  runDoc();
} else if (nonFlagArgs[0] === "test") {
  runTest();
} else if (nonFlagArgs[0] === "pkg") {
  runPkg();
} else if (nonFlagArgs[0] === "pretty") {
  runPrettyTerse("pretty");
} else if (nonFlagArgs[0] === "terse") {
  runPrettyTerse("terse");
} else {
  runFile();
}

// ── Fmt subcommand ──

function runFmt(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  const checkMode = args.includes("--check");
  const diffMode = args.includes("--diff");
  const stdinMode = args.includes("--stdin");

  // Collect files to format
  const targets = nonFlagArgs.slice(1); // after "fmt"

  if (stdinMode) {
    // Read from stdin
    const source = readFileSync("/dev/stdin", "utf-8");
    const result = formatSource(source, "<stdin>", startTime, phaseTiming, diagnostics);
    if (result === null) {
      emitFmtResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }
    process.stdout.write(result);
    emitFmtResult(startTime, phaseTiming, true, { files: [{ file: "<stdin>", formatted: true }] }, diagnostics);
    return;
  }

  if (targets.length === 0) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "no input file specified",
      location: { file: "", line: 0, col: 0 },
    });
    emitFmtResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  // Collect .clk files from targets (files or directories)
  const files = collectClkFiles(targets);
  const results: { file: string; formatted: boolean; diff?: string }[] = [];
  let allFormatted = true;

  for (const file of files) {
    const absFile = resolve(file);
    const source = readFileSync(absFile, "utf-8");
    const result = formatSource(source, absFile, startTime, phaseTiming, diagnostics);
    if (result === null) {
      // Parse error — skip this file
      results.push({ file, formatted: false });
      allFormatted = false;
      continue;
    }

    const isFormatted = source === result;
    if (!isFormatted) allFormatted = false;

    if (checkMode) {
      if (!isFormatted) {
        results.push({ file, formatted: false, diff: simpleDiff(source, result, file) });
      } else {
        results.push({ file, formatted: true });
      }
    } else if (diffMode) {
      if (!isFormatted) {
        process.stdout.write(simpleDiff(source, result, file));
      }
      results.push({ file, formatted: isFormatted });
    } else {
      // Write in place
      if (!isFormatted) {
        writeFileSync(absFile, result);
      }
      results.push({ file, formatted: isFormatted });
    }
  }

  if (checkMode && !allFormatted) {
    emitFmtResult(startTime, phaseTiming, false, { files: results }, diagnostics);
    return;
  }

  emitFmtResult(startTime, phaseTiming, true, { files: results }, diagnostics);
}

function formatSource(
  source: string,
  file: string,
  _startTime: number,
  phaseTiming: Record<string, number>,
  diagnostics: Diagnostic[],
): string | null {
  let phaseStart = Date.now();
  const tokens = lex(source);
  phaseTiming.lex = (phaseTiming.lex ?? 0) + (Date.now() - phaseStart);

  if (!Array.isArray(tokens)) {
    diagnostics.push(toDiagnostic(tokens, file));
    return null;
  }

  phaseStart = Date.now();
  const ast = parse(tokens);
  phaseTiming.parse = (phaseTiming.parse ?? 0) + (Date.now() - phaseStart);

  if ("code" in ast) {
    diagnostics.push(toDiagnostic(ast as any, file));
    return null;
  }

  phaseStart = Date.now();
  const formatted = format(ast as Program, source);
  phaseTiming.format = (phaseTiming.format ?? 0) + (Date.now() - phaseStart);

  return formatted;
}

function collectClkFiles(targets: string[]): string[] {
  const files: string[] = [];
  for (const target of targets) {
    const absTarget = resolve(target);
    const stat = statSync(absTarget);
    if (stat.isFile() && absTarget.endsWith(".clk")) {
      files.push(target);
    } else if (stat.isDirectory()) {
      const walk = (dir: string) => {
        for (const entry of readdirSync(dir, { withFileTypes: true })) {
          const full = join(dir, entry.name);
          if (entry.isDirectory()) walk(full);
          else if (entry.isFile() && entry.name.endsWith(".clk")) files.push(full);
        }
      };
      walk(absTarget);
    }
  }
  return files;
}

function simpleDiff(original: string, formatted: string, file: string): string {
  const origLines = original.split("\n");
  const fmtLines = formatted.split("\n");
  const lines: string[] = [`--- a/${file}`, `+++ b/${file}`];
  // Simple line-by-line diff
  const maxLen = Math.max(origLines.length, fmtLines.length);
  let i = 0;
  while (i < maxLen) {
    // Find a changed region
    if (i < origLines.length && i < fmtLines.length && origLines[i] === fmtLines[i]) {
      i++;
      continue;
    }
    // Found a difference — emit a hunk
    const start = i;
    let origEnd = i;
    let fmtEnd = i;
    // Advance past differing lines (simple approach)
    while (origEnd < origLines.length && fmtEnd < fmtLines.length &&
           origLines[origEnd] !== fmtLines[fmtEnd]) {
      origEnd++;
      fmtEnd++;
    }
    // If one side is longer
    while (origEnd < origLines.length &&
           (fmtEnd >= fmtLines.length || origLines[origEnd] !== fmtLines[fmtEnd])) {
      origEnd++;
    }
    while (fmtEnd < fmtLines.length &&
           (origEnd >= origLines.length || origLines[origEnd] !== fmtLines[fmtEnd])) {
      fmtEnd++;
    }

    lines.push(`@@ -${start + 1},${origEnd - start} +${start + 1},${fmtEnd - start} @@`);
    for (let j = start; j < origEnd; j++) lines.push(`-${origLines[j]}`);
    for (let j = start; j < fmtEnd; j++) lines.push(`+${fmtLines[j]}`);
    i = Math.max(origEnd, fmtEnd);
  }
  return lines.join("\n") + "\n";
}

function emitFmtResult(
  startTime: number,
  phaseTiming: Record<string, number>,
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
): void {
  const totalMs = Date.now() - startTime;
  if (jsonMode) {
    console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  } else if (!ok) {
    for (const d of diagnostics) {
      console.error(`${d.code}: ${d.message}`);
    }
  }
  process.exit(ok ? 0 : 1);
}

// ── Pretty / Terse subcommand ──

function runPrettyTerse(direction: Direction): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  const writeMode = args.includes("--write");
  const diffMode = args.includes("--diff");
  const stdinMode = args.includes("--stdin");

  const targets = nonFlagArgs.slice(1); // after "pretty" or "terse"

  if (stdinMode) {
    const source = readFileSync("/dev/stdin", "utf-8");
    const phaseStart = Date.now();
    const result = transform(source, direction);
    phaseTiming.transform = Date.now() - phaseStart;

    if (jsonMode) {
      const totalMs = Date.now() - startTime;
      console.log(JSON.stringify(makeEnvelope(true, {
        source: result.source,
        transformations: result.transformations,
        direction: result.direction,
      }, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
    } else {
      process.stdout.write(result.source);
    }
    process.exit(0);
    return;
  }

  if (targets.length === 0) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "no input file specified",
      location: { file: "", line: 0, col: 0 },
    });
    const totalMs = Date.now() - startTime;
    if (jsonMode) {
      console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
    } else {
      for (const d of diagnostics) console.error(`${d.code}: ${d.message}`);
    }
    process.exit(1);
    return;
  }

  const files = collectClkFiles(targets);
  const results: { file: string; transformations: number }[] = [];
  let totalTransformations = 0;

  for (const file of files) {
    const absFile = resolve(file);
    const source = readFileSync(absFile, "utf-8");

    const phaseStart = Date.now();
    const result = transform(source, direction);
    phaseTiming.transform = (phaseTiming.transform ?? 0) + (Date.now() - phaseStart);

    totalTransformations += result.transformations;
    results.push({ file, transformations: result.transformations });

    if (writeMode) {
      if (source !== result.source) {
        writeFileSync(absFile, result.source);
      }
    } else if (diffMode) {
      if (source !== result.source) {
        process.stdout.write(simpleDiff(source, result.source, file));
      }
    } else {
      process.stdout.write(result.source);
    }
  }

  if (jsonMode) {
    const totalMs = Date.now() - startTime;
    console.log(JSON.stringify(makeEnvelope(true, {
      files: results,
      transformations: totalTransformations,
      direction,
    }, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  }
  process.exit(0);
}

// ── Build subcommand ──

function runBuild(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  const allFlag = args.includes("--all");
  const packageFlag = getFlagValue("--package");
  const jobsFlag = getFlagValue("--jobs");
  const maxJobs = jobsFlag ? parseInt(jobsFlag, 10) : 4;

  // Try workspace mode
  if (allFlag || packageFlag) {
    runWorkspaceBuild(startTime, phaseTiming, diagnostics, allFlag, packageFlag, maxJobs);
    return;
  }

  // Non-workspace: build files specified as args
  const targets = nonFlagArgs.slice(1);
  if (targets.length === 0) {
    // Try workspace detection
    try {
      const ctx = findWorkspaceRoot(process.cwd());
      if (ctx.mode === "workspace" && ctx.nearestMember) {
        // Build nearest member
        runWorkspaceBuild(startTime, phaseTiming, diagnostics, false, undefined, maxJobs, ctx.nearestMember);
        return;
      } else if (ctx.mode === "workspace") {
        diagnostics.push({
          severity: "error", code: "E519", phase: "build",
          message: "Workspace command requires --package or --all (no nearest member)",
          location: { file: "", line: 0, col: 0 },
        });
        emitBuildResult(startTime, phaseTiming, false, null, diagnostics);
        return;
      }
    } catch {
      // Not in workspace — fall through to file build
    }

    diagnostics.push({
      severity: "error", code: "E001", phase: "build",
      message: "no input file specified",
      location: { file: "", line: 0, col: 0 },
    });
    emitBuildResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  // Single file build
  const files = collectClkFiles(targets);
  if (files.length === 0) {
    diagnostics.push({
      severity: "error", code: "E001", phase: "build",
      message: "no .clk files found",
      location: { file: "", line: 0, col: 0 },
    });
    emitBuildResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  const memberResults: { file: string; ok: boolean; modules: number }[] = [];
  for (const file of files) {
    const absFile = resolve(file);
    const result = buildSingleFile(absFile, phaseTiming, diagnostics);
    memberResults.push({ file, ok: result.ok, modules: result.ok ? 1 : 0 });
  }

  const ok = memberResults.every(r => r.ok);
  emitBuildResult(startTime, phaseTiming, ok, { files: memberResults }, diagnostics);
}

function buildSingleFile(
  absFile: string,
  phaseTiming: Record<string, number>,
  diagnostics: Diagnostic[],
): { ok: boolean } {
  const source = readFileSync(absFile, "utf-8");

  let phaseStart = Date.now();
  const tokens = lex(source);
  phaseTiming.lex = (phaseTiming.lex ?? 0) + (Date.now() - phaseStart);

  if (!Array.isArray(tokens)) {
    diagnostics.push(toDiagnostic(tokens, absFile));
    return { ok: false };
  }

  phaseStart = Date.now();
  const ast = parse(tokens);
  phaseTiming.parse = (phaseTiming.parse ?? 0) + (Date.now() - phaseStart);

  if ("code" in ast) {
    diagnostics.push(toDiagnostic(ast as any, absFile));
    return { ok: false };
  }

  phaseStart = Date.now();
  const program: Program = {
    topLevels: (ast as Program).topLevels.map(tl => {
      if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
      if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
      return tl;
    }),
  };
  phaseTiming.desugar = (phaseTiming.desugar ?? 0) + (Date.now() - phaseStart);

  phaseStart = Date.now();
  const typeErrors = typeCheck(program);
  phaseTiming.check = (phaseTiming.check ?? 0) + (Date.now() - phaseStart);

  if (typeErrors.length > 0) {
    for (const err of typeErrors) {
      diagnostics.push(toDiagnostic(err, absFile));
    }
    if (typeErrors.some(e => (e as any).severity === "error" || !("severity" in e))) {
      return { ok: false };
    }
  }

  phaseStart = Date.now();
  compileProgram(program);
  phaseTiming.compile = (phaseTiming.compile ?? 0) + (Date.now() - phaseStart);

  return { ok: true };
}

function runWorkspaceBuild(
  startTime: number,
  phaseTiming: Record<string, number>,
  diagnostics: Diagnostic[],
  allFlag: boolean,
  packageFlag: string | undefined,
  maxJobs: number,
  nearestMember?: string,
): void {
  try {
    const ctx = findWorkspaceRoot(process.cwd());
    if (ctx.mode !== "workspace" || !ctx.workspaceRoot) {
      diagnostics.push({
        severity: "error", code: "E518", phase: "build",
        message: "Not inside a workspace (no clank.workspace found)",
        location: { file: "", line: 0, col: 0 },
      });
      emitBuildResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    const resolution = resolveWorkspace(ctx.workspaceRoot);

    // Filter members based on flags
    let targetMembers: Set<string>;
    if (allFlag) {
      targetMembers = new Set(resolution.members.map(m => m.name));
    } else {
      const targetName = packageFlag ?? nearestMember;
      if (!targetName) {
        diagnostics.push({
          severity: "error", code: "E519", phase: "build",
          message: "Workspace command requires --package or --all (no nearest member)",
          location: { file: "", line: 0, col: 0 },
        });
        emitBuildResult(startTime, phaseTiming, false, null, diagnostics);
        return;
      }

      // Include the target and all its transitive dependencies
      targetMembers = new Set<string>();
      const addWithDeps = (name: string) => {
        if (targetMembers.has(name)) return;
        targetMembers.add(name);
        const deps = resolution.graph.edges.get(name) ?? [];
        for (const dep of deps) addWithDeps(dep);
      };

      // Find the member by name or relative path
      const member = resolution.members.find(m => m.name === targetName || m.relativePath === targetName);
      if (!member) {
        diagnostics.push({
          severity: "error", code: "E519", phase: "build",
          message: `Package '${targetName}' not found in workspace`,
          location: { file: "", line: 0, col: 0 },
        });
        emitBuildResult(startTime, phaseTiming, false, null, diagnostics);
        return;
      }
      addWithDeps(member.name);
    }

    // Generate workspace lockfile
    const lockStart = Date.now();
    writeWorkspaceLockfile(ctx.workspaceRoot);
    phaseTiming["lockfile"] = Date.now() - lockStart;

    // Build members in parallel
    const buildMember = async (member: WorkspaceMember): Promise<{ ok: boolean; error?: string }> => {
      if (!targetMembers.has(member.name)) {
        return { ok: true }; // skip — not a target
      }
      const memberDiags: Diagnostic[] = [];
      const srcDir = join(member.path, "src");
      if (!existsSync(srcDir)) {
        return { ok: true }; // no src dir — nothing to build
      }

      const files = collectClkFiles([srcDir]);
      for (const file of files) {
        const absFile = resolve(file);
        const result = buildSingleFile(absFile, phaseTiming, memberDiags);
        if (!result.ok) {
          diagnostics.push(...memberDiags);
          return { ok: false, error: `Build failed for ${member.name}` };
        }
      }
      diagnostics.push(...memberDiags);
      return { ok: true };
    };

    // Filter the resolution to only include target members
    const filteredResolution = { ...resolution };

    buildWorkspaceParallel(filteredResolution, buildMember, maxJobs).then(result => {
      const data = {
        members_built: result.membersBuilt.filter(m => targetMembers.has(m.name)).map(m => ({
          name: m.name,
          path: m.path,
          status: m.status,
          ...(m.blockedBy ? { blocked_by: m.blockedBy } : {}),
          ...(m.error ? { error: m.error } : {}),
        })),
        build_order: result.buildOrder,
        parallelism_achieved: result.parallelismAchieved,
      };
      emitBuildResult(startTime, phaseTiming, result.ok, data, diagnostics);
    });
  } catch (err) {
    diagnostics.push({
      severity: "error", code: "E520", phase: "build",
      message: err instanceof Error ? err.message : String(err),
      location: { file: "", line: 0, col: 0 },
    });
    emitBuildResult(startTime, phaseTiming, false, null, diagnostics);
  }
}

function emitBuildResult(
  startTime: number,
  phaseTiming: Record<string, number>,
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
): void {
  const totalMs = Date.now() - startTime;
  if (jsonMode) {
    console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  } else {
    if (!ok) {
      for (const d of diagnostics) {
        if (d.severity === "error") {
          console.error(`${d.code}: ${d.message}`);
        }
      }
    } else if (data && typeof data === "object" && "members_built" in data) {
      const bd = data as { members_built: { name: string; status: string }[] };
      for (const m of bd.members_built) {
        if (m.status === "success") {
          console.log(`  ✓ ${m.name}`);
        } else if (m.status === "skipped_dep_failed") {
          console.log(`  ⊘ ${m.name} (skipped)`);
        }
      }
    }
  }
  process.exit(ok ? 0 : 1);
}

// ── Check subcommand ──

function runCheck(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  const allFlag = args.includes("--all");
  const packageFlag = getFlagValue("--package");

  // Workspace-aware check
  if (allFlag || packageFlag) {
    runWorkspaceCheck(startTime, phaseTiming, diagnostics, allFlag, packageFlag);
    return;
  }

  const targets = nonFlagArgs.slice(1); // after "check"

  if (targets.length === 0) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "no input file specified",
      location: { file: "", line: 0, col: 0 },
    });
    emitCheckResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  const files = collectClkFiles(targets);
  if (files.length === 0) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "no .clk files found",
      location: { file: "", line: 0, col: 0 },
    });
    emitCheckResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  const fileResults: { file: string; ok: boolean; errors: number; warnings: number }[] = [];

  for (const file of files) {
    const absFile = resolve(file);
    const source = readFileSync(absFile, "utf-8");
    let fileErrors = 0;
    let fileWarnings = 0;

    // Lex
    let phaseStart = Date.now();
    const tokens = lex(source);
    phaseTiming.lex = (phaseTiming.lex ?? 0) + (Date.now() - phaseStart);

    if (!Array.isArray(tokens)) {
      diagnostics.push(toDiagnostic(tokens, absFile));
      fileResults.push({ file, ok: false, errors: 1, warnings: 0 });
      continue;
    }

    // Parse
    phaseStart = Date.now();
    const ast = parse(tokens);
    phaseTiming.parse = (phaseTiming.parse ?? 0) + (Date.now() - phaseStart);

    if ("code" in ast) {
      diagnostics.push(toDiagnostic(ast as any, absFile));
      fileResults.push({ file, ok: false, errors: 1, warnings: 0 });
      continue;
    }

    // Desugar
    phaseStart = Date.now();
    const program: Program = {
      topLevels: (ast as Program).topLevels.map(tl => {
        if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
        if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
        return tl;
      }),
    };
    phaseTiming.desugar = (phaseTiming.desugar ?? 0) + (Date.now() - phaseStart);

    // Type check
    phaseStart = Date.now();
    const typeErrors = typeCheck(program);
    phaseTiming.check = (phaseTiming.check ?? 0) + (Date.now() - phaseStart);

    for (const err of typeErrors) {
      const diag = toDiagnostic(err, absFile);
      diagnostics.push(diag);
      if (diag.severity === "error") fileErrors++;
      else if (diag.severity === "warning") fileWarnings++;
    }

    fileResults.push({ file, ok: fileErrors === 0, errors: fileErrors, warnings: fileWarnings });
  }

  const hasErrors = diagnostics.some(d => d.severity === "error");
  emitCheckResult(startTime, phaseTiming, !hasErrors, { files: fileResults }, diagnostics);
}

function emitCheckResult(
  startTime: number,
  phaseTiming: Record<string, number>,
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
): void {
  const totalMs = Date.now() - startTime;
  if (jsonMode) {
    console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  } else {
    for (const d of diagnostics) {
      const prefix = d.severity === "warning" ? "warning" : "error";
      const loc = d.location.file ? `${d.location.file}:${d.location.line}:${d.location.col}` : "";
      console.error(`${prefix}: ${d.message}${loc ? ` [${loc}]` : ""}`);
    }
  }
  process.exit(ok ? 0 : 1);
}

function runWorkspaceCheck(
  startTime: number,
  phaseTiming: Record<string, number>,
  diagnostics: Diagnostic[],
  allFlag: boolean,
  packageFlag: string | undefined,
): void {
  try {
    const ctx = findWorkspaceRoot(process.cwd());
    if (ctx.mode !== "workspace" || !ctx.workspaceRoot) {
      diagnostics.push({
        severity: "error", code: "E518", phase: "check",
        message: "Not inside a workspace",
        location: { file: "", line: 0, col: 0 },
      });
      emitCheckResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    const resolution = resolveWorkspace(ctx.workspaceRoot);
    let targetMembers: Set<string>;

    if (allFlag) {
      targetMembers = new Set(resolution.members.map(m => m.name));
    } else {
      const targetName = packageFlag;
      if (!targetName) {
        diagnostics.push({
          severity: "error", code: "E519", phase: "check",
          message: "Workspace command requires --package or --all",
          location: { file: "", line: 0, col: 0 },
        });
        emitCheckResult(startTime, phaseTiming, false, null, diagnostics);
        return;
      }
      targetMembers = new Set<string>();
      const addWithDeps = (name: string) => {
        if (targetMembers.has(name)) return;
        targetMembers.add(name);
        for (const dep of resolution.graph.edges.get(name) ?? []) addWithDeps(dep);
      };
      const member = resolution.members.find(m => m.name === targetName);
      if (!member) {
        diagnostics.push({
          severity: "error", code: "E519", phase: "check",
          message: `Package '${targetName}' not found in workspace`,
          location: { file: "", line: 0, col: 0 },
        });
        emitCheckResult(startTime, phaseTiming, false, null, diagnostics);
        return;
      }
      addWithDeps(member.name);
    }

    const fileResults: { file: string; ok: boolean; errors: number; warnings: number }[] = [];

    for (const memberName of resolution.buildOrder) {
      if (!targetMembers.has(memberName)) continue;
      const member = resolution.graph.members.get(memberName)!;
      const srcDir = join(member.path, "src");
      if (!existsSync(srcDir)) continue;

      const files = collectClkFiles([srcDir]);
      for (const file of files) {
        const absFile = resolve(file);
        const source = readFileSync(absFile, "utf-8");
        let fileErrors = 0;
        let fileWarnings = 0;

        let phaseStart = Date.now();
        const tokens = lex(source);
        phaseTiming.lex = (phaseTiming.lex ?? 0) + (Date.now() - phaseStart);

        if (!Array.isArray(tokens)) {
          diagnostics.push(toDiagnostic(tokens, absFile));
          fileResults.push({ file, ok: false, errors: 1, warnings: 0 });
          continue;
        }

        phaseStart = Date.now();
        const ast = parse(tokens);
        phaseTiming.parse = (phaseTiming.parse ?? 0) + (Date.now() - phaseStart);

        if ("code" in ast) {
          diagnostics.push(toDiagnostic(ast as any, absFile));
          fileResults.push({ file, ok: false, errors: 1, warnings: 0 });
          continue;
        }

        phaseStart = Date.now();
        const program: Program = {
          topLevels: (ast as Program).topLevels.map(tl => {
            if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
            if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
            return tl;
          }),
        };
        phaseTiming.desugar = (phaseTiming.desugar ?? 0) + (Date.now() - phaseStart);

        phaseStart = Date.now();
        const typeErrors = typeCheck(program);
        phaseTiming.check = (phaseTiming.check ?? 0) + (Date.now() - phaseStart);

        for (const err of typeErrors) {
          const diag = toDiagnostic(err, absFile);
          diagnostics.push(diag);
          if (diag.severity === "error") fileErrors++;
          else if (diag.severity === "warning") fileWarnings++;
        }

        fileResults.push({ file, ok: fileErrors === 0, errors: fileErrors, warnings: fileWarnings });
      }
    }

    const hasErrors = diagnostics.some(d => d.severity === "error");
    emitCheckResult(startTime, phaseTiming, !hasErrors, { files: fileResults }, diagnostics);
  } catch (err) {
    diagnostics.push({
      severity: "error", code: "E520", phase: "check",
      message: err instanceof Error ? err.message : String(err),
      location: { file: "", line: 0, col: 0 },
    });
    emitCheckResult(startTime, phaseTiming, false, null, diagnostics);
  }
}

// ── Lint subcommand ──

function runLint(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  // Build targets: skip "lint", --flag, --rule <value> pairs
  const targets: string[] = [];
  {
    const lintArgs = args.slice(args.indexOf("lint") + 1);
    for (let i = 0; i < lintArgs.length; i++) {
      if (lintArgs[i] === "--rule") { i++; continue; } // skip --rule and its value
      if (lintArgs[i].startsWith("--")) continue;       // skip other flags
      targets.push(lintArgs[i]);
    }
  }

  if (targets.length === 0) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "no input file specified",
      location: { file: "", line: 0, col: 0 },
    });
    emitLintResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  // Parse --rule flags: +ruleName to enable, -ruleName to disable
  const enabledRules = new Set<string>();
  const disabledRules = new Set<string>();
  let hasExplicitEnable = false;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--rule" && i + 1 < args.length) {
      const ruleArg = args[i + 1];
      if (ruleArg.startsWith("-")) {
        disabledRules.add(ruleArg.slice(1));
      } else if (ruleArg.startsWith("+")) {
        enabledRules.add(ruleArg.slice(1));
        hasExplicitEnable = true;
      } else {
        enabledRules.add(ruleArg);
        hasExplicitEnable = true;
      }
      i++; // skip the value
    }
  }

  const lintOpts: LintOptions = {};
  if (hasExplicitEnable) lintOpts.enabledRules = enabledRules;
  else if (disabledRules.size > 0) lintOpts.disabledRules = disabledRules;

  const files = collectClkFiles(targets);
  if (files.length === 0) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "no .clk files found",
      location: { file: "", line: 0, col: 0 },
    });
    emitLintResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  const fileResults: { file: string; warnings: number }[] = [];

  for (const file of files) {
    const absFile = resolve(file);
    const source = readFileSync(absFile, "utf-8");

    // Lex
    let phaseStart = Date.now();
    const tokens = lex(source);
    phaseTiming.lex = (phaseTiming.lex ?? 0) + (Date.now() - phaseStart);

    if (!Array.isArray(tokens)) {
      diagnostics.push(toDiagnostic(tokens, absFile));
      fileResults.push({ file, warnings: 0 });
      continue;
    }

    // Parse
    phaseStart = Date.now();
    const ast = parse(tokens);
    phaseTiming.parse = (phaseTiming.parse ?? 0) + (Date.now() - phaseStart);

    if ("code" in ast) {
      diagnostics.push(toDiagnostic(ast as any, absFile));
      fileResults.push({ file, warnings: 0 });
      continue;
    }

    // Desugar
    phaseStart = Date.now();
    const program: Program = {
      topLevels: (ast as Program).topLevels.map(tl => {
        if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
        if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
        return tl;
      }),
    };
    phaseTiming.desugar = (phaseTiming.desugar ?? 0) + (Date.now() - phaseStart);

    // Lint
    phaseStart = Date.now();
    const lintDiags = lint(program, lintOpts);
    phaseTiming.lint = (phaseTiming.lint ?? 0) + (Date.now() - phaseStart);

    for (const ld of lintDiags) {
      diagnostics.push({
        severity: "warning",
        code: ld.code,
        phase: "check",
        message: ld.message,
        location: { file: absFile, line: ld.location.line, col: ld.location.col },
      });
    }

    fileResults.push({ file, warnings: lintDiags.length });
  }

  const hasErrors = diagnostics.some(d => d.severity === "error");
  emitLintResult(startTime, phaseTiming, !hasErrors, { files: fileResults }, diagnostics);
}

function emitLintResult(
  startTime: number,
  phaseTiming: Record<string, number>,
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
): void {
  const totalMs = Date.now() - startTime;
  if (jsonMode) {
    console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  } else {
    for (const d of diagnostics) {
      const prefix = d.severity === "warning" ? "warning" : "error";
      const loc = d.location.file ? `${d.location.file}:${d.location.line}:${d.location.col}` : "";
      console.error(`${prefix}[${d.code}]: ${d.message}${loc ? ` [${loc}]` : ""}`);
    }
  }
  process.exit(ok ? 0 : 1);
}

// ── Eval subcommand ──

function runEval(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  // Parse eval-specific flags
  const fileFlag = getFlagValue("--file");
  const sessionFlag = getFlagValue("--session");
  const typeMode = args.includes("--type");
  const bindingsMode = args.includes("--bindings");
  const resetMode = args.includes("--reset");

  // The expression is the last non-flag argument after "eval"
  const exprArgs = nonFlagArgs.slice(1);
  const exprStr = exprArgs[exprArgs.length - 1];

  // Handle session --bindings (list bindings, no expression needed)
  if (sessionFlag && bindingsMode) {
    const sessionPath = resolveSessionPath(sessionFlag);
    if (!existsSync(sessionPath)) {
      emitEvalResult(startTime, phaseTiming, true, { bindings: [] }, diagnostics);
      return;
    }
    const session = JSON.parse(readFileSync(sessionPath, "utf-8"));
    emitEvalResult(startTime, phaseTiming, true, { bindings: session.bindings ?? [] }, diagnostics);
    return;
  }

  // Handle session --reset
  if (sessionFlag && resetMode) {
    const sessionPath = resolveSessionPath(sessionFlag);
    if (existsSync(sessionPath)) {
      writeFileSync(sessionPath, JSON.stringify({ bindings: [] }));
    }
    emitEvalResult(startTime, phaseTiming, true, { reset: true }, diagnostics);
    return;
  }

  if (!exprStr) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "no expression provided",
      location: { file: "<eval>", line: 0, col: 0 },
    });
    emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  // Load file definitions if --file is set
  let fileProgram: Program | undefined;
  let baseDir = ".";
  if (fileFlag) {
    const absFile = resolve(fileFlag);
    baseDir = dirname(absFile);
    const fileSrc = readFileSync(absFile, "utf-8");

    let phaseStart = Date.now();
    const fileToks = lex(fileSrc);
    phaseTiming.lex = Date.now() - phaseStart;

    if (!Array.isArray(fileToks)) {
      diagnostics.push(toDiagnostic(fileToks, absFile));
      emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    phaseStart = Date.now();
    const fileAst = parse(fileToks);
    phaseTiming.parse = Date.now() - phaseStart;

    if ("code" in fileAst) {
      diagnostics.push(toDiagnostic(fileAst as any, absFile));
      emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    fileProgram = {
      topLevels: (fileAst as Program).topLevels.map(tl => {
        if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
        if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
        return tl;
      }),
    };
  }

  // Lex the expression
  let phaseStart = Date.now();
  const exprToks = lex(exprStr);
  phaseTiming.lex = phaseTiming.lex ? phaseTiming.lex + (Date.now() - phaseStart) : Date.now() - phaseStart;

  if (!Array.isArray(exprToks)) {
    diagnostics.push(toDiagnostic(exprToks, "<eval>"));
    emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  // Parse the expression
  phaseStart = Date.now();
  const exprAst = parseExpression(exprToks);
  phaseTiming.parse = phaseTiming.parse ? phaseTiming.parse + (Date.now() - phaseStart) : Date.now() - phaseStart;

  if ("code" in exprAst) {
    diagnostics.push(toDiagnostic(exprAst as any, "<eval>"));
    emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  // Desugar the expression
  phaseStart = Date.now();
  const desugared = desugar(exprAst);
  phaseTiming.desugar = Date.now() - phaseStart;

  // Session mode: load/save bindings
  if (sessionFlag) {
    phaseStart = Date.now();
    const sessionPath = resolveSessionPath(sessionFlag);
    const env = createEnv(fileProgram, baseDir);

    // Restore previous bindings
    if (existsSync(sessionPath)) {
      const session = JSON.parse(readFileSync(sessionPath, "utf-8"));
      for (const binding of session.bindings ?? []) {
        const bindToks = lex(binding.expr);
        if (!Array.isArray(bindToks)) continue;
        const bindAst = parseExpression(bindToks);
        if ("code" in bindAst) continue;
        const bindDesugared = desugar(bindAst);
        const result = evalExprWithEnv(bindDesugared, env);
        if (result.ok) {
          env.set(binding.name, result.value);
        }
      }
    }

    // Evaluate the new expression
    const result = evalExprWithEnv(desugared, env);
    phaseTiming.eval = Date.now() - phaseStart;

    if (!result.ok) {
      diagnostics.push(toDiagnostic(result.error, "<eval>"));
      emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    // If this is a let binding (top-level, no body), save to session
    if (exprAst.tag === "let" && exprAst.body === null) {
      const sessionData = existsSync(sessionPath)
        ? JSON.parse(readFileSync(sessionPath, "utf-8"))
        : { bindings: [] };
      // Update or add binding
      const existing = sessionData.bindings.findIndex((b: any) => b.name === exprAst.name);
      const bindingEntry = { name: exprAst.name, expr: exprStr, type: valueTypeName(result.value) };
      if (existing >= 0) {
        sessionData.bindings[existing] = bindingEntry;
      } else {
        sessionData.bindings.push(bindingEntry);
      }
      ensureDir(dirname(sessionPath));
      writeFileSync(sessionPath, JSON.stringify(sessionData, null, 2));
    }

    emitEvalResult(startTime, phaseTiming, true, {
      value: valueToJSON(result.value),
      type: valueTypeName(result.value),
      effects: [],
    }, diagnostics);
    return;
  }

  // Type-only mode: we can't do full type inference without running the checker on a program,
  // but we can evaluate and report the type of the result
  if (typeMode) {
    phaseStart = Date.now();
    const result = evalExpr(desugared, fileProgram, baseDir);
    phaseTiming.eval = Date.now() - phaseStart;

    if (!result.ok) {
      diagnostics.push(toDiagnostic(result.error, "<eval>"));
      emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    emitEvalResult(startTime, phaseTiming, true, {
      type: valueTypeName(result.value),
      effects: [],
      constraints: [],
    }, diagnostics);
    return;
  }

  // Normal eval
  phaseStart = Date.now();
  const result = evalExpr(desugared, fileProgram, baseDir);
  phaseTiming.eval = Date.now() - phaseStart;

  if (!result.ok) {
    diagnostics.push(toDiagnostic(result.error, "<eval>"));
    emitEvalResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  emitEvalResult(startTime, phaseTiming, true, {
    value: valueToJSON(result.value),
    type: valueTypeName(result.value),
    effects: [],
  }, diagnostics);
}

function emitEvalResult(
  startTime: number,
  phaseTiming: Record<string, number>,
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
): void {
  const totalMs = Date.now() - startTime;
  if (jsonMode) {
    console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  } else if (!ok) {
    for (const d of diagnostics) {
      console.error(`${d.code}: ${d.message}`);
    }
  } else if (data && typeof data === "object" && "value" in data) {
    console.log(JSON.stringify((data as any).value));
  } else if (data && typeof data === "object" && "type" in data) {
    console.log((data as any).type);
  } else if (data && typeof data === "object" && "bindings" in data) {
    for (const b of (data as any).bindings) {
      console.log(`${b.name}: ${b.type} = ${b.expr}`);
    }
  } else if (data && typeof data === "object" && "reset" in data) {
    console.log("session reset");
  }
  process.exit(ok ? 0 : 1);
}

function getFlagValue(flag: string): string | undefined {
  const idx = args.indexOf(flag);
  if (idx === -1 || idx + 1 >= args.length) return undefined;
  return args[idx + 1];
}

function resolveSessionPath(name: string): string {
  return resolve(".clank", "sessions", `${name}.json`);
}

function ensureDir(dir: string): void {
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }
}

// ── Doc subcommand ──

function runDoc(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  // doc search <query> [files...]
  // doc show <name> [files...]
  const subCmd = nonFlagArgs[1]; // "search" or "show"
  const query = nonFlagArgs[2];

  if (!subCmd || (subCmd !== "search" && subCmd !== "show")) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: "usage: clank doc search <query> [files...] | clank doc show <name> [files...]",
      location: { file: "", line: 0, col: 0 },
    });
    emitDocResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  if (!query) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: `no ${subCmd === "search" ? "query" : "name"} provided`,
      location: { file: "", line: 0, col: 0 },
    });
    emitDocResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  // Collect all entries: builtins + any provided files
  let phaseStart = Date.now();
  const allEntries = [...getAllBuiltinEntries()];

  const fileTargets = nonFlagArgs.slice(3);
  if (fileTargets.length > 0) {
    const files = collectClkFiles(fileTargets);
    for (const file of files) {
      const absFile = resolve(file);
      const source = readFileSync(absFile, "utf-8");

      const tokens = lex(source);
      if (!Array.isArray(tokens)) continue;

      const ast = parse(tokens);
      if ("code" in ast) continue;

      const program: Program = {
        topLevels: (ast as Program).topLevels.map(tl => {
          if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
          if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
          return tl;
        }),
      };

      allEntries.push(...extractProgramEntries(program, file));
    }
  }
  phaseTiming.collect = Date.now() - phaseStart;

  if (subCmd === "search") {
    phaseStart = Date.now();
    const results = searchEntries(allEntries, query);
    phaseTiming.search = Date.now() - phaseStart;

    const data = {
      query,
      count: results.length,
      entries: results.map(e => entryToJSON(e)),
    };

    if (jsonMode) {
      emitDocResult(startTime, phaseTiming, true, data, diagnostics);
    } else {
      if (results.length === 0) {
        console.log(`No results for "${query}"`);
      } else {
        for (const entry of results) {
          console.log(formatEntryShort(entry));
        }
      }
      process.exit(0);
    }
  } else {
    // show
    phaseStart = Date.now();
    const entry = findEntry(allEntries, query);
    phaseTiming.search = Date.now() - phaseStart;

    if (!entry) {
      diagnostics.push({
        severity: "error",
        code: "E001",
        phase: "lex",
        message: `no entry found for '${query}'`,
        location: { file: "", line: 0, col: 0 },
      });
      emitDocResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    if (jsonMode) {
      emitDocResult(startTime, phaseTiming, true, entryToJSON(entry), diagnostics);
    } else {
      console.log(formatEntryDetailed(entry));
      process.exit(0);
    }
  }
}

function emitDocResult(
  startTime: number,
  phaseTiming: Record<string, number>,
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
): void {
  const totalMs = Date.now() - startTime;
  if (jsonMode) {
    console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  } else if (!ok) {
    for (const d of diagnostics) {
      console.error(`${d.code}: ${d.message}`);
    }
  }
  process.exit(ok ? 0 : 1);
}

// ── Test subcommand ──

function runTest(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  const filterFlag = getFlagValue("--filter");
  const allFlag = args.includes("--all");
  const packageFlag = getFlagValue("--package");

  // Workspace-aware test
  if (allFlag || packageFlag) {
    runWorkspaceTest(startTime, phaseTiming, diagnostics, allFlag, packageFlag, filterFlag);
    return;
  }

  const targets = nonFlagArgs.slice(1); // after "test"

  // Default: look for test/ directory if no targets given
  const effectiveTargets = targets.length > 0 ? targets : ["test/"];

  // Collect .clk files
  let files: string[];
  try {
    files = collectClkFiles(effectiveTargets);
  } catch {
    files = [];
  }

  if (files.length === 0) {
    diagnostics.push({
      severity: "error",
      code: "E001",
      phase: "lex",
      message: targets.length > 0 ? "no .clk files found" : "no test files found (looked in test/)",
      location: { file: "", line: 0, col: 0 },
    });
    emitTestResult(startTime, phaseTiming, false, null, diagnostics);
    return;
  }

  const testResults: {
    name: string;
    module: string;
    status: "pass" | "fail";
    duration_ms: number;
    failure?: { message: string; location: { file: string; line: number; col: number }; expected?: string; actual?: string };
  }[] = [];

  for (const file of files) {
    const absFile = resolve(file);
    const source = readFileSync(absFile, "utf-8");

    // Lex
    let phaseStart = Date.now();
    const tokens = lex(source);
    phaseTiming.lex = (phaseTiming.lex ?? 0) + (Date.now() - phaseStart);

    if (!Array.isArray(tokens)) {
      diagnostics.push(toDiagnostic(tokens, absFile));
      continue;
    }

    // Parse
    phaseStart = Date.now();
    const ast = parse(tokens);
    phaseTiming.parse = (phaseTiming.parse ?? 0) + (Date.now() - phaseStart);

    if ("code" in ast) {
      diagnostics.push(toDiagnostic(ast as any, absFile));
      continue;
    }

    // Desugar
    phaseStart = Date.now();
    const program: Program = {
      topLevels: (ast as Program).topLevels.map(tl => {
        if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
        if (tl.tag === "test-decl") return { ...tl, body: desugar(tl.body) };
        return tl;
      }),
    };
    phaseTiming.desugar = (phaseTiming.desugar ?? 0) + (Date.now() - phaseStart);

    // Extract module name from mod-decl
    const modDecl = program.topLevels.find(tl => tl.tag === "mod-decl");
    const moduleName = modDecl && modDecl.tag === "mod-decl" ? modDecl.path.join(".") : file;

    // Collect test declarations
    const testDecls = program.topLevels.filter(
      (tl): tl is Extract<typeof tl, { tag: "test-decl" }> => tl.tag === "test-decl"
    );

    // Also find fn test_* definitions
    const testDefs = program.topLevels.filter(
      (tl): tl is Extract<typeof tl, { tag: "definition" }> =>
        tl.tag === "definition" && tl.name.startsWith("test_")
    );

    if (testDecls.length === 0 && testDefs.length === 0) continue;

    // Set up the environment: load non-test definitions + imports
    phaseStart = Date.now();
    const env = createEnv(program, dirname(absFile));
    phaseTiming.eval = (phaseTiming.eval ?? 0) + (Date.now() - phaseStart);

    // Run test declarations: test "name" = expr
    for (const td of testDecls) {
      if (filterFlag && !td.name.includes(filterFlag)) continue;

      const testStart = Date.now();
      try {
        const result = evalExprWithEnv(td.body, env);
        const duration = Date.now() - testStart;
        phaseTiming.eval = (phaseTiming.eval ?? 0) + duration;

        if (!result.ok) {
          testResults.push({
            name: td.name,
            module: moduleName,
            status: "fail",
            duration_ms: duration,
            failure: {
              message: result.error.message,
              location: { file: absFile, line: result.error.location.line, col: result.error.location.col },
            },
          });
        } else {
          testResults.push({
            name: td.name,
            module: moduleName,
            status: "pass",
            duration_ms: duration,
          });
        }
      } catch (e: unknown) {
        const duration = Date.now() - testStart;
        phaseTiming.eval = (phaseTiming.eval ?? 0) + duration;
        const msg = e && typeof e === "object" && "message" in e ? (e as any).message : String(e);
        const loc = e && typeof e === "object" && "location" in e ? (e as any).location : td.loc;
        testResults.push({
          name: td.name,
          module: moduleName,
          status: "fail",
          duration_ms: duration,
          failure: {
            message: msg,
            location: { file: absFile, line: loc.line ?? 0, col: loc.col ?? 0 },
          },
        });
      }
    }

    // Run fn test_* definitions: call them with no args
    for (const td of testDefs) {
      const displayName = td.name;
      if (filterFlag && !displayName.includes(filterFlag)) continue;

      const testStart = Date.now();
      try {
        const callExpr: import("./ast.js").Expr = {
          tag: "apply",
          fn: { tag: "var", name: td.name, loc: td.loc },
          args: [],
          loc: td.loc,
        };
        const result = evalExprWithEnv(callExpr, env);
        const duration = Date.now() - testStart;
        phaseTiming.eval = (phaseTiming.eval ?? 0) + duration;

        if (!result.ok) {
          testResults.push({
            name: displayName,
            module: moduleName,
            status: "fail",
            duration_ms: duration,
            failure: {
              message: result.error.message,
              location: { file: absFile, line: result.error.location.line, col: result.error.location.col },
            },
          });
        } else {
          testResults.push({
            name: displayName,
            module: moduleName,
            status: "pass",
            duration_ms: duration,
          });
        }
      } catch (e: unknown) {
        const duration = Date.now() - testStart;
        phaseTiming.eval = (phaseTiming.eval ?? 0) + duration;
        const msg = e && typeof e === "object" && "message" in e ? (e as any).message : String(e);
        const loc = e && typeof e === "object" && "location" in e ? (e as any).location : td.loc;
        testResults.push({
          name: displayName,
          module: moduleName,
          status: "fail",
          duration_ms: duration,
          failure: {
            message: msg,
            location: { file: absFile, line: loc.line ?? 0, col: loc.col ?? 0 },
          },
        });
      }
    }
  }

  const totalPassed = testResults.filter(t => t.status === "pass").length;
  const totalFailed = testResults.filter(t => t.status === "fail").length;
  const allPassed = totalFailed === 0 && testResults.length > 0;

  const summary = {
    total: testResults.length,
    passed: totalPassed,
    failed: totalFailed,
    skipped: 0,
  };

  emitTestResult(startTime, phaseTiming, allPassed, { summary, tests: testResults }, diagnostics);
}

function emitTestResult(
  startTime: number,
  phaseTiming: Record<string, number>,
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
): void {
  const totalMs = Date.now() - startTime;
  if (jsonMode) {
    console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
  } else {
    // Human-readable output
    for (const d of diagnostics) {
      const prefix = d.severity === "warning" ? "warning" : "error";
      const loc = d.location.file ? `${d.location.file}:${d.location.line}:${d.location.col}` : "";
      console.error(`${prefix}: ${d.message}${loc ? ` [${loc}]` : ""}`);
    }
    if (data && typeof data === "object" && "tests" in data) {
      const { summary, tests } = data as any;
      for (const t of tests) {
        if (t.status === "pass") {
          console.log(`  ok - ${t.module} > ${t.name}`);
        } else {
          console.log(`  FAIL - ${t.module} > ${t.name}`);
          if (t.failure) {
            console.log(`    ${t.failure.message}`);
          }
        }
      }
      console.log(`\n${summary.total} tests: ${summary.passed} passed, ${summary.failed} failed`);
    }
  }
  process.exit(ok ? 0 : 1);
}

function runWorkspaceTest(
  startTime: number,
  phaseTiming: Record<string, number>,
  diagnostics: Diagnostic[],
  allFlag: boolean,
  packageFlag: string | undefined,
  filterFlag: string | undefined,
): void {
  try {
    const ctx = findWorkspaceRoot(process.cwd());
    if (ctx.mode !== "workspace" || !ctx.workspaceRoot) {
      diagnostics.push({
        severity: "error", code: "E518", phase: "test",
        message: "Not inside a workspace",
        location: { file: "", line: 0, col: 0 },
      });
      emitTestResult(startTime, phaseTiming, false, null, diagnostics);
      return;
    }

    const resolution = resolveWorkspace(ctx.workspaceRoot);
    let targetMembers: Set<string>;

    if (allFlag) {
      targetMembers = new Set(resolution.members.map(m => m.name));
    } else {
      const targetName = packageFlag;
      if (!targetName) {
        diagnostics.push({
          severity: "error", code: "E519", phase: "test",
          message: "Workspace command requires --package or --all",
          location: { file: "", line: 0, col: 0 },
        });
        emitTestResult(startTime, phaseTiming, false, null, diagnostics);
        return;
      }
      const member = resolution.members.find(m => m.name === targetName);
      if (!member) {
        diagnostics.push({
          severity: "error", code: "E519", phase: "test",
          message: `Package '${targetName}' not found in workspace`,
          location: { file: "", line: 0, col: 0 },
        });
        emitTestResult(startTime, phaseTiming, false, null, diagnostics);
        return;
      }
      targetMembers = new Set([member.name]);
    }

    const memberResults: {
      name: string;
      summary: { total: number; passed: number; failed: number; skipped: number };
      status: "pass" | "fail" | "no_tests";
      duration_ms: number;
    }[] = [];

    // Run tests in dependency order
    for (const memberName of resolution.buildOrder) {
      if (!targetMembers.has(memberName)) continue;
      const member = resolution.graph.members.get(memberName)!;
      const testDir = join(member.path, "test");
      const memberStart = Date.now();

      let testFiles: string[];
      try {
        testFiles = existsSync(testDir) ? collectClkFiles([testDir]) : [];
      } catch {
        testFiles = [];
      }

      if (testFiles.length === 0) {
        memberResults.push({
          name: memberName,
          summary: { total: 0, passed: 0, failed: 0, skipped: 0 },
          status: "no_tests",
          duration_ms: Date.now() - memberStart,
        });
        continue;
      }

      let memberPassed = 0;
      let memberFailed = 0;
      let memberTotal = 0;

      for (const file of testFiles) {
        const absFile = resolve(file);
        const source = readFileSync(absFile, "utf-8");

        let phaseStart = Date.now();
        const tokens = lex(source);
        phaseTiming.lex = (phaseTiming.lex ?? 0) + (Date.now() - phaseStart);

        if (!Array.isArray(tokens)) {
          diagnostics.push(toDiagnostic(tokens, absFile));
          continue;
        }

        phaseStart = Date.now();
        const ast = parse(tokens);
        phaseTiming.parse = (phaseTiming.parse ?? 0) + (Date.now() - phaseStart);

        if ("code" in ast) {
          diagnostics.push(toDiagnostic(ast as any, absFile));
          continue;
        }

        phaseStart = Date.now();
        const program: Program = {
          topLevels: (ast as Program).topLevels.map(tl => {
            if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
            if (tl.tag === "test-decl") return { ...tl, body: desugar(tl.body) };
            return tl;
          }),
        };
        phaseTiming.desugar = (phaseTiming.desugar ?? 0) + (Date.now() - phaseStart);

        const modDecl = program.topLevels.find(tl => tl.tag === "mod-decl");
        const moduleName = modDecl && modDecl.tag === "mod-decl" ? modDecl.path.join(".") : file;

        const testDecls = program.topLevels.filter(
          (tl): tl is Extract<typeof tl, { tag: "test-decl" }> => tl.tag === "test-decl"
        );
        const testDefs = program.topLevels.filter(
          (tl): tl is Extract<typeof tl, { tag: "definition" }> =>
            tl.tag === "definition" && tl.name.startsWith("test_")
        );

        if (testDecls.length === 0 && testDefs.length === 0) continue;

        phaseStart = Date.now();
        const env = createEnv(program, dirname(absFile));
        phaseTiming.eval = (phaseTiming.eval ?? 0) + (Date.now() - phaseStart);

        for (const td of testDecls) {
          if (filterFlag && !td.name.includes(filterFlag)) continue;
          memberTotal++;
          const testStart = Date.now();
          try {
            const result = evalExprWithEnv(td.body, env);
            if (result.ok) {
              memberPassed++;
            } else {
              memberFailed++;
            }
          } catch {
            memberFailed++;
          }
        }

        for (const td of testDefs) {
          if (filterFlag && !td.name.includes(filterFlag)) continue;
          memberTotal++;
          try {
            const result = evalExprWithEnv(td.body, env);
            if (result.ok) {
              memberPassed++;
            } else {
              memberFailed++;
            }
          } catch {
            memberFailed++;
          }
        }
      }

      memberResults.push({
        name: memberName,
        summary: { total: memberTotal, passed: memberPassed, failed: memberFailed, skipped: 0 },
        status: memberFailed > 0 ? "fail" : "pass",
        duration_ms: Date.now() - memberStart,
      });
    }

    const wsSummary = {
      total: memberResults.reduce((s, m) => s + m.summary.total, 0),
      passed: memberResults.reduce((s, m) => s + m.summary.passed, 0),
      failed: memberResults.reduce((s, m) => s + m.summary.failed, 0),
      skipped: 0,
    };

    const ok = wsSummary.failed === 0;
    const data = {
      members: memberResults,
      workspace_summary: wsSummary,
    };

    if (jsonMode) {
      const totalMs = Date.now() - startTime;
      console.log(JSON.stringify(makeEnvelope(ok, data, diagnostics, { total_ms: totalMs, phases: phaseTiming })));
    } else {
      for (const m of memberResults) {
        if (m.status === "no_tests") continue;
        const icon = m.status === "pass" ? "ok" : "FAIL";
        console.log(`  ${icon} - ${m.name} (${m.summary.passed}/${m.summary.total} passed)`);
      }
      console.log(`\nWorkspace: ${wsSummary.total} tests: ${wsSummary.passed} passed, ${wsSummary.failed} failed`);
    }
    process.exit(ok ? 0 : 1);
  } catch (err) {
    diagnostics.push({
      severity: "error", code: "E520", phase: "test",
      message: err instanceof Error ? err.message : String(err),
      location: { file: "", line: 0, col: 0 },
    });
    emitTestResult(startTime, phaseTiming, false, null, diagnostics);
  }
}

// ── File execution (original mode) ──

// ── Pkg subcommand ──

function runPkg(): void {
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  const subcommand = nonFlagArgs[1]; // "init" or "resolve"

  if (subcommand === "init") {
    const nameIdx = args.indexOf("--name");
    const name = nameIdx >= 0 ? args[nameIdx + 1] : undefined;
    const entryIdx = args.indexOf("--entry");
    const entry = entryIdx >= 0 ? args[entryIdx + 1] : undefined;

    const resolveStart = Date.now();
    const result = pkgInit({ name, entry });
    phaseTiming["init"] = Date.now() - resolveStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E508", phase: "link",
          message: result.error!, location: { file: "clank.pkg", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        console.log(`Initialized package '${result.data!.package}'`);
        for (const f of result.data!.created_files) {
          console.log(`  created ${f}`);
        }
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "add") {
    const nameIdx = args.indexOf("--name");
    const name = nameIdx >= 0 ? args[nameIdx + 1] : undefined;
    const pathIdx = args.indexOf("--path");
    const depPath = pathIdx >= 0 ? args[pathIdx + 1] : undefined;
    const githubIdx = args.indexOf("--github");
    const github = githubIdx >= 0 ? args[githubIdx + 1] : undefined;
    const versionIdx = args.indexOf("--version");
    const constraint = versionIdx >= 0 ? args[versionIdx + 1] : undefined;
    const dev = args.includes("--dev");

    if (!name) {
      const msg = "Missing required --name flag for pkg add";
      if (jsonMode) {
        diagnostics.push({
          severity: "error", code: "E508", phase: "link",
          message: msg, location: { file: "<cli>", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        console.error(`Error: ${msg}`);
        process.exit(1);
      }
      return;
    }

    const addStart = Date.now();
    const result = pkgAdd({ name, constraint, path: depPath, github, dev });
    phaseTiming["add"] = Date.now() - addStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E508", phase: "link",
          message: result.error!, location: { file: "clank.pkg", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        const d = result.data!;
        const desc = d.github ? `{ github = "${d.github}"${d.constraint !== "*" ? `, version = "${d.constraint}"` : ""} }` : d.path ? `{ path = "${d.path}" }` : `"${d.constraint}"`;
        console.log(`Added ${d.name} = ${desc} to [${d.section}]`);
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "remove") {
    const nameIdx = args.indexOf("--name");
    const name = nameIdx >= 0 ? args[nameIdx + 1] : undefined;
    const dev = args.includes("--dev");

    if (!name) {
      const msg = "Missing required --name flag for pkg remove";
      if (jsonMode) {
        diagnostics.push({
          severity: "error", code: "E508", phase: "link",
          message: msg, location: { file: "<cli>", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        console.error(`Error: ${msg}`);
        process.exit(1);
      }
      return;
    }

    const removeStart = Date.now();
    const result = pkgRemove({ name, dev });
    phaseTiming["remove"] = Date.now() - removeStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E508", phase: "link",
          message: result.error!, location: { file: "clank.pkg", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        console.log(`Removed ${result.data!.name} from [${result.data!.section}]`);
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "verify") {
    const verifyStart = Date.now();
    const manifestPath = findManifest(".");
    phaseTiming["verify"] = Date.now() - verifyStart;

    if (!manifestPath) {
      const msg = "No clank.pkg found in current directory or any parent";
      if (jsonMode) {
        diagnostics.push({
          severity: "error", code: "E508", phase: "link",
          message: msg, location: { file: "<cli>", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        console.error(`Error: ${msg}`);
        process.exit(1);
      }
      return;
    }

    const result = verifyLockfile(manifestPath);
    phaseTiming["verify"] = Date.now() - verifyStart;

    if (jsonMode) {
      console.log(JSON.stringify(makeEnvelope(result.ok, {
        ok: result.ok,
        stale: result.stale,
        missing: result.missing,
        extra: result.extra,
      }, diagnostics, {
        total_ms: Date.now() - startTime, phases: phaseTiming,
      })));
    } else {
      if (result.ok) {
        console.log("Lockfile is up to date.");
      } else {
        if (result.missing.length > 0) {
          console.error(`Missing from lockfile: ${result.missing.join(", ")}`);
        }
        if (result.stale.length > 0) {
          console.error(`Stale in lockfile: ${result.stale.join(", ")}`);
        }
        if (result.extra.length > 0) {
          console.error(`Extra in lockfile: ${result.extra.join(", ")}`);
        }
        console.error("Run 'clank pkg resolve' to update the lockfile.");
        process.exit(1);
      }
    }
  } else if (subcommand === "resolve") {
    const resolveStart = Date.now();
    const result = pkgResolve();
    phaseTiming["resolve"] = Date.now() - resolveStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E502", phase: "link",
          message: result.error!, location: { file: "clank.pkg", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        const data = result.data!;
        if (data.packages.length === 0) {
          console.log("No local dependencies to resolve.");
        } else {
          console.log(`Resolved ${data.packages.length} package(s):`);
          for (const pkg of data.packages) {
            console.log(`  ${pkg.name}@${pkg.version} (${pkg.path})`);
            for (const mod of pkg.modules) {
              console.log(`    ${mod}`);
            }
          }
        }
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "install") {
    const dev = args.includes("--dev");

    const installStart = Date.now();
    const result = pkgInstall({ dev });
    phaseTiming["install"] = Date.now() - installStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E509", phase: "link",
          message: result.error!, location: { file: "clank.pkg", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        const d = result.data!;
        if (d.installed.length === 0) {
          console.log("No remote dependencies to install.");
        } else {
          console.log(`Installed ${d.installed.length} package(s):`);
          for (const pkg of d.installed) {
            console.log(`  ${pkg.name}@${pkg.version} (github.com/${pkg.github})`);
          }
          console.log(`Linked to ${d.linked}`);
        }
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "publish") {
    const dryRun = args.includes("--dry-run");

    const publishStart = Date.now();
    const result = pkgPublish({ dryRun });
    phaseTiming["publish"] = Date.now() - publishStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E510", phase: "link",
          message: result.error!, location: { file: "clank.pkg", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        const d = result.data!;
        if (dryRun) {
          console.log(`Dry run — would publish ${d.package}@${d.version}`);
          console.log(`  tag: ${d.tag}`);
          console.log(`  repository: ${d.repository}`);
          console.log(`  integrity: ${d.integrity}`);
        } else {
          console.log(`Published ${d.package}@${d.version}`);
          console.log(`  tag: ${d.tag}`);
          console.log(`  repository: ${d.repository}`);
          console.log(`  integrity: ${d.integrity}`);
        }
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "search") {
    const queryIdx = args.indexOf("--query");
    const query = queryIdx >= 0 ? args[queryIdx + 1] : nonFlagArgs[2] ?? "";
    const repos: string[] = [];
    for (let i = 0; i < args.length; i++) {
      if (args[i] === "--repo" && args[i + 1]) repos.push(args[i + 1]);
    }

    const searchStart = Date.now();
    const result = pkgSearch({ repos, query });
    phaseTiming["search"] = Date.now() - searchStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E510", phase: "link",
          message: result.error!, location: { file: "<cli>", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        const pkgs = result.data!.packages;
        if (pkgs.length === 0) {
          console.log("No packages found.");
        } else {
          for (const pkg of pkgs) {
            console.log(`${pkg.name} (${pkg.repository}) — ${pkg.description}`);
            if (pkg.keywords.length > 0) console.log(`  keywords: ${pkg.keywords.join(", ")}`);
            console.log(`  latest: ${pkg.latest}`);
          }
        }
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "info") {
    const repoIdx = args.indexOf("--repo");
    const repo = repoIdx >= 0 ? args[repoIdx + 1] : nonFlagArgs[2] ?? "";

    const infoStart = Date.now();
    const result = pkgInfo({ repo });
    phaseTiming["info"] = Date.now() - infoStart;

    if (jsonMode) {
      if (result.ok) {
        console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      } else {
        diagnostics.push({
          severity: "error", code: "E510", phase: "link",
          message: result.error!, location: { file: "<cli>", line: 1, col: 1 },
        });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
          total_ms: Date.now() - startTime, phases: phaseTiming,
        })));
      }
    } else {
      if (result.ok) {
        const d = result.data!;
        console.log(`${d.name} (github.com/${d.repository})`);
        if (d.description) console.log(`  ${d.description}`);
        if (d.license) console.log(`  license: ${d.license}`);
        if (d.authors.length > 0) console.log(`  authors: ${d.authors.join(", ")}`);
        if (d.keywords.length > 0) console.log(`  keywords: ${d.keywords.join(", ")}`);
        console.log(`  latest: ${d.latest}`);
        console.log(`  versions: ${d.versions.join(", ")}`);
      } else {
        console.error(`Error: ${result.error}`);
        process.exit(1);
      }
    }
  } else if (subcommand === "workspace") {
    const wsSubcommand = nonFlagArgs[2]; // "init", "list", "add", "remove"

    if (wsSubcommand === "init") {
      const membersIdx = args.indexOf("--members");
      const membersList = membersIdx >= 0 ? args[membersIdx + 1]?.split(",") : undefined;

      const wsStart = Date.now();
      const result = workspaceInit({ members: membersList });
      phaseTiming["workspace-init"] = Date.now() - wsStart;

      if (jsonMode) {
        if (result.ok) {
          console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
            total_ms: Date.now() - startTime, phases: phaseTiming,
          })));
        } else {
          diagnostics.push({
            severity: "error", code: "E508", phase: "link",
            message: result.error!, location: { file: "clank.workspace", line: 1, col: 1 },
          });
          console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
            total_ms: Date.now() - startTime, phases: phaseTiming,
          })));
        }
      } else {
        if (result.ok) {
          console.log(`Initialized workspace at ${result.data!.root}`);
          if (result.data!.members.length > 0) {
            console.log(`  members: ${result.data!.members.join(", ")}`);
          }
        } else {
          console.error(`Error: ${result.error}`);
          process.exit(1);
        }
      }
    } else if (wsSubcommand === "list") {
      const wsStart = Date.now();
      const result = workspaceList();
      phaseTiming["workspace-list"] = Date.now() - wsStart;

      if (jsonMode) {
        if (result.ok) {
          console.log(JSON.stringify(makeEnvelope(true, result.data, diagnostics, {
            total_ms: Date.now() - startTime, phases: phaseTiming,
          })));
        } else {
          diagnostics.push({
            severity: "error", code: "E518", phase: "link",
            message: result.error!, location: { file: "<cli>", line: 1, col: 1 },
          });
          console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
            total_ms: Date.now() - startTime, phases: phaseTiming,
          })));
        }
      } else {
        if (result.ok) {
          const d = result.data!;
          console.log(`Workspace: ${d.root}`);
          for (const m of d.members) {
            const deps = m.dependents.length > 0 ? ` (depended by: ${m.dependents.join(", ")})` : "";
            console.log(`  ${m.name}@${m.version} — ${m.path}${deps}`);
          }
        } else {
          console.error(`Error: ${result.error}`);
          process.exit(1);
        }
      }
    } else if (wsSubcommand === "add") {
      const memberPath = nonFlagArgs[3];
      if (!memberPath) {
        const msg = "Usage: clank pkg workspace add <path>";
        if (jsonMode) {
          diagnostics.push({ severity: "error", code: "E508", phase: "link", message: msg, location: { file: "<cli>", line: 1, col: 1 } });
          console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, { total_ms: Date.now() - startTime, phases: phaseTiming })));
        } else {
          console.error(msg);
          process.exit(1);
        }
        return;
      }
      const wsStart = Date.now();
      const result = workspaceAddMember(memberPath);
      phaseTiming["workspace-add"] = Date.now() - wsStart;

      if (jsonMode) {
        console.log(JSON.stringify(makeEnvelope(result.ok, result.data, result.ok ? diagnostics : [...diagnostics, { severity: "error" as const, code: "E515", phase: "link", message: result.error!, location: { file: "clank.workspace", line: 1, col: 1 } }], { total_ms: Date.now() - startTime, phases: phaseTiming })));
      } else {
        if (result.ok) {
          console.log(`Added member '${result.data!.name}' at ${result.data!.member}`);
        } else {
          console.error(`Error: ${result.error}`);
          process.exit(1);
        }
      }
    } else if (wsSubcommand === "remove") {
      const memberPath = nonFlagArgs[3];
      if (!memberPath) {
        const msg = "Usage: clank pkg workspace remove <path>";
        if (jsonMode) {
          diagnostics.push({ severity: "error", code: "E508", phase: "link", message: msg, location: { file: "<cli>", line: 1, col: 1 } });
          console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, { total_ms: Date.now() - startTime, phases: phaseTiming })));
        } else {
          console.error(msg);
          process.exit(1);
        }
        return;
      }
      const wsStart = Date.now();
      const result = workspaceRemoveMember(memberPath);
      phaseTiming["workspace-remove"] = Date.now() - wsStart;

      if (jsonMode) {
        console.log(JSON.stringify(makeEnvelope(result.ok, result.data, result.ok ? diagnostics : [...diagnostics, { severity: "error" as const, code: "E515", phase: "link", message: result.error!, location: { file: "clank.workspace", line: 1, col: 1 } }], { total_ms: Date.now() - startTime, phases: phaseTiming })));
      } else {
        if (result.ok) {
          console.log(`Removed member '${result.data!.member}' from workspace`);
        } else {
          console.error(`Error: ${result.error}`);
          process.exit(1);
        }
      }
    } else {
      const msg = wsSubcommand
        ? `Unknown workspace subcommand: ${wsSubcommand}`
        : "Usage: clank pkg workspace <init|list|add|remove>";
      if (jsonMode) {
        diagnostics.push({ severity: "error", code: "E508", phase: "link", message: msg, location: { file: "<cli>", line: 1, col: 1 } });
        console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, { total_ms: Date.now() - startTime, phases: phaseTiming })));
      } else {
        console.error(msg);
        process.exit(1);
      }
    }
  } else {
    const msg = subcommand
      ? `Unknown pkg subcommand: ${subcommand}`
      : "Usage: clank pkg <init|add|remove|install|publish|search|info|verify|resolve|workspace>";
    if (jsonMode) {
      diagnostics.push({
        severity: "error", code: "E508", phase: "link",
        message: msg, location: { file: "<cli>", line: 1, col: 1 },
      });
      console.log(JSON.stringify(makeEnvelope(false, null, diagnostics, {
        total_ms: Date.now() - startTime, phases: phaseTiming,
      })));
    } else {
      console.error(msg);
      process.exit(1);
    }
  }
}

function runFile(): void {
  const file = nonFlagArgs[0];

  if (!file) {
    if (jsonMode) {
      console.log(JSON.stringify(makeEnvelope(false, null, [{
        severity: "error",
        code: "E001",
        phase: "lex",
        message: "no input file specified",
        location: { file: "", line: 0, col: 0 },
      }], { total_ms: 0, phases: {} })));
    } else {
      console.error("Usage: clank [--vm] [--json] <file.clk>");
      console.error("       clank eval [--file <file.clk>] [--session <name>] [--type] [--json] \"<expr>\"");
    }
    process.exit(1);
  }

  const absFile = resolve(file);
  const source = readFileSync(absFile, "utf-8");
  const startTime = Date.now();
  const phaseTiming: Record<string, number> = {};
  const diagnostics: Diagnostic[] = [];

  // Lex
  let phaseStart = Date.now();
  const tokens = lex(source);
  phaseTiming.lex = Date.now() - phaseStart;

  if (!Array.isArray(tokens)) {
    diagnostics.push(toDiagnostic(tokens, absFile));
    if (jsonMode) {
      emitJsonAndExit(startTime, phaseTiming, false, null, diagnostics, absFile);
    } else {
      console.error(JSON.stringify(tokens));
      process.exit(1);
    }
  }

  // Parse
  phaseStart = Date.now();
  const ast = parse(tokens);
  phaseTiming.parse = Date.now() - phaseStart;

  if ("code" in ast) {
    diagnostics.push(toDiagnostic(ast as any, absFile));
    if (jsonMode) {
      emitJsonAndExit(startTime, phaseTiming, false, null, diagnostics, absFile);
    } else {
      console.error(JSON.stringify(ast));
      process.exit(1);
    }
  }

  // Desugar all top-level bodies
  phaseStart = Date.now();
  const program: Program = {
    topLevels: (ast as Program).topLevels.map(tl => {
      if (tl.tag === "definition") {
        return { ...tl, body: desugar(tl.body) };
      }
      if (tl.tag === "impl-block") {
        return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
      }
      return tl;
    }),
  };
  phaseTiming.desugar = Date.now() - phaseStart;

  // Type check
  phaseStart = Date.now();
  const typeErrors = typeCheck(program);
  phaseTiming.check = Date.now() - phaseStart;

  for (const err of typeErrors) {
    diagnostics.push(toDiagnostic(err, absFile));
  }

  const hasErrors = diagnostics.some(d => d.severity === "error");
  if (!jsonMode) {
    for (const err of typeErrors) {
      const sev = err.code.startsWith("W") ? "warning" : "error";
      if (sev === "warning") {
        console.error(JSON.stringify(err));
      }
    }
  }
  if (hasErrors) {
    if (jsonMode) {
      emitJsonAndExit(startTime, phaseTiming, false, null, diagnostics, absFile);
    } else {
      for (const err of typeErrors) {
        if (!err.code.startsWith("W")) {
          console.error(JSON.stringify(err));
        }
      }
      process.exit(1);
    }
  }

  // In --json mode, capture stdout from print() calls in tree-walker
  const capturedStdout: string[] = [];
  const origLog = console.log;
  if (jsonMode) {
    console.log = (...logArgs: unknown[]) => {
      capturedStdout.push(logArgs.map(String).join(" "));
    };
  }

  // Resolve package dependencies if a clank.pkg exists
  let packageModuleMap: Map<string, string> | undefined;
  const manifestPath = findManifest(dirname(absFile));
  if (manifestPath) {
    try {
      const resolution = resolvePackages(manifestPath);
      if (resolution.moduleMap.size > 0) {
        packageModuleMap = resolution.moduleMap;
      }
    } catch {
      // Package resolution failure is non-fatal for file execution
    }

    // Warn if lockfile exists but is stale
    const lockPath = join(dirname(manifestPath), "clank.lock");
    if (existsSync(lockPath)) {
      try {
        const verification = verifyLockfile(manifestPath);
        if (!verification.ok) {
          const parts: string[] = [];
          if (verification.stale.length > 0) parts.push(`stale: ${verification.stale.join(", ")}`);
          if (verification.missing.length > 0) parts.push(`missing: ${verification.missing.join(", ")}`);
          if (verification.extra.length > 0) parts.push(`extra: ${verification.extra.join(", ")}`);
          console.error(`Warning: clank.lock is out of date (${parts.join("; ")}). Run 'clank pkg resolve' to update.`);
        }
      } catch {
        // Verification failure is non-fatal
      }
    }
  }

  // Execute
  phaseStart = Date.now();
  if (useVM) {
    try {
      const resolved = resolveModulesForVM(program, dirname(absFile), packageModuleMap);
      const mod = compileProgram(resolved);
      const { stdout } = execute(mod);
      phaseTiming.eval = Date.now() - phaseStart;
      if (jsonMode) {
        console.log = origLog;
        emitJsonAndExit(startTime, phaseTiming, true, { stdout }, diagnostics, absFile);
      } else {
        for (const line of stdout) {
          console.log(line);
        }
      }
    } catch (e: unknown) {
      phaseTiming.eval = Date.now() - phaseStart;
      console.log = origLog;
      if (isRuntimeError(e)) {
        diagnostics.push(toDiagnostic(e, absFile));
        if (jsonMode) {
          emitJsonAndExit(startTime, phaseTiming, false, null, diagnostics, absFile);
        } else {
          console.error(JSON.stringify(e));
          process.exit(1);
        }
      }
      throw e;
    }
  } else {
    const result = run(program, dirname(absFile), packageModuleMap);
    phaseTiming.eval = Date.now() - phaseStart;
    console.log = origLog;
    if (result && typeof result === "object" && "code" in result && "message" in result) {
      diagnostics.push(toDiagnostic(result as any, absFile));
      if (jsonMode) {
        emitJsonAndExit(startTime, phaseTiming, false, null, diagnostics, absFile);
      } else {
        console.error(JSON.stringify(result));
        process.exit(1);
      }
    }
    if (jsonMode) {
      emitJsonAndExit(startTime, phaseTiming, true, { stdout: capturedStdout }, diagnostics, absFile);
    }
  }

  // ── Helpers scoped to runFile ──

  function emitJsonAndExit(startTime: number, phaseTiming: Record<string, number>, ok: boolean, data: unknown | null, diags: Diagnostic[], _absFile: string): never {
    phaseTiming.eval = phaseTiming.eval ?? 0;
    const totalMs = Date.now() - startTime;
    console.log(JSON.stringify(makeEnvelope(ok, data, diags, { total_ms: totalMs, phases: phaseTiming })));
    process.exit(ok ? 0 : 1);
  }

  function resolveModulesForVM(program: Program, baseDir: string, packageModuleMap?: Map<string, string>): Program {
    const loaded = new Set<string>();
    const moduleTopLevels: TopLevel[] = [];

    function processUseDecls(prog: Program, dir: string): void {
      for (const tl of prog.topLevels) {
        if (tl.tag !== "use-decl") continue;
        const absPath = resolveModulePath(tl.path, dir, packageModuleMap);
        if (loaded.has(absPath)) continue;
        loaded.add(absPath);
        const modProg = loadModuleAST(absPath);
        const modDir = dirname(absPath);
        processUseDecls(modProg, modDir);

        const pubNames = new Set<string>();
        for (const mtl of modProg.topLevels) {
          if (mtl.tag === "definition" && mtl.pub) pubNames.add(mtl.name);
          if (mtl.tag === "type-decl" && mtl.pub) {
            for (const v of mtl.variants) pubNames.add(v.name);
          }
          if (mtl.tag === "effect-decl" && mtl.pub) {
            pubNames.add(mtl.name);
            for (const op of mtl.ops) pubNames.add(op.name);
          }
          if (mtl.tag === "effect-alias" && mtl.pub) pubNames.add(mtl.name);
        }

        for (const imp of tl.imports) {
          if (!pubNames.has(imp.name)) {
            const diag = toDiagnostic({
              code: "E223",
              message: `'${imp.name}' is not exported by module '${tl.path.join(".")}'`,
              location: { line: 0, col: 0 },
            }, absFile);
            if (jsonMode) {
              diagnostics.push(diag);
              emitJsonAndExit(startTime, phaseTiming, false, null, diagnostics, absFile);
            } else {
              console.error(JSON.stringify({
                code: "E223",
                message: `'${imp.name}' is not exported by module '${tl.path.join(".")}'`,
              }));
              process.exit(1);
            }
          }
        }

        for (const mtl of modProg.topLevels) {
          if (mtl.tag === "definition" && mtl.pub) moduleTopLevels.push(mtl);
          else if (mtl.tag === "type-decl" && mtl.pub) moduleTopLevels.push(mtl);
          else if (mtl.tag === "effect-decl" && mtl.pub) moduleTopLevels.push(mtl);
          else if (mtl.tag === "effect-alias" && mtl.pub) moduleTopLevels.push(mtl);
        }
      }
    }

    processUseDecls(program, baseDir);
    return { topLevels: [...moduleTopLevels, ...program.topLevels] };
  }
}

// ── Shared helpers ──

function isRuntimeError(e: unknown): e is { code: string; message: string; location: { line: number; col: number } } {
  return typeof e === "object" && e !== null && "code" in e && "message" in e && "location" in e;
}

function resolveModulePath(modPath: string[], baseDir: string, packageModuleMap?: Map<string, string>): string {
  // Check package module map first (for cross-package imports)
  if (packageModuleMap) {
    const qualifiedName = modPath.join(".");
    const pkgPath = packageModuleMap.get(qualifiedName);
    if (pkgPath) return resolve(pkgPath);
  }

  const dirBased = join(baseDir, ...modPath.slice(0, -1), modPath[modPath.length - 1] + ".clk");
  if (existsSync(dirBased)) return resolve(dirBased);
  const flatBased = join(baseDir, modPath.join("-") + ".clk");
  if (existsSync(flatBased)) return resolve(flatBased);
  throw new Error(`module '${modPath.join(".")}' not found`);
}

function loadModuleAST(absPath: string): Program {
  const src = readFileSync(absPath, "utf-8");
  const toks = lex(src);
  if (!Array.isArray(toks)) throw new Error(`lex error in module '${absPath}'`);
  const modAst = parse(toks);
  if ("code" in modAst) throw new Error(`parse error in module '${absPath}'`);
  return {
    topLevels: (modAst as Program).topLevels.map(tl => {
      if (tl.tag === "definition") return { ...tl, body: desugar(tl.body) };
      if (tl.tag === "impl-block") return { ...tl, methods: tl.methods.map(m => ({ ...m, body: desugar(m.body) })) };
      return tl;
    }),
  };
}
