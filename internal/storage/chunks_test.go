package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFlushMovesHeadIntoChunksAndQueryStillWorks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owl.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Append 1000 samples at a 5 s cadence, ending "now".
	now := time.Now().UnixMilli()
	const n = 1000
	const interval = int64(5000)
	first := now - int64(n)*interval
	batch := make([]Sample, 0, n)
	for i := 0; i < n; i++ {
		batch = append(batch, Sample{
			Metric: "cpu",
			Labels: map[string]string{"job": "host", "cpu": "0"},
			TS:     first + int64(i)*interval,
			Value:  float64(i),
		})
	}
	if err := s.Append(batch); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Head holds everything before flush.
	if headCount := countRows(t, s, "samples"); headCount != n {
		t.Fatalf("head count before flush = %d, want %d", headCount, n)
	}
	if chunkCount := countRows(t, s, "chunks"); chunkCount != 0 {
		t.Fatalf("chunks before flush = %d, want 0", chunkCount)
	}

	// Flush samples older than "now - 1s" — i.e. essentially all.
	f := &Flusher{
		Store:       s,
		HeadWindow:  time.Second,
		Interval:    time.Hour,
		MinSamples:  16,
		MaxPerChunk: 300,
	}
	if err := f.FlushOnce(context.Background(), now); err != nil {
		t.Fatalf("FlushOnce: %v", err)
	}

	// Most samples should now be in chunks; the last few (within the
	// 1 s head window) stay raw.
	headAfter := countRows(t, s, "samples")
	chunksAfter := countRows(t, s, "chunks")
	t.Logf("after flush: head=%d, chunks=%d (3-4 chunks of <=300 samples each)", headAfter, chunksAfter)
	if chunksAfter < 3 || chunksAfter > 5 {
		t.Errorf("chunks after flush = %d, want 3-4 (1000 samples / 300 per chunk)", chunksAfter)
	}
	if headAfter >= n {
		t.Errorf("flush did not reduce head: still %d rows", headAfter)
	}

	// Query the entire range and verify we recovered every sample.
	series, err := s.Query("cpu", first, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("Query returned %d series, want 1", len(series))
	}
	if len(series[0].Points) != n {
		t.Fatalf("Query returned %d points, want %d", len(series[0].Points), n)
	}
	// Spot-check first, middle, last values.
	for _, idx := range []int{0, n / 2, n - 1} {
		got := series[0].Points[idx]
		wantTS := first + int64(idx)*interval
		wantVal := float64(idx)
		if got.TS != wantTS {
			t.Errorf("point[%d] ts = %d, want %d", idx, got.TS, wantTS)
		}
		if got.Value != wantVal {
			t.Errorf("point[%d] value = %v, want %v", idx, got.Value, wantVal)
		}
	}
}

func TestFlushSkipsSeriesBelowMinSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owl.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	now := time.Now().UnixMilli()
	if err := s.Append([]Sample{
		{Metric: "m", Labels: nil, TS: now - 1_000_000, Value: 1},
		{Metric: "m", Labels: nil, TS: now - 999_000, Value: 2},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	f := &Flusher{Store: s, HeadWindow: time.Second, MinSamples: 16, MaxPerChunk: 1000}
	if err := f.FlushOnce(context.Background(), now); err != nil {
		t.Fatalf("FlushOnce: %v", err)
	}
	if c := countRows(t, s, "chunks"); c != 0 {
		t.Errorf("min samples filter ignored: %d chunks", c)
	}
	if c := countRows(t, s, "samples"); c != 2 {
		t.Errorf("head erroneously drained: %d samples", c)
	}
}

func TestEnforceTimeDropsOldChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owl.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	now := time.Now().UnixMilli()
	old := now - (10 * 24 * time.Hour).Milliseconds()
	batch := []Sample{}
	for i := 0; i < 100; i++ {
		batch = append(batch, Sample{
			Metric: "m", Labels: nil,
			TS:    old + int64(i)*1000,
			Value: float64(i),
		})
	}
	if err := s.Append(batch); err != nil {
		t.Fatalf("Append: %v", err)
	}
	f := &Flusher{Store: s, HeadWindow: time.Second, MinSamples: 16, MaxPerChunk: 1000}
	if err := f.FlushOnce(context.Background(), now); err != nil {
		t.Fatalf("FlushOnce: %v", err)
	}
	chunksBefore := countRows(t, s, "chunks")
	if chunksBefore == 0 {
		t.Fatal("flush produced no chunks")
	}

	deleted, err := EnforceTime(s, 24*time.Hour, now)
	if err != nil {
		t.Fatalf("EnforceTime: %v", err)
	}
	if deleted == 0 {
		t.Error("EnforceTime did not delete any old samples")
	}
	if c := countRows(t, s, "chunks"); c != 0 {
		t.Errorf("chunks after EnforceTime = %d, want 0", c)
	}
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
