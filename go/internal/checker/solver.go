package checker

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ── Predicate AST ──

type Pred interface {
	predNode()
}

type PVar struct{ Name string }
type PLit struct{ Value int64 }
type PAdd struct{ Left, Right Pred }
type PSub struct{ Left, Right Pred }
type PMul struct{ Left, Right Pred }
type PNeg struct{ Operand Pred }
type PCmp struct {
	Op          string // "<", ">", "<=", ">=", "==", "!="
	Left, Right Pred
}
type PAnd struct{ Left, Right Pred }
type POr struct{ Left, Right Pred }
type PNot struct{ Operand Pred }
type PTrue struct{}
type PFalse struct{}

func (PVar) predNode()   {}
func (PLit) predNode()   {}
func (PAdd) predNode()   {}
func (PSub) predNode()   {}
func (PMul) predNode()   {}
func (PNeg) predNode()   {}
func (PCmp) predNode()   {}
func (PAnd) predNode()   {}
func (POr) predNode()    {}
func (PNot) predNode()   {}
func (PTrue) predNode()  {}
func (PFalse) predNode() {}

// ── Tokenizer ──

type pToken struct {
	tag   string // "num", "ident", "op", "lparen", "rparen", "eof"
	sval  string
	nval  int64
}

func tokenizePred(s string) []pToken {
	var tokens []pToken
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == ' ' || ch == '\t' || ch == '\n' {
			i++
			continue
		}
		if ch >= '0' && ch <= '9' {
			start := i
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			var n int64
			for _, c := range s[start:i] {
				n = n*10 + int64(c-'0')
			}
			tokens = append(tokens, pToken{tag: "num", nval: n})
			continue
		}
		if unicode.IsLetter(rune(ch)) || ch == '_' {
			start := i
			for i < len(s) && (unicode.IsLetter(rune(s[i])) || unicode.IsDigit(rune(s[i])) || s[i] == '_' || s[i] == '-') {
				i++
			}
			tokens = append(tokens, pToken{tag: "ident", sval: s[start:i]})
			continue
		}
		if ch == '(' {
			tokens = append(tokens, pToken{tag: "lparen"})
			i++
			continue
		}
		if ch == ')' {
			tokens = append(tokens, pToken{tag: "rparen"})
			i++
			continue
		}
		// Two-char operators
		if i+1 < len(s) {
			two := s[i : i+2]
			switch two {
			case "<=", ">=", "==", "!=", "&&", "||":
				tokens = append(tokens, pToken{tag: "op", sval: two})
				i += 2
				continue
			}
		}
		// Single-char operators
		switch ch {
		case '+', '-', '*', '<', '>', '!':
			tokens = append(tokens, pToken{tag: "op", sval: string(ch)})
			i++
			continue
		}
		i++ // skip unknown
	}
	tokens = append(tokens, pToken{tag: "eof"})
	return tokens
}

// ── Parser ──

type predParser struct {
	tokens []pToken
	pos    int
}

func (p *predParser) peek() pToken {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return pToken{tag: "eof"}
}

func (p *predParser) advance() pToken {
	t := p.peek()
	p.pos++
	return t
}

func (p *predParser) atOp(v string) bool {
	t := p.peek()
	return t.tag == "op" && t.sval == v
}

func (p *predParser) parse() Pred {
	return p.parseOr()
}

func (p *predParser) parseOr() Pred {
	left := p.parseAnd()
	for p.atOp("||") {
		p.advance()
		left = POr{Left: left, Right: p.parseAnd()}
	}
	return left
}

func (p *predParser) parseAnd() Pred {
	left := p.parseNot()
	for p.atOp("&&") {
		p.advance()
		left = PAnd{Left: left, Right: p.parseNot()}
	}
	return left
}

func (p *predParser) parseNot() Pred {
	if p.atOp("!") {
		p.advance()
		return PNot{Operand: p.parseNot()}
	}
	return p.parseCmp()
}

func (p *predParser) parseCmp() Pred {
	// Handle implicit v: if first token is a comparison operator
	t := p.peek()
	if t.tag == "op" && isCmpOp(t.sval) {
		op := p.advance().sval
		right := p.parseAddSub()
		return PCmp{Op: op, Left: PVar{Name: "v"}, Right: right}
	}
	left := p.parseAddSub()
	t2 := p.peek()
	if t2.tag == "op" && isCmpOp(t2.sval) {
		op := p.advance().sval
		right := p.parseAddSub()
		return PCmp{Op: op, Left: left, Right: right}
	}
	return left
}

func isCmpOp(s string) bool {
	switch s {
	case "<", ">", "<=", ">=", "==", "!=":
		return true
	}
	return false
}

