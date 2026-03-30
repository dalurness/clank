// Package eval implements the Clank tree-walking evaluator.
package eval

import (
	"fmt"
	"strings"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

// ── Runtime values ──

// Value is the tagged union for all runtime values.
type Value interface {
	valueTag() string
}

type ValInt struct{ Val int64 }
type ValRat struct{ Val float64 }
type ValBool struct{ Val bool }
type ValStr struct{ Val string }
type ValUnit struct{}
type ValList struct{ Elements []Value }
type ValTuple struct{ Elements []Value }
type ValRecord struct{ Fields *OrderedMap }
type ValVariant struct {
	Name   string
	Fields []Value
}
type ValClosure struct {
	Params []ast.Param
	Body   ast.Expr
	Env    *Env
}
type ValBuiltin struct {
	Name string
	Fn   func(args []Value, loc token.Loc) Value
}
type ValEffectDef struct {
	Name string
	Ops  []string
}

// Async value types
type ValFuture struct{ Task *AsyncTask }
type ValSender struct{ Channel *EvalChannel }
type ValReceiver struct{ Channel *EvalChannel }

func (ValInt) valueTag() string       { return "int" }
func (ValRat) valueTag() string       { return "rat" }
func (ValBool) valueTag() string      { return "bool" }
func (ValStr) valueTag() string       { return "str" }
func (ValUnit) valueTag() string      { return "unit" }
func (ValList) valueTag() string      { return "list" }
func (ValTuple) valueTag() string     { return "tuple" }
func (ValRecord) valueTag() string    { return "record" }
func (ValVariant) valueTag() string   { return "variant" }
func (ValClosure) valueTag() string   { return "closure" }
func (ValBuiltin) valueTag() string   { return "builtin" }
func (ValEffectDef) valueTag() string { return "effect-def" }
func (ValFuture) valueTag() string    { return "future" }
func (ValSender) valueTag() string    { return "sender" }
func (ValReceiver) valueTag() string  { return "receiver" }

// ── OrderedMap for record fields ──

// OrderedMap preserves insertion order of record fields.
type OrderedMap struct {
	keys   []string
	values map[string]Value
}

func NewOrderedMap() *OrderedMap {
	return &OrderedMap{values: make(map[string]Value)}
}

func (m *OrderedMap) Set(key string, val Value) {
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = val
}

func (m *OrderedMap) Get(key string) (Value, bool) {
	v, ok := m.values[key]
	return v, ok
}

func (m *OrderedMap) Has(key string) bool {
	_, ok := m.values[key]
	return ok
}

func (m *OrderedMap) Len() int {
	return len(m.keys)
}

func (m *OrderedMap) Keys() []string {
	return m.keys
}

func (m *OrderedMap) Clone() *OrderedMap {
	nm := NewOrderedMap()
	for _, k := range m.keys {
		nm.Set(k, m.values[k])
	}
	return nm
}

// ── Runtime errors ──

// RuntimeError represents a runtime error with a code and location.
type RuntimeError struct {
	Code     string
	Message  string
	Location token.Loc
}

func (e *RuntimeError) Error() string {
	return fmt.Sprintf("[%s] %s at %d:%d", e.Code, e.Message, e.Location.Line, e.Location.Col)
}

func runtimeError(code, msg string, loc token.Loc) *RuntimeError {
	return &RuntimeError{Code: code, Message: msg, Location: loc}
}

// ── Effect system: perform signal ──

// PerformSignal is thrown when an effect operation is performed.
type PerformSignal struct {
	Op        string
	Args      []Value
	PerformID int
}

func (p *PerformSignal) Error() string {
	return fmt.Sprintf("unhandled effect operation: %s", p.Op)
}

// ── Environment ──

// Env is a scoped environment for variable bindings.
type Env struct {
	bindings map[string]Value
	parent   *Env
}

func NewEnv(parent *Env) *Env {
	return &Env{bindings: make(map[string]Value), parent: parent}
}

func (e *Env) Get(name string, loc token.Loc) Value {
	if val, ok := e.bindings[name]; ok {
		return val
	}
	if e.parent != nil {
		return e.parent.Get(name, loc)
	}
	panic(runtimeError("E202", fmt.Sprintf("unbound variable '%s'", name), loc))
}

func (e *Env) Set(name string, val Value) {
	e.bindings[name] = val
}

func (e *Env) Extend() *Env {
	return NewEnv(e)
}

// ── Value display ──

// ShowValue converts a Value to its string representation.
func ShowValue(v Value) string {
	switch val := v.(type) {
	case ValInt:
		return fmt.Sprintf("%d", val.Val)
	case ValRat:
		s := fmt.Sprintf("%g", val.Val)
		return s
	case ValBool:
		if val.Val {
			return "true"
		}
		return "false"
	case ValStr:
		return val.Val
	case ValUnit:
		return "()"
	case ValList:
		parts := make([]string, len(val.Elements))
		for i, el := range val.Elements {
			parts[i] = ShowValue(el)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case ValTuple:
		parts := make([]string, len(val.Elements))
		for i, el := range val.Elements {
			parts[i] = ShowValue(el)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case ValRecord:
		parts := make([]string, 0, val.Fields.Len())
		for _, k := range val.Fields.Keys() {
			v, _ := val.Fields.Get(k)
			parts = append(parts, fmt.Sprintf("%s: %s", k, ShowValue(v)))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case ValVariant:
		if len(val.Fields) == 0 {
			return val.Name
		}
		parts := make([]string, len(val.Fields))
		for i, f := range val.Fields {
			parts[i] = ShowValue(f)
		}
		return fmt.Sprintf("%s(%s)", val.Name, strings.Join(parts, ", "))
	case ValClosure:
		return "<fn>"
	case ValBuiltin:
		return fmt.Sprintf("<builtin:%s>", val.Name)
	case ValEffectDef:
		return fmt.Sprintf("<effect:%s>", val.Name)
	case ValFuture:
		return "<future>"
	case ValSender:
		return "<sender>"
	case ValReceiver:
		return "<receiver>"
	default:
		return "?"
	}
}

// ShowValueBrief is like ShowValue but abbreviates large values.
func ShowValueBrief(v Value) string {
	switch val := v.(type) {
	case ValList:
		return fmt.Sprintf("[...%d elements]", len(val.Elements))
	case ValVariant:
		if len(val.Fields) > 0 {
			return val.Name + "(...)"
		}
		return val.Name
	default:
		return ShowValue(v)
	}
}

// ── Value equality ──

// ValEqual returns true if two values are structurally equal.
func ValEqual(a, b Value) bool {
	switch av := a.(type) {
	case ValInt:
		if bv, ok := b.(ValInt); ok {
			return av.Val == bv.Val
		}
	case ValRat:
		if bv, ok := b.(ValRat); ok {
			return av.Val == bv.Val
		}
	case ValBool:
		if bv, ok := b.(ValBool); ok {
			return av.Val == bv.Val
		}
	case ValStr:
		if bv, ok := b.(ValStr); ok {
			return av.Val == bv.Val
		}
	case ValUnit:
		if _, ok := b.(ValUnit); ok {
			return true
		}
	case ValList:
		if bv, ok := b.(ValList); ok {
			if len(av.Elements) != len(bv.Elements) {
				return false
			}
			for i := range av.Elements {
				if !ValEqual(av.Elements[i], bv.Elements[i]) {
					return false
				}
			}
			return true
		}
	case ValTuple:
		if bv, ok := b.(ValTuple); ok {
			if len(av.Elements) != len(bv.Elements) {
				return false
			}
			for i := range av.Elements {
				if !ValEqual(av.Elements[i], bv.Elements[i]) {
					return false
				}
			}
			return true
		}
	case ValRecord:
		if bv, ok := b.(ValRecord); ok {
			if av.Fields.Len() != bv.Fields.Len() {
				return false
			}
			for _, k := range av.Fields.Keys() {
				va, _ := av.Fields.Get(k)
				vb, ok := bv.Fields.Get(k)
				if !ok || !ValEqual(va, vb) {
					return false
				}
			}
			return true
		}
	case ValVariant:
		if bv, ok := b.(ValVariant); ok {
			if av.Name != bv.Name || len(av.Fields) != len(bv.Fields) {
				return false
			}
			for i := range av.Fields {
				if !ValEqual(av.Fields[i], bv.Fields[i]) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// ── Helper: runtime type tag ──

func RuntimeTypeTag(v Value) string {
	switch v := v.(type) {
	case ValInt:
		return "Int"
	case ValRat:
		return "Rat"
	case ValBool:
		return "Bool"
	case ValStr:
		return "Str"
	case ValUnit:
		return "Unit"
	case ValList:
		return "List"
	case ValTuple:
		return "Tuple"
	case ValRecord:
		return "Record"
	case ValVariant:
		return v.Name
	case ValClosure:
		return "Fn"
	case ValBuiltin:
		return "Fn"
	case ValEffectDef:
		return "Effect"
	case ValFuture:
		return "Future"
	case ValSender:
		return "Sender"
	case ValReceiver:
		return "Receiver"
	default:
		return "?"
	}
}
