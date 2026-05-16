package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotFoundUnknownPathRendersHTML(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does/not/exist", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.Bytes()
	for _, want := range [][]byte{
		[]byte("404"),
		[]byte("not found"),
		[]byte("/does/not/exist"), // the requested path is the figure
	} {
		if !bytes.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestNotFoundMissingDashboardIDRendersHTML(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/no-such-dashboard", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.Bytes()
	if !bytes.Contains(body, []byte("/d/no-such-dashboard")) {
		t.Error("404 must echo the missing dashboard path")
	}
	if !bytes.Contains(body, []byte("Dashboards available")) {
		t.Error("404 must list real dashboards when a Loader is wired")
	}
}

func TestNotFoundMissingStaticAssetRendersHTML(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/no-such-file.css", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("/static/no-such-file.css")) {
		t.Error("404 must echo the missing static path")
	}
}

func TestNotFoundOmitsListWhenNoLoader(t *testing.T) {
	s := NewServer(Options{}) // no Loader
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anywhere", nil)
	s.ServeHTTP(rec, req)

	if bytes.Contains(rec.Body.Bytes(), []byte("Dashboards available")) {
		t.Error("dashboard list must be omitted when no loader is configured")
	}
}

func TestNotFoundDoesNotAffectAPI(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboards/missing", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	// API endpoints must NOT return the HTML 404 — they serve
	// programmatic clients and stay as plain text.
	if ct := rec.Header().Get("Content-Type"); ct == "text/html; charset=utf-8" {
		t.Errorf("API 404 must not be HTML, got content-type %q", ct)
	}
}
