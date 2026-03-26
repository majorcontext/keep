// Package keep is an API-level policy engine for AI agents.
package keep

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/engine"
	"github.com/majorcontext/keep/internal/rate"
	"github.com/majorcontext/keep/internal/redact"
	"github.com/majorcontext/keep/internal/secrets"
)

// Type aliases re-exported from internal packages.
type Call = engine.Call
type CallContext = engine.CallContext
type EvalResult = engine.EvalResult
type Decision = engine.Decision
type AuditEntry = engine.AuditEntry
type RuleResult = engine.RuleResult
type RedactedField = engine.RedactedField
type Mutation = redact.Mutation

// Decision constants re-exported from the engine package.
const (
	Allow  = engine.Allow
	Deny   = engine.Deny
	Redact = engine.Redact
)

// Engine holds compiled evaluators for each policy scope.
type Engine struct {
	mu         sync.RWMutex
	evaluators map[string]*engine.Evaluator
	rateStore  *rate.Store
	secrets    *secrets.Detector
	cfg        engineConfig
}

type engineConfig struct {
	rulesDir     string
	profilesDir  string
	packsDir     string
	modeOverride config.Mode
	auditHook    func(AuditEntry)
}

// Option configures Load behavior.
type Option func(*engineConfig)

// WithProfilesDir sets the directory to load profile YAML files from.
func WithProfilesDir(dir string) Option { return func(c *engineConfig) { c.profilesDir = dir } }

// WithPacksDir sets the directory to load starter pack YAML files from.
func WithPacksDir(dir string) Option { return func(c *engineConfig) { c.packsDir = dir } }

// WithMode overrides the mode for all scopes. Valid values are "enforce"
// and "audit_only". Returns an error from Load/LoadFromBytes if invalid.
func WithMode(mode string) Option {
	return func(c *engineConfig) { c.modeOverride = config.Mode(mode) }
}

// WithAuditHook registers a callback invoked synchronously after every
// Evaluate call. The hook receives the AuditEntry from the evaluation
// result. It is not called when Evaluate returns an error (e.g. unknown scope).
func WithAuditHook(hook func(AuditEntry)) Option {
	return func(c *engineConfig) { c.auditHook = hook }
}

// WithForceEnforce overrides every scope's mode to "enforce".
// Deprecated: Use WithMode("enforce") instead.
func WithForceEnforce() Option { return WithMode("enforce") }

// LoadFromBytes creates an Engine from raw YAML bytes representing a single
// rule file. The YAML must contain a valid Keep rule file with a scope field.
// Pack references are not supported — all rules must be inline.
//
// The returned Engine is safe for concurrent use. Call Close when done.
//
// This constructor is intended for embedding Keep in other programs (e.g. Moat)
// where the caller controls configuration and does not use the filesystem.
func LoadFromBytes(data []byte, opts ...Option) (*Engine, error) {
	var cfg engineConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	rf, err := config.ParseRuleFile(data)
	if err != nil {
		return nil, fmt.Errorf("keep: %w", err)
	}

	lr := &config.LoadResult{
		Scopes:        map[string]*config.RuleFile{rf.Scope: rf},
		ResolvedRules: map[string][]config.Rule{rf.Scope: rf.Rules},
		Profiles:      map[string]*config.Profile{},
	}

	return buildEngine(lr, cfg)
}

// Load reads rule files from rulesDir, compiles all CEL expressions and
// redact patterns, and returns a ready-to-use Engine.
func Load(rulesDir string, opts ...Option) (*Engine, error) {
	cfg := engineConfig{rulesDir: rulesDir}
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	loadResult, err := config.LoadAll(rulesDir, cfg.profilesDir, cfg.packsDir)
	if err != nil {
		return nil, fmt.Errorf("keep: load config: %w", err)
	}

	return buildEngine(loadResult, cfg)
}

// Close stops the rate counter GC goroutine. Call this when the engine
// is no longer needed to prevent goroutine leaks.
func (e *Engine) Close() {
	if e.rateStore != nil {
		e.rateStore.StopGC()
	}
}

