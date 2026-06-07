package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "owl.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateCreatesV3Schema(t *testing.T) {
	db := openTempDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	for _, table := range []string{"series", "samples", "chunks"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("user_version = %d, want %d", version, currentSchemaVersion)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTempDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestMigrateDropsLegacySchemaAndKeepsRunning(t *testing.T) {
	db := openTempDB(t)
	// Simulate a v0 (legacy) DB: original denormalised samples table
	// with some data, and user_version left at 0.
	for _, stmt := range []string{
		`CREATE TABLE samples (
			metric TEXT NOT NULL,
			labels TEXT NOT NULL,
			ts     INTEGER NOT NULL,
			value  REAL NOT NULL
		)`,
		`INSERT INTO samples VALUES ('cpu', 'job=host', 100, 1.0)`,
		`INSERT INTO samples VALUES ('cpu', 'job=host', 200, 2.0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed legacy: %v", err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate from v0: %v", err)
	}
	// The new schema must be in place and the legacy data must be gone.
	for _, table := range []string{"series", "samples", "chunks"} {
		var name string
		if err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name); err != nil {
			t.Errorf("table %q missing after migration: %v", table, err)
		}
	}
	var headCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&headCount); err != nil {
		t.Fatalf("count samples: %v", err)
	}
	if headCount != 0 {
		t.Errorf("legacy samples not dropped: %d remaining", headCount)
	}
}
