// Clank VM runtime
// Fetch-decode-execute loop for the stack VM bytecode
// Per vm-instruction-set.md and compilation-strategy.md

import { Op, type BytecodeModule, type BytecodeWord } from "./compiler.js";

// ── Tagged Value Representation ──
// Using TypeScript objects instead of bit-packed 64-bit values for clarity.
// The tag discriminates the type; payload carries the data.

export const Tag = {
  INT: 0,
  RAT: 1,
  BOOL: 2,
  STR: 3,
  BYTE: 4,
  UNIT: 5,
  HEAP: 6,
  QUOTE: 7,
} as const;

export type Value =
  | { tag: typeof Tag.INT; value: number }
  | { tag: typeof Tag.RAT; value: number }
  | { tag: typeof Tag.BOOL; value: boolean }
  | { tag: typeof Tag.STR; value: string }
  | { tag: typeof Tag.BYTE; value: number }
  | { tag: typeof Tag.UNIT }
  | { tag: typeof Tag.HEAP; value: HeapObject }
  | { tag: typeof Tag.QUOTE; wordId: number };

export type HeapObject =
  | { kind: "list"; items: Value[] }
  | { kind: "tuple"; items: Value[] }
  | { kind: "record"; fields: Map<string, Value> }
  | { kind: "union"; variantTag: number; fields: Value[] }
  | { kind: "closure"; wordId: number; captures: Value[] }
  | { kind: "continuation"; cont: ContinuationData };

// ── Effect Handler Types ──

type HandlerFrame = {
  effectId: number;          // string table index for the effect operation
  handlerOffset: number;     // absolute code position of handler clauses
  wordId: number;            // word containing the handler code
  dataStackDepth: number;    // data stack depth when handler was installed
  callStackDepth: number;    // call stack depth when handler was installed
  handlerStackDepth: number; // handler stack depth when handler was installed
  locals: Value[];           // snapshot of locals at install time
};

type ContinuationData = {
  dataStack: Value[];        // captured data stack slice
  callStack: CallFrame[];    // captured call frames
  handlerStack: HandlerFrame[]; // captured handler frames (including catching handler)
  ip: number;                // IP to resume at (after EFFECT_PERFORM)
  wordId: number;            // word to resume in
  locals: Value[];           // locals snapshot at perform site
  baseDataDepth: number;     // catching handler's original dataStackDepth
  baseCallDepth: number;     // catching handler's original callStackDepth
  baseHandlerDepth: number;  // catching handler's original handlerStackDepth
};

// ── Value constructors ──

export const Val = {
  int: (n: number): Value => ({ tag: Tag.INT, value: n }),
  rat: (n: number): Value => ({ tag: Tag.RAT, value: n }),
  bool: (b: boolean): Value => ({ tag: Tag.BOOL, value: b }),
  str: (s: string): Value => ({ tag: Tag.STR, value: s }),
  byte: (n: number): Value => ({ tag: Tag.BYTE, value: n }),
  unit: (): Value => ({ tag: Tag.UNIT }),
  list: (items: Value[]): Value => ({ tag: Tag.HEAP, value: { kind: "list", items } }),
  tuple: (items: Value[]): Value => ({ tag: Tag.HEAP, value: { kind: "tuple", items } }),
  record: (fields: Map<string, Value>): Value => ({ tag: Tag.HEAP, value: { kind: "record", fields } }),
  union: (variantTag: number, fields: Value[]): Value => ({ tag: Tag.HEAP, value: { kind: "union", variantTag, fields } }),
  closure: (wordId: number, captures: Value[]): Value => ({ tag: Tag.HEAP, value: { kind: "closure", wordId, captures } }),
  continuation: (cont: ContinuationData): Value => ({ tag: Tag.HEAP, value: { kind: "continuation", cont } }),
  quote: (wordId: number): Value => ({ tag: Tag.QUOTE, wordId }),
};

// ── Call Frame ──

type CallFrame = {
  returnIp: number;         // instruction pointer to return to
  returnWordId: number;     // word we return into
  locals: Value[];          // local variable storage
  stackBase: number;        // data stack pointer at frame entry
};

// ── Trap Error ──

export class VMTrap extends Error {
  constructor(
    public code: string,
    message: string,
    public location?: { module: string; word: string; offset: number },
    public stackSnapshot?: string[],
  ) {
    super(`${code}: ${message}`);
    this.name = "VMTrap";
  }
}

// ── VM ──

const MAX_CALL_DEPTH = 10000;
const MAX_LOCALS = 256;

export class VM {
  private dataStack: Value[] = [];
  private callStack: CallFrame[] = [];
  private handlerStack: HandlerFrame[] = [];
  private wordMap: Map<number, BytecodeWord> = new Map();
  private strings: string[] = [];
  private rationals: number[] = [];
  private variantNames: string[] = [];

  // Current execution state
  private ip = 0;
  private currentWord: BytecodeWord | null = null;
  private halted = false;

  // I/O capture (for testing)
  public stdout: string[] = [];

