// Command owl-docs renders the static documentation site for owl.
//
// It reads Markdown content from a source directory (default
// docs/site/content), processes a small set of partials that embed
// code-derived reference material, and writes static HTML to an
// output directory (default docs/site/dist).
//
// Invocation:
//
//	owl-docs --in docs/site/content --out docs/site/dist
//	owl-docs --check --in docs/site/content
//	owl-docs --base-url /owl/  (for GitHub Pages subdirectory hosting)
//
// In --check mode the binary validates content (config examples,
// fixture references, internal links, metrics-registry coverage)
// without writing any output, and exits non-zero on failure.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/neverbot/owl/internal/design"
)

//go:embed static/docs.css
var docsCSS []byte

//go:embed static/search.js
var searchJS []byte

// baseURL is the URL prefix every site-internal absolute path lives
// under. It is empty by default (sites rooted at "/") and is set to
// values like "/owl" when publishing under a GitHub Pages
// subdirectory. The variable is read by templates and partials via
// helper functions so a fresh build with a different prefix is just
// a flag away.
var baseURL = ""

// withBase prepends baseURL to a site-internal absolute path. The
// input must start with "/"; the output is the same path with the
// prefix stitched in, with no double-slash.
func withBase(p string) string {
	if !strings.HasPrefix(p, "/") {
		return p
	}
	if baseURL == "" {
		return p
	}
	return baseURL + p
}

func main() {
	in := flag.String("in", "docs/site/content", "content source directory")
	out := flag.String("out", "docs/site/dist", "output directory (ignored in --check)")
	check := flag.Bool("check", false, "validate without writing output")
	base := flag.String("base-url", "", "URL prefix (e.g. /owl) for hosting under a subdirectory; empty for root")
	flag.Parse()

	baseURL = normaliseBaseURL(*base)

	if err := run(*in, *out, *check); err != nil {
		slog.Error("owl-docs failed", "err", err)
		os.Exit(1)
	}
}

// normaliseBaseURL trims trailing slashes from a base-url flag so
// withBase can always concatenate "baseURL + /path" without worrying
// about doubles. An empty input stays empty.
func normaliseBaseURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.TrimRight(s, "/")
}

// run is the testable entrypoint. It either validates content
// (check=true) or renders the site to outDir.
func run(inDir, outDir string, check bool) error {
	if check {
		return runChecks(inDir)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir out: %w", err)
	}
	staticDir := filepath.Join(outDir, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		return fmt.Errorf("mkdir static: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "owl.css"), design.TokensCSS(), 0o644); err != nil {
		return fmt.Errorf("write tokens.css: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "app.js"), design.ChartJS(), 0o644); err != nil {
		return fmt.Errorf("write chart.js: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "docs.css"), docsCSS, 0o644); err != nil {
		return fmt.Errorf("write docs.css: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "search.js"), searchJS, 0o644); err != nil {
		return fmt.Errorf("write search.js: %w", err)
	}
	// Favicons live at the site root (not under /static/) so the
	// shapes browsers look up by convention (/favicon.svg,
	// /apple-touch-icon.png, /favicon-16.png, /favicon-32.png) work
	// even when no <link rel="icon"> tag is present, and the rest of
	// the asset paths stay short.
	for name, payload := range map[string][]byte{
		"favicon.svg":          design.FaviconSVG(),
		"favicon-16.png":       design.Favicon16(),
		"favicon-32.png":       design.Favicon32(),
		"apple-touch-icon.png": design.Favicon180(),
	} {
		if err := os.WriteFile(filepath.Join(outDir, name), payload, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := copyAssetsIP(outDir); err != nil {
		return fmt.Errorf("copy assets/ip: %w", err)
	}
	if err := copyScreenshots(outDir); err != nil {
		return fmt.Errorf("copy screenshots: %w", err)
	}
	if err := renderAll(inDir, outDir); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}

// copyAssetsIP mirrors assets/ip/*.svg into dist/assets/ so pages can
// reference brand variants of the owl mark (light, dark, accent) as
// /assets/owl-mark-*.svg. PNGs and the readme are skipped — the
// docs site only needs the SVG variants. Missing source directory is
// not an error.
func copyAssetsIP(outDir string) error {
	const src = "assets/ip"
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	dst := filepath.Join(outDir, "assets")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// copyScreenshots mirrors docs/screenshots/* into dist/screenshots/
// so pages can reference them as /screenshots/<name>.png (which then
// gets the base-URL prefix at render time). Missing source directory
// is not an error — the readme uses them, but docs pages may not.
func copyScreenshots(outDir string) error {
	const src = "docs/screenshots"
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	dst := filepath.Join(outDir, "screenshots")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
