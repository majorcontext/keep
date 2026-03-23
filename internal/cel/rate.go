package cel

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/majorcontext/keep/internal/rate"
)

// parseWindow parses a window string like "1h", "30s", "5m" into a duration.
// Valid units: s (seconds), m (minutes), h (hours). Max: 24h. Min: 1s.
func parseWindow(window string) (time.Duration, error) {
	if len(window) < 2 {
		return 0, fmt.Errorf("rate: invalid window %q: too short", window)
	}

	unit := window[len(window)-1]
	numStr := strings.TrimSpace(window[:len(window)-1])

	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("rate: invalid window %q: %w", window, err)
	}

	var d time.Duration
	switch unit {
	case 's':
		d = time.Duration(n) * time.Second
	case 'm':
		d = time.Duration(n) * time.Minute
	case 'h':
		d = time.Duration(n) * time.Hour
	default:
		return 0, fmt.Errorf("rate: invalid window %q: unknown unit %q (want s, m, or h)", window, unit)
	}

	const minWindow = time.Second
	const maxWindow = 24 * time.Hour

	if d < minWindow {
		return 0, fmt.Errorf("rate: window %q is below minimum 1s", window)
	}
	if d > maxWindow {
		return 0, fmt.Errorf("rate: window %q exceeds maximum 24h", window)
	}

	return d, nil
}

// rateCountFunc increments the counter and returns the count within the window.
//
// NOTE: Rate limit keys are global across all scopes. Users should include
// scope information in their key expressions (e.g., rateCount(context.scope + ":" + context.agent_id, "1h"))
// to avoid cross-scope counter collisions.
//
// NOTE: rateCount always increments the counter, even in audit_only mode.
// This means audit_only evaluation has a side effect on rate counters.
// This is a known limitation — suppressing increment in audit_only would
// require threading mode information into the CEL function binding.
func rateCountFunc(store *rate.Store, key string, window string) (int, error) {
	if store == nil {
		return 0, fmt.Errorf("rate: no rate store configured")
	}

	d, err := parseWindow(window)
	if err != nil {
		return 0, err
	}

	store.Increment(key)
	return store.Count(key, d), nil
}
