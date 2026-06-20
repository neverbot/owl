package main

import (
	"bytes"
	"testing"
)

func TestFixturesAreDeterministic(t *testing.T) {
	for _, name := range FixtureNames() {
		a, _ := LookupFixture(name)
		b, _ := LookupFixture(name)
		ab, _ := MarshalFixture(a)
		bb, _ := MarshalFixture(b)
		if !bytes.Equal(ab, bb) {
			t.Errorf("fixture %q is non-deterministic", name)
		}
	}
}

func TestFixturesAreNonEmpty(t *testing.T) {
	for _, name := range FixtureNames() {
		f, _ := LookupFixture(name)
		if len(f.Series) == 0 && len(f.Events) == 0 {
			t.Errorf("fixture %q has neither series nor events", name)
		}
		for _, s := range f.Series {
			if len(s.Points) == 0 {
				t.Errorf("fixture %q series %q has no points", name, s.Metric)
			}
		}
	}
}
