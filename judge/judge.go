package judge

import "context"

// Provider sends content to an LLM and returns a structured verdict.
type Provider interface {
	Judge(ctx context.Context, req Request) (Verdict, error)
}

// Request is the input to a judge call.
type Request struct {
	Prompt  string // The judgment prompt from the rule
	Content string // The content to judge
	Model   string // Model identifier (shortcut or full ID)
}

// Verdict is the judge's response.
type Verdict struct {
	Decision Decision // allow or deny
	Reason   string   // LLM's reasoning
	Usage    Usage    // Token consumption
}

// Decision is the judge's binary outcome.
type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
)

// Usage tracks token consumption for a judge call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}
