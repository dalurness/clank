package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Manifest parsing ──

func TestParseMinimalManifest(t *testing.T) {
	m, err := ParseManifest("name = \"my-app\"\nversion = \"1.0.0\"\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "my-app" {
		t.Errorf("name: got %q, want 'my-app'", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Errorf("version: got %q, want '1.0.0'", m.Version)
	}
}

func TestParseFullManifest(t *testing.T) {
	source := `
name = "my-app"
version = "1.0.0"
entry = "main"
description = "A test app"
license = "MIT"
repository = "https://github.com/test/my-app"
authors = ["alice", "bob"]
clank = ">= 0.2.0"
keywords = ["http", "server"]

[deps]
net-http = "1.2"
json-schema = "0.3"

[dev-deps]
test-helpers = "0.1"

[effects]
io = true
async = true

[exports]
modules = ["main", "lib.parser"]
`
	m, err := ParseManifest(source, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "my-app" {
		t.Errorf("name: %q", m.Name)
	}
	if m.Entry != "main" {
		t.Errorf("entry: %q", m.Entry)
	}
	if m.Description != "A test app" {
		t.Errorf("description: %q", m.Description)
	}
	if m.License != "MIT" {
		t.Errorf("license: %q", m.License)
	}
	if len(m.Authors) != 2 {
		t.Errorf("authors count: %d", len(m.Authors))
	}
	if m.Authors[0] != "alice" {
		t.Errorf("first author: %q", m.Authors[0])
	}
	if len(m.Keywords) != 2 {
		t.Errorf("keywords count: %d", len(m.Keywords))
	}
	if len(m.Deps) != 2 {
		t.Errorf("deps count: %d", len(m.Deps))
	}
	if dep, ok := m.Deps["net-http"]; !ok || dep.Constraint != "1.2" {
		t.Errorf("net-http dep: %+v", m.Deps["net-http"])
	}
	if len(m.DevDeps) != 1 {
		t.Errorf("dev-deps count: %d", len(m.DevDeps))
	}
	if len(m.Effects) != 2 {
		t.Errorf("effects count: %d", len(m.Effects))
	}
	if !m.Effects["io"] {
		t.Error("io effect should be true")
	}
	if len(m.Exports) != 2 {
		t.Errorf("exports count: %d", len(m.Exports))
	}
	if m.Exports[0] != "main" {
		t.Errorf("first export: %q", m.Exports[0])
	}
}

func TestParsePathDep(t *testing.T) {
	m, err := ParseManifest("name = \"app\"\nversion = \"0.1.0\"\n\n[deps]\nmy-lib = { path = \"../my-lib\" }\n", "")
	if err != nil {
		t.Fatal(err)
	}
	dep, ok := m.Deps["my-lib"]
	if !ok {
		t.Fatal("missing my-lib dep")
	}
	if dep.Path != "../my-lib" {
		t.Errorf("path: %q", dep.Path)
	}
	if dep.Constraint != "*" {
		t.Errorf("default constraint: %q", dep.Constraint)
	}
}

func TestParseComments(t *testing.T) {
	m, err := ParseManifest("# This is a comment\nname = \"app\" # inline comment\nversion = \"1.0.0\"\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "app" {
		t.Errorf("name: %q", m.Name)
	}
}

func TestParseEmptySections(t *testing.T) {
	m, err := ParseManifest("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\n\n[effects]\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Deps) != 0 {
		t.Errorf("deps: %d", len(m.Deps))
	}
	if len(m.Effects) != 0 {
		t.Errorf("effects: %d", len(m.Effects))
	}
}

func TestErrorMissingName(t *testing.T) {
	_, err := ParseManifest("version = \"1.0.0\"\n", "")
	if err == nil {
		t.Fatal("expected error")
	}
	pkgErr, ok := err.(*PkgError)
	if !ok {
		t.Fatalf("expected PkgError, got %T", err)
	}
	if pkgErr.Code != "E508" {
		t.Errorf("code: %q", pkgErr.Code)
	}
}

func TestErrorMissingVersion(t *testing.T) {
	_, err := ParseManifest("name = \"app\"\n", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*PkgError); !ok {
		t.Fatalf("expected PkgError, got %T", err)
	}
}

func TestErrorInvalidName(t *testing.T) {
	_, err := ParseManifest("name = \"MyApp\"\nversion = \"1.0.0\"\n", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*PkgError); !ok {
		t.Fatalf("expected PkgError, got %T", err)
	}
}

func TestErrorUnknownSection(t *testing.T) {
	_, err := ParseManifest("name = \"app\"\nversion = \"1.0.0\"\n\n[unknown]\n", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestErrorMalformedLine(t *testing.T) {
	_, err := ParseManifest("name = \"app\"\nversion = \"1.0.0\"\nbadline\n", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Serialization ──

func TestRoundTripMinimal(t *testing.T) {
	source := "name = \"my-app\"\nversion = \"1.0.0\"\n"
	m, err := ParseManifest(source, "")
	if err != nil {
		t.Fatal(err)
	}
	serialized := SerializeManifest(m)
	m2, err := ParseManifest(serialized, "")
	if err != nil {
		t.Fatal(err)
	}
	if m2.Name != m.Name {
		t.Errorf("name: %q vs %q", m2.Name, m.Name)
	}
	if m2.Version != m.Version {
		t.Errorf("version: %q vs %q", m2.Version, m.Version)
	}
}

func TestRoundTripWithDeps(t *testing.T) {
	source := "name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nfoo = \"1.0\"\nbar = { path = \"../bar\" }\n"
	m, err := ParseManifest(source, "")
	if err != nil {
		t.Fatal(err)
	}
	serialized := SerializeManifest(m)
	m2, err := ParseManifest(serialized, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Deps) != 2 {
		t.Errorf("deps preserved: %d", len(m2.Deps))
	}
	if dep, ok := m2.Deps["bar"]; !ok || dep.Path != "../bar" {
		t.Errorf("path dep preserved: %+v", m2.Deps["bar"])
	}
}

// ── Local dependency resolution ──

func TestResolveSingleLocalDep(t *testing.T) {
	tmp := t.TempDir()

	// Create root package
	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"my-app\"\nversion = \"1.0.0\"\n\n[deps]\nmy-lib = { path = \"../my-lib\" }\n"), 0644)

	// Create dep package
	os.MkdirAll(filepath.Join(tmp, "my-lib", "src"), 0755)
	os.WriteFile(filepath.Join(tmp, "my-lib", "clank.pkg"),
		[]byte("name = \"my-lib\"\nversion = \"0.2.0\"\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "my-lib", "src", "utils.clk"),
		[]byte("mod my-lib.utils\n"), 0644)

	manifest, err := LoadManifest(filepath.Join(tmp, "root", "clank.pkg"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveLocalDeps(manifest, filepath.Join(tmp, "root"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved dep, got %d", len(resolved))
	}
	if resolved[0].Name != "my-lib" {
		t.Errorf("name: %q", resolved[0].Name)
	}
	if resolved[0].Manifest.Version != "0.2.0" {
		t.Errorf("version: %q", resolved[0].Manifest.Version)
	}
	if _, ok := resolved[0].Modules["my-lib.utils"]; !ok {
		t.Error("expected my-lib.utils module")
	}
}

func TestResolveTransitiveDeps(t *testing.T) {
	tmp := t.TempDir()

	// root -> lib-a -> lib-b
	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nlib-a = { path = \"../lib-a\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "lib-a", "src"), 0755)
	os.WriteFile(filepath.Join(tmp, "lib-a", "clank.pkg"),
		[]byte("name = \"lib-a\"\nversion = \"0.1.0\"\n\n[deps]\nlib-b = { path = \"../lib-b\" }\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "lib-a", "src", "a.clk"), []byte("mod lib-a.a\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "lib-b", "src"), 0755)
	os.WriteFile(filepath.Join(tmp, "lib-b", "clank.pkg"),
		[]byte("name = \"lib-b\"\nversion = \"0.1.0\"\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "lib-b", "src", "b.clk"), []byte("mod lib-b.b\n"), 0644)

	manifest, _ := LoadManifest(filepath.Join(tmp, "root", "clank.pkg"))
	resolved, err := ResolveLocalDeps(manifest, filepath.Join(tmp, "root"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 deps (transitive), got %d", len(resolved))
	}
	names := make(map[string]bool)
	for _, r := range resolved {
		names[r.Name] = true
	}
	if !names["lib-a"] || !names["lib-b"] {
		t.Error("missing lib-a or lib-b")
	}
}

func TestCircularDependency(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "a"), 0755)
	os.WriteFile(filepath.Join(tmp, "a", "clank.pkg"),
		[]byte("name = \"a\"\nversion = \"1.0.0\"\n\n[deps]\nb = { path = \"../b\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "b"), 0755)
	os.WriteFile(filepath.Join(tmp, "b", "clank.pkg"),
		[]byte("name = \"b\"\nversion = \"1.0.0\"\n\n[deps]\na = { path = \"../a\" }\n"), 0644)

	manifest, _ := LoadManifest(filepath.Join(tmp, "a", "clank.pkg"))
	_, err := ResolveLocalDeps(manifest, filepath.Join(tmp, "a"), false)
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
	pkgErr, ok := err.(*PkgError)
	if !ok {
		t.Fatalf("expected PkgError, got %T", err)
	}
	if pkgErr.Code != "E505" {
		t.Errorf("error code: %q", pkgErr.Code)
	}
}

func TestMissingDep(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nmissing = { path = \"../missing\" }\n"), 0644)

	manifest, _ := LoadManifest(filepath.Join(tmp, "root", "clank.pkg"))
	_, err := ResolveLocalDeps(manifest, filepath.Join(tmp, "root"), false)
	if err == nil {
		t.Fatal("expected error")
	}
	pkgErr, ok := err.(*PkgError)
	if !ok {
		t.Fatalf("expected PkgError, got %T", err)
	}
	if pkgErr.Code != "E502" {
		t.Errorf("error code: %q", pkgErr.Code)
	}
}

func TestNameMismatch(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nfoo = { path = \"../wrong-name\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "wrong-name"), 0755)
	os.WriteFile(filepath.Join(tmp, "wrong-name", "clank.pkg"),
		[]byte("name = \"bar\"\nversion = \"1.0.0\"\n"), 0644)

	manifest, _ := LoadManifest(filepath.Join(tmp, "root", "clank.pkg"))
	_, err := ResolveLocalDeps(manifest, filepath.Join(tmp, "root"), false)
	if err == nil {
		t.Fatal("expected error")
	}
	pkgErr, ok := err.(*PkgError)
	if !ok {
		t.Fatalf("expected PkgError, got %T", err)
	}
	if pkgErr.Code != "E508" {
		t.Errorf("error code: %q", pkgErr.Code)
	}
}

func TestSkipRegistryDeps(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nremote-pkg = \"1.0\"\n"), 0644)

	manifest, _ := LoadManifest(filepath.Join(tmp, "root", "clank.pkg"))
	resolved, err := ResolveLocalDeps(manifest, filepath.Join(tmp, "root"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected 0 local deps, got %d", len(resolved))
	}
}

// ── Module discovery ──

func TestDiscoverModules(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "pkg", "src", "lib"), 0755)
	os.WriteFile(filepath.Join(tmp, "pkg", "src", "main.clk"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "pkg", "src", "lib", "parser.clk"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmp, "pkg", "src", "lib", "types.clk"), []byte(""), 0644)

	modules := DiscoverModules(filepath.Join(tmp, "pkg"), "my-pkg")
	if len(modules) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(modules))
	}
	if _, ok := modules["my-pkg.main"]; !ok {
		t.Error("missing my-pkg.main")
	}
	if _, ok := modules["my-pkg.lib.parser"]; !ok {
		t.Error("missing my-pkg.lib.parser")
	}
	if _, ok := modules["my-pkg.lib.types"]; !ok {
		t.Error("missing my-pkg.lib.types")
	}
}

func TestDiscoverModulesEmpty(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "pkg", "src"), 0755)
	modules := DiscoverModules(filepath.Join(tmp, "pkg"), "my-pkg")
	if len(modules) != 0 {
		t.Errorf("expected 0 modules, got %d", len(modules))
	}
}

func TestDiscoverModulesNoSrc(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "pkg"), 0755)
	modules := DiscoverModules(filepath.Join(tmp, "pkg"), "my-pkg")
	if len(modules) != 0 {
		t.Errorf("expected 0 modules, got %d", len(modules))
	}
}

// ── pkg init ──

func TestPkgInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new-pkg")
	os.MkdirAll(dir, 0755)
	result := PkgInit(PkgInitOptions{Name: "my-app", Dir: dir})
	if !result.Ok {
		t.Fatalf("expected ok, got error: %s", result.Error)
	}
	if result.Package != "my-app" {
		t.Errorf("package: %q", result.Package)
	}
	if _, err := os.Stat(filepath.Join(dir, "clank.pkg")); os.IsNotExist(err) {
		t.Error("clank.pkg not created")
	}
	if _, err := os.Stat(filepath.Join(dir, "src")); os.IsNotExist(err) {
		t.Error("src/ not created")
	}
	m, _ := LoadManifest(filepath.Join(dir, "clank.pkg"))
	if m.Name != "my-app" {
		t.Errorf("parsed name: %q", m.Name)
	}
	if m.Version != "0.1.0" {
		t.Errorf("default version: %q", m.Version)
	}
}

func TestPkgInitWithEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new-pkg")
	os.MkdirAll(dir, 0755)
	result := PkgInit(PkgInitOptions{Name: "my-app", Entry: "main", Dir: dir})
	if !result.Ok {
		t.Fatalf("expected ok, got error: %s", result.Error)
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "main.clk")); os.IsNotExist(err) {
		t.Error("entry file not created")
	}
	m, _ := LoadManifest(filepath.Join(dir, "clank.pkg"))
	if m.Entry != "main" {
		t.Errorf("entry: %q", m.Entry)
	}
}

func TestPkgInitAlreadyExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "existing")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"existing\"\nversion = \"1.0.0\"\n"), 0644)
	result := PkgInit(PkgInitOptions{Name: "existing", Dir: dir})
	if result.Ok {
		t.Error("expected failure")
	}
	if !strings.Contains(result.Error, "already exists") {
		t.Errorf("error: %q", result.Error)
	}
}

func TestPkgInitInvalidName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bad")
	os.MkdirAll(dir, 0755)
	result := PkgInit(PkgInitOptions{Name: "BadName", Dir: dir})
	if result.Ok {
		t.Error("expected failure")
	}
}

// ── pkg add ──

func TestPkgAddVersionDep(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "add-test")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := PkgAdd(PkgAddOptions{Name: "http-client", Constraint: "1.2", Dir: dir})
	if !result.Ok {
		t.Fatalf("expected ok: %s", result.Error)
	}
	if result.Name != "http-client" {
		t.Errorf("name: %q", result.Name)
	}
	if result.Section != "deps" {
		t.Errorf("section: %q", result.Section)
	}
	m, _ := LoadManifest(filepath.Join(dir, "clank.pkg"))
	if dep, ok := m.Deps["http-client"]; !ok || dep.Constraint != "1.2" {
		t.Error("dep not persisted")
	}
}

