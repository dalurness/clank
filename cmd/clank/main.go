package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	File    string `json:"file,omitempty"`
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

// helpTopics holds per-command help text keyed by space-joined command
// path. `clank pkg add --help` resolves to helpTopics["pkg add"]. Lookups
// fall back from the longest prefix match down to the empty string
// (top-level). Keep these next to the commands they document so flag
// changes are caught in review.
var helpTopics = map[string]string{
	"": `Clank — a small ML-ish language for agents.

Usage:
  clank [--json] <command> [flags] [args]

Core commands:
  run <file>              Run a program
  check <file>            Type-check without running
  eval <expr>             Evaluate an expression
  fmt <file>              Format source code
  lint <file>             Lint source code
  test [files...]         Run tests
  doc [target]            Search/show documentation
  pkg <subcommand>        Package management
  spec                    Print the language specification
  skill                   Print/install the agent skill for working with clank
  version                 Print the clank version
  update                  Update the clank binary to the latest release

Global flags:
  --json                  Structured output for agents/tools
  --pretty                Pretty-print (where supported)
  --help, -h              Show help for a command

See 'clank <command> --help' for per-command details.
`,

	"run": `clank run <file> [args...]

Run a Clank program. The file may import other modules via 'use'
(local or '&external') — they'll be resolved automatically. The
file's 'main' function, if present, is called as the entry point.

Arguments after the file — or anything after '--' — are passed to
the program and are visible via cli.args() / cli.parse().

Flags:
  --json    Emit VM errors as structured JSON
`,

	"check": `clank check <file>

Type-check a Clank program without running it. Reports type errors
with location info. Exits 0 on success, 1 on any hard error.

Flags:
  --json    Emit diagnostics as a JSON array
`,

	"eval": `clank eval <expression>
clank eval -f <file>

Evaluate a single Clank expression and print its result. Useful for
quick sanity checks and agent-facing math/string ops.
`,

	"fmt": `clank fmt <file>

Canonical source formatting. Modifies the file in place. Use
--stdin to read from stdin instead. --check exits non-zero if the
file would be changed (useful in CI).
`,

	"lint": `clank lint <file>

Run all enabled lint rules on a single file. Rule codes start with
'W'. Use --rule <name> to enable/disable individual rules.

Flags:
  --json          JSON diagnostics
  --rule <name>   Enable a specific rule
`,

	"test": `clank test [files...] [--filter <substring>]

Run tests. With no arguments, discovers '*_test.clk' files under
./test/. With --filter, only tests whose name contains the
substring run.
`,

	"doc": `clank doc [<target>]
clank doc search <query> [<target>]
clank doc show <name> [<target>]

List, search, or show documentation for a target.

Targets:
  (omitted)                    current project (containing clank.pkg)
  /cmd/main.clk                project-relative path (file or dir)
  ./libs                       cwd-relative path
  github.com/user/repo         remote repo (fetched after y/N prompt)
  user/repo                    same as above (bare slug)
  <lib>[@version]              installed dependency

Flags:
  --all       Include private (non-pub) declarations
  --yes       Skip the y/N prompt for remote fetches (required in --json mode)
  --json      Structured output

Examples:
  clank doc                                list current project
  clank doc ./examples/expense-tracker     list a subtree
  clank doc search fold                    search current project + builtins
  clank doc show map                       show one entry
  clank doc github.com/foo/bar             list a remote package
`,

	"pkg": `clank pkg <subcommand>

Subcommands:
  init [<name>]          Create a new clank.pkg in the current directory
  add <target>           Add a dependency (github URL, slug, or local path)
  install                Fetch + resolve + lockfile + lint for a clean clone
  update [<name>]        Update unpinned github deps to their latest snapshot
  list                   List resolved dependencies (direct + transitive)
  remove <name>          Remove a dependency from the manifest

See 'clank pkg <subcommand> --help' for details.
`,

	"pkg update": `clank pkg update [<name>] [--dev]

Re-fetch unpinned GitHub dependencies (those added without @<ref>)
from their default branch, refresh the cache, and rewrite
clank.lock. With <name>, only that dependency is updated.

Pinned dependencies are skipped — change a pin by re-adding:

  clank pkg add github.com/user/repo@v2.0.0

Flags:
  --dev     Include dev-deps
`,

	"pkg list": `clank pkg list [--dev]

List every dependency the project resolves, direct and transitive,
with version and source. Deps declared in clank.pkg but absent
from the cache are flagged MISSING (fix with 'clank pkg install').

Flags:
  --dev     Include dev-deps
  --json    Structured output
`,

	"pkg init": `clank pkg init [<name>] [--entry <name>]

Create a new clank.pkg in the current directory. With no argument,
uses the directory's base name as the package name. --entry
<name> also creates src/<name>.clk with a stub main function.

The generated manifest records the current clank binary version as
a '>= <version>' floor under the 'clank' field. On a dev build the
field is omitted.
`,

	"pkg add": `clank pkg add <target> [--dev]

Add a dependency. The target is one positional — no flags for name,
version, path, or source. Clank infers everything from the target:

  clank pkg add github.com/user/repo          latest from default branch
  clank pkg add github.com/user/repo@v1.2.3   pinned tag
  clank pkg add user/repo                     bare slug form
  clank pkg add https://github.com/user/repo  URL form
  clank pkg add git@github.com:user/repo.git  SSH form
  clank pkg add ./libs/util                   local path

For github targets, the package is fetched immediately into
~/.clank/cache/, its own clank.pkg is parsed, and its declared
'name' becomes the dep key in your manifest. No --name required.

Flags:
  --dev     Add to [dev-deps] instead of [deps]
`,

	"pkg install": `clank pkg install [--dev]

Post-clone installer. Reads clank.pkg, fetches any GitHub deps
missing from the cache, resolves the full module graph, writes
clank.lock, and runs package-level lint (duplicate-pub-name)
against each resolved dep.

This is the single command to run after cloning a project — it
covers what used to be 'install', 'resolve', and 'verify'.

Flags:
  --dev     Include dev-deps
`,

	"pkg remove": `clank pkg remove <name> [--dev]

Remove a dependency from clank.pkg by name. --dev removes from
the [dev-deps] section instead of [deps].
`,

	"skill": `clank skill [show|install] [--user]

Print or install the agent skill for working with clank — a compact
SKILL.md covering the write/check/run/test loop, syntax essentials,
and package management. Uses the Agent Skills open standard
(agentskills.io), so any compliant harness picks it up: Claude Code,
Codex, Copilot, Cursor, Gemini CLI, and others.

  clank skill                  print to stdout
  clank skill install          write .agents/skills/clank/SKILL.md at
                               the project root
  clank skill install --user   install for all projects (~/.agents/)
`,

	"update": `clank update

Self-update: download the latest clank release for this platform
and replace the running binary in place. 'clank upgrade' is an
alias. Requires write access to the binary's directory (use sudo
or an elevated shell if it lives in a system path).
`,
}

