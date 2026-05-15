package web

import (
	"io"
	"net/http"
)

// reload handles POST /-/reload. The actual work is in the OnReload
// hook injected via Options. We accept GET too because it's friendlier
// from a browser, but a real CI/automation will use POST.
func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.opt.OnReload == nil {
		http.Error(w, "reload not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.opt.OnReload(); err != nil {
		http.Error(w, "reload failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "reloaded\n")
}
