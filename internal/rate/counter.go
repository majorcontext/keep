// Package rate provides an in-memory sliding window counter store
// for Keep's rateCount() CEL function.
package rate

import (
	"sync"
	"time"
)

const (
	// maxKeys is the maximum number of distinct keys in the store.
	maxKeys = 100_000
	// maxTimestampsPerKey is the maximum timestamps stored per key.
	maxTimestampsPerKey = 100_000
)

// Clock abstracts time for testing.
type Clock interface {
	Now() time.Time
}

// realClock uses time.Now.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Store is a thread-safe sliding window counter store.
type Store struct {
	mu           sync.Mutex
	data         map[string][]time.Time
	clock        Clock
	stopCh       chan struct{}
	stopOnce     sync.Once
	onKeyDropped func(key string) // optional callback when a key is dropped at capacity
}

// NewStore creates a new counter store using real time.
func NewStore() *Store {
	return &Store{
		data:  make(map[string][]time.Time),
		clock: realClock{},
	}
}

// NewStoreWithClock creates a store with a custom clock for testing.
func NewStoreWithClock(clock Clock) *Store {
	return &Store{
		data:  make(map[string][]time.Time),
		clock: clock,
	}
}

// OnKeyDropped sets a callback that is invoked (under lock) when a new key
// is dropped because the store has reached maxKeys capacity.
func (s *Store) OnKeyDropped(fn func(key string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onKeyDropped = fn
}

// Increment records a hit for the given key at the current time.
// If the store has reached maxKeys and the key is new, the increment is skipped.
// If a key has reached maxTimestampsPerKey, the oldest entries are trimmed.
func (s *Store) Increment(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Enforce key limit: skip new keys when at capacity.
	if _, exists := s.data[key]; !exists {
		if len(s.data) >= maxKeys {
			if s.onKeyDropped != nil {
				s.onKeyDropped(key)
			}
			return
		}
	}

	s.data[key] = append(s.data[key], s.clock.Now())

	// Enforce per-key timestamp limit: trim oldest entries.
	if len(s.data[key]) > maxTimestampsPerKey {
		excess := len(s.data[key]) - maxTimestampsPerKey
		s.data[key] = s.data[key][excess:]
	}
}

// Count returns the number of hits for key within the given window duration.
func (s *Store) Count(key string, window time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := s.clock.Now().Add(-window)
	timestamps := s.data[key]

	count := 0
	for _, ts := range timestamps {
		if !ts.Before(cutoff) {
			count++
		}
	}
	return count
}

// StartGC begins periodic garbage collection in a background goroutine.
// Runs every interval, removing entries older than maxAge.
// Call StopGC to stop the goroutine.
func (s *Store) StartGC(interval, maxAge time.Duration) {
	s.mu.Lock()
	if s.stopCh != nil {
		s.mu.Unlock()
		return // already running
	}
	s.stopCh = make(chan struct{})
	stopCh := s.stopCh
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.GC(maxAge)
			case <-stopCh:
				return
			}
		}
	}()
}

// StopGC stops the periodic garbage collection goroutine.
// It is safe to call multiple times.
func (s *Store) StopGC() {
	s.mu.Lock()
	ch := s.stopCh
	s.mu.Unlock()

	if ch != nil {
		s.stopOnce.Do(func() { close(ch) })
	}
}

// GC removes entries older than maxAge from all keys.
// Removes keys that become empty.
func (s *Store) GC(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := s.clock.Now().Add(-maxAge)

	for key, timestamps := range s.data {
		fresh := timestamps[:0]
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				fresh = append(fresh, ts)
			}
		}
		if len(fresh) == 0 {
			delete(s.data, key)
		} else {
			s.data[key] = fresh
		}
	}
}
