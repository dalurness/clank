// Package doc implements queryable documentation for builtins and user-defined
// functions. It supports name search, type-directed search (T -> U patterns),
// and detailed show.
package doc

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/checker"
)

// DocEntry is a single searchable documentation entry.
type DocEntry struct {
	Name        string
	Kind        string // "builtin", "function", "type", "effect"
	Signature   string
	Type        checker.Type
	Description string
	Params      []ParamDoc // nil for builtins
	ReturnType  string     // empty for builtins
	Effects     []string   // effect names
	Pub         *bool      // nil for builtins
	File        string     // source file path
}

// ParamDoc is a documented parameter.
type ParamDoc struct {
	Name string
	Type string
}

// ── Show types for display ──

// ShowType converts a checker.Type to a human-readable string.
func ShowType(t checker.Type) string {
	switch t := t.(type) {
	case checker.TPrimitive:
		if t.Name == "unit" {
			return "()"
		}
		return strings.ToUpper(t.Name[:1]) + t.Name[1:]
	case checker.TFn:
		effs := ""
		if len(t.Effects) > 0 {
			names := make([]string, len(t.Effects))
			for i, e := range t.Effects {
				switch e := e.(type) {
				case checker.ENamed:
					names[i] = e.Name
				case checker.EVar:
					names[i] = fmt.Sprintf("e%d", e.ID)
				}
			}
			effs = " {" + strings.Join(names, ", ") + "}"
		}
		paramStr := ShowType(t.Param)
		if _, ok := t.Param.(checker.TFn); ok {
			paramStr = "(" + paramStr + ")"
		}
		return paramStr + " ->" + effs + " " + ShowType(t.Result)
	case checker.TList:
		return "[" + ShowType(t.Element) + "]"
	case checker.TTuple:
		elems := make([]string, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = ShowType(e)
		}
		return "(" + strings.Join(elems, ", ") + ")"
	case checker.TRecord:
		fields := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = f.Name + ": " + ShowType(f.Type)
		}
		return "{" + strings.Join(fields, ", ") + "}"
	case checker.TVariant:
		names := make([]string, len(t.Variants))
		for i, v := range t.Variants {
			names[i] = v.Name
		}
		return strings.Join(names, " | ")
	case checker.TVar:
		return fmt.Sprintf("t%d", t.ID)
	case checker.TGeneric:
		if t.Name == "?" {
			return "a"
		}
		if len(t.Args) > 0 {
			args := make([]string, len(t.Args))
			for i, a := range t.Args {
				args[i] = ShowType(a)
			}
			return t.Name + "<" + strings.Join(args, ", ") + ">"
		}
		return t.Name
	}
	return "?"
}

// ShowTypeExpr converts an AST type expression to a string.
func ShowTypeExpr(te ast.TypeExpr) string {
	switch te := te.(type) {
	case ast.TypeName:
		return te.Name
	case ast.TypeList:
		return "[" + ShowTypeExpr(te.Element) + "]"
	case ast.TypeTuple:
		if len(te.Elements) == 0 {
			return "()"
		}
		elems := make([]string, len(te.Elements))
		for i, e := range te.Elements {
			elems[i] = ShowTypeExpr(e)
		}
		return "(" + strings.Join(elems, ", ") + ")"
	case ast.TypeFn:
		effs := ""
		if len(te.Effects) > 0 {
			names := make([]string, len(te.Effects))
			for i, e := range te.Effects {
				names[i] = e.Name
			}
			effs = " {" + strings.Join(names, ", ") + "}"
		}
		return ShowTypeExpr(te.Param) + " ->" + effs + " " + ShowTypeExpr(te.Result)
	case ast.TypeGeneric:
		args := make([]string, len(te.Args))
		for i, a := range te.Args {
			args[i] = ShowTypeExpr(a)
		}
		return te.Name + "<" + strings.Join(args, ", ") + ">"
	case ast.TypeRecord:
		fields := make([]string, len(te.Fields))
		for i, f := range te.Fields {
			fields[i] = f.Name + ": " + ShowTypeExpr(f.Type)
		}
		return "{" + strings.Join(fields, ", ") + "}"
	case ast.TypeUnion:
		return ShowTypeExpr(te.Left) + " | " + ShowTypeExpr(te.Right)
	case ast.TypeRefined:
		return ShowTypeExpr(te.Base) + "{" + te.Predicate + "}"
	case ast.TypeBorrow:
		return "&" + ShowTypeExpr(te.Inner)
	}
	return "?"
}

