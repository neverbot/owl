package web

import (
	"net/http"
	"strings"

	"github.com/neverbot/owl/internal/dashboards"
)

// dashboardTemplateData is passed to templates/dashboard.html.
type dashboardTemplateData struct {
	Title     string
	RefreshMS int64
	Panels    []panelTemplateData
}

// panelTemplateData is one row in the panel grid for the template.
type panelTemplateData struct {
	ID       string
	Title    string
	Expr     string
	Legend   string // Grafana-style template, e.g. "{{name}}"
	Unit     string
	Status   string
	Reason   string
	ColStart int
	ColSpan  int
	RowStart int
	RowSpan  int
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
		http.NotFound(w, r)
		return
	}

	if s.opt.Loader == nil {
		http.NotFound(w, r)
		return
	}

	d, ok := s.opt.Loader.Get(id)
	if !ok {
		http.NotFound(w, r)
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
		expr := ""
		legend := ""
		if len(p.Targets) > 0 {
			expr = p.Targets[0].Expr
			legend = p.Targets[0].LegendFormat
		}
		panels = append(panels, panelTemplateData{
			ID:       p.ID,
			Title:    p.Title,
			Expr:     expr,
			Legend:   legend,
			Unit:     p.Unit,
			Status:   p.Support.Status,
			Reason:   p.Support.Reason,
			ColStart: p.GridPos.X + 1,
			ColSpan:  p.GridPos.W,
			RowStart: p.GridPos.Y + 1,
			RowSpan:  p.GridPos.H,
		})
	}

	return dashboardTemplateData{
		Title:     d.Title,
		RefreshMS: refreshMS,
		Panels:    panels,
	}
}
