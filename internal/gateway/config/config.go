package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	keepconfig "github.com/majorcontext/keep/internal/config"
)

// GatewayConfig holds the top-level gateway configuration.
type GatewayConfig struct {
	Listen      string          `yaml:"listen"`
	RulesDir    string          `yaml:"rules_dir"`
	ProfilesDir string          `yaml:"profiles_dir,omitempty"`
	PacksDir    string          `yaml:"packs_dir,omitempty"`
	Provider    string          `yaml:"provider"`
	Upstream    string          `yaml:"upstream"`
	Scope       string          `yaml:"scope"`
	Decompose   DecomposeConfig `yaml:"decompose,omitempty"`
	Log         keepconfig.LogConfig `yaml:"log,omitempty"`
}

// DecomposeConfig controls which message parts are decomposed into separate spans.
type DecomposeConfig struct {
	ToolResult      *bool `yaml:"tool_result,omitempty"`
	ToolUse         *bool `yaml:"tool_use,omitempty"`
	Text            *bool `yaml:"text,omitempty"`
	RequestSummary  *bool `yaml:"request_summary,omitempty"`
	ResponseSummary *bool `yaml:"response_summary,omitempty"`
}

// ToolResultEnabled returns whether tool_result decomposition is enabled (default: true).
func (d DecomposeConfig) ToolResultEnabled() bool { return d.ToolResult == nil || *d.ToolResult }

// ToolUseEnabled returns whether tool_use decomposition is enabled (default: true).
func (d DecomposeConfig) ToolUseEnabled() bool { return d.ToolUse == nil || *d.ToolUse }

// TextEnabled returns whether text decomposition is enabled (default: false).
func (d DecomposeConfig) TextEnabled() bool { return d.Text != nil && *d.Text }

// RequestSummaryEnabled returns whether request_summary decomposition is enabled (default: true).
func (d DecomposeConfig) RequestSummaryEnabled() bool {
	return d.RequestSummary == nil || *d.RequestSummary
}

// ResponseSummaryEnabled returns whether response_summary decomposition is enabled (default: true).
func (d DecomposeConfig) ResponseSummaryEnabled() bool {
	return d.ResponseSummary == nil || *d.ResponseSummary
}

// validProviders is the set of accepted provider names.
// Only "anthropic" is implemented. OpenAI support is planned.
var validProviders = map[string]bool{
	"anthropic": true,
}

// Load reads and validates a gateway config from a YAML file.
func Load(path string) (*GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gateway config: %w", err)
	}
	var cfg GatewayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("gateway config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *GatewayConfig) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("gateway config: listen is required")
	}
	if c.RulesDir == "" {
		return fmt.Errorf("gateway config: rules_dir is required")
	}
	if c.Provider == "" {
		return fmt.Errorf("gateway config: provider is required")
	}
	if !validProviders[c.Provider] {
		return fmt.Errorf("gateway config: provider %q is not valid, must be: anthropic (openai support coming soon)", c.Provider)
	}
	if c.Upstream == "" {
		return fmt.Errorf("gateway config: upstream is required")
	}
	if c.Scope == "" {
		return fmt.Errorf("gateway config: scope is required")
	}
	return nil
}

func (c *GatewayConfig) applyDefaults() {
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}
	if c.Log.Output == "" {
		c.Log.Output = "stdout"
	}
}
