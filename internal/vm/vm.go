package vm

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/dalurness/clank/internal/compiler"
)

const (
	maxCallDepth = 10000
	maxLocals    = 256
)

// VMTrap is a runtime error raised by the VM.
type VMTrap struct {
	Code    string
	Message string
	Word    string
	Offset  int
}

func (e *VMTrap) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// VM is the Clank bytecode interpreter.
type VM struct {
	dataStack    []Value
	callStack    []CallFrame
	handlerStack []HandlerFrame
	wordMap      map[int]*compiler.BytecodeWord
	strings      []string
	rationals    []float64
	variantNames []string
	dispatchTbl  map[string]map[string]int
	module       *compiler.BytecodeModule

	ip          int
	currentWord *compiler.BytecodeWord
	halted      bool
	topFrame    *CallFrame // implicit top-level frame

	// Concurrency: parent pointer for child VMs spawned by goroutines.
	// nil for the root VM. Child VMs route shared state access through root().
	parent *VM
	mu     sync.Mutex      // protects Stdout, tasks, taskGroups, ID counters
	ctx    context.Context  // cancellation context; Background for root, derived for children

	// Async state
	nextTaskID    int
	nextGroupID   int
	nextChannelID int
	nextTVarID    int
	nextIterID    int
	nextRefID     int
	currentTaskID int
	tasks         map[int]*taskState
	taskGroups    map[int]*taskGroup
	activeGroups  []int
	asyncMode     bool

	// STM transaction write-log — records pre-modification TVar state
	// so that atomically/or-else can restore on abort.
	txnWriteLog []tvarSnapshot
	inTxn       bool

	// Configurable timeouts
	httpTimeout int // HTTP request timeout in milliseconds (0 = use default 30s)

	// I/O capture (for testing)
	Stdout []string
}

// taskResult communicates a goroutine-spawned task's completion.
type taskResult struct {
	value *Value
	err   error
}

type taskState struct {
	id          int
	status      string
	dataStack   []Value
	callStack   []CallFrame
	handlerStack []HandlerFrame
	ip          int
	currentWord *compiler.BytecodeWord
	topFrame    *CallFrame
	result      *Value
	errMsg      string
	parentID    int
	groupID     int
	cancelFlag  bool
	shieldDepth int
	awaiters    []int
	resultCh    chan taskResult   // goroutine sends completion here
	ctx         context.Context   // derived from parent; cancelled to interrupt I/O
	cancel      context.CancelFunc
}

type taskGroup struct {
	id             int
	parentTaskID   int
	childTaskIDs   map[int]bool
	dataStackDepth int
}

// New creates a VM for the given bytecode module.
func New(mod *compiler.BytecodeModule) *VM {
	vm := &VM{
		wordMap:       make(map[int]*compiler.BytecodeWord),
		strings:       mod.Strings,
		rationals:     mod.Rationals,
		variantNames:  mod.VariantNames,
		module:        mod,
		ctx:           context.Background(),
		nextTaskID:    1,
		nextGroupID:   1,
		nextChannelID: 1,
		nextTVarID:    1,
		nextIterID:    1,
		nextRefID:     1,
		tasks:         make(map[int]*taskState),
		taskGroups:    make(map[int]*taskGroup),
	}
	if mod.DispatchTable != nil {
		vm.dispatchTbl = mod.DispatchTable
	} else {
		vm.dispatchTbl = make(map[string]map[string]int)
	}
	for i := range mod.Words {
		w := &mod.Words[i]
		vm.wordMap[w.WordID] = w
	}
	// Set package-level variant names so ValueToString can resolve them
	activeVariantNames = mod.VariantNames
	return vm
}

// root returns the root VM (for shared state access). Child VMs delegate to parent.
func (vm *VM) root() *VM {
	if vm.parent != nil {
		return vm.parent
	}
	return vm
}

// appendStdout appends a line to Stdout in a thread-safe way.
func (vm *VM) appendStdout(s string) {
	root := vm.root()
	root.mu.Lock()
	root.Stdout = append(root.Stdout, s)
	root.mu.Unlock()
}

// spawnTaskVM creates a child VM for a spawned task. It shares read-only data
// (wordMap, strings, variantNames, etc.) with the parent but has its own
// mutable execution state (stacks, ip, halted).
func (vm *VM) spawnTaskVM(task *taskState) *VM {
	root := vm.root()
	child := &VM{
		// Shared read-only
		wordMap:      root.wordMap,
		strings:      root.strings,
		rationals:    root.rationals,
		variantNames: root.variantNames,
		dispatchTbl:  root.dispatchTbl,
		module:       root.module,

		// Own mutable execution state (copied from task)
		dataStack:    append([]Value{}, task.dataStack...),
		callStack:    append([]CallFrame{}, task.callStack...),
		handlerStack: append([]HandlerFrame{}, task.handlerStack...),
		ip:           0,
		currentWord:  task.currentWord,
		topFrame:     task.topFrame,

		// Parent pointer for shared state access
		parent: root,
		ctx:    task.ctx,

		// Own async bookkeeping (nested spawns route through root)
		currentTaskID: task.id,
		tasks:         root.tasks,
		taskGroups:    root.taskGroups,
	}
	return child
}

// Run executes the module's entry word.
func (vm *VM) Run() (*Value, error) {
	if vm.module.EntryWordID == nil {
		return nil, &VMTrap{Code: "E010", Message: "no main word found"}
	}
	return vm.CallWord(*vm.module.EntryWordID)
}

// CallWord executes a word by ID and returns the top of the data stack.
func (vm *VM) CallWord(wordID int) (*Value, error) {
	word, ok := vm.wordMap[wordID]
	if !ok {
		return nil, vm.trap("E010", fmt.Sprintf("word ID %d not found", wordID))
	}
	vm.currentWord = word
	vm.ip = 0
	vm.halted = false

	if err := vm.execute(); err != nil {
		return nil, err
	}

	if len(vm.dataStack) > 0 {
		v := vm.dataStack[len(vm.dataStack)-1]
		return &v, nil
	}
	return nil, nil
}

// ── Fetch-Decode-Execute Loop ──

