// Package rate provides an in-memory sliding window counter store
// for Keep's rateCount() CEL function.
package rate

import (
	"sync"
	"time"
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
	mu     sync.Mutex
	data   map[string][]time.Time
	clock  Clock
	stopCh chan struct{}
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

// Increment records a hit for the given key at the current time.
func (s *Store) Increment(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = append(s.data[key], s.clock.Now())
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
	s.stopCh = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.GC(maxAge)
			case <-s.stopCh:
				return
			}
		}
	}()
}

// StopGC stops the periodic garbage collection goroutine.
func (s *Store) StopGC() {
	if s.stopCh != nil {
		close(s.stopCh)
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
