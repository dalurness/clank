package pkg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

type semver struct {
	Major, Minor, Patch int
}

func parseSemver(v string) *semver {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return nil
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return &semver{major, minor, patch}
}

func compareSemver(a, b semver) int {
	if a.Major != b.Major {
		return a.Major - b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor - b.Minor
	}
	return a.Patch - b.Patch
}

// VersionSatisfies checks if a version satisfies a constraint.
func VersionSatisfies(version, constraint string) bool {
	if constraint == "*" {
		return true
	}

	ver := parseSemver(version)
	if ver == nil {
		return false
	}

	// Compound constraints: ">= 1.2, < 2.0"
	if strings.Contains(constraint, ",") {
		parts := strings.Split(constraint, ",")
		for _, p := range parts {
			if !VersionSatisfies(version, strings.TrimSpace(p)) {
				return false
			}
		}
		return true
	}

	// ">= X.Y.Z"
	if strings.HasPrefix(constraint, ">=") {
		target := parseSemver(strings.TrimSpace(constraint[2:]))
		if target == nil {
			return false
		}
		return compareSemver(*ver, *target) >= 0
	}

	// "< X.Y.Z"
	if strings.HasPrefix(constraint, "<") {
		target := parseSemver(strings.TrimSpace(constraint[1:]))
		if target == nil {
			return false
		}
		return compareSemver(*ver, *target) < 0
	}

	// Exact version: "1.2.3"
	exact := parseSemver(constraint)
	if exact != nil {
		matched, _ := regexp.MatchString(`^\d+\.\d+\.\d+$`, constraint)
		if matched {
			return compareSemver(*ver, *exact) == 0
		}
	}

	// Compatible range: "1.2" means >= 1.2.0, < 2.0.0
	parts := strings.Split(constraint, ".")
	if len(parts) == 2 {
		major, err1 := strconv.Atoi(parts[0])
		minor, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return false
		}
		return ver.Major == major && ver.Minor >= minor
	}

	// Major only: "1" means >= 1.0.0, < 2.0.0
	if len(parts) == 1 {
		major, err := strconv.Atoi(parts[0])
		if err != nil {
			return false
		}
		return ver.Major == major
	}

	return false
}

// SatisfiesOrDev reports whether version satisfies constraint, treating a
// "dev" version as satisfying any constraint and an empty constraint as
// satisfied by any version. It's the check the CLI uses when deciding
// whether to show a version-mismatch hint on parse or type errors — dev
// builds are assumed to have everything, and packages that declare no
// floor never warn.
func SatisfiesOrDev(version, constraint string) bool {
	if version == "dev" || constraint == "" {
		return true
	}
	return VersionSatisfies(version, constraint)
}

// SelectVersion selects the minimum version satisfying a constraint (MVS).
func SelectVersion(versions []string, constraint string) string {
	for _, v := range versions {
		if VersionSatisfies(v, constraint) {
			return v
		}
	}
	return ""
}

// MergeConstraints merges multiple version constraints using MVS (maximum of lower bounds).
func MergeConstraints(constraints []string) string {
	if len(constraints) == 0 {
		return "*"
	}
	if len(constraints) == 1 {
		return constraints[0]
	}

	var effective []string
	for _, c := range constraints {
		if c != "*" {
			effective = append(effective, c)
		}
	}
	if len(effective) == 0 {
		return "*"
	}
	if len(effective) == 1 {
		return effective[0]
	}

	var maxLower *semver
	var upperBound *semver

	for _, c := range effective {
		lower, upper := extractBounds(c)
		if lower != nil {
			if maxLower == nil || compareSemver(*lower, *maxLower) > 0 {
				maxLower = lower
			}
		}
		if upper != nil {
			if upperBound == nil || compareSemver(*upper, *upperBound) < 0 {
				upperBound = upper
			}
		}
	}

	if maxLower == nil {
		return effective[0]
	}

	if upperBound != nil {
		return fmt.Sprintf(">= %d.%d.%d, < %d.%d.%d",
			maxLower.Major, maxLower.Minor, maxLower.Patch,
			upperBound.Major, upperBound.Minor, upperBound.Patch)
	}

	return fmt.Sprintf(">= %d.%d.%d", maxLower.Major, maxLower.Minor, maxLower.Patch)
}

func extractBounds(constraint string) (lower, upper *semver) {
	// Handle compound constraints
	if strings.Contains(constraint, ",") {
		for _, part := range strings.Split(constraint, ",") {
			l, u := extractBounds(strings.TrimSpace(part))
			if l != nil {
				lower = l
			}
			if u != nil {
				upper = u
			}
		}
		return
	}

	if strings.HasPrefix(constraint, ">=") {
		lower = parseSemver(strings.TrimSpace(constraint[2:]))
		return
	}
	if strings.HasPrefix(constraint, "<") {
		upper = parseSemver(strings.TrimSpace(constraint[1:]))
		return
	}

	// Compatible range "1.2" → lower=1.2.0, upper=2.0.0
	parts := strings.Split(constraint, ".")
	if len(parts) == 2 {
		major, err1 := strconv.Atoi(parts[0])
		minor, err2 := strconv.Atoi(parts[1])
		if err1 == nil && err2 == nil {
			lower = &semver{major, minor, 0}
			upper = &semver{major + 1, 0, 0}
			return
		}
	}

	// Exact version
	exact := parseSemver(constraint)
	if exact != nil {
		lower = exact
		return
	}

	return
}