func (vm *VM) execute() error {
	for !vm.halted {
		word := vm.currentWord
		code := word.Code

		if vm.ip >= len(code) {
			if len(vm.callStack) == 0 {
				return nil
			}
			vm.doReturn()
			continue
		}

		opcode := code[vm.ip]
		vm.ip++
		if err := vm.dispatch(opcode, code); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) dispatch(opcode byte, code []byte) error {
	switch opcode {
	// ── Stack Manipulation ──
	case compiler.OpNOP:
		// no-op

	case compiler.OpDUP:
		v, err := vm.peek()
		if err != nil {
			return err
		}
		vm.push(v)

	case compiler.OpDROP:
		_, err := vm.pop()
		return err

	case compiler.OpSWAP:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		vm.push(b)
		vm.push(a)

	case compiler.OpROT:
		c, err := vm.pop()
		if err != nil {
			return err
		}
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		vm.push(b)
		vm.push(c)
		vm.push(a)

	case compiler.OpOVER:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.peek()
		if err != nil {
			return err
		}
		vm.push(b)
		vm.push(a)

	case compiler.OpPICK:
		n := int(vm.readU8(code))
		idx := len(vm.dataStack) - 1 - n
		if idx < 0 {
			return vm.trap("E001", "PICK: stack underflow")
		}
		vm.push(vm.dataStack[idx])

	case compiler.OpROLL:
		n := int(vm.readU8(code))
		idx := len(vm.dataStack) - 1 - n
		if idx < 0 {
			return vm.trap("E001", "ROLL: stack underflow")
		}
		val := vm.dataStack[idx]
		vm.dataStack = append(vm.dataStack[:idx], vm.dataStack[idx+1:]...)
		vm.push(val)

	// ── Constants / Literals ──
	case compiler.OpPUSH_INT:
		vm.push(ValInt(int(vm.readU8(code))))

	case compiler.OpPUSH_INT16:
		vm.push(ValInt(int(vm.readU16(code))))

	case compiler.OpPUSH_INT32:
		vm.push(ValInt(int(vm.readU32(code))))

	case compiler.OpPUSH_TRUE:
		vm.push(ValBool(true))

	case compiler.OpPUSH_FALSE:
		vm.push(ValBool(false))

	case compiler.OpPUSH_UNIT:
		vm.push(ValUnit())

	case compiler.OpPUSH_STR:
		idx := vm.readU16(code)
		vm.push(ValStr(vm.strings[idx]))

	case compiler.OpPUSH_BYTE:
		vm.push(ValByte(vm.readU8(code)))

	case compiler.OpPUSH_RAT:
		idx := vm.readU32(code)
		vm.push(ValRat(vm.rationals[idx]))

	// ── Arithmetic ──
	case compiler.OpADD:
		return vm.binaryArith(func(a, b float64) float64 { return a + b })
	case compiler.OpSUB:
		return vm.binaryArith(func(a, b float64) float64 { return a - b })
	case compiler.OpMUL:
		return vm.binaryArith(func(a, b float64) float64 { return a * b })

	case compiler.OpDIV:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		bv, ok := NumericValue(b)
		if !ok {
			return vm.trap("E002", "DIV: expected numeric type")
		}
		if bv == 0 {
			if !vm.doRaise("division by zero") {
				return vm.trap("E003", "division by zero")
			}
			return nil
		}
		av, _ := NumericValue(a)
		if a.Tag == TagINT && b.Tag == TagINT {
			// Int / Int -> truncated Int
			vm.push(ValInt(int(math.Trunc(av / bv))))
		} else {
			vm.push(ValRat(av / bv))
		}

	case compiler.OpMOD:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		bv, ok := NumericValue(b)
		if !ok {
			return vm.trap("E002", "MOD: expected numeric type")
		}
		if bv == 0 {
			if !vm.doRaise("division by zero (mod)") {
				return vm.trap("E003", "division by zero (mod)")
			}
			return nil
		}
		av, _ := NumericValue(a)
		vm.push(ValInt(int(av) % int(bv)))

	case compiler.OpNEG:
		a, err := vm.pop()
		if err != nil {
			return err
		}
		switch a.Tag {
		case TagINT:
			vm.push(ValInt(-a.IntVal))
		case TagRAT:
			vm.push(ValRat(-a.RatVal))
		default:
			return vm.trap("E002", "NEG: expected numeric type")
		}

	// ── Comparison ──
	case compiler.OpEQ:
		b, _ := vm.pop()
		a, _ := vm.pop()
		vm.push(ValBool(ValuesEqual(a, b)))

	case compiler.OpNEQ:
		b, _ := vm.pop()
		a, _ := vm.pop()
		vm.push(ValBool(!ValuesEqual(a, b)))

	case compiler.OpLT:
		b, _ := vm.pop()
		a, _ := vm.pop()
		av, _ := NumericValue(a)
		bv, _ := NumericValue(b)
		vm.push(ValBool(av < bv))

	case compiler.OpGT:
		b, _ := vm.pop()
		a, _ := vm.pop()
		av, _ := NumericValue(a)
		bv, _ := NumericValue(b)
		vm.push(ValBool(av > bv))

	case compiler.OpLTE:
		b, _ := vm.pop()
		a, _ := vm.pop()
		av, _ := NumericValue(a)
		bv, _ := NumericValue(b)
		vm.push(ValBool(av <= bv))

	case compiler.OpGTE:
		b, _ := vm.pop()
		a, _ := vm.pop()
		av, _ := NumericValue(a)
		bv, _ := NumericValue(b)
		vm.push(ValBool(av >= bv))

	// ── Logic ──
	case compiler.OpAND:
		b, err := vm.popBool()
		if err != nil {
			return err
		}
		a, err := vm.popBool()
		if err != nil {
			return err
		}
		vm.push(ValBool(a && b))

	case compiler.OpOR:
		b, err := vm.popBool()
		if err != nil {
			return err
		}
		a, err := vm.popBool()
		if err != nil {
			return err
		}
		vm.push(ValBool(a || b))

	case compiler.OpNOT:
		a, err := vm.popBool()
		if err != nil {
			return err
		}
		vm.push(ValBool(!a))

	// ── Control Flow ──
	case compiler.OpJMP:
		offset := vm.readU16(code)
		vm.ip += int(offset)

	case compiler.OpJMP_IF:
		offset := vm.readU16(code)
		cond, err := vm.popBool()
		if err != nil {
			return err
		}
		if cond {
			vm.ip += int(offset)
		}

	case compiler.OpJMP_UNLESS:
		offset := vm.readU16(code)
		cond, err := vm.popBool()
		if err != nil {
			return err
		}
		if !cond {
			vm.ip += int(offset)
		}

	case compiler.OpCALL:
		wordID := int(vm.readU16(code))
		return vm.doCall(wordID)

	case compiler.OpCALL_DYN:
		callee, err := vm.pop()
		if err != nil {
			return err
		}
		return vm.doCallDyn(callee)

	case compiler.OpRET:
		if len(vm.callStack) == 0 {
			vm.halted = true
			return nil
		}
		vm.doReturn()

	case compiler.OpTAIL_CALL:
		wordID := int(vm.readU16(code))
		return vm.doTailCall(wordID)

	case compiler.OpTAIL_CALL_DYN:
		callee, err := vm.pop()
		if err != nil {
			return err
		}
		return vm.doTailCallDyn(callee)

	// ── Quotations and Closures ──
	case compiler.OpQUOTE:
		wordID := int(vm.readU16(code))
		vm.push(ValQuote(wordID))

	case compiler.OpCLOSURE:
		wordID := int(vm.readU16(code))
		captureCount := int(vm.readU8(code))
		captures := make([]Value, captureCount)
		for i := captureCount - 1; i >= 0; i-- {
			v, err := vm.pop()
			if err != nil {
				return err
			}
			captures[i] = v
		}
		vm.push(ValClosure(wordID, captures))

	// ── Local Variables ──
	case compiler.OpLOCAL_GET:
		idx := int(vm.readU8(code))
		frame := vm.currentFrame()
		for len(frame.Locals) <= idx {
			frame.Locals = append(frame.Locals, ValUnit())
		}
		vm.push(frame.Locals[idx])

	case compiler.OpLOCAL_SET:
		idx := int(vm.readU8(code))
		frame := vm.currentFrame()
		for len(frame.Locals) <= idx {
			frame.Locals = append(frame.Locals, ValUnit())
		}
		v, err := vm.pop()
		if err != nil {
			return err
		}
		frame.Locals[idx] = v

	// ── Heap / Compound Values ──
	case compiler.OpLIST_NEW:
		count := int(vm.readU8(code))
		items := make([]Value, count)
		for i := count - 1; i >= 0; i-- {
			v, _ := vm.pop()
			items[i] = v
		}
		vm.push(ValList(items))

	case compiler.OpLIST_LEN:
		list, err := vm.popList()
		if err != nil {
			return err
		}
		vm.push(ValInt(len(list)))

	case compiler.OpLIST_HEAD:
		list, err := vm.popList()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return vm.trap("E004", "LIST_HEAD: empty list")
		}
		vm.push(list[0])

	case compiler.OpLIST_TAIL:
		list, err := vm.popList()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return vm.trap("E004", "LIST_TAIL: empty list")
		}
		vm.push(ValList(append([]Value{}, list[1:]...)))

	case compiler.OpLIST_CONS:
		list, err := vm.popList()
		if err != nil {
			return err
		}
		elem, err := vm.pop()
		if err != nil {
			return err
		}
		newList := make([]Value, 0, len(list)+1)
		newList = append(newList, elem)
		newList = append(newList, list...)
		vm.push(ValList(newList))

	case compiler.OpLIST_CAT:
		b, err := vm.popList()
		if err != nil {
			return err
		}
		a, err := vm.popList()
		if err != nil {
			return err
		}
		newList := make([]Value, 0, len(a)+len(b))
		newList = append(newList, a...)
		newList = append(newList, b...)
		vm.push(ValList(newList))

	case compiler.OpLIST_IDX:
		idxVal, err := vm.pop()
		if err != nil {
			return err
		}
		list, err := vm.popList()
		if err != nil {
			return err
		}
		i := numVal(idxVal)
		if i < 0 || i >= len(list) {
			return vm.trap("E004", fmt.Sprintf("LIST_IDX: index %d out of bounds (length %d)", i, len(list)))
		}
		vm.push(list[i])

	case compiler.OpLIST_REV:
		list, err := vm.popList()
		if err != nil {
			return err
		}
		rev := make([]Value, len(list))
		for i, v := range list {
			rev[len(list)-1-i] = v
		}
		vm.push(ValList(rev))

	case compiler.OpTUPLE_NEW:
		arity := int(vm.readU8(code))
		items := make([]Value, arity)
		for i := arity - 1; i >= 0; i-- {
			v, _ := vm.pop()
			items[i] = v
		}
		vm.push(ValTuple(items))

	case compiler.OpTUPLE_GET:
		idx := int(vm.readU8(code))
		val, err := vm.pop()
		if err != nil {
			return err
		}
		if val.Tag != TagHEAP || val.Heap.Kind != KindTuple {
			return vm.trap("E002", "TUPLE_GET: expected tuple")
		}
		if idx >= len(val.Heap.Items) {
			return vm.trap("E004", fmt.Sprintf("TUPLE_GET: index %d out of bounds", idx))
		}
		vm.push(val.Heap.Items[idx])

	case compiler.OpRECORD_NEW:
		fieldCount := int(vm.readU8(code))
		fieldNames := make([]string, fieldCount)
		for i := 0; i < fieldCount; i++ {
			fieldNames[i] = vm.strings[vm.readU16(code)]
		}
		values := make([]Value, fieldCount)
		for i := fieldCount - 1; i >= 0; i-- {
			v, _ := vm.pop()
			values[i] = v
		}
		fields := make(map[string]Value, fieldCount)
		for i := 0; i < fieldCount; i++ {
			fields[fieldNames[i]] = values[i]
		}
		vm.push(ValRecord(fields, fieldNames))

	case compiler.OpRECORD_GET:
		fieldID := vm.readU16(code)
		val, err := vm.pop()
		if err != nil {
			return err
		}
		if val.Tag != TagHEAP || val.Heap.Kind != KindRecord {
			return vm.trap("E002", "RECORD_GET: expected record")
		}
		fieldName := vm.strings[fieldID]
		fv, ok := val.Heap.Fields[fieldName]
		if !ok {
			return vm.trap("E002", fmt.Sprintf("RECORD_GET: field '%s' not found", fieldName))
		}
		vm.push(fv)

	case compiler.OpRECORD_SET:
		fieldID := vm.readU16(code)
		rec, err := vm.pop()
		if err != nil {
			return err
		}
		val, err := vm.pop()
		if err != nil {
			return err
		}
		if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
			return vm.trap("E002", "RECORD_SET: expected record")
		}
		fieldName := vm.strings[fieldID]
		newFields := make(map[string]Value, len(rec.Heap.Fields))
		newOrder := make([]string, len(rec.Heap.FieldOrder))
		copy(newOrder, rec.Heap.FieldOrder)
		for k, v := range rec.Heap.Fields {
			newFields[k] = v
		}
		if _, exists := newFields[fieldName]; !exists {
			newOrder = append(newOrder, fieldName)
		}
		newFields[fieldName] = val
		vm.push(ValRecord(newFields, newOrder))

	case compiler.OpRECORD_REST:
		excludeCount := int(vm.readU8(code))
		excludeNames := make(map[string]bool, excludeCount)
		for i := 0; i < excludeCount; i++ {
			excludeNames[vm.strings[vm.readU16(code)]] = true
		}
		src, err := vm.pop()
		if err != nil {
			return err
		}
		if src.Tag != TagHEAP || src.Heap.Kind != KindRecord {
			return vm.trap("E002", "RECORD_REST: expected record")
		}
		newFields := make(map[string]Value)
		var newOrder []string
		for _, k := range src.Heap.FieldOrder {
			if !excludeNames[k] {
				newFields[k] = src.Heap.Fields[k]
				newOrder = append(newOrder, k)
			}
		}
		vm.push(ValRecord(newFields, newOrder))

	case compiler.OpUNION_NEW:
		variantTag := int(vm.readU8(code))
		arity := int(vm.readU8(code))
		fields := make([]Value, arity)
		for i := arity - 1; i >= 0; i-- {
			v, _ := vm.pop()
			fields[i] = v
		}
		vm.push(ValUnion(variantTag, fields))

	case compiler.OpVARIANT_TAG:
		val, err := vm.pop()
		if err != nil {
			return err
		}
		if val.Tag != TagHEAP || val.Heap.Kind != KindUnion {
			return vm.trap("E002", "VARIANT_TAG: expected union")
		}
		vm.push(ValInt(val.Heap.VariantTag))

	case compiler.OpVARIANT_FIELD:
		idx := int(vm.readU8(code))
		val, err := vm.pop()
		if err != nil {
			return err
		}
		if val.Tag != TagHEAP || val.Heap.Kind != KindUnion {
			return vm.trap("E002", "VARIANT_FIELD: expected union")
		}
		if idx >= len(val.Heap.UFields) {
			return vm.trap("E004", fmt.Sprintf("VARIANT_FIELD: index %d out of bounds", idx))
		}
		vm.push(val.Heap.UFields[idx])

	case compiler.OpTUPLE_GET_DYN:
		idxVal, err := vm.pop()
		if err != nil {
			return err
		}
		val, err := vm.pop()
		if err != nil {
			return err
		}
		if val.Tag != TagHEAP || val.Heap.Kind != KindTuple {
			return vm.trap("E002", "TUPLE_GET_DYN: expected tuple")
		}
		i := numVal(idxVal)
		if i < 0 || i >= len(val.Heap.Items) {
			return vm.trap("E004", fmt.Sprintf("TUPLE_GET_DYN: index %d out of bounds", i))
		}
		vm.push(val.Heap.Items[i])

	// ── String Operations ──
	case compiler.OpSTR_CAT:
		b, err := vm.popStr()
		if err != nil {
			return err
		}
		a, err := vm.popStr()
		if err != nil {
			return err
		}
		vm.push(ValStr(a + b))

	case compiler.OpCONCAT:
		b, err := vm.pop()
		if err != nil {
			return err
		}
		a, err := vm.pop()
		if err != nil {
			return err
		}
		if a.Tag == TagSTR && b.Tag == TagSTR {
			vm.push(ValStr(a.StrVal + b.StrVal))
		} else if a.Tag == TagHEAP && a.Heap.Kind == KindList && b.Tag == TagHEAP && b.Heap.Kind == KindList {
			combined := make([]Value, 0, len(a.Heap.Items)+len(b.Heap.Items))
			combined = append(combined, a.Heap.Items...)
			combined = append(combined, b.Heap.Items...)
			vm.push(ValList(combined))
		} else {
			return vm.trap("E002", "++: operands must be both strings or both lists")
		}

	case compiler.OpSTR_LEN:
		s, err := vm.popStr()
		if err != nil {
			return err
		}
		vm.push(ValInt(len([]rune(s))))

	case compiler.OpSTR_SPLIT:
		delim, err := vm.popStr()
		if err != nil {
			return err
		}
		s, err := vm.popStr()
		if err != nil {
			return err
		}
		parts := strings.Split(s, delim)
		items := make([]Value, len(parts))
		for i, p := range parts {
			items[i] = ValStr(p)
		}
		vm.push(ValList(items))

	case compiler.OpSTR_JOIN:
		delim, err := vm.popStr()
		if err != nil {
			return err
		}
		list, err := vm.popList()
		if err != nil {
			return err
		}
		parts := make([]string, len(list))
		for i, v := range list {
			parts[i] = ValueToString(v)
		}
		vm.push(ValStr(strings.Join(parts, delim)))

	case compiler.OpSTR_TRIM:
		s, err := vm.popStr()
		if err != nil {
			return err
		}
		vm.push(ValStr(strings.TrimSpace(s)))

	case compiler.OpTO_STR:
		v, err := vm.pop()
		if err != nil {
			return err
		}
		vm.push(ValStr(ValueToString(v)))

	// ── I/O ──
	case compiler.OpIO_PRINT:
		s, err := vm.popStr()
		if err != nil {
			return err
		}
		vm.appendStdout(s)
		vm.push(ValUnit())

	// ── Effect Handlers ──
	case compiler.OpHANDLE_PUSH:
		effectID := int(vm.readU16(code))
		handlerOffset := int(vm.readU16(code))
		groupIdx := int(vm.readU8(code))
		groupBase := len(vm.handlerStack) - groupIdx
		vm.handlerStack = append(vm.handlerStack, HandlerFrame{
			EffectID:          effectID,
			HandlerOffset:     handlerOffset,
			WordID:            vm.currentWord.WordID,
			DataStackDepth:    len(vm.dataStack),
			CallStackDepth:    len(vm.callStack),
			HandlerStackDepth: groupBase,
			Locals:            copyLocals(vm.currentFrame().Locals),
		})

	case compiler.OpHANDLE_POP:
		if len(vm.handlerStack) == 0 {
			return vm.trap("E011", "HANDLE_POP: no handler on stack")
		}
		vm.handlerStack = vm.handlerStack[:len(vm.handlerStack)-1]

	case compiler.OpEFFECT_PERFORM:
		effectID := int(vm.readU16(code))
		argCount := int(vm.readU8(code))
		args := make([]Value, argCount)
		for i := argCount - 1; i >= 0; i-- {
			v, _ := vm.pop()
			args[i] = v
		}
		if !vm.doEffectPerform(effectID, args) {
			name := fmt.Sprintf("%d", effectID)
			if effectID < len(vm.strings) {
				name = vm.strings[effectID]
			}
			return vm.trap("E011", fmt.Sprintf("unhandled effect: %s", name))
		}

	case compiler.OpRESUME:
		contVal, err := vm.pop()
		if err != nil {
			return err
		}
		resumeValue, err := vm.pop()
		if err != nil {
			return err
		}
		if contVal.Tag != TagHEAP || contVal.Heap.Kind != KindContinuation {
			return vm.trap("E002", "RESUME: expected continuation")
		}
		cont := contVal.Heap.Cont

		baseDataDepth := len(vm.dataStack)
		baseCallDepth := len(vm.callStack)
		baseHandlerDepth := len(vm.handlerStack)

		// Restore continuation's data stack
		vm.dataStack = append(vm.dataStack, cont.DataStack...)
		// Push resume value
		vm.dataStack = append(vm.dataStack, resumeValue)

		// Restore call stack frames
		for _, f := range cont.CallStack {
			vm.callStack = append(vm.callStack, CallFrame{
				ReturnIP:     f.ReturnIP,
				ReturnWordID: f.ReturnWordID,
				Locals:       copyLocals(f.Locals),
				StackBase:    f.StackBase,
			})
		}

		// Restore handler stack with adjusted depths
		for _, h := range cont.HandlerStack {
			vm.handlerStack = append(vm.handlerStack, HandlerFrame{
				EffectID:          h.EffectID,
				HandlerOffset:     h.HandlerOffset,
				WordID:            h.WordID,
				Locals:            copyLocals(h.Locals),
				DataStackDepth:    baseDataDepth + (h.DataStackDepth - cont.BaseDataDepth),
				CallStackDepth:    baseCallDepth + (h.CallStackDepth - cont.BaseCallDepth),
				HandlerStackDepth: baseHandlerDepth + (h.HandlerStackDepth - cont.BaseHandlerDepth),
			})
		}

		// Restore execution point
		resumeWord, ok := vm.wordMap[cont.WordID]
		if !ok {
			return vm.trap("E010", fmt.Sprintf("resume word %d not found", cont.WordID))
		}
		vm.currentWord = resumeWord
		vm.ip = cont.IP

		// Restore locals at the perform site
		if len(cont.CallStack) == 0 {
			vm.currentFrame().Locals = copyLocals(cont.Locals)
		}

	// ── Interface Dispatch ──
	case compiler.OpDISPATCH:
		methodIdx := int(vm.readU16(code))
		argCount := int(vm.readU8(code))
		methodName := vm.strings[methodIdx]

		firstArgPos := len(vm.dataStack) - argCount
		dispatchArg := vm.dataStack[firstArgPos]
		typeTag := vm.runtimeTypeTag(dispatchArg)

		methodImpls, ok := vm.dispatchTbl[methodName]
		if !ok {
			return vm.trap("E212", fmt.Sprintf("no impls registered for method '%s'", methodName))
		}
		wordID, ok := methodImpls[typeTag]
		if !ok {
			return vm.trap("E212", fmt.Sprintf("no impl of '%s' for type '%s'", methodName, typeTag))
		}
		return vm.doCall(wordID)

	// ── VM Control ──
	case compiler.OpHALT:
		vm.halted = true
		return nil

	case compiler.OpTRAP:
		errCode := vm.readU16(code)
		return vm.trap("E000", fmt.Sprintf("TRAP instruction with code %d", errCode))

	case compiler.OpDEBUG:
		parts := make([]string, len(vm.dataStack))
		for i, v := range vm.dataStack {
			parts[i] = ValueToString(v)
		}
		vm.appendStdout(fmt.Sprintf(`{"debug":[%s]}`, strings.Join(parts, ",")))

	// ── Async ──
	case compiler.OpTASK_SPAWN:
		return vm.opTaskSpawn()
	case compiler.OpTASK_AWAIT:
		return vm.opTaskAwait()
	case compiler.OpTASK_YIELD:
		if err := vm.checkCancellation(); err != nil {
			return err
		}
		vm.push(ValUnit())
	case compiler.OpTASK_SLEEP:
		return vm.builtinSleep()
	case compiler.OpTASK_GROUP_ENTER:
		vm.opTaskGroupEnter()
	case compiler.OpTASK_GROUP_EXIT:
		return vm.opTaskGroupExit(code)
	case compiler.OpCHAN_NEW:
		return vm.opChanNew()
	case compiler.OpCHAN_SEND:
		return vm.opChanSend(code)
	case compiler.OpCHAN_RECV:
		return vm.opChanRecv(code)
	case compiler.OpCHAN_TRY_RECV:
		return vm.opChanTryRecv()
	case compiler.OpCHAN_CLOSE:
		return vm.opChanClose()
	case compiler.OpSELECT_BUILD:
		return vm.opSelectBuild(code)
	case compiler.OpSELECT_WAIT:
		return vm.opSelectWait(code)
	case compiler.OpTASK_CANCEL_CHECK:
		cancelled := false
		if vm.currentTaskID != 0 {
			root := vm.root()
			root.mu.Lock()
			if t, ok := root.tasks[vm.currentTaskID]; ok {
				cancelled = t.cancelFlag && t.shieldDepth == 0
			}
			root.mu.Unlock()
		}
		vm.push(ValBool(cancelled))
	case compiler.OpTASK_SHIELD_ENTER:
		if vm.currentTaskID != 0 {
			root := vm.root()
			root.mu.Lock()
			if t, ok := root.tasks[vm.currentTaskID]; ok {
				t.shieldDepth++
			}
			root.mu.Unlock()
		}
	case compiler.OpTASK_SHIELD_EXIT:
		if vm.currentTaskID != 0 {
			root := vm.root()
			root.mu.Lock()
			if t, ok := root.tasks[vm.currentTaskID]; ok {
				if t.shieldDepth > 0 {
					t.shieldDepth--
				}
				if t.shieldDepth == 0 && t.cancelFlag && t.cancel != nil {
					t.cancel()
				}
			}
			root.mu.Unlock()
		}

	// ── STM ──
	case compiler.OpTVAR_NEW:
		initial, _ := vm.pop()
		root := vm.root()
		root.mu.Lock()
		tv := &TVar{ID: root.nextTVarID, Version: 0, Val: initial, Occupied: true, HandleCount: 1}
		root.nextTVarID++
		root.mu.Unlock()
		vm.push(ValTVarVal(tv))
	case compiler.OpTVAR_READ:
		v, _ := vm.pop()
		if v.Tag != TagHEAP || v.Heap.Kind != KindTVar {
			return vm.trap("E002", "TVAR_READ: expected TVar")
		}
		tv := v.Heap.TVar
		tv.mu.Lock()
		val := tv.Val
		tv.mu.Unlock()
		vm.push(val)
	case compiler.OpTVAR_WRITE:
		newVal, _ := vm.pop()
		tvVal, _ := vm.pop()
		if tvVal.Tag != TagHEAP || tvVal.Heap.Kind != KindTVar {
			return vm.trap("E002", "TVAR_WRITE: expected TVar")
		}
		tv := tvVal.Heap.TVar
		tv.mu.Lock()
		if tv.Closed {
			tv.mu.Unlock()
			return vm.trap("E020", "TVAR_WRITE: TVar is closed")
		}
		vm.recordTVarSnapshot(tv)
		tv.Val = newVal
		tv.Occupied = true
		tv.Version++
		tv.mu.Unlock()
		vm.push(ValUnit())
	case compiler.OpTVAR_TAKE:
		tvVal, _ := vm.pop()
		if tvVal.Tag != TagHEAP || tvVal.Heap.Kind != KindTVar {
			return vm.trap("E002", "TVAR_TAKE: expected TVar")
		}
		tv := tvVal.Heap.TVar
		tv.mu.Lock()
		if tv.Closed {
			tv.mu.Unlock()
			return vm.trap("E020", "TVAR_TAKE: TVar is closed")
		}
		if !tv.Occupied {
			tv.mu.Unlock()
			return vm.trap("E020", "TVAR_TAKE: TVar is empty")
		}
		vm.recordTVarSnapshot(tv)
		val := tv.Val
		tv.Val = ValUnit()
		tv.Occupied = false
		tv.Version++
		tv.mu.Unlock()
		vm.push(val)
	case compiler.OpTVAR_PUT:
		newVal, _ := vm.pop()
		tvVal, _ := vm.pop()
		if tvVal.Tag != TagHEAP || tvVal.Heap.Kind != KindTVar {
			return vm.trap("E002", "TVAR_PUT: expected TVar")
		}
		tv := tvVal.Heap.TVar
		tv.mu.Lock()
		if tv.Closed {
			tv.mu.Unlock()
			return vm.trap("E020", "TVAR_PUT: TVar is closed")
		}
		if tv.Occupied {
			tv.mu.Unlock()
			return vm.trap("E020", "TVAR_PUT: TVar is already occupied")
		}
		vm.recordTVarSnapshot(tv)
		tv.Val = newVal
		tv.Occupied = true
		tv.Version++
		tv.mu.Unlock()
		vm.push(ValUnit())

	// ── Ref ──
	case compiler.OpREF_NEW:
		initial, _ := vm.pop()
		root := vm.root()
		root.mu.Lock()
		ref := &RefCell{ID: root.nextRefID, Val: initial, HandleCount: 1}
		root.nextRefID++
		root.mu.Unlock()
		vm.push(ValRef(ref))
	case compiler.OpREF_READ:
		v, _ := vm.pop()
		if v.Tag != TagHEAP || v.Heap.Kind != KindRef {
			return vm.trap("E002", "REF_READ: expected Ref")
		}
		vm.push(v.Heap.Ref.Val)
	case compiler.OpREF_WRITE:
		newVal, _ := vm.pop()
		refVal, _ := vm.pop()
		if refVal.Tag != TagHEAP || refVal.Heap.Kind != KindRef {
			return vm.trap("E002", "REF_WRITE: expected Ref")
		}
		refVal.Heap.Ref.Val = newVal
		vm.push(ValUnit())
	case compiler.OpREF_CAS, compiler.OpREF_MODIFY:
		return vm.trap("E000", "advanced Ref operations not yet implemented in Go runtime")
	case compiler.OpREF_CLOSE:
		v, _ := vm.pop()
		if v.Tag == TagHEAP && v.Heap.Kind == KindRef {
			v.Heap.Ref.HandleCount--
			if v.Heap.Ref.HandleCount <= 0 {
				v.Heap.Ref.Closed = true
			}
		} else if v.Tag == TagHEAP && v.Heap.Kind == KindTVar {
			v.Heap.TVar.HandleCount--
			if v.Heap.TVar.HandleCount <= 0 {
				v.Heap.TVar.Closed = true
			}
		}
		vm.push(ValUnit())

	// ── Iterator ──
	case compiler.OpITER_NEW:
		cleanupFn, _ := vm.pop()
		generatorFn, _ := vm.pop()
		root := vm.root()
		root.mu.Lock()
		iter := &IteratorState{
			ID: root.nextIterID, GeneratorFn: generatorFn, CleanupFn: cleanupFn,
		}
		root.nextIterID++
		root.mu.Unlock()
		vm.push(ValIter(iter))
	case compiler.OpITER_NEXT:
		return vm.opIterNext(code)
	case compiler.OpITER_CLOSE:
		v, _ := vm.pop()
		if v.Tag == TagHEAP && v.Heap.Kind == KindIterator {
			v.Heap.Iter.Closed = true
			v.Heap.Iter.Done = true
		}
		vm.push(ValUnit())

	default:
		return vm.trap("E000", fmt.Sprintf("unknown opcode 0x%02x", opcode))
	}
	return nil
}

