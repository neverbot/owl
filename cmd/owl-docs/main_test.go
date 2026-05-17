package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesDesignAssets(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "index.md"),
		[]byte("---\ntitle: Home\n---\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run(in, out, false); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, p := range []string{"static/owl.css", "static/app.js"} {
		full := filepath.Join(out, p)
		info, err := os.Stat(full)
		if err != nil {
			t.Errorf("missing %s: %v", p, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", p)
		}
	}
}

func TestRunWritesSearchJS(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "index.md"),
		[]byte("---\ntitle: Home\n---\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(in, out, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(out, "static/search.js"))
	if err != nil || info.Size() == 0 {
		t.Fatalf("search.js missing or empty: %v", err)
	}
}

func TestRunCheckRunsValidators(t *testing.T) {
	// With no metrics.md present the metric-coverage validator must
	// flag the absence, proving --check now invokes runChecks rather
	// than the previous no-op.
	in := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "index.md"),
		[]byte("---\ntitle: Home\n---\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(in, "", true)
	if err == nil {
		t.Fatal("expected validation failure (missing metrics.md)")
	}
}
