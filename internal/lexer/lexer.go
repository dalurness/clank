// Package lexer implements the Clank lexer.
package lexer

import (
	"strings"

	"github.com/dalurness/clank/internal/token"
)

func isAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentChar(ch byte) bool {
	return isAlpha(ch) || isDigit(ch) || ch == '_' || ch == '-'
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

// Lex tokenizes source into a slice of tokens, or returns a LexError.
func Lex(source string) ([]token.Token, *token.LexError) {
	l := &lexer{source: source, line: 1, col: 1}
	return l.lex()
}

type lexer struct {
	source      string
	pos         int
	line        int
	col         int
	interpDepth int // >0 when inside ${...} within an interpolated string
}

func (l *lexer) loc() token.Loc {
	return token.Loc{Line: l.line, Col: l.col}
}

func (l *lexer) endLoc() (int, int) {
	return l.line, l.col
}

func (l *lexer) peek() byte {
	if l.pos < len(l.source) {
		return l.source[l.pos]
	}
	return 0
}

func (l *lexer) advance() byte {
	ch := l.source[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *lexer) lexError(msg string, at *token.Loc) *token.LexError {
	loc := l.loc()
	if at != nil {
		loc = *at
	}
	start := l.pos - 20
	if start < 0 {
		start = 0
	}
	end := l.pos + 20
	if end > len(l.source) {
		end = len(l.source)
	}
	return &token.LexError{Code: "E001", Message: msg, Location: loc, Context: l.source[start:end]}
}

func (l *lexer) lex() ([]token.Token, *token.LexError) {
	var tokens []token.Token

	for l.pos < len(l.source) {
		ch := l.peek()

		// Whitespace
		if isWhitespace(ch) {
			l.advance()
			continue
		}

		// Comment
		if ch == '#' {
			for l.pos < len(l.source) && l.peek() != '\n' {
				l.advance()
			}
			continue
		}

		start := l.loc()

		// String literal (with interpolation support)
		if ch == '"' {
			l.advance() // opening quote
			toks, err := l.scanString(&start)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, toks...)
			continue
		}

		// Number literal
		if isDigit(ch) {
			var num strings.Builder
			for l.pos < len(l.source) && isDigit(l.peek()) {
				num.WriteByte(l.advance())
			}
			if l.peek() == '.' && l.pos+1 < len(l.source) && isDigit(l.source[l.pos+1]) {
				num.WriteByte(l.advance()) // the dot
				for l.pos < len(l.source) && isDigit(l.peek()) {
					num.WriteByte(l.advance())
				}
				endLine, endCol := l.endLoc()
				tokens = append(tokens, token.Token{Tag: token.Rat, Value: num.String(), Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
			} else {
				endLine, endCol := l.endLoc()
				tokens = append(tokens, token.Token{Tag: token.Int, Value: num.String(), Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
			}
			continue
		}

		// Identifier or keyword
		if isAlpha(ch) || ch == '_' {
			var word strings.Builder
			for l.pos < len(l.source) && isIdentChar(l.peek()) {
				word.WriteByte(l.advance())
			}
			endLine, endCol := l.endLoc()
			w := word.String()
			loc := token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}
			if w == "true" || w == "false" {
				tokens = append(tokens, token.Token{Tag: token.Bool, Value: w, Loc: loc})
			} else if token.Keywords[w] {
				tokens = append(tokens, token.Token{Tag: token.Keyword, Value: w, Loc: loc})
			} else {
				tokens = append(tokens, token.Token{Tag: token.Ident, Value: w, Loc: loc})
			}
			continue
		}

		// Multi-char operators
		if l.pos+1 < len(l.source) {
			two := l.source[l.pos : l.pos+2]
			found := false
			for _, op := range token.MultiOps {
				if two == op {
					found = true
					break
				}
			}
			if found {
				l.advance()
				l.advance()
				endLine, endCol := l.endLoc()
				tokens = append(tokens, token.Token{Tag: token.Op, Value: two, Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
				continue
			}
		}

		// Range operators: ..= (3-char) and .. (2-char)
		if ch == '.' && l.pos+1 < len(l.source) && l.source[l.pos+1] == '.' {
			if l.pos+2 < len(l.source) && l.source[l.pos+2] == '=' {
				l.advance()
				l.advance()
				l.advance()
				endLine, endCol := l.endLoc()
				tokens = append(tokens, token.Token{Tag: token.Op, Value: "..=", Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
			} else {
				l.advance()
				l.advance()
				endLine, endCol := l.endLoc()
				tokens = append(tokens, token.Token{Tag: token.Op, Value: "..", Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
			}
			continue
		}

		// Single-char operators
		if token.SingleOps[ch] {
			l.advance()
			endLine, endCol := l.endLoc()
			tokens = append(tokens, token.Token{Tag: token.Op, Value: string(ch), Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
			continue
		}

		// Delimiters
		if token.Delimiters[ch] {
			// When inside string interpolation, a } at depth 1 ends the interpolation
			if ch == '}' && l.interpDepth > 0 {
				l.advance()
				endLine, endCol := l.endLoc()
				tokens = append(tokens, token.Token{Tag: token.InterpEnd, Value: "}", Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
				l.interpDepth--
				// Resume scanning the rest of the string
				strStart := l.loc()
				toks, err := l.scanString(&strStart)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, toks...)
				continue
			}
			l.advance()
			endLine, endCol := l.endLoc()
			tokens = append(tokens, token.Token{Tag: token.Delim, Value: string(ch), Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
			continue
		}

		// Unknown character
		return nil, l.lexError("unexpected character '"+string(ch)+"'", &start)
	}

	tokens = append(tokens, token.Token{Tag: token.EOF, Value: "", Loc: l.loc()})
	return tokens, nil
}

// scanString scans the body of a string literal (after the opening " or after
// an InterpEnd resumes). It returns tokens until the closing " or the next ${.
// On ${, it emits a Str token for the content so far, then an InterpStart
// token, increments interpDepth, and returns — the main loop lexes the
// expression, and when it hits the matching }, it calls scanString again.
func (l *lexer) scanString(start *token.Loc) ([]token.Token, *token.LexError) {
	var tokens []token.Token
	var buf strings.Builder

	for l.pos < len(l.source) && l.peek() != '"' {
		// Interpolation: ${
		if l.peek() == '$' && l.pos+1 < len(l.source) && l.source[l.pos+1] == '{' {
			// Emit accumulated string content
			endLine, endCol := l.endLoc()
			tokens = append(tokens, token.Token{Tag: token.Str, Value: buf.String(), Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
			buf.Reset()

			// Emit InterpStart and consume ${
			interpStart := l.loc()
			l.advance() // $
			l.advance() // {
			endLine, endCol = l.endLoc()
			tokens = append(tokens, token.Token{Tag: token.InterpStart, Value: "${", Loc: token.Loc{Line: interpStart.Line, Col: interpStart.Col, EndLine: endLine, EndCol: endCol}})
			l.interpDepth++
			return tokens, nil
		}

		// Escape sequences
		if l.peek() == '\\' {
			l.advance()
			if l.pos >= len(l.source) {
				return nil, l.lexError("unterminated string escape", start)
			}
			esc := l.advance()
			switch esc {
			case 'n':
				buf.WriteByte('\n')
			case 't':
				buf.WriteByte('\t')
			case '\\':
				buf.WriteByte('\\')
			case '"':
				buf.WriteByte('"')
			case '$':
				buf.WriteByte('$')
			default:
				return nil, l.lexError("invalid escape \\"+string(esc), start)
			}
			continue
		}

		buf.WriteByte(l.advance())
	}

	if l.pos >= len(l.source) {
		return nil, l.lexError("unterminated string", start)
	}

	l.advance() // closing "
	endLine, endCol := l.endLoc()
	tokens = append(tokens, token.Token{Tag: token.Str, Value: buf.String(), Loc: token.Loc{Line: start.Line, Col: start.Col, EndLine: endLine, EndCol: endCol}})
	return tokens, nil
}
