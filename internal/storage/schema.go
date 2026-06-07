package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// currentSchemaVersion is bumped on incompatible schema changes. The
// startup migration drops user data when the on-disk version is lower
// (see Migrate). owl's product profile — short-lived metrics, small
// self-host deployments — makes this trade acceptable; operators who
// want to preserve old data run the dedicated migration tool offline.
//
//	v0: initial schema (legacy samples(metric, labels, ts, value))
//	v3: series + samples(WITHOUT ROWID) + chunks (current)
const currentSchemaVersion = 3

// schemaStatements creates the v3 schema from scratch. All statements
// are idempotent (IF NOT EXISTS) so Migrate can re-run safely.
var schemaStatements = []string{
	// One row per unique (metric, labels). Samples reference the id
	// instead of the strings, which is where most of the per-sample
	// disk savings come from — high-cardinality docker series no
	// longer pay their ~130 bytes of metric+labels overhead on every
	// scrape.
	`CREATE TABLE IF NOT EXISTS series (
		id     INTEGER PRIMARY KEY,
		metric TEXT NOT NULL,
		labels TEXT NOT NULL,
		UNIQUE(metric, labels)
	) STRICT`,

	// Head: recent (raw, point-writable) samples. WITHOUT ROWID makes
	// the (series_id, ts) primary key the actual storage — no separate
	// rowid column and no extra index for the PK. Samples land here on
	// Append; the flush worker periodically encodes older head ranges
	// into compressed chunks.
	`CREATE TABLE IF NOT EXISTS samples (
		series_id INTEGER NOT NULL,
		ts        INTEGER NOT NULL,
		value     REAL NOT NULL,
		PRIMARY KEY (series_id, ts)
	) STRICT, WITHOUT ROWID`,

	// Index on ts alone for the retention DELETE that scans across all
	// series. Same shape as the legacy schema's secondary index.
	`CREATE INDEX IF NOT EXISTS idx_samples_ts ON samples (ts)`,

	// Chunks: compressed (Gorilla XOR + delta-of-delta) sample blobs.
	// The (series_id, start_ts) primary key gives us efficient range
	// lookups when answering queries.
	`CREATE TABLE IF NOT EXISTS chunks (
		series_id INTEGER NOT NULL,
		start_ts  INTEGER NOT NULL,
		end_ts    INTEGER NOT NULL,
		count     INTEGER NOT NULL,
		encoding  INTEGER NOT NULL,
		data      BLOB NOT NULL,
		PRIMARY KEY (series_id, start_ts)
	) STRICT, WITHOUT ROWID`,

	// Secondary index over end_ts so EnforceTime can locate all
	// chunks fully older than the cutoff in O(log n).
	`CREATE INDEX IF NOT EXISTS idx_chunks_end_ts ON chunks (end_ts)`,
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

// Migrate brings the database to currentSchemaVersion. The strategy
// is intentionally simple: if the on-disk user_version is lower than
// what the binary expects, drop the data tables and rebuild. The
// project's storage ADR (context/decisions/2026-06-07-storage-
// footprint.md) explains why this lossy default is the right trade
// for owl's user profile.
//
// A fresh database has user_version=0; the migration there is just a
// table create, no data loss.
func Migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("on-disk schema version %d is newer than binary %d; refusing to downgrade",
			version, currentSchemaVersion)
	}
	if version < currentSchemaVersion {
		// Probe how much data we are about to drop so the log line
		// makes the loss visible.
		var legacyCount int64
		if err := db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&legacyCount); err == nil && legacyCount > 0 {
			slog.Warn("incompatible storage schema; dropping all samples and starting fresh",
				"old_version", version, "new_version", currentSchemaVersion, "samples_dropped", legacyCount)
		}
		for _, stmt := range []string{
			`DROP TABLE IF EXISTS samples`,
			`DROP TABLE IF EXISTS series`,
			`DROP TABLE IF EXISTS chunks`,
			// Older schemas may have left these indexes orphaned even
			// without the table; clean them up too.
			`DROP INDEX IF EXISTS idx_samples_metric_ts`,
			`DROP INDEX IF EXISTS idx_samples_ts`,
			`DROP INDEX IF EXISTS idx_chunks_end_ts`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("drop: %w", err)
			}
		}
	}
	for _, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create: %w", err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, currentSchemaVersion)); err != nil {
		return fmt.Errorf("write user_version: %w", err)
	}
	return nil
}
