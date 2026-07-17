package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillMarkdownFormat(t *testing.T) {
	if !strings.HasPrefix(skillMarkdown, "---\nname: clank\n") {
		t.Error("skill must start with frontmatter (name: clank)")
	}
	if !strings.Contains(skillMarkdown, "description:") {
		t.Error("skill frontmatter missing description")
	}
	// The pieces an agent needs to be productive.
	for _, want := range []string{"clank spec", "clank check", "clank pkg add", "use &", "--json"} {
		if !strings.Contains(skillMarkdown, want) {
			t.Errorf("skill missing %q", want)
		}
	}
}

func TestSkillInstallWritesFile(t *testing.T) {
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	if code := cmdSkill([]string{"install"}, false, nil); code != 0 {
		t.Fatalf("install returned %d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "clank", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md not written: %v", err)
	}
	if string(data) != skillMarkdown {
		t.Error("written skill differs from embedded markdown")
	}
}
