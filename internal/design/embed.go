// Package design holds the cross-surface visual assets used by both the
// owl runtime web layer and the owl-docs static site generator.
//
// Nothing else in the repo imports anything from design's siblings —
// the package is a leaf of the dependency graph, so a change here can
// only ripple outward.
package design

import "embed"

//go:embed tokens.css chart.js
var Assets embed.FS

// TokensCSS returns the contents of tokens.css as a byte slice.
// The runtime web layer serves this at /static/owl.css; the docs
// generator writes it to dist/static/owl.css. Same bytes, two
// destinations.
func TokensCSS() []byte {
	b, err := Assets.ReadFile("tokens.css")
	if err != nil {
		panic("design: tokens.css missing from embed (build problem)")
	}
	return b
}

// ChartJS returns the contents of chart.js as a byte slice.
// Same shape as TokensCSS — one source of truth, two consumers.
func ChartJS() []byte {
	b, err := Assets.ReadFile("chart.js")
	if err != nil {
		panic("design: chart.js missing from embed (build problem)")
	}
	return b
}
