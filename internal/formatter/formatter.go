// Package formatter pretty-prints a Clank AST back to canonical source.
// Enforces: 2-space indent, blank lines between top-level defs,
// sorted imports, one-per-line params if >80 chars, aligned match arms.
package formatter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dalurness/clank/internal/ast"
)

// Comment is a source comment with its line number.
type Comment struct {
	Line int
	Text string
}

// ExtractComments extracts comments from source, preserving their line numbers.
func ExtractComments(source string) []Comment {
	var comments []Comment
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			comments = append(comments, Comment{Line: i + 1, Text: line})
		}
	}
	return comments
}

// Format formats a program AST back to canonical source, preserving comments.
func Format(program *ast.Program, source string) string {
	comments := ExtractComments(source)
	var out []string
	topLevels := program.TopLevels
	commentIdx := 0

	for i, tl := range topLevels {
		tlLine := tl.TopLoc().Line

		// Collect comments that belong before this top-level
		var preComments []Comment
		for commentIdx < len(comments) && comments[commentIdx].Line < tlLine {
			preComments = append(preComments, comments[commentIdx])
			commentIdx++
		}

		if i > 0 {
			out = append(out, "")
		}

		// Emit pre-comments with internal blank lines preserved
		for c, cm := range preComments {
			if c > 0 {
				prevLine := preComments[c-1].Line
				if cm.Line-prevLine > 1 {
					out = append(out, "")
				}
			}
			out = append(out, cm.Text)
		}

		// Gap between comments and definition
		if len(preComments) > 0 {
			lastCommentLine := preComments[len(preComments)-1].Line
			if tlLine-lastCommentLine > 1 {
				out = append(out, "")
			}
		}

		out = append(out, formatTopLevel(tl))
	}

	// Trailing comments
	if commentIdx < len(comments) {
		out = append(out, "")
		for commentIdx < len(comments) {
			out = append(out, comments[commentIdx].Text)
			commentIdx++
		}
	}

	return strings.Join(out, "\n") + "\n"
}

// ── Effect ref helpers ──

func formatEffectRef(ref ast.EffectRef) string {
	if len(ref.Args) > 0 {
		return fmt.Sprintf("%s<%s>", ref.Name, strings.Join(ref.Args, ", "))
	}
	return ref.Name
}

// ── Top-level formatters ──

func formatTopLevel(tl ast.TopLevel) string {
	switch d := tl.(type) {
	case ast.TopModDecl:
		return "mod " + strings.Join(d.Path, ".")
	case ast.TopUseDecl:
		return formatUseDecl(d)
	case ast.TopDefinition:
		return formatDefinition(d)
	case ast.TopTypeDecl:
		return formatTypeDecl(d)
	case ast.TopEffectDecl:
		return formatEffectDecl(d)
	case ast.TopEffectAlias:
		return formatEffectAlias(d)
	case ast.TopTestDecl:
		return formatTestDecl(d)
	case ast.TopInterfaceDecl:
		return formatInterfaceDecl(d)
	case ast.TopImplBlock:
		return formatImplBlock(d)
	}
	return ""
}

func formatUseDecl(d ast.TopUseDecl) string {
	path := strings.Join(d.Path, ".")
	sorted := make([]ast.ImportItem, len(d.Imports))
	copy(sorted, d.Imports)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	var parts []string
	for _, item := range sorted {
		if item.Alias != "" {
			parts = append(parts, fmt.Sprintf("%s as %s", item.Name, item.Alias))
		} else {
			parts = append(parts, item.Name)
		}
	}
	return fmt.Sprintf("use %s (%s)", path, strings.Join(parts, ", "))
}

