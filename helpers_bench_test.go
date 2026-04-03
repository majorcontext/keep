package keep

import "testing"

// BenchmarkSafeEvaluateOverhead compares Engine.Evaluate vs SafeEvaluate
// on the happy path to quantify the defer/recover overhead.
func BenchmarkSafeEvaluateOverhead(b *testing.B) {
	eng, err := LoadFromBytes([]byte(`
scope: bench
mode: enforce
rules:
  - name: block-deletes
    match:
      operation: "delete_*"
    action: deny
    message: "deletes not allowed"
`))
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()

	call := Call{Operation: "delete_issue"}

	b.Run("Engine.Evaluate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = eng.Evaluate(call, "bench")
		}
	})

	b.Run("SafeEvaluate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = SafeEvaluate(eng, call, "bench")
		}
	})
}
