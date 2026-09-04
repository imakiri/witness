// Benchmarks for the hot path: a metric or log call in a loop.
//
// What they measure, and the numbers that shaped the design (Ryzen 5 7600X):
//
//	BenchmarkCount             293 ns/op   5 allocs   event built and delivered
//	BenchmarkCaller            146 ns/op   1 alloc    of which: finding the call site
//	BenchmarkCountFilteredOut   12 ns/op   0 allocs   observer declined the type
//
// Caller used to be 527 ns/4 allocs and dominated everything else, because it
// walked 16 frames to keep one and re-symbolised the same call site on every
// call. It now walks one frame and caches file:line by pc.
//
// The filtered case is the answer to a metric too hot to leave switched on:
// an observer that declines the type via core.EventTypeFilter is consulted
// before the stack walk, so the call costs a type assertion.
package witness

import (
	"context"
	"testing"

	"github.com/imakiri/witness/core"
)

type nopObserver struct{ n int }

func (o *nopObserver) Observe(e core.Event) { o.n++ }

func benchCtx(obs core.Observer) context.Context {
	ctx, _ := Instance(context.Background(), obs, "bench", "v1")
	return ctx
}

func BenchmarkCount(b *testing.B) {
	ctx := benchCtx(&nopObserver{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Count(ctx, "jobs_total", 1)
	}
}

func BenchmarkCountWithLabel(b *testing.B) {
	ctx := benchCtx(&nopObserver{})
	r := record{key: "queue", value: "settle"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Count(ctx, "jobs_total", 1, r)
	}
}

func BenchmarkCountNilObserver(b *testing.B) {
	ctx := benchCtx(core.NilObserver{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Count(ctx, "jobs_total", 1)
	}
}

func BenchmarkCaller(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = core.Caller(1)
	}
}

func BenchmarkObserveOnly(b *testing.B) {
	c := core.From(benchCtx(&nopObserver{}))
	at := core.Caller(0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Observe(core.EventTypeMetricCounter(), "jobs_total", at)
	}
}

// metricsOnly is a core.EventTypeFilter that takes counters and nothing else.
type metricsOnly struct{ nopObserver }

func (metricsOnly) Accepts(et core.EventType) bool {
	return et == core.EventTypeMetricCounter()
}

func BenchmarkCountFilteredOut(b *testing.B) {
	ctx := benchCtx(&struct{ metricsOnly }{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Info(ctx, "not wanted")
	}
}

func BenchmarkCountAccepted(b *testing.B) {
	ctx := benchCtx(&struct{ metricsOnly }{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Count(ctx, "jobs_total", 1)
	}
}
