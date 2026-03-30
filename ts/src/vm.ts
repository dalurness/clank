// Clank VM runtime
// Fetch-decode-execute loop for the stack VM bytecode
// Per vm-instruction-set.md and compilation-strategy.md

import { Op, type BytecodeModule, type BytecodeWord } from "./compiler.js";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);

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
  | { kind: "continuation"; cont: ContinuationData }
  | { kind: "future"; taskId: number }
  | { kind: "tvar"; tvar: TVar }
  | { kind: "iterator"; iter: IteratorState }
  | { kind: "ref"; ref: Ref }
  | { kind: "sender"; channel: Channel }
  | { kind: "receiver"; channel: Channel }
  | { kind: "select-set"; arms: SelectArm[] };

// ── Channel (inter-task communication) ──

export type Channel = {
  id: number;
  buffer: Value[];
  capacity: number;             // max buffer size
  senderOpen: boolean;          // false once sender closed
  receiverOpen: boolean;        // false once receiver closed
};

type SelectArm = {
  source: Value;                // the channel receiver or future
  handler: Value;               // closure to call with received value
};

// ── Ref (Mutable Reference Cell) ──

export type Ref = {
  id: number;
  value: Value;                 // current stored value
  closed: boolean;              // true after ref-close
  handleCount: number;          // number of live handles (decremented by REF_CLOSE)
  empty: boolean;               // true when affine value has been taken (take/put protocol)
};

// ── TVar (Software Transactional Memory) ──

export type TVar = {
  id: number;
  version: number;          // monotonic commit counter
  value: Value;             // current committed value
  occupied: boolean;        // for affine take/put
  waitQueue: Set<TxnWaiter>; // tasks blocked on retry
  handleCount: number;      // number of live handles (decremented by REF_CLOSE)
  closed: boolean;          // true after all handles closed
};

type TxnWaiter = {
  wake: () => void;
  woken: boolean;
};

// ── Iterator (Streaming I/O) ──

export type IteratorState = {
  id: number;
  generatorFn: Value;          // closure/quote that yields values
  cleanupFn: Value;            // cleanup function (run on close)
  done: boolean;               // true when generator exhausted
  closed: boolean;             // true after close-iter
  buffer: Value[];             // yielded values not yet consumed
  index: number;               // for list-backed iterators: current position
  source: Value[] | null;      // for list-backed iterators: the source array
  nativeNext?: () => Value | null; // native lazy generator (null = done)
  nativeCleanup?: () => void;  // native cleanup for demand-driven I/O (fd close, etc.)
};

type ReadEntry = {
  tvar: TVar;
  version: number;          // version observed at read time
  value: Value;             // value observed at read time (for consistent snapshot)
};

type WriteEntry = {
  tvar: TVar;
  newValue: Value;
  newOccupied: boolean;
};

type OrElseCheckpoint = {
  readSetLen: number;
  writeSetLen: number;
};

type TxnDescriptor = {
  snapshotVersion: number;
  readSet: ReadEntry[];
  writeSet: WriteEntry[];
  status: "active" | "committed" | "aborted";
  orElseStack: OrElseCheckpoint[];
  abortCount: number;
};

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
  future: (taskId: number): Value => ({ tag: Tag.HEAP, value: { kind: "future", taskId } }),
  tvar: (tvar: TVar): Value => ({ tag: Tag.HEAP, value: { kind: "tvar", tvar } }),
  iter: (iter: IteratorState): Value => ({ tag: Tag.HEAP, value: { kind: "iterator", iter } }),
  ref: (ref: Ref): Value => ({ tag: Tag.HEAP, value: { kind: "ref", ref } }),
  sender: (channel: Channel): Value => ({ tag: Tag.HEAP, value: { kind: "sender", channel } }),
  receiver: (channel: Channel): Value => ({ tag: Tag.HEAP, value: { kind: "receiver", channel } }),
  selectSet: (arms: SelectArm[]): Value => ({ tag: Tag.HEAP, value: { kind: "select-set", arms } }),
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

// ── STM Retry Signal ──
// Thrown to signal a retry within a transaction — not a real error.
class STMRetrySignal extends Error {
  constructor() { super("STM retry"); this.name = "STMRetrySignal"; }
}

// ── Task State (for async scheduler) ──

type TaskStatus = "running" | "suspended" | "completed" | "cancelled" | "failed";

type Task = {
  id: number;
  status: TaskStatus;
  dataStack: Value[];
  callStack: CallFrame[];
  handlerStack: HandlerFrame[];
  ip: number;
  currentWord: BytecodeWord | null;
  topFrame: CallFrame | null;  // top-level frame snapshot
  result?: Value;
  error?: string;
  parentId: number | null;
  groupId: number | null;     // task group this task belongs to
  cancelFlag: boolean;
  shieldDepth: number;        // >0 means cancellation is deferred
  awaiters: number[];         // tasks waiting on this task's future
};

