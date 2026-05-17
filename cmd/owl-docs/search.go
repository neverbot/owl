package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// SearchRecord is one searchable entry: a page, or one of its h2/h3
// headings. Terms is pre-tokenised lowercase text used for matching.
type SearchRecord struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	Breadcrumb string `json:"breadcrumb"`
	Snippet    string `json:"snippet"`
	Terms      string `json:"terms"`
}

// buildSearchIndex extracts records from every page's expanded body.
// The page title plus headings (h2/h3) with their first following
// paragraph form the entries.
func buildSearchIndex(pages []*Page, expanded map[string]string) []SearchRecord {
	var out []SearchRecord
	for _, p := range pages {
		body := expanded[p.SourcePath]
		entries := extractSearchEntries(p, body)
		out = append(out, entries...)
	}
	return out
}

// extractSearchEntries walks one page's Markdown AST and returns the
// page-level record followed by one record per h2/h3 heading.
func extractSearchEntries(p *Page, body string) []SearchRecord {
	src := []byte(body)
	reader := text.NewReader(src)
	doc := goldmark.DefaultParser().Parse(reader)

	page := SearchRecord{
		URL:        p.URL,
		Title:      p.Frontmatter.Title,
		Breadcrumb: p.Frontmatter.Section,
		Snippet:    firstParagraphText(doc, src),
	}
	page.Terms = tokenise(page.Title + " " + page.Snippet)
	out := []SearchRecord{page}

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok || (h.Level != 2 && h.Level != 3) {
			return ast.WalkContinue, nil
		}
		title := plainText(h, src)
		anchor := slugify(title)
		snippet := paragraphAfter(h, src)
		out = append(out, SearchRecord{
			URL:        p.URL + "#" + anchor,
			Title:      title,
			Breadcrumb: p.Frontmatter.Section + " · " + p.Frontmatter.Title,
			Snippet:    snippet,
			Terms:      tokenise(title + " " + snippet),
		})
		return ast.WalkContinue, nil
	})
	return out
}

// plainText concatenates the immediate text children of n.
func plainText(n ast.Node, src []byte) string {
	var b bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(src))
		}
	}
	return b.String()
}

// firstParagraphText returns the text of the first paragraph child of doc.
func firstParagraphText(doc ast.Node, src []byte) string {
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		if p, ok := c.(*ast.Paragraph); ok {
			return plainText(p, src)
		}
	}
	return ""
}

// paragraphAfter returns the text of the first paragraph following h.
func paragraphAfter(h ast.Node, src []byte) string {
	for n := h.NextSibling(); n != nil; n = n.NextSibling() {
		if p, ok := n.(*ast.Paragraph); ok {
			return plainText(p, src)
		}
	}
	return ""
}

// slugify lowercases s and collapses runs of non-alphanumerics into a
// single hyphen, matching the heading anchors goldmark emits.
func slugify(s string) string {
	var b strings.Builder
	prevHyphen := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// tokenise lowercases s, replaces non-alphanumerics with spaces, and
// collapses whitespace so the client-side matcher can do simple
// substring checks.
func tokenise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// writeSearchIndex serialises records as JSON to outDir/search-index.json.
func writeSearchIndex(outDir string, records []SearchRecord) error {
	b, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "search-index.json"), b, 0o644)
}
