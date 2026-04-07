// Package linter implements lint rules beyond type checking for Clank programs.
// Produces structured diagnostics (warnings) for code quality issues.
package linter

import (
	"fmt"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

// LintDiagnostic represents a lint warning.
type LintDiagnostic struct {
	Code     string    `json:"code"`
	Message  string    `json:"message"`
	Location token.Loc `json:"location"`
}

// LintRules maps rule codes to rule names.
var LintRules = map[string]string{
	"W100": "unused-variable",
	"W101": "unused-import",
	"W102": "shadowed-binding",
	"W103": "missing-pub-annotation",
	"W104": "unreachable-match-arm",
	"W105": "empty-effect-handler",
	"W106": "builtin-shadow",
}

// ruleNameToCode is a reverse mapping.
var ruleNameToCode = func() map[string]string {
	m := make(map[string]string)
	for code, name := range LintRules {
		m[name] = code
	}
	return m
}()

// BuiltinNames is the set of builtin function names (for W106).
// This matches the builtins registered in eval.Builtins().
var BuiltinNames = map[string]bool{
	"add": true, "sub": true, "mul": true, "div": true, "mod": true,
	"eq": true, "neq": true, "lt": true, "gt": true, "lte": true, "gte": true,
	"and": true, "or": true, "not": true, "negate": true,
	"str.cat": true, "show": true, "print": true,
	"len": true, "head": true, "tail": true, "cons": true, "cat": true, "rev": true,
	"get": true, "map": true, "filter": true, "fold": true, "flat-map": true,
	"tuple.get": true, "range": true, "zip": true, "fst": true, "snd": true,
	"split": true, "join": true, "trim": true,
	"__for_each": true, "__for_filter": true, "__for_fold": true,
}

// LintOptions controls which rules are enabled/disabled.
type LintOptions struct {
	EnabledRules  map[string]bool // if non-nil, only these rules run
	DisabledRules map[string]bool // if non-nil, skip these rules
}

func isRuleEnabled(code string, opts LintOptions) bool {
	name := LintRules[code]
	if opts.EnabledRules != nil {
		return opts.EnabledRules[code] || opts.EnabledRules[name]
	}
	if opts.DisabledRules != nil {
		return !opts.DisabledRules[code] && !opts.DisabledRules[name]
	}
	return true
}

// Lint runs all enabled lint rules on a program and returns diagnostics.
func Lint(program *ast.Program, opts LintOptions) []LintDiagnostic {
	var diags []LintDiagnostic

	// Build top-level scope
	topScope := make(map[string]bool)
	for _, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			topScope[d.Name] = true
		case ast.TopTypeDecl:
			for _, v := range d.Variants {
				topScope[v.Name] = true
			}
		case ast.TopEffectDecl:
			topScope[d.Name] = true
			for _, op := range d.Ops {
				topScope[op.Name] = true
			}
		case ast.TopUseDecl:
			for _, imp := range d.Imports {
				name := imp.Name
				if imp.Alias != "" {
					name = imp.Alias
				}
				topScope[name] = true
			}
		}
	}

	// Per-definition checks
	for _, tl := range program.TopLevels {
		def, ok := tl.(ast.TopDefinition)
		if !ok {
			continue
		}

		fnScope := copySet(topScope)
		for _, p := range def.Sig.Params {
			if p.Name != "_" {
				fnScope[p.Name] = true
			}
		}

		if isRuleEnabled("W100", opts) {
			checkUnusedVars(def.Body, &diags)
		}
		if isRuleEnabled("W102", opts) {
			checkShadowedBindings(def.Body, fnScope, &diags)
		}
		if isRuleEnabled("W104", opts) {
			checkUnreachableArms(def.Body, &diags)
		}
		if isRuleEnabled("W105", opts) {
			checkEmptyHandlers(def.Body, &diags)
		}
	}

	// Program-wide checks
	if isRuleEnabled("W101", opts) {
		checkUnusedImports(program, &diags)
	}
	if isRuleEnabled("W103", opts) {
		checkMissingPubAnnotations(program, &diags)
	}
	if isRuleEnabled("W106", opts) {
		checkBuiltinShadow(program, &diags)
	}

	return diags
}

