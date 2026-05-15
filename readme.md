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

## Try it from a clone

```sh
git clone https://github.com/neverbot/owl.git
cd owl
docker compose up
```

Then browse to `http://localhost:9090/`. You should see the bundled
**Owl Runtime** dashboard with three panels driven by the binary's own
Go runtime metrics, updating every few seconds.

`docker compose up` pulls
[`ghcr.io/neverbot/owl:master`](https://github.com/neverbot/owl/pkgs/container/owl)
by default. To rebuild from your local checkout instead — useful when
hacking on the code — run `docker compose up --build`. `docker compose
down -v` removes the data volume for a clean slate.

## Deploying

The published container image is the same artefact as the quickstart,
just consumed directly:

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

A minimal `config.yml`:

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

owl ships its own PromQL parser and evaluator — small, focused on the
features a single-host operator actually uses on a dashboard. Anything
outside the subset returns a parse error with a clear "unsupported"
message that names the offending construct, and panels that use such
queries render an explanation in place of the chart (the rest of the
dashboard keeps working).

### Supported

**Selectors**

```promql
metric_name
metric_name{job="api"}
metric_name{status=~"5..", method!="OPTIONS"}
```

Label-matcher operators: `=`, `!=`, `=~` (regex match), `!~` (regex
non-match). Regex anchoring follows Prometheus's convention: the
pattern is implicitly anchored at both ends.

**Functions**

```promql
rate(metric_name[1m])
rate(http_requests_total{status="200"}[5m])
```

`rate(expr[Nw])` where `N` is a positive integer and `w` is `s`, `m`,
or `h`. Counter resets are detected: when a sample is strictly less
than its predecessor, the engine treats it as a fresh start (handles
process restarts cleanly).

**Aggregations**

```promql
sum(expr)            avg(expr)
min(expr)            max(expr)
count(expr)

sum   by (job)      (rate(http_requests_total[1m]))
avg   by (instance) (cpu_usage)
count by (status)   (http_requests_total)
```

Operators: `sum`, `avg`, `min`, `max`, `count`. The `by (labels)` form
groups output series by the listed labels. `without (labels)` is **not**
implemented yet.

**Arithmetic**

```promql
cpu_usage * 100
100 - cpu_idle_pct
node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes
errors_total / requests_total
sum by (status) (rate(http_requests_total[1m])) / sum (rate(http_requests_total[1m]))
```

Operators: `+`, `-`, `*`, `/`.

- **scalar OP expr** and **expr OP scalar** apply the op pointwise with
  the scalar.
- **expr OP expr** (series-on-series) matches LHS series to RHS series
  by **exact label set** (the metric name is dropped on output, per
  Prometheus convention). If one side has a single series and the other
  has many, the single series is broadcast against every other.
  Timestamps are inner-joined: only points that exist on both sides
  produce output.

Division by zero returns `0` (not `+Inf` / `NaN`), to keep the chart
layer clean of special-case rendering.

### Not supported

The list below is concrete. Any of these will return a parse error or
fail to match a series; the dashboard layer marks the affected panel
as "unsupported" with the engine's reason. PRs welcome.

**Functions**: `irate`, `increase`, `delta`, `idelta`, `deriv`,
`predict_linear`, `holt_winters`, `histogram_quantile`,
`*_over_time` (avg_over_time, max_over_time, …), `abs`/`ceil`/`floor`/`round`/`sqrt`/`ln`/`log2`/`log10`/`exp`,
`topk`/`bottomk`/`quantile`, `clamp`/`clamp_min`/`clamp_max`,
`label_replace`/`label_join`, `time`/`vector`/`scalar`,
`sort`/`sort_desc`, `absent`/`absent_over_time`, `changes`, `resets`.

**Aggregation operators**: `stddev`, `stdvar`, `quantile`.

**Aggregation modifiers**: `without (labels)` (only `by` is supported).

**Vector-matching modifiers**: `on(labels)`, `ignoring(labels)`,
`group_left`/`group_right` — matching is exact-label-set only.

**Modifiers / syntax**: `offset`, `@` modifier (instant queries at a
fixed time), subqueries (`expr[5m:30s]`), `__name__` regex matching,
string literals, numeric literals as a top-level expression.

**Logical / set ops**: `and`, `or`, `unless`.

**Comparison ops**: `>`, `<`, `==`, `!=`, `>=`, `<=` (relevant for
alerting once it lands).

**Operator precedence**: left-to-right only, no PEMDAS. Use parentheses
around aggregations and `rate()` calls (the parser already requires
this for the unambiguous cases); chained arithmetic without parens
binds left.

If a dashboard you care about uses one of these and it would be easy
to add, raise an issue with the exact expression — most of these are a
short addition to the parser/evaluator once a real need pins them
down.

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
storage with retention, runtime self-metrics, a Linux host collector
(`/proc` parsing, opt-in, off by default), HTTP scraper, the PromQL
subset documented above, dashboard loader, and the web server that
renders them. Container metrics from the Docker socket, target
auto-discovery, threshold alerting, and `SIGHUP`-driven config reload
are not yet implemented.

### Host metrics caveat (macOS / Windows / Docker Desktop)

On Linux hosts, owl runs in a container that shares the host kernel.
Bind-mounting `/proc:/host/proc:ro` (already in the example
`compose.yml`) gives the host collector a direct view of the real
host's CPU / memory / load / disk / net stats.

On macOS and Windows, Docker Desktop runs a hidden Linux VM. The
"host" the container sees is that VM, not the underlying OS. The host
collector will surface the VM's metrics — useful for confirming the
loop works end-to-end, but not the OS-level metrics you'd see on a
production Linux deployment. There is no clean fix from owl's side; it
is a property of Docker Desktop's architecture.
