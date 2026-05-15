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
