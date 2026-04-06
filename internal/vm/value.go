// Package vm implements a stack-based bytecode interpreter for Clank.
package vm

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Tag discriminates value types on the data stack.
const (
	TagINT   = 0
	TagRAT   = 1
	TagBOOL  = 2
	TagSTR   = 3
	TagBYTE  = 4
	TagUNIT  = 5
	TagHEAP  = 6
	TagQUOTE = 7
)

// HeapKind discriminates heap object types.
const (
	KindList         = "list"
	KindTuple        = "tuple"
	KindRecord       = "record"
	KindUnion        = "union"
	KindClosure      = "closure"
	KindContinuation = "continuation"
	KindFuture       = "future"
	KindTVar         = "tvar"
	KindIterator     = "iterator"
	KindRef          = "ref"
	KindSender       = "sender"
	KindReceiver     = "receiver"
	KindSelectSet    = "select-set"
)

// Value is a tagged VM value.
type Value struct {
	Tag     int
	IntVal  int
	RatVal  float64
	BoolVal bool
	StrVal  string
	ByteVal byte
	WordID  int         // for TagQUOTE
	Heap    *HeapObject // for TagHEAP
}

// HeapObject is a compound value stored on the heap.
type HeapObject struct {
	Kind       string
	Items      []Value            // list, tuple
	Fields     map[string]Value   // record
	FieldOrder []string           // record field insertion order
	VariantTag int                // union
	UFields    []Value            // union fields
	WordID     int                // closure
	Captures   []Value            // closure
	Cont       *ContinuationData  // continuation
	TaskID     int                // future
	Channel    *Channel           // sender, receiver
	Iter       *IteratorState     // iterator
	Ref        *RefCell           // ref
	TVar       *TVar              // tvar
	SelectArms []SelectArm        // select-set
}

// ContinuationData captures execution state for effect handler resume.
type ContinuationData struct {
	DataStack        []Value
	CallStack        []CallFrame
	HandlerStack     []HandlerFrame
	IP               int
	WordID           int
	Locals           []Value
	BaseDataDepth    int
	BaseCallDepth    int
	BaseHandlerDepth int
}

// Channel for inter-task communication, backed by a Go channel.
type Channel struct {
	ID           int
	GoCh         chan Value
	Capacity     int
	mu           sync.Mutex // protects SenderOpen, ReceiverOpen
	SenderOpen   bool
	ReceiverOpen bool
}

// SelectArm is one arm of a select expression.
type SelectArm struct {
	Source  Value
	Handler Value
}

// RefCell is a mutable reference cell.
type RefCell struct {
	ID          int
	Val         Value
	Closed      bool
	HandleCount int
	Empty       bool
}

// TVar is an STM transactional variable.
type TVar struct {
	mu          sync.Mutex
	ID          int
	Version     int
	Val         Value
	Occupied    bool
	HandleCount int
	Closed      bool
}

// IteratorState tracks streaming iterator state.
type IteratorState struct {
	ID           int
	GeneratorFn  Value
	CleanupFn    Value
	Done         bool
	Closed       bool
	Buffer       []Value
	Index        int
	Source       []Value // nil if not list-backed
	NativeNext   func() *Value
	NativeCleanup func()
}

// HandlerFrame represents an installed effect handler.
type HandlerFrame struct {
	EffectID          int
	HandlerOffset     int
	WordID            int
	DataStackDepth    int
	CallStackDepth    int
	HandlerStackDepth int
	Locals            []Value
}

// CallFrame represents a function call frame.
type CallFrame struct {
	ReturnIP     int
	ReturnWordID int
	Locals       []Value
	StackBase    int
}

// ── Value constructors ──

func ValInt(n int) Value    { return Value{Tag: TagINT, IntVal: n} }
func ValRat(n float64) Value { return Value{Tag: TagRAT, RatVal: n} }
func ValBool(b bool) Value  { return Value{Tag: TagBOOL, BoolVal: b} }
func ValStr(s string) Value { return Value{Tag: TagSTR, StrVal: s} }
func ValByte(b byte) Value  { return Value{Tag: TagBYTE, ByteVal: b} }
func ValUnit() Value        { return Value{Tag: TagUNIT} }
func ValQuote(wordID int) Value { return Value{Tag: TagQUOTE, WordID: wordID} }

func ValList(items []Value) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindList, Items: items}}
}

func ValTuple(items []Value) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindTuple, Items: items}}
}

func ValRecord(fields map[string]Value, order []string) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindRecord, Fields: fields, FieldOrder: order}}
}

func ValUnion(tag int, fields []Value) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindUnion, VariantTag: tag, UFields: fields}}
}

func ValClosure(wordID int, captures []Value) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindClosure, WordID: wordID, Captures: captures}}
}

func ValContinuation(cont *ContinuationData) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindContinuation, Cont: cont}}
}

