package dashboards

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"unsafe"
)

// index is the immutable snapshot that Loader exposes via Get and List.
// It is replaced atomically on each Reload.
type index struct {
	byID    map[string]*Dashboard
	ordered []*Dashboard // sorted by ID for deterministic List output
}

// Loader reads dashboard JSON files from a directory and indexes them.
// Reload must be called before Get/List to populate the index.
type Loader struct {
	dir  string
	caps Capabilities
	// idx holds a *index; stored as unsafe.Pointer so we can use
	// atomic.LoadPointer / atomic.StorePointer without generics dependency.
	idx unsafe.Pointer //nolint:unused // accessed via atomic helpers
}

// NewLoader returns a Loader that reads from dir.
// Reload must be called before Get/List to actually populate the index.
func NewLoader(dir string, caps Capabilities) *Loader {
	l := &Loader{
		dir:  dir,
		caps: caps,
	}
	empty := &index{byID: make(map[string]*Dashboard)}
	atomic.StorePointer(&l.idx, unsafe.Pointer(empty))
	return l
}

// Reload re-reads all *.json files from the configured directory and rebuilds
// the in-memory index atomically. Files that fail to parse are logged and
// skipped. Returns an error only if the directory itself is unreadable.
func (l *Loader) Reload() error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return fmt.Errorf("dashboards: read directory %q: %w", l.dir, err)
	}

	byID := make(map[string]*Dashboard, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		slug := strings.TrimSuffix(name, ".json")
		fullPath := filepath.Join(l.dir, name)

		data, err := os.ReadFile(fullPath)
		if err != nil {
			slog.Warn("dashboards: read failed", "path", fullPath, "err", err)
			continue
		}
		d, err := ParseDashboard(slug, data)
		if err != nil {
			slog.Warn("dashboards: parse failed", "path", fullPath, "err", err)
			continue
		}
		d.Source = fullPath
		d.Panels = annotateSupport(d.Panels, l.caps)
		byID[slug] = d
	}

	ordered := make([]*Dashboard, 0, len(byID))
	for _, d := range byID {
		ordered = append(ordered, d)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	newIdx := &index{byID: byID, ordered: ordered}
	atomic.StorePointer(&l.idx, unsafe.Pointer(newIdx))
	return nil
}

// loadIndex returns the current index snapshot.
func (l *Loader) loadIndex() *index {
	return (*index)(atomic.LoadPointer(&l.idx))
}

// Get returns a dashboard by its slug (filename without .json).
// Returns (nil, false) if not found.
func (l *Loader) Get(id string) (*Dashboard, bool) {
	idx := l.loadIndex()
	d, ok := idx.byID[id]
	return d, ok
}

// List returns all dashboards in deterministic order (sorted by ID/slug).
func (l *Loader) List() []*Dashboard {
	return l.loadIndex().ordered
}

// Dir returns the directory the Loader was configured to read from.
// Exposed for diagnostic copy on the homepage and in error messages.
func (l *Loader) Dir() string {
	return l.dir
}
