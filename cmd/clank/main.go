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
	"github.com/dalurness/clank/internal/formatter"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/linter"
	"github.com/dalurness/clank/internal/loader"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/pkg"
	"github.com/dalurness/clank/internal/pretty"
	"github.com/dalurness/clank/internal/testrunner"
	"github.com/dalurness/clank/internal/vm"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// structuredError is the JSON error output format.
type structuredError struct {
	Stage   string `json:"stage"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
}

func main() {
	// Propagate the binary's build-time version into the pkg package so any
	// manifest or lockfile this process writes is stamped with the same
	// version the user sees in `clank version`.
	pkg.ClankVersion = Version
	os.Exit(run())
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: clank [--json] [--pretty] [command] [flags] <file.clk>\n")
	fmt.Fprintf(os.Stderr, "       clank run -c '<code>'          Run inline code\n")
	fmt.Fprintf(os.Stderr, "       clank eval '<expr>'            Evaluate an expression\n")
	fmt.Fprintf(os.Stderr, "       clank eval -f <file.clk>       Evaluate a file\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  run         Run a program (default). Use -c for inline code\n")
	fmt.Fprintf(os.Stderr, "  check       Type-check a program. Use -c for inline code\n")
	fmt.Fprintf(os.Stderr, "  eval        Evaluate an expression and print the result\n")
	fmt.Fprintf(os.Stderr, "  fmt         Format source code\n")
	fmt.Fprintf(os.Stderr, "  lint        Lint source code\n")
	fmt.Fprintf(os.Stderr, "  doc, docs   List/search/show docs for a target (project, /path, github URL, dep)\n")
	fmt.Fprintf(os.Stderr, "  test        Run tests\n")
	fmt.Fprintf(os.Stderr, "  pkg         Package management\n")
	fmt.Fprintf(os.Stderr, "  pretty      Expand terse identifiers to verbose form\n")
	fmt.Fprintf(os.Stderr, "  terse       Compress verbose identifiers to terse form\n")
	fmt.Fprintf(os.Stderr, "  spec        Print the language specification\n")
	fmt.Fprintf(os.Stderr, "  version     Print the Clank version\n")
	fmt.Fprintf(os.Stderr, "  update      Update to the latest version\n")
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
	checkMode := false
	stdinMode := false
	prettyMode := false
	command := "run"
	var file string
	var inlineCode string
	var evalFile string
	var ruleFlags []string

	var positional []string
	for i := 0; i < len(rawArgs); i++ {
		switch rawArgs[i] {
		case "--json":
			jsonOut = true
		case "--check":
			checkMode = true
		case "--stdin":
			stdinMode = true
		case "-c":
			if i+1 < len(rawArgs) {
				inlineCode = rawArgs[i+1]
				i++
			} else {
				fmt.Fprintf(os.Stderr, "error: -c requires a code argument\n")
				return 1
			}
			continue
		case "-f":
			if i+1 < len(rawArgs) {
				evalFile = rawArgs[i+1]
				i++
			} else {
				fmt.Fprintf(os.Stderr, "error: -f requires a file argument\n")
				return 1
			}
			continue
		case "--rule", "--filter", "--name", "--entry", "--path", "--version", "--github":
			if i+1 < len(rawArgs) {
				if rawArgs[i] == "--rule" {
					ruleFlags = append(ruleFlags, rawArgs[i+1])
				}
				i++ // skip the value
			}
		case "--pretty":
			prettyMode = true
		case "--dev", "--all", "--yes":
			// boolean flags for pkg/test/doc — just consume
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

	if len(positional) == 0 && inlineCode == "" && evalFile == "" {
		usage()
		return 1
	}

	// Determine which command we're running
	if len(positional) > 0 {
		switch positional[0] {
		// Commands that handle their own file loading
		case "doc", "docs":
			return cmdDoc(positional[1:], jsonOut, rawArgs)
		case "test":
			return cmdTest(positional[1:], jsonOut, rawArgs)
		case "pkg":
			return cmdPkg(positional[1:], jsonOut, rawArgs)
		case "spec":
			return cmdSpec()
		case "version":
			fmt.Println(Version)
			return 0
		case "update":
			return cmdUpdate()

		case "eval":
			command = "eval"
			// eval: remaining positional args are the expression (unless -f is used)
			if evalFile == "" && inlineCode == "" {
				if len(positional) < 2 {
					fmt.Fprintf(os.Stderr, "error: eval requires an expression or -f <file>\n")
					return 1
				}
				inlineCode = strings.Join(positional[1:], " ")
			}

		case "run", "check":
			command = positional[0]
			if inlineCode != "" {
				// -c flag provides the source
			} else if len(positional) < 2 {
				fmt.Fprintf(os.Stderr, "error: %s requires a file argument or -c '<code>'\n", command)
				return 1
			} else {
				file = positional[1]
			}

		case "fmt", "lint", "pretty", "terse":
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
			// No command specified — default to "run" with file
			file = positional[0]
		}
	} else if inlineCode != "" {
		// clank -e '<code>' — defaults to run
		command = "run"
	} else if evalFile != "" {
		// clank -f <file> — defaults to eval
		command = "eval"
	}

	// -f is only valid with eval
	if evalFile != "" && command != "eval" {
		fmt.Fprintf(os.Stderr, "error: -f can only be used with eval\n")
		return 1
	}

	// Read source: inline code, eval file (-f), stdin (--stdin), or file
	var source []byte
	var err error
	if inlineCode != "" {
		source = []byte(inlineCode)
	} else if evalFile != "" {
		source, err = os.ReadFile(evalFile)
		file = evalFile
	} else if stdinMode && file == "" {
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

	// eval: wrap bare expressions in a main function
	if command == "eval" {
		source = []byte(wrapExprSource(string(source)))
	}

	// Pretty/terse operate on raw source — dispatch before lex/parse
	if command == "pretty" || command == "terse" {
		return cmdPrettyTerse(string(source), command, file, jsonOut, stdinMode)
	}

	// --pretty: expand terse source to verbose form before processing
	if prettyMode {
		result := pretty.Transform(string(source), pretty.Pretty)
		source = []byte(result.Source)
	}

	// Set baseDir for module imports
	var baseDir string
	if file != "" {
		absPath, _ := filepath.Abs(file)
		baseDir = filepath.Dir(absPath)
	} else {
		baseDir, _ = os.Getwd()
	}

	// Lex
	tokens, lexErr := lexer.Lex(string(source))
	if lexErr != nil {
		return reportError(jsonOut, structuredError{
			Stage:   "lex",
			Code:    lexErr.Code,
			Message: withFloorHint(lexErr.Message, file),
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
			Message: withFloorHint(parseErr.Message, file),
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
		return cmdCheck(desugared, baseDir, jsonOut)
	case "eval":
		return cmdEval(desugared, baseDir, jsonOut)
	case "run":
		// Link imports first (resolves modules, expands type imports)
		linked := loader.LinkWithPackages(desugared, baseDir, resolvePackageModules(baseDir))
		if linked.Error != nil {
			return reportError(jsonOut, structuredError{
				Stage:   "link",
				Message: withFloorHint(linked.Error.Error(), baseDir),
			})
		}
		// Type-check the linked program
		typeErrors := checker.TypeCheckWithResolvers(linked.Program, nil, makeEffectAliasResolver(baseDir))
		hasErrors := false
		for _, te := range typeErrors {
			if !strings.HasPrefix(te.Code, "W") {
				hasErrors = true
				break
			}
		}
		if hasErrors {
			hint := floorHintForFile(baseDir)
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
				if hint != "" {
					errs = append(errs, structuredError{Stage: "note", Message: hint})
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
				if hint != "" {
					fmt.Fprintf(os.Stderr, "%s\n", hint)
				}
			}
			return 1
		}
		exitCode := cmdRun(linked.Program, jsonOut)
		if exitCode == 0 {
			// Stamp the lockfile with the running binary's version so the
			// project records which clank actually executed it. Scoped to
			// projects (has a clank.pkg) — single-file scripts don't get
			// a surprise lockfile dropped next to them.
			if manifestPath := pkg.FindManifest(baseDir); manifestPath != "" {
				_ = pkg.TouchLockfileVersion(filepath.Dir(manifestPath), Version)
			}
		}
		return exitCode
	}
	return 0
}

// resolvePackageModules checks for a clank.pkg manifest and resolves the
// package module map if one exists. Returns nil if no manifest found.
func resolvePackageModules(baseDir string) map[string]string {
	manifestPath := pkg.FindManifest(baseDir)
	if manifestPath == "" {
		return nil
	}
	resolution, err := pkg.ResolvePackages(manifestPath, false)
	if err != nil {
		return nil
	}
	return resolution.ModuleMap
}

// makeEffectAliasResolver builds a ModuleEffectAliasResolver that parses
// imported module files and extracts their pub effect alias declarations.
func makeEffectAliasResolver(baseDir string) checker.ModuleEffectAliasResolver {
	return func(modulePath []string) map[string]*checker.EffectAliasInfo {
		absPath, err := loader.ResolveModulePath(modulePath, baseDir)
		if err != nil {
			return nil
		}

		source, err := os.ReadFile(absPath)
		if err != nil {
			return nil
		}

		tokens, lexErr := lexer.Lex(string(source))
		if lexErr != nil {
			return nil
		}

		program, parseErr := parser.Parse(tokens)
		if parseErr != nil {
			return nil
		}

		aliases := map[string]*checker.EffectAliasInfo{}
		for _, tl := range program.TopLevels {
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

// cmdRun compiles and executes via the bytecode VM.
func cmdRun(program *ast.Program, jsonOut bool) int {
	mod := compiler.CompileProgram(program)
	if mod.EntryWordID == nil {
		return 0 // no main function
	}
	result, stdout, err := vm.Execute(mod)
	for _, line := range stdout {
		fmt.Println(line)
	}
	if err != nil {
		return reportVMError(jsonOut, err)
	}
	_ = result
	return 0
}

// cmdCheck runs the type checker and reports errors.
func cmdCheck(program *ast.Program, baseDir string, jsonOut bool) int {
	typeErrors := checker.TypeCheckWithResolvers(program, nil, makeEffectAliasResolver(baseDir))
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

// wrapExprSource wraps a bare expression in a main function for eval.
// If the source already contains a top-level definition (has ':' before '='),
// it is returned as-is.
// topLevelKeywords are keywords that can only appear at the start of a
// top-level declaration, never at the start of a bare expression.
var topLevelKeywords = map[string]bool{
	"type": true, "effect": true, "pub": true, "mod": true,
	"use": true, "interface": true, "impl": true, "test": true,
	"affine": true, "opaque": true, "alias": true,
}

func wrapExprSource(source string) string {
	trimmed := strings.TrimSpace(source)
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Check if line starts with a top-level keyword
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		firstWord := fields[0]
		if topLevelKeywords[firstWord] {
			return source
		}
		// Check if first non-comment line looks like a definition (has ':' before '=')
		colonIdx := strings.Index(line, ":")
		eqIdx := strings.Index(line, "=")
		if colonIdx > 0 && (eqIdx < 0 || colonIdx < eqIdx) {
			return source
		}
		break
	}
	// Wrap the expression as the body of main. cmdEval will print the result.
	return fmt.Sprintf("main : () -> <> auto = %s", trimmed)
}

// cmdEval compiles, links, and runs the program, printing the final result value.
func cmdEval(program *ast.Program, baseDir string, jsonOut bool) int {
	linked := loader.LinkWithPackages(program, baseDir, resolvePackageModules(baseDir))
	if linked.Error != nil {
		return reportError(jsonOut, structuredError{
			Stage:   "link",
			Message: linked.Error.Error(),
		})
	}
	mod := compiler.CompileProgram(linked.Program)
	if mod.EntryWordID == nil {
		fmt.Println("()")
		return 0
	}
	result, stdout, err := vm.Execute(mod)
	for _, line := range stdout {
		fmt.Println(line)
	}
	if err != nil {
		return reportVMError(jsonOut, err)
	}
	if result != nil {
		fmt.Println(vm.ValueToString(*result))
	}
	return 0
}

// withFloorHint appends a floor-mismatch note to a message if one applies
// for the given source file. Used at error-report sites so callers can
// keep their existing structuredError construction inline.
func withFloorHint(msg, sourceFile string) string {
	if hint := floorHintForFile(sourceFile); hint != "" {
		return msg + "\n" + hint
	}
	return msg
}

// floorHintForFile returns a one-line advisory note if the manifest owning
// sourceFile (or the nearest ancestor clank.pkg of its directory) declares
// a clank constraint that the current binary version does not satisfy.
// Returns "" when there's no manifest, no constraint, or the constraint is
// already satisfied — including the case where this binary is a dev build,
// which is assumed to satisfy every constraint.
//
// The hint is best-effort and silently returns "" on any error. The
// intent is to enrich unrelated parse/type errors with a "maybe you need
// to upgrade clank" nudge, not to introduce a new failure mode.
func floorHintForFile(sourceFile string) string {
	if sourceFile == "" {
		return ""
	}
	startDir := sourceFile
	if info, err := os.Stat(sourceFile); err == nil && !info.IsDir() {
		startDir = filepath.Dir(sourceFile)
	}
	manifestPath := pkg.FindManifest(startDir)
	if manifestPath == "" {
		return ""
	}
	m, err := pkg.LoadManifest(manifestPath)
	if err != nil || m == nil || m.Clank == "" {
		return ""
	}
	if pkg.SatisfiesOrDev(Version, m.Clank) {
		return ""
	}
	name := m.Name
	if name == "" {
		name = filepath.Base(filepath.Dir(manifestPath))
	}
	return fmt.Sprintf(
		"note: %s declares clank %s, you're running %s — upgrading may resolve this",
		name, m.Clank, Version,
	)
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

// reportVMError converts a VM error to structured output.
func reportVMError(jsonOut bool, err error) int {
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

// cmdDoc implements the target-based doc UX:
//
//	clank doc                          list current project (public only)
//	clank doc <target>                 list target
//	clank doc search <query> [target]  search within target
//	clank doc show <name> [target]     show a single entry
//
// Targets: empty = current project; "/path" = project-relative path;
// "github.com/user/repo" or "user/repo" = remote fetch (prompts y/N);
// "<name>[@version]" = manifest dep, falling back to ~/.clank/cache/.
//
// Flags: --all (include private), --yes (skip fetch prompt), --json.
func cmdDoc(args []string, jsonOut bool, rawArgs []string) int {
	includeAll := hasFlag(rawArgs, "--all")
	yes := hasFlag(rawArgs, "--yes")

	// Drop flags from positional args — ParseArgs already consumed them
	// from rawArgs, but they'll still appear in args as raw tokens.
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		positional = append(positional, a)
	}

	mode := "list"
	var query, target string

	if len(positional) > 0 {
		switch positional[0] {
		case "search":
			if len(positional) < 2 {
				fmt.Fprintf(os.Stderr, "usage: clank doc search <query> [<target>]\n")
				return 1
			}
			mode = "search"
			query = positional[1]
			if len(positional) >= 3 {
				target = positional[2]
			}
		case "show":
			if len(positional) < 2 {
				fmt.Fprintf(os.Stderr, "usage: clank doc show <name> [<target>]\n")
				return 1
			}
			mode = "show"
			query = positional[1]
			if len(positional) >= 3 {
				target = positional[2]
			}
		default:
			target = positional[0]
		}
	}

	baseDir, err := os.Getwd()
	if err != nil {
		baseDir = "."
	}

	fetcher := makeDocFetcher(yes, jsonOut)

	resolved, err := doc.ResolveTarget(target, baseDir, fetcher)
	if err != nil {
		return docError(jsonOut, err)
	}

	// Builtins are always included so agents can discover stdlib from any
	// invocation — they're filtered out of the "public only" test because
	// their Pub field is nil.
	entries := append([]doc.DocEntry{}, doc.GetBuiltinEntries()...)
	for _, f := range resolved.Files {
		program, err := parseFile(f)
		if err != nil {
			continue
		}
		entries = append(entries, doc.ExtractProgramEntries(*program, f)...)
	}

	if !includeAll {
		entries = filterPublicEntries(entries)
	}

	switch mode {
	case "list":
		return docList(entries, resolved, jsonOut)
	case "search":
		return docSearch(query, entries, resolved, jsonOut)
	case "show":
		return docShow(query, entries, jsonOut)
	}
	return 0
}

// makeDocFetcher returns a doc.Fetcher that prompts the user y/N before
// downloading. In --json mode there's no TTY to prompt on, so fetches are
// refused unless --yes was passed. The goal is that agents cannot silently
// pull code off the network.
func makeDocFetcher(yes, jsonOut bool) doc.Fetcher {
	return func(slug, version string) (string, error) {
		if !yes {
			if jsonOut {
				return "", fmt.Errorf("fetching %s requires --yes in --json mode", slug)
			}
			label := "github.com/" + slug
			if version != "" && version != "latest" {
				label += "@" + version
			}
			fmt.Fprintf(os.Stderr, "About to download %s. Continue? [y/N] ", label)
			var reply string
			fmt.Fscanln(os.Stdin, &reply)
			reply = strings.ToLower(strings.TrimSpace(reply))
			if reply != "y" && reply != "yes" {
				return "", fmt.Errorf("fetch cancelled")
			}
		}
		return pkg.FetchGitHub(slug, version)
	}
}

func filterPublicEntries(entries []doc.DocEntry) []doc.DocEntry {
	out := entries[:0]
	for _, e := range entries {
		if e.Pub == nil || *e.Pub {
			out = append(out, e)
		}
	}
	return out
}

func docList(entries []doc.DocEntry, resolved doc.ResolveResult, jsonOut bool) int {
	// Exclude builtins from the "list" view — users asking "what's in this
	// library" don't want the entire stdlib mixed in. Search and show still
	// see builtins.
	var listed []doc.DocEntry
	for _, e := range entries {
		if e.Kind == "builtin" {
			continue
		}
		listed = append(listed, e)
	}

	if jsonOut {
		data := make([]map[string]interface{}, len(listed))
		for i, e := range listed {
			data[i] = doc.EntryToMap(e)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok": true,
			"data": map[string]interface{}{
				"target":  resolved.Label,
				"kind":    resolved.Kind,
				"count":   len(listed),
				"entries": data,
			},
		})
		return 0
	}

	if len(listed) == 0 {
		fmt.Printf("No public entries in %s\n", resolved.Label)
		return 0
	}
	fmt.Printf("%s (%d entries)\n", resolved.Label, len(listed))
	for _, entry := range listed {
		fmt.Println(doc.FormatEntryShort(entry))
	}
	return 0
}

func docSearch(query string, entries []doc.DocEntry, resolved doc.ResolveResult, jsonOut bool) int {
	results := doc.SearchEntries(entries, query)
	if jsonOut {
		data := make([]map[string]interface{}, len(results))
		for i, e := range results {
			data[i] = doc.EntryToMap(e)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok": true,
			"data": map[string]interface{}{
				"query":   query,
				"target":  resolved.Label,
				"count":   len(results),
				"entries": data,
			},
		})
		return 0
	}
	if len(results) == 0 {
		fmt.Printf("No results for %q\n", query)
		return 0
	}
	for _, entry := range results {
		fmt.Println(doc.FormatEntryShort(entry))
	}
	return 0
}

func docShow(name string, entries []doc.DocEntry, jsonOut bool) int {
	entry := doc.FindEntry(entries, name)
	if entry == nil {
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{
				"ok":    false,
				"error": fmt.Sprintf("no entry found for '%s'", name),
			})
		} else {
			fmt.Fprintf(os.Stderr, "no entry found for '%s'\n", name)
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

func docError(jsonOut bool, err error) int {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{"ok": false, "error": err.Error()})
	} else {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
	}
	return 1
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

		// Link imports for this test file
		absFile, _ := filepath.Abs(file)
		fileDir := filepath.Dir(absFile)
		linked := loader.LinkWithPackages(program, fileDir, resolvePackageModules(fileDir))
		if linked.Error != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: link error: %v\n", file, linked.Error)
			continue
		}

		// For each test, we compile and execute a program where the test body
		// replaces main. All other top-levels (definitions, types, impls) are kept.
		baseTops := linked.Program.TopLevels
		evalFn := func(expr ast.Expr) error {
			// Build a program with all definitions + a main that runs the test body
			var testTops []ast.TopLevel
			for _, tl := range baseTops {
				switch tl.(type) {
				case ast.TopTestDecl:
					continue // skip all test declarations
				case ast.TopDefinition:
					d := tl.(ast.TopDefinition)
					if d.Name == "main" {
						continue // skip existing main
					}
					testTops = append(testTops, tl)
				default:
					testTops = append(testTops, tl)
				}
			}
			// Add a synthetic main that executes the test expression
			testTops = append(testTops, ast.TopDefinition{
				Name: "main",
				Sig:  ast.TypeSig{},
				Body: expr,
			})
			testProgram := &ast.Program{TopLevels: testTops}
			mod := compiler.CompileProgram(testProgram)
			if mod.EntryWordID == nil {
				return nil
			}
			_, _, err := vm.Execute(mod)
			return err
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
		fmt.Fprintf(os.Stderr, "usage: clank pkg <init|resolve|add|remove|install|verify>\n")
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
	case "install":
		return cmdPkgInstall(jsonOut, rawArgs)
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

func cmdPkgInstall(jsonOut bool, rawArgs []string) int {
	name := getFlagValue(rawArgs, "--name")

	result := pkg.PkgInstall(pkg.PkgInstallOptions{
		Name: name,
		Dev:  hasFlag(rawArgs, "--dev"),
		Dir:  ".",
	})

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		installed := make([]map[string]interface{}, len(result.Installed))
		for i, p := range result.Installed {
			installed[i] = map[string]interface{}{
				"name":    p.Name,
				"version": p.Version,
				"github":  p.GitHub,
				"path":    p.Path,
			}
		}
		enc.Encode(map[string]interface{}{
			"ok":        result.Ok,
			"installed": installed,
			"error":     result.Error,
		})
	} else {
		if result.Ok {
			if len(result.Installed) == 0 {
				fmt.Println("No GitHub dependencies to install.")
			} else {
				for _, p := range result.Installed {
					fmt.Printf("Installed %s@%s from %s -> %s\n", p.Name, p.Version, p.GitHub, p.Path)
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			return 1
		}
	}
	return 0
}
