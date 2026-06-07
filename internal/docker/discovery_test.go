package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/scrape"
)

type stubLister struct {
	containers []Container
}

func (s stubLister) ListContainers(_ context.Context) ([]Container, error) {
	return s.containers, nil
}

type erroringLister struct{ err error }

func (e erroringLister) ListContainers(_ context.Context) ([]Container, error) {
	return nil, e.err
}

func TestDiscoveryToTargetHappyPath(t *testing.T) {
	d := NewDiscovery(stubLister{}, DiscoveryOptions{
		Prefix: "owl.scrape", DefaultInterval: 15 * time.Second, DefaultTimeout: 10 * time.Second,
	})
	ct := Container{
		Names: []string{"/traefik"},
		Image: "traefik:v3",
		Labels: map[string]string{
			"owl.scrape":          "true",
			"owl.scrape.port":     "8082",
			"owl.scrape.path":     "/metrics",
			"owl.scrape.interval": "5s",
		},
	}
	got, ok := d.toTarget(ct)
	if !ok {
		t.Fatal("toTarget returned not-ok")
	}
	if got.URL != "http://traefik:8082/metrics" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Interval != 5*time.Second {
		t.Errorf("Interval = %v", got.Interval)
	}
	if got.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v (want default 10s)", got.Timeout)
	}
	if got.Labels["job"] != "traefik" {
		t.Errorf("job label = %q", got.Labels["job"])
	}
}

func TestDiscoverySkipsContainersWithoutOptIn(t *testing.T) {
	d := NewDiscovery(stubLister{}, DiscoveryOptions{Prefix: "owl.scrape"})
	if _, ok := d.toTarget(Container{
		Names: []string{"/x"},
	}); ok {
		t.Error("container without labels should not be a target")
	}
	if _, ok := d.toTarget(Container{
		Names:  []string{"/x"},
		Labels: map[string]string{"owl.scrape": "true"}, // missing port
	}); ok {
		t.Error("container missing .port should not be a target")
	}
}

func TestDiscoveryDefaultsAndComposeService(t *testing.T) {
	d := NewDiscovery(stubLister{}, DiscoveryOptions{
		Prefix: "owl.scrape", DefaultInterval: 12 * time.Second, DefaultTimeout: 4 * time.Second,
	})
	ct := Container{
		Names: []string{"/owl-traefik-1"},
		Labels: map[string]string{
			"owl.scrape":                 "true",
			"owl.scrape.port":            "8082",
			"com.docker.compose.service": "traefik",
		},
	}
	got, ok := d.toTarget(ct)
	if !ok {
		t.Fatal("expected target")
	}
	if got.URL != "http://owl-traefik-1:8082/metrics" {
		t.Errorf("URL default path failed: %q", got.URL)
	}
	if got.Interval != 12*time.Second {
		t.Errorf("Interval = %v (want default 12s)", got.Interval)
	}
	if got.Labels["job"] != "traefik" {
		t.Errorf("job derived from compose.service = %q", got.Labels["job"])
	}
}

func TestDiscoveryHealthSnapshotIsPendingBeforeFirstScan(t *testing.T) {
	d := NewDiscovery(stubLister{}, DiscoveryOptions{Prefix: "owl.scrape", Interval: 7 * time.Second})
	h := d.HealthSnapshot()
	if !h.LastScan.IsZero() {
		t.Errorf("LastScan = %v, want zero", h.LastScan)
	}
	if h.Interval != 7*time.Second {
		t.Errorf("Interval = %v, want 7s", h.Interval)
	}
	if h.ContainersSeen != 0 || h.OptedIn != 0 || h.LastError != "" {
		t.Errorf("expected empty health, got %+v", h)
	}
}

func TestDiscoveryHealthSnapshotRecordsSuccess(t *testing.T) {
	d := NewDiscovery(stubLister{containers: []Container{
		{Names: []string{"/in"}, Labels: map[string]string{"owl.scrape": "true", "owl.scrape.port": "9090"}},
		{Names: []string{"/out"}, Labels: map[string]string{"unrelated": "1"}},
		{Names: []string{"/missing-port"}, Labels: map[string]string{"owl.scrape": "true"}},
	}}, DiscoveryOptions{Prefix: "owl.scrape", Interval: time.Second})

	if _, err := d.scan(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}

	h := d.HealthSnapshot()
	if h.LastScan.IsZero() {
		t.Error("LastScan should be set after scan")
	}
	if h.LastError != "" {
		t.Errorf("LastError = %q, want empty", h.LastError)
	}
	if h.ContainersSeen != 3 {
		t.Errorf("ContainersSeen = %d, want 3", h.ContainersSeen)
	}
	if h.OptedIn != 1 {
		t.Errorf("OptedIn = %d, want 1 (the one with port; missing-port is opted in but skipped)", h.OptedIn)
	}
	if h.Duration < 0 {
		t.Errorf("Duration = %v", h.Duration)
	}
}

func TestDiscoveryHealthSnapshotRecordsError(t *testing.T) {
	d := NewDiscovery(erroringLister{err: errors.New("socket closed")}, DiscoveryOptions{Prefix: "owl.scrape"})
	if _, err := d.scan(context.Background()); err == nil {
		t.Fatal("scan should have errored")
	}
	h := d.HealthSnapshot()
	if h.LastError == "" {
		t.Error("LastError should be set")
	}
	if h.OptedIn != 0 {
		t.Errorf("OptedIn = %d, want 0 on error", h.OptedIn)
	}
}

func TestDiscoveryRunEmitsAtLeastOneSnapshot(t *testing.T) {
	d := NewDiscovery(stubLister{containers: []Container{
		{Names: []string{"/x"}, Labels: map[string]string{"owl.scrape": "true", "owl.scrape.port": "9090"}},
	}}, DiscoveryOptions{Prefix: "owl.scrape", Interval: 10 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	out := make(chan []scrape.Target, 4)
	go d.Run(ctx, out)

	select {
	case got := <-out:
		if len(got) != 1 {
			t.Fatalf("snapshot len = %d, want 1", len(got))
		}
	case <-time.After(time.Second):
		t.Fatal("no snapshot received")
	}
}
