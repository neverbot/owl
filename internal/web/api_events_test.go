package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neverbot/owl/internal/events"
)

// fakeEventsQuerier implements EventsQuerier with a canned result.
type fakeEventsQuerier struct {
	recv events.EventFilter
	out  []events.Event
}

// QueryEvents records the filter and returns the canned output.
func (f *fakeEventsQuerier) QueryEvents(filter events.EventFilter) ([]events.Event, error) {
	f.recv = filter
	return f.out, nil
}

// TestAPIEventsHappyPath asserts the handler returns the events
// wrapped in {events:[...]}.
func TestAPIEventsHappyPath(t *testing.T) {
	q := &fakeEventsQuerier{out: []events.Event{{ID: "x", TS: 1, Source: "s", Kind: "k", Payload: map[string]any{}, Render: "r"}}}
	s := NewServer(Options{Events: q})
	r := httptest.NewRequest(http.MethodGet, "/api/events?from=0&to=100&source=s&kind=k&limit=5", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 || body.Events[0]["id"] != "x" {
		t.Fatalf("body=%#v", body)
	}
	if q.recv.From != 0 || q.recv.To != 100 || q.recv.Limit != 5 {
		t.Fatalf("filter=%#v", q.recv)
	}
	if len(q.recv.Sources) != 1 || q.recv.Sources[0] != "s" {
		t.Fatalf("sources=%v", q.recv.Sources)
	}
}

// TestAPIEventsMethodNotAllowed asserts that non-GET requests return 405.
func TestAPIEventsMethodNotAllowed(t *testing.T) {
	q := &fakeEventsQuerier{}
	s := NewServer(Options{Events: q})
	r := httptest.NewRequest(http.MethodPost, "/api/events", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// TestAPIEventsDisabled asserts that a nil Events option returns 503.
func TestAPIEventsDisabled(t *testing.T) {
	s := NewServer(Options{}) // Events is nil
	r := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