// ── Call / Return Mechanics ──

func (vm *VM) doCall(wordID int) error {
	target, ok := vm.wordMap[wordID]
	if !ok {
		if wordID < 300 {
			return vm.dispatchBuiltin(wordID)
		}
		return vm.trap("E010", fmt.Sprintf("CALL: word ID %d not found", wordID))
	}
	if len(vm.callStack) >= maxCallDepth {
		return vm.trap("E008", "stack overflow: max call depth exceeded")
	}
	vm.callStack = append(vm.callStack, CallFrame{
		ReturnIP:     vm.ip,
		ReturnWordID: vm.currentWord.WordID,
		Locals:       vm.currentFrame().Locals,
		StackBase:    len(vm.dataStack),
	})
	vm.currentWord = target
	vm.ip = 0
	frame := &vm.callStack[len(vm.callStack)-1]
	frame.Locals = make([]Value, target.LocalCount)
	for i := range frame.Locals {
		frame.Locals[i] = ValUnit()
	}
	return nil
}

func (vm *VM) doReturn() {
	frame := vm.callStack[len(vm.callStack)-1]
	vm.callStack = vm.callStack[:len(vm.callStack)-1]
	parent := vm.wordMap[frame.ReturnWordID]
	vm.currentWord = parent
	vm.ip = frame.ReturnIP
}

