package pkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTarGz creates an in-memory .tar.gz containing the given files.
// files maps relative paths to content strings.
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for path, content := range files {
		hdr := &tar.Header{
			Name: path,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	tw.Close()
	gw.Close()
	return buf.Bytes()
}

// makeTarGzWithDir creates a tarball with a top-level directory (like GitHub archives).
func makeTarGzWithDir(t *testing.T, topDir string, files map[string]string) []byte {
	t.Helper()
	prefixed := make(map[string]string)
	// Add directory entry
	prefixed[topDir+"/"] = ""
	for path, content := range files {
		prefixed[topDir+"/"+path] = content
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for path, content := range prefixed {
		if strings.HasSuffix(path, "/") {
			hdr := &tar.Header{
				Name:     path,
				Mode:     0755,
				Typeflag: tar.TypeDir,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			continue
		}
		// Ensure parent dirs exist
		dir := filepath.Dir(path)
		if dir != "." && dir != topDir {
			dirHdr := &tar.Header{
				Name:     dir + "/",
				Mode:     0755,
				Typeflag: tar.TypeDir,
			}
			tw.WriteHeader(dirHdr) // ignore duplicate dir errors
		}
		hdr := &tar.Header{
			Name: path,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func TestPkgInstallFromGitHub(t *testing.T) {
	// Create a fake GitHub tarball
	tarball := makeTarGzWithDir(t, "my-lib-1.0.0", map[string]string{
		"clank.pkg":     "name = \"my-lib\"\nversion = \"1.0.0\"\n",
		"src/utils.clk": "mod my-lib.utils\n",
	})

	// Start test HTTP server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expect: /owner/repo/archive/refs/tags/v1.0.0.tar.gz
		if strings.Contains(r.URL.Path, "v1.0.0") {
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(tarball)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Override HTTP client to route to test server
	origClient := httpClient
	httpClient = srv.Client()
	defer func() { httpClient = origClient }()

	// Override fetchGitHubPackage to use test server URL
	origFetch := fetchGitHubFunc
	fetchGitHubFunc = func(slug, version string) (string, error) {
		url := srv.URL + "/" + slug + "/archive/refs/tags/v" + version + ".tar.gz"
		return fetchFromURL(url)
	}
	defer func() { fetchGitHubFunc = origFetch }()

	// Set up cache dir
	cacheDir := t.TempDir()
	globalCacheDirOverride = cacheDir
	defer func() { globalCacheDirOverride = "" }()

	// Create project with a GitHub dep
	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nmy-lib = { github = \"owner/repo\", version = \"1.0.0\" }\n"), 0644)

	result := PkgInstall(PkgInstallOptions{Dir: projDir})
	if !result.Ok {
		t.Fatalf("expected ok, got error: %s", result.Error)
	}
	if len(result.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(result.Installed))
	}
	if result.Installed[0].Name != "my-lib" {
		t.Errorf("name: %q", result.Installed[0].Name)
	}
	if result.Installed[0].Version != "1.0.0" {
		t.Errorf("version: %q", result.Installed[0].Version)
	}

	// Verify cache was populated
	cachePath := filepath.Join(cacheDir, "my-lib@1.0.0")
	if _, err := os.Stat(filepath.Join(cachePath, "clank.pkg")); os.IsNotExist(err) {
		t.Error("clank.pkg not found in cache")
	}
	if _, err := os.Stat(filepath.Join(cachePath, "src", "utils.clk")); os.IsNotExist(err) {
		t.Error("src/utils.clk not found in cache")
	}
}

func TestPkgInstallAlreadyCached(t *testing.T) {
	cacheDir := t.TempDir()
	globalCacheDirOverride = cacheDir
	defer func() { globalCacheDirOverride = "" }()

	// Pre-populate cache
	cachePath := filepath.Join(cacheDir, "my-lib@1.0.0")
	os.MkdirAll(cachePath, 0755)
	os.WriteFile(filepath.Join(cachePath, "clank.pkg"),
		[]byte("name = \"my-lib\"\nversion = \"1.0.0\"\n"), 0644)

	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nmy-lib = { github = \"owner/repo\", version = \"1.0.0\" }\n"), 0644)

	result := PkgInstall(PkgInstallOptions{Dir: projDir})
	if !result.Ok {
		t.Fatalf("expected ok, got error: %s", result.Error)
	}
	if len(result.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(result.Installed))
	}
	if result.Installed[0].Path != cachePath {
		t.Errorf("expected cached path %q, got %q", cachePath, result.Installed[0].Path)
	}
}

func TestPkgInstallNoManifest(t *testing.T) {
	dir := t.TempDir()
	result := PkgInstall(PkgInstallOptions{Dir: dir})
	if result.Ok {
		t.Error("expected failure")
	}
	if !strings.Contains(result.Error, "No clank.pkg") {
		t.Errorf("error: %q", result.Error)
	}
}

func TestPkgInstallNoGitHubDeps(t *testing.T) {
	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nlocal-lib = { path = \"../lib\" }\n"), 0644)

	result := PkgInstall(PkgInstallOptions{Dir: projDir})
	if !result.Ok {
		t.Fatalf("expected ok, got error: %s", result.Error)
	}
	if len(result.Installed) != 0 {
		t.Errorf("expected 0 installed, got %d", len(result.Installed))
	}
}

func TestPkgInstallSpecificName(t *testing.T) {
	cacheDir := t.TempDir()
	globalCacheDirOverride = cacheDir
	defer func() { globalCacheDirOverride = "" }()

	// Pre-populate cache for lib-a
	cachePath := filepath.Join(cacheDir, "lib-a@1.0.0")
	os.MkdirAll(cachePath, 0755)
	os.WriteFile(filepath.Join(cachePath, "clank.pkg"),
		[]byte("name = \"lib-a\"\nversion = \"1.0.0\"\n"), 0644)

	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n\n[deps]\nlib-a = { github = \"owner/a\", version = \"1.0.0\" }\nlib-b = { github = \"owner/b\", version = \"2.0.0\" }\n"), 0644)

	// Install only lib-a (which is cached)
	result := PkgInstall(PkgInstallOptions{Dir: projDir, Name: "lib-a"})
	if !result.Ok {
		t.Fatalf("expected ok, got error: %s", result.Error)
	}
	if len(result.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(result.Installed))
	}
	if result.Installed[0].Name != "lib-a" {
		t.Errorf("name: %q", result.Installed[0].Name)
	}
}

