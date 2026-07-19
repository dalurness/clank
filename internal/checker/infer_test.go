package checker

import (
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
)

// inferMain parses source and infers the type of `main`, desugaring
// definition bodies the same way the CLI does before checking.
func inferMain(t *testing.T, source string) (*InferredInfo, []TypeError) {
	t.Helper()
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		t.Fatalf("lex error: %v", lexErr)
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		t.Fatalf("parse error: %v", parseErr)
	}
	for i, tl := range program.TopLevels {
		if d, ok := tl.(ast.TopDefinition); ok {
			d.Body = desugar.Desugar(d.Body)
			program.TopLevels[i] = d
		}
	}
	return InferDefinition(program, nil, nil, "main")
}

func hardErrors(errs []TypeError) []TypeError {
	var out []TypeError
	for _, e := range errs {
		if !strings.HasPrefix(e.Code, "W") {
			out = append(out, e)
		}
	}
	return out
}

func TestInferDefinitionSimpleExpr(t *testing.T) {
	info, errs := inferMain(t, "main : () -> <> auto = 1 + 2")
	if he := hardErrors(errs); len(he) > 0 {
		t.Fatalf("unexpected errors: %v", he)
	}
	if info == nil {
		t.Fatal("expected info, got nil")
	}
	if info.Type != "Int" {
		t.Errorf("expected Int, got %q", info.Type)
	}
	if len(info.Effects) != 0 {
		t.Errorf("expected no effects, got %v", info.Effects)
	}
}

func TestInferDefinitionAutoDoesNotFailReturnCheck(t *testing.T) {
	// The eval wrapper's `auto` return must not produce E307 for the
	// captured definition.
	_, errs := inferMain(t, `main : () -> <> auto = "hello"`)
	if he := hardErrors(errs); len(he) > 0 {
		t.Fatalf("auto return should not error for captured def: %v", he)
	}
}

func TestInferDefinitionFunctionType(t *testing.T) {
	info, errs := inferMain(t, "main : () -> <> auto = fn(x: Int) => x + 1")
	if he := hardErrors(errs); len(he) > 0 {
		t.Fatalf("unexpected errors: %v", he)
	}
	if info == nil {
		t.Fatal("expected info, got nil")
	}
	if !strings.Contains(info.Type, "Int") || !strings.Contains(info.Type, "->") {
		t.Errorf("expected a function type over Int, got %q", info.Type)
	}
}

func TestInferDefinitionUserEffect(t *testing.T) {
	src := `effect log {
  info : Str -> ()
}

main : () -> <> auto = perform info("hi")`
	info, errs := inferMain(t, src)
	if he := hardErrors(errs); len(he) > 0 {
		t.Fatalf("unexpected errors: %v", he)
	}
	if info == nil {
		t.Fatal("expected info, got nil")
	}
	if len(info.Effects) != 1 || info.Effects[0] != "log" {
		t.Errorf("expected effects [log], got %v", info.Effects)
	}
	// No W401 for the captured def — the `<>` row is the wrapper's, not
	// the user's.
	for _, e := range errs {
		if e.Code == "W401" {
			t.Errorf("captured def should not emit W401, got: %s", e.Message)
		}
	}
}

func TestInferDefinitionMissing(t *testing.T) {
	info, _ := inferMain(t, "helper : () -> <> Int = 1")
	if info != nil {
		t.Fatalf("expected nil info for missing def, got %+v", info)
	}
}

func TestInferDefinitionStillReportsBodyErrors(t *testing.T) {
	_, errs := inferMain(t, `main : () -> <> auto = 1 + "x"`)
	if he := hardErrors(errs); len(he) == 0 {
		t.Fatal("expected a type error for 1 + \"x\"")
	}
}
