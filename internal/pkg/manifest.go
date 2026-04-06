// Package pkg implements the clank.pkg manifest parser, local dependency
// resolution, lockfile management, and version constraint handling.
// Registry protocol (network operations) is out of scope.
package pkg

import (
	"fmt"
	"regexp"
	"strings"
)

// Manifest represents a parsed clank.pkg file.
type Manifest struct {
	Name        string
	Version     string
	Entry       string
	Description string
	License     string
	Repository  string
	Authors     []string
	Clank       string
	Keywords    []string
	Deps        map[string]Dependency // preserves insertion order in Go 1.22+ but we use sorted keys
	DevDeps     map[string]Dependency
	Effects     map[string]bool
	Exports     []string
}

// Dependency is a single dependency declaration.
type Dependency struct {
	Name       string
	Constraint string // version constraint, e.g. "1.2", ">= 1.2.0", "*"
	Path       string // local path dependency (empty if not local)
	GitHub     string // GitHub repo slug (empty if not GitHub dep)
}

// PkgError is a structured error with an error code.
type PkgError struct {
	Code    string
	Message string
}

func (e *PkgError) Error() string {
	return e.Message
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type section int

const (
	sectionRoot section = iota
	sectionDeps
	sectionDevDeps
	sectionEffects
	sectionExports
)

// ParseManifest parses a clank.pkg manifest from source text.
func ParseManifest(source string, filePath string) (*Manifest, error) {
	lines := strings.Split(source, "\n")
	sec := sectionRoot

	m := &Manifest{
		Authors:  []string{},
		Keywords: []string{},
		Deps:     make(map[string]Dependency),
		DevDeps:  make(map[string]Dependency),
		Effects:  make(map[string]bool),
		Exports:  []string{},
	}

	for i, raw := range lines {
		lineNum := i + 1

		// Strip comments
		if idx := strings.Index(raw, "#"); idx >= 0 {
			raw = raw[:idx]
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// Section header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			switch name {
			case "deps":
				sec = sectionDeps
			case "dev-deps":
				sec = sectionDevDeps
			case "effects":
				sec = sectionEffects
			case "exports":
				sec = sectionExports
			default:
				return nil, &PkgError{"E508", fmt.Sprintf("Unknown section [%s] at line %d", name, lineNum)}
			}
			continue
		}

		// Key = value
		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			return nil, &PkgError{"E508", fmt.Sprintf("Expected key = value at line %d: %s", lineNum, line)}
		}

		key := strings.TrimSpace(line[:eqIdx])
		valueRaw := strings.TrimSpace(line[eqIdx+1:])

		switch sec {
		case sectionRoot:
			if err := parseRootField(m, key, valueRaw, lineNum); err != nil {
				return nil, err
			}
		case sectionDeps:
			dep, err := parseDep(key, valueRaw, lineNum)
			if err != nil {
				return nil, err
			}
			m.Deps[key] = dep
		case sectionDevDeps:
			dep, err := parseDep(key, valueRaw, lineNum)
			if err != nil {
				return nil, err
			}
			m.DevDeps[key] = dep
		case sectionEffects:
			val, err := parseBool(valueRaw, key, lineNum)
			if err != nil {
				return nil, err
			}
			m.Effects[key] = val
		case sectionExports:
			if key == "modules" {
				list, err := parseList(valueRaw, lineNum)
				if err != nil {
					return nil, err
				}
				m.Exports = list
			} else {
				return nil, &PkgError{"E508", fmt.Sprintf("Unknown exports field '%s' at line %d", key, lineNum)}
			}
		}
	}

	// Validate required fields
	if filePath == "" {
		filePath = "clank.pkg"
	}
	if m.Name == "" {
		return nil, &PkgError{"E508", fmt.Sprintf("Missing required field 'name' in %s", filePath)}
	}
	if m.Version == "" {
		return nil, &PkgError{"E508", fmt.Sprintf("Missing required field 'version' in %s", filePath)}
	}

	// Validate name format
	if !nameRe.MatchString(m.Name) {
		return nil, &PkgError{"E508", fmt.Sprintf("Invalid package name '%s': must match [a-z][a-z0-9-]* (max 64 chars)", m.Name)}
	}
	if len(m.Name) > 64 {
		return nil, &PkgError{"E508", fmt.Sprintf("Package name '%s' exceeds 64 character limit", m.Name)}
	}

	return m, nil
}

