package docker

import (
	"context"
	"log"
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
	return &Collector{client: client, app: app, interval: interval}
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
// parallel, and appends the merged batch.
func (c *Collector) CollectOnce(ctx context.Context) {
	containers, err := c.client.ListContainers(ctx)
	if err != nil {
		log.Printf("docker collector: list: %v", err)
		return
	}
	if len(containers) == 0 {
		return
	}

	now := time.Now().UnixMilli()
	type result struct {
		samples []storage.Sample
	}
	results := make(chan result, len(containers))

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
				log.Printf("docker collector: stats %s: %v", ct.Name(), err)
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
		return
	}
	if err := c.app.Append(batch); err != nil {
		log.Printf("docker collector: append: %v", err)
	}
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
		// Memory usage minus cache, matching cAdvisor's working set.
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
