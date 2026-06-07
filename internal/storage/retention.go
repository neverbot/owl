package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// EnforceTime deletes head samples and chunks older than (now - keep).
// Returns the total number of *samples* deleted (head rows plus the
// `count` of every dropped chunk).
//
// Chunks are dropped only when their end_ts is strictly older than the
// cutoff — a chunk that straddles the boundary is kept whole, so a
// small overshoot is possible. The trade-off is intentional: chunks
// are immutable and re-encoding a partial chunk on every retention
// tick would dwarf the savings.
//
// A cheap probe short-circuits the call when nothing is eligible —
// the common case once the time horizon has been reached.
func EnforceTime(s *Store, keep time.Duration, now int64) (int64, error) {
	cutoff := now - keep.Milliseconds()

	// Probe both head and chunks. Either being non-empty is enough to
	// justify the DELETE.
	var probe int
	probeErr := s.db.QueryRow(`SELECT 1 FROM samples WHERE ts < ? LIMIT 1`, cutoff).Scan(&probe)
	headEligible := probeErr == nil
	if probeErr != nil && probeErr != sql.ErrNoRows {
		return 0, fmt.Errorf("probe head: %w", probeErr)
	}
	probeErr = s.db.QueryRow(`SELECT 1 FROM chunks WHERE end_ts < ? LIMIT 1`, cutoff).Scan(&probe)
	chunksEligible := probeErr == nil
	if probeErr != nil && probeErr != sql.ErrNoRows {
		return 0, fmt.Errorf("probe chunks: %w", probeErr)
	}
	if !headEligible && !chunksEligible {
		return 0, nil
	}

	var totalDeleted int64

	if chunksEligible {
		// Sum the counts of the chunks we're about to drop so the
		// returned figure reflects logical samples, not chunk rows.
		var chunkSamples sql.NullInt64
		if err := s.db.QueryRow(
			`SELECT COALESCE(SUM(count), 0) FROM chunks WHERE end_ts < ?`,
			cutoff,
		).Scan(&chunkSamples); err != nil {
			return 0, fmt.Errorf("sum chunk samples: %w", err)
		}
		if _, err := s.db.Exec(`DELETE FROM chunks WHERE end_ts < ?`, cutoff); err != nil {
			return 0, fmt.Errorf("enforce time chunks: %w", err)
		}
		totalDeleted += chunkSamples.Int64
	}

	if headEligible {
		res, err := s.db.Exec(`DELETE FROM samples WHERE ts < ?`, cutoff)
		if err != nil {
			return totalDeleted, fmt.Errorf("enforce time head: %w", err)
		}
		n, _ := res.RowsAffected()
		totalDeleted += n
	}

	if totalDeleted > 0 {
		// Reclaim space so EnforceSize sees the new on-disk footprint.
		if _, err := s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
			return totalDeleted, fmt.Errorf("wal checkpoint: %w", err)
		}
	}
	return totalDeleted, nil
}

// EnforceSize deletes the oldest samples until the on-disk size is at
// or below cap. cap <= 0 disables the check. Returns rows deleted.
//
// Strategy: drop the oldest chunk first (big rows, cheap to delete),
// then fall back to oldest head rows when chunks are exhausted.
// VACUUM at the end of every pass to actually reclaim space so the
// next size check sees the new footprint.
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
		size, err := s.Size()
		if err != nil {
			return total, err
		}
		if size <= cap {
			return total, nil
		}

		// Prefer dropping the oldest chunk: one row, many samples,
		// big disk impact. Fall back to head rows when no chunks
		// remain.
		var oldestChunkStart sql.NullInt64
		var oldestChunkSeriesID sql.NullInt64
		var oldestChunkSamples sql.NullInt64
		row := s.db.QueryRow(
			`SELECT series_id, start_ts, count FROM chunks
			 ORDER BY start_ts LIMIT 1`,
		)
		if err := row.Scan(&oldestChunkSeriesID, &oldestChunkStart, &oldestChunkSamples); err != nil && err != sql.ErrNoRows {
			return total, fmt.Errorf("locate oldest chunk: %w", err)
		}

		if oldestChunkStart.Valid {
			if _, err := s.db.Exec(
				`DELETE FROM chunks WHERE series_id = ? AND start_ts = ?`,
				oldestChunkSeriesID.Int64, oldestChunkStart.Int64,
			); err != nil {
				return total, fmt.Errorf("drop chunk: %w", err)
			}
			total += oldestChunkSamples.Int64
		} else {
			// No chunks — drop the oldest 10 % of head rows.
			var headCount int64
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&headCount); err != nil {
				return total, fmt.Errorf("count head: %w", err)
			}
			if headCount == 0 {
				return total, nil
			}
			toDrop := headCount / 10
			if toDrop < 1 {
				toDrop = 1
			}
			res, err := s.db.Exec(
				`DELETE FROM samples WHERE (series_id, ts) IN (
				   SELECT series_id, ts FROM samples ORDER BY ts LIMIT ?
				 )`, toDrop,
			)
			if err != nil {
				return total, fmt.Errorf("enforce size head pass %d: %w", pass, err)
			}
			n, _ := res.RowsAffected()
			total += n
		}

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
