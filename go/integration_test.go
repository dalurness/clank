package clank_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/checker"
	"github.com/dalurness/clank/internal/compiler"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/eval"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/linter"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/pretty"
	"github.com/dalurness/clank/internal/vm"
)

// testRoot returns the path to the project test/ directory.
func testRoot(t *testing.T) string {
	t.Helper()
	// go/ is at project root; test/ is a sibling
	return filepath.Join("..", "test")
}

// parseExpected extracts expected output lines from # Expected output: comments.
func parseExpected(source string) (lines []string, found bool) {
	inBlock := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# Expected output:") {
			inBlock = true
			continue
		}
		if inBlock {
			if strings.HasPrefix(trimmed, "# ") {
				lines = append(lines, trimmed[2:])
			} else if trimmed == "#" {
				lines = append(lines, "")
			} else {
				break
			}
		}
	}
	return lines, inBlock
}

// isErrorTest returns true if the file expects an error rather than output.
func isErrorTest(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "Expected:") && strings.Contains(trimmed, "error") {
			return true
		}
		if strings.Contains(trimmed, "Expected") && strings.Contains(trimmed, "error") &&
			strings.HasPrefix(trimmed, "#") {
			return true
		}
	}
	return false
}

// usesImports returns true if the file uses `use` (module imports).
func usesImports(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "use ") {
			return true
		}
	}
	return false
}

// checkProgram lexes, parses, desugars, and type-checks a program.
// Returns type errors (nil if none) and any pipeline error.
func checkProgram(source string) ([]checker.TypeError, error) {
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		return nil, fmt.Errorf("lex error: %s", lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return nil, fmt.Errorf("parse error: %s", parseErr.Message)
	}

	// Desugar before type checking (checker expects desugared AST)
	desugared := &ast.Program{TopLevels: make([]ast.TopLevel, len(program.TopLevels))}
	for i, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopTestDecl:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopImplBlock:
			methods := make([]ast.ImplMethod, len(d.Methods))
			for j, m := range d.Methods {
				methods[j] = ast.ImplMethod{Name: m.Name, Body: desugar.Desugar(m.Body)}
			}
			d.Methods = methods
			desugared.TopLevels[i] = d
		default:
			desugared.TopLevels[i] = tl
		}
	}

	typeErrors := checker.TypeCheck(desugared)
	return typeErrors, nil
}

// runProgram lexes, parses, desugars, and evaluates a program, returning captured stdout.
func runProgram(source string) (string, error) {
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		return "", fmt.Errorf("lex error: %s", lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return "", fmt.Errorf("parse error: %s", parseErr.Message)
	}

	// Desugar all top-level definition bodies
	desugared := &ast.Program{TopLevels: make([]ast.TopLevel, len(program.TopLevels))}
	for i, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopTestDecl:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopImplBlock:
			methods := make([]ast.ImplMethod, len(d.Methods))
			for j, m := range d.Methods {
				methods[j] = ast.ImplMethod{Name: m.Name, Body: desugar.Desugar(m.Body)}
			}
			d.Methods = methods
			desugared.TopLevels[i] = d
		default:
			desugared.TopLevels[i] = tl
		}
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	_, runErr := eval.Run(desugared)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	if runErr != nil {
		return buf.String(), runErr
	}
	return buf.String(), nil
}

// prettyPipeline applies --pretty expansion then lex/parse/desugar.
func prettyPipeline(terseSource string) (*ast.Program, error) {
	result := pretty.Transform(terseSource, pretty.Pretty)
	source := result.Source

	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		return nil, fmt.Errorf("lex error: %s", lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return nil, fmt.Errorf("parse error: %s", parseErr.Message)
	}

	desugared := &ast.Program{TopLevels: make([]ast.TopLevel, len(program.TopLevels))}
	for i, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopTestDecl:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopImplBlock:
			methods := make([]ast.ImplMethod, len(d.Methods))
			for j, m := range d.Methods {
				methods[j] = ast.ImplMethod{Name: m.Name, Body: desugar.Desugar(m.Body)}
			}
			d.Methods = methods
			desugared.TopLevels[i] = d
		default:
			desugared.TopLevels[i] = tl
		}
	}
	return desugared, nil
}