  constructor(private module: BytecodeModule) {
    for (const word of module.words) {
      this.wordMap.set(word.wordId, word);
    }
    this.strings = module.strings;
    this.rationals = module.rationals;
    this.variantNames = module.variantNames || [];
  }

  // ── Public API ──

  run(): Value | undefined {
    if (this.module.entryWordId === null) {
      throw new VMTrap("E010", "no main word found");
    }
    return this.callWord(this.module.entryWordId);
  }

  callWord(wordId: number): Value | undefined {
    const word = this.wordMap.get(wordId);
    if (!word) {
      throw this.trap("E010", `word ID ${wordId} not found`);
    }

    this.currentWord = word;
    this.ip = 0;
    this.halted = false;

    this.execute();

    return this.dataStack.length > 0 ? this.dataStack[this.dataStack.length - 1] : undefined;
  }

  // ── Fetch-Decode-Execute Loop ──

  private execute(): void {
    while (!this.halted) {
      const word = this.currentWord!;
      const code = word.code;

      if (this.ip >= code.length) {
        // Implicit return at end of code
        if (this.callStack.length === 0) return;
        this.doReturn();
        continue;
      }

      const opcode = code[this.ip++];
      this.dispatch(opcode, code);
    }
  }

  private dispatch(opcode: number, code: number[]): void {
    switch (opcode) {
      // ── Stack Manipulation ──
      case Op.NOP:
        break;

      case Op.DUP:
        this.push(this.peek());
        break;

      case Op.DROP:
        this.pop();
        break;

      case Op.SWAP: {
        const b = this.pop();
        const a = this.pop();
        this.push(b);
        this.push(a);
        break;
      }

      case Op.ROT: {
        const c = this.pop();
        const b = this.pop();
        const a = this.pop();
        this.push(b);
        this.push(c);
        this.push(a);
        break;
      }

      case Op.OVER: {
        const b = this.pop();
        const a = this.peek();
        this.push(b);
        this.push(a);
        break;
      }

      case Op.PICK: {
        const n = this.readU8(code);
        const idx = this.dataStack.length - 1 - n;
        if (idx < 0) throw this.trap("E001", "PICK: stack underflow");
        this.push(this.dataStack[idx]);
        break;
      }

      case Op.ROLL: {
        const n = this.readU8(code);
        const idx = this.dataStack.length - 1 - n;
        if (idx < 0) throw this.trap("E001", "ROLL: stack underflow");
        const [val] = this.dataStack.splice(idx, 1);
        this.push(val);
        break;
      }

      // ── Constants / Literals ──
      case Op.PUSH_INT:
        this.push(Val.int(this.readU8(code)));
        break;

      case Op.PUSH_INT16:
        this.push(Val.int(this.readU16(code)));
        break;

      case Op.PUSH_INT32:
        this.push(Val.int(this.readU32(code)));
        break;

      case Op.PUSH_TRUE:
        this.push(Val.bool(true));
        break;

      case Op.PUSH_FALSE:
        this.push(Val.bool(false));
        break;

      case Op.PUSH_UNIT:
        this.push(Val.unit());
        break;

      case Op.PUSH_STR: {
        const idx = this.readU16(code);
        this.push(Val.str(this.strings[idx]));
        break;
      }

      case Op.PUSH_BYTE:
        this.push(Val.byte(this.readU8(code)));
        break;

      case Op.PUSH_RAT: {
        const idx = this.readU32(code);
        this.push(Val.rat(this.rationals[idx]));
        break;
      }

      // ── Arithmetic ──
      case Op.ADD: this.binaryArith((a, b) => a + b); break;
      case Op.SUB: this.binaryArith((a, b) => a - b); break;
      case Op.MUL: this.binaryArith((a, b) => a * b); break;

      case Op.DIV: {
        const b = this.pop();
        const a = this.pop();
        const bv = this.numericValue(b);
        if (bv === 0) {
          // Perform the raise effect (matching tree-walker semantics)
          if (!this.doRaise("division by zero", code)) {
            throw this.trap("E003", "division by zero");
          }
          break;
        }
        const av = this.numericValue(a);
        // Int / Int -> truncated Int (matching tree-walker semantics)
        if (a.tag === Tag.INT && b.tag === Tag.INT) {
          this.push(Val.int(Math.trunc(av / bv)));
        } else {
          this.push(Val.rat(av / bv));
        }
        break;
      }

      case Op.MOD: {
        const b = this.pop();
        const a = this.pop();
        const bv = this.numericValue(b);
        if (bv === 0) {
          if (!this.doRaise("division by zero (mod)", code)) {
            throw this.trap("E003", "division by zero (mod)");
          }
          break;
        }
        this.push(Val.int(this.numericValue(a) % bv));
        break;
      }

      case Op.NEG: {
        const a = this.pop();
        if (a.tag === Tag.INT) this.push(Val.int(-a.value));
        else if (a.tag === Tag.RAT) this.push(Val.rat(-a.value));
        else throw this.trap("E002", "NEG: expected numeric type");
        break;
      }

      // ── Comparison ──
      case Op.EQ:  this.push(Val.bool(this.valuesEqual(this.pop(), this.pop()))); break;
      case Op.NEQ: this.push(Val.bool(!this.valuesEqual(this.pop(), this.pop()))); break;

      case Op.LT: {
        const b = this.pop();
        const a = this.pop();
        this.push(Val.bool(this.numericValue(a) < this.numericValue(b)));
        break;
      }

      case Op.GT: {
        const b = this.pop();
        const a = this.pop();
        this.push(Val.bool(this.numericValue(a) > this.numericValue(b)));
        break;
      }

      case Op.LTE: {
        const b = this.pop();
        const a = this.pop();
        this.push(Val.bool(this.numericValue(a) <= this.numericValue(b)));
        break;
      }

      case Op.GTE: {
        const b = this.pop();
        const a = this.pop();
        this.push(Val.bool(this.numericValue(a) >= this.numericValue(b)));
        break;
      }

      // ── Logic ──
      case Op.AND: {
        const b = this.popBool();
        const a = this.popBool();
        this.push(Val.bool(a && b));
        break;
      }

      case Op.OR: {
        const b = this.popBool();
        const a = this.popBool();
        this.push(Val.bool(a || b));
        break;
      }

      case Op.NOT:
        this.push(Val.bool(!this.popBool()));
        break;

      // ── Control Flow ──
      case Op.JMP: {
        const offset = this.readU16(code);
        this.ip += offset;
        break;
      }

      case Op.JMP_IF: {
        const offset = this.readU16(code);
        if (this.popBool()) this.ip += offset;
        break;
      }

      case Op.JMP_UNLESS: {
        const offset = this.readU16(code);
        if (!this.popBool()) this.ip += offset;
        break;
      }

      case Op.CALL: {
        const wordId = this.readU16(code);
        this.doCall(wordId);
        break;
      }

      case Op.CALL_DYN: {
        const callee = this.pop();
        this.doCallDyn(callee);
        break;
      }

      case Op.RET: {
        if (this.callStack.length === 0) {
          this.halted = true;
          return;
        }
        this.doReturn();
        break;
      }

      case Op.TAIL_CALL: {
        const wordId = this.readU16(code);
        this.doTailCall(wordId);
        break;
      }

      case Op.TAIL_CALL_DYN: {
        const callee = this.pop();
        this.doTailCallDyn(callee);
        break;
      }

      // ── Quotations and Closures ──
      case Op.QUOTE: {
        const wordId = this.readU16(code);
        this.push(Val.quote(wordId));
        break;
      }

      case Op.CLOSURE: {
        const wordId = this.readU16(code);
        const captureCount = this.readU8(code);
        const captures: Value[] = [];
        // Captures were pushed before the CLOSURE instruction
        // Pop them in reverse (they were pushed in order)
        for (let i = 0; i < captureCount; i++) {
          captures.unshift(this.pop());
        }
        this.push(Val.closure(wordId, captures));
        break;
      }

      // ── Local Variables ──
      case Op.LOCAL_GET: {
        const idx = this.readU8(code);
        const frame = this.currentFrame();
        if (idx >= frame.locals.length) {
          // Extend locals array on first access
          while (frame.locals.length <= idx) {
            frame.locals.push(Val.unit());
          }
        }
        this.push(frame.locals[idx]);
        break;
      }

      case Op.LOCAL_SET: {
        const idx = this.readU8(code);
        const frame = this.currentFrame();
        while (frame.locals.length <= idx) {
          frame.locals.push(Val.unit());
        }
        frame.locals[idx] = this.pop();
        break;
      }

      // ── Heap / Compound Values ──
      case Op.LIST_NEW: {
        const count = this.readU8(code);
        const items: Value[] = [];
        for (let i = 0; i < count; i++) items.unshift(this.pop());
        this.push(Val.list(items));
        break;
      }

      case Op.LIST_LEN: {
        const list = this.popList();
        this.push(Val.int(list.length));
        break;
      }

      case Op.LIST_HEAD: {
        const list = this.popList();
        if (list.length === 0) throw this.trap("E004", "LIST_HEAD: empty list");
        this.push(list[0]);
        break;
      }

      case Op.LIST_TAIL: {
        const list = this.popList();
        if (list.length === 0) throw this.trap("E004", "LIST_TAIL: empty list");
        this.push(Val.list(list.slice(1)));
        break;
      }

      case Op.LIST_CONS: {
        const list = this.popList();
        const elem = this.pop();
        this.push(Val.list([elem, ...list]));
        break;
      }

      case Op.LIST_CAT: {
        const b = this.popList();
        const a = this.popList();
        this.push(Val.list([...a, ...b]));
        break;
      }

      case Op.LIST_IDX: {
        const idx = this.pop();
        const list = this.popList();
        const i = this.numericValue(idx);
        if (i < 0 || i >= list.length) {
          throw this.trap("E004", `LIST_IDX: index ${i} out of bounds (length ${list.length})`);
        }
        this.push(list[i]);
        break;
      }

      case Op.LIST_REV: {
        const list = this.popList();
        this.push(Val.list([...list].reverse()));
        break;
      }

      case Op.TUPLE_NEW: {
        const arity = this.readU8(code);
        const items: Value[] = [];
        for (let i = 0; i < arity; i++) items.unshift(this.pop());
        this.push(Val.tuple(items));
        break;
      }

      case Op.TUPLE_GET: {
        const idx = this.readU8(code);
        const val = this.pop();
        if (val.tag !== Tag.HEAP || val.value.kind !== "tuple") {
          throw this.trap("E002", "TUPLE_GET: expected tuple");
        }
        if (idx >= val.value.items.length) {
          throw this.trap("E004", `TUPLE_GET: index ${idx} out of bounds`);
        }
        this.push(val.value.items[idx]);
        break;
      }

      case Op.RECORD_NEW: {
        const fieldCount = this.readU8(code);
        // Read field name string indices (emitted inline after the instruction)
        const fieldNames: string[] = [];
        for (let i = 0; i < fieldCount; i++) {
          fieldNames.push(this.strings[this.readU16(code)]);
        }
        const fields = new Map<string, Value>();
        const values: Value[] = [];
        for (let i = 0; i < fieldCount; i++) values.unshift(this.pop());
        for (let i = 0; i < fieldCount; i++) {
          fields.set(fieldNames[i], values[i]);
        }
        this.push(Val.record(fields));
        break;
      }

      case Op.RECORD_GET: {
        const fieldId = this.readU16(code);
        const val = this.pop();
        if (val.tag !== Tag.HEAP || val.value.kind !== "record") {
          throw this.trap("E002", "RECORD_GET: expected record");
        }
        // fieldId is a string table index — look up field by name
        const fieldName = this.strings[fieldId];
        const field = val.value.fields.get(fieldName) ?? val.value.fields.get(String(fieldId));
        if (field === undefined) {
          throw this.trap("E002", `RECORD_GET: field '${fieldName}' not found`);
        }
        this.push(field);
        break;
      }

      case Op.RECORD_SET: {
        const fieldId = this.readU16(code);
        const rec = this.pop();
        const val = this.pop();
        if (rec.tag !== Tag.HEAP || rec.value.kind !== "record") {
          throw this.trap("E002", "RECORD_SET: expected record");
        }
        const fieldName = this.strings[fieldId];
        const newFields = new Map(rec.value.fields);
        newFields.set(fieldName, val);
        this.push(Val.record(newFields));
        break;
      }

      case Op.UNION_NEW: {
        const variantTag = this.readU8(code);
        const arity = this.readU8(code);
        const fields: Value[] = [];
        for (let i = 0; i < arity; i++) fields.unshift(this.pop());
        this.push(Val.union(variantTag, fields));
        break;
      }

      case Op.VARIANT_TAG: {
        const val = this.peek();
        if (val.tag !== Tag.HEAP || val.value.kind !== "union") {
          throw this.trap("E002", "VARIANT_TAG: expected union");
        }
        this.push(Val.int(val.value.variantTag));
        break;
      }

      case Op.VARIANT_FIELD: {
        const idx = this.readU8(code);
        const val = this.pop();
        if (val.tag !== Tag.HEAP || val.value.kind !== "union") {
          throw this.trap("E002", "VARIANT_FIELD: expected union");
        }
        if (idx >= val.value.fields.length) {
          throw this.trap("E004", `VARIANT_FIELD: index ${idx} out of bounds`);
        }
        this.push(val.value.fields[idx]);
        break;
      }

      case Op.TUPLE_GET_DYN: {
        const idx = this.pop();
        const val = this.pop();
        if (val.tag !== Tag.HEAP || val.value.kind !== "tuple") {
          throw this.trap("E002", "TUPLE_GET_DYN: expected tuple");
        }
        const i = this.numericValue(idx);
        if (i < 0 || i >= val.value.items.length) {
          throw this.trap("E004", `TUPLE_GET_DYN: index ${i} out of bounds`);
        }
        this.push(val.value.items[i]);
        break;
      }

      // ── String Operations ──
      case Op.STR_CAT: {
        const b = this.popStr();
        const a = this.popStr();
        this.push(Val.str(a + b));
        break;
      }

      case Op.STR_LEN: {
        const s = this.popStr();
        this.push(Val.int([...s].length));
        break;
      }

      case Op.STR_SPLIT: {
        const delim = this.popStr();
        const s = this.popStr();
        this.push(Val.list(s.split(delim).map(p => Val.str(p))));
        break;
      }

      case Op.STR_JOIN: {
        const delim = this.popStr();
        const list = this.popList();
        this.push(Val.str(list.map(v => this.valueToString(v)).join(delim)));
        break;
      }

      case Op.STR_TRIM: {
        const s = this.popStr();
        this.push(Val.str(s.trim()));
        break;
      }

      case Op.TO_STR:
        this.push(Val.str(this.valueToString(this.pop())));
        break;

      // ── I/O ──
      case Op.IO_PRINT: {
        const s = this.popStr();
        this.stdout.push(s);
        this.push(Val.unit());
        break;
      }

      // ── Effect Handlers ──
      case Op.HANDLE_PUSH: {
        const effectId = this.readU16(code);
        const handlerOffset = this.readU16(code);
        const groupIdx = this.readU8(code);
        // groupIdx encodes position within handler group (0 = first op).
        // Group base = current depth minus how many siblings already pushed.
        const groupBase = this.handlerStack.length - groupIdx;
        this.handlerStack.push({
          effectId,
          handlerOffset,
          wordId: this.currentWord!.wordId,
          dataStackDepth: this.dataStack.length,
          callStackDepth: this.callStack.length,
          handlerStackDepth: groupBase,
          locals: [...this.currentFrame().locals],
        });
        break;
      }

      case Op.HANDLE_POP: {
        if (this.handlerStack.length === 0) {
          throw this.trap("E011", "HANDLE_POP: no handler on stack");
        }
        this.handlerStack.pop();
        break;
      }

      case Op.EFFECT_PERFORM: {
        const effectId = this.readU16(code);
        const argCount = this.readU8(code);

        // Pop effect arguments
        const args: Value[] = [];
        for (let i = 0; i < argCount; i++) args.unshift(this.pop());

        if (!this.doEffectPerform(effectId, args)) {
          throw this.trap("E011", `unhandled effect: ${this.strings[effectId] ?? effectId}`);
        }
        break;
      }

      case Op.RESUME: {
        const contVal = this.pop();
        const resumeValue = this.pop();

        if (contVal.tag !== Tag.HEAP || contVal.value.kind !== "continuation") {
          throw this.trap("E002", "RESUME: expected continuation");
        }
        const cont = contVal.value.cont;

        const baseDataDepth = this.dataStack.length;
        const baseCallDepth = this.callStack.length;
        const baseHandlerDepth = this.handlerStack.length;

        // Restore continuation's data stack
        for (const v of cont.dataStack) this.dataStack.push(v);

        // Push resume value (this is what perform "returns")
        this.dataStack.push(resumeValue);

        // Restore call stack frames
        for (const f of cont.callStack) {
          this.callStack.push({ ...f, locals: [...f.locals] });
        }

        // Restore handler stack with adjusted depths
        for (const h of cont.handlerStack) {
          this.handlerStack.push({
            ...h,
            locals: [...h.locals],
            dataStackDepth: baseDataDepth + (h.dataStackDepth - cont.baseDataDepth),
            callStackDepth: baseCallDepth + (h.callStackDepth - cont.baseCallDepth),
            handlerStackDepth: baseHandlerDepth + (h.handlerStackDepth - cont.baseHandlerDepth),
          });
        }

        // Restore execution point
        const resumeWord = this.wordMap.get(cont.wordId);
        if (!resumeWord) throw this.trap("E010", `resume word ${cont.wordId} not found`);
        this.currentWord = resumeWord;
        this.ip = cont.ip;

        // Restore locals at the perform site
        if (cont.callStack.length > 0) {
          // Top frame of restored callStack has perform-site locals (already restored)
        } else {
          // Perform happened in same function as handler — restore locals from snapshot
          this.currentFrame().locals = [...cont.locals];
        }
        break;
      }

      // ── VM Control ──
      case Op.HALT:
        this.halted = true;
        return;

      case Op.TRAP: {
        const errCode = this.readU16(code);
        throw this.trap("E000", `TRAP instruction with code ${errCode}`);
      }

      case Op.DEBUG: {
        const snapshot = this.dataStack.map(v => this.valueToString(v));
        this.stdout.push(JSON.stringify({ debug: snapshot }));
        break;
      }

      default:
        throw this.trap("E000", `unknown opcode 0x${opcode.toString(16).padStart(2, "0")}`);
    }
  }

