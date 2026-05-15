package scrape

import (
	"context"
	"log/slog"
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
	}
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

	if err := ScrapeOnce(ctx, tgt, m.app); err != nil && ctx.Err() == nil {
		slog.Error("scrape failed", "target", tgt.Name, "err", err)
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := ScrapeOnce(ctx, tgt, m.app); err != nil && ctx.Err() == nil {
				slog.Error("scrape failed", "target", tgt.Name, "err", err)
			}
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
