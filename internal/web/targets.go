package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/neverbot/owl/internal/scrape"
)

// apiTargets returns the latest health entry for every active scrape
// target and every enabled internal collector as JSON. Disabled (503)
// when no ScrapeHealth source is wired. The `collectors` key is
// omitted when no CollectorsHealth source is wired.
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
	body := map[string]any{
		"targets": s.opt.Scrape.HealthSnapshot(),
	}
	if s.opt.Collectors != nil {
		body["collectors"] = s.opt.Collectors.CollectorsSnapshot()
	}
	if s.opt.Containers != nil {
		body["containers"] = s.opt.Containers.ContainersSnapshot()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// targetsView renders the /targets page: server-rendered tables of
// every active scrape target and every enabled internal collector,
// each row showing status, last scrape / collection time and the
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
	var collectors []collectorRow
	if s.opt.Collectors != nil {
		for _, h := range s.opt.Collectors.CollectorsSnapshot() {
			collectors = append(collectors, toCollectorRow(h))
		}
	}
	var containers []containerRow
	if s.opt.Containers != nil {
		for _, ci := range s.opt.Containers.ContainersSnapshot() {
			containers = append(containers, toContainerRow(ci))
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "targets.html", targetsTemplateData{Rows: rows, Collectors: collectors, Containers: containers}); err != nil {
		_ = err
	}
}

type targetsTemplateData struct {
	Rows       []targetRow
	Collectors []collectorRow
	Containers []containerRow
}

type containerRow struct {
	Name     string
	Compose  string // "service · project" when present
	Image    string
	Memory   string
	LastSeen string
}

func toContainerRow(ci ContainerInfo) containerRow {
	row := containerRow{
		Name:   ci.Name,
		Image:  ci.Image,
		Memory: humanBytes(ci.MemoryWorkingSetBytes),
	}
	switch {
	case ci.ComposeService != "" && ci.ComposeProject != "":
		row.Compose = ci.ComposeService + " · " + ci.ComposeProject
	case ci.ComposeService != "":
		row.Compose = ci.ComposeService
	case ci.ComposeProject != "":
		row.Compose = ci.ComposeProject
	}
	if ci.LastSeen.IsZero() {
		row.LastSeen = "—"
	} else {
		row.LastSeen = relativeTime(ci.LastSeen)
	}
	return row
}

// humanBytes renders n as a short, binary-prefixed value (KiB, MiB,
// GiB, …) with at most one decimal place. Mirrors the convention used
// across the bundled dashboards so the /targets row reads at the same
// scale as the chart that plots it.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatUint(n, 10) + " B"
	}
	div, exp := uint64(unit), 0
	for r := n / unit; r >= unit; r /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}[exp]
	v := float64(n) / float64(div)
	if v >= 10 {
		return strconv.FormatFloat(v, 'f', 0, 64) + " " + suffix
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + suffix
}

type collectorRow struct {
	Name           string
	Kind           string
	Status         string // "ok", "down", "pending"
	LastCollection string
	Duration       string
	Samples        int
	Interval       string
	Error          string
	Extra          string
}

func toCollectorRow(h CollectorHealth) collectorRow {
	row := collectorRow{
		Name:     h.Name,
		Kind:     h.Kind,
		Samples:  h.LastSamples,
		Error:    h.LastError,
		Extra:    h.Extra,
		Interval: h.Interval.String(),
	}
	switch {
	case h.LastCollection.IsZero():
		row.Status = "pending"
		row.LastCollection = "—"
	case h.LastError != "":
		row.Status = "down"
		row.LastCollection = relativeTime(h.LastCollection)
	default:
		row.Status = "ok"
		row.LastCollection = relativeTime(h.LastCollection)
	}
	if h.Duration > 0 {
		row.Duration = h.Duration.Round(time.Millisecond).String()
	}
	return row
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
	Auth       string
	Error      string
}

func toTargetRow(h scrape.TargetHealth) targetRow {
	row := targetRow{
		Name:     h.Name,
		URL:      h.URL,
		Job:      h.Labels["job"],
		Instance: h.Labels["instance"],
		Samples:  h.LastSamples,
		Auth:     h.Auth,
		Error:    h.LastError,
	}
	if row.Auth == "" {
		row.Auth = "none"
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
