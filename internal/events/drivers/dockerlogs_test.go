package drivers

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/neverbot/owl/internal/events"
)

// fakeLogsClient implements LogsClient against a canned body.
type fakeLogsClient struct {
	body  string
	since string
	calls int
}

// ContainerLogs records the since arg and returns the canned body.
func (f *fakeLogsClient) ContainerLogs(_ context.Context, _, since string) (io.ReadCloser, error) {
	f.since = since
	f.calls++
	return io.NopCloser(strings.NewReader(f.body)), nil
}

// TestDockerLogsParsesRFC3339Prefix asserts each line's leading
// RFC3339Nano timestamp becomes Record.RawTS, and the new cursor is
// the latest timestamp seen.
func TestDockerLogsParsesRFC3339Prefix(t *testing.T) {
	body := "2026-06-20T12:00:00.000Z hello\n" +
		"2026-06-20T12:00:01.000Z world\n"
	cli := &fakeLogsClient{body: body}
	d := NewDockerLogs("s", "watchtower", cli)
	seq, cur, err := d.Read(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	var got []events.Record = collect(seq)
	if len(got) != 2 || string(got[1].Bytes) != "world" {
		t.Fatalf("got %#v", got)
	}
	if cur != "2026-06-20T12:00:01Z" && cur != "2026-06-20T12:00:01.000Z" {
		t.Fatalf("cursor=%q", cur)
	}
}

// TestDockerLogsPassesCursorAsSince asserts the cursor flows through
// to the LogsClient as the --since parameter on the next call.
func TestDockerLogsPassesCursorAsSince(t *testing.T) {
	cli := &fakeLogsClient{body: ""}
	d := NewDockerLogs("s", "watchtower", cli)
	if _, _, err := d.Read(context.Background(), "2026-06-20T11:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if cli.since != "2026-06-20T11:00:00Z" {
		t.Fatalf("since=%q", cli.since)
	}
}
