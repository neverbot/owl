// Package config defines Owl's configuration model and its live manager.
package config

import "time"

// Config is Owl's complete runtime configuration.
type Config struct {
	Listen     string           `yaml:"listen"     doc:"HTTP listen address, e.g. \"0.0.0.0:9090\"."`
	LogLevel   string           `yaml:"log_level"  doc:"slog level: debug, info, warn, or error."`
	Storage    StorageConfig    `yaml:"storage"    doc:"SQLite storage settings."`
	Scrape     ScrapeConfig     `yaml:"scrape"     doc:"Defaults applied to every scrape target."`
	Targets    []TargetConfig   `yaml:"targets"    doc:"Explicit /metrics endpoints to scrape."`
	Docker     DockerConfig     `yaml:"docker"     doc:"Docker integration (per-container metrics plus label-based target discovery)."`
	Host       HostConfig       `yaml:"host"       doc:"Linux host collector reading /proc and /sys."`
	Dashboards DashboardsConfig `yaml:"dashboards" doc:"Where dashboard JSON files live, plus optional mtime watcher."`
	Alerts     AlertsConfig     `yaml:"alerts"     doc:"Threshold alert rules and the webhook delivery target."`
}

// StorageConfig controls the SQLite store and retention policy.
type StorageConfig struct {
	Path      string          `yaml:"path"      doc:"Filesystem path to the SQLite database file."`
	Retention RetentionPolicy `yaml:"retention" doc:"Time- and size-based retention policy."`
}

// RetentionPolicy is the dual time+size policy. Both apply; whichever
// triggers first wins. Interval controls how often the retention
// worker runs — both checks fire together on each tick. Lower values
// react sooner to size-cap violations; higher values reduce periodic
// CPU and disk noise. 30 minutes is a good default for a tiny
// self-hosted host; bring it down to 5 min on a high-write workload.
type RetentionPolicy struct {
	Time     time.Duration `yaml:"time"     doc:"Maximum sample age; older samples are deleted on each retention tick."`
	Size     int64         `yaml:"size"     doc:"Maximum on-disk size in bytes; 0 disables the size cap."`
	Interval time.Duration `yaml:"interval" doc:"How often the retention worker runs; 0 falls back to the default."`
}

// ScrapeConfig holds defaults applied to every scrape target unless the
// target overrides them.
type ScrapeConfig struct {
	DefaultInterval time.Duration `yaml:"default_interval" doc:"Default scrape interval applied to targets that do not override it."`
	DefaultTimeout  time.Duration `yaml:"default_timeout"  doc:"Default HTTP timeout applied to targets that do not override it."`
}

// TargetConfig declares an explicit HTTP scrape target. The auto-
// discovered Docker targets live separately and are merged at runtime.
//
// Keep and Drop are optional metric-name filters applied to the parsed
// batch before it reaches storage. Each entry is a regular expression
// (RE2 syntax, anchored implicitly at both ends by the parser). When
// Keep is non-empty only metrics whose name matches one of the
// patterns are stored; Drop then removes any survivor whose name
// matches one of its patterns. Empty filters preserve the historical
// behaviour of storing everything the endpoint exposes.
type TargetConfig struct {
	Name     string            `yaml:"name"               doc:"Logical target name shown in the UI and attached as the \"job\" label."`
	URL      string            `yaml:"url"                doc:"HTTP URL of the Prometheus-format /metrics endpoint to scrape."`
	Interval time.Duration     `yaml:"interval,omitempty" doc:"Per-target scrape interval; overrides scrape.default_interval."`
	Timeout  time.Duration     `yaml:"timeout,omitempty"  doc:"Per-target HTTP timeout; overrides scrape.default_timeout."`
	Labels   map[string]string `yaml:"labels,omitempty"   doc:"Static labels attached to every sample scraped from this target."`
	Keep     []string          `yaml:"keep,omitempty"     doc:"Regex allow-list of metric names; empty stores everything."`
	Drop     []string          `yaml:"drop,omitempty"     doc:"Regex deny-list of metric names applied after keep."`
}

