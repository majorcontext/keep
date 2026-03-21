package secrets

import (
	"strings"
	"testing"
)

// awsKey is a fake AWS access key that matches the gitleaks pattern.
// It does not end in "EXAMPLE" (which is allowlisted) and has the correct format.
const awsKey = "AKIAIOSFODNN7REALKEY"

// githubPAT is a fake GitHub PAT that matches the gitleaks github-pat rule.
// It is exactly 36 alphanumeric chars after "ghp_" and does not contain
// the "abcdefghijklmnopqrstuvwxyz" stopword.
const githubPAT = "ghp_0123456789ABCDEFGHIJ0123456789ABCDEF"

// stripeKey matches the gitleaks stripe-access-token rule.
const stripeKey = "sk_live_abcdefghijklmnopqrstuvwx"

func TestDetect_AWSKey(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	findings := d.Detect("access key is " + awsKey)
	if len(findings) == 0 {
		t.Fatal("expected to detect AWS access key")
	}
	found := false
	for _, f := range findings {
		if strings.Contains(f.Match, awsKey) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding to contain the AWS key, got %+v", findings)
	}
}

func TestDetect_GitHubPAT(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	findings := d.Detect(githubPAT)
	if len(findings) == 0 {
		t.Fatal("expected to detect GitHub PAT")
	}
}

func TestDetect_StripeKey(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	findings := d.Detect("stripe key " + stripeKey)
	if len(findings) == 0 {
		t.Fatal("expected to detect Stripe key")
	}
}

func TestDetect_NoMatch(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	findings := d.Detect("this is just normal text with no secrets")
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d: %+v", len(findings), findings)
	}
}

func TestDetect_Multiple(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	text := "aws key " + awsKey + " and github " + githubPAT
	findings := d.Detect(text)
	if len(findings) < 2 {
		t.Errorf("expected at least 2 findings, got %d: %+v", len(findings), findings)
	}
}

func TestRedact_ReplacesWithRuleID(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	text := "my key is " + awsKey + " ok"
	redacted, findings := d.Redact(text)
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	if strings.Contains(redacted, awsKey) {
		t.Errorf("expected key to be redacted, got: %s", redacted)
	}
	if !strings.Contains(redacted, "[REDACTED:") {
		t.Errorf("expected [REDACTED:...] placeholder, got: %s", redacted)
	}
}

func TestRedact_ReturnsFindings(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	_, findings := d.Redact("key is " + awsKey)
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	f := findings[0]
	if f.RuleID == "" {
		t.Error("expected non-empty RuleID")
	}
	if !strings.Contains(f.Match, awsKey) {
		t.Errorf("expected Match to contain the key, got: %s", f.Match)
	}
}

func TestRedact_NoMatch(t *testing.T) {
	d, err := NewDetector()
	if err != nil {
		t.Fatal(err)
	}
	text := "nothing secret here"
	redacted, findings := d.Redact(text)
	if redacted != text {
		t.Errorf("expected unchanged text, got: %s", redacted)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d", len(findings))
	}
}

func TestNilDetector(t *testing.T) {
	var d *Detector
	findings := d.Detect(awsKey)
	if findings != nil {
		t.Errorf("expected nil from nil detector, got %+v", findings)
	}
	redacted, findings := d.Redact(awsKey)
	if redacted != awsKey {
		t.Errorf("expected original text from nil detector, got: %s", redacted)
	}
	if findings != nil {
		t.Errorf("expected nil findings from nil detector, got %+v", findings)
	}
}