func (vm *VM) doTailCall(wordID int) error {
	target, ok := vm.wordMap[wordID]
	if !ok {
		if wordID < 256 {
			if err := vm.dispatchBuiltin(wordID); err != nil {
				return err
			}
			if len(vm.callStack) == 0 {
				vm.halted = true
				return nil
			}
			vm.doReturn()
			return nil
		}
		return vm.trap("E010", fmt.Sprintf("TAIL_CALL: word ID %d not found", wordID))
	}
	vm.currentWord = target
	vm.ip = 0
	frame := vm.currentFrame()
	frame.Locals = make([]Value, target.LocalCount)
	for i := range frame.Locals {
		frame.Locals[i] = ValUnit()
	}
	return nil
}

func (vm *VM) doCallDyn(callee Value) error {
	switch callee.Tag {
	case TagQUOTE:
		return vm.doCall(callee.WordID)
	case TagHEAP:
		if callee.Heap.Kind == KindClosure {
			for _, cap := range callee.Heap.Captures {
				vm.push(cap)
			}
			return vm.doCall(callee.Heap.WordID)
		}
	}
	return vm.trap("E002", "CALL_DYN: expected quote or closure")
}

func (vm *VM) doTailCallDyn(callee Value) error {
	switch callee.Tag {
	case TagQUOTE:
		return vm.doTailCall(callee.WordID)
	case TagHEAP:
		if callee.Heap.Kind == KindClosure {
			for _, cap := range callee.Heap.Captures {
				vm.push(cap)
			}
			return vm.doTailCall(callee.Heap.WordID)
		}
	}
	return vm.trap("E002", "TAIL_CALL_DYN: expected quote or closure")
}

// callBuiltinFn calls a closure/quote synchronously and returns the result.
func (vm *VM) callBuiltinFn(fn Value, args []Value) (Value, error) {
	for _, arg := range args {
		vm.push(arg)
	}
	savedCallDepth := len(vm.callStack)
	if err := vm.doCallDyn(fn); err != nil {
		return ValUnit(), err
	}
	for !vm.halted && len(vm.callStack) > savedCallDepth {
		word := vm.currentWord
		code := word.Code
		if vm.ip >= len(code) {
			if len(vm.callStack) <= savedCallDepth {
				break
			}
			vm.doReturn()
			continue
		}
		op := code[vm.ip]
		vm.ip++
		if err := vm.dispatch(op, code); err != nil {
			return ValUnit(), err
		}
	}
	v, _ := vm.pop()
	return v, nil
}

// dispatchMethodSync calls an interface method synchronously.
func (vm *VM) dispatchMethodSync(methodName string, args []Value) (Value, error) {
	typeTag := vm.runtimeTypeTag(args[0])
	methodImpls, ok := vm.dispatchTbl[methodName]
	if !ok {
		return ValUnit(), vm.trap("E212", fmt.Sprintf("no impls registered for method '%s'", methodName))
	}
	wordID, ok := methodImpls[typeTag]
	if !ok {
		return ValUnit(), vm.trap("E212", fmt.Sprintf("no impl of '%s' for type '%s'", methodName, typeTag))
	}
	for _, arg := range args {
		vm.push(arg)
	}
	savedCallDepth := len(vm.callStack)
	if err := vm.doCall(wordID); err != nil {
		return ValUnit(), err
	}
	for !vm.halted && len(vm.callStack) > savedCallDepth {
		word := vm.currentWord
		code := word.Code
		if vm.ip >= len(code) {
			if len(vm.callStack) <= savedCallDepth {
				break
			}
			vm.doReturn()
			continue
		}
		op := code[vm.ip]
		vm.ip++
		if err := vm.dispatch(op, code); err != nil {
			return ValUnit(), err
		}
	}
	v, _ := vm.pop()
	return v, nil
}

// ── Effect Handling ──

func (vm *VM) doEffectPerform(effectID int, args []Value) bool {
	handlerIdx := -1
	for i := len(vm.handlerStack) - 1; i >= 0; i-- {
		if vm.handlerStack[i].EffectID == effectID {
			handlerIdx = i
			break
		}
	}
	if handlerIdx == -1 {
		return false
	}

	handler := vm.handlerStack[handlerIdx]
	performLocals := copyLocals(vm.currentFrame().Locals)

	contDataStack := make([]Value, len(vm.dataStack)-handler.DataStackDepth)
	copy(contDataStack, vm.dataStack[handler.DataStackDepth:])
	vm.dataStack = vm.dataStack[:handler.DataStackDepth]

	contCallStack := make([]CallFrame, len(vm.callStack)-handler.CallStackDepth)
	for i, f := range vm.callStack[handler.CallStackDepth:] {
		contCallStack[i] = CallFrame{
			ReturnIP: f.ReturnIP, ReturnWordID: f.ReturnWordID,
			Locals: copyLocals(f.Locals), StackBase: f.StackBase,
		}
	}
	vm.callStack = vm.callStack[:handler.CallStackDepth]

	contHandlerStack := make([]HandlerFrame, len(vm.handlerStack)-handler.HandlerStackDepth)
	copy(contHandlerStack, vm.handlerStack[handler.HandlerStackDepth:])
	vm.handlerStack = vm.handlerStack[:handler.HandlerStackDepth]

	continuation := ValContinuation(&ContinuationData{
		DataStack:        contDataStack,
		CallStack:        contCallStack,
		HandlerStack:     contHandlerStack,
		IP:               vm.ip,
		WordID:           vm.currentWord.WordID,
		Locals:           performLocals,
		BaseDataDepth:    handler.DataStackDepth,
		BaseCallDepth:    handler.CallStackDepth,
		BaseHandlerDepth: handler.HandlerStackDepth,
	})

	handlerWord := vm.wordMap[handler.WordID]
	vm.currentWord = handlerWord
	vm.ip = handler.HandlerOffset
	vm.currentFrame().Locals = copyLocals(handler.Locals)

	for _, arg := range args {
		vm.push(arg)
	}
	vm.push(continuation)
	return true
}

func (vm *VM) doRaise(message string) bool {
	raiseIdx := -1
	for i, s := range vm.strings {
		if s == "raise" {
			raiseIdx = i
			break
		}
	}
	if raiseIdx == -1 {
		return false
	}
	return vm.doEffectPerform(raiseIdx, []Value{ValStr(message)})
}

// ── Builtin Dispatch (word IDs 0-299) ──

func (vm *VM) dispatchBuiltin(wordID int) error {
	switch wordID {
	case 1: // map
		return vm.builtinMap()
	case 2: // filter
		return vm.builtinFilter()
	case 3: // fold
		return vm.builtinFold()
	case 4: // flat-map
		return vm.builtinFlatMap()
	case 5: // range
		return vm.builtinRange()
	case 6: // zip
		return vm.builtinZip()
	case 7: // task-group
		return vm.builtinTaskGroup()
	case 8: // shield
		return vm.builtinShield()
	case 9: // check-cancel
		vm.push(ValUnit())
		return nil

	// STM builtins
	case 65: // atomically
		return vm.builtinAtomically()
	case 66: // or-else
		return vm.builtinOrElse()
	case 67: // retry
		return vm.trap("E020", "STM retry: no alternative branch (single-threaded runtime)")

	// Cmp builtins
	case 230, 231, 232: // cmp$Int, cmp$Rat, cmp$Str
		return vm.builtinCmp()

	// Show/Eq/Clone for records
	case 240:
		return vm.builtinShowRecord()
	case 241:
		return vm.builtinEqRecord()
	case 242:
		return vm.builtinCloneRecord()
	case 243:
		return vm.builtinCmpRecord()
	case 244: // default$Record
		vm.pop()
		vm.push(ValRecord(make(map[string]Value), nil))
		return nil

	// Show/Eq/Clone for lists
	case 250:
		return vm.builtinShowList()
	case 251:
		return vm.builtinEqList()
	case 252:
		return vm.builtinCloneList()

	// Show/Eq/Clone for tuples
	case 253:
		return vm.builtinShowTuple()
	case 254:
		return vm.builtinEqTuple()
	case 255:
		return vm.builtinCloneTuple()

	// Cmp for lists/tuples
	case 256:
		return vm.builtinCmpList()
	case 257:
		return vm.builtinCmpTuple()

	// Clone Ref/TVar
	case 258:
		return vm.builtinCloneRef()
	case 259:
		return vm.builtinCloneTVar()

	// Iterator combinators
	case 70: // iter.of
		return vm.builtinIterOf()
	case 71: // iter.range
		return vm.builtinIterRange()
	case 72: // iter.collect / collect
		return vm.builtinIterCollect()
	case 84: // iter.drain / drain
		return vm.builtinIterDrain()
	case 112: // next
		return vm.builtinIterNextFn()

	// For-loop dispatch
	case 113: // __for_each
		return vm.builtinForEach()
	case 114: // __for_filter
		return vm.builtinForFilter()
	case 115: // __for_fold
		return vm.builtinForFold()

	// HTTP
	case 120: // http.get
		return vm.builtinHttpRequest("GET")
	case 121: // http.post
		return vm.builtinHttpRequest("POST")
	case 122: // http.put
		return vm.builtinHttpRequest("PUT")
	case 123: // http.del
		return vm.builtinHttpRequest("DELETE")
	case 129: // http.set-timeout
		ms, popErr := vm.pop()
		if popErr != nil {
			return popErr
		}
		if ms.Tag != TagINT {
			return vm.trap("E002", "http.set-timeout: expected Int (milliseconds)")
		}
		root := vm.root()
		root.mu.Lock()
		root.httpTimeout = ms.IntVal
		root.mu.Unlock()
		vm.push(ValUnit())
		return nil

	// Process execution
	case 155: // proc.run
		return vm.builtinProcRun()
	case 156: // proc.sh
		return vm.builtinProcSh()
	case 162: // proc.exit
		return vm.builtinProcExit()

	// Filesystem
	case 200: // fs.read
		return vm.builtinFsRead()
	case 201: // fs.write
		return vm.builtinFsWrite()
	case 202: // fs.exists
		return vm.builtinFsExists()
	case 203: // fs.ls
		return vm.builtinFsLs()
	case 204: // fs.mkdir
		return vm.builtinFsMkdir()
	case 205: // fs.rm
		return vm.builtinFsRm()

	// JSON
	case 207: // json.enc
		return vm.builtinJsonEnc()
	case 208: // json.dec
		return vm.builtinJsonDec()
	case 209: // json.get
		return vm.builtinJsonGet()
	case 210: // json.set
		return vm.builtinJsonSet()
	case 211: // json.keys
		return vm.builtinJsonKeys()
	case 212: // json.merge
		return vm.builtinJsonMerge()

	// Environment
	case 214: // env.get
		return vm.builtinEnvGet()
	case 215: // env.set
		return vm.builtinEnvSet()
	case 216: // env.has
		return vm.builtinEnvHas()
	case 217: // env.all
		return vm.builtinEnvAll()

	// Regex
	case 220: // rx.ok
		return vm.builtinRxTest()
	case 221: // rx.find
		return vm.builtinRxFind()
	case 222: // rx.replace
		return vm.builtinRxReplace()
	case 223: // rx.split
		return vm.builtinRxSplit()

	// Math
	case 224: // math.abs
		return vm.builtinMathAbs()
	case 225: // math.min
		return vm.builtinMathMin()
	case 226: // math.max
		return vm.builtinMathMax()
	case 227: // math.floor
		return vm.builtinMathFloor()
	case 228: // math.ceil
		return vm.builtinMathCeil()
	case 229: // math.sqrt
		return vm.builtinMathSqrt()

	default:
		return vm.trap("E010", fmt.Sprintf("unknown builtin word ID %d", wordID))
	}
}

