---
title: /metrics
section: Reference
nav_order: 5
---

owl exposes its own runtime, storage, dashboard and alerter state at
`GET /metrics` in Prometheus text exposition format. The payload is
rendered on demand on each request — nothing is persisted until
something scrapes it.

## The `owl-self` target

The bundled `examples/config.yml` ships an `owl-self` scrape target
that points back at the process's own endpoint:

```yaml
targets:
  - name: owl-self
    url: "http://127.0.0.1:9090/metrics"
    labels:
      job: owl
```

That single entry is what makes the bundled **Owl Health** dashboard
light up out of the box. Drop or override it to point an external
Prometheus at the same endpoint instead — both consumers see the
same series.

With self-scrape on, expressions like
`increase(owl_alerts_webhook_failures_total[10m]) > 0` or
`rate(owl_alerts_evaluations_total[5m]) == 0` become valid alert
rules that catch a stuck alerter or a broken webhook receiver. See
[Alerting](/alerting/) for the rule shape.

## What is exported

The payload covers four families:

- **process** — `owl_goroutines`, `owl_heap_objects_bytes`,
  `owl_gc_pause_seconds_total`.
- **storage** — `owl_storage_samples_total`,
  `owl_storage_size_bytes`. The latter sums `.db`, `-wal` and `-shm`
  on disk.
- **dashboards** — `owl_dashboards_loaded`, the count of JSON files
  successfully parsed from `dashboards.dir`.
- **alerts** — `owl_alerts_evaluations_total`,
  `owl_alerts_webhook_sends_total`,
  `owl_alerts_webhook_failures_total`, `owl_alerts_firing`.

## Registry

The table below is generated from `internal/web.Registry()`, the
single source of truth that both the runtime handler and this docs
generator iterate. Adding a metric in one place is enough; this page
re-renders to match.

{{> metrics-table}}
