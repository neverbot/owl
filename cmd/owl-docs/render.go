package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"html/template"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"gopkg.in/yaml.v3"
)

// docsMarkdown is the configured goldmark instance shared by every
// page render. It enables the GFM table extension, allows raw HTML
// (so partial expansions like {{> chart …}} survive the rendering
// stage), produces auto-generated anchor IDs for headings, and emits
// chroma syntax highlighting as class names so the docs stylesheet
// owns the colour palette.
var docsMarkdown = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.Strikethrough,
		extension.Linkify,
		highlighting.NewHighlighting(
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
				chromahtml.ClassPrefix("chr-"),
			),
		),
	),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// Page is one Markdown source file with its parsed frontmatter and
// raw body. Partial expansion and HTML rendering operate on this
// structure.
type Page struct {
	// SourcePath is the path of the .md file relative to the content
	// root (e.g. "operating/host.md").
	SourcePath string
	// URL is the public URL the page will be served at, derived from
	// SourcePath (e.g. "/operating/host/").
	URL string
	// Frontmatter holds the parsed YAML metadata.
	Frontmatter Frontmatter
	// Body is the Markdown body (everything after the second "---").
	Body string
}

// Frontmatter is the YAML block at the top of every content page.
type Frontmatter struct {
	Title    string `yaml:"title"`
	NavOrder int    `yaml:"nav_order"`
	Section  string `yaml:"section"`
}

// parsePage reads one .md file and returns a Page.
// It returns an error if the frontmatter is missing or malformed.
func parsePage(contentRoot, relPath string) (*Page, error) {
	full := filepath.Join(contentRoot, relPath)
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return nil, fmt.Errorf("%s: missing frontmatter (must start with ---)", relPath)
	}
	rest := raw[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return nil, fmt.Errorf("%s: unterminated frontmatter", relPath)
	}
	var fm Frontmatter
	if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
		return nil, fmt.Errorf("%s: frontmatter: %w", relPath, err)
	}
	if fm.Title == "" {
		return nil, fmt.Errorf("%s: frontmatter missing required field 'title'", relPath)
	}
	body := string(rest[end+5:])
	return &Page{
		SourcePath:  relPath,
		URL:         urlFor(relPath),
		Frontmatter: fm,
		Body:        body,
	}, nil
}

// urlFor maps a source path to its public URL.
// "index.md" -> "/", "getting-started.md" -> "/getting-started/",
// "operating/host.md" -> "/operating/host/".
func urlFor(relPath string) string {
	if relPath == "index.md" {
		return "/"
	}
	trimmed := strings.TrimSuffix(relPath, ".md")
	return "/" + trimmed + "/"
}

