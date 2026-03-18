// Package keep is an API-level policy engine for AI agents.
package keep

import (
	"fmt"
	"sort"
	"sync"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/config"
	"github.com/majorcontext/keep/internal/engine"
	"github.com/majorcontext/keep/internal/rate"
	"github.com/majorcontext/keep/internal/redact"
)

// Type aliases re-exported from internal packages.
type Call = engine.Call
type CallContext = engine.CallContext
type EvalResult = engine.EvalResult
type Decision = engine.Decision
type AuditEntry = engine.AuditEntry
type RuleResult = engine.RuleResult
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
	cfg        engineConfig
}

type engineConfig struct {
	rulesDir     string
	profilesDir  string
	packsDir     string
	forceEnforce bool
}

// Option configures Load behavior.
type Option func(*engineConfig)

// WithProfilesDir sets the directory to load profile YAML files from.
func WithProfilesDir(dir string) Option { return func(c *engineConfig) { c.profilesDir = dir } }

// WithPacksDir sets the directory to load starter pack YAML files from.
func WithPacksDir(dir string) Option { return func(c *engineConfig) { c.packsDir = dir } }

// WithForceEnforce overrides every scope's mode to "enforce".
func WithForceEnforce() Option { return func(c *engineConfig) { c.forceEnforce = true } }

// Load reads rule files from rulesDir, compiles all CEL expressions and
// redact patterns, and returns a ready-to-use Engine.
func Load(rulesDir string, opts ...Option) (*Engine, error) {
	cfg := engineConfig{rulesDir: rulesDir}
	for _, opt := range opts {
		opt(&cfg)
	}

	// 1. Load config.
	loadResult, err := config.LoadAll(rulesDir, cfg.profilesDir, cfg.packsDir)
	if err != nil {
		return nil, fmt.Errorf("keep: load config: %w", err)
	}

	// 2. Create rate store.
	store := rate.NewStore()

	// 3. Create CEL environment.
	celEnv, err := keepcel.NewEnv(keepcel.WithRateStore(store))
	if err != nil {
		return nil, fmt.Errorf("keep: create cel env: %w", err)
	}

	// 4. Build evaluators for each scope.
	evaluators, err := buildEvaluators(loadResult, celEnv, cfg)
	if err != nil {
		return nil, err
	}

	store.StartGC(60*time.Second, 24*time.Hour)

	return &Engine{
		evaluators: evaluators,
		rateStore:  store,
		cfg:        cfg,
	}, nil
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
		return EvalResult{}, fmt.Errorf("keep: scope %q not found", scope)
	}

	return ev.Evaluate(call), nil
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

	celEnv, err := keepcel.NewEnv(keepcel.WithRateStore(e.rateStore))
	if err != nil {
		return fmt.Errorf("keep: reload cel env: %w", err)
	}

	evaluators, err := buildEvaluators(loadResult, celEnv, e.cfg)
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

// buildEvaluators creates compiled evaluators for every scope in the load result.
func buildEvaluators(lr *config.LoadResult, celEnv *keepcel.Env, cfg engineConfig) (map[string]*engine.Evaluator, error) {
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
		if cfg.forceEnforce {
			mode = config.ModeEnforce
		}
		if mode == "" {
			mode = config.ModeAuditOnly // default
		}

		onError := rf.OnError
		if onError == "" {
			onError = config.ErrorModeClosed // default
		}

		ev, err := engine.NewEvaluator(celEnv, scopeName, mode, onError, rules, aliases)
		if err != nil {
			return nil, fmt.Errorf("keep: compile scope %q: %w", scopeName, err)
		}
		evaluators[scopeName] = ev
	}
	return evaluators, nil
}
