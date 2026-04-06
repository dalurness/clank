// Package checker implements the Clank bidirectional type checker.
// It validates programs after parsing/desugaring, before evaluation.
package checker

import "sync/atomic"

// ── Type representations ──

// Type is a tagged union for semantic types (after resolving syntax).
type Type interface {
	typeNode()
}

type TPrimitive struct{ Name string } // "int", "rat", "bool", "str", "unit"
type TFn struct {
	Param   Type
	Effects []Effect
	Result  Type
}
type TList struct{ Element Type }
type TTuple struct{ Elements []Type }
type TRecord struct {
	Fields []RecordField
	RowVar int // -1 if closed
}
type TVariant struct{ Variants []VariantCase }
type TVar struct{ ID int }
type TGeneric struct {
	Name string
	Args []Type
}
type TBorrow struct{ Inner Type }

func (TPrimitive) typeNode() {}
func (TFn) typeNode()        {}
func (TList) typeNode()      {}
func (TTuple) typeNode()     {}
func (TRecord) typeNode()    {}
func (TVariant) typeNode()   {}
func (TVar) typeNode()       {}
func (TGeneric) typeNode()   {}
func (TBorrow) typeNode()    {}

type RecordField struct {
	Name string
	Tags []string
	Type Type
}

type VariantCase struct {
	Name   string
	Fields []Type
}

// ── Effects ──

type Effect interface {
	effectNode()
}

type ENamed struct{ Name string }
type EVar struct{ ID int }

func (ENamed) effectNode() {}
func (EVar) effectNode()   {}

// ── Type schemes (polymorphic types) ──

type TypeScheme struct {
	TypeVars   []int
	EffectVars []int
	Body       Type
}

// ── Constructors ──

var (
	TInt  Type = TPrimitive{"int"}
	TRat  Type = TPrimitive{"rat"}
	TBool Type = TPrimitive{"bool"}
	TStr  Type = TPrimitive{"str"}
	TUnit Type = TPrimitive{"unit"}
	TAny  Type = TGeneric{Name: "?", Args: nil}
)

func NewTFn(param, result Type, effects ...Effect) Type {
	return TFn{Param: param, Effects: effects, Result: result}
}

func NewTList(elem Type) Type { return TList{Element: elem} }

func NewTTuple(elems []Type) Type { return TTuple{Elements: elems} }

func NewTRecord(fields []RecordField, rowVar ...int) Type {
	rv := -1
	if len(rowVar) > 0 {
		rv = rowVar[0]
	}
	return TRecord{Fields: fields, RowVar: rv}
}

func NewTBorrow(inner Type) Type { return TBorrow{Inner: inner} }

func NewENamed(name string) Effect { return ENamed{Name: name} }

// ── Fresh variable counter ──

var nextVarID int64 = 1000

func FreshVar() int {
	return int(atomic.AddInt64(&nextVarID, 1) - 1)
}

func ResetVarCounter() {
	atomic.StoreInt64(&nextVarID, 1000)
}
