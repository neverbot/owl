// Package alert evaluates threshold rules against the query engine and
// dispatches firing / resolved events to a webhook.
//
// A rule fires when its PromQL expression's value crosses the
// configured threshold and stays there for at least Rule.For. It
// resolves once the value no longer crosses the threshold. Each
// transition produces exactly one webhook delivery, so a flapping
// metric still only generates one firing event per crossing.
package alert

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/neverbot/owl/internal/query"
	"github.com/neverbot/owl/internal/storage"
)

// Querier is the slice of query.Engine the alerter depends on. Defined
// as an interface so tests can plug a fake without spinning up the
// whole stack.
type Querier interface {
	QueryRange(expr string, from, to, step int64) (query.Result, error)
}

// Webhook is the destination for firing / resolved events.
type Webhook interface {
	Send(ctx context.Context, e Event) error
}

// Rule is one alerting rule. Op is "<", "<=", ">", ">=" — anything
// else parses as ">" so misconfigurations are loud rather than silent.
type Rule struct {
	Name      string
	Expr      string
	Op        string
	Threshold float64
	For       time.Duration
}

// Status values an Event can carry.
const (
	StatusFiring   = "firing"
	StatusResolved = "resolved"
)

// Event is the webhook payload. One Event per state transition per
// rule.
type Event struct {
	Rule       string            `json:"rule"`
	Expr       string            `json:"expr"`
	Op         string            `json:"op"`
	Threshold  float64           `json:"threshold"`
	Value      float64           `json:"value"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels,omitempty"`
	FiredAt    time.Time         `json:"fired_at"`
	ResolvedAt time.Time         `json:"resolved_at,omitempty"`
}

// Manager periodically evaluates a rule set against the query engine
// and dispatches webhook events on state transitions. The rule set
// can be replaced at runtime via SetRules (see /-/reload wiring).
//
// Each rule fans out across every series its expression returns: an
// expression like `container_memory_usage_bytes > 1e9` produces one
// independent alert per container, keyed by the series' labels. State
// is therefore indexed by (rule name, series fingerprint).
type Manager struct {
	mu       sync.Mutex // guards rules and state
	rules    []Rule
	state    map[stateKey]*ruleState
	q        Querier
	w        Webhook
	interval time.Duration
	now      func() time.Time
}

// stateKey identifies a per-rule, per-series alert lineage.
type stateKey struct {
	rule string
	fp   string
}

type ruleState struct {
	// triggeredSince is the wall-clock time at which the rule first
	// started crossing the threshold within the current run. Reset to
	// zero when the value crosses back.
	triggeredSince time.Time

	// fired is true while an alert is active for this rule — i.e. we
	// have already sent a "firing" event and have yet to send the
	// matching "resolved".
	fired bool

	// lastValue is kept for the resolved-event payload.
	lastValue float64

	// firedAt is the time we sent the firing event, copied into both
	// the firing and the eventual resolved payloads.
	firedAt time.Time
}

// New constructs a Manager. Interval is the evaluation period; pass 0
// to default to 10 s.
func New(q Querier, w Webhook, rules []Rule, interval time.Duration) *Manager {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Manager{
		rules:    rules,
		state:    make(map[stateKey]*ruleState, len(rules)),
		q:        q,
		w:        w,
		interval: interval,
		now:      time.Now,
	}
}

// Run blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	m.EvaluateOnce(ctx)
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.EvaluateOnce(ctx)
		}
	}
}

// SetRules replaces the rule set under a lock. Per-series state for
// rules that remain (matched by Name) is preserved so an already-
// firing alert doesn't re-fire on every reload. State for rules that
// disappear is discarded.
func (m *Manager) SetRules(rules []Rule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules[:0:0], rules...) // copy
	keepNames := make(map[string]struct{}, len(rules))
	for _, r := range rules {
		keepNames[r.Name] = struct{}{}
	}
	keep := make(map[stateKey]*ruleState, len(m.state))
	for k, st := range m.state {
		if _, ok := keepNames[k.rule]; ok {
			keep[k] = st
		}
	}
	m.state = keep
}

