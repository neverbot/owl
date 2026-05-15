package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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

func TestAPIQueryReturnsSeriesJSON(t *testing.T) {
	st := newStore(t)
	now := time.Now().UnixMilli()
	_ = st.Append([]storage.Sample{
		{Metric: "owl_runtime_goroutines", Labels: map[string]string{"job": "owl"}, TS: now - 1000, Value: 7},
		{Metric: "owl_runtime_goroutines", Labels: map[string]string{"job": "owl"}, TS: now, Value: 9},
	})

	s := NewServer(Options{Store: st})

	rec := httptest.NewRecorder()
	url := "/api/query?metric=owl_runtime_goroutines&from=0&to=" + strconv.FormatInt(now+1, 10)
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

func TestAPIQueryRejectsMissingMetric(t *testing.T) {
	s := NewServer(Options{Store: newStore(t)})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/query", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAPIQueryRejectsNonGET(t *testing.T) {
	s := NewServer(Options{Store: newStore(t)})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/query?metric=x", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
