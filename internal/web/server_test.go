package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthyReturns200OK(t *testing.T) {
	s := NewServer(Options{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/-/healthy", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "ok\n" {
		t.Errorf("body = %q, want %q", string(body), "ok\n")
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	s := NewServer(Options{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
