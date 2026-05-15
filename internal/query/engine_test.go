package query_test

import (
	"os"
	"testing"

	"github.com/neverbot/owl/internal/query"
	"github.com/neverbot/owl/internal/storage"
)

func openTestStore(t *testing.T) *storage.Store {
	t.Helper()
	f, err := os.CreateTemp("", "owl-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	st, err := storage.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestEngineQueryRange(t *testing.T) {
	st := openTestStore(t)
	_ = st.Append([]storage.Sample{
		{Metric: "cpu", Labels: map[string]string{"host": "a"}, TS: 1000, Value: 0.3},
		{Metric: "cpu", Labels: map[string]string{"host": "a"}, TS: 2000, Value: 0.5},
		{Metric: "cpu", Labels: map[string]string{"host": "b"}, TS: 1000, Value: 0.7},
	})

	eng := query.NewEngine(st)
	res, err := eng.QueryRange("cpu", 0, 3000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series) != 2 {
		t.Errorf("want 2 series, got %d", len(res.Series))
	}
}

func TestEngineQueryRateIntegration(t *testing.T) {
	st := openTestStore(t)
	_ = st.Append([]storage.Sample{
		{Metric: "reqs_total", Labels: map[string]string{"job": "api"}, TS: 0, Value: 100},
		{Metric: "reqs_total", Labels: map[string]string{"job": "api"}, TS: 60_000, Value: 220},
	})

	eng := query.NewEngine(st)
	res, err := eng.QueryRange("rate(reqs_total[1m])", 0, 60_000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series) != 1 {
		t.Fatalf("want 1 series, got %d", len(res.Series))
	}
	got := res.Series[0].Points[0].Value
	if got < 1.9 || got > 2.1 {
		t.Errorf("rate: want ~2.0, got %v", got)
	}
}

func TestEngineCapabilities(t *testing.T) {
	eng := query.NewEngine(nil)
	caps := eng.Capabilities()
	if len(caps.Functions) == 0 {
		t.Error("Capabilities.Functions should not be empty")
	}
	if len(caps.Aggrs) == 0 {
		t.Error("Capabilities.Aggrs should not be empty")
	}
	if len(caps.Matchers) == 0 {
		t.Error("Capabilities.Matchers should not be empty")
	}
	if len(caps.Operators) == 0 {
		t.Error("Capabilities.Operators should not be empty")
	}
}

func TestEngineIsSupported(t *testing.T) {
	eng := query.NewEngine(nil)

	supported := []string{
		"cpu_usage",
		`http_requests_total{job="api"}`,
		"rate(reqs_total[1m])",
		"sum(cpu)",
		"sum by (job) (rate(reqs_total[1m]))",
		"cpu * 2",
		"2 + cpu",
	}
	for _, expr := range supported {
		ok, reason := eng.IsSupported(expr)
		if !ok {
			t.Errorf("IsSupported(%q) = false (%s), want true", expr, reason)
		}
	}

	unsupported := []string{
		"histogram_quantile(0.9, rate(reqs_bucket[5m]))",
		"",
	}
	for _, expr := range unsupported {
		ok, _ := eng.IsSupported(expr)
		if ok {
			t.Errorf("IsSupported(%q) = true, want false", expr)
		}
	}
}
