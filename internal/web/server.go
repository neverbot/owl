// Package web is the HTTP server exposing Owl's API and embedded UI.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/neverbot/owl/internal/dashboards"
	"github.com/neverbot/owl/internal/query"
	"github.com/neverbot/owl/internal/scrape"
	"github.com/neverbot/owl/internal/storage"
)

//go:embed static/*
var staticFS embed.FS

//go:embed templates/*
var templateFS embed.FS

// Options configures the HTTP server.
type Options struct {
	Store  *storage.Store
	Engine *query.Engine
	Loader *dashboards.Loader
	// Scrape exposes per-target health for /api/targets and /targets.
	// nil disables those endpoints.
	Scrape ScrapeHealth
	// OnReload is invoked when a client POSTs to /-/reload (or the
	// process receives SIGHUP; that wiring lives in cmd/owl). The
	// hook is responsible for re-reading the config file and the
	// dashboards directory and swapping them in atomically. If
	// OnReload is nil, /-/reload returns 503 with a clear message.
	OnReload func() error
}

// ScrapeHealth is the slice of scrape.Manager the web layer needs.
// Defined as an interface so unit tests can plug a fake.
type ScrapeHealth interface {
	HealthSnapshot() []scrape.TargetHealth
}

// Server is an http.Handler routing all of Owl's HTTP traffic.
type Server struct {
	mux  *http.ServeMux
	opt  Options
	tmpl *template.Template
}

// NewServer constructs the HTTP handler with all routes registered.
func NewServer(opt Options) *Server {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.html"))
	s := &Server{
		mux:  http.NewServeMux(),
		opt:  opt,
		tmpl: tmpl,
	}
	s.registerRoutes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/-/healthy", s.healthy)
	s.mux.HandleFunc("/-/reload", s.reload)
	s.mux.HandleFunc("/metrics", s.metrics)
	s.mux.HandleFunc("/api/query", s.apiQuery)
	s.mux.HandleFunc("/api/dashboards/", s.apiDashboardByID)
	s.mux.HandleFunc("/api/dashboards", s.apiDashboards)
	s.mux.HandleFunc("/api/targets", s.apiTargets)
	s.mux.HandleFunc("/targets", s.targetsView)
	s.mux.HandleFunc("/d/", s.dashboardView)
	s.mux.HandleFunc("/", s.indexOrStatic)
}

func (s *Server) indexOrStatic(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		s.serveIndex(w, r)
	case strings.HasPrefix(r.URL.Path, "/static/"):
		s.serveStatic(w, r, "static/"+r.URL.Path[len("/static/"):])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(staticFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css")
	}
	_, _ = w.Write(data)
}
