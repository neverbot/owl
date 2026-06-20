package events

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Parse decodes a single record into a flat map suitable for filter
// and mapper consumption. format must be one of "json", "regex",
// "plain"; pattern is required when format=="regex" and ignored
// otherwise.
func Parse(line []byte, format string, pattern *regexp.Regexp) (map[string]any, error) {
	switch format {
	case "json":
		var out map[string]any
		if err := json.Unmarshal(line, &out); err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		return out, nil
	case "regex":
		if pattern == nil {
			return nil, fmt.Errorf("parse regex: nil pattern")
		}
		m := pattern.FindSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("parse regex: no match")
		}
		out := make(map[string]any, len(pattern.SubexpNames()))
		for i, name := range pattern.SubexpNames() {
			if i == 0 || name == "" {
				continue
			}
			out[name] = string(m[i])
		}
		return out, nil
	case "plain":
		return map[string]any{"line": string(line)}, nil
	default:
		return nil, fmt.Errorf("parse: unknown format %q", format)
	}
}
