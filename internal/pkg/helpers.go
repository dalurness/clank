package pkg

import "sort"

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedDeps returns dependencies sorted by name.
func sortedDeps(m map[string]Dependency) []Dependency {
	keys := sortedKeys(m)
	deps := make([]Dependency, len(keys))
	for i, k := range keys {
		deps[i] = m[k]
	}
	return deps
}
