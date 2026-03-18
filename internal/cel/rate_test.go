package cel_test

import (
	"testing"
	"time"

	keepcel "github.com/majorcontext/keep/internal/cel"
	"github.com/majorcontext/keep/internal/rate"
)

// mockClock implements rate.Clock for testing.
type mockClock struct {
	t time.Time
}

func (m *mockClock) Now() time.Time { return m.t }

func TestRateCount_CEL(t *testing.T) {
	clk := &mockClock{t: time.Now()}
	store := rate.NewStoreWithClock(clk)

	// Pre-load 6 hits.
	for i := 0; i < 6; i++ {
		store.Increment("test:key")
	}

	env, err := keepcel.NewEnv(keepcel.WithRateStore(store))
	if err != nil {
		t.Fatalf("NewEnv() error: %v", err)
	}

	prog, err := env.Compile("rateCount('test:key', '1h') > 5")
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	got, err := prog.Eval(nil, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if !got {
		t.Error("expected true (6 > 5), got false")
	}
}

func TestRateCount_UnderLimit(t *testing.T) {
	clk := &mockClock{t: time.Now()}
	store := rate.NewStoreWithClock(clk)

	// Pre-load 3 hits.
	for i := 0; i < 3; i++ {
		store.Increment("test:key")
	}

	env, err := keepcel.NewEnv(keepcel.WithRateStore(store))
	if err != nil {
		t.Fatalf("NewEnv() error: %v", err)
	}

	prog, err := env.Compile("rateCount('test:key', '1h') > 5")
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	got, err := prog.Eval(nil, nil)
	if err != nil {
		t.Fatalf("Eval() error: %v", err)
	}
	if got {
		t.Error("expected false (3 > 5 is false), got true")
	}
}

func TestRateCount_WindowParsing(t *testing.T) {
	store := rate.NewStore()
	env, err := keepcel.NewEnv(keepcel.WithRateStore(store))
	if err != nil {
		t.Fatalf("NewEnv() error: %v", err)
	}

	windows := []string{"30s", "5m", "1h", "24h"}
	for _, w := range windows {
		expr := "rateCount('key', '" + w + "') >= 0"
		_, err := env.Compile(expr)
		if err != nil {
			t.Errorf("Compile(%q) unexpected error: %v", expr, err)
		}
	}
}

func TestRateCount_InvalidWindow(t *testing.T) {
	store := rate.NewStore()
	env, err := keepcel.NewEnv(keepcel.WithRateStore(store))
	if err != nil {
		t.Fatalf("NewEnv() error: %v", err)
	}

	// 25h exceeds the 24h max.
	prog, err := env.Compile("rateCount('key', '25h') >= 0")
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	_, err = prog.Eval(nil, nil)
	if err == nil {
		t.Error("expected error for window '25h' (exceeds 24h max), got nil")
	}
}

func TestRateCount_ZeroWindow(t *testing.T) {
	store := rate.NewStore()
	env, err := keepcel.NewEnv(keepcel.WithRateStore(store))
	if err != nil {
		t.Fatalf("NewEnv() error: %v", err)
	}

	// 0s is below the 1s minimum.
	prog, err := env.Compile("rateCount('key', '0s') >= 0")
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	_, err = prog.Eval(nil, nil)
	if err == nil {
		t.Error("expected error for window '0s' (below 1s min), got nil")
	}
}