func formatDefinition(d ast.TopDefinition) string {
	pub := ""
	if d.Pub {
		pub = "pub "
	}
	sig := formatTypeSig(d.Sig)
	header := fmt.Sprintf("%s%s : %s =", pub, d.Name, sig)

	// Special case: body is a do-block
	if do, ok := d.Body.(ast.ExprDo); ok {
		doInline := formatDo(do, 0)
		if !strings.Contains(doInline, "\n") && len(header)+1+len(doInline) <= 80 {
			return header + " " + doInline
		}
		doStr := formatDo(do, 2)
		return header + " " + strings.TrimLeft(doStr, " ")
	}

	body := formatExprBody(d.Body, 2, true)
	return header + "\n" + body
}

func formatTestDecl(d ast.TopTestDecl) string {
	header := fmt.Sprintf("test %q =", d.Name)
	body := formatExprBody(d.Body, 2, true)
	return header + "\n" + body
}

func formatInterfaceDecl(d ast.TopInterfaceDecl) string {
	pub := ""
	if d.Pub {
		pub = "pub "
	}
	typeParams := ""
	if len(d.TypeParams) > 0 {
		typeParams = "<" + strings.Join(d.TypeParams, ", ") + ">"
	}
	supers := ""
	if len(d.Supers) > 0 {
		supers = " : " + strings.Join(d.Supers, " + ")
	}
	var methods []string
	for _, m := range d.Methods {
		methods = append(methods, fmt.Sprintf("  %s : %s", m.Name, formatTypeSig(m.Sig)))
	}
	return fmt.Sprintf("%sinterface %s%s%s {\n%s\n}", pub, d.Name, typeParams, supers, strings.Join(methods, ",\n"))
}

func formatImplBlock(d ast.TopImplBlock) string {
	typeArgs := ""
	if len(d.TypeArgs) > 0 {
		var parts []string
		for _, ta := range d.TypeArgs {
			parts = append(parts, formatTypeExpr(ta))
		}
		typeArgs = "<" + strings.Join(parts, ", ") + ">"
	}
	forType := formatTypeExpr(d.ForType)
	constraints := ""
	if len(d.Constraints) > 0 {
		var parts []string
		for _, c := range d.Constraints {
			parts = append(parts, fmt.Sprintf("%s %s", c.Interface, c.TypeParam))
		}
		constraints = " where " + strings.Join(parts, ", ")
	}
	var methods []string
	for _, m := range d.Methods {
		body := formatExprBody(m.Body, 2, false)
		methods = append(methods, fmt.Sprintf("  %s = %s", m.Name, strings.TrimSpace(body)))
	}
	return fmt.Sprintf("impl %s%s for %s%s {\n%s\n}", d.Interface, typeArgs, forType, constraints, strings.Join(methods, "\n"))
}

func formatTypeDecl(d ast.TopTypeDecl) string {
	pub := ""
	if d.Pub {
		pub = "pub "
	}
	typeParams := ""
	if len(d.TypeParams) > 0 {
		typeParams = "<" + strings.Join(d.TypeParams, ", ") + ">"
	}
	var variants []string
	for _, v := range d.Variants {
		variants = append(variants, formatVariant(v))
	}

	singleLine := fmt.Sprintf("%stype %s%s = %s", pub, d.Name, typeParams, strings.Join(variants, " | "))
	if len(singleLine) <= 80 {
		return singleLine
	}

	lines := []string{fmt.Sprintf("%stype %s%s", pub, d.Name, typeParams)}
	for i, v := range variants {
		prefix := "  | "
		if i == 0 {
			prefix = "  = "
		}
		lines = append(lines, prefix+v)
	}
	return strings.Join(lines, "\n")
}

func formatVariant(v ast.Variant) string {
	if len(v.Fields) == 0 {
		return v.Name
	}
	var parts []string
	for _, f := range v.Fields {
		parts = append(parts, formatTypeExpr(f))
	}
	return fmt.Sprintf("%s(%s)", v.Name, strings.Join(parts, ", "))
}