// Evaluate runs all rules in the given scope against the call and returns
// the policy decision.
func (e *Engine) Evaluate(call Call, scope string) (EvalResult, error) {
	e.mu.RLock()
	ev, ok := e.evaluators[scope]
	e.mu.RUnlock()

	if !ok {
		return EvalResult{}, fmt.Errorf("keep: scope %q not found (available: %s)", scope, strings.Join(e.Scopes(), ", "))
	}

	result := ev.Evaluate(call)
	if e.cfg.auditHook != nil {
		e.cfg.auditHook(result.Audit)
	}
	return result, nil
}

// Scopes returns the sorted list of loaded scope names.
func (e *Engine) Scopes() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	scopes := make([]string, 0, len(e.evaluators))
	for name := range e.evaluators {
		scopes = append(scopes, name)
	}
	sort.Strings(scopes)
	return scopes
}

// Reload re-reads all configuration from disk and recompiles evaluators.
// The rate store is preserved across reloads.
func (e *Engine) Reload() error {
	loadResult, err := config.LoadAll(e.cfg.rulesDir, e.cfg.profilesDir, e.cfg.packsDir)
	if err != nil {
		return fmt.Errorf("keep: reload: %w", err)
	}

	celEnv, err := keepcel.NewEnv(keepcel.WithRateStore(e.rateStore), keepcel.WithSecretDetector(e.secrets))
	if err != nil {
		return fmt.Errorf("keep: reload cel env: %w", err)
	}

	evaluators, err := buildEvaluators(loadResult, celEnv, e.cfg, e.secrets)
	if err != nil {
		return fmt.Errorf("keep: reload: %w", err)
	}

	e.mu.Lock()
	e.evaluators = evaluators
	e.mu.Unlock()

	return nil
}

// ApplyMutations returns a new params map with the given mutations applied.
// The original map is not modified.
func ApplyMutations(params map[string]any, mutations []Mutation) map[string]any {
	return redact.ApplyMutations(params, mutations)
}

// LintWarning is a non-fatal issue found during linting.
type LintWarning = config.LintWarning

// LintRules loads rule files from the given directory and returns lint warnings
// without building a full engine. This is used by the validate command.
func LintRules(rulesDir string, profilesDir string, packsDir string) ([]LintWarning, error) {
	lr, err := config.LoadAll(rulesDir, profilesDir, packsDir)
	if err != nil {
		return nil, err
	}
	return config.LintAll(lr), nil
}

// ValidateRuleBytes parses and validates a Keep rule file from raw YAML bytes
// without compiling an engine. Use this to catch invalid rules early (e.g. at
// deploy time) before the engine is needed at runtime.
func ValidateRuleBytes(data []byte) error {
	rf, err := config.ParseRuleFile(data)
	if err != nil {
		return fmt.Errorf("keep: %w", err)
	}

	// Also verify CEL expressions compile, since ParseRuleFile only checks
	// YAML structure and field validation.
	celEnv, err := keepcel.NewEnv()
	if err != nil {
		return fmt.Errorf("keep: create cel env: %w", err)
	}
	for _, rule := range rf.Rules {
		if rule.Match.When != "" {
			if _, err := celEnv.Compile(rule.Match.When); err != nil {
				return fmt.Errorf("keep: rule %q: invalid CEL expression: %w", rule.Name, err)
			}
		}
	}

	return nil
}

// RuleSet is a programmatic builder for constructing policy rules without
// generating YAML. It produces the same internal representation as LoadFromBytes.
type RuleSet struct {
	scope string
	mode  string
	allow []string
	deny  []string
}

// NewRuleSet creates a new rule builder for the given scope.
// Mode should be "enforce" or "audit_only".
func NewRuleSet(scope, mode string) *RuleSet {
	return &RuleSet{scope: scope, mode: mode}
}

// Allow adds operations to the allowlist. When an allowlist is present,
// operations not in the list are denied.
func (rs *RuleSet) Allow(ops ...string) {
	rs.allow = append(rs.allow, ops...)
}

// Deny adds operations to the denylist. Deny takes precedence over Allow
// for overlapping entries.
func (rs *RuleSet) Deny(ops ...string) {
	rs.deny = append(rs.deny, ops...)
}

