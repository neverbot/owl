package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/neverbot/owl/internal/dashboards"
)

// fakeCaps is a no-op Capabilities implementation for tests.
type fakeCaps struct{}

func (fakeCaps) IsSupported(expr string) (bool, string) { return true, "" }

// buildTestLoader creates a Loader backed by a temp directory with two dashboards.
func buildTestLoader(t *testing.T) *dashboards.Loader {
	t.Helper()
	dir := t.TempDir()
	dash1 := []byte(`{"title":"Alpha","refresh":"5s","panels":[
		{"id":1,"type":"timeseries","title":"P1","gridPos":{"x":0,"y":0,"w":12,"h":8},
		 "targets":[{"expr":"metric_a","refId":"A"}]}
	]}`)
	dash2 := []byte(`{"title":"Beta","refresh":"10s","panels":[
		{"id":1,"type":"timeseries","title":"P1","gridPos":{"x":0,"y":0,"w":12,"h":8},
		 "targets":[{"expr":"metric_b","refId":"A"}]},
		{"id":2,"type":"timeseries","title":"P2","gridPos":{"x":12,"y":0,"w":12,"h":8},
		 "targets":[{"expr":"metric_c","refId":"A"}]}
	]}`)
	if err := os.WriteFile(dir+"/alpha.json", dash1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/beta.json", dash2, 0o644); err != nil {
		t.Fatal(err)
	}
	loader := dashboards.NewLoader(dir, fakeCaps{})
	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	return loader
}

func TestAPIDashboardsListsAll(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboards", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var got dashboardsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}

	if len(got.Dashboards) != 2 {
		t.Fatalf("len(dashboards) = %d, want 2", len(got.Dashboards))
	}
	// Sorted by id: alpha, beta
	if got.Dashboards[0].ID != "alpha" {
		t.Errorf("first id = %q, want alpha", got.Dashboards[0].ID)
	}
	if got.Dashboards[0].Title != "Alpha" {
		t.Errorf("first title = %q, want Alpha", got.Dashboards[0].Title)
	}
	if got.Dashboards[0].PanelCount != 1 {
		t.Errorf("first panel_count = %d, want 1", got.Dashboards[0].PanelCount)
	}
	if got.Dashboards[1].PanelCount != 2 {
		t.Errorf("second panel_count = %d, want 2", got.Dashboards[1].PanelCount)
	}
}

func TestAPIDashboardsNoLoader(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboards", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got dashboardsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Dashboards) != 0 {
		t.Errorf("want empty list, got %d entries", len(got.Dashboards))
	}
}

func TestAPIDashboardsByID(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboards/alpha", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Decode into a generic map to avoid importing dashboards types
	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if got["ID"] != "alpha" {
		t.Errorf("id = %v, want alpha", got["ID"])
	}
	if got["Title"] != "Alpha" {
		t.Errorf("title = %v, want Alpha", got["Title"])
	}
	panels, _ := got["Panels"].([]interface{})
	if len(panels) != 1 {
		t.Errorf("len(panels) = %d, want 1", len(panels))
	}
}

func TestAPIDashboardsByIDNotFound(t *testing.T) {
	loader := buildTestLoader(t)
	s := NewServer(Options{Loader: loader})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboards/nonexistent", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestAPIDashboardsRejectsNonGET(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/dashboards", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
