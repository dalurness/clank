package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// globalCacheDirOverride allows tests to redirect the cache to a temp directory.
var globalCacheDirOverride string

// GlobalCacheDir returns the machine-level package cache directory (~/.clank/cache/).
func GlobalCacheDir() string {
	if globalCacheDirOverride != "" {
		return globalCacheDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".clank", "cache")
}

// CachedPackagePath returns the path for a specific package version in the global cache.
func CachedPackagePath(name, version string) string {
	cacheDir := GlobalCacheDir()
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, fmt.Sprintf("%s@%s", name, version))
}

// InstallToCache copies a package from sourcePath into the global cache.
// Returns the cache path. If the package is already cached, returns immediately.
func InstallToCache(name, version, sourcePath string) (string, error) {
	cachePath := CachedPackagePath(name, version)
	if cachePath == "" {
		return "", fmt.Errorf("cannot determine cache directory")
	}

	// Already cached — check for clank.pkg
	if _, err := os.Stat(filepath.Join(cachePath, "clank.pkg")); err == nil {
		return cachePath, nil
	}

	return copyToCache(cachePath, sourcePath)
}

// ReplaceInCache is InstallToCache without the already-cached short
// circuit: any existing entry for name@version is deleted first. Used by
// `pkg update` so a default branch that moved without a version bump
// still refreshes the cached copy.
func ReplaceInCache(name, version, sourcePath string) (string, error) {
	cachePath := CachedPackagePath(name, version)
	if cachePath == "" {
		return "", fmt.Errorf("cannot determine cache directory")
	}
	if err := os.RemoveAll(cachePath); err != nil {
		return "", fmt.Errorf("clearing cache entry: %w", err)
	}
	return copyToCache(cachePath, sourcePath)
}

func copyToCache(cachePath, sourcePath string) (string, error) {
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}
	if err := copyDir(sourcePath, cachePath); err != nil {
		os.RemoveAll(cachePath)
		return "", fmt.Errorf("copying to cache: %w", err)
	}
	return cachePath, nil
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolvedDep is a resolved dependency with the full list of its source
// files. The old per-module qualified-path map is gone: packages now expose
// a single flat namespace to consumers, so the loader only needs the list
// of .clk files that contribute to that namespace.
type ResolvedDep struct {
	Name     string
	Manifest *Manifest
	Path     string   // absolute path to the dependency root
	Files    []string // absolute paths to every .clk file under src/
}

// PackageResolution is the result of resolving all packages for a manifest.
// PackageFiles maps package name → absolute paths of every .clk file in
// that package's src/ tree. The loader uses this to route `use &pkg`
// imports: it loads every file in the list and merges their pub symbols
// into one qualifier namespace.
type PackageResolution struct {
	Packages     []ResolvedDep
	PackageFiles map[string][]string
}

// ── Find manifest ──

// FindManifest walks up the directory tree looking for a clank.pkg file.
func FindManifest(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, "clank.pkg")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// LoadManifest reads and parses a clank.pkg file.
func LoadManifest(manifestPath string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	return ParseManifest(string(data), manifestPath)
}

// ── Local dependency resolution ──

// ResolveLocalDeps resolves all local (path) dependencies from a manifest.
func ResolveLocalDeps(manifest *Manifest, manifestDir string, includeDev bool) ([]ResolvedDep, error) {
	var resolved []ResolvedDep
	visited := make(map[string]bool)

	depsToResolve := make(map[string]Dependency)
	for k, v := range manifest.Deps {
		depsToResolve[k] = v
	}
	if includeDev {
		for k, v := range manifest.DevDeps {
			depsToResolve[k] = v
		}
	}

	for _, dep := range sortedDeps(depsToResolve) {
		if dep.Path == "" {
			continue // skip registry deps
		}
		if err := resolveLocalDep(dep, manifestDir, &resolved, visited); err != nil {
			return nil, err
		}
	}

	return resolved, nil
}

