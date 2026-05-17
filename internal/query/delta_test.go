package query

import (
	"math"
	"testing"

	"github.com/neverbot/owl/internal/storage"
)

func TestParseDelta(t *testing.T) {
	node, err := Parse("delta(temperature[5m])")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := node.(*RangeFuncNode)
	if !ok {
		t.Fatalf("expected *RangeFuncNode, got %T", node)
	}
	if r.Func != "delta" {
		t.Errorf("func: want delta, got %q", r.Func)
	}
	if r.Window.Value != 5 || r.Window.Unit != "m" {
		t.Errorf("window: want 5m, got %+v", r.Window)
	}
}

func TestEvalDeltaGaugeDecrease(t *testing.T) {
	// delta is for gauges; unlike increase it does NOT treat a decrease
	// as a counter reset. Gauge drops from 50 to 30 over the window:
	// delta = 30 - 50 = -20.
	q := newFakeQuerier()
	q.addSeries("temp", map[string]string{"room": "kitchen"}, []storage.Point{
		{TS: 0, Value: 50},
		{TS: 60_000, Value: 30},
	})

	node, err := Parse("delta(temp[1m])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || len(series[0].Points) != 1 {
		t.Fatalf("want 1 series with 1 point, got %d series", len(series))
	}
	got := series[0].Points[0].Value
	want := -20.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("delta: want %v, got %v", want, got)
	}
}

func TestEvalDeltaPositive(t *testing.T) {
	// Three intermediate points; delta only cares about last - first.
	q := newFakeQuerier()
	q.addSeries("temp", map[string]string{}, []storage.Point{
		{TS: 0, Value: 10},
		{TS: 20_000, Value: 999},
		{TS: 40_000, Value: -5},
		{TS: 60_000, Value: 25},
	})

	node, err := Parse("delta(temp[1m])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	got := series[0].Points[0].Value
	want := 15.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("delta: want %v, got %v", want, got)
	}
}
