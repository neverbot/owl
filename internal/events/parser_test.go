package events

import (
	"reflect"
	"regexp"
	"testing"
)

// TestParse covers the three formats and one malformed-input case.
func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		format  string
		pattern *regexp.Regexp
		want    map[string]any
		wantErr bool
	}{
		{
			name:   "json object",
			line:   `{"a":1,"b":"x"}`,
			format: "json",
			want:   map[string]any{"a": float64(1), "b": "x"},
		},
		{
			name:    "json invalid",
			line:    `{`,
			format:  "json",
			wantErr: true,
		},
		{
			name:    "regex named groups",
			line:    "1.2.3.4 GET /",
			format:  "regex",
			pattern: regexp.MustCompile(`^(?P<ip>\S+) (?P<m>\S+) (?P<path>\S+)$`),
			want:    map[string]any{"ip": "1.2.3.4", "m": "GET", "path": "/"},
		},
		{
			name:    "regex no match",
			line:    "nope",
			format:  "regex",
			pattern: regexp.MustCompile(`^(?P<ip>\d+)$`),
			wantErr: true,
		},
		{
			name:   "plain",
			line:   "hello world",
			format: "plain",
			want:   map[string]any{"line": "hello world"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.line), tt.format, tt.pattern)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v want %#v", got, tt.want)
			}
		})
	}
}
