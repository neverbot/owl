package storage

import (
	"database/sql"
	"fmt"
	"sync"
)

// seriesCache maps a canonical (metric, labels) key to its series id.
// It is owned by *Store and consulted on every Append. Cold misses
// hit the DB; warm hits are pure in-memory map lookups.
//
// Cache invariant: a key resolves to the same id forever (series rows
// are never deleted). Memory cost is bounded by the number of unique
// active series, typically <1000 in owl deployments.
type seriesCache struct {
	mu   sync.RWMutex
	ids  map[string]int64
	stmt *sql.Stmt
}

func newSeriesCache(db *sql.DB) (*seriesCache, error) {
	// INSERT ... ON CONFLICT DO UPDATE SET id=id RETURNING id is the
	// single-round-trip "insert if absent, give me the id either way"
	// pattern. The no-op DO UPDATE makes the row "match" the conflict
	// so RETURNING fires whether we inserted or hit an existing row.
	stmt, err := db.Prepare(`
		INSERT INTO series (metric, labels) VALUES (?, ?)
		ON CONFLICT (metric, labels) DO UPDATE SET id = id
		RETURNING id`)
	if err != nil {
		return nil, fmt.Errorf("prepare series upsert: %w", err)
	}
	return &seriesCache{ids: make(map[string]int64), stmt: stmt}, nil
}

// close releases the prepared statement.
func (c *seriesCache) close() error {
	if c.stmt != nil {
		return c.stmt.Close()
	}
	return nil
}

// idFor returns the series id for (metric, labels), inserting a new
// row if necessary. The cache key embeds a NUL byte so canonical
// label strings like "a=b" can't collide with metric names.
func (c *seriesCache) idFor(metric, labels string) (int64, error) {
	key := metric + "\x00" + labels
	c.mu.RLock()
	id, ok := c.ids[key]
	c.mu.RUnlock()
	if ok {
		return id, nil
	}
	// Cold path: round-trip to the DB. Two callers racing to insert
	// the same series both succeed (SQLite serialises writes, the
	// second sees the existing row via ON CONFLICT) and both end up
	// caching the same id.
	if err := c.stmt.QueryRow(metric, labels).Scan(&id); err != nil {
		return 0, fmt.Errorf("series upsert: %w", err)
	}
	c.mu.Lock()
	c.ids[key] = id
	c.mu.Unlock()
	return id, nil
}

// warm loads every existing (metric, labels)→id mapping from the
// series table. Called once at Open so the first Append doesn't pay
// N round-trips to re-discover known series after a restart.
func (c *seriesCache) warm(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, metric, labels FROM series`)
	if err != nil {
		return fmt.Errorf("warm: %w", err)
	}
	defer rows.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	for rows.Next() {
		var id int64
		var metric, labels string
		if err := rows.Scan(&id, &metric, &labels); err != nil {
			return fmt.Errorf("warm scan: %w", err)
		}
		c.ids[metric+"\x00"+labels] = id
	}
	return rows.Err()
}
