---
title: Storage
section: Operating
nav_order: 5
---

owl stores time series in a single SQLite file. The schema is built
around two ideas you will see if you open the DB: a small **head**
that holds recent samples raw, and a larger **chunks** table that
holds older samples in a compressed binary format. The compression
chain — series interning, delta-of-delta timestamps, and XOR float
values — typically lands steady-state samples in the single-digit
bytes per sample range, low enough that days to weeks of history
fit on a small VPS disk.

## Two phases, one file

| Table     | Holds                                | Lives for         |
|-----------|--------------------------------------|-------------------|
| `series`  | one row per unique (metric, labels)  | forever           |
| `samples` | recent raw samples (the *head*)      | `head_window`     |
| `chunks`  | compressed older samples             | until retention drops them |

Every scrape, host tick, or docker stat goes through the same
`Append` path. The writer:

1. Looks up (or inserts) the (metric, labels) tuple in `series` and
   gets back an integer `series_id`.
2. Inserts `(series_id, ts, value)` into the head `samples` table.

The lookup is cached in memory, so steady-state appends never
round-trip to the series table — they cost one row insert each.
The (metric, labels) strings are stored exactly once per unique
series instead of once per sample, which is where the first big
saving comes from.

## The head: recent samples, raw

The `samples` table is the head. Samples land here as raw
`(series_id INTEGER, ts INTEGER, value REAL)` rows in a
`WITHOUT ROWID` clustered index. Point writes are cheap, range
queries by `(series_id, ts)` are O(log n) on the primary key, and
nothing in the head is encoded — what you wrote is what you read.

The head is bounded by `storage.head_window` (default `2h`). Older
samples are eligible for the next flush.

## Chunks: compressed older samples

On a fixed cadence (`storage.flush_interval`, default `10m`), a
background worker walks every series that has samples older than
`head_window`. For each, it reads that range out of the head,
encodes it into a binary chunk, inserts the chunk into the `chunks`
table, and deletes the rows from the head — all in one transaction
per series.

The chunk encoding is the same one Facebook published for Gorilla
(VLDB 2015), implemented in owl as ~600 lines of pure Go:

- **Timestamps** use **delta-of-delta** coding. On a regular
  scrape cadence (every 5 / 10 / 15 seconds), consecutive deltas
  are identical, so the second-derivative is zero and each
  timestamp after the first two compresses to a single bit.
- **Values** use **XOR float compression**. Consecutive samples of
  most metrics differ in only a few bits (or not at all, for
  constants like memory limits). The encoder emits that XOR
  difference; values that don't change at all cost a single bit.

A chunk holds up to 1000 samples and starts with a 30-byte header
that bootstraps the decoder. Each chunk is independent, so a
corrupt chunk only affects its own time range.

After the flush, the worker runs a SQLite `VACUUM` so the freed
head pages are reclaimed on disk. Without this, the file would
stay large even with empty head rows — SQLite normally re-uses
freed pages on later inserts, but the flush pattern (DELETE many,
re-INSERT none in the same place) leaves them stranded.

## Configuration

```yaml
storage:
  path: "/data/owl.db"
  head_window: 2h
  flush_interval: 10m
  retention:
    time: 30d
    size: 500MB
    interval: 30m
```

`head_window` and `flush_interval` are independent of the retention
policy, which is documented separately at
[Retention](/operating/retention/). In short: the flusher decides
*when raw samples become compressed*; retention decides *when any
sample (raw or compressed) is dropped entirely*.

When tuning:

- **Smaller `head_window`** (e.g. `30m`) reclaims disk faster but
  also runs the flush worker over a colder set of points each
  time. On owl-scale workloads it makes no measurable difference.
- **Smaller `flush_interval`** means a crash loses fewer minutes
  of un-flushed head samples (see *Crash safety* below) at the
  cost of more frequent `VACUUM` calls. Defaults are tuned for
  the small-host operator profile.

## Disk footprint

Storage cost per sample varies by where the sample currently lives
and by the shape of the metric.

| Stage             | Typical bytes per sample | Why it varies |
|-------------------|--------------------------|---------------|
| Head (raw)        | ~25–35                   | One row each in the head table; the variance is SQLite's per-row B-tree overhead. Independent of value shape. |
| Chunk (compressed)| ~1–10                    | Constant gauges (memory limits, build_info) approach ~1 bit per sample after the first; slow-moving counters compress to a couple of bytes; jittery gauges sit toward the high end. |

In steady state, most samples in a mature deployment live in
chunks, so the average bytes per sample across the full retention
window tracks the chunk regime more than the head regime.

Four levers shift the average:

- **Series cardinality.** A small set of series means the `series`
  table contributes negligibly per sample; thousands of unique
  series amortise less efficiently.
- **Scrape cadence regularity.** Timestamps cost ~1 bit each when
  the cadence is constant. Irregular intervals push the
  per-timestamp cost up.
- **Value entropy.** Rate of change matters more than absolute
  magnitude. A metric that hovers near a constant compresses
  better than one that jitters.
- **Chunk length.** The encoder caps chunks at 1000 samples;
  longer runs amortise the per-chunk header (~30 bytes) better.

Steady-state on-disk size sits around
`bytes_per_sample × samples_per_day × retention_days`. The exact
numbers are best measured against your own workload — install,
collect for a few days, divide `owl_storage_size_bytes` by
`owl_storage_samples_total`.

## Crash safety

The head is durable through SQLite's WAL. A `SIGKILL` or power
loss recovers cleanly on the next open — committed samples are
still there.

Chunks are written one-series-at-a-time inside a transaction that
covers both the chunk insert and the matching head delete. A crash
mid-flush leaves either:

- the original head rows intact (transaction rolled back), or
- the new chunk in place and the head rows gone (transaction
  committed).

Never a partial state. The flusher retries the rolled-back series
on the next tick.

The one window where data can be lost is the time between an
append and the next successful flush: head samples that exist only
in WAL during a hard crash *that loses the WAL file* (uncommon,
generally requires losing the filesystem). On a clean SIGTERM the
process drains and commits before exiting, so an orderly restart
loses nothing.

## Inspecting the database

owl ships with no introspection command of its own, but the file is
a stock SQLite database — any `sqlite3` binary opens it. The
schema is enough to answer most "what's in there?" questions:

```sh
sqlite3 /data/owl.db <<'SQL'
-- Top 10 series by sample count (head only)
SELECT s.metric, s.labels, COUNT(*) AS n
FROM samples x JOIN series s ON s.id = x.series_id
GROUP BY x.series_id ORDER BY n DESC LIMIT 10;

-- Chunks per series, with sample count and byte size
SELECT s.metric, s.labels,
       COUNT(*) AS chunks,
       SUM(c.count) AS samples,
       SUM(length(c.data)) AS bytes
FROM chunks c JOIN series s ON s.id = c.series_id
GROUP BY c.series_id ORDER BY bytes DESC LIMIT 10;
SQL
```

The chunks' `data` column is a binary blob owl can decode but
sqlite3 can't — there's no SQL way to expand it into rows. For
that, query owl itself via `/api/query`.

## See also

- [Retention](/operating/retention/) — time and size limits.
- [Metric sources](/operating/targets/) — where samples come from.
- Gorilla paper: *Gorilla: A Fast, Scalable, In-Memory Time Series
  Database* (VLDB 2015) — original algorithm description.
