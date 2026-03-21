// Clank bytecode compiler
// Compiles desugared AST to stack VM bytecode per compilation-strategy.md
// and vm-instruction-set.md

import type { Expr, Program, TopLevel, Pattern, MatchArm, Param } from "./ast.js";

// ── Opcodes ──

export const Op = {
  NOP:           0x00,
  DUP:           0x01,
  DROP:          0x02,
  SWAP:          0x03,
  ROT:           0x04,
  OVER:          0x05,
  PICK:          0x06,
  ROLL:          0x07,

  PUSH_INT:      0x10,
  PUSH_INT16:    0x11,
  PUSH_INT32:    0x12,
  PUSH_TRUE:     0x13,
  PUSH_FALSE:    0x14,
  PUSH_UNIT:     0x15,
  PUSH_STR:      0x16,
  PUSH_BYTE:     0x17,
  PUSH_RAT:      0x18,

  ADD:           0x20,
  SUB:           0x21,
  MUL:           0x22,
  DIV:           0x23,
  MOD:           0x24,
  NEG:           0x25,

  EQ:            0x28,
  NEQ:           0x29,
  LT:            0x2A,
  GT:            0x2B,
  LTE:           0x2C,
  GTE:           0x2D,

  AND:           0x30,
  OR:            0x31,
  NOT:           0x32,

  JMP:           0x38,
  JMP_IF:        0x39,
  JMP_UNLESS:    0x3A,
  CALL:          0x3B,
  CALL_DYN:      0x3C,
  RET:           0x3D,
  TAIL_CALL:     0x3E,
  TAIL_CALL_DYN: 0x3F,

  QUOTE:         0x40,
  CLOSURE:       0x41,

  LOCAL_GET:     0x48,
  LOCAL_SET:     0x49,

  LIST_NEW:      0x50,
  LIST_LEN:      0x51,
  LIST_HEAD:     0x52,
  LIST_TAIL:     0x53,
  LIST_CONS:     0x54,
  LIST_CAT:      0x55,
  LIST_IDX:      0x56,
  LIST_REV:      0x57,
  TUPLE_NEW:     0x58,
  TUPLE_GET:     0x59,
  RECORD_NEW:    0x5A,
  RECORD_GET:    0x5B,
  RECORD_SET:    0x5C,
  UNION_NEW:     0x5E,
  VARIANT_TAG:   0x5F,
  VARIANT_FIELD: 0x60,
  TUPLE_GET_DYN: 0x61,

  STR_CAT:       0x62,
  STR_LEN:       0x63,
  STR_SPLIT:     0x64,
  STR_JOIN:      0x65,
  STR_TRIM:      0x66,
  TO_STR:        0x67,

  HANDLE_PUSH:   0x70,
  HANDLE_POP:    0x71,
  EFFECT_PERFORM:0x72,
  RESUME:        0x73,
  RESUME_DISCARD:0x74,

  IO_PRINT:      0x90,

  HALT:          0xF0,
  TRAP:          0xF1,
  DEBUG:         0xF2,
} as const;

// Builtin function names → direct opcodes (no CALL overhead)
const BUILTIN_OPS: Record<string, number[]> = {
  add:        [Op.ADD],
  sub:        [Op.SUB],
  mul:        [Op.MUL],
  div:        [Op.DIV],
  mod:        [Op.MOD],
  negate:     [Op.NEG],
  eq:         [Op.EQ],
  neq:        [Op.NEQ],
  lt:         [Op.LT],
  gt:         [Op.GT],
  lte:        [Op.LTE],
  gte:        [Op.GTE],
  not:        [Op.NOT],
  "str.cat":  [Op.STR_CAT],
  show:       [Op.TO_STR],
  print:      [Op.IO_PRINT],
  len:        [Op.LIST_LEN],
  head:       [Op.LIST_HEAD],
  tail:       [Op.LIST_TAIL],
  cons:       [Op.LIST_CONS],
  cat:        [Op.LIST_CAT],
  rev:        [Op.LIST_REV],
  split:      [Op.STR_SPLIT],
  join:       [Op.STR_JOIN],
  trim:       [Op.STR_TRIM],
  "tuple.get":[Op.TUPLE_GET_DYN],
  get:         [Op.LIST_IDX],
  fst:         [Op.TUPLE_GET, 0],
  snd:         [Op.TUPLE_GET, 1],
  "str.split": [Op.STR_SPLIT],
  "str.join":  [Op.STR_JOIN],
  "str.trim":  [Op.STR_TRIM],
};

// Builtin functions dispatched by the VM at runtime (word IDs 1-255)
// Used for higher-order builtins and operations needing runtime loops
const VM_BUILTINS: Record<string, number> = {
  map:    1,
  filter: 2,
  fold:   3,
  "flat-map": 4,
  range:  5,
  zip:    6,
};

// ── Bytecode module (in-memory representation) ──

