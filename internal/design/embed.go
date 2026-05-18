// Package design holds the cross-surface visual assets used by both the
// owl runtime web layer and the owl-docs static site generator.
//
// Nothing else in the repo imports anything from design's siblings —
// the package is a leaf of the dependency graph, so a change here can
// only ripple outward.
package design

import "embed"

//go:embed tokens.css chart.js owl-mark.svg favicon.svg favicon-16.png favicon-32.png favicon-180.png
var Assets embed.FS

// TokensCSS returns the contents of tokens.css as a byte slice.
// The runtime web layer serves this at /static/owl.css; the docs
// generator writes it to dist/static/owl.css. Same bytes, two
// destinations.
func TokensCSS() []byte { return mustRead("tokens.css") }

// ChartJS returns the contents of chart.js as a byte slice.
// Same shape as TokensCSS — one source of truth, two consumers.
func ChartJS() []byte { return mustRead("chart.js") }

// OwlMarkSVG returns the canonical owl-face SVG mark. It uses
// currentColor so it inherits the surrounding text colour when
// inlined into HTML.
func OwlMarkSVG() []byte { return mustRead("owl-mark.svg") }

// FaviconSVG returns the favicon SVG, which carries an internal
// prefers-color-scheme rule so it adapts to the user's OS theme
// without needing two files.
func FaviconSVG() []byte { return mustRead("favicon.svg") }

// Favicon16 returns the 16×16 PNG fallback for browsers that do not
// render SVG favicons.
func Favicon16() []byte { return mustRead("favicon-16.png") }

// Favicon32 returns the 32×32 PNG fallback (the size most browsers
// pick for tab icons when the SVG path is not available).
func Favicon32() []byte { return mustRead("favicon-32.png") }

// Favicon180 returns the 180×180 PNG used as the apple-touch-icon
// when the site is bookmarked to a home screen on iOS / iPadOS.
func Favicon180() []byte { return mustRead("favicon-180.png") }

// mustRead pulls a file from the embedded FS or panics — every file
// listed in the //go:embed directive above must exist, so a missing
// read is a build problem rather than a runtime error.
func mustRead(name string) []byte {
	b, err := Assets.ReadFile(name)
	if err != nil {
		panic("design: " + name + " missing from embed (build problem)")
	}
	return b
}
