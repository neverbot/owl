// Package scrape pulls Prometheus-format metrics from HTTP endpoints.
package scrape

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/neverbot/owl/internal/scrape/expfmt"
	"github.com/neverbot/owl/internal/storage"
)

// Target is one scrape target as Owl sees it at runtime. It is a
// flattened view of TargetConfig plus discovery output.
type Target struct {
	Name     string            // unique stable id
	URL      string            // full http URL of the /metrics endpoint
	Interval time.Duration     // ignored by ScrapeOnce, used by Manager
	Timeout  time.Duration     // per-request timeout
	Labels   map[string]string // attached to every sample
}

// ScrapeOnce performs one GET against tgt.URL with tgt.Timeout, parses
// the response as Prometheus exposition format, and writes the resulting
// samples to app. Returns the number of samples appended and any error
// encountered. Each sample is enriched with the target's labels plus
// instance=<host:port>, and timestamped at scrape time if the exposition
// did not provide one.
func ScrapeOnce(ctx context.Context, tgt Target, app storage.Appender) (int, error) {
	timeout := tgt.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, tgt.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4")
	req.Header.Set("User-Agent", "owl-scraper/0.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d from %s", resp.StatusCode, tgt.URL)
	}

	parsed, err := expfmt.Parse(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", tgt.URL, err)
	}

	now := time.Now().UnixMilli()
	instance := hostPort(tgt.URL)

	batch := make([]storage.Sample, 0, len(parsed))
	for _, p := range parsed {
		labels := mergeLabels(tgt.Labels, p.Labels, instance)
		ts := p.Timestamp
		if ts == 0 {
			ts = now
		}
		batch = append(batch, storage.Sample{
			Metric: p.Metric,
			Labels: labels,
			TS:     ts,
			Value:  p.Value,
		})
	}
	if len(batch) == 0 {
		return 0, nil
	}
	if err := app.Append(batch); err != nil {
		return 0, err
	}
	return len(batch), nil
}

// mergeLabels merges per-target labels with per-sample labels and adds
// instance. Per-sample labels win over per-target on conflict.
func mergeLabels(targetLabels, sampleLabels map[string]string, instance string) map[string]string {
	out := make(map[string]string, len(targetLabels)+len(sampleLabels)+1)
	for k, v := range targetLabels {
		out[k] = v
	}
	for k, v := range sampleLabels {
		out[k] = v
	}
	if instance != "" {
		if _, set := out["instance"]; !set {
			out["instance"] = instance
		}
	}
	return out
}

// hostPort returns "host:port" extracted from a URL, or empty on parse
// failure.
func hostPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}
