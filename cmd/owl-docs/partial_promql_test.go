package main

import (
	"strings"
	"testing"
)

func TestPromqlCapabilitiesPartialIncludesRate(t *testing.T) {
	out, err := promqlCapabilitiesPartial(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"`rate`", "`sum`"} {
		if !strings.Contains(out, want) {
			t.Errorf("promql-capabilities missing %q in:\n%s", want, out)
		}
	}
}
