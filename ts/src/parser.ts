// Clank parser — recursive descent with precedence climbing
// Produces AST from lexer tokens. Phase 1: core expressions only.
// Phase 2 (IMPL-006/009): pipeline, lambda, match, do-blocks, type decls.

import type { Token } from "./lexer.js";
import type {
  Constraint, DoStep, EffectRef, Expr, HandlerArm, ImportItem, Loc, MatchArm, MethodSig, OpSig, Param, Pattern, Program, TopLevel,
  TypeExpr, TypeSig, Variant,
} from "./ast.js";

// ── Parse error ──

export type ParseError = {
  code: string;
  message: string;
  location: Loc;
  context: string;
};

// ── Entry point ──

export function parse(tokens: Token[]): Program | ParseError {
  const p = new Parser(tokens);
  return p.run();
}

export function parseExpression(tokens: Token[]): Expr | ParseError {
  const p = new Parser(tokens);
  return p.runExpr();
}

// ── Comparison operators (non-associative) ──

const CMP_OPS = new Set(["==", "!=", "<", ">", "<=", ">="]);

// ── Parser ──

class Parser {
  private pos = 0;

  constructor(private tokens: Token[]) {}

  // ── Navigation ──

  private peek(): Token {
    return this.tokens[this.pos];
  }

  private advance(): Token {
    return this.tokens[this.pos++];
  }

  private at(tag: string, value?: string): boolean {
    const t = this.peek();
    return t.tag === tag && (value === undefined || t.value === value);
  }

  private expect(tag: string, value?: string): Token {
    if (!this.at(tag, value)) {
      const t = this.peek();
      const expected = value ? `'${value}'` : tag;
      this.fail(`expected ${expected}, got '${t.value || t.tag}'`);
    }
    return this.advance();
  }

  private fail(message: string): never {
    const t = this.peek();
    throw {
      code: "E100",
      message,
      location: t.loc,
      context: `near '${t.value || t.tag}'`,
    } as ParseError;
  }

  // ── Entry ──

  run(): Program | ParseError {
    try {
      return this.parseProgram();
    } catch (e: unknown) {
      if (e && typeof e === "object" && "code" in e) return e as ParseError;
      throw e;
    }
  }

  runExpr(): Expr | ParseError {
    try {
      const expr = this.parseExpr();
      if (!this.at("eof")) {
        this.fail("unexpected token after expression");
      }
      return expr;
    } catch (e: unknown) {
      if (e && typeof e === "object" && "code" in e) return e as ParseError;
      throw e;
    }
  }

  // ── Program ──

  private parseProgram(): Program {
    const topLevels: TopLevel[] = [];
    while (!this.at("eof")) {
      if (this.at("keyword", "mod")) {
        topLevels.push(this.parseModDecl());
      } else if (this.at("keyword", "use")) {
        topLevels.push(this.parseUseDecl());
      } else if (this.at("keyword", "pub")) {
        topLevels.push(...this.parsePubDecl());
      } else if (this.at("keyword", "affine")) {
        topLevels.push(this.parseAffineTypeDecl(false));
      } else if (this.at("keyword", "type")) {
        topLevels.push(this.parseTypeDecl(false));
      } else if (this.at("keyword", "effect")) {
        topLevels.push(this.parseEffectDecl(false));
      } else if (this.at("keyword", "interface")) {
        topLevels.push(this.parseInterfaceDecl(false));
      } else if (this.at("keyword", "impl")) {
        topLevels.push(this.parseImplBlock(false));
      } else if (this.at("keyword", "test")) {
        topLevels.push(this.parseTestDecl());
      } else if (this.at("keyword", "extern")) {
        topLevels.push(...this.parseExternDecl(false));
      } else {
        topLevels.push(this.parseDefinition(false));
      }
    }
    return { topLevels };
  }

  // ── Module declaration ──
  // mod path.to.module

  private parseModDecl(): TopLevel {
    const loc = this.expect("keyword", "mod").loc;
    const path = this.parseModPath();
    return { tag: "mod-decl", path, loc };
  }

  // ── Use declaration ──
  // use path.to.module (name1, name2 as alias)

  private parseUseDecl(): TopLevel {
    const loc = this.expect("keyword", "use").loc;
    const path = this.parseModPath();
    this.expect("delim", "(");
    const imports: ImportItem[] = [];
    if (!this.at("delim", ")")) {
      do {
        const name = this.expect("ident").value;
        let alias: string | null = null;
        if (this.at("ident") && this.peek().value === "as") {
          this.advance(); // consume 'as'
          alias = this.expect("ident").value;
        }
        imports.push({ name, alias });
      } while (this.at("delim", ",") && this.advance());
    }
    this.expect("delim", ")");
    return { tag: "use-decl", path, imports, loc };
  }

  // ── Pub prefix ──
  // pub definition | pub type ... | pub effect ...

  private parsePubDecl(): TopLevel[] {
    this.expect("keyword", "pub");
    if (this.at("keyword", "affine")) {
      return [this.parseAffineTypeDecl(true)];
    } else if (this.at("keyword", "type")) {
      return [this.parseTypeDecl(true)];
    } else if (this.at("keyword", "effect")) {
      return [this.parseEffectDecl(true)];
    } else if (this.at("keyword", "interface")) {
      return [this.parseInterfaceDecl(true)];
    } else if (this.at("keyword", "impl")) {
      return [this.parseImplBlock(true)];
    } else if (this.at("keyword", "extern")) {
      return this.parseExternDecl(true);
    } else {
      return [this.parseDefinition(true)];
    }
  }

  // ── Module path ──
  // ident { '.' ident }

  private parseModPath(): string[] {
    const path = [this.expectIdentOrKeyword()];
    while (this.at("delim", ".")) {
      this.advance();
      path.push(this.expectIdentOrKeyword());
    }
    return path;
  }

  private expectIdentOrKeyword(): string {
    const t = this.peek();
    if (t.tag === "ident" || t.tag === "keyword") {
      this.advance();
      return t.value;
    }
    this.fail(`expected identifier, got '${t.value || t.tag}'`);
  }

  // ── Top-level definition ──
  // ident : type-sig = body