func copySet(s map[string]bool) map[string]bool {
	c := make(map[string]bool, len(s))
	for k, v := range s {
		c[k] = v
	}
	return c
}

// ── collectRefs: collect all variable references in an expression ──

func collectRefs(expr ast.Expr, refs map[string]bool) {
	switch e := expr.(type) {
	case ast.ExprVar:
		refs[e.Name] = true
	case ast.ExprLiteral:
		// no refs
	case ast.ExprLet:
		collectRefs(e.Value, refs)
		if e.Body != nil {
			collectRefs(e.Body, refs)
		}
	case ast.ExprLetPattern:
		collectRefs(e.Value, refs)
		if e.Body != nil {
			collectRefs(e.Body, refs)
		}
	case ast.ExprIf:
		collectRefs(e.Cond, refs)
		collectRefs(e.Then, refs)
		collectRefs(e.Else, refs)
	case ast.ExprMatch:
		collectRefs(e.Subject, refs)
		for _, arm := range e.Arms {
			collectPatternRefs(arm.Pattern, refs)
			collectRefs(arm.Body, refs)
		}
	case ast.ExprLambda:
		collectRefs(e.Body, refs)
	case ast.ExprApply:
		collectRefs(e.Fn, refs)
		for _, a := range e.Args {
			collectRefs(a, refs)
		}
	case ast.ExprPipeline:
		collectRefs(e.Left, refs)
		collectRefs(e.Right, refs)
	case ast.ExprInfix:
		collectRefs(e.Left, refs)
		collectRefs(e.Right, refs)
	case ast.ExprUnary:
		collectRefs(e.Operand, refs)
	case ast.ExprBlock:
		for _, expr := range e.Exprs {
			collectRefs(expr, refs)
		}
	case ast.ExprFor:
		collectRefs(e.Collection, refs)
		if e.Guard != nil {
			collectRefs(e.Guard, refs)
		}
		if e.Fold != nil {
			collectRefs(e.Fold.Init, refs)
		}
		collectRefs(e.Body, refs)
	case ast.ExprRange:
		collectRefs(e.Start, refs)
		collectRefs(e.End, refs)
	case ast.ExprHandle:
		collectRefs(e.Expr, refs)
		for _, arm := range e.Arms {
			collectRefs(arm.Body, refs)
		}
	case ast.ExprPerform:
		collectRefs(e.Expr, refs)
	case ast.ExprList:
		for _, el := range e.Elements {
			collectRefs(el, refs)
		}
	case ast.ExprTuple:
		for _, el := range e.Elements {
			collectRefs(el, refs)
		}
	case ast.ExprRecord:
		for _, f := range e.Fields {
			collectRefs(f.Value, refs)
		}
	case ast.ExprRecordUpdate:
		collectRefs(e.Base, refs)
		for _, f := range e.Fields {
			collectRefs(f.Value, refs)
		}
	case ast.ExprFieldAccess:
		collectRefs(e.Object, refs)
	case ast.ExprBorrow:
		collectRefs(e.Expr, refs)
	case ast.ExprClone:
		collectRefs(e.Expr, refs)
	case ast.ExprDiscard:
		collectRefs(e.Expr, refs)
	}
}

func collectPatternRefs(pat ast.Pattern, refs map[string]bool) {
	switch p := pat.(type) {
	case ast.PatVariant:
		refs[p.Name] = true
		for _, a := range p.Args {
			collectPatternRefs(a, refs)
		}
	case ast.PatTuple:
		for _, el := range p.Elements {
			collectPatternRefs(el, refs)
		}
	}
}

// ── W100: unused variables ──