func ValFuture(taskID int) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindFuture, TaskID: taskID}}
}

func ValTVarVal(tv *TVar) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindTVar, TVar: tv}}
}

func ValIter(iter *IteratorState) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindIterator, Iter: iter}}
}

func ValRef(ref *RefCell) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindRef, Ref: ref}}
}

func ValSender(ch *Channel) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindSender, Channel: ch}}
}

func ValReceiver(ch *Channel) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindReceiver, Channel: ch}}
}

func ValSelectSet(arms []SelectArm) Value {
	return Value{Tag: TagHEAP, Heap: &HeapObject{Kind: KindSelectSet, SelectArms: arms}}
}

// ── Display ──

// activeVariantNames is set by the VM before formatting, so ValueToString
// can look up variant names. This avoids threading variantNames through every call.
var activeVariantNames []string

func (v Value) String() string {
	return ValueToString(v)
}

func ValueToString(v Value) string {
	switch v.Tag {
	case TagINT:
		return fmt.Sprintf("%d", v.IntVal)
	case TagRAT:
		return fmt.Sprintf("%g", v.RatVal)
	case TagBOOL:
		if v.BoolVal {
			return "true"
		}
		return "false"
	case TagSTR:
		return v.StrVal
	case TagBYTE:
		return fmt.Sprintf("0x%02x", v.ByteVal)
	case TagUNIT:
		return "()"
	case TagQUOTE:
		return fmt.Sprintf("<quote:%d>", v.WordID)
	case TagHEAP:
		return heapToString(v.Heap)
	}
	return "?"
}

func heapToString(o *HeapObject) string {
	switch o.Kind {
	case KindList:
		parts := make([]string, len(o.Items))
		for i, v := range o.Items {
			parts[i] = ValueToString(v)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case KindTuple:
		parts := make([]string, len(o.Items))
		for i, v := range o.Items {
			parts[i] = ValueToString(v)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case KindRecord:
		order := o.FieldOrder
		if len(order) == 0 {
			// Fallback: collect keys from map (unordered)
			for k := range o.Fields {
				order = append(order, k)
			}
			sort.Strings(order)
		}
		parts := make([]string, len(order))
		for i, k := range order {
			parts[i] = k + ": " + ValueToString(o.Fields[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case KindUnion:
		name := fmt.Sprintf("variant:%d", o.VariantTag)
		if o.VariantTag < len(activeVariantNames) && activeVariantNames[o.VariantTag] != "" {
			name = activeVariantNames[o.VariantTag]
		}
		if len(o.UFields) == 0 {
			return name
		}
		parts := make([]string, len(o.UFields))
		for i, f := range o.UFields {
			parts[i] = ValueToString(f)
		}
		return fmt.Sprintf("%s(%s)", name, strings.Join(parts, ", "))
	case KindClosure:
		return fmt.Sprintf("<closure:%d>", o.WordID)
	case KindContinuation:
		return "<continuation>"
	case KindFuture:
		return fmt.Sprintf("<future:%d>", o.TaskID)
	}
	return "<heap>"
}

// ValuesEqual checks structural equality of two values.
func ValuesEqual(a, b Value) bool {
	if a.Tag != b.Tag {
		return false
	}
	switch a.Tag {
	case TagINT:
		return a.IntVal == b.IntVal
	case TagRAT:
		return a.RatVal == b.RatVal
	case TagBOOL:
		return a.BoolVal == b.BoolVal
	case TagSTR:
		return a.StrVal == b.StrVal
	case TagBYTE:
		return a.ByteVal == b.ByteVal
	case TagUNIT:
		return true
	case TagQUOTE:
		return a.WordID == b.WordID
	case TagHEAP:
		return heapEqual(a.Heap, b.Heap)
	}
	return false
}

func heapEqual(a, b *HeapObject) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindList, KindTuple:
		if len(a.Items) != len(b.Items) {
			return false
		}
		for i := range a.Items {
			if !ValuesEqual(a.Items[i], b.Items[i]) {
				return false
			}
		}
		return true
	case KindUnion:
		if a.VariantTag != b.VariantTag || len(a.UFields) != len(b.UFields) {
			return false
		}
		for i := range a.UFields {
			if !ValuesEqual(a.UFields[i], b.UFields[i]) {
				return false
			}
		}
		return true
	case KindRecord:
		if len(a.Fields) != len(b.Fields) {
			return false
		}
		for k, av := range a.Fields {
			bv, ok := b.Fields[k]
			if !ok || !ValuesEqual(av, bv) {
				return false
			}
		}
		return true
	}
	return false
}

// NumericValue extracts a numeric value (int or rat) as float64.
func NumericValue(v Value) (float64, bool) {
	switch v.Tag {
	case TagINT:
		return float64(v.IntVal), true
	case TagRAT:
		return v.RatVal, true
	}
	return 0, false
}
