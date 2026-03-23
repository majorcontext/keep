// content.go — text-analysis helpers for Keep rule expressions.
package cel

import (
	"strings"
	"unicode/utf8"
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

// EstimateTokensFunc returns a rough token count (chars/4).
func EstimateTokensFunc(field string) int {
	return utf8.RuneCountInString(field) / 4
}
