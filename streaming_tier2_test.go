package clank_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════════════
// Streaming I/O — Iterator edge cases and lazy evaluation
// ═══════════════════════════════════════════════════════════════════════════

func TestIterOfEmptyList(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([]) |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("expected [], got %q", out)
	}
}

func TestIterRangeEmptyWhenStartGteEnd(t *testing.T) {
	src := `
main : () -> <io> () =
  let r1 = iter.range(5, 5) |> iter.collect
  let _ = print(show(r1))
  let r2 = iter.range(10, 3) |> iter.collect
  print(show(r2))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "[]" {
		t.Errorf("range(5,5): expected [], got %q", lines[0])
	}
	if lines[1] != "[]" {
		t.Errorf("range(10,3): expected [], got %q", lines[1])
	}
}

func TestIterTakeMoreThanAvailable(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1]) |> iter.take(10) |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[1]" {
		t.Errorf("expected [1], got %q", out)
	}
}

func TestIterCountOfEmpty(t *testing.T) {
	src := `
main : () -> <io> () =
  let c = iter.of([]) |> iter.count
  print(show(c))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "0" {
		t.Errorf("expected 0, got %q", out)
	}
}

func TestIterLazyRangeTake(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.range(0, 10000000) |> iter.take(3) |> iter.collect
  print(show(result))
`
	start := time.Now()
	out, err := runProgram(src, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[0, 1, 2]" {
		t.Errorf("expected [0, 1, 2], got %q", out)
	}
	if elapsed > 5*time.Second {
		t.Errorf("lazy range+take should be fast, took %v", elapsed)
	}
}

func TestIterAnyShortCircuits(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.range(0, 10000000) |> iter.any(fn(x) => x > 5)
  print(show(result))
`
	start := time.Now()
	out, err := runProgram(src, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "true" {
		t.Errorf("expected true, got %q", out)
	}
	if elapsed > 5*time.Second {
		t.Errorf("iter.any should short-circuit, took %v", elapsed)
	}
}

func TestIterMapFilterCollectPipeline(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1, 2, 3, 4, 5])
    |> iter.map(fn(x) => x * x)
    |> iter.filter(fn(x) => x > 5)
    |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[9, 16, 25]" {
		t.Errorf("expected [9, 16, 25], got %q", out)
	}
}

func TestIterRangeTakeSumPipeline(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.range(0, 100) |> iter.take(5) |> iter.sum
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "10" {
		t.Errorf("expected 10 (0+1+2+3+4), got %q", out)
	}
}

func TestForLoopOverLazyIterator(t *testing.T) {
	src := `
main : () -> <io> () =
  for x in iter.range(1, 4) do print(show(x))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	expected := []string{"1", "2", "3"}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), out)
	}
	for i, e := range expected {
		if lines[i] != e {
			t.Errorf("line %d: expected %q, got %q", i, e, lines[i])
		}
	}
}

func TestForFoldOverLazyRange(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = for x in iter.range(1, 6) fold acc = 0 do acc + x
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "15" {
		t.Errorf("expected 15, got %q", out)
	}
}

func TestFsStreamLinesPipedThroughIterMap(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-lines.txt")
	err := os.WriteFile(tmpFile, []byte("hello\nworld\nfoo\n"), 0644)
	if err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	// Normalize path separators for Clank (use forward slashes)
	clankPath := strings.ReplaceAll(tmpFile, `\`, `/`)

	src := `
main : () -> <io> () =
  let lines = fs.stream-lines("` + clankPath + `")
    |> iter.map(fn(line) => line ++ "!")
    |> iter.collect
  print(show(lines))
`
	out, runErr := runProgram(src, "")
	if runErr != nil {
		t.Fatalf("error: %v", runErr)
	}
	expected := `[hello!, world!, foo!]`
	if strings.TrimSpace(out) != expected {
		t.Errorf("expected %q, got %q", expected, strings.TrimSpace(out))
	}
}

func TestFsStreamLinesEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "empty.txt")
	err := os.WriteFile(tmpFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	clankPath := strings.ReplaceAll(tmpFile, `\`, `/`)

	src := `
main : () -> <io> () =
  let lines = fs.stream-lines("` + clankPath + `") |> iter.collect
  print(show(lines))
`
	out, runErr := runProgram(src, "")
	if runErr != nil {
		t.Fatalf("error: %v", runErr)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("expected [], got %q", out)
	}
}

func TestProcStreamMultiLineOutput(t *testing.T) {
	src := `
main : () -> <io> () =
  let count = proc.stream("echo hello") |> iter.count
  print(show(count))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// echo produces at least 1 line
	trimmed := strings.TrimSpace(out)
	if trimmed != "1" {
		t.Errorf("expected 1 line from echo, got %q", trimmed)
	}
}

func TestIterFlatmapInnerExpansion(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([2, 3])
    |> iter.flatmap(fn(x) => range(0, x))
    |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// range(0, 2) = [0,1,2], range(0, 3) = [0,1,2,3]
	if strings.TrimSpace(out) != "[0, 1, 2, 0, 1, 2, 3]" {
		t.Errorf("expected [0, 1, 2, 0, 1, 2, 3], got %q", out)
	}
}