// SigToString converts a TypeSig to a human-readable string.
func SigToString(sig ast.TypeSig) string {
	params := make([]string, len(sig.Params))
	for i, p := range sig.Params {
		params[i] = ShowTypeExpr(p.Type)
	}
	effs := ""
	if len(sig.Effects) > 0 {
		names := make([]string, len(sig.Effects))
		for i, e := range sig.Effects {
			names[i] = e.Name
		}
		effs = " {" + strings.Join(names, ", ") + "}"
	}
	return "(" + strings.Join(params, ", ") + ") ->" + effs + " " + ShowTypeExpr(sig.ReturnType)
}

// ── Builtin registry ──

// BuiltinEntry is a builtin function with its type and description.
type BuiltinEntry struct {
	Name        string
	Type        checker.Type
	Description string
}

// builtinRegistry returns the list of builtin entries (mirrors the TS BUILTIN_REGISTRY).
func builtinRegistry() []BuiltinEntry {
	tInt := checker.TInt
	tBool := checker.TBool
	tStr := checker.TStr
	tUnit := checker.TUnit
	tAny := checker.TAny
	tAnyList := checker.NewTList(tAny)

	fn := checker.NewTFn

	return []BuiltinEntry{
		// Arithmetic
		{"add", fn(tInt, fn(tInt, tInt)), "Add two numbers"},
		{"sub", fn(tInt, fn(tInt, tInt)), "Subtract second from first"},
		{"mul", fn(tInt, fn(tInt, tInt)), "Multiply two numbers"},
		{"div", fn(tInt, fn(tInt, tInt)), "Integer division"},
		{"mod", fn(tInt, fn(tInt, tInt)), "Modulo (remainder)"},

		// Comparison
		{"eq", fn(tAny, fn(tAny, tBool)), "Structural equality"},
		{"neq", fn(tAny, fn(tAny, tBool)), "Structural inequality"},
		{"lt", fn(tInt, fn(tInt, tBool)), "Less than"},
		{"gt", fn(tInt, fn(tInt, tBool)), "Greater than"},
		{"lte", fn(tInt, fn(tInt, tBool)), "Less than or equal"},
		{"gte", fn(tInt, fn(tInt, tBool)), "Greater than or equal"},

		// Logic
		{"not", fn(tBool, tBool), "Boolean negation"},
		{"negate", fn(tInt, tInt), "Numeric negation"},
		{"and", fn(tBool, fn(tBool, tBool)), "Boolean AND"},
		{"or", fn(tBool, fn(tBool, tBool)), "Boolean OR"},

		// Strings
		{"str.cat", fn(tStr, fn(tStr, tStr)), "Concatenate two strings"},
		{"concat", fn(tAny, fn(tAny, tAny)), "Concatenate strings or lists (++ operator)"},
		{"show", fn(tAny, tStr), "Convert any value to its string representation"},
		{"print", fn(tStr, tUnit), "Print a string to stdout"},

		// List operations
		{"len", fn(tAnyList, tInt), "Length of a list"},
		{"head", fn(tAnyList, tAny), "First element of a list (errors on empty)"},
		{"tail", fn(tAnyList, tAnyList), "All elements except the first (errors on empty)"},
		{"cons", fn(tAny, fn(tAnyList, tAnyList)), "Prepend an element to a list"},
		{"cat", fn(tAnyList, fn(tAnyList, tAnyList)), "Concatenate two lists"},
		{"rev", fn(tAnyList, tAnyList), "Reverse a list"},
		{"get", fn(tAnyList, fn(tInt, tAny)), "Get element at index (errors on out of bounds)"},
		{"map", fn(tAnyList, fn(fn(tAny, tAny), tAnyList)), "Apply a function to each element"},
		{"filter", fn(tAnyList, fn(fn(tAny, tBool), tAnyList)), "Keep elements where predicate returns true"},
		{"fold", fn(tAnyList, fn(tAny, fn(fn(tAny, fn(tAny, tAny)), tAny))), "Left fold with accumulator"},
		{"flat-map", fn(tAnyList, fn(fn(tAny, tAnyList), tAnyList)), "Map each element to a list, then flatten"},
		{"range", fn(tInt, fn(tInt, tAnyList)), "Generate list of integers from start to end (inclusive)"},
		{"zip", fn(tAnyList, fn(tAnyList, tAnyList)), "Zip two lists into list of tuples"},
		{"fst", fn(tAny, tAny), "First element of a tuple"},
		{"snd", fn(tAny, tAny), "Second element of a tuple"},
		{"tuple.get", fn(tAny, fn(tInt, tAny)), "Get tuple element by index"},
		{"split", fn(tStr, fn(tStr, tAnyList)), "Split string by separator"},
		{"join", fn(tAnyList, fn(tStr, tStr)), "Join list of strings with separator"},
		{"trim", fn(tStr, tStr), "Trim whitespace from both ends of a string"},

		// Filesystem
		{"fs.read", fn(tStr, tStr), "Read file contents as string"},
		{"fs.write", fn(tStr, fn(tStr, tUnit)), "Write string to file (path, content)"},
		{"fs.exists", fn(tStr, tBool), "Check if a file or directory exists"},
		{"fs.ls", fn(tStr, checker.NewTList(tStr)), "List directory entries"},
		{"fs.mkdir", fn(tStr, tUnit), "Create directory (recursive)"},
		{"fs.rm", fn(tStr, tUnit), "Remove file or directory"},

		// JSON
		{"json.enc", fn(tAny, tStr), "Encode a value to JSON string"},
		{"json.dec", fn(tStr, tAny), "Decode JSON string to a value"},
		{"json.get", fn(tAny, fn(tStr, tAny)), "Get field from record by key, returns Option"},
		{"json.set", fn(tAny, fn(tStr, fn(tAny, tAny))), "Set field on record (record, key, value)"},
		{"json.keys", fn(tAny, checker.NewTList(tStr)), "Get list of keys from a record"},
		{"json.merge", fn(tAny, fn(tAny, tAny)), "Merge two records (right wins on conflict)"},

		// Environment
		{"env.get", fn(tStr, tAny), "Get environment variable, returns Option[Str]"},
		{"env.set", fn(tStr, fn(tStr, tUnit)), "Set environment variable (key, value)"},
		{"env.has", fn(tStr, tBool), "Check if environment variable exists"},
		{"env.all", fn(tUnit, tAny), "Get all environment variables as List[(Str, Str)]"},

		// Process execution
		{"proc.run", fn(tStr, fn(checker.NewTList(tStr), tAny)), "Run command with args, returns {stdout, stderr, code}"},
		{"proc.sh", fn(tStr, tStr), "Run shell command, returns stdout"},
		{"proc.exit", fn(tInt, tUnit), "Exit process with code"},

		// HTTP
		{"http.get", fn(tStr, tAny), "HTTP GET request, returns {status, body, headers}"},
		{"http.post", fn(tStr, fn(tStr, tAny)), "HTTP POST request (url, body), returns {status, body, headers}"},
		{"http.put", fn(tStr, fn(tStr, tAny)), "HTTP PUT request (url, body), returns {status, body, headers}"},
		{"http.del", fn(tStr, tAny), "HTTP DELETE request, returns {status, body, headers}"},
		{"http.set-timeout", fn(tInt, tUnit), "Set HTTP request timeout in milliseconds (default: 30000)"},

		// Regex
		{"rx.ok", fn(tStr, fn(tStr, tBool)), "Test if string matches regex pattern"},
		{"rx.find", fn(tStr, fn(tStr, checker.NewTList(tStr))), "Find all matches of regex pattern in string"},
		{"rx.replace", fn(tStr, fn(tStr, fn(tStr, tStr))), "Replace all regex matches (string, pattern, replacement)"},
		{"rx.split", fn(tStr, fn(tStr, checker.NewTList(tStr))), "Split string by regex pattern"},

		// Math
		{"math.abs", fn(tInt, tInt), "Absolute value"},
		{"math.min", fn(tInt, fn(tInt, tInt)), "Minimum of two numbers"},
		{"math.max", fn(tInt, fn(tInt, tInt)), "Maximum of two numbers"},
		{"math.floor", fn(tAny, tInt), "Floor (round down to integer)"},
		{"math.ceil", fn(tAny, tInt), "Ceiling (round up to integer)"},
		{"math.sqrt", fn(tAny, tAny), "Square root (returns Rat)"},

		// HTTP Server
		{"srv.new", fn(tUnit, tAny), "Create empty route table"},
		{"srv.get", fn(tAny, fn(tStr, fn(tAny, tAny))), "Add GET route handler"},
		{"srv.post", fn(tAny, fn(tStr, fn(tAny, tAny))), "Add POST route handler"},
		{"srv.put", fn(tAny, fn(tStr, fn(tAny, tAny))), "Add PUT route handler"},
		{"srv.del", fn(tAny, fn(tStr, fn(tAny, tAny))), "Add DELETE route handler"},
		{"srv.start", fn(tAny, fn(tInt, tAny)), "Start HTTP server on port"},
		{"srv.stop", fn(tAny, tUnit), "Stop HTTP server"},
		{"srv.res", fn(tInt, fn(tStr, tAny)), "Create response: status body"},
		{"srv.json", fn(tInt, fn(tAny, tAny)), "Create JSON response: status json"},
		{"srv.hdr", fn(tAny, fn(tStr, fn(tStr, tAny))), "Add header to response"},
		{"srv.mw", fn(tAny, fn(tAny, tAny)), "Add middleware to route table"},

		// DateTime
		{"dt.now", fn(tUnit, tAny), "Current datetime UTC as record"},
		{"dt.unix", fn(tUnit, tInt), "Current unix timestamp (seconds)"},
		{"dt.from", fn(tInt, tAny), "Datetime record from unix timestamp"},
		{"dt.to", fn(tAny, tInt), "Datetime record to unix timestamp"},
		{"dt.parse", fn(tStr, fn(tStr, tAny)), "Parse datetime string with format"},
		{"dt.fmt", fn(tAny, fn(tStr, tStr)), "Format datetime with format string"},
		{"dt.add", fn(tAny, fn(tInt, tAny)), "Add milliseconds to datetime"},
		{"dt.sub", fn(tAny, fn(tAny, tInt)), "Difference between datetimes in ms"},
		{"dt.tz", fn(tAny, fn(tStr, tAny)), "Convert datetime to timezone"},
		{"dt.iso", fn(tAny, tStr), "Format as ISO 8601"},
		{"dt.ms", fn(tInt, tInt), "Milliseconds to duration"},
		{"dt.sec", fn(tInt, tInt), "Seconds to duration (ms)"},
		{"dt.min", fn(tInt, tInt), "Minutes to duration (ms)"},
		{"dt.hr", fn(tInt, tInt), "Hours to duration (ms)"},
		{"dt.day", fn(tInt, tInt), "Days to duration (ms)"},

		// CSV
		{"csv.dec", fn(tStr, tAny), "Parse CSV string to [[Str]]"},
		{"csv.enc", fn(tAny, tStr), "Encode [[Str]] to CSV string"},
		{"csv.decf", fn(tStr, tAny), "Parse CSV file to [[Str]]"},
		{"csv.encf", fn(tStr, fn(tAny, tUnit)), "Write [[Str]] to CSV file"},
		{"csv.hdr", fn(tAny, tAny), "Extract header row"},
		{"csv.rows", fn(tAny, tAny), "Extract data rows (skip header)"},
		{"csv.maps", fn(tAny, tAny), "Rows as records keyed by header"},
		{"csv.opts", fn(tAny, fn(tStr, tAny)), "Parse with custom options"},

		// Logging
		{"log.trace", fn(tStr, tUnit), "Log at trace level"},
		{"log.debug", fn(tStr, tUnit), "Log at debug level"},
		{"log.info", fn(tStr, tUnit), "Log at info level"},
		{"log.warn", fn(tStr, tUnit), "Log at warn level"},
		{"log.error", fn(tStr, tUnit), "Log at error level"},
		{"log.level", fn(tStr, tUnit), "Set minimum log level (trace/debug/info/warn/error)"},
		{"log.ctx", fn(tStr, fn(tStr, tUnit)), "Add persistent context key-value"},
		{"log.json", fn(tBool, tUnit), "Enable/disable JSON output format"},

		// CLI
		{"cli.args", fn(tUnit, tAny), "Raw argument list"},
		{"cli.parse", fn(tAny, tAny), "Parse args with option schema"},
		{"cli.opt", fn(tStr, fn(tStr, fn(tStr, tAny))), "Create option: name short desc"},
		{"cli.req", fn(tAny, tAny), "Mark option as required"},
		{"cli.def", fn(tAny, fn(tStr, tAny)), "Set default value for option"},
		{"cli.get", fn(tAny, fn(tStr, tAny)), "Get option value as Option[Str]"},
		{"cli.flag", fn(tAny, fn(tStr, tBool)), "Check if flag is set"},
		{"cli.pos", fn(tAny, fn(tInt, tAny)), "Get positional arg by index"},

		// Iterator combinators
		{"iter.of", fn(tAnyList, tAny), "Create iterator from list"},
		{"iter.range", fn(tInt, fn(tInt, tAny)), "Create iterator over integer range"},
		{"iter.collect", fn(tAny, tAnyList), "Collect iterator into list"},
		{"iter.map", fn(tAny, fn(fn(tAny, tAny), tAny)), "Lazily map function over iterator"},
		{"iter.filter", fn(tAny, fn(fn(tAny, tBool), tAny)), "Lazily filter iterator by predicate"},
		{"iter.take", fn(tAny, fn(tInt, tAny)), "Take first n elements"},
		{"iter.drop", fn(tAny, fn(tInt, tAny)), "Skip first n elements"},
		{"iter.fold", fn(tAny, fn(tAny, fn(fn(tAny, fn(tAny, tAny)), tAny))), "Fold iterator with accumulator"},
		{"iter.count", fn(tAny, tInt), "Count elements in iterator"},
		{"iter.sum", fn(tAny, tInt), "Sum integer iterator"},
		{"iter.any", fn(tAny, fn(fn(tAny, tBool), tBool)), "Any element matches predicate"},
		{"iter.all", fn(tAny, fn(fn(tAny, tBool), tBool)), "All elements match predicate"},
		{"iter.find", fn(tAny, fn(fn(tAny, tBool), tAny)), "First match, returns Option"},
		{"iter.each", fn(tAny, fn(fn(tAny, tUnit), tUnit)), "Execute side-effect per element"},
		{"iter.drain", fn(tAny, tUnit), "Consume iterator, discard values"},
		{"iter.enumerate", fn(tAny, tAny), "Pair each element with its index"},
		{"iter.chain", fn(tAny, fn(tAny, tAny)), "Chain two iterators sequentially"},
		{"iter.zip", fn(tAny, fn(tAny, tAny)), "Zip two iterators into tuple iterator"},
		{"iter.take-while", fn(tAny, fn(fn(tAny, tBool), tAny)), "Take while predicate holds"},
		{"iter.drop-while", fn(tAny, fn(fn(tAny, tBool), tAny)), "Drop while predicate holds"},
		{"iter.flatmap", fn(tAny, fn(fn(tAny, tAnyList), tAny)), "Map then flatten"},
		{"iter.first", fn(tAny, tAny), "First element as Option"},
		{"iter.last", fn(tAny, tAny), "Last element as Option"},
		{"iter.join", fn(tAny, fn(tStr, tStr)), "Join iterator elements as string"},
		{"iter.repeat", fn(tAny, tAny), "Infinite iterator of same value"},
		{"iter.once", fn(tAny, tAny), "Iterator yielding one value"},
		{"iter.empty", fn(tUnit, tAny), "Empty iterator"},
		{"iter.unfold", fn(tAny, fn(fn(tAny, tAny), tAny)), "Build iterator from state function"},
		{"iter.scan", fn(tAny, fn(tAny, fn(fn(tAny, fn(tAny, tAny)), tAny))), "Running fold as iterator"},
		{"iter.dedup", fn(tAny, tAny), "Remove consecutive duplicates"},
		{"iter.chunk", fn(tAny, fn(tInt, tAny)), "Group into chunks of size n"},
		{"iter.window", fn(tAny, fn(tInt, tAny)), "Sliding window of size n"},
		{"iter.intersperse", fn(tAny, fn(tAny, tAny)), "Insert separator between elements"},
		{"iter.cycle", fn(tAny, tAny), "Infinitely repeat iterator"},
		{"iter.nth", fn(tAny, fn(tInt, tAny)), "Nth element as Option"},
		{"iter.min", fn(tAny, tAny), "Minimum element as Option"},
		{"iter.max", fn(tAny, tAny), "Maximum element as Option"},
		{"iter.generate", fn(fn(tUnit, tAny), tAny), "Generate from () -> Option[a] function"},

		// Collections
		{"col.rev", fn(tAnyList, tAnyList), "Reverse a list"},
		{"col.sort", fn(tAnyList, tAnyList), "Sort list (numbers or strings)"},
		{"col.sortby", fn(tAnyList, fn(fn(tAny, fn(tAny, tInt)), tAnyList)), "Sort with comparator (a -> a -> Int)"},
		{"col.uniq", fn(tAnyList, tAnyList), "Remove adjacent duplicates"},
		{"col.zip", fn(tAnyList, fn(tAnyList, tAnyList)), "Zip two lists into list of tuples"},
		{"col.unzip", fn(tAnyList, tAny), "Unzip list of tuples into tuple of lists"},
		{"col.flat", fn(tAnyList, tAnyList), "Flatten one level of nesting"},
		{"col.flatmap", fn(tAnyList, fn(fn(tAny, tAnyList), tAnyList)), "Map then flatten"},
		{"col.take", fn(tAnyList, fn(tInt, tAnyList)), "First n elements"},
		{"col.drop", fn(tAnyList, fn(tInt, tAnyList)), "Drop first n elements"},
		{"col.nth", fn(tAnyList, fn(tInt, tAny)), "Element at index, returns Option"},
		{"col.find", fn(tAnyList, fn(fn(tAny, tBool), tAny)), "First match, returns Option"},
		{"col.any", fn(tAnyList, fn(fn(tAny, tBool), tBool)), "Any element matches predicate"},
		{"col.all", fn(tAnyList, fn(fn(tAny, tBool), tBool)), "All elements match predicate"},
		{"col.count", fn(tAnyList, fn(fn(tAny, tBool), tInt)), "Count elements matching predicate"},
		{"col.enum", fn(tAnyList, tAnyList), "Pairs with indices: [(0, a), (1, b), ...]"},
		{"col.chunk", fn(tAnyList, fn(tInt, tAnyList)), "Split into chunks of size n"},
		{"col.win", fn(tAnyList, fn(tInt, tAnyList)), "Sliding window of size n"},
		{"col.intersperse", fn(tAnyList, fn(tAny, tAnyList)), "Insert separator between elements"},
		{"col.rep", fn(tAny, fn(tInt, tAnyList)), "Repeat value n times"},
		{"col.sum", fn(tAnyList, tInt), "Sum of integer list"},
		{"col.prod", fn(tAnyList, tInt), "Product of integer list"},
		{"col.min", fn(tAnyList, tAny), "Minimum element"},
		{"col.max", fn(tAnyList, tAny), "Maximum element"},
		{"col.group", fn(tAnyList, fn(fn(tAny, tAny), tAnyList)), "Group by key function"},
		{"col.scan", fn(tAnyList, fn(tAny, fn(fn(tAny, fn(tAny, tAny)), tAnyList))), "Running fold"},
	}
}

