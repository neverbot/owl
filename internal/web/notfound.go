package web

import (
	"net/http"

	"github.com/neverbot/owl/internal/dashboards"
)

// notFoundData is what `templates/notfound.html` consumes.
type notFoundData struct {
	Path  string
	Items []indexItem
}

// serveNotFound renders the human-readable 404 page. The requested
// path is shown as the figure of the page; the loaded dashboards are
// surfaced underneath so the operator can recover without thinking
// about how they got here. API clients hitting JSON endpoints keep
// receiving plain-text 404s — this handler is for the HTML surface.
func (s *Server) serveNotFound(w http.ResponseWriter, r *http.Request) {
	var list []*dashboards.Dashboard
	if s.opt.Loader != nil {
		list = s.opt.Loader.List()
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
	w.WriteHeader(http.StatusNotFound)
	_ = s.tmpl.ExecuteTemplate(w, "notfound.html", notFoundData{
		Path:  r.URL.Path,
		Items: items,
	})
}
