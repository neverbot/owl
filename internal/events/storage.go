package events

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Store is the events-package view over the shared SQLite database.
// It owns no connection lifecycle — the constructor takes a *sql.DB
// managed by internal/storage.
type Store struct{ db *sql.DB }

// NewStore returns a Store backed by db. db must already have the
// events schema applied (storage.Migrate handles this).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// EventFilter is the AND-of-criteria filter used by QueryEvents and
// the /api/events endpoint. Empty slices mean "no constraint on
// this dimension".
type EventFilter struct {
	From    int64    // unix ms inclusive
	To      int64    // unix ms inclusive; 0 ⇒ no upper bound
	Sources []string // OR within the slice, AND with the rest
	Kinds   []string // OR within the slice, AND with the rest
	Limit   int      // 0 ⇒ no limit
}

// InsertEvent writes ev with INSERT OR IGNORE so duplicate ids are
// a no-op. Payload is serialised as compact JSON.
func (s *Store) InsertEvent(ev Event) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO events (id, ts, source, kind, payload, render)
         VALUES (?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.TS, ev.Source, ev.Kind, string(payload), ev.Render,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// QueryEvents returns matching events ordered by ts descending.
func (s *Store) QueryEvents(f EventFilter) ([]Event, error) {
	var where []string
	var args []any
	where = append(where, "ts >= ?")
	args = append(args, f.From)
	if f.To > 0 {
		where = append(where, "ts <= ?")
		args = append(args, f.To)
	}
	if len(f.Sources) > 0 {
		where = append(where, "source IN ("+placeholders(len(f.Sources))+")")
		for _, src := range f.Sources {
			args = append(args, src)
		}
	}
	if len(f.Kinds) > 0 {
		where = append(where, "kind IN ("+placeholders(len(f.Kinds))+")")
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	q := `SELECT id, ts, source, kind, payload, render FROM events WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY ts DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var ev Event
		var payload string
		if err := rows.Scan(&ev.ID, &ev.TS, &ev.Source, &ev.Kind, &payload, &ev.Render); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		_ = json.Unmarshal([]byte(payload), &ev.Payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// LoadCursor returns the opaque cursor previously saved for source,
// or "" when the source has never reported one.
func (s *Store) LoadCursor(source string) (string, error) {
	var c string
	err := s.db.QueryRow(`SELECT cursor FROM event_source_state WHERE source = ?`, source).Scan(&c)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load cursor: %w", err)
	}
	return c, nil
}

// SaveCursor upserts the opaque cursor for source with updated_at = now.
func (s *Store) SaveCursor(source, cursor string) error {
	_, err := s.db.Exec(
		`INSERT INTO event_source_state (source, cursor, updated_at) VALUES (?, ?, ?)
         ON CONFLICT(source) DO UPDATE SET cursor = excluded.cursor, updated_at = excluded.updated_at`,
		source, cursor, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("save cursor: %w", err)
	}
	return nil
}

// placeholders returns "?, ?, ..." with n entries; used to build
// dynamic IN clauses.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',', ' ')
		}
		out = append(out, '?')
	}
	return string(out)
}
