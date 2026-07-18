package pkg

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// PkgInstallOptions configures a pkg install operation.
type PkgInstallOptions struct {
	Name string // install a specific dep by name (empty = all GitHub deps)
	Dev  bool   // include dev-deps
	Dir  string // working directory (defaults to ".")
}

// PkgInstallResult is the result of a pkg install.
type PkgInstallResult struct {
	Ok        bool
	Installed []InstalledPkg
	Error     string
}

// InstalledPkg describes a single installed package.
type InstalledPkg struct {
	Name    string
	Version string
	GitHub  string
	Path    string // cache path
}

// httpClient is the HTTP client used for fetching. Tests can override this.
var httpClient = http.DefaultClient

// fetchGitHubFunc is the function used to fetch GitHub packages. Tests can override this.
var fetchGitHubFunc = fetchGitHubPackage

// PkgInstall fetches GitHub dependencies and populates the global cache.
func PkgInstall(opts PkgInstallOptions) PkgInstallResult {
	startDir := opts.Dir
	if startDir == "" {
		startDir = "."
	}
	startDir, _ = filepath.Abs(startDir)
	manifestPath := FindManifest(startDir)

	if manifestPath == "" {
		return PkgInstallResult{Ok: false, Error: "No clank.pkg found in current directory or any parent"}
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return PkgInstallResult{Ok: false, Error: err.Error()}
	}

	// Collect GitHub deps to install
	allDeps := make(map[string]Dependency)
	for k, v := range manifest.Deps {
		allDeps[k] = v
	}
	if opts.Dev {
		for k, v := range manifest.DevDeps {
			allDeps[k] = v
		}
	}

	var queue []Dependency
	for _, dep := range sortedDeps(allDeps) {
		if dep.GitHub == "" {
			continue
		}
		if opts.Name != "" && dep.Name != opts.Name {
			continue
		}
		queue = append(queue, dep)
	}

	if opts.Name != "" && len(queue) == 0 {
		return PkgInstallResult{Ok: false, Error: fmt.Sprintf("No GitHub dependency '%s' found in manifest", opts.Name)}
	}

	// Walk github deps breadth-first: after a dep is satisfied (from
	// cache or fetched), its own manifest's github deps are queued so
	// transitive dependencies land in the cache too.
	var installed []InstalledPkg
	seen := make(map[string]bool)
	for len(queue) > 0 {
		dep := queue[0]
		queue = queue[1:]
		if seen[dep.Name] {
			continue
		}
		seen[dep.Name] = true

		cachePath, err := ensureCached(dep)
		if err != nil {
			return PkgInstallResult{Ok: false, Error: err.Error()}
		}

		depManifest, err := LoadManifest(filepath.Join(cachePath, "clank.pkg"))
		if err != nil {
			return PkgInstallResult{Ok: false, Error: fmt.Sprintf("Reading manifest of %s: %s", dep.Name, err)}
		}
		if msg := fetchedPathDepError(dep.Name, depManifest); msg != "" {
			return PkgInstallResult{Ok: false, Error: msg}
		}
		installed = append(installed, InstalledPkg{
			Name:    dep.Name,
			Version: depManifest.Version,
			GitHub:  dep.GitHub,
			Path:    cachePath,
		})
		for _, sub := range sortedDeps(depManifest.Deps) {
			if sub.GitHub != "" && !seen[sub.Name] {
				queue = append(queue, sub)
			}
		}
	}

	return PkgInstallResult{Ok: true, Installed: installed}
}

// fetchedPathDepError returns a non-empty error message when a fetched
// package's manifest declares a path dependency. Paths only resolve on
// the author's machine, so such a package can never work for consumers —
// fail at install/add time instead of surfacing a confusing "package not
// found" error later at link time. Dev-deps are exempt: they are not
// installed transitively.
func fetchedPathDepError(pkgName string, m *Manifest) string {
	for _, sub := range sortedDeps(m.Deps) {
		if sub.Path != "" {
			return fmt.Sprintf(
				"%s@%s declares a path dependency %s = { path = %q }, which only resolves on its author's machine.\n"+
					"The package cannot be used from a fetch; its author should publish '%s' and depend on it with\n"+
					"  %s = { github = \"<user>/<repo>\", version = \"...\" }",
				pkgName, m.Version, sub.Name, sub.Path, sub.Name, sub.Name)
		}
	}
	return ""
}

