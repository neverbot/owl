package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"html/template"

	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v3"
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

// renderBody converts Markdown to HTML using goldmark with default
// extensions. Partial expansion happens *before* this call.
func renderBody(body string) (string, error) {
	var buf bytes.Buffer
	if err := goldmark.New().Convert([]byte(body), &buf); err != nil {
		return "", fmt.Errorf("goldmark: %w", err)
	}
	return buf.String(), nil
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
func renderAll(inDir, outDir string) error {
	pages, err := loadPages(inDir)
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		return errors.New("no .md files found under " + inDir)
	}
	nav := buildNav(pages)
	for _, p := range pages {
		body, err := renderBody(p.Body)
		if err != nil {
			return fmt.Errorf("%s: %w", p.SourcePath, err)
		}
		view := PageView{
			Title:    p.Frontmatter.Title,
			Section:  p.Frontmatter.Section,
			URL:      p.URL,
			BodyHTML: template.HTML(body), // body is already-escaped HTML from goldmark
			Nav:      nav,
		}
		var out bytes.Buffer
		if err := docsTemplates.ExecuteTemplate(&out, "layout", view); err != nil {
			return fmt.Errorf("%s: render layout: %w", p.SourcePath, err)
		}
		if err := writePage(outDir, p, out.String()); err != nil {
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
