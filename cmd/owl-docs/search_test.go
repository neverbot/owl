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
