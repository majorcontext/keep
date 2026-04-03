package config

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseRuleFile(f *testing.F) {
	// Seed with valid rule files from testdata.
	seedDirs := []string{
		"testdata/rules",
		"testdata/rules-with-defs",
		"testdata/rules-duplicate-scope",
	}
	for _, dir := range seedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			ext := filepath.Ext(entry.Name())
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			f.Add(data)
		}
	}

	// Additional hand-crafted seeds.
	f.Add([]byte(""))
	f.Add([]byte("{{{}}}"))
	f.Add([]byte("scope: fuzz\nrules:\n  - name: r\n    action: deny\n    match:\n      operation: \"*\"\n"))
	f.Add([]byte("not yaml at all \x00\xff\xfe"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// ParseRuleFile must never panic, regardless of input.
		// Errors are expected and acceptable.
		_, _ = ParseRuleFile(data)
	})
}