func checkUnusedVars(expr ast.Expr, diags *[]LintDiagnostic) {
	switch e := expr.(type) {
	case ast.ExprLet:
		checkUnusedVars(e.Value, diags)
		if e.Body != nil {
			checkUnusedVars(e.Body, diags)
			if e.Name != "_" {
				refs := make(map[string]bool)
				collectRefs(e.Body, refs)
				if !refs[e.Name] {
					*diags = append(*diags, LintDiagnostic{
						Code:     "W100",
						Message:  fmt.Sprintf("unused variable '%s'", e.Name),
						Location: e.Loc,
					})
				}
			}
		}
	case ast.ExprLetPattern:
		checkUnusedVars(e.Value, diags)
		if e.Body != nil {
			checkUnusedVars(e.Body, diags)
		}
	case ast.ExprIf:
		checkUnusedVars(e.Cond, diags)
		checkUnusedVars(e.Then, diags)
		checkUnusedVars(e.Else, diags)
	case ast.ExprMatch:
		checkUnusedVars(e.Subject, diags)
		for _, arm := range e.Arms {
			checkUnusedVarsInPattern(arm.Pattern, arm.Body, diags)
			checkUnusedVars(arm.Body, diags)
		}
	case ast.ExprLambda:
		for _, p := range e.Params {
			if p.Name != "_" {
				refs := make(map[string]bool)
				collectRefs(e.Body, refs)
				if !refs[p.Name] {
					*diags = append(*diags, LintDiagnostic{
						Code:     "W100",
						Message:  fmt.Sprintf("unused variable '%s'", p.Name),
						Location: e.Loc,
					})
				}
			}
		}
		checkUnusedVars(e.Body, diags)
	case ast.ExprApply:
		checkUnusedVars(e.Fn, diags)
		for _, a := range e.Args {
			checkUnusedVars(a, diags)
		}
	case ast.ExprPipeline:
		checkUnusedVars(e.Left, diags)
		checkUnusedVars(e.Right, diags)
	case ast.ExprInfix:
		checkUnusedVars(e.Left, diags)
		checkUnusedVars(e.Right, diags)
	case ast.ExprUnary:
		checkUnusedVars(e.Operand, diags)
	case ast.ExprBlock:
		for i, expr := range e.Exprs {
			checkUnusedVars(expr, diags)
			// Check for unused let bindings (body-less lets in block context)
			if letExpr, ok := expr.(ast.ExprLet); ok && letExpr.Body == nil && letExpr.Name != "_" {
				refs := make(map[string]bool)
				for j := i + 1; j < len(e.Exprs); j++ {
					collectRefs(e.Exprs[j], refs)
				}
				if !refs[letExpr.Name] {
					*diags = append(*diags, LintDiagnostic{
						Code:     "W100",
						Message:  fmt.Sprintf("unused variable '%s'", letExpr.Name),
						Location: letExpr.Loc,
					})
				}
			}
		}
	case ast.ExprFor:
		checkUnusedVars(e.Collection, diags)
		if e.Guard != nil {
			checkUnusedVars(e.Guard, diags)
		}
		if e.Fold != nil {
			checkUnusedVars(e.Fold.Init, diags)
		}
		checkUnusedVars(e.Body, diags)
	case ast.ExprRange:
		checkUnusedVars(e.Start, diags)
		checkUnusedVars(e.End, diags)
	case ast.ExprHandle:
		checkUnusedVars(e.Expr, diags)
		for _, arm := range e.Arms {
			checkUnusedVars(arm.Body, diags)
		}
	case ast.ExprPerform:
		checkUnusedVars(e.Expr, diags)
	case ast.ExprList:
		for _, el := range e.Elements {
			checkUnusedVars(el, diags)
		}
	case ast.ExprTuple:
		for _, el := range e.Elements {
			checkUnusedVars(el, diags)
		}
	case ast.ExprRecord:
		for _, f := range e.Fields {
			checkUnusedVars(f.Value, diags)
		}
	case ast.ExprRecordUpdate:
		checkUnusedVars(e.Base, diags)
		for _, f := range e.Fields {
			checkUnusedVars(f.Value, diags)
		}
	case ast.ExprFieldAccess:
		checkUnusedVars(e.Object, diags)
	}
}