  private parseDefinition(pub: boolean): TopLevel {
    const nameTok = this.expect("ident");
    this.expect("delim", ":");
    const sig = this.parseTypeSig();
    // Optional where constraints
    const constraints = this.tryParseConstraints();
    this.expect("op", "=");
    const body = this.parseBody();
    return { tag: "definition", name: nameTok.value, sig, body, pub, constraints, loc: nameTok.loc };
  }

  // ── Test declaration ──
  // test "name" = body

  private parseTestDecl(): TopLevel {
    const loc = this.expect("keyword", "test").loc;
    const nameTok = this.expect("str");
    this.expect("op", "=");
    const body = this.parseBody();
    return { tag: "test-decl", name: nameTok.value, body, loc };
  }

  // ── Extern declaration ──
  // extern "lib" name : type-sig [where host = "js", symbol = "name", unsafe]

  private parseExternDecl(pub: boolean): TopLevel[] {
    const loc = this.expect("keyword", "extern").loc;

    // extern mod "lib" [where ...] { members }
    if (this.at("keyword", "mod")) {
      this.advance();
      const library = this.expect("str").value;
      // Parse optional shared where attributes
      const shared = this.parseExternWhereAttrs();
      // Infer host from library name if not specified
      let host = shared.host;
      if (!host) {
        if (library.startsWith("node:")) host = "js";
        else if (library.startsWith("lib")) host = "c";
      }
      this.expect("delim", "{");
      const decls: TopLevel[] = [];
      while (!this.at("delim", "}")) {
        let memberPub = pub;
        if (this.at("keyword", "pub")) {
          this.advance();
          memberPub = true;
        }
        const memberLoc = this.peek().loc;
        const name = this.expect("ident").value;
        this.expect("delim", ":");
        const sig = this.parseTypeSig();
        // Parse optional per-member where attributes (override shared)
        const memberAttrs = this.parseExternWhereAttrs();
        decls.push({
          tag: "extern-decl",
          name,
          sig,
          library,
          host: memberAttrs.host ?? host,
          symbol: memberAttrs.symbol ?? null,
          unsafe: memberAttrs.unsafe || shared.unsafe,
          pub: memberPub,
          loc: memberLoc,
        });
      }
      this.expect("delim", "}");
      return decls;
    }

    // Single extern declaration: extern "lib" name : sig [where ...]
    const library = this.expect("str").value;
    const name = this.expect("ident").value;
    this.expect("delim", ":");
    const sig = this.parseTypeSig();
    const attrs = this.parseExternWhereAttrs();
    // Infer host from library name if not specified
    let host = attrs.host;
    if (!host) {
      if (library.startsWith("node:")) host = "js";
      else if (library.startsWith("lib")) host = "c";
    }
    return [{ tag: "extern-decl", name, sig, library, host, symbol: attrs.symbol, unsafe: attrs.unsafe, pub, loc }];
  }

  private parseExternWhereAttrs(): { host: string | null; symbol: string | null; unsafe: boolean } {
    let host: string | null = null;
    let symbol: string | null = null;
    let isUnsafe = false;
    if (this.at("keyword", "where")) {
      this.advance();
      do {
        if (this.at("keyword", "unsafe")) {
          this.advance();
          isUnsafe = true;
        } else {
          const attrName = this.expect("ident").value;
          this.expect("op", "=");
          const attrVal = this.expect("str").value;
          if (attrName === "host") host = attrVal;
          else if (attrName === "symbol") symbol = attrVal;
          else this.fail(`unknown extern attribute '${attrName}'`);
        }
      } while (this.at("delim", ",") && this.advance());
    }
    return { host, symbol, unsafe: isUnsafe };
  }

  // ── Affine type declaration ──
  // affine type Name = Variant1(T) | Variant2(T, U)

  private parseAffineTypeDecl(pub: boolean): TopLevel {
    const loc = this.expect("keyword", "affine").loc;
    this.expect("keyword", "type");
    const name = this.expect("ident").value;

    const typeParams: string[] = [];
    if (this.at("op", "<")) {
      this.advance();
      do {
        typeParams.push(this.expect("ident").value);
      } while (this.at("delim", ",") && this.advance());
      this.expect("op", ">");
    }

    this.expect("op", "=");

    const variants: Variant[] = [];
    variants.push(this.parseVariant());
    while (this.at("delim", "|")) {
      this.advance();
      variants.push(this.parseVariant());
    }

    const deriving: string[] = [];
    if (this.at("keyword", "deriving")) {
      this.advance();
      this.expect("delim", "(");
      if (!this.at("delim", ")")) {
        do {
          deriving.push(this.expect("ident").value);
        } while (this.at("delim", ",") && this.advance());
      }
      this.expect("delim", ")");
    }

    return { tag: "type-decl", name, typeParams, variants, affine: true, deriving, pub, loc };
  }

  // ── Type declaration ──
  // type Name = Variant1(T) | Variant2(T, U)

  private parseTypeDecl(pub: boolean): TopLevel {
    const loc = this.expect("keyword", "type").loc;
    const name = this.expect("ident").value;

    // Optional type parameters: <A, B>
    const typeParams: string[] = [];
    if (this.at("op", "<")) {
      this.advance();
      do {
        typeParams.push(this.expect("ident").value);
      } while (this.at("delim", ",") && this.advance());
      this.expect("op", ">");
    }

    this.expect("op", "=");

    const variants: Variant[] = [];
    // First variant (no leading |)
    variants.push(this.parseVariant());
    // Subsequent variants with | separator
    while (this.at("delim", "|")) {
      this.advance();
      variants.push(this.parseVariant());
    }

    // Optional deriving clause: deriving (Eq, Show, Clone)
    const deriving: string[] = [];
    if (this.at("keyword", "deriving")) {
      this.advance();
      this.expect("delim", "(");
      if (!this.at("delim", ")")) {
        do {
          deriving.push(this.expect("ident").value);
        } while (this.at("delim", ",") && this.advance());
      }
      this.expect("delim", ")");
    }

    return { tag: "type-decl", name, typeParams, variants, affine: false, deriving, pub, loc };
  }

