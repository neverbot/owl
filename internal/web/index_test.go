package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndexListsDashboards(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.Bytes()

	if !bytes.Contains(body, []byte(`href="/d/alpha"`)) {
		t.Error("index missing link to /d/alpha")
	}
	if !bytes.Contains(body, []byte(`href="/d/beta"`)) {
		t.Error("index missing link to /d/beta")
	}
	if !bytes.Contains(body, []byte("Alpha")) {
		t.Error("index missing dashboard title Alpha")
	}
}

func TestIndexEmptyState(t *testing.T) {
	// No loader → empty state message
	s := NewServer(Options{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.Bytes()
	if !bytes.Contains(body, []byte("No dashboards loaded")) {
		t.Error("index missing empty-state message")
	}
}

func TestIndexHTMLTitle(t *testing.T) {
	s := NewServer(Options{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.ServeHTTP(rec, req)

	if !bytes.Contains(rec.Body.Bytes(), []byte("<title>owl</title>")) {
		t.Error("index missing <title>owl</title>")
	}
}