// GetBuiltinEntries returns doc entries for all builtins (excluding raise, exn, io).
func GetBuiltinEntries() []DocEntry {
	var entries []DocEntry
	for _, b := range builtinRegistry() {
		entries = append(entries, DocEntry{
			Name:        b.Name,
			Kind:        "builtin",
			Signature:   ShowType(b.Type),
			Type:        b.Type,
			Description: b.Description,
		})
	}
	return entries
}

// ── Extract entries from a parsed program ──

func resolveTypeExprToType(te ast.TypeExpr) checker.Type {
	switch te := te.(type) {
	case ast.TypeName:
		switch te.Name {
		case "Int":
			return checker.TInt
		case "Rat":
			return checker.TRat
		case "Bool":
			return checker.TBool
		case "Str":
			return checker.TStr
		case "Unit":
			return checker.TUnit
		default:
			return checker.TGeneric{Name: te.Name}
		}
	case ast.TypeList:
		return checker.NewTList(resolveTypeExprToType(te.Element))
	case ast.TypeTuple:
		if len(te.Elements) == 0 {
			return checker.TUnit
		}
		elems := make([]checker.Type, len(te.Elements))
		for i, e := range te.Elements {
			elems[i] = resolveTypeExprToType(e)
		}
		return checker.TTuple{Elements: elems}
	case ast.TypeFn:
		effects := make([]checker.Effect, len(te.Effects))
		for i, e := range te.Effects {
			effects[i] = checker.ENamed{Name: e.Name}
		}
		return checker.NewTFn(
			resolveTypeExprToType(te.Param),
			resolveTypeExprToType(te.Result),
			effects...,
		)
	case ast.TypeGeneric:
		args := make([]checker.Type, len(te.Args))
		for i, a := range te.Args {
			args[i] = resolveTypeExprToType(a)
		}
		return checker.TGeneric{Name: te.Name, Args: args}
	case ast.TypeRecord:
		fields := make([]checker.RecordField, len(te.Fields))
		for i, f := range te.Fields {
			fields[i] = checker.RecordField{
				Name: f.Name,
				Tags: f.Tags,
				Type: resolveTypeExprToType(f.Type),
			}
		}
		return checker.TRecord{Fields: fields, RowVar: -1}
	default:
		return checker.TAny
	}
}

