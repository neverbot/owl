package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesDesignAssets(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()

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

func TestRunCheckIsNoop(t *testing.T) {
	in := t.TempDir()
	if err := run(in, "", true); err != nil {
		t.Fatalf("--check returned error: %v", err)
	}
}
