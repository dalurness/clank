// Package ast defines the abstract syntax tree for the Clank language.
//
// Tagged unions are represented as Go interfaces with a private marker method,
// and concrete types implement the interface. Use type switches to dispatch.
package ast

import "github.com/dalurness/clank/internal/token"

// ── Literals ──

// Literal is a tagged union for literal values.
type Literal interface {
	literalNode()
}

type LitInt struct{ Value int64 }
type LitRat struct{ Value float64 }
type LitBool struct{ Value bool }
type LitStr struct{ Value string }
type LitUnit struct{}

func (LitInt) literalNode()  {}
func (LitRat) literalNode()  {}
func (LitBool) literalNode() {}
func (LitStr) literalNode()  {}
func (LitUnit) literalNode() {}

// ── Patterns ──

// PatField is a named field in a record pattern.
type PatField struct {
	Name    string
	Pattern Pattern // nil if punned
}

// Pattern is a tagged union for pattern matching forms.
type Pattern interface {
	patternNode()
	PatLoc() token.Loc
}

type PatVar struct {
	Name string
	Loc  token.Loc
}

type PatLiteral struct {
	Value Literal
	Loc   token.Loc
}

type PatVariant struct {
	Name string
	Args []Pattern
	Loc  token.Loc
}

type PatTuple struct {
	Elements []Pattern
	Loc      token.Loc
}

type PatRecord struct {
	Fields []PatField
	Rest   string // empty if no rest capture
	Loc    token.Loc
}

type PatWildcard struct {
	Loc token.Loc
}

func (p PatVar) patternNode()      {}
func (p PatLiteral) patternNode()  {}
func (p PatVariant) patternNode()  {}
func (p PatTuple) patternNode()    {}
func (p PatRecord) patternNode()   {}
func (p PatWildcard) patternNode() {}

func (p PatVar) PatLoc() token.Loc      { return p.Loc }
func (p PatLiteral) PatLoc() token.Loc  { return p.Loc }
func (p PatVariant) PatLoc() token.Loc  { return p.Loc }
func (p PatTuple) PatLoc() token.Loc    { return p.Loc }
func (p PatRecord) PatLoc() token.Loc   { return p.Loc }
func (p PatWildcard) PatLoc() token.Loc { return p.Loc }

// ── Effect references ──

// EffectRef is an effect name with optional type arguments.
type EffectRef struct {
	Name string
	Args []string
}

// ── Type expressions ──

// TypeExpr is a tagged union for type annotations.
type TypeExpr interface {
	typeExprNode()
	TypeLoc() token.Loc
}

// RecordTypeField is a field in a record type.
type RecordTypeField struct {
	Name string
	Tags []string
	Type TypeExpr
}

type TypeName struct {
	Name string
	Loc  token.Loc
}

type TypeList struct {
	Element TypeExpr
	Loc     token.Loc
}

type TypeTuple struct {
	Elements []TypeExpr
	Loc      token.Loc
}

type TypeRecord struct {
	Fields []RecordTypeField
	RowVar string // empty if closed
	Loc    token.Loc
}

type TypeUnion struct {
	Left  TypeExpr
	Right TypeExpr
	Loc   token.Loc
}

type TypeFn struct {
	Param      TypeExpr
	Effects    []EffectRef
	Subtracted []EffectRef
	Result     TypeExpr
	Loc        token.Loc
}

type TypeGeneric struct {
	Name string
	Args []TypeExpr
	Loc  token.Loc
}

type TypeRefined struct {
	Base      TypeExpr
	Predicate string
	Loc       token.Loc
}

type TypeBorrow struct {
	Inner TypeExpr
	Loc   token.Loc
}

type TypeTagProject struct {
	Base    TypeExpr
	TagName string
	Loc     token.Loc
}

type TypeTypeFilter struct {
	Base       TypeExpr
	FilterType TypeExpr
	Loc        token.Loc
}

type TypePick struct {
	Base       TypeExpr
	FieldNames []string
	Loc        token.Loc
}

type TypeOmit struct {
	Base       TypeExpr
	FieldNames []string
	Loc        token.Loc
}

