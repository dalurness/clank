package clank_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// CLI tests for `clank eval`: --type, --file, and the --json envelope.
// ════════════════════════════════════════════════════════════════════════════

// evalEnvelope mirrors eval's --json output shape.
type evalEnvelopeT struct {
	OK   bool `json:"ok"`
	Data *struct {
		Value   json.RawMessage `json:"value"`
		Display string          `json:"display"`
		Type    *string         `json:"type"`
		Effects []string        `json:"effects"`
	} `json:"data"`
	Error *struct {
		Stage   string `json:"stage"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Stdout      []string                 `json:"stdout"`
	Diagnostics []map[string]interface{} `json:"diagnostics"`
	Timing      *struct {
		TotalMs *int64 `json:"total_ms"`
	} `json:"timing"`
}

func TestEval_TypeSimpleExpr(t *testing.T) {
	stdout, stderr, exitCode := runClank(t, "eval", "--type", "1 + 2")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", exitCode, stderr)
	}
	if strings.TrimSpace(stdout) != "Int" {
		t.Errorf("expected 'Int', got %q", stdout)
	}
}

func TestEval_TypeFunctionExpr(t *testing.T) {
	stdout, _, exitCode := runClank(t, "eval", "--type", "fn(x: Int) => x + 1")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	got := strings.TrimSpace(stdout)
	if !strings.Contains(got, "Int") || !strings.Contains(got, "->") {
		t.Errorf("expected a function type over Int, got %q", got)
	}
}

func TestEval_TypeBuiltinSymbol(t *testing.T) {
	stdout, _, exitCode := runClank(t, "eval", "--type", "map")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "->") {
		t.Errorf("expected a function type for map, got %q", stdout)
	}
}

func TestEval_TypeShowsBuiltinEffects(t *testing.T) {
	// print performs io; fs.read performs io and exn. eval --type now
	// surfaces builtin effect rows.
	stdout, _, exitCode := runClank(t, "eval", "--type", `print("x")`)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "io") {
		t.Errorf("expected io effect for print, got %q", stdout)
	}

	stdout, _, exitCode = runClank(t, "eval", "--type", "fs.read")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if !strings.Contains(stdout, "io") || !strings.Contains(stdout, "exn") {
		t.Errorf("expected <io, exn> for fs.read, got %q", stdout)
	}
}

func TestEval_TypeJSONBuiltinEffects(t *testing.T) {
	stdout, _, exitCode := runClank(t, "--json", "eval", "--type", `print("x")`)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data *struct {
			Effects []string `json:"effects"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("bad json: %v\n%s", err, stdout)
	}
	if env.Data == nil || len(env.Data.Effects) != 1 || env.Data.Effects[0] != "io" {
		t.Errorf("expected effects [io], got %+v", env.Data)
	}
}

func TestEval_TypeErrorFails(t *testing.T) {
	stdout, stderr, exitCode := runClank(t, "eval", "--type", "1 + true")
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", exitCode, stdout)
	}
	if !strings.Contains(stderr, "type") {
		t.Errorf("expected a type diagnostic on stderr, got %q", stderr)
	}
}

