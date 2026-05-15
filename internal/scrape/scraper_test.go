package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

type fakeAppender struct {
	mu sync.Mutex
	in []storage.Sample
}

func (f *fakeAppender) Append(samples []storage.Sample) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, samples...)
	return nil
}

func (f *fakeAppender) snapshot() []storage.Sample {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.Sample, len(f.in))
	copy(out, f.in)
	return out
}

func TestScrapeOnceWritesSamples(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# HELP m gauge\n# TYPE m gauge\nfoo 1\nbar{job=\"x\"} 2\n"))
	}))
	defer ts.Close()

	app := &fakeAppender{}
	tgt := Target{Name: "demo", URL: ts.URL, Timeout: time.Second, Labels: map[string]string{"job": "demo"}}

	if err := ScrapeOnce(context.Background(), tgt, app); err != nil {
		t.Fatalf("ScrapeOnce: %v", err)
	}

	got := app.snapshot()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, s := range got {
		if s.TS == 0 {
			t.Errorf("sample missing TS: %+v", s)
		}
		if s.Labels["job"] != "demo" && s.Labels["job"] != "x" {
			t.Errorf("sample %q has unexpected labels %+v", s.Metric, s.Labels)
		}
		if _, ok := s.Labels["instance"]; !ok {
			t.Errorf("sample %q missing instance label: %+v", s.Metric, s.Labels)
		}
	}
}

func TestScrapeOnceErrorOnNon200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	tgt := Target{Name: "bad", URL: ts.URL, Timeout: time.Second}
	err := ScrapeOnce(context.Background(), tgt, &fakeAppender{})
	if err == nil {
		t.Error("expected error on 500 response")
	}
}

func TestScrapeOnceErrorOnTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("foo 1\n"))
	}))
	defer ts.Close()

	tgt := Target{Name: "slow", URL: ts.URL, Timeout: 50 * time.Millisecond}
	err := ScrapeOnce(context.Background(), tgt, &fakeAppender{})
	if err == nil {
		t.Error("expected timeout error")
	}
}
