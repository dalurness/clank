// Package token defines token types for the Clank lexer.
package token

// Tag classifies a token.
type Tag int

const (
	Int Tag = iota
	Rat
	Str
	Bool
	Ident
	Keyword
	Op
	Delim
	InterpStart // ${ inside a string — starts interpolated expression
	InterpEnd   // } that closes an interpolated expression
	EOF
)

var tagNames = [...]string{
	Int:         "int",
	Rat:         "rat",
	Str:         "str",
	Bool:        "bool",
	Ident:       "ident",
	Keyword:     "keyword",
	Op:          "op",
	Delim:       "delim",
	InterpStart: "interp_start",
	InterpEnd:   "interp_end",
	EOF:         "eof",
}

func (t Tag) String() string {
	if int(t) < len(tagNames) {
		return tagNames[t]
	}
	return "unknown"
}

// Loc is a source location span.
type Loc struct {
	Line    int `json:"line"`
	Col     int `json:"col"`
	EndLine int `json:"end_line,omitempty"`
	EndCol  int `json:"end_col,omitempty"`
}

// Token is a single lexical token.
type Token struct {
	Tag   Tag
	Value string
	Loc   Loc
}

// LexError represents a lexer error.
type LexError struct {
	Code     string
	Message  string
	Location Loc
	Context  string
}

func (e *LexError) Error() string {
	return e.Message
}

// Keywords recognized by the Clank lexer.
var Keywords = map[string]bool{
	"let": true, "in": true, "for": true, "fn": true, "if": true,
	"then": true, "else": true, "match": true, "do": true, "type": true,
	"effect": true, "affine": true, "handle": true, "resume": true,
	"perform": true, "mod": true, "use": true, "pub": true, "clone": true,
	"true": true, "false": true, "interface": true, "impl": true,
	"Self": true, "deriving": true, "where": true, "opaque": true,
	"return": true, "test": true, "alias": true, "discard": true,
}

// MultiOps are multi-character operators in longest-match order.
var MultiOps = []string{
	"==", "!=", "<=", ">=", "&&", "||", "++", "|>", "=>", "->",
}

// SingleOps are single-character operators.
var SingleOps = map[byte]bool{
	'+': true, '-': true, '*': true, '/': true, '%': true,
	'<': true, '>': true, '!': true, '=': true, '\\': true,
}

// Delimiters are single-character delimiters.
var Delimiters = map[byte]bool{
	'{': true, '}': true, '(': true, ')': true, '[': true, ']': true,
	',': true, ':': true, '.': true, '|': true, '&': true, '@': true,
}
