package keep_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/majorcontext/keep"
)

func FuzzValidateRuleBytes(f *testing.F) {
	// Seed with valid rule files from testdata.
	seedDirs := []string{
		"testdata/rules",
		"testdata/rules-with-defs",
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
	f.Add([]byte(nil))
	f.Add([]byte("{{{}}}"))
	f.Add([]byte("scope: fuzz\nmode: enforce\nrules:\n  - name: r\n    action: deny\n    match:\n      operation: \"*\"\n    message: \"blocked\"\n"))
	f.Add([]byte("scope: fuzz\nrules:\n  - name: cel-rule\n    match:\n      operation: \"foo\"\n      when: \"params.x == 1\"\n    action: deny\n    message: \"no\"\n"))
	f.Add([]byte("not yaml \x00\xff"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// ValidateRuleBytes must never panic, regardless of input.
		// Errors are expected and acceptable.
		_ = keep.ValidateRuleBytes(data)
	})
}
