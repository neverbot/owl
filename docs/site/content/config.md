---
title: Configuration
section: Reference
nav_order: 1
---

owl reads configuration from three layers, in order of increasing
precedence:

1. **`config.yml`** — the structured fields below. Mounted into the
   container at `/etc/owl/config.yml` by default.
2. **Environment variables** — convenient for operational values and
   secrets. `OWL_LISTEN_ADDR`, `OWL_LOG_LEVEL`, `OWL_DB_PATH`,
   `OWL_ALERT_WEBHOOK_URL` shadow their YAML equivalents.
3. **CLI flags** — `--config <path>`, `--check-config`, `--version`.

Validate any file without starting the server:

```sh
owl --config /etc/owl/config.yml --check-config
```

Most fields take effect on live reload (`SIGHUP` or
`POST /-/reload`); the [Reload](/) section of the readme lists the
two that still need a restart (`listen`, and `storage.path` /
`storage.retention`).

## Schema

{{> config-schema}}

## Common scenarios

- **Pointing an external Prometheus at owl** — drop or rename the
  `owl-self` target. owl will still expose `/metrics`; whatever
  scrapes it stores the samples.
- **Keeping the SQLite footprint down** — add per-target `keep` /
  `drop` regex filters under `targets[]`. The bundled examples filter
  Traefik's bucket series, which are responsible for the bulk of its
  cardinality.
- **Hot-swapping the webhook** — change `alerts.webhook_url` and
  reload. In-flight POSTs finish against the old URL; the next state
  transition uses the new one.
- **Auto-reloading dashboard edits** — set `dashboards.watch: true`
  and an `dashboards.watch_interval`. Off by default because owl
  prefers not to poll your filesystem unless asked.
