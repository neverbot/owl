package main

import "testing"

func TestDescribeListen(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"0.0.0.0:9090", "listening on port 9090 (all interfaces)"},
		{":9090", "listening on port 9090 (all interfaces)"},
		{"127.0.0.1:9090", "listening on http://127.0.0.1:9090"},
		{"10.99.0.1:9090", "listening on http://10.99.0.1:9090"},
		{"example.local:8080", "listening on http://example.local:8080"},
		{"not-a-valid-host-port", "listening on not-a-valid-host-port"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := describeListen(tt.addr); got != tt.want {
				t.Errorf("describeListen(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}
