// Package runtime emits self-metrics from the Go runtime so Owl can
// observe its own health from day one — no /proc or external collectors
// required. These metrics are tagged with job="owl".
package runtime

import (
	"context"
	"math"
	"runtime"
	rtmetrics "runtime/metrics"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

// Collector samples the Go runtime at a fixed interval and writes the
// results to the provided Appender.
type Collector struct {
	app      storage.Appender
	interval time.Duration
	labels   map[string]string
}

// New constructs a Collector. The labels attached to every sample are
// fixed to {"job":"owl"}.
func New(app storage.Appender, interval time.Duration) *Collector {
	return &Collector{
		app:      app,
		interval: interval,
		labels:   map[string]string{"job": "owl"},
	}
}

// Run blocks until ctx is cancelled, calling CollectOnce on each tick.
func (c *Collector) Run(ctx context.Context) {
	// Emit immediately on start so the first dashboard load has data.
	c.CollectOnce()

	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.CollectOnce()
		}
	}
}

// CollectOnce captures one snapshot of runtime metrics and appends it.
func (c *Collector) CollectOnce() {
	now := time.Now().UnixMilli()

	// Goroutine count is cheap and stable.
	goroutines := float64(runtime.NumGoroutine())

	// Allocated bytes and GC pause total via runtime/metrics.
	samples := []rtmetrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/gc/pauses:seconds"},
	}
	rtmetrics.Read(samples)

	var allocBytes float64
	if samples[0].Value.Kind() == rtmetrics.KindUint64 {
		allocBytes = float64(samples[0].Value.Uint64())
	}

	var gcPauseTotalMs float64
	if samples[1].Value.Kind() == rtmetrics.KindFloat64Histogram {
		h := samples[1].Value.Float64Histogram()
		for i, count := range h.Counts {
			if i+1 >= len(h.Buckets) {
				break
			}
			lo, hi := h.Buckets[i], h.Buckets[i+1]
			// Skip buckets with infinite boundaries to avoid NaN/Inf values
			// that SQLite STRICT mode rejects as NOT NULL violations.
			if math.IsInf(lo, 0) || math.IsInf(hi, 0) {
				continue
			}
			mid := (lo + hi) / 2
			gcPauseTotalMs += float64(count) * mid * 1000.0
		}
	}
	if math.IsNaN(gcPauseTotalMs) || math.IsInf(gcPauseTotalMs, 0) {
		gcPauseTotalMs = 0
	}

	batch := []storage.Sample{
		{Metric: "owl_runtime_goroutines", Labels: c.labels, TS: now, Value: goroutines},
		{Metric: "owl_runtime_alloc_bytes", Labels: c.labels, TS: now, Value: allocBytes},
		{Metric: "owl_runtime_gc_pause_total_ms", Labels: c.labels, TS: now, Value: gcPauseTotalMs},
	}
	_ = c.app.Append(batch)
}
