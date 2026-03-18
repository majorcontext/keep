package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// RelayConfig holds the top-level relay configuration.
type RelayConfig struct {
	Listen      string    `yaml:"listen"`
	RulesDir    string    `yaml:"rules_dir"`
	ProfilesDir string    `yaml:"profiles_dir,omitempty"`
	PacksDir    string    `yaml:"packs_dir,omitempty"`
	Routes      []Route   `yaml:"routes"`
	Log         LogConfig `yaml:"log,omitempty"`
}

// Route maps a scope to an upstream MCP endpoint with optional auth.
type Route struct {
	Scope    string `yaml:"scope"`
	Upstream string `yaml:"upstream"`
	Auth     *Auth  `yaml:"auth,omitempty"`
}

// Auth describes how to authenticate requests to an upstream.
type Auth struct {
	Type     string `yaml:"type"`
	TokenEnv string `yaml:"token_env,omitempty"`
	Header   string `yaml:"header,omitempty"`
}

// LogConfig controls log format and output destination.
type LogConfig struct {
	Format string `yaml:"format,omitempty"`
	Output string `yaml:"output,omitempty"`
}

// Load reads and validates a relay config from a YAML file.
func Load(path string) (*RelayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("relay config: %w", err)
	}
	var cfg RelayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("relay config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *RelayConfig) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("relay config: listen is required")
	}
	if c.RulesDir == "" {
		return fmt.Errorf("relay config: rules_dir is required")
	}
	if c.Routes == nil {
		return fmt.Errorf("relay config: routes is required")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("relay config: routes must not be empty")
	}
	for i, r := range c.Routes {
		if r.Scope == "" {
			return fmt.Errorf("relay config: routes[%d]: scope is required", i)
		}
		if r.Upstream == "" {
			return fmt.Errorf("relay config: routes[%d]: upstream is required", i)
		}
	}
	return nil
}

func (c *RelayConfig) applyDefaults() {
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}
	if c.Log.Output == "" {
		c.Log.Output = "stdout"
	}
}
