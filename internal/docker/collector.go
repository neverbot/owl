package docker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

// Collector polls the Docker daemon for per-container stats and emits
// cAdvisor-compatible `container_*` metrics. Labels carried on every
// sample: name (container name without leading slash), image,
// compose_service, compose_project. Stopped containers are skipped.
type Collector struct {
	client   ContainerStatsLister
	app      storage.Appender
	interval time.Duration

	healthMu sync.RWMutex
	health   Health
}

// Health captures the most recent collection outcome for the docker
// container-metrics collector. Returned by HealthSnapshot for the
// /targets page and the /api/targets JSON endpoint.
type Health struct {
	// Interval is the configured period between collections.
	Interval time.Duration `json:"interval"`
	// LastCollection is the wall-clock time of the most recent
	// CollectOnce. Zero until the first tick fires.
	LastCollection time.Time `json:"last_collection,omitempty"`
	// Duration is how long the most recent CollectOnce took.
	Duration time.Duration `json:"duration,omitempty"`
	// LastError is the message from the most recent failed tick, or
	// empty on success. Set when ListContainers or Append fails;
	// per-container stats failures are logged but do not flip the
	// overall health here.
	LastError string `json:"last_error,omitempty"`
	// LastSamples is the number of samples appended by the most
	// recent successful tick (0 on error or when no running
	// container was visible).
	LastSamples int `json:"last_samples"`
	// ContainersSeen is the number of running containers that were
	// returned by ListContainers on the most recent tick (regardless
	// of whether stats collection then succeeded for each).
	ContainersSeen int `json:"containers_seen"`
}

// ContainerStatsLister is the slice of the Docker client surface the
// collector depends on. Defined as an interface so tests can supply a
// fake without touching the real socket.
type ContainerStatsLister interface {
	ListContainers(ctx context.Context) ([]Container, error)
	ContainerStats(ctx context.Context, id string) (*Stats, error)
}

// NewCollector wires the collector to a Docker client and a storage
// Appender. interval ≤ 0 falls back to 10 seconds.
func NewCollector(client ContainerStatsLister, app storage.Appender, interval time.Duration) *Collector {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Collector{
		client:   client,
		app:      app,
		interval: interval,
		health:   Health{Interval: interval},
	}
}

// HealthSnapshot returns a copy of the collector's current Health.
// Safe to call concurrently with Run.
func (c *Collector) HealthSnapshot() Health {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	return c.health
}

func (c *Collector) recordHealth(containersSeen, samples int, dur time.Duration, err error) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.health.LastCollection = time.Now()
	c.health.Duration = dur
	c.health.ContainersSeen = containersSeen
	if err != nil {
		c.health.LastError = err.Error()
		c.health.LastSamples = 0
		return
	}
	c.health.LastError = ""
	c.health.LastSamples = samples
}

// Run blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	c.CollectOnce(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.CollectOnce(ctx)
		}
	}
}

// CollectOnce lists running containers, fetches stats for each in
// parallel, and appends the merged batch. Updates the Health snapshot
// with the tick's outcome.
func (c *Collector) CollectOnce(ctx context.Context) {
	start := time.Now()
	containers, err := c.client.ListContainers(ctx)
	if err != nil {
		slog.Error("docker list failed", "err", err)
		c.recordHealth(0, 0, time.Since(start), err)
		return
	}

	running := 0
	for _, ct := range containers {
		if ct.State == "" || ct.State == "running" {
			running++
		}
	}

	if running == 0 {
		c.recordHealth(0, 0, time.Since(start), nil)
		return
	}

	now := time.Now().UnixMilli()
	type result struct {
		samples []storage.Sample
	}
	results := make(chan result, running)

	var wg sync.WaitGroup
	for _, ct := range containers {
		if ct.State != "" && ct.State != "running" {
			continue
		}
		wg.Add(1)
		go func(ct Container) {
			defer wg.Done()
			stats, err := c.client.ContainerStats(ctx, ct.ID)
			if err != nil {
				slog.Error("docker stats failed", "container", ct.Name(), "err", err)
				return
			}
			results <- result{samples: containerSamples(ct, stats, now)}
		}(ct)
	}
	go func() { wg.Wait(); close(results) }()

	var batch []storage.Sample
	for r := range results {
		batch = append(batch, r.samples...)
	}
	if len(batch) == 0 {
		c.recordHealth(running, 0, time.Since(start), nil)
		return
	}
	err = c.app.Append(batch)
	if err != nil {
		slog.Error("docker append failed", "err", err)
	}
	c.recordHealth(running, len(batch), time.Since(start), err)
}

