package events

import (
	"testing"
	"time"

	"github.com/neverbot/owl/internal/config"
)

// TestMap covers JSONPath extraction, ts default, missing fields.
func TestMap(t *testing.T) {
	parsed := map[string]any{
		"container": "nginx",
		"old":       "1.0",
		"new":       "1.1",
		"ts":        "1700000000000",
	}
	m := config.MappingConfig{
		TS:   "$.ts",
		Kind: "container-updated",
		Payload: map[string]string{
			"container": "$.container",
			"from":      "$.old",
			"to":        "$.new",
		},
	}
	ev, err := Map(parsed, m, "src", func() time.Time { return time.UnixMilli(99) })
	if err != nil {
		t.Fatal(err)
	}
	if ev.Source != "src" || ev.Kind != "container-updated" || ev.TS != 1700000000000 {
		t.Fatalf("ev=%#v", ev)
	}
	if ev.Payload["container"] != "nginx" || ev.Payload["from"] != "1.0" {
		t.Fatalf("payload=%#v", ev.Payload)
	}
}

// TestMapDefaultsTimestamp asserts ts falls back to now when the
// configured path is absent.
func TestMapDefaultsTimestamp(t *testing.T) {
	ev, err := Map(map[string]any{}, config.MappingConfig{Kind: "k"}, "s", func() time.Time { return time.UnixMilli(42) })
	if err != nil {
		t.Fatal(err)
	}
	if ev.TS != 42 {
		t.Fatalf("ts=%d", ev.TS)
	}
}

// TestMapMissingPayload leaves the entry as empty string when the
// configured path is absent — mirrors how render() handles missing
// keys.
func TestMapMissingPayload(t *testing.T) {
	ev, err := Map(map[string]any{"a": "1"}, config.MappingConfig{
		Kind:    "k",
		Payload: map[string]string{"a": "$.a", "missing": "$.x"},
	}, "s", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Payload["missing"] != "" {
		t.Fatalf("missing=%v", ev.Payload["missing"])
	}
}
