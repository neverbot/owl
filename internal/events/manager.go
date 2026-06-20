package events

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/neverbot/owl/internal/config"
)

// Source is one runtime event source, equivalent to scrape.Target.
// The cmd/owl wiring layer builds these from EventSourceConfig.
type Source struct {
	Name     string
	Driver   Driver
	Interval time.Duration
	Format   string
	Pattern  *regexp.Regexp
	Match    []config.MatchRule
	Mapping  config.MappingConfig
	Template *template.Template // compiled render template; nil => empty render
}

// Manager runs one goroutine per Source. SetSources replaces the
// active set on the next reconciliation tick (mirrors scrape.Manager).
type Manager struct {
	store *Store

	mu      sync.Mutex
	current map[string]sourceRunner
	pending []Source
	rev     uint64
}

// sourceRunner pairs a running Source's cancel func with its config.
type sourceRunner struct {
	src    Source
	cancel context.CancelFunc
}

// NewManager returns a Manager that writes events to store.
func NewManager(store *Store) *Manager {
	return &Manager{store: store, current: map[string]sourceRunner{}}
}

// SetSources queues a new source list; Run picks it up on the next
// 100 ms reconciliation tick.
func (m *Manager) SetSources(s []Source) {
	m.mu.Lock()
	cp := make([]Source, len(s))
	copy(cp, s)
	m.pending = cp
	m.rev++
	m.mu.Unlock()
}

// Run reconciles every 100 ms and supervises one goroutine per
// active source. Returns on ctx cancellation.
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

// reconcile compares the pending source list against current runners,
// stopping removed sources and starting new ones.
func (m *Manager) reconcile(ctx context.Context, lastRev *uint64) {
	m.mu.Lock()
	if m.rev == *lastRev {
		m.mu.Unlock()
		return
	}
	*lastRev = m.rev
	want := make(map[string]Source, len(m.pending))
	for _, s := range m.pending {
		want[s.Name] = s
	}
	for name, run := range m.current {
		if _, keep := want[name]; !keep {
			run.cancel()
			delete(m.current, name)
		}
	}
	for name, s := range want {
		if _, ok := m.current[name]; ok {
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		m.current[name] = sourceRunner{src: s, cancel: cancel}
		src := s
		go m.runSource(runCtx, src)
	}
	m.mu.Unlock()
}

// stopAll cancels every active source runner.
func (m *Manager) stopAll() {
	m.mu.Lock()
	for n, r := range m.current {
		r.cancel()
		delete(m.current, n)
	}
	m.mu.Unlock()
}

// runSource polls the source on its configured interval until ctx is
// cancelled.
func (m *Manager) runSource(ctx context.Context, s Source) {
	interval := s.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	m.pollOnce(ctx, s)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.pollOnce(ctx, s)
		}
	}
}

// pollOnce reads one batch of records from the driver, maps them to
// Events, inserts them into the store, and advances the cursor.
func (m *Manager) pollOnce(ctx context.Context, s Source) {
	cursor, err := m.store.LoadCursor(s.Name)
	if err != nil {
		slog.Error("events: load cursor", "src", s.Name, "err", err)
		return
	}
	seq, newCursor, err := s.Driver.Read(ctx, cursor)
	if err != nil {
		slog.Error("events: read", "src", s.Name, "err", err)
		return
	}
	seq(func(r Record) bool {
		if ctx.Err() != nil {
			return false
		}
		parsed, err := Parse(r.Bytes, s.Format, s.Pattern)
		if err != nil {
			return true // skip malformed lines
		}
		if !matchAll(parsed, s.Match) {
			return true
		}
		ev, err := Map(parsed, s.Mapping, s.Name, time.Now)
		if err != nil {
			return true
		}
		if r.RawTS > 0 {
			ev.TS = r.RawTS
		}
		ev.Render = Render(s.Template, ev.Payload)
		ev.ID = FNV1aID(ev.Source, ev.TS, ev.Kind, ev.Payload)
		if err := m.store.InsertEvent(ev); err != nil {
			slog.Error("events: insert", "src", s.Name, "err", err)
		}
		return true
	})
	// Cursor advances even when every record was filtered.
	if newCursor != cursor {
		if err := m.store.SaveCursor(s.Name, newCursor); err != nil {
			slog.Error("events: save cursor", "src", s.Name, "err", err)
		}
	}
}

// matchAll returns true iff every rule passes against parsed.
func matchAll(parsed map[string]any, rules []config.MatchRule) bool {
	for _, r := range rules {
		v, ok := parsed[r.Field]
		if !ok {
			return false
		}
		str, _ := v.(string)
		switch {
		case r.Equals != "" && str != r.Equals:
			return false
		case r.Contains != "" && !strings.Contains(str, r.Contains):
			return false
		}
	}
	return true
}
