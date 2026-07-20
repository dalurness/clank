package checker

import (
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
)

// checkSource runs the full front-end (lex → parse → desugar → type check)
// on a source string, the same way the CLI does, and returns all
// diagnostics.
func checkSource(t *testing.T, source string) []TypeError {
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
	return TypeCheck(program)
}

// hasEffectError reports whether an E401 naming the given effect was raised.
func hasEffectError(errs []TypeError, effect string) bool {
	for _, e := range errs {
		if e.Code == "E401" && strings.Contains(e.Message, "'"+effect+"'") {
			return true
		}
	}
	return false
}

func assertNoHardErrors(t *testing.T, errs []TypeError) {
	t.Helper()
	for _, e := range errs {
		if !strings.HasPrefix(e.Code, "W") {
			t.Errorf("unexpected error: %s", e.Error())
		}
	}
}

func TestEffectBuiltinIoRequiresDeclaration(t *testing.T) {
	// A pure signature that calls print must be rejected: print performs io.
	errs := checkSource(t, "main : () -> <> () =\n  print(\"x\")\n")
	if !hasEffectError(errs, "io") {
		t.Fatalf("expected E401 for io, got: %v", errs)
	}
}

func TestEffectBuiltinIoDeclaredOk(t *testing.T) {
	errs := checkSource(t, "main : () -> <io> () =\n  print(\"x\")\n")
	assertNoHardErrors(t, errs)
}

func TestEffectPropagatesThroughCall(t *testing.T) {
	// A <> helper that calls an <io> function inherits io transitively.
	src := "greet : () -> <io> () =\n  print(\"hi\")\n\n" +
		"bad : () -> <> () =\n  greet()\n\n" +
		"main : () -> <io> () =\n  bad()\n"
	errs := checkSource(t, src)
	if !hasEffectError(errs, "io") {
		t.Fatalf("expected transitive E401 for io on 'bad', got: %v", errs)
	}
}

func TestEffectPropagatesThroughInlineLambdaHOF(t *testing.T) {
	// An effectful inline lambda passed to map propagates io to the caller.
	src := "main : () -> <> () =\n  let _ = map([1, 2], fn(x) => print(show(x)))\n  ()\n"
	errs := checkSource(t, src)
	if !hasEffectError(errs, "io") {
		t.Fatalf("expected E401 for io via map lambda, got: %v", errs)
	}
}

func TestEffectPropagatesThroughNamedCallbackHOF(t *testing.T) {
	// Effect polymorphism: a *named* effectful function passed to map
	// propagates its effect to the caller via map's shared effect row var.
	src := "shout : (s: Str) -> <io> Str =\n  let _ = print(s)\n  s\n\n" +
		"main : () -> <> () =\n  let _ = map([\"a\"], shout)\n  ()\n"
	errs := checkSource(t, src)
	if !hasEffectError(errs, "io") {
		t.Fatalf("expected E401 for io via named callback, got: %v", errs)
	}
}

func TestEffectHOFCallbackPropagationDeclaredOk(t *testing.T) {
	// Declaring the effect the callback performs makes the HOF call legal.
	src := "main : () -> <io> () =\n  let _ = map([1, 2], fn(x) => print(show(x)))\n  ()\n"
	assertNoHardErrors(t, checkSource(t, src))
}

func TestEffectFallibleBuiltinNeedsExn(t *testing.T) {
	// fs.read carries <io, exn>; declaring only <io> is insufficient.
	errs := checkSource(t, "main : () -> <io> () =\n  let _ = fs.read(\"x\")\n  ()\n")
	if !hasEffectError(errs, "exn") {
		t.Fatalf("expected E401 for exn from fs.read, got: %v", errs)
	}
}

func TestEffectFallibleBuiltinDeclaredOk(t *testing.T) {
	assertNoHardErrors(t, checkSource(t, "main : () -> <io, exn> () =\n  let _ = fs.read(\"x\")\n  ()\n"))
}

func TestEffectHandleDischargesExnNotIo(t *testing.T) {
	// handle discharges the exn from fs.read (raise arm) but not io, so the
	// declared row needs io and must NOT need exn.
	src := "main : () -> <io> () =\n" +
		"  handle print(fs.read(\"x\")) {\n" +
		"    return v -> v,\n" +
		"    raise msg resume _ -> ()\n" +
		"  }\n"
	assertNoHardErrors(t, checkSource(t, src))
}

func TestEffectAsyncBuiltinNeedsAsync(t *testing.T) {
	errs := checkSource(t, "main : () -> <io> () =\n  sleep(1)\n")
	if !hasEffectError(errs, "async") {
		t.Fatalf("expected E401 for async from sleep, got: %v", errs)
	}
}

func TestEffectSubsumptionDeclareMoreThanPerformed(t *testing.T) {
	// Declaring more effects than the body performs is allowed (<> ⊆ <io>).
	assertNoHardErrors(t, checkSource(t, "main : () -> <io, exn> () =\n  print(\"x\")\n"))
}

func TestEffectPureBuiltinStaysPure(t *testing.T) {
	// A pure builtin pipeline must not require any effect.
	src := "main : () -> <> Int =\n  fold([1, 2, 3], 0, fn(acc, x) => acc + x)\n"
	assertNoHardErrors(t, checkSource(t, src))
}
