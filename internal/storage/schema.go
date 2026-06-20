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
//	v4: + events, event_source_state
const currentSchemaVersion = 4

// schemaStatements creates the v4 schema from scratch. All statements
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

	// Events: discrete occurrences emitted by external sources (e.g.
	// GitHub deployments, CI runs, alert firings). id is a
	// source-assigned opaque string; payload and render are JSON blobs.
	`CREATE TABLE IF NOT EXISTS events (
		id      TEXT PRIMARY KEY,
		ts      INTEGER NOT NULL,
		source  TEXT NOT NULL,
		kind    TEXT NOT NULL,
		payload TEXT NOT NULL,
		render  TEXT NOT NULL
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS events_ts ON events(ts)`,
	`CREATE INDEX IF NOT EXISTS events_source_ts ON events(source, ts)`,

	// event_source_state: per-source polling cursor so the event
	// collector can resume from the last seen position after restart.
	`CREATE TABLE IF NOT EXISTS event_source_state (
		source     TEXT PRIMARY KEY,
		cursor     TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`,
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
			`DROP TABLE IF EXISTS events`,
			`DROP TABLE IF EXISTS event_source_state`,
			// Older schemas may have left these indexes orphaned even
			// without the table; clean them up too.
			`DROP INDEX IF EXISTS idx_samples_metric_ts`,
			`DROP INDEX IF EXISTS idx_samples_ts`,
			`DROP INDEX IF EXISTS idx_chunks_end_ts`,
			`DROP INDEX IF EXISTS events_ts`,
			`DROP INDEX IF EXISTS events_source_ts`,
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