func formatEffectDecl(d ast.TopEffectDecl) string {
	pub := ""
	if d.Pub {
		pub = "pub "
	}
	if len(d.Ops) == 0 {
		return fmt.Sprintf("%seffect %s {}", pub, d.Name)
	}
	lines := []string{fmt.Sprintf("%seffect %s {", pub, d.Name)}
	for i, op := range d.Ops {
		comma := ""
		if i < len(d.Ops)-1 {
			comma = ","
		}
		lines = append(lines, "  "+formatOpSig(op)+comma)
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

func formatEffectAlias(d ast.TopEffectAlias) string {
	pub := ""
	if d.Pub {
		pub = "pub "
	}
	params := ""
	if len(d.Params) > 0 {
		params = "<" + strings.Join(d.Params, ", ") + ">"
	}
	var effs []string
	for _, e := range d.Effects {
		effs = append(effs, formatEffectRef(e))
	}
	effects := strings.Join(effs, ", ")
	if len(d.Subtracted) > 0 {
		var subs []string
		for _, s := range d.Subtracted {
			subs = append(subs, formatEffectRef(s))
		}
		effects += " \\ " + strings.Join(subs, ", ")
	}
	return fmt.Sprintf("%seffect alias %s%s = <%s>", pub, d.Name, params, effects)
}

func formatOpSig(op ast.OpSig) string {
	sig := op.Sig
	if len(sig.Params) == 0 {
		return fmt.Sprintf("%s : () -> %s", op.Name, formatTypeExpr(sig.ReturnType))
	}
	paramType := sig.Params[0].Type
	return fmt.Sprintf("%s : %s -> %s", op.Name, formatTypeExpr(paramType), formatTypeExpr(sig.ReturnType))
}

// ── Type signature ──

func formatTypeSig(sig ast.TypeSig) string {
	var params []string
	for _, p := range sig.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, formatTypeExprAsParam(p.Type)))
	}
	effects := ""
	if len(sig.Effects) > 0 {
		var effs []string
		for _, e := range sig.Effects {
			effs = append(effs, formatEffectRef(e))
		}
		effects = strings.Join(effs, ", ")
	}
	if len(sig.Subtracted) > 0 {
		var subs []string
		for _, s := range sig.Subtracted {
			subs = append(subs, formatEffectRef(s))
		}
		effects += " \\ " + strings.Join(subs, ", ")
	}
	ret := formatTypeExpr(sig.ReturnType)

	singleLine := fmt.Sprintf("(%s) -> <%s> %s", strings.Join(params, ", "), effects, ret)
	if len(singleLine) <= 80 {
		return singleLine
	}

	// Multi-line params
	var paramLines []string
	for i, p := range params {
		suffix := ","
		if i == len(params)-1 {
			suffix = ""
		}
		paramLines = append(paramLines, "  "+p+suffix)
	}
	return fmt.Sprintf("(\n%s\n) -> <%s> %s", strings.Join(paramLines, "\n"), effects, ret)
}

// ── Type expressions ──

func formatTypeExprAsParam(t ast.TypeExpr) string {
	if _, ok := t.(ast.TypeFn); ok {
		return "(" + formatTypeExpr(t) + ")"
	}
	return formatTypeExpr(t)
}