func resolveLocalDep(dep Dependency, baseDir string, resolved *[]ResolvedDep, visited map[string]bool) error {
	if dep.Path == "" {
		return nil
	}

	depPath := filepath.Join(baseDir, dep.Path)
	depPath, _ = filepath.Abs(depPath)

	if visited[depPath] {
		return &PkgError{"E505", fmt.Sprintf("Circular dependency detected: %s at %s", dep.Name, depPath)}
	}
	visited[depPath] = true

	depManifestPath := filepath.Join(depPath, "clank.pkg")
	if _, err := os.Stat(depManifestPath); os.IsNotExist(err) {
		return &PkgError{"E502", fmt.Sprintf("Dependency '%s' not found: no clank.pkg at %s", dep.Name, depPath)}
	}

	depManifest, err := LoadManifest(depManifestPath)
	if err != nil {
		return err
	}

	if depManifest.Name != dep.Name {
		return &PkgError{"E508", fmt.Sprintf("Dependency name mismatch: expected '%s' but clank.pkg at %s declares '%s'", dep.Name, depPath, depManifest.Name)}
	}

	*resolved = append(*resolved, ResolvedDep{
		Name:     dep.Name,
		Manifest: depManifest,
		Path:     depPath,
		Files:    DiscoverPackageFiles(depPath),
	})

	// Recursively resolve transitive local deps
	for _, transitiveDep := range sortedDeps(depManifest.Deps) {
		if transitiveDep.Path != "" {
			if err := resolveLocalDep(transitiveDep, depPath, resolved, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// FindCachedPackage scans the global cache for entries matching
// <name>@<version> that satisfy the constraint and returns the best match:
// the highest semver wins; non-semver cache keys (like a branch snapshot)
// are used only when no semver entry qualifies. Returns ("", "") when
// nothing in the cache satisfies the constraint.
func FindCachedPackage(name, constraint string) (path, version string) {
	cacheDir := GlobalCacheDir()
	if cacheDir == "" {
		return "", ""
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return "", ""
	}

	prefix := name + "@"
	var bestVer *semver
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		v := strings.TrimPrefix(entry.Name(), prefix)
		if !VersionSatisfies(v, constraint) {
			continue
		}
		if _, err := os.Stat(filepath.Join(cacheDir, entry.Name(), "clank.pkg")); err != nil {
			continue
		}
		parsed := parseSemver(v)
		switch {
		case path == "":
			// first qualifying entry
		case parsed == nil:
			continue // never prefer a non-semver entry over an existing pick
		case bestVer != nil && compareSemver(*parsed, *bestVer) <= 0:
			continue
		}
		path = filepath.Join(cacheDir, entry.Name())
		version = v
		bestVer = parsed
	}
	return path, version
}

// resolveCachedDep looks up a dependency in the global cache directory,
// picking the highest cached version that satisfies the constraint.
func resolveCachedDep(dep Dependency, resolved *[]ResolvedDep, resolvedNames map[string]bool) error {
	depPath, _ := FindCachedPackage(dep.Name, dep.Constraint)
	if depPath == "" {
		return fmt.Errorf("package %s not found in cache", dep.Name)
	}
	depManifest, err := LoadManifest(filepath.Join(depPath, "clank.pkg"))
	if err != nil {
		return err
	}
	*resolved = append(*resolved, ResolvedDep{
		Name:     depManifest.Name,
		Manifest: depManifest,
		Path:     depPath,
		Files:    DiscoverPackageFiles(depPath),
	})
	resolvedNames[depManifest.Name] = true
	return nil
}

// ── Module discovery ──

// DiscoverPackageFiles returns the sorted list of .clk files under a
// package's src/ directory, recursively. Internal subdirectory structure
// is invisible to consumers — the whole set contributes to one flat
// package namespace. Returns an empty slice if there's no src/ dir.
func DiscoverPackageFiles(packageDir string) []string {
	srcDir := filepath.Join(packageDir, "src")
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return nil
	}
	var files []string
	walkPackageFiles(srcDir, &files)
	return files
}

func walkPackageFiles(dir string, out *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			walkPackageFiles(fullPath, out)
		} else if strings.HasSuffix(entry.Name(), ".clk") {
			*out = append(*out, fullPath)
		}
	}
}

// ── Package resolution ──

// ResolvePackages resolves all packages for a manifest, building a module map.
func ResolvePackages(manifestPath string, includeDev bool) (*PackageResolution, error) {
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	manifestDir := filepath.Dir(manifestPath)
	packages, err := ResolveLocalDeps(manifest, manifestDir, includeDev)
	if err != nil {
		return nil, err
	}

	// Also pick up installed deps from ~/.clank/cache/, walking the
	// dependency graph transitively: a github dep's own github deps are
	// resolved from the cache too, so consumers only declare what they
	// use directly.
	if GlobalCacheDir() != "" {
		resolvedNames := make(map[string]bool)
		for _, p := range packages {
			resolvedNames[p.Name] = true
		}

		// Collect all non-local deps from manifest
		allDeps := make(map[string]Dependency)
		for k, v := range manifest.Deps {
			allDeps[k] = v
		}
		if includeDev {
			for k, v := range manifest.DevDeps {
				allDeps[k] = v
			}
		}

		for _, dep := range sortedDeps(allDeps) {
			if dep.Path != "" || resolvedNames[dep.Name] {
				continue // skip local and already-resolved deps
			}
			// Check global cache for this dep
			if err := resolveCachedDep(dep, &packages, resolvedNames); err != nil {
				continue // silently skip unresolvable cached deps
			}
		}

		// Breadth-first over every resolved package's own github deps.
		for i := 0; i < len(packages); i++ {
			for _, dep := range sortedDeps(packages[i].Manifest.Deps) {
				if dep.GitHub == "" || resolvedNames[dep.Name] {
					continue
				}
				if err := resolveCachedDep(dep, &packages, resolvedNames); err != nil {
					continue
				}
			}
		}
	}

	// Build the package-name → file-list map the loader uses to resolve
	// `use &pkg` imports. Collisions on package name are impossible by
	// construction — PkgAdd rejects duplicate names at add time.
	pkgFiles := make(map[string][]string, len(packages))
	for _, p := range packages {
		pkgFiles[p.Name] = p.Files
	}

	return &PackageResolution{Packages: packages, PackageFiles: pkgFiles}, nil
}

