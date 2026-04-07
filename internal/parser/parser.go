package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/token"
)

// ParseError describes a parser error.
type ParseError struct {
	Code     string
	Message  string
	Location token.Loc
	Context  string
}

func (e *ParseError) Error() string {
	return e.Message
}

// cmpOps are comparison operators (non-associative).
var cmpOps = map[string]bool{
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
}

// Parse parses a token stream into a Program.
func Parse(tokens []token.Token) (*ast.Program, *ParseError) {
	p := &parser{tokens: tokens}
	return p.run()
}

// ParseExpression parses a single expression from tokens.
func ParseExpression(tokens []token.Token) (ast.Expr, *ParseError) {
	p := &parser{tokens: tokens}
	return p.runExpr()
}

type parser struct {
	tokens []token.Token
	pos    int
}

// ── Navigation ──

func (p *parser) peek() token.Token {
	return p.tokens[p.pos]
}

func (p *parser) advance() token.Token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) at(tag token.Tag, value ...string) bool {
	t := p.peek()
	if t.Tag != tag {
		return false
	}
	if len(value) > 0 && t.Value != value[0] {
		return false
	}
	return true
}

func (p *parser) expect(tag token.Tag, value ...string) token.Token {
	if !p.at(tag, value...) {
		t := p.peek()
		var expected string
		if len(value) > 0 {
			expected = "'" + value[0] + "'"
		} else {
			expected = tag.String()
		}
		actual := t.Value
		if actual == "" {
			actual = t.Tag.String()
		}
		p.fail(fmt.Sprintf("expected %s, got '%s'", expected, actual))
	}
	return p.advance()
}

func (p *parser) fail(message string) {
	t := p.peek()
	actual := t.Value
	if actual == "" {
		actual = t.Tag.String()
	}
	panic(&ParseError{
		Code:     "E100",
		Message:  message,
		Location: t.Loc,
		Context:  fmt.Sprintf("near '%s'", actual),
	})
}

// ── Entry points ──

func (p *parser) run() (prog *ast.Program, err *ParseError) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*ParseError); ok {
				err = pe
			} else {
				panic(r)
			}
		}
	}()
	return p.parseProgram(), nil
}

func (p *parser) runExpr() (expr ast.Expr, err *ParseError) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*ParseError); ok {
				err = pe
			} else {
				panic(r)
			}
		}
	}()
	e := p.parseExpr()
	if !p.at(token.EOF) {
		p.fail("unexpected token after expression")
	}
	return e, nil
}

// ── Program ──

func (p *parser) parseProgram() *ast.Program {
	var topLevels []ast.TopLevel
	for !p.at(token.EOF) {
		switch {
		case p.at(token.Keyword, "mod"):
			topLevels = append(topLevels, p.parseModDecl())
		case p.at(token.Keyword, "use"):
			topLevels = append(topLevels, p.parseUseDecl())
		case p.at(token.Keyword, "pub"):
			topLevels = append(topLevels, p.parsePubDecl()...)
		case p.at(token.Keyword, "affine"):
			topLevels = append(topLevels, p.parseAffineTypeDecl(false))
		case p.at(token.Keyword, "type"):
			topLevels = append(topLevels, p.parseTypeDecl(false))
		case p.at(token.Keyword, "effect"):
			topLevels = append(topLevels, p.parseEffectDecl(false))
		case p.at(token.Keyword, "interface"):
			topLevels = append(topLevels, p.parseInterfaceDecl(false))
		case p.at(token.Keyword, "impl"):
			topLevels = append(topLevels, p.parseImplBlock(false))
		case p.at(token.Keyword, "test"):
			topLevels = append(topLevels, p.parseTestDecl())
		default:
			topLevels = append(topLevels, p.parseDefinition(false))
		}
	}
	return &ast.Program{TopLevels: topLevels}
}

// ── Module declaration ──

func (p *parser) parseModDecl() ast.TopLevel {
	loc := p.expect(token.Keyword, "mod").Loc
	path := p.parseModPath()
	return ast.TopModDecl{Path: path, Loc: loc}
}

// ── Use declaration ──

