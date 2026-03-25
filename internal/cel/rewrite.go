package cel

import "strings"

// originalParamsFunctions lists CEL functions that need access to
// pre-normalization params. The rewriter injects _originalParams as a
// second argument to single-argument calls of these functions.
//
// To register a new function:
//  1. Add its name here.
//  2. Add a binary overload in env.go that accepts (arg, dyn) and reads
//     the _originalParams map from the second argument.
//  3. Add a test case in rewrite_test.go.
var originalParamsFunctions = []string{
	"hasSecrets",
}

// InjectOriginalParams rewrites CEL expressions so that single-argument
// calls to any function in originalParamsFunctions get _originalParams
// injected as a second argument.
//
// Only single-argument calls are rewritten; calls that already have
// multiple arguments are left as-is. String literals inside the call are
// handled correctly (parentheses inside strings do not confuse the rewriter).
func InjectOriginalParams(expr string) string {
	for _, fn := range originalParamsFunctions {
		expr = injectOriginalParamsForFunc(expr, fn)
	}
	return expr
}


func injectOriginalParamsForFunc(expr string, fnName string) string {
	prefix := fnName + "("
	result := expr
	offset := 0

	for {
		idx := strings.Index(result[offset:], prefix)
		if idx < 0 {
			break
		}
		start := offset + idx + len(prefix) // position after opening paren

		// Find the matching closing paren, respecting nesting and string literals.
		depth := 1
		commaFound := false
		i := start
		for i < len(result) && depth > 0 {
			c := result[i]
			switch {
			case c == '\'' || c == '"':
				// Skip string literal.
				i++
				for i < len(result) {
					if result[i] == '\\' && i+1 < len(result) {
						i += 2
						continue
					}
					if result[i] == c {
						i++
						break
					}
					i++
				}
				continue
			case c == '(':
				depth++
			case c == ')':
				depth--
			case c == ',' && depth == 1:
				commaFound = true
			}
			i++
		}

		if depth != 0 {
			// Unbalanced parens — skip.
			offset = start
			continue
		}

		closePos := i - 1 // position of the closing ')'

		if !commaFound {
			// Single-argument call: inject a second arg for original-case lookup.
			// If the argument starts with "params.", replace the prefix with
			// "_originalParams." so the two-arg overload receives only the
			// specific field value (original case) rather than the full map.
			// For other expressions (e.g. lower(params.text)) fall back to
			// injecting the full _originalParams map.
			arg := result[start:closePos]
			var inject string
			if strings.HasPrefix(strings.TrimSpace(arg), "params.") {
				trimmed := strings.TrimSpace(arg)
				origArg := "_originalParams." + trimmed[len("params."):]
				inject = ", " + origArg
			} else {
				inject = ", _originalParams"
			}
			result = result[:closePos] + inject + result[closePos:]
			offset = closePos + len(inject) + 1
		} else {
			// Already has multiple arguments — skip.
			offset = closePos + 1
		}
	}

	return result
}
