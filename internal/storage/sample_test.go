package storage

import "testing"

func TestCanonicalLabelsIsDeterministic(t *testing.T) {
	a := CanonicalLabels(map[string]string{"job": "host", "instance": "vps1"})
	b := CanonicalLabels(map[string]string{"instance": "vps1", "job": "host"})
	if a != b {
		t.Fatalf("CanonicalLabels not deterministic:\n  a=%q\n  b=%q", a, b)
	}
	if a == "" {
		t.Fatalf("CanonicalLabels returned empty string")
	}
}

func TestCanonicalLabelsEmpty(t *testing.T) {
	if got := CanonicalLabels(nil); got != "" {
		t.Errorf("CanonicalLabels(nil) = %q, want empty", got)
	}
	if got := CanonicalLabels(map[string]string{}); got != "" {
		t.Errorf("CanonicalLabels({}) = %q, want empty", got)
	}
}
