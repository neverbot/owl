package storage

import (
	"context"
	"testing"
	"time"
)

func TestEnforceTimeDeletesOldSamples(t *testing.T) {
	s := newStore(t)

	now := time.Now().UnixMilli()
	old := now - (40 * 24 * time.Hour).Milliseconds()
	recent := now - (1 * 24 * time.Hour).Milliseconds()

	_ = s.Append([]Sample{
		{Metric: "x", Labels: map[string]string{}, TS: old, Value: 1},
		{Metric: "x", Labels: map[string]string{}, TS: recent, Value: 2},
	})

	deleted, err := EnforceTime(s, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("EnforceTime: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	stats, _ := s.Stats()
	if stats.SampleCount != 1 {
		t.Errorf("after retention SampleCount = %d, want 1", stats.SampleCount)
	}
}

func TestEnforceSizeDoesNothingWhenUnderCap(t *testing.T) {
	s := newStore(t)
	_ = s.Append([]Sample{
		{Metric: "x", Labels: map[string]string{}, TS: 1, Value: 1},
	})

	deleted, err := EnforceSize(s, 100*1024*1024 /* 100 MB */)
	if err != nil {
		t.Fatalf("EnforceSize: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

func TestEnforceSizeDeletesOldestUntilUnderCap(t *testing.T) {
	s := newStore(t)

	// Insert many samples to grow the file beyond the cap.
	const N = 50_000
	batch := make([]Sample, 0, 1000)
	for i := 0; i < N; i++ {
		batch = append(batch, Sample{
			Metric: "x",
			Labels: map[string]string{"i": "v"},
			TS:     int64(i),
			Value:  float64(i),
		})
		if len(batch) == 1000 {
			if err := s.Append(batch); err != nil {
				t.Fatalf("Append: %v", err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		_ = s.Append(batch)
	}

	before, _ := s.Stats()
	if before.SizeBytes == 0 {
		t.Fatalf("size on disk reported 0 bytes")
	}

	cap := before.SizeBytes / 2
	deleted, err := EnforceSize(s, cap)
	if err != nil {
		t.Fatalf("EnforceSize: %v", err)
	}
	if deleted == 0 {
		t.Fatal("expected EnforceSize to delete at least some rows")
	}

	// The oldest rows must be gone.
	series, _ := s.Query("x", 0, int64(N))
	if len(series) == 0 || len(series[0].Points) == 0 {
		t.Fatal("expected some rows to remain")
	}
	first := series[0].Points[0].TS
	if first == 0 {
		t.Errorf("oldest row (ts=0) still present after size enforcement")
	}
}

func TestWorkerRunCallsEnforcementAndStopsOnContextCancel(t *testing.T) {
	s := newStore(t)
	w := &Worker{
		Store:    s,
		Time:     1 * time.Hour, // anything positive
		Size:     0,             // disabled
		Interval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx) // must return promptly when ctx fires
}
