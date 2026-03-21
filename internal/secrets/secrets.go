package secrets

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zricethezav/gitleaks/v8/detect"
)

// Detector wraps the gitleaks detector for secret detection.
type Detector struct {
	d *detect.Detector
}

// Finding represents a detected secret.
type Finding struct {
	RuleID      string // e.g. "aws-access-key"
	Description string // human-readable description from gitleaks
	Match       string // the matched text
}

// NewDetector creates a Detector with gitleaks default config (~160 patterns).
func NewDetector() (*Detector, error) {
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("secrets: init gitleaks: %w", err)
	}
	return &Detector{d: d}, nil
}

// Detect scans text and returns all secret findings.
// Safe to call on nil receiver (returns nil).
func (d *Detector) Detect(text string) []Finding {
	if d == nil {
		return nil
	}
	gitleaksFindings := d.d.DetectString(text)
	if len(gitleaksFindings) == 0 {
		return nil
	}
	findings := make([]Finding, len(gitleaksFindings))
	for i, f := range gitleaksFindings {
		findings[i] = Finding{
			RuleID:      f.RuleID,
			Description: f.Description,
			Match:       f.Match,
		}
	}
	return findings
}

// Redact scans text, replaces each detected secret with [REDACTED:<RuleID>],
// and returns the redacted string plus the findings.
// Safe to call on nil receiver (returns original text, nil).
func (d *Detector) Redact(text string) (string, []Finding) {
	if d == nil {
		return text, nil
	}
	findings := d.Detect(text)
	if len(findings) == 0 {
		return text, nil
	}

	// Sort findings by length of match descending so longer matches are
	// replaced first, preventing partial replacements.
	sort.Slice(findings, func(i, j int) bool {
		return len(findings[i].Match) > len(findings[j].Match)
	})

	redacted := text
	for _, f := range findings {
		placeholder := fmt.Sprintf("[REDACTED:%s]", f.RuleID)
		redacted = strings.ReplaceAll(redacted, f.Match, placeholder)
	}
	return redacted, findings
}
