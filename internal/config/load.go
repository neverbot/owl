package config

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML file at path, expands ${VAR}/file: secrets,
// merges the result on top of Default(), and validates it. Unknown
// fields are rejected so typos surface.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	c, err := loadFromBytes(raw, defaultEnv(), defaultRead())
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

// LoadBytes is Load that takes an in-memory YAML payload rather than
// a path. It applies Default(), expands ${VAR}/file: secrets,
// unmarshals strictly, then validates. Used by docs validation to
// parse inline examples.
func LoadBytes(data []byte) (Config, error) {
	return loadFromBytes(data, defaultEnv(), defaultRead())
}

// loadFromBytes is the common path used by Load and LoadBytes. It
// parses the YAML into a node tree, expands every string scalar via
// expandNode, re-marshals to bytes, and finally decodes strictly
// into a Config so KnownFields(true) still rejects typos. Failing to
// re-marshal is unreachable in practice — we just round-tripped from
// a valid node — but we propagate it for completeness.
func loadFromBytes(data []byte, env func(string) (string, bool), read func(string) ([]byte, error)) (Config, error) {
	c := Default()
	if len(data) == 0 {
		if err := Validate(&c); err != nil {
			return Config{}, fmt.Errorf("invalid: %w", err)
		}
		return c, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Config{}, fmt.Errorf("parse: %w", err)
	}
	if err := expandNode(&doc, env, read); err != nil {
		return Config{}, err
	}
	if doc.Kind != 0 {
		expanded, err := yaml.Marshal(&doc)
		if err != nil {
			return Config{}, fmt.Errorf("re-marshal: %w", err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(expanded))
		dec.KnownFields(true)
		if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("decode: %w", err)
		}
	}
	if err := Validate(&c); err != nil {
		return Config{}, fmt.Errorf("invalid: %w", err)
	}
	return c, nil
}

// Validate enforces semantic invariants on a Config. It must be called
// after Load and after env-var application.
func Validate(c *Config) error {
	if c.Listen == "" {
		return errors.New("listen must be set")
	}
	if c.Storage.Path == "" {
		return errors.New("storage.path must be set")
	}
	if c.Storage.Retention.Time <= 0 {
		return errors.New("storage.retention.time must be positive")
	}
	if c.Storage.Retention.Size < 0 {
		return errors.New("storage.retention.size must be >= 0")
	}
	if c.Storage.Retention.Interval <= 0 {
		return errors.New("storage.retention.interval must be positive")
	}
	if c.Scrape.DefaultInterval <= 0 {
		return errors.New("scrape.default_interval must be positive")
	}
	if c.Scrape.DefaultTimeout <= 0 {
		return errors.New("scrape.default_timeout must be positive")
	}
	if c.Scrape.DefaultTimeout >= c.Scrape.DefaultInterval {
		return errors.New("scrape.default_timeout must be < scrape.default_interval")
	}
	for i, t := range c.Targets {
		if t.Name == "" {
			return fmt.Errorf("targets[%d]: name is required", i)
		}
		if t.URL == "" {
			return fmt.Errorf("targets[%d] (%s): url is required", i, t.Name)
		}
		for j, p := range t.Keep {
			if _, err := regexp.Compile(p); err != nil {
				return fmt.Errorf("targets[%d] (%s): keep[%d] %q: %w", i, t.Name, j, p, err)
			}
		}
		for j, p := range t.Drop {
			if _, err := regexp.Compile(p); err != nil {
				return fmt.Errorf("targets[%d] (%s): drop[%d] %q: %w", i, t.Name, j, p, err)
			}
		}
		if err := validateAuth(i, t); err != nil {
			return err
		}
		if err := validateTLS(i, t); err != nil {
			return err
		}
	}
	if err := validateEvents(c); err != nil {
		return err
	}
	return nil
}

// UnmarshalYAML on RetentionPolicy accepts string values only: "7d", "12h"
// for Time and "100MB", "2GB" for Size. Numeric YAML scalars are rejected
// — durations and sizes must be expressed with explicit units to keep the
// config readable.
//
// Note: the outer decoder's KnownFields(true) setting does not propagate
// into custom UnmarshalYAML methods. A typo inside the retention mapping
// (e.g. "tme" instead of "time") will be silently ignored here rather
// than surfacing as an "unknown field" error.
func (r *RetentionPolicy) UnmarshalYAML(node *yaml.Node) error {
	// Accept either the mapping form or a structural decoding.
	type raw struct {
		Time     string `yaml:"time"`
		Size     string `yaml:"size"`
		Interval string `yaml:"interval"`
	}
	var x raw
	if err := node.Decode(&x); err != nil {
		return err
	}
	if x.Time != "" {
		d, err := parseDuration(x.Time)
		if err != nil {
			return fmt.Errorf("retention.time: %w", err)
		}
		r.Time = d
	}
	if x.Size != "" {
		n, err := parseSize(x.Size)
		if err != nil {
			return fmt.Errorf("retention.size: %w", err)
		}
		r.Size = n
	}
	if x.Interval != "" {
		d, err := parseDuration(x.Interval)
		if err != nil {
			return fmt.Errorf("retention.interval: %w", err)
		}
		r.Interval = d
	}
	return nil
}

