// Package config defines Owl's configuration model and its live manager.
package config

import "time"

// Config is Owl's complete runtime configuration.
type Config struct {
	Listen     string           `yaml:"listen"`
	LogLevel   string           `yaml:"log_level"`
	Storage    StorageConfig    `yaml:"storage"`
	Scrape     ScrapeConfig     `yaml:"scrape"`
	Targets    []TargetConfig   `yaml:"targets"`
	Docker     DockerConfig     `yaml:"docker"`
	Host       HostConfig       `yaml:"host"`
	Dashboards DashboardsConfig `yaml:"dashboards"`
	Alerts     AlertsConfig     `yaml:"alerts"`
}

// StorageConfig controls the SQLite store and retention policy.
type StorageConfig struct {
	Path      string          `yaml:"path"`
	Retention RetentionPolicy `yaml:"retention"`
}

// RetentionPolicy is the dual time+size policy. Both apply; whichever
// triggers first wins. Interval controls how often the retention
// worker runs — both checks fire together on each tick. Lower values
// react sooner to size-cap violations; higher values reduce periodic
// CPU and disk noise. 30 minutes is a good default for a tiny
// self-hosted host; bring it down to 5 min on a high-write workload.
type RetentionPolicy struct {
	Time     time.Duration `yaml:"time"`
	Size     int64         `yaml:"size"`     // bytes; 0 disables the size cap
	Interval time.Duration `yaml:"interval"` // 0 falls back to the default
}

// ScrapeConfig holds defaults applied to every scrape target unless the
// target overrides them.
type ScrapeConfig struct {
	DefaultInterval time.Duration `yaml:"default_interval"`
	DefaultTimeout  time.Duration `yaml:"default_timeout"`
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
	Name     string            `yaml:"name"`
	URL      string            `yaml:"url"`
	Interval time.Duration     `yaml:"interval,omitempty"`
	Timeout  time.Duration     `yaml:"timeout,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
	Keep     []string          `yaml:"keep,omitempty"`
	Drop     []string          `yaml:"drop,omitempty"`
}

// DockerConfig groups every Docker-related capability. Both the
// per-container metrics collector and the label-based scrape-target
// discovery share the same socket connection.
type DockerConfig struct {
	Enabled    bool                  `yaml:"enabled"`
	SocketPath string                `yaml:"socket_path"`
	Metrics    DockerMetricsConfig   `yaml:"metrics"`
	Discovery  DockerDiscoveryConfig `yaml:"discovery"`
}

// DockerMetricsConfig controls the per-container metrics collector.
type DockerMetricsConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

// DockerDiscoveryConfig controls auto-discovery of scrape targets from
// container labels (containers marked with LabelPrefix=true are scraped).
type DockerDiscoveryConfig struct {
	Enabled     bool          `yaml:"enabled"`
	LabelPrefix string        `yaml:"label_prefix"`
	Interval    time.Duration `yaml:"interval"`
}

// HostConfig controls the optional Linux host collector that reads
// /proc and /sys. When running owl in a container, these typically
// point at bind-mounts (/host/proc, /host/sys). Disabled by default
// so deployments where neither path is present do not log noisy
// errors on every tick.
type HostConfig struct {
	Enabled  bool          `yaml:"enabled"`
	ProcPath string        `yaml:"proc_path"`
	SysPath  string        `yaml:"sys_path"`
	Interval time.Duration `yaml:"interval,omitempty"`
}

// DashboardsConfig points at the directory containing dashboard JSON
// files. The optional watcher polls file mtimes and reloads when one
// changes, so the operator can edit a panel and see it without
// sending SIGHUP. Off by default; opt-in keeps the project's
// "zero surprises" stance — nothing watches your filesystem unless
// you ask.
type DashboardsConfig struct {
	Dir           string        `yaml:"dir"`
	Watch         bool          `yaml:"watch"`
	WatchInterval time.Duration `yaml:"watch_interval"`
}

// AlertsConfig holds the webhook URL (typically injected from an env var)
// and the inline rule list.
type AlertsConfig struct {
	WebhookURL string      `yaml:"webhook_url"`
	Rules      []AlertRule `yaml:"rules"`
}

// AlertRule is the simplest possible threshold rule: when expression
// evaluates above (or below, controlled by Op) Threshold for For duration,
// fire.
type AlertRule struct {
	Name      string        `yaml:"name"`
	Expr      string        `yaml:"expr"`
	Op        string        `yaml:"op"` // ">", ">=", "<", "<="
	Threshold float64       `yaml:"threshold"`
	For       time.Duration `yaml:"for"`
}
