// Clank formatter — pretty-prints AST back to canonical source
// Enforces: 2-space indent, blank lines between top-level defs,
// sorted imports, one-per-line params if >80 chars, aligned match arms.

import type {
  DoStep, EffectRef, Expr, HandlerArm, ImportItem, MatchArm, Param, Pattern,
  Program, TopLevel, TypeExpr, TypeSig, Variant, OpSig,
} from "./ast.js";

function formatEffectRef(ref: EffectRef): string {
  return ref.args.length > 0 ? `${ref.name}<${ref.args.join(", ")}>` : ref.name;
}

// ── Comment extraction ──

export type Comment = { line: number; text: string };

/** Extract comments from source, preserving their line numbers. */
export function extractComments(source: string): Comment[] {
  const comments: Comment[] = [];
  const lines = source.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const trimmed = lines[i].trimStart();
    if (trimmed.startsWith("#")) {
      comments.push({ line: i + 1, text: lines[i] });
    }
  }
  return comments;
}

// ── Main formatter entry point ──

export function format(program: Program, source: string): string {
  const comments = extractComments(source);
  const sourceLines = source.split("\n");
  const out: string[] = [];

  const topLevels = program.topLevels;
  let commentIdx = 0;

  for (let i = 0; i < topLevels.length; i++) {
    const tl = topLevels[i];
    const tlLine = tl.loc.line;

    // Collect comments that belong before this top-level
    const preComments: Comment[] = [];
    while (commentIdx < comments.length && comments[commentIdx].line < tlLine) {
      preComments.push(comments[commentIdx]);
      commentIdx++;
    }

    if (i > 0) {
      // Always insert blank line between top-level groups
      out.push("");
    }

    // Emit pre-comments with internal blank lines preserved
    for (let c = 0; c < preComments.length; c++) {
      if (c > 0) {
        // Check if there were blank lines between consecutive comments
        const prevLine = preComments[c - 1].line;
        const curLine = preComments[c].line;
        if (curLine - prevLine > 1) {
          // There was at least one blank line between these comments
          out.push("");
        }
      }
      out.push(preComments[c].text);
    }

    // If there are pre-comments and a gap to the definition, add blank line
    if (preComments.length > 0) {
      const lastCommentLine = preComments[preComments.length - 1].line;
      if (tlLine - lastCommentLine > 1) {
        // Blank line between last comment and the definition
        out.push("");
      }
    }

    out.push(formatTopLevel(tl));
  }

  // Trailing comments after all top-levels
  if (commentIdx < comments.length) {
    out.push("");
    while (commentIdx < comments.length) {
      out.push(comments[commentIdx].text);
      commentIdx++;
    }
  }

  // Ensure trailing newline
  return out.join("\n") + "\n";
}

// ── Top-level formatters ──

function formatTopLevel(tl: TopLevel): string {
  switch (tl.tag) {
    case "mod-decl":
      return `mod ${tl.path.join(".")}`;
    case "use-decl":
      return formatUseDecl(tl);
    case "definition":
      return formatDefinition(tl);
    case "type-decl":
      return formatTypeDecl(tl);
    case "effect-decl":
      return formatEffectDecl(tl);
    case "effect-alias":
      return formatEffectAlias(tl);
    case "test-decl":
      return formatTestDecl(tl);
  }
}

function formatUseDecl(tl: Extract<TopLevel, { tag: "use-decl" }>): string {
  const path = tl.path.join(".");
  // Sort imports alphabetically
  const sorted = [...tl.imports].sort((a, b) => a.name.localeCompare(b.name));
  const imports = sorted.map(formatImportItem).join(", ");
  return `use ${path} (${imports})`;
}

function formatImportItem(item: ImportItem): string {
  if (item.alias) return `${item.name} as ${item.alias}`;
  return item.name;
}

