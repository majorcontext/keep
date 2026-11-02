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
	"graphql",
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
	result := expr
	offset := 0

	for {
		start := findCallStart(result, fnName, offset)
		if start < 0 {
			break
		}

		// Find the matching closing paren, respecting nesting and string literals.
		depth := 1
		commaFound := false
		i := start
		for i < len(result) && depth > 0 {
			if end := skipCELLiteralOrComment(result, i); end > i {
				i = end
				continue
			}
			c := result[i]
			switch {
			case c == '(' || c == '[' || c == '{':
				depth++
			case c == ')' || c == ']' || c == '}':
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
			// Keep trailing newlines so a final line comment cannot swallow
			// the injected argument's closing parenthesis.
			trimmed := strings.TrimLeft(arg, " \t\r\n\f")
			if strings.HasPrefix(trimmed, "params.") || strings.HasPrefix(trimmed, "params[") {
				origArg := "_originalParams" + trimmed[len("params"):]
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

// findCallStart returns the position after the opening paren of the next global
// function call at or after from, or -1. Whitespace before '(' is allowed.
// Occurrences inside a string literal
// (`'graphql(x)'`) or as the tail of a longer identifier or a method call
// (`mygraphql(`, `x.graphql(`) are not call sites of the global function and
// are skipped, so the rewrite never corrupts a literal or an unrelated call.
func findCallStart(expr, fnName string, from int) int {
	var previous byte
	for i := 0; i < len(expr); i++ {
		if end := skipCELLiteralOrComment(expr, i); end > i {
			if expr[i] != '/' {
				previous = expr[i]
			}
			i = end - 1
			continue
		}
		c := expr[i]
		if isCELSpace(c) {
			continue
		}
		if i >= from && previous != '.' && (i == 0 || !isIdentPart(expr[i-1])) && strings.HasPrefix(expr[i:], fnName) {
			j := i + len(fnName)
			for j < len(expr) {
				if isCELSpace(expr[j]) {
					j++
				} else if strings.HasPrefix(expr[j:], "//") {
					j = skipCELLiteralOrComment(expr, j)
				} else {
					break
				}
			}
			if j < len(expr) && expr[j] == '(' {
				return j + 1
			}
		}
		previous = c
	}
	return -1
}

func isCELSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f'
}

// skipCELLiteralOrComment returns the position after a quoted string/bytes
// literal or line comment starting at i, or i otherwise. Raw literals don't
// process escapes; triple-quoted literals can contain individual quote marks.
func skipCELLiteralOrComment(expr string, i int) int {
	if strings.HasPrefix(expr[i:], "//") {
		if end := strings.IndexByte(expr[i:], '\n'); end >= 0 {
			return i + end
		}
		return len(expr)
	}
	quote := i
	if expr[quote] == 'b' || expr[quote] == 'B' {
		quote++
	}
	raw := quote < len(expr) && (expr[quote] == 'r' || expr[quote] == 'R')
	if raw {
		quote++
	}
	if quote >= len(expr) || (expr[quote] != '\'' && expr[quote] != '"') {
		return i
	}
	delimiter := expr[quote : quote+1]
	if strings.HasPrefix(expr[quote:], strings.Repeat(delimiter, 3)) {
		delimiter = strings.Repeat(delimiter, 3)
	}
	j := quote + len(delimiter)
	for j < len(expr) {
		if !raw && expr[j] == '\\' {
			j += 2
			continue
		}
		if strings.HasPrefix(expr[j:], delimiter) {
			return j + len(delimiter)
		}
		j++
	}
	return len(expr)
}

// PrepareWhen turns a rule's `when` expression into the source that is
// compiled: profile aliases and defs are substituted, then original-case
// params are injected for the functions that need them. The engine and
// rule-file validation both use it, so an expression validates exactly when
// it compiles at load time.
func PrepareWhen(expr string, aliases, defs map[string]string) string {
	resolved := ResolveAliases(expr, aliases)
	resolved = ResolveAliases(resolved, defs)
	return InjectOriginalParams(resolved)
}
