// Package redact handles regex-based field redaction for Keep's redact action.
package redact

import (
	"regexp"
	"strings"

	"github.com/majorcontext/keep/internal/config"
)

// Mutation describes a single field change.
type Mutation struct {
	Path     string
	Original string
	Replaced string
}

// CompiledPattern is a pre-compiled redact pattern.
type CompiledPattern struct {
	Regex   *regexp.Regexp
	Replace string
}

// CompilePatterns compiles a list of config redact patterns into CompiledPatterns.
func CompilePatterns(patterns []config.RedactPattern) ([]CompiledPattern, error) {
	compiled := make([]CompiledPattern, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p.Match)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, CompiledPattern{
			Regex:   re,
			Replace: p.Replace,
		})
	}
	return compiled, nil
}

// Apply runs compiled patterns against the string value at the given field path
// in params. Returns the list of mutations (empty if no patterns matched).
// Does not modify params.
//
// The target path uses the form "params.field" or "params.nested.field".
// The "params." prefix is stripped before navigating the map.
func Apply(params map[string]any, target string, patterns []CompiledPattern) []Mutation {
	keys := pathKeys(target)
	if len(keys) == 0 {
		return nil
	}

	// Navigate to the value.
	current := map[string]any(params)
	for i, key := range keys {
		v, ok := current[key]
		if !ok {
			return nil
		}
		if i == len(keys)-1 {
			// Final key: must be a string.
			str, ok := v.(string)
			if !ok {
				return nil
			}
			// Apply patterns sequentially.
			mutated := str
			for _, cp := range patterns {
				mutated = cp.Regex.ReplaceAllString(mutated, cp.Replace)
			}
			if mutated == str {
				// No change — no mutations.
				return nil
			}
			return []Mutation{
				{
					Path:     target,
					Original: str,
					Replaced: mutated,
				},
			}
		}
		// Intermediate key: must be a nested map.
		nested, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		current = nested
	}
	return nil
}

// ApplyMutations returns a new params map with mutations applied.
// The original map is not modified. Deep-copies the map structure.
func ApplyMutations(params map[string]any, mutations []Mutation) map[string]any {
	result := deepCopyMap(params)
	for _, m := range mutations {
		keys := pathKeys(m.Path)
		if len(keys) == 0 {
			continue
		}
		setNestedValue(result, keys, m.Replaced)
	}
	return result
}

// pathKeys strips the leading "params." prefix from target and splits on ".".
func pathKeys(target string) []string {
	t := strings.TrimPrefix(target, "params.")
	if t == "" || t == target {
		// Either empty after strip, or no "params." prefix existed; treat as-is
		// but only if something remains.
		if t == "" {
			return nil
		}
	}
	return strings.Split(t, ".")
}

// deepCopyMap recursively copies a map[string]any.
func deepCopyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if nested, ok := v.(map[string]any); ok {
			dst[k] = deepCopyMap(nested)
		} else {
			dst[k] = v
		}
	}
	return dst
}

// setNestedValue sets the value at the given key path inside m.
// If any intermediate key is missing or not a map, the operation is skipped.
func setNestedValue(m map[string]any, keys []string, value string) {
	current := m
	for i, key := range keys {
		if i == len(keys)-1 {
			// Only set if the key currently exists.
			if _, ok := current[key]; !ok {
				return
			}
			current[key] = value
			return
		}
		v, ok := current[key]
		if !ok {
			return
		}
		nested, ok := v.(map[string]any)
		if !ok {
			return
		}
		current = nested
	}
}
