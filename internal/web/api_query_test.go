package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/neverbot/owl/internal/query"
	"github.com/neverbot/owl/internal/storage"
)

func newStore(t *testing.T) *storage.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "owl.db")
	s, err := storage.Open(path)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newEngine(t *testing.T, st *storage.Store) *query.Engine {
	t.Helper()
	return query.NewEngine(st)
}

func TestAPIQueryReturnsSeriesJSON(t *testing.T) {
	st := newStore(t)
	now := time.Now().UnixMilli()
	_ = st.Append([]storage.Sample{
		{Metric: "owl_runtime_goroutines", Labels: map[string]string{"job": "owl"}, TS: now - 1000, Value: 7},
		{Metric: "owl_runtime_goroutines", Labels: map[string]string{"job": "owl"}, TS: now, Value: 9},
	})

	eng := newEngine(t, st)
	s := NewServer(Options{Store: st, Engine: eng})

	rec := httptest.NewRecorder()
	url := "/api/query?expr=owl_runtime_goroutines&from=0&to=" + strconv.FormatInt(now+1, 10)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Series []struct {
			Metric string
			Labels map[string]string
			Points [][]float64
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(got.Series) != 1 {
		t.Fatalf("len(series) = %d, want 1", len(got.Series))
	}
	if len(got.Series[0].Points) != 2 {
		t.Errorf("len(points) = %d, want 2", len(got.Series[0].Points))
	}
	if got.Series[0].Points[1][1] != 9 {
		t.Errorf("last value = %v, want 9", got.Series[0].Points[1][1])
	}
}

func TestAPIQueryRejectsMissingExpr(t *testing.T) {
	eng := newEngine(t, newStore(t))
	s := NewServer(Options{Engine: eng})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/query", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAPIQueryRejectsNonGET(t *testing.T) {
	eng := newEngine(t, newStore(t))
	s := NewServer(Options{Engine: eng})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/query?expr=x", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestAPIQueryRejectsBadExpr(t *testing.T) {
	eng := newEngine(t, newStore(t))
	s := NewServer(Options{Engine: eng})

	rec := httptest.NewRecorder()
	// empty string is unsupported
	req := httptest.NewRequest(http.MethodGet, "/api/query?expr=metric1+%2B+metric2", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAPIQueryDefaultTimeRange(t *testing.T) {
	st := newStore(t)
	now := time.Now().UnixMilli()
	// Write a point 1 second ago — should be within 5-min default window
	_ = st.Append([]storage.Sample{
		{Metric: "test_metric", Labels: map[string]string{}, TS: now - 1000, Value: 42},
	})

	eng := newEngine(t, st)
	s := NewServer(Options{Store: st, Engine: eng})

	// No from/to → defaults to [now-5min, now]
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/query?expr=test_metric", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Series []struct{ Points [][]float64 }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Series) == 0 || len(got.Series[0].Points) == 0 {
		t.Error("expected at least one point in default time range")
	}
}
