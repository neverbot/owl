package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

type erroringAppender struct{ err error }

func (e *erroringAppender) Append(_ []storage.Sample) error { return e.err }

type erroringDocker struct{ err error }

func (e *erroringDocker) ListContainers(_ context.Context) ([]Container, error) {
	return nil, e.err
}
func (e *erroringDocker) ContainerStats(_ context.Context, _ string) (*Stats, error) {
	return nil, e.err
}

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

func TestHealthSnapshotIsPendingBeforeFirstTick(t *testing.T) {
	c := NewCollector(&fakeDocker{}, &fakeAppender{}, 7*time.Second)
	h := c.HealthSnapshot()
	if !h.LastCollection.IsZero() {
		t.Errorf("LastCollection = %v, want zero", h.LastCollection)
	}
	if h.Interval != 7*time.Second {
		t.Errorf("Interval = %v, want 7s", h.Interval)
	}
	if h.ContainersSeen != 0 || h.LastSamples != 0 || h.LastError != "" {
		t.Errorf("expected empty health, got %+v", h)
	}
}

func TestHealthSnapshotRecordsSuccessfulCollection(t *testing.T) {
	d := &fakeDocker{
		containers: []Container{{ID: "abc", Names: []string{"/owl"}, State: "running"}},
		stats: map[string]*Stats{
			"abc": {Memory: MemoryStats{Usage: 1024}},
		},
	}
	c := NewCollector(d, &fakeAppender{}, time.Second)
	c.CollectOnce(context.Background())

	h := c.HealthSnapshot()
	if h.LastCollection.IsZero() {
		t.Error("LastCollection should be set after CollectOnce")
	}
	if h.LastError != "" {
		t.Errorf("LastError = %q, want empty", h.LastError)
	}
	if h.ContainersSeen != 1 {
		t.Errorf("ContainersSeen = %d, want 1", h.ContainersSeen)
	}
	if h.LastSamples == 0 {
		t.Errorf("LastSamples = %d, want > 0", h.LastSamples)
	}
	if h.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", h.Duration)
	}
}

func TestHealthSnapshotRecordsListError(t *testing.T) {
	c := NewCollector(&erroringDocker{err: errors.New("nope")}, &fakeAppender{}, time.Second)
	c.CollectOnce(context.Background())

	h := c.HealthSnapshot()
	if h.LastError == "" {
		t.Error("LastError should reflect ListContainers failure")
	}
	if h.LastSamples != 0 {
		t.Errorf("LastSamples = %d, want 0", h.LastSamples)
	}
}

func TestHealthSnapshotRecordsAppendError(t *testing.T) {
	d := &fakeDocker{
		containers: []Container{{ID: "abc", Names: []string{"/owl"}, State: "running"}},
		stats:      map[string]*Stats{"abc": {Memory: MemoryStats{Usage: 1024}}},
	}
	c := NewCollector(d, &erroringAppender{err: errors.New("disk full")}, time.Second)
	c.CollectOnce(context.Background())

	h := c.HealthSnapshot()
	if h.LastError == "" {
		t.Error("LastError should reflect Append failure")
	}
	if h.LastSamples != 0 {
		t.Errorf("LastSamples = %d, want 0 on error", h.LastSamples)
	}
	if h.ContainersSeen != 1 {
		t.Errorf("ContainersSeen = %d, want 1 (containers were seen, even though append failed)", h.ContainersSeen)
	}
}

func TestAnonMemoryPrefersCgroupV2(t *testing.T) {
	v, ok := anonMemory(MemoryStats{Stats: map[string]uint64{
		"anon":      42,
		"total_rss": 99,
	}})
	if !ok || v != 42 {
		t.Errorf("anon (cgroup v2): got (%d, %v), want (42, true)", v, ok)
	}
}

func TestAnonMemoryFallsBackToCgroupV1(t *testing.T) {
	v, ok := anonMemory(MemoryStats{Stats: map[string]uint64{
		"total_rss":     99,
		"inactive_file": 10,
	}})
	if !ok || v != 99 {
		t.Errorf("anon (cgroup v1): got (%d, %v), want (99, true)", v, ok)
	}
}

func TestAnonMemoryAbsentWhenStatsMissing(t *testing.T) {
	if v, ok := anonMemory(MemoryStats{Stats: map[string]uint64{}}); ok {
		t.Errorf("anon absent: got (%d, true), want (_, false)", v)
	}
}

func TestCollectOnceEmitsAnonMemoryWhenAvailable(t *testing.T) {
	d := &fakeDocker{
		containers: []Container{{ID: "abc", Names: []string{"/owl"}, State: "running"}},
		stats: map[string]*Stats{
			"abc": {
				Memory: MemoryStats{
					Usage: 10 * 1024 * 1024,
					Stats: map[string]uint64{"anon": 3 * 1024 * 1024},
				},
			},
		},
	}
	app := &fakeAppender{}
	c := NewCollector(d, app, time.Millisecond)
	c.CollectOnce(context.Background())

	for _, s := range app.snapshot() {
		if s.Metric == "container_memory_anon_bytes" {
			if s.Value != float64(3*1024*1024) {
				t.Errorf("anon = %v, want %d", s.Value, 3*1024*1024)
			}
			return
		}
	}
	t.Error("container_memory_anon_bytes not emitted")
}