export type BytecodeWord = {
  name: string;
  wordId: number;
  code: number[];
  localCount: number;
  isPublic: boolean;
};

export type BytecodeModule = {
  words: BytecodeWord[];
  strings: string[];
  rationals: number[];
  variantNames: string[];  // maps variant tag → name for display
  entryWordId: number | null;
};

// ── Code emitter ──

class CodeEmitter {
  code: number[] = [];

  get pos(): number { return this.code.length; }

  emit(op: number): void { this.code.push(op); }

  emitU8(op: number, val: number): void {
    this.code.push(op, val & 0xFF);
  }

  emitU16(op: number, val: number): void {
    this.code.push(op, (val >> 8) & 0xFF, val & 0xFF);
  }

  emitU32(op: number, val: number): void {
    this.code.push(op, (val >> 24) & 0xFF, (val >> 16) & 0xFF, (val >> 8) & 0xFF, val & 0xFF);
  }

  // Emit a placeholder u16 jump offset, return the patch position
  emitJumpPlaceholder(op: number): number {
    this.code.push(op);
    const patch = this.code.length;
    this.code.push(0, 0);
    return patch;
  }

  // Patch a jump placeholder with the offset from patch location to current pos
  patchJump(patch: number): void {
    const offset = this.code.length - patch - 2; // offset is relative to after the operand
    this.code[patch] = (offset >> 8) & 0xFF;
    this.code[patch + 1] = offset & 0xFF;
  }
}

// ── Local slot allocator (per function scope) ──

class LocalScope {
  private slots: Map<string, number> = new Map();
  private nextSlot = 0;

  allocate(name: string): number {
    const slot = this.nextSlot++;
    this.slots.set(name, slot);
    return slot;
  }

  get(name: string): number | undefined {
    return this.slots.get(name);
  }

  get count(): number { return this.nextSlot; }

  // Create a child scope (for match arms, etc.) that shares the same slot space
  child(): LocalScope {
    const c = new LocalScope();
    c.slots = new Map(this.slots);
    c.nextSlot = this.nextSlot;
    return c;
  }
}

// ── Compiler ──

export class Compiler {
  private strings: string[] = [];
  private stringIndex: Map<string, number> = new Map();
  private rationals: number[] = [];
  private rationalIndex: Map<number, number> = new Map();
  private words: BytecodeWord[] = [];
  private wordIds: Map<string, number> = new Map();
  private nextWordId = 256; // 0-255 reserved for builtins
  // Tracks resume continuation variables (name → local slot) during handler compilation
  private resumeVars: Map<string, number> = new Map();

  // Deferred lambda bodies: collected during compilation, emitted as separate words
  private lambdaBodies: { name: string; params: Param[]; body: Expr; captures: string[]; parentScope: LocalScope }[] = [];

  // Variant constructor info: name → { tag, arity }
  private variantInfo: Map<string, { tag: number; arity: number }> = new Map();
  private variantNames: string[] = [];
  private nextVariantTag = 0;

  // Effect operation info: op name → declared arity
  private effectOps: Map<string, number> = new Map();

  private internString(s: string): number {
    const existing = this.stringIndex.get(s);
    if (existing !== undefined) return existing;
    const idx = this.strings.length;
    this.strings.push(s);
    this.stringIndex.set(s, idx);
    return idx;
  }

  private internRational(r: number): number {
    const existing = this.rationalIndex.get(r);
    if (existing !== undefined) return existing;
    const idx = this.rationals.length;
    this.rationals.push(r);
    this.rationalIndex.set(r, idx);
    return idx;
  }

  private allocWordId(name: string): number {
    const existing = this.wordIds.get(name);
    if (existing !== undefined) return existing;
    const id = this.nextWordId++;
    this.wordIds.set(name, id);
    return id;
  }