// ensureCached makes sure a github dependency is present in the global
// cache, fetching it if necessary, and returns its cache path.
//
// Unpinned deps (constraint "" or "*") reuse any cached version — installs
// stay deterministic and offline-friendly once fetched; `clank pkg update`
// is the explicit way to move to a newer snapshot. Pinned deps fetch the
// exact tag when it isn't cached yet.
func ensureCached(dep Dependency) (string, error) {
	if path, _ := FindCachedPackage(dep.Name, dep.Constraint); path != "" {
		return path, nil
	}

	version := dep.Constraint
	if version == "" || version == "*" {
		version = "latest"
	}
	tmpDir, err := fetchGitHubFunc(dep.GitHub, version)
	if err != nil {
		return "", fmt.Errorf("Fetching %s: %s", dep.Name, err)
	}
	defer os.RemoveAll(tmpDir)

	// Cache under the real version from the fetched manifest so a
	// default-branch fetch isn't keyed as a meaningless "latest".
	cacheVersion := version
	if fetched, err := LoadManifest(filepath.Join(tmpDir, "clank.pkg")); err == nil && fetched.Version != "" {
		if version == "latest" {
			cacheVersion = fetched.Version
		}
	} else if err != nil {
		return "", fmt.Errorf("Fetched %s has no valid clank.pkg: %s", dep.GitHub, err)
	}

	cachePath, err := InstallToCache(dep.Name, cacheVersion, tmpDir)
	if err != nil {
		return "", fmt.Errorf("Caching %s: %s", dep.Name, err)
	}
	return cachePath, nil
}

// ── pkg update ──

// PkgUpdateOptions configures a pkg update operation.
type PkgUpdateOptions struct {
	Name string // update a specific dep (empty = all unpinned GitHub deps)
	Dev  bool   // include dev-deps
	Dir  string
}

// UpdatedPkg describes one dependency examined by PkgUpdate.
type UpdatedPkg struct {
	Name       string
	GitHub     string
	OldVersion string // highest version previously cached ("" if none)
	NewVersion string
	Changed    bool
}

// PkgUpdateResult is the result of a pkg update.
type PkgUpdateResult struct {
	Ok      bool
	Updated []UpdatedPkg
	Skipped []string // pinned deps left alone (change pins with pkg add repo@ref)
	Error   string
}

// PkgUpdate re-fetches unpinned GitHub dependencies from their default
// branch and refreshes the cache. Pinned deps (exact version constraint)
// are reported as skipped — re-pin with `clank pkg add user/repo@<ref>`.
func PkgUpdate(opts PkgUpdateOptions) PkgUpdateResult {
	startDir := opts.Dir
	if startDir == "" {
		startDir = "."
	}
	startDir, _ = filepath.Abs(startDir)
	manifestPath := FindManifest(startDir)
	if manifestPath == "" {
		return PkgUpdateResult{Ok: false, Error: "No clank.pkg found in current directory or any parent"}
	}
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return PkgUpdateResult{Ok: false, Error: err.Error()}
	}

	allDeps := make(map[string]Dependency)
	for k, v := range manifest.Deps {
		allDeps[k] = v
	}
	if opts.Dev {
		for k, v := range manifest.DevDeps {
			allDeps[k] = v
		}
	}

	var targets []Dependency
	var skipped []string
	for _, dep := range sortedDeps(allDeps) {
		if dep.GitHub == "" {
			continue
		}
		if opts.Name != "" && dep.Name != opts.Name {
			continue
		}
		if dep.Constraint != "" && dep.Constraint != "*" {
			skipped = append(skipped, dep.Name)
			continue
		}
		targets = append(targets, dep)
	}
	if opts.Name != "" && len(targets) == 0 && len(skipped) == 0 {
		return PkgUpdateResult{Ok: false, Error: fmt.Sprintf("No GitHub dependency '%s' found in manifest", opts.Name)}
	}

	var updated []UpdatedPkg
	for _, dep := range targets {
		_, oldVersion := FindCachedPackage(dep.Name, "*")

		tmpDir, err := fetchGitHubFunc(dep.GitHub, "latest")
		if err != nil {
			return PkgUpdateResult{Ok: false, Error: fmt.Sprintf("Fetching %s: %s", dep.Name, err), Updated: updated, Skipped: skipped}
		}

		fetched, err := LoadManifest(filepath.Join(tmpDir, "clank.pkg"))
		if err != nil {
			os.RemoveAll(tmpDir)
			return PkgUpdateResult{Ok: false, Error: fmt.Sprintf("Fetched %s has no valid clank.pkg: %s", dep.GitHub, err), Updated: updated, Skipped: skipped}
		}
		if _, err := ReplaceInCache(dep.Name, fetched.Version, tmpDir); err != nil {
			os.RemoveAll(tmpDir)
			return PkgUpdateResult{Ok: false, Error: fmt.Sprintf("Caching %s: %s", dep.Name, err), Updated: updated, Skipped: skipped}
		}
		os.RemoveAll(tmpDir)

		updated = append(updated, UpdatedPkg{
			Name:       dep.Name,
			GitHub:     dep.GitHub,
			OldVersion: oldVersion,
			NewVersion: fetched.Version,
			Changed:    oldVersion != fetched.Version,
		})
	}

	// Refresh the lockfile so the new resolution is recorded.
	WriteLockfile(manifestPath, opts.Dev)

	return PkgUpdateResult{Ok: true, Updated: updated, Skipped: skipped}
}