func (TypeName) typeExprNode()       {}
func (TypeList) typeExprNode()       {}
func (TypeTuple) typeExprNode()      {}
func (TypeRecord) typeExprNode()     {}
func (TypeUnion) typeExprNode()      {}
func (TypeFn) typeExprNode()         {}
func (TypeGeneric) typeExprNode()    {}
func (TypeRefined) typeExprNode()    {}
func (TypeBorrow) typeExprNode()     {}
func (TypeTagProject) typeExprNode() {}
func (TypeTypeFilter) typeExprNode() {}
func (TypePick) typeExprNode()       {}
func (TypeOmit) typeExprNode()       {}

func (t TypeName) TypeLoc() token.Loc       { return t.Loc }
func (t TypeList) TypeLoc() token.Loc       { return t.Loc }
func (t TypeTuple) TypeLoc() token.Loc      { return t.Loc }
func (t TypeRecord) TypeLoc() token.Loc     { return t.Loc }
func (t TypeUnion) TypeLoc() token.Loc      { return t.Loc }
func (t TypeFn) TypeLoc() token.Loc         { return t.Loc }
func (t TypeGeneric) TypeLoc() token.Loc    { return t.Loc }
func (t TypeRefined) TypeLoc() token.Loc    { return t.Loc }
func (t TypeBorrow) TypeLoc() token.Loc     { return t.Loc }
func (t TypeTagProject) TypeLoc() token.Loc { return t.Loc }
func (t TypeTypeFilter) TypeLoc() token.Loc { return t.Loc }
func (t TypePick) TypeLoc() token.Loc       { return t.Loc }
func (t TypeOmit) TypeLoc() token.Loc       { return t.Loc }

// ── Expressions ──

// Expr is a tagged union for all expression forms.
type Expr interface {
	exprNode()
	ExprLoc() token.Loc
}

// MatchArm is a single arm in a match expression.
type MatchArm struct {
	Pattern Pattern
	Body    Expr
}

// HandlerArm is a single handler arm.
type HandlerArm struct {
	Name       string
	Params     []Param
	ResumeName string // empty if not captured
	Body       Expr
}

// DoStep is a single step in a do block.
type DoStep struct {
	Bind string // empty if no binding
	Expr Expr
}

// Param is a function parameter with optional type annotation.
type Param struct {
	Name string
	Type TypeExpr // nil if not annotated
}

// RecordField is a field in a record expression.
type RecordField struct {
	Name  string
	Tags  []string
	Value Expr
}

// RecordUpdateField is a field in a record update expression.
type RecordUpdateField struct {
	Name  string
	Value Expr
}

// FoldClause is the fold clause in a for expression.
type FoldClause struct {
	Acc  string
	Init Expr
}

type ExprLiteral struct {
	Value Literal
	Loc   token.Loc
}

type ExprVar struct {
	Name string
	Loc  token.Loc
}

type ExprLet struct {
	Name  string
	Value Expr
	Body  Expr // nil if no body
	Loc   token.Loc
}

type ExprLetPattern struct {
	Pattern Pattern
	Value   Expr
	Body    Expr // nil if no body
	Loc     token.Loc
}

type ExprIf struct {
	Cond Expr
	Then Expr
	Else Expr
	Loc  token.Loc
}

type ExprMatch struct {
	Subject Expr
	Arms    []MatchArm
	Loc     token.Loc
}

type ExprLambda struct {
	Params []Param
	Body   Expr
	Loc    token.Loc
}

type ExprApply struct {
	Fn   Expr
	Args []Expr
	Loc  token.Loc
}

type ExprPipeline struct {
	Left  Expr
	Right Expr
	Loc   token.Loc
}

type ExprInfix struct {
	Op    string
	Left  Expr
	Right Expr
	Loc   token.Loc
}

type ExprUnary struct {
	Op      string
	Operand Expr
	Loc     token.Loc
}

type ExprDo struct {
	Steps []DoStep
	Loc   token.Loc
}

type ExprHandle struct {
	Expr Expr
	Arms []HandlerArm
	Loc  token.Loc
}