func checkUnusedVarsInPattern(pat ast.Pattern, body ast.Expr, diags *[]LintDiagnostic) {
	refs := make(map[string]bool)
	collectRefs(body, refs)

	var walkPat func(p ast.Pattern)
	walkPat = func(p ast.Pattern) {
		switch pp := p.(type) {
		case ast.PatVar:
			if pp.Name != "_" && !refs[pp.Name] {
				*diags = append(*diags, LintDiagnostic{
					Code:     "W100",
					Message:  fmt.Sprintf("unused variable '%s'", pp.Name),
					Location: pp.Loc,
				})
			}
		case ast.PatVariant:
			for _, a := range pp.Args {
				walkPat(a)
			}
		case ast.PatTuple:
			for _, el := range pp.Elements {
				walkPat(el)
			}
		}
	}
	walkPat(pat)
}

// ── W101: unused imports ──

func checkUnusedImports(program *ast.Program, diags *[]LintDiagnostic) {
	allRefs := make(map[string]bool)
	for _, tl := range program.TopLevels {
		if def, ok := tl.(ast.TopDefinition); ok {
			collectRefs(def.Body, allRefs)
		}
	}

	// Collect type refs from signatures
	for _, tl := range program.TopLevels {
		if def, ok := tl.(ast.TopDefinition); ok {
			for _, p := range def.Sig.Params {
				collectTypeRefs(p.Type, allRefs)
			}
			collectTypeRefs(def.Sig.ReturnType, allRefs)
		}
	}

	for _, tl := range program.TopLevels {
		ud, ok := tl.(ast.TopUseDecl)
		if !ok {
			continue
		}
		for _, imp := range ud.Imports {
			usedName := imp.Name
			if imp.Alias != "" {
				usedName = imp.Alias
			}
			if !allRefs[usedName] {
				*diags = append(*diags, LintDiagnostic{
					Code:     "W101",
					Message:  fmt.Sprintf("unused import '%s'", usedName),
					Location: ud.Loc,
				})
			}
		}
	}
}

func collectTypeRefs(te ast.TypeExpr, refs map[string]bool) {
	if te == nil {
		return
	}
	switch t := te.(type) {
	case ast.TypeName:
		refs[t.Name] = true
	case ast.TypeList:
		collectTypeRefs(t.Element, refs)
	case ast.TypeTuple:
		for _, el := range t.Elements {
			collectTypeRefs(el, refs)
		}
	case ast.TypeFn:
		collectTypeRefs(t.Param, refs)
		collectTypeRefs(t.Result, refs)
	case ast.TypeGeneric:
		refs[t.Name] = true
		for _, a := range t.Args {
			collectTypeRefs(a, refs)
		}
	case ast.TypeRecord:
		for _, f := range t.Fields {
			collectTypeRefs(f.Type, refs)
		}
	case ast.TypeUnion:
		collectTypeRefs(t.Left, refs)
		collectTypeRefs(t.Right, refs)
	case ast.TypeRefined:
		collectTypeRefs(t.Base, refs)
	case ast.TypeBorrow:
		collectTypeRefs(t.Inner, refs)
	}
}

// ── W102: shadowed bindings ──

