package events

import (
	"database/sql"
	"testing"

	"github.com/neverbot/owl/internal/storage"
	_ "modernc.org/sqlite"
)

// openTestStore returns a fresh events.Store backed by an in-memory
// SQLite with the full schema applied.
func openTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.ApplyPragmas(db); err != nil {
		t.Fatalf("pragmas: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewStore(db), db
}

// TestInsertEventIdempotent asserts INSERT OR IGNORE swallows
// duplicate ids without an error.
func TestInsertEventIdempotent(t *testing.T) {
	es, db := openTestStore(t)
	ev := Event{ID: "x", TS: 1, Source: "s", Kind: "k", Payload: map[string]any{"a": 1}, Render: "r"}
	if err := es.InsertEvent(ev); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := es.InsertEvent(ev); err != nil {
		t.Fatalf("dupe: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
}

// TestQueryEventsFilters covers from/to bounds and source/kind ANDs.
func TestQueryEventsFilters(t *testing.T) {
	es, _ := openTestStore(t)
	for i, ev := range []Event{
		{ID: "a", TS: 10, Source: "s1", Kind: "k1", Payload: map[string]any{}, Render: "a"},
		{ID: "b", TS: 20, Source: "s1", Kind: "k2", Payload: map[string]any{}, Render: "b"},
		{ID: "c", TS: 30, Source: "s2", Kind: "k1", Payload: map[string]any{}, Render: "c"},
	} {
		if err := es.InsertEvent(ev); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	out, err := es.QueryEvents(EventFilter{From: 15, To: 40, Sources: []string{"s1", "s2"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	// QueryEvents returns ts DESC.
	if out[0].ID != "c" || out[1].ID != "b" {
		t.Fatalf("order: %#v", out)
	}
}

// TestCursorRoundTrip asserts Save/Load is a faithful round-trip
// and an unknown source returns ("", nil).
func TestCursorRoundTrip(t *testing.T) {
	es, _ := openTestStore(t)
	if got, err := es.LoadCursor("missing"); err != nil || got != "" {
		t.Fatalf("missing: %q err=%v", got, err)
	}
	if err := es.SaveCursor("s1", `{"inode":7,"offset":42}`); err != nil {
		t.Fatal(err)
	}
	got, err := es.LoadCursor("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"inode":7,"offset":42}` {
		t.Fatalf("got %q", got)
	}
}
