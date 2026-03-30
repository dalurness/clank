package eval

import (
	"fmt"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

// ── Handler stack for effect system ──

type handlerFrame struct {
	arms      []ast.HandlerArm
	expr      ast.Expr
	env       *Env
	loc       token.Loc
	resumeLog map[int]Value
}

var handlerStack []*handlerFrame
var performCounter int

// ── Interface/impl dispatch table ──

type implTable map[string]map[string]Value

var implTbl implTable
var interfaceMethodNames map[string]string

func init() {
	implTbl = make(implTable)
	interfaceMethodNames = make(map[string]string)
}

func registerImpl(methodName, typeTag string, impl Value) {
	if implTbl[methodName] == nil {
		implTbl[methodName] = make(map[string]Value)
	}
	implTbl[methodName][typeTag] = impl
}

func typeExprToTag(te ast.TypeExpr) string {
	switch t := te.(type) {
	case ast.TypeName:
		return t.Name
	case ast.TypeList:
		return "List"
	case ast.TypeTuple:
		return "Tuple"
	case ast.TypeRecord:
		return "Record"
	case ast.TypeGeneric:
		return t.Name
	default:
		return "?"
	}
}

func makeInterfaceDispatcher(methodName string) Value {
	return ValBuiltin{
		Name: methodName,
		Fn: func(args []Value, loc token.Loc) Value {
			if len(args) == 0 {
				panic(runtimeError("E212", fmt.Sprintf("interface method '%s' called with no arguments", methodName), loc))
			}
			tag := RuntimeTypeTag(args[0])
			if methods, ok := implTbl[methodName]; ok {
				if impl, ok := methods[tag]; ok {
					return ApplyValue(impl, args, loc)
				}
			}
			panic(runtimeError("E212", fmt.Sprintf("no impl of method '%s' for type '%s'", methodName, tag), loc))
		},
	}
}

// ── Effect operation performer ──

func makeEffectOp(opName, effectName string) Value {
	return ValBuiltin{
		Name: opName,
		Fn: func(args []Value, loc token.Loc) Value {
			id := performCounter
			performCounter++

			// Check if we're replaying and this perform has a logged resume value
			for i := len(handlerStack) - 1; i >= 0; i-- {
				frame := handlerStack[i]
				if val, ok := frame.resumeLog[id]; ok {
					return val
				}
				// Stop searching if this handler handles the operation
				for _, a := range frame.arms {
					if a.Name == opName {
						goto doneSearching
					}
				}
			}
		doneSearching:
			// Not replaying — perform the effect
			panic(&PerformSignal{Op: opName, Args: args, PerformID: id})
		},
	}
}

// ── Evaluator ──

// Evaluate evaluates an expression in the given environment.
func Evaluate(expr ast.Expr, env *Env) Value {
	switch e := expr.(type) {
	case ast.ExprLiteral:
		switch lit := e.Value.(type) {
		case ast.LitInt:
			return ValInt{Val: lit.Value}
		case ast.LitRat:
			return ValRat{Val: lit.Value}
		case ast.LitBool:
			return ValBool{Val: lit.Value}
		case ast.LitStr:
			return ValStr{Val: lit.Value}
		case ast.LitUnit:
			return ValUnit{}
		}

	case ast.ExprVar:
		return env.Get(e.Name, e.Loc)

	case ast.ExprLet:
		val := Evaluate(e.Value, env)
		if e.Body == nil {
			return val
		}
		if e.Name == "_" {
			return Evaluate(e.Body, env)
		}
		child := env.Extend()
		child.Set(e.Name, val)
		return Evaluate(e.Body, child)

	case ast.ExprIf:
		cond := Evaluate(e.Cond, env)
		bv, ok := cond.(ValBool)
		if !ok {
			panic(runtimeError("E200", fmt.Sprintf("if condition must be Bool, got %s", cond.valueTag()), e.Loc))
		}
		if bv.Val {
			return Evaluate(e.Then, env)
		}
		return Evaluate(e.Else, env)

	case ast.ExprLambda:
		return ValClosure{Params: e.Params, Body: e.Body, Env: env}

	case ast.ExprApply:
		fn := Evaluate(e.Fn, env)
		args := make([]Value, len(e.Args))
		for i, a := range e.Args {
			args[i] = Evaluate(a, env)
		}
		return ApplyValue(fn, args, e.Loc)

	// These should have been desugared away
	case ast.ExprPipeline, ast.ExprInfix, ast.ExprUnary, ast.ExprDo, ast.ExprFor, ast.ExprRange, ast.ExprLetPattern:
		panic(runtimeError("E205", fmt.Sprintf("'%T' should have been desugared", expr), expr.ExprLoc()))

	case ast.ExprList:
		elements := make([]Value, len(e.Elements))
		for i, el := range e.Elements {
			elements[i] = Evaluate(el, env)
		}
		return ValList{Elements: elements}

	case ast.ExprTuple:
		elements := make([]Value, len(e.Elements))
		for i, el := range e.Elements {
			elements[i] = Evaluate(el, env)
		}
		return ValTuple{Elements: elements}

	case ast.ExprMatch:
		subject := Evaluate(e.Subject, env)
		for _, arm := range e.Arms {
			bindings := matchPattern(arm.Pattern, subject)
			if bindings != nil {
				armEnv := env.Extend()
				for name, val := range bindings {
					armEnv.Set(name, val)
				}
				return Evaluate(arm.Body, armEnv)
			}
		}
		panic(runtimeError("E208", fmt.Sprintf("no matching pattern for %s", ShowValueBrief(subject)), e.Loc))

	case ast.ExprPerform:
		return Evaluate(e.Expr, env)

	case ast.ExprHandle:
		return evaluateHandle(e.Expr, e.Arms, env, e.Loc, make(map[int]Value))

	case ast.ExprRecord:
		fields := NewOrderedMap()
		if e.Spread != nil {
			base := Evaluate(e.Spread, env)
			rec, ok := base.(ValRecord)
			if !ok {
				panic(runtimeError("E206", fmt.Sprintf("spread requires a record value (got %s)", base.valueTag()), e.Loc))
			}
			for _, k := range rec.Fields.Keys() {
				v, _ := rec.Fields.Get(k)
				fields.Set(k, v)
			}
		}
		for _, f := range e.Fields {
			fields.Set(f.Name, Evaluate(f.Value, env))
		}
		return ValRecord{Fields: fields}

	case ast.ExprRecordUpdate:
		base := Evaluate(e.Base, env)
		rec, ok := base.(ValRecord)
		if !ok {
			panic(runtimeError("E206", fmt.Sprintf("cannot update non-record value (got %s)", base.valueTag()), e.Loc))
		}
		fields := rec.Fields.Clone()
		for _, f := range e.Fields {
			if !fields.Has(f.Name) {
				panic(runtimeError("E206", fmt.Sprintf("record has no field '%s'", f.Name), e.Loc))
			}
			fields.Set(f.Name, Evaluate(f.Value, env))
		}
		return ValRecord{Fields: fields}

	case ast.ExprFieldAccess:
		return evaluateFieldAccess(e, env)

	case ast.ExprBorrow:
		return Evaluate(e.Expr, env)
	case ast.ExprClone:
		return Evaluate(e.Expr, env)
	case ast.ExprDiscard:
		Evaluate(e.Expr, env)
		return ValUnit{}
	}

	panic(runtimeError("E299", "unknown AST node", token.Loc{}))
}

// evaluateFieldAccess handles field access with dotted builtin fallback properly.
// This is needed because the defer/recover approach above is too broad.
func evaluateFieldAccess(e ast.ExprFieldAccess, env *Env) Value {
	if varExpr, ok := e.Object.(ast.ExprVar); ok {
		dottedName := varExpr.Name + "." + e.Field
		if val := envLookup(env, dottedName); val != nil {
			return val
		}
	}
	obj := Evaluate(e.Object, env)
	if rec, ok := obj.(ValRecord); ok {
		val, exists := rec.Fields.Get(e.Field)
		if !exists {
			panic(runtimeError("E206", fmt.Sprintf("record has no field '%s'", e.Field), e.Loc))
		}
		return val
	}
	panic(runtimeError("E206", fmt.Sprintf("cannot access field '%s' on %s", e.Field, obj.valueTag()), e.Loc))
}

// envLookup tries to get a value without panicking.
func envLookup(env *Env, name string) Value {
	defer func() { recover() }()
	return env.Get(name, token.Loc{})
}

// ── Handle expression evaluation ──

func evaluateHandle(
	expr ast.Expr,
	arms []ast.HandlerArm,
	env *Env,
	loc token.Loc,
	resumeLog map[int]Value,
) (result Value) {
	frame := &handlerFrame{arms: arms, expr: expr, env: env, loc: loc, resumeLog: resumeLog}
	handlerStack = append(handlerStack, frame)
	savedCounter := performCounter
	performCounter = 0

	defer func() {
		r := recover()
		if r == nil {
			return
		}

		// Pop handler stack
		handlerStack = handlerStack[:len(handlerStack)-1]
		performCounter = savedCounter

		signal, ok := r.(*PerformSignal)
		if !ok {
			panic(r) // re-throw non-PerformSignal panics
		}

		// Find matching arm
		var arm *ast.HandlerArm
		for i := range arms {
			if arms[i].Name == signal.Op {
				arm = &arms[i]
				break
			}
		}
		if arm == nil {
			panic(signal) // propagate to outer handler
		}

		// Build one-shot continuation
		capturedResumeLog := make(map[int]Value)
		for k, v := range resumeLog {
			capturedResumeLog[k] = v
		}
		capturedPerformID := signal.PerformID
		used := false

		continuation := ValBuiltin{
			Name: fmt.Sprintf("<resume:%s>", signal.Op),
			Fn: func(kArgs []Value, kLoc token.Loc) Value {
				if used {
					panic(runtimeError("E211", "continuation already resumed (one-shot)", kLoc))
				}
				used = true
				newLog := make(map[int]Value)
				for k, v := range capturedResumeLog {
					newLog[k] = v
				}
				var resumeVal Value = ValUnit{}
				if len(kArgs) > 0 {
					resumeVal = kArgs[0]
				}
				newLog[capturedPerformID] = resumeVal
				return evaluateHandle(expr, arms, env, loc, newLog)
			},
		}

		// Call handler arm body
		armEnv := env.Extend()
		for i, p := range arm.Params {
			if i < len(signal.Args) {
				armEnv.Set(p.Name, signal.Args[i])
			} else {
				armEnv.Set(p.Name, ValUnit{})
			}
		}
		if arm.ResumeName != "" {
			armEnv.Set(arm.ResumeName, continuation)
		}
		result = Evaluate(arm.Body, armEnv)
	}()

	evalResult := Evaluate(expr, env)

	// Normal completion — pop handler stack
	handlerStack = handlerStack[:len(handlerStack)-1]
	performCounter = savedCounter

	// Apply return arm if present
	for _, arm := range arms {
		if arm.Name == "return" {
			armEnv := env.Extend()
			armEnv.Set(arm.Params[0].Name, evalResult)
			return Evaluate(arm.Body, armEnv)
		}
	}
	return evalResult
}

// ── Apply a callable value ──

// ApplyValue calls a function value with the given arguments.
func ApplyValue(fn Value, args []Value, loc token.Loc) Value {
	switch f := fn.(type) {
	case ValBuiltin:
		return f.Fn(args, loc)
	case ValClosure:
		if len(args) != len(f.Params) {
			panic(runtimeError("E203", fmt.Sprintf("expected %d arguments, got %d", len(f.Params), len(args)), loc))
		}
		callEnv := f.Env.Extend()
		for i, p := range f.Params {
			callEnv.Set(p.Name, args[i])
		}
		return Evaluate(f.Body, callEnv)
	default:
		panic(runtimeError("E204", fmt.Sprintf("cannot call %s as a function", fn.valueTag()), loc))
	}
}

// ── Pattern matching ──

func matchPattern(pattern ast.Pattern, value Value) map[string]Value {
	switch p := pattern.(type) {
	case ast.PatWildcard:
		return map[string]Value{}

	case ast.PatVar:
		return map[string]Value{p.Name: value}

	case ast.PatLiteral:
		switch lit := p.Value.(type) {
		case ast.LitInt:
			if v, ok := value.(ValInt); ok && v.Val == lit.Value {
				return map[string]Value{}
			}
		case ast.LitRat:
			if v, ok := value.(ValRat); ok && v.Val == lit.Value {
				return map[string]Value{}
			}
		case ast.LitBool:
			if v, ok := value.(ValBool); ok && v.Val == lit.Value {
				return map[string]Value{}
			}
		case ast.LitStr:
			if v, ok := value.(ValStr); ok && v.Val == lit.Value {
				return map[string]Value{}
			}
		case ast.LitUnit:
			if _, ok := value.(ValUnit); ok {
				return map[string]Value{}
			}
		}
		return nil

	case ast.PatVariant:
		v, ok := value.(ValVariant)
		if !ok || v.Name != p.Name || len(v.Fields) != len(p.Args) {
			return nil
		}
		bindings := map[string]Value{}
		for i, arg := range p.Args {
			sub := matchPattern(arg, v.Fields[i])
			if sub == nil {
				return nil
			}
			for k, val := range sub {
				bindings[k] = val
			}
		}
		return bindings

	case ast.PatTuple:
		v, ok := value.(ValTuple)
		if !ok || len(v.Elements) != len(p.Elements) {
			return nil
		}
		bindings := map[string]Value{}
		for i, el := range p.Elements {
			sub := matchPattern(el, v.Elements[i])
			if sub == nil {
				return nil
			}
			for k, val := range sub {
				bindings[k] = val
			}
		}
		return bindings

	case ast.PatRecord:
		v, ok := value.(ValRecord)
		if !ok {
			return nil
		}
		bindings := map[string]Value{}
		matchedNames := map[string]bool{}
		for _, pf := range p.Fields {
			fieldVal, exists := v.Fields.Get(pf.Name)
			if !exists {
				return nil
			}
			matchedNames[pf.Name] = true
			if pf.Pattern != nil {
				sub := matchPattern(pf.Pattern, fieldVal)
				if sub == nil {
					return nil
				}
				for k, val := range sub {
					bindings[k] = val
				}
			} else {
				// Punned field
				bindings[pf.Name] = fieldVal
			}
		}
		if p.Rest == "" {
			// Closed record pattern — must match exactly
			if v.Fields.Len() != len(p.Fields) {
				return nil
			}
		} else if p.Rest != "_" {
			// Open record pattern with rest capture
			restFields := NewOrderedMap()
			for _, k := range v.Fields.Keys() {
				if !matchedNames[k] {
					val, _ := v.Fields.Get(k)
					restFields.Set(k, val)
				}
			}
			bindings[p.Rest] = ValRecord{Fields: restFields}
		}
		return bindings
	}
	return nil
}
