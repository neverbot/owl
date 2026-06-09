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
