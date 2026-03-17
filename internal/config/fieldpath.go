package config

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateFieldPath checks that a dot-separated path is syntactically valid.
// Each segment must be a valid identifier (letter or underscore followed by
// alphanumeric/underscores).
func ValidateFieldPath(path string) error {
	if path == "" {
		return fmt.Errorf("field path must not be empty")
	}
	segments := strings.Split(path, ".")
	for _, seg := range segments {
		if err := validateIdentifier(seg); err != nil {
			return fmt.Errorf("field path %q: segment %q: %w", path, seg, err)
		}
	}
	return nil
}

// IsParamsPath returns true if the path starts with "params.".
func IsParamsPath(path string) bool {
	return strings.HasPrefix(path, "params.")
}

func validateIdentifier(s string) error {
	if s == "" {
		return fmt.Errorf("segment must not be empty")
	}
	runes := []rune(s)
	first := runes[0]
	if first != '_' && !unicode.IsLetter(first) {
		return fmt.Errorf("must start with a letter or underscore, got %q", first)
	}
	for _, r := range runes[1:] {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return fmt.Errorf("invalid character %q", r)
		}
	}
	return nil
}
