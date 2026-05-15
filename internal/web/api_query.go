package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
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

// defaultWindowMS is the default look-back window when from is not specified.
const defaultWindowMS = 5 * 60 * 1000 // 5 minutes

func (s *Server) apiQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	expr := r.URL.Query().Get("expr")
	if expr == "" {
		http.Error(w, `missing "expr" query parameter`, http.StatusBadRequest)
		return
	}

	now := time.Now().UnixMilli()
	to := parseInt64(r.URL.Query().Get("to"), now)
	from := parseInt64(r.URL.Query().Get("from"), to-defaultWindowMS)
	step := parseInt64(r.URL.Query().Get("step"), 0) // 0 → engine default (15 s)

	res, err := s.opt.Engine.QueryRange(expr, from, to, step)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := queryResponse{Series: make([]querySeriesJSON, 0, len(res.Series))}
	for _, ser := range res.Series {
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