  // ── Call / Return Mechanics ──

  private doCall(wordId: number): void {
    const target = this.wordMap.get(wordId);
    if (!target) {
      if (wordId < 256) {
        this.dispatchBuiltin(wordId);
        return;
      }
      throw this.trap("E010", `CALL: word ID ${wordId} not found`);
    }

    if (this.callStack.length >= MAX_CALL_DEPTH) {
      throw this.trap("E008", "stack overflow: max call depth exceeded");
    }

    // Save current frame
    this.callStack.push({
      returnIp: this.ip,
      returnWordId: this.currentWord!.wordId,
      locals: this.currentFrame().locals,
      stackBase: this.dataStack.length,
    });

    // Enter new word
    this.currentWord = target;
    this.ip = 0;

    // Create new locals for the frame (reuse the frame's local slot from callStack)
    const frame = this.callStack[this.callStack.length - 1];
    frame.locals = new Array(target.localCount).fill(null).map(() => Val.unit());
  }

  private doReturn(): void {
    const frame = this.callStack.pop()!;
    const parent = this.wordMap.get(frame.returnWordId);
    if (!parent) throw this.trap("E010", `RET: parent word ${frame.returnWordId} not found`);
    this.currentWord = parent;
    this.ip = frame.returnIp;
  }

  private doTailCall(wordId: number): void {
    const target = this.wordMap.get(wordId);
    if (!target) {
      if (wordId < 256) {
        this.dispatchBuiltin(wordId);
        // Tail position: return from current frame after builtin completes
        if (this.callStack.length === 0) {
          this.halted = true;
          return;
        }
        this.doReturn();
        return;
      }
      throw this.trap("E010", `TAIL_CALL: word ID ${wordId} not found`);
    }

    // Reuse current frame — just switch word and reset IP
    this.currentWord = target;
    this.ip = 0;

    // Reset locals for the new word
    const frame = this.currentFrame();
    frame.locals = new Array(target.localCount).fill(null).map(() => Val.unit());
  }

