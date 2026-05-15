package config

import "time"

// Default returns the baseline Config used before any YAML, env, or flag
// overrides are applied.
func Default() Config {
	return Config{
		Listen:   "127.0.0.1:9090",
		LogLevel: "info",
		Storage: StorageConfig{
			Path: "/var/lib/owl/owl.db",
			Retention: RetentionPolicy{
				Time: 30 * 24 * time.Hour, // 30 days
				Size: 0,                   // disabled unless user opts in
			},
		},
		Scrape: ScrapeConfig{
			DefaultInterval: 15 * time.Second,
			DefaultTimeout:  10 * time.Second,
		},
		Discovery: DiscoveryConfig{
			Docker: DockerDiscoveryConfig{
				Enabled:     false,
				SocketPath:  "/var/run/docker.sock",
				LabelPrefix: "owl.scrape",
			},
		},
		Dashboards: DashboardsConfig{
			Dir: "/etc/owl/dashboards",
		},
	}
}
