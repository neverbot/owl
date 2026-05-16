package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/query"
	"github.com/neverbot/owl/internal/storage"
)

// fakeQuerier returns whatever value was last set, packaged as one
// series, when QueryRange is called.
type fakeQuerier struct {
	value float64
	ok    bool
}

func (f *fakeQuerier) QueryRange(_ string, _, to, _ int64) (query.Result, error) {
	if !f.ok {
		return query.Result{}, nil
	}
	return query.Result{
		Series: []storage.Series{{
			Metric: "x",
			Labels: map[string]string{"job": "test"},
			Points: []storage.Point{{TS: to, Value: f.value}},
		}},
	}, nil
}

type capturingWebhook struct {
	mu     sync.Mutex
	events []Event
}

func (c *capturingWebhook) Send(_ context.Context, e Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}
func (c *capturingWebhook) all() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

func newManager(q Querier, w Webhook, rules []Rule, nowFn func() time.Time) *Manager {
	m := New(q, w, rules, time.Second)
	if nowFn != nil {
		m.now = nowFn
	}
	return m
}

func TestFireAfterForDuration(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	w := &capturingWebhook{}
	rules := []Rule{{
		Name: "high_cpu", Expr: "cpu", Op: ">", Threshold: 0.5,
		For: 30 * time.Second,
	}}

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0
	m := newManager(q, w, rules, func() time.Time { return now })

	// First evaluation: crossed but not yet held long enough.
	m.EvaluateOnce(context.Background())
	if len(w.all()) != 0 {
		t.Fatalf("unexpected event before For elapsed: %+v", w.all())
	}

	// Advance 15 s — still inside For window.
	now = t0.Add(15 * time.Second)
	m.EvaluateOnce(context.Background())
	if len(w.all()) != 0 {
		t.Fatalf("unexpected event at 15s: %+v", w.all())
	}

	// Advance to 31 s — crosses the For threshold.
	now = t0.Add(31 * time.Second)
	m.EvaluateOnce(context.Background())
	evs := w.all()
	if len(evs) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(evs))
	}
	if evs[0].Status != StatusFiring {
		t.Errorf("status = %q, want firing", evs[0].Status)
	}
	if evs[0].Value != 0.9 {
		t.Errorf("value = %v", evs[0].Value)
	}
}

func TestNoDuplicateFireWhileActive(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	w := &capturingWebhook{}
	rules := []Rule{{Name: "r", Expr: "x", Op: ">", Threshold: 0.5, For: 0}}

	t0 := time.Now()
	now := t0
	m := newManager(q, w, rules, func() time.Time { return now })

	// For=0 means immediate fire on first match.
	m.EvaluateOnce(context.Background())
	// Many more cycles while still above threshold.
	for i := 0; i < 5; i++ {
		now = now.Add(time.Second)
		m.EvaluateOnce(context.Background())
	}
	if got := len(w.all()); got != 1 {
		t.Errorf("len(events) = %d, want 1 (no duplicates while active)", got)
	}
}

func TestResolveOnceConditionClears(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	w := &capturingWebhook{}
	rules := []Rule{{Name: "r", Expr: "x", Op: ">", Threshold: 0.5, For: 0}}

	now := time.Now()
	m := newManager(q, w, rules, func() time.Time { return now })

	m.EvaluateOnce(context.Background()) // fire
	q.value = 0.1                        // condition clears
	now = now.Add(time.Second)
	m.EvaluateOnce(context.Background()) // resolve

	evs := w.all()
	if len(evs) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(evs))
	}
	if evs[0].Status != StatusFiring || evs[1].Status != StatusResolved {
		t.Errorf("statuses = %q, %q", evs[0].Status, evs[1].Status)
	}
	if evs[1].ResolvedAt == nil || evs[1].ResolvedAt.IsZero() {
		t.Error("resolved event missing resolved_at")
	}
	if evs[0].ResolvedAt != nil {
		t.Errorf("firing event must not carry resolved_at, got %v", *evs[0].ResolvedAt)
	}
}

func TestQueryFailureDoesNotResolve(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	w := &capturingWebhook{}
	rules := []Rule{{Name: "r", Expr: "x", Op: ">", Threshold: 0.5, For: 0}}

	now := time.Now()
	m := newManager(q, w, rules, func() time.Time { return now })

	m.EvaluateOnce(context.Background()) // fire
	q.ok = false                         // query produces no data
	now = now.Add(time.Second)
	m.EvaluateOnce(context.Background()) // must NOT send resolved

	if got := len(w.all()); got != 1 {
		t.Errorf("len(events) = %d, want 1 (no resolved while query failing)", got)
	}
}

