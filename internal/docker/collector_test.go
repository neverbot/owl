package docker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

type fakeDocker struct {
	containers []Container
	stats      map[string]*Stats
}

func (f *fakeDocker) ListContainers(_ context.Context) ([]Container, error) {
	return f.containers, nil
}
func (f *fakeDocker) ContainerStats(_ context.Context, id string) (*Stats, error) {
	return f.stats[id], nil
}

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

func TestCollectOnceEmitsContainerMetrics(t *testing.T) {
	d := &fakeDocker{
		containers: []Container{
			{
				ID:    "abc",
				Names: []string{"/owl"},
				Image: "owl:dev",
				State: "running",
				Labels: map[string]string{
					"com.docker.compose.service": "owl",
					"com.docker.compose.project": "owl-stack",
				},
			},
			{ID: "stopped", Names: []string{"/stopped"}, State: "exited"},
		},
		stats: map[string]*Stats{
			"abc": {
				CPUStats: CPUStats{CPUUsage: struct {
					TotalUsage  uint64   `json:"total_usage"`
					PercpuUsage []uint64 `json:"percpu_usage"`
				}{TotalUsage: 2_500_000_000}},
				Memory: MemoryStats{
					Usage:    10 * 1024 * 1024,
					MaxUsage: 12 * 1024 * 1024,
					Limit:    64 * 1024 * 1024,
					Stats:    map[string]uint64{"inactive_file": 2 * 1024 * 1024},
				},
				Networks: map[string]Net{
					"eth0": {RxBytes: 1000, TxBytes: 2000},
				},
				BlkIO: BlockIO{IoServiceBytesRecursive: []BlockIOEntry{
					{Op: "Read", Value: 100},
					{Op: "Write", Value: 200},
				}},
			},
		},
	}

	app := &fakeAppender{}
	c := NewCollector(d, app, time.Millisecond)
	c.CollectOnce(context.Background())

	got := app.snapshot()
	if len(got) == 0 {
		t.Fatal("no samples emitted")
	}

	idx := map[string]storage.Sample{}
	for _, s := range got {
		// First sample per metric is fine — single container in the test.
		if _, ok := idx[s.Metric]; !ok {
			idx[s.Metric] = s
		}
	}

	cpu, ok := idx["container_cpu_usage_seconds_total"]
	if !ok {
		t.Fatal("missing container_cpu_usage_seconds_total")
	}
	if cpu.Value != 2.5 {
		t.Errorf("cpu = %v, want 2.5", cpu.Value)
	}
	if cpu.Labels["name"] != "owl" {
		t.Errorf("name = %q, want owl", cpu.Labels["name"])
	}
	if cpu.Labels["compose_service"] != "owl" || cpu.Labels["compose_project"] != "owl-stack" {
		t.Errorf("compose labels missing: %+v", cpu.Labels)
	}

	mem := idx["container_memory_usage_bytes"]
	want := uint64(10*1024*1024 - 2*1024*1024)
	if mem.Value != float64(want) {
		t.Errorf("memory working_set = %v, want %d", mem.Value, want)
	}

	if _, ok := idx["container_network_receive_bytes_total"]; !ok {
		t.Error("missing network rx")
	}
	if _, ok := idx["container_fs_reads_bytes_total"]; !ok {
		t.Error("missing fs reads")
	}
}
