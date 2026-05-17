---
title: Retention
section: Operating
nav_order: 3
---

owl stores every sample in a single SQLite database
(`storage.path`, typically `/data/owl.db`). Disks are finite, so the
storage layer runs a retention worker that drops old samples on a
schedule. Two limits apply simultaneously and whichever triggers
first wins.

```yaml
storage:
  path: "/data/owl.db"
  retention:
    time: 30d
    size: 500MB
    interval: 30m
```

## Dual policy

- **`time`** — drop samples whose timestamp is older than now minus
  this window. Use a duration (`24h`, `7d`, `30d`). Set to `0` to
  disable the time limit.
- **`size`** — drop the oldest samples first whenever the on-disk
  footprint exceeds this many bytes. Use a suffixed integer (`100MB`,
  `2GB`). Set to `0` to disable the size limit. The reported size sums
  the `.db`, `-wal` and `-shm` sidecars, so it reflects what `df`
  sees.

Either limit alone is sufficient; running both belt-and-braces is
the recommended default. A `time` cap keeps queries fast even when
the disk is enormous; a `size` cap keeps the disk from filling when
ingest spikes unexpectedly.

## `retention.interval`

`retention.interval` controls how often the worker wakes up to check
both limits. The default is `30m` — small enough that a surprise
ingest burst is contained within tens of minutes, large enough that
the worker is invisible in normal operation.

Earlier versions of owl ran the worker every minute. That used to
manifest as periodic spikes on the `owl_goroutines` and CPU charts —
the worker scanning the full samples table every 60 seconds was
cheap enough but visible. Today the worker does a cheap size check
first (using `PRAGMA page_count * page_size`) and only walks the
table when the dual policy actually requires a delete.

If you set a small `time` (under 6h) and a small `size` (under
100MB), pick an interval at or below the smaller of the two limits.
Otherwise `30m` is fine.

## Live monitoring

The `owl_storage_size_bytes` gauge on
[/metrics](/metrics/#owl_storage_size_bytes) reports the current
on-disk footprint and is plotted on the bundled **Owl Health**
dashboard. Watch it after enabling a new noisy target — the curve
should plateau within one or two retention intervals once the new
ingest rate is absorbed.

For ingest rate, take the delta between two readings of
`owl_storage_samples_total` divided by the elapsed seconds. Or, on a
dashboard, `rate(owl_storage_samples_total[5m])` (it is a gauge
modelled as a counter for `rate` to work cleanly).

## When to restart

`storage.path` and `storage.retention.*` settings are read at
startup; changing them requires a process restart. `retention.interval`
is read on each tick, so it does take effect on the next live
reload, but the rest of the retention block does not.
