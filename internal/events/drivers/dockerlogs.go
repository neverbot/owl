package drivers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"

	"github.com/neverbot/owl/internal/events"
)

// LogsClient is the narrow interface the docker_logs driver depends
// on. It is satisfied by a thin adapter over internal/docker.Client
// (added separately so the events package never imports docker).
// The since argument is empty on the first read and the RFC3339Nano
// cursor thereafter.
type LogsClient interface {
	ContainerLogs(ctx context.Context, container, since string) (io.ReadCloser, error)
}

// DockerLogs implements events.Driver against a single container's
// log stream. Cursor format is the RFC3339Nano timestamp of the last
// line seen.
type DockerLogs struct {
	name      string
	container string
	cli       LogsClient
}

// NewDockerLogs constructs a DockerLogs driver.
func NewDockerLogs(name, container string, cli LogsClient) *DockerLogs {
	return &DockerLogs{name: name, container: container, cli: cli}
}

// Name returns the source name.
func (d *DockerLogs) Name() string { return d.name }

// Read calls the daemon with --since=cursor and streams lines. Each
// Docker log line is "<RFC3339Nano> <message>"; we split, set
// Record.RawTS, and emit the message bytes only.
func (d *DockerLogs) Read(ctx context.Context, cursor string) (iter.Seq[events.Record], string, error) {
	body, err := d.cli.ContainerLogs(ctx, d.container, cursor)
	if err != nil {
		return nil, cursor, fmt.Errorf("docker logs: %w", err)
	}

	// Buffer everything so we can determine the new cursor before
	// returning. Log volume is expected to be modest (events!), so
	// buffering is acceptable; if it grows we can switch to a
	// teeing scanner.
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines [][]byte
	var tsList []int64
	var lastStamp string
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		stamp, rest := splitDockerLogLine(line)
		lines = append(lines, rest)
		if stamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				tsList = append(tsList, t.UnixMilli())
				lastStamp = stamp
			} else {
				tsList = append(tsList, 0)
			}
		} else {
			tsList = append(tsList, 0)
		}
	}
	_ = body.Close()

	seq := func(yield func(events.Record) bool) {
		for i, l := range lines {
			if ctx.Err() != nil {
				return
			}
			if !yield(events.Record{Bytes: l, RawTS: tsList[i]}) {
				return
			}
		}
	}
	newCursor := cursor
	if lastStamp != "" {
		newCursor = lastStamp
	}
	return seq, newCursor, nil
}

// splitDockerLogLine peels the leading RFC3339Nano timestamp off a
// docker log line. Returns ("", line) when the format is unexpected.
func splitDockerLogLine(line []byte) (stamp string, rest []byte) {
	sp := strings.IndexByte(string(line), ' ')
	if sp <= 0 {
		return "", line
	}
	return string(line[:sp]), line[sp+1:]
}