  private parseVariant(): Variant {
    const name = this.expect("ident").value;
    const fields: TypeExpr[] = [];
    if (this.at("delim", "(")) {
      this.advance();
      if (!this.at("delim", ")")) {
        fields.push(this.parseTypeExpr());
        while (this.at("delim", ",")) {
          this.advance();
          fields.push(this.parseTypeExpr());
        }
      }
      this.expect("delim", ")");
    }
    return { name, fields };
  }

  // ── Effect alias declaration ──
  // effect alias Name = <eff1, eff2>
  // effect alias Name<A, B> = <eff1, A, B>

  private parseEffectAlias(pub: boolean, loc: Loc): TopLevel {
    this.expect("keyword", "alias");
    const name = this.expect("ident").value;

    // Optional type parameters: <A, B>
    const params: string[] = [];
    if (this.at("op", "<")) {
      this.advance();
      do {
        params.push(this.expect("ident").value);
      } while (this.at("delim", ",") && this.advance());
      this.expect("op", ">");
    }

    this.expect("op", "=");

    // Parse the effect set: <eff1, eff2, ...> or <eff1, eff2 \ eff3>
    const { effects, subtracted } = this.parseEffectAnn();
    return { tag: "effect-alias", name, params, effects, subtracted, pub, loc };
  }

  // ── Effect declaration ──
  // effect Name { op1 : Type -> Type, op2 : Type -> Type }

  private parseEffectDecl(pub: boolean): TopLevel {
    const loc = this.expect("keyword", "effect").loc;
    // Check for 'effect alias' syntax
    if (this.at("keyword", "alias")) {
      return this.parseEffectAlias(pub, loc);
    }
    const name = this.expect("ident").value;
    this.expect("delim", "{");
    const ops: OpSig[] = [];
    while (!this.at("delim", "}")) {
      const opName = this.expect("ident").value;
      this.expect("delim", ":");
      const opType = this.parseTypeExpr();
      let paramType: TypeExpr;
      let returnType: TypeExpr;
      if (opType.tag === "t-fn") {
        paramType = opType.param;
        returnType = opType.result;
      } else {
        this.fail(`expected function type for effect operation '${opName}'`);
      }
      // Normalize: if param is an empty tuple (unit), treat as arity-0
      const params = (paramType.tag === "t-tuple" && paramType.elements.length === 0)
        ? []
        : [{ name: "_", type: paramType }];
      const sig: TypeSig = {
        params,
        effects: [],
        subtracted: [],
        returnType,
      };
      ops.push({ name: opName, sig });
      // Optional comma separator between operations
      if (this.at("delim", ",")) this.advance();
    }
    this.expect("delim", "}");
    return { tag: "effect-decl", name, ops, pub, loc };
  }

  // ── Type signature ──
  // (params) -> <effects> return-type

  private parseTypeSig(): TypeSig {
    this.expect("delim", "(");
    const params: { name: string; type: TypeExpr }[] = [];
    if (!this.at("delim", ")")) {
      do {
        const name = this.expect("ident").value;
        this.expect("delim", ":");
        const type = this.parseTypeExpr();
        params.push({ name, type });
      } while (this.at("delim", ",") && this.advance());
    }
    this.expect("delim", ")");
    this.expect("op", "->");
    const { effects, subtracted } = this.parseEffectAnn();
    const returnType = this.parseTypeExpr();
    return { params, effects, subtracted, returnType };
  }

  // ── Effect annotation ──
  // <> or <eff1, eff2, ...> or <eff1, eff2 \ eff3, eff4>

  private parseEffectRef(): EffectRef {
    const name = this.expect("ident").value;
    const args: string[] = [];
    if (this.at("op", "<")) {
      this.advance();
      do {
        args.push(this.expect("ident").value);
      } while (this.at("delim", ",") && this.advance());
      this.expect("op", ">");
    }
    return { name, args };
  }

  private parseEffectAnn(): { effects: EffectRef[]; subtracted: EffectRef[] } {
    this.expect("op", "<");
    const effects: EffectRef[] = [];
    if (!this.at("op", ">")) {
      do {
        effects.push(this.parseEffectRef());
      } while (this.at("delim", ",") && this.advance());
    }
    // Check for subtraction operator: backslash
    let subtracted: EffectRef[] = [];
    if (this.at("op", "\\")) {
      this.advance();
      do {
        subtracted.push(this.parseEffectRef());
      } while (this.at("delim", ",") && this.advance());
    }
    this.expect("op", ">");
    return { effects, subtracted };
  }

  // ── Type expression ──

  private parseTypeExpr(): TypeExpr {
    let base = this.parseBaseTypeExpr();

    // Postfix type-level queries: T @tag (tag projection)
    while (this.at("delim", "@")) {
      this.advance();
      const tagName = this.expect("ident").value;
      base = { tag: "t-tag-project", base, tagName, loc: base.loc };
    }

    // Function type: T -> <effects> U (right-associative)
    if (this.at("op", "->")) {
      this.advance();
      let effects: EffectRef[] = [];
      let subtracted: EffectRef[] = [];
      if (this.at("op", "<")) {
        const ann = this.parseEffectAnn();
        effects = ann.effects;
        subtracted = ann.subtracted;
      }
      const result = this.parseTypeExpr();
      return { tag: "t-fn", param: base, effects, subtracted, result, loc: base.loc };
    }

    return base;
  }