// ── Stack helpers ──

func (vm *VM) push(v Value) {
	vm.dataStack = append(vm.dataStack, v)
}

func (vm *VM) pop() (Value, error) {
	if len(vm.dataStack) == 0 {
		return ValUnit(), vm.trap("E001", "stack underflow")
	}
	v := vm.dataStack[len(vm.dataStack)-1]
	vm.dataStack = vm.dataStack[:len(vm.dataStack)-1]
	return v, nil
}

func (vm *VM) peek() (Value, error) {
	if len(vm.dataStack) == 0 {
		return ValUnit(), vm.trap("E001", "stack underflow (peek)")
	}
	return vm.dataStack[len(vm.dataStack)-1], nil
}

func (vm *VM) popBool() (bool, error) {
	v, err := vm.pop()
	if err != nil {
		return false, err
	}
	if v.Tag != TagBOOL {
		return false, vm.trap("E002", fmt.Sprintf("expected Bool, got %s", tagName(v)))
	}
	return v.BoolVal, nil
}

func (vm *VM) popStr() (string, error) {
	v, err := vm.pop()
	if err != nil {
		return "", err
	}
	if v.Tag != TagSTR {
		return "", vm.trap("E002", fmt.Sprintf("expected Str, got %s", tagName(v)))
	}
	return v.StrVal, nil
}

func (vm *VM) popList() ([]Value, error) {
	v, err := vm.pop()
	if err != nil {
		return nil, err
	}
	if v.Tag != TagHEAP || v.Heap.Kind != KindList {
		return nil, vm.trap("E002", fmt.Sprintf("expected List, got %s", tagName(v)))
	}
	return v.Heap.Items, nil
}

// ── Operand reading ──

func (vm *VM) readU8(code []byte) byte {
	v := code[vm.ip]
	vm.ip++
	return v
}

func (vm *VM) readU16(code []byte) uint16 {
	hi := code[vm.ip]
	lo := code[vm.ip+1]
	vm.ip += 2
	return uint16(hi)<<8 | uint16(lo)
}

func (vm *VM) readU32(code []byte) uint32 {
	b3 := code[vm.ip]
	b2 := code[vm.ip+1]
	b1 := code[vm.ip+2]
	b0 := code[vm.ip+3]
	vm.ip += 4
	return uint32(b3)<<24 | uint32(b2)<<16 | uint32(b1)<<8 | uint32(b0)
}

// ── Frame access ──

func (vm *VM) currentFrame() *CallFrame {
	if len(vm.callStack) > 0 {
		return &vm.callStack[len(vm.callStack)-1]
	}
	if vm.topFrame == nil {
		locals := make([]Value, maxLocals)
		for i := range locals {
			locals[i] = ValUnit()
		}
		vm.topFrame = &CallFrame{Locals: locals}
	}
	return vm.topFrame
}

// ── Arithmetic helper ──

func (vm *VM) binaryArith(fn func(a, b float64) float64) error {
	b, err := vm.pop()
	if err != nil {
		return err
	}
	a, err := vm.pop()
	if err != nil {
		return err
	}
	av, aok := NumericValue(a)
	bv, bok := NumericValue(b)
	if !aok || !bok {
		return vm.trap("E002", "expected numeric type")
	}
	if a.Tag == TagRAT || b.Tag == TagRAT {
		result := fn(av, bv)
		if math.IsInf(result, 0) || math.IsNaN(result) {
			return vm.trap("E003", "arithmetic overflow (rational)")
		}
		vm.push(ValRat(result))
	} else {
		result := fn(av, bv)
		intResult := int(result)
		// Overflow check: verify the float64 round-trip is exact and within int range
		if result != float64(intResult) || result > 9.2e18 || result < -9.2e18 {
			return vm.trap("E003", "integer overflow")
		}
		vm.push(ValInt(intResult))
	}
	return nil
}

// ── Misc helpers ──

func numVal(v Value) int {
	switch v.Tag {
	case TagINT:
		return v.IntVal
	case TagRAT:
		return int(v.RatVal)
	}
	return 0
}

func tagName(v Value) string {
	switch v.Tag {
	case TagINT:
		return "Int"
	case TagRAT:
		return "Rat"
	case TagBOOL:
		return "Bool"
	case TagSTR:
		return "Str"
	case TagBYTE:
		return "Byte"
	case TagUNIT:
		return "Unit"
	case TagQUOTE:
		return "Quote"
	case TagHEAP:
		return v.Heap.Kind
	}
	return "?"
}

func (vm *VM) runtimeTypeTag(v Value) string {
	switch v.Tag {
	case TagINT:
		return "Int"
	case TagRAT:
		return "Rat"
	case TagBOOL:
		return "Bool"
	case TagSTR:
		return "Str"
	case TagBYTE:
		return "Byte"
	case TagUNIT:
		return "Unit"
	case TagQUOTE:
		return "Fn"
	case TagHEAP:
		switch v.Heap.Kind {
		case KindList:
			return "List"
		case KindTuple:
			return "Tuple"
		case KindRecord:
			return "Record"
		case KindUnion:
			if v.Heap.VariantTag < len(vm.variantNames) && vm.variantNames[v.Heap.VariantTag] != "" {
				return vm.variantNames[v.Heap.VariantTag]
			}
			return "?"
		case KindClosure:
			return "Fn"
		case KindContinuation:
			return "Continuation"
		case KindFuture:
			return "Future"
		case KindTVar:
			return "TVar"
		case KindIterator:
			return "Iter"
		case KindRef:
			return "Ref"
		case KindSender:
			return "Sender"
		case KindReceiver:
			return "Receiver"
		case KindSelectSet:
			return "SelectSet"
		}
	}
	return "?"
}

func (vm *VM) findVariantTag(name string) (int, error) {
	for i, n := range vm.variantNames {
		if n == name {
			return i, nil
		}
	}
	return -1, vm.trap("E010", fmt.Sprintf("variant '%s' not found in variant names", name))
}

func (vm *VM) trap(code, message string) *VMTrap {
	word := "unknown"
	if vm.currentWord != nil {
		word = vm.currentWord.Name
	}
	return &VMTrap{Code: code, Message: message, Word: word, Offset: vm.ip}
}

func copyLocals(src []Value) []Value {
	if src == nil {
		return nil
	}
	dst := make([]Value, len(src))
	copy(dst, src)
	return dst
}

// ── Builtin implementations ──

func (vm *VM) builtinMap() error {
	fn, _ := vm.pop()
	collection, _ := vm.pop()
	if collection.Tag == TagHEAP && collection.Heap.Kind == KindIterator {
		iter := collection.Heap.Iter
		if iter.Closed {
			return vm.trap("E017", "map: iterator is closed")
		}
		var results []Value
		for {
			v := vm.iterNext(iter, collection)
			if v == nil {
				break
			}
			r, err := vm.callBuiltinFn(fn, []Value{*v})
			if err != nil {
				return err
			}
			results = append(results, r)
		}
		iter.Done = true
		iter.Closed = true
		vm.push(ValList(results))
		return nil
	}
	if collection.Tag != TagHEAP || collection.Heap.Kind != KindList {
		return vm.trap("E002", fmt.Sprintf("expected List or Iterator, got %s", tagName(collection)))
	}
	list := collection.Heap.Items
	results := make([]Value, len(list))
	for i, el := range list {
		r, err := vm.callBuiltinFn(fn, []Value{el})
		if err != nil {
			return err
		}
		results[i] = r
	}
	vm.push(ValList(results))
	return nil
}

func (vm *VM) builtinFilter() error {
	fn, _ := vm.pop()
	collection, _ := vm.pop()
	if collection.Tag == TagHEAP && collection.Heap.Kind == KindIterator {
		iter := collection.Heap.Iter
		if iter.Closed {
			return vm.trap("E017", "filter: iterator is closed")
		}
		var results []Value
		for {
			v := vm.iterNext(iter, collection)
			if v == nil {
				break
			}
			r, err := vm.callBuiltinFn(fn, []Value{*v})
			if err != nil {
				return err
			}
			if r.Tag == TagBOOL && r.BoolVal {
				results = append(results, *v)
			}
		}
		iter.Done = true
		iter.Closed = true
		vm.push(ValList(results))
		return nil
	}
	if collection.Tag != TagHEAP || collection.Heap.Kind != KindList {
		return vm.trap("E002", fmt.Sprintf("expected List or Iterator, got %s", tagName(collection)))
	}
	list := collection.Heap.Items
	var results []Value
	for _, el := range list {
		r, err := vm.callBuiltinFn(fn, []Value{el})
		if err != nil {
			return err
		}
		if r.Tag == TagBOOL && r.BoolVal {
			results = append(results, el)
		}
	}
	vm.push(ValList(results))
	return nil
}

func (vm *VM) builtinFold() error {
	fn, _ := vm.pop()
	init, _ := vm.pop()
	collection, _ := vm.pop()
	if collection.Tag == TagHEAP && collection.Heap.Kind == KindIterator {
		iter := collection.Heap.Iter
		if iter.Closed {
			return vm.trap("E017", "fold: iterator is closed")
		}
		acc := init
		for {
			v := vm.iterNext(iter, collection)
			if v == nil {
				break
			}
			r, err := vm.callBuiltinFn(fn, []Value{acc, *v})
			if err != nil {
				return err
			}
			acc = r
		}
		iter.Done = true
		iter.Closed = true
		vm.push(acc)
		return nil
	}
	if collection.Tag != TagHEAP || collection.Heap.Kind != KindList {
		return vm.trap("E002", fmt.Sprintf("expected List or Iterator, got %s", tagName(collection)))
	}
	acc := init
	for _, el := range collection.Heap.Items {
		r, err := vm.callBuiltinFn(fn, []Value{acc, el})
		if err != nil {
			return err
		}
		acc = r
	}
	vm.push(acc)
	return nil
}

func (vm *VM) builtinFlatMap() error {
	fn, _ := vm.pop()
	list, err := vm.popList()
	if err != nil {
		return err
	}
	var results []Value
	for _, el := range list {
		inner, err := vm.callBuiltinFn(fn, []Value{el})
		if err != nil {
			return err
		}
		if inner.Tag != TagHEAP || inner.Heap.Kind != KindList {
			return vm.trap("E200", fmt.Sprintf("flat-map: function must return a list, got %s", tagName(inner)))
		}
		results = append(results, inner.Heap.Items...)
	}
	vm.push(ValList(results))
	return nil
}