func checkShadowedBindings(expr ast.Expr, scope map[string]bool, diags *[]LintDiagnostic) {
	switch e := expr.(type) {
	case ast.ExprLet:
		checkShadowedBindings(e.Value, scope, diags)
		if e.Name != "_" && scope[e.Name] {
			*diags = append(*diags, LintDiagnostic{
				Code:     "W102",
				Message:  fmt.Sprintf("'%s' shadows an existing binding", e.Name),
				Location: e.Loc,
			})
		}
		if e.Body != nil {
			inner := copySet(scope)
			if e.Name != "_" {
				inner[e.Name] = true
			}
			checkShadowedBindings(e.Body, inner, diags)
		}
	case ast.ExprLetPattern:
		checkShadowedBindings(e.Value, scope, diags)
		if e.Body != nil {
			checkShadowedBindings(e.Body, scope, diags)
		}
	case ast.ExprIf:
		checkShadowedBindings(e.Cond, scope, diags)
		checkShadowedBindings(e.Then, scope, diags)
		checkShadowedBindings(e.Else, scope, diags)
	case ast.ExprMatch:
		checkShadowedBindings(e.Subject, scope, diags)
		for _, arm := range e.Arms {
			armScope := copySet(scope)
			addPatternBindings(arm.Pattern, armScope)
			checkShadowedBindings(arm.Body, armScope, diags)
		}
	case ast.ExprLambda:
		lamScope := copySet(scope)
		for _, p := range e.Params {
			if p.Name != "_" && scope[p.Name] {
				*diags = append(*diags, LintDiagnostic{
					Code:     "W102",
					Message:  fmt.Sprintf("'%s' shadows an existing binding", p.Name),
					Location: e.Loc,
				})
			}
			if p.Name != "_" {
				lamScope[p.Name] = true
			}
		}
		checkShadowedBindings(e.Body, lamScope, diags)
	case ast.ExprApply:
		checkShadowedBindings(e.Fn, scope, diags)
		for _, a := range e.Args {
			checkShadowedBindings(a, scope, diags)
		}
	case ast.ExprPipeline:
		checkShadowedBindings(e.Left, scope, diags)
		checkShadowedBindings(e.Right, scope, diags)
	case ast.ExprInfix:
		checkShadowedBindings(e.Left, scope, diags)
		checkShadowedBindings(e.Right, scope, diags)
	case ast.ExprUnary:
		checkShadowedBindings(e.Operand, scope, diags)
	case ast.ExprBlock:
		blockScope := copySet(scope)
		for _, expr := range e.Exprs {
			checkShadowedBindings(expr, blockScope, diags)
		}
	case ast.ExprFor:
		checkShadowedBindings(e.Collection, scope, diags)
		if e.Guard != nil {
			checkShadowedBindings(e.Guard, scope, diags)
		}
		if e.Fold != nil {
			checkShadowedBindings(e.Fold.Init, scope, diags)
		}
		checkShadowedBindings(e.Body, scope, diags)
	case ast.ExprRange:
		checkShadowedBindings(e.Start, scope, diags)
		checkShadowedBindings(e.End, scope, diags)
	case ast.ExprHandle:
		checkShadowedBindings(e.Expr, scope, diags)
		for _, arm := range e.Arms {
			armScope := copySet(scope)
			for _, p := range arm.Params {
				if p.Name != "_" {
					armScope[p.Name] = true
				}
			}
			if arm.ResumeName != "" {
				armScope[arm.ResumeName] = true
			}
			checkShadowedBindings(arm.Body, armScope, diags)
		}
	case ast.ExprPerform:
		checkShadowedBindings(e.Expr, scope, diags)
	case ast.ExprList:
		for _, el := range e.Elements {
			checkShadowedBindings(el, scope, diags)
		}
	case ast.ExprTuple:
		for _, el := range e.Elements {
			checkShadowedBindings(el, scope, diags)
		}
	case ast.ExprRecord:
		for _, f := range e.Fields {
			checkShadowedBindings(f.Value, scope, diags)
		}
	case ast.ExprRecordUpdate:
		checkShadowedBindings(e.Base, scope, diags)
		for _, f := range e.Fields {
			checkShadowedBindings(f.Value, scope, diags)
		}
	case ast.ExprFieldAccess:
		checkShadowedBindings(e.Object, scope, diags)
	}
}

func addPatternBindings(pat ast.Pattern, scope map[string]bool) {
	switch p := pat.(type) {
	case ast.PatVar:
		if p.Name != "_" {
			scope[p.Name] = true
		}
	case ast.PatVariant:
		for _, a := range p.Args {
			addPatternBindings(a, scope)
		}
	case ast.PatTuple:
		for _, el := range p.Elements {
			addPatternBindings(el, scope)
		}
	}
}

// ── W103: missing type annotations on pub functions ──

