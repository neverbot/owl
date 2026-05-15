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
	if evs[1].ResolvedAt.IsZero() {
		t.Error("resolved event missing resolved_at")
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
