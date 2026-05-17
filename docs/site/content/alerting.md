---
title: Alerting
section: Reference
nav_order: 4
---

owl's alerter is a small fixed-interval loop that evaluates threshold
rules against the same query engine the dashboards use, then posts a
JSON event to a configured webhook on every state transition. There
is no rule UI, no notification routing tree, no silence catalogue —
the webhook is the integration point.

## Rule shape

Rules live under `alerts:` in `config.yml`. The example below parses
cleanly and would run as-is once a real `webhook_url` is provided.

```yaml config-example
listen: "0.0.0.0:9090"

storage:
  path: "/data/owl.db"
  retention:
    time: 7d
    size: 100MB

scrape:
  default_interval: 30s
  default_timeout: 10s

targets:
  - name: owl-self
    url: "http://127.0.0.1:9090/metrics"
    labels:
      job: owl

dashboards:
  dir: "/etc/owl/dashboards"

alerts:
  webhook_url: "https://hooks.example/abc"
  rules:
    - name: high_cpu
      expr: 'sum(rate(node_cpu_seconds_total{mode!="idle"}[1m])) / count(node_cpu_seconds_total{mode="idle"})'
      op: ">"
      threshold: 0.8
      for: 2m
    - name: low_disk
      expr: "node_filesystem_avail_bytes"
      op: "<"
      threshold: 1073741824
      for: 5m
    - name: webhook_stuck
      expr: "increase(owl_alerts_webhook_failures_total[10m])"
      op: ">"
      threshold: 0
      for: 0s
```

Each rule has `name`, `expr` (any expression the [PromQL
subset](/promql/) supports), `op` (`>`, `>=`, `<`, `<=`),
`threshold`, and a `for` duration the condition must hold before owl
marks the rule firing.

## Per-series fan-out

A rule **fans out across every series its expression returns**. The
`low_disk` example above produces one independent alert per
filesystem. `node_filesystem_avail_bytes{mountpoint="/"}` and
`{mountpoint="/data"}` track separate firing / resolved lifecycles
identified by their labels. One rule covers every container, every
filesystem, every interface the matcher selects.

The dedup unit is therefore `(rule_name, series_labels)`. The
webhook receives one firing event per crossing series, then one
resolved event per series that clears. Nothing is re-sent while a
pair stays in either state.

## Webhook payload

Firing event:

```json
{
  "rule":      "low_disk",
  "expr":      "node_filesystem_avail_bytes",
  "op":        "<",
  "threshold": 1073741824,
  "value":     536870912,
  "status":    "firing",
  "labels":    {"mountpoint": "/data", "fstype": "ext4"},
  "fired_at":  "2026-05-15T20:31:04Z"
}
```

Resolved event — same shape plus `resolved_at`:

```json
{
  "rule":        "low_disk",
  "expr":        "node_filesystem_avail_bytes",
  "op":          "<",
  "threshold":   1073741824,
  "value":       1610612736,
  "status":      "resolved",
  "labels":      {"mountpoint": "/data", "fstype": "ext4"},
  "fired_at":    "2026-05-15T20:31:04Z",
  "resolved_at": "2026-05-15T20:48:22Z"
}
```

The webhook URL is normally injected via `OWL_ALERT_WEBHOOK_URL` so
it stays out of YAML. The delivery sink is swapped atomically on
reload: in-flight POSTs finish against the old URL; the next state
transition uses the new one.

## Self-instrumentation

Four counters on `/metrics` describe the alerter's own state:
`owl_alerts_evaluations_total`, `owl_alerts_webhook_sends_total`,
`owl_alerts_webhook_failures_total` and `owl_alerts_firing`. Wire
them back into your rules to catch a stuck loop or a broken receiver
— the third rule in the example above does exactly that. See
[/metrics](/metrics/) for the full list.