func (p *predParser) parseAddSub() Pred {
	left := p.parseMul()
	for p.atOp("+") || p.atOp("-") {
		op := p.advance().sval
		right := p.parseMul()
		if op == "+" {
			left = PAdd{Left: left, Right: right}
		} else {
			left = PSub{Left: left, Right: right}
		}
	}
	return left
}

func (p *predParser) parseMul() Pred {
	left := p.parseUnary()
	for p.atOp("*") {
		p.advance()
		left = PMul{Left: left, Right: p.parseUnary()}
	}
	return left
}

func (p *predParser) parseUnary() Pred {
	if p.atOp("-") {
		p.advance()
		return PNeg{Operand: p.parseAtom()}
	}
	return p.parseAtom()
}

func (p *predParser) parseAtom() Pred {
	t := p.peek()
	switch t.tag {
	case "num":
		p.advance()
		return PLit{Value: t.nval}
	case "ident":
		p.advance()
		if t.sval == "true" {
			return PTrue{}
		}
		if t.sval == "false" {
			return PFalse{}
		}
		return PVar{Name: t.sval}
	case "lparen":
		p.advance()
		inner := p.parseOr()
		if p.peek().tag == "rparen" {
			p.advance()
		}
		return inner
	}
	return PTrue{}
}

func parsePredicate(s string) Pred {
	pp := &predParser{tokens: tokenizePred(s)}
	return pp.parse()
}

// ── Variable substitution ──

func substituteVar(p Pred, from, to string) Pred {
	switch p := p.(type) {
	case PVar:
		if p.Name == from {
			return PVar{Name: to}
		}
		return p
	case PLit, PTrue, PFalse:
		return p
	case PAdd:
		return PAdd{Left: substituteVar(p.Left, from, to), Right: substituteVar(p.Right, from, to)}
	case PSub:
		return PSub{Left: substituteVar(p.Left, from, to), Right: substituteVar(p.Right, from, to)}
	case PMul:
		return PMul{Left: substituteVar(p.Left, from, to), Right: substituteVar(p.Right, from, to)}
	case PNeg:
		return PNeg{Operand: substituteVar(p.Operand, from, to)}
	case PCmp:
		return PCmp{Op: p.Op, Left: substituteVar(p.Left, from, to), Right: substituteVar(p.Right, from, to)}
	case PAnd:
		return PAnd{Left: substituteVar(p.Left, from, to), Right: substituteVar(p.Right, from, to)}
	case POr:
		return POr{Left: substituteVar(p.Left, from, to), Right: substituteVar(p.Right, from, to)}
	case PNot:
		return PNot{Operand: substituteVar(p.Operand, from, to)}
	}
	return p
}

// ── Direct evaluation ──

func evalArith(p Pred, env map[string]int64) (int64, bool) {
	switch p := p.(type) {
	case PLit:
		return p.Value, true
	case PVar:
		v, ok := env[p.Name]
		return v, ok
	case PAdd:
		l, ok1 := evalArith(p.Left, env)
		r, ok2 := evalArith(p.Right, env)
		if ok1 && ok2 {
			return l + r, true
		}
		return 0, false
	case PSub:
		l, ok1 := evalArith(p.Left, env)
		r, ok2 := evalArith(p.Right, env)
		if ok1 && ok2 {
			return l - r, true
		}
		return 0, false
	case PMul:
		l, ok1 := evalArith(p.Left, env)
		r, ok2 := evalArith(p.Right, env)
		if ok1 && ok2 {
			return l * r, true
		}
		return 0, false
	case PNeg:
		o, ok := evalArith(p.Operand, env)
		if ok {
			return -o, true
		}
		return 0, false
	}
	return 0, false
}

func evalPred(p Pred, env map[string]int64) (bool, bool) { // (result, ok)
	switch p := p.(type) {
	case PTrue:
		return true, true
	case PFalse:
		return false, true
	case PAnd:
		l, ok1 := evalPred(p.Left, env)
		r, ok2 := evalPred(p.Right, env)
		if ok1 && ok2 {
			return l && r, true
		}
		return false, false
	case POr:
		l, ok1 := evalPred(p.Left, env)
		r, ok2 := evalPred(p.Right, env)
		if ok1 && ok2 {
			return l || r, true
		}
		return false, false
	case PNot:
		o, ok := evalPred(p.Operand, env)
		if ok {
			return !o, true
		}
		return false, false
	case PCmp:
		l, ok1 := evalArith(p.Left, env)
		r, ok2 := evalArith(p.Right, env)
		if !ok1 || !ok2 {
			return false, false
		}
		switch p.Op {
		case "<":
			return l < r, true
		case ">":
			return l > r, true
		case "<=":
			return l <= r, true
		case ">=":
			return l >= r, true
		case "==":
			return l == r, true
		case "!=":
			return l != r, true
		}
	}
	return false, false
}