// ExtractProgramEntries extracts doc entries from a parsed AST program.
func ExtractProgramEntries(program ast.Program, file string) []DocEntry {
	var entries []DocEntry
	for _, tl := range program.TopLevels {
		switch tl := tl.(type) {
		case ast.TopDefinition:
			retType := resolveTypeExprToType(tl.Sig.ReturnType)
			var fnType checker.Type
			if len(tl.Sig.Params) == 0 {
				fnType = checker.NewTFn(checker.TUnit, retType)
			} else {
				fnType = retType
				for i := len(tl.Sig.Params) - 1; i >= 0; i-- {
					fnType = checker.NewTFn(resolveTypeExprToType(tl.Sig.Params[i].Type), fnType)
				}
			}
			params := make([]ParamDoc, len(tl.Sig.Params))
			for i, p := range tl.Sig.Params {
				params[i] = ParamDoc{Name: p.Name, Type: ShowTypeExpr(p.Type)}
			}
			effects := make([]string, len(tl.Sig.Effects))
			for i, e := range tl.Sig.Effects {
				effects[i] = e.Name
			}
			pub := tl.Pub
			entries = append(entries, DocEntry{
				Name:        tl.Name,
				Kind:        "function",
				Signature:   SigToString(tl.Sig),
				Type:        fnType,
				Description: "User-defined function",
				Params:      params,
				ReturnType:  ShowTypeExpr(tl.Sig.ReturnType),
				Effects:     effects,
				Pub:         &pub,
				File:        file,
			})
		case ast.TopTypeDecl:
			sig := "type " + tl.Name
			if len(tl.TypeParams) > 0 {
				sig += "<" + strings.Join(tl.TypeParams, ", ") + ">"
			}
			variants := make([]string, len(tl.Variants))
			for i, v := range tl.Variants {
				if len(v.Fields) > 0 {
					fields := make([]string, len(v.Fields))
					for j, f := range v.Fields {
						fields[j] = ShowTypeExpr(f)
					}
					variants[i] = v.Name + "(" + strings.Join(fields, ", ") + ")"
				} else {
					variants[i] = v.Name
				}
			}
			sig += " = " + strings.Join(variants, " | ")
			pub := tl.Pub
			entries = append(entries, DocEntry{
				Name:        tl.Name,
				Kind:        "type",
				Signature:   sig,
				Type:        checker.TGeneric{Name: tl.Name},
				Description: "User-defined type",
				Pub:         &pub,
				File:        file,
			})
		case ast.TopEffectDecl:
			ops := make([]string, len(tl.Ops))
			for i, op := range tl.Ops {
				ops[i] = op.Name + ": " + SigToString(op.Sig)
			}
			sig := "effect " + tl.Name + " { " + strings.Join(ops, "; ") + " }"
			pub := tl.Pub
			entries = append(entries, DocEntry{
				Name:        tl.Name,
				Kind:        "effect",
				Signature:   sig,
				Type:        checker.TGeneric{Name: "effect"},
				Description: "User-defined effect",
				Pub:         &pub,
				File:        file,
			})
		}
	}
	return entries
}

