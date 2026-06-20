package main

import "fmt"

func init() {
	registerPartial("events", eventsPartial)
}

// eventsPartial expands to a runtime-shaped events panel: an
// <article class="panel"> with the .panel__events table
// chart.js's refreshEvents fills in. data-static points at the
// fixture JSON (an {"events": [...]} envelope) so the panel renders
// without an owl backend, and data-refresh="0" disables the polling
// timer. data-event-targets must be present (even empty) so the
// chart.js dispatch logic recognises this as an events panel; its
// contents don't matter when data-static is set because the static
// path short-circuits the query construction.
//
// Required args: fixture (registered name in fixtures.go).
// Optional args: title.
func eventsPartial(args map[string]string) (string, error) {
	name := args["fixture"]
	if name == "" {
		return "", fmt.Errorf("events: missing fixture=…")
	}
	if _, ok := LookupFixture(name); !ok {
		return "", fmt.Errorf("events: unknown fixture %q", name)
	}
	referenceFixture(name)
	title := args["title"]
	// The partial emits a single-line HTML blob on purpose: indented
	// multi-line output gets misread as a markdown code block when
	// the line preceding any child is blank.
	return fmt.Sprintf(
		`<article class="panel" data-static=%q data-event-targets='[]' data-refresh="0"><header class="panel__header"><h2 class="panel__title">%s</h2></header><div class="panel__events"><table><thead><tr><th>time</th><th>source / kind</th><th>event</th></tr></thead><tbody></tbody></table></div></article>`,
		withBase("/data/"+name+".json"), title), nil
}
