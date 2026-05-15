package dashboards

import (
	"context"
	"io/fs"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

// mutableFS wraps an fs.FS the test can swap atomically while the
// watcher goroutine reads through it. fstest.MapFS is a bare map, so
// mutating it from one goroutine while another reads is a data race;
// this wrapper holds a pointer the test can replace under a lock.
type mutableFS struct {
	mu sync.RWMutex
	v  fs.FS
}

func (m *mutableFS) set(f fs.FS) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.v = f
}

func (m *mutableFS) Open(name string) (fs.File, error) {
	m.mu.RLock()
	v := m.v
	m.mu.RUnlock()
	return v.Open(name)
}

func (m *mutableFS) ReadDir(name string) ([]fs.DirEntry, error) {
	m.mu.RLock()
	v := m.v
	m.mu.RUnlock()
	return fs.ReadDir(v, name)
}

func newMutableFS(initial fstest.MapFS) *mutableFS {
	return &mutableFS{v: initial}
}

func TestWatcherFingerprintStableForUnchangedDir(t *testing.T) {
	t0 := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"dash/a.json": {Data: []byte("{}"), ModTime: t0},
		"dash/b.json": {Data: []byte("{}"), ModTime: t0.Add(1 * time.Second)},
		"dash/x.txt":  {Data: []byte("ignored"), ModTime: t0},
	}
	w := &Watcher{Dir: "dash", FS: fsys, Interval: 10 * time.Millisecond, OnChange: func() error { return nil }}
	a := w.fingerprint()
	b := w.fingerprint()
	if a == "" {
		t.Fatal("fingerprint empty for a readable dir")
	}
	if a != b {
		t.Errorf("fingerprint unstable: %q vs %q", a, b)
	}
}

func TestWatcherFingerprintIgnoresNonJSON(t *testing.T) {
	t0 := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"dash/a.json":   {Data: []byte("{}"), ModTime: t0},
		"dash/notes.md": {Data: []byte("hi"), ModTime: t0},
	}
	w := &Watcher{Dir: "dash", FS: fsys}
	a := w.fingerprint()

	fsys["dash/notes.md"] = &fstest.MapFile{Data: []byte("bye"), ModTime: t0.Add(time.Hour)}
	b := w.fingerprint()
	if a != b {
		t.Errorf("fingerprint flipped on non-json change: %q vs %q", a, b)
	}
}

func TestWatcherFiresOnceOnChange(t *testing.T) {
	t0 := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	mfs := newMutableFS(fstest.MapFS{
		"dash/a.json": {Data: []byte("{}"), ModTime: t0},
	})

	var calls atomic.Int32
	w := &Watcher{
		Dir:      "dash",
		FS:       mfs,
		Interval: 5 * time.Millisecond,
		OnChange: func() error { calls.Add(1); return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()

	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("calls before change = %d, want 0", got)
	}

	mfs.set(fstest.MapFS{
		"dash/a.json": {Data: []byte("{}"), ModTime: t0.Add(time.Hour)},
	})
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	if got := calls.Load(); got != 1 {
		t.Errorf("calls after one change = %d, want 1", got)
	}
}

func TestWatcherFiresAgainOnSubsequentChange(t *testing.T) {
	t0 := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	mfs := newMutableFS(fstest.MapFS{
		"dash/a.json": {Data: []byte("{}"), ModTime: t0},
	})

	var calls atomic.Int32
	w := &Watcher{
		Dir:      "dash",
		FS:       mfs,
		Interval: 5 * time.Millisecond,
		OnChange: func() error { calls.Add(1); return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()

	// Give the watcher a tick to seed its baseline fingerprint
	// before the first swap, otherwise it might race past the
	// initial state and start from the post-swap fingerprint.
	time.Sleep(15 * time.Millisecond)

	mfs.set(fstest.MapFS{
		"dash/a.json": {Data: []byte("{}"), ModTime: t0.Add(time.Hour)},
	})
	waitFor(t, &calls, 1, 200*time.Millisecond)

	mfs.set(fstest.MapFS{
		"dash/a.json": {Data: []byte("{}"), ModTime: t0.Add(time.Hour)},
		"dash/b.json": {Data: []byte("{}"), ModTime: t0.Add(2 * time.Hour)},
	})
	waitFor(t, &calls, 2, 200*time.Millisecond)

	cancel()
	<-done
}

func TestWatcherMissingDirDoesNotFire(t *testing.T) {
	fsys := fstest.MapFS{} // no entries — readdir returns ENOENT

	var calls atomic.Int32
	w := &Watcher{
		Dir:      "missing",
		FS:       fsys,
		Interval: 5 * time.Millisecond,
		OnChange: func() error { calls.Add(1); return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if got := calls.Load(); got != 0 {
		t.Errorf("calls = %d, want 0 (no change vs equally-empty baseline)", got)
	}
}

func waitFor(t *testing.T, c *atomic.Int32, want int32, max time.Duration) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if c.Load() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for calls >= %d, got %d", want, c.Load())
}
