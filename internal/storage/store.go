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
	db   *sql.DB
	path string
}

// Stats summarises the database for /api/stats and the retention worker.
type Stats struct {
	SampleCount int64
	SizeBytes   int64
}

// Open opens (or creates) the SQLite file at path, applies pragmas, and
// runs migrations. The directory of path must exist.
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
	return &Store{db: db, path: path}, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// Append writes a batch of samples in a single transaction. An empty
// batch is a no-op.
func (s *Store) Append(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO samples (metric, labels, ts, value) VALUES (?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, smp := range samples {
		if _, err := stmt.Exec(smp.Metric, CanonicalLabels(smp.Labels), smp.TS, smp.Value); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec: %w", err)
		}
	}
	return tx.Commit()
}

// Query returns all series for the given metric whose ts falls in
// [from, to]. Series are sorted by their canonical label string.
func (s *Store) Query(metric string, from, to int64) ([]Series, error) {
	rows, err := s.db.Query(
		`SELECT labels, ts, value FROM samples
		 WHERE metric = ? AND ts >= ? AND ts <= ?
		 ORDER BY labels, ts`,
		metric, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	bucket := make(map[string]*Series)
	order := []string{}

	for rows.Next() {
		var labelsStr string
		var ts int64
		var value float64
		if err := rows.Scan(&labelsStr, &ts, &value); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		ser, ok := bucket[labelsStr]
		if !ok {
			ser = &Series{
				Metric: metric,
				Labels: parseCanonicalLabels(labelsStr),
			}
			bucket[labelsStr] = ser
			order = append(order, labelsStr)
		}
		ser.Points = append(ser.Points, Point{TS: ts, Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	sort.Strings(order)
	out := make([]Series, 0, len(order))
	for _, k := range order {
		out = append(out, *bucket[k])
	}
	return out, nil
}

// Stats reports the on-disk size and the sample row count.
func (s *Store) Stats() (Stats, error) {
	var st Stats
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&st.SampleCount); err != nil {
		return st, fmt.Errorf("count: %w", err)
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		return st, fmt.Errorf("stat: %w", err)
	}
	st.SizeBytes = fi.Size()
	return st, nil
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