function formatDefinition(tl: Extract<TopLevel, { tag: "definition" }>): string {
  const pub = tl.pub ? "pub " : "";
  const sig = formatTypeSig(tl.sig);
  const header = `${pub}${tl.name} : ${sig} =`;

  // Special case: body is a do-block — format as `= do {`
  if (tl.body.tag === "do") {
    const doStr = formatDo(tl.body, 2);
    // Put do { on same line if short enough
    const doInline = formatDo(tl.body, 0);
    if (!doInline.includes("\n") && header.length + 1 + doInline.length <= 80) {
      return `${header} ${doInline}`;
    }
    return `${header} ${doStr.trimStart()}`;
  }

  const body = formatExprBody(tl.body, 2, true);
  return `${header}\n${body}`;
}

function formatTestDecl(tl: Extract<TopLevel, { tag: "test-decl" }>): string {
  const header = `test "${tl.name}" =`;
  const body = formatExprBody(tl.body, 2, true);
  return `${header}\n${body}`;
}

function formatTypeDecl(tl: Extract<TopLevel, { tag: "type-decl" }>): string {
  const pub = tl.pub ? "pub " : "";
  const typeParams = tl.typeParams.length > 0 ? `<${tl.typeParams.join(", ")}>` : "";
  const variants = tl.variants.map(formatVariant);

  // Try single-line: type Name = V1(T) | V2(T)
  const singleLine = `${pub}type ${tl.name}${typeParams} = ${variants.join(" | ")}`;
  if (singleLine.length <= 80) {
    return singleLine;
  }

  // Multi-line
  const lines = [`${pub}type ${tl.name}${typeParams}`];
  for (let i = 0; i < variants.length; i++) {
    const prefix = i === 0 ? "  = " : "  | ";
    lines.push(`${prefix}${variants[i]}`);
  }
  return lines.join("\n");
}

function formatVariant(v: Variant): string {
  if (v.fields.length === 0) return v.name;
  return `${v.name}(${v.fields.map(formatTypeExpr).join(", ")})`;
}

function formatEffectDecl(tl: Extract<TopLevel, { tag: "effect-decl" }>): string {
  const pub = tl.pub ? "pub " : "";
  if (tl.ops.length === 0) {
    return `${pub}effect ${tl.name} {}`;
  }
  const lines = [`${pub}effect ${tl.name} {`];
  for (let i = 0; i < tl.ops.length; i++) {
    const op = tl.ops[i];
    const comma = i < tl.ops.length - 1 ? "," : "";
    lines.push(`  ${formatOpSig(op)}${comma}`);
  }
  lines.push("}");
  return lines.join("\n");
}

function formatEffectAlias(tl: Extract<TopLevel, { tag: "effect-alias" }>): string {
  const pub = tl.pub ? "pub " : "";
  const params = tl.params.length > 0 ? `<${tl.params.join(", ")}>` : "";
  let effects = tl.effects.map(formatEffectRef).join(", ");
  if (tl.subtracted.length > 0) {
    effects += ` \\ ${tl.subtracted.map(formatEffectRef).join(", ")}`;
  }
  return `${pub}effect alias ${tl.name}${params} = <${effects}>`;
}

function formatOpSig(op: OpSig): string {
  // Effect operations are stored as TypeSig but originally written as: name : ParamType -> ReturnType
  const { sig } = op;
  if (sig.params.length === 0) {
    return `${op.name} : () -> ${formatTypeExpr(sig.returnType)}`;
  }
  // Single param (the normalized form)
  const paramType = sig.params[0].type;
  return `${op.name} : ${formatTypeExpr(paramType)} -> ${formatTypeExpr(sig.returnType)}`;
}

// ── Type signature ──

function formatTypeSig(sig: TypeSig): string {
  const params = sig.params.map(p => `${p.name}: ${formatTypeExprAsParam(p.type)}`);
  let effects = sig.effects.length > 0 ? sig.effects.map(formatEffectRef).join(", ") : "";
  if (sig.subtracted.length > 0) {
    effects += ` \\ ${sig.subtracted.map(formatEffectRef).join(", ")}`;
  }
  const ret = formatTypeExpr(sig.returnType);

  // Try single-line first
  const singleLine = `(${params.join(", ")}) -> <${effects}> ${ret}`;
  if (singleLine.length <= 80) return singleLine;

  // Multi-line params: one per line
  const paramLines = params.map(p => `  ${p},`);
  // Remove trailing comma from last param
  if (paramLines.length > 0) {
    paramLines[paramLines.length - 1] = paramLines[paramLines.length - 1].slice(0, -1);
  }
  return `(\n${paramLines.join("\n")}\n) -> <${effects}> ${ret}`;
}