func TestPkgAddPathDep(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "add-path-test")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "my-lib"), 0755)
	os.WriteFile(filepath.Join(tmp, "my-lib", "clank.pkg"), []byte("name = \"my-lib\"\nversion = \"0.1.0\"\n"), 0644)

	result := PkgAdd(PkgAddOptions{Name: "my-lib", Path: "../my-lib", Dir: dir})
	if !result.Ok {
		t.Fatalf("expected ok: %s", result.Error)
	}
	if result.Path != "../my-lib" {
		t.Errorf("path: %q", result.Path)
	}
}

func TestPkgAddDevDep(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "add-dev-test")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := PkgAdd(PkgAddOptions{Name: "test-utils", Constraint: "0.1", Dev: true, Dir: dir})
	if !result.Ok {
		t.Fatalf("expected ok: %s", result.Error)
	}
	if result.Section != "dev-deps" {
		t.Errorf("section: %q", result.Section)
	}
	m, _ := LoadManifest(filepath.Join(dir, "clank.pkg"))
	if _, ok := m.DevDeps["test-utils"]; !ok {
		t.Error("dev dep not persisted")
	}
}

func TestPkgAddDuplicate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "add-dup-test")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nfoo = \"1.0\"\n"), 0644)

	result := PkgAdd(PkgAddOptions{Name: "foo", Constraint: "2.0", Dir: dir})
	if result.Ok {
		t.Error("expected failure")
	}
	if !strings.Contains(result.Error, "already exists") {
		t.Errorf("error: %q", result.Error)
	}
}

func TestPkgAddNoManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no-manifest")
	os.MkdirAll(dir, 0755)
	result := PkgAdd(PkgAddOptions{Name: "foo", Dir: dir})
	if result.Ok {
		t.Error("expected failure")
	}
	if !strings.Contains(result.Error, "No clank.pkg") {
		t.Errorf("error: %q", result.Error)
	}
}

// ── pkg remove ──

func TestPkgRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "remove-test")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nfoo = \"1.0\"\nbar = \"2.0\"\n"), 0644)

	result := PkgRemove(PkgRemoveOptions{Name: "foo", Dir: dir})
	if !result.Ok {
		t.Fatalf("expected ok: %s", result.Error)
	}
	m, _ := LoadManifest(filepath.Join(dir, "clank.pkg"))
	if _, ok := m.Deps["foo"]; ok {
		t.Error("foo should be removed")
	}
	if _, ok := m.Deps["bar"]; !ok {
		t.Error("bar should be preserved")
	}
}

func TestPkgRemoveDevDep(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "remove-dev-test")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"app\"\nversion = \"1.0.0\"\n\n[dev-deps]\ntest-utils = \"0.1\"\n"), 0644)

	result := PkgRemove(PkgRemoveOptions{Name: "test-utils", Dev: true, Dir: dir})
	if !result.Ok {
		t.Fatalf("expected ok: %s", result.Error)
	}
	m, _ := LoadManifest(filepath.Join(dir, "clank.pkg"))
	if _, ok := m.DevDeps["test-utils"]; ok {
		t.Error("test-utils should be removed")
	}
}

