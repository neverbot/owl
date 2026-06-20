package storage

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// Store is the persistent SQLite-backed time-series store.
type Store struct {
	db     *sql.DB
	path   string
	series *seriesCache
}

// Stats summarises the database for /api/stats and the retention worker.
type Stats struct {
	SampleCount int64
	SizeBytes   int64
}

// Open opens (or creates) the SQLite file at path, applies pragmas,
// runs migrations and warms the series cache. The directory of path
// must exist.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	// One writer at a time. modernc.org/sqlite supports multiple readers
	// transparently through WAL.
	db.SetMaxOpenConns(1)

	if err := ApplyPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := Migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	cache, err := newSeriesCache(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := cache.warm(db); err != nil {
		_ = cache.close()
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db, path: path, series: cache}, nil
}

// DB returns the underlying *sql.DB so adjacent subsystems (events
// store) can reuse the same single-writer connection.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s.series != nil {
		_ = s.series.close()
	}
	return s.db.Close()
}

// Append writes a batch of samples in a single transaction. Each
// sample's (metric, labels) tuple is interned via the series cache; a
// freshly-seen series triggers one extra DB round-trip the first time
// and is a pure map lookup thereafter. An empty batch is a no-op.
func (s *Store) Append(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	// Resolve series ids first so the insert transaction below sees
	// only fast inserts. The series upsert is its own prepared
	// statement on s.db, separate from this transaction.
	ids := make([]int64, len(samples))
	for i, smp := range samples {
		id, err := s.series.idFor(smp.Metric, CanonicalLabels(smp.Labels))
		if err != nil {
			return err
		}
		ids[i] = id
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// INSERT OR REPLACE so a re-scrape that produces the same (series,
	// ts) overwrites rather than failing the whole batch. owl's
	// scrapers never re-emit historic timestamps in normal operation;
	// REPLACE keeps us robust against the rare clock-skew case.
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO samples (series_id, ts, value) VALUES (?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()
	for i, smp := range samples {
		if _, err := stmt.Exec(ids[i], smp.TS, smp.Value); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec: %w", err)
		}
	}
	return tx.Commit()
}

// Query returns all series for the given metric whose ts falls in
// [from, to]. Series are sorted by their canonical label string.
//
// Reads both the head table (raw samples) and the chunks table
// (compressed blobs), merging by ts. Either source may be empty for
// a given series; head-only series (very recent metrics) and
// chunks-only series (older metrics already flushed) both work.
func (s *Store) Query(metric string, from, to int64) ([]Series, error) {
	// Resolve the matching series first so we know exactly which ids
	// to fetch samples / chunks for.
	seriesRows, err := s.db.Query(
		`SELECT id, labels FROM series WHERE metric = ?`, metric,
	)
	if err != nil {
		return nil, fmt.Errorf("series scan: %w", err)
	}
	type seriesInfo struct {
		id     int64
		labels string
	}
	var seriesList []seriesInfo
	for seriesRows.Next() {
		var info seriesInfo
		if err := seriesRows.Scan(&info.id, &info.labels); err != nil {
			seriesRows.Close()
			return nil, fmt.Errorf("series scan: %w", err)
		}
		seriesList = append(seriesList, info)
	}
	seriesRows.Close()
	if err := seriesRows.Err(); err != nil {
		return nil, fmt.Errorf("series rows: %w", err)
	}

	out := make([]Series, 0, len(seriesList))
	for _, info := range seriesList {
		headPoints, err := s.queryHead(info.id, from, to)
		if err != nil {
			return nil, err
		}
		chunkPoints, err := s.queryChunks(info.id, from, to)
		if err != nil {
			return nil, err
		}
		points := mergePoints(chunkPoints, headPoints)
		if len(points) == 0 {
			continue
		}
		out = append(out, Series{
			Metric: metric,
			Labels: parseCanonicalLabels(info.labels),
			Points: points,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return CanonicalLabels(out[i].Labels) < CanonicalLabels(out[j].Labels)
	})
	return out, nil
}

// queryHead reads raw samples for one series in the time range.
func (s *Store) queryHead(seriesID, from, to int64) ([]Point, error) {
	rows, err := s.db.Query(
		`SELECT ts, value FROM samples
		 WHERE series_id = ? AND ts >= ? AND ts <= ?
		 ORDER BY ts`,
		seriesID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("head query: %w", err)
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var ts int64
		var val float64
		if err := rows.Scan(&ts, &val); err != nil {
			return nil, fmt.Errorf("head scan: %w", err)
		}
		out = append(out, Point{TS: ts, Value: val})
	}
	return out, rows.Err()
}

// mergePoints assumes both inputs are sorted by ts ascending and
// non-overlapping (chunks hold flushed samples, head holds recent
// ones the flush worker hasn't touched yet).
func mergePoints(a, b []Point) []Point {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]Point, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i].TS < b[j].TS:
			out = append(out, a[i])
			i++
		case a[i].TS > b[j].TS:
			out = append(out, b[j])
			j++
		default:
			// Same ts: head wins (newer write).
			out = append(out, b[j])
			i++
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// Stats reports the on-disk size and the sample row count.
// Stats reports both the on-disk footprint and the row count. The
// row count requires a full table scan in SQLite, so callers that
// only need the size should use Size() instead.
//
// SampleCount aggregates raw head rows + samples held inside chunk
// blobs, so retention's "how many rows do we own?" question still has
// a single answer.
func (s *Store) Stats() (Stats, error) {
	var st Stats
	var head, chunked sql.NullInt64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&head); err != nil {
		return st, fmt.Errorf("count head: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(count), 0) FROM chunks`).Scan(&chunked); err != nil {
		return st, fmt.Errorf("count chunks: %w", err)
	}
	st.SampleCount = head.Int64 + chunked.Int64
	size, err := s.Size()
	if err != nil {
		return st, err
	}
	st.SizeBytes = size
	return st, nil
}

// Vacuum runs a WAL checkpoint and `VACUUM`. After a flush pass moves
// many head rows into chunks, SQLite leaves the freed pages on the
// freelist but does not shrink the file. Vacuum reclaims that space.
// It is also what the retention worker calls when it deletes data, so
// the on-disk size mirrors logical size.
//
// VACUUM holds a write lock for the duration, so it is intentionally
// called from low-frequency callers (flusher, retention) rather than
// per-write.
func (s *Store) Vacuum() error {
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	if _, err := s.db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}

// Size returns the on-disk footprint across every SQLite-managed file
// (the main `.db` plus the `-wal` and `-shm` sidecar files when
// present). Cheap — three os.Stat calls and no database query — so
// the retention worker can call it every tick without scanning the
// samples table.
func (s *Store) Size() (int64, error) {
	fi, err := os.Stat(s.path)
	if err != nil {
		return 0, fmt.Errorf("stat: %w", err)
	}
	total := fi.Size()
	for _, suffix := range []string{"-wal", "-shm"} {
		if fi, err := os.Stat(s.path + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total, nil
}

// Range returns the smallest and largest ts in the samples table, in
// milliseconds since epoch. ok is false when the table is empty
// (minTS and maxTS are then meaningless). Considers both head and
// chunks so the calendar UI sees the full retention window even when
// most data has already been flushed.
func (s *Store) Range() (minTS, maxTS int64, ok bool, err error) {
	var headMin, headMax, chunkMin, chunkMax sql.NullInt64
	row := s.db.QueryRow(`SELECT MIN(ts), MAX(ts) FROM samples`)
	if err := row.Scan(&headMin, &headMax); err != nil {
		return 0, 0, false, fmt.Errorf("range head: %w", err)
	}
	row = s.db.QueryRow(`SELECT MIN(start_ts), MAX(end_ts) FROM chunks`)
	if err := row.Scan(&chunkMin, &chunkMax); err != nil {
		return 0, 0, false, fmt.Errorf("range chunks: %w", err)
	}
	if !headMin.Valid && !chunkMin.Valid {
		return 0, 0, false, nil
	}
	switch {
	case headMin.Valid && chunkMin.Valid:
		minTS = minInt64(headMin.Int64, chunkMin.Int64)
		maxTS = maxInt64(headMax.Int64, chunkMax.Int64)
	case headMin.Valid:
		minTS, maxTS = headMin.Int64, headMax.Int64
	default:
		minTS, maxTS = chunkMin.Int64, chunkMax.Int64
	}
	return minTS, maxTS, true, nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// parseCanonicalLabels reverses CanonicalLabels. It assumes the input was
// produced by CanonicalLabels (so no escaping is needed).
func parseCanonicalLabels(s string) map[string]string {
	if s == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		out[kv[:eq]] = kv[eq+1:]
	}
	return out
}