// ── Type expressions ──

/** Wrap function types in parens when used as a parameter type. */
function formatTypeExprAsParam(t: TypeExpr): string {
  if (t.tag === "t-fn") return `(${formatTypeExpr(t)})`;
  return formatTypeExpr(t);
}

function formatTypeExpr(t: TypeExpr): string {
  switch (t.tag) {
    case "t-name":
      return t.name;
    case "t-list":
      return `[${formatTypeExpr(t.element)}]`;
    case "t-tuple":
      if (t.elements.length === 0) return "()";
      return `(${t.elements.map(formatTypeExpr).join(", ")})`;
    case "t-record":
      return `{${t.fields.map(f => `${f.name}: ${formatTypeExpr(f.type)}`).join(", ")}}`;
    case "t-union":
      return `${formatTypeExpr(t.left)} | ${formatTypeExpr(t.right)}`;
    case "t-fn": {
      const param = formatTypeExpr(t.param);
      const result = formatTypeExpr(t.result);
      if (t.effects.length > 0) {
        let effs = t.effects.map(formatEffectRef).join(", ");
        if (t.subtracted.length > 0) {
          effs += ` \\ ${t.subtracted.map(formatEffectRef).join(", ")}`;
        }
        return `${param} -> <${effs}> ${result}`;
      }
      return `${param} -> ${result}`;
    }
    case "t-generic":
      return `${t.name}<${t.args.map(formatTypeExpr).join(", ")}>`;
    case "t-refined":
      return `${formatTypeExpr(t.base)} where ${t.predicate}`;
  }
}

// ── Expression formatting ──

function formatExpr(expr: Expr, indent: number): string {
  const pad = " ".repeat(indent);
  switch (expr.tag) {
    case "literal":
      return pad + formatLiteral(expr);
    case "var":
      return pad + expr.name;
    case "let":
      return formatLet(expr, indent, false);
    case "if":
      return formatIf(expr, indent);
    case "match":
      return formatMatch(expr, indent);
    case "lambda":
      return formatLambda(expr, indent);
    case "apply":
      return formatApply(expr, indent);
    case "pipeline":
      return formatPipeline(expr, indent);
    case "infix":
      return formatInfix(expr, indent);
    case "unary":
      return pad + `${expr.op}${formatExprInline(expr.operand)}`;
    case "do":
      return formatDo(expr, indent);
    case "for":
      return formatFor(expr, indent);
    case "handle":
      return formatHandle(expr, indent);
    case "perform":
      return pad + `perform ${formatExprInline(expr.expr)}`;
    case "list":
      return formatList(expr, indent);
    case "tuple":
      return pad + `(${expr.elements.map(formatExprInline).join(", ")})`;
    case "record":
      return formatRecord(expr, indent);
    case "record-update":
      return formatRecordUpdate(expr, indent);
    case "field-access":
      return pad + `${formatExprInline(expr.object)}.${expr.field}`;
    case "range":
      return pad + `${formatExprInline(expr.start)}${expr.inclusive ? "..=" : ".."}${formatExprInline(expr.end)}`;
  }
}

/** Format an expression without leading indentation (inline context). */
function formatExprInline(expr: Expr): string {
  return formatExpr(expr, 0);
}

function formatLiteral(expr: Extract<Expr, { tag: "literal" }>): string {
  const lit = expr.value;
  switch (lit.tag) {
    case "int": return String(lit.value);
    case "rat": {
      const s = String(lit.value);
      return s.includes(".") ? s : s + ".0";
    }
    case "bool": return lit.value ? "true" : "false";
    case "str": return `"${escapeStr(lit.value)}"`;
    case "unit": return "()";
  }
}

function escapeStr(s: string): string {
  return s
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\t/g, "\\t");
}