// DockerConfig groups every Docker-related capability. Both the
// per-container metrics collector and the label-based scrape-target
// discovery share the same socket connection.
type DockerConfig struct {
	Enabled    bool                  `yaml:"enabled"     doc:"Master switch for the Docker integration; off disables both metrics and discovery."`
	SocketPath string                `yaml:"socket_path" doc:"Path to the Docker daemon socket inside the owl container."`
	Metrics    DockerMetricsConfig   `yaml:"metrics"     doc:"Per-container metrics collector (CPU, memory, network, block I/O)."`
	Discovery  DockerDiscoveryConfig `yaml:"discovery"   doc:"Auto-discovery of scrape targets from container labels."`
}

// DockerMetricsConfig controls the per-container metrics collector.
type DockerMetricsConfig struct {
	Enabled  bool          `yaml:"enabled"  doc:"Enable the per-container metrics collector."`
	Interval time.Duration `yaml:"interval" doc:"How often the collector samples each container's stats."`
}

// DockerDiscoveryConfig controls auto-discovery of scrape targets from
// container labels (containers marked with LabelPrefix=true are scraped).
type DockerDiscoveryConfig struct {
	Enabled     bool          `yaml:"enabled"      doc:"Enable container-label scrape-target discovery."`
	LabelPrefix string        `yaml:"label_prefix" doc:"Label prefix used to opt containers into discovery (e.g. \"owl.scrape\")."`
	Interval    time.Duration `yaml:"interval"     doc:"How often the discovery loop re-reads the container list."`
}

// HostConfig controls the optional Linux host collector that reads
// /proc and /sys. When running owl in a container, these typically
// point at bind-mounts (/host/proc, /host/sys). Disabled by default
// so deployments where neither path is present do not log noisy
// errors on every tick.
type HostConfig struct {
	Enabled  bool          `yaml:"enabled"            doc:"Enable the Linux host collector; requires /proc and /sys to be readable."`
	ProcPath string        `yaml:"proc_path"          doc:"Path to the procfs root, typically /proc or /host/proc inside a container."`
	SysPath  string        `yaml:"sys_path"           doc:"Path to the sysfs root, typically /sys or /host/sys inside a container."`
	Interval time.Duration `yaml:"interval,omitempty" doc:"How often the host collector samples; 0 uses the built-in default."`
}

// DashboardsConfig points at the directory containing dashboard JSON
// files. The optional watcher polls file mtimes and reloads when one
// changes, so the operator can edit a panel and see it without
// sending SIGHUP. Off by default; opt-in keeps the project's
// "zero surprises" stance — nothing watches your filesystem unless
// you ask.
type DashboardsConfig struct {
	Dir           string        `yaml:"dir"            doc:"Directory containing dashboard JSON files; loaded recursively on startup and reload."`
	Watch         bool          `yaml:"watch"          doc:"Enable the mtime-based watcher that hot-reloads dashboards when their files change."`
	WatchInterval time.Duration `yaml:"watch_interval" doc:"How often the watcher polls file mtimes; ignored when watch is false."`
}

// AlertsConfig holds the webhook URL (typically injected from an env var)
// and the inline rule list.
type AlertsConfig struct {
	WebhookURL string      `yaml:"webhook_url" doc:"Outbound webhook URL the alerter POSTs to when a rule fires or resolves."`
	Rules      []AlertRule `yaml:"rules"       doc:"Inline list of threshold alert rules evaluated on every tick."`
}

// AlertRule is the simplest possible threshold rule: when expression
// evaluates above (or below, controlled by Op) Threshold for For duration,
// fire.
type AlertRule struct {
	Name      string        `yaml:"name"      doc:"Human-readable rule name included in the webhook payload."`
	Expr      string        `yaml:"expr"      doc:"PromQL expression evaluated on every alerter tick."`
	Op        string        `yaml:"op"        doc:"Comparison operator: \">\", \">=\", \"<\", or \"<=\"."`
	Threshold float64       `yaml:"threshold" doc:"Numeric threshold the expression is compared against."`
	For       time.Duration `yaml:"for"       doc:"How long the condition must hold continuously before the rule fires."`
}
