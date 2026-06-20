package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/neverbot/owl/internal/dashboards"
)

// dashboardTemplateData is passed to templates/dashboard.html.
type dashboardTemplateData struct {
	Title     string
	RefreshMS int64
	Panels    []panelTemplateData
}

// panelQuery is one element of the data-queries JSON array; the
// dashboard template ships these to chart.js, which fans out one
// /api/query fetch per entry and merges the returned series.
type panelQuery struct {
	Expr   string `json:"expr"`
	Legend string `json:"legend"`
}

// panelTemplateData is one row in the panel grid for the template.
type panelTemplateData struct {
	ID              string
	Title           string
	Queries         string // JSON-encoded [{expr, legend}, ...] in targets[] order
	Unit            string
	Status          string
	Reason          string
	IsStat          bool   // true for stat or gauge panels — template emits .panel__stat
	Calc            string // reduction operator; empty when IsStat is false
	Decimals        string // decimal places as decimal string, or "" when unset
	GraphMode       string // "area" turns on the sparkline; empty/none disables it
	IsEvents        bool   // true for events panels — template emits .panel__events table
	EventTargetsJSON string // JSON-encoded [{source,kind},...] for events panels
	AnnotationsJSON  string // JSON-encoded [{source,kind},...] overlaid on timeseries panels
	ColStart        int
	ColSpan         int
	RowStart        int
	RowSpan         int
}

// dashboardView handles GET /d/{id}.
func (s *Server) dashboardView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/d/")
	id = strings.Trim(id, "/")
	if id == "" {
		s.serveNotFound(w, r)
		return
	}

	if s.opt.Loader == nil {
		s.serveNotFound(w, r)
		return
	}

	d, ok := s.opt.Loader.Get(id)
	if !ok {
		s.serveNotFound(w, r)
		return
	}

	data := buildDashboardData(d)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		// Header already written; just log.
		_ = err
	}
}

// buildDashboardData converts a *dashboards.Dashboard into template data.
func buildDashboardData(d *dashboards.Dashboard) dashboardTemplateData {
	refreshMS := d.Refresh.Milliseconds()
	if refreshMS == 0 {
		refreshMS = 5000 // default 5 s
	}

	panels := make([]panelTemplateData, 0, len(d.Panels))
	for _, p := range d.Panels {
		queries := make([]panelQuery, 0, len(p.Targets))
		for _, t := range p.Targets {
			queries = append(queries, panelQuery{Expr: t.Expr, Legend: t.LegendFormat})
		}
		// json.Marshal of []panelQuery cannot fail in practice — the
		// element struct is a flat string pair — so we drop the error.
		qjson, _ := json.Marshal(queries)

		decimals := ""
		if p.Decimals != nil {
			decimals = strconv.Itoa(*p.Decimals)
		}
		// json.Marshal of []EventTarget / []Annotation cannot fail in practice —
		// both element structs are flat string pairs — so we drop the errors.
		evTargets, _ := json.Marshal(p.EventTargets)
		anns, _ := json.Marshal(p.Annotations)
		panels = append(panels, panelTemplateData{
			ID:              p.ID,
			Title:           p.Title,
			Queries:         string(qjson),
			Unit:            p.Unit,
			Status:          p.Support.Status,
			Reason:          p.Support.Reason,
			IsStat:          p.Type == "stat" || p.Type == "gauge",
			Calc:            p.Calc,
			Decimals:        decimals,
			GraphMode:       p.GraphMode,
			IsEvents:        p.Type == "events",
			EventTargetsJSON: string(evTargets),
			AnnotationsJSON:  string(anns),
			ColStart:        p.GridPos.X + 1,
			ColSpan:         p.GridPos.W,
			RowStart:        p.GridPos.Y + 1,
			RowSpan:         p.GridPos.H,
		})
	}

	return dashboardTemplateData{
		Title:     d.Title,
		RefreshMS: refreshMS,
		Panels:    panels,
	}
}
