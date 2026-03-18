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
	// Enforced is true when the Decision was actually applied to the call.
	// It is false in audit_only mode, where Decision records what would have
	// happened but the call is allowed regardless.
	Enforced bool
}

// RuleResult records what happened when a single rule was checked.
type RuleResult struct {
	Name         string
	Matched      bool
	Action       string
	Skipped      bool
	Error        bool   // true if a CEL eval error occurred for this rule
	ErrorMessage string // the CEL eval error message, populated when Error is true
}

// compiledRule pairs a parsed rule with its compiled CEL program and redact patterns.
type compiledRule struct {
	rule     config.Rule
	program  *keepcel.Program        // nil if no when clause
	patterns []redact.CompiledPattern // nil if not redact action
}

// Evaluator runs the rule evaluation loop for a single scope.
type Evaluator struct {
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

	// NOTE: audit_only mode prevents enforcement of deny/redact decisions but
	// does NOT suppress side effects in CEL functions (e.g., rateCount still
	// increments counters). This is a known limitation.
	auditOnly := ev.mode == config.ModeAuditOnly

	var rulesEvaluated []RuleResult
	var mutations []redact.Mutation

	// In audit_only mode, track the first deny match so we can report it
	// at the end without short-circuiting rule evaluation.
	var auditDenyRule string
	var auditDenyMessage string
	auditDenied := false

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
			matched, evalErr := evalSafe(cr.program, celParams, celCtx)
			if evalErr != nil {
				errMsg := evalErr.Error()
				// In audit_only mode, always treat errors as not-matched.
				if auditOnly || ev.onError == config.ErrorModeOpen {
					rulesEvaluated = append(rulesEvaluated, RuleResult{
						Name:         cr.rule.Name,
						Matched:      false,
						Error:        true,
						ErrorMessage: errMsg,
					})
					continue
				}
				// ErrorModeClosed: deny immediately.
				msg := fmt.Sprintf("Rule %q evaluation error: %s. Call denied (fail-closed).", cr.rule.Name, errMsg)
				rulesEvaluated = append(rulesEvaluated, RuleResult{
					Name:         cr.rule.Name,
					Matched:      false,
					Error:        true,
					ErrorMessage: errMsg,
				})
				decision := Deny
				return EvalResult{
					Decision: decision,
					Rule:     cr.rule.Name,
					Message:  msg,
					Audit: AuditEntry{
						Timestamp:      call.Context.Timestamp,
						Scope:          ev.scope,
						Operation:      call.Operation,
						AgentID:        call.Context.AgentID,
						UserID:         call.Context.UserID,
						Direction:      call.Context.Direction,
						Decision:       decision,
						Rule:           cr.rule.Name,
						Message:        msg,
						RulesEvaluated: rulesEvaluated,
						ParamsSummary:  paramsSummary(celParams),
						Enforced:       true,
					},
				}
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
			if !auditOnly {
				// Enforce mode: short-circuit and return deny immediately.
				return EvalResult{
					Decision: Deny,
					Rule:     cr.rule.Name,
					Message:  cr.rule.Message,
					Audit: AuditEntry{
						Timestamp:      call.Context.Timestamp,
						Scope:          ev.scope,
						Operation:      call.Operation,
						AgentID:        call.Context.AgentID,
						UserID:         call.Context.UserID,
						Direction:      call.Context.Direction,
						Decision:       Deny,
						Rule:           cr.rule.Name,
						Message:        cr.rule.Message,
						RulesEvaluated: rulesEvaluated,
						ParamsSummary:  paramsSummary(celParams),
						Enforced:       true,
					},
				}
			}
			// audit_only mode: record the first deny match, then continue
			// evaluating remaining rules so audit is complete.
			if !auditDenied {
				auditDenied = true
				auditDenyRule = cr.rule.Name
				auditDenyMessage = cr.rule.Message
			}

		case config.ActionLog:
			// Already recorded in rulesEvaluated; continue.
			continue

		case config.ActionRedact:
			if cr.rule.Redact != nil && !auditOnly {
				m := redact.Apply(call.Params, cr.rule.Redact.Target, cr.patterns)
				mutations = append(mutations, m...)
			}
		}
	}

	// After all rules.

	// In audit_only mode with a deny match, the audit decision is Deny
	// but the actual decision is Allow (not enforced).
	if auditOnly && auditDenied {
		return EvalResult{
			Decision: Allow,
			Audit: AuditEntry{
				Timestamp:      call.Context.Timestamp,
				Scope:          ev.scope,
				Operation:      call.Operation,
				AgentID:        call.Context.AgentID,
				UserID:         call.Context.UserID,
				Direction:      call.Context.Direction,
				Decision:       Deny,
				Rule:           auditDenyRule,
				Message:        auditDenyMessage,
				RulesEvaluated: rulesEvaluated,
				ParamsSummary:  paramsSummary(celParams),
				Enforced:       false,
			},
		}
	}

	// In audit_only mode, mutations are never applied and the decision is always Allow.
	auditDecision := Allow
	returnDecision := Allow
	if len(mutations) > 0 {
		auditDecision = Redact
		if !auditOnly {
			returnDecision = Redact
		}
	}

	enforced := !auditOnly
	returnMutations := mutations
	if auditOnly {
		returnMutations = nil
	}

	// Compute paramsSummary after mutations so redacted values are reflected.
	summary := paramsSummary(celParams)
	if len(mutations) > 0 {
		mutatedParams := redact.ApplyMutations(celParams, mutations)
		summary = paramsSummary(mutatedParams)
	}

	return EvalResult{
		Decision:  returnDecision,
		Mutations: returnMutations,
		Audit: AuditEntry{
			Timestamp:      call.Context.Timestamp,
			Scope:          ev.scope,
			Operation:      call.Operation,
			AgentID:        call.Context.AgentID,
			UserID:         call.Context.UserID,
			Direction:      call.Context.Direction,
			Decision:       auditDecision,
			RulesEvaluated: rulesEvaluated,
			ParamsSummary:  summary,
			Enforced:       enforced,
		},
	}
}

// evalSafe wraps program evaluation to recover from panics.
// Returns the boolean result and any error.
func evalSafe(prog *keepcel.Program, params map[string]any, ctx map[string]any) (result bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = false
			err = fmt.Errorf("CEL eval panic: %v", r)
		}
	}()
	return prog.Eval(params, ctx)
}

// paramsSummary returns a JSON-serialized summary of params, truncated to 256 runes.
// When truncated, an ellipsis marker ("...") is appended.
func paramsSummary(params map[string]any) string {
	if params == nil {
		return "{}"
	}
	b, err := json.Marshal(params)
	if err != nil {
		return "{}"
	}
	s := string(b)
	runes := []rune(s)
	if len(runes) > 256 {
		s = string(runes[:256]) + "..."
	}
	return s
}
