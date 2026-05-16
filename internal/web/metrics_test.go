package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neverbot/owl/internal/alert"
)

type fakeAlerterStats struct{ s alert.Stats }

func (f fakeAlerterStats) Snapshot() alert.Stats { return f.s }

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

func TestMetricsEndpointExposesAlerterStats(t *testing.T) {
	s := NewServer(Options{
		Alerter: fakeAlerterStats{s: alert.Stats{
			EvaluationsTotal:     17,
			WebhookSendsTotal:    5,
			WebhookFailuresTotal: 1,
			Firing:               2,
		}},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	s.ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, want := range []string{
		"# TYPE owl_alerts_evaluations_total counter\nowl_alerts_evaluations_total 17",
		"# TYPE owl_alerts_webhook_sends_total counter\nowl_alerts_webhook_sends_total 5",
		"# TYPE owl_alerts_webhook_failures_total counter\nowl_alerts_webhook_failures_total 1",
		"# TYPE owl_alerts_firing gauge\nowl_alerts_firing 2",
	} {
		if !bytes.Contains([]byte(body), []byte(want)) {
			t.Errorf("missing line %q\nbody:\n%s", want, body)
		}
	}
}

func TestMetricsEndpointOmitsAlerterWhenNil(t *testing.T) {
	s := NewServer(Options{}) // no Alerter
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	s.ServeHTTP(rec, req)
	if bytes.Contains(rec.Body.Bytes(), []byte("owl_alerts_")) {
		t.Error("owl_alerts_* lines leaked when Alerter is nil")
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
