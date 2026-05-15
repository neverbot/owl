package scrape

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

// Manager runs one goroutine per scrape target. The active target set
// is updated via Set(), which atomically swaps and triggers a
// reconciliation: targets removed are cancelled, targets added are
// started, unchanged targets keep ticking.
type Manager struct {
	app storage.Appender

	mu      sync.Mutex
	current map[string]targetRunner // keyed by Target.Name
	pending []Target
	rev     uint64 // incremented on every Set

	healthMu sync.RWMutex
	health   map[string]*TargetHealth // keyed by Target.Name
}

// TargetHealth captures the most recent scrape outcome for one target.
// Returned by Manager.HealthSnapshot for the /api/targets endpoint.
type TargetHealth struct {
	Name        string            `json:"name"`
	URL         string            `json:"url"`
	Labels      map[string]string `json:"labels,omitempty"`
	Interval    time.Duration     `json:"interval"`
	LastScrape  time.Time         `json:"last_scrape,omitempty"`
	LastError   string            `json:"last_error,omitempty"`
	LastSamples int               `json:"last_samples"`
	Duration    time.Duration     `json:"duration,omitempty"`
}

type targetRunner struct {
	tgt    Target
	cancel context.CancelFunc
}

// NewManager returns a Manager that writes scraped samples to app.
func NewManager(app storage.Appender) *Manager {
	return &Manager{
		app:     app,
		current: make(map[string]targetRunner),
		health:  make(map[string]*TargetHealth),
	}
}

// HealthSnapshot returns the latest known health entry for every
// active target. Output is sorted by Name for determinism.
func (m *Manager) HealthSnapshot() []TargetHealth {
	m.healthMu.RLock()
	names := make([]string, 0, len(m.health))
	for n := range m.health {
		names = append(names, n)
	}
	m.healthMu.RUnlock()
	sort.Strings(names)

	out := make([]TargetHealth, 0, len(names))
	m.healthMu.RLock()
	for _, n := range names {
		h := m.health[n]
		if h == nil {
			continue
		}
		// Copy to avoid handing out an aliased pointer.
		cp := *h
		if h.Labels != nil {
			cp.Labels = make(map[string]string, len(h.Labels))
			for k, v := range h.Labels {
				cp.Labels[k] = v
			}
		}
		out = append(out, cp)
	}
	m.healthMu.RUnlock()
	return out
}

func (m *Manager) recordHealth(tgt Target, samples int, dur time.Duration, err error) {
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	h, ok := m.health[tgt.Name]
	if !ok {
		h = &TargetHealth{Name: tgt.Name, URL: tgt.URL, Interval: tgt.Interval, Labels: tgt.Labels}
		m.health[tgt.Name] = h
	}
	h.URL = tgt.URL
	h.Interval = tgt.Interval
	h.Labels = tgt.Labels
	h.LastScrape = time.Now()
	h.LastSamples = samples
	h.Duration = dur
	if err != nil {
		h.LastError = err.Error()
	} else {
		h.LastError = ""
	}
}

func (m *Manager) forgetHealth(name string) {
	m.healthMu.Lock()
	delete(m.health, name)
	m.healthMu.Unlock()
}

// Set replaces the active target set on the next reconciliation tick.
// Calling Set before Run is fine — Run will pick the latest set up.
func (m *Manager) Set(targets []Target) {
	m.mu.Lock()
	cp := make([]Target, len(targets))
	copy(cp, targets)
	m.pending = cp
	m.rev++
	m.mu.Unlock()
}

// Run blocks until ctx is cancelled. It reconciles every 100 ms,
// applying the latest Set() result, and supervises one goroutine per
// active target.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	var lastRev uint64

	m.reconcile(ctx, &lastRev)

	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-tick.C:
			m.reconcile(ctx, &lastRev)
		}
	}
}

func (m *Manager) reconcile(ctx context.Context, lastRev *uint64) {
	m.mu.Lock()
	if m.rev == *lastRev {
		m.mu.Unlock()
		return
	}
	*lastRev = m.rev
	want := make(map[string]Target, len(m.pending))
	for _, t := range m.pending {
		want[t.Name] = t
	}
	have := m.current

	// Stop and remove disappearing targets.
	for name, run := range have {
		if _, keep := want[name]; !keep {
			run.cancel()
			delete(have, name)
			m.forgetHealth(name)
		}
	}

	// Add new and update changed targets.
	for name, t := range want {
		existing, ok := have[name]
		if ok && targetsEqual(existing.tgt, t) {
			continue
		}
		if ok {
			existing.cancel()
		}
		runCtx, cancel := context.WithCancel(ctx)
		have[name] = targetRunner{tgt: t, cancel: cancel}
		go m.runTarget(runCtx, t)
	}
	m.mu.Unlock()
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	for name, run := range m.current {
		run.cancel()
		delete(m.current, name)
	}
	m.mu.Unlock()
}

func (m *Manager) runTarget(ctx context.Context, tgt Target) {
	interval := tgt.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}

	scrape := func() {
		start := time.Now()
		n, err := ScrapeOnce(ctx, tgt, m.app)
		m.recordHealth(tgt, n, time.Since(start), err)
		if err != nil && ctx.Err() == nil {
			slog.Error("scrape failed", "target", tgt.Name, "err", err)
		}
	}

	scrape()

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			scrape()
		}
	}
}

// targetsEqual returns true if a and b represent the same scrape configuration,
// including all label key/value pairs.
func targetsEqual(a, b Target) bool {
	if a.Name != b.Name || a.URL != b.URL || a.Interval != b.Interval || a.Timeout != b.Timeout {
		return false
	}
	if len(a.Labels) != len(b.Labels) {
		return false
	}
	for k, v := range a.Labels {
		if b.Labels[k] != v {
			return false
		}
	}
	return true
}