  compile(program: Program): BytecodeModule {
    // Register VM builtin word IDs (reserved range 0-255)
    for (const [name, id] of Object.entries(VM_BUILTINS)) {
      this.wordIds.set(name, id);
    }

    // Synthesize wrapper words for BUILTIN_OPS so they can be used as values
    // (e.g., fold(xs, 0, add) where add is passed as a higher-order function)
    let nextBuiltinWordId = 10;
    for (const [name, ops] of Object.entries(BUILTIN_OPS)) {
      if (!this.wordIds.has(name)) {
        const id = nextBuiltinWordId++;
        this.wordIds.set(name, id);
        this.words.push({
          name,
          wordId: id,
          code: [...ops, Op.RET],
          localCount: 0,
          isPublic: false,
        });
      }
    }

    // Pre-register built-in effect operations
    this.effectOps.set("raise", 1); // exn.raise : Str -> a

    // First pass: allocate word IDs for all definitions, register variants and effect ops
    for (const tl of program.topLevels) {
      if (tl.tag === "definition") {
        this.allocWordId(tl.name);
      } else if (tl.tag === "type-decl") {
        for (const variant of tl.variants) {
          const tag = this.nextVariantTag++;
          this.variantInfo.set(variant.name, { tag, arity: variant.fields.length });
          this.variantNames[tag] = variant.name;
        }
      } else if (tl.tag === "effect-decl") {
        for (const op of tl.ops) {
          // Parser normalizes () -> T to arity-0 (empty params array)
          this.effectOps.set(op.name, op.sig.params.length);
        }
      } else if (tl.tag === "use-decl") {
        // Wire up aliased imports: map alias → same word ID as original
        for (const imp of tl.imports) {
          if (imp.alias) {
            const originalId = this.wordIds.get(imp.name);
            if (originalId !== undefined) {
              this.wordIds.set(imp.alias, originalId);
            }
            // Also alias variant constructors
            const vinfo = this.variantInfo.get(imp.name);
            if (vinfo !== undefined) {
              this.variantInfo.set(imp.alias, vinfo);
            }
          }
        }
      }
    }

    // Second pass: compile each definition
    for (const tl of program.topLevels) {
      this.compileTopLevel(tl);
    }

    // Process any deferred lambda bodies
    this.flushLambdaBodies();

    const mainId = this.wordIds.get("main") ?? null;
    return {
      words: this.words,
      strings: this.strings,
      rationals: this.rationals,
      variantNames: this.variantNames,
      entryWordId: mainId,
    };
  }

  private compileTopLevel(tl: TopLevel): void {
    switch (tl.tag) {
      case "definition": {
        const wordId = this.wordIds.get(tl.name)!;
        const emitter = new CodeEmitter();
        const scope = new LocalScope();

        // Allocate parameter slots in forward order (a=0, b=1, ...)
        const params = tl.sig.params;
        for (const p of params) {
          scope.allocate(p.name);
        }
        // Prologue: pop args in reverse order (last arg on top of stack)
        for (let i = params.length - 1; i >= 0; i--) {
          emitter.emitU8(Op.LOCAL_SET, scope.get(params[i].name)!);
        }

        // Compile body in tail position
        this.compileExpr(tl.body, emitter, scope, true);
        emitter.emit(Op.RET);

        this.words.push({
          name: tl.name,
          wordId,
          code: emitter.code,
          localCount: scope.count,
          isPublic: tl.pub,
        });
        break;
      }
      // Type/effect/module/use declarations don't produce bytecode
      case "type-decl":
      case "effect-decl":
      case "effect-alias":
      case "mod-decl":
      case "use-decl":
      case "test-decl":
        break;
    }
  }

  private compileExpr(expr: Expr, e: CodeEmitter, scope: LocalScope, tail: boolean): void {
    switch (expr.tag) {
      case "literal":
        this.compileLiteral(expr.value, e);
        break;

      case "var": {
        const slot = scope.get(expr.name);
        if (slot !== undefined) {
          e.emitU8(Op.LOCAL_GET, slot);
        } else {
          // Check for 0-arity variant constructor
          const vinfo = this.variantInfo.get(expr.name);
          if (vinfo !== undefined && vinfo.arity === 0) {
            e.emitU8(Op.UNION_NEW, vinfo.tag);
            e.code.push(0); // arity = 0
          } else {
            // Could be a reference to a top-level function — push as QUOTE
            const wordId = this.wordIds.get(expr.name);
            if (wordId !== undefined) {
              e.emitU16(Op.QUOTE, wordId);
            } else {
              // Unknown variable — emit a CALL placeholder (might be a builtin used as value)
              const strIdx = this.internString(expr.name);
              e.emitU16(Op.PUSH_STR, strIdx);
            }
          }
        }
        break;
      }

      case "let": {
        // Evaluate value
        this.compileExpr(expr.value, e, scope, false);
        // Assign to local
        const slot = scope.allocate(expr.name);
        e.emitU8(Op.LOCAL_SET, slot);
        // Compile body (or just leave unit)
        if (expr.body) {
          this.compileExpr(expr.body, e, scope, tail);
        } else {
          // Top-level let with no body: result is unit
          e.emit(Op.PUSH_UNIT);
        }
        break;
      }

      case "if": {
        this.compileExpr(expr.cond, e, scope, false);
        const elsePatch = e.emitJumpPlaceholder(Op.JMP_UNLESS);
        this.compileExpr(expr.then, e, scope, tail);
        const endPatch = e.emitJumpPlaceholder(Op.JMP);
        e.patchJump(elsePatch);
        this.compileExpr(expr.else, e, scope, tail);
        e.patchJump(endPatch);
        break;
      }

      case "apply":
        this.compileApply(expr, e, scope, tail);
        break;

      case "lambda":
        this.compileLambda(expr, e, scope);
        break;

      case "match":
        this.compileMatch(expr, e, scope, tail);
        break;

      case "list": {
        for (const el of expr.elements) {
          this.compileExpr(el, e, scope, false);
        }
        e.emitU8(Op.LIST_NEW, expr.elements.length);
        break;
      }

      case "tuple": {
        for (const el of expr.elements) {
          this.compileExpr(el, e, scope, false);
        }
        e.emitU8(Op.TUPLE_NEW, expr.elements.length);
        break;
      }

      case "record": {
        for (const f of expr.fields) {
          this.compileExpr(f.value, e, scope, false);
        }
        e.emitU8(Op.RECORD_NEW, expr.fields.length);
        // Emit field name string indices inline after the instruction
        for (const f of expr.fields) {
          const nameIdx = this.internString(f.name);
          e.code.push((nameIdx >> 8) & 0xFF, nameIdx & 0xFF);
        }
        break;
      }

      case "field-access": {
        // Check for dotted builtin (e.g. str.cat, tuple.get)
        if (expr.object.tag === "var") {
          const dottedName = `${expr.object.name}.${expr.field}`;
          if (dottedName in BUILTIN_OPS) {
            // This is a reference to a dotted builtin used as a value (e.g., passed to map)
            // Push as a string for now (won't work for all cases, but handles value references)
            const strIdx = this.internString(dottedName);
            e.emitU16(Op.PUSH_STR, strIdx);
            break;
          }
        }
        this.compileExpr(expr.object, e, scope, false);
        const fieldId = this.internString(expr.field);
        e.emitU16(Op.RECORD_GET, fieldId);
        break;
      }

      case "record-update": {
        // Compile base record
        this.compileExpr(expr.base, e, scope, false);
        // For each updated field: push new value, swap so record is on top, RECORD_SET
        for (const f of expr.fields) {
          this.compileExpr(f.value, e, scope, false);
          e.emit(Op.SWAP);
          const fieldId = this.internString(f.name);
          e.emitU16(Op.RECORD_SET, fieldId);
        }
        break;
      }

      case "handle":
        this.compileHandle(expr, e, scope, tail);
        break;

      case "perform":
        this.compilePerform(expr, e, scope);
        break;

      // These should have been desugared
      case "pipeline":
      case "infix":
      case "unary":
      case "do":
      case "for":
      case "range":
        throw new Error(`Compiler: unexpected sugared node '${expr.tag}' — run desugar first`);

      default: {
        const _: never = expr;
        throw new Error(`Compiler: unknown node tag '${(expr as any).tag}'`);
      }
    }
  }

