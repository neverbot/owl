package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardViewRendersHTML(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/alpha", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.Bytes()

	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}

	checks := [][]byte{
		[]byte("<h1>Alpha</h1>"),
		[]byte(`class="panel"`),
		[]byte(`data-expr="metric_a"`),
		[]byte(`data-status="supported"`),
		[]byte(`data-unit=`),
		[]byte(`data-panel-id=`),
		[]byte(`data-refresh=`),
		[]byte(`<svg>`),
		[]byte(`<span class="last">—</span>`),
	}
	for _, want := range checks {
		if !bytes.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestDashboardViewNotFound(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/nope", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestDashboardViewGridPos(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/beta", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.Bytes()

	// beta has two panels:
	// P1: gridPos x=0 y=0 w=12 h=8 → grid-column:1/span 12; grid-row:1/span 8
	// P2: gridPos x=12 y=0 w=12 h=8 → grid-column:13/span 12; grid-row:1/span 8
	if !bytes.Contains(body, []byte("grid-column:1/span 12")) {
		t.Error("body missing grid-column:1/span 12")
	}
	if !bytes.Contains(body, []byte("grid-column:13/span 12")) {
		t.Error("body missing grid-column:13/span 12")
	}
}

func TestDashboardViewNoLoader(t *testing.T) {
	s := NewServer(Options{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/anything", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestDashboardViewRefreshAttribute(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/alpha", nil)
	s.ServeHTTP(rec, req)

	body := rec.Body.Bytes()
	// alpha has refresh "5s" → 5000 ms
	if !bytes.Contains(body, []byte(`data-refresh="5000"`)) {
		t.Errorf("body missing data-refresh=5000, got:\n%s", body)
	}
}