func checkMissingPubAnnotations(program *ast.Program, diags *[]LintDiagnostic) {
	for _, tl := range program.TopLevels {
		def, ok := tl.(ast.TopDefinition)
		if !ok || !def.Pub {
			continue
		}
		for _, p := range def.Sig.Params {
			if tn, ok := p.Type.(ast.TypeName); ok && tn.Name == "_" {
				*diags = append(*diags, LintDiagnostic{
					Code:     "W103",
					Message:  fmt.Sprintf("pub function '%s' has untyped parameter '%s'", def.Name, p.Name),
					Location: def.Loc,
				})
			}
		}
		if tn, ok := def.Sig.ReturnType.(ast.TypeName); ok && tn.Name == "_" {
			*diags = append(*diags, LintDiagnostic{
				Code:     "W103",
				Message:  fmt.Sprintf("pub function '%s' is missing return type annotation", def.Name),
				Location: def.Loc,
			})
		}
	}
}

// ── W104: unreachable match arms ──

func checkUnreachableArms(expr ast.Expr, diags *[]LintDiagnostic) {
	switch e := expr.(type) {
	case ast.ExprMatch:
		checkUnreachableArms(e.Subject, diags)
		catchAllSeen := false
		for _, arm := range e.Arms {
			if catchAllSeen {
				*diags = append(*diags, LintDiagnostic{
					Code:     "W104",
					Message:  "unreachable match arm after catch-all pattern",
					Location: arm.Pattern.PatLoc(),
				})
			}
			switch arm.Pattern.(type) {
			case ast.PatWildcard, ast.PatVar:
				catchAllSeen = true
			}
			checkUnreachableArms(arm.Body, diags)
		}
	case ast.ExprLet:
		checkUnreachableArms(e.Value, diags)
		if e.Body != nil {
			checkUnreachableArms(e.Body, diags)
		}
	case ast.ExprLetPattern:
		checkUnreachableArms(e.Value, diags)
		if e.Body != nil {
			checkUnreachableArms(e.Body, diags)
		}
	case ast.ExprIf:
		checkUnreachableArms(e.Cond, diags)
		checkUnreachableArms(e.Then, diags)
		checkUnreachableArms(e.Else, diags)
	case ast.ExprLambda:
		checkUnreachableArms(e.Body, diags)
	case ast.ExprApply:
		checkUnreachableArms(e.Fn, diags)
		for _, a := range e.Args {
			checkUnreachableArms(a, diags)
		}
	case ast.ExprPipeline:
		checkUnreachableArms(e.Left, diags)
		checkUnreachableArms(e.Right, diags)
	case ast.ExprInfix:
		checkUnreachableArms(e.Left, diags)
		checkUnreachableArms(e.Right, diags)
	case ast.ExprUnary:
		checkUnreachableArms(e.Operand, diags)
	case ast.ExprBlock:
		for _, expr := range e.Exprs {
			checkUnreachableArms(expr, diags)
		}
	case ast.ExprFor:
		checkUnreachableArms(e.Collection, diags)
		if e.Guard != nil {
			checkUnreachableArms(e.Guard, diags)
		}
		if e.Fold != nil {
			checkUnreachableArms(e.Fold.Init, diags)
		}
		checkUnreachableArms(e.Body, diags)
	case ast.ExprRange:
		checkUnreachableArms(e.Start, diags)
		checkUnreachableArms(e.End, diags)
	case ast.ExprHandle:
		checkUnreachableArms(e.Expr, diags)
		for _, arm := range e.Arms {
			checkUnreachableArms(arm.Body, diags)
		}
	case ast.ExprPerform:
		checkUnreachableArms(e.Expr, diags)
	case ast.ExprList:
		for _, el := range e.Elements {
			checkUnreachableArms(el, diags)
		}
	case ast.ExprTuple:
		for _, el := range e.Elements {
			checkUnreachableArms(el, diags)
		}
	case ast.ExprRecord:
		for _, f := range e.Fields {
			checkUnreachableArms(f.Value, diags)
		}
	case ast.ExprRecordUpdate:
		checkUnreachableArms(e.Base, diags)
		for _, f := range e.Fields {
			checkUnreachableArms(f.Value, diags)
		}
	case ast.ExprFieldAccess:
		checkUnreachableArms(e.Object, diags)
	}
}

