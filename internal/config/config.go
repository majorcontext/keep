// Package config parses and validates Keep rule files, profiles, and starter packs.
package config

// RuleFile is the parsed representation of a single YAML rule file.
type RuleFile struct {
	Scope   string            `yaml:"scope"`
	Profile string            `yaml:"profile,omitempty"`
	Mode    Mode              `yaml:"mode,omitempty"`
	OnError ErrorMode         `yaml:"on_error,omitempty"`
	Defs    map[string]string `yaml:"defs,omitempty"`
	Packs   []PackRef         `yaml:"packs,omitempty"`
	Rules   []Rule            `yaml:"rules"`
}

// Mode controls whether rules are enforced or only audited.
type Mode string

const (
	ModeEnforce   Mode = "enforce"
	ModeAuditOnly Mode = "audit_only"
)

// ErrorMode controls behavior when a CEL expression errors at eval time.
type ErrorMode string

const (
	ErrorModeClosed ErrorMode = "closed"
	ErrorModeOpen   ErrorMode = "open"
)

// Rule is an atomic unit of policy.
type Rule struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description,omitempty"`
	Match       Match       `yaml:"match,omitempty"`
	Action      Action      `yaml:"action"`
	Message     string      `yaml:"message,omitempty"`
	Redact      *RedactSpec `yaml:"redact,omitempty"`
}

// Match determines when a rule applies.
type Match struct {
	Operation string `yaml:"operation,omitempty"`
	When      string `yaml:"when,omitempty"`
}

// Action is what to do when a rule matches.
type Action string

const (
	ActionDeny   Action = "deny"
	ActionLog    Action = "log"
	ActionRedact Action = "redact"
)

// RedactSpec defines what to redact and how.
type RedactSpec struct {
	Target   string          `yaml:"target"`
	Patterns []RedactPattern `yaml:"patterns"`
}

// RedactPattern is a regex pattern and its replacement.
type RedactPattern struct {
	Match   string `yaml:"match"`
	Replace string `yaml:"replace"`
}

// PackRef references a starter pack with optional overrides.
type PackRef struct {
	Name      string                 `yaml:"name"`
	Overrides map[string]interface{} `yaml:"overrides,omitempty"`
}

// Profile maps short alias names to parameter field paths.
type Profile struct {
	Name    string            `yaml:"name"`
	Aliases map[string]string `yaml:"aliases"`
}

// StarterPack is a reusable set of rules.
type StarterPack struct {
	Name    string `yaml:"name"`
	Profile string `yaml:"profile,omitempty"`
	Rules   []Rule `yaml:"rules"`
}
