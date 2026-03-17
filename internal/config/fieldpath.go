package config

import (
	"fmt"
	"strings"
)

// ValidateFieldPath checks that a dot-separated path is syntactically valid.
// Each segment must be a valid identifier (ASCII letter or underscore followed
// by ASCII alphanumeric/underscores). ASCII-only identifiers are required to
// prevent Unicode homoglyph confusion.
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
	first := rune(s[0])
	if first != '_' && !isASCIILetter(first) {
		return fmt.Errorf("must start with a letter or underscore, got %q", first)
	}
	for _, r := range s[1:] {
		if r != '_' && !isASCIILetter(r) && !isASCIIDigit(r) {
			return fmt.Errorf("invalid character %q", r)
		}
	}
	return nil
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
