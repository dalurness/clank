package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/checker"
	"github.com/dalurness/clank/internal/compiler"
	"github.com/dalurness/clank/internal/desugar"
	"github.com/dalurness/clank/internal/doc"
	"github.com/dalurness/clank/internal/eval"
	"github.com/dalurness/clank/internal/formatter"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/linter"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/pkg"
	"github.com/dalurness/clank/internal/pretty"
	"github.com/dalurness/clank/internal/testrunner"
	"github.com/dalurness/clank/internal/vm"
)

// structuredError is the JSON error output format.
type structuredError struct {
	Stage   string `json:"stage"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
}

func main() {
	os.Exit(run())
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: clank [--json] [command] [flags] <file.clk>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  run [--vm]  Run a program (default)\n")
	fmt.Fprintf(os.Stderr, "  check       Type-check a program\n")
	fmt.Fprintf(os.Stderr, "  eval        Evaluate and print the result\n")
	fmt.Fprintf(os.Stderr, "  fmt         Format source code\n")
	fmt.Fprintf(os.Stderr, "  lint        Lint source code\n")
	fmt.Fprintf(os.Stderr, "  doc         Search and view documentation\n")
	fmt.Fprintf(os.Stderr, "  test        Run tests\n")
	fmt.Fprintf(os.Stderr, "  pkg         Package management\n")
	fmt.Fprintf(os.Stderr, "  pretty      Expand terse identifiers to verbose form\n")
	fmt.Fprintf(os.Stderr, "  terse       Compress verbose identifiers to terse form\n")
}

// getFlagValue returns the value following a flag in args, or "" if not found.
func getFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// hasFlag returns true if a boolean flag is present in args.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func run() int {
	// Manual arg parsing to support: clank [--json] [cmd] [--vm] file.clk
	rawArgs := os.Args[1:]
	jsonOut := false
	useVM := false
	checkMode := false
	stdinMode := false
	command := "run"
	var file string
	var ruleFlags []string

	var positional []string
	for i := 0; i < len(rawArgs); i++ {
		switch rawArgs[i] {
		case "--json":
			jsonOut = true
		case "--vm":
			useVM = true
		case "--check":
			checkMode = true
		case "--stdin":
			stdinMode = true
		case "--rule", "--filter", "--name", "--entry", "--path", "--version", "--github":
			if i+1 < len(rawArgs) {
				if rawArgs[i] == "--rule" {
					ruleFlags = append(ruleFlags, rawArgs[i+1])
				}
				i++ // skip the value
			}
		case "--dev", "--all":
			// boolean flags for pkg/test — just consume
		case "--help", "-h":
			usage()
			return 0
		default:
			if strings.HasPrefix(rawArgs[i], "-") {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n", rawArgs[i])
				return 1
			}
			positional = append(positional, rawArgs[i])
		}
	}

	if len(positional) == 0 {
		usage()
		return 1
	}

	// Commands that handle their own file loading
	switch positional[0] {
	case "doc":
		return cmdDoc(positional[1:], jsonOut, rawArgs)
	case "test":
		return cmdTest(positional[1:], jsonOut, rawArgs)
	case "pkg":
		return cmdPkg(positional[1:], jsonOut, rawArgs)
	}

	// Determine command and file
	switch positional[0] {
	case "run", "check", "eval", "fmt", "lint", "pretty", "terse":
		command = positional[0]
		if (command == "fmt" || command == "pretty" || command == "terse") && stdinMode {
			// fmt/pretty/terse --stdin: no file arg needed
		} else if len(positional) < 2 {
			fmt.Fprintf(os.Stderr, "error: %s requires a file argument\n", command)
			return 1
		} else {
			file = positional[1]
		}
	default:
		file = positional[0]
	}

	// Read source file (or stdin)
	var source []byte
	var err error
	if stdinMode && file == "" {
		source, err = io.ReadAll(os.Stdin)
	} else {
		source, err = os.ReadFile(file)
	}
	if err != nil {
		return reportError(jsonOut, structuredError{
			Stage:   "io",
			Message: err.Error(),
		})
	}

	// Pretty/terse operate on raw source — dispatch before lex/parse
	if command == "pretty" || command == "terse" {
		return cmdPrettyTerse(string(source), command, file, jsonOut, stdinMode)
	}

	// Set BaseDir for module imports
	if file != "" {
		absPath, _ := filepath.Abs(file)
		eval.BaseDir = filepath.Dir(absPath)
	}

	// Lex
	tokens, lexErr := lexer.Lex(string(source))
	if lexErr != nil {
		return reportError(jsonOut, structuredError{
			Stage:   "lex",
			Code:    lexErr.Code,
			Message: lexErr.Message,
			Line:    lexErr.Location.Line,
			Col:     lexErr.Location.Col,
		})
	}

	// Parse
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return reportError(jsonOut, structuredError{
			Stage:   "parse",
			Code:    parseErr.Code,
			Message: parseErr.Message,
			Line:    parseErr.Location.Line,
			Col:     parseErr.Location.Col,
		})
	}

	// Desugar
	desugared := desugarProgram(program)

	switch command {
	case "fmt":
		return cmdFmt(program, string(source), file, jsonOut, checkMode, stdinMode)
	case "lint":
		return cmdLint(program, file, jsonOut, ruleFlags)
	case "check":
		return cmdCheck(desugared, jsonOut)
	case "eval":
		return cmdEval(desugared, jsonOut)
	case "run":
		// Type-check before running (parity with TS reference)
		typeErrors := checker.TypeCheck(desugared)
		hasErrors := false
		for _, te := range typeErrors {
			if !strings.HasPrefix(te.Code, "W") {
				hasErrors = true
				break
			}
		}
		if hasErrors {
			if jsonOut {
				errs := make([]structuredError, 0, len(typeErrors))
				for _, te := range typeErrors {
					if !strings.HasPrefix(te.Code, "W") {
						errs = append(errs, structuredError{
							Stage:   "type",
							Code:    te.Code,
							Message: te.Message,
							Line:    te.Location.Line,
							Col:     te.Location.Col,
						})
					}
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				enc.Encode(errs)
			} else {
				for _, te := range typeErrors {
					if !strings.HasPrefix(te.Code, "W") {
						fmt.Fprintf(os.Stderr, "%s\n", te.Error())
					}
				}
			}
			return 1
		}
		if useVM {
			return cmdRunVM(desugared, jsonOut)
		}
		return cmdRun(desugared, jsonOut)
	}
	return 0
}

// desugarProgram applies desugaring to all definition bodies.
func desugarProgram(program *ast.Program) *ast.Program {
	out := &ast.Program{TopLevels: make([]ast.TopLevel, len(program.TopLevels))}
	for i, tl := range program.TopLevels {
		switch d := tl.(type) {
		case ast.TopDefinition:
			d.Body = desugar.Desugar(d.Body)
			out.TopLevels[i] = d
		case ast.TopTestDecl:
			d.Body = desugar.Desugar(d.Body)
			out.TopLevels[i] = d
		case ast.TopImplBlock:
			methods := make([]ast.ImplMethod, len(d.Methods))
			for j, m := range d.Methods {
				methods[j] = ast.ImplMethod{Name: m.Name, Body: desugar.Desugar(m.Body)}
			}
			d.Methods = methods
			out.TopLevels[i] = d
		default:
			out.TopLevels[i] = tl
		}
	}
	return out
}

// cmdRun executes the program via the tree-walking evaluator.
func cmdRun(program *ast.Program, jsonOut bool) int {
	_, err := eval.Run(program)
	if err != nil {
		return reportRuntimeError(jsonOut, err)
	}
	return 0
}

// cmdRunVM compiles and executes via the bytecode VM.
func cmdRunVM(program *ast.Program, jsonOut bool) int {
	mod := compiler.CompileProgram(program)
	if mod.EntryWordID == nil {
		return 0 // no main function
	}
	result, stdout, err := vm.Execute(mod)
	for _, line := range stdout {
		fmt.Println(line)
	}
	if err != nil {
		if trap, ok := err.(*vm.VMTrap); ok {
			return reportError(jsonOut, structuredError{
				Stage:   "vm",
				Code:    trap.Code,
				Message: trap.Message,
			})
		}
		return reportError(jsonOut, structuredError{
			Stage:   "vm",
			Message: err.Error(),
		})
	}
	_ = result
	return 0
}

// cmdCheck runs the type checker and reports errors.
func cmdCheck(program *ast.Program, jsonOut bool) int {
	typeErrors := checker.TypeCheck(program)
	if len(typeErrors) == 0 {
		if !jsonOut {
			fmt.Println("ok")
		}
		return 0
	}

	if jsonOut {
		errs := make([]structuredError, len(typeErrors))
		for i, te := range typeErrors {
			errs[i] = structuredError{
				Stage:   "type",
				Code:    te.Code,
				Message: te.Message,
				Line:    te.Location.Line,
				Col:     te.Location.Col,
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(errs)
	} else {
		for _, te := range typeErrors {
			fmt.Fprintf(os.Stderr, "%s\n", te.Error())
		}
	}
	return 1
}

// cmdEval runs the program and prints the final result value.
func cmdEval(program *ast.Program, jsonOut bool) int {
	result, err := eval.Run(program)
	if err != nil {
		return reportRuntimeError(jsonOut, err)
	}
	fmt.Println(eval.ShowValue(result))
	return 0
}

// reportError outputs a single structured error and returns exit code 1.
func reportError(jsonOut bool, se structuredError) int {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(se)
	} else {
		var parts []string
		if se.Line > 0 {
			parts = append(parts, fmt.Sprintf("%d:%d", se.Line, se.Col))
		}
		parts = append(parts, se.Stage)
		if se.Code != "" {
			parts = append(parts, fmt.Sprintf("[%s]", se.Code))
		}
		parts = append(parts, se.Message)
		fmt.Fprintf(os.Stderr, "%s\n", strings.Join(parts, " "))
	}
	return 1
}

// reportRuntimeError converts a runtime error to structured output.
func reportRuntimeError(jsonOut bool, err error) int {
	if re, ok := err.(*eval.RuntimeError); ok {
		return reportError(jsonOut, structuredError{
			Stage:   "runtime",
			Code:    re.Code,
			Message: re.Message,
			Line:    re.Location.Line,
			Col:     re.Location.Col,
		})
	}
	return reportError(jsonOut, structuredError{
		Stage:   "runtime",
		Message: err.Error(),
	})
}

// cmdFmt formats source code.
func cmdFmt(program *ast.Program, source string, file string, jsonOut bool, checkMode bool, stdinMode bool) int {
	formatted := formatter.Format(program, source)

	if checkMode {
		if formatted != source {
			if file != "" {
				fmt.Fprintf(os.Stderr, "%s: not formatted\n", file)
			}
			return 1
		}
		return 0
	}

	if stdinMode || file == "" {
		fmt.Print(formatted)
		return 0
	}

	// Write back to file
	if formatted != source {
		if err := os.WriteFile(file, []byte(formatted), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", file, err)
			return 1
		}
	}
	return 0
}

// cmdLint runs the linter on a program.
func cmdLint(program *ast.Program, file string, jsonOut bool, ruleFlags []string) int {
	opts := linter.LintOptions{}

	// Parse rule flags
	for _, flag := range ruleFlags {
		if strings.HasPrefix(flag, "+") {
			if opts.EnabledRules == nil {
				opts.EnabledRules = make(map[string]bool)
			}
			opts.EnabledRules[flag[1:]] = true
		} else if strings.HasPrefix(flag, "-") {
			if opts.DisabledRules == nil {
				opts.DisabledRules = make(map[string]bool)
			}
			opts.DisabledRules[flag[1:]] = true
		}
	}

	diags := linter.Lint(program, opts)

	if jsonOut {
		type envelope struct {
			OK          bool                    `json:"ok"`
			Diagnostics []linter.LintDiagnostic `json:"diagnostics"`
		}
		env := envelope{
			OK:          len(diags) == 0,
			Diagnostics: diags,
		}
		if env.Diagnostics == nil {
			env.Diagnostics = []linter.LintDiagnostic{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(env)
	} else {
		for _, d := range diags {
			fmt.Fprintf(os.Stderr, "%d:%d %s [%s] %s\n", d.Location.Line, d.Location.Col, "lint", d.Code, d.Message)
		}
	}

	return 0
}

// cmdPrettyTerse transforms source between terse and verbose forms.
func cmdPrettyTerse(source string, command string, file string, jsonOut bool, stdinMode bool) int {
	var direction pretty.Direction
	if command == "pretty" {
		direction = pretty.Pretty
	} else {
		direction = pretty.Terse
	}

	result := pretty.Transform(source, direction)

	if jsonOut {
		type envelope struct {
			Source          string `json:"source"`
			Transformations int    `json:"transformations"`
			Direction       string `json:"direction"`
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(envelope{
			Source:          result.Source,
			Transformations: result.Transformations,
			Direction:       result.Direction.String(),
		})
	} else if stdinMode || file == "" {
		fmt.Print(result.Source)
	} else {
		if result.Source != source {
			if err := os.WriteFile(file, []byte(result.Source), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "error writing %s: %v\n", file, err)
				return 1
			}
		}
	}
	return 0
}

// parseFile reads, lexes, and parses a .clk file, returning the desugared program.
func parseFile(file string) (*ast.Program, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	tokens, lexErr := lexer.Lex(string(source))
	if lexErr != nil {
		return nil, fmt.Errorf("lex error: %s", lexErr.Message)
	}

	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return nil, fmt.Errorf("parse error: %s", parseErr.Message)
	}

	return desugarProgram(program), nil
}

// collectClkFiles collects .clk files from a list of file/directory targets.
func collectClkFiles(targets []string) ([]string, error) {
	var files []string
	for _, t := range targets {
		info, err := os.Stat(t)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			found, err := testrunner.DiscoverTestFiles(t)
			if err != nil {
				return nil, err
			}
			files = append(files, found...)
		} else {
			abs, _ := filepath.Abs(t)
			files = append(files, abs)
		}
	}
	return files, nil
}

// ── Doc subcommand ──

// cmdDoc implements: clank doc search <query> [files...] | clank doc show <name> [files...]
func cmdDoc(args []string, jsonOut bool, rawArgs []string) int {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: clank doc search <query> [files...] | clank doc show <name> [files...]\n")
		return 1
	}

	subCmd := args[0]
	if subCmd != "search" && subCmd != "show" {
		fmt.Fprintf(os.Stderr, "usage: clank doc search <query> [files...] | clank doc show <name> [files...]\n")
		return 1
	}

	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "error: no %s provided\n", map[string]string{"search": "query", "show": "name"}[subCmd])
		return 1
	}

	query := args[1]
	fileTargets := args[2:]

	// Collect all entries: builtins + any provided files
	allEntries := doc.GetBuiltinEntries()

	for _, f := range fileTargets {
		program, err := parseFile(f)
		if err != nil {
			continue
		}
		allEntries = append(allEntries, doc.ExtractProgramEntries(*program, f)...)
	}

	if subCmd == "search" {
		results := doc.SearchEntries(allEntries, query)

		if jsonOut {
			data := make([]map[string]interface{}, len(results))
			for i, e := range results {
				data[i] = doc.EntryToMap(e)
			}
			out := map[string]interface{}{
				"query":   query,
				"count":   len(results),
				"entries": data,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"ok": true, "data": out})
		} else {
			if len(results) == 0 {
				fmt.Printf("No results for %q\n", query)
			} else {
				for _, entry := range results {
					fmt.Println(doc.FormatEntryShort(entry))
				}
			}
		}
		return 0
	}

	// show
	entry := doc.FindEntry(allEntries, query)
	if entry == nil {
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{
				"ok":    false,
				"error": fmt.Sprintf("no entry found for '%s'", query),
			})
		} else {
			fmt.Fprintf(os.Stderr, "no entry found for '%s'\n", query)
		}
		return 1
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{"ok": true, "data": doc.EntryToMap(*entry)})
	} else {
		fmt.Println(doc.FormatEntryDetailed(*entry))
	}
	return 0
}

// ── Test subcommand ──

// cmdTest implements: clank test [files...] [--filter <str>] [--json]
func cmdTest(args []string, jsonOut bool, rawArgs []string) int {
	filterFlag := getFlagValue(rawArgs, "--filter")

	// Determine test targets
	targets := args
	if len(targets) == 0 {
		targets = []string{"test/"}
	}

	files, err := collectClkFiles(targets)
	if err != nil || len(files) == 0 {
		msg := "no test files found"
		if len(args) == 0 {
			msg = "no test files found (looked in test/)"
		}
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"ok": false, "error": msg})
		} else {
			fmt.Fprintf(os.Stderr, "error: %s\n", msg)
		}
		return 1
	}

	var allResults []testrunner.TestResult

	for _, file := range files {
		program, err := parseFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", file, err)
			continue
		}

		moduleName := testrunner.ExtractModule(*program)
		if moduleName == "" {
			moduleName = file
		}

		tests := testrunner.DiscoverTests(*program, moduleName)
		if len(tests) == 0 {
			continue
		}

		tests = testrunner.FilterTests(tests, filterFlag)
		if len(tests) == 0 {
			continue
		}

		// Set up evaluator environment for this file
		absFile, _ := filepath.Abs(file)
		eval.BaseDir = filepath.Dir(absFile)
		env := eval.InitGlobalEnv()
		eval.LoadTopLevels(env, program)

		evalFn := func(expr ast.Expr) (retErr error) {
			defer func() {
				if r := recover(); r != nil {
					if re, ok := r.(*eval.RuntimeError); ok {
						retErr = re
						return
					}
					if ps, ok := r.(*eval.PerformSignal); ok {
						retErr = fmt.Errorf("unhandled effect: %s", ps.Op)
						return
					}
					retErr = fmt.Errorf("%v", r)
				}
			}()
			eval.Evaluate(expr, env)
			return nil
		}

		result := testrunner.RunTests(tests, evalFn)
		allResults = append(allResults, result.Tests...)
	}

	totalPassed := 0
	totalFailed := 0
	for _, t := range allResults {
		if t.Status == "pass" {
			totalPassed++
		} else {
			totalFailed++
		}
	}

	ok := totalFailed == 0 && len(allResults) > 0

	if jsonOut {
		type summary struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		}
		type output struct {
			OK      bool                    `json:"ok"`
			Summary summary                 `json:"summary"`
			Tests   []testrunner.TestResult `json:"tests"`
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if allResults == nil {
			allResults = []testrunner.TestResult{}
		}
		enc.Encode(output{
			OK:      ok,
			Summary: summary{Total: len(allResults), Passed: totalPassed, Failed: totalFailed},
			Tests:   allResults,
		})
	} else {
		for _, t := range allResults {
			if t.Status == "pass" {
				fmt.Printf("  ok - %s > %s\n", t.Module, t.Name)
			} else {
				fmt.Printf("  FAIL - %s > %s\n", t.Module, t.Name)
				if t.Failure != nil {
					fmt.Printf("    %s\n", t.Failure.Message)
				}
			}
		}
		fmt.Printf("\n%d tests: %d passed, %d failed\n", len(allResults), totalPassed, totalFailed)
	}

	if ok {
		return 0
	}
	return 1
}

// ── Pkg subcommand ──

// cmdPkg implements: clank pkg <init|resolve|add|remove|verify> [flags]
func cmdPkg(args []string, jsonOut bool, rawArgs []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: clank pkg <init|resolve|add|remove|verify>\n")
		return 1
	}

	subCmd := args[0]

	switch subCmd {
	case "init":
		return cmdPkgInit(jsonOut, rawArgs)
	case "add":
		return cmdPkgAdd(jsonOut, rawArgs)
	case "remove":
		return cmdPkgRemove(jsonOut, rawArgs)
	case "resolve":
		return cmdPkgResolve(jsonOut)
	case "verify":
		return cmdPkgVerify(jsonOut)
	default:
		fmt.Fprintf(os.Stderr, "unknown pkg subcommand: %s\n", subCmd)
		return 1
	}
}

func cmdPkgInit(jsonOut bool, rawArgs []string) int {
	name := getFlagValue(rawArgs, "--name")
	entry := getFlagValue(rawArgs, "--entry")

	result := pkg.PkgInit(pkg.PkgInitOptions{
		Name:  name,
		Entry: entry,
		Dir:   ".",
	})

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok":            result.Ok,
			"package":       result.Package,
			"created_files": result.CreatedFiles,
			"error":         result.Error,
		})
	} else {
		if result.Ok {
			fmt.Printf("Initialized package '%s'\n", result.Package)
			for _, f := range result.CreatedFiles {
				fmt.Printf("  created %s\n", f)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			return 1
		}
	}
	return 0
}

func cmdPkgAdd(jsonOut bool, rawArgs []string) int {
	name := getFlagValue(rawArgs, "--name")
	if name == "" {
		msg := "Missing required --name flag for pkg add"
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"ok": false, "error": msg})
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		}
		return 1
	}

	result := pkg.PkgAdd(pkg.PkgAddOptions{
		Name:       name,
		Constraint: getFlagValue(rawArgs, "--version"),
		Path:       getFlagValue(rawArgs, "--path"),
		GitHub:     getFlagValue(rawArgs, "--github"),
		Dev:        hasFlag(rawArgs, "--dev"),
		Dir:        ".",
	})

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok":         result.Ok,
			"name":       result.Name,
			"section":    result.Section,
			"constraint": result.Constraint,
			"path":       result.Path,
			"github":     result.GitHub,
			"error":      result.Error,
		})
	} else {
		if result.Ok {
			var desc string
			if result.GitHub != "" {
				desc = fmt.Sprintf(`{ github = "%s"`, result.GitHub)
				if result.Constraint != "*" && result.Constraint != "" {
					desc += fmt.Sprintf(`, version = "%s"`, result.Constraint)
				}
				desc += " }"
			} else if result.Path != "" {
				desc = fmt.Sprintf(`{ path = "%s" }`, result.Path)
			} else {
				desc = fmt.Sprintf(`"%s"`, result.Constraint)
			}
			fmt.Printf("Added %s = %s to [%s]\n", result.Name, desc, result.Section)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			return 1
		}
	}
	return 0
}

func cmdPkgRemove(jsonOut bool, rawArgs []string) int {
	name := getFlagValue(rawArgs, "--name")
	if name == "" {
		msg := "Missing required --name flag for pkg remove"
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"ok": false, "error": msg})
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		}
		return 1
	}

	result := pkg.PkgRemove(pkg.PkgRemoveOptions{
		Name: name,
		Dev:  hasFlag(rawArgs, "--dev"),
		Dir:  ".",
	})

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok":      result.Ok,
			"name":    result.Name,
			"section": result.Section,
			"error":   result.Error,
		})
	} else {
		if result.Ok {
			fmt.Printf("Removed %s from [%s]\n", result.Name, result.Section)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			return 1
		}
	}
	return 0
}

func cmdPkgResolve(jsonOut bool) int {
	result := pkg.PkgResolve(".")

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok":       result.Ok,
			"root":     result.Root,
			"packages": result.Packages,
			"error":    result.Error,
		})
	} else {
		if result.Ok {
			if len(result.Packages) == 0 {
				fmt.Println("No local dependencies to resolve.")
			} else {
				fmt.Printf("Resolved %d package(s):\n", len(result.Packages))
				for _, p := range result.Packages {
					fmt.Printf("  %s@%s (%s)\n", p.Name, p.Version, p.Path)
					for _, m := range p.Modules {
						fmt.Printf("    %s\n", m)
					}
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			return 1
		}
	}
	return 0
}

func cmdPkgVerify(jsonOut bool) int {
	manifestPath := pkg.FindManifest(".")
	if manifestPath == "" {
		msg := "No clank.pkg found in current directory or any parent"
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"ok": false, "error": msg})
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		}
		return 1
	}

	result := pkg.VerifyLockfile(manifestPath, true)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok":      result.Ok,
			"stale":   result.Stale,
			"missing": result.Missing,
			"extra":   result.Extra,
		})
	} else {
		if result.Ok {
			fmt.Println("Lockfile is up to date.")
		} else {
			if len(result.Missing) > 0 {
				fmt.Fprintf(os.Stderr, "Missing from lockfile: %s\n", strings.Join(result.Missing, ", "))
			}
			if len(result.Stale) > 0 {
				fmt.Fprintf(os.Stderr, "Stale in lockfile: %s\n", strings.Join(result.Stale, ", "))
			}
			if len(result.Extra) > 0 {
				fmt.Fprintf(os.Stderr, "Extra in lockfile: %s\n", strings.Join(result.Extra, ", "))
			}
			fmt.Fprintf(os.Stderr, "Run 'clank pkg resolve' to update the lockfile.\n")
			return 1
		}
	}
	return 0
}
