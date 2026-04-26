package judge

import (
	"context"
	"crypto/sha256"
	"sync"
)

const defaultMaxSize = 10_000

var _ Provider = (*Cache)(nil)

// Cache wraps a Provider with an in-memory verdict cache.
// Identical requests (same model, prompt, content) return cached verdicts
// without calling the underlying provider. Errors are not cached.
type Cache struct {
	provider Provider
	mu       sync.RWMutex
	entries  map[[32]byte]Verdict
	order    [][32]byte // insertion order for oldest-first eviction
	maxSize  int
}

// CacheOption configures a Cache.
type CacheOption func(*Cache)

// WithMaxSize sets the maximum number of cached verdicts.
// When exceeded, the oldest entries are evicted. Default: 10,000.
// Values less than 1 are clamped to 1.
func WithMaxSize(n int) CacheOption {
	return func(c *Cache) {
		if n < 1 {
			n = 1
		}
		c.maxSize = n
	}
}

// NewCache creates a caching wrapper around a Provider.
func NewCache(provider Provider, opts ...CacheOption) *Cache {
	c := &Cache{
		provider: provider,
		entries:  make(map[[32]byte]Verdict),
		maxSize:  defaultMaxSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Judge returns a cached verdict if available, otherwise calls the
// underlying provider and caches the result. Errors are not cached.
func (c *Cache) Judge(ctx context.Context, req Request) (Verdict, error) {
	key := cacheKey(req)

	c.mu.RLock()
	if v, ok := c.entries[key]; ok {
		c.mu.RUnlock()
		v.Cached = true
		return v, nil
	}
	c.mu.RUnlock()

	v, err := c.provider.Judge(ctx, req)
	if err != nil {
		return v, err
	}

	c.mu.Lock()
	// Guard against concurrent misses on the same key both inserting.
	if _, exists := c.entries[key]; !exists {
		// Evict oldest if at capacity.
		for len(c.entries) >= c.maxSize && len(c.order) > 0 {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.entries[key] = v
		c.order = append(c.order, key)
	}
	c.mu.Unlock()

	return v, nil
}

func cacheKey(req Request) [32]byte {
	h := sha256.New()
	h.Write([]byte(req.Model))
	h.Write([]byte{0})
	h.Write([]byte(req.Prompt))
	h.Write([]byte{0})
	h.Write([]byte(req.Content))
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}