func TestSetRulesPreservesStateForKeptRules(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	w := &capturingWebhook{}
	rules := []Rule{{Name: "r", Expr: "x", Op: ">", Threshold: 0.5, For: 0}}

	now := time.Now()
	m := newManager(q, w, rules, func() time.Time { return now })

	m.EvaluateOnce(context.Background()) // fire
	if len(w.all()) != 1 {
		t.Fatalf("setup: expected 1 event, got %d", len(w.all()))
	}

	// Replace with the same rule. State should carry over so we do
	// NOT re-fire while still above threshold.
	m.SetRules(rules)
	now = now.Add(time.Second)
	m.EvaluateOnce(context.Background())
	if got := len(w.all()); got != 1 {
		t.Errorf("after SetRules with same rule: %d events, want 1", got)
	}
}

func TestSetRulesAddsAndRemovesRules(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	w := &capturingWebhook{}
	rules := []Rule{{Name: "r1", Expr: "x", Op: ">", Threshold: 0.5, For: 0}}

	now := time.Now()
	m := newManager(q, w, rules, func() time.Time { return now })

	m.EvaluateOnce(context.Background()) // r1 fires

	// Replace r1 with r2 (new name). r1's state is discarded.
	m.SetRules([]Rule{{Name: "r2", Expr: "x", Op: ">", Threshold: 0.5, For: 0}})
	now = now.Add(time.Second)
	m.EvaluateOnce(context.Background()) // r2 fires for the first time

	evs := w.all()
	if len(evs) != 2 {
		t.Fatalf("events = %d, want 2 (one per rule)", len(evs))
	}
	if evs[0].Rule != "r1" || evs[1].Rule != "r2" {
		t.Errorf("rules = %q, %q", evs[0].Rule, evs[1].Rule)
	}
}

// multiQuerier returns several series with caller-supplied values and
// labels, so we can drive fan-out scenarios.
type multiQuerier struct {
	series []storage.Series
	err    error
}

func (m *multiQuerier) QueryRange(_ string, _, _, _ int64) (query.Result, error) {
	if m.err != nil {
		return query.Result{}, m.err
	}
	return query.Result{Series: append([]storage.Series(nil), m.series...)}, nil
}

func TestMultiSeriesFanOut(t *testing.T) {
	now := time.Now()
	mk := func(name string, v float64) storage.Series {
		return storage.Series{
			Metric: "mem",
			Labels: map[string]string{"container": name},
			Points: []storage.Point{{TS: now.UnixMilli(), Value: v}},
		}
	}
	q := &multiQuerier{series: []storage.Series{
		mk("alpha", 0.9), // crosses
		mk("beta", 0.1),  // below
		mk("gamma", 0.95),
	}}
	w := &capturingWebhook{}
	rules := []Rule{{Name: "hi_mem", Expr: "mem", Op: ">", Threshold: 0.5, For: 0}}

	cur := now
	m := newManager(q, w, rules, func() time.Time { return cur })
	m.EvaluateOnce(context.Background())

	evs := w.all()
	if len(evs) != 2 {
		t.Fatalf("len(events) = %d, want 2 (alpha + gamma)", len(evs))
	}
	got := map[string]bool{}
	for _, e := range evs {
		if e.Status != StatusFiring {
			t.Errorf("status = %q, want firing", e.Status)
		}
		got[e.Labels["container"]] = true
	}
	if !got["alpha"] || !got["gamma"] || got["beta"] {
		t.Errorf("fired containers = %v, want {alpha, gamma}", got)
	}

	// beta crosses on the next cycle; alpha and gamma stay firing —
	// no duplicates. Only one new event (beta).
	q.series[1] = mk("beta", 0.7)
	cur = cur.Add(time.Second)
	m.EvaluateOnce(context.Background())
	if got := len(w.all()); got != 3 {
		t.Errorf("after beta crosses: %d events, want 3", got)
	}

	// alpha resolves on the next cycle — one resolved event.
	q.series[0] = mk("alpha", 0.1)
	cur = cur.Add(time.Second)
	m.EvaluateOnce(context.Background())
	evs = w.all()
	if len(evs) != 4 {
		t.Fatalf("after alpha resolves: %d events, want 4", len(evs))
	}
	last := evs[3]
	if last.Status != StatusResolved || last.Labels["container"] != "alpha" {
		t.Errorf("last event = %+v, want resolved alpha", last)
	}
}