  private doCallDyn(callee: Value): void {
    if (callee.tag === Tag.QUOTE) {
      this.doCall(callee.wordId);
    } else if (callee.tag === Tag.HEAP && callee.value.kind === "closure") {
      const closure = callee.value;
      // Push captures onto the stack (they'll be popped by the lambda's prologue)
      for (const cap of closure.captures) {
        this.push(cap);
      }
      this.doCall(closure.wordId);
    } else {
      throw this.trap("E002", "CALL_DYN: expected quote or closure");
    }
  }

  private doTailCallDyn(callee: Value): void {
    if (callee.tag === Tag.QUOTE) {
      this.doTailCall(callee.wordId);
    } else if (callee.tag === Tag.HEAP && callee.value.kind === "closure") {
      const closure = callee.value;
      for (const cap of closure.captures) {
        this.push(cap);
      }
      this.doTailCall(closure.wordId);
    } else {
      throw this.trap("E002", "TAIL_CALL_DYN: expected quote or closure");
    }
  }

  // ── Builtin Dispatch (word IDs 0-255) ──

  static readonly BUILTIN_MAP = 1;
  static readonly BUILTIN_FILTER = 2;
  static readonly BUILTIN_FOLD = 3;
  static readonly BUILTIN_FLAT_MAP = 4;
  static readonly BUILTIN_RANGE = 5;
  static readonly BUILTIN_ZIP = 6;