type TaskGroup = {
  id: number;
  parentTaskId: number;
  childTaskIds: Set<number>;
  dataStackDepth: number;     // parent's data stack depth at group entry
};

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
  private dispatchTable: Map<string, Map<string, number>> = new Map();

  // Current execution state
  private ip = 0;
  private currentWord: BytecodeWord | null = null;
  private halted = false;

  // ── Async scheduler state ──
  private tasks: Map<number, Task> = new Map();
  private taskGroups: Map<number, TaskGroup> = new Map();
  private nextTaskId = 1;
  private nextGroupId = 1;
  private currentTaskId = 0;        // 0 = root (no async)
  private activeGroupStack: number[] = []; // stack of group IDs for current task
  private asyncMode = false;        // true once any spawn happens

  // ── Channel state ──
  private nextChannelId = 1;

  // ── STM state ──
  private globalClock = 0;
  private commitLocked = false;  // global commit lock (single-threaded: simple flag)
  private nextTvarId = 1;
  private activeTxn: TxnDescriptor | null = null;  // current transaction descriptor

  // ── Iterator state ──
  private nextIteratorId = 1;

  // ── Ref state ──
  private nextRefId = 1;

  // ── Host function registry (for embedding API) ──
  private hostFunctions: Map<string, (...args: unknown[]) => unknown> = new Map();

  // I/O capture (for testing)
  public stdout: string[] = [];

  constructor(private module: BytecodeModule) {
    for (const word of module.words) {
      this.wordMap.set(word.wordId, word);
    }
    this.strings = module.strings;
    this.rationals = module.rationals;
    this.variantNames = module.variantNames || [];
    this.dispatchTable = module.dispatchTable || new Map();
  }

  // ── Public API ──

  /** Register a host function that can be called via CALL_EXTERN.
   *  Key format: "library::symbol" (e.g. "mylib::greet"). */
  registerHostFunction(key: string, fn: (...args: unknown[]) => unknown): void {
    this.hostFunctions.set(key, fn);
  }

  /** Convert a JS value to a VM value. */
  toVmValue(v: unknown): Value {
    return this.jsToVmValue(v);
  }

  /** Convert a VM value to a JS value. */
  toJsValue(v: Value): unknown {
    return this.vmValueToJs(v);
  }

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

  /** Call a word with arguments pre-pushed onto the data stack. */
  callWordWithArgs(wordId: number, args: Value[]): Value | undefined {
    for (const arg of args) {
      this.push(arg);
    }
    return this.callWord(wordId);
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

      case Op.RECORD_REST: {
        // Create a new record from the source record, excluding N named fields
        const excludeCount = this.readU8(code);
        const excludeNames = new Set<string>();
        for (let i = 0; i < excludeCount; i++) {
          excludeNames.add(this.strings[this.readU16(code)]);
        }
        const src = this.pop();
        if (src.tag !== Tag.HEAP || src.value.kind !== "record") {
          throw this.trap("E002", "RECORD_REST: expected record");
        }
        const restFields = new Map<string, Value>();
        for (const [k, v] of src.value.fields) {
          if (!excludeNames.has(k)) restFields.set(k, v);
        }
        this.push(Val.record(restFields));
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

      // ── Interface Dispatch ──
      case Op.DISPATCH: {
        const methodIdx = this.readU16(code);
        const argCount = this.readU8(code);
        const methodName = this.strings[methodIdx];

        // Peek at the first argument (deepest in the arg group on stack)
        const firstArgPos = this.dataStack.length - argCount;
        const dispatchArg = this.dataStack[firstArgPos];
        const typeTag = this.runtimeTypeTag(dispatchArg);

        const methodImpls = this.dispatchTable.get(methodName);
        if (!methodImpls) {
          throw this.trap("E212", `no impls registered for method '${methodName}'`);
        }
        const wordId = methodImpls.get(typeTag);
        if (wordId === undefined) {
          throw this.trap("E212", `no impl of '${methodName}' for type '${typeTag}'`);
        }

        this.doCall(wordId);
        break;
      }

      // ── FFI ──
      case Op.CALL_EXTERN: {
        const externIdx = this.readU16(code);
        const argCount = this.readU8(code);
        const ext = (this.module.externs ?? [])[externIdx];
        if (!ext) {
          throw this.trap("E800", `unknown extern index ${externIdx}`);
        }
        // Only JS/Node interop supported
        if (ext.host !== "js" && ext.host !== null) {
          throw this.trap("E804", `extern host '${ext.host}' is not supported (only 'js' is available in the Node runtime)`);
        }
        // Collect args from stack (they are in forward order: first arg deepest)
        const args: Value[] = [];
        for (let i = 0; i < argCount; i++) {
          args.unshift(this.pop());
        }
        // Convert VM values to JS values
        const jsArgs = args.map(v => this.vmValueToJs(v));
        try {
          // Check host function registry first
          const hostKey = `${ext.library}::${ext.symbol}`;
          const hostFn = this.hostFunctions.get(hostKey);
          if (hostFn) {
            const result = hostFn(...jsArgs);
            this.push(this.jsToVmValue(result));
          } else {
            let mod: any;
            try {
              mod = require(ext.library);
            } catch {
              throw this.trap("E800", `failed to load extern module '${ext.library}'`);
            }
            const fn = mod[ext.symbol];
            if (typeof fn !== "function") {
              throw this.trap("E800", `'${ext.symbol}' is not a function in module '${ext.library}'`);
            }
            const result = fn(...jsArgs);
            this.push(this.jsToVmValue(result));
          }
        } catch (e: unknown) {
          if (e instanceof VMTrap) throw e;
          throw this.trap("E800", `extern call '${ext.name}' threw: ${e instanceof Error ? e.message : String(e)}`);
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

      // ── Async / Concurrency ──
      case Op.TASK_SPAWN: {
        const closure = this.pop();
        if (closure.tag !== Tag.QUOTE && !(closure.tag === Tag.HEAP && closure.value.kind === "closure")) {
          throw this.trap("E002", "TASK_SPAWN: expected closure or quote");
        }
        this.asyncMode = true;

        // Create a new task
        const taskId = this.nextTaskId++;
        const groupId = this.activeGroupStack.length > 0
          ? this.activeGroupStack[this.activeGroupStack.length - 1]
          : null;

        const task: Task = {
          id: taskId,
          status: "suspended",
          dataStack: [],
          callStack: [],
          handlerStack: [],
          ip: 0,
          currentWord: null,
          topFrame: null,
          parentId: this.currentTaskId,
          groupId,
          cancelFlag: false,
          shieldDepth: 0,
          awaiters: [],
        };

        // Set up the child task's execution: push closure args and set entry point
        if (closure.tag === Tag.QUOTE) {
          const word = this.wordMap.get(closure.wordId);
          if (!word) throw this.trap("E010", `TASK_SPAWN: word ${closure.wordId} not found`);
          task.currentWord = word;
          task.ip = 0;
        } else if (closure.tag === Tag.HEAP && closure.value.kind === "closure") {
          const cls = closure.value;
          const word = this.wordMap.get(cls.wordId);
          if (!word) throw this.trap("E010", `TASK_SPAWN: word ${cls.wordId} not found`);
          task.currentWord = word;
          task.ip = 0;
          // Push captures onto child's data stack
          for (const cap of cls.captures) {
            task.dataStack.push(cap);
          }
        }

        this.tasks.set(taskId, task);

        // Register with task group
        if (groupId !== null) {
          const group = this.taskGroups.get(groupId);
          if (group) group.childTaskIds.add(taskId);
        }

        // Push future onto parent's stack
        this.push(Val.future(taskId));
        break;
      }

      case Op.TASK_AWAIT: {
        const futureVal = this.pop();
        if (futureVal.tag !== Tag.HEAP || futureVal.value.kind !== "future") {
          throw this.trap("E002", "TASK_AWAIT: expected Future");
        }
        const awaitedId = futureVal.value.taskId;
        const awaitedTask = this.tasks.get(awaitedId);

        if (!awaitedTask) {
          throw this.trap("E013", "TASK_AWAIT: future references unknown task");
        }

        // Check cancellation before suspending
        this.checkCancellation();

        // If task hasn't run yet, run it to completion inline
        if (awaitedTask.status === "suspended") {
          this.runTaskToCompletion(awaitedTask);
        }

        if (awaitedTask.status === "completed") {
          this.push(awaitedTask.result ?? Val.unit());
        } else if (awaitedTask.status === "failed") {
          if (!this.doRaise(awaitedTask.error ?? "task failed", code)) {
            throw this.trap("E014", `child task failed: ${awaitedTask.error}`);
          }
        } else if (awaitedTask.status === "cancelled") {
          if (!this.doRaise("task cancelled", code)) {
            throw this.trap("E011", "awaited task was cancelled");
          }
        }
        break;
      }

      case Op.TASK_YIELD: {
        this.checkCancellation();
        // In synchronous simulation, yield is a no-op cancellation check
        this.push(Val.unit());
        break;
      }

      case Op.TASK_SLEEP: {
        this.pop(); // consume milliseconds argument
        this.checkCancellation();
        // In synchronous simulation, sleep is a no-op
        this.push(Val.unit());
        break;
      }

      case Op.TASK_GROUP_ENTER: {
        const groupId = this.nextGroupId++;
        const group: TaskGroup = {
          id: groupId,
          parentTaskId: this.currentTaskId,
          childTaskIds: new Set(),
          dataStackDepth: this.dataStack.length,
        };
        this.taskGroups.set(groupId, group);
        this.activeGroupStack.push(groupId);
        break;
      }

      case Op.TASK_GROUP_EXIT: {
        if (this.activeGroupStack.length === 0) {
          throw this.trap("E000", "TASK_GROUP_EXIT: no active task group");
        }
        const groupId = this.activeGroupStack.pop()!;
        const group = this.taskGroups.get(groupId);
        if (!group) break;

        // Cancel all still-running children
        for (const childId of group.childTaskIds) {
          const child = this.tasks.get(childId);
          if (child && (child.status === "suspended" || child.status === "running")) {
            child.cancelFlag = true;
          }
        }

        // Run children to completion (they'll observe cancellation at yield points)
        this.runGroupChildren(group);

        // Check for child failures
        let firstError: string | undefined;
        for (const childId of group.childTaskIds) {
          const child = this.tasks.get(childId);
          if (child && child.status === "failed" && !firstError) {
            firstError = child.error;
          }
        }

        this.taskGroups.delete(groupId);

        if (firstError) {
          if (!this.doRaise(firstError, code)) {
            throw this.trap("E014", `child task failed: ${firstError}`);
          }
        }
        break;
      }

      // ── Channels ──

      case Op.CHAN_NEW: {
        const capVal = this.pop();
        if (capVal.tag !== Tag.INT) {
          throw this.trap("E002", "CHAN_NEW: expected Int capacity");
        }
        const capacity = capVal.value;
        if (capacity < 0) {
          throw this.trap("E002", "CHAN_NEW: capacity must be non-negative");
        }
        const chan: Channel = {
          id: this.nextChannelId++,
          buffer: [],
          capacity,
          senderOpen: true,
          receiverOpen: true,
        };
        // Push (Sender, Receiver) as a tuple
        this.push(Val.tuple([Val.sender(chan), Val.receiver(chan)]));
        break;
      }

      case Op.CHAN_SEND: {
        const val = this.pop();
        const senderVal = this.pop();
        if (senderVal.tag !== Tag.HEAP || senderVal.value.kind !== "sender") {
          throw this.trap("E002", "CHAN_SEND: expected Sender");
        }
        const chan = senderVal.value.channel;
        if (!chan.receiverOpen) {
          if (!this.doRaise("channel closed", code)) {
            throw this.trap("E012", "CHAN_SEND: channel receiver is closed");
          }
          break;
        }
        if (!chan.senderOpen) {
          if (!this.doRaise("channel closed", code)) {
            throw this.trap("E012", "CHAN_SEND: sender is closed");
          }
          break;
        }
        this.checkCancellation();
        // In synchronous simulation, blocking if full is not possible.
        // We allow unbounded buffering beyond capacity for simplicity.
        chan.buffer.push(val);
        this.push(Val.unit());
        break;
      }

      case Op.CHAN_RECV: {
        const recvVal = this.pop();
        if (recvVal.tag !== Tag.HEAP || recvVal.value.kind !== "receiver") {
          throw this.trap("E002", "CHAN_RECV: expected Receiver");
        }
        const chan = recvVal.value.channel;
        this.checkCancellation();
        if (chan.buffer.length > 0) {
          this.push(chan.buffer.shift()!);
        } else if (!chan.senderOpen) {
          // Channel closed and empty
          if (!this.doRaise("channel closed", code)) {
            throw this.trap("E012", "CHAN_RECV: channel is closed and empty");
          }
        } else {
          // In synchronous simulation, empty channel with open sender
          // means no data yet — treat as closed for now
          if (!this.doRaise("channel empty", code)) {
            throw this.trap("E012", "CHAN_RECV: channel is empty");
          }
        }
        break;
      }

      case Op.CHAN_TRY_RECV: {
        const recvVal = this.pop();
        if (recvVal.tag !== Tag.HEAP || recvVal.value.kind !== "receiver") {
          throw this.trap("E002", "CHAN_TRY_RECV: expected Receiver");
        }
        const chan = recvVal.value.channel;
        if (chan.buffer.length > 0) {
          // Return Some(value) — represented as union variant
          const someTag = this.findVariantTag("Some");
          this.push(Val.union(someTag, [chan.buffer.shift()!]));
        } else {
          // Return None
          const noneTag = this.findVariantTag("None");
          this.push(Val.union(noneTag, []));
        }
        break;
      }

      case Op.CHAN_CLOSE: {
        const endVal = this.pop();
        if (endVal.tag !== Tag.HEAP) {
          throw this.trap("E002", "CHAN_CLOSE: expected Sender or Receiver");
        }
        if (endVal.value.kind === "sender") {
          endVal.value.channel.senderOpen = false;
        } else if (endVal.value.kind === "receiver") {
          endVal.value.channel.receiverOpen = false;
        } else {
          throw this.trap("E002", "CHAN_CLOSE: expected Sender or Receiver");
        }
        this.push(Val.unit());
        break;
      }

      // ── Select ──

      case Op.SELECT_BUILD: {
        const armCount = this.readU8(code);
        if (armCount === 0) {
          throw this.trap("E015", "SELECT_BUILD: zero arms");
        }
        const arms: SelectArm[] = [];
        // Arms are pushed as (source, handler) pairs, bottom to top
        // Pop them in reverse
        const pairs: { source: Value; handler: Value }[] = [];
        for (let i = 0; i < armCount; i++) {
          const handler = this.pop();
          const source = this.pop();
          pairs.unshift({ source, handler });
        }
        for (const p of pairs) {
          arms.push({ source: p.source, handler: p.handler });
        }
        this.push(Val.selectSet(arms));
        break;
      }

      case Op.SELECT_WAIT: {
        const setVal = this.pop();
        if (setVal.tag !== Tag.HEAP || setVal.value.kind !== "select-set") {
          throw this.trap("E002", "SELECT_WAIT: expected SelectSet");
        }
        const arms = setVal.value.arms;
        this.checkCancellation();

        // Try each arm to find one that's ready
        // Shuffle for fairness when multiple are ready
        const indices = arms.map((_, i) => i);
        for (let i = indices.length - 1; i > 0; i--) {
          const j = Math.floor(Math.random() * (i + 1));
          [indices[i], indices[j]] = [indices[j], indices[i]];
        }

        let fired = false;
        for (const idx of indices) {
          const arm = arms[idx];
          const src = arm.source;

          if (src.tag === Tag.HEAP && src.value.kind === "receiver") {
            const chan = src.value.channel;
            if (chan.buffer.length > 0) {
              const val = chan.buffer.shift()!;
              const result = this.callBuiltinFn(arm.handler, [val]);
              this.push(result);
              fired = true;
              break;
            }
          } else if (src.tag === Tag.HEAP && src.value.kind === "future") {
            const task = this.tasks.get(src.value.taskId);
            if (task) {
              if (task.status === "suspended") {
                this.runTaskToCompletion(task);
              }
              if (task.status === "completed") {
                const val = task.result ?? Val.unit();
                const result = this.callBuiltinFn(arm.handler, [val]);
                this.push(result);
                fired = true;
                break;
              }
            }
          } else if (src.tag === Tag.INT) {
            // Timeout arm — in sync simulation, treat as immediately ready
            // (timeout value in ms; we can't actually wait)
            const result = this.callBuiltinFn(arm.handler, [Val.unit()]);
            this.push(result);
            fired = true;
            break;
          }
        }

        if (!fired) {
          // In sync simulation, try running any pending futures
          for (const idx of indices) {
            const arm = arms[idx];
            const src = arm.source;
            if (src.tag === Tag.HEAP && src.value.kind === "receiver") {
              const chan = src.value.channel;
              if (!chan.senderOpen && chan.buffer.length === 0) {
                // Closed and empty — skip
                continue;
              }
            }
          }
          // If still nothing, fall through to timeout or error
          // Check for timeout arms (they always fire in sync mode)
          for (const arm of arms) {
            if (arm.source.tag === Tag.INT) {
              const result = this.callBuiltinFn(arm.handler, [Val.unit()]);
              this.push(result);
              fired = true;
              break;
            }
          }
          if (!fired) {
            throw this.trap("E015", "SELECT_WAIT: no arms ready and no timeout");
          }
        }
        break;
      }

      case Op.TASK_CANCEL_CHECK: {
        const task = this.currentTaskId !== 0 ? this.tasks.get(this.currentTaskId) : null;
        const cancelled = task ? task.cancelFlag && task.shieldDepth === 0 : false;
        this.push(Val.bool(cancelled));
        break;
      }

      case Op.TASK_SHIELD_ENTER: {
        const task = this.currentTaskId !== 0 ? this.tasks.get(this.currentTaskId) : null;
        if (task) task.shieldDepth++;
        break;
      }

      case Op.TASK_SHIELD_EXIT: {
        const task = this.currentTaskId !== 0 ? this.tasks.get(this.currentTaskId) : null;
        if (task && task.shieldDepth > 0) task.shieldDepth--;
        break;
      }

      // ── STM (Software Transactional Memory) ──
      case Op.TVAR_NEW: {
        const initial = this.pop();
        const tvar: TVar = {
          id: this.nextTvarId++,
          version: 0,
          value: initial,
          occupied: true,
          waitQueue: new Set(),
          handleCount: 1,
          closed: false,
        };
        this.push(Val.tvar(tvar));
        break;
      }

      case Op.TVAR_READ: {
        const tvarVal = this.pop();
        if (tvarVal.tag !== Tag.HEAP || tvarVal.value.kind !== "tvar") {
          throw this.trap("E002", "TVAR_READ: expected TVar");
        }
        const tvar = tvarVal.value.tvar;
        if (tvar.closed) {
          throw this.trap("E013", "TVAR_READ: TVar is closed");
        }
        if (this.activeTxn) {
          this.push(this.txnRead(this.activeTxn, tvar));
        } else {
          // Outside transaction: read committed value directly
          this.push(tvar.value);
        }
        break;
      }

      case Op.TVAR_WRITE: {
        const val = this.pop();
        const tvarVal = this.pop();
        if (tvarVal.tag !== Tag.HEAP || tvarVal.value.kind !== "tvar") {
          throw this.trap("E002", "TVAR_WRITE: expected TVar");
        }
        const tvar = tvarVal.value.tvar;
        if (tvar.closed) {
          throw this.trap("E014", "TVAR_WRITE: TVar is closed");
        }
        if (this.activeTxn) {
          this.txnWrite(this.activeTxn, tvar, val);
        } else {
          throw this.trap("E020", "tvar-write outside of atomically block");
        }
        this.push(Val.unit());
        break;
      }

      case Op.TVAR_TAKE: {
        const tvarVal = this.pop();
        if (tvarVal.tag !== Tag.HEAP || tvarVal.value.kind !== "tvar") {
          throw this.trap("E002", "TVAR_TAKE: expected TVar");
        }
        const tvar = tvarVal.value.tvar;
        if (tvar.closed) {
          throw this.trap("E013", "TVAR_TAKE: TVar is closed");
        }
        if (!this.activeTxn) {
          throw this.trap("E020", "tvar-take outside of atomically block");
        }
        this.push(this.txnTake(this.activeTxn, tvar));
        break;
      }

      case Op.TVAR_PUT: {
        const val = this.pop();
        const tvarVal = this.pop();
        if (tvarVal.tag !== Tag.HEAP || tvarVal.value.kind !== "tvar") {
          throw this.trap("E002", "TVAR_PUT: expected TVar");
        }
        const tvar = tvarVal.value.tvar;
        if (tvar.closed) {
          throw this.trap("E014", "TVAR_PUT: TVar is closed");
        }
        if (!this.activeTxn) {
          throw this.trap("E020", "tvar-put outside of atomically block");
        }
        this.txnPut(this.activeTxn, tvar, val);
        this.push(Val.unit());
        break;
      }

      // ── Ref (Mutable Reference Cell) ──

      case Op.REF_NEW: {
        const initial = this.pop();
        const ref: Ref = {
          id: this.nextRefId++,
          value: initial,
          closed: false,
          handleCount: 1,
          empty: false,
        };
        this.push(Val.ref(ref));
        break;
      }

      case Op.REF_READ: {
        // Function-call: (Ref -- value)
        // Affine dispatch: if cell holds affine value, performs take (empties cell)
        const refVal = this.pop();
        if (refVal.tag !== Tag.HEAP || refVal.value.kind !== "ref") {
          throw this.trap("E002", "REF_READ: expected Ref");
        }
        const ref = refVal.value.ref;
        if (ref.closed) {
          throw this.trap("E011", "REF_READ: Ref is closed");
        }
        if (this.isAffineValue(ref.value) || ref.empty) {
          // Affine dispatch: take (empties cell)
          if (ref.empty) {
            throw this.trap("E011", "REF_READ: Ref is empty (affine take on empty cell)");
          }
          const value = ref.value;
          ref.value = Val.unit();
          ref.empty = true;
          this.push(value);
        } else {
          // Unrestricted: non-destructive copy
          this.push(ref.value);
        }
        break;
      }

      case Op.REF_WRITE: {
        // Function-call: (Ref value -- Unit)
        // Affine dispatch: if value is affine, performs put (fills empty cell)
        const newValue = this.pop();
        const refVal = this.pop();
        if (refVal.tag !== Tag.HEAP || refVal.value.kind !== "ref") {
          throw this.trap("E002", "REF_WRITE: expected Ref");
        }
        const ref = refVal.value.ref;
        if (ref.closed) {
          throw this.trap("E012", "REF_WRITE: Ref is closed");
        }
        if (this.isAffineValue(newValue)) {
          // Affine dispatch: put (fills empty cell)
          if (!ref.empty) {
            throw this.trap("E012", "REF_WRITE: Ref is full (affine put on non-empty cell)");
          }
          ref.value = newValue;
          ref.empty = false;
        } else {
          // Unrestricted: overwrite (old value becomes garbage)
          ref.value = newValue;
        }
        this.push(Val.unit());
        break;
      }

      case Op.REF_CAS: {
        // Function-call: (Ref expected new -- (Bool, current))
        const newVal = this.pop();
        const expected = this.pop();
        const refVal = this.pop();
        if (refVal.tag !== Tag.HEAP || refVal.value.kind !== "ref") {
          throw this.trap("E002", "REF_CAS: expected Ref");
        }
        const ref = refVal.value.ref;
        if (ref.closed) {
          throw this.trap("E011", "REF_CAS: Ref is closed");
        }
        const current = ref.value;
        const success = this.valuesEqual(current, expected);
        if (success) {
          ref.value = newVal;
        }
        this.push(Val.tuple([Val.bool(success), current]));
        break;
      }

      case Op.REF_MODIFY: {
        // Function-call: (Ref fn -- new_value)
        const fn = this.pop();
        const refVal = this.pop();
        if (refVal.tag !== Tag.HEAP || refVal.value.kind !== "ref") {
          throw this.trap("E002", "REF_MODIFY: expected Ref");
        }
        const ref = refVal.value.ref;
        if (ref.closed) {
          throw this.trap("E011", "REF_MODIFY: Ref is closed");
        }
        if (this.isAffineValue(ref.value) || ref.empty) {
          throw this.trap("E002", "REF_MODIFY: cannot modify Ref containing affine value (use ref-take/ref-put)");
        }
        const current = ref.value;
        const newVal = this.callBuiltinFn(fn, [current]);
        ref.value = newVal;
        this.push(refVal);  // Ref stays (borrow)
        this.push(newVal);
        break;
      }

      case Op.REF_CLOSE: {
        // Function-call: (Ref -- Unit)
        // Also handles TVar (dispatches on heap kind)
        const handleVal = this.pop();
        if (handleVal.tag !== Tag.HEAP) {
          throw this.trap("E002", "REF_CLOSE: expected Ref or TVar");
        }
        if (handleVal.value.kind === "ref") {
          const ref = handleVal.value.ref;
          if (ref.closed) {
            throw this.trap("E011", "REF_CLOSE: Ref is already closed");
          }
          ref.handleCount--;
          if (ref.handleCount <= 0) {
            ref.closed = true;
          }
        } else if (handleVal.value.kind === "tvar") {
          const tvar = handleVal.value.tvar;
          if (tvar.closed) {
            throw this.trap("E011", "REF_CLOSE: TVar is already closed");
          }
          tvar.handleCount--;
          if (tvar.handleCount <= 0) {
            tvar.closed = true;
          }
        } else {
          throw this.trap("E002", "REF_CLOSE: expected Ref or TVar");
        }
        this.push(Val.unit());
        break;
      }

      // ── Iterator (Streaming I/O) ──
      case Op.ITER_NEW: {
        const cleanupFn = this.pop();
        const generatorFn = this.pop();
        const iter: IteratorState = {
          id: this.nextIteratorId++,
          generatorFn,
          cleanupFn,
          done: false,
          closed: false,
          buffer: [],
          index: 0,
          source: null,
        };
        this.push(Val.iter(iter));
        break;
      }

      case Op.ITER_NEXT: {
        const iterVal = this.pop();
        if (iterVal.tag !== Tag.HEAP || iterVal.value.kind !== "iterator") {
          throw this.trap("E002", "ITER_NEXT: expected Iterator");
        }
        const iter = iterVal.value.iter;
        if (iter.closed) {
          throw this.trap("E017", "ITER_NEXT: iterator is closed");
        }
        // Cancellation check
        this.checkCancellation();
        // List-backed iterator fast path
        if (iter.source !== null) {
          if (iter.index >= iter.source.length) {
            iter.done = true;
            if (!this.doRaise("IterDone", code)) {
              throw this.trap("E016", "iterator exhausted");
            }
            break;
          }
          const value = iter.source[iter.index++];
          this.push(iterVal); // keep iterator on stack (borrow)
          this.push(value);
          break;
        }
        // Buffer-backed iterator
        if (iter.buffer.length > 0) {
          const value = iter.buffer.shift()!;
          this.push(iterVal);
          this.push(value);
          break;
        }
        if (iter.done) {
          if (!this.doRaise("IterDone", code)) {
            throw this.trap("E016", "iterator exhausted");
          }
          break;
        }
        // Call the generator function to get next value
        const result = this.callBuiltinFn(iter.generatorFn, [iterVal]);
        // The generator returns a value — if it signals done, we catch it
        if (result.tag === Tag.HEAP && result.value.kind === "union") {
          // Check for None/IterDone variant
          const varName = this.variantNames[result.value.variantTag] ?? "";
          if (varName === "None" || varName === "IterDone") {
            iter.done = true;
            if (!this.doRaise("IterDone", code)) {
              throw this.trap("E016", "iterator exhausted");
            }
            break;
          }
          // Some(value) variant
          if (varName === "Some" && result.value.fields.length > 0) {
            this.push(iterVal);
            this.push(result.value.fields[0]);
            break;
          }
        }
        // Non-variant result: treat as yielded value directly
        this.push(iterVal);
        this.push(result);
        break;
      }

      case Op.ITER_CLOSE: {
        const iterVal = this.pop();
        if (iterVal.tag !== Tag.HEAP || iterVal.value.kind !== "iterator") {
          throw this.trap("E002", "ITER_CLOSE: expected Iterator");
        }
        const iter = iterVal.value.iter;
        if (!iter.closed) {
          iter.closed = true;
          iter.done = true;
          // Run native cleanup (demand-driven I/O resources)
          if (iter.nativeCleanup) iter.nativeCleanup();
          // Run cleanup function
          if (iter.cleanupFn.tag !== Tag.UNIT) {
            this.callBuiltinFn(iter.cleanupFn, []);
          }
        }
        this.push(Val.unit());
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
      if (wordId < 300) {
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
  static readonly BUILTIN_CMP_INT = 230;
  static readonly BUILTIN_CMP_RAT = 231;
  static readonly BUILTIN_CMP_STR = 232;
  static readonly BUILTIN_SHOW_RECORD = 240;
  static readonly BUILTIN_EQ_RECORD = 241;
  static readonly BUILTIN_CLONE_RECORD = 242;
  static readonly BUILTIN_CMP_RECORD = 243;
  static readonly BUILTIN_DEFAULT_RECORD = 244;
  static readonly BUILTIN_SHOW_LIST = 250;
  static readonly BUILTIN_EQ_LIST = 251;
  static readonly BUILTIN_CLONE_LIST = 252;
  static readonly BUILTIN_SHOW_TUPLE = 253;
  static readonly BUILTIN_EQ_TUPLE = 254;
  static readonly BUILTIN_CLONE_TUPLE = 255;
  static readonly BUILTIN_CMP_LIST = 256;
  static readonly BUILTIN_CMP_TUPLE = 257;
  static readonly BUILTIN_CLONE_REF = 258;
  static readonly BUILTIN_CLONE_TVAR = 259;

  private dispatchBuiltin(wordId: number): void {
    switch (wordId) {
      case VM.BUILTIN_MAP: {
        const fn = this.pop();
        const collection = this.pop();
        // Support both lists and iterators for `for` desugaring
        if (collection.tag === Tag.HEAP && collection.value.kind === "iterator") {
          const src = collection.value.iter;
          if (src.closed) throw this.trap("E017", "map: iterator is closed");
          const results: Value[] = [];
          let v: Value | null;
          while ((v = this.iterNext(src, collection)) !== null) {
            results.push(this.callBuiltinFn(fn, [v]));
          }
          src.done = true; src.closed = true;
          this.push(Val.list(results));
        } else {
          if (collection.tag !== Tag.HEAP || collection.value.kind !== "list") {
            throw this.trap("E002", `expected List or Iterator, got ${this.tagName(collection)}`);
          }
          const list = collection.value.items;
          const results: Value[] = [];
          for (const el of list) {
            results.push(this.callBuiltinFn(fn, [el]));
          }
          this.push(Val.list(results));
        }
        break;
      }

      case VM.BUILTIN_FILTER: {
        const fn = this.pop();
        const collection = this.pop();
        // Support both lists and iterators for `for` desugaring
        if (collection.tag === Tag.HEAP && collection.value.kind === "iterator") {
          const src = collection.value.iter;
          if (src.closed) throw this.trap("E017", "filter: iterator is closed");
          const results: Value[] = [];
          let v: Value | null;
          while ((v = this.iterNext(src, collection)) !== null) {
            const result = this.callBuiltinFn(fn, [v]);
            if (result.tag === Tag.BOOL && result.value) {
              results.push(v);
            }
          }
          src.done = true; src.closed = true;
          this.push(Val.list(results));
        } else {
          if (collection.tag !== Tag.HEAP || collection.value.kind !== "list") {
            throw this.trap("E002", `expected List or Iterator, got ${this.tagName(collection)}`);
          }
          const list = collection.value.items;
          const results: Value[] = [];
          for (const el of list) {
            const result = this.callBuiltinFn(fn, [el]);
            if (result.tag === Tag.BOOL && result.value) {
              results.push(el);
            }
          }
          this.push(Val.list(results));
        }
        break;
      }

      case VM.BUILTIN_FOLD: {
        const fn = this.pop();
        const init = this.pop();
        const collection = this.pop();
        // Support both lists and iterators for `for` desugaring
        if (collection.tag === Tag.HEAP && collection.value.kind === "iterator") {
          const src = collection.value.iter;
          if (src.closed) throw this.trap("E017", "fold: iterator is closed");
          let acc = init;
          let v: Value | null;
          while ((v = this.iterNext(src, collection)) !== null) {
            acc = this.callBuiltinFn(fn, [acc, v]);
          }
          src.done = true; src.closed = true;
          this.push(acc);
        } else {
          if (collection.tag !== Tag.HEAP || collection.value.kind !== "list") {
            throw this.trap("E002", `expected List or Iterator, got ${this.tagName(collection)}`);
          }
          const list = collection.value.items;
          let acc = init;
          for (const el of list) {
            acc = this.callBuiltinFn(fn, [acc, el]);
          }
          this.push(acc);
        }
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

      case 7: { // task-group
        const bodyFn = this.pop();
        // Enter task group
        const groupId = this.nextGroupId++;
        const group: TaskGroup = {
          id: groupId,
          parentTaskId: this.currentTaskId,
          childTaskIds: new Set(),
          dataStackDepth: this.dataStack.length,
        };
        this.taskGroups.set(groupId, group);
        this.activeGroupStack.push(groupId);
        this.asyncMode = true;

        // Call the body function
        let bodyResult: Value;
        let bodyError: VMTrap | null = null;
        try {
          bodyResult = this.callBuiltinFn(bodyFn, []);
        } catch (err) {
          if (err instanceof VMTrap) {
            bodyError = err;
          } else {
            throw err;
          }
          bodyResult = Val.unit();
        }

        // Exit task group
        this.activeGroupStack.pop();

        // Cancel still-running children
        for (const childId of group.childTaskIds) {
          const child = this.tasks.get(childId);
          if (child && child.status === "suspended") {
            child.cancelFlag = true;
          }
        }

        // Run remaining children to completion
        this.runGroupChildren(group);

        // Check for child failures
        let firstError: string | undefined;
        for (const childId of group.childTaskIds) {
          const child = this.tasks.get(childId);
          if (child && child.status === "failed" && !firstError) {
            firstError = child.error;
          }
        }

        this.taskGroups.delete(groupId);

        if (bodyError) throw bodyError;
        if (firstError) {
          throw this.trap("E014", `child task failed: ${firstError}`);
        }

        this.push(bodyResult);
        break;
      }

      case 8: { // shield
        const bodyFn = this.pop();
        const task = this.currentTaskId !== 0 ? this.tasks.get(this.currentTaskId) : null;
        if (task) task.shieldDepth++;
        try {
          const result = this.callBuiltinFn(bodyFn, []);
          if (task) task.shieldDepth--;
          this.push(result);
        } catch (err) {
          if (task) task.shieldDepth--;
          throw err;
        }
        break;
      }

      case 9: { // check-cancel
        const task = this.currentTaskId !== 0 ? this.tasks.get(this.currentTaskId) : null;
        if (task && task.cancelFlag && task.shieldDepth === 0) {
          task.status = "cancelled";
          throw this.trap("E011", "task cancelled");
        }
        this.push(Val.unit());
        break;
      }

      case VM.BUILTIN_CMP_INT:
      case VM.BUILTIN_CMP_RAT:
      case VM.BUILTIN_CMP_STR: {
        const b = this.pop();
        const a = this.pop();
        const av = this.numericOrStrValue(a);
        const bv = this.numericOrStrValue(b);
        // Find variant tags for Lt, Eq_, Gt
        const ltTag = this.findVariantTag("Lt");
        const eqTag = this.findVariantTag("Eq_");
        const gtTag = this.findVariantTag("Gt");
        if (av < bv) {
          this.push(Val.union(ltTag, []));
        } else if (av > bv) {
          this.push(Val.union(gtTag, []));
        } else {
          this.push(Val.union(eqTag, []));
        }
        break;
      }

      case VM.BUILTIN_SHOW_RECORD: {
        const rec = this.pop();
        if (rec.tag !== Tag.HEAP || rec.value.kind !== "record") {
          throw this.trap("E002", "show$Record: expected record");
        }
        const keys = [...rec.value.fields.keys()].sort();
        const parts: string[] = [];
        for (const k of keys) {
          const val = rec.value.fields.get(k)!;
          const shown = this.dispatchMethodSync("show", [val]);
          if (shown.tag !== Tag.STR) {
            throw this.trap("E002", "show$Record: show did not return Str");
          }
          parts.push(`${k}: ${shown.value}`);
        }
        this.push(Val.str(`{${parts.join(", ")}}`));
        break;
      }

      case VM.BUILTIN_EQ_RECORD: {
        const b = this.pop();
        const a = this.pop();
        if (a.tag !== Tag.HEAP || a.value.kind !== "record" ||
            b.tag !== Tag.HEAP || b.value.kind !== "record") {
          this.push(Val.bool(false));
          break;
        }
        if (a.value.fields.size !== b.value.fields.size) {
          this.push(Val.bool(false));
          break;
        }
        let allEq = true;
        for (const [k, av] of a.value.fields) {
          const bv = b.value.fields.get(k);
          if (bv === undefined) { allEq = false; break; }
          const result = this.dispatchMethodSync("eq", [av, bv]);
          if (result.tag !== Tag.BOOL || !result.value) { allEq = false; break; }
        }
        this.push(Val.bool(allEq));
        break;
      }

      case VM.BUILTIN_CLONE_RECORD: {
        const rec = this.pop();
        if (rec.tag !== Tag.HEAP || rec.value.kind !== "record") {
          throw this.trap("E002", "clone$Record: expected record");
        }
        const clonedFields = new Map<string, Value>();
        for (const [k, v] of rec.value.fields) {
          clonedFields.set(k, this.dispatchMethodSync("clone", [v]));
        }
        this.push(Val.record(clonedFields));
        break;
      }

      case VM.BUILTIN_CMP_RECORD: {
        const bRec = this.pop();
        const aRec = this.pop();
        if (aRec.tag !== Tag.HEAP || aRec.value.kind !== "record" ||
            bRec.tag !== Tag.HEAP || bRec.value.kind !== "record") {
          throw this.trap("E002", "cmp$Record: expected records");
        }
        const aKeys = [...aRec.value.fields.keys()].sort();
        const bKeys = [...bRec.value.fields.keys()].sort();
        const ltTag = this.findVariantTag("Lt");
        const eqTag = this.findVariantTag("Eq_");
        const gtTag = this.findVariantTag("Gt");
        // Compare key sets lexicographically first
        const minLen = Math.min(aKeys.length, bKeys.length);
        let cmpResult: Value | null = null;
        for (let i = 0; i < minLen; i++) {
          if (aKeys[i] < bKeys[i]) { cmpResult = Val.union(ltTag, []); break; }
          if (aKeys[i] > bKeys[i]) { cmpResult = Val.union(gtTag, []); break; }
        }
        if (!cmpResult && aKeys.length < bKeys.length) cmpResult = Val.union(ltTag, []);
        if (!cmpResult && aKeys.length > bKeys.length) cmpResult = Val.union(gtTag, []);
        // Same keys: compare values field-by-field
        if (!cmpResult) {
          for (const k of aKeys) {
            const av = aRec.value.fields.get(k)!;
            const bv = bRec.value.fields.get(k)!;
            const r = this.dispatchMethodSync("cmp", [av, bv]);
            if (r.tag === Tag.HEAP && r.value.kind === "union" && r.value.variantTag !== eqTag) {
              cmpResult = r;
              break;
            }
          }
        }
        this.push(cmpResult ?? Val.union(eqTag, []));
        break;
      }

      case VM.BUILTIN_DEFAULT_RECORD: {
        this.pop(); // drop dispatch arg
        this.push(Val.record(new Map()));
        break;
      }

      // ── List show/eq/clone Builtins ──

      case VM.BUILTIN_SHOW_LIST: {
        const lst = this.pop();
        if (lst.tag !== Tag.HEAP || lst.value.kind !== "list") {
          throw this.trap("E002", "show$List: expected list");
        }
        const parts: string[] = [];
        for (const item of lst.value.items) {
          const shown = this.dispatchMethodSync("show", [item]);
          if (shown.tag !== Tag.STR) {
            throw this.trap("E002", "show$List: show did not return Str");
          }
          parts.push(shown.value);
        }
        this.push(Val.str(`[${parts.join(", ")}]`));
        break;
      }

      case VM.BUILTIN_EQ_LIST: {
        const b = this.pop();
        const a = this.pop();
        if (a.tag !== Tag.HEAP || a.value.kind !== "list" ||
            b.tag !== Tag.HEAP || b.value.kind !== "list") {
          this.push(Val.bool(false));
          break;
        }
        if (a.value.items.length !== b.value.items.length) {
          this.push(Val.bool(false));
          break;
        }
        let listEq = true;
        for (let i = 0; i < a.value.items.length; i++) {
          const result = this.dispatchMethodSync("eq", [a.value.items[i], b.value.items[i]]);
          if (result.tag !== Tag.BOOL || !result.value) { listEq = false; break; }
        }
        this.push(Val.bool(listEq));
        break;
      }

      case VM.BUILTIN_CLONE_LIST: {
        const lst = this.pop();
        if (lst.tag !== Tag.HEAP || lst.value.kind !== "list") {
          throw this.trap("E002", "clone$List: expected list");
        }
        const clonedItems: Value[] = [];
        for (const item of lst.value.items) {
          clonedItems.push(this.dispatchMethodSync("clone", [item]));
        }
        this.push(Val.list(clonedItems));
        break;
      }

      // ── Tuple show/eq/clone Builtins ──

      case VM.BUILTIN_SHOW_TUPLE: {
        const tup = this.pop();
        if (tup.tag !== Tag.HEAP || tup.value.kind !== "tuple") {
          throw this.trap("E002", "show$Tuple: expected tuple");
        }
        const parts: string[] = [];
        for (const item of tup.value.items) {
          const shown = this.dispatchMethodSync("show", [item]);
          if (shown.tag !== Tag.STR) {
            throw this.trap("E002", "show$Tuple: show did not return Str");
          }
          parts.push(shown.value);
        }
        this.push(Val.str(`(${parts.join(", ")})`));
        break;
      }

      case VM.BUILTIN_EQ_TUPLE: {
        const b = this.pop();
        const a = this.pop();
        if (a.tag !== Tag.HEAP || a.value.kind !== "tuple" ||
            b.tag !== Tag.HEAP || b.value.kind !== "tuple") {
          this.push(Val.bool(false));
          break;
        }
        if (a.value.items.length !== b.value.items.length) {
          this.push(Val.bool(false));
          break;
        }
        let tupleEq = true;
        for (let i = 0; i < a.value.items.length; i++) {
          const result = this.dispatchMethodSync("eq", [a.value.items[i], b.value.items[i]]);
          if (result.tag !== Tag.BOOL || !result.value) { tupleEq = false; break; }
        }
        this.push(Val.bool(tupleEq));
        break;
      }

      case VM.BUILTIN_CLONE_TUPLE: {
        const tup = this.pop();
        if (tup.tag !== Tag.HEAP || tup.value.kind !== "tuple") {
          throw this.trap("E002", "clone$Tuple: expected tuple");
        }
        const clonedItems: Value[] = [];
        for (const item of tup.value.items) {
          clonedItems.push(this.dispatchMethodSync("clone", [item]));
        }
        this.push(Val.tuple(clonedItems));
        break;
      }

      // ── Ref/TVar clone Builtins ──

      case VM.BUILTIN_CLONE_REF: {
        const refVal = this.pop();
        if (refVal.tag !== Tag.HEAP || refVal.value.kind !== "ref") {
          throw this.trap("E002", "clone$Ref: expected Ref");
        }
        const ref = refVal.value.ref;
        if (ref.closed) {
          throw this.trap("E011", "clone$Ref: Ref is closed");
        }
        ref.handleCount++;
        // Return a new Value pointing to the same Ref object
        this.push(Val.ref(ref));
        break;
      }

      case VM.BUILTIN_CLONE_TVAR: {
        const tvarVal = this.pop();
        if (tvarVal.tag !== Tag.HEAP || tvarVal.value.kind !== "tvar") {
          throw this.trap("E002", "clone$TVar: expected TVar");
        }
        const tvar = tvarVal.value.tvar;
        if (tvar.closed) {
          throw this.trap("E011", "clone$TVar: TVar is closed");
        }
        tvar.handleCount++;
        this.push(Val.tvar(tvar));
        break;
      }

      // ── List/Tuple cmp (Ord) Builtins ──

      case VM.BUILTIN_CMP_LIST: {
        const b = this.pop();
        const a = this.pop();
        if (a.tag !== Tag.HEAP || a.value.kind !== "list" ||
            b.tag !== Tag.HEAP || b.value.kind !== "list") {
          throw this.trap("E002", "cmp$List: expected lists");
        }
        const ltTag = this.findVariantTag("Lt");
        const eqTag = this.findVariantTag("Eq_");
        const gtTag = this.findVariantTag("Gt");
        const minLen = Math.min(a.value.items.length, b.value.items.length);
        let listCmpResult: Value | null = null;
        for (let i = 0; i < minLen; i++) {
          const r = this.dispatchMethodSync("cmp", [a.value.items[i], b.value.items[i]]);
          if (r.tag === Tag.HEAP && r.value.kind === "union" && r.value.variantTag !== eqTag) {
            listCmpResult = r;
            break;
          }
        }
        if (!listCmpResult) {
          if (a.value.items.length < b.value.items.length) listCmpResult = Val.union(ltTag, []);
          else if (a.value.items.length > b.value.items.length) listCmpResult = Val.union(gtTag, []);
        }
        this.push(listCmpResult ?? Val.union(eqTag, []));
        break;
      }

      case VM.BUILTIN_CMP_TUPLE: {
        const b = this.pop();
        const a = this.pop();
        if (a.tag !== Tag.HEAP || a.value.kind !== "tuple" ||
            b.tag !== Tag.HEAP || b.value.kind !== "tuple") {
          throw this.trap("E002", "cmp$Tuple: expected tuples");
        }
        const ltTag = this.findVariantTag("Lt");
        const eqTag = this.findVariantTag("Eq_");
        const gtTag = this.findVariantTag("Gt");
        const minLen = Math.min(a.value.items.length, b.value.items.length);
        let tupleCmpResult: Value | null = null;
        for (let i = 0; i < minLen; i++) {
          const r = this.dispatchMethodSync("cmp", [a.value.items[i], b.value.items[i]]);
          if (r.tag === Tag.HEAP && r.value.kind === "union" && r.value.variantTag !== eqTag) {
            tupleCmpResult = r;
            break;
          }
        }
        if (!tupleCmpResult) {
          if (a.value.items.length < b.value.items.length) tupleCmpResult = Val.union(ltTag, []);
          else if (a.value.items.length > b.value.items.length) tupleCmpResult = Val.union(gtTag, []);
        }
        this.push(tupleCmpResult ?? Val.union(eqTag, []));
        break;
      }

      // ── STM Builtins ──

      case 65: { // atomically
        const bodyFn = this.pop();
        if (this.activeTxn) {
          throw this.trap("E021", "nested atomically is not supported");
        }
        this.push(this.runAtomically(bodyFn));
        break;
      }

      case 66: { // or-else
        const action2 = this.pop();
        const action1 = this.pop();
        if (!this.activeTxn) {
          throw this.trap("E020", "or-else outside of atomically block");
        }
        this.push(this.txnOrElse(this.activeTxn, action1, action2));
        break;
      }

      case 67: { // retry
        if (!this.activeTxn) {
          throw this.trap("E020", "retry outside of atomically block");
        }
        // Check if we're in an or-else branch
        if (this.activeTxn.orElseStack.length > 0) {
          // Signal retry to or-else handler via sentinel error
          throw new STMRetrySignal();
        }
        // Top-level retry: block on read set TVars
        this.txnRetry(this.activeTxn);
        // After wake, the transaction will be restarted by runAtomically loop
        throw new STMRetrySignal();
      }

      // ── Tier 2: HTTP Client ──

      case 120: { // http.get
        const url = this.popStr();
        this.push(this.vmHttpRequest("GET", url, null));
        break;
      }
      case 121: { // http.post
        const body = this.popStr();
        const url = this.popStr();
        this.push(this.vmHttpRequest("POST", url, body));
        break;
      }
      case 122: { // http.put
        const body = this.popStr();
        const url = this.popStr();
        this.push(this.vmHttpRequest("PUT", url, body));
        break;
      }
      case 123: { // http.del
        const url = this.popStr();
        this.push(this.vmHttpRequest("DELETE", url, null));
        break;
      }
      case 124: { // http.patch
        const body = this.popStr();
        const url = this.popStr();
        this.push(this.vmHttpRequest("PATCH", url, body));
        break;
      }
      case 125: { // http.req
        const req = this.pop();
        const fields = this.heapRecord(req);
        const method = this.valStr(fields.get("method") ?? Val.str("GET"));
        const url = this.valStr(fields.get("url") ?? Val.str(""));
        const bodyOpt = fields.get("body");
        const bodyStr = bodyOpt && bodyOpt.tag === Tag.HEAP && (bodyOpt.value as any).kind === "union" && (bodyOpt.value as any).variantTag !== 0
          ? this.valStr((bodyOpt.value as any).fields[0]) : null;
        this.push(this.vmHttpRequest(method, url, bodyStr));
        break;
      }
      case 126: { // http.hdr
        const val = this.popStr();
        const key = this.popStr();
        const req = this.pop();
        const fields = new Map(this.heapRecord(req));
        const headers = fields.get("headers");
        const headerList: Value[] = headers && headers.tag === Tag.HEAP && (headers.value as any).kind === "list"
          ? [...(headers.value as any).items] : [];
        headerList.push(Val.tuple([Val.str(key), Val.str(val)]));
        fields.set("headers", Val.list(headerList));
        this.push(Val.record(fields));
        break;
      }
      case 127: { // http.json
        const res = this.pop();
        const fields = this.heapRecord(res);
        const body = fields.get("body");
        if (!body || body.tag !== Tag.STR) throw this.trap("E300", "http.json: no body");
        try {
          this.push(this.jsonToVmValue(JSON.parse(body.value)));
        } catch {
          throw this.trap("E300", "http.json: invalid JSON");
        }
        break;
      }
      case 128: { // http.ok?
        const res = this.pop();
        const fields = this.heapRecord(res);
        const status = fields.get("status");
        const code = status && status.tag === Tag.INT ? status.value : 0;
        this.push(Val.bool(code >= 200 && code < 300));
        break;
      }

      // ── Tier 2: HTTP Server ──

      case 130: { // srv.new
        this.pop(); // unit arg
        this.push(Val.list([]));
        break;
      }
      case 131: case 132: case 133: case 134: { // srv.get/post/put/del
        const handler = this.pop();
        const path = this.popStr();
        const routes = this.popList();
        const methods: Record<number, string> = { 131: "GET", 132: "POST", 133: "PUT", 134: "DELETE" };
        const method = methods[wordId];
        const route = Val.record(new Map<string, Value>([
          ["method", Val.str(method)],
          ["path", Val.str(path)],
          ["handler", handler],
        ]));
        this.push(Val.list([...routes, route]));
        break;
      }
      case 135: { // srv.start
        const port = this.pop();
        const _routes = this.pop();
        const portVal = port.tag === Tag.INT ? port.value : 0;
        this.push(Val.record(new Map([
          ["port", Val.int(portVal)],
          ["running", Val.bool(true)],
        ])));
        break;
      }
      case 136: { // srv.stop
        this.pop();
        this.push(Val.unit());
        break;
      }
      case 137: { // srv.res
        const body = this.popStr();
        const status = this.pop();
        const statusVal = status.tag === Tag.INT ? status.value : 200;
        this.push(Val.record(new Map([
          ["status", Val.int(statusVal)],
          ["headers", Val.list([])],
          ["body", Val.str(body)],
        ])));
        break;
      }
      case 138: { // srv.json
        const json = this.pop();
        const status = this.pop();
        const statusVal = status.tag === Tag.INT ? status.value : 200;
        this.push(Val.record(new Map([
          ["status", Val.int(statusVal)],
          ["headers", Val.list([Val.tuple([Val.str("Content-Type"), Val.str("application/json")])])],
          ["body", Val.str(this.showValue(json))],
        ])));
        break;
      }
      case 139: { // srv.hdr
        const val = this.popStr();
        const key = this.popStr();
        const res = this.pop();
        const fields = new Map(this.heapRecord(res));
        const headers = fields.get("headers");
        const headerList: Value[] = headers && headers.tag === Tag.HEAP && (headers.value as any).kind === "list"
          ? [...(headers.value as any).items] : [];
        headerList.push(Val.tuple([Val.str(key), Val.str(val)]));
        fields.set("headers", Val.list(headerList));
        this.push(Val.record(fields));
        break;
      }
      case 140: { // srv.mw
        const _mw = this.pop();
        const routes = this.pop();
        this.push(routes); // stub: pass through
        break;
      }

      // ── Tier 2: CSV ──

      case 145: { // csv.dec
        const input = this.popStr();
        this.push(this.vmCsvParse(input));
        break;
      }
      case 146: { // csv.enc
        const rows = this.popList();
        this.push(Val.str(this.vmCsvEncode(rows)));
        break;
      }
      case 147: { // csv.decf
        const path = this.popStr();
        try {
          const { readFileSync } = require("fs");
          const content: string = readFileSync(path, "utf-8");
          this.push(this.vmCsvParse(content));
        } catch (e: any) {
          throw this.trap("E300", `csv.decf: ${e.message}`);
        }
        break;
      }
      case 148: { // csv.encf
        const rows = this.popList();
        const path = this.popStr();
        try {
          const { writeFileSync } = require("fs");
          writeFileSync(path, this.vmCsvEncode(rows), "utf-8");
          this.push(Val.unit());
        } catch (e: any) {
          throw this.trap("E300", `csv.encf: ${e.message}`);
        }
        break;
      }
      case 149: { // csv.hdr
        const rows = this.popList();
        if (rows.length === 0) throw this.trap("E300", "csv.hdr: empty data");
        this.push(rows[0]);
        break;
      }
      case 150: { // csv.rows
        const rows = this.popList();
        if (rows.length === 0) throw this.trap("E300", "csv.rows: empty data");
        this.push(Val.list(rows.slice(1)));
        break;
      }
      case 151: { // csv.maps
        const rows = this.popList();
        if (rows.length === 0) throw this.trap("E300", "csv.maps: empty data");
        const header = rows[0];
        const headerItems = header.tag === Tag.HEAP && (header.value as any).kind === "list"
          ? (header.value as any).items as Value[] : [];
        const keys = headerItems.map((h: Value) => h.tag === Tag.STR ? h.value : "");
        const result: Value[] = [];
        for (let i = 1; i < rows.length; i++) {
          const row = rows[i];
          const items = row.tag === Tag.HEAP && (row.value as any).kind === "list"
            ? (row.value as any).items as Value[] : [];
          const fields = new Map<string, Value>();
          for (let j = 0; j < keys.length; j++) {
            fields.set(keys[j], items[j] ?? Val.str(""));
          }
          result.push(Val.record(fields));
        }
        this.push(Val.list(result));
        break;
      }
      case 152: { // csv.opts
        const input = this.popStr();
        const opts = this.pop();
        const fields = this.heapRecord(opts);
        const delim = this.valStr(fields.get("delim") ?? Val.str(","));
        this.push(this.vmCsvParse(input, delim));
        break;
      }

      // ── Tier 2: Process ──

      case 155: { // proc.run
        const argList = this.popList().map(a => this.valStr(a));
        const cmd = this.popStr();
        try {
          const { spawnSync } = require("child_process");
          const result = spawnSync(cmd, argList, { encoding: "utf-8", timeout: 30000 });
          this.push(Val.record(new Map([
            ["code", Val.int(result.status ?? -1)],
            ["out", Val.str(result.stdout ?? "")],
            ["err", Val.str(result.stderr ?? "")],
          ])));
        } catch (e: any) {
          throw this.trap("E300", `proc.run: ${e.message}`);
        }
        break;
      }
      case 156: { // proc.sh
        const cmd = this.popStr();
        try {
          const { execSync } = require("child_process");
          const out: string = execSync(cmd, { encoding: "utf-8", timeout: 30000 });
          this.push(Val.record(new Map([
            ["code", Val.int(0)],
            ["out", Val.str(out)],
            ["err", Val.str("")],
          ])));
        } catch (e: any) {
          this.push(Val.record(new Map([
            ["code", Val.int(e.status ?? 1)],
            ["out", Val.str(e.stdout ?? "")],
            ["err", Val.str(e.stderr ?? "")],
          ])));
        }
        break;
      }
      case 157: { // proc.ok
        const argList = this.popList().map(a => this.valStr(a));
        const cmd = this.popStr();
        try {
          const { spawnSync } = require("child_process");
          const result = spawnSync(cmd, argList, { encoding: "utf-8", timeout: 30000 });
          if (result.status !== 0) throw this.trap("E300", `proc.ok: exit code ${result.status}`);
          this.push(Val.str(result.stdout ?? ""));
        } catch (e: any) {
          if (e.code) throw e;
          throw this.trap("E300", `proc.ok: ${e.message}`);
        }
        break;
      }
      case 158: { // proc.pipe
        const stdin = this.popStr();
        const argList = this.popList().map(a => this.valStr(a));
        const cmd = this.popStr();
        try {
          const { spawnSync } = require("child_process");
          const result = spawnSync(cmd, argList, { encoding: "utf-8", input: stdin, timeout: 30000 });
          this.push(Val.record(new Map([
            ["code", Val.int(result.status ?? -1)],
            ["out", Val.str(result.stdout ?? "")],
            ["err", Val.str(result.stderr ?? "")],
          ])));
        } catch (e: any) {
          throw this.trap("E300", `proc.pipe: ${e.message}`);
        }
        break;
      }
      case 159: { // proc.bg (stub)
        const _argList = this.popList();
        const _cmd = this.popStr();
        this.push(Val.record(new Map([
          ["pid", Val.int(0)],
          ["_handle", Val.str("stub")],
        ])));
        break;
      }
      case 160: { // proc.wait (stub)
        this.pop();
        this.push(Val.record(new Map([
          ["code", Val.int(0)],
          ["out", Val.str("")],
          ["err", Val.str("")],
        ])));
        break;
      }
      case 161: { // proc.kill (stub)
        this.pop();
        this.push(Val.unit());
        break;
      }
      case 162: { // proc.exit
        const code = this.pop();
        if (typeof process !== "undefined") process.exit(code.tag === Tag.INT ? code.value : 1);
        this.push(Val.unit());
        break;
      }
      case 163: { // proc.pid
        this.pop(); // unit
        this.push(Val.int(typeof process !== "undefined" ? process.pid : 0));
        break;
      }

      // ── Tier 2: DateTime ──

      case 170: { // dt.now
        this.pop(); // unit
        this.push(this.vmDateToRecord(new Date()));
        break;
      }
      case 171: { // dt.unix
        this.pop(); // unit
        this.push(Val.int(Math.floor(Date.now() / 1000)));
        break;
      }
      case 172: { // dt.from
        const ts = this.pop();
        this.push(this.vmDateToRecord(new Date((ts.tag === Tag.INT ? ts.value : 0) * 1000)));
        break;
      }
      case 173: { // dt.to
        const dt = this.pop();
        this.push(Val.int(Math.floor(this.vmRecordToDate(dt).getTime() / 1000)));
        break;
      }
      case 174: { // dt.parse
        const _fmt = this.popStr();
        const value = this.popStr();
        const ms = Date.parse(value);
        if (isNaN(ms)) throw this.trap("E300", `dt.parse: invalid date "${value}"`);
        this.push(this.vmDateToRecord(new Date(ms)));
        break;
      }
      case 175: { // dt.fmt
        const fmt = this.popStr();
        const dt = this.pop();
        const d = this.vmRecordToDate(dt);
        this.push(Val.str(fmt
          .replace("YYYY", String(d.getUTCFullYear()))
          .replace("MM", String(d.getUTCMonth() + 1).padStart(2, "0"))
          .replace("DD", String(d.getUTCDate()).padStart(2, "0"))
          .replace("HH", String(d.getUTCHours()).padStart(2, "0"))
          .replace("mm", String(d.getUTCMinutes()).padStart(2, "0"))
          .replace("ss", String(d.getUTCSeconds()).padStart(2, "0"))
        ));
        break;
      }
      case 176: { // dt.add
        const ms = this.pop();
        const dt = this.pop();
        const d = this.vmRecordToDate(dt);
        this.push(this.vmDateToRecord(new Date(d.getTime() + (ms.tag === Tag.INT ? ms.value : 0))));
        break;
      }
      case 177: { // dt.sub
        const b = this.pop();
        const a = this.pop();
        this.push(Val.int(this.vmRecordToDate(a).getTime() - this.vmRecordToDate(b).getTime()));
        break;
      }
      case 178: { // dt.tz (stub)
        const tz = this.popStr();
        const dt = this.pop();
        const fields = new Map(this.heapRecord(dt));
        fields.set("tz", Val.str(tz));
        this.push(Val.record(fields));
        break;
      }
      case 179: { // dt.iso
        const dt = this.pop();
        this.push(Val.str(this.vmRecordToDate(dt).toISOString()));
        break;
      }
      case 180: { // dt.ms
        const n = this.pop();
        this.push(Val.int(n.tag === Tag.INT ? n.value : 0));
        break;
      }
      case 181: { // dt.sec
        const n = this.pop();
        this.push(Val.int((n.tag === Tag.INT ? n.value : 0) * 1000));
        break;
      }
      case 182: { // dt.min
        const n = this.pop();
        this.push(Val.int((n.tag === Tag.INT ? n.value : 0) * 60000));
        break;
      }
      case 183: { // dt.hr
        const n = this.pop();
        this.push(Val.int((n.tag === Tag.INT ? n.value : 0) * 3600000));
        break;
      }
      case 184: { // dt.day
        const n = this.pop();
        this.push(Val.int((n.tag === Tag.INT ? n.value : 0) * 86400000));
        break;
      }

      // ── Iterator Combinators ──

      case 70: { // iter.of / iter-of — list to iterator
        const list = this.popList();
        const iter: IteratorState = {
          id: this.nextIteratorId++,
          generatorFn: Val.unit(),
          cleanupFn: Val.unit(),
          done: false,
          closed: false,
          buffer: [],
          index: 0,
          source: list,
        };
        this.push(Val.iter(iter));
        break;
      }

      case 71: { // iter.range / iter-range — truly lazy range [start, end)
        const end = this.numericValue(this.pop());
        const start = this.numericValue(this.pop());
        let current = start;
        const iter = this.makeLazyIter(() => {
          if (current >= end) return null;
          return Val.int(current++);
        });
        this.push(Val.iter(iter));
        break;
      }

      case 72: { // iter.collect / collect — consume iterator into list
        const iterVal = this.pop();
        if (iterVal.tag !== Tag.HEAP || iterVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.collect: expected Iterator");
        }
        const iter = iterVal.value.iter;
        if (iter.closed) throw this.trap("E017", "iter.collect: iterator is closed");
        const result = this.iterCollectAll(iter, iterVal);
        this.push(Val.list(result));
        break;
      }

      case 73: { // iter.map — truly lazy map
        const mapFn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.map: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.map: iterator is closed");
        const iter = this.makeLazyIter(() => {
          const val = this.iterNext(src, srcVal);
          if (val === null) return null;
          return this.callBuiltinFn(mapFn, [val]);
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 74: { // iter.filter — truly lazy filter
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.filter: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.filter: iterator is closed");
        const iter = this.makeLazyIter(() => {
          while (true) {
            const val = this.iterNext(src, srcVal);
            if (val === null) return null;
            const r = this.callBuiltinFn(fn, [val]);
            if (r.tag === Tag.BOOL && r.value) return val;
          }
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 75: { // iter.take — truly lazy take
        const n = this.numericValue(this.pop());
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.take: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.take: iterator is closed");
        let taken = 0;
        const iter = this.makeLazyIter(() => {
          if (taken >= n) return null;
          taken++;
          return this.iterNext(src, srcVal);
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 76: { // iter.drop — truly lazy drop
        const n = this.numericValue(this.pop());
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.drop: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.drop: iterator is closed");
        let dropped = false;
        const iter = this.makeLazyIter(() => {
          if (!dropped) {
            for (let i = 0; i < n; i++) {
              if (this.iterNext(src, srcVal) === null) return null;
            }
            dropped = true;
          }
          return this.iterNext(src, srcVal);
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 77: { // iter.fold — streaming fold (consumes lazily)
        const fn = this.pop();
        const init = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.fold: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.fold: iterator is closed");
        let acc = init;
        let val: Value | null;
        while ((val = this.iterNext(src, srcVal)) !== null) {
          acc = this.callBuiltinFn(fn, [acc, val]);
        }
        src.done = true; src.closed = true;
        this.push(acc);
        break;
      }

      case 78: { // iter.count — streaming count
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.count: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.count: iterator is closed");
        let count = 0;
        while (this.iterNext(src, srcVal) !== null) count++;
        src.done = true; src.closed = true;
        this.push(Val.int(count));
        break;
      }

      case 79: { // iter.sum — streaming sum
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.sum: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.sum: iterator is closed");
        let sum = 0;
        let val: Value | null;
        while ((val = this.iterNext(src, srcVal)) !== null) sum += this.numericValue(val);
        src.done = true; src.closed = true;
        this.push(Val.int(sum));
        break;
      }

      case 80: { // iter.any — streaming, short-circuits
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.any: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.any: iterator is closed");
        let found = false;
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) {
          const r = this.callBuiltinFn(fn, [v]);
          if (r.tag === Tag.BOOL && r.value) { found = true; break; }
        }
        src.done = true; src.closed = true;
        this.push(Val.bool(found));
        break;
      }

      case 81: { // iter.all — streaming, short-circuits
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.all: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.all: iterator is closed");
        let allMatch = true;
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) {
          const r = this.callBuiltinFn(fn, [v]);
          if (!(r.tag === Tag.BOOL && r.value)) { allMatch = false; break; }
        }
        src.done = true; src.closed = true;
        this.push(Val.bool(allMatch));
        break;
      }

      case 82: { // iter.find — streaming, short-circuits
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.find: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.find: iterator is closed");
        const noneTag = this.findVariantTag("None");
        const someTag = this.findVariantTag("Some");
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) {
          const r = this.callBuiltinFn(fn, [v]);
          if (r.tag === Tag.BOOL && r.value) {
            src.done = true; src.closed = true;
            this.push(Val.union(someTag, [v]));
            return;
          }
        }
        src.done = true; src.closed = true;
        this.push(Val.union(noneTag, []));
        break;
      }

      case 83: { // iter.each — streaming
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.each: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.each: iterator is closed");
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) {
          this.callBuiltinFn(fn, [v]);
        }
        src.done = true; src.closed = true;
        this.push(Val.unit());
        break;
      }

      case 84: { // iter.drain — streaming
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.drain: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.drain: iterator is closed");
        while (this.iterNext(src, srcVal) !== null) { /* drain */ }
        src.done = true; src.closed = true;
        this.push(Val.unit());
        break;
      }

      case 85: { // iter.enumerate — truly lazy
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.enumerate: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.enumerate: iterator is closed");
        let idx = 0;
        const iter = this.makeLazyIter(() => {
          const val = this.iterNext(src, srcVal);
          if (val === null) return null;
          return Val.tuple([Val.int(idx++), val]);
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 86: { // iter.chain — truly lazy
        const bVal = this.pop();
        const aVal = this.pop();
        if (aVal.tag !== Tag.HEAP || aVal.value.kind !== "iterator" ||
            bVal.tag !== Tag.HEAP || bVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.chain: expected two Iterators");
        }
        const aSrc = aVal.value.iter;
        const bSrc = bVal.value.iter;
        let inSecond = false;
        const iter = this.makeLazyIter(() => {
          if (!inSecond) {
            const val = this.iterNext(aSrc, aVal);
            if (val !== null) return val;
            inSecond = true;
          }
          return this.iterNext(bSrc, bVal);
        });
        this.push(Val.iter(iter));
        break;
      }

      case 87: { // iter.zip — truly lazy
        const bVal = this.pop();
        const aVal = this.pop();
        if (aVal.tag !== Tag.HEAP || aVal.value.kind !== "iterator" ||
            bVal.tag !== Tag.HEAP || bVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.zip: expected two Iterators");
        }
        const aSrc = aVal.value.iter;
        const bSrc = bVal.value.iter;
        const iter = this.makeLazyIter(() => {
          const a = this.iterNext(aSrc, aVal);
          if (a === null) return null;
          const b = this.iterNext(bSrc, bVal);
          if (b === null) return null;
          return Val.tuple([a, b]);
        });
        this.push(Val.iter(iter));
        break;
      }

      case 88: { // iter.take-while — truly lazy
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.take-while: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.take-while: iterator is closed");
        let stopped = false;
        const iter = this.makeLazyIter(() => {
          if (stopped) return null;
          const val = this.iterNext(src, srcVal);
          if (val === null) return null;
          const r = this.callBuiltinFn(fn, [val]);
          if (!(r.tag === Tag.BOOL && r.value)) { stopped = true; return null; }
          return val;
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 89: { // iter.drop-while — truly lazy
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.drop-while: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.drop-while: iterator is closed");
        let dropping = true;
        const iter = this.makeLazyIter(() => {
          while (dropping) {
            const val = this.iterNext(src, srcVal);
            if (val === null) return null;
            const r = this.callBuiltinFn(fn, [val]);
            if (!(r.tag === Tag.BOOL && r.value)) { dropping = false; return val; }
          }
          return this.iterNext(src, srcVal);
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 90: { // iter.flatmap — truly lazy
        const fn = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.flatmap: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.flatmap: iterator is closed");
        let innerIter: IteratorState | null = null;
        let innerVal: Value = Val.unit();
        const iter = this.makeLazyIter(() => {
          while (true) {
            // Drain current inner iterator
            if (innerIter !== null) {
              const val = this.iterNext(innerIter, innerVal);
              if (val !== null) return val;
              innerIter = null;
            }
            // Get next outer element
            const outerVal = this.iterNext(src, srcVal);
            if (outerVal === null) return null;
            const inner = this.callBuiltinFn(fn, [outerVal]);
            if (inner.tag === Tag.HEAP && inner.value.kind === "iterator") {
              innerIter = inner.value.iter;
              innerVal = inner;
            } else if (inner.tag === Tag.HEAP && inner.value.kind === "list") {
              // Convert list to source-backed
              innerIter = {
                id: -1, generatorFn: Val.unit(), cleanupFn: Val.unit(),
                done: false, closed: false, buffer: [], index: 0, source: inner.value.items,
              };
              innerVal = inner;
            } else {
              throw this.trap("E002", "iter.flatmap: function must return Iterator or List");
            }
          }
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 91: { // iter.first — streaming, takes only 1
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.first: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.first: iterator is closed");
        const noneTag = this.findVariantTag("None");
        const someTag = this.findVariantTag("Some");
        const first = this.iterNext(src, srcVal);
        src.done = true; src.closed = true;
        this.push(first !== null ? Val.union(someTag, [first]) : Val.union(noneTag, []));
        break;
      }

      case 92: { // iter.last — streaming
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.last: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.last: iterator is closed");
        const noneTag = this.findVariantTag("None");
        const someTag = this.findVariantTag("Some");
        let lastVal: Value | null = null;
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) lastVal = v;
        src.done = true; src.closed = true;
        this.push(lastVal !== null ? Val.union(someTag, [lastVal]) : Val.union(noneTag, []));
        break;
      }

      case 93: { // iter.join — streaming
        const sep = this.popStr();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.join: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.join: iterator is closed");
        const parts: string[] = [];
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) {
          parts.push(this.valueToString(v));
        }
        src.done = true; src.closed = true;
        this.push(Val.str(parts.join(sep)));
        break;
      }

      case 94: { // iter.repeat — truly infinite lazy
        const val = this.pop();
        const iter = this.makeLazyIter(() => val);
        this.push(Val.iter(iter));
        break;
      }

      case 95: { // iter.once
        const val = this.pop();
        const iter: IteratorState = {
          id: this.nextIteratorId++,
          generatorFn: Val.unit(), cleanupFn: Val.unit(),
          done: false, closed: false, buffer: [], index: 0, source: [val],
        };
        this.push(Val.iter(iter));
        break;
      }

      case 96: { // iter.empty
        this.pop(); // consume unit arg
        const iter: IteratorState = {
          id: this.nextIteratorId++,
          generatorFn: Val.unit(), cleanupFn: Val.unit(),
          done: true, closed: false, buffer: [], index: 0, source: [],
        };
        this.push(Val.iter(iter));
        break;
      }

      case 97: { // iter.unfold — truly lazy
        const fn = this.pop();
        const seed = this.pop();
        let state = seed;
        const iter = this.makeLazyIter(() => {
          const result = this.callBuiltinFn(fn, [state]);
          if (result.tag === Tag.HEAP && result.value.kind === "union") {
            const varName = this.variantNames[result.value.variantTag] ?? "";
            if (varName === "None") return null;
            if (varName === "Some" && result.value.fields.length > 0) {
              const pair = result.value.fields[0];
              if (pair.tag === Tag.HEAP && pair.value.kind === "tuple" && pair.value.items.length >= 2) {
                const value = pair.value.items[0];
                state = pair.value.items[1];
                return value;
              }
              return pair;
            }
          }
          return null;
        });
        this.push(Val.iter(iter));
        break;
      }

      case 98: { // iter.scan — truly lazy
        const fn = this.pop();
        const init = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.scan: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.scan: iterator is closed");
        let acc = init;
        let emittedInit = false;
        const iter = this.makeLazyIter(() => {
          if (!emittedInit) { emittedInit = true; return init; }
          const val = this.iterNext(src, srcVal);
          if (val === null) return null;
          acc = this.callBuiltinFn(fn, [acc, val]);
          return acc;
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 99: { // iter.dedup — truly lazy
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.dedup: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.dedup: iterator is closed");
        let prev: Value | undefined;
        const vm = this;
        const iter = this.makeLazyIter(() => {
          while (true) {
            const val = vm.iterNext(src, srcVal);
            if (val === null) return null;
            if (prev === undefined || !vm.valuesEqual(val, prev)) {
              prev = val;
              return val;
            }
          }
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 100: { // iter.chunk — truly lazy
        const n = this.numericValue(this.pop());
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.chunk: expected Iterator");
        }
        if (n <= 0) throw this.trap("E003", "iter.chunk: chunk size must be > 0");
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.chunk: iterator is closed");
        const iter = this.makeLazyIter(() => {
          const chunk: Value[] = [];
          for (let i = 0; i < n; i++) {
            const val = this.iterNext(src, srcVal);
            if (val === null) break;
            chunk.push(val);
          }
          return chunk.length > 0 ? Val.list(chunk) : null;
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 101: { // iter.window — lazy with buffer
        const n = this.numericValue(this.pop());
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.window: expected Iterator");
        }
        if (n <= 0) throw this.trap("E003", "iter.window: window size must be > 0");
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.window: iterator is closed");
        const windowBuf: Value[] = [];
        let windowReady = false;
        const iter = this.makeLazyIter(() => {
          if (!windowReady) {
            // Fill initial window
            for (let i = 0; i < n; i++) {
              const val = this.iterNext(src, srcVal);
              if (val === null) return null; // not enough elements
              windowBuf.push(val);
            }
            windowReady = true;
            return Val.list([...windowBuf]);
          }
          const val = this.iterNext(src, srcVal);
          if (val === null) return null;
          windowBuf.shift();
          windowBuf.push(val);
          return Val.list([...windowBuf]);
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 102: { // iter.intersperse — truly lazy
        const sep = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.intersperse: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.intersperse: iterator is closed");
        let needSep = false;
        let pending: Value | null = null;
        const iter = this.makeLazyIter(() => {
          if (pending !== null) {
            const val = pending;
            pending = null;
            return val;
          }
          const val = this.iterNext(src, srcVal);
          if (val === null) return null;
          if (needSep) {
            pending = val;
            return sep;
          }
          needSep = true;
          return val;
        }, src.cleanupFn);
        this.push(Val.iter(iter));
        break;
      }

      case 103: { // iter.cycle — truly infinite lazy (buffers source elements)
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.cycle: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.cycle: iterator is closed");
        const buf: Value[] = [];
        let collecting = true;
        let cycleIdx = 0;
        const iter = this.makeLazyIter(() => {
          if (collecting) {
            const val = this.iterNext(src, srcVal);
            if (val !== null) { buf.push(val); return val; }
            collecting = false;
            if (buf.length === 0) return null;
            cycleIdx = 0;
          }
          return buf[cycleIdx++ % buf.length];
        });
        this.push(Val.iter(iter));
        break;
      }

      case 104: { // iter.nth — streaming, skips n elements
        const n = this.numericValue(this.pop());
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.nth: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.nth: iterator is closed");
        const noneTag = this.findVariantTag("None");
        const someTag = this.findVariantTag("Some");
        let result: Value | null = null;
        for (let i = 0; i <= n; i++) {
          result = this.iterNext(src, srcVal);
          if (result === null) break;
        }
        src.done = true; src.closed = true;
        this.push(result !== null ? Val.union(someTag, [result]) : Val.union(noneTag, []));
        break;
      }

      case 105: { // iter.min — streaming
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.min: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.min: iterator is closed");
        const noneTag = this.findVariantTag("None");
        const someTag = this.findVariantTag("Some");
        let minVal: Value | null = null;
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) {
          if (minVal === null || this.numericValue(v) < this.numericValue(minVal)) minVal = v;
        }
        src.done = true; src.closed = true;
        this.push(minVal !== null ? Val.union(someTag, [minVal]) : Val.union(noneTag, []));
        break;
      }

      case 106: { // iter.max — streaming
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter.max: expected Iterator");
        }
        const src = srcVal.value.iter;
        if (src.closed) throw this.trap("E017", "iter.max: iterator is closed");
        const noneTag = this.findVariantTag("None");
        const someTag = this.findVariantTag("Some");
        let maxVal: Value | null = null;
        let v: Value | null;
        while ((v = this.iterNext(src, srcVal)) !== null) {
          if (maxVal === null || this.numericValue(v) > this.numericValue(maxVal)) maxVal = v;
        }
        src.done = true; src.closed = true;
        this.push(maxVal !== null ? Val.union(someTag, [maxVal]) : Val.union(noneTag, []));
        break;
      }

      case 107: { // iter.generate — truly lazy
        const fn = this.pop();
        const iter = this.makeLazyIter(() => {
          const result = this.callBuiltinFn(fn, [Val.unit()]);
          if (result.tag === Tag.HEAP && result.value.kind === "union") {
            const varName = this.variantNames[result.value.variantTag] ?? "";
            if (varName === "None") return null;
            if (varName === "Some" && result.value.fields.length > 0) {
              return result.value.fields[0];
            }
          }
          return null;
        });
        this.push(Val.iter(iter));
        break;
      }

      case 111: { // close-iter (standalone)
        const iterVal = this.pop();
        if (iterVal.tag !== Tag.HEAP || iterVal.value.kind !== "iterator") {
          throw this.trap("E002", "close-iter: expected Iterator");
        }
        const iter = iterVal.value.iter;
        if (!iter.closed) {
          iter.closed = true;
          iter.done = true;
          if (iter.nativeCleanup) iter.nativeCleanup();
          if (iter.cleanupFn.tag !== Tag.UNIT) {
            this.callBuiltinFn(iter.cleanupFn, []);
          }
        }
        this.push(Val.unit());
        break;
      }

      case 112: { // next (standalone)
        const iterVal = this.pop();
        if (iterVal.tag !== Tag.HEAP || iterVal.value.kind !== "iterator") {
          throw this.trap("E002", "next: expected Iterator");
        }
        const iter = iterVal.value.iter;
        if (iter.closed) throw this.trap("E017", "next: iterator is closed");
        this.checkCancellation();
        const nextVal = this.iterNext(iter, iterVal);
        if (nextVal === null) {
          throw this.trap("E016", "iterator exhausted");
        }
        this.push(iterVal);
        this.push(nextVal);
        break;
      }

      // ── Runtime-dispatched for-loop builtins ──
      // These dispatch on collection type: List → eager list ops, Iter → iter protocol

      case 113: { // __for_each — for P in E do B
        // List: map(E, fn) → returns list
        // Iter: iter.each(E, fn) → returns unit (side-effect iteration)
        const fn = this.pop();
        const collection = this.pop();
        if (collection.tag === Tag.HEAP && collection.value.kind === "iterator") {
          // Iterator path: consume via iter.each, return unit
          const src = collection.value.iter;
          if (src.closed) throw this.trap("E017", "__for_each: iterator is closed");
          let v: Value | null;
          while ((v = this.iterNext(src, collection)) !== null) {
            this.callBuiltinFn(fn, [v]);
          }
          src.done = true; src.closed = true;
          this.push(Val.unit());
        } else {
          // List path: map(E, fn) → returns new list
          if (collection.tag !== Tag.HEAP || collection.value.kind !== "list") {
            throw this.trap("E002", `expected List or Iterator, got ${this.tagName(collection)}`);
          }
          const list = collection.value.items;
          const results: Value[] = [];
          for (const el of list) {
            results.push(this.callBuiltinFn(fn, [el]));
          }
          this.push(Val.list(results));
        }
        break;
      }

      case 114: { // __for_filter — for P in E if G ...
        // List: filter(E, fn) → returns list
        // Iter: iter.filter(E, fn) → returns lazy iterator
        const fn = this.pop();
        const collection = this.pop();
        if (collection.tag === Tag.HEAP && collection.value.kind === "iterator") {
          // Iterator path: lazy filter via iter.filter
          const src = collection.value.iter;
          if (src.closed) throw this.trap("E017", "__for_filter: iterator is closed");
          const iter = this.makeLazyIter(() => {
            while (true) {
              const val = this.iterNext(src, collection);
              if (val === null) return null;
              const r = this.callBuiltinFn(fn, [val]);
              if (r.tag === Tag.BOOL && r.value) return val;
            }
          }, src.cleanupFn);
          this.push(Val.iter(iter));
        } else {
          // List path: filter(E, fn) → returns filtered list
          if (collection.tag !== Tag.HEAP || collection.value.kind !== "list") {
            throw this.trap("E002", `expected List or Iterator, got ${this.tagName(collection)}`);
          }
          const list = collection.value.items;
          const results: Value[] = [];
          for (const el of list) {
            const result = this.callBuiltinFn(fn, [el]);
            if (result.tag === Tag.BOOL && result.value) {
              results.push(el);
            }
          }
          this.push(Val.list(results));
        }
        break;
      }

      case 115: { // __for_fold — for P in E fold A = I do B
        // List: fold(E, init, fn) → returns accumulated value
        // Iter: iter.fold(E, init, fn) → returns accumulated value
        const fn = this.pop();
        const init = this.pop();
        const collection = this.pop();
        if (collection.tag === Tag.HEAP && collection.value.kind === "iterator") {
          // Iterator path: streaming fold
          const src = collection.value.iter;
          if (src.closed) throw this.trap("E017", "__for_fold: iterator is closed");
          let acc = init;
          let v: Value | null;
          while ((v = this.iterNext(src, collection)) !== null) {
            acc = this.callBuiltinFn(fn, [acc, v]);
          }
          src.done = true; src.closed = true;
          this.push(acc);
        } else {
          // List path: fold(E, init, fn)
          if (collection.tag !== Tag.HEAP || collection.value.kind !== "list") {
            throw this.trap("E002", `expected List or Iterator, got ${this.tagName(collection)}`);
          }
          const list = collection.value.items;
          let acc = init;
          for (const el of list) {
            acc = this.callBuiltinFn(fn, [acc, el]);
          }
          this.push(acc);
        }
        break;
      }

      // ── Channel-Iterator Bridge ──

      case 108: { // iter-recv — create iterator from channel receiver
        const recvVal = this.pop();
        if (recvVal.tag !== Tag.HEAP || recvVal.value.kind !== "receiver") {
          throw this.trap("E002", "iter-recv: expected Receiver");
        }
        const channel = recvVal.value.channel;
        const iter: IteratorState = {
          id: this.nextIteratorId++,
          generatorFn: Val.unit(), // not used; we use the channel directly
          cleanupFn: Val.unit(),
          done: false,
          closed: false,
          buffer: [],
          index: 0,
          source: null,
        };
        // Use a generator that drains the channel buffer
        // Since the VM is synchronous, we drain everything available now
        while (channel.buffer.length > 0) {
          iter.buffer.push(channel.buffer.shift()!);
        }
        if (!channel.senderOpen && iter.buffer.length === 0) {
          iter.done = true;
        }
        // Fall back to source-backed for what we have
        iter.source = iter.buffer.splice(0);
        this.push(Val.iter(iter));
        break;
      }

      case 109: { // iter-send — consume iterator, sending each value to channel sender
        const senderVal = this.pop();
        const srcVal = this.pop();
        if (srcVal.tag !== Tag.HEAP || srcVal.value.kind !== "iterator") {
          throw this.trap("E002", "iter-send: expected Iterator as first arg");
        }
        if (senderVal.tag !== Tag.HEAP || senderVal.value.kind !== "sender") {
          throw this.trap("E002", "iter-send: expected Sender as second arg");
        }
        const src = srcVal.value.iter;
        const channel = senderVal.value.channel;
        if (src.closed) throw this.trap("E017", "iter-send: iterator is closed");
        const items = this.iterCollectAll(src, srcVal);
        for (const item of items) {
          if (!channel.senderOpen) throw this.trap("E018", "iter-send: sender closed");
          channel.buffer.push(item);
        }
        this.push(Val.unit());
        break;
      }

      case 110: { // iter-spawn — create iterator backed by spawned task producing values via channel
        const bodyFn = this.pop();
        const capacity = 16; // default channel capacity
        const channel: Channel = {
          id: this.nextChannelId++,
          buffer: [],
          capacity,
          senderOpen: true,
          receiverOpen: true,
        };
        // Call the body function with the sender; it should send values
        const sender = Val.sender(channel);
        try {
          this.callBuiltinFn(bodyFn, [sender]);
        } catch {
          // Body completed (normally or with error); close sender
        }
        channel.senderOpen = false;
        const bufValues = [...channel.buffer];
        channel.buffer.length = 0;
        const iter: IteratorState = {
          id: this.nextIteratorId++,
          generatorFn: Val.unit(),
          cleanupFn: Val.unit(),
          done: bufValues.length === 0,
          closed: false,
          buffer: [],
          index: 0,
          source: bufValues,
        };
        this.push(Val.iter(iter));
        break;
      }

      // ── Streaming I/O Builtins ──

      case 190: { // fs.stream-lines — demand-driven file line reading
        const path = this.popStr();
        try {
          const { openSync, readSync, closeSync } = require("fs");
          const fd = openSync(path, "r");
          const chunkSize = 4096;
          const buf = Buffer.alloc(chunkSize);
          let remainder = "";
          let eof = false;
          let fdClosed = false;
          const closeFd = () => { if (!fdClosed) { fdClosed = true; try { closeSync(fd); } catch {} } };
          const iter = this.makeLazyIter(() => {
            while (true) {
              const nlIdx = remainder.indexOf("\n");
              if (nlIdx >= 0) {
                const line = remainder.substring(0, nlIdx);
                remainder = remainder.substring(nlIdx + 1);
                return Val.str(line);
              }
              if (eof) {
                if (remainder.length > 0) {
                  const line = remainder;
                  remainder = "";
                  return Val.str(line);
                }
                closeFd();
                return null;
              }
              const bytesRead = readSync(fd, buf, 0, chunkSize, null);
              if (bytesRead === 0) { eof = true; continue; }
              remainder += buf.toString("utf-8", 0, bytesRead);
            }
          });
          iter.nativeCleanup = closeFd;
          this.push(Val.iter(iter));
        } catch (e: any) {
          throw this.trap("E300", `fs.stream-lines: ${e.message}`);
        }
        break;
      }

      case 191: { // http.stream-lines — demand-driven HTTP response line iteration
        const url = this.popStr();
        try {
          const res = this.vmHttpRequest("GET", url, null);
          const fields = this.heapRecord(res);
          const body = fields.get("body");
          const bodyStr = body && body.tag === Tag.STR ? body.value : "";
          // HTTP response is buffered (sync VM limitation), but yield lazily via nativeNext
          let offset = 0;
          const iter = this.makeLazyIter(() => {
            if (offset >= bodyStr.length) return null;
            const nlIdx = bodyStr.indexOf("\n", offset);
            if (nlIdx >= 0) {
              const line = bodyStr.substring(offset, nlIdx);
              offset = nlIdx + 1;
              return Val.str(line);
            }
            // Last line (no trailing newline)
            if (offset < bodyStr.length) {
              const line = bodyStr.substring(offset);
              offset = bodyStr.length;
              return Val.str(line);
            }
            return null;
          });
          this.push(Val.iter(iter));
        } catch (e: any) {
          if (e instanceof VMTrap) throw e;
          throw this.trap("E300", `http.stream-lines: ${e.message}`);
        }
        break;
      }

      case 192: { // proc.stream — demand-driven process stdout line iteration
        const argList = this.popList().map(a => this.valStr(a));
        const cmd = this.popStr();
        try {
          const { spawnSync } = require("child_process");
          const result = spawnSync(cmd, argList, { encoding: "utf-8", timeout: 30000 });
          const output: string = result.stdout ?? "";
          // Process output is buffered (spawnSync), but yield lazily via nativeNext
          let offset = 0;
          const iter = this.makeLazyIter(() => {
            if (offset >= output.length) return null;
            const nlIdx = output.indexOf("\n", offset);
            if (nlIdx >= 0) {
              const line = output.substring(offset, nlIdx);
              offset = nlIdx + 1;
              return Val.str(line);
            }
            if (offset < output.length) {
              const line = output.substring(offset);
              offset = output.length;
              return Val.str(line);
            }
            return null;
          });
          this.push(Val.iter(iter));
        } catch (e: any) {
          throw this.trap("E300", `proc.stream: ${e.message}`);
        }
        break;
      }

      case 193: { // io.stdin-lines — demand-driven stdin line reading
        try {
          const { openSync, readSync, closeSync } = require("fs");
          let fd: number;
          let fdValid = true;
          try {
            fd = 0; // stdin file descriptor
            // Verify stdin is readable by attempting a zero-byte read concept
            // (actual reads happen on demand in nativeNext)
          } catch {
            fdValid = false;
          }
          if (!fdValid) {
            // stdin not available — return empty iterator
            const iter = this.makeLazyIter(() => null);
            this.push(Val.iter(iter));
            break;
          }
          const chunkSize = 4096;
          const buf = Buffer.alloc(chunkSize);
          let remainder = "";
          let eof = false;
          const iter = this.makeLazyIter(() => {
            while (true) {
              const nlIdx = remainder.indexOf("\n");
              if (nlIdx >= 0) {
                const line = remainder.substring(0, nlIdx);
                remainder = remainder.substring(nlIdx + 1);
                return Val.str(line);
              }
              if (eof) {
                if (remainder.length > 0) {
                  const line = remainder;
                  remainder = "";
                  return Val.str(line);
                }
                return null;
              }
              try {
                const bytesRead = readSync(fd, buf, 0, chunkSize, null);
                if (bytesRead === 0) { eof = true; continue; }
                remainder += buf.toString("utf-8", 0, bytesRead);
              } catch {
                eof = true;
              }
            }
          });
          this.push(Val.iter(iter));
        } catch (e: any) {
          throw this.trap("E300", `io.stdin-lines: ${e.message}`);
        }
        break;
      }

      default:
        throw this.trap("E010", `unknown builtin word ID ${wordId}`);
    }
  }

  // ── Iterator Helpers ──

  /** Pull the next value from an iterator. Returns null when exhausted. */
  private iterNext(iter: IteratorState, iterVal: Value): Value | null {
    if (iter.done) return null;
    // Fast path: list-backed
    if (iter.source !== null) {
      if (iter.index >= iter.source.length) { iter.done = true; return null; }
      return iter.source[iter.index++];
    }
    // Native lazy generator
    if (iter.nativeNext) {
      const val = iter.nativeNext();
      if (val === null) { iter.done = true; return null; }
      return val;
    }
    // Buffer drain
    if (iter.buffer.length > 0) return iter.buffer.shift()!;
    // Clank closure generator
    try {
      const val = this.callBuiltinFn(iter.generatorFn, [iterVal]);
      if (val.tag === Tag.HEAP && val.value.kind === "union") {
        const varName = this.variantNames[val.value.variantTag] ?? "";
        if (varName === "None" || varName === "IterDone") { iter.done = true; return null; }
        if (varName === "Some" && val.value.fields.length > 0) return val.value.fields[0];
      }
      return val;
    } catch (e) {
      if (e instanceof VMTrap && e.code === "E016") { iter.done = true; return null; }
      throw e;
    }
  }

  /** Collect all remaining values from an iterator, closing it afterward. */
  private iterCollectAll(iter: IteratorState, iterVal: Value): Value[] {
    const result: Value[] = [];
    let val: Value | null;
    while ((val = this.iterNext(iter, iterVal)) !== null) {
      result.push(val);
    }
    iter.done = true;
    iter.closed = true;
    if (iter.nativeCleanup) iter.nativeCleanup();
    if (iter.cleanupFn.tag !== Tag.UNIT) {
      this.callBuiltinFn(iter.cleanupFn, []);
    }
    return result;
  }

  /** Take up to N values from an iterator, closing it afterward. */
  private iterTakeN(iter: IteratorState, iterVal: Value, n: number): Value[] {
    const result: Value[] = [];
    for (let i = 0; i < n; i++) {
      const val = this.iterNext(iter, iterVal);
      if (val === null) break;
      result.push(val);
    }
    // Close the source iterator
    iter.done = true;
    iter.closed = true;
    if (iter.nativeCleanup) iter.nativeCleanup();
    if (iter.cleanupFn.tag !== Tag.UNIT) {
      this.callBuiltinFn(iter.cleanupFn, []);
    }
    return result;
  }

  /** Create a lazy iterator with a native next function. */
  private makeLazyIter(nativeNext: () => Value | null, cleanupFn?: Value): IteratorState {
    return {
      id: this.nextIteratorId++,
      generatorFn: Val.unit(),
      cleanupFn: cleanupFn ?? Val.unit(),
      done: false,
      closed: false,
      buffer: [],
      index: 0,
      source: null,
      nativeNext,
    };
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

  /** Dispatch an interface method synchronously from within a VM builtin.
   *  Looks up the dispatch table, pushes args, calls the impl, and returns the result. */
  private dispatchMethodSync(methodName: string, args: Value[]): Value {
    const typeTag = this.runtimeTypeTag(args[0]);
    const methodImpls = this.dispatchTable.get(methodName);
    if (!methodImpls) {
      throw this.trap("E212", `no impls registered for method '${methodName}'`);
    }
    const wordId = methodImpls.get(typeTag);
    if (wordId === undefined) {
      throw this.trap("E212", `no impl of '${methodName}' for type '${typeTag}'`);
    }
    for (const arg of args) {
      this.push(arg);
    }
    const savedCallDepth = this.callStack.length;
    this.doCall(wordId);
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

  // ── Async Scheduler Helpers ──

  private checkCancellation(): void {
    if (this.currentTaskId === 0) return;
    const task = this.tasks.get(this.currentTaskId);
    if (!task) return;
    if (task.cancelFlag && task.shieldDepth === 0) {
      task.status = "cancelled";
      throw this.trap("E011", "task cancelled");
    }
  }

  private suspendCurrentTask(): void {
    if (this.currentTaskId === 0) return;
    const task = this.tasks.get(this.currentTaskId);
    if (!task) return;
    // Save execution state
    task.dataStack = [...this.dataStack];
    task.callStack = this.callStack.map(f => ({ ...f, locals: [...f.locals] }));
    task.handlerStack = this.handlerStack.map(h => ({ ...h, locals: [...h.locals] }));
    task.ip = this.ip;
    task.currentWord = this.currentWord;
    task.topFrame = (this as any)._topFrame ? { ...(this as any)._topFrame, locals: [...(this as any)._topFrame.locals] } : null;
    task.status = "suspended";
  }

  private restoreTask(task: Task): void {
    this.dataStack = [...task.dataStack];
    this.callStack = task.callStack.map(f => ({ ...f, locals: [...f.locals] }));
    this.handlerStack = task.handlerStack.map(h => ({ ...h, locals: [...h.locals] }));
    this.ip = task.ip;
    this.currentWord = task.currentWord;
    if (task.topFrame) {
      (this as any)._topFrame = { ...task.topFrame, locals: [...task.topFrame.locals] };
    }
    this.currentTaskId = task.id;
    task.status = "running";
  }

  private scheduleNext(): void {
    // Find a runnable task (suspended tasks, preferring those that aren't the current one)
    for (const [id, task] of this.tasks) {
      if (task.status === "suspended") {
        this.restoreTask(task);
        this.execute();
        return;
      }
    }
    // No more tasks to run — return to parent context
  }

  private runTaskToCompletion(task: Task): void {
    // Save parent state
    const savedDataStack = [...this.dataStack];
    const savedCallStack = this.callStack.map(f => ({ ...f, locals: [...f.locals] }));
    const savedHandlerStack = this.handlerStack.map(h => ({ ...h, locals: [...h.locals] }));
    const savedIp = this.ip;
    const savedWord = this.currentWord;
    const savedTaskId = this.currentTaskId;
    const savedTopFrame = (this as any)._topFrame;
    const savedHalted = this.halted;
    const savedGroupStack = [...this.activeGroupStack];

    // Set up child task execution
    this.dataStack = [...task.dataStack];
    this.callStack = task.callStack.map(f => ({ ...f, locals: [...f.locals] }));
    this.handlerStack = task.handlerStack.map(h => ({ ...h, locals: [...h.locals] }));
    this.ip = task.ip;
    this.currentWord = task.currentWord;
    this.currentTaskId = task.id;
    this.halted = false;
    (this as any)._topFrame = task.topFrame ? { ...task.topFrame, locals: [...task.topFrame.locals] } : undefined;
    this.activeGroupStack = [];
    task.status = "running";

    try {
      this.execute();
      // Task completed normally
      task.result = this.dataStack.length > 0 ? this.dataStack[this.dataStack.length - 1] : Val.unit();
      task.status = "completed";
    } catch (err: any) {
      if (err instanceof VMTrap && err.code === "E011") {
        task.status = "cancelled";
      } else {
        task.status = "failed";
        task.error = err instanceof VMTrap ? err.message : String(err);
      }
    }

    // Wake up awaiters
    for (const awaiterId of task.awaiters) {
      const awaiter = this.tasks.get(awaiterId);
      if (awaiter && awaiter.status === "suspended") {
        // Push the result onto the awaiter's stack
        if (task.status === "completed") {
          awaiter.dataStack.push(task.result ?? Val.unit());
        }
        // Keep awaiter suspended — it'll be picked up by scheduler
      }
    }

    // Restore parent state
    this.dataStack = savedDataStack;
    this.callStack = savedCallStack;
    this.handlerStack = savedHandlerStack;
    this.ip = savedIp;
    this.currentWord = savedWord;
    this.currentTaskId = savedTaskId;
    this.halted = savedHalted;
    (this as any)._topFrame = savedTopFrame;
    this.activeGroupStack = savedGroupStack;
  }

  private runGroupChildren(group: TaskGroup): void {
    // Run all children in the group until they complete
    let changed = true;
    while (changed) {
      changed = false;
      for (const childId of group.childTaskIds) {
        const child = this.tasks.get(childId);
        if (child && (child.status === "suspended" || child.status === "running")) {
          this.runTaskToCompletion(child);
          changed = true;
        }
      }
    }
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

  private numericOrStrValue(v: Value): number | string {
    if (v.tag === Tag.INT) return v.value;
    if (v.tag === Tag.RAT) return v.value;
    if (v.tag === Tag.STR) return v.value;
    throw this.trap("E002", `expected numeric or string type, got ${this.tagName(v)}`);
  }

  private findVariantTag(name: string): number {
    const idx = this.variantNames.indexOf(name);
    if (idx !== -1) return idx;
    throw this.trap("E010", `variant '${name}' not found in variant names`);
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

  /** Check if a value is affine (must be consumed exactly once, cannot be freely copied). */
  private isAffineValue(v: Value): boolean {
    if (v.tag !== Tag.HEAP) return false;
    const kind = v.value.kind;
    return kind === "ref" || kind === "tvar" || kind === "iterator"
      || kind === "sender" || kind === "receiver" || kind === "future";
  }

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

  private runtimeTypeTag(v: Value): string {
    switch (v.tag) {
      case Tag.INT: return "Int";
      case Tag.RAT: return "Rat";
      case Tag.BOOL: return "Bool";
      case Tag.STR: return "Str";
      case Tag.BYTE: return "Byte";
      case Tag.UNIT: return "Unit";
      case Tag.QUOTE: return "Fn";
      case Tag.HEAP: {
        switch (v.value.kind) {
          case "list": return "List";
          case "tuple": return "Tuple";
          case "record": return "Record";
          case "union": return this.variantNames[v.value.variantTag] || "?";
          case "closure": return "Fn";
          case "continuation": return "Continuation";
          case "future": return "Future";
          case "tvar": return "TVar";
          case "iterator": return "Iter";
          case "ref": return "Ref";
          case "sender": return "Sender";
          case "receiver": return "Receiver";
          case "select-set": return "SelectSet";
        }
      }
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
          case "future": return `<future:${obj.taskId}>`;
          case "tvar": return `<tvar:${obj.tvar.id}>`;
          case "iterator": return `<iter:${obj.iter.id}>`;
          case "ref": return `<ref:${obj.ref.id}>`;
          case "sender": return `<sender:${obj.channel.id}>`;
          case "receiver": return `<receiver:${obj.channel.id}>`;
          case "select-set": return `<select-set:${obj.arms.length}>`;
        }
      }
    }
  }

  // ── FFI value conversion ──

  private vmValueToJs(v: Value): unknown {
    switch (v.tag) {
      case Tag.INT: return v.value;
      case Tag.RAT: return v.value;
      case Tag.BOOL: return v.value;
      case Tag.STR: return v.value;
      case Tag.BYTE: return v.value;
      case Tag.UNIT: return undefined;
      case Tag.QUOTE: return undefined;
      case Tag.HEAP: {
        const obj = v.value;
        switch (obj.kind) {
          case "list": return obj.items.map(i => this.vmValueToJs(i));
          case "tuple": return obj.items.map(i => this.vmValueToJs(i));
          case "record": {
            const result: Record<string, unknown> = {};
            for (const [k, val] of obj.fields) result[k] = this.vmValueToJs(val);
            return result;
          }
          case "union": {
            const name = this.variantNames[obj.variantTag] ?? `variant:${obj.variantTag}`;
            if (name === "None" && obj.fields.length === 0) return null;
            if (name === "Some" && obj.fields.length === 1) return this.vmValueToJs(obj.fields[0]);
            return { tag: name, fields: obj.fields.map(f => this.vmValueToJs(f)) };
          }
          default: return undefined;
        }
      }
    }
  }

  private jsToVmValue(v: unknown): Value {
    if (v === undefined || v === null) return Val.unit();
    if (typeof v === "number") {
      return Number.isInteger(v) ? Val.int(v) : Val.rat(v);
    }
    if (typeof v === "bigint") return Val.int(Number(v));
    if (typeof v === "boolean") return Val.bool(v);
    if (typeof v === "string") return Val.str(v);
    if (Array.isArray(v)) return Val.list(v.map(i => this.jsToVmValue(i)));
    if (typeof v === "object") {
      if (v instanceof Buffer) return Val.str(v.toString("utf-8"));
      const fields = new Map<string, Value>();
      for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
        fields.set(k, this.jsToVmValue(val));
      }
      return Val.record(fields);
    }
    return Val.str(String(v));
  }

  // ── STM Runtime ──

  private static readonly BASE_BACKOFF_US = 10;
  private static readonly MAX_BACKOFF_US = 1000;
  private static readonly MAX_RETRIES = 64;

  private runAtomically(bodyFn: Value): Value {
    const descriptor: TxnDescriptor = {
      snapshotVersion: this.globalClock,
      readSet: [],
      writeSet: [],
      status: "active",
      orElseStack: [],
      abortCount: 0,
    };

    for (;;) {
      descriptor.snapshotVersion = this.globalClock;
      descriptor.readSet = [];
      descriptor.writeSet = [];
      descriptor.status = "active";
      descriptor.orElseStack = [];

      const savedTxn = this.activeTxn;
      this.activeTxn = descriptor;

      try {
        const result = this.callBuiltinFn(bodyFn, []);
        // Try to commit
        if (this.txnCommit(descriptor)) {
          this.activeTxn = savedTxn;
          return result;
        }
        // Commit failed — validation error, retry
        this.activeTxn = savedTxn;
      } catch (e) {
        this.activeTxn = savedTxn;
        if (e instanceof STMRetrySignal) {
          // retry or abort-and-restart — loop
        } else {
          // Real exception — abort transaction, propagate
          descriptor.status = "aborted";
          throw e;
        }
      }

      // Backoff on repeated aborts
      descriptor.abortCount++;
      if (descriptor.abortCount > VM.MAX_RETRIES) {
        throw this.trap("E022", "STM livelock: too many retries");
      }
      // In single-threaded JS, backoff is a no-op — just loop
    }
  }

  private txnRead(desc: TxnDescriptor, tvar: TVar): Value {
    // 1. Check write set (most recent buffered value wins)
    for (let i = desc.writeSet.length - 1; i >= 0; i--) {
      if (desc.writeSet[i].tvar === tvar) return desc.writeSet[i].newValue;
    }

    // 2. Check read set (already read this TVar)
    for (const entry of desc.readSet) {
      if (entry.tvar === tvar) return entry.value;
    }

    // 3. First read — read committed state
    const currentVersion = tvar.version;
    const currentValue = tvar.value;

    // 4. Eager validation: version must not exceed snapshot
    if (currentVersion > desc.snapshotVersion) {
      desc.status = "aborted";
      throw new STMRetrySignal(); // will be caught by runAtomically
    }

    // 5. Record in read set
    desc.readSet.push({ tvar, version: currentVersion, value: currentValue });
    return currentValue;
  }

  private txnWrite(desc: TxnDescriptor, tvar: TVar, value: Value): void {
    // Ensure TVar is in read set for validation
    let known = false;
    for (const e of desc.readSet) { if (e.tvar === tvar) { known = true; break; } }
    if (!known) {
      for (const e of desc.writeSet) { if (e.tvar === tvar) { known = true; break; } }
    }
    if (!known) {
      const currentVersion = tvar.version;
      if (currentVersion > desc.snapshotVersion) {
        desc.status = "aborted";
        throw new STMRetrySignal();
      }
      desc.readSet.push({ tvar, version: currentVersion, value: tvar.value });
    }

    // Buffer the write (upsert)
    for (let i = 0; i < desc.writeSet.length; i++) {
      if (desc.writeSet[i].tvar === tvar) {
        desc.writeSet[i].newValue = value;
        desc.writeSet[i].newOccupied = true;
        return;
      }
    }
    desc.writeSet.push({ tvar, newValue: value, newOccupied: true });
  }

  private txnTake(desc: TxnDescriptor, tvar: TVar): Value {
    // Check write set for buffered state
    for (let i = desc.writeSet.length - 1; i >= 0; i--) {
      if (desc.writeSet[i].tvar === tvar) {
        if (!desc.writeSet[i].newOccupied) {
          throw this.trap("E023", "tvar-take: TVar is empty");
        }
        const val = desc.writeSet[i].newValue;
        desc.writeSet[i].newOccupied = false;
        desc.writeSet[i].newValue = Val.unit();
        return val;
      }
    }

    // Read committed state
    const currentVersion = tvar.version;
    if (currentVersion > desc.snapshotVersion) {
      desc.status = "aborted";
      throw new STMRetrySignal();
    }
    if (!tvar.occupied) {
      throw this.trap("E023", "tvar-take: TVar is empty");
    }

    const val = tvar.value;
    desc.readSet.push({ tvar, version: currentVersion, value: val });
    desc.writeSet.push({ tvar, newValue: Val.unit(), newOccupied: false });
    return val;
  }

  private txnPut(desc: TxnDescriptor, tvar: TVar, value: Value): void {
    // Check write set for buffered state
    for (let i = desc.writeSet.length - 1; i >= 0; i--) {
      if (desc.writeSet[i].tvar === tvar) {
        if (desc.writeSet[i].newOccupied) {
          throw this.trap("E024", "tvar-put: TVar is occupied");
        }
        desc.writeSet[i].newValue = value;
        desc.writeSet[i].newOccupied = true;
        return;
      }
    }

    // Read committed state
    const currentVersion = tvar.version;
    if (currentVersion > desc.snapshotVersion) {
      desc.status = "aborted";
      throw new STMRetrySignal();
    }
    if (tvar.occupied) {
      throw this.trap("E024", "tvar-put: TVar is occupied");
    }

    desc.readSet.push({ tvar, version: currentVersion, value: tvar.value });
    desc.writeSet.push({ tvar, newValue: value, newOccupied: true });
  }

  private txnValidate(desc: TxnDescriptor): boolean {
    for (const entry of desc.readSet) {
      if (entry.tvar.version !== entry.version) return false;
    }
    return true;
  }

  private txnCommit(desc: TxnDescriptor): boolean {
    // In single-threaded JS, no real lock needed — but follow the protocol
    if (!this.txnValidate(desc)) {
      desc.status = "aborted";
      return false;
    }

    // Increment global clock
    const newVersion = ++this.globalClock;

    // Write-back: apply all buffered writes
    for (const entry of desc.writeSet) {
      entry.tvar.value = entry.newValue;
      entry.tvar.occupied = entry.newOccupied;
      entry.tvar.version = newVersion;
    }

    // Wake retriers
    for (const entry of desc.writeSet) {
      for (const waiter of entry.tvar.waitQueue) {
        waiter.woken = true;
        waiter.wake();
      }
      entry.tvar.waitQueue.clear();
    }

    desc.status = "committed";
    return true;
  }

  private txnRetry(desc: TxnDescriptor): void {
    // In single-threaded JS, retry without blocking would be an infinite loop.
    // Since there's no concurrent thread to change the TVars, a top-level retry
    // outside or-else is a deadlock. Raise a trap.
    throw this.trap("E025", "STM retry: no concurrent writers — would block forever (single-threaded)");
  }

  private txnOrElse(desc: TxnDescriptor, action1: Value, action2: Value): Value {
    // Save checkpoint
    const checkpoint: OrElseCheckpoint = {
      readSetLen: desc.readSet.length,
      writeSetLen: desc.writeSet.length,
    };
    desc.orElseStack.push(checkpoint);

    // Try action1
    try {
      const result = this.callBuiltinFn(action1, []);
      desc.orElseStack.pop();
      return result;
    } catch (e) {
      if (!(e instanceof STMRetrySignal)) {
        desc.orElseStack.pop();
        throw e;
      }
    }

    // action1 retried — rollback to checkpoint
    desc.readSet.length = checkpoint.readSetLen;
    desc.writeSet.length = checkpoint.writeSetLen;
    desc.orElseStack.pop();

    // Try action2
    // If action2 also retries, the STMRetrySignal propagates up
    return this.callBuiltinFn(action2, []);
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

  // ── Tier 2 helper methods ──

  private heapRecord(v: Value): Map<string, Value> {
    if (v.tag === Tag.HEAP && (v.value as any).kind === "record") {
      return (v.value as any).fields as Map<string, Value>;
    }
    throw this.trap("E200", `expected Record, got ${v.tag}`);
  }

  private valStr(v: Value): string {
    if (v.tag === Tag.STR) return v.value;
    throw this.trap("E200", `expected Str, got ${v.tag}`);
  }

  private showValue(v: Value): string {
    switch (v.tag) {
      case Tag.INT: return String(v.value);
      case Tag.RAT: return String(v.value);
      case Tag.BOOL: return v.value ? "true" : "false";
      case Tag.STR: return v.value;
      case Tag.UNIT: return "()";
      case Tag.HEAP: {
        const obj = v.value;
        if (obj.kind === "list") return "[" + obj.items.map(i => this.showValue(i)).join(", ") + "]";
        if (obj.kind === "tuple") return "(" + obj.items.map(i => this.showValue(i)).join(", ") + ")";
        if (obj.kind === "record") return "{" + [...obj.fields.entries()].map(([k, val]) => `${k}: ${this.showValue(val)}`).join(", ") + "}";
        return "<heap>";
      }
      default: return "?";
    }
  }

  private vmHttpRequest(method: string, url: string, body: string | null): Value {
    const { execSync } = require("child_process");
    try {
      const bodyArgs = body !== null ? ["-d", body, "-H", "Content-Type: application/json"] : [];
      const cmd = ["curl", "-s", "-X", method, "-w", "\\n%{http_code}", ...bodyArgs, url];
      const raw: string = execSync(cmd.map(a => `'${a}'`).join(" "), { encoding: "utf-8", timeout: 30000 });
      const lines = raw.trimEnd().split("\n");
      const statusCode = parseInt(lines[lines.length - 1], 10);
      const responseBody = lines.slice(0, -1).join("\n");
      return Val.record(new Map<string, Value>([
        ["status", Val.int(statusCode)],
        ["headers", Val.list([])],
        ["body", Val.str(responseBody)],
      ]));
    } catch (e: any) {
      throw this.trap("E300", `http.${method.toLowerCase()} failed: ${e.message}`);
    }
  }

  private jsonToVmValue(v: any): Value {
    if (v === null) return Val.union(0, []); // JNull
    if (typeof v === "boolean") return Val.union(1, [Val.bool(v)]); // JBool
    if (typeof v === "number") return Number.isInteger(v)
      ? Val.union(2, [Val.int(v)])  // JInt
      : Val.union(3, [Val.rat(v)]); // JRat
    if (typeof v === "string") return Val.union(4, [Val.str(v)]); // JStr
    if (Array.isArray(v)) return Val.union(5, [Val.list(v.map(el => this.jsonToVmValue(el)))]); // JArr
    if (typeof v === "object") {
      const pairs = Object.entries(v).map(([k, val]) => Val.tuple([Val.str(k), this.jsonToVmValue(val)]));
      return Val.union(6, [Val.list(pairs)]); // JObj
    }
    return Val.unit();
  }

  private vmCsvParse(input: string, delim = ","): Value {
    const rows: Value[] = [];
    const lines = input.split("\n");
    for (const line of lines) {
      if (line.trim() === "") continue;
      const cells: Value[] = [];
      let current = "";
      let inQuotes = false;
      for (let i = 0; i < line.length; i++) {
        const ch = line[i];
        if (inQuotes) {
          if (ch === '"' && line[i + 1] === '"') { current += '"'; i++; }
          else if (ch === '"') { inQuotes = false; }
          else { current += ch; }
        } else {
          if (ch === '"') { inQuotes = true; }
          else if (ch === delim) { cells.push(Val.str(current)); current = ""; }
          else { current += ch; }
        }
      }
      cells.push(Val.str(current));
      rows.push(Val.list(cells));
    }
    return Val.list(rows);
  }

  private vmCsvEncode(rows: Value[]): string {
    return rows.map(row => {
      const items = row.tag === Tag.HEAP && (row.value as any).kind === "list"
        ? (row.value as any).items as Value[] : [];
      return items.map(cell => {
        const s = cell.tag === Tag.STR ? cell.value : this.showValue(cell);
        return s.includes(",") || s.includes('"') || s.includes("\n")
          ? `"${s.replace(/"/g, '""')}"` : s;
      }).join(",");
    }).join("\n");
  }

  private vmDateToRecord(d: Date): Value {
    return Val.record(new Map<string, Value>([
      ["year", Val.int(d.getUTCFullYear())],
      ["month", Val.int(d.getUTCMonth() + 1)],
      ["day", Val.int(d.getUTCDate())],
      ["hour", Val.int(d.getUTCHours())],
      ["min", Val.int(d.getUTCMinutes())],
      ["sec", Val.int(d.getUTCSeconds())],
      ["tz", Val.str("UTC")],
    ]));
  }

  private vmRecordToDate(v: Value): Date {
    const fields = this.heapRecord(v);
    const get = (k: string): number => {
      const f = fields.get(k);
      return f && f.tag === Tag.INT ? f.value : 0;
    };
    return new Date(Date.UTC(get("year"), get("month") - 1, get("day"), get("hour"), get("min"), get("sec")));
  }

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
