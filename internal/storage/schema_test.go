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

func TestMigrateCreatesSamplesTable(t *testing.T) {
	db := openTempDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='samples'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("samples table missing: %v", err)
	}

	var indexName string
	err = db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='samples' AND name LIKE 'idx_samples_%'`,
	).Scan(&indexName)
	if err != nil {
		t.Fatalf("expected index on samples missing: %v", err)
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
