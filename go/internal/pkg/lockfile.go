package pkg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ClankVersion is the current clank version used in lockfiles.
const ClankVersion = "0.2.0"

// LockPackage is a single package entry in the lockfile.
type LockPackage struct {
	Version   string            `json:"version"`
	Resolved  string            `json:"resolved"`
	Integrity string            `json:"integrity"`
	Deps      map[string]string `json:"deps"`
	Effects   []string          `json:"effects"`
}

// Lockfile represents a clank.lock file.
type Lockfile struct {
	LockVersion  int                    `json:"lock_version"`
	ClankVersion string                 `json:"clank_version"`
	ResolvedAt   string                 `json:"resolved_at"`
	Packages     map[string]LockPackage `json:"packages"`
}

// ── Integrity ──

func computeIntegrity(depPath string) string {
	h := sha256.New()
	manifestPath := filepath.Join(depPath, "clank.pkg")
	if data, err := os.ReadFile(manifestPath); err == nil {
		h.Write(data)
	}
	srcDir := filepath.Join(depPath, "src")
	if _, err := os.Stat(srcDir); err == nil {
		hashDir(srcDir, h)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func hashDir(dir string, h interface{ Write([]byte) (int, error) }) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	// Sort for deterministic output
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	sort.Strings(names)

	for _, name := range names {
		fullPath := filepath.Join(dir, name)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if info.IsDir() {
			hashDir(fullPath, h)
		} else {
			h.Write([]byte(name))
			if data, err := os.ReadFile(fullPath); err == nil {
				h.Write(data)
			}
		}
	}
}

// ── Serialize / Parse ──

// SerializeLockfile produces a deterministic JSON representation.
func SerializeLockfile(lock *Lockfile) string {
	// Sort packages by key
	sorted := make(map[string]LockPackage)
	keys := make([]string, 0, len(lock.Packages))
	for k := range lock.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sorted[k] = lock.Packages[k]
	}

	// We need ordered JSON, so build it manually
	out := orderedLockfileJSON(lock, keys)
	return out + "\n"
}

func orderedLockfileJSON(lock *Lockfile, sortedKeys []string) string {
	// Use encoding/json for the sorted output
	type lockfileJSON struct {
		LockVersion  int                    `json:"lock_version"`
		ClankVersion string                 `json:"clank_version"`
		ResolvedAt   string                 `json:"resolved_at"`
		Packages     map[string]LockPackage `json:"packages"`
	}
	lj := lockfileJSON{
		LockVersion:  lock.LockVersion,
		ClankVersion: lock.ClankVersion,
		ResolvedAt:   lock.ResolvedAt,
		Packages:     lock.Packages,
	}
	data, _ := json.MarshalIndent(lj, "", "  ")
	return string(data)
}