  private dispatchBuiltin(wordId: number): void {
    switch (wordId) {
      case VM.BUILTIN_MAP: {
        const fn = this.pop();
        const list = this.popList();
        const results: Value[] = [];
        for (const el of list) {
          results.push(this.callBuiltinFn(fn, [el]));
        }
        this.push(Val.list(results));
        break;
      }

      case VM.BUILTIN_FILTER: {
        const fn = this.pop();
        const list = this.popList();
        const results: Value[] = [];
        for (const el of list) {
          const result = this.callBuiltinFn(fn, [el]);
          if (result.tag === Tag.BOOL && result.value) {
            results.push(el);
          }
        }
        this.push(Val.list(results));
        break;
      }

      case VM.BUILTIN_FOLD: {
        const fn = this.pop();
        const init = this.pop();
        const list = this.popList();
        let acc = init;
        for (const el of list) {
          acc = this.callBuiltinFn(fn, [acc, el]);
        }
        this.push(acc);
        break;
      }

      case VM.BUILTIN_FLAT_MAP: {
        const fn = this.pop();
        const list = this.popList();
        const results: Value[] = [];
        for (const el of list) {
          const inner = this.callBuiltinFn(fn, [el]);
          if (inner.tag !== Tag.HEAP || inner.value.kind !== "list") {
            throw this.trap("E200", `flat-map: function must return a list, got ${this.tagName(inner)}`);
          }
          results.push(...inner.value.items);
        }
        this.push(Val.list(results));
        break;
      }

      case VM.BUILTIN_RANGE: {
        const end = this.numericValue(this.pop());
        const start = this.numericValue(this.pop());
        const items: Value[] = [];
        for (let i = start; i <= end; i++) {
          items.push(Val.int(i));
        }
        this.push(Val.list(items));
        break;
      }

      case VM.BUILTIN_ZIP: {
        const ys = this.popList();
        const xs = this.popList();
        const len = Math.min(xs.length, ys.length);
        const items: Value[] = [];
        for (let i = 0; i < len; i++) {
          items.push(Val.tuple([xs[i], ys[i]]));
        }
        this.push(Val.list(items));
        break;
      }

      default:
        throw this.trap("E010", `unknown builtin word ID ${wordId}`);
    }
  }