type ExprPerform struct {
	Expr Expr
	Loc  token.Loc
}

type ExprList struct {
	Elements []Expr
	Loc      token.Loc
}

type ExprTuple struct {
	Elements []Expr
	Loc      token.Loc
}

type ExprRecord struct {
	Fields []RecordField
	Spread Expr // nil if no spread
	Loc    token.Loc
}

type ExprRecordUpdate struct {
	Base   Expr
	Fields []RecordUpdateField
	Loc    token.Loc
}

type ExprFieldAccess struct {
	Object Expr
	Field  string
	Loc    token.Loc
}

type ExprFor struct {
	Bind       Pattern
	Collection Expr
	Guard      Expr // nil if no guard
	Fold       *FoldClause
	Body       Expr
	Loc        token.Loc
}

type ExprRange struct {
	Start     Expr
	End       Expr
	Inclusive bool
	Loc       token.Loc
}

type ExprBorrow struct {
	Expr Expr
	Loc  token.Loc
}

type ExprClone struct {
	Expr Expr
	Loc  token.Loc
}

type ExprDiscard struct {
	Expr Expr
	Loc  token.Loc
}

func (ExprLiteral) exprNode()      {}
func (ExprVar) exprNode()          {}
func (ExprLet) exprNode()          {}
func (ExprLetPattern) exprNode()   {}
func (ExprIf) exprNode()           {}
func (ExprMatch) exprNode()        {}
func (ExprLambda) exprNode()       {}
func (ExprApply) exprNode()        {}
func (ExprPipeline) exprNode()     {}
func (ExprInfix) exprNode()        {}
func (ExprUnary) exprNode()        {}
func (ExprDo) exprNode()           {}
func (ExprHandle) exprNode()       {}
func (ExprPerform) exprNode()      {}
func (ExprList) exprNode()         {}
func (ExprTuple) exprNode()        {}
func (ExprRecord) exprNode()       {}
func (ExprRecordUpdate) exprNode() {}
func (ExprFieldAccess) exprNode()  {}
func (ExprFor) exprNode()          {}
func (ExprRange) exprNode()        {}
func (ExprBorrow) exprNode()       {}
func (ExprClone) exprNode()        {}
func (ExprDiscard) exprNode()      {}

func (e ExprLiteral) ExprLoc() token.Loc      { return e.Loc }
func (e ExprVar) ExprLoc() token.Loc          { return e.Loc }
func (e ExprLet) ExprLoc() token.Loc          { return e.Loc }
func (e ExprLetPattern) ExprLoc() token.Loc   { return e.Loc }
func (e ExprIf) ExprLoc() token.Loc           { return e.Loc }
func (e ExprMatch) ExprLoc() token.Loc        { return e.Loc }
func (e ExprLambda) ExprLoc() token.Loc       { return e.Loc }
func (e ExprApply) ExprLoc() token.Loc        { return e.Loc }
func (e ExprPipeline) ExprLoc() token.Loc     { return e.Loc }
func (e ExprInfix) ExprLoc() token.Loc        { return e.Loc }
func (e ExprUnary) ExprLoc() token.Loc        { return e.Loc }
func (e ExprDo) ExprLoc() token.Loc           { return e.Loc }
func (e ExprHandle) ExprLoc() token.Loc       { return e.Loc }
func (e ExprPerform) ExprLoc() token.Loc      { return e.Loc }
func (e ExprList) ExprLoc() token.Loc         { return e.Loc }
func (e ExprTuple) ExprLoc() token.Loc        { return e.Loc }
func (e ExprRecord) ExprLoc() token.Loc       { return e.Loc }
func (e ExprRecordUpdate) ExprLoc() token.Loc { return e.Loc }
func (e ExprFieldAccess) ExprLoc() token.Loc  { return e.Loc }
func (e ExprFor) ExprLoc() token.Loc          { return e.Loc }
func (e ExprRange) ExprLoc() token.Loc        { return e.Loc }
func (e ExprBorrow) ExprLoc() token.Loc       { return e.Loc }
func (e ExprClone) ExprLoc() token.Loc        { return e.Loc }
func (e ExprDiscard) ExprLoc() token.Loc      { return e.Loc }

