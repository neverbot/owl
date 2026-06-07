---
title: Metric sources
section: Operating
nav_order: 3
---

The `/targets` page is owl's live view of where data is coming from
right now. Three sections, each rendered only when its source is
configured.

## Scrape targets

HTTP endpoints owl pulls on a schedule. One row per target, with the
last scrape time, duration, sample count and error if any. Includes:

- Targets declared in `config.yml` under `targets:`.
- Targets auto-discovered from container labels by the docker
  integration (see [Docker integration](/operating/docker/)).
- The bundled `owl-self` target, which scrapes owl's own `/metrics`.

## Internal collectors

In-process sources that **write directly to storage**, bypassing the
HTTP scrape pipeline. The aggregate row shows the collector's last
tick, duration and the number of samples it produced.

Three kinds today:

| Kind | Source | Enabled by |
|---|---|---|
| `host` | `/proc` and `/sys` parsers, emits `node_*` series | `host.enabled: true` |
| `docker_metrics` | Docker daemon stats, emits `container_*` series | `docker.metrics.enabled: true` |
| `docker_discovery` | Docker daemon container list, produces scrape targets | `docker.discovery.enabled: true` |

The `docker_discovery` row reports `N of M containers opted in` so
you can immediately distinguish "the label is missing or wrong" from
"the daemon is unreachable".

## Containers

When `docker.metrics.enabled` is on, owl renders a per-container row
below the collectors table: name, compose service and project (when
labelled), image, last seen, and memory working set. This is the
detail behind the aggregate `docker_metrics` row — the actual things
owl is observing on the host.

## `/api/targets`

The same data is available as JSON. The response carries up to three
top-level keys; each is omitted when its source is not wired, so
consumers that only care about scrape targets see the same shape as
before the collectors were added.

```json
{
  "targets": [
    {
      "name": "traefik",
      "url": "http://traefik:8082/metrics",
      "labels": {"job": "traefik", "instance": "traefik"},
      "interval": 15000000000,
      "last_scrape": "2026-06-07T12:34:56Z",
      "last_samples": 142,
      "duration": 8000000
    }
  ],
  "collectors": [
    {
      "name": "host",
      "kind": "host",
      "interval": 5000000000,
      "last_collection": "2026-06-07T12:34:58Z",
      "duration": 3000000,
      "last_samples": 57
    },
    {
      "name": "docker",
      "kind": "docker_metrics",
      "interval": 10000000000,
      "last_collection": "2026-06-07T12:34:55Z",
      "duration": 12000000,
      "last_samples": 64,
      "extra": "8 containers seen"
    },
    {
      "name": "docker-discovery",
      "kind": "docker_discovery",
      "interval": 30000000000,
      "last_collection": "2026-06-07T12:34:30Z",
      "duration": 4000000,
      "last_samples": 2,
      "extra": "2 of 8 containers opted in"
    }
  ],
  "containers": [
    {
      "name": "owl",
      "image": "ghcr.io/neverbot/owl:latest",
      "compose_service": "owl",
      "compose_project": "owl-stack",
      "memory_working_set_bytes": 13107200,
      "last_seen": "2026-06-07T12:34:55Z"
    }
  ]
}
```

Durations are nanoseconds (Go's encoding default). Timestamps are
RFC 3339 in UTC.
