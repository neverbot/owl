package host

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

// Collector periodically reads a curated subset of Linux /proc files
// and writes node_exporter-compatible samples to the provided Appender.
type Collector struct {
	app      storage.Appender
	proc     fs.FS
	interval time.Duration
	now      func() int64

	healthMu sync.RWMutex
	health   Health
}

// Health captures the most recent collection outcome for the host
// collector. Returned by HealthSnapshot for the /targets page and the
// /api/targets JSON endpoint.
type Health struct {
	// Interval is the configured period between collections.
	Interval time.Duration `json:"interval"`
	// LastCollection is the wall-clock time of the most recent
	// CollectOnce. Zero until the first tick fires.
	LastCollection time.Time `json:"last_collection,omitempty"`
	// Duration is how long the most recent CollectOnce took.
	Duration time.Duration `json:"duration,omitempty"`
	// LastError carries the storage Append error from the most
	// recent tick, or empty when the tick succeeded. Per-parser
	// failures are logged but not surfaced here so the collector's
	// overall health does not flap on a single missing /proc file.
	LastError string `json:"last_error,omitempty"`
	// LastSamples is the number of samples appended by the most
	// recent successful tick (0 on error or when no parser produced
	// output).
	LastSamples int `json:"last_samples"`
}

// Options configures a host Collector.
type Options struct {
	// ProcPath is the on-disk path that backs the Collector's view of
	// /proc. In a containerised deployment this is usually the path
	// where the host's /proc was bind-mounted (e.g. "/host/proc").
	ProcPath string
	// Interval between snapshots. Defaults to 5 s when zero.
	Interval time.Duration
	// NowFunc is exposed for tests; production calls time.Now.UnixMilli.
	NowFunc func() int64
}

// New constructs a Collector that reads from procPath. The directory
// is opened lazily on each tick (so a brief unavailability does not
// kill the worker).
func New(app storage.Appender, opt Options) *Collector {
	if opt.Interval <= 0 {
		opt.Interval = 5 * time.Second
	}
	nowFn := opt.NowFunc
	if nowFn == nil {
		nowFn = nowMs
	}
	return &Collector{
		app:      app,
		proc:     os.DirFS(opt.ProcPath),
		interval: opt.Interval,
		now:      nowFn,
		health:   Health{Interval: opt.Interval},
	}
}

// NewFromFS is the test-friendly constructor: the caller passes the
// already-open fs.FS rooted at the equivalent of /proc.
func NewFromFS(app storage.Appender, proc fs.FS, interval time.Duration, nowFn func() int64) *Collector {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if nowFn == nil {
		nowFn = nowMs
	}
	return &Collector{
		app:      app,
		proc:     proc,
		interval: interval,
		now:      nowFn,
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

func (c *Collector) recordHealth(samples int, dur time.Duration, err error) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.health.LastCollection = time.Now()
	c.health.Duration = dur
	if err != nil {
		c.health.LastError = err.Error()
		c.health.LastSamples = 0
		return
	}
	c.health.LastError = ""
	c.health.LastSamples = samples
}

// Run blocks until ctx is cancelled, calling CollectOnce on each tick.
// The first tick fires immediately so a freshly opened dashboard has
// data without waiting an entire interval.
func (c *Collector) Run(ctx context.Context) {
	c.CollectOnce()

	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.CollectOnce()
		}
	}
}

// CollectOnce reads every supported /proc file and appends the
// resulting samples in a single batch. Individual parser failures are
// logged and skipped — a missing /proc/net/dev should not prevent the
// CPU samples from being recorded. Updates the Health snapshot with
// the tick's outcome.
func (c *Collector) CollectOnce() {
	start := time.Now()
	now := c.now()
	var batch []storage.Sample

	for name, fn := range parsers() {
		samples, err := fn(c.proc, now)
		if err != nil {
			slog.Error("host parser failed", "source", name, "err", err)
			continue
		}
		batch = append(batch, samples...)
	}
	if len(batch) == 0 {
		c.recordHealth(0, time.Since(start), nil)
		return
	}
	err := c.app.Append(batch)
	if err != nil {
		slog.Error("host append failed", "err", err)
	}
	c.recordHealth(len(batch), time.Since(start), err)
}

// parsers returns the parser set in a fixed iteration order so output
// is deterministic for tests. (Yes, Go maps are unordered; the wrapping
// loop in CollectOnce relies on each entry being independent.)
func parsers() map[string]func(fs.FS, int64) ([]storage.Sample, error) {
	return map[string]func(fs.FS, int64) ([]storage.Sample, error){
		"stat":      ParseStat,
		"meminfo":   ParseMeminfo,
		"loadavg":   ParseLoadavg,
		"net/dev":   ParseNetDev,
		"diskstats": ParseDiskstats,
	}
}
