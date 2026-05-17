---
title: Home
nav_order: 0
---

owl is a tiny self-hosted observability tool. One static Go binary,
one container, one SQLite file. Built for a single low-end host where
Prometheus + Grafana is too heavy and a SaaS funnel is the wrong
shape.

It scrapes Prometheus-format `/metrics` endpoints, stores time series
in embedded SQLite, evaluates a useful PromQL subset, and renders
dashboards defined as Grafana-compatible JSON. The container image
sits around 12 MB; idle RAM stays under 20 MB.

{{> chart fixture=hero-multi-series expr="demo_signal" unit=ops title="Owl"}}

## Start here

- [Getting Started](/getting-started/) — pull the image, see the
  bundled Owl Health dashboard within a scrape interval.
- [Configuration](/config/) — every YAML field, where it can be
  overridden, what reloads live.
- [PromQL](/promql/) — the supported subset, function by function.
- [/metrics](/metrics/) — owl's own metrics, scraped by owl by
  default.

## What it is not

owl is not Prometheus, not Grafana, not Datadog, not Netdata. There is
no SaaS, no cloud sign-in, no telemetry, no agent fleet. There is no
SPA, no build pipeline, no CDN. A YAML file, a directory of JSON
dashboards, a binary that runs in a distroless container — that is
the whole shape of it.
