package web

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// queryResponse is what /api/query returns. Points are encoded as
// [timestamp_ms, value] pairs for a compact wire format.
type queryResponse struct {
	Series []querySeriesJSON `json:"series"`
}

type querySeriesJSON struct {
	Metric string            `json:"metric"`
	Labels map[string]string `json:"labels"`
	Points [][2]float64      `json:"points"`
}

func (s *Server) apiQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metric := r.URL.Query().Get("metric")
	if metric == "" {
		http.Error(w, `missing "metric" query parameter`, http.StatusBadRequest)
		return
	}

	from := parseInt64(r.URL.Query().Get("from"), 0)
	to := parseInt64(r.URL.Query().Get("to"), 1<<62)

	series, err := s.opt.Store.Query(metric, from, to)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := queryResponse{Series: make([]querySeriesJSON, 0, len(series))}
	for _, ser := range series {
		points := make([][2]float64, 0, len(ser.Points))
		for _, p := range ser.Points {
			points = append(points, [2]float64{float64(p.TS), p.Value})
		}
		resp.Series = append(resp.Series, querySeriesJSON{
			Metric: ser.Metric,
			Labels: ser.Labels,
			Points: points,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseInt64(s string, def int64) int64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}
