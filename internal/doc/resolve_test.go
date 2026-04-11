package doc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsGitHubTarget(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"github.com/foo/bar", true},
		{"https://github.com/foo/bar", true},
		{"http://github.com/foo/bar", true},
		{"foo/bar", true},
		{"foo/bar@v1.2.3", true},
		{"http", false},
		{"http@1.2", false},
		{"foo/bar/baz", false}, // too many slashes — treated as path-ish
		{"foo/bar.clk", false}, // looks like a file
		{"/cmd/main.clk", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isGitHubTarget(c.in); got != c.want {
			t.Errorf("isGitHubTarget(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseGitHubTarget(t *testing.T) {
	cases := []struct {
		in       string
		wantSlug string
		wantRef  string
	}{
		{"github.com/foo/bar", "foo/bar", ""},
		{"https://github.com/foo/bar", "foo/bar", ""},
		{"foo/bar", "foo/bar", ""},
		{"foo/bar@v1.2.3", "foo/bar", "v1.2.3"},
		{"github.com/foo/bar@main", "foo/bar", "main"},
	}
	for _, c := range cases {
		slug, ref := parseGitHubTarget(c.in)
		if slug != c.wantSlug || ref != c.wantRef {
			t.Errorf("parseGitHubTarget(%q) = (%q, %q), want (%q, %q)",
				c.in, slug, ref, c.wantSlug, c.wantRef)
		}
	}
}

func TestSplitNameVersion(t *testing.T) {
	cases := []struct {
		in, wantN, wantV string
	}{
		{"http", "http", ""},
		{"http@1.2.3", "http", "1.2.3"},
		{"http@latest", "http", "latest"},
		{"@scoped", "@scoped", ""}, // no version — @ at index 0
	}
	for _, c := range cases {
		n, v := splitNameVersion(c.in)
		if n != c.wantN || v != c.wantV {
			t.Errorf("splitNameVersion(%q) = (%q, %q), want (%q, %q)",
				c.in, n, v, c.wantN, c.wantV)
		}
	}
}

func TestWalkClkFiles(t *testing.T) {
	root := t.TempDir()
	// layout:
	//   a.clk
	//   sub/b.clk
	//   test/skip.clk    (should be skipped)
	//   .git/skip.clk    (should be skipped)
	//   README.md        (not .clk)
	mustWrite(t, filepath.Join(root, "a.clk"), "def a() -> int = 1")
	mustWrite(t, filepath.Join(root, "sub", "b.clk"), "def b() -> int = 2")
	mustWrite(t, filepath.Join(root, "test", "skip.clk"), "def skip() -> int = 3")
	mustWrite(t, filepath.Join(root, ".git", "skip.clk"), "x")
	mustWrite(t, filepath.Join(root, "README.md"), "docs")

	files, err := WalkClkFiles(root)
	if err != nil {
		t.Fatalf("WalkClkFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 .clk files, got %d: %v", len(files), files)
	}
	for _, f := range files {
		base := filepath.Base(f)
		if base != "a.clk" && base != "b.clk" {
			t.Errorf("unexpected file %s", f)
		}
	}
}

func TestResolveTargetEmpty(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "clank.pkg"), "name = \"demo\"\nversion = \"0.1.0\"\n")
	mustWrite(t, filepath.Join(root, "main.clk"), "def main() -> int = 1")

	res, err := ResolveTarget("", root, nil)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if res.Kind != "project" {
		t.Errorf("Kind = %q, want project", res.Kind)
	}
	if len(res.Files) != 1 || !strings.HasSuffix(res.Files[0], "main.clk") {
		t.Errorf("Files = %v", res.Files)
	}
}

func TestResolveTargetProjectPathFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "clank.pkg"), "name = \"demo\"\nversion = \"0.1.0\"\n")
	mustWrite(t, filepath.Join(root, "cmd", "main.clk"), "def main() -> int = 1")
	mustWrite(t, filepath.Join(root, "cmd", "other.clk"), "def other() -> int = 2")

	// Run from a subdirectory — projectRoot should still find clank.pkg above.
	res, err := ResolveTarget("/cmd/main.clk", filepath.Join(root, "cmd"), nil)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if res.Kind != "path" {
		t.Errorf("Kind = %q, want path", res.Kind)
	}
	if len(res.Files) != 1 || filepath.Base(res.Files[0]) != "main.clk" {
		t.Errorf("Files = %v", res.Files)
	}
}

