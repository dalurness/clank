package checker

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

// TypeError is a structured type error.
type TypeError struct {
	Code     string
	Message  string
	Location token.Loc
}

func (e TypeError) Error() string {
	return fmt.Sprintf("[%s] %s (at %d:%d)", e.Code, e.Message, e.Location.Line, e.Location.Col)
}

// ── Checker state ──

type checkerState struct {
	// Refinement tracking
	fnRefInfo       map[string]*fnRefInfo
	varRefinements  map[string]string
	pathConditions  []string
	fnConstraints   map[string]*fnConstraintInfo
	fnParamTypeExps map[string][]ast.TypeExpr

	// Row/type variable substitution
	rowSubst      map[int]*rowSubstEntry
	namedRowVars  map[string]int
	typeSubst     map[int]Type
	knownTypes    map[string]bool
	fnTypeParamVs map[string]map[string]Type

	// Registries
	implRegistry map[string][]implEntry
}

type fnRefInfo struct {
	paramPreds []string // "" means no predicate
	returnPred string   // "" means no predicate
}

type fnConstraintInfo struct {
	constraints   []ast.Constraint
	paramTypeExps []ast.TypeExpr
}

type rowSubstEntry struct {
	fields []RecordField
	tail   int // -1 if none
}

type implEntry struct {
	interface_ string
	forType    string
	typeArgs   []string
	loc        token.Loc
}

// ── Integer literal extraction ──

func intLiteralValue(expr ast.Expr) (int64, bool) {
	switch e := expr.(type) {
	case ast.ExprLiteral:
		if lit, ok := e.Value.(ast.LitInt); ok {
			return lit.Value, true
		}
	case ast.ExprApply:
		if v, ok := e.Fn.(ast.ExprVar); ok && v.Name == "negate" && len(e.Args) == 1 {
			if inner, ok := intLiteralValue(e.Args[0]); ok {
				return -inner, true
			}
		}
	case ast.ExprUnary:
		if e.Op == "-" {
			if inner, ok := intLiteralValue(e.Operand); ok {
				return -inner, true
			}
		}
	}
	return 0, false
}

// ── Refinement context helpers ──

type refinementState struct {
	vars  map[string]string
	paths []string
}

func (s *checkerState) saveRefinementState() refinementState {
	vars := make(map[string]string, len(s.varRefinements))
	for k, v := range s.varRefinements {
		vars[k] = v
	}
	paths := make([]string, len(s.pathConditions))
	copy(paths, s.pathConditions)
	return refinementState{vars: vars, paths: paths}
}

func (s *checkerState) restoreRefinementState(st refinementState) {
	s.varRefinements = make(map[string]string, len(st.vars))
	for k, v := range st.vars {
		s.varRefinements[k] = v
	}
	s.pathConditions = make([]string, len(st.paths))
	copy(s.pathConditions, st.paths)
}

func (s *checkerState) collectAssumptions(targetVar string) []string {
	assumptions := make([]string, len(s.pathConditions))
	copy(assumptions, s.pathConditions)
	for name, pred := range s.varRefinements {
		if name == targetVar {
			continue
		}
		assumptions = append(assumptions, SubstitutePredVar(pred, "v", name))
	}
	return assumptions
}

func extractConditionPredicate(expr ast.Expr) (pos, neg string, ok bool) {
	app, isApp := expr.(ast.ExprApply)
	if !isApp {
		return "", "", false
	}
	v, isVar := app.Fn.(ast.ExprVar)
	if !isVar || len(app.Args) != 2 {
		return "", "", false
	}
	type opEntry struct{ sym, negSym string }
	cmpOps := map[string]opEntry{
		"gt": {">", "<="}, "lt": {"<", ">="}, "gte": {">=", "<"},
		"lte": {"<=", ">"}, "eq": {"==", "!="}, "neq": {"!=", "=="},
	}
	entry, found := cmpOps[v.Name]
	if !found {
		return "", "", false
	}
	leftStr, lok := exprToPredString(app.Args[0])
	rightStr, rok := exprToPredString(app.Args[1])
	if !lok || !rok {
		return "", "", false
	}
	return fmt.Sprintf("%s %s %s", leftStr, entry.sym, rightStr),
		fmt.Sprintf("%s %s %s", leftStr, entry.negSym, rightStr), true
}

func exprToPredString(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case ast.ExprVar:
		return e.Name, true
	case ast.ExprLiteral:
		if lit, ok := e.Value.(ast.LitInt); ok {
			return fmt.Sprintf("%d", lit.Value), true
		}
		return "", false
	case ast.ExprApply:
		v, isVar := e.Fn.(ast.ExprVar)
		if !isVar {
			return "", false
		}
		if v.Name == "negate" && len(e.Args) == 1 {
			inner, ok := exprToPredString(e.Args[0])
			if ok {
				return fmt.Sprintf("(-%s)", inner), true
			}
			return "", false
		}
		if len(e.Args) == 2 {
			arithOps := map[string]string{"add": "+", "sub": "-", "mul": "*"}
			sym, found := arithOps[v.Name]
			if !found {
				return "", false
			}
			left, lok := exprToPredString(e.Args[0])
			right, rok := exprToPredString(e.Args[1])
			if lok && rok {
				return fmt.Sprintf("(%s %s %s)", left, sym, right), true
			}
		}
		return "", false
	}
	return "", false
}

func patternToPredicate(pat ast.Pattern, subjectStr string) (string, bool) {
	if pl, ok := pat.(ast.PatLiteral); ok {
		if lit, ok := pl.Value.(ast.LitInt); ok {
			return fmt.Sprintf("%s == %d", subjectStr, lit.Value), true
		}
	}
	return "", false
}

func (s *checkerState) checkRefinementObligation(
	argExpr ast.Expr, reqPred, errorCode, errorPrefix string, errors *[]TypeError,
) bool {
	// Case 1: integer literal
	if litVal, ok := intLiteralValue(argExpr); ok {
		if !CheckLiteral(litVal, reqPred) {
			*errors = append(*errors, TypeError{
				Code:     errorCode,
				Message:  fmt.Sprintf("%s: %d does not satisfy {%s}", errorPrefix, litVal, reqPred),
				Location: exprLoc(argExpr),
			})
		}
		return true
	}
	// Case 2: variable with known refinement
	if v, ok := argExpr.(ast.ExprVar); ok {
		knownPred, has := s.varRefinements[v.Name]
		if has {
			assumptions := s.collectAssumptions("")
			assumptions = append(assumptions, SubstitutePredVar(knownPred, "v", v.Name))
			goal := SubstitutePredVar(reqPred, "v", v.Name)
			result := CheckRefinementWithContext(assumptions, goal)
			if result == "invalid" {
				*errors = append(*errors, TypeError{
					Code:     errorCode,
					Message:  fmt.Sprintf("%s: {%s} does not imply {%s}", errorPrefix, knownPred, reqPred),
					Location: exprLoc(argExpr),
				})
			}
			return true
		}
		// Variable without explicit refinement — check path conditions
		if len(s.pathConditions) > 0 {
			assumptions := s.collectAssumptions("")
			goal := SubstitutePredVar(reqPred, "v", v.Name)
			result := CheckRefinementWithContext(assumptions, goal)
			if result == "invalid" {
				*errors = append(*errors, TypeError{
					Code:     errorCode,
					Message:  fmt.Sprintf("%s: path conditions do not imply {%s}", errorPrefix, reqPred),
					Location: exprLoc(argExpr),
				})
			}
			return result != "unknown"
		}
		// No refinement and no path conditions — unrefined variable cannot satisfy refinement
		*errors = append(*errors, TypeError{
			Code:     errorCode,
			Message:  fmt.Sprintf("%s: variable '%s' has no refinement to satisfy {%s}", errorPrefix, v.Name, reqPred),
			Location: exprLoc(argExpr),
		})
		return true
	}
	// Case 3: arithmetic expression
	if exprStr, ok := exprToPredString(argExpr); ok {
		assumptions := s.collectAssumptions("")
		goal := SubstitutePredVar(reqPred, "v", exprStr)
		result := CheckRefinementWithContext(assumptions, goal)
		if result == "invalid" {
			*errors = append(*errors, TypeError{
				Code:     errorCode,
				Message:  fmt.Sprintf("%s: expression does not satisfy {%s}", errorPrefix, reqPred),
				Location: exprLoc(argExpr),
			})
		}
		return result != "unknown"
	}
	return false
}

func (s *checkerState) checkReturnRefinementPerBranch(
	body ast.Expr, retPred string, errors *[]TypeError, params []ast.TypeSigParam,
) {
	saved := s.saveRefinementState()
	s.varRefinements = make(map[string]string)
	s.pathConditions = nil
	for _, p := range params {
		if tr, ok := p.Type.(ast.TypeRefined); ok {
			s.varRefinements[p.Name] = tr.Predicate
		}
	}
	s.checkReturnBranches(body, retPred, errors)
	s.restoreRefinementState(saved)
}

func (s *checkerState) checkReturnBranches(body ast.Expr, retPred string, errors *[]TypeError) {
	switch b := body.(type) {
	case ast.ExprIf:
		pos, neg, ok := extractConditionPredicate(b.Cond)
		preRef := s.saveRefinementState()
		if ok {
			s.pathConditions = append(s.pathConditions, pos)
		}
		s.checkReturnBranches(b.Then, retPred, errors)
		s.restoreRefinementState(preRef)
		if ok {
			s.pathConditions = append(s.pathConditions, neg)
		}
		s.checkReturnBranches(b.Else, retPred, errors)
		s.restoreRefinementState(preRef)
		return
	case ast.ExprMatch:
		subjectStr, sok := exprToPredString(b.Subject)
		preRef := s.saveRefinementState()
		for _, arm := range b.Arms {
			s.restoreRefinementState(preRef)
			if sok {
				if patPred, ok := patternToPredicate(arm.Pattern, subjectStr); ok {
					s.pathConditions = append(s.pathConditions, patPred)
				}
			}
			s.checkReturnBranches(arm.Body, retPred, errors)
		}
		s.restoreRefinementState(preRef)
		return
	}
	s.checkRefinementObligation(body, retPred, "E311", "return refinement not satisfied", errors)
}

// ── Type substitution ──

func (s *checkerState) applyTypeSubst(t Type) Type {
	switch t := t.(type) {
	case TVar:
		if sub, ok := s.typeSubst[t.ID]; ok {
			return s.applyTypeSubst(sub)
		}
		return t
	case TFn:
		return TFn{Param: s.applyTypeSubst(t.Param), Result: s.applyTypeSubst(t.Result), Effects: t.Effects}
	case TList:
		return TList{Element: s.applyTypeSubst(t.Element)}
	case TTuple:
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = s.applyTypeSubst(e)
		}
		return TTuple{Elements: elems}
	case TRecord:
		fields := make([]RecordField, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = RecordField{Name: f.Name, Tags: f.Tags, Type: s.applyTypeSubst(f.Type)}
		}
		return TRecord{Fields: fields, RowVar: t.RowVar}
	case TGeneric:
		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = s.applyTypeSubst(a)
		}
		return TGeneric{Name: t.Name, Args: args}
	}
	return t
}

func (s *checkerState) typeVarOccursIn(varID int, t Type) bool {
	resolved := s.applyTypeSubst(t)
	switch r := resolved.(type) {
	case TVar:
		return r.ID == varID
	case TFn:
		return s.typeVarOccursIn(varID, r.Param) || s.typeVarOccursIn(varID, r.Result)
	case TList:
		return s.typeVarOccursIn(varID, r.Element)
	case TTuple:
		for _, e := range r.Elements {
			if s.typeVarOccursIn(varID, e) {
				return true
			}
		}
	case TRecord:
		for _, f := range r.Fields {
			if s.typeVarOccursIn(varID, f.Type) {
				return true
			}
		}
	case TGeneric:
		for _, a := range r.Args {
			if s.typeVarOccursIn(varID, a) {
				return true
			}
		}
	}
	return false
}

func (s *checkerState) unifyTypes(a, b Type) bool {
	ra := s.applyTypeSubst(a)
	rb := s.applyTypeSubst(b)

	if typeSame(ra, rb) {
		return true
	}

	if va, ok := ra.(TVar); ok {
		if vb, ok := rb.(TVar); ok && va.ID == vb.ID {
			return true
		}
		if s.typeVarOccursIn(va.ID, rb) {
			return false
		}
		s.typeSubst[va.ID] = rb
		return true
	}
	if vb, ok := rb.(TVar); ok {
		if s.typeVarOccursIn(vb.ID, ra) {
			return false
		}
		s.typeSubst[vb.ID] = ra
		return true
	}

	// Generic "?" is permissive
	if isAnyType(ra) || isAnyType(rb) {
		return true
	}

	if ga, ok := ra.(TGeneric); ok {
		if gb, ok := rb.(TGeneric); ok {
			if ga.Name != gb.Name || len(ga.Args) != len(gb.Args) {
				return false
			}
			for i := range ga.Args {
				if !s.unifyTypes(ga.Args[i], gb.Args[i]) {
					return false
				}
			}
			return true
		}
	}
	if _, ok := ra.(TGeneric); ok {
		return true
	}
	if _, ok := rb.(TGeneric); ok {
		return true
	}

	// Same tag — structural
	switch ra := ra.(type) {
	case TPrimitive:
		if rb, ok := rb.(TPrimitive); ok {
			return ra.Name == rb.Name
		}
		return false
	case TFn:
		if rb, ok := rb.(TFn); ok {
			return s.unifyTypes(ra.Param, rb.Param) && s.unifyTypes(ra.Result, rb.Result)
		}
		return false
	case TList:
		if rb, ok := rb.(TList); ok {
			return s.unifyTypes(ra.Element, rb.Element)
		}
		return false
	case TTuple:
		if rb, ok := rb.(TTuple); ok {
			if len(ra.Elements) != len(rb.Elements) {
				return false
			}
			for i := range ra.Elements {
				if !s.unifyTypes(ra.Elements[i], rb.Elements[i]) {
					return false
				}
			}
			return true
		}
		return false
	}
	return true
}

func (s *checkerState) freshTypeVar() Type {
	return TVar{ID: FreshVar()}
}

// ── HM generalization / instantiation ──

func (s *checkerState) freeTypeVarsInType(t Type) map[int]bool {
	result := make(map[int]bool)
	s.walkFreeVars(t, result)
	return result
}

func (s *checkerState) walkFreeVars(t Type, result map[int]bool) {
	resolved := s.applyTypeSubst(t)
	switch r := resolved.(type) {
	case TVar:
		result[r.ID] = true
	case TFn:
		s.walkFreeVars(r.Param, result)
		s.walkFreeVars(r.Result, result)
	case TList:
		s.walkFreeVars(r.Element, result)
	case TTuple:
		for _, e := range r.Elements {
			s.walkFreeVars(e, result)
		}
	case TRecord:
		for _, f := range r.Fields {
			s.walkFreeVars(f.Type, result)
		}
	case TGeneric:
		for _, a := range r.Args {
			s.walkFreeVars(a, result)
		}
	}
}

