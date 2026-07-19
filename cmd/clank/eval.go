package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/checker"
	"github.com/dalurness/clank/internal/compiler"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/loader"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/pretty"
	"github.com/dalurness/clank/internal/vm"
)

// evalRequest carries everything `clank eval` needs. eval is the single
// inline-execution command; its inputs compose:
//
//	expr only            evaluate the expression (bare exprs are wrapped
//	                     in main; full programs run as-is)
//	--stdin              read the expression from stdin instead
//	--file f             no expression: evaluate the file itself
//	--file f + expr      evaluate the expression with the file's
//	                     definitions in scope (its main is replaced)
//	--type               print the inferred type instead of running
type evalRequest struct {
	expr     string
	file     string
	stdin    bool
	typeMode bool
	jsonOut  bool
	pretty   bool
}

// evalData is the data section of eval's --json envelope. Value is the
// result as native JSON when it has a clean JSON form (data values);
// Display is always the clank rendering. Type/Effects come from the
// checker and Type is null when hard type errors made inference
// unreliable.
type evalData struct {
	Value   json.RawMessage `json:"value"`
	Display string          `json:"display"`
	Type    *string         `json:"type"`
	Effects []string        `json:"effects"`
}

type evalTiming struct {
	TotalMs int64 `json:"total_ms"`
}

type evalEnvelope struct {
	OK          bool              `json:"ok"`
	Data        *evalData         `json:"data,omitempty"`
	Error       *structuredError  `json:"error,omitempty"`
	Stdout      []string          `json:"stdout"`
	Diagnostics []structuredError `json:"diagnostics"`
	Timing      evalTiming        `json:"timing"`
}

// typeEnvelope is eval --type's --json output.
type typeEnvelope struct {
	OK          bool              `json:"ok"`
	Data        *typeData         `json:"data,omitempty"`
	Diagnostics []structuredError `json:"diagnostics"`
}

type typeData struct {
	Type    string   `json:"type"`
	Effects []string `json:"effects"`
}

func cmdEval(req evalRequest) int {
	start := time.Now()

	exprSrc := req.expr
	if req.stdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return reportError(req.jsonOut, structuredError{Stage: "io", Message: err.Error()})
		}
		exprSrc = string(stripBOM(b))
	}

	var baseDir string
	var scopeProgram *ast.Program // --file's parsed program when an expression is present
	evalName := ""                // file name attached to the eval source for error locations

	if req.file != "" {
		absPath, _ := filepath.Abs(req.file)
		baseDir = filepath.Dir(absPath)
		if strings.TrimSpace(exprSrc) == "" {
			// No expression: the file itself is the code to evaluate.
			b, err := os.ReadFile(req.file)
			if err != nil {
				return reportError(req.jsonOut, structuredError{Stage: "io", Message: err.Error()})
			}
			exprSrc = string(stripBOM(b))
			evalName = filepath.ToSlash(req.file)
		} else {
			p, se := parseEvalScopeFile(req.file)
			if se != nil {
				return reportError(req.jsonOut, *se)
			}
			scopeProgram = p
		}
	} else {
		baseDir, _ = os.Getwd()
	}

	if strings.TrimSpace(exprSrc) == "" {
		return reportError(req.jsonOut, structuredError{
			Stage:   "io",
			Message: "eval requires an expression, --file <file>, or --stdin",
		})
	}

	source := wrapExprSource(exprSrc)
	if req.pretty {
		source = pretty.Transform(source, pretty.Pretty).Source
	}

	tokens, lexErr := lexer.LexNamed(source, evalName)
	if lexErr != nil {
		return reportError(req.jsonOut, structuredError{
			Stage:   "lex",
			Code:    lexErr.Code,
			Message: withFloorHint(lexErr.Message, req.file),
			File:    lexErr.Location.File,
			Line:    lexErr.Location.Line,
			Col:     lexErr.Location.Col,
		})
	}
	exprProgram, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return reportError(req.jsonOut, structuredError{
			Stage:   "parse",
			Code:    parseErr.Code,
			Message: withFloorHint(parseErr.Message, req.file),
			File:    parseErr.Location.File,
			Line:    parseErr.Location.Line,
			Col:     parseErr.Location.Col,
		})
	}

	merged := exprProgram
	if scopeProgram != nil {
		merged = mergeEvalPrograms(scopeProgram, exprProgram)
	}
	desugared := desugarProgram(merged)

	linked := loader.LinkWithPackages(desugared, baseDir, resolvePackageFiles(baseDir))
	if linked.Error != nil {
		return reportError(req.jsonOut, structuredError{
			Stage:   "link",
			Message: withFloorHint(linked.Error.Error(), baseDir),
		})
	}

	if req.typeMode {
		return evalTypeMode(linked.Program, baseDir, req.jsonOut)
	}
	return evalRunMode(linked.Program, baseDir, req.jsonOut, start)
}

