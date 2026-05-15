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
	"sync"
	"time"

	"github.com/neverbot/owl/internal/query"
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
type Manager struct {
	mu       sync.Mutex // guards rules and state
	rules    []Rule
	state    map[string]*ruleState
	q        Querier
	w        Webhook
	interval time.Duration
	now      func() time.Time
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
		state:    make(map[string]*ruleState, len(rules)),
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

// SetRules replaces the rule set under a lock. State for rules that
// remain (matched by Name) is preserved so a rule that's already
// firing doesn't re-fire on every reload. State for rules that
// disappear is discarded.
func (m *Manager) SetRules(rules []Rule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules[:0:0], rules...) // copy
	keep := make(map[string]*ruleState, len(rules))
	for _, r := range rules {
		if st, ok := m.state[r.Name]; ok {
			keep[r.Name] = st
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
	m.mu.Lock()
	st, ok := m.state[r.Name]
	if !ok {
		st = &ruleState{}
		m.state[r.Name] = st
	}
	m.mu.Unlock()

	value, labels, ok := m.currentValue(r)
	if !ok {
		// Query failed or returned no points; do not flip state. A
		// transient query failure should not "resolve" an active alert.
		return
	}
	st.lastValue = value

	now := m.now()
	crossed := compare(r.Op, value, r.Threshold)

	switch {
	case crossed && !st.fired:
		// Threshold breached — start counting toward `For`.
		if st.triggeredSince.IsZero() {
			st.triggeredSince = now
		}
		if now.Sub(st.triggeredSince) >= r.For {
			st.fired = true
			st.firedAt = now
			m.dispatch(ctx, Event{
				Rule: r.Name, Expr: r.Expr, Op: r.Op, Threshold: r.Threshold,
				Value: value, Status: StatusFiring, Labels: labels, FiredAt: now,
			})
		}
	case !crossed && st.fired:
		// Resolved.
		st.fired = false
		st.triggeredSince = time.Time{}
		m.dispatch(ctx, Event{
			Rule: r.Name, Expr: r.Expr, Op: r.Op, Threshold: r.Threshold,
			Value: value, Status: StatusResolved, Labels: labels,
			FiredAt: st.firedAt, ResolvedAt: now,
		})
	case !crossed:
		// Below threshold; reset the count so a future breach starts
		// from zero.
		st.triggeredSince = time.Time{}
	}
}

// currentValue evaluates the rule's expression as an instant-ish query
// (most recent step worth of data) and returns the latest value of the
// first series. Returns ok=false on query failure or empty result.
func (m *Manager) currentValue(r Rule) (float64, map[string]string, bool) {
	now := m.now().UnixMilli()
	from := now - m.interval.Milliseconds() - 60_000 // ~one step + 60 s of slack
	res, err := m.q.QueryRange(r.Expr, from, now, m.interval.Milliseconds())
	if err != nil {
		slog.Error("alert query failed", "rule", r.Name, "err", err)
		return 0, nil, false
	}
	if len(res.Series) == 0 || len(res.Series[0].Points) == 0 {
		return 0, nil, false
	}
	s := res.Series[0]
	return s.Points[len(s.Points)-1].Value, s.Labels, true
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
