package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSearchIndexCoversHeadings(t *testing.T) {
	p := &Page{
		SourcePath:  "promql.md",
		URL:         "/promql/",
		Frontmatter: Frontmatter{Title: "PromQL", Section: "Reference"},
		Body:        "intro paragraph.\n\n## rate\n\nrate description.\n\n### irate\n\nirate description.\n",
	}
	idx := buildSearchIndex([]*Page{p}, map[string]string{"promql.md": p.Body})
	if len(idx) != 3 {
		t.Fatalf("want 3 records, got %d: %+v", len(idx), idx)
	}
	urls := []string{idx[0].URL, idx[1].URL, idx[2].URL}
	want := []string{"/promql/", "/promql/#rate", "/promql/#irate"}
	for i, w := range want {
		if urls[i] != w {
			t.Errorf("idx[%d].URL = %q, want %q", i, urls[i], w)
		}
	}
	if !strings.Contains(idx[1].Terms, "rate description") {
		t.Errorf("heading record snippet/terms missing: %+v", idx[1])
	}
}

func TestBuildSearchIndexCoversWholeSection(t *testing.T) {
	body := "intro.\n\n## Socket permissions\n\nFirst paragraph here.\n\nThe bundled `compose.yml` uses `group_add: [\"0\"]`.\n\n```yaml\nservices:\n  owl:\n    group_add: [\"999\"]\n```\n\n## Next\n\nunrelated.\n"
	p := &Page{
		SourcePath:  "docker.md",
		URL:         "/operating/docker/",
		Frontmatter: Frontmatter{Title: "Docker", Section: "Operating"},
		Body:        body,
	}
	idx := buildSearchIndex([]*Page{p}, map[string]string{"docker.md": body})
	var socket *SearchRecord
	for i := range idx {
		if strings.HasSuffix(idx[i].URL, "#socket-permissions") {
			socket = &idx[i]
			break
		}
	}
	if socket == nil {
		t.Fatalf("missing socket-permissions entry: %+v", idx)
	}
	if !strings.Contains(socket.Terms, "group add") {
		t.Errorf("terms missing group_add tokens (second paragraph + code block): %q", socket.Terms)
	}
	if strings.Contains(socket.Terms, "unrelated") {
		t.Errorf("terms bled into next section: %q", socket.Terms)
	}
	if !strings.Contains(socket.Body, "group_add") {
		t.Errorf("body should keep verbatim group_add for client-side excerpt: %q", socket.Body)
	}
	if strings.Contains(socket.Body, "unrelated") {
		t.Errorf("body bled into next section: %q", socket.Body)
	}
}

func TestWriteSearchIndexIsValidJSON(t *testing.T) {
	out := t.TempDir()
	recs := []SearchRecord{{URL: "/x/", Title: "x", Terms: "x"}}
	if err := writeSearchIndex(out, recs); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, "search-index.json"))
	var back []SearchRecord
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if back[0].URL != "/x/" {
		t.Errorf("round-trip lost url")
	}
}