func init() {
	// Command aliases share their primary command's help topic.
	helpTopics["upgrade"] = helpTopics["update"]
	helpTopics["docs"] = helpTopics["doc"]
}

// resolveHelpTopic walks positional args from longest prefix to shortest,
// returning the first matching help topic. Falls back to the top-level
// help text if nothing matches.
func resolveHelpTopic(positional []string) string {
	for i := len(positional); i > 0; i-- {
		key := strings.Join(positional[:i], " ")
		if topic, ok := helpTopics[key]; ok {
			return topic
		}
	}
	return helpTopics[""]
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
	helpRequested := false
	command := "run"
	var file string
	var inlineCode string
	var evalFile string
	var ruleFlags []string

	var positional []string
	var programArgs []string // args after "--" or trailing run args, passed to cli.args
	for i := 0; i < len(rawArgs); i++ {
		if rawArgs[i] == "--" {
			// Everything after "--" belongs to the program under `run`,
			// not to clank.
			programArgs = append(programArgs, rawArgs[i+1:]...)
			break
		}
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
			// Don't dispatch to top-level usage here — let it fall
			// through so we can resolve a subcommand-specific help
			// topic from the collected positional args below.
			helpRequested = true
		default:
			if strings.HasPrefix(rawArgs[i], "-") {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n", rawArgs[i])
				return 1
			}
			positional = append(positional, rawArgs[i])
		}
	}

	// --help / -h resolution: look up the longest matching prefix of
	// positional args in helpTopics. `clank pkg add --help` matches
	// "pkg add", `clank pkg --help` matches "pkg", `clank --help`
	// falls through to the top-level topic.
	if helpRequested {
		fmt.Print(resolveHelpTopic(positional))
		return 0
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
		case "skill":
			return cmdSkill(positional[1:], jsonOut, rawArgs)
		case "version":
			fmt.Println(Version)
			return 0
		case "update", "upgrade":
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
				// Positionals after the file are the program's args.
				programArgs = append(positional[2:], programArgs...)
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
			programArgs = append(positional[1:], programArgs...)
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
	tokens, lexErr := lexer.LexNamed(string(source), filepath.ToSlash(file))
	if lexErr != nil {
		return reportError(jsonOut, structuredError{
			Stage:   "lex",
			Code:    lexErr.Code,
			Message: withFloorHint(lexErr.Message, file),
			File:    lexErr.Location.File,
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
			File:    parseErr.Location.File,
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
		// Link imports first, same as run — checking only the entry file
		// would misreport imported modules and packages (unbound names,
		// unknown types). check and run must always agree.
		checkLinked := loader.LinkWithPackages(desugared, baseDir, resolvePackageFiles(baseDir))
		if checkLinked.Error != nil {
			return reportError(jsonOut, structuredError{
				Stage:   "link",
				Message: withFloorHint(checkLinked.Error.Error(), baseDir),
			})
		}
		return cmdCheck(checkLinked.Program, baseDir, jsonOut)
	case "eval":
		return cmdEval(desugared, baseDir, jsonOut)
	case "run":
		// Link imports first (resolves modules, expands type imports)
		linked := loader.LinkWithPackages(desugared, baseDir, resolvePackageFiles(baseDir))
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
							File:    te.Location.File,
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
		exitCode := cmdRun(linked.Program, programArgs, jsonOut)
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

// resolvePackageFiles checks for a clank.pkg manifest and resolves the
// package file map if one exists. The return value maps each dep package
// name to the list of .clk files in its src/ tree — the loader loads all
// of them and merges their pub symbols into a single flat namespace per
// package. Returns nil if no manifest found.
func resolvePackageFiles(baseDir string) map[string][]string {
	manifestPath := pkg.FindManifest(baseDir)
	if manifestPath == "" {
		return nil
	}
	resolution, err := pkg.ResolvePackages(manifestPath, false)
	if err != nil {
		return nil
	}
	return resolution.PackageFiles
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
func cmdRun(program *ast.Program, programArgs []string, jsonOut bool) int {
	mod := compiler.CompileProgram(program)
	if mod.EntryWordID == nil {
		return 0 // no main function
	}
	result, stdout, err := vm.ExecuteArgs(mod, programArgs)
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
				File:    te.Location.File,
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
	linked := loader.LinkWithPackages(program, baseDir, resolvePackageFiles(baseDir))
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
		if se.Line > 0 && se.File != "" {
			parts = append(parts, fmt.Sprintf("%s:%d:%d", se.File, se.Line, se.Col))
		} else if se.Line > 0 {
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
		msg := trap.Message
		if trap.Word != "" && trap.Word != "unknown" {
			msg = fmt.Sprintf("%s (in %s)", trap.Message, trap.Word)
		}
		return reportError(jsonOut, structuredError{
			Stage:   "vm",
			Code:    trap.Code,
			Message: msg,
			File:    trap.Loc.File,
			Line:    trap.Loc.Line,
			Col:     trap.Loc.Col,
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
			fmt.Fprintf(os.Stderr, "%s %s [%s] %s\n", d.Location, "lint", d.Code, d.Message)
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

	tokens, lexErr := lexer.LexNamed(string(source), filepath.ToSlash(file))
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
	typeFailedFiles := 0

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
		linked := loader.LinkWithPackages(program, fileDir, resolvePackageFiles(fileDir))
		if linked.Error != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: link error: %v\n", file, linked.Error)
			continue
		}

		// Type-check before running — green tests on a file that doesn't
		// compile would be a lie.
		typeErrors := checker.TypeCheckWithResolvers(linked.Program, nil, makeEffectAliasResolver(fileDir))
		hardErrors := false
		for _, te := range typeErrors {
			if !strings.HasPrefix(te.Code, "W") {
				fmt.Fprintf(os.Stderr, "%s: %s\n", file, te.Error())
				hardErrors = true
			}
		}
		if hardErrors {
			typeFailedFiles++
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

	ok := totalFailed == 0 && typeFailedFiles == 0 && len(allResults) > 0

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

// cmdPkg implements: clank pkg <init|add|install|update|list|remove>
//
// `resolve` and `verify` were folded into `install` — both used to produce
// state that made deps importable, but the split confused first-time
// users. `install` now does fetch + resolve + lockfile + lint in one
// step, and `add` does the same when given a new target.
func cmdPkg(args []string, jsonOut bool, rawArgs []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: clank pkg <init|add|install|update|list|remove>\nSee `clank pkg <subcommand> --help` for details.\n")
		return 1
	}

	subCmd := args[0]
	subArgs := args[1:]

	switch subCmd {
	case "init":
		return cmdPkgInit(subArgs, jsonOut, rawArgs)
	case "add":
		return cmdPkgAdd(subArgs, jsonOut, rawArgs)
	case "remove":
		return cmdPkgRemove(subArgs, jsonOut, rawArgs)
	case "install":
		return cmdPkgInstall(jsonOut, rawArgs)
	case "update":
		return cmdPkgUpdate(subArgs, jsonOut, rawArgs)
	case "list":
		return cmdPkgList(jsonOut, rawArgs)
	default:
		fmt.Fprintf(os.Stderr, "unknown pkg subcommand: %s\nsee `clank pkg --help`\n", subCmd)
		return 1
	}
}

func cmdPkgInit(args []string, jsonOut bool, rawArgs []string) int {
	// `clank pkg init <name>` — positional name, falling back to the
	// directory's base name inside PkgInit when omitted.
	var name string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			name = a
			break
		}
	}
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

// cmdPkgAdd implements: clank pkg add <target> [--dev]
//
// The target is one positional: a github slug (user/repo), a GitHub
// URL (https/ssh), or a local path (./libs/util). Github targets are
// fetched eagerly, cached, and the dep is keyed by its own manifest's
// declared name — no --name flag needed.
func cmdPkgAdd(args []string, jsonOut bool, rawArgs []string) int {
	// Drop flags from positional args (main's flag loop already consumed
	// known flags but leaves them in args).
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) == 0 {
		msg := "usage: clank pkg add <target>\n  target: github.com/user/repo, user/repo, git@github.com:user/repo, or ./path"
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"ok": false, "error": msg})
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		}
		return 1
	}

	target := positional[0]
	result := pkg.PkgAddFromTarget(target, hasFlag(rawArgs, "--dev"), ".")

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
			"updated":    result.Updated,
			"error":      result.Error,
		})
		if !result.Ok {
			return 1
		}
		return 0
	}

	if !result.Ok {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		return 1
	}
	var desc string
	switch {
	case result.GitHub != "" && result.Constraint != "*":
		desc = fmt.Sprintf(`{ github = "%s", version = "%s" }`, result.GitHub, result.Constraint)
	case result.GitHub != "":
		desc = fmt.Sprintf(`{ github = "%s" }`, result.GitHub)
	case result.Path != "":
		desc = fmt.Sprintf(`{ path = "%s" }`, result.Path)
	default:
		desc = fmt.Sprintf(`"%s"`, result.Constraint)
	}
	verb := "Added"
	if result.Updated {
		verb = "Updated"
	}
	fmt.Printf("%s %s = %s in [%s]\n", verb, result.Name, desc, result.Section)
	return 0
}

