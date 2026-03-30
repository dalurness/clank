// Clank AST type definitions
// Discriminated unions for all AST nodes

// ── Source location ──

export type Loc = { line: number; col: number; end_line?: number; end_col?: number };

// ── Literals ──

export type Literal =
  | { tag: "int"; value: number }
  | { tag: "rat"; value: number }
  | { tag: "bool"; value: boolean }
  | { tag: "str"; value: string }
  | { tag: "unit" };

// ── Patterns ──

export type PatField = { name: string; pattern: Pattern | null };

export type Pattern =
  | { tag: "p-var"; name: string; loc: Loc }
  | { tag: "p-literal"; value: Literal; loc: Loc }
  | { tag: "p-variant"; name: string; args: Pattern[]; loc: Loc }
  | { tag: "p-tuple"; elements: Pattern[]; loc: Loc }
  | { tag: "p-record"; fields: PatField[]; rest: string | null; loc: Loc }
  | { tag: "p-wildcard"; loc: Loc };

// ── Effect references (effect name with optional type arguments) ──

export type EffectRef = { name: string; args: string[] };

// ── Type annotations ──

export type TypeExpr =
  | { tag: "t-name"; name: string; loc: Loc }
  | { tag: "t-list"; element: TypeExpr; loc: Loc }
  | { tag: "t-tuple"; elements: TypeExpr[]; loc: Loc }
  | { tag: "t-record"; fields: { name: string; tags: string[]; type: TypeExpr }[]; rowVar: string | null; loc: Loc }
  | { tag: "t-union"; left: TypeExpr; right: TypeExpr; loc: Loc }
  | { tag: "t-fn"; param: TypeExpr; effects: EffectRef[]; subtracted: EffectRef[]; result: TypeExpr; loc: Loc }
  | { tag: "t-generic"; name: string; args: TypeExpr[]; loc: Loc }
  | { tag: "t-refined"; base: TypeExpr; predicate: string; loc: Loc }
  | { tag: "t-borrow"; inner: TypeExpr; loc: Loc }
  | { tag: "t-tag-project"; base: TypeExpr; tagName: string; loc: Loc }
  | { tag: "t-type-filter"; base: TypeExpr; filterType: TypeExpr; loc: Loc }
  | { tag: "t-pick"; base: TypeExpr; fieldNames: string[]; loc: Loc }
  | { tag: "t-omit"; base: TypeExpr; fieldNames: string[]; loc: Loc };

// ── Expressions ──

export type Expr =
  | { tag: "literal"; value: Literal; loc: Loc }
  | { tag: "var"; name: string; loc: Loc }
  | { tag: "let"; name: string; value: Expr; body: Expr | null; loc: Loc }
  | { tag: "let-pattern"; pattern: Pattern; value: Expr; body: Expr | null; loc: Loc }
  | { tag: "if"; cond: Expr; then: Expr; else: Expr; loc: Loc }
  | { tag: "match"; subject: Expr; arms: MatchArm[]; loc: Loc }
  | { tag: "lambda"; params: Param[]; body: Expr; loc: Loc }
  | { tag: "apply"; fn: Expr; args: Expr[]; loc: Loc }
  | { tag: "pipeline"; left: Expr; right: Expr; loc: Loc }
  | { tag: "infix"; op: string; left: Expr; right: Expr; loc: Loc }
  | { tag: "unary"; op: string; operand: Expr; loc: Loc }
  | { tag: "do"; steps: DoStep[]; loc: Loc }
  | { tag: "handle"; expr: Expr; arms: HandlerArm[]; loc: Loc }
  | { tag: "perform"; expr: Expr; loc: Loc }
  | { tag: "list"; elements: Expr[]; loc: Loc }
  | { tag: "tuple"; elements: Expr[]; loc: Loc }
  | { tag: "record"; fields: { name: string; tags: string[]; value: Expr }[]; spread: Expr | null; loc: Loc }
  | { tag: "record-update"; base: Expr; fields: { name: string; value: Expr }[]; loc: Loc }
  | { tag: "field-access"; object: Expr; field: string; loc: Loc }
  | { tag: "for"; bind: Pattern; collection: Expr; guard: Expr | null; fold: { acc: string; init: Expr } | null; body: Expr; loc: Loc }
  | { tag: "range"; start: Expr; end: Expr; inclusive: boolean; loc: Loc }
  | { tag: "borrow"; expr: Expr; loc: Loc }
  | { tag: "clone"; expr: Expr; loc: Loc }
  | { tag: "discard"; expr: Expr; loc: Loc };

export type MatchArm = { pattern: Pattern; body: Expr };
export type HandlerArm = { name: string; params: Param[]; resumeName: string | null; body: Expr };
export type DoStep = { bind: string | null; expr: Expr };
export type Param = { name: string; type: TypeExpr | null };

// ── Type signature ──

export type TypeSig = {
  params: { name: string; type: TypeExpr }[];
  effects: EffectRef[];
  subtracted: EffectRef[];
  returnType: TypeExpr;
};

// ── Interface constraints (where clauses) ──

export type Constraint = { interface_: string; typeArgs: TypeExpr[]; typeParam: string; loc: Loc };
export type MethodSig = { name: string; sig: TypeSig };

// ── Import item (for use declarations) ──

export type ImportItem = { name: string; alias: string | null };

// ── Top-level forms ──

export type TopLevel =
  | { tag: "definition"; name: string; sig: TypeSig; body: Expr; pub: boolean; constraints: Constraint[]; loc: Loc }
  | { tag: "type-decl"; name: string; typeParams: string[]; variants: Variant[]; affine: boolean; deriving: string[]; pub: boolean; loc: Loc }
  | { tag: "effect-decl"; name: string; ops: OpSig[]; pub: boolean; loc: Loc }
  | { tag: "effect-alias"; name: string; params: string[]; effects: EffectRef[]; subtracted: EffectRef[]; pub: boolean; loc: Loc }
  | { tag: "mod-decl"; path: string[]; loc: Loc }
  | { tag: "use-decl"; path: string[]; imports: ImportItem[]; loc: Loc }
  | { tag: "test-decl"; name: string; body: Expr; loc: Loc }
  | { tag: "interface-decl"; name: string; typeParams: string[]; supers: string[]; methods: MethodSig[]; pub: boolean; loc: Loc }
  | { tag: "impl-block"; interface_: string; typeArgs: TypeExpr[]; forType: TypeExpr; constraints: Constraint[]; methods: { name: string; body: Expr }[]; pub: boolean; loc: Loc }
  | { tag: "extern-decl"; name: string; sig: TypeSig; library: string; host: string | null; symbol: string | null; unsafe: boolean; pub: boolean; loc: Loc };

export type Variant = { name: string; fields: TypeExpr[] };
export type OpSig = { name: string; sig: TypeSig };

// ── Program ──

export type Program = { topLevels: TopLevel[] };