  private parseBaseTypeExpr(): TypeExpr {
    const loc = this.peek().loc;

    // &T — borrow type annotation
    if (this.at("delim", "&")) {
      this.advance();
      const inner = this.parseBaseTypeExpr();
      return { tag: "t-borrow", inner, loc };
    }

    // () or (T, U) or (T)
    if (this.at("delim", "(")) {
      this.advance();
      if (this.at("delim", ")")) {
        this.advance();
        return { tag: "t-tuple", elements: [], loc };
      }
      const first = this.parseTypeExpr();
      if (this.at("delim", ",")) {
        const elements = [first];
        while (this.at("delim", ",")) {
          this.advance();
          elements.push(this.parseTypeExpr());
        }
        this.expect("delim", ")");
        return { tag: "t-tuple", elements, loc };
      }
      this.expect("delim", ")");
      return first;
    }

    // [T]
    if (this.at("delim", "[")) {
      this.advance();
      const element = this.parseTypeExpr();
      this.expect("delim", "]");
      return { tag: "t-list", element, loc };
    }

    // {field: Type, ...} or {field: Type, ... | r} — record type with optional row variable
    if (this.at("delim", "{")) {
      this.advance();
      const fields: { name: string; tags: string[]; type: TypeExpr }[] = [];
      let rowVar: string | null = null;
      if (!this.at("delim", "}")) {
        // Check for bare row variable: {r}
        if (this.at("ident") && this.pos + 1 < this.tokens.length &&
            this.tokens[this.pos + 1].tag === "delim" && this.tokens[this.pos + 1].value === "}") {
          // Could be bare row variable {r} — check if next token is }
          // Ambiguity: {ident} could be {r} (bare row var) or {name: Type} missing colon
          // Heuristic: if the ident starts lowercase and no colon follows, treat as row var
          const peeked = this.tokens[this.pos];
          if (peeked.value[0] === peeked.value[0].toLowerCase() && peeked.value[0] !== peeked.value[0].toUpperCase()) {
            rowVar = this.advance().value;
            this.expect("delim", "}");
            return { tag: "t-record", fields, rowVar, loc };
          }
        }
        do {
          // Parse optional @tag annotations before field name
          const tags: string[] = [];
          while (this.at("delim", "@")) {
            this.advance();
            tags.push(this.expect("ident").value);
          }
          const name = this.expect("ident").value;
          this.expect("delim", ":");
          const type = this.parseTypeExpr();
          fields.push({ name, tags, type });
        } while (this.at("delim", ",") && this.advance());
        // Check for row variable tail: | r
        if (this.at("delim", "|")) {
          this.advance();
          rowVar = this.expect("ident").value;
        }
      }
      this.expect("delim", "}");
      return { tag: "t-record", fields, rowVar, loc };
    }

    // Self type (inside interface/impl blocks)
    if (this.at("keyword", "Self")) {
      const selfLoc = this.advance().loc;
      return { tag: "t-name", name: "Self", loc: selfLoc };
    }

    // Named type (may have generic args, or Pick/Omit builtins)
    if (this.at("ident")) {
      const name = this.advance().value;
      // Built-in Pick<T, "f1" | "f2"> and Omit<T, "f1" | "f2">
      if ((name === "Pick" || name === "Omit") && this.at("op", "<")) {
        this.advance();
        const baseType = this.parseTypeExpr();
        this.expect("delim", ",");
        // Parse string literal field names separated by |
        const fieldNames: string[] = [];
        fieldNames.push(this.expect("str").value);
        while (this.at("delim", "|")) {
          this.advance();
          fieldNames.push(this.expect("str").value);
        }
        this.expect("op", ">");
        const queryTag = name === "Pick" ? "t-pick" as const : "t-omit" as const;
        return { tag: queryTag, base: baseType, fieldNames, loc };
      }
      let base: TypeExpr;
      if (this.at("op", "<")) {
        this.advance();
        const args: TypeExpr[] = [this.parseTypeExpr()];
        while (this.at("delim", ",")) {
          this.advance();
          args.push(this.parseTypeExpr());
        }
        this.expect("op", ">");
        base = { tag: "t-generic", name, args, loc };
      } else {
        base = { tag: "t-name", name, loc };
      }

      // Refinement type: Type{predicate}
      if (this.at("delim", "{")) {
        this.advance();
        let predicate = "";
        let depth = 1;
        while (depth > 0 && !this.at("eof")) {
          if (this.at("delim", "{")) depth++;
          if (this.at("delim", "}")) { depth--; if (depth === 0) break; }
          if (predicate) predicate += " ";
          predicate += this.advance().value;
        }
        this.expect("delim", "}");
        return { tag: "t-refined", base, predicate: predicate.trim(), loc };
      }

      return base;
    }

    this.fail("expected type expression");
  }

  // ── Constraint parsing ──

  private tryParseConstraints(): Constraint[] {
    if (!this.at("keyword", "where")) return [];
    this.advance();
    const constraints: Constraint[] = [];
    do {
      constraints.push(this.parseConstraint());
    } while (this.at("delim", ",") && this.advance());
    return constraints;
  }

  private parseConstraint(): Constraint {
    const loc = this.peek().loc;
    const interface_ = this.expect("ident").value;
    const typeArgs: TypeExpr[] = [];
    if (this.at("op", "<")) {
      this.advance();
      do {
        typeArgs.push(this.parseTypeExpr());
      } while (this.at("delim", ",") && this.advance());
      this.expect("op", ">");
    }
    const typeParam = this.expect("ident").value;
    return { interface_, typeArgs, typeParam, loc };
  }

  // Convert a type expression to a TypeSig (for interface method signatures)
  private typeExprToSig(te: TypeExpr): TypeSig {
    if (te.tag === "t-fn") {
      // Extract params from the left side
      const params: { name: string; type: TypeExpr }[] = [];
      if (te.param.tag === "t-tuple" && te.param.elements.length === 0) {
        // () -> T: no params
      } else if (te.param.tag === "t-tuple") {
        for (let i = 0; i < te.param.elements.length; i++) {
          params.push({ name: `_${i}`, type: te.param.elements[i] });
        }
      } else {
        params.push({ name: "_0", type: te.param });
      }
      return { params, effects: te.effects, subtracted: te.subtracted, returnType: te.result };
    }
    // Non-function type — treat as a constant
    return { params: [], effects: [], subtracted: [], returnType: te };
  }

  // ── Interface declaration ──