// ── pkg init ──

// PkgInitOptions configures a pkg init operation.
type PkgInitOptions struct {
	Name  string
	Entry string
	Dir   string
}

// PkgInitResult is the result of a pkg init.
type PkgInitResult struct {
	Ok           bool
	Package      string
	CreatedFiles []string
	Error        string
}

// initialClankConstraint returns the value PkgInit should seed into a new
// manifest's `clank` field. A real semver (like "0.5.0") becomes a floor
// (">= 0.5.0"). A dev build is left empty — seeding a nonsense constraint
// like "dev" or ">= dev" would be worse than no constraint at all.
func initialClankConstraint(ver string) string {
	if parseSemver(ver) == nil {
		return ""
	}
	return ">= " + ver
}

// PkgInit initializes a new package directory.
func PkgInit(opts PkgInitOptions) PkgInitResult {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	dir, _ = filepath.Abs(dir)
	name := opts.Name
	if name == "" {
		name = filepath.Base(dir)
	}

	if !nameRe.MatchString(name) {
		return PkgInitResult{Ok: false, Error: fmt.Sprintf("Invalid package name '%s': must match [a-z][a-z0-9-]*", name)}
	}

	manifestPath := filepath.Join(dir, "clank.pkg")
	if _, err := os.Stat(manifestPath); err == nil {
		return PkgInitResult{Ok: false, Error: fmt.Sprintf("clank.pkg already exists at %s", manifestPath)}
	}

	m := &Manifest{
		Name:     name,
		Version:  "0.1.0",
		Clank:    initialClankConstraint(ClankVersion),
		Authors:  []string{},
		Keywords: []string{},
		Deps:     make(map[string]Dependency),
		DevDeps:  make(map[string]Dependency),
		Effects:  make(map[string]bool),
		Exports:  []string{},
	}

	if opts.Entry != "" {
		m.Entry = opts.Entry
	}

	var createdFiles []string

	if err := os.WriteFile(manifestPath, []byte(SerializeManifest(m)), 0644); err != nil {
		return PkgInitResult{Ok: false, Error: err.Error()}
	}
	createdFiles = append(createdFiles, "clank.pkg")

	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(srcDir, 0755)

	if opts.Entry != "" {
		entryFile := filepath.Join(srcDir, opts.Entry+".clk")
		if _, err := os.Stat(entryFile); os.IsNotExist(err) {
			content := fmt.Sprintf("mod %s\n\nmain : () -> <io> () =\n  print(\"hello from %s\")\n", opts.Entry, name)
			os.WriteFile(entryFile, []byte(content), 0644)
			createdFiles = append(createdFiles, "src/"+opts.Entry+".clk")
		}
	}

	return PkgInitResult{Ok: true, Package: name, CreatedFiles: createdFiles}
}

// ── pkg add ──

// PkgAddOptions configures a pkg add operation.
type PkgAddOptions struct {
	Name       string
	Constraint string
	Path       string
	GitHub     string
	Dev        bool
	Dir        string
}

// PkgAddResult is the result of a pkg add.
type PkgAddResult struct {
	Ok         bool
	Name       string
	Section    string
	Constraint string
	Path       string
	GitHub     string
	Updated    bool // true when an existing dep entry was replaced
	Error      string
}