func parseRootField(m *Manifest, key, valueRaw string, lineNum int) error {
	switch key {
	case "name":
		s, err := parseString(valueRaw, key, lineNum)
		if err != nil {
			return err
		}
		m.Name = s
	case "version":
		s, err := parseString(valueRaw, key, lineNum)
		if err != nil {
			return err
		}
		m.Version = s
	case "entry":
		s, err := parseString(valueRaw, key, lineNum)
		if err != nil {
			return err
		}
		m.Entry = s
	case "description":
		s, err := parseString(valueRaw, key, lineNum)
		if err != nil {
			return err
		}
		m.Description = s
	case "license":
		s, err := parseString(valueRaw, key, lineNum)
		if err != nil {
			return err
		}
		m.License = s
	case "repository":
		s, err := parseString(valueRaw, key, lineNum)
		if err != nil {
			return err
		}
		m.Repository = s
	case "authors":
		list, err := parseList(valueRaw, lineNum)
		if err != nil {
			return err
		}
		m.Authors = list
	case "clank":
		s, err := parseString(valueRaw, key, lineNum)
		if err != nil {
			return err
		}
		m.Clank = s
	case "keywords":
		list, err := parseList(valueRaw, lineNum)
		if err != nil {
			return err
		}
		m.Keywords = list
	default:
		return &PkgError{"E508", fmt.Sprintf("Unknown field '%s' at line %d", key, lineNum)}
	}
	return nil
}

func parseDep(name, valueRaw string, lineNum int) (Dependency, error) {
	// Inline table: { path = "..." } or { github = "user/repo", version = "1.2.3" }
	if strings.HasPrefix(valueRaw, "{") {
		lastBrace := strings.LastIndex(valueRaw, "}")
		if lastBrace < 0 {
			return Dependency{}, &PkgError{"E508", fmt.Sprintf("Unclosed inline table for '%s' at line %d", name, lineNum)}
		}
		inner := strings.TrimSpace(valueRaw[1:lastBrace])
		fields, err := parseInlineTable(inner, lineNum)
		if err != nil {
			return Dependency{}, err
		}
		if gh, ok := fields["github"]; ok {
			constraint := "*"
			if v, ok := fields["version"]; ok {
				constraint = v
			}
			return Dependency{Name: name, Constraint: constraint, GitHub: gh}, nil
		}
		if p, ok := fields["path"]; ok {
			constraint := "*"
			if v, ok := fields["version"]; ok {
				constraint = v
			}
			return Dependency{Name: name, Constraint: constraint, Path: p}, nil
		}
		return Dependency{}, &PkgError{"E508", fmt.Sprintf("Dependency '%s' missing 'path' or 'github' field at line %d", name, lineNum)}
	}

	// Regular version constraint
	s, err := parseString(valueRaw, name, lineNum)
	if err != nil {
		return Dependency{}, err
	}
	return Dependency{Name: name, Constraint: s}, nil
}

func parseInlineTable(source string, lineNum int) (map[string]string, error) {
	result := make(map[string]string)
	parts := strings.Split(source, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		eqIdx := strings.Index(trimmed, "=")
		if eqIdx < 0 {
			return nil, &PkgError{"E508", fmt.Sprintf("Expected key = value in inline table at line %d", lineNum)}
		}
		k := strings.TrimSpace(trimmed[:eqIdx])
		v := strings.TrimSpace(trimmed[eqIdx+1:])
		s, err := parseString(v, k, lineNum)
		if err != nil {
			return nil, err
		}
		result[k] = s
	}
	return result, nil
}

func parseString(raw, field string, lineNum int) (string, error) {
	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
		return raw[1 : len(raw)-1], nil
	}
	return "", &PkgError{"E508", fmt.Sprintf("Expected quoted string for '%s' at line %d, got: %s", field, lineNum, raw)}
}