function formatLet(expr: Extract<Expr, { tag: "let" }>, indent: number, isTopBody: boolean = false): string {
  const pad = " ".repeat(indent);

  // Implicit sequence: let _ = expr in body (desugared bare expression sequencing)
  if (expr.name === "_" && expr.body !== null) {
    const valStr = formatExprInline(expr.value);
    const bodyStr = formatExprBody(expr.body, indent, isTopBody);
    return `${pad}${valStr}\n${bodyStr}`;
  }

  if (expr.body === null) {
    // Standalone let (no in)
    const valStr = formatExprAtIndent(expr.value, indent + 2);
    return `${pad}let ${expr.name} = ${valStr}`;
  }

  // Check if this is a "top-level body" let (implicit sequencing, no in keyword)
  // vs an inline let-in expression.
  if (isTopBody) {
    const valStr = formatExprAtIndent(expr.value, indent + 2);
    const bodyStr = formatExprBody(expr.body, indent, true);
    return `${pad}let ${expr.name} = ${valStr}\n${bodyStr}`;
  }

  // Inline let-in: let x = val in body
  const valStr = formatExprInline(expr.value);
  const bodyStr = formatExprInline(expr.body);
  return `${pad}let ${expr.name} = ${valStr} in ${bodyStr}`;
}

/** Format expression inline if simple, or with indentation if multi-line. */
function formatExprAtIndent(expr: Expr, indent: number): string {
  // Try inline first
  const inline = formatExprInline(expr);
  if (!inline.includes("\n")) return inline;
  // Multi-line: format with proper indent, then strip leading whitespace
  // since the caller already handles positioning
  const full = formatExpr(expr, indent);
  return full.trimStart();
}

/** Format an expression that's part of a definition body (with implicit sequencing). */
function formatExprBody(expr: Expr, indent: number, isTopBody: boolean): string {
  if (expr.tag === "let") {
    return formatLet(expr, indent, isTopBody);
  }
  return formatExpr(expr, indent);
}

function formatIf(expr: Extract<Expr, { tag: "if" }>, indent: number): string {
  const pad = " ".repeat(indent);
  const cond = formatExprInline(expr.cond);
  const then_ = formatExprInline(expr.then);

  // Check if else branch is an if-else chain
  if (expr.else.tag === "if") {
    // Format as: if ... then ...\n  else if ... then ...\n  else ...
    const elseStr = formatIf(expr.else as Extract<Expr, { tag: "if" }>, 0);
    const singleLine = `${pad}if ${cond} then ${then_}\n${pad}else ${elseStr}`;
    return singleLine;
  }

  const else_ = formatExprInline(expr.else);

  // Try single-line
  const singleLine = `${pad}if ${cond} then ${then_} else ${else_}`;
  if (singleLine.length <= 80 && !then_.includes("\n") && !else_.includes("\n")) {
    return singleLine;
  }

  // Multi-line
  return `${pad}if ${cond} then ${then_}\n${pad}else ${else_}`;
}

function formatMatch(expr: Extract<Expr, { tag: "match" }>, indent: number): string {
  const pad = " ".repeat(indent);
  const subject = formatExprInline(expr.subject);
  const lines = [`${pad}match ${subject} {`];

  // Format arms, aligning => arrows
  const armTexts = expr.arms.map(arm => ({
    pattern: formatPattern(arm.pattern),
    body: formatExprInline(arm.body),
  }));

  // Find max pattern length for alignment
  const maxPatLen = Math.max(...armTexts.map(a => a.pattern.length));

  for (const arm of armTexts) {
    const padded = arm.pattern.padEnd(maxPatLen);
    const armLine = `${pad}  ${padded} => ${arm.body}`;
    lines.push(armLine);
  }

  lines.push(`${pad}}`);
  return lines.join("\n");
}

function formatLambda(expr: Extract<Expr, { tag: "lambda" }>, indent: number): string {
  const pad = " ".repeat(indent);
  const params = expr.params.map(formatParam).join(", ");
  const body = formatExprInline(expr.body);
  return `${pad}fn(${params}) => ${body}`;
}

function formatParam(p: Param): string {
  if (p.type) return `${p.name}: ${formatTypeExpr(p.type)}`;
  return p.name;
}

function formatApply(expr: Extract<Expr, { tag: "apply" }>, indent: number): string {
  const pad = " ".repeat(indent);
  const fn = formatExprInline(expr.fn);
  const args = expr.args.map(formatExprInline).join(", ");
  return `${pad}${fn}(${args})`;
}

