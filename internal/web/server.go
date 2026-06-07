// Package web is the HTTP server exposing Owl's API and embedded UI.
package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

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
	// Collectors exposes per-collector health (host, docker container
	// metrics, …) on the same /targets page so operators can see the
	// in-process metric sources that bypass the scrape pipeline. nil
	// means no internal collectors are enabled.
	Collectors CollectorsHealth
	// Containers exposes the docker collector's per-container view,
	// rendered as a third section on /targets. nil disables the
	// section.
	Containers ContainersHealth
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

// CollectorsHealth exposes the latest health of every enabled internal
// collector (host, docker container metrics). Implementations live in
// the wiring layer (cmd/owl) and adapt each collector's native Health
// type to the uniform CollectorHealth shape rendered on /targets.
type CollectorsHealth interface {
	CollectorsSnapshot() []CollectorHealth
}

// ContainersHealth exposes the per-container view captured by the
// docker collector on its most recent tick. nil means the docker
// integration is disabled or no containers were observed yet.
type ContainersHealth interface {
	ContainersSnapshot() []ContainerInfo
}

// ContainerInfo is one row in the "containers" section of /targets.
// MemoryWorkingSetBytes mirrors cAdvisor's `container_memory_usage_bytes`
// (usage minus inactive file cache), so the value here matches what
// the Containers dashboard plots.
type ContainerInfo struct {
	Name                  string    `json:"name"`
	Image                 string    `json:"image"`
	ComposeService        string    `json:"compose_service,omitempty"`
	ComposeProject        string    `json:"compose_project,omitempty"`
	MemoryWorkingSetBytes uint64    `json:"memory_working_set_bytes"`
	LastSeen              time.Time `json:"last_seen,omitempty"`
}

// CollectorHealth is the uniform per-collector health snapshot the
// web layer renders. Kind is a short token ("host", "docker_metrics")
// so future kinds can be added without changing the schema. Extra is
// a free-form, single-line note shown next to the row (for example
// "3 containers seen").
type CollectorHealth struct {
	Name           string        `json:"name"`
	Kind           string        `json:"kind"`
	Interval       time.Duration `json:"interval"`
	LastCollection time.Time     `json:"last_collection,omitempty"`
	Duration       time.Duration `json:"duration,omitempty"`
	LastError      string        `json:"last_error,omitempty"`
	LastSamples    int           `json:"last_samples"`
	Extra          string        `json:"extra,omitempty"`
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

	// nowFn is overridable so tests can pin time around the
	// /api/range cache without sleeping.
	nowFn func() time.Time

	rangeMu       sync.Mutex
	rangeCachedAt time.Time
	rangeMinTS    *int64
	rangeMaxTS    *int64
}

// NewServer constructs the HTTP handler with all routes registered.
func NewServer(opt Options) *Server {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.html"))
	s := &Server{
		mux:  http.NewServeMux(),
		opt:  opt,
		tmpl: tmpl,
	}
	s.nowFn = time.Now
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
	s.mux.HandleFunc("/api/range", s.apiRange)
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

// rangeTTL is how long /api/range caches the MIN/MAX query result.
// 30 s is well under the user-perceptible threshold for "is my
// retention boundary current?" and well over the burst window a
// page that polls every panel could ever produce.
const rangeTTL = 30 * time.Second

// apiRange returns the smallest and largest sample timestamp in the
// store, in milliseconds since epoch. Either field is null when the
// store is empty. The result is cached for rangeTTL to absorb bursts
// of dashboard opens and calendar popovers.
func (s *Server) apiRange(w http.ResponseWriter, r *http.Request) {
	if s.opt.Store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	s.rangeMu.Lock()
	defer s.rangeMu.Unlock()

	if s.nowFn().Sub(s.rangeCachedAt) > rangeTTL {
		minTS, maxTS, ok, err := s.opt.Store.Range()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if ok {
			s.rangeMinTS = &minTS
			s.rangeMaxTS = &maxTS
		} else {
			s.rangeMinTS = nil
			s.rangeMaxTS = nil
		}
		s.rangeCachedAt = s.nowFn()
	}

	body := struct {
		MinTS *int64 `json:"min_ts"`
		MaxTS *int64 `json:"max_ts"`
	}{MinTS: s.rangeMinTS, MaxTS: s.rangeMaxTS}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
