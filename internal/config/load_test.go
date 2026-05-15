package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sampleYAML = `
listen: "0.0.0.0:9090"
log_level: "debug"
storage:
  path: "/data/owl.db"
  retention:
    time: 7d
    size: 100MB
scrape:
  default_interval: 30s
  default_timeout: 5s
targets:
  - name: traefik
    url: "http://traefik:8082/metrics"
    labels:
      job: traefik
discovery:
  docker:
    enabled: true
dashboards:
  dir: "/etc/owl/dashboards"
`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp %s: %v", name, err)
	}
	return p
}

func TestLoadMergesYAMLOnTopOfDefaults(t *testing.T) {
	path := writeTemp(t, "owl.yml", sampleYAML)

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if c.Listen != "0.0.0.0:9090" {
		t.Errorf("Listen = %q, want 0.0.0.0:9090", c.Listen)
	}
	if c.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", c.LogLevel)
	}
	if c.Storage.Path != "/data/owl.db" {
		t.Errorf("Storage.Path = %q", c.Storage.Path)
	}
	if c.Storage.Retention.Time != 7*24*time.Hour {
		t.Errorf("Retention.Time = %v, want 7d", c.Storage.Retention.Time)
	}
	if c.Storage.Retention.Size != 100*1024*1024 {
		t.Errorf("Retention.Size = %d, want 100MB in bytes", c.Storage.Retention.Size)
	}
	if c.Scrape.DefaultInterval != 30*time.Second {
		t.Errorf("DefaultInterval = %v, want 30s", c.Scrape.DefaultInterval)
	}
	if len(c.Targets) != 1 || c.Targets[0].Name != "traefik" {
		t.Errorf("Targets parsed incorrectly: %+v", c.Targets)
	}
	if !c.Discovery.Docker.Enabled {
		t.Error("Discovery.Docker.Enabled should be true")
	}

	// Unset fields keep their default values:
	if c.Discovery.Docker.SocketPath != "/var/run/docker.sock" {
		t.Errorf("default SocketPath was not preserved: %q", c.Discovery.Docker.SocketPath)
	}
}

func TestLoadReturnsErrorOnMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/owl.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadReturnsErrorOnInvalidYAML(t *testing.T) {
	path := writeTemp(t, "bad.yml", "this is: not: valid: yaml: [")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestLoadValidatesRequiredFields(t *testing.T) {
	// listen is required and must be non-empty.
	path := writeTemp(t, "empty-listen.yml", `listen: ""`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty listen")
	}
}
