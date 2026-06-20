package main

import (
	"strings"
	"testing"
)

func TestEventsPartialEmitsPanel(t *testing.T) {
	resetReferencedFixtures()
	out, err := eventsPartial(map[string]string{
		"fixture": "events-watchtower",
		"title":   "Update events",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		`data-static="/data/events-watchtower.json"`,
		`data-event-targets='[]'`,
		`data-refresh="0"`,
		`panel__events`,
		`>Update events<`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}

func TestEventsPartialUnknownFixture(t *testing.T) {
	_, err := eventsPartial(map[string]string{"fixture": "no-such"})
	if err == nil {
		t.Fatal("expected error for unknown fixture")
	}
}

func TestEventsPartialRequiresFixture(t *testing.T) {
	_, err := eventsPartial(map[string]string{})
	if err == nil {
		t.Fatal("expected error when fixture arg missing")
	}
}

func TestChartPartialAnnotationsAttribute(t *testing.T) {
	resetReferencedFixtures()
	out, err := chartPartial(map[string]string{
		"fixture":     "host-cpu",
		"annotations": "events-watchtower",
		"expr":        "node_cpu",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-annotations-static="/data/events-watchtower.json"`) {
		t.Errorf("missing data-annotations-static attr in:\n%s", out)
	}
}

func TestChartPartialUnknownAnnotationsFixture(t *testing.T) {
	_, err := chartPartial(map[string]string{
		"fixture":     "host-cpu",
		"annotations": "no-such",
	})
	if err == nil {
		t.Fatal("expected error for unknown annotations fixture")
	}
}
