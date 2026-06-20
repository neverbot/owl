package config

import (
	"os"
	"path/filepath"
	"strings"
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
docker:
  enabled: true
  discovery:
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
	if !c.Docker.Enabled {
		t.Error("Docker.Enabled should be true")
	}
	if !c.Docker.Discovery.Enabled {
		t.Error("Docker.Discovery.Enabled should be true")
	}

	// Unset fields keep their default values:
	if c.Docker.SocketPath != "/var/run/docker.sock" {
		t.Errorf("default SocketPath was not preserved: %q", c.Docker.SocketPath)
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

func TestLoadRejectsInvalidKeepRegex(t *testing.T) {
	// An unterminated alternation must fail validation rather than
	// crash later in mustCompilePatterns or silently drop nothing.
	path := writeTemp(t, "bad-keep.yml", `
targets:
  - name: t
    url: "http://x/metrics"
    keep:
      - "(?P<oops"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid keep regex")
	}
}

func TestLoadAcceptsValidKeepAndDrop(t *testing.T) {
	path := writeTemp(t, "filters.yml", `
targets:
  - name: t
    url: "http://x/metrics"
    keep:
      - "^foo_.*$"
    drop:
      - "_bucket$"
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Targets[0].Keep) != 1 || c.Targets[0].Keep[0] != "^foo_.*$" {
		t.Errorf("keep: %v", c.Targets[0].Keep)
	}
	if len(c.Targets[0].Drop) != 1 || c.Targets[0].Drop[0] != "_bucket$" {
		t.Errorf("drop: %v", c.Targets[0].Drop)
	}
}

func TestLoadBytesValid(t *testing.T) {
	c, err := LoadBytes([]byte(sampleYAML))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if c.Listen != "0.0.0.0:9090" {
		t.Errorf("listen: %q", c.Listen)
	}
}

func TestLoadBytesInvalid(t *testing.T) {
	if _, err := LoadBytes([]byte(": not yaml :")); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadBytesEmpty(t *testing.T) {
	c, err := LoadBytes(nil)
	if err != nil {
		t.Fatalf("LoadBytes empty: %v", err)
	}
	if c.Listen == "" {
		t.Error("expected defaults applied")
	}
}

func TestLoadEmptyFileUsesDefaults(t *testing.T) {
	// An empty or comment-only YAML file must not be a parse error: the
	// caller gets a Config equal to Default(), which then passes Validate.
	d := Default()

	for _, body := range []string{"", "   \n  \n", "# only a comment\n"} {
		path := writeTemp(t, "empty.yml", body)
		got, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%q): %v", body, err)
		}
		if got.Listen != d.Listen || got.Storage.Path != d.Storage.Path {
			t.Errorf("Load(%q) did not return defaults: %+v", body, got)
		}
	}
}

func TestLoadBytes_ExpandsEnv(t *testing.T) {
	t.Setenv("OWL_TEST_LISTEN", "127.0.0.1:9090")
	data := []byte(`
listen: ${OWL_TEST_LISTEN}
storage:
  path: /tmp/owl.db
  retention:
    time: 1h
    size: 100MB
    interval: 1m
scrape:
  default_interval: 30s
  default_timeout: 10s
`)
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if c.Listen != "127.0.0.1:9090" {
		t.Fatalf("want listen 127.0.0.1:9090, got %q", c.Listen)
	}
}

func TestLoadBytes_MissingEnvFails(t *testing.T) {
	data := []byte(`
listen: ${OWL_MISSING_TEST_VAR}
storage:
  path: /tmp/owl.db
  retention:
    time: 1h
    size: 100MB
    interval: 1m
scrape:
  default_interval: 30s
  default_timeout: 10s
`)
	if _, err := LoadBytes(data); err == nil {
		t.Fatal("expected error for missing variable")
	}
}

func loadValidating(t *testing.T, extra string) error {
	t.Helper()
	base := `
listen: 127.0.0.1:9090
storage:
  path: /tmp/owl.db
  retention:
    time: 1h
    size: 100MB
    interval: 1m
scrape:
  default_interval: 30s
  default_timeout: 10s
`
	_, err := LoadBytes([]byte(base + extra))
	return err
}

func TestValidate_AuthBearerAndBasicMutuallyExclusive(t *testing.T) {
	err := loadValidating(t, `
targets:
  - name: x
    url: http://x/metrics
    auth:
      bearer_token: abc
      basic:
        username: u
        password: p
`)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want mutually-exclusive error, got %v", err)
	}
}

