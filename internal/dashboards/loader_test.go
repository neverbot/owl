package dashboards

import (
	"os"
	"path/filepath"
	"testing"
)

// nullCaps is a Capabilities that reports everything as supported.
type nullCaps struct{}

func (nullCaps) IsSupported(_ string) (bool, string) { return true, "" }

func TestLoader_ReloadAndGet(t *testing.T) {
	dir := t.TempDir()

	// Copy runtime fixture into temp dir.
	data, err := os.ReadFile("testdata/runtime.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.json"), data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	l := NewLoader(dir, nullCaps{})
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	d, ok := l.Get("runtime")
	if !ok {
		t.Fatal("Get('runtime') returned false")
	}
	if d.Title != "Runtime" {
		t.Errorf("Title = %q, want %q", d.Title, "Runtime")
	}
	if d.Source != filepath.Join(dir, "runtime.json") {
		t.Errorf("Source = %q, want %q", d.Source, filepath.Join(dir, "runtime.json"))
	}
	if len(d.Panels) != 3 {
		t.Errorf("len(Panels) = %d, want 3", len(d.Panels))
	}
}

func TestLoader_GetMissing(t *testing.T) {
	l := NewLoader(t.TempDir(), nullCaps{})
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	_, ok := l.Get("nonexistent")
	if ok {
		t.Error("Get('nonexistent') should return false")
	}
}

func TestLoader_List(t *testing.T) {
	dir := t.TempDir()

	// Write two dashboards — names chosen so alphabetical order != insertion order.
	writeJSON := func(name, title string) {
		t.Helper()
		data := []byte(`{"title":"` + title + `","panels":[]}`)
		if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeJSON("zebra", "Zebra")
	writeJSON("alpha", "Alpha")

	l := NewLoader(dir, nullCaps{})
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	list := l.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	// Must be sorted by ID (slug).
	if list[0].ID != "alpha" || list[1].ID != "zebra" {
		t.Errorf("List order = [%s, %s], want [alpha, zebra]", list[0].ID, list[1].ID)
	}
}

func TestLoader_SkipsNonJSON(t *testing.T) {
	dir := t.TempDir()

	// A .txt file and a .json file.
	os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("not json"), 0o644)
	os.WriteFile(filepath.Join(dir, "ok.json"), []byte(`{"title":"OK","panels":[]}`), 0o644)

	l := NewLoader(dir, nullCaps{})
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	list := l.List()
	if len(list) != 1 || list[0].ID != "ok" {
		t.Errorf("expected 1 dashboard 'ok', got %v", len(list))
	}
}

func TestLoader_SkipsBadJSON(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{not json`), 0o644)
	os.WriteFile(filepath.Join(dir, "good.json"), []byte(`{"title":"Good","panels":[]}`), 0o644)

	l := NewLoader(dir, nullCaps{})
	// Reload must not return an error for individual bad files.
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload returned error for bad file: %v", err)
	}
	list := l.List()
	if len(list) != 1 || list[0].ID != "good" {
		t.Errorf("expected 1 dashboard 'good', got %d", len(list))
	}
}

func TestLoader_ReloadIsAtomic(t *testing.T) {
	// Reload replaces the whole index; removed files disappear after reload.
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "first.json"), []byte(`{"title":"First","panels":[]}`), 0o644)

	l := NewLoader(dir, nullCaps{})
	if err := l.Reload(); err != nil {
		t.Fatalf("first Reload: %v", err)
	}
	if _, ok := l.Get("first"); !ok {
		t.Fatal("expected 'first' after first Reload")
	}

	// Remove the file and add a different one.
	os.Remove(filepath.Join(dir, "first.json"))
	os.WriteFile(filepath.Join(dir, "second.json"), []byte(`{"title":"Second","panels":[]}`), 0o644)

	if err := l.Reload(); err != nil {
		t.Fatalf("second Reload: %v", err)
	}
	if _, ok := l.Get("first"); ok {
		t.Error("'first' should be gone after second Reload")
	}
	if _, ok := l.Get("second"); !ok {
		t.Error("'second' should appear after second Reload")
	}
}

func TestLoader_DirectoryNotFound(t *testing.T) {
	l := NewLoader("/nonexistent/path/xyz", nullCaps{})
	err := l.Reload()
	if err == nil {
		t.Error("Reload should error when directory does not exist")
	}
}
