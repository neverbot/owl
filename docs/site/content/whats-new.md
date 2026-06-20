---
title: What's new
section: Updates
nav_order: 1
---

User-visible changes shipped to `ghcr.io/neverbot/owl:latest`,
newest first. owl does not yet cut formal version releases — the
rolling `:latest` tag is updated on every push to master. Anything
that changes default behaviour, adds a config knob, or removes a
feature shows up here.

## 2026-06-20

- **Events.** Owl gains a second data type alongside numeric
  samples: discrete events with structured payload. Two drivers
  in v1 (`file_tail`, `docker_logs`), a new tabular events panel,
  and vertical annotations on timeseries panels. Opt-in via
  `events.enabled: true`. See [Events](/operating/events/) for the
  full reference.

## 2026-06-18

- **Authenticated scrape targets.** Per-target `auth` block supporting
  bearer tokens, HTTP Basic and arbitrary headers, plus an optional
  `tls` block (`insecure_skip_verify`, `ca_file`) for `https://`
  endpoints. Any string in `config.yml` can carry a `${VAR}`,
  `${VAR:-default}` or `file:/abs/path` reference; secrets resolve on
  every load and every `SIGHUP` / `POST /-/reload`, so rotating a
  `file:` secret needs no redeploy. 401 and 403 responses now produce
  specialised error messages on `/targets` so the operator knows
  exactly where to look. `/api/targets` JSON gains `last_status_code`
  and `auth` fields. See [Authentication](/operating/authentication/)
  for the full surface.
- **Stat panels render real numbers.** Panels of `type: "stat"` (and
  `type: "gauge"`, which currently renders the same way) collapse the
  query result to a single big number via
  `options.reduceOptions.calcs`. Supported calcs: `lastNotNull`
  (default), `last`, `first`, `max`, `min`, `mean`, `sum`. Formatting
  respects `fieldConfig.defaults.unit` and
  `fieldConfig.defaults.decimals`. Multi-series queries render the
  first series' reduced value; documented as an anti-pattern.
- **Stat panels with opt-in sparkline.** Set
  `options.graphMode: "area"` and the stat panel layers a low-opacity
  chart behind the headline, with its own Y/X axes (value min/max,
  time range / "now"). Hovering the panel scrubs through the series:
  the headline temporarily reads the value under the cursor, a
  reserved slot underneath shows the time delta (so the number does
  not shift on hover), a dashed crosshair and a per-series hover-point
  match the convention used by the multi-line chart panels. See
  [Stat panels](/dashboards/#stat-panels).
- **Multi-target panels render every series.** Panels that declare
  more than one `targets[]` no longer silently drop everything past
  the first. The web layer emits a single `data-queries` JSON array
  to the browser, and `chart.js` fans out one `/api/query` fetch per
  target via `Promise.all`, merges the returned series preserving
  YAML order, and renders them with their own `legendFormat`
  templates. Bug fix; no config change needed — existing dashboards
  that already declared multiple targets start showing the missing
  lines on first reload.

## 2026-06-09

- New storage layer: per-sample disk footprint drops into the
  single-digit bytes per sample range thanks to series interning
  and Gorilla-style chunk compression. Two new config knobs,
  `storage.head_window` (default `2h`) and
  `storage.flush_interval` (default `10m`); see
  [Storage](/operating/storage/) for the full picture.
- `/targets` is now [Metric sources](/operating/targets/) and shows
  three sections when their sources are enabled: scrape targets,
  internal collectors (host, docker metrics, docker discovery), and
  the list of containers being observed. `/api/targets` JSON gains
  `collectors` and `containers` keys; existing consumers see the
  same `targets` key as before.
- Documentation search now indexes the full body of every section
  and highlights the verbatim query in match-centered excerpts.
- Multi-arch container image cross-compiles from amd64 instead of
  running under QEMU emulation, cutting CI build time by ~5 minutes
  per push.

## 2026-05-18

First iteration with a complete feature set, published as a single
static Go binary at `ghcr.io/neverbot/owl:latest`, multi-arch
(`linux/amd64`, `linux/arm64`), built on
`gcr.io/distroless/static-debian12:nonroot`.

- Scrape Prometheus `/metrics` endpoints on per-target intervals,
  with optional keep/drop regex filters on metric names. See
  [Configuration](/config/).
- Embedded SQLite storage with a dual time + size retention
  policy. See [Retention](/operating/retention/).
- PromQL subset covering `rate`, `irate`, `increase`, `delta`, the
  `*_over_time` family, `topk`, `bottomk`, `histogram_quantile`,
  and `without(...)` in aggregations. See [PromQL](/promql/).
- Server-rendered dashboards loaded from a Grafana JSON subset,
  with an opt-in mtime watcher that hot-reloads edited files. See
  [Dashboards](/dashboards/).
- Threshold alert rules with `>`, `>=`, `<`, `<=` and a `for`
  duration, delivered to a configured webhook on firing and
  resolved. See [Alerting](/alerting/).
- Linux host collector reading `/proc` and `/sys` for
  node_exporter-compatible `node_*` metrics, opt-in via
  `host.enabled`. See [Host collector](/operating/host/).
- Docker integration: per-container metrics including a
  cgroup-aware `container_memory_anon_bytes` gauge that excludes
  the page cache, plus label-based scrape-target discovery. See
  [Docker integration](/operating/docker/).
- Web UI with light and dark themes (system-preference default), a
  12-colour series palette, time-range picker, calendar-based time
  navigation, hover crosshair, and a `/targets` health page.
- Self-observability: owl scrapes its own `/metrics` and the
  bundled **Owl Health** dashboard plots the result. SIGHUP and
  `POST /-/reload` re-read config and dashboards atomically.
- Public documentation site (the one you are reading) built from
  the same repository, with config schema, PromQL capabilities and
  the `/metrics` catalogue reflected directly from the binary.