func TestValidate_AuthAuthorizationHeaderConflict(t *testing.T) {
	err := loadValidating(t, `
targets:
  - name: x
    url: http://x/metrics
    auth:
      bearer_token: abc
      headers:
        Authorization: Bearer other
`)
	if err == nil || !strings.Contains(err.Error(), "Authorization") {
		t.Fatalf("want Authorization-conflict error, got %v", err)
	}
}

func TestValidate_AuthOnlyHeadersAllowed(t *testing.T) {
	if err := loadValidating(t, `
targets:
  - name: x
    url: http://x/metrics
    auth:
      headers:
        X-API-Key: k
`); err != nil {
		t.Fatalf("headers-only auth must be valid, got %v", err)
	}
}

func TestValidate_TLSOnHTTPRejected(t *testing.T) {
	err := loadValidating(t, `
targets:
  - name: x
    url: http://x/metrics
    tls:
      insecure_skip_verify: true
`)
	if err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("want http-not-allowed error, got %v", err)
	}
}

func TestValidate_TLSCAFileMustExist(t *testing.T) {
	err := loadValidating(t, `
targets:
  - name: x
    url: https://x/metrics
    tls:
      ca_file: /nonexistent/path/ca.pem
`)
	if err == nil {
		t.Fatal("want missing-ca-file error")
	}
}

// TestLoadEvents_Minimal asserts a minimal events block with one
// file_tail source parses, validates and round-trips.
func TestLoadEvents_Minimal(t *testing.T) {
	data := []byte(`
listen: 127.0.0.1:9090
storage:
  path: /tmp/owl.db
  retention: { time: 7d, size: 0, interval: 1m }
scrape: { default_interval: 30s, default_timeout: 10s }
events:
  enabled: true
  sources:
    - name: access
      driver: file_tail
      path: /var/log/nginx/access.log
      from: end
      interval: 30s
      format: regex
      pattern: '^(?P<ip>\S+).*$'
      mapping:
        kind: hit
        payload: { ip: "$.ip" }
      render: "hit from {{.ip}}"
`)
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !c.Events.Enabled {
		t.Fatal("enabled false")
	}
	if len(c.Events.Sources) != 1 || c.Events.Sources[0].Driver != "file_tail" {
		t.Fatalf("sources: %#v", c.Events.Sources)
	}
}

// TestLoadEvents_BadDriver asserts validation rejects unknown drivers.
func TestLoadEvents_BadDriver(t *testing.T) {
	data := []byte(`
listen: 127.0.0.1:9090
storage: { path: /tmp/owl.db, retention: { time: 7d, size: 0, interval: 1m } }
scrape: { default_interval: 30s, default_timeout: 10s }
events:
  enabled: true
  sources:
    - { name: x, driver: journald, format: plain, mapping: { kind: k } }
`)
	if _, err := LoadBytes(data); err == nil {
		t.Fatal("want validation error, got nil")
	}
}

// TestLoadEvents_DockerLogsRequiresContainer asserts the
// driver-specific required field check.
func TestLoadEvents_DockerLogsRequiresContainer(t *testing.T) {
	data := []byte(`
listen: 127.0.0.1:9090
storage: { path: /tmp/owl.db, retention: { time: 7d, size: 0, interval: 1m } }
scrape: { default_interval: 30s, default_timeout: 10s }
events:
  enabled: true
  sources:
    - { name: w, driver: docker_logs, format: json, mapping: { kind: k } }
`)
	if _, err := LoadBytes(data); err == nil {
		t.Fatal("want error about missing container")
	}
}