// ── Linear expressions ──

type linExpr struct {
	vars     map[string]int64
	constant int64
}

func linConst(c int64) linExpr {
	return linExpr{vars: map[string]int64{}, constant: c}
}

func linVar_(name string) linExpr {
	return linExpr{vars: map[string]int64{name: 1}, constant: 0}
}

func linAdd(a, b linExpr) linExpr {
	vars := make(map[string]int64, len(a.vars))
	for k, v := range a.vars {
		vars[k] = v
	}
	for k, v := range b.vars {
		vars[k] += v
	}
	return linExpr{vars: vars, constant: a.constant + b.constant}
}

func linSub(a, b linExpr) linExpr {
	vars := make(map[string]int64, len(a.vars))
	for k, v := range a.vars {
		vars[k] = v
	}
	for k, v := range b.vars {
		vars[k] -= v
	}
	return linExpr{vars: vars, constant: a.constant - b.constant}
}

func linScale(a linExpr, k int64) linExpr {
	vars := make(map[string]int64, len(a.vars))
	for v, c := range a.vars {
		vars[v] = c * k
	}
	return linExpr{vars: vars, constant: a.constant * k}
}

func toLinExpr(p Pred) (linExpr, bool) {
	switch p := p.(type) {
	case PLit:
		return linConst(p.Value), true
	case PVar:
		return linVar_(p.Name), true
	case PAdd:
		l, ok1 := toLinExpr(p.Left)
		r, ok2 := toLinExpr(p.Right)
		if ok1 && ok2 {
			return linAdd(l, r), true
		}
		return linExpr{}, false
	case PSub:
		l, ok1 := toLinExpr(p.Left)
		r, ok2 := toLinExpr(p.Right)
		if ok1 && ok2 {
			return linSub(l, r), true
		}
		return linExpr{}, false
	case PNeg:
		o, ok := toLinExpr(p.Operand)
		if ok {
			return linScale(o, -1), true
		}
		return linExpr{}, false
	case PMul:
		l, ok1 := toLinExpr(p.Left)
		r, ok2 := toLinExpr(p.Right)
		if !ok1 || !ok2 {
			return linExpr{}, false
		}
		if len(l.vars) == 0 {
			return linScale(r, l.constant), true
		}
		if len(r.vars) == 0 {
			return linScale(l, r.constant), true
		}
		return linExpr{}, false // non-linear
	}
	return linExpr{}, false
}

// ── Constraint normalization (all constraints: linExpr <= 0) ──

type constraint struct{ expr linExpr }

func cmpToConstraints(op string, left, right Pred) ([]constraint, bool) {
	l, ok1 := toLinExpr(left)
	r, ok2 := toLinExpr(right)
	if !ok1 || !ok2 {
		return nil, false
	}
	diff := linSub(l, r)
	switch op {
	case "<=":
		return []constraint{{expr: diff}}, true
	case "<":
		return []constraint{{expr: linAdd(diff, linConst(1))}}, true
	case ">=":
		return []constraint{{expr: linSub(r, l)}}, true
	case ">":
		return []constraint{{expr: linAdd(linSub(r, l), linConst(1))}}, true
	case "==":
		return []constraint{{expr: diff}, {expr: linSub(r, l)}}, true
	case "!=":
		return nil, false // handled by preprocessing
	}
	return nil, false
}

// ── Predicate normalization ──

func negatePred(p Pred) Pred {
	switch p := p.(type) {
	case PTrue:
		return PFalse{}
	case PFalse:
		return PTrue{}
	case PNot:
		return p.Operand
	case PAnd:
		return POr{Left: negatePred(p.Left), Right: negatePred(p.Right)}
	case POr:
		return PAnd{Left: negatePred(p.Left), Right: negatePred(p.Right)}
	case PCmp:
		negOps := map[string]string{"<": ">=", ">": "<=", "<=": ">", ">=": "<", "==": "!=", "!=": "=="}
		return PCmp{Op: negOps[p.Op], Left: p.Left, Right: p.Right}
	}
	return PNot{Operand: p}
}

func normalize(p Pred) Pred {
	switch p := p.(type) {
	case PNot:
		return negateNormalized(normalize(p.Operand))
	case PAnd:
		return PAnd{Left: normalize(p.Left), Right: normalize(p.Right)}
	case POr:
		return POr{Left: normalize(p.Left), Right: normalize(p.Right)}
	}
	return p
}

