// content.go — text-analysis helpers for Keep rule expressions.
package cel

import (
	"strings"
)

// ContainsAnyFunc returns true if field contains any of the terms (case-insensitive).
func ContainsAnyFunc(field string, terms []string) bool {
	lower := strings.ToLower(field)
	for _, term := range terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

// EstimateTokensFunc returns a rough token count (len/4).
func EstimateTokensFunc(field string) int {
	return len(field) / 4
}
