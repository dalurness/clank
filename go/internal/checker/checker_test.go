package checker

import (
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

func TestEffectAliasArityZeroParams(t *testing.T) {
	// A zero-param effect alias used with type arguments should produce E502.
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopEffectDecl{
				Name: "state",
				Ops: []ast.OpSig{
					{Name: "get", Sig: ast.TypeSig{ReturnType: ast.TypeName{Name: "Int"}}},
				},
				Loc: loc,
			},
			ast.TopEffectAlias{
				Name:    "IO",
				Params:  nil,
				Effects: []ast.EffectRef{{Name: "io"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "run",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "IO", Args: []string{"state"}}},
					ReturnType: ast.TypeName{Name: "Int"},
				},
				Body: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc},
				Loc:  loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc},
				Loc:  loc,
			},
		},
	}

	errors := TypeCheck(program)
	found := false
	for _, e := range errors {
		if e.Code == "E502" && strings.Contains(e.Message, "expects 0 type argument(s), got 1") {
			found = true
			break
		}
	}
	if !found {
		msgs := make([]string, len(errors))
		for i, e := range errors {
			msgs[i] = e.Error()
		}
		t.Errorf("expected E502 arity error for zero-param alias with args, got errors: %v", msgs)
	}
}

func TestEffectAliasArityTooManyArgs(t *testing.T) {
	// A one-param alias used with 2 args should produce E502.
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopEffectDecl{
				Name: "state",
				Ops: []ast.OpSig{
					{Name: "get", Sig: ast.TypeSig{ReturnType: ast.TypeName{Name: "Int"}}},
				},
				Loc: loc,
			},
			ast.TopEffectAlias{
				Name:    "Stateful",
				Params:  []string{"S"},
				Effects: []ast.EffectRef{{Name: "S"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "run",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "Stateful", Args: []string{"state", "io"}}},
					ReturnType: ast.TypeName{Name: "Int"},
				},
				Body: ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc},
				Loc:  loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc},
				Loc:  loc,
			},
		},
	}

	errors := TypeCheck(program)
	found := false
	for _, e := range errors {
		if e.Code == "E502" && strings.Contains(e.Message, "expects 1 type argument(s), got 2") {
			found = true
			break
		}
	}
	if !found {
		msgs := make([]string, len(errors))
		for i, e := range errors {
			msgs[i] = e.Error()
		}
		t.Errorf("expected E502 arity error, got errors: %v", msgs)
	}
}

func TestEffectAliasCrossModulePropagation(t *testing.T) {
	// An imported effect alias should be resolved via ModuleEffectAliasResolver.
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopEffectDecl{
				Name: "logger",
				Ops: []ast.OpSig{
					{Name: "log_msg", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopUseDecl{
				Path:    []string{"myaliases"},
				Imports: []ast.ImportItem{{Name: "WithLog"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "greet",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "WithLog"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprPerform{
					Expr: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "log_msg", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "hello"}, Loc: loc}},
						Loc:  loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc},
				Loc:  loc,
			},
		},
	}

	// Provide a resolver that knows myaliases exports WithLog = <logger, io>
	aliasResolver := func(modulePath []string) map[string]*EffectAliasInfo {
		if len(modulePath) == 1 && modulePath[0] == "myaliases" {
			return map[string]*EffectAliasInfo{
				"WithLog": {
					Params:  nil,
					Effects: []ast.EffectRef{{Name: "logger"}, {Name: "io"}},
				},
			}
		}
		return nil
	}

	errors := TypeCheckWithResolvers(program, nil, aliasResolver)

	// greet performs log_msg (effect: logger), and WithLog expands to <logger, io>.
	// So there should be no "performs effect not declared" warning for logger.
	for _, e := range errors {
		if e.Code == "W401" && strings.Contains(e.Message, "logger") {
			t.Errorf("unexpected W401 for 'logger': alias should have propagated from module; got: %s", e.Message)
		}
	}
}

// ── typeEqual tightening tests (TASK-196) ──