func (vm *VM) builtinRange() error {
	endVal, _ := vm.pop()
	startVal, _ := vm.pop()
	start := numVal(startVal)
	end := numVal(endVal)
	if end < start {
		vm.push(ValList(nil))
		return nil
	}
	items := make([]Value, 0, end-start+1)
	for i := start; i <= end; i++ {
		items = append(items, ValInt(i))
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinZip() error {
	ys, err := vm.popList()
	if err != nil {
		return err
	}
	xs, err := vm.popList()
	if err != nil {
		return err
	}
	n := len(xs)
	if len(ys) < n {
		n = len(ys)
	}
	items := make([]Value, n)
	for i := 0; i < n; i++ {
		items[i] = ValTuple([]Value{xs[i], ys[i]})
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinTaskGroup() error {
	bodyFn, _ := vm.pop()
	root := vm.root()
	root.mu.Lock()
	groupID := root.nextGroupID
	root.nextGroupID++
	group := &taskGroup{
		id: groupID, parentTaskID: vm.currentTaskID,
		childTaskIDs: make(map[int]bool), dataStackDepth: len(vm.dataStack),
	}
	root.taskGroups[groupID] = group
	root.asyncMode = true
	root.mu.Unlock()
	vm.activeGroups = append(vm.activeGroups, groupID)

	result, bodyErr := vm.callBuiltinFn(bodyFn, nil)

	// Pop group
	if len(vm.activeGroups) > 0 {
		vm.activeGroups = vm.activeGroups[:len(vm.activeGroups)-1]
	}

	// Cancel still-running children and wait for them
	root.mu.Lock()
	for childID := range group.childTaskIDs {
		if child, ok := root.tasks[childID]; ok && (child.status == "running" || child.status == "suspended") {
			child.cancelFlag = true
			if child.shieldDepth == 0 && child.cancel != nil {
				child.cancel()
			}
		}
	}
	root.mu.Unlock()

	// Wait for all children to finish
	for childID := range group.childTaskIDs {
		root.mu.Lock()
		child, ok := root.tasks[childID]
		root.mu.Unlock()
		if ok && (child.status == "running" || child.status == "suspended") {
			<-child.resultCh
		}
	}

	// Check for child failures
	var firstChildErr string
	root.mu.Lock()
	for childID := range group.childTaskIDs {
		if child, ok := root.tasks[childID]; ok && child.status == "failed" {
			firstChildErr = child.errMsg
			if firstChildErr == "" {
				firstChildErr = "child task failed"
			}
			break
		}
	}
	delete(root.taskGroups, groupID)
	root.mu.Unlock()

	if bodyErr != nil {
		return bodyErr
	}
	if firstChildErr != "" {
		if !vm.doRaise(firstChildErr) {
			return vm.trap("E014", firstChildErr)
		}
		return nil
	}
	vm.push(result)
	return nil
}

func (vm *VM) builtinShield() error {
	bodyFn, _ := vm.pop()
	if vm.currentTaskID != 0 {
		root := vm.root()
		root.mu.Lock()
		t, ok := root.tasks[vm.currentTaskID]
		if ok {
			t.shieldDepth++
		}
		root.mu.Unlock()
		if ok {
			defer func() {
				root.mu.Lock()
				t.shieldDepth--
				if t.shieldDepth == 0 && t.cancelFlag && t.cancel != nil {
					t.cancel()
				}
				root.mu.Unlock()
			}()
		}
	}
	result, err := vm.callBuiltinFn(bodyFn, nil)
	if err != nil {
		return err
	}
	vm.push(result)
	return nil
}

func (vm *VM) builtinCmp() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	ltTag, err := vm.findVariantTag("Lt")
	if err != nil {
		return err
	}
	eqTag, err := vm.findVariantTag("Eq_")
	if err != nil {
		return err
	}
	gtTag, err := vm.findVariantTag("Gt")
	if err != nil {
		return err
	}
	var av, bv interface{}
	if a.Tag == TagSTR {
		av = a.StrVal
		bv = b.StrVal
	} else {
		av2, _ := NumericValue(a)
		bv2, _ := NumericValue(b)
		av = av2
		bv = bv2
	}
	switch {
	case fmt.Sprint(av) < fmt.Sprint(bv):
		vm.push(ValUnion(ltTag, nil))
	case fmt.Sprint(av) > fmt.Sprint(bv):
		vm.push(ValUnion(gtTag, nil))
	default:
		vm.push(ValUnion(eqTag, nil))
	}
	return nil
}

func (vm *VM) builtinShowRecord() error {
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "show$Record: expected record")
	}
	keys := make([]string, 0, len(rec.Heap.Fields))
	for k := range rec.Heap.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		v := rec.Heap.Fields[k]
		shown, err := vm.dispatchMethodSync("show", []Value{v})
		if err != nil {
			return err
		}
		if shown.Tag != TagSTR {
			return vm.trap("E002", "show$Record: show did not return Str")
		}
		parts[i] = k + ": " + shown.StrVal
	}
	vm.push(ValStr("{" + strings.Join(parts, ", ") + "}"))
	return nil
}

func (vm *VM) builtinEqRecord() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindRecord || b.Tag != TagHEAP || b.Heap.Kind != KindRecord {
		vm.push(ValBool(false))
		return nil
	}
	if len(a.Heap.Fields) != len(b.Heap.Fields) {
		vm.push(ValBool(false))
		return nil
	}
	for k, av := range a.Heap.Fields {
		bv, ok := b.Heap.Fields[k]
		if !ok {
			vm.push(ValBool(false))
			return nil
		}
		result, err := vm.dispatchMethodSync("eq", []Value{av, bv})
		if err != nil {
			return err
		}
		if result.Tag != TagBOOL || !result.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinCloneRecord() error {
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "clone$Record: expected record")
	}
	newFields := make(map[string]Value, len(rec.Heap.Fields))
	for k, v := range rec.Heap.Fields {
		cloned, err := vm.dispatchMethodSync("clone", []Value{v})
		if err != nil {
			return err
		}
		newFields[k] = cloned
	}
	vm.push(ValRecord(newFields, rec.Heap.FieldOrder))
	return nil
}

func (vm *VM) builtinCmpRecord() error {
	bRec, _ := vm.pop()
	aRec, _ := vm.pop()
	if aRec.Tag != TagHEAP || aRec.Heap.Kind != KindRecord || bRec.Tag != TagHEAP || bRec.Heap.Kind != KindRecord {
		return vm.trap("E002", "cmp$Record: expected records")
	}
	ltTag, _ := vm.findVariantTag("Lt")
	eqTag, _ := vm.findVariantTag("Eq_")
	gtTag, _ := vm.findVariantTag("Gt")

	aKeys := make([]string, 0, len(aRec.Heap.Fields))
	for k := range aRec.Heap.Fields {
		aKeys = append(aKeys, k)
	}
	sort.Strings(aKeys)
	bKeys := make([]string, 0, len(bRec.Heap.Fields))
	for k := range bRec.Heap.Fields {
		bKeys = append(bKeys, k)
	}
	sort.Strings(bKeys)

	minLen := len(aKeys)
	if len(bKeys) < minLen {
		minLen = len(bKeys)
	}
	for i := 0; i < minLen; i++ {
		if aKeys[i] < bKeys[i] {
			vm.push(ValUnion(ltTag, nil))
			return nil
		}
		if aKeys[i] > bKeys[i] {
			vm.push(ValUnion(gtTag, nil))
			return nil
		}
	}
	if len(aKeys) < len(bKeys) {
		vm.push(ValUnion(ltTag, nil))
		return nil
	}
	if len(aKeys) > len(bKeys) {
		vm.push(ValUnion(gtTag, nil))
		return nil
	}
	for _, k := range aKeys {
		r, err := vm.dispatchMethodSync("cmp", []Value{aRec.Heap.Fields[k], bRec.Heap.Fields[k]})
		if err != nil {
			return err
		}
		if r.Tag == TagHEAP && r.Heap.Kind == KindUnion && r.Heap.VariantTag != eqTag {
			vm.push(r)
			return nil
		}
	}
	vm.push(ValUnion(eqTag, nil))
	return nil
}

func (vm *VM) builtinShowList() error {
	lst, _ := vm.pop()
	if lst.Tag != TagHEAP || lst.Heap.Kind != KindList {
		return vm.trap("E002", "show$List: expected list")
	}
	parts := make([]string, len(lst.Heap.Items))
	for i, item := range lst.Heap.Items {
		shown, err := vm.dispatchMethodSync("show", []Value{item})
		if err != nil {
			return err
		}
		if shown.Tag != TagSTR {
			return vm.trap("E002", "show$List: show did not return Str")
		}
		parts[i] = shown.StrVal
	}
	vm.push(ValStr("[" + strings.Join(parts, ", ") + "]"))
	return nil
}