func TestPkgRemoveNonexistent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "remove-missing-test")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"), []byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := PkgRemove(PkgRemoveOptions{Name: "nonexistent", Dir: dir})
	if result.Ok {
		t.Error("expected failure")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("error: %q", result.Error)
	}
}

func TestPkgRemoveNoManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no-manifest")
	os.MkdirAll(dir, 0755)
	result := PkgRemove(PkgRemoveOptions{Name: "foo", Dir: dir})
	if result.Ok {
		t.Error("expected failure")
	}
}

// ── pkg resolve ──

func TestPkgResolve(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nmy-lib = { path = \"../my-lib\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "my-lib", "src"), 0755)
	os.WriteFile(filepath.Join(tmp, "my-lib", "clank.pkg"),
		[]byte("name = \"my-lib\"\nversion = \"0.1.0\"\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "my-lib", "src", "core.clk"), []byte(""), 0644)

	result := PkgResolve(filepath.Join(tmp, "root"))
	if !result.Ok {
		t.Fatalf("expected ok: %s", result.Error)
	}
	if len(result.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(result.Packages))
	}
	if result.Packages[0].Name != "my-lib" {
		t.Errorf("name: %q", result.Packages[0].Name)
	}
}

func TestPkgResolveNoManifest(t *testing.T) {
	result := PkgResolve(t.TempDir())
	if result.Ok {
		t.Error("expected failure")
	}
	if !strings.Contains(result.Error, "No clank.pkg") {
		t.Errorf("error: %q", result.Error)
	}
}

// ── Lockfile ──

func TestLockfileRoundTrip(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nmy-lib = { path = \"../my-lib\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "my-lib", "src"), 0755)
	os.WriteFile(filepath.Join(tmp, "my-lib", "clank.pkg"),
		[]byte("name = \"my-lib\"\nversion = \"0.2.0\"\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "my-lib", "src", "utils.clk"), []byte("mod my-lib.utils\n"), 0644)

	lock, err := GenerateLockfile(filepath.Join(tmp, "root", "clank.pkg"), false)
	if err != nil {
		t.Fatal(err)
	}
	if lock.LockVersion != 1 {
		t.Errorf("lock version: %d", lock.LockVersion)
	}
	keys := make([]string, 0, len(lock.Packages))
	for k := range lock.Packages {
		keys = append(keys, k)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 package, got %d", len(keys))
	}

	p, ok := lock.Packages["my-lib@0.2.0"]
	if !ok {
		t.Fatal("missing my-lib@0.2.0")
	}
	if p.Version != "0.2.0" {
		t.Errorf("version: %q", p.Version)
	}
	if !strings.HasPrefix(p.Integrity, "sha256:") {
		t.Errorf("integrity: %q", p.Integrity)
	}

	// Round-trip
	serialized := SerializeLockfile(lock)
	parsed, err := ParseLockfile(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.LockVersion != 1 {
		t.Errorf("parsed lock version: %d", parsed.LockVersion)
	}
	pp, ok := parsed.Packages["my-lib@0.2.0"]
	if !ok {
		t.Fatal("missing parsed my-lib@0.2.0")
	}
	if pp.Version != p.Version {
		t.Errorf("version: %q vs %q", pp.Version, p.Version)
	}
	if pp.Integrity != p.Integrity {
		t.Errorf("integrity mismatch")
	}
}

func TestWriteLockfile(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nmy-lib = { path = \"../my-lib\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "my-lib", "src"), 0755)
	os.WriteFile(filepath.Join(tmp, "my-lib", "clank.pkg"),
		[]byte("name = \"my-lib\"\nversion = \"0.1.0\"\n"), 0644)

	lockPath, err := WriteLockfile(filepath.Join(tmp, "root", "clank.pkg"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("clank.lock not created")
	}
	content, _ := os.ReadFile(lockPath)
	if !strings.Contains(string(content), "my-lib") {
		t.Error("lockfile should contain dep name")
	}
	if !strings.Contains(string(content), "sha256:") {
		t.Error("lockfile should contain integrity")
	}
}

func TestReadLockfileMissing(t *testing.T) {
	result := ReadLockfile("/tmp/nonexistent-clank.lock")
	if result != nil {
		t.Error("expected nil for missing file")
	}
}

func TestVerifyLockfileStale(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nmy-lib = { path = \"../my-lib\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "my-lib", "src"), 0755)
	os.WriteFile(filepath.Join(tmp, "my-lib", "clank.pkg"),
		[]byte("name = \"my-lib\"\nversion = \"0.1.0\"\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "my-lib", "src", "a.clk"), []byte("mod my-lib.a\n"), 0644)

	WriteLockfile(filepath.Join(tmp, "root", "clank.pkg"), false)

	// Verify ok
	result := VerifyLockfile(filepath.Join(tmp, "root", "clank.pkg"), false)
	if !result.Ok {
		t.Error("expected ok initially")
	}

	// Modify dep
	os.WriteFile(filepath.Join(tmp, "my-lib", "src", "a.clk"), []byte("mod my-lib.a\n# changed\n"), 0644)

	// Verify stale
	result = VerifyLockfile(filepath.Join(tmp, "root", "clank.pkg"), false)
	if result.Ok {
		t.Error("expected stale after change")
	}
	found := false
	for _, s := range result.Stale {
		if s == "my-lib" {
			found = true
		}
	}
	if !found {
		t.Error("my-lib should be stale")
	}
}

func TestVerifyLockfileMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "root")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := VerifyLockfile(filepath.Join(dir, "clank.pkg"), false)
	if result.Ok {
		t.Error("expected not ok")
	}
	found := false
	for _, m := range result.Missing {
		if m == "clank.lock" {
			found = true
		}
	}
	if !found {
		t.Error("should report missing clank.lock")
	}
}

func TestLockfileSorted(t *testing.T) {
	tmp := t.TempDir()

	os.MkdirAll(filepath.Join(tmp, "root"), 0755)
	os.WriteFile(filepath.Join(tmp, "root", "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nzeta-lib = { path = \"../zeta-lib\" }\nalpha-lib = { path = \"../alpha-lib\" }\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "zeta-lib"), 0755)
	os.WriteFile(filepath.Join(tmp, "zeta-lib", "clank.pkg"),
		[]byte("name = \"zeta-lib\"\nversion = \"0.1.0\"\n"), 0644)

	os.MkdirAll(filepath.Join(tmp, "alpha-lib"), 0755)
	os.WriteFile(filepath.Join(tmp, "alpha-lib", "clank.pkg"),
		[]byte("name = \"alpha-lib\"\nversion = \"0.2.0\"\n"), 0644)

	lock, _ := GenerateLockfile(filepath.Join(tmp, "root", "clank.pkg"), false)
	serialized := SerializeLockfile(lock)
	alphaIdx := strings.Index(serialized, "alpha-lib@")
	zetaIdx := strings.Index(serialized, "zeta-lib@")
	if alphaIdx >= zetaIdx {
		t.Error("alpha-lib should appear before zeta-lib in serialized output")
	}
}