func TestTypeEqualDistinctGenerics(t *testing.T) {
	// Two TGeneric types with different names must not be equal.
	a := TGeneric{Name: "Option", Args: []Type{TInt}}
	b := TGeneric{Name: "Result", Args: []Type{TInt}}
	if typeEqual(a, b) {
		t.Error("typeEqual(Option<Int>, Result<Int>) should be false")
	}
}

func TestTypeEqualSameGeneric(t *testing.T) {
	a := TGeneric{Name: "Option", Args: []Type{TInt}}
	b := TGeneric{Name: "Option", Args: []Type{TInt}}
	if !typeEqual(a, b) {
		t.Error("typeEqual(Option<Int>, Option<Int>) should be true")
	}
}

func TestTypeEqualGenericDifferentArgs(t *testing.T) {
	a := TGeneric{Name: "Option", Args: []Type{TInt}}
	b := TGeneric{Name: "Option", Args: []Type{TStr}}
	if typeEqual(a, b) {
		t.Error("typeEqual(Option<Int>, Option<Str>) should be false")
	}
}

func TestTypeEqualGenericVsPrimitive(t *testing.T) {
	// A named generic type must not equal a primitive.
	a := TGeneric{Name: "Ordering"}
	b := TInt
	if typeEqual(a, b) {
		t.Error("typeEqual(Ordering, Int) should be false")
	}
	if typeEqual(b, a) {
		t.Error("typeEqual(Int, Ordering) should be false")
	}
}

func TestTypeEqualDefaultFalse(t *testing.T) {
	// Unmatched type pairs (e.g. TVariant vs TPrimitive) must return false.
	a := TVariant{Variants: []VariantCase{{Name: "A"}}}
	b := TInt
	if typeEqual(a, b) {
		t.Error("typeEqual(Variant, Int) should be false")
	}
}

func TestTypeEqualVariantStructural(t *testing.T) {
	a := TVariant{Variants: []VariantCase{{Name: "Some", Fields: []Type{TInt}}, {Name: "None"}}}
	b := TVariant{Variants: []VariantCase{{Name: "Some", Fields: []Type{TInt}}, {Name: "None"}}}
	if !typeEqual(a, b) {
		t.Error("typeEqual on identical variants should be true")
	}
	c := TVariant{Variants: []VariantCase{{Name: "Ok", Fields: []Type{TInt}}, {Name: "Err"}}}
	if typeEqual(a, c) {
		t.Error("typeEqual on different variants should be false")
	}
}

// ── Composite literal inference tests (TASK-196) ──

func TestUnifyRecordTypes(t *testing.T) {
	// unifyTypes should structurally compare TRecord types.
	s := &checkerState{
		typeSubst: make(map[int]Type),
		rowSubst:  make(map[int]*rowSubstEntry),
	}
	a := TRecord{Fields: []RecordField{{Name: "x", Type: TInt}}, RowVar: -1}
	b := TRecord{Fields: []RecordField{{Name: "x", Type: TInt}}, RowVar: -1}
	if !s.unifyTypes(a, b) {
		t.Error("unifyTypes on identical records should succeed")
	}
}

func TestUnifyRecordMismatch(t *testing.T) {
	s := &checkerState{
		typeSubst: make(map[int]Type),
		rowSubst:  make(map[int]*rowSubstEntry),
	}
	a := TRecord{Fields: []RecordField{{Name: "x", Type: TInt}}, RowVar: -1}
	b := TRecord{Fields: []RecordField{{Name: "x", Type: TStr}}, RowVar: -1}
	if s.unifyTypes(a, b) {
		t.Error("unifyTypes on records with different field types should fail")
	}
}

func TestUnifyTVarWithRecord(t *testing.T) {
	// A TVar should unify with a record and be substituted.
	s := &checkerState{
		typeSubst: make(map[int]Type),
		rowSubst:  make(map[int]*rowSubstEntry),
	}
	tv := TVar{ID: 9999}
	rec := TRecord{Fields: []RecordField{{Name: "name", Type: TStr}}, RowVar: -1}
	if !s.unifyTypes(tv, rec) {
		t.Error("TVar should unify with TRecord")
	}
	if s.typeSubst[9999] == nil {
		t.Error("TVar should be substituted to the record type")
	}
}

