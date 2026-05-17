package main

import (
	"strings"
	"testing"
)

func TestMetricsTablePartialIncludesProcessFamily(t *testing.T) {
	out, err := metricsTablePartial(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"`owl_goroutines`",
		"`owl_storage_samples_total`",
		"`owl_alerts_evaluations_total`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics-table missing %q", want)
		}
	}
}