  private compileLiteral(lit: { tag: string; value?: any }, e: CodeEmitter): void {
    switch (lit.tag) {
      case "int": {
        const v = lit.value;
        if (v >= 0 && v <= 255) {
          e.emitU8(Op.PUSH_INT, v);
        } else if (v >= 0 && v <= 0xFFFF) {
          e.emitU16(Op.PUSH_INT16, v);
        } else {
          e.emitU32(Op.PUSH_INT32, v);
        }
        break;
      }
      case "rat": {
        const idx = this.internRational(lit.value);
        e.emitU32(Op.PUSH_RAT, idx);
        break;
      }
      case "bool":
        e.emit(lit.value ? Op.PUSH_TRUE : Op.PUSH_FALSE);
        break;
      case "str": {
        const idx = this.internString(lit.value);
        e.emitU16(Op.PUSH_STR, idx);
        break;
      }
      case "unit":
        e.emit(Op.PUSH_UNIT);
        break;
    }
  }

  private compileApply(expr: Extract<Expr, { tag: "apply" }>, e: CodeEmitter, scope: LocalScope, tail: boolean): void {
    // Check if calling a resume continuation
    if (expr.fn.tag === "var" && this.resumeVars.has(expr.fn.name)) {
      if (expr.args.length > 0) {
        this.compileExpr(expr.args[0], e, scope, false);
      } else {
        e.emit(Op.PUSH_UNIT);
      }
      const kSlot = this.resumeVars.get(expr.fn.name)!;
      e.emitU8(Op.LOCAL_GET, kSlot);
      e.emit(Op.RESUME);
      return;
    }

    if (expr.fn.tag === "var") {
      // Check if calling a known effect operation (emit EFFECT_PERFORM)
      if (this.effectOps.has(expr.fn.name) && !scope.get(expr.fn.name)) {
        const effectId = this.internString(expr.fn.name);
        for (const arg of expr.args) {
          this.compileExpr(arg, e, scope, false);
        }
        e.emit(Op.EFFECT_PERFORM);
        e.code.push((effectId >> 8) & 0xFF, effectId & 0xFF);
        e.code.push(expr.args.length & 0xFF);
        return;
      }

      // Check if calling a variant constructor
      const vinfo = this.variantInfo.get(expr.fn.name);
      if (vinfo !== undefined && vinfo.arity > 0) {
        for (const arg of expr.args) {
          this.compileExpr(arg, e, scope, false);
        }
        e.emitU8(Op.UNION_NEW, vinfo.tag);
        e.code.push(vinfo.arity & 0xFF);
        return;
      }

      // Check if calling a builtin that maps to a direct opcode
      if (expr.fn.name in BUILTIN_OPS) {
        const ops = BUILTIN_OPS[expr.fn.name];
        for (const arg of expr.args) {
          this.compileExpr(arg, e, scope, false);
        }
        for (const op of ops) {
          e.emit(op);
        }
        return;
      }

      // Check if calling a known word by name
      const wordId = this.wordIds.get(expr.fn.name);
      if (wordId !== undefined) {
        for (const arg of expr.args) {
          this.compileExpr(arg, e, scope, false);
        }
        if (tail) {
          e.emitU16(Op.TAIL_CALL, wordId);
        } else {
          e.emitU16(Op.CALL, wordId);
        }
        return;
      }
    }

    // Check for dotted builtin calls (e.g. str.cat(a, b), tuple.get(t, 0))
    if (expr.fn.tag === "field-access" && expr.fn.object.tag === "var") {
      const dottedName = `${expr.fn.object.name}.${expr.fn.field}`;
      if (dottedName in BUILTIN_OPS) {
        const ops = BUILTIN_OPS[dottedName];
        for (const arg of expr.args) {
          this.compileExpr(arg, e, scope, false);
        }
        for (const op of ops) {
          e.emit(op);
        }
        return;
      }
    }

    // Dynamic call: fn is an expression (closure/quote on stack)
    for (const arg of expr.args) {
      this.compileExpr(arg, e, scope, false);
    }
    this.compileExpr(expr.fn, e, scope, false);
    if (tail) {
      e.emit(Op.TAIL_CALL_DYN);
    } else {
      e.emit(Op.CALL_DYN);
    }
  }