// PkgAddFromTarget handles the user-facing `clank pkg add <target>` flow.
// The target is one positional string — a github slug/URL, a local path,
// or a github target with a version suffix. For github targets this
// function fetches the package eagerly, reads its own clank.pkg to get
// the authoritative package name, and writes the full github form to the
// consumer manifest. For local paths it loads the path dep's clank.pkg
// and uses its declared name as the dep key. No --name flag required;
// no surprises where the written form is an un-resolvable bare constraint.
func PkgAddFromTarget(target string, dev bool, dir string) PkgAddResult {
	if dir == "" {
		dir = "."
	}
	dir, _ = filepath.Abs(dir)
	manifestPath := FindManifest(dir)
	if manifestPath == "" {
		return PkgAddResult{Ok: false, Error: "No clank.pkg found in current directory or any parent"}
	}

	// Local path target — resolve manifest locally, no fetch.
	if strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || strings.HasPrefix(target, "/") {
		absDep := target
		if !filepath.IsAbs(absDep) {
			absDep = filepath.Join(filepath.Dir(manifestPath), target)
		}
		depManifestPath := filepath.Join(absDep, "clank.pkg")
		if _, err := os.Stat(depManifestPath); os.IsNotExist(err) {
			return PkgAddResult{Ok: false, Error: fmt.Sprintf("No clank.pkg found at %s", absDep)}
		}
		depManifest, err := LoadManifest(depManifestPath)
		if err != nil {
			return PkgAddResult{Ok: false, Error: err.Error()}
		}
		return PkgAdd(PkgAddOptions{
			Name:       depManifest.Name,
			Constraint: depManifest.Version,
			Path:       target,
			Dev:        dev,
			Dir:        dir,
		})
	}

	// GitHub target — normalize, fetch eagerly, read manifest for real name.
	if !LooksLikeGitHubTarget(target) {
		return PkgAddResult{Ok: false, Error: fmt.Sprintf("unrecognized target %q: expected github slug (user/repo), URL, or local path (./...)", target)}
	}
	slug, ref, err := NormalizeGitHubTarget(target)
	if err != nil {
		return PkgAddResult{Ok: false, Error: err.Error()}
	}
	// Normalize v-prefixed tags up front: the fetcher tries the
	// v-prefixed tag URL first, so "0.1.0" finds tag v0.1.0 directly.
	ref = strings.TrimPrefix(ref, "v")

	tmpDir, err := FetchGitHub(slug, ref)
	if err != nil {
		return PkgAddResult{Ok: false, Error: fmt.Sprintf("fetching %s: %s", slug, err)}
	}
	defer os.RemoveAll(tmpDir)

	fetchedManifestPath := filepath.Join(tmpDir, "clank.pkg")
	if _, err := os.Stat(fetchedManifestPath); os.IsNotExist(err) {
		return PkgAddResult{Ok: false, Error: fmt.Sprintf("fetched %s does not contain a clank.pkg at its root", slug)}
	}
	fetchedManifest, err := LoadManifest(fetchedManifestPath)
	if err != nil {
		return PkgAddResult{Ok: false, Error: fmt.Sprintf("parsing fetched clank.pkg: %s", err)}
	}

	// Cache the fetched package under the authoritative name + version.
	// Pinned refs are normalized (v1.2.3 → 1.2.3) so cache keys and
	// constraints compare as plain semver.
	cacheVersion := fetchedManifest.Version
	constraint := "*"
	if ref != "" && ref != "latest" {
		cacheVersion = strings.TrimPrefix(ref, "v")
		constraint = cacheVersion
	}
	if _, err := InstallToCache(fetchedManifest.Name, cacheVersion, tmpDir); err != nil {
		return PkgAddResult{Ok: false, Error: fmt.Sprintf("caching %s: %s", fetchedManifest.Name, err)}
	}

	// Unpinned deps record "*" — they track the repo's default branch.
	// A fresh `clank pkg install` fetches whatever the branch holds; the
	// lockfile records what was actually resolved. Pin with @<tag> for
	// an exact version.
	return PkgAdd(PkgAddOptions{
		Name:       fetchedManifest.Name,
		Constraint: constraint,
		GitHub:     slug,
		Dev:        dev,
		Dir:        dir,
	})
}