// ParseLockfile parses a clank.lock JSON file.
func ParseLockfile(source string) (*Lockfile, error) {
	var raw struct {
		LockVersion  int                    `json:"lock_version"`
		ClankVersion string                 `json:"clank_version"`
		ResolvedAt   string                 `json:"resolved_at"`
		Packages     map[string]LockPackage `json:"packages"`
	}
	if err := json.Unmarshal([]byte(source), &raw); err != nil {
		return nil, err
	}
	if raw.LockVersion == 0 {
		raw.LockVersion = 1
	}
	if raw.ClankVersion == "" {
		raw.ClankVersion = "0.1.0"
	}
	if raw.ResolvedAt == "" {
		raw.ResolvedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if raw.Packages == nil {
		raw.Packages = make(map[string]LockPackage)
	}
	return &Lockfile{
		LockVersion:  raw.LockVersion,
		ClankVersion: raw.ClankVersion,
		ResolvedAt:   raw.ResolvedAt,
		Packages:     raw.Packages,
	}, nil
}

// ── Generate lockfile ──

// GenerateLockfile creates a lockfile from the resolved dependencies.
func GenerateLockfile(manifestPath string, includeDev bool) (*Lockfile, error) {
	resolution, err := ResolvePackages(manifestPath, includeDev)
	if err != nil {
		return nil, err
	}
	return GenerateLockfileFromResolution(manifestPath, resolution)
}

// GenerateLockfileFromResolution creates a lockfile from an already-resolved package set.
func GenerateLockfileFromResolution(manifestPath string, resolution *PackageResolution) (*Lockfile, error) {
	manifestDir := filepath.Dir(manifestPath)
	packages := resolution.Packages

	cacheDir := GlobalCacheDir()
	lockPackages := make(map[string]LockPackage)
	for _, p := range packages {
		key := fmt.Sprintf("%s@%s", p.Name, p.Manifest.Version)
		deps := make(map[string]string)
		for _, dep := range sortedDeps(p.Manifest.Deps) {
			deps[dep.Name] = dep.Constraint
		}
		var effects []string
		for _, name := range sortedKeys(p.Manifest.Effects) {
			if p.Manifest.Effects[name] {
				effects = append(effects, name)
			}
		}

		// Determine resolved source: cache: if under global cache, path: otherwise
		resolved := ""
		if cacheDir != "" && strings.HasPrefix(p.Path, cacheDir) {
			resolved = "cache:" + filepath.Base(p.Path)
		} else {
			relPath, _ := filepath.Rel(manifestDir, p.Path)
			resolved = "path:" + relPath
		}

		lockPackages[key] = LockPackage{
			Version:   p.Manifest.Version,
			Resolved:  resolved,
			Integrity: computeIntegrity(p.Path),
			Deps:      deps,
			Effects:   effects,
		}
	}

	return &Lockfile{
		LockVersion:  1,
		ClankVersion: ClankVersion,
		ResolvedAt:   time.Now().UTC().Format(time.RFC3339),
		Packages:     lockPackages,
	}, nil
}

// WriteLockfile generates and writes a clank.lock file.
func WriteLockfile(manifestPath string, includeDev bool) (string, error) {
	lock, err := GenerateLockfile(manifestPath, includeDev)
	if err != nil {
		return "", err
	}
	lockPath := filepath.Join(filepath.Dir(manifestPath), "clank.lock")
	if err := os.WriteFile(lockPath, []byte(SerializeLockfile(lock)), 0644); err != nil {
		return "", err
	}
	return lockPath, nil
}

// ReadLockfile reads and parses a clank.lock file. Returns nil if not found.
func ReadLockfile(lockPath string) *Lockfile {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil
	}
	lock, err := ParseLockfile(string(data))
	if err != nil {
		return nil
	}
	return lock
}

// ── Verify lockfile ──

// VerifyResult describes whether the lockfile is up to date.
type VerifyResult struct {
	Ok      bool
	Stale   []string
	Missing []string
	Extra   []string
}

// VerifyLockfile checks if the lockfile is up to date with the manifest.
func VerifyLockfile(manifestPath string, includeDev bool) VerifyResult {
	manifestDir := filepath.Dir(manifestPath)
	lockPath := filepath.Join(manifestDir, "clank.lock")
	lock := ReadLockfile(lockPath)

	if lock == nil {
		return VerifyResult{Ok: false, Missing: []string{"clank.lock"}}
	}

	current, err := GenerateLockfile(manifestPath, includeDev)
	if err != nil {
		return VerifyResult{Ok: false, Missing: []string{"clank.lock"}}
	}

	lockMap := buildNameMap(lock.Packages)
	currentMap := buildNameMap(current.Packages)

	// GitHub deps in manifest
	manifest, _ := LoadManifest(manifestPath)
	expectedGithubDeps := make(map[string]bool)
	if manifest != nil {
		allDeps := make(map[string]Dependency)
		for k, v := range manifest.Deps {
			allDeps[k] = v
		}
		if includeDev {
			for k, v := range manifest.DevDeps {
				allDeps[k] = v
			}
		}
		for name, dep := range allDeps {
			if dep.GitHub != "" {
				expectedGithubDeps[name] = true
			}
		}
	}

	var stale, missing, extra []string

	for name, entry := range currentMap {
		locked, ok := lockMap[name]
		if !ok {
			missing = append(missing, name)
		} else if locked.pkg.Integrity != entry.pkg.Integrity || locked.pkg.Version != entry.pkg.Version {
			stale = append(stale, name)
		}
	}

	for name := range expectedGithubDeps {
		if _, ok := lockMap[name]; !ok {
			if _, ok := currentMap[name]; !ok {
				missing = append(missing, name)
			}
		}
	}

	for name := range lockMap {
		if _, ok := currentMap[name]; !ok {
			if !expectedGithubDeps[name] {
				extra = append(extra, name)
			}
		}
	}

	return VerifyResult{
		Ok:      len(stale) == 0 && len(missing) == 0 && len(extra) == 0,
		Stale:   stale,
		Missing: missing,
		Extra:   extra,
	}
}

type nameMapEntry struct {
	key string
	pkg LockPackage
}

func buildNameMap(packages map[string]LockPackage) map[string]nameMapEntry {
	m := make(map[string]nameMapEntry)
	for key, pkg := range packages {
		name := strings.SplitN(key, "@", 2)[0]
		m[name] = nameMapEntry{key: key, pkg: pkg}
	}
	return m
}