// parseEvalScopeFile reads and parses the --file target whose definitions
// are brought into the expression's scope.
func parseEvalScopeFile(file string) (*ast.Program, *structuredError) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, &structuredError{Stage: "io", Message: err.Error()}
	}
	tokens, lexErr := lexer.LexNamed(string(stripBOM(b)), filepath.ToSlash(file))
	if lexErr != nil {
		return nil, &structuredError{
			Stage:   "lex",
			Code:    lexErr.Code,
			Message: withFloorHint(lexErr.Message, file),
			File:    lexErr.Location.File,
			Line:    lexErr.Location.Line,
			Col:     lexErr.Location.Col,
		}
	}
	program, parseErr := parser.Parse(tokens)
	if parseErr != nil {
		return nil, &structuredError{
			Stage:   "parse",
			Code:    parseErr.Code,
			Message: withFloorHint(parseErr.Message, file),
			File:    parseErr.Location.File,
			Line:    parseErr.Location.Line,
			Col:     parseErr.Location.Col,
		}
	}
	return program, nil
}

// mergeEvalPrograms brings the scope file's top-levels into the
// expression program. The scope file's own main is dropped — the
// expression's main is the entry point. Everything else (including
// non-pub definitions) is visible, the same as writing the expression
// inside that file.
func mergeEvalPrograms(scope, expr *ast.Program) *ast.Program {
	out := &ast.Program{}
	for _, tl := range scope.TopLevels {
		if d, ok := tl.(ast.TopDefinition); ok && d.Name == "main" {
			continue
		}
		out.TopLevels = append(out.TopLevels, tl)
	}
	out.TopLevels = append(out.TopLevels, expr.TopLevels...)
	return out
}

// evalTypeMode asks the checker for main's inferred body type and
// performed effects without executing anything.
func evalTypeMode(program *ast.Program, baseDir string, jsonOut bool) int {
	info, typeErrors := checker.InferDefinition(program, nil, makeEffectAliasResolver(baseDir), "main")
	if hard := typeErrorsToStructured(typeErrors, false); len(hard) > 0 {
		if jsonOut {
			emitJSON(typeEnvelope{OK: false, Diagnostics: hard})
		} else {
			for _, se := range hard {
				printStructuredError(se)
			}
		}
		return 1
	}
	if info == nil {
		return reportError(jsonOut, structuredError{
			Stage:   "type",
			Message: "--type: nothing to infer (no expression or main definition)",
		})
	}
	if jsonOut {
		emitJSON(typeEnvelope{
			OK:          true,
			Data:        &typeData{Type: info.Type, Effects: info.Effects},
			Diagnostics: []structuredError{},
		})
		return 0
	}
	if len(info.Effects) > 0 {
		fmt.Printf("<%s> %s\n", strings.Join(info.Effects, ", "), info.Type)
	} else {
		fmt.Println(info.Type)
	}
	return 0
}

// evalRunMode compiles and runs the linked program. Text mode prints the
// program's output then the result value, exactly as before. JSON mode
// additionally runs the checker (best effort — eval stays permissive:
// hard type errors become diagnostics but the program still runs) and
// wraps everything in one envelope.
func evalRunMode(program *ast.Program, baseDir string, jsonOut bool, start time.Time) int {
	if !jsonOut {
		mod := compiler.CompileProgram(program)
		if mod.EntryWordID == nil {
			fmt.Println("()")
			return 0
		}
		result, stdout, err := vm.Execute(mod)
		for _, line := range stdout {
			fmt.Println(line)
		}
		if err != nil {
			return reportVMError(false, err)
		}
		if result != nil {
			fmt.Println(vm.ValueToString(*result))
		}
		return 0
	}

	info, typeErrors := checker.InferDefinition(program, nil, makeEffectAliasResolver(baseDir), "main")
	diags := typeErrorsToStructured(typeErrors, true)
	if diags == nil {
		diags = []structuredError{}
	}
	hardErrs := false
	for _, d := range diags {
		if !strings.HasPrefix(d.Code, "W") {
			hardErrs = true
			break
		}
	}

	env := evalEnvelope{OK: true, Stdout: []string{}, Diagnostics: diags}

	mod := compiler.CompileProgram(program)
	var result *vm.Value
	var stdout []string
	var err error
	if mod.EntryWordID != nil {
		result, stdout, err = vm.Execute(mod)
	}
	env.Timing = evalTiming{TotalMs: time.Since(start).Milliseconds()}
	if stdout != nil {
		env.Stdout = stdout
	}
	if err != nil {
		se := vmErrorToStructured(err)
		env.OK = false
		env.Error = &se
		emitJSON(env)
		return 1
	}

	data := evalData{Display: "()", Effects: []string{}}
	if info != nil && !hardErrs {
		t := info.Type
		data.Type = &t
		if len(info.Effects) > 0 {
			data.Effects = info.Effects
		}
	}
	if result != nil {
		data.Display = vm.ValueToString(*result)
		if raw, ok := vm.ValueToJSON(*result); ok {
			data.Value = json.RawMessage(raw)
		}
	}
	env.Data = &data
	emitJSON(env)
	return 0
}
