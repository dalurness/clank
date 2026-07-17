package linter

import (
	"fmt"
	"os"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/lexer"
	"github.com/dalurness/clank/internal/parser"
	"github.com/dalurness/clank/internal/token"
)

// LintPackage runs package-level rules across a set of source files that
// all belong to the same package. This is distinct from Lint, which
// operates on a single parsed program — some checks can only be made
// once every file's declarations are visible at the same time.
//
// Currently implements:
//
//	W220 duplicate-pub-name — two files in the same package export
//	                          the same name (function, type, variant,
//	                          effect, effect op, or interface method).
//
// Files that fail to lex or parse are skipped — hard syntax errors are
// the parser's job to report, not the linter's. LintPackage is strictly
// about *multi-file* consistency rules.
func LintPackage(files []string) []LintDiagnostic {
	if len(files) < 2 {
		// Single-file packages can't have cross-file collisions.
		return nil
	}

	type origin struct {
		file string
		loc  token.Loc
	}
	seen := make(map[string]origin)
	var diags []LintDiagnostic

	report := func(name, file string, loc token.Loc) {
		if other, exists := seen[name]; exists {
			diags = append(diags, LintDiagnostic{
				Code: "W220",
				Message: fmt.Sprintf(
					"duplicate pub name '%s' in package\n  first:  %s\n  second: %s",
					name, other.file, file,
				),
				Location: loc,
			})
			return
		}
		seen[name] = origin{file: file, loc: loc}
	}

	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		tokens, lexErr := lexer.Lex(string(source))
		if lexErr != nil {
			continue
		}
		program, parseErr := parser.Parse(tokens)
		if parseErr != nil {
			continue
		}
		for _, tl := range program.TopLevels {
			switch decl := tl.(type) {
			case ast.TopDefinition:
				if decl.Pub {
					report(decl.Name, file, decl.Loc)
				}
			case ast.TopTypeDecl:
				if decl.Pub {
					report(decl.Name, file, decl.Loc)
					for _, v := range decl.Variants {
						report(v.Name, file, decl.Loc)
					}
				}
			case ast.TopEffectDecl:
				if decl.Pub {
					report(decl.Name, file, decl.Loc)
					for _, op := range decl.Ops {
						report(op.Name, file, decl.Loc)
					}
				}
			case ast.TopEffectAlias:
				if decl.Pub {
					report(decl.Name, file, decl.Loc)
				}
			case ast.TopInterfaceDecl:
				if decl.Pub {
					for _, m := range decl.Methods {
						report(m.Name, file, decl.Loc)
					}
				}
			}
		}
	}
	return diags
}