func (s *checkerState) generalizeType(envFreeVars map[int]bool, t Type) TypeScheme {
	resolved := s.applyTypeSubst(t)
	varsInType := s.freeTypeVarsInType(resolved)
	var quantified []int
	for v := range varsInType {
		if !envFreeVars[v] {
			quantified = append(quantified, v)
		}
	}
	return TypeScheme{TypeVars: quantified, Body: resolved}
}

func (s *checkerState) instantiateScheme(scheme TypeScheme) Type {
	if len(scheme.TypeVars) == 0 {
		return scheme.Body
	}
	mapping := make(map[int]Type, len(scheme.TypeVars))
	for _, v := range scheme.TypeVars {
		mapping[v] = s.freshTypeVar()
	}
	return s.instantiateBody(scheme.Body, mapping)
}

func (s *checkerState) instantiateBody(t Type, mapping map[int]Type) Type {
	switch t := t.(type) {
	case TVar:
		if m, ok := mapping[t.ID]; ok {
			return m
		}
		return t
	case TFn:
		return TFn{
			Param:   s.instantiateBody(t.Param, mapping),
			Result:  s.instantiateBody(t.Result, mapping),
			Effects: t.Effects,
		}
	case TList:
		return TList{Element: s.instantiateBody(t.Element, mapping)}
	case TTuple:
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = s.instantiateBody(e, mapping)
		}
		return TTuple{Elements: elems}
	case TRecord:
		fields := make([]RecordField, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = RecordField{Name: f.Name, Tags: f.Tags, Type: s.instantiateBody(f.Type, mapping)}
		}
		return TRecord{Fields: fields, RowVar: t.RowVar}
	case TGeneric:
		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = s.instantiateBody(a, mapping)
		}
		return TGeneric{Name: t.Name, Args: args}
	}
	return t
}

// ── Row substitution ──

func (s *checkerState) applyRowSubst(t Type) Type {
	switch t := t.(type) {
	case TRecord:
		resolvedFields := make([]RecordField, len(t.Fields))
		for i, f := range t.Fields {
			resolvedFields[i] = RecordField{Name: f.Name, Tags: f.Tags, Type: s.applyRowSubst(f.Type)}
		}
		if t.RowVar >= 0 {
			allFields := append([]RecordField{}, resolvedFields...)
			tail := t.RowVar
			for tail >= 0 {
				sub, ok := s.rowSubst[tail]
				if !ok {
					break
				}
				for _, f := range sub.fields {
					tags := f.Tags
					if tags == nil {
						tags = []string{}
					}
					allFields = append(allFields, RecordField{Name: f.Name, Tags: tags, Type: s.applyRowSubst(f.Type)})
				}
				tail = sub.tail
			}
			sort.Slice(allFields, func(i, j int) bool {
				return allFields[i].Name < allFields[j].Name
			})
			return TRecord{Fields: allFields, RowVar: tail}
		}
		return TRecord{Fields: resolvedFields, RowVar: -1}
	case TFn:
		return TFn{Param: s.applyRowSubst(t.Param), Result: s.applyRowSubst(t.Result), Effects: t.Effects}
	case TList:
		return TList{Element: s.applyRowSubst(t.Element)}
	case TTuple:
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = s.applyRowSubst(e)
		}
		return TTuple{Elements: elems}
	case TBorrow:
		return TBorrow{Inner: s.applyRowSubst(t.Inner)}
	}
	return t
}

func rowOccursIn(rowVarID int, t Type) bool {
	switch t := t.(type) {
	case TRecord:
		if t.RowVar == rowVarID {
			return true
		}
		for _, f := range t.Fields {
			if rowOccursIn(rowVarID, f.Type) {
				return true
			}
		}
	case TFn:
		return rowOccursIn(rowVarID, t.Param) || rowOccursIn(rowVarID, t.Result)
	case TList:
		return rowOccursIn(rowVarID, t.Element)
	case TTuple:
		for _, e := range t.Elements {
			if rowOccursIn(rowVarID, e) {
				return true
			}
		}
	case TBorrow:
		return rowOccursIn(rowVarID, t.Inner)
	}
	return false
}

func (s *checkerState) freshenRowVars(t Type, mapping map[int]int) Type {
	if mapping == nil {
		mapping = make(map[int]int)
	}
	freshenID := func(id int) int {
		if _, ok := mapping[id]; !ok {
			mapping[id] = FreshVar()
		}
		return mapping[id]
	}
	switch t := t.(type) {
	case TRecord:
		fields := make([]RecordField, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = RecordField{Name: f.Name, Tags: f.Tags, Type: s.freshenRowVars(f.Type, mapping)}
		}
		rv := t.RowVar
		if rv >= 0 {
			rv = freshenID(rv)
		}
		return TRecord{Fields: fields, RowVar: rv}
	case TFn:
		return TFn{Param: s.freshenRowVars(t.Param, mapping), Result: s.freshenRowVars(t.Result, mapping), Effects: t.Effects}
	case TList:
		return TList{Element: s.freshenRowVars(t.Element, mapping)}
	case TTuple:
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = s.freshenRowVars(e, mapping)
		}
		return TTuple{Elements: elems}
	case TBorrow:
		return TBorrow{Inner: s.freshenRowVars(t.Inner, mapping)}
	}
	return t
}

func (s *checkerState) unifyRecords(
	a, b TRecord, errors *[]TypeError, loc token.Loc,
) bool {
	aRes := s.applyRowSubst(a).(TRecord)
	bRes := s.applyRowSubst(b).(TRecord)

	aFields := make(map[string]Type, len(aRes.Fields))
	for _, f := range aRes.Fields {
		aFields[f.Name] = f.Type
	}
	bFields := make(map[string]Type, len(bRes.Fields))
	for _, f := range bRes.Fields {
		bFields[f.Name] = f.Type
	}

	// Shared fields
	for name, at := range aFields {
		if bt, ok := bFields[name]; ok {
			if !typeEqual(at, bt) && !numCompatible(at, bt) {
				*errors = append(*errors, TypeError{
					Code:     "E302",
					Message:  fmt.Sprintf(`field type mismatch for "%s": expected %s, got %s`, name, showType(at), showType(bt)),
					Location: loc,
				})
				return false
			}
		}
	}

	// Only in a
	var onlyInA []RecordField
	for _, f := range aRes.Fields {
		if _, ok := bFields[f.Name]; !ok {
			onlyInA = append(onlyInA, f)
		}
	}
	// Only in b
	var onlyInB []RecordField
	for _, f := range bRes.Fields {
		if _, ok := aFields[f.Name]; !ok {
			onlyInB = append(onlyInB, f)
		}
	}

	if len(onlyInB) > 0 {
		if aRes.RowVar >= 0 {
			if rowOccursIn(aRes.RowVar, b) {
				*errors = append(*errors, TypeError{Code: "E301", Message: "infinite record type (occurs check)", Location: loc})
				return false
			}
			newTail := -1
			if bRes.RowVar >= 0 {
				newTail = bRes.RowVar
			}
			s.rowSubst[aRes.RowVar] = &rowSubstEntry{fields: onlyInB, tail: newTail}
		} else {
			names := make([]string, len(onlyInB))
			for i, f := range onlyInB {
				names[i] = f.Name
			}
			*errors = append(*errors, TypeError{
				Code:     "E303",
				Message:  fmt.Sprintf("record has extra fields: %s", strings.Join(names, ", ")),
				Location: loc,
			})
			return false
		}
	}

	if len(onlyInA) > 0 {
		if bRes.RowVar >= 0 {
			if rowOccursIn(bRes.RowVar, a) {
				*errors = append(*errors, TypeError{Code: "E301", Message: "infinite record type (occurs check)", Location: loc})
				return false
			}
			newTail := -1
			if aRes.RowVar >= 0 {
				newTail = aRes.RowVar
			}
			s.rowSubst[bRes.RowVar] = &rowSubstEntry{fields: onlyInA, tail: newTail}
		} else {
			names := make([]string, len(onlyInA))
			for i, f := range onlyInA {
				names[i] = f.Name
			}
			*errors = append(*errors, TypeError{
				Code:     "E301",
				Message:  fmt.Sprintf("record missing required field(s): %s", strings.Join(names, ", ")),
				Location: loc,
			})
			return false
		}
	}

	if len(onlyInA) == 0 && len(onlyInB) == 0 {
		if aRes.RowVar >= 0 && bRes.RowVar >= 0 && aRes.RowVar != bRes.RowVar {
			s.rowSubst[aRes.RowVar] = &rowSubstEntry{tail: bRes.RowVar}
		}
	}

	return true
}

// ── Type environment ──

type typeEnv struct {
	bindings    map[string]Type
	schemes     map[string]TypeScheme
	constraints map[string]*fnConstraintInfo
	parent      *typeEnv
}

func newTypeEnv(parent *typeEnv) *typeEnv {
	return &typeEnv{
		bindings:    make(map[string]Type),
		schemes:     make(map[string]TypeScheme),
		constraints: make(map[string]*fnConstraintInfo),
		parent:      parent,
	}
}

func (e *typeEnv) get(name string) (Type, bool) {
	if t, ok := e.bindings[name]; ok {
		return t, true
	}
	if e.parent != nil {
		return e.parent.get(name)
	}
	return nil, false
}

func (e *typeEnv) set(name string, t Type) {
	e.bindings[name] = t
}

func (e *typeEnv) getScheme(name string) (TypeScheme, bool) {
	if s, ok := e.schemes[name]; ok {
		return s, true
	}
	if e.parent != nil {
		return e.parent.getScheme(name)
	}
	return TypeScheme{}, false
}

func (e *typeEnv) setScheme(name string, s TypeScheme) {
	e.schemes[name] = s
}

func (e *typeEnv) getConstraint(name string) (*fnConstraintInfo, bool) {
	if c, ok := e.constraints[name]; ok {
		return c, true
	}
	if e.parent != nil {
		return e.parent.getConstraint(name)
	}
	return nil, false
}

func (e *typeEnv) setConstraint(name string, c *fnConstraintInfo) {
	e.constraints[name] = c
}

func (e *typeEnv) freeTypeVars(s *checkerState) map[int]bool {
	result := make(map[int]bool)
	for _, t := range e.bindings {
		for v := range s.freeTypeVarsInType(t) {
			result[v] = true
		}
	}
	if e.parent != nil {
		for v := range e.parent.freeTypeVars(s) {
			result[v] = true
		}
	}
	return result
}

func (e *typeEnv) extend() *typeEnv {
	return newTypeEnv(e)
}

// ── Resolve TypeExpr → Type ──

var primitiveTypeNames = map[string]bool{"Int": true, "Rat": true, "Bool": true, "Str": true, "Unit": true}

func (s *checkerState) isTypeParam(name string) bool {
	if primitiveTypeNames[name] {
		return false
	}
	if s.knownTypes[name] {
		return false
	}
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func (s *checkerState) collectTypeParams(te ast.TypeExpr) map[string]bool {
	params := make(map[string]bool)
	s.walkTypeParams(te, params)
	return params
}

func (s *checkerState) walkTypeParams(te ast.TypeExpr, params map[string]bool) {
	switch t := te.(type) {
	case ast.TypeName:
		if s.isTypeParam(t.Name) {
			params[t.Name] = true
		}
	case ast.TypeList:
		s.walkTypeParams(t.Element, params)
	case ast.TypeTuple:
		for _, e := range t.Elements {
			s.walkTypeParams(e, params)
		}
	case ast.TypeFn:
		s.walkTypeParams(t.Param, params)
		s.walkTypeParams(t.Result, params)
	case ast.TypeGeneric:
		if s.isTypeParam(t.Name) && len(t.Args) == 0 {
			params[t.Name] = true
		}
		for _, a := range t.Args {
			s.walkTypeParams(a, params)
		}
	case ast.TypeRecord:
		for _, f := range t.Fields {
			s.walkTypeParams(f.Type, params)
		}
	case ast.TypeRefined:
		s.walkTypeParams(t.Base, params)
	case ast.TypeBorrow:
		s.walkTypeParams(t.Inner, params)
	}
}

func resolveType(te ast.TypeExpr, s *checkerState) Type {
	switch t := te.(type) {
	case ast.TypeName:
		switch t.Name {
		case "Int":
			return TInt
		case "Rat":
			return TRat
		case "Bool":
			return TBool
		case "Str":
			return TStr
		case "Unit":
			return TUnit
		default:
			return TGeneric{Name: t.Name}
		}
	case ast.TypeList:
		return TList{Element: resolveType(t.Element, s)}
	case ast.TypeTuple:
		if len(t.Elements) == 0 {
			return TUnit
		}
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = resolveType(e, s)
		}
		return TTuple{Elements: elems}
	case ast.TypeFn:
		effects := make([]Effect, len(t.Effects))
		for i, e := range t.Effects {
			effects[i] = ENamed{Name: e.Name}
		}
		return TFn{
			Param:   resolveType(t.Param, s),
			Result:  resolveType(t.Result, s),
			Effects: effects,
		}
	case ast.TypeGeneric:
		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = resolveType(a, s)
		}
		return TGeneric{Name: t.Name, Args: args}
	case ast.TypeRecord:
		fields := make([]RecordField, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = RecordField{Name: f.Name, Tags: f.Tags, Type: resolveType(f.Type, s)}
		}
		if t.RowVar != "" {
			rv, ok := s.namedRowVars[t.RowVar]
			if !ok {
				rv = FreshVar()
				s.namedRowVars[t.RowVar] = rv
			}
			return TRecord{Fields: fields, RowVar: rv}
		}
		return TRecord{Fields: fields, RowVar: -1}
	case ast.TypeUnion:
		return TAny
	case ast.TypeRefined:
		return resolveType(t.Base, s)
	case ast.TypeBorrow:
		return TBorrow{Inner: resolveType(t.Inner, s)}
	case ast.TypeTagProject:
		baseType := resolveType(t.Base, s)
		if rec, ok := baseType.(TRecord); ok {
			var filtered []RecordField
			for _, f := range rec.Fields {
				for _, tag := range f.Tags {
					if tag == t.TagName {
						filtered = append(filtered, f)
						break
					}
				}
			}
			return TRecord{Fields: filtered, RowVar: -1}
		}
		return TAny
	case ast.TypeTypeFilter:
		baseType := resolveType(t.Base, s)
		filterType := resolveType(t.FilterType, s)
		if rec, ok := baseType.(TRecord); ok {
			var filtered []RecordField
			for _, f := range rec.Fields {
				if typeEqual(f.Type, filterType) {
					filtered = append(filtered, f)
				}
			}
			return TRecord{Fields: filtered, RowVar: -1}
		}
		return TAny
	case ast.TypePick:
		baseType := resolveType(t.Base, s)
		if rec, ok := baseType.(TRecord); ok {
			nameSet := make(map[string]bool, len(t.FieldNames))
			for _, n := range t.FieldNames {
				nameSet[n] = true
			}
			var filtered []RecordField
			for _, f := range rec.Fields {
				if nameSet[f.Name] {
					filtered = append(filtered, f)
				}
			}
			return TRecord{Fields: filtered, RowVar: -1}
		}
		return TAny
	case ast.TypeOmit:
		baseType := resolveType(t.Base, s)
		if rec, ok := baseType.(TRecord); ok {
			nameSet := make(map[string]bool, len(t.FieldNames))
			for _, n := range t.FieldNames {
				nameSet[n] = true
			}
			var filtered []RecordField
			for _, f := range rec.Fields {
				if !nameSet[f.Name] {
					filtered = append(filtered, f)
				}
			}
			return TRecord{Fields: filtered, RowVar: -1}
		}
		return TAny
	}
	return TAny
}

