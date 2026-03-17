package config

import (
	"testing"
)

// --- LoadPacks tests ---

func TestLoadPacks_Valid(t *testing.T) {
	packs, err := LoadPacks("testdata/packs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pack, ok := packs["linear-safe-defaults"]
	if !ok {
		t.Fatalf("expected pack %q not found; got keys: %v", "linear-safe-defaults", mapKeys(packs))
	}
	if len(pack.Rules) != 3 {
		t.Errorf("got %d rules, want 3", len(pack.Rules))
	}
}

func TestLoadPacks_EmptyDir(t *testing.T) {
	packs, err := LoadPacks("testdata/packs-empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 0 {
		t.Errorf("got %d packs, want 0", len(packs))
	}
}

func TestLoadPacks_NonexistentDir(t *testing.T) {
	packs, err := LoadPacks("testdata/does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 0 {
		t.Errorf("got %d packs, want 0", len(packs))
	}
}

// --- ResolvePacks tests ---

func TestResolvePacks_NoOverrides(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "pack-rule", Match: Match{Operation: "foo"}, Action: ActionDeny, Message: "blocked"},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{{Name: "test-pack"}},
		Rules: []Rule{
			{Name: "inline-rule", Match: Match{Operation: "bar"}, Action: ActionLog},
		},
	}

	resolved, err := ResolvePacks(rf, packs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("got %d rules, want 2", len(resolved))
	}
	if resolved[0].Name != "pack-rule" {
		t.Errorf("first rule = %q, want pack-rule", resolved[0].Name)
	}
	if resolved[1].Name != "inline-rule" {
		t.Errorf("second rule = %q, want inline-rule", resolved[1].Name)
	}
}

func TestResolvePacks_Disabled(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "rule-a", Match: Match{Operation: "foo"}, Action: ActionDeny},
			{Name: "rule-b", Match: Match{Operation: "bar"}, Action: ActionLog},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"rule-a": "disabled",
				},
			},
		},
		Rules: []Rule{},
	}

	resolved, err := ResolvePacks(rf, packs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d rules, want 1", len(resolved))
	}
	if resolved[0].Name != "rule-b" {
		t.Errorf("remaining rule = %q, want rule-b", resolved[0].Name)
	}
}

func TestResolvePacks_OverrideWhen(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "my-rule", Match: Match{Operation: "create", When: "params.x == 1"}, Action: ActionDeny},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"my-rule": map[string]interface{}{
						"when": "params.x == 2",
					},
				},
			},
		},
	}

	resolved, err := ResolvePacks(rf, packs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d rules, want 1", len(resolved))
	}
	if resolved[0].Match.When != "params.x == 2" {
		t.Errorf("when = %q, want %q", resolved[0].Match.When, "params.x == 2")
	}
	// operation should be unchanged
	if resolved[0].Match.Operation != "create" {
		t.Errorf("operation = %q, want %q", resolved[0].Match.Operation, "create")
	}
}

func TestResolvePacks_OverrideMessage(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "my-rule", Match: Match{Operation: "foo"}, Action: ActionDeny, Message: "original"},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"my-rule": map[string]interface{}{
						"message": "overridden",
					},
				},
			},
		},
	}

	resolved, err := ResolvePacks(rf, packs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d rules, want 1", len(resolved))
	}
	if resolved[0].Message != "overridden" {
		t.Errorf("message = %q, want %q", resolved[0].Message, "overridden")
	}
}

func TestResolvePacks_OverrideAction(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "my-rule", Match: Match{Operation: "foo"}, Action: ActionDeny},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"my-rule": map[string]interface{}{
						"action": "log",
					},
				},
			},
		},
	}

	resolved, err := ResolvePacks(rf, packs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d rules, want 1", len(resolved))
	}
	if resolved[0].Action != ActionLog {
		t.Errorf("action = %q, want %q", resolved[0].Action, ActionLog)
	}
}

func TestResolvePacks_UnknownOverrideTarget(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "real-rule", Match: Match{Operation: "foo"}, Action: ActionDeny},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"nonexistent-rule": "disabled",
				},
			},
		},
	}

	_, err := ResolvePacks(rf, packs)
	if err == nil {
		t.Fatal("expected error for unknown override target, got nil")
	}
}

func TestResolvePacks_UnknownPackRef(t *testing.T) {
	packs := map[string]*StarterPack{}

	rf := &RuleFile{
		Packs: []PackRef{
			{Name: "nonexistent-pack"},
		},
	}

	_, err := ResolvePacks(rf, packs)
	if err == nil {
		t.Fatal("expected error for unknown pack ref, got nil")
	}
}

func TestResolvePacks_InvalidOverrideAction(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "my-rule", Match: Match{Operation: "foo"}, Action: ActionDeny},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"my-rule": map[string]interface{}{
						"action": "block",
					},
				},
			},
		},
	}

	_, err := ResolvePacks(rf, packs)
	if err == nil {
		t.Fatal("expected error for invalid override action, got nil")
	}
}

func TestResolvePacks_CannotOverrideName(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "my-rule", Match: Match{Operation: "foo"}, Action: ActionDeny},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"my-rule": map[string]interface{}{
						"name": "new-name",
					},
				},
			},
		},
	}

	_, err := ResolvePacks(rf, packs)
	if err == nil {
		t.Fatal("expected error for overriding name, got nil")
	}
}

func TestResolvePacks_CannotOverrideOperation(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "my-rule", Match: Match{Operation: "foo"}, Action: ActionDeny},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"my-rule": map[string]interface{}{
						"operation": "bar",
					},
				},
			},
		},
	}

	_, err := ResolvePacks(rf, packs)
	if err == nil {
		t.Fatal("expected error for overriding operation, got nil")
	}
}

func TestResolvePacks_InvalidOverrideString(t *testing.T) {
	pack := &StarterPack{
		Name: "test-pack",
		Rules: []Rule{
			{Name: "my-rule", Match: Match{Operation: "foo"}, Action: ActionDeny},
		},
	}
	packs := map[string]*StarterPack{"test-pack": pack}

	rf := &RuleFile{
		Packs: []PackRef{
			{
				Name: "test-pack",
				Overrides: map[string]interface{}{
					"my-rule": "enabled",
				},
			},
		},
	}

	_, err := ResolvePacks(rf, packs)
	if err == nil {
		t.Fatal("expected error for invalid string override value, got nil")
	}
}

// mapKeys is a helper to print map keys for diagnostics.
func mapKeys(m map[string]*StarterPack) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
