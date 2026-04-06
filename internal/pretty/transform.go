package pretty

import "strings"

// TransformResult holds the output of a pretty/terse transformation.
type TransformResult struct {
	Source          string
	Transformations int
	Direction       Direction
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentChar(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '-'
}

// scanIdent scans an identifier starting at pos, including trailing ? or !.
func scanIdent(source string, pos int) string {
	end := pos
	for end < len(source) && isIdentChar(source[end]) {
		end++
	}
	if end < len(source) && (source[end] == '?' || source[end] == '!') {
		end++
	}
	return source[pos:end]
}

// collectDefinedNames scans source for user-defined names that should not be
// subject to unqualified expansion. This includes:
//   - let/type/effect/alias bindings (let x = ...)
//   - top-level function definitions (name : type = ...)
//   - match arm bindings (name => ...)
//   - lambda parameters (fn(name, ...) => ...)
func collectDefinedNames(source string) map[string]bool {
	names := make(map[string]bool)
	defKeywords := map[string]bool{"let": true, "type": true, "effect": true, "alias": true}
	pos := 0
	// Track whether we're at the start of a line (for top-level definitions)
	atLineStart := true
	for pos < len(source) {
		ch := source[pos]
		// Skip strings
		if ch == '"' {
			atLineStart = false
			pos++
			for pos < len(source) && source[pos] != '"' {
				if source[pos] == '\\' {
					pos++
				}
				pos++
			}
			if pos < len(source) {
				pos++
			}
			continue
		}
		// Skip comments
		if ch == '#' {
			for pos < len(source) && source[pos] != '\n' {
				pos++
			}
			continue
		}
		if ch == '\n' {
			atLineStart = true
			pos++
			continue
		}
		if ch == ' ' || ch == '\t' {
			pos++
			continue
		}
		if isIdentStart(ch) {
			ident := scanIdent(source, pos)
			afterIdent := pos + len(ident)

			// fn(...) lambda parameters
			if ident == "fn" && afterIdent < len(source) && source[afterIdent] == '(' {
				collectParenNames(source, afterIdent, names)
				pos = afterIdent
				atLineStart = false
				continue
			}

			// Keyword-based definitions: let x, type X, etc.
			if defKeywords[ident] {
				p := afterIdent
				for p < len(source) && (source[p] == ' ' || source[p] == '\t') {
					p++
				}
				if p < len(source) && isIdentStart(source[p]) {
					name := scanIdent(source, p)
					names[name] = true
				}
				pos = afterIdent
				atLineStart = false
				continue
			}

			// Top-level definition: identifier at line start followed by : (with optional whitespace)
			if atLineStart {
				p := afterIdent
				for p < len(source) && (source[p] == ' ' || source[p] == '\t') {
					p++
				}
				if p < len(source) && source[p] == ':' {
					names[ident] = true
				}
			}

			// Match arm binding: identifier followed by => (with optional whitespace)
			{
				p := afterIdent
				for p < len(source) && (source[p] == ' ' || source[p] == '\t') {
					p++
				}
				if p+1 < len(source) && source[p] == '=' && source[p+1] == '>' {
					names[ident] = true
				}
			}

			pos = afterIdent
			atLineStart = false
			continue
		}
		atLineStart = false
		pos++
	}
	return names
}

// collectParenNames extracts identifiers from fn(name, name, ...) parameter lists.
func collectParenNames(source string, openParen int, names map[string]bool) {
	pos := openParen + 1 // skip '('
	for pos < len(source) && source[pos] != ')' {
		if isIdentStart(source[pos]) {
			name := scanIdent(source, pos)
			names[name] = true
			pos += len(name)
		} else {
			pos++
		}
	}
}

// Transform applies bidirectional lexical substitution to source code.
func Transform(source string, direction Direction) TransformResult {
	t := &expansionTable
	var qualifiedMap, moduleMap, unqualMap map[string]string
	if direction == Pretty {
		qualifiedMap = t.qualified.toVerbose
		moduleMap = t.modulePaths.toVerbose
		unqualMap = t.unqualified.toVerbose
	} else {
		qualifiedMap = t.qualified.toTerse
		moduleMap = t.modulePaths.toTerse
		unqualMap = t.unqualified.toTerse
	}
	// Collect user-defined names to avoid expanding them
	userDefined := collectDefinedNames(source)

	var b strings.Builder
	b.Grow(len(source))
	pos := 0
	transformations := 0
	inUseStatement := false
	inImportList := false

	for pos < len(source) {
		ch := source[pos]

		// Skip string literals
		if ch == '"' {
			b.WriteByte(ch)
			pos++
			for pos < len(source) && source[pos] != '"' {
				if source[pos] == '\\' {
					b.WriteByte(source[pos])
					pos++
					if pos < len(source) {
						b.WriteByte(source[pos])
						pos++
					}
				} else {
					b.WriteByte(source[pos])
					pos++
				}
			}
			if pos < len(source) {
				b.WriteByte(source[pos])
				pos++
			}
			continue
		}

		// Skip comments
		if ch == '#' {
			for pos < len(source) && source[pos] != '\n' {
				b.WriteByte(source[pos])
				pos++
			}
			continue
		}

		// Newline resets use-statement context
		if ch == '\n' {
			b.WriteByte(ch)
			pos++
			inUseStatement = false
			inImportList = false
			continue
		}

		// Track import list parentheses
		if inUseStatement && ch == '(' {
			inImportList = true
			b.WriteByte(ch)
			pos++
			continue
		}
		if inImportList && ch == ')' {
			inImportList = false
			inUseStatement = false
			b.WriteByte(ch)
			pos++
			continue
		}

		// Identifier
		if isIdentStart(ch) {
			ident := scanIdent(source, pos)

			// Check if 'use' keyword
			if ident == "use" {
				inUseStatement = true
				inImportList = false
				b.WriteString(ident)
				pos += len(ident)
				continue
			}

			// Look ahead for dot-qualified name
			fullIdent := ident
			lookAhead := pos + len(ident)
			if lookAhead < len(source) && source[lookAhead] == '.' {
				afterDot := lookAhead + 1
				if afterDot < len(source) && isIdentStart(source[afterDot]) {
					secondPart := scanIdent(source, afterDot)
					fullIdent = ident + "." + secondPart
				}
			}

			// In import list: expand bare function names
			if inImportList {
				expanded := expandImportedName(ident, direction)
				if expanded != "" && expanded != ident {
					b.WriteString(expanded)
					transformations++
				} else {
					b.WriteString(ident)
				}
				pos += len(ident)
				continue
			}

			// In use statement: expand module path
			if inUseStatement && !inImportList {
				if len(fullIdent) > len(ident) {
					if moduleExpanded, ok := moduleMap[fullIdent]; ok {
						b.WriteString(moduleExpanded)
						transformations++
						pos += len(fullIdent)
						continue
					}
				}
				b.WriteString(ident)
				pos += len(ident)
				continue
			}

			// Try qualified expansion
			if len(fullIdent) > len(ident) {
				if expanded, ok := qualifiedMap[fullIdent]; ok {
					b.WriteString(expanded)
					transformations++
					pos += len(fullIdent)
					continue
				}
			}

			// Try unqualified expansion (skip user-defined names)
			if !userDefined[ident] {
				if unqualExpanded, ok := unqualMap[ident]; ok {
					b.WriteString(unqualExpanded)
					transformations++
					pos += len(ident)
					continue
				}
			}

			// No expansion
			b.WriteString(ident)
			pos += len(ident)
			continue
		}

		// Any other character
		b.WriteByte(ch)
		pos++
	}

	return TransformResult{
		Source:          b.String(),
		Transformations: transformations,
		Direction:       direction,
	}
}

// expandImportedName expands a bare function name in an import list by
// checking it against all known module prefixes.
func expandImportedName(name string, direction Direction) string {
	t := &expansionTable
	var qualifiedMap map[string]string
	var unqualMap map[string]string
	var prefixes map[string]bool
	if direction == Pretty {
		qualifiedMap = t.qualified.toVerbose
		unqualMap = t.unqualified.toVerbose
		prefixes = t.barePrefixTerse
	} else {
		qualifiedMap = t.qualified.toTerse
		unqualMap = t.unqualified.toTerse
		prefixes = t.barePrefixVerbose
	}

	for prefix := range prefixes {
		qualName := prefix + "." + name
		if expanded, ok := qualifiedMap[qualName]; ok {
			return splitDotAfter(expanded)
		}
	}

	if expanded, ok := unqualMap[name]; ok {
		return expanded
	}
	return ""
}
