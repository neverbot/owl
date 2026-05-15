package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
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

	if _, err := ScrapeOnce(context.Background(), tgt, app); err != nil {
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
	_, err := ScrapeOnce(context.Background(), tgt, &fakeAppender{})
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
	_, err := ScrapeOnce(context.Background(), tgt, &fakeAppender{})
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestScrapeOnceKeepFilter(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("apples_total 1\noranges_total 2\nbananas_total 3\n"))
	}))
	defer ts.Close()

	app := &fakeAppender{}
	tgt := Target{
		Name: "fruit", URL: ts.URL, Timeout: time.Second,
		Keep: []*regexp.Regexp{regexp.MustCompile(`^(apples|bananas)_total$`)},
	}
	n, err := ScrapeOnce(context.Background(), tgt, app)
	if err != nil {
		t.Fatalf("ScrapeOnce: %v", err)
	}
	if n != 2 {
		t.Fatalf("appended = %d, want 2 (apples, bananas)", n)
	}
	names := []string{}
	for _, s := range app.snapshot() {
		names = append(names, s.Metric)
	}
	for _, want := range []string{"apples_total", "bananas_total"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q in batch, got %v", want, names)
		}
	}
	for _, name := range names {
		if name == "oranges_total" {
			t.Errorf("oranges_total should have been filtered out")
		}
	}
}

func TestScrapeOnceDropFilter(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("apples_total 1\noranges_total 2\nbananas_total 3\n"))
	}))
	defer ts.Close()

	app := &fakeAppender{}
	tgt := Target{
		Name: "fruit", URL: ts.URL, Timeout: time.Second,
		Drop: []*regexp.Regexp{regexp.MustCompile(`^oranges_`)},
	}
	n, err := ScrapeOnce(context.Background(), tgt, app)
	if err != nil {
		t.Fatalf("ScrapeOnce: %v", err)
	}
	if n != 2 {
		t.Fatalf("appended = %d, want 2", n)
	}
}

func TestScrapeOnceKeepAndDropCombined(t *testing.T) {
	// Keep matches all three, drop then removes bananas → 2 survive.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("apples_total 1\noranges_total 2\nbananas_total 3\n"))
	}))
	defer ts.Close()

	app := &fakeAppender{}
	tgt := Target{
		Name: "fruit", URL: ts.URL, Timeout: time.Second,
		Keep: []*regexp.Regexp{regexp.MustCompile(`_total$`)},
		Drop: []*regexp.Regexp{regexp.MustCompile(`^bananas_`)},
	}
	n, err := ScrapeOnce(context.Background(), tgt, app)
	if err != nil {
		t.Fatalf("ScrapeOnce: %v", err)
	}
	if n != 2 {
		t.Fatalf("appended = %d, want 2 (apples + oranges)", n)
	}
}

func TestKeepMetricSemantics(t *testing.T) {
	keep := []*regexp.Regexp{regexp.MustCompile(`^foo$`)}
	drop := []*regexp.Regexp{regexp.MustCompile(`^bar$`)}
	cases := []struct {
		name string
		k, d []*regexp.Regexp
		in   string
		want bool
	}{
		{"empty filters keep everything", nil, nil, "anything", true},
		{"keep matches", keep, nil, "foo", true},
		{"keep does not match", keep, nil, "baz", false},
		{"drop matches", nil, drop, "bar", false},
		{"drop does not match", nil, drop, "foo", true},
		{"keep matches then drop removes", keep, []*regexp.Regexp{regexp.MustCompile(`^foo$`)}, "foo", false},
	}
	for _, tc := range cases {
		if got := keepMetric(tc.in, tc.k, tc.d); got != tc.want {
			t.Errorf("%s: keepMetric(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}
