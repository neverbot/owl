// Package web is the HTTP server exposing Owl's API and embedded UI.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/neverbot/owl/internal/alert"
	"github.com/neverbot/owl/internal/dashboards"
	"github.com/neverbot/owl/internal/design"
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
	// Alerter exposes counters for the /metrics endpoint. nil omits
	// the owl_alerts_* gauges from the exposition.
	Alerter AlerterStats
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

// AlerterStats is the slice of alert.Manager the /metrics handler
// needs. Defined as an interface so tests can plug a fake.
type AlerterStats interface {
	Snapshot() alert.Stats
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
	s.mux.HandleFunc("/favicon.svg", s.serveFaviconSVG)
	s.mux.HandleFunc("/favicon-16.png", s.serveFavicon16)
	s.mux.HandleFunc("/favicon-32.png", s.serveFavicon32)
	s.mux.HandleFunc("/apple-touch-icon.png", s.serveAppleTouchIcon)
	s.mux.HandleFunc("/", s.indexOrStatic)
}

// serveFaviconSVG serves the modern, theme-adaptive SVG favicon.
// Modern browsers pick this first when offered both SVG and PNG via
// <link rel="icon"> tags.
func (s *Server) serveFaviconSVG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write(design.FaviconSVG())
}

// serveFavicon16 serves the 16×16 PNG fallback for the favicon.
func (s *Server) serveFavicon16(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(design.Favicon16())
}

// serveFavicon32 serves the 32×32 PNG fallback for the favicon.
func (s *Server) serveFavicon32(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(design.Favicon32())
}

// serveAppleTouchIcon serves the 180×180 PNG used as the iOS home-screen
// icon when the runtime UI is bookmarked there.
func (s *Server) serveAppleTouchIcon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(design.Favicon180())
}

func (s *Server) indexOrStatic(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		s.serveIndex(w, r)
	case strings.HasPrefix(r.URL.Path, "/static/"):
		s.serveStatic(w, r, "static/"+r.URL.Path[len("/static/"):])
	default:
		s.serveNotFound(w, r)
	}
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, name string) {
	// The shared CSS tokens and chart JS live in internal/design and
	// are served at the same runtime URLs that the templates already
	// reference. Anything else falls back to the local staticFS.
	switch name {
	case "static/owl.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(design.TokensCSS())
		return
	case "static/app.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(design.ChartJS())
		return
	}
	data, err := fs.ReadFile(staticFS, name)
	if err != nil {
		s.serveNotFound(w, r)
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
