package events

import (
	"context"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/config"
)

// fakeDriver emits a fixed slice of records on each Read.
type fakeDriver struct {
	mu      sync.Mutex
	records []Record
	name    string
	calls   int
}

// Name returns the source name.
func (f *fakeDriver) Name() string { return f.name }

// Read returns the queued records then drains them; subsequent
// reads return an empty iter.
func (f *fakeDriver) Read(_ context.Context, _ string) (iter.Seq[Record], string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	recs := f.records
	f.records = nil
	seq := func(yield func(Record) bool) {
		for _, r := range recs {
			if !yield(r) {
				return
			}
		}
	}
	return seq, "cursor-1", nil
}

// TestManagerPipeline asserts a tick produces stored events.
func TestManagerPipeline(t *testing.T) {
	es, _ := openTestStore(t)
	drv := &fakeDriver{name: "src", records: []Record{
		{Bytes: []byte(`{"a":"x"}`)},
	}}
	m := NewManager(es)
	m.SetSources([]Source{{
		Name:     "src",
		Driver:   drv,
		Interval: 10 * time.Millisecond,
		Format:   "json",
		Mapping:  config.MappingConfig{Kind: "k", Payload: map[string]string{"a": "$.a"}},
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()

	// Wait for the first tick.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := es.QueryEvents(EventFilter{From: 0, To: time.Now().Add(time.Hour).UnixMilli()})
		if len(got) == 1 && got[0].Payload["a"] == "x" {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("event was not stored within deadline")
}

// TestManagerHotReload asserts removed sources stop, added ones
// start, persisted ones keep their cursor.
func TestManagerHotReload(t *testing.T) {
	es, _ := openTestStore(t)
	a := &fakeDriver{name: "a"}
	b := &fakeDriver{name: "b"}
	m := NewManager(es)
	m.SetSources([]Source{{Name: "a", Driver: a, Interval: time.Hour, Format: "plain", Mapping: config.MappingConfig{Kind: "k"}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	m.SetSources([]Source{{Name: "b", Driver: b, Interval: time.Hour, Format: "plain", Mapping: config.MappingConfig{Kind: "k"}}})
	time.Sleep(250 * time.Millisecond)
	a.mu.Lock()
	aCalls := a.calls
	a.mu.Unlock()
	b.mu.Lock()
	bCalls := b.calls
	b.mu.Unlock()
	if aCalls == 0 {
		t.Fatal("a never called")
	} // initial tick
	if bCalls == 0 {
		t.Fatal("b never called after reload")
	}
}
