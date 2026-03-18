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