  private parseInterfaceDecl(pub: boolean): TopLevel {
    const loc = this.expect("keyword", "interface").loc;
    const name = this.expect("ident").value;
    const typeParams: string[] = [];
    if (this.at("op", "<")) {
      this.advance();
      do {
        typeParams.push(this.expect("ident").value);
      } while (this.at("delim", ",") && this.advance());
      this.expect("op", ">");
    }
    const supers: string[] = [];
    if (this.at("delim", ":")) {
      this.advance();
      supers.push(this.expect("ident").value);
      while (this.at("op", "+")) {
        this.advance();
        supers.push(this.expect("ident").value);
      }
    }
    this.expect("delim", "{");
    const methods: MethodSig[] = [];
    while (!this.at("delim", "}")) {
      const methodName = this.expect("ident").value;
      this.expect("delim", ":");
      // Parse method signature as a type expression (not parseTypeSig, which expects named params)
      const typeExpr = this.parseTypeExpr();
      const sig = this.typeExprToSig(typeExpr);
      methods.push({ name: methodName, sig });
      if (this.at("delim", ",")) this.advance();
    }
    this.expect("delim", "}");
    return { tag: "interface-decl", name, typeParams, supers, methods, pub, loc };
  }

  // Parse a type expression for impl 'for' clause (no refinement types — { starts the impl body)
  private parseImplTypeExpr(): TypeExpr {
    const loc = this.peek().loc;

    // () or (T, U) or (T)
    if (this.at("delim", "(")) {
      this.advance();
      if (this.at("delim", ")")) { this.advance(); return { tag: "t-tuple", elements: [], loc }; }
      const first = this.parseTypeExpr();
      if (this.at("delim", ",")) {
        const elements = [first];
        while (this.at("delim", ",")) { this.advance(); elements.push(this.parseTypeExpr()); }
        this.expect("delim", ")");
        return { tag: "t-tuple", elements, loc };
      }
      this.expect("delim", ")");
      return first;
    }

    // [T]
    if (this.at("delim", "[")) {
      this.advance();
      const element = this.parseTypeExpr();
      this.expect("delim", "]");
      return { tag: "t-list", element, loc };
    }

    // Self
    if (this.at("keyword", "Self")) {
      this.advance();
      return { tag: "t-name", name: "Self", loc };
    }

    // Named type with optional generic args (no refinement!)
    if (this.at("ident")) {
      const name = this.advance().value;
      if (this.at("op", "<")) {
        this.advance();
        const args: TypeExpr[] = [this.parseTypeExpr()];
        while (this.at("delim", ",")) { this.advance(); args.push(this.parseTypeExpr()); }
        this.expect("op", ">");
        return { tag: "t-generic", name, args, loc };
      }
      return { tag: "t-name", name, loc };
    }

    this.fail("expected type expression in impl");
  }

  // ── Impl block ──

  private parseImplBlock(pub: boolean): TopLevel {
    const loc = this.expect("keyword", "impl").loc;
    const interface_ = this.expect("ident").value;
    const typeArgs: TypeExpr[] = [];
    if (this.at("op", "<")) {
      this.advance();
      do {
        typeArgs.push(this.parseTypeExpr());
      } while (this.at("delim", ",") && this.advance());
      this.expect("op", ">");
    }
    this.expect("keyword", "for");
    const forType = this.parseImplTypeExpr();
    const constraints = this.tryParseConstraints();
    this.expect("delim", "{");
    const methods: { name: string; body: Expr }[] = [];
    while (!this.at("delim", "}")) {
      const methodName = this.expect("ident").value;
      this.expect("op", "=");
      const body = this.parseExpr();
      methods.push({ name: methodName, body });
    }
    this.expect("delim", "}");
    return { tag: "impl-block", interface_, typeArgs, forType, constraints, methods, pub: true, loc };
  }

  // ── Body (implicit sequencing in definition bodies) ──

  private parseBody(): Expr {
    const expr = this.parseExpr();

    if (this.atBodyEnd()) return expr;

    // Let without 'in' — rest of body becomes the let body
    if (expr.tag === "let" && expr.body === null) {
      expr.body = this.parseBody();
      return expr;
    }
    if (expr.tag === "let-pattern" && expr.body === null) {
      expr.body = this.parseBody();
      return expr;
    }

    // Bare expression followed by more — sequence via let _ = expr in rest
    const rest = this.parseBody();
    return { tag: "let", name: "_", value: expr, body: rest, loc: expr.loc };
  }

  private atBodyEnd(): boolean {
    if (this.at("eof")) return true;
    // type, effect, mod, use, pub start new top-level forms
    if (this.at("keyword", "type")) return true;
    if (this.at("keyword", "effect")) return true;
    if (this.at("keyword", "mod")) return true;
    if (this.at("keyword", "use")) return true;
    if (this.at("keyword", "pub")) return true;
    if (this.at("keyword", "test")) return true;
    if (this.at("keyword", "affine")) return true;
    if (this.at("keyword", "interface")) return true;
    if (this.at("keyword", "impl")) return true;
    // New definition: ident followed by ':'
    if (
      this.at("ident") &&
      this.pos + 1 < this.tokens.length &&
      this.tokens[this.pos + 1].tag === "delim" &&
      this.tokens[this.pos + 1].value === ":"
    ) {
      return true;
    }
    return false;
  }

  // ── Expressions ──

  private parseExpr(): Expr {
    if (this.at("keyword", "let")) return this.parseLet();
    if (this.at("keyword", "if")) return this.parseIf();
    if (this.at("keyword", "match")) return this.parseMatch();
    if (this.at("keyword", "for")) return this.parseFor();
    if (this.at("keyword", "do")) return this.parseDo();
    if (this.at("keyword", "perform")) return this.parsePerform();
    if (this.at("keyword", "handle")) return this.parseHandle();
    if (this.at("keyword", "clone")) return this.parseClone();
    if (this.at("keyword", "discard")) return this.parseDiscard();
    return this.parsePipeExpr();
  }

  // ── Clone expression ──
  // clone expr

  private parseClone(): Expr {
    const loc = this.expect("keyword", "clone").loc;
    const expr = this.parsePipeExpr();
    return { tag: "clone", expr, loc };
  }

  // ── Discard expression ──
  // discard expr

  private parseDiscard(): Expr {
    const loc = this.expect("keyword", "discard").loc;
    const expr = this.parsePipeExpr();
    return { tag: "discard", expr, loc };
  }

