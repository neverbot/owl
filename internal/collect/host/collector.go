package host

import (
	"context"
	"io/fs"
	"log"
	"os"
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
	}
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
// CPU samples from being recorded.
func (c *Collector) CollectOnce() {
	now := c.now()
	var batch []storage.Sample

	for name, fn := range parsers() {
		samples, err := fn(c.proc, now)
		if err != nil {
			log.Printf("host collector: %s: %v", name, err)
			continue
		}
		batch = append(batch, samples...)
	}
	if len(batch) == 0 {
		return
	}
	if err := c.app.Append(batch); err != nil {
		log.Printf("host collector: append: %v", err)
	}
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