func TestIterCycleTake(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1, 2, 3])
    |> iter.cycle
    |> iter.take(7)
    |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[1, 2, 3, 1, 2, 3, 1]" {
		t.Errorf("expected [1, 2, 3, 1, 2, 3, 1], got %q", out)
	}
}

func TestIterRepeatTake(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.repeat(42) |> iter.take(5) |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[42, 42, 42, 42, 42]" {
		t.Errorf("expected [42, 42, 42, 42, 42], got %q", out)
	}
}

func TestIterOnce(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.once(99) |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[99]" {
		t.Errorf("expected [99], got %q", out)
	}
}

func TestIterEmpty(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.empty() |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("expected [], got %q", out)
	}
}

func TestIterIntersperseOnRange(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1, 2, 3]) |> iter.intersperse(0) |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[1, 0, 2, 0, 3]" {
		t.Errorf("expected [1, 0, 2, 0, 3], got %q", out)
	}
}

func TestIterDedupConsecutive(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1, 1, 2, 2, 3, 1]) |> iter.dedup |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[1, 2, 3, 1]" {
		t.Errorf("expected [1, 2, 3, 1], got %q", out)
	}
}

func TestIterScanRunningFold(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1, 2, 3])
    |> iter.scan(0, fn(acc, x) => acc + x)
    |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// scan yields running accumulator after each element (not including initial)
	if strings.TrimSpace(out) != "[1, 3, 6]" {
		t.Errorf("expected [1, 3, 6], got %q", out)
	}
}

func TestIterTakeWhile(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1, 2, 3, 1])
    |> iter.take-while(fn(x) => x < 3)
    |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[1, 2]" {
		t.Errorf("expected [1, 2], got %q", out)
	}
}

func TestIterDropWhile(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of([1, 2, 3, 1])
    |> iter.drop-while(fn(x) => x < 3)
    |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[3, 1]" {
		t.Errorf("expected [3, 1], got %q", out)
	}
}

func TestIterJoin(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.of(["a", "b", "c"]) |> iter.join(", ")
  print(result)
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "a, b, c" {
		t.Errorf("expected 'a, b, c', got %q", out)
	}
}

func TestIterLazyFilterTakeOnLargeRange(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.range(0, 10000000)
    |> iter.filter(fn(x) => x > 999990)
    |> iter.take(3)
    |> iter.collect
  print(show(result))
`
	start := time.Now()
	out, err := runProgram(src, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[999991, 999992, 999993]" {
		t.Errorf("expected [999991, 999992, 999993], got %q", out)
	}
	if elapsed > 30*time.Second {
		t.Errorf("lazy filter+take should complete in reasonable time, took %v", elapsed)
	}
}

func TestIterLazyChainDoesNotMaterializeSecond(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.chain(iter.range(0, 3), iter.range(100, 10000000))
    |> iter.take(5)
    |> iter.collect
  print(show(result))
`
	start := time.Now()
	out, err := runProgram(src, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[0, 1, 2, 100, 101]" {
		t.Errorf("expected [0, 1, 2, 100, 101], got %q", out)
	}
	if elapsed > 5*time.Second {
		t.Errorf("lazy chain should be fast, took %v", elapsed)
	}
}

func TestForLoopFilteredOverIterator(t *testing.T) {
	src := `
main : () -> <io> () =
  for x in iter.of([1, 2, 3, 4, 5]) if x > 2 do print(show(x))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	expected := []string{"3", "4", "5"}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), out)
	}
	for i, e := range expected {
		if lines[i] != e {
			t.Errorf("line %d: expected %q, got %q", i, e, lines[i])
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Tier 2 Builtins — CSV, DateTime, Process
// ═══════════════════════════════════════════════════════════════════════════

func TestCsvMapsReturnsRecordsKeyedByHeader(t *testing.T) {
	src := `
main : () -> <io> () =
  let data = csv.dec("name,age\nalice,30")
  let maps = csv.maps(data)
  let first = get(maps, 0)
  let _ = print(first.name)
  print(first.age)
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "alice" {
		t.Errorf("expected 'alice', got %q", lines[0])
	}
	if lines[1] != "30" {
		t.Errorf("expected '30', got %q", lines[1])
	}
}

func TestDtNowReturnsRecordWithYearMonthDay(t *testing.T) {
	src := `
main : () -> <io> () =
  let now = dt.now()
  let _ = print(show(now.year >= 2025))
  let _ = print(show(now.month >= 1))
  print(show(now.day >= 1))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
	for i, line := range lines {
		if line != "true" {
			t.Errorf("line %d: expected 'true', got %q", i, line)
		}
	}
}

func TestDtFromToRoundtrip(t *testing.T) {
	src := `
main : () -> <io> () =
  let ts = 1735689600
  let rec = dt.from(ts)
  let back = dt.to(rec)
  print(show(back == ts))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "true" {
		t.Errorf("expected true, got %q", out)
	}
}