// ── W105: empty effect handlers ──

func checkEmptyHandlers(expr ast.Expr, diags *[]LintDiagnostic) {
	switch e := expr.(type) {
	case ast.ExprHandle:
		opArms := 0
		for _, arm := range e.Arms {
			if arm.Name != "return" {
				opArms++
			}
		}
		if opArms == 0 {
			*diags = append(*diags, LintDiagnostic{
				Code:     "W105",
				Message:  "effect handler has no operation arms",
				Location: e.Loc,
			})
		}
		checkEmptyHandlers(e.Expr, diags)
		for _, arm := range e.Arms {
			checkEmptyHandlers(arm.Body, diags)
		}
	case ast.ExprLet:
		checkEmptyHandlers(e.Value, diags)
		if e.Body != nil {
			checkEmptyHandlers(e.Body, diags)
		}
	case ast.ExprLetPattern:
		checkEmptyHandlers(e.Value, diags)
		if e.Body != nil {
			checkEmptyHandlers(e.Body, diags)
		}
	case ast.ExprIf:
		checkEmptyHandlers(e.Cond, diags)
		checkEmptyHandlers(e.Then, diags)
		checkEmptyHandlers(e.Else, diags)
	case ast.ExprMatch:
		checkEmptyHandlers(e.Subject, diags)
		for _, arm := range e.Arms {
			checkEmptyHandlers(arm.Body, diags)
		}
	case ast.ExprLambda:
		checkEmptyHandlers(e.Body, diags)
	case ast.ExprApply:
		checkEmptyHandlers(e.Fn, diags)
		for _, a := range e.Args {
			checkEmptyHandlers(a, diags)
		}
	case ast.ExprPipeline:
		checkEmptyHandlers(e.Left, diags)
		checkEmptyHandlers(e.Right, diags)
	case ast.ExprInfix:
		checkEmptyHandlers(e.Left, diags)
		checkEmptyHandlers(e.Right, diags)
	case ast.ExprUnary:
		checkEmptyHandlers(e.Operand, diags)
	case ast.ExprBlock:
		for _, expr := range e.Exprs {
			checkEmptyHandlers(expr, diags)
		}
	case ast.ExprFor:
		checkEmptyHandlers(e.Collection, diags)
		if e.Guard != nil {
			checkEmptyHandlers(e.Guard, diags)
		}
		if e.Fold != nil {
			checkEmptyHandlers(e.Fold.Init, diags)
		}
		checkEmptyHandlers(e.Body, diags)
	case ast.ExprRange:
		checkEmptyHandlers(e.Start, diags)
		checkEmptyHandlers(e.End, diags)
	case ast.ExprPerform:
		checkEmptyHandlers(e.Expr, diags)
	case ast.ExprList:
		for _, el := range e.Elements {
			checkEmptyHandlers(el, diags)
		}
	case ast.ExprTuple:
		for _, el := range e.Elements {
			checkEmptyHandlers(el, diags)
		}
	case ast.ExprRecord:
		for _, f := range e.Fields {
			checkEmptyHandlers(f.Value, diags)
		}
	case ast.ExprRecordUpdate:
		checkEmptyHandlers(e.Base, diags)
		for _, f := range e.Fields {
			checkEmptyHandlers(f.Value, diags)
		}
	case ast.ExprFieldAccess:
		checkEmptyHandlers(e.Object, diags)
	}
}

// ── W106: builtin name shadowing ──

func checkBuiltinShadow(program *ast.Program, diags *[]LintDiagnostic) {
	for _, tl := range program.TopLevels {
		def, ok := tl.(ast.TopDefinition)
		if !ok {
			continue
		}
		if BuiltinNames[def.Name] {
			*diags = append(*diags, LintDiagnostic{
				Code:    "W106",
				Message: fmt.Sprintf("function '%s' shadows builtin '%s' — this may cause infinite recursion when the corresponding operator dispatches to '%s'", def.Name, def.Name, def.Name),
				Location: def.Loc,
			})
		}
	}
}
