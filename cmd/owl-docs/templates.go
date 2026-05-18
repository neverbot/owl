package main

import (
	"embed"
	"fmt"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

// docsTemplates is the parsed template set shared by every rendered
// page. Parsed once at startup; cheap to execute per page.
var docsTemplates = mustParseTemplates()

// mustParseTemplates parses every template under templates/ at init.
// It panics on failure because a malformed template would block the
// generator from producing any output and should be caught immediately.
// The funcs map exposes withBase to templates as "base", so any href
// or src can prefix site-internal paths with the configured base URL.
func mustParseTemplates() *template.Template {
	t, err := template.New("docs").Funcs(template.FuncMap{
		"base": withBase,
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		panic(fmt.Sprintf("parse templates: %v", err))
	}
	return t
}

// PageView is the data structure handed to the layout template.
// Constructed once per page, it carries everything templates read.
type PageView struct {
	Title    string
	Section  string
	URL      string
	BodyHTML template.HTML
	Nav      []NavSection
	// BaseURL is the prefix every site-internal absolute URL lives
	// under. It is the empty string when the site is rooted at "/",
	// or e.g. "/owl" when published to a GitHub Pages subdirectory.
	// Templates use the `base` func instead of reading this directly.
	BaseURL string
}

// NavSection groups pages under one side-nav heading.
type NavSection struct {
	Section string
	Pages   []NavLink
}

// NavLink is one entry in the side nav.
type NavLink struct {
	Title string
	URL   string
}
