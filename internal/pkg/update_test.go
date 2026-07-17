package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCachePkg drops a minimal package into the cache dir.
func fakeCachePkg(t *testing.T, cacheDir, name, version, manifest string) string {
	t.Helper()
	path := filepath.Join(cacheDir, name+"@"+version)
	if err := os.MkdirAll(filepath.Join(path, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if manifest == "" {
		manifest = "name = \"" + name + "\"\nversion = \"" + version + "\"\n"
	}
	os.WriteFile(filepath.Join(path, "clank.pkg"), []byte(manifest), 0644)
	os.WriteFile(filepath.Join(path, "src", name+".clk"), []byte(""), 0644)
	return path
}

// fakeFetch overrides fetchGitHubFunc to serve packages from a map of
// slug → directory. Restores the original on cleanup.
func fakeFetch(t *testing.T, repos map[string]map[string]string) {
	t.Helper()
	origFetch := fetchGitHubFunc
	fetchGitHubFunc = func(slug, version string) (string, error) {
		files, ok := repos[slug]
		if !ok {
			return "", os.ErrNotExist
		}
		tmp, err := os.MkdirTemp("", "clank-fake-fetch-*")
		if err != nil {
			return "", err
		}
		for rel, content := range files {
			path := filepath.Join(tmp, rel)
			os.MkdirAll(filepath.Dir(path), 0755)
			os.WriteFile(path, []byte(content), 0644)
		}
		return tmp, nil
	}
	t.Cleanup(func() { fetchGitHubFunc = origFetch })
}

func useTempCache(t *testing.T) string {
	t.Helper()
	cacheDir := t.TempDir()
	globalCacheDirOverride = cacheDir
	t.Cleanup(func() { globalCacheDirOverride = "" })
	return cacheDir
}

// ── FindCachedPackage ──

func TestFindCachedPackageHighestVersion(t *testing.T) {
	cacheDir := useTempCache(t)
	fakeCachePkg(t, cacheDir, "lib", "0.1.0", "")
	fakeCachePkg(t, cacheDir, "lib", "0.10.0", "")
	fakeCachePkg(t, cacheDir, "lib", "0.2.0", "")

	_, version := FindCachedPackage("lib", "*")
	if version != "0.10.0" {
		t.Errorf("expected highest version 0.10.0, got %q", version)
	}
}

func TestFindCachedPackagePrefersSemverOverSnapshot(t *testing.T) {
	cacheDir := useTempCache(t)
	fakeCachePkg(t, cacheDir, "lib", "latest", "name = \"lib\"\nversion = \"0.0.1\"\n")
	fakeCachePkg(t, cacheDir, "lib", "0.1.0", "")

	_, version := FindCachedPackage("lib", "*")
	if version != "0.1.0" {
		t.Errorf("expected semver entry to win, got %q", version)
	}
}

func TestFindCachedPackageConstraint(t *testing.T) {
	cacheDir := useTempCache(t)
	fakeCachePkg(t, cacheDir, "lib", "1.0.0", "")
	fakeCachePkg(t, cacheDir, "lib", "2.0.0", "")

	_, version := FindCachedPackage("lib", "1.0.0")
	if version != "1.0.0" {
		t.Errorf("expected exact pin 1.0.0, got %q", version)
	}
	path, _ := FindCachedPackage("lib", "3.0.0")
	if path != "" {
		t.Errorf("expected no match for 3.0.0, got %q", path)
	}
}

// ── Transitive install ──

func TestPkgInstallTransitiveDeps(t *testing.T) {
	cacheDir := useTempCache(t)
	fakeFetch(t, map[string]map[string]string{
		"owner/a": {
			"clank.pkg": "name = \"lib-a\"\nversion = \"1.0.0\"\n\n[deps]\nlib-b = { github = \"owner/b\" }\n",
			"src/a.clk": "pub a : () -> <> Int =\n  1\n",
		},
		"owner/b": {
			"clank.pkg": "name = \"lib-b\"\nversion = \"2.0.0\"\n",
			"src/b.clk": "pub b : () -> <> Int =\n  2\n",
		},
	})

	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nlib-a = { github = \"owner/a\" }\n"), 0644)

	result := PkgInstall(PkgInstallOptions{Dir: projDir})
	if !result.Ok {
		t.Fatalf("install failed: %s", result.Error)
	}
	if len(result.Installed) != 2 {
		t.Fatalf("expected 2 installed (direct + transitive), got %d", len(result.Installed))
	}

	// Both should be cached under their real manifest versions, not "latest".
	if _, err := os.Stat(filepath.Join(cacheDir, "lib-a@1.0.0", "clank.pkg")); err != nil {
		t.Error("lib-a not cached under manifest version")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "lib-b@2.0.0", "clank.pkg")); err != nil {
		t.Error("transitive lib-b not cached under manifest version")
	}
}

// ── Transitive resolution from cache ──

