package host

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

func (f *fakeAppender) batchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

func TestCollectOnceEmitsSamplesFromAllParsers(t *testing.T) {
	app := &fakeAppender{}
	c := NewFromFS(app, makeProc(), time.Millisecond, func() int64 { return 1000 })
	c.CollectOnce()

	got := app.all()
	if app.batchCount() != 1 {
		t.Errorf("batchCount = %d, want 1 (one batch per CollectOnce)", app.batchCount())
	}

	want := []string{
		"node_cpu_seconds_total",
		"node_boot_time_seconds",
		"node_memory_MemTotal_bytes",
		"node_load1",
		"node_network_receive_bytes_total",
		"node_disk_read_bytes_total",
	}
	for _, metric := range want {
		found := false
		for _, s := range got {
			if s.Metric == metric {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("metric %q missing from batch", metric)
		}
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	app := &fakeAppender{}
	c := NewFromFS(app, makeProc(), 10*time.Millisecond, func() int64 { return 1000 })

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
	if app.batchCount() < 2 {
		t.Errorf("batchCount = %d, want at least 2 ticks in 50ms with 10ms interval", app.batchCount())
	}
}