// cmdPkgRemove implements: clank pkg remove <name> [--dev]
func cmdPkgRemove(args []string, jsonOut bool, rawArgs []string) int {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) == 0 {
		msg := "usage: clank pkg remove <name>"
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(map[string]interface{}{"ok": false, "error": msg})
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		}
		return 1
	}

	result := pkg.PkgRemove(pkg.PkgRemoveOptions{
		Name: positional[0],
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

// cmdPkgInstall implements the collapsed post-clone install flow.
// One command: fetch missing GitHub deps, resolve the full module graph,
// rewrite the lockfile, and run package-level lint (duplicate-pub-name)
// against each resolved dep. This replaces what used to be three
// separate commands (install/resolve/verify) with overlapping
// responsibilities.
func cmdPkgInstall(jsonOut bool, rawArgs []string) int {
	// Step 1: fetch any missing GitHub deps into the global cache.
	installResult := pkg.PkgInstall(pkg.PkgInstallOptions{
		Dev: hasFlag(rawArgs, "--dev"),
		Dir: ".",
	})
	if !installResult.Ok {
		return pkgInstallError(jsonOut, installResult.Error)
	}

	// Step 2: resolve the module graph. This picks up everything in
	// ~/.clank/cache/ that matches the manifest.
	manifestPath := pkg.FindManifest(".")
	if manifestPath == "" {
		return pkgInstallError(jsonOut, "No clank.pkg found in current directory or any parent")
	}
	resolution, err := pkg.ResolvePackages(manifestPath, hasFlag(rawArgs, "--dev"))
	if err != nil {
		return pkgInstallError(jsonOut, err.Error())
	}

	// Step 3: write the lockfile.
	if _, err := pkg.WriteLockfile(manifestPath, hasFlag(rawArgs, "--dev")); err != nil {
		return pkgInstallError(jsonOut, "writing lockfile: "+err.Error())
	}

	// Step 4: run the package-level linter across each resolved dep.
	// Duplicate-pub-name collisions within a single dep turn into
	// install-time errors so consumers never hit them at use site.
	var lintIssues []string
	for _, dep := range resolution.Packages {
		diags := linter.LintPackage(dep.Files)
		for _, d := range diags {
			lintIssues = append(lintIssues, fmt.Sprintf("[%s] %s: %s", dep.Name, d.Code, d.Message))
		}
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		installed := make([]map[string]interface{}, len(installResult.Installed))
		for i, p := range installResult.Installed {
			installed[i] = map[string]interface{}{
				"name":    p.Name,
				"version": p.Version,
				"github":  p.GitHub,
				"path":    p.Path,
			}
		}
		resolved := make([]map[string]interface{}, len(resolution.Packages))
		for i, p := range resolution.Packages {
			resolved[i] = map[string]interface{}{
				"name":    p.Name,
				"version": p.Manifest.Version,
				"path":    p.Path,
				"files":   p.Files,
			}
		}
		enc.Encode(map[string]interface{}{
			"ok":          true,
			"installed":   installed,
			"resolved":    resolved,
			"lint_issues": lintIssues,
		})
		if len(lintIssues) > 0 {
			return 1
		}
		return 0
	}

	if len(installResult.Installed) == 0 && len(resolution.Packages) == 0 {
		fmt.Println("No dependencies to install.")
	} else {
		for _, p := range installResult.Installed {
			fmt.Printf("Installed %s@%s from %s\n", p.Name, p.Version, p.GitHub)
		}
		fmt.Printf("Resolved %d package(s), wrote clank.lock.\n", len(resolution.Packages))
	}
	if len(lintIssues) > 0 {
		fmt.Fprintf(os.Stderr, "\nPackage lint issues found:\n")
		for _, issue := range lintIssues {
			fmt.Fprintf(os.Stderr, "  %s\n", issue)
		}
		return 1
	}
	return 0
}

// cmdPkgUpdate implements: clank pkg update [<name>] [--dev]
//
// Re-fetches unpinned GitHub deps from their default branch, refreshes
// the cache and lockfile, and reports what moved. Pinned deps are left
// alone — change a pin with `clank pkg add user/repo@<ref>`.
func cmdPkgUpdate(args []string, jsonOut bool, rawArgs []string) int {
	var name string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			name = a
			break
		}
	}

	result := pkg.PkgUpdate(pkg.PkgUpdateOptions{
		Name: name,
		Dev:  hasFlag(rawArgs, "--dev"),
		Dir:  ".",
	})

	if jsonOut {
		updated := make([]map[string]interface{}, len(result.Updated))
		for i, u := range result.Updated {
			updated[i] = map[string]interface{}{
				"name":        u.Name,
				"github":      u.GitHub,
				"old_version": u.OldVersion,
				"new_version": u.NewVersion,
				"changed":     u.Changed,
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok":      result.Ok,
			"updated": updated,
			"skipped": result.Skipped,
			"error":   result.Error,
		})
		if !result.Ok {
			return 1
		}
		return 0
	}

	if !result.Ok {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		return 1
	}
	if len(result.Updated) == 0 && len(result.Skipped) == 0 {
		fmt.Println("No GitHub dependencies to update.")
		return 0
	}
	for _, u := range result.Updated {
		switch {
		case u.Changed && u.OldVersion != "":
			fmt.Printf("Updated %s: %s -> %s\n", u.Name, u.OldVersion, u.NewVersion)
		case u.Changed:
			fmt.Printf("Updated %s to %s\n", u.Name, u.NewVersion)
		default:
			fmt.Printf("Refreshed %s (still %s)\n", u.Name, u.NewVersion)
		}
	}
	for _, s := range result.Skipped {
		fmt.Printf("Skipped %s (pinned — use `clank pkg add <repo>@<ref>` to change the pin)\n", s)
	}
	return 0
}

