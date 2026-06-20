---
title: Events
section: Operating
nav_order: 7
---

# Events

Events are owl's second data type alongside numeric samples:
discrete records with a structured payload, ingested pull-based and
stored in the same SQLite. They show up as a tabular panel and as
vertical annotations over timeseries panels.

## Configuration

Enable the subsystem and declare one or more sources:

```yaml
events:
  enabled: true
  sources:
    - name: watchtower
      driver: docker_logs
      container: watchtower
      from: 1m
      interval: 30s
      format: json
      match:
        - field: msg
          contains: "Found new image"
      mapping:
        ts: "$.time"
        kind: "container-updated"
        payload:
          container: "$.container"
          from: "$.old_image"
          to: "$.new_image"
      render: "{{.container}} updated {{.from}} → {{.to}}"
```

## Drivers

### file_tail

Tails a single file on disk. Cursor: `{"inode": N, "offset": N}`.
Rotation (new inode at the same path) is detected automatically and
the driver resumes from offset 0 of the new file. Required field:
`path`.

### docker_logs

Reads stdout+stderr of a named container via the daemon socket.
Cursor: RFC3339Nano timestamp of the last line seen. Requires the
`docker.enabled` integration. Required field: `container`.

## Formats

- `json` — each line is a JSON object.
- `regex` — each line is matched against `pattern`; named captures
  populate the parsed record.
- `plain` — each line becomes `{"line": "..."}`; useful for simple
  template-only rendering.

## Panels

- **events panel** (`"type": "events"`): tabular view, three
  columns (time, source / kind, event). 50 rows per page with
  scroll inside the panel. Empty `targets: []` means "all events
  in the window".
- **annotations on timeseries** (`"annotations": [{}]` on a
  timeseries panel): 1px vertical lines drawn behind the data
  line, coloured per source from the 12-slot palette, with a 6px
  hover zone exposing the event's ts and render text.

## Retention

Events share the retention window with samples. There is no
separate budget — when a day's samples expire, the events from
that day expire with them.