func parseBool(raw, field string, lineNum int) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, &PkgError{"E508", fmt.Sprintf("Expected true/false for '%s' at line %d, got: %s", field, lineNum, raw)}
}

func parseList(raw string, lineNum int) ([]string, error) {
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil, &PkgError{"E508", fmt.Sprintf("Expected list [...] at line %d, got: %s", lineNum, raw)}
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return []string{}, nil
	}
	parts := strings.Split(inner, ",")
	result := make([]string, 0, len(parts))
	for _, item := range parts {
		trimmed := strings.TrimSpace(item)
		if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
			result = append(result, trimmed[1:len(trimmed)-1])
		} else {
			result = append(result, trimmed)
		}
	}
	return result, nil
}

// ── Serialization ──

func serializeDep(dep Dependency) string {
	if dep.GitHub != "" {
		parts := []string{fmt.Sprintf(`github = "%s"`, dep.GitHub)}
		if dep.Constraint != "" && dep.Constraint != "*" {
			parts = append(parts, fmt.Sprintf(`version = "%s"`, dep.Constraint))
		}
		return fmt.Sprintf("%s = { %s }", dep.Name, strings.Join(parts, ", "))
	}
	if dep.Path != "" {
		return fmt.Sprintf(`%s = { path = "%s" }`, dep.Name, dep.Path)
	}
	return fmt.Sprintf(`%s = "%s"`, dep.Name, dep.Constraint)
}

// SerializeManifest converts a Manifest back to clank.pkg format.
func SerializeManifest(m *Manifest) string {
	var lines []string

	lines = append(lines, fmt.Sprintf(`name = "%s"`, m.Name))
	lines = append(lines, fmt.Sprintf(`version = "%s"`, m.Version))
	if m.Entry != "" {
		lines = append(lines, fmt.Sprintf(`entry = "%s"`, m.Entry))
	}
	if m.Description != "" {
		lines = append(lines, fmt.Sprintf(`description = "%s"`, m.Description))
	}
	if m.License != "" {
		lines = append(lines, fmt.Sprintf(`license = "%s"`, m.License))
	}
	if m.Repository != "" {
		lines = append(lines, fmt.Sprintf(`repository = "%s"`, m.Repository))
	}
	if len(m.Authors) > 0 {
		quoted := make([]string, len(m.Authors))
		for i, a := range m.Authors {
			quoted[i] = fmt.Sprintf(`"%s"`, a)
		}
		lines = append(lines, fmt.Sprintf("authors = [%s]", strings.Join(quoted, ", ")))
	}
	if m.Clank != "" {
		lines = append(lines, fmt.Sprintf(`clank = "%s"`, m.Clank))
	}
	if len(m.Keywords) > 0 {
		quoted := make([]string, len(m.Keywords))
		for i, k := range m.Keywords {
			quoted[i] = fmt.Sprintf(`"%s"`, k)
		}
		lines = append(lines, fmt.Sprintf("keywords = [%s]", strings.Join(quoted, ", ")))
	}

	if len(m.Deps) > 0 {
		lines = append(lines, "")
		lines = append(lines, "[deps]")
		for _, dep := range sortedDeps(m.Deps) {
			lines = append(lines, serializeDep(dep))
		}
	}

	if len(m.DevDeps) > 0 {
		lines = append(lines, "")
		lines = append(lines, "[dev-deps]")
		for _, dep := range sortedDeps(m.DevDeps) {
			lines = append(lines, serializeDep(dep))
		}
	}

	if len(m.Effects) > 0 {
		lines = append(lines, "")
		lines = append(lines, "[effects]")
		for _, name := range sortedKeys(m.Effects) {
			lines = append(lines, fmt.Sprintf("%s = %v", name, m.Effects[name]))
		}
	}

	if len(m.Exports) > 0 {
		lines = append(lines, "")
		lines = append(lines, "[exports]")
		quoted := make([]string, len(m.Exports))
		for i, e := range m.Exports {
			quoted[i] = fmt.Sprintf(`"%s"`, e)
		}
		lines = append(lines, fmt.Sprintf("modules = [%s]", strings.Join(quoted, ", ")))
	}

	lines = append(lines, "") // trailing newline
	return strings.Join(lines, "\n")
}
