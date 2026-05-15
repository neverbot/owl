package storage

import (
	"context"
	"fmt"
	"time"
)

// EnforceTime deletes samples older than (now - keep). Returns the number
// of rows deleted.
func EnforceTime(s *Store, keep time.Duration, now int64) (int64, error) {
	cutoff := now - keep.Milliseconds()
	res, err := s.db.Exec(`DELETE FROM samples WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("enforce time: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		// Reclaim space so EnforceSize sees the new on-disk footprint.
		if _, err := s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
			return n, fmt.Errorf("wal checkpoint: %w", err)
		}
	}
	return n, nil
}

// EnforceSize deletes the oldest samples until the on-disk size is at or
// below cap. cap <= 0 disables the check. Returns rows deleted.
//
// The algorithm: while size > cap, delete the oldest 10 % of rows,
// checkpoint, re-measure. Bounded loop (max 20 passes) to guarantee
// termination even in pathological cases.
func EnforceSize(s *Store, cap int64) (int64, error) {
	if cap <= 0 {
		return 0, nil
	}
	var total int64
	for pass := 0; pass < 20; pass++ {
		stats, err := s.Stats()
		if err != nil {
			return total, err
		}
		if stats.SizeBytes <= cap {
			return total, nil
		}
		if stats.SampleCount == 0 {
			return total, nil
		}

		// Drop the oldest 10 % (at least 1 row).
		toDrop := stats.SampleCount / 10
		if toDrop < 1 {
			toDrop = 1
		}
		res, err := s.db.Exec(
			`DELETE FROM samples WHERE rowid IN (
			   SELECT rowid FROM samples ORDER BY ts LIMIT ?
			 )`, toDrop,
		)
		if err != nil {
			return total, fmt.Errorf("enforce size pass %d: %w", pass, err)
		}
		n, _ := res.RowsAffected()
		total += n

		if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return total, fmt.Errorf("wal checkpoint: %w", err)
		}
		if _, err := s.db.Exec(`VACUUM`); err != nil {
			return total, fmt.Errorf("vacuum: %w", err)
		}
	}
	return total, nil
}

// Worker periodically applies the dual time+size retention policy.
type Worker struct {
	Store    *Store
	Time     time.Duration // > 0 enables time-based retention
	Size     int64         // > 0 enables size-based retention
	Interval time.Duration // how often to run
}

// Run blocks until ctx is cancelled, applying retention on each tick.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if w.Time > 0 {
				_, _ = EnforceTime(w.Store, w.Time, time.Now().UnixMilli())
			}
			if w.Size > 0 {
				_, _ = EnforceSize(w.Store, w.Size)
			}
		}
	}
}