// TestPrettyFlagCheck verifies --pretty expansion works with type-checking.
func TestPrettyFlagCheck(t *testing.T) {
	// Terse source that should type-check after --pretty expansion
	terseSource := `main : () -> <io> () =
  let x = 42
  print(show(x))
`
	program, err := prettyPipeline(terseSource)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}

	typeErrors := checker.TypeCheck(program)
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

// TestPrettyFlagLint verifies --pretty expansion works with linting.
func TestPrettyFlagLint(t *testing.T) {
	terseSource := `main : () -> <io> () =
  let x = 42
  print(show(x))
`
	program, err := prettyPipeline(terseSource)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}

	diags := linter.Lint(program, linter.LintOptions{})
	// Should lint without panicking; diagnostics are informational
	_ = diags
}

// TestPrettyFlagEval verifies --pretty expansion works with evaluation.
func TestPrettyFlagEval(t *testing.T) {
	// Terse source using str.len (terse) which expands to string.length (verbose)
	terseSource := `main : () -> <io> () =
  let x = 42
  print(show(x))
`
	result := pretty.Transform(terseSource, pretty.Pretty)
	output, err := runProgram(result.Source)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 1 || lines[0] != "42" {
		t.Errorf("expected output '42', got %q", output)
	}
}

// TestPrettyFlagPreservesSemantics verifies that terse and verbose forms
// produce the same evaluation result when processed through --pretty.
func TestPrettyFlagPreservesSemantics(t *testing.T) {
	verboseSource := `factorial : (n: Int) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)

main : () -> <io> () =
  print(show(factorial(5)))
`
	// Convert to terse then back through --pretty pipeline
	terseResult := pretty.Transform(verboseSource, pretty.Terse)
	prettyResult := pretty.Transform(terseResult.Source, pretty.Pretty)

	outputDirect, err := runProgram(verboseSource)
	if err != nil {
		t.Fatalf("direct run error: %v", err)
	}

	outputPretty, err := runProgram(prettyResult.Source)
	if err != nil {
		t.Fatalf("pretty run error: %v", err)
	}

	if outputDirect != outputPretty {
		t.Errorf("semantic mismatch:\n  direct: %q\n  pretty: %q", outputDirect, outputPretty)
	}
}

