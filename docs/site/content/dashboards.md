---
title: Dashboards
section: Reference
nav_order: 3
---

Dashboards are JSON files dropped into `dashboards.dir`. Each
`*.json` becomes a dashboard whose ID is the filename slug. The
format is a deliberate subset of Grafana's dashboard JSON, so a
Grafana export can usually be dropped in and rendered with a
best-effort result. Unknown fields are ignored silently.

## Panel shape

One panel looks like this:

```json
{
  "id": 1,
  "title": "Heap objects",
  "type": "timeseries",
  "gridPos": { "x": 0, "y": 0, "w": 12, "h": 8 },
  "fieldConfig": { "defaults": { "unit": "bytes" } },
  "targets": [
    {
      "expr": "owl_heap_objects_bytes",
      "legendFormat": "{{instance}}"
    }
  ]
}
```

A dashboard is `{ "title": "...", "panels": [ ... ] }`, optionally
with a `time.from` / `time.to` window and a `refresh` interval that
seeds the top-bar picker.

## Supported fields

- **Top level:** `title`, `panels[]`, `time.from`, `time.to`,
  `refresh`.
- **Per panel:** `id`, `title`, `type` (one of `timeseries`, `stat`,
  `gauge`), `gridPos.{x, y, w, h}` on a 24-column grid,
  `fieldConfig.defaults.unit`, `targets[].expr`,
  `targets[].legendFormat`.

Anything else in the JSON is ignored without warning so existing
Grafana exports remain syntactically valid input. Panels with an
unsupported query or `type` render a small explanation in place of
the chart; the rest of the dashboard still works.

## Panel types

{{> chart fixture=gauge-memory expr="process_resident_memory_bytes" unit=bytes title="Resident memory (gauge)"}}

- **`timeseries`** — the default. SVG line chart with axes, a
  crosshair on hover, and a tooltip listing series sorted by value.
- **`stat`** — a single big number for the latest sample. Suited to
  current goroutine count, currently firing alerts, samples in
  storage.
- **`gauge`** — a single value rendered against a min/max range.
  Useful for capacity-style readings (disk used %, memory headroom).

For dense overviews, `timeseries` with multiple series remains the
right default — owl's chart layer is designed to make many lines on
one panel legible.

{{> chart fixture=hero-multi-series expr="demo_signal" unit=ops title="Multi-series timeseries"}}

## Units

`fieldConfig.defaults.unit` is rendered in the panel's top-right
corner and used for tooltip formatting. The chart layer recognises
the common Grafana units: `bytes`, `s`, `ms`, `percent`, `ops`,
`reqps`, `short`. Anything else is shown verbatim. Counters expressed
through `rate()` typically read best with `ops` (operations per
second); histogram quantiles read best with `s` or `ms`.

## Legend templating

`legendFormat` supports Grafana-style `{{label}}` placeholders, which
are substituted with each series's label values. The bundled
**Containers** dashboard uses `{{name}}` to render one line per
container; the **Owl Health** dashboard uses bare label-free strings
because every metric there is single-instance.

```json
"legendFormat": "{{instance}} · {{mode}}"
```

If a referenced label is absent on a particular series, the
placeholder collapses to the empty string.

## Grid

`gridPos` is positions on a 24-column grid. Owl walks the panels in
file order, places each at the (`x`, `y`) you specify, and uses (`w`,
`h`) as the cell footprint. The viewport is one column wide on narrow
screens; the grid otherwise scales to its container.

## Bundled examples

The repository ships three dashboards in `dashboards/`:

- **`health.json`** — self-monitoring from `/metrics`.
- **`host.json`** — Linux host stats (opt-in via `host.enabled`).
- **`containers.json`** — per-container metrics from the Docker
  integration.

They double as worked examples: legitimate `expr`, `legendFormat`,
units and grid sizes you can copy.
