package doc

import (
	"testing"

	"github.com/dalurness/clank/internal/ast"
	"github.com/dalurness/clank/internal/checker"
	"github.com/dalurness/clank/internal/token"
)

func TestGetBuiltinEntries(t *testing.T) {
	entries := GetBuiltinEntries()
	if len(entries) == 0 {
		t.Fatal("expected non-empty builtin entries")
	}
	// All should be kind "builtin"
	for _, e := range entries {
		if e.Kind != "builtin" {
			t.Errorf("entry %q has kind %q, want 'builtin'", e.Name, e.Kind)
		}
	}
}

func TestSearchByName(t *testing.T) {
	entries := GetBuiltinEntries()

	results := SearchEntries(entries, "map")
	found := false
	for _, r := range results {
		if r.Name == "map" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'map' in search results")
	}
}

func TestSearchByNameAdd(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "add")
	found := false
	for _, r := range results {
		if r.Name == "add" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'add' in search results")
	}
}

func TestSearchNoResults(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "zzzznothing")
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "MAP")
	found := false
	for _, r := range results {
		if r.Name == "map" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected case-insensitive match for 'map'")
	}
}

func TestFindEntry(t *testing.T) {
	entries := GetBuiltinEntries()
	e := FindEntry(entries, "fold")
	if e == nil {
		t.Fatal("expected to find 'fold'")
	}
	if e.Kind != "builtin" {
		t.Errorf("expected kind 'builtin', got %q", e.Kind)
	}
	if e.Signature == "" {
		t.Error("expected non-empty signature")
	}
}

func TestFindEntryNotFound(t *testing.T) {
	entries := GetBuiltinEntries()
	e := FindEntry(entries, "nonexistent")
	if e != nil {
		t.Error("expected nil for non-existent entry")
	}
}

func TestTypeSearchIntToInt(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "Int -> Int")
	found := false
	for _, r := range results {
		if r.Name == "negate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'negate' in Int -> Int results")
	}
}

func TestTypeSearchIntToIntToInt(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "Int -> Int -> Int")
	names := make(map[string]bool)
	for _, r := range results {
		names[r.Name] = true
	}
	if !names["add"] {
		t.Error("expected 'add' in results")
	}
	if !names["sub"] {
		t.Error("expected 'sub' in results")
	}
}

func TestTypeSearchStrToStr(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "Str -> Str")
	found := false
	for _, r := range results {
		if r.Name == "trim" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'trim' in Str -> Str results")
	}
}

func TestTypeSearchWildcard(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "a -> Str")
	names := make(map[string]bool)
	for _, r := range results {
		names[r.Name] = true
	}
	if !names["show"] {
		t.Error("expected 'show' in a -> Str results")
	}
	if !names["trim"] {
		t.Error("expected 'trim' in a -> Str results")
	}
}

func TestTypeSearchBoolToBool(t *testing.T) {
	entries := GetBuiltinEntries()
	results := SearchEntries(entries, "Bool -> Bool")
	found := false
	for _, r := range results {
		if r.Name == "not" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'not' in Bool -> Bool results")
	}
}

func TestExtractProgramEntries(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	program := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{
				Name: "double",
				Sig: ast.TypeSig{
					Params: []ast.TypeSigParam{
						{Name: "x", Type: ast.TypeName{Name: "Int", Loc: loc}},
					},
					ReturnType: ast.TypeName{Name: "Int", Loc: loc},
				},
				Body: ast.ExprLiteral{Value: ast.LitInt{Value: 0}, Loc: loc},
				Pub:  true,
				Loc:  loc,
			},
			ast.TopTypeDecl{
				Name:       "Color",
				TypeParams: nil,
				Variants: []ast.Variant{
					{Name: "Red"},
					{Name: "Green"},
					{Name: "Blue"},
				},
				Pub: true,
				Loc: loc,
			},
		},
	}

	entries := ExtractProgramEntries(program, "test.clk")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Check function entry
	fn := entries[0]
	if fn.Name != "double" {
		t.Errorf("expected name 'double', got %q", fn.Name)
	}
	if fn.Kind != "function" {
		t.Errorf("expected kind 'function', got %q", fn.Kind)
	}
	if len(fn.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(fn.Params))
	}
	if fn.Params[0].Name != "x" {
		t.Errorf("expected param name 'x', got %q", fn.Params[0].Name)
	}
	if fn.ReturnType != "Int" {
		t.Errorf("expected return type 'Int', got %q", fn.ReturnType)
	}
	if fn.Pub == nil || !*fn.Pub {
		t.Error("expected pub to be true")
	}
	if fn.File != "test.clk" {
		t.Errorf("expected file 'test.clk', got %q", fn.File)
	}

	// Check type entry
	ty := entries[1]
	if ty.Name != "Color" {
		t.Errorf("expected name 'Color', got %q", ty.Name)
	}
	if ty.Kind != "type" {
		t.Errorf("expected kind 'type', got %q", ty.Kind)
	}
}

