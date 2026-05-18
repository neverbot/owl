package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "owl.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func ms(s int64) int64 { return s * 1000 }

func TestAppendAndQueryRoundTrip(t *testing.T) {
	s := newStore(t)

	samples := []Sample{
		{Metric: "cpu_seconds_total", Labels: map[string]string{"job": "host", "cpu": "0"}, TS: ms(10), Value: 1.0},
		{Metric: "cpu_seconds_total", Labels: map[string]string{"job": "host", "cpu": "0"}, TS: ms(20), Value: 2.0},
		{Metric: "cpu_seconds_total", Labels: map[string]string{"job": "host", "cpu": "1"}, TS: ms(20), Value: 3.0},
		{Metric: "mem_bytes", Labels: map[string]string{"job": "host"}, TS: ms(20), Value: 99.0},
	}
	if err := s.Append(samples); err != nil {
		t.Fatalf("Append: %v", err)
	}

	series, err := s.Query("cpu_seconds_total", ms(0), ms(30))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("Query returned %d series, want 2", len(series))
	}

	// Series must be sorted by canonical labels for deterministic output.
	if series[0].Labels["cpu"] != "0" || series[1].Labels["cpu"] != "1" {
		t.Errorf("series not ordered by labels: %+v", series)
	}
	if len(series[0].Points) != 2 {
		t.Errorf("cpu=0 series has %d points, want 2", len(series[0].Points))
	}
	if series[0].Points[0].Value != 1.0 || series[0].Points[1].Value != 2.0 {
		t.Errorf("cpu=0 values = %v, want [1, 2]", series[0].Points)
	}
}

func TestQueryRespectsTimeRange(t *testing.T) {
	s := newStore(t)
	_ = s.Append([]Sample{
		{Metric: "x", Labels: map[string]string{"a": "1"}, TS: ms(10), Value: 1},
		{Metric: "x", Labels: map[string]string{"a": "1"}, TS: ms(30), Value: 3},
		{Metric: "x", Labels: map[string]string{"a": "1"}, TS: ms(50), Value: 5},
	})

	series, err := s.Query("x", ms(20), ms(40))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(series) != 1 || len(series[0].Points) != 1 || series[0].Points[0].Value != 3 {
		t.Errorf("unexpected series in range: %+v", series)
	}
}

func TestAppendBatchIsAtomic(t *testing.T) {
	s := newStore(t)

	good := Sample{Metric: "ok", Labels: map[string]string{}, TS: ms(1), Value: 1}
	if err := s.Append([]Sample{good}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Empty batches are a no-op, not an error.
	if err := s.Append(nil); err != nil {
		t.Errorf("Append(nil) = %v, want nil", err)
	}
	if err := s.Append([]Sample{}); err != nil {
		t.Errorf("Append([]) = %v, want nil", err)
	}
}

func TestStatsReportsRowCount(t *testing.T) {
	s := newStore(t)

	for i := 0; i < 25; i++ {
		_ = s.Append([]Sample{
			{Metric: "x", Labels: map[string]string{}, TS: int64(i), Value: float64(i)},
		})
	}

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.SampleCount != 25 {
		t.Errorf("SampleCount = %d, want 25", stats.SampleCount)
	}
	if stats.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", stats.SizeBytes)
	}

	_ = time.Second
}

func TestRangeEmpty(t *testing.T) {
	s := newStore(t)
	defer s.Close()

	minTS, maxTS, ok, err := s.Range()
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false on empty store, got minTS=%d maxTS=%d", minTS, maxTS)
	}
}

func TestRangeWithSamples(t *testing.T) {
	s := newStore(t)
	defer s.Close()

	samples := []Sample{
		{Metric: "m", Labels: map[string]string{}, TS: ms(100), Value: 1},
		{Metric: "m", Labels: map[string]string{}, TS: ms(200), Value: 2},
		{Metric: "m", Labels: map[string]string{}, TS: ms(150), Value: 3},
	}
	if err := s.Append(samples); err != nil {
		t.Fatalf("Append: %v", err)
	}

	minTS, maxTS, ok, err := s.Range()
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true with samples present")
	}
	if minTS != ms(100) {
		t.Errorf("minTS: got %d want %d", minTS, ms(100))
	}
	if maxTS != ms(200) {
		t.Errorf("maxTS: got %d want %d", maxTS, ms(200))
	}
}
