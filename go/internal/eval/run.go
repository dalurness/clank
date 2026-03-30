package eval

import (
	"fmt"
	"sort"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

// BaseDir is the directory used for resolving module imports.
// Set this before calling Run or LoadTopLevels when module imports are needed.
var BaseDir string

// ── Variant constructor ──

func makeVariantConstructor(vname string, arity int) Value {
	if arity == 0 {
		return ValVariant{Name: vname, Fields: []Value{}}
	}
	return ValBuiltin{
		Name: vname,
		Fn: func(args []Value, loc token.Loc) Value {
			if len(args) != arity {
				panic(runtimeError("E203", fmt.Sprintf("%s expects %d arguments, got %d", vname, arity, len(args)), loc))
			}
			fields := make([]Value, len(args))
			copy(fields, args)
			return ValVariant{Name: vname, Fields: fields}
		},
	}
}

// ── Derived impls ──

func registerDerivedImpls(variants []ast.Variant, deriving []string, env *Env) {
	noLoc := token.Loc{}

	for _, ifaceName := range deriving {
		switch ifaceName {
		case "Show":
			for _, v := range variants {
				vname := v.Name
				registerImpl("show", vname, ValBuiltin{
					Name: "show:" + vname,
					Fn: func(args []Value, _ token.Loc) Value {
						val := args[0].(ValVariant)
						if len(val.Fields) == 0 {
							return ValStr{Val: val.Name}
						}
						parts := make([]string, len(val.Fields))
						for i, f := range val.Fields {
							shown := ApplyValue(env.Get("show", noLoc), []Value{f}, noLoc)
							parts[i] = shown.(ValStr).Val
						}
						return ValStr{Val: fmt.Sprintf("%s(%s)", val.Name, joinStrings(parts, ", "))}
					},
				})
			}
		case "Eq":
			for _, v := range variants {
				vname := v.Name
				registerImpl("eq", vname, ValBuiltin{
					Name: "eq:" + vname,
					Fn: func(args []Value, _ token.Loc) Value {
						a := args[0].(ValVariant)
						b, ok := args[1].(ValVariant)
						if !ok || a.Name != b.Name {
							return ValBool{Val: false}
						}
						for i := range a.Fields {
							result := ApplyValue(env.Get("eq", noLoc), []Value{a.Fields[i], b.Fields[i]}, noLoc)
							if bv, ok := result.(ValBool); ok && !bv.Val {
								return ValBool{Val: false}
							}
						}
						return ValBool{Val: true}
					},
				})
			}

		case "Ord":
			for vi, v := range variants {
				vname := v.Name
				variantIndex := vi
				registerImpl("cmp", vname, ValBuiltin{
					Name: "cmp:" + vname,
					Fn: func(args []Value, loc token.Loc) Value {
						a := args[0].(ValVariant)
						b := args[1].(ValVariant)
						// Find ordinal indices
						ai := variantIndex
						bi := -1
						for j, vv := range variants {
							if vv.Name == b.Name {
								bi = j
								break
							}
						}
						if ai < bi {
							return ValVariant{Name: "Lt", Fields: []Value{}}
						}
						if ai > bi {
							return ValVariant{Name: "Gt", Fields: []Value{}}
						}
						// Same variant: compare fields lexicographically
						for i := 0; i < len(a.Fields); i++ {
							result := callCmp(a.Fields[i], b.Fields[i], loc)
							if v, ok := result.(ValVariant); ok && v.Name != "Eq_" {
								return result
							}
						}
						return ValVariant{Name: "Eq_", Fields: []Value{}}
					},
				})
			}

		case "Default":
			// Default uses the first nullary variant
			for _, v := range variants {
				if len(v.Fields) == 0 {
					vname := v.Name
					registerImpl("default", vname, ValBuiltin{
						Name: "default:" + vname,
						Fn: func(_ []Value, _ token.Loc) Value {
							return ValVariant{Name: vname, Fields: []Value{}}
						},
					})
					break
				}
			}
		}
	}
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}

// ── Built-in interface impls ──

func registerBuiltinImpls() {
	// show for primitives
	registerImpl("show", "Int", ValBuiltin{Name: "show:Int", Fn: func(args []Value, _ token.Loc) Value {
		return ValStr{Val: fmt.Sprintf("%d", args[0].(ValInt).Val)}
	}})
	registerImpl("show", "Rat", ValBuiltin{Name: "show:Rat", Fn: func(args []Value, _ token.Loc) Value {
		return ValStr{Val: fmt.Sprintf("%g", args[0].(ValRat).Val)}
	}})
	registerImpl("show", "Bool", ValBuiltin{Name: "show:Bool", Fn: func(args []Value, _ token.Loc) Value {
		if args[0].(ValBool).Val {
			return ValStr{Val: "true"}
		}
		return ValStr{Val: "false"}
	}})
	registerImpl("show", "Str", ValBuiltin{Name: "show:Str", Fn: func(args []Value, _ token.Loc) Value {
		return args[0]
	}})
	registerImpl("show", "Unit", ValBuiltin{Name: "show:Unit", Fn: func(_ []Value, _ token.Loc) Value {
		return ValStr{Val: "()"}
	}})

	// eq for primitives
	registerImpl("eq", "Int", ValBuiltin{Name: "eq:Int", Fn: func(args []Value, _ token.Loc) Value {
		return ValBool{Val: args[0].(ValInt).Val == args[1].(ValInt).Val}
	}})
	registerImpl("eq", "Rat", ValBuiltin{Name: "eq:Rat", Fn: func(args []Value, _ token.Loc) Value {
		return ValBool{Val: args[0].(ValRat).Val == args[1].(ValRat).Val}
	}})
	registerImpl("eq", "Bool", ValBuiltin{Name: "eq:Bool", Fn: func(args []Value, _ token.Loc) Value {
		return ValBool{Val: args[0].(ValBool).Val == args[1].(ValBool).Val}
	}})
	registerImpl("eq", "Str", ValBuiltin{Name: "eq:Str", Fn: func(args []Value, _ token.Loc) Value {
		return ValBool{Val: args[0].(ValStr).Val == args[1].(ValStr).Val}
	}})

	// default for primitives
	registerImpl("default", "Int", ValBuiltin{Name: "default:Int", Fn: func(_ []Value, _ token.Loc) Value {
		return ValInt{Val: 0}
	}})
	registerImpl("default", "Rat", ValBuiltin{Name: "default:Rat", Fn: func(_ []Value, _ token.Loc) Value {
		return ValRat{Val: 0.0}
	}})
	registerImpl("default", "Bool", ValBuiltin{Name: "default:Bool", Fn: func(_ []Value, _ token.Loc) Value {
		return ValBool{Val: false}
	}})
	registerImpl("default", "Str", ValBuiltin{Name: "default:Str", Fn: func(_ []Value, _ token.Loc) Value {
		return ValStr{Val: ""}
	}})
	registerImpl("default", "Unit", ValBuiltin{Name: "default:Unit", Fn: func(_ []Value, _ token.Loc) Value {
		return ValUnit{}
	}})

	// cmp for Int, Rat, Str
	cmpResult := func(less bool, greater bool) Value {
		if less {
			return ValVariant{Name: "Lt", Fields: []Value{}}
		}
		if greater {
			return ValVariant{Name: "Gt", Fields: []Value{}}
		}
		return ValVariant{Name: "Eq_", Fields: []Value{}}
	}
	registerImpl("cmp", "Int", ValBuiltin{Name: "cmp:Int", Fn: func(args []Value, _ token.Loc) Value {
		a, b := args[0].(ValInt).Val, args[1].(ValInt).Val
		return cmpResult(a < b, a > b)
	}})
	registerImpl("cmp", "Rat", ValBuiltin{Name: "cmp:Rat", Fn: func(args []Value, _ token.Loc) Value {
		a, b := args[0].(ValRat).Val, args[1].(ValRat).Val
		return cmpResult(a < b, a > b)
	}})
	registerImpl("cmp", "Str", ValBuiltin{Name: "cmp:Str", Fn: func(args []Value, _ token.Loc) Value {
		a, b := args[0].(ValStr).Val, args[1].(ValStr).Val
		return cmpResult(a < b, a > b)
	}})

	// cmp for List — lexicographic element-wise comparison
	registerImpl("cmp", "List", ValBuiltin{Name: "cmp:List", Fn: func(args []Value, loc token.Loc) Value {
		a := args[0].(ValList).Elements
		b := args[1].(ValList).Elements
		minLen := len(a)
		if len(b) < minLen {
			minLen = len(b)
		}
		for i := 0; i < minLen; i++ {
			result := callCmp(a[i], b[i], loc)
			if v, ok := result.(ValVariant); ok && v.Name != "Eq_" {
				return result
			}
		}
		return cmpResult(len(a) < len(b), len(a) > len(b))
	}})

	// cmp for Tuple — element-wise comparison
	registerImpl("cmp", "Tuple", ValBuiltin{Name: "cmp:Tuple", Fn: func(args []Value, loc token.Loc) Value {
		a := args[0].(ValTuple).Elements
		b := args[1].(ValTuple).Elements
		minLen := len(a)
		if len(b) < minLen {
			minLen = len(b)
		}
		for i := 0; i < minLen; i++ {
			result := callCmp(a[i], b[i], loc)
			if v, ok := result.(ValVariant); ok && v.Name != "Eq_" {
				return result
			}
		}
		return cmpResult(len(a) < len(b), len(a) > len(b))
	}})

	// cmp for Record — compare fields lexicographically by sorted key order
	registerImpl("cmp", "Record", ValBuiltin{Name: "cmp:Record", Fn: func(args []Value, loc token.Loc) Value {
		a := args[0].(ValRecord)
		b := args[1].(ValRecord)
		keys := make([]string, len(a.Fields.Keys()))
		copy(keys, a.Fields.Keys())
		sort.Strings(keys)
		for _, key := range keys {
			av, _ := a.Fields.Get(key)
			bv, ok := b.Fields.Get(key)
			if !ok {
				continue
			}
			result := callCmp(av, bv, loc)
			if v, ok := result.(ValVariant); ok && v.Name != "Eq_" {
				return result
			}
		}
		return ValVariant{Name: "Eq_", Fields: []Value{}}
	}})
}

// callCmp dispatches cmp for a value via the impl table.
func callCmp(a, b Value, loc token.Loc) Value {
	tag := RuntimeTypeTag(a)
	if methods, ok := implTbl["cmp"]; ok {
		if impl, ok := methods[tag]; ok {
			return ApplyValue(impl, []Value{a, b}, loc)
		}
	}
	panic(runtimeError("E212", fmt.Sprintf("no impl of method 'cmp' for type '%s'", tag), loc))
}

// ── InitGlobalEnv creates a fresh global environment with builtins ──

func InitGlobalEnv() *Env {
	SetApplyFn(ApplyValue)
	handlerStack = nil
	performCounter = 0
	implTbl = make(implTable)
	interfaceMethodNames = make(map[string]string)
	registerBuiltinImpls()
	resetModuleCache()

	global := NewEnv(nil)
	for name, fn := range Builtins() {
		global.Set(name, ValBuiltin{Name: name, Fn: fn})
	}

	// Built-in Ordering type for Ord interface
	global.Set("Lt", ValVariant{Name: "Lt", Fields: []Value{}})
	global.Set("Gt", ValVariant{Name: "Gt", Fields: []Value{}})
	global.Set("Eq_", ValVariant{Name: "Eq_", Fields: []Value{}})

	// Built-in interface method dispatchers with fallback to builtins
	builtinShow := global.Get("show", token.Loc{})
	global.Set("show", ValBuiltin{
		Name: "show",
		Fn: func(args []Value, loc token.Loc) Value {
			tag := RuntimeTypeTag(args[0])
			if methods, ok := implTbl["show"]; ok {
				if impl, ok := methods[tag]; ok {
					return ApplyValue(impl, args, loc)
				}
			}
			return ApplyValue(builtinShow, args, loc)
		},
	})

	builtinEq := global.Get("eq", token.Loc{})
	global.Set("eq", ValBuiltin{
		Name: "eq",
		Fn: func(args []Value, loc token.Loc) Value {
			tag := RuntimeTypeTag(args[0])
			if methods, ok := implTbl["eq"]; ok {
				if impl, ok := methods[tag]; ok {
					return ApplyValue(impl, args, loc)
				}
			}
			return ApplyValue(builtinEq, args, loc)
		},
	})

	global.Set("cmp", makeInterfaceDispatcher("cmp"))
	global.Set("clone", makeInterfaceDispatcher("clone"))
	global.Set("default", makeInterfaceDispatcher("default"))
	global.Set("into", makeInterfaceDispatcher("into"))
	global.Set("from", makeInterfaceDispatcher("from"))

	// Built-in effects
	global.Set("exn", ValEffectDef{Name: "exn", Ops: []string{"raise"}})
	global.Set("raise", makeEffectOp("raise", "exn"))
	global.Set("io", ValEffectDef{Name: "io", Ops: []string{"print", "read-ln"}})

	// Override div to raise on division by zero (via effect system)
	global.Set("div", ValBuiltin{
		Name: "div",
		Fn: func(args []Value, loc token.Loc) Value {
			a := expectNum(args[0], loc)
			b := expectNum(args[1], loc)
			if b == 0 {
				id := performCounter
				performCounter++
				for i := len(handlerStack) - 1; i >= 0; i-- {
					frame := handlerStack[i]
					if val, ok := frame.resumeLog[id]; ok {
						return val
					}
					for _, arm := range frame.arms {
						if arm.Name == "raise" {
							goto doneSearch
						}
					}
				}
			doneSearch:
				panic(&PerformSignal{Op: "raise", Args: []Value{ValStr{Val: "division by zero"}}, PerformID: id})
			}
			if isRat(args[0], args[1]) {
				return ValRat{Val: a / b}
			}
			return ValInt{Val: int64(a / b)}
		},
	})

	// Override mod to raise on modulo by zero
	global.Set("mod", ValBuiltin{
		Name: "mod",
		Fn: func(args []Value, loc token.Loc) Value {
			a := expectNum(args[0], loc)
			b := expectNum(args[1], loc)
			if b == 0 {
				id := performCounter
				performCounter++
				for i := len(handlerStack) - 1; i >= 0; i-- {
					frame := handlerStack[i]
					if val, ok := frame.resumeLog[id]; ok {
						return val
					}
					for _, arm := range frame.arms {
						if arm.Name == "raise" {
							goto doneSearchMod
						}
					}
				}
			doneSearchMod:
				panic(&PerformSignal{Op: "raise", Args: []Value{ValStr{Val: "modulo by zero"}}, PerformID: id})
			}
			return ValInt{Val: int64(a) % int64(b)}
		},
	})

	return global
}

// ── LoadTopLevels processes top-level declarations ──

func LoadTopLevels(env *Env, program *ast.Program) {
	for _, tl := range program.TopLevels {
		switch decl := tl.(type) {
		case ast.TopModDecl:
			continue

		case ast.TopUseDecl:
			HandleUseDecl(decl, env, BaseDir)

		case ast.TopTypeDecl:
			for _, v := range decl.Variants {
				env.Set(v.Name, makeVariantConstructor(v.Name, len(v.Fields)))
			}
			if len(decl.Deriving) > 0 {
				registerDerivedImpls(decl.Variants, decl.Deriving, env)
			}

		case ast.TopEffectDecl:
			opNames := make([]string, len(decl.Ops))
			for i, op := range decl.Ops {
				opNames[i] = op.Name
			}
			env.Set(decl.Name, ValEffectDef{Name: decl.Name, Ops: opNames})
			for _, op := range decl.Ops {
				env.Set(op.Name, makeEffectOp(op.Name, decl.Name))
			}

		case ast.TopEffectAlias:
			effects := make([]string, len(decl.Effects))
			for i, e := range decl.Effects {
				effects[i] = e.Name
			}
			env.Set(decl.Name, ValEffectDef{Name: decl.Name, Ops: effects})

		case ast.TopInterfaceDecl:
			for _, m := range decl.Methods {
				interfaceMethodNames[m.Name] = decl.Name
				env.Set(m.Name, makeInterfaceDispatcher(m.Name))
			}

		case ast.TopImplBlock:
			typeTag := typeExprToTag(decl.ForType)
			for _, m := range decl.Methods {
				bodyValue := Evaluate(m.Body, env)
				// For From<T>, dispatch `from` on the source type (T), not Self
				if decl.Interface == "From" && m.Name == "from" && len(decl.TypeArgs) > 0 {
					sourceTypeTag := typeExprToTag(decl.TypeArgs[0])
					registerImpl(m.Name, sourceTypeTag, bodyValue)
				} else {
					registerImpl(m.Name, typeTag, bodyValue)
				}
			}
			// Blanket rule: impl From<A> for B → register into for type A
			if decl.Interface == "From" && len(decl.TypeArgs) > 0 {
				sourceTypeTag := typeExprToTag(decl.TypeArgs[0])
				if fromImpl, ok := implTbl["from"][sourceTypeTag]; ok {
					registerImpl("into", sourceTypeTag, ValBuiltin{
						Name: fmt.Sprintf("into:%s->%s", sourceTypeTag, typeTag),
						Fn: func(args []Value, loc token.Loc) Value {
							return ApplyValue(fromImpl, args, loc)
						},
					})
				}
			}

		case ast.TopDefinition:
			params := make([]ast.Param, len(decl.Sig.Params))
			for i, p := range decl.Sig.Params {
				params[i] = ast.Param{Name: p.Name}
			}
			closure := ValClosure{Params: params, Body: decl.Body, Env: env}
			env.Set(decl.Name, closure)

		case ast.TopTestDecl:
			// Tests are not loaded — they're run separately
			continue
		}
	}
}

// ── Run executes a program ──

// Run executes a Clank program by loading top-levels and calling main.
func Run(program *ast.Program) (result Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(*RuntimeError); ok {
				err = re
				return
			}
			if ps, ok := r.(*PerformSignal); ok {
				err = fmt.Errorf("unhandled effect: %s", ps.Op)
				return
			}
			panic(r)
		}
	}()

	env := InitGlobalEnv()
	LoadTopLevels(env, program)

	// Find and call main
	mainVal := envLookup(env, "main")
	if mainVal == nil {
		return ValUnit{}, nil
	}
	result = ApplyValue(mainVal, []Value{}, token.Loc{})
	return result, nil
}

// RunExpr evaluates a single expression in a fresh environment.
func RunExpr(expr ast.Expr) (result Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(*RuntimeError); ok {
				err = re
				return
			}
			if ps, ok := r.(*PerformSignal); ok {
				err = fmt.Errorf("unhandled effect: %s", ps.Op)
				return
			}
			panic(r)
		}
	}()

	env := InitGlobalEnv()
	result = Evaluate(expr, env)
	return result, nil
}
