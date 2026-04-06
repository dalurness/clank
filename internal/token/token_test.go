package token_test

import (
	"testing"

	"github.com/dalurness/clank/internal/token"
)

func TestTagString(t *testing.T) {
	tests := []struct {
		tag  token.Tag
		want string
	}{
		{token.Int, "int"},
		{token.Rat, "rat"},
		{token.Str, "str"},
		{token.Bool, "bool"},
		{token.Ident, "ident"},
		{token.Keyword, "keyword"},
		{token.Op, "op"},
		{token.Delim, "delim"},
		{token.EOF, "eof"},
	}
	for _, tt := range tests {
		if got := tt.tag.String(); got != tt.want {
			t.Errorf("Tag(%d).String() = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

func TestKeywords(t *testing.T) {
	expected := []string{
		"let", "in", "for", "fn", "if", "then", "else", "match", "do", "type",
		"effect", "affine", "handle", "resume", "perform", "mod", "use", "pub",
		"clone", "true", "false", "interface", "impl", "Self", "deriving",
		"where", "opaque", "return", "test", "alias", "discard",
	}
	for _, kw := range expected {
		if !token.Keywords[kw] {
			t.Errorf("expected %q to be a keyword", kw)
		}
	}
	if token.Keywords["notakeyword"] {
		t.Error("unexpected keyword 'notakeyword'")
	}
}

func TestLexError(t *testing.T) {
	err := &token.LexError{
		Code:    "E001",
		Message: "unexpected character",
		Location: token.Loc{Line: 1, Col: 5},
		Context: "let x = @",
	}
	if err.Error() != "unexpected character" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}
