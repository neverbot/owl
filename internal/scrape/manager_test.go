package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerScrapesAllTargetsRepeatedly(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("up 1\n"))
	}))
	defer ts.Close()

	app := &fakeAppender{}
	mgr := NewManager(app)
	mgr.Set([]Target{
		{Name: "t1", URL: ts.URL, Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	mgr.Run(ctx)

	got := atomic.LoadInt32(&hits)
	if got < 2 {
		t.Errorf("hits = %d, want >= 2 in 100 ms with interval 20 ms", got)
	}
	if len(app.snapshot()) == 0 {
		t.Error("manager produced no samples")
	}
}

func TestManagerReconcilesOnSet(t *testing.T) {
	var hitsA, hitsB int32
	tsA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsA, 1)
		_, _ = w.Write([]byte("a 1\n"))
	}))
	defer tsA.Close()
	tsB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitsB, 1)
		_, _ = w.Write([]byte("b 1\n"))
	}))
	defer tsB.Close()

	app := &fakeAppender{}
	mgr := NewManager(app)
	mgr.Set([]Target{
		{Name: "a", URL: tsA.URL, Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() { mgr.Run(ctx); close(done) }()

	time.Sleep(40 * time.Millisecond)
	mgr.Set([]Target{
		{Name: "b", URL: tsB.URL, Interval: 20 * time.Millisecond, Timeout: 100 * time.Millisecond},
	})

	<-done

	if atomic.LoadInt32(&hitsA) == 0 {
		t.Error("target A was never hit")
	}
	if atomic.LoadInt32(&hitsB) == 0 {
		t.Error("target B was never hit after reconciliation")
	}
}

func TestHealthSnapshotRecordsScrapeOutcome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("up 1\nrequests_total 42\n"))
	}))
	defer srv.Close()

	app := &fakeAppender{}
	mgr := NewManager(app)
	mgr.Set([]Target{
		{Name: "ok", URL: srv.URL, Interval: 20 * time.Millisecond, Timeout: 200 * time.Millisecond},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	mgr.Run(ctx)

	h := mgr.HealthSnapshot()
	if len(h) != 1 {
		t.Fatalf("len(health) = %d, want 1", len(h))
	}
	if h[0].Name != "ok" {
		t.Errorf("name = %q", h[0].Name)
	}
	if h[0].LastError != "" {
		t.Errorf("expected success, got error: %q", h[0].LastError)
	}
	if h[0].LastSamples != 2 {
		t.Errorf("samples = %d, want 2", h[0].LastSamples)
	}
	if h[0].LastScrape.IsZero() {
		t.Error("last_scrape should be set")
	}
	if h[0].Duration <= 0 {
		t.Errorf("duration = %v, want > 0", h[0].Duration)
	}
}

func TestHealthSnapshotRecordsErrors(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()

	mgr := NewManager(&fakeAppender{})
	mgr.Set([]Target{
		{Name: "broken", URL: bad.URL, Interval: 20 * time.Millisecond, Timeout: 200 * time.Millisecond},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	mgr.Run(ctx)

	h := mgr.HealthSnapshot()
	if len(h) != 1 || h[0].LastError == "" {
		t.Fatalf("expected one entry with LastError set, got %+v", h)
	}
	if h[0].LastSamples != 0 {
		t.Errorf("samples on error = %d, want 0", h[0].LastSamples)
	}
}
