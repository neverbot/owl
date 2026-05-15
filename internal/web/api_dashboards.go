package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// dashboardSummary is the compact representation returned by GET /api/dashboards.
type dashboardSummary struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	PanelCount int    `json:"panel_count"`
}

// dashboardsResponse wraps the list for GET /api/dashboards.
type dashboardsResponse struct {
	Dashboards []dashboardSummary `json:"dashboards"`
}

// apiDashboards handles GET /api/dashboards.
func (s *Server) apiDashboards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.opt.Loader == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dashboardsResponse{Dashboards: []dashboardSummary{}})
		return
	}

	list := s.opt.Loader.List()
	summaries := make([]dashboardSummary, 0, len(list))
	for _, d := range list {
		summaries = append(summaries, dashboardSummary{
			ID:         d.ID,
			Title:      d.Title,
			PanelCount: len(d.Panels),
		})
	}
	resp := dashboardsResponse{Dashboards: summaries}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// apiDashboardByID handles GET /api/dashboards/{id}.
func (s *Server) apiDashboardByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path is /api/dashboards/{id}; strip the prefix.
	id := strings.TrimPrefix(r.URL.Path, "/api/dashboards/")
	id = strings.Trim(id, "/")
	if id == "" {
		// Trailing slash with no id — redirect to the index.
		http.Redirect(w, r, "/api/dashboards", http.StatusMovedPermanently)
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(d)
}
