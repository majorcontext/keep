// Package llm provides a provider-agnostic pipeline for evaluating
// LLM API requests and responses against Keep policy rules.
//
// The pipeline decomposes provider-specific message formats into flat
// keep.Call objects, evaluates each against the policy engine, and
// reassembles mutations back into the provider format.
//
// Provider support is implemented via the Codec interface. See the
// anthropic sub-package for the Anthropic Messages API codec.
package llm

import (
	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/sse"
)

// DecomposeConfig controls which message components are decomposed into
// separate policy calls. Zero value enables the default set (tool_result,
// tool_use, request/response summaries enabled; text disabled).
type DecomposeConfig struct {
	// ToolResult decomposes tool_result blocks (default: true).
	ToolResult *bool
	// ToolUse decomposes tool_use blocks (default: true).
	ToolUse *bool
	// Text decomposes text content blocks (default: false).
	Text *bool
	// RequestSummary emits an llm.request summary call (default: true).
	RequestSummary *bool
	// ResponseSummary emits an llm.response summary call (default: true).
	ResponseSummary *bool
}

// ToolResultEnabled returns whether tool_result decomposition is enabled.
func (d DecomposeConfig) ToolResultEnabled() bool { return d.ToolResult == nil || *d.ToolResult }

// ToolUseEnabled returns whether tool_use decomposition is enabled.
func (d DecomposeConfig) ToolUseEnabled() bool { return d.ToolUse == nil || *d.ToolUse }

// TextEnabled returns whether text decomposition is enabled.
func (d DecomposeConfig) TextEnabled() bool { return d.Text != nil && *d.Text }

// RequestSummaryEnabled returns whether request summary is enabled.
func (d DecomposeConfig) RequestSummaryEnabled() bool {
	return d.RequestSummary == nil || *d.RequestSummary
}

// ResponseSummaryEnabled returns whether response summary is enabled.
func (d DecomposeConfig) ResponseSummaryEnabled() bool {
	return d.ResponseSummary == nil || *d.ResponseSummary
}

// Result is the outcome of evaluating a request or response against policy.
type Result struct {
	// Decision is the aggregate policy decision: Allow, Deny, or Redact.
	Decision keep.Decision
	// Rule is the name of the rule that triggered a Deny or the first Redact.
	// Empty for Allow decisions.
	Rule string
	// Message is the deny message from the triggering rule.
	Message string
	// Body is the (possibly redacted) request or response JSON.
	// For Allow decisions, this is the original body unchanged.
	// For Redact decisions, mutations have been applied.
	// For Deny decisions, this is nil.
	Body []byte
	// Audits contains one AuditEntry per decomposed call that was evaluated.
	Audits []keep.AuditEntry
}

// StreamResult is the outcome of evaluating a streaming response.
type StreamResult struct {
	// Decision is the aggregate policy decision.
	Decision keep.Decision
	// Rule is the name of the triggering rule.
	Rule string
	// Message is the deny message.
	Message string
	// Events contains the SSE events to send to the client.
	// For Allow decisions, these are the original events.
	// For Redact decisions, these are synthesized from the patched response.
	// For Deny decisions, this is nil.
	Events []sse.Event
	// Body is the reassembled (and possibly redacted) response JSON.
	// Useful for logging/debugging the complete response after policy.
	// For Deny decisions, this is nil.
	Body []byte
	// RawBody is the reassembled response JSON before policy evaluation.
	// Consumers can use this for pre-policy logging without re-reassembling
	// the stream. For Deny decisions, this is still populated.
	RawBody []byte
	// Audits contains one AuditEntry per decomposed call that was evaluated.
	Audits []keep.AuditEntry
}

// Codec decomposes provider-specific LLM messages into keep.Call objects
// and reassembles policy mutations back into the provider format.
//
// Each method pair (Decompose/Reassemble) shares an opaque handle that
// carries parsed state and position mappings. The pipeline passes the
// handle from Decompose to Reassemble without inspecting it.
//
// Implementations must be safe for concurrent use.
type Codec interface {
	// DecomposeRequest breaks a request body into policy calls.
	// Returns the calls and an opaque handle for ReassembleRequest.
	DecomposeRequest(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error)

	// DecomposeResponse breaks a response body into policy calls.
	// Returns the calls and an opaque handle for ReassembleResponse.
	DecomposeResponse(body []byte, scope string, cfg DecomposeConfig) ([]keep.Call, any, error)

	// ReassembleRequest patches mutations into the request using the
	// handle from DecomposeRequest. Returns original body if no mutations.
	ReassembleRequest(handle any, results []keep.EvalResult) ([]byte, error)

	// ReassembleResponse patches mutations into the response.
	ReassembleResponse(handle any, results []keep.EvalResult) ([]byte, error)

	// ReassembleStream reassembles provider-specific SSE events into a
	// complete response body suitable for DecomposeResponse.
	ReassembleStream(events []sse.Event) (body []byte, err error)

	// SynthesizeEvents creates replacement SSE events from a patched
	// response body (the output of ReassembleResponse after redaction).
	SynthesizeEvents(patchedBody []byte) ([]sse.Event, error)
}
