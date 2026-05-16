package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// EnforceTime deletes samples older than (now - keep). Returns the
// number of rows deleted.
//
// A cheap probe (`LIMIT 1` against the secondary ts index) short-
// circuits the call when nothing is eligible — the common case once
// the time horizon has been reached, since most ticks see no rows
// past the cutoff. Without this, the DELETE would scan the (metric,
// ts) index from the top on every tick even when there is no work to
// do.
func EnforceTime(s *Store, keep time.Duration, now int64) (int64, error) {
	cutoff := now - keep.Milliseconds()
	var probe int
	err := s.db.QueryRow(`SELECT 1 FROM samples WHERE ts < ? LIMIT 1`, cutoff).Scan(&probe)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("probe: %w", err)
	}
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

// EnforceSize deletes the oldest samples until the on-disk size is at
// or below cap. cap <= 0 disables the check. Returns rows deleted.
//
// Hot path: a single Size() call (three os.Stat() syscalls, no DB
// query) checks whether the cap has been crossed. If not — the
// overwhelmingly common case — the function returns immediately.
// Only when the cap is exceeded does the loop fall back to Stats()
// for the row count needed to compute the 10 % batch.
//
// Algorithm once over cap: while size > cap, delete the oldest 10 %
// of rows, checkpoint, re-measure. Bounded loop (max 20 passes) to
// guarantee termination even in pathological cases.
func EnforceSize(s *Store, cap int64) (int64, error) {
	if cap <= 0 {
		return 0, nil
	}
	size, err := s.Size()
	if err != nil {
		return 0, err
	}
	if size <= cap {
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