  // Level 1: |> (left-assoc, lowest precedence)
  private parsePipeExpr(): Expr {
    let left = this.parseOrExpr();
    while (this.at("op", "|>")) {
      const loc = this.advance().loc;
      const right = this.parseOrExpr();
      left = { tag: "pipeline", left, right, loc };
    }
    return left;
  }

  // ── Let binding ──

  private parseLet(): Expr {
    const loc = this.advance().loc;
    // Check for pattern destructuring: let {fields} = expr or let (a, b) = expr
    if (this.at("delim", "{") || this.at("delim", "(")) {
      const pattern = this.parsePattern();
      this.expect("op", "=");
      const value = this.parseExpr();
      let body: Expr | null = null;
      if (this.at("keyword", "in")) {
        this.advance();
        body = this.parseExpr();
      }
      return { tag: "let-pattern", pattern, value, body, loc };
    }
    const name = this.expect("ident").value;
    this.expect("op", "=");
    const value = this.parseExpr();
    let body: Expr | null = null;
    if (this.at("keyword", "in")) {
      this.advance();
      body = this.parseExpr();
    }
    return { tag: "let", name, value, body, loc };
  }

  // ── If/then/else ──

  private parseIf(): Expr {
    const loc = this.advance().loc;
    const cond = this.parseExpr();
    this.expect("keyword", "then");
    const then_ = this.parseExpr();
    this.expect("keyword", "else");
    const else_ = this.parseExpr();
    return { tag: "if", cond, then: then_, else: else_, loc };
  }

  // ── Match expression ──
  // match expr { pattern => body ... }

  private parseMatch(): Expr {
    const loc = this.expect("keyword", "match").loc;
    const subject = this.parsePipeExpr();
    this.expect("delim", "{");
    const arms: MatchArm[] = [];
    while (!this.at("delim", "}")) {
      const pattern = this.parsePattern();
      this.expect("op", "=>");
      const body = this.parseExpr();
      arms.push({ pattern, body });
    }
    this.expect("delim", "}");
    return { tag: "match", subject, arms, loc };
  }

  // ── Do-block ──
  // do { step1  step2  ... }
  // step = ident <- expr | expr

  private parseDo(): Expr {
    const loc = this.expect("keyword", "do").loc;
    this.expect("delim", "{");
    const steps: DoStep[] = [];
    while (!this.at("delim", "}")) {
      steps.push(this.parseDoStep());
    }
    this.expect("delim", "}");
    if (steps.length === 0) {
      this.fail("do-block must contain at least one step");
    }
    return { tag: "do", steps, loc };
  }

  private parseDoStep(): DoStep {
    // Try to parse: ident <- expr
    // Lookahead: if current is ident and next is <-
    if (
      this.at("ident") &&
      this.pos + 1 < this.tokens.length &&
      this.tokens[this.pos + 1].tag === "op" &&
      this.tokens[this.pos + 1].value === "<-"
    ) {
      const name = this.advance().value;
      this.advance(); // consume <-
      const expr = this.parseExpr();
      return { bind: name, expr };
    }
    // Bare expression
    const expr = this.parseExpr();
    return { bind: null, expr };
  }

  // ── For expression ──
  // for pattern in expr [if expr] do expr
  // for pattern in expr [if expr] fold ident = expr do expr

  private parseFor(): Expr {
    const loc = this.expect("keyword", "for").loc;
    const bind = this.parsePattern();
    this.expect("keyword", "in");
    const collection = this.parsePipeExpr();

    // Optional guard: if expr
    let guard: Expr | null = null;
    if (this.at("keyword", "if")) {
      this.advance();
      guard = this.parsePipeExpr();
    }

    // fold or do
    let fold: { acc: string; init: Expr } | null = null;
    if (this.at("ident") && this.peek().value === "fold") {
      this.advance(); // consume 'fold' (contextual keyword)
      const acc = this.expect("ident").value;
      this.expect("op", "=");
      const init = this.parsePipeExpr();
      fold = { acc, init };
    }

    this.expect("keyword", "do");
    const body = this.parseExpr();

    return { tag: "for", bind, collection, guard, fold, body, loc };
  }

  // ── Perform expression ──
  // perform expr

  private parsePerform(): Expr {
    const loc = this.expect("keyword", "perform").loc;
    const expr = this.parsePipeExpr();
    return { tag: "perform", expr, loc };
  }

  // ── Handle expression ──
  // handle expr { return pattern -> expr, opName params resume k -> expr, ... }

  private parseHandle(): Expr {
    const loc = this.expect("keyword", "handle").loc;
    const expr = this.parsePipeExpr();
    this.expect("delim", "{");
    const arms: HandlerArm[] = [];
    while (!this.at("delim", "}")) {
      if (this.at("keyword", "return")) {
        // return clause: return pattern -> body
        this.advance();
        const param = this.expect("ident").value;
        this.expect("op", "->");
        const body = this.parseExpr();
        arms.push({ name: "return", params: [{ name: param, type: null }], resumeName: null, body });
      } else {
        // operation clause: opName params resume k -> body
        const opName = this.expect("ident").value;
        const params: Param[] = [];
        // Parse parameters until we hit 'resume'
        while (!this.at("keyword", "resume")) {
          params.push({ name: this.expect("ident").value, type: null });
        }
        this.expect("keyword", "resume");
        const resumeName = this.expect("ident").value;
        this.expect("op", "->");
        const body = this.parseExpr();
        arms.push({ name: opName, params, resumeName, body });
      }
      // Optional comma between handler clauses
      if (this.at("delim", ",")) this.advance();
    }
    this.expect("delim", "}");
    return { tag: "handle", expr, arms, loc };
  }

  // ── Pattern ──