// ── Type signatures ──

// TypeSig is a function type signature with effects.
type TypeSig struct {
	Params     []TypeSigParam
	Effects    []EffectRef
	Subtracted []EffectRef
	ReturnType TypeExpr
}

// TypeSigParam is a named parameter in a type signature.
type TypeSigParam struct {
	Name string
	Type TypeExpr
}

// ── Constraints ──

// Constraint is an interface constraint (where clause).
type Constraint struct {
	Interface string
	TypeArgs  []TypeExpr
	TypeParam string
	Loc       token.Loc
}

// MethodSig is a method signature in an interface declaration.
type MethodSig struct {
	Name string
	Sig  TypeSig
}

// ── Imports ──

// ImportItem is a single imported name with optional alias.
type ImportItem struct {
	Name  string
	Alias string // empty if no alias
}

// ── Top-level declarations ──

// TopLevel is a tagged union for all top-level forms.
type TopLevel interface {
	topLevelNode()
	TopLoc() token.Loc
}

// Variant is a variant in a type declaration.
type Variant struct {
	Name   string
	Fields []TypeExpr
}

// OpSig is an operation signature in an effect declaration.
type OpSig struct {
	Name string
	Sig  TypeSig
}

// ImplMethod is a method implementation in an impl block.
type ImplMethod struct {
	Name string
	Body Expr
}

type TopDefinition struct {
	Name        string
	Sig         TypeSig
	Body        Expr
	Pub         bool
	Constraints []Constraint
	Loc         token.Loc
}

type TopTypeDecl struct {
	Name       string
	TypeParams []string
	Variants   []Variant
	Affine     bool
	Deriving   []string
	Pub        bool
	Loc        token.Loc
}

type TopEffectDecl struct {
	Name string
	Ops  []OpSig
	Pub  bool
	Loc  token.Loc
}

type TopEffectAlias struct {
	Name       string
	Params     []string
	Effects    []EffectRef
	Subtracted []EffectRef
	Pub        bool
	Loc        token.Loc
}

type TopModDecl struct {
	Path []string
	Loc  token.Loc
}

type TopUseDecl struct {
	Path    []string
	Imports []ImportItem
	Loc     token.Loc
}

type TopTestDecl struct {
	Name string
	Body Expr
	Loc  token.Loc
}

type TopInterfaceDecl struct {
	Name       string
	TypeParams []string
	Supers     []string
	Methods    []MethodSig
	Pub        bool
	Loc        token.Loc
}

type TopImplBlock struct {
	Interface   string
	TypeArgs    []TypeExpr
	ForType     TypeExpr
	Constraints []Constraint
	Methods     []ImplMethod
	Pub         bool
	Loc         token.Loc
}

func (TopDefinition) topLevelNode()    {}
func (TopTypeDecl) topLevelNode()      {}
func (TopEffectDecl) topLevelNode()    {}
func (TopEffectAlias) topLevelNode()   {}
func (TopModDecl) topLevelNode()       {}
func (TopUseDecl) topLevelNode()       {}
func (TopTestDecl) topLevelNode()      {}
func (TopInterfaceDecl) topLevelNode() {}
func (TopImplBlock) topLevelNode()     {}

func (t TopDefinition) TopLoc() token.Loc    { return t.Loc }
func (t TopTypeDecl) TopLoc() token.Loc      { return t.Loc }
func (t TopEffectDecl) TopLoc() token.Loc    { return t.Loc }
func (t TopEffectAlias) TopLoc() token.Loc   { return t.Loc }
func (t TopModDecl) TopLoc() token.Loc       { return t.Loc }
func (t TopUseDecl) TopLoc() token.Loc       { return t.Loc }
func (t TopTestDecl) TopLoc() token.Loc      { return t.Loc }
func (t TopInterfaceDecl) TopLoc() token.Loc { return t.Loc }
func (t TopImplBlock) TopLoc() token.Loc     { return t.Loc }

// ── Program ──

// Program is the top-level AST node representing a complete source file.
type Program struct {
	TopLevels []TopLevel
}