func TestResolveTargetProjectPathDir(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "clank.pkg"), "name = \"demo\"\nversion = \"0.1.0\"\n")
	mustWrite(t, filepath.Join(root, "cmd", "a.clk"), "def a() -> int = 1")
	mustWrite(t, filepath.Join(root, "cmd", "b.clk"), "def b() -> int = 2")

	res, err := ResolveTarget("/cmd", root, nil)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if res.Kind != "path" {
		t.Errorf("Kind = %q, want path", res.Kind)
	}
	if len(res.Files) != 2 {
		t.Errorf("expected 2 files, got %v", res.Files)
	}
}

func TestResolveTargetAbsolutePathOutsideProject(t *testing.T) {
	// Regression: on Linux, t.TempDir() returns "/tmp/..." — a real
	// absolute path that also starts with "/". Earlier code matched the
	// "/..." project-relative branch first and rewrote it to
	// <projectRoot>/tmp/..., producing a false "path not found". The
	// real-absolute-path check must win.
	projectRoot := t.TempDir()
	mustWrite(t, filepath.Join(projectRoot, "clank.pkg"),
		"name = \"demo\"\nversion = \"0.1.0\"\n")

	// A file that lives outside the project root entirely.
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.clk")
	mustWrite(t, outsideFile, "def outside-fn() -> int = 1")

	res, err := ResolveTarget(outsideFile, projectRoot, nil)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if res.Kind != "path" {
		t.Errorf("Kind = %q, want path", res.Kind)
	}
	if len(res.Files) != 1 || res.Files[0] != outsideFile {
		t.Errorf("Files = %v, want %q", res.Files, outsideFile)
	}
}

func TestResolveTargetProjectPathMissing(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "clank.pkg"), "name = \"demo\"\nversion = \"0.1.0\"\n")

	_, err := ResolveTarget("/does/not/exist", root, nil)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !strings.Contains(err.Error(), "path not found") {
		t.Errorf("error = %v, want 'path not found'", err)
	}
}

func TestResolveTargetGitHubUsesFetcher(t *testing.T) {
	// Fake fetcher: returns a temp dir with a .clk file in it.
	fakeRoot := t.TempDir()
	mustWrite(t, filepath.Join(fakeRoot, "lib.clk"), "def greet() -> int = 42")

	var gotSlug, gotRef string
	fetcher := func(slug, ref string) (string, error) {
		gotSlug = slug
		gotRef = ref
		return fakeRoot, nil
	}

	res, err := ResolveTarget("github.com/foo/bar@v1.0.0", t.TempDir(), fetcher)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if gotSlug != "foo/bar" || gotRef != "v1.0.0" {
		t.Errorf("fetcher got (%q, %q), want (foo/bar, v1.0.0)", gotSlug, gotRef)
	}
	if res.Kind != "github" {
		t.Errorf("Kind = %q, want github", res.Kind)
	}
	if len(res.Files) != 1 {
		t.Errorf("expected 1 file, got %v", res.Files)
	}
}

func TestResolveTargetGitHubRefusesWithoutFetcher(t *testing.T) {
	_, err := ResolveTarget("foo/bar", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error when fetcher is nil")
	}
	if !strings.Contains(err.Error(), "refusing to fetch") {
		t.Errorf("error = %v", err)
	}
}

func TestResolveTargetManifestPathDep(t *testing.T) {
	root := t.TempDir()
	// Local dep layout:
	//   root/clank.pkg (depends on "util" at path ./libs/util)
	//   root/libs/util/clank.pkg
	//   root/libs/util/util.clk
	mustWrite(t, filepath.Join(root, "clank.pkg"),
		"name = \"demo\"\nversion = \"0.1.0\"\n[deps]\nutil = { path = \"./libs/util\" }\n")
	mustWrite(t, filepath.Join(root, "libs", "util", "clank.pkg"),
		"name = \"util\"\nversion = \"0.1.0\"\n")
	mustWrite(t, filepath.Join(root, "libs", "util", "util.clk"),
		"def helper() -> int = 1")

	res, err := ResolveTarget("util", root, nil)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if res.Kind != "dep" {
		t.Errorf("Kind = %q, want dep", res.Kind)
	}
	if len(res.Files) != 1 || filepath.Base(res.Files[0]) != "util.clk" {
		t.Errorf("Files = %v", res.Files)
	}
}

func TestResolveTargetUnknownBareName(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "clank.pkg"), "name = \"demo\"\nversion = \"0.1.0\"\n")

	_, err := ResolveTarget("nonexistent-lib-xyz", root, nil)
	if err == nil {
		t.Fatal("expected error for unknown library")
	}
	if !strings.Contains(err.Error(), "unknown library") {
		t.Errorf("error = %v", err)
	}
}

// mustWrite creates the parent directory and writes content to the file,
// failing the test on any error.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