func TestResolvePackagesTransitiveFromCache(t *testing.T) {
	cacheDir := useTempCache(t)
	fakeCachePkg(t, cacheDir, "lib-a", "1.0.0",
		"name = \"lib-a\"\nversion = \"1.0.0\"\n\n[deps]\nlib-b = { github = \"owner/b\" }\n")
	fakeCachePkg(t, cacheDir, "lib-b", "2.0.0", "")

	projDir := t.TempDir()
	manifestPath := filepath.Join(projDir, "clank.pkg")
	os.WriteFile(manifestPath,
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nlib-a = { github = \"owner/a\" }\n"), 0644)

	resolution, err := ResolvePackages(manifestPath, false)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(resolution.Packages) != 2 {
		t.Fatalf("expected 2 packages (direct + transitive), got %d", len(resolution.Packages))
	}
	if _, ok := resolution.PackageFiles["lib-b"]; !ok {
		t.Error("transitive lib-b missing from PackageFiles map")
	}
}

// ── pkg update ──

func TestPkgUpdateRefreshesUnpinned(t *testing.T) {
	cacheDir := useTempCache(t)
	fakeCachePkg(t, cacheDir, "lib", "1.0.0", "")
	fakeFetch(t, map[string]map[string]string{
		"owner/lib": {
			"clank.pkg": "name = \"lib\"\nversion = \"1.1.0\"\n",
			"src/l.clk": "pub l : () -> <> Int =\n  1\n",
		},
	})

	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nlib = { github = \"owner/lib\" }\n"), 0644)

	result := PkgUpdate(PkgUpdateOptions{Dir: projDir})
	if !result.Ok {
		t.Fatalf("update failed: %s", result.Error)
	}
	if len(result.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(result.Updated))
	}
	u := result.Updated[0]
	if u.OldVersion != "1.0.0" || u.NewVersion != "1.1.0" || !u.Changed {
		t.Errorf("unexpected update record: %+v", u)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "lib@1.1.0", "clank.pkg")); err != nil {
		t.Error("new version not cached")
	}
	// Lockfile refreshed.
	if _, err := os.Stat(filepath.Join(projDir, "clank.lock")); err != nil {
		t.Error("lockfile not written")
	}
}

func TestPkgUpdateSkipsPinned(t *testing.T) {
	useTempCache(t)
	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nlib = { github = \"owner/lib\", version = \"1.0.0\" }\n"), 0644)

	result := PkgUpdate(PkgUpdateOptions{Dir: projDir})
	if !result.Ok {
		t.Fatalf("update failed: %s", result.Error)
	}
	if len(result.Updated) != 0 {
		t.Errorf("pinned dep should not be updated: %+v", result.Updated)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "lib" {
		t.Errorf("expected lib skipped, got %v", result.Skipped)
	}
}

func TestPkgUpdateUnknownName(t *testing.T) {
	useTempCache(t)
	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := PkgUpdate(PkgUpdateOptions{Dir: projDir, Name: "nope"})
	if result.Ok {
		t.Fatal("expected failure for unknown dep")
	}
	if !strings.Contains(result.Error, "nope") {
		t.Errorf("error should name the dep: %q", result.Error)
	}
}

// ── pkg add unpinned constraint ──

func TestPkgAddFromTargetUnpinnedWritesStar(t *testing.T) {
	useTempCache(t)
	fakeFetch(t, map[string]map[string]string{
		"owner/lib": {
			"clank.pkg": "name = \"lib\"\nversion = \"0.3.0\"\n",
			"src/l.clk": "pub l : () -> <> Int =\n  1\n",
		},
	})

	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := PkgAddFromTarget("github.com/owner/lib", false, projDir)
	if !result.Ok {
		t.Fatalf("add failed: %s", result.Error)
	}
	if result.Constraint != "*" {
		t.Errorf("unpinned add should record '*', got %q", result.Constraint)
	}

	m, _ := LoadManifest(filepath.Join(projDir, "clank.pkg"))
	if m.Deps["lib"].Constraint != "*" {
		t.Errorf("manifest constraint: %q", m.Deps["lib"].Constraint)
	}
}

func TestPkgAddFromTargetPinnedNormalizesRef(t *testing.T) {
	cacheDir := useTempCache(t)
	fakeFetch(t, map[string]map[string]string{
		"owner/lib": {
			"clank.pkg": "name = \"lib\"\nversion = \"2.0.0\"\n",
			"src/l.clk": "pub l : () -> <> Int =\n  1\n",
		},
	})

	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := PkgAddFromTarget("owner/lib@v2.0.0", false, projDir)
	if !result.Ok {
		t.Fatalf("add failed: %s", result.Error)
	}
	if result.Constraint != "2.0.0" {
		t.Errorf("pin should be normalized to 2.0.0, got %q", result.Constraint)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "lib@2.0.0", "clank.pkg")); err != nil {
		t.Error("pinned version not cached under normalized key")
	}
}
