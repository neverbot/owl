package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/neverbot/owl/internal/scrape"
)

// apiTargets returns the latest health entry for every active scrape
// target as JSON. Disabled (503) when no ScrapeHealth source is wired.
func (s *Server) apiTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.opt.Scrape == nil {
		http.Error(w, "scrape manager not wired", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"targets": s.opt.Scrape.HealthSnapshot(),
	})
}

// targetsView renders the /targets page: server-rendered table of
// every active scrape target with status, last scrape time and the
// last error if any.
func (s *Server) targetsView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var rows []targetRow
	if s.opt.Scrape != nil {
		for _, h := range s.opt.Scrape.HealthSnapshot() {
			rows = append(rows, toTargetRow(h))
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "targets.html", targetsTemplateData{Rows: rows}); err != nil {
		_ = err
	}
}

type targetsTemplateData struct {
	Rows []targetRow
}

type targetRow struct {
	Name       string
	URL        string
	Job        string
	Instance   string
	Status     string // "ok", "down", "pending"
	LastScrape string // human-readable relative time
	Duration   string
	Samples    int
	Error      string
}

func toTargetRow(h scrape.TargetHealth) targetRow {
	row := targetRow{
		Name:     h.Name,
		URL:      h.URL,
		Job:      h.Labels["job"],
		Instance: h.Labels["instance"],
		Samples:  h.LastSamples,
		Error:    h.LastError,
	}
	switch {
	case h.LastScrape.IsZero():
		row.Status = "pending"
		row.LastScrape = "—"
	case h.LastError != "":
		row.Status = "down"
		row.LastScrape = relativeTime(h.LastScrape)
	default:
		row.Status = "ok"
		row.LastScrape = relativeTime(h.LastScrape)
	}
	if h.Duration > 0 {
		row.Duration = h.Duration.Round(time.Millisecond).String()
	}
	return row
}

func relativeTime(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < time.Second {
		return "now"
	}
	if d < time.Minute {
		return d.String() + " ago"
	}
	if d < time.Hour {
		return d.Round(time.Second).String() + " ago"
	}
	return d.Round(time.Minute).String() + " ago"
}
