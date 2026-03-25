package rate

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockClock is a controllable clock for testing.
type mockClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *mockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newMockClock() *mockClock {
	return &mockClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestCounter_Increment(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	s.Increment("key1")

	if got := s.Count("key1", time.Minute); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}
}

func TestCounter_MultipleIncrements(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	for i := 0; i < 5; i++ {
		s.Increment("key1")
	}

	if got := s.Count("key1", time.Minute); got != 5 {
		t.Errorf("Count() = %d, want 5", got)
	}
}

func TestCounter_WindowExpiry(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	s.Increment("key1")

	// Advance past the window
	clk.Advance(2 * time.Minute)

	if got := s.Count("key1", time.Minute); got != 0 {
		t.Errorf("Count() = %d, want 0 after window expiry", got)
	}
}

func TestCounter_SlidingWindow(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	// t=0: increment
	s.Increment("key1")

	// t=30s: increment
	clk.Advance(30 * time.Second)
	s.Increment("key1")

	// t=90s: increment; with 1m window, t=0 hit has expired
	clk.Advance(60 * time.Second)
	s.Increment("key1")

	// At t=90s with 1m window: t=0 is expired, t=30s and t=90s are within window
	if got := s.Count("key1", time.Minute); got != 2 {
		t.Errorf("Count() = %d, want 2 (t=0 should be expired)", got)
	}
}

func TestCounter_IndependentKeys(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	s.Increment("keyA")
	s.Increment("keyA")
	s.Increment("keyA")

	s.Increment("keyB")
	s.Increment("keyB")

	if got := s.Count("keyA", time.Minute); got != 3 {
		t.Errorf("Count(keyA) = %d, want 3", got)
	}
	if got := s.Count("keyB", time.Minute); got != 2 {
		t.Errorf("Count(keyB) = %d, want 2", got)
	}
}

func TestCounter_Concurrent(t *testing.T) {
	s := NewStore()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			s.Increment("shared-key")
		}()
	}

	wg.Wait()

	if got := s.Count("shared-key", time.Minute); got != goroutines {
		t.Errorf("Count() = %d, want %d after concurrent increments", got, goroutines)
	}
}

func TestStore_StopGC_Idempotent(t *testing.T) {
	s := NewStore()
	s.StartGC(time.Minute, 5*time.Minute)

	// Calling StopGC twice should not panic.
	s.StopGC()
	s.StopGC()
}

func TestStore_MaxKeys(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	// Fill to capacity.
	for i := 0; i < maxKeys; i++ {
		s.Increment(fmt.Sprintf("key-%d", i))
	}

	// New key beyond limit should be skipped.
	s.Increment("overflow-key")

	s.mu.Lock()
	_, exists := s.data["overflow-key"]
	total := len(s.data)
	s.mu.Unlock()

	if exists {
		t.Error("expected overflow-key to be skipped when at maxKeys")
	}
	if total != maxKeys {
		t.Errorf("expected %d keys, got %d", maxKeys, total)
	}

	// Existing key should still work.
	s.Increment("key-0")
	if got := s.Count("key-0", time.Minute); got != 2 {
		t.Errorf("expected 2 for existing key, got %d", got)
	}
}

func TestStore_OnKeyDropped(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	var dropped []string
	s.OnKeyDropped(func(key string) {
		dropped = append(dropped, key)
	})

	// Fill to capacity.
	for i := 0; i < maxKeys; i++ {
		s.Increment(fmt.Sprintf("key-%d", i))
	}

	if len(dropped) != 0 {
		t.Fatalf("expected no drops while filling, got %d", len(dropped))
	}

	// These should trigger the callback.
	s.Increment("overflow-1")
	s.Increment("overflow-2")

	if len(dropped) != 2 {
		t.Fatalf("expected 2 drops, got %d", len(dropped))
	}
	if dropped[0] != "overflow-1" || dropped[1] != "overflow-2" {
		t.Errorf("dropped = %v, want [overflow-1, overflow-2]", dropped)
	}
}

func TestStore_MaxTimestampsPerKey(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	// Fill a single key past the limit.
	for i := 0; i < maxTimestampsPerKey+100; i++ {
		s.Increment("flood")
	}

	s.mu.Lock()
	got := len(s.data["flood"])
	s.mu.Unlock()

	if got != maxTimestampsPerKey {
		t.Errorf("expected timestamps capped at %d, got %d", maxTimestampsPerKey, got)
	}
}

func TestCounter_GC(t *testing.T) {
	clk := newMockClock()
	s := NewStoreWithClock(clk)

	s.Increment("key1")
	s.Increment("key2")

	// Advance time so entries are old
	clk.Advance(5 * time.Minute)

	// Add a fresh entry for key2
	s.Increment("key2")

	// GC entries older than 2 minutes
	s.GC(2 * time.Minute)

	// key1 should be fully removed (all entries expired)
	s.mu.Lock()
	_, hasKey1 := s.data["key1"]
	_, hasKey2 := s.data["key2"]
	key2Len := len(s.data["key2"])
	s.mu.Unlock()

	if hasKey1 {
		t.Error("GC should have removed key1 entirely")
	}
	if !hasKey2 {
		t.Error("GC should have kept key2 (has a fresh entry)")
	}
	if key2Len != 1 {
		t.Errorf("key2 should have 1 entry after GC, got %d", key2Len)
	}
}