  private compileHandle(
    expr: Extract<Expr, { tag: "handle" }>,
    e: CodeEmitter,
    scope: LocalScope,
    tail: boolean,
  ): void {
    // Separate return arm from operation arms
    const returnArm = expr.arms.find(a => a.name === "return") ?? null;
    const opArms = expr.arms.filter(a => a.name !== "return");

    // Emit one HANDLE_PUSH per operation arm, each with its own effectId
    // and handler offset. groupIdx encodes position within the group so the
    // VM can compute the group base depth for continuation capture.
    const handlerPatches: number[] = [];
    // Return-only handlers still need a HANDLE_PUSH/HANDLE_POP pair to
    // bracket the body, so ensure at least one frame when returnArm exists.
    const frameCount = opArms.length > 0 ? opArms.length : (returnArm ? 1 : 0);
    for (let gi = 0; gi < opArms.length; gi++) {
      const armEffectId = this.internString(opArms[gi].name);
      e.emit(Op.HANDLE_PUSH);
      e.code.push((armEffectId >> 8) & 0xFF, armEffectId & 0xFF);
      const patch = e.code.length;
      e.code.push(0, 0); // placeholder for handler_offset
      e.code.push(gi & 0xFF); // groupIdx
      handlerPatches.push(patch);
    }
    if (opArms.length === 0 && returnArm) {
      // Sentinel frame for return-only handler — effectId 0xFFFF (unused)
      e.emit(Op.HANDLE_PUSH);
      e.code.push(0xFF, 0xFF);
      e.code.push(0, 0); // handler_offset (unused — no operation clause)
      e.code.push(0);    // groupIdx
    }

    // Compile body expression (not in tail position — handler wraps it)
    this.compileExpr(expr.expr, e, scope, false);

    // HANDLE_POP — body completed normally, remove all handler frames
    for (let gi = 0; gi < frameCount; gi++) {
      e.emit(Op.HANDLE_POP);
    }

    // Return clause: processes the body's normal result
    if (returnArm) {
      const returnScope = scope.child();
      if (returnArm.params.length > 0) {
        const slot = returnScope.allocate(returnArm.params[0].name);
        e.emitU8(Op.LOCAL_SET, slot);
      }
      this.compileExpr(returnArm.body, e, returnScope, tail);
    }
    // If no return clause, body result passes through on stack

    // Jump past handler clauses
    const endPatches: number[] = [];
    endPatches.push(e.emitJumpPlaceholder(Op.JMP));

    // Emit operation handler clauses, one per operation arm
    for (let gi = 0; gi < opArms.length; gi++) {
      const arm = opArms[gi];

      // Patch handler_offset to current position
      const handlerOff = e.code.length;
      e.code[handlerPatches[gi]] = (handlerOff >> 8) & 0xFF;
      e.code[handlerPatches[gi] + 1] = handlerOff & 0xFF;

      const armScope = scope.child();

      // At handler entry, stack has: ...args, continuation (top)
      // Pop continuation first (it's on top)
      if (arm.resumeName) {
        const kSlot = armScope.allocate(arm.resumeName);
        e.emitU8(Op.LOCAL_SET, kSlot);
        this.resumeVars.set(arm.resumeName, kSlot);
      } else {
        e.emit(Op.DROP); // discard continuation
      }

      // Determine how many actual effect args are on the stack
      const opArity = this.effectOps.get(arm.name) ?? arm.params.length;

      // Pop actual effect args in reverse order, pad extra params with unit
      for (let i = arm.params.length - 1; i >= 0; i--) {
        const slot = armScope.allocate(arm.params[i].name);
        if (i >= opArity) {
          // This param exceeds the effect operation's declared arity — push unit
          e.emit(Op.PUSH_UNIT);
        }
        e.emitU8(Op.LOCAL_SET, slot);
      }

      // Compile handler body (not in tail position — result flows to end of handle)
      this.compileExpr(arm.body, e, armScope, false);
      // Jump to end of handle expression (NOT RET — call stack was trimmed)
      endPatches.push(e.emitJumpPlaceholder(Op.JMP));

      // Clean up resume var tracking
      if (arm.resumeName) {
        this.resumeVars.delete(arm.resumeName);
      }
    }

    // Patch all end jumps
    for (const p of endPatches) {
      e.patchJump(p);
    }
  }