  private parsePattern(): Pattern {
    const t = this.peek();

    // Wildcard: _
    if (t.tag === "ident" && t.value === "_") {
      this.advance();
      return { tag: "p-wildcard", loc: t.loc };
    }

    // Literal patterns: int, rat, bool, str
    if (t.tag === "int") {
      this.advance();
      return { tag: "p-literal", value: { tag: "int", value: Number(t.value) }, loc: t.loc };
    }
    if (t.tag === "rat") {
      this.advance();
      return { tag: "p-literal", value: { tag: "rat", value: Number(t.value) }, loc: t.loc };
    }
    if (t.tag === "bool") {
      this.advance();
      return { tag: "p-literal", value: { tag: "bool", value: t.value === "true" }, loc: t.loc };
    }
    if (t.tag === "str") {
      this.advance();
      return { tag: "p-literal", value: { tag: "str", value: t.value }, loc: t.loc };
    }

    // Tuple pattern: (p1, p2, ...)
    if (t.tag === "delim" && t.value === "(") {
      this.advance();
      if (this.at("delim", ")")) {
        this.advance();
        return { tag: "p-literal", value: { tag: "unit" }, loc: t.loc };
      }
      const first = this.parsePattern();
      if (this.at("delim", ",")) {
        const elements = [first];
        while (this.at("delim", ",")) {
          this.advance();
          elements.push(this.parsePattern());
        }
        this.expect("delim", ")");
        return { tag: "p-tuple", elements, loc: t.loc };
      }
      // Single parenthesized pattern
      this.expect("delim", ")");
      return first;
    }

    // Record pattern: {field1, field2: pat, ... | rest}
    if (t.tag === "delim" && t.value === "{") {
      this.advance();
      const fields: { name: string; pattern: Pattern | null }[] = [];
      let rest: string | null = null;
      if (!this.at("delim", "}")) {
        do {
          // Check for | rest at any point
          if (this.at("delim", "|")) {
            this.advance();
            const restTok = this.peek();
            if (restTok.tag === "ident" && restTok.value === "_") {
              this.advance();
              rest = "_";
            } else {
              rest = this.expect("ident").value;
            }
            break;
          }
          const name = this.expect("ident").value;
          // Check for : pattern (rename/nested pattern)
          let pattern: Pattern | null = null;
          if (this.at("delim", ":")) {
            this.advance();
            pattern = this.parsePattern();
          }
          fields.push({ name, pattern });
        } while (this.at("delim", ",") && this.advance());
        // Check for | rest after fields
        if (rest === null && this.at("delim", "|")) {
          this.advance();
          const restTok = this.peek();
          if (restTok.tag === "ident" && restTok.value === "_") {
            this.advance();
            rest = "_";
          } else {
            rest = this.expect("ident").value;
          }
        }
      }
      this.expect("delim", "}");
      return { tag: "p-record", fields, rest, loc: t.loc };
    }

    // Identifier — variant destructure, nullary variant, or variable binding
    // Convention: Capitalized names are variant constructors, lowercase are variables
    if (t.tag === "ident") {
      this.advance();
      const isUpper = t.value[0] >= "A" && t.value[0] <= "Z";
      // Variant destructure: Name(p1, p2, ...)
      if (this.at("delim", "(")) {
        this.advance();
        const args: Pattern[] = [];
        if (!this.at("delim", ")")) {
          args.push(this.parsePattern());
          while (this.at("delim", ",")) {
            this.advance();
            args.push(this.parsePattern());
          }
        }
        this.expect("delim", ")");
        return { tag: "p-variant", name: t.value, args, loc: t.loc };
      }
      // Capitalized without parens — nullary variant pattern
      if (isUpper) {
        return { tag: "p-variant", name: t.value, args: [], loc: t.loc };
      }
      // Variable binding
      return { tag: "p-var", name: t.value, loc: t.loc };
    }

    this.fail(`unexpected '${t.value || t.tag}' in pattern`);
  }

  // ── Binary operators (precedence climbing) ──

  // Level 2: || (left-assoc)
  private parseOrExpr(): Expr {
    let left = this.parseAndExpr();
    while (this.at("op", "||")) {
      const loc = this.advance().loc;
      const right = this.parseAndExpr();
      left = { tag: "infix", op: "||", left, right, loc };
    }
    return left;
  }

  // Level 3: && (left-assoc)
  private parseAndExpr(): Expr {
    let left = this.parseCmpExpr();
    while (this.at("op", "&&")) {
      const loc = this.advance().loc;
      const right = this.parseCmpExpr();
      left = { tag: "infix", op: "&&", left, right, loc };
    }
    return left;
  }

  // Level 4: == != < > <= >= (non-associative)
  private parseCmpExpr(): Expr {
    const left = this.parseRangeExpr();
    const t = this.peek();
    if (t.tag === "op" && CMP_OPS.has(t.value)) {
      const op = this.advance();
      const right = this.parseRangeExpr();
      const next = this.peek();
      if (next.tag === "op" && CMP_OPS.has(next.value)) {
        this.fail("comparison operators are non-associative; use parentheses");
      }
      return { tag: "infix", op: op.value, left, right, loc: op.loc };
    }
    return left;
  }

  // Level 4.5: .. ..= (non-associative)
  private parseRangeExpr(): Expr {
    const left = this.parseConcatExpr();
    const t = this.peek();
    if (t.tag === "op" && (t.value === ".." || t.value === "..=")) {
      const op = this.advance();
      const inclusive = op.value === "..=";
      const right = this.parseConcatExpr();
      const next = this.peek();
      if (next.tag === "op" && (next.value === ".." || next.value === "..=")) {
        this.fail("range operators are non-associative; use parentheses");
      }
      return { tag: "range", start: left, end: right, inclusive, loc: op.loc };
    }
    return left;
  }

  // Level 5: ++ (right-assoc)
  private parseConcatExpr(): Expr {
    const left = this.parseAddExpr();
    if (this.at("op", "++")) {
      const loc = this.advance().loc;
      const right = this.parseConcatExpr();
      return { tag: "infix", op: "++", left, right, loc };
    }
    return left;
  }

  // Level 6: + - (left-assoc)
  private parseAddExpr(): Expr {
    let left = this.parseMulExpr();
    while (this.at("op", "+") || this.at("op", "-")) {
      const op = this.advance();
      const right = this.parseMulExpr();
      left = { tag: "infix", op: op.value, left, right, loc: op.loc };
    }
    return left;
  }

