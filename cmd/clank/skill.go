package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dalurness/clank/internal/pkg"
)

// skillMarkdown is the agent skill installed by `clank skill install`.
// It follows the Agent Skills open standard (agentskills.io): a SKILL.md
// with YAML frontmatter, installed under .agents/skills/ — the
// cross-client convention scanned by Claude Code, Codex, Copilot,
// Cursor, Gemini CLI, and other compliant harnesses.
//
// Keep this compact: it's meant to be loaded into a context window at
// the start of a session, not to replace `clank spec` or `clank doc`.
const skillMarkdown = `---
name: clank
description: Write, run, test, and manage dependencies for Clank programs — a language designed for AI agents. Use when working with .clk files or a clank.pkg project.
---

# Clank

Clank is a strongly-typed language optimized for agents: terse syntax,
tracked effects, refinement types, and a spec small enough to read in
one pass.

## Cold start

- ` + "`clank spec`" + ` — the full language spec (~3500 tokens, read it once)
- ` + "`clank doc`" + ` / ` + "`clank doc search <q>`" + ` / ` + "`clank doc show <name>`" + ` — stdlib + project docs
- Every command accepts ` + "`--json`" + ` for structured output and ` + "`--help`" + ` for usage

## Core loop

1. Write ` + "`file.clk`" + `
2. ` + "`clank check file.clk`" + ` — type-check without running (structured errors with --json)
3. ` + "`clank run file.clk [args...]`" + ` — run (calls ` + "`main : () -> <io> ()`" + `);
   args after the file (or after ` + "`--`" + `) reach the program via ` + "`cli.args()`" + `
4. ` + "`clank test`" + ` — type-checks, then runs inline ` + "`test \"name\" = expr`" + ` blocks
5. ` + "`clank fmt file.clk`" + ` — canonical formatting (in place)

Quick sanity checks: ` + "`clank eval '1 + 2'`" + `.

## Syntax essentials

` + "```clank" + `
# comment
factorial : (n: Int) -> <> Int =          # <> = pure, <io> = does I/O
  if n == 0 then 1 else n * factorial(n - 1)

type Shape = Circle(Rat) | Rect(Rat, Rat) # sum type

area : (s: Shape) -> <> Rat =
  match s {
    Circle(r) => r * r * 3.14159
    Rect(w, h) => w * h
  }

main : () -> <io> () =
  let xs = range(1, 10) |> filter(fn(x) => x % 2 == 0) |> map(show)
  print(join(xs, ", "))
` + "```" + `

- Strings concat with ` + "`++`" + `; ` + "`show`" + ` converts values to Str
- Stdlib is import-free and module-qualified: ` + "`fs.read`" + `, ` + "`json.dec`" + `,
  ` + "`http.get`" + `, ` + "`proc.sh`" + `, ` + "`env.get`" + `, ` + "`rx.find`" + `, ` + "`math.abs`" + `, ` + "`str.*`" + `,
  ` + "`col.*`" + `, ` + "`iter.*`" + `
- Effects are part of the type: pure ` + "`<>`" + `, ` + "`<io>`" + `, ` + "`<exn[E]>`" + `, ` + "`<async>`" + `

Gotchas:

- ` + "`len`" + ` is for lists; string length is ` + "`str.len`" + `. ` + "`str.rep`" + ` is replace,
  not repeat (repeat: ` + "`join(col.rep(s, n), \"\")`" + `)
- ` + "`range(a, b)`" + ` and ` + "`iter.range(a, b)`" + ` include ` + "`b`" + `
- No list patterns in ` + "`match`" + ` — destructure with ` + "`head`" + `/` + "`tail`" + `/` + "`col.nth`" + `
- Effect operations take exactly one parameter (use a tuple for more)
- Sort with a comparator: ` + "`col.sortby(xs, fn(a, b) => ...)`" + ` returning Int

## Modules and packages

Local modules (files in the project):

` + "```clank" + `
use utils            # qualified: utils.helper
use utils (helper)   # unqualified
` + "```" + `

External packages use the ` + "`&`" + ` sigil — one flat namespace per package:

` + "```clank" + `
use &hello-clank             # qualified: hello-clank.greet
use &hello-clank as hc       # aliased:   hc.greet
use &hello-clank (greet)     # selective
` + "```" + `

Managing dependencies (go-style, no registry):

` + "```bash" + `
clank pkg init [name]                        # new project manifest
clank pkg add github.com/user/repo           # track default branch
clank pkg add github.com/user/repo@v1.2.0    # pin a tag (re-add to change pin)
clank pkg add ./local/lib                    # local path dep
clank pkg install                            # restore everything after clone
clank pkg update [name]                      # move unpinned deps to latest
clank pkg list                               # what's resolved (add --json)
` + "```" + `

## Debugging

- Errors carry codes (E1xx parse, E2xx type, E5xx pkg; W2xx lint) and
  line/col; ` + "`--json`" + ` gives them as structured diagnostics
- ` + "`clank lint file.clk`" + ` for style/correctness warnings
- ` + "`clank pretty file.clk`" + ` / ` + "`clank terse file.clk`" + ` convert between
  verbose and terse identifier forms
`

// cmdSkill implements:
//
//	clank skill               print the skill markdown to stdout
//	clank skill show          same
//	clank skill install       write .agents/skills/clank/SKILL.md at the project root
//	clank skill install --user  write to ~/.agents/skills/clank/SKILL.md instead
func cmdSkill(args []string, jsonOut bool, rawArgs []string) int {
	sub := "show"
	for _, a := range args {
		if a == "install" || a == "show" {
			sub = a
			break
		}
	}

	if sub == "show" {
		fmt.Print(skillMarkdown)
		return 0
	}

	// install
	var baseDir string
	if hasFlag(rawArgs, "--user") {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot determine home directory: %v\n", err)
			return 1
		}
		baseDir = home
	} else if manifestPath := pkg.FindManifest("."); manifestPath != "" {
		baseDir = filepath.Dir(manifestPath)
	} else {
		baseDir, _ = os.Getwd()
	}

	skillDir := filepath.Join(baseDir, ".agents", "skills", "clank")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error: creating %s: %v\n", skillDir, err)
		return 1
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(skillMarkdown), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error: writing %s: %v\n", skillPath, err)
		return 1
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{"ok": true, "path": skillPath})
		return 0
	}
	fmt.Printf("Installed agent skill: %s\n", skillPath)
	fmt.Println("Any harness supporting the Agent Skills standard (Claude Code, Codex, Copilot, Cursor, ...) will pick it up.")
	return 0
}
