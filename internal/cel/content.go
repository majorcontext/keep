package cel

import (
	"regexp"
	"strings"
)

// Pre-compiled PII patterns.
var (
	reSSN        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	reCreditCard = regexp.MustCompile(`\b(?:4\d{12}(?:\d{3})?|5[1-5]\d{14}|3[47]\d{13}|6(?:011|5\d{2})\d{12})\b`)
	reUSPhone    = regexp.MustCompile(`\b(?:\(\d{3}\)\s*|\d{3}[-.])\d{3}[-.]?\d{4}\b`)
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

// ContainsPIIFunc returns true if field matches common PII patterns (SSN, credit card, US phone).
func ContainsPIIFunc(field string) bool {
	return reSSN.MatchString(field) ||
		reCreditCard.MatchString(field) ||
		reUSPhone.MatchString(field)
}

// ContainsPHIFunc is a stub — always returns false for M0.
func ContainsPHIFunc(field string) bool {
	return false
}

// EstimateTokensFunc returns a rough token count (len/4).
func EstimateTokensFunc(field string) int {
	return len(field) / 4
}
