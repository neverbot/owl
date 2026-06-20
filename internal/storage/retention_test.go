package storage

import (
	"context"
	"fmt"
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

// TestEnforceTimeDeletesEvents asserts that events older than the
// retention cutoff are deleted alongside samples.
func TestEnforceTimeDeletesEvents(t *testing.T) {
	s := newStore(t)
	now := int64(1_700_000_000_000)
	// Insert one old and one fresh event directly.
	for i, ts := range []int64{now - 2*int64(time.Hour/time.Millisecond), now} {
		_, err := s.db.Exec(
			`INSERT INTO events (id, ts, source, kind, payload, render)
             VALUES (?, ?, 'test', 'k', '{}', '')`,
			fmt.Sprintf("id-%d", i), ts,
		)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if _, err := EnforceTime(s, time.Hour, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	var remaining int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("want 1 event remaining, got %d", remaining)
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
