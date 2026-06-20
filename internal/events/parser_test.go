package events

import (
	"reflect"
	"regexp"
	"testing"
)

// TestParse covers the four formats and one malformed-input case.
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
		{
			name:   "logfmt bare",
			line:   `level=info container=nginx count=42`,
			format: "logfmt",
			want:   map[string]any{"level": "info", "container": "nginx", "count": "42"},
		},
		{
			name:   "logfmt quoted with spaces",
			line:   `level=info msg="Found new image" container=nginx`,
			format: "logfmt",
			want:   map[string]any{"level": "info", "msg": "Found new image", "container": "nginx"},
		},
		{
			name:   "logfmt escapes",
			line:   `msg="say \"hi\"\nbye"`,
			format: "logfmt",
			want:   map[string]any{"msg": "say \"hi\"\nbye"},
		},
		{
			name:   "logfmt watchtower-style",
			line:   `time="2026-06-20T10:30:00Z" level=info msg="Found new image" container=nginx old_image=nginx:1.2.3 new_image=nginx:1.2.4`,
			format: "logfmt",
			want: map[string]any{
				"time":      "2026-06-20T10:30:00Z",
				"level":     "info",
				"msg":       "Found new image",
				"container": "nginx",
				"old_image": "nginx:1.2.3",
				"new_image": "nginx:1.2.4",
			},
		},
		{
			name:   "logfmt empty value",
			line:   `a= b=2`,
			format: "logfmt",
			want:   map[string]any{"a": "", "b": "2"},
		},
		{
			name:   "logfmt bare key",
			line:   `flag verbose=true`,
			format: "logfmt",
			want:   map[string]any{"flag": "", "verbose": "true"},
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