func TestTypeSearchFindsUserDefinedFunctions(t *testing.T) {
	loc := token.Loc{Line: 1, Col: 1}
	program := ast.Program{
		TopLevels: []ast.TopLevel{
			ast.TopDefinition{
				Name: "isEven",
				Sig: ast.TypeSig{
					Params: []ast.TypeSigParam{
						{Name: "n", Type: ast.TypeName{Name: "Int", Loc: loc}},
					},
					ReturnType: ast.TypeName{Name: "Bool", Loc: loc},
				},
				Body: ast.ExprLiteral{Value: ast.LitBool{Value: true}, Loc: loc},
				Loc:  loc,
			},
		},
	}

	entries := GetBuiltinEntries()
	entries = append(entries, ExtractProgramEntries(program, "test.clk")...)

	results := SearchEntries(entries, "Int -> Bool")
	found := false
	for _, r := range results {
		if r.Name == "isEven" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'isEven' in Int -> Bool results")
	}
}

func TestFormatEntryShort(t *testing.T) {
	entry := DocEntry{
		Name:      "trim",
		Kind:      "builtin",
		Signature: "Str -> Str",
	}
	s := FormatEntryShort(entry)
	if s != "trim: Str -> Str  [builtin]" {
		t.Errorf("unexpected short format: %q", s)
	}
}

func TestFormatEntryDetailed(t *testing.T) {
	pub := true
	entry := DocEntry{
		Name:        "double",
		Kind:        "function",
		Signature:   "(Int) -> Int",
		Description: "User-defined function",
		Params:      []ParamDoc{{Name: "x", Type: "Int"}},
		ReturnType:  "Int",
		File:        "test.clk",
		Pub:         &pub,
	}
	s := FormatEntryDetailed(entry)
	if !contains(s, "double") || !contains(s, "Signature") || !contains(s, "Parameters") {
		t.Errorf("detailed format missing expected content: %q", s)
	}
}

func TestEntryToMap(t *testing.T) {
	entry := DocEntry{
		Name:        "add",
		Kind:        "builtin",
		Signature:   "Int -> Int -> Int",
		Description: "Add two numbers",
	}
	m := EntryToMap(entry)
	if m["name"] != "add" {
		t.Errorf("expected name 'add', got %v", m["name"])
	}
	if m["kind"] != "builtin" {
		t.Errorf("expected kind 'builtin', got %v", m["kind"])
	}
}

func TestParseTypePattern(t *testing.T) {
	tests := []struct {
		pattern string
		isNil   bool
	}{
		{"Int -> Bool", false},
		{"[Int] -> Int", false},
		{"a -> Str", false},
		{"(Int) -> Int", false},
		{"Int", false},
		{"", true},
	}
	for _, tt := range tests {
		result := ParseTypePattern(tt.pattern)
		if (result == nil) != tt.isNil {
			t.Errorf("ParseTypePattern(%q): nil=%v, want nil=%v", tt.pattern, result == nil, tt.isNil)
		}
	}
}

func TestShowType(t *testing.T) {
	if s := ShowType(checker.TInt); s != "Int" {
		t.Errorf("expected 'Int', got %q", s)
	}
	if s := ShowType(checker.TUnit); s != "()" {
		t.Errorf("expected '()', got %q", s)
	}
	fn := checker.NewTFn(checker.TInt, checker.TBool)
	if s := ShowType(fn); s != "Int -> Bool" {
		t.Errorf("expected 'Int -> Bool', got %q", s)
	}
	list := checker.NewTList(checker.TStr)
	if s := ShowType(list); s != "[Str]" {
		t.Errorf("expected '[Str]', got %q", s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsStr(s, sub)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
