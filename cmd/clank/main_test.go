package main

import "testing"

func TestWrapExprSource(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantWrapped bool // true if we expect wrapping to occur
	}{
		{"bare expression", "1 + 2", true},
		{"function call", "div(10, 2)", true},
		{"string literal", `"hello"`, true},
		{"type decl", "type Option<a> = Some(a) | None", false},
		{"effect decl", "effect Foo { op : () -> Int }", false},
		{"definition with annotation", "foo : Int = 42", false},
		{"pub definition", "pub bar : Int = 1", false},
		{"comment then expr", "# comment\n1 + 2", true},
		{"blank lines then expr", "\n\n  1 + 2  \n", true},
		{"empty string", "", true},
		{"only comments", "# just a comment", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapExprSource(tt.input)
			wasWrapped := result != tt.input
			if wasWrapped != tt.wantWrapped {
				if tt.wantWrapped {
					t.Errorf("expected wrapping, got: %q", result)
				} else {
					t.Errorf("expected no wrapping, got: %q", result)
				}
			}
		})
	}
}