func TestFingerprintStableUnderKeyOrder(t *testing.T) {
	a := fingerprint(map[string]string{"a": "1", "b": "2"})
	b := fingerprint(map[string]string{"b": "2", "a": "1"})
	if a != b {
		t.Errorf("fingerprint not stable: %q vs %q", a, b)
	}
	if fingerprint(nil) != "" || fingerprint(map[string]string{}) != "" {
		t.Errorf("empty labels should fingerprint to \"\"")
	}
}

func TestSetWebhookHotSwap(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	rules := []Rule{{Name: "r", Expr: "x", Op: ">", Threshold: 0.5, For: 0}}

	now := time.Now()
	m := newManager(q, nil, rules, func() time.Time { return now })
	m.EvaluateOnce(context.Background())

	// No webhook attached yet — install one and verify subsequent
	// transitions deliver to it. Resolved is the next observable
	// transition because the rule already fired in memory.
	w := &capturingWebhook{}
	m.SetWebhook(w)
	q.value = 0.1
	now = now.Add(time.Second)
	m.EvaluateOnce(context.Background())

	evs := w.all()
	if len(evs) != 1 || evs[0].Status != StatusResolved {
		t.Fatalf("post-SetWebhook events = %+v, want one resolved", evs)
	}
}

func TestCompareOps(t *testing.T) {
	cases := []struct {
		op   string
		v, t float64
		want bool
	}{
		{">", 1, 0.5, true},
		{">", 0.4, 0.5, false},
		{">=", 0.5, 0.5, true},
		{"<", 0.4, 0.5, true},
		{"<=", 0.5, 0.5, true},
		{"unknown", 1, 0.5, true},
	}
	for _, tc := range cases {
		if got := compare(tc.op, tc.v, tc.t); got != tc.want {
			t.Errorf("compare(%q,%v,%v) = %v", tc.op, tc.v, tc.t, got)
		}
	}
}

func TestSnapshotCountersAndFiring(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	w := &capturingWebhook{}
	rules := []Rule{
		{Name: "a", Expr: "x", Op: ">", Threshold: 0.5, For: 0},
		{Name: "b", Expr: "x", Op: ">", Threshold: 0.5, For: 0},
	}
	now := time.Now()
	m := newManager(q, w, rules, func() time.Time { return now })

	// Before any cycle: all zero.
	if s := m.Snapshot(); s.EvaluationsTotal != 0 || s.WebhookSendsTotal != 0 || s.Firing != 0 {
		t.Fatalf("initial snapshot non-zero: %+v", s)
	}

	// One cycle: both rules fire immediately.
	m.EvaluateOnce(context.Background())
	s := m.Snapshot()
	if s.EvaluationsTotal != 1 {
		t.Errorf("evaluations after 1 cycle = %d, want 1", s.EvaluationsTotal)
	}
	if s.WebhookSendsTotal != 2 {
		t.Errorf("webhook sends = %d, want 2 (one per rule)", s.WebhookSendsTotal)
	}
	if s.WebhookFailuresTotal != 0 {
		t.Errorf("failures = %d, want 0", s.WebhookFailuresTotal)
	}
	if s.Firing != 2 {
		t.Errorf("firing = %d, want 2", s.Firing)
	}

	// One series clears, the other stays firing.
	q.value = 0.1
	now = now.Add(time.Second)
	m.EvaluateOnce(context.Background())
	q.value = 0.9
	now = now.Add(time.Second)
	m.EvaluateOnce(context.Background())

	s = m.Snapshot()
	if s.EvaluationsTotal != 3 {
		t.Errorf("evaluations = %d, want 3", s.EvaluationsTotal)
	}
	// Sequence: fire(2) + resolved(2) + re-fire(2) = 6 sends total.
	if s.WebhookSendsTotal != 6 {
		t.Errorf("webhook sends = %d, want 6", s.WebhookSendsTotal)
	}
}

type erroringWebhook struct{}

func (erroringWebhook) Send(context.Context, Event) error {
	return errBoom
}

var errBoom = &dummyErr{"boom"}

type dummyErr struct{ s string }

func (e *dummyErr) Error() string { return e.s }

func TestSnapshotCountsWebhookFailures(t *testing.T) {
	q := &fakeQuerier{value: 0.9, ok: true}
	rules := []Rule{{Name: "r", Expr: "x", Op: ">", Threshold: 0.5, For: 0}}
	m := newManager(q, erroringWebhook{}, rules, time.Now)

	m.EvaluateOnce(context.Background())
	s := m.Snapshot()
	if s.WebhookSendsTotal != 1 || s.WebhookFailuresTotal != 1 {
		t.Errorf("sends=%d failures=%d, want 1/1", s.WebhookSendsTotal, s.WebhookFailuresTotal)
	}
}