func resolveTypeWithVars(te ast.TypeExpr, varMapping map[string]Type, s *checkerState) Type {
	switch t := te.(type) {
	case ast.TypeName:
		if mapped, ok := varMapping[t.Name]; ok {
			return mapped
		}
		return resolveType(te, s)
	case ast.TypeList:
		return TList{Element: resolveTypeWithVars(t.Element, varMapping, s)}
	case ast.TypeTuple:
		if len(t.Elements) == 0 {
			return TUnit
		}
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = resolveTypeWithVars(e, varMapping, s)
		}
		return TTuple{Elements: elems}
	case ast.TypeFn:
		effects := make([]Effect, len(t.Effects))
		for i, e := range t.Effects {
			effects[i] = ENamed{Name: e.Name}
		}
		return TFn{
			Param:   resolveTypeWithVars(t.Param, varMapping, s),
			Result:  resolveTypeWithVars(t.Result, varMapping, s),
			Effects: effects,
		}
	case ast.TypeGeneric:
		if mapped, ok := varMapping[t.Name]; ok && len(t.Args) == 0 {
			return mapped
		}
		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = resolveTypeWithVars(a, varMapping, s)
		}
		return TGeneric{Name: t.Name, Args: args}
	case ast.TypeRecord:
		fields := make([]RecordField, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = RecordField{Name: f.Name, Tags: f.Tags, Type: resolveTypeWithVars(f.Type, varMapping, s)}
		}
		if t.RowVar != "" {
			rv, ok := s.namedRowVars[t.RowVar]
			if !ok {
				rv = FreshVar()
				s.namedRowVars[t.RowVar] = rv
			}
			return TRecord{Fields: fields, RowVar: rv}
		}
		return TRecord{Fields: fields, RowVar: -1}
	case ast.TypeRefined:
		return resolveTypeWithVars(t.Base, varMapping, s)
	case ast.TypeBorrow:
		return TBorrow{Inner: resolveTypeWithVars(t.Inner, varMapping, s)}
	}
	return resolveType(te, s)
}

// ── Display types ──

func showType(t Type) string {
	switch t := t.(type) {
	case TPrimitive:
		if t.Name == "unit" {
			return "()"
		}
		return strings.ToUpper(t.Name[:1]) + t.Name[1:]
	case TFn:
		effs := ""
		if len(t.Effects) > 0 {
			names := make([]string, len(t.Effects))
			for i, e := range t.Effects {
				if en, ok := e.(ENamed); ok {
					names[i] = en.Name
				} else {
					names[i] = "?"
				}
			}
			effs = " {" + strings.Join(names, ", ") + "}"
		}
		return fmt.Sprintf("(%s ->%s %s)", showType(t.Param), effs, showType(t.Result))
	case TList:
		return fmt.Sprintf("[%s]", showType(t.Element))
	case TTuple:
		parts := make([]string, len(t.Elements))
		for i, e := range t.Elements {
			parts[i] = showType(e)
		}
		return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
	case TRecord:
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			tagPrefix := ""
			for _, tag := range f.Tags {
				tagPrefix += "@" + tag + " "
			}
			parts[i] = fmt.Sprintf("%s%s: %s", tagPrefix, f.Name, showType(f.Type))
		}
		fieldStr := strings.Join(parts, ", ")
		if t.RowVar >= 0 {
			if fieldStr != "" {
				return fmt.Sprintf("{%s | r%d}", fieldStr, t.RowVar)
			}
			return fmt.Sprintf("{r%d}", t.RowVar)
		}
		return fmt.Sprintf("{%s}", fieldStr)
	case TBorrow:
		return "&" + showType(t.Inner)
	case TVariant:
		names := make([]string, len(t.Variants))
		for i, v := range t.Variants {
			names[i] = v.Name
		}
		return strings.Join(names, " | ")
	case TVar:
		return fmt.Sprintf("t%d", t.ID)
	case TGeneric:
		if len(t.Args) > 0 {
			argStrs := make([]string, len(t.Args))
			for i, a := range t.Args {
				argStrs[i] = showType(a)
			}
			return fmt.Sprintf("%s<%s>", t.Name, strings.Join(argStrs, ", "))
		}
		return t.Name
	}
	return "?"
}

// ── Type equality / compatibility ──

func typeEqual(a, b Type) bool {
	if _, ok := a.(TGeneric); ok {
		return true
	}
	if _, ok := b.(TGeneric); ok {
		return true
	}
	if _, ok := a.(TVar); ok {
		return true
	}
	if _, ok := b.(TVar); ok {
		return true
	}
	switch a := a.(type) {
	case TPrimitive:
		if b, ok := b.(TPrimitive); ok {
			return a.Name == b.Name
		}
		return false
	case TFn:
		if b, ok := b.(TFn); ok {
			return typeEqual(a.Param, b.Param) && typeEqual(a.Result, b.Result)
		}
		return false
	case TList:
		if b, ok := b.(TList); ok {
			return typeEqual(a.Element, b.Element)
		}
		return false
	case TTuple:
		if b, ok := b.(TTuple); ok {
			if len(a.Elements) != len(b.Elements) {
				return false
			}
			for i := range a.Elements {
				if !typeEqual(a.Elements[i], b.Elements[i]) {
					return false
				}
			}
			return true
		}
		return false
	case TBorrow:
		if b, ok := b.(TBorrow); ok {
			return typeEqual(a.Inner, b.Inner)
		}
		return false
	case TRecord:
		if b, ok := b.(TRecord); ok {
			aMap := make(map[string]Type, len(a.Fields))
			for _, f := range a.Fields {
				aMap[f.Name] = f.Type
			}
			bMap := make(map[string]Type, len(b.Fields))
			for _, f := range b.Fields {
				bMap[f.Name] = f.Type
			}
			for name, at := range aMap {
				if bt, ok := bMap[name]; ok {
					if !typeEqual(at, bt) {
						return false
					}
				}
			}
			if a.RowVar < 0 && b.RowVar < 0 {
				if len(a.Fields) != len(b.Fields) {
					return false
				}
				for name := range bMap {
					if _, ok := aMap[name]; !ok {
						return false
					}
				}
			}
			return true
		}
		return false
	}
	return true
}

func numCompatible(a, b Type) bool {
	isNum := func(t Type) bool {
		if p, ok := t.(TPrimitive); ok {
			return p.Name == "int" || p.Name == "rat"
		}
		return false
	}
	return isNum(a) && isNum(b)
}

// ── Helpers ──

func isAnyType(t Type) bool {
	if g, ok := t.(TGeneric); ok {
		return g.Name == "?"
	}
	return false
}

func typeSame(a, b Type) bool {
	// Quick identity check for common cases
	switch a := a.(type) {
	case TPrimitive:
		if b, ok := b.(TPrimitive); ok {
			return a.Name == b.Name
		}
	}
	return false
}

func exprLoc(e ast.Expr) token.Loc {
	return e.ExprLoc()
}

func containsBorrow(t Type) bool {
	switch t := t.(type) {
	case TBorrow:
		return true
	case TList:
		return containsBorrow(t.Element)
	case TTuple:
		for _, e := range t.Elements {
			if containsBorrow(e) {
				return true
			}
		}
	case TRecord:
		for _, f := range t.Fields {
			if containsBorrow(f.Type) {
				return true
			}
		}
	case TFn:
		return containsBorrow(t.Param) || containsBorrow(t.Result)
	}
	return false
}

func containsBorrowInData(t Type) bool {
	switch t := t.(type) {
	case TList:
		return containsBorrow(t.Element)
	case TTuple:
		for _, e := range t.Elements {
			if containsBorrow(e) {
				return true
			}
		}
	case TRecord:
		for _, f := range t.Fields {
			if containsBorrow(f.Type) {
				return true
			}
		}
	}
	return false
}

func isAffineType(t Type, affineTypes map[string]bool) bool {
	switch t := t.(type) {
	case TGeneric:
		return affineTypes[t.Name]
	case TList:
		return isAffineType(t.Element, affineTypes)
	case TTuple:
		for _, e := range t.Elements {
			if isAffineType(e, affineTypes) {
				return true
			}
		}
	case TRecord:
		for _, f := range t.Fields {
			if isAffineType(f.Type, affineTypes) {
				return true
			}
		}
	case TBorrow:
		return false
	}
	return false
}

// ── Canonical type names ──

var primitiveCanonical = map[string]string{"int": "Int", "rat": "Rat", "bool": "Bool", "str": "Str", "unit": "Unit"}
var canonicalToType = map[string]Type{"Int": TInt, "Rat": TRat, "Bool": TBool, "Str": TStr, "Unit": TUnit}

func typeFromCanonicalName(name string) Type {
	if t, ok := canonicalToType[name]; ok {
		return t
	}
	return TAny
}

func canonicalTypeName(t Type) string {
	switch t := t.(type) {
	case TPrimitive:
		if n, ok := primitiveCanonical[t.Name]; ok {
			return n
		}
	case TGeneric:
		if t.Name == "?" {
			return ""
		}
		return t.Name
	case TList:
		inner := canonicalTypeName(t.Element)
		if inner != "" {
			return "[" + inner + "]"
		}
	case TTuple:
		parts := make([]string, len(t.Elements))
		for i, e := range t.Elements {
			p := canonicalTypeName(e)
			if p == "" {
				return ""
			}
			parts[i] = p
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case TRecord:
		if t.RowVar >= 0 {
			return ""
		}
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			ft := canonicalTypeName(f.Type)
			if ft == "" {
				return ""
			}
			parts[i] = f.Name + ": " + ft
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case TBorrow:
		inner := canonicalTypeName(t.Inner)
		if inner != "" {
			return "&" + inner
		}
	}
	return ""
}

// ── Affine context ──

type affineCtx struct {
	affineTypes    map[string]bool
	cloneableTypes map[string]bool
	impls          map[string][]implEntry
	bindings       map[string]token.Loc // affine var → def loc
	consumed       map[string]token.Loc // affine var → consumption loc
}

func newAffineCtx(affineTypes, cloneableTypes map[string]bool, impls map[string][]implEntry) *affineCtx {
	return &affineCtx{
		affineTypes:    affineTypes,
		cloneableTypes: cloneableTypes,
		impls:          impls,
		bindings:       make(map[string]token.Loc),
		consumed:       make(map[string]token.Loc),
	}
}

func (a *affineCtx) implementsClone(typeName string) bool {
	if a.cloneableTypes[typeName] {
		return true
	}
	for _, e := range a.impls["Clone"] {
		if e.forType == typeName {
			return true
		}
	}
	return false
}

func (a *affineCtx) isTypeCloneable(t Type) bool {
	switch t := t.(type) {
	case TList:
		return a.isTypeCloneable(t.Element)
	case TTuple:
		for _, e := range t.Elements {
			if !a.isTypeCloneable(e) {
				return false
			}
		}
		return true
	case TRecord:
		for _, f := range t.Fields {
			if !a.isTypeCloneable(f.Type) {
				return false
			}
		}
		return true
	default:
		name := canonicalTypeName(t)
		return name != "" && a.implementsClone(name)
	}
}

func (a *affineCtx) registerAffine(name string, loc token.Loc) {
	a.bindings[name] = loc
}

func (a *affineCtx) isAffineVar(name string) bool {
	_, ok := a.bindings[name]
	return ok
}

func (a *affineCtx) affineBindingNames() map[string]bool {
	result := make(map[string]bool, len(a.bindings))
	for k := range a.bindings {
		result[k] = true
	}
	return result
}

func (a *affineCtx) consume(name string, loc token.Loc, errors *[]TypeError) {
	if _, ok := a.bindings[name]; !ok {
		return
	}
	if prev, ok := a.consumed[name]; ok {
		*errors = append(*errors, TypeError{
			Code:     "E600",
			Message:  fmt.Sprintf("affine variable '%s' used after move (first consumed at %d:%d)", name, prev.Line, prev.Col),
			Location: loc,
		})
	} else {
		a.consumed[name] = loc
	}
}

func (a *affineCtx) isConsumed(name string) bool {
	_, ok := a.consumed[name]
	return ok
}

func (a *affineCtx) snapshot() map[string]token.Loc {
	snap := make(map[string]token.Loc, len(a.consumed))
	for k, v := range a.consumed {
		snap[k] = v
	}
	return snap
}

func (a *affineCtx) restore(snap map[string]token.Loc) {
	a.consumed = make(map[string]token.Loc, len(snap))
	for k, v := range snap {
		a.consumed[k] = v
	}
}

func (a *affineCtx) snapshotBindings() map[string]token.Loc {
	snap := make(map[string]token.Loc, len(a.bindings))
	for k, v := range a.bindings {
		snap[k] = v
	}
	return snap
}

func (a *affineCtx) restoreBindings(snap map[string]token.Loc) {
	a.bindings = make(map[string]token.Loc, len(snap))
	for k, v := range snap {
		a.bindings[k] = v
	}
}

func (a *affineCtx) consumedSince(snap map[string]token.Loc) map[string]bool {
	result := make(map[string]bool)
	for name := range a.consumed {
		if _, ok := snap[name]; !ok {
			result[name] = true
		}
	}
	return result
}

func (a *affineCtx) checkBranchConsistency(
	preBranch map[string]token.Loc,
	branchConsumed []map[string]bool,
	loc token.Loc,
	errors *[]TypeError,
) {
	if len(branchConsumed) < 2 {
		return
	}
	allConsumed := make(map[string]bool)
	for _, bc := range branchConsumed {
		for n := range bc {
			allConsumed[n] = true
		}
	}
	for name := range allConsumed {
		inSome := false
		notInSome := false
		for _, bc := range branchConsumed {
			if bc[name] {
				inSome = true
			} else {
				notInSome = true
			}
		}
		if inSome && notInSome {
			*errors = append(*errors, TypeError{
				Code:     "E601",
				Message:  fmt.Sprintf("affine variable '%s' consumed in some branches but not others", name),
				Location: loc,
			})
		}
	}
	a.restore(preBranch)
	for name := range allConsumed {
		if _, ok := a.consumed[name]; !ok {
			a.consumed[name] = loc
		}
	}
}

func (a *affineCtx) checkUnconsumed(scopeVars []string, loc token.Loc, errors *[]TypeError) {
	for _, name := range scopeVars {
		if _, ok := a.bindings[name]; ok {
			if _, ok := a.consumed[name]; !ok {
				*errors = append(*errors, TypeError{
					Code:     "W600",
					Message:  fmt.Sprintf("affine variable '%s' is never consumed (potential resource leak)", name),
					Location: loc,
				})
			}
		}
	}
}

// ── Type parameter substitution ──

func substituteTypeParams(t Type, bindings map[string]string) Type {
	switch t := t.(type) {
	case TGeneric:
		if bound, ok := bindings[t.Name]; ok {
			switch bound {
			case "Int":
				return TInt
			case "Rat":
				return TRat
			case "Bool":
				return TBool
			case "Str":
				return TStr
			case "Unit":
				return TUnit
			default:
				args := make([]Type, len(t.Args))
				for i, a := range t.Args {
					args[i] = substituteTypeParams(a, bindings)
				}
				return TGeneric{Name: bound, Args: args}
			}
		}
		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = substituteTypeParams(a, bindings)
		}
		return TGeneric{Name: t.Name, Args: args}
	case TFn:
		return TFn{Param: substituteTypeParams(t.Param, bindings), Result: substituteTypeParams(t.Result, bindings), Effects: t.Effects}
	case TList:
		return TList{Element: substituteTypeParams(t.Element, bindings)}
	case TTuple:
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = substituteTypeParams(e, bindings)
		}
		return TTuple{Elements: elems}
	case TRecord:
		fields := make([]RecordField, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = RecordField{Name: f.Name, Tags: f.Tags, Type: substituteTypeParams(f.Type, bindings)}
		}
		return TRecord{Fields: fields, RowVar: t.RowVar}
	case TBorrow:
		return TBorrow{Inner: substituteTypeParams(t.Inner, bindings)}
	}
	return t
}