func (p *parser) parseUseDecl() ast.TopLevel {
	loc := p.expect(token.Keyword, "use").Loc
	path := p.parseModPath()

	// Qualified import: `use foo.bar` or `use foo.bar as name`
	if !p.at(token.Delim, "(") {
		var qualifier string
		if p.at(token.Ident) && p.peek().Value == "as" {
			p.advance() // consume "as"
			qualifier = p.expectIdentOrKeyword()
		}
		return ast.TopUseDecl{Path: path, Qualified: true, Qualifier: qualifier, Loc: loc}
	}

	// Unqualified import: `use foo.bar (x, y)`
	p.expect(token.Delim, "(")
	var imports []ast.ImportItem
	if !p.at(token.Delim, ")") {
		for {
			name := p.expect(token.Ident).Value
			var alias string
			if p.at(token.Ident) && p.peek().Value == "as" {
				p.advance()
				alias = p.expect(token.Ident).Value
			}
			imports = append(imports, ast.ImportItem{Name: name, Alias: alias})
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
	}
	p.expect(token.Delim, ")")
	return ast.TopUseDecl{Path: path, Imports: imports, Loc: loc}
}

// ── Pub prefix ──

func (p *parser) parsePubDecl() []ast.TopLevel {
	p.expect(token.Keyword, "pub")
	switch {
	case p.at(token.Keyword, "affine"):
		return []ast.TopLevel{p.parseAffineTypeDecl(true)}
	case p.at(token.Keyword, "type"):
		return []ast.TopLevel{p.parseTypeDecl(true)}
	case p.at(token.Keyword, "effect"):
		return []ast.TopLevel{p.parseEffectDecl(true)}
	case p.at(token.Keyword, "interface"):
		return []ast.TopLevel{p.parseInterfaceDecl(true)}
	case p.at(token.Keyword, "impl"):
		return []ast.TopLevel{p.parseImplBlock(true)}
	default:
		return []ast.TopLevel{p.parseDefinition(true)}
	}
}

// ── Module path ──

func (p *parser) parseModPath() []string {
	path := []string{p.expectIdentOrKeyword()}
	for p.at(token.Delim, ".") {
		p.advance()
		path = append(path, p.expectIdentOrKeyword())
	}
	return path
}

func (p *parser) expectIdentOrKeyword() string {
	t := p.peek()
	if t.Tag == token.Ident || t.Tag == token.Keyword {
		p.advance()
		return t.Value
	}
	p.fail(fmt.Sprintf("expected identifier, got '%s'", t.Value))
	return "" // unreachable
}

// ── Top-level definition ──

func (p *parser) parseDefinition(pub bool) ast.TopLevel {
	nameTok := p.expect(token.Ident)
	p.expect(token.Delim, ":")
	sig := p.parseTypeSig()
	constraints := p.tryParseConstraints()
	p.expect(token.Op, "=")
	body := p.parseBody()
	return ast.TopDefinition{Name: nameTok.Value, Sig: sig, Body: body, Pub: pub, Constraints: constraints, Loc: nameTok.Loc}
}

// ── Test declaration ──

func (p *parser) parseTestDecl() ast.TopLevel {
	loc := p.expect(token.Keyword, "test").Loc
	nameTok := p.expect(token.Str)
	p.expect(token.Op, "=")
	body := p.parseBody()
	return ast.TopTestDecl{Name: nameTok.Value, Body: body, Loc: loc}
}


// ── Affine type declaration ──

func (p *parser) parseAffineTypeDecl(pub bool) ast.TopLevel {
	loc := p.expect(token.Keyword, "affine").Loc
	p.expect(token.Keyword, "type")
	name := p.expect(token.Ident).Value
	typeParams := p.parseOptionalTypeParams()
	p.expect(token.Op, "=")
	variants := p.parseVariants()
	deriving := p.parseOptionalDeriving()
	return ast.TopTypeDecl{Name: name, TypeParams: typeParams, Variants: variants, Affine: true, Deriving: deriving, Pub: pub, Loc: loc}
}

// ── Type declaration ──

func (p *parser) parseTypeDecl(pub bool) ast.TopLevel {
	loc := p.expect(token.Keyword, "type").Loc
	name := p.expect(token.Ident).Value
	typeParams := p.parseOptionalTypeParams()
	p.expect(token.Op, "=")
	variants := p.parseVariants()
	deriving := p.parseOptionalDeriving()
	return ast.TopTypeDecl{Name: name, TypeParams: typeParams, Variants: variants, Affine: false, Deriving: deriving, Pub: pub, Loc: loc}
}

func (p *parser) parseOptionalTypeParams() []string {
	var params []string
	if p.at(token.Op, "<") {
		p.advance()
		for {
			params = append(params, p.expect(token.Ident).Value)
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
		p.expect(token.Op, ">")
	}
	return params
}

func (p *parser) parseVariants() []ast.Variant {
	var variants []ast.Variant
	variants = append(variants, p.parseVariant())
	for p.at(token.Delim, "|") {
		p.advance()
		variants = append(variants, p.parseVariant())
	}
	return variants
}

func (p *parser) parseVariant() ast.Variant {
	name := p.expect(token.Ident).Value
	var fields []ast.TypeExpr
	if p.at(token.Delim, "(") {
		p.advance()
		if !p.at(token.Delim, ")") {
			fields = append(fields, p.parseTypeExpr())
			for p.at(token.Delim, ",") {
				p.advance()
				fields = append(fields, p.parseTypeExpr())
			}
		}
		p.expect(token.Delim, ")")
	}
	return ast.Variant{Name: name, Fields: fields}
}

func (p *parser) parseOptionalDeriving() []string {
	var deriving []string
	if p.at(token.Keyword, "deriving") {
		p.advance()
		p.expect(token.Delim, "(")
		if !p.at(token.Delim, ")") {
			for {
				deriving = append(deriving, p.expect(token.Ident).Value)
				if !p.at(token.Delim, ",") {
					break
				}
				p.advance()
			}
		}
		p.expect(token.Delim, ")")
	}
	return deriving
}

// ── Effect alias declaration ──

func (p *parser) parseEffectAlias(pub bool, loc token.Loc) ast.TopLevel {
	p.expect(token.Keyword, "alias")
	name := p.expect(token.Ident).Value
	var params []string
	if p.at(token.Op, "<") {
		p.advance()
		for {
			params = append(params, p.expect(token.Ident).Value)
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
		p.expect(token.Op, ">")
	}
	p.expect(token.Op, "=")
	ann := p.parseEffectAnn()
	return ast.TopEffectAlias{Name: name, Params: params, Effects: ann.effects, Subtracted: ann.subtracted, Pub: pub, Loc: loc}
}

// ── Effect declaration ──

func (p *parser) parseEffectDecl(pub bool) ast.TopLevel {
	loc := p.expect(token.Keyword, "effect").Loc
	if p.at(token.Keyword, "alias") {
		return p.parseEffectAlias(pub, loc)
	}
	name := p.expect(token.Ident).Value
	p.expect(token.Delim, "{")
	var ops []ast.OpSig
	for !p.at(token.Delim, "}") {
		opName := p.expect(token.Ident).Value
		p.expect(token.Delim, ":")
		opType := p.parseTypeExpr()
		var paramType, returnType ast.TypeExpr
		if fn, ok := opType.(ast.TypeFn); ok {
			paramType = fn.Param
			returnType = fn.Result
		} else {
			p.fail(fmt.Sprintf("expected function type for effect operation '%s'", opName))
		}
		var sigParams []ast.TypeSigParam
		if tuple, ok := paramType.(ast.TypeTuple); ok && len(tuple.Elements) == 0 {
			// unit — no params
		} else {
			sigParams = []ast.TypeSigParam{{Name: "_", Type: paramType}}
		}
		ops = append(ops, ast.OpSig{Name: opName, Sig: ast.TypeSig{
			Params: sigParams, ReturnType: returnType,
		}})
		if p.at(token.Delim, ",") {
			p.advance()
		}
	}
	p.expect(token.Delim, "}")
	return ast.TopEffectDecl{Name: name, Ops: ops, Pub: pub, Loc: loc}
}

// ── Type signature ──

func (p *parser) parseTypeSig() ast.TypeSig {
	p.expect(token.Delim, "(")
	var params []ast.TypeSigParam
	if !p.at(token.Delim, ")") {
		for {
			name := p.expect(token.Ident).Value
			p.expect(token.Delim, ":")
			ty := p.parseTypeExpr()
			params = append(params, ast.TypeSigParam{Name: name, Type: ty})
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
	}
	p.expect(token.Delim, ")")
	p.expect(token.Op, "->")
	ann := p.parseEffectAnn()
	returnType := p.parseTypeExpr()
	return ast.TypeSig{Params: params, Effects: ann.effects, Subtracted: ann.subtracted, ReturnType: returnType}
}

// ── Effect annotation ──

type effectAnn struct {
	effects    []ast.EffectRef
	subtracted []ast.EffectRef
}

func (p *parser) parseEffectRef() ast.EffectRef {
	name := p.expect(token.Ident).Value
	var args []string
	if p.at(token.Op, "<") {
		p.advance()
		for {
			args = append(args, p.expect(token.Ident).Value)
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
		p.expect(token.Op, ">")
	}
	return ast.EffectRef{Name: name, Args: args}
}

func (p *parser) parseEffectAnn() effectAnn {
	p.expect(token.Op, "<")
	var effects []ast.EffectRef
	if !p.at(token.Op, ">") {
		for {
			effects = append(effects, p.parseEffectRef())
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
	}
	var subtracted []ast.EffectRef
	if p.at(token.Op, "\\") {
		p.advance()
		for {
			subtracted = append(subtracted, p.parseEffectRef())
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
	}
	p.expect(token.Op, ">")
	return effectAnn{effects: effects, subtracted: subtracted}
}

// ── Type expression ──

func (p *parser) parseTypeExpr() ast.TypeExpr {
	base := p.parseBaseTypeExpr()

	// Postfix @tag projection
	for p.at(token.Delim, "@") {
		p.advance()
		tagName := p.expect(token.Ident).Value
		base = ast.TypeTagProject{Base: base, TagName: tagName, Loc: base.TypeLoc()}
	}

	// Function type: T -> <effects> U
	if p.at(token.Op, "->") {
		p.advance()
		var effects, subtracted []ast.EffectRef
		if p.at(token.Op, "<") {
			ann := p.parseEffectAnn()
			effects = ann.effects
			subtracted = ann.subtracted
		}
		result := p.parseTypeExpr()
		return ast.TypeFn{Param: base, Effects: effects, Subtracted: subtracted, Result: result, Loc: base.TypeLoc()}
	}

	return base
}

func (p *parser) parseBaseTypeExpr() ast.TypeExpr {
	loc := p.peek().Loc

	// &T — borrow type
	if p.at(token.Delim, "&") {
		p.advance()
		inner := p.parseBaseTypeExpr()
		return ast.TypeBorrow{Inner: inner, Loc: loc}
	}

	// () or (T, U) or (T)
	if p.at(token.Delim, "(") {
		p.advance()
		if p.at(token.Delim, ")") {
			p.advance()
			return ast.TypeTuple{Elements: nil, Loc: loc}
		}
		first := p.parseTypeExpr()
		if p.at(token.Delim, ",") {
			elements := []ast.TypeExpr{first}
			for p.at(token.Delim, ",") {
				p.advance()
				elements = append(elements, p.parseTypeExpr())
			}
			p.expect(token.Delim, ")")
			return ast.TypeTuple{Elements: elements, Loc: loc}
		}
		p.expect(token.Delim, ")")
		return first
	}

	// [T]
	if p.at(token.Delim, "[") {
		p.advance()
		element := p.parseTypeExpr()
		p.expect(token.Delim, "]")
		return ast.TypeList{Element: element, Loc: loc}
	}

	// {field: Type, ...} or {field: Type, ... | r}
	if p.at(token.Delim, "{") {
		p.advance()
		var fields []ast.RecordTypeField
		var rowVar string
		if !p.at(token.Delim, "}") {
			// Check for bare row variable: {r}
			if p.at(token.Ident) && p.pos+1 < len(p.tokens) &&
				p.tokens[p.pos+1].Tag == token.Delim && p.tokens[p.pos+1].Value == "}" {
				peeked := p.tokens[p.pos]
				if len(peeked.Value) > 0 && unicode.IsLower(rune(peeked.Value[0])) {
					rowVar = p.advance().Value
					p.expect(token.Delim, "}")
					return ast.TypeRecord{Fields: fields, RowVar: rowVar, Loc: loc}
				}
			}
			for {
				var tags []string
				for p.at(token.Delim, "@") {
					p.advance()
					tags = append(tags, p.expect(token.Ident).Value)
				}
				name := p.expect(token.Ident).Value
				p.expect(token.Delim, ":")
				ty := p.parseTypeExpr()
				fields = append(fields, ast.RecordTypeField{Name: name, Tags: tags, Type: ty})
				if !p.at(token.Delim, ",") {
					break
				}
				p.advance()
			}
			if p.at(token.Delim, "|") {
				p.advance()
				rowVar = p.expect(token.Ident).Value
			}
		}
		p.expect(token.Delim, "}")
		return ast.TypeRecord{Fields: fields, RowVar: rowVar, Loc: loc}
	}

	// Self
	if p.at(token.Keyword, "Self") {
		selfLoc := p.advance().Loc
		return ast.TypeName{Name: "Self", Loc: selfLoc}
	}

	// Named type
	if p.at(token.Ident) {
		name := p.advance().Value

		// Pick/Omit builtins
		if (name == "Pick" || name == "Omit") && p.at(token.Op, "<") {
			p.advance()
			baseType := p.parseTypeExpr()
			p.expect(token.Delim, ",")
			var fieldNames []string
			fieldNames = append(fieldNames, p.expect(token.Str).Value)
			for p.at(token.Delim, "|") {
				p.advance()
				fieldNames = append(fieldNames, p.expect(token.Str).Value)
			}
			p.expect(token.Op, ">")
			if name == "Pick" {
				return ast.TypePick{Base: baseType, FieldNames: fieldNames, Loc: loc}
			}
			return ast.TypeOmit{Base: baseType, FieldNames: fieldNames, Loc: loc}
		}

		var base ast.TypeExpr
		if p.at(token.Op, "<") {
			p.advance()
			args := []ast.TypeExpr{p.parseTypeExpr()}
			for p.at(token.Delim, ",") {
				p.advance()
				args = append(args, p.parseTypeExpr())
			}
			p.expect(token.Op, ">")
			base = ast.TypeGeneric{Name: name, Args: args, Loc: loc}
		} else {
			base = ast.TypeName{Name: name, Loc: loc}
		}

		// Refinement type: Type{predicate}
		if p.at(token.Delim, "{") {
			p.advance()
			var parts []string
			depth := 1
			for depth > 0 && !p.at(token.EOF) {
				if p.at(token.Delim, "{") {
					depth++
				}
				if p.at(token.Delim, "}") {
					depth--
					if depth == 0 {
						break
					}
				}
				parts = append(parts, p.advance().Value)
			}
			p.expect(token.Delim, "}")
			predicate := strings.TrimSpace(strings.Join(parts, " "))
			return ast.TypeRefined{Base: base, Predicate: predicate, Loc: loc}
		}

		return base
	}

	p.fail("expected type expression")
	return nil // unreachable
}

// ── Constraint parsing ──

func (p *parser) tryParseConstraints() []ast.Constraint {
	if !p.at(token.Keyword, "where") {
		return nil
	}
	p.advance()
	var constraints []ast.Constraint
	for {
		constraints = append(constraints, p.parseConstraint())
		if !p.at(token.Delim, ",") {
			break
		}
		p.advance()
	}
	return constraints
}

func (p *parser) parseConstraint() ast.Constraint {
	loc := p.peek().Loc
	iface := p.expect(token.Ident).Value
	var typeArgs []ast.TypeExpr
	if p.at(token.Op, "<") {
		p.advance()
		for {
			typeArgs = append(typeArgs, p.parseTypeExpr())
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
		p.expect(token.Op, ">")
	}
	typeParam := p.expect(token.Ident).Value
	return ast.Constraint{Interface: iface, TypeArgs: typeArgs, TypeParam: typeParam, Loc: loc}
}

func (p *parser) typeExprToSig(te ast.TypeExpr) ast.TypeSig {
	if fn, ok := te.(ast.TypeFn); ok {
		var params []ast.TypeSigParam
		if tuple, ok := fn.Param.(ast.TypeTuple); ok && len(tuple.Elements) == 0 {
			// () -> T: no params
		} else if tuple, ok := fn.Param.(ast.TypeTuple); ok {
			for i, elem := range tuple.Elements {
				params = append(params, ast.TypeSigParam{Name: fmt.Sprintf("_%d", i), Type: elem})
			}
		} else {
			params = []ast.TypeSigParam{{Name: "_0", Type: fn.Param}}
		}
		return ast.TypeSig{Params: params, Effects: fn.Effects, Subtracted: fn.Subtracted, ReturnType: fn.Result}
	}
	return ast.TypeSig{ReturnType: te}
}

// ── Interface declaration ──

func (p *parser) parseInterfaceDecl(pub bool) ast.TopLevel {
	loc := p.expect(token.Keyword, "interface").Loc
	name := p.expect(token.Ident).Value
	typeParams := p.parseOptionalTypeParams()
	var supers []string
	if p.at(token.Delim, ":") {
		p.advance()
		supers = append(supers, p.expect(token.Ident).Value)
		for p.at(token.Op, "+") {
			p.advance()
			supers = append(supers, p.expect(token.Ident).Value)
		}
	}
	p.expect(token.Delim, "{")
	var methods []ast.MethodSig
	for !p.at(token.Delim, "}") {
		methodName := p.expect(token.Ident).Value
		p.expect(token.Delim, ":")
		typeExpr := p.parseTypeExpr()
		sig := p.typeExprToSig(typeExpr)
		methods = append(methods, ast.MethodSig{Name: methodName, Sig: sig})
		if p.at(token.Delim, ",") {
			p.advance()
		}
	}
	p.expect(token.Delim, "}")
	return ast.TopInterfaceDecl{Name: name, TypeParams: typeParams, Supers: supers, Methods: methods, Pub: pub, Loc: loc}
}

// ── Impl type expression (no refinement) ──

func (p *parser) parseImplTypeExpr() ast.TypeExpr {
	loc := p.peek().Loc

	if p.at(token.Delim, "(") {
		p.advance()
		if p.at(token.Delim, ")") {
			p.advance()
			return ast.TypeTuple{Loc: loc}
		}
		first := p.parseTypeExpr()
		if p.at(token.Delim, ",") {
			elements := []ast.TypeExpr{first}
			for p.at(token.Delim, ",") {
				p.advance()
				elements = append(elements, p.parseTypeExpr())
			}
			p.expect(token.Delim, ")")
			return ast.TypeTuple{Elements: elements, Loc: loc}
		}
		p.expect(token.Delim, ")")
		return first
	}

	if p.at(token.Delim, "[") {
		p.advance()
		element := p.parseTypeExpr()
		p.expect(token.Delim, "]")
		return ast.TypeList{Element: element, Loc: loc}
	}

	if p.at(token.Keyword, "Self") {
		p.advance()
		return ast.TypeName{Name: "Self", Loc: loc}
	}

	if p.at(token.Ident) {
		name := p.advance().Value
		if p.at(token.Op, "<") {
			p.advance()
			args := []ast.TypeExpr{p.parseTypeExpr()}
			for p.at(token.Delim, ",") {
				p.advance()
				args = append(args, p.parseTypeExpr())
			}
			p.expect(token.Op, ">")
			return ast.TypeGeneric{Name: name, Args: args, Loc: loc}
		}
		return ast.TypeName{Name: name, Loc: loc}
	}

	p.fail("expected type expression in impl")
	return nil
}

// ── Impl block ──

func (p *parser) parseImplBlock(pub bool) ast.TopLevel {
	loc := p.expect(token.Keyword, "impl").Loc
	iface := p.expect(token.Ident).Value
	var typeArgs []ast.TypeExpr
	if p.at(token.Op, "<") {
		p.advance()
		for {
			typeArgs = append(typeArgs, p.parseTypeExpr())
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
		p.expect(token.Op, ">")
	}
	p.expect(token.Keyword, "for")
	forType := p.parseImplTypeExpr()
	constraints := p.tryParseConstraints()
	p.expect(token.Delim, "{")
	var methods []ast.ImplMethod
	for !p.at(token.Delim, "}") {
		methodName := p.expect(token.Ident).Value
		p.expect(token.Op, "=")
		body := p.parseExpr()
		methods = append(methods, ast.ImplMethod{Name: methodName, Body: body})
	}
	p.expect(token.Delim, "}")
	return ast.TopImplBlock{Interface: iface, TypeArgs: typeArgs, ForType: forType, Constraints: constraints, Methods: methods, Pub: true, Loc: loc}
}

// ── Body ──

func (p *parser) parseBody() ast.Expr {
	return p.parseExprSeq(p.atBodyEnd)
}

// parseExprSeq parses a sequence of expressions with implicit let chaining.
// The atEnd function determines when the sequence terminates. This is used
// by parseBody (top-level function bodies) and parseExprBlock (match arms,
// lambda bodies) with different termination conditions.
func (p *parser) parseExprSeq(atEnd func() bool) ast.Expr {
	expr := p.parseExpr()

	if atEnd() {
		return expr
	}

	// Let without 'in' — rest of sequence becomes the let body
	if let, ok := expr.(ast.ExprLet); ok && let.Body == nil {
		let.Body = p.parseExprSeq(atEnd)
		return let
	}
	if letPat, ok := expr.(ast.ExprLetPattern); ok && letPat.Body == nil {
		letPat.Body = p.parseExprSeq(atEnd)
		return letPat
	}

	// Bare expression followed by more — sequence via let _ = expr in rest
	rest := p.parseExprSeq(atEnd)
	return ast.ExprLet{Name: "_", Value: expr, Body: rest, Loc: expr.ExprLoc()}
}


func (p *parser) atBodyEnd() bool {
	if p.at(token.EOF) {
		return true
	}
	for _, kw := range []string{"type", "effect", "mod", "use", "pub", "test", "affine", "interface", "impl"} {
		if p.at(token.Keyword, kw) {
			return true
		}
	}
	// New definition: ident followed by ':'
	if p.at(token.Ident) && p.pos+1 < len(p.tokens) &&
		p.tokens[p.pos+1].Tag == token.Delim && p.tokens[p.pos+1].Value == ":" {
		return true
	}
	return false
}

// Keywords that start expression forms. Used by both parseExpr (top-level
// dispatch) and parseAtom (so these expressions work as operands in binary
// expressions like  4 + match v { ... }).
var exprKeywords = map[string]bool{
	"let": true, "if": true, "match": true, "for": true,
	"perform": true, "handle": true,
	"clone": true, "discard": true,
}

// ── Expressions ──

func (p *parser) parseExpr() ast.Expr {
	switch {
	case p.at(token.Keyword, "let"):
		return p.parseLet()
	case p.at(token.Keyword, "if"):
		return p.parseIf()
	case p.at(token.Keyword, "match"):
		return p.parseMatch()
	case p.at(token.Keyword, "for"):
		return p.parseFor()
	case p.at(token.Keyword, "perform"):
		return p.parsePerform()
	case p.at(token.Keyword, "handle"):
		return p.parseHandle()
	case p.at(token.Keyword, "clone"):
		return p.parseClone()
	case p.at(token.Keyword, "discard"):
		return p.parseDiscard()
	default:
		return p.parsePipeExpr()
	}
}

func (p *parser) parseClone() ast.Expr {
	loc := p.expect(token.Keyword, "clone").Loc
	expr := p.parsePipeExpr()
	return ast.ExprClone{Expr: expr, Loc: loc}
}

func (p *parser) parseDiscard() ast.Expr {
	loc := p.expect(token.Keyword, "discard").Loc
	expr := p.parsePipeExpr()
	return ast.ExprDiscard{Expr: expr, Loc: loc}
}

// Level 1: |> (left-assoc)
func (p *parser) parsePipeExpr() ast.Expr {
	left := p.parseOrExpr()
	for p.at(token.Op, "|>") {
		loc := p.advance().Loc
		right := p.parseOrExpr()
		left = ast.ExprPipeline{Left: left, Right: right, Loc: loc}
	}
	return left
}

// ── Let binding ──

func (p *parser) parseLet() ast.Expr {
	loc := p.advance().Loc
	// Pattern destructuring
	if p.at(token.Delim, "{") || p.at(token.Delim, "(") {
		pattern := p.parsePattern()
		p.expect(token.Op, "=")
		value := p.parseExpr()
		var body ast.Expr
		if p.at(token.Keyword, "in") {
			p.advance()
			body = p.parseExpr()
		}
		return ast.ExprLetPattern{Pattern: pattern, Value: value, Body: body, Loc: loc}
	}
	name := p.expect(token.Ident).Value
	p.expect(token.Op, "=")
	value := p.parseExpr()
	var body ast.Expr
	if p.at(token.Keyword, "in") {
		p.advance()
		body = p.parseExpr()
	}
	return ast.ExprLet{Name: name, Value: value, Body: body, Loc: loc}
}

// ── If/then/else ──

func (p *parser) parseIf() ast.Expr {
	loc := p.advance().Loc
	cond := p.parseExpr()
	p.expect(token.Keyword, "then")
	then := p.parseExpr()
	p.expect(token.Keyword, "else")
	else_ := p.parseExpr()
	return ast.ExprIf{Cond: cond, Then: then, Else: else_, Loc: loc}
}

// ── Match expression ──

func (p *parser) parseMatch() ast.Expr {
	loc := p.expect(token.Keyword, "match").Loc
	subject := p.parsePipeExpr()
	p.expect(token.Delim, "{")
	var arms []ast.MatchArm
	for !p.at(token.Delim, "}") {
		pattern := p.parsePattern()
		p.expect(token.Op, "=>")
		body := p.parseMatchArmBody()
		arms = append(arms, ast.MatchArm{Pattern: pattern, Body: body})
	}
	p.expect(token.Delim, "}")
	return ast.ExprMatch{Subject: subject, Arms: arms, Loc: loc}
}

// parseLambdaBody parses a lambda body with implicit let chaining.
// Stops at ), ], }, and , which are natural lambda boundaries.
func (p *parser) parseLambdaBody() ast.Expr {
	expr := p.parseExpr()

	if let, ok := expr.(ast.ExprLet); ok && let.Body == nil && !p.atLambdaEnd() {
		let.Body = p.parseLambdaBody()
		return let
	}
	if letPat, ok := expr.(ast.ExprLetPattern); ok && letPat.Body == nil && !p.atLambdaEnd() {
		letPat.Body = p.parseLambdaBody()
		return letPat
	}

	return expr
}

func (p *parser) atLambdaEnd() bool {
	t := p.peek()
	if t.Tag == token.EOF {
		return true
	}
	if t.Tag == token.Delim {
		switch t.Value {
		case ")", "]", "}", ",":
			return true
		}
	}
	return false
}

// parseMatchArmBody parses a match arm body with implicit let chaining.
// let x = expr <continuation> is treated as let x = expr in <continuation>.
// This does NOT chain bare expressions (unlike parseBody) because the start
// of the next match arm pattern is ambiguous with a bare expression.
func (p *parser) parseMatchArmBody() ast.Expr {
	expr := p.parseExpr()

	// If this is a let without 'in' and more expressions follow, chain them
	if let, ok := expr.(ast.ExprLet); ok && let.Body == nil && !p.at(token.Delim, "}") {
		let.Body = p.parseMatchArmBody()
		return let
	}
	if letPat, ok := expr.(ast.ExprLetPattern); ok && letPat.Body == nil && !p.at(token.Delim, "}") {
		letPat.Body = p.parseMatchArmBody()
		return letPat
	}

	return expr
}

// ── Block expression ──

// parseBlock parses a brace block: { expr1  expr2  ... }
// The opening { has already been consumed.
func (p *parser) parseBlock(loc token.Loc) ast.Expr {
	var exprs []ast.Expr
	for !p.at(token.Delim, "}") {
		exprs = append(exprs, p.parseExpr())
	}
	p.expect(token.Delim, "}")
	if len(exprs) == 0 {
		p.fail("block must contain at least one expression")
	}
	return ast.ExprBlock{Exprs: exprs, Loc: loc}
}

// looksLikeRecord returns true if the current position looks like the start
// of a record literal (ident:, @tag, or ..spread) rather than a block.
func (p *parser) looksLikeRecord() bool {
	if p.at(token.Op, "..") {
		return true
	}
	if p.at(token.Delim, "@") {
		return true
	}
	if p.at(token.Ident) && p.pos+1 < len(p.tokens) &&
		p.tokens[p.pos+1].Tag == token.Delim && p.tokens[p.pos+1].Value == ":" {
		return true
	}
	return false
}

// ── For expression ──

func (p *parser) parseFor() ast.Expr {
	loc := p.expect(token.Keyword, "for").Loc
	bind := p.parsePattern()
	p.expect(token.Keyword, "in")
	collection := p.parsePipeExpr()

	var guard ast.Expr
	if p.at(token.Keyword, "if") {
		p.advance()
		guard = p.parsePipeExpr()
	}

	var fold *ast.FoldClause
	if p.at(token.Ident) && p.peek().Value == "fold" {
		p.advance()
		acc := p.expect(token.Ident).Value
		p.expect(token.Op, "=")
		init := p.parsePipeExpr()
		fold = &ast.FoldClause{Acc: acc, Init: init}
	}

	p.expect(token.Keyword, "do")
	body := p.parseForBody()

	return ast.ExprFor{Bind: bind, Collection: collection, Guard: guard, Fold: fold, Body: body, Loc: loc}
}

// parseForBody parses a for-loop body with implicit let chaining.
// Like parseMatchArmBody: chains lets but not bare expressions.
func (p *parser) parseForBody() ast.Expr {
	expr := p.parseExpr()

	if let, ok := expr.(ast.ExprLet); ok && let.Body == nil && !p.atBodyEnd() {
		let.Body = p.parseForBody()
		return let
	}
	if letPat, ok := expr.(ast.ExprLetPattern); ok && letPat.Body == nil && !p.atBodyEnd() {
		letPat.Body = p.parseForBody()
		return letPat
	}

	return expr
}

// ── Perform expression ──

func (p *parser) parsePerform() ast.Expr {
	loc := p.expect(token.Keyword, "perform").Loc
	expr := p.parsePipeExpr()
	return ast.ExprPerform{Expr: expr, Loc: loc}
}

// ── Handle expression ──

func (p *parser) parseHandle() ast.Expr {
	loc := p.expect(token.Keyword, "handle").Loc
	expr := p.parsePipeExpr()
	p.expect(token.Delim, "{")
	var arms []ast.HandlerArm
	for !p.at(token.Delim, "}") {
		if p.at(token.Keyword, "return") {
			p.advance()
			param := p.expect(token.Ident).Value
			p.expect(token.Op, "->")
			body := p.parseExpr()
			arms = append(arms, ast.HandlerArm{Name: "return", Params: []ast.Param{{Name: param}}, Body: body})
		} else {
			opName := p.expect(token.Ident).Value
			var params []ast.Param
			for !p.at(token.Keyword, "resume") {
				params = append(params, ast.Param{Name: p.expect(token.Ident).Value})
			}
			p.expect(token.Keyword, "resume")
			resumeName := p.expect(token.Ident).Value
			p.expect(token.Op, "->")
			body := p.parseExpr()
			arms = append(arms, ast.HandlerArm{Name: opName, Params: params, ResumeName: resumeName, Body: body})
		}
		if p.at(token.Delim, ",") {
			p.advance()
		}
	}
	p.expect(token.Delim, "}")
	return ast.ExprHandle{Expr: expr, Arms: arms, Loc: loc}
}

// ── Pattern ──

func (p *parser) parsePattern() ast.Pattern {
	t := p.peek()

	// Wildcard: _
	if t.Tag == token.Ident && t.Value == "_" {
		p.advance()
		return ast.PatWildcard{Loc: t.Loc}
	}

	// Literal patterns
	if t.Tag == token.Int {
		p.advance()
		v, _ := strconv.ParseInt(t.Value, 10, 64)
		return ast.PatLiteral{Value: ast.LitInt{Value: v}, Loc: t.Loc}
	}
	if t.Tag == token.Rat {
		p.advance()
		v, _ := strconv.ParseFloat(t.Value, 64)
		return ast.PatLiteral{Value: ast.LitRat{Value: v}, Loc: t.Loc}
	}
	if t.Tag == token.Bool {
		p.advance()
		return ast.PatLiteral{Value: ast.LitBool{Value: t.Value == "true"}, Loc: t.Loc}
	}
	if t.Tag == token.Str {
		p.advance()
		return ast.PatLiteral{Value: ast.LitStr{Value: t.Value}, Loc: t.Loc}
	}

	// Tuple pattern
	if t.Tag == token.Delim && t.Value == "(" {
		p.advance()
		if p.at(token.Delim, ")") {
			p.advance()
			return ast.PatLiteral{Value: ast.LitUnit{}, Loc: t.Loc}
		}
		first := p.parsePattern()
		if p.at(token.Delim, ",") {
			elements := []ast.Pattern{first}
			for p.at(token.Delim, ",") {
				p.advance()
				elements = append(elements, p.parsePattern())
			}
			p.expect(token.Delim, ")")
			return ast.PatTuple{Elements: elements, Loc: t.Loc}
		}
		p.expect(token.Delim, ")")
		return first
	}

	// Record pattern
	if t.Tag == token.Delim && t.Value == "{" {
		p.advance()
		var fields []ast.PatField
		var rest string
		if !p.at(token.Delim, "}") {
			for {
				if p.at(token.Delim, "|") {
					p.advance()
					restTok := p.peek()
					if restTok.Tag == token.Ident && restTok.Value == "_" {
						p.advance()
						rest = "_"
					} else {
						rest = p.expect(token.Ident).Value
					}
					break
				}
				name := p.expect(token.Ident).Value
				var pat ast.Pattern
				if p.at(token.Delim, ":") {
					p.advance()
					pat = p.parsePattern()
				}
				fields = append(fields, ast.PatField{Name: name, Pattern: pat})
				if !p.at(token.Delim, ",") {
					break
				}
				p.advance()
			}
			if rest == "" && p.at(token.Delim, "|") {
				p.advance()
				restTok := p.peek()
				if restTok.Tag == token.Ident && restTok.Value == "_" {
					p.advance()
					rest = "_"
				} else {
					rest = p.expect(token.Ident).Value
				}
			}
		}
		p.expect(token.Delim, "}")
		return ast.PatRecord{Fields: fields, Rest: rest, Loc: t.Loc}
	}

	// Identifier — variant or variable
	if t.Tag == token.Ident {
		p.advance()
		isUpper := len(t.Value) > 0 && t.Value[0] >= 'A' && t.Value[0] <= 'Z'
		if p.at(token.Delim, "(") {
			p.advance()
			var args []ast.Pattern
			if !p.at(token.Delim, ")") {
				args = append(args, p.parsePattern())
				for p.at(token.Delim, ",") {
					p.advance()
					args = append(args, p.parsePattern())
				}
			}
			p.expect(token.Delim, ")")
			return ast.PatVariant{Name: t.Value, Args: args, Loc: t.Loc}
		}
		if isUpper {
			return ast.PatVariant{Name: t.Value, Args: nil, Loc: t.Loc}
		}
		return ast.PatVar{Name: t.Value, Loc: t.Loc}
	}

	p.fail(fmt.Sprintf("unexpected '%s' in pattern", t.Value))
	return nil
}

// ── Binary operators (precedence climbing) ──

// Level 2: || (left-assoc)
func (p *parser) parseOrExpr() ast.Expr {
	left := p.parseAndExpr()
	for p.at(token.Op, "||") {
		loc := p.advance().Loc
		right := p.parseAndExpr()
		left = ast.ExprInfix{Op: "||", Left: left, Right: right, Loc: loc}
	}
	return left
}

// Level 3: && (left-assoc)
func (p *parser) parseAndExpr() ast.Expr {
	left := p.parseCmpExpr()
	for p.at(token.Op, "&&") {
		loc := p.advance().Loc
		right := p.parseCmpExpr()
		left = ast.ExprInfix{Op: "&&", Left: left, Right: right, Loc: loc}
	}
	return left
}

// Level 4: == != < > <= >= (non-associative)
func (p *parser) parseCmpExpr() ast.Expr {
	left := p.parseRangeExpr()
	t := p.peek()
	if t.Tag == token.Op && cmpOps[t.Value] {
		op := p.advance()
		right := p.parseRangeExpr()
		next := p.peek()
		if next.Tag == token.Op && cmpOps[next.Value] {
			p.fail("comparison operators are non-associative; use parentheses")
		}
		return ast.ExprInfix{Op: op.Value, Left: left, Right: right, Loc: op.Loc}
	}
	return left
}

// Level 4.5: .. ..= (non-associative)
func (p *parser) parseRangeExpr() ast.Expr {
	left := p.parseConcatExpr()
	t := p.peek()
	if t.Tag == token.Op && (t.Value == ".." || t.Value == "..=") {
		op := p.advance()
		inclusive := op.Value == "..="
		right := p.parseConcatExpr()
		next := p.peek()
		if next.Tag == token.Op && (next.Value == ".." || next.Value == "..=") {
			p.fail("range operators are non-associative; use parentheses")
		}
		return ast.ExprRange{Start: left, End: right, Inclusive: inclusive, Loc: op.Loc}
	}
	return left
}

// Level 5: ++ (right-assoc)
func (p *parser) parseConcatExpr() ast.Expr {
	left := p.parseAddExpr()
	if p.at(token.Op, "++") {
		loc := p.advance().Loc
		right := p.parseConcatExpr()
		return ast.ExprInfix{Op: "++", Left: left, Right: right, Loc: loc}
	}
	return left
}

// Level 6: + - (left-assoc)
func (p *parser) parseAddExpr() ast.Expr {
	left := p.parseMulExpr()
	for p.at(token.Op, "+") || p.at(token.Op, "-") {
		op := p.advance()
		right := p.parseMulExpr()
		left = ast.ExprInfix{Op: op.Value, Left: left, Right: right, Loc: op.Loc}
	}
	return left
}

// Level 7: * / % (left-assoc)
func (p *parser) parseMulExpr() ast.Expr {
	left := p.parseUnaryExpr()
	for p.at(token.Op, "*") || p.at(token.Op, "/") || p.at(token.Op, "%") {
		op := p.advance()
		right := p.parseUnaryExpr()
		left = ast.ExprInfix{Op: op.Value, Left: left, Right: right, Loc: op.Loc}
	}
	return left
}

// Level 8: - ! & (prefix unary)
func (p *parser) parseUnaryExpr() ast.Expr {
	if p.at(token.Op, "-") || p.at(token.Op, "!") {
		op := p.advance()
		operand := p.parseUnaryExpr()
		return ast.ExprUnary{Op: op.Value, Operand: operand, Loc: op.Loc}
	}
	if p.at(token.Delim, "&") {
		loc := p.advance().Loc
		operand := p.parseUnaryExpr()
		return ast.ExprBorrow{Expr: operand, Loc: loc}
	}
	return p.parsePostfixExpr()
}

// Level 9-10: f(args), expr.field
func (p *parser) parsePostfixExpr() ast.Expr {
	expr := p.parseAtom()
	for {
		if p.at(token.Delim, "(") {
			p.advance()
			var args []ast.Expr
			if !p.at(token.Delim, ")") {
				args = append(args, p.parseExpr())
				for p.at(token.Delim, ",") {
					p.advance()
					args = append(args, p.parseExpr())
				}
			}
			p.expect(token.Delim, ")")
			expr = ast.ExprApply{Fn: expr, Args: args, Loc: expr.ExprLoc()}
		} else if p.at(token.Delim, ".") {
			p.advance()
			field := p.expectIdentOrKeyword()
			expr = ast.ExprFieldAccess{Object: expr, Field: field, Loc: expr.ExprLoc()}
		} else {
			break
		}
	}
	return expr
}

// ── Atoms ──

func (p *parser) parseAtom() ast.Expr {
	t := p.peek()

	// fn(params) => expr — lambda
	if t.Tag == token.Keyword && t.Value == "fn" {
		p.advance()
		p.expect(token.Delim, "(")
		var params []ast.Param
		if !p.at(token.Delim, ")") {
			for {
				name := p.expect(token.Ident).Value
				var ty ast.TypeExpr
				if p.at(token.Delim, ":") {
					p.advance()
					ty = p.parseTypeExpr()
				}
				params = append(params, ast.Param{Name: name, Type: ty})
				if !p.at(token.Delim, ",") {
					break
				}
				p.advance()
			}
		}
		p.expect(token.Delim, ")")
		p.expect(token.Op, "=>")
		body := p.parseLambdaBody()
		return ast.ExprLambda{Params: params, Body: body, Loc: t.Loc}
	}

	if t.Tag == token.Int {
		p.advance()
		v, _ := strconv.ParseInt(t.Value, 10, 64)
		return ast.ExprLiteral{Value: ast.LitInt{Value: v}, Loc: t.Loc}
	}

	if t.Tag == token.Rat {
		p.advance()
		v, _ := strconv.ParseFloat(t.Value, 64)
		return ast.ExprLiteral{Value: ast.LitRat{Value: v}, Loc: t.Loc}
	}

	if t.Tag == token.Bool {
		p.advance()
		return ast.ExprLiteral{Value: ast.LitBool{Value: t.Value == "true"}, Loc: t.Loc}
	}

	if t.Tag == token.Str {
		p.advance()
		lit := ast.ExprLiteral{Value: ast.LitStr{Value: t.Value}, Loc: t.Loc}
		// Check for string interpolation: Str InterpStart expr InterpEnd Str ...
		if !p.at(token.InterpStart) {
			return lit
		}
		return p.parseStringInterp(lit)
	}

	// Interpolation starting at beginning of string: "${x}"
	// The lexer emits: Str("") InterpStart ... InterpEnd Str("")
	// but we handle it above since the Str("") still comes first.

	if t.Tag == token.Ident {
		p.advance()
		return ast.ExprVar{Name: t.Value, Loc: t.Loc}
	}

	// ( ) — unit, (expr) — parens, (a, b) — tuple
	if t.Tag == token.Delim && t.Value == "(" {
		p.advance()
		if p.at(token.Delim, ")") {
			p.advance()
			return ast.ExprLiteral{Value: ast.LitUnit{}, Loc: t.Loc}
		}
		inner := p.parseExpr()
		if p.at(token.Delim, ",") {
			elements := []ast.Expr{inner}
			for p.at(token.Delim, ",") {
				p.advance()
				elements = append(elements, p.parseExpr())
			}
			p.expect(token.Delim, ")")
			return ast.ExprTuple{Elements: elements, Loc: t.Loc}
		}
		p.expect(token.Delim, ")")
		return inner
	}

	// [a, b, c] — list literal
	if t.Tag == token.Delim && t.Value == "[" {
		p.advance()
		var elements []ast.Expr
		if !p.at(token.Delim, "]") {
			elements = append(elements, p.parseExpr())
			for p.at(token.Delim, ",") {
				p.advance()
				elements = append(elements, p.parseExpr())
			}
		}
		p.expect(token.Delim, "]")
		return ast.ExprList{Elements: elements, Loc: t.Loc}
	}

	// {name: val, ...} — record literal
	if t.Tag == token.Delim && t.Value == "{" {
		p.advance()
		// Empty record
		if p.at(token.Delim, "}") {
			p.advance()
			return ast.ExprRecord{Loc: t.Loc}
		}
		// Record update: { ident | field: val }
		if p.at(token.Ident) && p.pos+1 < len(p.tokens) &&
			p.tokens[p.pos+1].Tag == token.Delim && p.tokens[p.pos+1].Value == "|" {
			baseToken := p.advance()
			base := ast.ExprVar{Name: baseToken.Value, Loc: baseToken.Loc}
			p.expect(token.Delim, "|")
			var fields []ast.RecordUpdateField
			for {
				name := p.expect(token.Ident).Value
				p.expect(token.Delim, ":")
				value := p.parseExpr()
				fields = append(fields, ast.RecordUpdateField{Name: name, Value: value})
				if !p.at(token.Delim, ",") {
					break
				}
				p.advance()
			}
			p.expect(token.Delim, "}")
			return ast.ExprRecordUpdate{Base: base, Fields: fields, Loc: t.Loc}
		}
		// Block expression: { expr1  expr2  ... }
		if !p.looksLikeRecord() {
			return p.parseBlock(t.Loc)
		}
		// Regular record literal
		var fields []ast.RecordField
		var spread ast.Expr
		for {
			// Spread: ..expr
			if p.at(token.Op, "..") {
				p.advance()
				spread = p.parseExpr()
				break
			}
			var tags []string
			for p.at(token.Delim, "@") {
				p.advance()
				tags = append(tags, p.expect(token.Ident).Value)
			}
			name := p.expect(token.Ident).Value
			p.expect(token.Delim, ":")
			value := p.parseExpr()
			fields = append(fields, ast.RecordField{Name: name, Tags: tags, Value: value})
			if !p.at(token.Delim, ",") {
				break
			}
			p.advance()
		}
		p.expect(token.Delim, "}")
		return ast.ExprRecord{Fields: fields, Spread: spread, Loc: t.Loc}
	}

	// Keyword expressions (match, if, etc.) are valid atoms so they can
	// appear as operands:  4 + match v { ... }
	if t.Tag == token.Keyword && exprKeywords[t.Value] {
		return p.parseExpr()
	}

	p.fail(fmt.Sprintf("unexpected '%s'", t.Value))
	return nil
}

// parseStringInterp builds an ExprInfix(++) chain from an interpolated string.
// Called after the first Str token has been consumed and InterpStart is next.
// Each interpolated expression is wrapped in show() for auto-conversion to Str.
func (p *parser) parseStringInterp(accumulated ast.Expr) ast.Expr {
	for p.at(token.InterpStart) {
		loc := p.advance().Loc // consume InterpStart

		// Parse the interpolated expression
		expr := p.parseExpr()
		p.expect(token.InterpEnd)

		// Wrap in show() for auto-string conversion
		showExpr := ast.ExprApply{
			Fn:   ast.ExprVar{Name: "show", Loc: loc},
			Args: []ast.Expr{expr},
			Loc:  loc,
		}

		// Chain: accumulated ++ show(expr)
		accumulated = ast.ExprInfix{
			Op:   "++",
			Left: accumulated,
			Right: showExpr,
			Loc:  loc,
		}

		// If there's a trailing Str segment, chain it too
		if p.at(token.Str) {
			strTok := p.advance()
			strLit := ast.ExprLiteral{Value: ast.LitStr{Value: strTok.Value}, Loc: strTok.Loc}
			// Only chain non-empty trailing strings (optimization)
			if strTok.Value != "" || p.at(token.InterpStart) {
				accumulated = ast.ExprInfix{
					Op:    "++",
					Left:  accumulated,
					Right: strLit,
					Loc:   strTok.Loc,
				}
			}
		}
	}
	return accumulated
}
