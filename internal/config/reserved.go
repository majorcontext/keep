package config

// ReservedNames is the unified set of identifiers that must not be used as
// def names or profile alias names because they shadow CEL built-in variables,
// operators, or Keep-specific functions.
var ReservedNames = map[string]bool{
	// Top-level variables
	"params":  true,
	"context": true,
	"now":     true,

	// CEL standard functions / macros
	"size":       true,
	"has":        true,
	"matches":    true,
	"startsWith": true,
	"endsWith":   true,
	"contains":   true,
	"exists":     true,
	"all":        true,
	"filter":     true,
	"exists_one": true,

	// Keep custom functions
	"containsAny":    true,
	"estimateTokens": true,
	"inTimeWindow":   true,
	"rateCount":      true,
	"lower":          true,
	"upper":          true,
	"matchesDomain":  true,
	"dayOfWeek":      true,
	"hasSecrets":     true,

	// CEL type identifiers
	"int":       true,
	"uint":      true,
	"double":    true,
	"bool":      true,
	"string":    true,
	"bytes":     true,
	"list":      true,
	"map":       true,
	"type":      true,
	"null_type": true,

	// Literal keywords
	"true":  true,
	"false": true,
	"null":  true,
}
