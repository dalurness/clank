// Clank desugaring pass
// AST-to-AST transform that eliminates syntactic sugar, producing a Core AST
// where the evaluator only handles: literal, var, let, if, apply, lambda, match
//
// Transforms:
//   pipeline:  x |> f(y)      → f(x, y)
//   infix:     a + b          → add(a, b)
//   infix:     a ++ b         → str.cat(a, b)
//   unary:     !x             → not(x)
//   unary:     -x             → negate(x)
//   do-block:  do { x <- e1; e2 } → let x = e1 in e2

import type { Expr, DoStep, MatchArm, HandlerArm, Loc, Pattern, Param } from "./ast.ts";

// Operator-to-function mapping for infix desugaring
const INFIX_OPS: Record<string, string> = {
  "+": "add",
  "-": "sub",
  "*": "mul",
  "/": "div",
  "%": "mod",
  "==": "eq",
  "!=": "neq",
  "<": "lt",
  ">": "gt",
  "<=": "lte",
  ">=": "gte",
  "&&": "and",
  "||": "or",
  "++": "str.cat",
};

const UNARY_OPS: Record<string, string> = {
  "!": "not",
  "-": "negate",
};

export function desugar(expr: Expr): Expr {
  switch (expr.tag) {
    // Core nodes — recurse into children
    case "literal":
      return expr;

    case "var":
      return expr;

    case "let":
      return {
        tag: "let",
        name: expr.name,
        value: desugar(expr.value),
        body: expr.body ? desugar(expr.body) : null,
        loc: expr.loc,
      };

    case "if":
      return {
        tag: "if",
        cond: desugar(expr.cond),
        then: desugar(expr.then),
        else: desugar(expr.else),
        loc: expr.loc,
      };

    case "match":
      return {
        tag: "match",
        subject: desugar(expr.subject),
        arms: expr.arms.map((a: MatchArm) => ({
          pattern: a.pattern,
          body: desugar(a.body),
        })),
        loc: expr.loc,
      };

    case "lambda":
      return {
        tag: "lambda",
        params: expr.params,
        body: desugar(expr.body),
        loc: expr.loc,
      };

    case "apply":
      return {
        tag: "apply",
        fn: desugar(expr.fn),
        args: expr.args.map(desugar),
        loc: expr.loc,
      };

    // Sugar — transform away

    case "pipeline": {
      // x |> f(args...) → f(x, args...)
      // x |> f          → f(x)
      const left = desugar(expr.left);
      const right = desugar(expr.right);
      if (right.tag === "apply") {
        return {
          tag: "apply",
          fn: right.fn,
          args: [left, ...right.args],
          loc: expr.loc,
        };
      }
      // right is a plain function reference: x |> f → f(x)
      return { tag: "apply", fn: right, args: [left], loc: expr.loc };
    }

    case "infix": {
      // Short-circuit: a && b → if a then b else false
      if (expr.op === "&&") {
        return {
          tag: "if",
          cond: desugar(expr.left),
          then: desugar(expr.right),
          else: { tag: "literal", value: { tag: "bool", value: false }, loc: expr.loc },
          loc: expr.loc,
        };
      }
      // Short-circuit: a || b → if a then true else b
      if (expr.op === "||") {
        return {
          tag: "if",
          cond: desugar(expr.left),
          then: { tag: "literal", value: { tag: "bool", value: true }, loc: expr.loc },
          else: desugar(expr.right),
          loc: expr.loc,
        };
      }
      const fn = INFIX_OPS[expr.op] ?? expr.op;
      return {
        tag: "apply",
        fn: { tag: "var", name: fn, loc: expr.loc },
        args: [desugar(expr.left), desugar(expr.right)],
        loc: expr.loc,
      };
    }

    case "unary": {
      const fn = UNARY_OPS[expr.op] ?? expr.op;
      return {
        tag: "apply",
        fn: { tag: "var", name: fn, loc: expr.loc },
        args: [desugar(expr.operand)],
        loc: expr.loc,
      };
    }

    case "do":
      return desugarDo(expr.steps, expr.loc);

    case "for":
      return desugarFor(expr);

    case "range": {
      // Builtin range(start, end) is inclusive [start, end].
      // start..end   (half-open) → range(start, sub(end, 1))
      // start..=end  (inclusive) → range(start, end)
      const start = desugar(expr.start);
      const end = desugar(expr.end);
      const rangeFn: Expr = { tag: "var", name: "range", loc: expr.loc };
      const endArg = expr.inclusive
        ? end
        : {
            tag: "apply" as const,
            fn: { tag: "var" as const, name: "sub", loc: expr.loc },
            args: [end, { tag: "literal" as const, value: { tag: "int" as const, value: 1 }, loc: expr.loc }],
            loc: expr.loc,
          };
      return {
        tag: "apply",
        fn: rangeFn,
        args: [start, endArg],
        loc: expr.loc,
      };
    }

    // Pass-through nodes — recurse but keep the tag
    case "handle":
      return {
        tag: "handle",
        expr: desugar(expr.expr),
        arms: expr.arms.map((a: HandlerArm) => ({
          name: a.name,
          params: a.params,
          resumeName: a.resumeName,
          body: desugar(a.body),
        })),
        loc: expr.loc,
      };

    case "perform":
      return {
        tag: "perform",
        expr: desugar(expr.expr),
        loc: expr.loc,
      };

    case "list":
      return {
        tag: "list",
        elements: expr.elements.map(desugar),
        loc: expr.loc,
      };

    case "tuple":
      return {
        tag: "tuple",
        elements: expr.elements.map(desugar),
        loc: expr.loc,
      };

    case "record":
      return {
        tag: "record",
        fields: expr.fields.map((f: { name: string; value: Expr }) => ({
          name: f.name,
          value: desugar(f.value),
        })),
        loc: expr.loc,
      };

    case "record-update":
      return {
        tag: "record-update",
        base: desugar(expr.base),
        fields: expr.fields.map((f: { name: string; value: Expr }) => ({
          name: f.name,
          value: desugar(f.value),
        })),
        loc: expr.loc,
      };

    case "field-access":
      return {
        tag: "field-access",
        object: desugar(expr.object),
        field: expr.field,
        loc: expr.loc,
      };

    case "borrow":
      return { tag: "borrow", expr: desugar(expr.expr), loc: expr.loc };
    case "clone":
      return { tag: "clone", expr: desugar(expr.expr), loc: expr.loc };
    case "discard":
      return { tag: "discard", expr: desugar(expr.expr), loc: expr.loc };

    default: {
      const _exhaustive: never = expr;
      throw new Error(`Unknown AST node: ${(expr as any).tag}`);
    }
  }
}

