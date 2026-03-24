package cel

import "strings"

// RewriteHasSecrets transforms hasSecrets(expr) calls into
// hasSecrets(expr, _originalParams) so that the binary overload receives
// the pre-normalization params map for case-sensitive secret detection.
//
// Only single-argument calls are rewritten; two-argument calls are left as-is.
// String literals inside the call are handled correctly (parentheses inside
// strings do not confuse the rewriter).
func RewriteHasSecrets(expr string) string {
	const fn = "hasSecrets("
	result := expr
	offset := 0

	for {
		idx := strings.Index(result[offset:], fn)
		if idx < 0 {
			break
		}
		start := offset + idx + len(fn) // position after opening paren

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
			// Single-argument call: inject _originalParams.
			inject := ", _originalParams"
			result = result[:closePos] + inject + result[closePos:]
			offset = closePos + len(inject) + 1
		} else {
			// Already has multiple arguments — skip.
			offset = closePos + 1
		}
	}

	return result
}