// ── Type pattern matching ──

var singleLowerLetter = regexp.MustCompile(`^[a-z]$`)
var namedTypeRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// ParseTypePattern parses a simple type pattern like "Int -> Bool".
// Lowercase single letters are treated as type variables (match anything).
func ParseTypePattern(pattern string) checker.Type {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return nil
	}

	// Arrow type: split on ->
	if idx := findTopLevelArrow(trimmed); idx != -1 {
		left := ParseTypePattern(trimmed[:idx])
		right := ParseTypePattern(trimmed[idx+2:])
		if left == nil || right == nil {
			return nil
		}
		return checker.NewTFn(left, right)
	}

	// List type: [T]
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		inner := ParseTypePattern(trimmed[1 : len(trimmed)-1])
		if inner == nil {
			return nil
		}
		return checker.NewTList(inner)
	}

	// Parenthesized type
	if strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")") {
		return ParseTypePattern(trimmed[1 : len(trimmed)-1])
	}

	// Primitive names
	switch trimmed {
	case "Int":
		return checker.TInt
	case "Rat":
		return checker.TRat
	case "Bool":
		return checker.TBool
	case "Str":
		return checker.TStr
	case "Unit", "()":
		return checker.TUnit
	}

	// Single lowercase letter = type variable (wildcard)
	if singleLowerLetter.MatchString(trimmed) {
		return checker.TAny
	}

	// Named type
	if namedTypeRe.MatchString(trimmed) {
		return checker.TGeneric{Name: trimmed}
	}

	return nil
}