func TestUnifyGenericNotWildcard(t *testing.T) {
	// A TGeneric should NOT unify with a non-matching type.
	s := &checkerState{
		typeSubst: make(map[int]Type),
		rowSubst:  make(map[int]*rowSubstEntry),
	}
	g := TGeneric{Name: "Option"}
	if s.unifyTypes(g, TInt) {
		t.Error("TGeneric{Option} should not unify with TInt")
	}
	if s.unifyTypes(TStr, g) {
		t.Error("TStr should not unify with TGeneric{Option}")
	}
}

func TestEffectAliasCrossModuleRename(t *testing.T) {
	// An imported effect alias with a local rename should also propagate.
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopEffectDecl{
				Name: "logger",
				Ops: []ast.OpSig{
					{Name: "log_msg", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopUseDecl{
				Path:    []string{"myaliases"},
				Imports: []ast.ImportItem{{Name: "WithLog", Alias: "MyEffects"}},
				Loc:     loc,
			},
			ast.TopDefinition{
				Name: "greet",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "MyEffects"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprPerform{
					Expr: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "log_msg", Loc: loc},
						Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "hello"}, Loc: loc}},
						Loc:  loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc},
				Loc:  loc,
			},
		},
	}

	aliasResolver := func(modulePath []string) map[string]*EffectAliasInfo {
		if len(modulePath) == 1 && modulePath[0] == "myaliases" {
			return map[string]*EffectAliasInfo{
				"WithLog": {
					Params:  nil,
					Effects: []ast.EffectRef{{Name: "logger"}, {Name: "io"}},
				},
			}
		}
		return nil
	}

	errors := TypeCheckWithResolvers(program, nil, aliasResolver)

	for _, e := range errors {
		if e.Code == "W401" && strings.Contains(e.Message, "logger") {
			t.Errorf("unexpected W401 for 'logger': renamed alias should have propagated; got: %s", e.Message)
		}
	}
}

// ── Zero-param lambda inference ──

func TestZeroParamLambdaInferredAsFnType(t *testing.T) {
	// A zero-param lambda fn() => 42 should be inferred as () -> Int, not Int.
	// If the bug existed, the let-binding would have type Int and thunk() would fail.
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}},
					ReturnType: ast.TypeTuple{},
				},
				Body: ast.ExprLet{
					Name: "thunk",
					Value: ast.ExprLambda{
						Params: nil,
						Body:   ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc},
						Loc:    loc,
					},
					Body: ast.ExprApply{
						Fn: ast.ExprVar{Name: "print", Loc: loc},
						Args: []ast.Expr{
							ast.ExprApply{
								Fn: ast.ExprVar{Name: "show", Loc: loc},
								Args: []ast.Expr{
									ast.ExprApply{
										Fn:   ast.ExprVar{Name: "thunk", Loc: loc},
										Args: nil,
										Loc:  loc,
									},
								},
								Loc: loc,
							},
						},
						Loc: loc,
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}

	errors := TypeCheck(program)
	for _, e := range errors {
		if e.Code == "E303" || e.Code == "E304" || e.Code == "E307" {
			t.Errorf("zero-param lambda should infer as () -> Int; got error: %s", e.Error())
		}
	}
}

// ── Parameterized interface constraint type arg validation ──

