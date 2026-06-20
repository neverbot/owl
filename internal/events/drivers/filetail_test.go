package drivers

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"github.com/neverbot/owl/internal/events"
)

// collect drains an iter.Seq[Record] into a slice.
func collect(seq iter.Seq[events.Record]) []events.Record {
	var out []events.Record
	seq(func(r events.Record) bool { out = append(out, r); return true })
	return out
}

// TestFileTailReadsFromBeginning asserts an empty cursor with
// from=beginning yields every line in the file.
func TestFileTailReadsFromBeginning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewFileTail("s", path, "beginning")
	seq, cur, err := d.Read(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := collect(seq)
	if len(got) != 3 || string(got[0].Bytes) != "a" || string(got[2].Bytes) != "c" {
		t.Fatalf("got %#v", got)
	}
	if cur == "" {
		t.Fatal("cursor empty")
	}
}

// TestFileTailFromEndSkipsExisting asserts from=end skips current
// content on the first read.
func TestFileTailFromEndSkipsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewFileTail("s", path, "end")
	seq, cur, err := d.Read(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := collect(seq); len(got) != 0 {
		t.Fatalf("want 0, got %#v", got)
	}
	// Append new content; the second read should yield only "new".
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("new\n")
	_ = f.Close()
	seq2, _, err := d.Read(context.Background(), cur)
	if err != nil {
		t.Fatal(err)
	}
	got := collect(seq2)
	if len(got) != 1 || string(got[0].Bytes) != "new" {
		t.Fatalf("got %#v", got)
	}
}

// TestFileTailRotation asserts a new inode (file replaced) causes
// the driver to read from offset 0 of the new file.
func TestFileTailRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewFileTail("s", path, "beginning")
	_, cur, err := d.Read(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	// Replace the file (simulating logrotate move-create).
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("rotated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seq, _, err := d.Read(context.Background(), cur)
	if err != nil {
		t.Fatal(err)
	}
	got := collect(seq)
	if len(got) != 1 || string(got[0].Bytes) != "rotated" {
		t.Fatalf("after rotation, got %#v", got)
	}
}
