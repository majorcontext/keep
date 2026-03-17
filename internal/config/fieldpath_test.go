package config

import (
	"testing"
)

func TestValidateFieldPath_Valid(t *testing.T) {
	valid := []string{
		"params.content",
		"params.input.command",
		"context.agent_id",
		"context.labels.sandbox_id",
	}
	for _, path := range valid {
		t.Run(path, func(t *testing.T) {
			if err := ValidateFieldPath(path); err != nil {
				t.Errorf("ValidateFieldPath(%q) = %v, want nil", path, err)
			}
		})
	}
}

func TestValidateFieldPath_Invalid(t *testing.T) {
	invalid := []string{
		"",
		".",
		".foo",
		"foo.",
		"foo..bar",
		"123",
	}
	for _, path := range invalid {
		t.Run(path, func(t *testing.T) {
			if err := ValidateFieldPath(path); err == nil {
				t.Errorf("ValidateFieldPath(%q) = nil, want error", path)
			}
		})
	}
}

func TestIsParamsPath(t *testing.T) {
	if !IsParamsPath("params.foo") {
		t.Errorf("IsParamsPath(%q) = false, want true", "params.foo")
	}
	if IsParamsPath("context.foo") {
		t.Errorf("IsParamsPath(%q) = true, want false", "context.foo")
	}
}
