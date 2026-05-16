package dashboards

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// grafanaDashboard is the subset of Grafana's dashboard JSON schema that
// Owl cares about. Unknown fields are silently discarded by the JSON decoder.
type grafanaDashboard struct {
	Title   string         `json:"title"`
	Refresh string         `json:"refresh"`
	Time    grafanaTime    `json:"time"`
	Panels  []grafanaPanel `json:"panels"`
}

type grafanaTime struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type grafanaPanel struct {
	// ID may be an integer or a string in Grafana JSON.
	ID          json.RawMessage `json:"id"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	GridPos     grafanaGridPos  `json:"gridPos"`
	FieldConfig grafanaFieldCfg `json:"fieldConfig"`
	Targets     []grafanaTarget `json:"targets"`
}

type grafanaGridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type grafanaFieldCfg struct {
	Defaults grafanaDefaults `json:"defaults"`
}

type grafanaDefaults struct {
	Unit string `json:"unit"`
}

type grafanaTarget struct {
	Expr         string `json:"expr"`
	LegendFormat string `json:"legendFormat"`
	RefID        string `json:"refId"`
}

// ParseDashboard parses a Grafana-format dashboard JSON byte slice into an
// Owl Dashboard. The id parameter is the slug (filename without .json).
// Unknown fields in the JSON are silently ignored. Missing fields produce
// zero values. Returns an error only for invalid JSON.
func ParseDashboard(id string, data []byte) (*Dashboard, error) {
	var raw grafanaDashboard
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse dashboard JSON: %w", err)
	}

	d := &Dashboard{
		ID:    id,
		Title: raw.Title,
		Time: TimeRange{
			From: raw.Time.From,
			To:   raw.Time.To,
		},
		Refresh: parseRefresh(raw.Refresh),
	}

	d.Panels = make([]Panel, 0, len(raw.Panels))
	for _, gp := range raw.Panels {
		p := Panel{
			ID:    panelID(gp.ID),
			Type:  gp.Type,
			Title: gp.Title,
			GridPos: GridPos{
				X: gp.GridPos.X,
				Y: gp.GridPos.Y,
				W: gp.GridPos.W,
				H: gp.GridPos.H,
			},
			Unit: normaliseUnit(gp.FieldConfig.Defaults.Unit),
		}
		for _, gt := range gp.Targets {
			p.Targets = append(p.Targets, Target{
				Expr:         gt.Expr,
				LegendFormat: gt.LegendFormat,
				RefID:        gt.RefID,
			})
		}
		d.Panels = append(d.Panels, p)
	}

	return d, nil
}

// panelID converts a Grafana panel id (which may be a JSON number or string)
// into an Owl string ID.
func panelID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try unquoting as a JSON string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Fall back to JSON number.
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	// Last resort: strip surrounding whitespace and return the raw bytes.
	return string(raw)
}

// parseRefresh converts a Grafana refresh string (like "5s", "10m", "1h")
// into a time.Duration. Returns zero on empty input or parse failure.
func parseRefresh(s string) time.Duration {
	if s == "" {
		return 0
	}
	// time.ParseDuration handles "5s", "10m", "1h30m", etc.
	d, err := time.ParseDuration(s)
	if err != nil {
		// Grafana also accepts bare numbers as seconds (e.g. "30").
		n, err2 := strconv.ParseFloat(s, 64)
		if err2 != nil {
			return 0
		}
		return time.Duration(n * float64(time.Second))
	}
	return d
}

// normaliseUnit collapses Grafana's "no specific unit" sentinels into
// an empty string. Grafana exports use "short" (compact number, no
// unit) and "none" interchangeably for dimensionless quantities;
// surfacing either string in the panel corner is noise that does not
// help an operator read the chart, so we drop them.
func normaliseUnit(u string) string {
	switch u {
	case "short", "none":
		return ""
	default:
		return u
	}
}
