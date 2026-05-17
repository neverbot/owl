package query

import (
	"math"
	"testing"

	"github.com/neverbot/owl/internal/storage"
)

func TestParseOverTimeFamily(t *testing.T) {
	for _, fn := range []string{
		"avg_over_time", "sum_over_time",
		"min_over_time", "max_over_time", "count_over_time",
	} {
		node, err := Parse(fn + "(cpu[5m])")
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		r, ok := node.(*RangeFuncNode)
		if !ok {
			t.Fatalf("%s: expected *RangeFuncNode, got %T", fn, node)
		}
		if r.Func != fn {
			t.Errorf("%s: Func = %q", fn, r.Func)
		}
		if r.Window.Value != 5 || r.Window.Unit != "m" {
			t.Errorf("%s: window: want 5m, got %+v", fn, r.Window)
		}
	}
}

func TestEvalAvgOverTime(t *testing.T) {
	// Samples: 10, 20, 30 → avg = 20.
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{}, []storage.Point{
		{TS: 0, Value: 10},
		{TS: 30_000, Value: 20},
		{TS: 60_000, Value: 30},
	})

	node, err := Parse("avg_over_time(cpu[1m])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	got := series[0].Points[0].Value
	want := 20.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("avg_over_time: want %v, got %v", want, got)
	}
}

func TestEvalSumOverTime(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{}, []storage.Point{
		{TS: 0, Value: 1},
		{TS: 30_000, Value: 2},
		{TS: 60_000, Value: 3},
	})

	node, err := Parse("sum_over_time(cpu[1m])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if got := series[0].Points[0].Value; got != 6 {
		t.Errorf("sum_over_time: want 6, got %v", got)
	}
}

func TestEvalMinMaxOverTime(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{}, []storage.Point{
		{TS: 0, Value: 7},
		{TS: 30_000, Value: 2},
		{TS: 60_000, Value: 9},
	})

	nMin, _ := Parse("min_over_time(cpu[1m])")
	nMax, _ := Parse("max_over_time(cpu[1m])")
	ev := newEvaluator(q, 0, 60_000)
	sMin, err := ev.eval(nMin)
	if err != nil {
		t.Fatal(err)
	}
	sMax, err := ev.eval(nMax)
	if err != nil {
		t.Fatal(err)
	}
	if got := sMin[0].Points[0].Value; got != 2 {
		t.Errorf("min_over_time: want 2, got %v", got)
	}
	if got := sMax[0].Points[0].Value; got != 9 {
		t.Errorf("max_over_time: want 9, got %v", got)
	}
}

func TestEvalCountOverTime(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{}, []storage.Point{
		{TS: 0, Value: 1},
		{TS: 20_000, Value: 1},
		{TS: 40_000, Value: 1},
		{TS: 60_000, Value: 1},
	})

	node, err := Parse("count_over_time(cpu[1m])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if got := series[0].Points[0].Value; got != 4 {
		t.Errorf("count_over_time: want 4, got %v", got)
	}
}

func TestEvalCountOverTimeSingleSample(t *testing.T) {
	// With only one sample in the window, count_over_time must still
	// emit (= 1). The other range funcs (rate/delta/...) require >= 2.
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{}, []storage.Point{
		{TS: 60_000, Value: 42},
	})

	node, err := Parse("count_over_time(cpu[1m])")
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
	if got := series[0].Points[0].Value; got != 1 {
		t.Errorf("count_over_time(single): want 1, got %v", got)
	}
}