  private compilePerform(
    expr: Extract<Expr, { tag: "perform" }>,
    e: CodeEmitter,
    scope: LocalScope,
  ): void {
    if (expr.expr.tag === "apply" && expr.expr.fn.tag === "var") {
      // perform op(args) — structured effect operation
      const opName = expr.expr.fn.name;
      const effectId = this.internString(opName);

      // Compile arguments
      for (const arg of expr.expr.args) {
        this.compileExpr(arg, e, scope, false);
      }

      // EFFECT_PERFORM: op(1) + effect_id(u16) + arg_count(u8)
      e.emit(Op.EFFECT_PERFORM);
      e.code.push((effectId >> 8) & 0xFF, effectId & 0xFF);
      e.code.push(expr.expr.args.length & 0xFF);
    } else {
      // Generic perform: compile expression, then emit EFFECT_PERFORM
      this.compileExpr(expr.expr, e, scope, false);
      e.emit(Op.EFFECT_PERFORM);
      e.code.push(0, 0); // effect_id = 0
      e.code.push(0);    // op_idx = 0
    }
  }

  private compileLambda(expr: Extract<Expr, { tag: "lambda" }>, e: CodeEmitter, scope: LocalScope): void {
    // Find free variables (captures)
    const paramNames = new Set(expr.params.map(p => p.name));
    const freeVars: string[] = [];
    this.findFreeVars(expr.body, paramNames, scope, freeVars);

    // Generate a unique name for the lambda body
    const lambdaName = `__lambda_${this.nextWordId}`;
    const lambdaWordId = this.allocWordId(lambdaName);

    // Defer compilation of the lambda body
    this.lambdaBodies.push({
      name: lambdaName,
      params: expr.params,
      body: expr.body,
      captures: freeVars,
      parentScope: scope,
    });

    if (freeVars.length === 0) {
      // No captures: use QUOTE
      e.emitU16(Op.QUOTE, lambdaWordId);
    } else {
      // Push captured values, then CLOSURE
      for (const v of freeVars) {
        const slot = scope.get(v);
        if (slot !== undefined) {
          e.emitU8(Op.LOCAL_GET, slot);
        }
      }
      // CLOSURE encoding: opcode(1) + u16:code_offset + u8:capture_count
      e.code.push(Op.CLOSURE, (lambdaWordId >> 8) & 0xFF, lambdaWordId & 0xFF, freeVars.length & 0xFF);
    }
  }

  private findFreeVars(expr: Expr, bound: Set<string>, scope: LocalScope, free: string[]): void {
    const seen = new Set<string>();
    const collect = (e: Expr, localBound: Set<string>): void => {
      switch (e.tag) {
        case "var":
          if (!localBound.has(e.name) && scope.get(e.name) !== undefined && !seen.has(e.name)) {
            seen.add(e.name);
            free.push(e.name);
          }
          break;
        case "literal":
          break;
        case "let": {
          collect(e.value, localBound);
          const next = new Set(localBound);
          next.add(e.name);
          if (e.body) collect(e.body, next);
          break;
        }
        case "if":
          collect(e.cond, localBound);
          collect(e.then, localBound);
          collect(e.else, localBound);
          break;
        case "apply":
          collect(e.fn, localBound);
          for (const a of e.args) collect(a, localBound);
          break;
        case "lambda": {
          const inner = new Set(localBound);
          for (const p of e.params) inner.add(p.name);
          collect(e.body, inner);
          break;
        }
        case "match":
          collect(e.subject, localBound);
          for (const arm of e.arms) {
            const armBound = new Set(localBound);
            this.collectPatternVars(arm.pattern, armBound);
            collect(arm.body, armBound);
          }
          break;
        case "list":
        case "tuple":
          for (const el of e.elements) collect(el, localBound);
          break;
        case "record":
          for (const f of e.fields) collect(f.value, localBound);
          break;
        case "field-access":
          collect(e.object, localBound);
          break;
        case "handle":
          collect(e.expr, localBound);
          for (const arm of e.arms) {
            const armBound = new Set(localBound);
            for (const p of arm.params) armBound.add(p.name);
            if (arm.resumeName) armBound.add(arm.resumeName);
            collect(arm.body, armBound);
          }
          break;
        case "perform":
          collect(e.expr, localBound);
          break;
        case "record-update":
          collect(e.base, localBound);
          for (const f of e.fields) collect(f.value, localBound);
          break;
        case "pipeline":
        case "infix":
        case "unary":
        case "do":
        case "for":
        case "range":
          break; // should be desugared
      }
    };
    collect(expr, bound);
  }

