// Package dashboards reads Grafana-format dashboard JSON files from a
// directory and exposes them in memory. Unknown fields are silently ignored;
// only the subset documented in the Owl architecture spec is extracted.
package dashboards

import "time"

// Capabilities describes which PromQL expressions the query engine supports.
// The loader uses it to annotate panels with a PanelSupport status.
// internal/query's *Engine will satisfy this interface at wire-up time;
// tests use a local fake.
type Capabilities interface {
	// IsSupported reports whether the given PromQL expression is supported.
	// If not, reason is a short human-readable explanation.
	IsSupported(expr string) (supported bool, reason string)
}

// Dashboard is Owl's in-memory representation of one Grafana dashboard.
type Dashboard struct {
	ID      string // filename slug (e.g., "cluster-overview")
	Title   string
	Panels  []Panel
	Time    TimeRange     // default time range (may be zero)
	Refresh time.Duration // default refresh interval (zero if not set or unparseable)
	Source  string        // absolute path of the source JSON file
}

// Panel is one panel extracted from a Grafana dashboard.
type Panel struct {
	ID      string // Grafana panel id coerced to string
	Title   string
	Type    string // raw type from JSON ("timeseries", "stat", "gauge", or other)
	GridPos GridPos
	Targets []Target
	Unit    string       // fieldConfig.defaults.unit
	Support PanelSupport // result of consulting Capabilities
}

// GridPos holds the Grafana grid position of a panel.
type GridPos struct {
	X, Y, W, H int
}

// Target is one PromQL query target inside a panel.
type Target struct {
	Expr         string
	LegendFormat string
	RefID        string
}

// TimeRange holds a Grafana time-range pair like {"from":"now-1h","to":"now"}.
type TimeRange struct {
	From, To string
}

// PanelSupport describes whether a panel can be rendered by this Owl instance.
type PanelSupport struct {
	// Status is one of "supported", "unsupported", or "partial".
	// "partial" is reserved for future use; the MVP only emits
	// "supported" and "unsupported".
	Status string
	// Reason is non-empty when Status is not "supported".
	Reason string
}

// supportedPanelTypes is the set of Grafana panel types Owl honours in the MVP.
var supportedPanelTypes = map[string]bool{
	"timeseries": true,
	"stat":       true,
	"gauge":      true,
}
