package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsEndpoint(t *testing.T) {
	s := NewServer(Options{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.Bytes()
	if !bytes.Contains(body, []byte("# HELP owl_goroutines")) {
		t.Error("missing owl_goroutines HELP")
	}
	if !bytes.Contains(body, []byte("# TYPE owl_goroutines gauge")) {
		t.Error("missing owl_goroutines TYPE")
	}
	if !bytes.Contains(body, []byte("owl_heap_objects_bytes")) {
		t.Error("missing owl_heap_objects_bytes")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestMetricsRejectsNonGET(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
