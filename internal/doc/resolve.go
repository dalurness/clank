package doc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dalurness/clank/internal/pkg"
)

// Fetcher fetches a GitHub package and returns the absolute path to the
// extracted root. It is called only when network access is actually needed,
// giving callers a chance to prompt the user for confirmation first. A nil
// Fetcher disables all network access — any target that would require a
// fetch will return an error instead.
type Fetcher func(slug, version string) (string, error)

// ResolveResult describes the files that make up a resolved doc target.
type ResolveResult struct {
	Files []string // absolute .clk file paths
	Label string   // human-readable description of the resolved target
	Kind  string   // "project" | "path" | "github" | "dep" | "cache"
}

// ResolveTarget turns a user-supplied target string into a set of .clk
// files. baseDir is the working directory used to locate the current
// project's clank.pkg. See the plan in the repo for the full resolution
// rules — briefly: empty = current project, "/..." = project-relative
// path, "github.com/..." or "user/repo" = remote fetch, bare name =
// manifest dep then global cache.
func ResolveTarget(target, baseDir string, fetch Fetcher) (ResolveResult, error) {
	if target == "" {
		root := projectRoot(baseDir)
		files, err := WalkClkFiles(root)
		if err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{Files: files, Label: root, Kind: "project"}, nil
	}

	if abs, ok := pathTarget(target, baseDir); ok {
		return resolveLocalPath(abs)
	}

	if isGitHubTarget(target) {
		return resolveGitHub(target, fetch)
	}

	return resolveBareName(target, baseDir, fetch)
}

// pathTarget reports whether target looks like a filesystem path and, if so,
// returns its absolute form. Accepts:
//   - "/foo"    → project-root-relative (where project root = dir containing clank.pkg)
//   - "./foo"   → baseDir-relative
//   - "../foo"  → baseDir-relative
//   - absolute OS paths ("C:\foo", "C:/foo", "/home/x/foo")
//
// On Windows + git bash, MSYS rewrites a leading "/" into a Windows path
// before the binary sees it (e.g. "/cmd/main.go" → "C:/Program Files/Git/cmd/main.go").
// Treating any absolute path as a path target means users can still type "/..."
// from git bash and get sensible behavior — the fake prefix is stripped below
// via a filesystem check.
func pathTarget(target, baseDir string) (string, bool) {
	if strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") ||
		target == "." || target == ".." {
		abs, err := filepath.Abs(filepath.Join(baseDir, filepath.FromSlash(target)))
		if err != nil {
			return "", false
		}
		return abs, true
	}

	if strings.HasPrefix(target, "/") {
		root := projectRoot(baseDir)
		rel := strings.TrimPrefix(target, "/")
		return filepath.Join(root, filepath.FromSlash(rel)), true
	}

	if filepath.IsAbs(target) {
		if _, err := os.Stat(target); err == nil {
			return target, true
		}
		// git-bash MSYS rewrite: a leading "/x" becomes "<msys-root>/x". If the
		// trailing portion exists under projectRoot, assume that's what the user
		// meant.
		if mangled, ok := unmangleMSYS(target, baseDir); ok {
			return mangled, true
		}
	}
	return "", false
}

// unmangleMSYS attempts to recover a project-relative path from an MSYS-rewritten
// absolute path. It walks suffixes of the path, checking whether any of them
// exist under the project root.
func unmangleMSYS(target, baseDir string) (string, bool) {
	root := projectRoot(baseDir)
	// Normalize separators — MSYS typically emits forward slashes.
	norm := filepath.ToSlash(target)
	parts := strings.Split(norm, "/")
	for i := 1; i < len(parts); i++ {
		candidate := filepath.Join(root, filepath.Join(parts[i:]...))
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

func resolveLocalPath(abs string) (ResolveResult, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return ResolveResult{}, fmt.Errorf("path not found: %s", abs)
	}
	if info.IsDir() {
		files, err := WalkClkFiles(abs)
		if err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{Files: files, Label: abs, Kind: "path"}, nil
	}
	return ResolveResult{Files: []string{abs}, Label: abs, Kind: "path"}, nil
}

func resolveGitHub(target string, fetch Fetcher) (ResolveResult, error) {
	slug, ref := parseGitHubTarget(target)
	if fetch == nil {
		return ResolveResult{}, fmt.Errorf("refusing to fetch %s without confirmation (pass --yes)", slug)
	}
	root, err := fetch(slug, ref)
	if err != nil {
		return ResolveResult{}, err
	}
	files, err := WalkClkFiles(root)
	if err != nil {
		return ResolveResult{}, err
	}
	label := "github.com/" + slug
	if ref != "" && ref != "latest" {
		label += "@" + ref
	}
	return ResolveResult{Files: files, Label: label, Kind: "github"}, nil
}

func resolveBareName(target, baseDir string, fetch Fetcher) (ResolveResult, error) {
	name, version := splitNameVersion(target)

	if manifestPath := pkg.FindManifest(baseDir); manifestPath != "" {
		manifest, err := pkg.LoadManifest(manifestPath)
		if err == nil {
			if dep, ok := manifest.Deps[name]; ok {
				return resolveManifestDep(name, version, dep, manifestPath, fetch)
			}
		}
	}

	if res, ok := tryCache(name, version); ok {
		return res, nil
	}

	return ResolveResult{}, fmt.Errorf(
		"unknown library %q: not in clank.pkg and not in cache. "+
			"Pass a github slug (e.g. github.com/user/repo) to fetch it",
		name,
	)
}

func resolveManifestDep(name, version string, dep pkg.Dependency, manifestPath string, fetch Fetcher) (ResolveResult, error) {
	ver := version
	if ver == "" {
		ver = dep.Constraint
		if ver == "" || ver == "*" {
			ver = "latest"
		}
	}

	if dep.Path != "" {
		abs := dep.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(filepath.Dir(manifestPath), dep.Path)
		}
		files, err := WalkClkFiles(abs)
		if err != nil {
			return ResolveResult{}, err
		}
		return ResolveResult{Files: files, Label: name + " (path)", Kind: "dep"}, nil
	}

	if cached := pkg.CachedPackagePath(name, ver); cached != "" {
		if _, err := os.Stat(filepath.Join(cached, "clank.pkg")); err == nil {
			files, err := WalkClkFiles(cached)
			if err != nil {
				return ResolveResult{}, err
			}
			return ResolveResult{Files: files, Label: name + "@" + ver, Kind: "dep"}, nil
		}
	}

	if dep.GitHub == "" {
		return ResolveResult{}, fmt.Errorf("dependency %q has no source to fetch", name)
	}
	if fetch == nil {
		return ResolveResult{}, fmt.Errorf("refusing to fetch %s without confirmation (pass --yes)", name)
	}
	root, err := fetch(dep.GitHub, ver)
	if err != nil {
		return ResolveResult{}, err
	}
	files, err := WalkClkFiles(root)
	if err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{Files: files, Label: name + "@" + ver, Kind: "dep"}, nil
}

