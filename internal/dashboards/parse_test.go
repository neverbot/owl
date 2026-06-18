package dashboards

import (
	"os"
	"testing"
	"time"
)

func TestParseDashboard_Runtime(t *testing.T) {
	data, err := os.ReadFile("testdata/runtime.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	d, err := ParseDashboard("runtime", data)
	if err != nil {
		t.Fatalf("ParseDashboard: %v", err)
	}

	// Top-level fields
	if d.ID != "runtime" {
		t.Errorf("ID = %q, want %q", d.ID, "runtime")
	}
	if d.Title != "Runtime" {
		t.Errorf("Title = %q, want %q", d.Title, "Runtime")
	}
	if d.Refresh != 5*time.Second {
		t.Errorf("Refresh = %v, want 5s", d.Refresh)
	}
	if d.Time.From != "now-15m" {
		t.Errorf("Time.From = %q, want %q", d.Time.From, "now-15m")
	}
	if d.Time.To != "now" {
		t.Errorf("Time.To = %q, want %q", d.Time.To, "now")
	}

	// Panel count
	if len(d.Panels) != 3 {
		t.Fatalf("len(Panels) = %d, want 3", len(d.Panels))
	}

	// First panel
	p0 := d.Panels[0]
	if p0.ID != "1" {
		t.Errorf("Panels[0].ID = %q, want %q", p0.ID, "1")
	}
	if p0.Type != "timeseries" {
		t.Errorf("Panels[0].Type = %q, want %q", p0.Type, "timeseries")
	}
	if p0.Title != "goroutines" {
		t.Errorf("Panels[0].Title = %q, want %q", p0.Title, "goroutines")
	}
	if p0.GridPos != (GridPos{X: 0, Y: 0, W: 12, H: 8}) {
		t.Errorf("Panels[0].GridPos = %+v, want {0 0 12 8}", p0.GridPos)
	}
	if p0.Unit != "" {
		t.Errorf("Panels[0].Unit = %q, want %q (\"short\" should normalise to empty)", p0.Unit, "")
	}
	if len(p0.Targets) != 1 {
		t.Fatalf("Panels[0].Targets len = %d, want 1", len(p0.Targets))
	}
	tgt := p0.Targets[0]
	if tgt.Expr != "owl_runtime_goroutines" {
		t.Errorf("target Expr = %q, want %q", tgt.Expr, "owl_runtime_goroutines")
	}
	if tgt.LegendFormat != "goroutines" {
		t.Errorf("target LegendFormat = %q, want %q", tgt.LegendFormat, "goroutines")
	}
	if tgt.RefID != "A" {
		t.Errorf("target RefID = %q, want %q", tgt.RefID, "A")
	}

	// Second panel — no legendFormat
	p1 := d.Panels[1]
	if p1.ID != "2" {
		t.Errorf("Panels[1].ID = %q, want %q", p1.ID, "2")
	}
	if len(p1.Targets) != 1 || p1.Targets[0].LegendFormat != "" {
		t.Errorf("Panels[1] expected one target with empty LegendFormat")
	}
}

func TestParseDashboard_UnknownFields(t *testing.T) {
	data, err := os.ReadFile("testdata/unknown_fields.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	d, err := ParseDashboard("unknown-fields", data)
	if err != nil {
		t.Fatalf("ParseDashboard must not error on unknown fields: %v", err)
	}

	if d.Title != "Unknown Fields Test" {
		t.Errorf("Title = %q, want %q", d.Title, "Unknown Fields Test")
	}
	if len(d.Panels) != 1 {
		t.Fatalf("len(Panels) = %d, want 1", len(d.Panels))
	}

	p := d.Panels[0]
	// Panel id is a string in this fixture
	if p.ID != "panel-99" {
		t.Errorf("Panel ID = %q, want %q", p.ID, "panel-99")
	}
	if p.Type != "unknown-type" {
		t.Errorf("Panel Type = %q, want %q", p.Type, "unknown-type")
	}
	if p.Unit != "percent" {
		t.Errorf("Panel Unit = %q, want %q", p.Unit, "percent")
	}
}

func TestParseDashboard_RefreshVariants(t *testing.T) {
	cases := []struct {
		json string
		want time.Duration
	}{
		{`{"title":"T","panels":[],"refresh":"10s"}`, 10 * time.Second},
		{`{"title":"T","panels":[],"refresh":"1m"}`, time.Minute},
		{`{"title":"T","panels":[],"refresh":"1h"}`, time.Hour},
		{`{"title":"T","panels":[],"refresh":""}`, 0},
		{`{"title":"T","panels":[],"refresh":"bogus"}`, 0},
		{`{"title":"T","panels":[]}`, 0},
	}
	for _, tc := range cases {
		d, err := ParseDashboard("x", []byte(tc.json))
		if err != nil {
			t.Errorf("json=%s: unexpected error: %v", tc.json, err)
			continue
		}
		if d.Refresh != tc.want {
			t.Errorf("json=%s: Refresh=%v, want %v", tc.json, d.Refresh, tc.want)
		}
	}
}

func TestParseDashboard_PanelIDCoercion(t *testing.T) {
	// Grafana uses integer ids; they must become strings
	raw := `{
		"title": "T",
		"panels": [
			{"id": 42, "type": "stat", "title": "x", "gridPos": {"x":0,"y":0,"w":6,"h":4}, "targets": []}
		]
	}`
	d, err := ParseDashboard("t", []byte(raw))
	if err != nil {
		t.Fatalf("ParseDashboard: %v", err)
	}
	if len(d.Panels) != 1 {
		t.Fatalf("want 1 panel")
	}
	if d.Panels[0].ID != "42" {
		t.Errorf("Panel.ID = %q, want %q", d.Panels[0].ID, "42")
	}
}

func TestParseDashboard_InvalidJSON(t *testing.T) {
	_, err := ParseDashboard("bad", []byte(`{not valid json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestNormaliseUnit(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"short", ""},
		{"none", ""},
		{"bytes", "bytes"},
		{"s", "s"},
		{"ops", "ops"},
		{"Short", "Short"}, // case-sensitive; Grafana exports always lowercase
	}
	for _, c := range cases {
		if got := normaliseUnit(c.in); got != c.want {
			t.Errorf("normaliseUnit(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParse_StatCalcDefault(t *testing.T) {
	data := []byte(`{
		"panels": [
			{"id": 1, "type": "stat", "title": "p", "targets": [{"expr": "up"}]}
		]
	}`)
	d, err := ParseDashboard("x", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(d.Panels) != 1 {
		t.Fatalf("got %d panels, want 1", len(d.Panels))
	}
	if got := d.Panels[0].Calc; got != "lastNotNull" {
		t.Errorf("Calc = %q, want lastNotNull (default)", got)
	}
}

func TestParse_StatCalcExplicit(t *testing.T) {
	data := []byte(`{
		"panels": [
			{
				"id": 1, "type": "stat", "title": "p",
				"options": {"reduceOptions": {"calcs": ["max"]}}
			}
		]
	}`)
	d, err := ParseDashboard("x", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := d.Panels[0].Calc; got != "max" {
		t.Errorf("Calc = %q, want max", got)
	}
}

func TestParse_StatCalcUnknownFallsBack(t *testing.T) {
	data := []byte(`{
		"panels": [
			{
				"id": 1, "type": "stat", "title": "p",
				"options": {"reduceOptions": {"calcs": ["bogus"]}}
			}
		]
	}`)
	d, err := ParseDashboard("x", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := d.Panels[0].Calc; got != "lastNotNull" {
		t.Errorf("Calc = %q, want lastNotNull (fallback)", got)
	}
}

func TestParse_FieldConfigDecimals(t *testing.T) {
	data := []byte(`{
		"panels": [
			{
				"id": 1, "type": "stat", "title": "p",
				"fieldConfig": {"defaults": {"decimals": 2}}
			}
		]
	}`)
	d, err := ParseDashboard("x", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Panels[0].Decimals == nil || *d.Panels[0].Decimals != 2 {
		t.Errorf("Decimals = %v, want *int(2)", d.Panels[0].Decimals)
	}
}

func TestParse_FieldConfigDecimalsUnset(t *testing.T) {
	data := []byte(`{
		"panels": [
			{"id": 1, "type": "stat", "title": "p"}
		]
	}`)
	d, err := ParseDashboard("x", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Panels[0].Decimals != nil {
		t.Errorf("Decimals = %v, want nil", d.Panels[0].Decimals)
	}
}

func TestParse_StatGraphModeArea(t *testing.T) {
	data := []byte(`{
		"panels": [
			{
				"id": 1, "type": "stat", "title": "p",
				"options": {"graphMode": "area"}
			}
		]
	}`)
	d, err := ParseDashboard("x", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := d.Panels[0].GraphMode; got != "area" {
		t.Errorf("GraphMode = %q, want area", got)
	}
}

func TestParse_StatGraphModeNone(t *testing.T) {
	data := []byte(`{
		"panels": [
			{"id": 1, "type": "stat", "title": "p", "options": {"graphMode": "none"}}
		]
	}`)
	d, err := ParseDashboard("x", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Panels[0].GraphMode != "none" {
		t.Errorf("GraphMode = %q, want none", d.Panels[0].GraphMode)
	}
}
