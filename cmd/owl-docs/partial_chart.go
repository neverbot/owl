package main

import (
	"fmt"
	"sync"
)

func init() {
	registerPartial("chart", chartPartial)
}

// referencedFixtures collects every fixture name expanded across the
// whole build. renderAll materialises only what's referenced.
var (
	referencedMu       sync.Mutex
	referencedFixtures = map[string]struct{}{}
)

// resetReferencedFixtures clears the set of referenced fixtures.
// renderAll calls this at the top of every build so successive runs
// (tests, watch mode) don't leak references across invocations.
func resetReferencedFixtures() {
	referencedMu.Lock()
	referencedFixtures = map[string]struct{}{}
	referencedMu.Unlock()
}

// referenceFixture records that name was seen during partial
// expansion so renderAll can materialise it later.
func referenceFixture(name string) {
	referencedMu.Lock()
	referencedFixtures[name] = struct{}{}
	referencedMu.Unlock()
}

// referencedFixtureList returns every recorded fixture name.
// Order is unspecified; callers that need stability should sort.
func referencedFixtureList() []string {
	referencedMu.Lock()
	defer referencedMu.Unlock()
	out := make([]string, 0, len(referencedFixtures))
	for k := range referencedFixtures {
		out = append(out, k)
	}
	return out
}

// chartPartial expands to a runtime panel div pointing at the fixture
// JSON file. Required args: fixture (name). Optional: expr (decorative
// label of the query), unit (display unit), title.
func chartPartial(args map[string]string) (string, error) {
	name := args["fixture"]
	if name == "" {
		return "", fmt.Errorf("chart: missing fixture=…")
	}
	if _, ok := LookupFixture(name); !ok {
		return "", fmt.Errorf("chart: unknown fixture %q", name)
	}
	referenceFixture(name)
	title := args["title"]
	if title == "" {
		title = args["expr"]
	}
	unit := args["unit"]
	return fmt.Sprintf(
		`<div class="panel" data-static="/data/%s.json" data-expr=%q data-unit=%q data-refresh="0">
  <div class="panel__header">
    <span class="panel__title">%s</span>
    <span class="panel__unit">%s</span>
  </div>
  <div class="panel__chart"></div>
  <div class="panel__footer">
    <span class="panel__value">—</span>
  </div>
</div>`,
		name, args["expr"], unit, title, unit), nil
}
