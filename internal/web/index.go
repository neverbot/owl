package web

import (
	"net/http"

	"github.com/neverbot/owl/internal/dashboards"
)

// indexTemplateData is passed to templates/index.html.
type indexTemplateData struct {
	Items         []indexItem
	DashboardsDir string
}

type indexItem struct {
	ID         string
	Title      string
	PanelCount int
}

// serveIndex server-renders the homepage listing all known dashboards.
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	var list []*dashboards.Dashboard
	var dir string
	if s.opt.Loader != nil {
		list = s.opt.Loader.List()
		dir = s.opt.Loader.Dir()
	}

	items := make([]indexItem, 0, len(list))
	for _, d := range list {
		items = append(items, indexItem{
			ID:         d.ID,
			Title:      d.Title,
			PanelCount: len(d.Panels),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", indexTemplateData{
		Items:         items,
		DashboardsDir: dir,
	}); err != nil {
		_ = err
	}
}