function formatPipeline(expr: Extract<Expr, { tag: "pipeline" }>, indent: number): string {
  const pad = " ".repeat(indent);
  // Collect pipeline chain
  const parts: string[] = [];
  let current: Expr = expr;
  while (current.tag === "pipeline") {
    parts.push(formatExprInline(current.right));
    current = current.left;
  }
  parts.push(formatExprInline(current));
  parts.reverse();

  const singleLine = `${pad}${parts.join(" |> ")}`;
  if (singleLine.length <= 80) return singleLine;

  // Multi-line pipeline
  const lines = [pad + parts[0]];
  for (let i = 1; i < parts.length; i++) {
    lines.push(`${pad}  |> ${parts[i]}`);
  }
  return lines.join("\n");
}

function formatInfix(expr: Extract<Expr, { tag: "infix" }>, indent: number): string {
  const pad = " ".repeat(indent);
  const left = formatInfixOperand(expr.left, expr.op, "left");
  const right = formatInfixOperand(expr.right, expr.op, "right");
  return `${pad}${left} ${expr.op} ${right}`;
}

const PRECEDENCE: Record<string, number> = {
  "||": 1,
  "&&": 2,
  "==": 3, "!=": 3, "<": 3, ">": 3, "<=": 3, ">=": 3,
  "++": 4,
  "+": 5, "-": 5,
  "*": 6, "/": 6, "%": 6,
};

function formatInfixOperand(expr: Expr, parentOp: string, side: "left" | "right"): string {
  if (expr.tag === "infix") {
    const childPrec = PRECEDENCE[expr.op] ?? 0;
    const parentPrec = PRECEDENCE[parentOp] ?? 0;
    // Need parens if child has lower precedence
    // Also need parens for same-precedence on the wrong side for non-associative ops
    const needParens = childPrec < parentPrec ||
      // Right-side same-precedence for left-associative ops
      (childPrec === parentPrec && side === "right" && !isRightAssoc(parentOp)) ||
      // Left-side same-precedence for right-associative ops
      (childPrec === parentPrec && side === "left" && isRightAssoc(parentOp) && expr.op !== parentOp);
    if (needParens) {
      return `(${formatExprInline(expr)})`;
    }
  }
  return formatExprInline(expr);
}

function isRightAssoc(op: string): boolean {
  return op === "++";
}

function formatDo(expr: Extract<Expr, { tag: "do" }>, indent: number): string {
  const pad = " ".repeat(indent);
  const lines = [`${pad}do {`];
  for (const step of expr.steps) {
    lines.push(formatDoStep(step, indent + 2));
  }
  lines.push(`${pad}}`);
  return lines.join("\n");
}

function formatDoStep(step: DoStep, indent: number): string {
  const pad = " ".repeat(indent);
  if (step.bind) {
    return `${pad}${step.bind} <- ${formatExprInline(step.expr)}`;
  }
  return formatExpr(step.expr, indent);
}

function formatFor(expr: Extract<Expr, { tag: "for" }>, indent: number): string {
  const pad = " ".repeat(indent);
  let result = `${pad}for ${formatPatternInline(expr.bind)} in ${formatExprInline(expr.collection)}`;
  if (expr.guard) {
    result += ` if ${formatExprInline(expr.guard)}`;
  }
  if (expr.fold) {
    result += ` fold ${expr.fold.acc} = ${formatExprInline(expr.fold.init)}`;
  }
  result += ` do ${formatExprInline(expr.body)}`;
  return result;
}

function formatPatternInline(pat: import("./ast.js").Pattern): string {
  switch (pat.tag) {
    case "p-var": return pat.name;
    case "p-wildcard": return "_";
    case "p-literal": {
      const lit = pat.value;
      switch (lit.tag) {
        case "int": return String(lit.value);
        case "rat": return String(lit.value);
        case "bool": return String(lit.value);
        case "str": return `"${lit.value}"`;
        case "unit": return "()";
      }
    }
    case "p-tuple": return `(${pat.elements.map(formatPatternInline).join(", ")})`;
    case "p-variant": return pat.args.length > 0
      ? `${pat.name}(${pat.args.map(formatPatternInline).join(", ")})`
      : pat.name;
  }
}