func findTopLevelArrow(s string) int {
	depth := 0
	for i := 0; i < len(s)-1; i++ {
		ch := s[i]
		if ch == '(' || ch == '[' {
			depth++
		} else if ch == ')' || ch == ']' {
			depth--
		} else if depth == 0 && ch == '-' && s[i+1] == '>' {
			return i
		}
	}
	return -1
}

// typeMatchesPattern checks if a type matches a pattern.
func typeMatchesPattern(ty, pattern checker.Type) bool {
	// Pattern is a wildcard
	if g, ok := pattern.(checker.TGeneric); ok && g.Name == "?" {
		return true
	}

	switch p := pattern.(type) {
	case checker.TFn:
		if t, ok := ty.(checker.TFn); ok {
			if typeMatchesPattern(t.Param, p.Param) && typeMatchesPattern(t.Result, p.Result) {
				return true
			}
			// Also try matching the result part for curried functions
			return typeMatchesPattern(t.Result, pattern)
		}
	case checker.TList:
		if t, ok := ty.(checker.TList); ok {
			return typeMatchesPattern(t.Element, p.Element)
		}
	case checker.TPrimitive:
		if t, ok := ty.(checker.TPrimitive); ok {
			return p.Name == t.Name
		}
	case checker.TGeneric:
		if t, ok := ty.(checker.TGeneric); ok {
			return p.Name == t.Name
		}
	}

	return false
}

