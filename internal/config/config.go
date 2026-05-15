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
	Discovery  DiscoveryConfig  `yaml:"discovery"`
	Dashboards DashboardsConfig `yaml:"dashboards"`
	Alerts     AlertsConfig     `yaml:"alerts"`
}

// StorageConfig controls the SQLite store and retention policy.
type StorageConfig struct {
	Path      string          `yaml:"path"`
	Retention RetentionPolicy `yaml:"retention"`
}

// RetentionPolicy is the dual time+size policy. Both apply; whichever
// triggers first wins.
type RetentionPolicy struct {
	Time time.Duration `yaml:"time"`
	Size int64         `yaml:"size"` // bytes; 0 disables the size cap
}

// ScrapeConfig holds defaults applied to every scrape target unless the
// target overrides them.
type ScrapeConfig struct {
	DefaultInterval time.Duration `yaml:"default_interval"`
	DefaultTimeout  time.Duration `yaml:"default_timeout"`
}

// TargetConfig declares an explicit HTTP scrape target. The auto-discovered
// Docker targets live separately and are merged at runtime.
type TargetConfig struct {
	Name     string            `yaml:"name"`
	URL      string            `yaml:"url"`
	Interval time.Duration     `yaml:"interval,omitempty"`
	Timeout  time.Duration     `yaml:"timeout,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
}

// DiscoveryConfig controls auto-discovery of targets via Docker labels.
type DiscoveryConfig struct {
	Docker DockerDiscoveryConfig `yaml:"docker"`
}

// DockerDiscoveryConfig holds the parameters for auto-discovering scrape
// targets from Docker container labels.
type DockerDiscoveryConfig struct {
	Enabled     bool   `yaml:"enabled"`
	SocketPath  string `yaml:"socket_path"`
	LabelPrefix string `yaml:"label_prefix"`
}

// DashboardsConfig points at the directory containing dashboard JSON files.
type DashboardsConfig struct {
	Dir string `yaml:"dir"`
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
