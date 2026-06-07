package storage

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/neverbot/owl/internal/storage/chunkenc"
)

// chunkEncodingGorillaV1 is the only encoding owl writes today. Stored
// per chunk so a future format (zstd-wrapped, doubles delta, …) can
// coexist with old data.
const chunkEncodingGorillaV1 = 1

// Flusher periodically encodes head samples older than headWindow into
// Gorilla chunks. Designed to live in a goroutine for the lifetime of
// the process; cancellation via ctx.
type Flusher struct {
	Store       *Store
	HeadWindow  time.Duration // samples with ts < (now - HeadWindow) are eligible for flush
	Interval    time.Duration // how often to scan for eligible samples
	MinSamples  int           // minimum samples in a series's eligible range before we bother flushing; default 16
	MaxPerChunk int           // maximum samples per chunk; default 1000 (Gorilla sweet spot)
}

// Run blocks until ctx is cancelled. The first tick fires after
// Interval so startup isn't followed by an immediate read of an
// empty head.
func (f *Flusher) Run(ctx context.Context) {
	t := time.NewTicker(f.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := f.FlushOnce(ctx, time.Now().UnixMilli()); err != nil {
				slog.Error("chunk flush failed", "err", err)
				continue
			}
			// Reclaim the disk pages the flush just freed. Without
			// this, SQLite keeps the file the same size on disk
			// after the head DELETE and the file only shrinks when
			// the retention worker decides to VACUUM. Tying VACUUM
			// to the flush cadence (~10 min by default) keeps the
			// on-disk size honest.
			if err := f.Store.Vacuum(); err != nil {
				slog.Error("post-flush vacuum failed", "err", err)
			}
		}
	}
}

// FlushOnce performs a single flush pass: identifies series with
// eligible head samples and flushes them. Exported for tests and for
// the migration tool.
func (f *Flusher) FlushOnce(ctx context.Context, nowMs int64) error {
	minSamples := f.MinSamples
	if minSamples <= 0 {
		minSamples = 16
	}
	maxPerChunk := f.MaxPerChunk
	if maxPerChunk <= 0 {
		maxPerChunk = 1000
	}
	headWindow := f.HeadWindow
	if headWindow <= 0 {
		headWindow = 2 * time.Hour
	}
	cutoff := nowMs - headWindow.Milliseconds()

	// Find series with at least one sample below the cutoff. A series
	// returned here may still flush zero rows if the count is under
	// minSamples — that check happens per-series during flush.
	rows, err := f.Store.db.Query(
		`SELECT DISTINCT series_id FROM samples WHERE ts < ?`,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("flush series scan: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("flush scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("flush rows: %w", err)
	}

	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := f.flushSeries(id, cutoff, minSamples, maxPerChunk); err != nil {
			slog.Error("flush series failed", "series_id", id, "err", err)
			// Continue with other series; one bad series should not
			// block the whole tick.
		}
	}
	return nil
}

// flushSeries flushes a single series. Returns the number of samples
// flushed across all chunks produced this call.
func (f *Flusher) flushSeries(seriesID, cutoff int64, minSamples, maxPerChunk int) (int, error) {
	tx, err := f.Store.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT ts, value FROM samples WHERE series_id = ? AND ts < ? ORDER BY ts`,
		seriesID, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("flush read: %w", err)
	}
	type point struct {
		ts  int64
		val float64
	}
	var pts []point
	for rows.Next() {
		var p point
		if err := rows.Scan(&p.ts, &p.val); err != nil {
			rows.Close()
			return 0, fmt.Errorf("flush scan: %w", err)
		}
		pts = append(pts, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("flush rows: %w", err)
	}
	if len(pts) < minSamples {
		return 0, nil
	}

	// Split into chunks of at most maxPerChunk samples.
	insertChunk, err := tx.Prepare(
		`INSERT INTO chunks (series_id, start_ts, end_ts, count, encoding, data)
		 VALUES (?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer insertChunk.Close()

	flushed := 0
	for i := 0; i < len(pts); i += maxPerChunk {
		end := i + maxPerChunk
		if end > len(pts) {
			end = len(pts)
		}
		c := chunkenc.NewChunk()
		for _, p := range pts[i:end] {
			c.Append(p.ts, p.val)
		}
		if _, err := insertChunk.Exec(
			seriesID, c.StartTS(), c.EndTS(), c.Count(), chunkEncodingGorillaV1, c.Bytes(),
		); err != nil {
			return 0, fmt.Errorf("insert chunk: %w", err)
		}
		flushed += c.Count()
	}

	// Wipe the flushed head rows. Bounded by maxFlushedTS to keep us
	// from racing a concurrent insert that landed during this method;
	// any sample at or below maxFlushedTS we know is in our chunks.
	maxFlushedTS := pts[len(pts)-1].ts
	if _, err := tx.Exec(
		`DELETE FROM samples WHERE series_id = ? AND ts <= ?`,
		seriesID, maxFlushedTS,
	); err != nil {
		return 0, fmt.Errorf("delete head: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return flushed, nil
}

// queryChunks reads all chunks for one series that overlap the given
// [from, to] range and decodes them, returning the decoded points
// inside the range.
func (s *Store) queryChunks(seriesID, from, to int64) ([]Point, error) {
	rows, err := s.db.Query(
		`SELECT start_ts, end_ts, count, encoding, data FROM chunks
		 WHERE series_id = ? AND end_ts >= ? AND start_ts <= ?
		 ORDER BY start_ts`,
		seriesID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("chunks query: %w", err)
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var startTS, endTS int64
		var count, encoding int
		var data []byte
		if err := rows.Scan(&startTS, &endTS, &count, &encoding, &data); err != nil {
			return nil, fmt.Errorf("chunks scan: %w", err)
		}
		if encoding != chunkEncodingGorillaV1 {
			return nil, fmt.Errorf("unknown chunk encoding %d", encoding)
		}
		it, err := chunkenc.Iterator(data)
		if err != nil {
			return nil, fmt.Errorf("chunk iter: %w", err)
		}
		for {
			s, ok := it.Next()
			if !ok {
				break
			}
			if s.TS < from || s.TS > to {
				continue
			}
			out = append(out, Point{TS: s.TS, Value: s.Value})
		}
		if err := it.Err(); err != nil {
			return nil, fmt.Errorf("chunk decode: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("chunks rows: %w", err)
	}
	// chunks are emitted in ascending start_ts; samples within each
	// chunk are already ts-sorted, and chunks don't overlap, so the
	// concatenation is already sorted. Belt and braces: re-sort.
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, nil
}
