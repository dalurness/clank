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

	// Copy source into cache
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

// ResolvedDep is a resolved local dependency with its discovered modules.
type ResolvedDep struct {
	Name     string
	Manifest *Manifest
	Path     string            // absolute path to the dependency root
	Modules  map[string]string // qualified module path -> absolute file path
}

// PackageResolution is the result of resolving all packages for a manifest.
type PackageResolution struct {
	Packages  []ResolvedDep
	ModuleMap map[string]string // qualified module path -> absolute file path
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

	modules := DiscoverModules(depPath, dep.Name)

	*resolved = append(*resolved, ResolvedDep{
		Name:     dep.Name,
		Manifest: depManifest,
		Path:     depPath,
		Modules:  modules,
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

// resolveCachedDep looks up a dependency in the global cache directory.
// It scans for entries matching <name>@<version> and picks the first match
// that satisfies the dependency's constraint.
func resolveCachedDep(dep Dependency, cacheDir string, resolved *[]ResolvedDep, resolvedNames map[string]bool) error {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}

	prefix := dep.Name + "@"
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		version := strings.TrimPrefix(entry.Name(), prefix)
		if !VersionSatisfies(version, dep.Constraint) {
			continue
		}

		depPath := filepath.Join(cacheDir, entry.Name())
		depManifestPath := filepath.Join(depPath, "clank.pkg")
		if _, err := os.Stat(depManifestPath); os.IsNotExist(err) {
			continue
		}
		depManifest, err := LoadManifest(depManifestPath)
		if err != nil {
			continue
		}
		modules := DiscoverModules(depPath, depManifest.Name)
		*resolved = append(*resolved, ResolvedDep{
			Name:     depManifest.Name,
			Manifest: depManifest,
			Path:     depPath,
			Modules:  modules,
		})
		resolvedNames[depManifest.Name] = true
		return nil
	}

	return fmt.Errorf("package %s not found in cache", dep.Name)
}

// ── Module discovery ──

// DiscoverModules finds all .clk files in a package's src/ directory.
func DiscoverModules(packageDir, packageName string) map[string]string {
	modules := make(map[string]string)
	srcDir := filepath.Join(packageDir, "src")
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return modules
	}

	walkModules(srcDir, "", packageName, modules)
	return modules
}

func walkModules(dir, prefix, packageName string, modules map[string]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			newPrefix := entry.Name()
			if prefix != "" {
				newPrefix = prefix + "." + entry.Name()
			}
			walkModules(fullPath, newPrefix, packageName, modules)
		} else if strings.HasSuffix(entry.Name(), ".clk") {
			modName := strings.TrimSuffix(entry.Name(), ".clk")
			modPath := modName
			if prefix != "" {
				modPath = prefix + "." + modName
			}
			qualifiedPath := packageName + "." + modPath
			modules[qualifiedPath] = fullPath
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

	// Also pick up installed deps from ~/.clank/cache/
	cacheDir := GlobalCacheDir()
	if cacheDir != "" {
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
			if err := resolveCachedDep(dep, cacheDir, &packages, resolvedNames); err != nil {
				continue // silently skip unresolvable cached deps
			}
		}
	}

	moduleMap := make(map[string]string)
	for _, p := range packages {
		for modPath, filePath := range p.Modules {
			if _, exists := moduleMap[modPath]; exists {
				return nil, &PkgError{"E509", fmt.Sprintf("Module path collision: '%s' is provided by multiple packages", modPath)}
			}
			moduleMap[modPath] = filePath
		}
	}

	return &PackageResolution{Packages: packages, ModuleMap: moduleMap}, nil
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
		Clank:    ClankVersion,
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
			content := fmt.Sprintf("mod %s.%s\n\nmain : () -> <> ()\nmain = fn () -> print(\"hello from %s\")\n", name, opts.Entry, name)
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
	Error      string
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

	if _, exists := targetMap[opts.Name]; exists {
		return PkgAddResult{Ok: false, Error: fmt.Sprintf("Dependency '%s' already exists in [%s]", opts.Name, section)}
	}

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
	Modules []string
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
		modules := make([]string, 0, len(p.Modules))
		for modPath := range p.Modules {
			modules = append(modules, modPath)
		}
		pkgs = append(pkgs, ResolvedPkgInfo{
			Name:    p.Name,
			Version: p.Manifest.Version,
			Path:    p.Path,
			Modules: modules,
		})
	}

	return PkgResolveResult{
		Ok:       true,
		Root:     filepath.Dir(manifestPath),
		Packages: pkgs,
	}
}