func TestParameterizedConstraintRejectsNonParamImpl(t *testing.T) {
	// An impl without type args (e.g. impl Serializable for Int) should NOT
	// satisfy a parameterized constraint (e.g. where Serializable<Bool> T).
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopInterfaceDecl{
				Name:       "Serializable",
				TypeParams: []string{"T"},
				Methods: []ast.MethodSig{
					{Name: "serialize", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "self", Type: ast.TypeName{Name: "Self"}}},
						ReturnType: ast.TypeName{Name: "Str"},
					}},
				},
				Loc: loc,
			},
			ast.TopImplBlock{
				Interface: "Serializable",
				ForType:   ast.TypeName{Name: "Int"},
				TypeArgs:  nil, // no type args — this is the bug trigger
				Methods: []ast.ImplMethod{
					{Name: "serialize",
						Body: ast.ExprLambda{
							Params: []ast.Param{{Name: "n"}},
							Body: ast.ExprApply{
								Fn:   ast.ExprVar{Name: "show", Loc: loc},
								Args: []ast.Expr{ast.ExprVar{Name: "n", Loc: loc}},
								Loc:  loc,
							},
							Loc: loc,
						}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "save",
				Sig: ast.TypeSig{
					Params:     []ast.TypeSigParam{{Name: "x", Type: ast.TypeName{Name: "T"}}},
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Constraints: []ast.Constraint{
					{Interface: "Serializable", TypeParam: "T", TypeArgs: []ast.TypeExpr{ast.TypeName{Name: "Bool"}}},
				},
				Body: ast.ExprApply{
					Fn:   ast.ExprVar{Name: "serialize", Loc: loc},
					Args: []ast.Expr{ast.ExprVar{Name: "x", Loc: loc}},
					Loc:  loc,
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}},
					ReturnType: ast.TypeTuple{},
				},
				Body: ast.ExprApply{
					Fn: ast.ExprVar{Name: "print", Loc: loc},
					Args: []ast.Expr{
						ast.ExprApply{
							Fn:   ast.ExprVar{Name: "save", Loc: loc},
							Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
							Loc:  loc,
						},
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}

	errors := TypeCheck(program)
	found := false
	for _, e := range errors {
		if e.Code == "E205" && strings.Contains(e.Message, "Serializable<Bool>") {
			found = true
			break
		}
	}
	if !found {
		msgs := make([]string, len(errors))
		for i, e := range errors {
			msgs[i] = e.Error()
		}
		t.Errorf("expected E205 for non-parameterized impl vs parameterized constraint, got: %v", msgs)
	}
}

func TestParameterizedConstraintMatchingTypeArgsPass(t *testing.T) {
	// impl From<Int> for Str auto-generates Into for Int with typeArgs=["Str"].
	// A constraint Into<Str> T with T=Int should pass.
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopImplBlock{
				Interface: "From",
				ForType:   ast.TypeName{Name: "Str"},
				TypeArgs:  []ast.TypeExpr{ast.TypeName{Name: "Int"}},
				Methods: []ast.ImplMethod{
					{Name: "from",
						Body: ast.ExprLambda{
							Params: []ast.Param{{Name: "n"}},
							Body: ast.ExprApply{
								Fn:   ast.ExprVar{Name: "show", Loc: loc},
								Args: []ast.Expr{ast.ExprVar{Name: "n", Loc: loc}},
								Loc:  loc,
							},
							Loc: loc,
						}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "convert",
				Sig: ast.TypeSig{
					Params:     []ast.TypeSigParam{{Name: "x", Type: ast.TypeName{Name: "T"}}},
					ReturnType: ast.TypeName{Name: "Str"},
				},
				Constraints: []ast.Constraint{
					{Interface: "Into", TypeParam: "T", TypeArgs: []ast.TypeExpr{ast.TypeName{Name: "Str"}}},
				},
				Body: ast.ExprApply{
					Fn:   ast.ExprVar{Name: "into", Loc: loc},
					Args: []ast.Expr{ast.ExprVar{Name: "x", Loc: loc}},
					Loc:  loc,
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "main",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}},
					ReturnType: ast.TypeTuple{},
				},
				Body: ast.ExprApply{
					Fn: ast.ExprVar{Name: "print", Loc: loc},
					Args: []ast.Expr{
						ast.ExprApply{
							Fn:   ast.ExprVar{Name: "convert", Loc: loc},
							Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitInt{Value: 42}, Loc: loc}},
							Loc:  loc,
						},
					},
					Loc: loc,
				},
				Loc: loc,
			},
		},
	}

	errors := TypeCheck(program)
	for _, e := range errors {
		if e.Code == "E205" {
			t.Errorf("expected no E205 for matching parameterized constraint, got: %s", e.Error())
		}
	}
}