  private callBuiltinFn(fn: Value, args: Value[]): Value {
    for (const arg of args) {
      this.push(arg);
    }
    const savedCallDepth = this.callStack.length;
    this.doCallDyn(fn);
    while (!this.halted && this.callStack.length > savedCallDepth) {
      const word = this.currentWord!;
      const code = word.code;
      if (this.ip >= code.length) {
        if (this.callStack.length <= savedCallDepth) break;
        this.doReturn();
        continue;
      }
      const opcode = code[this.ip++];
      this.dispatch(opcode, code);
    }
    return this.pop();
  }

  // ── Frame Access ──

  private currentFrame(): CallFrame {
    if (this.callStack.length > 0) {
      return this.callStack[this.callStack.length - 1];
    }
    // Top-level frame (implicit)
    if (!(this as any)._topFrame) {
      (this as any)._topFrame = {
        returnIp: 0,
        returnWordId: 0,
        locals: new Array(MAX_LOCALS).fill(null).map(() => Val.unit()),
        stackBase: 0,
      };
    }
    return (this as any)._topFrame;
  }

  // ── Operand Reading ──

  private readU8(code: number[]): number {
    return code[this.ip++];
  }

  private readU16(code: number[]): number {
    const hi = code[this.ip++];
    const lo = code[this.ip++];
    return (hi << 8) | lo;
  }

