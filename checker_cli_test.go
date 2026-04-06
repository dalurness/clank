package clank_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// ── Binary builder (shared across CLI tests) ──

var (
	clankBinary string
	buildOnce   sync.Once
	buildErr    error
)

func buildClank(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "clank-test-bin")
		if err != nil {
			buildErr = err
			return
		}
		binary := filepath.Join(dir, "clank")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", binary, "./cmd/clank")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildError{err: err, output: string(out)}
			return
		}
		clankBinary = binary
	})
	if buildErr != nil {
		t.Fatalf("build failed: %v", buildErr)
	}
	return clankBinary
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string {
	return e.err.Error() + "\n" + e.output
}

// writeTmpFile creates a temporary .clk file and returns its path.
func writeTmpFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// runClank runs the clank binary with the given args and returns stdout, stderr, and exit code.
func runClank(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	binary := buildClank(t)
	cmd := exec.Command(binary, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec error: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// ════════════════════════════════════════════════════════════════════════════
// Checker Hardening Tests (ported from test/checker-hardening.test.ts)
// These verify the type checker correctly infers types for valid programs.
// ════════════════════════════════════════════════════════════════════════════

func TestCheckerHardening_ForComprehensionInfersListElementType(t *testing.T) {
	src := `
double-all : (xs: [Int]) -> <> [Int] =
  for x in xs do mul(x, 2)

main : () -> <io> () = print(show(len(double-all([1, 2, 3]))))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_ForFoldInfersAccumulatorType(t *testing.T) {
	src := `
sum-list : (xs: [Int]) -> <> Int =
  for x in xs fold acc = 0 do add(acc, x)

main : () -> <io> () = print(show(sum-list([1, 2, 3])))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_RangeInfersIntList(t *testing.T) {
	src := `
first-ten : () -> <> [Int] =
  range(1, 10)

main : () -> <io> () = print(show(len(first-ten())))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_MapInfersReturnElementType(t *testing.T) {
	src := `
double-all : (xs: [Int]) -> <> [Int] =
  map(xs, fn(x) => mul(x, 2))

main : () -> <io> () = print(show(len(double-all([1, 2, 3]))))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_FoldInfersReturnTypeFromInit(t *testing.T) {
	src := `
sum-list : (xs: [Int]) -> <> Int =
  fold(xs, 0, fn(acc, x) => add(acc, x))

main : () -> <io> () = print(show(sum-list([1, 2, 3])))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_FilterPreservesElementType(t *testing.T) {
	src := `
positives : (xs: [Int]) -> <> [Int] =
  filter(xs, fn(x) => gt(x, 0))

main : () -> <io> () = print(show(len(positives([1, 2, 3]))))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_LetPolymorphism_IdentityAtMultipleTypes(t *testing.T) {
	src := `
test-poly : () -> <io> () =
  let id = fn(x) => x
  let a = id(42)
  let b = id("hello")
  print(str.cat(show(a), b))

main : () -> <io> () = test-poly()
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_LetPolymorphism_ConstAtMultipleTypes(t *testing.T) {
	src := `
test-const : () -> <io> () =
  let const = fn(x, y) => x
  let a = const(1, "ignored")
  let b = const("hello", 42)
  print(str.cat(show(a), b))

main : () -> <io> () = test-const()
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_CloneReturnsSameType(t *testing.T) {
	src := `
clone-int : (x: Int) -> <> Int = clone(x)

main : () -> <io> () = print(show(clone-int(42)))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_CloneStrReturnsStr(t *testing.T) {
	src := `
clone-str : (s: Str) -> <> Str = clone(s)

main : () -> <io> () = print(clone-str("hi"))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_EqUnifiesBothArgs(t *testing.T) {
	src := `
same-type : (a: Int, b: Int) -> <> Bool = eq(a, b)

main : () -> <io> () = print(show(same-type(1, 2)))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_HeadInfersElementType(t *testing.T) {
	src := `
first : (xs: [Int]) -> <> Int = head(xs)

main : () -> <io> () = print(show(first([1, 2, 3])))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_ConsPreservesElementType(t *testing.T) {
	src := `
prepend : (x: Int, xs: [Int]) -> <> [Int] = cons(x, xs)

main : () -> <io> () = print(show(len(prepend(1, [2, 3]))))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_RevPreservesElementType(t *testing.T) {
	src := `
reversed : (xs: [Int]) -> <> [Int] = rev(xs)

main : () -> <io> () = print(show(len(reversed([1, 2, 3]))))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

func TestCheckerHardening_ChainedFilterThenMap(t *testing.T) {
	src := `
transform : (xs: [Int]) -> <> [Int] =
  map(filter(xs, fn(x) => gt(x, 0)), fn(x) => mul(x, 2))

main : () -> <io> () = print(show(len(transform([1, 2, 3]))))
`
	typeErrors, err := checkProgram(src)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	for _, te := range typeErrors {
		if !strings.HasPrefix(te.Code, "W") {
			t.Errorf("unexpected type error: %s", te.Error())
		}
	}
}

// ════════════════════════════════════════════════════════════════════════════
// Doc Subcommand Tests (ported from test/doc.test.ts)
// These test the `clank doc search` and `clank doc show` CLI commands.
// ════════════════════════════════════════════════════════════════════════════

func TestDoc_SearchBuiltinMap(t *testing.T) {
	stdout, _, exitCode := runClank(t, "doc", "search", "map", "--json")
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if out["ok"] != true {
		t.Fatal("expected ok: true")
	}
	data := out["data"].(map[string]interface{})
	entries := data["entries"].([]interface{})
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	names := extractNames(entries)
	if !containsStr(names, "map") {
		t.Errorf("expected 'map' in results, got %v", names)
	}
}

func TestDoc_SearchBuiltinAdd(t *testing.T) {
	stdout, _, exitCode := runClank(t, "doc", "search", "add", "--json")
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := out["data"].(map[string]interface{})
	entries := data["entries"].([]interface{})
	names := extractNames(entries)
	if !containsStr(names, "add") {
		t.Errorf("expected 'add' in results, got %v", names)
	}
}

func TestDoc_SearchNoResults(t *testing.T) {
	stdout, _, exitCode := runClank(t, "doc", "search", "zzzznothing", "--json")
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := out["data"].(map[string]interface{})
	entries := data["entries"].([]interface{})
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %d", len(entries))
	}
}

func TestDoc_SearchCaseInsensitive(t *testing.T) {
	stdout, _, exitCode := runClank(t, "doc", "search", "MAP", "--json")
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := out["data"].(map[string]interface{})
	entries := data["entries"].([]interface{})
	names := extractNames(entries)
	if !containsStr(names, "map") {
		t.Errorf("expected case-insensitive match for 'map', got %v", names)
	}
}

func TestDoc_ShowBuiltinFold(t *testing.T) {
	stdout, _, exitCode := runClank(t, "doc", "show", "fold", "--json")
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["ok"] != true {
		t.Fatal("expected ok: true")
	}
	data := out["data"].(map[string]interface{})
	if data["name"] != "fold" {
		t.Errorf("expected name 'fold', got %v", data["name"])
	}
	if data["kind"] != "builtin" {
		t.Errorf("expected kind 'builtin', got %v", data["kind"])
	}
}

func TestDoc_ShowNonExistent(t *testing.T) {
	stdout, _, exitCode := runClank(t, "doc", "show", "nonexistent", "--json")
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["ok"] != false {
		t.Error("expected ok: false")
	}
}

func TestDoc_SearchFindsUserDefinedFunction(t *testing.T) {
	src := `
factorial : (n: Int) -> <> Int =
  if n == 0 then 1 else n * factorial(n - 1)

main : () -> <io> () =
  print(show(factorial(5)))
`
	file := writeTmpFile(t, "factorial.clk", src)
	stdout, _, exitCode := runClank(t, "doc", "search", "factorial", "--json", file)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := out["data"].(map[string]interface{})
	entries := data["entries"].([]interface{})
	names := extractNames(entries)
	if !containsStr(names, "factorial") {
		t.Errorf("expected 'factorial' in results, got %v", names)
	}
}

func TestDoc_DocWithNoSubcommand(t *testing.T) {
	_, _, exitCode := runClank(t, "doc", "--json")
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// JSON Output Tests (ported from test/json-output.test.ts)
// These test the `clank check --json` command for structured output.
// ════════════════════════════════════════════════════════════════════════════

func TestJSON_CheckCleanFileProducesExitZero(t *testing.T) {
	src := "main : () -> <io> () = print(\"hello\")\n"
	file := writeTmpFile(t, "json-ok.clk", src)
	_, _, exitCode := runClank(t, "check", file)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestJSON_CheckTypeErrorProducesStructuredJSON(t *testing.T) {
	src := "main : () -> <io> () = print(undefined_var)\n"
	file := writeTmpFile(t, "json-type-err.clk", src)
	stdout, _, exitCode := runClank(t, "check", "--json", file)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	var errs []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &errs); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if len(errs) < 1 {
		t.Fatal("expected at least one diagnostic")
	}
	d := errs[0]
	if d["stage"] != "type" {
		t.Errorf("expected stage 'type', got %v", d["stage"])
	}
	if d["code"] != "E300" {
		t.Errorf("expected code 'E300', got %v", d["code"])
	}
	msg, _ := d["message"].(string)
	if !strings.Contains(msg, "undefined_var") {
		t.Errorf("expected message to mention 'undefined_var', got %q", msg)
	}
}

func TestJSON_CheckMultipleTypeErrors(t *testing.T) {
	src := `
foo : (x: Int) -> <> Int = unknown1
bar : (x: Int) -> <> Int = unknown2
main : () -> <io> () = print("ok")
`
	file := writeTmpFile(t, "json-multi.clk", src)
	stdout, _, exitCode := runClank(t, "check", "--json", file)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	var errs []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &errs); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if len(errs) < 2 {
		t.Errorf("expected at least 2 diagnostics, got %d", len(errs))
	}
}

func TestJSON_RunCleanFileProducesOutput(t *testing.T) {
	src := "main : () -> <io> () = print(\"hello\")\n"
	file := writeTmpFile(t, "json-run.clk", src)
	stdout, _, exitCode := runClank(t, "run", file)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("expected stdout to contain 'hello', got %q", stdout)
	}
}

func TestJSON_RunWithTypeErrorFails(t *testing.T) {
	src := "main : () -> <io> () = print(undefined_var)\n"
	file := writeTmpFile(t, "json-run-err.clk", src)
	_, _, exitCode := runClank(t, "run", file)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// Lint Tests (ported from test/lint.test.ts)
// These test the `clank lint --json` command.
// ════════════════════════════════════════════════════════════════════════════

func TestLint_CleanFileNoWarnings(t *testing.T) {
	src := "main : () -> <io> () = print(\"hello\")\n"
	file := writeTmpFile(t, "lint-clean.clk", src)
	stdout, _, exitCode := runClank(t, "lint", "--json", file)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if env["ok"] != true {
		t.Error("expected ok: true")
	}
	diags := env["diagnostics"].([]interface{})
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics, got %d", len(diags))
	}
}

func TestLint_W100_UnusedVariable(t *testing.T) {
	src := `
main : () -> <io> () =
  let unused = 42
  print("hello")
`
	file := writeTmpFile(t, "lint-unused.clk", src)
	stdout, _, _ := runClank(t, "lint", "--json", file)
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	diags := env["diagnostics"].([]interface{})
	found := false
	for _, d := range diags {
		dm := d.(map[string]interface{})
		if dm["code"] == "W100" {
			found = true
			msg := dm["message"].(string)
			if !strings.Contains(msg, "unused") {
				t.Errorf("expected W100 message to mention 'unused', got %q", msg)
			}
		}
	}
	if !found {
		t.Errorf("expected W100 diagnostic, got %v", diags)
	}
}

func TestLint_W100_UnderscoreIgnored(t *testing.T) {
	src := `
main : () -> <io> () =
  let _ = 42
  print("hello")
`
	file := writeTmpFile(t, "lint-underscore.clk", src)
	stdout, _, _ := runClank(t, "lint", "--json", file)
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	diags := env["diagnostics"].([]interface{})
	for _, d := range diags {
		dm := d.(map[string]interface{})
		if dm["code"] == "W100" {
			t.Error("expected no W100 for underscore variable")
		}
	}
}

func TestLint_W102_ShadowedBinding(t *testing.T) {
	src := `
main : () -> <io> () =
  let x = 1
  let x = 2
  print(show(x))
`
	file := writeTmpFile(t, "lint-shadow.clk", src)
	stdout, _, _ := runClank(t, "lint", "--json", file)
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	diags := env["diagnostics"].([]interface{})
	found := false
	for _, d := range diags {
		dm := d.(map[string]interface{})
		if dm["code"] == "W102" {
			found = true
			msg := dm["message"].(string)
			if !strings.Contains(msg, "shadow") {
				t.Errorf("expected W102 message to mention 'shadow', got %q", msg)
			}
		}
	}
	if !found {
		t.Errorf("expected W102 diagnostic, got %v", diags)
	}
}

func TestLint_W100_UsedVariableNoWarning(t *testing.T) {
	src := `
main : () -> <io> () =
  let x = 42
  print(show(x))
`
	file := writeTmpFile(t, "lint-used.clk", src)
	stdout, _, _ := runClank(t, "lint", "--json", file)
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	diags := env["diagnostics"].([]interface{})
	for _, d := range diags {
		dm := d.(map[string]interface{})
		if dm["code"] == "W100" {
			t.Error("expected no W100 for used variable")
		}
	}
}

func TestLint_JSONEnvelopeStructure(t *testing.T) {
	src := "main : () -> <io> () = print(\"ok\")\n"
	file := writeTmpFile(t, "lint-envelope.clk", src)
	stdout, _, _ := runClank(t, "lint", "--json", file)
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if _, ok := env["ok"]; !ok {
		t.Error("envelope missing 'ok' field")
	}
	if _, ok := env["diagnostics"]; !ok {
		t.Error("envelope missing 'diagnostics' field")
	}
	diags, ok := env["diagnostics"].([]interface{})
	if !ok {
		t.Error("diagnostics is not an array")
	}
	_ = diags
}

// ── Helpers ──

func extractNames(entries []interface{}) []string {
	var names []string
	for _, e := range entries {
		em := e.(map[string]interface{})
		if name, ok := em["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
