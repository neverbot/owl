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

func TestEvalIncrease(t *testing.T) {
	// Counter goes from 100 to 220 across 60 s. Increase over a 1m
	// window is the raw delta, 120. (Equivalent to rate * 60 = 2 * 60.)
	q := newFakeQuerier()
	q.addSeries("reqs_total", map[string]string{"job": "api"}, []storage.Point{
		{TS: 0, Value: 100},
		{TS: 60_000, Value: 220},
	})

	node, err := Parse("increase(reqs_total[1m])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || len(series[0].Points) != 1 {
		t.Fatalf("want 1 series with 1 point, got %d series, %d pts",
			len(series), len(series[0].Points))
	}
	got := series[0].Points[0].Value
	want := 120.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("increase: want %v, got %v", want, got)
	}
}

func TestEvalIRateUsesOnlyLastTwoSamples(t *testing.T) {
	// Three samples: 0→100, 10s→110 (slow), 60s→220 (sudden jump).
	// rate(1m) smears the burst: (10 + 110) / 60 = 2.0/s.
	// irate(1m) uses just the last pair: (220-110) / (60-10) = 2.2/s.
	q := newFakeQuerier()
	q.addSeries("reqs_total", map[string]string{}, []storage.Point{
		{TS: 0, Value: 100},
		{TS: 10_000, Value: 110},
		{TS: 60_000, Value: 220},
	})

	node, err := Parse("irate(reqs_total[1m])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 60_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	got := series[0].Points[0].Value
	want := 110.0 / 50.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("irate: want %v, got %v", want, got)
	}
}

func TestEvalIRateCounterReset(t *testing.T) {
	// Last two samples: 100 (TS 10s) → 5 (TS 20s) means counter reset.
	// irate treats the post-reset value as a fresh delta: 5 / 10 = 0.5/s.
	q := newFakeQuerier()
	q.addSeries("reqs_total", map[string]string{}, []storage.Point{
		{TS: 0, Value: 50},
		{TS: 10_000, Value: 100},
		{TS: 20_000, Value: 5},
	})

	node, err := Parse("irate(reqs_total[30s])")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 30_000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	got := series[0].Points[0].Value
	want := 0.5
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("irate after reset: want %v, got %v", want, got)
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

func TestEvalSumWithout(t *testing.T) {
	// Three series differing only in `host`. `sum without (host)` should
	// drop the host label and collapse them by the remaining `job` label.
	q := newFakeQuerier()
	q.addSeries("reqs", map[string]string{"job": "api", "host": "h1"}, []storage.Point{{TS: 1000, Value: 10}})
	q.addSeries("reqs", map[string]string{"job": "api", "host": "h2"}, []storage.Point{{TS: 1000, Value: 20}})
	q.addSeries("reqs", map[string]string{"job": "worker", "host": "h3"}, []storage.Point{{TS: 1000, Value: 5}})

	node, err := Parse("sum without (host) (reqs)")
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
	byJob := make(map[string]float64)
	for _, s := range series {
		if _, present := s.Labels["host"]; present {
			t.Errorf("expected `host` to be dropped, got %v", s.Labels)
		}
		byJob[s.Labels["job"]] = s.Points[0].Value
	}
	if byJob["api"] != 30 {
		t.Errorf("sum without (host) job=api: want 30, got %v", byJob["api"])
	}
	if byJob["worker"] != 5 {
		t.Errorf("sum without (host) job=worker: want 5, got %v", byJob["worker"])
	}
}

func TestEvalSumWithoutCollapsesToSingle(t *testing.T) {
	// Two series differing only in `host`. `sum without (host)` collapses
	// them into a single output series with empty labels.
	q := newFakeQuerier()
	q.addSeries("reqs", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 3}})
	q.addSeries("reqs", map[string]string{"host": "b"}, []storage.Point{{TS: 1000, Value: 4}})

	node, err := Parse("sum without (host) (reqs)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 collapsed series, got %d", len(series))
	}
	if series[0].Points[0].Value != 7 {
		t.Errorf("sum: want 7, got %v", series[0].Points[0].Value)
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

func TestEvalSeriesOnSeriesSubtraction(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("total", map[string]string{"job": "host"}, []storage.Point{
		{TS: 1000, Value: 1000},
		{TS: 2000, Value: 1000},
	})
	q.addSeries("avail", map[string]string{"job": "host"}, []storage.Point{
		{TS: 1000, Value: 300},
		{TS: 2000, Value: 250},
	})

	node, err := Parse("total - avail")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("len(series) = %d, want 1", len(series))
	}
	pts := series[0].Points
	if len(pts) != 2 {
		t.Fatalf("len(points) = %d, want 2", len(pts))
	}
	if pts[0].Value != 700 || pts[1].Value != 750 {
		t.Errorf("values = [%v, %v], want [700, 750]", pts[0].Value, pts[1].Value)
	}
}

