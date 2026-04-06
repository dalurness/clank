package lexer_test

import (
	"testing"

	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/token"
)

func TestLexBasicTokens(t *testing.T) {
	tokens, err := lexer.Lex("10 + 5")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	expected := []struct {
		tag   token.Tag
		value string
	}{
		{token.Int, "10"},
		{token.Op, "+"},
		{token.Int, "5"},
		{token.EOF, ""},
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, e := range expected {
		if tokens[i].Tag != e.tag || tokens[i].Value != e.value {
			t.Errorf("token %d: expected %s(%q), got %s(%q)", i, e.tag, e.value, tokens[i].Tag, tokens[i].Value)
		}
	}
}

func TestLexKeywords(t *testing.T) {
	tokens, err := lexer.Lex("let x = if true then 1 else 0")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	expected := []struct {
		tag   token.Tag
		value string
	}{
		{token.Keyword, "let"},
		{token.Ident, "x"},
		{token.Op, "="},
		{token.Keyword, "if"},
		{token.Bool, "true"},
		{token.Keyword, "then"},
		{token.Int, "1"},
		{token.Keyword, "else"},
		{token.Int, "0"},
		{token.EOF, ""},
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, e := range expected {
		if tokens[i].Tag != e.tag || tokens[i].Value != e.value {
			t.Errorf("token %d: expected %s(%q), got %s(%q)", i, e.tag, e.value, tokens[i].Tag, tokens[i].Value)
		}
	}
}

func TestLexStringLiteral(t *testing.T) {
	tokens, err := lexer.Lex(`"hello\nworld"`)
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].Tag != token.Str || tokens[0].Value != "hello\nworld" {
		t.Errorf("expected str(hello\\nworld), got %s(%q)", tokens[0].Tag, tokens[0].Value)
	}
}

func TestLexRational(t *testing.T) {
	tokens, err := lexer.Lex("3.14")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if tokens[0].Tag != token.Rat || tokens[0].Value != "3.14" {
		t.Errorf("expected rat(3.14), got %s(%q)", tokens[0].Tag, tokens[0].Value)
	}
}

func TestLexMultiCharOps(t *testing.T) {
	tokens, err := lexer.Lex("== != <= >= && || ++ |> => <- ->")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	ops := []string{"==", "!=", "<=", ">=", "&&", "||", "++", "|>", "=>", "<-", "->"}
	for i, op := range ops {
		if tokens[i].Tag != token.Op || tokens[i].Value != op {
			t.Errorf("token %d: expected op(%q), got %s(%q)", i, op, tokens[i].Tag, tokens[i].Value)
		}
	}
}

func TestLexRangeOps(t *testing.T) {
	tokens, err := lexer.Lex("1..10 1..=10")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	expected := []struct {
		tag   token.Tag
		value string
	}{
		{token.Int, "1"},
		{token.Op, ".."},
		{token.Int, "10"},
		{token.Int, "1"},
		{token.Op, "..="},
		{token.Int, "10"},
		{token.EOF, ""},
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, e := range expected {
		if tokens[i].Tag != e.tag || tokens[i].Value != e.value {
			t.Errorf("token %d: expected %s(%q), got %s(%q)", i, e.tag, e.value, tokens[i].Tag, tokens[i].Value)
		}
	}
}

func TestLexDelimiters(t *testing.T) {
	tokens, err := lexer.Lex("{}()[],:.|&@")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	delims := []string{"{", "}", "(", ")", "[", "]", ",", ":", ".", "|", "&", "@"}
	for i, d := range delims {
		if tokens[i].Tag != token.Delim || tokens[i].Value != d {
			t.Errorf("token %d: expected delim(%q), got %s(%q)", i, d, tokens[i].Tag, tokens[i].Value)
		}
	}
}

func TestLexComments(t *testing.T) {
	tokens, err := lexer.Lex("# this is a comment\n42")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].Tag != token.Int || tokens[0].Value != "42" {
		t.Errorf("expected int(42), got %s(%q)", tokens[0].Tag, tokens[0].Value)
	}
}

func TestLexIdentWithHyphen(t *testing.T) {
	tokens, err := lexer.Lex("is-even")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if tokens[0].Tag != token.Ident || tokens[0].Value != "is-even" {
		t.Errorf("expected ident(is-even), got %s(%q)", tokens[0].Tag, tokens[0].Value)
	}
}

func TestLexLocations(t *testing.T) {
	tokens, err := lexer.Lex("a\nb")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if tokens[0].Loc.Line != 1 || tokens[0].Loc.Col != 1 {
		t.Errorf("token 0: expected line 1 col 1, got line %d col %d", tokens[0].Loc.Line, tokens[0].Loc.Col)
	}
	if tokens[1].Loc.Line != 2 || tokens[1].Loc.Col != 1 {
		t.Errorf("token 1: expected line 2 col 1, got line %d col %d", tokens[1].Loc.Line, tokens[1].Loc.Col)
	}
}

func TestLexUnterminatedString(t *testing.T) {
	_, err := lexer.Lex(`"hello`)
	if err == nil {
		t.Fatal("expected lex error for unterminated string")
	}
	if err.Message != "unterminated string" {
		t.Errorf("expected 'unterminated string', got %q", err.Message)
	}
}

func TestLexUnexpectedChar(t *testing.T) {
	_, err := lexer.Lex("~")
	if err == nil {
		t.Fatal("expected lex error for unexpected character")
	}
}

func TestLexBooleans(t *testing.T) {
	tokens, err := lexer.Lex("true false")
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if tokens[0].Tag != token.Bool || tokens[0].Value != "true" {
		t.Errorf("expected bool(true), got %s(%q)", tokens[0].Tag, tokens[0].Value)
	}
	if tokens[1].Tag != token.Bool || tokens[1].Value != "false" {
		t.Errorf("expected bool(false), got %s(%q)", tokens[1].Tag, tokens[1].Value)
	}
}

func TestLexEscapeSequences(t *testing.T) {
	tokens, err := lexer.Lex(`"a\tb\\c\"d"`)
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if tokens[0].Value != "a\tb\\c\"d" {
		t.Errorf("expected escape handling, got %q", tokens[0].Value)
	}
}

func TestLexInvalidEscape(t *testing.T) {
	_, err := lexer.Lex(`"\q"`)
	if err == nil {
		t.Fatal("expected lex error for invalid escape")
	}
}

func TestLexStringInterpolation(t *testing.T) {
	tokens, err := lexer.Lex(`"hello ${name}!"`)
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	expected := []struct {
		tag   token.Tag
		value string
	}{
		{token.Str, "hello "},
		{token.InterpStart, "${"},
		{token.Ident, "name"},
		{token.InterpEnd, "}"},
		{token.Str, "!"},
		{token.EOF, ""},
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, exp := range expected {
		if tokens[i].Tag != exp.tag || tokens[i].Value != exp.value {
			t.Errorf("token[%d]: expected %v %q, got %v %q", i, exp.tag, exp.value, tokens[i].Tag, tokens[i].Value)
		}
	}
}

func TestLexStringInterpolationEscape(t *testing.T) {
	tokens, err := lexer.Lex(`"escaped \${x}"`)
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	// \$ prevents interpolation — produces literal ${x}
	if tokens[0].Tag != token.Str || tokens[0].Value != "escaped ${x}" {
		t.Errorf("expected Str(\"escaped ${x}\"), got %v %q", tokens[0].Tag, tokens[0].Value)
	}
}

func TestLexStringInterpolationExpr(t *testing.T) {
	tokens, err := lexer.Lex(`"val: ${1 + 2}"`)
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	expected := []struct {
		tag   token.Tag
		value string
	}{
		{token.Str, "val: "},
		{token.InterpStart, "${"},
		{token.Int, "1"},
		{token.Op, "+"},
		{token.Int, "2"},
		{token.InterpEnd, "}"},
		{token.Str, ""},
		{token.EOF, ""},
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, exp := range expected {
		if tokens[i].Tag != exp.tag || tokens[i].Value != exp.value {
			t.Errorf("token[%d]: expected %v %q, got %v %q", i, exp.tag, exp.value, tokens[i].Tag, tokens[i].Value)
		}
	}
}

func TestLexStringNoInterp(t *testing.T) {
	// Plain strings should still work exactly as before
	tokens, err := lexer.Lex(`"hello world"`)
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	if len(tokens) != 2 { // Str + EOF
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].Tag != token.Str || tokens[0].Value != "hello world" {
		t.Errorf("expected Str(\"hello world\"), got %v %q", tokens[0].Tag, tokens[0].Value)
	}
}

func TestLexStringAdjacentInterp(t *testing.T) {
	tokens, err := lexer.Lex(`"${a}${b}"`)
	if err != nil {
		t.Fatalf("unexpected lex error: %s", err.Message)
	}
	expected := []struct {
		tag   token.Tag
		value string
	}{
		{token.Str, ""},
		{token.InterpStart, "${"},
		{token.Ident, "a"},
		{token.InterpEnd, "}"},
		{token.Str, ""},
		{token.InterpStart, "${"},
		{token.Ident, "b"},
		{token.InterpEnd, "}"},
		{token.Str, ""},
		{token.EOF, ""},
	}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, exp := range expected {
		if tokens[i].Tag != exp.tag || tokens[i].Value != exp.value {
			t.Errorf("token[%d]: expected %v %q, got %v %q", i, exp.tag, exp.value, tokens[i].Tag, tokens[i].Value)
		}
	}
}
