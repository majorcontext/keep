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
	matched, err := path.Match(pattern, name)
	if err != nil {
		// Pattern is malformed. This should not happen since patterns are
		// validated at config load time. Treat as no-match.
		return false
	}
	return matched
}
