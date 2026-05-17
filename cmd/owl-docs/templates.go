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
func mustParseTemplates() *template.Template {
	t, err := template.ParseFS(templateFS, "templates/*.html")
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