func TestEval_TypeJSON(t *testing.T) {
	stdout, _, exitCode := runClank(t, "--json", "eval", "--type", "1 + 2")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data *struct {
			Type    string   `json:"type"`
			Effects []string `json:"effects"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if !env.OK || env.Data == nil || env.Data.Type != "Int" {
		t.Errorf("expected ok data.type Int, got %s", stdout)
	}
}

func TestEval_JSONEnvelopeSuccess(t *testing.T) {
	stdout, _, exitCode := runClank(t, "--json", "eval", "map([1,2,3], fn(x) => x * 2)")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	var env evalEnvelopeT
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if !env.OK || env.Data == nil {
		t.Fatalf("expected ok with data, got %s", stdout)
	}
	var nums []int
	if err := json.Unmarshal(env.Data.Value, &nums); err != nil || len(nums) != 3 || nums[0] != 2 || nums[1] != 4 || nums[2] != 6 {
		t.Errorf("expected value [2,4,6], got %s", env.Data.Value)
	}
	if env.Data.Display != "[2, 4, 6]" {
		t.Errorf("expected display '[2, 4, 6]', got %q", env.Data.Display)
	}
	if env.Data.Type == nil || *env.Data.Type != "[Int]" {
		t.Errorf("expected type [Int], got %v", env.Data.Type)
	}
	if env.Timing == nil || env.Timing.TotalMs == nil {
		t.Error("expected timing.total_ms")
	}
}

func TestEval_JSONEnvelopeCapturesStdoutAndVMError(t *testing.T) {
	prog := `main : () -> <io> () =
  let _ = print("before")
  let _ = 1 / 0
  ()`
	out, _, code := runClank(t, "--json", "eval", prog)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout: %s", code, out)
	}
	var env evalEnvelopeT
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, out)
	}
	if env.OK {
		t.Error("expected ok: false")
	}
	if env.Error == nil || env.Error.Code != "E003" {
		t.Errorf("expected vm error E003, got %+v", env.Error)
	}
	if len(env.Stdout) != 1 || env.Stdout[0] != "before" {
		t.Errorf("expected stdout [before], got %v", env.Stdout)
	}
}

func TestEval_FileWithExpression(t *testing.T) {
	src := `helper : (n: Int) -> <> Int = n * 3

main : () -> <io> () =
  print("file main should not run")
`
	file := writeTmpFile(t, "scope.clk", src)
	stdout, stderr, exitCode := runClank(t, "eval", "--file", file, "helper(7)")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", exitCode, stderr)
	}
	if strings.Contains(stdout, "file main should not run") {
		t.Error("scope file's main ran — it should be replaced by the expression")
	}
	if strings.TrimSpace(stdout) != "21" {
		t.Errorf("expected 21, got %q", stdout)
	}
}

func TestEval_FileWithoutExpressionEvalsFile(t *testing.T) {
	src := `main : () -> <io> () = print("file ran")
`
	file := writeTmpFile(t, "whole.clk", src)
	stdout, stderr, exitCode := runClank(t, "eval", "--file", file)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, "file ran") {
		t.Errorf("expected file's main output, got %q", stdout)
	}
}

func TestEval_FileShortFlagAlias(t *testing.T) {
	src := `double : (n: Int) -> <> Int = n * 2
`
	file := writeTmpFile(t, "alias.clk", src)
	stdout, _, exitCode := runClank(t, "eval", "-f", file, "double(21)")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if strings.TrimSpace(stdout) != "42" {
		t.Errorf("expected 42, got %q", stdout)
	}
}

func TestEval_TypeWithFileScope(t *testing.T) {
	src := `shout : (s: Str) -> <> Str = str.up(s)
`
	file := writeTmpFile(t, "typescope.clk", src)
	stdout, _, exitCode := runClank(t, "eval", "--type", "--file", file, "shout")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	got := strings.TrimSpace(stdout)
	if !strings.Contains(got, "Str") || !strings.Contains(got, "->") {
		t.Errorf("expected (Str -> Str)-ish type, got %q", got)
	}
}

func TestEval_FileOnlyValidWithEval(t *testing.T) {
	prog := writeTmpFile(t, "p.clk", "main : () -> <io> () = print(\"x\")\n")
	_, stderr, exitCode := runClank(t, "run", "--file", "x.clk", prog)
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	if !strings.Contains(stderr, "--file") {
		t.Errorf("expected --file error, got %q", stderr)
	}
}

func TestEval_TypeOnlyValidWithEval(t *testing.T) {
	file := writeTmpFile(t, "t.clk", "main : () -> <io> () = print(\"x\")\n")
	_, stderr, exitCode := runClank(t, "check", "--type", file)
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	if !strings.Contains(stderr, "--type") {
		t.Errorf("expected --type error, got %q", stderr)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// --json envelope audit: every command emits one {"ok": ...} object.
// ════════════════════════════════════════════════════════════════════════════

func TestJSON_CheckCleanFileEmitsOkEnvelope(t *testing.T) {
	file := writeTmpFile(t, "ok.clk", "main : () -> <io> () = print(\"hello\")\n")
	stdout, _, exitCode := runClank(t, "check", "--json", file)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	var env struct {
		OK          bool                     `json:"ok"`
		Diagnostics []map[string]interface{} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if !env.OK {
		t.Error("expected ok: true")
	}
	if env.Diagnostics == nil {
		t.Error("expected diagnostics: [] (not null)")
	}
}

func TestJSON_RunWrapsProgramStdout(t *testing.T) {
	file := writeTmpFile(t, "run.clk", "main : () -> <io> () = print(\"wrapped\")\n")
	stdout, _, exitCode := runClank(t, "run", "--json", file)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", exitCode, stdout)
	}
	var env struct {
		OK     bool     `json:"ok"`
		Stdout []string `json:"stdout"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if !env.OK || len(env.Stdout) != 1 || env.Stdout[0] != "wrapped" {
		t.Errorf("expected ok with stdout [wrapped], got %s", stdout)
	}
}

func TestJSON_RunParseErrorEmitsEnvelope(t *testing.T) {
	file := writeTmpFile(t, "bad.clk", "main :::= broken\n")
	stdout, _, exitCode := runClank(t, "run", "--json", file)
	if exitCode != 1 {
		t.Fatalf("expected exit 1, got %d", exitCode)
	}
	var env struct {
		OK          bool                     `json:"ok"`
		Diagnostics []map[string]interface{} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if env.OK || len(env.Diagnostics) == 0 {
		t.Errorf("expected ok:false with diagnostics, got %s", stdout)
	}
}

func TestJSON_VersionEmitsEnvelope(t *testing.T) {
	stdout, _, exitCode := runClank(t, "--json", "version")
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	var env struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if !env.OK || env.Version == "" {
		t.Errorf("expected ok with version, got %s", stdout)
	}
}

func TestJSON_FmtCheckEmitsEnvelope(t *testing.T) {
	file := writeTmpFile(t, "fmt.clk", "main : () -> <io> () = print(\"x\")\n")
	stdout, _, _ := runClank(t, "fmt", "--json", "--check", file)
	var env struct {
		OK      *bool `json:"ok"`
		Changed *bool `json:"changed"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if env.OK == nil || env.Changed == nil {
		t.Errorf("expected ok and changed fields, got %s", stdout)
	}
}
