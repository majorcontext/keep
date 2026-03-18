package engine

import (
	"encoding/json"
	"fmt"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/redact"
)

// Decision is the outcome of a policy evaluation.
type Decision string

const (
	Allow  Decision = "allow"
	Deny   Decision = "deny"
	Redact Decision = "redact"
)

// Call is the normalized input to the policy engine.
type Call struct {
	Operation string
	Params    map[string]any
	Context   CallContext
}

// CallContext is metadata about who is making the call and when.
type CallContext struct {
	AgentID   string
	UserID    string
	Timestamp time.Time
	Scope     string
	Direction string
	Labels    map[string]string
}

// EvalResult is the output of a policy evaluation.
type EvalResult struct {
	Decision  Decision
	Rule      string
	Message   string
	Mutations []redact.Mutation
	Audit     AuditEntry
}

// AuditEntry is the structured log record for a single evaluation.
type AuditEntry struct {
	Timestamp      time.Time
	Scope          string
	Operation      string
	AgentID        string
	UserID         string
	Direction      string
	Decision       Decision
	Rule           string
	Message        string
	RulesEvaluated []RuleResult
	ParamsSummary  string
}

// RuleResult records what happened when a single rule was checked.
type RuleResult struct {
	Name    string
	Matched bool
	Action  string
	Skipped bool
}

// compiledRule pairs a parsed rule with its compiled CEL program and redact patterns.
type compiledRule struct {
	rule     config.Rule
	program  *keepcel.Program        // nil if no when clause
	patterns []redact.CompiledPattern // nil if not redact action
}

// Evaluator runs the rule evaluation loop for a single scope.
type Evaluator struct {
	celEnv  *keepcel.Env
	rules   []compiledRule
	mode    config.Mode
	onError config.ErrorMode
	scope   string
}

// NewEvaluator creates an evaluator for a scope. Compiles all CEL expressions
// and redact patterns at creation time. Returns an error if any expression
// fails to compile.
func NewEvaluator(
	celEnv *keepcel.Env,
	scope string,
	mode config.Mode,
	onError config.ErrorMode,
	rules []config.Rule,
	aliases map[string]string,
) (*Evaluator, error) {
	compiled := make([]compiledRule, 0, len(rules))
	for _, r := range rules {
		cr := compiledRule{rule: r}

		if r.Match.When != "" {
			resolved := keepcel.ResolveAliases(r.Match.When, aliases)
			prog, err := celEnv.Compile(resolved)
			if err != nil {
				return nil, fmt.Errorf("rule %q: compile when: %w", r.Name, err)
			}
			cr.program = prog
		}

		if r.Action == config.ActionRedact && r.Redact != nil {
			patterns, err := redact.CompilePatterns(r.Redact.Patterns)
			if err != nil {
				return nil, fmt.Errorf("rule %q: compile redact patterns: %w", r.Name, err)
			}
			cr.patterns = patterns
		}

		compiled = append(compiled, cr)
	}

	return &Evaluator{
		celEnv:  celEnv,
		rules:   compiled,
		mode:    mode,
		onError: onError,
		scope:   scope,
	}, nil
}

// Evaluate runs all rules against the given call and returns the result.
func (ev *Evaluator) Evaluate(call Call) EvalResult {
	celParams := call.Params
	celCtx := map[string]any{
		"agent_id":  call.Context.AgentID,
		"user_id":   call.Context.UserID,
		"timestamp": call.Context.Timestamp,
		"scope":     call.Context.Scope,
		"direction": call.Context.Direction,
		"labels":    call.Context.Labels,
	}

	var rulesEvaluated []RuleResult
	var mutations []redact.Mutation

	for _, cr := range ev.rules {
		// Check operation glob.
		if !GlobMatch(cr.rule.Match.Operation, call.Operation) {
			rulesEvaluated = append(rulesEvaluated, RuleResult{
				Name:    cr.rule.Name,
				Skipped: true,
			})
			continue
		}

		// Evaluate when clause if present.
		if cr.program != nil {
			matched, err := cr.program.Eval(celParams, celCtx)
			if err != nil {
				// Treat eval error as not matched (error handling deferred to Task 10).
				rulesEvaluated = append(rulesEvaluated, RuleResult{
					Name:    cr.rule.Name,
					Matched: false,
				})
				continue
			}
			if !matched {
				rulesEvaluated = append(rulesEvaluated, RuleResult{
					Name:    cr.rule.Name,
					Matched: false,
				})
				continue
			}
		}

		// Rule matched.
		rulesEvaluated = append(rulesEvaluated, RuleResult{
			Name:    cr.rule.Name,
			Matched: true,
			Action:  string(cr.rule.Action),
		})

		switch cr.rule.Action {
		case config.ActionDeny:
			decision := Deny
			result := EvalResult{
				Decision: decision,
				Rule:     cr.rule.Name,
				Message:  cr.rule.Message,
				Audit: AuditEntry{
					Timestamp:      call.Context.Timestamp,
					Scope:          ev.scope,
					Operation:      call.Operation,
					AgentID:        call.Context.AgentID,
					UserID:         call.Context.UserID,
					Direction:      call.Context.Direction,
					Decision:       decision,
					Rule:           cr.rule.Name,
					Message:        cr.rule.Message,
					RulesEvaluated: rulesEvaluated,
					ParamsSummary:  paramsSummary(celParams),
				},
			}
			return result

		case config.ActionLog:
			// Already recorded in rulesEvaluated; continue.
			continue

		case config.ActionRedact:
			if cr.rule.Redact != nil {
				m := redact.Apply(call.Params, cr.rule.Redact.Target, cr.patterns)
				mutations = append(mutations, m...)
			}
		}
	}

	// After all rules.
	decision := Allow
	if len(mutations) > 0 {
		decision = Redact
	}

	return EvalResult{
		Decision:  decision,
		Mutations: mutations,
		Audit: AuditEntry{
			Timestamp:      call.Context.Timestamp,
			Scope:          ev.scope,
			Operation:      call.Operation,
			AgentID:        call.Context.AgentID,
			UserID:         call.Context.UserID,
			Direction:      call.Context.Direction,
			Decision:       decision,
			RulesEvaluated: rulesEvaluated,
			ParamsSummary:  paramsSummary(celParams),
		},
	}
}

// paramsSummary returns a JSON-serialized summary of params, truncated to 256 chars.
func paramsSummary(params map[string]any) string {
	if params == nil {
		return "{}"
	}
	b, err := json.Marshal(params)
	if err != nil {
		return "{}"
	}
	s := string(b)
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}
