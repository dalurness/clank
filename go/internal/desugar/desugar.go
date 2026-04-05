// Package desugar implements the Clank desugaring pass.
//
// AST-to-AST transform that eliminates syntactic sugar, producing a Core AST
// where the evaluator only handles: literal, var, let, if, apply, lambda, match.
//
// Transforms:
//
//	pipeline:  x |> f(y)      → f(x, y)
//	infix:     a + b          → add(a, b)
//	infix:     a ++ b         → str.cat(a, b)
//	unary:     !x             → not(x)
//	unary:     -x             → negate(x)
//	do-block:  do { x <- e1; e2 } → let x = e1 in e2
package desugar

import (
	"fmt"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

// Operator-to-function mapping for infix desugaring.
var infixOps = map[string]string{
	"+":  "add",
	"-":  "sub",
	"*":  "mul",
	"/":  "div",
	"%":  "mod",
	"==": "eq",
	"!=": "neq",
	"<":  "lt",
	">":  "gt",
	"<=": "lte",
	">=": "gte",
	"&&": "and",
	"||": "or",
	"++": "str.cat",
}

var unaryOps = map[string]string{
	"!": "not",
	"-": "negate",
}

// Desugar transforms an expression, eliminating syntactic sugar.
func Desugar(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case ast.ExprLiteral:
		return e
	case ast.ExprVar:
		return e
	case ast.ExprLet:
		var body ast.Expr
		if e.Body != nil {
			body = Desugar(e.Body)
		}
		return ast.ExprLet{Name: e.Name, Value: Desugar(e.Value), Body: body, Loc: e.Loc}
	case ast.ExprLetPattern:
		desugaredValue := Desugar(e.Value)
		var desugaredBody ast.Expr
		if e.Body != nil {
			desugaredBody = Desugar(e.Body)
		} else {
			desugaredBody = ast.ExprLiteral{Value: ast.LitUnit{}, Loc: e.Loc}
		}
		return ast.ExprMatch{
			Subject: desugaredValue,
			Arms:    []ast.MatchArm{{Pattern: e.Pattern, Body: desugaredBody}},
			Loc:     e.Loc,
		}
	case ast.ExprIf:
		return ast.ExprIf{
			Cond: Desugar(e.Cond),
			Then: Desugar(e.Then),
			Else: Desugar(e.Else),
			Loc:  e.Loc,
		}
	case ast.ExprMatch:
		arms := make([]ast.MatchArm, len(e.Arms))
		for i, a := range e.Arms {
			arms[i] = ast.MatchArm{Pattern: a.Pattern, Body: Desugar(a.Body)}
		}
		return ast.ExprMatch{Subject: Desugar(e.Subject), Arms: arms, Loc: e.Loc}
	case ast.ExprLambda:
		return ast.ExprLambda{Params: e.Params, Body: Desugar(e.Body), Loc: e.Loc}
	case ast.ExprApply:
		args := make([]ast.Expr, len(e.Args))
		for i, a := range e.Args {
			args[i] = Desugar(a)
		}
		return ast.ExprApply{Fn: Desugar(e.Fn), Args: args, Loc: e.Loc}

	// Sugar — transform away

	case ast.ExprPipeline:
		left := Desugar(e.Left)
		right := Desugar(e.Right)
		if apply, ok := right.(ast.ExprApply); ok {
			args := make([]ast.Expr, 0, 1+len(apply.Args))
			args = append(args, left)
			args = append(args, apply.Args...)
			return ast.ExprApply{Fn: apply.Fn, Args: args, Loc: e.Loc}
		}
		return ast.ExprApply{Fn: right, Args: []ast.Expr{left}, Loc: e.Loc}

	case ast.ExprInfix:
		// Short-circuit: a && b → if a then b else false
		if e.Op == "&&" {
			return ast.ExprIf{
				Cond: Desugar(e.Left),
				Then: Desugar(e.Right),
				Else: ast.ExprLiteral{Value: ast.LitBool{Value: false}, Loc: e.Loc},
				Loc:  e.Loc,
			}
		}
		// Short-circuit: a || b → if a then true else b
		if e.Op == "||" {
			return ast.ExprIf{
				Cond: Desugar(e.Left),
				Then: ast.ExprLiteral{Value: ast.LitBool{Value: true}, Loc: e.Loc},
				Else: Desugar(e.Right),
				Loc:  e.Loc,
			}
		}
		fn := e.Op
		if mapped, ok := infixOps[e.Op]; ok {
			fn = mapped
		}
		return ast.ExprApply{
			Fn:   ast.ExprVar{Name: fn, Loc: e.Loc},
			Args: []ast.Expr{Desugar(e.Left), Desugar(e.Right)},
			Loc:  e.Loc,
		}

	case ast.ExprUnary:
		fn := e.Op
		if mapped, ok := unaryOps[e.Op]; ok {
			fn = mapped
		}
		return ast.ExprApply{
			Fn:   ast.ExprVar{Name: fn, Loc: e.Loc},
			Args: []ast.Expr{Desugar(e.Operand)},
			Loc:  e.Loc,
		}

	case ast.ExprDo:
		return desugarDo(e.Steps, e.Loc)

	case ast.ExprFor:
		return desugarFor(e)

	case ast.ExprRange:
		start := Desugar(e.Start)
		end := Desugar(e.End)
		rangeFn := ast.ExprVar{Name: "range", Loc: e.Loc}
		var endArg ast.Expr
		if e.Inclusive {
			endArg = end
		} else {
			endArg = ast.ExprApply{
				Fn:   ast.ExprVar{Name: "sub", Loc: e.Loc},
				Args: []ast.Expr{end, ast.ExprLiteral{Value: ast.LitInt{Value: 1}, Loc: e.Loc}},
				Loc:  e.Loc,
			}
		}
		return ast.ExprApply{Fn: rangeFn, Args: []ast.Expr{start, endArg}, Loc: e.Loc}

	// Pass-through nodes — recurse but keep the tag

	case ast.ExprHandle:
		arms := make([]ast.HandlerArm, len(e.Arms))
		for i, a := range e.Arms {
			arms[i] = ast.HandlerArm{
				Name:       a.Name,
				Params:     a.Params,
				ResumeName: a.ResumeName,
				Body:       Desugar(a.Body),
			}
		}
		return ast.ExprHandle{Expr: Desugar(e.Expr), Arms: arms, Loc: e.Loc}

	case ast.ExprPerform:
		return ast.ExprPerform{Expr: Desugar(e.Expr), Loc: e.Loc}

	case ast.ExprList:
		elems := make([]ast.Expr, len(e.Elements))
		for i, el := range e.Elements {
			elems[i] = Desugar(el)
		}
		return ast.ExprList{Elements: elems, Loc: e.Loc}

	case ast.ExprTuple:
		elems := make([]ast.Expr, len(e.Elements))
		for i, el := range e.Elements {
			elems[i] = Desugar(el)
		}
		return ast.ExprTuple{Elements: elems, Loc: e.Loc}

	case ast.ExprRecord:
		fields := make([]ast.RecordField, len(e.Fields))
		for i, f := range e.Fields {
			fields[i] = ast.RecordField{Name: f.Name, Tags: f.Tags, Value: Desugar(f.Value)}
		}
		var spread ast.Expr
		if e.Spread != nil {
			spread = Desugar(e.Spread)
		}
		return ast.ExprRecord{Fields: fields, Spread: spread, Loc: e.Loc}

	case ast.ExprRecordUpdate:
		fields := make([]ast.RecordUpdateField, len(e.Fields))
		for i, f := range e.Fields {
			fields[i] = ast.RecordUpdateField{Name: f.Name, Value: Desugar(f.Value)}
		}
		return ast.ExprRecordUpdate{Base: Desugar(e.Base), Fields: fields, Loc: e.Loc}

	case ast.ExprFieldAccess:
		return ast.ExprFieldAccess{Object: Desugar(e.Object), Field: e.Field, Loc: e.Loc}

	case ast.ExprBorrow:
		return ast.ExprBorrow{Expr: Desugar(e.Expr), Loc: e.Loc}
	case ast.ExprClone:
		return ast.ExprClone{Expr: Desugar(e.Expr), Loc: e.Loc}
	case ast.ExprDiscard:
		return ast.ExprDiscard{Expr: Desugar(e.Expr), Loc: e.Loc}

	default:
		panic(fmt.Sprintf("desugar: unknown AST node type %T", expr))
	}
}

