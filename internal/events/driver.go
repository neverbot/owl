package events

import (
	"context"
	"iter"
)

// Record is one raw line as the driver pulled it. RawTS is optional;
// when non-zero, the manager prefers it over the parsed ts (used by
// docker_logs, which already carries an RFC3339Nano stamp ahead of
// the line). Bytes contains the line itself, stripped of trailing
// newline if any.
type Record struct {
	Bytes []byte
	RawTS int64 // unix ms; 0 means "fall back to mapping/parsing"
}

// Driver is the only contract a source has to satisfy. Read returns
// a single-use iter.Seq over the records produced since cursor, the
// new cursor to persist after consumption, and any error. cursor is
// opaque to the manager — each driver decides its own format.
type Driver interface {
	Name() string
	Read(ctx context.Context, cursor string) (iter.Seq[Record], string, error)
}