// FetchGitHub downloads a GitHub repo tarball into a temp directory and
// returns the path to the extracted root. Version may be empty or "latest"
// to fetch the default branch. The caller owns the returned directory and
// is responsible for cleanup via os.RemoveAll when done.
func FetchGitHub(slug, version string) (string, error) {
	if version == "" {
		version = "latest"
	}
	return fetchGitHubFunc(slug, version)
}

// NormalizeGitHubTarget accepts any of the common GitHub identifier forms
// and returns a canonical (slug, ref) pair. Supported inputs:
//
//	user/repo                    → ("user/repo", "")
//	user/repo@v1.2.3             → ("user/repo", "v1.2.3")
//	github.com/user/repo         → ("user/repo", "")
//	https://github.com/user/repo → ("user/repo", "")
//	https://github.com/user/repo.git
//	git@github.com:user/repo.git
//
// Returns an error for inputs that don't look like github targets. Local
// paths ("./foo") and non-github URLs should be detected by the caller
// before calling this function.
func NormalizeGitHubTarget(raw string) (slug, ref string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", fmt.Errorf("empty target")
	}

	// git@github.com:user/repo(.git)
	if strings.HasPrefix(s, "git@github.com:") {
		s = strings.TrimPrefix(s, "git@github.com:")
	} else {
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		s = strings.TrimPrefix(s, "github.com/")
	}
	s = strings.TrimSuffix(s, ".git")

	// Split off optional @ref suffix.
	if i := strings.LastIndex(s, "@"); i > 0 {
		ref = s[i+1:]
		s = s[:i]
	}

	// Must be user/repo with exactly one slash and both halves non-empty.
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("not a github target: %q (expected user/repo)", raw)
	}
	return s, ref, nil
}

// LooksLikeGitHubTarget reports whether a raw target string looks like a
// github identifier in any of the supported forms. Used to decide whether
// to fetch or treat as a local path.
func LooksLikeGitHubTarget(raw string) bool {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "github.com/") ||
		strings.HasPrefix(s, "https://github.com/") ||
		strings.HasPrefix(s, "http://github.com/") ||
		strings.HasPrefix(s, "git@github.com:") {
		return true
	}
	// bare user/repo: exactly one slash, no leading dot or slash
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "/") {
		return false
	}
	// strip optional @ref for the check
	if i := strings.LastIndex(s, "@"); i > 0 {
		s = s[:i]
	}
	parts := strings.Split(s, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[0], ".")
}

// fetchGitHubPackage downloads and extracts a GitHub repo tarball into a temp directory.
// Returns the path to the extracted package root (the single directory inside the tarball).
func fetchGitHubPackage(slug, version string) (string, error) {
	// GitHub archive URL: https://github.com/{owner}/{repo}/archive/refs/tags/{version}.tar.gz
	// For "latest" we use the default branch archive
	var url string
	if version == "latest" {
		url = fmt.Sprintf("https://github.com/%s/archive/refs/heads/main.tar.gz", slug)
	} else {
		url = fmt.Sprintf("https://github.com/%s/archive/refs/tags/v%s.tar.gz", slug, version)
	}

	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// If tag with v-prefix fails, try without v-prefix
	if resp.StatusCode == 404 && version != "latest" {
		resp.Body.Close()
		url = fmt.Sprintf("https://github.com/%s/archive/refs/tags/%s.tar.gz", slug, version)
		resp, err = httpClient.Get(url)
		if err != nil {
			return "", fmt.Errorf("HTTP request failed: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub returned status %d for %s", resp.StatusCode, url)
	}

	tmpDir, err := os.MkdirTemp("", "clank-pkg-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	if err := extractTarGz(resp.Body, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("extracting archive: %w", err)
	}

	// GitHub tarballs contain a single top-level directory (e.g., repo-version/)
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("reading extracted dir: %w", err)
	}

	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(tmpDir, entries[0].Name()), nil
	}

	return tmpDir, nil
}

// fetchFromURL downloads and extracts a tarball from a specific URL.
// Used by tests to redirect fetches to a local test server.
func fetchFromURL(url string) (string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	tmpDir, err := os.MkdirTemp("", "clank-pkg-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	if err := extractTarGz(resp.Body, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("extracting archive: %w", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("reading extracted dir: %w", err)
	}

	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(tmpDir, entries[0].Name()), nil
	}
	return tmpDir, nil
}

// extractTarGz extracts a .tar.gz stream into destDir.
func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Sanitize path to prevent directory traversal
		cleanName := filepath.Clean(header.Name)
		if strings.Contains(cleanName, "..") {
			continue
		}

		target := filepath.Join(destDir, cleanName)

		// Ensure target is within destDir
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			// Limit file size to 50MB to prevent resource exhaustion
			const maxFileSize = 50 << 20
			if header.Size > maxFileSize {
				continue
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode&0755|0644))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxFileSize)); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}