// containerSamples translates one container's stats into samples.
func containerSamples(ct Container, st *Stats, now int64) []storage.Sample {
	base := baseLabels(ct)

	out := []storage.Sample{
		// CPU is reported in nanoseconds; convert to seconds.
		{
			Metric: "container_cpu_usage_seconds_total",
			Labels: base, TS: now,
			Value: float64(st.CPUStats.CPUUsage.TotalUsage) / 1e9,
		},
		// Memory usage minus inactive file cache, matching cAdvisor's
		// "working set". Note this still includes the active page
		// cache — for write-heavy containers like owl itself the
		// kernel parks dirty pages in this metric and it looks like
		// a steady leak. Use container_memory_anon_bytes for the
		// honest "process anonymous memory" reading.
		{
			Metric: "container_memory_usage_bytes",
			Labels: base, TS: now,
			Value: float64(workingSet(st.Memory)),
		},
		{
			Metric: "container_memory_max_usage_bytes",
			Labels: base, TS: now,
			Value: float64(st.Memory.MaxUsage),
		},
		{
			Metric: "container_memory_limit_bytes",
			Labels: base, TS: now,
			Value: float64(st.Memory.Limit),
		},
	}
	if anon, ok := anonMemory(st.Memory); ok {
		out = append(out, storage.Sample{
			Metric: "container_memory_anon_bytes",
			Labels: base, TS: now,
			Value: float64(anon),
		})
	}

	// Network: one (rx, tx) pair per interface.
	for iface, n := range st.Networks {
		lbl := withLabel(base, "interface", iface)
		out = append(out,
			storage.Sample{Metric: "container_network_receive_bytes_total", Labels: lbl, TS: now, Value: float64(n.RxBytes)},
			storage.Sample{Metric: "container_network_transmit_bytes_total", Labels: lbl, TS: now, Value: float64(n.TxBytes)},
		)
	}

	// Block I/O: sum of Read / Write across all devices, keep the
	// number of series small for now.
	var ioRead, ioWrite uint64
	for _, e := range st.BlkIO.IoServiceBytesRecursive {
		switch e.Op {
		case "Read", "read":
			ioRead += e.Value
		case "Write", "write":
			ioWrite += e.Value
		}
	}
	out = append(out,
		storage.Sample{Metric: "container_fs_reads_bytes_total", Labels: base, TS: now, Value: float64(ioRead)},
		storage.Sample{Metric: "container_fs_writes_bytes_total", Labels: base, TS: now, Value: float64(ioWrite)},
	)
	return out
}

// baseLabels builds the common label set carried by every sample of
// one container. Compose labels are surfaced as first-class labels so
// dashboards can group by service / project without parsing.
func baseLabels(ct Container) map[string]string {
	lbl := map[string]string{
		"job":   "docker",
		"name":  ct.Name(),
		"image": ct.Image,
	}
	if v := ct.Labels["com.docker.compose.service"]; v != "" {
		lbl["compose_service"] = v
	}
	if v := ct.Labels["com.docker.compose.project"]; v != "" {
		lbl["compose_project"] = v
	}
	return lbl
}

// withLabel returns a copy of in with k=v added. Avoids mutating the
// shared baseLabels map.
func withLabel(in map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for kk, vv := range in {
		out[kk] = vv
	}
	out[k] = v
	return out
}

// anonMemory returns the container's anonymous (non-file-cache)
// memory and a boolean indicating whether any of the candidate
// fields were present. cgroup v2 exposes this as "anon"; cgroup v1
// uses "total_rss" or "rss". Subtracting the file cache here gives
// the operator a metric that tracks the actual process footprint
// instead of conflating it with kernel-side page caching of files
// the container writes to.
func anonMemory(m MemoryStats) (uint64, bool) {
	for _, k := range []string{"anon", "total_rss", "rss"} {
		if v, ok := m.Stats[k]; ok {
			return v, true
		}
	}
	return 0, false
}

// workingSet mimics cAdvisor's working_set definition:
// usage - inactive_file (when present). Falls back to usage if the
// daemon didn't report inactive_file.
func workingSet(m MemoryStats) uint64 {
	inactiveFile := m.Stats["inactive_file"]
	if inactiveFile == 0 {
		inactiveFile = m.Stats["total_inactive_file"]
	}
	if inactiveFile > m.Usage {
		return m.Usage
	}
	return m.Usage - inactiveFile
}
