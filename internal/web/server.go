// Package web is the HTTP server exposing Owl's API and embedded UI.
package web

import (
	"net/http"

	"github.com/neverbot/owl/internal/storage"
)

// Options configures the HTTP server. All fields are optional in tests.
type Options struct {
	Store *storage.Store
}

// Server is an http.Handler routing all of Owl's HTTP traffic.
type Server struct {
	mux *http.ServeMux
	opt Options
}

// NewServer constructs the HTTP handler with all routes registered.
func NewServer(opt Options) *Server {
	s := &Server{
		mux: http.NewServeMux(),
		opt: opt,
	}
	s.registerRoutes()
	return s
}

// ServeHTTP dispatches the request to the internal mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/-/healthy", s.healthy)
	s.mux.HandleFunc("/api/query", s.apiQuery)
}