/** Flatten do-block steps into nested let expressions */
function desugarDo(steps: DoStep[], loc: Loc): Expr {
  if (steps.length === 0) {
    return { tag: "literal", value: { tag: "unit" }, loc };
  }
  if (steps.length === 1) {
    return desugar(steps[0].expr);
  }
  const [head, ...tail] = steps;
  const rest = desugarDo(tail, loc);
  if (head.bind) {
    return {
      tag: "let",
      name: head.bind,
      value: desugar(head.expr),
      body: rest,
      loc,
    };
  }
  // No binding — sequence: just use a let with a throwaway name
  return {
    tag: "let",
    name: "_",
    value: desugar(head.expr),
    body: rest,
    loc,
  };
}

/** Convert a pattern to lambda params */
function patternToParams(pat: Pattern): Param[] {
  switch (pat.tag) {
    case "p-var":
      return [{ name: pat.name, type: null }];
    case "p-tuple":
      // For tuple destructuring, use a single param name and let match handle it
      // Actually, since lambdas don't support pattern params, we need a wrapper.
      // For simple var patterns, use directly. For complex patterns, use a match.
      return [{ name: "__for_elem", type: null }];
    default:
      return [{ name: "__for_elem", type: null }];
  }
}

/** Wrap body in a match if the pattern is not a simple variable */
function wrapBodyWithPattern(pat: Pattern, body: Expr, loc: Loc): Expr {
  if (pat.tag === "p-var") return body;
  // match __for_elem { pattern => body }
  return {
    tag: "match",
    subject: { tag: "var", name: "__for_elem", loc },
    arms: [{ pattern: pat, body }],
    loc,
  };
}

/**
 * Desugar for-expressions:
 *   for P in E do B            → map(E, fn(P) => B)
 *   for P in E if G do B       → map(filter(E, fn(P) => G), fn(P) => B)
 *   for P in E fold A = I do B → fold(E, I, fn(A, P) => B)
 *   for P in E if G fold A = I do B → fold(filter(E, fn(P) => G), I, fn(A, P) => B)
 */
function desugarFor(expr: Extract<Expr, { tag: "for" }>): Expr {
  const loc = expr.loc;
  const collection = desugar(expr.collection);
  const body = desugar(expr.body);
  const guard = expr.guard ? desugar(expr.guard) : null;

  const elemParams = patternToParams(expr.bind);
  const wrappedBody = wrapBodyWithPattern(expr.bind, body, loc);
  const wrappedGuard = guard ? wrapBodyWithPattern(expr.bind, guard, loc) : null;

  // Build filter(collection, fn(P) => G) if there's a guard
  let source = collection;
  if (wrappedGuard) {
    const filterFn: Expr = {
      tag: "lambda",
      params: elemParams,
      body: wrappedGuard,
      loc,
    };
    source = {
      tag: "apply",
      fn: { tag: "var", name: "filter", loc },
      args: [source, filterFn],
      loc,
    };
  }

  if (expr.fold) {
    // fold form: fold(source, init, fn(acc, P) => B)
    const init = desugar(expr.fold.init);
    const foldFn: Expr = {
      tag: "lambda",
      params: [{ name: expr.fold.acc, type: null }, ...elemParams],
      body: wrappedBody,
      loc,
    };
    return {
      tag: "apply",
      fn: { tag: "var", name: "fold", loc },
      args: [source, init, foldFn],
      loc,
    };
  }

  // map form: map(source, fn(P) => B)
  const mapFn: Expr = {
    tag: "lambda",
    params: elemParams,
    body: wrappedBody,
    loc,
  };
  return {
    tag: "apply",
    fn: { tag: "var", name: "map", loc },
    args: [source, mapFn],
    loc,
  };
}
