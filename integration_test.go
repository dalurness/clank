package clank_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/checker"
	"github.com/dalurness/clank/internal/compiler"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/linter"
	"github.com/dalurness/clank/internal/loader"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/pkg"
	"github.com/dalurness/clank/internal/pretty"
	"github.com/dalurness/clank/internal/vm"
)

// testRoot returns the path to the project test/ directory.
func testRoot(t *testing.T) string {
	t.Helper()
	return "test"
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

// desugarAll desugars all top-level definition bodies.
func desugarAll(program *ast.Program) *ast.Program {
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
	return desugared
}

// checkProgram lexes, parses, desugars, and type-checks a program.
func checkProgram(source string) ([]checker.TypeError, error) {
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		return nil, fmt.Errorf("lex error: %s", lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return nil, fmt.Errorf("parse error: %s", parseErr.Message)
	}

	desugared := desugarAll(program)
	typeErrors := checker.TypeCheck(desugared)
	return typeErrors, nil
}

// runProgram lexes, parses, desugars, links, compiles and executes a program via the VM.
// baseDir is used to resolve module imports. If empty, no import resolution is done.
func runProgram(source string, baseDir string) (string, error) {
	tokens, lexErr := lexer.Lex(source)
	if lexErr != nil {
		return "", fmt.Errorf("lex error: %s", lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return "", fmt.Errorf("parse error: %s", parseErr.Message)
	}

	desugared := desugarAll(program)

	// Link imports if baseDir is set
	linked := desugared
	if baseDir != "" {
		// Check for package module map
		var packageModules map[string]string
		manifestPath := pkg.FindManifest(baseDir)
		if manifestPath != "" {
			if resolution, err := pkg.ResolvePackages(manifestPath, false); err == nil {
				packageModules = resolution.ModuleMap
			}
		}
		result := loader.LinkWithPackages(desugared, baseDir, packageModules)
		if result.Error != nil {
			return "", result.Error
		}
		linked = result.Program
	}

	mod := compiler.CompileProgram(linked)
	_, stdout, err := vm.Execute(mod)
	if err != nil {
		return strings.Join(stdout, "\n"), err
	}
	return strings.Join(stdout, "\n"), nil
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

	return desugarAll(program), nil
}

// TestPrettyFlagCheck verifies --pretty expansion works with type-checking.
func TestPrettyFlagCheck(t *testing.T) {
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
	_ = diags
}

// TestPrettyFlagEval verifies --pretty expansion works with evaluation.
func TestPrettyFlagEval(t *testing.T) {
	terseSource := `main : () -> <io> () =
  let x = 42
  print(show(x))
`
	result := pretty.Transform(terseSource, pretty.Pretty)
	output, err := runProgram(result.Source, "")
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
	terseResult := pretty.Transform(verboseSource, pretty.Terse)
	prettyResult := pretty.Transform(terseResult.Source, pretty.Pretty)

	outputDirect, err := runProgram(verboseSource, "")
	if err != nil {
		t.Fatalf("direct run error: %v", err)
	}

	outputPretty, err := runProgram(prettyResult.Source, "")
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
	dir := t.TempDir()

	libSource := `mod myeffects

pub effect logger {
  log_msg : Str -> ()
}

pub effect alias WithLog = <logger, io>
`
	if err := os.WriteFile(filepath.Join(dir, "myeffects.clk"), []byte(libSource), 0644); err != nil {
		t.Fatalf("write lib: %v", err)
	}

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
	tokens, lexErr := lexer.Lex(mainSource)
	if lexErr != nil {
		t.Fatalf("lex: %s", lexErr.Message)
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		t.Fatalf("parse: %s", parseErr.Message)
	}
	desugared := desugarAll(program)

	aliasResolver := func(modulePath []string) map[string]*checker.EffectAliasInfo {
		modPath, err := loader.ResolveModulePath(modulePath, dir)
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
		useCheck bool
	}{
		{"phase1", filepath.Join(root, "phase1"), false},
		{"phase2", filepath.Join(root, "phase2"), false},
		{"phase3", filepath.Join(root, "phase3"), false},
		{"phase5", filepath.Join(root, "phase5"), false},
		{"phase6", filepath.Join(root, "phase6"), true},
		{"phase7", filepath.Join(root, "phase7"), false},
		{"examples", filepath.Join(root, "examples"), false},
		{"stdlib", filepath.Join(root, "stdlib"), false},
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
					baseDir := filepath.Dir(file)

					if dir.useCheck {
						if isErrorTest(src) {
							typeErrors, pipeErr := checkProgram(src)
							if pipeErr != nil {
								t.Fatalf("pipeline error: %v", pipeErr)
							}
							if len(typeErrors) > 0 {
								return
							}
							_, runErr := runProgram(src, baseDir)
							if runErr != nil {
								return
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

					output, runErr := runProgram(src, baseDir)
					if runErr != nil {
						t.Fatalf("runtime error: %v", runErr)
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

	// Test cross-package imports via clank.pkg
	t.Run("pkg-import", func(t *testing.T) {
		pkgDir := filepath.Join(root, "phase5", "pkg-import")
		mainFile := filepath.Join(pkgDir, "main.clk")
		source, err := os.ReadFile(mainFile)
		if err != nil {
			t.Skipf("pkg-import test not found: %v", err)
		}
		src := string(source)
		expected, hasExpected := parseExpected(src)
		if !hasExpected {
			t.Skip("no expected output")
		}
		output, runErr := runProgram(src, pkgDir)
		if runErr != nil {
			t.Fatalf("runtime error: %v", runErr)
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
				t.Errorf("line %d: expected %q, got %q", i+1, expected[i], actualLines[i])
			}
		}
	})
}

// TestParallelSpawnTiming verifies that spawned tasks run in real goroutines
// by spawning 3 tasks that each sleep 100ms. If truly parallel, total time
// should be ~100ms, not ~300ms.
func TestParallelSpawnTiming(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let a = spawn(fn() => let _ = sleep(100) in 1)
  let b = spawn(fn() => let _ = sleep(100) in 2)
  let c = spawn(fn() => let _ = sleep(100) in 3)
  let total = await(a) + await(b) + await(c)
  print(show(total))
`
	start := time.Now()
	output, err := runProgram(source, "")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "6" {
		t.Errorf("expected output '6', got %q", output)
	}
	// If parallel: ~100ms. If sequential: ~300ms. Use 250ms as threshold.
	if elapsed > 250*time.Millisecond {
		t.Errorf("expected parallel execution (~100ms), but took %v — tasks may be running sequentially", elapsed)
	}
}

// TestCancellationInterruptsSleep verifies that context cancellation interrupts
// a sleeping task promptly. A task group spawns a child that sleeps for 5 seconds,
// then the body raises. The group should return in well under 5 seconds because
// the context cancellation interrupts the sleep.
func TestCancellationInterruptsSleep(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let result = handle task-group(fn() =>
    let _ = spawn(fn() => sleep(5000))
    raise("cancel now")
  ) {
    return x -> "ok",
    raise msg resume _ -> msg
  }
  print(result)
`
	start := time.Now()
	output, err := runProgram(source, "")
	elapsed := time.Since(start)

	if err != nil {
		// The task group may propagate the child cancellation as a trap.
		// That's OK — we just want to verify it completes promptly.
		if elapsed > 1*time.Second {
			t.Errorf("expected fast cancellation, but took %v — sleep may not be context-aware", elapsed)
		}
		return
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected fast cancellation, but took %v — sleep may not be context-aware", elapsed)
	}
	_ = output
}

// TestCancellationInterruptsChannelRecv verifies that context cancellation
// interrupts a task blocked on channel recv.
func TestCancellationInterruptsChannelRecv(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let result = handle task-group(fn() =>
    let (tx, rx) = channel(0)
    let _ = spawn(fn() => recv(rx))
    raise("cancel recv")
  ) {
    return x -> "ok",
    raise msg resume _ -> msg
  }
  print(result)
`
	start := time.Now()
	output, err := runProgram(source, "")
	elapsed := time.Since(start)

	if err != nil {
		// Child cancellation may surface as a trap — that's OK.
		// The key assertion is timing.
		if elapsed > 1*time.Second {
			t.Errorf("expected fast cancellation of blocked recv, but took %v", elapsed)
		}
		return
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected fast cancellation of blocked recv, but took %v", elapsed)
	}
	_ = output
}

// TestSpawnedTaskPrint verifies that print() from a spawned goroutine
// is captured in Stdout and visible after await.
func TestSpawnedTaskPrint(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let f = spawn(fn() => print("hello from goroutine"))
  let _ = await(f)
  print("hello from main")
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "hello from goroutine" {
		t.Errorf("line 1: expected 'hello from goroutine', got %q", lines[0])
	}
	if lines[1] != "hello from main" {
		t.Errorf("line 2: expected 'hello from main', got %q", lines[1])
	}
}

// TestSpawnErrorPropagation verifies that an error raised in a spawned task
// is propagated when the future is awaited. The spawned task handles its own
// error and returns it as a value, which the parent can read.
func TestSpawnErrorPropagation(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let f = spawn(fn() =>
    handle raise("task error") {
      return x -> "ok",
      raise msg resume _ -> "caught: " ++ msg
    }
  )
  print(await(f))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "caught: task error" {
		t.Errorf("expected 'caught: task error', got %q", output)
	}
}

// TestNestedSpawnThreeLevels verifies that nested goroutine spawning works
// correctly through three levels of depth.
func TestNestedSpawnThreeLevels(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let f = spawn(fn() =>
    let g = spawn(fn() =>
      let h = spawn(fn() => 7)
      await(h) * 3
    )
    await(g) + 1
  )
  print(show(await(f)))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "22" {
		t.Errorf("expected '22', got %q", output)
	}
}

// TestChannelBetweenGoroutines verifies that Go channels work correctly
// when producer and consumer are in separate goroutines.
func TestChannelBetweenGoroutines(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let (tx, rx) = channel(0)
  let producer = spawn(fn() =>
    let _ = send(tx, 10)
    let _ = send(tx, 20)
    send(tx, 30)
  )
  let consumer = spawn(fn() =>
    let a = recv(rx)
    let b = recv(rx)
    let c = recv(rx)
    a + b + c
  )
  let total = await(consumer)
  let _ = await(producer)
  print(show(total))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "60" {
		t.Errorf("expected '60', got %q", output)
	}
}

// TestHttpSetTimeout verifies that http.set-timeout is accepted and doesn't error.
// We can't easily test actual HTTP timeout behavior without a server, but we verify
// the builtin compiles and executes without error.
func TestHttpSetTimeout(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let _ = http.set-timeout(5000)
  print("timeout set")
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "timeout set" {
		t.Errorf("expected 'timeout set', got %q", output)
	}
}

// TestTaskGroupChildFailure verifies that a child task failure in a task group
// is detected. The child handles its own error and returns an error marker.
func TestTaskGroupChildFailure(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let result = task-group(fn() =>
    let a = spawn(fn() => "ok")
    let b = spawn(fn() =>
      handle raise("child failed") {
        return x -> "ok",
        raise msg resume _ -> "error: " ++ msg
      }
    )
    await(a) ++ " " ++ await(b)
  )
  print(result)
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "ok error: child failed" {
		t.Errorf("expected 'ok error: child failed', got %q", output)
	}
}

// ── Tier 2 stdlib tests ──

func TestColSortBy(t *testing.T) {
	source := `
main : () -> <io> () =
  let sorted = col.sortby([3, 1, 4, 1, 5], fn(a, b) => a - b)
  print(show(sorted))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "[1, 1, 3, 4, 5]" {
		t.Errorf("expected '[1, 1, 3, 4, 5]', got %q", output)
	}
}

func TestColGroup(t *testing.T) {
	source := `
main : () -> <io> () =
  let groups = col.group([1, 2, 3, 4, 5, 6], fn(x) => x % 2)
  let _ = print(show(len(groups)))
  print(show(col.sort(map(groups, fn(g) => fst(g)))))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "2" {
		t.Errorf("expected 2 groups, got %q", lines[0])
	}
	if lines[1] != "[0, 1]" {
		t.Errorf("expected group keys '[0, 1]', got %q", lines[1])
	}
}

func TestColScan(t *testing.T) {
	source := `
main : () -> <io> () =
  let running = col.scan([1, 2, 3, 4], 0, fn(acc, x) => acc + x)
  print(show(running))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "[1, 3, 6, 10]" {
		t.Errorf("expected '[1, 3, 6, 10]', got %q", output)
	}
}

func TestIterMapFilterCollect(t *testing.T) {
	source := `
main : () -> <io> () =
  let result = iter.range(1, 11)
    |> iter.filter(fn(x) => x % 2 == 0)
    |> iter.map(fn(x) => x * x)
    |> iter.collect
  print(show(result))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "[4, 16, 36, 64, 100]" {
		t.Errorf("expected '[4, 16, 36, 64, 100]', got %q", output)
	}
}

func TestIterChunkWindow(t *testing.T) {
	source := `
main : () -> <io> () =
  let chunks = iter.range(1, 8) |> iter.chunk(3) |> iter.collect
  let _ = print(show(chunks))
  let wins = iter.range(1, 6) |> iter.window(3) |> iter.collect
  print(show(wins))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "[[1, 2, 3], [4, 5, 6], [7]]" {
		t.Errorf("chunks: expected '[[1, 2, 3], [4, 5, 6], [7]]', got %q", lines[0])
	}
	if lines[1] != "[[1, 2, 3], [2, 3, 4], [3, 4, 5]]" {
		t.Errorf("windows: expected '[[1, 2, 3], [2, 3, 4], [3, 4, 5]]', got %q", lines[1])
	}
}

func TestIterDedup(t *testing.T) {
	source := `
main : () -> <io> () =
  let result = iter.of([1, 1, 2, 2, 3, 1, 1])
    |> iter.dedup
    |> iter.collect
  print(show(result))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "[1, 2, 3, 1]" {
		t.Errorf("expected '[1, 2, 3, 1]', got %q", output)
	}
}

func TestIterScanUnfold(t *testing.T) {
	source := `
type Option<a> = Some(a) | None

main : () -> <io> () =
  # scan: running sum
  let scanned = iter.range(1, 6) |> iter.scan(0, fn(acc, x) => acc + x) |> iter.collect
  let _ = print(show(scanned))

  # unfold: generate fibonacci-like
  let fibs = iter.unfold((0, 1), fn(state) =>
    let a = fst(state)
    let b = snd(state)
    if a > 20 then None
    else Some((a, (b, a + b)))
  ) |> iter.collect
  print(show(fibs))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "[1, 3, 6, 10, 15]" {
		t.Errorf("scan: expected '[1, 3, 6, 10, 15]', got %q", lines[0])
	}
	if lines[1] != "[0, 1, 1, 2, 3, 5, 8, 13]" {
		t.Errorf("unfold: expected '[0, 1, 1, 2, 3, 5, 8, 13]', got %q", lines[1])
	}
}

func TestDtRoundTrip(t *testing.T) {
	source := `
main : () -> <io> () =
  let ts = 1700000000
  let rec = dt.from(ts)
  let back = dt.to(rec)
  let _ = print(show(back == ts))
  # Add 1 hour and check difference
  let added = dt.add(rec, dt.hr(1))
  let diff = dt.sub(added, rec)
  print(show(diff))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "true" {
		t.Errorf("round-trip: expected 'true', got %q", lines[0])
	}
	if lines[1] != "3600000" {
		t.Errorf("dt.sub: expected '3600000', got %q", lines[1])
	}
}

func TestCsvRoundTrip(t *testing.T) {
	source := `
main : () -> <io> () =
  let data = "name,age\nalice,30\nbob,25"
  let parsed = csv.dec(data)
  let encoded = csv.enc(parsed)
  let reparsed = csv.dec(trim(encoded))
  let hdr = csv.hdr(reparsed)
  let _ = print(get(hdr, 0))
  let _ = print(get(hdr, 1))
  let rows = csv.rows(reparsed)
  let first = get(rows, 0)
  print(get(first, 0))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "name" {
		t.Errorf("header[0]: expected 'name', got %q", lines[0])
	}
	if lines[1] != "age" {
		t.Errorf("header[1]: expected 'age', got %q", lines[1])
	}
	if lines[2] != "alice" {
		t.Errorf("data[0][0]: expected 'alice', got %q", lines[2])
	}
}

func TestLogLevel(t *testing.T) {
	source := `
main : () -> <io> () =
  let _ = log.level("error")
  let _ = log.info("should not appear")
  let _ = log.error("should appear")
  print("done")
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "done" {
		t.Errorf("expected 'done', got %q", output)
	}
}

func TestCliParse(t *testing.T) {
	source := `
main : () -> <io> () =
  let args = cli.args()
  print(show(len(args)))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	// cli.args in test context returns os.Args[1:] which varies,
	// but it should produce a valid integer
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		t.Errorf("expected integer output, got empty")
	}
}

func TestSrvStartStop(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let server = srv.new()
    |> srv.get("/health", fn(_req) => srv.res(200, "ok"))
    |> srv.get("/echo", fn(req) => srv.res(200, req.path))
    |> srv.start(18923)

  # Use HTTP client to verify
  let resp = http.get("http://127.0.0.1:18923/health")
  let _ = print(resp.body)

  let resp2 = http.get("http://127.0.0.1:18923/echo")
  let _ = print(resp2.body)

  let _ = srv.stop(server)
  print("stopped")
`
	start := time.Now()
	output, err := runProgram(source, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), output)
	}
	if lines[0] != "ok" {
		t.Errorf("health: expected 'ok', got %q", lines[0])
	}
	if lines[1] != "/echo" {
		t.Errorf("echo: expected '/echo', got %q", lines[1])
	}
	if lines[2] != "stopped" {
		t.Errorf("expected 'stopped', got %q", lines[2])
	}
	if elapsed > 5*time.Second {
		t.Errorf("server test took too long: %v", elapsed)
	}
}

func TestSrvJsonResponse(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let server = srv.new()
    |> srv.get("/data", fn(_req) => srv.json(200, json.enc({name: "test", value: 42})))
    |> srv.start(18924)

  let resp = http.get("http://127.0.0.1:18924/data")
  let _ = print(show(resp.status))
  let _ = print(resp.body)

  srv.stop(server)
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), output)
	}
	if lines[0] != "200" {
		t.Errorf("status: expected '200', got %q", lines[0])
	}
	if !strings.Contains(lines[1], "test") {
		t.Errorf("body should contain 'test', got %q", lines[1])
	}
}

func TestSrvPathParams(t *testing.T) {
	source := `
main : () -> <io, async> () =
  let server = srv.new()
    |> srv.get("/users/:id", fn(req) => srv.res(200, req.params.id))
    |> srv.start(18925)

  let resp = http.get("http://127.0.0.1:18925/users/42")
  let _ = print(resp.body)

  srv.stop(server)
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "42" {
		t.Errorf("expected '42', got %q", output)
	}
}

func TestIterIntersperse(t *testing.T) {
	source := `
main : () -> <io> () =
  let result = iter.of([1, 2, 3])
    |> iter.intersperse(0)
    |> iter.collect
  print(show(result))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "[1, 0, 2, 0, 3]" {
		t.Errorf("expected '[1, 0, 2, 0, 3]', got %q", output)
	}
}

func TestIterMinMax(t *testing.T) {
	source := `
type Option<a> = Some(a) | None

main : () -> <io> () =
  let mn = iter.of([3, 1, 4, 1, 5]) |> iter.min
  let mx = iter.of([3, 1, 4, 1, 5]) |> iter.max
  let _ = print(show(mn))
  print(show(mx))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "1") {
		t.Errorf("min: expected Some(1), got %q", lines[0])
	}
	if !strings.Contains(lines[1], "5") {
		t.Errorf("max: expected Some(5), got %q", lines[1])
	}
}
