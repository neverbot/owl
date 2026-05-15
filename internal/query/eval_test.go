package query

import (
	"math"
	"testing"

	"github.com/neverbot/owl/internal/storage"
)

// fakeQuerier implements Querier for testing without a real database.
type fakeQuerier struct {
	data map[string][]storage.Series // keyed by metric name
}

func (f *fakeQuerier) Query(metric string, from, to int64) ([]storage.Series, error) {
	return f.data[metric], nil
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{data: make(map[string][]storage.Series)}
}

func (f *fakeQuerier) addSeries(metric string, labels map[string]string, points []storage.Point) {
	f.data[metric] = append(f.data[metric], storage.Series{
		Metric: metric,
		Labels: labels,
		Points: points,
	})
}

func TestEvalBareSelector(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu_usage", map[string]string{"job": "api"}, []storage.Point{
		{TS: 1000, Value: 0.5},
		{TS: 2000, Value: 0.7},
	})

	node, err := Parse("cpu_usage")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 1000, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 2 {
		t.Errorf("want 2 points, got %d", len(series[0].Points))
	}
}

func TestEvalSelectorWithMatcher(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("http_requests_total", map[string]string{"job": "api", "status": "200"}, []storage.Point{
		{TS: 1000, Value: 10},
	})
	q.addSeries("http_requests_total", map[string]string{"job": "api", "status": "500"}, []storage.Point{
		{TS: 1000, Value: 2},
	})

	node, err := Parse(`http_requests_total{status="200"}`)
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 1000, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 series (status=200 only), got %d", len(series))
	}
	if series[0].Labels["status"] != "200" {
		t.Errorf("unexpected label: %v", series[0].Labels)
	}
}

func TestEvalRate(t *testing.T) {
	// Two points 60 seconds apart, counter increases by 120.
	// Expected rate: 120 / 60 = 2 per second.
	q := newFakeQuerier()
	q.addSeries("reqs_total", map[string]string{"job": "api"}, []storage.Point{
		{TS: 0, Value: 100},
		{TS: 60_000, Value: 220},
	})

	node, err := Parse("rate(reqs_total[1m])")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 1 {
		t.Fatalf("want 1 rate point, got %d", len(series[0].Points))
	}
	got := series[0].Points[0].Value
	want := 2.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("rate: want %v, got %v", want, got)
	}
}

func TestEvalRateCounterReset(t *testing.T) {
	// Counter resets from 100 to 5 then goes to 55.
	// After reset: delta = 55 (treat prev=100 as fresh start, take just 55).
	// Time span: 20s. Rate = 55 / 20 = 2.75
	q := newFakeQuerier()
	q.addSeries("reqs_total", map[string]string{}, []storage.Point{
		{TS: 0, Value: 100},
		{TS: 10_000, Value: 5},  // reset
		{TS: 20_000, Value: 55}, // continues from reset
	})

	node, err := Parse("rate(reqs_total[30s])")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 0, 30_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	got := series[0].Points[0].Value
	want := 55.0 / 20.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("rate after reset: want %.4f, got %.4f", want, got)
	}
}

func TestEvalSum(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 10}})
	q.addSeries("cpu", map[string]string{"host": "b"}, []storage.Point{{TS: 1000, Value: 20}})

	node, err := Parse("sum(cpu)")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 aggregated series, got %d", len(series))
	}
	if series[0].Points[0].Value != 30 {
		t.Errorf("sum: want 30, got %v", series[0].Points[0].Value)
	}
}

func TestEvalAvg(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 10}})
	q.addSeries("cpu", map[string]string{"host": "b"}, []storage.Point{{TS: 1000, Value: 20}})

	node, err := Parse("avg(cpu)")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if series[0].Points[0].Value != 15 {
		t.Errorf("avg: want 15, got %v", series[0].Points[0].Value)
	}
}

func TestEvalSumBy(t *testing.T) {
	q := newFakeQuerier()
	// Three series: two for job=api, one for job=worker
	q.addSeries("reqs", map[string]string{"job": "api", "host": "h1"}, []storage.Point{{TS: 1000, Value: 10}})
	q.addSeries("reqs", map[string]string{"job": "api", "host": "h2"}, []storage.Point{{TS: 1000, Value: 20}})
	q.addSeries("reqs", map[string]string{"job": "worker", "host": "h3"}, []storage.Point{{TS: 1000, Value: 5}})

	node, err := Parse("sum by (job) (reqs)")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("want 2 groups (api, worker), got %d", len(series))
	}
	// Find series by job label
	byJob := make(map[string]float64)
	for _, s := range series {
		byJob[s.Labels["job"]] = s.Points[0].Value
	}
	if byJob["api"] != 30 {
		t.Errorf("sum by job=api: want 30, got %v", byJob["api"])
	}
	if byJob["worker"] != 5 {
		t.Errorf("sum by job=worker: want 5, got %v", byJob["worker"])
	}
}

func TestEvalBinaryOpScalarRight(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 10}})

	node, err := Parse("cpu * 2")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if series[0].Points[0].Value != 20 {
		t.Errorf("want 20, got %v", series[0].Points[0].Value)
	}
}

func TestEvalBinaryOpScalarLeft(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 4}})

	node, err := Parse("100 / cpu")
	if err != nil {
		t.Fatal(err)
	}

	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if series[0].Points[0].Value != 25 {
		t.Errorf("want 25, got %v", series[0].Points[0].Value)
	}
}