func (vm *VM) builtinEqList() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindList || b.Tag != TagHEAP || b.Heap.Kind != KindList {
		vm.push(ValBool(false))
		return nil
	}
	if len(a.Heap.Items) != len(b.Heap.Items) {
		vm.push(ValBool(false))
		return nil
	}
	for i := range a.Heap.Items {
		r, err := vm.dispatchMethodSync("eq", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag != TagBOOL || !r.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinCloneList() error {
	lst, _ := vm.pop()
	if lst.Tag != TagHEAP || lst.Heap.Kind != KindList {
		return vm.trap("E002", "clone$List: expected list")
	}
	cloned := make([]Value, len(lst.Heap.Items))
	for i, item := range lst.Heap.Items {
		c, err := vm.dispatchMethodSync("clone", []Value{item})
		if err != nil {
			return err
		}
		cloned[i] = c
	}
	vm.push(ValList(cloned))
	return nil
}

func (vm *VM) builtinShowTuple() error {
	tup, _ := vm.pop()
	if tup.Tag != TagHEAP || tup.Heap.Kind != KindTuple {
		return vm.trap("E002", "show$Tuple: expected tuple")
	}
	parts := make([]string, len(tup.Heap.Items))
	for i, item := range tup.Heap.Items {
		shown, err := vm.dispatchMethodSync("show", []Value{item})
		if err != nil {
			return err
		}
		if shown.Tag != TagSTR {
			return vm.trap("E002", "show$Tuple: show did not return Str")
		}
		parts[i] = shown.StrVal
	}
	vm.push(ValStr("(" + strings.Join(parts, ", ") + ")"))
	return nil
}

func (vm *VM) builtinEqTuple() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindTuple || b.Tag != TagHEAP || b.Heap.Kind != KindTuple {
		vm.push(ValBool(false))
		return nil
	}
	if len(a.Heap.Items) != len(b.Heap.Items) {
		vm.push(ValBool(false))
		return nil
	}
	for i := range a.Heap.Items {
		r, err := vm.dispatchMethodSync("eq", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag != TagBOOL || !r.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinCloneTuple() error {
	tup, _ := vm.pop()
	if tup.Tag != TagHEAP || tup.Heap.Kind != KindTuple {
		return vm.trap("E002", "clone$Tuple: expected tuple")
	}
	cloned := make([]Value, len(tup.Heap.Items))
	for i, item := range tup.Heap.Items {
		c, err := vm.dispatchMethodSync("clone", []Value{item})
		if err != nil {
			return err
		}
		cloned[i] = c
	}
	vm.push(ValTuple(cloned))
	return nil
}

func (vm *VM) builtinCmpList() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindList || b.Tag != TagHEAP || b.Heap.Kind != KindList {
		return vm.trap("E002", "cmp$List: expected lists")
	}
	ltTag, _ := vm.findVariantTag("Lt")
	eqTag, _ := vm.findVariantTag("Eq_")
	gtTag, _ := vm.findVariantTag("Gt")
	minLen := len(a.Heap.Items)
	if len(b.Heap.Items) < minLen {
		minLen = len(b.Heap.Items)
	}
	for i := 0; i < minLen; i++ {
		r, err := vm.dispatchMethodSync("cmp", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag == TagHEAP && r.Heap.Kind == KindUnion && r.Heap.VariantTag != eqTag {
			vm.push(r)
			return nil
		}
	}
	if len(a.Heap.Items) < len(b.Heap.Items) {
		vm.push(ValUnion(ltTag, nil))
	} else if len(a.Heap.Items) > len(b.Heap.Items) {
		vm.push(ValUnion(gtTag, nil))
	} else {
		vm.push(ValUnion(eqTag, nil))
	}
	return nil
}

func (vm *VM) builtinCmpTuple() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindTuple || b.Tag != TagHEAP || b.Heap.Kind != KindTuple {
		return vm.trap("E002", "cmp$Tuple: expected tuples")
	}
	ltTag, _ := vm.findVariantTag("Lt")
	eqTag, _ := vm.findVariantTag("Eq_")
	gtTag, _ := vm.findVariantTag("Gt")
	minLen := len(a.Heap.Items)
	if len(b.Heap.Items) < minLen {
		minLen = len(b.Heap.Items)
	}
	for i := 0; i < minLen; i++ {
		r, err := vm.dispatchMethodSync("cmp", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag == TagHEAP && r.Heap.Kind == KindUnion && r.Heap.VariantTag != eqTag {
			vm.push(r)
			return nil
		}
	}
	if len(a.Heap.Items) < len(b.Heap.Items) {
		vm.push(ValUnion(ltTag, nil))
	} else if len(a.Heap.Items) > len(b.Heap.Items) {
		vm.push(ValUnion(gtTag, nil))
	} else {
		vm.push(ValUnion(eqTag, nil))
	}
	return nil
}

func (vm *VM) builtinCloneRef() error {
	v, _ := vm.pop()
	if v.Tag != TagHEAP || v.Heap.Kind != KindRef {
		return vm.trap("E002", "clone$Ref: expected Ref")
	}
	v.Heap.Ref.HandleCount++
	vm.push(ValRef(v.Heap.Ref))
	return nil
}

func (vm *VM) builtinCloneTVar() error {
	v, _ := vm.pop()
	if v.Tag != TagHEAP || v.Heap.Kind != KindTVar {
		return vm.trap("E002", "clone$TVar: expected TVar")
	}
	v.Heap.TVar.HandleCount++
	vm.push(ValTVarVal(v.Heap.TVar))
	return nil
}

// ── STM builtins ──

// tvarSnapshot captures TVar state for rollback on transaction abort.
type tvarSnapshot struct {
	tv       *TVar
	val      Value
	occupied bool
	version  int
}

// recordTVarSnapshot records the pre-modification state of a TVar into the
// transaction write-log. Only the first modification per TVar per transaction
// is recorded — subsequent modifications don't overwrite the original snapshot.
func (vm *VM) recordTVarSnapshot(tv *TVar) {
	if !vm.inTxn {
		return
	}
	for _, s := range vm.txnWriteLog {
		if s.tv == tv {
			return // already recorded
		}
	}
	vm.txnWriteLog = append(vm.txnWriteLog, tvarSnapshot{
		tv: tv, val: tv.Val, occupied: tv.Occupied, version: tv.Version,
	})
}

// restoreTVars rolls back TVars to their pre-transaction state.
func restoreTVars(snaps []tvarSnapshot) {
	for _, s := range snaps {
		s.tv.Val = s.val
		s.tv.Occupied = s.occupied
		s.tv.Version = s.version
	}
}

func (vm *VM) builtinAtomically() error {
	bodyFn, _ := vm.pop()
	prevInTxn := vm.inTxn
	prevLog := vm.txnWriteLog
	vm.inTxn = true
	vm.txnWriteLog = nil
	result, err := vm.callBuiltinFn(bodyFn, nil)
	if err != nil {
		// Transaction aborted — restore all TVars modified during the txn.
		restoreTVars(vm.txnWriteLog)
		vm.inTxn = prevInTxn
		vm.txnWriteLog = prevLog
		return err
	}
	// Commit — merge write-log into parent transaction if nested.
	if prevInTxn {
		prevLog = append(prevLog, vm.txnWriteLog...)
	}
	vm.inTxn = prevInTxn
	vm.txnWriteLog = prevLog
	vm.push(result)
	return nil
}

func (vm *VM) builtinOrElse() error {
	altFn, _ := vm.pop()
	bodyFn, _ := vm.pop()
	prevInTxn := vm.inTxn
	prevLog := vm.txnWriteLog
	vm.inTxn = true
	vm.txnWriteLog = nil
	result, err := vm.callBuiltinFn(bodyFn, nil)
	if err != nil {
		// First branch failed — restore TVars and try alternative.
		restoreTVars(vm.txnWriteLog)
		vm.txnWriteLog = nil
		result, err = vm.callBuiltinFn(altFn, nil)
		if err != nil {
			restoreTVars(vm.txnWriteLog)
			vm.inTxn = prevInTxn
			vm.txnWriteLog = prevLog
			return err
		}
	}
	// Commit — merge into parent.
	if prevInTxn {
		prevLog = append(prevLog, vm.txnWriteLog...)
	}
	vm.inTxn = prevInTxn
	vm.txnWriteLog = prevLog
	vm.push(result)
	return nil
}

// ── Iterator builtins ──

func (vm *VM) iterNext(iter *IteratorState, iterVal Value) *Value {
	if iter.Done || iter.Closed {
		return nil
	}
	// Native next function (lazy iterators)
	if iter.NativeNext != nil {
		v := iter.NativeNext()
		return v
	}
	// List-backed fast path
	if iter.Source != nil {
		if iter.Index >= len(iter.Source) {
			iter.Done = true
			return nil
		}
		v := iter.Source[iter.Index]
		iter.Index++
		return &v
	}
	// Buffer-backed
	if len(iter.Buffer) > 0 {
		v := iter.Buffer[0]
		iter.Buffer = iter.Buffer[1:]
		return &v
	}
	if iter.Done {
		return nil
	}
	// Call generator function
	result, err := vm.callBuiltinFn(iter.GeneratorFn, []Value{iterVal})
	if err != nil {
		iter.Done = true
		return nil
	}
	if result.Tag == TagHEAP && result.Heap.Kind == KindUnion {
		if result.Heap.VariantTag < len(vm.variantNames) {
			name := vm.variantNames[result.Heap.VariantTag]
			if name == "None" || name == "IterDone" {
				iter.Done = true
				return nil
			}
			if name == "Some" && len(result.Heap.UFields) > 0 {
				v := result.Heap.UFields[0]
				return &v
			}
		}
	}
	return &result
}

func (vm *VM) builtinIterOf() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	src := make([]Value, len(list))
	copy(src, list)
	root := vm.root()
	root.mu.Lock()
	iter := &IteratorState{
		ID: root.nextIterID, GeneratorFn: ValUnit(), CleanupFn: ValUnit(),
		Source: src,
	}
	root.nextIterID++
	root.mu.Unlock()
	vm.push(ValIter(iter))
	return nil
}

func (vm *VM) builtinIterRange() error {
	endVal, _ := vm.pop()
	startVal, _ := vm.pop()
	start := numVal(startVal)
	end := numVal(endVal)
	current := start
	root := vm.root()
	root.mu.Lock()
	iter := &IteratorState{
		ID: root.nextIterID, GeneratorFn: ValUnit(), CleanupFn: ValUnit(),
		NativeNext: func() *Value {
			if current >= end {
				return nil
			}
			v := ValInt(current)
			current++
			return &v
		},
	}
	root.nextIterID++
	root.mu.Unlock()
	vm.push(ValIter(iter))
	return nil
}

func (vm *VM) builtinIterCollect() error {
	iterVal, _ := vm.pop()
	if iterVal.Tag != TagHEAP || iterVal.Heap.Kind != KindIterator {
		return vm.trap("E002", "iter.collect: expected Iterator")
	}
	iter := iterVal.Heap.Iter
	if iter.Closed {
		return vm.trap("E017", "iter.collect: iterator is closed")
	}
	var result []Value
	for {
		v := vm.iterNext(iter, iterVal)
		if v == nil {
			break
		}
		result = append(result, *v)
	}
	iter.Done = true
	iter.Closed = true
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinIterDrain() error {
	iterVal, _ := vm.pop()
	if iterVal.Tag != TagHEAP || iterVal.Heap.Kind != KindIterator {
		return vm.trap("E002", "iter.drain: expected Iterator")
	}
	iter := iterVal.Heap.Iter
	if iter.Closed {
		return vm.trap("E017", "iter.drain: iterator is closed")
	}
	for vm.iterNext(iter, iterVal) != nil {
	}
	iter.Done = true
	iter.Closed = true
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinIterNextFn() error {
	iterVal, _ := vm.pop()
	if iterVal.Tag != TagHEAP || iterVal.Heap.Kind != KindIterator {
		return vm.trap("E002", "next: expected Iterator")
	}
	iter := iterVal.Heap.Iter
	if iter.Closed {
		return vm.trap("E017", "next: iterator is closed")
	}
	v := vm.iterNext(iter, iterVal)
	if v == nil {
		if !vm.doRaise("IterDone") {
			return vm.trap("E016", "iterator exhausted")
		}
		return nil
	}
	vm.push(iterVal)
	vm.push(*v)
	return nil
}

func (vm *VM) opIterNext(code []byte) error {
	iterVal, _ := vm.pop()
	if iterVal.Tag != TagHEAP || iterVal.Heap.Kind != KindIterator {
		return vm.trap("E002", "ITER_NEXT: expected Iterator")
	}
	iter := iterVal.Heap.Iter
	if iter.Closed {
		return vm.trap("E017", "ITER_NEXT: iterator is closed")
	}
	// List-backed fast path
	if iter.Source != nil {
		if iter.Index >= len(iter.Source) {
			iter.Done = true
			if !vm.doRaise("IterDone") {
				return vm.trap("E016", "iterator exhausted")
			}
			return nil
		}
		v := iter.Source[iter.Index]
		iter.Index++
		vm.push(iterVal)
		vm.push(v)
		return nil
	}
	v := vm.iterNext(iter, iterVal)
	if v == nil {
		if !vm.doRaise("IterDone") {
			return vm.trap("E016", "iterator exhausted")
		}
		return nil
	}
	vm.push(iterVal)
	vm.push(*v)
	return nil
}

// For-loop dispatch
func (vm *VM) builtinForEach() error {
	fn, _ := vm.pop()
	collection, _ := vm.pop()
	if collection.Tag == TagHEAP && collection.Heap.Kind == KindIterator {
		iter := collection.Heap.Iter
		var results []Value
		for {
			v := vm.iterNext(iter, collection)
			if v == nil {
				break
			}
			r, err := vm.callBuiltinFn(fn, []Value{*v})
			if err != nil {
				return err
			}
			results = append(results, r)
		}
		iter.Done = true
		iter.Closed = true
		vm.push(ValList(results))
		return nil
	}
	if collection.Tag != TagHEAP || collection.Heap.Kind != KindList {
		return vm.trap("E002", "expected List or Iterator")
	}
	results := make([]Value, 0, len(collection.Heap.Items))
	for _, el := range collection.Heap.Items {
		r, err := vm.callBuiltinFn(fn, []Value{el})
		if err != nil {
			return err
		}
		results = append(results, r)
	}
	vm.push(ValList(results))
	return nil
}

func (vm *VM) builtinForFilter() error {
	return vm.builtinFilter()
}

func (vm *VM) builtinForFold() error {
	return vm.builtinFold()
}

// checkCancellation checks if the current task has been cancelled and is unshielded.
func (vm *VM) checkCancellation() error {
	if vm.currentTaskID != 0 {
		root := vm.root()
		root.mu.Lock()
		t, ok := root.tasks[vm.currentTaskID]
		root.mu.Unlock()
		if ok && t.cancelFlag && t.shieldDepth == 0 {
			root.mu.Lock()
			t.status = "cancelled"
			root.mu.Unlock()
			return vm.trap("E011", "task cancelled")
		}
	}
	return nil
}

// ── Async (goroutine-backed) ──

func (vm *VM) opTaskSpawn() error {
	closure, _ := vm.pop()
	if closure.Tag != TagQUOTE && !(closure.Tag == TagHEAP && closure.Heap.Kind == KindClosure) {
		return vm.trap("E002", "TASK_SPAWN: expected closure or quote")
	}

	root := vm.root()
	root.mu.Lock()
	root.asyncMode = true
	taskID := root.nextTaskID
	root.nextTaskID++

	groupID := 0
	if len(vm.activeGroups) > 0 {
		groupID = vm.activeGroups[len(vm.activeGroups)-1]
	}

	childCtx, childCancel := context.WithCancel(vm.ctx)
	task := &taskState{
		id: taskID, status: "running",
		parentID: vm.currentTaskID, groupID: groupID,
		resultCh: make(chan taskResult, 1),
		ctx: childCtx, cancel: childCancel,
	}

	if closure.Tag == TagQUOTE {
		word := root.wordMap[closure.WordID]
		if word == nil {
			root.mu.Unlock()
			return vm.trap("E010", fmt.Sprintf("TASK_SPAWN: word %d not found", closure.WordID))
		}
		task.currentWord = word
	} else {
		cls := closure.Heap
		word := root.wordMap[cls.WordID]
		if word == nil {
			root.mu.Unlock()
			return vm.trap("E010", fmt.Sprintf("TASK_SPAWN: word %d not found", cls.WordID))
		}
		task.currentWord = word
		task.dataStack = append(task.dataStack, cls.Captures...)
	}

	root.tasks[taskID] = task
	if groupID != 0 {
		if g, ok := root.taskGroups[groupID]; ok {
			g.childTaskIDs[taskID] = true
		}
	}
	root.mu.Unlock()

	// Launch goroutine for real concurrent execution
	child := vm.spawnTaskVM(task)
	go func() {
		execErr := child.execute()
		var res taskResult
		if execErr == nil && len(child.dataStack) > 0 {
			v := child.dataStack[len(child.dataStack)-1]
			res.value = &v
		} else if execErr != nil {
			res.err = execErr
		}

		root.mu.Lock()
		if execErr == nil {
			task.status = "completed"
			task.result = res.value
		} else {
			task.status = "failed"
			task.errMsg = execErr.Error()
		}
		root.mu.Unlock()

		task.resultCh <- res
	}()

	vm.push(ValFuture(taskID))
	return nil
}

func (vm *VM) opTaskAwait() error {
	futureVal, _ := vm.pop()
	if futureVal.Tag != TagHEAP || futureVal.Heap.Kind != KindFuture {
		return vm.trap("E002", "TASK_AWAIT: expected Future")
	}
	if err := vm.checkCancellation(); err != nil {
		return err
	}

	root := vm.root()
	root.mu.Lock()
	task, ok := root.tasks[futureVal.Heap.TaskID]
	status := ""
	if ok {
		status = task.status
	}
	root.mu.Unlock()

	if !ok {
		return vm.trap("E013", "TASK_AWAIT: future references unknown task")
	}

	// Block until goroutine finishes (if still running)
	if status == "running" || status == "suspended" {
		<-task.resultCh
	}

	// Read final status
	root.mu.Lock()
	status = task.status
	result := task.result
	errMsg := task.errMsg
	root.mu.Unlock()

	switch status {
	case "completed":
		if result != nil {
			vm.push(*result)
		} else {
			vm.push(ValUnit())
		}
	case "cancelled":
		if !vm.doRaise("awaited task was cancelled") {
			return vm.trap("E011", "awaited task was cancelled")
		}
	case "failed":
		if !vm.doRaise(errMsg) {
			return vm.trap("E014", fmt.Sprintf("child task failed: %s", errMsg))
		}
	}
	return nil
}



func (vm *VM) opTaskGroupEnter() {
	root := vm.root()
	root.mu.Lock()
	groupID := root.nextGroupID
	root.nextGroupID++
	group := &taskGroup{
		id: groupID, parentTaskID: vm.currentTaskID,
		childTaskIDs: make(map[int]bool), dataStackDepth: len(vm.dataStack),
	}
	root.taskGroups[groupID] = group
	root.mu.Unlock()
	vm.activeGroups = append(vm.activeGroups, groupID)
}

func (vm *VM) opTaskGroupExit(code []byte) error {
	if len(vm.activeGroups) == 0 {
		return vm.trap("E000", "TASK_GROUP_EXIT: no active task group")
	}
	groupID := vm.activeGroups[len(vm.activeGroups)-1]
	vm.activeGroups = vm.activeGroups[:len(vm.activeGroups)-1]

	root := vm.root()
	root.mu.Lock()
	group, ok := root.taskGroups[groupID]
	root.mu.Unlock()
	if !ok {
		return nil
	}

	// Wait for all children to complete (they're running in goroutines)
	for childID := range group.childTaskIDs {
		root.mu.Lock()
		child, ok := root.tasks[childID]
		root.mu.Unlock()
		if ok && (child.status == "running" || child.status == "suspended") {
			<-child.resultCh
		}
	}

	// Check for failures
	for childID := range group.childTaskIDs {
		root.mu.Lock()
		child, ok := root.tasks[childID]
		root.mu.Unlock()
		if ok && child.status == "failed" {
			root.mu.Lock()
			delete(root.taskGroups, groupID)
			root.mu.Unlock()
			if !vm.doRaise(child.errMsg) {
				return vm.trap("E014", fmt.Sprintf("child task failed: %s", child.errMsg))
			}
			return nil
		}
	}

	root.mu.Lock()
	delete(root.taskGroups, groupID)
	root.mu.Unlock()
	return nil
}

// ── Channel operations (backed by Go channels) ──

func (vm *VM) opChanNew() error {
	capVal, _ := vm.pop()
	cap := 0
	if capVal.Tag == TagINT {
		cap = capVal.IntVal
	}
	// Minimum buffer of 256 to avoid deadlock when send+recv happen on the same goroutine
	if cap < 256 {
		cap = 256
	}
	root := vm.root()
	root.mu.Lock()
	ch := &Channel{ID: root.nextChannelID, GoCh: make(chan Value, cap), Capacity: cap, SenderOpen: true, ReceiverOpen: true}
	root.nextChannelID++
	root.mu.Unlock()
	vm.push(ValTuple([]Value{ValSender(ch), ValReceiver(ch)}))
	return nil
}

func (vm *VM) opChanSend(code []byte) error {
	val, _ := vm.pop()
	senderVal, _ := vm.pop()
	if senderVal.Tag != TagHEAP || senderVal.Heap.Kind != KindSender {
		return vm.trap("E002", "CHAN_SEND: expected Sender")
	}
	ch := senderVal.Heap.Channel
	ch.mu.Lock()
	open := ch.ReceiverOpen && ch.SenderOpen
	ch.mu.Unlock()
	if !open {
		if !vm.doRaise("channel closed") {
			return vm.trap("E012", "CHAN_SEND: channel is closed")
		}
		return nil
	}
	select {
	case ch.GoCh <- val:
		// sent
	case <-vm.ctx.Done():
		return vm.trap("E011", "task cancelled")
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) opChanRecv(code []byte) error {
	recvVal, _ := vm.pop()
	if recvVal.Tag != TagHEAP || recvVal.Heap.Kind != KindReceiver {
		return vm.trap("E002", "CHAN_RECV: expected Receiver")
	}
	ch := recvVal.Heap.Channel
	// Try non-blocking receive first
	select {
	case v := <-ch.GoCh:
		vm.push(v)
		return nil
	default:
	}
	// Channel is empty — check if sender is closed
	ch.mu.Lock()
	senderOpen := ch.SenderOpen
	ch.mu.Unlock()
	if !senderOpen {
		if !vm.doRaise("channel closed") {
			return vm.trap("E012", "CHAN_RECV: channel is closed and empty")
		}
		return nil
	}
	// Block until a value arrives or context is cancelled
	select {
	case v, ok := <-ch.GoCh:
		if !ok {
			if !vm.doRaise("channel closed") {
				return vm.trap("E012", "CHAN_RECV: channel is closed and empty")
			}
			return nil
		}
		vm.push(v)
		return nil
	case <-vm.ctx.Done():
		return vm.trap("E011", "task cancelled")
	}
}

func (vm *VM) opChanTryRecv() error {
	recvVal, _ := vm.pop()
	if recvVal.Tag != TagHEAP || recvVal.Heap.Kind != KindReceiver {
		return vm.trap("E002", "CHAN_TRY_RECV: expected Receiver")
	}
	ch := recvVal.Heap.Channel
	select {
	case v := <-ch.GoCh:
		someTag, _ := vm.findVariantTag("Some")
		vm.push(ValUnion(someTag, []Value{v}))
	default:
		noneTag, _ := vm.findVariantTag("None")
		vm.push(ValUnion(noneTag, nil))
	}
	return nil
}

func (vm *VM) opChanClose() error {
	endVal, _ := vm.pop()
	if endVal.Tag != TagHEAP {
		return vm.trap("E002", "CHAN_CLOSE: expected Sender or Receiver")
	}
	if endVal.Heap.Kind == KindSender {
		ch := endVal.Heap.Channel
		ch.mu.Lock()
		ch.SenderOpen = false
		ch.mu.Unlock()
	} else if endVal.Heap.Kind == KindReceiver {
		ch := endVal.Heap.Channel
		ch.mu.Lock()
		ch.ReceiverOpen = false
		ch.mu.Unlock()
	} else {
		return vm.trap("E002", "CHAN_CLOSE: expected Sender or Receiver")
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) opSelectBuild(code []byte) error {
	armCount := int(vm.readU8(code))
	if armCount == 0 {
		return vm.trap("E015", "SELECT_BUILD: zero arms")
	}
	pairs := make([]SelectArm, armCount)
	for i := armCount - 1; i >= 0; i-- {
		handler, _ := vm.pop()
		source, _ := vm.pop()
		pairs[i] = SelectArm{Source: source, Handler: handler}
	}
	vm.push(ValSelectSet(pairs))
	return nil
}

func (vm *VM) opSelectWait(code []byte) error {
	setVal, _ := vm.pop()
	if setVal.Tag != TagHEAP || setVal.Heap.Kind != KindSelectSet {
		return vm.trap("E002", "SELECT_WAIT: expected SelectSet")
	}
	arms := setVal.Heap.SelectArms
	root := vm.root()
	for _, arm := range arms {
		src := arm.Source
		if src.Tag == TagHEAP && src.Heap.Kind == KindReceiver {
			ch := src.Heap.Channel
			// Try non-blocking receive from Go channel
			select {
			case v := <-ch.GoCh:
				result, err := vm.callBuiltinFn(arm.Handler, []Value{v})
				if err != nil {
					return err
				}
				vm.push(result)
				return nil
			default:
			}
		} else if src.Tag == TagHEAP && src.Heap.Kind == KindFuture {
			root.mu.Lock()
			task, ok := root.tasks[src.Heap.TaskID]
			root.mu.Unlock()
			if ok {
				// Check if already completed
				root.mu.Lock()
				status := task.status
				root.mu.Unlock()
				if status == "running" || status == "suspended" {
					<-task.resultCh
				}
				root.mu.Lock()
				status = task.status
				taskResult := task.result
				root.mu.Unlock()
				if status == "completed" {
					v := ValUnit()
					if taskResult != nil {
						v = *taskResult
					}
					result, err := vm.callBuiltinFn(arm.Handler, []Value{v})
					if err != nil {
						return err
					}
					vm.push(result)
					return nil
				}
			}
		} else if src.Tag == TagINT {
			result, err := vm.callBuiltinFn(arm.Handler, []Value{ValUnit()})
			if err != nil {
				return err
			}
			vm.push(result)
			return nil
		}
	}
	return vm.trap("E015", "SELECT_WAIT: no arms ready and no timeout")
}

// ── Public API ──

// Execute creates a VM, runs the module, and returns the result + captured stdout.
func Execute(mod *compiler.BytecodeModule) (*Value, []string, error) {
	v := New(mod)
	result, err := v.Run()
	return result, v.Stdout, err
}