  private readU32(code: number[]): number {
    const b3 = code[this.ip++];
    const b2 = code[this.ip++];
    const b1 = code[this.ip++];
    const b0 = code[this.ip++];
    return ((b3 << 24) | (b2 << 16) | (b1 << 8) | b0) >>> 0;
  }

  // ── Stack Operations ──

  private push(val: Value): void {
    this.dataStack.push(val);
  }

  private pop(): Value {
    if (this.dataStack.length === 0) {
      throw this.trap("E001", "stack underflow");
    }
    return this.dataStack.pop()!;
  }

  private peek(): Value {
    if (this.dataStack.length === 0) {
      throw this.trap("E001", "stack underflow (peek)");
    }
    return this.dataStack[this.dataStack.length - 1];
  }

  private popBool(): boolean {
    const val = this.pop();
    if (val.tag !== Tag.BOOL) {
      throw this.trap("E002", `expected Bool, got ${this.tagName(val)}`);
    }
    return val.value;
  }

  private popStr(): string {
    const val = this.pop();
    if (val.tag !== Tag.STR) {
      throw this.trap("E002", `expected Str, got ${this.tagName(val)}`);
    }
    return val.value;
  }

  private popList(): Value[] {
    const val = this.pop();
    if (val.tag !== Tag.HEAP || val.value.kind !== "list") {
      throw this.trap("E002", `expected List, got ${this.tagName(val)}`);
    }
    return val.value.items;
  }

  private peekList(): Value[] {
    const val = this.peek();
    if (val.tag !== Tag.HEAP || val.value.kind !== "list") {
      throw this.trap("E002", `expected List, got ${this.tagName(val)}`);
    }
    return val.value.items;
  }

  // ── Arithmetic Helpers ──

  private numericValue(v: Value): number {
    if (v.tag === Tag.INT) return v.value;
    if (v.tag === Tag.RAT) return v.value;
    throw this.trap("E002", `expected numeric type, got ${this.tagName(v)}`);
  }

  private binaryArith(fn: (a: number, b: number) => number): void {
    const b = this.pop();
    const a = this.pop();
    const av = this.numericValue(a);
    const bv = this.numericValue(b);
    const result = fn(av, bv);
    // Promote to Rat if either operand is Rat
    if (a.tag === Tag.RAT || b.tag === Tag.RAT) {
      this.push(Val.rat(result));
    } else {
      this.push(Val.int(result));
    }
  }

  // ── Value Equality ──

  private valuesEqual(a: Value, b: Value): boolean {
    if (a.tag !== b.tag) return false;
    switch (a.tag) {
      case Tag.INT: return a.value === (b as typeof a).value;
      case Tag.RAT: return a.value === (b as typeof a).value;
      case Tag.BOOL: return a.value === (b as typeof a).value;
      case Tag.STR: return a.value === (b as typeof a).value;
      case Tag.BYTE: return a.value === (b as typeof a).value;
      case Tag.UNIT: return true;
      case Tag.QUOTE: return a.wordId === (b as typeof a).wordId;
      case Tag.HEAP: {
        const bh = (b as typeof a).value;
        if (a.value.kind !== bh.kind) return false;
        if (a.value.kind === "list" && bh.kind === "list") {
          if (a.value.items.length !== bh.items.length) return false;
          return a.value.items.every((v, i) => this.valuesEqual(v, bh.items[i]));
        }
        if (a.value.kind === "tuple" && bh.kind === "tuple") {
          if (a.value.items.length !== bh.items.length) return false;
          return a.value.items.every((v, i) => this.valuesEqual(v, bh.items[i]));
        }
        if (a.value.kind === "union" && bh.kind === "union") {
          if (a.value.variantTag !== bh.variantTag) return false;
          if (a.value.fields.length !== bh.fields.length) return false;
          return a.value.fields.every((v, i) => this.valuesEqual(v, bh.fields[i]));
        }
        if (a.value.kind === "record" && bh.kind === "record") {
          if (a.value.fields.size !== bh.fields.size) return false;
          for (const [k, v] of a.value.fields) {
            const bv = bh.fields.get(k);
            if (bv === undefined || !this.valuesEqual(v, bv)) return false;
          }
          return true;
        }
        return false;
      }
    }
  }

