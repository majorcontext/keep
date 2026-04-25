package judge_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/majorcontext/keep/judge"
)

// countingProvider tracks how many times Judge is called.
type countingProvider struct {
	calls   atomic.Int64
	verdict judge.Verdict
}

func (p *countingProvider) Judge(_ context.Context, _ judge.Request) (judge.Verdict, error) {
	p.calls.Add(1)
	return p.verdict, nil
}

// mustJudge calls cache.Judge and fatals on error.
func mustJudge(t *testing.T, cache judge.Provider, req judge.Request) judge.Verdict {
	t.Helper()
	v, err := cache.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	return v
}

func TestCacheHit(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Deny, Reason: "bad"}}
	cache := judge.NewCache(inner)

	req := judge.Request{Model: "haiku", Prompt: "safe?", Content: "hello"}

	// First call — miss.
	v1, err := cache.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if v1.Cached {
		t.Error("first call should not be cached")
	}
	if v1.Decision != judge.Deny {
		t.Errorf("Decision = %q, want deny", v1.Decision)
	}

	// Second call — hit.
	v2, err := cache.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !v2.Cached {
		t.Error("second call should be cached")
	}
	if v2.Decision != judge.Deny {
		t.Errorf("Decision = %q, want deny", v2.Decision)
	}
	if v2.Reason != "bad" {
		t.Errorf("Reason = %q, want %q", v2.Reason, "bad")
	}

	if inner.calls.Load() != 1 {
		t.Errorf("provider called %d times, want 1", inner.calls.Load())
	}
}

func TestCacheDifferentKeys(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Allow}}
	cache := judge.NewCache(inner)

	req1 := judge.Request{Model: "haiku", Prompt: "safe?", Content: "hello"}
	req2 := judge.Request{Model: "haiku", Prompt: "safe?", Content: "goodbye"}

	if _, err := cache.Judge(context.Background(), req1); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Judge(context.Background(), req2); err != nil {
		t.Fatal(err)
	}

	if inner.calls.Load() != 2 {
		t.Errorf("provider called %d times, want 2 (different content)", inner.calls.Load())
	}
}

func TestCacheEviction(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Allow}}
	// maxSize=3 so req3 evicts req1 (oldest) while req2 stays cached.
	cache := judge.NewCache(inner, judge.WithMaxSize(3))

	req1 := judge.Request{Model: "m", Prompt: "p", Content: "a"}
	req2 := judge.Request{Model: "m", Prompt: "p", Content: "b"}
	req3 := judge.Request{Model: "m", Prompt: "p", Content: "c"}
	req4 := judge.Request{Model: "m", Prompt: "p", Content: "d"}

	mustJudge(t, cache, req1) // 1 call
	mustJudge(t, cache, req2) // 2 calls
	mustJudge(t, cache, req3) // 3 calls
	mustJudge(t, cache, req4) // 4 calls, evicts req1

	// req1 should be evicted — re-calling it should trigger a new provider call.
	mustJudge(t, cache, req1) // 5 calls
	if inner.calls.Load() != 5 {
		t.Errorf("provider called %d times, want 5 (req1 evicted)", inner.calls.Load())
	}

	// req4 should still be cached (it was inserted most recently before req1 eviction).
	mustJudge(t, cache, req4)
	if inner.calls.Load() != 5 {
		t.Errorf("provider called %d times, want 5 (req4 cached)", inner.calls.Load())
	}
}

func TestCacheDoesNotCacheErrors(t *testing.T) {
	callCount := atomic.Int64{}
	errProvider := &errorThenSuccessProvider{calls: &callCount}
	cache := judge.NewCache(errProvider)

	req := judge.Request{Model: "m", Prompt: "p", Content: "c"}

	// First call errors.
	_, err := cache.Judge(context.Background(), req)
	if err == nil {
		t.Fatal("expected error on first call")
	}

	// Second call should retry (error not cached).
	v, err := cache.Judge(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if v.Decision != judge.Allow {
		t.Errorf("Decision = %q, want allow", v.Decision)
	}
	if callCount.Load() != 2 {
		t.Errorf("provider called %d times, want 2", callCount.Load())
	}
}

type errorThenSuccessProvider struct {
	calls *atomic.Int64
}

func (p *errorThenSuccessProvider) Judge(_ context.Context, _ judge.Request) (judge.Verdict, error) {
	n := p.calls.Add(1)
	if n == 1 {
		return judge.Verdict{}, context.DeadlineExceeded
	}
	return judge.Verdict{Decision: judge.Allow, Reason: "ok"}, nil
}

func TestCacheConcurrent(t *testing.T) {
	inner := &countingProvider{verdict: judge.Verdict{Decision: judge.Allow}}
	cache := judge.NewCache(inner)

	req := judge.Request{Model: "m", Prompt: "p", Content: "c"}

	// Prime the cache.
	mustJudge(t, cache, req)

	// Concurrent reads should all hit cache.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := cache.Judge(context.Background(), req)
			if err != nil {
				t.Errorf("concurrent call: %v", err)
			}
			if !v.Cached {
				t.Error("concurrent call should be cached")
			}
		}()
	}
	wg.Wait()

	if inner.calls.Load() != 1 {
		t.Errorf("provider called %d times, want 1", inner.calls.Load())
	}
}
