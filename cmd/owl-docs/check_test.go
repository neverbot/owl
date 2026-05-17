package main

import (
	"strings"
	"testing"

	"github.com/neverbot/owl/internal/web"
)

func TestCheckFlagsMissingMetricsMd(t *testing.T) {
	in := t.TempDir()
	writeContent(t, in, "index.md", "---\ntitle: Home\n---\nHi\n")
	if err := runChecks(in); err == nil {
		t.Fatal("expected failure when metrics.md is absent")
	}
}

func TestCheckFlagsEmptyMetricsMd(t *testing.T) {
	in := t.TempDir()
	writeContent(t, in, "index.md", "---\ntitle: Home\n---\nHi\n")
	writeContent(t, in, "metrics.md", "---\ntitle: Metrics\n---\nplaceholder\n")
	err := runChecks(in)
	if err == nil {
		t.Fatal("expected metrics-coverage failure for empty metrics.md")
	}
	if !strings.Contains(err.Error(), "does not mention metric") {
		t.Fatalf("want coverage failure, got %v", err)
	}
}

func TestCheckBadConfigExample(t *testing.T) {
	in := t.TempDir()
	writeContent(t, in, "index.md", "---\ntitle: Home\n---\n"+
		"```yaml config-example\n: not valid yaml :\n```\n")
	// metrics.md present so we isolate the config-example failure.
	writeContent(t, in, "metrics.md", metricsAllPage(t))
	err := runChecks(in)
	if err == nil || !strings.Contains(err.Error(), "config-example") {
		t.Fatalf("want config-example failure, got %v", err)
	}
}

func TestCheckBrokenInternalLink(t *testing.T) {
	in := t.TempDir()
	writeContent(t, in, "index.md", "---\ntitle: Home\n---\n[bad](/nowhere/)\n")
	writeContent(t, in, "metrics.md", metricsAllPage(t))
	err := runChecks(in)
	if err == nil || !strings.Contains(err.Error(), "broken internal link") {
		t.Fatalf("want broken link failure, got %v", err)
	}
}

// metricsAllPage builds a metrics.md body that mentions every
// registered metric, so coverage passes and other validators can be
// exercised in isolation.
func metricsAllPage(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\ntitle: Metrics\n---\n")
	// Lazy import: defer to runtime via the actual registry through a
	// thin shim avoids a cyclic test dep — we just reference names in
	// runChecks's view. We need the actual names here.
	for _, d := range web.Registry() {
		b.WriteString(d.Name)
		b.WriteString("\n")
	}
	return b.String()
}