// desugarDo flattens do-block steps into nested let expressions.
func desugarDo(steps []ast.DoStep, loc token.Loc) ast.Expr {
	if len(steps) == 0 {
		return ast.ExprLiteral{Value: ast.LitUnit{}, Loc: loc}
	}
	if len(steps) == 1 {
		return Desugar(steps[0].Expr)
	}
	head := steps[0]
	rest := desugarDo(steps[1:], loc)

	// If the step is a bare `let x = e` (no `in`, no `<-` bind), lift the
	// let so the remainder of the block becomes its body. This makes
	//   do { let x = e1; e2 }
	// equivalent to
	//   do { x <- e1; e2 }
	// i.e. `x` is in scope for all subsequent steps.
	if head.Bind == "" {
		if letExpr, ok := head.Expr.(ast.ExprLet); ok && letExpr.Body == nil {
			return ast.ExprLet{
				Name:  letExpr.Name,
				Value: Desugar(letExpr.Value),
				Body:  rest,
				Loc:   letExpr.Loc,
			}
		}
	}

	name := head.Bind
	if name == "" {
		name = "_"
	}
	return ast.ExprLet{
		Name:  name,
		Value: Desugar(head.Expr),
		Body:  rest,
		Loc:   loc,
	}
}

// patternToParams converts a pattern to lambda params.
func patternToParams(pat ast.Pattern) []ast.Param {
	if pv, ok := pat.(ast.PatVar); ok {
		return []ast.Param{{Name: pv.Name}}
	}
	return []ast.Param{{Name: "__for_elem"}}
}