func negateNormalized(p Pred) Pred {
	switch p := p.(type) {
	case PTrue:
		return PFalse{}
	case PFalse:
		return PTrue{}
	case PAnd:
		return POr{Left: negateNormalized(p.Left), Right: negateNormalized(p.Right)}
	case POr:
		return PAnd{Left: negateNormalized(p.Left), Right: negateNormalized(p.Right)}
	case PCmp:
		negOps := map[string]string{"<": ">=", ">": "<=", "<=": ">", ">=": "<", "==": "!=", "!=": "=="}
		return PCmp{Op: negOps[p.Op], Left: p.Left, Right: p.Right}
	case PNot:
		return normalize(p.Operand)
	}
	return PNot{Operand: p}
}

func eliminateNe(p Pred) Pred {
	switch p := p.(type) {
	case PCmp:
		if p.Op == "!=" {
			return POr{
				Left:  PCmp{Op: "<", Left: p.Left, Right: p.Right},
				Right: PCmp{Op: ">", Left: p.Left, Right: p.Right},
			}
		}
		return p
	case PAnd:
		return PAnd{Left: eliminateNe(p.Left), Right: eliminateNe(p.Right)}
	case POr:
		return POr{Left: eliminateNe(p.Left), Right: eliminateNe(p.Right)}
	case PNot:
		return PNot{Operand: eliminateNe(p.Operand)}
	}
	return p
}

// ── DNF conversion ──

type conjunct []Pred

func toDNF(p Pred) []conjunct {
	switch p := p.(type) {
	case PTrue:
		return []conjunct{{}}
	case PFalse:
		return nil
	case PAnd:
		leftDNF := toDNF(p.Left)
		rightDNF := toDNF(p.Right)
		var result []conjunct
		for _, l := range leftDNF {
			for _, r := range rightDNF {
				combined := make(conjunct, 0, len(l)+len(r))
				combined = append(combined, l...)
				combined = append(combined, r...)
				result = append(result, combined)
			}
		}
		return result
	case POr:
		return append(toDNF(p.Left), toDNF(p.Right)...)
	case PCmp:
		return []conjunct{{p}}
	default:
		return []conjunct{{p}}
	}
}

// ── Fourier-Motzkin elimination ──

func cleanExpr(e linExpr) linExpr {
	vars := make(map[string]int64)
	for v, c := range e.vars {
		if c != 0 {
			vars[v] = c
		}
	}
	return linExpr{vars: vars, constant: e.constant}
}

func getVariables(constraints []constraint) map[string]bool {
	vars := make(map[string]bool)
	for _, c := range constraints {
		for v, coeff := range c.expr.vars {
			if coeff != 0 {
				vars[v] = true
			}
		}
	}
	return vars
}

func fourierMotzkin(constraints []constraint, deadline time.Time) (bool, bool) { // (satisfiable, ok)
	if time.Now().After(deadline) {
		return false, false
	}
	cleaned := make([]constraint, len(constraints))
	for i, c := range constraints {
		cleaned[i] = constraint{expr: cleanExpr(c.expr)}
	}
	constraints = cleaned

	vars := getVariables(constraints)
	if len(vars) == 0 {
		for _, c := range constraints {
			if c.expr.constant > 0 {
				return false, true
			}
		}
		return true, true
	}

	// Pick first variable
	var x string
	for v := range vars {
		x = v
		break
	}

	type bound struct {
		coeff int64
		rest  linExpr
	}
	var upper, lower []bound
	var noX []constraint

	for _, c := range constraints {
		a := c.expr.vars[x]
		if a == 0 {
			noX = append(noX, c)
		} else {
			rest := linExpr{vars: make(map[string]int64), constant: c.expr.constant}
			for v, coeff := range c.expr.vars {
				if v != x {
					rest.vars[v] = coeff
				}
			}
			if a > 0 {
				upper = append(upper, bound{coeff: a, rest: rest})
			} else {
				lower = append(lower, bound{coeff: -a, rest: rest})
			}
		}
	}

	if len(upper) == 0 || len(lower) == 0 {
		return fourierMotzkin(noX, deadline)
	}

	newConstraints := make([]constraint, len(noX), len(noX)+len(upper)*len(lower))
	copy(newConstraints, noX)
	for _, u := range upper {
		for _, l := range lower {
			if time.Now().After(deadline) {
				return false, false
			}
			combined := linAdd(linScale(l.rest, u.coeff), linScale(u.rest, l.coeff))
			newConstraints = append(newConstraints, constraint{expr: combined})
		}
	}

	if len(newConstraints) > 10000 {
		return false, false
	}

	return fourierMotzkin(newConstraints, deadline)
}

