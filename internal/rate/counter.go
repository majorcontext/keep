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
	mu    sync.Mutex
	data  map[string][]time.Time
	clock Clock
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