// EvaluateOnce runs one evaluation cycle across every rule.
func (m *Manager) EvaluateOnce(ctx context.Context) {
	m.mu.Lock()
	rules := append([]Rule(nil), m.rules...) // snapshot under lock
	m.mu.Unlock()
	for _, r := range rules {
		m.evaluateRule(ctx, r)
	}
}

func (m *Manager) evaluateRule(ctx context.Context, r Rule) {
	series, ok := m.evaluate(r)
	if !ok {
		// Query failed; do not flip any state. A transient query
		// failure should not "resolve" active alerts.
		return
	}
	now := m.now()
	for _, s := range series {
		if len(s.Points) == 0 {
			// This series exists in the result but carries no points
			// in the window — treat like "no observation for this
			// lineage", same conservative rule as a failed query.
			continue
		}
		value := s.Points[len(s.Points)-1].Value
		fp := fingerprint(s.Labels)

		m.mu.Lock()
		key := stateKey{rule: r.Name, fp: fp}
		st, exists := m.state[key]
		if !exists {
			st = &ruleState{}
			m.state[key] = st
		}
		m.mu.Unlock()

		st.lastValue = value
		crossed := compare(r.Op, value, r.Threshold)

		switch {
		case crossed && !st.fired:
			if st.triggeredSince.IsZero() {
				st.triggeredSince = now
			}
			if now.Sub(st.triggeredSince) >= r.For {
				st.fired = true
				st.firedAt = now
				m.dispatch(ctx, Event{
					Rule: r.Name, Expr: r.Expr, Op: r.Op, Threshold: r.Threshold,
					Value: value, Status: StatusFiring, Labels: s.Labels, FiredAt: now,
				})
			}
		case !crossed && st.fired:
			st.fired = false
			st.triggeredSince = time.Time{}
			m.dispatch(ctx, Event{
				Rule: r.Name, Expr: r.Expr, Op: r.Op, Threshold: r.Threshold,
				Value: value, Status: StatusResolved, Labels: s.Labels,
				FiredAt: st.firedAt, ResolvedAt: now,
			})
		case !crossed:
			st.triggeredSince = time.Time{}
		}
	}
}

// evaluate runs the rule's PromQL over a window wide enough to capture
// at least one step's worth of data. Returns the series slice and a
// boolean: ok=false on query error (caller should not flip state).
// An empty slice with ok=true is fine — it just means the expression
// produced nothing this cycle, which leaves all per-series state alone.
func (m *Manager) evaluate(r Rule) ([]storage.Series, bool) {
	now := m.now().UnixMilli()
	from := now - m.interval.Milliseconds() - 60_000 // ~one step + 60 s of slack
	res, err := m.q.QueryRange(r.Expr, from, now, m.interval.Milliseconds())
	if err != nil {
		slog.Error("alert query failed", "rule", r.Name, "err", err)
		return nil, false
	}
	return res.Series, true
}

// fingerprint serialises labels into a stable key. Empty labels return
// "" so a single-series-no-labels rule still gets a state entry.
func fingerprint(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\x00')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

func (m *Manager) dispatch(ctx context.Context, e Event) {
	if m.w == nil {
		slog.Warn("alert fired without webhook",
			"rule", e.Rule, "status", e.Status,
			"value", e.Value, "threshold", e.Threshold)
		return
	}
	if err := m.w.Send(ctx, e); err != nil {
		slog.Error("alert webhook failed", "rule", e.Rule, "status", e.Status, "err", err)
	}
}

// compare returns true when value crosses threshold under op.
func compare(op string, value, threshold float64) bool {
	switch op {
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case ">=":
		return value >= threshold
	case ">":
		return value > threshold
	default:
		// Unknown op — be conservative and treat as ">".
		return value > threshold
	}
}
