package dashboards

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Watcher polls the dashboards directory for *.json mtime changes and
// invokes OnChange when the fingerprint of the directory shifts. It
// is the deliberately-simple alternative to fsnotify: zero new
// dependencies, no platform-specific syscalls, and a worst-case
// latency bounded by the configured interval. For owl's use case
// (an operator editing a panel JSON) a few seconds of detection lag
// is unnoticeable.
//
// The fingerprint covers the sorted list of `path\x00mtime` pairs, so
// any combination of file rename, delete, create or content rewrite
// flips it. Subdirectories are not recursed: dashboards live flat in
// the root directory by convention.
type Watcher struct {
	Dir      string
	Interval time.Duration

	// OnChange is invoked from the watcher's goroutine when the
	// fingerprint changes. Errors are logged and otherwise ignored —
	// the next tick reads the directory afresh, so a transient I/O
	// failure self-heals.
	OnChange func() error

	// FS is the filesystem the watcher reads from. Defaults to the OS
	// filesystem rooted at "/"; tests inject fstest.MapFS.
	FS fs.FS
}

// Run blocks until ctx is cancelled. It samples the directory once on
// entry to seed the baseline fingerprint, so a watcher that starts
// with the directory already in the desired state does NOT fire
// OnChange spuriously.
func (w *Watcher) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	prev := w.fingerprint()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cur := w.fingerprint()
			if cur == prev {
				continue
			}
			prev = cur
			if err := w.OnChange(); err != nil {
				slog.Error("dashboards watcher reload failed", "err", err)
			}
		}
	}
}

// fingerprint returns a stable hash of every *.json file's path +
// mtime in the watched directory. Errors collapse to an empty string,
// which means "directory not readable right now"; OnChange will fire
// once the directory comes back. Two empty fingerprints in a row are
// considered "no change", so a missing directory does not produce a
// reload storm.
func (w *Watcher) fingerprint() string {
	entries, err := w.listEntries()
	if err != nil {
		return ""
	}
	// Stable order — readdir is documented as ordered by name on
	// both unix and windows but we sort defensively, since the fs.FS
	// abstraction does not promise it.
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	h := sha1.New()
	for _, e := range entries {
		h.Write([]byte(e.name))
		h.Write([]byte{0})
		h.Write([]byte(e.mtime.UTC().Format(time.RFC3339Nano)))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

type fpEntry struct {
	name  string
	mtime time.Time
}

func (w *Watcher) listEntries() ([]fpEntry, error) {
	fsys, dir := w.fsRoot()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	out := make([]fpEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, fpEntry{name: e.Name(), mtime: info.ModTime()})
	}
	return out, nil
}

// fsRoot adapts (FS, Dir) to fs.ReadDir's two-argument shape. When FS
// is nil the watcher uses the OS filesystem rooted at "/" and treats
// Dir as an absolute path with the leading slash trimmed; fs.FS does
// not accept leading slashes.
func (w *Watcher) fsRoot() (fs.FS, string) {
	if w.FS != nil {
		dir := w.Dir
		if dir == "" {
			dir = "."
		}
		return w.FS, filepath.ToSlash(dir)
	}
	return os.DirFS("/"), strings.TrimPrefix(filepath.ToSlash(w.Dir), "/")
}
