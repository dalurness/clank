package pkg

import "testing"

func TestVersionSatisfies(t *testing.T) {
	tests := []struct {
		version    string
		constraint string
		want       bool
	}{
		// Wildcard
		{"1.0.0", "*", true},
		{"0.0.1", "*", true},

		// Exact version
		{"1.2.3", "1.2.3", true},
		{"1.2.4", "1.2.3", false},

		// Compatible range: "1.2" means >= 1.2.0, < 2.0.0
		{"1.2.0", "1.2", true},
		{"1.3.0", "1.2", true},
		{"1.1.9", "1.2", false},
		{"2.0.0", "1.2", false},

		// Major only: "1" means >= 1.0.0, < 2.0.0
		{"1.0.0", "1", true},
		{"1.9.9", "1", true},
		{"2.0.0", "1", false},
		{"0.9.0", "1", false},

		// >= constraint
		{"1.2.0", ">= 1.2.0", true},
		{"1.3.0", ">= 1.2.0", true},
		{"1.1.9", ">= 1.2.0", false},

		// < constraint
		{"1.9.9", "< 2.0.0", true},
		{"2.0.0", "< 2.0.0", false},

		// Compound: ">= 1.2.0, < 2.0.0"
		{"1.2.0", ">= 1.2.0, < 2.0.0", true},
		{"1.9.9", ">= 1.2.0, < 2.0.0", true},
		{"2.0.0", ">= 1.2.0, < 2.0.0", false},
		{"1.1.9", ">= 1.2.0, < 2.0.0", false},

		// Invalid
		{"notaversion", "1.0", false},
	}

	for _, tt := range tests {
		got := VersionSatisfies(tt.version, tt.constraint)
		if got != tt.want {
			t.Errorf("VersionSatisfies(%q, %q) = %v, want %v", tt.version, tt.constraint, got, tt.want)
		}
	}
}

func TestSelectVersion(t *testing.T) {
	versions := []string{"0.1.0", "0.2.0", "1.0.0", "1.1.0", "1.2.0", "2.0.0"}

	tests := []struct {
		constraint string
		want       string
	}{
		{"*", "0.1.0"},
		{"1.0", "1.0.0"},
		{"1.1", "1.1.0"},
		{">= 1.0.0, < 2.0.0", "1.0.0"},
		{">= 1.1.0", "1.1.0"},
		{">= 3.0.0", ""},
	}

	for _, tt := range tests {
		got := SelectVersion(versions, tt.constraint)
		if got != tt.want {
			t.Errorf("SelectVersion(%q) = %q, want %q", tt.constraint, got, tt.want)
		}
	}
}

func TestMergeConstraints(t *testing.T) {
	tests := []struct {
		constraints []string
		want        string
	}{
		{nil, "*"},
		{[]string{"1.2"}, "1.2"},
		{[]string{"*"}, "*"},
		{[]string{"*", "*"}, "*"},
	}

	for _, tt := range tests {
		got := MergeConstraints(tt.constraints)
		if got != tt.want {
			t.Errorf("MergeConstraints(%v) = %q, want %q", tt.constraints, got, tt.want)
		}
	}
}

func TestMergeConstraintsMVS(t *testing.T) {
	// Two compatible ranges should produce merged constraint
	result := MergeConstraints([]string{"1.2", "1.4"})
	// The merged constraint should satisfy both: >= 1.4.0, < 2.0.0
	if !VersionSatisfies("1.4.0", result) {
		t.Errorf("merged constraint %q should satisfy 1.4.0", result)
	}
	if VersionSatisfies("1.3.0", result) {
		t.Errorf("merged constraint %q should not satisfy 1.3.0", result)
	}
}