func TestEvalHistogramQuantileBasic(t *testing.T) {
	// Five buckets with cumulative counts at TS 1000:
	//   le=0.1 → 1, le=0.5 → 4, le=1   → 7, le=5 → 9, le=+Inf → 10.
	// Total = 10. q=0.5 → target = 5. Falls into bucket le=1 (prev count 4).
	// Interp: 0.5 + (5-4)/(7-4) * (1-0.5) = 0.5 + (1/3)*0.5 = 0.6666...
	q := newFakeQuerier()
	add := func(le string, v float64) {
		q.addSeries("h_bucket", map[string]string{"le": le}, []storage.Point{{TS: 1000, Value: v}})
	}
	add("0.1", 1)
	add("0.5", 4)
	add("1", 7)
	add("5", 9)
	add("+Inf", 10)

	node, err := Parse("histogram_quantile(0.5, h_bucket)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	if _, has := series[0].Labels["le"]; has {
		t.Errorf("expected `le` to be dropped, got %v", series[0].Labels)
	}
	got := series[0].Points[0].Value
	want := 0.5 + (1.0/3.0)*0.5
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("histogram_quantile(0.5): want %v, got %v", want, got)
	}
}

func TestEvalHistogramQuantileGroups(t *testing.T) {
	// Two services with identical bucket structure but different counts.
	// Each group must produce its own output series with `le` dropped.
	q := newFakeQuerier()
	addSvc := func(svc, le string, v float64) {
		q.addSeries("h_bucket", map[string]string{"service": svc, "le": le},
			[]storage.Point{{TS: 1000, Value: v}})
	}
	addSvc("api", "1", 1)
	addSvc("api", "2", 2)
	addSvc("api", "+Inf", 2)
	addSvc("worker", "1", 5)
	addSvc("worker", "2", 10)
	addSvc("worker", "+Inf", 10)

	node, err := Parse("histogram_quantile(0.9, h_bucket)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("want 2 groups, got %d", len(series))
	}
	for _, s := range series {
		if _, has := s.Labels["le"]; has {
			t.Errorf("expected `le` dropped, got %v", s.Labels)
		}
		if _, has := s.Labels["service"]; !has {
			t.Errorf("expected `service` preserved, got %v", s.Labels)
		}
	}
}

func TestEvalHistogramQuantileAllZero(t *testing.T) {
	// All buckets empty: no observations → no point emitted.
	q := newFakeQuerier()
	q.addSeries("h_bucket", map[string]string{"le": "1"}, []storage.Point{{TS: 1000, Value: 0}})
	q.addSeries("h_bucket", map[string]string{"le": "+Inf"}, []storage.Point{{TS: 1000, Value: 0}})

	node, err := Parse("histogram_quantile(0.5, h_bucket)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 0 {
		t.Errorf("want 0 series (no observations), got %d", len(series))
	}
}

func TestEvalHistogramQuantileMissingLE(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("h_bucket", map[string]string{"service": "api"},
		[]storage.Point{{TS: 1000, Value: 1}})

	node, err := Parse("histogram_quantile(0.5, h_bucket)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	_, err = ev.eval(node)
	if err == nil {
		t.Fatal("expected error for missing `le` label, got nil")
	}
}

func TestEvalSeriesOnSeriesBroadcast(t *testing.T) {
	// LHS has two devices, RHS has a single global series; broadcast.
	q := newFakeQuerier()
	q.addSeries("dev_bytes", map[string]string{"device": "sda"}, []storage.Point{{TS: 1000, Value: 200}})
	q.addSeries("dev_bytes", map[string]string{"device": "sdb"}, []storage.Point{{TS: 1000, Value: 400}})
	q.addSeries("total_bytes", map[string]string{}, []storage.Point{{TS: 1000, Value: 1000}})

	node, err := Parse("dev_bytes / total_bytes")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("len(series) = %d, want 2", len(series))
	}
	for _, s := range series {
		if len(s.Points) != 1 {
			t.Fatalf("series %v has %d points, want 1", s.Labels, len(s.Points))
		}
	}
}
