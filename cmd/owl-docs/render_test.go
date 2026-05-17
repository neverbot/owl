package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeContent(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParsePageHappy(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "x.md", "---\ntitle: Hello\nsection: Start\nnav_order: 1\n---\nBody text\n")
	p, err := parsePage(dir, "x.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Frontmatter.Title != "Hello" {
		t.Errorf("title: %q", p.Frontmatter.Title)
	}
	if p.URL != "/x/" {
		t.Errorf("url: %q", p.URL)
	}
	if !strings.Contains(p.Body, "Body text") {
		t.Errorf("body lost frontmatter trim")
	}
}

func TestParsePageMissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "x.md", "no frontmatter here\n")
	if _, err := parsePage(dir, "x.md"); err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParsePageMissingTitle(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "x.md", "---\nsection: Start\n---\nbody\n")
	if _, err := parsePage(dir, "x.md"); err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestUrlFor(t *testing.T) {
	cases := map[string]string{
		"index.md":           "/",
		"getting-started.md": "/getting-started/",
		"operating/host.md":  "/operating/host/",
	}
	for in, want := range cases {
		if got := urlFor(in); got != want {
			t.Errorf("urlFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderAllProducesLayout(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writeContent(t, in, "index.md", "---\ntitle: Home\n---\nHello\n")
	writeContent(t, in, "getting-started.md",
		"---\ntitle: Start\nsection: Start\nnav_order: 1\n---\n# Begin\n")
	if err := renderAll(in, out); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(out, "getting-started/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		`<title>Start · owl docs</title>`,
		`/static/owl.css`,
		`class="topbar"`,
		`class="docs__nav"`,
		`<h1`,
	}
	for _, w := range wants {
		if !strings.Contains(string(html), w) {
			t.Errorf("layout missing %q in:\n%s", w, html)
		}
	}
}

func TestRenderAllEndToEnd(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writeContent(t, in, "index.md", "---\ntitle: Home\n---\n# Welcome\n")
	writeContent(t, in, "getting-started.md", "---\ntitle: Start\nsection: Start\n---\n## First\n")

	if err := renderAll(in, out); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"index.html", "getting-started/index.html"} {
		full := filepath.Join(out, p)
		b, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		if !strings.Contains(string(b), "<h1") && !strings.Contains(string(b), "<h2") {
			t.Errorf("%s lacks rendered heading: %s", p, b)
		}
	}
}
