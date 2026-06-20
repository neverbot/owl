package events

import (
	"strconv"
	"strings"
	"time"

	"github.com/neverbot/owl/internal/config"
)

// Map turns a parsed record into an Event using the source's mapping
// configuration. now is the clock used when MappingConfig.TS is
// empty or its target field is missing in parsed.
func Map(parsed map[string]any, m config.MappingConfig, source string, now func() time.Time) (Event, error) {
	ts := now().UnixMilli()
	if m.TS != "" {
		if v, ok := extractPath(parsed, m.TS); ok {
			if parsedMs, ok := toMillis(v); ok {
				ts = parsedMs
			}
		}
	}
	payload := make(map[string]any, len(m.Payload))
	for k, path := range m.Payload {
		if v, ok := extractPath(parsed, path); ok {
			payload[k] = v
		} else {
			payload[k] = ""
		}
	}
	return Event{
		TS:      ts,
		Source:  source,
		Kind:    m.Kind,
		Payload: payload,
	}, nil
}

// extractPath resolves a tiny JSONPath ("$.a.b.c") against parsed.
// Returns (value, true) when found, (nil, false) otherwise.
// Anything more elaborate (filters, array indices, wildcards) is
// explicitly NOT supported — by design.
func extractPath(root map[string]any, path string) (any, bool) {
	if !strings.HasPrefix(path, "$.") {
		return nil, false
	}
	parts := strings.Split(path[2:], ".")
	var cur any = root
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// toMillis turns a JSON-ish value into a unix-milliseconds int64.
// Accepts integers, floats (truncated), numeric strings, and RFC3339
// timestamps. Returns ok=false on anything else.
func toMillis(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n, true
		}
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t.UnixMilli(), true
		}
		return 0, false
	default:
		return 0, false
	}
}
