// Package engine implements Keep's core policy evaluation.
package engine

import "path"

// GlobMatch returns true if the operation name matches the glob pattern.
// Supports * (any sequence) and ? (any single character).
// An empty pattern matches everything.
func GlobMatch(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	matched, _ := path.Match(pattern, name)
	return matched
}
