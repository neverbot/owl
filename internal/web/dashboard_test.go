package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neverbot/owl/internal/dashboards"
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
		[]byte(`class="page-title">Alpha</h1>`),
		[]byte(`class="panel`),
		[]byte(`data-expr="metric_a"`),
		[]byte(`data-status="supported"`),
		[]byte(`data-unit=`),
		[]byte(`data-panel-id=`),
		[]byte(`data-refresh=`),
		[]byte(`class="panel__chart"`),
		[]byte(`class="panel__value`),
		[]byte(`data-theme-toggle`),
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

func TestBuildDashboardData_StatPanelFlags(t *testing.T) {
	dec := 2
	d := &dashboards.Dashboard{
		ID:    "x",
		Title: "X",
		Panels: []dashboards.Panel{
			{
				ID:        "1",
				Type:      "stat",
				Title:     "Scans",
				Unit:      "none",
				Calc:      "max",
				Decimals:  &dec,
				GraphMode: "area",
				Targets:   []dashboards.Target{{Expr: "up"}},
				Support:   dashboards.PanelSupport{Status: "supported"},
			},
		},
	}
	got := buildDashboardData(d)
	if len(got.Panels) != 1 {
		t.Fatalf("got %d panels, want 1", len(got.Panels))
	}
	p := got.Panels[0]
	if !p.IsStat {
		t.Error("IsStat = false, want true")
	}
	if p.Calc != "max" {
		t.Errorf("Calc = %q, want max", p.Calc)
	}
	if p.Decimals != "2" {
		t.Errorf("Decimals = %q, want \"2\"", p.Decimals)
	}
	if p.GraphMode != "area" {
		t.Errorf("GraphMode = %q, want area", p.GraphMode)
	}
}

func TestBuildDashboardData_GaugePanelIsStat(t *testing.T) {
	d := &dashboards.Dashboard{
		Panels: []dashboards.Panel{
			{ID: "1", Type: "gauge", Calc: "lastNotNull", Support: dashboards.PanelSupport{Status: "supported"}},
		},
	}
	got := buildDashboardData(d)
	if !got.Panels[0].IsStat {
		t.Error("gauge should set IsStat = true")
	}
}

func TestBuildDashboardData_TimeseriesIsNotStat(t *testing.T) {
	d := &dashboards.Dashboard{
		Panels: []dashboards.Panel{
			{ID: "1", Type: "timeseries", Support: dashboards.PanelSupport{Status: "supported"}},
		},
	}
	got := buildDashboardData(d)
	if got.Panels[0].IsStat {
		t.Error("timeseries should set IsStat = false")
	}
}
