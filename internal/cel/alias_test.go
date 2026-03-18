package cel

import (
	"testing"
)

func TestResolveAliases_Simple(t *testing.T) {
	aliases := map[string]string{
		"priority": "params.priority",
	}
	got := ResolveAliases("priority == 0", aliases)
	want := "params.priority == 0"
	if got != want {
		t.Errorf("ResolveAliases() = %q, want %q", got, want)
	}
}

func TestResolveAliases_Multiple(t *testing.T) {
	aliases := map[string]string{
		"priority": "params.priority",
		"team":     "params.team",
	}
	got := ResolveAliases("team in ['A'] && priority > 1", aliases)
	want := "params.team in ['A'] && params.priority > 1"
	if got != want {
		t.Errorf("ResolveAliases() = %q, want %q", got, want)
	}
}

func TestResolveAliases_NoProfile(t *testing.T) {
	expr := "priority == 0"

	// nil aliases
	got := ResolveAliases(expr, nil)
	if got != expr {
		t.Errorf("ResolveAliases(nil) = %q, want %q", got, expr)
	}

	// empty aliases
	got = ResolveAliases(expr, map[string]string{})
	if got != expr {
		t.Errorf("ResolveAliases(empty) = %q, want %q", got, expr)
	}
}

func TestResolveAliases_ExplicitParams(t *testing.T) {
	aliases := map[string]string{
		"priority": "params.priority",
	}
	expr := "params.priority == 0"
	got := ResolveAliases(expr, aliases)
	if got != expr {
		t.Errorf("ResolveAliases() = %q, want %q (should be unchanged)", got, expr)
	}
}

func TestResolveAliases_BuiltinNotReplaced(t *testing.T) {
	// "size" is not in aliases, so it should not be replaced.
	// Profile validation prevents adding builtins as aliases, but we test
	// that even if it were there, regular usage like size() is fine.
	aliases := map[string]string{
		"to": "params.to",
	}
	expr := "size(params.to) > 5"
	got := ResolveAliases(expr, aliases)
	// "to" in "params.to" is preceded by a dot and must NOT be replaced.
	if got != expr {
		t.Errorf("ResolveAliases() = %q, want %q", got, expr)
	}
}

func TestResolveAliases_NestedInFunction(t *testing.T) {
	aliases := map[string]string{
		"title": "params.title",
	}
	got := ResolveAliases("containsAny(title, ['a'])", aliases)
	want := "containsAny(params.title, ['a'])"
	if got != want {
		t.Errorf("ResolveAliases() = %q, want %q", got, want)
	}
}

func TestResolveAliases_DotAccessNotReplaced(t *testing.T) {
	aliases := map[string]string{
		"priority": "params.priority",
	}
	expr := "params.priority.toString()"
	got := ResolveAliases(expr, aliases)
	// "priority" after the dot in "params.priority" should NOT be replaced.
	if got != expr {
		t.Errorf("ResolveAliases() = %q, want %q (should be unchanged)", got, expr)
	}
}
