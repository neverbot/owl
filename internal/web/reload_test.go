package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReloadReturns503WhenHookMissing(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/-/reload", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestReloadCallsHookAndReturns200(t *testing.T) {
	called := 0
	s := NewServer(Options{OnReload: func() error { called++; return nil }})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/-/reload", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if called != 1 {
		t.Errorf("hook called %d times, want 1", called)
	}
}

func TestReloadReturns500OnHookFailure(t *testing.T) {
	s := NewServer(Options{OnReload: func() error { return errors.New("boom") }})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/-/reload", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestReloadAcceptsGETAndPOST(t *testing.T) {
	s := NewServer(Options{OnReload: func() error { return nil }})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/-/reload", nil)
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s /-/reload: status = %d", method, rec.Code)
		}
	}
}

func TestReloadRejectsPUT(t *testing.T) {
	s := NewServer(Options{OnReload: func() error { return nil }})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/-/reload", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
