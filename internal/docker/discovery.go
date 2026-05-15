package docker

import (
	"context"
	"log"
	"time"

	"github.com/neverbot/owl/internal/scrape"
)

// Discovery polls the Docker daemon for containers carrying scrape
// labels and produces []scrape.Target snapshots. The labels read on
// each container, all prefixed with the configured prefix
// (default "owl.scrape"):
//
//	owl.scrape          (required, must be "true" to opt-in)
//	owl.scrape.port     (required, target's TCP port inside the container network)
//	owl.scrape.path     (optional, defaults to "/metrics")
//	owl.scrape.interval (optional, Go duration; falls back to the scraper's default)
//	owl.scrape.timeout  (optional, Go duration; falls back to the scraper's default)
//	owl.scrape.job      (optional, sets the "job" label on samples;
//	                     defaults to com.docker.compose.service or the container name)
//
// The URL is constructed as http://<container_name>:<port><path>.
// Container names are resolvable via Docker's embedded DNS when owl and
// the target share a Docker network.
type Discovery struct {
	client          ContainerLister
	prefix          string
	interval        time.Duration
	defaultInterval time.Duration
	defaultTimeout  time.Duration
}

// ContainerLister is the slice of the Docker client surface the
// discovery loop needs. Defined as an interface so tests can supply a
// fake.
type ContainerLister interface {
	ListContainers(ctx context.Context) ([]Container, error)
}

// DiscoveryOptions configures a Discovery instance.
type DiscoveryOptions struct {
	Prefix          string        // e.g. "owl.scrape"
	Interval        time.Duration // poll cadence; default 30 s
	DefaultInterval time.Duration // per-target fallback scrape interval
	DefaultTimeout  time.Duration // per-target fallback scrape timeout
}

// NewDiscovery wires a Discovery instance.
func NewDiscovery(client ContainerLister, opt DiscoveryOptions) *Discovery {
	if opt.Prefix == "" {
		opt.Prefix = "owl.scrape"
	}
	if opt.Interval <= 0 {
		opt.Interval = 30 * time.Second
	}
	return &Discovery{
		client:          client,
		prefix:          opt.Prefix,
		interval:        opt.Interval,
		defaultInterval: opt.DefaultInterval,
		defaultTimeout:  opt.DefaultTimeout,
	}
}

// Run blocks until ctx is cancelled, scanning the daemon on each tick
// and emitting target snapshots on the returned channel. The channel
// is closed when Run returns.
//
// Snapshots are emitted every interval regardless of whether the set
// changed; downstream consumers (scrape.Manager) deduplicate cheaply
// via their revision counter.
func (d *Discovery) Run(ctx context.Context, out chan<- []scrape.Target) {
	defer close(out)
	send := func() {
		targets, err := d.scan(ctx)
		if err != nil {
			log.Printf("docker discovery: %v", err)
			return
		}
		select {
		case out <- targets:
		case <-ctx.Done():
		}
	}
	send()
	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send()
		}
	}
}

// scan performs one daemon list and converts opted-in containers to
// scrape.Target values.
func (d *Discovery) scan(ctx context.Context) ([]scrape.Target, error) {
	containers, err := d.client.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]scrape.Target, 0)
	for _, ct := range containers {
		t, ok := d.toTarget(ct)
		if !ok {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// toTarget builds a scrape.Target from a container, or returns ok=false
// if the container did not opt in via the prefix label.
func (d *Discovery) toTarget(ct Container) (scrape.Target, bool) {
	lbls := ct.Labels
	if lbls[d.prefix] != "true" {
		return scrape.Target{}, false
	}
	port := lbls[d.prefix+".port"]
	if port == "" {
		log.Printf("docker discovery: container %s opted in but missing %s.port", ct.Name(), d.prefix)
		return scrape.Target{}, false
	}
	path := lbls[d.prefix+".path"]
	if path == "" {
		path = "/metrics"
	} else if path[0] != '/' {
		path = "/" + path
	}

	interval := parseDur(lbls[d.prefix+".interval"], d.defaultInterval)
	timeout := parseDur(lbls[d.prefix+".timeout"], d.defaultTimeout)

	job := lbls[d.prefix+".job"]
	if job == "" {
		job = lbls["com.docker.compose.service"]
	}
	if job == "" {
		job = ct.Name()
	}

	return scrape.Target{
		Name:     "docker:" + ct.Name(),
		URL:      "http://" + ct.Name() + ":" + port + path,
		Interval: interval,
		Timeout:  timeout,
		Labels: map[string]string{
			"job":      job,
			"instance": ct.Name(),
		},
	}, true
}

// parseDur parses a Go duration with a fallback when the value is
// empty or malformed. Empty fallback returns zero, which lets the
// scraper apply its own configured default.
func parseDur(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// IntervalSeconds is exposed for diagnostics; the scrape manager picks
// the per-target interval directly from Target.Interval.
func (d *Discovery) IntervalSeconds() int64 {
	return int64(d.interval / time.Second)
}