// TestCrossModuleEffectAliasResolution verifies that a pub effect alias exported
// from one module is resolved during type checking of the importing module.
func TestCrossModuleEffectAliasResolution(t *testing.T) {
	// Create temp directory with two module files
	dir := t.TempDir()

	// Library module: exports a pub effect alias
	libSource := `mod myeffects

pub effect logger {
  log_msg : Str -> ()
}

pub effect alias WithLog = <logger, io>
`
	if err := os.WriteFile(filepath.Join(dir, "myeffects.clk"), []byte(libSource), 0644); err != nil {
		t.Fatalf("write lib: %v", err)
	}

	// Main module: imports and uses the alias
	mainSource := `use myeffects (WithLog, log_msg)

greet : () -> <WithLog> () =
  perform log_msg("hello")
  print("done")

main : () -> <io> () =
  handle greet() {
    return v -> v,
    log_msg msg resume k -> k(())
  }
`
	// Parse and type-check the main module with the resolver
	tokens, lexErr := lexer.Lex(mainSource)
	if lexErr != nil {
		t.Fatalf("lex: %s", lexErr.Message)
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		t.Fatalf("parse: %s", parseErr.Message)
	}
	desugared := &ast.Program{TopLevels: make([]ast.TopLevel, len(program.TopLevels))}
	for i, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		default:
			desugared.TopLevels[i] = tl
		}
	}

	// Build resolver that reads module files from temp dir
	aliasResolver := func(modulePath []string) map[string]*checker.EffectAliasInfo {
		modPath, err := eval.ResolveModulePath(modulePath, dir)
		if err != nil {
			return nil
		}
		src, err := os.ReadFile(modPath)
		if err != nil {
			return nil
		}
		toks, lexErr := lexer.Lex(string(src))
		if lexErr != nil {
			return nil
		}
		prog, parseErr := parser.Parse(toks)
		if parseErr != nil {
			return nil
		}
		aliases := map[string]*checker.EffectAliasInfo{}
		for _, tl := range prog.TopLevels {
			if ea, ok := tl.(ast.TopEffectAlias); ok && ea.Pub {
				aliases[ea.Name] = &checker.EffectAliasInfo{
					Params:  ea.Params,
					Effects: ea.Effects,
				}
			}
		}
		if len(aliases) == 0 {
			return nil
		}
		return aliases
	}

	typeErrors := checker.TypeCheckWithResolvers(desugared, nil, aliasResolver)

	// Filter to real errors (not warnings)
	var realErrors []checker.TypeError
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			realErrors = append(realErrors, te)
		}
	}
	if len(realErrors) > 0 {
		msgs := make([]string, len(realErrors))
		for i, e := range realErrors {
			msgs[i] = e.Error()
		}
		t.Fatalf("unexpected type errors:\n%s", strings.Join(msgs, "\n"))
	}

	// Also verify that logger is recognized via the alias (no W401 for it)
	for _, te := range typeErrors {
		if te.Code == "W401" && strings.Contains(te.Message, "logger") {
			t.Errorf("W401 for 'logger' — alias should have propagated from myeffects module")
		}
	}
}

// collectTestFiles finds .clk files in a directory that have expected output.
func collectTestFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.clk"))
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	return matches
}

func TestIntegration(t *testing.T) {
	root := testRoot(t)

	dirs := []struct {
		name     string
		path     string
		useCheck bool // run type checker
	}{
		{"phase1", filepath.Join(root, "phase1"), false},
		{"phase2", filepath.Join(root, "phase2"), false},
		{"phase3", filepath.Join(root, "phase3"), false},
		{"phase5", filepath.Join(root, "phase5"), false},
		{"phase6", filepath.Join(root, "phase6"), true},
		{"phase7", filepath.Join(root, "phase7"), false},
		{"phase8", filepath.Join(root, "phase8"), true},
		{"examples", filepath.Join(root, "examples"), false},
	}

	for _, dir := range dirs {
		t.Run(dir.name, func(t *testing.T) {
			files := collectTestFiles(t, dir.path)
			if len(files) == 0 {
				t.Skipf("no .clk files in %s", dir.path)
			}

			for _, file := range files {
				base := filepath.Base(file)
				t.Run(base, func(t *testing.T) {
					source, err := os.ReadFile(file)
					if err != nil {
						t.Fatalf("read error: %v", err)
					}
					src := string(source)

					// Set BaseDir for module imports
					eval.BaseDir = filepath.Dir(file)

					if dir.useCheck {
						// Error tests: run checker and verify errors are reported
						if isErrorTest(src) {
							typeErrors, pipeErr := checkProgram(src)
							if pipeErr != nil {
								t.Fatalf("pipeline error: %v", pipeErr)
							}
							if len(typeErrors) > 0 {
								return // type error found as expected
							}
							// No type errors — try running; expect a runtime error
							_, runErr := runProgram(src)
							if runErr != nil {
								return // runtime error found as expected
							}
							t.Fatalf("expected error but neither checker nor runtime reported one")
							return
						}
					} else {
						if isErrorTest(src) {
							t.Skip("expects error (type checker)")
						}
					}

					expected, hasExpected := parseExpected(src)
					if !hasExpected {
						t.Skip("no expected output (library module)")
					}

					if dir.useCheck {
						// Run checker (should report no errors for pass tests)
						typeErrors, pipeErr := checkProgram(src)
						if pipeErr != nil {
							t.Fatalf("pipeline error: %v", pipeErr)
						}
						if len(typeErrors) > 0 {
							msgs := make([]string, len(typeErrors))
							for i, e := range typeErrors {
								msgs[i] = e.Error()
							}
							t.Fatalf("unexpected type errors:\n%s", strings.Join(msgs, "\n"))
						}
					}

					output, runErr := runProgram(src)
					if runErr != nil {
						t.Fatalf("runtime error: %v", runErr)
					}

					// Compare output line by line
					actualLines := strings.Split(strings.TrimRight(output, "\n"), "\n")
					if output == "" {
						actualLines = nil
					}

					if len(actualLines) != len(expected) {
						t.Fatalf("output line count mismatch:\nexpected %d lines: %v\ngot %d lines: %v",
							len(expected), expected, len(actualLines), actualLines)
					}

					for i := range expected {
						if actualLines[i] != expected[i] {
							t.Errorf("line %d:\n  expected: %q\n  got:      %q", i+1, expected[i], actualLines[i])
						}
					}
				})
			}
		})
	}
}

