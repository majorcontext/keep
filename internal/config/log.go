package config

// LogConfig controls log format and output destination.
// Used by both relay and gateway configurations.
type LogConfig struct {
	Format string `yaml:"format,omitempty"`
	Output string `yaml:"output,omitempty"`
}