  private collectPatternVars(pat: Pattern, bound: Set<string>): void {
    switch (pat.tag) {
      case "p-var":
        bound.add(pat.name);
        break;
      case "p-variant":
        for (const a of pat.args) this.collectPatternVars(a, bound);
        break;
      case "p-tuple":
        for (const el of pat.elements) this.collectPatternVars(el, bound);
        break;
      case "p-literal":
      case "p-wildcard":
        break;
    }
  }

  private flushLambdaBodies(): void {
    // Process lambda bodies iteratively (lambdas can contain lambdas)
    while (this.lambdaBodies.length > 0) {
      const pending = [...this.lambdaBodies];
      this.lambdaBodies = [];

      for (const lambda of pending) {
        const wordId = this.wordIds.get(lambda.name)!;
        const emitter = new CodeEmitter();
        const bodyScope = new LocalScope();

        // Captures are pushed first by CALL_DYN, then args
        // Pop in reverse: args first (reverse), then captures (reverse)
        // Per spec: captured values pushed before args by CALL_DYN

        // Bind captures (popped after args, but pushed before args)
        // Stack at entry: [...captures, ...args] (top = last arg)
        // Pop order: last arg, ..., first arg, last capture, ..., first capture

        // First, allocate slots for captures and params
        // Captures come first in LOCAL_SET order (popped last → set first)
        // Actually per the spec example (§3.6):
        //   LOCAL_SET 1  # factor (captured, pushed first)
        //   LOCAL_SET 0  # x (argument)
        // So args are on top of captures. Pop args first (reverse), then captures (reverse).

        // Allocate param slots first (they get lower indices)
        for (const p of lambda.params) {
          bodyScope.allocate(p.name);
        }
        // Then capture slots
        for (const c of lambda.captures) {
          bodyScope.allocate(c);
        }

        // Emit prologue: pop captures first (on top, pushed by doCallDyn),
        // then args (below captures, pushed by caller before CALL_DYN)
        for (let i = lambda.captures.length - 1; i >= 0; i--) {
          const slot = bodyScope.get(lambda.captures[i])!;
          emitter.emitU8(Op.LOCAL_SET, slot);
        }
        for (let i = lambda.params.length - 1; i >= 0; i--) {
          const slot = bodyScope.get(lambda.params[i].name)!;
          emitter.emitU8(Op.LOCAL_SET, slot);
        }

        // Compile body
        this.compileExpr(lambda.body, emitter, bodyScope, true);
        emitter.emit(Op.RET);

        this.words.push({
          name: lambda.name,
          wordId,
          code: emitter.code,
          localCount: bodyScope.count,
          isPublic: false,
        });
      }
    }
  }

  private compileMatch(
    expr: Extract<Expr, { tag: "match" }>,
    e: CodeEmitter,
    scope: LocalScope,
    tail: boolean,
  ): void {
    // Evaluate subject
    this.compileExpr(expr.subject, e, scope, false);

    // Store subject in a temp local
    const subjectSlot = scope.allocate("__match_subject");
    e.emitU8(Op.LOCAL_SET, subjectSlot);

    const endPatches: number[] = [];

    for (let i = 0; i < expr.arms.length; i++) {
      const arm = expr.arms[i];
      const isLast = i === expr.arms.length - 1;

      // Create a child scope for this arm's bindings
      const armScope = scope.child();

      // Compile pattern test + bindings
      let nextArmPatch: number | null = null;
      if (!isLast) {
        // Emit pattern test; if it fails, jump to next arm
        nextArmPatch = this.compilePatternTest(arm.pattern, subjectSlot, e, armScope);
      } else {
        // Last arm: just bind (no test needed — it's the fallthrough/wildcard)
        this.compilePatternBind(arm.pattern, subjectSlot, e, armScope);
      }

      // Compile arm body
      this.compileExpr(arm.body, e, armScope, tail);

      if (!isLast) {
        endPatches.push(e.emitJumpPlaceholder(Op.JMP));
      }

      if (nextArmPatch !== null) {
        e.patchJump(nextArmPatch);
      }
    }

    // Patch all end jumps
    for (const p of endPatches) {
      e.patchJump(p);
    }
  }

