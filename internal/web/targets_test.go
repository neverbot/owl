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
		[]byte(`metric sources`),
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

type fakeContainersHealth struct {
	entries []ContainerInfo
}

func (f *fakeContainersHealth) ContainersSnapshot() []ContainerInfo {
	out := make([]ContainerInfo, len(f.entries))
	copy(out, f.entries)
	return out
}

func TestAPITargetsIncludesContainers(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	containers := &fakeContainersHealth{entries: []ContainerInfo{
		{Name: "owl", Image: "owl:dev", ComposeService: "owl", ComposeProject: "stack",
			MemoryWorkingSetBytes: 12 * 1024 * 1024, LastSeen: now},
	}}
	s := NewServer(Options{Scrape: &fakeScrapeHealth{}, Containers: containers})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/targets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Containers []ContainerInfo `json:"containers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Containers) != 1 || got.Containers[0].Name != "owl" {
		t.Errorf("containers = %+v", got.Containers)
	}
	if got.Containers[0].MemoryWorkingSetBytes != 12*1024*1024 {
		t.Errorf("memory = %d", got.Containers[0].MemoryWorkingSetBytes)
	}
}

func TestTargetsViewRendersContainersSection(t *testing.T) {
	now := time.Now()
	containers := &fakeContainersHealth{entries: []ContainerInfo{
		{Name: "owl", Image: "owl:dev", ComposeService: "owl", ComposeProject: "stack",
			MemoryWorkingSetBytes: 12 * 1024 * 1024, LastSeen: now},
		{Name: "traefik", Image: "traefik:v3", MemoryWorkingSetBytes: 256 * 1024 * 1024, LastSeen: now},
	}}
	s := NewServer(Options{Scrape: &fakeScrapeHealth{}, Containers: containers})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/targets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.Bytes()
	for _, want := range [][]byte{
		[]byte("containers"),
		[]byte("owl:dev"),
		[]byte("traefik:v3"),
		[]byte("owl · stack"),
		[]byte("12 MiB"),
		[]byte("256 MiB"),
	} {
		if !bytes.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestTargetsViewHidesContainersSectionWhenAbsent(t *testing.T) {
	s := NewServer(Options{Scrape: &fakeScrapeHealth{}})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/targets", nil))
	body := rec.Body.Bytes()
	// "containers" appears in tabs/labels sometimes; the discriminator
	// here is the page-hint line we render only when the section is on.
	if bytes.Contains(body, []byte("observed on the last docker tick")) {
		t.Error("containers section should be hidden when not wired")
	}
}

func TestHumanBytesFormatting(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{12 * 1024 * 1024, "12 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
