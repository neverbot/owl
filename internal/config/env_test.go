package config

import (
	"reflect"
	"testing"
)

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("OWL_LISTEN_ADDR", "0.0.0.0:8080")
	t.Setenv("OWL_LOG_LEVEL", "warn")
	t.Setenv("OWL_DB_PATH", "/tmp/owl.db")
	t.Setenv("OWL_ALERT_WEBHOOK_URL", "https://hooks.example/abc")

	c := Default()
	ApplyEnv(&c)

	if c.Listen != "0.0.0.0:8080" {
		t.Errorf("Listen = %q", c.Listen)
	}
	if c.LogLevel != "warn" {
		t.Errorf("LogLevel = %q", c.LogLevel)
	}
	if c.Storage.Path != "/tmp/owl.db" {
		t.Errorf("Storage.Path = %q", c.Storage.Path)
	}
	if c.Alerts.WebhookURL != "https://hooks.example/abc" {
		t.Errorf("Alerts.WebhookURL = %q", c.Alerts.WebhookURL)
	}
}

func TestApplyEnvLeavesUnchangedWhenUnset(t *testing.T) {
	c := Default()
	original := c
	ApplyEnv(&c)
	if !reflect.DeepEqual(c, original) {
		t.Errorf("ApplyEnv modified config without any env vars set")
	}
}
