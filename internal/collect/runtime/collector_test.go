package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

type fakeAppender struct {
	mu      sync.Mutex
	batches [][]storage.Sample
}

func (f *fakeAppender) Append(samples []storage.Sample) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]storage.Sample, len(samples))
	copy(cp, samples)
	f.batches = append(f.batches, cp)
	return nil
}

func (f *fakeAppender) all() []storage.Sample {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.Sample
	for _, b := range f.batches {
		out = append(out, b...)
	}
	return out
}

func TestCollectOnceEmitsExpectedMetrics(t *testing.T) {
	app := &fakeAppender{}
	c := New(app, 10*time.Millisecond)
	c.CollectOnce()

	got := app.all()
	if len(got) == 0 {
		t.Fatalf("CollectOnce emitted no samples")
	}

	wanted := map[string]bool{
		"owl_runtime_goroutines":        false,
		"owl_runtime_alloc_bytes":       false,
		"owl_runtime_gc_pause_total_ms": false,
	}
	for _, s := range got {
		if _, ok := wanted[s.Metric]; ok {
			wanted[s.Metric] = true
		}
		if s.TS == 0 {
			t.Errorf("sample %q has zero TS", s.Metric)
		}
		if s.Labels["job"] != "owl" {
			t.Errorf("sample %q missing label job=owl: %+v", s.Metric, s.Labels)
		}
	}
	for name, seen := range wanted {
		if !seen {
			t.Errorf("expected metric %q not emitted", name)
		}
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	app := &fakeAppender{}
	c := New(app, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}

	if len(app.all()) == 0 {
		t.Error("Run terminated without emitting any sample")
	}
}