func extractTypeBindings(paramExpr ast.TypeExpr, argType Type, bindings map[string]string) {
	switch pe := paramExpr.(type) {
	case ast.TypeName:
		name := pe.Name
		if _, ok := bindings[name]; ok {
			return
		}
		switch at := argType.(type) {
		case TPrimitive:
			bindings[name] = strings.ToUpper(at.Name[:1]) + at.Name[1:]
		case TGeneric:
			if at.Name != "?" {
				bindings[name] = at.Name
			}
		default:
			cn := canonicalTypeName(argType)
			if cn != "" {
				bindings[name] = cn
			}
		}
	case ast.TypeList:
		if at, ok := argType.(TList); ok {
			extractTypeBindings(pe.Element, at.Element, bindings)
		}
	case ast.TypeTuple:
		if at, ok := argType.(TTuple); ok {
			for i := 0; i < len(pe.Elements) && i < len(at.Elements); i++ {
				extractTypeBindings(pe.Elements[i], at.Elements[i], bindings)
			}
		}
	case ast.TypeFn:
		if at, ok := argType.(TFn); ok {
			extractTypeBindings(pe.Param, at.Param, bindings)
			extractTypeBindings(pe.Result, at.Result, bindings)
		}
	case ast.TypeGeneric:
		if at, ok := argType.(TGeneric); ok {
			for i := 0; i < len(pe.Args) && i < len(at.Args); i++ {
				extractTypeBindings(pe.Args[i], at.Args[i], bindings)
			}
		}
	case ast.TypeRefined:
		extractTypeBindings(pe.Base, argType, bindings)
	case ast.TypeBorrow:
		extractTypeBindings(pe.Inner, argType, bindings)
	}
}

func refineCompositeReturn(fnName string, argTypes []Type) Type {
	if len(argTypes) == 0 {
		return nil
	}
	arg0 := argTypes[0]
	switch fnName {
	case "head", "get":
		if l, ok := arg0.(TList); ok {
			if !isAnyType(l.Element) {
				return l.Element
			}
		}
	case "tail", "rev", "filter":
		if l, ok := arg0.(TList); ok {
			return TList{Element: l.Element}
		}
	case "cons":
		if len(argTypes) >= 2 {
			if l, ok := argTypes[1].(TList); ok {
				return TList{Element: l.Element}
			}
		}
		if !isAnyType(arg0) {
			return TList{Element: arg0}
		}
	case "cat":
		if l, ok := arg0.(TList); ok {
			return TList{Element: l.Element}
		}
	case "map":
		if len(argTypes) >= 2 {
			if fn, ok := argTypes[1].(TFn); ok {
				return TList{Element: fn.Result}
			}
		}
		if l, ok := arg0.(TList); ok {
			return TList{Element: l.Element}
		}
	case "flat-map":
		if len(argTypes) >= 2 {
			if fn, ok := argTypes[1].(TFn); ok {
				if l, ok := fn.Result.(TList); ok {
					return l
				}
			}
		}
		if _, ok := arg0.(TList); ok {
			return TList{Element: TAny}
		}
	case "fold":
		if len(argTypes) >= 2 {
			return argTypes[1]
		}
	case "zip":
		if len(argTypes) >= 2 {
			if la, ok := arg0.(TList); ok {
				if lb, ok := argTypes[1].(TList); ok {
					return TList{Element: TTuple{Elements: []Type{la.Element, lb.Element}}}
				}
			}
		}
	case "range":
		return TList{Element: TInt}
	case "split":
		return TList{Element: TStr}
	case "fst":
		if t, ok := arg0.(TTuple); ok && len(t.Elements) >= 1 {
			return t.Elements[0]
		}
	case "snd":
		if t, ok := arg0.(TTuple); ok && len(t.Elements) >= 2 {
			return t.Elements[1]
		}
	}
	return nil
}

