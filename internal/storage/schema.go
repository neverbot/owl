package storage

import (
	"database/sql"
	"fmt"
)

// schemaStatements is the ordered list of CREATE statements that define
// Owl's storage schema. Statements are idempotent (IF NOT EXISTS) so
// Migrate can be safely re-run on every startup.
//
// The schema is intentionally simple in the foundation phase: a single
// denormalised samples table with a compound index on (metric, ts). If
// label cardinality becomes a problem we will normalise into a
// series+samples pair in a future migration.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS samples (
		metric TEXT    NOT NULL,
		labels TEXT    NOT NULL,
		ts     INTEGER NOT NULL,
		value  REAL    NOT NULL
	) STRICT`,

	`CREATE INDEX IF NOT EXISTS idx_samples_metric_ts
		ON samples (metric, ts)`,

	// Secondary index over ts alone so the retention worker can ask
	// "is there anything older than X?" with a cheap LIMIT 1 probe,
	// and so DELETE WHERE ts<? does not have to scan every (metric,
	// ts) bucket. Costs roughly one extra B-tree insert per Append
	// and ~10 MB of disk per million rows; well worth it given the
	// worker runs unconditionally on every tick.
	`CREATE INDEX IF NOT EXISTS idx_samples_ts ON samples (ts)`,
}

// pragmas are applied once at open time. They configure SQLite for a
// write-heavy time-series workload on a single-writer process.
var pragmas = []string{
	`PRAGMA journal_mode = WAL`,
	`PRAGMA synchronous  = NORMAL`,
	`PRAGMA temp_store   = MEMORY`,
	`PRAGMA foreign_keys = ON`,
}

// ApplyPragmas runs the connection-level PRAGMAs.
func ApplyPragmas(db *sql.DB) error {
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	return nil
}

// Migrate creates the schema if missing. Safe to call repeatedly.
func Migrate(db *sql.DB) error {
	for _, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate %q: %w", stmt, err)
		}
	}
	return nil
}
