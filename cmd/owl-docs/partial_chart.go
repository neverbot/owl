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

// chartPartial expands to a runtime-shaped panel that mirrors what
// the dashboard template emits in /d/<id>: an <article class="panel">
// with an <svg class="panel__chart"> placeholder the chart engine
// fills in. The data-static attribute tells chart.js to fetch the
// fixture JSON beside the page instead of /api/query, and
// data-refresh="0" disables the polling timer (fixtures are static).
//
// Required args: fixture (registered name in fixtures.go).
// Optional args: expr (decorative query label), unit, title.
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
	unitMarkup := ""
	if unit != "" {
		unitMarkup = fmt.Sprintf(`<span class="panel__unit">%s</span>`, unit)
	}
	return fmt.Sprintf(
		`<article class="panel" data-static="/data/%s.json" data-expr=%q data-unit=%q data-refresh="0">
  <header class="panel__header">
    <h2 class="panel__title">%s</h2>
    %s
  </header>
  <svg class="panel__chart" aria-label="%s chart"></svg>
  <footer class="panel__footer">
    <span class="panel__value panel__value--placeholder">—</span>
    <div class="panel__legend"></div>
  </footer>
</article>`,
		name, args["expr"], unit, title, unitMarkup, title), nil
}
