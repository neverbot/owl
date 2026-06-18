package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChartPartialEmitsPanel(t *testing.T) {
	resetReferencedFixtures()
	out, err := chartPartial(map[string]string{
		"fixture": "rate-typical",
		"expr":    "rate(http_requests_total[1m])",
		"unit":    "ops",
		"legend":  "{{job}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		`data-static="/data/rate-typical.json"`,
		`data-refresh="0"`,
		`data-unit="ops"`,
		`data-queries='[{"expr":"rate(http_requests_total[1m])","legend":"{{job}}"}]'`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "data-expr=") {
		t.Errorf("data-expr should not appear; got:\n%s", out)
	}
}

func TestChartPartialUnknownFixture(t *testing.T) {
	_, err := chartPartial(map[string]string{"fixture": "no-such"})
	if err == nil {
		t.Fatal("expected error for unknown fixture")
	}
}

func TestChartPartialRequiresFixture(t *testing.T) {
	_, err := chartPartial(map[string]string{})
	if err == nil {
		t.Fatal("expected error when fixture arg missing")
	}
}

func TestRenderAllMaterialisesReferencedFixtures(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writeContent(t, in, "index.md",
		"---\ntitle: Home\n---\n{{> chart fixture=rate-typical expr=\"rate\" unit=ops}}\n")
	if err := renderAll(in, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "data/rate-typical.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"series"`) {
		t.Errorf("rate-typical.json doesn't look like a fixture: %s", b)
	}
}
