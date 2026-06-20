package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/neverbot/owl/internal/events"
)

// defaultEventsLimit caps a /api/events response when the client
// doesn't ask for fewer. 200 is well above the events panel's page
// size (50) and below the cardinality where the table panel would
// get unwieldy.
const defaultEventsLimit = 200

// apiEvents serves GET /api/events. Query params:
//
//	from, to: unix ms (to=0 → no upper bound; from is required).
//	source, kind: repeatable (OR within each, AND across).
//	limit: capped at defaultEventsLimit when absent or > cap.
func (s *Server) apiEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.opt.Events == nil {
		http.Error(w, "events disabled", http.StatusServiceUnavailable)
		return
	}
	qv := r.URL.Query()
	from, _ := strconv.ParseInt(qv.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(qv.Get("to"), 10, 64)
	limit, _ := strconv.Atoi(qv.Get("limit"))
	if limit <= 0 || limit > defaultEventsLimit {
		limit = defaultEventsLimit
	}
	filter := events.EventFilter{
		From:    from,
		To:      to,
		Sources: qv["source"],
		Kinds:   qv["kind"],
		Limit:   limit,
	}
	out, err := s.opt.Events.QueryEvents(filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type wire struct {
		ID      string         `json:"id"`
		TS      int64          `json:"ts"`
		Source  string         `json:"source"`
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
		Render  string         `json:"render"`
	}
	list := make([]wire, 0, len(out))
	for _, e := range out {
		list = append(list, wire{ID: e.ID, TS: e.TS, Source: e.Source, Kind: e.Kind, Payload: e.Payload, Render: e.Render})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Events []wire `json:"events"`
	}{Events: list})
}