// Compile builds an Engine from the rule set. Options (WithMode, WithAuditHook)
// are applied the same as with LoadFromBytes.
func (rs *RuleSet) Compile(opts ...Option) (*Engine, error) {
	var cfg engineConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	rules := rs.buildRules()
	rf := &config.RuleFile{
		Scope: rs.scope,
		Mode:  config.Mode(rs.mode),
		Rules: rules,
	}

	lr := &config.LoadResult{
		Scopes:        map[string]*config.RuleFile{rs.scope: rf},
		ResolvedRules: map[string][]config.Rule{rs.scope: rules},
		Profiles:      map[string]*config.Profile{},
	}

	return buildEngine(lr, cfg)
}

func (rs *RuleSet) buildRules() []config.Rule {
	var rules []config.Rule

	// Deny rules come first — exact match, short-circuits on hit.
	for _, op := range rs.deny {
		rules = append(rules, config.Rule{
			Name:    "deny-" + op,
			Match:   config.Match{Operation: op},
			Action:  config.ActionDeny,
			Message: fmt.Sprintf("%s is not allowed", op),
		})
	}

	// If there's an allowlist, add a catch-all deny that skips allowed
	// operations via a CEL when clause on context.operation.
	if len(rs.allow) > 0 {
		// Build CEL expression: !(context.operation in ['op1', 'op2'])
		quoted := make([]string, len(rs.allow))
		for i, op := range rs.allow {
			quoted[i] = fmt.Sprintf("'%s'", op)
		}
		when := fmt.Sprintf("!(context.operation in [%s])", strings.Join(quoted, ", "))

		rules = append(rules, config.Rule{
			Name:    "deny-unlisted",
			Match:   config.Match{Operation: "*", When: when},
			Action:  config.ActionDeny,
			Message: "operation not in allowlist",
		})
	}

	return rules
}

func (c *engineConfig) validate() error {
	if c.modeOverride != "" && c.modeOverride != config.ModeEnforce && c.modeOverride != config.ModeAuditOnly {
		return fmt.Errorf("keep: invalid mode %q (must be %q or %q)", c.modeOverride, config.ModeEnforce, config.ModeAuditOnly)
	}
	return nil
}

// buildEngine creates a ready-to-use Engine from a LoadResult and config.
func buildEngine(lr *config.LoadResult, cfg engineConfig) (*Engine, error) {
	store := rate.NewStore()

	detector, err := secrets.NewDetector()
	if err != nil {
		return nil, fmt.Errorf("keep: init secrets detector: %w", err)
	}

	celEnv, err := keepcel.NewEnv(keepcel.WithRateStore(store), keepcel.WithSecretDetector(detector))
	if err != nil {
		return nil, fmt.Errorf("keep: create cel env: %w", err)
	}

	evaluators, err := buildEvaluators(lr, celEnv, cfg, detector)
	if err != nil {
		return nil, err
	}

	store.StartGC(60*time.Second, 24*time.Hour)

	return &Engine{
		evaluators: evaluators,
		rateStore:  store,
		secrets:    detector,
		cfg:        cfg,
	}, nil
}

// buildEvaluators creates compiled evaluators for every scope in the load result.
func buildEvaluators(lr *config.LoadResult, celEnv *keepcel.Env, cfg engineConfig, detector *secrets.Detector) (map[string]*engine.Evaluator, error) {
	evaluators := make(map[string]*engine.Evaluator, len(lr.Scopes))
	for scopeName, rf := range lr.Scopes {
		rules := lr.ResolvedRules[scopeName]

		// Get profile aliases if scope has a profile.
		var aliases map[string]string
		if rf.Profile != "" {
			if p, ok := lr.Profiles[rf.Profile]; ok {
				aliases = p.Aliases
			}
		}

		mode := rf.Mode
		if cfg.modeOverride != "" {
			mode = cfg.modeOverride
		}
		if mode == "" {
			mode = config.ModeAuditOnly // default
		}

		onError := rf.OnError
		if onError == "" {
			onError = config.ErrorModeClosed // default
		}

		ev, err := engine.NewEvaluator(celEnv, scopeName, mode, onError, rules, aliases, rf.Defs, detector, rf.CaseSensitive)
		if err != nil {
			return nil, fmt.Errorf("keep: compile scope %q: %w", scopeName, err)
		}
		evaluators[scopeName] = ev
	}
	return evaluators, nil
}
