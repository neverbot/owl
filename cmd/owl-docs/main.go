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

	"github.com/neverbot/owl/internal/design"
)

//go:embed static/docs.css
var docsCSS []byte

//go:embed static/search.js
var searchJS []byte

func main() {
	in := flag.String("in", "docs/site/content", "content source directory")
	out := flag.String("out", "docs/site/dist", "output directory (ignored in --check)")
	check := flag.Bool("check", false, "validate without writing output")
	flag.Parse()

	if err := run(*in, *out, *check); err != nil {
		slog.Error("owl-docs failed", "err", err)
		os.Exit(1)
	}
}

// run is the testable entrypoint. It either validates content
// (check=true) or renders the site to outDir.
func run(inDir, outDir string, check bool) error {
	if check {
		// Task 16 fills this in.
		return nil
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
	if err := renderAll(inDir, outDir); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	return nil
}
