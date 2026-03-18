package cel_test

import (
	"testing"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
)

// --- Unit tests for temporal helper functions ---

func TestInTimeWindow_Inside(t *testing.T) {
	// 10:00 AM Pacific (UTC-8 in March = UTC-7 but use fixed offset for predictability)
	// America/Los_Angeles in March 2026 is PDT = UTC-7
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	ts := time.Date(2026, 3, 16, 10, 0, 0, 0, loc) // 10:00 AM LA time

	got := keepcel.InTimeWindow("09:00", "18:00", "America/Los_Angeles", ts)
	if !got {
		t.Error("expected true: 10:00 AM is inside 09:00-18:00 America/Los_Angeles")
	}
}

func TestInTimeWindow_Outside(t *testing.T) {
	// 02:00 AM Pacific is outside 09:00-18:00
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	ts := time.Date(2026, 3, 16, 2, 0, 0, 0, loc) // 02:00 AM LA time

	got := keepcel.InTimeWindow("09:00", "18:00", "America/Los_Angeles", ts)
	if got {
		t.Error("expected false: 02:00 AM is outside 09:00-18:00 America/Los_Angeles")
	}
}

func TestInTimeWindow_NoWrap(t *testing.T) {
	// Window 22:00-06:00: end < start, so no midnight wrap — always returns false.
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	// Test at 23:00 — logically "inside" a wrapped window, but spec says no wrap.
	ts := time.Date(2026, 3, 16, 23, 0, 0, 0, loc)
	got := keepcel.InTimeWindow("22:00", "06:00", "America/Los_Angeles", ts)
	if got {
		t.Error("expected false: no midnight wrap support, 22:00-06:00 window always returns false")
	}

	// Also test at 03:00 — also inside a wrapped window, but should be false.
	ts2 := time.Date(2026, 3, 16, 3, 0, 0, 0, loc)
	got2 := keepcel.InTimeWindow("22:00", "06:00", "America/Los_Angeles", ts2)
	if got2 {
		t.Error("expected false: no midnight wrap support, 22:00-06:00 window always returns false")
	}
}

func TestDayOfWeek_UTC(t *testing.T) {
	// 2026-03-16 is a Monday
	ts := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	got := keepcel.DayOfWeek(ts)
	if got != "monday" {
		t.Errorf("expected %q, got %q", "monday", got)
	}
}

func TestDayOfWeek_WithTimezone(t *testing.T) {
	// 2026-03-16 00:30 UTC is still Sunday 2026-03-15 in America/Los_Angeles (UTC-7 in PDT)
	ts := time.Date(2026, 3, 16, 0, 30, 0, 0, time.UTC)

	// In UTC it's monday
	gotUTC := keepcel.DayOfWeek(ts)
	if gotUTC != "monday" {
		t.Errorf("UTC: expected %q, got %q", "monday", gotUTC)
	}

	// In America/Los_Angeles (UTC-7) it's still sunday (00:30 UTC - 7h = 17:30 previous day)
	gotTZ := keepcel.DayOfWeekTZ("America/Los_Angeles", ts)
	if gotTZ != "sunday" {
		t.Errorf("America/Los_Angeles: expected %q, got %q", "sunday", gotTZ)
	}
}

// --- CEL integration tests ---

func TestInTimeWindow_CEL(t *testing.T) {
	env := mustNewEnv(t)

	prog := mustCompile(t, env, "inTimeWindow(now, '09:00', '18:00', 'America/Los_Angeles')")

	// 2026-03-16 17:00 UTC = 10:00 AM PDT (UTC-7) — inside the window
	ctx := map[string]any{
		"timestamp": time.Date(2026, 3, 16, 17, 0, 0, 0, time.UTC),
	}

	got, err := prog.Eval(nil, ctx)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: 10:00 AM PDT is inside 09:00-18:00 America/Los_Angeles")
	}
}

func TestDayOfWeek_CEL(t *testing.T) {
	env := mustNewEnv(t)

	prog := mustCompile(t, env, "dayOfWeek(now) == 'monday'")

	// 2026-03-16 is a Monday in UTC.
	ctx := map[string]any{
		"timestamp": time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC),
	}

	got, err := prog.Eval(nil, ctx)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: 2026-03-16 is a Monday")
	}
}

func TestDayOfWeek_CEL_WithTZ(t *testing.T) {
	env := mustNewEnv(t)

	prog := mustCompile(t, env, "dayOfWeek(now, 'America/Los_Angeles') == 'monday'")

	// 2026-03-17 06:00 UTC = 2026-03-16 23:00 PDT (still Monday in LA)
	ctx := map[string]any{
		"timestamp": time.Date(2026, 3, 17, 6, 0, 0, 0, time.UTC),
	}

	got, err := prog.Eval(nil, ctx)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true: 2026-03-17 06:00 UTC is Monday in America/Los_Angeles")
	}
}