// parseDuration extends time.ParseDuration with a "d" (24h) suffix.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		num := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(num)
		if err != nil {
			return 0, fmt.Errorf("invalid days value %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// parseSize parses sizes like "500", "100KB", "2MB", "1GB" into bytes.
// It is case-insensitive. No suffix means bytes.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	mults := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}
	for _, m := range mults {
		if strings.HasSuffix(s, m.suffix) {
			num := strings.TrimSuffix(s, m.suffix)
			n, err := strconv.ParseInt(strings.TrimSpace(num), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return n * m.mult, nil
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

// validateEvents enforces semantic invariants on EventsConfig: each
// source must have a name, a recognised driver, a recognised format,
// the driver-specific required fields, and (when format=regex) a
// compilable pattern with at least one named group.
func validateEvents(c *Config) error {
	if !c.Events.Enabled {
		return nil
	}
	seen := map[string]bool{}
	for i, src := range c.Events.Sources {
		if src.Name == "" {
			return fmt.Errorf("events.sources[%d]: name is required", i)
		}
		if seen[src.Name] {
			return fmt.Errorf("events.sources[%d]: duplicate name %q", i, src.Name)
		}
		seen[src.Name] = true
		switch src.Driver {
		case "file_tail":
			if src.Path == "" {
				return fmt.Errorf("events.sources[%d] (%s): file_tail driver requires path", i, src.Name)
			}
		case "docker_logs":
			if src.Container == "" {
				return fmt.Errorf("events.sources[%d] (%s): docker_logs driver requires container", i, src.Name)
			}
		default:
			return fmt.Errorf("events.sources[%d] (%s): unknown driver %q (want file_tail or docker_logs)", i, src.Name, src.Driver)
		}
		switch src.Format {
		case "json", "regex", "plain":
		default:
			return fmt.Errorf("events.sources[%d] (%s): unknown format %q (want json, regex or plain)", i, src.Name, src.Format)
		}
		if src.Format == "regex" {
			if src.Pattern == "" {
				return fmt.Errorf("events.sources[%d] (%s): format=regex requires pattern", i, src.Name)
			}
			if _, err := regexp.Compile(src.Pattern); err != nil {
				return fmt.Errorf("events.sources[%d] (%s): pattern: %w", i, src.Name, err)
			}
		}
		if src.Mapping.Kind == "" {
			return fmt.Errorf("events.sources[%d] (%s): mapping.kind is required", i, src.Name)
		}
		for j, m := range src.Match {
			if m.Field == "" {
				return fmt.Errorf("events.sources[%d] (%s): match[%d].field is required", i, src.Name, j)
			}
			if (m.Equals == "") == (m.Contains == "") {
				return fmt.Errorf("events.sources[%d] (%s): match[%d] requires exactly one of equals or contains", i, src.Name, j)
			}
		}
	}
	return nil
}

// validateAuth enforces the cross-field rules on a target's optional
// auth block. See AuthConfig for the full contract.
func validateAuth(i int, t TargetConfig) error {
	if t.Auth == nil {
		return nil
	}
	a := t.Auth
	if a.BearerToken != "" && a.Basic != nil {
		return fmt.Errorf("targets[%d] (%s): auth bearer_token and basic are mutually exclusive", i, t.Name)
	}
	if a.Basic != nil {
		if a.Basic.Username == "" {
			return fmt.Errorf("targets[%d] (%s): auth.basic.username is required", i, t.Name)
		}
		if a.Basic.Password == "" {
			return fmt.Errorf("targets[%d] (%s): auth.basic.password is required", i, t.Name)
		}
	}
	if (a.BearerToken != "" || a.Basic != nil) && a.Headers != nil {
		for k := range a.Headers {
			if strings.EqualFold(k, "Authorization") {
				return fmt.Errorf("targets[%d] (%s): auth.headers Authorization conflicts with bearer_token/basic", i, t.Name)
			}
		}
	}
	return nil
}

// validateTLS enforces the rules on a target's optional TLS block:
// only valid on https:// URLs; ca_file must point to a readable PEM
// that contains at least one certificate.
func validateTLS(i int, t TargetConfig) error {
	if t.TLS == nil {
		return nil
	}
	if !strings.HasPrefix(t.URL, "https://") {
		return fmt.Errorf("targets[%d] (%s): tls block not allowed on http URL", i, t.Name)
	}
	if t.TLS.CAFile != "" {
		data, err := os.ReadFile(t.TLS.CAFile)
		if err != nil {
			return fmt.Errorf("targets[%d] (%s): tls.ca_file: %w", i, t.Name, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return fmt.Errorf("targets[%d] (%s): tls.ca_file %q: no certificates parsed", i, t.Name, t.TLS.CAFile)
		}
	}
	return nil
}
