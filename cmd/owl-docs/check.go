package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/neverbot/owl/internal/config"
	"github.com/neverbot/owl/internal/web"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// runChecks loads all pages, expands partials, and runs every
// validator. It returns a joined error containing every problem found,
// or nil when all validators pass.
func runChecks(inDir string) error {
	pages, err := loadPages(inDir)
	if err != nil {
		return err
	}
	var problems []string
	urls := map[string]bool{}
	for _, p := range pages {
		urls[p.URL] = true
	}
	for _, p := range pages {
		expanded, err := expandPartials(p.Body)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: partial: %v", p.SourcePath, err))
			continue
		}
		problems = append(problems, checkConfigExamples(p, expanded)...)
		problems = append(problems, checkInternalLinks(p, expanded, urls)...)
	}
	problems = append(problems, checkMetricsCoverage(pages)...)
	if len(problems) == 0 {
		return nil
	}
	return errors.New("docs-check failures:\n  - " + strings.Join(problems, "\n  - "))
}

// fencedCodeRE matches Markdown fenced code blocks and captures the
// info string (language + modifiers) and the body.
var fencedCodeRE = regexp.MustCompile("(?ms)^```([\\w\\- ]+)\\n(.*?)^```")

// checkConfigExamples ensures every ```yaml config-example fenced
// block parses cleanly through config.LoadBytes.
func checkConfigExamples(p *Page, body string) []string {
	var out []string
	for _, m := range fencedCodeRE.FindAllStringSubmatch(body, -1) {
		lang := strings.TrimSpace(m[1])
		if lang != "yaml config-example" {
			continue
		}
		if _, err := config.LoadBytes([]byte(m[2])); err != nil {
			out = append(out, fmt.Sprintf("%s: config-example does not parse: %v", p.SourcePath, err))
		}
	}
	return out
}

// checkInternalLinks verifies every Markdown link whose destination
// starts with "/" resolves to a known page URL. Anchors are not
// inspected.
func checkInternalLinks(p *Page, body string, urls map[string]bool) []string {
	var out []string
	reader := text.NewReader([]byte(body))
	doc := goldmark.DefaultParser().Parse(reader)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link, ok := n.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		dest := string(link.Destination)
		if !strings.HasPrefix(dest, "/") {
			return ast.WalkContinue, nil
		}
		urlPart := dest
		if i := strings.Index(dest, "#"); i >= 0 {
			urlPart = dest[:i]
		}
		if urlPart != "" && !urls[urlPart] {
			out = append(out, fmt.Sprintf("%s: broken internal link %q", p.SourcePath, dest))
		}
		return ast.WalkContinue, nil
	})
	return out
}

// checkMetricsCoverage compares web.Registry() against the body of
// metrics.md. Every registered metric must be named somewhere in the
// page. The check is skipped (with a single problem entry) when
// metrics.md is missing.
func checkMetricsCoverage(pages []*Page) []string {
	var out []string
	var metricsBody string
	for _, p := range pages {
		if p.SourcePath == "metrics.md" {
			metricsBody = p.Body
			break
		}
	}
	if metricsBody == "" {
		out = append(out, "metrics.md not found — metric coverage check skipped")
		return out
	}
	for _, d := range web.Registry() {
		if !strings.Contains(metricsBody, d.Name) {
			out = append(out, fmt.Sprintf(
				"metrics.md does not mention metric %q (registered in internal/web)",
				d.Name))
		}
	}
	return out
}