// loadPages walks contentRoot and returns every .md file as a Page,
// sorted by Section + NavOrder + URL.
func loadPages(contentRoot string) ([]*Page, error) {
	var pages []*Page
	err := filepath.WalkDir(contentRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, err := filepath.Rel(contentRoot, path)
		if err != nil {
			return err
		}
		p, err := parsePage(contentRoot, rel)
		if err != nil {
			return err
		}
		pages = append(pages, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// renderBody converts Markdown to HTML using docsMarkdown, which
// is configured with GFM tables, raw-HTML pass-through (so partial
// HTML survives), auto heading IDs and chroma syntax highlighting.
// Partial expansion happens *before* this call. After rendering, any
// site-internal absolute href ("/getting-started/", "/promql/#rate")
// is rewritten with the configured base URL prefix so the same
// markdown content publishes correctly under "/" or "/owl/".
func renderBody(body string) (string, error) {
	var buf bytes.Buffer
	if err := docsMarkdown.Convert([]byte(body), &buf); err != nil {
		return "", fmt.Errorf("goldmark: %w", err)
	}
	return rewriteInternalHrefs(buf.String()), nil
}

// internalAttrRE matches href="/path" or src="/path" attributes whose
// path starts with a single slash followed by a letter, digit or "#"
// — i.e. a site-internal absolute URL. Protocol-relative ("//cdn")
// and full URLs ("https://…") are skipped because they don't begin
// with "/x".
var internalAttrRE = regexp.MustCompile(`(href|src)="(/[A-Za-z0-9#][^"]*)"`)

// rewriteInternalHrefs prefixes every internal absolute href and src
// in the rendered HTML with the configured base URL. No-op when
// baseURL is empty. Both attributes are handled so markdown images
// (rendered as <img src=…>) get the same prefix treatment as links.
func rewriteInternalHrefs(s string) string {
	if baseURL == "" {
		return s
	}
	return internalAttrRE.ReplaceAllStringFunc(s, func(match string) string {
		g := internalAttrRE.FindStringSubmatch(match)
		return g[1] + `="` + withBase(g[2]) + `"`
	})
}

// writePage materialises a Page to disk as outDir/<URL>/index.html.
// Templates (Task 7) wrap the rendered body with the docs layout.
func writePage(outDir string, p *Page, html string) error {
	target := filepath.Join(outDir, strings.TrimPrefix(p.URL, "/"), "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(html), 0o644)
}

// renderAll loads, expands, renders, and writes every page.
// Partial expansion is added in Task 8; layout wrapping in Task 7.
// At the end it materialises every fixture referenced by a {{> chart}}
// partial into outDir/data/<name>.json so the static panels can fetch
// them via data-static.
func renderAll(inDir, outDir string) error {
	resetReferencedFixtures()
	pages, err := loadPages(inDir)
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		return errors.New("no .md files found under " + inDir)
	}
	nav := buildNav(pages)
	expanded := map[string]string{}
	for _, p := range pages {
		e, err := expandPartials(p.Body)
		if err != nil {
			return fmt.Errorf("%s: %w", p.SourcePath, err)
		}
		expanded[p.SourcePath] = e
		body, err := renderBody(e)
		if err != nil {
			return fmt.Errorf("%s: %w", p.SourcePath, err)
		}
		view := PageView{
			Title:    p.Frontmatter.Title,
			Section:  p.Frontmatter.Section,
			URL:      p.URL,
			BodyHTML: template.HTML(body), // body is already-escaped HTML from goldmark
			Nav:      nav,
			BaseURL:  baseURL,
		}
		var out bytes.Buffer
		if err := docsTemplates.ExecuteTemplate(&out, "layout", view); err != nil {
			return fmt.Errorf("%s: render layout: %w", p.SourcePath, err)
		}
		if err := writePage(outDir, p, out.String()); err != nil {
			return err
		}
	}
	if err := writeFixtures(outDir); err != nil {
		return fmt.Errorf("write fixtures: %w", err)
	}
	if err := writeSearchIndex(outDir, buildSearchIndex(pages, expanded)); err != nil {
		return fmt.Errorf("write search index: %w", err)
	}
	return nil
}

// writeFixtures materialises every fixture referenced during the last
// renderAll into outDir/data/<name>.json. Fixtures not referenced by
// any page are skipped so the output stays minimal.
func writeFixtures(outDir string) error {
	dir := filepath.Join(outDir, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range referencedFixtureList() {
		f, _ := LookupFixture(name)
		b, err := MarshalFixture(f)
		if err != nil {
			return fmt.Errorf("marshal %q: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// buildNav groups pages into ordered sections for the side nav.
// Pages without a section land under "Misc".
func buildNav(pages []*Page) []NavSection {
	const misc = "Misc"
	byOrder := []string{"Start", "Reference", "Operating", misc}
	groups := map[string][]NavLink{}
	for _, p := range pages {
		sec := p.Frontmatter.Section
		if sec == "" {
			if p.URL == "/" {
				continue // homepage doesn't appear in side nav
			}
			sec = misc
		}
		groups[sec] = append(groups[sec], NavLink{
			Title: p.Frontmatter.Title, URL: p.URL,
		})
	}
	// Stable order within each section: by nav_order then URL.
	for sec := range groups {
		links := groups[sec]
		sortNavLinks(links, pages)
		groups[sec] = links
	}
	out := make([]NavSection, 0, len(byOrder))
	for _, sec := range byOrder {
		if len(groups[sec]) > 0 {
			out = append(out, NavSection{Section: sec, Pages: groups[sec]})
		}
	}
	return out
}

// sortNavLinks sorts links in place by (NavOrder, URL).
func sortNavLinks(links []NavLink, allPages []*Page) {
	orderOf := map[string]int{}
	for _, p := range allPages {
		orderOf[p.URL] = p.Frontmatter.NavOrder
	}
	for i := 1; i < len(links); i++ {
		for j := i; j > 0; j-- {
			a, b := links[j-1], links[j]
			ao, bo := orderOf[a.URL], orderOf[b.URL]
			if ao < bo || (ao == bo && a.URL <= b.URL) {
				break
			}
			links[j-1], links[j] = b, a
		}
	}
}
