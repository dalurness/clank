package pretty

import "testing"

func TestTransformPrettyQualified(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"str.len x", "string.length x"},
		{"str.slc x 0 3", "string.slice x 0 3"},
		{"fs.read path", "filesystem.read path"},
		{"col.rev xs", "collection.reverse xs"},
		{"map.del m k", "map.delete m k"},
		{"set.inter a b", "set.intersection a b"},
		{"http.req url", "http.request url"},
		{"err.ctx e msg", "error.context e msg"},
		{"proc.sh cmd", "process.shell cmd"},
		{"env.get key", "environment.get key"},
		{"srv.mw handler", "server.middleware handler"},
		{"dt.fmt t pat", "datetime.format t pat"},
		{"csv.dec data", "csv.decode data"},
		{"rx.match pat s", "regex.match pat s"},
	}
	for _, tt := range tests {
		got := Transform(tt.input, Pretty)
		if got.Source != tt.want {
			t.Errorf("Pretty(%q) = %q, want %q", tt.input, got.Source, tt.want)
		}
		if got.Transformations != 1 {
			t.Errorf("Pretty(%q) transformations = %d, want 1", tt.input, got.Transformations)
		}
	}
}

func TestTransformTerseQualified(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"string.length x", "str.len x"},
		{"filesystem.read path", "fs.read path"},
		{"collection.reverse xs", "col.rev xs"},
		{"datetime.format t pat", "dt.fmt t pat"},
		{"regex.match pat s", "rx.match pat s"},
	}
	for _, tt := range tests {
		got := Transform(tt.input, Terse)
		if got.Source != tt.want {
			t.Errorf("Terse(%q) = %q, want %q", tt.input, got.Source, tt.want)
		}
	}
}

func TestTransformUnqualified(t *testing.T) {
	got := Transform("len xs", Pretty)
	if got.Source != "length xs" {
		t.Errorf("Pretty unqualified: got %q, want %q", got.Source, "length xs")
	}
	got = Transform("length xs", Terse)
	if got.Source != "len xs" {
		t.Errorf("Terse unqualified: got %q, want %q", got.Source, "len xs")
	}
}

func TestTransformModulePath(t *testing.T) {
	got := Transform("use std.str", Pretty)
	if got.Source != "use std.string" {
		t.Errorf("Pretty module path: got %q, want %q", got.Source, "use std.string")
	}
	if got.Transformations != 1 {
		t.Errorf("transformations = %d, want 1", got.Transformations)
	}

	got = Transform("use std.string", Terse)
	if got.Source != "use std.str" {
		t.Errorf("Terse module path: got %q, want %q", got.Source, "use std.str")
	}
}

func TestTransformImportList(t *testing.T) {
	input := "use std.str (slc len)"
	got := Transform(input, Pretty)
	want := "use std.string (slice length)"
	if got.Source != want {
		t.Errorf("Pretty import list: got %q, want %q", got.Source, want)
	}
	if got.Transformations != 3 { // std.str→std.string, slc→slice, len→length
		t.Errorf("transformations = %d, want 3", got.Transformations)
	}
}

func TestTransformSkipsStrings(t *testing.T) {
	input := `"str.len in a string" str.len x`
	got := Transform(input, Pretty)
	want := `"str.len in a string" string.length x`
	if got.Source != want {
		t.Errorf("String skip: got %q, want %q", got.Source, want)
	}
	if got.Transformations != 1 {
		t.Errorf("transformations = %d, want 1", got.Transformations)
	}
}

func TestTransformSkipsComments(t *testing.T) {
	input := "# str.len comment\nstr.len x"
	got := Transform(input, Pretty)
	want := "# str.len comment\nstring.length x"
	if got.Source != want {
		t.Errorf("Comment skip: got %q, want %q", got.Source, want)
	}
}

func TestTransformNoChange(t *testing.T) {
	input := "foo bar baz"
	got := Transform(input, Pretty)
	if got.Source != input {
		t.Errorf("No-change: got %q, want %q", got.Source, input)
	}
	if got.Transformations != 0 {
		t.Errorf("transformations = %d, want 0", got.Transformations)
	}
}

func TestTransformEscapedString(t *testing.T) {
	input := `"escaped \"str.len\" inside" str.len x`
	got := Transform(input, Pretty)
	want := `"escaped \"str.len\" inside" string.length x`
	if got.Source != want {
		t.Errorf("Escaped string: got %q, want %q", got.Source, want)
	}
}

func TestRoundTrip(t *testing.T) {
	inputs := []string{
		"str.len x\nfs.read path\nlen xs",
		"use std.str (slc len)\nstr.slc x 0 3",
		`"str.len" str.len x # str.len`,
	}
	for _, input := range inputs {
		pretty := Transform(input, Pretty)
		back := Transform(pretty.Source, Terse)
		if back.Source != input {
			t.Errorf("Round-trip failed:\n  input:  %q\n  pretty: %q\n  terse:  %q", input, pretty.Source, back.Source)
		}
	}
}

func TestTransformNewlineResetsUse(t *testing.T) {
	input := "use std.str\nstr.len x"
	got := Transform(input, Pretty)
	want := "use std.string\nstring.length x"
	if got.Source != want {
		t.Errorf("Newline reset: got %q, want %q", got.Source, want)
	}
}

func TestTransformIdenticalPairsSkipped(t *testing.T) {
	// map.new → map.new should not count as a transformation
	input := "map.new"
	got := Transform(input, Pretty)
	if got.Transformations != 0 {
		t.Errorf("Identical pair should not be a transformation, got %d", got.Transformations)
	}
}

func TestDirectionString(t *testing.T) {
	if Pretty.String() != "pretty" {
		t.Errorf("Pretty.String() = %q", Pretty.String())
	}
	if Terse.String() != "terse" {
		t.Errorf("Terse.String() = %q", Terse.String())
	}
}