// wrapBodyWithPattern wraps body in a match if the pattern is not a simple variable.
func wrapBodyWithPattern(pat ast.Pattern, body ast.Expr, loc token.Loc) ast.Expr {
	if _, ok := pat.(ast.PatVar); ok {
		return body
	}
	return ast.ExprMatch{
		Subject: ast.ExprVar{Name: "__for_elem", Loc: loc},
		Arms:    []ast.MatchArm{{Pattern: pat, Body: body}},
		Loc:     loc,
	}
}

// desugarFor desugars for-expressions using runtime-dispatched builtins.
func desugarFor(expr ast.ExprFor) ast.Expr {
	loc := expr.Loc
	collection := Desugar(expr.Collection)
	body := Desugar(expr.Body)
	var guard ast.Expr
	if expr.Guard != nil {
		guard = Desugar(expr.Guard)
	}

	elemParams := patternToParams(expr.Bind)
	wrappedBody := wrapBodyWithPattern(expr.Bind, body, loc)
	var wrappedGuard ast.Expr
	if guard != nil {
		wrappedGuard = wrapBodyWithPattern(expr.Bind, guard, loc)
	}

	source := collection
	if wrappedGuard != nil {
		filterFn := ast.ExprLambda{Params: elemParams, Body: wrappedGuard, Loc: loc}
		source = ast.ExprApply{
			Fn:   ast.ExprVar{Name: "__for_filter", Loc: loc},
			Args: []ast.Expr{source, filterFn},
			Loc:  loc,
		}
	}

	if expr.Fold != nil {
		init := Desugar(expr.Fold.Init)
		foldParams := make([]ast.Param, 0, 1+len(elemParams))
		foldParams = append(foldParams, ast.Param{Name: expr.Fold.Acc})
		foldParams = append(foldParams, elemParams...)
		foldFn := ast.ExprLambda{Params: foldParams, Body: wrappedBody, Loc: loc}
		return ast.ExprApply{
			Fn:   ast.ExprVar{Name: "__for_fold", Loc: loc},
			Args: []ast.Expr{source, init, foldFn},
			Loc:  loc,
		}
	}

	mapFn := ast.ExprLambda{Params: elemParams, Body: wrappedBody, Loc: loc}
	return ast.ExprApply{
		Fn:   ast.ExprVar{Name: "__for_each", Loc: loc},
		Args: []ast.Expr{source, mapFn},
		Loc:  loc,
	}
}