// cmdPkgList implements: clank pkg list [--dev]
//
// Shows every dependency the project resolves to, direct and transitive,
// with versions and where they came from.
func cmdPkgList(jsonOut bool, rawArgs []string) int {
	manifestPath := pkg.FindManifest(".")
	if manifestPath == "" {
		return pkgInstallError(jsonOut, "No clank.pkg found in current directory or any parent")
	}
	manifest, err := pkg.LoadManifest(manifestPath)
	if err != nil {
		return pkgInstallError(jsonOut, err.Error())
	}
	includeDev := hasFlag(rawArgs, "--dev")
	resolution, err := pkg.ResolvePackages(manifestPath, includeDev)
	if err != nil {
		return pkgInstallError(jsonOut, err.Error())
	}

	direct := make(map[string]pkg.Dependency)
	for k, v := range manifest.Deps {
		direct[k] = v
	}
	if includeDev {
		for k, v := range manifest.DevDeps {
			direct[k] = v
		}
	}
	resolvedNames := make(map[string]bool)

	type row struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		Source     string `json:"source"`
		Direct     bool   `json:"direct"`
		Constraint string `json:"constraint,omitempty"`
		Path       string `json:"path"`
	}
	var rows []row
	for _, p := range resolution.Packages {
		resolvedNames[p.Name] = true
		src := "path"
		constraint := ""
		d, isDirect := direct[p.Name]
		if isDirect {
			constraint = d.Constraint
			if d.GitHub != "" {
				src = "github.com/" + d.GitHub
			}
		} else if p.Manifest.Repository != "" {
			src = p.Manifest.Repository
		}
		rows = append(rows, row{
			Name:       p.Name,
			Version:    p.Manifest.Version,
			Source:     src,
			Direct:     isDirect,
			Constraint: constraint,
			Path:       p.Path,
		})
	}

	// Deps declared in the manifest but not resolvable right now.
	var missing []string
	for name := range direct {
		if !resolvedNames[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{
			"ok":       true,
			"packages": rows,
			"missing":  missing,
		})
		return 0
	}

	if len(rows) == 0 && len(missing) == 0 {
		fmt.Println("No dependencies.")
		return 0
	}
	for _, r := range rows {
		kind := "direct"
		if !r.Direct {
			kind = "transitive"
		}
		fmt.Printf("%s@%s  %s  (%s)\n", r.Name, r.Version, r.Source, kind)
	}
	for _, name := range missing {
		fmt.Printf("%s  MISSING — run `clank pkg install`\n", name)
	}
	return 0
}

func pkgInstallError(jsonOut bool, msg string) int {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{"ok": false, "error": msg})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
	return 1
}

