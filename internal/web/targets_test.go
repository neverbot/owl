package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/scrape"
)

type fakeScrapeHealth struct {
	entries []scrape.TargetHealth
}

func (f *fakeScrapeHealth) HealthSnapshot() []scrape.TargetHealth {
	out := make([]scrape.TargetHealth, len(f.entries))
	copy(out, f.entries)
	return out
}

func TestAPITargetsReturnsHealthJSON(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	f := &fakeScrapeHealth{entries: []scrape.TargetHealth{
		{Name: "traefik", URL: "http://traefik:8082/metrics",
			Interval: 15 * time.Second, LastScrape: now, LastSamples: 42,
			Labels: map[string]string{"job": "traefik"}},
		{Name: "broken", URL: "http://x", Interval: 15 * time.Second,
			LastScrape: now, LastError: "boom"},
	}}
	s := NewServer(Options{Scrape: f})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Targets []scrape.TargetHealth
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(got.Targets))
	}
	if got.Targets[0].Name != "traefik" {
		t.Errorf("first target = %q", got.Targets[0].Name)
	}
	if got.Targets[1].LastError != "boom" {
		t.Errorf("error = %q", got.Targets[1].LastError)
	}
}

func TestAPITargetsReturns503WithoutScrape(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestTargetsViewRendersTable(t *testing.T) {
	f := &fakeScrapeHealth{entries: []scrape.TargetHealth{
		{Name: "traefik", URL: "http://traefik:8082/metrics",
			Interval: 15 * time.Second, LastScrape: time.Now(), LastSamples: 7,
			Duration: 25 * time.Millisecond,
			Labels:   map[string]string{"job": "traefik"}},
	}}
	s := NewServer(Options{Scrape: f})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.Bytes()
	for _, want := range [][]byte{
		[]byte(`scrape targets`),
		[]byte(`traefik`),
		[]byte(`http://traefik:8082/metrics`),
		[]byte(`t-status--ok`),
	} {
		if !bytes.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestTargetsViewEmptyState(t *testing.T) {
	s := NewServer(Options{Scrape: &fakeScrapeHealth{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	s.ServeHTTP(rec, req)
	if !bytes.Contains(rec.Body.Bytes(), []byte("No active scrape targets")) {
		t.Error("empty state message missing")
	}
}

type fakeCollectorsHealth struct {
	entries []CollectorHealth
}

func (f *fakeCollectorsHealth) CollectorsSnapshot() []CollectorHealth {
	out := make([]CollectorHealth, len(f.entries))
	copy(out, f.entries)
	return out
}

func TestAPITargetsIncludesCollectors(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	scr := &fakeScrapeHealth{entries: []scrape.TargetHealth{
		{Name: "owl-self", URL: "http://localhost:8080/metrics", LastScrape: now, LastSamples: 9},
	}}
	col := &fakeCollectorsHealth{entries: []CollectorHealth{
		{Name: "host", Kind: "host", Interval: 5 * time.Second,
			LastCollection: now, Duration: 3 * time.Millisecond, LastSamples: 50},
		{Name: "docker", Kind: "docker_metrics", Interval: 10 * time.Second,
			LastCollection: now, Duration: 7 * time.Millisecond, LastSamples: 22, Extra: "3 containers"},
	}}
	s := NewServer(Options{Scrape: scr, Collectors: col})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/targets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Targets    []scrape.TargetHealth `json:"targets"`
		Collectors []CollectorHealth     `json:"collectors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Targets) != 1 || got.Targets[0].Name != "owl-self" {
		t.Errorf("targets = %+v", got.Targets)
	}
	if len(got.Collectors) != 2 {
		t.Fatalf("collectors len = %d, want 2", len(got.Collectors))
	}
	if got.Collectors[0].Kind != "host" || got.Collectors[1].Extra != "3 containers" {
		t.Errorf("collectors = %+v", got.Collectors)
	}
}

func TestAPITargetsOmitsCollectorsWhenAbsent(t *testing.T) {
	s := NewServer(Options{Scrape: &fakeScrapeHealth{}})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/targets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"collectors"`)) {
		t.Errorf("collectors key should be omitted when nil, body=%s", rec.Body.String())
	}
}

func TestTargetsViewRendersCollectorsSection(t *testing.T) {
	now := time.Now()
	col := &fakeCollectorsHealth{entries: []CollectorHealth{
		{Name: "host", Kind: "host", Interval: 5 * time.Second,
			LastCollection: now, Duration: 3 * time.Millisecond, LastSamples: 50},
		{Name: "docker", Kind: "docker_metrics", Interval: 10 * time.Second,
			LastCollection: now, Duration: 7 * time.Millisecond, LastSamples: 22, Extra: "3 containers"},
	}}
	s := NewServer(Options{Scrape: &fakeScrapeHealth{}, Collectors: col})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/targets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.Bytes()
	for _, want := range [][]byte{
		[]byte("internal collectors"),
		[]byte("host"),
		[]byte("docker_metrics"),
		[]byte("3 containers"),
	} {
		if !bytes.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestTargetsViewHidesCollectorsSectionWhenAbsent(t *testing.T) {
	s := NewServer(Options{Scrape: &fakeScrapeHealth{}})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/targets", nil))
	if bytes.Contains(rec.Body.Bytes(), []byte("internal collectors")) {
		t.Error("collectors section should be hidden when none are wired")
	}
}