  // Emit pattern test code. Returns patch position for the "fail" jump.
  private compilePatternTest(pat: Pattern, subjectSlot: number, e: CodeEmitter, scope: LocalScope): number {
    switch (pat.tag) {
      case "p-wildcard":
      case "p-var": {
        // Always matches — bind and jump nowhere
        this.compilePatternBind(pat, subjectSlot, e, scope);
        // We need to return a patch that we won't actually use for failure.
        // Since wildcard/var always matches, emit a never-taken jump.
        // Actually, the caller only creates nextArmPatch for non-last arms.
        // For safety, emit a dummy JMP_UNLESS that will never fire.
        e.emit(Op.PUSH_TRUE);
        return e.emitJumpPlaceholder(Op.JMP_UNLESS);
      }

      case "p-literal": {
        e.emitU8(Op.LOCAL_GET, subjectSlot);
        this.compileLiteral(pat.value, e);
        e.emit(Op.EQ);
        return e.emitJumpPlaceholder(Op.JMP_UNLESS);
      }

      case "p-variant": {
        // Test tag
        e.emitU8(Op.LOCAL_GET, subjectSlot);
        e.emit(Op.VARIANT_TAG);
        const vinfo = this.variantInfo.get(pat.name);
        const tagIdx = vinfo ? vinfo.tag : this.internString(pat.name);
        e.emitU8(Op.PUSH_INT, tagIdx);
        e.emit(Op.EQ);
        const failPatch = e.emitJumpPlaceholder(Op.JMP_UNLESS);

        // Bind fields
        for (let i = 0; i < pat.args.length; i++) {
          const argPat = pat.args[i];
          if (argPat.tag === "p-var") {
            e.emitU8(Op.LOCAL_GET, subjectSlot);
            e.emitU8(Op.VARIANT_FIELD, i);
            const slot = scope.allocate(argPat.name);
            e.emitU8(Op.LOCAL_SET, slot);
          } else if (argPat.tag === "p-wildcard") {
            // skip
          }
          // Nested patterns could be handled recursively but keeping it simple
        }

        return failPatch;
      }

      case "p-tuple": {
        // Tuples always match structurally in a well-typed program.
        // Bind the elements recursively.
        for (let i = 0; i < pat.elements.length; i++) {
          const elPat = pat.elements[i];
          if (elPat.tag === "p-var") {
            e.emitU8(Op.LOCAL_GET, subjectSlot);
            e.emitU8(Op.TUPLE_GET, i);
            const slot = scope.allocate(elPat.name);
            e.emitU8(Op.LOCAL_SET, slot);
          } else if (elPat.tag === "p-tuple" || elPat.tag === "p-variant") {
            // Extract element into a temp slot for recursive binding
            e.emitU8(Op.LOCAL_GET, subjectSlot);
            e.emitU8(Op.TUPLE_GET, i);
            const tempSlot = scope.allocate(`__tuple_el_${subjectSlot}_${i}`);
            e.emitU8(Op.LOCAL_SET, tempSlot);
            this.compilePatternBind(elPat, tempSlot, e, scope);
          }
        }
        e.emit(Op.PUSH_TRUE);
        return e.emitJumpPlaceholder(Op.JMP_UNLESS);
      }
    }
  }

  private compilePatternBind(pat: Pattern, subjectSlot: number, e: CodeEmitter, scope: LocalScope): void {
    switch (pat.tag) {
      case "p-var": {
        e.emitU8(Op.LOCAL_GET, subjectSlot);
        const slot = scope.allocate(pat.name);
        e.emitU8(Op.LOCAL_SET, slot);
        break;
      }
      case "p-wildcard":
        break;
      case "p-variant":
        for (let i = 0; i < pat.args.length; i++) {
          const argPat = pat.args[i];
          if (argPat.tag === "p-var") {
            e.emitU8(Op.LOCAL_GET, subjectSlot);
            e.emitU8(Op.VARIANT_FIELD, i);
            const slot = scope.allocate(argPat.name);
            e.emitU8(Op.LOCAL_SET, slot);
          }
        }
        break;
      case "p-tuple":
        for (let i = 0; i < pat.elements.length; i++) {
          const elPat = pat.elements[i];
          if (elPat.tag === "p-var") {
            e.emitU8(Op.LOCAL_GET, subjectSlot);
            e.emitU8(Op.TUPLE_GET, i);
            const slot = scope.allocate(elPat.name);
            e.emitU8(Op.LOCAL_SET, slot);
          } else if (elPat.tag === "p-tuple" || elPat.tag === "p-variant") {
            e.emitU8(Op.LOCAL_GET, subjectSlot);
            e.emitU8(Op.TUPLE_GET, i);
            const tempSlot = scope.allocate(`__tuple_bind_${subjectSlot}_${i}`);
            e.emitU8(Op.LOCAL_SET, tempSlot);
            this.compilePatternBind(elPat, tempSlot, e, scope);
          }
        }
        break;
      case "p-literal":
        break;
    }
  }
}

// ── Public API ──

export function compileProgram(program: Program): BytecodeModule {
  const compiler = new Compiler();
  return compiler.compile(program);
}