func TestPkgInstallNameNotFound(t *testing.T) {
	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "clank.pkg"),
		[]byte("name = \"app\"\nversion = \"1.0.0\"\n"), 0644)

	result := PkgInstall(PkgInstallOptions{Dir: projDir, Name: "nonexistent"})
	if result.Ok {
		t.Error("expected failure")
	}
	if !strings.Contains(result.Error, "nonexistent") {
		t.Errorf("error: %q", result.Error)
	}
}

func TestExtractTarGz(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{
		"hello.txt":     "hello world",
		"sub/nested.txt": "nested content",
	})

	destDir := t.TempDir()
	if err := extractTarGz(bytes.NewReader(tarball), destDir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Errorf("content: %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(destDir, "sub", "nested.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "nested content" {
		t.Errorf("nested content: %q", string(data))
	}
}

func TestExtractTarGzPathTraversal(t *testing.T) {
	// Create a tarball with a path-traversal entry
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: "../../../etc/evil",
		Mode: 0644,
		Size: 4,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("evil"))
	tw.Close()
	gw.Close()

	destDir := t.TempDir()
	if err := extractTarGz(bytes.NewReader(buf.Bytes()), destDir); err != nil {
		t.Fatal(err)
	}

	// Evil file should NOT exist outside destDir
	if _, err := os.Stat(filepath.Join(destDir, "..", "..", "..", "etc", "evil")); !os.IsNotExist(err) {
		t.Error("path traversal was not blocked")
	}
}
