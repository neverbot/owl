package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFoundationSmoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owl.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	now := time.Now().UnixMilli()
	old := now - (60 * 24 * time.Hour).Milliseconds()

	if err := s.Append([]Sample{
		{Metric: "cpu", Labels: map[string]string{"job": "host"}, TS: old, Value: 1.0},
		{Metric: "cpu", Labels: map[string]string{"job": "host"}, TS: now, Value: 2.0},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if _, err := EnforceTime(s, 30*24*time.Hour, now); err != nil {
		t.Fatalf("EnforceTime: %v", err)
	}

	series, err := s.Query("cpu", 0, now+1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(series) != 1 || len(series[0].Points) != 1 || series[0].Points[0].Value != 2.0 {
		t.Fatalf("unexpected post-retention state: %+v", series)
	}

	stats, _ := s.Stats()
	if stats.SampleCount != 1 {
		t.Errorf("SampleCount = %d, want 1", stats.SampleCount)
	}
}
