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

// ── Effect row variable unification ──

func TestEffectRowVariableOpenRow(t *testing.T) {
	// A function with effect row variable <E, io> should accept a body
	// performing io without W401 errors. The E is an open tail.
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopEffectDecl{
				Name: "io",
				Ops: []ast.OpSig{
					{Name: "print_line", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopEffectDecl{
				Name: "exn",
				Ops: []ast.OpSig{
					{Name: "raise", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "greet",
				Sig: ast.TypeSig{
					Params:     []ast.TypeSigParam{{Name: "name", Type: ast.TypeName{Name: "Str"}}},
					Effects:    []ast.EffectRef{{Name: "E"}, {Name: "io"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprPerform{
					Expr: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "print_line", Loc: loc},
						Args: []ast.Expr{ast.ExprVar{Name: "name", Loc: loc}},
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

	errors := TypeCheck(program)
	for _, e := range errors {
		if e.Code == "W401" {
			t.Errorf("effect row variable <E, io> should allow io in body, got: %s", e.Error())
		}
	}
}

func TestEffectRowVariableAbsorbsExtra(t *testing.T) {
	// A function with <E, io> should also allow performing exn (absorbed by E).
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopEffectDecl{
				Name: "io",
				Ops: []ast.OpSig{
					{Name: "print_line", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopEffectDecl{
				Name: "exn",
				Ops: []ast.OpSig{
					{Name: "raise", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "risky_greet",
				Sig: ast.TypeSig{
					Params:     []ast.TypeSigParam{{Name: "name", Type: ast.TypeName{Name: "Str"}}},
					Effects:    []ast.EffectRef{{Name: "E"}, {Name: "io"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprLet{
					Name: "_",
					Value: ast.ExprPerform{
						Expr: ast.ExprApply{
							Fn:   ast.ExprVar{Name: "print_line", Loc: loc},
							Args: []ast.Expr{ast.ExprVar{Name: "name", Loc: loc}},
							Loc:  loc,
						},
						Loc: loc,
					},
					Body: ast.ExprPerform{
						Expr: ast.ExprApply{
							Fn:   ast.ExprVar{Name: "raise", Loc: loc},
							Args: []ast.Expr{ast.ExprLiteral{Value: ast.LitStr{Value: "error"}, Loc: loc}},
							Loc:  loc,
						},
						Loc: loc,
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

	errors := TypeCheck(program)
	for _, e := range errors {
		if e.Code == "W401" {
			t.Errorf("effect row variable <E, io> should absorb exn via E, got: %s", e.Error())
		}
	}
}

func TestEffectSubtypingSubsetAllowed(t *testing.T) {
	// A closed row <io> should be accepted where <io, exn> is expected (subset).
	// Declaring <io, exn> but only performing io should pass (already worked).
	loc := token.Loc{Line: 1, Col: 1}
	program := &ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopEffectDecl{
				Name: "io",
				Ops: []ast.OpSig{
					{Name: "print_line", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopEffectDecl{
				Name: "exn",
				Ops: []ast.OpSig{
					{Name: "raise", Sig: ast.TypeSig{
						Params:     []ast.TypeSigParam{{Name: "msg", Type: ast.TypeName{Name: "Str"}}},
						ReturnType: ast.TypeName{Name: "()"},
					}},
				},
				Loc: loc,
			},
			ast.TopDefinition{
				Name: "safe_fn",
				Sig: ast.TypeSig{
					Params:     nil,
					Effects:    []ast.EffectRef{{Name: "io"}, {Name: "exn"}},
					ReturnType: ast.TypeName{Name: "()"},
				},
				Body: ast.ExprPerform{
					Expr: ast.ExprApply{
						Fn:   ast.ExprVar{Name: "print_line", Loc: loc},
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

	errors := TypeCheck(program)
	for _, e := range errors {
		if e.Code == "W401" {
			t.Errorf("declaring <io, exn> but only performing io should be fine, got: %s", e.Error())
		}
	}
}

func TestEffectRowUnification(t *testing.T) {
	// Test that unifyEffectRows correctly binds row variables.
	ResetVarCounter()
	s := &checkerState{
		effectRowSubst: make(map[int]*effectRowSubstEntry),
		rowSubst:       make(map[int]*rowSubstEntry),
		typeSubst:      make(map[int]Type),
		knownTypes:     make(map[string]bool),
	}

	// Row a: <E, io> where E is var 100
	rowA := []Effect{EVar{ID: 100}, ENamed{Name: "io"}}
	// Row b: <io, exn> (closed)
	rowB := []Effect{ENamed{Name: "io"}, ENamed{Name: "exn"}}

	var errs []TypeError
	ok := s.unifyEffectRows(rowA, rowB, &errs, token.Loc{})
	if !ok {
		t.Fatalf("unifyEffectRows should succeed, got errors: %v", errs)
	}

	// E (var 100) should now be bound to contain exn
	sub, exists := s.effectRowSubst[100]
	if !exists {
		t.Fatal("expected effect row var 100 to be substituted")
	}
	foundExn := false
	for _, e := range sub.effects {
		if n, ok := e.(ENamed); ok && n.Name == "exn" {
			foundExn = true
		}
	}
	if !foundExn {
		t.Errorf("expected E to absorb 'exn', got: %v", sub.effects)
	}
}

func TestEffectRowSubsetCheck(t *testing.T) {
	// <io> is a subset of <io, exn>
	s := &checkerState{
		effectRowSubst: make(map[int]*effectRowSubstEntry),
	}

	sub := []Effect{ENamed{Name: "io"}}
	sup := []Effect{ENamed{Name: "io"}, ENamed{Name: "exn"}}

	if !s.effectRowIsSubset(sub, sup) {
		t.Error("<io> should be a subset of <io, exn>")
	}

	// <io, exn> is NOT a subset of <io>
	if s.effectRowIsSubset(sup, sub) {
		t.Error("<io, exn> should NOT be a subset of <io>")
	}
}

func TestExtractTypeBindingsCompositeWithTAny(t *testing.T) {
	// Regression: composites containing tAny (e.g. {x: Int, y: ?}) should still
	// produce a binding via showType fallback, not silently skip.

	// Record with a tAny field: {x: Int, y: ?}
	recType := TRecord{
		Fields: []RecordField{
			{Name: "x", Type: TPrimitive{Name: "int"}},
			{Name: "y", Type: TGeneric{Name: "?"}},
		},
		RowVar: -1,
	}
	paramExpr := ast.TypeName{Name: "T"}
	bindings := map[string]string{}
	extractTypeBindings(paramExpr, recType, bindings)

	if val, ok := bindings["T"]; !ok {
		t.Error("expected T to be bound for record containing tAny, but it was not")
	} else if val == "" {
		t.Error("expected T binding to be non-empty")
	} else {
		// showType should produce something like "{x: Int, y: ?}"
		if !strings.Contains(val, "x:") {
			t.Errorf("expected T binding to contain field name, got: %s", val)
		}
	}

	// List with tAny element: [?]
	listType := TList{Element: TGeneric{Name: "?"}}
	bindings2 := map[string]string{}
	extractTypeBindings(paramExpr, listType, bindings2)

	if val, ok := bindings2["T"]; !ok {
		t.Error("expected T to be bound for list containing tAny, but it was not")
	} else if val == "" {
		t.Error("expected T binding to be non-empty")
	}

	// Tuple with tAny element: (Int, ?)
	tupleType := TTuple{Elements: []Type{TPrimitive{Name: "int"}, TGeneric{Name: "?"}}}
	bindings3 := map[string]string{}
	extractTypeBindings(paramExpr, tupleType, bindings3)

	if val, ok := bindings3["T"]; !ok {
		t.Error("expected T to be bound for tuple containing tAny, but it was not")
	} else if val == "" {
		t.Error("expected T binding to be non-empty")
	}
}