// PkgAdd adds a dependency to the manifest.
func PkgAdd(opts PkgAddOptions) PkgAddResult {
	startDir := opts.Dir
	if startDir == "" {
		startDir = "."
	}
	startDir, _ = filepath.Abs(startDir)
	manifestPath := FindManifest(startDir)

	if manifestPath == "" {
		return PkgAddResult{Ok: false, Error: "No clank.pkg found in current directory or any parent"}
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return PkgAddResult{Ok: false, Error: err.Error()}
	}

	section := "deps"
	targetMap := manifest.Deps
	if opts.Dev {
		section = "dev-deps"
		targetMap = manifest.DevDeps
	}

	// Re-adding an existing dep replaces its entry (go-get semantics):
	// `clank pkg add user/repo@v2` is how you change a pin.
	_, updated := targetMap[opts.Name]

	constraint := opts.Constraint
	if constraint == "" {
		constraint = "*"
	}

	dep := Dependency{
		Name:       opts.Name,
		Constraint: constraint,
		Path:       opts.Path,
		GitHub:     opts.GitHub,
	}

	// Validate path dep
	if dep.Path != "" {
		depDir := filepath.Join(filepath.Dir(manifestPath), dep.Path)
		depManifestPath := filepath.Join(depDir, "clank.pkg")
		if _, err := os.Stat(depManifestPath); os.IsNotExist(err) {
			return PkgAddResult{Ok: false, Error: fmt.Sprintf("No clank.pkg found at %s", depDir)}
		}
		depManifest, err := LoadManifest(depManifestPath)
		if err != nil {
			return PkgAddResult{Ok: false, Error: err.Error()}
		}
		if depManifest.Name != opts.Name {
			return PkgAddResult{Ok: false, Error: fmt.Sprintf("Package at %s declares name '%s', expected '%s'", dep.Path, depManifest.Name, opts.Name)}
		}
	}

	targetMap[opts.Name] = dep
	os.WriteFile(manifestPath, []byte(SerializeManifest(manifest)), 0644)

	// Try to write lockfile (non-fatal)
	WriteLockfile(manifestPath, opts.Dev)

	return PkgAddResult{
		Ok:         true,
		Name:       opts.Name,
		Section:    section,
		Constraint: dep.Constraint,
		Path:       dep.Path,
		GitHub:     dep.GitHub,
		Updated:    updated,
	}
}

// ── pkg remove ──

// PkgRemoveOptions configures a pkg remove operation.
type PkgRemoveOptions struct {
	Name string
	Dev  bool
	Dir  string
}

// PkgRemoveResult is the result of a pkg remove.
type PkgRemoveResult struct {
	Ok      bool
	Name    string
	Section string
	Error   string
}

// PkgRemove removes a dependency from the manifest.
func PkgRemove(opts PkgRemoveOptions) PkgRemoveResult {
	startDir := opts.Dir
	if startDir == "" {
		startDir = "."
	}
	startDir, _ = filepath.Abs(startDir)
	manifestPath := FindManifest(startDir)

	if manifestPath == "" {
		return PkgRemoveResult{Ok: false, Error: "No clank.pkg found in current directory or any parent"}
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return PkgRemoveResult{Ok: false, Error: err.Error()}
	}

	section := "deps"
	targetMap := manifest.Deps
	if opts.Dev {
		section = "dev-deps"
		targetMap = manifest.DevDeps
	}

	if _, exists := targetMap[opts.Name]; !exists {
		return PkgRemoveResult{Ok: false, Error: fmt.Sprintf("Dependency '%s' not found in [%s]", opts.Name, section)}
	}

	delete(targetMap, opts.Name)
	os.WriteFile(manifestPath, []byte(SerializeManifest(manifest)), 0644)

	return PkgRemoveResult{Ok: true, Name: opts.Name, Section: section}
}

// ── pkg resolve ──

// PkgResolveResult is the result of a pkg resolve.
type PkgResolveResult struct {
	Ok       bool
	Root     string
	Packages []ResolvedPkgInfo
	Error    string
}

// ResolvedPkgInfo is a resolved package summary.
type ResolvedPkgInfo struct {
	Name    string
	Version string
	Path    string
	Files   []string
}

// PkgResolve resolves all packages for the current project.
func PkgResolve(dir string) PkgResolveResult {
	if dir == "" {
		dir = "."
	}
	dir, _ = filepath.Abs(dir)
	manifestPath := FindManifest(dir)

	if manifestPath == "" {
		return PkgResolveResult{Ok: false, Error: "No clank.pkg found in current directory or any parent"}
	}

	resolution, err := ResolvePackages(manifestPath, false)
	if err != nil {
		if pkgErr, ok := err.(*PkgError); ok {
			return PkgResolveResult{Ok: false, Error: pkgErr.Message}
		}
		return PkgResolveResult{Ok: false, Error: err.Error()}
	}

	// Write lockfile on successful resolve
	WriteLockfile(manifestPath, false)

	var pkgs []ResolvedPkgInfo
	for _, p := range resolution.Packages {
		pkgs = append(pkgs, ResolvedPkgInfo{
			Name:    p.Name,
			Version: p.Manifest.Version,
			Path:    p.Path,
			Files:   p.Files,
		})
	}

	return PkgResolveResult{
		Ok:       true,
		Root:     filepath.Dir(manifestPath),
		Packages: pkgs,
	}
}
