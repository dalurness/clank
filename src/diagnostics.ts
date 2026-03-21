// Clank structured diagnostic types per tooling spec (plan/features/tooling-spec.md §3)

import type { Loc } from "./ast.js";

// ── Diagnostic schema (§3.1) ──

export type Severity = "error" | "warning" | "info";
export type Phase = "lex" | "parse" | "desugar" | "check" | "eval" | "link";

export type DiagnosticLocation = {
  file: string;
  line: number;
  col: number;
  end_line?: number;
  end_col?: number;
};

export type Diagnostic = {
  severity: Severity;
  code: string;
  phase: Phase;
  message: string;
  location: DiagnosticLocation;
  context?: string;
  related?: { message: string; location: DiagnosticLocation }[];
  fix?: {
    description: string;
    edits: { file: string; line: number; col: number; end_col: number; replacement: string }[];
  };
};

// ── Command output envelope (§3.4) ──

export type CommandOutput = {
  ok: boolean;
  data: unknown | null;
  diagnostics: Diagnostic[];
  timing: {
    total_ms: number;
    phases: Record<string, number>;
  };
};

// ── Helpers to convert internal error types to diagnostics ──

function phaseFromCode(code: string): Phase {
  const num = parseInt(code.slice(1));
  if (code.startsWith("E")) {
    if (num < 100) return "lex";       // E001: lexer errors
    if (num < 200) return "parse";     // E100: parse errors
    if (num < 300) return "eval";      // E200-E223: runtime/module errors
    if (num < 400) return "check";     // E300-E307: type errors
    if (num < 500) return "check";     // E400-E499: effect errors
    if (num < 600) return "link";      // E500-E599: module/import errors
    return "check";                    // E600+: ownership/refinement errors
  }
  if (code.startsWith("W")) return "check"; // W400-W401: warnings from type checker
  return "eval";
}

function severityFromCode(code: string): Severity {
  if (code.startsWith("W")) return "warning";
  return "error";
}

export function toDiagnostic(
  err: { code: string; message: string; location: Loc; context?: string },
  file: string,
): Diagnostic {
  return {
    severity: severityFromCode(err.code),
    code: err.code,
    phase: phaseFromCode(err.code),
    message: err.message,
    location: {
      file,
      line: err.location.line,
      col: err.location.col,
      ...(err.location.end_line !== undefined ? { end_line: err.location.end_line } : {}),
      ...(err.location.end_col !== undefined ? { end_col: err.location.end_col } : {}),
    },
    ...(err.context ? { context: err.context } : {}),
  };
}

export function makeEnvelope(
  ok: boolean,
  data: unknown | null,
  diagnostics: Diagnostic[],
  timing: { total_ms: number; phases: Record<string, number> },
): CommandOutput {
  return { ok, data, diagnostics, timing };
}
