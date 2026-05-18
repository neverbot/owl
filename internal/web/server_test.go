package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/storage"
)

func newTestServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "owl.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close(); _ = os.RemoveAll(dir) })
	srv := NewServer(Options{Store: store})
	return srv, store
}

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

func TestRootServesIndex(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("<title>owl</title>")) {
		t.Error("index.html title missing from response")
	}
}

func TestStaticAssetIsServed(t *testing.T) {
	s := NewServer(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/javascript; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestAPIRangeEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/range", nil)
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200; body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		MinTS *int64 `json:"min_ts"`
		MaxTS *int64 `json:"max_ts"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if body.MinTS != nil || body.MaxTS != nil {
		t.Errorf("expected nulls on empty store, got %v / %v", body.MinTS, body.MaxTS)
	}
}

func TestAPIRangeWithSamples(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.Append([]storage.Sample{
		{Metric: "m", Labels: map[string]string{}, TS: 1_000_000_000_000, Value: 1},
		{Metric: "m", Labels: map[string]string{}, TS: 2_000_000_000_000, Value: 2},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/range", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	var body struct {
		MinTS *int64 `json:"min_ts"`
		MaxTS *int64 `json:"max_ts"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.MinTS == nil || *body.MinTS != 1_000_000_000_000 {
		t.Errorf("min_ts: got %v want 1_000_000_000_000", body.MinTS)
	}
	if body.MaxTS == nil || *body.MaxTS != 2_000_000_000_000 {
		t.Errorf("max_ts: got %v want 2_000_000_000_000", body.MaxTS)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type: got %q want application/json", ct)
	}
}

func TestAPIRangeCached(t *testing.T) {
	srv, store := newTestServer(t)
	now := time.Unix(1_700_000_000, 0)
	srv.nowFn = func() time.Time { return now }

	if err := store.Append([]storage.Sample{
		{Metric: "m", Labels: map[string]string{}, TS: 1_000, Value: 1},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	rr1 := httptest.NewRecorder()
	srv.ServeHTTP(rr1, httptest.NewRequest(http.MethodGet, "/api/range", nil))
	if rr1.Code != http.StatusOK {
		t.Fatalf("first status: %d", rr1.Code)
	}

	if err := store.Append([]storage.Sample{
		{Metric: "m", Labels: map[string]string{}, TS: 9_999, Value: 9},
	}); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	rr2 := httptest.NewRecorder()
	srv.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/api/range", nil))
	var body struct {
		MaxTS *int64 `json:"max_ts"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.MaxTS == nil || *body.MaxTS != 1_000 {
		t.Errorf("cached max_ts: got %v want 1000 (stale read within TTL)", body.MaxTS)
	}

	srv.nowFn = func() time.Time { return now.Add(31 * time.Second) }
	rr3 := httptest.NewRecorder()
	srv.ServeHTTP(rr3, httptest.NewRequest(http.MethodGet, "/api/range", nil))
	if err := json.Unmarshal(rr3.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.MaxTS == nil || *body.MaxTS != 9_999 {
		t.Errorf("post-TTL max_ts: got %v want 9999", body.MaxTS)
	}
}
