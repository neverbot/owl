// Package drivers contains the concrete events.Driver
// implementations: file_tail and docker_logs.
package drivers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
	"syscall"

	"github.com/neverbot/owl/internal/events"
)

// FileTail implements events.Driver against a single file on disk.
// Cursor format is JSON {"inode": N, "offset": N}. On the first
// read the driver applies the from policy: "end" sets offset to
// the current file size (skip existing content); "beginning" sets
// offset to 0; any other value is treated as "beginning".
type FileTail struct {
	name string
	path string
	from string
}

// NewFileTail returns a FileTail for path with the given first-read
// policy ("end" or "beginning").
func NewFileTail(name, path, from string) *FileTail {
	return &FileTail{name: name, path: path, from: from}
}

// Name returns the source name this driver was constructed with.
func (d *FileTail) Name() string { return d.name }

// ftCursor is the on-disk cursor shape.
type ftCursor struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

// Read opens the file, applies the cursor (or first-read policy),
// streams lines from the current offset to EOF, and returns the new
// cursor (current inode + post-read offset).
func (d *FileTail) Read(ctx context.Context, cursor string) (iter.Seq[events.Record], string, error) {
	f, err := os.Open(d.path)
	if err != nil {
		return nil, cursor, fmt.Errorf("open %s: %w", d.path, err)
	}

	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, cursor, fmt.Errorf("stat: %w", err)
	}
	ino := inodeOf(fi)
	size := fi.Size()

	cur := ftCursor{}
	if cursor != "" {
		_ = json.Unmarshal([]byte(cursor), &cur)
	}

	var startOffset int64
	switch {
	case cursor == "" && d.from == "end":
		startOffset = size
	case cursor == "" || cur.Inode != ino:
		startOffset = 0 // first read from beginning, or rotated
	default:
		startOffset = cur.Offset
	}
	if startOffset > size {
		startOffset = 0 // truncated
	}
	if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, cursor, fmt.Errorf("seek: %w", err)
	}

	seq := func(yield func(events.Record) bool) {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := append([]byte(nil), sc.Bytes()...)
			if !yield(events.Record{Bytes: line}) {
				return
			}
		}
	}

	// We compute the new offset eagerly so the cursor is correct
	// even if the consumer drains lazily. After the file is fully
	// read by the scanner, the post-read position equals the file
	// size at open time; new writes after this snapshot are picked
	// up by the next tick.
	next := ftCursor{Inode: ino, Offset: size}
	newCursor, _ := json.Marshal(next)
	return seq, string(newCursor), nil
}

// inodeOf returns the inode of fi on Unix; on other platforms it
// returns a stable placeholder derived from fi.Size + ModTime so
// rotation detection still works approximately.
func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return uint64(fi.Size())
}
