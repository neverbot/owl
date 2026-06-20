package events

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Parse decodes a single record into a flat map suitable for filter
// and mapper consumption. format must be one of "json", "logfmt",
// "regex", "plain"; pattern is required when format=="regex" and
// ignored otherwise.
func Parse(line []byte, format string, pattern *regexp.Regexp) (map[string]any, error) {
	switch format {
	case "json":
		var out map[string]any
		if err := json.Unmarshal(line, &out); err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		return out, nil
	case "logfmt":
		return parseLogfmt(line), nil
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

// parseLogfmt decodes a single logfmt line into a string-valued map.
// The grammar accepted is the de-facto one used by go-logfmt and the
// Go ecosystem broadly: space-separated key=value pairs, where value
// is either a bare token (terminated by whitespace) or a
// double-quoted string with `\n`, `\t`, `\r`, `\\`, and `\"` escapes.
// Bare keys (no `=`) and bare values containing `=` are tolerated:
// the former map to "", the latter is split on the first `=`. Type
// inference is not done — every value is a string, matching the
// shape Parse returns for the regex and plain formats.
func parseLogfmt(line []byte) map[string]any {
	out := make(map[string]any)
	s := string(line)
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		keyStart := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		if i == keyStart {
			i++
			continue
		}
		key := s[keyStart:i]
		if i >= len(s) || s[i] != '=' {
			out[key] = ""
			continue
		}
		i++ // skip '='
		if i < len(s) && s[i] == '"' {
			i++
			var b strings.Builder
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					switch s[i+1] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					case '\\', '"':
						b.WriteByte(s[i+1])
					default:
						b.WriteByte(s[i])
						b.WriteByte(s[i+1])
					}
					i += 2
					continue
				}
				b.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++ // closing quote
			}
			out[key] = b.String()
		} else {
			valStart := i
			for i < len(s) && s[i] != ' ' && s[i] != '\t' {
				i++
			}
			out[key] = s[valStart:i]
		}
	}
	return out
}
