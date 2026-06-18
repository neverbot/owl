package main

import "fmt"

func init() {
	registerPartial("stat", statPartial)
}

// statPartial expands to a runtime-shaped stat panel: an
// <article class="panel panel--stat"> with the .panel__stat cell
// chart.js's refreshStat fills in. data-static points at the
// fixture JSON beside the page so the panel renders without an
// owl backend, and data-refresh="0" disables the polling timer.
// A single-entry data-queries array carries the optional legend so
// the runtime and docs paths share the same legend-resolution logic.
//
// Required args: fixture (registered name in fixtures.go).
// Optional args: expr (decorative query label), unit, title, legend,
//
//	calc (reduction operator; default lastNotNull),
//	decimals (digits after the point as a string),
//	sparkline (any non-empty value turns on graphMode=area).
func statPartial(args map[string]string) (string, error) {
	name := args["fixture"]
	if name == "" {
		return "", fmt.Errorf("stat: missing fixture=…")
	}
	if _, ok := LookupFixture(name); !ok {
		return "", fmt.Errorf("stat: unknown fixture %q", name)
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
	calc := args["calc"]
	if calc == "" {
		calc = "lastNotNull"
	}
	decimals := args["decimals"]
	graphMode := ""
	tail := ""
	if args["sparkline"] != "" {
		graphMode = "area"
		// time slot first (reserves vertical space so the headline
		// doesn't jump on hover), then the sparkline SVG.
		tail = `<span class="panel__stat-time"></span><svg class="panel__sparkline" aria-hidden="true"></svg>`
	}
	queries := mustMarshalQueries([]chartQuery{{
		Expr:   args["expr"],
		Legend: args["legend"],
	}})
	// The partial emits a single-line HTML blob on purpose: indented
	// multi-line output gets misread as a markdown code block when
	// the line preceding any child is blank.
	return fmt.Sprintf(
		`<article class="panel panel--stat" data-static=%q data-queries='%s' data-unit=%q data-calc=%q data-decimals=%q data-graph-mode=%q data-refresh="0"><header class="panel__header"><h2 class="panel__title">%s</h2>%s</header><div class="panel__stat"><span class="panel__stat-value panel__stat-value--placeholder">—</span>%s</div></article>`,
		withBase("/data/"+name+".json"), queries, unit, calc, decimals, graphMode,
		title, unitMarkup, tail), nil
}