// runProgramVM compiles and executes a program through the bytecode VM pipeline.
func runProgramVM(source string) (string, error) {
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		return "", fmt.Errorf("lex error: %s", lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return "", fmt.Errorf("parse error: %s", parseErr.Message)
	}

	desugared := &ast.Program{TopLevels: make([]ast.TopLevel, len(program.TopLevels))}
	for i, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopTestDecl:
			d.Body = desugar.Desugar(d.Body)
			desugared.TopLevels[i] = d
		case ast.TopImplBlock:
			methods := make([]ast.ImplMethod, len(d.Methods))
			for j, m := range d.Methods {
				methods[j] = ast.ImplMethod{Name: m.Name, Body: desugar.Desugar(m.Body)}
			}
			d.Methods = methods
			desugared.TopLevels[i] = d
		default:
			desugared.TopLevels[i] = tl
		}
	}

	mod := compiler.CompileProgram(desugared)
	_, stdout, err := vm.Execute(mod)
	if err != nil {
		return strings.Join(stdout, "\n"), err
	}
	return strings.Join(stdout, "\n"), nil
}

func TestVMIntegration(t *testing.T) {
	root := testRoot(t)

	dirs := []struct {
		name string
		path string
	}{
		{"phase7", filepath.Join(root, "phase7")},
	}

	for _, dir := range dirs {
		t.Run(dir.name, func(t *testing.T) {
			files := collectTestFiles(t, dir.path)
			if len(files) == 0 {
				t.Skipf("no .clk files in %s", dir.path)
			}

			for _, file := range files {
				base := filepath.Base(file)
				t.Run(base, func(t *testing.T) {
					source, err := os.ReadFile(file)
					if err != nil {
						t.Fatalf("read error: %v", err)
					}
					src := string(source)

					if usesImports(src) || isErrorTest(src) {
						t.Skip("skipping (imports or error test)")
					}

					expected, hasExpected := parseExpected(src)
					if !hasExpected {
						t.Skip("no expected output")
					}

					output, runErr := runProgramVM(src)
					if runErr != nil {
						t.Fatalf("VM runtime error: %v", runErr)
					}

					actualLines := strings.Split(strings.TrimRight(output, "\n"), "\n")
					if output == "" {
						actualLines = nil
					}

					if len(actualLines) != len(expected) {
						t.Fatalf("output line count mismatch:\nexpected %d lines: %v\ngot %d lines: %v",
							len(expected), expected, len(actualLines), actualLines)
					}

					for i := range expected {
						if actualLines[i] != expected[i] {
							t.Errorf("line %d:\n  expected: %q\n  got:      %q", i+1, expected[i], actualLines[i])
						}
					}
				})
			}
		})
	}
}