func typeExprName(te ast.TypeExpr) string {
	switch t := te.(type) {
	case ast.TypeName:
		return t.Name
	case ast.TypeList:
		return "[" + typeExprName(t.Element) + "]"
	case ast.TypeTuple:
		parts := make([]string, len(t.Elements))
		for i, e := range t.Elements {
			parts[i] = typeExprName(e)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case ast.TypeGeneric:
		if len(t.Args) > 0 {
			argParts := make([]string, len(t.Args))
			for i, a := range t.Args {
				argParts[i] = typeExprName(a)
			}
			return t.Name + "<" + strings.Join(argParts, ", ") + ">"
		}
		return t.Name
	case ast.TypeBorrow:
		return "&" + typeExprName(t.Inner)
	}
	return "?"
}

// ── Impl lookup ──

var derivableInterfaces = map[string]bool{"Clone": true, "Show": true, "Eq": true, "Ord": true, "Default": true}

func (s *checkerState) hasImpl(interfaceName, typeName string, constraintTypeArgs []string) bool {
	entries := s.implRegistry[interfaceName]
	// Direct impl lookup
	for _, e := range entries {
		if e.forType == typeName {
			if len(constraintTypeArgs) == 0 || len(e.typeArgs) == 0 {
				return true
			}
			if len(e.typeArgs) == len(constraintTypeArgs) {
				match := true
				for i := range e.typeArgs {
					if e.typeArgs[i] != constraintTypeArgs[i] {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	// Composite type structural check for derivable interfaces
	if derivableInterfaces[interfaceName] {
		if strings.HasPrefix(typeName, "[") && strings.HasSuffix(typeName, "]") {
			elemType := typeName[1 : len(typeName)-1]
			return s.hasImpl(interfaceName, elemType, nil)
		}
		if strings.HasPrefix(typeName, "(") && strings.HasSuffix(typeName, ")") {
			inner := typeName[1 : len(typeName)-1]
			elemTypes := splitTopLevelComma(inner)
			for _, et := range elemTypes {
				if !s.hasImpl(interfaceName, trimSpace(et), nil) {
					return false
				}
			}
			return true
		}
		if strings.HasPrefix(typeName, "{") && strings.HasSuffix(typeName, "}") {
			inner := typeName[1 : len(typeName)-1]
			fieldParts := splitTopLevelComma(inner)
			for _, fp := range fieldParts {
				colonIdx := strings.Index(fp, ":")
				if colonIdx < 0 {
					return false
				}
				if !s.hasImpl(interfaceName, trimSpace(fp[colonIdx+1:]), nil) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func (s *checkerState) resolveConstraintInfo(expr ast.Expr, env *typeEnv) *fnConstraintInfo {
	switch e := expr.(type) {
	case ast.ExprVar:
		if c, ok := s.fnConstraints[e.Name]; ok {
			return c
		}
		if c, ok := env.getConstraint(e.Name); ok {
			return c
		}
	case ast.ExprApply:
		if v, ok := e.Fn.(ast.ExprVar); ok {
			if c, ok := s.fnConstraints[v.Name]; ok {
				return c
			}
			if c, ok := env.getConstraint(v.Name); ok {
				return c
			}
		}
	case ast.ExprPipeline:
		return s.resolveConstraintInfo(e.Right, env)
	case ast.ExprLambda:
		if app, ok := e.Body.(ast.ExprApply); ok {
			return s.resolveConstraintInfo(app.Fn, env)
		}
	}
	return nil
}

// ── Match exhaustiveness ──

func checkMatchExhaustiveness(
	expr ast.ExprMatch,
	subjectType Type,
	registry map[string][]string,
	errors *[]TypeError,
) {
	g, ok := subjectType.(TGeneric)
	if !ok || g.Name == "?" {
		return
	}
	allVariants := registry[g.Name]
	if len(allVariants) == 0 {
		return
	}
	for _, arm := range expr.Arms {
		switch arm.Pattern.(type) {
		case ast.PatWildcard, ast.PatVar:
			return
		}
	}
	covered := make(map[string]bool)
	for _, arm := range expr.Arms {
		if pv, ok := arm.Pattern.(ast.PatVariant); ok {
			covered[pv.Name] = true
		}
	}
	var missing []string
	for _, v := range allVariants {
		if !covered[v] {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		s := ""
		if len(missing) > 1 {
			s = "s"
		}
		*errors = append(*errors, TypeError{
			Code:     "W400",
			Message:  fmt.Sprintf("non-exhaustive match: missing variant%s %s", s, strings.Join(missing, ", ")),
			Location: expr.Loc,
		})
	}
}

// ── Infer expression type ──

func (s *checkerState) inferExpr(
	expr ast.Expr, env *typeEnv, errors *[]TypeError,
	registry map[string][]string, aff *affineCtx,
) Type {
	switch e := expr.(type) {
	case ast.ExprLiteral:
		switch e.Value.(type) {
		case ast.LitInt:
			return TInt
		case ast.LitRat:
			return TRat
		case ast.LitBool:
			return TBool
		case ast.LitStr:
			return TStr
		case ast.LitUnit:
			return TUnit
		}

	case ast.ExprVar:
		// HM: instantiate polymorphic schemes
		if scheme, ok := env.getScheme(e.Name); ok && len(scheme.TypeVars) > 0 {
			if aff.isAffineVar(e.Name) {
				aff.consume(e.Name, e.Loc, errors)
			}
			return s.instantiateScheme(scheme)
		}
		t, ok := env.get(e.Name)
		if !ok {
			*errors = append(*errors, TypeError{Code: "E300", Message: fmt.Sprintf("unbound variable '%s'", e.Name), Location: e.Loc})
			return TAny
		}
		if aff.isAffineVar(e.Name) {
			aff.consume(e.Name, e.Loc, errors)
		}
		return t

	case ast.ExprLet:
		valType := s.inferExpr(e.Value, env, errors, registry, aff)
		if containsBorrowInData(valType) {
			*errors = append(*errors, TypeError{
				Code:     "E603",
				Message:  fmt.Sprintf("let binding '%s' contains a borrow in a compound type '%s' (borrows cannot be stored in data structures)", e.Name, showType(valType)),
				Location: e.Loc,
			})
		}
		if e.Body == nil {
			return valType
		}
		if e.Name == "_" {
			return s.inferExpr(e.Body, env, errors, registry, aff)
		}
		child := env.extend()
		resolvedVal := s.applyTypeSubst(valType)
		child.set(e.Name, resolvedVal)
		if _, ok := e.Value.(ast.ExprLambda); ok {
			envFreeVars := env.freeTypeVars(s)
			scheme := s.generalizeType(envFreeVars, resolvedVal)
			if len(scheme.TypeVars) > 0 {
				child.setScheme(e.Name, scheme)
			}
		}
		if cInfo := s.resolveConstraintInfo(e.Value, env); cInfo != nil {
			child.setConstraint(e.Name, cInfo)
		}
		preLetRef := s.saveRefinementState()
		if v, ok := e.Value.(ast.ExprVar); ok {
			if ref, has := s.varRefinements[v.Name]; has {
				s.varRefinements[e.Name] = ref
			}
		} else {
			if valPredStr, ok := exprToPredString(e.Value); ok {
				s.pathConditions = append(s.pathConditions, fmt.Sprintf("%s == %s", e.Name, valPredStr))
			}
		}
		if isAffineType(valType, aff.affineTypes) {
			aff.registerAffine(e.Name, e.Loc)
		}
		bodyType := s.inferExpr(e.Body, child, errors, registry, aff)
		s.restoreRefinementState(preLetRef)
		if isAffineType(valType, aff.affineTypes) && e.Name != "_" {
			aff.checkUnconsumed([]string{e.Name}, e.Loc, errors)
		}
		return bodyType

	case ast.ExprIf:
		condType := s.inferExpr(e.Cond, env, errors, registry, aff)
		if p, ok := condType.(TPrimitive); ok && p.Name != "bool" {
			*errors = append(*errors, TypeError{
				Code:     "E301",
				Message:  fmt.Sprintf("if condition must be Bool, got %s", showType(condType)),
				Location: e.Loc,
			})
		}
		pos, neg, condOk := extractConditionPredicate(e.Cond)
		preBranch := aff.snapshot()
		preRef := s.saveRefinementState()

		if condOk {
			s.pathConditions = append(s.pathConditions, pos)
		}
		thenType := s.inferExpr(e.Then, env, errors, registry, aff)
		thenConsumed := aff.consumedSince(preBranch)
		aff.restore(preBranch)
		s.restoreRefinementState(preRef)

		if condOk {
			s.pathConditions = append(s.pathConditions, neg)
		}
		elseType := s.inferExpr(e.Else, env, errors, registry, aff)
		elseConsumed := aff.consumedSince(preBranch)
		s.restoreRefinementState(preRef)

		aff.checkBranchConsistency(preBranch, []map[string]bool{thenConsumed, elseConsumed}, e.Loc, errors)

		if !typeEqual(thenType, elseType) && !numCompatible(thenType, elseType) {
			*errors = append(*errors, TypeError{
				Code:     "E302",
				Message:  fmt.Sprintf("if branches have different types: %s vs %s", showType(thenType), showType(elseType)),
				Location: e.Loc,
			})
		}
		return thenType

	case ast.ExprLambda:
		lamEnv := env.extend()
		paramTypes := make([]Type, len(e.Params))
		for i, p := range e.Params {
			var pt Type
			if p.Type != nil {
				pt = resolveType(p.Type, s)
			} else {
				pt = s.freshTypeVar()
			}
			lamEnv.set(p.Name, pt)
			paramTypes[i] = pt
			if isAffineType(pt, aff.affineTypes) {
				aff.registerAffine(p.Name, e.Loc)
			}
		}
		retType := s.inferExpr(e.Body, lamEnv, errors, registry, aff)
		if containsBorrow(retType) {
			*errors = append(*errors, TypeError{
				Code:     "E603",
				Message:  fmt.Sprintf("lambda cannot return a borrow type '%s' (borrows cannot escape their scope)", showType(retType)),
				Location: e.Loc,
			})
		}
		for _, pt := range paramTypes {
			if _, ok := pt.(TBorrow); !ok && containsBorrow(pt) {
				*errors = append(*errors, TypeError{
					Code:     "E603",
					Message:  fmt.Sprintf("lambda parameter contains a borrow in a compound type '%s' (borrows cannot be stored in data structures)", showType(pt)),
					Location: e.Loc,
				})
			}
		}
		var fnType Type = retType
		if len(paramTypes) == 0 {
			fnType = NewTFn(TUnit, fnType)
		} else {
			for i := len(paramTypes) - 1; i >= 0; i-- {
				fnType = NewTFn(paramTypes[i], fnType)
			}
		}
		return fnType

	case ast.ExprApply:
		rawFnType := s.inferExpr(e.Fn, env, errors, registry, aff)
		fnType := s.freshenRowVars(rawFnType, nil)
		if fn, ok := fnType.(TFn); ok {
			// 0-arg call on Unit -> T
			if len(e.Args) == 0 {
				if p, ok := fn.Param.(TPrimitive); ok && p.Name == "unit" {
					return fn.Result
				}
			}
			// Uncurry
			var paramTypes []Type
			cur := fnType
			for {
				if f, ok := cur.(TFn); ok && len(paramTypes) < len(e.Args) {
					paramTypes = append(paramTypes, f.Param)
					cur = f.Result
				} else {
					break
				}
			}
			if len(e.Args) > len(paramTypes) {
				*errors = append(*errors, TypeError{
					Code:     "E303",
					Message:  fmt.Sprintf("expected %d arguments, got %d", len(paramTypes), len(e.Args)),
					Location: e.Loc,
				})
			}

			argTypes := make([]Type, 0, len(e.Args))
			n := len(e.Args)
			if len(paramTypes) < n {
				n = len(paramTypes)
			}
			for i := 0; i < n; i++ {
				argType := s.inferExpr(e.Args[i], env, errors, registry, aff)
				argTypes = append(argTypes, argType)
				resolvedArg := s.applyRowSubst(argType)
				resolvedParam := s.applyRowSubst(paramTypes[i])
				if ra, ok := resolvedArg.(TRecord); ok {
					if rp, ok := resolvedParam.(TRecord); ok {
						s.unifyRecords(rp, ra, errors, e.Args[i].ExprLoc())
						continue
					}
				}
				if !s.unifyTypes(argType, paramTypes[i]) && !numCompatible(argType, paramTypes[i]) {
					*errors = append(*errors, TypeError{
						Code:     "E304",
						Message:  fmt.Sprintf("argument %d: expected %s, got %s", i+1, showType(paramTypes[i]), showType(argType)),
						Location: e.Args[i].ExprLoc(),
					})
				}
			}

			// Refinement checking at call sites
			if v, ok := e.Fn.(ast.ExprVar); ok {
				if refInfo, ok := s.fnRefInfo[v.Name]; ok {
					for i := 0; i < len(e.Args) && i < len(refInfo.paramPreds); i++ {
						if refInfo.paramPreds[i] == "" {
							continue
						}
						s.checkRefinementObligation(e.Args[i], refInfo.paramPreds[i], "E310", "refinement not satisfied", errors)
					}
				}
			}

			// Type parameter bindings
			typeBindings := make(map[string]string)
			cInfo := s.resolveConstraintInfo(e.Fn, env)
			var paramTEs []ast.TypeExpr
			if cInfo != nil {
				paramTEs = cInfo.paramTypeExps
			}
			if paramTEs == nil {
				if v, ok := e.Fn.(ast.ExprVar); ok {
					paramTEs = s.fnParamTypeExps[v.Name]
				}
			}
			if paramTEs != nil {
				for i := 0; i < len(argTypes) && i < len(paramTEs); i++ {
					extractTypeBindings(paramTEs[i], argTypes[i], typeBindings)
				}
			}
			if cInfo != nil && len(cInfo.constraints) > 0 {
				for _, c := range cInfo.constraints {
					concreteType, ok := typeBindings[c.TypeParam]
					if ok && concreteType != "?" {
						var resolvedTypeArgs []string
						if len(c.TypeArgs) > 0 {
							resolvedTypeArgs = make([]string, len(c.TypeArgs))
							for i, ta := range c.TypeArgs {
								name := typeExprName(ta)
								if bound, ok := typeBindings[name]; ok {
									resolvedTypeArgs[i] = bound
								} else {
									resolvedTypeArgs[i] = name
								}
							}
						}
						typeArgStr := ""
						if len(resolvedTypeArgs) > 0 {
							typeArgStr = "<" + strings.Join(resolvedTypeArgs, ", ") + ">"
						}
						if !s.hasImpl(c.Interface, concreteType, resolvedTypeArgs) {
							*errors = append(*errors, TypeError{
								Code:     "E205",
								Message:  fmt.Sprintf("where constraint not satisfied: '%s' does not implement '%s%s'", concreteType, c.Interface, typeArgStr),
								Location: e.Loc,
							})
						}
					}
				}
			}

			// Return type substitution
			returnType := cur
			if len(typeBindings) > 0 {
				returnType = substituteTypeParams(cur, typeBindings)
			}
			resolved := s.applyTypeSubst(returnType)

			// Per-call-site specialization
			if v, ok := e.Fn.(ast.ExprVar); ok && len(argTypes) > 0 {
				fnName := v.Name
				if g, ok := resolved.(TGeneric); ok && g.Name == "?" {
					if fnName == "clone" && len(argTypes) == 1 {
						return argTypes[0]
					}
					if fnName == "cmp" && len(argTypes) == 2 {
						return TGeneric{Name: "Ordering"}
					}
					if fnName == "from" && len(argTypes) == 1 {
						argName := canonicalTypeName(argTypes[0])
						if argName != "" {
							if fromImpls, ok := s.implRegistry["From"]; ok {
								var matches []implEntry
								for _, ie := range fromImpls {
									if len(ie.typeArgs) > 0 && ie.typeArgs[0] == argName {
										matches = append(matches, ie)
									}
								}
								if len(matches) == 1 {
									return typeFromCanonicalName(matches[0].forType)
								}
							}
						}
					}
					if fnName == "into" && len(argTypes) == 1 {
						argName := canonicalTypeName(argTypes[0])
						if argName != "" {
							if intoImpls, ok := s.implRegistry["Into"]; ok {
								var matches []implEntry
								for _, ie := range intoImpls {
									if ie.forType == argName {
										matches = append(matches, ie)
									}
								}
								if len(matches) == 1 && len(matches[0].typeArgs) > 0 {
									return typeFromCanonicalName(matches[0].typeArgs[0])
								}
							}
						}
					}
				}
				if refined := refineCompositeReturn(fnName, argTypes); refined != nil {
					return refined
				}
			}
			return resolved
		}
		// Non-function type being called
		for _, arg := range e.Args {
			s.inferExpr(arg, env, errors, registry, aff)
		}
		return TAny

	case ast.ExprMatch:
		subjectType := s.inferExpr(e.Subject, env, errors, registry, aff)
		var resultType Type
		preBranch := aff.snapshot()
		preMatchBindings := aff.affineBindingNames()
		preRef := s.saveRefinementState()
		var branchConsumed []map[string]bool
		subjectPredStr, _ := exprToPredString(e.Subject)
		preMatchAffineBindings := aff.snapshotBindings()

		for _, arm := range e.Arms {
			aff.restore(preBranch)
			aff.restoreBindings(preMatchAffineBindings)
			s.restoreRefinementState(preRef)
			armEnv := env.extend()
			s.bindPatternVars(arm.Pattern, armEnv, aff, subjectType)
			if subjectPredStr != "" {
				if patPred, ok := patternToPredicate(arm.Pattern, subjectPredStr); ok {
					s.pathConditions = append(s.pathConditions, patPred)
				}
			}
			armType := s.inferExpr(arm.Body, armEnv, errors, registry, aff)
			// Check pattern-bound affine vars consumed
			postArmBindings := aff.affineBindingNames()
			var patternBound []string
			for name := range postArmBindings {
				if !preMatchBindings[name] {
					patternBound = append(patternBound, name)
				}
			}
			if len(patternBound) > 0 {
				aff.checkUnconsumed(patternBound, arm.Pattern.PatLoc(), errors)
			}
			consumed := aff.consumedSince(preBranch)
			scopedConsumed := make(map[string]bool)
			for name := range consumed {
				if preMatchBindings[name] {
					scopedConsumed[name] = true
				}
			}
			branchConsumed = append(branchConsumed, scopedConsumed)
			if resultType == nil {
				resultType = armType
			} else if !typeEqual(resultType, armType) && !numCompatible(resultType, armType) {
				*errors = append(*errors, TypeError{
					Code:     "E305",
					Message:  fmt.Sprintf("match arms have inconsistent types: %s vs %s", showType(resultType), showType(armType)),
					Location: e.Loc,
				})
			}
		}
		s.restoreRefinementState(preRef)
		aff.restoreBindings(preMatchAffineBindings)
		if len(branchConsumed) >= 2 {
			aff.checkBranchConsistency(preBranch, branchConsumed, e.Loc, errors)
		}
		checkMatchExhaustiveness(e, subjectType, registry, errors)
		if resultType == nil {
			return TAny
		}
		return resultType

	case ast.ExprList:
		if len(e.Elements) == 0 {
			return TList{Element: TAny}
		}
		elemType := s.inferExpr(e.Elements[0], env, errors, registry, aff)
		for i := 1; i < len(e.Elements); i++ {
			et := s.inferExpr(e.Elements[i], env, errors, registry, aff)
			if !typeEqual(elemType, et) && !numCompatible(elemType, et) {
				*errors = append(*errors, TypeError{
					Code:     "E306",
					Message:  fmt.Sprintf("list element %d has type %s, expected %s", i+1, showType(et), showType(elemType)),
					Location: e.Elements[i].ExprLoc(),
				})
			}
		}
		return TList{Element: elemType}

	case ast.ExprTuple:
		elems := make([]Type, len(e.Elements))
		for i, el := range e.Elements {
			elems[i] = s.inferExpr(el, env, errors, registry, aff)
		}
		return TTuple{Elements: elems}

	case ast.ExprHandle:
		s.inferExpr(e.Expr, env, errors, registry, aff)
		resultType := TAny
		for _, arm := range e.Arms {
			armEnv := env.extend()
			for _, p := range arm.Params {
				armEnv.set(p.Name, TAny)
			}
			if arm.ResumeName != "" {
				armEnv.set(arm.ResumeName, NewTFn(TAny, TAny))
			}
			armType := s.inferExpr(arm.Body, armEnv, errors, registry, aff)
			if arm.Name == "return" {
				resultType = armType
			}
		}
		return resultType

	case ast.ExprPerform:
		return s.inferExpr(e.Expr, env, errors, registry, aff)

	case ast.ExprBorrow:
		innerType := s.inferExpr(e.Expr, env, errors, registry, aff)
		if v, ok := e.Expr.(ast.ExprVar); ok && aff.isAffineVar(v.Name) {
			if aff.isConsumed(v.Name) {
				snap := aff.snapshot()
				delete(snap, v.Name)
				aff.restore(snap)
			}
		}
		return TBorrow{Inner: innerType}

	case ast.ExprClone:
		innerType := s.inferExpr(e.Expr, env, errors, registry, aff)
		var ownedType Type
		if b, ok := innerType.(TBorrow); ok {
			ownedType = b.Inner
		} else {
			ownedType = innerType
		}
		typeName := canonicalTypeName(ownedType)
		if typeName != "" && !aff.isTypeCloneable(ownedType) {
			*errors = append(*errors, TypeError{
				Code:     "E602",
				Message:  fmt.Sprintf("type '%s' does not implement Clone", typeName),
				Location: e.Loc,
			})
		}
		return ownedType

	case ast.ExprDiscard:
		s.inferExpr(e.Expr, env, errors, registry, aff)
		return TUnit

	case ast.ExprRecord:
		var fields []RecordField
		seen := make(map[string]bool)
		for _, f := range e.Fields {
			if seen[f.Name] {
				*errors = append(*errors, TypeError{
					Code:     "E304",
					Message:  fmt.Sprintf(`duplicate field "%s" in record literal`, f.Name),
					Location: e.Loc,
				})
			}
			seen[f.Name] = true
			ft := s.inferExpr(f.Value, env, errors, registry, aff)
			tags := f.Tags
			if tags == nil {
				tags = []string{}
			}
			fields = append(fields, RecordField{Name: f.Name, Tags: tags, Type: ft})
		}
		if e.Spread != nil {
			baseType := s.inferExpr(e.Spread, env, errors, registry, aff)
			resolved := s.applyRowSubst(baseType)
			if rec, ok := resolved.(TRecord); ok {
				for _, bf := range rec.Fields {
					if !seen[bf.Name] {
						fields = append(fields, RecordField{Name: bf.Name, Tags: bf.Tags, Type: bf.Type})
					}
				}
				return TRecord{Fields: fields, RowVar: rec.RowVar}
			}
		}
		return TRecord{Fields: fields, RowVar: -1}

	case ast.ExprFieldAccess:
		// Dotted builtin check
		if v, ok := e.Object.(ast.ExprVar); ok {
			dottedName := v.Name + "." + e.Field
			if scheme, ok := env.getScheme(dottedName); ok && len(scheme.TypeVars) > 0 {
				return s.instantiateScheme(scheme)
			}
			if bt, ok := env.get(dottedName); ok {
				return bt
			}
		}
		objType := s.inferExpr(e.Object, env, errors, registry, aff)
		resolved := s.applyRowSubst(objType)
		if rec, ok := resolved.(TRecord); ok {
			for _, f := range rec.Fields {
				if f.Name == e.Field {
					return s.applyRowSubst(f.Type)
				}
			}
			if rec.RowVar >= 0 {
				fieldType := s.freshTypeVar()
				newTail := FreshVar()
				s.rowSubst[rec.RowVar] = &rowSubstEntry{
					fields: []RecordField{{Name: e.Field, Tags: []string{}, Type: fieldType}},
					tail:   newTail,
				}
				return fieldType
			}
			*errors = append(*errors, TypeError{
				Code:     "E301",
				Message:  fmt.Sprintf(`record missing required field "%s" — record type is %s`, e.Field, showType(resolved)),
				Location: e.Loc,
			})
			return TAny
		}
		return TAny

	case ast.ExprRecordUpdate:
		baseType := s.inferExpr(e.Base, env, errors, registry, aff)
		resolved := s.applyRowSubst(baseType)
		if rec, ok := resolved.(TRecord); ok {
			baseFields := make(map[string]Type, len(rec.Fields))
			for _, f := range rec.Fields {
				baseFields[f.Name] = f.Type
			}
			for _, f := range e.Fields {
				valType := s.inferExpr(f.Value, env, errors, registry, aff)
				if existing, ok := baseFields[f.Name]; ok {
					if !typeEqual(existing, valType) && !numCompatible(existing, valType) {
						*errors = append(*errors, TypeError{
							Code:     "E302",
							Message:  fmt.Sprintf(`field "%s" update type mismatch: expected %s, got %s`, f.Name, showType(existing), showType(valType)),
							Location: e.Loc,
						})
					}
				} else if rec.RowVar < 0 {
					*errors = append(*errors, TypeError{
						Code:     "E301",
						Message:  fmt.Sprintf(`cannot update field "%s" — not present in record type %s`, f.Name, showType(resolved)),
						Location: e.Loc,
					})
				}
			}
			return resolved
		}
		for _, f := range e.Fields {
			s.inferExpr(f.Value, env, errors, registry, aff)
		}
		return TAny

	case ast.ExprPipeline:
		leftType := s.inferExpr(e.Left, env, errors, registry, aff)
		rawRightType := s.inferExpr(e.Right, env, errors, registry, aff)
		rightType := s.freshenRowVars(rawRightType, nil)
		if fn, ok := rightType.(TFn); ok {
			resolvedArg := s.applyRowSubst(leftType)
			resolvedParam := s.applyRowSubst(fn.Param)
			if ra, ok := resolvedArg.(TRecord); ok {
				if rp, ok := resolvedParam.(TRecord); ok {
					s.unifyRecords(rp, ra, errors, e.Loc)
					goto pipelineConstraints
				}
			}
			if !typeEqual(leftType, fn.Param) && !numCompatible(leftType, fn.Param) {
				*errors = append(*errors, TypeError{
					Code:     "E304",
					Message:  fmt.Sprintf("pipeline argument: expected %s, got %s", showType(fn.Param), showType(leftType)),
					Location: e.Loc,
				})
			}
		pipelineConstraints:
			// Where-constraint checking for pipeline RHS
			if cInfo := s.resolveConstraintInfo(e.Right, env); cInfo != nil && len(cInfo.constraints) > 0 {
				bindings := make(map[string]string)
				if len(cInfo.paramTypeExps) > 0 {
					extractTypeBindings(cInfo.paramTypeExps[0], leftType, bindings)
				}
				for _, c := range cInfo.constraints {
					concreteType, ok := bindings[c.TypeParam]
					if ok && concreteType != "?" {
						var resolvedTypeArgs []string
						if len(c.TypeArgs) > 0 {
							resolvedTypeArgs = make([]string, len(c.TypeArgs))
							for i, ta := range c.TypeArgs {
								name := typeExprName(ta)
								if bound, ok := bindings[name]; ok {
									resolvedTypeArgs[i] = bound
								} else {
									resolvedTypeArgs[i] = name
								}
							}
						}
						typeArgStr := ""
						if len(resolvedTypeArgs) > 0 {
							typeArgStr = "<" + strings.Join(resolvedTypeArgs, ", ") + ">"
						}
						if !s.hasImpl(c.Interface, concreteType, resolvedTypeArgs) {
							*errors = append(*errors, TypeError{
								Code:     "E205",
								Message:  fmt.Sprintf("where constraint not satisfied: '%s' does not implement '%s%s'", concreteType, c.Interface, typeArgStr),
								Location: e.Loc,
							})
						}
					}
				}
			}
			return fn.Result
		}
		return TAny

	case ast.ExprFor:
		collType := s.inferExpr(e.Collection, env, errors, registry, aff)
		forEnv := env.extend()
		var elemType Type = TAny
		if l, ok := collType.(TList); ok {
			elemType = l.Element
		}
		s.bindPatternVars(e.Bind, forEnv, aff, elemType)
		if e.Guard != nil {
			s.inferExpr(e.Guard, forEnv, errors, registry, aff)
		}
		if e.Fold != nil {
			initType := s.inferExpr(e.Fold.Init, env, errors, registry, aff)
			forEnv.set(e.Fold.Acc, initType)
			bodyType := s.inferExpr(e.Body, forEnv, errors, registry, aff)
			if !typeEqual(bodyType, initType) && !numCompatible(bodyType, initType) {
				*errors = append(*errors, TypeError{
					Code:     "E302",
					Message:  fmt.Sprintf("for-fold body type %s doesn't match accumulator type %s", showType(bodyType), showType(initType)),
					Location: e.Loc,
				})
			}
			return initType
		}
		bodyType := s.inferExpr(e.Body, forEnv, errors, registry, aff)
		return TList{Element: bodyType}

	case ast.ExprRange:
		s.inferExpr(e.Start, env, errors, registry, aff)
		s.inferExpr(e.End, env, errors, registry, aff)
		return TList{Element: TInt}

	case ast.ExprDo:
		var lastType Type = TUnit
		doEnv := env.extend()
		for _, step := range e.Steps {
			lastType = s.inferExpr(step.Expr, doEnv, errors, registry, aff)
			if step.Bind != "" {
				doEnv.set(step.Bind, lastType)
			}
		}
		return lastType

	case ast.ExprLetPattern:
		valType := s.inferExpr(e.Value, env, errors, registry, aff)
		if e.Body == nil {
			return valType
		}
		child := env.extend()
		s.bindPatternVars(e.Pattern, child, aff, valType)
		return s.inferExpr(e.Body, child, errors, registry, aff)

	case ast.ExprInfix, ast.ExprUnary:
		return TAny
	}
	return TAny
}

// ── Pattern binding ──

func (s *checkerState) bindPatternVars(pat ast.Pattern, env *typeEnv, aff *affineCtx, subjectType Type) {
	switch p := pat.(type) {
	case ast.PatVar:
		t := subjectType
		if t == nil {
			t = TAny
		}
		env.set(p.Name, t)
		if aff != nil && isAffineType(t, aff.affineTypes) {
			aff.registerAffine(p.Name, p.Loc)
		}
	case ast.PatTuple:
		for i, elem := range p.Elements {
			var elemType Type
			if tup, ok := subjectType.(TTuple); ok && i < len(tup.Elements) {
				elemType = tup.Elements[i]
			}
			s.bindPatternVars(elem, env, aff, elemType)
		}
	case ast.PatVariant:
		ctorType, ok := env.get(p.Name)
		if ok {
			if fn, ok := ctorType.(TFn); ok {
				var fieldTypes []Type
				cur := Type(fn)
				for {
					if f, ok := cur.(TFn); ok {
						fieldTypes = append(fieldTypes, f.Param)
						cur = f.Result
					} else {
						break
					}
				}
				for i, arg := range p.Args {
					var ft Type
					if i < len(fieldTypes) {
						ft = fieldTypes[i]
					}
					s.bindPatternVars(arg, env, aff, ft)
				}
			} else {
				for _, arg := range p.Args {
					s.bindPatternVars(arg, env, aff, nil)
				}
			}
		} else {
			for _, arg := range p.Args {
				s.bindPatternVars(arg, env, aff, nil)
			}
		}
	case ast.PatRecord:
		if rec, ok := s.applyRowSubst(subjectType).(TRecord); ok {
			fieldMap := make(map[string]Type, len(rec.Fields))
			for _, f := range rec.Fields {
				fieldMap[f.Name] = f.Type
			}
			for _, pf := range p.Fields {
				fieldType := fieldMap[pf.Name]
				if pf.Pattern != nil {
					s.bindPatternVars(pf.Pattern, env, aff, fieldType)
				} else {
					t := fieldType
					if t == nil {
						t = TAny
					}
					env.set(pf.Name, t)
					if aff != nil && isAffineType(t, aff.affineTypes) {
						aff.registerAffine(pf.Name, p.Loc)
					}
				}
			}
			if p.Rest != "" && p.Rest != "_" {
				matchedNames := make(map[string]bool)
				for _, f := range p.Fields {
					matchedNames[f.Name] = true
				}
				var restFields []RecordField
				for _, f := range rec.Fields {
					if !matchedNames[f.Name] {
						restFields = append(restFields, f)
					}
				}
				env.set(p.Rest, TRecord{Fields: restFields, RowVar: rec.RowVar})
			}
		} else {
			for _, pf := range p.Fields {
				if pf.Pattern != nil {
					s.bindPatternVars(pf.Pattern, env, aff, nil)
				} else {
					env.set(pf.Name, TAny)
				}
			}
			if p.Rest != "" && p.Rest != "_" {
				env.set(p.Rest, TAny)
			}
		}
	}
}

// ── Register builtins ──

func registerPolyBuiltin(env *typeEnv, name string, varCount int, buildType func(vars []Type) Type) {
	var ids []int
	var typeVars []Type
	for i := 0; i < varCount; i++ {
		id := FreshVar()
		ids = append(ids, id)
		typeVars = append(typeVars, TVar{ID: id})
	}
	bodyType := buildType(typeVars)
	env.set(name, bodyType)
	if varCount > 0 {
		env.setScheme(name, TypeScheme{TypeVars: ids, Body: bodyType})
	}
}

func registerBuiltins(env *typeEnv) {
	// Arithmetic
	env.set("add", NewTFn(TInt, NewTFn(TInt, TInt)))
	env.set("sub", NewTFn(TInt, NewTFn(TInt, TInt)))
	env.set("mul", NewTFn(TInt, NewTFn(TInt, TInt)))
	env.set("div", NewTFn(TInt, NewTFn(TInt, TInt)))
	env.set("mod", NewTFn(TInt, NewTFn(TInt, TInt)))
	// Comparison
	env.set("lt", NewTFn(TInt, NewTFn(TInt, TBool)))
	env.set("gt", NewTFn(TInt, NewTFn(TInt, TBool)))
	env.set("lte", NewTFn(TInt, NewTFn(TInt, TBool)))
	env.set("gte", NewTFn(TInt, NewTFn(TInt, TBool)))
	// Logic
	env.set("not", NewTFn(TBool, TBool))
	env.set("negate", NewTFn(TInt, TInt))
	env.set("and", NewTFn(TBool, NewTFn(TBool, TBool)))
	env.set("or", NewTFn(TBool, NewTFn(TBool, TBool)))
	// Strings
	env.set("str.cat", NewTFn(TStr, NewTFn(TStr, TStr)))
	env.set("print", NewTFn(TStr, TUnit))

	// More string operations
	env.set("split", NewTFn(TStr, NewTFn(TStr, TList{Element: TStr})))
	env.set("join", NewTFn(TList{Element: TStr}, NewTFn(TStr, TStr)))
	env.set("trim", NewTFn(TStr, TStr))

	// Runtime dispatch helpers (desugared from for-expressions)
	env.set("__for_each", NewTFn(TAny, NewTFn(TAny, TAny)))
	env.set("__for_filter", NewTFn(TAny, NewTFn(TAny, TAny)))
	env.set("__for_fold", NewTFn(TAny, NewTFn(TAny, NewTFn(TAny, TAny))))

	// Range
	env.set("range", NewTFn(TInt, NewTFn(TInt, TList{Element: TInt})))

	// Tuple operations
	env.set("tuple.get", NewTFn(TAny, NewTFn(TInt, TAny)))

	// Polymorphic overrides
	registerPolyBuiltin(env, "eq", 1, func(v []Type) Type { return NewTFn(v[0], NewTFn(v[0], TBool)) })
	registerPolyBuiltin(env, "neq", 1, func(v []Type) Type { return NewTFn(v[0], NewTFn(v[0], TBool)) })
	registerPolyBuiltin(env, "show", 1, func(v []Type) Type { return NewTFn(v[0], TStr) })
	registerPolyBuiltin(env, "len", 1, func(v []Type) Type { return NewTFn(TList{Element: v[0]}, TInt) })
	registerPolyBuiltin(env, "head", 1, func(v []Type) Type { return NewTFn(TList{Element: v[0]}, v[0]) })
	registerPolyBuiltin(env, "tail", 1, func(v []Type) Type { return NewTFn(TList{Element: v[0]}, TList{Element: v[0]}) })
	registerPolyBuiltin(env, "cons", 1, func(v []Type) Type { return NewTFn(v[0], NewTFn(TList{Element: v[0]}, TList{Element: v[0]})) })
	registerPolyBuiltin(env, "cat", 1, func(v []Type) Type {
		return NewTFn(TList{Element: v[0]}, NewTFn(TList{Element: v[0]}, TList{Element: v[0]}))
	})
	registerPolyBuiltin(env, "rev", 1, func(v []Type) Type { return NewTFn(TList{Element: v[0]}, TList{Element: v[0]}) })
	registerPolyBuiltin(env, "get", 1, func(v []Type) Type { return NewTFn(TList{Element: v[0]}, NewTFn(TInt, v[0])) })
	registerPolyBuiltin(env, "map", 2, func(v []Type) Type {
		return NewTFn(TList{Element: v[0]}, NewTFn(NewTFn(v[0], v[1]), TList{Element: v[1]}))
	})
	registerPolyBuiltin(env, "filter", 1, func(v []Type) Type {
		return NewTFn(TList{Element: v[0]}, NewTFn(NewTFn(v[0], TBool), TList{Element: v[0]}))
	})
	registerPolyBuiltin(env, "fold", 2, func(v []Type) Type {
		return NewTFn(TList{Element: v[0]}, NewTFn(v[1], NewTFn(NewTFn(v[1], NewTFn(v[0], v[1])), v[1])))
	})
	registerPolyBuiltin(env, "flat-map", 2, func(v []Type) Type {
		return NewTFn(TList{Element: v[0]}, NewTFn(NewTFn(v[0], TList{Element: v[1]}), TList{Element: v[1]}))
	})
	registerPolyBuiltin(env, "zip", 2, func(v []Type) Type {
		return NewTFn(TList{Element: v[0]}, NewTFn(TList{Element: v[1]}, TList{Element: TTuple{Elements: []Type{v[0], v[1]}}}))
	})
	registerPolyBuiltin(env, "fst", 2, func(v []Type) Type { return NewTFn(TTuple{Elements: []Type{v[0], v[1]}}, v[0]) })
	registerPolyBuiltin(env, "snd", 2, func(v []Type) Type { return NewTFn(TTuple{Elements: []Type{v[0], v[1]}}, v[1]) })
	registerPolyBuiltin(env, "raise", 2, func(v []Type) Type { return NewTFn(v[0], v[1]) })
}

// ── Effect collection ──

type effectOpMap = map[string]string

func collectEffects(expr ast.Expr, opMap effectOpMap, handled map[string]bool) map[string]bool {
	effects := make(map[string]bool)
	walkEffects(expr, opMap, handled, effects)
	return effects
}

func walkEffects(e ast.Expr, opMap effectOpMap, handled, effects map[string]bool) {
	switch e := e.(type) {
	case ast.ExprPerform:
		var inner ast.Expr
		if app, ok := e.Expr.(ast.ExprApply); ok {
			inner = app.Fn
		} else {
			inner = e.Expr
		}
		if v, ok := inner.(ast.ExprVar); ok {
			if eff, ok := opMap[v.Name]; ok {
				if !handled[eff] {
					effects[eff] = true
				}
			}
		}
		walkEffects(e.Expr, opMap, handled, effects)
	case ast.ExprHandle:
		innerHandled := make(map[string]bool, len(handled))
		for k := range handled {
			innerHandled[k] = true
		}
		for _, arm := range e.Arms {
			if arm.Name != "return" {
				if eff, ok := opMap[arm.Name]; ok {
					innerHandled[eff] = true
				}
			}
		}
		for eff := range collectEffects(e.Expr, opMap, innerHandled) {
			effects[eff] = true
		}
		for _, arm := range e.Arms {
			walkEffects(arm.Body, opMap, handled, effects)
		}
	case ast.ExprLet:
		walkEffects(e.Value, opMap, handled, effects)
		if e.Body != nil {
			walkEffects(e.Body, opMap, handled, effects)
		}
	case ast.ExprIf:
		walkEffects(e.Cond, opMap, handled, effects)
		walkEffects(e.Then, opMap, handled, effects)
		walkEffects(e.Else, opMap, handled, effects)
	case ast.ExprLambda:
		walkEffects(e.Body, opMap, handled, effects)
	case ast.ExprApply:
		walkEffects(e.Fn, opMap, handled, effects)
		for _, a := range e.Args {
			walkEffects(a, opMap, handled, effects)
		}
	case ast.ExprMatch:
		walkEffects(e.Subject, opMap, handled, effects)
		for _, arm := range e.Arms {
			walkEffects(arm.Body, opMap, handled, effects)
		}
	case ast.ExprList:
		for _, el := range e.Elements {
			walkEffects(el, opMap, handled, effects)
		}
	case ast.ExprTuple:
		for _, el := range e.Elements {
			walkEffects(el, opMap, handled, effects)
		}
	case ast.ExprDo:
		for _, step := range e.Steps {
			walkEffects(step.Expr, opMap, handled, effects)
		}
	case ast.ExprFor:
		walkEffects(e.Collection, opMap, handled, effects)
		if e.Guard != nil {
			walkEffects(e.Guard, opMap, handled, effects)
		}
		if e.Fold != nil {
			walkEffects(e.Fold.Init, opMap, handled, effects)
		}
		walkEffects(e.Body, opMap, handled, effects)
	case ast.ExprRange:
		walkEffects(e.Start, opMap, handled, effects)
		walkEffects(e.End, opMap, handled, effects)
	case ast.ExprPipeline:
		walkEffects(e.Left, opMap, handled, effects)
		walkEffects(e.Right, opMap, handled, effects)
	case ast.ExprInfix:
		walkEffects(e.Left, opMap, handled, effects)
		walkEffects(e.Right, opMap, handled, effects)
	case ast.ExprUnary:
		walkEffects(e.Operand, opMap, handled, effects)
	case ast.ExprRecord:
		for _, f := range e.Fields {
			walkEffects(f.Value, opMap, handled, effects)
		}
	case ast.ExprRecordUpdate:
		walkEffects(e.Base, opMap, handled, effects)
		for _, f := range e.Fields {
			walkEffects(f.Value, opMap, handled, effects)
		}
	case ast.ExprFieldAccess:
		walkEffects(e.Object, opMap, handled, effects)
	case ast.ExprBorrow:
		walkEffects(e.Expr, opMap, handled, effects)
	case ast.ExprClone:
		walkEffects(e.Expr, opMap, handled, effects)
	case ast.ExprDiscard:
		walkEffects(e.Expr, opMap, handled, effects)
	}
}

// ── Effect aliases ──

type effectAliasEntry struct {
	params  []string
	effects []ast.EffectRef
}

func effectRefKey(ref ast.EffectRef) string {
	if len(ref.Args) > 0 {
		return ref.Name + "<" + strings.Join(ref.Args, ", ") + ">"
	}
	return ref.Name
}

func substituteEffectRefs(effects []ast.EffectRef, params, args []string) []ast.EffectRef {
	subst := make(map[string]string, len(params))
	for i := 0; i < len(params) && i < len(args); i++ {
		subst[params[i]] = args[i]
	}
	result := make([]ast.EffectRef, len(effects))
	for i, ref := range effects {
		name := ref.Name
		if s, ok := subst[name]; ok {
			name = s
		}
		refArgs := make([]string, len(ref.Args))
		for j, a := range ref.Args {
			if s, ok := subst[a]; ok {
				refArgs[j] = s
			} else {
				refArgs[j] = a
			}
		}
		result[i] = ast.EffectRef{Name: name, Args: refArgs}
	}
	return result
}

func expandEffects(effects []ast.EffectRef, aliases map[string]*effectAliasEntry) []ast.EffectRef {
	resultKeys := make(map[string]bool)
	var result []ast.EffectRef
	seen := make(map[string]bool)
	var expand func(ref ast.EffectRef)
	expand = func(ref ast.EffectRef) {
		alias, ok := aliases[ref.Name]
		if ok && !seen[ref.Name] {
			seen[ref.Name] = true
			substituted := substituteEffectRefs(alias.effects, alias.params, ref.Args)
			for _, eff := range substituted {
				expand(eff)
			}
			delete(seen, ref.Name)
		} else {
			key := effectRefKey(ref)
			if !resultKeys[key] {
				resultKeys[key] = true
				result = append(result, ref)
			}
		}
	}
	for _, eff := range effects {
		expand(eff)
	}
	return result
}

func resolveEffectSubtraction(effects, subtracted []ast.EffectRef, errors *[]TypeError, loc token.Loc) []ast.EffectRef {
	if len(subtracted) == 0 {
		return effects
	}
	if len(effects) == 0 {
		for _, eff := range subtracted {
			*errors = append(*errors, TypeError{
				Code:     "E500",
				Message:  fmt.Sprintf("cannot subtract effect '%s' from empty row <>", effectRefKey(eff)),
				Location: loc,
			})
		}
		return nil
	}
	effectKeySet := make(map[string]bool)
	for _, eff := range effects {
		effectKeySet[effectRefKey(eff)] = true
	}
	for _, eff := range subtracted {
		if !effectKeySet[effectRefKey(eff)] {
			*errors = append(*errors, TypeError{
				Code:    "E501",
				Message: fmt.Sprintf("cannot subtract effect '%s' from row <%s> ('%s' is not present)", effectRefKey(eff), effectKeysString(effects), effectRefKey(eff)),
			})
		}
	}
	subtractKeySet := make(map[string]bool)
	for _, eff := range subtracted {
		subtractKeySet[effectRefKey(eff)] = true
	}
	var result []ast.EffectRef
	for _, eff := range effects {
		if !subtractKeySet[effectRefKey(eff)] {
			result = append(result, eff)
		}
	}
	return result
}

func effectKeysString(effects []ast.EffectRef) string {
	keys := make([]string, len(effects))
	for i, eff := range effects {
		keys[i] = effectRefKey(eff)
	}
	return strings.Join(keys, ", ")
}

// ── Interface/impl registries ──

type interfaceEntry struct {
	name       string
	typeParams []string
	supers     []string
	methods    []ast.MethodSig
}

// ── Entry point ──

// TypeCheck validates a program and returns any type errors.
func TypeCheck(program *ast.Program) []TypeError {
	var errors []TypeError

	s := &checkerState{
		fnRefInfo:       make(map[string]*fnRefInfo),
		varRefinements:  make(map[string]string),
		fnConstraints:   make(map[string]*fnConstraintInfo),
		fnParamTypeExps: make(map[string][]ast.TypeExpr),
		rowSubst:        make(map[int]*rowSubstEntry),
		namedRowVars:    make(map[string]int),
		typeSubst:       make(map[int]Type),
		knownTypes:      make(map[string]bool),
		fnTypeParamVs:   make(map[string]map[string]Type),
		implRegistry:    make(map[string][]implEntry),
	}
	ResetVarCounter()

	env := newTypeEnv(nil)
	registry := make(map[string][]string)    // type name → variant names
	opMap := make(effectOpMap)                // op name → effect name
	effectAliases := make(map[string]*effectAliasEntry)
	interfaces := make(map[string]*interfaceEntry)
	affineTypes := make(map[string]bool)
	cloneableTypes := make(map[string]bool)

	registerBuiltins(env)

	// Built-in interfaces
	// Built-in interface method signatures (used for method checking in impl blocks)
	selfType := ast.TypeName{Name: "Self"}
	tTypeName := ast.TypeName{Name: "T"}
	noEffects := []ast.EffectRef{}
	noLoc := token.Loc{}
	_ = noLoc
	builtinInterfaceDefs := []struct {
		name       string
		typeParams []string
		supers     []string
		methods    []ast.MethodSig
	}{
		{"Clone", nil, nil, []ast.MethodSig{{Name: "clone", Sig: ast.TypeSig{Params: []ast.TypeSigParam{{Name: "self", Type: selfType}}, Effects: noEffects, ReturnType: selfType}}}},
		{"Show", nil, nil, []ast.MethodSig{{Name: "show", Sig: ast.TypeSig{Params: []ast.TypeSigParam{{Name: "self", Type: selfType}}, Effects: noEffects, ReturnType: ast.TypeName{Name: "Str"}}}}},
		{"Eq", nil, nil, []ast.MethodSig{{Name: "eq", Sig: ast.TypeSig{Params: []ast.TypeSigParam{{Name: "a", Type: selfType}, {Name: "b", Type: selfType}}, Effects: noEffects, ReturnType: ast.TypeName{Name: "Bool"}}}}},
		{"Ord", nil, []string{"Eq"}, []ast.MethodSig{{Name: "cmp", Sig: ast.TypeSig{Params: []ast.TypeSigParam{{Name: "a", Type: selfType}, {Name: "b", Type: selfType}}, Effects: noEffects, ReturnType: ast.TypeName{Name: "Ordering"}}}}},
		{"Default", nil, nil, []ast.MethodSig{{Name: "default", Sig: ast.TypeSig{Params: nil, Effects: noEffects, ReturnType: selfType}}}},
		{"Into", []string{"T"}, nil, []ast.MethodSig{{Name: "into", Sig: ast.TypeSig{Params: []ast.TypeSigParam{{Name: "self", Type: selfType}}, Effects: noEffects, ReturnType: tTypeName}}}},
		{"From", []string{"T"}, nil, []ast.MethodSig{{Name: "from", Sig: ast.TypeSig{Params: []ast.TypeSigParam{{Name: "val", Type: tTypeName}}, Effects: noEffects, ReturnType: selfType}}}},
	}
	for _, bi := range builtinInterfaceDefs {
		interfaces[bi.name] = &interfaceEntry{name: bi.name, typeParams: bi.typeParams, supers: bi.supers, methods: bi.methods}
		s.implRegistry[bi.name] = nil
	}
	// Primitive impls
	for _, prim := range []string{"Int", "Rat", "Bool", "Str", "Unit"} {
		for _, iface := range []string{"Clone", "Show", "Eq", "Default"} {
			s.implRegistry[iface] = append(s.implRegistry[iface], implEntry{interface_: iface, forType: prim, loc: token.Loc{}})
		}
	}
	for _, prim := range []string{"Int", "Rat", "Str"} {
		s.implRegistry["Ord"] = append(s.implRegistry["Ord"], implEntry{interface_: "Ord", forType: prim, loc: token.Loc{}})
	}

	// Register interface method builtins
	registerPolyBuiltin(env, "clone", 1, func(v []Type) Type { return NewTFn(v[0], v[0]) })
	registerPolyBuiltin(env, "cmp", 1, func(v []Type) Type { return NewTFn(v[0], NewTFn(v[0], TGeneric{Name: "Ordering"})) })
	registerPolyBuiltin(env, "default", 1, func(v []Type) Type { return NewTFn(TUnit, v[0]) })
	registerPolyBuiltin(env, "into", 2, func(v []Type) Type { return NewTFn(v[0], v[1]) })
	registerPolyBuiltin(env, "from", 2, func(v []Type) Type { return NewTFn(v[0], v[1]) })

	// Pre-pass: collect known type names
	for _, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopTypeDecl:
			s.knownTypes[d.Name] = true
			for _, v := range d.Variants {
				s.knownTypes[v.Name] = true
			}
		case ast.TopInterfaceDecl:
			s.knownTypes[d.Name] = true
		case ast.TopEffectDecl:
			s.knownTypes[d.Name] = true
		case ast.TopEffectAlias:
			s.knownTypes[d.Name] = true
		}
	}
	s.knownTypes["Ordering"] = true
	s.knownTypes["Self"] = true

	// First pass: register declarations
	for _, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopModDecl:
			continue
		case ast.TopUseDecl:
			for _, imp := range d.Imports {
				name := imp.Name
				if imp.Alias != "" {
					name = imp.Alias
				}
				env.set(name, TAny)
				if imp.Alias != "" {
					if ea, ok := effectAliases[imp.Name]; ok {
						effectAliases[imp.Alias] = ea
					}
				}
			}
			continue
		case ast.TopTestDecl:
			continue
		case ast.TopTypeDecl:
			variants := make([]string, len(d.Variants))
			for i, v := range d.Variants {
				variants[i] = v.Name
			}
			registry[d.Name] = variants
			if d.Affine {
				affineTypes[d.Name] = true
			}
			if len(d.Deriving) > 0 {
				for _, iface := range d.Deriving {
					if iface == "Clone" {
						cloneableTypes[d.Name] = true
					}
				}
			}
			for _, v := range d.Variants {
				if len(v.Fields) == 0 {
					env.set(v.Name, TGeneric{Name: d.Name})
				} else {
					var ct Type = TGeneric{Name: d.Name}
					for i := len(v.Fields) - 1; i >= 0; i-- {
						ct = NewTFn(resolveType(v.Fields[i], s), ct)
					}
					env.set(v.Name, ct)
				}
			}
			if len(d.Deriving) > 0 {
				for _, ifaceName := range d.Deriving {
					if !derivableInterfaces[ifaceName] {
						errors = append(errors, TypeError{Code: "E201", Message: fmt.Sprintf("cannot derive '%s': not a derivable interface", ifaceName), Location: d.Loc})
						continue
					}
					if _, ok := interfaces[ifaceName]; !ok {
						errors = append(errors, TypeError{Code: "E201", Message: fmt.Sprintf("unknown interface '%s'", ifaceName), Location: d.Loc})
						continue
					}
					s.implRegistry[ifaceName] = append(s.implRegistry[ifaceName], implEntry{interface_: ifaceName, forType: d.Name, loc: d.Loc})
				}
			}
		case ast.TopInterfaceDecl:
			if _, ok := interfaces[d.Name]; ok {
				errors = append(errors, TypeError{Code: "E200", Message: fmt.Sprintf("duplicate interface declaration '%s'", d.Name), Location: d.Loc})
			} else {
				for _, sup := range d.Supers {
					if _, ok := interfaces[sup]; !ok {
						errors = append(errors, TypeError{Code: "E200", Message: fmt.Sprintf("unknown superinterface '%s' in interface '%s'", sup, d.Name), Location: d.Loc})
					}
				}
				interfaces[d.Name] = &interfaceEntry{name: d.Name, typeParams: d.TypeParams, supers: d.Supers, methods: d.Methods}
				if s.implRegistry[d.Name] == nil {
					s.implRegistry[d.Name] = nil
				}
				for _, m := range d.Methods {
					retType := resolveType(m.Sig.ReturnType, s)
					var fnType Type
					if len(m.Sig.Params) == 0 {
						fnType = NewTFn(TUnit, retType)
					} else {
						fnType = retType
						for i := len(m.Sig.Params) - 1; i >= 0; i-- {
							fnType = NewTFn(resolveType(m.Sig.Params[i].Type, s), fnType)
						}
					}
					env.set(m.Name, fnType)
				}
			}
		case ast.TopImplBlock:
			ifaceName := d.Interface
			iface, ok := interfaces[ifaceName]
			if !ok {
				errors = append(errors, TypeError{Code: "E200", Message: fmt.Sprintf("unknown interface '%s'", ifaceName), Location: d.Loc})
			} else {
				forTypeName := typeExprName(d.ForType)
				implTypeArgs := make([]string, len(d.TypeArgs))
				for i, ta := range d.TypeArgs {
					implTypeArgs[i] = typeExprName(ta)
				}
				// Coherence check
				isDup := false
				for _, e := range s.implRegistry[ifaceName] {
					if e.forType == forTypeName && len(e.typeArgs) == len(implTypeArgs) {
						match := true
						for i := range e.typeArgs {
							if e.typeArgs[i] != implTypeArgs[i] {
								match = false
								break
							}
						}
						if match {
							isDup = true
							break
						}
					}
				}
				if isDup {
					typeArgStr := ""
					if len(implTypeArgs) > 0 {
						typeArgStr = "<" + strings.Join(implTypeArgs, ", ") + ">"
					}
					errors = append(errors, TypeError{Code: "E202", Message: fmt.Sprintf("duplicate impl of '%s%s' for '%s' (coherence violation)", ifaceName, typeArgStr, forTypeName), Location: d.Loc})
				} else {
					s.implRegistry[ifaceName] = append(s.implRegistry[ifaceName], implEntry{interface_: ifaceName, forType: forTypeName, typeArgs: implTypeArgs, loc: d.Loc})
				}
				// Check methods
				provided := make(map[string]bool, len(d.Methods))
				for _, m := range d.Methods {
					provided[m.Name] = true
				}
				if iface != nil {
					for _, m := range iface.methods {
						if !provided[m.Name] {
							errors = append(errors, TypeError{Code: "E203", Message: fmt.Sprintf("impl of '%s' for '%s' is missing method '%s'", ifaceName, forTypeName, m.Name), Location: d.Loc})
						}
					}
					expected := make(map[string]bool, len(iface.methods))
					for _, m := range iface.methods {
						expected[m.Name] = true
					}
					for _, m := range d.Methods {
						if !expected[m.Name] {
							errors = append(errors, TypeError{Code: "E203", Message: fmt.Sprintf("impl of '%s' for '%s' provides unexpected method '%s'", ifaceName, forTypeName, m.Name), Location: d.Loc})
						}
					}
					for _, sup := range iface.supers {
						hasSup := false
						for _, e := range s.implRegistry[sup] {
							if e.forType == forTypeName {
								hasSup = true
								break
							}
						}
						if !hasSup {
							errors = append(errors, TypeError{Code: "E204", Message: fmt.Sprintf("impl of '%s' for '%s' requires impl of superinterface '%s'", ifaceName, forTypeName, sup), Location: d.Loc})
						}
					}
				}
				// Blanket From → Into
				if ifaceName == "From" && len(d.TypeArgs) > 0 {
					sourceType := typeExprName(d.TypeArgs[0])
					if s.implRegistry["Into"] == nil {
						s.implRegistry["Into"] = nil
					}
					hasInto := false
					for _, e := range s.implRegistry["Into"] {
						if e.forType == sourceType {
							hasInto = true
							break
						}
					}
					if !hasInto {
						s.implRegistry["Into"] = append(s.implRegistry["Into"], implEntry{interface_: "Into", forType: sourceType, typeArgs: []string{forTypeName}, loc: d.Loc})
					}
				}
			}
		case ast.TopEffectAlias:
			expandedBase := expandEffects(d.Effects, effectAliases)
			expandedSub := expandEffects(d.Subtracted, effectAliases)
			resolved := resolveEffectSubtraction(expandedBase, expandedSub, &errors, d.Loc)
			effectAliases[d.Name] = &effectAliasEntry{params: d.Params, effects: resolved}
		case ast.TopEffectDecl:
			env.set(d.Name, TGeneric{Name: "effect"})
			for _, op := range d.Ops {
				var paramType Type = TUnit
				if len(op.Sig.Params) > 0 {
					paramType = resolveType(op.Sig.Params[0].Type, s)
				}
				retType := resolveType(op.Sig.ReturnType, s)
				env.set(op.Name, NewTFn(paramType, retType))
				opMap[op.Name] = d.Name
			}
		case ast.TopDefinition:
			s.namedRowVars = make(map[string]int)
			// Collect type params
			sigTypeParams := make(map[string]bool)
			for _, p := range d.Sig.Params {
				for tp := range s.collectTypeParams(p.Type) {
					sigTypeParams[tp] = true
				}
			}
			for tp := range s.collectTypeParams(d.Sig.ReturnType) {
				sigTypeParams[tp] = true
			}
			for _, c := range d.Constraints {
				if s.isTypeParam(c.TypeParam) {
					sigTypeParams[c.TypeParam] = true
				}
			}

			if len(sigTypeParams) > 0 {
				varMapping := make(map[string]Type, len(sigTypeParams))
				var varIds []int
				for tp := range sigTypeParams {
					id := FreshVar()
					varIds = append(varIds, id)
					varMapping[tp] = TVar{ID: id}
				}
				s.fnTypeParamVs[d.Name] = varMapping
				retType := resolveTypeWithVars(d.Sig.ReturnType, varMapping, s)
				var fnType Type
				if len(d.Sig.Params) == 0 {
					fnType = NewTFn(TUnit, retType)
				} else {
					fnType = retType
					for i := len(d.Sig.Params) - 1; i >= 0; i-- {
						fnType = NewTFn(resolveTypeWithVars(d.Sig.Params[i].Type, varMapping, s), fnType)
					}
				}
				env.set(d.Name, fnType)
				env.setScheme(d.Name, TypeScheme{TypeVars: varIds, Body: fnType})
			} else {
				retType := resolveType(d.Sig.ReturnType, s)
				var fnType Type
				if len(d.Sig.Params) == 0 {
					fnType = NewTFn(TUnit, retType)
				} else {
					fnType = retType
					for i := len(d.Sig.Params) - 1; i >= 0; i-- {
						fnType = NewTFn(resolveType(d.Sig.Params[i].Type, s), fnType)
					}
				}
				env.set(d.Name, fnType)
			}
			// Refinement info
			paramPreds := make([]string, len(d.Sig.Params))
			for i, p := range d.Sig.Params {
				if tr, ok := p.Type.(ast.TypeRefined); ok {
					paramPreds[i] = tr.Predicate
				}
			}
			var returnPred string
			if tr, ok := d.Sig.ReturnType.(ast.TypeRefined); ok {
				returnPred = tr.Predicate
			}
			hasPreds := returnPred != ""
			for _, p := range paramPreds {
				if p != "" {
					hasPreds = true
					break
				}
			}
			if hasPreds {
				s.fnRefInfo[d.Name] = &fnRefInfo{paramPreds: paramPreds, returnPred: returnPred}
			}
			// Param type exprs
			paramTEs := make([]ast.TypeExpr, len(d.Sig.Params))
			for i, p := range d.Sig.Params {
				paramTEs[i] = p.Type
			}
			s.fnParamTypeExps[d.Name] = paramTEs
			// Where-constraints
			if len(d.Constraints) > 0 {
				s.fnConstraints[d.Name] = &fnConstraintInfo{
					constraints:   d.Constraints,
					paramTypeExps: paramTEs,
				}
			}
		case ast.TopExternDecl:
			s.namedRowVars = make(map[string]int)
			// Validate effect annotations
			builtinEffects := map[string]bool{"io": true, "exn": true, "async": true}
			for _, eff := range d.Sig.Effects {
				if !builtinEffects[eff.Name] && effectAliases[eff.Name] == nil {
					if len(eff.Name) == 1 && eff.Name[0] >= 'a' && eff.Name[0] <= 'z' {
						continue
					}
					errors = append(errors, TypeError{
						Code:     "E803",
						Message:  fmt.Sprintf("extern function '%s' cannot declare user-defined effect '%s' (only io, exn, async allowed)", d.Name, eff.Name),
						Location: d.Loc,
					})
				}
			}
			retType := resolveType(d.Sig.ReturnType, s)
			var fnType Type
			if len(d.Sig.Params) == 0 {
				fnType = NewTFn(TUnit, retType)
			} else {
				fnType = retType
				for i := len(d.Sig.Params) - 1; i >= 0; i-- {
					fnType = NewTFn(resolveType(d.Sig.Params[i].Type, s), fnType)
				}
			}
			env.set(d.Name, fnType)
			// Refinement info for externs
			paramPreds := make([]string, len(d.Sig.Params))
			for i, p := range d.Sig.Params {
				if tr, ok := p.Type.(ast.TypeRefined); ok {
					paramPreds[i] = tr.Predicate
				}
			}
			var returnPred string
			if tr, ok := d.Sig.ReturnType.(ast.TypeRefined); ok {
				returnPred = tr.Predicate
			}
			hasPreds := returnPred != ""
			for _, p := range paramPreds {
				if p != "" {
					hasPreds = true
					break
				}
			}
			if hasPreds {
				s.fnRefInfo[d.Name] = &fnRefInfo{paramPreds: paramPreds, returnPred: returnPred}
			}
			paramTEs := make([]ast.TypeExpr, len(d.Sig.Params))
			for i, p := range d.Sig.Params {
				paramTEs[i] = p.Type
			}
			s.fnParamTypeExps[d.Name] = paramTEs
		}
	}

	// Second pass: check function bodies
	for _, tl := range program.TopLevels {
		def, ok := tl.(ast.TopDefinition)
		if !ok {
			continue
		}
		bodyEnv := env.extend()
		aff := newAffineCtx(affineTypes, cloneableTypes, s.implRegistry)
		var affineParams []string

		// Propagate where-constraints
		if fnCInfo, ok := s.fnConstraints[def.Name]; ok {
			bodyEnv.setConstraint(def.Name, fnCInfo)
		}

		s.varRefinements = make(map[string]string)
		s.pathConditions = nil

		bodyVarMapping := s.fnTypeParamVs[def.Name]
		bodyResolve := func(te ast.TypeExpr) Type {
			if bodyVarMapping != nil {
				return resolveTypeWithVars(te, bodyVarMapping, s)
			}
			return resolveType(te, s)
		}

		for _, p := range def.Sig.Params {
			pt := bodyResolve(p.Type)
			bodyEnv.set(p.Name, pt)
			if isAffineType(pt, affineTypes) {
				aff.registerAffine(p.Name, def.Loc)
				affineParams = append(affineParams, p.Name)
			}
			if tr, ok := p.Type.(ast.TypeRefined); ok {
				s.varRefinements[p.Name] = tr.Predicate
			}
		}

		bodyType := s.inferExpr(def.Body, bodyEnv, &errors, registry, aff)
		aff.checkUnconsumed(affineParams, def.Loc, &errors)

		expectedRet := bodyResolve(def.Sig.ReturnType)

		if containsBorrow(expectedRet) {
			errors = append(errors, TypeError{
				Code:     "E603",
				Message:  fmt.Sprintf("function '%s' cannot return a borrow type '%s' (borrows cannot escape their scope)", def.Name, showType(expectedRet)),
				Location: def.Loc,
			})
		}
		for _, p := range def.Sig.Params {
			pt := bodyResolve(p.Type)
			if _, ok := pt.(TBorrow); !ok && containsBorrow(pt) {
				errors = append(errors, TypeError{
					Code:     "E603",
					Message:  fmt.Sprintf("parameter '%s' contains a borrow in a compound type '%s' (borrows cannot be stored in data structures)", p.Name, showType(pt)),
					Location: def.Loc,
				})
			}
		}

		unitReturn := false
		if p, ok := expectedRet.(TPrimitive); ok && p.Name == "unit" {
			unitReturn = true
		}
		if !unitReturn {
			resolvedBody := s.applyTypeSubst(s.applyRowSubst(bodyType))
			resolvedExpected := s.applyTypeSubst(s.applyRowSubst(expectedRet))
			if rb, ok := resolvedBody.(TRecord); ok {
				if re, ok := resolvedExpected.(TRecord); ok {
					s.unifyRecords(re, rb, &errors, def.Loc)
					goto afterReturnCheck
				}
			}
			if !typeEqual(bodyType, expectedRet) && !numCompatible(bodyType, expectedRet) {
				errors = append(errors, TypeError{
					Code:     "E307",
					Message:  fmt.Sprintf("function '%s' returns %s, expected %s", def.Name, showType(bodyType), showType(expectedRet)),
					Location: def.Loc,
				})
			}
		}
	afterReturnCheck:

		// Return refinement checking
		if tr, ok := def.Sig.ReturnType.(ast.TypeRefined); ok {
			s.checkReturnRefinementPerBranch(def.Body, tr.Predicate, &errors, def.Sig.Params)
		}

		// Effect checking
		expandedEffects := expandEffects(def.Sig.Effects, effectAliases)
		expandedSubtracted := expandEffects(def.Sig.Subtracted, effectAliases)
		resolvedEffects := resolveEffectSubtraction(expandedEffects, expandedSubtracted, &errors, def.Loc)
		bodyEffects := collectEffects(def.Body, opMap, make(map[string]bool))
		declaredEffects := make(map[string]bool, len(resolvedEffects))
		for _, r := range resolvedEffects {
			declaredEffects[r.Name] = true
		}
		for eff := range bodyEffects {
			if !declaredEffects[eff] {
				errors = append(errors, TypeError{
					Code:     "W401",
					Message:  fmt.Sprintf("function '%s' performs effect '%s' not declared in signature", def.Name, eff),
					Location: def.Loc,
				})
			}
		}
	}

	return errors
}