  // ── Display Helpers ──

  private tagName(v: Value): string {
    switch (v.tag) {
      case Tag.INT: return "Int";
      case Tag.RAT: return "Rat";
      case Tag.BOOL: return "Bool";
      case Tag.STR: return "Str";
      case Tag.BYTE: return "Byte";
      case Tag.UNIT: return "Unit";
      case Tag.QUOTE: return "Quote";
      case Tag.HEAP: return v.value.kind;
    }
  }

  valueToString(v: Value): string {
    switch (v.tag) {
      case Tag.INT: return String(v.value);
      case Tag.RAT: return String(v.value);
      case Tag.BOOL: return v.value ? "true" : "false";
      case Tag.STR: return v.value;
      case Tag.BYTE: return `0x${v.value.toString(16).padStart(2, "0")}`;
      case Tag.UNIT: return "()";
      case Tag.QUOTE: return `<quote:${v.wordId}>`;
      case Tag.HEAP: {
        const obj = v.value;
        switch (obj.kind) {
          case "list": return `[${obj.items.map(i => this.valueToString(i)).join(", ")}]`;
          case "tuple": return `(${obj.items.map(i => this.valueToString(i)).join(", ")})`;
          case "record": {
            const entries = [...obj.fields.entries()].map(([k, v]) => `${k}: ${this.valueToString(v)}`);
            return `{${entries.join(", ")}}`;
          }
          case "union": {
            const name = this.variantNames[obj.variantTag] ?? `variant:${obj.variantTag}`;
            if (obj.fields.length === 0) return name;
            return `${name}(${obj.fields.map(f => this.valueToString(f)).join(", ")})`;
          }
          case "closure": return `<closure:${obj.wordId}>`;
          case "continuation": return `<continuation>`;
        }
      }
    }
  }

  // ── Effect Perform Helper ──
  // Performs an effect at runtime (used by EFFECT_PERFORM opcode and builtins like div).
  // Returns true if a handler was found and dispatched, false otherwise.

  private doEffectPerform(effectId: number, args: Value[]): boolean {
    // Search handler stack for matching effect
    let handlerIdx = -1;
    for (let i = this.handlerStack.length - 1; i >= 0; i--) {
      if (this.handlerStack[i].effectId === effectId) {
        handlerIdx = i;
        break;
      }
    }
    if (handlerIdx === -1) return false;

    const handler = this.handlerStack[handlerIdx];

    // Snapshot current locals before capture
    const performLocals = [...this.currentFrame().locals];

    // Capture continuation: data stack slice from handler depth
    const contDataStack = this.dataStack.splice(handler.dataStackDepth);

    // Capture call stack from handler depth (deep copy locals arrays)
    const contCallStack = this.callStack.splice(handler.callStackDepth).map(f => ({
      ...f,
      locals: [...f.locals],
    }));

    // Capture handler stack from group base onward (includes all sibling handlers)
    const contHandlerStack = this.handlerStack.splice(handler.handlerStackDepth);

    const continuation = Val.continuation({
      dataStack: contDataStack,
      callStack: contCallStack,
      handlerStack: contHandlerStack,
      ip: this.ip,
      wordId: this.currentWord!.wordId,
      locals: performLocals,
      baseDataDepth: handler.dataStackDepth,
      baseCallDepth: handler.callStackDepth,
      baseHandlerDepth: handler.handlerStackDepth,
    });

    // Jump to handler code
    const handlerWord = this.wordMap.get(handler.wordId);
    if (!handlerWord) throw this.trap("E010", `handler word ${handler.wordId} not found`);
    this.currentWord = handlerWord;
    this.ip = handler.handlerOffset;

    // Restore handler's locals
    this.currentFrame().locals = [...handler.locals];

    // Push effect args and continuation onto stack for handler clause
    for (const arg of args) this.push(arg);
    this.push(continuation);
    return true;
  }

  // Perform the built-in "raise" effect. Returns true if handled.
  private doRaise(message: string, _code: number[]): boolean {
    const raiseIdx = this.strings.indexOf("raise");
    if (raiseIdx === -1) return false;
    return this.doEffectPerform(raiseIdx, [Val.str(message)]);
  }

  // ── Error Helpers ──

  private trap(code: string, message: string): VMTrap {
    return new VMTrap(code, message, {
      module: "main",
      word: this.currentWord?.name ?? "unknown",
      offset: this.ip,
    });
  }
}

// ── Public API ──

export function execute(module: BytecodeModule): { result: Value | undefined; stdout: string[] } {
  const vm = new VM(module);
  const result = vm.run();
  return { result, stdout: vm.stdout };
}