// typeMatchesPatternDeep also tries matching against sub-arrows of curried functions.
func typeMatchesPatternDeep(ty, pattern checker.Type) bool {
	if typeMatchesPattern(ty, pattern) {
		return true
	}
	if t, ok := ty.(checker.TFn); ok {
		return typeMatchesPatternDeep(t.Result, pattern)
	}
	return false
}

// ── Search ──

// SearchEntries filters entries by query. If query contains "->", does
// type-directed search; otherwise does case-insensitive name search.
func SearchEntries(entries []DocEntry, query string) []DocEntry {
	if strings.Contains(query, "->") {
		pattern := ParseTypePattern(query)
		if pattern != nil {
			var results []DocEntry
			for _, e := range entries {
				if typeMatchesPatternDeep(e.Type, pattern) {
					results = append(results, e)
				}
			}
			return results
		}
	}

	lower := strings.ToLower(query)
	var results []DocEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), lower) {
			results = append(results, e)
		}
	}
	return results
}

// ── Show ──

// FindEntry finds an entry by exact name.
func FindEntry(entries []DocEntry, name string) *DocEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

// ── Format for display ──

// FormatEntryShort returns a one-line summary.
func FormatEntryShort(entry DocEntry) string {
	return fmt.Sprintf("%s: %s  [%s]", entry.Name, entry.Signature, entry.Kind)
}

