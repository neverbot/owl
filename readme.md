# owl

Tiny, lightweight self-hosted observability. One static Go binary, one
container, one SQLite file. Built for a single low-end host where
Prometheus + Grafana is too heavy and a SaaS funnel is the wrong shape.

## What it does today

- Scrapes Prometheus-format `/metrics` endpoints on an interval, parses
  the text exposition format, persists samples to SQLite.
- Emits self-metrics from the Go runtime (`owl_runtime_goroutines`,
  `owl_runtime_alloc_bytes`, `owl_runtime_gc_pause_total_ms`) so the
  binary always has something to show.
- Stores time series in an embedded SQLite database with a dual
  retention policy: drop samples older than a time window, or once the
  database exceeds a size cap — whichever triggers first.
- Loads dashboards as JSON files from a directory. The format is a
  subset of Grafana's dashboard JSON, so a Grafana export can usually
  be dropped in and rendered with a best-effort result.
- Renders dashboards as server-rendered HTML at `/d/{id}`. A small
  vanilla-JS layer polls the API per panel and draws primitive SVG
  sparklines. No build pipeline, no SPA, no CDN.
- Evaluates a useful subset of PromQL on the fly. Panels whose queries
  fall outside the subset render with a clear "unsupported" message
  instead of breaking the whole dashboard.

## Quick start

```sh
docker pull ghcr.io/neverbot/owl:master
```

Create a `config.yml`:

```yaml
listen: "0.0.0.0:9090"

storage:
  path: "/data/owl.db"
  retention:
    time: 30d
    size: 500MB

scrape:
  default_interval: 15s
  default_timeout: 10s

targets:
  - name: traefik
    url: "http://traefik:8082/metrics"
    labels:
      job: traefik

dashboards:
  dir: "/etc/owl/dashboards"
```

Drop one or more dashboard JSON files in a directory (Grafana exports
work):

```sh
docker run --rm -d \
  --name owl \
  -p 9090:9090 \
  -v $PWD/config.yml:/etc/owl/config.yml:ro \
  -v $PWD/dashboards:/etc/owl/dashboards:ro \
  -v owl-data:/data \
  ghcr.io/neverbot/owl:master \
  --config /etc/owl/config.yml
```

Browse to `http://localhost:9090/`.

## Configuration

Three layers, in order of increasing precedence:

1. **`config.yml`** (mounted; structured fields).
2. **Environment variables** for operational values and secrets:
   - `OWL_LISTEN_ADDR`
   - `OWL_LOG_LEVEL`
   - `OWL_DB_PATH`
   - `OWL_ALERT_WEBHOOK_URL`
3. **CLI flags**: `--config <path>`, `--check-config`, `--version`.

Validate without starting:

```sh
owl --config /etc/owl/config.yml --check-config
```

## Dashboards

Each `*.json` file in `dashboards.dir` becomes a dashboard whose ID is
the filename slug. Fields honoured:

- Top level: `title`, `panels[]`, `time.from`, `time.to`, `refresh`.
- Per panel: `id`, `title`, `type` (`timeseries`, `stat`, `gauge`),
  `gridPos.{x,y,w,h}` (24-column grid),
  `fieldConfig.defaults.unit`, `targets[].expr`,
  `targets[].legendFormat`.

Unknown fields are ignored silently. Panels with an unsupported query
or panel type render an explanation in place of the chart, but the rest
of the dashboard still works.

## PromQL subset

```
metric_name
metric_name{label="value", other!="x", regex=~"foo.*"}
rate(metric_name[5m])
sum(expr)              avg(expr)   min(expr)   max(expr)   count(expr)
sum by (job) (rate(http_requests_total[1m]))
expr + scalar          scalar * expr           etc.
```

Counter resets inside `rate()` are detected (previous > current is
treated as a fresh start). Series-on-series arithmetic, `offset`,
subqueries, `histogram_quantile`, and `irate`/`increase` are not in
the subset; expressions using them parse with a clear error.

## API

| Endpoint | Description |
|---|---|
| `GET /` | Index of dashboards |
| `GET /d/{id}` | Server-rendered dashboard view |
| `GET /api/query?expr=&from=&to=&step=` | Evaluate a PromQL expression and return series JSON |
| `GET /api/dashboards` | List of dashboards |
| `GET /api/dashboards/{id}` | One dashboard with its panels |
| `GET /-/healthy` | `ok` on a healthy process |
| `GET /static/*` | Embedded JS / CSS assets |

Times are millisecond Unix timestamps.

## Build targets and runtime profile

- Container image: published to
  `ghcr.io/neverbot/owl` on every push to `master` and on every `v*`
  tag. Multi-arch (`linux/amd64`, `linux/arm64`). Built on
  `gcr.io/distroless/static-debian12:nonroot`; image size sits around
  12 MB today.
- Targets that shape the design: image ≤ 30 MB, idle RAM ≤ 20 MB,
  active RAM ≤ 40 MB, no external dependencies (no SaaS, no cloud
  sign-in, no telemetry).
- Tested on Go 1.25 with the race detector on.

## Building from source

```sh
make test        # go test ./... -race -count=1
make build       # CGO_ENABLED=0 static binary
make vet
make fmt
make tidy
```

Container:

```sh
docker build -t owl:dev .
docker run --rm owl:dev --version
```

## Status

Early. The pieces wired today are: configuration loader, SQLite
storage with retention, runtime self-metrics, HTTP scraper, query
engine for the PromQL subset above, dashboard loader, and the web
server that renders them. Host metrics from `/proc` and `/sys`,
container metrics from the Docker socket, target auto-discovery,
threshold alerting, and `SIGHUP`-driven config reload are not yet
implemented.
