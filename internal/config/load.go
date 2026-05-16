package config

import (
	"bytes"
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

// Load reads a YAML file at path, merges it on top of Default(), and
// validates the result. Unknown fields are rejected so typos surface.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	c := Default()
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := Validate(&c); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
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