func isConjunctSatisfiable(preds []Pred, deadline time.Time) (bool, bool) {
	var constraints []constraint
	for _, p := range preds {
		cmp, ok := p.(PCmp)
		if !ok {
			return false, false
		}
		cs, ok := cmpToConstraints(cmp.Op, cmp.Left, cmp.Right)
		if !ok {
			return false, false
		}
		constraints = append(constraints, cs...)
	}
	return fourierMotzkin(constraints, deadline)
}

// ── Public API ──

// CheckLiteral checks if an integer literal satisfies a predicate.
func CheckLiteral(value int64, predicate string) bool {
	pred := parsePredicate(predicate)
	result, ok := evalPred(pred, map[string]int64{"v": value})
	return ok && result
}

// CheckSubrefinement checks if one refinement implies another.
func CheckSubrefinement(assumption, conclusion string) string {
	return checkSubrefinementTimeout(assumption, conclusion, 5*time.Second)
}

func checkSubrefinementTimeout(assumption, conclusion string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	assump := parsePredicate(assumption)
	concl := parsePredicate(conclusion)
	negConcl := normalize(negatePred(concl))
	combined := normalize(PAnd{Left: assump, Right: negConcl})
	combined = eliminateNe(combined)
	dnf := toDNF(combined)
	if time.Now().After(deadline) || len(dnf) > 1000 {
		return "unknown"
	}
	for _, conj := range dnf {
		if time.Now().After(deadline) {
			return "unknown"
		}
		sat, ok := isConjunctSatisfiable(conj, deadline)
		if !ok {
			return "unknown"
		}
		if sat {
			return "invalid"
		}
	}
	return "valid"
}

// CheckRefinementWithContext checks a conclusion given multiple assumptions.
func CheckRefinementWithContext(assumptions []string, conclusion string) string {
	return checkRefinementWithContextTimeout(assumptions, conclusion, 5*time.Second)
}

func checkRefinementWithContextTimeout(assumptions []string, conclusion string, timeout time.Duration) string {
	assumPreds := make([]Pred, len(assumptions))
	for i, a := range assumptions {
		assumPreds[i] = parsePredicate(a)
	}
	conclPred := parsePredicate(conclusion)
	return checkImplication(assumPreds, conclPred, timeout)
}

func checkImplication(assumptions []Pred, conclusion Pred, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	negConcl := normalize(negatePred(conclusion))
	combined := negConcl
	for _, a := range assumptions {
		combined = PAnd{Left: combined, Right: a}
	}
	combined = normalize(combined)
	combined = eliminateNe(combined)
	dnf := toDNF(combined)
	if time.Now().After(deadline) || len(dnf) > 1000 {
		return "unknown"
	}
	for _, conj := range dnf {
		if time.Now().After(deadline) {
			return "unknown"
		}
		sat, ok := isConjunctSatisfiable(conj, deadline)
		if !ok {
			return "unknown"
		}
		if sat {
			return "invalid"
		}
	}
	return "valid"
}

// SubstitutePredVar substitutes all occurrences of a variable in a predicate string.
func SubstitutePredVar(predicate, from, to string) string {
	pred := parsePredicate(predicate)
	subst := substituteVar(pred, from, to)
	return predToString(subst)
}

func predToString(p Pred) string {
	switch p := p.(type) {
	case PVar:
		return p.Name
	case PLit:
		return fmt.Sprintf("%d", p.Value)
	case PTrue:
		return "true"
	case PFalse:
		return "false"
	case PAdd:
		return fmt.Sprintf("(%s + %s)", predToString(p.Left), predToString(p.Right))
	case PSub:
		return fmt.Sprintf("(%s - %s)", predToString(p.Left), predToString(p.Right))
	case PMul:
		return fmt.Sprintf("(%s * %s)", predToString(p.Left), predToString(p.Right))
	case PNeg:
		return fmt.Sprintf("(-%s)", predToString(p.Operand))
	case PCmp:
		return fmt.Sprintf("(%s %s %s)", predToString(p.Left), p.Op, predToString(p.Right))
	case PAnd:
		return fmt.Sprintf("(%s && %s)", predToString(p.Left), predToString(p.Right))
	case POr:
		return fmt.Sprintf("(%s || %s)", predToString(p.Left), predToString(p.Right))
	case PNot:
		return fmt.Sprintf("(!%s)", predToString(p.Operand))
	}
	return "?"
}

// splitTopLevelComma splits a string by commas at the top level (not inside brackets/parens).
func splitTopLevelComma(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// trimSpace trims whitespace from a string (avoiding strings import in hot paths).
func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
