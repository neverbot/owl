package docker

import (
	"context"
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
