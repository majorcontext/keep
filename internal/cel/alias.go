package cel

import "strings"

// ResolveAliases rewrites a CEL expression string, replacing unqualified
// identifiers that match alias names with their params.* paths.
// Returns the expression unchanged if aliases is nil or empty.
//
// An identifier is considered unqualified when it is NOT immediately preceded
// by a dot (field-access operator). String literals (single- or double-quoted)
// are left untouched.
func ResolveAliases(expr string, aliases map[string]string) string {
	if len(aliases) == 0 {
		return expr
	}

	var b strings.Builder
	b.Grow(len(expr))

	i := 0
	n := len(expr)

	// prevDot tracks whether the character immediately before the current
	// identifier token was a '.' (possibly with no whitespace in between,
	// since CEL field access has no spaces around the dot).
	// We track the last non-whitespace, non-identifier character we wrote.
	prevNonIdent := byte(0) // zero means "start of expression"

	for i < n {
		c := expr[i]

		// Handle single-quoted string literals.
		if c == '\'' {
			b.WriteByte(c)
			i++
			for i < n {
				ch := expr[i]
				b.WriteByte(ch)
				i++
				if ch == '\'' {
					break
				}
				// simple escape: skip next char
				if ch == '\\' && i < n {
					b.WriteByte(expr[i])
					i++
				}
			}
			prevNonIdent = '\''
			continue
		}

		// Handle double-quoted string literals.
		if c == '"' {
			b.WriteByte(c)
			i++
			for i < n {
				ch := expr[i]
				b.WriteByte(ch)
				i++
				if ch == '"' {
					break
				}
				if ch == '\\' && i < n {
					b.WriteByte(expr[i])
					i++
				}
			}
			prevNonIdent = '"'
			continue
		}

		// Collect an identifier: starts with a letter or underscore,
		// continues with letters, digits, or underscores.
		if isIdentStart(c) {
			start := i
			for i < n && isIdentPart(expr[i]) {
				i++
			}
			ident := expr[start:i]

			if prevNonIdent == '.' {
				// Field access — do not replace.
				b.WriteString(ident)
			} else if replacement, ok := aliases[ident]; ok {
				b.WriteString(replacement)
			} else {
				b.WriteString(ident)
			}
			prevNonIdent = expr[i-1] // last char of identifier
			continue
		}

		// For all other characters, write them out and update prevNonIdent
		// if they are not whitespace.
		b.WriteByte(c)
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			prevNonIdent = c
		}
		i++
	}

	return b.String()
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