function formatHandle(expr: Extract<Expr, { tag: "handle" }>, indent: number): string {
  const pad = " ".repeat(indent);
  // Format the subject expression
  let subjectStr: string;
  if (expr.expr.tag === "handle") {
    // Nested handle — format with proper indentation, wrap in parens
    const inner = formatHandle(expr.expr as Extract<Expr, { tag: "handle" }>, indent);
    subjectStr = `(${inner.trimStart()})`;
  } else {
    subjectStr = formatExprInline(expr.expr);
    if (needsParens(expr.expr)) {
      subjectStr = `(${subjectStr})`;
    }
  }
  const lines = [`${pad}handle ${subjectStr} {`];
  for (let i = 0; i < expr.arms.length; i++) {
    const arm = expr.arms[i];
    const comma = i < expr.arms.length - 1 ? "," : "";
    if (arm.name === "return") {
      const paramName = arm.params[0]?.name ?? "_";
      const body = formatExprInline(arm.body);
      lines.push(`${pad}  return ${paramName} -> ${body}${comma}`);
    } else {
      const params = arm.params.map(p => p.name).join(" ");
      const resume = arm.resumeName ? ` resume ${arm.resumeName}` : "";
      const body = formatExprInline(arm.body);
      lines.push(`${pad}  ${arm.name} ${params}${resume} -> ${body}${comma}`);
    }
  }
  lines.push(`${pad}}`);
  return lines.join("\n");
}

function formatList(expr: Extract<Expr, { tag: "list" }>, indent: number): string {
  const pad = " ".repeat(indent);
  if (expr.elements.length === 0) return `${pad}[]`;
  const items = expr.elements.map(formatExprInline).join(", ");
  const singleLine = `${pad}[${items}]`;
  if (singleLine.length <= 80) return singleLine;

  // Multi-line
  const lines = [`${pad}[`];
  for (let i = 0; i < expr.elements.length; i++) {
    const comma = i < expr.elements.length - 1 ? "," : "";
    lines.push(`${pad}  ${formatExprInline(expr.elements[i])}${comma}`);
  }
  lines.push(`${pad}]`);
  return lines.join("\n");
}

function formatRecord(expr: Extract<Expr, { tag: "record" }>, indent: number): string {
  const pad = " ".repeat(indent);
  if (expr.fields.length === 0) return `${pad}{}`;
  const items = expr.fields.map(f => `${f.name}: ${formatExprInline(f.value)}`).join(", ");
  return `${pad}{${items}}`;
}

function formatRecordUpdate(expr: Extract<Expr, { tag: "record-update" }>, indent: number): string {
  const pad = " ".repeat(indent);
  const base = formatExprInline(expr.base);
  const fields = expr.fields.map(f => `${f.name}: ${formatExprInline(f.value)}`).join(", ");
  return `${pad}{${base} | ${fields}}`;
}

/** Check if an expression needs parens when used as a handle/perform subject. */
function needsParens(expr: Expr): boolean {
  return expr.tag === "perform" || expr.tag === "handle" || expr.tag === "if" ||
         expr.tag === "match" || expr.tag === "let" || expr.tag === "do";
}

// ── Pattern formatting ──

function formatPattern(p: Pattern): string {
  switch (p.tag) {
    case "p-var":
      return p.name;
    case "p-wildcard":
      return "_";
    case "p-literal": {
      const lit = p.value;
      switch (lit.tag) {
        case "int": return String(lit.value);
        case "rat": {
          const s = String(lit.value);
          return s.includes(".") ? s : s + ".0";
        }
        case "bool": return lit.value ? "true" : "false";
        case "str": return `"${escapeStr(lit.value)}"`;
        case "unit": return "()";
      }
    }
    // falls through (unreachable but satisfies TS)
    case "p-variant":
      if (p.args.length === 0) return p.name;
      return `${p.name}(${p.args.map(formatPattern).join(", ")})`;
    case "p-tuple":
      return `(${p.elements.map(formatPattern).join(", ")})`;
  }
}
