package config

import "os"

// ApplyEnv overlays known OWL_* environment variables on top of c, in
// place. Unset variables leave c untouched.
//
// Variables recognised:
//
//	OWL_LISTEN_ADDR        -> Listen
//	OWL_LOG_LEVEL          -> LogLevel
//	OWL_DB_PATH            -> Storage.Path
//	OWL_ALERT_WEBHOOK_URL  -> Alerts.WebhookURL
//
// Only string-valued operational settings and secrets are accepted via
// env vars. Structured settings (target lists, retention policy, rules)
// remain in the YAML file.
func ApplyEnv(c *Config) {
	if v := os.Getenv("OWL_LISTEN_ADDR"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("OWL_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("OWL_DB_PATH"); v != "" {
		c.Storage.Path = v
	}
	if v := os.Getenv("OWL_ALERT_WEBHOOK_URL"); v != "" {
		c.Alerts.WebhookURL = v
	}
}