// FormatEntryDetailed returns a multi-line detail view.
func FormatEntryDetailed(entry DocEntry) string {
	var b strings.Builder
	b.WriteString(entry.Name)
	b.WriteString("\n  Kind: " + entry.Kind)
	b.WriteString("\n  Signature: " + entry.Signature)
	b.WriteString("\n  Description: " + entry.Description)
	if len(entry.Params) > 0 {
		b.WriteString("\n  Parameters:")
		for _, p := range entry.Params {
			b.WriteString("\n    " + p.Name + ": " + p.Type)
		}
	}
	if entry.ReturnType != "" {
		b.WriteString("\n  Returns: " + entry.ReturnType)
	}
	if len(entry.Effects) > 0 {
		b.WriteString("\n  Effects: " + strings.Join(entry.Effects, ", "))
	}
	if entry.File != "" {
		b.WriteString("\n  File: " + entry.File)
	}
	if entry.Pub != nil {
		b.WriteString(fmt.Sprintf("\n  Public: %v", *entry.Pub))
	}
	return b.String()
}

// EntryToMap converts a DocEntry to a map for JSON serialization.
func EntryToMap(entry DocEntry) map[string]interface{} {
	m := map[string]interface{}{
		"name":        entry.Name,
		"kind":        entry.Kind,
		"signature":   entry.Signature,
		"description": entry.Description,
	}
	if len(entry.Params) > 0 {
		params := make([]map[string]string, len(entry.Params))
		for i, p := range entry.Params {
			params[i] = map[string]string{"name": p.Name, "type": p.Type}
		}
		m["params"] = params
	}
	if entry.ReturnType != "" {
		m["returnType"] = entry.ReturnType
	}
	if len(entry.Effects) > 0 {
		m["effects"] = entry.Effects
	}
	if entry.File != "" {
		m["file"] = entry.File
	}
	if entry.Pub != nil {
		m["pub"] = *entry.Pub
	}
	return m
}

// isLower checks if a rune is lowercase.
func isLower(r rune) bool {
	return unicode.IsLower(r)
}