func tryCache(name, version string) (ResolveResult, bool) {
	if version != "" {
		cached := pkg.CachedPackagePath(name, version)
		if cached == "" {
			return ResolveResult{}, false
		}
		if _, err := os.Stat(filepath.Join(cached, "clank.pkg")); err != nil {
			return ResolveResult{}, false
		}
		files, err := WalkClkFiles(cached)
		if err != nil {
			return ResolveResult{}, false
		}
		return ResolveResult{Files: files, Label: name + "@" + version, Kind: "cache"}, true
	}

	hit, hitVer := findLatestCached(name)
	if hit == "" {
		return ResolveResult{}, false
	}
	files, err := WalkClkFiles(hit)
	if err != nil {
		return ResolveResult{}, false
	}
	return ResolveResult{Files: files, Label: name + "@" + hitVer, Kind: "cache"}, true
}

// projectRoot returns the directory containing clank.pkg above baseDir,
// or baseDir itself if no manifest is found above it.
func projectRoot(baseDir string) string {
	if baseDir == "" {
		baseDir = "."
	}
	if manifestPath := pkg.FindManifest(baseDir); manifestPath != "" {
		return filepath.Dir(manifestPath)
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return baseDir
	}
	return abs
}

// WalkClkFiles walks root recursively and returns absolute paths to every
// .clk file, sorted. It skips .git, .clank, test, and node_modules dirs.
func WalkClkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			switch d.Name() {
			case ".git", ".clank", "test", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".clk") {
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			files = append(files, abs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// isGitHubTarget reports whether target looks like a GitHub slug or URL.
// Accepts "github.com/user/repo", "https://github.com/user/repo", and bare
// "user/repo". Anything with more than one slash (besides the URL forms) is
// treated as a file path and rejected here.
func isGitHubTarget(target string) bool {
	if strings.HasPrefix(target, "github.com/") ||
		strings.HasPrefix(target, "https://github.com/") ||
		strings.HasPrefix(target, "http://github.com/") {
		return true
	}
	i := strings.Index(target, "/")
	if i <= 0 || i >= len(target)-1 {
		return false
	}
	rest := target[i+1:]
	if strings.Contains(rest, "/") {
		return false
	}
	if strings.HasSuffix(rest, ".clk") {
		return false
	}
	return true
}

// parseGitHubTarget normalizes a github target into (slug, ref). ref is
// empty when unspecified. An optional @ref suffix is stripped from the
// slug.
func parseGitHubTarget(target string) (slug, ref string) {
	target = strings.TrimPrefix(target, "https://")
	target = strings.TrimPrefix(target, "http://")
	target = strings.TrimPrefix(target, "github.com/")
	if i := strings.LastIndex(target, "@"); i > 0 {
		return target[:i], target[i+1:]
	}
	return target, ""
}

func splitNameVersion(target string) (name, version string) {
	if i := strings.Index(target, "@"); i > 0 {
		return target[:i], target[i+1:]
	}
	return target, ""
}

// findLatestCached returns the cache path and version of the most recently
// modified cached package matching name. Returns empty strings on miss.
func findLatestCached(name string) (string, string) {
	cacheDir := pkg.GlobalCacheDir()
	if cacheDir == "" {
		return "", ""
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return "", ""
	}
	prefix := name + "@"
	var bestPath, bestVer string
	var bestMod int64
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		full := filepath.Join(cacheDir, e.Name())
		if _, err := os.Stat(filepath.Join(full, "clank.pkg")); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mod := info.ModTime().Unix()
		if bestPath == "" || mod > bestMod {
			bestPath = full
			bestVer = strings.TrimPrefix(e.Name(), prefix)
			bestMod = mod
		}
	}
	return bestPath, bestVer
}
