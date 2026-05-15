// Package expfmt parses the Prometheus text-based exposition format.
// Only the parts Owl needs are supported: untyped, counter, gauge.
// HELP and TYPE lines are recognised but not retained — we want sample
// rows, not metadata.
package expfmt

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Sample is a single (metric, labels, value, optional timestamp) row.
type Sample struct {
	Metric    string
	Labels    map[string]string
	Value     float64
	Timestamp int64 // ms; 0 means "not provided"
}

// Parse reads an entire exposition body and returns one Sample per line
// that carries a value. Comment lines and blank lines are ignored.
func Parse(r io.Reader) ([]Sample, error) {
	br := bufio.NewReader(r)
	out := make([]Sample, 0, 64)
	for lineNo := 1; ; lineNo++ {
		line, err := br.ReadString('\n')
		if line != "" {
			s, parseErr := parseLine(strings.TrimRight(line, "\r\n"))
			if parseErr != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, parseErr)
			}
			if s != nil {
				out = append(out, *s)
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read line %d: %w", lineNo, err)
		}
	}
}

func parseLine(line string) (*Sample, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}
	if line[0] == '#' {
		// Discards HELP / TYPE / any other comment.
		return nil, nil
	}

	// Find the metric name: up to '{' or whitespace.
	end := len(line)
	for i, r := range line {
		if r == '{' || r == ' ' || r == '\t' {
			end = i
			break
		}
	}
	metric := line[:end]
	if metric == "" {
		return nil, fmt.Errorf("missing metric name in %q", line)
	}

	rest := line[end:]
	labels := map[string]string{}

	if strings.HasPrefix(rest, "{") {
		// Parse labels up to the closing '}'.
		closing := indexClosingBrace(rest)
		if closing < 0 {
			return nil, fmt.Errorf("unterminated labels in %q", line)
		}
		if err := parseLabels(rest[1:closing], labels); err != nil {
			return nil, fmt.Errorf("labels: %w", err)
		}
		rest = rest[closing+1:]
	}

	// rest now starts with whitespace + value [+ timestamp].
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return nil, fmt.Errorf("missing value in %q", line)
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid value %q: %w", fields[0], err)
	}
	var ts int64
	if len(fields) >= 2 {
		ts, err = strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp %q: %w", fields[1], err)
		}
	}

	return &Sample{
		Metric:    metric,
		Labels:    labels,
		Value:     val,
		Timestamp: ts,
	}, nil
}

// indexClosingBrace returns the index of the '}' that closes the labels
// block, respecting quoted strings (which may contain '}' literally).
func indexClosingBrace(s string) int {
	inQuote := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == '}' && !inQuote {
			return i
		}
	}
	return -1
}

// parseLabels accepts the content between { and } and populates the
// provided map. Format: k="v",k2="v2"  with backslash-escaped quotes
// and commas inside quoted strings allowed.
func parseLabels(s string, out map[string]string) error {
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			return fmt.Errorf("missing = in %q", s)
		}
		key := strings.TrimSpace(s[:eq])
		rest := strings.TrimSpace(s[eq+1:])
		if rest == "" || rest[0] != '"' {
			return fmt.Errorf("expected quoted value for key %q", key)
		}
		// Walk the quoted value, honoring backslash escapes.
		i := 1
		var b strings.Builder
		for i < len(rest) {
			c := rest[i]
			if c == '\\' && i+1 < len(rest) {
				b.WriteByte(rest[i+1])
				i += 2
				continue
			}
			if c == '"' {
				break
			}
			b.WriteByte(c)
			i++
		}
		if i >= len(rest) {
			return fmt.Errorf("unterminated quoted value for key %q", key)
		}
		out[key] = b.String()
		// Skip past the closing quote.
		rest = rest[i+1:]
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return nil
		}
		if rest[0] != ',' {
			return fmt.Errorf("expected comma after value for key %q", key)
		}
		s = rest[1:]
	}
}
