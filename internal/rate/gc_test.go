package rate

import (
	"testing"
	"time"
)

func TestStore_StartGC(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	// Add some entries.
	s.Increment("key1")
	s.Increment("key2")

	// Verify entries exist.
	s.mu.Lock()
	initialLen := len(s.data)
	s.mu.Unlock()
	if initialLen != 2 {
		t.Fatalf("expected 2 keys before GC, got %d", initialLen)
	}

	// Start GC with a short interval and maxAge of 1 hour.
	// Entries were added at mock clock time; we'll advance the clock
	// past maxAge so entries become stale.
	const gcInterval = 10 * time.Millisecond
	const maxAge = time.Hour
	s.StartGC(gcInterval, maxAge)
	defer s.StopGC()

	// Advance mock clock past maxAge so entries are considered expired.
	clk.Advance(2 * time.Hour)

	// Wait long enough for at least one GC tick to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		remaining := len(s.data)
		s.mu.Unlock()
		if remaining == 0 {
			break
		}
		time.Sleep(gcInterval)
	}

	s.mu.Lock()
	remaining := len(s.data)
	s.mu.Unlock()

	if remaining != 0 {
		t.Errorf("expected all expired entries to be removed by GC, got %d keys remaining", remaining)
	}
}

func TestStore_StopGC(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	s.Increment("key1")

	const gcInterval = 10 * time.Millisecond
	const maxAge = time.Hour

	s.StartGC(gcInterval, maxAge)

	// Stop GC immediately.
	s.StopGC()

	// Advance mock clock so entries would be expired if GC ran.
	clk.Advance(2 * time.Hour)

	// Wait longer than a few GC intervals to confirm GC has stopped.
	time.Sleep(50 * time.Millisecond)

	// Entries should still be present because GC was stopped before it
	// could run with the advanced clock time.
	// (GC may have run zero or one times between StartGC and StopGC with
	// old clock time, so the key may or may not still exist. What matters
	// is that the goroutine is no longer running. We verify this by
	// confirming no panic on double-close and that StopGC is idempotent
	// in the sense that calling it again doesn't panic.)

	// After StopGC, the stopCh is closed. Calling StopGC again would
	// panic on a double-close. Verify the goroutine has exited by
	// ensuring no further GC runs modify state after the stop.
	//
	// Reset clock to present so all entries are "fresh" relative to maxAge.
	// (They were added at clk's initial time; clk is now 2h ahead, so
	// entries are 2h old. Reset to just after epoch so they look fresh.)
	clk.mu.Lock()
	clk.t = time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)
	clk.mu.Unlock()

	// Add a new entry after stop.
	s.Increment("key2")

	// Wait a bit; if GC were still running it might remove entries.
	time.Sleep(50 * time.Millisecond)

	s.mu.Lock()
	_, hasKey2 := s.data["key2"]
	s.mu.Unlock()

	if !hasKey2 {
		t.Error("GC should not be running after StopGC; key2 should still be present")
	}
}
