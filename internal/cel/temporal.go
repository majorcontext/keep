package cel

import (
	"fmt"
	"strings"
	"time"
)

// InTimeWindow reports whether timestamp falls within the [start, end) time-of-day
// window in the given IANA timezone. start and end are "HH:MM" strings (24-hour).
// If end <= start, midnight-wrapping is NOT supported and false is always returned.
func InTimeWindow(start, end, tz string, timestamp time.Time) bool {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false
	}

	startMins, err := parseHHMM(start)
	if err != nil {
		return false
	}
	endMins, err := parseHHMM(end)
	if err != nil {
		return false
	}

	// No midnight-wrap support: if end <= start, always return false.
	if endMins <= startMins {
		return false
	}

	local := timestamp.In(loc)
	nowMins := local.Hour()*60 + local.Minute()

	return nowMins >= startMins && nowMins < endMins
}

// DayOfWeek returns the lowercase name of the weekday (e.g., "monday") for
// timestamp interpreted in UTC.
func DayOfWeek(timestamp time.Time) string {
	return strings.ToLower(timestamp.UTC().Weekday().String())
}

// DayOfWeekTZ returns the lowercase name of the weekday for timestamp
// interpreted in the given IANA timezone. Returns empty string on invalid tz.
func DayOfWeekTZ(tz string, timestamp time.Time) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return ""
	}
	return strings.ToLower(timestamp.In(loc).Weekday().String())
}

// parseHHMM parses a "HH:MM" string and returns total minutes since midnight.
func parseHHMM(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, fmt.Errorf("temporal: invalid time %q: %w", s, err)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("temporal: out-of-range time %q", s)
	}
	return h*60 + m, nil
}

// rewriteTemporalCalls rewrites user-facing sugar expressions into their
// internal form that takes an explicit _timestamp first argument.
//
//	inTimeWindow('HH:MM', 'HH:MM', 'tz')   →  inTimeWindow(_timestamp, 'HH:MM', 'HH:MM', 'tz')
//	dayOfWeek()                              →  dayOfWeek(_timestamp)
//	dayOfWeek('tz')                          →  dayOfWeek(_timestamp, 'tz')
//
// LIMITATION: This uses simple string substitution and is NOT string-literal-aware.
// Function names appearing inside quoted strings (e.g. 'call inTimeWindow(a,b,c)')
// will be incorrectly rewritten. A full fix requires AST-level macro rewriting,
// which is deferred to a future refactor.
func rewriteTemporalCalls(expr string) string {
	expr = strings.ReplaceAll(expr, "inTimeWindow(", "inTimeWindow(_timestamp, ")
	// Handle zero-arg dayOfWeek() BEFORE the generic dayOfWeek( replacement.
	// Use a two-pass approach: first replace the zero-arg form with a sentinel,
	// then do the generic replacement, then restore the sentinel.
	const sentinel = "\x00DOW_NOARG\x00"
	expr = strings.ReplaceAll(expr, "dayOfWeek()", sentinel)
	expr = strings.ReplaceAll(expr, "dayOfWeek(", "dayOfWeek(_timestamp, ")
	expr = strings.ReplaceAll(expr, sentinel, "dayOfWeek(_timestamp)")
	return expr
}