func formatTypeExpr(t ast.TypeExpr) string {
	switch te := t.(type) {
	case ast.TypeName:
		return te.Name
	case ast.TypeList:
		return "[" + formatTypeExpr(te.Element) + "]"
	case ast.TypeTuple:
		if len(te.Elements) == 0 {
			return "()"
		}
		var parts []string
		for _, el := range te.Elements {
			parts = append(parts, formatTypeExpr(el))
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case ast.TypeRecord:
		var fields []string
		for _, f := range te.Fields {
			tagPrefix := ""
			for _, tag := range f.Tags {
				tagPrefix += "@" + tag + " "
			}
			fields = append(fields, tagPrefix+f.Name+": "+formatTypeExpr(f.Type))
		}
		fieldStr := strings.Join(fields, ", ")
		if te.RowVar != "" {
			if fieldStr != "" {
				return "{" + fieldStr + " | " + te.RowVar + "}"
			}
			return "{" + te.RowVar + "}"
		}
		return "{" + fieldStr + "}"
	case ast.TypeTagProject:
		return formatTypeExpr(te.Base) + " @" + te.TagName
	case ast.TypeTypeFilter:
		return formatTypeExpr(te.Base) + " : " + formatTypeExpr(te.FilterType)
	case ast.TypePick:
		var names []string
		for _, n := range te.FieldNames {
			names = append(names, fmt.Sprintf("%q", n))
		}
		return fmt.Sprintf("Pick<%s, %s>", formatTypeExpr(te.Base), strings.Join(names, " | "))
	case ast.TypeOmit:
		var names []string
		for _, n := range te.FieldNames {
			names = append(names, fmt.Sprintf("%q", n))
		}
		return fmt.Sprintf("Omit<%s, %s>", formatTypeExpr(te.Base), strings.Join(names, " | "))
	case ast.TypeUnion:
		return formatTypeExpr(te.Left) + " | " + formatTypeExpr(te.Right)
	case ast.TypeFn:
		param := formatTypeExpr(te.Param)
		result := formatTypeExpr(te.Result)
		if len(te.Effects) > 0 {
			var effs []string
			for _, e := range te.Effects {
				effs = append(effs, formatEffectRef(e))
			}
			effStr := strings.Join(effs, ", ")
			if len(te.Subtracted) > 0 {
				var subs []string
				for _, s := range te.Subtracted {
					subs = append(subs, formatEffectRef(s))
				}
				effStr += " \\ " + strings.Join(subs, ", ")
			}
			return fmt.Sprintf("%s -> <%s> %s", param, effStr, result)
		}
		return param + " -> " + result
	case ast.TypeGeneric:
		var args []string
		for _, a := range te.Args {
			args = append(args, formatTypeExpr(a))
		}
		return fmt.Sprintf("%s<%s>", te.Name, strings.Join(args, ", "))
	case ast.TypeRefined:
		return formatTypeExpr(te.Base) + " where " + te.Predicate
	case ast.TypeBorrow:
		return "&" + formatTypeExpr(te.Inner)
	}
	return ""
}

// ── Expression formatting ──

func formatExpr(expr ast.Expr, indent int) string {
	pad := strings.Repeat(" ", indent)
	switch e := expr.(type) {
	case ast.ExprLiteral:
		return pad + formatLiteral(e)
	case ast.ExprVar:
		return pad + e.Name
	case ast.ExprLet:
		return formatLet(e, indent, false)
	case ast.ExprLetPattern:
		return formatLetPattern(e, indent, false)
	case ast.ExprIf:
		return formatIf(e, indent)
	case ast.ExprMatch:
		return formatMatch(e, indent)
	case ast.ExprLambda:
		return formatLambda(e, indent)
	case ast.ExprApply:
		return formatApply(e, indent)
	case ast.ExprPipeline:
		return formatPipeline(e, indent)
	case ast.ExprInfix:
		return formatInfix(e, indent)
	case ast.ExprUnary:
		return pad + e.Op + formatExprInline(e.Operand)
	case ast.ExprDo:
		return formatDo(e, indent)
	case ast.ExprFor:
		return formatFor(e, indent)
	case ast.ExprHandle:
		return formatHandle(e, indent)
	case ast.ExprPerform:
		return pad + "perform " + formatExprInline(e.Expr)
	case ast.ExprList:
		return formatList(e, indent)
	case ast.ExprTuple:
		var parts []string
		for _, el := range e.Elements {
			parts = append(parts, formatExprInline(el))
		}
		return pad + "(" + strings.Join(parts, ", ") + ")"
	case ast.ExprRecord:
		return formatRecord(e, indent)
	case ast.ExprRecordUpdate:
		return formatRecordUpdate(e, indent)
	case ast.ExprFieldAccess:
		return pad + formatExprInline(e.Object) + "." + e.Field
	case ast.ExprRange:
		op := ".."
		if e.Inclusive {
			op = "..="
		}
		return pad + formatExprInline(e.Start) + op + formatExprInline(e.End)
	case ast.ExprBorrow:
		return pad + "&" + formatExprInline(e.Expr)
	case ast.ExprClone:
		return pad + "clone " + formatExprInline(e.Expr)
	case ast.ExprDiscard:
		return pad + "discard " + formatExprInline(e.Expr)
	}
	return ""
}

func formatExprInline(expr ast.Expr) string {
	return formatExpr(expr, 0)
}

func formatLiteral(e ast.ExprLiteral) string {
	switch lit := e.Value.(type) {
	case ast.LitInt:
		return fmt.Sprintf("%d", lit.Value)
	case ast.LitRat:
		s := fmt.Sprintf("%g", lit.Value)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	case ast.LitBool:
		if lit.Value {
			return "true"
		}
		return "false"
	case ast.LitStr:
		return fmt.Sprintf("%q", lit.Value)
	case ast.LitUnit:
		return "()"
	}
	return ""
}

func escapeStr(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

func formatLet(e ast.ExprLet, indent int, isTopBody bool) string {
	pad := strings.Repeat(" ", indent)

	// Implicit sequence: let _ = expr in body
	if e.Name == "_" && e.Body != nil {
		valStr := formatExprInline(e.Value)
		bodyStr := formatExprBody(e.Body, indent, isTopBody)
		return pad + valStr + "\n" + bodyStr
	}

	if e.Body == nil {
		valStr := formatExprAtIndent(e.Value, indent+2)
		return pad + "let " + e.Name + " = " + valStr
	}

	if isTopBody {
		valStr := formatExprAtIndent(e.Value, indent+2)
		bodyStr := formatExprBody(e.Body, indent, true)
		return pad + "let " + e.Name + " = " + valStr + "\n" + bodyStr
	}

	// Inline let-in
	valStr := formatExprInline(e.Value)
	bodyStr := formatExprInline(e.Body)
	return pad + "let " + e.Name + " = " + valStr + " in " + bodyStr
}

func formatLetPattern(e ast.ExprLetPattern, indent int, isTopBody bool) string {
	pad := strings.Repeat(" ", indent)
	patStr := formatPatternInline(e.Pattern)

	if e.Body == nil {
		valStr := formatExprAtIndent(e.Value, indent+2)
		return pad + "let " + patStr + " = " + valStr
	}

	if isTopBody {
		valStr := formatExprAtIndent(e.Value, indent+2)
		bodyStr := formatExprBody(e.Body, indent, true)
		return pad + "let " + patStr + " = " + valStr + "\n" + bodyStr
	}

	valStr := formatExprInline(e.Value)
	bodyStr := formatExprInline(e.Body)
	return pad + "let " + patStr + " = " + valStr + " in " + bodyStr
}

func formatExprAtIndent(expr ast.Expr, indent int) string {
	inline := formatExprInline(expr)
	if !strings.Contains(inline, "\n") {
		return inline
	}
	full := formatExpr(expr, indent)
	return strings.TrimLeft(full, " ")
}

func formatExprBody(expr ast.Expr, indent int, isTopBody bool) string {
	if e, ok := expr.(ast.ExprLet); ok {
		return formatLet(e, indent, isTopBody)
	}
	if e, ok := expr.(ast.ExprLetPattern); ok {
		return formatLetPattern(e, indent, isTopBody)
	}
	return formatExpr(expr, indent)
}

func formatIf(e ast.ExprIf, indent int) string {
	pad := strings.Repeat(" ", indent)
	cond := formatExprInline(e.Cond)
	then := formatExprInline(e.Then)

	// Chain if-else
	if elseIf, ok := e.Else.(ast.ExprIf); ok {
		elseStr := formatIf(elseIf, 0)
		return pad + "if " + cond + " then " + then + "\n" + pad + "else " + elseStr
	}

	else_ := formatExprInline(e.Else)

	singleLine := pad + "if " + cond + " then " + then + " else " + else_
	if len(singleLine) <= 80 && !strings.Contains(then, "\n") && !strings.Contains(else_, "\n") {
		return singleLine
	}

	return pad + "if " + cond + " then " + then + "\n" + pad + "else " + else_
}

func formatMatch(e ast.ExprMatch, indent int) string {
	pad := strings.Repeat(" ", indent)
	subject := formatExprInline(e.Subject)
	lines := []string{pad + "match " + subject + " {"}

	type armText struct {
		pattern string
		body    string
	}
	var armTexts []armText
	maxPatLen := 0
	for _, arm := range e.Arms {
		p := formatPattern(arm.Pattern)
		b := formatExprInline(arm.Body)
		armTexts = append(armTexts, armText{p, b})
		if len(p) > maxPatLen {
			maxPatLen = len(p)
		}
	}

	for _, arm := range armTexts {
		padded := arm.pattern + strings.Repeat(" ", maxPatLen-len(arm.pattern))
		lines = append(lines, pad+"  "+padded+" => "+arm.body)
	}

	lines = append(lines, pad+"}")
	return strings.Join(lines, "\n")
}

func formatLambda(e ast.ExprLambda, indent int) string {
	pad := strings.Repeat(" ", indent)
	var params []string
	for _, p := range e.Params {
		params = append(params, formatParam(p))
	}
	body := formatExprInline(e.Body)
	return pad + "fn(" + strings.Join(params, ", ") + ") => " + body
}

func formatParam(p ast.Param) string {
	if p.Type != nil {
		return p.Name + ": " + formatTypeExpr(p.Type)
	}
	return p.Name
}

func formatApply(e ast.ExprApply, indent int) string {
	pad := strings.Repeat(" ", indent)
	fn := formatExprInline(e.Fn)
	var args []string
	for _, a := range e.Args {
		args = append(args, formatExprInline(a))
	}
	return pad + fn + "(" + strings.Join(args, ", ") + ")"
}

func formatPipeline(e ast.ExprPipeline, indent int) string {
	pad := strings.Repeat(" ", indent)
	var parts []string
	var current ast.Expr = e
	for {
		if p, ok := current.(ast.ExprPipeline); ok {
			parts = append(parts, formatExprInline(p.Right))
			current = p.Left
		} else {
			parts = append(parts, formatExprInline(current))
			break
		}
	}
	// Reverse
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}

	singleLine := pad + strings.Join(parts, " |> ")
	if len(singleLine) <= 80 {
		return singleLine
	}

	lines := []string{pad + parts[0]}
	for i := 1; i < len(parts); i++ {
		lines = append(lines, pad+"  |> "+parts[i])
	}
	return strings.Join(lines, "\n")
}

var precedence = map[string]int{
	"||": 1,
	"&&": 2,
	"==": 3, "!=": 3, "<": 3, ">": 3, "<=": 3, ">=": 3,
	"++": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
}

func isRightAssoc(op string) bool {
	return op == "++"
}

func formatInfix(e ast.ExprInfix, indent int) string {
	pad := strings.Repeat(" ", indent)
	left := formatInfixOperand(e.Left, e.Op, "left")
	right := formatInfixOperand(e.Right, e.Op, "right")
	return pad + left + " " + e.Op + " " + right
}

func formatInfixOperand(expr ast.Expr, parentOp string, side string) string {
	if inf, ok := expr.(ast.ExprInfix); ok {
		childPrec := precedence[inf.Op]
		parentPrec := precedence[parentOp]
		needParens := childPrec < parentPrec ||
			(childPrec == parentPrec && side == "right" && !isRightAssoc(parentOp)) ||
			(childPrec == parentPrec && side == "left" && isRightAssoc(parentOp) && inf.Op != parentOp)
		if needParens {
			return "(" + formatExprInline(expr) + ")"
		}
	}
	return formatExprInline(expr)
}

func formatDo(e ast.ExprDo, indent int) string {
	pad := strings.Repeat(" ", indent)
	lines := []string{pad + "do {"}
	for _, step := range e.Steps {
		lines = append(lines, formatDoStep(step, indent+2))
	}
	lines = append(lines, pad+"}")
	return strings.Join(lines, "\n")
}

func formatDoStep(step ast.DoStep, indent int) string {
	pad := strings.Repeat(" ", indent)
	if step.Bind != "" {
		return pad + step.Bind + " <- " + formatExprInline(step.Expr)
	}
	return formatExpr(step.Expr, indent)
}

func formatFor(e ast.ExprFor, indent int) string {
	pad := strings.Repeat(" ", indent)
	result := pad + "for " + formatPatternInline(e.Bind) + " in " + formatExprInline(e.Collection)
	if e.Guard != nil {
		result += " if " + formatExprInline(e.Guard)
	}
	if e.Fold != nil {
		result += " fold " + e.Fold.Acc + " = " + formatExprInline(e.Fold.Init)
	}
	result += " do " + formatExprInline(e.Body)
	return result
}

func formatHandle(e ast.ExprHandle, indent int) string {
	pad := strings.Repeat(" ", indent)
	var subjectStr string
	if nested, ok := e.Expr.(ast.ExprHandle); ok {
		inner := formatHandle(nested, indent)
		subjectStr = "(" + strings.TrimLeft(inner, " ") + ")"
	} else {
		subjectStr = formatExprInline(e.Expr)
		if needsParens(e.Expr) {
			subjectStr = "(" + subjectStr + ")"
		}
	}
	lines := []string{pad + "handle " + subjectStr + " {"}
	for i, arm := range e.Arms {
		comma := ""
		if i < len(e.Arms)-1 {
			comma = ","
		}
		if arm.Name == "return" {
			paramName := "_"
			if len(arm.Params) > 0 {
				paramName = arm.Params[0].Name
			}
			body := formatExprInline(arm.Body)
			lines = append(lines, pad+"  return "+paramName+" -> "+body+comma)
		} else {
			var paramNames []string
			for _, p := range arm.Params {
				paramNames = append(paramNames, p.Name)
			}
			resume := ""
			if arm.ResumeName != "" {
				resume = " resume " + arm.ResumeName
			}
			body := formatExprInline(arm.Body)
			lines = append(lines, pad+"  "+arm.Name+" "+strings.Join(paramNames, " ")+resume+" -> "+body+comma)
		}
	}
	lines = append(lines, pad+"}")
	return strings.Join(lines, "\n")
}

func formatList(e ast.ExprList, indent int) string {
	pad := strings.Repeat(" ", indent)
	if len(e.Elements) == 0 {
		return pad + "[]"
	}
	var items []string
	for _, el := range e.Elements {
		items = append(items, formatExprInline(el))
	}
	singleLine := pad + "[" + strings.Join(items, ", ") + "]"
	if len(singleLine) <= 80 {
		return singleLine
	}

	lines := []string{pad + "["}
	for i, item := range items {
		comma := ""
		if i < len(items)-1 {
			comma = ","
		}
		lines = append(lines, pad+"  "+item+comma)
	}
	lines = append(lines, pad+"]")
	return strings.Join(lines, "\n")
}

func formatRecord(e ast.ExprRecord, indent int) string {
	pad := strings.Repeat(" ", indent)
	if len(e.Fields) == 0 && e.Spread == nil {
		return pad + "{}"
	}
	var items []string
	for _, f := range e.Fields {
		tagPrefix := ""
		for _, t := range f.Tags {
			tagPrefix += "@" + t + " "
		}
		items = append(items, tagPrefix+f.Name+": "+formatExprInline(f.Value))
	}
	if e.Spread != nil {
		items = append(items, ".."+formatExprInline(e.Spread))
	}
	return pad + "{" + strings.Join(items, ", ") + "}"
}

func formatRecordUpdate(e ast.ExprRecordUpdate, indent int) string {
	pad := strings.Repeat(" ", indent)
	base := formatExprInline(e.Base)
	var fields []string
	for _, f := range e.Fields {
		fields = append(fields, f.Name+": "+formatExprInline(f.Value))
	}
	return pad + "{" + base + " | " + strings.Join(fields, ", ") + "}"
}

func needsParens(expr ast.Expr) bool {
	switch expr.(type) {
	case ast.ExprPerform, ast.ExprHandle, ast.ExprIf, ast.ExprMatch, ast.ExprLet, ast.ExprDo:
		return true
	}
	return false
}

// ── Pattern formatting ──

func formatPatternInline(pat ast.Pattern) string {
	switch p := pat.(type) {
	case ast.PatVar:
		return p.Name
	case ast.PatWildcard:
		return "_"
	case ast.PatLiteral:
		return formatPatLiteral(p.Value)
	case ast.PatTuple:
		var parts []string
		for _, el := range p.Elements {
			parts = append(parts, formatPatternInline(el))
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case ast.PatVariant:
		if len(p.Args) == 0 {
			return p.Name
		}
		var args []string
		for _, a := range p.Args {
			args = append(args, formatPatternInline(a))
		}
		return p.Name + "(" + strings.Join(args, ", ") + ")"
	case ast.PatRecord:
		var fields []string
		for _, f := range p.Fields {
			if f.Pattern != nil {
				fields = append(fields, f.Name+": "+formatPatternInline(f.Pattern))
			} else {
				fields = append(fields, f.Name)
			}
		}
		rest := ""
		if p.Rest != "" {
			rest = " | " + p.Rest
		}
		return "{" + strings.Join(fields, ", ") + rest + "}"
	}
	return ""
}

func formatPattern(pat ast.Pattern) string {
	switch p := pat.(type) {
	case ast.PatVar:
		return p.Name
	case ast.PatWildcard:
		return "_"
	case ast.PatLiteral:
		return formatPatLiteralFull(p.Value)
	case ast.PatVariant:
		if len(p.Args) == 0 {
			return p.Name
		}
		var args []string
		for _, a := range p.Args {
			args = append(args, formatPattern(a))
		}
		return p.Name + "(" + strings.Join(args, ", ") + ")"
	case ast.PatTuple:
		var parts []string
		for _, el := range p.Elements {
			parts = append(parts, formatPattern(el))
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case ast.PatRecord:
		var fields []string
		for _, f := range p.Fields {
			if f.Pattern != nil {
				fields = append(fields, f.Name+": "+formatPattern(f.Pattern))
			} else {
				fields = append(fields, f.Name)
			}
		}
		rest := ""
		if p.Rest != "" {
			rest = " | " + p.Rest
		}
		return "{" + strings.Join(fields, ", ") + rest + "}"
	}
	return ""
}

func formatPatLiteral(lit ast.Literal) string {
	switch l := lit.(type) {
	case ast.LitInt:
		return fmt.Sprintf("%d", l.Value)
	case ast.LitRat:
		return fmt.Sprintf("%g", l.Value)
	case ast.LitBool:
		return fmt.Sprintf("%t", l.Value)
	case ast.LitStr:
		return fmt.Sprintf("%q", l.Value)
	case ast.LitUnit:
		return "()"
	}
	return ""
}

func formatPatLiteralFull(lit ast.Literal) string {
	switch l := lit.(type) {
	case ast.LitInt:
		return fmt.Sprintf("%d", l.Value)
	case ast.LitRat:
		s := fmt.Sprintf("%g", l.Value)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	case ast.LitBool:
		if l.Value {
			return "true"
		}
		return "false"
	case ast.LitStr:
		return "\"" + escapeStr(l.Value) + "\""
	case ast.LitUnit:
		return "()"
	}
	return ""
}
