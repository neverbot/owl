package expfmt

import (
	"strings"
	"testing"
)

const promBody = `# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 17
# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="get",code="200"} 1027
http_requests_total{method="post",code="200"} 3
http_requests_total{method="get",code="500"} 5 1395066363000
# A blank comment
no_labels_no_help 42.5
weird{a="x,y",b="quoted \"inner\""} 1
`

func TestParseExtractsAllSamples(t *testing.T) {
	samples, err := Parse(strings.NewReader(promBody))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(samples) < 5 {
		t.Fatalf("expected at least 5 samples, got %d: %+v", len(samples), samples)
	}

	byMetric := map[string]int{}
	for _, s := range samples {
		byMetric[s.Metric]++
	}
	if byMetric["go_goroutines"] != 1 {
		t.Errorf("go_goroutines count = %d", byMetric["go_goroutines"])
	}
	if byMetric["http_requests_total"] != 3 {
		t.Errorf("http_requests_total count = %d", byMetric["http_requests_total"])
	}
	if byMetric["no_labels_no_help"] != 1 {
		t.Errorf("no_labels_no_help count = %d", byMetric["no_labels_no_help"])
	}

	for _, s := range samples {
		if s.Metric == "go_goroutines" && s.Value != 17 {
			t.Errorf("go_goroutines value = %v", s.Value)
		}
		if s.Metric == "weird" {
			if s.Labels["a"] != "x,y" {
				t.Errorf("weird.a = %q", s.Labels["a"])
			}
			if s.Labels["b"] != `quoted "inner"` {
				t.Errorf("weird.b = %q", s.Labels["b"])
			}
		}
	}
}

func TestParseSkipsCommentsAndBlanks(t *testing.T) {
	samples, err := Parse(strings.NewReader(`
# comment

metric_a 1
# HELP metric_b "blah"
metric_b 2
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(samples) != 2 {
		t.Errorf("len(samples) = %d, want 2", len(samples))
	}
}

func TestParseRejectsMalformedLine(t *testing.T) {
	_, err := Parse(strings.NewReader("malformed line with no value"))
	if err == nil {
		t.Error("expected error for malformed line, got nil")
	}
}
