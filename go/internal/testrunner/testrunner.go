// Package testrunner implements the clank test subcommand: discovery of test
// declarations (test "name" = expr) and test_* functions, execution, filtering,
// and structured result reporting.
package testrunner

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dalurness/clank/internal/ast"
)

// TestResult is the result of running a single test.
type TestResult struct {
	Name       string  `json:"name"`
	Module     string  `json:"module"`
	Status     string  `json:"status"` // "pass" or "fail"
	DurationMs float64 `json:"duration_ms"`
	Failure    *Failure `json:"failure,omitempty"`
}

// Failure describes why a test failed.
type Failure struct {
	Message string `json:"message"`
}

// Summary contains aggregate test counts.
type Summary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// RunResult is the full result of a test run.
type RunResult struct {
	Ok       bool         `json:"ok"`
	Summary  Summary      `json:"summary"`
	Tests    []TestResult `json:"tests"`
	TotalMs  float64      `json:"total_ms"`
}

// TestCase is a discovered test to run.
type TestCase struct {
	Name   string
	Module string
	Body   ast.Expr
}

// ── Discovery ──

// DiscoverTests extracts test cases from a parsed program.
// It finds both `test "name" = expr` declarations and `test_* : () -> ...` functions.
func DiscoverTests(program ast.Program, module string) []TestCase {
	var tests []TestCase
	for _, tl := range program.TopLevels {
		switch tl := tl.(type) {
		case ast.TopTestDecl:
			tests = append(tests, TestCase{
				Name:   tl.Name,
				Module: module,
				Body:   tl.Body,
			})
		case ast.TopDefinition:
			if strings.HasPrefix(tl.Name, "test_") {
				tests = append(tests, TestCase{
					Name:   tl.Name,
					Module: module,
					Body:   tl.Body,
				})
			}
		}
	}
	return tests
}

// FilterTests returns only tests whose names contain the filter string.
func FilterTests(tests []TestCase, filter string) []TestCase {
	if filter == "" {
		return tests
	}
	var filtered []TestCase
	for _, tc := range tests {
		if strings.Contains(tc.Name, filter) {
			filtered = append(filtered, tc)
		}
	}
	return filtered
}

// ── File discovery ──

// DiscoverTestFiles finds .clk files in a directory (non-recursive).
func DiscoverTestFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".clk") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// IsDirectory returns true if the given path is a directory.
func IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ── Execution ──

// EvalFunc is a function that evaluates an expression and returns an error
// (nil on success). The test runner does not depend on any specific evaluator.
type EvalFunc func(expr ast.Expr) error

// RunTests executes a set of test cases using the provided evaluator.
func RunTests(tests []TestCase, eval EvalFunc) RunResult {
	start := time.Now()
	var results []TestResult
	passed, failed := 0, 0

	for _, tc := range tests {
		tStart := time.Now()
		err := eval(tc.Body)
		dur := time.Since(tStart).Seconds() * 1000

		if err != nil {
			failed++
			results = append(results, TestResult{
				Name:       tc.Name,
				Module:     tc.Module,
				Status:     "fail",
				DurationMs: dur,
				Failure:    &Failure{Message: err.Error()},
			})
		} else {
			passed++
			results = append(results, TestResult{
				Name:       tc.Name,
				Module:     tc.Module,
				Status:     "pass",
				DurationMs: dur,
			})
		}
	}

	totalMs := time.Since(start).Seconds() * 1000
	ok := failed == 0 && len(tests) > 0

	return RunResult{
		Ok: ok,
		Summary: Summary{
			Total:  len(tests),
			Passed: passed,
			Failed: failed,
		},
		Tests:   results,
		TotalMs: totalMs,
	}
}

// ExtractModule extracts the module name from the program's mod declaration.
func ExtractModule(program ast.Program) string {
	for _, tl := range program.TopLevels {
		if mod, ok := tl.(ast.TopModDecl); ok {
			return strings.Join(mod.Path, ".")
		}
	}
	return ""
}