  // Level 7: * / % (left-assoc)
  private parseMulExpr(): Expr {
    let left = this.parseUnaryExpr();
    while (this.at("op", "*") || this.at("op", "/") || this.at("op", "%")) {
      const op = this.advance();
      const right = this.parseUnaryExpr();
      left = { tag: "infix", op: op.value, left, right, loc: op.loc };
    }
    return left;
  }

  // Level 8: - ! & (prefix unary)
  private parseUnaryExpr(): Expr {
    if (this.at("op", "-") || this.at("op", "!")) {
      const op = this.advance();
      const operand = this.parseUnaryExpr();
      return { tag: "unary", op: op.value, operand, loc: op.loc };
    }
    // & prefix — borrow expression
    if (this.at("delim", "&")) {
      const loc = this.advance().loc;
      const operand = this.parseUnaryExpr();
      return { tag: "borrow", expr: operand, loc };
    }
    return this.parsePostfixExpr();
  }

  // Level 9-10: f(args), expr.field
  private parsePostfixExpr(): Expr {
    let expr = this.parseAtom();
    while (true) {
      if (this.at("delim", "(")) {
        this.advance();
        const args: Expr[] = [];
        if (!this.at("delim", ")")) {
          args.push(this.parseExpr());
          while (this.at("delim", ",")) {
            this.advance();
            args.push(this.parseExpr());
          }
        }
        this.expect("delim", ")");
        expr = { tag: "apply", fn: expr, args, loc: expr.loc };
      } else if (this.at("delim", ".")) {
        this.advance();
        const field = this.expect("ident").value;
        expr = { tag: "field-access", object: expr, field, loc: expr.loc };
      } else {
        break;
      }
    }
    return expr;
  }

  // ── Atoms ──

  private parseAtom(): Expr {
    const t = this.peek();

    // fn(params) => expr — lambda
    if (t.tag === "keyword" && t.value === "fn") {
      this.advance();
      this.expect("delim", "(");
      const params: Param[] = [];
      if (!this.at("delim", ")")) {
        do {
          const name = this.expect("ident").value;
          let type: TypeExpr | null = null;
          if (this.at("delim", ":")) {
            this.advance();
            type = this.parseTypeExpr();
          }
          params.push({ name, type });
        } while (this.at("delim", ",") && this.advance());
      }
      this.expect("delim", ")");
      this.expect("op", "=>");
      const body = this.parseExpr();
      return { tag: "lambda", params, body, loc: t.loc };
    }

    if (t.tag === "int") {
      this.advance();
      return { tag: "literal", value: { tag: "int", value: Number(t.value) }, loc: t.loc };
    }

    if (t.tag === "rat") {
      this.advance();
      return { tag: "literal", value: { tag: "rat", value: Number(t.value) }, loc: t.loc };
    }

    if (t.tag === "bool") {
      this.advance();
      return { tag: "literal", value: { tag: "bool", value: t.value === "true" }, loc: t.loc };
    }

    if (t.tag === "str") {
      this.advance();
      return { tag: "literal", value: { tag: "str", value: t.value }, loc: t.loc };
    }

    if (t.tag === "ident") {
      this.advance();
      return { tag: "var", name: t.value, loc: t.loc };
    }

    // ( ) — unit, (expr) — parens, (a, b) — tuple
    if (t.tag === "delim" && t.value === "(") {
      this.advance();
      if (this.at("delim", ")")) {
        this.advance();
        return { tag: "literal", value: { tag: "unit" }, loc: t.loc };
      }
      const inner = this.parseExpr();
      if (this.at("delim", ",")) {
        const elements = [inner];
        while (this.at("delim", ",")) {
          this.advance();
          elements.push(this.parseExpr());
        }
        this.expect("delim", ")");
        return { tag: "tuple", elements, loc: t.loc };
      }
      this.expect("delim", ")");
      return inner;
    }

    // [a, b, c] — list literal
    if (t.tag === "delim" && t.value === "[") {
      this.advance();
      const elements: Expr[] = [];
      if (!this.at("delim", "]")) {
        elements.push(this.parseExpr());
        while (this.at("delim", ",")) {
          this.advance();
          elements.push(this.parseExpr());
        }
      }
      this.expect("delim", "]");
      return { tag: "list", elements, loc: t.loc };
    }

    // {name: val, ...} — record literal
    // {name: val, ..base} — record literal with spread
    // {base | name: val, ...} — record update
    if (t.tag === "delim" && t.value === "{") {
      this.advance();
      // Empty record
      if (this.at("delim", "}")) {
        this.advance();
        return { tag: "record", fields: [], spread: null, loc: t.loc };
      }
      // Check for record update: { ident | field: val }
      // Lookahead: if ident followed by |, it's a record update
      if (
        this.at("ident") &&
        this.pos + 1 < this.tokens.length &&
        this.tokens[this.pos + 1].tag === "delim" &&
        this.tokens[this.pos + 1].value === "|"
      ) {
        const baseToken = this.advance();
        const base: Expr = { tag: "var", name: baseToken.value, loc: baseToken.loc };
        this.expect("delim", "|");
        const fields: { name: string; value: Expr }[] = [];
        do {
          const name = this.expect("ident").value;
          this.expect("delim", ":");
          const value = this.parseExpr();
          fields.push({ name, value });
        } while (this.at("delim", ",") && this.advance());
        this.expect("delim", "}");
        return { tag: "record-update", base, fields, loc: t.loc };
      }
      // Regular record literal (with optional @tag annotations on fields and spread)
      const fields: { name: string; tags: string[]; value: Expr }[] = [];
      let spread: Expr | null = null;
      do {
        // Check for spread: ..expr
        if (this.at("op", "..")) {
          this.advance();
          spread = this.parseExpr();
          break;
        }
        // Parse optional @tag annotations
        const tags: string[] = [];
        while (this.at("delim", "@")) {
          this.advance();
          tags.push(this.expect("ident").value);
        }
        const name = this.expect("ident").value;
        this.expect("delim", ":");
        const value = this.parseExpr();
        fields.push({ name, tags, value });
      } while (this.at("delim", ",") && this.advance());
      this.expect("delim", "}");
      return { tag: "record", fields, spread, loc: t.loc };
    }

    this.fail(`unexpected '${t.value || t.tag}'`);
  }
}
