package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ParseRuleFile parses and validates a single rule file from raw YAML bytes.
// It does not resolve pack references — callers that need pack resolution
// should use LoadAll instead.
func ParseRuleFile(data []byte) (*RuleFile, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("parse rule file: empty input")
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("parse rule file: input size %d exceeds maximum of %d bytes", len(data), maxFileBytes)
	}

	var rf RuleFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse rule file: invalid YAML: %w", err)
	}

	if err := Validate(&rf); err != nil {
		return nil, fmt.Errorf("parse rule file: %w", err)
	}

	if len(rf.Packs) > 0 {
		return nil, fmt.Errorf("parse rule file: pack references are not supported when loading from bytes (scope %q references %d packs)", rf.Scope, len(rf.Packs))
	}

	return &rf, nil
}
