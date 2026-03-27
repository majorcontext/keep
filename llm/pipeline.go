package llm

import (
	"fmt"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/sse"
)

// EvaluateRequest decomposes a provider-specific request body into policy
// calls, evaluates each against the engine, and reassembles mutations.
//
// On deny: returns Result with Decision=Deny, Body=nil.
// On redact: returns Result with Decision=Redact, Body=patched JSON.
// On allow: returns Result with Decision=Allow, Body=original JSON.
func EvaluateRequest(engine *keep.Engine, codec Codec, body []byte, scope string, cfg DecomposeConfig) (*Result, error) {
	calls, handle, err := codec.DecomposeRequest(body, scope, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm: decompose request: %w", err)
	}

	results, outcome, err := evaluateCalls(engine, calls, scope)
	if err != nil {
		return nil, err
	}

	if outcome.denied {
		return &Result{
			Decision: keep.Deny,
			Rule:     outcome.rule,
			Message:  outcome.message,
			Audits:   collectAudits(results),
		}, nil
	}

	outBody := body
	if outcome.redacted {
		outBody, err = codec.ReassembleRequest(handle, results)
		if err != nil {
			return nil, fmt.Errorf("llm: reassemble request: %w", err)
		}
	}

	decision := keep.Allow
	if outcome.redacted {
		decision = keep.Redact
	}

	return &Result{
		Decision: decision,
		Rule:     outcome.rule,
		Message:  outcome.message,
		Body:     outBody,
		Audits:   collectAudits(results),
	}, nil
}

// EvaluateResponse decomposes a provider-specific response body into policy
// calls, evaluates each, and reassembles mutations.
func EvaluateResponse(engine *keep.Engine, codec Codec, body []byte, scope string, cfg DecomposeConfig) (*Result, error) {
	calls, handle, err := codec.DecomposeResponse(body, scope, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm: decompose response: %w", err)
	}

	results, outcome, err := evaluateCalls(engine, calls, scope)
	if err != nil {
		return nil, err
	}

	if outcome.denied {
		return &Result{
			Decision: keep.Deny,
			Rule:     outcome.rule,
			Message:  outcome.message,
			Audits:   collectAudits(results),
		}, nil
	}

	outBody := body
	if outcome.redacted {
		outBody, err = codec.ReassembleResponse(handle, results)
		if err != nil {
			return nil, fmt.Errorf("llm: reassemble response: %w", err)
		}
	}

	decision := keep.Allow
	if outcome.redacted {
		decision = keep.Redact
	}

	return &Result{
		Decision: decision,
		Rule:     outcome.rule,
		Message:  outcome.message,
		Body:     outBody,
		Audits:   collectAudits(results),
	}, nil
}

// EvaluateStream reassembles SSE events into a complete response, evaluates
// policy, and returns either the original events (clean) or synthesized
// events (redacted).
func EvaluateStream(engine *keep.Engine, codec Codec, events []sse.Event, scope string, cfg DecomposeConfig) (*StreamResult, error) {
	// Reassemble SSE events into a complete response.
	body, err := codec.ReassembleStream(events)
	if err != nil {
		return nil, fmt.Errorf("llm: reassemble stream: %w", err)
	}

	// Decompose the reassembled response.
	calls, handle, err := codec.DecomposeResponse(body, scope, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm: decompose stream response: %w", err)
	}

	results, outcome, err := evaluateCalls(engine, calls, scope)
	if err != nil {
		return nil, err
	}

	if outcome.denied {
		return &StreamResult{
			Decision: keep.Deny,
			Rule:     outcome.rule,
			Message:  outcome.message,
			Audits:   collectAudits(results),
		}, nil
	}

	outBody := body
	outEvents := events
	if outcome.redacted {
		// Reassemble the patched response body, then synthesize events from it.
		outBody, err = codec.ReassembleResponse(handle, results)
		if err != nil {
			return nil, fmt.Errorf("llm: reassemble stream response: %w", err)
		}
		outEvents, err = codec.SynthesizeEvents(outBody)
		if err != nil {
			return nil, fmt.Errorf("llm: synthesize events: %w", err)
		}
	}

	decision := keep.Allow
	if outcome.redacted {
		decision = keep.Redact
	}

	return &StreamResult{
		Decision: decision,
		Rule:     outcome.rule,
		Message:  outcome.message,
		Events:   outEvents,
		Body:     outBody,
		Audits:   collectAudits(results),
	}, nil
}

// evalOutcome tracks the aggregate result across multiple call evaluations.
type evalOutcome struct {
	denied   bool
	redacted bool
	rule     string
	message  string
}

// evaluateCalls runs each call through the engine and collects results.
// Short-circuits on the first deny.
func evaluateCalls(engine *keep.Engine, calls []keep.Call, scope string) ([]keep.EvalResult, evalOutcome, error) {
	results := make([]keep.EvalResult, 0, len(calls))
	var outcome evalOutcome

	for _, call := range calls {
		result, err := engine.Evaluate(call, scope)
		if err != nil {
			return results, outcome, fmt.Errorf("llm: evaluate: %w", err)
		}
		results = append(results, result)

		switch result.Decision {
		case keep.Deny:
			outcome.denied = true
			outcome.rule = result.Rule
			outcome.message = result.Message
			return results, outcome, nil
		case keep.Redact:
			if !outcome.redacted {
				outcome.redacted = true
				outcome.rule = result.Rule
			}
		}
	}

	return results, outcome, nil
}

// collectAudits extracts audit entries from evaluation results.
func collectAudits(results []keep.EvalResult) []keep.AuditEntry {
	audits := make([]keep.AuditEntry, len(results))
	for i, r := range results {
		audits[i] = r.Audit
	}
	return audits
}