func TestDtFromFieldValues(t *testing.T) {
	src := `
main : () -> <io> () =
  let rec = dt.from(1735689600)
  let _ = print(show(rec.year))
  let _ = print(show(rec.month))
  print(show(rec.day))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "2025" {
		t.Errorf("year: expected 2025, got %q", lines[0])
	}
	if lines[1] != "1" {
		t.Errorf("month: expected 1, got %q", lines[1])
	}
	if lines[2] != "1" {
		t.Errorf("day: expected 1, got %q", lines[2])
	}
}

func TestDtAddDay(t *testing.T) {
	src := `
main : () -> <io> () =
  let rec = dt.from(1735689600)
  let added = dt.add(rec, dt.day(1))
  let ts = dt.to(added)
  print(show(ts))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// 1735689600 + 86400 = 1735776000
	if strings.TrimSpace(out) != "1735776000" {
		t.Errorf("expected 1735776000, got %q", out)
	}
}

func TestDtSubDatetimes(t *testing.T) {
	src := `
main : () -> <io> () =
  let dt1 = dt.from(1735776000)
  let dt2 = dt.from(1735689600)
  let diff = dt.sub(dt1, dt2)
  print(show(diff))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// 1 day in ms = 86400000
	if strings.TrimSpace(out) != "86400000" {
		t.Errorf("expected 86400000, got %q", out)
	}
}

func TestDtDurationHelpers(t *testing.T) {
	src := `
main : () -> <io> () =
  let _ = print(show(dt.ms(42)))
  let _ = print(show(dt.sec(5)))
  let _ = print(show(dt.min(2)))
  let _ = print(show(dt.hr(1)))
  print(show(dt.day(1)))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	expected := []string{"42", "5000", "120000", "3600000", "86400000"}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), out)
	}
	for i, e := range expected {
		if lines[i] != e {
			t.Errorf("line %d: expected %s, got %q", i, e, lines[i])
		}
	}
}

func TestDtIsoFormat(t *testing.T) {
	src := `
main : () -> <io> () =
  let rec = dt.from(1735689600)
  print(dt.iso(rec))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "2025-01-01") {
		t.Errorf("expected ISO string starting with 2025-01-01, got %q", trimmed)
	}
}

func TestProcRunReturnsRecord(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = proc.run("echo", ["hello"])
  let _ = print(show(result.code))
  print(trim(result.stdout))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "0" {
		t.Errorf("exit code: expected 0, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "hello") {
		t.Errorf("stdout: expected 'hello', got %q", lines[1])
	}
}

func TestProcShRunsShellCommand(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = proc.sh("echo test123")
  print(trim(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(strings.TrimSpace(out), "test123") {
		t.Errorf("expected 'test123', got %q", out)
	}
}

func TestIterEachSideEffects(t *testing.T) {
	src := `
main : () -> <io> () =
  iter.of([1, 2, 3]) |> iter.each(fn(x) => print(show(x)))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	expected := []string{"1", "2", "3"}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %q", len(expected), len(lines), out)
	}
	for i, e := range expected {
		if lines[i] != e {
			t.Errorf("line %d: expected %q, got %q", i, e, lines[i])
		}
	}
}

func TestIterRangeDropTakeCollect(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.range(0, 10) |> iter.drop(3) |> iter.take(4) |> iter.collect
  print(show(result))
`
	out, err := runProgram(src, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.TrimSpace(out) != "[3, 4, 5, 6]" {
		t.Errorf("expected [3, 4, 5, 6], got %q", out)
	}
}

func TestIterLazyScanOnLargeRange(t *testing.T) {
	src := `
main : () -> <io> () =
  let result = iter.range(1, 10000000)
    |> iter.scan(0, fn(acc, x) => acc + x)
    |> iter.take(5)
    |> iter.collect
  print(show(result))
`
	start := time.Now()
	out, err := runProgram(src, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// scan emits running accumulator: 1, 1+2=3, 3+3=6, 6+4=10, 10+5=15
	if strings.TrimSpace(out) != "[1, 3, 6, 10, 15]" {
		t.Errorf("expected [1, 3, 6, 10, 15], got %q", out)
	}
	if elapsed > 5*time.Second {
		t.Errorf("lazy scan should be fast, took %v", elapsed)
	}
}

func TestFsStreamLinesCollect(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "data.txt")
	err := os.WriteFile(tmpFile, []byte("hello\nworld\nfoo\n"), 0644)
	if err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	clankPath := strings.ReplaceAll(tmpFile, `\`, `/`)

	src := `
main : () -> <io> () =
  let lines = fs.stream-lines("` + clankPath + `") |> iter.collect
  print(show(lines))
`
	out, runErr := runProgram(src, "")
	if runErr != nil {
		t.Fatalf("error: %v", runErr)
	}
	expected := `[hello, world, foo]`
	if strings.TrimSpace(out) != expected {
		t.Errorf("expected %q, got %q", expected, strings.TrimSpace(out))
	}
}
