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

	var toInstall []Dependency
	for _, dep := range sortedDeps(allDeps) {
		if dep.GitHub == "" {
			continue
		}
		if opts.Name != "" && dep.Name != opts.Name {
			continue
		}
		toInstall = append(toInstall, dep)
	}

	if opts.Name != "" && len(toInstall) == 0 {
		return PkgInstallResult{Ok: false, Error: fmt.Sprintf("No GitHub dependency '%s' found in manifest", opts.Name)}
	}

	if len(toInstall) == 0 {
		return PkgInstallResult{Ok: true, Installed: nil}
	}

	var installed []InstalledPkg
	for _, dep := range toInstall {
		version := dep.Constraint
		if version == "" || version == "*" {
			version = "latest"
		}

		// Check if already cached
		cachePath := CachedPackagePath(dep.Name, version)
		if cachePath != "" {
			if _, err := os.Stat(filepath.Join(cachePath, "clank.pkg")); err == nil {
				installed = append(installed, InstalledPkg{
					Name:    dep.Name,
					Version: version,
					GitHub:  dep.GitHub,
					Path:    cachePath,
				})
				continue
			}
		}

		// Fetch from GitHub
		tmpDir, err := fetchGitHubFunc(dep.GitHub, version)
		if err != nil {
			return PkgInstallResult{Ok: false, Error: fmt.Sprintf("Fetching %s: %s", dep.Name, err)}
		}
		defer os.RemoveAll(tmpDir)

		cachePath, err = InstallToCache(dep.Name, version, tmpDir)
		if err != nil {
			return PkgInstallResult{Ok: false, Error: fmt.Sprintf("Caching %s: %s", dep.Name, err)}
		}

		installed = append(installed, InstalledPkg{
			Name:    dep.Name,
			Version: version,
			GitHub:  dep.GitHub,
			Path:    cachePath,
		})
	}

	return PkgInstallResult{Ok: true, Installed: installed}
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
